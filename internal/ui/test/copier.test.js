import assert from 'node:assert/strict';
import { test } from 'node:test';
import { canCopy, cropRect, decoderConfig, encoderConfig, keeper, readRecords, supported, writeRecord } from '../assets/js/src/copier.js';

const codecs = { VideoEncoder: function VideoEncoder() {}, VideoDecoder: function VideoDecoder() {} };
const brands = (...names) => ({ userAgentData: { brands: names.map((brand) => ({ brand, version: '154' })) } });

test('Chrome and Edge make copies themselves, and Firefox leaves them to the module', () => {
    assert.equal(canCopy(brands('Google Chrome', 'Chromium', 'Not?A_Brand'), codecs), true);
    assert.equal(canCopy(brands('Microsoft Edge', 'Chromium'), codecs), true);
    // Firefox has no userAgentData, and Chromium without WebCodecs can't.
    assert.equal(canCopy({}, codecs), false);
    assert.equal(canCopy(brands('Chromium'), {}), false);
});

test("records carry a copy's packets both ways", () => {
    const a = writeRecord(true, 0, 33333, 3, (dst) => dst.set([1, 2, 3]));
    const b = writeRecord(false, 2_001_667, 33333.4, 2, (dst) => dst.set([9, 8]));
    const both = new Uint8Array(a.length + b.length);
    both.set(a);
    both.set(b, a.length);
    const read = readRecords(both.buffer);
    assert.deepEqual(read.map((r) => [r.key, r.pts, r.duration, [...r.data]]), [[true, 0, 33333, [1, 2, 3]], [false, 2_001_667, 33333, [9, 8]]]);
    assert.throws(() => readRecords(both.buffer.slice(0, both.length - 1)), /cut short/);
});

test('a copy keeps at most its frames a second, from where the video starts', () => {
    // A phone's 60 frames a second, from a frame before the edit's start.
    const keep = keeper(30, 0);
    const kept = [];
    for (let i = -1; i < 12; i++) {
        const ts = Math.round(i * 1e6 / 59.94);
        if (keep(ts)) kept.push(i);
    }
    assert.deepEqual(kept, [0, 2, 4, 6, 8, 10]);
    // 24 frames a second keep every one, and jitter of less than a quarter
    // of a slot doesn't drop one.
    const film = keeper(30, 0);
    assert.deepEqual([0, 41667, 83333, 125000].map(film), [true, true, true, true]);
    const jitter = keeper(30, 0);
    assert.deepEqual([0, 26000, 60000].map(jitter), [true, true, true]);
});

test("the copy shows what the container crops the stream's frames to", () => {
    const visible = { x: 0, y: 0, width: 1440, height: 1080 };
    assert.deepEqual(cropRect(visible, [50, 50, 66, 66]), { x: 66, y: 50, width: 1308, height: 980 });
    assert.equal(cropRect(visible, [0, 0, 0, 0]), null);
    assert.equal(cropRect(visible, undefined), null);
    assert.equal(cropRect(visible, [600, 600, 0, 0]), null);
});

const offer = {
    id: 1,
    decoding: { codec: 'avc1.64001f', description: btoa(String.fromCharCode(1, 0x64, 0, 0x1f)), coded_width: 1440, coded_height: 1080, crop: [0, 0, 0, 0] },
    encoding: { codec: 'av01.0.08M.08', width: 1280, height: 960, fps: 30, bitrate: 1_474_000, keyframe_us: 5_000_000, start_us: 0 },
};

test("WebCodecs' configurations come from the offer", () => {
    const d = decoderConfig(offer.decoding);
    assert.equal(d.codec, 'avc1.64001f');
    assert.deepEqual([...d.description], [1, 0x64, 0, 0x1f]);
    assert.equal(d.codedWidth, 1440);
    assert.equal('description' in decoderConfig({ codec: 'vp8', coded_width: 2, coded_height: 2 }), false);
    assert.deepEqual(encoderConfig(offer.encoding), {
        codec: 'av01.0.08M.08', width: 1280, height: 960, bitrate: 1_474_000, framerate: 30, bitrateMode: 'variable', latencyMode: 'quality',
        hardwareAcceleration: 'no-preference',
    });
});

test("a browser that can't decode the video or encode the copy says why", async () => {
    const scope = (decodes, encodes) => ({
        VideoDecoder: { isConfigSupported: async () => ({ supported: decodes }) },
        VideoEncoder: { isConfigSupported: async () => { if (encodes === 'throws') throw new TypeError('bad'); return { supported: encodes }; } },
    });
    assert.equal(await supported(offer, scope(true, true)), '');
    assert.equal(await supported(offer, scope(false, true)), "it can't decode avc1.64001f");
    assert.equal(await supported(offer, scope(true, false)), "it can't encode av01.0.08M.08");
    assert.equal(await supported(offer, scope(true, 'throws')), "it can't encode av01.0.08M.08");
});
