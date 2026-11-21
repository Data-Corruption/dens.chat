// The rest of the call's controls: the arrow in the call's block,
// the grid it opens, with the soundboard where the den has one and the
// call's lock, and where a popup stands by its button.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { CallMore } from '../src/voice/callmore.jsx';
import { popupAt } from '../src/ui/popup.js';

// What a popup asks of the page: the window's size, its button's place,
// and the presses and keys of the whole document.
const heard = {};
globalThis.window = { innerWidth: 1000, innerHeight: 800, addEventListener() {}, removeEventListener() {} };
document.addEventListener = (type, fn) => (heard[type] = fn);
document.removeEventListener = (type) => delete heard[type];
const Node = document.createElement('div').constructor;
Node.prototype.getBoundingClientRect = () => ({ left: 12, top: 700, bottom: 732 });
Node.prototype.contains = function contains(node) {
    for (let n = node; n; n = n.parentNode) if (n === this) return true;
    return false;
};

const wait = (ms = 10) => new Promise((resolve) => setTimeout(resolve, ms));
const marked = (name) => (n) => n.attributes[name] !== undefined;
const press = (node) => node.dispatch('click', { target: node });

const call = { den: 'd1', channel: '5', sealed: true, deafened: false };
const quiet = { den: 'd1', role: 'member', sounds: [] };
const loud = { den: 'd1', role: 'member', soundLimits: { sounds: 50 }, sounds: [{ id: '1', name: 'Horn', duration_ms: 900 }] };
const board = { waitUntil: 0, notice: '' };

test('a popup stands up from its button, or down, within the window', () => {
    assert.deepEqual(popupAt({ left: 12, top: 700, bottom: 732 }, false, 1000, 800, 288), { left: 12, bottom: 108 });
    assert.deepEqual(popupAt({ left: 12, top: 100, bottom: 132 }, true, 1000, 800, 288), { left: 12, top: 140 });
    assert.equal(popupAt({ left: 900, top: 700, bottom: 732 }, false, 1000, 800, 288).left, 704, 'kept from the right edge');
    assert.equal(popupAt({ left: -40, top: 700, bottom: 732 }, false, 1000, 800, 288).left, 8, 'and the left');
});

test("the arrow opens the lock, and the soundboard where the den has one, and closes on a press elsewhere", async () => {
    const root = document.createElement('div');
    const show = (c, den) => mount(h(CallMore, { call: c, den, board, sounding: null }), root);
    try {
        show({ ...call, sealed: undefined }, quiet);
        assert.equal(root.find(marked('data-call-more-button')), null, 'no arrow with nothing to open');

        show(call, quiet);
        const arrow = root.find(marked('data-call-more-button'));
        assert.equal(arrow.attributes['aria-expanded'], 'false');
        assert.equal(root.find(marked('data-call-more')), null, 'closed until pressed');
        press(arrow);
        await wait();
        const grid = root.find(marked('data-call-more'));
        assert.ok(grid, 'the grid');
        assert.deepEqual({ ...grid.style }, { left: '12px', bottom: '108px' }, 'up from the arrow');
        assert.equal(arrow.attributes['aria-expanded'], 'true');
        assert.equal(grid.find(marked('data-call-lock')).attributes['data-call-lock'], 'sealed');
        assert.equal(grid.find(marked('data-soundboard-button')), null, 'no soundboard in a den without one');

        heard.pointerdown({ target: grid });
        await wait();
        assert.ok(root.find(marked('data-call-more')), 'a press in the grid leaves it open');
        heard.pointerdown({ target: document.createElement('div') });
        await wait();
        assert.equal(root.find(marked('data-call-more')), null, 'a press elsewhere closes it');

        show(call, loud);
        press(root.find(marked('data-call-more-button')));
        await wait();
        assert.equal(root.find(marked('data-soundboard-button')).attributes['data-soundboard-button'], 'block');
        heard.keydown({ key: 'Escape' });
        await wait();
        assert.equal(root.find(marked('data-call-more')), null, 'Escape closes it');

        show({ ...call, sealed: false }, quiet);
        assert.match(root.find(marked('data-call-more-button')).attributes.class, /text-warning/, "a call that isn't sealed marks the arrow");
    } finally {
        mount(null, root);
    }
});
