import importlib.util
import json
import os
from pathlib import Path
import signal
import sys
import tempfile
import unittest
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
from acceptance_evidence import (
    check_container_absent, collector_succeeded, native_process_check, remove_snapshot, snapshot_manifest,
)

spec = importlib.util.spec_from_file_location("acceptance_runner", SCRIPTS / "run-acceptance.py")
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class EvidenceTests(unittest.TestCase):
    cid = "a" * 64

    def test_readonly_snapshot_cleanup_preserves_link_targets(self):
        with tempfile.TemporaryDirectory() as folder:
            base = Path(folder)
            outside = base / "original"
            outside.mkdir()
            original = outside / "source.go"
            original.write_text("original source")
            original.chmod(0o444)
            snapshot = base / "snapshot"
            nested = snapshot / "pkg" / "nested"
            nested.mkdir(parents=True)
            (nested / "copied.go").write_text("copied source")
            (nested / "copied.go").chmod(0o444)
            (snapshot / "link").symlink_to(outside, target_is_directory=True)
            for path in (nested, nested.parent, snapshot):
                path.chmod(0o555)
            outside.chmod(0o555)
            try:
                remove_snapshot(snapshot)
                self.assertFalse(snapshot.exists())
                self.assertEqual(original.read_text(), "original source")
                self.assertEqual(outside.stat().st_mode & 0o777, 0o555)
                self.assertEqual(original.stat().st_mode & 0o777, 0o444)
            finally:
                outside.chmod(0o755)
                if snapshot.exists():
                    for path in (snapshot, nested.parent, nested):
                        path.chmod(0o755)

    def test_snapshot_replaced_by_symlink_does_not_remove_target(self):
        with tempfile.TemporaryDirectory() as folder:
            base = Path(folder)
            original = base / "original"
            original.mkdir()
            link = base / "snapshot"
            link.symlink_to(original, target_is_directory=True)
            remove_snapshot(link)
            self.assertFalse(link.is_symlink())
            self.assertTrue(original.is_dir())

    def inventory(self, ids="", code=0, inspect_code=1, diagnostic="error: no such object"):
        def execute(args):
            if args[1] == "ps":
                return {"exit_code": code, "stdout": ids, "stderr": "unavailable" if code else ""}
            return {"exit_code": inspect_code, "stdout": "[]", "stderr": diagnostic}
        return execute

    def test_cleanup_does_not_depend_on_error_language_or_capitalization(self):
        for message in ("No such object", "error: no such object", "kein solches Objekt"):
            result = check_container_absent(self.cid, self.inventory(diagnostic=message))
            self.assertTrue(result["container_absent"])

    def test_daemon_unavailable_and_malformed_inventory_are_not_cleanup_passes(self):
        for execute in (self.inventory(code=1), self.inventory(ids="not-json"),
                        self.inventory(ids=json.dumps("short-id"))):
            self.assertIsNone(check_container_absent(self.cid, execute)["container_absent"])

    def test_existing_exact_id_and_contradictory_inspect_refuse_pass(self):
        self.assertFalse(check_container_absent(self.cid, self.inventory(json.dumps(self.cid)))["container_absent"])
        self.assertFalse(check_container_absent(self.cid, self.inventory(inspect_code=0))["container_absent"])
        self.assertTrue(check_container_absent(self.cid, self.inventory(json.dumps("b" * 64)))["container_absent"])
        with self.assertRaises(ValueError):
            check_container_absent("$(untrusted)", self.inventory())

    def test_pid_permission_failure_is_not_process_absence(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "processes.json"
            path.write_text(json.dumps({"processes": [{"pid": 234, "role": "worker"}]}))
            def denied(*_):
                raise PermissionError()
            self.assertEqual(len(native_process_check(path, denied)["remaining_processes"]), 1)
            def absent(*_):
                raise ProcessLookupError()
            self.assertFalse(native_process_check(path, absent)["remaining_processes"])
            path.write_text('{"processes": []}')
            self.assertTrue(native_process_check(path)["errors"])

    def test_mutation_changes_manifest_and_external_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder) / "source"
            root.mkdir()
            source = root / "main.go"
            source.write_text("before")
            before = snapshot_manifest(root)
            source.write_text("after")
            self.assertNotEqual(before, snapshot_manifest(root))
            (root / "outside").symlink_to(Path(folder))
            with self.assertRaises(ValueError):
                snapshot_manifest(root)

    def test_application_failure_interruption_or_forced_cleanup_cannot_pass(self):
        evidence = {
            "application_exit_code": 0, "errors": [], "interruption_signals": [],
            "snapshot_removed": True, "actual_snapshot_verified": True,
            "container_cleanup": {"container_absent": True},
        }
        self.assertTrue(collector_succeeded(evidence, False))
        for key, value in (("application_exit_code", 1), ("errors", ["failed"]),
                           ("interruption_signals", [signal.SIGTERM]),
                           ("forced_container_cleanup", True), ("forced_process_cleanup", True),
                           ("actual_snapshot_verified", False)):
            self.assertFalse(collector_succeeded(dict(evidence, **{key: value}), False))


@unittest.skipIf(os.getuid() == 0, "native acceptance intentionally rejects root")
class RunnerTests(unittest.TestCase):
    def run_fixture(self, folder, mutate, readonly=False):
        base = Path(folder)
        repository = base / "repository"
        (repository / "scripts").mkdir(parents=True)
        (repository / "README.md").write_text("original\n")
        script = repository / "scripts" / "fixture.sh"
        script.write_text(
            "#!/bin/sh\nset -eu\n"
            "printf '{\"processes\":[{\"pid\":%s,\"role\":\"fixture\"}]}\\n' \"$$\" > \"$SHIKI_RUN_EVIDENCE_DIR/processes.json\"\n"
            + ("printf new > injected.go\n" if mutate == "add" else
               "chmod 000 README.md\n" if mutate == "unreadable" else
               "printf changed > README.md\n" if mutate else "")
        )
        script.chmod(0o700)
        output = base / "evidence"
        args = runner.parser().parse_args([
            "--label", "fixture", "--output", str(output), "--timeout-seconds", "60",
            "--", "scripts/fixture.sh",
        ])
        guard = base / "guard-fixture"
        guard.write_text("unit fixture, unused because real scenario child has exited")
        if readonly:
            (repository / "scripts").chmod(0o555)
            repository.chmod(0o555)
        with patch.object(runner, "__file__", str(repository / "scripts" / "run-acceptance.py")), \
             patch.object(runner, "build_process_guard", return_value=guard):
            result = runner.run(args)
        return result, json.loads((output / "evidence.json").read_text()), repository

    def test_real_child_from_readonly_repository_finishes_cleanup(self):
        with tempfile.TemporaryDirectory() as folder:
            repository = Path(folder) / "repository"
            try:
                code, evidence, repository = self.run_fixture(folder, False, readonly=True)
                self.assertEqual(code, 0)
                self.assertEqual(evidence["application_exit_code"], 0)
                self.assertTrue(evidence["snapshot_removed"])
                self.assertFalse(evidence["changed_source_inputs"])
                self.assertFalse(evidence["errors"])
                self.assertEqual(repository.stat().st_mode & 0o777, 0o555)
                self.assertEqual((repository / "scripts").stat().st_mode & 0o777, 0o555)
            finally:
                repository.chmod(0o755)
                (repository / "scripts").chmod(0o755)

    def test_real_child_exit_snapshot_and_original_repository(self):
        with tempfile.TemporaryDirectory() as folder:
            code, evidence, repository = self.run_fixture(folder, False)
            self.assertEqual(code, 0)
            self.assertEqual(evidence["application_exit_code"], 0)
            self.assertTrue(evidence["snapshot_removed"])
            self.assertFalse(evidence["process_cleanup"]["remaining_processes"])
            self.assertEqual((repository / "README.md").read_text(), "original\n")

    def test_source_mutation_is_failure_despite_successful_child_and_cleanup(self):
        with tempfile.TemporaryDirectory() as folder:
            code, evidence, repository = self.run_fixture(folder, True)
            self.assertEqual(code, 1)
            self.assertEqual(evidence["application_exit_code"], 0)
            self.assertEqual(evidence["changed_source_inputs"], ["README.md"])
            self.assertTrue(evidence["snapshot_removed"])
            self.assertEqual((repository / "README.md").read_text(), "original\n")

    def test_failed_postcheck_retains_snapshot_and_original_error(self):
        with tempfile.TemporaryDirectory() as folder:
            code, evidence, repository = self.run_fixture(folder, "unreadable")
            self.assertEqual(code, 1)
            self.assertEqual(evidence["application_exit_code"], 0)
            self.assertFalse(evidence.get("source_postcheck_completed", False))
            self.assertFalse(evidence["snapshot_removed"])
            self.assertEqual(evidence["snapshot_retained_reason"], "source postcheck incomplete")
            self.assertTrue(any("PermissionError" in error for error in evidence["errors"]))
            self.assertTrue((Path(folder) / "evidence" / "source").is_dir())
            self.assertEqual((repository / "README.md").read_text(), "original\n")

    def test_new_source_file_cannot_escape_manifest_check(self):
        with tempfile.TemporaryDirectory() as folder:
            code, evidence, repository = self.run_fixture(folder, "add")
            self.assertEqual(code, 1)
            self.assertEqual(evidence["application_exit_code"], 0)
            self.assertEqual(evidence["changed_source_inputs"], ["injected.go"])
            self.assertFalse((repository / "injected.go").exists())


if __name__ == "__main__":
    unittest.main()
