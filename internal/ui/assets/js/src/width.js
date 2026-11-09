// Appearance's Width (M19.1): how wide messages run. Full takes the pane;
// Wide and Narrow hold the message list, its composer and the channel's
// header to 1,200 or 900 pixels, centered, with the pane's own background
// either side. The browser keeps the choice, as it keeps Density; until
// the member makes one, it's Full.

import { useEffect, useState } from 'preact/hooks';

const KEY = 'DENS_WIDTH';
export const WIDTH_FULL = 'full';
export const WIDTH_WIDE = 'wide';
export const WIDTH_NARROW = 'narrow';

// The classes each choice puts on the header's, the list's and the foot's
// content, spelled out so Tailwind keeps them.
const CLASSES = {
    [WIDTH_FULL]: '',
    [WIDTH_WIDE]: 'mx-auto w-full max-w-[1200px]',
    [WIDTH_NARROW]: 'mx-auto w-full max-w-[900px]',
};

const listeners = new Set();
// chosen is the member's choice on this page, which lasts until it reloads
// where storage is unavailable.
let chosen = null;

/** width is the member's choice, Full, Wide or Narrow */
export function width() {
    let stored = chosen;
    try {
        stored = localStorage.getItem(KEY) ?? chosen;
    } catch {
        // Storage can be unavailable (private windows).
    }
    return stored in CLASSES ? stored : WIDTH_FULL;
}

export function setWidth(choice) {
    chosen = choice;
    try {
        localStorage.setItem(KEY, choice);
    } catch {
        // As above.
    }
    listeners.forEach((fn) => fn());
}

/** widthClass is the classes a choice puts on the content it holds */
export function widthClass(choice) {
    return CLASSES[choice] ?? '';
}

/** useWidth is the choice, kept current: read again as it starts
    listening, so a choice made before isn't missed */
export function useWidth() {
    const [choice, setChoice] = useState(width);
    useEffect(() => {
        const update = () => setChoice(width());
        update();
        listeners.add(update);
        return () => listeners.delete(update);
    }, []);
    return choice;
}

/** useWidthClass is the current choice's classes, kept current */
export function useWidthClass() {
    return widthClass(useWidth());
}
