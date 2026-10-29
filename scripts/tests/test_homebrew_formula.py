"""The formula generator preserves the authenticated release boundary."""
import base64
import hashlib
import importlib.util
import json
import io
import tarfile
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("homebrew_formula", Path(__file__).resolve().parents[1] / "homebrew-formula.py")
formula = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(formula)


MIT_LICENSE = b'MIT License\n\nCopyright (c) 2026 Shikigami contributors\n\nPermission is hereby granted, free of charge, to any person obtaining a copy\nof this software and associated documentation files (the "Software"), to deal\nin the Software without restriction, including without limitation the rights\nto use, copy, modify, merge, publish, distribute, sublicense, and/or sell\ncopies of the Software, and to permit persons to whom the Software is\nfurnished to do so, subject to the following conditions:\n\nThe above copyright notice and this permission notice shall be included in all\ncopies or substantial portions of the Software.\n\nTHE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR\nIMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,\nFITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE\nAUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER\nLIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,\nOUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE\nSOFTWARE.\n'

class FormulaTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="shiki-formula-test-")
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.version = "v0.0.1-alpha.1"
        self.public = self.directory / "key.pub"
        self.public.write_text("-----BEGIN PUBLIC KEY-----\nYQ==\n-----END PUBLIC KEY-----\n")
        self.assets = {}
        for system, arch in formula.TARGETS:
            name = f"shikigami-{self.version}-{system}-{arch}.tar.gz"
            path = self.directory / name
            with tarfile.open(path, "w:gz") as archive:
                member = tarfile.TarInfo("LICENSE")
                member.size = len(MIT_LICENSE)
                archive.addfile(member, io.BytesIO(MIT_LICENSE))
            archive_bytes = path.read_bytes()
            payload = dict(version=self.version, os=system, arch=arch, archive=name,
                           archive_size=len(archive_bytes), archive_sha256=hashlib.sha256(archive_bytes).hexdigest(), checksums_sha256="b" * 64)
            raw = json.dumps(dict(format=1, payload=base64.b64encode(json.dumps(payload).encode()).decode(), signature="YQ==")).encode()
            (self.directory / (name + ".sig.json")).write_bytes(raw)
            self.assets[name] = dict(size=len(archive_bytes), digest="sha256:" + payload["archive_sha256"])
            self.assets[name + ".sig.json"] = dict(size=len(raw), digest="sha256:" + hashlib.sha256(raw).hexdigest())

    def render(self):
        return formula.render_formula(self.version, "DDSRem-Dev/Shikigami", self.directory, self.assets, "c" * 64, self.public)

    def test_each_platform_keeps_its_authenticated_archive(self):
        text = self.render()
        for system, arch in formula.TARGETS:
            name = f"shikigami-{self.version}-{system}-{arch}.tar.gz"
            self.assertIn(f"releases/download/{self.version}/{name}", text)
            self.assertIn(f'sha256 "{self.assets[name]["digest"][7:]}"', text)
        self.assertIn('license "MIT"', text)
        self.assertEqual(text.count("# shikigami-release:"), 1)
        self.assertIn('version "0.0.1-alpha.1"', text)

    def test_signature_replaced_after_verification_is_rejected(self):
        p = next(self.directory.glob("*.sig.json"))
        p.write_bytes(p.read_bytes() + b" ")
        with self.assertRaisesRegex(ValueError, "signature changed"):
            self.render()

    def test_wrong_authenticated_target_size_or_hash_is_rejected(self):
        name = f"shikigami-{self.version}-darwin-arm64.tar.gz"
        for key, value in (("size", 4), ("digest", "sha256:" + "0" * 64)):
            old = self.assets[name][key]
            self.assets[name][key] = value
            with self.assertRaisesRegex(ValueError, "metadata mismatch"):
                self.render()
            self.assets[name][key] = old

    def test_archive_license_is_exact_not_a_keyword_guess(self):
        path = self.directory / "license-fixture.tar.gz"
        standard = Path(__file__).resolve().parents[2] / "LICENSE"
        for text, expected in ((MIT_LICENSE, "MIT"), (standard.read_bytes(), "GPL-3.0-only"), (b"GNU GENERAL PUBLIC LICENSE Version 3\n", None)):
            with tarfile.open(path, "w:gz") as archive:
                member = tarfile.TarInfo("LICENSE")
                member.size = len(text)
                archive.addfile(member, io.BytesIO(text))
            raw = path.read_bytes()
            authenticated = dict(size=len(raw), digest="sha256:" + hashlib.sha256(raw).hexdigest())
            if expected:
                self.assertEqual(formula.archive_license(path, authenticated), expected)
            else:
                with self.assertRaisesRegex(ValueError, "unknown authenticated LICENSE"):
                    formula.archive_license(path, authenticated)
            authenticated["digest"] = "sha256:" + "0" * 64
            with self.assertRaisesRegex(ValueError, "archive changed"):
                formula.archive_license(path, authenticated)

    def test_archive_path_replaced_during_license_read_is_rejected(self):
        path = self.directory / f"shikigami-{self.version}-darwin-arm64.tar.gz"
        real_open = tarfile.open
        def replace_path(*args, **kwargs):
            replacement = self.directory / "replacement.tar.gz"
            replacement.write_bytes(path.read_bytes())
            replacement.replace(path)
            return real_open(*args, **kwargs)
        with patch.object(formula.tarfile, "open", side_effect=replace_path):
            with self.assertRaisesRegex(ValueError, "archive changed while reading LICENSE"):
                formula.archive_license(path, self.assets[path.name])

    def test_mixed_target_licenses_are_rejected(self):
        path = self.directory / f"shikigami-{self.version}-darwin-arm64.tar.gz"
        text = (Path(__file__).resolve().parents[2] / "LICENSE").read_bytes()
        with tarfile.open(path, "w:gz") as archive:
            member = tarfile.TarInfo("LICENSE")
            member.size = len(text)
            archive.addfile(member, io.BytesIO(text))
        raw = path.read_bytes()
        self.assets[path.name] = dict(size=len(raw), digest="sha256:" + hashlib.sha256(raw).hexdigest())
        signed = path.with_name(path.name + ".sig.json")
        envelope = json.loads(signed.read_bytes())
        payload = json.loads(base64.b64decode(envelope["payload"]))
        payload.update(archive_size=len(raw), archive_sha256=hashlib.sha256(raw).hexdigest())
        envelope["payload"] = base64.b64encode(json.dumps(payload).encode()).decode()
        encoded = json.dumps(envelope).encode()
        signed.write_bytes(encoded)
        self.assets[signed.name] = dict(size=len(encoded), digest="sha256:" + hashlib.sha256(encoded).hexdigest())
        with self.assertRaisesRegex(ValueError, "disagree on LICENSE"):
            self.render()

    def test_ruby_injection_identity_and_untrusted_repository_are_rejected(self):
        for version in ('v0.0.1";system("false")', "v01.0.0", "v0.0.1-alpha.0"):
            with self.assertRaises(ValueError):
                formula.render_formula(version, "DDSRem-Dev/Shikigami", self.directory, self.assets, "c" * 64, self.public)
        with self.assertRaises(ValueError):
            formula.render_formula(self.version, "untrusted/elsewhere", self.directory, self.assets, "c" * 64, self.public)


if __name__ == "__main__":
    unittest.main()
