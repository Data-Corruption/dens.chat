// Reactions (M15.3), checked without a browser: the chips a message shows,
// each event applied to its emoji's set, so applying one twice changes
// nothing; a DM's sets kept by their revision; what an edit's answer keeps;
// a load's replay; the tip that says who; a message's room; and the quick
// row while the den's emoji are off.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { QUICK, denKey, noteUses, quickRow, setEmojiList, unicodeKey } from '../assets/js/src/emoji.js';
import { replayed, withUpdate } from '../assets/js/src/paging.js';
import { MAX_EMOJI, chipsOf, keptReactions, reacted, room, whoReacted, withReaction } from '../assets/js/src/reactions.js';
import list from '../assets/emoji.json';

setEmojiList(list);

const shown = (chips) => chips.map((c) => `${c.emoji || (c.emoji_id ? `#${c.emoji_id}` : `:${c.name}:`)}${c.members.join(',')}${c.mine ? '*' : ''}`);
const message = (reactions) => ({ id: '5', channel_id: '2', author_id: '1', revision: 1, text: 'hi', reactions });

test("a channel's chips show the den's reactions in order, the member's marked, a deleted emoji's gone", () => {
    const m = message([
        { emoji: '👍', members: ['3', '4'] },
        { emoji_id: '12', members: ['4'] },
        { emoji_id: '99', members: ['3'] },
        { emoji: '🎉', members: [] },
    ]);
    const den = new Map([['12', { id: '12', name: 'party' }]]);
    assert.deepEqual(shown(chipsOf(m, '4', den)), ['👍3,4*', '#124*']);
    assert.deepEqual(shown(chipsOf(m, '3', null)), ['👍3,4*', '#124', '#993*'], "an older den's page goes by what it's sent");
    assert.deepEqual(chipsOf({ ...m, reactions: undefined }, '3', den), []);
});

test("a DM's chips join its members' sets, the lower ID's first, each emoji once, the den's by name", () => {
    const m = {
        id: '8', channel_id: '3', revision: 1, text: 'hi',
        dm_reactions: [{ member_id: '10', revision: 2, emoji: ['😂', 'party'] }, { member_id: '9', revision: 1, emoji: ['👍', '😂'] }],
    };
    assert.deepEqual(shown(chipsOf(m, '10', new Map())), ['👍9', '😂9,10*', ':party:10*']);
});

test('an event applies to its emoji\'s set: twice changes nothing, and the last member out takes the chip', () => {
    let m = message([]);
    const added = (member, emoji) => ({ message_id: '5', channel_id: '2', member_id: member, emoji });
    const steps = [
        ['reaction.added', added('3', '👍'), ['👍3']],
        ['reaction.added', added('3', '👍'), ['👍3']],
        ['reaction.added', { message_id: '5', channel_id: '2', member_id: '4', emoji_id: '12' }, ['👍3', '#124']],
        ['reaction.added', added('4', '👍'), ['👍3,4', '#124']],
        ['reaction.removed', added('9', '👍'), ['👍3,4', '#124']],
        ['reaction.removed', added('3', '👍'), ['👍4', '#124']],
        ['reaction.cleared', { message_id: '5', channel_id: '2', emoji_id: '12', members: ['4', '7'] }, ['👍4']],
        ['reaction.removed', added('4', '👍'), []],
    ];
    for (const [t, d, want] of steps) {
        const before = m;
        m = reacted(m, t, d);
        assert.deepEqual(shown(chipsOf(m, '', null)), want, `${t} ${JSON.stringify(d)}`);
        if (JSON.stringify(before) === JSON.stringify(m)) assert.equal(m, before, 'an event that changes nothing leaves the message as it was');
    }
    let full = message([]);
    for (let i = 0; i <= MAX_EMOJI; i++) full = reacted(full, 'reaction.added', { message_id: '5', member_id: '3', emoji_id: String(i + 1) });
    assert.equal(full.reactions.length, MAX_EMOJI, 'a message holds 20 different emoji');
});

test("a DM's set replaces the member's where it's newer", () => {
    let m = { id: '8', revision: 1, dm_reactions: [] };
    const set = (member, revision, emoji) => ({ message_id: '8', channel_id: '3', member_id: member, revision, emoji });
    m = reacted(m, 'dm.reactions', set('9', 2, ['👍']));
    const held = m;
    assert.equal(reacted(m, 'dm.reactions', set('9', 1, ['🎉'])), held, 'an older set changes nothing');
    assert.equal(reacted(m, 'dm.reactions', set('9', 2, ['🎉'])), held, 'nor one of the same revision');
    m = reacted(m, 'dm.reactions', set('9', 3, []));
    m = reacted(m, 'dm.reactions', set('10', 1, ['😂']));
    assert.deepEqual(shown(chipsOf(m, '9', null)), ['😂10']);
});

test("an edit's answer keeps the reactions held at its revision, and an event brings the den's", () => {
    const held = { ...message([{ emoji: '👍', members: ['3', '4'] }]), dm_reactions: undefined };
    const answer = message([{ emoji: '👍', members: ['3'] }]);
    assert.deepEqual(keptReactions(held, answer, true).reactions, held.reactions, 'an answer at the revision held');
    assert.deepEqual(keptReactions(held, answer, false).reactions, answer.reactions, 'an event at it, as after being away');
    const edited = { ...answer, revision: 2, text: 'edited' };
    assert.deepEqual(keptReactions(held, edited, true).reactions, answer.reactions, 'a newer revision brings its own');
    const dm = (sets) => ({ id: '8', revision: 1, text: 'x', dm_reactions: sets });
    const merged = keptReactions(dm([{ member_id: '9', revision: 3, emoji: ['👍'] }]), dm([{ member_id: '9', revision: 2, emoji: [] }, { member_id: '10', revision: 1, emoji: ['😂'] }]), false);
    assert.deepEqual(merged.dm_reactions.map((s) => `${s.member_id}@${s.revision}`).sort(), ['10@1', '9@3']);

    const run = { messages: [held], hasOlder: false, hasNewer: false, loaded: true };
    assert.deepEqual(withUpdate(run, answer, true).messages[0].reactions, held.reactions);
    assert.deepEqual(withUpdate(run, answer).messages[0].reactions, answer.reactions);
});

test("a load's replay applies the reactions' events that came while it was out", () => {
    const page = { messages: [message([{ emoji: '👍', members: ['3'] }])], hasOlder: false, hasNewer: false, loaded: true };
    const events = [
        { t: 'reaction.added', d: { message_id: '5', channel_id: '2', member_id: '3', emoji: '👍' } },
        { t: 'reaction.added', d: { message_id: '5', channel_id: '2', member_id: '4', emoji: '🎉' } },
        { t: 'reaction.removed', d: { message_id: '5', channel_id: '2', member_id: '3', emoji: '👍' } },
        { t: 'reaction.added', d: { message_id: '6', channel_id: '2', member_id: '4', emoji: '🎉' } },
    ];
    assert.deepEqual(shown(chipsOf(replayed(page, events).messages[0], '', null)), ['🎉4']);
    assert.equal(withReaction(page, 'reaction.added', events[3].d), page, "another message's event leaves the run as it was");
});

test('a tip says who reacted: You first, then names, up to 15, then how many more', () => {
    const nameOf = (id) => `M${id}`;
    assert.equal(whoReacted(['3'], '3', nameOf), 'You');
    assert.equal(whoReacted(['4', '3'], '3', nameOf), 'You and M4');
    assert.equal(whoReacted(['4', '5', '6'], '3', nameOf), 'M4, M5 and M6');
    const many = Array.from({ length: 20 }, (_, i) => String(i + 1));
    assert.equal(whoReacted(many, '20', nameOf), `You, ${many.slice(0, 14).map(nameOf).join(', ')} and 5 more`);
});

test("a message has room for an emoji it holds, or a new one under 20", () => {
    const chips = Array.from({ length: MAX_EMOJI }, (_, i) => ({ key: `d:${i}` }));
    assert.equal(room(chips, 'd:3'), true);
    assert.equal(room(chips, 'u:👍'), false);
    assert.equal(room(chips.slice(1), 'u:👍'), true);
});

test("the quick row leaves the den's emoji out while they're off, and keeps their scores", () => {
    const kept = new Map();
    globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, String(v)) };
    const WAVE = { id: '2', name: 'wave', image: { id: '92', width: 64, height: 64 } };
    const t = 1_000_000_000_000;
    noteUses([denKey('d1', WAVE.id), denKey('d1', WAVE.id), unicodeKey('🔥')], t);
    const row = (on) => quickRow('d1', [WAVE], t, undefined, on).map((x) => (x.den ? `:${x.den.name}:` : x.unicode.emoji));
    assert.deepEqual(row(true), [':wave:', '🔥', ...QUICK.slice(0, 4)]);
    assert.deepEqual(row(false), ['🔥', ...QUICK.slice(0, 5)]);
    assert.deepEqual(row(true)[0], ':wave:', 'its score stays while off');
    delete globalThis.localStorage;
});
