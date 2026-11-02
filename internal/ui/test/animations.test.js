import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { ALWAYS, HoverRow, ON_HOVER, Shown, animations, setAnimations, usePicture } from '../assets/js/src/animations.js';
import { animatedImage, wholeRect } from '../assets/js/src/crop.jsx';

const settle = () => new Promise((resolve) => setTimeout(resolve, 20));

// system stands in for the browser's storage and its system's asking for
// less motion, which reduce says.
function system(reduce) {
    const kept = new Map();
    globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, v) };
    globalThis.window = {
        matchMedia: (q) => ({ matches: q === '(prefers-reduced-motion: reduce)' && reduce.on, addEventListener() {}, removeEventListener() {} }),
    };
    return kept;
}

function done() {
    delete globalThis.localStorage;
    delete globalThis.window;
}

test("Animations is Always until the system asks for less motion, and the member's choice holds over the system's", () => {
    const reduce = { on: false };
    const kept = system(reduce);
    assert.equal(animations(), ALWAYS);
    reduce.on = true;
    assert.equal(animations(), ON_HOVER);
    setAnimations(ALWAYS);
    assert.equal(kept.get('DENS_ANIMATIONS'), ALWAYS, 'the browser keeps it');
    assert.equal(animations(), ALWAYS);
    kept.set('DENS_ANIMATIONS', 'sometimes');
    assert.equal(animations(), ON_HOVER, "a value the page didn't write leaves it to the system");
    // Without storage, the choice lasts until the page reloads.
    globalThis.localStorage = {
        getItem() {
            throw new Error('denied');
        },
        setItem() {
            throw new Error('denied');
        },
    };
    reduce.on = false;
    setAnimations(ON_HOVER);
    assert.equal(animations(), ON_HOVER);
    done();
});

test("On hover plays a row's pictures while a pointer that hovers is over the row, and a profile's while it shows", async () => {
    system({ on: false });
    setAnimations(ON_HOVER);
    const seen = {};
    function Picture({ name, animated }) {
        seen[name] = usePicture(animated);
        return null;
    }
    const root = document.createElement('div');
    mount(
        h('div', null,
            h(HoverRow, { as: 'li' }, h(Picture, { name: 'row', animated: true }), h(Picture, { name: 'still', animated: false })),
            h(HoverRow, { as: 'li' }, h(Picture, { name: 'other row', animated: true })),
            h(Shown, null, h(Picture, { name: 'profile', animated: true })),
            h(Picture, { name: 'elsewhere', animated: true })),
        root,
    );
    await settle();
    assert.deepEqual(seen, { row: 'still', still: 'file', 'other row': 'still', profile: 'file', elsewhere: 'still' });
    const row = root.find((n) => n.localName === 'li');
    row.dispatch('pointerenter', { pointerType: 'mouse' });
    await settle();
    assert.equal(seen.row, 'hover', 'its preview, with the picture over it');
    assert.equal(seen['other row'], 'still', 'another row holds still');
    row.dispatch('pointerleave', { pointerType: 'mouse' });
    await settle();
    assert.equal(seen.row, 'still');
    row.dispatch('pointerenter', { pointerType: 'touch' });
    await settle();
    assert.equal(seen.row, 'still', "a finger's touch isn't a hover");
    row.dispatch('pointerleave', { pointerType: 'touch' });

    setAnimations(ALWAYS);
    await settle();
    assert.deepEqual(seen, { row: 'file', still: 'file', 'other row': 'file', profile: 'file', elsewhere: 'file' }, 'Always plays them all');
    mount(null, root);
    done();
});

// gif writes a GIF of one-pixel frames, each after an extension and with a
// color table of its own, cut short by cut bytes.
function gif(frames, cut = 0) {
    const b = [...'GIF89a'].map((c) => c.charCodeAt(0));
    b.push(1, 0, 1, 0, 0x80, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff);
    for (let i = 0; i < frames; i++) {
        b.push(0x21, 0xf9, 4, 0, 10, 0, 0, 0);
        b.push(0x2c, 0, 0, 0, 0, 1, 0, 1, 0, 0x80, 1, 2, 3, 4, 5, 6, 2, 2, 0x4c, 0x01, 0);
    }
    b.push(0x3b);
    return new Uint8Array(b.slice(0, b.length - cut));
}

function webp(flags) {
    const b = [...'RIFF\0\0\0\0WEBPVP8X'].map((c) => c.charCodeAt(0));
    b.push(10, 0, 0, 0, flags, 0, 0, 0, 0, 0, 0, 0, 0, 0);
    return new Uint8Array(b);
}

test('an animated GIF has more than one whole frame, and an animated WebP says so in its header', () => {
    assert.equal(animatedImage(gif(1)), false);
    assert.equal(animatedImage(gif(2)), true);
    assert.equal(animatedImage(gif(2, 4)), false, 'cut short in its second frame');
    assert.equal(animatedImage(webp(0x02)), true);
    assert.equal(animatedImage(webp(0x10)), false, 'alpha alone');
    assert.equal(animatedImage(new Uint8Array([...'RIFF\0\0\0\0WEBPVP8 '].map((c) => c.charCodeAt(0)))), false, 'a simple still');
    assert.equal(animatedImage(new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])), false, 'a PNG, animated or not');
});

test("an animated picture's crop is whole pixels on the image, square for a picture", () => {
    assert.deepEqual(wholeRect({ x: 10.4, y: 20.6, w: 100.2, h: 99.8 }, 200, 200, true), { x: 10, y: 21, w: 100, h: 100 });
    assert.deepEqual(wholeRect({ x: 150.7, y: 0.2, w: 50.4, h: 50.4 }, 200, 200, true), { x: 150, y: 0, w: 50, h: 50 }, 'kept on the image');
    assert.deepEqual(wholeRect({ x: 0, y: 0, w: 300.4, h: 100.1 }, 300, 200, false), { x: 0, y: 0, w: 300, h: 100 });
    assert.deepEqual(wholeRect({ x: -0.3, y: 0, w: 120.4, h: 120.6 }, 120, 140, true), { x: 0, y: 0, w: 120, h: 120 });
});
