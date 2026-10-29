"""Formula publication preserves newer versions and rejects partial releases."""

import base64
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("release_homebrew", Path(__file__).resolve().parents[1] / "release-homebrew.py")
homebrew = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(homebrew)


def formula(version, extra=""):
    return f'# shikigami-release: {version}\nclass Shikigami < Formula\n  version "{version[1:]}"\n{extra}end\n'


def remote(version, extra=""):
    text = formula(version, extra)
    return {"type": "file", "path": homebrew.FORMULA, "encoding": "base64", "size": len(text.encode()),
            "sha": homebrew.blob_hash(text), "content": base64.b64encode(text.encode()).decode()}


class FormulaPublicationTests(unittest.TestCase):
    def test_semver_order_uses_numeric_components_and_release_stages(self):
        versions = ["v0.0.1-alpha.2", "v0.0.1-alpha.10", "v0.0.1-beta.1", "v0.0.1-rc.1", "v0.0.1", "v0.0.2-alpha.1", "v0.0.10"]
        self.assertEqual(sorted(reversed(versions), key=homebrew.version_order), versions)

    def test_matching_same_version_is_idempotent_but_conflict_is_rejected(self):
        with patch.object(homebrew.release, "github_api", return_value=remote("v0.0.1-alpha.1")) as api:
            result = homebrew.update_formula("owner/repo", "v0.0.1-alpha.1", formula("v0.0.1-alpha.1"))
            self.assertEqual(result["status"], "unchanged")
            self.assertEqual(api.call_count, 1)
            with self.assertRaisesRegex(ValueError, "same-version"):
                homebrew.update_formula("owner/repo", "v0.0.1-alpha.1", formula("v0.0.1-alpha.1", "# changed\n"))

    def test_old_runs_and_future_prereleases_do_not_replace_stable(self):
        for old, new, status in [("v0.0.1-beta.1", "v0.0.1-alpha.9", "newer-version-retained"),
                                 ("v1.0.0", "v2.0.0-alpha.1", "stable-channel-retained")]:
            with self.subTest(old=old, new=new), patch.object(homebrew.release, "github_api", return_value=remote(old)) as api:
                self.assertEqual(homebrew.update_formula("owner/repo", new, formula(new))["status"], status)
                self.assertEqual(api.call_count, 1)

    def test_update_is_compare_and_swap_of_only_formula_on_master(self):
        version = "v0.0.1-alpha.2"
        before = remote("v0.0.1-alpha.1")
        result = {"content": {"path": homebrew.FORMULA, "sha": homebrew.blob_hash(formula(version))}, "commit": {"sha": "a" * 40}}
        with patch.object(homebrew.release, "github_api", side_effect=[before, result]) as api:
            self.assertEqual(homebrew.update_formula("owner/repo", version, formula(version))["status"], "updated")
            path, method, data = api.call_args.args
            self.assertEqual((path, method, data["branch"], data["sha"]),
                             ("repos/owner/repo/contents/Formula/shikigami.rb", "PUT", "master", before["sha"]))
            self.assertEqual(base64.b64decode(data["content"]).decode(), formula(version))

    def test_concurrent_newer_publication_wins_after_conflict(self):
        with patch.object(homebrew.release, "github_api", side_effect=[remote("v0.0.1-alpha.1"), RuntimeError("conflict (HTTP 409)"), remote("v0.0.1-alpha.3")]) as api:
            result = homebrew.update_formula("owner/repo", "v0.0.1-alpha.2", formula("v0.0.1-alpha.2"))
            self.assertEqual(result["status"], "newer-version-retained")
            self.assertEqual(api.call_count, 3)

    def test_conflict_retries_are_bounded_and_permission_errors_not_retried(self):
        calls = [remote("v0.0.1-alpha.1"), RuntimeError("conflict (HTTP 409)")] * 3
        with patch.object(homebrew.release, "github_api", side_effect=calls) as api:
            with self.assertRaisesRegex(RuntimeError, "409"):
                homebrew.update_formula("owner/repo", "v0.0.1-alpha.2", formula("v0.0.1-alpha.2"))
            self.assertEqual(api.call_count, 6)
        with patch.object(homebrew.release, "github_api", side_effect=[None, RuntimeError("denied (HTTP 403)")]) as api:
            with self.assertRaisesRegex(RuntimeError, "403"):
                homebrew.update_formula("owner/repo", "v0.0.1", formula("v0.0.1"))
            self.assertEqual(api.call_count, 2)

    def test_remote_content_identity_and_marker_must_match(self):
        for change in ({"sha": "0" * 40}, {"type": "symlink"}, {"size": 999999}, {"content": "!!!"}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                homebrew.decode_formula(dict(remote("v1.0.0"), **change))
        with self.assertRaisesRegex(ValueError, "matching"):
            homebrew.formula_version(formula("v1.0.0").replace('version "1.0.0"', 'version "2.0.0"'))

    def test_draft_or_missing_public_assets_cannot_update_formula(self):
        expected = {"archive": {"size": 2, "digest": "sha256:" + "a" * 64}}
        for draft, assets in [(True, []), (False, [])]:
            current = {"id": 1, "draft": draft, "published_at": "2026-10-29T00:00:00Z"}
            with self.subTest(draft=draft), patch.object(homebrew.release, "validate_source"), \
                    patch.object(homebrew.publish, "validate_existing"), \
                    patch.object(homebrew.release, "github_api", side_effect=[current, assets]) as api:
                with self.assertRaises(ValueError):
                    homebrew.require_public_release("owner/repo", "v1.0.0", "a" * 40, "1", expected)
                self.assertFalse(any(len(call.args) > 1 for call in api.call_args_list))


if __name__ == "__main__":
    unittest.main()
