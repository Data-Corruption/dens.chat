import assert from 'node:assert/strict';
import { test } from 'node:test';
import { NO_DM, denChannel, foldedShows, hiddenMarks, lastDM, listed, readFolded, recentDMs } from '../assets/js/src/chat.jsx';
import { dividerAt } from '../assets/js/src/messages.jsx';

const view = {
    groups: [{ id: 'g2', name: 'Later', position: 1 }, { id: 'g1', name: 'Games', position: 0 }],
    channels: [
        { id: '30', kind: 'text', name: 'chess', group_id: 'g2', position: 0 },
        { id: '20', kind: 'voice', name: 'lounge', group_id: 'g1', position: 1 },
        { id: '21', kind: 'text', name: 'clips', group_id: 'g1', position: 0 },
        { id: '11', kind: 'text', name: 'rules', position: 1 },
        { id: '10', kind: 'voice', name: 'hangout', position: 0 },
        { id: '90', kind: 'dm', members: ['1', '2'] },
    ],
};

test('the channel list shows channels in no group first, then each group by position, without DMs', () => {
    const { ungrouped, groups } = listed(view);
    assert.deepEqual(ungrouped.map((c) => c.id), ['10', '11']);
    assert.deepEqual(groups.map((g) => [g.group.id, g.channels.map((c) => c.id)]), [['g1', ['21', '20']], ['g2', ['30']]]);
});

test("the Den tab opens the den's last text channel, or the list's first", () => {
    assert.equal(denChannel(view, '30').id, '30');
    assert.equal(denChannel(view, '').id, '11', 'the first text channel as the list shows it');
    assert.equal(denChannel(view, '99').id, '11', 'one gone, or out of sight');
    assert.equal(denChannel(view, '20').id, '11', 'a voice channel is joined, not opened');
    assert.equal(denChannel(view, '90').id, '11', 'a DM is not a den channel');
    assert.equal(denChannel({ groups: [], channels: [] }, ''), null);
});

test('the browser keeps folded groups by ID, and reads anything else as none', () => {
    assert.deepEqual([...readFolded('["g1","g2"]')], ['g1', 'g2']);
    assert.deepEqual([...readFolded('')], []);
    assert.deepEqual([...readFolded('{')], []);
    assert.deepEqual([...readFolded('{"g1":true}')], []);
    assert.deepEqual([...readFolded('["g1",2,null]')], ['g1']);
});

test('a folded group shows the open channel and calls in progress, and marks what it hides', () => {
    const channels = listed(view).groups[0].channels;
    const calls = new Map([['20', { channel_id: '20', members: [{ id: '1' }] }]]);
    assert.deepEqual(foldedShows(channels, '', new Map()).map((c) => c.id), []);
    assert.deepEqual(foldedShows(channels, '21', new Map()).map((c) => c.id), ['21']);
    assert.deepEqual(foldedShows(channels, '', calls).map((c) => c.id), ['20']);
    assert.deepEqual(foldedShows(channels, '', new Map([['20', { channel_id: '20', members: [] }]])), [], 'a call that emptied');

    const reads = new Map([
        ['21', { channel_id: '21', message_id: '5', last_message_id: '7', mention_count: 2 }],
        ['30', { channel_id: '30', message_id: '9', last_message_id: '9', mention_count: 0 }],
    ]);
    assert.deepEqual(hiddenMarks(channels, reads), { unread: true, mentions: 2 });
    assert.deepEqual(hiddenMarks(listed(view).groups[1].channels, reads), { unread: false, mentions: 0 });
    assert.deepEqual(hiddenMarks([], reads), { unread: false, mentions: 0 });
});

test('a channel opened with unread messages gets the New divider where they start, and one opened read gets none', () => {
    assert.equal(dividerAt('100', '105'), '100');
    assert.equal(dividerAt('105', '105'), '', 'nothing unread: messages that arrive while it is open get none');
    assert.equal(dividerAt('', '105'), '', 'never read');
    assert.equal(dividerAt(undefined, undefined), '');
    assert.equal(dividerAt('99', '100'), '99', 'IDs compare as numbers, not text');
});

test('the DM list puts the most recently active first, and a closed DM keeps out unless it is open', () => {
    const dms = {
        groups: [],
        channels: [
            { id: '40', kind: 'dm', members: ['1', '2'] },
            { id: '41', kind: 'dm', members: ['1', '3'] },
            { id: '42', kind: 'dm', members: ['1', '4'] },
            { id: '43', kind: 'dm', members: ['1', '5'] },
            { id: '10', kind: 'text', name: 'general', position: 0 },
        ],
    };
    const reads = new Map([
        ['40', { channel_id: '40', last_message_id: '100' }],
        ['41', { channel_id: '41', last_message_id: '300' }],
        ['42', { channel_id: '42', last_message_id: '200', closed: true }],
    ]);
    assert.deepEqual(recentDMs(dms, reads, '').map((c) => c.id), ['41', '40', '43'], 'by the newest message, then the newest DM, a closed one out');
    assert.deepEqual(recentDMs(dms, reads, '42').map((c) => c.id), ['41', '42', '40', '43'], 'a closed DM that is open stays in its place');
    assert.equal(NO_DM, 'dms', "the address's place for no DM, which the page's router takes");
});

test('the Direct messages tab opens the DM last open, never another, or none', () => {
    const view = {
        groups: [],
        channels: [{ id: '40', kind: 'dm', members: ['1', '2'] }, { id: '41', kind: 'dm', members: ['1', '3'] }, { id: '10', kind: 'text', name: 'general', position: 0 }],
    };
    const reads = new Map([
        ['40', { channel_id: '40', last_message_id: '100' }],
        ['41', { channel_id: '41', last_message_id: '300' }],
    ]);
    assert.equal(lastDM(view, reads, '40').id, '40', 'the one last open, though another is more recent');
    assert.equal(lastDM(view, reads, ''), null, 'none open before: none, not the most recent');
    assert.equal(lastDM(view, reads, '99'), null, 'one gone');
    assert.equal(lastDM(view, reads, '10'), null, 'a den channel is no DM');
    reads.set('40', { channel_id: '40', last_message_id: '100', closed: true });
    assert.equal(lastDM(view, reads, '40'), null, 'one closed since');
});
