# Spikes

Throwaway experiments that answer a design question before the code that depends on it. Each is a directory in this Go module, whose dependencies never reach the product, and each goes once its findings are in `docs/dev/design.md`.

## Video copies (M5.4)

A phone video too large for a den should go as a smaller copy that fits, and every video should go smaller by default, as photos do since M5.3. The design names two ways to make the copy (see "The video spike" in `docs/dev/design.md`), and this spike builds and measures both:

- **A, the media module:** libaom's realtime AV1 encoder in WebAssembly beside FFmpeg, translated to Go like the rest of the module. One engine on every platform, whatever the browser.
- **B, WebCodecs:** the browser's own decoders and encoders, often in hardware, with the module demuxing the original and muxing the copy.

### How

- **`native.sh`** encodes the phone videos with the pinned native FFmpeg in `tools/` (BtbN's LGPL build of FFmpeg 9.0.2): libaom's realtime AV1 at speed 8 and libvpx's realtime VP9, on one thread without SIMD, as the module would run, at 720p and 1080p at three bitrates each, at 30 frames a second at most. Each copy is scored with VMAF and SSIM against the original scaled the same way, frame for frame.
- **`build.sh`** builds libaom 3.15.2 (its realtime encoder alone: eight bits, generic C, no threads, `-O2`) and FFmpeg for WebAssembly as `scripts/ffmpeg.sh` builds the module, links them with the module's driver and the spike's transcoder (`driver/transcode.c`), translates the result with wasm2go, and builds the same C natively. **`bench`** runs the translation with the worker's host functions.
- **`probe`** is a page and its server: what a browser's WebCodecs decodes and encodes, how fast, and how well, its copies scored as `native.sh` scores its own. It ran headless in Chromium 154 and Firefox ESR 153 on Linux, in a container without a GPU, and in Chrome 154, Edge 155, Firefox 157 and Waterfox (Firefox 153) on Windows 11, on a Ryzen 7 9800X3D (8 cores, 16 threads) with an RTX 4090.
- **The videos** are the phone fixtures in `test-files/mobile/videos`: an iPhone 12 Pro's 21.7 seconds of HEVC Main 10 in HLG at 1080p and 60 frames a second, shot through a rainy train window, the hardest and longest; an iPhone 14 Pro's HEVC at 1920 × 1440; an iPhone X's H.264 at 1440 × 1080; and a Nokia 6.1's H.264 at 1080p, each two or three seconds.

```sh
spikes/video/native.sh [VIDEO...]           # results in out/spikes/video/native/results.csv
spikes/video/build.sh [8|10]                # VARIANT=o2 OPT=-O2 for FFmpeg built for speed
(cd spikes && go run ./video/bench -in IN -out OUT -side 1280 -kbps 1200 -speed 10 -frames 300)
(cd spikes && go run ./video/probe)         # then open http://127.0.0.1:8790/
```

### Results

**AV1 is the codec.** On one thread without SIMD, libvpx's realtime VP9 ran two to three times faster than libaom's realtime AV1, but scored lower at the same size and overshot its bitrate, by up to twice on short clips. libaom kept to within a few percent of its target.

| iPhone 12 Pro, HDR, 8-bit copy | Bitrate | VMAF | SSIM | Native fps, one thread, no SIMD |
| --- | --- | --- | --- | --- |
| 1080p | 2.5 Mbps | 77.7 | 0.923 | 12.1 |
| 720p | 1.2 Mbps | 74.2 | 0.894 | 28.1 |

On the short clips, libaom at 720p and 1.2 Mbps scored 70 to 84, and at 2 Mbps 80 to 87; at 1080p and 2.5 Mbps, 74 to 85.

**HDR.** An eight-bit copy of the HLG video, keeping its HLG and BT.2020 tags so browsers tone-map it, scored a little higher than a ten-bit one (77.7 against 74.9 at 1080p) and encoded 1.7 times faster, and the two looked the same tone-mapped. A ten-bit build doesn't earn its size; banding in a smooth HDR sky is the risk, and this clip has none to show it. FFmpeg keeps a ten-bit source's depth unless the copy's format is named.

**A, the module.**

- It builds. libaom needs CMake, pinned in `scripts/vendor.sh`, and a `setjmp.h` of the spike's own: wasi-libc has no setjmp without exception handling, so setjmp returns 0 and longjmp traps. libaom longjmps only out of a fatal error, which then ends the job, as any trap does.
- Size: the module goes from 2.70 to 4.71 MB of WebAssembly, its translation from 14.5 to 23.8 MB of Go, and a binary grows 17.4 MB, on Linux and Windows alike. With FFmpeg built for speed (`-O2`), 5.44 MB, 29.1 MB of Go, and 28.5 MB more in the binary.
- A cold compile of the translation takes 33 seconds and peaks at 7.9 GB, against 20 seconds and under 5 GB today; with FFmpeg at `-O2`, 38 seconds and 9.6 GB.
- Speed, one worker, the HDR video's 60 frames a second taken to 30, 300 frames:

  | | 1080p | 720p |
  | --- | --- | --- |
  | Decoding and scaling, FFmpeg at `-Oz` | 8.4 fps | 7.4 fps |
  | Encoding too, libaom speed 8 / 10 | 1.8 / 2.0 fps | 3.4 / 3.7 fps |
  | FFmpeg at `-O2`, unreferenced frames skipped, bilinear scaling: decoding | 21.4 fps | 18.1 fps |
  | The same, encoding at speed 10 | 2.3 fps | 5.5 fps |

  The same C natively ran about twice as fast. The iPhone X's H.264 encoded at 5.7 fps at 720p. A worker's module peaked at 105 MiB at 720p and 159 MiB at 1080p. Skipping the frames nothing refers to halves the decoding of a phone's 60-frame video, which carries every other frame as one.
- Workers in parallel, each on its own share of the video, scale: four gave 20.7 fps in all at 720p, and eight 35.1 fps, 85% of eight times one.
- Quality at speed 10, on the HDR video's first ten seconds: VMAF 75.3 at 720p and 1.2 Mbps, 76.8 at 1080p and 2.5 Mbps, about what speed 8 scores.

**B, WebCodecs.**

| Browser | HEVC (iPhone) | AV1 encoding | AV1 at 720p | On its bitrate |
| --- | --- | --- | --- | --- |
| Chrome 154, Edge 155, Windows, RTX 4090 | Hardware only, HLG included, 900 to 1,600 fps | Hardware, ten-bit too; software | 150 to 370 fps in hardware, about 100 in software | Within 20% |
| Chromium 154, Linux, no GPU | No | Software | 70 to 170 fps | Within 20% |
| Firefox 157, Waterfox, Windows; Firefox ESR 153, Linux | No | Software, VP9 too | 6 to 17 fps | Two to four times over |

- Chromium scores VMAF 86 to 93 at 720p and 1.2 Mbps, and at 1080p and 2.5 Mbps, when frames go to the encoder at their own size and it scales them. Drawing them through a canvas first cost 15 to 20 points.
- No browser here decodes HEVC in WebCodecs without hardware for it, and Firefox doesn't at all. iPhones record HEVC unless set otherwise.
- Firefox's encoders run slower than the module's would with a few workers, and miss their bitrate too far to fit a den's limit.

### What it means

- **The module, with workers in parallel,** makes a copy in every browser the same way, and keeps working on Linux and on machines whose GPUs decode no HEVC. At 720p it would make a 30-second clip in about half a minute here, with eight workers at idle priority, and in perhaps two minutes on a four-core laptop. It costs 17 to 28 MB of binary and 3 to 5 GB more to compile the module, and needs work to split a video at its keyframes and join the copies.
- **WebCodecs** makes a copy in seconds in Chrome and Edge with a GPU that decodes HEVC, and costs nothing in the binary, but leaves Firefox and Waterfox without one for an iPhone's video, and with an unfit one for anything else, and Chromium without HEVC hardware without one for an iPhone's video.
- **Both**, WebCodecs where it works and the module elsewhere, is the fastest everywhere and the largest to build and test.
