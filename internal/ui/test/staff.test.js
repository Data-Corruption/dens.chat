// Staff's tools in Manage: Members' order and matching, the day each was
// last seen, and the audit log's words for each kind of line.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { dayTime } from '../src/ui/components.jsx';
import { auditText, changeText, matchMember, seenDay, sortMembers } from '../src/staff.jsx';
import cases from '../../denproto/testdata/audit.json';

test("each kind of line reads in its words, a kind this page doesn't know as itself", () => {
    const nameOf = (id) => `@${cases.members[id]}`;
    for (const c of cases.cases) {
        const got = auditText(c.line, nameOf, 'en-US');
        assert.equal(got.text, c.text, `line ${c.line.id}`);
        assert.deepEqual(got.changes, c.changes || [], `line ${c.line.id}'s changes`);
    }
});

test("an event's times show as dates and times, and a member gone from the view as someone", () => {
    const starts = 1795179600000;
    assert.equal(changeText({ field: 'starts_at', from: String(starts), to: String(starts + 1800000) }, 'event.updated', 'en-US'),
        `Starts: ${dayTime(starts, 'en-US')} → ${dayTime(starts + 1800000, 'en-US')}`);
    const line = { id: '1', at: 1, kind: 'ban.lifted', by: '9', member_id: '8' };
    assert.equal(auditText(line, () => 'someone').text, "someone lifted someone's ban");
});

test("a line that lacks what its sentence names reads as its kind, and a call's without a channel in words", () => {
    const nameOf = (id) => `@${cases.members[id]}`;
    const text = (line) => auditText({ id: '1', at: 1, by: '3', member_id: '2', ...line }, nameOf);
    for (const line of [{ kind: 'channel.deleted' }, { kind: 'role.changed' }, { kind: 'emoji.renamed', name: 'hello' }, { kind: 'record.reset' }]) {
        const got = text(line);
        assert.equal(got.text, `@carol: ${line.kind}`);
        assert.ok(!got.text.includes('undefined'));
    }
    assert.deepEqual(text({ kind: 'group.updated', changes: [{ field: 'name', from: 'Games', to: 'Play' }] }),
        { text: '@carol: group.updated', changes: ['Name: Games → Play'] });
    assert.equal(text({ kind: 'call.moved' }).text, '@carol moved @bob to another call');
    assert.equal(text({ kind: 'call.disconnected' }).text, '@carol disconnected @bob from a call');
});

test('the day a member was last seen is its day in UTC, wherever the page is', () => {
    const day = Date.UTC(2026, 10, 22);
    assert.equal(seenDay(day, 'en-US'), 'Nov 22, 2026');
});

const members = [
    { id: '1', username: 'alice', display_name: 'Alice Liddell', role: 'owner', joined_at: 10 },
    { id: '2', username: 'bob_k', display_name: 'Robert', role: 'member', joined_at: 30 },
    { id: '3', username: 'carol', display_name: 'Carol the Second', role: 'moderator', joined_at: 20 },
];

test("a few letters find a member by the start of their username, their display name or a word in it", () => {
    const found = (q) => members.filter((m) => matchMember(m, q)).map((m) => m.username);
    assert.deepEqual(found(''), ['alice', 'bob_k', 'carol']);
    assert.deepEqual(found('BO'), ['bob_k']);
    assert.deepEqual(found('rob'), ['bob_k']);
    assert.deepEqual(found('sec'), ['carol']);
    assert.deepEqual(found('lid'), ['alice']);
    assert.deepEqual(found('ice'), []);
});

test('Members sorts by each column, ties by name, the online above every day', () => {
    const rows = members.map((m, i) => ({ m, seen: [Infinity, 500, 0][i] }));
    const order = (by) => sortMembers(rows, by).map((r) => r.m.username);
    assert.deepEqual(order('name'), ['alice', 'carol', 'bob_k']);
    assert.deepEqual(order('role'), ['alice', 'carol', 'bob_k']);
    assert.deepEqual(order('joined'), ['bob_k', 'carol', 'alice']);
    assert.deepEqual(order('seen'), ['alice', 'bob_k', 'carol']);
});
