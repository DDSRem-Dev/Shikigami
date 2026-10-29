#!/usr/bin/env python3
"""Render the Homebrew formula only from an authenticated fixed release bundle."""
import argparse
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import sys
import tarfile

sys.dont_write_bytecode = True
VERSION = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(alpha|beta|rc)\.([1-9][0-9]*))?\Z")
TARGETS = (("darwin", "arm64"), ("darwin", "amd64"), ("linux", "arm64"), ("linux", "amd64"))
MEMBERS = sorted(["shiki", "shiki-agent", "shiki-daemon", "shiki-install", "shiki-plugin-sync", "shiki-plugin-port", "sync.manifest.json", "port.manifest.json", "release.json", "SHA256SUMS", "install.sh", "install-client.sh", "uninstall.sh", "LICENSE"])


# Exact complete license texts, not keyword guesses. The GPL text itself is the
# canonical v3 license; the project's GPL-3.0-only grant is documented in README.
LICENSE_DIGESTS = {
    "3b304152d2a01132a28f195757839b99ecf9cd4564f9e75a661111170e1634fd": "MIT",
    "3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986": "GPL-3.0-only",
}


def archive_license(path, expected):
    with path.open("rb") as stream:
        before = os.fstat(stream.fileno())
        digest = hashlib.sha256()
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
        checksum = digest.hexdigest()
        if before.st_size != expected["size"] or "sha256:" + checksum != expected["digest"]:
            raise ValueError("archive changed after authenticated input verification")
        stream.seek(0)
        licenses = []
        with tarfile.open(fileobj=stream, mode="r|gz") as archive:
            for count, member in enumerate(archive, 1):
                if count > 32:
                    raise ValueError("unexpected release archive membership")
                if member.name != "LICENSE":
                    continue
                if not member.isfile() or not 0 < member.size <= 65536:
                    raise ValueError("invalid authenticated LICENSE member")
                licenses.append(archive.extractfile(member).read())
        after = os.fstat(stream.fileno())
        current = path.lstat()
        fields = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")
        if any(getattr(before, key) != getattr(after, key) or getattr(before, key) != getattr(current, key) for key in fields):
            raise ValueError("archive changed while reading LICENSE")
    if len(licenses) != 1:
        raise ValueError("exactly one authenticated LICENSE is required")
    value = LICENSE_DIGESTS.get(hashlib.sha256(licenses[0]).hexdigest())
    if value is None:
        raise ValueError("unknown authenticated LICENSE text")
    return value


def render_formula(version, repository, directory, expected_assets, fingerprint, public_key_path):
    """Inputs must already have passed release-publish.verify_inputs.

    Recheck digests while consuming signatures; embed authenticated metadata so
    Homebrew also checks Ed25519 and member hashes before installing binaries.
    """
    directory = Path(directory)
    if not VERSION.fullmatch(version) or repository != "DDSRem-Dev/Shikigami":
        raise ValueError("unsupported fixed release identity")
    if not re.fullmatch(r"[a-f0-9]{64}", fingerprint):
        raise ValueError("invalid signing fingerprint")
    public = Path(public_key_path).read_text()
    if not re.fullmatch(r"-----BEGIN PUBLIC KEY-----\n[A-Za-z0-9+/=\n]+-----END PUBLIC KEY-----\n?", public):
        raise ValueError("invalid public key PEM")
    metadata = {}
    licenses = set()
    for system, arch in TARGETS:
        name = f"shikigami-{version}-{system}-{arch}.tar.gz"
        signed = name + ".sig.json"
        raw = (directory / signed).read_bytes()
        expected = expected_assets[signed]
        if expected["size"] != len(raw) or expected["digest"] != "sha256:" + hashlib.sha256(raw).hexdigest():
            raise ValueError("signature changed after authenticated input verification")
        envelope = json.loads(raw)
        payload = json.loads(base64.b64decode(envelope["payload"], validate=True))
        archive = expected_assets[name]
        if payload["version"] != version or payload["os"] != system or payload["arch"] != arch or payload["archive"] != name or archive["digest"] != "sha256:" + payload["archive_sha256"] or archive["size"] != payload["archive_size"]:
            raise ValueError("authenticated target metadata mismatch")
        licenses.add(archive_license(directory / name, archive))
        metadata[f"{system}-{arch}"] = envelope
    if len(licenses) != 1:
        raise ValueError("release targets disagree on LICENSE")
    license_id = licenses.pop()
    declarations = []
    for system in ("darwin", "linux"):
        declarations.append(f"  on_{'macos' if system == 'darwin' else 'linux'} do")
        for arch in ("arm64", "amd64"):
            name = f"shikigami-{version}-{system}-{arch}.tar.gz"
            declarations += [f"    on_{'arm' if arch == 'arm64' else 'intel'} do", f'      url "https://github.com/{repository}/releases/download/{version}/{name}"', f'      sha256 "{expected_assets[name]["digest"].removeprefix("sha256:")}"', "    end"]
        declarations.append("  end")
    return TEMPLATE.replace("@VERSION@", version).replace("@DECLARATIONS@", "\n".join(declarations)).replace("@BREW_VERSION@", version[1:]).replace("@METADATA@", json.dumps(metadata, sort_keys=True, separators=(",", ":"))).replace("@PUBLIC@", public.rstrip()).replace("@FINGERPRINT@", fingerprint).replace("@MEMBERS@", json.dumps(MEMBERS)).replace("@LICENSE@", license_id)


TEMPLATE = '''# Generated by scripts/homebrew-formula.py from verified release assets.
# shikigami-release: @VERSION@
require "digest"
require "json"
require "openssl"

class Shikigami < Formula
  desc "Terminal sessions, file synchronization and TCP forwarding through a local agent"
  homepage "https://github.com/DDSRem-Dev/Shikigami"
  version "@BREW_VERSION@"
  license "@LICENSE@"
  skip_clean "libexec"

@DECLARATIONS@

  RELEASE_SIGNATURES = JSON.parse(<<~'JSON').freeze
    @METADATA@
  JSON
  RELEASE_PUBLIC_KEY = <<~'PEM'.freeze
@PUBLIC@
  PEM
  RELEASE_FINGERPRINT = "@FINGERPRINT@".freeze
  BUNDLE_MEMBERS = @MEMBERS@.freeze

  def release_target
    "#{OS.mac? ? 'darwin' : 'linux'}-#{Hardware::CPU.arm? ? 'arm64' : 'amd64'}"
  end

  def signed_payload
    envelope = RELEASE_SIGNATURES.fetch(release_target)
    key = OpenSSL::PKey.read(RELEASE_PUBLIC_KEY)
    odie "Unexpected release signing key" unless key.oid == "ED25519" && Digest::SHA256.hexdigest(key.public_to_der) == RELEASE_FINGERPRINT
    payload = envelope.fetch("payload").unpack1("m0")
    signature = envelope.fetch("signature").unpack1("m0")
    odie "Invalid release signature" unless envelope.fetch("format") == 1 && key.verify(nil, signature, "Shikigami release archive signature\\x00format=1\\x00" + payload)
    metadata = JSON.parse(payload)
    system, arch = release_target.split("-")
    expected = ["archive", "archive_sha256", "archive_size", "arch", "checksums_sha256", "os", "version"].sort
    odie "Unexpected signed release identity" unless metadata.keys.sort == expected && metadata.fetch("version") == "@VERSION@" && metadata.fetch("os") == system && metadata.fetch("arch") == arch && metadata.fetch("archive") == "shikigami-@VERSION@-#{release_target}.tar.gz"
    metadata
  end

  def install
    metadata = signed_payload
    # Homebrew verifies this fixed checksum before extraction; this also checks
    # that the downloaded archive matches the authenticated Ed25519 metadata.
    odie "Release archive differs from signature" unless cached_download.size == metadata.fetch("archive_size") && Digest::SHA256.file(cached_download).hexdigest == metadata.fetch("archive_sha256")
    # Homebrew may add .brew_home to its staging directory. Only the fixed,
    # authenticated members below are inspected and copied into libexec.
    BUNDLE_MEMBERS.each do |name|
      member = buildpath/name
      odie "Non-regular bundle member" unless member.file? && !member.symlink?
    end
    odie "Release checksum pin mismatch" unless Digest::SHA256.file("SHA256SUMS").hexdigest == metadata.fetch("checksums_sha256")
    names = []
    File.readlines("SHA256SUMS", chomp: true).each do |line|
      match = /\\A([a-f0-9]{64})  ([A-Za-z0-9._-]+)\\z/.match(line)
      odie "Invalid release checksums" unless match && BUNDLE_MEMBERS.include?(match[2]) && !names.include?(match[2])
      odie "Release payload checksum mismatch" unless Digest::SHA256.file(match[2]).hexdigest == match[1]
      names << match[2]
    end
    odie "Incomplete release checksums" unless names.sort == BUNDLE_MEMBERS - ["SHA256SUMS"]
    bundle = libexec/"bundle"
    bundle.install BUNDLE_MEMBERS
    # Homebrew 4/7 move top-level libexec metafiles. A nested complete bundle
    # preserves LICENSE and every authenticated member across those versions.
    cp bundle/"LICENSE", prefix/"LICENSE"
    bin.install_symlink bundle/"shiki", bundle/"shiki-agent", bundle/"shiki-daemon", bundle/"shiki-install"
    (share/"shikigami").mkpath
    (share/"shikigami/checksums-pin").write metadata.fetch("checksums_sha256") + "\\n"
    (share/"shikigami/release-signing.pub").write RELEASE_PUBLIC_KEY
  end

  def caveats
    <<~EOS
      This installs an authenticated bundle, without activating services or login.
      To review a client installation before activation:
        #{opt_bin}/shiki-install install --role client --bundle #{opt_libexec}/bundle --version @VERSION@ --checksums-sha256 $(cat #{opt_share}/shikigami/checksums-pin) --stage-only
      Remove --stage-only only when ready to activate the managed client installation.
      The installer copies into its own versioned prefix and installs matching plugins.
      brew upgrade/uninstall changes the Homebrew bundle, not the managed service or data.
      Use shiki-install install to upgrade a managed installation; use its uninstall command
      before removing a managed service. No brew services entry is provided.
    EOS
  end

  test do
    assert_match "shiki @VERSION@", shell_output("#{bin}/shiki --version")
    assert_path_exists libexec/"bundle/sync.manifest.json"
    assert_path_exists libexec/"bundle/port.manifest.json"
    args = [bin/"shiki-install", "install", "--role", "client", "--bundle", libexec/"bundle",
            "--version", "@VERSION@", "--checksums-sha256", (share/"shikigami/checksums-pin").read.strip,
            "--prefix", testpath/"managed", "--config", testpath/"client.yaml",
            "--state-dir", testpath/"state", "--plugins-dir", testpath/"plugins",
            "--bin-dir", testpath/"commands", "--path", "/usr/bin:/bin", "--non-interactive", "--stage-only"]
    2.times { system(*args) }
    refute_path_exists testpath/"managed/current"
    refute_path_exists testpath/"client.yaml"
    refute_path_exists testpath/"state"
    refute_path_exists testpath/"plugins"
  end
end
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--repository", default="DDSRem-Dev/Shikigami")
    parser.add_argument("--commit", required=True)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--verifier", type=Path, required=True)
    parser.add_argument("--public-key", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location("release_publish", Path(__file__).with_name("release-publish.py"))
    publish = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(publish)
    expected, fingerprint = publish.verify_inputs(args.directory, args.version, args.repository, args.commit, args.run_id, args.verifier, args.public_key)
    rendered = render_formula(args.version, args.repository, args.directory, expected, fingerprint, args.public_key)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(rendered)


if __name__ == "__main__":
    main()
