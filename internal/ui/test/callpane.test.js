import assert from 'node:assert/strict';
import { test } from 'node:test';
import { GAP, ROW, ROW_ROOM, paneLayout } from '../src/voice/pane.js';

// fits says a layout's tiles fit width by height without scrolling.
function fits({ tiles }, count, width, height) {
    const rows = Math.ceil(count / tiles.cols);
    return tiles.cols * tiles.w + (tiles.cols - 1) * GAP <= width && rows * tiles.h + (rows - 1) * GAP <= height;
}

// The pane's room in a window of 1280 by 800: the channel list's 256
// pixels, the pane's padding, its header and its foot, and a notice
// above it; and at a phone's 390 by 844.
const DESKTOP = { w: 1280 - 256 - 16, h: 800 - 49 - 72 - 16 - 64 };
const PHONE = { w: 390 - 16, h: 844 - 49 - 72 - 16 };

test("a call of 15 fits a desktop's window without scrolling, and a phone's too", () => {
    for (const room of [DESKTOP, PHONE]) {
        const l = paneLayout(15, 0, room.w, room.h);
        assert.equal(l.stage, null);
        assert.ok(fits(l, 15, room.w, room.h), JSON.stringify({ room, l }));
        assert.ok(l.tiles.w >= 96, 'tiles a finger and a name fit');
    }
});

test("one member's tile and two members' grow no wider than a tile's most, side by side", () => {
    const one = paneLayout(1, 0, DESKTOP.w, DESKTOP.h);
    assert.deepEqual(one.tiles, { row: false, cols: 1, w: 320, h: 240 });
    const two = paneLayout(2, 0, DESKTOP.w, DESKTOP.h);
    assert.equal(two.tiles.cols, 2);
    assert.equal(two.tiles.w, 320);
    // On a phone, two stand one above the other.
    const phone = paneLayout(2, 0, PHONE.w, PHONE.h);
    assert.ok(fits(phone, 2, PHONE.w, PHONE.h));
});

test('a watched share takes the stage, the tiles a row under it, and two shares stand side by side', () => {
    const one = paneLayout(3, 1, DESKTOP.w, DESKTOP.h);
    assert.deepEqual({ cols: one.stage.cols, rows: one.stage.rows }, { cols: 1, rows: 1 });
    assert.equal(one.stage.height, DESKTOP.h - ROW_ROOM - GAP);
    assert.equal(one.tiles.row, true);
    assert.equal(one.tiles.h, ROW);
    const two = paneLayout(15, 2, DESKTOP.w, DESKTOP.h);
    assert.deepEqual({ cols: two.stage.cols, rows: two.stage.rows }, { cols: 2, rows: 1 });
    // A phone's pane is tall, so its two stand one above the other.
    const phone = paneLayout(3, 2, PHONE.w, PHONE.h);
    assert.deepEqual({ cols: phone.stage.cols, rows: phone.stage.rows }, { cols: 1, rows: 2 });
});

test('a pane too small for its tiles fits as many across as it can, and scrolls', () => {
    const l = paneLayout(15, 0, 300, 200);
    assert.equal(l.tiles.cols, 2);
    assert.ok(l.tiles.w >= 96);
    assert.deepEqual(paneLayout(0, 0, 300, 200).tiles, { row: false, cols: 1, w: 0, h: 0 });
});
