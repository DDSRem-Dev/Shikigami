"""Ordinary-user product commands inside the owned systemd PID namespace."""
import json
import os
from pathlib import Path
import shutil
import tarfile

from common import digest, inventory, process_identity, run, wait_for, write_json


class Fixture:
    def __init__(self):
        assert os.geteuid() == 0 and Path("/proc/1/comm").read_text().strip() == "systemd"
        assert Path("/proc/1/cgroup").read_text().strip() == "0::/init.scope"
        self.evidence = Path("/evidence")
        self.evidence.mkdir(mode=0o700)
        self.home = Path("/home/shikitest")
        self.config = self.home / ".shiki/config.yaml"
        self.state = self.home / ".shiki/agent"
        self.prefix = self.home / ".local/share/shiki/install"
        self.sync = self.state / "plugin-state/sync"
        self.ledger = self.sync / "ledger/state.json"
        self.daemon_config = Path("/etc/shiki/daemon.yaml")
        self.cli_path = self.home / ".local/bin/shiki"
        self.a, self.b = "schema-development-a", "schema-development-b"
        self.env = {**os.environ, "PATH": "/usr/local/go/bin:/usr/bin:/bin", "HOME": str(self.home),
                    "TMPDIR": "/build/tmp", "GOCACHE": "/var/cache/shiki-go", "GOPATH": "/go",
                    "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off",
                    "GOMAXPROCS": "2", "GOFLAGS": "-p=1 -mod=readonly -buildvcs=false", "PYTHONDONTWRITEBYTECODE": "1"}
        os.chown("/var/cache/shiki-go", 1000, 1000)
        for directory in (Path("/build"), Path("/build/tmp")):
            directory.mkdir(mode=0o700)
            os.chown(directory, 1000, 1000)
        self.bundles = {}
        self.records = []
        self.auth_identity = None
        self.auth_probe_phases = []

    def user(self, args, **kwargs):
        return run(["/usr/sbin/runuser", "-u", "shikitest", "--", "env", "HOME=" + str(self.home),
                    "USER=shikitest", "XDG_RUNTIME_DIR=/run/user/1000",
                    "PATH=/home/shikitest/.local/bin:/usr/local/go/bin:/usr/bin:/bin", *args], env=self.env, **kwargs)

    def write(self, path, contents):
        path = Path(path)
        if not path.parent.exists():
            self.user(["mkdir", "-p", "-m", "700", path.parent])
        path.write_bytes(contents)
        os.chmod(path, 0o600)
        os.chown(path, 1000, 1000)

    def build(self):
        for source, version in (("/source-a", self.a), ("/source-b", self.b)):
            print("Building separate frozen source " + version, flush=True)
            work = Path("/work-" + version)
            original_inputs = inventory(source)
            shutil.copytree(source, work)
            run(["chown", "-R", "1000:1000", work])
            assert inventory(work) == original_inputs, "writable build copy differs before compilation"
            build = self.user(["env", "SHIKI_VERSION=" + version, "bash", "scripts/build.sh"], cwd=work, timeout=360)
            release = self.user(["go", "run", "scripts/release.go", "--dir", "bin", "--output", "/build/" + version, "--version", version], cwd=work, timeout=180)
            (self.evidence / (version + "-build.log")).write_text(build.stdout + build.stderr + release.stdout + release.stderr)
            archive = next(Path("/build", version).glob("*.tar.gz"))
            bundle = Path("/build", version, "bundle")
            bundle.mkdir(mode=0o755)
            with tarfile.open(archive) as packed:
                for entry in packed:
                    if not entry.isfile() or Path(entry.name).name != entry.name:
                        raise RuntimeError("release contains non-flat regular payload")
                    with packed.extractfile(entry) as src, (bundle / entry.name).open("xb") as dst:
                        shutil.copyfileobj(src, dst)
                    os.chmod(bundle / entry.name, 0o755 if entry.name.startswith("shiki") and not entry.name.endswith("json") else 0o644)
            pin = Path(str(archive) + ".checksums-pin").read_text().strip()
            assert all(digest(work / name) == checksum for name, checksum in original_inputs.items()), "build changed original source inputs"
            self.bundles[version] = (bundle, pin)
            build_info = {name: self.user(["go", "version", "-m", bundle / name]).stdout
                          for name in ("shiki", "shiki-agent", "shiki-daemon", "shiki-install", "shiki-plugin-sync", "shiki-plugin-port")}
            write_json(self.evidence / (version + "-artifacts.json"), {
                "version": version, "source": source, "archive_sha256": digest(archive),
                "build_source_inputs_unchanged": len(original_inputs), "binary_build_info": build_info,
                "go": self.user(["go", "version"]).stdout.strip(), "architecture": run(["uname", "-m"]).stdout.strip(),
                "checksums_pin": pin, "files": {p.name: digest(p) for p in sorted(bundle.iterdir())}})

            if version == self.b:
                # The product source remains the exact accepted frozen B.
                # Compile separately recorded opt-in tooling using B's real
                # transport/protocol packages, without changing any B input.
                helper = work / "test/diagnostics/schemaupgrade/authprobe.go"
                if helper.exists():
                    raise RuntimeError("auth probe would overwrite a frozen product input")
                shutil.copyfile("/tools/authprobe.go", helper)
                os.chown(helper, 1000, 1000)
                self.auth_probe_binary = Path("/build/schema-auth-probe")
                result = self.user(["go", "build", "-trimpath", "-tags=schemaupgrade", "-o", self.auth_probe_binary,
                                    "./test/diagnostics/schemaupgrade"], cwd=work, timeout=180)
                (self.evidence / "auth-probe-build.log").write_text(result.stdout + result.stderr)
                assert all(digest(work / name) == checksum for name, checksum in original_inputs.items())
                assert digest(helper) == digest("/tools/authprobe.go")
                write_json(self.evidence / "auth-probe-artifact.json", {
                    "source_b_original_inputs_unchanged": len(original_inputs),
                    "extra_tool_source": str(helper.relative_to(work)), "extra_tool_sha256": digest(helper),
                    "binary_sha256": digest(self.auth_probe_binary),
                    "binary_build_info": self.user(["go", "version", "-m", self.auth_probe_binary]).stdout,
                    "gomaxprocs": self.env["GOMAXPROCS"], "goflags": self.env["GOFLAGS"],
                    "shipped_product_binary": False})

    def auth_probe(self, phase, project, *, revoked):
        result = self.user([self.auth_probe_binary, "--project", project,
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
        assert receipt["origin"] == "https://127.0.0.1:19527" and receipt["uid"] == 1000
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

    def ctl(self, *args):
        return self.user(["systemctl", "--user", *args])

    def agent_pid(self):
        return int(self.ctl("show", "shiki-agent.service", "--property=MainPID", "--value").stdout.strip())

    def children(self, *, require_sync=True):
        parent = self.agent_pid()
        assert parent > 1, "installed agent has no live MainPID"
        result = [process_identity(parent)]
        selected = (self.prefix / "current").resolve(strict=True)
        receipt = json.loads((self.prefix / "installation.json").read_text())
        bundle = self.bundles[receipt["active"]["version"]][0]
        assert result[0]["executable"] == str(selected / "shiki-agent")
        sync_workers = 0
        for entry in Path("/proc").iterdir():
            if not entry.name.isdigit():
                continue
            try:
                fields = (entry / "stat").read_text().rsplit(")", 1)[1].split()
                command = (entry / "cmdline").read_bytes().split(b"\0")[0]
                if int(fields[1]) == parent and command.endswith((b"shiki-plugin-sync", b"shiki-plugin-port")):
                    identity = process_identity(int(entry.name))
                    assert identity["executable"] == str(selected / Path(os.fsdecode(command)).name)
                    result.append(identity)
                    if command.endswith(b"shiki-plugin-sync"):
                        sync_workers += 1
            except FileNotFoundError:
                continue
        assert all(record["uids"] == [1000] * 4 for record in result)
        for record in result:
            executable = Path(record["executable"])
            record["sha256"] = digest(executable)
            assert record["sha256"] == digest(bundle / executable.name), "live executable differs from selected build"
        if require_sync:
            assert sync_workers == 1, "ready boundary requires exactly one real sync child"
        return result

    def client_operation(self, operation, version, expected, *, pending=False):
        bundle, pin = self.bundles[version]
        args = [bundle / "shiki-install", operation, "--role", "client", "--non-interactive"]
        if operation == "install":
            args += ["--version", version, "--bundle", bundle, "--checksums-sha256", pin]
        response = self.user(args, timeout=90, check=False)
        if response.returncode not in (0, 3):
            raise RuntimeError(response.stderr)
        initial = json.loads(response.stdout)
        assert initial["version"] == expected, initial
        if pending:
            assert response.returncode == 3 and initial["state"] == "application_pending", initial
            assert initial["reason"] == "ENABLED_PLUGIN_NOT_READY", initial

        def probe():
            output = self.user([self.prefix / "current/shiki-install", "probe", "--role", "client", "--non-interactive"], timeout=30, check=False)
            if output.returncode not in (0, 3):
                raise RuntimeError(output.stderr)
            report = json.loads(output.stdout)
            assert report["version"] == expected
            if pending:
                return report if output.returncode == 3 and report["state"] == "application_pending" and report["reason"] == "ENABLED_PLUGIN_NOT_READY" else None
            return report if output.returncode == 0 and report["state"] == "local_application_ready" else None

        report = wait_for(probe, timeout=45)
        self.records.append({"operation": operation, "version": expected, "initial_exit": response.returncode,
                             "initial_state": initial["state"], "final_state": report["state"], "reason": report["reason"]})
        return initial

    def sync_status(self, mapping):
        return self.cli("sync", "status", mapping["id"])

    def mappings(self):
        return {m["id"]: m for m in self.cli("sync", "mappings")["mappings"]}

    def plugin(self):
        entries = self.cli("plugins", "list", "--side", "local")["plugins"]
        return next(p for p in entries if p["plugin_id"] == "sync")

    def setup(self):
        fixture = self.home / "schema-terminal-fixture"
        self.write(fixture, b'#!/bin/sh\nif [ "${1:-}" = --version ]; then echo 0.0.0-schema-fixture; exit 0; fi\nif [ "${1:-} ${2:-} ${3:-}" = "auth status --json" ]; then echo \'{"loggedIn":true}\'; exit 0; fi\nwhile :; do sleep 60; done\n')
        os.chmod(fixture, 0o700)
        tls = self.home / "tls"
        self.user(["mkdir", "-m", "700", tls])
        self.user(["openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes", "-keyout", tls / "key.pem", "-out", tls / "cert.pem", "-days", "1", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1"])
        self.user(["chmod", "600", tls / "key.pem", tls / "cert.pem"])
        for directory in (Path("/etc/shiki"), Path("/var/lib/shiki")):
            directory.mkdir(mode=0o700)
            os.chown(directory, 1000, 1000)
        bundle, pin = self.bundles[self.a]
        self.user([bundle / "shiki-daemon", "init", "--config", self.daemon_config, "--user", "shikitest", "--state-dir", "/var/lib/shiki", "--projects-dir", self.home / "projects", "--tailscale=false", "--direct-tls-address", "127.0.0.1:19527", "--tls-cert", tls / "cert.pem", "--tls-key", tls / "key.pem", "--claude-executable", fixture])
        self.token = self.home / "original-device.token"
        args = ["--role", "daemon", "--user", "shikitest", "--prefix", "/opt/shiki", "--non-interactive"]
        response = run([bundle / "shiki-install", "install", *args, "--config", self.daemon_config,
                        "--state-dir", "/var/lib/shiki", "--plugins-dir", "/var/lib/shiki/plugins",
                        "--bin-dir", "/opt/shiki-commands", "--projects-dir", self.home / "projects",
                        "--claude-executable", fixture, "--path", "/home/shikitest/.local/bin:/usr/bin:/bin",
                        "--version", self.a, "--bundle", bundle, "--checksums-sha256", pin,
                        "--token-output", self.token], timeout=90, check=False)
        if response.returncode not in (0, 3):
            raise RuntimeError(response.stderr)

        def daemon_ready():
            call = run(["/opt/shiki/current/shiki-install", "probe", *args], check=False)
            if call.returncode not in (0, 3):
                raise RuntimeError(call.stderr)
            report = json.loads(call.stdout)
            with (self.evidence / "daemon-readiness.jsonl").open("a") as log:
                log.write(json.dumps({key: report.get(key) for key in
                          ("state", "version", "reason", "application_state", "claude_version", "claude_login_state")}) + "\n")
            return report if report["state"] == "local_application_ready" else None

        self.daemon = wait_for(daemon_ready)
        if not self.token.exists():
            run(["/opt/shiki/current/shiki-install", "probe", *args, "--token-output", self.token])
        run(["loginctl", "enable-linger", "shikitest"])
        run(["systemctl", "start", "user@1000.service"])
        wait_for(lambda: Path("/run/user/1000/bus").exists())
        self.write(self.config, b"schema_version: 1\nplugins_dir: ~/.shiki/plugins\nagent:\n  socket: ~/.shiki/run/agent.sock\n  state_dir: ~/.shiki/agent\n  auto_start: false\ntailscale:\n  enabled: false\n  hostname: schema-development\n  state_dir: ~/.shiki/tailscale\n  control_url: https://controlplane.tailscale.com\nservers: {}\nprojects: {}\n")
        self.client_operation("install", self.a, self.a)
        self.cli("connect", "schema", "--tls-address", "127.0.0.1:19527", "--tls-server-name", "localhost", "--tls-ca", str(tls / "cert.pem"), "--token-file", str(self.token))
        self.cli("plugins", "enable", "sync", "--side", "remote", "--server", "schema")
        self.cli("plugins", "enable", "sync", "--side", "local")
        wait_for(lambda: self.plugin()["state"] == "ready")
        write_json(self.evidence / "services.json", {"systemd": run(["systemctl", "--version"]).stdout.splitlines()[0],
                   "agent_unit": self.ctl("cat", "shiki-agent.service").stdout,
                   "daemon_unit": run(["systemctl", "cat", "shiki-daemon.service"]).stdout})
