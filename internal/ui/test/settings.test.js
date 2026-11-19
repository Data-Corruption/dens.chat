// The settings' frame (M19.1), checked without a browser: the sections'
// list and names, openSettings reaching each, the dialog listing them
// down its side with the one shown marked, and Appearance taking the
// window (M22.8).

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { RelaySettings, SECTIONS, SettingsDialog, onOpenSettings, openSettings, relayState } from '../assets/js/src/settings.jsx';
import { keepTheme, ownChoice, setTheme } from '../assets/js/src/theme.js';
import { cleanTheme, variables } from '../assets/js/src/themes.js';

// A wide window with a mouse: wider than the phone layout, and not coarse.
globalThis.window = {
    matchMedia: (q) => ({ matches: q.includes('min-width'), addEventListener() {}, removeEventListener() {} }), addEventListener() {}, removeEventListener() {},
    history: { state: null, pushState() {}, replaceState() {}, go() {} }, location: { protocol: 'http:', host: 'localhost', pathname: '/', href: 'http://localhost/' },
};

function all(node, match, found = []) {
    if (node.nodeType === 1 && match(node)) found.push(node);
    for (const child of node.childNodes || []) all(child, match, found);
    return found;
}

test('the sections are Appearance, Chat, Notifications, Voice, Relay and System, and openSettings opens each', () => {
    assert.deepEqual(SECTIONS.map(([, name]) => name), ['Appearance', 'Chat', 'Notifications', 'Voice', 'Relay', 'System']);
    const opened = [];
    const off = onOpenSettings(({ section, denID }) => opened.push(denID ? `${section} of ${denID}` : section));
    for (const [id] of SECTIONS) openSettings(id);
    openSettings();
    openSettings('password', 'd1');
    off();
    assert.deepEqual(opened, ['appearance', 'chat', 'notifications', 'voice', 'relay', 'system', '', 'password of d1']);
});

test("a device's row says how its link stands, or what it offers", () => {
    assert.equal(relayState({ linked: true, apart: true, offers: ['keys'] }), 'Linked');
    assert.equal(relayState({ apart: true, offers: ['keys'] }), 'Not on this network');
    assert.equal(relayState({ offers: ['keys'] }), 'Keybind relay');
    assert.equal(relayState({ offers: ['keys', 'sound'] }), 'Keybind relay, Sharing its sound');
    assert.equal(relayState({ offers: ['sound'] }), 'Sharing its sound');
    assert.equal(relayState({ offers: [] }), 'Keybind relay off');
});

// relayPanel renders the Relay section as the service answers: the
// settings, with what the install relays, and the member's other devices,
// and whether the member refused the install its network (M20.3).
async function relayPanel(relay, devices, refused = false) {
    const saved = [];
    globalThis.fetch = async (path, init) => {
        if (init?.method === 'POST') saved.push(JSON.parse(init.body));
        const body = path === '/api/relay/devices' ? { devices, refused } : path === '/api/settings' ? { relay } : { ok: true };
        return { ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify(body) };
    };
    const root = document.createElement('div');
    mount(h(RelaySettings, {}), root);
    for (let i = 0; i < 20; i++) await new Promise((r) => setTimeout(r, 5));
    return {
        titles: all(root, (n) => n.localName === 'h2').map((t) => t.textContent.trim()),
        toggle: all(root, (n) => n.localName === 'input' && n.attributes.id === 'relay-keys')[0],
        sound: all(root, (n) => n.localName === 'input' && n.attributes.id === 'relay-sound')[0],
        buffer: all(root, (n) => n.localName === 'input' && n.attributes.name === 'relay-buffer').map((b) => b.attributes.value),
        rows: all(root, (n) => 'data-device' in n.attributes).map((li) => li.textContent.trim()),
        listen: all(root, (n) => 'data-listen' in n.attributes).map((b) => b.attributes['data-listen']),
        refused: all(root, (n) => 'data-relay-refused' in n.attributes).map((p) => p.textContent.trim()),
        text: root.textContent,
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
        assert.deepEqual(keys.titles, ['Keybind relay', 'Audio relay', 'Your devices']);
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
        assert.deepEqual(none.titles, ['Audio relay', 'Your devices'], 'no switch where it relays no keys');
        assert.ok(!none.toggle && !none.sound, 'nor where it shares no sound');
        assert.deepEqual(none.buffer, ['low', 'high'], 'and the buffer of what it plays');
    } finally {
        mount(null, none.root);
    }
});

test('the Relay section shares the sound where the install can, and has Listen beside each device that shares its own', async () => {
    const devices = [
        { id: 'a', name: 'Desk', offers: ['keys', 'sound'] },
        { id: 'b', name: 'Laptop', offers: ['keys'] },
    ];
    const panel = await relayPanel({ can: ['keys', 'sound'], keys: true, sound: false }, devices);
    try {
        assert.ok(panel.sound && !('checked' in panel.sound.attributes), 'Share this computer\'s sound, off');
        assert.deepEqual(panel.rows, ['DeskKeybind relay, Sharing its soundListen', 'LaptopKeybind relay']);
        assert.deepEqual(panel.listen, ['a'], 'Listen beside the desk alone');
        panel.sound.dispatch('change', { currentTarget: { checked: true } });
        for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 5));
        assert.deepEqual(panel.saved, [{ relaySound: true }], 'the switch goes to the service');
    } finally {
        mount(null, panel.root);
    }
});

test("on the phone, the Relay section's keys are the call's button, it shares nothing, and it says where the member refused it the network", async () => {
    const devices = [{ id: 'a', name: 'Desk', offers: ['keys', 'sound'] }];
    globalThis.document.getElementById = (id) => (id === 'app' ? { dataset: { inApp: 'true', keys: 'false' } } : null);
    try {
        const phone = await relayPanel({ can: ['keys'], keys: true }, devices);
        try {
            assert.deepEqual(phone.titles, ['Keybind relay', 'Audio relay', 'Your devices']);
            assert.ok(phone.text.includes("Relay the call's button"), 'the button in the keys\' place');
            assert.ok(!phone.sound, 'no sharing');
            assert.deepEqual(phone.listen, ['a'], 'Listen beside the desk');
            assert.deepEqual(phone.refused, [], 'nothing refused yet');
        } finally {
            mount(null, phone.root);
        }
        const refused = await relayPanel({ can: ['keys'], keys: true }, devices, true);
        try {
            assert.deepEqual(refused.refused, ["Android keeps Dens off your network. Allow it in Android's settings, under Apps."]);
        } finally {
            mount(null, refused.root);
        }
    } finally {
        delete globalThis.document.getElementById;
    }
});

test('the dialog lists the sections down its side, the one shown marked, and opens at Chat', () => {
    const root = document.createElement('div');
    const picked = [];
    mount(h(SettingsDialog, { section: '', onSection: (s) => picked.push(s), onClose() {}, instance: 'main', version: 'v0', pair: '' }), root);
    try {
        const nav = all(root, (n) => n.localName === 'nav' && n.attributes['aria-label'] === 'Sections')[0];
        assert.ok(nav, 'a list of the sections');
        const items = all(nav, (n) => n.localName === 'button');
        assert.deepEqual(items.map((b) => b.textContent.trim()), ['Appearance', 'Chat', 'Notifications', 'Voice', 'Relay', 'System']);
        assert.deepEqual(items.map((b) => b.attributes['aria-current'] || ''), ['', 'page', '', '', '', ''], 'Appearance takes the window, so Chat is first open');
        const titles = all(root, (n) => n.localName === 'h2' && /\bcard-title\b/.test(n.attributes.class || '')).map((t) => t.textContent.trim());
        assert.deepEqual(titles, ['Settings', 'Photos and videos', 'Links']);
        items[3].dispatch('click');
        assert.deepEqual(picked, ['voice']);
    } finally {
        mount(null, root);
    }
});

// What a theme's box computes to, here none of DaisyUI's variables, so a
// built-in theme reads as Light's or Dark's own.
globalThis.getComputedStyle = () => ({ colorScheme: 'light', getPropertyValue: () => '' });

test('Appearance takes the window, its list folded to ← back, which goes to the section open before, and ✕ closes', async () => {
    document.body = document.createElement('body');
    document.addEventListener ||= () => {};
    document.removeEventListener ||= () => {};
    const root = document.createElement('div');
    const picked = [];
    let closed = 0;
    const show = (section) => mount(h(SettingsDialog, { section, onSection: (s) => picked.push(s), onClose: () => closed++, instance: 'main', version: 'v0', pair: '' }), root);
    show('voice');
    try {
        all(root, (n) => n.localName === 'button' && n.attributes['data-section'] === 'appearance')[0].dispatch('click');
        assert.deepEqual(picked, ['appearance']);
        show('appearance');
        assert.equal(all(root, (n) => n.localName === 'nav' && n.attributes['aria-label'] === 'Sections').length, 0, 'no list beside it');
        assert.equal(all(root, (n) => n.localName === 'h2')[0].textContent.trim(), 'Appearance');
        assert.ok(all(root, (n) => 'data-themes' in n.attributes).length && all(root, (n) => 'data-theme-preview' in n.attributes).length,
            'the themes, and the preview');
        all(root, (n) => n.localName === 'button' && 'data-back' in n.attributes)[0].dispatch('click');
        assert.deepEqual(picked, ['appearance', 'voice'], 'back to Voice');
        all(root, (n) => n.localName === 'button' && n.attributes['aria-label'] === 'Close')[0].dispatch('click');
        assert.equal(closed, 1);
    } finally {
        mount(null, root);
    }
});

// Socket stands in for the event stream's socket, which the den's status
// starts, and which says nothing here.
class Socket {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = Socket.OPEN;
    addEventListener() {}
    send() {}
    close() {}
}

test("on a den's page, the member's own sections of it are a group above the app's, Leave den at the column's foot", async () => {
    globalThis.WebSocket = Socket;
    document.addEventListener ||= () => {};
    document.removeEventListener ||= () => {};
    const den = { den_id: 'd1', name: 'Voice Den', state: 'connected', role: 'member', own: false, username: 'bob' };
    globalThis.fetch = async (path) => {
        const body = path === '/api/dens' ? { dens: [den] } : path === '/api/dens/d1/state' ? { me: { id: 'm1' }, keeps_identities: true } : { ok: true };
        return { ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify(body) };
    };
    const root = document.createElement('div');
    mount(h(SettingsDialog, { section: 'code', denID: 'd1', onSection() {}, onClose() {}, navigate() {}, instance: 'main', version: 'v0', pair: '' }), root);
    try {
        for (let i = 0; i < 20; i++) await new Promise((r) => setTimeout(r, 5));
        const nav = all(root, (n) => n.localName === 'nav' && n.attributes['aria-label'] === 'Sections')[0];
        const read = all(nav, (n) => n.localName === 'p' || n.localName === 'button')
            .map((x) => (x.localName === 'p' ? `[${x.textContent.trim()}]` : x.textContent.trim() + (x.attributes['aria-current'] ? '*' : '')));
        assert.deepEqual(read, ['[Voice Den]', 'Devices', 'Password and recovery', 'Your code*', 'Your files', '[App]', 'Appearance', 'Chat', 'Notifications',
            'Voice', 'Relay', 'System', 'Leave den']);
        assert.equal(all(root, (n) => n.localName === 'h2' && n.textContent.trim().startsWith('Your code')).length, 1, 'with the section shown');
    } finally {
        mount(null, root);
    }
});

test("a theme of the member's own is listed under My themes in its colors, and wearing it sets its variables", () => {
    document.documentElement = document.createElement('html');
    const dusk = {
        name: 'Dusk', dark: true, corners: 'rounded', base: '#1d232a', text: '#a6adbb', primary: '#605dff', secondary: '#f43098', accent: '#00d3bb',
        neutral: '#09090b', info: '#00bafe', success: '#00d390', warning: '#fcb700', error: '#ff627d',
    };
    const render = () => {
        const root = document.createElement('div');
        mount(h(SettingsDialog, { section: 'appearance', onSection() {}, onClose() {}, instance: 'main', version: 'v0', pair: '' }), root);
        const own = all(root, (n) => n.localName === 'div' && n.attributes['aria-label'] === 'My themes')[0];
        const found = own && {
            names: all(own, (n) => n.localName === 'label').map((l) => l.textContent.trim()),
            worn: all(own, (n) => 'data-worn' in n.attributes).length,
            swatch: all(own, (n) => 'data-theme' in n.attributes)[0],
        };
        mount(null, root);
        return found;
    };
    assert.equal(render(), undefined, 'with none, no My themes');
    const id = keepTheme(dusk);
    const kept = render();
    assert.deepEqual([kept.names, kept.worn], [['Dusk'], 0], 'kept, not worn');
    assert.equal(kept.swatch.attributes['data-theme'], 'dark');
    assert.deepEqual({ ...kept.swatch.style }, variables(cleanTheme(dusk)), 'its swatch wears its variables');
    setTheme(ownChoice(id));
    assert.deepEqual([render().names, render().worn], [['DuskIn use'], 1], 'worn, marked so');
    assert.deepEqual({ ...document.documentElement.style }, variables(cleanTheme(dusk)), 'and the page wears it');
    setTheme('system');
});
