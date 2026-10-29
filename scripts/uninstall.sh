#!/bin/bash
set -euo pipefail
export PATH=/usr/bin:/bin:/usr/sbin:/sbin
# Obtain this script itself from a trusted checkout or verified release archive.
# The digest below must come from an independent trusted channel, never from a
# checksum file fetched alongside an otherwise unauthenticated download.
shiki_bundle=''
shiki_pin=''
shiki_args=("$@")
while (($#)); do
  case "$1" in
    --bundle=*) shiki_bundle="${1#*=}" ;;
    --bundle) shift; (($#)) || { echo 'Missing --bundle value.' >&2; exit 2; }; shiki_bundle="$1" ;;
    --checksums-sha256=*) shiki_pin="${1#*=}" ;;
    --checksums-sha256) shift; (($#)) || { echo 'Missing checksum pin.' >&2; exit 2; }; shiki_pin="$1" ;;
  esac
  shift
done
[[ "$shiki_bundle" = /* && "$shiki_pin" =~ ^[0-9a-f]{64}$ ]] || {
  echo 'Supply --bundle=<absolute directory> and independently trusted --checksums-sha256=<digest>.' >&2; exit 2;
}
shiki_hash() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'
  else echo 'A SHA-256 utility is required.' >&2; return 1; fi
}
# A nonblocking, bounded copy is required before any untrusted helper executes.
# Perl is supplied by macOS and many Linux bases; never silently fall back to cp.
[[ -x /usr/bin/perl ]] || { echo 'Secure bootstrap needs /usr/bin/perl; use a separately verified shiki-install directly.' >&2; exit 1; }
shiki_copy() {
  /usr/bin/perl -MFcntl=:DEFAULT,:mode -e '
    my ($source, $target, $limit) = @ARGV;
    sysopen(my $in, $source, O_RDONLY|O_NOFOLLOW|O_NONBLOCK) or die "Cannot open bootstrap input\n";
    my @info = stat($in);
    S_ISREG($info[2]) && $info[7] <= $limit or die "Unsafe or oversized bootstrap input\n";
    sysopen(my $out, $target, O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW, 0600) or die "Cannot create private bootstrap copy\n";
    my $total = 0;
    while (1) {
      my $remaining = $limit - $total + 1;
      my $count = sysread($in, my $buffer, $remaining < 65536 ? $remaining : 65536);
      defined($count) or die "Cannot read bootstrap input\n";
      last if $count == 0;
      $total += $count;
      $total <= $limit or die "Bootstrap input grew beyond its bound\n";
      my $offset = 0;
      while ($offset < $count) {
        my $written = syswrite($out, $buffer, $count - $offset, $offset);
        defined($written) && $written > 0 or die "Cannot write private bootstrap copy\n";
        $offset += $written;
      }
    }
    close($out) or die "Cannot close private bootstrap copy\n";
    close($in);
  ' "$1" "$2" "$3"
}
shiki_bootstrap_dir="$(mktemp -d /tmp/shiki-install.XXXXXXXX)"
trap 'rm -rf "$shiki_bootstrap_dir"' EXIT HUP INT TERM
chmod 700 "$shiki_bootstrap_dir"
[[ -f "$shiki_bundle/SHA256SUMS" && ! -L "$shiki_bundle/SHA256SUMS" ]] || { echo 'Unsafe checksum manifest.' >&2; exit 1; }
shiki_copy "$shiki_bundle/SHA256SUMS" "$shiki_bootstrap_dir/SHA256SUMS" 65536
[[ "$(shiki_hash "$shiki_bootstrap_dir/SHA256SUMS")" = "$shiki_pin" ]] || { echo 'Trusted release checksum mismatch.' >&2; exit 1; }
shiki_installer_hash="$(awk '$2 == "shiki-install" { n++; hash=$1 } END { if (n != 1) exit 1; print hash }' "$shiki_bootstrap_dir/SHA256SUMS")"
[[ "$shiki_installer_hash" =~ ^[0-9a-f]{64}$ && -f "$shiki_bundle/shiki-install" && ! -L "$shiki_bundle/shiki-install" ]] || { echo 'Unsafe installer payload.' >&2; exit 1; }
shiki_copy "$shiki_bundle/shiki-install" "$shiki_bootstrap_dir/shiki-install" 1073741824
chmod 700 "$shiki_bootstrap_dir/shiki-install"
[[ "$(shiki_hash "$shiki_bootstrap_dir/shiki-install")" = "$shiki_installer_hash" ]] || { echo 'Installer payload checksum mismatch.' >&2; exit 1; }
case "$(basename "$0")" in
  install-client.sh) shiki_operation=install; shiki_role=client ;;
  uninstall.sh) shiki_operation=uninstall; shiki_role=client ;;
  *) shiki_operation=install; shiki_role=daemon ;;
esac
# Bundle and pin authenticated this temporary helper above. They are bootstrap
# inputs, not overrides for the installed receipt selected by uninstall.
if [[ "$shiki_operation" = uninstall ]]; then
  set -- "${shiki_args[@]}"
  shiki_args=()
  while (($#)); do
    case "$1" in
      --bundle=*|--checksums-sha256=*) shift ;;
      --bundle|--checksums-sha256) shift 2 ;;
      *) shiki_args+=("$1"); shift ;;
    esac
  done
fi
"$shiki_bootstrap_dir/shiki-install" "$shiki_operation" --role "$shiki_role" "${shiki_args[@]}"
