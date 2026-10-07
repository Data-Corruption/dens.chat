#!/usr/bin/env bash

# Builds RNNoise for WebAssembly, as the page would run it: a reactor module
# with no main, exporting what the AudioWorklet calls. Four variants, for
# the browsers to compare: the regular and the little model, each with and
# without WebAssembly SIMD. The model is the one RNNoise's v0.2 tag names,
# unpacked over the release's own, without its debugging copies in floats.
#
# Usage: spikes/rnnoise/build.sh   (from the repository's root)
# Output: out/spikes/rnnoise/rnnoise-{regular,little}-{simd,scalar}.wasm

set -euo pipefail
exec </dev/null
export LC_ALL=C

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT"
OUT=out/spikes/rnnoise
OPT=${OPT:--O3}

paths=$(./scripts/vendor.sh rnnoise-src rnnoise-model wasi-sdk binaryen)
tool() { printf '%s\n' "$paths" | sed -n "s/^$1=//p"; }
wasi=$(tool wasi-sdk)
wasm_opt=$(tool binaryen)

work=$OUT/work
rm -rf "$work"
mkdir -p "$work/src" "$work/model"
tar -xzf "$(tool rnnoise-src)" -C "$work/src" --strip-components=1
tar -xzf "$(tool rnnoise-model)" -C "$work/model"

SOURCES=(denoise.c rnn.c pitch.c kiss_fft.c celt_lpc.c nnet.c nnet_default.c parse_lpcnet_weights.c rnnoise_tables.c)
EXPORTS=(rnnoise_create rnnoise_destroy rnnoise_process_frame rnnoise_get_frame_size malloc free)

for model in regular little; do
  suffix=""
  [[ "$model" == little ]] && suffix=_little
  cp "$work/model/src/rnnoise_data$suffix.c" "$work/src/src/rnnoise_data.c"
  cp "$work/model/src/rnnoise_data$suffix.h" "$work/src/src/rnnoise_data.h"
  for simd in simd scalar; do
    flags=()
    [[ "$simd" == simd ]] && flags+=(-msimd128)
    name="rnnoise-$model-$simd"
    exports=()
    for e in "${EXPORTS[@]}"; do exports+=("-Wl,--export=$e"); done
    "$wasi/bin/clang" --target=wasm32-wasip1 --sysroot="$wasi/share/wasi-sysroot" \
      -ffile-prefix-map="$ROOT"=. "$OPT" "${flags[@]}" -DRNNOISE_BUILD -DDISABLE_DEBUG_FLOAT \
      -I"$work/src/include" -I"$work/src/src" -I"$ROOT/spikes/rnnoise" -mexec-model=reactor \
      "${SOURCES[@]/#/$work/src/src/}" "$work/src/src/rnnoise_data.c" -o "$work/$name.raw.wasm" -lm \
      -Wl,--stack-first -Wl,-z,stack-size=262144 "${exports[@]}"
    opt_flags=()
    [[ "$simd" == simd ]] && opt_flags+=(--enable-simd)
    "$wasm_opt" "$OPT" "${opt_flags[@]}" "$work/$name.raw.wasm" -o "$OUT/$name.wasm" --strip-debug --strip-producers
    printf '%s: %s bytes, %s gzipped\n' "$name" "$(stat -c %s "$OUT/$name.wasm")" "$(gzip -9c "$OUT/$name.wasm" | wc -c)"
  done
done
echo "imports of rnnoise-regular-simd:"
"$(dirname "$wasm_opt")/wasm-dis" "$OUT/rnnoise-regular-simd.wasm" | grep '(import ' || echo "  none"
