#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
: "${SHIKI_VERSION:?Set an explicit SHIKI_VERSION for this local build.}"
./scripts/build.sh
env -u GOOS -u GOARCH go run ./scripts/release.go --dir bin --output dist --version "$SHIKI_VERSION" --os "${GOOS:-$(go env GOOS)}" --arch "${GOARCH:-$(go env GOARCH)}"
if [[ -n "${SHIKI_SIGNING_KEY:-}" ]]; then
  shiki_release_os="${GOOS:-$(go env GOOS)}"
  shiki_release_arch="${GOARCH:-$(go env GOARCH)}"
  shiki_release_archive="dist/shikigami-${SHIKI_VERSION}-${shiki_release_os}-${shiki_release_arch}.tar.gz"
  env -u GOOS -u GOARCH go run ./scripts/release-trust.go sign \
    --archive "$shiki_release_archive" \
    --checksums-pin "$shiki_release_archive.checksums-pin" \
    --private-key "$SHIKI_SIGNING_KEY" \
    --signature "$shiki_release_archive.sig.json" \
    --version "$SHIKI_VERSION" --os "$shiki_release_os" --arch "$shiki_release_arch"
fi
