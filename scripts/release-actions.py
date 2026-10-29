#!/usr/bin/env python3
"""Validate fixed tag sources and bundles before release signing or publication."""

import argparse
from datetime import datetime
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
    if len(version) > 80 or not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:alpha|beta|rc)\.[1-9][0-9]*)?", version):
        raise ValueError("an explicit stable or alpha/beta/rc version tag is required")
    return "-" in version


def github_api(path, method="GET", data=None, missing=False):
    command = ["gh", "api", "--method", method, path]
    if data is not None:
        command += ["--input", "-"]
    result = subprocess.run(command, input=None if data is None else json.dumps(data),
                            text=True, capture_output=True, timeout=60)
    if result.returncode:
        if missing and "(HTTP 404)" in result.stderr:
            return None
        raise RuntimeError("GitHub API request failed: " + result.stderr.strip())
    return json.loads(result.stdout) if result.stdout.strip() else None


def validate_source(repository, ref, commit, run_id):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("invalid repository")
    if not ref.startswith("refs/tags/"):
        raise ValueError("release requires a tag push")
    version = ref.removeprefix("refs/tags/")
    validate_version(version)
    if not re.fullmatch(r"[0-9a-f]{40}", commit) or not re.fullmatch(r"[1-9][0-9]*", run_id):
        raise ValueError("invalid source commit or workflow run")
    run = github_api(f"repos/{repository}/actions/runs/{run_id}")
    if (run.get("id") != int(run_id) or run.get("repository", {}).get("full_name") != repository
            or run.get("head_repository", {}).get("full_name") != repository
            or run.get("event") != "push" or run.get("path") != ".github/workflows/release.yml"
            or run.get("head_sha") != commit or run.get("head_branch") != version):
        raise ValueError("release run does not match this repository's tag source")
    created = run.get("created_at", "")
    if not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z", created):
        raise ValueError("release run has no fixed build start time")
    datetime.strptime(created, "%Y-%m-%dT%H:%M:%SZ")
    tag = github_api(f"repos/{repository}/git/ref/tags/{version}")
    obj = tag.get("object", {})
    for _ in range(8):
        if obj.get("type") != "tag":
            break
        if not re.fullmatch(r"[0-9a-f]{40}", obj.get("sha", "")):
            raise ValueError("invalid annotated tag object")
        obj = github_api(f"repos/{repository}/git/tags/{obj['sha']}").get("object", {})
    if obj.get("type") != "commit" or obj.get("sha") != commit:
        raise ValueError("version tag moved or does not name this source commit")
    comparison = github_api(f"repos/{repository}/compare/{commit}...master")
    if (comparison.get("status") not in ("ahead", "identical")
            or comparison.get("merge_base_commit", {}).get("sha") != commit):
        raise ValueError("release source must be reachable from master")
    return {"commit": commit, "version": version, "built_at": created,
            "prerelease": str("-" in version).lower()}


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
    gate = commands.add_parser("tag")
    gate.add_argument("--repository", required=True)
    gate.add_argument("--ref", required=True)
    gate.add_argument("--commit", required=True)
    gate.add_argument("--run-id", required=True)
    gate.add_argument("--output", type=Path)
    bundles = commands.add_parser("bundles")
    bundles.add_argument("--directory", type=Path, required=True)
    bundles.add_argument("--version", required=True)
    args = parser.parse_args()
    if args.command == "tag":
        result = validate_source(args.repository, args.ref, args.commit, args.run_id)
        if args.output:
            with args.output.open("a") as output:
                for name, value in result.items():
                    output.write(f"{name}={value}\n")
        else:
            print(json.dumps(result))
    else:
        print(json.dumps(validate_bundles(args.directory, args.version), indent=2))


if __name__ == "__main__":
    main()
