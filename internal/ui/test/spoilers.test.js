// Spoilers (M17.2), checked without a browser: which files waiting to go
// may go as one, a spoiler's photo covered until it's clicked, with
// nothing of it named, and the viewer leaving out those still covered.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { Attachments, covers } from '../assets/js/src/files.jsx';
import { spoilable, viewed } from '../assets/js/src/messages.jsx';

globalThis.window = { matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) };

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

test("the viewer shows a message's images but the spoilers still covered", () => {
    const m = { id: '5', attachments: [photo('a', 'twist.jpg', true), photo('c', 'credits.jpg', false), { id: 'd', name: 'notes.txt', type: 'text/plain' }] };
    assert.deepEqual(viewed(m, m.attachments[1], new Set(['a'])), { message: '5', files: [m.attachments[1]], index: 0 });
    assert.deepEqual(viewed(m, m.attachments[0]).files.map((f) => f.id), ['a', 'c']);
    assert.equal(viewed(m, m.attachments[0], new Set(['a'])), null);
});
