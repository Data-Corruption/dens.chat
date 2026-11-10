// Appearance's sizes: how large a message's photos and videos show, and
// its YouTube players, on a wide window: Small, Medium, Large or Largest,
// by the width of a landscape one. Each still gives way to the pane, and
// a photo is never enlarged past its own pixels. The phone layout keeps
// Small, where the screen is the limit. The browser keeps each choice, as
// it keeps Density; until the member makes one, photos and videos are
// Medium and players Small.

import { useEffect, useState } from 'preact/hooks';
import { useNarrow } from './nav.js';

export const SIZES = ['small', 'medium', 'large', 'largest'];
const WIDTHS = { small: 400, medium: 640, large: 960, largest: 1280 };

/** MEDIA and PLAYERS are the two settings: a message's photos and videos,
    and its YouTube players */
export const MEDIA = { key: 'DENS_MEDIA_SIZE', first: 'medium' };
export const PLAYERS = { key: 'DENS_PLAYER_SIZE', first: 'small' };

const listeners = new Set();
// chosen are the member's choices on this page, which last until it
// reloads where storage is unavailable.
const chosen = {};

/** size is the member's choice for a setting */
export function size(setting) {
    let stored = chosen[setting.key];
    try {
        stored = localStorage.getItem(setting.key) ?? stored;
    } catch {
        // Storage can be unavailable (private windows).
    }
    return SIZES.includes(stored) ? stored : setting.first;
}

export function setSize(setting, choice) {
    chosen[setting.key] = choice;
    try {
        localStorage.setItem(setting.key, choice);
    } catch {
        // As above.
    }
    listeners.forEach((fn) => fn());
}

/** useSize is a setting's choice, kept current: read again as it starts
    listening, so a choice made before isn't missed */
export function useSize(setting) {
    const [choice, setChoice] = useState(() => size(setting));
    useEffect(() => {
        const update = () => setChoice(size(setting));
        update();
        listeners.add(update);
        return () => listeners.delete(update);
    }, [setting]);
    return choice;
}

/** useShownSize is the size a setting's things show at here: the choice,
    or Small on the phone layout */
export function useShownSize(setting) {
    const choice = useSize(setting);
    return useNarrow() ? 'small' : choice;
}

/** mediaBox is the box a photo or video fits at a size, four wide to three
    high; several in one message get a smaller one each */
export function mediaBox(choice, several) {
    const w = Math.round((WIDTHS[choice] || WIDTHS.small) * (several ? 0.6 : 1));
    return { w, h: Math.round(w * 0.75) };
}

// The players' classes, spelled out so Tailwind keeps them. A Short's box
// is as tall as a landscape player is wide, up to 720 pixels, a window's
// worth.
const LANDSCAPE = { small: 'w-[400px]', medium: 'w-[640px]', large: 'w-[960px]', largest: 'w-[1280px]' };
const PORTRAIT = { small: 'w-[225px]', medium: 'w-[360px]', large: 'w-[405px]', largest: 'w-[405px]' };

/** playerClass sizes a YouTube player's box before anything in it loads:
    16:9, or a Short's 9:16 */
export function playerClass(choice, short) {
    return short ? `aspect-[9/16] ${PORTRAIT[choice] || PORTRAIT.small}` : `aspect-video ${LANDSCAPE[choice] || LANDSCAPE.small}`;
}
