// Notes to self (M17.3): what they are, their row at the top of the DM
// list, what stands in their composer's place, and how a file in them and
// a link to one of them read.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { isNotes, isPrivate, notesGate, ownNotes } from '../assets/js/src/private.js';
import { NotesRow } from '../assets/js/src/notes.jsx';
import { DMGate } from '../assets/js/src/private.jsx';
import { lastDM, listed, recentDMs } from '../assets/js/src/chat.jsx';
import { fileWhere } from '../assets/js/src/files.jsx';
import { messageLink } from '../assets/js/src/links.js';
import { chipLabel, place } from '../assets/js/src/msglinks.jsx';
import { ProfileCard } from '../assets/js/src/people.jsx';

const HOME = 'TqFAWIFQdzzjqs54au739ASc4QD6ZJyU-73blg8dqUI';

const view = {
    me: { id: '1' },
    members: [{ id: '1', display_name: 'Alice', username: 'alice' }, { id: '2', display_name: 'Bob', username: 'bob' }],
    groups: [],
    channels: [
        { id: '10', kind: 'text', name: 'general', position: 0 },
        { id: '40', kind: 'dm', members: ['1', '2'] },
        { id: '50', kind: 'notes', members: ['1'] },
    ],
};

function find(node, match) {
    if (match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}

const button = (text) => (n) => n.localName === 'button' && n.textContent.trim() === text;

test("the member's notes are sealed and in the Direct messages tab, apart from the DMs", () => {
    const notes = view.channels[2];
    assert.ok(isPrivate(notes) && isNotes(notes) && !isNotes(view.channels[1]));
    assert.equal(ownNotes(view), notes);
    assert.equal(ownNotes({ ...view, channels: view.channels.slice(0, 2) }), null, 'none until the den makes them');
    const reads = new Map([
        ['40', { channel_id: '40', last_message_id: '100' }],
        ['50', { channel_id: '50', last_message_id: '900', message_id: '900' }],
    ]);
    // Their row stays at the top, so the recent DMs leave them out, newer or
    // not, and so does the den's channel list.
    assert.deepEqual(recentDMs(view, reads, '50').map((c) => c.id), ['40']);
    assert.deepEqual(listed(view).ungrouped.map((c) => c.id), ['10']);
    // The Direct messages tab opens them when they were last open there.
    assert.equal(lastDM(view, reads, '50'), notes);
});

test('the notes take notes once this device holds the DM recovery key, which their key comes from', () => {
    assert.equal(notesGate({ ...view, seal: true }), '');
    assert.equal(notesGate({ ...view, seal: false }), 'no_seal');
    const root = document.createElement('div');
    mount(h(DMGate, { denID: 'D', gate: 'no_seal', dm: null, notes: true }), root);
    assert.match(root.textContent, /can't read or write your notes here/);
    assert.ok(find(root, button('Enter it')));
    mount(null, root);
});

test('the notes row opens the notes, and shows them open', () => {
    const root = document.createElement('div');
    let opened = 0;
    mount(h('ul', null, h(NotesRow, { open: false, onOpen: () => opened++ })), root);
    const row = find(root, (n) => n.attributes?.['data-notes-row'] !== undefined);
    assert.ok(row, 'the row is there');
    assert.match(row.attributes.class, /hover:bg-base-300/);
    const open = find(root, button('Notes'));
    open.dispatch('click', { target: open });
    assert.equal(opened, 1);
    mount(h('ul', null, h(NotesRow, { open: true, onOpen: () => opened++ })), root);
    assert.match(find(root, (n) => n.attributes?.['data-notes-row'] !== undefined).attributes.class, /bg-base-300/);
    mount(null, root);
});

test("a file in the member's notes says so in their list of files, and a link to a note names the notes", () => {
    assert.equal(fileWhere({ message_id: '7', channel_id: '50' }, view), 'In your notes');
    const links = { denID: HOME, view: { ...view, url: 'https://den.example.com', name: 'Home' }, dens: [{ den_id: HOME, name: 'Home', url: 'https://den.example.com' }] };
    const link = messageLink(`https://den.example.com/#m/${HOME}/50/7`);
    const where = place(link, links);
    assert.equal(where.channel?.id, '50');
    assert.equal(chipLabel(link, where, HOME), 'your notes');
    // Anyone else's, which no one else sees, reads as any channel out of sight.
    const theirs = messageLink(`https://den.example.com/#m/${HOME}/51/7`);
    assert.equal(chipLabel(theirs, place(theirs, links), HOME), '#unknown');
});

// A window for the profile card's place in the history, as the
// disappearing test's is, and a service that answers its request for the
// member's bio.
globalThis.window = {
    location: { pathname: '/den/d/10', hash: '' },
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    addEventListener() {},
    removeEventListener() {},
    history: {
        state: null,
        pushState(state) {
            this.state = state;
        },
        replaceState(state) {
            this.state = state;
        },
        go() {},
    },
};
globalThis.fetch = async () => ({ ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify({ bio: '' }) });

test("the member's own profile card opens their notes, and nobody else's has Notes", () => {
    const root = document.createElement('div');
    const [alice, bob] = view.members.map((m) => ({ ...m, role: 'member', joined_at: 1 }));
    let opened = 0;
    const card = (member, onNotes) => h(ProfileCard, { denID: 'd', member, me: alice, role: 'member', online: true, kept: null,
        onClose() {}, onMessage() {}, onNotes, onCompare() {}, onEdit() {}, onRemove() {} });
    mount(card(alice, () => opened++), root);
    const notes = find(root, button('Notes'));
    assert.ok(notes && find(root, button('Edit profile')), 'Notes beside Edit profile on her own card');
    notes.dispatch('click', { target: notes });
    assert.equal(opened, 1);
    mount(card(bob, () => opened++), root);
    assert.equal(find(root, button('Notes')), null, "another member's card has no Notes");
    mount(card(alice, null), root);
    assert.equal(find(root, button('Notes')), null, 'nor her own where the den has no notes for her Dens');
    mount(null, root);
});

