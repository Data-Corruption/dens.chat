// The picture behind the chat, checked without a browser: what a dens view
// names read, the style that draws it under its dim, with how far a
// message under the pointer covers it, and the files the page turns away
// before sending them.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_FILE, backgroundStyle, backgroundURL, checkBackground, onBackground, readBackground, slideDim, slideHover } from '../assets/js/src/background.js';

test('a dens view names the background by its version, with its dim and hover', () => {
    assert.deepEqual(readBackground({ version: 1700000000000, dim: 80, hover: 35 }), { version: 1700000000000, dim: 80, hover: 35 });
    assert.deepEqual(readBackground({ version: 5, dim: 0, hover: 0 }), { version: 5, dim: 0, hover: 0 });
    assert.deepEqual(readBackground({ version: 5 }), { version: 5, dim: 70, hover: 100 });
    assert.deepEqual(readBackground({ version: 5, dim: 95, hover: 101 }), { version: 5, dim: 70, hover: 100 });
    assert.deepEqual(readBackground({ version: 5, dim: '40%', hover: '5%' }), { version: 5, dim: 70, hover: 100 });
    for (const none of [undefined, null, {}, { version: 0, dim: 50 }, { version: '1); url(x', dim: 50 }, { version: 1.5 }]) {
        assert.equal(readBackground(none), null, JSON.stringify(none));
    }
});

test("the list draws the picture, filling it, under the theme's background at the dim", () => {
    assert.equal(backgroundStyle(null), undefined);
    const style = backgroundStyle({ version: 7, dim: 40, hover: 100 });
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

// A message under the pointer, or with its menu open, covers the picture
// by the hover, which its row reads from the list's style; with no
// picture, it covers the page as it always did.
test('a message under the pointer covers the picture by the hover', () => {
    for (const hover of [0, 50, 100]) assert.equal(backgroundStyle({ version: 7, dim: 40, hover })['--row-cover'], `${hover}%`);
});

test('with no background, a hover slid shows nothing', () => {
    const heard = [];
    const off = onBackground((bg) => heard.push(bg));
    slideHover(30);
    off();
    assert.deepEqual(heard, [null]);
});
