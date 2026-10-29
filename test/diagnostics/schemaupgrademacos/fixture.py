"""Real-HOME native LaunchAgent and private ordinary-user daemon fixture."""
import json
import os
from pathlib import Path
import plistlib
import pwd
import re
import shutil
import socket
import subprocess
import tarfile

from common import digest, gone, initialize, inventory, process_identity, replace_json, run, wait_for, write_json


def preflight():
    account = pwd.getpwuid(os.getuid())
    if os.uname().sysname != "Darwin" or os.getuid() == 0 or os.environ.get("HOME") != account.pw_dir:
        raise RuntimeError("real ordinary macOS account/HOME required")
    domain = f"gui/{os.getuid()}"
    run(["/bin/launchctl", "print", domain])
    plist = Path(account.pw_dir) / "Library/LaunchAgents/dev.shikigami.agent.plist"
    for path in (plist, plist.with_name("." + plist.name + ".install")):
        if path.exists() or path.is_symlink():
            raise RuntimeError("existing standard plist/staging file must not be replaced")
    job = run(["/bin/launchctl", "print", domain + "/dev.shikigami.agent"], check=False)
    if job.returncode == 0 or "Could not find service" not in job.stderr + job.stdout:
        raise RuntimeError("cannot establish exact standard job absence")
    if not shutil.which("tmux") or not shutil.which("openssl"):
        raise RuntimeError("native tmux and openssl required")
    return {"uid": os.getuid(), "real_home": account.pw_dir, "gui_domain": domain,
            "standard_job_plist_staging_absent": True}


class Fixture:
    def __init__(self, root, evidence):
        self.root, self.evidence = Path(root), Path(evidence)
        self.runtime = self.root / "runtime"
        self.runtime.mkdir(mode=0o700)
        self.home = self.runtime  # Fixture files only; HOME is never overridden.
        self.account = pwd.getpwuid(os.getuid())
        self.domain = f"gui/{os.getuid()}"
        self.target = self.domain + "/dev.shikigami.agent"
        self.plist = Path(self.account.pw_dir) / "Library/LaunchAgents/dev.shikigami.agent.plist"
        self.config = self.runtime / "client.yaml"
        self.state = self.runtime / "state"
        self.prefix = self.runtime / "install"
        self.sync = self.state / "plugin-state/sync"
        self.ledger = self.sync / "ledger/state.json"
        self.daemon_config = self.runtime / "daemon.yaml"
        self.token = self.runtime / "original-device.token"
        self.cli_path = self.prefix / "current/shiki"
        self.a, self.b = "schema-native-a", "schema-native-b"
        toolchain = Path(run(["go", "env", "GOROOT"]).stdout.strip()) / "bin"
        assert (toolchain / "go").is_file()
        self.env = {**os.environ, "PATH": str(toolchain) + os.pathsep + os.environ["PATH"], "GOTOOLCHAIN": "local", "GOMAXPROCS": "2", "GOFLAGS": "-p=1 -mod=readonly -buildvcs=false",
                    "GOPROXY": "off", "GOSUMDB": "off", "PYTHONDONTWRITEBYTECODE": "1"}
        self.bundles, self.records, self.processes, self.sessions = {}, [], [], []
        self.daemon_child = None
        self.install_attempted = False
        self.auth_identity, self.auth_probe_phases = None, []

    def user(self, args, **kwargs):
        return run(args, env=self.env, **kwargs)

    def write(self, path, contents):
        path = Path(path)
        path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        with path.open("wb") as output:
            os.chmod(path, 0o600)
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())

    def persist_owned(self):
        value = {"processes": self.processes, "sessions": self.sessions}
        replace_json(self.runtime / "owned.json", value)
        replace_json(self.evidence / "owned-processes.json", value)

    def record_process(self, pid, role):
        record = process_identity(pid)
        record["role"] = role
        self.processes.append(record)
        self.persist_owned()
        return record

    def record_session(self, session_id):
        # Persist the immutable ID and private start command before further
        # pane inspection can fail. Cleanup never targets a shared tmux server.
        record = {"session_id": session_id, "fixture": str(self.runtime / "terminal-fixture")}
        self.sessions.append(record)
        self.persist_owned()
        fields = self.user(["tmux", "-L", f"shiki-{os.getuid()}", "list-panes", "-t", "=shiki-" + session_id,
                            "-F", "#{session_name}\t#{session_id}\t#{pane_id}\t#{pane_pid}\t#{pane_start_command}"]).stdout.splitlines()
        assert len(fields) == 1
        parts = fields[0].split("\t", 4)
        assert len(parts) == 5 and parts[0] == "shiki-" + session_id and record["fixture"] in parts[4]
        record.update(tmux_id=parts[1], pane_id=parts[2], pid=int(parts[3]))
        self.persist_owned()
        # macOS /bin/sh dispatches to /bin/bash after exec; establish the
        # explicit interpreter identity only once fixture startup has finished.
        wait_for(lambda: process_identity(record["pid"])["executable"] == "/bin/bash")
        self.pane_identity = self.record_process(record["pid"], "pane")
        write_json(self.evidence / "pane-baseline.json", self.pane_identity)
        return record["pid"]

    def build(self):
        for tag, version in (("a", self.a), ("b", self.b)):
            source = self.evidence / ("source-" + tag)
            original = inventory(source)
            work = self.root / ("work-" + tag)
            shutil.copytree(source, work)
            assert inventory(work) == original
            print("Building native frozen source " + version, flush=True)
            build = self.user(["env", "SHIKI_VERSION=" + version, "bash", "scripts/build.sh"], cwd=work, timeout=600)
            release_dir = self.runtime / ("release-" + tag)
            release = self.user(["go", "run", "scripts/release.go", "--dir", "bin", "--output", release_dir, "--version", version], cwd=work, timeout=180)
            (self.evidence / (version + "-build.log")).write_text(build.stdout + build.stderr + release.stdout + release.stderr)
            archive = next(release_dir.glob("*.tar.gz"))
            bundle = self.runtime / ("bundle-" + tag)
            bundle.mkdir(mode=0o700)
            with tarfile.open(archive) as packed:
                for entry in packed:
                    if not entry.isfile() or Path(entry.name).name != entry.name or entry.mode & ~0o777:
                        raise RuntimeError("unsafe release payload")
                    with packed.extractfile(entry) as src, (bundle / entry.name).open("xb") as dst:
                        shutil.copyfileobj(src, dst)
                    os.chmod(bundle / entry.name, entry.mode)
            pin = Path(str(archive) + ".checksums-pin").read_text().strip()
            self.bundles[version] = (bundle, pin)
            assert all(digest(work / name) == value for name, value in original.items())
            binaries = ("shiki", "shiki-agent", "shiki-daemon", "shiki-install", "shiki-plugin-sync", "shiki-plugin-port")
            write_json(self.evidence / (version + "-artifacts.json"), {"source_files": len(original), "version": version,
                       "archive_sha256": digest(archive), "files": {p.name: digest(p) for p in bundle.iterdir()},
                       "go": self.user(["go", "version"]).stdout.strip(), "binary_build_info": {
                           name: self.user(["go", "version", "-m", bundle / name]).stdout for name in binaries}})
            if tag == "b":
                extra = work / "test/diagnostics/schemaupgrademacos"
                assert not extra.exists(), "native tool must not overwrite any frozen input"
                extra.mkdir()
                shutil.copy2(Path(__file__).parent / "probe.go", extra / "probe.go")
                self.user(["go", "build", "-trimpath", "-tags=schemamacos", "-o", self.root / "probe", "./test/diagnostics/schemaupgrademacos"], cwd=work, timeout=180)
                self.user(["go", "build", "-trimpath", "-o", self.root / "processguard", "./test/fixtures/processguard"], cwd=work, timeout=180)
                assert all(digest(work / name) == value for name, value in original.items())
                write_json(self.evidence / "native-tools-build.json", {"source_b_original_inputs_unchanged": len(original),
                           "extra_native_probe_source_sha256": digest(extra / "probe.go"),
                           "probe_sha256": digest(self.root / "probe"), "processguard_sha256": digest(self.root / "processguard"),
                           "goflags": self.env["GOFLAGS"], "gomaxprocs": "2", "shipped": False})
        initialize(self.root, self.root / "probe")

    def auth_probe(self, phase, project, *, revoked):
        result = self.user([self.root / "probe", "--root", self.root, "--project", project,
                            "--expect-revoked=" + str(revoked).lower()], check=False, timeout=40)
        # Never include unchecked helper output in an exception/evidence file.
        # Fixed private paths keep both secret values and registry data out of
        # argv; this final guard also catches an accidental future log leak.
        output = result.stdout + result.stderr
        for path in (self.token, self.home / "must-stay-revoked.token"):
            if path.read_text().strip() in output:
                raise RuntimeError("AUTH_PROBE_SECRET_OUTPUT_REFUSED")
        try:
            receipt = json.loads(result.stdout)
        except (ValueError, TypeError):
            raise RuntimeError("AUTH_PROBE_INVALID_RECEIPT") from None
        assert isinstance(receipt, dict)
        assert set(receipt).issubset({"origin", "project_id", "uid", "expected_revoked", "credentials",
                                    "passed", "preparation_reason"})
        receipt["phase"] = phase
        receipt["helper_exit"] = result.returncode
        receipt["stderr_empty"] = not result.stderr
        write_json(self.evidence / ("auth-rpc-" + phase + ".json"), receipt)
        assert result.returncode == 0 and not result.stderr and receipt["passed"], "AUTH_PROBE_FAILED"
        assert receipt["origin"] == "https://" + (self.runtime / "address").read_text() and receipt["uid"] == os.getuid()
        assert receipt["project_id"] == project and receipt["expected_revoked"] is revoked
        credentials = receipt["credentials"]
        assert [item["role"] for item in credentials] == ["valid", "candidate"]
        certificate_hashes = set()
        for credential in credentials:
            assert credential["passed"] and len(credential["calls"]) == 3
            assert [call["method"] for call in credential["calls"]] == ["NegotiateProtocol", "GetSystemInfo", "GetProject"]
            denied = revoked and credential["role"] == "candidate"
            for call in credential["calls"]:
                assert call["matched_expected_outcome"]
                assert call["code"] == ("unauthenticated" if denied else "ok")
                assert call["remote_error"] is denied
                assert len(call["responses"]) == 1
                response = call["responses"][0]
                assert response["http2"] and response["tls_verified"]
                assert response["http_status"] == (401 if denied else 200)
                certificate_hashes.add(response["peer_certificate_sha256"])
        assert len(certificate_hashes) == 1
        identity = {"daemon_id": credentials[0]["daemon_id"], "certificate_sha256": certificate_hashes.pop()}
        assert identity["daemon_id"] == self.daemon["daemon_id"]
        if self.auth_identity is None:
            self.auth_identity = identity
        assert identity == self.auth_identity, "AUTH_PROBE_TARGET_CHANGED"
        if not revoked:
            assert credentials[1]["daemon_id"] == identity["daemon_id"]
        self.auth_probe_phases.append(phase)

    def cli(self, *args, check=True):
        result = self.user([self.cli_path, "--config", self.config, "--json", "--timeout", "10s", *args], timeout=20, check=check)
        return json.loads(result.stdout) if check else result

    def agent_pid(self):
        state = self.user(["/bin/launchctl", "print", self.target], check=False)
        if state.returncode:
            if "Could not find service" not in state.stderr + state.stdout:
                raise RuntimeError("cannot establish native job absence")
            self.user(["/bin/launchctl", "print", self.domain])
            return 0
        assert "program = " + str(self.prefix / "current/shiki-agent") + "\n" in state.stdout
        match = re.search(r"(?m)^\s*pid = ([0-9]+)$", state.stdout)
        return int(match.group(1)) if match else 0

    def ctl(self, operation, _unit):
        data = plistlib.loads(self.plist.read_bytes())
        assert data.get("ProgramArguments", [None])[0] == str(self.prefix / "current/shiki-agent")
        if operation == "stop":
            assert self.agent_pid() > 1
            return self.user(["/bin/launchctl", "bootout", self.target])
        if operation == "start":
            assert self.agent_pid() == 0
            return self.user(["/bin/launchctl", "bootstrap", self.domain, self.plist])
        raise RuntimeError("unexpected native lifecycle operation")

    def children(self, *, require_sync=True, daemon=False):
        parent = self.daemon_child.pid if daemon else self.agent_pid()
        assert parent > 1
        host = self.record_process(parent, "daemon" if daemon else "agent")
        bundle = self.bundles[self.a][0] if daemon else (self.prefix / "current").resolve(strict=True)
        assert host["executable"] == str(bundle / ("shiki-daemon" if daemon else "shiki-agent"))
        records = [host]
        count = 0
        for line in self.user(["/bin/ps", "-axo", "pid=,ppid="]).stdout.splitlines():
            fields = line.split()
            if len(fields) != 2 or int(fields[1]) != parent:
                continue
            try:
                record = process_identity(int(fields[0]))
            except FileNotFoundError:
                continue
            name = Path(record["executable"]).name
            if name not in ("shiki-plugin-sync", "shiki-plugin-port"):
                continue
            assert record["executable"] == str(bundle / name)
            records.append(self.record_process(record["pid"], "server-worker" if daemon else "client-worker"))
            count += name == "shiki-plugin-sync"
        for record in records:
            record["sha256"] = digest(record["executable"])
            assert record["uids"] and all(uid == os.getuid() for uid in record["uids"])
            selected = self.a if daemon else json.loads((self.prefix / "installation.json").read_text())["active"]["version"]
            assert record["sha256"] == digest(self.bundles[selected][0] / Path(record["executable"]).name)
        self.persist_owned()
        if require_sync:
            assert count == 1, "ready boundary requires one real sync child"
        return records

    def client_operation(self, operation, version, expected, *, pending=False):
        bundle, pin = self.bundles[version]
        args = [bundle / "shiki-install", operation, "--role", "client", "--prefix", self.prefix, "--non-interactive"]
        if operation == "install":
            args += ["--version", version, "--bundle", bundle, "--checksums-sha256", pin,
                     "--config", self.config, "--state-dir", self.state, "--plugins-dir", self.runtime / "plugins",
                     "--bin-dir", self.runtime / "commands", "--path", "/opt/homebrew/bin:/usr/bin:/bin"]
            if not self.install_attempted:
                write_json(self.evidence / "activation-preflight.json", preflight())
            self.install_attempted = True
        response = self.user(args, check=False, timeout=90)
        assert response.returncode in (0, 3), response.stderr
        initial = json.loads(response.stdout)
        assert initial["version"] == expected
        if pending:
            assert response.returncode == 3 and initial["state"] == "application_pending" and initial["reason"] == "ENABLED_PLUGIN_NOT_READY"
        def probe():
            result = self.user([self.prefix / "current/shiki-install", "probe", "--role", "client", "--prefix", self.prefix, "--non-interactive"], check=False, timeout=30)
            assert result.returncode in (0, 3), result.stderr
            value = json.loads(result.stdout)
            assert value["version"] == expected and value["external_peer_acceptance"] == "pending"
            if pending:
                return value if result.returncode == 3 and value["state"] == "application_pending" and value["reason"] == "ENABLED_PLUGIN_NOT_READY" else None
            return value if result.returncode == 0 and value["state"] == "local_application_ready" else None
        final = wait_for(probe, timeout=45)
        self.records.append({"operation": operation, "version": expected, "initial_exit": response.returncode,
                             "initial_state": initial["state"], "final_state": final["state"], "reason": final["reason"]})
        return initial

    def sync_status(self, mapping):
        return self.cli("sync", "status", mapping["id"])

    def mappings(self):
        return {mapping["id"]: mapping for mapping in self.cli("sync", "mappings")["mappings"]}

    def plugin(self):
        return next(p for p in self.cli("plugins", "list", "--side", "local")["plugins"] if p["plugin_id"] == "sync")

    def setup(self):
        fixture = self.runtime / "terminal-fixture"
        self.write(fixture, b'#!/bin/bash\nif [ "${1:-}" = --version ]; then echo 0.0.0-schema-native-fixture; exit 0; fi\nif [ "${1:-} ${2:-} ${3:-}" = "auth status --json" ]; then echo \'{"loggedIn":true}\'; exit 0; fi\nwhile :; do sleep 60; done\n')
        os.chmod(fixture, 0o700)
        tls = self.runtime / "tls"
        tls.mkdir(mode=0o700)
        self.user(["openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes", "-keyout", tls / "key.pem", "-out", tls / "cert.pem", "-days", "1", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1"])
        for path in (tls / "cert.pem", tls / "key.pem"):
            os.chmod(path, 0o600)
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            address = "127.0.0.1:" + str(reservation.getsockname()[1])
        self.write(self.runtime / "address", address.encode())
        binary = self.bundles[self.a][0] / "shiki-daemon"
        self.user([binary, "init", "--config", self.daemon_config, "--user", self.account.pw_name,
                   "--state-dir", self.runtime / "daemon-state",
                   "--projects-dir", self.runtime / "projects", "--tailscale=false", "--direct-tls-address", address,
                   "--tls-cert", tls / "cert.pem", "--tls-key", tls / "key.pem", "--claude-executable", fixture])
        config_text = self.daemon_config.read_text()
        config_text, count = re.subn(r'(?m)^plugins_dir:.*$', 'plugins_dir: ' + json.dumps(str(self.bundles[self.a][0])), config_text)
        assert count == 1, "controlled daemon config must contain one plugins_dir"
        self.write(self.daemon_config, config_text.encode())
        self.user([binary, "token", "create", "--config", self.daemon_config, "--output", self.token, "--expires", "2h"])
        self.daemon_log = (self.runtime / "daemon.log").open("w")
        self.daemon_child = subprocess.Popen([str(binary), "serve", "--config", str(self.daemon_config)], env=self.env,
                                             stdout=self.daemon_log, stderr=subprocess.STDOUT)
        self.record_process(self.daemon_child.pid, "daemon")
        def ready():
            result = self.user([self.root / "probe", "--root", self.root, "--ready"], check=False)
            if result.returncode:
                return None
            value = json.loads(result.stdout)
            return {"daemon_id": value["credentials"][0]["daemon_id"]} if value["passed"] else None
        self.daemon = wait_for(ready)
        self.write(self.config, ("schema_version: 1\nplugins_dir: " + str(self.runtime / "plugins") + "\nagent:\n  socket: " + str(self.runtime / "run/agent.sock") + "\n  state_dir: " + str(self.state) + "\n  auto_start: false\ntailscale:\n  enabled: false\n  hostname: schema-native\n  state_dir: " + str(self.runtime / "tailscale") + "\n  control_url: https://controlplane.tailscale.com\nservers: {}\nprojects: {}\n").encode())
        self.client_operation("install", self.a, self.a)
        self.cli("connect", "schema", "--tls-address", address, "--tls-server-name", "localhost", "--tls-ca", str(tls / "cert.pem"), "--token-file", str(self.token))
        self.cli("plugins", "enable", "sync", "--side", "remote", "--server", "schema")
        self.cli("plugins", "enable", "sync", "--side", "local")
        wait_for(lambda: self.plugin()["state"] == "ready")
        write_json(self.evidence / "native-service.json", {"real_home": self.account.pw_dir, "uid": os.getuid(),
                   "domain": self.domain, "plist": plistlib.loads(self.plist.read_bytes()), "daemon_id": self.daemon["daemon_id"],
                   "daemon_is_native_service": False, "runtime": str(self.runtime)})

    def close(self):
        from cleanup import clean_session
        errors = []
        for session in self.sessions:
            try:
                clean_session(session, self.root)
            except Exception as error:
                errors.append(str(error))
        if self.install_attempted:
            try:
                if self.plist.exists() or self.plist.is_symlink():
                    assert not self.plist.is_symlink()
                    assert plistlib.loads(self.plist.read_bytes()).get("ProgramArguments", [None])[0] == str(self.prefix / "current/shiki-agent")
                self.agent_pid()  # Refuse a foreign job before installer mutation.
                helper = self.prefix / "current/shiki-install"
                self.user([helper, "uninstall", "--role", "client", "--prefix", self.prefix, "--non-interactive"], timeout=60)
            except Exception as error:
                errors.append(str(error))
        if self.daemon_child is not None:
            try:
                if self.daemon_child.poll() is None:
                    self.daemon_child.terminate()
                self.daemon_child.wait(timeout=20)
                self.daemon_log.close()
                assert self.daemon_child.returncode == 0, "daemon did not shut down normally"
            except Exception as error:
                errors.append(str(error))
        try:
            wait_for(lambda: gone(self.processes), timeout=20)
            final = preflight()
            write_json(self.evidence / "normal-cleanup.json", {"passed": True, "final_preflight": final,
                       "owned_process_generations_absent": True, "daemon_exit": self.daemon_child.returncode if self.daemon_child else None})
        except Exception as error:
            errors.append(str(error))
        if errors:
            raise RuntimeError("native normal cleanup incomplete: " + "; ".join(errors))
