#!/usr/bin/env bash
# Makes the spike's test files in out/spikes/ffmpeg/fixtures/: short videos and
# audio files carrying what their containers can hold, and large videos. It
# builds the ffmpeg command natively from the pinned source to make them; the
# spike never strips with it.
#
#   spikes/ffmpeg/fixtures.sh [large]
set -euo pipefail
exec </dev/null
cd "$(dirname "$0")/../.."
repo=$(pwd)

src_tar=$(./scripts/vendor.sh ffmpeg-src | sed -n 's/^ffmpeg-src=//p')
build="$repo/out/spikes/ffmpeg/build"
src="$build/$(basename "$src_tar" .tar.xz)"
[[ -f "$src/configure" ]] || tar -xJf "$src_tar" -C "$build"

tools="$build/native-tools"
ffmpeg="$tools/prefix/bin/ffmpeg"
if [[ ! -x "$ffmpeg" ]]; then
  echo "== the ffmpeg command, natively"
  rm -rf "$tools" && mkdir -p "$tools"
  (cd "$tools" && "$src/configure" --prefix="$tools/prefix" --disable-autodetect --disable-asm \
    --disable-doc --disable-network >configure.log 2>&1) || { tail -20 "$tools/configure.log" >&2; exit 1; }
  make -C "$tools" -j"$(nproc)" >"$tools/make.log" 2>&1 || { tail -20 "$tools/make.log" >&2; exit 1; }
  make -C "$tools" install >"$tools/install.log" 2>&1
fi
ff() { "$ffmpeg" -hide_banner -loglevel error -y "$@"; }

out="$repo/out/spikes/ffmpeg/fixtures"
mkdir -p "$out"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# What a phone or an editor leaves in a file, in every form a container
# can hold it. The coordinates are the Eiffel Tower's.
meta=(
  -metadata title="Fixture title" -metadata artist="Fixture Artist" -metadata author="Fixture Author"
  -metadata comment="Taken at home" -metadata description="Fixture description"
  -metadata copyright="Fixture Owner" -metadata creation_time="2024-05-01T12:00:00Z" -metadata date=2024
  -metadata location="+48.8584+002.2945/" -metadata location-eng="+48.8584+002.2945/"
  -metadata make=TestPhone -metadata model=TP-1 -metadata encoder="TestPhone Camera 1.0"
)
apple=(
  -metadata com.apple.quicktime.location.ISO6709="+48.8584+002.2945+035.000/"
  -metadata com.apple.quicktime.make=TestPhone -metadata com.apple.quicktime.model=TP-1
  -metadata com.apple.quicktime.software=17.0 -metadata com.apple.quicktime.creationdate=2024-05-01T12:00:00+0200
)
cat >"$work/chapters.txt" <<'EOF'
;FFMETADATA1
[CHAPTER]
TIMEBASE=1/1000
START=0
END=1500
title=Chapter at home
[CHAPTER]
TIMEBASE=1/1000
START=1500
END=3000
title=Chapter at the office
EOF
cat >"$work/coords.srt" <<'EOF'
1
00:00:00,000 --> 00:00:01,500
GPS 48.8584, 2.2945

2
00:00:01,500 --> 00:00:03,000
GPS 48.8585, 2.2946
EOF
printf 'Fixture attachment: owner notes\n' >"$work/notes.txt"

echo "== short files"
# Browsers play H.264 and AAC, which this build can't encode, so the videos
# copy a phone sample's streams, without its metadata, when there is one.
phone_av="$repo/test-files/mobile/videos/android-6-0-1-model-unknown-h264.mp4"
if [[ -f "$phone_av" ]]; then
  ff -i "$phone_av" -map 0:v -map 0:a -c copy -map_metadata -1 -map_chapters -1 "$work/av.mp4"
else
  ff -f lavfi -i testsrc2=size=640x360:rate=30:duration=3 -f lavfi -i sine=frequency=440:duration=3 \
    -c:v mpeg4 -q:v 5 -c:a aac -shortest "$work/av.mp4"
fi
ff -f lavfi -i color=red:size=64x64 -frames:v 1 "$work/cover.jpg"
ff -f lavfi -i sine=frequency=440:duration=3 "$work/a.wav"

# MP4 and MOV: everything as udta and as Apple keys, chapters, a subtitle
# track of coordinates and cover art, and a rotated video.
ff -i "$work/av.mp4" -i "$work/chapters.txt" -i "$work/coords.srt" -i "$work/cover.jpg" \
  -map 0:v -map 0:a -map 2 -map 3 -map_chapters 1 -c copy -c:s mov_text -c:v:1 mjpeg -disposition:v:1 attached_pic \
  -movflags use_metadata_tags "${meta[@]}" "${apple[@]}" "$out/meta.mp4"
ff -i "$work/av.mp4" -i "$work/chapters.txt" -i "$work/coords.srt" \
  -map 0:v -map 0:a -map 2 -map_chapters 1 -c copy -c:s mov_text \
  -movflags use_metadata_tags "${meta[@]}" "${apple[@]}" "$out/meta.mov"
ff -display_rotation 90 -i "$work/av.mp4" -c copy "${meta[@]}" "$out/rotated.mp4"

# Matroska: tags, chapters, subtitles, an attachment and cover art.
ff -i "$work/av.mp4" -i "$work/chapters.txt" -i "$work/coords.srt" \
  -map 0:v -map 0:a -map 2 -map_chapters 1 -c copy -c:s srt "${meta[@]}" \
  -attach "$work/notes.txt" -metadata:s:t mimetype=text/plain \
  -attach "$work/cover.jpg" -metadata:s:t:1 mimetype=image/jpeg -metadata:s:t:1 filename=cover.jpg \
  "$out/meta.mkv"

# WebM takes only VP8, VP9 and AV1 video, which this build can't encode, so
# its fixture is audio: Opus with tags and a subtitle track.
ff -i "$work/a.wav" -i "$work/coords.srt" -map 0 -map 1 -c:a opus -strict experimental -ar 48000 \
  -c:s webvtt "${meta[@]}" "$out/meta.webm"

# Audio files: ID3 with a picture, iTunes tags with a cover, Vorbis
# comments with a picture, Ogg Opus and RIFF INFO.
# FFmpeg has no MP3 encoder of its own, but silent MP3 frames need none: a
# header for 128 kb/s at 44.1 kHz, and zeros. It plays as silence.
for _ in $(seq 115); do printf '\xff\xfb\x90\x64'; head -c 413 /dev/zero; done >"$work/silence.mp3"
ff -i "$work/silence.mp3" -i "$work/cover.jpg" -map 0 -map 1 -c:a copy -c:v mjpeg -disposition:v attached_pic \
  -id3v2_version 3 "${meta[@]}" "$out/meta.mp3"
ff -i "$work/a.wav" -i "$work/cover.jpg" -map 0 -map 1 -c:a aac -c:v mjpeg -disposition:v attached_pic \
  "${meta[@]}" "$out/meta.m4a"
ff -i "$work/a.wav" -i "$work/cover.jpg" -map 0 -map 1 -c:a flac -c:v mjpeg -disposition:v attached_pic \
  "${meta[@]}" "$out/meta.flac"
ff -i "$work/a.wav" -c:a opus -strict experimental -ar 48000 "${meta[@]}" "$out/meta.ogg"
ff -i "$work/a.wav" -c:a pcm_s16le "${meta[@]}" "$out/meta.wav"

if [[ "${1:-}" == "large" ]]; then
  echo "== large files"
  # One phone video repeated, with its metadata, so stripping one is real work.
  phone="$repo/test-files/mobile/videos/iphone-12-pro-hevc.mov"
  for gb in 1 2 4; do
    loops=$((gb * 1024 / 34 - 1))
    ff -stream_loop "$loops" -i "$phone" -map 0:v -map 0:a -c copy -map_metadata 0 \
      -movflags use_metadata_tags "$out/large-${gb}g.mov"
  done
fi
ls -la "$out"
