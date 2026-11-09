// Push to talk's release delay and the deafen key, as the page drives its
// call, with stand-ins for the browser, the service and the den, against
// node:test's mock clock: the gate the voice processor runs, the ring here
// and what the den hears of it stay open for the delay after the key's
// release, whether the page or the helper saw it, and a press within the
// delay keeps them open.

import assert from 'node:assert/strict';
import { mock, test } from 'node:test';

const realSetTimeout = setTimeout;
const tick = () => new Promise((r) => realSetTimeout(r, 5));

const app = { dataset: { voiceWorklet: '/voice.worklet.js', rnnoiseModule: '/rnnoise.wasm' } };
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

const json = (body) => ({ ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify(body) });
globalThis.fetch = async (url) => {
    if (url === '/api/dens/den1/state') return json({ me: { id: '7' }, call_limits: {}, calls: [], members: [] });
    return { ok: false, status: 404, headers: { get: () => null }, text: async () => '' };
};

// sent is what the page told the den, by type.
const sent = [];
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
    send(text) {
        sent.push(JSON.parse(text));
    }
    receive(msg) {
        for (const fn of this.listeners.message || []) fn({ data: JSON.stringify(msg) });
    }
}
globalThis.WebSocket = Socket;
const deliver = (msg) => sockets.at(-1).receive(msg);

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
globalThis.AudioContext = class {
    constructor(opts = {}) {
        this.sampleRate = opts.sampleRate || 44100;
        this.state = 'suspended';
        this.destination = new AudioNode();
        this.audioWorklet = { addModule: async () => {} };
    }
    async resume() {
        this.state = 'running';
    }
    async close() {
        this.state = 'closed';
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
};
// gates are what the page told its voice processor's gate, the last last.
const gates = [];
globalThis.AudioWorkletNode = class extends AudioNode {
    constructor(ctx, name, opts) {
        super();
        gates.push(opts?.processorOptions?.gate?.mode);
    }
    port = { postMessage: (m) => m.gate && gates.push(m.gate.mode), onmessage: null };
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
globalThis.Audio = class {
    srcObject = null;
    play() {
        return Promise.resolve();
    }
    pause() {}
};

const { joinCall, leaveCall, onCall, setVoice } = await import('../assets/js/src/voice.js');

// The page's call, as the page's components get it.
let seen = null;
onCall((s) => (seen = s));

// open says whether the member is heard now: the processor's gate open,
// their ring lit here, and the den told they hold push to talk.
function open() {
    const talks = sent.filter((m) => m.t === 'voice.talk').at(-1);
    return { gate: gates.at(-1), ring: !!seen?.speaking.has('self'), den: !!talks?.d.on };
}
const HEARD = { gate: 'open', ring: true, den: true };
const QUIET = { gate: 'closed', ring: false, den: false };

const key = (type, code = 'KeyV') => {
    for (const fn of listeners[type] || []) fn({ code, key: 'v', target: { tagName: 'BODY' }, repeat: false, preventDefault() {}, stopPropagation() {} });
};

async function join() {
    await joinCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    await tick();
    deliver({ t: 'den', d: { den: 'den1', events: [{ t: 'voice.state', d: { full: true, calls: [{ channel_id: 'lounge', members: [{ id: '7' }] }] } }] } });
}

test("push to talk stays open for its release delay after the key's release, and a press within it keeps it open", async () => {
    setVoice({ mode: 'ptt', releaseDelay: 400 });
    await join();
    deliver({ t: 'keys', d: { keys: { ptt: { code: 'KeyV', label: 'V' } } } });
    mock.timers.enable({ apis: ['setTimeout'] });
    try {
        assert.deepEqual(open(), QUIET);
        key('keydown');
        assert.deepEqual(open(), HEARD, 'held');
        key('keyup');
        assert.deepEqual(open(), HEARD, 'let go, within the delay');
        mock.timers.tick(399);
        assert.deepEqual(open(), HEARD);
        mock.timers.tick(1);
        assert.deepEqual(open(), QUIET, 'the delay passed');

        // A press within the delay keeps it open, and its own release
        // starts the delay again.
        key('keydown');
        key('keyup');
        mock.timers.tick(300);
        key('keydown');
        mock.timers.tick(1000);
        assert.deepEqual(open(), HEARD, 'pressed again within it, and held');
        key('keyup');
        mock.timers.tick(399);
        assert.deepEqual(open(), HEARD);
        mock.timers.tick(1);
        assert.deepEqual(open(), QUIET);

        // The page losing focus lets go too, after the delay, which a
        // release while it waits doesn't stretch.
        key('keydown');
        key('keyup');
        mock.timers.tick(200);
        for (const fn of listeners.blur || []) fn();
        mock.timers.tick(200);
        assert.deepEqual(open(), QUIET, 'once, from the first release');

        // The helper's release, from another program, waits as the page's.
        deliver({ t: 'keys', d: { helper: true, covers: ['ptt'], keys: { ptt: { code: 'KeyV', label: 'V' } } } });
        deliver({ t: 'key', d: { action: 'ptt', down: true } });
        deliver({ t: 'key', d: { action: 'ptt', down: false } });
        assert.deepEqual(open(), HEARD, "the helper's release, within the delay");
        mock.timers.tick(400);
        assert.deepEqual(open(), QUIET);

        // With no delay, the release closes it at once.
        setVoice({ releaseDelay: 0 });
        deliver({ t: 'key', d: { action: 'ptt', down: true } });
        deliver({ t: 'key', d: { action: 'ptt', down: false } });
        assert.deepEqual(open(), QUIET);
    } finally {
        mock.timers.reset();
        leaveCall();
    }
});

test('the deafen key deafens and hears again, once for a held key, and the helper reads it in other programs', async () => {
    setVoice({ mode: 'voice' });
    await join();
    const deafened = () => seen.call.deafened;
    deliver({ t: 'keys', d: { keys: { toggleDeafen: { code: 'KeyD', label: 'D' } } } });
    key('keydown', 'KeyD');
    assert.equal(deafened(), true);
    assert.equal(seen.call.muted, true, 'deafened, the member is muted too');
    for (const fn of listeners.keydown || []) fn({ code: 'KeyD', key: 'd', target: { tagName: 'BODY' }, repeat: true, preventDefault() {}, stopPropagation() {} });
    key('keyup', 'KeyD');
    assert.equal(deafened(), true, "a held key's repeats do nothing");
    key('keydown', 'KeyD');
    key('keyup', 'KeyD');
    assert.equal(deafened(), false);
    assert.equal(seen.call.muted, false, 'and hearing again unmutes them, as they were');
    assert.equal(sent.filter((m) => m.t === 'voice.mute').at(-1).d.deafened, false, 'the den hears of each');

    // Through the helper, in another program; the page's own sight of the
    // key during the call is the helper's, so it acts once.
    deliver({ t: 'keys', d: { helper: true, covers: ['toggleDeafen'], keys: { toggleDeafen: { code: 'KeyD', label: 'D' } } } });
    deliver({ t: 'key', d: { action: 'toggleDeafen', down: true } });
    key('keydown', 'KeyD');
    deliver({ t: 'key', d: { action: 'toggleDeafen', down: false } });
    key('keyup', 'KeyD');
    assert.equal(deafened(), true);
    deliver({ t: 'key', d: { action: 'toggleDeafen', down: true } });
    deliver({ t: 'key', d: { action: 'toggleDeafen', down: false } });
    assert.equal(deafened(), false);
    leaveCall();
});
