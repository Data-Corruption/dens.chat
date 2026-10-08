// The call this page holds (M2): its microphone, its peer connection to
// the den, and the audio of everyone else in the call. The page signals
// over its event stream, and the service on this computer relays to the
// den, writing into each offer where the den's media goes. So the page
// names no STUN or TURN server, and a call reaches nothing but the den.
// The den makes every offer; the page only answers.
//
// From M3 a call rides out a dropped connection: when the den's connection
// drops, the den holds the call, and this page keeps its peer connection
// and takes the call back once the den is back. When the browser's own
// connection to the den's media ports breaks, the page asks for an ICE
// restart. The page also measures who is speaking, from the audio it plays,
// plays each member as loud as the member here chose, and runs RNNoise
// between the microphone and the call unless they turned it off.

import { onConnection, onEvent, sendCall } from './events.js';

const MIC = 'DENS_MIC';
const SPEAKER = 'DENS_SPEAKER';
const MUTED = 'DENS_MUTED';
const RNNOISE = 'DENS_RNNOISE';
const VOLUMES = 'DENS_VOLUMES';
// A call that drops joins again, as the den and this computer's service
// come back, this many times within this long, before it gives up.
const REJOINS = 5;
const REJOIN_WINDOW = 2 * 60 * 1000;
// A join the den doesn't offer anything for within this long has failed,
// and so has a resume it doesn't answer.
const OFFER_WAIT = 20 * 1000;
const RESUME_WAIT = 10 * 1000;
// A connection disconnected this long asks for an ICE restart, and one
// still not up this long after asking joins again.
const RESTART_AFTER = 2000;
const RESTART_WAIT = 15 * 1000;
// Speaking: a level above this, in dBFS, measured this often, and held
// this long after it drops.
const SPEAKING_DB = -50;
const METER_MS = 100;
const SPEAKING_HOLD = 400;

function stored(key) {
    try {
        return localStorage.getItem(key) || '';
    } catch {
        // Storage can be unavailable; defaults stand in.
        return '';
    }
}

function store(key, value) {
    try {
        if (value) localStorage.setItem(key, value);
        else localStorage.removeItem(key);
    } catch {
        // As above.
    }
}

const listeners = new Set();
// call is the page's call, or null. label names its den and channel for
// the page to show; ports are the den's media ports, from its offers.
let call = null;
// notice says why the last call ended, when that's worth saying.
let notice = null;
let pc = null;
// mic is the microphone's stream, and sent the track the call sends: the
// microphone's own, or what RNNoise makes of it.
let mic = null;
let sent = null;
let cleaner = null;
// audios plays each other member's stream, by their member ID: the den
// labels the stream it sends their audio on with it.
const audios = new Map();
// meters measure each member's audio, and this page's own as sent ("self").
let meterContext = null;
const meters = new Map();
let meterTimer = null;
let speaking = new Set();
const lastLoud = new Map();
// What the page last heard about each den's connection, and its own
// stream to the service.
let denStates = new Map();
let live = true;
let rejoinTimer = null;
let started = false;
// volumes is how loud each member plays here, by den and member.
let volumes = readVolumes(stored(VOLUMES));

function start() {
    if (started) return;
    started = true;
    onEvent(handle);
    onConnection((up) => {
        live = up;
        // The service's stream dropped, and the den's connection with it:
        // the den holds the call, which this page takes back when both are
        // back.
        if (!up && call && call.status !== 'reconnecting') hold();
        tryResume();
        tryRejoin();
    });
}

function snapshot() {
    return {
        call: call && { den: call.den, channel: call.channel, label: call.label, status: call.status, muted: call.muted },
        notice,
        speaking,
        volumes,
    };
}

function notify() {
    const s = snapshot();
    listeners.forEach((fn) => fn(s));
}

// onCall calls fn with the call, the last notice, who is speaking and how
// loud each member plays, now and on every change; it returns a function
// that stops.
export function onCall(fn) {
    start();
    listeners.add(fn);
    fn(snapshot());
    return () => listeners.delete(fn);
}

// joinCall joins a voice channel's call, leaving any other. label holds
// the den's and the channel's names, to show while the call lasts.
export async function joinCall(den, channel, label) {
    start();
    if (call && call.den === den && call.channel === channel && call.status !== 'reconnecting') return;
    clearTimeout(rejoinTimer);
    rejoinTimer = null;
    if (call) clearTimers(call);
    closePeer();
    notice = null;
    call = { den, channel, label, status: 'joining', muted: stored(MUTED) === '1', attempts: 0, connected: false };
    // The click that joins lets audio start, so the meters' context starts
    // here.
    startMeters();
    notify();
    await connect();
}

// leaveCall hangs up.
export function leaveCall() {
    if (!call) return;
    sendCall('voice.leave', { den: call.den });
    finish(null);
}

export function setMuted(muted) {
    store(MUTED, muted ? '1' : '');
    if (!call) return;
    call.muted = muted;
    if (sent) sent.enabled = !muted;
    sendCall('voice.mute', { den: call.den, muted });
    notify();
}

export function clearNotice() {
    notice = null;
    notify();
}

async function connect() {
    const c = call;
    try {
        if (!mic) await openMic();
    } catch (e) {
        if (call === c) finish({ text: micProblem(e) });
        return;
    }
    if (call !== c) return;
    c.version = 0;
    c.answer = '';
    c.attached = false;
    c.held = false;
    c.resuming = false;
    pc = new RTCPeerConnection({ iceServers: [] });
    const own = pc;
    pc.ontrack = (e) => play(e);
    pc.onconnectionstatechange = () => {
        if (pc !== own || call !== c) return;
        stateChanged(c, own.connectionState);
    };
    if (!sendCall('voice.join', { den: c.den, channel: c.channel, muted: c.muted })) {
        dropped('disconnected');
        return;
    }
    clearTimeout(c.offerBy);
    c.offerBy = setTimeout(() => {
        if (call !== c || pc !== own || c.version) return;
        sendCall('voice.leave', { den: c.den });
        dropped('failed');
    }, OFFER_WAIT);
}

// stateChanged follows the browser's connection to the den's media ports.
// One that breaks after it was up asks for an ICE restart first.
function stateChanged(c, state) {
    switch (state) {
        case 'connected':
            c.status = c.held ? 'reconnecting' : 'connected';
            c.connected = true;
            c.attempts = 0;
            clearTimeout(c.restartAfter);
            clearTimeout(c.restartBy);
            c.restartAfter = c.restartBy = null;
            notify();
            break;
        case 'disconnected':
            clearTimeout(c.restartAfter);
            c.restartAfter = setTimeout(() => {
                if (call === c && pc?.connectionState !== 'connected') restart(c);
            }, RESTART_AFTER);
            break;
        case 'failed':
            if (!c.connected) {
                sendCall('voice.leave', { den: c.den });
                dropped('failed');
            } else {
                restart(c);
            }
            break;
    }
}

// restart asks the den for an ICE restart, unless the den is away, when
// resuming asks for one; a connection still down after it joins again.
function restart(c) {
    if (c.held || c.restartBy) return;
    sendCall('voice.restart', { den: c.den });
    c.restartBy = setTimeout(() => {
        c.restartBy = null;
        if (call !== c || pc?.connectionState === 'connected') return;
        sendCall('voice.leave', { den: c.den });
        dropped('failed');
    }, RESTART_WAIT);
}

async function openMic() {
    const want = stored(MIC);
    const rnnoise = rnnoiseChosen(stored(RNNOISE)) && !rnnoiseFailed;
    // Only one suppressor runs: the browser's, or RNNoise. Echo
    // cancellation stays, since it needs the raw microphone.
    const audio = { echoCancellation: true, noiseSuppression: !rnnoise, autoGainControl: true };
    let stream;
    try {
        stream = await navigator.mediaDevices.getUserMedia({ audio: want ? { ...audio, deviceId: { exact: want } } : audio });
    } catch (e) {
        // A microphone that's gone gives way to the default.
        if (!want || (e.name !== 'OverconstrainedError' && e.name !== 'NotFoundError')) throw e;
        store(MIC, '');
        stream = await navigator.mediaDevices.getUserMedia({ audio });
    }
    let track = stream.getAudioTracks()[0];
    let chain = null;
    if (rnnoise) {
        try {
            chain = await clean(stream);
            track = chain.track;
        } catch {
            // Without RNNoise the call goes on with the browser's own
            // suppressor, which the settings say, until the member tries
            // RNNoise again; their choice stands.
            stream.getTracks().forEach((t) => t.stop());
            rnnoiseFailed = true;
            return openMic();
        }
    }
    closeCleaner();
    mic = stream;
    cleaner = chain;
    sent = track;
    sent.enabled = !call?.muted;
    meter('self', new MediaStream([sent]));
}

// RNNoise ---------------------------------------------------------------

let compiled = null;
let rnnoiseFailed = false;

// clean runs a microphone through RNNoise in an AudioWorklet, and returns
// the track the call sends. The module is compiled once, on the page,
// under its CSP's 'wasm-unsafe-eval', and posted to the worklet.
async function clean(stream) {
    const root = document.getElementById('app');
    if (!compiled) {
        const res = await fetch(root.dataset.rnnoiseModule);
        if (!res.ok) throw new Error(`RNNoise: ${res.status}`);
        compiled = await WebAssembly.compile(await res.arrayBuffer());
    }
    const ctx = new AudioContext({ sampleRate: 48000 });
    try {
        await ctx.audioWorklet.addModule(root.dataset.rnnoiseWorklet);
        const node = new AudioWorkletNode(ctx, 'rnnoise', { processorOptions: { module: compiled } });
        const dest = ctx.createMediaStreamDestination();
        ctx.createMediaStreamSource(stream).connect(node).connect(dest);
        // A context that never runs would send silence, as where a browser
        // has no audio output, so RNNoise gives way to the browser's
        // suppressor then. Starting can take seconds, as a Bluetooth headset
        // wakes, and the join waits only that long, so the wait is generous.
        await Promise.race([ctx.resume(), new Promise((r) => setTimeout(r, 10 * 1000))]);
        if (ctx.state !== 'running') throw new Error('this browser kept RNNoise from running');
        return { ctx, track: dest.stream.getAudioTracks()[0] };
    } catch (e) {
        await ctx.close();
        throw e;
    }
}

function closeCleaner() {
    if (cleaner) {
        cleaner.ctx.close().catch(() => {});
        cleaner = null;
    }
}

// rnnoiseChosen reads the member's choice of suppressor: RNNoise, unless
// they turned it off here.
export function rnnoiseChosen(kept) {
    return kept !== '0';
}

// noiseSuppression is the suppressor this browser uses in calls: the
// browser's own or RNNoise, and whether RNNoise couldn't start.
export function noiseSuppression() {
    return { rnnoise: rnnoiseChosen(stored(RNNOISE)), failed: rnnoiseFailed };
}

// setRNNoise turns RNNoise on or off, mid-call too, by replacing the track
// the call sends.
export async function setRNNoise(on) {
    store(RNNOISE, on ? '' : '0');
    rnnoiseFailed = false;
    await reopenMic();
}

// reopenMic opens the microphone again as chosen, and sends it.
async function reopenMic() {
    if (!call || !mic) return;
    const old = mic;
    const oldCleaner = cleaner;
    cleaner = null;
    mic = null;
    try {
        await openMic();
    } catch (e) {
        mic = old;
        cleaner = oldCleaner;
        throw e;
    }
    const sender = pc?.getTransceivers()[0]?.sender;
    if (sender && call?.attached) await sender.replaceTrack(sent);
    old.getTracks().forEach((t) => t.stop());
    if (oldCleaner) oldCleaner.ctx.close().catch(() => {});
}

// Signaling ---------------------------------------------------------------

function handle(msg) {
    if (msg.t === 'dens') {
        denStates = new Map(msg.d.dens.map((d) => [d.den_id, d.state]));
        // A den that shut this device out ends its call for good.
        const state = call && denStates.get(call.den);
        if (state === 'removed' || state === 'revoked') finish({ text: endedText('gone') });
        else {
            tryResume();
            tryRejoin();
        }
        return;
    }
    if (msg.t !== 'call' || !call) return;
    const e = msg.d;
    if (e.den !== call.den || e.channel !== call.channel) return;
    if (e.offer) answer(e.offer);
    else if (e.resumed) resumed();
    else if (e.ended) ended(e.ended);
}

// answerFor decides what an offer gets: an answer to a new one, the same
// answer again to one the den sent again after a resume, as the den may
// not have heard it, or nothing for an older one.
export function answerFor(c, offer) {
    if (offer.version > c.version) return 'answer';
    if (offer.version === c.version && c.answer) return 'again';
    return 'ignore';
}

async function answer(offer) {
    const c = call;
    const own = pc;
    if (!own) return;
    switch (answerFor(c, offer)) {
        case 'ignore':
            return;
        case 'again':
            sendCall('voice.answer', { den: c.den, version: offer.version, sdp: c.answer });
            return;
    }
    c.version = offer.version;
    c.ports = [offer.udp_port, offer.tcp_port];
    try {
        await own.setRemoteDescription({ type: 'offer', sdp: offer.sdp });
        if (!c.attached) {
            // The den's first section takes the microphone; each of the
            // rest brings another member's audio.
            const mine = own.getTransceivers()[0];
            mine.direction = 'sendonly';
            await mine.sender.replaceTrack(sent);
            c.attached = true;
        }
        await own.setLocalDescription();
        if (call !== c || pc !== own) return;
        c.answer = own.localDescription.sdp;
        sendCall('voice.answer', { den: c.den, version: offer.version, sdp: c.answer });
        if (c.status === 'joining') {
            c.status = 'connecting';
            notify();
        }
    } catch {
        if (call !== c || pc !== own) return;
        sendCall('voice.leave', { den: c.den });
        finish({ text: "This browser couldn't set the call up." });
    }
}

function play(e) {
    const stream = e.streams[0];
    if (!stream) return;
    const member = stream.id;
    let audio = audios.get(member);
    if (!audio) {
        audio = new Audio();
        audio.autoplay = true;
        const speaker = stored(SPEAKER);
        if (speaker && audio.setSinkId) audio.setSinkId(speaker).catch(() => {});
        audios.set(member, audio);
    }
    audio.volume = volumeIn(volumes, call?.den, member);
    audio.srcObject = stream;
    audio.play().catch(() => {});
    meter(member, stream);
    // A member who leaves takes their track with them.
    stream.onremovetrack = () => {
        if (stream.getTracks().length === 0 && audio.srcObject === stream) {
            audio.srcObject = null;
            audios.delete(member);
            unmeter(member);
        }
    };
}

function ended(reason) {
    // The service's connection to the den dropped: the den holds the call.
    if (reason === 'disconnected') {
        hold();
        return;
    }
    // A resume that found no call joins again; another failure, as before.
    if (reason === 'failed' && call.resuming) {
        call.resuming = false;
        dropped('failed');
        return;
    }
    if (reason === 'failed') dropped(reason);
    else finish({ text: endedText(reason, call.ports) });
}

// hold keeps the call while the den is away: the peer connection stays,
// and its media flows if its path does.
function hold() {
    const c = call;
    if (!c || !pc) {
        if (c) dropped('disconnected');
        return;
    }
    c.held = true;
    c.resuming = false;
    clearTimeout(c.restartBy);
    c.restartBy = null;
    if (c.status !== 'reconnecting') {
        c.status = 'reconnecting';
        clearTimeout(c.giveUp);
        c.giveUp = setTimeout(() => {
            if (call === c && c.status === 'reconnecting') finish({ text: endedText('lost') });
        }, REJOIN_WINDOW);
    }
    notify();
    tryResume();
}

// tryResume asks the den for the held call once the den and the service
// are back.
function tryResume() {
    const c = call;
    if (!c || !c.held || c.resuming || !pc || !live || denStates.get(c.den) !== 'connected') return;
    c.resuming = true;
    if (!sendCall('voice.join', { den: c.den, channel: c.channel, muted: c.muted, resume: true })) {
        c.resuming = false;
        return;
    }
    clearTimeout(c.resumeBy);
    c.resumeBy = setTimeout(() => {
        if (call === c && c.resuming) ended('failed');
    }, RESUME_WAIT);
}

// resumed is the den taking the call back: if the media path broke too,
// as on another network, it needs an ICE restart.
function resumed() {
    const c = call;
    clearTimeout(c.resumeBy);
    clearTimeout(c.giveUp);
    c.held = false;
    c.resuming = false;
    c.attempts = 0;
    if (pc?.connectionState === 'connected') {
        c.status = 'connected';
    } else {
        c.status = 'connecting';
        restart(c);
    }
    // A mark changed while the den was away goes now.
    sendCall('voice.mute', { den: c.den, muted: c.muted });
    notify();
}

// dropped holds on to a call whose connection broke, to join it again
// once the den and this computer's service are back. A call that failed
// without ever coming up most likely can't reach the den's media ports,
// which trying again won't open.
function dropped(reason) {
    const c = call;
    closePeer();
    if (reason === 'failed' && !c.connected) {
        finish({ text: endedText('failed', c.ports) });
        return;
    }
    if (c.attempts >= REJOINS) {
        finish({ text: endedText('lost') });
        return;
    }
    c.attempts += 1;
    c.held = false;
    c.resuming = false;
    if (c.status !== 'reconnecting') {
        c.status = 'reconnecting';
        clearTimeout(c.giveUp);
        c.giveUp = setTimeout(() => {
            if (call === c && c.status === 'reconnecting') finish({ text: endedText('lost') });
        }, REJOIN_WINDOW);
    }
    notify();
    tryRejoin();
}

function tryRejoin() {
    const c = call;
    if (!c || c.status !== 'reconnecting' || c.held || pc || rejoinTimer || !live || denStates.get(c.den) !== 'connected') return;
    rejoinTimer = setTimeout(() => {
        rejoinTimer = null;
        if (call === c && c.status === 'reconnecting' && !c.held && !pc && live && denStates.get(c.den) === 'connected') connect();
    }, 500 * c.attempts);
}

function clearTimers(c) {
    for (const t of [c.giveUp, c.offerBy, c.resumeBy, c.restartAfter, c.restartBy]) clearTimeout(t);
}

function closePeer() {
    if (pc) {
        pc.ontrack = null;
        pc.onconnectionstatechange = null;
        pc.close();
        pc = null;
    }
    for (const [member, audio] of audios) {
        audio.pause();
        audio.srcObject = null;
        unmeter(member);
    }
    audios.clear();
}

// finish ends the call for good, and says why when there's a reason to.
function finish(why) {
    clearTimeout(rejoinTimer);
    rejoinTimer = null;
    if (call) clearTimers(call);
    closePeer();
    mic?.getTracks().forEach((t) => t.stop());
    mic = null;
    sent = null;
    closeCleaner();
    stopMeters();
    notice = why && call ? { ...why, den: call.den, channel: call.channel, label: call.label } : null;
    call = null;
    notify();
}

// Speaking ----------------------------------------------------------------

function startMeters() {
    if (meterContext) return;
    try {
        meterContext = new AudioContext();
        meterContext.resume().catch(() => {});
    } catch {
        meterContext = null;
        return;
    }
    meterTimer = setInterval(measure, METER_MS);
}

function stopMeters() {
    clearInterval(meterTimer);
    meterTimer = null;
    for (const key of [...meters.keys()]) unmeter(key);
    meterContext?.close().catch(() => {});
    meterContext = null;
    lastLoud.clear();
    speaking = new Set();
}

// meter measures a stream's level, by member, or "self" for this page's.
// Chromium measures a remote stream only while a media element plays it,
// as play() does.
function meter(key, stream) {
    if (!meterContext) return;
    unmeter(key);
    try {
        const source = meterContext.createMediaStreamSource(stream);
        const analyser = meterContext.createAnalyser();
        analyser.fftSize = 512;
        source.connect(analyser);
        meters.set(key, { source, analyser, buffer: new Float32Array(analyser.fftSize) });
    } catch {
        // A stream without audio yet; its track's arrival meters it again.
    }
}

function unmeter(key) {
    const m = meters.get(key);
    if (!m) return;
    m.source.disconnect();
    meters.delete(key);
}

function measure() {
    const now = performance.now();
    const levels = new Map();
    for (const [key, m] of meters) {
        m.analyser.getFloatTimeDomainData(m.buffer);
        levels.set(key, rmsDb(m.buffer));
    }
    if (call?.muted) levels.delete('self');
    const next = speakingNow(levels, lastLoud, now);
    if (next.size !== speaking.size || [...next].some((k) => !speaking.has(k))) {
        speaking = next;
        notify();
    }
}

// rmsDb is a block's level in dBFS.
export function rmsDb(samples) {
    let s = 0;
    for (const x of samples) s += x * x;
    return 10 * Math.log10(Math.max(s / Math.max(1, samples.length), 1e-12));
}

// speakingNow says who is speaking: anyone whose level is above the
// threshold now, or was within the hold, which keeps the mark steady
// between words. last remembers when each was last loud.
export function speakingNow(levels, last, now) {
    for (const [key, db] of levels) if (db > SPEAKING_DB) last.set(key, now);
    const out = new Set();
    for (const [key, at] of last) {
        if (levels.has(key) && now - at <= SPEAKING_HOLD) out.add(key);
        else if (now - at > SPEAKING_HOLD) last.delete(key);
    }
    return out;
}

// Volumes -----------------------------------------------------------------

// A member's volume, from 0 to 1, is the member here's own choice: the
// browser keeps it by den and member, as it keeps the microphone and
// speaker, so someone turned down stays down in the next call. It's the
// volume of the media element that plays them, which tops out at 1: more
// would mean playing them through Web Audio instead.

const volumeKey = (den, member) => `${den}:${member}`;

// readVolumes reads the volumes the browser kept, skipping anything that
// isn't one.
export function readVolumes(text) {
    let kept;
    try {
        kept = JSON.parse(text || '{}');
    } catch {
        return {};
    }
    const out = {};
    if (!kept || typeof kept !== 'object' || Array.isArray(kept)) return out;
    for (const [key, v] of Object.entries(kept)) {
        if (typeof v === 'number' && v >= 0 && v < 1) out[key] = v;
    }
    return out;
}

// clampVolume keeps a volume between 0 and 1, in steps of 5%.
export function clampVolume(v) {
    const n = Number(v);
    if (!Number.isFinite(n)) return 1;
    return Math.min(1, Math.max(0, Math.round(n * 20) / 20));
}

// volumeIn is how loud a member of a den plays: full unless chosen.
export function volumeIn(volumes, den, member) {
    return volumes[volumeKey(den, member)] ?? 1;
}

// withVolume returns volumes with a member's set; full drops out, since
// it's what everyone plays at unless chosen.
export function withVolume(volumes, den, member, volume) {
    const next = { ...volumes };
    const v = clampVolume(volume);
    if (v === 1) delete next[volumeKey(den, member)];
    else next[volumeKey(den, member)] = v;
    return next;
}

// setVolume sets how loud a member plays here, mid-call too, and keeps it.
export function setVolume(den, member, volume) {
    volumes = withVolume(volumes, den, member, volume);
    store(VOLUMES, Object.keys(volumes).length ? JSON.stringify(volumes) : '');
    const audio = call?.den === den && audios.get(member);
    if (audio) audio.volume = volumeIn(volumes, den, member);
    notify();
}

// Devices -----------------------------------------------------------------

// devices lists the microphones and speakers, and which are chosen. A
// browser that can't choose a speaker gives none.
export async function devices() {
    const all = await navigator.mediaDevices.enumerateDevices();
    const named = (kind) => all.filter((d) => d.kind === kind && d.deviceId).map((d, i) => ({ id: d.deviceId, label: d.label || `${kind === 'audioinput' ? 'Microphone' : 'Speaker'} ${i + 1}` }));
    return {
        inputs: named('audioinput'),
        outputs: typeof HTMLMediaElement.prototype.setSinkId === 'function' ? named('audiooutput') : null,
        mic: stored(MIC),
        speaker: stored(SPEAKER),
    };
}

// chooseMic switches microphones, mid-call too; '' is the default.
export async function chooseMic(id) {
    store(MIC, id);
    await reopenMic();
}

// chooseSpeaker plays the call through a speaker; '' is the default.
export function chooseSpeaker(id) {
    store(SPEAKER, id);
    for (const audio of audios.values()) audio.setSinkId?.(id).catch(() => {});
}

// applyCalls updates who is in each call from a den's voice.state: a call
// with no one left goes, and a full one replaces them all.
export function applyCalls(calls, state) {
    const next = new Map(state.full ? [] : (calls || []).map((c) => [c.channel_id, c]));
    for (const c of state.calls || []) {
        if (c.members.length) next.set(c.channel_id, c);
        else next.delete(c.channel_id);
    }
    return [...next.values()];
}

// endedText says why a call ended, for the member.
export function endedText(reason, ports) {
    switch (reason) {
        case 'moved':
            return 'Your call moved to another tab or device.';
        case 'not_found':
            return "That voice channel isn't there.";
        case 'full':
            return 'That call is full.';
        case 'forbidden':
            return "You can't see that channel any more.";
        case 'deleted':
            return 'That channel was deleted.';
        case 'rate_limited':
            return "You're joining calls too fast. Wait a moment and try again.";
        case 'disconnected_by_staff':
            return 'You were disconnected from the call. You can join it again.';
        case 'lost':
            return 'Lost the call. Join again once the den is back.';
        case 'gone':
            return 'The call ended: this device is no longer in that den.';
    }
    if (ports?.[0] && ports?.[1]) {
        return `Couldn't connect to the den's voice ports. Its owner can check that UDP ${ports[0]} and TCP ${ports[1]} are open to the internet.`;
    }
    return "Couldn't connect the call.";
}

// micProblem says why the microphone didn't open.
export function micProblem(e) {
    switch (e?.name) {
        case 'NotAllowedError':
            return "Calls need your microphone. Allow it for this page in your browser's site settings, then join again.";
        case 'NotFoundError':
            return 'No microphone was found.';
        case 'NotReadableError':
            return 'Your microphone is in use by another program, or it could not be opened.';
    }
    return "Your microphone couldn't be opened.";
}
