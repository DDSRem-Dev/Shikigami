#!/usr/bin/env python3
"""Build only the opt-in diagnostic and run authenticated published binaries."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", required=True, help="Go 1.26.10 executable for the diagnostic only")
    args = parser.parse_args()
    source = Path(__file__).resolve().parent
    repository = source.parents[2]
    # Nested plugin CoreAPI and go-plugin sockets must fit Darwin sockaddr_un.
    output = Path(tempfile.mkdtemp(prefix="shiki-rc-", dir="/tmp"))
    (output / "evidence").mkdir(mode=0o700)
    shutil.copytree(source, output / "fixture-source", ignore=shutil.ignore_patterns("__pycache__"))
    manifest = {str(p.relative_to(repository)): digest(p) for p in source.glob("*.go")}
    manifest.update({name: digest(repository / name) for name in ("go.mod", "go.sum", "configs/release-signing.pub")})
    (output / "fixture-source.json").write_text(json.dumps(manifest, indent=2) + "\n")
    environment = os.environ.copy()
    environment.update(GOTOOLCHAIN="local", GOMAXPROCS="2", GOFLAGS="-p=1 -mod=readonly -buildvcs=false")
    commands = [
        ("build", [args.go, "test", "-c", "-tags=releasecompat", "-o", str(output / "compat.test"), "./test/diagnostics/releasecompat"]),
        ("run", [str(output / "compat.test"), "-test.v", "-test.timeout=8m", "-test.run=^TestPublicReleaseVersionBoundaries$"]),
    ]
    environment.update(SHIKI_RELEASE_COMPAT_ROOT=str(output / "evidence"), SHIKI_RELEASE_COMPAT_REPO=str(repository))
    print(output, flush=True)
    result = 0
    for label, command in commands:
        started = time.monotonic()
        with (output / (label + ".log")).open("w") as log:
            completed = subprocess.run(command, cwd=repository, env=environment, stdout=log, stderr=subprocess.STDOUT)
        record = {"argv": command, "exit_code": completed.returncode, "elapsed_seconds": time.monotonic() - started,
                  "gomaxprocs": "2", "goflags": environment["GOFLAGS"]}
        (output / (label + ".json")).write_text(json.dumps(record, indent=2) + "\n")
        print(label, completed.returncode, flush=True)
        result = completed.returncode
        if result:
            print((output / (label + ".log")).read_text(), flush=True)
            break
    receipt = {"fixture_inputs_unchanged": all(digest(repository / name) == value for name, value in manifest.items()),
               "diagnostic_sha256": digest(output / "compat.test") if (output / "compat.test").exists() else None,
               "product_binaries_rebuilt": False, "exit_code": result}
    (output / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
    return result


if __name__ == "__main__":
    raise SystemExit(main())
