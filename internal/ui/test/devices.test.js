import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { YourDen, labelParts } from '../src/yourden.jsx';
import labelCases from '../../denclient/testdata/labels.json';

// A device's label shows as its name, with what's in the parentheses at its
// end, a computer's system or a phone's model, under it, as Dens makes
// them; the Go tests read the same cases.
test('a device shows by its name, with what it is under it', () => {
    for (const c of labelCases.cases) {
        assert.deepEqual(labelParts(c.label), { name: c.name, detail: c.detail }, c.label);
    }
    assert.deepEqual(labelParts(''), { name: '', detail: '' });
    assert.deepEqual(labelParts(undefined), { name: '', detail: '' });
});

// A window for the page's event stream, whose socket is held here: Node's
// own would reconnect for ever.
globalThis.window = {
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }), addEventListener() {}, removeEventListener() {},
    location: { protocol: 'http:', host: 'localhost', pathname: '/', href: 'http://localhost/' },
};
document.addEventListener ??= () => {};
document.removeEventListener ??= () => {};
const sockets = [];
globalThis.WebSocket = class {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = 1;
    listeners = {};
    constructor() {
        sockets.push(this);
    }
    addEventListener(type, fn) {
        (this.listeners[type] ||= []).push(fn);
    }
    send() {}
    receive(msg) {
        for (const fn of this.listeners.message || []) fn({ data: JSON.stringify(msg) });
    }
};

const findAll = (node, match, out = []) => {
    if (match(node)) out.push(node);
    for (const child of node.childNodes || []) findAll(child, match, out);
    return out;
};
const buttonNamed = (text) => (n) => n.localName === 'button' && n.textContent.trim() === text;
const settle = () => new Promise((resolve) => setTimeout(resolve, 5));

// Rename shows on this device's row alone, where the den keeps device
// names, and names the install in the settings, what it is staying.
test("this device's row has Rename where the den keeps device names, which names the install", async () => {
    const asked = [];
    const devices = [
        { key_id: 'a', label: 'Pearl (Windows)', created_at: 1, last_seen_at: 1, current: true },
        { key_id: 'b', label: 'Galaxy S24 Ultra (SM-S928U1)', created_at: 1, last_seen_at: 1 },
    ];
    globalThis.fetch = async (path, opts = {}) => {
        asked.push({ method: opts.method || 'GET', path, body: opts.body ? JSON.parse(opts.body) : undefined });
        const body = path === '/api/dens/d/devices' ? { devices, recovery_codes_left: 9 } : { ok: true };
        return { ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify(body) };
    };
    const root = document.createElement('div');
    const renames = () => findAll(root, buttonNamed('Rename'));
    const den = { den_id: 'd', state: 'connected' };
    mount(h(YourDen, { den, view: { device_names: false }, at: 'devices', onClose() {}, navigate() {} }), root);
    for (let i = 0; i < 50 && !findAll(root, buttonNamed('Sign out')).length; i++) await settle();
    assert.equal(renames().length, 0, 'none on a den that keeps the label a device signed in with');

    mount(null, root);
    mount(h(YourDen, { den, view: { device_names: true }, at: 'devices', onClose() {}, navigate() {} }), root);
    for (let i = 0; i < 50 && !renames().length; i++) await settle();
    assert.equal(renames().length, 1, "on this device's row alone");
    renames()[0].dispatch('click');
    await settle();
    const input = findAll(root, (n) => n.attributes?.['data-device-name'] !== undefined)[0];
    assert.equal(input.attributes.value, 'Pearl', 'its name, without what it is');
    input.value = 'Bob’s desk';
    input.dispatch('input');
    await settle();
    findAll(root, (n) => n.localName === 'form' && n.listeners.Submit)[0].dispatch('Submit', { preventDefault() {} });
    for (let i = 0; i < 50 && !asked.some((a) => a.method === 'POST'); i++) await settle();
    assert.deepEqual(asked.find((a) => a.method === 'POST'), { method: 'POST', path: '/api/settings', body: { deviceName: 'Bob’s desk' } });

    // The den tells this device's sessions, and the list shows the label.
    devices[0] = { ...devices[0], label: 'Bob’s desk (Windows)' };
    for (const s of sockets) s.receive({ t: 'den', d: { den: 'd', events: [{ t: 'device.updated', d: { key_id: 'a', label: 'Bob’s desk (Windows)' } }] } });
    const named = () => findAll(root, (n) => n.localName === 'span' && n.textContent === 'Bob’s desk').length;
    for (let i = 0; i < 50 && !named(); i++) await settle();
    assert.ok(named(), 'the row reads the new name');
    mount(null, root);
});
