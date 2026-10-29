#!/usr/bin/env bash
set -euo pipefail

main() {
  shiki_activity_direction=upload
  if [[ $# == 1 && "$1" == --download ]]; then
    shiki_activity_direction=download
  elif [[ $# != 0 ]]; then
    echo 'Usage: test-launchd-activity.sh [--download]' >&2; exit 2
  fi
  [[ "$(uname -s)" == Darwin && "$(id -u)" != 0 ]] || { echo 'Run as the ordinary logged-in macOS user.' >&2; exit 1; }
  shiki_activity_repository="$(cd "$(dirname "$0")/.." && pwd)"
  shiki_activity_root="$(mktemp -d /tmp/shiki-launchd.XXXXXXXX)"
  shiki_activity_root="$(cd "$shiki_activity_root" && pwd -P)"
  shiki_activity_pid=''
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM HUP
  mkdir "$shiki_activity_root/source"
  tar -C "$shiki_activity_repository" --exclude='./.git' --exclude='./.tools' --exclude='./bin' --exclude='./dist' \
    -cf - . | tar -xf - -C "$shiki_activity_root/source"
  cd "$shiki_activity_root/source"
  go version
  go build -mod=readonly -o "$shiki_activity_root/processguard" ./test/fixtures/processguard
  go test -c -race -mod=readonly -tags=e2e -o "$shiki_activity_root/launchd-activity.test" ./test/e2e
  if [[ -n "${SHIKI_RUN_EVIDENCE_DIR:-}" ]]; then
    python3 - "$SHIKI_RUN_EVIDENCE_DIR" "$shiki_activity_root" "$shiki_activity_direction" <<'PY'
import hashlib,json,pathlib,sys
out,root=map(pathlib.Path,sys.argv[1:3])
direction=sys.argv[3]
source=root/'source'
files={str(p.relative_to(source)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(source.rglob('*')) if p.is_file()}
(out/'launchd-activity-source-hashes.json').write_text(json.dumps(files,indent=2)+'\n')
(out/'launchd-activity-runner.json').write_text(json.dumps({'root':str(root),'snapshot':str(source),'test_binary':str(root/'launchd-activity.test'),'test_sha256':hashlib.sha256((root/'launchd-activity.test').read_bytes()).hexdigest(),'mode':'native LaunchAgent activity; TLS daemon fixture','sync_direction':direction,'replaced_host':'agent'},indent=2)+'\n')
PY
  fi
  # Python owns/reaps the exact child. Bash can reap asynchronously, so a
  # delayed raw signal to its remembered child PID would not be safe.
  trap - EXIT INT TERM HUP
  exec python3 - "$shiki_activity_root" "$shiki_activity_direction" <<'PYRUN'
import json,os,pathlib,signal,subprocess,sys,time
root=pathlib.Path(sys.argv[1])
child=None
interrupted=[]
def interrupt(signum,frame):
    interrupted.append(signum)
    if child is not None and child.poll() is None:
        child.send_signal(signal.SIGTERM)
for sig in (signal.SIGINT,signal.SIGTERM,signal.SIGHUP):
    signal.signal(sig,interrupt)
result=1
try:
    env=dict(os.environ,SHIKI_LAUNCHD_ACTIVITY_ROOT=str(root),SHIKI_LAUNCHD_ACTIVITY_DIRECTION=sys.argv[2])
    child=subprocess.Popen([str(root/'launchd-activity.test'),'-test.v',
        '-test.run=^TestNativeLaunchAgentUpgradeWithActiveTransfers$','-test.timeout=14m'],env=env)
    deadline=time.monotonic()+14*60+20
    cancel_deadline=None
    while child.poll() is None:
        if interrupted and cancel_deadline is None:
            child.terminate()
            cancel_deadline=time.monotonic()+50
        if time.monotonic()>deadline or (cancel_deadline and time.monotonic()>cancel_deadline):
            child.kill()
            child.wait(timeout=5)
            break
        time.sleep(0.1)
    result=128+interrupted[0] if interrupted else (child.returncode if child.returncode>=0 else 1)
finally:
    if child is not None and child.poll() is None:
        child.terminate()
        try: child.wait(timeout=50)
        except subprocess.TimeoutExpired:
            child.kill()
            try: child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                print('Test child remains after KILL; continuing independent cleanup',file=sys.stderr)
    raw={'test_returncode':None if child is None else child.returncode,'interruption_signals':interrupted,'runner_result_before_cleanup':result}
    print('LAUNCHD_ACTIVITY_RUNNER_RESULT '+json.dumps(raw),flush=True)
    if os.environ.get('SHIKI_RUN_EVIDENCE_DIR'):
        try:
            (pathlib.Path(os.environ['SHIKI_RUN_EVIDENCE_DIR'])/'launchd-activity-result.json').write_text(json.dumps(raw,indent=2)+'\n')
        except Exception as error:
            print(f'Cannot retain runner result: {error}',file=sys.stderr);result=1
    try:
        cleanup=subprocess.run([sys.executable,str(root/'source/scripts/test-launchd-activity-cleanup.py'),str(root)],timeout=90)
        cleanup_result_path=root/'activity-cleanup-result.json'
        cleanup_result=json.loads(cleanup_result_path.read_text()) if cleanup_result_path.exists() else {'errors':['cleanup result absent']}
        if os.environ.get('SHIKI_RUN_EVIDENCE_DIR'):
            (pathlib.Path(os.environ['SHIKI_RUN_EVIDENCE_DIR'])/'launchd-activity-cleanup.json').write_text(json.dumps(cleanup_result,indent=2)+'\n')
        if cleanup.returncode or cleanup_result.get('errors'):
            result=1
        elif result==0 and cleanup_result.get('fallback_actions'):
            print('Application success required fallback cleanup; rejecting acceptance',file=sys.stderr)
            result=1
    except Exception as error:
        print(f'Cleanup incomplete: {error}',file=sys.stderr);result=1
    if result==0:
        import shutil
        shutil.rmtree(root)
    else:
        print(f'Failed fixture retained for inspection: {root}',file=sys.stderr)
sys.exit(result)
PYRUN
}
cleanup() {
  shiki_activity_result=$?
  trap - EXIT INT TERM HUP
  # Before the Python owner starts no test process or job has been created.
  if (( shiki_activity_result == 0 )); then
    rm -rf "$shiki_activity_root"
  else
    echo "Build/setup fixture retained: $shiki_activity_root" >&2
  fi
  exit "$shiki_activity_result"
}

main "$@"; exit "$?"
