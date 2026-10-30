"""Cache isolation and failure-save decisions without builds, networks or services."""
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from contextlib import redirect_stdout

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
SPEC = importlib.util.spec_from_file_location("ci_go_cache", ROOT / "scripts/ci_go_cache.py")
cache = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(cache)


class GoCacheTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="shiki-go-cache-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.repository = self.root / "source"
        self.repository.mkdir()
        subprocess.run(["git", "init", "-q", str(self.repository)], check=True, capture_output=True)
        for name, text in {
            "go.mod": "module example.invalid/cache\n\ngo 1.26.5\n",
            "go.sum": "test-only-root-dependency\n",
            "main.go": "package main\nfunc main() {}\n",
            "README.md": "documentation\n",
            "docs/status.md": "not acceptance\n",
            ".gitignore": "bin/\n",
            "internal/assets/message.txt": "embedded content\n",
            "internal/assets/assets.go": 'package assets\nimport _ "embed"\n//go:embed message.txt\nvar Message string\n',
            "test/analyzer/go.mod": "module example.invalid/analyzer\n\ngo 1.26.5\n",
            "test/analyzer/go.sum": "test-only-analyzer-dependency\n",
            "scripts/tool-versions.env": "TOOL_VERSION=v1.0.0\n",
            ".github/workflows/ci.yml": "run: go run example.invalid/scanner@v1.2.3 ./...\n",
        }.items():
            path = self.repository / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True, capture_output=True)
        self.hashes = cache.input_hashes(self.repository)
        self.go = {"GOVERSION": "go1.26.10", "GOHOSTOS": "linux", "GOHOSTARCH": "amd64", "CGO_ENABLED": "1"}
        self.keys = cache.cache_keys("native", "ubuntu-24.04", "linux", "amd64", self.go, self.hashes)

    def test_docs_only_reuses_build_key_but_midrun_edits_block_save(self):
        (self.repository / "docs/status.md").write_text("updated progress\n")
        (self.repository / "README.md").write_text("updated overview\n")
        after = cache.input_hashes(self.repository)
        self.assertEqual(after["source"], self.hashes["source"])
        self.assertEqual(after["dependencies"], self.hashes["dependencies"])
        self.assertNotEqual(after["identity"], self.hashes["identity"])
        result = self.plan(hashes=after)
        self.assertEqual(result["reason"], "source_changed")
        self.assertFalse(result["save_build"])
        self.assertFalse(result["save_modules"])

    def test_non_go_embed_assets_and_build_scripts_change_source_key(self):
        (self.repository / "internal/assets/message.txt").write_text("different embedded bytes\n")
        after = cache.input_hashes(self.repository)
        self.assertNotEqual(after["source"], self.hashes["source"])
        self.assertEqual(after["dependencies"], self.hashes["dependencies"])
        (self.repository / "scripts/build.sh").write_text("go build -trimpath ./...\n")
        self.assertNotEqual(cache.input_hashes(self.repository)["source"], after["source"])

    def test_root_embedding_documents_prevents_excluding_real_build_inputs(self):
        (self.repository / "main.go").write_text('package main\nimport _ "embed"\n//go:embed docs/* README.md\nvar Documents string\n')
        before = cache.input_hashes(self.repository)
        (self.repository / "docs/status.md").write_text("embedded document changed\n")
        self.assertNotEqual(cache.input_hashes(self.repository)["source"], before["source"])

    def test_go_package_under_docs_keeps_its_embedded_documents(self):
        (self.repository / "docs/assets.go").write_text('package docs\nimport _ "embed"\n//go:embed status.md\nvar Status string\n')
        before = cache.input_hashes(self.repository)
        (self.repository / "docs/status.md").write_text("different package input\n")
        self.assertNotEqual(cache.input_hashes(self.repository)["source"], before["source"])

    def test_root_nested_dependencies_and_locked_tool_versions_change_module_key(self):
        for name in ("go.mod", "go.sum", "test/analyzer/go.mod", "test/analyzer/go.sum", "scripts/tool-versions.env"):
            with self.subTest(name=name):
                path = self.repository / name
                previous = path.read_text()
                path.write_text(previous + "changed\n")
                self.assertNotEqual(cache.input_hashes(self.repository)["dependencies"], self.hashes["dependencies"])
                path.write_text(previous)
        workflow = self.repository / ".github/workflows/ci.yml"
        workflow.write_text("run: go run example.invalid/scanner@v1.2.4 ./...\n")
        self.assertNotEqual(cache.input_hashes(self.repository)["dependencies"], self.hashes["dependencies"])

    def test_untracked_go_inputs_count_but_ignored_outputs_do_not(self):
        (self.repository / "bin").mkdir()
        (self.repository / "bin/shiki").write_bytes(b"ignored runtime output")
        self.assertEqual(cache.input_hashes(self.repository), self.hashes)
        (self.repository / "new.go").write_text("package main\n")
        self.assertNotEqual(cache.input_hashes(self.repository)["source"], self.hashes["source"])

    def test_symlink_source_input_is_rejected(self):
        asset = self.repository / "internal/assets/message.txt"
        asset.unlink()
        asset.symlink_to(self.repository / "go.mod")
        with self.assertRaises(ValueError):
            cache.input_hashes(self.repository)

    def test_roles_targets_host_and_cgo_have_distinct_build_keys(self):
        keys = {self.keys["build_warm_key"]}
        for operating_system in ("linux", "darwin"):
            for architecture in ("amd64", "arm64"):
                candidate = cache.cache_keys("compile", "ubuntu-24.04", operating_system, architecture, self.go, self.hashes)
                keys.add(candidate["build_warm_key"])
                # Downloaded module sources can be shared on the same host;
                # target-specific compiler output cannot.
                self.assertEqual(candidate["modules_key"], self.keys["modules_key"])
        self.assertEqual(len(keys), 5)
        changed = cache.cache_keys("native", "ubuntu-24.04", "linux", "amd64", {**self.go, "CGO_ENABLED": "0"}, self.hashes)
        self.assertNotIn(changed["build_warm_key"], keys)
        with self.assertRaises(ValueError):
            cache.cache_keys("native", "ubuntu-24.04", "darwin", "amd64", self.go, self.hashes)
        with self.assertRaises(ValueError):
            cache.cache_keys("compile", "ubuntu-24.04", "linux", "amd64", {**self.go, "GOVERSION": "go1.25.0"}, self.hashes)

    def entry(self, name="a", size=3):
        directory = self.root / "go-build"
        directory.mkdir(exist_ok=True)
        path = directory / (name * 64 + "-d")
        path.write_bytes(b"x" * size)
        return directory, path

    def test_inventory_uses_only_stat_and_preserves_source_bytes_modes(self):
        directory, path = self.entry()
        before = path.stat()
        with patch.object(Path, "read_bytes", side_effect=AssertionError("cache contents must not be read")):
            result = cache.inventory(directory, 100)
        self.assertTrue(result["eligible"])
        self.assertEqual((result["bytes"], result["files"]), (3, 1))
        self.assertEqual(result["entries"], {path.name: 3})
        self.assertEqual(path.read_bytes(), b"xxx")
        self.assertEqual(path.stat().st_mode, before.st_mode)
        self.assertEqual(path.stat().st_mtime_ns, before.st_mtime_ns)

    def test_inventory_size_count_time_and_symlink_budgets(self):
        directory, path = self.entry()
        for kwargs, reason in (({"byte_limit": 2}, "size_limit"),
                               ({"byte_limit": 100, "files_limit": 0}, "size_limit"),
                               ({"byte_limit": 100, "seconds": 0}, "scan_time_limit")):
            with self.subTest(reason=reason):
                result = cache.inventory(directory, **kwargs)
                self.assertFalse(result["eligible"])
                self.assertEqual(result["reason"], reason)
        (directory / "linked").symlink_to(path)
        self.assertEqual(cache.inventory(directory, 100)["reason"], "unsafe_entry")

    def state(self):
        return {"hashes": self.hashes, "keys": self.keys, "build_hit": False, "modules_hit": False,
                "matched_key": "", "baseline_eligible": True, "baseline_entries": {}}

    def plan(self, *, state=None, hashes=None, started=True, warm=False, build=None, modules=None):
        return cache.save_plan(state or self.state(), hashes or self.hashes, build_started=started, warm=warm,
                               build=build or {"eligible": True, "entries": {"a-d": 3}},
                               modules=modules or {"eligible": True})

    def test_failure_with_new_build_artifact_can_save_only_partial(self):
        result = self.plan()
        self.assertTrue(result["save_build"])
        self.assertEqual(result["build_save_key"], self.keys["build_partial_key"])
        self.assertFalse(self.plan(started=False)["save_build"])
        state = self.state()
        state["baseline_entries"] = {"a-d": 3}
        self.assertFalse(self.plan(state=state)["save_build"])
        self.assertFalse(self.plan(state=state, build={"eligible": True, "entries": {}})["save_build"])

    def test_success_promotes_same_source_partial_without_claiming_test_cache_pass(self):
        state = self.state()
        state["matched_key"] = self.keys["build_partial_key"]
        state["baseline_entries"] = {"a-d": 3}
        result = self.plan(state=state, warm=True)
        self.assertTrue(result["save_build"])
        self.assertEqual(result["build_save_key"], self.keys["build_warm_key"])
        state["build_hit"] = True
        state["modules_hit"] = True
        result = self.plan(state=state, warm=True)
        self.assertFalse(result["save_build"])
        self.assertFalse(result["save_modules"])

    def test_unbounded_or_unknown_baseline_is_never_saved(self):
        state = self.state()
        state["baseline_eligible"] = False
        self.assertFalse(self.plan(state=state)["save_build"])
        result = self.plan(build={"eligible": False, "entries": {}}, modules={"eligible": False})
        self.assertFalse(result["save_build"])
        self.assertFalse(result["save_modules"])

    def test_cache_paths_reject_workspace_parent_and_symlink(self):
        with patch.dict(os.environ, {"RUNNER_TEMP": str(self.root)}):
            for path in (self.root, self.repository, self.repository / "cache", Path("/")):
                with self.subTest(path=path), self.assertRaises(ValueError):
                    cache.cache_path(str(path), self.repository)
            directory, _ = self.entry()
            self.assertEqual(cache.cache_path(str(directory), self.repository), directory)
            link = self.root / "link"
            link.symlink_to(directory, target_is_directory=True)
            with self.assertRaises(ValueError):
                cache.cache_path(str(link), self.repository)

    def prepare_cli(self):
        build = self.root / "go-build"
        modules = self.root / "modules"
        module = modules / "example.invalid/dependency@v1.0.0/go.mod"
        module.parent.mkdir(parents=True)
        module.write_text("module example.invalid/dependency\n")
        output = self.root / "outputs"
        output.touch()
        environment = {"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted",
                       "RUNNER_TEMP": str(self.root), "GITHUB_OUTPUT": str(output),
                       "SHIKI_CACHE_BUILD_OUTCOME": "failure", "SHIKI_CACHE_ACCEPTANCE_OUTCOME": "failure",
                       "SHIKI_BUILD_CACHE_HIT": "false", "SHIKI_MODULE_CACHE_HIT": "false",
                       "SHIKI_BUILD_CACHE_MATCHED": ""}
        for context in (patch.dict(os.environ, environment), patch.object(cache, "ROOT", self.repository),
                        patch.object(cache, "command", return_value=json.dumps(
                            {**self.go, "GOCACHE": str(build), "GOMODCACHE": str(modules)}))):
            context.start()
            self.addCleanup(context.stop)
        with patch.object(sys, "argv", ["ci_go_cache.py", "prepare", "--role", "native", "--runner", "ubuntu-24.04",
                                            "--target-os", "linux", "--target-arch", "amd64"]):
            cache.main()
        with patch.object(sys, "argv", ["ci_go_cache.py", "baseline"]):
            cache.main()
        return build, output

    def test_cli_cold_baseline_then_failed_build_exports_bounded_partial_plan(self):
        build, output = self.prepare_cli()
        build.mkdir()
        entry = build / ("a" * 64 + "-d")
        entry.write_bytes(b"private compiled bytes must not be printed")
        with patch.object(sys, "argv", ["ci_go_cache.py", "plan"]), redirect_stdout(io.StringIO()) as printed:
            cache.main()
        result = json.loads((self.root / "shiki-go-cache/save-plan.json").read_text())
        self.assertTrue(result["save_build"])
        self.assertTrue(result["save_modules"])
        self.assertTrue(result["build_save_key"].endswith("-partial"))
        self.assertIn("save_build=true", output.read_text())
        self.assertNotIn("private compiled", printed.getvalue())
        self.assertNotIn("entries", result["build"])
        self.assertEqual((self.root / "shiki-go-cache/state.json").stat().st_mode & 0o777, 0o600)

    def test_cli_source_change_during_inventory_cannot_save_under_old_key(self):
        build, _ = self.prepare_cli()
        build.mkdir()
        (build / ("a" * 64 + "-d")).write_bytes(b"compiled bytes")
        inventory = cache.inventory
        def change_source(*args, **kwargs):
            value = inventory(*args, **kwargs)
            (self.repository / "main.go").write_text("package main\nfunc main() { println(1) }\n")
            return value
        with patch.object(cache, "inventory", side_effect=change_source), \
                patch.object(sys, "argv", ["ci_go_cache.py", "plan"]), redirect_stdout(io.StringIO()):
            cache.main()
        result = json.loads((self.root / "shiki-go-cache/save-plan.json").read_text())
        self.assertEqual(result["reason"], "source_changed")
        self.assertFalse(result["save_build"])
        self.assertFalse(result["save_modules"])


if __name__ == "__main__":
    unittest.main()
