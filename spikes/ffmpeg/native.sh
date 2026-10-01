#!/usr/bin/env bash
# Builds the driver natively for every release target from this one machine,
# with zig as the C compiler: FFmpeg's strip-only libraries and the driver,
# statically linked (musl on Linux, MinGW's runtime on Windows). Prints each
# build's time and the driver's size. Track B's own build, before confinement.
#
#   spikes/ffmpeg/native.sh [TARGET...]   # linux-amd64 linux-arm64 windows-amd64 windows-arm64
set -euo pipefail
exec </dev/null
cd "$(dirname "$0")/../.."
repo=$(pwd)

paths=$(./scripts/vendor.sh zig wasi-sdk ffmpeg-src)
tool() { printf '%s\n' "$paths" | sed -n "s/^$1=//p"; }
zig=$(tool zig)
llvm="$(tool wasi-sdk)/bin" # its llvm-nm and llvm-strip read ELF and COFF too
src_tar=$(tool ffmpeg-src)

build="$repo/out/spikes/ffmpeg/build"
src="$build/$(basename "$src_tar" .tar.xz)"
[[ -f "$src/configure" ]] || tar -xJf "$src_tar" -C "$build"

# The same components as the module (build.sh).
components=(
  --disable-everything
  "--enable-demuxer=mov,matroska,mp3,flac,ogg,wav,aac"
  "--enable-muxer=mov,mp4,ipod,matroska,webm,mp3,flac,ogg,oga,opus,wav"
  "--enable-parser=h264,hevc,aac,mpegaudio,opus,vorbis,flac,vp8,vp9,av1,mjpeg,ac3"
  "--enable-bsf=aac_adtstoasc,vp9_superframe"
  --disable-programs --disable-doc --disable-network --disable-autodetect --disable-debug
  --disable-avdevice --disable-avfilter --disable-swscale --disable-swresample
  --disable-pthreads --disable-w32threads --disable-asm --optflags=-O2
)

targets=("$@")
[[ ${#targets[@]} -gt 0 ]] || targets=(linux-amd64 linux-arm64 windows-amd64 windows-arm64)
for target in "${targets[@]}"; do
  case "$target" in
    linux-amd64) triple=x86_64-linux-musl os=linux arch=x86_64 exe= ;;
    linux-arm64) triple=aarch64-linux-musl os=linux arch=aarch64 exe= ;;
    windows-amd64) triple=x86_64-windows-gnu os=mingw32 arch=x86_64 exe=.exe ;;
    windows-arm64) triple=aarch64-windows-gnu os=mingw32 arch=aarch64 exe=.exe ;;
    *) printf 'error: unknown target %s\n' "$target" >&2; exit 2 ;;
  esac
  dir="$build/native-$target"
  rm -rf "$dir" && mkdir -p "$dir"
  # configure runs the compiler as one word, so zig gets a wrapper. zig's
  # linker refuses two flags configure gives MinGW, which only place the
  # image; --dynamicbase still asks for its address to be randomized.
  cat >"$dir/cc" <<EOF
#!/bin/sh
for arg do
  shift
  case "\$arg" in
    -Wl,--pic-executable,-e,mainCRTStartup | -Wl,--image-base,*) continue ;;
  esac
  set -- "\$@" "\$arg"
done
exec "$zig" cc -target $triple "\$@"
EOF
  printf '#!/bin/sh\nexec "%s" ar "$@"\n' "$zig" >"$dir/ar"
  printf '#!/bin/sh\nexec "%s" ranlib "$@"\n' "$zig" >"$dir/ranlib"
  chmod +x "$dir/cc" "$dir/ar" "$dir/ranlib"
  start=$(date +%s)
  (cd "$dir" && "$src/configure" --prefix="$dir/prefix" --enable-cross-compile \
    --target-os="$os" --arch="$arch" --cc="$dir/cc" --ar="$dir/ar" --ranlib="$dir/ranlib" \
    --nm="$llvm/llvm-nm" --strip="$llvm/llvm-strip" "${components[@]}" >configure.log 2>&1) ||
    { tail -20 "$dir/configure.log" >&2; exit 1; }
  make -C "$dir" -j"$(nproc)" >"$dir/make.log" 2>&1 || { tail -20 "$dir/make.log" >&2; exit 1; }
  make -C "$dir" install >"$dir/install.log" 2>&1
  # Static on Linux; on Windows FFmpeg's random seed needs bcrypt.
  extra=(-static)
  [[ "$os" == mingw32 ]] && extra=(-lbcrypt)
  "$dir/cc" -O2 -I"$dir/prefix/include" spikes/ffmpeg/driver/driver.c spikes/ffmpeg/driver/host.c \
    -o "$repo/out/spikes/ffmpeg/dmnative-$target$exe" -L"$dir/prefix/lib" -lavformat -lavcodec -lavutil -lm "${extra[@]}"
  "$llvm/llvm-strip" "$repo/out/spikes/ffmpeg/dmnative-$target$exe" -o "$repo/out/spikes/ffmpeg/dmnative-$target-stripped$exe"
  printf '%s: built in %ds, driver %s bytes, stripped of symbols %s bytes\n' "$target" "$(($(date +%s) - start))" \
    "$(stat -c %s "$repo/out/spikes/ffmpeg/dmnative-$target$exe")" \
    "$(stat -c %s "$repo/out/spikes/ffmpeg/dmnative-$target-stripped$exe")"
done
