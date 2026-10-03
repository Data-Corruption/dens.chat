// The call this page holds (M2): its microphone, its peer connection to
// the den, and the audio of everyone else in the call. The page signals
// over its event stream, and the service on this computer relays to the
// den, writing into each offer where the den's media goes. So the page
// names no STUN or TURN server, and a call reaches nothing but the den.
// The den makes every offer; the page only answers.

import { onConnection, onEvent, sendCall } from './events.js';

const MIC = 'DENS_MIC';
const SPEAKER = 'DENS_SPEAKER';
const MUTED = 'DENS_MUTED';
// A call that drops joins again, as the den and this computer's service
// come back, this many times within this long, before it gives up.
const REJOINS = 5;
const REJOIN_WINDOW = 2 * 60 * 1000;
// A join the den doesn't offer anything for within this long has failed.
const OFFER_WAIT = 20 * 1000;

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
let mic = null;
// audios plays each other member's stream, by their member ID: the den
// labels the stream it sends their audio on with it.
const audios = new Map();
// What the page last heard about each den's connection, and its own
// stream to the service.
let denStates = new Map();
let live = true;
let rejoinTimer = null;
let started = false;

function start() {
    if (started) return;
    started = true;
    onEvent(handle);
    onConnection((up) => {
        live = up;
        // The service hangs up a page's call when the page's stream closes.
        if (!up && call && call.status !== 'reconnecting') dropped('disconnected');
        tryRejoin();
    });
}

function snapshot() {
    return {
        call: call && { den: call.den, channel: call.channel, label: call.label, status: call.status, muted: call.muted },
        notice,
    };
}

function notify() {
    const s = snapshot();
    listeners.forEach((fn) => fn(s));
}

// onCall calls fn with the call and the last notice, now and on every
// change; it returns a function that stops.
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
    closePeer();
    notice = null;
    call = { den, channel, label, status: 'joining', muted: stored(MUTED) === '1', attempts: 0, connected: false };
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
    mic?.getAudioTracks().forEach((t) => {
        t.enabled = !muted;
    });
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
    c.attached = false;
    pc = new RTCPeerConnection({ iceServers: [] });
    const own = pc;
    pc.ontrack = (e) => play(e);
    pc.onconnectionstatechange = () => {
        if (pc !== own || call !== c) return;
        if (pc.connectionState === 'connected') {
            c.status = 'connected';
            c.connected = true;
            c.attempts = 0;
            notify();
        } else if (pc.connectionState === 'failed') {
            sendCall('voice.leave', { den: c.den });
            dropped('failed');
        }
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

async function openMic() {
    const want = stored(MIC);
    const audio = { echoCancellation: true, noiseSuppression: true, autoGainControl: true };
    try {
        mic = await navigator.mediaDevices.getUserMedia({ audio: want ? { ...audio, deviceId: { exact: want } } : audio });
    } catch (e) {
        // A microphone that's gone gives way to the default.
        if (!want || (e.name !== 'OverconstrainedError' && e.name !== 'NotFoundError')) throw e;
        store(MIC, '');
        mic = await navigator.mediaDevices.getUserMedia({ audio });
    }
    mic.getAudioTracks().forEach((t) => {
        t.enabled = !call?.muted;
    });
}

function handle(msg) {
    if (msg.t === 'dens') {
        denStates = new Map(msg.d.dens.map((d) => [d.den_id, d.state]));
        // A den that shut this device out ends its call for good.
        const state = call && denStates.get(call.den);
        if (state === 'removed' || state === 'revoked') finish({ text: endedText('gone') });
        else tryRejoin();
        return;
    }
    if (msg.t !== 'call' || !call) return;
    const e = msg.d;
    if (e.den !== call.den || e.channel !== call.channel) return;
    if (e.offer) answer(e.offer);
    else if (e.ended) ended(e.ended);
}

async function answer(offer) {
    const c = call;
    const own = pc;
    if (!own || offer.version <= c.version) return;
    c.version = offer.version;
    c.ports = [offer.udp_port, offer.tcp_port];
    try {
        await own.setRemoteDescription({ type: 'offer', sdp: offer.sdp });
        if (!c.attached) {
            // The den's first section takes the microphone; each of the
            // rest brings another member's audio.
            const mine = own.getTransceivers()[0];
            mine.direction = 'sendonly';
            await mine.sender.replaceTrack(mic.getAudioTracks()[0]);
            c.attached = true;
        }
        await own.setLocalDescription();
        if (call !== c || pc !== own) return;
        sendCall('voice.answer', { den: c.den, version: offer.version, sdp: own.localDescription.sdp });
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
    audio.srcObject = stream;
    audio.play().catch(() => {});
    // A member who leaves takes their track with them.
    stream.onremovetrack = () => {
        if (stream.getTracks().length === 0 && audio.srcObject === stream) {
            audio.srcObject = null;
            audios.delete(member);
        }
    };
}

function ended(reason) {
    if (reason === 'disconnected' || reason === 'failed') dropped(reason);
    else finish({ text: endedText(reason, call.ports) });
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
    if (c.status !== 'reconnecting') {
        c.status = 'reconnecting';
        c.giveUp = setTimeout(() => {
            if (call === c && c.status === 'reconnecting') finish({ text: endedText('lost') });
        }, REJOIN_WINDOW);
    }
    notify();
    tryRejoin();
}

function tryRejoin() {
    const c = call;
    if (!c || c.status !== 'reconnecting' || pc || rejoinTimer || !live || denStates.get(c.den) !== 'connected') return;
    rejoinTimer = setTimeout(() => {
        rejoinTimer = null;
        if (call === c && c.status === 'reconnecting' && !pc && live && denStates.get(c.den) === 'connected') connect();
    }, 500 * c.attempts);
}

function closePeer() {
    if (pc) {
        pc.ontrack = null;
        pc.onconnectionstatechange = null;
        pc.close();
        pc = null;
    }
    for (const audio of audios.values()) {
        audio.pause();
        audio.srcObject = null;
    }
    audios.clear();
}

// finish ends the call for good, and says why when there's a reason to.
function finish(why) {
    clearTimeout(rejoinTimer);
    rejoinTimer = null;
    if (call) {
        clearTimeout(call.giveUp);
        clearTimeout(call.offerBy);
    }
    closePeer();
    mic?.getTracks().forEach((t) => t.stop());
    mic = null;
    notice = why && call ? { ...why, den: call.den, channel: call.channel, label: call.label } : null;
    call = null;
    notify();
}

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
    if (!call || !mic) return;
    const old = mic;
    mic = null;
    try {
        await openMic();
    } catch (e) {
        mic = old;
        throw e;
    }
    const sender = pc?.getTransceivers()[0]?.sender;
    if (sender && call?.attached) await sender.replaceTrack(mic.getAudioTracks()[0]);
    old.getTracks().forEach((t) => t.stop());
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
