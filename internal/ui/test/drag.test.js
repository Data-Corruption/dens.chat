import assert from 'node:assert/strict';
import { test } from 'node:test';
import { DRAG_CHANNEL, DRAG_GROUP, DRAG_MEMBER, channelDrop, dragged, groupDrop, moveTargets } from '../assets/js/src/drag.js';

// A den's list: general and Lounge in no group, Talk's news and its
// staff-only Staff, and Games' Raid; and a DM, which no list holds.
const view = {
    groups: [{ id: 'g2', name: 'Games', position: 1 }, { id: 'g1', name: 'Talk', position: 0 }],
    channels: [
        { id: '2', kind: 'voice', name: 'Lounge', position: 1 },
        { id: '1', kind: 'text', name: 'general', position: 0 },
        { id: '3', kind: 'text', name: 'news', group_id: 'g1', position: 0 },
        { id: '4', kind: 'voice', name: 'Staff', group_id: 'g1', position: 1, staff_only: true },
        { id: '5', kind: 'voice', name: 'Raid', group_id: 'g2', position: 0 },
        { id: '9', kind: 'dm', name: '', position: 0, members: ['7', '8'] },
    ],
};

test('a channel dropped in its own list sends its place there, and one dropped where it is sends nothing', () => {
    assert.deepEqual(channelDrop(view, '1', { group: '', near: '2', after: true }), { position: 1 });
    assert.deepEqual(channelDrop(view, '2', { group: '', near: '1' }), { position: 0 });
    assert.deepEqual(channelDrop(view, '4', { group: 'g1', near: '3' }), { position: 0 });
    // Just before the next, or just after the one before, is where it is.
    assert.equal(channelDrop(view, '1', { group: '', near: '2' }), null);
    assert.equal(channelDrop(view, '2', { group: '', near: '1', after: true }), null);
    assert.equal(channelDrop(view, '1', { group: '', near: '1', after: true }), null);
});

test('a channel dropped in another group, or on its heading, sends the group with its place', () => {
    assert.deepEqual(channelDrop(view, '1', { group: 'g1', near: '3', after: true }), { position: 1, group_id: 'g1' });
    assert.deepEqual(channelDrop(view, '3', { group: '', near: '2', after: true }), { position: 2, group_id: '' });
    assert.deepEqual(channelDrop(view, '2', { group: 'g2' }), { position: 0, group_id: 'g2' });
    // On its own group's heading, it goes first.
    assert.deepEqual(channelDrop(view, '4', { group: 'g1' }), { position: 0 });
    assert.equal(channelDrop(view, '3', { group: 'g1' }), null);
    // A DM isn't in the list, and a place beside a channel that's gone isn't one.
    assert.equal(channelDrop(view, '9', { group: '' }), null);
    assert.equal(channelDrop(view, '1', { group: 'g2', near: '404' }), null);
});

test('a group dropped beside another sends its place among the groups', () => {
    assert.deepEqual(groupDrop(view, 'g1', { near: 'g2', after: true }), { position: 1 });
    assert.deepEqual(groupDrop(view, 'g2', { near: 'g1' }), { position: 0 });
    assert.equal(groupDrop(view, 'g1', { near: 'g2' }), null);
    assert.equal(groupDrop(view, 'g1', { near: 'g1' }), null);
});

test('Move to lists the voice channels a member could join but their own, in the list\'s order', () => {
    const names = (list) => list.map((c) => c.name);
    assert.deepEqual(names(moveTargets(view, { role: 'member' }, '2')), ['Raid']);
    assert.deepEqual(names(moveTargets(view, { role: 'moderator' }, '2')), ['Staff', 'Raid']);
    assert.deepEqual(names(moveTargets(view, { role: 'owner' }, '4')), ['Lounge', 'Raid']);
    assert.deepEqual(names(moveTargets(view, undefined, '5')), ['Lounge']);
});

test("a drag says what it carries by its types, and anything else, as files, is none of Dens's", () => {
    const drag = (...types) => ({ dataTransfer: { types } });
    assert.equal(dragged(drag(DRAG_CHANNEL)), 'channel');
    assert.equal(dragged(drag('text/plain', DRAG_GROUP)), 'group');
    assert.equal(dragged(drag(DRAG_MEMBER)), 'member');
    assert.equal(dragged(drag('Files')), '');
    assert.equal(dragged({}), '');
});

test("Move up and Move down send a channel's place one up or down in its list, a group's among the groups, and nothing at an end", async () => {
    const { channelMoves, groupMoves } = await import('../assets/js/src/drag.js');
    assert.deepEqual(channelMoves(view, '1'), { up: null, down: { position: 1 } }, 'general, first of the ungrouped');
    assert.deepEqual(channelMoves(view, '2'), { up: { position: 0 }, down: null }, 'Lounge, last of them');
    assert.deepEqual(channelMoves(view, '4'), { up: { position: 0 }, down: null }, "Staff, in Talk");
    assert.deepEqual(channelMoves(view, '5'), { up: null, down: null }, "Raid, alone in Games");
    assert.deepEqual(channelMoves(view, '9'), { up: null, down: null }, 'a DM moves nowhere');
    assert.deepEqual(groupMoves(view, 'g1'), { up: null, down: { position: 1 } });
    assert.deepEqual(groupMoves(view, 'g2'), { up: { position: 0 }, down: null });
});
