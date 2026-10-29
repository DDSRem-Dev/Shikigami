#!/usr/bin/env python3
"""Observe install staging durability and fault its changed fsync boundaries in isolated ordinary-user Linux."""
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


def fsyncs(trace):
    return [line for line in trace.read_text().splitlines() if "fsync(" in line]


def decoded_path(line):
    match = re.search(r"fsync\(\d+<([^>]+)>\)", line)
    if not match:
        raise RuntimeError(f"cannot decode fsync path: {line}")
    return match.group(1)


def relative_path(line, root):
    return re.sub(r"\.stage-[^/]+", ".stage-*", decoded_path(line).removeprefix(str(root)))


def inside():
    if os.geteuid() != 1000:
        raise RuntimeError("fault runtime must be ordinary UID 1000")
    evidence, work = pathlib.Path("/evidence"), pathlib.Path("/work")
    env = dict(os.environ, GOCACHE="/cache", GOPATH="/go", GOENV="off",
               GOFLAGS="-p=1 -mod=readonly -buildvcs=false", GOTOOLCHAIN="local", GOMAXPROCS="2",
               GORACE="halt_on_error=1", TMPDIR="/work")
    binaries = {}
    for label in ("before", "after"):
        os.chdir("/source-before" if label == "before" else "/source")
        binary = work / (label + ".test")
        print(f"Building frozen {label} race-enabled staging scenario", flush=True)
        run(["go", "test", "-c", "-race", "-o", binary, "./test/fault"], env=env,
            output=evidence / f"{label}-build.log", timeout=900)
        binaries[label] = [binary, "-test.v", "-test.count=1", "-test.timeout=30s"]
    def invoke(label, test, selected, output, trace=None, ordinal=None):
        command = binaries[label] + ["-test.run=^" + test + "$"]
        if trace:
            prefix = ["strace", "-f", "-qq", "-yy", "-o", trace,
                      "-e", "trace=fsync,renameat,renameat2,unlinkat"]
            if ordinal:
                prefix += ["-e", f"inject=fsync:error=EIO:when={ordinal}"]
            command = prefix + command
        run(command, env=selected, output=output, timeout=45)
    observations = {}
    for label in ("before", "after"):
        root = work / (label + "-observation")
        selected = dict(env, SHIKI_STAGE_ROOT=str(root))
        invoke(label, "TestPrepareInstallStage", selected, evidence / f"{label}-prepare.log")
        for kind in ("fresh", "existing"):
            trace = evidence / f"{label}-{kind}.strace"
            invoke(label, "TestObserveInstallStage", selected, evidence / f"{label}-{kind}.log", trace)
            lines = fsyncs(trace)
            observations[label + "-" + kind] = [relative_path(line, root) for line in lines]
    before = observations["before-fresh"]
    missing = ["release.json", "SHA256SUMS", "releases"]
    if any(any(path.endswith("/" + name) for path in before) for name in missing):
        raise RuntimeError("before source does not reproduce all three documented missing syncs")
    if observations["before-existing"]:
        raise RuntimeError("before existing-release path unexpectedly fsyncs")
    for kind in ("fresh", "existing"):
        paths = observations["after-" + kind]
        if len(paths) != 17 or paths[-2:] != ["/prefix/releases", "/prefix"]:
            raise RuntimeError(f"incomplete after staging durability sequence: {paths}")
        if not paths[12].endswith("/release.json") or not paths[13].endswith("/SHA256SUMS"):
            raise RuntimeError("metadata not synced before publication")
    results = []
    for kind in ("fresh", "existing"):
        for ordinal, target in enumerate(observations["after-" + kind], 1):
            if ordinal not in ({13, 14, 16, 17} if kind == "fresh" else {13, 14, 15, 16, 17}):
                continue
            scenario = f"{kind}-{ordinal:02d}"
            root = work / scenario
            selected = dict(env, SHIKI_STAGE_ROOT=str(root), SHIKI_STAGE_EXISTING="1" if kind == "existing" else "0")
            print(f"Injecting {scenario}: {target}", flush=True)
            invoke("after", "TestPrepareInstallStage", selected, evidence / f"{scenario}-prepare.log")
            trace = evidence / f"{scenario}.strace"
            invoke("after", "TestInjectedInstallStage", selected, evidence / f"{scenario}.log", trace, ordinal)
            injected = [line for line in trace.read_text().splitlines() if "(INJECTED)" in line]
            if len(injected) != 1 or "EIO" not in injected[0] or relative_path(injected[0], root) != target:
                raise RuntimeError(f"injection missed exact expected path: {scenario}: {injected}")
            invoke("after", "TestRecoverInstallStage", selected, evidence / f"{scenario}-recovery.log")
            results.append({"scenario": scenario, "ordinal_on_locked_thread": ordinal,
                            "target": target, "injected_trace": injected[0],
                            "failed_without_activation_or_state_change": True, "new_process_retry_twice": "passed"})
    report = {"platform": run(["uname", "-sm"]).strip(), "go": run(["go", "version"]).strip(),
              "strace": run(["strace", "--version"]).splitlines()[0], "uid": os.geteuid(), "race": True,
              "before_red_kind": "observed syscall contract omissions; not power-loss behavior reproduction",
              "observations": observations, "injections": results,
              "limits": ["kernel syscall EIO injection is not hardware power loss",
                         "synthetic verified payloads and inert lifecycle; no actual service or payload execution",
                         "selected Stage fsync points only; not all installer operations or storage filesystems"]}
    (evidence / "results.json").write_text(json.dumps(report, indent=2) + "\n")
    print("All 9 changed staging fsync EIO cases and new-process retries passed", flush=True)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--before-artifacts", help="frozen pre-fix artifacts.go for observed red comparison")
    parser.add_argument("--output", help="new private evidence directory (must not exist)")
    parser.add_argument("--inside", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.inside:
        inside()
        return
    if not args.before_artifacts:
        parser.error("--before-artifacts is required outside the container")
    baseline = pathlib.Path(args.before_artifacts).resolve().read_bytes()
    repository = pathlib.Path(__file__).resolve().parent.parent
    mirror = os.environ.get("SHIKI_TEST_DEBIAN_MIRROR", "https://deb.debian.org").rstrip("/")
    if not re.fullmatch(r"https://[A-Za-z0-9.-]+(?::[0-9]+)?(?:/[A-Za-z0-9._/-]*)?", mirror):
        raise RuntimeError("Debian mirror must be a plain HTTPS origin/path")
    if args.output:
        output = pathlib.Path(args.output).resolve()
        output.mkdir(mode=0o700, parents=False, exist_ok=False)
    else:
        output = pathlib.Path(tempfile.mkdtemp(prefix="shiki-install-stage-", dir="/tmp"))
    # The private 0700 parent protects the evidence on the host. Its bind-mounted
    # child must be writable by the container's different ordinary UID.
    evidence = output / "evidence"
    evidence.mkdir(mode=0o777)
    evidence.chmod(0o777)
    source = output / "source"
    shutil.copytree(repository, source, ignore=shutil.ignore_patterns(".git", ".tools", "bin", "dist", "__pycache__"))
    before_source = output / "source-before"
    shutil.copytree(source, before_source)
    (before_source / "internal/install/artifacts.go").write_bytes(baseline)
    (output / "before-artifacts-sha256.txt").write_text(hashlib.sha256(baseline).hexdigest() + "\n")
    hashes = {}
    for path in sorted(source.rglob("*")):
        if path.is_file() and (path.suffix in (".go", ".proto", ".py") or path.name in ("go.mod", "go.sum", "test-faults.py", "test-faults.Dockerfile")):
            hashes[str(path.relative_to(source))] = hashlib.sha256(path.read_bytes()).hexdigest()
    (output / "source-sha256.json").write_text(json.dumps(hashes, indent=2) + "\n")
    modules = run(["go", "env", "GOMODCACHE"]).strip()
    name = "shiki-install-stage-" + uuid.uuid4().hex[:12]
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
        arguments = ["docker", "create", "--name", name, "--label", "dev.shikigami.test=install-stage",
                     "--network=none", "--user=1000:1000", "--cap-drop=ALL", "--security-opt=no-new-privileges",
                     "--read-only", "--pids-limit=256", "--memory=2g", "--cpus=2",
                     "--tmpfs", "/work:rw,exec,nosuid,size=1g,mode=0700,uid=1000,gid=1000",
                     "--tmpfs", "/tmp:rw,exec,nosuid,size=128m,mode=1777",

                     "--mount", f"type=bind,source={source},target=/source,readonly",
                     "--mount", f"type=bind,source={before_source},target=/source-before,readonly",
                     "--mount", f"type=bind,source={modules},target=/go/pkg/mod,readonly",
                     "--mount", "type=volume,source=shiki-fault-go-cache,target=/cache",
                     "--mount", f"type=bind,source={evidence},target=/evidence",
                     image, "python3", "/source/scripts/test-install-stage.py", "--inside"]
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
