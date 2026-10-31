import assert from 'node:assert/strict';
import { test } from 'node:test';
import { LONG_PRESS, SLOP, coarse, createPress, dismisses, tipShift } from '../assets/js/src/touch.js';

// clock is setTimeout and clearTimeout on a time the test moves on.
function clock() {
    let now = 0;
    let ids = 0;
    const waiting = new Map();
    return {
        setTimeout(fn, ms) {
            waiting.set(++ids, { fn, at: now + ms });
            return ids;
        },
        clearTimeout(id) {
            waiting.delete(id);
        },
        pass(ms) {
            now += ms;
            for (const [id, t] of [...waiting].sort((a, b) => a[1].at - b[1].at)) {
                if (t.at > now) continue;
                waiting.delete(id);
                t.fn();
            }
        },
    };
}

const at = (x, y, more = {}) => ({ button: 0, isPrimary: true, pointerId: 1, clientX: x, clientY: y, ...more });

test('a press held still for the long press time is a long press, and one let go sooner is not', () => {
    const timers = clock();
    let long = 0;
    const press = createPress({ onLong: () => long++, timers });
    press.down(at(100, 100));
    timers.pass(LONG_PRESS - 1);
    assert.equal(long, 0);
    timers.pass(1);
    assert.equal(long, 1, 'held long enough');
    press.up();

    press.down(at(100, 100));
    timers.pass(LONG_PRESS / 2);
    press.up();
    timers.pass(LONG_PRESS);
    assert.equal(long, 1, 'let go sooner');
});

test('a press that moves past the slop, or that the browser takes over, is no long press', () => {
    const timers = clock();
    let long = 0;
    const press = createPress({ onLong: () => long++, timers });
    press.down(at(100, 100));
    press.move(at(100 + SLOP - 1, 100));
    timers.pass(LONG_PRESS);
    assert.equal(long, 1, 'a finger never stays quite still');
    press.up();

    press.down(at(100, 100));
    press.move(at(100, 100 + SLOP + 1));
    timers.pass(LONG_PRESS);
    assert.equal(long, 1, 'a scroll starting');

    press.down(at(100, 100));
    press.cancel();
    timers.pass(LONG_PRESS);
    assert.equal(long, 1, 'taken over, as a swipe between panes is');

    // Another finger's move doesn't count against the first.
    press.down(at(100, 100));
    press.move(at(300, 300, { pointerId: 2 }));
    timers.pass(LONG_PRESS);
    assert.equal(long, 2);
});

test("only a primary button's press counts", () => {
    const timers = clock();
    let long = 0;
    const press = createPress({ onLong: () => long++, timers });
    press.down(at(100, 100, { button: 2 }));
    timers.pass(LONG_PRESS);
    press.down(at(100, 100, { isPrimary: false }));
    timers.pass(LONG_PRESS);
    assert.equal(long, 0);
});

test("a long press's click goes nowhere, and a tap's goes on", () => {
    const timers = clock();
    const press = createPress({ onLong: () => {}, timers });
    press.down(at(100, 100));
    timers.pass(LONG_PRESS);
    press.up();
    assert.equal(press.click(), true, "the long press's");
    assert.equal(press.click(), false, 'once');

    press.down(at(100, 100));
    timers.pass(50);
    press.up();
    assert.equal(press.click(), false, "a tap's");

    // A browser that sends no click after a long press leaves nothing to
    // swallow the next one, from the keyboard, once a moment has passed.
    press.down(at(100, 100));
    timers.pass(LONG_PRESS);
    press.up();
    timers.pass(1000);
    assert.equal(press.click(), false);
});

test('a sheet dragged down closes past a third of its height, or flicked, and otherwise goes back up', () => {
    assert.equal(dismisses(100, 300, 0.1), false);
    assert.equal(dismisses(101, 300, 0.1), true, 'past a third');
    assert.equal(dismisses(40, 300, 0.8), true, 'flicked');
    assert.equal(dismisses(2 * SLOP, 300, 2), false, 'a flick has to go somewhere');
    assert.equal(dismisses(80, 300, 0), false, 'held still before letting go');
});

test('a tip moves in from the edge it would run past, and one wider than the window starts at its margin', () => {
    const box = (left, width) => ({ left, right: left + width, width });
    assert.equal(tipShift(box(100, 200), 390), 0);
    assert.equal(tipShift(box(250, 200), 390), -68, 'past the right edge');
    assert.equal(tipShift(box(-40, 200), 390), 48, 'past the left');
    assert.equal(tipShift(box(20, 380), 390), -12, 'wider than the room');
});

test("the pointer is coarse as the window's media say", () => {
    const win = (matches) => ({ matchMedia: (q) => ({ matches: q === '(pointer: coarse)' && matches }) });
    assert.equal(coarse(win(true)), true);
    assert.equal(coarse(win(false)), false);
    assert.equal(coarse({}), false, 'no media to ask');
});
