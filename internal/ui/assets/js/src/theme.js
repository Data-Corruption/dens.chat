// Theme Management
// Dark/light switching, remembered per browser, defaulting to the system preference

const LIGHT_THEME = 'nord';
const DARK_THEME = 'forest';
const THEME_KEY = 'DENS_THEME';

function stored() {
    try {
        return localStorage.getItem(THEME_KEY);
    } catch {
        return null;
    }
}

function store(theme) {
    try {
        localStorage.setItem(THEME_KEY, theme);
    } catch {
        // Storage can be unavailable (private windows); the theme still applies.
    }
}

/** Current theme, defaulting to the system preference */
export function getTheme() {
    return stored() ||
        (window.matchMedia?.('(prefers-color-scheme: dark)').matches ? DARK_THEME : LIGHT_THEME);
}

export function setTheme(theme) {
    store(theme);
    document.documentElement.setAttribute('data-theme', theme);
}

export function toggleTheme() {
    setTheme(getTheme() === DARK_THEME ? LIGHT_THEME : DARK_THEME);
}

/** Apply the theme before the page renders, to avoid a flash */
export function initTheme() {
    document.documentElement.setAttribute('data-theme', getTheme());
}
