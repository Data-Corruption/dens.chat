# Media test files

From [metadata-extractor-images](https://github.com/drewnoakes/metadata-extractor-images) at commit `651ad0e67aa8d43d358ad05f9bc07b52d8b9ac6e`, whose README says "You are free to use these media files however you wish", unchanged:

| File | Source | What it has |
| --- | --- | --- |
| `with-gps.mov` | `mov/with-gps.mov` | An iPhone 6's H.264 video turned a quarter, with AAC, a location, make and model in QuickTime keys, and Apple's user data in every frame |
| `with-gps.mp4` | `mp4/with-gps.mp4` | An Android phone's H.264 video, with a location in QuickTime user data |
| `rotated.heic` | `heic/Issue 263 dotnet.heic` | An iPhone XR's photo: 48 HEVC tiles turned a quarter, with EXIF and its GPS, and Apple's Display P3 profile |

Made with FFmpeg 9.0.2's `ffmpeg` command, each carrying what its container can hold, the location among it (the coordinates are the Eiffel Tower's): `meta.mp4` (with a chapter, a subtitle track of coordinates and cover art), `meta.mkv` (with a chapter, subtitles and an attachment), `meta.webm` (Opus, with subtitles), `meta.mp3` (ID3 with a picture; its frames are silent), `meta.m4a`, `meta.flac` (each with cover art), `meta.ogg` (Opus) and `meta.wav`. The video is FFmpeg's test pattern, 64 × 48 for a second, and the audio a 440 Hz tone. For example:

```sh
ffmpeg -f lavfi -i testsrc2=size=64x48:rate=10:duration=1 -f lavfi -i sine=frequency=440:duration=1 \
  -c:v mpeg4 -c:a aac -shortest av.mp4
ffmpeg -i av.mp4 -i chapters.txt -i coords.srt -i cover.jpg -map 0:v -map 0:a -map 2 -map 3 -map_chapters 1 \
  -c copy -c:s mov_text -c:v:1 mjpeg -disposition:v:1 attached_pic -movflags use_metadata_tags \
  -metadata location="+48.8584+002.2945/" -metadata com.apple.quicktime.location.ISO6709="+48.8584+002.2945+035.000/" \
  -metadata make=TestPhone -metadata model=TP-1 meta.mp4
```
