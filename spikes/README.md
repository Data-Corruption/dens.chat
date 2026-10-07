# Spikes

Throwaway experiments that answer a design question before the code that depends on it. Each is a directory in this Go module, whose dependencies never reach the product, and each goes once its findings are in `docs/dev/design.md`.

## RNNoise in the page

In M3 a member can turn on RNNoise, Xiph's small noise-suppression network, in place of the browser's own noise suppression (see "Noise suppression" under "Voice and screen share" in `docs/dev/design.md`). It runs in the page, in an AudioWorklet between the microphone and the call. This spike checks that it can, under the page's CSP and in the four target browsers, and what that costs.

### Questions

1. **Building.** Whether RNNoise builds for WebAssembly with the wasi-sdk the media module pins, and what the module needs from its host.
2. **The page.** Whether the page can compile it under its CSP with `'wasm-unsafe-eval'` added, and not without, and hand it to an AudioWorklet, in Chrome, Edge, Firefox and Waterfox.
3. **Cost.** The module's size; CPU per 10 ms frame for the regular and the little model, with and without WebAssembly SIMD; and the delay it adds to a call.
4. **What it takes out.** Fan noise, keyboard clicks and other voices, and what it does to clean speech.
5. **Calls.** A microphone through it at 48 kHz, with the browser's echo cancellation on and its own suppressor off.

### How it works

- **Inputs.** RNNoise 0.2's release tarball for the code, and the model its v0.2 tag names (`model_version`, 0b50c45) from media.xiph.org, both pinned in `scripts/vendor.sh`. The release's code matches the tag's file for file, as GitLab's archive of the tag hashes in MSYS2's PKGBUILD, and the model's SHA-256 is the one Fedora's sources record. The release carries a model of its own, which isn't the one the tag names, so the build unpacks the tag's over it. GitLab builds its archives on the fly, so the pins are the two files that stay put.
- **`build.sh`** compiles a reactor module, with no `main`, for `wasm32-wasip1`: it exports `rnnoise_create`, `rnnoise_process_frame`, `rnnoise_destroy`, `rnnoise_get_frame_size`, `malloc` and `free`, and keeps its stack below its data, so running out of stack traps. `DISABLE_DEBUG_FLOAT` drops the 4.4 MB of float copies the model keeps for debugging; the network runs on its int8 weights. RNNoise's generic vector code, which WebAssembly builds take, includes Opus's `os_support.h`, which RNNoise doesn't carry, for one macro, so `os_support.h` here supplies it. binaryen's `wasm-opt -O3` follows. Four variants: the regular and the little model, each with and without SIMD.
- **`page/worklet.js`** is the processor. The audio thread hands it 128 samples at a time, and RNNoise takes 480 (10 ms at 48 kHz), so it gathers frames, scales them to 16-bit range as RNNoise expects, and queues what comes back. Its output starts a frame behind, which keeps it from running dry: frames of 480 and blocks of 128 line up only every 1,920 samples, and a lead of 448 is the least that works.
- **`page/spike.js`** compiles each variant on the page, hands the module to the worklet, and renders a minute of audio through it in an `OfflineAudioContext`, beside the same path without RNNoise, for its CPU. It lines the output up with the input for the delay, measures how far fan noise, keyboard clicks and other voices drop, and how much of clean speech survives. Then it runs a microphone through it for two seconds. With `listen=1` it saves recordings to listen to.
- **`serve`** hosts the page with Dens's CSP plus `'wasm-unsafe-eval'`, and under `/strict/` with the CSP as it is today, and collects results.
- **`drive.mjs`** runs the page headless in the Windows browsers from WSL, with a throwaway profile and a fake microphone each, and closes each through its debugging protocol.
- **`tts.sh`** makes the speech: a minute of one voice and of another, from Windows' text to speech.

Running it, from the repository's root:

```sh
spikes/rnnoise/build.sh
spikes/rnnoise/tts.sh
(cd spikes && go build -o ../out/spikes/rnnoise/serve ./rnnoise/serve)
out/spikes/rnnoise/serve -root . &
tools/node spikes/rnnoise/drive.mjs [chrome] [edge] [firefox] [waterfox] [--strict] [--listen]
```

### Results

Run on 2026-10-05, on Windows 11 with the browsers headless, from WSL: Chrome 154, Edge 154, Firefox 157 and Waterfox 6.7 (Gecko 153).

**Building.** RNNoise builds with wasi-sdk 34 once `os_support.h` is supplied, and the module imports nothing: no WASI calls, so the worklet gives it no functions at all. It exports its memory, and RNNoise allocates its state and two 480-sample buffers once, then nothing per frame.

| Variant | Size | Gzipped |
| --- | --- | --- |
| regular, SIMD | 1,465,424 bytes | 1,217,405 |
| regular, scalar | 1,452,530 | 1,214,259 |
| little, SIMD | 868,138 | 740,429 |
| little, scalar | 855,244 | 735,670 |

**The page.** All four browsers compile the module on the page under Dens's CSP with `'wasm-unsafe-eval'`, post the compiled `WebAssembly.Module` to the worklet in `processorOptions`, and run it; none needed the fallback of passing bytes. Under today's CSP all four refuse to compile it: Chromium names the missing `'unsafe-eval'` in `script-src`, and Gecko says the call was "blocked by CSP".

**Cost.** CPU per 10 ms frame, from a minute rendered offline, less the same path without RNNoise:

| Browser | regular, SIMD | regular, scalar | little, SIMD | little, scalar |
| --- | --- | --- | --- | --- |
| Chrome | 217 µs | 242 µs | 137 µs | 155 µs |
| Edge | 223 µs | 248 µs | 140 µs | 153 µs |
| Firefox | 227 µs | 229 µs | 128 µs | 140 µs |
| Waterfox | 219 µs | 228 µs | 143 µs | 151 µs |

That's about 2.2% of one core for the regular model and 1.4% for the little one, 40 to 75 times faster than real time. SIMD gains 5 to 10%: RNNoise's generic C hardly vectorizes, and it warns as much. Kernels of its own in WebAssembly SIMD could come later if CPU ever matters; at 2% it doesn't. The delay is 1,440 samples, 30 ms, in every browser and both models: RNNoise's own 20 ms, and 10 ms of gathering frames.

**What it takes out.** The same in every browser, sample for sample:

| | regular | little |
| --- | --- | --- |
| Fan noise (stationary, pink) | 49.8 dB down | 38.4 dB down |
| Keyboard clicks | 20.0 dB down | 24.2 dB down |
| Another voice, as from a TV | 0.3 dB down | 0.3 dB down |
| Clean speech, the error below the signal | 12.8 dB | 12.7 dB |

RNNoise keeps any voice: it tells speech from noise, not one speaker from another. The clean-speech figure counts every change to the waveform, gain included, so it says less than listening does; `out/spikes/rnnoise/listen/` holds 20 seconds of speech with keyboard clicks, a fan and another voice, before and after each model.

**Calls.** A microphone runs through it in a 48 kHz `AudioContext` in all four browsers, at 100 frames a second, into a `MediaStreamDestination` whose track a call would send. Chrome and Edge report echo cancellation on, noise suppression off and gain control on, at 48 kHz, as asked. Firefox's and Waterfox's fake microphone reports echo cancellation and gain control off, and no sample rate; with a real microphone they're to check by hand, beside whether echo cancellation still works with the browser's suppressor off, which needs speakers and a real room.
