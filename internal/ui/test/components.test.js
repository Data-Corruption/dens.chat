import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { HoverRow } from '../assets/js/src/animations.js';
import { LongName, nameScroll, useBackdrop } from '../assets/js/src/components.jsx';

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

test("a name wider than its row scrolls by what's cut off, at one pace, while a mouse rests on the row", async () => {
    assert.equal(nameScroll(100, 100), null, 'a name that fits');
    assert.equal(nameScroll(100.6, 100), null, 'a pixel cut off');
    assert.deepEqual(nameScroll(160, 100), { by: 60, ms: 1500 });
    assert.deepEqual(nameScroll(320.2, 100), { by: 221, ms: 5525 });
    // The row measures the name as the pointer comes, and the stylesheet
    // moves it, as far as the row said. Effects run after a frame, which
    // the tests have none of.
    const root = document.createElement('div');
    mount(h(HoverRow, { id: 'row' }, h(LongName, { name: 'A name too long for its row' })), root);
    const row = root.find((n) => n.attributes.id === 'row');
    const name = root.find((n) => n.localName === 'span');
    Object.assign(name, { scrollWidth: 240, clientWidth: 120 });
    const scrolling = async (want) => {
        for (let i = 0; i < 60 && ('data-scrolling' in name.attributes) !== want; i++) await new Promise((resolve) => setTimeout(resolve, 5));
        return 'data-scrolling' in name.attributes;
    };
    assert.equal(await scrolling(false), false);
    row.dispatch('pointerenter', { pointerType: 'mouse' });
    assert.equal(await scrolling(true), true);
    assert.equal(name.style['--name-by'], '120px');
    assert.equal(name.style['--name-ms'], '3000ms');
    row.dispatch('pointerleave', { pointerType: 'mouse' });
    assert.equal(await scrolling(false), false, 'back as it leaves');
    // A finger's touch isn't a hover.
    row.dispatch('pointerenter', { pointerType: 'touch' });
    assert.equal(await scrolling(true), false, 'not under a finger');
    mount(null, root);
});
