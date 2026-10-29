"""Release source gates and archive/sidecar consistency before secrets are exposed."""

import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("release_actions", ROOT / "scripts/release-actions.py")
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)
VERSION = "v0.0.1-alpha.1"


class ReleaseGateTests(unittest.TestCase):
    def setUp(self):
        self.commit = "a" * 40
        self.tag = {"object": {"type": "commit", "sha": self.commit}}
        self.run = {"id": 123, "repository": {"full_name": "owner/repo"},
                    "head_repository": {"full_name": "owner/repo"}, "event": "push",
                    "path": ".github/workflows/release.yml", "head_sha": self.commit, "head_branch": VERSION,
                    "created_at": "2026-10-29T12:00:00Z"}
        self.comparison = {"status": "ahead", "merge_base_commit": {"sha": self.commit}}
        self.requests = []

    def api(self, path):
        self.requests.append(path)
        if "/actions/runs/" in path:
            return self.run
        if "/git/ref/tags/" in path:
            return self.tag
        if "/git/tags/" in path:
            return {"object": {"type": "commit", "sha": self.commit}}
        return self.comparison

    def gate(self, **kwargs):
        args = dict(repository="owner/repo", ref="refs/tags/" + VERSION,
                    commit=self.commit, run_id="123")
        args.update(kwargs)
        with patch.object(release, "github_api", side_effect=self.api):
            return release.validate_source(**args)

    def test_lightweight_and_annotated_tags_pin_source_and_run_start(self):
        for tag in (self.tag, {"object": {"type": "tag", "sha": "b" * 40}}):
            self.tag = tag
            result = self.gate()
            self.assertEqual(result, {"commit": self.commit, "version": VERSION,
                                      "built_at": "2026-10-29T12:00:00Z", "prerelease": "true"})
        self.assertTrue(any("/git/tags/" in path for path in self.requests))
        self.run["head_branch"] = "v1.2.3"
        self.assertEqual(self.gate(ref="refs/tags/v1.2.3")["prerelease"], "false")

    def test_gate_rejects_moved_tag_untrusted_run_and_non_master_source(self):
        for field, value in {"id": 124, "event": "workflow_dispatch", "head_sha": "b" * 40,
                             "path": ".github/workflows/ci.yml", "created_at": "bad", "head_branch": "v0.0.1-alpha.2",
                             "repository": {"full_name": "fork/repo"},
                             "head_repository": {"full_name": "fork/repo"}}.items():
            with self.subTest(field=field):
                saved = self.run[field]
                self.run[field] = value
                with self.assertRaises(ValueError):
                    self.gate()
                self.run[field] = saved
        for obj in ({"type": "commit", "sha": "b" * 40}, {"type": "tree", "sha": self.commit}):
            self.tag = {"object": obj}
            with self.assertRaises(ValueError):
                self.gate()
        self.tag = {"object": {"type": "commit", "sha": self.commit}}
        for status in ("behind", "diverged"):
            self.comparison["status"] = status
            with self.assertRaises(ValueError):
                self.gate()
        self.comparison = {"status": "ahead", "merge_base_commit": {"sha": "c" * 40}}
        with self.assertRaises(ValueError):
            self.gate()

    def test_invalid_identifiers_are_rejected_before_network(self):
        for kwargs in ({"ref": "refs/heads/master"}, {"ref": "refs/tags/../master"},
                       {"repository": "owner/repo/extra"}, {"commit": "A" * 40},
                       {"run_id": "123/attempts/1"}, {"run_id": "0"}):
            with self.subTest(kwargs=kwargs), self.assertRaises(ValueError):
                self.gate(**kwargs)
        self.assertEqual(self.requests, [])

    def test_versions_are_fixed_stable_or_supported_prereleases(self):
        for version in (VERSION, "v1.2.3-beta.2", "v2.0.0-rc.1", "v1.2.3", "v0.0.0"):
            release.validate_version(version)
        for version in ("dev", "latest", "v01.2.3-alpha.1", "v1.2.3-alpha.0",
                        VERSION + "\ncommit=evil", "$(touch x)", "v1.2.3-alpha.01", "v" + "1" * 81,
                        "v1.2.3+metadata", "v1.2.3-preview.1"):
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
