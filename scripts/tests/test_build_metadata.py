"""Exercise shell metadata validation and the flags actually sent to Go."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
PACKAGE = "github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
PROGRAMS = {
    "bin/shiki", "bin/shiki-agent", "bin/shiki-daemon", "bin/shiki-install",
    "bin/shiki-plugin-sync", "bin/shiki-plugin-port",
}


class BuildMetadataTests(unittest.TestCase):
    entrypoint = "build.sh"

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="shiki-build-metadata-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "scripts").mkdir()
        for name in ("build.sh", "build-embedded.sh", "build-metadata.sh"):
            shutil.copyfile(ROOT / "scripts" / name, self.root / "scripts" / name)
        for entrypoint in ("cmd/shiki", "cmd/shiki-agent", "cmd/shiki-daemon",
                           "cmd/shiki-install", "plugins/sync", "plugins/port"):
            (self.root / entrypoint).mkdir(parents=True)
        (self.root / "internal/plugin").mkdir(parents=True)
        self.log = self.root / "go-calls.jsonl"
        fakebin = self.root / "fakebin"
        fakebin.mkdir()
        fakego = fakebin / "go"
        fakego.write_text(
            "#!/usr/bin/env python3\n"
            "import json, os, sys\n"
            "with open(os.environ['SHIKI_TEST_GO_LOG'], 'a') as stream:\n"
            "    stream.write(json.dumps(sys.argv[1:]) + '\\n')\n"
        )
        fakego.chmod(0o700)
        self.env = dict(os.environ)
        for name in ("SHIKI_VERSION", "SHIKI_COMMIT", "SHIKI_BUILT_AT", "SHIKI_EMBED_VERIFY"):
            self.env.pop(name, None)
        self.env.update(PATH=str(fakebin) + os.pathsep + os.environ["PATH"],
                        SHIKI_TEST_GO_LOG=str(self.log))

    def run_script(self, metadata):
        self.log.unlink(missing_ok=True)
        return subprocess.run(
            ["bash", str(self.root / "scripts" / self.entrypoint)],
            env={**self.env, **metadata}, text=True, capture_output=True, timeout=20,
        )

    def emitted_flags(self, metadata):
        result = self.run_script(metadata)
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = [json.loads(line) for line in self.log.read_text().splitlines()]
        if self.entrypoint == "build.sh":
            self.assertEqual(calls[0], ["build", "./..."])
        builds = [call for call in calls if call[0] == "build" and "-o" in call]
        self.assertEqual(len(builds), 6)
        expected = PROGRAMS
        if self.entrypoint == "build-embedded.sh":
            expected = {name.replace("bin/shiki-plugin-", "internal/plugin/embedded-assets/shiki-plugin-")
                        for name in PROGRAMS}
            self.assertFalse((self.root / "internal/plugin/embedded-assets").exists())
        self.assertEqual({call[call.index("-o") + 1] for call in builds}, expected)
        flags = {call[call.index("-ldflags") + 1] for call in builds}
        self.assertEqual(len(flags), 1)
        # Both plugin manifests still receive the same explicit version.
        manifests = [call for call in calls if call[0] == "run"]
        self.assertEqual(len(manifests), 2)
        for call in manifests:
            self.assertEqual(call[call.index("--version") + 1],
                             metadata.get("SHIKI_VERSION") or "dev")
        return flags.pop()

    def assert_linked_metadata(self, flags, expected):
        # A tiny real executable isolates linker behavior from application startup.
        # Use the repository's actual buildinfo source, not a duplicate definition.
        probe = self.root / "probe"
        probe.mkdir()
        package = probe / "pkg/plugin/buildinfo"
        package.mkdir(parents=True)
        shutil.copyfile(ROOT / "pkg/plugin/buildinfo/buildinfo.go", package / "buildinfo.go")
        (probe / "go.mod").write_text("module github.com/DDSRem-Dev/Shikigami\n\ngo 1.26.5\n")
        (probe / "main.go").write_text(
            'package main\nimport ("encoding/json"; "os"; "' + PACKAGE + '")\n'
            'func main() { if err := json.NewEncoder(os.Stdout).Encode(buildinfo.Current()); '
            'err != nil { panic(err) } }\n'
        )
        env = dict(os.environ)
        for name in ("GOOS", "GOARCH", "GOWORK"):
            env.pop(name, None)
        env.update(GOMAXPROCS="2", GOFLAGS="-p=1 -mod=readonly -buildvcs=false", GOWORK="off")
        binary = probe / "probe"
        built = subprocess.run(
            [shutil.which("go"), "build", "-trimpath", "-ldflags", flags, "-o", str(binary), "."],
            cwd=probe, env=env, text=True, capture_output=True, timeout=90,
        )
        self.assertEqual(built.returncode, 0, built.stderr)
        result = subprocess.run([str(binary)], text=True, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        actual = json.loads(result.stdout)
        self.assertEqual({key: actual[key] for key in expected}, expected)

    def test_explicit_metadata_reaches_all_six_builds_and_real_binary(self):
        metadata = {"SHIKI_VERSION": "v0.0.0-metadata-test", "SHIKI_COMMIT": "a1" * 20,
                    "SHIKI_BUILT_AT": "2024-02-29T23:59:59Z"}
        self.assert_linked_metadata(self.emitted_flags(metadata), {
            "version": metadata["SHIKI_VERSION"], "commit": metadata["SHIKI_COMMIT"],
            "built_at": metadata["SHIKI_BUILT_AT"],
        })

    def test_defaults_remain_unknown_in_real_binary(self):
        self.assert_linked_metadata(self.emitted_flags({}), {
            "version": "dev", "commit": "unknown", "built_at": "unknown",
        })

    def test_sha256_commit_and_utc_calendar_boundaries(self):
        for stamp in ("2000-02-29T00:00:00Z", "2026-04-30T01:02:03Z"):
            with self.subTest(stamp=stamp):
                self.emitted_flags({"SHIKI_COMMIT": "AB" * 32, "SHIKI_BUILT_AT": stamp})

    def test_invalid_metadata_is_rejected_before_any_build(self):
        invalid = {
            "SHIKI_VERSION": ["v1 -X injected=value", "v1\nvalue", "$(touch sentinel)"],
            "SHIKI_COMMIT": ["a" * 39, "a" * 41, "a" * 63, "g" * 40,
                             "a" * 40 + " -X injected=value", "unknown\n"],
            "SHIKI_BUILT_AT": ["2026-02-29T00:00:00Z", "1900-02-29T00:00:00Z",
                               "2026-04-31T00:00:00Z", "2026-00-01T00:00:00Z",
                               "2026-01-01T24:00:00Z", "2026-01-01T00:00:60Z",
                               "2026-01-01T00:00:00+00:00", "2026-01-01T00:00:00Z -X x=y",
                               "2026-01-01T00:00:00Z\n", "`touch sentinel`"],
        }
        for key, values in invalid.items():
            for value in values:
                with self.subTest(key=key, value=value):
                    result = self.run_script({key: value})
                    self.assertEqual(result.returncode, 1)
                    self.assertIn(key, result.stderr)
                    self.assertFalse(self.log.exists(), "invalid input reached Go")
                    self.assertFalse((self.root / "bin").exists())
                    self.assertFalse((self.root / "internal/plugin/embedded-assets").exists())


class EmbeddedBuildMetadataTests(BuildMetadataTests):
    entrypoint = "build-embedded.sh"


if __name__ == "__main__":
    unittest.main()
