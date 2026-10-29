#!/usr/bin/env bash
set -euo pipefail

shiki_soak_snapshot=''
shiki_soak_child=''
shiki_soak_cidfile=''
cleanup() {
  shiki_soak_result=$?
  trap - EXIT INT TERM HUP
  if [[ -n "$shiki_soak_child" ]] && kill -0 "$shiki_soak_child" 2>/dev/null; then
    kill -TERM "$shiki_soak_child" 2>/dev/null || true
  fi
  # docker alone creates this new file inside our private snapshot directory.
  # This is the exact disposable test container, never a name/pattern search.
  if [[ -n "$shiki_soak_cidfile" && -s "$shiki_soak_cidfile" ]]; then
    shiki_soak_cid=$(cat "$shiki_soak_cidfile")
    if [[ "$shiki_soak_cid" =~ ^[a-f0-9]{64}$ ]]; then
      docker stop --time 15 "$shiki_soak_cid" >/dev/null 2>&1 || true
      docker rm -f "$shiki_soak_cid" >/dev/null 2>&1 || true
    fi
  fi
  if [[ -n "$shiki_soak_child" ]]; then
    wait "$shiki_soak_child" || true
  fi
  [[ -z "$shiki_soak_snapshot" ]] || rm -rf "$shiki_soak_snapshot"
  exit "$shiki_soak_result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

main() {
  local shiki_repository shiki_duration shiki_minutes shiki_timeout shiki_tcp_clients
  shiki_repository=$(cd "$(dirname "$0")/.." && pwd)
  shiki_duration=${SHIKI_SOAK_DURATION:-1h}
  if [[ ! "$shiki_duration" =~ ^([1-9][0-9]{0,3})(m|h)$ ]]; then
    echo 'SHIKI_SOAK_DURATION must be whole minutes or hours, between 1m and 8h.' >&2
    exit 2
  fi
  shiki_minutes=${BASH_REMATCH[1]}
  if [[ "${BASH_REMATCH[2]}" == h ]]; then
    shiki_minutes=$((shiki_minutes * 60))
  fi
  if (( shiki_minutes > 480 )); then
    echo 'SHIKI_SOAK_DURATION must not exceed 8h (worker rotation is a separate experiment).' >&2
    exit 2
  fi
  shiki_tcp_clients=${SHIKI_SOAK_TCP_CLIENTS:-0}
  if [[ ! "$shiki_tcp_clients" =~ ^([0-9]|1[0-6])$ ]]; then
    echo 'SHIKI_SOAK_TCP_CLIENTS must be 0 (disabled) or 1..16.' >&2
    exit 2
  fi
  shiki_timeout="$((shiki_minutes + 8))m"
  cd "$shiki_repository"
  case "${1:-}" in
    '')
      [[ $# == 0 ]] || { echo 'Usage: test-soak.sh [--linux]' >&2; exit 2; }
      [[ $(id -u) != 0 ]] || { echo 'Run locally as an ordinary user; root E2E skips are not acceptance.' >&2; exit 2; }
      command -v tmux >/dev/null || { echo 'tmux is required.' >&2; exit 1; }
      ;;
    --linux)
      [[ $# == 1 ]] || { echo 'Usage: test-soak.sh [--linux]' >&2; exit 2; }
      ;;
    *) echo 'Usage: test-soak.sh [--linux]' >&2; exit 2 ;;
  esac
  shiki_soak_snapshot=$(mktemp -d "${TMPDIR:-/tmp}/shiki-soak-source.XXXXXX")
  tar --exclude='./.git' --exclude='./.tools' --exclude='./bin' --exclude='./dist' \
    -cf - . | tar -xf - -C "$shiki_soak_snapshot"
  cd "$shiki_soak_snapshot"
  go version
  go list -mod=readonly -m github.com/klauspost/compress
  if command -v sha256sum >/dev/null; then
    sha256sum go.mod go.sum
  else
    shasum -a 256 go.mod go.sum
  fi
  local shiki_args=(-tags=e2e -count=1 -timeout="$shiki_timeout" \
    -run '^TestCLIThreeProcessTLSLifecycle$' -v ./test/e2e \
    -args -shiki-soak-duration="$shiki_duration" -shiki-soak-tcp-clients="$shiki_tcp_clients")
  if [[ "${1:-}" == --linux ]]; then
    shiki_soak_cidfile="$shiki_soak_snapshot/container.cid"
    SHIKI_TEST_CONTAINER_CIDFILE="$shiki_soak_cidfile" ./scripts/test-linux.sh "${shiki_args[@]}" &
  else
    go test -c -mod=readonly -race -tags=e2e -o "$shiki_soak_snapshot/soak.test" ./test/e2e
    GORACE=halt_on_error=1 "$shiki_soak_snapshot/soak.test" -test.v -test.count=1 \
      -test.timeout="$shiki_timeout" -test.run '^TestCLIThreeProcessTLSLifecycle$' \
      -shiki-soak-duration="$shiki_duration" -shiki-soak-tcp-clients="$shiki_tcp_clients" &
  fi
  shiki_soak_child=$!
  wait "$shiki_soak_child"
  shiki_soak_child=''
}

main "$@"; exit "$?"
