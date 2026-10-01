# Spikes

Throwaway experiments that answer a design question before the code that depends on it. Each is a directory in this Go module, whose dependencies never reach the product, and each goes when the work it informs lands, its findings in `docs/dev/design.md`.

## ffmpeg in WebAssembly

Dens needs ffmpeg to strip video and audio, and to convert the photo formats it can't strip without decoding them. The design compiles ffmpeg to WebAssembly and translates it to Go with wasm2go: one pure-Go build for Linux and Windows, with the module as a sandbox around hostile files (see "ffmpeg" under "Files and media" in `docs/dev/design.md`). This spike tests that. Each step's results land here as it runs.

A second track ran beside it at first: a native ffmpeg that Dens would install and confine with each operating system's sandboxing. It stopped after its builds (B1, in the results). Dens doesn't recompress what members send, which was the one job that needed native speed, and the module holds a hostile file the same way on every platform, where a native ffmpeg needs confinement written and tested for each one, and a second program to build, sign and ship.

### Questions

1. **Stripping.** Whether copying the streams into a new container through FFmpeg's libraries leaves no location, device, owner or date in phone videos and audio files, keeps what playback needs, such as a phone video's rotation, and how fast it runs, in how much memory, as files grow to several gigabytes.
2. **Stills and posters.** How long the photo formats Dens refuses today take to become a JPEG or PNG, upright and in their own colors: HEIC, AVIF, TIFF and DNG, JPEG 2000, Photoshop and JPEG XL. And how long a video's first frame takes to become its preview.
3. **Size.** How much the module adds to the Dens binary, and whether its translation compiles on a CI runner in reasonable time and memory.
4. **Containment.** Whether the translation keeps the module's guarantees. wasm2go assumes it translates trusted modules; ours is one, but a decoder taken over by a hostile file runs inside it. Every trap, overrun and runaway must end the job and nothing else, and the module must reach nothing but the functions Dens gives it.
5. **Licenses.** The module is linked into Dens, so it keeps to what the LGPL allows, and which decoders and encoders that leaves.

### How it works

- **FFmpeg 9.0**, the current release, built for `wasm32-wasip1` with wasi-sdk: single-threaded, with no assembly, network or programs, and only the demuxers, muxers, parsers, decoders and encoders Dens would use, with zlib for PNG and TIFF.
- **A driver of Dens's own**, in C, takes the place of the `ffmpeg` command, which needs threads since FFmpeg 7.0. It exports four operations, probe, strip, still and poster, and does all its I/O through functions the Go side provides: read and seek the input, write and seek the output, and log. FFmpeg still parses and writes every container. The driver only chooses what to copy: the video and audio streams, without metadata, chapters, attachments, subtitles or data tracks, but with the side data playback needs, such as rotation. It assembles a HEIC from its tiles, which FFmpeg hands over as a stream group.
- **wasi-libc's imports** get a few Go functions: standard error goes to the log, the clock and randomness work, and anything that would open a file or a socket, or start a process, fails.
- **binaryen's `wasm-opt`, then wasm2go**, as go-sqlite3's translation of SQLite does, into a Go package the spike's command links.
- **A worker process.** The spike's command runs each job in a child copy of itself, as Dens would run a hidden command, with a cap on the module's memory and a deadline, and reports time and peak memory.
- **A native build of the driver**, with the system's C compiler, shows what the translation costs.
- **Pins.** wasi-sdk, binaryen, exiftool, and the FFmpeg and zlib sources go into `scripts/vendor.sh` by version and SHA-256. wasm2go comes through the Go checksum database.

Running it, from the repository's root:

```sh
spikes/ffmpeg/build.sh             # FFmpeg for WebAssembly, the module, its translation, and the native driver
spikes/ffmpeg/fixtures.sh [large]  # the generated files, and with "large" the 1, 2 and 4 GB videos
spikes/ffmpeg/bench.sh LABEL       # strip the large videos, translated and native
out/spikes/ffmpeg/ffspike probe IN
out/spikes/ffmpeg/ffspike strip IN OUT [MUXER]
out/spikes/ffmpeg/ffspike still IN OUT [MAX_SIDE [QUALITY]]
out/spikes/ffmpeg/ffspike poster IN OUT [MAX_SIDE [QUALITY]]
out/spikes/ffmpeg/ffspike trap unsafe|safe NAME [ARG]
out/spikes/ffmpeg/ffspike fuzz [-duration D] [-jobs N] ASAN_DRIVER SEED...
out/spikes/ffmpeg/ffspike check EXIFTOOL ORIGINAL STRIPPED
```

`build.sh` takes `OPT` for FFmpeg's and the driver's optimization (default `-Oz`), `WOPT` for wasm-opt's (default `-Oz`), and `SMALL=1` for FFmpeg's `--enable-small`. Everything lands in `out/spikes/ffmpeg/`, and the translation in the gitignored `spikes/ffmpeg/ffwasm/`.

### Files to test with

- **Phone samples** in the gitignored `test-files/mobile/`: 16 public test fixtures with a location in the EXIF GPS block, QuickTime keys or QuickTime user data. iPhone HEICs at 12 megapixels, two of them rotated and one with an HDR gain map; iPhone MOVs in H.264 and HEVC, most of them rotated, one a Live Photo's video; Android MP4s; and JPEGs, two of them Pixel motion photos with a video after the image. Its manifest lists each file's source and SHA-256. The JPEGs also go through today's `internal/media` stripper, as a check on it. A 48-megapixel HEIC is still to find. Results name tags, never their values.
- **Made by the spike:** short videos and audio files in MP4, MOV, Matroska, WebM, MP3, M4A, FLAC and Ogg, each carrying what its container can hold: location (MP4's `©xyz` and Apple's keys included), make, model, software, dates, chapters, cover art, attachments and a subtitle track of coordinates, as some drones write, plus a rotated video. exiftool, a reader independent of FFmpeg, must find all of it in each input and none of it in the output.
- **Large files:** one video repeated to 1, 2 and 4 GB.
- **Damaged files:** copies of the files above, cut short or with random bytes changed, run through every operation for an hour.

### Steps

Each step records its numbers here.

1. **A1, build** FFmpeg with only what probing and stripping need, the driver, and the translation. Module and Go source sizes, compile time and peak memory, and how much a binary grows, for Linux and Windows on amd64 and arm64. Done.
2. **A2, strip** the files above: exiftool before and after, rotation kept, and which metadata inside the codec streams a copy keeps. Outputs played by hand in the target browsers. Throughput and peak memory by file size, on Linux and Windows, beside the native driver. Done.
3. **B1, native builds** for the four targets, against a third-party build, for size, contents, licenses and what shipping each would take. Done; the native track ends here.
4. **A4, the translation's containment:** read what it does with an out-of-bounds access, a bad indirect call, `unreachable`, the integer operations that trap, and a stack that runs out, with and without wasm2go's `-unsafe`, and check that each ends the job and nothing else. Balloon memory and loop past the deadline on purpose. Run the damaged files through every operation, beside the native driver built with AddressSanitizer, which shows which of them hit real memory bugs the module had to hold.
5. **Stills and posters:** add the image decoders, swscale and the JPEG and PNG encoders. The HEICs, the other formats, and the first frames of 1080p and 4K H.264 and HEVC videos: time, memory, orientation, and whether a Display P3 photo keeps its colors. What the encoders write from a frame's side data, since FFmpeg attaches a HEIF image's EXIF to its stream. Then dav1d for AVIF, and libjxl if it builds. Done; AVIF waits until after v1, and JPEG XL wasn't attempted.
6. **Write-up:** the results against what counts as a go, and the design doc's ffmpeg section and open question updated to match.

### What counts as a go

- Probing and stripping add at most 10 MB to the binary, and everything through stills and posters at most 35 MB; the Linux amd64 binary is about 33 MB today. The translation compiles on a 16 GB CI runner in under 10 minutes.
- Stripping any file up to a den's per-file limit takes under 128 MB, and runs at 50 MB/s or more. MP4's frame index grows with the video, by about 16 MB per GB of 1080p phone video, so that holds to about 8 GB; Matroska's stays flat.
- A 48-megapixel HEIC converts in under 10 seconds within 512 MB, and a 4K video's first frame in under 5 seconds.
- Every failure in A4 ends the job, never the process that started it.
- exiftool finds nothing left in any stripped file.

A miss isn't a no-go by itself. Some have their own fixes, such as fewer formats, a bigger budget or threads, and the write-up says which apply.

### Results

FFmpeg 9.0.2, configured as LGPL version 2.1 or later; wasi-sdk 34 (clang 23), binaryen 133 and wasm2go v0.4.16. The development machine has 16 cores and runs Linux in WSL2; the Windows runs were on the same machine.

#### A1, build

- FFmpeg's strip-only build for `wasm32-wasip1` configures and builds in about 15 seconds. It needs one of wasi-libc's emulation libraries, for `clock()`.
- The module imports its memory, Dens's five functions (read, write, seek, log and result), and the 15 WASI functions wasi-libc calls: the environment, the clock, `fd_*` calls for the standard streams and preopened directories, `path_open` (FFmpeg's random seed tries `/dev/urandom`), `poll_oneoff` (sleeping) and `proc_exit`. The Go side answers `path_open` with "not capable", and every descriptor past the standard streams with "bad descriptor".
- The module's memory is one Go slice, its capacity the memory cap, so growing never copies it, and pages the module never touches cost no RAM.
- Sizes, with the translation linked into a small program for Linux amd64. Binaries are in MiB; the Linux amd64 Dens binary is 31.6 MiB (33.2 MB).

| FFmpeg and driver | wasm-opt | Module | Go source | Binary grows by |
| --- | --- | --- | --- | --- |
| `-O2` | `-O3` | 1.45 MB | 7.4 MB | 12.1 MiB |
| `-Oz` | `-Oz` | 1.20 MB | 5.6 MB | 9.6 MiB |
| `-O2`, `--enable-small` | `-O3` | 1.42 MB | 7.3 MB | 12.0 MiB |
| `-Oz`, `--enable-small` | `-Oz` | 1.18 MB | 5.5 MB | 9.5 MiB |

- With `-Oz`, the default, the binary grows by 9.6 MiB on Linux amd64, 9.0 MiB on Linux arm64, 9.6 MiB on Windows amd64 and 9.1 MiB on Windows arm64.
- A cold Go build of the translation takes 6.9 seconds on 16 cores (27 seconds of CPU) and peaks at 1.6 GB, for Linux and Windows alike. At `-O2` it takes 9.4 seconds and peaks at 2.4 GB.

#### A2, strip

- Probing a phone video takes 3 to 8 ms in the module, and the worker peaks at 9 to 19 MB.
- exiftool finds nothing left in any of the 16 stripped files, the 6 phone videos and the 10 generated ones. Gone: locations in QuickTime keys and user data (`©xyz`); Apple's make, model, software, creation date, lens and Live Photo identifiers; Android's version; ID3, iTunes, Vorbis comment, RIFF INFO and Matroska tags; chapters, cover art, attachments and subtitle tracks; and Apple's timed-metadata tracks. Dates a container must have are zero. What's left FFmpeg writes itself: the file type's minor version, MP4's standard "Apple" handler vendor, its own vendor code "FFmpeg", AAC's roll recovery group, and "ffmpeg" as the Vorbis comment vendor; the rest is the format's own versions, and FLAC's checksum of the decoded audio.
- The check (`ffspike check`) compares exiftool's tags before and after by name, and never prints a value. Any tag whose name could say where, when, on what or by whom a file was made counts as left behind, whether or not its value changed, unless it's zero or on a short list of fields that describe the format.

| File | Sensitive tags removed | Dates zeroed | Left |
| --- | --- | --- | --- |
| android-6-0-1-model-unknown-h264.mp4 | 4 | 10 | none |
| android-nokia-6-1-h264.mp4 | 2 | 10 | none |
| iphone-12-pro-hevc.mov | 56 | 10 | none |
| iphone-14-pro-live-photo-hevc.mov | 59 | 10 | none |
| iphone-6-h264.mov | 30 | 10 | none |
| iphone-x-h264.mov | 30 | 10 | none |
| meta.flac | 13 | 0 | none |
| meta.m4a | 8 | 6 | none |
| meta.mkv | 18 | 0 | none |
| meta.mov | 35 | 10 | none |
| meta.mp3 | 13 | 1 | none |
| meta.mp4 | 34 | 10 | none |
| meta.ogg | 12 | 0 | none |
| meta.wav | 6 | 0 | none |
| meta.webm | 15 | 0 | none |
| rotated.mp4 | 8 | 10 | none |

- The driver copies a stream's side data only from a list of what playback needs: rotation, stereo and 360° layouts, cropping, Dolby Vision and HEVC configuration, and HDR's light levels, mastering display and viewing environment. Anything else stays behind, such as the EXIF block, GPS included, that FFmpeg attaches to a HEIF image's stream, or an ICC profile's descriptions. None of the files here carried any.
- Kept for playback: every rotation, HDR's ambient viewing environment, and frame cropping. Lost: the iPhone 12 Pro's Dolby Vision configuration. FFmpeg writes it only into MP4, and only with `-strict unofficial`, never into QuickTime MOV. Without it the video plays as its HLG base layer; writing phone MOVs out as MP4 would keep it.
- Inside the codec streams: the Android videos carry no SEI messages. The iPhone videos carry Apple's "user data unregistered" SEI in nearly every frame: 19 bytes under UUID `47564adc-5c4c-433f-94ef-c5113cd143a8`, or 1 byte under `0387f44e-cd0a-4bdc-a194-3ac3d49b171f` on the iPhone X, with no readable text. A copy keeps it; FFmpeg's `filter_units` bitstream filter could drop it.
- Matroska stores presentation times only. Without decoders FFmpeg can't tell how far a stream reorders its frames, so the decode times it infers for the iPhone's HEVC ran backwards and the muxer refused them. The driver derives its own from the presentation times, over the deepest reordering H.264 and HEVC allow.
- The translated driver on Linux and on Windows, and the same driver natively, write byte-identical files.
- Large files, stripped into the same container on Linux. Times vary by up to three times between runs, since the disk sets the pace; CPU time follows the wall time.

| File | Translated | Native | Translated peak (module) | Native peak |
| --- | --- | --- | --- | --- |
| 1 GB MOV | 1.6 s | 0.7 s | 30 MB (24 MB) | 17 MB |
| 2 GB MOV | 3.9 s | 1.2 s | 42 MB (38 MB) | 25 MB |
| 4 GB MOV | 6.7 s | 2.9 s | 67 MB (64 MB) | 39 MB |
| 1 GB MKV | 1.4 s | | 12 MB (6 MB) | |
| 4 GB MKV | 7.2 s | | 13 MB (6 MB) | |

- On Windows, the translated driver stripped the 1 GB MOV in 0.64 seconds and the 4 GB one in 2.5 seconds, peaking at 32 and 69 MB.
- Memory for MP4 and MOV grows with the number of frames, natively too: the format indexes every frame, and the writer holds the copy's whole index until it writes it at the end, about 16 MB per GB of 1080p phone video. Matroska stays flat. So "no more memory for 4 GB than for 40 MB" can't hold for MP4; "under 128 MB" holds to about 8 GB.
- Played by hand in Chrome, Firefox and Waterfox, and in VLC, the stripped files play as their originals do. What a browser won't play, it wouldn't play before stripping either:
  - Chrome downloads a `.mov` file opened on its own instead of playing it. Written out as MP4, the same streams are an ordinary MP4.
  - The iPhone X's video and the Live Photo's carry raw PCM audio, which no browser plays in MOV. Written out as MP4 it becomes ISO PCM (`ipcm`), and Chrome plays the iPhone X's video that way, sound included.
  - The generated MP3 is silent by construction, since FFmpeg has no MP3 encoder: its frames are valid and empty.
  - The generated videos played only in VLC: they were MPEG-4 Part 2, FFmpeg's own encoder, which browsers don't decode. They're now copies of the Android sample's H.264 and AAC, whose own stripped copy plays in all three; the new set hasn't been played by hand.

#### A4, the translation's containment

- The translated Go imports only `embed`, `encoding/binary`, `math`, `math/bits`, `runtime` and `unsafe`. Nothing reaches a file, a socket or a process except through the host functions Dens provides.
- `-unsafe` makes no difference to containment. The six helpers that use `unsafe`, the 16-, 32- and 64-bit loads and stores, each check the address against the memory's length first. Byte loads and stores index the slice. An access's offset is added in 64 bits, so it can't wrap around to low memory, as in WebAssembly.
- `traps.c` is a module with one export per attempt, built and translated as the driver is, with and without `-unsafe`. `ffspike trap` runs each in the worker. Every attempt behaves the same in both translations:

| Attempt | What happens |
| --- | --- |
| Load or store at the end of memory, across it, or at the top of the address space | Panics: index out of range |
| Fill or copy past the end of memory | Panics, once the host's memory has no spare capacity; see below |
| Call through a null pointer, past the function table, or with the wrong signature | Panics: nil, index out of range, or the wrong function type |
| `unreachable` | Panics |
| Divide by zero, or the most negative number by −1 | Panics |
| Shadow stack past its 1 MB | Runs below address 0, wraps to the top of the address space, and panics, since the stack comes first in memory |
| Recursion past Go's stack | The worker dies with a fatal stack overflow at the 64 MB limit it sets, after 0.2 s and 134 MB |
| Memory balloon | `malloc` refuses at the cap: 240 MB under 256, 1008 MB under 1024, with the worker at 245 and 1014 MB |
| Endless loop | The parent kills the worker at the deadline |

- A panic is recovered and ends the job with an error. The two failures Go can't recover from, its own stack running out and the deadline, end the worker, and the parent reports them.
- **Bulk memory ran past the end.** wasm2go's `memory.fill`, `memory.copy` and `memory.init` slice the memory with `mem[x:y]`, which Go checks against the slice's capacity, not its length. The spike's host had reserved the whole cap as capacity, so a fill past the end of memory succeeded, writing into pages the module could grow into later; a page grown afterwards wasn't zero. The writes stayed inside memory reserved for the module, but WebAssembly says they trap. The host now gives the module a slice whose capacity is its length, cut from the reserved memory, and they do. Dens's own host must do the same. go-sqlite3's host reserves its capacity with `PROT_NONE`, so there such a write would fault instead of trapping.
- **Damaged files:** ffspike fuzz damages the 16 videos and generated files over and over: flipped bytes, cut short, 32-bit fields set to edge values, chunks copied elsewhere, runs of random bytes, mostly in the first 64 KB where containers keep their headers. Each damaged copy goes through the module's worker and through the native driver built with AddressSanitizer, and where both strip, their outputs are compared byte for byte.
  - In an hour, 12 at a time, 1,191,521 damaged files went through both. They agreed on every one: 905,664 stripped by both, 285,709 refused by both. Nothing trapped, killed a worker or ran past the deadline, and AddressSanitizer found no memory bug. Where both stripped, the outputs were identical but for 148 MP3s.
  - The only differences came from FFmpeg itself: for an MP3 frame with a free-format header, `avpriv_mpegaudio_decode_header` leaves the bitrate unset, and the MP3 writer compares it anyway, so the stack's leftovers decide whether the file's tag says variable (`Xing`) or constant (`Info`) bitrate. AddressSanitizer doesn't see uninitialized reads. FFmpeg's current code keeps the header in the writer's context, which ends it.

#### Stills and posters

The module gains FFmpeg's HEVC, H.264, MJPEG, TIFF, JPEG 2000 and Photoshop decoders, the TIFF, JPEG 2000 and Photoshop demuxers, swscale, and the JPEG and PNG encoders, with zlib 1.3.2. The driver exports two more operations:

- `dm_still` turns an image into a JPEG, or a PNG when it has transparency. It assembles a tile grid, which FFmpeg hands over as a stream group, and crops it to the image the grid presents. It turns the image as its display matrix says, the way the ffmpeg command reads one (`fftools/ffmpeg_filter.c`), keeps its ICC profile as Dens keeps it in a JPEG, PNG or WebP, and scales it to fit a size if asked.
- `dm_poster` makes a video's preview: its first frame, cropped as the container asks, turned, and fitting 640 × 640.
- The encoders get a fresh frame carrying only the ICC profile, so nothing else the source carried, EXIF included, reaches them, and they're set bit-exact, so the JPEG names no encoder.
- The image demuxers call two more WASI functions, `fd_readdir` and `path_filestat_get`, looking for numbered sequences on disk; the host refuses both.
- The module needs more memory before it runs, 43 pages here. The host must read that from the module's memory import, not assume it: with the capacity fix from A4, the first stills trapped until it did.

**HEIC.** Your four iPhone HEICs are each 48 tiles of 512 × 512 on a 4096 × 3072 canvas, cropped to 4032 × 3024; the iPhone 15 Pro's and XR's turn a quarter clockwise. Against the ffmpeg command's own rendering, from the same FFmpeg, every still has the same size and orientation, and differs only by the JPEG's loss. exiftool finds nothing left but the ICC profile: Apple's Display P3, in two versions.

| File | Translated | Native | Worker peak | PSNR against ffmpeg |
| --- | --- | --- | --- | --- |
| iphone-15-pro.heic | 0.57 s | 0.31 s | 79 MB | 46.6 dB |
| iphone-7.heic | 0.49 s | 0.29 s | 67 MB | 46.3 dB |
| iphone-8.heic | 0.64 s | 0.38 s | 71 MB | 43.8 dB |
| iphone-xr.heic | 0.71 s | 0.42 s | 80 MB | 43.5 dB |

**Other formats**, from exiftool's test images and 12-megapixel files made from a still, with GPS, make, model, serial number, artist and XMP written by exiftool:

- TIFF and JPEG 2000 convert: a 12-megapixel TIFF in 0.30 s (0.19 s natively), and a JPEG 2000 in 0.95 s (0.73 s) within 124 MB. Nothing exiftool wrote is left.
- Photoshop files convert; a paletted TIFF becomes a PNG, as its palette may hold transparency.
- DNG and Canon's CR2 don't decode: FFmpeg's TIFF decoder doesn't handle their tiles and raw data. A Nikon NEF gives only its small preview. Camera raw stays refused.
- **A 48-megapixel image** (a TIFF, as there's no 48-megapixel HEIC to hand) converts in 1.25 s (0.75 s natively), with the worker at 213 MB and the module's memory at 468 MB, under a 512 MB cap. That fits once the driver holds at most two full images at a time, scaling into the output when nothing turns and freeing each image as soon as it's used; before, the module's memory reached 597 MB and the cap refused it. A 48-megapixel HEIC adds four times the 12-megapixel decoding, which puts it near 2.5 s.

**Posters**, against the ffmpeg command's first frame scaled to the same size. Phone videos store a cropping rectangle beside the stream, which the ffmpeg command applies; without it, the Live Photo's and the iPhone X's posters showed the wrong part of the frame (19 and 16 dB).

| File | Translated | Native | Worker peak | PSNR against ffmpeg |
| --- | --- | --- | --- | --- |
| android-6-0-1-model-unknown-h264.mp4 | 0.11 s | 0.07 s | 24 MB | 42.0 dB |
| android-nokia-6-1-h264.mp4 | 0.24 s | 0.14 s | 31 MB | 41.2 dB |
| iphone-12-pro-hevc.mov | 0.24 s | 0.13 s | 51 MB | 38.7 dB |
| iphone-14-pro-live-photo-hevc.mov | 0.25 s | 0.15 s | 43 MB | 40.6 dB |
| iphone-6-h264.mov | 0.01 s | 0.00 s | 15 MB | 43.6 dB |
| iphone-x-h264.mov | 0.21 s | 0.14 s | 29 MB | 39.4 dB |
| rotated.mp4 | 0.07 s | 0.04 s | 18 MB | 42.0 dB |
| meta.mkv | 0.11 s | 0.07 s | 23 MB | 42.0 dB |

- The iPhone 12 Pro's video is 10-bit HLG. Neither its poster nor the ffmpeg command's maps HDR to SDR, so both look washed out; that needs a tone mapper, which this build has none of. No sample is 4K; a 1080p HEVC poster takes 0.24 s, so a 4K one, with four times the pixels, would be near 1 s.
- Every still and poster above is byte-identical to the native driver's. The translation costs 1.3 to 1.9 times native.

**AVIF and AV1**, tried with dav1d 1.5.4 and left out. dav1d built for WASI with meson and ninja, given `-D_GNU_SOURCE` as emscripten gets, and with its threads dependency emptied, since `-pthread` asks for shared memory; it starts no thread when asked for one. Five of libavif's test images converted, a grid, a crop with a turn and a mirror, a gain map (kept as its SDR base) and EXIF, XMP and GPS that went, and Netflix's 2048 × 858 lossless AVIF took 0.49 s (0.29 s natively). AVIF and HEIF keep transparency as a second image FFmpeg doesn't merge, so a transparent AVIF lost it. AVIF now waits until after v1, when Dens strips it in place instead and keeps the file as sent, and dav1d goes until then; so do the VP8 and VP9 decoders, which served only WebM posters. WebM and AV1 videos still probe and strip: a VP9 WebM from libvpx's test vectors and an AV1 Matroska file from libaom's report their sizes, 1920 × 1080 and 352 × 288, from FFmpeg's parsers alone, and strip.

**JPEG XL** wasn't attempted. libjxl needs cmake and C++, with highway and brotli; phones don't save JPEG XL files on their own, and only Safari shows them.

**Size**, with the translation linked into a small program:

| Decoders | Module | Go source | Linux amd64 binary grows by | Cold build |
| --- | --- | --- | --- | --- |
| All, with VP8, VP9 and dav1d | 3.60 MB | 19.7 MB | 35.7 MiB | 28 s, 6.7 GB |
| Without VP8 and VP9 | 3.13 MB | 16.2 MB | 29.8 MiB | 22 s, 5.0 GB |
| Without VP8, VP9 and dav1d (kept) | 2.69 MB | 14.4 MB | 25.8 MiB | 20 s, 4.2 GB |

- The kept build grows a binary by 25.8 MiB on Linux amd64, 24.3 MiB on Linux arm64, 25.9 MiB on Windows amd64 and 24.3 MiB on Windows arm64; the Linux amd64 binary itself is 31.6 MiB.
- A cold build's peak comes from the largest functions, not from compiling many at once: held to 4 CPUs it still peaks at 4.7 GB. VP9's inter prediction made one function of 44,384 lines after wasm-opt; without VP9 the largest has 9,037.
- Of the module's code, swscale is about an eighth.

**Damaged files** (`ffspike fuzz`, now with stills and posters, against the native driver under AddressSanitizer):

- An hour of 160,923 runs over the HEICs, the AVIFs, exiftool's TIFF, JPEG 2000 and Photoshop images, and phone videos for stripping and posters: the module and the native driver agreed on every one, nothing trapped or killed a worker, and AddressSanitizer found no memory bug. One damaged AVIF grid ran past the 20-second deadline in both. Ten stills differed from the native ones, all from one 10-bit 4:4:4 AVIF through dav1d: the two native builds agree with each other and the module differs repeatably, so it isn't uninitialized memory; it left with AVIF. Ten minutes of posters of a cut of the iPhone 12 Pro's 10-bit HEVC, 38,920 runs, matched byte for byte.
- **A bug in the spike's own driver,** caught in a first minute of fuzzing: freeing the decoded image before copying its ICC profile, when the profile lives on that image, as a TIFF's does, read freed memory. AddressSanitizer flagged it 200 times. The module gave the right output every time, since the freed bytes were still in place: it holds a bug like this inside its memory, but doesn't find it. Fuzzing Dens's own C code natively under AddressSanitizer belongs in its tests for that reason. Fixed.
- **Copying aspect ratios:** an AV1 Matroska file whose container states a pixel aspect ratio of 35:32 and whose codec states 1:1 failed to strip: the driver copied one to the stream and the other to its parameters, and the muxer refused the mismatch. It now copies the container's to both, as the ffmpeg command does, which also writes an explicit 1:1 box into two of the phone videos' copies.

#### B1, builds

`native.sh` and the zig and BtbN pins that made these are in commit `8f5963e`; they went with the native track.

- **Dens's own:** zig 0.16 cross-compiles FFmpeg's strip-only libraries and the driver for all four release targets from one Linux machine, statically linked: musl on Linux, MinGW's runtime on Windows. Builds take 23 to 25 seconds for Linux and 170 to 234 seconds for Windows the first time, while zig compiles MinGW's runtime once. Without symbols, the driver is 2.0 MB on Linux amd64, 1.8 MB on Linux arm64, 2.0 MB on Windows amd64 and 1.8 MB on Windows arm64. zig's linker refuses two flags FFmpeg's configure gives MinGW, which only place the image (`--pic-executable`, `--image-base`); `--dynamicbase` stays. On Windows, FFmpeg's random seed links `bcrypt`.
- The static Linux driver and the Windows one write byte-identical files to the translated module's.
- **BtbN's:** the 9.0 builds are taken daily from the branch's head (`n9.0.2-17-g2a571b6068` on September 30), unsigned, with a SHA-256 digest per asset on the GitHub release. The LGPL archives are 112 to 164 MB, the GPL ones 121 to 185 MB. On Linux, `ffmpeg` alone is 142 MB, dynamically linked against glibc and 8 other system libraries; on Windows it is 134 MB. ffprobe and ffplay come alongside, each as large. The LGPL build is configured `--enable-version3`, so it is LGPL version 3, and links about fifty outside libraries, network ones among them: SSH, SRT, RIST, ZeroMQ, PulseAudio, XCB, an XML parser and an SVG renderer.
- BtbN's `ffmpeg -map 0:v -map 0:a -map_metadata -1 -map_chapters -1 -c copy -fflags +bitexact` strips at the same speed and peaks at the same memory as the driver (69 MB for the 4 GB MOV), and exiftool finds nothing left. Its file differs from the driver's only by an audio channel layout box, which it can write because its build decodes AAC; the strip-only driver doesn't know the layout.

What shipping each would take:

- **Dens's own, embedded:** the release builds the driver for each target with zig and embeds it in the Dens binary for that target, adding about 2 MB, and more once it decodes. `dens install` and `dens update` write it beside the binary in their transactions, root-owned and read-only. No second artifact, signature or download: it's signed and updated with the binary.
- **Dens's own, published:** the same builds as release artifacts beside the binary, signed with cosign, fetched and verified by the installer scripts and `dens update`. A second artifact per target in the fixed release layout.
- **BtbN's:** the installers fetch a pinned archive of 112 to 185 MB from GitHub, verified only by the pinned hash, and extract `ffmpeg`. Each FFmpeg security fix means a new pin, and BtbN keeps a month's last build for two years. Mirroring it on Dens's release host makes Dens the one offering the source of FFmpeg and its fifty libraries.

### Write-up

**Go**: Dens runs FFmpeg as a WebAssembly module translated to Go, in a worker process. The native track ended after its builds: a native ffmpeg needs confinement written and tested for each OS and a second program to ship, and with recompression dropped nothing Dens does needs native speed.

| Goal | Result |
| --- | --- |
| Probing and stripping add at most 10 MB | 9.6 MiB (10.06 MB) on Linux amd64, at the limit |
| Everything through stills and posters adds at most 35 MB | 24.3 to 25.9 MiB on the four release targets, without VP8, VP9 and dav1d |
| Compiles on a 16 GB CI runner in under 10 minutes | 20 s cold, peaking at 4.2 to 4.7 GB |
| Stripping any file up to the den's limit under 128 MB | Under 70 MB for a 4 GB phone video; MP4's frame index grows 16 MB per GB, so this holds to about 8 GB, and Matroska stays flat |
| Stripping at 50 MB/s or more | 0.5 to 2 GB/s, at the disk's pace, on Linux and Windows |
| A 48-megapixel HEIC in under 10 s within 512 MB | No 48-megapixel HEIC was found. A 48-megapixel TIFF takes 1.25 s within a 512 MB cap, and four times a 12-megapixel HEIC's decoding puts a HEIC near 2.5 s |
| A 4K video's first frame in under 5 s | No 4K sample; a 1080p HEVC poster takes 0.24 s, so 4K would be near 1 s |
| Every failure in A4 ends the job, never its parent | Yes, once the host gives the module a slice with no spare capacity |
| exiftool finds nothing left in any stripped file | Nothing in 16 videos and audio files, 4 HEICs and the TIFF, JPEG 2000 and Photoshop files, but the ICC profile Dens keeps |

What the implementation in Dens has to carry over from here, all in `docs/dev/design.md` under "ffmpeg":

- The host's rules: refuse file, directory and socket calls; a memory slice with no spare capacity; the module's declared minimum memory; traps recovered as job errors; a worker process with a memory cap, a deadline and a lower Go stack limit.
- The driver's rules: video and audio only, side data from a list (with the ICC profile), the container's aspect ratio for stream and codec, Matroska's decode times derived from presentation times, no encoder tag, MOV written as MP4, SEI user data dropped. A still or a poster goes to the encoder as a fresh frame, cropped and turned as the source asks, with at most two full images held.
- The tests: the trap module's attempts, exiftool's check of every kind of file, and the driver fuzzed natively under AddressSanitizer beside the module. That fuzzing found a use-after-free in the driver that the module had held without a trace.

Left for when it's built: whether `filter_units` drops the iPhone's SEI user data without changing playback, and whether Firefox plays an MP4 of the iPhone's PCM audio, which Chrome does. wasm2go's check of bulk memory against capacity, not length, may be worth reporting to its author: go-sqlite3's host reserves its spare capacity unmapped, so there such a write would fault, not trap.
