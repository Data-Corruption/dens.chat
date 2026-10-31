import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    actionFor, answerFor, applyCalls, behindBy, bindingTypes, byName, clampVolume, endedText, gateFor, keyLabel, keyMatches, micProblem, playAgain,
    pressed, readCallVolume, readKeyStatus, readKeys, readVoice, readVolumes, released, ringing, rings, rmsDb, rnnoiseChosen, sendRate, setVoice,
    speakingNow, summarize, talking, typesInto, voiceSettings, volumeIn, withVolume,
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
    assert.match(micProblem({ name: 'NotAllowedError' }, false), /Allow it for this page in your browser's site settings/);
    // In the Android app, the app holds the page's microphone (M13.3).
    assert.match(micProblem({ name: 'NotAllowedError' }, true), /Allow it for Dens in Android's settings, under Apps/);
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
    const defaults = { mode: 'voice', auto: true, threshold: -50, button: 'toggleMute' };
    assert.deepEqual(readVoice(''), defaults);
    assert.deepEqual(readVoice('not json'), defaults);
    assert.deepEqual(readVoice('[1]'), { ...defaults });
    // Keys the browser kept before the service kept them are left behind.
    const v = readVoice(JSON.stringify({ mode: 'ptt', auto: false, threshold: -63.4, keys: { ptt: { code: 'KeyV', label: 'V' } } }));
    assert.deepEqual(v, { mode: 'ptt', auto: false, threshold: -63, button: 'ptt' });
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

test('the keys and the helper come from the service, kept to what they can be', () => {
    assert.deepEqual(readKeys({
        ptt: { code: 'KeyV', label: 'V', ctrl: 1 }, toggleMute: { code: '', label: 'X' }, pushMute: { code: 'F13' }, other: { code: 'KeyB', label: 'B' },
    }), { ptt: { code: 'KeyV', label: 'V', ctrl: true, alt: false, shift: false, meta: false } });
    assert.deepEqual(readKeys(undefined), {});
    const none = {
        keys: {}, helper: false, covers: [], note: '', problem: '', confirms: false, bindable: false, binding: false, names: {}, configurable: false, desktop: false,
        notifies: false,
    };
    assert.deepEqual(readKeyStatus(null), none);
    // Without a helper, nothing is covered, whatever came.
    assert.deepEqual(readKeyStatus({ keys: {}, helper: false, covers: ['ptt'], note: 'n' }), none);
    const st = readKeyStatus({ keys: { ptt: { code: 'Mouse4', label: 'Mouse 4' } }, helper: true, covers: ['pushMute', 'shout', 'ptt'], note: 'limits' });
    assert.deepEqual(st.covers, ['ptt', 'pushMute']);
    assert.equal(st.note, 'limits');
    assert.equal(st.keys.ptt.code, 'Mouse4');
    // A desktop that owns the keys, which named two of them.
    const desk = readKeyStatus({
        keys: {}, helper: true, covers: ['ptt'], confirms: true, bindable: false, names: { ptt: 'Press F8', toggleMute: '', shout: 'x' },
        configurable: true, desktop: true,
    });
    assert.deepEqual([desk.confirms, desk.bindable, desk.configurable, desk.desktop], [true, false, true, true]);
    assert.deepEqual(desk.names, { ptt: 'Press F8' });
    // Without a helper the desktop says nothing, but the member's use of it stays.
    assert.deepEqual(readKeyStatus({ helper: false, confirms: true, names: { ptt: 'F8' }, desktop: true, notifies: true }), { ...none, desktop: true });
    // A helper says whether it shows notifications (M12.3).
    assert.equal(readKeyStatus({ helper: true, notifies: true }).notifies, true);
});

test('a key fires its binding with at least its modifiers, and not while it types into a field', () => {
    const b = { code: 'KeyV', label: 'V', ctrl: false, alt: false, shift: false, meta: false };
    const key = (o) => ({ code: 'KeyV', ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, target: null, ...o });
    assert.ok(keyMatches(b, key({})));
    // A player holds Shift to run, and still pushes to talk.
    assert.ok(keyMatches(b, key({ shiftKey: true })));
    assert.ok(!keyMatches({ ...b, ctrl: true }, key({ shiftKey: true })));
    assert.ok(!keyMatches(b, key({ code: 'KeyB' })));
    assert.ok(!keyMatches(undefined, key({})));
    assert.ok(keyMatches({ ...b, ctrl: true, shift: true }, key({ ctrlKey: true, shiftKey: true })));
    // Of two actions on one key, the one with more modifiers takes it.
    const keys = { ptt: b, toggleMute: { ...b, ctrl: true }, pushMute: { ...b, code: 'Mouse4', label: 'Mouse 4' } };
    assert.equal(actionFor(keys, key({})), 'ptt');
    assert.equal(actionFor(keys, key({ ctrlKey: true })), 'toggleMute');
    assert.equal(actionFor(keys, key({ ctrlKey: true, altKey: true })), 'toggleMute');
    assert.equal(actionFor(keys, key({ code: 'Mouse4' })), 'pushMute');
    assert.equal(actionFor(keys, key({ code: 'KeyB' })), null);
    // A binding types into a field unless a modifier, a function key or a
    // mouse button makes it.
    assert.ok(bindingTypes(b));
    assert.ok(bindingTypes({ ...b, shift: true }));
    assert.ok(!bindingTypes({ ...b, ctrl: true }));
    assert.ok(!bindingTypes({ ...b, code: 'F13' }));
    assert.ok(!bindingTypes({ ...b, code: 'Mouse5' }));
    assert.ok(!bindingTypes(undefined));
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

// The call's button's role (M13.4) sets how the member sends where the
// button is, and the role and the way of sending never disagree.
test("the call's button's role sets how the member sends", () => {
    const read = (kept) => readVoice(JSON.stringify(kept));
    assert.equal(read({}).button, 'toggleMute', 'toggle mute by default');
    assert.equal(read({ button: 'pushMute' }).button, 'pushMute');
    assert.equal(read({ button: 'shout' }).button, 'toggleMute');
    // Settings from before the button follow their way of sending, so a
    // member who sent by push to talk still does, on the button.
    assert.deepEqual([read({ mode: 'ptt' }).button, read({ mode: 'ptt', button: 'pushMute' }).button], ['ptt', 'ptt']);
    assert.deepEqual(read({ mode: 'voice', button: 'ptt' }), { ...read({}), button: 'toggleMute' }, 'the way of sending wins');

    setVoice({ button: 'ptt' });
    let v = voiceSettings();
    assert.deepEqual([v.mode, v.button], ['ptt', 'ptt']);
    assert.deepEqual(gateFor(v, true, false), { mode: 'closed' }, 'silent until held');
    assert.deepEqual(gateFor(v, true, true), { mode: 'open' });
    setVoice({ button: 'pushMute' });
    v = voiceSettings();
    assert.deepEqual([v.mode, v.button], ['voice', 'pushMute']);
    assert.deepEqual(gateFor(v, true, false), { mode: 'auto' }, 'by voice activity');
    setVoice({ button: 'toggleMute' });
    assert.deepEqual([voiceSettings().mode, voiceSettings().button], ['voice', 'toggleMute']);
    // Choosing how to send, where the button isn't, keeps the two together.
    setVoice({ mode: 'ptt' });
    assert.equal(voiceSettings().button, 'ptt');
    setVoice({ mode: 'voice' });
    assert.equal(voiceSettings().button, 'toggleMute');
});

// Each role's press and release, as the call's button drives them, the
// keys and the helper too.
test("each role's press and its letting go", () => {
    const at = { held: false, pushed: false, muted: false };
    // Toggle mute: each press toggles, and letting go does nothing.
    let s = pressed('toggleMute', at);
    assert.equal(s.muted, true);
    assert.deepEqual(released('toggleMute', s), s);
    assert.equal(pressed('toggleMute', s).muted, false);
    // Push to mute: muted while held, and only a member who wasn't muted.
    s = pressed('pushMute', at);
    assert.deepEqual(s, { held: false, pushed: true, muted: true });
    assert.deepEqual(released('pushMute', s), at);
    const muted = { ...at, muted: true };
    assert.deepEqual(pressed('pushMute', muted), muted, 'muted already');
    assert.deepEqual(released('pushMute', muted), muted, 'and still muted after');
    // Push to talk: held while held, whatever else.
    s = pressed('ptt', at);
    assert.deepEqual(s, { ...at, held: true });
    assert.deepEqual(released('ptt', s), at);
    assert.deepEqual(released('ptt', at), at, 'let go twice');
    const ptt = readVoice(JSON.stringify({ mode: 'ptt' }));
    assert.equal(talking(ptt, pressed('ptt', at).held, false), true, 'rings while held');
    assert.equal(talking(ptt, pressed('ptt', muted).held, true), false, 'never while muted');
});

// Push to talk lights its ring for everyone in the call (M11): the den
// tells the others, and this page's member rings at once here.
test('push to talk rings while held, through the den for the others, and never for a muted member', () => {
    const ptt = readVoice(JSON.stringify({ mode: 'ptt' }));
    assert.equal(talking(ptt, true, false), true);
    assert.equal(talking(ptt, true, true), false, 'muted');
    assert.equal(talking(ptt, false, false), false, 'let go');
    assert.equal(talking(readVoice(''), true, false), false, 'sending by voice activity, where the key does nothing');
    // The meters hear 3; the den says 1, this page's member, and 2 hold it.
    const ring = ringing(new Set(['3']), new Set(['1', '2']), '1', true);
    assert.deepEqual([...ring].sort(), ['2', '3', 'self']);
    assert.deepEqual([...ringing(new Set(), new Set(['1']), '1', false)], [], "the den's word on this page's member comes late, and the key here decides");
    assert.equal(rings({ id: '2' }, '1', ring), true);
    assert.equal(rings({ id: '3' }, '1', ring), true);
    assert.equal(rings({ id: '1' }, '1', ring), true);
    for (const m of [{ id: '2', muted: true }, { id: '2', staff_muted: true }, { id: '1', muted: true }, { id: '1', staff_muted: true }, { id: '4' }]) {
        assert.equal(rings(m, '1', ring), false, JSON.stringify(m));
    }
    assert.equal(rings({ id: '2' }, '1', null), false, 'outside the call, nobody rings');
});

// The page's notes for the log sum its connection's statistics up (M11).
test("a call's statistics sum up for the log, as Chromium and Firefox give them", () => {
    const report = (stats) => new Map(stats.map((x) => [x.id, x]));
    const chromium = report([
        { id: 'T1', type: 'transport', selectedCandidatePairId: 'CP1' },
        { id: 'CP1', type: 'candidate-pair', localCandidateId: 'L1', remoteCandidateId: 'R1', currentRoundTripTime: 0.028 },
        { id: 'CP2', type: 'candidate-pair', localCandidateId: 'L2', remoteCandidateId: 'R2', currentRoundTripTime: 0.5 },
        { id: 'L1', type: 'local-candidate', candidateType: 'host', protocol: 'udp', address: '192.0.2.5' },
        { id: 'R1', type: 'remote-candidate', candidateType: 'host', protocol: 'udp', address: '198.51.100.7' },
        { id: 'O1', type: 'outbound-rtp', kind: 'audio', mid: '0', packetsSent: 1000, bytesSent: 120000, targetBitrate: 96000 },
        { id: 'O2', type: 'outbound-rtp', kind: 'audio', mid: '3', packetsSent: 0, bytesSent: 0, targetBitrate: 128000 },
        { id: 'I1', type: 'inbound-rtp', packetsReceived: 900, bytesReceived: 100000, packetsLost: 3, jitter: 0.004 },
        { id: 'I2', type: 'inbound-rtp', packetsReceived: 100, bytesReceived: 20000, packetsLost: 1, jitter: 0.002 },
    ]);
    assert.deepEqual(summarize(chromium), {
        protocol: 'udp', local: 'host', remote: 'host', packets_sent: 1000, bytes_sent: 120000, packets_received: 1000, bytes_received: 120000,
        packets_lost: 4, jitter: 4, rtt: 28, target_rate: 96,
    }, "the voice's encoder aims at its rate, whatever a share's sound does");
    const firefox = report([
        { id: 'p1', type: 'candidate-pair', localCandidateId: 'l1', remoteCandidateId: 'r1', selected: true, nominated: true, state: 'succeeded' },
        { id: 'l1', type: 'local-candidate', candidateType: 'host', protocol: 'tcp' },
        { id: 'r1', type: 'remote-candidate', candidateType: 'host', protocol: 'tcp' },
        { id: 'o1', type: 'outbound-rtp', kind: 'audio', packetsSent: 10, bytesSent: 1000 },
        { id: 'ri1', type: 'remote-inbound-rtp', roundTripTime: 0.031 },
        { id: 'i1', type: 'inbound-rtp', packetsReceived: 5, bytesReceived: 500, packetsLost: -2, jitter: 0.01 },
    ]);
    assert.deepEqual(summarize(firefox), {
        protocol: 'tcp', local: 'host', remote: 'host', packets_sent: 10, bytes_sent: 1000, packets_received: 5, bytes_received: 500,
        packets_lost: 0, jitter: 10, rtt: 31, target_rate: 0,
    });
    assert.deepEqual(summarize(new Map()), {
        protocol: '', local: '', remote: '', packets_sent: 0, bytes_sent: 0, packets_received: 0, bytes_received: 0, packets_lost: 0, jitter: 0, rtt: 0,
        target_rate: 0,
    }, 'before there is a path');
});

// The notes say what a call sent a second since the last, and how far its
// voice processor's clock fell behind the page's (M12).
test('a call sends at the rate its bytes went since the last summary', () => {
    assert.equal(sendRate({ bytes: 1000, at: 1000 }, 51000, 11000), 40, '50 kB in 10 s is 40 kbit/s');
    assert.equal(sendRate(null, 51000, 11000), 0, 'without a summary before');
    assert.equal(sendRate({ bytes: 1000, at: 11000 }, 2000, 11000), 0, 'no time gone by');
    assert.equal(sendRate({ bytes: 5000, at: 1000 }, 0, 2000), 0, 'counters that started again');
});

test("a voice processor falls behind by the time its context didn't render", () => {
    const start = { contextTime: 2, performanceTime: 10000 };
    assert.equal(behindBy(start, { contextTime: 12, performanceTime: 20003 }), 3);
    assert.equal(behindBy(start, { contextTime: 11.5, performanceTime: 20000 }), 500, 'half a second missed');
    assert.equal(behindBy(start, { contextTime: 12.002, performanceTime: 20000 }), 0, 'never ahead');
});

// Chromium pauses a frozen page's media elements, and the page plays them
// again as it thaws (M12).
test("the call's elements play again, those stopped with a stream", () => {
    const played = [];
    const el = (name, paused, stream = {}) => ({ name, paused, srcObject: stream, play: () => (played.push(name), Promise.resolve()) });
    playAgain([el('paused', true), el('playing', false), el('emptied', true, null)]);
    assert.deepEqual(played, ['paused']);
    const refused = { paused: true, srcObject: {}, play: () => Promise.reject(new Error('NotAllowedError')) };
    assert.doesNotThrow(() => playAgain([refused]), 'a browser that refuses waits for the next');
});

// The call's volume turns everyone down at once (M12), and the browser
// keeps it.
test("a call plays at full volume unless chosen, and the kept volume makes sense or is ignored", () => {
    assert.equal(readCallVolume(''), 1, 'nothing kept');
    assert.equal(readCallVolume('0.5'), 0.5);
    assert.equal(readCallVolume('0'), 0, 'silent, as chosen');
    assert.equal(readCallVolume('0.33'), 0.35, 'in steps of 5%');
    assert.equal(readCallVolume('7'), 1, 'never past full');
    assert.equal(readCallVolume('loud'), 1);
});
