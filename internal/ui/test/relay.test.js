// Relay's player (M20.2), run in Node: the worklet's buffer as plain
// code, a block at a time as an output asks for them, its target
// following the output's stride, its steering, a gap the path left faded
// across, a run dry counted; the worker cutting a message into packets;
// and the member's buffer, and why a listening ended.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import './worklet-globals.js';
import { FADE, HEADER, MOST_STEER, Player } from '../assets/js/relay.worklet.js';
import { split } from '../assets/js/relay.worker.js';
import { BUFFER_HIGH, BUFFER_LOW, bufferChoice, endedText, setBufferChoice } from '../assets/js/src/relay.js';

const RATE = 48000;
const FRAMES = 240;

// packet is a packet as the service sends one: its place, its rate, and
// frames that all hold value.
function packet(place, value = 0.5, rate = RATE, frames = FRAMES) {
    const buf = new ArrayBuffer(HEADER + frames * 4);
    const view = new DataView(buf);
    view.setUint32(0, place >>> 0, true);
    view.setUint32(4, rate, true);
    const pcm = new Int16Array(buf, HEADER);
    pcm.fill(Math.round(value * 32767));
    return buf;
}

// output plays blocks of 128 every stride milliseconds, feeding a packet
// whenever the source's clock, clock times the output's, says one is due,
// and returns what it played.
function output(player, { ms, stride = 128 / 48, clock = 1, feed = true, from = 0, lose = () => false }) {
    const played = [];
    let place = from;
    let wall = 0;
    let due = 0;
    const blocks = Math.round(ms / stride);
    for (let b = 0; b < blocks; b++) {
        wall += stride;
        // A block of 128 at the output's rate is this much of the source's.
        due += 128 * clock;
        while (feed && due >= FRAMES) {
            if (!lose(place)) player.packet(packet(place));
            place += FRAMES;
            due -= FRAMES;
        }
        const l = new Float32Array(128);
        const r = new Float32Array(128);
        player.render(l, r, wall);
        played.push(...l);
    }
    return { played, place };
}

test('the player keeps its buffer, starting once it holds it, and plays what came', () => {
    const p = new Player({ buffer: 40, rate: RATE });
    for (let i = 0; i < 7; i++) p.packet(packet(i * FRAMES));
    const l = new Float32Array(128);
    const r = new Float32Array(128);
    p.render(l, r, 0);
    assert.ok(l.every((x) => x === 0), 'nothing plays under its buffer of 40 ms');
    for (let i = 7; i < 12; i++) p.packet(packet(i * FRAMES));
    p.render(l, r, 3);
    assert.ok(p.playing);
    assert.ok(l[0] > 0 && l[0] < 0.1, 'it fades in');
    p.render(l, r, 6);
    assert.ok(Math.abs(l[64] - 0.5) < 0.01, `then plays the packets as they came: ${l[64]}`);
    const report = p.report();
    assert.equal(report.packets, 12);
    assert.ok(report.level < -5 && report.level > -8, `at their level: ${report.level} dB`);
});

test('its target follows the output: one and a half of its longest stride, and 10 ms', () => {
    const p = new Player({ buffer: 40, rate: RATE });
    output(p, { ms: 2000, stride: 10 });
    p.report();
    assert.equal(p.target, 40, 'an output asking every 10 ms leaves it at 40');
    output(p, { ms: 2000, stride: 100, from: 96000 });
    p.report();
    assert.equal(p.target, 160, 'one asking every 100 ms moves it to 160');
    const high = new Player({ buffer: 150, rate: RATE });
    output(high, { ms: 2000, stride: 10 });
    high.report();
    assert.equal(high.target, 150, 'never under the member\'s choice');
});

test('it plays a hair fast or slow to keep its buffer, never more than 0.5%', () => {
    // A source whose clock runs 0.3% fast against this output's: once
    // settled, the player plays that much faster, near its buffer.
    const fast = new Player({ buffer: 40, rate: RATE });
    const settled = output(fast, { ms: 20000, clock: 1.003 });
    fast.report();
    output(fast, { ms: 5000, clock: 1.003, from: settled.place });
    const r = fast.report();
    assert.ok(r.steer > 2000 && r.steer <= MOST_STEER * 1e6, `it plays faster, by ${r.steer} millionths`);
    assert.ok(r.depth > 40 && r.depth < 70, `and keeps near its buffer: ${r.depth} ms`);
    // A source 1% fast is more than it steers for: it skips the pile.
    const faster = new Player({ buffer: 40, rate: RATE });
    output(faster, { ms: 60000, clock: 1.01 });
    const f = faster.report();
    assert.ok(f.skips > 0 && f.steer <= MOST_STEER * 1e6, `${f.skips} skips, steering ${f.steer}`);
    const slow = new Player({ buffer: 40, rate: RATE });
    const before = output(slow, { ms: 20000, clock: 0.998 });
    slow.report();
    output(slow, { ms: 5000, clock: 0.998, from: before.place });
    const s = slow.report();
    assert.ok(s.steer < -1500, `and slower for a slow source, by ${s.steer} millionths`);
    assert.equal(s.dry + r.dry, 0, 'without running dry');
});

test('a packet lost on the way is counted, and faded across: out before the gap, in after it', () => {
    const p = new Player({ buffer: 40, rate: RATE });
    const lost = 20 * FRAMES;
    const { played } = output(p, { ms: 400, lose: (place) => place === lost });
    const r = p.report();
    assert.equal(r.lost, 1);
    assert.equal(r.dry, 0, 'the gap is silence, not a buffer run dry');
    // The gap plays where its place was: silent at its middle, and the
    // level falls to it and rises after it without a jump.
    let jump = 0;
    for (let i = 1; i < played.length; i++) jump = Math.max(jump, Math.abs(played[i] - played[i - 1]));
    assert.ok(jump < 0.5 / FADE * 2, `the steepest step is ${jump}, a fade's`);
    assert.ok(played.some((x) => x === 0) && played.at(-1) > 0.4, 'silent across the gap, and playing after');
});

test('a buffer that runs dry is counted, fades out what it had, and starts again once it holds its buffer', () => {
    const p = new Player({ buffer: 40, rate: RATE });
    const first = output(p, { ms: 300 });
    const { played } = output(p, { ms: 100, feed: false });
    let r = p.report();
    assert.equal(r.dry, 1);
    assert.ok(!r.playing);
    let jump = 0;
    for (let i = 1; i < played.length; i++) jump = Math.max(jump, Math.abs(played[i] - played[i - 1]));
    assert.ok(jump < 0.05, `it fades out, its steepest step ${jump}`);
    output(p, { ms: 300, from: first.place });
    r = p.report();
    assert.ok(r.playing && r.dry === 1, 'and plays again');
});

test('a packet older than the last is dropped, and a long gap or jump back starts the stream again', () => {
    const p = new Player({ buffer: 40, rate: RATE });
    for (let i = 0; i < 10; i++) p.packet(packet(i * FRAMES));
    p.packet(packet(3 * FRAMES));
    assert.equal(p.counts.stale, 1);
    p.packet(packet(RATE * 10));
    assert.equal(p.counts.lost, 0, 'ten seconds on is no loss to fill');
    assert.equal(p.written - p.read, FRAMES, 'it starts again from the packet');
    // A link made again starts its places over: it plays from there.
    p.packet(packet(0));
    p.packet(packet(FRAMES));
    assert.equal(p.counts.stale, 1, 'not taken for packets late');
    assert.equal(p.written - p.read, 2 * FRAMES, 'it starts again from the first');
});

test('the worker cuts a message into its packets, each under its length', () => {
    const a = new Uint8Array([1, 2, 3]);
    const b = new Uint8Array([4, 5, 6, 7, 8]);
    const msg = new Uint8Array(4 + a.length + 4 + b.length);
    const view = new DataView(msg.buffer);
    view.setUint32(0, a.length, true);
    msg.set(a, 4);
    view.setUint32(4 + a.length, b.length, true);
    msg.set(b, 8 + a.length);
    const packets = split(msg.buffer);
    assert.deepEqual(packets.map((p) => [...new Uint8Array(p)]), [[1, 2, 3], [4, 5, 6, 7, 8]]);
    assert.deepEqual(split(msg.buffer.slice(0, 9)).length, 1, 'one cut short is left out');
});

test("the member's buffer, Low unless chosen, or High on a phone", () => {
    const store = new Map();
    globalThis.localStorage = { getItem: (k) => store.get(k) ?? null, setItem: (k, v) => store.set(k, v) };
    try {
        assert.equal(bufferChoice(false), BUFFER_LOW);
        assert.equal(bufferChoice(true), BUFFER_HIGH);
        setBufferChoice(BUFFER_LOW);
        assert.equal(bufferChoice(true), BUFFER_LOW, 'chosen, it stays');
    } finally {
        delete globalThis.localStorage;
    }
});

test('why a listening ended, in a line', () => {
    assert.equal(endedText('taken', 'Desk'), 'Another of your devices took over listening to Desk.');
    assert.equal(endedText('stopped', 'Desk'), 'Desk stopped sharing.');
    assert.equal(endedText('apart', 'Desk'), "Desk isn't on this network.");
    assert.equal(endedText('nonsense', 'Desk'), 'Lost touch with Dens on this device.');
});
