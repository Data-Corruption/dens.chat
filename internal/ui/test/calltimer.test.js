// The call timer: a call's length as it shows on its channel's row,
// the rainbow while it beats the channel's record, the fireworks once as
// it passes it, and Appearance's Call timer, which hides it all.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { beating, callLength, callTimer, crossed, setCallTimer } from '../assets/js/src/calltimer.js';
import { CallLength } from '../assets/js/src/calltimer.jsx';

const kept = new Map();
globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, String(v)), removeItem: (k) => kept.delete(k) };

const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function find(node, match) {
    if (match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}

const marked = (name) => (n) => n.attributes?.[name] !== undefined;

test('a length shows as m:ss, then h:mm:ss from an hour', () => {
    assert.equal(callLength(0), '0:00');
    assert.equal(callLength(59_999), '0:59');
    assert.equal(callLength(61_000), '1:01');
    assert.equal(callLength(3_599_000), '59:59');
    assert.equal(callLength(3_600_000), '1:00:00');
    assert.equal(callLength(3_723_000), '1:02:03');
    assert.equal(callLength(100 * 3_600_000), '100:00:00');
    assert.equal(callLength(-5), '0:00', 'a start a moment ahead of the clock');
});

test('a call beats a record it passed, and with no record has nothing to beat', () => {
    const call = { started_at: 1_000, record_ms: 10_000 };
    assert.equal(beating(call, 11_000), false, 'as long as the record');
    assert.equal(beating(call, 11_001), true, 'past it');
    assert.equal(beating({ started_at: 1_000, record_ms: 0 }, 1_000_000), false, 'no record');
    assert.equal(beating({ started_at: 1_000 }, 1_000_000), false, 'from a den that gives none');
});

test('the fireworks go off once, as the call passes its record, and not for a page that looks once it passed', () => {
    const call = { started_at: 0, record_ms: 10_000 };
    assert.equal(crossed(call, 9_800, 10_050), true, 'seen under it, then past it');
    assert.equal(crossed(call, 10_050, 10_300), false, 'past it already');
    assert.equal(crossed(call, null, 10_050), false, 'a page that opens past it');
    assert.equal(crossed(call, 9_000, 9_800), false, 'not there yet');
    assert.equal(crossed({ started_at: 0, record_ms: 0 }, 9_800, 10_050), false, 'no record');
});

test("the row shows the length, the rainbow past the record and fireworks as it passes, and nothing with the timer off", async () => {
    kept.clear();
    const now = Date.now();
    const crossing = { channel_id: '41', started_at: now - 9_900, record_ms: 10_000, members: [{ id: '1' }] };
    const past = { channel_id: '42', started_at: now - 20_000, record_ms: 10_000, members: [{ id: '1' }] };
    const first = { channel_id: '43', started_at: now - 20_000, record_ms: 0, members: [{ id: '1' }] };
    const older = { channel_id: '44', members: [{ id: '1' }] };
    const roots = [crossing, past, first, older].map((call) => {
        const root = document.createElement('div');
        mount(h(CallLength, { call }), root);
        return root;
    });
    // Each length listens to the page's one timer until it goes, which a
    // failed assertion mustn't leave running.
    try {
        await rows(roots);
    } finally {
        for (const root of roots) mount(null, root);
    }
});

async function rows([a, b, c, d]) {
    assert.equal(find(a, marked('data-call-length'))?.textContent.trim(), '0:09', 'under its record');
    assert.equal(find(a, marked('data-beating')), null);
    assert.equal(d.textContent, '', 'a den older than the timer gives no length');
    await wait(600);
    assert.ok(find(a, marked('data-beating')), 'the rainbow past the record');
    assert.ok(find(a, marked('data-fireworks')), 'fireworks as it passes');
    assert.ok(find(b, marked('data-beating')), 'the rainbow for a page that opens past it');
    assert.equal(find(b, marked('data-fireworks')), null, 'but no fireworks');
    assert.equal(find(c, marked('data-call-length'))?.textContent.trim(), '0:20');
    assert.equal(find(c, marked('data-beating')), null, 'a first call has nothing to beat');
    assert.match(c.textContent, /No longest call yet/, "the length's tip says there's no record");
    assert.match(a.textContent, /Longest call: 0:10/, "the length's tip names the record");

    setCallTimer(false);
    assert.equal(callTimer(), false);
    assert.equal(kept.get('DENS_CALL_TIMER'), 'off', 'the browser keeps it');
    await wait(10);
    for (const root of [a, b, c, d]) assert.equal(root.textContent, '', 'nothing with the timer off');
    setCallTimer(true);
    await wait(10);
    assert.ok(find(b, marked('data-call-length')), 'back once it is on');
    assert.equal(kept.has('DENS_CALL_TIMER'), false);
}

test('a record reset as the fireworks play takes them off, and the rainbow with them', async () => {
    kept.clear();
    const root = document.createElement('div');
    const call = { channel_id: '45', started_at: Date.now() - 9_900, record_ms: 10_000, members: [{ id: '1' }] };
    mount(h(CallLength, { call }), root);
    try {
        await wait(600);
        assert.ok(find(root, marked('data-fireworks')), 'fireworks as it passes');
        mount(h(CallLength, { call: { ...call, record_ms: 0 } }), root);
        await wait(300);
        assert.equal(find(root, marked('data-fireworks')), null, 'none once the record is reset');
        assert.equal(find(root, marked('data-beating')), null, 'nothing to beat');
    } finally {
        mount(null, root);
    }
});
