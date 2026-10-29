#!/usr/bin/env bash
set -euo pipefail

main() {
  if [[ -z "${SHIKI_RUN_EVIDENCE_DIR:-}" ]]; then
    echo 'Use scripts/run-acceptance.py to supply an independent private evidence directory.' >&2
    exit 1
  fi
  for shiki_memory_option in "$@"; do
    case "$shiki_memory_option" in --preflight|--idle-control|--defer-profiles) ;; *) echo 'Usage: test-memory-windows.sh [--preflight] [--idle-control] [--defer-profiles]' >&2; exit 1;; esac
  done
  shiki_memory_binary="$SHIKI_RUN_EVIDENCE_DIR/memoryprofile.test"
  go test -c -trimpath -mod=readonly -tags=memoryprofile -o "$shiki_memory_binary" ./test/diagnostics/memoryprofile
  go version -m "$shiki_memory_binary" > "$SHIKI_RUN_EVIDENCE_DIR/build-info.txt"
  python3 - "$shiki_memory_binary" <<'PY'
import hashlib, json, pathlib, sys
path = pathlib.Path(sys.argv[1])
(path.parent / 'binary-sha256.json').write_text(json.dumps({'binary': path.name, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}, indent=2) + '\n')
PY
  # Production IPC must run on a local filesystem with the actual ordinary
  # user's ownership, not a Docker Desktop evidence bind mount. A short path
  # also leaves room for nested private go-plugin Unix sockets on macOS.
  exec python3 scripts/run-memory-windows.py "$shiki_memory_binary" "$SHIKI_RUN_EVIDENCE_DIR" "$@"
}

main "$@"; exit "$?"
