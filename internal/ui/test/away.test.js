// Away, as the page drives its call, with stand-ins for the browser, the
// service and the den: it mutes the member and tells the den, and ends at
// the toggle, at an unmute and at a press of push to talk, by its key, the
// call's button or the helper, which the den hears unmuted before the
// press; deafening keeps it; a move and a join again after the den
// restarted keep it, and a join of the member's own puts the microphone
// back as it was. And the call's pane: a click's join opens it, and a
// move's leaves the page where it is.

import assert from 'node:assert/strict';
import { test } from 'node:test';

const realSetTimeout = setTimeout;
const tick = (ms = 5) => new Promise((r) => realSetTimeout(r, ms));

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
// The window's history, which the call's pane takes an entry in, in a
// window wider than the phone layout.
const history = {
    entries: [{ state: null, path: '/den/den1/1' }],
    at: 0,
    get state() {
        return this.entries[this.at].state;
    },
    pushState(state, _, path) {
        this.entries.splice(this.at + 1, Infinity, { state, path });
        this.at += 1;
        globalThis.window.location.pathname = path;
    },
    replaceState(state, _, path) {
        this.entries[this.at] = { state, path };
        globalThis.window.location.pathname = path;
    },
    go(delta) {
        realSetTimeout(() => {
            this.at += delta;
            globalThis.window.location.pathname = this.entries[this.at].path;
            for (const fn of listeners.popstate || []) fn({ state: this.entries[this.at].state });
        }, 0);
    },
};
globalThis.window = {
    location: { protocol: 'http:', host: 'localhost:8484', pathname: '/den/den1/1', hash: '' },
    addEventListener: on,
    history,
    matchMedia: (q) => ({ matches: q.includes('min-width') }),
};
const kept = new Map();
globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, v), removeItem: (k) => kept.delete(k) };

const json = (body) => ({ ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify(body) });
const state = {
    me: { id: '7' }, call_limits: {}, calls: [], members: [], away: true,
    channels: [{ id: 'lounge', name: 'Lounge', kind: 'voice' }, { id: 'games', name: 'Games', kind: 'voice' }],
};
globalThis.fetch = async (url) => {
    if (url === '/api/dens/den1/state') return json(state);
    return { ok: false, status: 404, headers: { get: () => null }, text: async () => '' };
};

// sent is what the page told the den, in order.
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
globalThis.AudioWorkletNode = class extends AudioNode {
    port = { postMessage: () => {}, onmessage: null };
};
// peers are the page's peer connections, the last last, which a test
// says are connected, as the browser would.
const peers = [];
globalThis.RTCPeerConnection = class {
    connectionState = 'new';
    iceConnectionState = 'new';
    constructor() {
        peers.push(this);
    }
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

const { joinCall, leaveCall, onCall, openCall, pressButton, releaseButton, setAway, setDeafened, setMuted, setVoice } = await import('../src/voice/voice.js');
const { nav } = await import('../src/core/nav.js');

let seen = null;
onCall((s) => (seen = s));

const marks = () => sent.filter((m) => m.t === 'voice.mute').at(-1)?.d;
const joins = () => sent.filter((m) => m.t === 'voice.join');
const now = () => ({ away: !!seen.call.away, muted: seen.call.muted });

const key = (type, code) => {
    for (const fn of listeners[type] || []) fn({ code, key: code, target: { tagName: 'BODY' }, repeat: false, preventDefault() {}, stopPropagation() {} });
};

async function join(channel = 'lounge') {
    await joinCall('den1', channel, { den: 'Den', channel: 'Lounge' });
    await tick();
    deliver({ t: 'den', d: { den: 'den1', events: [{ t: 'voice.state', d: { full: true, calls: [{ channel_id: channel, members: [{ id: '7' }] }] } }] } });
}

test("Away mutes the member and tells the den, and the toggle puts the microphone back as it was", async () => {
    setVoice({ mode: 'voice' });
    await join();
    assert.equal(seen.den.away, true, "the den's info says it keeps Away");
    setAway(true);
    assert.deepEqual(now(), { away: true, muted: true });
    assert.deepEqual(marks(), { den: 'den1', muted: true, deafened: false, away: true });
    setAway(false);
    assert.deepEqual(now(), { away: false, muted: false });
    assert.deepEqual(marks(), { den: 'den1', muted: false, deafened: false, away: false });

    // Muted before, muted after.
    setMuted(true);
    setAway(true);
    setAway(false);
    assert.deepEqual(now(), { away: false, muted: true });
    setMuted(false);
    leaveCall();
});

test('an unmute ends Away, and so does the toggle mute key', async () => {
    setVoice({ mode: 'voice' });
    await join();
    setAway(true);
    setMuted(false);
    assert.deepEqual(now(), { away: false, muted: false });
    assert.equal(marks().away, false);

    deliver({ t: 'keys', d: { keys: { toggleMute: { code: 'KeyM', label: 'M' } } } });
    setAway(true);
    key('keydown', 'KeyM');
    key('keyup', 'KeyM');
    assert.deepEqual(now(), { away: false, muted: false });
    assert.equal(marks().away, false);
    leaveCall();
});

test("push to talk ends Away by its key, the call's button and the helper, and the den hears the unmute before the press", async () => {
    setVoice({ mode: 'ptt', releaseDelay: 0, button: 'ptt' });
    await join();
    deliver({ t: 'keys', d: { keys: { ptt: { code: 'KeyV', label: 'V' } } } });
    setAway(true);
    const from = sent.length;
    key('keydown', 'KeyV');
    assert.deepEqual(now(), { away: false, muted: false });
    const after = sent.slice(from).filter((m) => m.t === 'voice.mute' || m.t === 'voice.talk');
    assert.deepEqual(after.map((m) => m.t), ['voice.mute', 'voice.talk'], 'unmuted, then the press');
    assert.equal(after[1].d.on, true);
    key('keyup', 'KeyV');

    setAway(true);
    pressButton();
    assert.deepEqual(now(), { away: false, muted: false }, "the call's button");
    releaseButton();

    deliver({ t: 'keys', d: { helper: true, covers: ['ptt'], keys: { ptt: { code: 'KeyV', label: 'V' } } } });
    setAway(true);
    deliver({ t: 'key', d: { action: 'ptt', down: true } });
    assert.deepEqual(now(), { away: false, muted: false }, 'the helper');
    deliver({ t: 'key', d: { action: 'ptt', down: false } });

    // A press while muted before Away ends it, and leaves them muted.
    setMuted(true);
    setAway(true);
    deliver({ t: 'key', d: { action: 'ptt', down: true } });
    deliver({ t: 'key', d: { action: 'ptt', down: false } });
    assert.deepEqual(now(), { away: false, muted: true });
    setMuted(false);
    deliver({ t: 'keys', d: { keys: {} } });
    leaveCall();
});

test('deafening keeps Away, and hearing again leaves the member away', async () => {
    setVoice({ mode: 'voice' });
    await join();
    setAway(true);
    setDeafened(true);
    assert.deepEqual(marks(), { den: 'den1', muted: true, deafened: true, away: true });
    setDeafened(false);
    assert.deepEqual(now(), { away: true, muted: true });
    assert.deepEqual(marks(), { den: 'den1', muted: true, deafened: false, away: true });
    setAway(false);
    assert.deepEqual(now(), { away: false, muted: false });
    leaveCall();
});

test("a move and a join again keep Away, and a join of the member's own puts the microphone back as it was", async () => {
    setVoice({ mode: 'voice' });
    await join();
    setAway(true);
    deliver({ t: 'call', d: { den: 'den1', channel: 'lounge', move_to: 'games' } });
    await tick(20);
    assert.equal(seen.call.channel, 'games');
    assert.deepEqual(now(), { away: true, muted: true }, 'moved');
    const moved = joins().at(-1).d;
    assert.equal(moved.channel, 'games');
    assert.equal(moved.muted, true);
    assert.equal(moved.away, true);

    // The den restarted: the page joins again, away as it was.
    deliver({ t: 'dens', d: { dens: [{ den_id: 'den1', state: 'connected' }] } });
    peers.at(-1).connectionState = 'connected';
    peers.at(-1).onconnectionstatechange();
    const before = joins().length;
    deliver({ t: 'call', d: { den: 'den1', channel: 'games', ended: 'failed' } });
    await tick(700);
    assert.equal(joins().length, before + 1, 'joined again');
    assert.equal(joins().at(-1).d.away, true);
    assert.equal(joins().at(-1).d.muted, true);

    // Leaving and joining again starts the member as they were before
    // Away.
    leaveCall();
    await join();
    assert.deepEqual(now(), { away: false, muted: false });
    assert.equal(joins().at(-1).d.muted, false);
    assert.ok(!joins().at(-1).d.away);

    // So does joining another call of their own while away.
    setAway(true);
    await join('games');
    assert.deepEqual(now(), { away: false, muted: false });
    leaveCall();
});

test("a click's join opens the call's pane, again for the call the member is in, and a move's join leaves the page where it is", async () => {
    setVoice({ mode: 'voice' });
    openCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    await tick(20);
    assert.equal(window.location.pathname, '/den/den1/lounge');
    const at = history.at;
    openCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    await tick(20);
    assert.equal(history.at, at, 'its own call again: the pane, as it was');
    nav().den('den1', '1', 'chan');
    await tick(20);
    assert.equal(window.location.pathname, '/den/den1/1');
    openCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    await tick(20);
    assert.equal(window.location.pathname, '/den/den1/lounge', 'its own call, from a channel');

    deliver({ t: 'call', d: { den: 'den1', channel: 'lounge', move_to: 'games' } });
    await tick(20);
    assert.equal(seen.call.channel, 'games');
    assert.equal(window.location.pathname, '/den/den1/lounge', "a move's join stays");
    leaveCall();
});
