// The WebView's features, as Dens's page would use them: what's there
// without a tap, and then what a tap allows. Each result goes to the probe,
// which keeps them in features.json for the report.
'use strict';

const results = {};
const shown = document.getElementById('results');

function note(key, value) {
    results[key] = value;
    shown.textContent += `${key}: ${JSON.stringify(value)}\n`;
}

async function attempt(key, fn) {
    try {
        note(key, await fn());
    } catch (e) {
        note(key, `error: ${e && e.name ? e.name + ': ' : ''}${e && e.message ? e.message : e}`);
    }
}

async function send() {
    await fetch('/result', { method: 'POST', body: JSON.stringify(results), headers: { 'Content-Type': 'application/json' } });
}

const has = (v) => typeof v !== 'undefined';
const withTimeout = (p, ms) => Promise.race([p, new Promise((_, reject) => setTimeout(() => reject(new Error(`no answer in ${ms} ms`)), ms))]);

// The codecs Dens decodes from phones and encodes its copies with: the
// page's copy is AV1 (media.PageAV1).
const DECODE = ['avc1.64002a', 'hvc1.1.6.L153.B0', 'hev1.1.6.L153.B0', 'hvc1.2.4.L153.B0', 'av01.0.08M.08', 'vp09.00.40.08'];
const ENCODE = ['av01.0.08M.08', 'avc1.42001f', 'vp09.00.10.08', 'hvc1.1.6.L93.B0'];
const ACCEL = ['prefer-hardware', 'prefer-software'];

async function webcodecs() {
    note('VideoDecoder', has(window.VideoDecoder));
    note('VideoEncoder', has(window.VideoEncoder));
    note('AudioEncoder', has(window.AudioEncoder));
    note('ImageDecoder', has(window.ImageDecoder));
    if (!window.VideoDecoder) return;
    for (const codec of DECODE) {
        const row = {};
        for (const hw of ACCEL) {
            try {
                row[hw] = (await VideoDecoder.isConfigSupported({ codec, codedWidth: 1920, codedHeight: 1080, hardwareAcceleration: hw })).supported;
            } catch (e) {
                row[hw] = `error: ${e.message}`;
            }
        }
        note(`decode ${codec}`, row);
    }
    for (const codec of ENCODE) {
        const row = {};
        for (const hw of ACCEL) {
            for (const [width, height, bitrate] of [[1280, 720, 1.2e6], [1920, 1080, 4e6]]) {
                try {
                    row[`${hw} ${height}p`] = (await VideoEncoder.isConfigSupported({
                        codec, width, height, bitrate, framerate: 30, bitrateMode: 'variable', latencyMode: 'quality', hardwareAcceleration: hw,
                    })).supported;
                } catch (e) {
                    row[`${hw} ${height}p`] = `error: ${e.message}`;
                }
            }
        }
        note(`encode ${codec}`, row);
    }
    for (const hw of ACCEL) await attempt(`AV1 encode speed ${hw}`, () => encodeSpeed('av01.0.08M.08', hw));
}

// encodeSpeed encodes 90 frames of 720p that change every frame, as a
// video's would, and says how fast and how large.
async function encodeSpeed(codec, hw) {
    const config = { codec, width: 1280, height: 720, bitrate: 1.2e6, framerate: 30, bitrateMode: 'variable', latencyMode: 'quality', hardwareAcceleration: hw };
    if (!(await VideoEncoder.isConfigSupported(config)).supported) return 'unsupported';
    let bytes = 0;
    let chunks = 0;
    let failure = null;
    const encoder = new VideoEncoder({ output: (c) => { bytes += c.byteLength; chunks++; }, error: (e) => { failure = e; } });
    encoder.configure(config);
    const canvas = new OffscreenCanvas(1280, 720);
    const g = canvas.getContext('2d');
    const start = performance.now();
    for (let i = 0; i < 90; i++) {
        g.fillStyle = `hsl(${i * 4}, 70%, 50%)`;
        g.fillRect(0, 0, 1280, 720);
        for (let j = 0; j < 40; j++) {
            g.fillStyle = `hsl(${(i * 7 + j * 31) % 360}, 80%, ${30 + (j % 5) * 10}%)`;
            g.fillRect((i * 13 + j * 97) % 1280, (i * 5 + j * 53) % 720, 120, 90);
        }
        const frame = new VideoFrame(canvas, { timestamp: i * 33333 });
        encoder.encode(frame, { keyFrame: i === 0 });
        frame.close();
        while (encoder.encodeQueueSize > 8) await new Promise((r) => setTimeout(r, 1));
    }
    await encoder.flush();
    encoder.close();
    if (failure) throw failure;
    const secs = (performance.now() - start) / 1000;
    return `${(90 / secs).toFixed(1)} fps, ${chunks} chunks, ${Math.round(bytes * 8 / 3 / 1000)} kbps`;
}

async function voiceWorklet(source) {
    const ctx = new AudioContext({ sampleRate: 48000 });
    const out = { sampleRate: ctx.sampleRate, state: ctx.state, baseLatency: ctx.baseLatency, outputLatency: ctx.outputLatency };
    try {
        const start = performance.now();
        const module = await WebAssembly.compile(await (await fetch('/rnnoise.wasm')).arrayBuffer());
        out.compileMs = Math.round(performance.now() - start);
        await ctx.audioWorklet.addModule('/voice.worklet.js');
        const node = new AudioWorkletNode(ctx, 'voice', { outputChannelCount: [1], processorOptions: { module, gate: { mode: 'auto' } } });
        const reports = [];
        node.port.onmessage = (e) => reports.push(e.data);
        let input;
        if (source) {
            input = ctx.createMediaStreamSource(source);
        } else {
            input = ctx.createOscillator();
            input.start();
        }
        input.connect(node).connect(ctx.destination);
        await withTimeout(ctx.resume(), 5000).catch(() => {});
        out.stateAfterResume = ctx.state;
        await new Promise((r) => setTimeout(r, 2000));
        out.reports = reports.length;
        out.lastReport = reports[reports.length - 1] || null;
    } finally {
        await ctx.close();
    }
    return out;
}

async function noTap() {
    note('userAgent', navigator.userAgent);
    note('secureContext', window.isSecureContext);
    note('viewport', { dpr: devicePixelRatio, screen: `${screen.width}x${screen.height}`, inner: `${innerWidth}x${innerHeight}`, touch: navigator.maxTouchPoints });
    const probe = getComputedStyle(document.getElementById('probe'));
    note('safe-area-inset', [probe.paddingTop, probe.paddingRight, probe.paddingBottom, probe.paddingLeft].join(' '));
    note('media queries', Object.fromEntries(['(hover: hover)', '(pointer: coarse)', '(prefers-color-scheme: dark)', '(prefers-reduced-motion: reduce)', '(display-mode: browser)']
        .map((q) => [q, matchMedia(q).matches])));
    note('cpus and memory', { hardwareConcurrency: navigator.hardwareConcurrency, deviceMemory: navigator.deviceMemory });
    note('RTCPeerConnection', has(window.RTCPeerConnection));
    await attempt('RTC offer', async () => {
        const pc = new RTCPeerConnection();
        pc.addTransceiver('audio');
        const offer = await pc.createOffer();
        pc.close();
        return { opus: /opus\/48000/i.test(offer.sdp), red: /red\/48000/i.test(offer.sdp) };
    });
    note('getUserMedia', has(navigator.mediaDevices?.getUserMedia));
    note('getDisplayMedia', has(navigator.mediaDevices?.getDisplayMedia));
    note('selectAudioOutput', has(navigator.mediaDevices?.selectAudioOutput));
    note('setSinkId on media elements', 'setSinkId' in HTMLMediaElement.prototype);
    note('setSinkId on AudioContext', has(window.AudioContext) && 'setSinkId' in AudioContext.prototype);
    note('AudioWorkletNode', has(window.AudioWorkletNode));
    await attempt('devices before permission', async () => countDevices(await navigator.mediaDevices.enumerateDevices()));
    note('Notification', has(window.Notification) ? Notification.permission : 'absent');
    note('window.open', has(window.open));
    note('fullscreenEnabled', document.fullscreenEnabled);
    note('clipboard', { writeText: has(navigator.clipboard?.writeText), readText: has(navigator.clipboard?.readText) });
    note('navigator.share', has(navigator.share));
    note('wakeLock', 'wakeLock' in navigator);
    note('setAppBadge', has(navigator.setAppBadge));
    note('vibrate', has(navigator.vibrate));
    note('serviceWorker', 'serviceWorker' in navigator);
    note('indexedDB', has(window.indexedDB));
    await attempt('storage', async () => {
        const e = await navigator.storage.estimate();
        return { quotaMB: Math.round(e.quota / 1048576), usageMB: Math.round(e.usage / 1048576), persisted: await navigator.storage.persisted() };
    });
    note('WebAssembly', has(window.WebAssembly));
    await attempt('voice worklet, oscillator, no tap', () => voiceWorklet(null));
    await webcodecs();
}

function countDevices(list) {
    const kinds = {};
    for (const d of list) kinds[d.kind] = (kinds[d.kind] || 0) + 1;
    return { kinds, labelled: list.filter((d) => d.label).length };
}

async function tapped() {
    document.getElementById('status').textContent = 'Running the tests that need a tap…';
    note('activation at the tap', navigator.userActivation ? navigator.userActivation.isActive : 'no userActivation');
    // A tap's activation lasts seconds, so these go first.
    await attempt('requestFullscreen', async () => {
        const el = document.getElementById('full');
        el.style.display = 'block';
        await withTimeout(el.requestFullscreen(), 3000);
        await new Promise((r) => setTimeout(r, 500));
        const ok = document.fullscreenElement === el;
        await document.exitFullscreen();
        el.style.display = 'none';
        return ok ? 'entered' : 'no fullscreen element';
    });
    await attempt('clipboard writeText', async () => { await navigator.clipboard.writeText('aprobe'); return 'ok'; });
    await attempt('clipboard readText', async () => (await navigator.clipboard.readText()).length + ' characters');
    await attempt('wakeLock request', async () => { const l = await navigator.wakeLock.request('screen'); await l.release(); return 'ok'; });
    await attempt('Notification.requestPermission', async () => (window.Notification ? await withTimeout(Notification.requestPermission(), 5000) : 'absent'));
    await attempt('getUserMedia audio', async () => {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: false, autoGainControl: true } });
        const track = stream.getAudioTracks()[0];
        const out = { label: track.label, settings: track.getSettings() };
        out.devices = countDevices(await navigator.mediaDevices.enumerateDevices());
        out.worklet = await voiceWorklet(stream).catch((e) => `error: ${e.message}`);
        stream.getTracks().forEach((t) => t.stop());
        return out;
    });
    await attempt('getDisplayMedia call', async () => {
        const s = await navigator.mediaDevices.getDisplayMedia({ video: true });
        s.getTracks().forEach((t) => t.stop());
        return 'ok';
    });
    // Last: in a WebView it tries to take the page's place.
    await attempt('window.open call', async () => {
        const w = window.open('about:blank', '_blank');
        if (!w) return 'returned null';
        w.close();
        return 'returned a window';
    });
    await send();
    document.getElementById('status').textContent = 'Done: the report has these.';
}

document.getElementById('tap').addEventListener('click', () => tapped().catch((e) => note('tap tests', `error: ${e.message}`)));
noTap().then(send).then(() => {
    document.getElementById('status').textContent = 'Tap the button for the rest.';
}).catch((e) => {
    note('no-tap tests', `error: ${e.message}`);
    send();
});
