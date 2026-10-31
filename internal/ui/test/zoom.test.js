import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ZOOM, clampView, fitted, isFitted, maxScale, panned, pinched, toggled, wheeled, zoomAt } from '../assets/js/src/zoom.js';

const stage = { width: 390, height: 700 };
// A photo 4032 by 3024, fitted to the phone's width.
const box = { width: 390, height: 292.5 };
const near = (a, b) => Object.keys(b).every((k) => Math.abs(a[k] - b[k]) < 1e-9);

test('a fitted picture sits in the middle of its stage', () => {
    assert.deepEqual(fitted(box, stage), { scale: 1, x: 0, y: 203.75 });
    assert.equal(isFitted(fitted(box, stage)), true);
    assert.equal(isFitted({ scale: 1.5, x: 0, y: 0 }), false);
});

test('a zoomed picture stops at its edges, and a side smaller than the stage stays in the middle', () => {
    // Twice as large, it's 780 by 585: wider than the stage, and shorter.
    assert.deepEqual(clampView({ scale: 2, x: 100, y: 0 }, box, stage, 8), { scale: 2, x: 0, y: 57.5 });
    assert.deepEqual(clampView({ scale: 2, x: -5000, y: 0 }, box, stage, 8), { scale: 2, x: -390, y: 57.5 });
    assert.deepEqual(clampView({ scale: 2, x: -200, y: 0 }, box, stage, 8), { scale: 2, x: -200, y: 57.5 });
    // Past the most it may zoom, and below fitted, it's held to them.
    assert.equal(clampView({ scale: 20, x: 0, y: 0 }, box, stage, 8).scale, 8);
    assert.deepEqual(clampView({ scale: 0.5, x: 7, y: 7 }, box, stage, 8), fitted(box, stage));
    // Comparing's 100% may be smaller than fitted, which its least allows.
    assert.equal(clampView({ scale: 0.5, x: 0, y: 0 }, box, stage, 8, 0.5).scale, 0.5);
});

test('zooming keeps the spot under the pointer where it is', () => {
    const v = zoomAt(fitted(box, stage), 3, 100, 350, box, stage, 8);
    assert.equal(v.scale, 3);
    // The spot under (100, 350) was 100 across and 146.25 down the fitted
    // picture, and is three times that into the zoomed one.
    assert.ok(near({ x: 100 - 3 * 100, y: 350 - 3 * 146.25 }, { x: v.x, y: v.y }));
    // Zooming back out at the same spot undoes it.
    assert.ok(near(zoomAt(v, 1, 100, 350, box, stage, 8), fitted(box, stage)));
});

test('a pinch follows the fingers: their spread zooms, and their middle carries the picture', () => {
    const start = fitted(box, stage);
    // Fingers 100 apart about the middle spread to 250 apart, then move.
    const mid0 = { x: 195, y: 350 };
    const wide = pinched(start, mid0, 100, mid0, 250, box, stage, 8);
    assert.equal(wide.scale, 2.5);
    assert.ok(near(wide, zoomAt(start, 2.5, 195, 350, box, stage, 8)));
    const moved = pinched(start, mid0, 100, { x: 245, y: 350 }, 250, box, stage, 8);
    assert.equal(moved.x - wide.x, 50);
    // Fingers that close past fitted leave it fitted.
    assert.deepEqual(pinched(start, mid0, 100, mid0, 20, box, stage, 8), start);
});

test('a finger pans a zoomed picture as far as it reaches, and a fitted one not at all', () => {
    const v = zoomAt(fitted(box, stage), 2, 195, 350, box, stage, 8);
    assert.equal(panned(v, -40, 0, box, stage, 8).x, v.x - 40);
    assert.equal(panned(v, -4000, 0, box, stage, 8).x, -390);
    assert.deepEqual(panned(fitted(box, stage), 80, 80, box, stage, 8), fitted(box, stage));
});

test('a double tap zooms in at the spot, and out from anything larger', () => {
    const v = toggled(fitted(box, stage), 50, 300, box, stage, 8);
    assert.equal(v.scale, ZOOM);
    assert.ok(near(v, zoomAt(fitted(box, stage), ZOOM, 50, 300, box, stage, 8)));
    assert.deepEqual(toggled(v, 50, 300, box, stage, 8), fitted(box, stage));
    // A picture that may zoom less than that zooms as far as it may.
    assert.equal(toggled(fitted(box, stage), 50, 300, box, stage, 2).scale, 2);
});

test('the wheel up zooms in, down out, a line as 16 pixels, and a fast turn no further than a slow one', () => {
    assert.ok(wheeled(-100) > 1);
    assert.ok(wheeled(100) < 1);
    assert.equal(wheeled(-3, 1), wheeled(-48));
    assert.equal(wheeled(-100) * wheeled(100), 1);
    assert.equal(wheeled(-5000), wheeled(-200));
});

test('a picture zooms until its pixels show twice as large as the screen\'s, and at least four times', () => {
    // 4032 pixels on a screen of two to each page pixel, fitted 390 wide.
    assert.equal(maxScale(4032, box, 2), 4032 / 390);
    assert.equal(maxScale(400, { width: 390, height: 292.5 }, 2), 4);
    assert.equal(maxScale(0, box, 2), 4);
});
