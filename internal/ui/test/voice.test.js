import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    answerFor, applyCalls, byName, clampVolume, endedText, gateFor, keyLabel, keyMatches, micProblem, readVoice, readVolumes, rmsDb, rnnoiseChosen,
    speakingNow, typesInto, volumeIn, withVolume,
} from '../assets/js/src/voice.js';

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
    for (const reason of ['moved', 'not_found', 'full', 'forbidden', 'deleted', 'rate_limited', 'disconnected_by_staff', 'lost', 'gone']) {
        assert.ok(endedText(reason) && endedText(reason) !== "Couldn't connect the call.", reason);
    }
});

test("a microphone that won't open says why", () => {
    assert.match(micProblem({ name: 'NotAllowedError' }), /Allow it/);
    assert.match(micProblem({ name: 'NotFoundError' }), /No microphone/);
    assert.match(micProblem({ name: 'NotReadableError' }), /in use/);
    assert.match(micProblem(new Error('odd')), /couldn't be opened/);
});

test('an offer sent again after a resume gets the same answer again, and an older one nothing', () => {
    const c = { version: 3, answer: 'v=0 answer' };
    assert.equal(answerFor(c, { version: 4 }), 'answer');
    assert.equal(answerFor(c, { version: 3 }), 'again');
    assert.equal(answerFor(c, { version: 2 }), 'ignore');
    // Before the first answer there's nothing to send again.
    assert.equal(answerFor({ version: 0, answer: '' }, { version: 0 }), 'ignore');
    assert.equal(answerFor({ version: 0, answer: '' }, { version: 1 }), 'answer');
});

test('a level reads in dBFS', () => {
    assert.ok(Math.abs(rmsDb(new Float32Array(100).fill(1))) < 1e-9);
    assert.ok(Math.abs(rmsDb(new Float32Array(100).fill(0.1)) + 20) < 1e-6);
    assert.ok(rmsDb(new Float32Array(100)) < -100);
});

test('speaking lights above the threshold and holds between words', () => {
    const last = new Map();
    assert.deepEqual([...speakingNow(new Map([['1', -30], ['2', -70]]), last, 0)], ['1']);
    // A quiet moment within the hold keeps the mark.
    assert.deepEqual([...speakingNow(new Map([['1', -70], ['2', -70]]), last, 300)], ['1']);
    // Past it, the mark goes.
    assert.deepEqual([...speakingNow(new Map([['1', -70], ['2', -70]]), last, 500)], []);
    // A member whose meter went, as one who left, isn't speaking.
    speakingNow(new Map([['3', -20]]), last, 600);
    assert.deepEqual([...speakingNow(new Map(), last, 650)], []);
});

test('a member plays at full volume unless chosen, and the choice is kept by den and member', () => {
    let volumes = {};
    assert.equal(volumeIn(volumes, 'den', '2'), 1);
    volumes = withVolume(volumes, 'den', '2', 0.4);
    assert.equal(volumeIn(volumes, 'den', '2'), 0.4);
    assert.equal(volumeIn(volumes, 'other den', '2'), 1);
    assert.equal(volumeIn(volumes, 'den', '3'), 1);
    volumes = withVolume(volumes, 'den', '3', 0);
    assert.equal(volumeIn(volumes, 'den', '3'), 0);
    // Full is everyone's unless chosen, so it isn't kept.
    volumes = withVolume(volumes, 'den', '2', 1);
    assert.deepEqual(volumes, { 'den:3': 0 });
    assert.deepEqual(readVolumes(JSON.stringify(volumes)), volumes);
});

test('volumes stay between 0 and full, in steps of 5%', () => {
    assert.equal(clampVolume(0.42), 0.4);
    assert.equal(clampVolume(0.43), 0.45);
    assert.equal(clampVolume(-1), 0);
    assert.equal(clampVolume(3), 1);
    assert.equal(clampVolume('0.5'), 0.5);
    assert.equal(clampVolume(NaN), 1);
    assert.equal(clampVolume(undefined), 1);
});

test('kept volumes that make no sense are skipped', () => {
    assert.deepEqual(readVolumes(''), {});
    assert.deepEqual(readVolumes('not json'), {});
    assert.deepEqual(readVolumes('[0.5]'), {});
    assert.deepEqual(readVolumes('null'), {});
    assert.deepEqual(readVolumes('{"d:1":0.5,"d:2":"0.5","d:3":-0.1,"d:4":1,"d:5":7,"d:6":null,"d:7":0}'), { 'd:1': 0.5, 'd:7': 0 });
});

test('RNNoise runs unless the member turned it off', () => {
    assert.equal(rnnoiseChosen(''), true);
    assert.equal(rnnoiseChosen('1'), true);
    assert.equal(rnnoiseChosen('0'), false);
});

test('voice settings fall back to their defaults, piece by piece', () => {
    const defaults = { mode: 'voice', auto: true, threshold: -50, keys: {} };
    assert.deepEqual(readVoice(''), defaults);
    assert.deepEqual(readVoice('not json'), defaults);
    assert.deepEqual(readVoice('[1]'), { ...defaults });
    const v = readVoice(JSON.stringify({
        mode: 'ptt',
        auto: false,
        threshold: -63.4,
        keys: { ptt: { code: 'KeyV', label: 'V', ctrl: 1 }, toggleMute: { code: '', label: 'X' }, pushMute: { code: 'F13' }, other: { code: 'KeyB', label: 'B' } },
    }));
    assert.deepEqual(v, { mode: 'ptt', auto: false, threshold: -63, keys: { ptt: { code: 'KeyV', label: 'V', ctrl: true, alt: false, shift: false, meta: false } } });
    assert.equal(readVoice('{"threshold": 20}').threshold, 0);
    assert.equal(readVoice('{"threshold": -500}').threshold, -100);
    assert.equal(readVoice('{"mode": "shout", "auto": "yes"}').mode, 'voice');
    assert.equal(readVoice('{"auto": "yes"}').auto, true);
});

test('the gate follows how the member sends', () => {
    const v = readVoice('');
    assert.deepEqual(gateFor(v, true, false), { mode: 'auto' });
    // Automatic needs RNNoise; without it the level decides.
    assert.deepEqual(gateFor(v, false, false), { mode: 'level', threshold: -50 });
    assert.deepEqual(gateFor({ ...v, auto: false, threshold: -30 }, true, false), { mode: 'level', threshold: -30 });
    const ptt = { ...v, mode: 'ptt' };
    assert.deepEqual(gateFor(ptt, true, false), { mode: 'closed' });
    assert.deepEqual(gateFor(ptt, false, true), { mode: 'open' });
});

test('a key fires its binding with exactly its modifiers, and not while it types into a field', () => {
    const b = { code: 'KeyV', label: 'V', ctrl: false, alt: false, shift: false, meta: false };
    const key = (o) => ({ code: 'KeyV', ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, target: null, ...o });
    assert.ok(keyMatches(b, key({})));
    assert.ok(!keyMatches(b, key({ ctrlKey: true })));
    assert.ok(!keyMatches(b, key({ code: 'KeyB' })));
    assert.ok(!keyMatches(undefined, key({})));
    assert.ok(keyMatches({ ...b, ctrl: true, shift: true }, key({ ctrlKey: true, shiftKey: true })));
    assert.ok(typesInto(key({ target: { tagName: 'TEXTAREA' } })));
    assert.ok(typesInto(key({ target: { tagName: 'INPUT', type: 'text' } })));
    assert.ok(typesInto(key({ target: { isContentEditable: true } })));
    assert.ok(!typesInto(key({ target: { tagName: 'TEXTAREA' }, ctrlKey: true })));
    assert.ok(!typesInto(key({ target: { tagName: 'TEXTAREA' }, code: 'F13' })));
    assert.ok(!typesInto(key({ target: { tagName: 'INPUT', type: 'range' } })));
    assert.ok(!typesInto(key({ target: { tagName: 'BUTTON' } })));
    assert.ok(!typesInto(key({ target: null })));
    assert.equal(keyLabel({ ...b, ctrl: true, shift: true }), 'Ctrl+Shift+V');
    assert.equal(keyLabel(undefined), '');
});

test("a call's members show by name, whatever order they joined in", () => {
    const members = new Map([
        ['3', { id: '3', display_name: 'bob' }],
        ['10', { id: '10', display_name: 'Alice' }],
        ['2', { id: '2', display_name: 'Carol' }],
        ['4', { id: '4', display_name: 'Carol' }],
    ]);
    const joined = ['4', '2', '3', '99', '10'].map((id) => ({ id, muted: false }));
    assert.deepEqual(byName(joined, members).map((m) => m.id), ['99', '10', '3', '2', '4']);
    assert.deepEqual(joined.map((m) => m.id), ['4', '2', '3', '99', '10'], 'the den\'s order is left as it was');
});
