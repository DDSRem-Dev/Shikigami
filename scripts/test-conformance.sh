#!/usr/bin/env bash
set -euo pipefail
# Full root-module regression, explicitly enabling the independently decoded
# terminal scenario. Other opt-in/platform tests keep their own honest skips.
main() {
  if [[ -z "${SHIKI_RUN_EVIDENCE_DIR:-}" ]]; then
    echo 'Use scripts/run-acceptance.py with a new evidence directory.' >&2
    exit 1
  fi
  if [[ $# != 0 ]]; then echo 'Usage: test-conformance.sh' >&2; exit 1; fi
  # Python helper imports must not add bytecode files to the frozen source tree.
  export PYTHONDONTWRITEBYTECODE=1
  shiki_conformance_analyzer="$SHIKI_RUN_EVIDENCE_DIR/terminal-analyzer"
  go -C test/diagnostics/terminalstate/analyzer build -mod=readonly -trimpath -o "$shiki_conformance_analyzer" .
  go version -m "$shiki_conformance_analyzer" > "$SHIKI_RUN_EVIDENCE_DIR/analyzer-build.txt"
  export SHIKI_TERMINAL_ANALYZER="$shiki_conformance_analyzer"
  exec go test -mod=readonly -race -count=1 -json -tags=e2e ./...
}
main "$@"; exit "$?"
