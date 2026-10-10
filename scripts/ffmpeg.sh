#!/usr/bin/env bash

# Builds Dens's media module: FFmpeg, zlib and libaom for WebAssembly, linked
# with the driver in internal/media/ffmpeg/driver, optimized with binaryen
# and translated to Go with wasm2go into internal/media/ffmpeg/module, which
# the repository keeps. Every input is pinned in scripts/vendor.sh, and the
# build doesn't depend on where it runs, so CI regenerates the same bytes.
#
# The driver also builds natively under AddressSanitizer, which the fuzz
# target in internal/media/ffmpeg runs damaged files through beside the
# module: the module holds a memory bug, but doesn't find one.
#
# Usage:
#   ./scripts/ffmpeg.sh                  # regenerate internal/media/ffmpeg/module
#   ./scripts/ffmpeg.sh --check          # regenerate elsewhere, and fail if it differs
#   ./scripts/ffmpeg.sh --source OUT     # write FFmpeg's source, as the binaries include it, to OUT (.tar.xz)
#   ./scripts/ffmpeg.sh --asan           # build the driver natively under AddressSanitizer; prints its path
#   ./scripts/ffmpeg.sh --fuzz DURATION  # fuzz the driver, in the module and under AddressSanitizer

set -euo pipefail
exec </dev/null
umask 022
export LC_ALL=C

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

MODULE_DIR=internal/media/ffmpeg/module
DRIVER_DIR=internal/media/ffmpeg/driver
# The setjmp.h the driver gives libaom (see the file).
AOM_SHIM=$DRIVER_DIR/aom

# What Dens does with media needs, and nothing else: the containers phones and
# browsers use and the image formats' own demuxers; the parsers a stream copy
# needs, VP8, VP9 and AV1 among them, which give a WebM's size without
# decoding it; the bitstream filters muxers insert; the decoders of phone
# video, of the photo formats Dens converts, and of JPEG and PNG, for
# smaller copies; the JPEG and PNG encoders, with zlib for PNG and TIFF; and
# libaom's AV1 encoder, for smaller copies of videos.
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
# Single-threaded, with no assembly, programs or network, and LGPL only.
COMMON=(
  --disable-programs --disable-doc --disable-network --disable-autodetect --disable-debug
  --disable-avdevice --disable-avfilter --disable-swresample
  --disable-pthreads --disable-w32threads --disable-os2threads --disable-asm --disable-runtime-cpudetect
)
OPT=-Oz
# libaom's realtime AV1 encoder alone, in eight bits, as CMake's Release
# builds it (-O3): no decoder, threads, SIMD or tools, and no C++ (libyuv,
# libwebm).
AOM=(
  -G "Unix Makefiles" -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=0
  -DAOM_TARGET_CPU=generic -DCONFIG_RUNTIME_CPU_DETECT=0 -DCONFIG_MULTITHREAD=0 -DCONFIG_AV1_DECODER=0
  -DCONFIG_REALTIME_ONLY=1 -DCONFIG_AV1_HIGHBITDEPTH=0 -DCONFIG_LIBYUV=0 -DCONFIG_WEBM_IO=0
  -DENABLE_DOCS=0 -DENABLE_EXAMPLES=0 -DENABLE_TESTS=0 -DENABLE_TOOLS=0 -DENABLE_TESTDATA=0
)

usage() {
  sed -n '3,18p' "$0" | sed 's/^# \{0,1\}//'
}

# vendor fetches the pinned inputs a mode needs: FFmpeg's, zlib's and
# libaom's source, and the tools named. Only the module needs the
# WebAssembly toolchain (MODULE_TOOLS), and only builds need CMake, which
# libaom builds with.
MODULE_TOOLS=(wasi-sdk binaryen wasm2go cmake)
paths=
tool() { printf '%s\n' "$paths" | sed -n "s/^$1=//p"; }
vendor() {
  paths=$(./scripts/vendor.sh ffmpeg-src zlib-src aom-src "$@")
  ffmpeg_tar=$(tool ffmpeg-src)
  zlib_tar=$(tool zlib-src)
  aom_tar=$(tool aom-src)
  wasi=$(tool wasi-sdk)
  wasm_opt=$(tool binaryen)
  wasm2go=$(tool wasm2go)
  cmake=$(tool cmake)
}

# build_aom DIR wasm|native: build libaom in DIR, for the module or natively
# under AddressSanitizer, and install its library, its public headers and its
# pkg-config file in DIR/prefix by hand: its own install rules want its tools
# too.
build_aom() {
  local work="$1" target="$2" args
  rm -rf "$work"
  mkdir -p "$work/src" "$work/prefix/include/aom" "$work/prefix/lib/pkgconfig"
  tar -xzf "$aom_tar" -C "$work/src" --strip-components=1
  if [[ "$target" == wasm ]]; then
    # libaom records the toolchain file in its build configuration, which
    # the module carries (aom_codec_build_config), as its path from the
    # build, so it's a file of the build's own, which names no machine's
    # wasi-sdk.
    printf 'include("%s/share/cmake/wasi-sdk-p1.cmake")\n' "$wasi" >"$work/wasm.cmake"
    args=(-DCMAKE_TOOLCHAIN_FILE="$work/wasm.cmake" -DWASI_SDK_PREFIX="$wasi"
      -DCMAKE_C_FLAGS="-I$ROOT/$AOM_SHIM -ffile-prefix-map=$ROOT=.")
  else
    args=(-DCMAKE_C_FLAGS="-g -fsanitize=address -fno-omit-frame-pointer")
  fi
  "$cmake" -S "$work/src" -B "$work/build" "${AOM[@]}" "${args[@]}" -DCMAKE_INSTALL_PREFIX="$work/prefix" \
    >"$work/configure.log" 2>&1 || { tail -20 "$work/configure.log" >&2; return 1; }
  make -C "$work/build" -j"$(nproc)" aom aom_pc >"$work/make.log" 2>&1 ||
    { grep -m 20 -E 'error|Error' "$work/make.log" >&2; return 1; }
  cp "$work/src"/aom/*.h "$work/prefix/include/aom/"
  cp "$work/build/libaom.a" "$work/prefix/lib/"
  cp "$work/build/aom.pc" "$work/prefix/lib/pkgconfig/"
}

# build_module OUT_DIR: build the module and its translation into OUT_DIR.
build_module() {
  local out="$1"
  local work="$ROOT/out/ffmpeg"
  local stamp="$work/.inputs"
  local inputs
  # The libraries' inputs: the driver itself is linked anew each time.
  inputs=$(cat "$ffmpeg_tar" "$zlib_tar" "$aom_tar" "$AOM_SHIM"/* "$0" | sha256sum | cut -d' ' -f1)
  mkdir -p "$work"

  # The compiler, behind a wrapper so FFmpeg's recorded configuration names
  # only "wasm-cc", and no path of this machine reaches the module.
  local sysroot="$wasi/share/wasi-sysroot"
  mkdir -p "$work/bin"
  cat >"$work/bin/wasm-cc" <<EOF
#!/bin/sh
exec "$wasi/bin/clang" --target=wasm32-wasip1 --sysroot="$sysroot" -ffile-prefix-map="$ROOT"=. \\
  -I"$work/zlib/prefix/include" -L"$work/zlib/prefix/lib" -Qunused-arguments "\$@"
EOF
  chmod +x "$work/bin/wasm-cc"
  local tools="$work/bin:$wasi/bin"

  if [[ ! -f "$stamp" || "$(cat "$stamp")" != "$inputs" ]]; then
    rm -rf "$work/src" "$work/zlib" "$work/ffmpeg"
    mkdir -p "$work/src" "$work/zlib/src" "$work/ffmpeg"
    tar -xJf "$ffmpeg_tar" -C "$work/src" --strip-components=1
    tar -xJf "$zlib_tar" -C "$work/zlib/src" --strip-components=1

    echo "== libaom for WebAssembly" >&2
    build_aom "$work/aom" wasm

    echo "== zlib for WebAssembly" >&2
    (cd "$work/zlib/src" && PATH="$tools:$PATH" CC=wasm-cc AR=llvm-ar RANLIB=llvm-ranlib CFLAGS="$OPT" \
      ./configure --static --prefix="$work/zlib/prefix" >"$work/zlib/configure.log" 2>&1 &&
      PATH="$tools:$PATH" make -j"$(nproc)" libz.a >"$work/zlib/make.log" 2>&1 &&
      make install >"$work/zlib/install.log" 2>&1) ||
      { tail -20 "$work/zlib/configure.log" "$work/zlib/make.log" >&2; return 1; }

    echo "== FFmpeg for WebAssembly" >&2
    (cd "$work/ffmpeg" && PATH="$tools:$PATH" PKG_CONFIG_LIBDIR="$work/aom/prefix/lib/pkgconfig" "$work/src/configure" \
      --enable-cross-compile --target-os=none --arch=wasm32 \
      --cc=wasm-cc --ar=llvm-ar --ranlib=llvm-ranlib --nm=llvm-nm --strip=llvm-strip \
      --pkg-config=pkg-config --pkg-config-flags=--static --optflags="$OPT" \
      "${COMMON[@]}" "${COMPONENTS[@]}" >configure.log 2>&1) ||
      { tail -20 "$work/ffmpeg/configure.log" >&2; return 1; }
    PATH="$tools:$PATH" make -C "$work/ffmpeg" -j"$(nproc)" >"$work/ffmpeg/make.log" 2>&1 ||
      { tail -20 "$work/ffmpeg/make.log" >&2; return 1; }
    printf '%s' "$inputs" >"$stamp"
  fi

  echo "== the module" >&2
  # A reactor module, with no main, that imports its memory, so its host sets
  # the cap. The stack comes first, so running out of it goes below address 0
  # and traps instead of running into the module's data.
  local f="$work/ffmpeg"
  PATH="$tools:$PATH" wasm-cc "$OPT" -mexec-model=reactor -I"$work/src" -I"$f" \
    "$DRIVER_DIR/driver.c" -o "$work/raw.wasm" \
    -L"$f/libavformat" -L"$f/libavcodec" -L"$f/libswscale" -L"$f/libavutil" -L"$work/aom/prefix/lib" \
    -L"$work/zlib/prefix/lib" -lavformat -lavcodec -lswscale -lavutil -laom -lz -lm -lwasi-emulated-process-clocks \
    -Wl,--import-memory -Wl,--stack-first -Wl,-z,stack-size=1048576 -Wl,--max-memory=4294967296 \
    -Wl,--export=malloc -Wl,--export=free
  "$wasm_opt" "$OPT" "$work/raw.wasm" -o "$work/module.wasm" --strip-debug --strip-producers

  echo "== the translation" >&2
  mkdir -p "$out"
  "$wasm2go" -pkg module -unsafe -embed -o "$out/module.go" "$work/module.wasm"
  gofmt -w "$out/module.go"
  local pages
  pages=$("$(dirname "$wasm_opt")/wasm-dis" "$work/module.wasm" |
    sed -n 's/.*(import "env" "memory" (memory [^ ]* \([0-9]*\).*/\1/p')
  [[ -n "$pages" ]] || { printf 'error: the module imports no memory\n' >&2; return 1; }
  cat >"$out/pages.go" <<EOF
// Code generated by scripts/ffmpeg.sh. DO NOT EDIT.

package module

// MinPages is the memory the module needs before it runs, in 64 KiB pages.
const MinPages = $pages
EOF
  printf 'module %s bytes; translation %s bytes of Go and %s of data\n' "$(stat -c %s "$work/module.wasm")" \
    "$(stat -c %s "$out/module.go")" "$(stat -c %s "$out/module.dat")" >&2
}

# build_asan: build FFmpeg, zlib and the driver natively with
# AddressSanitizer into out/ffmpeg/asan, and print the driver's path.
build_asan() {
  local work="$ROOT/out/ffmpeg/asan"
  local stamp="$work/.inputs"
  local inputs
  inputs=$(cat "$ffmpeg_tar" "$zlib_tar" "$aom_tar" "$0" | sha256sum | cut -d' ' -f1)
  mkdir -p "$work"
  if [[ ! -f "$stamp" || "$(cat "$stamp")" != "$inputs" ]]; then
    rm -rf "$work/src" "$work/zlib" "$work/ffmpeg"
    mkdir -p "$work/src" "$work/zlib/src" "$work/ffmpeg"
    tar -xJf "$ffmpeg_tar" -C "$work/src" --strip-components=1
    tar -xJf "$zlib_tar" -C "$work/zlib/src" --strip-components=1

    echo "== libaom under AddressSanitizer" >&2
    build_aom "$work/aom" native

    echo "== zlib under AddressSanitizer" >&2
    (cd "$work/zlib/src" && CFLAGS="-O1 -g -fsanitize=address -fno-omit-frame-pointer" \
      ./configure --static --prefix="$work/zlib/prefix" >"$work/zlib/configure.log" 2>&1 &&
      make -j"$(nproc)" libz.a >"$work/zlib/make.log" 2>&1 &&
      make install >"$work/zlib/install.log" 2>&1) ||
      { tail -20 "$work/zlib/configure.log" "$work/zlib/make.log" >&2; return 1; }

    echo "== FFmpeg under AddressSanitizer" >&2
    (cd "$work/ffmpeg" && PKG_CONFIG_LIBDIR="$work/aom/prefix/lib/pkgconfig" "$work/src/configure" \
      --toolchain=gcc-asan --optflags="-O1 -g" --pkg-config-flags=--static \
      --extra-cflags="-I$work/zlib/prefix/include" --extra-ldflags="-L$work/zlib/prefix/lib" \
      "${COMMON[@]}" "${COMPONENTS[@]}" >configure.log 2>&1) ||
      { tail -20 "$work/ffmpeg/configure.log" >&2; return 1; }
    make -C "$work/ffmpeg" -j"$(nproc)" >"$work/ffmpeg/make.log" 2>&1 ||
      { tail -20 "$work/ffmpeg/make.log" >&2; return 1; }
    printf '%s' "$inputs" >"$stamp"
  fi

  echo "== the driver under AddressSanitizer" >&2
  local f="$work/ffmpeg"
  cc -O1 -g -fsanitize=address -fno-omit-frame-pointer -I"$work/src" -I"$f" \
    "$DRIVER_DIR/driver.c" "$DRIVER_DIR/native.c" -o "$work/dm-native" \
    -L"$f/libavformat" -L"$f/libavcodec" -L"$f/libswscale" -L"$f/libavutil" -L"$work/aom/prefix/lib" \
    -L"$work/zlib/prefix/lib" -lavformat -lavcodec -lswscale -lavutil -laom -lz -lm
  printf '%s\n' "$work/dm-native"
}

case "${1:-}" in
  "")
    vendor "${MODULE_TOOLS[@]}"
    build_module "$MODULE_DIR"
    ;;
  --check)
    vendor "${MODULE_TOOLS[@]}"
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    build_module "$tmp"
    for f in module.go module.dat pages.go; do
      if ! cmp -s "$tmp/$f" "$MODULE_DIR/$f"; then
        printf 'error: %s/%s differs from what scripts/ffmpeg.sh builds; run it and commit the result\n' \
          "$MODULE_DIR" "$f" >&2
        exit 1
      fi
    done
    echo "🟢 The media module matches its inputs" >&2
    ;;
  --source)
    [[ $# -eq 2 ]] || { usage >&2; exit 2; }
    vendor
    out=$(realpath -m "$2")
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    # FFmpeg's, zlib's and libaom's signed release tarballs as they are, with
    # the driver and this script, which build the module from them.
    mkdir -p "$tmp/dens-media/internal/media/ffmpeg" "$tmp/dens-media/scripts"
    cp "$ffmpeg_tar" "$zlib_tar" "$aom_tar" "$tmp/dens-media/"
    cp -r "$DRIVER_DIR" "$tmp/dens-media/internal/media/ffmpeg/"
    cp "$0" scripts/vendor.sh "$tmp/dens-media/scripts/"
    tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -C "$tmp" -cf - dens-media |
      xz -9 -T1 >"$out"
    printf '%s\n' "$out"
    ;;
  --asan)
    vendor cmake
    build_asan
    ;;
  --fuzz)
    [[ $# -eq 2 ]] || { usage >&2; exit 2; }
    vendor cmake
    asan=$(build_asan)
    # No minimizing: Go's fuzzer would shrink each input that reaches new Go
    # code, which says little about the C under test, and it reports one it
    # was shrinking when the time ran out as a failure.
    DENS_FFMPEG_ASAN="$asan" go test ./internal/media/ffmpeg -run '^$' -fuzz '^FuzzDriver$' \
      -fuzztime "$2" -fuzzminimizetime 0
    ;;
  -h | --help)
    usage
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
