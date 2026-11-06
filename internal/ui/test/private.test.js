import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { LOCKED, dividerText, dmGate, keyChanges, keysChangedAt, sameDigits } from '../assets/js/src/private.js';
import { Codes, DMGate, KeysChanged } from '../assets/js/src/private.jsx';
import { keysChanged } from '../assets/js/src/calllock.jsx';

const names = (id) => ({ 1: 'Alice', 2: 'Bob' })[id] || 'Unknown member';
const day = Date.UTC(2026, 8, 1);

test('a divider marks where a DM moves to a newer key from before M16.1, and says why', () => {
    const keys = [
        { id: '10', retired: 'started_over', retired_by: '2', retired_at: day },
        { id: '11', retired: 'restarted', retired_by: '1', retired_at: day, checks: [{ member_id: '1', at: day }] },
        { id: '12', checks: [{ member_id: '1', at: day }, { member_id: '2', at: day + 1000 }] },
    ];
    const messages = [
        { id: '1', key_id: '10' },
        { id: '2', key_id: '10' },
        { id: '3', key_id: '12' },
        { id: '4' }, // not a DM message
        { id: '5', key_id: '12' },
    ];
    const changes = keyChanges(messages, keys, names, '1');
    assert.deepEqual([...changes.keys()], ['3']);
    assert.match(changes.get('3'), /^Bob started over on .+\. You checked codes again on .+\.$/);
    assert.match(dividerText(keys[1], keys[2], names, '1'), /^Alice started the check again on /);
    assert.equal(dividerText(undefined, { id: '9' }, names, '1'), 'The keys changed.');
});

test("from M16.1 a key's change has a divider only where this member started over", () => {
    const legacy = { id: '10', retired: 'restarted', retired_by: '1', retired_at: day };
    const signed = { id: '11', signed: true, retired: 'restarted', retired_by: '2', retired_at: day };
    const mine = { id: '12', signed: true, retired: 'started_over', retired_by: '1', retired_at: day };
    const theirs = { id: '13', signed: true, retired: 'started_over', retired_by: '2', retired_at: day };
    const next = { id: '14', signed: true };
    // The update retired an exchange one member alone had checked.
    assert.equal(dividerText(legacy, next, names, '1'), 'The keys changed.');
    assert.match(dividerText({ ...legacy, retired: 'started_over', retired_by: '2' }, next, names, '1'), /^Bob started over on /);
    // A change of the other's identity has a line of its own.
    assert.equal(dividerText(signed, next, names, '1'), '');
    assert.equal(dividerText(theirs, next, names, '1'), '');
    assert.match(dividerText(mine, next, names, '1'), /^You started over on /);
    const messages = [{ id: '1', key_id: '11' }, { id: '2', key_id: '14' }];
    assert.equal(keyChanges(messages, [signed, next], names, '1').size, 0);
});

test("a member's keys changing shows before the first message after it, or at the end", () => {
    const messages = [{ id: '1', created_at: 100 }, { id: '2', created_at: 200 }, { id: '3', created_at: 300 }];
    let at = keysChangedAt(messages, [250, 150, 400]);
    assert.deepEqual([...at.before.entries()], [['2', [150]], ['3', [250]]]);
    assert.deepEqual(at.after, [400]);
    // Only once the list holds the newest does one after the last show.
    at = keysChangedAt(messages, [400], { hasNewer: true });
    assert.deepEqual(at.after, []);
    // One from before the first message loaded waits for the start.
    at = keysChangedAt(messages, [50], { hasOlder: true });
    assert.equal(at.before.size, 0);
    at = keysChangedAt(messages, [50]);
    assert.deepEqual([...at.before.entries()], [['1', [50]]]);
    assert.deepEqual(keysChangedAt([], [10]).after, [10]);
    assert.equal(keysChangedAt(messages, undefined).before.size, 0);
});

test('a DM takes messages at once, unless something stands in its composer', () => {
    const channel = { id: '5', kind: 'dm', members: ['1', '2'] };
    const view = {
        me: { id: '1' }, seal: true, keeps_identities: true, identities: {}, dm_keys: [],
        members: [{ id: '1', identity: 'a' }, { id: '2', identity: 'b' }],
    };
    assert.equal(dmGate(view, channel), '');
    assert.equal(dmGate({ ...view, seal: false }, channel), 'no_seal');
    assert.equal(dmGate({ ...view, identities: { 2: { held: true } } }, channel), 'held');
    assert.equal(dmGate({ ...view, members: [{ id: '1', identity: 'a' }, { id: '2' }] }, channel), 'update');
    assert.equal(dmGate({ ...view, keeps_identities: false }, channel), 'den');
    // A key both checked before M16.1 goes on as it did, on any den.
    const checked = [{ id: '9', channel_id: '5', checks: [{ member_id: '1' }, { member_id: '2' }] }];
    assert.equal(dmGate({ ...view, keeps_identities: false, dm_keys: checked }, channel), '');
    assert.equal(dmGate({ ...view, members: [{ id: '1' }, { id: '2' }], dm_keys: checked }, channel), '');
    const half = [{ id: '9', channel_id: '5', checks: [{ member_id: '1' }] }];
    assert.equal(dmGate({ ...view, keeps_identities: false, dm_keys: half }, channel), 'den');
});

test('typed digits compare without their spaces and dashes', () => {
    assert.equal(sameDigits('1234 5678-9012 3456 7890', '12345678901234567890'), true);
    assert.equal(sameDigits('1234 5678 9012 3456 7891', '1234 5678 9012 3456 7890'), false);
    assert.equal(sameDigits('', ''), false);
});

test('every reason a message stays sealed has words for it', () => {
    for (const reason of ['no_seal', 'unchecked', 'lost', 'broken', 'keys']) assert.ok(LOCKED[reason]);
});

// service answers the page's requests as the local service would, by
// method and path, and records them with their bodies.
function service(answers) {
    const asked = [];
    globalThis.fetch = async (path, opts = {}) => {
        const method = opts.method || 'GET';
        const body = opts.body ? JSON.parse(opts.body) : undefined;
        asked.push({ method, path, body });
        let answer = answers[`${method} ${path}`];
        let status = 200;
        if (typeof answer === 'function') {
            try {
                answer = answer(body);
            } catch (e) {
                [status, answer] = [400, { error: e.message }];
            }
        }
        if (answer === undefined) [status, answer] = [404, { error: 'no such thing' }];
        return { ok: status < 400, status, headers: { get: () => null }, text: async () => JSON.stringify(answer) };
    };
    return asked;
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 5));

// until waits for the page to show what cond looks for, as its effects
// run after paint and its answers come in.
async function until(cond, what) {
    for (let i = 0; i < 100; i++) {
        if (cond()) return;
        await settle();
    }
    assert.fail(`timed out waiting for ${what}`);
}

function find(node, match) {
    if (match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}

const byTag = (tag) => (n) => n.localName === tag;
const button = (text) => (n) => n.localName === 'button' && n.textContent.trim() === text;

// type puts digits in a field as the member types them.
function type(input, value) {
    input.value = value;
    input.dispatch('input', { target: input });
}

// submit sends a form, whose listener Preact names Submit where the test
// DOM's elements have no onsubmit.
function submit(form) {
    form.dispatch(form.listeners.submit ? 'submit' : 'Submit', { target: form, preventDefault() {} });
}

function click(node) {
    node.dispatch('click', { target: node });
}

test("comparing codes shows this member's code and takes the other's as they read it out", async () => {
    const BOB = { id: '2', display_name: 'Bob', identity: 'b' };
    const mine = '1234 5678 9012 3456 7890';
    const theirs = '0987 6543 2109 8765 4321';
    const asked = service({
        'GET /api/dens/D/members/2/code': { mine },
        'POST /api/dens/D/members/2/compare': (body) => {
            if (body.digits.replace(/[\s-]/g, '') !== theirs.replace(/ /g, '')) {
                throw new Error("those digits don't match. Check you typed what they read out to you");
            }
            return { mine, verified: true };
        },
    });
    const root = document.createElement('div');
    mount(h(Codes, { denID: 'D', member: BOB }), root);
    // The code shows in groups of four, and the other's never reaches the
    // page.
    await until(() => root.textContent.includes(mine), 'the code');
    assert.ok(!root.textContent.includes(theirs));
    const input = find(root, byTag('input'));
    const form = find(root, (n) => n.localName === 'form');
    // Their own digits are refused without asking the service.
    type(input, mine);
    await settle();
    submit(form);
    await until(() => /Those are your own digits/.test(root.textContent), 'the refusal of own digits');
    assert.equal(asked.filter((a) => a.method === 'POST').length, 0);
    // A miss says someone may be in the middle.
    type(input, '0987 6543 2109 8765 4320');
    await settle();
    submit(form);
    await until(() => /don't match/.test(root.textContent), 'the miss');
    assert.ok(!find(root, (n) => n.attributes?.['data-verified'] !== undefined));
    // A match, typed with dashes as read out, marks them verified.
    type(input, theirs.replace(/ /g, '-'));
    await settle();
    submit(form);
    await until(() => /The codes match/.test(root.textContent), 'the match');
    const posted = asked.filter((a) => a.method === 'POST');
    assert.equal(posted.length, 2);
    assert.equal(posted[1].body.digits, theirs.replace(/ /g, '-'));
    assert.match(root.textContent, /The codes match\./);
    // The dialog doesn't name the other member, whose name could read as
    // part of its words, and leads to the guide for where to compare.
    assert.ok(!root.textContent.includes('Bob'));
    const guide = find(root, (n) => n.attributes?.['data-docs-link'] !== undefined);
    assert.equal(guide.attributes.href, 'https://dens.chat/docs/join/#comparing-codes');
    assert.equal(guide.attributes.target, '_blank');
    mount(null, root);
});

test('a DM held since the other\'s keys changed offers Compare and Send anyway', async () => {
    const asked = service({ 'POST /api/dens/D/members/2/send-anyway': { ok: true } });
    const root = document.createElement('div');
    let compared = 0;
    mount(h(DMGate, { denID: 'D', gate: 'held', dm: { id: '2', display_name: 'Bob' }, onCompare: () => compared++ }), root);
    assert.match(root.textContent, /Bob's keys changed since you compared codes/);
    click(find(root, button('Compare')));
    assert.equal(compared, 1);
    click(find(root, button('Send anyway')));
    await until(() => asked.length === 1, 'sending anyway');
    assert.deepEqual(asked.map((a) => `${a.method} ${a.path}`), ['POST /api/dens/D/members/2/send-anyway']);
    mount(h(DMGate, { denID: 'D', gate: 'update', dm: { id: '2', display_name: 'Bob' } }), root);
    assert.match(root.textContent, /Bob's Dens needs an update/);
    mount(h(KeysChanged, { name: 'Bob', at: day, onCompare: () => compared++ }), root);
    assert.match(root.textContent, /^Bob's keys changed on .+\.Compare$/);
    click(find(root, button('Compare')));
    assert.equal(compared, 2);
    mount(null, root);
});

// A call's list marks a member whose keys changed since this member
// compared codes with them, or since the call began (M16.2).
test("a call shows a member's keys changed since they were verified, or in the call", () => {
    const began = day;
    assert.equal(keysChanged(undefined, began), false);
    assert.equal(keysChanged({ changes: [] }, began), false);
    assert.equal(keysChanged({ changes: [day - 1000] }, began), false, 'a change before the call, never verified');
    assert.equal(keysChanged({ changes: [day - 1000], held: true }, began), true, 'held since they were verified');
    assert.equal(keysChanged({ changes: [day + 5000] }, began), true, 'a change in the call');
    assert.equal(keysChanged({ changes: [day + 5000] }, Infinity), false, "outside the page's call, only the hold shows");
});
