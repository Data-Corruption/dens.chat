// A theme of the member's own: a name, Light or Dark, the corners, and ten
// colors as #rrggbb. Five the member picks, the background, the text,
// primary, secondary and accent; five come from the theme it started
// from, since they carry meaning: neutral, info, success, warning and
// error. The rest derives from them here: the two deeper backgrounds, and
// each color's own text.
//
// A theme reaches the page as DaisyUI's variables, variables(), and
// another member as a code, themeCode(). A code is a member's message, so
// readCode() takes one only as a pattern of hex digits: nothing but a
// color it wrote out itself ever reaches a style.

/** The most themes a browser keeps, and the longest name, in characters */
export const MOST_THEMES = 20;
export const NAME_MOST = 40;

/** What a theme's code starts with, and its format's version */
export const CODE = 'dens-theme:';
const CODE_VERSION = '1';

/** The colors a theme holds, in a code's order: the member's five, then
    the five carried from the theme it started from */
export const PICKED = ['base', 'text', 'primary', 'secondary', 'accent'];
export const CARRIED = ['neutral', 'info', 'success', 'warning', 'error'];
const COLORS = [...PICKED, ...CARRIED];
// TINTS are the colors that carry text of their own.
const TINTS = [...PICKED.slice(2), ...CARRIED];

// CORNERS are the corners by name, each with DaisyUI's three radii, for
// selectors, fields and boxes, and its letter in a code: Rounded is
// Light's own, and Round the roundest of DaisyUI's themes.
const CORNERS = {
    square: ['s', '0rem', '0rem', '0rem'],
    rounded: ['r', '0.5rem', '0.25rem', '0.5rem'],
    round: ['o', '1rem', '2rem', '1rem'],
};
export const CORNER_NAMES = Object.keys(CORNERS);

/** LIGHT are DaisyUI's Light's colors, for a color the page can't read
    from the theme a new one starts from */
export const LIGHT = {
    base: '#ffffff', text: '#18181b', primary: '#422ad5', secondary: '#f43098', accent: '#00d3bb',
    neutral: '#09090b', info: '#00bafe', success: '#00d390', warning: '#fcb700', error: '#ff637d',
};

const HEX = /^#[0-9a-f]{6}$/;
// A name is text for a label: no control characters, line breaks or
// overrides of its direction.
const UNNAMEABLE = /[\p{Cc}\u2028\u2029\u202a-\u202e\u2066-\u2069]/u;

/** hexColor reads what a member typed as a color, #rrggbb with or without
    its #, or the three-digit form, and returns it as #rrggbb, or '' */
export function hexColor(text) {
    const t = String(text).trim().toLowerCase().replace(/^#/, '');
    if (/^[0-9a-f]{6}$/.test(t)) return `#${t}`;
    if (/^[0-9a-f]{3}$/.test(t)) return `#${[...t].map((c) => c + c).join('')}`;
    return '';
}

/** cleanName is a theme's name as kept, trimmed, or '' for one that's
    empty, too long or not plain text */
export function cleanName(name) {
    if (typeof name !== 'string') return '';
    const n = name.trim();
    return n && [...n].length <= NAME_MOST && !UNNAMEABLE.test(n) ? n : '';
}

/** cleanTheme is a theme with only the fields a theme has, each checked,
    or null: what the browser kept and what a code said both pass it */
export function cleanTheme(raw) {
    if (!raw || typeof raw !== 'object') return null;
    const name = cleanName(raw.name);
    if (!name || typeof raw.dark !== 'boolean' || !Object.hasOwn(CORNERS, raw.corners)) return null;
    const theme = { name, dark: raw.dark, corners: raw.corners };
    for (const color of COLORS) {
        if (typeof raw[color] !== 'string' || !HEX.test(raw[color])) return null;
        theme[color] = raw[color];
    }
    return theme;
}

// Colors ------------------------------------------------------------------

// A color's lightness, chroma and hue are OKLCH's, as DaisyUI's themes are
// written, by Björn Ottosson's matrices.

function rgb(hex) {
    return [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
}

const linear = (v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
const gamma = (v) => (v <= 0.0031308 ? v * 12.92 : 1.055 * v ** (1 / 2.4) - 0.055);

/** oklch is a #rrggbb color's lightness, from 0 to 1, chroma and hue */
export function oklch(hex) {
    const [r, g, b] = rgb(hex).map(linear);
    const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
    const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
    const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
    const a = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
    const bb = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
    return [0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s, Math.hypot(a, bb), Math.atan2(bb, a)];
}

function channels(l, c, h) {
    const a = c * Math.cos(h);
    const b = c * Math.sin(h);
    const lc = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
    const mc = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
    const sc = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
    return [
        4.0767416621 * lc - 3.3077115913 * mc + 0.2309699292 * sc,
        -1.2684380046 * lc + 2.6097574011 * mc - 0.3413193965 * sc,
        -0.0041960863 * lc - 0.7034186147 * mc + 1.707614701 * sc,
    ];
}

/** fromOklch is the #rrggbb nearest a lightness, chroma and hue: a color
    sRGB can't show loses chroma until it can, keeping its hue */
export function fromOklch(l, c, h) {
    const light = Math.min(1, Math.max(0, l));
    let lin = channels(light, c, h);
    for (let chroma = c; chroma > 0.0005 && lin.some((v) => v < -0.0005 || v > 1.0005);) {
        chroma *= 0.95;
        lin = channels(light, chroma, h);
    }
    return `#${lin.map((v) => Math.round(gamma(Math.min(1, Math.max(0, v))) * 255).toString(16).padStart(2, '0')).join('')}`;
}

/** contrast is WCAG's contrast ratio of two #rrggbb colors, from 1 to 21 */
export function contrast(one, other) {
    const lum = (hex) => {
        const [r, g, b] = rgb(hex).map(linear);
        return 0.2126 * r + 0.7152 * g + 0.0722 * b;
    };
    const [hi, lo] = [lum(one), lum(other)].sort((x, y) => y - x);
    return (hi + 0.05) / (lo + 0.05);
}

/** deeper are the two backgrounds past a theme's own, base-200 and
    base-300: each a step darker, as DaisyUI's themes go, or lighter under
    a background too dark to show a darker one, as its Black goes */
export function deeper(base) {
    const [l, c, h] = oklch(base);
    if (l >= 0.2) return [fromOklch(l - 0.02, c, h), fromOklch(l - 0.045, c, h)];
    const up = l + 0.04 + (0.2 - l) * 0.75;
    return [fromOklch(up, c, h), fromOklch(up + 0.03, c, h)];
}

/** textOn is the text a color carries, as a button's label: near white or
    near black with a little of the color's hue, whichever reads better */
export function textOn(color) {
    const [, c, h] = oklch(color);
    const light = fromOklch(0.98, Math.min(c, 0.02), h);
    const dark = fromOklch(0.2, Math.min(c, 0.04), h);
    return contrast(light, color) >= contrast(dark, color) ? light : dark;
}

// Variables ---------------------------------------------------------------

/** VARIABLES are DaisyUI's variables a theme sets, each of which a page
    leaving the theme takes back */
export const VARIABLES = [
    '--color-base-100', '--color-base-200', '--color-base-300', '--color-base-content',
    ...TINTS.flatMap((name) => [`--color-${name}`, `--color-${name}-content`]),
    '--radius-selector', '--radius-field', '--radius-box',
];

/** variables are DaisyUI's variables as a theme sets them, by name: its
    colors and those derived from them, and its corners. What it leaves,
    DaisyUI's Light or Dark gives. */
export function variables(theme) {
    const [base200, base300] = deeper(theme.base);
    const [, selector, field, box] = CORNERS[theme.corners];
    const vars = {
        '--color-base-100': theme.base,
        '--color-base-200': base200,
        '--color-base-300': base300,
        '--color-base-content': theme.text,
        '--radius-selector': selector,
        '--radius-field': field,
        '--radius-box': box,
    };
    for (const name of TINTS) {
        vars[`--color-${name}`] = theme[name];
        vars[`--color-${name}-content`] = textOn(theme[name]);
    }
    return vars;
}

/** cornersOf names the corners nearest a field's radius in rem, as a theme
    being started from has it */
export function cornersOf(rem) {
    if (!(rem > 0.01)) return 'square';
    return rem >= 1 ? 'round' : 'rounded';
}

// Codes -------------------------------------------------------------------

// A code is dens-theme:1.d.r.<colors>.<name>: its format's version, d for
// Dark or l for Light, the corners' letter, the ten colors' hex digits end
// to end, and the name's UTF-8 as hex digits, so no character of it means
// anything to a message's markdown.
const CODE_FORM = new RegExp(`^${CODE}${CODE_VERSION}\\.([dl])\\.([sro])\\.([0-9a-f]{${COLORS.length * 6}})\\.((?:[0-9a-f]{2}){1,${NAME_MOST * 4}})$`);

/** themeCode is a theme as a code another member pastes */
export function themeCode(theme) {
    const name = [...new TextEncoder().encode(theme.name)].map((b) => b.toString(16).padStart(2, '0')).join('');
    return `${CODE}${CODE_VERSION}.${theme.dark ? 'd' : 'l'}.${CORNERS[theme.corners][0]}.${COLORS.map((c) => theme[c].slice(1)).join('')}.${name}`;
}

/** readCode is the theme a code holds, or null for anything else: a field
    more or fewer, a color that isn't hex digits, or a name that's too
    long, isn't UTF-8 or isn't plain text */
export function readCode(code) {
    const m = CODE_FORM.exec(String(code).trim().toLowerCase());
    if (!m) return null;
    let name;
    try {
        name = new TextDecoder('utf-8', { fatal: true }).decode(Uint8Array.from(m[4].match(/../g), (b) => parseInt(b, 16)));
    } catch {
        return null;
    }
    const theme = { name, dark: m[1] === 'd', corners: CORNER_NAMES.find((c) => CORNERS[c][0] === m[2]) };
    COLORS.forEach((color, i) => {
        theme[color] = `#${m[3].slice(i * 6, i * 6 + 6)}`;
    });
    return cleanTheme(theme);
}

// Kept --------------------------------------------------------------------

const ID = /^[a-z0-9]{8}$/;

/** readThemes is the member's themes as the browser kept them, each with
    its id: what isn't a list of themes reads as none, and what isn't a
    theme is left out */
export function readThemes(stored) {
    let list;
    try {
        list = JSON.parse(stored || '[]');
    } catch {
        return [];
    }
    if (!Array.isArray(list)) return [];
    const out = [];
    for (const raw of list) {
        const theme = cleanTheme(raw);
        if (theme && ID.test(raw.id) && !out.some((t) => t.id === raw.id) && out.length < MOST_THEMES) out.push({ id: raw.id, ...theme });
    }
    return out;
}

/** newID is an id for a theme the browser is about to keep */
export function newID() {
    const bytes = crypto.getRandomValues(new Uint8Array(8));
    return [...bytes].map((b) => (b % 36).toString(36)).join('');
}
