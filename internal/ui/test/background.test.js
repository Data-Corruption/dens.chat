// The picture behind the chat, checked without a browser: what a dens view
// names read, the style that draws it under its dim, and the files the
// page turns away before sending them.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_FILE, backgroundStyle, backgroundURL, checkBackground, onBackground, readBackground, slideDim } from '../assets/js/src/background.js';

test('a dens view names the background by its version, with its dim', () => {
    assert.deepEqual(readBackground({ version: 1700000000000, dim: 80 }), { version: 1700000000000, dim: 80 });
    assert.deepEqual(readBackground({ version: 5, dim: 0 }), { version: 5, dim: 0 });
    assert.deepEqual(readBackground({ version: 5 }), { version: 5, dim: 70 });
    assert.deepEqual(readBackground({ version: 5, dim: 95 }), { version: 5, dim: 70 });
    assert.deepEqual(readBackground({ version: 5, dim: '40%' }), { version: 5, dim: 70 });
    for (const none of [undefined, null, {}, { version: 0, dim: 50 }, { version: '1); url(x', dim: 50 }, { version: 1.5 }]) {
        assert.equal(readBackground(none), null, JSON.stringify(none));
    }
});

test("the list draws the picture, filling it, under the theme's background at the dim", () => {
    assert.equal(backgroundStyle(null), undefined);
    const style = backgroundStyle({ version: 7, dim: 40 });
    assert.equal(backgroundURL(7), '/api/background?v=7');
    assert.match(style.backgroundImage, /^linear-gradient\(color-mix\(in oklab, var\(--color-base-100\) 40%, transparent\), .*\), url\("\/api\/background\?v=7"\)$/);
    assert.equal(style.backgroundSize, 'cover');
});

test('the page turns away a file of another kind, or past 10 MB', () => {
    assert.equal(checkBackground({ type: 'image/png', size: 1000 }), '');
    assert.equal(checkBackground({ type: 'image/jpeg', size: MAX_FILE }), '');
    assert.equal(checkBackground({ type: 'image/webp', size: 1 }), '');
    assert.match(checkBackground({ type: 'image/gif', size: 1000 }), /PNG, a JPEG or a WebP/);
    assert.match(checkBackground({ type: '', size: 1000 }), /PNG, a JPEG or a WebP/);
    assert.match(checkBackground({ type: 'image/png', size: MAX_FILE + 1 }), /10 MB/);
});

test('with no background, a dim slid shows nothing', () => {
    const heard = [];
    const off = onBackground((bg) => heard.push(bg));
    slideDim(30);
    off();
    assert.deepEqual(heard, [null]);
});
