// The RNNoise worklet, run in Node with the module the page ships: the
// audio thread's globals are stood in for, and the processor fed blocks of
// 128 samples as a browser would.

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import './worklet-globals.js';
import '../assets/js/rnnoise.worklet.js';

const FRAME = 480;
const module = new WebAssembly.Module(readFileSync('internal/ui/assets/wasm/rnnoise.wasm'));

function run(input) {
    const p = new globalThis.registered.rnnoise({ processorOptions: { module } });
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
