"""Release publication never exposes a partial or conflicting asset set."""

import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("release_publish", ROOT / "scripts/release-publish.py")
publish = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(publish)
VERSION = "v0.0.1-alpha.1"
COMMIT = "a" * 40


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory(prefix="shiki-publish-test-")
        self.addCleanup(self.folder.cleanup)
        self.directory = Path(self.folder.name)
        self.expected = {name: {"size": 3, "digest": "sha256:" + digest * 64}
                         for name, digest in (("first", "a"), ("second", "b"))}
        self.current = None
        self.assets = []
        self.calls = []
        self.uploads = []
        self.fail_upload = None
        self.version = VERSION
        self.notes = None

    def api(self, path, method="GET", data=None, missing=False):
        self.calls.append((path, method, data))
        if method == "POST" and path.endswith("generate-notes"):
            self.notes = data
            return {"body": "Generated changes"}
        if method == "POST" and path.endswith("/releases"):
            self.current = dict(data, id=42, html_url="https://github.com/owner/repo/releases/tag/" + self.version)
            return copy.deepcopy(self.current)
        if method == "PATCH":
            self.assertEqual(len(self.assets), len(self.expected), "published before every upload completed")
            self.current.update(data)
            return copy.deepcopy(self.current)
        if "/assets?" in path:
            return copy.deepcopy(self.assets)
        if path.endswith("/releases/42"):
            return copy.deepcopy(self.current)
        if "/releases/tags/" in path:
            return copy.deepcopy(self.current)
        if "releases?per_page" in path:
            return []
        self.fail("unexpected API call " + path)

    def upload(self, command, **kwargs):
        self.assertNotIn("--clobber", command)
        name = Path(command[4]).name
        self.uploads.append(name)
        if name == self.fail_upload:
            raise subprocess.CalledProcessError(1, command)
        self.assets.append(dict(name=name, state="uploaded", **self.expected[name]))

    def run_publish(self, source_check=None):
        with patch.object(publish.release, "github_api", side_effect=self.api), \
                patch.object(publish.release, "validate_source", side_effect=source_check), \
                patch.object(publish.subprocess, "run", side_effect=self.upload):
            return publish.publish("owner/repo", self.version, COMMIT, "123", self.directory,
                                   self.expected, "c" * 64, self.directory)

    def test_interrupted_upload_keeps_draft_and_resumes_only_missing_assets(self):
        self.fail_upload = "second"
        with self.assertRaises(subprocess.CalledProcessError):
            self.run_publish()
        self.assertTrue(self.current["draft"])
        self.assertEqual([a["name"] for a in self.assets], ["first"])
        self.assertFalse(any(method == "PATCH" for _, method, _ in self.calls))
        self.fail_upload = None
        self.uploads = []
        self.run_publish()
        self.assertEqual(self.uploads, ["second"])
        self.assertFalse(self.current["draft"])
        self.assertEqual(self.current["make_latest"], "false")

    def test_published_matching_release_is_noop_and_conflicts_are_never_overwritten(self):
        self.run_publish()
        self.calls, self.uploads = [], []
        self.run_publish()
        self.assertEqual(self.uploads, [])
        self.assertFalse(any(method in ("POST", "PATCH") for _, method, _ in self.calls))
        self.assets[0]["digest"] = "sha256:" + "0" * 64
        with self.assertRaisesRegex(ValueError, "refusing to overwrite"):
            self.run_publish()
        self.assertEqual(self.uploads, [])

    def test_tag_movement_before_publication_leaves_complete_draft_private(self):
        calls = []

        def source(*args):
            calls.append(args)
            if len(calls) == 2:
                raise ValueError("tag moved")

        with self.assertRaisesRegex(ValueError, "tag moved"):
            self.run_publish(source)
        self.assertTrue(self.current["draft"])
        self.assertEqual(len(self.assets), len(self.expected))
        self.assertFalse(any(method == "PATCH" for _, method, _ in self.calls))

    def test_draft_edited_during_upload_is_not_published(self):
        calls = []

        def source(*args):
            calls.append(args)
            if len(calls) == 2:
                self.current["target_commitish"] = "b" * 40

        with self.assertRaisesRegex(ValueError, "different source or workflow"):
            self.run_publish(source)
        self.assertTrue(self.current["draft"])
        self.assertFalse(any(method == "PATCH" for _, method, _ in self.calls))

    def test_wrong_source_or_run_cannot_reuse_a_draft(self):
        self.fail_upload = "first"
        with self.assertRaises(subprocess.CalledProcessError):
            self.run_publish()
        self.current["body"] = self.current["body"].replace('"run":"123"', '"run":"456"')
        self.uploads = []
        with self.assertRaisesRegex(ValueError, "different source or workflow"):
            self.run_publish()
        self.assertEqual(self.uploads, [])

    def test_stable_tag_uses_generated_notes_and_legacy_latest_policy(self):
        self.version = "v1.2.3"
        self.run_publish()
        self.assertFalse(self.current["prerelease"])
        self.assertEqual(self.current["make_latest"], "legacy")
        self.assertEqual(self.notes, {"tag_name": self.version, "target_commitish": COMMIT})

    def test_frozen_version_notes_replace_generated_notes(self):
        (self.directory / (VERSION + ".md")).write_text("Version-specific limits")
        self.run_publish()
        self.assertIsNone(self.notes)
        self.assertIn("Version-specific limits", self.current["body"])
        self.assertIn("docs/18-release-readiness.md", self.current["body"])

    def test_duplicate_extra_missing_or_unfinished_remote_assets_are_rejected(self):
        asset = dict(name="first", state="uploaded", **self.expected["first"])
        for assets in ([asset, asset], [dict(asset, name="unknown")],
                       [dict(asset, state="starter")], [dict(asset, digest=None)]):
            with self.subTest(assets=assets), self.assertRaises(ValueError):
                publish.missing_assets(assets, self.expected)
        self.assertEqual(publish.missing_assets([asset], self.expected), ["second"])

    def test_verifier_is_not_run_for_missing_signed_file_set(self):
        with patch.object(publish.subprocess, "check_output") as verify:
            with self.assertRaisesRegex(ValueError, "22"):
                publish.verify_inputs(self.directory, VERSION, "owner/repo", COMMIT, "123",
                                      Path("unused"), Path("unused"))
            verify.assert_not_called()

    def test_wrong_provenance_is_rejected_before_signature_verifier(self):
        names = {"PROVENANCE.json", "release-signing.pub"}
        for system, arch in publish.release.TARGETS:
            base = f"shikigami-{VERSION}-{system}-{arch}.tar.gz"
            names.update(base + suffix for suffix in ("", ".sha256", ".checksums-pin", ".sig.json", ".verification.json"))
        for name in names:
            (self.directory / name).write_text("{}")
        with patch.object(publish.subprocess, "check_output") as verify:
            with self.assertRaisesRegex(ValueError, "provenance"):
                publish.verify_inputs(self.directory, VERSION, "owner/repo", COMMIT, "123",
                                      Path("unused"), Path("unused"))
            verify.assert_not_called()
            (self.directory / "PROVENANCE.json").write_text("x" * 65537)
            with self.assertRaisesRegex(ValueError, "size limit"):
                publish.verify_inputs(self.directory, VERSION, "owner/repo", COMMIT, "123",
                                      Path("unused"), Path("unused"))
            verify.assert_not_called()


if __name__ == "__main__":
    unittest.main()
