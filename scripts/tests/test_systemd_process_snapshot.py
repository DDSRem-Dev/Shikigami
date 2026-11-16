"""Procfs inventory regressions without services, signals or process mutations."""
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
from platform_support import public_systemd_result, safe_script_failure

SPEC = importlib.util.spec_from_file_location("systemd_scenario_snapshot", SCRIPTS / "test-systemd.py")
scenario = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(scenario)


class WorkerSnapshotTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.proc = Path(self.folder.name)

    def process(self, pid, command=b"/trusted/shiki-plugin-sync", stat=None):
        entry = self.proc / str(pid)
        entry.mkdir()
        (entry / "cmdline").write_bytes(command + b"\0argument\0")
        (entry / "stat").write_bytes(stat if stat is not None else f"{pid} (worker) S 40 1 2".encode())
        return entry

    def test_unrelated_process_stat_is_never_read(self):
        self.process(100)
        unrelated = self.process(101, b"/other/process", b"malformed stat")
        (unrelated / "stat").unlink()
        self.process(102, b"/other/not-shiki-plugin-sync", b"malformed stat")
        original = Path.read_bytes
        def read(path):
            if path.parent.name in ("101", "102") and path.name == "stat":
                raise AssertionError("unrelated procfs stat read")
            return original(path)
        with patch.object(Path, "read_bytes", read):
            self.assertEqual(scenario.worker_processes(40, self.proc), [100])

    def test_matching_parent_and_arbitrary_comm_bytes(self):
        self.process(100, stat=b"100 (unusual ) \xff name) S 40 1 2")
        self.process(101, b"/trusted/shiki-plugin-port", b"101 (port) S 41 1 2")
        self.assertEqual(scenario.worker_processes(40, self.proc), [100])

    def test_process_exit_at_either_read_is_not_a_scan_failure(self):
        self.process(100)
        original = Path.read_bytes
        for filename in ("cmdline", "stat"):
            for error in (FileNotFoundError, ProcessLookupError):
                with self.subTest(filename=filename, error=error):
                    def read(path):
                        if path.name == filename:
                            raise error("fixture process exited")
                        return original(path)
                    with patch.object(Path, "read_bytes", read):
                        self.assertEqual(scenario.worker_processes(40, self.proc), [])

    def test_permission_denial_is_not_absence(self):
        self.process(100)
        original = Path.read_bytes
        def read(path):
            if path.name == "stat":
                raise PermissionError("fixture access denied")
            return original(path)
        with patch.object(Path, "read_bytes", read), self.assertRaises(PermissionError):
            scenario.worker_processes(40, self.proc)

    def test_malformed_worker_stat_is_not_absence(self):
        entry = self.process(100)
        for value in (b"", b"100 no separator", b"100 (worker) S", b"100 (worker) S bad", b"101 (worker) S 40"):
            with self.subTest(value=value):
                (entry / "stat").write_bytes(value)
                with self.assertRaises(ValueError):
                    scenario.worker_processes(40, self.proc)

    def test_safe_exception_kind_is_exported_without_private_content(self):
        private = "PRIVATE_TOKEN https://private.example/login /private/credential"
        for error in (ProcessLookupError(private), IndexError(private), UnicodeDecodeError("utf8", b"\xff", 0, 1, private)):
            with self.subTest(error=type(error).__name__):
                result = public_systemd_result(safe_script_failure(error))
                self.assertEqual(result["error_kind"], type(error).__name__)
                encoded = json.dumps(result)
                for fragment in ("PRIVATE_TOKEN", "https://private.example/login", "/private/credential"):
                    self.assertNotIn(fragment, encoded)


if __name__ == "__main__":
    unittest.main()
