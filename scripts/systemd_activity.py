"""Coordinate ordinary-user live traffic with privileged test-only upgrades."""
import atexit
import json
import os
from pathlib import Path
import subprocess
import time


class ActiveUpgradeProbe:
    def __init__(self, user, environment, *, home, evidence, cli, client_config,
                 project, session, remote_root, ledger):
        self.control = home / "upgrade-control"
        self.control.mkdir(mode=0o700)
        os.chown(self.control, 1000, 1000)
        config = {
            "CLI": str(cli), "ClientConfig": str(client_config), "Server": "systemd",
            "ProjectID": project, "SessionID": session, "RemoteRoot": str(remote_root),
            "LocalRoot": str(home / "upgrade-local"), "Ledger": str(ledger),
            "Control": str(self.control), "TLSAddress": "127.0.0.1:19527",
            "TokenFile": str(home / "device.token"), "TLSCert": str(home / "tls/cert.pem"),
        }
        config_path = self.control / "config.json"
        config_path.write_text(json.dumps(config))
        os.chmod(config_path, 0o600)
        os.chown(config_path, 1000, 1000)
        user(["go", "test", "-c", "-race", "-tags=e2e", "-o", "/build/upgrade.test", "./test/e2e"], cwd="/work", timeout=300)
        self.log = (evidence / "active-upgrade.log").open("w")
        self.process = subprocess.Popen([
            "/usr/sbin/runuser", "-u", "shikitest", "--", "env", "HOME=" + str(home),
            "USER=shikitest", "TERM=xterm-256color", "XDG_RUNTIME_DIR=/run/user/1000",
            "PATH=/home/shikitest/.local/bin:/usr/local/go/bin:/usr/bin:/bin",
            "SHIKI_UPGRADE_TEST_CONFIG=" + str(config_path), "/build/upgrade.test",
            "-test.v", "-test.run=^TestSystemdUpgradeWithActiveTransfers$", "-test.timeout=11m",
        ], env=environment, stdout=self.log, stderr=subprocess.STDOUT)
        self.records = {}
        atexit.register(self.close)
        self.wait("initialized", timeout=90)

    def wait(self, name, timeout=120):
        path = self.control / (name + ".json")
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if path.exists():
                return json.loads(path.read_text())
            code = self.process.poll()
            if code is not None:
                raise RuntimeError(f"active-upgrade probe exited {code} before {name}; inspect active-upgrade.log")
            time.sleep(0.05)
        raise RuntimeError(f"active-upgrade probe did not reach {name} in {timeout}s")

    def before(self, label):
        (self.control / (label + ".begin")).write_text("begin\n")
        self.records[label] = {"before": self.wait(label + ".ready")}

    def after(self, label, retired_processes):
        if any(Path(f"/proc/{pid}").exists() for pid in retired_processes):
            raise RuntimeError("old host/worker remains after installation")
        (self.control / (label + ".after")).write_text("installed\n")
        self.records[label]["after"] = self.wait(label + ".verified")
        self.records[label]["retired_processes"] = retired_processes
        self.records[label]["old_host_and_workers_reaped"] = True

    def finish(self):
        self.wait("complete")
        code = self.process.wait(timeout=30)
        if code != 0:
            raise RuntimeError(f"active-upgrade probe failed during cleanup: {code}")
        self.log.flush()
        return self.records

    def close(self):
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
        self.log.close()
