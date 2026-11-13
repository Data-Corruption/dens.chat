// Relay's sound in the page (M20.2): listening to another of the member's
// devices' sound, which starts at a click, as a browser needs before it
// plays anything, belongs to the page that clicked, as a call does, and
// ends with Stop or with the page; and this computer's own, which the
// service says who listens to, and the member stops. The player is a
// worker, which holds the listening's socket, and an audio worklet, which
// plays, in a context of its own, so the page's thread carries none of
// the sound. The buffer is the member's choice, kept by the browser:
// Low, 40 ms, or High, 150 ms, High on a phone until they choose. In the
// Android app a listening plays through the call's context while the
// phone is in a call: as media it goes silent on Bluetooth headphones
// once the call's microphone opens.

import { api } from './api.js';
import { onEvent } from './events.js';
import { autoplays, inApp } from './platform.js';

const KEY = 'DENS_RELAY_BUFFER';
export const BUFFER_LOW = 'low';
export const BUFFER_HIGH = 'high';
// BUFFERS are each choice's buffer, in milliseconds, and BATCHES the
// packets the service sends to a message: more, on High, wakes the page
// less often, which a phone feels.
export const BUFFERS = { [BUFFER_LOW]: 40, [BUFFER_HIGH]: 150 };
export const BATCHES = { [BUFFER_LOW]: 4, [BUFFER_HIGH]: 8 };
// The context runs at the rate most sources send; the worklet plays
// another by its own steps.
const RATE = 48000;

let chosen = null;

/** bufferChoice is the member's choice of buffer, Low or High */
export function bufferChoice(phone = inApp()) {
    let stored = chosen;
    try {
        stored = localStorage.getItem(KEY) ?? chosen;
    } catch {
        // Storage can be unavailable (private windows).
    }
    if (stored === BUFFER_LOW || stored === BUFFER_HIGH) return stored;
    return phone ? BUFFER_HIGH : BUFFER_LOW;
}

export function setBufferChoice(choice) {
    chosen = choice;
    try {
        localStorage.setItem(KEY, choice);
    } catch {
        // As above.
    }
}

// ENDED says why a listening ended, by the service's word for it, of the
// device it listened to.
const ENDED = {
    page: (name) => `Another tab took over listening to ${name}.`,
    taken: (name) => `Another of your devices took over listening to ${name}.`,
    stopped: (name) => `${name} stopped sharing.`,
    unshared: (name) => `${name} isn't sharing its sound.`,
    noanswer: (name) => `${name} didn't answer.`,
    lost: (name) => `Lost the link to ${name}.`,
    apart: (name) => `${name} isn't on this network.`,
    local: () => 'Android keeps Dens off your network.',
    closing: () => 'Dens stopped.',
    closed: () => 'Lost touch with Dens on this device.',
    player: () => "This browser couldn't play it.",
};

/** endedText says why a listening to the device named ended */
export function endedText(why, name) {
    return (ENDED[why] || ENDED.closed)(name);
}

// state is what the page knows of Relay: its own listening, with its
// device and how it stands (connecting, playing or waiting), and why the
// last one ended; this computer's sound, the device it goes to, as the
// service says, and why none comes; and the device holding the member's
// call that takes this install's keys, and whether they're muted there.
let state = { listening: null, notice: '', sharing: null, problem: '', call: null };
const listeners = new Set();
let watching = false;

function set(patch) {
    state = { ...state, ...patch };
    listeners.forEach((fn) => fn(state));
}

/** onRelay calls fn with the state now and as it changes; with stream,
    the service's word on this computer's sound and the member's call too,
    from the page's event stream */
export function onRelay(fn, stream = false) {
    if (stream && !watching) {
        watching = true;
        onEvent((m) => {
            if (m.t === 'relay') set({ sharing: m.d?.sharing || null, problem: m.d?.problem || '', call: m.d?.call || null });
        });
    }
    listeners.add(fn);
    fn(state);
    return () => listeners.delete(fn);
}

let player = null;
// callContext is the call's audio context while the phone is in a call,
// where a listening plays.
let callContext = null;
const modules = new WeakMap();

// ownContext is a listening's own context. On the phone it's opened for
// playback, unlike a call's: Chromium keeps a closed context's stream a
// few seconds for the next one opened like it, which would play as the
// call's, or not at all.
function ownContext() {
    return new AudioContext({ sampleRate: RATE, latencyHint: autoplays() ? 'playback' : 'interactive' });
}

/** listen plays a device's sound, {id, name}: called from the click that
    asks, which lets the context play. A listening already under way here
    ends. */
export async function listen(device, app = document.getElementById('app').dataset) {
    stopListening();
    const choice = bufferChoice();
    const own = callContext ? null : ownContext();
    const mine = { app, choice, ctx: own || callContext, own: !!own, node: null, moves: 0, worker: null, id: device.id, name: device.name, played: false };
    player = mine;
    set({ listening: { id: device.id, name: device.name, status: 'connecting' }, notice: '' });
    try {
        mine.worker = new Worker(app.relayWorker, { type: 'module' });
        mine.worker.onmessage = (e) => {
            if (e.data?.t === 'ended') end(mine, e.data.why);
            else if (e.data?.t === 'closed') end(mine, 'closed');
        };
        await place(mine, mine.ctx);
        if (player !== mine) return;
        const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = `${scheme}//${location.host}/api/relay/listen?device=${encodeURIComponent(device.id)}&batch=${BATCHES[choice]}`;
        mine.worker.postMessage({ url });
        await mine.ctx.resume();
    } catch {
        if (player === mine) end(mine, 'player');
    }
}

// place plays a listening through ctx: a worklet node there, which the
// worker feeds from then on, in place of the node before. It reports
// whether the listening still goes on, and still to ctx.
async function place(mine, ctx) {
    const move = ++mine.moves;
    if (!modules.has(ctx)) modules.set(ctx, ctx.audioWorklet.addModule(mine.app.relayWorklet));
    await modules.get(ctx);
    if (player !== mine || mine.moves !== move) return false;
    const node = new AudioWorkletNode(ctx, 'relay', {
        numberOfInputs: 0, outputChannelCount: [2], processorOptions: { buffer: BUFFERS[mine.choice] },
    });
    node.connect(ctx.destination);
    node.port.onmessage = (e) => mine.node === node && report(mine, e.data);
    const pipe = new MessageChannel();
    node.port.postMessage({ port: pipe.port2 }, [pipe.port2]);
    mine.worker.postMessage({ port: pipe.port1 }, [pipe.port1]);
    const was = mine.node;
    mine.node = node;
    was?.disconnect();
    return true;
}

/** setCallContext takes the phone's call's context once the call plays
    through it, and null as the call ends, before the context closes: a
    listening moves into it, and back into one of its own after */
export function setCallContext(ctx) {
    if (ctx === callContext) return;
    callContext = ctx;
    const mine = player;
    if (!mine) return;
    // The context it leaves closes however the move ends, where it was
    // its own; the call's isn't its to close.
    const was = mine.own ? mine.ctx : null;
    mine.ctx = ctx || ownContext();
    mine.own = !ctx;
    place(mine, mine.ctx)
        .then((placed) => placed && mine.own && mine.ctx.resume())
        .catch(() => player === mine && end(mine, 'player'))
        .finally(() => was?.close().catch(() => {}));
}

// report takes what the worklet counted every two seconds: playing, or,
// having played, waiting for more.
function report(mine, r) {
    if (player !== mine || !state.listening) return;
    mine.played ||= r.playing;
    const status = r.playing ? 'playing' : mine.played ? 'waiting' : 'connecting';
    if (status !== state.listening.status) set({ listening: { ...state.listening, status } });
}

// close stops a listening's worker, and its context where that's its own;
// a call's goes on.
function close(mine) {
    if (mine.worker) {
        mine.worker.postMessage({ stop: true });
        mine.worker.terminate();
    }
    if (mine.own) mine.ctx.close().catch(() => {});
    else mine.node?.disconnect();
}

function end(mine, why) {
    if (player !== mine) return;
    player = null;
    close(mine);
    set({ listening: null, notice: endedText(why, mine.name) });
}

/** stopListening ends this page's listening, as its Stop does */
export function stopListening() {
    const mine = player;
    if (!mine) return;
    player = null;
    close(mine);
    set({ listening: null });
}

/** clearNotice dismisses why the last listening ended */
export function clearNotice() {
    set({ notice: '' });
}

/** stopSharing stops this computer's sound going to the device that
    listens, as its Stop does */
export function stopSharing() {
    return api.post('/api/relay/stop', {});
}
