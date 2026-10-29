#!/usr/bin/env python3
"""Installer free-space checks on a dedicated 16 MiB Linux tmpfs, as UID 1000."""
import argparse
import hashlib
import json
import os
import pathlib
import shutil
import signal
import subprocess
import tempfile
import time
import uuid

IMAGE = "golang@sha256:632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=pathlib.Path, default=pathlib.Path(__file__).resolve().parent.parent)
    parser.add_argument("--output", type=pathlib.Path)
    args = parser.parse_args()
    if args.output:
        output = args.output.resolve()
        output.mkdir(mode=0o700)
    else:
        output = pathlib.Path(tempfile.mkdtemp(prefix="shiki-install-space-", dir="/tmp"))
    source = output / "source"
    shutil.copytree(args.source, source, ignore=shutil.ignore_patterns(".git", ".tools", "bin", "dist", "__pycache__"))
    hashes = {str(p.relative_to(source)): hashlib.sha256(p.read_bytes()).hexdigest()
              for p in sorted(source.rglob("*")) if p.is_file()}
    (output / "source-sha256.json").write_text(json.dumps(hashes, indent=2) + "\n")
    modules = subprocess.check_output(["go", "env", "GOMODCACHE"], text=True).strip()
    name = "shiki-install-space-" + uuid.uuid4().hex[:12]
    command = ["docker", "run", "--name", name, "--cidfile", str(output / "container.cid"),
               "--user", "1000:1000", "--read-only", "--network", "none", "--cap-drop", "ALL",
               "--security-opt", "no-new-privileges", "--tmpfs", "/tmp:rw,exec,size=2147483648,mode=1777",
               "--tmpfs", "/space:rw,size=16777216,mode=1777", "--mount",
               f"type=bind,source={source},target=/source,readonly", "--mount",
               f"type=bind,source={modules},target=/go/pkg/mod,readonly", "-w", "/source",
               "-e", "GOCACHE=/tmp/cache", "-e", "HOME=/tmp", "-e", "GOTOOLCHAIN=local",
               "-e", "SHIKI_INSTALL_SPACE_TMPFS=/space", IMAGE, "go", "test", "-mod=readonly",
               "-race", "-count=1", "-v", "./internal/install", "-run", "^Test(InstallSpace|Backup|Space)"]
    (output / "command.json").write_text(json.dumps(command, indent=2) + "\n")
    print(f"Evidence: {output}", flush=True)
    child = None
    start = time.monotonic()
    result = {"image": IMAGE, "uid": 1000, "tmpfs_bytes": 16 << 20, "exit_code": None}

    def interrupt(signum, _frame):
        raise KeyboardInterrupt(f"signal {signum}")

    signal.signal(signal.SIGTERM, interrupt)
    signal.signal(signal.SIGINT, interrupt)
    try:
        with (output / "run.log").open("w") as log:
            child = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)
            result["exit_code"] = child.wait(timeout=600)
    finally:
        inspect = subprocess.run(["docker", "inspect", name], capture_output=True, text=True)
        (output / "container.json").write_text(inspect.stdout)
        cleanup = subprocess.run(["docker", "rm", "-f", name], capture_output=True, text=True)
        if child is not None and child.poll() is None:
            child.wait(timeout=30)
        check = subprocess.run(["docker", "inspect", name], capture_output=True, text=True)
        result.update(elapsed_seconds=time.monotonic() - start,
                      container_removed=cleanup.returncode == 0 and "no such object" in check.stderr.lower(),
                      source_unchanged=all(hashlib.sha256((source / f).read_bytes()).hexdigest() == h for f, h in hashes.items()))
        (output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps(result), flush=True)
    if result["exit_code"] != 0 or not result["container_removed"] or not result["source_unchanged"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
