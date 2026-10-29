#!/usr/bin/env python3
"""Opt-in real-HOME macOS LaunchAgent schema recovery on private custom paths."""
import argparse
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import signal
import subprocess
import sys
import tempfile
import traceback

sys.dont_write_bytecode = True
from common import digest, inventory, run, write_json
from fixture import preflight


def freeze(source, destination):
    source = Path(source).resolve(strict=True)
    expected = json.loads((source.parent / "source-sha256.json").read_text())
    required = {"go.mod", "go.sum", "scripts/build.sh", "cmd/shiki-agent/main.go", "plugins/sync/main.go",
                "plugins/sync/client/worker.go", "pkg/plugin/state/store.go"}
    if not isinstance(expected, dict) or not required.issubset(expected):
        raise RuntimeError("source manifest is empty or lacks required product/build inputs")
    actual = inventory(source)
    extras = {name: checksum for name, checksum in actual.items() if name not in expected}
    if any("__pycache__" not in PurePosixPath(name).parts or not name.endswith(".pyc") for name in extras):
        raise RuntimeError(f"unrecognized extra source files: {source}")
    if any(actual.get(name) != checksum for name, checksum in expected.items()):
        raise RuntimeError(f"source does not match frozen manifest: {source}")
    # Acceptance helpers may have generated non-input Python cache files beside
    # an upstream snapshot. Copy only the independently recorded source inputs;
    # disclose the excluded cache hashes and reject every other unknown file.
    destination.mkdir(mode=0o700)
    for name in expected:
        relative = PurePosixPath(name)
        if relative.is_absolute() or ".." in relative.parts or str(relative) != name:
            raise RuntimeError("invalid source manifest path")
        target = destination / name
        target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        shutil.copy2(source / name, target)
    if inventory(destination) != expected:
        raise RuntimeError("copied source differs")
    return {"origin": str(source), "files": len(expected), "hashes": expected,
            "excluded_upstream_python_caches": extras,
            "origin_manifest_sha256": digest(source.parent / "source-sha256.json")}



def execute(args):
    initial = preflight()
    repository = Path(__file__).resolve().parents[3]
    output = Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix="shiki-schema-native-evidence-", dir="/private/tmp"))
    if output == repository or repository in output.parents:
        raise RuntimeError("evidence must be outside the repository")
    if args.output:
        output.mkdir(mode=0o700, parents=True, exist_ok=False)
    os.chmod(output, 0o700)
    write_json(output / "preflight.json", initial)
    old = freeze(args.old_source, output / "source-a")
    new = freeze(args.new_source, output / "source-b")
    differing = {name for name in set(old["hashes"]) | set(new["hashes"])
                 if old["hashes"].get(name) != new["hashes"].get(name)}
    if not any(name.startswith("plugins/sync/client/") and name.endswith(".go") for name in differing):
        raise RuntimeError("A/B require genuinely different sync source inputs")
    write_json(output / "source-a-sha256.json", old)
    write_json(output / "source-b-sha256.json", new)
    tools = output / "tools"
    shutil.copytree(Path(__file__).parent, tools, ignore=shutil.ignore_patterns("__pycache__"))
    shutil.copyfile(repository / "scripts/test-schema-upgrade-macos.sh", tools / "test-schema-upgrade-macos.sh")
    tool_hashes = inventory(tools)
    write_json(output / "tool-sha256.json", tool_hashes)
    root = Path(tempfile.mkdtemp(prefix="shiki-schema-macos.", dir="/private/tmp"))
    os.chmod(root, 0o700)
    write_json(output / "private-runtime.json", {"root": str(root), "real_home_unchanged": True})
    print(f"Evidence: {output}\nPrivate runtime: {root}", flush=True)
    child, failure = None, None
    cleanup = {"scenario_passed": False, "runtime_removed": False}
    try:
        with (output / "scenario.log").open("w") as log:
            child = subprocess.Popen([sys.executable, "-B", str(tools / "scenario.py"), str(root), str(output)],
                                     stdout=log, stderr=subprocess.STDOUT,
                                     env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})
            code = child.wait(timeout=1200)
        if code:
            raise RuntimeError(f"native scenario exited {code}; see scenario.log")
        cleanup["scenario_passed"] = True
    except BaseException as error:
        failure = error
    finally:
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        if child is not None and child.poll() is None:
            child.terminate()  # Exact Popen-owned child; never signal a remembered raw PID.
            try:
                child.wait(timeout=90)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=15)
                cleanup["scenario_forced_kill"] = True
        result = run([sys.executable, "-B", tools / "cleanup.py", root], check=False, timeout=180)
        (output / "cleanup.log").write_text(result.stdout + result.stderr)
        cleanup["cleanup_exit"] = result.returncode
        fallback = root / "cleanup-result.json"
        cleanup["fallback"] = json.loads(fallback.read_text()) if fallback.is_file() else {"errors": ["missing cleanup receipt"]}
        cleanup["sources_tools_unchanged"] = (inventory(output / "source-a") == old["hashes"]
                                                and inventory(output / "source-b") == new["hashes"]
                                                and inventory(tools) == tool_hashes)
        try:
            cleanup["final_preflight"] = preflight()
        except Exception as error:
            cleanup["final_preflight_error"] = str(error)
        cleanup["passed"] = (failure is None and cleanup["scenario_passed"] and result.returncode == 0
                             and not cleanup["fallback"].get("fallback_actions") and not cleanup["fallback"].get("errors")
                             and cleanup["sources_tools_unchanged"] and "final_preflight" in cleanup
                             and not cleanup.get("scenario_forced_kill"))
        if cleanup["passed"]:
            shutil.rmtree(root)
            cleanup["runtime_removed"] = not root.exists()
        write_json(output / "cleanup.json", cleanup)
    if failure:
        raise failure
    if not cleanup["passed"] or not cleanup["runtime_removed"]:
        raise RuntimeError("native cleanup/source validation failed; private runtime retained")
    print(f"PASS: {output}", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--old-source", required=True)
    parser.add_argument("--new-source", required=True)
    parser.add_argument("--output")
    args = parser.parse_args()
    def interrupted(signum, _frame):
        raise KeyboardInterrupt(f"signal {signum}")
    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    try:
        execute(args)
    except BaseException:
        traceback.print_exc()
        sys.exit(1)
