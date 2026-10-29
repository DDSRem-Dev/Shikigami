#!/usr/bin/env bash
set -euo pipefail
shiki_root=$(cd "$(dirname "$0")/.." && pwd)
source "$shiki_root/scripts/tool-versions.env"
case "$(uname -s):$(uname -m)" in
  Darwin:arm64|Darwin:x86_64) shiki_platform=osx-universal_binary ;;
  Linux:x86_64) shiki_platform=linux-x86_64 ;;
  Linux:aarch64|Linux:arm64) shiki_platform=linux-aarch_64 ;;
  *) echo 'Unsupported generator host; use macOS or Linux amd64/arm64.' >&2; exit 1 ;;
esac
shiki_archive="protoc-${PROTOC_VERSION}-${shiki_platform}.zip"
shiki_tools="$shiki_root/.tools"
mkdir -p "$shiki_tools/downloads" "$shiki_tools/bin"
if [[ ! -f "$shiki_tools/downloads/$shiki_archive" ]]; then
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
    "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/$shiki_archive" \
    --output "$shiki_tools/downloads/$shiki_archive.part"
  mv "$shiki_tools/downloads/$shiki_archive.part" "$shiki_tools/downloads/$shiki_archive"
fi
shiki_expected=$(awk -v archive="$shiki_archive" '$2 == archive {print $1}' "$shiki_root/scripts/protoc-checksums.txt")
if command -v sha256sum >/dev/null; then
  shiki_actual=$(sha256sum "$shiki_tools/downloads/$shiki_archive" | awk '{print $1}')
else
  shiki_actual=$(shasum -a 256 "$shiki_tools/downloads/$shiki_archive" | awk '{print $1}')
fi
if [[ -z "$shiki_expected" || "$shiki_actual" != "$shiki_expected" ]]; then
  echo 'protoc archive checksum mismatch; remove the cached archive before retrying.' >&2
  exit 1
fi
mkdir -p "$shiki_tools/protoc-$PROTOC_VERSION"
unzip -oq "$shiki_tools/downloads/$shiki_archive" -d "$shiki_tools/protoc-$PROTOC_VERSION"
GOBIN="$shiki_tools/bin" go install "google.golang.org/protobuf/cmd/protoc-gen-go@$PROTOC_GEN_GO_VERSION"
GOBIN="$shiki_tools/bin" go install "connectrpc.com/connect/v2/cmd/protoc-gen-connect-go@$PROTOC_GEN_CONNECT_GO_VERSION"
GOBIN="$shiki_tools/bin" go install "google.golang.org/grpc/cmd/protoc-gen-go-grpc@$PROTOC_GEN_GO_GRPC_VERSION"
"$shiki_tools/protoc-$PROTOC_VERSION/bin/protoc" --version
