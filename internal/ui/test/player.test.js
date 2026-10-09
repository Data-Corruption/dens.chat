import assert from 'node:assert/strict';
import { test } from 'node:test';
import { PLAYER_MIN, clampBox, defaultBox, readPlayer, resizeBox, tileLayout } from '../assets/js/src/player.js';

const view = { w: 1600, h: 900 };

test('the box the browser kept is read, or nothing', () => {
    assert.deepEqual(readPlayer(JSON.stringify({ x: 10, y: 20, w: 400, h: 300 })), { x: 10, y: 20, w: 400, h: 300 });
    for (const text of ['', 'null', 'nope', '[]', JSON.stringify({ x: 1, y: 2, w: 'wide', h: 3 })]) assert.equal(readPlayer(text), null, text);
});

test('the player starts at the right, above the message bar, with room for a 16:9 picture under its bar', () => {
    const box = defaultBox(view);
    assert.equal(box.x + box.w, view.w - 16);
    assert.equal(box.y + box.h, view.h - 96);
    assert.equal(box.h, Math.round((box.w * 9) / 16) + 32);
    const small = defaultBox({ w: 320, h: 480 });
    assert.ok(small.x >= 0 && small.x + small.w <= 320);
});

test('the player stays inside the window, however it was left', () => {
    assert.deepEqual(clampBox({ x: 1500, y: 800, w: 400, h: 300 }, view), { x: 1200, y: 600, w: 400, h: 300 });
    assert.deepEqual(clampBox({ x: -50, y: -10, w: 100, h: 50 }, view), { x: 0, y: 0, w: PLAYER_MIN.w, h: PLAYER_MIN.h });
    assert.deepEqual(clampBox({ x: 0, y: 0, w: 5000, h: 5000 }, { w: 800, h: 600 }), { x: 0, y: 0, w: 800, h: 600 });
});

test('resizing from a corner keeps the opposite one where it was', () => {
    const box = { x: 400, y: 300, w: 480, h: 300 };
    assert.deepEqual(resizeBox(box, 'se', 100, 50, view), { x: 400, y: 300, w: 580, h: 350 });
    assert.deepEqual(resizeBox(box, 'nw', -100, -50, view), { x: 300, y: 250, w: 580, h: 350 });
    assert.deepEqual(resizeBox(box, 'ne', 20, 30, view), { x: 400, y: 330, w: 500, h: 270 });
    assert.deepEqual(resizeBox(box, 'sw', -20, -30, view), { x: 380, y: 300, w: 500, h: 270 });
    // Not below the smallest, nor past the window.
    assert.deepEqual(resizeBox(box, 'se', -1000, -1000, view), { x: 400, y: 300, w: PLAYER_MIN.w, h: PLAYER_MIN.h });
    assert.deepEqual(resizeBox(box, 'se', 5000, 5000, view), { x: 400, y: 300, w: 1200, h: 600 });
    assert.deepEqual(resizeBox(box, 'nw', -5000, -5000, view), { x: 0, y: 0, w: 880, h: 600 });
});

test('shares lay out side by side in a wide player, and above each other in a tall one', () => {
    assert.deepEqual(tileLayout(1, 400, 300), { cols: 1, rows: 1 });
    assert.deepEqual(tileLayout(2, 800, 300), { cols: 2, rows: 1 });
    assert.deepEqual(tileLayout(2, 400, 600), { cols: 1, rows: 2 });
    assert.deepEqual(tileLayout(3, 800, 600), { cols: 2, rows: 2 });
    assert.deepEqual(tileLayout(4, 800, 600), { cols: 2, rows: 2 });
    // Four watched and the member's own preview.
    assert.deepEqual(tileLayout(5, 900, 500), { cols: 3, rows: 2 });
    assert.deepEqual(tileLayout(5, 400, 700), { cols: 2, rows: 3 });
});
