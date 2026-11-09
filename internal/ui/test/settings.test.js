// The settings' frame (M19.1), checked without a browser: the sections'
// list and names, openSettings reaching each, and the dialog listing them
// down its side with the one shown marked.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { SECTIONS, SettingsDialog, onOpenSettings, openSettings } from '../assets/js/src/settings.jsx';

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
    assert.deepEqual(titles, ['App settings', 'Theme', 'Messages', 'Animations', 'Verified mark'], 'Theme first, then how messages show');
    items[3].dispatch('click');
    assert.deepEqual(picked, ['voice']);
    mount(null, root);
});
