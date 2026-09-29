import assert from 'node:assert/strict';
import { test } from 'node:test';
import { clampOffset, coverScale, sourceRect, zoomAt } from '../assets/js/src/crop.jsx';
import { downloadURL, fitBox, formatSize } from '../assets/js/src/files.jsx';

test('sizes read as people write them', () => {
    assert.equal(formatSize(0), '0 B');
    assert.equal(formatSize(1023), '1023 B');
    assert.equal(formatSize(1024), '1 KB');
    assert.equal(formatSize(1536), '1.5 KB');
    assert.equal(formatSize(25 * 1024 * 1024), '25 MB');
    assert.equal(formatSize(2 * 1024 ** 3), '2 GB');
    assert.equal(formatSize(20 * 1024 ** 4), '20480 GB');
});

test('a preview takes its final size from the stated one, never enlarged', () => {
    assert.deepEqual(fitBox(1600, 1200, 400, 300), { width: 400, height: 300 });
    assert.deepEqual(fitBox(1200, 1600, 400, 300), { width: 225, height: 300 });
    assert.deepEqual(fitBox(200, 100, 400, 300), { width: 200, height: 100 });
    assert.deepEqual(fitBox(8000, 1000, 400, 300), { width: 400, height: 50 });
    // A sliver keeps a size that can be clicked; the preview is cropped.
    assert.deepEqual(fitBox(5000, 10, 400, 300), { width: 400, height: 48 });
    assert.deepEqual(fitBox(0, 0, 400, 300), { width: 400, height: 300 });
});

test("a download link carries the file's name as data", () => {
    assert.equal(downloadURL('abc', { id: '7', name: 'a b&c=d/e?.pdf' }), '/api/dens/abc/files/7?download=a%20b%26c%3Dd%2Fe%3F.pdf');
});

test('the crop frame stays covered', () => {
    // A 1000x500 image in a 256 square frame just covers it at 0.512.
    const s = coverScale(1000, 500, 256, 256);
    assert.equal(s, 0.512);
    assert.deepEqual(clampOffset({ x: 10, y: 10 }, 1000, 500, 256, 256, s), { x: 0, y: 0 });
    assert.deepEqual(clampOffset({ x: -1000, y: -5 }, 1000, 500, 256, 256, s), { x: 256 - 512, y: 0 });
});

test('zooming keeps the point under the pointer where it is', () => {
    const o = { x: -100, y: -40 };
    const next = zoomAt(o, 1, 2, 128, 128);
    // The image point under (128, 128) was (228, 168) at scale 1.
    assert.deepEqual(next, { x: 128 - 228 * 2, y: 128 - 168 * 2 });
    const r = sourceRect(next, 256, 256, 2);
    assert.equal(r.x + r.w / 2, 228);
    assert.equal(r.y + r.h / 2, 168);
});
