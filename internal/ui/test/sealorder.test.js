import assert from 'node:assert/strict';
import { test } from 'node:test';

// A sealed call's senders as Chromium treats them (M18): a sender made as an
// offer is applied takes a transform only until the end of the task the
// offer was applied in, and one it gets any later is taken without an error
// and never runs, the sender sending around it. The page seals every
// section it sends on in that task, its microphone's and its share's
// screen's and sound's, before any track goes on, and puts no track on a
// sender it sealed any later. It holds what each sender sent against the
// frames its worker saw on that track, and stops one sent around its seal,
// saying why, while a sender just attached, a paused screen and a muted
// voice stop nothing.

const realSetTimeout = setTimeout;
// A task the page sets for now runs at once while late holds, as if its
// offer's task had ended before the page sealed anything.
let late = false;
globalThis.setTimeout = (fn, ms, ...args) => {
    if (late && !ms) {
        fn(...args);
        return null;
    }
    const t = realSetTimeout(fn, ms, ...args);
    if (ms >= 1000) t.unref();
    return t;
};
// The check of the senders' seals, every second, runs when a test says.
const sealChecks = new Set();
const realSetInterval = setInterval;
const realClearInterval = clearInterval;
globalThis.setInterval = (fn, ms, ...args) => {
    if (ms === 1000) {
        const h = { fn };
        sealChecks.add(h);
        return h;
    }
    const t = realSetInterval(fn, ms, ...args);
    t.unref();
    return t;
};
globalThis.clearInterval = (h) => (sealChecks.has(h) ? sealChecks.delete(h) : realClearInterval(h));

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
globalThis.fetch = async (url) => {
    if (String(url).endsWith('/api/dens/den1/state')) {
        const body = JSON.stringify({ me: { id: '1', role: 'member' }, call_limits: {}, calls: [], members: [], sounds: [], identities: {}, sealed_calls: true, seal: true });
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
    constructor(kind = 'audio') {
        this.kind = kind;
    }
    stop() {
        this.readyState = 'ended';
    }
    getSettings() {
        return { width: 1920, height: 1080 };
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
        return this.tracks.filter((t) => t.kind === 'audio');
    }
    getVideoTracks() {
        return this.tracks.filter((t) => t.kind === 'video');
    }
}
globalThis.MediaStream = Stream;
// What the member shares: a screen, with its sound unless a test says.
let shareSound = true;
Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: {
        userAgent: '',
        mediaDevices: {
            getUserMedia: async () => new Stream(),
            getDisplayMedia: async () => new Stream(shareSound ? [new Track('video'), new Track('audio')] : [new Track('video')], 'display'),
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

const { TRACKS, freshCounts } = await import('../assets/js/sframe.worker.js');

// The worker counts the frames each sealed sender hands it, by track, and
// answers the page's question for its counts, as the real one does.
let counts = freshCounts();
const workers = [];
globalThis.Worker = class {
    constructor(url) {
        this.url = url;
        this.posted = [];
        this.onmessage = null;
        workers.push(this);
    }
    postMessage(m) {
        this.posted.push(m);
        if (m.t === 'end') counts = freshCounts();
        if (m.t === 'counts') realSetTimeout(() => this.onmessage?.({ data: { t: 'counts', ...structuredClone(counts) } }), 0);
    }
};
class Transform {
    constructor(worker, options) {
        this.worker = worker;
        this.options = options;
    }
}
globalThis.RTCRtpScriptTransform = Transform;

// A sender as Chromium's acts: a transform runs only where it came while
// the sender was open, in the task of the offer that made it. Its track
// goes on in a task of its own, as replaceTrack's does, and it notes, as
// each track went on, whether it went sealed, around a transform, or in
// the clear.
class Sender {
    #transform = null;
    open = true;
    runs = false;
    track = null;
    went = [];
    packets = 0;
    frames = 0;
    get transform() {
        return this.#transform;
    }
    set transform(t) {
        this.#transform = t;
        this.runs = this.open && !!t;
    }
    async replaceTrack(track) {
        await new Promise((resolve) => realSetTimeout(resolve, 1));
        this.track = track;
        if (track) this.went.push(this.runs ? 'sealed' : this.#transform ? 'around' : 'clear');
    }
    getStats() {
        return Promise.resolve(new Map([['out', { type: 'outbound-rtp', packetsSent: this.packets, framesEncoded: this.frames }]]));
    }
    getParameters() {
        return { encodings: [{}] };
    }
    setParameters() {
        return Promise.resolve();
    }
}

// The peer connection applies an offer in a task of its own, making a
// transceiver for each section it hasn't seen, whose sender closes as that
// task ends; where a test asks, as a later Chromium might, a section's
// sender is made closed.
const peers = [];
globalThis.RTCPeerConnection = class {
    closedAtOnce = new Set();
    constructor() {
        this.transceivers = [];
        peers.push(this);
    }
    getTransceivers() {
        return this.transceivers;
    }
    async setRemoteDescription({ sdp }) {
        await new Promise((resolve) => realSetTimeout(resolve, 1));
        const made = [];
        for (const section of sdp.split('\r\nm=').slice(1)) {
            const kind = section.slice(0, section.indexOf(' '));
            const mid = /a=mid:(\S+)/.exec(section)[1];
            if (this.transceivers.some((t) => t.mid === mid)) continue;
            const sender = new Sender();
            sender.open = !this.closedAtOnce.has(mid);
            this.transceivers.push({ mid, kind, direction: 'recvonly', sender, receiver: { transform: null } });
            made.push(sender);
        }
        setImmediate(() => made.forEach((s) => (s.open = false)));
    }
    async setLocalDescription() {
        this.localDescription = { sdp: 'v=0\r\n' };
    }
    getStats() {
        return Promise.resolve(new Map());
    }
    close() {}
};

const voice = await import('../assets/js/src/voice.js');
const {
    SEAL_LEAST, SOUND_TRACK, SCREEN_TRACK, TRACK_NAMES, UNSEALED_AGAIN, UNSEALED_SCREEN, UNSEALED_SOUND, UNSEALED_VOICE, VOICE_TRACK, frameNotes,
    joinCall, leaveCall, onCall, senderSent, sendingTransceivers, sentAround, startShare, stopShare,
} = voice;
let state = null;
onCall((s) => (state = s));
const settle = () => new Promise((resolve) => realSetTimeout(resolve, 20));

// offer is the den's offer: the microphone's section, the den's first,
// another member's voice, and the share's screen and sound once it has
// added them.
let version = 0;
function offer(share) {
    const sections = [['audio', '0', 'recvonly'], ['audio', '1', 'sendonly']];
    if (share) sections.push(['video', '2', 'recvonly'], ['audio', '3', 'recvonly']);
    const sdp = `v=0\r\n${sections.map(([kind, mid, dir]) => `m=${kind} 9 UDP/TLS/RTP/SAVPF 111\r\na=mid:${mid}\r\na=${dir}`).join('\r\n')}\r\n`;
    deliver({ t: 'call', d: { den: 'den1', channel: 'lounge', offer: { version: ++version, sdp, udp_port: 1, tcp_port: 2 } } });
}

async function join() {
    await joinCall('den1', 'lounge', { den: 'Den', channel: 'Lounge' });
    version = 0;
    return peers.at(-1);
}

const transceiver = (pc, mid) => pc.transceivers.find((t) => t.mid === mid);

// sending stands in for a moment of the page's senders sending: each with a
// track, but those a test pauses, sends n of a screen's frames or a voice's
// or a sound's packets, which reach the worker where the sender's transform
// runs; frames are what reach it, where a voice sends fewer packets, as one
// muted or held back sends DTX's.
function sending(pc, n = 50, { paused = [], packets = n } = {}) {
    for (const t of pc.transceivers) {
        const s = t.sender;
        if (!s.track || paused.includes(t.mid)) continue;
        if (t.kind === 'video') s.frames += n;
        else s.packets += packets;
        if (s.runs) counts[TRACKS[s.transform.options.track]].seen += n;
    }
}

// sealCheck runs the page's check of its senders as its next second comes.
async function sealCheck() {
    for (const h of [...sealChecks]) h.fn();
    await settle();
}

const unsealedNotes = () => sent('voice.note').map((m) => m.d.note).filter((n) => n.what === 'unsealed');

test('the sections the page sends on: the microphone\'s, and the share\'s once the den adds them', () => {
    const t = (mid) => ({ mid });
    const ts = [t('0'), t('1'), t('2'), t('3')];
    assert.deepEqual(sendingTransceivers(ts, { screen: null, sound: null }).map(([x, track]) => [x.mid, track]), [['0', VOICE_TRACK]]);
    assert.deepEqual(sendingTransceivers(ts, { screen: '2', sound: '3' }).map(([x, track]) => [x.mid, track]),
        [['0', VOICE_TRACK], ['2', SCREEN_TRACK], ['3', SOUND_TRACK]]);
    assert.deepEqual(sendingTransceivers(ts, { screen: '2', sound: null }).map(([x, track]) => [x.mid, track]), [['0', VOICE_TRACK], ['2', SCREEN_TRACK]],
        'a share without sound');
    assert.deepEqual(sendingTransceivers(ts, { screen: '9', sound: null }).map(([x]) => x.mid), ['0'], 'a section the browser lacks');
    assert.deepEqual(sendingTransceivers([], { screen: null, sound: null }), []);
    assert.deepEqual(TRACK_NAMES, TRACKS);
});

test('every sender is sealed in its offer\'s task, before its track goes on: the microphone, then the screen and its sound', async () => {
    const pc = await join();
    offer(false);
    await settle();
    const mic = transceiver(pc, '0');
    assert.equal(mic.direction, 'sendonly');
    assert.deepEqual(mic.sender.went, ['sealed']);
    assert.deepEqual(mic.sender.transform.options, { side: 'send', track: VOICE_TRACK });
    assert.equal(transceiver(pc, '1').sender.transform, null, "another member's voice isn't the page's to seal");

    // The share's first offer adds its sections, which the page seals as the
    // offer is applied, before the screen's track goes on, whose wait would
    // otherwise come before the sound's seal.
    await startShare();
    assert.equal(sent('voice.share').at(-1).d.sound, true);
    offer(true);
    await settle();
    for (const [mid, track] of [['2', SCREEN_TRACK], ['3', SOUND_TRACK]]) {
        const t = transceiver(pc, mid);
        assert.equal(t.direction, 'sendonly');
        assert.deepEqual(t.sender.transform.options, { side: 'send', track });
        assert.deepEqual(t.sender.went, ['sealed'], `the ${TRACK_NAMES[track]}'s sender`);
    }
    assert.ok(sent('voice.answer').length >= 2);

    // A later share in the call finds its senders sealed already.
    stopShare();
    await settle();
    await startShare();
    await settle();
    assert.deepEqual(transceiver(pc, '2').sender.went, ['sealed', 'sealed']);
    assert.deepEqual(transceiver(pc, '3').sender.went, ['sealed', 'sealed']);
    leaveCall();
});

test("a share started before the call's first offer goes on sealed, all three senders in one offer's task", async () => {
    const pc = await join();
    await startShare();
    offer(true);
    await settle();
    for (const mid of ['0', '2', '3']) assert.deepEqual(transceiver(pc, mid).sender.went, ['sealed'], `section ${mid}`);
    leaveCall();
});

test("no track goes on a sender the page sealed after its offer's task", async () => {
    const pc = await join();
    await startShare();
    late = true;
    offer(true);
    await settle();
    late = false;
    for (const mid of ['0', '2', '3']) {
        const s = transceiver(pc, mid).sender;
        assert.ok(s.transform, `section ${mid} sealed`);
        assert.deepEqual(s.went, [], `section ${mid} silent rather than in the clear`);
    }
    leaveCall();
});

test("which sender was sent around its seal: packets without frames, against what was sent and seen as the page last looked", () => {
    assert.equal(sentAround({ sent: 10, seen: 40 }, { sent: 10 + SEAL_LEAST, seen: 40 }), true);
    assert.equal(sentAround({ sent: 10, seen: 40 }, { sent: 10 + SEAL_LEAST - 1, seen: 40 }), false, 'too few to tell');
    assert.equal(sentAround({ sent: 10, seen: 40 }, { sent: 60, seen: 41 }), false, 'the worker saw its frames');
    assert.equal(sentAround({ sent: 10, seen: 40 }, { sent: 10, seen: 40 }), false, 'nothing sent, as a paused screen');
    const stats = (s) => new Map([['a', { type: 'remote-inbound-rtp', packetsReceived: 9 }], ['b', { type: 'outbound-rtp', ...s }]]);
    assert.equal(senderSent(stats({ packetsSent: 120, framesEncoded: 30 }), SCREEN_TRACK), 30, "a screen's frames");
    assert.equal(senderSent(stats({ packetsSent: 120 }), SCREEN_TRACK), 120, 'packets where a browser counts no frames');
    assert.equal(senderSent(stats({ packetsSent: 50 }), SOUND_TRACK), 50);
    assert.equal(senderSent(new Map(), VOICE_TRACK), 0, 'nothing sent yet');
});

test("a share's sound sent around its seal stops within a second, the share going on without it, and says why", async () => {
    const pc = await join();
    pc.closedAtOnce.add('3');
    offer(false);
    await settle();
    await startShare();
    offer(true);
    await settle();
    const sound = transceiver(pc, '3').sender;
    assert.deepEqual(sound.went, ['around']);
    // Just attached, nothing has gone yet, and nothing stops.
    await sealCheck();
    assert.ok(sound.track);
    sending(pc);
    await sealCheck();
    assert.equal(sound.track, null, 'the sound came off its sender');
    assert.equal(state.share.status !== undefined, true, 'the share goes on');
    assert.equal(state.share.sound, false);
    assert.equal(state.share.unsealed, true);
    assert.equal(state.shareNotice.text, UNSEALED_SOUND);
    assert.deepEqual(sent('voice.share').at(-1).d, { den: 'den1', on: true, sound: false });
    assert.deepEqual(unsealedNotes().at(-1), { what: 'unsealed', track: 'sound', sent: 50 });
    assert.ok(transceiver(pc, '2').sender.track, 'the screen goes on');
    assert.ok(transceiver(pc, '0').sender.track, 'the voice goes on');
    // A share later in the call goes without sound, since that sender
    // carries nothing more.
    stopShare();
    await settle();
    await startShare();
    await settle();
    assert.equal(state.share.sound, false);
    assert.equal(state.share.unsealed, true);
    assert.deepEqual(sent('voice.share').at(-1).d, { den: 'den1', on: true, sound: false });
    assert.equal(sound.track, null);
    leaveCall();
});

test('a screen sent around its seal ends the share, and the screen shares no more in that call', async () => {
    const pc = await join();
    pc.closedAtOnce.add('2');
    offer(false);
    await settle();
    await startShare();
    offer(true);
    await settle();
    const screen = transceiver(pc, '2').sender;
    assert.deepEqual(screen.went, ['around']);
    // Paused while nobody watches, it sends nothing, and nothing stops.
    sending(pc, 50, { paused: ['2'] });
    await sealCheck();
    assert.ok(screen.track);
    assert.equal(state.share.status, 'starting');
    sending(pc);
    await sealCheck();
    assert.equal(state.share, null);
    assert.equal(state.shareNotice.text, UNSEALED_SCREEN);
    assert.equal(sent('voice.share').at(-1).d.on, false);
    assert.deepEqual(unsealedNotes().at(-1), { what: 'unsealed', track: 'screen', sent: 50 });
    assert.ok(state.call, 'the call goes on');
    await startShare();
    assert.equal(state.share, null);
    assert.equal(state.shareNotice.text, UNSEALED_AGAIN);
    leaveCall();
});

test('a voice sent around its seal leaves the call, and says why; a muted one sealed stops nothing', async () => {
    let pc = await join();
    offer(false);
    await settle();
    // A muted voice, sealed: few packets, as DTX sends, but its frames of
    // silence reach the worker.
    sending(pc, 50, { packets: 2 });
    await sealCheck();
    sending(pc, 50, { packets: 2 });
    await sealCheck();
    assert.equal(state.call.status !== undefined, true);
    assert.equal(transceiver(pc, '0').sender.went.length, 1);
    assert.equal(sealChecks.size, 0, 'once the worker has seen its frames, the voice is checked only with the call\'s summaries');
    leaveCall();

    pc = await join();
    pc.closedAtOnce.add('0');
    offer(false);
    await settle();
    assert.deepEqual(transceiver(pc, '0').sender.went, ['around']);
    sending(pc, 50, { packets: 2 });
    await sealCheck();
    assert.ok(state.call, 'two packets are too few to tell');
    sending(pc, 50, { packets: 2 });
    await sealCheck();
    assert.equal(state.call, null);
    assert.equal(state.notice.text, UNSEALED_VOICE);
    assert.deepEqual(sent('voice.leave').at(-1).d, { den: 'den1' });
    assert.deepEqual(unsealedNotes().at(-1), { what: 'unsealed', track: 'voice', sent: 4 });
});

test("the call's notes count each track's frames, and why the worker dropped each", () => {
    const c = freshCounts();
    c.voice = { seen: 900, sealed: 450, opened: 300, empty: 4, dropped: { clear: 0, lacking: 2, track: 0, replay: 1, unopened: 0 } };
    c.sound.dropped.clear = 50;
    c.unnamed = 3;
    assert.deepEqual(frameNotes(c), {
        unnamed: 3,
        voice: { sealed: 450, opened: 300, empty: 4, dropped: { clear: 0, lacking: 2, track: 0, replay: 1, unopened: 0 } },
        screen: { sealed: 0, opened: 0, empty: 0, dropped: { clear: 0, lacking: 0, track: 0, replay: 0, unopened: 0 } },
        sound: { sealed: 0, opened: 0, empty: 0, dropped: { clear: 50, lacking: 0, track: 0, replay: 0, unopened: 0 } },
    });
});
