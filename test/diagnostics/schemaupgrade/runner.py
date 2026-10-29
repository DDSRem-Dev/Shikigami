#!/usr/bin/env python3
"""Real systemd schema1 -> schema2 -> refused schema1 -> controlled recovery.

Both inputs must be frozen source trees with their adjacent source-sha256.json.
No service is started on the host. This is not a release or automatic-restore tool.
"""
import argparse
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import signal
import sys
import tempfile
import traceback
import uuid

sys.dont_write_bytecode = True
from common import digest, inventory, run, wait_for, write_json


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
    repository = Path(__file__).resolve().parents[3]
    output = Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix="shiki-schema-upgrade-", dir="/tmp"))
    if output == repository or repository in output.parents:
        raise RuntimeError("evidence must be outside the repository")
    if args.output:
        output.mkdir(mode=0o700, parents=True, exist_ok=False)
    os.chmod(output, 0o700)
    old = freeze(args.old_source, output / "source-a")
    new = freeze(args.new_source, output / "source-b")
    differing = {name for name in set(old["hashes"]) | set(new["hashes"])
                 if old["hashes"].get(name) != new["hashes"].get(name)}
    if not any(name.startswith("plugins/sync/client/") and name.endswith(".go") for name in differing):
        raise RuntimeError("A/B must contain different sync implementation inputs, not just version labels")
    write_json(output / "source-a-sha256.json", old)
    write_json(output / "source-b-sha256.json", new)
    tools = output / "tools"
    shutil.copytree(Path(__file__).parent, tools, ignore=shutil.ignore_patterns("__pycache__"))
    shutil.copyfile(repository / "scripts/test-systemd.Dockerfile", tools / "Dockerfile")
    tool_hashes = inventory(tools)
    write_json(output / "tool-sha256.json", tool_hashes)
    engine = json.loads(run(["docker", "info", "--format", "{{json .}}" ]).stdout)
    if engine["OSType"] != "linux" or engine["CgroupVersion"] != "2":
        raise RuntimeError("Linux Docker with cgroup v2 required")
    modules = run(["go", "env", "GOMODCACHE"], cwd=repository).stdout.strip()
    name = "shiki-schema-" + uuid.uuid4().hex[:12]
    image = name + ":local"
    container_id = None
    passed = False
    cleanup = {"normal_shutdown": False, "forced_removal": False}
    print(f"Evidence: {output}", flush=True)
    try:
        build = run(["docker", "build", "-f", tools / "Dockerfile", "-t", image, tools], timeout=360)
        (output / "image-build.log").write_text(build.stdout + build.stderr)
        result = run(["docker", "run", "-dt", "--name", name, "--label", "dev.shikigami.schema-test=" + name,
                      "--cgroupns", "private", "--network", "none", "--cap-add", "SYS_ADMIN",
                      "--tmpfs", "/run", "--tmpfs", "/run/lock", "--tmpfs", "/tmp",
                      "--mount", f"type=bind,source={output / 'source-a'},target=/source-a,readonly",
                      "--mount", f"type=bind,source={output / 'source-b'},target=/source-b,readonly",
                      "--mount", f"type=bind,source={tools},target=/tools,readonly",
                      "--mount", f"type=bind,source={modules},target=/go/pkg/mod,readonly",
                      "--mount", "type=volume,source=shiki-systemd-go-cache,target=/var/cache/shiki-go",
                      image, "/bin/bash", "-c",
                      "set -euo pipefail; mount -o remount,rw /sys/fs/cgroup; exec /sbin/init --log-target=console --log-level=info"])
        container_id = result.stdout.strip()
        details = json.loads(run(["docker", "inspect", container_id]).stdout)[0]
        assert details["Id"] == container_id
        assert details["Config"]["Labels"]["dev.shikigami.schema-test"] == name
        assert not details["HostConfig"]["Privileged"]
        assert details["HostConfig"]["CgroupnsMode"] == "private"
        assert details["HostConfig"]["PidMode"] != "host"
        assert details["HostConfig"]["NetworkMode"] == "none"
        assert [cap.removeprefix("CAP_") for cap in details["HostConfig"]["CapAdd"]] == ["SYS_ADMIN"]
        expected_binds = {"/source-a": str(output / "source-a"), "/source-b": str(output / "source-b"),
                          "/tools": str(tools), "/go/pkg/mod": modules}
        actual_binds = {mount["Destination"]: mount for mount in details["Mounts"] if mount["Type"] == "bind"}
        assert set(actual_binds) == set(expected_binds)
        assert all(Path(actual_binds[target]["Source"]).resolve() == Path(source).resolve()
                   and not actual_binds[target]["RW"] for target, source in expected_binds.items())
        volumes = [mount for mount in details["Mounts"] if mount["Type"] == "volume"]
        assert len(volumes) == 1 and volumes[0]["Destination"] == "/var/cache/shiki-go"
        write_json(output / "isolation.json", {"container_id": container_id, "name": name,
                   "cgroup_namespace": "private", "network": "none", "privileged": False,
                   "cap_add": ["SYS_ADMIN"], "mounts": details["Mounts"],
                   "engine": {k: engine[k] for k in ("ServerVersion", "Architecture", "CgroupVersion")}})
        wait_for(lambda: run(["docker", "exec", container_id, "systemctl", "is-system-running"], check=False).stdout.strip() == "running")
        with (output / "scenario.log").open("w") as log:
            import subprocess
            scenario = subprocess.run(["docker", "exec", container_id, "python3", "-B", "/tools/scenario.py"],
                                      stdout=log, stderr=subprocess.STDOUT, timeout=1200)
        run(["docker", "cp", container_id + ":/evidence/.", output / "evidence"])
        if scenario.returncode:
            raise RuntimeError(f"service scenario exited {scenario.returncode}; see scenario.log")
        run(["docker", "exec", container_id, "systemctl", "poweroff"])
        exited = run(["docker", "wait", container_id], timeout=45)
        state = json.loads(run(["docker", "inspect", "--format", "{{json .State}}", container_id]).stdout)
        assert exited.stdout.strip() == "0" and not state["Running"] and state["Pid"] == 0 and not state["OOMKilled"]
        cleanup["normal_shutdown"] = True
        write_json(output / "shutdown.json", state)
        passed = True
    finally:
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        # The unique label is checked even if interruption hid docker run's ID.
        found = run(["docker", "inspect", name], check=False)
        if found.returncode == 0:
            owned = json.loads(found.stdout)[0]
            if owned["Config"]["Labels"].get("dev.shikigami.schema-test") != name:
                raise RuntimeError("refusing cleanup: container ownership differs")
            container_id = owned["Id"]
            logs = run(["docker", "logs", container_id], check=False)
            (output / "systemd.log").write_text(logs.stdout + logs.stderr)
            run(["docker", "cp", container_id + ":/evidence/.", output / "final-evidence"], check=False)
            if owned["State"]["Running"]:
                cleanup["forced_removal"] = True
                run(["docker", "stop", "--time", "20", container_id], check=False, timeout=30)
            run(["docker", "rm", "-f", container_id])
        # A daemon/CLI outage is not evidence that the exact owned ID vanished.
        listed = run(["docker", "ps", "-a", "--no-trunc", "--format", "{{.ID}}"])
        cleanup["container_absent"] = container_id is None or container_id not in listed.stdout.splitlines()
        if container_id is not None:
            absent = run(["docker", "inspect", container_id], check=False)
            cleanup["container_absent"] = cleanup["container_absent"] and absent.returncode != 0 and "no such object" in absent.stderr.lower()
        cleanup["container_id"] = container_id
        cleanup["sources_unchanged"] = inventory(output / "source-a") == old["hashes"] and inventory(output / "source-b") == new["hashes"] and inventory(tools) == tool_hashes
        cleanup["scenario_passed"] = passed
        write_json(output / "cleanup.json", cleanup)
        run(["docker", "image", "rm", image], check=False)
    if not cleanup["container_absent"] or not cleanup["sources_unchanged"] or cleanup["forced_removal"]:
        raise RuntimeError("cleanup/source verification did not pass")
    print(f"PASS: {output}", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--old-source", required=True)
    parser.add_argument("--new-source", required=True)
    parser.add_argument("--output")
    arguments = parser.parse_args()

    def interrupted(signum, _frame):
        raise KeyboardInterrupt(f"signal {signum}")

    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    try:
        execute(arguments)
    except BaseException:
        traceback.print_exc()
        sys.exit(1)
