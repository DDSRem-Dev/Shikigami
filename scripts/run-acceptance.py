#!/usr/bin/env python3
"""Run a local acceptance scenario with frozen source and retained evidence."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time

from acceptance_evidence import (
    check_container_absent, collector_succeeded, command, native_process_check,
    now, remove_snapshot, require_command, snapshot_manifest, write_json,
)

IMAGE = "golang@sha256:632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af"
EXCLUDED = {".git", ".tools", "bin", "dist", ".DS_Store", "__pycache__", ".pytest_cache"}
MAX_LOG_BYTES = 128 << 20
CONTAINER_SETUP = """
set -euo pipefail
sed -i "s|http://deb.debian.org|$SHIKI_TEST_DEBIAN_MIRROR|g" /etc/apt/sources.list.d/debian.sources
apt-get -o Acquire::Retries=3 update -qq
apt-get -o Acquire::Retries=3 install -y -qq tmux iproute2 python3 >/dev/null
useradd -m shikitest
/usr/local/go/bin/go version
uname -m
tmux -V
exec runuser -u shikitest -- env PATH=/usr/local/go/bin:/usr/bin:/bin \
  GOCACHE=/tmp/shiki-go-cache GOPATH=/go SHIKI_RUN_EVIDENCE_DIR=/evidence "$@"
"""


def parser():
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--platform", choices=("native", "linux/arm64", "linux/amd64"), default="native")
    result.add_argument("--label", required=True)
    result.add_argument("--output", type=Path, help="new evidence directory outside the repository")
    result.add_argument("--timeout-seconds", type=int, default=13 * 3600)
    result.add_argument("scenario", nargs=argparse.REMAINDER, help="-- scripts/test-scenario.sh [arguments]")
    return result


def container_command(args, snapshot, artifacts, output, scenario):
    mirror = os.environ.get("SHIKI_TEST_DEBIAN_MIRROR", "https://deb.debian.org")
    if not re.fullmatch(r"https://[a-zA-Z0-9.-]+(:[0-9]{1,5})?", mirror):
        raise ValueError("SHIKI_TEST_DEBIAN_MIRROR must be an HTTPS origin")
    require_command(["go", "mod", "download"], timeout=300, cwd=snapshot)
    cache = require_command(["go", "env", "GOMODCACHE"], cwd=snapshot)
    # Outer directory stays 0700. Only this newly owned child mount permits the
    # disposable container's ordinary UID1000 to write test evidence.
    artifacts.chmod(0o777)
    return [
        "docker", "run", "--rm", "--init", "--platform", args.platform,
        "--cidfile", str(output / "container.cid"),
        "--mount", f"type=bind,source={snapshot},target=/workspace,readonly",
        "--mount", f"type=bind,source={cache},target=/go/pkg/mod,readonly",
        "--mount", f"type=bind,source={artifacts},target=/evidence",
        "--env", f"SHIKI_TEST_DEBIAN_MIRROR={mirror}",
        "--workdir", "/workspace", IMAGE, "bash", "-c", CONTAINER_SETUP,
        "shiki-acceptance", *scenario,
    ]


def capture_container(output, snapshot, evidence):
    cidfile = output / "container.cid"
    if not cidfile.is_file():
        return None
    cid = cidfile.read_text().strip()
    if not re.fullmatch(r"[a-f0-9]{64}", cid):
        return None
    evidence["container_id"] = cid
    if not evidence.get("actual_snapshot_verified"):
        inspected = command(["docker", "inspect", cid])
        evidence["container_started_inspect"] = inspected
        if inspected["exit_code"] == 0:
            mounts = json.loads(inspected["stdout"])[0]["Mounts"]
            actual = next(m for m in mounts if m["Destination"] == "/workspace")
            if Path(actual["Source"]).resolve() != snapshot.resolve() or actual["RW"]:
                raise RuntimeError("container does not use the frozen read-only snapshot")
            evidence["actual_snapshot_verified"] = True
    return cid


def build_process_guard(snapshot, output):
    guard = output / "process-guard"
    require_command(["go", "build", "-mod=readonly", "-o", str(guard),
                     "./test/fixtures/processguard"], timeout=300, cwd=snapshot)
    return guard


def reclaim_native(artifacts, guard, evidence):
    inventory = artifacts / "processes.json"
    cleanup = native_process_check(inventory)
    if cleanup["remaining_processes"]:
        evidence["forced_process_cleanup"] = True
        evidence["native_cleanup_attempts"] = []
        for sig, allowance in (("TERM", 20), ("KILL", 5)):
            if guard is None:
                evidence["errors"].append("owned process cleanup helper unavailable")
                break
            result = command([str(guard), "--inventory", str(inventory), "--signal", sig])
            evidence["native_cleanup_attempts"].append({"signal": sig, "result": result})
            until = time.monotonic() + allowance
            while time.monotonic() < until:
                cleanup = native_process_check(inventory)
                if not cleanup["remaining_processes"]:
                    break
                time.sleep(0.1)
            if not cleanup["remaining_processes"]:
                break
    return cleanup


def run(args):
    repository = Path(__file__).resolve().parent.parent
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,63}", args.label):
        raise ValueError("label must be 1..64 lowercase letters, digits or hyphens")
    if not 60 <= args.timeout_seconds <= 30 * 3600:
        raise ValueError("timeout must be between 60 seconds and 30 hours")
    scenario = list(args.scenario)
    if scenario and scenario[0] == "--":
        scenario.pop(0)
    if not scenario:
        raise ValueError("a scenario script is required")
    script = (repository / scenario[0]).resolve()
    if not script.is_relative_to(repository / "scripts") or script.suffix != ".sh" or not script.is_file():
        raise ValueError("scenario must be an existing shell script inside scripts/")
    scenario[0] = "./" + str(script.relative_to(repository))
    native = args.platform == "native"
    if native and os.getuid() == 0:
        raise ValueError("native acceptance must run as an ordinary user")
    if args.output:
        output = args.output.expanduser().absolute()
        if output.resolve().is_relative_to(repository):
            raise ValueError("evidence directory must be outside the repository")
        output.mkdir(mode=0o700, parents=False, exist_ok=False)
    else:
        output = Path(tempfile.mkdtemp(prefix=f"shiki-{args.label}-"))
    output = output.resolve()
    output.chmod(0o700)
    snapshot, artifacts = output / "source", output / "artifacts"
    artifacts.mkdir(mode=0o700)
    evidence = {
        "started": now(), "platform": args.platform, "scenario": scenario,
        "repository": str(repository), "directory": str(output),
        "snapshot": str(snapshot), "timeout_seconds": args.timeout_seconds,
        "application_exit_code": None, "errors": [],
    }
    evidence_path = output / "evidence.json"
    write_json(evidence_path, evidence)
    print(json.dumps({"directory": str(output), "evidence": str(evidence_path)}), flush=True)
    child, cid, guard = None, None, None
    interrupted, old_signals = [], {}
    deadline = time.monotonic() + args.timeout_seconds

    def signal_group(signum):
        if child is not None and child.poll() is None:
            try:
                os.killpg(child.pid, signum)
            except ProcessLookupError:
                pass

    def stop(signum, _frame):
        interrupted.append(signum)
        signal_group(signal.SIGTERM)

    try:
        for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            old_signals[sig] = signal.signal(sig, stop)
        shutil.copytree(repository, snapshot, symlinks=True, ignore=shutil.ignore_patterns(*EXCLUDED))
        manifest = snapshot_manifest(snapshot)
        write_json(output / "source-sha256.json", manifest)
        evidence["source_files"] = len(manifest)
        evidence["source_manifest_sha256"] = hashlib.sha256((output / "source-sha256.json").read_bytes()).hexdigest()
        if native:
            guard = build_process_guard(snapshot, output)
            evidence["process_guard_sha256"] = hashlib.sha256(guard.read_bytes()).hexdigest()
        env = os.environ.copy()
        env["SHIKI_RUN_EVIDENCE_DIR"] = str(artifacts)
        launch = scenario if native else container_command(args, snapshot, artifacts, output, scenario)
        if not native:
            evidence["image"] = IMAGE
        if interrupted or time.monotonic() >= deadline:
            raise InterruptedError("interrupted or deadline reached before launch")
        with (output / "run.log").open("w") as log:
            child = subprocess.Popen(launch, cwd=snapshot, env=env, stdout=log,
                                     stderr=subprocess.STDOUT, start_new_session=True)
            evidence["runner_pid"] = child.pid
            write_json(evidence_path, evidence)
            stopping_at = None
            while child.poll() is None:
                if (output / "run.log").stat().st_size > MAX_LOG_BYTES and stopping_at is None:
                    evidence["errors"].append("scenario output exceeded 128 MiB log limit")
                    stop(signal.SIGTERM, None)
                if not native and not evidence.get("actual_snapshot_verified"):
                    cid = capture_container(output, snapshot, evidence) or cid
                    write_json(evidence_path, evidence)
                if time.monotonic() >= deadline and stopping_at is None:
                    evidence["timed_out"] = True
                    stop(signal.SIGTERM, None)
                if interrupted and stopping_at is None:
                    stopping_at = time.monotonic()
                if stopping_at is not None and time.monotonic() - stopping_at >= 30:
                    signal_group(signal.SIGKILL)
                time.sleep(0.25)
            evidence["application_exit_code"] = child.wait()
        if native:
            evidence["actual_snapshot_verified"] = True
        final_manifest = snapshot_manifest(snapshot)
        evidence["source_postcheck_completed"] = True
        evidence["changed_source_inputs"] = [
            name for name in sorted(manifest.keys() | final_manifest.keys())
            if final_manifest.get(name) != manifest.get(name)
        ]
        if evidence["changed_source_inputs"]:
            evidence["errors"].append("scenario changed frozen source inputs")
    except Exception as error:
        evidence["errors"].append(f"{type(error).__name__}: {error}")
    finally:
        if child is not None and child.poll() is None:
            signal_group(signal.SIGTERM)
            try:
                child.wait(timeout=30)
            except subprocess.TimeoutExpired:
                signal_group(signal.SIGKILL)
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    evidence["errors"].append("runner did not exit after SIGKILL")
        if child is not None:
            evidence["application_exit_code"] = child.poll()
        # CID creation can occur between our last observation and launch failure.
        cidfile = output / "container.cid"
        if cid is None and cidfile.is_file():
            value = cidfile.read_text().strip()
            if re.fullmatch(r"[a-f0-9]{64}", value):
                cid = value
                evidence["container_id"] = cid
        try:
            if cid:
                cleanup = check_container_absent(cid)
                if cleanup["container_absent"] is False:
                    evidence["forced_container_cleanup"] = True
                    evidence["container_stop"] = command(["docker", "stop", "--time", "15", cid])
                    evidence["container_remove"] = command(["docker", "rm", "-f", cid])
                    cleanup = check_container_absent(cid)
                evidence["container_cleanup"] = cleanup
            elif not native:
                evidence["errors"].append("no actual container identity captured")
            else:
                evidence["process_cleanup"] = reclaim_native(artifacts, guard, evidence)
        except Exception as error:
            evidence["errors"].append(f"cleanup observation: {error}")
        try:
            if child is not None and child.poll() is None:
                evidence["snapshot_retained_reason"] = "runner still active"
            elif (snapshot.exists() or snapshot.is_symlink()) and not evidence.get("source_postcheck_completed"):
                evidence["snapshot_retained_reason"] = "source postcheck incomplete"
            else:
                remove_snapshot(snapshot)
            evidence["snapshot_removed"] = not (snapshot.exists() or snapshot.is_symlink())
        except OSError as error:
            evidence["errors"].append(f"snapshot cleanup: {error}")
        evidence["interruption_signals"] = interrupted
        evidence["completed"] = now()
        logpath = output / "run.log"
        if logpath.is_file():
            evidence["log_sha256"] = hashlib.sha256(logpath.read_bytes()).hexdigest()
        evidence["collector_exit_code"] = 0 if collector_succeeded(evidence, native) else 1
        write_json(evidence_path, evidence)
        for sig, old in old_signals.items():
            signal.signal(sig, old)
        print(json.dumps({"evidence": str(evidence_path),
                          "application_exit_code": evidence["application_exit_code"],
                          "collector_exit_code": evidence["collector_exit_code"]}), flush=True)
    return evidence["collector_exit_code"]


if __name__ == "__main__":
    try:
        sys.exit(run(parser().parse_args()))
    except (ValueError, OSError) as error:
        print(error, file=sys.stderr)
        sys.exit(2)
