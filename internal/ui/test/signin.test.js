// Signing in on a new device with the password: the dialog's steps while
// one of the member's other devices approves it.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { useState } from 'preact/hooks';
import { Home } from '../assets/js/src/home.jsx';
import { Approvals, HiddenRequests } from '../assets/js/src/private.jsx';
import made from '../../denclient/testdata/qrcodes.json';

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
    await until(() => /Den password/.test(root.textContent), 'the sign-in form');
    assert.ok(find(root, (n) => 'data-den-address' in (n.attributes || {}) && n.textContent === 'https://den.example.com'), "the den's address under its title");
    const [username, password] = findAll(root, byTag('input'));
    type(username, 'bob');
    type(password, 'correct horse battery');
    await settle();
    submit(find(root, byTag('form')));
    await until(() => /Waiting for your other device/.test(root.textContent), 'the waiting step');
    assert.match(root.textContent, /Open Dens on one of your other devices/);
    assert.ok(find(root, (n) => /\bloading-spinner\b/.test(n.attributes?.class || '')), 'a spinner beside it');
    // The dialog shows it, so the cards behind it don't.
    assert.equal(findAll(root, (n) => /Waiting for your other device/.test(n.data || '')).length, 1);

    const dens = (version, signIns) => stream.fire('message', { data: JSON.stringify({ t: 'dens', d: { ...VIEW, version, sign_ins: signIns } }) });
    dens(2, [{ ...pending, stage: 'check', half: '1234 5678' }]);
    await until(() => /Type these digits into your other device/.test(root.textContent), 'the next step, from the list');
    assert.match(root.textContent, /1234 5678/);
    dens(3, []);
    await until(() => !/Type these digits/.test(root.textContent), 'the steps ended');
    assert.doesNotMatch(root.textContent, /Waiting for your other device/);
    mount(null, root);
});

// openAddDen mounts Home and opens Add a den, whose ways in it returns by
// what they're for.
async function openAddDen(answers) {
    service({ 'GET /api/dens': VIEW, ...answers });
    const root = document.createElement('div');
    mount(h(Home, { status: {}, navigate() {} }), root);
    await until(() => find(root, button('Add a den')), 'the list of dens');
    click(find(root, button('Add a den')));
    await settle();
    const way = (at) => find(root, (n) => n.localName === 'form' && n.attributes?.['data-way'] === at);
    return { root, way };
}

// A den's address takes its https:// or leaves it off, and signs in at
// the address with it.
test("Add a den's address signs in with or without its scheme", async () => {
    for (const typed of ['den.example.com', 'https://den.example.com', 'https://den.example.com/', 'Den.Example.com']) {
        let asked = null;
        const { root, way } = await openAddDen({});
        globalThis.fetch = ((inner) => async (path, opts = {}) => {
            if (path === '/api/dens/signin') asked = JSON.parse(opts.body);
            return inner(path, opts);
        })(globalThis.fetch);
        assert.ok(way('address') && way('invite'), 'the address and the invite each a way in');
        type(find(way('address'), byTag('input')), typed);
        await settle();
        submit(way('address'));
        await until(() => /Den password/.test(root.textContent), `the sign-in for ${typed}`);
        const [username, password] = findAll(root, byTag('input'));
        type(username, 'bob');
        type(password, 'correct horse battery');
        await settle();
        submit(find(root, byTag('form')));
        await until(() => asked, 'the sign-in asked');
        assert.match(asked.den, /^https:\/\/den\.example\.com\/?$/i, typed);
        mount(null, root);
    }
    const { root, way } = await openAddDen({});
    type(find(way('address'), byTag('input')), 'not an address');
    await settle();
    submit(way('address'));
    await until(() => /isn't a den's address/.test(root.textContent), 'the address refused');
    type(find(way('invite'), byTag('input')), 'https://den.example.com/invite');
    await settle();
    submit(way('invite'));
    await until(() => /isn't an invite/.test(root.textContent), 'the invite refused');
    mount(null, root);
});

// Scan shows only where the browser has a camera, as the Android
// emulator and a computer without one have none.
test('Add a den offers Scan only with a camera', async () => {
    const before = globalThis.navigator;
    try {
        for (const [devices, shown] of [[[{ kind: 'audioinput' }], false], [[{ kind: 'audioinput' }, { kind: 'videoinput' }], true]]) {
            Object.defineProperty(globalThis, 'navigator', {
                configurable: true,
                value: { userAgent: 'test', mediaDevices: { getUserMedia: async () => ({}), enumerateDevices: async () => devices } },
            });
            const { root, way } = await openAddDen({});
            if (shown) await until(() => find(root, button('Scan a code')), 'Scan');
            for (let i = 0; i < 10; i++) await settle();
            assert.equal(!!find(root, button('Scan a code')), shown, `cameras ${devices.map((d) => d.kind)}`);
            assert.ok(way('address'), 'the address beside it');
            mount(null, root);
        }
    } finally {
        Object.defineProperty(globalThis, 'navigator', { configurable: true, value: before });
    }
});

// A request opens as a dialog over the page: It's mine, then this
// device's digits under their code beside a box for the new device's,
// then Approved with them still shown, until the new device connects and
// the request leaves the list. One closed before then waits in a line
// that opens it again.
test('a sign-in to approve opens as a dialog, and closes once its device connects', async () => {
    const posted = [];
    const den = { den_id: 'd1', name: 'Voice Den', state: 'connected', requests: [] };
    const request = { id: 'r1', label: 'Laptop (Linux)', requested_at: 1, expires_at: Date.now() + 600000 };
    const answers = { 'GET /api/dens': { ...VIEW, dens: [den] }, 'GET /api/dens/d1/requests/r1/code': made.codes[2] };
    globalThis.fetch = async (path, opts = {}) => {
        const method = opts.method || 'GET';
        if (method !== 'GET') posted.push([method, path, opts.body ? JSON.parse(opts.body) : null]);
        const answer = answers[`${method} ${path}`] ?? (method === 'GET' ? undefined : {});
        const status = answer === undefined ? 404 : 200;
        return { ok: status < 400, status, headers: { get: () => null }, text: async () => JSON.stringify(answer ?? { error: 'no such thing' }) };
    };
    const root = document.createElement('div');
    let version = 1;
    const list = (requests) => ({ ...VIEW, version: ++version, dens: [{ ...den, requests }] });
    function Page() {
        const [view, setView] = useState(list([]));
        Page.set = (requests) => setView(list(requests));
        return h('div', null, h(HiddenRequests, { den: view.dens[0] }), h(Approvals));
    }
    mount(h(Page), root);
    await until(() => sockets.length, 'the event stream');
    const stream = sockets.at(-1);
    stream.fire('open');
    const show = (requests) => {
        const v = list(requests);
        stream.fire('message', { data: JSON.stringify({ t: 'dens', d: v }) });
        Page.set(requests);
    };
    const dialog = () => find(root, (n) => n.attributes?.role === 'dialog');

    show([request]);
    await until(() => dialog() && /Laptop \(Linux\)/.test(dialog().textContent), 'the dialog');
    assert.match(dialog().textContent, /Voice Den/);
    click(find(dialog(), button("It's mine")));
    await until(() => posted.some(([m, p]) => m === 'POST' && p === '/api/dens/d1/requests/r1/answer'), 'the answer');

    show([{ ...request, here: true, half: '9876 5432 1098 7654' }]);
    await until(() => find(root, (n) => 'data-qr' in (n.attributes || {})), "the digits' code");
    assert.match(dialog().textContent, /9876 5432 1098 7654/);
    type(find(dialog(), byTag('input')), '1234 5678 9012 3456');
    await settle();
    submit(find(dialog(), byTag('form')));
    await until(() => posted.some(([, p, body]) => p === '/api/dens/d1/requests/r1/approve' && body.digits === '1234 5678 9012 3456'), 'the approval');

    show([{ ...request, approved: true, half: '9876 5432 1098 7654' }]);
    await until(() => /Approved/.test(dialog()?.textContent || ''), 'approved');
    assert.ok(find(dialog(), (n) => 'data-qr' in (n.attributes || {})), 'its code still shown');
    // The new device connects, and the service drops the request.
    show([]);
    await until(() => !dialog(), 'the dialog closed by itself');

    // Closed before it's approved, it waits in a line, which opens it.
    show([{ ...request, id: 'r2' }]);
    await until(() => dialog(), 'a second request');
    click(find(dialog(), (n) => n.attributes?.['aria-label'] === 'Close'));
    await until(() => !dialog() && find(root, button('Show')), 'the line to open it again');
    click(find(root, button('Show')));
    await until(() => dialog(), 'the dialog again');
    mount(null, root);
});
