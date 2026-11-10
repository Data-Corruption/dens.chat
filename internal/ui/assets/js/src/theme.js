// Theme Management
// Every DaisyUI theme, chosen in the settings and remembered per browser;
// until one is chosen, the page follows the system's light or dark
// preference, with Light and Dark. A theme of the member's own (see
// themes.js) is kept by the browser too, and shows as DaisyUI's variables
// set on the page's root, over Light or Dark for what it leaves. The
// service hears the theme's background as it changes, for the Android
// app's bars (M13.3).

import { setBars } from './events.js';
import { CARRIED, LIGHT, MOST_THEMES, PICKED, VARIABLES, cleanTheme, cornersOf, newID, readThemes, variables } from './themes.js';

const LIGHT_THEME = 'light';
const DARK_THEME = 'dark';
const THEME_KEY = 'DENS_THEME';
const THEMES_KEY = 'DENS_THEMES';
// A theme of the member's own is chosen by its id, after this.
const OWN = 'custom:';

// THEMES lists every theme the stylesheet carries, by name.
export const THEMES = [
    'abyss', 'acid', 'aqua', 'autumn', 'black', 'bumblebee', 'business', 'caramellatte', 'cmyk', 'coffee', 'corporate', 'cupcake',
    'cyberpunk', 'dark', 'dim', 'dracula', 'emerald', 'fantasy', 'forest', 'garden', 'halloween', 'lemonade', 'light', 'lofi', 'luxury',
    'night', 'nord', 'pastel', 'retro', 'silk', 'sunset', 'synthwave', 'valentine', 'winter', 'wireframe',
];

// NAMES are the themes' names where capitals alone won't do.
const NAMES = { caramellatte: 'Caramel latte', cmyk: 'CMYK', lofi: 'Lo-fi' };

/** A theme's name, for the menu */
export function themeName(theme) {
    return NAMES[theme] || theme[0].toUpperCase() + theme.slice(1);
}

// kept holds what the page set where storage is unavailable, which lasts
// until it reloads.
const kept = {};

function stored(key) {
    try {
        return localStorage.getItem(key) ?? kept[key] ?? null;
    } catch {
        return kept[key] ?? null;
    }
}

function store(key, value) {
    kept[key] = value || null;
    try {
        if (value) localStorage.setItem(key, value);
        else localStorage.removeItem(key);
    } catch {
        // Storage can be unavailable (private windows); the theme still applies.
    }
}

const dark = () => window.matchMedia?.('(prefers-color-scheme: dark)');

/** The member's own themes, each with its id, oldest first */
export function ownThemes() {
    return readThemes(stored(THEMES_KEY));
}

/** What chooses one of the member's own themes */
export function ownChoice(id) {
    return OWN + id;
}

/** The member's choice: a theme's name, one of their own's choice, or
    "system" */
export function themeChoice() {
    const t = stored(THEME_KEY) || '';
    if (t.startsWith(OWN)) return ownThemes().some((own) => ownChoice(own.id) === t) ? t : 'system';
    return THEMES.includes(t) ? t : 'system';
}

// show puts a theme on a document's root: one of DaisyUI's by name, or one
// of the member's own, as DaisyUI's variables over Light or Dark.
function show(root, name, own) {
    root.setAttribute('data-theme', own ? (own.dark ? DARK_THEME : LIGHT_THEME) : name);
    const vars = own ? variables(own) : {};
    for (const v of VARIABLES) {
        if (vars[v]) root.style.setProperty(v, vars[v]);
        else root.style.removeProperty(v);
    }
}

// previewed is a theme the editor shows while the member makes it.
let previewed = null;

// watchers hear of each change of the theme, and colors are the ones the
// page read from it since the last, undefined until it reads them.
const watchers = new Set();
let colors;

function apply() {
    const choice = themeChoice();
    const own = previewed || (choice.startsWith(OWN) && ownThemes().find((t) => ownChoice(t.id) === choice)) || null;
    show(document.documentElement, choice === 'system' ? (dark()?.matches ? DARK_THEME : LIGHT_THEME) : choice, own);
    colors = undefined;
    watchers.forEach((fn) => fn());
    tellBars();
}

/** Calls fn as the page's theme changes, until what it returns is called */
export function onTheme(fn) {
    watchers.add(fn);
    return () => watchers.delete(fn);
}

/** The theme's three backgrounds, base-100 to base-300, and its text, as
    #rrggbb, for what must read against them; null where the page can't
    read them */
export function themeColors() {
    if (colors === undefined) {
        colors = null;
        try {
            const style = getComputedStyle(document.documentElement);
            const backs = ['--color-base-100', '--color-base-200', '--color-base-300'].map((v) => hex(style.getPropertyValue(v)));
            const text = hex(style.getPropertyValue('--color-base-content'));
            if (text && backs.every(Boolean)) colors = { backs, text };
        } catch {
            // No page to read them from.
        }
    }
    return colors;
}

/** Shows a theme the member is making, without keeping it, or with null
    the theme they chose again */
export function previewTheme(theme) {
    // Its name may still be unwritten, which changes nothing it shows.
    previewed = theme && cleanTheme({ ...theme, name: 'preview' });
    apply();
}

/** Gives another window's document the page's theme, as a share's has */
export function dressTheme(doc) {
    const root = document.documentElement;
    const theme = root.getAttribute('data-theme');
    if (theme) doc.documentElement.setAttribute('data-theme', theme);
    for (const v of VARIABLES) {
        const value = root.style.getPropertyValue(v);
        if (value) doc.documentElement.style.setProperty(v, value);
    }
}

/** Keeps a theme of the member's own, in place of the one with id, or as
    a new one, and returns its id, or '' for one that isn't a theme or is
    one more than the browser keeps */
export function keepTheme(theme, id = '') {
    const clean = cleanTheme(theme);
    if (!clean) return '';
    const list = ownThemes();
    const at = list.findIndex((t) => t.id === id);
    if (at < 0 && list.length >= MOST_THEMES) return '';
    const next = { id: at < 0 ? newID() : id, ...clean };
    if (at < 0) list.push(next);
    else list[at] = next;
    store(THEMES_KEY, JSON.stringify(list));
    apply();
    return next.id;
}

/** Forgets one of the member's own themes; chosen, the page follows the
    system again */
export function forgetTheme(id) {
    const rest = ownThemes().filter((t) => t.id !== id);
    store(THEMES_KEY, rest.length ? JSON.stringify(rest) : '');
    if (stored(THEME_KEY) === ownChoice(id)) store(THEME_KEY, '');
    apply();
}

/** The theme the page shows, as one of the member's own would hold it,
    for a new one to start from: its colors read from DaisyUI's variables,
    whether it's dark, and its corners */
export function shownTheme(name) {
    const style = getComputedStyle(document.documentElement);
    // A field's radius, which buttons share, says most of a theme's
    // corners. DaisyUI writes it in rem; one in pixels is as many
    // sixteenths.
    const radius = style.getPropertyValue('--radius-field').trim();
    const rem = parseFloat(radius) / (radius.endsWith('px') ? parseFloat(style.fontSize) || 16 : 1);
    const theme = { name, dark: style.colorScheme.includes('dark'), corners: cornersOf(rem) };
    const from = { base: '--color-base-100', text: '--color-base-content' };
    for (const color of [...PICKED, ...CARRIED]) {
        // A color the canvas can't read is Light's.
        theme[color] = hex(style.getPropertyValue(from[color] || `--color-${color}`)) || LIGHT[color];
    }
    return cleanTheme(theme);
}

// hex is a CSS color as #rrggbb, in sRGB, as a canvas draws it: a theme's
// colors are OKLCH, which Android takes no more than a hex's.
function hex(color) {
    const ctx = document.createElement('canvas').getContext?.('2d', { willReadFrequently: true });
    if (!ctx || !color) return '';
    // A color the canvas can't read leaves the one before.
    ctx.fillStyle = '#010203';
    ctx.fillStyle = color;
    if (ctx.fillStyle === '#010203') return '';
    ctx.fillRect(0, 0, 1, 1);
    const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
    return `#${[r, g, b].map((v) => v.toString(16).padStart(2, '0')).join('')}`;
}

/** Tells the service the page's background, once there's a page to read
    it from */
export function tellBars() {
    if (!document.body) return;
    const color = hex(getComputedStyle(document.body).backgroundColor);
    if (color) setBars(color);
}

/** Chooses a theme by name, one of the member's own by its choice, or
    "system" to follow the system again */
export function setTheme(choice) {
    store(THEME_KEY, choice === 'system' ? '' : choice);
    apply();
}

/** Apply the theme before the page renders, to avoid a flash, and follow
    the system's preference as it changes while that's the choice */
export function initTheme() {
    apply();
    dark()?.addEventListener?.('change', apply);
}
