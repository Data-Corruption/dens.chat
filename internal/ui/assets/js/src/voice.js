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
// plays each member as loud as the member here chose, and runs the
// microphone through the voice processor: RNNoise, unless the member
// turned it off, and the gate that sends only what they mean to (M3.3), by
// voice activity or while they push to talk.
//
// From M4.2 the page shares its member's screen in the call, and plays the
// shares they watch, which the player shows. It follows what the den says
// of the call's den, to know its limits and who shares and watches.
//
// From M11 a member holding push to talk rings for everyone in the call,
// through the den, and the page notes what its connection does for the
// service's log.
//
// From M12 the page plays its call again as it thaws from a freeze. Where
// audio may start without a click, as in Android's app, it makes its call's
// audio contexts once the microphone is open, and plays the members' voices
// through one of them, which Android then plays as a call's: under the
// call's volume, and on the route the app's shell sets.

import { api } from './api.js';
import { onConnection, onEvent, sendCall } from './events.js';
import { NO_CALLS, NO_SHARING, autoplays, canCall, canShare } from './platform.js';
import { MAX_WATCHING, displayOptions, fitPixels, readShare, receivingSections, refusalText, screenOwner, shareBlocked, shareQuality, watchersOf } from './share.js';

const MIC = 'DENS_MIC';
const SPEAKER = 'DENS_SPEAKER';
const MUTED = 'DENS_MUTED';
const DEAFENED = 'DENS_DEAFENED';
const RNNOISE = 'DENS_RNNOISE';
const VOLUMES = 'DENS_VOLUMES';
const CALL_VOLUME = 'DENS_CALL_VOLUME';
const VOICE = 'DENS_VOICE';
const SHARE = 'DENS_SHARE';
// How often a share checks its size, since a window it shows can change,
// and how long it waits for the den to take it.
const SHARE_CHECK = 2000;
const SHARE_WAIT = 15 * 1000;
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
// How long the microphone may take to open, as while the browser asks the
// member for it, before the call says what it's waiting for.
const MIC_WAIT = 1500;
const RESTART_WAIT = 15 * 1000;
// Speaking: a level above this, in dBFS, measured this often, and held
// this long after it drops.
const SPEAKING_DB = -50;
const METER_MS = 100;
const SPEAKING_HOLD = 400;
// The page sums its call's statistics up for the service's log this often,
// and keeps this many notes while its stream to the service is down (M11).
const STATS_EVERY = 10 * 1000;
const NOTES_KEPT = 64;

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
// mic is the microphone's stream, and sent the track the call sends: what
// the voice processor (chain) makes of it, or the microphone's own where
// the processor can't run.
let mic = null;
let sent = null;
let chain = null;
// warmContext is the voice processor's context, made in the click that
// joins a call, until the processor takes it.
let warmContext = null;
// audios plays each other member's stream, by their member ID: the den
// labels the stream it sends their audio on with it.
const audios = new Map();
// callContext is the call's audio context, where meters measure each
// member's audio, and this page's own as sent ("self"). Where audio may
// start without a click, it plays the members too, each through their gain
// in voices, by member ID (M12).
let callContext = null;
const meters = new Map();
const voices = new Map();
let meterTimer = null;
// speaking is who rings in the call: those the meters hear, audible, those
// the den says hold push to talk, talkers (M11), and "self" while this
// page's member speaks or holds it.
let speaking = new Set();
let audible = new Set();
let talkers = new Set();
const lastLoud = new Map();
// What the page last heard about each den's connection, and its own
// stream to the service.
let denStates = new Map();
let live = true;
let rejoinTimer = null;
let started = false;
// volumes is how loud each member plays here, by den and member.
let volumes = readVolumes(stored(VOLUMES));
// wholeVolume is how loud the whole call plays here (M12).
let wholeVolume = readCallVolume(stored(CALL_VOLUME));
// share is the page's screen share (M4.2), or null: its status ("picking"
// while the browser asks, "starting" until the den says it's live, then
// "live"), its stream, tracks and quality, and the timer that checks its
// size. shareNotice says why the last share or watch didn't go.
let share = null;
let shareNotice = null;
// watched holds the shares the page watches, by member: their label, the
// stream once it comes, and how loud its sound plays.
const watched = new Map();
// denView is what the page knows of the call's den: this member's ID, the
// den's limits, its calls the member can see, and members' names.
let denView = null;

function start() {
    if (started) return;
    started = true;
    watchKeys();
    onEvent(handle);
    if (typeof document !== 'undefined') {
        document.addEventListener('visibilitychange', () => note({ what: 'hidden', hidden: document.hidden }));
        // Chromium freezes a hidden page a minute after its sound stops,
        // which pauses its media elements and never plays them again: the
        // page plays its call's again as it thaws (M12).
        document.addEventListener('freeze', () => note({ what: 'frozen' }));
        document.addEventListener('resume', () => {
            note({ what: 'resumed' });
            playAgain(audios.values());
        });
    }
    onConnection((up) => {
        live = up;
        if (up) sendNotes();
        // The helper's releases come over the stream too.
        if (!up) {
            for (const action of ['ptt', 'pushMute']) if (covered(action)) releaseAction(action);
        }
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
        call: call && {
            den: call.den, channel: call.channel, label: call.label, status: call.status, muted: call.muted, deafened: call.deafened,
            waitingForMic: !!call.waitingForMic,
        },
        notice,
        speaking,
        volumes,
        share: share && { status: share.status, sound: !!share.audio, wanted: share.wanted, preview: !!share.preview, stream: share.stream || null },
        shareNotice,
        watched: [...watched.values()].map((w) => ({ ...w })),
        den: denView,
    };
}

function notify() {
    const s = snapshot();
    listeners.forEach((fn) => fn(s));
}

// inCall says whether the page is in a call now.
export function inCall() {
    return !!call;
}

// callLimits are the limits of the den the page's call is in, once known,
// or null outside a call, for the share settings to name a den's lower
// limits while they apply (M8.3).
export function callLimits() {
    return call ? denView?.limits || null : null;
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
    if (!canCall()) {
        notice = { text: NO_CALLS };
        notify();
        return;
    }
    if (call && call.den === den && call.channel === channel && call.status !== 'reconnecting') return;
    clearTimeout(rejoinTimer);
    rejoinTimer = null;
    if (call) clearTimers(call);
    closePeer();
    notice = null;
    endShare('');
    watched.clear();
    denView = null;
    stopMicTest();
    call = { den, channel, label, status: 'joining', muted: stored(MUTED) === '1', deafened: stored(DEAFENED) === '1', attempts: 0, connected: false };
    audible = new Set();
    talkers = new Set();
    unsent.length = 0;
    loadDen(call);
    // The click that joins lets audio start, so the call's context starts
    // here, and the voice processor's: one made after the browser has asked
    // for the microphone may need another click to start. Where audio may
    // start without one, both wait for the microphone: Android's app plays
    // a context made while the microphone is open as a call's, and one made
    // before it as media, never on a headset during a call (M12).
    if (!autoplays()) {
        startMeters();
        if (!mic && !processorFailed) warmContext ??= new AudioContext({ sampleRate: 48000 });
    }
    notify();
    await connect();
}

// leaveCall hangs up.
export function leaveCall() {
    if (!call) return;
    sendCall('voice.leave', { den: call.den });
    finish(null);
}

// setMuted mutes the member, or unmutes them, which also lets them hear
// again if they were deafened, as there's no talking without hearing.
export function setMuted(muted) {
    if (!muted && isDeafened()) hear(true);
    mute(muted);
}

// setDeafened stops the member hearing the call, and mutes them while it
// lasts; hearing again puts their microphone back as it was.
export function setDeafened(deafened) {
    if (deafened === isDeafened()) return;
    if (deafened) mutedBeforeDeafened = isMuted();
    hear(!deafened);
    mute(deafened || mutedBeforeDeafened);
}

let mutedBeforeDeafened = false;

function mute(muted) {
    store(MUTED, muted ? '1' : '');
    if (!call) return;
    call.muted = muted;
    applySending();
    sendCall('voice.mute', { den: call.den, muted, deafened: call.deafened });
    notify();
}

function hear(on) {
    store(DEAFENED, on ? '' : '1');
    if (call) call.deafened = !on;
    for (const member of audios.keys()) level(member);
    notify();
}

function isMuted() {
    return call ? call.muted : stored(MUTED) === '1';
}

function isDeafened() {
    return call ? call.deafened : stored(DEAFENED) === '1';
}

export function clearNotice() {
    notice = null;
    notify();
}

async function connect() {
    const c = call;
    const slow = setTimeout(() => {
        if (call !== c) return;
        c.waitingForMic = true;
        notify();
    }, MIC_WAIT);
    try {
        if (!mic) await openMic(c);
    } catch (e) {
        if (call === c) finish({ text: micProblem(e) });
        return;
    } finally {
        clearTimeout(slow);
    }
    if (call !== c) return;
    if (c.waitingForMic) {
        c.waitingForMic = false;
        notify();
    }
    c.version = 0;
    c.answer = '';
    c.attached = false;
    c.held = false;
    c.resuming = false;
    c.screenMid = c.soundMid = null;
    pc = new RTCPeerConnection({ iceServers: [] });
    const own = pc;
    sentBefore = { bytes: 0, at: performance.now() };
    pc.ontrack = (e) => play(e);
    pc.onconnectionstatechange = () => {
        if (pc !== own || call !== c) return;
        note({ what: 'state', state: own.connectionState, ice: own.iceConnectionState });
        noteStats();
        stateChanged(c, own.connectionState);
    };
    clearInterval(statsTimer);
    statsTimer = setInterval(noteStats, STATS_EVERY);
    if (!sendCall('voice.join', { den: c.den, channel: c.channel, muted: c.muted, deafened: c.deafened })) {
        dropped('disconnected', 'stream');
        return;
    }
    // The den hears push to talk afresh in each call it joins (M11).
    c.talkSent = false;
    tellTalking();
    // A call joined again, as after the den restarted, takes the page's
    // share and the shares it watched back, which the den checks again.
    if (share && share.status !== 'picking') {
        awaitShare(share);
        sendCall('voice.share', { den: c.den, on: true, sound: !!share.audio });
    }
    for (const member of watched.keys()) sendCall('voice.watch', { den: c.den, member_id: member, on: true });
    clearTimeout(c.offerBy);
    c.offerBy = setTimeout(() => {
        if (call !== c || pc !== own || c.version) return;
        sendCall('voice.leave', { den: c.den });
        dropped('failed', 'no_offer');
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
                if (call === c && pc?.connectionState !== 'connected') restart(c, 'disconnected');
            }, RESTART_AFTER);
            break;
        case 'failed':
            if (!c.connected) {
                sendCall('voice.leave', { den: c.den });
                dropped('failed', 'never_connected');
            } else {
                restart(c, 'failed');
            }
            break;
    }
}

// restart asks the den for an ICE restart, unless the den is away, when
// resuming asks for one; a connection still down after it joins again.
// why says what the connection did, for the log.
function restart(c, why) {
    if (c.held || c.restartBy) return;
    note({ what: 'restart', why });
    sendCall('voice.restart', { den: c.den });
    c.restartBy = setTimeout(() => {
        c.restartBy = null;
        if (call !== c || pc?.connectionState === 'connected') return;
        sendCall('voice.leave', { den: c.den });
        dropped('failed', 'restart_timeout');
    }, RESTART_WAIT);
}

// openMic opens the microphone for call c, and sends it, and reports
// whether it did: one that opens once c has ended, as when the member left
// while the browser asked for it, closes again, since nothing would.
async function openMic(c) {
    const v = await openVoice();
    if (call !== c) {
        v.stream.getTracks().forEach((t) => t.stop());
        v.chain?.ctx.close().catch(() => {});
        return false;
    }
    closeChain();
    mic = v.stream;
    chain = v.chain;
    sent = v.track;
    applySending();
    // Where audio may start without a click, the call's context starts now
    // that the microphone is open (M12); otherwise it started as the call
    // did.
    startMeters();
    meter('self', new MediaStream([sent]));
    return true;
}

// openVoice opens the microphone as chosen and runs it through the voice
// processor: with RNNoise, unless it's off or couldn't start, and with the
// browser's own suppressor otherwise, since only one runs. Echo
// cancellation stays, since it needs the raw microphone. Where the
// processor can't run, the microphone goes as it is.
async function openVoice() {
    const module = processorFailed ? null : await rnnoiseOrNone();
    const audio = { echoCancellation: true, noiseSuppression: !module, autoGainControl: true };
    const want = stored(MIC);
    let stream;
    try {
        stream = await navigator.mediaDevices.getUserMedia({ audio: want ? { ...audio, deviceId: { exact: want } } : audio });
    } catch (e) {
        // A microphone that's gone gives way to the default.
        if (!want || (e.name !== 'OverconstrainedError' && e.name !== 'NotFoundError')) throw e;
        store(MIC, '');
        stream = await navigator.mediaDevices.getUserMedia({ audio });
    }
    if (processorFailed) return { stream, chain: null, track: stream.getAudioTracks()[0] };
    try {
        const c = await processor(stream, module);
        return { stream, chain: c, track: c.track };
    } catch (e) {
        // The settings say so; the member's choices stand, for the next
        // call or another browser. The microphone opens again, with the
        // browser's own suppressor this time.
        stream.getTracks().forEach((t) => t.stop());
        if (e.rate) rnnoiseFailed = true;
        else processorFailed = true;
        return openVoice();
    }
}

// Voice processor ---------------------------------------------------------

let compiled = null;
let rnnoiseFailed = false;
let processorFailed = false;

// rnnoiseOrNone compiles RNNoise's module once, on the page, under its
// CSP's 'wasm-unsafe-eval', for the processor; or returns null when the
// member turned RNNoise off, or it can't be compiled here.
async function rnnoiseOrNone() {
    if (!rnnoiseChosen(stored(RNNOISE)) || rnnoiseFailed) return null;
    try {
        if (!compiled) {
            const res = await fetch(document.getElementById('app').dataset.rnnoiseModule);
            if (!res.ok) throw new Error(`RNNoise: ${res.status}`);
            compiled = await WebAssembly.compile(await res.arrayBuffer());
        }
        return compiled;
    } catch {
        rnnoiseFailed = true;
        return null;
    }
}

// processor runs a microphone through the voice processor in an
// AudioWorklet, and returns the track the call sends.
async function processor(stream, module) {
    const ctx = warmContext || new AudioContext({ sampleRate: 48000 });
    warmContext = null;
    // RNNoise works on 48 kHz audio alone, and a browser may run its audio
    // at another rate whatever the page asks, as some do to resist
    // fingerprinting: then the processor runs without it, in this context.
    if (module && ctx.sampleRate !== 48000) {
        warmContext = ctx;
        throw Object.assign(new Error(`audio runs at ${ctx.sampleRate} Hz`), { rate: true });
    }
    try {
        await ctx.audioWorklet.addModule(document.getElementById('app').dataset.voiceWorklet);
        const rnnoise = !!module;
        const node = new AudioWorkletNode(ctx, 'voice', {
            outputChannelCount: [1],
            processorOptions: { module, gate: gateFor(voice, rnnoise, pttHeld) },
        });
        node.port.onmessage = (e) => heard(e.data);
        // One voice, in one channel, all the way to the encoder.
        const dest = ctx.createMediaStreamDestination();
        dest.channelCount = 1;
        ctx.createMediaStreamSource(stream).connect(node).connect(dest);
        // A context that never runs would send silence, as where a browser
        // has no audio output, so the microphone goes as it is then.
        // Starting can take seconds, as a Bluetooth headset wakes, and the
        // join waits only that long, so the wait is generous.
        await Promise.race([ctx.resume(), new Promise((r) => setTimeout(r, 10 * 1000))]);
        if (ctx.state !== 'running') throw new Error("this browser kept the voice processor from running");
        return { ctx, node, rnnoise, track: dest.stream.getAudioTracks()[0] };
    } catch (e) {
        await ctx.close();
        throw e;
    }
}

function closeChain() {
    if (chain) {
        chain.ctx.close().catch(() => {});
        chain = null;
    }
}

// rnnoiseChosen reads the member's choice of suppressor: RNNoise, unless
// they turned it off here.
export function rnnoiseChosen(kept) {
    return kept !== '0';
}

// noiseSuppression is the suppressor this browser uses in calls: the
// browser's own or RNNoise, whether RNNoise couldn't start, and whether
// the voice processor couldn't run at all.
export function noiseSuppression() {
    return { rnnoise: rnnoiseChosen(stored(RNNOISE)), failed: rnnoiseFailed, processorFailed };
}

// setRNNoise turns RNNoise on or off, mid-call too, by replacing the track
// the call sends.
export async function setRNNoise(on) {
    store(RNNOISE, on ? '' : '0');
    rnnoiseFailed = false;
    processorFailed = false;
    await reopenMic();
    await restartMicTest();
}

// reopenMic opens the microphone again as chosen, and sends it. The one it
// replaces closes once the new one goes, or once the call has ended.
async function reopenMic() {
    const c = call;
    if (!c || !mic) return;
    const old = mic;
    const oldChain = chain;
    chain = null;
    mic = null;
    let opened;
    try {
        opened = await openMic(c);
    } catch (e) {
        if (call === c) {
            mic = old;
            chain = oldChain;
            throw e;
        }
        opened = false;
    }
    const sender = pc?.getTransceivers()[0]?.sender;
    if (opened && sender && call?.attached) await sender.replaceTrack(sent);
    old.getTracks().forEach((t) => t.stop());
    if (oldChain) oldChain.ctx.close().catch(() => {});
}

// Sending -----------------------------------------------------------------

// A member sends by voice activity ("voice"), automatically or above a
// level in dBFS, or while they push to talk ("ptt"), which the browser
// keeps. Their keys for push to talk, toggle mute and push to mute live in
// the service from M7, so its helper can read them in other programs.
export const VOICE_DEFAULTS = { mode: 'voice', auto: true, threshold: -50 };
const KEYS = ['ptt', 'toggleMute', 'pushMute'];

// readVoice reads the voice settings the browser kept, falling back to
// the defaults for anything that isn't one.
export function readVoice(text) {
    let kept;
    try {
        kept = JSON.parse(text || '{}');
    } catch {
        kept = {};
    }
    if (!kept || typeof kept !== 'object') kept = {};
    return {
        mode: kept.mode === 'ptt' ? 'ptt' : 'voice',
        auto: typeof kept.auto === 'boolean' ? kept.auto : VOICE_DEFAULTS.auto,
        threshold: Number.isFinite(kept.threshold) ? Math.min(0, Math.max(-100, Math.round(kept.threshold))) : VOICE_DEFAULTS.threshold,
    };
}

// readKeys keeps the bindings among keys, by action.
export function readKeys(keys) {
    const out = {};
    for (const name of KEYS) {
        const k = keys?.[name];
        if (k && typeof k.code === 'string' && k.code && typeof k.label === 'string') {
            out[name] = { code: k.code, label: k.label, ctrl: !!k.ctrl, alt: !!k.alt, shift: !!k.shift, meta: !!k.meta };
        }
    }
    return out;
}

// readKeyStatus reads what the service says of the keys and its helper:
// the keys, whether a helper is connected, the actions it reads in other
// programs during a call, what the system keeps from it, and why it reads
// none, where it can't. Where the desktop owns the keys (M7.2) it also says
// so (confirms), whether the member can set them up with it (bindable),
// whether its dialog is up (binding), its names for the keys, whether it
// can show its settings for them (configurable), and whether the member
// uses them (desktop).
export function readKeyStatus(d) {
    const helper = !!d?.helper;
    const names = {};
    for (const name of KEYS) {
        if (helper && typeof d.names?.[name] === 'string' && d.names[name]) names[name] = d.names[name];
    }
    return {
        keys: readKeys(d?.keys),
        helper,
        covers: helper && Array.isArray(d.covers) ? KEYS.filter((a) => d.covers.includes(a)) : [],
        note: helper && typeof d.note === 'string' ? d.note : '',
        problem: helper && typeof d.problem === 'string' ? d.problem : '',
        confirms: helper && !!d.confirms,
        bindable: helper && !!d.bindable,
        binding: helper && !!d.binding,
        names,
        configurable: helper && !!d.configurable,
        desktop: !!d?.desktop,
        // The helper shows notifications as the desktop's own (M12.3).
        notifies: helper && !!d.notifies,
    };
}

let voice = readVoice(stored(VOICE));
let keyStatus = readKeyStatus(null);
let pttHeld = false;
let pushMuted = false;

// voiceSettings is how the member sends, with their keys.
export function voiceSettings() {
    watchKeys();
    return { ...voice, keys: keyStatus.keys };
}

// keysElsewhere says what the helper does with the keys.
export function keysElsewhere() {
    watchKeys();
    return keyStatus;
}

// onKeys calls fn whenever the keys or the helper change; it returns a
// function that stops.
const keyListeners = new Set();

export function onKeys(fn) {
    watchKeys();
    keyListeners.add(fn);
    return () => keyListeners.delete(fn);
}

// setVoice changes how the member sends, which takes hold at once, in a
// call and in the settings' microphone test.
export function setVoice(changes) {
    voice = readVoice(JSON.stringify({ ...voice, ...changes }));
    store(VOICE, JSON.stringify(voice));
    pttHeld = pushMuted = false;
    applySending();
    notify();
}

// setKeys gives the service the member's keys, which it sends every page
// and its helper.
export async function setKeys(keys) {
    const next = readKeys(keys);
    await api.post('/api/settings', { voiceKeys: next });
    keyStatus = { ...keyStatus, keys: next };
    pttHeld = pushMuted = false;
    applySending();
    keyListeners.forEach((fn) => fn());
    notify();
}

// gateFor is the gate the voice processor runs: push to talk opens it
// while the key is held, and voice activity opens it for speech, by
// RNNoise's judgment when the member keeps it automatic, or by level.
export function gateFor(v, rnnoise, held) {
    if (v.mode === 'ptt') return { mode: held ? 'open' : 'closed' };
    if (v.auto && rnnoise) return { mode: 'auto' };
    return { mode: 'level', threshold: v.threshold };
}

// applySending sends what the member chose: nothing while muted, and what
// the gate lets through otherwise. Without the processor, push to talk
// holds the track back itself, and voice activity sends everything. The
// call hears whether they hold push to talk, which rings for it (M11).
function applySending() {
    for (const c of [chain, testing?.chain]) c?.node.port.postMessage({ gate: gateFor(voice, c.rnnoise, pttHeld) });
    if (sent) sent.enabled = !isMuted() && (chain || voice.mode !== 'ptt' || pttHeld);
    tellTalking();
    ring();
}

// Push to talk's ring (M11) ---------------------------------------------------

// talking says whether a member rings for holding push to talk: while they
// send by it, hold its key and aren't muted.
export function talking(v, held, muted) {
    return v.mode === 'ptt' && held && !muted;
}

function talks() {
    return !!call && talking(voice, pttHeld, call.muted);
}

// tellTalking tells the den, as it changes, whether the page's member
// holds push to talk.
function tellTalking() {
    const c = call;
    if (!c) return;
    const on = talks();
    if (on !== !!c.talkSent && sendCall('voice.talk', { den: c.den, on })) c.talkSent = on;
}

// talked is the den's word on who holds push to talk in the call.
function talked(members) {
    talkers = new Set(members);
    ring();
}

// ringing says who rings in a call: whoever the meters hear, whoever the
// den says holds push to talk, and this page's member, "self", while they
// hold it here, at once, where the den's word comes a moment later.
export function ringing(heardNow, talkersNow, me, self) {
    const out = new Set(heardNow);
    for (const id of talkersNow) if (id !== me) out.add(id);
    if (self) out.add('self');
    return out;
}

// rings says whether a member of a call rings, from who rings in it, by
// their ID or "self" for this page's member, me: never while they're
// muted, by themselves or by staff, whatever the den or the meters say.
export function rings(m, me, ringingNow) {
    return !m.muted && !m.staff_muted && !!ringingNow?.has(m.id === me ? 'self' : m.id);
}

// ring brings who rings in the call up to date, and tells the page as it
// changes.
function ring() {
    const next = ringing(audible, talkers, denView?.me, talks());
    if (next.size !== speaking.size || [...next].some((k) => !speaking.has(k))) {
        speaking = next;
        notify();
    }
}

// The voice processor's reports: its level, RNNoise's estimate of speech,
// and whether the gate was open, every 50 ms, for the settings' meter.
const levelListeners = new Set();

export function onVoiceLevel(fn) {
    levelListeners.add(fn);
    return () => levelListeners.delete(fn);
}

function heard(report) {
    levelListeners.forEach((fn) => fn(report));
}

// The settings' microphone test runs the processor as a call would, for
// its meter, when no call does.
let testing = null;

export async function startMicTest() {
    if (call || testing) return;
    const t = { stopped: false };
    testing = t;
    let v;
    try {
        v = await openVoice();
    } catch (e) {
        if (testing === t) testing = null;
        throw e;
    }
    if (testing !== t || t.stopped) {
        v.stream.getTracks().forEach((x) => x.stop());
        v.chain?.ctx.close().catch(() => {});
        return;
    }
    t.stream = v.stream;
    t.chain = v.chain;
    applySending();
}

export function stopMicTest() {
    const t = testing;
    testing = null;
    if (!t) return;
    t.stopped = true;
    t.stream?.getTracks().forEach((x) => x.stop());
    t.chain?.ctx.close().catch(() => {});
}

async function restartMicTest() {
    if (!testing) return;
    stopMicTest();
    await startMicTest();
}

// Keys ----------------------------------------------------------------------

// keyMatches says whether a key event is a binding's key, with all its
// modifiers held. Others may be held too, as a player holds Shift to run
// in a game and still pushes to talk.
export function keyMatches(binding, e) {
    return !!binding && e.code === binding.code && (!binding.ctrl || !!e.ctrlKey) && (!binding.alt || !!e.altKey) &&
        (!binding.shift || !!e.shiftKey) && (!binding.meta || !!e.metaKey);
}

// actionFor is the action a key event fires: of the bindings it matches,
// the one with the most modifiers, so Ctrl+V fires its own action and not
// V's.
export function actionFor(keys, e) {
    let action = null;
    let most = -1;
    for (const name of KEYS) {
        const b = keys[name];
        if (!keyMatches(b, e)) continue;
        const mods = [b.ctrl, b.alt, b.shift, b.meta].filter(Boolean).length;
        if (mods > most) {
            action = name;
            most = mods;
        }
    }
    return action;
}

// typesInto says whether a key event would type into the field it's in:
// keys don't fire there, unless they're function keys or held with Ctrl,
// Alt or Meta.
export function typesInto(e) {
    return isField(e.target) && !e.ctrlKey && !e.altKey && !e.metaKey && !/^F\d+$/.test(e.code);
}

// bindingTypes says whether a binding's key types something into a field.
export function bindingTypes(b) {
    return !!b && !b.ctrl && !b.alt && !b.meta && !/^F\d+$/.test(b.code) && !/^Mouse\d$/.test(b.code);
}

function isField(t) {
    return !!t && !!(t.isContentEditable || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' ||
        (t.tagName === 'INPUT' && !UNTYPED.includes(t.type)));
}

// UNTYPED are the inputs keys type nothing into.
const UNTYPED = ['checkbox', 'radio', 'range', 'button', 'submit', 'reset', 'color', 'file', 'image'];

// MOUSE names a mouse's side buttons as keys, by MouseEvent.button: back is
// Mouse4, and forward Mouse5.
const MOUSE = { 3: 'Mouse4', 4: 'Mouse5' };

// keyLabel names a binding as the member pressed it.
export function keyLabel(b) {
    if (!b) return '';
    const mods = [b.ctrl && 'Ctrl', b.alt && 'Alt', b.shift && 'Shift', b.meta && 'Meta'].filter(Boolean);
    return [...mods, b.label].join('+');
}

// covered says whether the helper reads an action's key in other programs,
// which during a call it does in this page too.
function covered(action, status = keyStatus) {
    return status.helper && status.covers.includes(action);
}

function pressAction(action) {
    if (action === 'ptt') {
        if (!pttHeld) {
            pttHeld = true;
            applySending();
        }
    } else if (action === 'toggleMute') {
        setMuted(!isMuted());
    } else if (action === 'pushMute' && !pushMuted && !isMuted()) {
        pushMuted = true;
        setMuted(true);
    }
}

function releaseAction(action) {
    if (action === 'ptt' && pttHeld) {
        pttHeld = false;
        applySending();
    }
    if (action === 'pushMute' && pushMuted) {
        pushMuted = false;
        setMuted(false);
    }
}

// listenIn has another window of the page's, as a share's own (M11), take
// the voice keys while it has focus, as the page does, and returns a
// function that stops.
export function listenIn(win) {
    const on = keyHandlers(win);
    for (const [target, type, fn, capture] of on) target.addEventListener(type, fn, capture);
    return () => {
        for (const [target, type, fn, capture] of on) target.removeEventListener(type, fn, capture);
    };
}

// keyHandlers are what a window of the page listens to for the keys.
function keyHandlers(win) {
    const doc = win.document || document;
    return [
        [win, 'keydown', keydown, true], [win, 'keyup', keyup, true], [win, 'pointerdown', pointerdown, true], [win, 'mousedown', mousedown, true],
        [win, 'mouseup', mouseup, true], [win, 'auxclick', auxclick, true], [win, 'blur', release, false],
        [doc, 'visibilitychange', () => doc.hidden && release(), false],
    ];
}

// The page acts on its own keys, except those the helper reads during a
// call, since it sees them pressed here too and a toggle would fire twice,
// and those the desktop owns, whose keys may be others.
function ownKey(action) {
    return !(covered(action) && (call || keyStatus.confirms));
}

// captureKey resolves to the next key or side button pressed, as a binding,
// or null on Escape; nothing else hears it.
let capture = null;
// swallow is a side button just captured, whose release mustn't take the
// browser back or forward.
let swallow = null;

export function captureKey() {
    listenForKeys();
    cancelCapture();
    return new Promise((resolve) => {
        capture = resolve;
    });
}

export function cancelCapture() {
    const c = capture;
    capture = null;
    c?.(null);
}

const MODIFIERS = ['Shift', 'Control', 'Alt', 'Meta', 'AltGraph', 'OS'];

function keydown(e) {
    if (capture) {
        if (MODIFIERS.includes(e.key)) return;
        e.preventDefault();
        e.stopPropagation();
        const done = capture;
        capture = null;
        const plain = !e.ctrlKey && !e.altKey && !e.shiftKey && !e.metaKey;
        if (e.key === 'Escape' && plain) {
            done(null);
            return;
        }
        const label = e.key === ' ' ? 'Space' : e.key.length === 1 ? e.key.toUpperCase() : e.key;
        done({ code: e.code, label, ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey, meta: e.metaKey });
        return;
    }
    if (typesInto(e)) return;
    const action = actionFor(keyStatus.keys, e);
    if (!action) return;
    e.preventDefault();
    // A held toggle toggles once.
    if (!ownKey(action) || (e.repeat && action !== 'ptt')) return;
    pressAction(action);
}

// keyup ends a push by its key alone, whatever modifiers were let go first.
function keyup(e) {
    for (const action of ['ptt', 'pushMute']) {
        if (keyStatus.keys[action]?.code === e.code && ownKey(action)) releaseAction(action);
    }
}

function mouseKey(e) {
    return { code: MOUSE[e.button], ctrlKey: e.ctrlKey, altKey: e.altKey, shiftKey: e.shiftKey, metaKey: e.metaKey };
}

// boundButton says whether a side button is any action's key, so its
// press and release are the page's, not the browser's back and forward.
function boundButton(code) {
    return KEYS.some((a) => keyStatus.keys[a]?.code === code);
}

// pointerdown ends the wait for a key on a press of any other button, as
// a click elsewhere in the page, as Escape does (M11).
function pointerdown(e) {
    if (capture && !MOUSE[e.button]) cancelCapture();
}

function mousedown(e) {
    const code = MOUSE[e.button];
    if (!code) return;
    if (capture) {
        e.preventDefault();
        e.stopPropagation();
        const done = capture;
        capture = null;
        swallow = code;
        done({ code, label: code === 'Mouse4' ? 'Mouse 4' : 'Mouse 5', ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey, meta: e.metaKey });
        return;
    }
    if (!boundButton(code)) return;
    e.preventDefault();
    const action = actionFor(keyStatus.keys, mouseKey(e));
    if (action && ownKey(action)) pressAction(action);
}

function mouseup(e) {
    const code = MOUSE[e.button];
    if (!code) return;
    if (swallow === code || boundButton(code)) e.preventDefault();
    if (swallow === code) swallow = null;
    for (const action of ['ptt', 'pushMute']) {
        if (keyStatus.keys[action]?.code === code && ownKey(action)) releaseAction(action);
    }
}

function auxclick(e) {
    const code = MOUSE[e.button];
    if (code && (swallow === code || boundButton(code))) e.preventDefault();
}

// release lets go of whatever is pushed when the page loses focus, which
// keeps its key's release from reaching it, unless the helper reads it.
function release() {
    for (const action of ['ptt', 'pushMute']) if (ownKey(action)) releaseAction(action);
}

// helperPress acts on a press the helper read, in this page's call.
function helperPress(p) {
    if (!call || !p || !KEYS.includes(p.action) || !covered(p.action)) return;
    if (!p.down) {
        releaseAction(p.action);
        return;
    }
    // A key that types doesn't fire while the member types in a field
    // here. One the desktop owns never reaches the field.
    if (!keyStatus.confirms && document.hasFocus() && isField(document.activeElement) && bindingTypes(keyStatus.keys[p.action])) return;
    pressAction(p.action);
}

// desktopKeys asks a desktop that owns the keys to bind them ("bind"),
// which shows its dialog, to show its settings for them ("configure"), or
// stops using them ("stop").
export async function desktopKeys(what) {
    await api.post('/api/shortcuts/desktop', { do: what });
}

// handleKeys follows the keys and the helper from the service: what the
// helper read is let go once it stops reading it.
function handleKeys(msg) {
    if (msg.t === 'key') {
        helperPress(msg.d);
        return;
    }
    if (msg.t !== 'keys') return;
    const before = keyStatus;
    keyStatus = readKeyStatus(msg.d);
    for (const action of ['ptt', 'pushMute']) {
        if (covered(action, before) && !covered(action)) releaseAction(action);
    }
    keyListeners.forEach((fn) => fn());
    notify();
}

let listening = false;
let watchingKeys = false;

function listenForKeys() {
    if (listening || typeof window === 'undefined') return;
    listening = true;
    listenIn(window);
}

function watchKeys() {
    if (watchingKeys || typeof window === 'undefined') return;
    watchingKeys = true;
    listenForKeys();
    onEvent(handleKeys);
}

// Signaling ---------------------------------------------------------------

function handle(msg) {
    if (msg.t === 'den' && call && msg.d.den === call.den) {
        if (msg.d.reset) loadDen(call);
        else denEvents(msg.d.events || []);
        return;
    }
    if (msg.t === 'reconnected' && call) {
        loadDen(call);
        return;
    }
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
    else if (e.refused) refused(e.refused);
    else if (e.talking) talked(e.talking);
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
            // rest brings another member's audio, or a share's (M4.2).
            const mine = own.getTransceivers()[0];
            mine.direction = 'sendonly';
            await mine.sender.replaceTrack(sent);
            c.attached = true;
        }
        // The den receives the member's screen and its sound on sections it
        // adds at their first share in the call.
        const sections = receivingSections(offer.sdp);
        c.screenMid = sections.screen;
        c.soundMid = sections.sound;
        await attachShare(own);
        await own.setLocalDescription();
        if (call !== c || pc !== own) return;
        c.answer = own.localDescription.sdp;
        sendCall('voice.answer', { den: c.den, version: offer.version, sdp: c.answer });
        sendParams();
        if (c.status === 'joining') {
            c.status = 'connecting';
            notify();
        }
    } catch {
        if (call !== c || pc !== own) return;
        sendCall('voice.leave', { den: c.den });
        note({ what: 'ended', why: 'browser' });
        finish({ text: "This browser couldn't set the call up." });
    }
}

function play(e) {
    const stream = e.streams[0];
    if (!stream) return;
    // A share the page watches plays in the player; one it didn't ask for,
    // which only a misbehaving den would send, plays nowhere.
    const sharer = screenOwner(stream.id);
    if (sharer || stream.id.startsWith('screen-')) {
        const w = watched.get(sharer);
        if (w && w.stream !== stream) {
            w.stream = stream;
            notify();
        }
        return;
    }
    const member = stream.id;
    let audio = audios.get(member);
    if (!audio) {
        audio = new Audio();
        audio.autoplay = true;
        const speaker = stored(SPEAKER);
        if (speaker && audio.setSinkId) audio.setSinkId(speaker).catch(() => {});
        audios.set(member, audio);
    }
    // Where audio may start without a click, the call's context plays the
    // member, as the call's sound (M12). Chromium decodes a member's stream
    // only while a media element plays it, so theirs plays it too, muted.
    if (autoplays()) playVoice(member, stream);
    level(member);
    audio.srcObject = stream;
    audio.play().catch(() => {});
    meter(member, stream);
    // A member who leaves takes their track with them.
    stream.onremovetrack = () => {
        if (stream.getTracks().length === 0 && audio.srcObject === stream) {
            audio.srcObject = null;
            audios.delete(member);
            unmeter(member);
            dropVoice(member);
        }
    };
}

// playVoice plays a member's stream through the call's context, at the
// level the member here chose for them (M12).
function playVoice(member, stream) {
    if (!callContext) return;
    dropVoice(member);
    try {
        const source = callContext.createMediaStreamSource(stream);
        const gain = callContext.createGain();
        source.connect(gain).connect(callContext.destination);
        voices.set(member, { source, gain });
    } catch {
        // A stream without audio yet, as in meter.
    }
}

function dropVoice(member) {
    const v = voices.get(member);
    if (!v) return;
    v.source.disconnect();
    v.gain.disconnect();
    voices.delete(member);
}

// level plays a member as loud as the member here chose, under the call's
// volume, and not at all while they're deafened: through the call's
// context where it plays them, with their element muted, and through their
// element otherwise.
function level(member) {
    const audio = audios.get(member);
    const v = voices.get(member);
    const volume = volumeIn(volumes, call?.den, member) * wholeVolume;
    if (audio) {
        audio.volume = volume;
        audio.muted = !!v || isDeafened();
    }
    if (v) v.gain.gain.value = isDeafened() ? 0 : volume;
}

// playAgain plays again each element that holds a stream but stopped, as
// a frozen page's do.
export function playAgain(elements) {
    for (const el of elements) if (el.srcObject && el.paused) el.play().catch(() => {});
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
        dropped('failed', 'resume_failed');
        return;
    }
    if (reason === 'failed') dropped(reason, 'den_failed');
    else finish({ text: endedText(reason, call.ports) });
}

// hold keeps the call while the den is away: the peer connection stays,
// and its media flows if its path does.
function hold() {
    const c = call;
    if (!c || !pc) {
        if (c) dropped('disconnected', 'den_away');
        return;
    }
    c.held = true;
    c.resuming = false;
    clearTimeout(c.restartBy);
    c.restartBy = null;
    // The den's word on push to talk waits for the resume (M11).
    talkers = new Set();
    ring();
    if (c.status !== 'reconnecting') {
        c.status = 'reconnecting';
        clearTimeout(c.giveUp);
        c.giveUp = setTimeout(() => gaveUp(c), REJOIN_WINDOW);
    }
    notify();
    tryResume();
}

// gaveUp ends a call that couldn't be taken back or joined again in time.
function gaveUp(c) {
    if (call !== c || c.status !== 'reconnecting') return;
    note({ what: 'ended', why: 'lost' });
    finish({ text: endedText('lost') });
}

// tryResume asks the den for the held call once the den and the service
// are back.
function tryResume() {
    const c = call;
    if (!c || !c.held || c.resuming || !pc || !live || denStates.get(c.den) !== 'connected') return;
    c.resuming = true;
    if (!sendCall('voice.join', { den: c.den, channel: c.channel, muted: c.muted, deafened: c.deafened, resume: true })) {
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
        restart(c, 'resumed');
    }
    // A mark changed while the den was away goes now, and push to talk the
    // den let go of as the socket closed (M11).
    sendCall('voice.mute', { den: c.den, muted: c.muted, deafened: c.deafened });
    c.talkSent = false;
    tellTalking();
    notify();
}

// dropped holds on to a call whose connection broke, to join it again
// once the den and this computer's service are back. A call that failed
// without ever coming up most likely can't reach the den's media ports,
// which trying again won't open. why says what broke, for the log.
function dropped(reason, why) {
    const c = call;
    closePeer();
    if (reason === 'failed' && !c.connected) {
        note({ what: 'ended', why: 'never_connected' });
        finish({ text: endedText('failed', c.ports) });
        return;
    }
    if (c.attempts >= REJOINS) {
        note({ what: 'ended', why: 'lost' });
        finish({ text: endedText('lost') });
        return;
    }
    note({ what: 'rejoin', why });
    c.attempts += 1;
    c.held = false;
    c.resuming = false;
    talkers = new Set();
    ring();
    if (c.status !== 'reconnecting') {
        c.status = 'reconnecting';
        clearTimeout(c.giveUp);
        c.giveUp = setTimeout(() => gaveUp(c), REJOIN_WINDOW);
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
    clearInterval(statsTimer);
    statsTimer = null;
    sentBefore = null;
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
        dropVoice(member);
    }
    audios.clear();
}

// finish ends the call for good, and says why when there's a reason to.
function finish(why) {
    clearTimeout(rejoinTimer);
    rejoinTimer = null;
    if (call) clearTimers(call);
    endShare('');
    watched.clear();
    denView = null;
    closePeer();
    mic?.getTracks().forEach((t) => t.stop());
    mic = null;
    sent = null;
    closeChain();
    warmContext?.close().catch(() => {});
    warmContext = null;
    stopMeters();
    audible = new Set();
    talkers = new Set();
    notice = why && call ? { ...why, den: call.den, channel: call.channel, label: call.label } : null;
    call = null;
    notify();
}

// Notes for the log (M11) -----------------------------------------------------

// The page notes what its call's connection does, which the service logs
// at debug by the notes' own fields alone: each change of its state, the
// restarts it asks for, each join again and giving up, with why, its tab
// hidden or shown, its page frozen and thawed (M12), and at each change and
// every STATS_EVERY a summary of its statistics. Notes made while the
// stream to the service is down wait, the newest NOTES_KEPT, and go once
// it's back, saying how long before.
let statsTimer = null;
const unsent = [];
// sentBefore is how much the call had sent at its last summary, and when,
// which the next measures the rate it sends at from.
let sentBefore = null;

function note(fields) {
    const c = call;
    if (!c) return;
    const n = { den: c.den, channel: c.channel, note: fields };
    if (live && !unsent.length && sendCall('voice.note', n)) return;
    unsent.push({ ...n, at: Date.now() });
    if (unsent.length > NOTES_KEPT) unsent.shift();
}

function sendNotes() {
    while (unsent.length) {
        const { at, ...n } = unsent[0];
        if (!sendCall('voice.note', { ...n, note: { ...n.note, ago: Date.now() - at } })) return;
        unsent.shift();
    }
}

// noteStats notes a summary of the call's statistics, with the rate it has
// sent at since the last, and how far the voice processor runs behind,
// which tell a renderer short of CPU, as on a phone off screen, apart from
// Opus's rate falling with the network's (M12).
async function noteStats() {
    const own = pc;
    if (!own) return;
    let report;
    try {
        report = await own.getStats();
    } catch {
        return;
    }
    if (pc !== own) return;
    const stats = summarize(report);
    const now = performance.now();
    stats.send_rate = sendRate(sentBefore, stats.bytes_sent, now);
    sentBefore = { bytes: stats.bytes_sent, at: now };
    const behind = processorBehind();
    if (behind !== null) stats.behind = behind;
    note({ what: 'stats', stats });
}

// sendRate is the rate in kbit/s a call sent at from before, what it had
// sent and when, to now, when it has sent bytes; 0 without before.
export function sendRate(before, bytes, now) {
    if (!before || now <= before.at) return 0;
    return Math.round((Math.max(0, bytes - before.bytes) * 8) / (now - before.at));
}

// processorBehind is how far the voice processor's audio has fallen behind
// the clock since the call first summed its statistics up, in milliseconds,
// or null where no processor runs. A processor that misses its turns, as
// in a renderer short of CPU, renders less than the time gone by, and what
// the call sends has gaps.
function processorBehind() {
    const ctx = chain?.ctx;
    const t = ctx?.getOutputTimestamp?.();
    if (!t?.performanceTime) return null;
    chain.clock ??= t;
    return behindBy(chain.clock, t);
}

// behindBy is how far an audio context's clock fell behind the page's
// between two of its output timestamps, in milliseconds.
export function behindBy(start, now) {
    return Math.max(0, Math.round(now.performanceTime - start.performanceTime - (now.contextTime - start.contextTime) * 1000));
}

// summarize sums a getStats report up for the log: the path the call takes,
// its protocol and its candidates' types, packets and bytes sent and
// received, packets lost on the way in, and jitter and round-trip time, in
// milliseconds, with the rate in kbit/s its encoder aims at (M12), which
// Firefox doesn't say. Chromium names the path in its transport's stats,
// and Firefox marks its candidate pair selected; Firefox times the round
// trip only in what the den reports back.
export function summarize(report) {
    const all = [...report.values()];
    const byID = new Map(all.map((x) => [x.id, x]));
    const transport = all.find((x) => x.type === 'transport' && x.selectedCandidatePairId);
    const pair = byID.get(transport?.selectedCandidatePairId) || all.find((x) => x.type === 'candidate-pair' && x.selected) ||
        all.find((x) => x.type === 'candidate-pair' && x.nominated && x.state === 'succeeded');
    const local = byID.get(pair?.localCandidateId);
    const remote = byID.get(pair?.remoteCandidateId);
    const sum = (type, field) => all.filter((x) => x.type === type).reduce((n, x) => n + (Number(x[field]) || 0), 0);
    const ms = (seconds) => Math.round((Number(seconds) || 0) * 1000);
    const rtt = pair?.currentRoundTripTime ?? all.find((x) => x.type === 'remote-inbound-rtp' && x.roundTripTime !== undefined)?.roundTripTime;
    const jitter = Math.max(0, ...all.filter((x) => x.type === 'inbound-rtp').map((x) => Number(x.jitter) || 0));
    // The voice goes on the den's first section, and a share's sound on one
    // of its own.
    const outAudio = all.filter((x) => x.type === 'outbound-rtp' && x.kind === 'audio');
    const voiceOut = outAudio.find((x) => x.mid === '0') || outAudio[0];
    return {
        protocol: local?.protocol || '', local: local?.candidateType || '', remote: remote?.candidateType || '',
        packets_sent: sum('outbound-rtp', 'packetsSent'), bytes_sent: sum('outbound-rtp', 'bytesSent'),
        packets_received: sum('inbound-rtp', 'packetsReceived'), bytes_received: sum('inbound-rtp', 'bytesReceived'),
        packets_lost: Math.max(0, sum('inbound-rtp', 'packetsLost')), jitter: ms(jitter), rtt: ms(rtt),
        target_rate: Math.round((Number(voiceOut?.targetBitrate) || 0) / 1000),
    };
}

// Screen share --------------------------------------------------------------

// loadDen fetches what the page needs to know of the call's den for
// sharing and watching, as the call starts and after the page's stream
// comes back; den events keep it current between.
async function loadDen(c) {
    try {
        const v = await api.get(`/api/dens/${c.den}/state`);
        if (call !== c) return;
        denView = {
            den: c.den,
            me: v.me.id,
            limits: v.call_limits,
            calls: v.calls || [],
            names: new Map((v.members || []).map((m) => [m.id, m.display_name])),
        };
        followDen();
    } catch {
        // It comes again with the stream; sharing waits for it.
    }
}

// denEvents applies a den's events to what the page knows of it.
function denEvents(events) {
    if (!denView) return;
    for (const e of events) {
        if (e.t === 'voice.state') denView = { ...denView, calls: applyCalls(denView.calls, e.d) };
        else if (e.t === 'den.updated' && e.d.call_limits) denView = { ...denView, limits: e.d.call_limits };
        else if (e.t === 'member.joined' || e.t === 'member.updated') {
            denView = { ...denView, names: new Map(denView.names).set(e.d.id, e.d.display_name) };
        }
    }
    followDen();
}

// myCall is the den's word on the page's call, and me this member in it.
function myCall() {
    const c = call;
    const found = c && denView ? denView.calls.find((x) => x.channel_id === c.channel) : null;
    return { found, me: found?.members.find((m) => m.id === denView.me) };
}

// followDen keeps the page's share and watching in step with the den: a
// share the den took goes live, one it ended stops, a share nobody watches
// pauses, and a share that ended leaves the player.
function followDen() {
    const { found, me } = myCall();
    if (share && found) {
        if (me?.sharing && share.status === 'starting') {
            share.status = 'live';
            clearTimeout(share.startBy);
        } else if (!me?.sharing && share.status === 'live') {
            endShare(me?.staff_muted ? 'Staff muted you, which ended your share.' : 'Your share ended.');
        }
        sendParams();
    } else if (!share && me?.sharing && call && !call.held) {
        // A share this page stopped while the den was away still runs there.
        sendCall('voice.share', { den: call.den, on: false });
    }
    if (found) {
        for (const member of [...watched.keys()]) {
            if (!found.members.some((m) => m.id === member && m.sharing)) watched.delete(member);
        }
    }
    notify();
}

// shareSettings is how the member shares, as the browser kept it.
export function shareSettings() {
    return readShare(stored(SHARE));
}

export function setShareSettings(changes) {
    store(SHARE, JSON.stringify(readShare(JSON.stringify({ ...shareSettings(), ...changes }))));
    notify();
}

// startShare asks the browser for a screen, a window or a tab to share,
// with its sound if the member wants it and the browser can, and shares it
// in the call. A share the den doesn't allow now is never asked for.
export async function startShare() {
    const c = call;
    if (!c || share) return;
    if (!canShare()) {
        shareNotice = { text: NO_SHARING };
        notify();
        return;
    }
    if (!denView) await loadDen(c);
    if (call !== c || share || !denView) return;
    shareNotice = null;
    const blocked = shareBlocked(denView.limits, denView.calls);
    if (blocked) {
        shareNotice = { text: blocked };
        notify();
        return;
    }
    const prefs = shareSettings();
    const quality = shareQuality(denView.limits, prefs);
    const mine = { status: 'picking', quality, wanted: prefs.sound };
    share = mine;
    notify();
    let stream;
    try {
        stream = await navigator.mediaDevices.getDisplayMedia(displayOptions(quality, prefs.sound));
    } catch (e) {
        if (share === mine) {
            share = null;
            // Closing the browser's picker is no failure.
            shareNotice = e?.name === 'NotAllowedError' || e?.name === 'AbortError' ? null : { text: "This browser couldn't share your screen." };
            notify();
        }
        return;
    }
    if (call !== c || share !== mine) {
        stream.getTracks().forEach((t) => t.stop());
        return;
    }
    mine.stream = stream;
    mine.video = stream.getVideoTracks()[0];
    mine.audio = stream.getAudioTracks()[0] || null;
    // The member's own screen shows in the player, from the capture itself,
    // when the player holds nothing else; otherwise when they ask.
    mine.preview = watched.size === 0;
    mine.video.contentHint = prefs.hint === 'motion' ? 'motion' : 'detail';
    // The browser's own stop button, or the shared window closing, ends it.
    mine.video.onended = () => share === mine && stopShare();
    awaitShare(mine);
    mine.timer = setInterval(sendParams, SHARE_CHECK);
    if (pc) await attachShare(pc);
    sendCall('voice.share', { den: c.den, on: true, sound: !!mine.audio });
    sendParams();
    notify();
}

// awaitShare has a share wait for the den to take it, and gives it up if
// the den never says.
function awaitShare(s) {
    s.status = 'starting';
    clearTimeout(s.startBy);
    s.startBy = setTimeout(() => {
        if (share === s && s.status === 'starting') endShare("The den didn't start your share. Try again in a moment.");
    }, SHARE_WAIT);
}

// setPreview shows the member's own screen in the player, or hides it.
export function setPreview(on) {
    if (!share?.stream) return;
    share.preview = !!on;
    notify();
}

// stopShare ends the page's share, at the member's word.
export function stopShare() {
    if (!share) return;
    if (call) sendCall('voice.share', { den: call.den, on: false });
    endShare('');
}

// endShare stops capturing and sending the page's share, and says why when
// the den ended it. The sections it went on stay, for the next share.
function endShare(why) {
    const s = share;
    if (s) {
        share = null;
        clearInterval(s.timer);
        clearTimeout(s.startBy);
        s.stream?.getTracks().forEach((t) => t.stop());
        for (const mid of [call?.screenMid, call?.soundMid]) {
            const t = mid && pc ? pc.getTransceivers().find((x) => x.mid === mid) : null;
            t?.sender.replaceTrack(null).catch(() => {});
        }
    }
    shareNotice = why ? { text: why } : null;
    notify();
}

// attachShare puts the page's screen and its sound on the den's sections
// for them, once the den has added them.
async function attachShare(own) {
    const c = call;
    const s = share;
    if (!c || !s?.video) return;
    for (const [mid, track] of [[c.screenMid, s.video], [c.soundMid, s.audio]]) {
        const t = mid && track ? own.getTransceivers().find((x) => x.mid === mid) : null;
        if (!t) continue;
        if (t.direction !== 'sendonly') t.direction = 'sendonly';
        if (t.sender.track !== track) await t.sender.replaceTrack(track);
    }
}

// sendParams holds the page's share to the den's bitrate and the size and
// frame rate it goes at, the size as the window shared is now, and pauses
// it while nobody watches. Chromium starts again with a keyframe; for
// Firefox, which doesn't, the den asks for one when packets come again.
async function sendParams() {
    const c = call;
    const s = share;
    const t = c?.screenMid && pc && s?.video ? pc.getTransceivers().find((x) => x.mid === c.screenMid) : null;
    if (!t || t.sender.track !== s.video) return;
    const p = t.sender.getParameters();
    const e = p.encodings?.[0];
    if (!e) return;
    const size = s.video.getSettings();
    const scale = fitPixels(size.width, size.height, s.quality.height).scale;
    const active = watchersOf(myCall().found, denView?.me).length > 0;
    if (e.maxBitrate === s.quality.bitrate && e.maxFramerate === s.quality.fps && Math.abs((e.scaleResolutionDownBy || 1) - scale) < 0.01 &&
        e.active === active) return;
    e.maxBitrate = s.quality.bitrate;
    e.maxFramerate = s.quality.fps;
    e.scaleResolutionDownBy = scale;
    e.active = active;
    await t.sender.setParameters(p).catch(() => {});
}

// watchShare watches a member's share in the page's call, which plays in
// the player once it comes. label names the member for the player.
export function watchShare(member, label) {
    const c = call;
    if (!c || watched.has(member) || watched.size >= MAX_WATCHING) return;
    watched.set(member, { member, label, stream: null, volume: 1 });
    shareNotice = null;
    sendCall('voice.watch', { den: c.den, member_id: member, on: true });
    notify();
}

// unwatchShare stops watching a member's share, and unwatchAll every one.
export function unwatchShare(member) {
    if (!watched.delete(member)) return;
    if (call) sendCall('voice.watch', { den: call.den, member_id: member, on: false });
    notify();
}

export function unwatchAll() {
    for (const member of [...watched.keys()]) unwatchShare(member);
}

// closePlayer stops watching every share and hides the member's own.
export function closePlayer() {
    if (share) share.preview = false;
    unwatchAll();
    notify();
}

// joinAndWatch joins a voice channel's call to watch a member's share in
// it, as clicking the share's mark outside the call does.
export async function joinAndWatch(den, channel, label, member, name) {
    // Joining sends the join before it returns, and the den takes frames in
    // order, so the watch finds the member in the call.
    if (!(call && call.den === den && call.channel === channel)) await joinCall(den, channel, label);
    if (call && call.den === den && call.channel === channel) watchShare(member, name);
}

// setShareVolume sets how loud a watched share's sound plays here.
export function setShareVolume(member, volume) {
    const w = watched.get(member);
    if (!w) return;
    w.volume = clampVolume(volume);
    notify();
}

// speakerID is the speaker the member chose, which shares play on too.
export function speakerID() {
    return stored(SPEAKER);
}

// refused is the den refusing the page's share, or its watch of one.
function refused(r) {
    const limits = denView?.limits;
    if (r.what === 'share') {
        endShare(refusalText('share', r.reason, limits));
        return;
    }
    watched.delete(r.member_id);
    shareNotice = { text: refusalText('watch', r.reason, limits) };
    notify();
}

export function clearShareNotice() {
    shareNotice = null;
    notify();
}

// Speaking ----------------------------------------------------------------

function startMeters() {
    if (callContext) return;
    try {
        callContext = new AudioContext();
        callContext.resume().catch(() => {});
    } catch {
        callContext = null;
        return;
    }
    meterTimer = setInterval(measure, METER_MS);
}

function stopMeters() {
    clearInterval(meterTimer);
    meterTimer = null;
    for (const key of [...meters.keys()]) unmeter(key);
    callContext?.close().catch(() => {});
    callContext = null;
    lastLoud.clear();
    speaking = new Set();
}

// meter measures a stream's level, by member, or "self" for this page's.
// Chromium measures a remote stream only while a media element plays it,
// as play() does.
function meter(key, stream) {
    if (!callContext) return;
    unmeter(key);
    try {
        const source = callContext.createMediaStreamSource(stream);
        const analyser = callContext.createAnalyser();
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
    audible = speakingNow(levels, lastLoud, now);
    ring();
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
//
// The call's volume turns everyone down at once, under their own (M12):
// a phone's loudspeaker plays a call loud even at its lowest call volume.
// The browser keeps it too.

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

// readCallVolume reads the call's volume the browser kept: full unless
// it's a volume.
export function readCallVolume(text) {
    const n = Number(text);
    return text && Number.isFinite(n) ? clampVolume(n) : 1;
}

// callVolume is how loud the whole call plays here.
export function callVolume() {
    return wholeVolume;
}

// setCallVolume sets how loud the whole call plays here, mid-call too, and
// keeps it.
export function setCallVolume(volume) {
    wholeVolume = clampVolume(volume);
    store(CALL_VOLUME, wholeVolume === 1 ? '' : String(wholeVolume));
    for (const member of audios.keys()) level(member);
}

// setVolume sets how loud a member plays here, mid-call too, and keeps it.
export function setVolume(den, member, volume) {
    volumes = withVolume(volumes, den, member, volume);
    store(VOLUMES, Object.keys(volumes).length ? JSON.stringify(volumes) : '');
    if (call?.den === den) level(member);
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
    await restartMicTest();
}

// chooseSpeaker plays the call through a speaker; '' is the default.
export function chooseSpeaker(id) {
    store(SPEAKER, id);
    for (const audio of audios.values()) audio.setSinkId?.(id).catch(() => {});
}

// byName orders a call's members by display name for the page, as the
// member list orders a den's; the den lists them as they joined. members
// maps IDs to members, and ties go by ID, so the order holds still.
export function byName(list, members) {
    const name = (m) => members.get(m.id)?.display_name || '';
    return [...list].sort((a, b) => name(a).localeCompare(name(b)) || a.id.localeCompare(b.id, undefined, { numeric: true }));
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
