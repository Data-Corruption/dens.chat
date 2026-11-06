// Appearance's Verified marks (M16.4): whether the check beside a member
// this member compared codes with shows beside their name, in the lists,
// a DM's header and a profile: always, while the pointer is over its row,
// or never; on a touch screen, which has no hover, shown or not. Where
// comparing is the subject, as in the codes and the call's lock, it says
// so whatever this is. The browser keeps the choice, as it keeps the
// theme; until the member makes one, it's On hover, and on a touch screen
// not shown.

import { useEffect, useState } from 'preact/hooks';
import { useCoarse } from './touch.js';

const KEY = 'DENS_VERIFIED_MARKS';
export const MARKS_ALWAYS = 'always';
export const MARKS_HOVER = 'hover';
export const MARKS_NEVER = 'never';

const listeners = new Set();
// chosen is the member's choice on this page, which lasts until it reloads
// where storage is unavailable.
let chosen = null;

/** verifiedMarks is the member's choice, Always, On hover or Never, or
    the default until they make one; touch says the screen has no hover,
    where On hover can't show a thing and so shows none */
export function verifiedMarks(touch) {
    let stored = chosen;
    try {
        stored = localStorage.getItem(KEY) ?? chosen;
    } catch {
        // Storage can be unavailable (private windows).
    }
    if (touch) return stored === MARKS_ALWAYS ? MARKS_ALWAYS : MARKS_NEVER;
    return stored === MARKS_ALWAYS || stored === MARKS_NEVER ? stored : MARKS_HOVER;
}

export function setVerifiedMarks(choice) {
    chosen = choice;
    try {
        localStorage.setItem(KEY, choice);
    } catch {
        // As above.
    }
    listeners.forEach((fn) => fn());
}

/** useVerifiedMarks is the choice for this screen, kept current: read
    again as it starts listening, so a choice made before isn't missed */
export function useVerifiedMarks() {
    const touch = useCoarse();
    const [choice, setChoice] = useState(() => verifiedMarks(touch));
    useEffect(() => {
        const update = () => setChoice(verifiedMarks(touch));
        update();
        listeners.add(update);
        return () => listeners.delete(update);
    }, [touch]);
    return choice;
}
