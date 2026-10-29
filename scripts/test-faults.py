#!/usr/bin/env python3
"""Isolated kernel-errno and real small-tmpfs persistence fault acceptance."""
import argparse
import hashlib
import json
import os
import pathlib
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import uuid

IMAGE_BASE = "golang@sha256:632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af"


def run(command, *, env=None, output=None, timeout=300):
    result = subprocess.run([str(part) for part in command], env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
    if output:
        pathlib.Path(output).write_text(result.stdout)
    if result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {command[0]}\n{result.stdout}")
    return result.stdout


def inside():
    if os.geteuid() != 1000:
        raise RuntimeError("fault runtime must be ordinary UID 1000")
    evidence = pathlib.Path("/evidence")
    work = pathlib.Path("/work")
    env = dict(os.environ, GOCACHE="/cache", GOPATH="/go", GOENV="off",
               GOFLAGS="-mod=readonly", GOTOOLCHAIN="local", GORACE="halt_on_error=1", TMPDIR="/work")
    os.chdir("/source")
    print("Building frozen race-enabled fault scenario as UID 1000", flush=True)
    run(["go", "test", "-c", "-race", "-o", work / "fault.test", "./test/fault"],
        env=env, output=evidence / "build.log", timeout=600)
    binary = work / "fault.test"
    test_base = [binary, "-test.v", "-test.count=1", "-test.timeout=30s"]
    results = []
    print("Running portable backup/credential fault scenarios", flush=True)
    run(test_base + ["-test.run=^(TestBackupRenameFailurePreservesPrimaryAndRetry|TestFailedRevocationWriteDoesNotUndoPriorRevocation)$"],
        env=env, output=evidence / "portable.log", timeout=45)
    # Ordinals are measured on a single locked helper thread. Every injection
    # additionally must match its actual decoded FD path in the trace below.
    scenarios = [
        ("state-file-sync", 3, r"/state/\.state-[^>]+>"),
        ("state-directory-sync", 4, r"/state>"),
        ("auth-directory-sync", 4, r"/auth>"),
        ("sync-directory-sync", 10, r"/project>"),
        ("sync-history-sync", 11, r"/sync-state/history/[^>]+>"),
    ]
    for scenario, ordinal, target in scenarios:
        print(f"Injecting {scenario}: fsync #{ordinal}", flush=True)
        root = work / scenario
        selected = dict(env, SHIKI_FAULT_ROOT=str(root), SHIKI_FAULT_SCENARIO=scenario)
        run(test_base + ["-test.run=^TestPreparePersistenceFault$"], env=selected,
            output=evidence / f"{scenario}-prepare.log", timeout=45)
        trace = evidence / f"{scenario}.strace"
        run(["strace", "-f", "-qq", "-yy", "-o", trace,
             "-e", "trace=fsync,renameat,renameat2,unlinkat",
             "-e", f"inject=fsync:error=EIO:when={ordinal}"] + test_base +
            ["-test.run=^TestInjectedPersistenceFault$"], env=selected,
            output=evidence / f"{scenario}.log", timeout=45)
        injected = [line for line in trace.read_text().splitlines() if "(INJECTED)" in line]
        if len(injected) != 1 or "EIO" not in injected[0] or not re.search(target, injected[0]):
            raise RuntimeError(f"injection missed expected syscall/path: {scenario}: {injected}")
        run(test_base + ["-test.run=^TestRecoverInjectedPersistenceFault$"], env=selected,
            output=evidence / f"{scenario}-recovery.log", timeout=45)
        results.append({"scenario": scenario, "syscall": "fsync", "errno": "EIO",
                        "ordinal_on_locked_thread": ordinal, "injected_trace": injected[0],
                        "behavior_test": "passed", "new_process_recovery": "passed"})
    print("Exhausting only dedicated 8 MiB tmpfs for real ENOSPC", flush=True)
    run(test_base + ["-test.run=^TestRealTmpfsExhaustion$"],
        env=dict(env, SHIKI_FAULT_TMPFS="/faults"),
        output=evidence / "enospc.log", timeout=45)
    report = {
        "platform": run(["uname", "-sm"]).strip(),
        "go": run(["go", "version"]).strip(),
        "strace": run(["strace", "--version"]).splitlines()[0],
        "uid": os.geteuid(), "race": True,
        "portable_backup_and_auth_rename_faults": "passed",
        "injected_scenarios": results,
        "real_enospc": {"filesystem": "dedicated 8 MiB tmpfs", "state": "passed", "sync": "passed"},
        "limits": ["syscall EIO injection is not hardware power loss",
                   "tmpfs exhaustion is not persistent-disk crash durability",
                   "no exhaustive fsync, ENOSPC, rename or process-kill point matrix",
                   "no external accounts, host disk exhaustion or host service changes"],
    }
    (evidence / "results.json").write_text(json.dumps(report, indent=2) + "\n")
    print("All selected persistence fault scenarios passed", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", help="new private evidence directory (must not exist)")
    parser.add_argument("--inside", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.inside:
        inside()
        return
    repository = pathlib.Path(__file__).resolve().parent.parent
    mirror = os.environ.get("SHIKI_TEST_DEBIAN_MIRROR", "https://deb.debian.org").rstrip("/")
    if not re.fullmatch(r"https://[A-Za-z0-9.-]+(?::[0-9]+)?(?:/[A-Za-z0-9._/-]*)?", mirror):
        raise RuntimeError("Debian mirror must be a plain HTTPS origin/path")
    if args.output:
        output = pathlib.Path(args.output).resolve()
        output.mkdir(mode=0o700, parents=False, exist_ok=False)
    else:
        output = pathlib.Path(tempfile.mkdtemp(prefix="shiki-faults-", dir="/tmp"))
    # The private 0700 parent protects the evidence on the host. Its bind-mounted
    # child must be writable by the container's different ordinary UID.
    evidence = output / "evidence"
    evidence.mkdir(mode=0o777)
    evidence.chmod(0o777)
    source = output / "source"
    shutil.copytree(repository, source, ignore=shutil.ignore_patterns(".git", ".tools", "bin", "dist", "__pycache__"))
    hashes = {}
    for path in sorted(source.rglob("*")):
        if path.is_file() and (path.suffix in (".go", ".proto") or path.name in ("go.mod", "go.sum", "test-faults.py", "test-faults.Dockerfile")):
            hashes[str(path.relative_to(source))] = hashlib.sha256(path.read_bytes()).hexdigest()
    (output / "source-sha256.json").write_text(json.dumps(hashes, indent=2) + "\n")
    modules = run(["go", "env", "GOMODCACHE"]).strip()
    name = "shiki-faults-" + uuid.uuid4().hex[:12]
    image = name + ":local"
    child = None
    canceled = False

    def interrupted(signum, _frame):
        nonlocal canceled
        canceled = True
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        print(f"Evidence directory: {output}", flush=True)
        print("Building pinned ordinary-user fault image", flush=True)
        run(["docker", "build", "--build-arg", f"DEBIAN_MIRROR={mirror}/debian", "-f",
             source / "scripts/test-faults.Dockerfile", "-t", image, source / "scripts"],
            output=output / "image-build.log", timeout=600)
        arguments = ["docker", "create", "--name", name, "--label", "dev.shikigami.test=faults",
                     "--network=none", "--user=1000:1000", "--cap-drop=ALL", "--security-opt=no-new-privileges",
                     "--read-only", "--pids-limit=256", "--memory=2g", "--cpus=4",
                     "--tmpfs", "/work:rw,exec,nosuid,size=1g,mode=0700,uid=1000,gid=1000",
                     "--tmpfs", "/tmp:rw,exec,nosuid,size=128m,mode=1777",
                     "--tmpfs", "/faults:rw,noexec,nosuid,size=8m,mode=0700,uid=1000,gid=1000",
                     "--mount", f"type=bind,source={source},target=/source,readonly",
                     "--mount", f"type=bind,source={modules},target=/go/pkg/mod,readonly",
                     "--mount", "type=volume,source=shiki-fault-go-cache,target=/cache",
                     "--mount", f"type=bind,source={evidence},target=/evidence",
                     image, "python3", "/source/scripts/test-faults.py", "--inside"]
        cid = run(arguments).strip()
        inspect = json.loads(run(["docker", "inspect", cid]))[0]
        host = inspect["HostConfig"]
        isolation = {"id": cid, "user": inspect["Config"]["User"],
                     "network": host["NetworkMode"], "readonly_rootfs": host["ReadonlyRootfs"],
                     "cap_drop": host["CapDrop"], "cap_add": host["CapAdd"],
                     "privileged": host["Privileged"], "security_opt": host["SecurityOpt"],
                     "tmpfs": host["Tmpfs"], "mounts": inspect["Mounts"], "base": IMAGE_BASE}
        (output / "isolation.json").write_text(json.dumps(isolation, indent=2) + "\n")
        child = subprocess.Popen(["docker", "start", "-a", cid], text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        with (output / "scenario.log").open("w") as log:
            for line in child.stdout:
                print(line, end="", flush=True)
                log.write(line)
        status = child.wait()
        state = json.loads(run(["docker", "inspect", cid]))[0]["State"]
        (output / "container-exit.json").write_text(json.dumps(state, indent=2) + "\n")
        if status != 0 or state["ExitCode"] != 0:
            raise RuntimeError(f"fault scenario failed: docker={status}, container={state['ExitCode']}")
    finally:
        if child is not None and child.poll() is None:
            subprocess.run(["docker", "stop", "--time", "10", name], capture_output=True, timeout=20)
            try:
                child.wait(timeout=15)
            except subprocess.TimeoutExpired:
                child.terminate()
                child.wait(timeout=5)
        subprocess.run(["docker", "rm", "-f", name], capture_output=True, timeout=20)
        subprocess.run(["docker", "image", "rm", image], capture_output=True, timeout=30)
        evidence.chmod(0o700)
        absent = subprocess.run(["docker", "inspect", name], capture_output=True).returncode != 0
        (output / "cleanup.json").write_text(json.dumps({"container_absent": absent, "canceled": canceled}, indent=2) + "\n")
        if not absent:
            raise RuntimeError(f"owned test container cleanup failed: {name}")
    print(f"Verified evidence: {output}", flush=True)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(143)
    except Exception as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
