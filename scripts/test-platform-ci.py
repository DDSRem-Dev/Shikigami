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
from platform_support import GO_VERSION, hosted_guard, require_verbose_pass, test_events, validate_identity, safe_test_locations, public_systemd_result

from platform_resources import GIB, parallelism, resources, go_environment, copy_build_cache, cgroup_limits
import platform_resources

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

    def test_failed_output_exports_only_known_locations_elapsed_and_fixed_kind(self):
        secret = "shiki_FAKE_CREDENTIAL_NEVER_PUBLISH"
        events = [
            {"Action": "output", "Package": "example", "Test": "TestRequired", "Output":
             f"    core_test.go:73: readiness timeout: {secret} https://login.invalid/secret\n"
             "-----BEGIN PRIVATE KEY-----\nSECRET_PRIVATE_BODY\n-----END PRIVATE KEY-----\n"
             f"    unknown_secret_test.go:12: {secret}\n"
             f"    /home/private/core_test.go:74: {secret}\n"
             f"    core_test.go:999999: {secret}\n"
             f"    core_test.go:75: an arbitrary error {secret}\n"},
            {"Action": "fail", "Package": "example", "Test": "TestRequired", "Elapsed": 3.123456},
            {"Action": "fail", "Package": "example"},
        ]
        self.log.write_text("\n".join(json.dumps(item) for item in events))
        result = test_events(self.log, validate=False)
        self.assertEqual(result["tests"], [{"package": "example", "test": "TestRequired", "outcome": "fail",
                         "elapsed_seconds": 3.123, "source_locations": [
                             {"file": "core_test.go", "line": 73, "kind": "condition_timeout"},
                             {"file": "core_test.go", "line": 75, "kind": "test_output"}]}])
        public = json.dumps(result)
        for forbidden in (secret, "https://", "PRIVATE", "SECRET", "unknown_secret", "/home", "arbitrary", "999999"):
            self.assertNotIn(forbidden, public)
        with self.assertRaises(RuntimeError):
            test_events(self.log, ["TestRequired"])

    def test_locations_are_bounded_and_dynamic_elapsed_is_not_published(self):
        output = "\n".join(f"core_test.go:{i}: arbitrary secret suffix" for i in range(1, 80))
        self.assertEqual(len(safe_test_locations(output, {"core_test.go": 100})), 20)
        for elapsed in ("secret elapsed", float("nan"), float("inf"), -1, 100000, True):
            events = [
                {"Action": "output", "Package": "example", "Test": "TestRequired", "Output": output},
                {"Action": "fail", "Package": "example", "Test": "TestRequired", "Elapsed": elapsed},
                {"Action": "fail", "Package": "example"},
            ]
            self.log.write_text("\n".join(json.dumps(item) for item in events))
            item = test_events(self.log, validate=False)["tests"][0]
            self.assertEqual(len(item["source_locations"]), 20)
            self.assertNotIn("elapsed_seconds", item)
            self.assertNotIn("secret", json.dumps(item))

    def test_concurrency_uses_measured_memory_and_cpu_with_conservative_fallback(self):
        for cpu, memory, packages, procs in ((4, 16, 2, 2), (3, 7, 1, 2), (2, 8, 1, 2), (4, 4, 1, 2), (1, 8, 1, 1)):
            value = parallelism(cpu, memory * GIB)
            self.assertEqual((value["package_parallelism"], value["gomaxprocs"]), (packages, procs))
            self.assertEqual(go_environment(value)["GOFLAGS"], f"-p={packages} -mod=readonly -buildvcs=false")
        fallback = parallelism(8, 16 * GIB, detected=False)
        self.assertEqual(fallback, {"package_parallelism": 1, "gomaxprocs": 1, "selection": "probe_unavailable"})
        with patch.object(platform_resources.platform, "system", return_value="Linux"), \
                patch.object(platform_resources.os, "sysconf", side_effect=PermissionError("private path")):
            measured = resources()
        self.assertFalse(measured["probe_complete"])
        self.assertEqual(measured["selection"], "probe_unavailable")
        self.assertEqual(measured["package_parallelism"], 1)
        for value in ({"package_parallelism": 4, "gomaxprocs": 2}, {"package_parallelism": 2, "gomaxprocs": 8}):
            with self.assertRaises(ValueError):
                go_environment(value)

    def test_fractional_cpu_and_ancestor_memory_limits_are_conservative(self):
        root = self.root / "cgroup"
        current = root / "job/child"
        current.mkdir(parents=True)
        (root / "cpu.max").write_text("max 100000")
        (root / "memory.max").write_text(str(8 * GIB))
        (root / "job/cpu.max").write_text("150000 100000")
        (current / "cpu.max").write_text("max 100000")
        (current / "memory.max").write_text("max")
        membership = self.root / "membership"
        membership.write_text("0::/job/child\n")
        cpus, memory = cgroup_limits(4, 16 * GIB, root, membership)
        self.assertEqual(cpus, 1)
        self.assertEqual(memory, 8 * GIB)
        self.assertEqual(parallelism(cpus, memory)["package_parallelism"], 1)
        membership.write_text("0::/../../outside\n")
        with self.assertRaises(ValueError):
            cgroup_limits(4, 16 * GIB, root, membership)

    def test_cache_deadline_removes_an_incomplete_entry(self):
        source, destination = self.root / "cache-large", self.root / "copy-large"
        source.mkdir()
        (source / "entry").write_bytes(b"x" * (2 * 1024 * 1024))
        # Expire after the first 1 MiB write, before the second chunk. No large
        # real copy or sleep is needed to verify the partial-entry cleanup.
        with patch.object(platform_resources.time, "monotonic", side_effect=[0, 0, 0, 0, 31, 31]):
            value = copy_build_cache(source, destination)
        self.assertFalse(value["complete"])
        self.assertEqual(value["stop_reason"], "time_limit")
        self.assertEqual(value["copied_bytes"], 0)
        self.assertFalse((destination / "entry").exists())
        self.assertEqual((source / "entry").stat().st_size, 2 * 1024 * 1024)

    def test_cache_copy_is_bounded_and_does_not_change_source_bytes_or_modes(self):
        source, destination = self.root / "cache", self.root / "copy"
        (source / "ab").mkdir(parents=True)
        original = source / "ab/entry"
        original.write_bytes(b"known Go cache entry")
        original.chmod(0o640)
        before = (original.read_bytes(), original.stat().st_mode)
        result = copy_build_cache(source, destination)
        self.assertTrue(result["complete"])
        self.assertEqual(result["copied_bytes"], len(before[0]))
        self.assertEqual((destination / "ab/entry").read_bytes(), before[0])
        self.assertEqual((original.read_bytes(), original.stat().st_mode), before)
        for folder, kwargs, reason in (("limited-bytes", {"byte_limit": 1}, "byte_limit"),
                                       ("limited-time", {"time_limit": 0}, "time_limit")):
            result = copy_build_cache(source, self.root / folder, **kwargs)
            self.assertFalse(result["complete"])
            self.assertEqual(result["stop_reason"], reason)
            self.assertEqual(result["copied_bytes"], 0)
            self.assertEqual((original.read_bytes(), original.stat().st_mode), before)
        (source / "unsafe").symlink_to(original)
        with self.assertRaises(ValueError):
            copy_build_cache(source, self.root / "symlink-copy")

    def test_systemd_failure_summary_drops_exception_body_and_unknown_values(self):
        raw = {"state": "failed", "cleanup": "passed", "phase": "build-bundle-a", "error_kind": "TimeoutExpired",
               "cleanup_error": "SECRET shiki_private https://login.invalid/private", "Output": "PRIVATE KEY BODY",
               "source_locations": [{"file": "test-systemd.py", "line": 225, "message": "SECRET"},
                                    {"file": "unknown_secret.py", "line": 3}],
               "active_upgrade_boundaries": ["agent-upgrade", "SECRET"],
               "build_cache_copy": {"copied_bytes": 100, "seconds": 0.5, "complete": True, "stop_reason": "complete", "path": "/private/secret"}}
        public = public_systemd_result(raw)
        self.assertEqual(public["state"], "failed")
        self.assertEqual(public["source_locations"], [{"file": "test-systemd.py", "line": 225}])
        self.assertNotIn("runtime", public)  # No claim before the helper confirms its PID1 gate.
        for secret in ("SECRET", "shiki_private", "https://", "PRIVATE KEY", "/private", "unknown_secret"):
            self.assertNotIn(secret, json.dumps(public))

    def test_systemd_nonzero_exit_still_exports_safe_failed_result(self):
        with patch.dict(os.environ, {"RUNNER_TEMP": str(self.root), "GOMAXPROCS": "2"}):
            acceptance = ci.Acceptance(self.root / "shiki-platform-test")
            acceptance.measured = {"package_parallelism": 2, "gomaxprocs": 2}
            def run(label, _args, **_kwargs):
                if label == "systemd-lifecycle":
                    directory = acceptance.private / "systemd"
                    directory.mkdir()
                    (directory / "result.json").write_text(json.dumps({"state": "failed", "cleanup": "passed", "phase": "build-bundle-a", "cleanup_error": "SECRET"}))
                    raise RuntimeError("scenario failed")
            with patch.object(acceptance, "run", side_effect=run), patch.object(ci, "native_identity", return_value={"os": "linux"}), \
                    patch.object(ci, "command", return_value="/fixture"):
                with self.assertRaisesRegex(RuntimeError, "scenario failed"):
                    acceptance.maintenance()
            summary = json.loads((acceptance.public / "systemd.json").read_text())
            self.assertEqual(summary["state"], "failed")
            self.assertEqual(summary["phase"], "build-bundle-a")
            self.assertNotIn("SECRET", json.dumps(summary))

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
            for root in ("/home/runner", "/tmp/shiki-platform-systemd-owned"):
                with self.subTest(root=root), self.assertRaisesRegex(ValueError, "unexpected native systemd root"):
                    systemd.validate_layout({"root": root})

    def test_account_setup_records_safe_outcomes_without_masking_failures(self):
        secret = "private command stderr and token"
        for code, outcome in ((0, "passed"), (9, "nonzero_exit"), (-15, "signal")):
            result = {}
            operation = lambda: SimpleNamespace(returncode=code, stdout=secret, stderr=secret)
            with patch.object(systemd.time, "monotonic", side_effect=(1.0, 3.5)):
                if code:
                    with self.assertRaises(RuntimeError):
                        systemd.account_step(result, "create-user", operation)
                else:
                    systemd.account_step(result, "create-user", operation)
            public = public_systemd_result(result)
            self.assertEqual(public["phase"], "create-user")
            self.assertEqual(public["account_setup"], [{"phase": "create-user", "outcome": outcome, "seconds": 2.5}])
            self.assertNotIn(secret, json.dumps(public))
        result = {}
        def timeout():
            raise subprocess.TimeoutExpired(secret, 60, output=secret, stderr=secret)
        with patch.object(systemd.time, "monotonic", side_effect=(0.0, 60.0)):
            with self.assertRaises(subprocess.TimeoutExpired):
                systemd.account_step(result, "create-user", timeout)
        result["account_setup"] += [
            {"phase": secret, "outcome": "passed", "seconds": 1},
            {"phase": "create-user", "outcome": secret, "seconds": 1},
            {"phase": "create-user", "outcome": "passed", "seconds": float("nan")},
            {"phase": "create-user", "outcome": "passed", "seconds": 999999},
        ]
        public = public_systemd_result(result)
        self.assertEqual(public["account_setup"], [{"phase": "create-user", "outcome": "timeout", "seconds": 60.0}])
        self.assertNotIn(secret, json.dumps(public))


if __name__ == "__main__":
    unittest.main(verbosity=2)
