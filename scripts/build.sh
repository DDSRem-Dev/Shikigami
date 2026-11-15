#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source ./scripts/build-metadata.sh
go build -trimpath ./...
mkdir -p bin
shiki_target_os="${GOOS:-$(go env GOOS)}"
# Emit executables only for implemented command entrypoints.
for shiki_command in shiki shiki-agent shiki-daemon shiki-install; do
  if [[ -d "cmd/$shiki_command" ]]; then
    go build -trimpath -ldflags "$shiki_ldflags" -o "bin/$shiki_command" "./cmd/$shiki_command"
  fi
done
if [[ "$shiki_target_os" == linux ]]; then
  go build -trimpath -ldflags "$shiki_ldflags" -o bin/shiki-region-helper ./plugins/region/cmd/helper
  go build -trimpath -ldflags "$shiki_ldflags" -o bin/shiki-browser-runtime ./plugins/browser/cmd/runtime
fi
shiki_plugins=(sync port browser)
[[ "$shiki_target_os" != linux ]] || shiki_plugins+=(region)
for shiki_plugin in "${shiki_plugins[@]}"; do
  go build -trimpath -ldflags "$shiki_ldflags" -o "bin/shiki-plugin-$shiki_plugin" "./plugins/$shiki_plugin"
  env -u GOOS -u GOARCH go run ./scripts/plugin-manifest.go --dir bin --id "$shiki_plugin" --version "$shiki_version" --os "$shiki_target_os"
done
