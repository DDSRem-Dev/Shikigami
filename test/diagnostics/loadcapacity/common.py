"""Bounded command execution and immutable input inventories for load diagnostics."""
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys

if sys.flags.optimize:
    raise RuntimeError("acceptance requires Python assertions; unset PYTHONOPTIMIZE and -O")


def run(args, *, timeout=60, check=True, cwd=None, env=None, input=None):
    result = subprocess.run([str(a) for a in args], cwd=cwd, env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, input=input)
    if check and result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {args}\n{result.stdout}\n{result.stderr}")
    return result


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
