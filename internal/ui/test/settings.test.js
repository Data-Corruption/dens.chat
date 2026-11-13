// The settings' frame (M19.1), checked without a browser: the sections'
// list and names, openSettings reaching each, and the dialog listing them
// down its side with the one shown marked.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { RelaySettings, SECTIONS, SettingsDialog, onOpenSettings, openSettings, relayState, shownSections } from '../assets/js/src/settings.jsx';
import { keepTheme, ownChoice, setTheme } from '../assets/js/src/theme.js';
import { variables } from '../assets/js/src/themes.js';

// A wide window with a mouse: wider than the phone layout, and not coarse.
globalThis.window = { matchMedia: (q) => ({ matches: q.includes('min-width'), addEventListener() {}, removeEventListener() {} }), addEventListener() {}, removeEventListener() {} };

function all(node, match, found = []) {
    if (node.nodeType === 1 && match(node)) found.push(node);
    for (const child of node.childNodes || []) all(child, match, found);
    return found;
}

test('the sections are Appearance, Chat, Notifications, Voice, Relay and System, and openSettings opens each', () => {
    assert.deepEqual(SECTIONS.map(([, name]) => name), ['Appearance', 'Chat', 'Notifications', 'Voice', 'Relay', 'System']);
    const opened = [];
    const off = onOpenSettings((section) => opened.push(section));
    for (const [id] of SECTIONS) openSettings(id);
    openSettings();
    off();
    assert.deepEqual(opened, ['appearance', 'chat', 'notifications', 'voice', 'relay', 'system', '']);
});

test('Relay shows where the install relays, after Voice', () => {
    assert.deepEqual(shownSections(true).map(([id]) => id), ['appearance', 'chat', 'notifications', 'voice', 'relay', 'system']);
    assert.deepEqual(shownSections(false).map(([id]) => id), ['appearance', 'chat', 'notifications', 'voice', 'system']);
    assert.deepEqual(shownSections().map(([id]) => id), ['appearance', 'chat', 'notifications', 'voice', 'system'], 'a page the service gave no Relay');
});

test("a device's row says how its link stands, or what it offers", () => {
    assert.equal(relayState({ linked: true, apart: true, offers: ['keys'] }), 'Linked');
    assert.equal(relayState({ apart: true, offers: ['keys'] }), 'Not on this network');
    assert.equal(relayState({ offers: ['keys', 'sound'] }), 'Keybind relay');
    assert.equal(relayState({ offers: [] }), 'Keybind relay off');
});

// relayPanel renders the Relay section as the service answers: the
// settings, with what the install relays, and the member's other devices.
async function relayPanel(relay, devices) {
    const saved = [];
    globalThis.fetch = async (path, init) => {
        if (init?.method === 'POST') saved.push(JSON.parse(init.body));
        const body = path === '/api/relay/devices' ? { devices } : path === '/api/settings' ? { relay } : { ok: true };
        return { ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify(body) };
    };
    const root = document.createElement('div');
    mount(h(RelaySettings, {}), root);
    for (let i = 0; i < 20; i++) await new Promise((r) => setTimeout(r, 5));
    return {
        titles: all(root, (n) => n.localName === 'h2').map((t) => t.textContent.trim()),
        toggle: all(root, (n) => n.localName === 'input' && n.attributes.id === 'relay-keys')[0],
        rows: all(root, (n) => 'data-device' in n.attributes).map((li) => li.textContent.trim()),
        saved,
        root,
    };
}

test('the Relay section shows Keybind relay where the install relays keys, and the devices that answered', async () => {
    const devices = [
        { id: 'a', name: 'Laptop', offers: ['keys'], linked: true },
        { id: 'b', name: 'Phone', offers: [] },
        { id: 'c', name: 'VM', offers: ['keys'], apart: true },
    ];
    const keys = await relayPanel({ can: ['keys'], keys: false }, devices);
    try {
        assert.deepEqual(keys.titles, ['Keybind relay', 'Your devices']);
        assert.ok(keys.toggle && !('checked' in keys.toggle.attributes), 'its switch, off');
        assert.deepEqual(keys.rows, ['LaptopLinked', 'PhoneKeybind relay off', 'VMNot on this network']);
        keys.toggle.dispatch('change', { currentTarget: { checked: true } });
        for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 5));
        assert.deepEqual(keys.saved, [{ relayKeys: true }], 'the switch goes to the service');
    } finally {
        mount(null, keys.root);
    }
    const none = await relayPanel({ can: [], keys: false }, []);
    try {
        assert.deepEqual(none.titles, ['Your devices'], 'no switch where it relays no keys');
        assert.ok(!none.toggle);
    } finally {
        mount(null, none.root);
    }
});

test('the dialog lists the sections down its side, the one shown marked, and opens at Appearance', () => {
    const root = document.createElement('div');
    const picked = [];
    mount(h(SettingsDialog, { section: '', onSection: (s) => picked.push(s), onClose() {}, instance: 'main', version: 'v0', pair: '' }), root);
    const nav = all(root, (n) => n.localName === 'nav' && n.attributes['aria-label'] === 'Sections')[0];
    assert.ok(nav, 'a list of the sections');
    const items = all(nav, (n) => n.localName === 'button');
    assert.deepEqual(items.map((b) => b.textContent.trim()), ['Appearance', 'Chat', 'Notifications', 'Voice', 'System']);
    assert.deepEqual(items.map((b) => b.attributes['aria-current'] || ''), ['page', '', '', '', '']);
    const titles = all(root, (n) => n.localName === 'h2' && /\bcard-title\b/.test(n.attributes.class || '')).map((t) => t.textContent.trim());
    assert.deepEqual(titles, ['App settings', 'Theme', 'Chat background', 'Messages', 'Name styles', 'Animations', 'Verified mark'],
        'Theme first, then the chat\'s background and how messages and names show');
    items[3].dispatch('click');
    assert.deepEqual(picked, ['voice']);
    mount(null, root);
});

test("a theme of the member's own is listed under Your themes in its colors, with its buttons once chosen", () => {
    document.documentElement = document.createElement('html');
    const dusk = {
        name: 'Dusk', dark: true, corners: 'rounded', base: '#1d232a', text: '#a6adbb', primary: '#605dff', secondary: '#f43098', accent: '#00d3bb',
        neutral: '#09090b', info: '#00bafe', success: '#00d390', warning: '#fcb700', error: '#ff627d',
    };
    const render = () => {
        const root = document.createElement('div');
        mount(h(SettingsDialog, { section: '', onSection() {}, onClose() {}, instance: 'main', version: 'v0', pair: '' }), root);
        const own = all(root, (n) => 'data-own-themes' in n.attributes)[0];
        const found = {
            names: all(own, (n) => n.localName === 'label').map((l) => l.textContent.trim()),
            checked: all(own, (n) => n.localName === 'input' && n.attributes.type === 'radio').map((i) => 'checked' in i.attributes),
            swatch: all(own, (n) => 'data-theme' in n.attributes)[0],
            buttons: all(own, (n) => n.localName === 'button').map((b) => b.textContent.trim()),
        };
        mount(null, root);
        return found;
    };
    assert.deepEqual(render().buttons, ['New theme', 'Paste a code'], 'with none, a member makes one or pastes one');
    const id = keepTheme(dusk);
    const kept = render();
    assert.deepEqual([kept.names, kept.checked, kept.buttons], [['Dusk'], [false], ['New theme', 'Paste a code']], 'kept, not chosen');
    assert.equal(kept.swatch.attributes['data-theme'], 'dark');
    assert.deepEqual({ ...kept.swatch.style }, variables(dusk), 'its swatch wears its variables');
    setTheme(ownChoice(id));
    const chosen = render();
    assert.deepEqual([chosen.checked, chosen.buttons], [[true], ['Edit', 'Copy code', 'Delete', 'New theme', 'Paste a code']]);
    assert.deepEqual({ ...document.documentElement.style }, variables(dusk), 'and the page wears it');
    setTheme('system');
});
