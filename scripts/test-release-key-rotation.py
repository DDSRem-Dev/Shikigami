#!/usr/bin/env python3
"""Exercise offline release-key rotation with disposable keys and inert bundles.

This is an acceptance drill, not a trust store or online revocation mechanism.
It never accepts real key paths, contacts GitHub, or executes bundle payloads.
"""

import argparse
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("rotation_release_actions", ROOT / "scripts/release-actions.py")
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)
VERSIONS = ("v0.0.0-alpha.1", "v0.0.0-alpha.2")


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def write(path, data):
    with path.open("xb") as stream:
        os.chmod(path, 0o600)
        stream.write(data)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def command(argv, *, env=None):
    return subprocess.run(list(map(str, argv)), capture_output=True, text=True,
                          timeout=180, env=env, cwd=ROOT)


def checked(argv, *, env=None):
    result = command(argv, env=env)
    require(result.returncode == 0, "drill prerequisite failed: " + result.stderr.strip())
    return result


def bundle(directory, version):
    archive = directory / f"shikigami-{version}-linux-arm64.tar.gz"
    members = {name: b"INERT KEY ROTATION FIXTURE; NEVER EXECUTE\n" for name in release.PAYLOADS - {"SHA256SUMS"}}
    members["release.json"] = json.dumps({"version": version, "os": "linux", "arch": "arm64"}).encode()
    members["SHA256SUMS"] = "".join(f"{hashlib.sha256(data).hexdigest()}  {name}\n"
                                         for name, data in sorted(members.items())).encode()
    with archive.open("xb") as output:
        os.chmod(archive, 0o600)
        with tarfile.open(fileobj=output, mode="w:gz") as tar:
            for name, data in sorted(members.items()):
                item = tarfile.TarInfo(name)
                item.size, item.mode, item.mtime = len(data), 0o600, 0
                tar.addfile(item, io.BytesIO(data))
    write(Path(str(archive) + ".sha256"), f"{digest(archive)}  {archive.name}\n".encode())
    write(Path(str(archive) + ".checksums-pin"), (hashlib.sha256(members["SHA256SUMS"]).hexdigest() + "\n").encode())
    release.validate_bundle(directory, version, "linux", "arm64")
    return archive


KEYGEN_SOURCE = r"""package main

import (
    "crypto/ed25519"
    "crypto/rand"
    "crypto/x509"
    "encoding/pem"
    "os"
    "path/filepath"
)

func save(path string, data []byte) {
    f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
    if err != nil { panic(err) }
    if _, err = f.Write(data); err != nil { f.Close(); panic(err) }
    if err = f.Close(); err != nil { panic(err) }
}

func main() {
    if len(os.Args) != 3 { panic("temporary key and public-output directories required") }
    for _, name := range []string{"old", "new"} {
        public, private, err := ed25519.GenerateKey(rand.Reader)
        if err != nil { panic(err) }
        secret, err := x509.MarshalPKCS8PrivateKey(private)
        if err != nil { panic(err) }
        save(filepath.Join(os.Args[1], name+".pem"), pem.EncodeToMemory(&pem.Block{Type:"PRIVATE KEY", Bytes:secret}))
        clear(private)
        clear(secret)
        der, err := x509.MarshalPKIXPublicKey(public)
        if err != nil { panic(err) }
        save(filepath.Join(os.Args[2], name+".pub"), pem.EncodeToMemory(&pem.Block{Type:"PUBLIC KEY", Bytes:der}))
        save(filepath.Join(os.Args[1], name+".der"), der)
    }
}
"""


def run_drill(evidence, verifier=None, go="go"):
    evidence = evidence.absolute()
    evidence.mkdir(mode=0o700)  # Existing outputs are never overwritten.
    records, fingerprints = [], {}
    report = {"status": "failed", "scope": "disposable keys, inert bundles, offline caller-selected trust",
              "online_revocation": False, "real_signing_configuration_changed": False,
              "checks": records, "test_key_fingerprints": fingerprints}
    workspace = None
    try:
        with tempfile.TemporaryDirectory(prefix="shiki-key-rotation-") as temporary:
            workspace = Path(temporary)
            require(stat.S_IMODE(workspace.stat().st_mode) == 0o700, "temporary keys require mode0700 directory")
            env = dict(os.environ, GOMAXPROCS="2", GOFLAGS="-p=1 -mod=readonly -buildvcs=false")
            for variable in ("GOOS", "GOARCH", "GOBIN"):
                env.pop(variable, None)
            if verifier is None:
                verifier = workspace / "release-trust"
                checked([go, "build", "-trimpath", "-o", verifier, "./scripts/release-trust.go"], env=env)
            verifier = Path(verifier).resolve(strict=True)
            generator_source, generator = workspace / "keygen.go", workspace / "keygen"
            write(generator_source, KEYGEN_SOURCE.encode())
            checked([go, "build", "-trimpath", "-o", generator, generator_source], env=env)
            checked([generator, workspace, evidence])
            for key in ("old", "new"):
                private = workspace / (key + ".pem")
                require(stat.S_IMODE(private.stat().st_mode) == 0o600, "temporary private key mode changed")
                fingerprints[key] = digest(workspace / (key + ".der"))
            require(fingerprints["old"] != fingerprints["new"], "test keys must differ")
            archives = [bundle(evidence, version) for version in VERSIONS]
            original = {path.name: digest(path) for path in evidence.iterdir()}

            def sign(label, number, key, success=True):
                archive, version = archives[number], VERSIONS[number]
                signature = evidence / (label + ".sig.json")
                result = command([verifier, "sign", "--archive", archive, "--checksums-pin", str(archive) + ".checksums-pin",
                                  "--private-key", workspace / (key + ".pem"), "--signature", signature,
                                  "--version", version, "--os", "linux", "--arch", "arm64"])
                require(result.returncode == (0 if success else 1), label + ": unexpected signer exit")
                require(signature.is_file() if success else not signature.exists(), label + ": invalid signature output state")
                records.append({"name": label, "operation": "sign", "exit_code": result.returncode,
                                "expected": "accepted" if success else "rejected", "stderr": result.stderr.strip()})
                return signature

            def verify(label, number, signature, key, success, fingerprint=None):
                output = evidence / label
                result = command([verifier, "verify", "--archive", archives[number], "--signature", signature,
                                  "--public-key", key, "--output", output, "--version", VERSIONS[number],
                                  "--os", "linux", "--arch", "arm64"])
                require(result.returncode == (0 if success else 1), label + ": unexpected verifier exit")
                if success:
                    receipt = json.loads(result.stdout)
                    require(receipt["key_fingerprint_sha256"] == fingerprint, label + ": wrong key identity")
                    require(receipt["archive_sha256"] == digest(archives[number]), label + ": wrong authenticated digest")
                    require(digest(Path(receipt["archive"])) == digest(archives[number]), label + ": private copy differs")
                    require(json.loads((output / "receipt.json").read_text()) == receipt, label + ": receipt differs")
                else:
                    require(not output.exists() and not result.stdout.strip(), label + ": failed verification published output")
                records.append({"name": label, "operation": "verify", "exit_code": result.returncode,
                                "expected": "accepted" if success else "rejected", "stderr": result.stderr.strip()})

            current = workspace / "current.pub"
            shutil.copyfile(evidence / "old.pub", current)
            old_signature = sign("old-release", 0, "old")
            verify("before-rotation", 0, old_signature, current, True, fingerprints["old"])
            # This switches only this drill's caller-selected key. It is not a product trust store.
            shutil.copyfile(evidence / "new.pub", workspace / "replacement.pub")
            os.replace(workspace / "replacement.pub", current)
            new_signature = sign("new-release", 1, "new")
            verify("after-rotation", 1, new_signature, current, True, fingerprints["new"])
            verify("old-release-current-trust-rejected", 0, old_signature, current, False)
            verify("new-release-old-key-rejected", 1, new_signature, evidence / "old.pub", False)
            verify("historical-old-release-explicit-key", 0, old_signature, evidence / "old.pub", True, fingerprints["old"])
            # Model a compromised old signing key; signatures contain no trusted signing time.
            forged = sign("withdrawn-key-new-release", 1, "old")
            verify("withdrawn-key-current-trust-rejected", 1, forged, current, False)
            verify("stale-explicit-key-still-authenticates", 1, forged, evidence / "old.pub", True, fingerprints["old"])
            (workspace / "old.pem").unlink()
            sign("removed-private-key-rejected", 1, "old", False)
            current.unlink()
            verify("missing-current-trust-rejected", 1, new_signature, current, False)
            require(all(digest(evidence / name) == value for name, value in original.items()), "input fixtures or public keys changed")
            report.update(status="passed", verifier_sha256=digest(verifier), fixture_inputs_unchanged=True,
                          limitation="Explicitly supplied old keys still authenticate old-key signatures; no online revocation or trusted timestamp exists.")
    finally:
        report["temporary_private_keys_removed"] = workspace is not None and not workspace.exists()
        write(evidence / "summary.json", (json.dumps(report, indent=2) + "\n").encode())
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--evidence", type=Path, required=True, help="new evidence directory; public test fixtures only, no private keys retained")
    parser.add_argument("--verifier", type=Path, help="prebuilt verifier from a trusted checkout; otherwise build it")
    parser.add_argument("--go", default="go", help="Go executable used only when building the verifier")
    args = parser.parse_args()
    result = run_drill(args.evidence, args.verifier, args.go)
    print(json.dumps({"status": result["status"], "checks": len(result["checks"]), "evidence": str(args.evidence)}))


if __name__ == "__main__":
    main()
