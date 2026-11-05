import assert from 'node:assert/strict';
import { test } from 'node:test';

// A sealed call as the page joins it (M16.2), with stand-ins for what the
// browser gives it: the microphone, the peer connection, its senders and
// receivers, Encoded Transform and the worker, and the stream to the
// service. A den that seals its calls takes the page only where it seals
// what it sends: its microphone goes on sealed, every receiver's frames go
// through the worker, which opens its member's and passes the soundboard's,
// and the call's keys reach the worker whole; where the browser can't seal,
// or the device has no seal, the page says why rather than join.

const realSetTimeout = setTimeout;
globalThis.setTimeout = (fn, ms, ...args) => {
    const t = realSetTimeout(fn, ms, ...args);
    if (ms >= 1000) t.unref();
    return t;
};

const app = { dataset: { voiceWorklet: '/voice.worklet.js', rnnoiseModule: '/rnnoise.wasm', sframeWorker: '/sframe.worker.js' } };
globalThis.document = {
    getElementById: (id) => (id === 'app' ? app : null),
    addEventListener() {},
    hidden: false,
    visibilityState: 'visible',
    hasFocus: () => true,
};
globalThis.window = { location: { protocol: 'http:', host: 'localhost:8484' }, addEventListener() {} };
const kept = new Map();
globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, v), removeItem: (k) => kept.delete(k) };

// den is what the service says of the call's den: whether it seals its
// calls, and whether this device holds the member's seal.
const den = { sealed_calls: true, seal: true };
globalThis.fetch = async (url) => {
    if (String(url).endsWith('/api/dens/den1/state')) {
        const body = JSON.stringify({ me: { id: '1', role: 'member' }, call_limits: {}, calls: [], members: [], sounds: [], identities: {}, ...den });
        return { ok: true, status: 200, headers: { get: () => 'application/json' }, json: async () => JSON.parse(body), text: async () => body };
    }
    return { ok: false, status: 404, headers: { get: () => null }, text: async () => '' };
};

const sockets = [];
class Socket {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = Socket.OPEN;
    sent = [];
    listeners = {};
    constructor() {
        sockets.push(this);
    }
    addEventListener(type, fn) {
        (this.listeners[type] ||= []).push(fn);
    }
    send(text) {
        this.sent.push(JSON.parse(text));
    }
}
globalThis.WebSocket = Socket;
const deliver = (message) => (sockets.at(-1).listeners.message || []).forEach((fn) => fn({ data: JSON.stringify(message) }));
const sent = (t) => sockets.flatMap((s) => s.sent).filter((m) => m.t === t);

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
let mics = 0;
Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: {
        userAgent: '',
        mediaDevices: {
            getUserMedia: async () => {
                mics++;
                return new Stream();
            },
        },
    },
});

class AudioNode {
    connect(node) {
        return node;
    }
    disconnect() {}
}
globalThis.AudioContext = class {
    sampleRate = 48000;
    state = 'suspended';
    destination = new AudioNode();
    audioWorklet = { addModule: async () => {} };
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
    port = { postMessage() {}, onmessage: null };
};

// The worker the page makes, which keeps what the page posts it, and the
// transforms, which keep their worker and options.
const workers = [];
globalThis.Worker = class {
    constructor(url, options) {
        this.url = url;
        this.options = options;
        this.posted = [];
        workers.push(this);
    }
    postMessage(m) {
        this.posted.push(m);
    }
};
const posted = (t) => workers.flatMap((w) => w.posted).filter((m) => m.t === t);
class Transform {
    constructor(worker, options) {
        this.worker = worker;
        this.options = options;
    }
}
globalThis.RTCRtpScriptTransform = Transform;

// A sender notes whether it was sealed as each track went on it.
class Sender {
    transform = null;
    track = null;
    went = [];
    async replaceTrack(track) {
        this.went.push(this.transform ? 'sealed' : 'clear');
        this.track = track;
    }
    getParameters() {
        return { encodings: [{}] };
    }
    setParameters() {
        return Promise.resolve();
    }
}
const peers = [];
globalThis.RTCPeerConnection = class {
    constructor() {
        this.transceivers = [{ mid: '0', direction: 'recvonly', sender: new Sender() }];
        peers.push(this);
    }
    getTransceivers() {
        return this.transceivers;
    }
    async setRemoteDescription() {}
    async setLocalDescription() {
        this.localDescription = { sdp: 'v=0\r\n' };
    }
    getStats() {
        return Promise.resolve(new Map());
    }
    close() {}
};
globalThis.Audio = class {
    volume = 1;
    muted = false;
    paused = true;
    srcObject = null;
    play() {
        this.paused = false;
        return Promise.resolve();
    }
    pause() {
        this.paused = true;
    }
};

const voice = await import('../assets/js/src/voice.js');
const { NO_SEAL, NO_SEALING, NO_SEALING_APP, fromBase64url, joinCall, leaveCall, onCall, sealedJoin, targetOf } = voice;
let state = null;
onCall((s) => (state = s));
const settle = () => new Promise((resolve) => realSetTimeout(resolve, 20));

test('why a page joins no call in a den that seals them, and joins one in a den that doesn\'t', () => {
    assert.equal(sealedJoin(null), null, "a den the page couldn't load is left to refuse");
    assert.equal(sealedJoin({ sealed: false }, false), null, 'an older den takes a call in the clear');
    assert.deepEqual(sealedJoin({ sealed: true, seal: true }, false, false), { text: NO_SEALING });
    assert.deepEqual(sealedJoin({ sealed: true, seal: true }, false, true), { text: NO_SEALING_APP, update: true });
    assert.deepEqual(sealedJoin({ sealed: true, seal: false }, true), { text: NO_SEAL, seal: true });
    assert.equal(sealedJoin({ sealed: true, seal: true }, true), null);
});

test("each stream's frames go to whose track it carries, and the soundboard's to no one's", () => {
    assert.deepEqual(targetOf('7', 'audio'), { member: '7', track: 0 });
    assert.deepEqual(targetOf('screen-7', 'video'), { member: '7', track: 1 });
    assert.deepEqual(targetOf('screen-7', 'audio'), { member: '7', track: 2 });
    assert.deepEqual(targetOf('soundboard', 'audio'), { member: '', track: 0 });
    assert.deepEqual([...fromBase64url('AAEC_-8')], [0, 1, 2, 255, 239]);
});

test('a browser that can\'t seal, or a device without the seal, says why and opens no microphone', async () => {
    globalThis.RTCRtpScriptTransform = undefined;
    await joinCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    assert.equal(state.call, null);
    assert.equal(state.notice.text, NO_SEALING);
    globalThis.RTCRtpScriptTransform = Transform;
    den.seal = false;
    await joinCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    assert.equal(state.notice.text, NO_SEAL);
    assert.equal(state.notice.seal, true);
    assert.equal(state.notice.den, 'den1');
    den.seal = true;
    assert.equal(mics, 0);
    assert.equal(sent('voice.join').length, 0);
});

test('a sealed call: the microphone goes on sealed, every receiver through the worker, and the keys to it whole', async () => {
    await joinCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    assert.equal(state.call.sealed, true);
    assert.deepEqual(sent('voice.join').at(-1).d, { den: 'den1', channel: 'lounge', muted: false, deafened: false, sealed: true });
    deliver({ t: 'call', d: { den: 'den1', channel: 'lounge', offer: { version: 1, sdp: 'v=0\r\n', udp_port: 1, tcp_port: 2 } } });
    await settle();
    // The worker comes with the call's first sealed sender.
    const [worker] = workers;
    assert.equal(worker.url, '/sframe.worker.js');
    assert.deepEqual(worker.options, { type: 'module' });
    const pc = peers.at(-1);
    const mine = pc.transceivers[0].sender;
    assert.deepEqual(mine.went, ['sealed'], 'sealed before the microphone went on');
    assert.deepEqual(mine.transform.options, { side: 'send', track: 0 });
    assert.equal(mine.transform.worker, worker);

    // Each receiver's frames go through the worker, named by the page's ID
    // for it, as whose its stream says; the soundboard's pass.
    const receivers = {};
    for (const [id, kind] of [['7', 'audio'], ['screen-7', 'video'], ['soundboard', 'audio']]) {
        receivers[id] = { transform: null };
        pc.ontrack({ streams: [new Stream([new Track()], id)], receiver: receivers[id], track: { kind } });
    }
    const targets = posted('target');
    assert.deepEqual(targets.map(({ member, track }) => [member, track]), [['7', 0], ['7', 1], ['', 0]]);
    for (const [i, id] of ['7', 'screen-7', 'soundboard'].entries()) {
        assert.deepEqual(receivers[id].transform.options, { side: 'receive', id: targets[i].id });
    }
    // A section the den gives another member keeps its transform, which
    // the page points at them.
    const reused = receivers['7'].transform;
    pc.ontrack({ streams: [new Stream([new Track()], '8')], receiver: receivers['7'], track: { kind: 'audio' } });
    assert.equal(receivers['7'].transform, reused);
    assert.deepEqual(posted('target').at(-1), { t: 'target', id: targets[0].id, member: '8', track: 0 });

    // The keys reach the worker whole, as bytes.
    const key = 'AAECAwQFBgcICQoLDA0ODw';
    deliver({ t: 'call', d: { den: 'den1', channel: 'lounge', keys: { own: { n: 5, key }, members: { 8: { n: 9, key } } } } });
    const keys = posted('keys').at(-1);
    assert.equal(keys.own.n, 5);
    assert.deepEqual([...keys.own.key], [...Array(16).keys()]);
    assert.deepEqual(Object.keys(keys.members), ['8']);
    // A frame under a key the page lacks has it ask again, at most every
    // half second.
    worker.onmessage({ data: { t: 'lack', member: '8' } });
    worker.onmessage({ data: { t: 'lack', member: '8' } });
    assert.deepEqual(sent('voice.keys').map((m) => m.d), [{ den: 'den1' }]);

    // Leaving forgets the keys.
    leaveCall();
    assert.deepEqual(posted('end').length >= 1, true);
    assert.equal(workers.length, 1, 'one worker, kept for the next call');
});

test("a call in a den that doesn't seal goes in the clear, as it always has", async () => {
    den.sealed_calls = false;
    const before = posted('target').length;
    await joinCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    assert.equal(state.call.sealed, false);
    assert.equal(sent('voice.join').at(-1).d.sealed, false);
    deliver({ t: 'call', d: { den: 'den1', channel: 'lounge', offer: { version: 1, sdp: 'v=0\r\n', udp_port: 1, tcp_port: 2 } } });
    await settle();
    const pc = peers.at(-1);
    assert.deepEqual(pc.transceivers[0].sender.went, ['clear']);
    const rx = { transform: null };
    pc.ontrack({ streams: [new Stream([new Track()], '7')], receiver: rx, track: { kind: 'audio' } });
    assert.equal(rx.transform, null);
    assert.equal(posted('target').length, before);

    // The den updates to seal its calls, which ends the call as it
    // restarts: the page joins it again, sealed.
    deliver({ t: 'dens', d: { dens: [{ den_id: 'den1', state: 'connected' }] } });
    pc.connectionState = 'connected';
    pc.onconnectionstatechange();
    deliver({ t: 'den', d: { den: 'den1', events: [{ t: 'den.updated', d: { call_limits: {}, sealed_calls: true } }] } });
    deliver({ t: 'call', d: { den: 'den1', channel: 'lounge', ended: 'failed' } });
    await new Promise((resolve) => realSetTimeout(resolve, 700));
    assert.equal(sent('voice.join').at(-1).d.sealed, true);
    assert.equal(state.call.sealed, true);
    leaveCall();
    den.sealed_calls = true;
});
