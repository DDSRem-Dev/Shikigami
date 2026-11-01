#!/usr/bin/env bash
set -euo pipefail
main() {
  if [[ -z "${SHIKI_RUN_EVIDENCE_DIR:-}" ]]; then
    echo 'Use scripts/run-acceptance.py with a new evidence directory.' >&2
    exit 1
  fi
  if [[ $# != 0 ]]; then echo 'Usage: test-terminal-state.sh' >&2; exit 1; fi
  # Keep the terminal fixture's tmux socket private to this acceptance run.
  # Hosted macOS runners can retain a user tmux socket from an interrupted
  # process; sharing it makes the first pane attach nondeterministic.
  shiki_tmux_tmpdir="$SHIKI_RUN_EVIDENCE_DIR/tmux-tmp"
  mkdir -m 700 "$shiki_tmux_tmpdir"
  export TMUX_TMPDIR="$shiki_tmux_tmpdir"
  mark_phase() { printf '{}\n' > "$SHIKI_RUN_EVIDENCE_DIR/phase-$1.running.json"; }
  complete_phase() { mv "$SHIKI_RUN_EVIDENCE_DIR/phase-$1.running.json" "$SHIKI_RUN_EVIDENCE_DIR/phase-$1.done.json"; }
  shiki_terminal_analyzer="$SHIKI_RUN_EVIDENCE_DIR/terminal-analyzer"
  shiki_terminal_test="$SHIKI_RUN_EVIDENCE_DIR/terminal-state.test"
  mark_phase analyzer-build
  go -C test/diagnostics/terminalstate/analyzer build -mod=readonly -trimpath -o "$shiki_terminal_analyzer" .
  complete_phase analyzer-build
  mark_phase test-compile
  go test -c -mod=readonly -race -o "$shiki_terminal_test" ./test/integration/terminalstate
  complete_phase test-compile
  go version -m "$shiki_terminal_analyzer" > "$SHIKI_RUN_EVIDENCE_DIR/analyzer-build.txt"
  go version -m "$shiki_terminal_test" > "$SHIKI_RUN_EVIDENCE_DIR/test-build.txt"
  export SHIKI_TERMINAL_ANALYZER="$shiki_terminal_analyzer"
  mark_phase test-run
  # Hosted macOS runners occasionally lose the short-lived tmux fixture while
  # the runner is under load.  Retry the complete isolated test process once;
  # a real assertion failure still fails the stage after both attempts.
  status=1
  for attempt in 1 2; do
    "$shiki_terminal_test" -test.v -test.count=1 -test.timeout=90s && status=0 || status=$?
    [[ "$status" == 0 || "$status" == 130 || "$status" == 143 || "$attempt" == 2 ]] && break
    echo "terminal semantic test attempt $attempt failed; retrying isolated fixture" >&2
  done
  complete_phase test-run
  return "$status"
}
main "$@"
