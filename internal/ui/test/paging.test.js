// The message list's run: what pages and events do to it, and where its
// far end is cut.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    MIN_PAGE, PAGE, PAGE_SCREENS, WINDOW, atTail, cutAbove, cutBelow, farIds, fromPage, mergeIn, pageSize, replayed, withDelete, withExpired, withNewer,
    withOlder, withUpdate,
} from '../assets/js/src/paging.js';

// IDs grow with time and outgrow JavaScript's numbers, so they compare as
// decimal strings of any length.
const msg = (id, extra = {}) => ({ id: String(id), revision: 1, text: `m${id}`, ...extra });
const ids = (list) => list.messages.map((m) => m.id);
const run = (from, to, extra = {}) => ({ messages: range(from, to).map((n) => msg(n)), hasOlder: true, hasNewer: false, loaded: true, ...extra });
function range(from, to) {
    const out = [];
    for (let n = from; n <= to; n++) out.push(n);
    return out;
}

test('pages merge in order, once each, by IDs of any length', () => {
    assert.deepEqual(mergeIn([msg(9), msg(11)], [msg(10), msg(9), msg(100)]).map((m) => m.id), ['9', '10', '11', '100']);
});

test("a run's far end is where it reaches past the view by more than the reach", () => {
    // Rows 100 px tall from y=0; the view shows 1000 to 1500.
    const rows = range(0, 39).map((n) => ({ id: String(n + 1), top: n * 100, bottom: n * 100 + 100 }));
    assert.deepEqual(farIds(rows, 1000, 1500, 300), { above: '6', below: '20' });
    assert.deepEqual(farIds(rows, 1000, 1500, 5000), { above: null, below: null });
    // A row that ends right at the margin isn't wholly beyond it, and stays.
    assert.deepEqual(farIds(rows, 1050, 1550, 300), { above: '7', below: '20' });
    assert.deepEqual(farIds([], 0, 100, 10), { above: null, below: null });
});

test('cuts take the far end up to and from a message, and a null cut takes nothing', () => {
    const messages = range(1, 10).map((n) => msg(n));
    assert.deepEqual(cutAbove(messages, '3').map((m) => m.id), range(4, 10).map(String));
    assert.deepEqual(cutBelow(messages, '8').map((m) => m.id), range(1, 7).map(String));
    assert.equal(cutAbove(messages, null), messages);
    assert.equal(cutBelow(messages, null), messages);
});

test('an older page lands above, and what lies far below goes, letting go of the newest', () => {
    const page = { messages: range(51, 100).map((n) => msg(n)), has_older: true };
    const kept = withOlder(run(101, 150), page, '131');
    assert.deepEqual(ids(kept), range(51, 130).map(String));
    assert.equal(kept.hasNewer, true);
    assert.equal(kept.hasOlder, true);
    // Nothing far below: the run keeps the newest.
    assert.equal(withOlder(run(101, 150), page, null).hasNewer, false);
    // However short the rows, the run holds at most WINDOW, from the top.
    const tall = withOlder(run(101, 100 + WINDOW), page, null);
    assert.equal(tall.messages.length, WINDOW);
    assert.equal(tall.messages[0].id, '51');
    assert.equal(tall.hasNewer, true);
});

test('a newer page lands below, and what lies far above goes', () => {
    const page = { messages: range(151, 200).map((n) => msg(n)), has_newer: false };
    const kept = withNewer(run(101, 150, { hasOlder: false, hasNewer: true }), page, '120');
    assert.deepEqual(ids(kept), range(121, 200).map(String));
    assert.equal(kept.hasOlder, true);
    assert.equal(kept.hasNewer, false);
    assert.equal(withNewer(run(1, PAGE, { hasOlder: false, hasNewer: true }), page, null).hasOlder, false);
});

test('a message at the live tail joins the run, which gives up what lies far above, or past WINDOW', () => {
    const kept = atTail(run(1, 150, { hasOlder: false }), msg(151), '30');
    assert.deepEqual(ids(kept), range(31, 151).map(String));
    assert.equal(kept.hasOlder, true);
    assert.equal(kept.hasNewer, false);
    const full = atTail(run(1, WINDOW, { hasOlder: false }), msg(WINDOW + 1), null);
    assert.equal(full.messages.length, WINDOW);
    assert.deepEqual([full.messages[0].id, full.messages.at(-1).id], ['2', String(WINDOW + 1)]);
    assert.equal(full.hasOlder, true);
});

test("events that came while a load was out apply again on top of its page, and twice changes nothing", () => {
    const page = fromPage({ messages: [msg(1), msg(2, { reply_to: '1', reply: { text: 'm1' } }), msg(3)], has_older: false, has_newer: false });
    const events = [
        { t: 'message.created', d: msg(4) },
        { t: 'message.updated', d: msg(1, { revision: 2, text: 'edited' }) },
        { t: 'message.deleted', d: { id: '3' } },
        { t: 'message.updated', d: msg(1, { revision: 1, text: 'stale' }) },
    ];
    const once = replayed(page, events);
    assert.deepEqual(ids(once), ['1', '2', '4']);
    assert.equal(once.messages[0].text, 'edited');
    assert.equal(once.messages[1].reply.text, 'edited');
    assert.deepEqual(replayed(once, events), once);
    // A run that doesn't hold the newest leaves new messages for its next
    // page.
    assert.deepEqual(ids(replayed({ ...page, hasNewer: true }, events.slice(0, 1))), ['1', '2', '3']);
});

test('a retention pass takes every message up to the one it names, and the quotes of them', () => {
    const list = run(8, 12);
    list.messages[3] = msg(11, { reply_to: '9', reply: { text: 'm9' } });
    list.messages[4] = msg(12, { reply_to: '7', reply: { text: 'm7' } });
    const after = withExpired(list, '10');
    assert.deepEqual(ids(after), ['11', '12']);
    assert.equal(after.messages[0].reply, undefined);
    assert.equal(after.messages[1].reply, undefined);
    // What it lost was the oldest left, so nothing older remains to load.
    assert.equal(after.hasOlder, false);
    // A run newer than the pass keeps its messages, and whatever is older.
    const newer = withExpired(run(20, 22), '10');
    assert.deepEqual(ids(newer), ['20', '21', '22']);
    assert.equal(newer.hasOlder, true);
    // IDs compare as numbers of any length, not as strings.
    assert.deepEqual(ids(withExpired(run(9, 11), '9')), ['10', '11']);
    // A pass that came while a load was out applies to its page too.
    assert.deepEqual(ids(replayed(run(1, 4), [{ t: 'messages.expired', d: { through: '2' } }])), ['3', '4']);
});

test("a deleted message leaves, and so does the quote of it in its replies", () => {
    const list = run(1, 2);
    list.messages[1] = msg(2, { reply_to: '1', reply: { text: 'm1' } });
    const after = withDelete(list, { id: '1' });
    assert.deepEqual(ids(after), ['2']);
    assert.equal(after.messages[0].reply, undefined);
    assert.equal(withUpdate(after, msg(2, { revision: 0 })), after);
});

test('a page from history asks for about three screens of messages, between 10 and 50', () => {
    // A screen of 600 px: short messages fill pages of 50 as before.
    assert.equal(pageSize(25, 600), PAGE);
    // Six lines of text, 169 px each: 1,800 px of them.
    assert.equal(pageSize(169, 600), Math.round((PAGE_SCREENS * 600) / 169));
    assert.equal(pageSize(169, 600), 11);
    // Photos, 340 px each, would be five a page; ten keeps reading back moving.
    assert.equal(pageSize(340, 600), MIN_PAGE);
    // Nothing laid out to go by: a full page.
    for (const [h, s] of [[0, 600], [NaN, 600], [25, 0]]) assert.equal(pageSize(h, s), PAGE);
});
