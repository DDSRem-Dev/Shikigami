#!/usr/bin/env python3
"""Real systemd acceptance, confined to one private Docker cgroup/PID namespace."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import traceback

sys.dont_write_bytecode = True
from systemd_activity import ActiveUpgradeProbe


def run(args, *, timeout=60, check=True, cwd=None, env=None):
    result = subprocess.run([str(a) for a in args], cwd=cwd, env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {args}\n{result.stdout}\n{result.stderr}")
    return result


def wait_for(check, timeout=30):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            result = check()
            if result:
                return result
        except (RuntimeError, OSError, ValueError) as error:
            last = error
        time.sleep(0.1)
    raise RuntimeError(f"condition did not settle within {timeout}s: {last}")


def phase(message):
    print(message, flush=True)


def host(output, prebuilt=None, fresh_only=False):
    if fresh_only and not prebuilt:
        raise RuntimeError("--fresh-only requires authenticated prebuilt bundles")
    repository = Path(__file__).resolve().parent.parent
    output = Path(output).resolve() if output else Path(tempfile.mkdtemp(prefix="shiki-systemd-", dir="/tmp"))
    if output == repository or repository in output.parents:
        raise RuntimeError("evidence directory must be outside the source repository")
    output.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(output, 0o700)
    info = json.loads(run(["docker", "info", "--format", "{{json .}} "]).stdout)
    if info["OSType"] != "linux" or info["CgroupVersion"] != "2":
        raise RuntimeError("this acceptance runner requires a Linux Docker engine with cgroup v2")
    phase(f"Evidence: {output}")
    name = "shiki-systemd-" + str(os.getpid())
    image = name + ":local"
    source = output / "source"
    source.mkdir()
    with tempfile.TemporaryFile() as archive:
        subprocess.run(["tar", "--exclude=./.git", "--exclude=./.tools", "--exclude=./bin", "--exclude=./dist", "--exclude=__pycache__", "-cf", "-", "."], cwd=repository, stdout=archive, check=True)
        archive.seek(0)
        subprocess.run(["tar", "-xf", "-", "-C", str(source)], stdin=archive, check=True)
    provenance = {}
    for relative in ("go.mod", "go.sum", "internal/install/service.go", "internal/install/probe.go", "internal/server/daemon.go", "internal/server/initialize.go", "internal/server/node_disabled.go", "internal/agent/runtime.go", "internal/agent/crash_guard.go", "internal/client/client.go", "internal/client/start.go", "cmd/shiki/agent.go", "scripts/test-systemd.py", "scripts/test-systemd.Dockerfile"):
        provenance[relative] = hashlib.sha256((source / relative).read_bytes()).hexdigest()
    for relative in ("scripts/systemd_activity.py", "test/e2e/upgrade_activity_test.go", "test/e2e/upgrade_helpers_test.go", "test/e2e/upgrade_network_test.go"):
        provenance[relative] = hashlib.sha256((source / relative).read_bytes()).hexdigest()
    for pattern in ("*.go", "*.proto"):
        for path in source.rglob(pattern):
            if path.is_file() and not path.is_symlink():
                provenance[str(path.relative_to(source))] = hashlib.sha256(path.read_bytes()).hexdigest()
    (output / "source-sha256.json").write_text(json.dumps(provenance, indent=2) + "\n")
    run(["go", "mod", "download"], cwd=repository, timeout=180)
    modules = run(["go", "env", "GOMODCACHE"], cwd=repository).stdout.strip()
    bundle_mount = []
    if prebuilt:
        entries = json.loads(Path(prebuilt).read_text())
        if len(entries) != 2 or entries[0]["version"] == entries[1]["version"]:
            raise RuntimeError("two distinct authenticated prebuilt versions are required")
        supplied = output / "prebuilt"
        supplied.mkdir(mode=0o755)
        copied = []
        for index, entry in enumerate(entries):
            directory = Path(entry["directory"])
            if not directory.is_absolute() or not directory.is_dir() or directory.is_symlink():
                raise RuntimeError("prebuilt directory must be an absolute regular directory")
            target = supplied / str(index)
            target.mkdir(mode=0o755)
            for path in directory.iterdir():
                if path.is_symlink() or not path.is_file():
                    raise RuntimeError("prebuilt payload must be a flat regular file")
                shutil.copyfile(path, target / path.name)
                os.chmod(target / path.name, 0o755 if path.name.startswith("shiki") and not path.name.endswith("json") else 0o644)
            if hashlib.sha256((target / "SHA256SUMS").read_bytes()).hexdigest() != entry["checksums_sha256"]:
                raise RuntimeError("prebuilt checksum pin mismatch")
            copied.append({**entry, "directory": "/release-fixtures/" + str(index)})
        (supplied / "manifest.json").write_text(json.dumps(copied))
        os.chmod(supplied / "manifest.json", 0o644)
        bundle_mount = ["--mount", f"type=bind,source={supplied},target=/release-fixtures,readonly", "--env", "SHIKI_INSTALL_TEST_BUNDLES=/release-fixtures/manifest.json"]
    if fresh_only:
        bundle_mount += ["--env", "SHIKI_INSTALL_TEST_FRESH_ONLY=1"]
    created = False
    try:
        phase("Building isolated systemd image")
        build = run(["docker", "build", "-f", source / "scripts/test-systemd.Dockerfile", "-t", image, source / "scripts"], timeout=300)
        (output / "image-build.log").write_text(build.stdout + build.stderr)
        created = True  # Cleanup also covers interruption during docker run.
        run(["docker", "run", "-dt", "--name", name, "--label", "dev.shikigami.systemd-test=" + name, "--cgroupns", "private", "--network", "none",
             "--cap-add", "SYS_ADMIN", "--tmpfs", "/run", "--tmpfs", "/run/lock", "--tmpfs", "/tmp",
             "--mount", f"type=bind,source={source},target=/source,readonly",
             "--mount", "type=volume,source=shiki-systemd-go-cache,target=/var/cache/shiki-go",
             "--mount", f"type=bind,source={modules},target=/go/pkg/mod,readonly", *bundle_mount, image,
             "/bin/bash", "-c", "set -euo pipefail; mount -o remount,rw /sys/fs/cgroup; exec /sbin/init --log-target=console --log-level=info"])
        wait_for(lambda: run(["docker", "exec", name, "systemctl", "is-system-running"], check=False).stdout.strip() == "running")
        inspect = json.loads(run(["docker", "inspect", name]).stdout)[0]
        assert inspect["HostConfig"]["CgroupnsMode"] == "private"
        assert not inspect["HostConfig"]["Privileged"]
        assert [cap.removeprefix("CAP_") for cap in inspect["HostConfig"]["CapAdd"]] == ["SYS_ADMIN"], inspect["HostConfig"]["CapAdd"]
        assert all(not mount["RW"] for mount in inspect["Mounts"] if mount["Type"] == "bind")
        assert all(mount["Destination"] == "/var/cache/shiki-go" for mount in inspect["Mounts"] if mount["Type"] == "volume")
        (output / "isolation.json").write_text(json.dumps({"container": name, "engine": {key: info[key] for key in ("ServerVersion", "KernelVersion", "Architecture", "CgroupDriver", "CgroupVersion")}, "cgroup_namespace": "private", "network": "none", "cap_add": ["SYS_ADMIN"], "privileged": False, "mounts": inspect["Mounts"]}, indent=2) + "\n")
        phase("Real systemd PID 1 ready; running service acceptance")
        with (output / "scenario.log").open("w") as log:
            result = subprocess.run(["docker", "exec", name, "python3", "/source/scripts/test-systemd.py", "--inside"], stdout=log, stderr=subprocess.STDOUT, timeout=900)
        run(["docker", "cp", name + ":/evidence/.", output], check=False)
        if result.returncode:
            raise RuntimeError(f"systemd scenario failed: {output / 'scenario.log'}")
        phase("Requesting real systemd shutdown")
        run(["docker", "exec", name, "systemctl", "poweroff"])
        result = run(["docker", "wait", name], timeout=45)
        assert result.stdout.strip() == "0", result.stdout
        state = json.loads(run(["docker", "inspect", "--format", "{{json .State}}", name]).stdout)
        assert not state["Running"] and state["Pid"] == 0 and not state["OOMKilled"]
        (output / "shutdown.json").write_text(json.dumps(state, indent=2) + "\n")
        evidence = json.loads((output / "evidence.json").read_text())
        evidence["systemd_shutdown"] = "passed: PID namespace exited with status 0"
        (output / "evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")
        phase(f"PASS: {output / 'evidence.json'}")
    finally:
        # A second terminal signal must not interrupt ownership-scoped cleanup.
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        if created:
            logs = run(["docker", "logs", name], check=False)
            (output / "systemd.log").write_text(logs.stdout + logs.stderr)
            run(["docker", "cp", name + ":/evidence/.", output / "container-evidence"], check=False)
            run(["docker", "stop", "--time", "20", name], check=False, timeout=30)
            cleanup = run(["docker", "inspect", "--format", "{{json .State}}", name], check=False)
            if cleanup.returncode == 0:
                (output / "cleanup.json").write_text(cleanup.stdout)
            run(["docker", "rm", "-f", name], check=False)
        run(["docker", "image", "rm", image], check=False)


def inside():
    assert os.geteuid() == 0 and Path("/proc/1/comm").read_text().strip() == "systemd"
    assert Path("/proc/1/cgroup").read_text().strip() == "0::/init.scope"
    evidence = Path("/evidence")
    evidence.mkdir(mode=0o700)
    home = Path("/home/shikitest")
    env = {**os.environ, "PATH": "/usr/local/go/bin:/usr/bin:/bin", "HOME": str(home), "TMPDIR": "/build/tmp", "GOCACHE": "/var/cache/shiki-go", "GOPATH": "/go", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off", "GOMAXPROCS": "2", "GOFLAGS": "-p=1 -mod=readonly -buildvcs=false"}
    os.chown("/var/cache/shiki-go", 1000, 1000)
    for directory in (Path("/work"), Path("/build")):
        directory.mkdir()
        os.chown(directory, 1000, 1000)
    Path("/build/tmp").mkdir(mode=0o700)
    os.chown("/build/tmp", 1000, 1000)
    shutil.copytree("/source", "/work", dirs_exist_ok=True)
    run(["chown", "-R", "1000:1000", "/work"])

    def user(args, **kwargs):
        return run(["/usr/sbin/runuser", "-u", "shikitest", "--", "env", "HOME=" + str(home), "USER=shikitest", "XDG_RUNTIME_DIR=/run/user/1000", "PATH=/home/shikitest/.local/bin:/usr/local/go/bin:/usr/bin:/bin", *args], env=env, **kwargs)

    prebuilt = os.environ.get("SHIKI_INSTALL_TEST_BUNDLES")
    if prebuilt:
        entries = json.loads(Path(prebuilt).read_text())
        versions = [entry["version"] for entry in entries]
        bundles = {entry["version"]: (Path(entry["directory"]), entry["checksums_sha256"]) for entry in entries}
        phase("Using supplied authenticated release payloads without rebuilding product binaries: " + ", ".join(versions))
    else:
        versions = ["systemd-test-a", "systemd-test-b"]
        bundles = {}
        for version in versions:
            phase("Building isolated release " + version)
            user(["env", "SHIKI_VERSION=" + version, "bash", "scripts/build.sh"], cwd="/work", timeout=300)
            result = user(["go", "run", "scripts/release.go", "--dir", "bin", "--output", "/build/" + version, "--version", version], cwd="/work", timeout=120)
            (evidence / (version + "-build.log")).write_text(result.stdout + result.stderr)
            archive = next(Path("/build", version).glob("*.tar.gz"))
            bundle = Path("/build", version, "bundle")
            bundle.mkdir(mode=0o755)
            with tarfile.open(archive) as packed:
                for member in packed.getmembers():
                    if not member.isfile() or Path(member.name).name != member.name:
                        raise RuntimeError("local release archive contains a non-flat regular payload")
                    with packed.extractfile(member) as source, (bundle / member.name).open("wb") as target:
                        shutil.copyfileobj(source, target)
            # Real clients and the ordinary-user init command must execute the
            # separately verified fixture bundle before installed links exist.
            for file in bundle.iterdir():
                os.chmod(file, 0o755 if file.name.startswith("shiki") and not file.name.endswith("json") else 0o644)
            bundles[version] = (bundle, Path(str(archive) + ".checksums-pin").read_text().strip())
    a, b = versions
    fixture = home / "claude-systemd-test-fixture"
    fixture.write_text("#!/bin/sh\nif [ \"${1:-}\" = --version ]; then printf '0.0.0-systemd-fixture\\n'; exit 0; fi\nif [ \"${1:-} ${2:-} ${3:-}\" = 'auth status --json' ]; then printf '{\"loggedIn\":true}'; exit 0; fi\nprintf 'SYSTEMD_FIXTURE_READY\\n'\nwhile IFS= read -r line; do printf '%s\\n' \"$line\" >> \"$HOME/upgrade-terminal-inputs\"; printf 'REPLY:%s\\n' \"$line\"; done\n")
    os.chmod(fixture, 0o700)
    os.chown(fixture, 1000, 1000)
    tls = home / "tls"
    user(["mkdir", "-m", "700", tls])
    user(["openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes", "-keyout", tls / "key.pem", "-out", tls / "cert.pem", "-days", "1", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1"])
    user(["chmod", "600", tls / "key.pem", tls / "cert.pem"])
    for directory in (Path("/etc/shiki"), Path("/var/lib/shiki")):
        directory.mkdir(mode=0o700)
        os.chown(directory, 1000, 1000)
    daemon_config = Path("/etc/shiki/daemon.yaml")
    user([bundles[a][0] / "shiki-daemon", "init", "--config", daemon_config, "--user", "shikitest", "--state-dir", "/var/lib/shiki", "--projects-dir", home / "projects", "--tailscale=false", "--direct-tls-address", "127.0.0.1:19527", "--tls-cert", tls / "cert.pem", "--tls-key", tls / "key.pem", "--claude-executable", fixture])
    token = home / "device.token"
    daemon_args = ["--role", "daemon", "--user", "shikitest", "--prefix", "/opt/shiki", "--non-interactive"]
    daemon_install_args = [*daemon_args, "--config", daemon_config, "--state-dir", "/var/lib/shiki", "--plugins-dir", "/var/lib/shiki/plugins", "--bin-dir", "/opt/shiki-commands", "--projects-dir", home / "projects", "--claude-executable", fixture, "--path", "/home/shikitest/.local/bin:/usr/bin:/bin"]

    def readiness(version, token_destination=None):
        def attempt():
            args = ["/opt/shiki/current/shiki-install", "probe", *daemon_args]
            if token_destination is not None:
                args += ["--token-output", token_destination]
            result = run(args, timeout=60, check=False)
            if result.returncode not in (0, 3):
                raise RuntimeError(result.stderr)
            report = json.loads(result.stdout)
            assert report["version"] == version, report
            return report if report["state"] == "local_application_ready" else None
        return wait_for(attempt)

    def install(version, initial=False, token_destination=token, expected_state="local_application_ready"):
        bundle, pin = bundles[version]
        args = [bundle / "shiki-install", "install", *daemon_install_args, "--version", version, "--bundle", bundle, "--checksums-sha256", pin]
        if initial:
            args += ["--token-output", token_destination]
        result = run(args, timeout=90, check=False)
        if result.returncode not in (0, 3):
            raise RuntimeError(result.stderr)
        data = json.loads(result.stdout)
        if result.returncode == 3:
            data = readiness(version, token_destination if initial else None)
        assert data["state"] == expected_state, data
        if expected_state == "local_application_ready":
            assert data["external_peer_acceptance"] == "pending"
        return data

    phase("Activating rendered daemon unit through real installer")
    initial = install(a, True)
    assert initial["token_issued"] and token.stat().st_mode & 0o777 == 0o600
    config_before = daemon_config.read_bytes()
    unit = run(["systemctl", "cat", "shiki-daemon.service"]).stdout
    (evidence / "daemon-unit.txt").write_text(unit)
    assert "KillMode=process" in unit and "User=shikitest" in unit
    run(["loginctl", "enable-linger", "shikitest"])
    run(["systemctl", "start", "user@1000.service"])
    wait_for(lambda: Path("/run/user/1000/bus").exists())
    client_config = home / ".shiki/config.yaml"
    user(["mkdir", "-p", "-m", "700", client_config.parent])
    client_config.write_text("schema_version: 1\nplugins_dir: ~/.shiki/plugins\nagent:\n  socket: ~/.shiki/run/agent.sock\n  state_dir: ~/.shiki/agent\n  auto_start: false\ntailscale:\n  enabled: false\n  hostname: shiki-systemd-test\n  state_dir: ~/.shiki/tailscale\n  control_url: https://controlplane.tailscale.com\nservers: {}\nprojects: {}\n")
    os.chmod(client_config, 0o600)
    os.chown(client_config, 1000, 1000)
    client_prefix = home / ".local/share/shiki/install"

    def client_operation(operation, version, expected_version=None, expected_state="local_application_ready"):
        bundle, pin = bundles[version]
        args = [bundle / "shiki-install", operation, "--role", "client", "--non-interactive"]
        if operation == "install":
            args += ["--version", version, "--bundle", bundle, "--checksums-sha256", pin]
        result = user(args, timeout=90, check=False)
        if result.returncode not in (0, 3):
            raise RuntimeError(result.stderr)
        report = json.loads(result.stdout)
        if result.returncode == 3:
            def ready():
                response = user([client_prefix / "current/shiki-install", "probe", "--role", "client", "--non-interactive"], timeout=60, check=False)
                if response.returncode not in (0, 3):
                    raise RuntimeError(response.stderr)
                data = json.loads(response.stdout)
                return data if data["state"] == "local_application_ready" else None
            report = wait_for(ready)
        if operation != "uninstall":
            assert report["state"] == expected_state and report["version"] == (expected_version or version), report
        return report

    client_operation("install", a)
    cli_binary = home / ".local/bin/shiki"

    def cli(*args):
        return json.loads(user([cli_binary, "--config", client_config, "--json", "--timeout", "20s", *args], timeout=40).stdout)

    def ctl(*args):
        return run(["systemctl", *args])

    def agent_ctl(*args):
        return user(["systemctl", "--user", *args])

    def main_pid(unit="shiki-daemon.service", agent=False):
        result = (agent_ctl if agent else ctl)("show", unit, "--property=MainPID", "--value")
        return int(result.stdout.strip())

    def process(pid):
        status = Path(f"/proc/{pid}/status").read_text()
        assert "Uid:\t1000\t1000\t1000\t1000" in status, status
        environment = dict(line.split("=", 1) for line in Path(f"/proc/{pid}/environ").read_bytes().decode().split("\0") if "=" in line)
        assert environment["HOME"] == str(home)
        return {"pid": pid, "uid": 1000, "home": environment["HOME"], "path": environment["PATH"], "cwd": user(["readlink", f"/proc/{pid}/cwd"]).stdout.strip(), "cgroup": Path(f"/proc/{pid}/cgroup").read_text().strip()}

    def workers(parent):
        found = []
        for entry in Path("/proc").iterdir():
            if not entry.name.isdigit():
                continue
            try:
                command = (entry / "cmdline").read_bytes().split(b"\0")[0]
                fields = (entry / "stat").read_text().split(")", 1)[1].split()
                if command.endswith((b"shiki-plugin-sync", b"shiki-plugin-port")) and int(fields[1]) == parent:
                    found.append(int(entry.name))
            except FileNotFoundError:
                pass
        return found

    def gone(pids):
        return all(not Path(f"/proc/{pid}").exists() for pid in pids)

    cli("connect", "systemd", "--tls-address", "127.0.0.1:19527", "--tls-server-name", "localhost", "--tls-ca", str(tls / "cert.pem"), "--token-file", str(token))
    user(["mkdir", "-p", "-m", "700", home / "projects/preserved"])
    marker = home / "projects/preserved/keep.txt"
    marker.write_text("preserved across systemd lifecycle\n")
    os.chown(marker, 1000, 1000)
    project = cli("projects", "create", "systemd", "--path", str(marker.parent))
    session = cli("sessions", "create", "durable", "--project", project["id"])
    session_id = session["id"]
    tmux_args = ["tmux", "-L", "shiki-1000", "display-message", "-p", "-t", "shiki-" + session_id, "#{pane_pid}"]
    pane = int(user(tmux_args).stdout.strip())
    cli("--server", "systemd", "plugins", "enable", "sync", "--side", "remote")
    cli("plugins", "enable", "sync", "--side", "local")
    daemon_pid, agent_pid = main_pid(), main_pid("shiki-agent.service", True)
    remote_worker = wait_for(lambda: workers(daemon_pid))
    local_worker = wait_for(lambda: workers(agent_pid))
    initial_processes = {"daemon": process(daemon_pid), "agent": process(agent_pid), "pane": process(pane), "remote_worker": [process(pid) for pid in remote_worker], "local_worker": [process(pid) for pid in local_worker]}
    assert initial_processes["daemon"]["cwd"] == str(home) and initial_processes["agent"]["cwd"] == str(home)
    assert "/system.slice/shiki-daemon.service" in initial_processes["daemon"]["cgroup"]
    assert "/user.slice/" in initial_processes["agent"]["cgroup"]
    records = []

    def preserved(label, expected_version):
        def ready():
            status = cli("sessions", "info", session_id)
            return status if int(status.get("claudePid", status.get("claude_pid", 0))) == pane else None
        wait_for(ready)
        assert int(user(tmux_args).stdout.strip()) == pane
        assert marker.read_text() == "preserved across systemd lifecycle\n"
        report = readiness(expected_version)
        assert report["state"] == "local_application_ready" and report["version"] == expected_version, report
        assert report["daemon_id"] == initial["daemon_id"] and not report.get("token_issued", False)
        worker = wait_for(lambda: workers(main_pid()))
        records.append({"operation": label, "daemon_pid": main_pid(), "agent_pid": main_pid("shiki-agent.service", True), "pane_pid": pane, "worker_pids": worker, "version": expected_version, "same_daemon_id_and_original_token": True})

    if os.environ.get("SHIKI_INSTALL_TEST_FRESH_ONLY") == "1":
        phase("Checking fresh candidate daemon/client, all four plugins, repeat installation and uninstall")
        cli("--server", "systemd", "plugins", "enable", "port", "--side", "remote")
        cli("plugins", "enable", "port", "--side", "local")
        wait_for(lambda: len(workers(daemon_pid)) == 2 and len(workers(agent_pid)) == 2)
        preserved("fresh candidate application and authenticated session", a)
        token_before = token.read_bytes()
        client_config_before = client_config.read_bytes()
        client_operation("install", a, expected_state="unchanged")
        install(a, expected_state="unchanged")
        assert token.read_bytes() == token_before
        preserved("same candidate repeat install preserves identity and token", a)
        old_agent, old_daemon = main_pid("shiki-agent.service", True), main_pid()
        old_workers = workers(old_agent) + workers(old_daemon)
        client_operation("uninstall", a)
        removed = run([bundles[a][0] / "shiki-install", "uninstall", *daemon_args], timeout=60)
        assert json.loads(removed.stdout)["state"] == "uninstalled"
        wait_for(lambda: gone([old_agent, old_daemon, *old_workers]))
        assert not Path("/etc/systemd/system/shiki-daemon.service").exists()
        assert not (home / ".config/systemd/user/shiki-agent.service").exists()
        assert daemon_config.read_bytes() == config_before and client_config.read_bytes() == client_config_before
        assert token.read_bytes() == token_before and marker.read_text() == "preserved across systemd lifecycle\n"
        assert int(user(tmux_args).stdout.strip()) == pane
        result = {"mode": "fresh-candidate-only", "version": a,
                  "systemd": run(["systemctl", "--version"]).stdout.splitlines()[0],
                  "platform": run(["uname", "-m"]).stdout.strip(), "pid1": "systemd",
                  "product_rebuilt": False, "initial_processes": initial_processes,
                  "operations": records, "all_four_plugins_ready": True,
                  "uninstall_preserves_config_state_project_token_tmux": True,
                  "all_host_and_worker_processes_reaped": True,
                  "release_trust": "operator-supplied authenticated bundles; see host provenance",
                  "transport": "loopback TLS only; runtime network disabled",
                  "claude": "explicit test fixture, no account acceptance",
                  "external_peer_acceptance": "pending"}
        (evidence / "evidence.json").write_text(json.dumps(result, indent=2) + "\n")
        phase("Fresh candidate install/run/repeat/uninstall PASS; tmux retained for systemd shutdown")
        return

    phase("Checking actual stop/restart, SIGKILL recovery and worker reap")
    ctl("stop", "shiki-daemon.service")
    wait_for(lambda: gone([daemon_pid, *remote_worker]))
    assert int(user(tmux_args).stdout.strip()) == pane
    ctl("start", "shiki-daemon.service")
    preserved("stop/start", a)
    old_pid, old_workers = main_pid(), workers(main_pid())
    ctl("restart", "shiki-daemon.service")
    wait_for(lambda: gone([old_pid, *old_workers]))
    preserved("systemctl restart", a)
    old_pid, old_workers = main_pid(), workers(main_pid())
    ctl("kill", "--kill-whom=main", "--signal=SIGKILL", "shiki-daemon.service")
    wait_for(lambda: gone([old_pid, *old_workers]))
    wait_for(lambda: main_pid() not in (0, old_pid), timeout=20)
    preserved("SIGKILL automatic Restart=on-failure", a)
    agent_ctl("stop", "shiki-agent.service")
    wait_for(lambda: gone([agent_pid, *local_worker]))
    agent_ctl("start", "shiki-agent.service")
    wait_for(lambda: cli("agent", "status").get("state") == "ready")
    preserved("user agent stop/start", a)
    old_pid = main_pid("shiki-agent.service", True)
    old_workers = wait_for(lambda: workers(old_pid))
    agent_ctl("kill", "--kill-whom=main", "--signal=SIGKILL", "shiki-agent.service")
    wait_for(lambda: gone([old_pid, *old_workers]))
    wait_for(lambda: main_pid("shiki-agent.service", True) not in (0, old_pid), timeout=20)
    preserved("user agent SIGKILL automatic restart", a)
    phase("Checking three-crash guard, explicit recovery and return to service ownership")
    for crash in (2, 3):
        old_pid = main_pid("shiki-agent.service", True)
        old_workers = wait_for(lambda: workers(old_pid))
        agent_ctl("kill", "--kill-whom=main", "--signal=SIGKILL", "shiki-agent.service")
        wait_for(lambda: gone([old_pid, *old_workers]))
        wait_for(lambda: main_pid("shiki-agent.service", True) not in (0, old_pid), timeout=20)

        def crash_state():
            status = cli("agent", "status")
            expected = "ready" if crash == 2 else "degraded"
            reason = status.get("reasonCode", status.get("reason_code", ""))
            return status if status.get("state") == expected and reason == ("" if crash == 2 else "AGENT_CRASH_LOOP") else None

        wait_for(crash_state)
    blocked_pid = main_pid("shiki-agent.service", True)
    stable_until = time.monotonic() + 15
    while time.monotonic() < stable_until:
        assert crash_state(), "diagnostic state changed without recovery"
        assert main_pid("shiki-agent.service", True) == blocked_pid and not workers(blocked_pid)
        time.sleep(0.25)
    ordinary = user([cli_binary, "--config", client_config, "--json", "agent", "start"], check=False)
    assert ordinary.returncode != 0 and "AGENT_CRASH_LOOP" in ordinary.stdout + ordinary.stderr
    assert main_pid("shiki-agent.service", True) == blocked_pid and crash_state()
    recovered = cli("agent", "start", "--recover-crash-loop")
    assert recovered.get("state") == "ready", recovered
    wait_for(lambda: gone([blocked_pid]))
    wait_for(lambda: main_pid("shiki-agent.service", True) == 0)
    assert agent_ctl("show", "shiki-agent.service", "--property=ExecMainStatus", "--value").stdout.strip() == "0"
    manual_pid = int((home / ".shiki/agent/instance.lock").read_text().strip())
    manual_workers = wait_for(lambda: workers(manual_pid))
    assert manual_pid != blocked_pid
    cli("agent", "stop")
    wait_for(lambda: gone([manual_pid, *manual_workers]))
    # Recovery used a CLI-owned host. Rejoin the original user-service cgroup
    # before testing successful managed stop and subsequent upgrade/rollback.
    agent_ctl("start", "shiki-agent.service")
    wait_for(lambda: cli("agent", "status").get("state") == "ready")
    managed_pid = main_pid("shiki-agent.service", True)
    managed_workers = wait_for(lambda: workers(managed_pid))
    assert "/shiki-agent.service" in process(managed_pid)["cgroup"]
    preserved("user agent three-crash explicit recovery and service handoff", a)
    cli("agent", "stop")
    wait_for(lambda: gone([managed_pid, *managed_workers]))
    stable_until = time.monotonic() + 15
    while time.monotonic() < stable_until:
        assert main_pid("shiki-agent.service", True) == 0
        assert agent_ctl("show", "shiki-agent.service", "--property=ExecMainStatus", "--value").stdout.strip() == "0"
        time.sleep(0.25)
    agent_ctl("start", "shiki-agent.service")
    wait_for(lambda: cli("agent", "status").get("state") == "ready")
    preserved("user agent normal stop exits zero without service respawn", a)
    crash_evidence = {"unexpected_exits_same_version": 3, "diagnostic_state": "degraded", "reason": "AGENT_CRASH_LOOP", "stable_diagnostic_pid": blocked_pid, "stable_seconds": 15, "diagnostic_workers": [], "ordinary_start_refused": True, "explicit_recovery_ready": True, "recovery_cli_pid": manual_pid, "returned_to_user_service_pid": managed_pid, "normal_stop_exit": 0, "normal_stop_no_respawn_seconds": 15}
    phase("Starting ordinary-user active transfer probe")
    activity = ActiveUpgradeProbe(user, env, home=home, evidence=evidence, cli=cli_binary, client_config=client_config, project=project["id"], session=session_id, remote_root=marker.parent, ledger=home / "upgrade-terminal-inputs")
    phase("Checking user-service upgrade, rollback, uninstall and preserved credentials")
    client_before = client_config.read_bytes()
    for operation, version, expected in (("install", b, b), ("rollback", b, a)):
        old_pid = main_pid("shiki-agent.service", True)
        old_workers = wait_for(lambda: workers(old_pid))
        label = "agent-upgrade" if operation == "install" else "agent-rollback"
        activity.before(label)
        assert main_pid("shiki-agent.service", True) == old_pid
        old_workers = wait_for(lambda: workers(old_pid))
        client_operation(operation, version, expected)
        wait_for(lambda: gone([old_pid, *old_workers]))
        activity.after(label, [old_pid, *old_workers])
        assert client_config.read_bytes() == client_before
        preserved("user agent " + ("upgrade" if operation == "install" else "rollback"), a)
    old_pid = main_pid("shiki-agent.service", True)
    old_workers = wait_for(lambda: workers(old_pid))
    assert client_operation("uninstall", a)["state"] == "uninstalled"
    wait_for(lambda: gone([old_pid, *old_workers]))
    assert not (home / ".config/systemd/user/shiki-agent.service").exists()
    assert not cli_binary.is_symlink() and not (client_prefix / "current").exists()
    assert client_config.read_bytes() == client_before and (home / ".shiki/agent").is_dir()
    assert any((client_prefix / "backups").iterdir()) and any((client_prefix / "releases").iterdir())
    client_operation("install", a)
    preserved("user agent reinstall preserves original credential", a)
    phase("Checking service-managed upgrade, rollback and preserved identity")
    old_pid = main_pid()
    old_workers = wait_for(lambda: workers(old_pid))
    activity.before("daemon-upgrade")
    assert main_pid() == old_pid
    old_workers = wait_for(lambda: workers(old_pid))
    install(b)
    wait_for(lambda: gone([old_pid, *old_workers]))
    activity.after("daemon-upgrade", [old_pid, *old_workers])
    preserved("upgrade", b)
    old_pid = main_pid()
    old_workers = wait_for(lambda: workers(old_pid))
    activity.before("daemon-rollback")
    assert main_pid() == old_pid
    old_workers = wait_for(lambda: workers(old_pid))
    result = run([bundles[b][0] / "shiki-install", "rollback", *daemon_args], timeout=90, check=False)
    if result.returncode not in (0, 3):
        raise RuntimeError(result.stderr)
    rollback = json.loads(result.stdout) if result.returncode == 0 else readiness(a)
    assert rollback["state"] == "local_application_ready", rollback
    wait_for(lambda: gone([old_pid, *old_workers]))
    activity.after("daemon-rollback", [old_pid, *old_workers])
    active_upgrade_evidence = activity.finish()
    preserved("rollback", a)
    assert daemon_config.read_bytes() == config_before
    tokens = json.loads(user(["/opt/shiki/current/shiki-daemon", "token", "list", "--config", daemon_config]).stdout)
    assert len(tokens) == 1
    phase("Checking uninstall preserves data, credentials and tmux")
    old_pid, old_workers = main_pid(), workers(main_pid())
    result = json.loads(run([bundles[a][0] / "shiki-install", "uninstall", *daemon_args], timeout=90).stdout)
    assert result["state"] == "uninstalled"
    wait_for(lambda: gone([old_pid, *old_workers]))
    assert not Path("/etc/systemd/system/shiki-daemon.service").exists()
    assert not Path("/opt/shiki/current").exists()
    assert daemon_config.read_bytes() == config_before and token.exists() and Path("/var/lib/shiki").is_dir()
    assert marker.read_text() == "preserved across systemd lifecycle\n" and int(user(tmux_args).stdout.strip()) == pane
    tokens = json.loads(user([bundles[a][0] / "shiki-daemon", "token", "list", "--config", daemon_config]).stdout)
    assert len(tokens) == 1
    # Reinstallation is a fresh activation and intentionally issues one new
    # Token. Keep using the original credential to prove it remains valid.
    restored = install(a, True, home / "reinstalled-device.token")
    assert restored["token_issued"] and restored["daemon_id"] == initial["daemon_id"]
    preserved("reinstall after uninstall", a)
    (evidence / "agent-unit.txt").write_text(agent_ctl("cat", "shiki-agent.service").stdout)
    (evidence / "journal.txt").write_text(ctl("status", "shiki-daemon.service", "--no-pager").stdout + run(["journalctl", "-u", "shiki-daemon.service", "--no-pager"]).stdout)
    data = {"systemd": run(["systemctl", "--version"]).stdout.splitlines()[0], "go": run(["/usr/local/go/bin/go", "version"]).stdout.strip(), "platform": run(["uname", "-m"]).stdout.strip(), "pid1": "systemd", "initial_processes": initial_processes, "operations": records, "uninstall_preserves_config_state_project_token_tmux": True, "user_agent_upgrade_rollback_uninstall_preserves_config_state_credentials": True, "token_count_before_reinstall": 1, "reinstall_issued_one_explicit_new_token": True, "transport": "loopback TLS only; runtime network disabled", "claude": "explicit test fixture, no account acceptance", "release_trust": "operator-supplied authenticated bundles; see host provenance" if prebuilt else "local build digests only; not publisher signatures", "external_peer_acceptance": "pending"}
    data["user_agent_crash_loop"] = crash_evidence
    data["active_upgrade_transfers"] = active_upgrade_evidence
    (evidence / "evidence.json").write_text(json.dumps(data, indent=2) + "\n")
    phase("Service lifecycle PASS; active daemon/agent/tmux/workers remain for systemd shutdown")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", help="private host evidence directory (default: new /tmp directory)")
    parser.add_argument("--bundles", help="JSON manifest of two previously authenticated extracted bundles; do not rebuild product binaries")
    parser.add_argument("--fresh-only", action="store_true", help="with --bundles, install/run/repeat/uninstall only the first supplied candidate in a fresh container")
    parser.add_argument("--inside", action="store_true", help=argparse.SUPPRESS)
    arguments = parser.parse_args()
    interrupted_signal = signal.SIGINT

    def interrupted(signum, _frame):
        global interrupted_signal
        interrupted_signal = signum
        raise KeyboardInterrupt

    if not arguments.inside:
        signal.signal(signal.SIGINT, interrupted)
        signal.signal(signal.SIGTERM, interrupted)
    try:
        if arguments.inside:
            inside()
        else:
            host(arguments.output, arguments.bundles, arguments.fresh_only)
    except KeyboardInterrupt:
        print("systemd acceptance interrupted; owned container cleanup completed", file=sys.stderr)
        sys.exit(128 + interrupted_signal)
    except Exception as error:
        print(f"systemd acceptance: {error}", file=sys.stderr)
        traceback.print_exc()
        if arguments.inside:
            destination = Path("/evidence")
            destination.mkdir(exist_ok=True)
            diagnostic = run(["journalctl", "-u", "shiki-daemon.service", "--no-pager"], check=False)
            (destination / "failure-journal.txt").write_text(diagnostic.stdout + diagnostic.stderr)
            diagnostic = run(["journalctl", "_UID=1000", "--no-pager"], check=False)
            (destination / "failure-agent-journal.txt").write_text(diagnostic.stdout + diagnostic.stderr)
            diagnostic = run(["ps", "-eo", "pid,ppid,uid,stat,comm"], check=False)
            (destination / "failure-processes.txt").write_text(diagnostic.stdout + diagnostic.stderr)
            control = Path("/home/shikitest/upgrade-control")
            for pattern in ("*.ready.json", "*.verified.json"):
                for path in control.glob(pattern):
                    shutil.copy2(path, destination / path.name)
        sys.exit(1)
