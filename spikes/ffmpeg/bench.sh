#!/usr/bin/env bash
# Times stripping the large fixtures with the translated driver and with the
# same driver natively, and records each run's peak memory.
#
#   spikes/ffmpeg/bench.sh LABEL
set -euo pipefail
exec </dev/null
cd "$(dirname "$0")/../.."

label="${1:-run}"
fixtures=out/spikes/ffmpeg/fixtures
work=out/spikes/ffmpeg/bench
mkdir -p "$work"
for f in "$fixtures"/large-*.mov; do
  size=$(stat -c %s "$f")
  # The translated driver, in its worker process; its report has the time
  # and the worker's peak RSS.
  rep=$(out/spikes/ffmpeg/ffspike strip "$f" "$work/out.mov" 2>/dev/null)
  ms=$(printf '%s' "$rep" | sed -n 's/.*"ms":\([0-9]*\).*/\1/p')
  rss=$(printf '%s' "$rep" | sed -n 's/.*"peak_rss_mb":\([0-9.]*\).*/\1/p')
  cpu=$(printf '%s' "$rep" | sed -n 's/.*"cpu_ms":\([0-9]*\).*/\1/p')
  mod=$(printf '%s' "$rep" | sed -n 's/.*"module_mb":\([0-9.]*\).*/\1/p')
  printf '%s wasm   %-14s %6d ms %7.0f MB/s  cpu %6d ms  rss %5.0f MB  module %5.1f MB\n' \
    "$label" "$(basename "$f")" "$ms" "$(echo "$size / 1048576 / ($ms / 1000)" | bc -l)" "$cpu" "$rss" "$mod"
  # The same driver, natively.
  start=$(date +%s%N)
  /usr/bin/time -f '%M %U %S' -o "$work/time.txt" out/spikes/ffmpeg/dmnative strip mov "$f" "$work/out.mov" >/dev/null 2>&1
  ms=$((($(date +%s%N) - start) / 1000000))
  read -r kb user sys <"$work/time.txt"
  printf '%s native %-14s %6d ms %7.0f MB/s  cpu %6.0f ms  rss %5.0f MB\n' \
    "$label" "$(basename "$f")" "$ms" "$(echo "$size / 1048576 / ($ms / 1000)" | bc -l)" \
    "$(echo "($user + $sys) * 1000" | bc -l)" "$((kb / 1024))"
  rm -f "$work/out.mov"
done
