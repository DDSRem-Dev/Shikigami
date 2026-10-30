#!/usr/bin/env python3
"""Optional hosted-CI Go caches; never cache acceptance results or runtime state."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
from platform_support import GO_VERSION, command, hosted_guard

ROOT = Path(__file__).resolve().parent.parent
GIB = 1024 ** 3
BUILD_LIMIT = 8 * GIB
MODULE_LIMIT = 4 * GIB
FILE_LIMIT = 250_000
SCAN_SECONDS = 15
CACHE_ENTRY = re.compile(r"[0-9a-f]{64}-[ad]")
ROOT_DOCUMENTS = {"README.md", "AGENTS.md", "CONTRIBUTING.md", "SECURITY.md", "CHANGELOG.md"}


def digest_files(files):
    digest = hashlib.sha256()
    for name, value in sorted(files.items()):
        digest.update(name.encode() + b"\0" + value.encode() + b"\0")
    return digest.hexdigest()


def input_hashes(repository):
    # Include untracked, nonignored inputs too: creating a new Go/embedded file
    # during a job must invalidate saving, not silently reuse an old source key.
    names = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        cwd=repository, timeout=30).split(b"\0")
    contents, hashes = {}, {}
    for raw in sorted(set(names)):
        if not raw:
            continue
        name = os.fsdecode(raw)
        path = repository / name
        if path.is_symlink() or not path.is_file() or path.resolve() != path:
            raise ValueError("cache source inputs must be regular files")
        data = path.read_bytes()
        contents[name] = data
        hashes[name] = hashlib.sha256(data).hexdigest()
    # Everything except known top-level documentation participates, including
    # non-Go embed assets, scripts, fixtures, build tags and workflow inputs.
    # A root Go package can embed those documents; conservatively retain all
    # documents if such a directive exists, without approximating Go glob rules.
    root_embed = any(name.endswith(".go") and
                     (name.startswith("docs/") or "/" not in name and
                      re.search(rb"(?m)^\s*//go:embed\s", data))
                     for name, data in contents.items())
    source = {name: value for name, value in hashes.items()
              if root_embed or not (name.startswith("docs/") or name in ROOT_DOCUMENTS)}
    dependencies = {name: value for name, value in hashes.items()
                    if Path(name).name in ("go.mod", "go.sum", "go.work", "go.work.sum") or
                    name == "scripts/tool-versions.env"}
    # Versioned tool invocations are dependencies too; unrelated workflow
    # changes should not cause a fresh multi-gigabyte module archive.
    tools = sorted(set(re.findall(rb"[A-Za-z0-9./_-]+@v[0-9][A-Za-z0-9.+_-]*",
                                  contents.get(".github/workflows/ci.yml", b""))))
    dependencies["versioned-ci-tools"] = hashlib.sha256(b"\n".join(tools)).hexdigest()
    if "go.mod" not in dependencies or "go.sum" not in dependencies:
        raise ValueError("root module inputs unavailable")
    return {"source": digest_files(source), "dependencies": digest_files(dependencies),
            "identity": digest_files(hashes)}


def cache_keys(role, runner, target_os, target_arch, go, hashes):
    if role not in ("native", "compile") or not re.fullmatch(r"[A-Za-z0-9.-]{1,64}", runner):
        raise ValueError("unsupported cache role or runner")
    if target_os not in ("linux", "darwin") or target_arch not in ("amd64", "arm64"):
        raise ValueError("unsupported cache target")
    if go["GOVERSION"] != GO_VERSION or go["GOHOSTOS"] not in ("linux", "darwin") or go["GOHOSTARCH"] not in ("amd64", "arm64"):
        raise ValueError("unexpected cache toolchain")
    if role == "native" and (go["GOHOSTOS"], go["GOHOSTARCH"]) != (target_os, target_arch):
        raise ValueError("native cache target differs from host")
    host = f"{go['GOHOSTOS']}-{go['GOHOSTARCH']}-{runner}-{GO_VERSION}"
    modules = f"shiki-mod-v1-{host}-{hashes['dependencies']}"
    # Compile uses CGO_ENABLED=0; native tests keep their actual CGO setting.
    cgo = "0" if role == "compile" else go["CGO_ENABLED"]
    if cgo not in ("0", "1"):
        raise ValueError("unexpected CGO setting")
    build = f"shiki-build-v1-{role}-{host}-{target_os}-{target_arch}-cgo{cgo}-{hashes['dependencies']}-"
    source = build + hashes["source"] + "-"
    return {"modules_key": modules, "build_prefix": build, "build_source_prefix": source,
            "build_warm_key": source + "warm", "build_partial_key": source + "partial"}


def cache_path(value, repository):
    path = Path(value)
    if not path.is_absolute() or str(path) != str(path.resolve()) or "\n" in value or "\r" in value:
        raise ValueError("Go cache path must be canonical")
    if path in (Path("/"), Path.home(), repository, Path(os.environ["RUNNER_TEMP"]).resolve()) or path.is_relative_to(repository) or repository.is_relative_to(path):
        raise ValueError("Go cache must be a dedicated directory outside source")
    if path.exists() and (not path.is_dir() or path.stat().st_uid != os.getuid()):
        raise ValueError("Go cache must be owned by the runner")
    return path


def inventory(path, byte_limit, *, files_limit=FILE_LIMIT, seconds=SCAN_SECONDS):
    """Bounded stat-only inventory. Never read/archive/delete cache payloads."""
    started = time.monotonic()
    result = {"eligible": False, "reason": "missing", "bytes": 0, "files": 0, "entries": {}}
    if not path.is_dir():
        return result
    def expired():
        return time.monotonic() - started >= seconds
    def fail(error):
        raise error
    for directory, directories, files in os.walk(path, followlinks=False, onerror=fail):
        for name in [*directories, *files]:
            if expired():
                result["reason"] = "scan_time_limit"
                return result
            child = Path(directory) / name
            info = child.lstat()
            if stat.S_ISLNK(info.st_mode) or info.st_uid != os.getuid():
                result["reason"] = "unsafe_entry"
                return result
            if stat.S_ISDIR(info.st_mode):
                continue
            if not stat.S_ISREG(info.st_mode):
                result["reason"] = "unsafe_entry"
                return result
            result["files"] += 1
            result["bytes"] += info.st_size
            if result["files"] > files_limit or result["bytes"] > byte_limit:
                result["reason"] = "size_limit"
                return result
            if CACHE_ENTRY.fullmatch(name):
                result["entries"][str(child.relative_to(path))] = info.st_size
        if expired():
            result["reason"] = "scan_time_limit"
            return result
    result["seconds"] = round(time.monotonic() - started, 3)
    result["eligible"] = result["files"] > 0
    result["reason"] = "within_budget" if result["eligible"] else "empty"
    return result


def public_inventory(value):
    return {key: item for key, item in value.items() if key != "entries"}


def save_plan(state, current_hashes, *, build_started, warm, build, modules):
    result = {"save_build": False, "save_modules": False, "build_save_key": "", "reason": "no_build"}
    if current_hashes != state["hashes"]:
        result["reason"] = "source_changed"
        return result
    if not build_started:
        return result
    result["reason"] = "checked"
    result["save_modules"] = modules["eligible"] and not state["modules_hit"]
    if state["build_hit"] or not build["eligible"] or not state["baseline_eligible"]:
        return result
    old = state["baseline_entries"]
    new_artifact = any(old.get(name) != size for name, size in build["entries"].items())
    # Promoting an existing partial cache after a successful full run is useful
    # even if no additional compilation was necessary. It records no test pass
    # for consumers: every test still executes with -count=1.
    promote = warm and state["matched_key"] == state["keys"]["build_partial_key"]
    if new_artifact or promote:
        result["save_build"] = True
        result["build_save_key"] = state["keys"]["build_warm_key" if warm else "build_partial_key"]
    return result


def write_state(path, value):
    temporary = path.with_suffix(".tmp")
    with temporary.open("x", encoding="utf-8") as stream:
        os.chmod(temporary, 0o600)
        json.dump(value, stream, sort_keys=True)
        stream.write("\n")
    temporary.replace(path)


def outputs(values):
    with Path(os.environ["GITHUB_OUTPUT"]).open("a") as stream:
        for key, value in values.items():
            value = str(value).lower() if isinstance(value, bool) else str(value)
            if "\n" in value or "\r" in value:
                raise ValueError("multiline cache output rejected")
            stream.write(f"{key}={value}\n")


def state_file():
    temporary = Path(os.environ["RUNNER_TEMP"]).resolve()
    directory = temporary / "shiki-go-cache"
    if directory.is_symlink():
        raise ValueError("cache state directory cannot be a symlink")
    directory.mkdir(mode=0o700, exist_ok=True)
    if directory.stat().st_uid != os.getuid() or directory.stat().st_mode & 0o077:
        raise ValueError("cache state must be private and owned")
    path = directory / "state.json"
    if path.is_symlink():
        raise ValueError("cache state cannot be a symlink")
    return path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("stage", choices=("prepare", "baseline", "plan"))
    parser.add_argument("--role", choices=("native", "compile"))
    parser.add_argument("--runner")
    parser.add_argument("--target-os", choices=("linux", "darwin"))
    parser.add_argument("--target-arch", choices=("amd64", "arm64"))
    args = parser.parse_args()
    hosted_guard()
    path = state_file()
    if args.stage == "prepare":
        if path.exists():
            raise ValueError("refusing earlier cache state")
        hashes = input_hashes(ROOT)
        go = json.loads(command(["go", "env", "-json", "GOVERSION", "GOHOSTOS", "GOHOSTARCH", "CGO_ENABLED", "GOCACHE", "GOMODCACHE"], cwd=ROOT))
        keys = cache_keys(args.role, args.runner, args.target_os, args.target_arch, go, hashes)
        build = cache_path(go["GOCACHE"], ROOT)
        modules = cache_path(go["GOMODCACHE"], ROOT)
        if build.is_relative_to(modules) or modules.is_relative_to(build):
            raise ValueError("Go cache directories must be separate")
        state = {"hashes": hashes, "keys": keys, "build_path": str(build), "modules_path": str(modules)}
        write_state(path, state)
        outputs({**keys, "build_path": build, "modules_path": modules})
        return
    state = json.loads(path.read_text())
    build_path = cache_path(state["build_path"], ROOT)
    module_path = cache_path(state["modules_path"], ROOT)
    if args.stage == "baseline":
        state["build_hit"] = os.environ.get("SHIKI_BUILD_CACHE_HIT") == "true"
        state["modules_hit"] = os.environ.get("SHIKI_MODULE_CACHE_HIT") == "true"
        state["matched_key"] = os.environ.get("SHIKI_BUILD_CACHE_MATCHED", "")
        before = {"eligible": True, "entries": {}}
        if not state["build_hit"]:
            before = inventory(build_path, BUILD_LIMIT)
            # An absent or empty cache is a valid baseline for a cold build.
            before["eligible"] = before["eligible"] or before["reason"] in ("missing", "empty")
        state["baseline_eligible"] = before["eligible"]
        state["baseline_entries"] = before["entries"]
        write_state(path, state)
        return
    hashes = input_hashes(ROOT)
    started = os.environ.get("SHIKI_CACHE_BUILD_OUTCOME") in ("success", "failure")
    warm = os.environ.get("SHIKI_CACHE_ACCEPTANCE_OUTCOME") == "success"
    empty = {"eligible": False, "entries": {}, "reason": "not_needed"}
    build, modules = empty, empty
    if hashes == state["hashes"] and started:
        if not state["build_hit"] and state["baseline_eligible"]:
            build = inventory(build_path, BUILD_LIMIT)
        if not state["modules_hit"]:
            modules = inventory(module_path, MODULE_LIMIT)
    # Recheck after the bounded scans too; never save under an obsolete source key.
    result = save_plan(state, input_hashes(ROOT), build_started=started, warm=warm, build=build, modules=modules)
    write_state(path.parent / "save-plan.json", {**result, "build": public_inventory(build), "modules": public_inventory(modules)})
    outputs(result)
    print(json.dumps({**result, "build": public_inventory(build), "modules": public_inventory(modules)}, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Cache failure is optional. Never print arbitrary paths, commands or
        # exception bodies from the runner; workflow gates require step success.
        print("Optional Go cache preparation failed; continuing without cache save.", file=sys.stderr)
        sys.exit(1)
