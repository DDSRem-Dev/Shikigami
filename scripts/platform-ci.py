#!/usr/bin/env python3
"""Native platform acceptance. Upload only public/, never private test logs/state."""
import argparse
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

sys.dont_write_bytecode = True
from platform_support import command, native_identity, source_identity, test_events, write_json, require_verbose_pass, public_systemd_result

from platform_resources import resources, go_environment

ROOT = Path(__file__).resolve().parent.parent
REQUIRED_INTEGRATION = (
    "TestProductionSyncProcessThroughSupervisor", "TestFullCrossProcessDataPlane",
    "TestCommittedUploadLostACK", "TestIndependentProcessVersionBoundaries",
)
REQUIRED_E2E = (
    "TestCLIThreeProcessTLSLifecycle", "TestCLIPortForwardTLSLifecycleAndRecovery",
    "TestInstallerLocalReadinessAndExplicitTokenDelivery",
)
MAINTENANCE_PREREQUISITES = ("preflight", "build-lint", "tool-tests", "platform-tool-tests")
IDENTITY_FIELDS = ("os", "arch", "machine", "go", "kernel", "wsl")


class Acceptance:
    def __init__(self, evidence):
        self.root = Path(evidence).absolute()
        temporary = Path(os.environ["RUNNER_TEMP"]).resolve()
        if self.root.is_symlink() or self.root.parent.resolve() != temporary or not self.root.name.startswith("shiki-platform"):
            raise ValueError("evidence must be a dedicated shiki-platform directory directly under RUNNER_TEMP")
        self.root.mkdir(mode=0o700, exist_ok=True)
        self.public, self.private = self.root / "public", self.root / "private"
        for path in (self.public, self.private):
            if path.is_symlink():
                raise ValueError("evidence subdirectory cannot be a symlink")
            path.mkdir(mode=0o700, exist_ok=True)
        self.state = self.public / "stages.json"
        self.stages = json.loads(self.state.read_text()) if self.state.exists() else {}

    def run(self, label, args, *, timeout=1800, env=None, required=None, required_text=()):
        print(f"Platform acceptance: {label}", flush=True)
        started = time.monotonic()
        record = {"state": "running"}
        self.stages[label] = record
        write_json(self.state, self.stages)
        log = self.private / (label + ".log")
        try:
            with log.open("w") as stream:
                process = subprocess.Popen([str(arg) for arg in args], cwd=ROOT, env=env,
                                           stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
                try:
                    code = process.wait(timeout=timeout)
                except BaseException:
                    # Give the exact wrapper its full cleanup window. Killing only
                    # sudo/bash would bypass owned-service teardown in its child.
                    previous = {sig: signal.signal(sig, signal.SIG_IGN) for sig in (signal.SIGINT, signal.SIGTERM)}
                    try:
                        if process.poll() is None:
                            try:
                                os.killpg(process.pid, signal.SIGTERM)
                            except ProcessLookupError:
                                pass
                            try:
                                process.wait(timeout=150)
                            except subprocess.TimeoutExpired:
                                os.killpg(process.pid, signal.SIGKILL)
                                process.wait(timeout=10)
                    finally:
                        for sig, handler in previous.items():
                            signal.signal(sig, handler)
                    raise
            record["exit_code"] = code
            if code < 0 or code in (130, 143):
                raise KeyboardInterrupt("platform child process was interrupted")
            if required is not None:
                write_json(self.public / (label + ".json"), test_events(log, validate=False))
            if code:
                raise RuntimeError(f"{label} failed; named outcomes are public and raw output remains private")
            if required is not None:
                test_events(log, required)
            require_verbose_pass(log, required_text)
            if required_text:
                record["required_tests_passed"] = list(required_text)
            record["state"] = "passed"
        except BaseException as error:
            record["state"] = "failed"
            if isinstance(error, (KeyboardInterrupt, SystemExit)):
                record["cancelled"] = True
            raise
        finally:
            record["seconds"] = round(time.monotonic() - started, 3)
            write_json(self.state, self.stages)

    def preflight(self):
        if (self.public / "source.json").exists():
            raise RuntimeError("refusing to reuse acceptance evidence from an earlier run")
        identity = native_identity(ROOT)
        identity["resources"] = resources()
        environment = go_environment(identity["resources"])
        os.environ.update(environment)
        if os.environ.get("GITHUB_ENV") and not identity["wsl"]:
            with Path(os.environ["GITHUB_ENV"]).open("a") as stream:
                for key, value in environment.items():
                    stream.write(f"{key}={value}\n")
        write_json(self.public / "platform.json", identity)
        write_json(self.public / "source.json", source_identity(ROOT))
        self.stages["preflight"] = {"state": "passed"}
        write_json(self.state, self.stages)

    def product(self):
        self.run("build-lint", ["make", "build", "lint"])
        self.run("tool-tests", ["make", "test-tools"])
        self.run("platform-tool-tests", ["python3", "-B", "scripts/test-platform-ci.py"])
        self.run("product-tests", ["go", "test", "-count=1", "-race", "-json", "./..."], required=REQUIRED_INTEGRATION)
        for name in ("release-trust", "release"):
            self.run(name + "-tests", ["go", "test", "-count=1", "-race", "-json", f"scripts/{name}.go", f"scripts/{name}_test.go"], required=())
        self.run("e2e-tests", ["go", "test", "-count=1", "-race", "-json", "-tags=e2e", "./test/e2e/..."], required=REQUIRED_E2E)
        self.run("embedded-plugins", ["bash", "scripts/build-embedded.sh"],
                 env={**os.environ, "SHIKI_EMBED_VERIFY": "1", "GOFLAGS": os.environ.get("GOFLAGS", "") + " -v"},
                 required_text=("TestEmbeddedPluginExtractsAndRunsThroughNativeHost",))

    def terminal(self):
        evidence = self.private / "terminal"
        evidence.mkdir(mode=0o700)
        self.run("terminal-semantics", ["bash", "scripts/test-terminal-state.sh"],
                 env={**os.environ, "SHIKI_RUN_EVIDENCE_DIR": str(evidence)}, required_text=("TestFrontendSemanticRedraw",))

    def maintenance(self):
        identity = self.maintenance_admission()
        if identity["os"] == "darwin":
            self.run("launchd-lifecycle", ["bash", "scripts/test-launchd.sh"], timeout=1200,
                     required_text=("TestNativeLaunchAgentLifecycle",))
            for direction in ("upload", "download"):
                evidence = self.private / ("launchd-" + direction)
                evidence.mkdir(mode=0o700)
                self.run("launchd-active-" + direction, ["bash", "scripts/test-launchd-activity.sh",
                         *(["--download"] if direction == "download" else [])], timeout=1200,
                         env={**os.environ, "SHIKI_RUN_EVIDENCE_DIR": str(evidence)},
                         required_text=("TestNativeLaunchAgentUpgradeWithActiveTransfers",))
        else:
            self.run("systemd-modules", ["go", "mod", "download"], timeout=300)
            result = None
            try:
                self.run("systemd-lifecycle", ["sudo", "--preserve-env=GITHUB_ACTIONS,RUNNER_ENVIRONMENT",
                         "python3", "-B", ROOT / "scripts/platform-systemd.py", "--source", ROOT,
                         "--go", command(["go", "env", "GOROOT"]) + "/bin/go",
                         "--modules", command(["go", "env", "GOMODCACHE"]),
                         "--cache", command(["go", "env", "GOCACHE"]),
                         "--parallelism", str(self.measured["package_parallelism"]),
                         "--gomaxprocs", os.environ["GOMAXPROCS"],
                         "--evidence", self.private / "systemd"], timeout=2100)
            finally:
                report = self.private / "systemd/result.json"
                value = json.loads(report.read_text()) if report.is_file() else {"phase": "helper-result-missing"}
                result = public_systemd_result(value)
                write_json(self.public / "systemd.json", result)
            if result.get("state") != "passed" or result.get("cleanup") != "passed":
                raise RuntimeError("native systemd scenario or exact cleanup did not pass")

    def maintenance_admission(self):
        record = {"state": "blocked", "reason": "verification_failed"}
        self.stages["maintenance-admission"] = record
        try:
            if any(stage.get("cancelled") is True or stage.get("state") == "cancelled" or
                   (type(stage.get("exit_code")) is int and (stage["exit_code"] < 0 or stage["exit_code"] in (130, 143)))
                   for stage in self.stages.values()):
                record["reason"] = "cancelled"
                raise RuntimeError("maintenance refused after interrupted verification")
            missing = [name for name in MAINTENANCE_PREREQUISITES if self.stages.get(name, {}).get("state") != "passed"]
            if missing:
                record.update(reason="prerequisites_not_passed", missing_prerequisites=missing)
                raise RuntimeError("maintenance prerequisites did not pass")
            record["reason"] = "runtime_identity_unavailable"
            identity = native_identity(ROOT)
            frozen = json.loads((self.public / "platform.json").read_text())
            record["reason"] = "runtime_identity_changed"
            if any(key not in identity or frozen.get(key) != identity[key] for key in IDENTITY_FIELDS):
                raise RuntimeError("maintenance runtime identity differs from preflight")
            record["reason"] = "source_verification_failed"
            original = json.loads((self.public / "source.json").read_text())
            current = source_identity(ROOT)
            record["reason"] = "source_changed"
            if original != current:
                raise RuntimeError("maintenance source differs from preflight")
            record.update(state="passed", reason="source_and_prerequisites_verified")
            return identity
        finally:
            write_json(self.state, self.stages)

    def linux_pipeline(self):
        if native_identity(ROOT)["os"] != "linux":
            raise RuntimeError("independent maintenance pipeline requires Linux")
        failures = []
        try:
            self.product()
        except Exception:
            failures.append("product")
        else:
            try:
                self.terminal()
            except Exception:
                failures.append("terminal")
        # BaseException (including cancellation) deliberately bypasses this
        # branch. Maintenance independently rechecks source and prerequisites.
        try:
            self.maintenance()
        except Exception:
            failures.append("maintenance")
        if failures:
            raise RuntimeError("Linux acceptance stages failed: " + ", ".join(failures))

    def finalize(self):
        source = self.public / "source.json"
        source_ok = source.exists() and json.loads(source.read_text()) == source_identity(ROOT)
        required = {"preflight", "build-lint", "tool-tests", "platform-tool-tests", "product-tests", "release-trust-tests",
                    "release-tests", "e2e-tests", "embedded-plugins", "terminal-semantics"}
        if os.environ["SHIKI_PLATFORM_OS"] == "darwin":
            required.update(("launchd-lifecycle", "launchd-active-upload", "launchd-active-download"))
        else:
            required.add("systemd-lifecycle")
        pending = sorted(name for name in required if self.stages.get(name, {}).get("state") != "passed")
        passed = source_ok and not pending and os.environ.get("SHIKI_PLATFORM_JOB_STATUS") == "success"
        write_json(self.public / "result.json", {"state": "passed" if passed else "failed", "source_unchanged": source_ok,
                    "incomplete_stages": pending, "scope": "native local product and installation/service acceptance",
                    "external_accounts": "not tested", "physical_restart_login": "not tested"})
        if not passed:
            raise RuntimeError("native acceptance incomplete; see public/result.json")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("stage", choices=("preflight", "product", "terminal", "maintenance", "linux-pipeline", "finalize"))
    parser.add_argument("--evidence", required=True)
    arguments = parser.parse_args()
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, interrupted)
    acceptance = Acceptance(arguments.evidence)
    try:
        if arguments.stage not in ("preflight", "finalize"):
            if not (acceptance.public / "source.json").exists():
                raise RuntimeError("preflight must freeze source before tests")
            native_identity(ROOT)
            acceptance.measured = json.loads((acceptance.public / "platform.json").read_text())["resources"]
            os.environ.update(go_environment(acceptance.measured))
        getattr(acceptance, arguments.stage.replace("-", "_"))()
    except (KeyboardInterrupt, SystemExit):
        acceptance.stages["runner-interruption"] = {"state": "cancelled"}
        write_json(acceptance.state, acceptance.stages)
        raise


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # All exceptions here are controlled labels, not subprocess output.
        print(f"platform acceptance: {error}", file=sys.stderr)
        sys.exit(1)
