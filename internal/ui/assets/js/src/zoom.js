// Zoom (M13.3): a picture fitted to its stage, zoomed and panned, as the
// viewer and comparing show one. On a touch screen two fingers pinch it, a
// double tap zooms in at the spot or back out, and once it's larger than
// the stage a finger pans it; with a mouse the wheel zooms at the pointer,
// a double click does what a double tap does, and a drag pans. A press on
// a fitted picture is its user's: the viewer's swipes, between pictures
// and down to close, and comparing's click to look closer.
//
// A view is { scale, x, y }: the picture, fitted to the stage at the size
// box, shown scale times as large, its top left corner at x, y in the
// stage's pixels.

import { useEffect, useRef, useState } from 'preact/hooks';
import { SLOP } from './touch.js';

// ZOOM is how far a double tap or a double click zooms in.
export const ZOOM = 2.5;

// A second tap within DOUBLE ms of the first, and within NEAR pixels of
// it, makes a double tap.
export const DOUBLE = 300;
export const NEAR = 30;

// fitted is the view of a picture of size box in a stage of size stage: as
// it's fitted, in the stage's middle.
export function fitted(box, stage) {
    return { scale: 1, x: (stage.width - box.width) / 2, y: (stage.height - box.height) / 2 };
}

// clampView keeps a view's scale from min to max, and its picture covering
// the stage each way it's larger than the stage, and in the middle each way
// it's smaller.
export function clampView(v, box, stage, max, min = 1) {
    const scale = Math.min(max, Math.max(min, v.scale));
    const axis = (o, b, s) => (b <= s ? (s - b) / 2 : Math.min(0, Math.max(s - b, o)));
    return { scale, x: axis(v.x, box.width * scale, stage.width), y: axis(v.y, box.height * scale, stage.height) };
}

// zoomAt zooms a view to scale, keeping the spot of the picture under the
// stage's point px, py where it is.
export function zoomAt(v, scale, px, py, box, stage, max, min = 1) {
    const s = Math.min(max, Math.max(min, scale));
    const k = s / v.scale;
    return clampView({ scale: s, x: px - (px - v.x) * k, y: py - (py - v.y) * k }, box, stage, max, min);
}

// pinched is where a pinch has taken the view start: its fingers began
// dist0 apart about the point mid0, and are now dist apart about mid. The
// scale follows their spread, and the spot that was under mid0 follows mid.
export function pinched(start, mid0, dist0, mid, dist, box, stage, max, min = 1) {
    const s = Math.min(max, Math.max(min, start.scale * (dist0 > 0 ? dist / dist0 : 1)));
    const k = s / start.scale;
    return clampView({ scale: s, x: mid.x - (mid0.x - start.x) * k, y: mid.y - (mid0.y - start.y) * k }, box, stage, max, min);
}

// panned is the view start moved by dx, dy, as far as its picture reaches.
export function panned(start, dx, dy, box, stage, max, min = 1) {
    return clampView({ scale: start.scale, x: start.x + dx, y: start.y + dy }, box, stage, max, min);
}

// wheeled is how much a turn of the wheel by dy zooms, as a factor, up
// zooming in; mode is its deltaMode, which counts lines or pages rather
// than pixels. A touchpad's pinch comes as the wheel too, in small steps.
export function wheeled(dy, mode = 0) {
    const px = mode === 1 ? dy * 16 : mode === 2 ? dy * 400 : dy;
    return Math.exp(-Math.max(-200, Math.min(200, px)) * 0.002);
}

// toggled is a double tap's view: ZOOM times as large at the spot, from a
// fitted view, and fitted again from a larger one.
export function toggled(v, px, py, box, stage, max, min = 1) {
    if (v.scale > 1.01) return fitted(box, stage);
    return zoomAt(v, Math.min(max, ZOOM), px, py, box, stage, max, min);
}

// maxScale is how far a picture w pixels wide, fitted box.width wide, may
// zoom: until its own pixels show twice as large as the screen's, whose
// ratio to the page's is dpr, and at least four times.
export function maxScale(w, box, dpr) {
    if (!(w > 0 && box.width > 0)) return 4;
    return Math.max(4, (2 * w) / (dpr > 0 ? dpr : 1) / box.width);
}

// isFitted says whether a view shows its picture as it's fitted.
export const isFitted = (v) => Math.abs(v.scale - 1) < 0.01;

// useZoom follows the presses on a stage of size stage showing a picture
// fitted at box, which zoom from min to max, and returns the view, the
// stage's handlers, and set, which shows a view. key names the picture: a
// new one starts fitted. With its picture fitted, a press is on's, but a
// pinch: on.drag(dx, dy) as it moves past the slop, on.drop(dx, dy,
// speed) as it's let go after, speed in pixels a millisecond each way,
// on.cancel() as the browser takes it, and on.tap(x, y) for one that
// didn't move, in the stage's pixels. A finger held hold ms without
// moving calls on.hold(true), and on.hold(false) once it's lifted, and is
// no tap. double, unless false, has a double tap zoom in and out.
export function useZoom({ box, stage, max, min = 1, key, on = {}, hold = 0, double = true }) {
    const [view, setView] = useState(() => fitted(box, stage));
    const live = useRef(null);
    live.current = { view, box, stage, max, min, on, hold, double };
    const pointers = useRef(new Map());
    const gesture = useRef(null);
    const lastTap = useRef(null);
    useEffect(() => {
        gesture.current = null;
        setView(fitted(box, stage));
    }, [key, box.width, box.height, stage.width, stage.height]);
    useEffect(() => () => clearTimeout(gesture.current?.timer), []);

    const clamp = (v) => {
        const l = live.current;
        return clampView(v, l.box, l.stage, l.max, l.min);
    };
    const at = (e) => {
        const r = e.currentTarget.getBoundingClientRect();
        return { x: e.clientX - r.left, y: e.clientY - r.top };
    };
    const pair = () => {
        const [a, b] = [...pointers.current.values()];
        return { mid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }, dist: Math.hypot(a.x - b.x, a.y - b.y) };
    };
    // press starts a one-pointer gesture from the point p.
    const press = (p, id, type) => {
        const l = live.current;
        const g = { kind: 'press', id, type, x: p.x, y: p.y, view: l.view, moved: false, held: false, last: { x: p.x, y: p.y, t: performance.now() }, speed: { x: 0, y: 0 } };
        if (l.hold && type !== 'mouse' && l.on.hold) {
            g.timer = setTimeout(() => {
                if (gesture.current !== g || g.moved) return;
                g.held = true;
                live.current.on.hold?.(true);
            }, l.hold);
        }
        gesture.current = g;
    };
    // settle fits a picture a pinch left only a little larger, which
    // would take the next press for a pan rather than its user's.
    const settle = () => {
        const l = live.current;
        if (l.view.scale > 1 && l.view.scale < 1.1) setView(fitted(l.box, l.stage));
    };
    // letGo ends a held press, which showed what it held.
    const letGo = (g) => {
        clearTimeout(g?.timer);
        if (g?.held) live.current.on.hold?.(false);
    };

    function down(e) {
        if (e.pointerType === 'mouse' && e.button !== 0) return;
        try {
            e.currentTarget.setPointerCapture(e.pointerId);
        } catch {
            // A pointer already gone; its up comes all the same.
        }
        const p = at(e);
        pointers.current.set(e.pointerId, p);
        const g = gesture.current;
        if (pointers.current.size === 2) {
            // A second finger makes a pinch of whatever the first began.
            letGo(g);
            if (g?.kind === 'press' && g.moved && g.dragging) live.current.on.cancel?.();
            const { mid, dist } = pair();
            gesture.current = { kind: 'pinch', view: live.current.view, mid, dist };
        } else if (pointers.current.size === 1) {
            press(p, e.pointerId, e.pointerType);
        }
    }

    function move(e) {
        if (!pointers.current.has(e.pointerId)) return;
        const p = at(e);
        pointers.current.set(e.pointerId, p);
        const g = gesture.current;
        const l = live.current;
        if (g?.kind === 'pinch' && pointers.current.size >= 2) {
            const { mid, dist } = pair();
            setView(pinched(g.view, g.mid, g.dist, mid, dist, l.box, l.stage, l.max, l.min));
            return;
        }
        if (g?.kind !== 'press' || g.id !== e.pointerId || g.held) return;
        const dx = p.x - g.x;
        const dy = p.y - g.y;
        const now = performance.now();
        const dt = now - g.last.t;
        if (dt > 0) g.speed = { x: (p.x - g.last.x) / dt, y: (p.y - g.last.y) / dt };
        g.last = { x: p.x, y: p.y, t: now };
        if (!g.moved) {
            if (Math.hypot(dx, dy) <= SLOP) return;
            g.moved = true;
            clearTimeout(g.timer);
            g.dragging = g.view.scale <= 1.01;
        }
        if (g.dragging) l.on.drag?.(dx, dy);
        else setView(panned(g.view, dx, dy, l.box, l.stage, l.max, l.min));
    }

    function up(e) {
        if (!pointers.current.delete(e.pointerId)) return;
        const g = gesture.current;
        const l = live.current;
        if (g?.kind === 'pinch') {
            // The finger that stays pans on from where the pinch left it.
            if (pointers.current.size === 1) {
                const [[id, p]] = [...pointers.current];
                gesture.current = { kind: 'press', id, type: 'touch', x: p.x, y: p.y, view: l.view, moved: true, dragging: false, pinched: true,
                    last: { ...p, t: performance.now() }, speed: { x: 0, y: 0 } };
            } else if (pointers.current.size === 0) {
                gesture.current = null;
                settle();
            }
            return;
        }
        if (g?.kind !== 'press' || g.id !== e.pointerId) return;
        gesture.current = null;
        letGo(g);
        if (g.pinched) settle();
        if (g.held) return;
        const p = at(e);
        if (g.moved) {
            if (g.dragging) l.on.drop?.(p.x - g.x, p.y - g.y, g.speed);
            return;
        }
        const t = lastTap.current;
        const now = performance.now();
        if (l.double && t && now - t.at < DOUBLE && Math.hypot(p.x - t.x, p.y - t.y) < NEAR) {
            lastTap.current = null;
            setView(toggled(l.view, p.x, p.y, l.box, l.stage, l.max, l.min));
            return;
        }
        lastTap.current = { at: now, x: p.x, y: p.y };
        l.on.tap?.(p.x, p.y);
    }

    function cancel(e) {
        if (!pointers.current.delete(e.pointerId)) return;
        const g = gesture.current;
        if (g?.kind === 'press' && g.id === e.pointerId) {
            gesture.current = null;
            letGo(g);
            if (g.dragging) live.current.on.cancel?.();
        } else if (g?.kind === 'pinch' && pointers.current.size === 0) {
            gesture.current = null;
        }
    }

    function wheel(e) {
        e.preventDefault();
        const l = live.current;
        const p = at(e);
        setView(zoomAt(l.view, l.view.scale * wheeled(e.deltaY, e.deltaMode), p.x, p.y, l.box, l.stage, l.max, l.min));
    }

    return {
        view: clamp(view),
        set: (v) => setView(clamp(v)),
        handlers: { onPointerDown: down, onPointerMove: move, onPointerUp: up, onPointerCancel: cancel, onWheel: wheel },
    };
}
