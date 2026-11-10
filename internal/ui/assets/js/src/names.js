// Name styles: a member's name in a font of the page's, a color
// or a gradient of two, which their den carries with them. A style is a
// member's, so it's read here as hostile: a font by its place in a list,
// and a color only as #rrggbb, so nothing else of it reaches a style.
//
// A color picked over one theme may vanish on another, so readable() moves
// it toward the theme's text until it reads against the theme's
// backgrounds, keeping its hue. Appearance's Name styles is the reader's
// say: colors and fonts, colors only, or off, which this browser keeps.

import { useEffect, useState } from 'preact/hooks';
import { contrast, fromOklch, oklch } from './themes.js';

/** FONTS are the fonts a name may show in, by the name a style carries
    and the one the picker shows, in the den's order */
export const FONTS = [
    ['pacifico', 'Pacifico'], ['fredoka', 'Fredoka'], ['zilla-slab', 'Zilla Slab'], ['space-mono', 'Space Mono'],
    ['orbitron', 'Orbitron'], ['pixelify-sans', 'Pixelify Sans'], ['bangers', 'Bangers'], ['cinzel', 'Cinzel'],
];
const FONT_NAMES = FONTS.map(([name]) => name);

const HEX = /^#[0-9a-f]{6}$/;
// A font as a den may send it: one of FONTS, or one a later Dens added,
// which shows as none.
const FONT_FORM = /^[a-z0-9-]{1,32}$/;

/** cleanStyle is a member's name style with only what a style has, each
    part checked: its font where the page has it, and its colors; null
    for none, or for anything that isn't one */
export function cleanStyle(raw) {
    if (!raw || typeof raw !== 'object') return null;
    const { font = '', color = '', color2 = '' } = raw;
    if (typeof font !== 'string' || (font && !FONT_FORM.test(font))) return null;
    if (typeof color !== 'string' || (color && !HEX.test(color))) return null;
    if (typeof color2 !== 'string' || (color2 && (!color || !HEX.test(color2)))) return null;
    const style = {};
    if (FONT_NAMES.includes(font)) style.font = font;
    if (color) style.color = color;
    if (color2) style.color2 = color2;
    return style.font || style.color ? style : null;
}

// Reading on every theme ---------------------------------------------------

/** A name's color reads at this contrast against its backgrounds, WCAG's
    for large text, or it's moved until it does */
export const READS = 3;
const STEP = 0.02;

/** readable is a color as it shows on a theme, by the theme's backgrounds
    and its text, each #rrggbb: itself where it reads against every
    background, or else its lightness moved a step at a time toward the
    text's side until it does, its hue kept; the text's own color if
    nothing on the way reads */
export function readable(color, backs, text) {
    const reads = (c) => backs.every((b) => contrast(c, b) >= READS);
    if (reads(color)) return color;
    const [l, c, h] = oklch(color);
    const up = oklch(text)[0] >= oklch(backs[0])[0];
    for (let light = l; light >= 0 && light <= 1;) {
        light += up ? STEP : -STEP;
        const moved = fromOklch(light, c, h);
        if (reads(moved)) return moved;
    }
    return text;
}

// shown keeps each color as a theme shows it, since every name on screen
// asks at each render.
const shown = new Map();

function onTheme(color, theme) {
    if (!theme) return color;
    const key = `${color} ${theme.backs.join(' ')} ${theme.text}`;
    let c = shown.get(key);
    if (!c) {
        if (shown.size > 500) shown.clear();
        c = readable(color, theme.backs, theme.text);
        shown.set(key, c);
    }
    return c;
}

// The reader's say ---------------------------------------------------------

const KEY = 'DENS_NAME_STYLES';
export const STYLES_ALL = 'all';
export const STYLES_COLORS = 'colors';
export const STYLES_OFF = 'off';

const listeners = new Set();
// chosen is the member's choice on this page, which lasts until it reloads
// where storage is unavailable.
let chosen = null;

/** nameStyles is the member's choice: colors and fonts until they make
    one, colors only, or off */
export function nameStyles() {
    let stored = chosen;
    try {
        stored = localStorage.getItem(KEY) ?? chosen;
    } catch {
        // Storage can be unavailable (private windows).
    }
    return stored === STYLES_COLORS || stored === STYLES_OFF ? stored : STYLES_ALL;
}

export function setNameStyles(choice) {
    chosen = choice;
    try {
        localStorage.setItem(KEY, choice);
    } catch {
        // As above.
    }
    listeners.forEach((fn) => fn());
}

/** useNameStyles is the choice, kept current: read again as it starts
    listening, so a choice made before isn't missed */
export function useNameStyles() {
    const [choice, setChoice] = useState(nameStyles);
    useEffect(() => {
        const update = () => setChoice(nameStyles());
        update();
        listeners.add(update);
        return () => listeners.delete(update);
    }, []);
    return choice;
}

/** nameLook is how a name shows to a reader: its font, '' for the page's,
    and its colors as the theme shows them, none, one, or two for a
    gradient; null for a plain name. choice is the reader's, and theme the
    page's colors, without which a color shows as picked. */
export function nameLook(style, choice, theme) {
    const s = cleanStyle(style);
    if (!s || choice === STYLES_OFF) return null;
    const font = choice === STYLES_COLORS ? '' : s.font || '';
    const colors = [s.color, s.color2].filter(Boolean).map((c) => onTheme(c, theme));
    return font || colors.length ? { font, colors } : null;
}

// Fonts ----------------------------------------------------------------------

// The characters each font's file covers, as Fontsource cuts its Latin
// subset, but the space: a browser fetches a face only for a name that
// has some, and measures a line by the first font that has a space, so
// every name's line stays the page's own, whatever its font.
const LATIN = 'U+0021-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2010-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FFFD';

const family = (font) => `Dens name ${font}`;

// faces are the fonts the browser was given, each under family(), or null
// until it's asked.
let faces = null;

function register() {
    faces = new Set();
    let paths = {};
    try {
        paths = JSON.parse(document.getElementById('app')?.dataset.fonts || '{}') || {};
    } catch {
        // No fonts, then.
    }
    for (const font of FONT_NAMES) {
        const path = paths[font];
        if (typeof path !== 'string' || !/^\/assets\/fonts\/[a-z0-9.-]+\.woff2$/.test(path)) continue;
        try {
            // One weight stands for all, so the browser doesn't thicken it
            // into a bold of its own.
            document.fonts.add(new FontFace(family(font), `url(${path}) format("woff2")`, { weight: '100 900', display: 'swap', unicodeRange: LATIN }));
            faces.add(font);
        } catch {
            // A browser without it shows the page's font.
        }
    }
}

/** fontFamily is the font-family that shows a font of FONTS, the page's
    own after it for what it lacks; '' where the page has no such font */
export function fontFamily(font) {
    if (!faces) register();
    return faces.has(font) ? `"${family(font)}", var(--default-font-family, ui-sans-serif, system-ui, sans-serif)` : '';
}
