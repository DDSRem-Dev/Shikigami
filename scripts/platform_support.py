"""Small, secret-free validation helpers for disposable native CI acceptance."""
import hashlib
from functools import lru_cache
import json
import math
import os
from pathlib import Path
import platform
import re
import subprocess


GO_VERSION = "go1.26.10"
ARCHES = {"amd64": "amd64", "x86_64": "amd64", "x64": "amd64", "arm64": "arm64", "aarch64": "arm64"}


def command(args, *, cwd=None):
    result = subprocess.run([str(arg) for arg in args], cwd=cwd, capture_output=True, text=True, timeout=60)
    if result.returncode:
        # Command output can contain credentials. Never include it in public errors.
        raise RuntimeError(f"platform prerequisite command failed: {Path(str(args[0])).name}")
    return result.stdout.strip()


def hosted_guard(environment=None):
    environment = os.environ if environment is None else environment
    if environment.get("GITHUB_ACTIONS") != "true" or environment.get("RUNNER_ENVIRONMENT") != "github-hosted":
        raise RuntimeError("native maintenance requires a disposable GitHub-hosted runner")


def validate_identity(expected_os, expected_arch, actual_os, machine, runner_arch, go_env, *, translated=False):
    actual_arch = ARCHES.get(machine.lower())
    if expected_os not in ("linux", "darwin") or expected_arch not in ("amd64", "arm64"):
        raise ValueError("unsupported native target")
    if actual_os != expected_os or actual_arch != expected_arch or ARCHES.get(runner_arch.lower()) != expected_arch:
        raise ValueError("runner, kernel and requested platform disagree")
    if translated:
        raise ValueError("Rosetta execution is not native platform acceptance")
    required = {"GOHOSTOS": expected_os, "GOOS": expected_os, "GOHOSTARCH": expected_arch,
                "GOARCH": expected_arch, "GOVERSION": GO_VERSION}
    if any(go_env.get(key) != value for key, value in required.items()):
        raise ValueError("Go host/target/version differs from the native acceptance target")
    return {"os": expected_os, "arch": expected_arch, "machine": machine, "go": GO_VERSION}


def native_identity(repository):
    hosted_guard()
    if os.geteuid() == 0:
        raise RuntimeError("product acceptance must run as an ordinary user")
    actual_os = platform.system().lower()
    translated = actual_os == "darwin" and subprocess.run(
        ["sysctl", "-n", "sysctl.proc_translated"], capture_output=True, text=True).stdout.strip() == "1"
    go_env = json.loads(command(["go", "env", "-json", "GOHOSTOS", "GOHOSTARCH", "GOOS", "GOARCH", "GOVERSION"], cwd=repository))
    result = validate_identity(os.environ["SHIKI_PLATFORM_OS"], os.environ["SHIKI_PLATFORM_ARCH"],
                               actual_os, platform.machine(), os.environ["RUNNER_ARCH"], go_env, translated=translated)
    result["kernel"] = platform.release()
    result["wsl"] = os.environ.get("SHIKI_PLATFORM_WSL") == "1"
    if result["wsl"]:
        if actual_os != "linux" or "microsoft" not in platform.release().lower():
            raise RuntimeError("WSL acceptance requires an actual Microsoft WSL Linux kernel")
        for directory in (repository, Path.home(), Path(os.environ["RUNNER_TEMP"])):
            if command(["stat", "-f", "-c", "%T", directory]) not in ("ext2/ext3", "btrfs"):
                raise RuntimeError("WSL source, home and evidence must use the Linux filesystem")
        if Path("/proc/1/comm").read_text().strip() != "systemd":
            raise RuntimeError("WSL service acceptance requires real systemd PID 1")
    return result


def source_identity(repository):
    paths = subprocess.check_output(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=repository).split(b"\0")
    digests = {}
    for raw in sorted(set(paths)):
        if not raw:
            continue
        relative = os.fsdecode(raw)
        path = repository / relative
        if path.is_symlink() or not path.is_file() or path.resolve() != path:
            raise RuntimeError("source inputs must be canonical regular files")
        digests[relative] = hashlib.sha256(path.read_bytes()).hexdigest()
    return {"commit": command(["git", "rev-parse", "HEAD"], cwd=repository), "sha256": digests}


@lru_cache(maxsize=1)
def known_test_files():
    """Only tracked source basenames and real line bounds may leave private logs."""
    root = Path(__file__).resolve().parent.parent
    paths = subprocess.check_output(["git", "ls-files", "-z", "--", "*_test.go"], cwd=root).split(b"\0")
    result = {}
    for raw in paths:
        if not raw:
            continue
        path = root / os.fsdecode(raw)
        if path.is_symlink() or not path.is_file() or not re.fullmatch(r"[A-Za-z0-9_]+_test\.go", path.name):
            continue
        result[path.name] = max(result.get(path.name, 0), len(path.read_bytes().splitlines()))
    return result


# Values and prefixes are fixed in source. Never publish any suffix of a match.
FAILURE_KINDS = (
    ("readiness timeout:", "condition_timeout"),
    ("terminal timed out waiting for ", "terminal_marker"),
    ("terminal output exceeded test bound", "terminal_bound"),
    ("waiting for ", "process_or_terminal_wait"),
    ("concurrent startup:", "concurrent_start"),
    ("startup JSON:", "startup_json"),
    ("agent not ready:", "agent_not_ready"),
    ("build command entrypoints:", "build"),
    ("build plugin:", "build"),
    ("expected protocol JSON:", "protocol_json"),
    ("context deadline exceeded", "context_deadline"),
    ("context canceled", "context_cancelled"),
)
LOCATION = re.compile(r"^[ \t]*([A-Za-z0-9_]+_test\.go):([1-9][0-9]{0,5}):[ \t]*(.*)$")


def safe_test_locations(output, known):
    if not isinstance(output, str):
        return []
    result = []
    for line in output.splitlines():
        match = LOCATION.fullmatch(line)
        if match is None:
            continue
        name, number, message = match.groups()
        line_number = int(number)
        if name not in known or line_number > known[name]:
            continue
        # A failed test can also emit t.Log/cleanup output at a source location.
        # The neutral default makes no claim that every location is an assertion.
        kind = next((value for prefix, value in FAILURE_KINDS if message.startswith(prefix)), "test_output")
        location = {"file": name, "line": line_number, "kind": kind}
        if location not in result:
            result.append(location)
        if len(result) == 20:
            break
    return result


def test_events(path, required=(), *, validate=True):
    """Keep outcomes and bounded source locations; never publish raw Output."""
    tests, packages, locations, elapsed = {}, {}, {}, {}
    known = known_test_files()
    with path.open() as stream:
        for line in stream:
            try:
                event = json.loads(line)
            except ValueError:
                continue  # Tool build errors are still reflected by the exit code.
            if not isinstance(event, dict):
                continue
            action = event.get("Action")
            package = event.get("Package", "")
            key = (package, event.get("Test", ""))
            if action == "output" and key[1]:
                observed = locations.setdefault(key, [])
                for location in safe_test_locations(event.get("Output"), known):
                    if location not in observed and len(observed) < 20:
                        observed.append(location)
            if action not in ("pass", "fail", "skip"):
                continue
            if "Test" in event:
                tests[key] = action
                value = event.get("Elapsed")
                if type(value) in (int, float) and math.isfinite(value) and 0 <= value <= 86400:
                    elapsed[key] = round(value, 3)
            else:
                packages[package] = action
    results = []
    for (package, name), outcome in sorted(tests.items()):
        item = {"package": package, "test": name, "outcome": outcome}
        key = (package, name)
        if key in elapsed:
            item["elapsed_seconds"] = elapsed[key]
        if outcome == "fail":
            item["source_locations"] = locations.get(key, [])
        results.append(item)
    result = {"packages": packages, "tests": results}
    if validate:
        missing = [name for name in required if not any(test == name and outcome == "pass" for (_, test), outcome in tests.items())]
        if missing:
            raise RuntimeError("required acceptance tests did not pass: " + ", ".join(missing))
        if not packages or "fail" in packages.values() or "fail" in tests.values():
            raise RuntimeError("test stream lacks successful package completion")
    return result



def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def require_verbose_pass(path, names):
    # Opt-in shell runners execute compiled Go tests. A successful wrapper or a
    # skipped/no-tests run is insufficient to close their acceptance stage.
    lines = path.read_text().splitlines()
    for name in names:
        if not any(line.startswith("--- PASS: " + name + " (") for line in lines):
            raise RuntimeError("required opt-in test did not pass: " + name)
        if any(line.startswith(prefix + name + " (") for prefix in ("--- SKIP: ", "--- FAIL: ") for line in lines):
            raise RuntimeError("required opt-in test failed or skipped: " + name)


ACCOUNT_PHASES = frozenset(("lookup-account", "lookup-group", "create-group", "create-user"))
SYSTEMD_PHASES = ACCOUNT_PHASES | frozenset(("setup-account", "check-system-directories", "copy-modules", "copy-build-cache", "check-layout", "service-scenario",
                           "build-bundle-a", "build-bundle-b", "activate-daemon", "service-recovery", "crash-guard",
                           "active-transfer-probe", "agent-maintenance", "daemon-maintenance", "uninstall-retention",
                           "scenario-complete", "helper-result-missing", "unknown"))
SCRIPT_NAMES = frozenset(("platform-systemd.py", "platform_support.py", "platform_resources.py", "test-systemd.py", "systemd_activity.py"))
ERROR_KINDS = frozenset(("AssertionError", "TimeoutExpired", "RuntimeError", "ValueError", "FileNotFoundError",
                         "PermissionError", "KeyboardInterrupt", "OSError", "CalledProcessError", "InstallerFailure", "other"))
INSTALLER_ERRORS = {
    "system_prefix_ancestor_unprotected": "system prefix ancestors must be root-owned and protected from replacement",
    "system_command_ancestor_unprotected": "system binary directory and its parents must be root-owned and not writable by other users",
    "private_directory_owner_or_mode": "existing private directory has unexpected owner or permissions",
    "systemctl_failed": "systemctl failed:",
    "ordinary_user_config_failed": "ordinary-user config failed:",
    "ordinary_user_probe_failed": "ordinary-user probe failed:",
    "token_issuance_failed": "device Token issuance failed:",
}


class InstallerFailure(RuntimeError):
    """Keep raw command errors private, with a separate fixed public taxonomy."""
    def __init__(self, response):
        super().__init__(response.stderr)
        self.exit_class = "signal" if response.returncode < 0 else "nonzero_exit"
        self.error_codes = [name for name, message in INSTALLER_ERRORS.items() if message in response.stderr]
        if not self.error_codes:
            self.error_codes = ["unclassified"]


def safe_script_failure(error):
    import traceback
    root = Path(__file__).resolve().parent
    locations = []
    for frame in traceback.extract_tb(error.__traceback__):
        file = Path(frame.filename)
        if file.parent.resolve() == root and file.name in SCRIPT_NAMES:
            locations.append({"file": file.name, "line": frame.lineno})
    kind = type(error).__name__
    result = {"error_kind": kind if kind in ERROR_KINDS else "other", "source_locations": locations[-20:]}
    if isinstance(error, InstallerFailure):
        result.update(installer_exit_class=error.exit_class, installer_error_codes=error.error_codes)
    return result


def public_systemd_result(value):
    """Fixed fields only, including on failure; cleanup_error stays private."""
    result = {"state": "passed" if value.get("state") == "passed" else "failed",
              "cleanup": value.get("cleanup") if value.get("cleanup") in ("passed", "failed") else "unknown",
              "phase": value.get("phase") if value.get("phase") in SYSTEMD_PHASES else "unknown",
              "scope": "native systemd acceptance", "transport": "loopback TLS fixture; no external acceptance"}
    if value.get("runtime") == "native systemd PID 1":
        result["runtime"] = "native systemd PID 1"
    if "error_kind" in value:
        result["error_kind"] = value["error_kind"] if value["error_kind"] in ERROR_KINDS else "other"
    if value.get("installer_exit_class") in ("signal", "nonzero_exit"):
        result["installer_exit_class"] = value["installer_exit_class"]
        result["installer_error_codes"] = [name for name in value.get("installer_error_codes", [])
                                            if isinstance(name, str) and (name in INSTALLER_ERRORS or name == "unclassified")][:8]
    result["source_locations"] = [
        {"file": item["file"], "line": item["line"]} for item in value.get("source_locations", [])
        if isinstance(item, dict) and item.get("file") in SCRIPT_NAMES and type(item.get("line")) is int and 1 <= item["line"] <= 10000
    ][:20]
    if type(value.get("operations")) is int and 0 <= value["operations"] <= 100:
        result["operations"] = value["operations"]
    result["system_directories"] = [
        {key: item[key] for key in ("path", "kind", "root_owned", "writable_by_others")}
        for item in value.get("system_directories", []) if isinstance(item, dict)
        and item.get("path") in ("/", "/var", "/var/lib", "/opt")
        and item.get("kind") in ("directory", "symlink", "other", "unavailable")
        and type(item.get("root_owned")) is bool and type(item.get("writable_by_others")) is bool
    ][:4]
    result["account_setup"] = [
        {"phase": item["phase"], "outcome": item["outcome"], "seconds": item["seconds"]}
        for item in value.get("account_setup", []) if isinstance(item, dict)
        and item.get("phase") in ACCOUNT_PHASES
        and item.get("outcome") in ("passed", "nonzero_exit", "signal", "timeout", "failed")
        and type(item.get("seconds")) in (int, float) and math.isfinite(item["seconds"])
        and 0 <= item["seconds"] <= 86400
    ][:4]
    allowed = {"agent-upgrade", "agent-rollback", "daemon-upgrade", "daemon-rollback"}
    result["active_upgrade_boundaries"] = sorted(name for name in value.get("active_upgrade_boundaries", []) if name in allowed)
    if value.get("uninstall_retains_data") is True:
        result["uninstall_retains_data"] = True
    cache = value.get("build_cache_copy")
    if isinstance(cache, dict):
        metrics = {key: cache[key] for key in ("copied_bytes", "copied_files", "seconds", "byte_limit", "time_limit_seconds")
                   if type(cache.get(key)) in (int, float) and math.isfinite(cache[key]) and 0 <= cache[key] <= 2**53}
        metrics["complete"] = cache.get("complete") is True
        metrics["stop_reason"] = cache.get("stop_reason") if cache.get("stop_reason") in ("complete", "time_limit", "byte_limit") else "unknown"
        result["build_cache_copy"] = metrics
    return result
