import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { changeText, groupGate, groupTitle, isPrivate, placeAt } from '../assets/js/src/private.js';
import { GroupGate, GroupLine, GroupPanel, memberItems, removerNote } from '../assets/js/src/groups.jsx';

const members = new Map([
    ['1', { id: '1', display_name: 'Alice', username: 'alice', identity: 'a' }],
    ['2', { id: '2', display_name: 'Bob', username: 'bob', identity: 'b' }],
    ['3', { id: '3', display_name: 'Carol', username: 'carol', identity: 'c' }],
    ['4', { id: '4', display_name: 'Dave', username: 'dave', identity: 'd' }],
    ['5', { id: '5', display_name: 'Erin', username: 'erin' }],
]);
const nameOf = (id) => members.get(id)?.display_name || 'Unknown member';

// findAll lists the elements at or under node that test accepts.
function findAll(node, test, out = []) {
    if (node.nodeType === 1 && test(node)) out.push(node);
    for (const c of node.childNodes || []) findAll(c, test, out);
    return out;
}
const attr = (n, name) => n.attributes?.[name];

test("a group's change says who made it and whom it took in or out, you among them", () => {
    const say = (c) => changeText(c, nameOf, '1');
    assert.equal(say({ kind: 'made', by: '2', subjects: ['1', '3'] }), 'Bob made the group.');
    assert.equal(say({ kind: 'made', by: '1', subjects: ['2', '3'], name: 'Weekend' }), 'You made the group “Weekend”.');
    assert.equal(say({ kind: 'added', by: '2', subjects: ['3', '4', '1'] }), 'Bob added Carol, Dave and you.');
    assert.equal(say({ kind: 'added', by: '1', subjects: ['4'] }), 'You added Dave.');
    assert.equal(say({ kind: 'removed', by: '1', subjects: ['3'] }), 'You removed Carol.');
    assert.equal(say({ kind: 'left', by: '3' }), 'Carol left the group.');
    assert.equal(say({ kind: 'departed', subjects: ['2'] }), 'Bob left the den.');
    assert.equal(say({ kind: 'named', by: '3', name: 'Trip' }), 'Carol named the group “Trip”.');
    assert.equal(say({ kind: 'named', by: '3', name: '' }), "Carol took the group's name away.");
    // One sealed under a key this device doesn't hold.
    assert.equal(say({ kind: 'named', by: '3' }), 'Carol renamed the group.');
});

test("a group is named by its name, or its other members' names, the first two past three", () => {
    assert.equal(groupTitle({ name: 'Weekend', members: ['1', '2'] }, members, '1'), 'Weekend');
    assert.equal(groupTitle({ members: ['1', '2', '3'] }, members, '1'), 'Bob, Carol');
    assert.equal(groupTitle({ members: ['1', '2', '3', '4', '5'] }, members, '1'), 'Bob, Carol and 2 others');
    assert.equal(groupTitle({ members: ['1'] }, members, '1'), 'Just you');
    assert.equal(groupTitle({ members: ['1', '9'] }, members, '1'), 'Unknown member');
    assert.ok(isPrivate({ kind: 'group_dm' }) && isPrivate({ kind: 'dm' }) && !isPrivate({ kind: 'text' }));
});

test("a group's members and lines are what its changes make them, each line before the first message after it", () => {
    const changes = [
        { n: 1, kind: 'made', by: '1', subjects: ['2', '3'], at: 100 },
        { n: 2, kind: 'added', by: '2', subjects: ['4'], at: 250 },
        { n: 3, kind: 'removed', by: '1', subjects: ['3'], at: 400 },
    ];
    const messages = [{ id: '10', created_at: 200 }, { id: '11', created_at: 300 }];
    const { before, after } = placeAt(messages, changes, (c) => c.at, {});
    assert.deepEqual(before.get('10').map((c) => c.n), [1]);
    assert.deepEqual(before.get('11').map((c) => c.n), [2]);
    assert.deepEqual(after.map((c) => c.n), [3]);
    // Older messages not loaded yet hold the lines from before them back.
    const back = placeAt(messages, changes, (c) => c.at, { hasOlder: true });
    assert.equal(back.before.get('10'), undefined);
    // A newer page not loaded holds back what comes after.
    assert.deepEqual(placeAt(messages, changes, (c) => c.at, { hasNewer: true }).after, []);

    const view = { me: { id: '1' }, seal: true, members: [...members.values()], identities: { 3: { verified: true } },
        group_dms: { 40: { members: ['1', '2', '4'], changes } } };
    const root = document.createElement('div');
    mount(h(GroupPanel, { denID: 'd', view, channel: { id: '40' }, group: view.group_dms[40], title: 'Bob, Dave', members, online: new Set(),
        onCompare() {}, onProfile() {}, onAdd() {}, onRename() {} }), root);
    const rows = findAll(root, (n) => attr(n, 'data-member') !== undefined).map((li) => attr(li, 'data-member'));
    assert.deepEqual(rows, ['1', '2', '4'], 'the members are in the order they joined, Carol removed');
    // A row is the picture and the name, the one who removes others marked
    // under theirs; the rest is each row's ⋯.
    assert.match(findAll(root, (n) => attr(n, 'data-member') === '1')[0].textContent, /Alice \(you\)\s*Made the group/);
    assert.equal(findAll(root, (n) => attr(n, 'data-remover') !== undefined).length, 1);
    assert.equal(findAll(root, (n) => n.localName === 'button' && n.textContent.trim() === 'Remove').length, 0, 'no Remove in the rows');
    assert.deepEqual(findAll(root, (n) => attr(n, 'data-member-more') !== undefined).map((b) => attr(b, 'aria-label')),
        ['Options for Bob', 'Options for Dave'], "the others' rows have a ⋯, the member's own none");
    mount(null, root);

    // The first member removes the others, and is marked for why.
    assert.equal(removerNote({ changes }, '1'), 'Made the group');
    assert.equal(removerNote({ changes }, '2'), 'Can remove members', 'the maker left, and Bob has been in longest');
    assert.equal(removerNote({ changes: changes.slice(1) }, '1'), 'Can remove members', 'the first change is past what the page holds');
    const keys = (o) => memberItems(o).map((i) => i.key);
    const bob = members.get('2');
    const erin = members.get('5');
    assert.deepEqual(keys({ self: false, member: bob, removes: true }), ['compare', 'remove']);
    assert.deepEqual(keys({ self: false, member: erin, removes: true }), ['remove'], 'no comparing with a Dens that publishes no identity');
    assert.deepEqual(keys({ self: false, member: bob, removes: false }), ['compare']);
    assert.deepEqual(keys({ self: false, member: erin, removes: true, broken: true }), [], 'nothing removes in a group that fails its checks');
    assert.deepEqual(keys({ self: true, member: bob, removes: true }), []);
    assert.equal(memberItems({ self: false, member: bob, kept: { verified: true } })[0].label, 'Compare codes again');

    const line = document.createElement('div');
    mount(h(GroupLine, { change: changes[1], nameOf, me: '1' }), line);
    assert.equal(line.textContent, 'Bob added Dave.');
    mount(null, line);
});

test('a group takes messages unless something stands in its composer', () => {
    const base = { me: { id: '1' }, seal: true, members: [...members.values()], identities: {},
        group_dms: { 40: { members: ['1', '2', '3'], changes: [] } } };
    const gate = (v) => groupGate(v, { id: '40', kind: 'group_dm' });
    assert.deepEqual(gate(base), { gate: '' });
    assert.deepEqual(gate({ ...base, seal: false }), { gate: 'no_seal' });
    assert.deepEqual(gate({ ...base, group_dms: { 40: { ...base.group_dms[40], syncing: true } } }), { gate: 'checking' });
    assert.deepEqual(gate({ ...base, group_dms: {} }), { gate: 'checking' });
    assert.deepEqual(gate({ ...base, group_dms: { 40: { ...base.group_dms[40], broken: 'no' } } }), { gate: 'broken' });
    assert.deepEqual(gate({ ...base, group_dms: { 40: { members: ['1'], changes: [] } } }), { gate: 'alone' });
    assert.deepEqual(gate({ ...base, group_dms: { 40: { members: ['1', '2', '5'], changes: [] } } }), { gate: 'update', member: '5' });
    assert.deepEqual(gate({ ...base, identities: { 3: { held: true } } }), { gate: 'held', member: '3' });

    const root = document.createElement('div');
    mount(h(GroupGate, { denID: 'd', gate: 'held', member: members.get('3'), onCompare() {} }), root);
    assert.match(root.textContent, /Carol's keys changed since you compared codes/);
    assert.ok(findAll(root, (n) => n.localName === 'button').some((b) => b.textContent.trim() === 'Send anyway'));
    mount(h(GroupGate, { denID: 'd', gate: 'broken', onCompare() {} }), root);
    assert.match(root.textContent, /someone may be in the middle/);
    mount(null, root);
});
