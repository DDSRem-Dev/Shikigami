#!/usr/bin/env bash
# Opt-in: uses the actual account GUI domain and HOME with private custom paths.
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
export PYTHONDONTWRITEBYTECODE=1
exec python3 -B "$script_dir/../test/diagnostics/schemaupgrademacos/runner.py" "$@"
