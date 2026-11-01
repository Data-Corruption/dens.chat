// A call's sounds as the page plays them (M14.2), with stand-ins for the
// browser and the den: the member joining once the den has them in the
// call, others joining, leaving and sharing, nothing of that while
// deafened, the microphone's sounds from each thing that moves it, the
// call's controls, deafening, the call's button, the keys, the helper and
// staff, and the member leaving; and where audio may start without a
// click, as in Android's app, through the call's context.

import assert from 'node:assert/strict';
import { test } from 'node:test';

const realSetTimeout = setTimeout;
globalThis.setTimeout = (fn, ms, ...args) => {
    const t = realSetTimeout(fn, ms, ...args);
    if (ms >= 1000) t.unref();
    return t;
};
const tick = () => new Promise((r) => realSetTimeout(r, 5));

const ASSETS = {
    message: '/assets/sounds/message.1.ogg', joined: '/assets/sounds/joined.2.ogg', left: '/assets/sounds/left.3.ogg',
    shareStarted: '/assets/sounds/shareStarted.4.ogg', shareEnded: '/assets/sounds/shareEnded.5.ogg', micOn: '/assets/sounds/micOn.6.ogg',
    micOff: '/assets/sounds/micOff.7.ogg',
};
const byURL = new Map(Object.entries(ASSETS).map(([name, url]) => [url, name]));
const app = { dataset: { voiceWorklet: '/voice.worklet.js', rnnoiseModule: '/rnnoise.wasm', sounds: JSON.stringify(ASSETS) } };
const listeners = {};
const on = (type, fn) => (listeners[type] ||= []).push(fn);
globalThis.document = {
    getElementById: (id) => (id === 'app' ? app : null),
    addEventListener: on,
    hidden: false,
    visibilityState: 'visible',
    hasFocus: () => true,
    activeElement: null,
};
globalThis.window = { location: { protocol: 'http:', host: 'localhost:8484' }, addEventListener: on };
const kept = new Map();
globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, v), removeItem: (k) => kept.delete(k) };

// members is the den's word on the call: its members, by ID, with their
// marks; the page's member is 7.
let members = [];
const json = (body) => ({ ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify(body) });
globalThis.fetch = async (url) => {
    if (url === '/api/dens/den1/state') {
        return json({ me: { id: '7' }, call_limits: {}, calls: [{ channel_id: 'lounge', members }], members: [] });
    }
    if (byURL.has(url)) return { ok: true, arrayBuffer: async () => new ArrayBuffer(8) };
    return { ok: false, status: 404, headers: { get: () => null }, text: async () => '' };
};

const sockets = [];
class Socket {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = Socket.OPEN;
    listeners = {};
    constructor() {
        sockets.push(this);
    }
    addEventListener(type, fn) {
        (this.listeners[type] ||= []).push(fn);
    }
    send() {}
    receive(msg) {
        for (const fn of this.listeners.message || []) fn({ data: JSON.stringify(msg) });
    }
}
globalThis.WebSocket = Socket;
// deliver has the service send the page a message.
const deliver = (msg) => sockets.at(-1).receive(msg);
// state has the den say who is in the call now.
const state = (list) => {
    members = list;
    deliver({ t: 'den', d: { den: 'den1', events: [{ t: 'voice.state', d: { full: true, calls: [{ channel_id: 'lounge', members: list }] } }] } });
};
const member = (id, marks = {}) => ({ id, muted: false, deafened: false, ...marks });

class Track {
    readyState = 'live';
    enabled = true;
    stop() {
        this.readyState = 'ended';
    }
}
class Stream {
    constructor(tracks = [new Track()], id = 'local') {
        this.tracks = tracks;
        this.id = id;
    }
    getTracks() {
        return this.tracks;
    }
    getAudioTracks() {
        return this.tracks;
    }
}
globalThis.MediaStream = Stream;
Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: { userAgent: '', mediaDevices: { getUserMedia: async () => new Stream() } },
});

class AudioNode {
    connect(node) {
        return node;
    }
    disconnect() {}
}
// started lists the sounds each context played, by name.
const contexts = [];
globalThis.AudioContext = class {
    constructor(opts = {}) {
        this.sampleRate = opts.sampleRate || 44100;
        this.state = 'suspended';
        this.destination = new AudioNode();
        this.audioWorklet = { addModule: async () => {} };
        this.started = [];
        contexts.push(this);
    }
    async resume() {
        this.state = 'running';
    }
    async close() {
        this.state = 'closed';
    }
    async decodeAudioData(data) {
        return { data };
    }
    createMediaStreamSource() {
        return new AudioNode();
    }
    createAnalyser() {
        return Object.assign(new AudioNode(), { fftSize: 2048, getFloatTimeDomainData: (b) => b.fill(0) });
    }
    createGain() {
        return Object.assign(new AudioNode(), { gain: { value: 1 } });
    }
    createMediaStreamDestination() {
        return Object.assign(new AudioNode(), { channelCount: 2, stream: new Stream() });
    }
    createBufferSource() {
        const ctx = this;
        return Object.assign(new AudioNode(), {
            start() {
                ctx.started.push(this.buffer.name);
            },
        });
    }
};
globalThis.AudioWorkletNode = class extends AudioNode {
    port = { postMessage() {}, onmessage: null };
};
globalThis.RTCPeerConnection = class {
    getTransceivers() {
        return [];
    }
    getStats() {
        return Promise.resolve(new Map());
    }
    close() {}
};

// played lists the sounds the page's elements played, by name.
let played = [];
globalThis.Audio = class {
    constructor(src) {
        this.src = src;
        this.volume = 1;
        this.sinkId = '';
        this.srcObject = null;
    }
    play() {
        if (byURL.has(this.src)) played.push(byURL.get(this.src));
        return Promise.resolve();
    }
    pause() {}
};
// A decoded sound remembers which it is, for a context's plays.
const decode = globalThis.AudioContext.prototype.decodeAudioData;
globalThis.AudioContext.prototype.decodeAudioData = async function (data) {
    const buffer = await decode.call(this, data);
    buffer.name = data.name;
    return buffer;
};
const fetchSound = globalThis.fetch;
globalThis.fetch = async (url, ...rest) => {
    const res = await fetchSound(url, ...rest);
    if (!byURL.has(url)) return res;
    return { ok: true, arrayBuffer: async () => Object.assign(new ArrayBuffer(8), { name: byURL.get(url) }) };
};

const { joinCall, leaveCall, pressButton, releaseButton, setDeafened, setMuted, setVoice } = await import('../assets/js/src/voice.js');

// heard is what the page played since the last look.
function heard() {
    const out = played;
    played = [];
    return out;
}

async function join(autoplay) {
    app.dataset.autoplay = String(autoplay);
    members = [member('8')];
    await joinCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    await tick();
}

test("the member's own joining and leaving, others', shares, and nothing of that while deafened", async () => {
    await join(false);
    assert.deepEqual(heard(), [], 'nothing, while the den has yet to say the member is in');
    state([member('8'), member('7')]);
    assert.deepEqual(heard(), ['joined'], 'the den has the member in the call');

    state([member('8'), member('7'), member('9')]);
    state([member('8'), member('7'), member('9', { sharing: true })]);
    state([member('8'), member('7'), member('9')]);
    state([member('8'), member('7')]);
    assert.deepEqual(heard(), ['joined', 'shareStarted', 'shareEnded', 'left']);

    setDeafened(true);
    assert.deepEqual(heard(), ['micOff'], 'deafening closes the microphone, which the member hears');
    state([member('8'), member('7', { deafened: true }), member('9', { sharing: true })]);
    state([member('8'), member('7', { deafened: true })]);
    assert.deepEqual(heard(), [], 'deafened, nothing of the call plays');
    setDeafened(false);
    assert.deepEqual(heard(), ['micOn']);

    leaveCall();
    assert.deepEqual(heard(), ['left'], 'the member leaving');
});

test('the microphone sounds as it opens and closes, whatever moves it, and not where it stays as it was', async () => {
    await join(false);
    state([member('8'), member('7')]);
    heard();

    setMuted(true);
    setMuted(false);
    assert.deepEqual(heard(), ['micOff', 'micOn'], "the call's controls");

    setMuted(true);
    setDeafened(true);
    setDeafened(false);
    assert.deepEqual(heard(), ['micOff'], 'deafening while muted, and hearing again, leave it closed');
    setMuted(false);
    heard();

    state([member('8'), member('7', { staff_muted: true })]);
    state([member('8'), member('7')]);
    assert.deepEqual(heard(), ['micOff', 'micOn'], 'staff muting and unmuting the member');

    // The call's button, with its role push to talk, which sends only while
    // it's held.
    setVoice({ button: 'ptt' });
    assert.deepEqual(heard(), ['micOff'], 'push to talk closes it until held');
    pressButton();
    releaseButton();
    assert.deepEqual(heard(), ['micOn', 'micOff'], "the call's button, held and let go");

    // The keys, here and through the helper.
    deliver({ t: 'keys', d: { keys: { ptt: { code: 'KeyV', label: 'V' } } } });
    const key = (type) => {
        for (const fn of listeners[type] || []) fn({ code: 'KeyV', key: 'v', target: { tagName: 'BODY' }, repeat: false, preventDefault() {}, stopPropagation() {} });
    };
    key('keydown');
    key('keyup');
    assert.deepEqual(heard(), ['micOn', 'micOff'], 'push to talk on its key');
    deliver({ t: 'keys', d: { helper: true, covers: ['ptt'], keys: { ptt: { code: 'KeyV', label: 'V' } } } });
    deliver({ t: 'key', d: { action: 'ptt', down: true } });
    deliver({ t: 'key', d: { action: 'ptt', down: false } });
    assert.deepEqual(heard(), ['micOn', 'micOff'], "push to talk through the helper, in another program");

    setMuted(true);
    pressButton();
    releaseButton();
    assert.deepEqual(heard(), [], 'held while muted, it stays closed');
    setMuted(false);
    setVoice({ button: 'toggleMute' });
    assert.deepEqual(heard(), ['micOn'], 'by voice activity, unmuted, it opens');
    leaveCall();
    heard();
});

test("where audio may start without a click, the call's sounds play through the call's context", async () => {
    await join(true);
    state([member('8'), member('7')]);
    state([member('8'), member('7'), member('9')]);
    setMuted(true);
    setMuted(false);
    await tick();
    const call = contexts.at(-1);
    assert.deepEqual(call.started, ['joined', 'joined', 'micOff', 'micOn'], "the call's context plays them, as it plays the voices");
    assert.deepEqual(heard(), [], 'no element plays them');
    leaveCall();
    assert.deepEqual(heard(), ['left'], "leaving plays its sound once the call's audio has gone, from an element");
});
