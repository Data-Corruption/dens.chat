import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { useBackdrop } from '../assets/js/src/components.jsx';

function Backdrop({ onClose }) {
    return h('div', { ...useBackdrop(onClose), id: 'backdrop' }, h('div', { role: 'dialog' }, 'text'));
}

test("a dialog closes on a click outside it, but not on one whose press started inside", () => {
    const root = document.createElement('div');
    let closed = 0;
    mount(h(Backdrop, { onClose: () => closed++ }), root);
    const backdrop = root.find((n) => n.attributes.id === 'backdrop');
    const dialog = root.find((n) => n.attributes.role === 'dialog');
    const press = (target) => backdrop.dispatch('pointerdown', { target, currentTarget: backdrop });
    const click = (target) => backdrop.dispatch('click', { target, currentTarget: backdrop });

    press(backdrop);
    click(backdrop);
    assert.equal(closed, 1, 'pressed and let go outside');

    // Selecting text from inside the dialog and letting go outside it
    // clicks the backdrop too.
    press(dialog);
    click(backdrop);
    assert.equal(closed, 1, 'pressed inside');

    press(dialog);
    click(dialog);
    assert.equal(closed, 1, 'a click inside');

    // A click with no press first, as a script's, counts as outside, and
    // a press inside doesn't stick to the next click.
    click(backdrop);
    assert.equal(closed, 2);
});
