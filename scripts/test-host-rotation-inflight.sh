#!/usr/bin/env bash
set -euo pipefail

shiki_rotation_repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$shiki_rotation_repo"
shiki_rotation_args=(-count=1 -run '^TestRealHostRotationRecoversInflightFiles$' -v ./test/integration/hostrotation)
case "${1:---native}" in
  --native)
    [[ $# -le 1 ]] || { echo 'usage: test-host-rotation-inflight.sh [--native|--linux]' >&2; exit 2; }
    exec go test -mod=readonly -race "${shiki_rotation_args[@]}"
    ;;
  --linux)
    [[ $# -eq 1 ]] || { echo 'usage: test-host-rotation-inflight.sh [--native|--linux]' >&2; exit 2; }
    exec bash scripts/test-linux.sh "${shiki_rotation_args[@]}"
    ;;
  *)
    echo 'usage: test-host-rotation-inflight.sh [--native|--linux]' >&2
    exit 2
    ;;
esac
