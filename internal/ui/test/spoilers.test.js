// Spoilers, checked without a browser: which files waiting to go
// may go as one, a spoiler's photo covered until it's clicked, with
// nothing of it named, the viewer leaving out those still covered, and a
// text spoiler holding its links out of reach until it's shown.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { Attachments, covers } from '../src/chat/files.jsx';
import { Markdown, Preview } from '../src/chat/markdown.jsx';
import { LinkContext } from '../src/chat/msglinks.jsx';
import { spoilable, viewed } from '../src/chat/messages.jsx';

globalThis.window = {
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    addEventListener() {},
    location: { protocol: 'http:', host: 'localhost:8484' },
};
// An event's chip, once shown, listens on the page's event stream.
globalThis.document.addEventListener = () => {};
globalThis.WebSocket = class {
    static OPEN = 1;
    readyState = 1;
    addEventListener() {}
    send() {}
    close() {}
};

const settle = () => new Promise((resolve) => setTimeout(resolve, 5));

function find(node, match) {
    if (node.nodeType === 1 && match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}

const photo = (id, name, spoiler) => ({ id, name, type: 'image/jpeg', size: 1000, width: 64, height: 48, thumb: { width: 64, height: 48 }, spoiler });

test('photos and videos waiting to go may go as spoilers, by what they turned out to be or what the browser says', () => {
    assert.equal(spoilable({ result: { type: 'image/png' } }), true);
    assert.equal(spoilable({ result: { type: 'video/mp4' } }), true);
    assert.equal(spoilable({ file: { type: 'video/webm' } }), true);
    assert.equal(spoilable({ result: { type: 'audio/ogg' }, file: { type: 'video/ogg' } }), false);
    assert.equal(spoilable({ file: { type: 'application/pdf' } }), false);
    assert.equal(spoilable({ file: {} }), false);
});

test('a spoiler shows covered until it is clicked, naming nothing of it, and the rest show as they are', async () => {
    const shown = new Set(['b']);
    assert.equal(covers(photo('a', 'twist.jpg', true), shown), true);
    assert.equal(covers(photo('b', 'twist.jpg', true), shown), false);
    assert.equal(covers(photo('c', 'credits.jpg', false), shown), false);

    const root = document.createElement('div');
    let opened = null;
    const files = [photo('a', 'twist.jpg', true), photo('c', 'credits.jpg', false)];
    mount(h(Attachments, { denID: '1', files, onOpen: (f, covered) => { opened = { f, covered }; } }), root);
    const cover = () => find(root, (n) => 'data-spoiler' in n.attributes);
    assert.ok(cover(), 'the spoiler is covered');
    assert.match(cover().textContent, /Spoiler/);
    assert.doesNotMatch(cover().textContent + JSON.stringify(cover().attributes), /twist/, 'the cover names nothing of the file');
    assert.ok(find(root, (n) => n.attributes['aria-label'] === 'Open credits.jpg'), 'the other photo shows');

    find(root, (n) => n.attributes['aria-label'] === 'Open credits.jpg').dispatch('click');
    assert.deepEqual([...opened.covered], ['a'], 'the viewer is told which still cover');

    cover().dispatch('click');
    await settle();
    assert.equal(cover(), null, 'clicked, it shows');
    assert.ok(find(root, (n) => n.attributes['aria-label'] === 'Open twist.jpg'));
    mount(null, root);
});

test('a spoiler drawn as a card, a photo too large to preview or a sound, shows covered too, naming nothing of it', async () => {
    const large = { id: 'd', name: 'ending.png', type: 'image/png', size: 9000000, width: 9000, height: 9000, spoiler: true };
    const sound = { id: 'e', name: 'reveal.ogg', type: 'audio/ogg', size: 2000, spoiler: true };
    const root = document.createElement('div');
    mount(h(Attachments, { denID: '1', files: [large, sound], onOpen: () => {} }), root);
    const cards = () => {
        const found = [];
        (function walk(n) {
            if (n.nodeType === 1 && 'data-spoiler' in n.attributes) found.push(n);
            for (const c of n.childNodes || []) walk(c);
        })(root);
        return found;
    };
    assert.equal(cards().length, 2, 'both covered');
    assert.doesNotMatch(root.textContent + JSON.stringify(cards().map((c) => c.attributes)), /ending|reveal/, 'nothing names them');
    cards()[0].dispatch('click');
    await settle();
    assert.equal(cards().length, 1, 'clicked, the photo shows');
    assert.match(root.textContent, /ending\.png/);
    assert.doesNotMatch(root.textContent, /reveal/, 'and the sound stays covered');
    mount(null, root);
});

test("the viewer shows a message's images but the spoilers still covered", () => {
    const m = { id: '5', channel_id: '3', attachments: [photo('a', 'twist.jpg', true), photo('c', 'credits.jpg', false), { id: 'd', name: 'notes.txt', type: 'text/plain' }] };
    assert.deepEqual(viewed(m, m.attachments[1], new Set(['a'])), { message: '5', at: { channel: '3', message: '5' }, files: [m.attachments[1]], index: 0 });
    assert.deepEqual(viewed(m, m.attachments[0]).files.map((f) => f.id), ['a', 'c']);
    assert.equal(viewed(m, m.attachments[0], new Set(['a'])), null);
});

test("a covered text spoiler holds what's in it out of reach, and its event's chip asks for nothing until it's shown", async () => {
    const asked = [];
    globalThis.fetch = async (path) => {
        asked.push(path);
        return { ok: false, status: 404, headers: { get: () => null }, text: async () => JSON.stringify({ error: 'gone' }) };
    };
    const den = 'TqFAWIFQdzzjqs54au739ASc4QD6ZJyU-73blg8dqUI';
    const links = { denID: den, view: { me: { id: '1' }, channels: [], events: [] }, dens: [{ den_id: den, name: 'Home', url: 'https://den.example.com' }] };
    const root = document.createElement('div');
    // Effects run about 100 ms after a render, here.
    const effects = () => new Promise((resolve) => setTimeout(resolve, 150));
    mount(h(LinkContext.Provider, { value: links }, h(Markdown, { text: `||see https://example.com/x at https://den.example.com/#e/${den}/12||` })), root);
    await effects();
    const cover = find(root, (n) => n.attributes.role === 'button');
    const chip = () => find(root, (n) => 'data-event-link' in n.attributes);
    assert.ok(find(cover, (n) => n !== cover && n.attributes.inert !== undefined), 'what it covers takes no focus or pointer');
    assert.equal(chip(), null, 'no chip while covered');
    assert.deepEqual(asked, [], 'nothing asked for');

    cover.dispatch('click');
    await effects();
    assert.equal(find(cover, (n) => n !== cover && n.attributes.inert !== undefined), null, 'shown, it takes them');
    assert.ok(chip(), 'and the chip shows');
    assert.deepEqual(asked, [`/api/dens/${den}/events/12`]);
    mount(null, root);
});

test("a reply's quote covers a spoiler's link as its text", () => {
    const root = document.createElement('div');
    mount(h(Preview, { text: 'see ||https://example.com/x||' }), root);
    const cover = find(root, (n) => /\bbg-base-300\b/.test(n.attributes.class || ''));
    assert.ok(cover && find(cover, (n) => n.textContent === 'https://example.com/x'), 'the link is in the cover');
    assert.match(cover.attributes.class, /\[&_\*\]:text-transparent/, "its own color doesn't show through");
    mount(null, root);
});
