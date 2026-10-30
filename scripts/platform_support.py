"""Small, secret-free validation helpers for disposable native CI acceptance."""
import hashlib
import json
import os
from pathlib import Path
import platform
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
    paths = subprocess.check_output(["git", "ls-files", "-z"], cwd=repository).split(b"\0")
    digests = {}
    for raw in paths:
        if not raw:
            continue
        relative = os.fsdecode(raw)
        path = repository / relative
        if path.is_symlink() or not path.is_file():
            raise RuntimeError("tracked source must be regular files")
        digests[relative] = hashlib.sha256(path.read_bytes()).hexdigest()
    return {"commit": command(["git", "rev-parse", "HEAD"], cwd=repository), "sha256": digests}


def test_events(path, required=(), *, validate=True):
    """Retain only named Go outcomes; Output is deliberately never published."""
    tests, packages = {}, {}
    with path.open() as stream:
        for line in stream:
            try:
                event = json.loads(line)
            except ValueError:
                continue  # Tool build errors are still reflected by the exit code.
            action = event.get("Action")
            if action not in ("pass", "fail", "skip"):
                continue
            package = event.get("Package", "")
            if "Test" in event:
                tests[(package, event["Test"])] = action
            else:
                packages[package] = action
    result = {"packages": packages, "tests": [{"package": package, "test": name, "outcome": outcome}
                                             for (package, name), outcome in sorted(tests.items())]}
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
