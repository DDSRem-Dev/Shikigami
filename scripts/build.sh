#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source ./scripts/build-metadata.sh
go build ./...
mkdir -p bin
# Emit executables only for implemented command entrypoints.
for shiki_command in shiki shiki-agent shiki-daemon shiki-install; do
  if [[ -d "cmd/$shiki_command" ]]; then
    go build -trimpath -ldflags "$shiki_ldflags" -o "bin/$shiki_command" "./cmd/$shiki_command"
  fi
done
for shiki_plugin in sync port; do
  if [[ -d "plugins/$shiki_plugin" ]]; then
    go build -trimpath -ldflags "$shiki_ldflags" -o "bin/shiki-plugin-$shiki_plugin" "./plugins/$shiki_plugin"
    env -u GOOS -u GOARCH go run ./scripts/plugin-manifest.go --dir bin --id "$shiki_plugin" --version "$shiki_version"
  fi
done
