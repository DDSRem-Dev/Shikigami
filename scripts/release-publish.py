#!/usr/bin/env python3
"""Verify signed release inputs, then publish an exact, resumable GitHub draft."""

import argparse
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile


SPEC = importlib.util.spec_from_file_location("release_actions", Path(__file__).with_name("release-actions.py"))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


def verify_inputs(directory, version, repository, commit, run_id, verifier, public_key):
    release.validate_version(version)
    names = {"PROVENANCE.json", "release-signing.pub"}
    for system, arch in release.TARGETS:
        base = f"shikigami-{version}-{system}-{arch}.tar.gz"
        names.update(base + suffix for suffix in ("", ".sha256", ".checksums-pin", ".sig.json", ".verification.json"))
    if {p.name for p in directory.iterdir()} != names:
        raise ValueError("exactly 22 signed release files are required")
    if any((directory / name).stat().st_size > 65536 for name in names if not name.endswith(".tar.gz")):
        raise ValueError("release metadata exceeds its size limit")
    digests = {name: release.file_digest(directory / name) for name in names}
    provenance = {"version": version, "source_commit": commit, "signing_workflow_commit": commit,
                  "repository": repository, "tag": version, "release_run_id": run_id}
    if json.loads((directory / "PROVENANCE.json").read_text()) != provenance:
        raise ValueError("signed artifact provenance does not match this release run")
    if (directory / "release-signing.pub").read_bytes() != public_key.read_bytes():
        raise ValueError("artifact public key differs from the trusted repository key")
    fingerprint = None
    with tempfile.TemporaryDirectory(prefix="shiki-publish-verify-") as temporary:
        for system, arch in release.TARGETS:
            bundle = release.validate_bundle(directory, version, system, arch)
            name = bundle["archive"]
            receipt = json.loads(subprocess.check_output([
                str(verifier), "verify", "--archive", str(directory / name),
                "--signature", str(directory / (name + ".sig.json")),
                "--public-key", str(public_key), "--output", str(Path(temporary) / f"{system}-{arch}"),
                "--version", version, "--os", system, "--arch", arch], timeout=120))
            receipt.pop("archive")
            if receipt != json.loads((directory / (name + ".verification.json")).read_text()):
                raise ValueError("signed artifact verification receipt mismatch")
            fingerprint = receipt["key_fingerprint_sha256"]
    return {name: {"size": (directory / name).stat().st_size, "digest": "sha256:" + digests[name]}
            for name in sorted(names)}, fingerprint


def find_release(repository, version):
    found = release.github_api(f"repos/{repository}/releases/tags/{version}", missing=True)
    if found is not None:
        return found
    # The tag endpoint may omit drafts. A private draft is still a reservation.
    for page in range(1, 101):
        rows = release.github_api(f"repos/{repository}/releases?per_page=100&page={page}")
        for row in rows:
            if row["tag_name"] == version:
                return row
        if len(rows) < 100:
            return None
    raise ValueError("release inventory exceeded its bounded lookup")


def validate_existing(existing, version, commit, marker):
    if (existing.get("tag_name") != version or existing.get("target_commitish") != commit
            or existing.get("prerelease") != ("-" in version)
            or marker not in existing.get("body", "")):
        raise ValueError("existing release belongs to a different source or workflow run")


def missing_assets(existing, expected):
    seen = set()
    for asset in existing:
        name = asset.get("name")
        if name not in expected or name in seen:
            raise ValueError("unexpected or duplicate remote release asset")
        seen.add(name)
        if (asset.get("state") != "uploaded" or asset.get("size") != expected[name]["size"]
                or asset.get("digest") != expected[name]["digest"]):
            raise ValueError("remote asset differs from verified input; refusing to overwrite")
    return sorted(set(expected) - seen)


def release_body(repository, version, commit, run_id, fingerprint, notes_directory, marker):
    path = notes_directory / (version + ".md")
    if path.is_symlink():
        raise ValueError("release notes must be a regular file")
    if path.exists():
        if not path.is_file() or path.stat().st_size > 100000:
            raise ValueError("release notes are not a bounded regular file")
        notes = path.read_text()
    else:
        notes = release.github_api(f"repos/{repository}/releases/generate-notes", "POST",
                                   {"tag_name": version, "target_commitish": commit})["body"]
    base = f"https://github.com/{repository}"
    return (notes.rstrip() + "\n\n" + marker + "\n\n"
            f"Source: `{commit}` · [Release workflow]({base}/actions/runs/{run_id})\n\n"
            f"Ed25519 SPKI SHA-256: `{fingerprint}`. Verify each archive and its `.sig.json` "
            f"using the [trusted public key]({base}/blob/{commit}/configs/release-signing.pub) "
            f"and [verification instructions]({base}/blob/{commit}/docs/11-automated-installation.md#本地离线签名与验签). "
            "A checksum or public key bundled with a download is not an independent trust source.\n\n"
            f"[Validation scope and remaining gates]({base}/blob/{commit}/docs/18-release-readiness.md).\n")


def publish(repository, version, commit, run_id, directory, expected, fingerprint, notes_directory):
    marker = "<!-- shikigami-release:" + json.dumps(
        {"tag": version, "source": commit, "run": run_id}, sort_keys=True, separators=(",", ":")) + " -->"
    release.validate_source(repository, "refs/tags/" + version, commit, run_id)
    current = find_release(repository, version)
    if current is None:
        body = release_body(repository, version, commit, run_id, fingerprint, notes_directory, marker)
        current = release.github_api(f"repos/{repository}/releases", "POST", {
            "tag_name": version, "target_commitish": commit, "name": version,
            "body": body, "draft": True, "prerelease": "-" in version})
    validate_existing(current, version, commit, marker)
    release_id = current["id"]
    endpoint = f"repos/{repository}/releases/{release_id}"
    assets = release.github_api(endpoint + "/assets?per_page=100")
    missing = missing_assets(assets, expected)
    if not current["draft"]:
        if missing:
            raise ValueError("published release is incomplete; refusing to modify it")
        return current["html_url"]
    for name in missing:
        # No --clobber: even our own draft's conflicting assets are preserved.
        subprocess.run(["gh", "release", "upload", version, str(directory / name), "--repo", repository],
                       check=True, timeout=300)
    assets = release.github_api(endpoint + "/assets?per_page=100")
    if missing_assets(assets, expected):
        raise ValueError("draft upload is incomplete")
    release.validate_source(repository, "refs/tags/" + version, commit, run_id)
    current = release.github_api(endpoint)
    validate_existing(current, version, commit, marker)
    if not current["draft"]:
        return current["html_url"]
    result = release.github_api(endpoint, "PATCH", {
        "draft": False, "make_latest": "false" if "-" in version else "legacy"})
    validate_existing(result, version, commit, marker)
    if result.get("draft"):
        raise ValueError("GitHub did not confirm release publication")
    return result["html_url"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("repository", "version", "commit", "run-id"):
        parser.add_argument("--" + name, required=True)
    for name in ("directory", "verifier", "public-key", "notes-directory"):
        parser.add_argument("--" + name, type=Path, required=True)
    args = parser.parse_args()
    # Authenticate the source before using identifiers in remote requests.
    release.validate_source(args.repository, "refs/tags/" + args.version, args.commit, args.run_id)
    assets, fingerprint = verify_inputs(args.directory, args.version, args.repository, args.commit,
                                        args.run_id, args.verifier, args.public_key)
    print(publish(args.repository, args.version, args.commit, args.run_id, args.directory,
                  assets, fingerprint, args.notes_directory))


if __name__ == "__main__":
    main()
