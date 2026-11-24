// Forward, checked without a browser: the places it lists across two
// dens, and those it leaves out; and a forward's mark, with no Edit or
// tick under it.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { forwardList } from '../src/core/switcher.js';
import { MessageRow, editable } from '../src/chat/messages.jsx';

globalThis.window = { matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) };

const HOME = 'home';
const OTHER = 'other';
const dens = [{ den_id: HOME, name: 'Home Den' }, { den_id: OTHER, name: 'Game Club' }];
const home = {
    state: 'connected', seal: true, forward: true, has_notes: true,
    me: { id: '1' },
    members: [{ id: '1', username: 'alice', display_name: 'Alice' }, { id: '2', username: 'bob', display_name: 'Bob Stone' },
        { id: '3', username: 'cy', display_name: 'Cy', left_at: 5 }],
    groups: [],
    channels: [
        { id: '10', kind: 'text', name: 'general', position: 0 },
        { id: '11', kind: 'voice', name: 'Lounge', position: 1 },
        { id: '90', kind: 'dm', members: ['1', '2'] },
        { id: '91', kind: 'dm', members: ['1', '3'] },
        { id: '95', kind: 'notes', members: ['1'] },
    ],
    read_states: [],
    group_dms: {},
};
const other = {
    state: 'connected', seal: true, forward: true, has_notes: true,
    me: { id: '7' },
    members: [{ id: '7', username: 'alice', display_name: 'Alice' }, { id: '8', username: 'gabe', display_name: 'Gabe' }, { id: '9', username: 'zed', display_name: 'Zed' }],
    groups: [],
    channels: [{ id: '20', kind: 'text', name: 'general', position: 0 }, { id: '30', kind: 'group_dm', members: ['7', '8', '9'] }],
    read_states: [],
    group_dms: { 30: { name: '', members: ['7', '8', '9'] } },
};
const names = (list) => list.map((e) => `${e.den === HOME ? 'home' : 'other'}:${e.kind}:${e.name}:${e.id}`);

test("Forward lists every place a message can go, the open den's first, and no voice channel, den or DM with someone gone", () => {
    const views = new Map([[HOME, home], [OTHER, other]]);
    assert.deepEqual(names(forwardList(dens, views, OTHER, '')),
        ['other:text:general:20', 'other:group:Gabe, Zed:30', 'other:notes:Notes:', 'home:text:general:10', 'home:dm:Bob Stone:90', 'home:notes:Notes:95']);
    assert.deepEqual(names(forwardList(dens, views, HOME, 'gen')), ['home:text:general:10', 'other:text:general:20']);
    assert.deepEqual(names(forwardList(dens, views, HOME, 'lou')), []);
    assert.deepEqual(names(forwardList(dens, views, HOME, 'cy')), []);
});

test("a den that keeps no forward's mark gets none in its channels, one offline none at all, and a device without the seal none sealed", () => {
    const older = new Map([[HOME, { ...home, forward: false }], [OTHER, { ...other, state: 'offline' }]]);
    assert.deepEqual(names(forwardList(dens, older, HOME, '')), ['home:dm:Bob Stone:90', 'home:notes:Notes:95']);
    const unsealed = new Map([[HOME, { ...home, seal: false }], [OTHER, null]]);
    assert.deepEqual(names(forwardList(dens, unsealed, HOME, '')), ['home:text:general:10']);
});

function find(node, match) {
    if (node.nodeType === 1 && match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}

test('a forward shows as forwarded, and its author may forward or delete it, not edit or tick it', () => {
    const root = document.createElement('div');
    const me = { id: '7', username: 'bob', display_name: 'Bob' };
    const m = { id: '10', channel_id: '2', author_id: '7', created_at: Date.UTC(2026, 10, 6, 12), revision: 1, text: '[ ] milk', forwarded: true };
    assert.equal(editable(m, me), false);
    assert.equal(editable({ ...m, forwarded: false }, me), true);
    const none = () => {};
    mount(h(MessageRow, {
        m, author: me, me, canReply: true, canEdit: editable(m, me), canForward: true, canDelete: true, link: '', editorNames: [], candidates: [], canReact: false,
        menuOpen: true, nameOf: () => '', onReact: none, onReactPick: none, onRemoveReactions: none, onUpdated: none, onPolled: none, onMenu: none,
        onReply: none, onEdit: none, onForward: none, onEditDone: none, onDelete: none, onJump: none, onView: none, onProfile: none, onToggle: none,
        denID: '1', channelID: '2',
    }), root);
    assert.equal(find(root, (n) => 'data-forwarded' in n.attributes)?.textContent.trim(), 'Forwarded');
    const items = [];
    (function walk(n) {
        if (n.nodeType === 1 && n.attributes.role === 'menuitem') items.push(n.textContent.trim());
        for (const c of n.childNodes || []) walk(c);
    })(root);
    assert.deepEqual(items, ['Reply', 'Forward', 'Copy text', 'Delete']);
    assert.ok('disabled' in find(root, (n) => n.localName === 'input').attributes, 'its task ticks for nobody');
    mount(null, root);
});
