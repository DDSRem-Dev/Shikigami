#!/usr/bin/env python3
"""Real native systemd acceptance, only on disposable GitHub-hosted runners.

The scenario runs as a newly allocated account. Never reuse UID1000 or the runner
account, and never stop a preexisting Shiki service. No Docker/mount/PID namespace
is introduced. Only the standard daemon unit is outside this run's private tree.
"""
import argparse
import importlib.util
import grp
import json
import os
from pathlib import Path
import pwd
import re
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True
from platform_support import hosted_guard, write_json, safe_script_failure
from platform_resources import copy_build_cache, go_environment

UNIT = Path("/etc/systemd/system/shiki-daemon.service")
ENABLED = Path("/etc/systemd/system/multi-user.target.wants/shiki-daemon.service")


def run(args, *, check=True, timeout=60):
    result = subprocess.run([str(arg) for arg in args], capture_output=True, text=True, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError("native maintenance command failed: " + Path(str(args[0])).name)
    return result


def account_step(result, name, operation):
    """Record only fixed setup phases and outcomes; command output stays private."""
    result["phase"] = name
    record = {"phase": name, "outcome": "failed"}
    result.setdefault("account_setup", []).append(record)
    started = time.monotonic()
    try:
        response = operation()
        if response is not None and response.returncode:
            record["outcome"] = "signal" if response.returncode < 0 else "nonzero_exit"
            raise RuntimeError("native account setup command failed")
        record["outcome"] = "passed"
        return response
    except subprocess.TimeoutExpired:
        record["outcome"] = "timeout"
        raise
    finally:
        record["seconds"] = round(time.monotonic() - started, 3)


def require_unused(lookup, name):
    try:
        lookup(name)
    except KeyError:
        return
    raise RuntimeError("refusing to reuse a native acceptance account or group")


def owned_processes(uid):
    result = {}
    for entry in Path("/proc").iterdir():
        if not entry.name.isdigit():
            continue
        try:
            if entry.stat().st_uid == uid:
                fields = (entry / "stat").read_text().rsplit(")", 1)[1].split()
                result[int(entry.name)] = fields[19]  # kernel starttime, not a reusable bare PID
        except (FileNotFoundError, ProcessLookupError):
            pass
    return result


def kill_owned(uid, sig):
    for pid, started in owned_processes(uid).items():
        if owned_processes(uid).get(pid) == started:
            try:
                os.kill(pid, sig)
            except ProcessLookupError:
                pass


def validate_layout(layout):
    """Reject cross-user/path injection before inside() performs privileged writes."""
    hosted_guard()
    root = Path(layout["root"])
    if root.parent != Path("/opt") or not re.fullmatch(r"shiki-platform-systemd-[a-z0-9_]+", root.name):
        raise ValueError("unexpected native systemd root")
    if root.is_symlink() or root.resolve() != root or root.stat().st_uid != 0:
        raise ValueError("native systemd root must be a canonical root-owned directory")
    account = pwd.getpwnam(layout["user"])
    if not re.fullmatch(r"shikici[0-9]+", account.pw_name) or account.pw_uid < 1000:
        raise ValueError("native acceptance requires a fresh dedicated ordinary account")
    if (account.pw_uid, account.pw_gid, account.pw_dir) != (layout["uid"], layout["gid"], layout["home"]):
        raise ValueError("native account identity differs from layout")
    for key in ("home", "work", "build", "cache", "gopath", "config", "state", "prefix", "commands", "modules"):
        path = Path(layout[key])
        if path.parent != root or path.is_symlink() or path.resolve() != path:
            raise ValueError("native state/build/install paths must be direct children of the owned root")
    if Path(layout["home"]).stat().st_uid != account.pw_uid:
        raise ValueError("native home must belong to the dedicated account")
    if not re.fullmatch(r"127\.0\.0\.1:[0-9]{1,5}", layout["address"]) or not 1024 <= int(layout["address"].split(":")[1]) <= 65535:
        raise ValueError("acceptance listener must use an unprivileged loopback port")
    go_environment({"package_parallelism": layout["parallelism"], "gomaxprocs": layout["gomaxprocs"]})
    if layout["build_timeout"] != 900:
        raise ValueError("unexpected native build time budget")


def preflight(source, go, modules, cache, evidence):
    hosted_guard()
    if os.geteuid() != 0 or Path("/proc/1/comm").read_text().strip() != "systemd":
        raise RuntimeError("native maintenance requires root under real systemd PID 1")
    if Path("/.dockerenv").exists() or run(["systemd-detect-virt", "--container"], check=False).returncode == 0:
        # WSL is a real supported client runtime but systemd-detect-virt reports
        # it as a container. Require its actual kernel when allowing that case.
        detected = run(["systemd-detect-virt", "--container"], check=False).stdout.strip()
        if detected != "wsl" or "microsoft" not in os.uname().release.lower():
            raise RuntimeError("container execution is not native platform acceptance")
    for path in (source, go, modules, cache):
        if not path.is_absolute() or path.is_symlink() or not path.exists() or path.resolve() != path:
            raise ValueError("source, Go and caches require existing canonical paths")
    if go.name != "go" or not (source / "scripts/test-systemd.py").is_file():
        raise ValueError("unexpected Go executable or source tree")
    if UNIT.exists() or UNIT.is_symlink() or ENABLED.exists() or ENABLED.is_symlink() or UNIT.with_name(UNIT.name + ".d").exists():
        raise RuntimeError("refusing an existing daemon service or drop-ins")
    loaded = run(["systemctl", "show", "shiki-daemon.service", "-p", "LoadState", "--value"], check=False).stdout.strip()
    if loaded != "not-found":
        raise RuntimeError("refusing a preloaded daemon service")
    if evidence.exists() or evidence.is_symlink() or not evidence.parent.is_dir():
        raise ValueError("native evidence must be a new directory under the private evidence root")
    evidence.mkdir(mode=0o700)


def cleanup(root, username, account, *, created, group, tmux_owned=False):
    failures = []
    def attempt(label, action):
        try:
            action()
        except Exception:
            failures.append(label)
    if created:
        if account is None:
            try:
                account = pwd.getpwnam(username)
            except KeyError:
                pass
        if account is not None:
            current = pwd.getpwnam(username)
            if current != account or current.pw_dir != str(root / "home") or current.pw_uid < 1000 or group is None or current.pw_gid != group.gr_gid:
                raise RuntimeError("account ownership changed; refusing process/user cleanup")
    program = str(root / "install/current/shiki-daemon")
    file_owned = UNIT.exists() or UNIT.is_symlink()
    if file_owned and (UNIT.is_symlink() or program not in UNIT.read_text()):
        raise RuntimeError("daemon service identity changed; not removed")
    # Interrupted uninstall can remove the file while leaving the loaded unit.
    loaded = run(["systemctl", "show", "shiki-daemon.service", "-p", "ExecStart", "--value"], check=False).stdout.strip()
    if loaded and program not in loaded:
        raise RuntimeError("loaded daemon service identity changed; not stopped")
    if file_owned or loaded:
        attempt("daemon stop", lambda: run(["systemctl", "stop", "shiki-daemon.service"]))
        run(["systemctl", "disable", "shiki-daemon.service"], check=False)
        if UNIT.exists():
            attempt("daemon unit removal", UNIT.unlink)
    if ENABLED.exists() or ENABLED.is_symlink():
        if ENABLED.is_symlink() and ENABLED.resolve() == UNIT:
            attempt("daemon enable-link removal", ENABLED.unlink)
        else:
            failures.append("daemon enable-link identity changed")
    if file_owned or loaded:
        attempt("daemon reload", lambda: run(["systemctl", "daemon-reload"]))
        if run(["systemctl", "is-active", "--quiet", "shiki-daemon.service"], check=False).returncode == 0:
            failures.append("daemon service remains active")
    staged = UNIT.with_name("." + UNIT.name + ".install")
    if staged.exists() and not staged.is_symlink() and staged.stat().st_size <= 65536:
        if str(root / "install") + "/current/shiki-daemon" in staged.read_text():
            attempt("staged daemon unit removal", staged.unlink)
    if created and account is not None:
        # This UID was created by this run and has no unrelated processes.
        run(["loginctl", "disable-linger", username], check=False)
        run(["loginctl", "terminate-user", username], check=False)
        run(["systemctl", "stop", f"user@{account.pw_uid}.service"], check=False)
        for sig in (signal.SIGTERM, signal.SIGKILL):
            kill_owned(account.pw_uid, sig)
            deadline = time.monotonic() + 10
            while owned_processes(account.pw_uid) and time.monotonic() < deadline:
                time.sleep(0.1)
        if owned_processes(account.pw_uid):
            failures.append("owned processes remain")
        else:
            attempt("account removal", lambda: run(["userdel", username]))
        try:
            pwd.getpwnam(username)
        except KeyError:
            pass
        else:
            failures.append("owned account remains")
        if Path("/var/lib/systemd/linger", username).exists():
            failures.append("owned linger remains")
    if tmux_owned and account is not None:
        tmux = Path(f"/tmp/tmux-{account.pw_uid}")
        if tmux.exists() or tmux.is_symlink():
            if tmux.is_symlink() or tmux.stat().st_uid != account.pw_uid or not tmux.is_dir():
                failures.append("tmux directory ownership changed")
            else:
                for item in tmux.iterdir():
                    info = item.lstat()
                    if item.name == f"shiki-{account.pw_uid}" and info.st_uid == account.pw_uid and stat.S_ISSOCK(info.st_mode):
                        attempt("owned tmux socket removal", item.unlink)
                    else:
                        failures.append("unrecognized object in dedicated tmux directory")
                if not list(tmux.iterdir()):
                    attempt("owned tmux directory removal", tmux.rmdir)
    if group is not None:
        try:
            current_group = grp.getgrnam(username)
        except KeyError:
            current_group = None
        if current_group is not None:
            if current_group.gr_gid != group.gr_gid or current_group.gr_mem or any(a.pw_gid == group.gr_gid for a in pwd.getpwall()):
                failures.append("group ownership changed; not removed")
            else:
                attempt("group removal", lambda: run(["groupdel", username]))
        try:
            grp.getgrnam(username)
        except KeyError:
            pass
        else:
            failures.append("owned group remains")
    if not failures:
        attempt("owned tree removal", lambda: shutil.rmtree(root))
        if root.exists():
            failures.append("owned tree remains")
    if UNIT.exists() or UNIT.is_symlink():
        failures.append("daemon unit remains")
    if failures:
        raise RuntimeError("; ".join(failures))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("source", "go", "modules", "cache", "evidence"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--parallelism", type=int, choices=(1, 2), required=True)
    parser.add_argument("--gomaxprocs", type=int, choices=(1, 2), required=True)
    args = parser.parse_args()
    preflight(args.source, args.go, args.modules, args.cache, args.evidence)
    owner = args.evidence.parent.stat()
    # The installer's system command directory requires protected ancestors;
    # /tmp is world-writable even when this run's immediate root is private.
    root = Path(tempfile.mkdtemp(prefix="shiki-platform-systemd-", dir="/opt"))
    os.chmod(root, 0o755)  # private descendants; service/build paths need traversal
    username = "shikici" + str(os.getpid())
    account, group, created, tmux_owned = None, None, False, False
    result = {"state": "failed", "cleanup": "pending", "phase": "setup-account", "runtime": "native systemd PID 1",
              "transport": "loopback TLS fixture; external connectivity not asserted",
              "claude": "explicit shell fixture; no real Claude login"}
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, interrupted)
    try:
        account_step(result, "lookup-account", lambda: require_unused(pwd.getpwnam, username))
        account_step(result, "lookup-group", lambda: require_unused(grp.getgrnam, username))
        # Avoid a signal between successful OS mutation and ownership recording.
        previous_mask = signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGINT, signal.SIGTERM})
        try:
            account_step(result, "create-group", lambda: run(["groupadd", username], check=False))
            group = grp.getgrnam(username)
            created = True
            account_step(result, "create-user", lambda: run(["useradd", "--gid", str(group.gr_gid), "--create-home", "--home-dir", root / "home", "--shell", "/bin/bash", username], check=False))
            account = pwd.getpwnam(username)
        finally:
            signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)
        os.chmod(root / "home", 0o700)
        tmux = Path(f"/tmp/tmux-{account.pw_uid}")
        if tmux.exists() or tmux.is_symlink():
            raise RuntimeError("refusing a UID with a preexisting tmux directory")
        tmux_owned = True
        for name in ("cache", "gopath"):
            (root / name).mkdir(mode=0o700)
            os.chown(root / name, account.pw_uid, account.pw_gid)
        # The runner's HOME can be 0700. Never chmod it or share its writable
        # cache with another UID: copy only modules into this run's owned tree.
        result["phase"] = "copy-modules"
        shutil.copytree(args.modules, root / "modules")
        run(["chown", "-R", f"{account.pw_uid}:{account.pw_gid}", root / "modules"], timeout=120)
        result["phase"] = "copy-build-cache"
        result["build_cache_copy"] = copy_build_cache(args.cache, root / "cache")
        run(["chown", "-R", f"{account.pw_uid}:{account.pw_gid}", root / "cache"], timeout=120)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            address = "127.0.0.1:" + str(listener.getsockname()[1])
        layout = dict(root=str(root), user=username, uid=account.pw_uid, gid=account.pw_gid,
                      evidence=str(args.evidence / "scenario"), source=str(args.source),
                      go_bin=str(args.go.parent), modules=str(root / "modules"), address=address, build_timeout=900,
                      transport=result["transport"], parallelism=args.parallelism, gomaxprocs=args.gomaxprocs)
        layout.update({name: str(root / child) for name, child in {
            "home": "home", "work": "work", "build": "build", "cache": "cache", "gopath": "gopath",
            "config": "config", "state": "state", "prefix": "install", "commands": "commands"}.items()})
        result["phase"] = "check-layout"
        validate_layout(layout)
        # Confirm the ordinary build account can reach immutable shared modules/Go.
        run(["runuser", "-u", username, "--", "test", "-r", root / "modules/cache/download"])
        run(["runuser", "-u", username, "--", args.go, "version"])
        spec = importlib.util.spec_from_file_location("systemd_scenario", args.source / "scripts/test-systemd.py")
        scenario = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(scenario)
        original_phase = scenario.phase
        phases = {
            "Building isolated release systemd-test-a": "build-bundle-a",
            "Building isolated release systemd-test-b": "build-bundle-b",
            "Activating rendered daemon unit through real installer": "activate-daemon",
            "Checking actual stop/restart, SIGKILL recovery and worker reap": "service-recovery",
            "Checking three-crash guard, explicit recovery and return to service ownership": "crash-guard",
            "Starting ordinary-user active transfer probe": "active-transfer-probe",
            "Checking user-service upgrade, rollback, uninstall and preserved credentials": "agent-maintenance",
            "Checking service-managed upgrade, rollback and preserved identity": "daemon-maintenance",
            "Checking uninstall preserves data, credentials and tmux": "uninstall-retention",
            "Service lifecycle PASS; active daemon/agent/tmux/workers remain for systemd shutdown": "scenario-complete",
        }
        def phase(message):
            result["phase"] = phases.get(message, "service-scenario")
            original_phase(message)
        scenario.phase = phase
        result["phase"] = "service-scenario"
        scenario.inside(layout)
        raw = json.loads((args.evidence / "scenario/evidence.json").read_text())
        # Fixed fields only. Never export readiness/config/token or raw journals.
        result.update(state="passed", operations=len(raw["operations"]),
                      active_upgrade_boundaries=sorted(raw["active_upgrade_transfers"]),
                      uid=account.pw_uid, systemd=raw["systemd"], go=raw["go"], platform=raw["platform"],
                      uninstall_retains_data=raw["uninstall_preserves_config_state_project_token_tmux"],
                      crash_loop_guard=raw["user_agent_crash_loop"]["reason"])
    except BaseException as error:
        result.update(safe_script_failure(error))
        raise
    finally:
        for sig in (signal.SIGINT, signal.SIGTERM):
            signal.signal(sig, signal.SIG_IGN)
        try:
            cleanup(root, username, account, created=created, group=group, tmux_owned=tmux_owned)
            result["cleanup"] = "passed"
        except Exception as error:
            result["cleanup"] = "failed"
            result["cleanup_error"] = str(error)
            result["state"] = "failed"
        write_json(args.evidence / "result.json", result)
        os.chown(args.evidence, owner.st_uid, owner.st_gid)
        os.chown(args.evidence / "result.json", owner.st_uid, owner.st_gid)
    if result["state"] != "passed" or result["cleanup"] != "passed":
        raise RuntimeError("native systemd acceptance or owned cleanup failed")


if __name__ == "__main__":
    try:
        main()
    except BaseException:
        # Detailed scenario exceptions can contain readiness/credential data.
        # The caller keeps this output private and publishes only result.json.
        import traceback
        traceback.print_exc()
        sys.exit(1)
