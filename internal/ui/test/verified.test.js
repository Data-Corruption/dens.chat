import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { HoverRow, Shown } from '../assets/js/src/animations.js';
import { VerifiedMark } from '../assets/js/src/private.jsx';
import { MARKS_ALWAYS, MARKS_HOVER, MARKS_NEVER, setVerifiedMarks, verifiedMarks } from '../assets/js/src/verified.js';

// touch is whether the screen has a finger and no hover, as matchMedia
// says.
let touch = false;
globalThis.window = { matchMedia: () => ({ matches: touch, addEventListener() {}, removeEventListener() {} }) };

const settle = () => new Promise((resolve) => setTimeout(resolve, 5));

// until waits for cond, as effects run a frame after a render.
async function until(cond, what) {
    for (let i = 0; i < 100; i++) {
        if (cond()) return;
        await settle();
    }
    assert.fail(`timed out waiting for ${what}`);
}

function find(node, match) {
    if (node.nodeType === 1 && match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}
const mark = (root) => find(root, (n) => n.attributes?.['data-verified-mark'] !== undefined);

test('verified marks show on hover with a mouse, and not on a touch screen, until the member picks', () => {
    assert.equal(verifiedMarks(false), MARKS_HOVER);
    assert.equal(verifiedMarks(true), MARKS_NEVER);
    setVerifiedMarks(MARKS_ALWAYS);
    assert.equal(verifiedMarks(false), MARKS_ALWAYS);
    assert.equal(verifiedMarks(true), MARKS_ALWAYS);
    // On hover can't show a thing without hover.
    setVerifiedMarks(MARKS_HOVER);
    assert.equal(verifiedMarks(true), MARKS_NEVER);
    setVerifiedMarks(MARKS_NEVER);
    assert.equal(verifiedMarks(false), MARKS_NEVER);
});

test("a verified mark shows in its row as the choice says, in a profile on hover, and where comparing is the subject always", async () => {
    const root = document.createElement('div');
    setVerifiedMarks(MARKS_HOVER);
    mount(h(HoverRow, { as: 'li' }, h('span', null, 'Bob'), h(VerifiedMark, { id: 'v', name: 'Bob' })), root);
    assert.equal(mark(root).attributes.class, 'invisible');
    const li = () => find(root, (n) => n.localName === 'li');
    li().dispatch('pointerenter', { pointerType: 'mouse' });
    await until(() => mark(root).attributes.class === '', 'the mark under the pointer');
    li().dispatch('pointerleave', {});
    await until(() => mark(root).attributes.class === 'invisible', 'the mark once the pointer left');
    // A finger's touch isn't a hover.
    li().dispatch('pointerenter', { pointerType: 'touch' });
    await settle();
    assert.equal(mark(root).attributes.class, 'invisible');

    mount(h(Shown, null, h(VerifiedMark, { id: 'v', name: 'Bob' })), root);
    await until(() => mark(root)?.attributes.class === '', 'the mark in a profile');

    setVerifiedMarks(MARKS_NEVER);
    await until(() => mark(root) === null, 'no mark');
    mount(h(VerifiedMark, { id: 'v', name: 'Bob', always: true }), root);
    assert.ok(mark(root));
    assert.match(root.textContent, /Verified: you compared codes with Bob\./);

    setVerifiedMarks(MARKS_ALWAYS);
    mount(h(HoverRow, { as: 'li' }, h(VerifiedMark, { id: 'v', name: 'Bob' })), root);
    await until(() => mark(root)?.attributes.class === '', 'the mark always');
    mount(null, root);
});
