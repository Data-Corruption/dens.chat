// The RNNoise spike's page. It runs under Dens's own CSP, with
// 'wasm-unsafe-eval' added, and posts what it measures to /results:
//
// - whether the page can compile the module, and how the worklet gets it;
// - for each build, how long a minute of audio takes to clean, beside the
//   same path without RNNoise, so per-frame CPU;
// - the delay RNNoise adds, by lining its output up with the input;
// - how much fan noise, keyboard clicks and other voices it takes out, and
//   how much of clean speech it keeps;
// - a live microphone through it, with the browser's echo cancellation on
//   and its own noise suppression off, as calls would run.

const RATE = 48000;
const FRAME = 480;
const VARIANTS = ['regular-simd', 'regular-scalar', 'little-simd', 'little-scalar'];
const params = new URLSearchParams(location.search);
const label = params.get('label') || navigator.userAgent;
const out = document.getElementById('out');

function log(line) {
    out.textContent += line + '\n';
}

// A small deterministic generator, so every browser hears the same noise.
function rng(seed) {
    let s = seed >>> 0;
    return () => {
        s = (s * 1664525 + 1013904223) >>> 0;
        return s / 4294967296;
    };
}

function fan(n, seed) {
    // Pink-ish noise: white noise through a few leaky integrators.
    const r = rng(seed);
    const x = new Float32Array(n);
    let a = 0, b = 0, c = 0;
    for (let i = 0; i < n; i++) {
        const w = r() * 2 - 1;
        a = 0.997 * a + 0.03 * w;
        b = 0.985 * b + 0.06 * w;
        c = 0.9 * c + 0.1 * w;
        x[i] = (a + b + c) * 0.12;
    }
    return x;
}

function keyboard(n, seed) {
    // Clicks every 80 to 280 ms: 6 ms of decaying noise each.
    const r = rng(seed);
    const x = new Float32Array(n);
    for (let at = 2000; at < n;) {
        const amp = 0.15 + r() * 0.25;
        for (let i = 0; i < 288 && at + i < n; i++) x[at + i] += (r() * 2 - 1) * amp * Math.exp(-i / 40);
        at += Math.floor(RATE * (0.08 + r() * 0.2));
    }
    return x;
}

function mix(...parts) {
    const n = Math.min(...parts.map(([x]) => x.length));
    const y = new Float32Array(n);
    for (const [x, gain] of parts) for (let i = 0; i < n; i++) y[i] += x[i] * gain;
    return y;
}

function rms(x, from = 0, to = x.length) {
    let s = 0;
    for (let i = from; i < to; i++) s += x[i] * x[i];
    return Math.sqrt(s / Math.max(1, to - from));
}

const db = (v) => 20 * Math.log10(Math.max(v, 1e-12));

async function loadWav(path) {
    const res = await fetch(path);
    if (!res.ok) throw new Error(`${path}: ${res.status}`);
    const ctx = new OfflineAudioContext(1, 1, RATE);
    const buffer = await ctx.decodeAudioData(await res.arrayBuffer());
    return buffer.getChannelData(0);
}

// render runs a signal through the worklet in an offline context, and
// times it.
async function render(signal, options) {
    const ctx = new OfflineAudioContext(1, signal.length, RATE);
    await ctx.audioWorklet.addModule('/worklet.js');
    const buffer = ctx.createBuffer(1, signal.length, RATE);
    buffer.copyToChannel(signal, 0);
    const source = new AudioBufferSourceNode(ctx, { buffer });
    const node = new AudioWorkletNode(ctx, 'rnnoise', { processorOptions: options });
    source.connect(node).connect(ctx.destination);
    source.start();
    const started = performance.now();
    const rendered = await ctx.startRendering();
    return { output: rendered.getChannelData(0), ms: performance.now() - started };
}

// align finds the lag at which the output best matches the input.
function align(input, output, maxLag = 4 * FRAME) {
    const from = RATE, to = Math.min(input.length - maxLag, RATE * 6);
    let best = 0, bestScore = -Infinity;
    for (let lag = 0; lag <= maxLag; lag++) {
        let s = 0;
        for (let i = from; i < to; i++) s += input[i] * output[i + lag];
        if (s > bestScore) {
            bestScore = s;
            best = lag;
        }
    }
    return best;
}

// kept is how much of a clean signal survives, as the error's level below
// the signal's, once lined up.
function kept(input, output, lag) {
    let sig = 0, err = 0;
    for (let i = RATE; i < input.length - lag; i++) {
        sig += input[i] * input[i];
        const d = output[i + lag] - input[i];
        err += d * d;
    }
    return 10 * Math.log10(sig / Math.max(err, 1e-12));
}

async function main() {
    const results = { label, userAgent: navigator.userAgent, steps: [] };
    const step = (name, data) => {
        results.steps.push({ name, ...data });
        log(`${name}: ${JSON.stringify(data)}`);
    };
    try {
        const modules = {};
        for (const v of VARIANTS) {
            const bytes = await (await fetch(`/rnnoise-${v}.wasm`)).arrayBuffer();
            try {
                modules[v] = { module: await WebAssembly.compile(bytes), bytes };
            } catch (e) {
                step('compile', { variant: v, error: String(e) });
                throw e;
            }
        }
        step('compile', { ok: true });

        // How the worklet gets the module: compiled here and posted, or as
        // bytes it compiles itself.
        let pass = 'module';
        try {
            await render(new Float32Array(RATE), { module: modules['regular-simd'].module });
        } catch (e) {
            step('post module', { error: String(e) });
            pass = 'bytes';
        }
        const opts = (v) => (pass === 'module' ? { module: modules[v].module } : { bytes: modules[v].bytes.slice(0) });
        step('pass', { pass });

        const speech = await loadWav('/speech.wav');
        const voices = await loadWav('/voices.wav');
        const n = Math.min(speech.length, voices.length, RATE * 60);
        const fanNoise = fan(n, 1);
        const keys = keyboard(n, 2);
        const signals = {
            speech: speech.subarray(0, n),
            fan: fanNoise,
            keyboard: keys,
            voices: mix([voices, 0.3]),
            'speech+keyboard': mix([speech, 1], [keys, 1]),
        };

        const base = await render(signals.speech, { bypass: true });
        step('bypass', { seconds: n / RATE, ms: Math.round(base.ms) });
        for (const v of VARIANTS) {
            const clean = await render(signals.speech, opts(v));
            const lag = align(signals.speech, clean.output);
            const frames = n / FRAME;
            const row = {
                variant: v,
                ms: Math.round(clean.ms),
                usPerFrame: Math.round(((clean.ms - base.ms) * 1000) / frames),
                realtime: +((n / RATE) * 1000 / clean.ms).toFixed(1),
                lagSamples: lag,
                speechKeptDb: +kept(signals.speech, clean.output, lag).toFixed(1),
            };
            for (const name of ['fan', 'keyboard', 'voices']) {
                const r = await render(signals[name], opts(v));
                row[`${name}CutDb`] = +(db(rms(signals[name], RATE)) - db(rms(r.output, RATE))).toFixed(1);
            }
            const both = await render(signals['speech+keyboard'], opts(v));
            row.speechOverKeyboardDb = {
                before: +kept(signals.speech, signals['speech+keyboard'], 0).toFixed(1),
                after: +kept(signals.speech, both.output, lag).toFixed(1),
            };
            step('bench', row);
        }

        // Recordings to listen to: 20 seconds of each mix, and what each
        // model makes of it.
        if (params.has('listen')) {
            const cut = (x) => x.subarray(0, RATE * 20);
            const mixes = {
                keyboard: mix([speech, 1], [keys, 1]),
                fan: mix([speech, 1], [fanNoise, 1]),
                voices: mix([speech, 1], [voices, 0.3]),
            };
            for (const [name, x] of Object.entries(mixes)) {
                await post(`${name}-before.wav`, cut(x));
                for (const v of ['regular-simd', 'little-simd']) {
                    await post(`${name}-${v.split('-')[0]}.wav`, cut((await render(cut(x), opts(v))).output));
                }
            }
            step('listen', { saved: Object.keys(mixes).length * 3 });
        }

        step('live', await live(opts('regular-simd')));
    } catch (e) {
        step('error', { error: String(e && e.stack ? e.stack : e) });
    }
    await fetch('/results', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(results) });
    log('done');
}

// post sends a recording to the server as a 16-bit WAV.
async function post(name, x) {
    const data = new DataView(new ArrayBuffer(44 + x.length * 2));
    const text = (at, s) => [...s].forEach((c, i) => data.setUint8(at + i, c.charCodeAt(0)));
    text(0, 'RIFF');
    data.setUint32(4, 36 + x.length * 2, true);
    text(8, 'WAVEfmt ');
    data.setUint32(16, 16, true);
    data.setUint16(20, 1, true);
    data.setUint16(22, 1, true);
    data.setUint32(24, RATE, true);
    data.setUint32(28, RATE * 2, true);
    data.setUint16(32, 2, true);
    data.setUint16(34, 16, true);
    text(36, 'data');
    data.setUint32(40, x.length * 2, true);
    for (let i = 0; i < x.length; i++) data.setInt16(44 + i * 2, Math.max(-1, Math.min(1, x[i])) * 32767, true);
    await fetch(`/audio?name=${encodeURIComponent(name)}`, { method: 'POST', body: data.buffer });
}

// live runs a microphone through RNNoise for two seconds, as a call would.
async function live(options) {
    const mic = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: false, autoGainControl: true },
    });
    const ctx = new AudioContext({ sampleRate: RATE });
    try {
        await ctx.audioWorklet.addModule('/worklet.js');
        const source = ctx.createMediaStreamSource(mic);
        const node = new AudioWorkletNode(ctx, 'rnnoise', { processorOptions: options });
        const dest = ctx.createMediaStreamDestination();
        const meter = ctx.createAnalyser();
        source.connect(node).connect(dest);
        node.connect(meter);
        await ctx.resume();
        await new Promise((r) => setTimeout(r, 2000));
        const stats = await new Promise((resolve) => {
            node.port.onmessage = (e) => resolve(e.data);
            node.port.postMessage('stats');
        });
        const level = new Float32Array(meter.fftSize);
        meter.getFloatTimeDomainData(level);
        const settings = mic.getAudioTracks()[0].getSettings();
        return {
            contextRate: ctx.sampleRate,
            state: ctx.state,
            frames: stats.frames,
            outputTrack: dest.stream.getAudioTracks().length,
            outputDb: +db(rms(level)).toFixed(1),
            echoCancellation: settings.echoCancellation,
            noiseSuppression: settings.noiseSuppression,
            autoGainControl: settings.autoGainControl,
            micRate: settings.sampleRate,
        };
    } finally {
        mic.getTracks().forEach((t) => t.stop());
        await ctx.close();
    }
}

main();
