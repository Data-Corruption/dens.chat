// The settings' frame (M19.1), checked without a browser: the sections'
// list and names, openSettings reaching each, and the dialog listing them
// down its side with the one shown marked.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { SECTIONS, SettingsDialog, onOpenSettings, openSettings } from '../assets/js/src/settings.jsx';
import { keepTheme, ownChoice, setTheme } from '../assets/js/src/theme.js';
import { variables } from '../assets/js/src/themes.js';

// A wide window with a mouse: wider than the phone layout, and not coarse.
globalThis.window = { matchMedia: (q) => ({ matches: q.includes('min-width'), addEventListener() {}, removeEventListener() {} }), addEventListener() {}, removeEventListener() {} };

function all(node, match, found = []) {
    if (node.nodeType === 1 && match(node)) found.push(node);
    for (const child of node.childNodes || []) all(child, match, found);
    return found;
}

test('the sections are Appearance, Chat, Notifications, Voice and System, and openSettings opens each', () => {
    assert.deepEqual(SECTIONS.map(([, name]) => name), ['Appearance', 'Chat', 'Notifications', 'Voice', 'System']);
    const opened = [];
    const off = onOpenSettings((section) => opened.push(section));
    for (const [id] of SECTIONS) openSettings(id);
    openSettings();
    off();
    assert.deepEqual(opened, ['appearance', 'chat', 'notifications', 'voice', 'system', '']);
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
    assert.deepEqual(titles, ['App settings', 'Theme', 'Chat background', 'Messages', 'Animations', 'Verified mark'], 'Theme first, then the chat\'s background and how messages show');
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
