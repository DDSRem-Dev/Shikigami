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


def process_identity(pid):
    path = Path("/proc") / str(pid)
    fields = (path / "stat").read_text().rsplit(")", 1)[1].split()
    status = (path / "status").read_text().splitlines()
    uid = next(line.split()[1:] for line in status if line.startswith("Uid:"))
    try:
        executable = os.readlink(path / "exe")
    except PermissionError:
        # Container root deliberately has no SYS_PTRACE capability. Read the
        # ordinary user's /proc symlink as that same user; do not enlarge the
        # container capability set to inspect a service we already own.
        if os.geteuid() != 0 or [int(value) for value in uid] != [1000] * 4:
            raise
        observed = run(["/usr/sbin/runuser", "-u", "shikitest", "--", "/usr/bin/readlink", path / "exe"], check=False)
        if observed.returncode:
            if not path.exists():
                raise FileNotFoundError(path)
            raise RuntimeError("same-user process executable inspection failed")
        executable = observed.stdout.rstrip("\n")
    # Recheck the process generation after the delegated symlink read.
    if (path / "stat").read_text().rsplit(")", 1)[1].split()[19] != fields[19]:
        raise RuntimeError("process identity changed while inspecting executable")
    return {"pid": pid, "start_ticks": int(fields[19]),
            "uids": [int(value) for value in uid],
            "cgroup": (path / "cgroup").read_text().strip(),
            "executable": executable}


def gone(records):
    for record in records:
        try:
            current = process_identity(record["pid"])
        except FileNotFoundError:
            continue
        if current["start_ticks"] == record["start_ticks"]:
            return False
    return True
