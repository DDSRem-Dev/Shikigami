#!/usr/bin/env python3
"""Explicit fixture-only restoration; never a shipped recovery command.

Runs as the current native ordinary user after the caller stops the installed agent and joins workers.
Only a verified installation archive's state/plugin-state/sync subtree is read.
The current schema2 subtree is retained, not removed or overwritten.
"""
import fcntl
import json
import os
from pathlib import Path, PurePosixPath
import stat
import sys
import tarfile

sys.dont_write_bytecode = True
from common import digest, inventory

PREFIX = PurePosixPath("state/plugin-state/sync")


def external_inventory(roots):
    return {str(root): inventory(root) if Path(root).is_dir() else {"file": digest(root)} for root in roots}


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def private_ancestors(path):
    for parent in (path, *path.parents):
        info = parent.lstat()
        if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode):
            raise RuntimeError("restoration path contains non-directory or symlink")


def restore(plan):
    if os.geteuid() == 0:
        raise RuntimeError("controlled restoration requires the ordinary service user")
    root = Path(plan["native_root"])
    if root.parent != Path("/private/tmp") or not root.name.startswith("shiki-schema-macos.") or root.is_symlink() or root.stat().st_uid != os.getuid():
        raise RuntimeError("invalid native fixture root")
    target = Path(plan["target"])
    if target != root / "runtime/state/plugin-state/sync":
        raise RuntimeError("fixture restoration scope differs")
    private_ancestors(target)
    archive = Path(plan["archive"])
    if digest(archive) != plan["archive_sha256"]:
        raise RuntimeError("installation backup changed")
    if inventory(target) != plan["schema2_files"] or external_inventory(plan["external_roots"]) != plan["external_before"]:
        raise RuntimeError("state or external files changed before restoration")
    preserved = Path(plan["preserved"])
    if preserved != root / "runtime/schema-recovery/sync-schema2-preserved" or preserved.exists():
        raise RuntimeError("unexpected or existing evidence destination")
    preserved.parent.mkdir(mode=0o700)
    private_ancestors(preserved.parent)
    staging = target.parent / "sync-schema1-restoration"
    if staging.exists():
        raise RuntimeError("restoration staging already exists")
    locks = []
    try:
        # An accidental live writer prevents evidence movement/restoration.
        for path in sorted(target.rglob("*.lock")):
            fd = os.open(path, os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK)
            locks.append(fd)
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        instance = target.parents[1] / "instance.lock"
        if instance.exists():
            fd = os.open(instance, os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK)
            locks.append(fd)
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        staging.mkdir(mode=0o700)
        names, restored, total = set(), {}, 0
        with tarfile.open(archive, "r:") as packed:
            for entry in packed:
                name = PurePosixPath(entry.name)
                if name != PREFIX and PREFIX not in name.parents:
                    continue
                relative = name.relative_to(PREFIX)
                if name.is_absolute() or ".." in name.parts or str(relative) in names or entry.mode & 0o077:
                    raise RuntimeError("unsafe or duplicate sync backup member")
                names.add(str(relative))
                if len(names) > 10000 or entry.size < 0:
                    raise RuntimeError("restoration inventory limit exceeded")
                total += entry.size
                if total > 64 << 20:
                    raise RuntimeError("fixture backup exceeds bounded restore size")
                destination = staging / str(relative)
                if entry.isdir():
                    destination.mkdir(mode=0o700, parents=True, exist_ok=True)
                    continue
                if not entry.isfile():
                    raise RuntimeError("restoration only permits private regular files")
                destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                with packed.extractfile(entry) as source, destination.open("xb") as output:
                    os.chmod(destination, 0o600)
                    remaining = entry.size
                    while remaining:
                        data = source.read(min(remaining, 256 << 10))
                        if not data:
                            raise RuntimeError("truncated backup member")
                        output.write(data)
                        remaining -= len(data)
                    output.flush()
                    os.fsync(output.fileno())
                restored[str(relative)] = digest(destination)
        ledger = staging / "ledger/state.json"
        if json.loads(ledger.read_text())["schema_version"] != 1 or digest(ledger) != plan["schema1_ledger_sha256"]:
            raise RuntimeError("backup is not the selected original schema1 ledger")
        for directory in sorted((x for x in staging.rglob("*") if x.is_dir()), key=lambda x: len(x.parts), reverse=True):
            sync_directory(directory)
        sync_directory(staging)
        # Recheck immediately before moving either tree; all original evidence
        # remains in one untouched subtree if the second rename fails.
        if inventory(target) != plan["schema2_files"] or external_inventory(plan["external_roots"]) != plan["external_before"]:
            raise RuntimeError("state changed during restoration staging")
        os.rename(target, preserved)
        sync_directory(preserved.parent)
        sync_directory(target.parent)
        os.rename(staging, target)
        sync_directory(target.parent)
        if inventory(preserved) != plan["schema2_files"] or inventory(target) != restored:
            raise RuntimeError("restoration/evidence digest mismatch")
        if external_inventory(plan["external_roots"]) != plan["external_before"]:
            raise RuntimeError("restoration changed external data or credentials")
        return {"restored_files": len(restored), "restored_bytes": total, "schema": 1,
                "schema2_evidence_retained": True, "external_files_unchanged": True,
                "automatic_product_restore": False, "schema1_ledger_sha256": digest(target / "ledger/state.json")}
    finally:
        for fd in locks:
            os.close(fd)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("expected a private restoration plan")
    print(json.dumps(restore(json.loads(Path(sys.argv[1]).read_text())), sort_keys=True))
