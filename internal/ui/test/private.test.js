import assert from 'node:assert/strict';
import { test } from 'node:test';
import { LOCKED, dividerText, keyChanges, needsCheck, sameDigits } from '../assets/js/src/private.js';

const names = (id) => ({ 1: 'Alice', 2: 'Bob' })[id] || 'Unknown member';
const day = Date.UTC(2026, 8, 1);

test('a divider marks where a DM moves to a newer key, and says why', () => {
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
    const changes = keyChanges(messages, keys, names);
    assert.deepEqual([...changes.keys()], ['3']);
    assert.match(changes.get('3'), /^Bob started over on .+\. You checked codes again on .+\.$/);
    assert.match(dividerText(keys[1], keys[2], names), /^Alice started the check again on /);
    assert.equal(dividerText(undefined, { id: '9' }, names), 'The keys changed.');
});

test('an edit under a newer key, among older messages, moves nothing back', () => {
    // A message edited after a new check carries the newer key; the one
    // after it, older, shows no divider going back.
    const keys = [{ id: '10' }, { id: '12', checks: [] }];
    const changes = keyChanges([{ id: '1', key_id: '12' }, { id: '2', key_id: '10' }, { id: '3', key_id: '12' }], keys, names);
    assert.deepEqual([...changes.keys()], ['3']);
});

test('a DM needs this member to check its live key once revealed', () => {
    const keys = [
        { id: '10', channel_id: '5', stage: 'revealed', retired: 'started_over' },
        { id: '11', channel_id: '5', stage: 'revealed', checks: [{ member_id: '2', at: day }] },
        { id: '12', channel_id: '6', stage: 'answered' },
    ];
    assert.equal(needsCheck(keys, '5', '1'), true);
    assert.equal(needsCheck(keys, '5', '2'), false);
    assert.equal(needsCheck(keys, '6', '1'), false);
    assert.equal(needsCheck(keys, '7', '1'), false);
});

test('typed digits compare without their spaces and dashes', () => {
    assert.equal(sameDigits('1234 5678-9012 3456', '1234567890123456'), true);
    assert.equal(sameDigits('1234 5678 9012 3457', '1234 5678 9012 3456'), false);
    assert.equal(sameDigits('', ''), false);
});

test('every reason a message stays sealed has words for it', () => {
    for (const reason of ['no_seal', 'unchecked', 'lost', 'broken']) assert.ok(LOCKED[reason]);
});
