import assert from 'node:assert/strict';
import { test } from 'node:test';
import { applyCalls, endedText, micProblem } from '../assets/js/src/voice.js';

const call = (channel, ...members) => ({ channel_id: channel, members: members.map((id) => ({ id })) });

test('a call that changes replaces what the page held of it, and one that empties goes', () => {
    let calls = [call('10', '1'), call('11', '2')];
    calls = applyCalls(calls, { calls: [call('10', '1', '3')] });
    assert.deepEqual(calls, [call('10', '1', '3'), call('11', '2')]);
    calls = applyCalls(calls, { calls: [call('11')] });
    assert.deepEqual(calls, [call('10', '1', '3')]);
    calls = applyCalls(calls, { calls: [call('12', '4')] });
    assert.deepEqual(calls, [call('10', '1', '3'), call('12', '4')]);
});

test('a full state replaces every call, as after a resume', () => {
    const calls = applyCalls([call('10', '1'), call('11', '2')], { calls: [call('12', '3')], full: true });
    assert.deepEqual(calls, [call('12', '3')]);
    assert.deepEqual(applyCalls(undefined, { calls: [], full: true }), []);
});

test("a call that never connected names the den's ports, when the page has them", () => {
    assert.match(endedText('failed', [7881, 7882]), /UDP 7881 and TCP 7882/);
    assert.equal(endedText('failed'), "Couldn't connect the call.");
    assert.equal(endedText('something new'), "Couldn't connect the call.");
    for (const reason of ['moved', 'not_found', 'full', 'forbidden', 'deleted', 'rate_limited', 'lost', 'gone']) {
        assert.ok(endedText(reason) && endedText(reason) !== "Couldn't connect the call.", reason);
    }
});

test("a microphone that won't open says why", () => {
    assert.match(micProblem({ name: 'NotAllowedError' }), /Allow it/);
    assert.match(micProblem({ name: 'NotFoundError' }), /No microphone/);
    assert.match(micProblem({ name: 'NotReadableError' }), /in use/);
    assert.match(micProblem(new Error('odd')), /couldn't be opened/);
});
