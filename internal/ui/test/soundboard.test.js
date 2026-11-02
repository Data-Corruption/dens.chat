// The den's soundboard (M14.3): the Ogg file the page writes a sound in,
// whose file for a fixed set of packets the Go tests read too, loudness
// evened, and the soundboard's search, waits and refusals.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import cases from '../../media/testdata/ogg.json';
import {
    CEILING, RATE, TARGET, boardWhy, even, evenGain, fold, loudness, matching, nameOf, oggOpus, opusSamples, peakOf, preSkipOf, seconds, soundRefusal, tooLong,
} from '../assets/js/src/soundboard.js';

const bytes = (b64) => new Uint8Array(Buffer.from(b64, 'base64'));

test("the page writes a sound's packets in Ogg as the Go tests read them", () => {
    const packets = cases.packets.map(bytes);
    const got = oggOpus({ channels: cases.channels, preSkip: cases.pre_skip, samples: cases.samples, packets });
    assert.deepEqual(Buffer.from(got).toString('base64'), cases.ogg);
});

test("a packet's samples come from its table of contents", () => {
    assert.equal(opusSamples(new Uint8Array([0xfc, 1])), 960, 'CELT, 20 ms');
    assert.equal(opusSamples(new Uint8Array([0xf4])), 480, 'CELT, 10 ms');
    assert.equal(opusSamples(new Uint8Array([0xff, 0x03])), 2880, 'three frames of 20 ms');
    assert.equal(opusSamples(new Uint8Array([0x18])), 2880, 'SILK, 60 ms');
    assert.equal(opusSamples(new Uint8Array([0x1b, 0x03])), 0, 'more than 120 ms');
    assert.equal(opusSamples(new Uint8Array([0xff])), 0, 'a count that is not there');
    assert.equal(opusSamples(new Uint8Array([])), 0);
});

// sine is a sine of hz at a peak of amplitude, seconds long, at RATE.
function sine(hz, amplitude, secs = 2) {
    const c = new Float32Array(Math.round(RATE * secs));
    for (let i = 0; i < c.length; i++) c[i] = amplitude * Math.sin((2 * Math.PI * hz * i) / RATE);
    return c;
}

test('loudness is measured as BS.1770 measures it', () => {
    // A 997 Hz sine at full scale in one channel reads -3.01 LKFS.
    assert.ok(Math.abs(loudness([sine(997, 1)]) - -3.01) < 0.1, String(loudness([sine(997, 1)])));
    // Two channels of it read 3 dB louder, and 20 dB down reads 20 less.
    assert.ok(Math.abs(loudness([sine(997, 1), sine(997, 1)]) - 0.0) < 0.1);
    assert.ok(Math.abs(loudness([sine(997, 0.1)]) - -23.01) < 0.1);
    assert.equal(loudness([new Float32Array(RATE)]), -Infinity, 'silence');
    // A sound shorter than a block is measured whole.
    assert.ok(Number.isFinite(loudness([sine(997, 0.5, 0.2)])));
});

test('a quiet sound and a loud one are evened to one loudness', () => {
    for (const amplitude of [0.01, 0.05, 0.4]) {
        const channels = [sine(440, amplitude), sine(440, amplitude)];
        assert.ok(even(channels) > 0);
        assert.ok(Math.abs(loudness(channels) - TARGET) < 0.2, `${amplitude}: ${loudness(channels)}`);
    }
});

test("a sound evened keeps its peaks under full scale, and silence can't be", () => {
    // Quiet, but for a click at full scale.
    const c = sine(440, 0.001);
    c[RATE] = 1;
    const gain = even([c]);
    assert.ok(gain > 0 && gain <= 10 ** (CEILING / 20) + 1e-9, String(gain));
    assert.ok(peakOf([c]) <= 10 ** (CEILING / 20) + 1e-6);
    assert.ok(loudness([c]) < TARGET - 6, 'the click holds it under the loudness');
    assert.equal(even([new Float32Array(RATE)]), 0);
    assert.equal(evenGain(-Infinity, 0), 1);
});

test("a sound's channels fold into the call's one or two, a film's centre into both", () => {
    // Each channel a constant, by its place, so each side's sum names its sources.
    const of = (...values) => values.map((v) => new Float32Array(4).fill(v));
    const sides = (channels) => fold(channels).map((c) => Number(c[0].toFixed(4)));
    const h = Math.SQRT1_2;
    assert.deepEqual(sides(of(0.5)), [0.5], 'mono stays mono');
    assert.deepEqual(sides(of(0.5, -0.25)), [0.5, -0.25], 'stereo stays');
    // 5.1: L, R, C, LFE, SL, SR.
    assert.deepEqual(sides(of(1, 2, 4, 8, 16, 32)), [1 + h * (4 + 16), 2 + h * (4 + 32)].map((v) => Number(v.toFixed(4))));
    // 7.1's back pair joins its sides too.
    assert.deepEqual(sides(of(1, 2, 4, 8, 16, 32, 64, 128)), [1 + h * (4 + 16 + 64), 2 + h * (4 + 32 + 128)].map((v) => Number(v.toFixed(4))));
    // Any other count, whose layout a file doesn't name, puts its extra channels into both.
    assert.deepEqual(sides(of(1, 2, 4)), [1 + h * 4, 2 + h * 4].map((v) => Number(v.toFixed(4))));
    assert.deepEqual(sides(of(1, 2, 4, 8)), [1 + h * 12, 2 + h * 12].map((v) => Number(v.toFixed(4))));
    // Evening changes what fold gives, so the decoder's own channels are left as they were.
    for (const decoded of [of(1), of(1, 2), of(1, 2, 4, 8, 16, 32)]) {
        fold(decoded)[0][0] = 99;
        assert.equal(decoded[0][0], 1);
    }
});

test('a length reads in tenths of a second under a minute, and in minutes past it', () => {
    assert.equal(seconds(2400), '2.4 s');
    assert.equal(seconds(10000), '10 s');
    assert.equal(seconds(10020), '10.1 s');
    assert.equal(seconds(125000), '2:05');
    assert.equal(tooLong(10000, 10), '');
    assert.match(tooLong(12300, 10), /12\.3 s.*10 s/);
    assert.match(tooLong(130000, 120), /2:10.*2:00/);
});

test("a sound's name is suggested from its file's", () => {
    assert.equal(nameOf('air_horn.mp3'), 'air horn');
    assert.equal(nameOf('  Big   bell.final.wav '), 'Big bell.final');
    assert.equal(nameOf('x'.repeat(40) + '.ogg'), 'x'.repeat(32));
    assert.equal(nameOf(''), '');
});

test("the soundboard's search finds names in any case, in order", () => {
    const sounds = [{ id: '3', name: 'bell' }, { id: '1', name: 'Air horn' }, { id: '2', name: 'Big Horn' }];
    assert.deepEqual(matching(sounds, '').map((s) => s.id), ['1', '3', '2']);
    assert.deepEqual(matching(sounds, ' HORN ').map((s) => s.id), ['1', '2']);
    assert.deepEqual(matching(sounds, 'drum'), []);
    assert.deepEqual(matching(undefined, ''), []);
});

test("the soundboard's tiles wait while the member is deafened, a sound plays, or their wait lasts", () => {
    assert.equal(boardWhy({ sounding: '', waitUntil: 0 }, 1000), '');
    assert.equal(boardWhy({ sounding: '', waitUntil: 0, deafened: true }, 1000), 'Undeafen to play sounds.');
    assert.equal(boardWhy({ sounding: '7', waitUntil: 4200, deafened: true }, 1000), 'Undeafen to play sounds.');
    assert.equal(boardWhy({ sounding: '7', waitUntil: 0 }, 1000), 'A sound is playing.');
    assert.equal(boardWhy({ sounding: '', waitUntil: 4200 }, 1000), 'Wait 4 s before your next sound.');
    assert.equal(boardWhy({ sounding: '', waitUntil: 1000 }, 1000), '');
});

test("each of the den's refusals of a sound says why", () => {
    for (const reason of ['playing', 'wait', 'off', 'staff_muted', 'not_found', 'rate_limited']) {
        assert.notEqual(soundRefusal(reason, { wait: 5 }), "The den couldn't play that sound.", reason);
    }
    assert.match(soundRefusal('wait', { wait: 5 }), /5 s/);
    assert.equal(soundRefusal('something new'), "The den couldn't play that sound.");
});

test("an encoder's Opus head gives its pre-skip, and without one it's libopus's", () => {
    const head = new Uint8Array(19);
    head.set(new TextEncoder().encode('OpusHead'), 0);
    head[10] = 0x38;
    head[11] = 0x01;
    assert.equal(preSkipOf(head), 312);
    head[10] = 0x78;
    head[11] = 0x00;
    assert.equal(preSkipOf(head.buffer), 120);
    assert.equal(preSkipOf(new DataView(head.buffer, 0, 19)), 120);
    assert.equal(preSkipOf(null), 312);
    assert.equal(preSkipOf(new Uint8Array(4)), 312);
});
