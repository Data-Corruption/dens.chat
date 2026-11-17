// A theme of the member's own: a name, Light or Dark, and DaisyUI's
// variables, each in a field of its own (FIELDS). It always holds ten
// colors, the background, the text, primary, secondary, accent, neutral
// and the four statuses, and the three radii. The rest it holds where it
// was given them, by a pasted block or the theme it started from: the two
// deeper backgrounds, each color's text, the two sizes, the border, depth
// and noise. What it leaves out derives here, the backgrounds and the
// texts, or is Light's or Dark's, which it shows over.
//
// A theme reaches the page as DaisyUI's variables, variables(), and
// another member as a block in the shape DaisyUI's theme generator
// writes, themeCode(). A block is a member's message, so readCode() reads
// each value it knows, a color as hex or oklch() and a number within the
// generator's range, and writes it out again itself: nothing pasted ever
// reaches a style as written. It reads v1.13.0's codes too.

/** The most themes a browser keeps, and the longest name, in characters */
export const MOST_THEMES = 20;
export const NAME_MOST = 40;

/** What a v1.13.0 code starts with */
export const CODE = 'dens-theme:';
const CODE_VERSION = '1';
// The longest paste read: a block the generator writes is about 1,300
// characters.
const PASTE_MOST = 8000;

// TINTS are the colors that carry text of their own.
const TINTS = ['primary', 'secondary', 'accent', 'neutral', 'info', 'success', 'warning', 'error'];

/** COLORS are the ten colors every theme holds, in a v1.13.0 code's order */
export const COLORS = ['base', 'text', ...TINTS];
const RADII = ['radiusSelector', 'radiusField', 'radiusBox'];

/** FIELDS are DaisyUI's variables a theme holds, by the field holding
    each, in the order its generator writes them */
export const FIELDS = {
    base: '--color-base-100',
    base200: '--color-base-200',
    base300: '--color-base-300',
    text: '--color-base-content',
    ...Object.fromEntries(TINTS.flatMap((t) => [[t, `--color-${t}`], [`${t}Content`, `--color-${t}-content`]])),
    radiusSelector: '--radius-selector',
    radiusField: '--radius-field',
    radiusBox: '--radius-box',
    sizeSelector: '--size-selector',
    sizeField: '--size-field',
    border: '--border',
    depth: '--depth',
    noise: '--noise',
};
const BY_VARIABLE = Object.fromEntries(Object.entries(FIELDS).map(([field, v]) => [v, field]));

/** VARIABLES are every variable a theme may set, each of which a page
    leaving the theme takes back */
export const VARIABLES = Object.values(FIELDS);

// NUMBERS are the fields that hold a number: its unit, and the least and
// most of it the generator gives.
const NUMBERS = {
    radiusSelector: ['rem', 0, 2],
    radiusField: ['rem', 0, 2],
    radiusBox: ['rem', 0, 2],
    sizeSelector: ['rem', 0.1875, 0.3125],
    sizeField: ['rem', 0.1875, 0.3125],
    border: ['px', 0.5, 2],
    depth: ['', 0, 1],
    noise: ['', 0, 1],
};

/** SHOWN are the numbers Light and Dark give, which a theme that holds
    none shows */
export const SHOWN = { radiusSelector: 0.5, radiusField: 0.25, radiusBox: 0.5, sizeSelector: 0.25, sizeField: 0.25, border: 1, depth: 1, noise: 0 };

/** DAISY are DaisyUI's Light's and Dark's colors, which a block leaving one
    out takes, as DaisyUI would under it */
export const DAISY = {
    light: {
        base: 'oklch(100% 0 0)', text: 'oklch(21% 0.006 285.885)', primary: 'oklch(45% 0.24 277.023)', secondary: 'oklch(65% 0.241 354.308)',
        accent: 'oklch(77% 0.152 181.912)', neutral: 'oklch(14% 0.005 285.823)', info: 'oklch(74% 0.16 232.661)', success: 'oklch(76% 0.177 163.223)',
        warning: 'oklch(82% 0.189 84.429)', error: 'oklch(71% 0.194 13.428)',
    },
    dark: {
        base: 'oklch(25.33% 0.016 252.42)', text: 'oklch(97.807% 0.029 256.847)', primary: 'oklch(58% 0.233 277.117)', secondary: 'oklch(65% 0.241 354.308)',
        accent: 'oklch(77% 0.152 181.912)', neutral: 'oklch(14% 0.005 285.823)', info: 'oklch(74% 0.16 232.661)', success: 'oklch(76% 0.177 163.223)',
        warning: 'oklch(82% 0.189 84.429)', error: 'oklch(71% 0.194 13.428)',
    },
};

// CORNERS are v1.13.0's corners by name, which a theme it kept names in
// place of the three radii, with each one's letter in its code.
const CORNERS = {
    square: ['s', { radiusSelector: 0, radiusField: 0, radiusBox: 0 }],
    rounded: ['r', { radiusSelector: 0.5, radiusField: 0.25, radiusBox: 0.5 }],
    round: ['o', { radiusSelector: 1, radiusField: 2, radiusBox: 1 }],
};

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

const NUM = String.raw`(\d+(?:\.\d*)?|\.\d+)`;
const OKLCH = new RegExp(String.raw`^oklch\(\s*${NUM}(%?)\s+${NUM}(%?)\s+(-?)${NUM}(deg)?\s*\)$`);

// number writes a number with at most places decimals, as a variable's
// value is written.
const number = (n, places = 3) => String(+n.toFixed(places));

/** readColor is a color as a theme holds it, #rrggbb or oklch(L% C H), from
    what a block or a member gave: #rrggbb, #rgb or oklch() with its
    lightness as a percentage or from 0 to 1; '' for anything else, a
    color with its alpha among them */
export function readColor(text) {
    if (typeof text !== 'string') return '';
    const t = text.trim().toLowerCase();
    if (t.startsWith('#')) return t.length === 4 || t.length === 7 ? hexColor(t) : '';
    const m = OKLCH.exec(t);
    if (!m) return '';
    const l = m[2] ? +m[1] : +m[1] * 100;
    // Chroma's percentage is of 0.4, as CSS has it.
    const c = m[4] ? (+m[3] * 0.4) / 100 : +m[3];
    const h = ((((m[5] ? -1 : 1) * +m[6]) % 360) + 360) % 360;
    if (!(l <= 100 && c <= 0.5)) return '';
    return `oklch(${number(l)}% ${number(c)} ${number(h)})`;
}

/** hexOf is a color a theme holds as #rrggbb, for what measures it: an
    oklch() color sRGB can't show loses chroma until it can */
export function hexOf(color) {
    if (color.startsWith('#')) return color;
    const [l, c, h] = color.slice(6, -1).split(' ').map(parseFloat);
    return fromOklch(l / 100, c, (h * Math.PI) / 180);
}

// readNumber is a number field's value from what a block gave, within the
// generator's range and in its unit, a bare 0 among them; null for
// anything else.
function readNumber(field, text) {
    const [unit, least, most] = NUMBERS[field];
    const m = /^(\d+(?:\.\d*)?|\.\d+)([a-z]*)$/.exec(String(text).trim().toLowerCase());
    if (!m || (m[2] !== unit && !(m[2] === '' && +m[1] === 0))) return null;
    const n = +(+m[1]).toFixed(5);
    return n >= least && n <= most ? n : null;
}

/** readValue is a field's value as a theme holds it, from a variable's
    text as a block or the page's style has it, or undefined */
export function readValue(field, text) {
    if (NUMBERS[field]) return readNumber(field, text) ?? undefined;
    return readColor(text) || undefined;
}

/** written is a field's value as its variable takes it */
function written(field, value) {
    return NUMBERS[field] ? `${value}${NUMBERS[field][0]}` : value;
}

/** cleanName is a theme's name as kept, trimmed, or '' for one that's
    empty, too long or not plain text */
export function cleanName(name) {
    if (typeof name !== 'string') return '';
    const n = name.trim();
    return n && [...n].length <= NAME_MOST && !UNNAMEABLE.test(n) ? n : '';
}

/** cleanTheme is a theme with only the fields a theme has, each as the page
    writes it, or null: what the browser kept, what the editor made and
    what a code said all pass it. A theme v1.13.0 kept names its corners,
    which give it its radii. */
export function cleanTheme(raw) {
    if (!raw || typeof raw !== 'object') return null;
    const name = cleanName(raw.name);
    if (!name || typeof raw.dark !== 'boolean') return null;
    const corners = Object.hasOwn(CORNERS, raw.corners) ? CORNERS[raw.corners][1] : {};
    const theme = { name, dark: raw.dark };
    for (const field of Object.keys(FIELDS)) {
        const value = raw[field] ?? corners[field];
        if (value === undefined) {
            if (COLORS.includes(field) || RADII.includes(field)) return null;
            continue;
        }
        const ok = NUMBERS[field] ? typeof value === 'number' && readNumber(field, written(field, value)) === value : !!value && readColor(value) === value;
        if (!ok) return null;
        theme[field] = value;
    }
    return theme;
}

/** editTheme is a theme with changes made in the editor: a background
    changed takes the deeper two it held away, and a color changed its
    text, so they derive from the new one */
export function editTheme(theme, changes) {
    const next = { ...theme, ...changes };
    if ('base' in changes && changes.base !== theme.base) {
        delete next.base200;
        delete next.base300;
    }
    for (const t of TINTS) {
        if (t in changes && changes[t] !== theme[t]) delete next[`${t}Content`];
    }
    return next;
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

// ON_BACKGROUND are the colors whose words the page draws on the
// background, as a link's or a mention's are primary's. A status is an
// icon and an edge beside words in the text's color, so a pale one costs
// nothing to read.
const ON_BACKGROUND = ['text', 'primary'];

/** lowContrast is how the text or primary reads on a theme's background
    where that's poorly, under 4.5 to 1 for the text and 3 to 1 for
    primary, or 0 where it reads well, or for any other color */
export function lowContrast(theme, field) {
    if (!ON_BACKGROUND.includes(field)) return 0;
    const ratio = contrast(hexOf(theme[field]), hexOf(theme.base));
    return ratio < (field === 'text' ? 4.5 : 3) ? ratio : 0;
}

// Variables ---------------------------------------------------------------

/** variables are DaisyUI's variables as a theme sets them, by name: what it
    holds, and the deeper backgrounds and each color's text where it holds
    none. What else it leaves, DaisyUI's Light or Dark gives. */
export function variables(theme) {
    const [base200, base300] = deeper(hexOf(theme.base));
    const derived = { base200, base300 };
    const vars = {};
    for (const [field, name] of Object.entries(FIELDS)) {
        let value = theme[field];
        if (value === undefined && field.endsWith('Content')) value = textOn(hexOf(theme[field.slice(0, -'Content'.length)]));
        value ??= derived[field];
        if (value !== undefined) vars[name] = written(field, value);
    }
    return vars;
}

/** numberShown is a number field's value as a theme shows it: its own, or
    Light's and Dark's */
export function numberShown(theme, field) {
    return theme[field] ?? SHOWN[field];
}

// Codes -------------------------------------------------------------------

// cssString writes a name as a CSS string.
const cssString = (text) => `"${text.replace(/["\\]/g, (c) => `\\${c}`)}"`;

/** themeCode is a theme as a block another member pastes, in the shape
    DaisyUI's theme generator writes, every variable in it */
export function themeCode(theme) {
    const vars = variables(theme);
    return [
        '@plugin "daisyui/theme" {',
        `  name: ${cssString(theme.name)};`,
        '  default: false;',
        '  prefersdark: false;',
        `  color-scheme: "${theme.dark ? 'dark' : 'light'}";`,
        ...Object.entries(FIELDS).map(([field, name]) => `  ${name}: ${vars[name] ?? written(field, SHOWN[field])};`),
        '}',
    ].join('\n');
}

/** readCode is the theme a block holds, or a v1.13.0 code, or null for
    anything else: a paste with nothing of a theme in it, or too long */
export function readCode(text) {
    const t = String(text ?? '').trim();
    if (!t || t.length > PASTE_MOST) return null;
    return t.toLowerCase().startsWith(CODE) ? readOldCode(t) : readBlock(t);
}

// declarations are a block's `key: value` pairs, from inside its braces, or
// the whole paste where it has none, its comments left out: split at each
// semicolon outside a quoted string, and ended by the closing brace.
function declarations(text) {
    const out = [];
    let at = '';
    let quote = '';
    let opened = false;
    for (let i = 0; i < text.length; i++) {
        const c = text[i];
        if (quote) {
            at += c;
            if (c === '\\' && i + 1 < text.length) at += text[++i];
            else if (c === quote) quote = '';
        } else if (c === '/' && text[i + 1] === '*') {
            const end = text.indexOf('*/', i + 2);
            if (end < 0) break;
            i = end + 1;
        } else if (c === '"' || c === "'") {
            quote = c;
            at += c;
        } else if (c === '{' && !opened) {
            // What stands before the brace names the block, as the
            // generator's @plugin line does.
            opened = true;
            out.length = 0;
            at = '';
        } else if (c === '}' || c === '{') {
            break;
        } else if (c === ';') {
            out.push(at);
            at = '';
        } else {
            at += c;
        }
    }
    out.push(at);
    return out.map((d) => d.trim()).filter(Boolean);
}

// cssText is a declaration's text: a quoted string's, with its quotes and
// backslashes as escapes, or a word as it is; '' for a string it can't
// read.
function cssText(value) {
    const q = value[0];
    if (q !== '"' && q !== "'") return value;
    if (value.length < 2 || value.at(-1) !== q) return '';
    let out = '';
    for (let i = 1; i < value.length - 1; i++) {
        let c = value[i];
        if (c === '\\') {
            c = value[++i];
            if (i >= value.length - 1 || !['"', "'", '\\'].includes(c)) return '';
        } else if (c === q) {
            return '';
        }
        out += c;
    }
    return out;
}

// readBlock reads each value of a block it knows, leaving out one it can't
// read; a color the block leaves out of the ten is Light's or Dark's, by
// its color-scheme, or by its background where it names none.
function readBlock(text) {
    const read = {};
    let name = '';
    let scheme = '';
    for (const d of declarations(text)) {
        const colon = d.indexOf(':');
        if (colon < 0) continue;
        const key = d.slice(0, colon).trim().toLowerCase();
        const value = d.slice(colon + 1).trim();
        if (key === 'name') name = cssText(value);
        else if (key === 'color-scheme') scheme = cssText(value).toLowerCase();
        else if (Object.hasOwn(BY_VARIABLE, key)) {
            const field = BY_VARIABLE[key];
            const v = readValue(field, value);
            if (v !== undefined) read[field] = v;
        }
    }
    if (!Object.keys(read).length) return null;
    const dark = scheme === 'dark' || (scheme !== 'light' && read.base !== undefined && oklch(hexOf(read.base))[0] < 0.5);
    return cleanTheme({ ...DAISY[dark ? 'dark' : 'light'], ...CORNERS.rounded[1], ...read, name: cleanName(name) || 'Pasted theme', dark });
}

// A v1.13.0 code is dens-theme:1.d.r.<colors>.<name>: its format's
// version, d for Dark or l for Light, the corners' letter, the ten colors'
// hex digits end to end, and the name's UTF-8 as hex digits.
const CODE_FORM = new RegExp(`^${CODE}${CODE_VERSION}\\.([dl])\\.([sro])\\.([0-9a-f]{${COLORS.length * 6}})\\.((?:[0-9a-f]{2}){1,${NAME_MOST * 4}})$`);

function readOldCode(code) {
    const m = CODE_FORM.exec(code.toLowerCase());
    if (!m) return null;
    let name;
    try {
        name = new TextDecoder('utf-8', { fatal: true }).decode(Uint8Array.from(m[4].match(/../g), (b) => parseInt(b, 16)));
    } catch {
        return null;
    }
    const theme = { name, dark: m[1] === 'd', corners: Object.keys(CORNERS).find((c) => CORNERS[c][0] === m[2]) };
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
