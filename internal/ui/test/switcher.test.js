// The quick switcher's list across two dens: what it matches, by a name's
// start or a word's, and in what order, the unread first and the open
// den's before the other's; and before anything is typed, the unread
// alone.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { switcherList } from '../src/core/switcher.js';

const HOME = 'home';
const OTHER = 'other';
const dens = [
    { den_id: HOME, name: 'Home Den', unread: 1, mentions: 0 },
    { den_id: OTHER, name: 'Game Club', unread: 2, mentions: 1 },
];
const read = (id, last, at = last, mentions = 0) => ({ channel_id: id, last_message_id: last, message_id: at, mention_count: mentions });
const home = {
    me: { id: '1' },
    members: [{ id: '1', username: 'alice', display_name: 'Alice' }, { id: '2', username: 'bob', display_name: 'Bob Stone' }],
    groups: [{ id: 'g1', name: 'Games', position: 0 }],
    channels: [
        { id: '10', kind: 'text', name: 'general', position: 0 },
        { id: '11', kind: 'voice', name: 'Lounge', position: 1 },
        { id: '12', kind: 'text', name: 'game night', position: 0, group_id: 'g1' },
        { id: '90', kind: 'dm', members: ['1', '2'] },
        { id: '95', kind: 'notes', members: ['1'] },
    ],
    read_states: [read('10', '5'), read('12', '9', '3'), read('90', '7')],
};
const other = {
    me: { id: '7' },
    members: [{ id: '7', username: 'alice', display_name: 'Alice' }, { id: '8', username: 'gabe', display_name: 'Gabe' }, { id: '9', username: 'zed', display_name: 'Zed' }],
    groups: [],
    channels: [
        { id: '20', kind: 'text', name: 'general', position: 0 },
        { id: '21', kind: 'text', name: 'Galleries', position: 1 },
        { id: '30', kind: 'dm', members: ['7', '8'] },
    ],
    read_states: [read('20', '4', '1', 1), read('21', '2'), read('30', '6', '5')],
};
const views = new Map([[HOME, home], [OTHER, other]]);
const names = (list) => list.map((e) => `${e.den === HOME ? 'home' : 'other'}:${e.kind}:${e.name}`);

test('before anything is typed it lists the unread alone, the open den\'s first', () => {
    assert.deepEqual(names(switcherList(dens, views, HOME, '')), ['home:text:game night', 'other:text:general', 'other:dm:Gabe']);
    assert.deepEqual(names(switcherList(dens, views, OTHER, '  ')), ['other:text:general', 'other:dm:Gabe', 'home:text:game night']);
});

test('a few letters match the start of a name or of a word in it, across dens, the unread first', () => {
    // A den with something unread is among the unread.
    assert.deepEqual(names(switcherList(dens, views, HOME, 'ga')), [
        'home:text:game night', 'other:den:Game Club', 'other:dm:Gabe', 'other:text:Galleries',
    ]);
    assert.deepEqual(names(switcherList(dens, views, HOME, 'gen')), ['other:text:general', 'home:text:general']);
    assert.deepEqual(names(switcherList(dens, views, HOME, 'NIGHT')), ['home:text:game night']);
    // A DM by its member's display name or username, and a den by its name.
    assert.deepEqual(names(switcherList(dens, views, HOME, 'stone')), ['home:dm:Bob Stone']);
    assert.deepEqual(names(switcherList(dens, views, HOME, 'bo')), ['home:dm:Bob Stone']);
    assert.deepEqual(names(switcherList(dens, views, HOME, 'home')), ['home:den:Home Den']);
    // A voice channel, which Enter joins; never the member's notes.
    assert.deepEqual(names(switcherList(dens, views, HOME, 'lou')), ['home:voice:Lounge']);
    assert.deepEqual(switcherList(dens, views, HOME, 'notes'), []);
});

test("a den whose state couldn't be fetched is listed by its name alone", () => {
    const some = new Map([[HOME, home], [OTHER, null]]);
    assert.deepEqual(names(switcherList(dens, some, HOME, 'g')), ['home:text:game night', 'other:den:Game Club', 'home:text:general']);
});

test('each entry says where it opens and what waits in it', () => {
    const [night] = switcherList(dens, views, HOME, 'night');
    assert.deepEqual({ den: night.den, id: night.id, unread: night.unread, mentions: night.mentions, denName: night.denName },
        { den: HOME, id: '12', unread: true, mentions: 0, denName: 'Home Den' });
    const [general] = switcherList(dens, views, HOME, 'general');
    assert.equal(general.mentions, 1);
    const [dm] = switcherList(dens, views, HOME, 'bob');
    assert.equal(dm.detail, '@bob');
    const keys = switcherList(dens, views, HOME, 'g').map((e) => e.key);
    assert.equal(new Set(keys).size, keys.length);
});

test('switcher includes personal and visible channel collections by name and channel name', () => {
    const withCols = new Map([
        [HOME, {
            ...home,
            collections: [
                { id: '1', name: 'Saved', count: 2, created_at: 10 },
                { id: '2', channel_id: '10', name: 'Raid Guides', who_adds: 'everyone', count: 3, created_at: 20 },
                { id: '3', channel_id: '999', name: 'Hidden Col', who_adds: 'staff', count: 1, created_at: 30 },
            ],
        }],
        [OTHER, other],
    ]);
    assert.deepEqual(switcherList(dens, withCols, HOME, 'sav').map((e) => [e.kind, e.id, e.detail]), [
        ['collection', '1', 'Personal'],
    ]);
    assert.deepEqual(switcherList(dens, withCols, HOME, 'raid').map((e) => [e.kind, e.id, e.channelId, e.detail]), [
        ['channel_collection', '2', '10', '#general'],
    ]);
    assert.deepEqual(switcherList(dens, withCols, HOME, 'hidden'), [], 'collections of channels not in view stay hidden');
});
