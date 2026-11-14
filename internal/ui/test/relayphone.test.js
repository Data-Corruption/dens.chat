// Relay in the Android app (M20.3), with stand-ins for the browser and the
// service: a listening plays through the phone's call's context while the
// call lasts and through one of its own around it, the worker feeding
// whichever node plays, and its own opened for playback, not as a call's;
// and the call's button, for a call another of the member's devices holds,
// shows under that device's name and sends its presses there.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';

const app = { dataset: { autoplay: 'true', inApp: 'true', keys: 'false', relayWorker: '/relay.worker.js', relayWorklet: '/relay.worklet.js' } };
globalThis.document.getElementById = (id) => (id === 'app' ? app : null);
globalThis.document.addEventListener = () => {};
globalThis.document.removeEventListener = () => {};
globalThis.document.hidden = false;
globalThis.location = { protocol: 'http:', host: 'localhost:8484' };
globalThis.window = {
    matchMedia: (q) => ({ matches: q.includes('pointer: coarse'), addEventListener() {}, removeEventListener() {} }),
    addEventListener() {},
    removeEventListener() {},
    location: globalThis.location,
};
const kept = new Map();
globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, v), removeItem: (k) => kept.delete(k) };
globalThis.fetch = async () => ({ ok: false, status: 404, headers: { get: () => null }, text: async () => '' });

// What the page told the service on its event socket, and the socket, to
// deliver the service's word.
const sent = [];
const sockets = [];
class Socket {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = Socket.OPEN;
    listeners = {};
    constructor(url) {
        this.url = url;
        sockets.push(this);
    }
    addEventListener(type, fn) {
        (this.listeners[type] ||= []).push(fn);
    }
    send(text) {
        sent.push(JSON.parse(text));
    }
    close() {}
    receive(msg) {
        for (const fn of this.listeners.message || []) fn({ data: JSON.stringify(msg) });
    }
}
globalThis.WebSocket = Socket;

// Audio's stand-ins: each context, as it was opened, and its nodes.
const contexts = [];
class Context {
    constructor(opts = {}) {
        this.opts = opts;
        this.state = 'running';
        this.nodes = [];
        this.destination = {};
        this.audioWorklet = { addModule: async () => {} };
        contexts.push(this);
    }
    resume() {
        return Promise.resolve();
    }
    close() {
        this.state = 'closed';
        return Promise.resolve();
    }
}
globalThis.AudioContext = Context;
globalThis.AudioWorkletNode = class {
    constructor(ctx, name, opts) {
        this.ctx = ctx;
        this.name = name;
        this.opts = opts;
        this.port = { postMessage() {} };
        ctx.nodes.push(this);
    }
    connect() {}
    disconnect() {
        this.disconnected = true;
    }
};
let worker = null;
globalThis.Worker = class {
    constructor(url) {
        this.url = url;
        this.posted = [];
        worker = this;
    }
    postMessage(m) {
        this.posted.push(Object.keys(m).sort().join('+'));
    }
    terminate() {
        this.terminated = true;
    }
};
globalThis.MessageChannel = class {
    port1 = {};
    port2 = {};
};

const { listen, onRelay, setCallContext, stopListening } = await import('../assets/js/src/relay.js');
const { CallButton } = await import('../assets/js/src/voice.jsx');

// Without requestAnimationFrame, Preact runs effects 100 ms after a render.
const settle = () => new Promise((r) => setTimeout(r, 150));

test("a listening plays through the phone's call's context while the call lasts, and through its own around it", async () => {
    contexts.length = 0;
    await listen({ id: 'd1', name: 'Desk' }, app.dataset);
    const own = contexts[0];
    assert.equal(own.opts.latencyHint, 'playback', "its own context is opened for playback, never as a call's");
    assert.equal(own.nodes.length, 1);
    assert.deepEqual(worker.posted, ['port', 'url'], 'the worker feeds the node, then opens the socket');

    const call = new Context();
    setCallContext(call);
    await settle();
    assert.equal(call.nodes.length, 1, "a node in the call's context");
    assert.equal(own.state, 'closed', 'its own context closed');
    assert.ok(own.nodes[0].disconnected);
    assert.deepEqual(worker.posted.slice(2), ['port'], 'the worker feeds the call\'s node');

    setCallContext(null);
    await settle();
    const after = contexts.at(-1);
    assert.notEqual(after, call);
    assert.equal(after.opts.latencyHint, 'playback');
    assert.equal(after.nodes.length, 1, 'a node in a context of its own again');
    assert.ok(call.nodes[0].disconnected, "the call's node let go");
    assert.equal(call.state, 'running', "the call's context isn't the player's to close");
    assert.deepEqual(worker.posted.slice(3), ['port']);

    stopListening();
    assert.equal(after.state, 'closed');
    assert.ok(worker.terminated);
});

test("a listening that starts during the phone's call plays in the call's context, and leaves it as it stops", async () => {
    const call = new Context();
    setCallContext(call);
    contexts.length = 0;
    await listen({ id: 'd1', name: 'Desk' }, app.dataset);
    assert.equal(contexts.length, 0, 'no context of its own');
    assert.equal(call.nodes.length, 1);
    stopListening();
    assert.equal(call.state, 'running');
    assert.ok(call.nodes[0].disconnected);
    setCallContext(null);
});

test("the call's button, for a call another of the member's devices holds, shows under its name, and sends its presses there", async () => {
    const root = document.createElement('div');
    mount(h(CallButton, {}), root);
    await settle();
    const button = () => root.find((n) => 'data-call-button' in n.attributes);
    assert.equal(button(), null, 'none while no device holds a call');

    let seen = null;
    const off = onRelay((s) => (seen = s), true);
    await settle();
    sockets.at(-1).receive({ t: 'relay', d: { sharing: null, call: { id: 'd1', name: 'Desk', muted: false } } });
    await settle();
    assert.deepEqual(seen.call, { id: 'd1', name: 'Desk', muted: false });
    const b = button();
    assert.ok(b && 'data-relayed' in b.attributes, 'the button, for the call elsewhere');
    assert.equal(b.attributes['aria-label'], 'Mute, on Desk');
    assert.equal(root.find((n) => 'data-relayed-to' in n.attributes)?.textContent, 'Desk');

    sent.length = 0;
    const press = { button: 0, isPrimary: true, pointerId: 1, currentTarget: {}, preventDefault() {} };
    b.dispatch('pointerdown', press);
    b.dispatch('pointerup', press);
    assert.deepEqual(sent, [{ t: 'relay.key', d: { action: 'toggleMute', down: true } }, { t: 'relay.key', d: { action: 'toggleMute', down: false } }]);

    sockets.at(-1).receive({ t: 'relay', d: { sharing: null, call: { id: 'd1', name: 'Desk', muted: true } } });
    await settle();
    assert.equal(button().attributes['aria-label'], 'Unmute, on Desk', 'muted there');
    sockets.at(-1).receive({ t: 'relay', d: { sharing: null } });
    await settle();
    assert.equal(button(), null, 'gone with the call');
    off();
    mount(null, root);
});
