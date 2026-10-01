#!/usr/bin/env bash
# Builds the ffmpeg spike: FFmpeg's libraries for WebAssembly, the driver
# linked into a module, its Go translation into spikes/ffmpeg/ffwasm, and the
# same driver natively as the baseline. Prints the sizes.
#
#   OPT=-Oz WOPT=-Oz SMALL= spikes/ffmpeg/build.sh
#
# OPT is clang's optimization for FFmpeg and the driver, WOPT is wasm-opt's, and
# SMALL=1 configures FFmpeg with --enable-small.
set -euo pipefail
exec </dev/null
cd "$(dirname "$0")/../.."
repo=$(pwd)

OPT="${OPT:--Oz}"
WOPT="${WOPT:--Oz}"
SMALL="${SMALL:-}"

paths=$(./scripts/vendor.sh wasi-sdk binaryen ffmpeg-src wasm2go)
tool() { printf '%s\n' "$paths" | sed -n "s/^$1=//p"; }
wasi=$(tool wasi-sdk)
wasm_opt=$(tool binaryen)
wasm2go=$(tool wasm2go)
src_tar=$(tool ffmpeg-src)

build="$repo/out/spikes/ffmpeg/build"
mkdir -p "$build"
src="$build/$(basename "$src_tar" .tar.xz)"
if [[ ! -f "$src/configure" ]]; then
  tar -xJf "$src_tar" -C "$build"
fi

# What probing and stripping need: the containers phones and browsers use,
# the parsers that fill in what a copy needs, and two bitstream filters
# muxers insert. No decoders or encoders.
components=(
  --disable-everything
  "--enable-demuxer=mov,matroska,mp3,flac,ogg,wav,aac"
  "--enable-muxer=mov,mp4,ipod,matroska,webm,mp3,flac,ogg,oga,opus,wav"
  "--enable-parser=h264,hevc,aac,mpegaudio,opus,vorbis,flac,vp8,vp9,av1,mjpeg,ac3"
  "--enable-bsf=aac_adtstoasc,vp9_superframe"
)
common=(
  --disable-programs --disable-doc --disable-network --disable-autodetect --disable-debug
  --disable-avdevice --disable-avfilter --disable-swscale --disable-swresample
)
if [[ -n "$SMALL" ]]; then
  common+=(--enable-small)
fi
profile="strip${OPT}${SMALL:+-small}"

# configure_build DIR ARGS...: configure FFmpeg into DIR/prefix and build it,
# unless DIR already holds a build with the same arguments.
configure_build() {
  local dir="$1"
  shift
  local stamp="$dir/.args"
  if [[ -f "$stamp" && "$(cat "$stamp")" == "$*" && -f "$dir/prefix/lib/libavformat.a" ]]; then
    return
  fi
  rm -rf "$dir" && mkdir -p "$dir"
  (cd "$dir" && "$src/configure" --prefix="$dir/prefix" "$@" >configure.log 2>&1) ||
    { tail -20 "$dir/configure.log" >&2; return 1; }
  make -C "$dir" -j"$(nproc)" >"$dir/make.log" 2>&1 || { tail -20 "$dir/make.log" >&2; return 1; }
  make -C "$dir" install >"$dir/install.log" 2>&1
  printf '%s' "$*" >"$stamp"
}

echo "== FFmpeg for WebAssembly ($profile)"
sysroot="$wasi/share/wasi-sysroot"
wasm="$build/wasm-$profile"
configure_build "$wasm" \
  --enable-cross-compile --target-os=none --arch=wasm32 \
  --cc="$wasi/bin/clang" --ar="$wasi/bin/llvm-ar" --ranlib="$wasi/bin/llvm-ranlib" \
  --nm="$wasi/bin/llvm-nm" --strip="$wasi/bin/llvm-strip" \
  --extra-cflags="--target=wasm32-wasip1 --sysroot=$sysroot" --optflags="$OPT" \
  --extra-ldflags="--target=wasm32-wasip1 --sysroot=$sysroot" \
  --disable-pthreads --disable-w32threads --disable-os2threads --disable-asm --disable-runtime-cpudetect \
  "${common[@]}" "${components[@]}"

echo "== the module"
mod="$build/mod-$profile"
mkdir -p "$mod"
# A reactor module, with no main, that imports its memory from the host so the
# host sets its cap. The stack comes first, so overflowing it traps instead of
# running into the module's data.
"$wasi/bin/clang" --target=wasm32-wasip1 --sysroot="$sysroot" "$OPT" -mexec-model=reactor \
  -I"$wasm/prefix/include" spikes/ffmpeg/driver/driver.c -o "$mod/raw.wasm" \
  -L"$wasm/prefix/lib" -lavformat -lavcodec -lavutil -lm -lwasi-emulated-process-clocks \
  -Wl,--import-memory -Wl,--stack-first -Wl,-z,stack-size=1048576 -Wl,--max-memory=4294967296 \
  -Wl,--export=malloc -Wl,--export=free
"$wasm_opt" "$WOPT" "$mod/raw.wasm" -o "$mod/dm.wasm" --strip-debug --strip-producers

echo "== the translation"
rm -rf spikes/ffmpeg/ffwasm && mkdir -p spikes/ffmpeg/ffwasm
"$wasm2go" -pkg ffwasm -unsafe -embed -o spikes/ffmpeg/ffwasm/ffwasm.go "$mod/dm.wasm"
(cd spikes && go build -o "$repo/out/spikes/ffmpeg/ffspike" ./ffmpeg)

# The same C code natively, without assembly as in the module, so the
# difference is what the translation costs.
echo "== the native baseline"
native="$build/native-$profile"
configure_build "$native" "${common[@]}" "${components[@]}" --disable-pthreads --disable-asm \
  --optflags="$OPT"
cc -O2 -I"$native/prefix/include" spikes/ffmpeg/driver/driver.c spikes/ffmpeg/driver/host.c \
  -o "$repo/out/spikes/ffmpeg/dmnative" -L"$native/prefix/lib" -lavformat -lavcodec -lavutil -lm

printf 'module: raw %s bytes, optimized %s bytes; Go source %s bytes, data %s bytes\n' \
  "$(stat -c %s "$mod/raw.wasm")" "$(stat -c %s "$mod/dm.wasm")" \
  "$(stat -c %s spikes/ffmpeg/ffwasm/ffwasm.go)" "$(stat -c %s spikes/ffmpeg/ffwasm/ffwasm.dat)"
