"""Run bootstrap against real Go-signed fixtures over a private HTTPS server."""

import base64
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
import os
from pathlib import Path
import platform
import shlex
import shutil
import signal
import ssl
import subprocess
import tarfile
import tempfile
import threading
import time
import unittest


ROOT = Path(__file__).resolve().parents[2]
VERSION = "v0.0.1-beta.1"


class BootstrapTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.name != "posix":
            raise unittest.SkipTest("bootstrap supports Linux and macOS")
        cls.tools = tempfile.TemporaryDirectory(prefix="shiki-bootstrap-tools-")
        cls.addClassCleanup(cls.tools.cleanup)
        cls.tool_root = Path(cls.tools.name)
        candidates = [shutil.which("openssl"), "/opt/homebrew/opt/openssl@3/bin/openssl",
                      "/usr/local/opt/openssl@3/bin/openssl"]
        cls.openssl = None
        for candidate in candidates:
            if candidate and Path(candidate).is_file():
                version = subprocess.check_output([candidate, "version"], text=True)
                if version.startswith("OpenSSL ") and int(version.split()[1].split(".")[0]) >= 3:
                    cls.openssl = candidate
                    break
        if cls.openssl is None:
            raise RuntimeError("bootstrap behavioral tests require OpenSSL 3+ with Ed25519")
        cls.verifier = cls.tool_root / "release-trust"
        subprocess.run(["go", "build", "-trimpath", "-o", str(cls.verifier),
                        "./scripts/release-trust.go"], cwd=ROOT, check=True, timeout=180)
        cls.private = cls.tool_root / "private.pem"
        cls.public = cls.tool_root / "public.pem"
        subprocess.run([cls.openssl, "genpkey", "-algorithm", "ED25519", "-out", str(cls.private)],
                       check=True, capture_output=True, timeout=15)
        cls.private.chmod(0o600)
        subprocess.run([cls.openssl, "pkey", "-in", str(cls.private), "-pubout", "-out", str(cls.public)],
                       check=True, capture_output=True, timeout=15)
        cls.cert = cls.tool_root / "https.pem"
        cls.cert_key = cls.tool_root / "https-key.pem"
        subprocess.run([cls.openssl, "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                        "-subj", "/CN=127.0.0.1", "-addext", "subjectAltName=IP:127.0.0.1",
                        "-keyout", str(cls.cert_key), "-out", str(cls.cert)],
                       check=True, capture_output=True, timeout=30)

    def setUp(self):
        folder = tempfile.TemporaryDirectory(prefix="shiki-bootstrap-test-")
        self.addCleanup(folder.cleanup)
        self.root = Path(folder.name)
        self.workspaces = self.root / "workspaces"
        self.workspaces.mkdir()
        self.args_file = self.root / "installer-args"
        self.pid_file = self.root / "installer-pid"
        self.cancelled = self.root / "installer-cancelled"
        self.system = {"Linux": "linux", "Darwin": "darwin"}[platform.system()]
        self.arch = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[platform.machine()]
        self.name = f"shikigami-{VERSION}-{self.system}-{self.arch}.tar.gz"
        self.archive = self.root / self.name
        helper = b'''#!/bin/bash
set -eu
trap 'printf cancelled > "$SHIKI_TEST_CANCELLED"; exit 73' TERM
printf '%s\\n' "$$" > "$SHIKI_TEST_PID"
printf '%s\\0' "$@" > "$SHIKI_TEST_ARGS"
if [[ "${SHIKI_TEST_WAIT:-0}" == 1 ]]; then
  while :; do sleep 0.1; done
fi
exit "${SHIKI_TEST_STATUS:-0}"
'''
        members = {name: b"fixture payload\n" for name in (
            "shiki", "shiki-agent", "shiki-daemon", "shiki-plugin-sync", "shiki-plugin-port",
            "sync.manifest.json", "port.manifest.json", "install.sh", "install-client.sh",
            "uninstall.sh", "LICENSE")}
        members["shiki-install"] = helper
        members["release.json"] = json.dumps({"version": VERSION, "os": self.system, "arch": self.arch}).encode()
        members["SHA256SUMS"] = "".join(f"{hashlib.sha256(data).hexdigest()}  {name}\n"
                                       for name, data in sorted(members.items())).encode()
        self.pin = hashlib.sha256(members["SHA256SUMS"]).hexdigest()
        self.members = members
        self.write_bundle(members.items())
        self.pin_file = self.root / "checksums-pin"
        self.pin_file.write_text(self.pin + "\n")
        self.signature = self.root / (self.name + ".sig.json")
        self.sign_bundle()
        self.requests = []
        self.behavior = "normal"
        self.downloading = threading.Event()
        self.stop_download = threading.Event()
        owner = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *args):
                pass

            def do_GET(self):
                owner.requests.append(self.path)
                if self.path.startswith("/api/releases?"):
                    self.send_bytes(json.dumps([{"tag_name": VERSION, "draft": False}]).encode())
                elif self.path.endswith(".sig.json"):
                    self.send_bytes(owner.signature.read_bytes())
                elif self.path.endswith(".tar.gz") or self.path.startswith("/redirect/"):
                    if owner.behavior == "downgrade":
                        self.redirect(f"http://127.0.0.1:{owner.server.server_port}/insecure")
                    elif owner.behavior == "redirect" and not self.path.startswith("/objects/"):
                        self.redirect(owner.base + "/objects/bundle.tar.gz")
                    elif owner.behavior == "redirect-loop":
                        number = int(self.path.rsplit("/", 1)[-1]) if self.path.startswith("/redirect/") else 0
                        self.redirect(owner.base + f"/redirect/{number + 1}")
                    elif owner.behavior in ("oversized", "slow"):
                        self.send_response(200)
                        self.send_header("Transfer-Encoding", "chunked")
                        self.end_headers()
                        block = b"x" * (4096 if owner.behavior == "oversized" else 1)
                        try:
                            for _ in range(64 if owner.behavior == "oversized" else 300):
                                if owner.stop_download.is_set():
                                    break
                                self.wfile.write(f"{len(block):x}\r\n".encode() + block + b"\r\n")
                                self.wfile.flush()
                                owner.downloading.set()
                                if owner.behavior == "slow":
                                    time.sleep(0.05)
                            self.wfile.write(b"0\r\n\r\n")
                        except (BrokenPipeError, ConnectionResetError, ssl.SSLError):
                            pass
                    else:
                        self.send_bytes(owner.archive.read_bytes())
                else:
                    self.send_error(404)

            def redirect(self, url):
                self.send_response(302)
                self.send_header("Location", url)
                self.send_header("Content-Length", "0")
                self.end_headers()

            def send_bytes(self, data):
                self.send_response(200)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(self.cert, self.cert_key)
        self.server.socket = context.wrap_socket(self.server.socket, server_side=True)
        self.base = f"https://127.0.0.1:{self.server.server_port}"
        self.server_thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.server_thread.start()
        self.addCleanup(self.stop_server)
        self.script = self.root / "bootstrap.sh"
        self.source = (ROOT / "scripts/bootstrap.sh").read_text()
        start = self.source.index("SHIKI_EMBEDDED_PUB='") + len("SHIKI_EMBEDDED_PUB='")
        end = self.source.index("'", start)
        self.source = self.source[:start] + self.public.read_text().rstrip() + self.source[end:]
        self.replace('SHIKI_API="https://api.github.com/repos/${SHIKI_REPO}"',
                     "SHIKI_API=" + shlex.quote(self.base + "/api"))
        self.replace('SHIKI_RELEASES="https://github.com/${SHIKI_REPO}/releases"',
                     "SHIKI_RELEASES=" + shlex.quote(self.base + "/releases"))
        self.replace("mktemp -d /tmp/shiki-bootstrap.XXXXXXXX",
                     "mktemp -d " + shlex.quote(str(self.workspaces / "shiki-bootstrap.XXXXXXXX")))
        self.environment = dict(os.environ, CURL_CA_BUNDLE=str(self.cert), NO_PROXY="127.0.0.1", no_proxy="127.0.0.1",
                                SHIKI_TEST_ARGS=str(self.args_file), SHIKI_TEST_PID=str(self.pid_file),
                                SHIKI_TEST_CANCELLED=str(self.cancelled))

    def write_bundle(self, members):
        with tarfile.open(self.archive, "w:gz") as archive:
            for name, data in members:
                info = tarfile.TarInfo(name)
                info.mode = 0o755 if name.endswith("shiki-install") else 0o644
                info.size = len(data)
                archive.addfile(info, io.BytesIO(data))

    def sign_bundle(self):
        # Use the actual release signer, not a second implementation of its
        # domain separator, canonical JSON layout or Ed25519 envelope.
        self.signature.unlink(missing_ok=True)
        subprocess.run([str(self.verifier), "sign", "--archive", str(self.archive),
                        "--checksums-pin", str(self.pin_file), "--private-key", str(self.private),
                        "--signature", str(self.signature), "--version", VERSION,
                        "--os", self.system, "--arch", self.arch], check=True, capture_output=True, timeout=15)

    def replace(self, old, new):
        self.assertEqual(self.source.count(old), 1, "test substitution must match one production site")
        self.source = self.source.replace(old, new)

    def stop_server(self):
        self.stop_download.set()
        self.server.shutdown()
        self.server.server_close()
        self.server_thread.join(timeout=5)

    def start(self, *, version=VERSION, **environment):
        self.script.write_text(self.source)
        command = ["bash", str(self.script), "--role=daemon", "--user=bootstrap-fixture", "--yes", "--non-interactive"]
        if version is not None:
            command.append("--version=" + version)
        process = subprocess.Popen(command, env=dict(self.environment, **environment),
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.addCleanup(self.stop_process, process)
        return process

    @staticmethod
    def stop_process(process):
        if process.poll() is None:
            process.terminate()
            try:
                process.communicate(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.communicate(timeout=5)

    def finish(self, process, expected):
        stdout, stderr = process.communicate(timeout=20)
        self.assertEqual(process.returncode, expected, stdout + stderr)
        self.assertEqual(list(self.workspaces.iterdir()), [], "private bootstrap workspace leaked")
        return stdout, stderr

    def assert_installer_arguments(self):
        arguments = self.args_file.read_bytes().decode().rstrip("\0").split("\0")
        self.assertEqual(arguments[0], "install")
        self.assertIn("--version=" + VERSION, arguments)
        self.assertIn("--checksums-sha256=" + self.pin, arguments)
        self.assertIn("--role=daemon", arguments)
        self.assertIn("--user=bootstrap-fixture", arguments)
        bundle = next(value.split("=", 1)[1] for value in arguments if value.startswith("--bundle="))
        self.assertTrue(Path(bundle).is_relative_to(self.workspaces))
        self.assertFalse(Path(bundle).exists())

    def test_actual_signer_bundle_reaches_installer_and_preserves_its_exit_status(self):
        for status in (0, 3, 47):
            with self.subTest(status=status):
                self.finish(self.start(SHIKI_TEST_STATUS=str(status)), status)
                self.assert_installer_arguments()

    def test_https_redirect_and_latest_prerelease_resolution_are_supported(self):
        self.behavior = "redirect"
        self.finish(self.start(version=None), 0)
        self.assertTrue(any(path.startswith("/api/releases?") for path in self.requests))
        self.assertIn("/objects/bundle.tar.gz", self.requests)
        self.assert_installer_arguments()

    def test_tampered_archive_or_signature_never_executes_and_cleans_up(self):
        original_archive = self.archive.read_bytes()
        original_signature = self.signature.read_bytes()
        for target in ("archive", "signature"):
            with self.subTest(target=target):
                self.archive.write_bytes(original_archive)
                self.signature.write_bytes(original_signature)
                if target == "archive":
                    self.archive.write_bytes(original_archive + b"tampered")
                else:
                    envelope = json.loads(original_signature)
                    raw = bytearray(base64.b64decode(envelope["signature"]))
                    raw[0] ^= 1
                    envelope["signature"] = base64.b64encode(raw).decode()
                    self.signature.write_text(json.dumps(envelope, separators=(",", ":")) + "\n")
                process = self.start()
                stdout, stderr = process.communicate(timeout=20)
                self.assertNotEqual(process.returncode, 0, stdout + stderr)
                self.assertFalse(self.args_file.exists())
                self.assertEqual(list(self.workspaces.iterdir()), [])

    def test_download_redirect_downgrade_and_loops_are_rejected(self):
        for behavior in ("downgrade", "redirect-loop"):
            with self.subTest(behavior=behavior):
                self.behavior = behavior
                self.requests.clear()
                process = self.start()
                stdout, stderr = process.communicate(timeout=20)
                self.assertNotEqual(process.returncode, 0, stdout + stderr)
                self.assertFalse(self.args_file.exists())
                self.assertNotIn("/insecure", self.requests)
                self.assertLessEqual(len(self.requests), 6)
                self.assertEqual(list(self.workspaces.iterdir()), [])

    def test_signed_unexpected_or_duplicate_members_never_expand_or_execute(self):
        for name in ("unexpected", "LICENSE"):
            with self.subTest(member=name):
                self.write_bundle(list(self.members.items()) + [(name, b"extra member")])
                self.sign_bundle()
                process = self.start()
                stdout, stderr = process.communicate(timeout=20)
                self.assertNotEqual(process.returncode, 0, stdout + stderr)
                self.assertIn("Unexpected or duplicate bundle member", stderr)
                self.assertFalse(self.args_file.exists())
                self.assertEqual(list(self.workspaces.iterdir()), [])

    def test_chunked_download_and_decompressed_stream_have_hard_byte_bounds(self):
        self.behavior = "oversized"
        self.replace("SHIKI_MAX_ARCHIVE=1073741824", "SHIKI_MAX_ARCHIVE=1024")
        process = self.start()
        stdout, stderr = process.communicate(timeout=20)
        self.assertNotEqual(process.returncode, 0, stdout + stderr)
        self.assertRegex(stderr, r"limit|file size|filesize")
        self.assertFalse(self.args_file.exists())
        self.assertEqual(list(self.workspaces.iterdir()), [])
        self.behavior = "normal"
        self.replace("SHIKI_MAX_ARCHIVE=1024", "SHIKI_MAX_ARCHIVE=1073741824")
        self.replace("SHIKI_MAX_BUNDLE=4294967296", "SHIKI_MAX_BUNDLE=512")
        process = self.start()
        stdout, stderr = process.communicate(timeout=20)
        self.assertNotEqual(process.returncode, 0, stdout + stderr)
        self.assertIn("limit", stderr)
        self.assertFalse(self.args_file.exists())
        self.assertEqual(list(self.workspaces.iterdir()), [])

    def test_system_libressl_is_rejected_with_an_explicit_ed25519_prerequisite(self):
        fake = self.root / "libressl"
        fake.write_text("#!/bin/bash\nprintf 'LibreSSL 3.3.6\\n'\n")
        fake.chmod(0o700)
        self.replace('for _candidate in "$(command -v openssl || true)" \\\n'
                     '      /opt/homebrew/opt/openssl@3/bin/openssl /usr/local/opt/openssl@3/bin/openssl; do',
                     "for _candidate in " + shlex.quote(str(fake)) + "; do")
        _, stderr = self.finish(self.start(), 1)
        self.assertIn("OpenSSL 3+", stderr)
        self.assertIn("Ed25519", stderr)
        self.assertFalse(self.args_file.exists())

    def test_slow_download_has_a_total_deadline_and_cleans_up(self):
        self.behavior = "slow"
        self.replace("--max-time 300", "--max-time 1")
        process = self.start()
        stdout, stderr = process.communicate(timeout=20)
        self.assertNotEqual(process.returncode, 0, stdout + stderr)
        self.assertFalse(self.args_file.exists())
        self.assertEqual(list(self.workspaces.iterdir()), [])

    def test_cancellation_during_download_retains_signal_status_and_cleans_up(self):
        self.behavior = "slow"
        process = self.start()
        self.assertTrue(self.downloading.wait(5), "download did not start")
        process.send_signal(signal.SIGTERM)
        self.finish(process, 143)
        self.assertFalse(self.args_file.exists())

    def test_cancellation_is_forwarded_to_installer_before_workspace_cleanup(self):
        for number, status in ((signal.SIGHUP, 129), (signal.SIGINT, 130), (signal.SIGTERM, 143)):
            with self.subTest(signal=number):
                self.args_file.unlink(missing_ok=True)
                self.pid_file.unlink(missing_ok=True)
                self.cancelled.unlink(missing_ok=True)
                process = self.start(SHIKI_TEST_WAIT="1")
                deadline = time.monotonic() + 5
                while not self.args_file.exists() and time.monotonic() < deadline and process.poll() is None:
                    time.sleep(0.02)
                self.assertTrue(self.args_file.exists(), "validated installer did not start")
                pid = int(self.pid_file.read_text())
                process.send_signal(number)
                self.finish(process, status)
                self.assertEqual(self.cancelled.read_text(), "cancelled")
                with self.assertRaises(ProcessLookupError):
                    os.kill(pid, 0)


if __name__ == "__main__":
    unittest.main()
