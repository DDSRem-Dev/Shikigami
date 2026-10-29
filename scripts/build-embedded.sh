#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source ./scripts/build-metadata.sh
shiki_embed_dir='internal/plugin/embedded-assets'
# One target is embedded into each host build. Never share this generated
# directory between simultaneous cross-platform builds in the same checkout.
if ! mkdir "$shiki_embed_dir"; then
  echo 'Embedded staging already exists; remove it after checking no build is active.' >&2; exit 1
fi
trap 'rm -rf "$shiki_embed_dir"' EXIT HUP INT TERM
for shiki_plugin in sync port; do
  go build -trimpath -ldflags "$shiki_ldflags" -o "$shiki_embed_dir/shiki-plugin-$shiki_plugin" "./plugins/$shiki_plugin"
  env -u GOOS -u GOARCH go run ./scripts/plugin-manifest.go --dir "$shiki_embed_dir" --id "$shiki_plugin" --version "$shiki_version"
done
mkdir -p bin
for shiki_command in shiki shiki-agent shiki-daemon shiki-install; do
  go build -trimpath -tags shiki_embedded -ldflags "$shiki_ldflags" -o "bin/$shiki_command" "./cmd/$shiki_command"
done
if [[ "${SHIKI_EMBED_VERIFY:-0}" == 1 ]]; then
  shiki_native_os="$(env -u GOOS -u GOARCH go env GOOS)"
  shiki_native_arch="$(env -u GOOS -u GOARCH go env GOARCH)"
  [[ "${GOOS:-$shiki_native_os}" == "$shiki_native_os" && "${GOARCH:-$shiki_native_arch}" == "$shiki_native_arch" ]] || {
    echo 'Runtime embedded verification requires a native target.' >&2; exit 1;
  }
  go test -race -tags shiki_embedded -ldflags "$shiki_ldflags" -count=1 -timeout=60s ./test/integration/embedded
fi
