#!/usr/bin/env bash

# Builds RNNoise for the page (M3): the noise suppression a member can turn
# on in a call, which runs in an AudioWorklet. A reactor module with no main
# and no imports, exporting what the worklet calls, written into the page's
# assets, which the repository keeps. Every input is pinned in
# scripts/vendor.sh, and the build doesn't depend on where it runs, so CI
# builds the same bytes.
#
# The code is RNNoise's release; the model is the one its v0.2 tag names,
# unpacked over the release's own, without the float copies it keeps for
# debugging. RNNoise's generic vector code, which WebAssembly builds take,
# includes Opus's os_support.h, which RNNoise doesn't carry, so
# internal/ui/rnnoise supplies it.
#
# Usage:
#   ./scripts/rnnoise.sh           # rebuild internal/ui/assets/wasm/rnnoise.wasm
#   ./scripts/rnnoise.sh --check   # rebuild elsewhere, and fail if it differs

set -euo pipefail
exec </dev/null
umask 022
export LC_ALL=C

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

OUT=internal/ui/assets/wasm/rnnoise.wasm
# regular or little: RNNoise's two models.
MODEL=regular
SOURCES=(denoise.c rnn.c pitch.c kiss_fft.c celt_lpc.c nnet.c nnet_default.c parse_lpcnet_weights.c rnnoise_tables.c rnnoise_data.c)
EXPORTS=(rnnoise_create rnnoise_destroy rnnoise_process_frame malloc free)

# build OUT: build the module into OUT.
build() {
  local out="$1"
  local paths
  paths=$(./scripts/vendor.sh rnnoise-src rnnoise-model wasi-sdk binaryen)
  tool() { printf '%s\n' "$paths" | sed -n "s/^$1=//p"; }
  local wasi wasm_opt work
  wasi=$(tool wasi-sdk)
  wasm_opt=$(tool binaryen)
  work=$(mktemp -d "$ROOT/out/rnnoise.XXXXXX")
  # shellcheck disable=SC2064 # expand now, while work is set
  trap "rm -rf '$work'" RETURN
  mkdir -p "$work/src" "$work/model"
  tar -xzf "$(tool rnnoise-src)" -C "$work/src" --strip-components=1
  tar -xzf "$(tool rnnoise-model)" -C "$work/model"
  local suffix=""
  [[ "$MODEL" == little ]] && suffix=_little
  cp "$work/model/src/rnnoise_data$suffix.c" "$work/src/src/rnnoise_data.c"
  cp "$work/model/src/rnnoise_data$suffix.h" "$work/src/src/rnnoise_data.h"
  local exports=() e
  for e in "${EXPORTS[@]}"; do exports+=("-Wl,--export=$e"); done
  # The stack comes first, so running out of it traps instead of running
  # into the model's weights.
  (cd "$work/src/src" && "$wasi/bin/clang" --target=wasm32-wasip1 --sysroot="$wasi/share/wasi-sysroot" \
    -ffile-prefix-map="$work"=. -O3 -msimd128 -DRNNOISE_BUILD -DDISABLE_DEBUG_FLOAT -Wno-\#warnings \
    -I../include -I. -I"$ROOT/internal/ui/rnnoise" -mexec-model=reactor "${SOURCES[@]}" -o "$work/raw.wasm" -lm \
    -Wl,--stack-first -Wl,-z,stack-size=262144 "${exports[@]}")
  "$wasm_opt" -O3 --enable-simd "$work/raw.wasm" -o "$out" --strip-debug --strip-producers
  if "$(dirname "$wasm_opt")/wasm-dis" "$out" | grep -q '(import '; then
    printf 'error: the module imports something; the worklet gives it nothing\n' >&2
    return 1
  fi
}

mkdir -p out
case "${1:-}" in
  "")
    build "$OUT"
    printf 'wrote %s (%s bytes)\n' "$OUT" "$(stat -c %s "$OUT")"
    ;;
  --check)
    tmp=$(mktemp "$ROOT/out/rnnoise-check.XXXXXX")
    trap 'rm -f "$tmp"' EXIT
    build "$tmp"
    if ! cmp -s "$tmp" "$OUT"; then
      printf 'error: %s differs from what its inputs build; run ./scripts/rnnoise.sh\n' "$OUT" >&2
      exit 1
    fi
    printf '%s is current\n' "$OUT"
    ;;
  *)
    echo "usage: $0 [--check]" >&2
    exit 2
    ;;
esac
