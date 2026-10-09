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

import { api } from './api.js';
import { onConnection, onEvent, sendCall } from './events.js';
import { MAX_WATCHING, displayOptions, fitPixels, readShare, receivingSections, refusalText, screenOwner, shareBlocked, shareQuality, watchersOf } from './share.js';

const MIC = 'DENS_MIC';
const SPEAKER = 'DENS_SPEAKER';
const MUTED = 'DENS_MUTED';
const DEAFENED = 'DENS_DEAFENED';
const RNNOISE = 'DENS_RNNOISE';
const VOLUMES = 'DENS_VOLUMES';
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
    listenForKeys();
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
    endShare('');
    watched.clear();
    denView = null;
    stopMicTest();
    call = { den, channel, label, status: 'joining', muted: stored(MUTED) === '1', deafened: stored(DEAFENED) === '1', attempts: 0, connected: false };
    loadDen(call);
    // The click that joins lets audio start, so the meters' context starts
    // here, and the voice processor's: one made after the browser has asked
    // for the microphone may need another click to start.
    startMeters();
    if (!mic && !processorFailed) warmContext ??= new AudioContext({ sampleRate: 48000 });
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
    for (const audio of audios.values()) audio.muted = !on;
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
        if (!mic) await openMic();
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
    pc.ontrack = (e) => play(e);
    pc.onconnectionstatechange = () => {
        if (pc !== own || call !== c) return;
        stateChanged(c, own.connectionState);
    };
    if (!sendCall('voice.join', { den: c.den, channel: c.channel, muted: c.muted, deafened: c.deafened })) {
        dropped('disconnected');
        return;
    }
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
    const v = await openVoice();
    closeChain();
    mic = v.stream;
    chain = v.chain;
    sent = v.track;
    applySending();
    meter('self', new MediaStream([sent]));
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

// reopenMic opens the microphone again as chosen, and sends it.
async function reopenMic() {
    if (!call || !mic) return;
    const old = mic;
    const oldChain = chain;
    chain = null;
    mic = null;
    try {
        await openMic();
    } catch (e) {
        mic = old;
        chain = oldChain;
        throw e;
    }
    const sender = pc?.getTransceivers()[0]?.sender;
    if (sender && call?.attached) await sender.replaceTrack(sent);
    old.getTracks().forEach((t) => t.stop());
    if (oldChain) oldChain.ctx.close().catch(() => {});
}

// Sending -----------------------------------------------------------------

// A member sends by voice activity ("voice"), automatically or above a
// level in dBFS, or while they push to talk ("ptt"), and binds keys for
// push to talk, toggle mute and push to mute. The browser keeps it all.
export const VOICE_DEFAULTS = { mode: 'voice', auto: true, threshold: -50, keys: {} };
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
    const keys = {};
    for (const name of KEYS) {
        const k = kept.keys?.[name];
        if (k && typeof k.code === 'string' && k.code && typeof k.label === 'string') {
            keys[name] = { code: k.code, label: k.label, ctrl: !!k.ctrl, alt: !!k.alt, shift: !!k.shift, meta: !!k.meta };
        }
    }
    return {
        mode: kept.mode === 'ptt' ? 'ptt' : 'voice',
        auto: typeof kept.auto === 'boolean' ? kept.auto : VOICE_DEFAULTS.auto,
        threshold: Number.isFinite(kept.threshold) ? Math.min(0, Math.max(-100, Math.round(kept.threshold))) : VOICE_DEFAULTS.threshold,
        keys,
    };
}

let voice = readVoice(stored(VOICE));
let pttHeld = false;
let pushMuted = false;

export function voiceSettings() {
    return voice;
}

// setVoice changes the voice settings, which take hold at once, in a call
// and in the settings' microphone test.
export function setVoice(changes) {
    voice = readVoice(JSON.stringify({ ...voice, ...changes }));
    store(VOICE, JSON.stringify(voice));
    pttHeld = pushMuted = false;
    applySending();
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
// holds the track back itself, and voice activity sends everything.
function applySending() {
    for (const c of [chain, testing?.chain]) c?.node.port.postMessage({ gate: gateFor(voice, c.rnnoise, pttHeld) });
    if (sent) sent.enabled = !isMuted() && (chain || voice.mode !== 'ptt' || pttHeld);
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

// keyMatches says whether a key event is a binding's key, with exactly its
// modifiers.
export function keyMatches(binding, e) {
    return !!binding && e.code === binding.code && !!e.ctrlKey === binding.ctrl && !!e.altKey === binding.alt &&
        !!e.shiftKey === binding.shift && !!e.metaKey === binding.meta;
}

// typesInto says whether a key event would type into the field it's in:
// keys don't fire there, unless they're function keys or held with Ctrl,
// Alt or Meta.
export function typesInto(e) {
    const t = e.target;
    const field = t && (t.isContentEditable || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' ||
        (t.tagName === 'INPUT' && !UNTYPED.includes(t.type)));
    return !!field && !e.ctrlKey && !e.altKey && !e.metaKey && !/^F\d+$/.test(e.code);
}

// UNTYPED are the inputs keys type nothing into.
const UNTYPED = ['checkbox', 'radio', 'range', 'button', 'submit', 'reset', 'color', 'file', 'image'];

// keyLabel names a binding as the member pressed it.
export function keyLabel(b) {
    if (!b) return '';
    const mods = [b.ctrl && 'Ctrl', b.alt && 'Alt', b.shift && 'Shift', b.meta && 'Meta'].filter(Boolean);
    return [...mods, b.label].join('+');
}

// captureKey resolves to the next key pressed, as a binding, or null on
// Escape; nothing else hears it.
let capture = null;

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
    const k = voice.keys;
    if (keyMatches(k.ptt, e)) {
        e.preventDefault();
        if (!pttHeld) {
            pttHeld = true;
            applySending();
        }
    } else if (e.repeat) {
        // A held toggle toggles once.
    } else if (keyMatches(k.toggleMute, e)) {
        e.preventDefault();
        setMuted(!isMuted());
    } else if (keyMatches(k.pushMute, e)) {
        e.preventDefault();
        if (!pushMuted && !isMuted()) {
            pushMuted = true;
            setMuted(true);
        }
    }
}

// keyup ends a push by its key alone, whatever modifiers were let go first.
function keyup(e) {
    if (pttHeld && e.code === voice.keys.ptt?.code) {
        pttHeld = false;
        applySending();
    }
    if (pushMuted && e.code === voice.keys.pushMute?.code) {
        pushMuted = false;
        setMuted(false);
    }
}

// release lets go of whatever is pushed when the page loses focus, which
// keeps its key's release from reaching it.
function release() {
    if (pttHeld) {
        pttHeld = false;
        applySending();
    }
    if (pushMuted) {
        pushMuted = false;
        setMuted(false);
    }
}

let listening = false;

function listenForKeys() {
    if (listening || typeof window === 'undefined') return;
    listening = true;
    window.addEventListener('keydown', keydown, true);
    window.addEventListener('keyup', keyup, true);
    window.addEventListener('blur', release);
    document.addEventListener('visibilitychange', () => document.hidden && release());
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
    audio.volume = volumeIn(volumes, call?.den, member);
    audio.muted = isDeafened();
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
        restart(c);
    }
    // A mark changed while the den was away goes now.
    sendCall('voice.mute', { den: c.den, muted: c.muted, deafened: c.deafened });
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
    notice = why && call ? { ...why, den: call.den, channel: call.channel, label: call.label } : null;
    call = null;
    notify();
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
