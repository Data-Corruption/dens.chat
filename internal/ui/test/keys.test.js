// The page's keys: which press is which, Esc only with nothing open to
// close and the message box empty, and a voice key bound over one taking
// the press, with the voice keys' own listener and the page's both
// hearing it, in the order a browser runs them.

import assert from 'node:assert/strict';
import { test } from 'node:test';

// A window whose listeners the test runs as a browser would: the
// capturing ones first, in the order they were added, then the rest.
const listeners = [];
const field = (tag, extra = {}) => ({ tagName: tag, matches: () => false, hasAttribute: () => false, value: '', ...extra });
const body = field('BODY');
let open = [];
const doc = {
    activeElement: body,
    hidden: false,
    querySelectorAll: () => open,
    addEventListener() {},
};
globalThis.document = doc;
globalThis.window = {
    document: doc,
    location: { protocol: 'http:', host: 'localhost:8484' },
    addEventListener: (type, fn, capture = false) => listeners.push({ type, fn, capture: !!capture }),
    removeEventListener: (type, fn, capture = false) => {
        const at = listeners.findIndex((l) => l.type === type && l.fn === fn && l.capture === !!capture);
        if (at >= 0) listeners.splice(at, 1);
    },
};
globalThis.localStorage = { getItem: () => null, setItem() {}, removeItem() {} };
globalThis.fetch = async () => ({ ok: true, status: 200, headers: { get: () => null }, text: async () => '{}' });
const sockets = [];
globalThis.WebSocket = class {
    constructor() {
        this.handlers = {};
        sockets.push(this);
    }
    addEventListener(type, fn) {
        (this.handlers[type] ||= []).push(fn);
    }
    send() {}
    close() {}
};

const { PAGE_KEYS, layerOpen, listenForPageKeys, onPageKey, pageKey, takenBy } = await import('../src/core/keys.js');
const { keysElsewhere } = await import('../src/voice/voice.js');

// press sends a keydown through the window's listeners, as a browser
// does, on the focus, and says whether its default went.
function press(key, mods = {}, target = doc.activeElement) {
    const code = { k: 'KeyK', '/': 'Slash', ArrowUp: 'ArrowUp', ArrowDown: 'ArrowDown', Escape: 'Escape' }[key] || key;
    const e = {
        type: 'keydown', key, code, target, repeat: false, ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, ...mods,
        defaultPrevented: false,
        preventDefault() {
            this.defaultPrevented = true;
        },
        stopPropagation() {},
    };
    for (const capture of [true, false]) {
        for (const l of listeners.filter((x) => x.type === 'keydown' && x.capture === capture)) l.fn(e);
    }
    return e.defaultPrevented;
}

test('each of the keys is its press, and no other', () => {
    const k = (key, mods = {}) => pageKey({ key, code: key === 'k' ? 'KeyK' : '', ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, ...mods });
    assert.equal(k('k', { ctrlKey: true }), 'switcher');
    assert.equal(k('K', { metaKey: true }), 'switcher');
    assert.equal(pageKey({ key: 'л', code: 'KeyK', ctrlKey: true }), 'switcher', 'a layout of other letters, by where K is');
    assert.equal(k('/', { ctrlKey: true }), 'keys');
    assert.equal(k('/', { ctrlKey: true, shiftKey: true }), 'keys', 'a layout where / takes Shift');
    assert.equal(k('ArrowUp', { altKey: true }), 'previous');
    assert.equal(k('ArrowDown', { altKey: true }), 'next');
    assert.equal(k('Escape'), 'escape');
    for (const [key, mods] of [['k', {}], ['k', { ctrlKey: true, shiftKey: true }], ['k', { altKey: true }], ['ArrowDown', {}], ['ArrowDown', { altKey: true, ctrlKey: true }],
        ['Escape', { shiftKey: true }], ['/', {}]]) {
        assert.equal(k(key, mods), null, `${key} ${JSON.stringify(mods)}`);
    }
});

test('Esc marks read only with nothing open to close and the message box empty', () => {
    const stop = listenForPageKeys(window);
    let escapes = 0;
    const unset = onPageKey('escape', () => escapes++);
    try {
        assert.equal(press('Escape'), true);
        assert.equal(escapes, 1);
        // A dialog, a menu or a sheet on screen closes first.
        open = [{ getClientRects: () => [{}] }];
        assert.equal(layerOpen(doc), true);
        assert.equal(press('Escape'), false);
        // One kept but not shown doesn't count.
        open = [{ getClientRects: () => [] }];
        press('Escape');
        assert.equal(escapes, 2);
        open = [];
        const box = (value, unsent = false) => field('TEXTAREA', { value, matches: (s) => s === '[data-message-box]', hasAttribute: (a) => a === 'data-unsent' && unsent });
        press('Escape', {}, box(''));
        assert.equal(escapes, 3, 'the message box, empty');
        press('Escape', {}, box('half a thought'));
        press('Escape', {}, box('', true));
        press('Escape', {}, field('TEXTAREA', { value: '' }));
        press('Escape', {}, field('INPUT', { type: 'search' }));
        assert.equal(escapes, 3, "text, a reply or files in the box, or another field's own Esc");
        press('Escape', {}, field('INPUT', { type: 'checkbox' }));
        assert.equal(escapes, 4, 'a checkbox types nothing');
    } finally {
        unset();
        stop();
    }
});

test('the getting-around keys go nowhere while something is open, and Ctrl+K is the page\'s even then', () => {
    const stop = listenForPageKeys(window);
    const heard = [];
    const unsets = ['switcher', 'previous', 'next'].map((n) => onPageKey(n, (at) => heard.push([n, at.layer])));
    try {
        assert.equal(press('ArrowDown', { altKey: true }), true);
        open = [{ getClientRects: () => [{}] }];
        assert.equal(press('ArrowDown', { altKey: true }), false);
        // Firefox opens its search bar on Ctrl+K unless the page takes it.
        assert.equal(press('k', { ctrlKey: true }), true);
        open = [];
        assert.deepEqual(heard, [['next', false], ['switcher', true]]);
    } finally {
        unsets.forEach((u) => u());
        stop();
    }
    // With nothing set for it, a key is the browser's.
    const again = listenForPageKeys(window);
    assert.equal(press('ArrowDown', { altKey: true }), false);
    again();
});

test('a voice key bound over a page key takes the press, and the list says which', async () => {
    keysElsewhere();
    sockets.at(-1).handlers.message?.forEach((fn) => fn({ data: JSON.stringify({ t: 'keys', d: { keys: { toggleMute: { code: 'KeyK', label: 'K', ctrl: true } } } }) }));
    const stop = listenForPageKeys(window);
    let switched = 0;
    let previous = 0;
    const unsets = [onPageKey('switcher', () => switched++), onPageKey('previous', () => previous++)];
    try {
        // The voice keys listen on the window too, capturing, so a press
        // of theirs is taken before the page's listener hears it.
        assert.equal(press('k', { ctrlKey: true }), true);
        assert.equal(switched, 0, 'Ctrl+K toggled mute, not the switcher');
        press('ArrowUp', { altKey: true });
        assert.equal(previous, 1);
    } finally {
        unsets.forEach((u) => u());
        stop();
    }
    const keys = { toggleMute: { code: 'KeyK', label: 'K', ctrl: true }, ptt: { code: 'ArrowUp', label: 'ArrowUp' } };
    const switcher = PAGE_KEYS.find((k) => k.name === 'switcher');
    assert.equal(takenBy(switcher, keys), 'toggleMute');
    // A voice key without Alt takes Alt's press too, as the voice keys
    // match a binding.
    assert.equal(takenBy(PAGE_KEYS.find((k) => k.name === 'previous'), keys), 'ptt');
    assert.equal(takenBy(PAGE_KEYS.find((k) => k.name === 'keys'), keys), '');
});
