// Touch in place of hover (M13.2): whether the page's pointer is a
// finger's, a long press, and when a sheet swiped down closes. The ways of
// working the page follow the primary pointer, a long press, sheets'
// rows and bigger targets for a coarse one and hover for one that hovers,
// as its layout follows the window's width (nav.js).

import { useEffect, useState } from 'preact/hooks';

const COARSE = '(pointer: coarse)';

// coarse says whether the page's primary pointer is coarse, as a finger
// is. win is the page's window, or a stand-in in the tests.
export function coarse(win = globalThis) {
    return !!win.matchMedia?.(COARSE).matches;
}

// useCoarse is coarse, kept current.
export function useCoarse() {
    const [is, setIs] = useState(() => typeof window !== 'undefined' && coarse(window));
    useEffect(() => {
        const q = window.matchMedia?.(COARSE);
        if (!q) return undefined;
        const change = () => setIs(q.matches);
        change();
        q.addEventListener('change', change);
        return () => q.removeEventListener('change', change);
    }, []);
    return is;
}

// A press held this long, in ms, as Android's own long press waits, is a
// long one, unless it moves more than SLOP pixels first, as a scroll or a
// swipe does.
export const LONG_PRESS = 500;
export const SLOP = 10;

// The click a long press may be followed by, as some browsers send one
// when it's let go, comes within this long of letting go.
const AFTER = 400;

// createPress follows the presses on something for a long one, and calls
// onLong with the event of a press held long enough. Its handlers take
// pointer events; the browser taking the press over, as for a scroll,
// cancels it. A long press's click goes no further, nor anything else's
// that it lands on, as a sheet opened under the finger: click says
// whether a click is that one. timers are setTimeout and clearTimeout,
// stand-ins in the tests.
export function createPress({ onLong, wait = LONG_PRESS, slop = SLOP, timers = globalThis }) {
    let start = null;
    let timer = 0;
    let fired = false;
    let forget = 0;
    function stop() {
        timers.clearTimeout(timer);
        timer = 0;
        start = null;
    }
    return {
        down(e) {
            stop();
            timers.clearTimeout(forget);
            fired = false;
            if (e.button !== 0 || e.isPrimary === false) return;
            start = { x: e.clientX, y: e.clientY, id: e.pointerId };
            timer = timers.setTimeout(() => {
                timer = 0;
                start = null;
                fired = true;
                onLong(e);
            }, wait);
        },
        move(e) {
            if (!start || e.pointerId !== start.id) return;
            if (Math.hypot(e.clientX - start.x, e.clientY - start.y) > slop) stop();
        },
        up() {
            stop();
            if (fired) forget = timers.setTimeout(() => { fired = false; }, AFTER);
        },
        cancel() {
            stop();
            fired = false;
        },
        click() {
            const was = fired;
            fired = false;
            return was;
        },
    };
}

// dismisses says whether a sheet dragged down dy pixels of its height,
// moving at speed pixels a millisecond as it's let go, closes: past a
// third of its height, or flicked down.
export function dismisses(dy, height, speed) {
    return dy > height / 3 || (dy > 2 * SLOP && speed > 0.5);
}

// tipShift is how far a tip whose box is rect moves sideways to stay
// within a window vw wide, margin from each edge; one wider than the room
// starts at the left margin.
export function tipShift(rect, vw, margin = 8) {
    if (rect.left < margin || rect.width > vw - 2 * margin) return margin - rect.left;
    if (rect.right > vw - margin) return vw - margin - rect.right;
    return 0;
}
