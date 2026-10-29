import assert from 'node:assert/strict';
import { test } from 'node:test';

// A call's audio as the page makes it (M12), with stand-ins for what the
// browser gives it: its audio contexts, the microphone, the peer
// connection, media elements and the stream to the service. In a browser
// the page makes its call's contexts in the click that joins, before the
// microphone; where the service says audio may start without one, as in
// Android's app, it makes them once the microphone is open and plays the
// members' voices through the call's context, which Android then plays as
// a call's.

// The page's own waits, as for an offer that never comes, outlast the
// tests, and don't hold the run open.
const realSetTimeout = setTimeout;
globalThis.setTimeout = (fn, ms, ...args) => {
    const t = realSetTimeout(fn, ms, ...args);
    if (ms >= 1000) t.unref();
    return t;
};

// happened lists what the page asked of the browser, in order.
const happened = [];
const app = { dataset: { voiceWorklet: '/voice.worklet.js', rnnoiseModule: '/rnnoise.wasm' } };
const docListeners = {};
globalThis.document = {
    getElementById: (id) => (id === 'app' ? app : null),
    addEventListener: (type, fn) => (docListeners[type] ||= []).push(fn),
    hidden: false,
    visibilityState: 'visible',
    hasFocus: () => true,
    activeElement: null,
};
globalThis.window = { location: { protocol: 'http:', host: 'localhost:8484' }, addEventListener() {} };
const kept = new Map();
globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, v), removeItem: (k) => kept.delete(k) };
// The service answers nothing the call needs: RNNoise's module and the
// den's state aren't there, so the processor runs without RNNoise.
globalThis.fetch = async () => ({ ok: false, status: 404, headers: { get: () => null }, text: async () => '' });

class Socket {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = Socket.OPEN;
    sent = [];
    addEventListener() {}
    send(text) {
        this.sent.push(JSON.parse(text));
    }
}
globalThis.WebSocket = Socket;

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

// micWait, while set, holds the microphone back until it's called.
let micWait = null;
const mics = [];
Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: {
        userAgent: '',
        mediaDevices: {
            getUserMedia: async () => {
                if (micWait) await new Promise((resolve) => (micWait = resolve));
                const s = new Stream();
                mics.push(s);
                happened.push('microphone');
                return s;
            },
        },
    },
});

class AudioNode {
    to = [];
    connect(node) {
        this.to.push(node);
        return node;
    }
    disconnect() {
        this.to = [];
    }
}

const contexts = [];
globalThis.AudioContext = class {
    constructor(opts = {}) {
        this.sampleRate = opts.sampleRate || 44100;
        this.state = 'suspended';
        this.destination = new AudioNode();
        this.gains = [];
        this.audioWorklet = { addModule: async () => {} };
        contexts.push(this);
        happened.push(`context ${contexts.length}`);
    }
    async resume() {
        this.state = 'running';
    }
    async close() {
        this.state = 'closed';
    }
    createMediaStreamSource(stream) {
        return Object.assign(new AudioNode(), { stream });
    }
    createAnalyser() {
        return Object.assign(new AudioNode(), { fftSize: 2048, getFloatTimeDomainData: (b) => b.fill(0) });
    }
    createGain() {
        const g = Object.assign(new AudioNode(), { gain: { value: 1 } });
        this.gains.push(g);
        return g;
    }
    createMediaStreamDestination() {
        return Object.assign(new AudioNode(), { channelCount: 2, stream: new Stream() });
    }
};
globalThis.AudioWorkletNode = class extends AudioNode {
    port = { postMessage() {}, onmessage: null };
};

const peers = [];
globalThis.RTCPeerConnection = class {
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

const elements = [];
globalThis.Audio = class {
    volume = 1;
    muted = false;
    paused = true;
    srcObject = null;
    constructor() {
        elements.push(this);
    }
    play() {
        this.paused = false;
        return Promise.resolve();
    }
    pause() {
        this.paused = true;
    }
};

const { joinCall, leaveCall, setCallVolume, setDeafened, setVolume } = await import('../assets/js/src/voice.js');

// joined joins a call, as the service's form says, and has the den send a
// member's audio; it returns what the call made of it.
async function joined(autoplay) {
    app.dataset.autoplay = String(autoplay);
    happened.length = 0;
    contexts.length = 0;
    elements.length = 0;
    await joinCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    const member = new Stream([new Track()], '7');
    peers.at(-1).ontrack({ streams: [member] });
    return { order: [...happened], element: elements.at(-1) };
}

test("in a browser, the call's contexts start in the click, before the microphone, and the voices play in their elements", async () => {
    const { order, element } = await joined(false);
    assert.deepEqual(order, ['context 1', 'context 2', 'microphone'], "the call's context, then the processor's");
    assert.equal(element.muted, false);
    assert.equal(element.paused, false);
    assert.equal(contexts[0].gains.length, 0, 'no voice plays through a context');
    setVolume('den1', '7', 0.3);
    assert.equal(element.volume, 0.3);
    setCallVolume(0.5);
    assert.equal(element.volume, 0.15, "the call's volume, under the member's own");
    setCallVolume(1);
    setDeafened(true);
    assert.equal(element.muted, true);
    setDeafened(false);
    setVolume('den1', '7', 1);
    leaveCall();
});

test("where audio may start without a click, the call's contexts start once the microphone is open, and the voices play through the call's", async () => {
    const { order, element } = await joined(true);
    assert.deepEqual(order, ['microphone', 'context 1', 'context 2'], "the processor's context, then the call's");
    const [processor, call] = contexts;
    assert.equal(processor.sampleRate, 48000);
    assert.equal(element.muted, true, 'the element plays the stream, for Chromium to decode it, and plays nothing itself');
    assert.equal(element.paused, false);
    assert.equal(call.gains.length, 1);
    const [gain] = call.gains;
    assert.deepEqual(gain.to, [call.destination]);
    assert.equal(gain.gain.value, 1);
    setVolume('den1', '7', 0.3);
    assert.equal(gain.gain.value, 0.3);
    setCallVolume(0.5);
    assert.equal(gain.gain.value, 0.15, "the call's volume, under the member's own");
    assert.equal(kept.get('DENS_CALL_VOLUME'), '0.5', 'kept by the browser');
    setCallVolume(1);
    assert.equal(kept.has('DENS_CALL_VOLUME'), false, 'full, as if never chosen');
    setDeafened(true);
    assert.equal(gain.gain.value, 0);
    setDeafened(false);
    assert.equal(gain.gain.value, 0.3);
    assert.equal(element.muted, true);
    setVolume('den1', '7', 1);
    leaveCall();
    assert.deepEqual(gain.to, [], 'leaving disconnects the voice');
    assert.equal(call.state, 'closed');
});

// Chromium pauses a frozen page's media elements and never plays them
// again, which left a call on Android silent once its page thawed.
test("a call's elements play again as the page thaws from a freeze", async () => {
    const { element } = await joined(false);
    element.pause();
    for (const fn of docListeners.freeze) fn();
    for (const fn of docListeners.resume) fn();
    assert.equal(element.paused, false);
    leaveCall();
});

test('a microphone that opens once its call has ended closes again', async () => {
    app.dataset.autoplay = 'true';
    micWait = true;
    const joining = joinCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    // The member leaves while the browser asks for the microphone.
    await new Promise((resolve) => setTimeout(resolve, 10));
    leaveCall();
    const give = micWait;
    micWait = null;
    give();
    await joining;
    assert.equal(mics.at(-1).getTracks()[0].readyState, 'ended');
});
