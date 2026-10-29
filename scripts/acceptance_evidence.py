"""Evidence checks for the acceptance runner; no product runtime dependency."""

import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def write_json(path, value):
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n")
    temporary.replace(path)


def remove_snapshot(snapshot):
    """Remove the collector-owned copy, including copied read-only directories.

    Call only after the scenario has ended and its input hashes were checked.
    Directory permissions suffice for deletion; never chmod files or link targets.
    """
    if snapshot.is_symlink():
        snapshot.unlink()
        return
    if not snapshot.exists():
        return
    for root, directories, _ in os.walk(snapshot, topdown=True, followlinks=False):
        path = Path(root)
        path.chmod(stat.S_IMODE(path.stat().st_mode) | stat.S_IRWXU)
        directories[:] = [name for name in directories if not (path / name).is_symlink()]
    shutil.rmtree(snapshot)


def command(args, timeout=30, **kwargs):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout, **kwargs)
    return {"exit_code": result.returncode, "stdout": result.stdout, "stderr": result.stderr}


def require_command(args, **kwargs):
    result = command(args, **kwargs)
    if result["exit_code"] != 0:
        raise RuntimeError(f"command failed: {args[0]}: {result['stderr'].strip()}")
    return result["stdout"].strip()


def check_container_absent(cid, execute=command):
    """A failed Docker inventory is unknown, never successful reclamation."""
    if not re.fullmatch(r"[a-f0-9]{64}", cid):
        raise ValueError("invalid container identity")
    inventory = execute(["docker", "ps", "-a", "--no-trunc", "--format", "{{json .ID}}"])
    result = {"container_id": cid, "inventory": inventory, "container_absent": None}
    if inventory["exit_code"] == 0:
        try:
            ids = [json.loads(line) for line in inventory["stdout"].splitlines() if line.strip()]
            if not all(isinstance(value, str) and re.fullmatch(r"[a-f0-9]{64}", value) for value in ids):
                raise ValueError("invalid full container ID in inventory")
            result["container_absent"] = cid not in ids
        except ValueError as error:
            result["inventory_error"] = str(error)
    result["inspect"] = execute(["docker", "inspect", cid])
    if result["inspect"]["exit_code"] == 0:
        result["container_absent"] = False  # Contradiction is not a pass.
    return result


def snapshot_manifest(snapshot):
    result = {}
    for path in sorted(snapshot.rglob("*")):
        if path.is_symlink():
            if not path.resolve().is_relative_to(snapshot):
                raise ValueError(f"source symlink escapes snapshot: {path.relative_to(snapshot)}")
            result[str(path.relative_to(snapshot))] = {"symlink": os.readlink(path)}
        elif path.is_file():
            result[str(path.relative_to(snapshot))] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def native_process_check(path, probe=os.kill):
    result = {"metadata_present": path.is_file(), "remaining_processes": [], "errors": []}
    if not path.is_file():
        return result
    try:
        records = json.loads(path.read_text())["processes"]
        if not isinstance(records, list) or not records:
            raise ValueError("expected a nonempty process inventory")
        for record in records:
            pid = record["pid"]
            if not isinstance(pid, int) or isinstance(pid, bool) or pid <= 1:
                raise ValueError("invalid tracked PID")
            try:
                probe(pid, 0)
                result["remaining_processes"].append(record)
            except ProcessLookupError:
                pass
            except PermissionError:
                result["remaining_processes"].append(record)
    except (OSError, KeyError, TypeError, ValueError) as error:
        result["errors"].append(str(error))
    return result


def collector_succeeded(evidence, native):
    if native:
        cleanup = evidence.get("process_cleanup", {})
        clean = (cleanup.get("metadata_present") is True
                 and not cleanup.get("remaining_processes") and not cleanup.get("errors"))
    else:
        clean = evidence.get("container_cleanup", {}).get("container_absent") is True
    return bool(evidence["application_exit_code"] == 0 and not evidence["errors"]
                and not evidence["interruption_signals"] and clean
                and evidence.get("snapshot_removed") and evidence.get("actual_snapshot_verified")
                and not evidence.get("forced_container_cleanup")
                and not evidence.get("forced_process_cleanup"))
