// Theme Management
// Every DaisyUI theme, chosen in the settings and remembered per browser;
// until one is chosen, the page follows the system's light or dark
// preference, with Light and Dark.

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
