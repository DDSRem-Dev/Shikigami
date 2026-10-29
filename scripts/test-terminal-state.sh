#!/usr/bin/env bash
set -euo pipefail
main() {
  if [[ -z "${SHIKI_RUN_EVIDENCE_DIR:-}" ]]; then
    echo 'Use scripts/run-acceptance.py with a new evidence directory.' >&2
    exit 1
  fi
  if [[ $# != 0 ]]; then echo 'Usage: test-terminal-state.sh' >&2; exit 1; fi
  shiki_terminal_analyzer="$SHIKI_RUN_EVIDENCE_DIR/terminal-analyzer"
  shiki_terminal_test="$SHIKI_RUN_EVIDENCE_DIR/terminal-state.test"
  go -C test/diagnostics/terminalstate/analyzer build -mod=readonly -trimpath -o "$shiki_terminal_analyzer" .
  go test -c -mod=readonly -race -o "$shiki_terminal_test" ./test/integration/terminalstate
  go version -m "$shiki_terminal_analyzer" > "$SHIKI_RUN_EVIDENCE_DIR/analyzer-build.txt"
  go version -m "$shiki_terminal_test" > "$SHIKI_RUN_EVIDENCE_DIR/test-build.txt"
  export SHIKI_TERMINAL_ANALYZER="$shiki_terminal_analyzer"
  exec "$shiki_terminal_test" -test.v -test.count=1 -test.timeout=90s
}
main "$@"; exit "$?"
