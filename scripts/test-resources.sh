#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
shiki_resource_cycles="${SHIKI_RESOURCE_CYCLES:-24}"
if [[ ! "$shiki_resource_cycles" =~ ^[0-9]+$ ]] || ((shiki_resource_cycles < 8 || shiki_resource_cycles > 100)); then
  echo 'SHIKI_RESOURCE_CYCLES must be an integer in 8..100.' >&2
  exit 2
fi
shiki_resource_args=(
  -tags=e2e -p=1 -count=1 -timeout=20m -v
  -run '^(TestBoundedPluginResourceRecovery|TestTwoProductionProcessesHalfClosePolicyAndStop|TestCLIThreeProcessTLSLifecycle)$'
  ./test/integration/plugin ./test/integration/port ./test/e2e
  -args "-shiki-resource-cycles=$shiki_resource_cycles"
)
case "${1:-}" in
  --linux)
    [[ $# == 1 ]] || { echo 'Usage: test-resources.sh [--linux]' >&2; exit 2; }
    exec ./scripts/test-linux.sh "${shiki_resource_args[@]}"
    ;;
  '')
    [[ $# == 0 ]] || { echo 'Usage: test-resources.sh [--linux]' >&2; exit 2; }
    ((EUID != 0)) || { echo 'Run as an ordinary user or pass --linux; root E2E skips are not acceptance.' >&2; exit 2; }
    command -v tmux >/dev/null || { echo 'tmux is required for terminal resource acceptance.' >&2; exit 1; }
    exec go test -race "${shiki_resource_args[@]}"
    ;;
  *) echo 'Usage: test-resources.sh [--linux]' >&2; exit 2 ;;
esac
