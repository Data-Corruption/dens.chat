// Appearance's File names on hover: a photo's or a video's name as
// the browser's tooltip while the pointer rests on it, on until the member
// turns it off. The browser keeps the choice, as it keeps Density.

import { useEffect, useState } from 'preact/hooks';

const KEY = 'DENS_FILE_NAMES';
const OFF = 'off';

const listeners = new Set();
// chosen is the member's choice on this page, which lasts until it reloads
// where storage is unavailable.
let chosen = null;

/** fileNames says whether a photo's or a video's name shows on hover */
export function fileNames() {
    let stored = chosen;
    try {
        stored = localStorage.getItem(KEY) ?? chosen;
    } catch {
        // Storage can be unavailable (private windows).
    }
    return stored !== OFF;
}

export function setFileNames(on) {
    chosen = on ? '' : OFF;
    try {
        if (on) localStorage.removeItem(KEY);
        else localStorage.setItem(KEY, OFF);
    } catch {
        // As above.
    }
    listeners.forEach((fn) => fn());
}

/** useFileNames is the choice, kept current */
export function useFileNames() {
    const [on, setOn] = useState(fileNames);
    useEffect(() => {
        const update = () => setOn(fileNames());
        update();
        listeners.add(update);
        return () => listeners.delete(update);
    }, []);
    return on;
}
