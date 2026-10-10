#!/usr/bin/env bash

# The video spike's native measurements: what a smaller copy of a phone
# video costs and keeps, made with the pinned native FFmpeg build in tools/
# (BtbN's LGPL build of FFmpeg 9.0.2, with libaom and libvpx). Each copy fits
# a longer side, at 30 frames a second at most, at a target bitrate, and is
# scored with VMAF and SSIM against the original scaled the same way without
# loss. Encoders run on one thread without SIMD, as the media module would,
# and again with both, as a browser's software encoder would.
#
# Usage: spikes/video/native.sh [VIDEO...]   (ONLY=hdr for the ten-bit runs alone)
# Results go to out/spikes/video/native/results.csv.

set -euo pipefail
exec </dev/null
export LC_ALL=C

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT"
FF=tools/ffmpeg-n9.0.2-17-g2a571b6068-linux64-lgpl-9.0/bin
[[ -x "$FF/ffmpeg" ]] || { echo "error: no native FFmpeg in $FF" >&2; exit 1; }
OUT=out/spikes/video/native
mkdir -p "$OUT"

videos=("$@")
if [[ ${#videos[@]} -eq 0 ]]; then
  videos=(test-files/mobile/videos/iphone-12-pro-hevc.mov test-files/mobile/videos/iphone-14-pro-live-photo-hevc.mov
    test-files/mobile/videos/iphone-x-h264.mov test-files/mobile/videos/android-nokia-6-1-h264.mp4)
fi

# fit scales a video, upright, so its longer side is at most $1, never
# enlarging it, at 30 frames a second at most.
fit() {
  printf "scale=w='if(gte(iw,ih),min(%d,iw),-2)':h='if(gte(iw,ih),-2,min(%d,ih))':flags=lanczos,fps=30" "$1" "$1"
}

# encoder NAME KBPS: FFmpeg's options for an encoder at a bitrate, one
# keyframe every five seconds, on one thread, in eight bits unless named
# ten: FFmpeg would keep a ten-bit source's depth.
encoder() {
  case "$1" in
    av1) echo "-c:v libaom-av1 -usage realtime -cpu-used 8 -threads 1 -row-mt 0 -b:v ${2}k -g 150 -pix_fmt yuv420p" ;;
    av1-10) echo "-c:v libaom-av1 -usage realtime -cpu-used 8 -threads 1 -row-mt 0 -b:v ${2}k -g 150 -pix_fmt yuv420p10le" ;;
    vp9) echo "-c:v libvpx-vp9 -deadline realtime -cpu-used 8 -threads 1 -row-mt 0 -b:v ${2}k -g 150 -pix_fmt yuv420p" ;;
  esac
}

csv="$OUT/results.csv"
[[ -f "$csv" ]] || echo "video,encoder,side,kbps,simd,frames,seconds,fps,bytes,actual_kbps,vmaf,ssim" >"$csv"

run() { # video encoder side kbps simd
  local v=$1 enc=$2 side=$3 kbps=$4 simd=$5
  local name out start end frames secs bytes dur actual vmaf ssim pix=yuv420p
  name=$(basename "${v%.*}")
  out="$OUT/$name-$enc-$side-$kbps-$simd.mkv"
  [[ "$enc" == av1-10 ]] && pix=yuv420p10le
  local env=(env)
  [[ "$simd" == scalar ]] && env=(env AOM_SIMD_CAPS_MASK=0 VPX_SIMD_CAPS_MASK=0)
  start=$(date +%s.%N)
  # shellcheck disable=SC2046
  "${env[@]}" "$FF/ffmpeg" -nostdin -v error -y -i "$v" -map 0:v:0 -an -vf "$(fit "$side")" $(encoder "$enc" "$kbps") "$out"
  end=$(date +%s.%N)
  frames=$("$FF/ffprobe" -v error -count_packets -select_streams v:0 -show_entries stream=nb_read_packets -of csv=p=0 "$out")
  dur=$("$FF/ffprobe" -v error -show_entries format=duration -of csv=p=0 "$out")
  bytes=$(stat -c %s "$out")
  secs=$(echo "$end - $start" | bc -l)
  actual=$(echo "$bytes * 8 / $dur / 1000" | bc -l)
  # The reference is the original scaled the same way, in the copy's pixel
  # format, scored frame for frame: both run in one time base, since the
  # scores pair frames by their times, and the copy's start at zero.
  local scores
  scores=$("$FF/ffmpeg" -nostdin -v info -i "$out" -i "$v" -lavfi \
    "[1:v]$(fit "$side"),format=$pix,settb=1/30,setpts=N,split=2[r1][r2];[0:v]format=$pix,settb=1/30,setpts=N,split=2[d1][d2];[d1][r1]libvmaf=n_threads=4;[d2][r2]ssim" \
    -f null - 2>&1 || true)
  vmaf=$(sed -n 's/.*VMAF score: \([0-9.]*\).*/\1/p' <<<"$scores" | tail -1)
  ssim=$(sed -n 's/.*SSIM .* All:\([0-9.]*\).*/\1/p' <<<"$scores" | tail -1)
  printf '%s,%s,%s,%s,%s,%s,%.2f,%.1f,%s,%.0f,%s,%s\n' "$name" "$enc" "$side" "$kbps" "$simd" "$frames" "$secs" \
    "$(echo "$frames / $secs" | bc -l)" "$bytes" "$actual" "${vmaf:-?}" "${ssim:-?}" | tee -a "$csv"
}

for v in "${videos[@]}"; do
  [[ "${ONLY:-}" == hdr ]] && break
  for enc in av1 vp9; do
    for kbps in 1500 2500 4000; do run "$v" "$enc" 1920 "$kbps" scalar; done
    for kbps in 800 1200 2000; do run "$v" "$enc" 1280 "$kbps" scalar; done
  done
  # A browser's software encoders use SIMD; a sample of what it buys.
  run "$v" av1 1920 2500 simd
  run "$v" vp9 1920 2500 simd
done
# HDR: the iPhone's HLG video at ten bits, against the eight-bit copies above.
for v in "${videos[@]}"; do
  if [[ "$("$FF/ffprobe" -v error -select_streams v:0 -show_entries stream=pix_fmt -of csv=p=0 "$v")" == *10le* ]]; then
    for kbps in 1500 2500; do run "$v" av1-10 1920 "$kbps" scalar; done
    run "$v" av1-10 1280 1200 scalar
  fi
done
