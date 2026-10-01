#!/usr/bin/env bash
# Builds the ffmpeg spike: FFmpeg's libraries for WebAssembly, the driver
# linked into a module, its Go translation into spikes/ffmpeg/ffwasm, and the
# same driver natively as the baseline. Prints the sizes.
#
#   OPT=-Oz WOPT=-Oz SMALL= ASAN= spikes/ffmpeg/build.sh
#
# OPT is clang's optimization for FFmpeg and the driver, WOPT is wasm-opt's,
# SMALL=1 configures FFmpeg with --enable-small, and ASAN=1 also builds the
# native driver under AddressSanitizer.
set -euo pipefail
exec </dev/null
cd "$(dirname "$0")/../.."
repo=$(pwd)

OPT="${OPT:--Oz}"
WOPT="${WOPT:--Oz}"
SMALL="${SMALL:-}"

paths=$(./scripts/vendor.sh wasi-sdk binaryen ffmpeg-src zlib-src wasm2go)
tool() { printf '%s\n' "$paths" | sed -n "s/^$1=//p"; }
wasi=$(tool wasi-sdk)
wasm_opt=$(tool binaryen)
wasm2go=$(tool wasm2go)
src_tar=$(tool ffmpeg-src)
zlib_tar=$(tool zlib-src)

build="$repo/out/spikes/ffmpeg/build"
mkdir -p "$build"
src="$build/$(basename "$src_tar" .tar.xz)"
if [[ ! -f "$src/configure" ]]; then
  tar -xJf "$src_tar" -C "$build"
fi

# What probing and stripping need: the containers phones and browsers use,
# the parsers that fill in what a copy needs, and two bitstream filters
# muxers insert. Stills and posters add the decoders of the photo formats and
# phone video, the image formats' own demuxers, swscale, and the JPEG and PNG
# encoders, with zlib for PNG and TIFF. VP8, VP9 and AV1 keep their parsers,
# which give a WebM's size without decoding it; their decoders stay out, for
# their size, and browsers decode them.
components=(
  --disable-everything
  "--enable-demuxer=mov,matroska,mp3,flac,ogg,wav,aac,image_tiff_pipe,image_j2k_pipe,image_psd_pipe"
  "--enable-muxer=mov,mp4,ipod,matroska,webm,mp3,flac,ogg,oga,opus,wav"
  "--enable-parser=h264,hevc,aac,mpegaudio,opus,vorbis,flac,vp8,vp9,av1,mjpeg,ac3"
  "--enable-bsf=aac_adtstoasc,vp9_superframe"
  "--enable-decoder=hevc,h264,mjpeg,tiff,jpeg2000,psd"
  "--enable-encoder=mjpeg,png"
  --enable-zlib
)
common=(
  --disable-programs --disable-doc --disable-network --disable-autodetect --disable-debug
  --disable-avdevice --disable-avfilter --disable-swresample
)
if [[ -n "$SMALL" ]]; then
  common+=(--enable-small)
fi
profile="media${OPT}${SMALL:+-small}"

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

# build_zlib DIR CC AR RANLIB [CFLAGS]: zlib's static library into DIR/prefix.
build_zlib() {
  local dir="$1" cc="$2" ar="$3" ranlib="$4" cflags="${5:-}"
  [[ -f "$dir/prefix/lib/libz.a" ]] && return
  rm -rf "$dir" && mkdir -p "$dir"
  tar -xJf "$zlib_tar" -C "$dir" --strip-components=1
  (cd "$dir" && CC="$cc" AR="$ar" RANLIB="$ranlib" CFLAGS="-O2 $cflags" ./configure --static --prefix="$dir/prefix" \
    >configure.log 2>&1 && make -j"$(nproc)" libz.a >make.log 2>&1 && make install >install.log 2>&1) ||
    { tail -20 "$dir/configure.log" "$dir/make.log" >&2; return 1; }
}

echo "== FFmpeg for WebAssembly ($profile)"
sysroot="$wasi/share/wasi-sysroot"
build_zlib "$build/zlib-wasm" "$wasi/bin/clang --target=wasm32-wasip1 --sysroot=$sysroot" \
  "$wasi/bin/llvm-ar" "$wasi/bin/llvm-ranlib"
build_zlib "$build/zlib-native" cc ar ranlib
wasm="$build/wasm-$profile"
configure_build "$wasm" \
  --enable-cross-compile --target-os=none --arch=wasm32 \
  --cc="$wasi/bin/clang" --ar="$wasi/bin/llvm-ar" --ranlib="$wasi/bin/llvm-ranlib" \
  --nm="$wasi/bin/llvm-nm" --strip="$wasi/bin/llvm-strip" \
  --extra-cflags="--target=wasm32-wasip1 --sysroot=$sysroot -I$build/zlib-wasm/prefix/include" --optflags="$OPT" \
  --extra-ldflags="--target=wasm32-wasip1 --sysroot=$sysroot -L$build/zlib-wasm/prefix/lib" \
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
  -L"$wasm/prefix/lib" -L"$build/zlib-wasm/prefix/lib" -lavformat -lavcodec -lswscale -lavutil -lz -lm \
  -lwasi-emulated-process-clocks \
  -Wl,--import-memory -Wl,--stack-first -Wl,-z,stack-size=1048576 -Wl,--max-memory=4294967296 \
  -Wl,--export=malloc -Wl,--export=free
"$wasm_opt" "$WOPT" "$mod/raw.wasm" -o "$mod/dm.wasm" --strip-debug --strip-producers

# min_pages_go WASM PKG: a Go file stating the memory the module imports at
# least, which the host must give it before it runs.
min_pages_go() {
  local pages
  pages=$("$(dirname "$wasm_opt")/wasm-dis" "$1" | sed -n 's/.*(import "env" "memory" (memory [^ ]* \([0-9]*\).*/\1/p')
  [[ -n "$pages" ]] || { printf 'error: no memory import in %s\n' "$1" >&2; return 1; }
  printf '// Code generated by build.sh. DO NOT EDIT.\n\npackage %s\n\n// MinPages is the memory the module needs before it runs, in 64 KiB pages.\nconst MinPages = %s\n' "$2" "$pages"
}

echo "== the translation"
rm -rf spikes/ffmpeg/ffwasm && mkdir -p spikes/ffmpeg/ffwasm
"$wasm2go" -pkg ffwasm -unsafe -embed -o spikes/ffmpeg/ffwasm/ffwasm.go "$mod/dm.wasm"
min_pages_go "$mod/dm.wasm" ffwasm >spikes/ffmpeg/ffwasm/pages.go

# A4's module of traps, built as the driver is, and translated with and
# without -unsafe.
echo "== the trap module"
traps="$build/traps"
mkdir -p "$traps"
"$wasi/bin/clang" --target=wasm32-wasip1 --sysroot="$sysroot" "$OPT" -mexec-model=reactor \
  spikes/ffmpeg/traps/traps.c -o "$traps/raw.wasm" \
  -Wl,--import-memory -Wl,--stack-first -Wl,-z,stack-size=1048576 -Wl,--max-memory=4294967296
"$wasm_opt" "$WOPT" "$traps/raw.wasm" -o "$traps/traps.wasm" --strip-debug --strip-producers
for variant in unsafe safe; do
  flag=()
  [[ "$variant" == unsafe ]] && flag=(-unsafe)
  rm -rf "spikes/ffmpeg/traps/$variant" && mkdir -p "spikes/ffmpeg/traps/$variant"
  "$wasm2go" -pkg "traps$variant" "${flag[@]}" -o "spikes/ffmpeg/traps/$variant/traps.go" "$traps/traps.wasm"
  min_pages_go "$traps/traps.wasm" "traps$variant" >"spikes/ffmpeg/traps/$variant/pages.go"
done
(cd spikes && go build -o "$repo/out/spikes/ffmpeg/ffspike" ./ffmpeg)

# The same C code natively, without assembly as in the module, so the
# difference is what the translation costs.
echo "== the native baseline"
native="$build/native-$profile"
zn="$build/zlib-native/prefix"
configure_build "$native" "${common[@]}" "${components[@]}" \
  --disable-pthreads --disable-asm --optflags="$OPT" --extra-cflags="-I$zn/include" --extra-ldflags="-L$zn/lib"
cc -O2 -I"$native/prefix/include" spikes/ffmpeg/driver/driver.c spikes/ffmpeg/driver/host.c \
  -o "$repo/out/spikes/ffmpeg/dmnative" -L"$native/prefix/lib" -L"$zn/lib" \
  -lavformat -lavcodec -lswscale -lavutil -lz -lm

# The native driver again, with FFmpeg's code and the driver's under
# AddressSanitizer, which A4 runs damaged files through to see which of them
# hit real memory bugs.
if [[ -n "${ASAN:-}" ]]; then
  echo "== the native baseline under AddressSanitizer"
  asan="$build/native-asan"
  configure_build "$asan" "${common[@]}" "${components[@]}" \
    --disable-pthreads --disable-asm --toolchain=gcc-asan --optflags=-O1 --extra-cflags="-I$zn/include" \
    --extra-ldflags="-L$zn/lib"
  cc -O1 -g -fsanitize=address -fno-omit-frame-pointer -I"$asan/prefix/include" \
    spikes/ffmpeg/driver/driver.c spikes/ffmpeg/driver/host.c \
    -o "$repo/out/spikes/ffmpeg/dmnative-asan" -L"$asan/prefix/lib" -L"$zn/lib" \
    -lavformat -lavcodec -lswscale -lavutil -lz -lm
fi

printf 'module: raw %s bytes, optimized %s bytes; Go source %s bytes, data %s bytes\n' \
  "$(stat -c %s "$mod/raw.wasm")" "$(stat -c %s "$mod/dm.wasm")" \
  "$(stat -c %s spikes/ffmpeg/ffwasm/ffwasm.go)" "$(stat -c %s spikes/ffmpeg/ffwasm/ffwasm.dat)"
