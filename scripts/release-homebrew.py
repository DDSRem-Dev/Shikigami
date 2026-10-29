#!/usr/bin/env python3
"""Advance the repository's Homebrew formula only from a verified public release."""

import argparse
import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import re


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


publish = module("homebrew_publish", "release-publish.py")
release = publish.release
FORMULA = "Formula/shikigami.rb"


def version_order(version):
    release.validate_version(version)
    base, _, suffix = version[1:].partition("-")
    numbers = tuple(map(int, base.split(".")))
    if not suffix:
        return numbers + (3, 0)
    stage, sequence = suffix.split(".")
    return numbers + ({"alpha": 0, "beta": 1, "rc": 2}[stage], int(sequence))


def formula_version(content):
    markers = re.findall(r"^# shikigami-release: (\S+)$", content, re.M)
    versions = re.findall(r'^  version "([^"\n]+)"$', content, re.M)
    if len(markers) != 1 or len(versions) != 1 or markers[0][1:] != versions[0]:
        raise ValueError("formula must have one matching release marker and version")
    release.validate_version(markers[0])
    return markers[0]


def blob_hash(content):
    data = content.encode()
    return hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()


def decode_formula(current):
    if (current.get("type") != "file" or current.get("path") != FORMULA
            or current.get("encoding") != "base64" or current.get("size", 0) > 100000):
        raise ValueError("remote formula is not a bounded regular file")
    encoded = current.get("content", "")
    if len(encoded) > 140000:
        raise ValueError("remote formula exceeds its content limit")
    text = base64.b64decode("".join(encoded.split()), validate=True).decode()
    if len(text.encode()) != current.get("size") or blob_hash(text) != current.get("sha"):
        raise ValueError("remote formula content does not match its Git identity")
    return text


def update_formula(repository, version, content):
    if formula_version(content) != version or len(content.encode()) > 100000:
        raise ValueError("generated formula identity or size is invalid")
    endpoint = f"repos/{repository}/contents/{FORMULA}"
    for attempt in range(3):
        current = release.github_api(endpoint + "?ref=master", missing=True)
        if current is not None:
            previous = decode_formula(current)
            old_version = formula_version(previous)
            if old_version == version:
                if previous != content:
                    raise ValueError("same-version formula differs; refusing to overwrite")
                return {"status": "unchanged", "version": version, "sha": current["sha"]}
            if "-" not in old_version and "-" in version:
                return {"status": "stable-channel-retained", "version": old_version}
            if version_order(old_version) > version_order(version):
                return {"status": "newer-version-retained", "version": old_version}
        request = {"message": f"chore: update Homebrew formula to {version}", "branch": "master",
                   "content": base64.b64encode(content.encode()).decode()}
        if current is not None:
            request["sha"] = current["sha"]
        try:
            result = release.github_api(endpoint, "PUT", request)
        except RuntimeError as error:
            # Compare-and-swap races may retry after rereading and rechecking the
            # version. Authentication, policy and other failures must stay visible.
            if "(HTTP 409)" not in str(error) or attempt == 2:
                raise
            continue
        if (result.get("content", {}).get("path") != FORMULA
                or result.get("content", {}).get("sha") != blob_hash(content)
                or not re.fullmatch(r"[0-9a-f]{40}", result.get("commit", {}).get("sha", ""))):
            raise ValueError("GitHub did not confirm the expected formula commit")
        return {"status": "updated", "version": version, "commit": result["commit"]["sha"]}
    raise AssertionError("unreachable formula update state")


def require_public_release(repository, version, commit, run_id, expected):
    release.validate_source(repository, "refs/tags/" + version, commit, run_id)
    current = release.github_api(f"repos/{repository}/releases/tags/{version}")
    marker = "<!-- shikigami-release:" + json.dumps(
        {"tag": version, "source": commit, "run": run_id}, sort_keys=True, separators=(",", ":")) + " -->"
    publish.validate_existing(current, version, commit, marker)
    if current.get("draft") is not False or not current.get("published_at"):
        raise ValueError("Homebrew may only reference a published release")
    assets = release.github_api(f"repos/{repository}/releases/{current['id']}/assets?per_page=100")
    if publish.missing_assets(assets, expected):
        raise ValueError("public release assets are incomplete")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("repository", "version", "commit", "run-id"):
        parser.add_argument("--" + name, required=True)
    for name in ("directory", "verifier", "public-key"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--output", type=Path, help="Write reviewed formula locally without publishing it")
    args = parser.parse_args()
    release.validate_source(args.repository, "refs/tags/" + args.version, args.commit, args.run_id)
    assets, fingerprint = publish.verify_inputs(args.directory, args.version, args.repository,
                                               args.commit, args.run_id, args.verifier, args.public_key)
    renderer = module("homebrew_formula", "homebrew-formula.py")
    content = renderer.render_formula(args.version, args.repository, args.directory,
                                      assets, fingerprint, args.public_key)
    require_public_release(args.repository, args.version, args.commit, args.run_id, assets)
    if args.output:
        with args.output.open("x") as output:
            output.write(content)
        print(json.dumps({"status": "prepared", "path": str(args.output)}))
    else:
        print(json.dumps(update_formula(args.repository, args.version, content)))


if __name__ == "__main__":
    main()
