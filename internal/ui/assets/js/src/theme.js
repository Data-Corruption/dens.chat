// Theme Management
// Every DaisyUI theme, chosen in the settings and remembered per browser;
// until one is chosen, the page follows the system's light or dark
// preference, with Light and Dark. The service hears the theme's
// background as it changes, for the Android app's bars (M13.3).

import { setBars } from './events.js';

const LIGHT_THEME = 'light';
const DARK_THEME = 'dark';
const THEME_KEY = 'DENS_THEME';

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

function stored() {
    try {
        return localStorage.getItem(THEME_KEY);
    } catch {
        return null;
    }
}

function store(theme) {
    try {
        if (theme) localStorage.setItem(THEME_KEY, theme);
        else localStorage.removeItem(THEME_KEY);
    } catch {
        // Storage can be unavailable (private windows); the theme still applies.
    }
}

const dark = () => window.matchMedia?.('(prefers-color-scheme: dark)');

/** The member's choice: a theme's name, or "system" */
export function themeChoice() {
    const t = stored();
    return THEMES.includes(t) ? t : 'system';
}

function apply() {
    const choice = themeChoice();
    const theme = choice === 'system' ? (dark()?.matches ? DARK_THEME : LIGHT_THEME) : choice;
    document.documentElement.setAttribute('data-theme', theme);
    tellBars();
}

// hex is a CSS color as #rrggbb, in sRGB, as a canvas draws it: a theme's
// colors are OKLCH, which Android takes no more than a hex's.
function hex(color) {
    const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
    if (!ctx) return '';
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

/** Chooses a theme by name, or "system" to follow the system again */
export function setTheme(choice) {
    store(choice === 'system' ? '' : choice);
    apply();
}

/** Apply the theme before the page renders, to avoid a flash, and follow
    the system's preference as it changes while that's the choice */
export function initTheme() {
    apply();
    dark()?.addEventListener?.('change', apply);
}
