#!/usr/bin/env python3
"""Fast validation tests: no Go builds, service/user mutations or network."""
import importlib.util
import json
import os
from pathlib import Path
import sys
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
from platform_support import GO_VERSION, hosted_guard, require_verbose_pass, test_events, validate_identity

SPEC = importlib.util.spec_from_file_location("native_systemd", Path(__file__).with_name("platform-systemd.py"))
systemd = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(systemd)
CI_SPEC = importlib.util.spec_from_file_location("platform_ci", Path(__file__).with_name("platform-ci.py"))
ci = importlib.util.module_from_spec(CI_SPEC)
CI_SPEC.loader.exec_module(ci)


class PlatformAcceptanceTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory(prefix="shiki-platform-unit-")
        self.addCleanup(self.folder.cleanup)
        self.root = Path(self.folder.name)
        self.log = self.root / "test.log"

    def test_hosted_guard_refuses_local_and_self_hosted(self):
        hosted_guard({"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted"})
        for env in ({}, {"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "self-hosted"},
                    {"GITHUB_ACTIONS": "false", "RUNNER_ENVIRONMENT": "github-hosted"}):
            with self.assertRaises(RuntimeError):
                hosted_guard(env)

    def test_native_architecture_and_go_target_are_independently_checked(self):
        for operating_system in ("linux", "darwin"):
            for architecture, machine, runner in (("amd64", "x86_64", "X64"), ("arm64", "aarch64", "ARM64")):
                env = dict(GOHOSTOS=operating_system, GOOS=operating_system, GOHOSTARCH=architecture,
                           GOARCH=architecture, GOVERSION=GO_VERSION)
                result = validate_identity(operating_system, architecture, operating_system, machine, runner, env)
                self.assertEqual(result["arch"], architecture)
                for key in env:
                    with self.subTest(key=key), self.assertRaises(ValueError):
                        validate_identity(operating_system, architecture, operating_system, machine, runner, {**env, key: "wrong"})
                with self.assertRaises(ValueError):
                    validate_identity(operating_system, architecture, operating_system, machine, runner, env, translated=True)
                with self.assertRaises(ValueError):
                    validate_identity(operating_system, architecture, operating_system, "wrong", runner, env)
                with self.assertRaises(ValueError):
                    validate_identity(operating_system, architecture, operating_system, machine, "wrong", env)

    def events(self, outcome):
        self.log.write_text("\n".join(json.dumps(item) for item in [
            {"Action": "output", "Package": "example", "Output": "credential must never be published"},
            {"Action": outcome, "Package": "example", "Test": "TestRequired"},
            {"Action": "pass", "Package": "example"},
        ]))

    def test_skip_missing_or_failed_required_tests_cannot_pass(self):
        self.events("pass")
        result = test_events(self.log, ["TestRequired"])
        self.assertNotIn("credential", json.dumps(result))
        for outcome in ("skip", "fail", "output"):
            self.events(outcome)
            with self.assertRaises(RuntimeError):
                test_events(self.log, ["TestRequired"])
        self.events("pass")
        with self.assertRaises(RuntimeError):
            test_events(self.log, ["TestNeverRun"])
        self.log.write_text("build failed\n")
        with self.assertRaises(RuntimeError):
            test_events(self.log)

    def test_verbose_opt_in_runner_requires_named_real_pass(self):
        self.log.write_text("--- PASS: TestNative (0.01s)\nPASS\n")
        require_verbose_pass(self.log, ["TestNative"])
        for text in ("PASS\n", "--- SKIP: TestNative (0.01s)\nPASS\n",
                     "--- PASS: TestNativeOther (0.01s)\n", "--- FAIL: TestNative (0.01s)\n"):
            self.log.write_text(text)
            with self.assertRaises(RuntimeError):
                require_verbose_pass(self.log, ["TestNative"])

    def test_cleanup_rejects_account_with_another_home_before_any_user_operation(self):
        unit = self.root / "shiki-daemon.service"
        account = SimpleNamespace(pw_name="shikici123", pw_uid=1005, pw_gid=1005, pw_dir="/home/unrelated")
        group = SimpleNamespace(gr_name="shikici123", gr_gid=1005, gr_mem=[])
        with patch.object(systemd, "UNIT", unit), patch.object(systemd.pwd, "getpwnam", return_value=account), \
                patch.object(systemd, "run") as run, patch.object(systemd, "kill_owned") as kill:
            with self.assertRaisesRegex(RuntimeError, "ownership changed"):
                systemd.cleanup(self.root, account.pw_name, account, created=True, group=group)
            run.assert_not_called()
            kill.assert_not_called()
        self.assertTrue(self.root.is_dir())

    def test_cleanup_refuses_foreign_unit(self):
        unit = self.root / "shiki-daemon.service"
        unit.write_text("ExecStart=/unrelated/shiki-daemon\n")
        with patch.object(systemd, "UNIT", unit), patch.object(systemd, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "identity changed"):
                systemd.cleanup(self.root, "shikici123", None, created=False, group=None)
            run.assert_not_called()
        self.assertTrue(unit.exists())

    def test_cleanup_stops_owned_loaded_service_after_unit_file_disappears(self):
        unit = self.root / "shiki-daemon.service"
        def command(args, **_kwargs):
            return SimpleNamespace(stdout=(str(self.root / "install/current/shiki-daemon") if "show" in args else ""),
                                   returncode=3 if "is-active" in args else 0)
        with patch.object(systemd, "UNIT", unit), patch.object(systemd, "ENABLED", self.root / "enabled"), \
                patch.object(systemd, "run", side_effect=command) as run:
            systemd.cleanup(self.root, "shikici123", None, created=False, group=None)
            self.assertIn(unittest.mock.call(["systemctl", "stop", "shiki-daemon.service"]), run.call_args_list)
        self.assertFalse(self.root.exists())

    def test_timeout_allows_the_exact_wrapper_to_finish_cleanup(self):
        marker = self.root / "cleaned"
        fixture = self.root / "wait.py"
        fixture.write_text("import pathlib,signal,sys\ndef stop(*_):\n pathlib.Path(sys.argv[1]).write_text('cleaned')\n raise SystemExit(0)\nsignal.signal(signal.SIGTERM,stop)\nsignal.pause()\n")
        with patch.dict(os.environ, {"RUNNER_TEMP": str(self.root)}):
            acceptance = ci.Acceptance(self.root / "shiki-platform-test")
            with self.assertRaises(subprocess.TimeoutExpired):
                acceptance.run("cancel", [sys.executable, fixture, marker], timeout=1)
        self.assertEqual(marker.read_text(), "cleaned")
        self.assertEqual(acceptance.stages["cancel"]["state"], "failed")

    def test_layout_paths_cannot_escape_owned_root(self):
        with patch.object(systemd, "hosted_guard"):
            with self.assertRaisesRegex(ValueError, "unexpected native systemd root"):
                systemd.validate_layout({"root": "/home/runner"})


if __name__ == "__main__":
    unittest.main(verbosity=2)
