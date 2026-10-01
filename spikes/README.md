# Spikes

Throwaway experiments that answer a design question before the code that depends on it. Each is a directory in this Go module, whose dependencies never reach the product, and each goes once its findings are in `docs/dev/design.md`.

## ffmpeg: in WebAssembly, or native and confined by the OS

Dens needs ffmpeg to strip video and audio, and to convert the photo formats it can't strip without decoding them. The design compiles ffmpeg to WebAssembly and translates it to Go with wasm2go: one pure-Go build for Linux and Windows, with the module as a sandbox around hostile files (see "ffmpeg" under "Files and media" in `docs/dev/design.md`). This spike tests that, and beside it the alternative: a native ffmpeg that Dens installs and confines with the operating system's own sandboxing. Both tracks run the same files through the same checks, so the write-up can compare them. The spike is planned; each step's results land here as it runs.

### Questions

1. **Stripping.** Whether copying the streams into a new container through FFmpeg's libraries leaves no location, device, owner or date in phone videos and audio files, keeps what playback needs, such as a phone video's rotation, and how fast it runs, in how much memory, as files grow to several gigabytes.
2. **Stills and posters.** How long the photo formats Dens refuses today take to become a JPEG or PNG, upright and in their own colors: HEIC, AVIF, TIFF and DNG, JPEG 2000, Photoshop and JPEG XL. And how long a video's first frame takes to become its preview.
3. **Size.** How much the WebAssembly build adds to the Dens binary, and whether its translation compiles on a CI runner in reasonable time and memory; how large the native builds are.
4. **Containment.** What a decoder taken over by a hostile file can reach. In WebAssembly, whether the translation keeps the module's guarantees: wasm2go assumes it translates trusted modules, and ours is one, but the taken-over decoder runs inside it. Natively, how much the OS can take away from ffmpeg, from inside Dens's own service sandbox, without the service gaining privileges.
5. **Shipping.** Where a native ffmpeg comes from, Dens's own builds or a pinned third-party build, and what installing, updating and releasing it take.
6. **Licenses.** The WebAssembly build is linked into Dens, so it keeps to what the LGPL allows. A native ffmpeg is a separate program, which may be a GPL build with x264 for M5's recompression, with its source offered.

### Track A: WebAssembly

- **FFmpeg 9.0**, the current release, built for `wasm32-wasip1` with wasi-sdk: single-threaded, with no assembly, network or programs, and only the demuxers, muxers, parsers, decoders and encoders Dens would use. AVIF and AV1 need dav1d, and JPEG XL needs libjxl, both BSD-licensed. They come after FFmpeg's own code works.
- **A driver of Dens's own**, in C, takes the place of the `ffmpeg` command, which needs threads since FFmpeg 7.0. It exports four operations, probe, strip, still and poster, and does all its I/O through functions the Go side provides: read and seek the input, write and seek the output, and log. FFmpeg still parses and writes every container. The driver only chooses what to copy: the video and audio streams, without metadata, chapters, attachments, subtitles or data tracks, but with the side data playback needs, such as rotation. It assembles a HEIC from its tiles, which FFmpeg hands over as a stream group.
- **wasi-libc's imports** get a few Go functions: standard error goes to the log, the clock and randomness work, and anything that would open a file or a socket, or start a process, fails.
- **binaryen's `wasm-opt`, then wasm2go**, as go-sqlite3's translation of SQLite does, into a Go package the spike's command links.
- **A worker process.** The spike's command runs each job in a child copy of itself, as Dens would run a hidden command, with a cap on the module's memory and a deadline, and reports time and peak memory.

### Track B: native, confined by the OS

- **Where it comes from**, two ways:
  - Dens's own build: the same FFmpeg 9.0 and driver, built natively in CI for the four release targets, signed with Dens's release identity and published beside the binary.
  - A third-party build: BtbN's FFmpeg-Builds publish LGPL and GPL builds for the same four targets, unsigned, and keep each month's last build for two years. The installer would fetch one pinned by SHA-256, or Dens would mirror it on its release host and take on offering its source.

  Either way, `dens install` and `dens update` would place and verify it inside their transactions, as they do the binary. The spike sketches those changes; it doesn't make them.
- **How it's confined**, a layer at a time, so the write-up shows what each one adds. A separate account alone keeps ffmpeg out of Dens's data, but not off the network or out of anything every user can read; the other layers take those away.
  - Linux: its own systemd unit, started for each job through a socket, with a dynamic user of its own, no network, a read-only system, no new privileges, a system call filter, and cgroup limits on memory and CPU, which also give jobs the lowest priority. The service hands it the open input and output files over the socket. Lighter: a child of the service that confines itself with Landlock and seccomp before it runs ffmpeg, if the service's own filter allows those calls; its unit forbids namespaces.
  - Windows: an AppContainer child in a job object, if a virtual service account can create one; or an account of its own, with a service of its own reached over a named pipe.
  - In Dens, the confinement would be a seam in `internal/platform/host`, and the install steps `System` methods in `internal/install`.
- **A hostile stand-in**: a small program run in ffmpeg's place in each confinement. It tries to read the service's data and the vault, open a socket, write beside its output, start a process and outlive its deadline. Each attempt must fail.
- **The same stripping.** Dens's own build runs the same driver as track A, so the tracks differ only in how ffmpeg runs. A third-party build runs the `ffmpeg` command with the equivalent options.

### Both tracks

- **A native build of the driver**, with the system's C compiler, is track B's own build on the development machine and shows what the WebAssembly translation costs.
- **Pins.** wasi-sdk, binaryen, exiftool, the third-party build, and the FFmpeg, dav1d and libjxl sources go into `scripts/vendor.sh` by version and SHA-256. wasm2go comes through the Go checksum database.

`spikes/ffmpeg/run.sh` will build everything and run the steps, with results in `out/spikes/ffmpeg/<run>/`.

### Files to test with

- **Phone samples** in the gitignored `test-files/mobile/`: 16 public test fixtures with a location in the EXIF GPS block, QuickTime keys or QuickTime user data. iPhone HEICs at 12 megapixels, two of them rotated and one with an HDR gain map; iPhone MOVs in H.264 and HEVC, most of them rotated, one a Live Photo's video; Android MP4s; and JPEGs, two of them Pixel motion photos with a video after the image. Its manifest lists each file's source and SHA-256. The JPEGs also go through today's `internal/media` stripper, as a check on it. A 48-megapixel HEIC is still to find. Results name tags, never their values.
- **Made by the spike:** short videos and audio files in MP4, MOV, Matroska, WebM, MP3, M4A, FLAC and Ogg, each carrying what its container can hold: location (MP4's `©xyz` and Apple's keys included), make, model, serial number, software, dates, chapters, cover art, attachments and a subtitle track of coordinates, as some drones write, plus a rotated video. exiftool, a reader independent of FFmpeg, must find all of it in each input and none of it in the output.
- **Large files:** one video repeated to 1, 2 and 4 GB.
- **Damaged files:** copies of the files above, cut short or with random bytes changed, run through every operation for an hour.

### Steps

Each step records its numbers here. A1 and A2 decide whether WebAssembly is viable at all, so they come first, and the spike stops a track early if its numbers rule it out.

1. **A1, build** FFmpeg with only what probing and stripping need, the driver, and the translation. Module and Go source sizes, compile time and peak memory, and how much a binary grows, for Linux and Windows on amd64 and arm64.
2. **A2, strip** the files above: exiftool before and after, rotation kept, and which metadata inside the codec streams a copy keeps, such as user data in H.264 and HEVC, or EXIF inside motion-JPEG frames. A few outputs are played by hand in the four target browsers. Throughput and peak memory by file size, on Linux and Windows, beside the native driver and the third-party `ffmpeg` command.
3. **B1, builds:** Dens's own native builds for the four targets in CI, for time and size, against the pinned third-party build, for size, contents and license. A sketch of what installing, updating and releasing ffmpeg would change.
4. **B2, confinement on Linux,** in an Incus container, with the service side under Dens's own unit hardening: the hostile stand-in through each layer.
5. **B3, confinement on Windows.** It needs a real virtual service account, so it runs on the CI Windows runner, or in an elevated install on your machine.
6. **Stills and posters,** both tracks: add the image decoders, swscale and the JPEG and PNG encoders. The HEICs, the other formats, and the first frames of 1080p and 4K H.264 and HEVC videos: time, memory, orientation, and whether a Display P3 photo keeps its colors. Then dav1d for AVIF, and libjxl if it builds.
7. **A4, the translation's containment:** read what it does with an out-of-bounds access, a bad indirect call and `unreachable`, with and without wasm2go's `-unsafe`, and check that each ends the job cleanly. Balloon memory and loop past the deadline on purpose, list the module's imports, and run the damaged files through both tracks.
8. **If time allows,** one recompression for M5 in both tracks, a JPEG and a 1080p clip in VP9 or AV1, natively with x264 too, for speed; and whether wasm2go's support for threads would help.
9. **Write-up:** a table of the two tracks, covering what a hostile file can reach, speed, size, build and release work, updates, licenses and OS-specific code; a decision; and the design doc's ffmpeg section and open question updated to match.

### What counts as a go

Proposed, to settle before A1.

WebAssembly:

- Probing and stripping add at most 10 MB to the binary, and everything through stills and posters at most 35 MB; the Linux amd64 binary is about 33 MB today. The translation compiles on a 16 GB CI runner in under 10 minutes.
- Stripping a 4 GB file takes no more memory than a 40 MB one, under 128 MB, and runs at 50 MB/s or more.
- A 48-megapixel HEIC converts in under 10 seconds within 512 MB, and a 4K video's first frame in under 5 seconds.
- Every failure in A4 ends the job, never the process that started it.

Native:

- The hostile stand-in fails every attempt in the chosen confinement, on Linux and Windows.
- The service gains no privilege or capability for it, and its unit stays as hardened as it is.
- Installing and updating ffmpeg stays inside the maintenance transactions, verified and failing closed.

Both: exiftool finds nothing left in any stripped file.

Neither track wins by default. The decision weighs everything in the write-up's table once it's all in: how well each holds a hostile file, against the OS-specific code and the second artifact a native ffmpeg brings, and its speed and GPL encoders, which might suit M5's recompression even if stripping goes the other way. A miss isn't a no-go by itself. Some have their own fixes, such as fewer formats, a bigger budget or threads, and the write-up says which apply.
