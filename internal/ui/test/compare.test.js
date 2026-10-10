import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    actualSize, clampPan, comparable, describe, mayGoSmaller, other, othersToSwitch, panTo, sendSmaller, sentLabel, setSendSmaller,
    switchBlocked, switchLabel, usage,
} from '../assets/js/src/compare.jsx';

// The browser's storage, as far as the setting uses it.
function fakeStorage(items = {}) {
    return {
        items,
        getItem: (k) => (k in items ? items[k] : null),
        setItem: (k, v) => { items[k] = String(v); },
        removeItem: (k) => { delete items[k]; },
    };
}

test('photos go smaller unless the member turned it off, which the browser keeps', () => {
    globalThis.localStorage = fakeStorage({ DENS_SEND_SMALLER: '0' });
    try {
        assert.equal(sendSmaller(), false);
        setSendSmaller(true);
        assert.equal(sendSmaller(), true);
        assert.deepEqual(globalThis.localStorage.items, {});
        setSendSmaller(false);
        assert.deepEqual(globalThis.localStorage.items, { DENS_SEND_SMALLER: '0' });
    } finally {
        delete globalThis.localStorage;
    }
    // Without storage the choice still holds for the page.
    setSendSmaller(true);
    assert.equal(sendSmaller(), true);
});

test('a file over the limit may still go as a copy when it may be a photo', () => {
    assert.equal(mayGoSmaller({ type: 'image/jpeg', name: 'a.jpg' }), true);
    assert.equal(mayGoSmaller({ type: 'image/png', name: 'shot' }), true);
    // Some browsers give a HEIC no type at all.
    assert.equal(mayGoSmaller({ type: '', name: 'IMG_0001.HEIC' }), true);
    assert.equal(mayGoSmaller({ type: '', name: 'scan.tif' }), true);
    assert.equal(mayGoSmaller({ type: 'application/pdf', name: 'paper.pdf' }), false);
    assert.equal(mayGoSmaller({ type: 'video/mp4', name: 'clip.mp4' }), false);
    assert.equal(mayGoSmaller({ type: '', name: 'heic.zip' }), false);
});

const versions = (sent, fits = true) => ({
    sent,
    full: { type: 'image/jpeg', size: 3_250_000, width: 4032, height: 3024, fits },
    smaller: { type: 'image/jpeg', size: 840_000, width: 2560, height: 1920, fits: true },
});
const entry = (key, sent, extra = {}) => ({ key, result: { id: `u${key}`, name: `${key}.jpg`, versions: versions(sent, extra.fits ?? true) }, ...extra });

test('each version is labeled with its pixels and size, and the chip says which goes', () => {
    assert.equal(describe(versions('smaller').smaller), '2560 × 1920 · 820 KB');
    assert.equal(describe(versions('smaller').full), '4032 × 3024 · 3.1 MB');
    assert.equal(sentLabel(versions('smaller')), 'smaller');
    assert.equal(sentLabel(versions('full')), 'full size');
    assert.equal(switchLabel(versions('smaller')), 'Send full size instead');
    assert.equal(switchLabel(versions('full')), 'Send smaller instead');
    assert.equal(other('smaller'), 'full');
    assert.equal(other('full'), 'smaller');
});

test("full size can't be picked over the den's limit, and says so", () => {
    assert.equal(switchBlocked(versions('smaller', false), { file_size: 25 * 1024 * 1024 }), "Its full size is over this den's 25 MB limit.");
    assert.equal(switchBlocked(versions('smaller', false), null), "Its full size is over this den's limit.");
    assert.equal(switchBlocked(versions('smaller'), { file_size: 1 }), '');
    assert.equal(switchBlocked(versions('full'), { file_size: 1 }), '');
});

test('only photos with two versions compare, and the choice can go to the others', () => {
    const files = [entry(1, 'smaller'), { key: 2, result: { id: 'u2', name: 'notes.pdf' } }, entry(3, 'smaller'), entry(4, 'full'),
        entry(5, 'smaller', { fits: false }), entry(6, 'smaller', { switching: true }), { key: 7, progress: 0.5 }];
    assert.deepEqual(comparable(files).map((f) => f.key), [1, 3, 4, 5, 6]);
    // To full size: the others going smaller, within the limit, not busy.
    assert.deepEqual(othersToSwitch(files, 1, 'full').map((f) => f.key), [3]);
    // To smaller: those going full size.
    assert.deepEqual(othersToSwitch(files, 1, 'smaller').map((f) => f.key), [4]);
    assert.deepEqual(othersToSwitch(files, 4, 'smaller'), []);
});

test('100% puts one of the photo\'s pixels on each of the screen\'s', () => {
    assert.deepEqual(actualSize({ width: 4032, height: 3024 }, 2), { width: 2016, height: 1512 });
    assert.deepEqual(actualSize({ width: 4032, height: 3024 }, 1), { width: 4032, height: 3024 });
    assert.deepEqual(actualSize({ width: 4032, height: 3024 }, 0), { width: 4032, height: 3024 });
});

test('dragging at 100% stops at the photo\'s edges, and a smaller side stays centered', () => {
    const view = { width: 1000, height: 800 };
    const box = { width: 4000, height: 600 };
    assert.deepEqual(clampPan({ x: 100, y: 0 }, box, view), { x: 0, y: 100 });
    assert.deepEqual(clampPan({ x: -5000, y: -50 }, box, view), { x: -3000, y: 100 });
    assert.deepEqual(clampPan({ x: -1200, y: 0 }, box, view), { x: -1200, y: 100 });
    // Looking at a spot centers it, as far as the photo reaches.
    assert.deepEqual(panTo(0.5, 0.5, box, view), { x: -1500, y: 100 });
    assert.deepEqual(panTo(0, 0, box, view), { x: 0, y: 100 });
    assert.deepEqual(panTo(1, 1, box, view), { x: -3000, y: 100 });
});

test("the comparison says how much of the member's space they use", () => {
    assert.equal(usage({ used: 120 * 1024 * 1024 }, { member_storage: 2 * 1024 ** 3 }), 'You use 120 MB of your 2 GB on this den.');
    assert.equal(usage({ used: 1024 }, null), 'You use 1 KB on this den.');
    assert.equal(usage(null, { member_storage: 1 }), '');
});
