#!/usr/bin/env bash
set -euo pipefail

# Parse the complete workflow before starting long-running commands.
main() {
  shiki_repository="$(cd "$(dirname "$0")/.." && pwd)"
  cd "$shiki_repository"
  shiki_test_platform="${SHIKI_TEST_PLATFORM:-}"
  case "$shiki_test_platform" in
    ''|linux/amd64|linux/arm64) ;;
    *) echo 'SHIKI_TEST_PLATFORM must be linux/amd64 or linux/arm64 when set.' >&2; exit 1 ;;
  esac
  shiki_docker_command=(docker run --rm --init)
  if [[ -n "${SHIKI_TEST_CONTAINER_CIDFILE:-}" ]]; then
    if [[ -e "$SHIKI_TEST_CONTAINER_CIDFILE" || -L "$SHIKI_TEST_CONTAINER_CIDFILE" ]]; then
      echo 'SHIKI_TEST_CONTAINER_CIDFILE must be a new private runner-owned path.' >&2
      exit 1
    fi
    shiki_docker_command+=(--cidfile "$SHIKI_TEST_CONTAINER_CIDFILE")
  fi
  if [[ -n "$shiki_test_platform" ]]; then
    shiki_docker_command+=(--platform "$shiki_test_platform")
  fi
  go mod download
  shiki_module_cache="$(go env GOMODCACHE)"
  shiki_debian_mirror="${SHIKI_TEST_DEBIAN_MIRROR:-https://deb.debian.org}"
  if [[ ! "$shiki_debian_mirror" =~ ^https://[a-zA-Z0-9.-]+(:[0-9]{1,5})?$ ]]; then
    echo "SHIKI_TEST_DEBIAN_MIRROR must be an HTTPS origin without a path." >&2
    exit 1
  fi
  # Freeze this invocation's source before concurrent development can change it.
  # Excluding VCS metadata also prevents nested test builds querying an unrelated
  # remote pseudo-version while the module cache is deliberately read-only.
  shiki_source_snapshot="$(mktemp -d "${TMPDIR:-/tmp}/shiki-linux-source.XXXXXX")"
  trap 'rm -rf "$shiki_source_snapshot"' EXIT
  tar --exclude='./.git' --exclude='./.tools' --exclude='./bin' --exclude='./dist' -cf - . | tar -xf - -C "$shiki_source_snapshot"
  # Official multi-platform Go 1.26.10 bookworm manifest, pinned for this test.
  shiki_test_image='golang@sha256:632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af'
  if [[ $# == 0 ]]; then set -- ./...; fi

  # Source/dependencies are read-only, tests run as an ordinary user. --init
  # reaps orphaned helpers in the intentional host-crash lifecycle tests.
  "${shiki_docker_command[@]}" \
    --env "SHIKI_TEST_DEBIAN_MIRROR=$shiki_debian_mirror" \
    --mount "type=bind,source=$shiki_source_snapshot,target=/workspace,readonly" \
    --mount "type=bind,source=$shiki_module_cache,target=/go/pkg/mod,readonly" \
    --workdir /workspace "$shiki_test_image" bash -c '
      set -euo pipefail
      sed -i "s|http://deb.debian.org|${SHIKI_TEST_DEBIAN_MIRROR}|g" /etc/apt/sources.list.d/debian.sources
      apt-get -o Acquire::Retries=3 update -qq
      apt-get -o Acquire::Retries=3 install -y -qq tmux iproute2 >/dev/null
      useradd -m shikitest
      /usr/local/go/bin/go version
      uname -m
      tmux -V
      exec runuser -u shikitest -- env PATH=/usr/local/go/bin:/usr/bin:/bin \
        GOCACHE=/tmp/shiki-go-cache GOPATH=/go \
        go test -mod=readonly -race "$@"
    ' shiki-linux-tests "$@"
}

# Exit from this already-parsed command list; never read a modified file tail.
main "$@"; exit "$?"
