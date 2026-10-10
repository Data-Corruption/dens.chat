#!/usr/bin/env bash

# The video spike's build of option A: libaom's realtime AV1 encoder in the
# media module. It builds libaom and FFmpeg for WebAssembly as
# scripts/ffmpeg.sh builds the module, with libaom's encoder and the spike's
# transcoder (driver/transcode.c) linked beside the module's own driver,
# translates the result with wasm2go, and reports what each step adds. The
# same C also builds natively, without SIMD, for what the translation costs.
#
# Usage:
#   spikes/video/build.sh [8|10]   # libaom for eight-bit video (default), or for ten
#
# AOM_OPT sets libaom's optimization (default -O2), OPT FFmpeg's and the
# drivers' (default -Oz, as the module's), and VARIANT names a build of
# other options. Everything lands in out/spikes/video/build-BITS[-VARIANT]/,
# and the translation in the gitignored spikes/video/aomwasm/, which
# spikes/video/bench runs.

set -euo pipefail
exec </dev/null
umask 022
export LC_ALL=C

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT"
BITS=${1:-8}
[[ "$BITS" == 8 || "$BITS" == 10 ]] || { echo "usage: $0 [8|10]" >&2; exit 2; }
HIGH=0
[[ "$BITS" == 10 ]] && HIGH=1
AOM_OPT=${AOM_OPT:--O2}
OPT=${OPT:--Oz}
SPIKE=spikes/video
WORK="$ROOT/out/spikes/video/build-$BITS${VARIANT:+-$VARIANT}"
DRIVER_DIR=internal/media/ffmpeg/driver
mkdir -p "$WORK"

paths=$(./scripts/vendor.sh ffmpeg-src zlib-src aom-src cmake wasi-sdk binaryen wasm2go)
tool() { printf '%s\n' "$paths" | sed -n "s/^$1=//p"; }
wasi=$(tool wasi-sdk)
cmake=$(tool cmake)
wasm_opt=$(tool binaryen)
wasm2go=$(tool wasm2go)

# The module's components, as scripts/ffmpeg.sh lists them, and libaom's
# encoder.
COMPONENTS=(
  --disable-everything
  "--enable-demuxer=mov,matroska,mp3,flac,ogg,wav,aac,image_tiff_pipe,image_j2k_pipe,image_psd_pipe,image_jpeg_pipe,image_png_pipe"
  "--enable-muxer=mov,mp4,ipod,matroska,webm,mp3,flac,ogg,oga,opus,wav"
  "--enable-parser=h264,hevc,aac,mpegaudio,opus,vorbis,flac,vp8,vp9,av1,mjpeg,ac3"
  "--enable-bsf=aac_adtstoasc,vp9_superframe"
  "--enable-decoder=hevc,h264,mjpeg,tiff,jpeg2000,psd,png"
  "--enable-encoder=mjpeg,png,libaom_av1"
  --enable-zlib --enable-libaom
)
COMMON=(
  --disable-programs --disable-doc --disable-network --disable-autodetect --disable-debug
  --disable-avdevice --disable-avfilter --disable-swresample
  --disable-pthreads --disable-w32threads --disable-os2threads --disable-asm --disable-runtime-cpudetect
)
# libaom's realtime encoder alone: no decoder, threads, SIMD or tools, and no
# C++ (libyuv, libwebm).
AOM=(
  -DAOM_TARGET_CPU=generic -DCONFIG_RUNTIME_CPU_DETECT=0 -DCONFIG_MULTITHREAD=0 -DCONFIG_AV1_DECODER=0
  -DCONFIG_REALTIME_ONLY=1 "-DCONFIG_AV1_HIGHBITDEPTH=$HIGH" -DCONFIG_LIBYUV=0 -DCONFIG_WEBM_IO=0
  -DENABLE_DOCS=0 -DENABLE_EXAMPLES=0 -DENABLE_TESTS=0 -DENABLE_TOOLS=0 -DENABLE_TESTDATA=0
  -DBUILD_SHARED_LIBS=0 -DCMAKE_BUILD_TYPE=Release
)

unpack() { # dir tarball
  rm -rf "$1" && mkdir -p "$1" && tar -xf "$2" -C "$1" --strip-components=1
}

# install_aom SRC BUILD PREFIX: the library, its public headers and its
# pkg-config file; libaom's own install rules want its tools too.
install_aom() {
  mkdir -p "$3/include/aom" "$3/lib/pkgconfig"
  cp "$1"/aom/*.h "$3/include/aom/"
  cp "$2/libaom.a" "$3/lib/"
  cp "$2/aom.pc" "$3/lib/pkgconfig/"
}

# wasm: zlib, libaom, FFmpeg, the module and its translation.
w="$WORK/wasm"
mkdir -p "$w/bin"
sysroot="$wasi/share/wasi-sysroot"
cat >"$w/bin/wasm-cc" <<EOF
#!/bin/sh
exec "$wasi/bin/clang" --target=wasm32-wasip1 --sysroot="$sysroot" -ffile-prefix-map="$ROOT"=. \\
  -I"$w/prefix/include" -L"$w/prefix/lib" -Qunused-arguments "\$@"
EOF
chmod +x "$w/bin/wasm-cc"
tools="$w/bin:$wasi/bin"
if [[ ! -f "$w/.built" ]]; then
  unpack "$w/zlib" "$(tool zlib-src)"
  echo "== zlib for WebAssembly" >&2
  (cd "$w/zlib" && PATH="$tools:$PATH" CC=wasm-cc AR=llvm-ar RANLIB=llvm-ranlib CFLAGS="$OPT" \
    ./configure --static --prefix="$w/prefix" >configure.log 2>&1 && PATH="$tools:$PATH" make -j"$(nproc)" libz.a >make.log 2>&1 &&
    make install >install.log 2>&1)

  unpack "$w/aom-src" "$(tool aom-src)"
  echo "== libaom for WebAssembly ($BITS-bit, $AOM_OPT)" >&2
  start=$(date +%s)
  "$cmake" -S "$w/aom-src" -B "$w/aom" -G "Unix Makefiles" "${AOM[@]}" \
    -DCMAKE_TOOLCHAIN_FILE="$wasi/share/cmake/wasi-sdk-p1.cmake" -DWASI_SDK_PREFIX="$wasi" \
    -DCMAKE_C_FLAGS="$AOM_OPT -I$ROOT/$SPIKE/driver/shim -ffile-prefix-map=$ROOT=." -DCMAKE_INSTALL_PREFIX="$w/prefix" \
    >"$w/aom-configure.log" 2>&1 || { tail -30 "$w/aom-configure.log" >&2; exit 1; }
  make -C "$w/aom" -j"$(nproc)" aom aom_pc >"$w/aom-make.log" 2>&1 || { grep -m 20 -E "error|Error" "$w/aom-make.log" >&2; exit 1; }
  install_aom "$w/aom-src" "$w/aom" "$w/prefix"
  echo "   libaom: $(( $(date +%s) - start )) s, $(stat -c %s "$w/prefix/lib/libaom.a") bytes" >&2

  unpack "$w/src" "$(tool ffmpeg-src)"
  mkdir -p "$w/ffmpeg"
  echo "== FFmpeg for WebAssembly, with libaom" >&2
  (cd "$w/ffmpeg" && PATH="$tools:$PATH" PKG_CONFIG_LIBDIR="$w/prefix/lib/pkgconfig" "$w/src/configure" \
    --enable-cross-compile --target-os=none --arch=wasm32 \
    --cc=wasm-cc --ar=llvm-ar --ranlib=llvm-ranlib --nm=llvm-nm --strip=llvm-strip \
    --pkg-config=pkg-config --pkg-config-flags=--static --optflags="$OPT" \
    "${COMMON[@]}" "${COMPONENTS[@]}" >configure.log 2>&1) || { tail -30 "$w/ffmpeg/configure.log" >&2; exit 1; }
  PATH="$tools:$PATH" make -C "$w/ffmpeg" -j"$(nproc)" >"$w/ffmpeg/make.log" 2>&1 || { tail -20 "$w/ffmpeg/make.log" >&2; exit 1; }
  touch "$w/.built"
fi

echo "== the module" >&2
f="$w/ffmpeg"
PATH="$tools:$PATH" wasm-cc "$OPT" -mexec-model=reactor -I"$w/src" -I"$f" -I"$DRIVER_DIR" \
  "$DRIVER_DIR/driver.c" "$SPIKE/driver/transcode.c" -o "$w/raw.wasm" \
  -L"$f/libavformat" -L"$f/libavcodec" -L"$f/libswscale" -L"$f/libavutil" \
  -lavformat -lavcodec -lswscale -lavutil -laom -lz -lm -lwasi-emulated-process-clocks \
  -Wl,--import-memory -Wl,--stack-first -Wl,-z,stack-size=1048576 -Wl,--max-memory=4294967296 \
  -Wl,--export=malloc -Wl,--export=free
"$wasm_opt" -Oz "$w/raw.wasm" -o "$w/module.wasm" --strip-debug --strip-producers

echo "== the translation" >&2
pkg="$SPIKE/aomwasm"
rm -rf "$pkg" && mkdir -p "$pkg"
"$wasm2go" -pkg aomwasm -unsafe -embed -o "$pkg/module.go" "$w/module.wasm"
gofmt -w "$pkg/module.go"
pages=$("$(dirname "$wasm_opt")/wasm-dis" "$w/module.wasm" | sed -n 's/.*(import "env" "memory" (memory [^ ]* \([0-9]*\).*/\1/p')
printf 'package aomwasm\n\n// MinPages is the memory the module needs before it runs, in 64 KiB pages.\nconst MinPages = %s\n' "$pages" >"$pkg/pages.go"
printf '%s\n' "$BITS" >"$pkg/BITS"
printf 'module %s bytes; translation %s bytes of Go and %s of data\n' "$(stat -c %s "$w/module.wasm")" \
  "$(stat -c %s "$pkg/module.go")" "$(stat -c %s "$pkg/module.dat")" >&2

# native: the same C, built with the system's compiler, without SIMD.
n="$WORK/native"
if [[ ! -f "$n/.built" ]]; then
  mkdir -p "$n"
  unpack "$n/zlib" "$(tool zlib-src)"
  echo "== zlib, natively" >&2
  (cd "$n/zlib" && CFLAGS="-O2" ./configure --static --prefix="$n/prefix" >configure.log 2>&1 &&
    make -j"$(nproc)" libz.a >make.log 2>&1 && make install >install.log 2>&1)
  unpack "$n/aom-src" "$(tool aom-src)"
  echo "== libaom, natively, without SIMD" >&2
  "$cmake" -S "$n/aom-src" -B "$n/aom" -G "Unix Makefiles" "${AOM[@]}" -DCMAKE_C_FLAGS="$AOM_OPT" \
    -DCMAKE_INSTALL_PREFIX="$n/prefix" >"$n/aom-configure.log" 2>&1 || { tail -30 "$n/aom-configure.log" >&2; exit 1; }
  make -C "$n/aom" -j"$(nproc)" aom aom_pc >"$n/aom-make.log" 2>&1 || { grep -m 20 -E "error|Error" "$n/aom-make.log" >&2; exit 1; }
  install_aom "$n/aom-src" "$n/aom" "$n/prefix"
  unpack "$n/src" "$(tool ffmpeg-src)"
  mkdir -p "$n/ffmpeg"
  echo "== FFmpeg, natively, with libaom" >&2
  (cd "$n/ffmpeg" && PKG_CONFIG_LIBDIR="$n/prefix/lib/pkgconfig" "$n/src/configure" --pkg-config-flags=--static \
    --optflags="$OPT" --extra-cflags="-I$n/prefix/include" --extra-ldflags="-L$n/prefix/lib" \
    "${COMMON[@]}" "${COMPONENTS[@]}" >configure.log 2>&1) || { tail -30 "$n/ffmpeg/configure.log" >&2; exit 1; }
  make -C "$n/ffmpeg" -j"$(nproc)" >"$n/ffmpeg/make.log" 2>&1 || { tail -20 "$n/ffmpeg/make.log" >&2; exit 1; }
  touch "$n/.built"
fi
echo "== the transcoder, natively" >&2
nf="$n/ffmpeg"
cc "$OPT" -I"$n/src" -I"$nf" -I"$DRIVER_DIR" "$DRIVER_DIR/driver.c" "$SPIKE/driver/transcode.c" "$SPIKE/driver/native_main.c" \
  -o "$n/vs-native" -L"$nf/libavformat" -L"$nf/libavcodec" -L"$nf/libswscale" -L"$nf/libavutil" -L"$n/prefix/lib" \
  -lavformat -lavcodec -lswscale -lavutil -laom -lz -lm
printf '%s\n' "$n/vs-native"
