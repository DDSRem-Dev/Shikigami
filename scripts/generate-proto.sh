#!/usr/bin/env bash
set -euo pipefail
shiki_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$shiki_root"
source scripts/tool-versions.env
shiki_protoc="$shiki_root/.tools/protoc-$PROTOC_VERSION/bin/protoc"
if [[ ! -x "$shiki_protoc" || ! -x .tools/bin/protoc-gen-connect-go || ! -x .tools/bin/protoc-gen-go || ! -x .tools/bin/protoc-gen-go-grpc ]]; then
  echo 'Missing pinned tools. Run scripts/dev-setup.sh first.' >&2
  exit 1
fi
[[ "$("$shiki_protoc" --version)" == "libprotoc $PROTOC_VERSION" ]]
[[ "$(.tools/bin/protoc-gen-go --version)" == "protoc-gen-go $PROTOC_GEN_GO_VERSION" ]]
[[ "$(.tools/bin/protoc-gen-connect-go --version)" == "${PROTOC_GEN_CONNECT_GO_VERSION#v}" ]]
[[ "$(.tools/bin/protoc-gen-go-grpc --version)" == "protoc-gen-go-grpc ${PROTOC_GEN_GO_GRPC_VERSION#v}" ]]
export PATH="$shiki_root/.tools/bin:$PATH"
shiki_module=github.com/DDSRem-Dev/Shikigami
"$shiki_protoc" -I proto -I ".tools/protoc-$PROTOC_VERSION/include" \
  --go_out=. --go_opt="module=$shiki_module" \
  --connect-go_out=. --connect-go_opt="module=$shiki_module" \
  proto/shiki.proto proto/types.proto proto/plugin.proto proto/agent.proto
"$shiki_protoc" -I proto -I ".tools/protoc-$PROTOC_VERSION/include" \
  --go-grpc_out=. --go-grpc_opt="module=$shiki_module" proto/plugin.proto
