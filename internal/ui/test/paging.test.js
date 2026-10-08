// The message list's run: what pages and events do to it.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fromPage, mergeIn, replayed, withDelete, withUpdate } from '../assets/js/src/paging.js';

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

test("a deleted message leaves, and so does the quote of it in its replies", () => {
    const list = run(1, 2);
    list.messages[1] = msg(2, { reply_to: '1', reply: { text: 'm1' } });
    const after = withDelete(list, { id: '1' });
    assert.deepEqual(ids(after), ['2']);
    assert.equal(after.messages[0].reply, undefined);
    assert.equal(withUpdate(after, msg(2, { revision: 0 })), after);
});
