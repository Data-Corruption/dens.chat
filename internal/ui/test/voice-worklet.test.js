// The voice worklet, run in Node with the RNNoise module the page ships:
// the audio thread's globals are stood in for, and the processor fed
// blocks of 128 samples as a browser would.

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import './worklet-globals.js';
import { Gate, levelOf } from '../assets/js/voice.worklet.js';

const FRAME = 480;
const module = new WebAssembly.Module(readFileSync('internal/ui/assets/wasm/rnnoise.wasm'));

function run(input, options = { module }, reports = []) {
    const p = new globalThis.registered.voice({ processorOptions: options });
    p.port.postMessage = (m) => reports.push(m);
    const output = new Float32Array(input.length);
    for (let at = 0; at + 128 <= input.length; at += 128) {
        const out = new Float32Array(128);
        p.process([[input.subarray(at, at + 128)]], [[out]]);
        output.set(out, at);
    }
    return output;
}

function rms(x, from = 0, to = x.length) {
    let s = 0;
    for (let i = from; i < to; i++) s += x[i] * x[i];
    return Math.sqrt(s / (to - from));
}

test('the processor starts a frame behind and never runs dry', () => {
    // Silence in, silence out: nothing appears from nowhere, and every
    // block is filled.
    const out = run(new Float32Array(48000));
    assert.ok(rms(out) < 1e-4);
    // A loud noise burst shows up in the output only after the first
    // frame's lead.
    const input = new Float32Array(48000);
    let s = 1;
    for (let i = 0; i < input.length; i++) {
        s = (s * 1664525 + 1013904223) >>> 0;
        input[i] = (s / 4294967296 - 0.5) * 0.5;
    }
    const burst = run(input);
    assert.ok(rms(burst, 0, FRAME - 32) === 0, 'output before the first frame is silence');
});

test('RNNoise takes out steady noise', () => {
    const input = new Float32Array(48000 * 3);
    let s = 7;
    let a = 0;
    for (let i = 0; i < input.length; i++) {
        s = (s * 1664525 + 1013904223) >>> 0;
        a = 0.98 * a + 0.05 * (s / 4294967296 - 0.5);
        input[i] = a;
    }
    const out = run(input);
    const cut = 20 * Math.log10(rms(input, 48000) / Math.max(rms(out, 48000), 1e-9));
    assert.ok(cut > 20, `steady noise dropped only ${cut.toFixed(1)} dB`);
});

// tone is a 440 Hz sine at the given amplitude.
function tone(seconds, amplitude) {
    const x = new Float32Array(48000 * seconds);
    for (let i = 0; i < x.length; i++) x[i] = amplitude * Math.sin((2 * Math.PI * 440 * i) / 48000);
    return x;
}

test('the gate passes what RNNoise takes for speech, and holds 300 ms after', () => {
    const g = new Gate();
    g.set({ mode: 'auto' });
    assert.equal(g.decide(-20, 0.9), true);
    for (let i = 0; i < 29; i++) assert.equal(g.decide(-90, 0.1), true, `frame ${i} of the hold`);
    assert.equal(g.decide(-90, 0.1), false);
    assert.equal(g.decide(-10, 0.2), false, 'loud noise is still noise');
});

test('the gate by level, shut and open', () => {
    const g = new Gate();
    g.set({ mode: 'level', threshold: -40 });
    assert.equal(g.decide(-50, -1), false);
    assert.equal(g.decide(-40, -1), true);
    g.set({ mode: 'closed' });
    assert.equal(g.decide(0, 1), false);
    g.set({ mode: 'open' });
    assert.equal(g.decide(-120, -1), true);
    g.set({ mode: 'nonsense', threshold: 'loud' });
    assert.equal(g.mode, 'open');
    assert.equal(g.threshold, -40);
});

test('the gate fades in over 2.5 ms and out over 20', () => {
    const g = new Gate();
    const up = new Float32Array(480).fill(1);
    g.fade(up, true);
    assert.ok(up[0] > 0 && up[0] < 0.05);
    assert.equal(up[119], 1);
    const down = new Float32Array(960).fill(1);
    g.fade(down, false);
    assert.ok(down[479] > 0.45 && down[479] < 0.55);
    assert.equal(down[959], 0);
});

test('levels read in dBFS', () => {
    assert.ok(Math.abs(levelOf(new Float32Array(480).fill(0.5)) - -6.02) < 0.01);
    assert.equal(levelOf(new Float32Array(480)), -120);
});

test('without RNNoise the processor only gates, as the member set it', () => {
    const quiet = tone(1, 0.001);
    const loud = tone(1, 0.3);
    // Open: what comes in goes out, a frame behind.
    const open = run(loud, { gate: { mode: 'open' } });
    assert.ok(Math.abs(open[48000 - 1] - loud[48000 - 1 - 480]) < 1e-6);
    // By level: the quiet tone stays shut, the loud one opens.
    assert.ok(rms(run(quiet, { gate: { mode: 'level', threshold: -40 } })) === 0);
    assert.ok(rms(run(loud, { gate: { mode: 'level', threshold: -40 } }), 9600) > 0.2);
});

test('the page sets the gate by message, and hears its level back', () => {
    const p = new globalThis.registered.voice({ processorOptions: { gate: { mode: 'closed' } } });
    const reports = [];
    p.port.postMessage = (m) => reports.push(m);
    const feed = (x) => {
        const out = new Float32Array(x.length);
        for (let at = 0; at + 128 <= x.length; at += 128) {
            const block = new Float32Array(128);
            p.process([[x.subarray(at, at + 128)]], [[block]]);
            out.set(block, at);
        }
        return out;
    };
    const loud = tone(1, 0.3);
    assert.equal(rms(feed(loud)), 0);
    p.port.onmessage({ data: { gate: { mode: 'open' } } });
    assert.ok(rms(feed(loud), 9600) > 0.2);
    assert.ok(reports.length >= 19, `${reports.length} reports in 2 seconds`);
    // A sine of amplitude 0.3 has an RMS of 0.3 / √2: -13.5 dBFS.
    assert.ok(reports.every((r) => r.level > -14 && r.level < -13 && r.speech === -1));
    assert.equal(reports[0].open, false);
    assert.equal(reports.at(-1).open, true);
});

test('a microphone with two channels goes as their mix, in one', () => {
    const p = new globalThis.registered.voice({ processorOptions: { gate: { mode: 'open' } } });
    const left = tone(1, 0.4);
    const right = new Float32Array(left.length);
    const out = new Float32Array(left.length);
    for (let at = 0; at + 128 <= left.length; at += 128) {
        const block = new Float32Array(128);
        p.process([[left.subarray(at, at + 128), right.subarray(at, at + 128)]], [[block]]);
        out.set(block, at);
    }
    // Half the left channel's level, a frame behind: the mix, not the left alone.
    assert.ok(Math.abs(out[48000 - 1] - left[48000 - 1 - 480] / 2) < 1e-6);
});
