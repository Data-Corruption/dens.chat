# Spikes

Throwaway experiments that answer a design question before the code that depends on it. Each is a directory in this Go module, whose dependencies never reach the product, and each goes once its findings are in `docs/dev/design.md`.

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

- **FFmpeg 9.0**, the current release, built for `wasm32-wasip1` with wasi-sdk: single-threaded, with no assembly, network or programs, and only the demuxers, muxers, parsers, decoders and encoders Dens would use. AVIF and AV1 need dav1d, and JPEG XL needs libjxl, both BSD-licensed. They come after FFmpeg's own code works.
- **A driver of Dens's own**, in C, takes the place of the `ffmpeg` command, which needs threads since FFmpeg 7.0. It exports four operations, probe, strip, still and poster, and does all its I/O through functions the Go side provides: read and seek the input, write and seek the output, and log. FFmpeg still parses and writes every container. The driver only chooses what to copy: the video and audio streams, without metadata, chapters, attachments, subtitles or data tracks, but with the side data playback needs, such as rotation. It assembles a HEIC from its tiles, which FFmpeg hands over as a stream group.
- **wasi-libc's imports** get a few Go functions: standard error goes to the log, the clock and randomness work, and anything that would open a file or a socket, or start a process, fails.
- **binaryen's `wasm-opt`, then wasm2go**, as go-sqlite3's translation of SQLite does, into a Go package the spike's command links.
- **A worker process.** The spike's command runs each job in a child copy of itself, as Dens would run a hidden command, with a cap on the module's memory and a deadline, and reports time and peak memory.
- **A native build of the driver**, with the system's C compiler, shows what the translation costs.
- **Pins.** wasi-sdk, binaryen, exiftool, and the FFmpeg, dav1d and libjxl sources go into `scripts/vendor.sh` by version and SHA-256. wasm2go comes through the Go checksum database.

Running it, from the repository's root:

```sh
spikes/ffmpeg/build.sh             # FFmpeg for WebAssembly, the module, its translation, and the native driver
spikes/ffmpeg/fixtures.sh [large]  # the generated files, and with "large" the 1, 2 and 4 GB videos
spikes/ffmpeg/bench.sh LABEL       # strip the large videos, translated and native
out/spikes/ffmpeg/ffspike probe IN
out/spikes/ffmpeg/ffspike strip IN OUT [MUXER]
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
5. **Stills and posters:** add the image decoders, swscale and the JPEG and PNG encoders. The HEICs, the other formats, and the first frames of 1080p and 4K H.264 and HEVC videos: time, memory, orientation, and whether a Display P3 photo keeps its colors. What the encoders write from a frame's side data, since FFmpeg attaches a HEIF image's EXIF to its stream. Then dav1d for AVIF, and libjxl if it builds.
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
- Sizes, with the translation linked into a small program for Linux amd64:

| FFmpeg and driver | wasm-opt | Module | Go source | Binary grows by |
| --- | --- | --- | --- | --- |
| `-O2` | `-O3` | 1.45 MB | 7.4 MB | 12.4 MB |
| `-Oz` | `-Oz` | 1.20 MB | 5.6 MB | 9.8 MB |
| `-O2`, `--enable-small` | `-O3` | 1.42 MB | 7.3 MB | 12.3 MB |
| `-Oz`, `--enable-small` | `-Oz` | 1.18 MB | 5.5 MB | 9.7 MB |

- With `-Oz`, the default, the binary grows by 9.8 MB on Linux amd64, 9.2 MB on Linux arm64, 9.8 MB on Windows amd64 and 9.3 MB on Windows arm64.
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
