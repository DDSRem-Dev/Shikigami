"""Measured, bounded CI parallelism and a private copy of the warm Go cache."""
import os
from pathlib import Path
import platform
import shutil
import stat
import subprocess
import time

GIB = 1024 ** 3


def parallelism(cpu_count, memory_bytes, *, detected=True):
    if not detected or cpu_count < 1 or memory_bytes < 1:
        return {"package_parallelism": 1, "gomaxprocs": 1, "selection": "probe_unavailable"}
    # Three CPUs allow two GOMAXPROCS=1 packages with runner headroom.
    # Keep two-CPU and memory-constrained hosts at one package.
    packages = 2 if cpu_count >= 3 and memory_bytes >= 6 * GIB else 1
    return {"package_parallelism": packages, "gomaxprocs": max(1, min(2, cpu_count // packages)),
            "selection": "measured_resources"}


def cgroup_limits(cpus, memory, root=Path("/sys/fs/cgroup"), membership=Path("/proc/self/cgroup")):
    """Apply this process's cgroup-v2 limits, including every visible ancestor."""
    line = next((line for line in membership.read_text().splitlines() if line.startswith("0::")), None)
    if line is None:
        raise ValueError("cgroup-v2 resource scope unavailable")
    relative = line[3:]
    if not relative.startswith("/") or ".." in Path(relative).parts:
        raise ValueError("invalid cgroup resource scope")
    current = root / relative.lstrip("/")
    if not current.is_dir():
        raise ValueError("current cgroup is not visible in the mounted hierarchy")
    while True:
        quota = current / "cpu.max"
        if quota.exists():
            amount, period = quota.read_text().split()
            if amount != "max":
                cpus = min(cpus, max(1, int(amount) // int(period)))
        limit = current / "memory.max"
        if limit.exists():
            amount = limit.read_text().strip()
            if amount != "max":
                memory = min(memory, int(amount))
        if current == root:
            break
        current = current.parent
    return cpus, memory


def resources():
    cpus, memory, detected = 0, 0, True
    try:
        cpus = os.cpu_count() or 0
        if hasattr(os, "sched_getaffinity"):
            cpus = min(cpus, len(os.sched_getaffinity(0)))
        if platform.system() == "Darwin":
            memory = int(subprocess.check_output(["sysctl", "-n", "hw.memsize"], text=True, timeout=5).strip())
        else:
            memory = os.sysconf("SC_PHYS_PAGES") * os.sysconf("SC_PAGE_SIZE")
            cpus, memory = cgroup_limits(cpus, memory)
    except (OSError, ValueError, ZeroDivisionError, subprocess.SubprocessError):
        detected = False
    detected = detected and cpus > 0 and memory > 0
    return {"available_cpus": cpus, "memory_bytes": memory, "probe_complete": detected,
            "cgroup_scope": ("v2_current_and_visible_ancestors" if platform.system() == "Linux" else "not_applicable") if detected else "unavailable",
            **parallelism(cpus, memory, detected=detected)}


def go_environment(measured):
    packages, procs = measured["package_parallelism"], measured["gomaxprocs"]
    if type(packages) is not int or packages not in (1, 2) or type(procs) is not int or procs not in (1, 2):
        raise ValueError("unbounded platform concurrency")
    return {"GOMAXPROCS": str(procs), "GOFLAGS": f"-p={packages} -mod=readonly -buildvcs=false"}


def copy_build_cache(source, destination, *, byte_limit=4 * GIB, time_limit=30):
    """Reuse completed entries without sharing writes or changing source modes.

    A bounded partial copy remains a valid Go cache. Missing entries are rebuilt;
    count=1 and every actual test assertion remain in force.
    """
    started = time.monotonic()
    copied, count, complete, reason = 0, 0, True, "complete"
    expired = lambda: time.monotonic() - started >= time_limit
    for directory, directories, files in os.walk(source):
        if expired():
            complete, reason = False, "time_limit"
            break
        relative = Path(directory).relative_to(source)
        target = destination / relative
        target.mkdir(mode=0o700, parents=True, exist_ok=True)
        for child in directories:
            if expired():
                complete, reason = False, "time_limit"
                break
            if (Path(directory) / child).is_symlink():
                raise ValueError("Go build cache contains a symlink directory")
        if not complete:
            break
        for name in files:
            if expired():
                complete, reason = False, "time_limit"
                break
            original = Path(directory) / name
            info = original.lstat()
            if not stat.S_ISREG(info.st_mode):
                raise ValueError("Go build cache contains a non-regular file")
            if copied + info.st_size > byte_limit:
                complete, reason = False, "byte_limit"
                break
            output, written = target / name, 0
            try:
                with original.open("rb") as reader, output.open("xb") as writer:
                    while True:
                        if expired():
                            complete, reason = False, "time_limit"
                            break
                        chunk = reader.read(1024 * 1024)
                        if not chunk:
                            break
                        if copied + written + len(chunk) > byte_limit:
                            complete, reason = False, "byte_limit"
                            break
                        writer.write(chunk)
                        written += len(chunk)
                if not complete:
                    output.unlink()
                    break
                shutil.copystat(original, output, follow_symlinks=False)
            except BaseException:
                output.unlink(missing_ok=True)
                raise
            copied += written
            count += 1
        if not complete:
            break
    return {"copied_bytes": copied, "copied_files": count, "seconds": round(time.monotonic() - started, 3),
            "complete": complete, "stop_reason": reason, "byte_limit": byte_limit, "time_limit_seconds": time_limit,
            "source_mode_unchanged": True, "destination": "owned private cache copy"}
