#!/usr/bin/env python3
"""Validate the CI source and unsigned bundles before the signing job gets a key."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tarfile


TARGETS = [(system, arch) for system in ("linux", "darwin") for arch in ("amd64", "arm64")]
PAYLOADS = {"shiki", "shiki-agent", "shiki-daemon", "shiki-install", "shiki-plugin-sync",
            "shiki-plugin-port", "sync.manifest.json", "port.manifest.json", "install.sh",
            "install-client.sh", "uninstall.sh", "LICENSE", "release.json", "SHA256SUMS"}


def validate_version(version):
    if len(version) > 80 or not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)-(?:alpha|beta|rc)\.[1-9][0-9]*", version):
        raise ValueError("an explicit alpha/beta/rc prerelease version is required")


def validate_run(run, repository, run_id):
    if not re.fullmatch(r"[1-9][0-9]*", run_id):
        raise ValueError("CI run ID must be a positive integer")
    if (run.get("id") != int(run_id) or run.get("repository", {}).get("full_name") != repository
            or run.get("head_repository", {}).get("full_name") != repository
            or run.get("path") != ".github/workflows/ci.yml"
            or run.get("event") != "push" or run.get("head_branch") != "master"
            or run.get("status") != "completed" or run.get("conclusion") != "success"
            or not re.fullmatch(r"[0-9a-f]{40}", run.get("head_sha", ""))):
        raise ValueError("signing requires successful completed CI for a push to this repository's master")
    return run["head_sha"]


def file_digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > (8 << 30):
        raise ValueError("bundle inputs must be regular files")
    with path.open("rb") as stream:
        return stream_digest(stream)


def stream_digest(stream):
    digest = hashlib.sha256()
    for block in iter(lambda: stream.read(1 << 20), b""):
        digest.update(block)
    return digest.hexdigest()


def validate_bundle(directory, version, system, arch):
    name = f"shikigami-{version}-{system}-{arch}.tar.gz"
    archive = directory / name
    digest = file_digest(archive)
    for suffix in (".sha256", ".checksums-pin"):
        path = directory / (name + suffix)
        if path.is_symlink() or not path.is_file() or path.stat().st_size > 1024:
            raise ValueError("invalid sidecar file")
    if (directory / (name + ".sha256")).read_text() != f"{digest}  {name}\n":
        raise ValueError("archive checksum mismatch")
    pin = (directory / (name + ".checksums-pin")).read_text()
    if not re.fullmatch(r"[0-9a-f]{64}\n", pin):
        raise ValueError("invalid checksum pin")
    hashes, metadata = {}, {}
    with tarfile.open(archive, "r|gz") as bundle:
        for member in bundle:
            if (member.name not in PAYLOADS or member.name in hashes or not member.isfile()
                    or member.size < 0 or member.size > (1 << 30)):
                raise ValueError("unexpected, duplicate, unsafe or oversized archive member")
            stream = bundle.extractfile(member)
            if member.name in ("SHA256SUMS", "release.json"):
                if member.size > 65536:
                    raise ValueError("oversized bundle metadata")
                data = stream.read()
                metadata[member.name] = data
                hashes[member.name] = hashlib.sha256(data).hexdigest()
            else:
                hashes[member.name] = stream_digest(stream)
    if set(hashes) != PAYLOADS or hashes["SHA256SUMS"] != pin.strip():
        raise ValueError("bundle membership or checksum pin mismatch")
    expected = {}
    for line in metadata["SHA256SUMS"].decode().splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9._-]+)", line)
        if not match or match[2] in expected:
            raise ValueError("invalid or duplicate payload checksum")
        expected[match[2]] = match[1]
    if expected != {name: digest for name, digest in hashes.items() if name != "SHA256SUMS"}:
        raise ValueError("payload checksum mismatch")
    manifest = json.loads(metadata["release.json"])
    if (manifest.get("version"), manifest.get("os"), manifest.get("arch")) != (version, system, arch):
        raise ValueError("release identity mismatch")
    return {"archive": name, "sha256": digest, "checksums_sha256": pin.strip()}


def validate_bundles(directory, version):
    validate_version(version)
    expected = {f"shikigami-{version}-{system}-{arch}.tar.gz{suffix}"
                for system, arch in TARGETS for suffix in ("", ".sha256", ".checksums-pin")}
    if {p.name for p in directory.iterdir()} != expected:
        raise ValueError("exactly four unsigned target bundles and their sidecars are required")
    return [validate_bundle(directory, version, system, arch) for system, arch in TARGETS]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    gate = commands.add_parser("gate")
    gate.add_argument("--repository", required=True)
    gate.add_argument("--run-id", required=True)
    gate.add_argument("--version", required=True)
    gate.add_argument("--output", type=Path, required=True)
    bundles = commands.add_parser("bundles")
    bundles.add_argument("--directory", type=Path, required=True)
    bundles.add_argument("--version", required=True)
    args = parser.parse_args()
    if args.command == "gate":
        validate_version(args.version)
        if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", args.repository):
            raise ValueError("invalid repository")
        if not re.fullmatch(r"[1-9][0-9]*", args.run_id):
            raise ValueError("CI run ID must be a positive integer")
        response = subprocess.check_output(["gh", "api", f"repos/{args.repository}/actions/runs/{args.run_id}"], timeout=30)
        commit = validate_run(json.loads(response), args.repository, args.run_id)
        with args.output.open("a") as output:
            output.write(f"commit={commit}\nversion={args.version}\nci_run_id={args.run_id}\n")
    else:
        print(json.dumps(validate_bundles(args.directory, args.version), indent=2))


if __name__ == "__main__":
    main()
