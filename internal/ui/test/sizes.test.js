// Appearance's sizes, checked without a browser: the browser's choices,
// photos and videos at Medium and players at Small until the member makes
// one; the box a photo fits at each, and a player's; a photo larger than
// the den's preview shown itself; and the phone layout keeping Small.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { Attachments, fitBox } from '../assets/js/src/files.jsx';
import { MEDIA, PLAYERS, SIZES, mediaBox, playerClass, setSize, size } from '../assets/js/src/sizes.js';
import { pictureURL } from '../assets/js/src/youtube.jsx';

// A wide window, or with wide false the phone layout.
const windowOf = (wide) => ({ matchMedia: () => ({ matches: wide, addEventListener() {}, removeEventListener() {} }), addEventListener() {}, removeEventListener() {} });
globalThis.window = windowOf(true);

function all(node, match, found = []) {
    if (node.nodeType === 1 && match(node)) found.push(node);
    for (const child of node.childNodes || []) all(child, match, found);
    return found;
}

test('photos and videos show at Medium and players at Small until the member chooses, each kept apart', () => {
    assert.deepEqual(SIZES, ['small', 'medium', 'large', 'largest']);
    assert.equal(size(MEDIA), 'medium');
    assert.equal(size(PLAYERS), 'small');
    setSize(MEDIA, 'largest');
    assert.equal(size(MEDIA), 'largest');
    assert.equal(size(PLAYERS), 'small', "the players' choice is its own");
    setSize(PLAYERS, 'large');
    assert.equal(size(PLAYERS), 'large');
    setSize(MEDIA, 'enormous');
    assert.equal(size(MEDIA), 'medium');
    setSize(PLAYERS, 'small');
});

test('a photo fits a box four wide to three high, several a smaller one each, and never grows past its own pixels', () => {
    assert.deepEqual(SIZES.map((s) => mediaBox(s, false)), [{ w: 400, h: 300 }, { w: 640, h: 480 }, { w: 960, h: 720 }, { w: 1280, h: 960 }]);
    assert.deepEqual(SIZES.map((s) => mediaBox(s, true)), [{ w: 240, h: 180 }, { w: 384, h: 288 }, { w: 576, h: 432 }, { w: 768, h: 576 }]);
    assert.deepEqual(mediaBox('anything else', false), { w: 400, h: 300 });
    const large = mediaBox('large', false);
    assert.deepEqual(fitBox(4000, 3000, large.w, large.h), { width: 960, height: 720 });
    assert.deepEqual(fitBox(3000, 4000, large.w, large.h), { width: 540, height: 720 }, 'a portrait one by its height');
    assert.deepEqual(fitBox(800, 600, large.w, large.h), { width: 800, height: 600 }, 'a small one as it is');
});

test("a player's box is 16:9 at each size, a Short's 9:16 and no taller than a window, and a larger player asks for a larger picture", () => {
    assert.deepEqual(SIZES.map((s) => playerClass(s, false)), ['aspect-video w-[400px]', 'aspect-video w-[640px]', 'aspect-video w-[960px]', 'aspect-video w-[1280px]']);
    assert.deepEqual(SIZES.map((s) => playerClass(s, true)), ['aspect-[9/16] w-[225px]', 'aspect-[9/16] w-[360px]', 'aspect-[9/16] w-[405px]', 'aspect-[9/16] w-[405px]']);
    assert.equal(playerClass(undefined, false), 'aspect-video w-[400px]');
    const video = { id: 'dQw4w9WgXcQ', start: 0 };
    assert.equal(pictureURL(video), '/api/youtube/dQw4w9WgXcQ/thumb');
    assert.equal(pictureURL(video, 'small'), '/api/youtube/dQw4w9WgXcQ/thumb');
    assert.equal(pictureURL(video, 'medium'), '/api/youtube/dQw4w9WgXcQ/large');
    assert.equal(pictureURL({ ...video, short: true }, 'largest'), '/api/youtube/dQw4w9WgXcQ/portrait');
});

test('a message shows its photo at the size chosen, the photo itself over its preview once larger than that, and the phone layout keeps Small', async () => {
    const photo = { id: '5', name: 'photo.jpg', type: 'image/jpeg', size: 900000, width: 4000, height: 3000, thumb: true };
    const page = document.createElement('div');
    const tile = () => all(page, (n) => n.localName === 'button')[0];
    const sharp = () => all(page, (n) => 'data-sharp' in n.attributes).map((n) => n.attributes.src);
    const settle = async () => { for (let i = 0; i < 60; i++) await new Promise((r) => setTimeout(r, 0)); };
    const show = async (files) => {
        mount(null, page);
        mount(h(Attachments, { denID: 'd', files, onOpen() {} }), page);
        await settle();
    };

    await show([photo]);
    assert.equal(tile().style.width, '640px', 'Medium until the member chooses');
    assert.deepEqual(sharp(), [], "the den's preview covers Medium");
    setSize(MEDIA, 'large');
    await settle();
    assert.equal(tile().style.width, '960px', 'the choice takes at once');
    assert.deepEqual(sharp(), ['/api/dens/d/files/5']);
    // A picture that moves, or one a browser may not draw, keeps its preview.
    await show([{ ...photo, type: 'image/gif', animated: true }]);
    assert.deepEqual(sharp(), []);
    await show([{ ...photo, type: 'image/heic' }]);
    assert.deepEqual(sharp(), []);
    // A small photo is no larger for a larger box.
    await show([{ ...photo, width: 500, height: 400 }]);
    assert.equal(tile().style.width, '500px');
    assert.deepEqual(sharp(), []);
    // Several get a smaller box each.
    await show([photo, { ...photo, id: '6' }]);
    assert.equal(tile().style.width, '576px');
    assert.deepEqual(sharp(), []);

    globalThis.window = windowOf(false);
    await show([photo]);
    assert.equal(tile().style.width, '400px', 'the phone layout keeps Small');
    assert.deepEqual(sharp(), []);
    globalThis.window = windowOf(true);
    setSize(MEDIA, 'medium');
    mount(null, page);
});
