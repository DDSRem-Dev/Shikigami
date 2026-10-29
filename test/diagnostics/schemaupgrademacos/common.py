"""Private helpers for the opt-in development schema/service experiment."""
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import time

if sys.flags.optimize:
    raise RuntimeError("acceptance requires Python assertions; unset PYTHONOPTIMIZE and -O")


def run(args, *, timeout=60, check=True, cwd=None, env=None):
    result = subprocess.run([str(a) for a in args], cwd=cwd, env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {args}\n{result.stdout}\n{result.stderr}")
    return result


def wait_for(probe, timeout=30):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            result = probe()
            if result:
                return result
        except (RuntimeError, OSError, ValueError) as error:
            last = error
        time.sleep(0.2)
    raise RuntimeError(f"condition did not settle within {timeout}s: {last}")


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def inventory(root):
    root = Path(root)
    result = {}
    for path in sorted(root.rglob("*")):
        mode = path.lstat().st_mode
        if stat.S_ISLNK(mode):
            raise RuntimeError(f"inventory refuses symlink: {path}")
        if stat.S_ISREG(mode):
            result[str(path.relative_to(root))] = digest(path)
        elif not stat.S_ISDIR(mode):
            raise RuntimeError(f"inventory refuses special file: {path}")
    return result


def write_json(path, value):
    path = Path(path)
    with path.open("x", encoding="utf-8") as output:
        os.chmod(path, 0o600)
        json.dump(value, output, indent=2, sort_keys=True)
        output.write("\n")


NATIVE_ROOT = None
NATIVE_PROBE = None


def initialize(root, probe):
    global NATIVE_ROOT, NATIVE_PROBE
    NATIVE_ROOT, NATIVE_PROBE = Path(root), Path(probe)


def process_identity(pid):
    result = run([NATIVE_PROBE, "--root", NATIVE_ROOT, "--inspect", str(pid)], check=False)
    if result.returncode:
        raise RuntimeError("same-user native process identity unavailable")
    value = json.loads(result.stdout)
    if value["state"] == "absent":
        raise FileNotFoundError(str(pid))
    return value


def gone(records):
    for record in records:
        try:
            current = process_identity(record["pid"])
        except FileNotFoundError:
            continue
        if current["created_ms"] == record["created_ms"]:
            return False
    return True


def replace_json(path, value):
    path = Path(path)
    temporary = path.with_suffix(path.suffix + ".tmp")
    with temporary.open("w") as output:
        os.chmod(temporary, 0o600)
        json.dump(value, output, indent=2, sort_keys=True)
        output.write("\n")
    os.replace(temporary, path)
