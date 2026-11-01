// The page's sounds (M14.2): seven of Kenney's Interface Sounds, which are
// CC0, among the page's assets: a message, a member joining and leaving the
// call, a share starting and ending, and the member's microphone opening
// and closing. The member turns each off, sets its volume, or replaces it
// with a file of their own, which the service keeps, beside the
// notifications' settings, so every page paired with the install shares
// them, and sends them with the notifications' status.
//
// A sound plays as the call's voices play: through the call's context
// where the page plays its call through one, as in the Android app, which
// Android then routes as the call's, to the earpiece, the loudspeaker or
// Bluetooth (M12.2); and otherwise from an element on the speaker the
// member chose. A browser plays a page's sound only once the member has
// clicked in it, so a tab untouched since it opened plays none, as it
// plays no call.
//
// The service places the message's sound (see notify.js); the call's
// follow the call (see voice.js).

import { APIError, pageHeaders, reloadIfStale } from './api.js';

// SOUNDS are the page's sounds, by name, with what the settings call them.
export const SOUNDS = [
    ['message', 'Message'],
    ['joined', 'Joined'],
    ['left', 'Left'],
    ['shareStarted', 'Share started'],
    ['shareEnded', 'Share ended'],
    ['micOn', 'Microphone on'],
    ['micOff', 'Microphone off'],
];

// GROUPS are the settings' groups of them.
export const GROUPS = [
    ['Messages', ['message']],
    ['Calls', ['joined', 'left', 'shareStarted', 'shareEnded', 'micOn', 'micOff']],
];

// DEFAULT_VOLUMES are how loud each plays until the member sets it, in
// percent: set so each sounds about as loud as the others, measured by its
// loudness, and well under a call's voices.
export const DEFAULT_VOLUMES = { message: 60, joined: 35, left: 30, shareStarted: 50, shareEnded: 40, micOn: 65, micOff: 85 };

// A member's own sound is a file of at most 1 MiB, which the service
// checks, that plays for at most 10 seconds, which the page checks, since
// it takes decoding the file.
export const MAX_FILE = 1 << 20;
export const MAX_SECONDS = 10;

// soundSettings reads what the service keeps of a sound, from the
// notifications' status: whether it's on, its volume in percent, and its
// own file's version, 0 while it plays the page's.
export function soundSettings(status, name) {
    const s = status?.sounds?.[name];
    const v = s?.volume;
    const own = Number(status?.own?.[name]);
    return {
        on: !s?.off,
        volume: Number.isInteger(v) && v >= 0 && v <= 100 ? v : DEFAULT_VOLUMES[name],
        own: Number.isInteger(own) && own > 0 ? own : 0,
    };
}

// soundURL is where a sound plays from: the member's own file, by its
// version, from the client listener; or the page's, from its assets, by
// name.
export function soundURL(name, own, assets) {
    return own ? `/api/sounds/${name}?v=${own}` : assets?.[name] || '';
}

// callSounds says what a change in the page's call plays, from what the
// page heard of the call before, null as the call begins, and now: whether
// the den has the member in it (in), its members by ID with whether each
// shares (members, the member among them), and whether the member's
// microphone is open (mic). The member coming in plays joined, and going
// out left, without the microphone's sounds; others coming and going play
// them too, as do shares starting and ending, the member's own among them,
// though a member leaving takes their share with them, which plays left
// alone. A deafened member hears none of that, only their microphone's
// sounds, which answer what they did. Each sound plays once for a change,
// however many members it takes in.
export function callSounds(before, now, deafened) {
    const out = [];
    if (!now.in || !before?.in) {
        if (now.in) out.push('joined');
        else if (before?.in) out.push('left');
        return deafened ? [] : out;
    }
    const heard = new Set();
    for (const [id, sharing] of now.members) {
        if (!before.members.has(id)) heard.add('joined');
        else if (sharing && !before.members.get(id)) heard.add('shareStarted');
        else if (!sharing && before.members.get(id)) heard.add('shareEnded');
    }
    for (const id of before.members.keys()) if (!now.members.has(id)) heard.add('left');
    if (!deafened) for (const name of ['joined', 'left', 'shareStarted', 'shareEnded']) if (heard.has(name)) out.push(name);
    if (now.mic !== before.mic) out.push(now.mic ? 'micOn' : 'micOff');
    return out;
}

// Playing -----------------------------------------------------------------

// status is the notifications' status, which holds the sounds' settings.
let status = null;
// route says where the call plays, which voice.js says: its context, where
// the page plays its call through one, or null, and the speaker chosen.
let route = () => ({ context: null, speaker: '' });
// elements play the sounds outside a context, one each, made once and
// played again; buffers hold each decoded for a context, by address.
const elements = new Map();
const buffers = new Map();
let assetsRead = null;

function assets() {
    if (assetsRead) return assetsRead;
    try {
        assetsRead = JSON.parse(document.getElementById('app')?.dataset.sounds || '{}') || {};
    } catch {
        assetsRead = {};
    }
    return assetsRead;
}

// heardSounds takes the notifications' status as it comes, with the
// sounds' settings, and readies the sounds it names, so the first plays
// without waiting for its file. What it no longer names goes.
export function heardSounds(st) {
    status = st;
    const urls = new Set(SOUNDS.map(([name]) => soundURL(name, soundSettings(st, name).own, assets())).filter(Boolean));
    for (const url of [...elements.keys()]) if (!urls.has(url)) elements.delete(url);
    for (const url of [...buffers.keys()]) if (!urls.has(url)) buffers.delete(url);
    if (typeof Audio === 'function') for (const url of urls) elementFor(url);
}

// setSoundRoute has route say where the call plays.
export function setSoundRoute(fn) {
    route = fn;
}

// playSound plays a sound as the member set it, and reports whether it
// played: not at all while it's off, unless preview, as the settings' ▶;
// at its volume, or at volume, in percent, as the settings' slider moves.
export function playSound(name, { preview = false, volume } = {}) {
    const s = soundSettings(status, name);
    const url = soundURL(name, s.own, assets());
    if ((!s.on && !preview) || !url) return false;
    const level = (volume ?? s.volume) / 100;
    const { context, speaker } = route();
    if (context) playThrough(context, url, level);
    else playElement(url, level, speaker);
    return true;
}

function elementFor(url) {
    let el = elements.get(url);
    if (!el) {
        el = new Audio(url);
        el.preload = 'auto';
        elements.set(url, el);
    }
    return el;
}

// playElement plays a sound from its element, from the start, on the
// speaker chosen, or the default one where the browser can't choose.
function playElement(url, level, speaker) {
    const el = elementFor(url);
    el.volume = level;
    const start = () => {
        el.currentTime = 0;
        el.play().catch(() => {});
    };
    if (el.setSinkId && (el.sinkId || '') !== (speaker || '')) el.setSinkId(speaker || '').then(start, start);
    else start();
}

// bufferFor decodes a sound for a context, once.
function bufferFor(ctx, url) {
    let b = buffers.get(url);
    if (!b) {
        b = fetch(url, { credentials: 'same-origin' })
            .then((res) => {
                if (!res.ok) throw new Error(`HTTP ${res.status}`);
                return res.arrayBuffer();
            })
            .then((data) => ctx.decodeAudioData(data));
        buffers.set(url, b);
        b.catch(() => buffers.get(url) === b && buffers.delete(url));
    }
    return b;
}

// playThrough plays a sound through a context, as a call's voice plays
// there.
async function playThrough(ctx, url, level) {
    let buffer;
    try {
        buffer = await bufferFor(ctx, url);
    } catch {
        return;
    }
    if (ctx.state === 'closed') return;
    const source = ctx.createBufferSource();
    source.buffer = buffer;
    const gain = ctx.createGain();
    gain.gain.value = level;
    source.connect(gain).connect(ctx.destination);
    source.onended = () => {
        source.disconnect();
        gain.disconnect();
    };
    source.start();
}

// warmSounds decodes the sounds for a call's context as it starts, so the
// first of each plays without waiting.
export function warmSounds(ctx) {
    for (const [name] of SOUNDS) {
        const url = soundURL(name, soundSettings(status, name).own, assets());
        if (url) bufferFor(ctx, url).catch(() => {});
    }
}

// The member's own -------------------------------------------------------------

// checkSound says why a file can't take a sound's place, or '' where it
// can: at most 1 MiB, playing in this browser, for at most 10 seconds.
// decode resolves to how long a file plays, in seconds: the browser's, or
// a stand-in in the tests.
export async function checkSound(file, decode = decodeAudio) {
    if (file.size > MAX_FILE) return 'That file is over 1 MiB.';
    let seconds;
    try {
        seconds = await decode(await file.arrayBuffer());
    } catch {
        seconds = 0;
    }
    if (!(seconds > 0)) return "That file doesn't play in this browser. Try an MP3, Ogg or WAV file.";
    if (seconds > MAX_SECONDS) return `That sound lasts ${Math.ceil(seconds)} seconds, and one plays for at most ${MAX_SECONDS}.`;
    return '';
}

// decodeAudio decodes a file as the browser plays audio, and resolves to
// how long it plays, in seconds.
async function decodeAudio(data) {
    const ctx = new OfflineAudioContext(1, 1, 48000);
    return (await ctx.decodeAudioData(data)).duration;
}

// uploadSound gives the service a file of the member's to take a sound's
// place, and resolves to the notifications' status with it.
export async function uploadSound(name, file) {
    const res = await fetch(`/api/sounds/${name}`, {
        method: 'POST',
        headers: pageHeaders({ 'Content-Type': 'application/octet-stream' }),
        body: file,
        credentials: 'same-origin',
    });
    if (reloadIfStale(res)) return new Promise(() => {});
    let data = null;
    try {
        data = await res.json();
    } catch {
        // An answer that isn't JSON says only its status, below.
    }
    if (!res.ok) throw new APIError(data?.error || `The file wasn't kept (HTTP ${res.status}).`, res.status);
    return data;
}
