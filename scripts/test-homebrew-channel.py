#!/usr/bin/env python3
"""Exercise the public Homebrew channel only on disposable GitHub-hosted runners."""

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import stat
import subprocess
import time


TAP = "ddsrem-dev/shikigami"
FORMULA = TAP + "/shikigami"
ORIGIN = "https://github.com/DDSRem-Dev/Shikigami"
MEMBERS = {"shiki", "shiki-agent", "shiki-daemon", "shiki-install", "shiki-plugin-sync",
           "shiki-plugin-port", "sync.manifest.json", "port.manifest.json", "install.sh",
           "install-client.sh", "uninstall.sh", "LICENSE", "release.json", "SHA256SUMS"}


def digest(path):
    h = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def write_json(path, value):
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
    temporary.replace(path)


def identity(path):
    try:
        info = path.lstat()
    except FileNotFoundError:
        return None
    if not stat.S_ISDIR(info.st_mode) or path.is_symlink():
        raise ValueError(f"refusing unexpected object at {path}")
    return {"device": info.st_dev, "inode": info.st_ino}


def require_owned(path, expected):
    actual = identity(path)
    if actual is not None and (expected is None or actual != expected):
        raise ValueError(f"refusing cleanup of unowned/replaced directory: {path}")
    return actual is not None


def snapshot_paths(paths):
    result = {}
    for root in paths:
        if not root.exists() and not root.is_symlink():
            result[str(root)] = None
            continue
        selected = [root]
        if root.is_dir() and not root.is_symlink():
            selected += sorted(root.rglob("*"))
        if len(selected) > 4096:
            raise ValueError("existing Shikigami state is too large for an isolated runner check")
        for path in selected:
            item = path.lstat()
            value = {"mode": item.st_mode, "uid": item.st_uid, "gid": item.st_gid}
            if path.is_symlink():
                value["link"] = os.readlink(path)
            elif path.is_file():
                value["sha256"] = digest(path)
            result[str(path)] = value
    return result


def formula_metadata(path, public_key):
    text = path.read_text()
    versions = re.findall(r"^# shikigami-release: (v[^\s]+)$", text, re.MULTILINE)
    public = re.search(r"RELEASE_PUBLIC_KEY = <<~'PEM'\.freeze\n(.*?)\n  PEM", text, re.DOTALL)
    envelopes = re.search(r"RELEASE_SIGNATURES = JSON.parse\(<<~'JSON'\)\.freeze\n(.*?)\n  JSON", text, re.DOTALL)
    if len(versions) != 1 or not public or not envelopes:
        raise ValueError("missing unique public formula release identity")
    if public[1].strip() != public_key.read_text().strip():
        raise ValueError("tap formula signing key differs from the independently trusted checkout key")
    return versions[0], json.loads(envelopes[1])


def verify_bundle(bundle, envelope, version, system, arch):
    metadata = json.loads(base64.b64decode(envelope["payload"], validate=True))
    if (metadata["version"], metadata["os"], metadata["arch"]) != (version, system, arch):
        raise ValueError("signed bundle target differs from native runner")
    if {p.name for p in bundle.iterdir()} != MEMBERS:
        raise ValueError("installed bundle must preserve exactly fourteen authenticated members")
    if any(p.is_symlink() or not p.is_file() for p in bundle.iterdir()):
        raise ValueError("installed bundle contains a non-regular member")
    if digest(bundle / "SHA256SUMS") != metadata["checksums_sha256"]:
        raise ValueError("installed bundle checksum pin differs from signed metadata")
    observed = {}
    for line in (bundle / "SHA256SUMS").read_text().splitlines():
        match = re.fullmatch(r"([a-f0-9]{64})  ([A-Za-z0-9._-]+)", line)
        if not match or match[2] not in MEMBERS or match[2] in observed:
            raise ValueError("invalid installed checksums")
        observed[match[2]] = digest(bundle / match[2])
        if observed[match[2]] != match[1]:
            raise ValueError("installed payload differs from authenticated checksum")
    if set(observed) != MEMBERS - {"SHA256SUMS"}:
        raise ValueError("incomplete installed payload checksums")
    manifest = json.loads((bundle / "release.json").read_text())
    if (manifest["version"], manifest["os"], manifest["arch"]) != (version, system, arch):
        raise ValueError("installed manifest target differs")
    return observed


def hosted_guard(system, arch):
    if (os.environ.get("GITHUB_ACTIONS") != "true"
            or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted" or os.geteuid() == 0):
        raise ValueError("complete brew acceptance is restricted to ordinary GitHub-hosted disposable runners")
    actual_system = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system())
    actual_arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64"}.get(platform.machine())
    declared = {"ARM64": "arm64", "X64": "amd64"}.get(os.environ.get("RUNNER_ARCH"))
    if (actual_system, actual_arch, declared) != (system, arch, arch):
        raise ValueError("native uname and hosted runner architecture must match the requested target")
    if actual_system == "darwin":
        translated = subprocess.run(["sysctl", "-in", "sysctl.proc_translated"], capture_output=True, text=True)
        if translated.stdout.strip() == "1":
            raise ValueError("Rosetta execution does not count as native acceptance")


class Channel:
    def __init__(self, evidence, state=None):
        self.evidence = evidence
        self.state = state or {"commands": [], "errors": [], "owned": {}, "completed_sequence": False}
        self.env = dict(os.environ)
        self.env.update(HOMEBREW_NO_AUTO_UPDATE="1", HOMEBREW_NO_INSTALL_CLEANUP="1",
                        HOMEBREW_NO_AUTOREMOVE="1", HOMEBREW_NO_ANALYTICS="1", HOMEBREW_NO_ENV_HINTS="1",
                        HOMEBREW_CACHE=str(evidence / "private/cache"), HOMEBREW_TEMP=str(evidence / "private/tmp"),
                        XDG_CONFIG_HOME=str(evidence / "private/config"))

    def save(self):
        write_json(self.evidence / "state.json", self.state)

    def command(self, args, *, timeout=300, allowed=(0,)):
        number = len(self.state["commands"])
        log = self.evidence / f"command-{number:03d}.log"
        diagnostic = self.evidence / f"command-{number:03d}.stderr.log"
        record = {"args": [str(x) for x in args], "log": log.name, "stderr_log": diagnostic.name,
                  "started": time.time(), "exit_code": None}
        self.state["commands"].append(record)
        self.save()
        with log.open("wb") as output, diagnostic.open("wb") as errors:
            process = subprocess.Popen(record["args"], stdout=output, stderr=errors,
                                       env=self.env, start_new_session=True)
            try:
                process.wait(timeout=timeout)
            finally:
                if process.poll() is None:
                    os.killpg(process.pid, signal.SIGTERM)
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.wait()
                record.update(exit_code=process.returncode, ended=time.time(), sha256=digest(log),
                              stderr_sha256=digest(diagnostic))
                self.save()
        text = log.read_text(errors="replace")
        if process.returncode not in allowed:
            raise RuntimeError(f"command {number} exited {process.returncode}: {' '.join(record['args'])}; see {log.name}")
        return text.strip()

    def brew(self, *args, **kwargs):
        return self.command([self.state["brew"], *args], **kwargs)

    def observe(self):
        return {"taps": sorted(self.brew("tap").splitlines()),
                "formulae": sorted(self.brew("list", "--formula", "--versions").splitlines()),
                "services": json.loads(self.brew("services", "list", "--json")),
                "protected": snapshot_paths([Path(p) for p in self.state["protected_paths"]])}

    def claim_after(self, key, replacing=False):
        entry = self.state["owned"][key]
        observed = identity(Path(entry["path"]))
        if entry["identity"] is not None and observed != entry["identity"] and not replacing:
            raise ValueError(f"owned object changed unexpectedly: {entry['path']}")
        entry["identity"] = observed
        self.save()

    def own(self, key, path, kind, reference):
        if path.exists() or path.is_symlink():
            raise ValueError(f"pre-existing {kind} cannot be owned: {path}")
        self.state["owned"][key] = {"path": str(path), "kind": kind, "reference": reference,
                                    "identity": None, "intent": True}
        self.save()

    def cleanup(self):
        failures = []
        # The formula goes first; then only dependencies with no installed users.
        entries = list(self.state["owned"].items())
        entries.sort(key=lambda pair: (pair[1]["kind"] == "tap", pair[0] != FORMULA))
        pending = []
        for key, entry in entries:
            try:
                path = Path(entry["path"])
                if not require_owned(path, entry["identity"]):
                    continue
                if entry["kind"] == "tap":
                    pending.append((key, entry))
                    continue
                else:
                    if self.brew("uses", "--installed", "--recursive", entry["reference"]):
                        pending.append((key, entry))
                        continue
                    self.brew("uninstall", entry["reference"])
                if path.exists() or path.is_symlink():
                    raise ValueError(f"owned {entry['kind']} remains after cleanup")
            except Exception as error:
                failures.append(str(error))
        # Dependencies can themselves depend on another new dependency. Only
        # remove unused leaves, never a pre-existing or still-referenced package.
        while pending:
            progressed = False
            next_pending = []
            for key, entry in pending:
                try:
                    path = Path(entry["path"])
                    if not require_owned(path, entry["identity"]):
                        progressed = True
                        continue
                    if entry["kind"] == "tap":
                        if any(item[1]["kind"] == "formula" for item in pending):
                            next_pending.append((key, entry))
                            continue
                        self.brew("untap", entry["reference"])
                    elif self.brew("uses", "--installed", "--recursive", entry["reference"]):
                        next_pending.append((key, entry))
                        continue
                    else:
                        self.brew("uninstall", entry["reference"])
                    if path.exists() or path.is_symlink():
                        raise ValueError(f"owned {entry['kind']} remains after cleanup")
                    progressed = True
                except Exception as error:
                    failures.append(str(error))
            if next_pending and not progressed:
                failures.append("owned dependencies still have installed users; preserving them and the tap")
                break
            pending = next_pending
        if "baseline" in self.state:
            try:
                final = self.observe()
                self.state["final"] = final
                if final != self.state["baseline"]:
                    failures.append("tap/formula/service/protected-state baseline was not restored")
            except Exception as error:
                failures.append(str(error))
        private = self.evidence / "private"
        try:
            if private.is_symlink():
                raise ValueError("private cache/config directory was replaced; refusing removal")
            if private.exists():
                shutil.rmtree(private)
        except Exception as error:
            failures.append(str(error))
        self.state["cleanup_errors"] = failures
        self.state.setdefault("cleanup_attempts", []).append({"ended": time.time(), "errors": failures})
        self.state["passed"] = (self.state["completed_sequence"] and not self.state["errors"]
                                and not any(attempt["errors"] for attempt in self.state["cleanup_attempts"]))
        self.save()
        write_json(self.evidence / "summary.json", self.state)
        return not failures

    def run(self, checkout, system, arch):
        brew = shutil.which("brew")
        if brew is None:
            brew = next((str(p) for p in (Path("/home/linuxbrew/.linuxbrew/bin/brew"), Path("/opt/homebrew/bin/brew"), Path("/usr/local/bin/brew")) if p.is_file()), None)
        if brew is None:
            raise ValueError("hosted runner has no preinstalled Homebrew; no local bootstrap fallback")
        self.state.update(brew=brew, system=system, arch=arch, uname=list(platform.uname()),
                          runner={key: os.environ.get(key) for key in ("RUNNER_OS", "RUNNER_ARCH", "ImageOS", "ImageVersion")})
        home = Path.home()
        self.state["protected_paths"] = [str(p) for p in (home / ".shiki", home / ".homebrew/trust.json",
            home / ".config/homebrew/trust.json", home / "Library/LaunchAgents/dev.shikigami.agent.plist",
            home / ".config/systemd/user/shiki-agent.service", Path("/etc/shiki"), Path("/var/lib/shiki"),
            Path("/etc/systemd/system/shiki-daemon.service"))]
        for name in ("config", "cache", "tmp"):
            (self.evidence / "private" / name).mkdir(parents=True, mode=0o700)
        self.state["brew_version"] = self.brew("--version")
        self.state["baseline"] = self.observe()
        if TAP in self.state["baseline"]["taps"] or any(line.split()[0].split("/")[-1] == "shikigami" for line in self.state["baseline"]["formulae"]):
            raise ValueError("pre-existing public tap or Shikigami formula is preserved; disposable acceptance requires their absence")
        source_formula = checkout / "Formula/shikigami.rb"
        version, envelopes = formula_metadata(source_formula, checkout / "configs/release-signing.pub")
        commit = self.command(["git", "-C", str(checkout), "log", "-1", "--format=%H", "--", "Formula/shikigami.rb"])
        if not re.fullmatch(r"[0-9a-f]{40}", commit):
            raise ValueError("full formula history is required to pin the public tap commit")
        self.state.update(version=version, formula_sha256=digest(source_formula), tap_commit=commit,
                          trusted_public_sha256=digest(checkout / "configs/release-signing.pub"))
        shutil.copyfile(source_formula, self.evidence / "expected-formula.rb")
        shutil.copyfile(checkout / "configs/release-signing.pub", self.evidence / "trusted-release-signing.pub")
        repository = Path(self.brew("--repository"))
        tap_path = repository / "Library/Taps/ddsrem-dev/homebrew-shikigami"
        self.own(TAP, tap_path, "tap", TAP)
        try:
            self.brew("tap", TAP, ORIGIN)
        finally:
            self.claim_after(TAP)
        observed = Path(self.brew("--repository", TAP)).resolve()
        if observed != tap_path.resolve():
            raise ValueError("public tap path differs from the owned path")
        self.state["cloned_tap_head"] = self.command(["git", "-C", str(tap_path), "rev-parse", "HEAD"])
        origin = self.command(["git", "-C", str(tap_path), "remote", "get-url", "origin"])
        if origin.removesuffix(".git") != ORIGIN:
            raise ValueError("tap origin is not the public repository")
        self.command(["git", "-C", str(tap_path), "fetch", "--depth=1", "origin", commit])
        self.command(["git", "-C", str(tap_path), "checkout", "--detach", commit])
        if self.command(["git", "-C", str(tap_path), "rev-parse", "HEAD"]) != commit:
            raise ValueError("tap commit was not pinned")
        if digest(tap_path / "Formula/shikigami.rb") != self.state["formula_sha256"]:
            raise ValueError("actual public tap formula differs from trusted checkout")
        brew_major = re.match(r"Homebrew ([0-9]+)\.", self.state["brew_version"].splitlines()[0])
        if not brew_major:
            raise ValueError("cannot identify the Homebrew major version")
        if int(brew_major[1]) >= 7:
            self.brew("trust", "--formula", FORMULA)
        dependencies = self.brew("deps", "--formula", FORMULA).splitlines()
        baseline_names = {line.split()[0] for line in self.state["baseline"]["formulae"]}
        for reference in [FORMULA, *dependencies]:
            if not re.fullmatch(r"[A-Za-z0-9@+_.-]+(?:/[A-Za-z0-9@+_.-]+)*", reference):
                raise ValueError("unexpected formula dependency reference")
            if reference in baseline_names or reference.split("/")[-1] in baseline_names:
                continue
            self.own(reference, Path(self.brew("--cellar", reference)), "formula", reference)
        for command in ("install", "test", "reinstall", "test"):
            try:
                self.brew(command, FORMULA, timeout=600)
            finally:
                for key, entry in self.state["owned"].items():
                    if entry["kind"] == "formula":
                        self.claim_after(key, replacing=command == "reinstall")
            if command in ("install", "reinstall"):
                prefix = Path(self.brew("--prefix", FORMULA)).resolve()
                if not prefix.is_relative_to(Path(self.state["owned"][FORMULA]["path"])):
                    raise ValueError("installed prefix is outside the owned Cellar")
                checked = verify_bundle(prefix / "libexec/bundle", envelopes[f"{system}-{arch}"], version, system, arch)
                self.state.setdefault("bundle_checks", []).append(checked)
                public = prefix / "share/shikigami/release-signing.pub"
                if public.read_text().strip() != (checkout / "configs/release-signing.pub").read_text().strip():
                    raise ValueError("installed trust key changed")
                self.save()
        self.state["completed_sequence"] = True
        self.save()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("run", "cleanup"))
    parser.add_argument("--evidence", type=Path, required=True)
    parser.add_argument("--checkout", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--os", choices=("darwin", "linux"), required=True)
    parser.add_argument("--arch", choices=("amd64", "arm64"), required=True)
    args = parser.parse_args()
    if (os.environ.get("GITHUB_ACTIONS") != "true"
            or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted" or os.geteuid() == 0):
        raise ValueError("this command must not run Homebrew on a local or self-hosted machine")
    evidence = args.evidence.resolve()
    runner_temp = Path(os.environ["RUNNER_TEMP"]).resolve()
    if evidence == runner_temp or not evidence.is_relative_to(runner_temp):
        raise ValueError("evidence must be a private child of RUNNER_TEMP")
    if args.action == "cleanup":
        if not (evidence / "state.json").exists():
            return 0
        channel = Channel(evidence, json.loads((evidence / "state.json").read_text()))
        return 0 if channel.cleanup() else 1
    evidence.mkdir(mode=0o700)
    channel = Channel(evidence)
    def interrupted(signum, frame):
        raise InterruptedError(f"received signal {signum}")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        hosted_guard(args.os, args.arch)
        channel.run(args.checkout.resolve(), args.os, args.arch)
    except BaseException as error:
        channel.state["errors"].append(str(error))
    finally:
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        channel.cleanup()
    return 0 if channel.state.get("passed") else 1


if __name__ == "__main__":
    raise SystemExit(main())
