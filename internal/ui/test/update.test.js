// An update's notice in a den, which the member may dismiss until the next
// update, and on Home, which shows it whatever they dismissed.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { UpdateNotice } from '../assets/js/src/update.jsx';

const kept = new Map();
globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, String(v)) };

const settle = () => new Promise((resolve) => setTimeout(resolve, 5));

function find(node, match) {
    if (match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}

const button = (text) => (n) => n.localName === 'button' && n.textContent.trim() === text;

// Each form's update, and the next one after it.
const FORMS = {
    installed: [
        { version: 'v1.12.0', updateVersion: 'v1.13.0', updateCommand: 'sudo dens update' },
        { version: 'v1.12.0', updateVersion: 'v1.13.1', updateCommand: 'sudo dens update' },
    ],
    android: [{ version: 'v1.12.0', updateReady: 'v1.13.0' }, { version: 'v1.12.0', updateReady: 'v1.13.1' }],
    'flatpak, installed': [
        { version: 'v1.12.0', flatpakUpdate: { installed: true, version: 'v1.13.0' } },
        { version: 'v1.12.0', flatpakUpdate: { installed: true, version: 'v1.13.1' } },
    ],
    // An update Flatpak finds says no version: the next is the one found
    // once Dens runs the last.
    'flatpak, available': [
        { version: 'v1.12.0', flatpakUpdate: { available: true, can_install: true } },
        { version: 'v1.13.0', flatpakUpdate: { available: true, can_install: true } },
    ],
    'flatpak, software center': [
        { version: 'v1.12.0', flatpakUpdate: { available: true } },
        { version: 'v1.13.0', flatpakUpdate: { available: true } },
    ],
};

test("a den's update notice stays dismissed until the next update, and Home's shows whatever was dismissed", async () => {
    for (const [form, [update, next]] of Object.entries(FORMS)) {
        kept.clear();
        const root = document.createElement('div');
        mount(h(UpdateNotice, { status: update, dismissible: true }), root);
        assert.ok(find(root, (n) => n.attributes?.['data-update-notice'] !== undefined), `${form}: the den shows it`);
        const dismiss = find(root, button('Dismiss'));
        dismiss.dispatch('click', { target: dismiss });
        await settle();
        assert.equal(root.textContent, '', `${form}: gone once dismissed`);
        // Another page, or the den opened again, keeps it dismissed.
        mount(null, root);
        mount(h(UpdateNotice, { status: update, dismissible: true }), root);
        assert.equal(root.textContent, '', `${form}: still dismissed`);
        mount(h(UpdateNotice, { status: update }), root);
        assert.ok(root.textContent.length > 0, `${form}: Home shows it all the same`);
        assert.equal(find(root, button('Dismiss')), null, `${form}: with nothing to dismiss there`);
        mount(null, root);
        mount(h(UpdateNotice, { status: next, dismissible: true }), root);
        assert.ok(find(root, button('Dismiss')), `${form}: the next update shows again`);
        mount(null, root);
    }
    const none = document.createElement('div');
    mount(h(UpdateNotice, { status: { version: 'v1.12.0' }, dismissible: true }), none);
    assert.equal(none.textContent, '', 'no update, no notice');
});

test('without storage, a dismissed notice stays gone until the page reloads', async () => {
    const before = globalThis.localStorage;
    globalThis.localStorage = {
        getItem() {
            throw new Error('denied');
        },
        setItem() {
            throw new Error('denied');
        },
    };
    const root = document.createElement('div');
    mount(h(UpdateNotice, { status: FORMS.installed[0], dismissible: true }), root);
    const dismiss = find(root, button('Dismiss'));
    dismiss.dispatch('click', { target: dismiss });
    await settle();
    assert.equal(root.textContent, '');
    mount(null, root);
    globalThis.localStorage = before;
});
