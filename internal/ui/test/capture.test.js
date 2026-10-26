import assert from 'node:assert/strict';
import { test } from 'node:test';
import { captureKey } from '../assets/js/src/voice.js';

// The page's listeners, which captureKey adds to window as it starts.
const listeners = {};
globalThis.window = { addEventListener: (type, fn) => { listeners[type] = fn; } };
globalThis.document = { addEventListener() {}, hidden: false };

const mouse = (button) => ({ button, ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, preventDefault() {}, stopPropagation() {} });

// A wait that never ends fails rather than hanging the run.
test('a click elsewhere ends the wait for a key, as Escape does', { timeout: 2000 }, async () => {
    const waiting = captureKey();
    listeners.pointerdown(mouse(0));
    assert.equal(await waiting, null);
});

test("a side button's press still records it", { timeout: 2000 }, async () => {
    const waiting = captureKey();
    listeners.pointerdown(mouse(3));
    listeners.mousedown(mouse(3));
    assert.deepEqual(await waiting, { code: 'Mouse4', label: 'Mouse 4', ctrl: false, alt: false, shift: false, meta: false });
});
