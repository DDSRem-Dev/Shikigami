#!/usr/bin/env python3
"""Real GUI-domain schema recovery, private paths, actual account HOME."""
import hashlib
import json
import os
import signal
from pathlib import Path
import sys
import tarfile
import traceback

sys.dont_write_bytecode = True
from common import digest, gone, inventory, process_identity, run, wait_for, write_json
from fixture import Fixture
from restore import external_inventory


def settled(f, mapping):
    state = f.sync_status(mapping)
    return state["state"] == "active" and int(state["pending_files"]) == 0 and int(state["conflict_count"]) == 0


def seed(f):
    mappings, files = {}, []
    for kind in ("clean", "conflict", "paused"):
        remote, local = f.home / "projects" / kind, f.home / "local" / kind
        for root in (remote, local):
            f.write(root / "same.txt", b"original common baseline\n")
            files.append(root)
        project = f.cli("projects", "create", "schema-" + kind, "--path", str(remote))
        listed = f.cli("sync", "map", project["id"], "--local", str(local), "--server", "schema")["mappings"]
        mapping = next(m for m in listed if m["local_root"] == str(local))
        f.cli("sync", "resume", mapping["id"])
        wait_for(lambda: settled(f, mapping))
        if kind != "clean":
            f.cli("sync", "pause", mapping["id"])
            wait_for(lambda: f.sync_status(mapping)["state"] == "paused")
            f.write(local / "same.txt", (kind + " local edit\n").encode())
            if kind == "conflict":
                f.write(remote / "same.txt", b"conflict remote edit\n")
                f.cli("sync", "resume", mapping["id"])
                wait_for(lambda: int(f.sync_status(mapping)["conflict_count"]) == 1)
                conflict = f.cli("sync", "conflicts", mapping["id"])["conflicts"][0]
                f.cli("sync", "resolve", conflict["id"], "--mapping", mapping["id"],
                      "--generation", conflict["generation"], "--choice", "keep-both", "--yes")
                wait_for(lambda: settled(f, mapping))
                # Retain a real completed request and copy lineage, then create
                # a different unresolved conflict for all service boundaries.
                f.cli("sync", "pause", mapping["id"])
                wait_for(lambda: f.sync_status(mapping)["state"] == "paused")
                f.write(local / "same.txt", b"second unresolved local edit\n")
                f.write(remote / "same.txt", b"second unresolved remote edit\n")
                f.cli("sync", "resume", mapping["id"])
                wait_for(lambda: int(f.sync_status(mapping)["conflict_count"]) == 1)
        mappings[kind] = f.mappings()[mapping["id"]]
        if kind == "clean":
            session = f.cli("sessions", "create", "schema-pane", "--project", project["id"])
            f.session_id = session["id"]
            f.pane = f.record_session(f.session_id)
    f.expected_files = {str(root): inventory(root) for root in files}
    f.file_roots = [str(root) for root in files]
    return mappings


def preserved(f, mappings, schema):
    current = f.mappings()
    assert all(current[m["id"]] == m for m in mappings.values()), "host mapping identity/revision/desired changed"
    assert {root: inventory(root) for root in f.file_roots} == f.expected_files, "project/local files changed"
    assert int(f.cli("sessions", "info", f.session_id)["claude_pid"]) == f.pane
    status = f.plugin()
    assert status["enabled"] and status["state"] == "ready"
    wait_for(lambda: settled(f, mappings["clean"]))
    wait_for(lambda: int(f.sync_status(mappings["conflict"])["conflict_count"]) == 1)
    wait_for(lambda: f.sync_status(mappings["paused"])["state"] == "paused")
    ledger = json.loads(f.ledger.read_text())
    assert ledger["schema_version"] == schema
    rows = ledger["buckets"]["sync_mappings"]
    if schema == 2:
        assert rows[mappings["paused"]["id"]]["legacy_unverified"].get("same.txt") is True
        assert rows[mappings["conflict"]["id"]]["legacy_unverified"].get("same.txt") is True
        assert not rows[mappings["clean"]["id"]]["legacy_unverified"]
        assert all(not r["operations"] for r in rows.values()), "fixture unexpectedly performed a schema2 transfer"
    return ledger


def exercise(f):
    f.build()
    f.setup()
    mappings = seed(f)
    initial = preserved(f, mappings, 1)
    assert initial["requests"] and all(r["status"] == "succeeded" for r in initial["requests"].values())
    assert initial["buckets"]["sync_mappings"][mappings["conflict"]["id"]]["copies"]
    agent = process_identity(f.agent_pid())
    daemon_pid = f.daemon_child.pid
    daemon_identity = process_identity(daemon_pid)
    daemon_workers = f.children(daemon=True)
    assert agent["uids"] and all(uid == os.getuid() for uid in agent["uids"] + daemon_identity["uids"])
    def stationary():
        assert process_identity(daemon_pid) == daemon_identity, "stationary daemon replaced"
        for record in daemon_workers:
            current = process_identity(record["pid"])
            assert all(current[key] == record[key] for key in ("created_ms", "executable", "uids"))
        actual_pane = process_identity(f.pane)
        expected_pane = {k: v for k, v in f.pane_identity.items() if k != "role"}
        if actual_pane != expected_pane:
            write_json(f.evidence / "pane-identity-mismatch.json", {"expected": expected_pane, "actual": actual_pane})
            raise AssertionError("pane replaced")
    stationary()
    write_json(f.evidence / "initial.json", {"schema": 1, "mapping_ids": {k: m["id"] for k, m in mappings.items()},
               "desired": {k: m["sync_enabled"] for k, m in mappings.items()}, "pane": f.pane,
               "files": f.expected_files, "ledger_sha256": digest(f.ledger),
               "agent_process": agent, "daemon_process": daemon_identity})
    before = f.children()
    upgraded = f.client_operation("install", f.b, f.b)
    wait_for(lambda: gone(before))
    migrated = preserved(f, mappings, 2)
    stationary()
    backup = Path(upgraded["backup"])
    with tarfile.open(backup, "r:") as packed:
        original = packed.extractfile("state/plugin-state/sync/ledger/state.json").read()
    old_ledger = json.loads(original)
    assert old_ledger["schema_version"] == 1
    original_hash = hashlib.sha256(original).hexdigest()
    migration_backups = list((f.sync / "ledger").glob("state.json.pre-migration-v1-*.json"))
    assert len(migration_backups) == 1 and migration_backups[0].read_bytes() == original
    for mapping in mappings.values():
        key = mapping["id"]
        old, new = old_ledger["buckets"]["sync_mappings"][key], migrated["buckets"]["sync_mappings"][key]
        assert old["mapping"] == new["mapping"] and old["baselines"] == new["baselines"]
        assert old["copies"] == new["copies"] and old["conflicts"] == new["conflicts"]
    assert old_ledger["requests"] == migrated["requests"]
    write_json(f.evidence / "migration.json", {"from": 1, "to": 2, "backup_sha256": digest(backup),
               "original_ledger_sha256": original_hash, "exact_migration_backup": True,
               "conflict_paused_desired_retained": True, "old_processes_reaped": before})

    # Change unrelated authentication state AFTER the upgrade backup. Restoring
    # an old whole-state archive would violate this deliberate revocation.
    daemon_binary = f.bundles[f.a][0] / "shiki-daemon"
    token_args = [daemon_binary, "--config", f.daemon_config, "token"]
    prior_tokens = json.loads(f.user([*token_args, "list"]).stdout)
    f.user([*token_args, "create", "--output", f.home / "must-stay-revoked.token",
            "--permissions", "project.list,system.info", "--expires", "1h"])
    issued = json.loads(f.user([*token_args, "list"]).stdout)
    old_ids = {token["id"] for token in prior_tokens}
    revoked = next(token["id"] for token in issued if token["id"] not in old_ids)
    candidate = next(token for token in issued if token["id"] == revoked)
    assert set(candidate["permissions"]) == {"project.list", "system.info"}
    auth_project = mappings["clean"]["project_id"]
    f.auth_probe("issued-before-revocation", auth_project, revoked=False)
    f.user([*token_args, "revoke", revoked])
    assert next(t for t in json.loads(f.user([*token_args, "list"]).stdout) if t["id"] == revoked)["revoked_at"] > 0
    f.auth_probe("revoked-before-rollback", auth_project, revoked=True)

    before = f.children()
    pre_rollback = f.ledger.read_bytes()
    backups_before = set((f.prefix / "backups").glob("*.tar"))
    f.client_operation("rollback", f.b, f.a, pending=True)
    wait_for(lambda: gone(before))
    failed = wait_for(lambda: (status if status["state"] == "failed" else None) if (status := f.plugin()) else None)
    assert failed["enabled"] and not failed.get("protocol"), "old worker advertised readiness"
    rollback_backups = set((f.prefix / "backups").glob("*.tar")) - backups_before
    assert len(rollback_backups) == 1, "rollback must produce its own stopped-state backup"
    with tarfile.open(rollback_backups.pop(), "r:") as packed:
        schema2 = packed.extractfile("state/plugin-state/sync/ledger/state.json").read()
    assert json.loads(schema2)["schema_version"] == 2 and f.ledger.read_bytes() == schema2
    for _ in range(3):
        failed = f.plugin()
        assert failed["state"] != "ready" and failed["enabled"] and not failed.get("protocol")
        assert f.ledger.read_bytes() == schema2, "old writer changed schema2 primary"
        assert f.mappings() == {m["id"]: m for m in mappings.values()}
    assert {root: inventory(root) for root in f.file_roots} == f.expected_files
    write_json(f.evidence / "rollback-refusal.json", {"artifact_version": f.a, "ledger_schema": 2,
               "schema2_sha256": hashlib.sha256(schema2).hexdigest(), "desired_enabled": True,
               "pre_rollback_primary_sha256": hashlib.sha256(pre_rollback).hexdigest(),
               "primary_matches_actual_stopped_backup": True,
               "worker_state": failed["state"], "protocol_absent": True, "ready": False,
               "application_pending_exit": 3, "old_processes_reaped": before})

    before = f.children(require_sync=False)
    f.ctl("stop", "shiki-agent.service")
    wait_for(lambda: gone(before) and f.agent_pid() == 0)
    f.auth_probe("revoked-before-restoration", auth_project, revoked=True)
    roots = [*f.file_roots, str(f.config), str(f.state / "resources"), str(f.state / "credentials"),
             str(f.runtime / "daemon-state/auth"), str(f.daemon_config), str(f.token), str(f.home / "must-stay-revoked.token")]
    plan = {"native_root": str(f.root), "target": str(f.sync), "archive": str(backup), "archive_sha256": digest(backup),
            "schema1_ledger_sha256": original_hash, "schema2_files": inventory(f.sync),
            "external_roots": roots, "external_before": external_inventory(roots),
            "preserved": str(f.home / "schema-recovery/sync-schema2-preserved")}
    plan_path = f.runtime / "restore-plan.json"
    write_json(plan_path, plan)
    restoration = json.loads(f.user(["python3", "-B", Path(__file__).parent / "restore.py", plan_path]).stdout)
    assert restoration["external_files_unchanged"] and restoration["schema2_evidence_retained"]
    f.ctl("start", "shiki-agent.service")
    wait_for(lambda: f.plugin()["state"] == "ready")
    f.client_operation("probe", f.a, f.a)
    restored = preserved(f, mappings, 1)
    stationary()
    assert restored["buckets"]["sync_mappings"].keys() == initial["buckets"]["sync_mappings"].keys()
    assert next(t for t in json.loads(f.user([*token_args, "list"]).stdout) if t["id"] == revoked)["revoked_at"] > 0
    f.auth_probe("revoked-after-restoration", auth_project, revoked=True)
    write_json(f.evidence / "controlled-restoration.json", restoration)

    before = f.children()
    f.client_operation("install", f.b, f.b)
    wait_for(lambda: gone(before))
    preserved(f, mappings, 2)
    final_processes = f.children()
    write_json(f.evidence / "final-b-processes.json", final_processes)
    f.auth_probe("revoked-final-b", auth_project, revoked=True)
    stationary()
    # Only after the controlled rollback is finished may fresh business work
    # alter files. This does not claim restoration is safe after arbitrary work.
    clean = mappings["clean"]
    changed = b"new schema2 operation after completed migration cycle\n"
    f.write(Path(clean["local_root"]) / "same.txt", changed)
    destination = f.home / "projects/clean/same.txt"
    wait_for(lambda: destination.read_bytes() == changed and settled(f, clean))
    changed_back = b"native remote-to-local known baseline operation after recovery\n"
    f.write(destination, changed_back)
    wait_for(lambda: (Path(clean["local_root"]) / "same.txt").read_bytes() == changed_back and settled(f, clean))
    stationary()
    write_json(f.evidence / "result.json", {"passed": True, "versions": {"a": f.a, "b": f.b},
               "operations": f.records, "migration_cycle": [1, 2, "old-writer-refused-2", 1, 2],
               "fresh_schema2_sync": True, "known_baseline_both_directions": True, "original_token_retained": True, "revocation_retained": True,
               "revoked_token_rpc_rejected": True, "auth_probe_phases": f.auth_probe_phases,
               "auth_probe_target": f.auth_identity,
               "pane_retained": f.pane, "daemon_service_replaced": False,
               "scope": "development artifacts; real macOS GUI LaunchAgent upgrade; stationary ordinary A daemon",
               "automatic_restore_supported": False, "external_peer_acceptance": "pending"})
    print("PASS: native business scenario; normal owned cleanup follows", flush=True)


def main():
    root, evidence = Path(sys.argv[1]), Path(sys.argv[2])
    f = Fixture(root, evidence)
    try:
        exercise(f)
    finally:
        f.close()


if __name__ == "__main__":
    def interrupted(signum, _frame):
        raise KeyboardInterrupt(f"signal {signum}")
    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    try:
        main()
    except BaseException:
        traceback.print_exc()
        sys.exit(1)
