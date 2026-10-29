"""Release source gates and archive/sidecar consistency before secrets are exposed."""

import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("release_actions", ROOT / "scripts/release-actions.py")
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)
VERSION = "v0.0.1-alpha.1"


class ReleaseGateTests(unittest.TestCase):
    def test_success_requires_exact_repository_workflow_commit_and_completed_ci(self):
        valid = {"id": 123, "repository": {"full_name": "owner/repo"},
                 "head_repository": {"full_name": "owner/repo"},
                 "path": ".github/workflows/ci.yml", "head_branch": "master", "event": "push",
                 "status": "completed", "conclusion": "success", "head_sha": "a" * 40}
        self.assertEqual(release.validate_run(valid, "owner/repo", "123"), "a" * 40)
        invalid = {"id": [124], "repository": [{"full_name": "other/repo"}],
                   "head_repository": [{"full_name": "fork/repo"}],
                   "path": [".github/workflows/sign-release.yml"], "head_branch": ["topic"],
                   "event": ["pull_request", "workflow_dispatch"], "status": ["in_progress", "queued"],
                   "conclusion": ["failure", "cancelled", "skipped", ""],
                   "head_sha": ["a" * 39, "G" * 40, "a" * 40 + "\ninjected=true"]}
        for field, values in invalid.items():
            for value in values:
                with self.subTest(field=field, value=value):
                    candidate = copy.deepcopy(valid)
                    candidate[field] = value
                    with self.assertRaises(ValueError):
                        release.validate_run(candidate, "owner/repo", "123")
        for identifier in ("0", "01", "123/attempts/1", "123\nx=y"):
            with self.subTest(identifier=identifier), self.assertRaises(ValueError):
                release.validate_run(valid, "owner/repo", identifier)

    def test_version_is_explicit_prerelease_without_shell_or_output_injection(self):
        for version in (VERSION, "v1.2.3-beta.2", "v2.0.0-rc.1"):
            release.validate_version(version)
        for version in ("dev", "latest", "v1.2.3", "v01.2.3-alpha.1", "v1.2.3-alpha.0",
                        VERSION + "\ncommit=evil", "$(touch x)", "v1.2.3-alpha.01", "v" + "1" * 81):
            with self.subTest(version=version), self.assertRaises(ValueError):
                release.validate_version(version)


class BundleTests(unittest.TestCase):
    def setUp(self):
        folder = tempfile.TemporaryDirectory(prefix="shiki-release-actions-")
        self.addCleanup(folder.cleanup)
        self.root = Path(folder.name)

    def bundle(self, system="linux", arch="amd64", mutation=None):
        name = f"shikigami-{VERSION}-{system}-{arch}.tar.gz"
        members = {item: b"fixture payload\n" for item in release.PAYLOADS - {"SHA256SUMS"}}
        members["release.json"] = json.dumps({"version": VERSION, "os": system, "arch": arch}).encode()
        if mutation == "identity":
            members["release.json"] = b'{"version":"wrong","os":"linux","arch":"amd64"}'
        members["SHA256SUMS"] = "".join(f"{hashlib.sha256(data).hexdigest()}  {item}\n"
                                            for item, data in sorted(members.items())).encode()
        if mutation == "content":
            members["shiki"] = b"corrupted"
        if mutation == "missing":
            members.pop("LICENSE")
        if mutation == "path":
            members["../outside"] = b"escape"
        path = self.root / name
        with tarfile.open(path, "w:gz") as archive:
            for item, data in members.items():
                header = tarfile.TarInfo(item)
                header.size = len(data)
                if mutation == "symlink" and item == "shiki":
                    header.type = tarfile.SYMTYPE
                    header.linkname = "/outside"
                    header.size = 0
                    archive.addfile(header)
                else:
                    archive.addfile(header, io.BytesIO(data))
                if mutation == "duplicate" and item == "LICENSE":
                    archive.addfile(header, io.BytesIO(data))
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        (self.root / (name + ".sha256")).write_text(f"{digest}  {name}\n")
        pin = hashlib.sha256(members["SHA256SUMS"]).hexdigest()
        (self.root / (name + ".checksums-pin")).write_text(pin + "\n")
        return path

    def test_exact_four_bundles_are_required(self):
        for system, arch in release.TARGETS:
            self.bundle(system, arch)
        self.assertEqual(len(release.validate_bundles(self.root, VERSION)), 4)
        (self.root / "unexpected").write_text("not a release output")
        with self.assertRaises(ValueError):
            release.validate_bundles(self.root, VERSION)
        (self.root / "unexpected").unlink()
        next(self.root.glob("*.sha256")).unlink()
        with self.assertRaises(ValueError):
            release.validate_bundles(self.root, VERSION)

    def test_archive_structure_identity_and_payload_are_checked(self):
        for mutation in ("identity", "content", "missing", "path", "symlink", "duplicate"):
            with self.subTest(mutation=mutation):
                self.bundle(mutation=mutation)
                with self.assertRaises(ValueError):
                    release.validate_bundle(self.root, VERSION, "linux", "amd64")

    def test_sidecars_and_files_cannot_be_substituted(self):
        archive = self.bundle()
        pin = Path(str(archive) + ".checksums-pin")
        pin.write_text("0" * 64 + "\n")
        with self.assertRaises(ValueError):
            release.validate_bundle(self.root, VERSION, "linux", "amd64")
        self.bundle()
        Path(str(archive) + ".sha256").write_text("0" * 64 + "  " + archive.name + "\n")
        with self.assertRaises(ValueError):
            release.validate_bundle(self.root, VERSION, "linux", "amd64")
        self.bundle()
        actual = archive.with_suffix(".original")
        archive.rename(actual)
        archive.symlink_to(actual)
        with self.assertRaises(ValueError):
            release.validate_bundle(self.root, VERSION, "linux", "amd64")


if __name__ == "__main__":
    unittest.main()
