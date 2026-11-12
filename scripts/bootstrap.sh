#!/bin/bash
# Shikigami one-line installer / upgrader.
#
# For a stable release, GitHub's latest/download alias may be used. For a
# prerelease, fetch this script from the explicit /releases/download/<tag> URL.
#
# Server (daemon) — run on the VPS as root or with sudo:
#   curl -fsSL https://github.com/DDSRem-Dev/Shikigami/releases/download/v0.0.1-alpha.2/bootstrap.sh \
#        | sudo bash -s -- --version=v0.0.1-alpha.2 --user=rem
#
# Client (agent + CLI) — run on the local machine as your own user:
#   curl -fsSL https://github.com/DDSRem-Dev/Shikigami/releases/download/v0.0.1-alpha.2/bootstrap.sh \
#        | bash -s -- --version=v0.0.1-alpha.2 --role=client
#
# Non-interactive VPS (CI / automation):
#   curl -fsSL .../bootstrap.sh | sudo bash -s -- \
#       --user=rem --tailscale-key-file=/run/secrets/ts-key \
#       --non-interactive --token-output=/run/secrets/shiki-token
#
# Options:
#   --role=daemon|client          default: daemon when running as root, else client
#   --user=NAME                   required for daemon; ordinary account to run the service
#   --version=vX.Y.Z              pin a specific release; default: latest published release
#   --tailscale-key-file=PATH     one-time auth key file (non-interactive daemon)
#   --non-interactive             never open /dev/tty; all required inputs must be flags
#   --token-output=PATH           write initial daemon Token to this 0600 file
#   --yes                         skip "about to install" confirmation prompt
#   --help                        show this message

set -euo pipefail
export LC_ALL=C
export PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin

# ---------------------------------------------------------------------------
# Embedded release-signing public key (SPKI PEM, Ed25519).
# Fingerprint (SHA-256 of DER): e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d
# ---------------------------------------------------------------------------
SHIKI_EMBEDDED_PUB='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAqoLSvxSfqJ8Auu3Y9+CCw8FmHJ7tpuUIvvtik1Jzhpg=
-----END PUBLIC KEY-----'

SHIKI_REPO="DDSRem-Dev/Shikigami"
SHIKI_API="https://api.github.com/repos/${SHIKI_REPO}"
SHIKI_RELEASES="https://github.com/${SHIKI_REPO}/releases"
SHIKI_MAX_META=65536      # bytes; for sig/checksums sidecar files
SHIKI_MAX_ARCHIVE=1073741824  # 1 GiB hard cap

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
shiki_role=""
shiki_user=""
shiki_version=""
shiki_key_file=""
shiki_token_output=""
shiki_noninteractive=0
shiki_yes=0
shiki_pass_through=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h)
      sed -n '/^# Options:/,/^[^#]/p' "$0" | grep '^#' | sed 's/^# \{0,1\}//'
      exit 0 ;;
    --role=*)       shiki_role="${1#*=}" ;;
    --role)         shift; shiki_role="$1" ;;
    --user=*)       shiki_user="${1#*=}" ;;
    --user)         shift; shiki_user="$1" ;;
    --version=*)    shiki_version="${1#*=}" ;;
    --version)      shift; shiki_version="$1" ;;
    --tailscale-key-file=*) shiki_key_file="${1#*=}" ;;
    --tailscale-key-file)   shift; shiki_key_file="$1" ;;
    --non-interactive) shiki_noninteractive=1 ;;
    --token-output=*) shiki_token_output="${1#*=}" ;;
    --token-output)   shift; shiki_token_output="$1" ;;
    --yes|-y)       shiki_yes=1 ;;
    # pass unknown flags through to shiki-install for forward compat
    --*=*|--*) shiki_pass_through+=("$1") ;;
    *) echo "bootstrap: unknown argument '$1'" >&2; exit 2 ;;
  esac
  shift
done

# ---------------------------------------------------------------------------
# Role auto-detection
# ---------------------------------------------------------------------------
if [[ -z "$shiki_role" ]]; then
  if [[ "$(id -u)" -eq 0 ]]; then
    shiki_role="daemon"
  else
    shiki_role="client"
  fi
fi
[[ "$shiki_role" == "daemon" || "$shiki_role" == "client" ]] || {
  echo "bootstrap: --role must be daemon or client" >&2; exit 2
}
if [[ "$shiki_role" == "daemon" && -z "$shiki_user" ]]; then
  echo "bootstrap: --user is required for daemon installation" >&2
  echo "  Example: sudo bash -s -- --user=rem" >&2
  exit 2
fi
if [[ "$shiki_role" == "client" && "$(id -u)" -eq 0 ]]; then
  echo "bootstrap: install the client as the ordinary user, not as root" >&2
  exit 2
fi

# ---------------------------------------------------------------------------
# Platform detection
# ---------------------------------------------------------------------------
shiki_os=""
case "$(uname -s)" in
  Linux)  shiki_os="linux" ;;
  Darwin) shiki_os="darwin" ;;
  *) echo "bootstrap: unsupported OS '$(uname -s)'" >&2; exit 1 ;;
esac
shiki_arch=""
case "$(uname -m)" in
  x86_64|amd64) shiki_arch="amd64" ;;
  aarch64|arm64) shiki_arch="arm64" ;;
  *) echo "bootstrap: unsupported architecture '$(uname -m)'" >&2; exit 1 ;;
esac

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
shiki_say() { printf '\033[1;34m[shiki]\033[0m %s\n' "$*"; }
shiki_ok()  { printf '\033[1;32m[shiki]\033[0m %s\n' "$*"; }
shiki_warn(){ printf '\033[1;33m[shiki]\033[0m %s\n' "$*" >&2; }
shiki_die() { printf '\033[1;31m[shiki]\033[0m %s\n' "$*" >&2; exit 1; }

shiki_hash() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    shiki_die "No SHA-256 utility found (sha256sum or shasum required)."
  fi
}

shiki_download() {
  # shiki_download <url> <dest> <max_bytes>
  local _url="$1" _dest="$2" _max="$3"
  if command -v curl >/dev/null 2>&1; then
    curl --fail --silent --show-error --location \
         --max-filesize "$_max" \
         --retry 3 --retry-delay 2 \
         --output "$_dest" "$_url"
  elif command -v wget >/dev/null 2>&1; then
    wget --quiet --max-redirect=5 \
         --output-document="$_dest" "$_url"
  else
    shiki_die "Neither curl nor wget is available."
  fi
  # Enforce the limit even for chunked responses without Content-Length.
  local _size
  _size=$(wc -c < "$_dest" 2>/dev/null || echo 0)
  if [[ "$_size" -gt "$_max" ]]; then
    rm -f "$_dest"
    shiki_die "Downloaded file exceeds size limit ($_max bytes)."
  fi
}

# ---------------------------------------------------------------------------
# Version resolution
# ---------------------------------------------------------------------------
shiki_validate_version() {
  [[ "$1" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.[1-9][0-9]*)?$ ]]
}

shiki_resolve_version() {
  local _ver _tmp
  if [[ -n "$shiki_version" ]]; then
    shiki_validate_version "$shiki_version" ||
      shiki_die "Invalid release version; use vMAJOR.MINOR.PATCH or a prerelease tag."
    printf '%s\n' "$shiki_version"
    return
  fi
  # /releases/latest excludes prereleases. The public project currently ships
  # prereleases, so query the newest published release instead.
  _tmp="$(mktemp)"
  if shiki_download "${SHIKI_API}/releases?per_page=1" "$_tmp" 65536 2>/dev/null; then
    _ver="$(grep -o '"tag_name"[[:space:]]*:[[:space:]]*"v[^"]*"' "$_tmp" |
      head -n 1 | sed -E 's/.*"([^"]+)"/\1/' || true)"
  fi
  rm -f "$_tmp"
  [[ -n "$_ver" ]] && shiki_validate_version "$_ver" ||
    shiki_die "Cannot determine latest published version. Use --version=<tag> to pin a release."
  printf '%s\n' "$_ver"
}

# A release bootstrap MUST authenticate the Ed25519 envelope. A same-origin
# SHA-256 sidecar is only transport integrity and is never an authentication
# fallback.
shiki_base64_decode() {
  if printf '%s' "$1" | base64 --decode > "$2" 2>/dev/null; then
    return 0
  fi
  rm -f "$2"
  printf '%s' "$1" | base64 -D > "$2"
}

shiki_verify_sig() {
  local _archive="$1" _sigfile="$2" _pub="$3" _version="$4" _os="$5" _arch="$6"
  local _envelope _payload_b64 _signature_b64 _payload _rest _archive_hash _archive_size _pin
  local _expected_name _actual_hash _actual_size _work
  _envelope="$(cat "$_sigfile")"
  [[ "${#_envelope}" -le "$SHIKI_MAX_META" ]] || shiki_die "Signature metadata is too large."
  # The publisher emits this canonical compact envelope. Reject alternate
  # field names, duplicate fields, whitespace and trailing JSON values.
  [[ "$_envelope" =~ ^\{\"format\":1,\"payload\":\"([A-Za-z0-9+/]+={0,2})\",\"signature\":\"([A-Za-z0-9+/]+={0,2})\"\}$ ]] ||
    shiki_die "Invalid signature envelope."
  _payload_b64="${BASH_REMATCH[1]}"
  _signature_b64="${BASH_REMATCH[2]}"
  _work="$(mktemp -d "$shiki_tmp/signature.XXXXXXXX")"
  chmod 700 "$_work"
  shiki_base64_decode "$_payload_b64" "$_work/payload" ||
    shiki_die "Invalid signature payload encoding."
  shiki_base64_decode "$_signature_b64" "$_work/signature" ||
    shiki_die "Invalid signature encoding."
  [[ "$(wc -c < "$_work/signature" | tr -d '[:space:]')" == 64 ]] ||
    shiki_die "Invalid signature size."
  printf '%b' 'Shikigami release archive signature\0format=1\0' > "$_work/message"
  cat "$_work/payload" >> "$_work/message"
  printf '%s' "$_pub" > "$_work/public.pem"
  command -v openssl >/dev/null 2>&1 ||
    shiki_die "OpenSSL is required to verify release signatures."
  openssl pkeyutl -verify -pubin -inkey "$_work/public.pem" -rawin \
    -in "$_work/message" -sigfile "$_work/signature" >/dev/null 2>&1 ||
    shiki_die "Ed25519 release signature verification failed."
  _payload="$(cat "$_work/payload")"
  _expected_name="shikigami-${_version}-${_os}-${_arch}.tar.gz"
  _rest="${_payload#"{\"version\":\"${_version}\",\"os\":\"${_os}\",\"arch\":\"${_arch}\",\"archive\":\"${_expected_name}\",\"archive_sha256\":\""}"
  [[ "$_rest" != "$_payload" ]] || shiki_die "Signed release metadata identity mismatch."
  _archive_hash="${_rest%%\",\"archive_size\":*}"
  _rest="${_rest#*\",\"archive_size\":}"
  _archive_size="${_rest%%,\"checksums_sha256\":\"*}"
  _pin="${_rest#*,\"checksums_sha256\":\"}"
  _pin="${_pin%\"}"
  [[ "$_rest" == "${_archive_size},\"checksums_sha256\":\"${_pin}\"}" ]] ||
    shiki_die "Invalid signed release metadata."
  [[ "$_archive_hash" =~ ^[0-9a-f]{64}$ && "$_pin" =~ ^[0-9a-f]{64}$ &&
     "$_archive_size" =~ ^[1-9][0-9]*$ ]] ||
    shiki_die "Invalid signed release metadata."
  _actual_size="$(wc -c < "$_archive" | tr -d '[:space:]')"
  _actual_hash="$(shiki_hash "$_archive")"
  [[ "$_actual_size" == "$_archive_size" && "$_actual_hash" == "$_archive_hash" ]] ||
    shiki_die "Signed archive size or SHA-256 mismatch."
  printf '%s' "$_pin"
}
# ---------------------------------------------------------------------------
# Bounded, safe copy (Perl, same as install.sh)
# ---------------------------------------------------------------------------
shiki_need_perl() {
  [[ -x /usr/bin/perl ]] || shiki_die "Secure copy needs /usr/bin/perl."
}

shiki_safe_copy() {
  /usr/bin/perl -MFcntl=:DEFAULT,:mode -e '
    my ($source, $target, $limit) = @ARGV;
    sysopen(my $in, $source, O_RDONLY|O_NOFOLLOW|O_NONBLOCK) or die "Cannot open: $!\n";
    my @st = stat($in);
    S_ISREG($st[2]) && $st[7] <= $limit or die "Unsafe or oversized input\n";
    sysopen(my $out, $target, O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW, 0600) or die "Cannot create: $!\n";
    my $total = 0;
    while (1) {
      my $rem = $limit - $total + 1;
      my $n = sysread($in, my $buf, $rem < 65536 ? $rem : 65536);
      defined($n) or die "Read error: $!\n";
      last if $n == 0;
      $total += $n;
      $total <= $limit or die "Input grew beyond bound\n";
      my $off = 0;
      while ($off < $n) {
        my $w = syswrite($out, $buf, $n - $off, $off);
        defined($w) && $w > 0 or die "Write error: $!\n";
        $off += $w;
      }
    }
    close($out) or die "Close error: $!\n";
  ' "$1" "$2" "$3"
}

# ---------------------------------------------------------------------------
# Main install flow
# ---------------------------------------------------------------------------
shiki_need_perl

shiki_say "Detecting platform: ${shiki_os}/${shiki_arch}"
shiki_say "Role: ${shiki_role}"

shiki_version="$(shiki_resolve_version)"
shiki_say "Version: ${shiki_version}"

# Confirmation prompt
if [[ "$shiki_yes" -eq 0 && "$shiki_noninteractive" -eq 0 && -t 0 ]]; then
  if [[ "$shiki_role" == "daemon" ]]; then
    printf '\033[1;34m[shiki]\033[0m About to install Shikigami daemon %s for user %s. Continue? [y/N] ' \
      "$shiki_version" "$shiki_user"
  else
    printf '\033[1;34m[shiki]\033[0m About to install Shikigami client %s for user %s. Continue? [y/N] ' \
      "$shiki_version" "$(id -un)"
  fi
  read -r _answer </dev/tty
  case "$_answer" in
    [yY]*) ;;
    *) echo "bootstrap: aborted." >&2; exit 1 ;;
  esac
fi

# Temp workspace
shiki_tmp="$(mktemp -d /tmp/shiki-bootstrap.XXXXXXXX)"
trap 'rm -rf "$shiki_tmp"' EXIT HUP INT TERM
chmod 700 "$shiki_tmp"

# Archive name and URLs
shiki_archive_name="shikigami-${shiki_version}-${shiki_os}-${shiki_arch}.tar.gz"
shiki_archive_url="${SHIKI_RELEASES}/download/${shiki_version}/${shiki_archive_name}"
shiki_sig_url="${shiki_archive_url}.sig.json"

shiki_archive="${shiki_tmp}/${shiki_archive_name}"
shiki_sig="${shiki_tmp}/${shiki_archive_name}.sig.json"

# Download
shiki_say "Downloading ${shiki_archive_name}..."
shiki_download "$shiki_archive_url" "$shiki_archive" "$SHIKI_MAX_ARCHIVE"
shiki_ok "Download complete."

# Download and verify the detached signature. The signed payload supplies the
# checksum pin consumed by the bundle installer; sidecars are not trust roots.
shiki_say "Downloading and verifying Ed25519 signature..."
shiki_download "$shiki_sig_url" "$shiki_sig" "$SHIKI_MAX_META"
shiki_checksums_pin="$(shiki_verify_sig "$shiki_archive" "$shiki_sig" \
  "$SHIKI_EMBEDDED_PUB" "$shiki_version" "$shiki_os" "$shiki_arch")"
shiki_ok "Ed25519 signature verified. Key fingerprint: e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d"

# Verify pin format
[[ "$shiki_checksums_pin" =~ ^[0-9a-f]{64}$ ]] || \
  shiki_die "Invalid checksums pin extracted from signature."

# Extract bundle
shiki_bundle="${shiki_tmp}/bundle"
mkdir -p "$shiki_bundle"
shiki_say "Extracting bundle..."
tar -xzf "$shiki_archive" -C "$shiki_bundle"

# Locate extracted directory (the current release archive is flat, but retain
# support for a single top-level directory in older verified archives).
shiki_bundle_dir=""
for _d in "$shiki_bundle"/*/; do
  if [[ -f "${_d}SHA256SUMS" ]]; then
    shiki_bundle_dir="${_d%/}"
    break
  fi
done
if [[ -z "$shiki_bundle_dir" && -f "${shiki_bundle}/SHA256SUMS" ]]; then
  shiki_bundle_dir="$shiki_bundle"
fi
[[ -n "$shiki_bundle_dir" ]] || shiki_die "Cannot locate SHA256SUMS in extracted bundle."

# Determine script and role. Release archives contain these files at the
# bundle root; they are not under a scripts/ subdirectory.
if [[ "$shiki_role" == "daemon" ]]; then
  shiki_script="${shiki_bundle_dir}/install.sh"
  [[ -f "$shiki_script" ]] || shiki_die "install.sh not found in bundle."
else
  shiki_script="${shiki_bundle_dir}/install-client.sh"
  [[ -f "$shiki_script" ]] || shiki_die "install-client.sh not found in bundle."
fi


# Build shiki-install invocation args
shiki_install_args=(
  "--bundle=${shiki_bundle_dir}"
  "--checksums-sha256=${shiki_checksums_pin}"
  "--role=${shiki_role}"
  "--version=${shiki_version}"
)
[[ -n "$shiki_user"         ]] && shiki_install_args+=("--user=${shiki_user}")
[[ -n "$shiki_key_file"     ]] && shiki_install_args+=("--tailscale-key-file=${shiki_key_file}")
[[ -n "$shiki_token_output" ]] && shiki_install_args+=("--token-output=${shiki_token_output}")
[[ "$shiki_noninteractive" -eq 1 ]] && shiki_install_args+=("--non-interactive")
# Forward any extra flags
shiki_install_args+=("${shiki_pass_through[@]+"${shiki_pass_through[@]}"}")

# Make a private copy of the bundle's install script and run it
shiki_script_copy="${shiki_tmp}/install-runner.sh"
shiki_safe_copy "$shiki_script" "$shiki_script_copy" 1048576
chmod 700 "$shiki_script_copy"

shiki_say "Running installer..."
exec bash "$shiki_script_copy" "${shiki_install_args[@]}"
