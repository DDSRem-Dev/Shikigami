#!/usr/bin/env bash
# Shared by the regular and embedded build entrypoints; source before writing outputs.
shiki_version="${SHIKI_VERSION:-dev}"
shiki_commit="${SHIKI_COMMIT:-unknown}"
shiki_built_at="${SHIKI_BUILT_AT:-unknown}"
if [[ ! "$shiki_version" =~ ^[a-zA-Z0-9._+-]+$ ]]; then
  echo 'SHIKI_VERSION contains unsupported characters.' >&2
  exit 1
fi
# Metadata is explicit: a dirty checkout must never inherit an unrelated HEAD.
# Restrict every value before forming the linker's space-delimited flag string.
if [[ "$shiki_commit" != unknown && ! "$shiki_commit" =~ ^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$ ]]; then
  echo 'SHIKI_COMMIT must be unknown or a full 40/64-character hexadecimal commit ID.' >&2
  exit 1
fi
if [[ "$shiki_built_at" != unknown ]]; then
  if [[ ! "$shiki_built_at" =~ ^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]Z$ ]]; then
    echo 'SHIKI_BUILT_AT must be unknown or a UTC timestamp in YYYY-MM-DDTHH:MM:SSZ format.' >&2
    exit 1
  fi
  shiki_year=$((10#${shiki_built_at:0:4}))
  shiki_month=$((10#${shiki_built_at:5:2}))
  shiki_day=$((10#${shiki_built_at:8:2}))
  shiki_days=31
  case "$shiki_month" in
    4|6|9|11) shiki_days=30 ;;
    2)
      shiki_days=28
      if (( shiki_year % 400 == 0 || (shiki_year % 4 == 0 && shiki_year % 100 != 0) )); then
        shiki_days=29
      fi
      ;;
  esac
  if (( shiki_day > shiki_days )); then
    echo 'SHIKI_BUILT_AT contains an invalid calendar date.' >&2
    exit 1
  fi
fi
shiki_buildinfo='github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo'
shiki_ldflags="-X $shiki_buildinfo.Version=$shiki_version -X $shiki_buildinfo.Commit=$shiki_commit -X $shiki_buildinfo.BuiltAt=$shiki_built_at"
