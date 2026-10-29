"""Exercise real offline key rotation and reject false-positive verifier results."""
import importlib.util
import json
from pathlib import Path
import tempfile
import shutil
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("release_key_rotation", ROOT / "scripts/test-release-key-rotation.py")
drill = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(drill)


class KeyRotationTests(unittest.TestCase):
    def setUp(self):
        folder = tempfile.TemporaryDirectory(prefix="shiki-key-rotation-test-")
        self.addCleanup(folder.cleanup)
        self.root = Path(folder.name)

    def test_real_keys_and_verifier_preserve_history_and_reject_withdrawn_trust(self):
        evidence = self.root / "evidence"
        result = drill.run_drill(evidence)
        self.assertEqual(result["status"], "passed")
        self.assertTrue(result["temporary_private_keys_removed"])
        self.assertFalse(result["online_revocation"])
        cases = {row["name"]: row for row in result["checks"]}
        self.assertEqual(len(cases), 12)
        for name in ("old-release-current-trust-rejected", "new-release-old-key-rejected",
                     "withdrawn-key-current-trust-rejected", "removed-private-key-rejected",
                     "missing-current-trust-rejected"):
            self.assertEqual(cases[name]["exit_code"], 1)
        self.assertEqual(cases["historical-old-release-explicit-key"]["exit_code"], 0)
        self.assertEqual(cases["stale-explicit-key-still-authenticates"]["exit_code"], 0)
        self.assertEqual(json.loads((evidence / "summary.json").read_text()), result)
        self.assertFalse(any(b"PRIVATE KEY" in p.read_bytes() for p in evidence.rglob("*") if p.is_file()))

    def test_existing_evidence_is_never_overwritten(self):
        marker = self.root / "summary.json"
        marker.write_text("keep evidence")
        with self.assertRaises(FileExistsError):
            drill.run_drill(self.root)
        self.assertEqual(marker.read_text(), "keep evidence")

    def test_false_success_cleans_generated_keys_and_records_failure(self):
        evidence = self.root / "failure"
        with self.assertRaisesRegex(RuntimeError, "invalid signature output state"):
            drill.run_drill(evidence, verifier=Path(shutil.which("true")))
        result = json.loads((evidence / "summary.json").read_text())
        self.assertEqual(result["status"], "failed")
        self.assertTrue(result["temporary_private_keys_removed"])
        self.assertEqual(set(result["test_key_fingerprints"]), {"old", "new"})
        self.assertFalse(any(b"PRIVATE KEY" in p.read_bytes() for p in evidence.rglob("*") if p.is_file()))


if __name__ == "__main__":
    unittest.main()
