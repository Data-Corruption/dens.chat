// Appearance's Staff marks: the owner's crown and a moderator's shield
// beside their names in messages and in a call's rows, off until the
// member turns them on. The member list and the profile card show them
// whatever the member chose. The browser keeps the choice, as it keeps
// Density.

import { useEffect, useState } from 'preact/hooks';

const KEY = 'DENS_STAFF_MARKS';
const ON = 'on';

const listeners = new Set();
// chosen is the member's choice on this page, which lasts until it reloads
// where storage is unavailable.
let chosen = null;

/** staffMarks says whether staff's marks show in messages and calls */
export function staffMarks() {
    let stored = chosen;
    try {
        stored = localStorage.getItem(KEY) ?? chosen;
    } catch {
        // Storage can be unavailable (private windows).
    }
    return stored === ON;
}

export function setStaffMarks(on) {
    chosen = on ? ON : '';
    try {
        if (on) localStorage.setItem(KEY, ON);
        else localStorage.removeItem(KEY);
    } catch {
        // As above.
    }
    listeners.forEach((fn) => fn());
}

/** useStaffMarks is the choice, kept current: read again as it starts
    listening, so a choice made before isn't missed */
export function useStaffMarks() {
    const [on, setOn] = useState(staffMarks);
    useEffect(() => {
        const update = () => setOn(staffMarks());
        update();
        listeners.add(update);
        return () => listeners.delete(update);
    }, []);
    return on;
}
