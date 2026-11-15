#!/usr/bin/env bash
set -euo pipefail
shiki_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$shiki_root"
source scripts/tool-versions.env
shiki_tool() {
  if [[ -x "$1" ]]; then
    printf '%s\n' "$1"
  elif [[ -x "$1.exe" ]]; then
    printf '%s\n' "$1.exe"
  else
    echo 'Missing pinned tools. Run scripts/dev-setup.sh first.' >&2
    return 1
  fi
}
shiki_protoc=$(shiki_tool "$shiki_root/.tools/protoc-$PROTOC_VERSION/bin/protoc")
shiki_protoc_gen_go=$(shiki_tool "$shiki_root/.tools/bin/protoc-gen-go")
shiki_protoc_gen_connect=$(shiki_tool "$shiki_root/.tools/bin/protoc-gen-connect-go")
shiki_protoc_gen_grpc=$(shiki_tool "$shiki_root/.tools/bin/protoc-gen-go-grpc")
[[ "$("$shiki_protoc" --version)" == "libprotoc $PROTOC_VERSION" ]]
[[ "$("$shiki_protoc_gen_go" --version)" == "${shiki_protoc_gen_go##*/} $PROTOC_GEN_GO_VERSION" ]]
[[ "$("$shiki_protoc_gen_connect" --version)" == "${PROTOC_GEN_CONNECT_GO_VERSION#v}" ]]
shiki_grpc_version=$("$shiki_protoc_gen_grpc" --version)
[[ "$shiki_grpc_version" == "protoc-gen-go-grpc ${PROTOC_GEN_GO_GRPC_VERSION#v}" || "$shiki_grpc_version" == "protoc-gen-go-grpc.exe ${PROTOC_GEN_GO_GRPC_VERSION#v}" ]]
shiki_module=github.com/DDSRem-Dev/Shikigami
"$shiki_protoc" -I proto -I ".tools/protoc-$PROTOC_VERSION/include" \
  --plugin="protoc-gen-go=$shiki_protoc_gen_go" \
  --plugin="protoc-gen-connect-go=$shiki_protoc_gen_connect" \
  --go_out=. --go_opt="module=$shiki_module" \
  --connect-go_out=. --connect-go_opt="module=$shiki_module" \
  proto/shiki.proto proto/types.proto proto/plugin.proto proto/agent.proto proto/extensions.proto
"$shiki_protoc" -I proto -I ".tools/protoc-$PROTOC_VERSION/include" \
  --plugin="protoc-gen-go-grpc=$shiki_protoc_gen_grpc" \
  --go-grpc_out=. --go-grpc_opt="module=$shiki_module" proto/plugin.proto proto/extensions.proto
