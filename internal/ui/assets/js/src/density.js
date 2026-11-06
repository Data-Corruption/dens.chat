// Appearance's Message density (M17.2): Default, as messages always
// showed, or Compact, a line each of the time, the author's name and the
// text, which wraps under the name, on a phone too. The browser keeps the
// choice, as it keeps Animations; until the member makes one, it's
// Default.

import { useEffect, useState } from 'preact/hooks';

const KEY = 'DENS_DENSITY';
export const DENSITY_DEFAULT = 'default';
export const DENSITY_COMPACT = 'compact';

const listeners = new Set();
// chosen is the member's choice on this page, which lasts until it reloads
// where storage is unavailable.
let chosen = null;

/** density is the member's choice, Default or Compact */
export function density() {
    let stored = chosen;
    try {
        stored = localStorage.getItem(KEY) ?? chosen;
    } catch {
        // Storage can be unavailable (private windows).
    }
    return stored === DENSITY_COMPACT ? DENSITY_COMPACT : DENSITY_DEFAULT;
}

export function setDensity(choice) {
    chosen = choice;
    try {
        localStorage.setItem(KEY, choice);
    } catch {
        // As above.
    }
    listeners.forEach((fn) => fn());
}

/** useDensity is the choice, kept current: read again as it starts
    listening, so a choice made before isn't missed */
export function useDensity() {
    const [choice, setChoice] = useState(density);
    useEffect(() => {
        const update = () => setChoice(density());
        update();
        listeners.add(update);
        return () => listeners.delete(update);
    }, []);
    return choice;
}
