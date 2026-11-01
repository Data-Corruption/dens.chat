// The page's sounds (M14.2): which sound each change in a call plays, the
// settings they play by, where they play, through an element on the
// speaker chosen or through the call's context, and the check of a file a
// member gives to take one's place.

import assert from 'node:assert/strict';
import { test } from 'node:test';

const app = { dataset: { sounds: JSON.stringify({ message: '/assets/sounds/message.a1.ogg', joined: '/assets/sounds/joined.b2.ogg', left: '/assets/sounds/left.c3.ogg' }) } };
globalThis.document = { getElementById: (id) => (id === 'app' ? app : null) };

// elements are the page's sound elements, as the browser made them, with
// each play's volume and speaker.
const elements = [];
globalThis.Audio = class {
    constructor(src) {
        this.src = src;
        this.volume = 1;
        this.sinkId = '';
        this.plays = [];
        elements.push(this);
    }
    setSinkId(id) {
        this.sinkId = id;
        return Promise.resolve();
    }
    play() {
        this.plays.push({ volume: this.volume, sinkId: this.sinkId, from: this.currentTime });
        return Promise.resolve();
    }
};
// fetched lists what the page fetched, for a context to decode.
const fetched = [];
globalThis.fetch = async (url) => {
    fetched.push(url);
    return { ok: true, arrayBuffer: async () => new ArrayBuffer(8) };
};

const { DEFAULT_VOLUMES, SOUNDS, callSounds, checkSound, heardSounds, playSound, setSoundRoute, soundSettings, soundURL, warmSounds } =
    await import('../assets/js/src/sounds.js');

// heard is what the page heard of its call: whether the den has the member
// (7) in it, its members with whether each shares, and the microphone.
function heard(members, mic = true, me = '7') {
    const m = new Map(Object.entries(members));
    return { in: m.has(me), members: m, mic };
}

test('the member joining and leaving the call plays its sounds, without the microphone\'s', () => {
    assert.deepEqual(callSounds(null, heard({ 7: false, 8: false })), ['joined'], 'coming in, to a call others are in');
    assert.deepEqual(callSounds(null, heard({ 8: false })), [], 'the den has yet to say the member is in');
    assert.deepEqual(callSounds(heard({ 8: false }), heard({ 7: false, 8: false })), ['joined'], 'and once it does');
    assert.deepEqual(callSounds(heard({ 7: false }, false), heard({ 7: false }, true)), ['micOn'], 'the microphone opening, in the call');
    assert.deepEqual(callSounds(heard({ 7: false, 8: false }), heard({ 8: false })), ['left'], 'the den taking the member out, as staff do');
    assert.deepEqual(callSounds(null, heard({ 7: false }), true), [], 'deafened, nothing of the call plays');
});

test("others joining, leaving and sharing play the call's sounds, once each for a change", () => {
    const two = heard({ 7: false, 8: false });
    assert.deepEqual(callSounds(two, heard({ 7: false, 8: false, 9: false })), ['joined']);
    assert.deepEqual(callSounds(two, heard({ 7: false, 8: false, 9: false, 10: false })), ['joined'], 'two joining together play it once');
    assert.deepEqual(callSounds(two, heard({ 7: false })), ['left']);
    assert.deepEqual(callSounds(two, heard({ 7: false, 8: true })), ['shareStarted']);
    assert.deepEqual(callSounds(heard({ 7: false, 8: true }), heard({ 7: false, 8: false })), ['shareEnded']);
    assert.deepEqual(callSounds(two, heard({ 7: true, 8: false })), ['shareStarted'], "the member's own share");
    assert.deepEqual(callSounds(heard({ 7: false, 8: true }), heard({ 7: false })), ['left'], 'a sharer leaving takes their share with them');
    assert.deepEqual(callSounds(two, heard({ 7: false, 9: false })), ['joined', 'left'], 'one in, another out');
    assert.deepEqual(callSounds(two, heard({ 7: false, 8: false })), [], 'nothing changed');
});

test("a deafened member hears none of the call's sounds but their microphone's", () => {
    const two = heard({ 7: false, 8: false });
    assert.deepEqual(callSounds(two, heard({ 7: false, 8: false, 9: true }, false), true), ['micOff']);
    assert.deepEqual(callSounds(two, heard({ 7: false, 8: true }), true), []);
    assert.deepEqual(callSounds(heard({ 7: false, 8: false }, false), heard({ 7: false }, true), true), ['micOn']);
    assert.deepEqual(callSounds(two, heard({ 8: false }, false), true), [], 'nor the member leaving');
});

test('what the service keeps of a sound, and where it plays from', () => {
    assert.deepEqual(soundSettings(null, 'joined'), { on: true, volume: DEFAULT_VOLUMES.joined, own: 0 }, 'on, at its default, until the member sets it');
    const st = { sounds: { joined: { off: true }, left: { volume: 0 }, micOn: { volume: 140 }, micOff: { volume: 'loud' } }, own: { left: 1700000000000, message: -1 } };
    assert.deepEqual(soundSettings(st, 'joined'), { on: false, volume: DEFAULT_VOLUMES.joined, own: 0 });
    assert.deepEqual(soundSettings(st, 'left'), { on: true, volume: 0, own: 1700000000000 });
    assert.equal(soundSettings(st, 'micOn').volume, DEFAULT_VOLUMES.micOn, 'a volume past 100 is none');
    assert.equal(soundSettings(st, 'micOff').volume, DEFAULT_VOLUMES.micOff);
    assert.equal(soundSettings(st, 'message').own, 0, 'a version that isn\'t one is none');
    assert.equal(soundURL('left', 1700000000000, {}), '/api/sounds/left?v=1700000000000', 'the member\'s own, from the client listener');
    assert.equal(soundURL('joined', 0, { joined: '/assets/sounds/joined.b2.ogg' }), '/assets/sounds/joined.b2.ogg');
    assert.equal(soundURL('micOn', 0, {}), '', 'one the build left out plays nothing');
    assert.equal(SOUNDS.length, 7);
    for (const [name] of SOUNDS) assert.ok(DEFAULT_VOLUMES[name] > 0 && DEFAULT_VOLUMES[name] <= 100, `${name} has a default volume`);
});

test('a sound plays from its element, at its volume, on the speaker chosen, and not once off', async () => {
    // An element plays once its speaker is set, which takes a moment.
    const flush = () => new Promise((r) => setTimeout(r));
    setSoundRoute(() => ({ context: null, speaker: 'headset' }));
    heardSounds({ sounds: { left: { volume: 20 }, message: { off: true } } });
    assert.ok(elements.some((e) => e.src === '/assets/sounds/joined.b2.ogg'), 'the status readies each sound it names');
    assert.ok(playSound('joined'));
    await flush();
    const joined = elements.find((e) => e.src === '/assets/sounds/joined.b2.ogg');
    assert.deepEqual(joined.plays.at(-1), { volume: DEFAULT_VOLUMES.joined / 100, sinkId: 'headset', from: 0 });
    playSound('left');
    await flush();
    assert.equal(elements.find((e) => e.src === '/assets/sounds/left.c3.ogg').plays.at(-1).volume, 0.2);
    const message = elements.find((e) => e.src === '/assets/sounds/message.a1.ogg');
    assert.equal(playSound('message'), false, 'off, it plays nothing');
    assert.equal(message.plays.length, 0);
    assert.ok(playSound('message', { preview: true, volume: 45 }), "the settings' ▶ plays it all the same, at its slider's volume");
    await flush();
    assert.equal(message.plays.at(-1).volume, 0.45);
    assert.equal(playSound('micOn'), false, 'a sound the build left out plays nothing');

    // The member's own file takes its place, by its version; the page's
    // element for the default goes.
    heardSounds({ own: { joined: 42 } });
    assert.ok(playSound('joined'));
    await flush();
    assert.equal(elements.at(-1).src, '/api/sounds/joined?v=42');
    assert.equal(elements.at(-1).plays.length, 1);
});

test("where the call plays through a context, a sound plays through it, decoded once", async () => {
    const started = [];
    const ctx = {
        state: 'running',
        destination: { name: 'destination' },
        decoded: 0,
        async decodeAudioData() {
            this.decoded++;
            return { duration: 0.29 };
        },
        createBufferSource() {
            return {
                connect: (gain) => gain,
                disconnect() {},
                start() {
                    started.push(this);
                },
            };
        },
        createGain() {
            const g = { gain: { value: 1 }, to: null, disconnect() {} };
            g.connect = (dest) => {
                g.to = dest;
                return dest;
            };
            return g;
        },
    };
    heardSounds({});
    fetched.length = 0;
    setSoundRoute(() => ({ context: ctx, speaker: 'headset' }));
    warmSounds(ctx);
    assert.deepEqual(fetched, ['/assets/sounds/message.a1.ogg', '/assets/sounds/joined.b2.ogg', '/assets/sounds/left.c3.ogg'], 'the call\'s context decodes them as it starts');
    const plays = elements.reduce((n, e) => n + e.plays.length, 0);
    playSound('joined');
    playSound('joined');
    await new Promise((r) => setTimeout(r, 10));
    assert.equal(started.length, 2);
    assert.equal(started[0].buffer.duration, 0.29);
    assert.equal(ctx.decoded, 3, 'each decoded once');
    assert.equal(elements.reduce((n, e) => n + e.plays.length, 0), plays, 'no element plays it');
    setSoundRoute(() => ({ context: null, speaker: '' }));
});

test("a member's own file must play here, for at most 10 seconds, and be at most 1 MiB", async () => {
    const file = (size) => ({ size, arrayBuffer: async () => new ArrayBuffer(4) });
    assert.equal(await checkSound(file(1000), async () => 2.5), '');
    assert.equal(await checkSound(file(1000), async () => 10), '');
    assert.match(await checkSound(file(1000), async () => 10.4), /lasts 11 seconds, and one plays for at most 10/);
    assert.match(await checkSound(file(1000), async () => { throw new Error('EncodingError'); }), /doesn't play in this browser/);
    assert.match(await checkSound(file(1000), async () => 0), /doesn't play in this browser/);
    assert.match(await checkSound(file((1 << 20) + 1), async () => 1), /over 1 MiB/);
});
