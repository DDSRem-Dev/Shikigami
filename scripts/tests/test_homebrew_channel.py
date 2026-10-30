"""Owned cleanup and channel checks use temporary files/fakes, never local brew."""

import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("homebrew_channel", Path(__file__).resolve().parents[1] / "test-homebrew-channel.py")
channel = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(channel)


class FakeChannel(channel.Channel):
    def __init__(self, evidence):
        super().__init__(evidence)
        self.calls = []
        self.users = {}
        self.uninstall_failures = set()
        self.extra_final = None
        self.state["baseline"] = {"preexisting": "preserve"}

    def brew(self, *args, **kwargs):
        self.calls.append(args)
        if args[0] == "uses":
            return "\n".join(name for name in self.users.get(args[-1], [])
                             if name not in self.state["owned"] or Path(self.state["owned"][name]["path"]).exists())
        if args[0] in ("uninstall", "untap"):
            if args[-1] in self.uninstall_failures:
                raise RuntimeError("simulated cleanup command failure")
            shutil.rmtree(self.state["owned"][args[-1]]["path"])
            return ""
        raise AssertionError(args)

    def observe(self):
        return self.extra_final or self.state["baseline"]

    def created(self, name, kind="formula"):
        path = self.evidence / (name.replace("/", "-") + "-owned")
        self.own(name, path, kind, name)
        path.mkdir()
        self.claim_after(name)
        return path


class ChannelTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="shiki-brew-fake-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def test_local_self_hosted_and_wrong_native_targets_are_rejected(self):
        with patch.dict(channel.os.environ, {}, clear=True), patch.object(channel.os, "geteuid", return_value=501):
            with self.assertRaisesRegex(ValueError, "GitHub-hosted"):
                channel.hosted_guard("linux", "arm64")
        env = {"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "self-hosted", "RUNNER_ARCH": "ARM64"}
        with patch.dict(channel.os.environ, env), patch.object(channel.os, "geteuid", return_value=1001):
            with self.assertRaises(ValueError):
                channel.hosted_guard("linux", "arm64")
        env["RUNNER_ENVIRONMENT"] = "github-hosted"
        with patch.dict(channel.os.environ, env), patch.object(channel.os, "geteuid", return_value=1001), \
                patch.object(channel.platform, "system", return_value="Linux"), \
                patch.object(channel.platform, "machine", return_value="x86_64"):
            with self.assertRaisesRegex(ValueError, "native uname"):
                channel.hosted_guard("linux", "arm64")

    def test_existing_or_replaced_objects_are_never_deleted(self):
        run = FakeChannel(self.root)
        existing = self.root / "preexisting"
        existing.mkdir()
        with self.assertRaises(ValueError):
            run.own("existing", existing, "formula", "existing")
        path = run.created(channel.FORMULA)
        old = self.root / "original"
        path.rename(old)
        path.mkdir()
        (path / "unrelated").write_text("do not remove")
        self.assertFalse(run.cleanup())
        self.assertTrue(existing.exists())
        self.assertTrue(old.exists())
        self.assertTrue((path / "unrelated").exists())
        self.assertFalse(any(call[0] == "uninstall" for call in run.calls))

    def test_partial_install_is_cleaned_without_turning_failure_into_pass(self):
        run = FakeChannel(self.root)
        run.created(channel.TAP, "tap")
        partial = run.created(channel.FORMULA)
        (partial / "unfinished").write_text("partial bundle")
        run.state["errors"].append("installation failed")
        self.assertTrue(run.cleanup())
        self.assertFalse(partial.exists())
        self.assertFalse(run.state["passed"])
        self.assertEqual([call[0] for call in run.calls if call[0] != "uses"], ["uninstall", "untap"])

    def test_unconfirmed_creation_is_preserved_and_failed(self):
        run = FakeChannel(self.root)
        path = self.root / "unknown-creation"
        run.own(channel.FORMULA, path, "formula", channel.FORMULA)
        path.mkdir()
        self.assertFalse(run.cleanup())
        self.assertTrue(path.exists())
        self.assertFalse(run.calls)

    def test_only_owned_unused_dependency_leaves_are_removed(self):
        run = FakeChannel(self.root)
        for name in (channel.FORMULA, "a-dependency", "z-dependent"):
            run.created(name)
        run.created(channel.TAP, "tap")
        run.users = {"a-dependency": ["z-dependent"], "z-dependent": [channel.FORMULA]}
        run.state["completed_sequence"] = True
        self.assertTrue(run.cleanup())
        self.assertTrue(run.state["passed"])
        removed = [call[-1] for call in run.calls if call[0] == "uninstall"]
        self.assertEqual(removed, [channel.FORMULA, "z-dependent", "a-dependency"])

    def test_foreign_dependency_users_and_cleanup_failure_remain_visible(self):
        run = FakeChannel(self.root)
        dependency = run.created("dependency")
        run.users["dependency"] = ["preexisting-consumer"]
        run.state["completed_sequence"] = True
        self.assertFalse(run.cleanup())
        self.assertTrue(dependency.exists())
        run.users.clear()
        self.assertTrue(run.cleanup())
        self.assertFalse(run.state["passed"], "cleanup retry must not erase the earlier failure")
        self.assertEqual(len(run.state["cleanup_attempts"]), 2)

    def test_protected_state_change_fails_final_baseline(self):
        run = FakeChannel(self.root)
        run.state["completed_sequence"] = True
        run.extra_final = {"preexisting": "changed"}
        self.assertFalse(run.cleanup())
        self.assertIn("baseline", run.state["cleanup_errors"][0])

    def test_command_timeout_reaps_its_own_process_and_retains_log(self):
        run = channel.Channel(self.root)
        with self.assertRaises(subprocess.TimeoutExpired):
            run.command([sys.executable, "-c", "import time;time.sleep(30)"], timeout=0.05)
        record = run.state["commands"][0]
        self.assertIsNotNone(record["exit_code"])
        self.assertTrue((self.root / record["log"]).exists())
        self.assertEqual(record["sha256"], channel.digest(self.root / record["log"]))

    def test_stderr_warnings_do_not_pollute_machine_readable_stdout(self):
        run = channel.Channel(self.root)
        output = run.command([sys.executable, "-c", "import sys;print('[]');print('Warning: example',file=sys.stderr)"])
        self.assertEqual(json.loads(output), [])
        record = run.state["commands"][0]
        self.assertIn("Warning: example", (self.root / record["stderr_log"]).read_text())
        self.assertEqual(record["stderr_sha256"], channel.digest(self.root / record["stderr_log"]))

    def test_formula_key_is_bound_to_independent_checkout_key(self):
        formula = self.root / "formula.rb"
        key = self.root / "key.pub"
        key.write_text("PUBLIC KEY\n")
        formula.write_text("# shikigami-release: v0.0.1-alpha.2\n"
                           "RELEASE_PUBLIC_KEY = <<~'PEM'.freeze\nPUBLIC KEY\n  PEM\n"
                           "RELEASE_SIGNATURES = JSON.parse(<<~'JSON').freeze\n{}\n  JSON\n")
        self.assertEqual(channel.formula_metadata(formula, key), ("v0.0.1-alpha.2", {}))
        key.write_text("OTHER KEY\n")
        with self.assertRaisesRegex(ValueError, "independently trusted"):
            channel.formula_metadata(formula, key)

    def test_installed_bundle_preserves_exact_members_hashes_and_target(self):
        bundle = self.root / "bundle"
        bundle.mkdir()
        for name in channel.MEMBERS - {"SHA256SUMS"}:
            (bundle / name).write_text(name)
        (bundle / "release.json").write_text(json.dumps({"version": "v0.0.1-alpha.2", "os": "linux", "arch": "arm64"}))
        checksums = "".join(f"{channel.digest(bundle / name)}  {name}\n" for name in sorted(channel.MEMBERS - {"SHA256SUMS"}))
        (bundle / "SHA256SUMS").write_text(checksums)
        payload = {"version": "v0.0.1-alpha.2", "os": "linux", "arch": "arm64", "checksums_sha256": hashlib.sha256(checksums.encode()).hexdigest()}
        envelope = {"payload": base64.b64encode(json.dumps(payload).encode()).decode()}
        self.assertEqual(len(channel.verify_bundle(bundle, envelope, "v0.0.1-alpha.2", "linux", "arm64")), 13)
        with self.assertRaisesRegex(ValueError, "native runner"):
            channel.verify_bundle(bundle, envelope, "v0.0.1-alpha.2", "linux", "amd64")
        (bundle / "shiki").write_text("corrupt")
        with self.assertRaisesRegex(ValueError, "payload"):
            channel.verify_bundle(bundle, envelope, "v0.0.1-alpha.2", "linux", "arm64")


if __name__ == "__main__":
    unittest.main()
