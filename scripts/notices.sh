#!/usr/bin/env bash

# Writes the third-party notices the binary carries, internal/build/notices.txt:
# the license of everything a release binary includes besides Dens's own
# code. dens licenses prints it, and the page links to it.
#
# The Go modules are those the binary links on every platform it's released
# for, with each license read from the module cache. The rest come with the
# pinned inputs in scripts/vendor.sh, not all of whose downloads carry their
# license, so scripts/notices/ keeps a copy of each, taken from the project's
# source at the pinned version. A new module or input shows up in review as a
# new license.
#
# Usage:
#   ./scripts/notices.sh           # write internal/build/notices.txt
#   ./scripts/notices.sh --check   # fail if it isn't current

set -euo pipefail
exec </dev/null
umask 022
export LC_ALL=C

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

OUT=internal/build/notices.txt
TEXTS=scripts/notices
TARGETS=(linux/amd64 linux/arm64 windows/amd64 windows/arm64)

# shellcheck source=vendor.sh
source scripts/vendor.sh

rule() {
  printf '%s\n' "================================================================================"
}

# section TITLE TEXT_FILE [LINE...]: one entry, its title and any lines about
# it, then its license.
section() {
  local title=$1 file=$2
  shift 2
  printf '\n'
  rule
  printf '%s\n' "$title"
  local line
  for line in "$@"; do
    printf '%s\n' "$line"
  done
  printf '%s\n' "--------------------------------------------------------------------------------"
  sed -e 's/[[:space:]]*$//' "$file"
}

# license DIR: the license file at the root of a module.
license() {
  local name
  for name in LICENSE LICENSE.txt LICENSE.md LICENCE COPYING; do
    if [[ -f "$1/$name" ]]; then
      printf '%s\n' "$1/$name"
      return
    fi
  done
  echo "error: no license file in $1" >&2
  return 1
}

generate() {
  local target modules=() mod
  for target in "${TARGETS[@]}"; do
    while IFS= read -r mod; do
      [[ -n "$mod" ]] && modules+=("$mod")
    done < <(GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 go list -deps \
      -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}' ./cmd)
  done
  mapfile -t modules < <(printf '%s\n' "${modules[@]}" | sort -u)

  printf '%s\n' "Third-party notices"
  printf '\n%s\n' "Dens is under the MIT license (LICENSE.md in its source). Its binary also"
  printf '%s\n' "includes the software below, each under the license that follows its name."

  section "Go: the standard library and runtime" "$(go env GOROOT)/LICENSE" "https://go.dev"
  local path version dir
  for mod in "${modules[@]}"; do
    read -r path version <<<"$mod"
    dir=$(go list -m -f '{{.Dir}}' "$path")
    [[ -n "$dir" ]] || { echo "error: $path isn't in the module cache; run go mod download" >&2; return 1; }
    section "$path $version" "$(license "$dir")"
  done

  section "FFmpeg ${DEFAULT_FFMPEG_VERSION}, in the media module" "$TEXTS/ffmpeg.txt" \
    "https://ffmpeg.org" \
    "Its libraries are under the GNU LGPL 2.1 or later. Each release publishes" \
    "their source as the binaries include them, as ffmpeg-source.tar.xz."
  section "zlib ${DEFAULT_ZLIB_VERSION}, in the media module" "$TEXTS/zlib.txt" "https://zlib.net"
  section "wasi-libc, from wasi-sdk ${DEFAULT_WASI_SDK_VERSION}, in the media module" "$TEXTS/wasi-libc.txt" \
    "https://github.com/WebAssembly/wasi-libc" \
    "Dens takes it under the MIT license, one of the three it offers."
  section "musl, as wasi-libc includes it" "$TEXTS/musl.txt" "https://musl.libc.org"
  section "cloudlibc, as wasi-libc includes it" "$TEXTS/cloudlibc.txt" "https://github.com/NuxiNL/cloudlibc"
  section "RNNoise ${DEFAULT_RNNOISE_VERSION}, with its model ${DEFAULT_RNNOISE_MODEL_VERSION}, in the page" "$TEXTS/rnnoise.txt" \
    "https://gitlab.xiph.org/xiph/rnnoise" \
    "Its model comes from the same project, beside its code."
  section "Preact ${DEFAULT_PREACT_VERSION}, in the page" "$TEXTS/preact.txt" "https://preactjs.com"
  section "Tailwind CSS ${DEFAULT_TAILWIND_VERSION#v}, in the page's styles" "$TEXTS/tailwindcss.txt" "https://tailwindcss.com"
  section "daisyUI ${DEFAULT_DAISYUI_VERSION#v}, in the page's styles" "$TEXTS/daisyui.txt" "https://daisyui.com"
}

case "${1:-}" in
  "")
    generate > "$OUT.tmp"
    mv "$OUT.tmp" "$OUT"
    echo "wrote $OUT"
    ;;
  --check)
    tmp=$(mktemp)
    trap 'rm -f "$tmp"' EXIT
    generate > "$tmp"
    if ! cmp -s "$tmp" "$OUT"; then
      diff -u "$OUT" "$tmp" | head -40 >&2 || :
      echo "error: $OUT isn't current; run ./scripts/notices.sh" >&2
      exit 1
    fi
    echo "$OUT is current"
    ;;
  *)
    echo "usage: $0 [--check]" >&2
    exit 2
    ;;
esac
