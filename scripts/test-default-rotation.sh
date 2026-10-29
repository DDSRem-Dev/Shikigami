#!/usr/bin/env bash
set -euo pipefail

# Native environment only. run-acceptance.py owns source snapshots, Linux
# containers, outer timeouts, signals and post-exit evidence.
main() {
  local shiki_repository shiki_mode shiki_test shiki_timeout shiki_binary
  shiki_repository=$(cd "$(dirname "$0")/.." && pwd)
  case "${1:-}" in
    '') [[ $# == 0 ]] || return 2
      shiki_mode=observe
      shiki_test=TestDefaultCredentialRotation
      shiki_timeout=13h
      ;;
    --preflight) [[ $# == 1 ]] || return 2
      shiki_mode=preflight
      shiki_test=TestDefaultCredentialRotationPreflight
      shiki_timeout=8m
      echo 'PREFLIGHT ONLY: NOT_12H_ACCEPTANCE. No credential clock or TTL is changed.'
      ;;
    *) echo 'Usage: test-default-rotation.sh [--preflight]' >&2; return 2 ;;
  esac
  [[ $(id -u) != 0 ]] || { echo 'An ordinary user is required; root skips are not acceptance.' >&2; return 2; }
  command -v tmux >/dev/null || { echo 'tmux is required.' >&2; return 1; }
  cd "$shiki_repository"
  # Bound cold-build concurrency on shared developer/container hosts. The
  # harness's nested go builds inherit this; shipped process scheduling does not.
  export GOFLAGS="${GOFLAGS:+$GOFLAGS }-p=1"
  shiki_default_directory=$(mktemp -d "${TMPDIR:-/tmp}/shiki-default-runner.XXXXXX")
  trap 'exit 130' INT
  trap 'exit 143' TERM HUP
  trap 'rm -rf "$shiki_default_directory"' EXIT
  shiki_binary="$shiki_default_directory/default-rotation.test"
  go version
  go test -c -mod=readonly -tags=e2e -o "$shiki_binary" ./test/e2e
  if [[ -n "${SHIKI_RUN_EVIDENCE_DIR:-}" ]]; then
    python3 - "$SHIKI_RUN_EVIDENCE_DIR" "$shiki_mode" "$shiki_binary" "$shiki_timeout" <<'PY'
import hashlib,json,os,pathlib,sys
root,mode,binary,timeout=sys.argv[1:]
value={"mode":mode,"test_binary":binary,"sha256":hashlib.sha256(pathlib.Path(binary).read_bytes()).hexdigest(),"build_mode":"ordinary","build_parallelism":1,"go_timeout":timeout,"production_credential_lifetime":"12h","natural_rotation_claim_allowed":mode=="observe"}
path=pathlib.Path(root)/"runner-metadata.json"
with path.open("x") as handle:
    json.dump(value,handle,indent=2)
os.chmod(path,0o644)
PY
  fi
  # exec would bypass the temporary-binary EXIT cleanup. The harness handles
  # INT/TERM itself; the evidence collector also signals the owned process group.
  "$shiki_binary" -test.v -test.count=1 -test.timeout="$shiki_timeout" \
    -test.run="^${shiki_test}$" -shiki-default-rotation="$shiki_mode"
}
main "$@"; exit "$?"
