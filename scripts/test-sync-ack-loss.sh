#!/usr/bin/env bash
set -euo pipefail

shiki_ack_repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$shiki_ack_repo"
shiki_ack_args=(-count=1 -v ./pkg/plugin/state ./plugins/sync/client ./test/integration/syncack ./test/architecture)
case "${1:---native}" in
  --native)
    [[ $# -le 1 ]] || { echo 'usage: test-sync-ack-loss.sh [--native|--linux]' >&2; exit 2; }
    exec go test -mod=readonly -race "${shiki_ack_args[@]}"
    ;;
  --linux)
    [[ $# -eq 1 ]] || { echo 'usage: test-sync-ack-loss.sh [--native|--linux]' >&2; exit 2; }
    exec bash scripts/test-linux.sh "${shiki_ack_args[@]}"
    ;;
  *)
    echo 'usage: test-sync-ack-loss.sh [--native|--linux]' >&2
    exit 2
    ;;
esac
