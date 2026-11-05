// Appearance's Animations (M15.1): animated pictures play always, or on
// hover, while the pointer is over their row: a message, a member in the
// list or a call, a DM in the list. A profile plays them while it shows,
// since the member opened it, and on a touch screen, which has no hover,
// only a profile does. The browser keeps the choice, as it keeps the theme;
// until the member makes one, it's Always, or On hover where the system
// asks for less motion.

import { createContext, h } from 'preact';
import { useContext, useEffect, useMemo, useState } from 'preact/hooks';

const KEY = 'DENS_ANIMATIONS';
export const ALWAYS = 'always';
export const ON_HOVER = 'hover';

const listeners = new Set();
let watching = false;
// chosen is the member's choice on this page, which lasts until it reloads
// where storage is unavailable.
let chosen = null;

const lessMotion = () => (typeof window === 'undefined' ? null : window.matchMedia?.('(prefers-reduced-motion: reduce)'));

/** The member's choice, Always or On hover, or the system's until they
    make one */
export function animations() {
    let stored = chosen;
    try {
        stored = localStorage.getItem(KEY) ?? chosen;
    } catch {
        // Storage can be unavailable (private windows).
    }
    if (stored === ALWAYS || stored === ON_HOVER) return stored;
    return lessMotion()?.matches ? ON_HOVER : ALWAYS;
}

export function setAnimations(choice) {
    chosen = choice;
    try {
        localStorage.setItem(KEY, choice);
    } catch {
        // As above.
    }
    listeners.forEach((fn) => fn());
}

/** onAnimations calls fn whenever the choice may have changed, by the
    member or the system, until the function it returns is called */
export function onAnimations(fn) {
    if (!watching) {
        watching = true;
        lessMotion()?.addEventListener?.('change', () => listeners.forEach((f) => f()));
    }
    listeners.add(fn);
    return () => listeners.delete(fn);
}

/** useAnimations is the choice, kept current */
export function useAnimations() {
    const [choice, setChoice] = useState(animations);
    useEffect(() => {
        const update = () => setChoice(animations());
        update();
        return onAnimations(update);
    }, []);
    return choice;
}

// Play is where a picture shows: a row, which plays it while the pointer is
// over it, or a profile, which plays it while it shows. Elsewhere it plays
// only under Always.
const Play = createContext(null);

const SHOWN = { shown: true };

/** Shown plays the pictures in it while it shows, as a profile does */
export function Shown({ children }) {
    return h(Play.Provider, { value: SHOWN }, children);
}

// hoverScope is a row's: whether a pointer that hovers is over it, and the
// pictures in it that listen.
function hoverScope() {
    const listening = new Set();
    const scope = {
        over: false,
        listen(fn) {
            listening.add(fn);
            return () => listening.delete(fn);
        },
        set(over) {
            if (scope.over === over) return;
            scope.over = over;
            listening.forEach((fn) => fn());
        },
    };
    return scope;
}

/** HoverRow renders a row, as the element named by `as`, whose pictures
    play while a pointer that hovers is over it. A finger's touch isn't a
    hover, though the browser reports one. */
export function HoverRow({ as = 'div', children, ...props }) {
    const scope = useMemo(hoverScope, []);
    return h(
        as,
        {
            ...props,
            onPointerEnter: (e) => e.pointerType !== 'touch' && scope.set(true),
            onPointerLeave: () => scope.set(false),
        },
        h(Play.Provider, { value: scope }, children),
    );
}

/** useOver says whether the pointer is over the row something shows in, a
    profile counting as one, since the member opened it: verified marks
    shown on hover follow it (M16.4) */
export function useOver() {
    const scope = useContext(Play);
    const [over, setOver] = useState(() => !!scope?.over);
    useEffect(() => {
        if (!scope || scope === SHOWN) return undefined;
        setOver(scope.over);
        return scope.listen(() => setOver(scope.over));
    }, [scope]);
    return scope === SHOWN || over;
}

/** usePicture says how a picture shows: "file", the picture itself, which
    a still one always is and an animated one is while it plays from the
    start, under Always or in a profile; "still", an animated one's preview,
    its first frame; or "hover", its preview with the picture over it while
    the pointer is on its row. */
export function usePicture(animated) {
    const scope = useContext(Play);
    const [state, setState] = useState(() => ({ choice: animations(), over: !!scope?.over }));
    useEffect(() => {
        if (!animated) return undefined;
        const update = () => {
            const next = { choice: animations(), over: !!scope?.over };
            setState((s) => (s.choice === next.choice && s.over === next.over ? s : next));
        };
        update();
        const off = onAnimations(update);
        const unlisten = scope?.listen?.(update);
        return () => {
            off();
            unlisten?.();
        };
    }, [animated, scope]);
    if (!animated || state.choice === ALWAYS || scope?.shown) return 'file';
    return state.over ? 'hover' : 'still';
}
