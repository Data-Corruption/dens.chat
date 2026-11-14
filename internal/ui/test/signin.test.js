// Signing in on a new device with the password: the dialog's steps while
// one of the member's other devices approves it.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { Home } from '../assets/js/src/home.jsx';

globalThis.window = {
    location: { protocol: 'http:', host: 'localhost:1', pathname: '/', hash: '' },
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    addEventListener() {},
    removeEventListener() {},
    history: { state: null, pushState() {}, replaceState() {}, go() {} },
};
document.addEventListener = () => {};

// The event stream's socket, open at once, through which the test sends
// the service's lists.
const sockets = [];
class Socket {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = Socket.OPEN;
    listeners = {};
    constructor() {
        sockets.push(this);
    }
    addEventListener(type, fn) {
        (this.listeners[type] ||= []).push(fn);
    }
    send() {}
    close() {}
    fire(type, event = {}) {
        for (const fn of this.listeners[type] || []) fn(event);
    }
}
globalThis.WebSocket = Socket;

// service answers the page's requests as the local service would, by
// method and path.
function service(answers) {
    globalThis.fetch = async (path, opts = {}) => {
        const answer = answers[`${opts.method || 'GET'} ${path}`];
        const status = answer === undefined ? 404 : 200;
        return { ok: status < 400, status, headers: { get: () => null }, text: async () => JSON.stringify(answer ?? { error: 'no such thing' }) };
    };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 5));

async function until(cond, what) {
    for (let i = 0; i < 100; i++) {
        if (cond()) return;
        await settle();
    }
    assert.fail(`timed out waiting for ${what}`);
}

function findAll(node, match, found = []) {
    if (match(node)) found.push(node);
    for (const child of node.childNodes || []) findAll(child, match, found);
    return found;
}

const find = (node, match) => findAll(node, match)[0] || null;
const button = (text) => (n) => n.localName === 'button' && n.textContent.trim() === text;
const byTag = (tag) => (n) => n.localName === tag;

function click(node) {
    node.dispatch('click', { target: node });
}

function type(input, value) {
    input.value = value;
    input.dispatch('input', { target: input });
}

function submit(form) {
    form.dispatch(form.listeners.submit ? 'submit' : 'Submit', { target: form, preventDefault() {} });
}

const VIEW = { epoch: 'e', version: 1, dens: [], sign_ins: [], hosting: { can_host: false, enabled: false } };

// The service answers the sign-in with its waiting step, which the dialog
// shows at once, before any newer list of sign-ins comes; from then on the
// list leads, and the sign-in leaving it ends the steps.
test("a sign-in waiting for another device says so from the request's answer", async () => {
    const pending = { id: '1', url: 'https://den.example.com', username: 'bob', stage: 'waiting', checked: false, expires_at: Date.now() + 600000 };
    service({
        'GET /api/dens': VIEW,
        'POST /api/dens/signin': { den: {}, recovery_codes_left: 0, signed_out: 0, pending },
    });
    const root = document.createElement('div');
    mount(h(Home, { status: {}, navigate() {} }), root);
    await until(() => sockets.length, 'the event stream');
    const stream = sockets.at(-1);
    stream.fire('open');
    await until(() => find(root, button('Add a den')), 'the list of dens');
    click(find(root, button('Add a den')));
    await settle();
    type(find(root, byTag('input')), 'https://den.example.com');
    await settle();
    submit(find(root, byTag('form')));
    await until(() => /Sign in to den\.example\.com/.test(root.textContent), 'the sign-in form');
    const [username, password] = findAll(root, byTag('input'));
    type(username, 'bob');
    type(password, 'correct horse battery');
    await settle();
    submit(find(root, byTag('form')));
    await until(() => /Waiting for your other device/.test(root.textContent), 'the waiting step');
    assert.match(root.textContent, /Approve this on another device/);
    assert.ok(find(root, (n) => /\bloading-spinner\b/.test(n.attributes?.class || '')), 'a spinner beside it');
    // The dialog shows it, so the cards behind it don't.
    assert.equal(findAll(root, (n) => /Waiting for your other device/.test(n.data || '')).length, 1);

    const dens = (version, signIns) => stream.fire('message', { data: JSON.stringify({ t: 'dens', d: { ...VIEW, version, sign_ins: signIns } }) });
    dens(2, [{ ...pending, stage: 'check', half: '1234 5678' }]);
    await until(() => /Compare digits with your other device/.test(root.textContent), 'the next step, from the list');
    assert.match(root.textContent, /1234 5678/);
    dens(3, []);
    await until(() => !/Compare digits/.test(root.textContent), 'the steps ended');
    assert.doesNotMatch(root.textContent, /Waiting for your other device/);
    mount(null, root);
});
