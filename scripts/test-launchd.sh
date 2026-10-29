#!/usr/bin/env bash
set -euo pipefail

[[ "$(uname -s)" == Darwin && "$(id -u)" != 0 ]] || { echo 'Run as the ordinary logged-in macOS user.' >&2; exit 1; }
shiki_repository="$(cd "$(dirname "$0")/.." && pwd)"
shiki_test_root="$(mktemp -d /tmp/shiki-launchd.XXXXXXXX)"
shiki_test_root="$(cd "$shiki_test_root" && pwd -P)"
shiki_test_pid=''

cleanup() {
  shiki_result=$?
  trap - EXIT INT TERM HUP
  if [[ -n "$shiki_test_pid" ]] && kill -0 "$shiki_test_pid" 2>/dev/null; then
    kill -TERM "$shiki_test_pid" 2>/dev/null || true
    wait "$shiki_test_pid" || true
  fi
  # A second cleanup layer covers Go timeout/panic. Never boot out a job or
  # remove a plist unless it names this invocation's unique installation.
  if ! python3 "$shiki_repository/scripts/test-launchd-cleanup.py" "$shiki_test_root"; then
    echo "Cleanup needs inspection; retained $shiki_test_root" >&2
    exit 1
  fi
  rm -rf "$shiki_test_root"
  exit "$shiki_result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
mkdir "$shiki_test_root/source"
tar -C "$shiki_repository" --exclude='./.git' --exclude='./.tools' --exclude='./bin' --exclude='./dist' -cf - . | tar -xf - -C "$shiki_test_root/source"
cd "$shiki_test_root/source"
go version
go list -m github.com/charmbracelet/bubbletea github.com/muesli/cancelreader github.com/klauspost/compress
shasum -a 256 go.mod go.sum
go test -c -race -mod=readonly -tags=e2e -o "$shiki_test_root/launchd.test" ./test/e2e
SHIKI_LAUNCHD_TEST_ROOT="$shiki_test_root" "$shiki_test_root/launchd.test" -test.v -test.run '^TestNativeLaunchAgentLifecycle$' -test.timeout=8m &
shiki_test_pid=$!
wait "$shiki_test_pid"
shiki_test_pid=''
