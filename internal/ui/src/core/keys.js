// The page's keys for getting around: Ctrl+K opens the switcher, Alt+↑
// and Alt+↓ go to the channel or DM before and after in the list, Ctrl+/
// opens the list of keys, and Esc, with nothing open to close and the
// message box empty, marks the channel read and goes to its newest
// message. One listener on the window hears them, and what shows sets
// what each does: the app the switcher's and the list's, a den's page the
// rest. A voice key bound over one takes the press: the voice keys'
// listener hears it first, capturing, and takes its default.

import { keyMatches } from '../voice/voice.js';

// PAGE_KEYS are the keys, by name, as the list of keys shows them, each
// with a press that stands for it.
export const PAGE_KEYS = [
    { name: 'switcher', keys: ['Ctrl', 'K'], what: 'Find a channel, DM, collection or den', press: { code: 'KeyK', ctrlKey: true } },
    { name: 'previous', keys: ['Alt', '↑'], what: 'The channel or DM above', press: { code: 'ArrowUp', altKey: true } },
    { name: 'next', keys: ['Alt', '↓'], what: 'The channel or DM below', press: { code: 'ArrowDown', altKey: true } },
    { name: 'escape', keys: ['Esc'], what: 'Mark the channel read, and go to its newest message', press: { code: 'Escape' } },
    { name: 'keys', keys: ['Ctrl', '/'], what: 'This list', press: { code: 'Slash', ctrlKey: true } },
];

// pageKey names the page key a keydown is, or null. Ctrl+K goes by the
// letter, and on a layout without Latin letters by where K is; Ctrl+/
// takes Shift where / does.
export function pageKey(e) {
    const ctrl = (e.ctrlKey || e.metaKey) && !e.altKey;
    const plain = !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey;
    const letter = /^[a-z]$/i.test(e.key) ? e.key.toLowerCase() : e.code === 'KeyK' ? 'k' : '';
    if (ctrl && !e.shiftKey && letter === 'k') return 'switcher';
    if (ctrl && e.key === '/') return 'keys';
    if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
        if (e.key === 'ArrowUp') return 'previous';
        if (e.key === 'ArrowDown') return 'next';
    }
    if (plain && e.key === 'Escape') return 'escape';
    return null;
}

// takenBy names the voice key, of keys, that takes a page key's press, or
// ''.
export function takenBy(pageKeyInfo, keys) {
    const e = { ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, ...pageKeyInfo.press };
    return Object.keys(keys).find((action) => keyMatches(keys[action], e)) || '';
}

const actions = new Map();

// onPageKey has fn do what a page key does, by pageKey's name, until the
// function it returns unsets it. fn gets { layer }, whether something
// Esc would close showed as the key went down.
export function onPageKey(name, fn) {
    actions.set(name, fn);
    return () => actions.get(name) === fn && actions.delete(name);
}

// LAYERS are what shows over the page that Esc closes: dialogs, a
// profile card and the emoji picker among them, menus, a sheet, a
// completion's list, and the call's more controls.
const LAYERS = '[role="dialog"], [role="menu"], [role="listbox"], [data-sheet], [data-call-more]';

// layerOpen says whether a layer shows, of those the page keeps: one
// hidden has no box.
export function layerOpen(doc) {
    return [...doc.querySelectorAll(LAYERS)].some((el) => el.getClientRects().length > 0);
}

// UNTYPED are the inputs Esc does nothing in.
const UNTYPED = ['checkbox', 'radio', 'range', 'button', 'submit', 'reset', 'color', 'file', 'image'];

// fieldTakesEscape says whether Esc in el, where the focus is, does
// something of its own: in any field but the message box, as editing a
// message, and in the message box with something in it, text, files, a
// poll or a reply.
function fieldTakesEscape(el) {
    if (!el) return false;
    const typed = el.isContentEditable || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || (el.tagName === 'INPUT' && !UNTYPED.includes(el.type));
    if (!typed) return false;
    return !el.matches('[data-message-box]') || !!el.value || el.hasAttribute('data-unsent');
}

// listenForPageKeys listens on win for the page's keys, and returns a
// function that stops. Whether something was open is read as the key goes
// down, capturing, before what it closes hears it.
export function listenForPageKeys(win) {
    let layer = false;
    let busy = false;
    const capture = (e) => {
        layer = layerOpen(win.document);
        busy = fieldTakesEscape(e.target);
    };
    const bubble = (e) => {
        const name = pageKey(e);
        const fn = name && actions.get(name);
        if (!fn || e.defaultPrevented) return;
        if (name === 'escape' && (layer || busy)) return;
        if ((name === 'previous' || name === 'next') && layer) return;
        e.preventDefault();
        fn({ layer });
    };
    win.addEventListener('keydown', capture, true);
    win.addEventListener('keydown', bubble);
    return () => {
        win.removeEventListener('keydown', capture, true);
        win.removeEventListener('keydown', bubble);
    };
}
