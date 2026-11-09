// Appearance's Density (M17.2), checked without a browser: the
// browser's choice, Default until the member makes one; which messages
// show under the one before's name, none in Compact; and Compact's row,
// a line of the time, the name and the text, with no picture.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { DENSITY_COMPACT, DENSITY_DEFAULT, density, setDensity } from '../assets/js/src/density.js';
import { MessageRow, continues } from '../assets/js/src/messages.jsx';

globalThis.window = { matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) };

test('messages show at Default until the member picks Compact, which the browser keeps', () => {
    assert.equal(density(), DENSITY_DEFAULT);
    setDensity(DENSITY_COMPACT);
    assert.equal(density(), DENSITY_COMPACT);
    setDensity('anything else');
    assert.equal(density(), DENSITY_DEFAULT);
});

test('a run shows its later messages under its first one\'s name, but in Compact each shows its own', () => {
    const at = Date.UTC(2026, 10, 6, 12);
    const first = { id: '1', author_id: '7', created_at: at };
    const next = { id: '2', author_id: '7', created_at: at + 60_000 };
    assert.equal(continues(first, next, false), true);
    assert.equal(continues(first, next, true), false);
    assert.equal(continues(null, first, false), false);
    assert.equal(continues(first, { ...next, author_id: '8' }, false), false);
    assert.equal(continues(first, { ...next, reply_to: '1' }, false), false);
    assert.equal(continues(first, { ...next, created_at: at + 10 * 60_000 }, false), false);
});

function find(node, match) {
    if (node.nodeType === 1 && match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}

test("Compact's row is a line of the time, the name and the text, wrapping under the name, and no picture", () => {
    const root = document.createElement('div');
    const author = { id: '7', username: 'bob', display_name: 'Bob' };
    const me = { id: '3', username: 'alice', display_name: 'Alice' };
    const m = { id: '10', channel_id: '2', author_id: '7', created_at: Date.UTC(2026, 10, 6, 12, 34), revision: 1, text: 'a line of text' };
    const none = () => {};
    const props = {
        m, author, me, canReply: false, canEdit: false, canDelete: false, link: '', editorNames: [], candidates: [], canReact: false,
        nameOf: () => '', onReact: none, onReactPick: none, onRemoveReactions: none, onUpdated: none, onPolled: none, onMenu: none,
        onReply: none, onEdit: none, onEditDone: none, onDelete: none, onJump: none, onView: none, onProfile: none, onToggle: none,
        denID: '1', channelID: '2',
    };
    mount(h(MessageRow, { ...props, line: true }), root);
    const row = find(root, (n) => n.attributes['data-id'] === '10');
    assert.ok(row && 'data-line' in row.attributes, 'the row is a line');
    assert.match(row.textContent, /Bob/);
    assert.match(row.textContent, /a line of text/);
    const name = find(row, (n) => n.localName === 'span' && /\bfloat-left\b/.test(n.attributes.class || ''));
    assert.ok(name && /Bob/.test(name.textContent), "the name floats at the text's start, so the text wraps under it");
    const time = find(row, (n) => /\bw-14\b/.test(n.attributes.class || ''));
    assert.ok(time && time.textContent.trim() !== '', 'the time leads the line');
    assert.equal(find(row, (n) => n.localName === 'img' || n.attributes['aria-label'] === "Bob's profile"), null, 'no picture');

    // Default shows the name over the text, beside the picture, and a
    // run's later message neither.
    mount(h(MessageRow, { ...props, line: false }), root);
    assert.ok(find(root, (n) => n.attributes['aria-label'] === "Bob's profile"), 'the picture, at Default');
    mount(h(MessageRow, { ...props, line: false, compact: true }), root);
    assert.equal(find(root, (n) => n.localName === 'button' && n.textContent === 'Bob'), null);
    mount(null, root);
});
