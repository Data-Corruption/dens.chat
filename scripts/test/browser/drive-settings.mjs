// The settings check, as alice, the den's owner, in the
// browser named at a desktop's width: every section of the settings and
// of Manage opened from the list down the side, each with the layout
// audit, and every hint in it read as one line, a hint that wrapped
// failing it, Voice with push to talk's release delay too, and
// Relay, its Audio relay card among it; then
// Appearance's sizes: a photo at Medium until she chooses, at Large and
// Largest the photo itself drawn over the den's preview, and at Small its
// preview again; a YouTube player at Small, then Large with the larger
// cover; neither moving the other, nor running past the pane. Then
// a theme's code copied and pasted into bob's browser, which then wears
// the same, a stranger's codes refused there with nothing reaching a
// style; and a picture behind the chat: a PNG set, found behind
// general's list under the dim, by the screen's own pixels, in a second
// browser paired with her Dens too, dimmed further, a GIF refused, and
// removed from both. Staff marks, off until she turns them on,
// the crown then on her messages and always in the member list; the bars
// on a green theme of her own, their hue the page's; and Hover, how far a
// message under the pointer covers the picture, in both browsers.
// Each section held to the one frame the settings and Manage
// share: its groups cards, one toggle size and regular labels, every
// button one of the page's kinds by the layout audit; Manage holding what
// staff manage, the owner's Events among it, bob found in its Members by
// two letters and his row's menu opened; and her own sections of the
// den, from the cog on its page, as its group above the app's, this
// device renamed from its row in Devices and read there. Then
// the code a block in the generator's shape; a stranger's blocks read for
// what the page reads alone; and a v1.13.0 code read as it was.
// Appearance as a page that takes the window, Back going to the
// section open before; its preview following a theme chosen, a name
// style, Density, the call timer, the sizes and file names on hover, and
// holding its place as the middle column scrolls; and at a desktop's
// width, the middle one and a phone's: a built-in theme chosen, the
// preview in its colors while the page keeps its own, worn by Use and
// marked in the list, copied by Customize, edited against the preview, a
// color too near the background marked, the edits waiting for Save, then
// worn; and DaisyUI's generator's block pasted, the preview setting every
// variable as it gives it, then the page.
//
//   run.sh settings chromium|firefox [PART...]
//
// PART is sections, preview, sizes, themes, codes or background; all unless given.
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import zlib from 'node:zlib';
import { ALICE, APPEARANCE, BOB, HOME, MANAGE, SETTINGS, checks, closeSettings, launch, lib, openAppearance, openManage, openSettings, pair, paneTab, waitFor } from './browsers.mjs';
import { audits, helpers } from './screens.mjs';

const [, , which, ...parts] = process.argv;
const runs = (part) => parts.length === 0 || parts.includes(part);
const { DEN: den, GENERAL: general, PHOTO: photo } = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);

// Wide enough for Wide's 1,200 pixels to bind inside the channel's pane.
const b = await launch(which, which === 'chromium' ? 9571 : 9572, { size: [1900, 1000], hover: true });
const out = `${HOME}/shot-settings/${which}`;
fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });
const h = helpers(b, `http://localhost:${ALICE.port}`, out);
const PANEL = `document.querySelector('[role=dialog] [data-panel]')`;

// WRAPPED names the hints shown in the open dialog that run past a line:
// each paragraph of the panel, measured against its own line height.
const WRAPPED = `(() => {
    const out = [];
    for (const p of document.querySelectorAll('[role=dialog] [data-panel] p')) {
        if (p.closest('[role=tooltip], [inert]')) continue;
        const s = getComputedStyle(p);
        if (s.display === 'none' || !p.getClientRects().length) continue;
        const lh = parseFloat(s.lineHeight) || parseFloat(s.fontSize) * 1.5;
        const lines = p.getBoundingClientRect().height / lh;
        if (lines > 1.5) out.push(JSON.stringify(p.textContent.trim().slice(0, 70)) + ' (' + Math.round(lines) + ' lines)');
    }
    return out;
})()`;
// FRAME reads a section as the one frame shows it: what stands in
// its panel outside a card, the toggles' heights and the labels' weights.
const FRAME = `JSON.stringify((() => {
    const panel = document.querySelector('[role=dialog] [data-panel]');
    const seen = (e) => e.getClientRects().length > 0;
    return {
        nav: !!document.querySelector('[role=dialog] nav[aria-label=Sections]'),
        loose: [...panel.children].filter((e) => !e.classList.contains('card') && !e.matches('[data-notice], .loading'))
            .map((e) => e.localName + '.' + [...e.classList].slice(0, 3).join('.')),
        toggles: [...new Set([...panel.querySelectorAll('input.toggle')].filter(seen).map((t) => Math.round(t.getBoundingClientRect().height)))],
        labels: [...new Set([...panel.querySelectorAll('legend')].filter(seen).map((l) => getComputedStyle(l).fontWeight))],
    };
})())`;
// frame reports a section held to the one frame: its groups cards, its
// toggles one size and its labels regular.
const frame = (name, read) => {
    const f = JSON.parse(read);
    c.report(f.nav && !f.loose.length && f.toggles.length <= 1 && f.labels.every((w) => w === '400'),
        `${name} is in the one frame, its groups cards, ${f.toggles.length ? `its toggles ${f.toggles.join('/')} pixels` : 'no toggles'}, its labels regular: ${read}`);
};
const sections = (frame) => b.eval(`[...document.querySelectorAll('${frame} nav button')].map((x) => x.textContent.trim() + (x.getAttribute('aria-current') === 'page' ? '*' : '')).join('|')`);
const slug = (s) => s.toLowerCase().replace(/[^a-z]+/g, '-');

// chunk is a PNG's chunk, and png a picture of one color, written out.
function chunk(type, data) {
    const body = Buffer.concat([Buffer.from(type), data]);
    const out = Buffer.alloc(body.length + 8);
    out.writeUInt32BE(data.length, 0);
    body.copy(out, 4);
    out.writeUInt32BE(zlib.crc32(body), body.length + 4);
    return out;
}
function png(file, w, h, [r, g, b]) {
    const header = Buffer.alloc(13);
    header.writeUInt32BE(w, 0);
    header.writeUInt32BE(h, 4);
    header.set([8, 2, 0, 0, 0], 8);
    const row = Buffer.concat([Buffer.from([0]), Buffer.alloc(w * 3).map((_, i) => [r, g, b][i % 3])]);
    const pixels = zlib.deflateSync(Buffer.concat(Array.from({ length: h }, () => row)));
    fs.writeFileSync(file, Buffer.concat([Buffer.from('\x89PNG\r\n\x1a\n', 'latin1'), chunk('IHDR', header), chunk('tEXt', Buffer.from('Comment\0SecretText')),
        chunk('IDAT', pixels), chunk('IEND', Buffer.alloc(0))]));
    return file;
}

// pixel reads a point of a screenshot, a PNG of 8 bits a channel, as the
// browsers take them, as [r, g, b].
function pixel(file, x, y) {
    const data = fs.readFileSync(file);
    const w = data.readUInt32BE(16);
    const n = { 2: 3, 6: 4 }[data[25]];
    if (data[24] !== 8 || !n || data[28] !== 0) throw new Error(`a screenshot of another kind: ${data[24]} bits, color type ${data[25]}`);
    const idat = [];
    for (let at = 8; at < data.length;) {
        const len = data.readUInt32BE(at);
        if (data.toString('latin1', at + 4, at + 8) === 'IDAT') idat.push(data.subarray(at + 8, at + 8 + len));
        at += 12 + len;
    }
    const raw = zlib.inflateSync(Buffer.concat(idat));
    const stride = w * n;
    let prev = Buffer.alloc(stride);
    for (let row = 0; row <= y; row++) {
        const filter = raw[row * (stride + 1)];
        const line = Buffer.from(raw.subarray(row * (stride + 1) + 1, (row + 1) * (stride + 1)));
        for (let i = 0; i < stride; i++) {
            const a = i >= n ? line[i - n] : 0;
            const up = prev[i];
            const c = i >= n ? prev[i - n] : 0;
            const p = a + up - c;
            const paeth = Math.abs(p - a) <= Math.abs(p - up) && Math.abs(p - a) <= Math.abs(p - c) ? a : Math.abs(p - up) <= Math.abs(p - c) ? up : c;
            line[i] = (line[i] + [0, a, up, (a + up) >> 1, paeth][filter]) & 255;
        }
        prev = line;
    }
    return [...prev.subarray(x * n, x * n + 3)];
}

// What a page shows, read as #rrggbb through a canvas: a color as CSS has
// it, an element's background or text, and one of the root's variables.
const HEX = `((c) => { const x = document.createElement('canvas').getContext('2d', { willReadFrequently: true }); x.fillStyle = c; x.fillRect(0, 0, 1, 1);
    const d = x.getImageData(0, 0, 1, 1).data; return '#' + [d[0], d[1], d[2]].map((v) => v.toString(16).padStart(2, '0')).join(''); })`;
const WEARS = `JSON.stringify((() => {
    const hex = ${HEX};
    const root = getComputedStyle(document.documentElement);
    const of = (v) => hex(root.getPropertyValue(v));
    const primary = [...document.querySelectorAll('.btn-primary')].find((x) => !x.disabled && x.getClientRects().length);
    return { theme: document.documentElement.dataset.theme, page: hex(getComputedStyle(document.body).backgroundColor), text: hex(getComputedStyle(document.body).color),
        base: of('--color-base-100'), base200: of('--color-base-200'), base300: of('--color-base-300'), primary: of('--color-primary'),
        secondary: of('--color-secondary'), accent: of('--color-accent'), error: of('--color-error'),
        button: primary ? hex(getComputedStyle(primary).backgroundColor) : '', radius: primary ? getComputedStyle(primary).borderTopLeftRadius : '',
        radiusField: root.getPropertyValue('--radius-field').trim(), border: root.getPropertyValue('--border').trim(), depth: root.getPropertyValue('--depth').trim(),
        set: document.documentElement.style.length, styled: document.documentElement.getAttribute('style') || '' };
})())`;
const wears = async (on) => JSON.parse(await on.eval(WEARS));
// Appearance's columns: the list of themes, the editor of the one
// shown, the bar of its unsaved edits, the middle column's panel, and the
// preview; and what the preview wears: its box's background and text, its
// variables, its channel list's background, and its primary button's
// color, corners and border.
const LIST = `document.querySelector('[data-themes]')`;
const EDITOR = `document.querySelector('[data-theme-editor]')`;
const BAR = `document.querySelector('[data-theme-bar]')`;
const TABPANEL = `document.querySelector('[role=dialog] [role=tabpanel]')`;
const PREVIEW = `document.querySelector('[data-theme-preview]')`;
const PREVIEW_WEARS = `JSON.stringify((() => {
    const hex = ${HEX};
    const box = ${PREVIEW};
    const style = getComputedStyle(box);
    const of = (v) => hex(style.getPropertyValue(v));
    const primary = box.querySelector('.btn-primary');
    const p = getComputedStyle(primary);
    return { theme: box.dataset.theme, page: hex(style.backgroundColor), text: hex(style.color), base200: of('--color-base-200'), base300: of('--color-base-300'),
        primary: of('--color-primary'), secondary: of('--color-secondary'), accent: of('--color-accent'), error: of('--color-error'),
        list: hex(getComputedStyle(box.querySelector('[data-preview-list]')).backgroundColor),
        button: hex(p.backgroundColor), radius: p.borderTopLeftRadius, borderWidth: p.borderTopWidth };
})())`;
const previewWears = async (on) => JSON.parse(await on.eval(PREVIEW_WEARS));
// STYLED is the root's style as a theme of the member's own leaves it:
// DaisyUI's variables alone, each a color or a number the page wrote.
const STYLED = /^(--[a-z0-9-]+: (#[0-9a-f]{6}|oklch\([0-9.]+% [0-9.]+ [0-9.]+\)|[0-9.]+(rem|px)?);\s?)+$/;
// PASTE is the field a theme's code is pasted into.
const PASTE = '[data-themes] form textarea';
// GENERATOR is a block as DaisyUI's theme generator copies it, its line
// before it among it, and OLD_CODE a v1.13.0 code, Plum, on Dark.
const GENERATOR = `
@plugin "daisyui/theme" {
  name: "mytheme";
  default: false;
  prefersdark: false;
  color-scheme: "light";
  --color-base-100: oklch(97.788% 0.004 56.375);
  --color-base-200: oklch(93.982% 0.007 61.449);
  --color-base-300: oklch(91.586% 0.006 53.44);
  --color-base-content: oklch(23.574% 0.066 313.189);
  --color-primary: oklch(85% 0.138 181.071);
  --color-primary-content: oklch(43% 0.078 188.216);
  --color-secondary: oklch(89% 0.061 343.231);
  --color-secondary-content: oklch(45% 0.187 3.815);
  --color-accent: oklch(90% 0.076 70.697);
  --color-accent-content: oklch(47% 0.157 37.304);
  --color-neutral: oklch(27% 0.006 286.033);
  --color-neutral-content: oklch(92% 0.004 286.32);
  --color-info: oklch(68% 0.169 237.323);
  --color-info-content: oklch(29% 0.066 243.157);
  --color-success: oklch(69% 0.17 162.48);
  --color-success-content: oklch(26% 0.051 172.552);
  --color-warning: oklch(79% 0.184 86.047);
  --color-warning-content: oklch(28% 0.066 53.813);
  --color-error: oklch(64% 0.246 16.439);
  --color-error-content: oklch(27% 0.105 12.094);
  --radius-selector: 1rem;
  --radius-field: 0.75rem;
  --radius-box: 2rem;
  --size-selector: 0.21875rem;
  --size-field: 0.28125rem;
  --border: 1.5px;
  --depth: 0;
  --noise: 1;
}
`;
const OLD_CODE = 'dens-theme:1.d.r.2b1d3aeae0f5c084fc7dd3fcfbbf2409090b00bafe00d390fcb700ff627d.506c756d';
// hue is a color's hue in OKLCH, in degrees, from its #rrggbb.
function hue(hex) {
    const lin = (v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
    const [r, g, bl] = [1, 3, 5].map((i) => lin(parseInt(hex.slice(i, i + 2), 16) / 255));
    const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * bl);
    const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * bl);
    const t = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * bl);
    return ((Math.atan2(0.0259040371 * l + 0.7827717662 * m - 0.808675766 * t, 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * t) * 180) / Math.PI + 360) % 360;
}
const apart = (x, y) => Math.min(Math.abs(x - y), 360 - Math.abs(x - y));
const q = JSON.stringify;
// set types into a field the page controls, as a member's typing or
// picking would: the value, then the event Preact listens for.
const set = (on, selector, value, events = ['input']) => on.eval(`(() => {
    const el = document.querySelector(${q(selector)});
    Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), 'value').set.call(el, ${q(value)});
    for (const e of ${q(events)}) el.dispatchEvent(new Event(e, { bubbles: true }));
    return true;
})()`);
const button = (text, within = 'document') => `[...${within}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${q(text)})`;
const press = async (on, text, within) => {
    await waitFor(() => on.eval(`!!${button(text, within)} && !${button(text, within)}.disabled`), `the ${text} button`);
    await on.eval(`${button(text, within)}.click(), true`);
    await sleep(250);
};
const choose = (on, group, text) => on.eval(`document.querySelector('[${group}] input[aria-label=${q(text)}]').click(), true`);
// toList and toEditor show the list or the editor, which share a column
// under a large window, and the screen on a phone.
const toList = async (on) => {
    await on.eval(`(document.querySelector('[data-foot-tab=themes]')?.click(), true)`);
    await waitFor(() => on.eval(`!!${LIST}`), 'the themes');
};
const toEditor = async (on) => {
    await on.eval(`(document.querySelector('[data-foot-tab=editor]')?.click(), true)`);
    if (!(await on.eval(`!!${EDITOR}`))) await on.eval(`(${paneTab('Theme')}?.click(), true)`);
    await waitFor(() => on.eval(`!!${EDITOR}`), 'the editor');
};
// listed reads the list: its rows by their values, the one checked, the
// one worn, and the member's own by name, * checked, + worn.
const listed = async (on) => {
    await toList(on);
    return JSON.parse(await on.eval(`JSON.stringify((() => {
        const rows = [...document.querySelectorAll('[data-theme-row]')];
        const name = (r) => r.querySelector('.truncate').textContent.trim();
        return { picked: rows.filter((r) => r.querySelector('input').checked).map((r) => r.dataset.themeRow).join(), worn: rows.filter((r) => 'worn' in r.dataset).map((r) => r.dataset.themeRow).join(),
            own: [...document.querySelectorAll('[role=radiogroup][aria-label="My themes"] [data-theme-row]')].map((r) => name(r) + (r.querySelector('input').checked ? '*' : '') + ('worn' in r.dataset ? '+' : '')).join('|') };
    })())`));
};
const ownNames = async (on) => (await listed(on)).own;
// pick chooses a theme in the list by its row's value, and shows the
// editor.
const pick = async (on, value) => {
    await toList(on);
    await on.eval(`document.querySelector('[data-theme-row=${q(value)}] input').click(), true`);
    await toEditor(on);
    await sleep(250);
};
// color sets one of the theme's colors as a member types it: its tile
// chosen, then the field under the tiles.
const color = async (on, field, label, value) => {
    await on.eval(`document.querySelector('[data-color-tile=${field}]').click(), true`);
    await waitFor(() => on.eval(`!!document.querySelector('[data-color="${label}"]')`), `${label}'s field`);
    await set(on, `[data-color="${label}"] input[type=text]`, value);
};
// PROBE reads a built-in theme's colors from a box wearing it.
const PROBE = (theme) => `JSON.stringify((() => { const hex = ${HEX}; const box = document.createElement('div'); box.dataset.theme = ${q(theme)}; document.body.append(box);
    const s = getComputedStyle(box); const of = (v) => hex(s.getPropertyValue(v));
    const out = { base: of('--color-base-100'), text: of('--color-base-content'), primary: of('--color-primary'), secondary: of('--color-secondary'), error: of('--color-error') };
    box.remove(); return out; })())`;
// pickNamed chooses one of the member's own themes by its name.
const pickNamed = async (on, name) => {
    await toList(on);
    await on.eval(`[...document.querySelectorAll('[data-theme-row]')].find((r) => r.querySelector('.truncate').textContent.trim() === ${q(name)}).querySelector('input').click(), true`);
    await toEditor(on);
    await sleep(250);
};
// seePreview shows the preview, a tab of its own on a phone.
const seePreview = async (on) => {
    await on.eval(`(document.querySelector('[data-foot-tab=preview]')?.click(), true)`);
    await waitFor(() => on.eval(`!!${PREVIEW}`), 'the preview');
    await sleep(200);
};
const holds = (got, want) => Object.entries(want).every(([k, v]) => got[k] === v);
// resize gives the page a desktop's width, the middle one, or a phone's,
// as a phone's screen in Chromium.
async function resize(on, [w, hgt], phone = false) {
    if (which === 'chromium') {
        await on.cdp('Emulation.setDeviceMetricsOverride', { width: w, height: hgt, deviceScaleFactor: phone ? 2 : 1, mobile: phone });
        await on.touch(phone);
    } else {
        await on.bidi('browsingContext.setViewport', { context: on.context, viewport: { width: w, height: hgt } });
    }
    await sleep(600);
}
let b2 = null;

// themesAt walks a theme from the list to the page at a width, which
// gives Appearance its layout: Dracula chosen, the preview in its colors
// while the page keeps its own, worn by Use and marked in the list;
// copied by Customize, edited against the preview, a color too near the
// background marked, the edits waiting for Save, then saved and worn;
// DaisyUI's generator's block pasted, the preview, then the page, setting
// each variable as it gives them; and both removed, the page following
// the system again.
async function themesAt(width, layout) {
    const at = (what) => `${width}: ${what}`;
    const name = slug(width);
    step(at('a built-in theme chosen, and worn'));
    await openAppearance(b, 'Theme');
    const shape = await b.eval(`${APPEARANCE}.dataset.appearance`);
    const before = await wears(b);
    await pick(b, 'dracula');
    const dracula = JSON.parse(await b.eval(PROBE('dracula')));
    const chosen = JSON.parse(await b.eval(`JSON.stringify({ name: ${EDITOR}.querySelector('[data-theme-name]')?.textContent.trim(),
        builtIn: ${EDITOR}.hasAttribute('data-built-in'), use: !!${button('Use', EDITOR)} })`));
    await h.snap(`themes-${name}-chosen`);
    await seePreview(b);
    const shown = await previewWears(b);
    const page = await wears(b);
    c.report(shape === layout && chosen.name === 'Dracula' && chosen.builtIn && chosen.use && shown.theme === 'dracula' && shown.page === dracula.base
        && shown.text === dracula.text && shown.primary === dracula.primary && page.theme === before.theme && page.page === before.page && page.page !== shown.page && page.set === 0,
    at(`Appearance laid out as ${shape}; Dracula, chosen, shows in the editor with Use (${q(chosen)}) and in the preview in its colors (${q(shown)}), the page keeping its own (${q(page)})`));
    let list = await listed(b);
    c.report(list.picked === 'dracula' && list.worn === 'system', at(`the list checks Dracula and marks System worn: ${q(list)}`));
    await toEditor(b);
    await press(b, 'Use', EDITOR);
    const worn = await wears(b);
    const mark = await b.eval(`!!${EDITOR}.querySelector('[data-worn-mark]') && !${button('Use', EDITOR)}`);
    list = await listed(b);
    c.report(worn.theme === 'dracula' && worn.page === dracula.base && worn.set === 0 && mark && list.worn === 'dracula'
        && (await b.eval(`localStorage.getItem('DENS_THEME')`)) === 'dracula', at(`Use wears it, which the editor and the list mark: ${q(worn)}, ${q(list)}`));

    step(at('copied by Customize, and edited against the preview'));
    await toEditor(b);
    await press(b, 'Customize', EDITOR);
    await waitFor(() => b.eval(`!!${EDITOR} && !${EDITOR}.hasAttribute('data-built-in')`), "Dracula's copy");
    const copy = JSON.parse(await b.eval(`JSON.stringify({ name: ${EDITOR}.querySelector('input[data-theme-name]').value, use: !!${button('Use', EDITOR)},
        tiles: [...document.querySelectorAll('button[data-color-tile]')].map((t) => t.dataset.colorTile).join() })`));
    list = await listed(b);
    c.report(copy.name === 'Dracula' && copy.use && copy.tiles === 'base,text,primary,neutral,info,success,warning,error' && list.own === 'Dracula*' && list.worn === 'dracula',
        at(`Customize copies it into My themes, shown and not worn, its colors' tiles in the generator's rows: ${q(copy)}, ${q(list)}`));
    await toEditor(b);
    await set(b, '[data-theme-editor] input[data-theme-name]', 'Check theme');
    await b.eval(`document.getElementById('theme-dark').click(), true`);
    await color(b, 'base', 'Background', '#f4efe6');
    await color(b, 'text', 'Text', '2a2118');
    await color(b, 'primary', 'Primary', '#f0e6da');
    await sleep(250);
    const low = await b.eval(`[...document.querySelectorAll('[data-low-contrast]')].map((e) => e.dataset.lowContrast).join('|')`);
    await set(b, '[data-color="Primary"] input[type=text]', '#c41');
    await set(b, '[data-color="Primary"] input[type=color]', '#c2410c');
    await b.eval(`document.querySelector('[data-radius=radiusField] input[value="0"]').click(), true`);
    await set(b, '[data-border]', '2');
    await sleep(300);
    const lowAfter = await b.eval(`[...document.querySelectorAll('[data-low-contrast]')].map((e) => e.dataset.lowContrast).join('|')`);
    c.report(low === 'Primary' && lowAfter === '', at(`a primary too near the background is marked, alone, until it reads: ${q(low)}, then ${q(lowAfter)}`));
    const bar = await b.eval(`${BAR}?.textContent.trim() || ''`);
    const waits = !(await b.eval(`!!${button('Use', EDITOR)}`));
    await h.snap(`themes-${name}-made`, { scroll: TABPANEL, max: 3 });
    await seePreview(b);
    const made = await previewWears(b);
    const framed = await b.eval(`${PREVIEW}.parentElement.matches('[data-preview-frame]') && ${PREVIEW}.previousElementSibling?.textContent === 'Preview'`);
    c.report(framed === (layout !== 'phone'), at(`the preview is framed as one beside the editor, and unframed as the phone's tab: ${framed}`));
    const kept = await wears(b);
    c.report(holds(made, { theme: 'light', page: '#f4efe6', text: '#2a2118', primary: '#c2410c', button: '#c2410c', radius: '0px', borderWidth: '2px' })
        && made.error === dracula.error && made.secondary === dracula.secondary,
    at(`the preview wears each edit as it's made, its buttons' corners and border, the rest Dracula's: ${q(made)}`));
    c.report(kept.theme === 'dracula' && kept.set === 0 && bar.startsWith('You have unsaved changes.') && waits,
        at(`the page keeps Dracula until Save, which the bar offers, Use waiting for it: ${q(bar)}`));
    if (layout === 'three') {
        await b.key('Escape');
        const asked = await waitFor(() => b.eval(`(${BAR}?.textContent || '').startsWith('Discard your unsaved changes?')`), 'the ask', 4000).catch(() => false);
        const stayed = await b.eval(`!!${APPEARANCE}`);
        await press(b, 'Keep editing', BAR);
        c.report(asked && stayed, at('Escape with edits unsaved asks first, and Appearance stays'));
    }
    await press(b, 'Save', BAR);
    await waitFor(() => b.eval(`!${BAR}`), 'the edits saved');
    const unworn = await wears(b);
    list = await listed(b);
    c.report(unworn.theme === 'dracula' && unworn.set === 0 && list.own === 'Check theme*' && JSON.parse(await b.eval(`localStorage.getItem('DENS_THEMES')`))[0].name === 'Check theme',
        at(`Save keeps it, renamed, not yet worn: ${q(list)}`));
    await toEditor(b);
    await press(b, 'Use', EDITOR);
    const saved = await wears(b);
    const stored = await b.eval(`localStorage.getItem('DENS_THEME')`);
    list = await listed(b);
    c.report(holds(saved, { theme: 'light', page: '#f4efe6', text: '#2a2118', primary: '#c2410c', radiusField: '0rem', border: '2px' }) && STYLED.test(saved.styled)
        && list.own === 'Check theme*+' && /^custom:[a-z0-9]{8}$/.test(stored),
    at(`Use wears it, the root's style DaisyUI's variables alone, each a value the page wrote: ${q(saved)}`));
    const swatch = await b.eval(`(() => { const hex = ${HEX}; const s = document.querySelector('[role=radiogroup][aria-label="My themes"] [data-theme]');
        return [hex(getComputedStyle(s).backgroundColor), ...[...s.children].map((d) => hex(getComputedStyle(d).backgroundColor))].join(' '); })()`);
    c.report(swatch === '#f4efe6 #2a2118 #c2410c', at(`its swatch shows its text and primary: ${swatch}`));
    await h.snap(`themes-${name}-own`, { scroll: LIST, max: 2 });

    step(at("DaisyUI's generator's block, pasted, shows as it does there"));
    await press(b, 'Paste a code', LIST);
    await set(b, PASTE, GENERATOR);
    await press(b, 'Add', LIST);
    await waitFor(async () => (await ownNames(b)) === 'Check theme+|mytheme*', "the generator's theme shown");
    const given = Object.fromEntries(GENERATOR.split('\n').filter((l) => l.trim().startsWith('--')).map((l) => l.trim().slice(0, -1).split(': ')));
    const read = (box) => b.eval(`JSON.stringify(Object.fromEntries(${q(Object.keys(given))}.map((v) => [v, ${box}.style.getPropertyValue(v)])))`).then(JSON.parse);
    await seePreview(b);
    const inPreview = await read(PREVIEW);
    const previewDiffer = Object.keys(given).filter((v) => inPreview[v] !== given[v]);
    const still = await wears(b);
    c.report(previewDiffer.length === 0 && (await b.eval(`${PREVIEW}.dataset.theme`)) === 'light' && still.page === '#f4efe6',
        at(`the preview sets each of its ${Object.keys(given).length} variables as the block gives it, the page keeping hers${previewDiffer.length ? `; differ: ${previewDiffer.join(', ')}` : ''}`));
    await toEditor(b);
    await press(b, 'Use', EDITOR);
    const onRoot = await read('document.documentElement');
    const differ = Object.keys(given).filter((v) => onRoot[v] !== given[v]);
    const generated = await wears(b);
    const drawn = await b.eval(`(() => { const hex = ${HEX}; return [hex(${q(given['--color-base-100'])}), hex(${q(given['--color-base-content'])})].join(' '); })()`);
    c.report(differ.length === 0 && generated.theme === 'light' && `${generated.page} ${generated.text}` === drawn,
        at(`worn, the page sets each as the block gives it, and draws them${differ.length ? `; differ: ${differ.map((v) => `${v} ${onRoot[v]}`).join(', ')}` : ''}: ${q(generated)}`));
    await b.shot(`${HOME}/shot-settings-${which}-${name}-generator.png`);

    step(at('both removed, the page following the system'));
    await toEditor(b);
    await press(b, 'Remove', EDITOR);
    await pickNamed(b, 'Check theme');
    await press(b, 'Remove', EDITOR);
    const after = await wears(b);
    list = await listed(b);
    c.report(list.own === '' && list.worn === 'system' && list.picked === 'system' && after.set === 0 && ['light', 'dark'].includes(after.theme)
        && (await b.eval(`localStorage.getItem('DENS_THEME')`)) === null && (await b.eval(`localStorage.getItem('DENS_THEMES')`)) === null,
    at(`removed, the page follows the system again, nothing left on the root: ${q(after)}, ${q(list)}`));
    await closeSettings(b);
}

try {
    await pair(b, ALICE);
    await b.go(`http://localhost:${ALICE.port}/`);

    if (runs('sections')) {
        step('every section of the settings, from the list down the side');
        await b.eval(`document.querySelector('button[aria-label="Settings"], a[aria-label="Settings"]').click(), true`);
        await waitFor(() => b.eval(`!!${SETTINGS}?.querySelector('nav [aria-current=page]')`), 'the settings');
        c.report((await sections('[role=dialog][aria-label="Settings"]')) === 'Appearance|Chat*|Notifications|Voice|Relay|System',
            `the settings list their sections down the side, open at Chat, Appearance taking the window: ${await sections('[role=dialog][aria-label="Settings"]')}`);
        const ready = {
            Chat: `document.getElementById('compress')`,
            Notifications: `document.getElementById('notify-dms') && document.querySelector('[data-sound]')`,
            Voice: `document.getElementById('noise') && document.querySelector('select')`,
            Relay: `document.getElementById('relay-keys') && document.getElementById('relay-sound') && document.querySelector('[data-docs-link]')`,
            System: `document.querySelector('a[href="/api/logs"]') && document.getElementById('hosting')`,
        };
        for (const name of ['Chat', 'Notifications', 'Voice', 'Relay', 'System']) {
            await openSettings(b, name);
            await waitFor(() => b.eval(`!!(${ready[name]})`), `${name}'s cards`);
            await sleep(400);
            await h.snap(`settings-${slug(name)}`, { scroll: PANEL, max: 6 });
            const wrapped = await b.eval(WRAPPED);
            c.report(wrapped.length === 0, `${name}: every hint reads in a line${wrapped.length ? `; wrapped: ${wrapped.join('; ')}` : ''}`);
            frame(name, await b.eval(FRAME));
        }
        // Do not disturb's switch, atop Notifications, holds every
        // notification until it's turned off, and lights Home's moon
        // behind the settings.
        await openSettings(b, 'Notifications');
        await waitFor(() => b.eval(`!!document.getElementById('notify-dnd')`), 'the Do not disturb switch');
        const atop = await b.eval(`${PANEL}.querySelector('input[type=checkbox]')?.id`);
        const quiet = () => JSON.parse(lib('api', 'alice', 'GET', '/api/settings')).notifications;
        await b.eval(`document.getElementById('notify-dnd').click(), true`);
        const on = await waitFor(() => quiet().dnd && !quiet().dndUntil, 'Do not disturb on', 5000).catch(() => false);
        const said = await waitFor(() => b.eval(`(() => { const p = document.getElementById('notify-dnd').parentElement.querySelector('p')?.textContent || '';
            return p.includes('until you turn it off') && p; })()`), 'its hint', 5000).catch(() => '');
        const lit = await b.eval(`document.querySelector('[data-quiet]')?.dataset.quiet === 'on'`);
        const dndWrapped = await b.eval(WRAPPED);
        await b.eval(`document.getElementById('notify-dnd').click(), true`);
        const off = await waitFor(() => !quiet().dnd, 'Do not disturb off', 5000).catch(() => false);
        c.report(atop === 'notify-dnd' && on && said && lit && dndWrapped.length === 0 && off,
            `Do not disturb's switch is atop Notifications (${atop}), on until turned off (${on}), saying so in a line (${said}${dndWrapped.length ? `; wrapped: ${dndWrapped.join('; ')}` : ''}), Home's moon lit behind (${lit}), and off again (${off})`);

        // Push to talk shows its release delay in Voice, which the audit and
        // the hints' measure take too.
        await openSettings(b, 'Voice');
        await b.eval(`document.querySelector('[role=radiogroup][aria-label="Sending your voice"] input[aria-label="Push to talk"]').click(), true`);
        await waitFor(() => b.eval(`!!document.querySelector('input[aria-label="Release delay"]')`), 'the release delay');
        await sleep(300);
        await h.snap('settings-voice-ptt', { scroll: PANEL, max: 6 });
        const pttWrapped = await b.eval(WRAPPED);
        c.report(pttWrapped.length === 0, `Voice with push to talk: every hint reads in a line${pttWrapped.length ? `; wrapped: ${pttWrapped.join('; ')}` : ''}`);
        await b.eval(`document.querySelector('[role=radiogroup][aria-label="Sending your voice"] input[aria-label="Voice activity"]').click(), true`);

        step('Appearance, a page that takes the window, from Voice and back');
        await b.eval(`${SETTINGS}.querySelector('[data-section=appearance]').click(), true`);
        await waitFor(() => b.eval(`!!${APPEARANCE}`), 'Appearance');
        const page = JSON.parse(await b.eval(`JSON.stringify((() => { const d = ${SETTINGS}; const r = d.getBoundingClientRect(); return {
            nav: !!d.querySelector('nav[aria-label=Sections]'), title: d.querySelector('h2').textContent.trim(), back: !!d.querySelector('[data-back]'),
            close: !!d.querySelector('button[aria-label=Close]'), layout: d.querySelector('[data-appearance]').dataset.appearance,
            size: [Math.round(r.width), Math.round(r.height)], window: [innerWidth, innerHeight],
            tabs: [...d.querySelectorAll('[data-pane-tab]')].map((t) => t.textContent.trim() + (t.getAttribute('aria-selected') === 'true' ? '*' : '')).join('|') }; })())`));
        c.report(!page.nav && page.title === 'Appearance' && page.back && page.close && page.layout === 'three' && q(page.size) === q(page.window) && page.tabs === 'Theme*|Messages|Calls',
            `Appearance takes the window, its list folded to ← back, with ✕, the themes, the editor's Theme, Messages and Calls, and the preview side by side: ${q(page)}`);
        for (const pane of ['Theme', 'Messages', 'Calls']) {
            await openAppearance(b, pane);
            await sleep(400);
            await h.snap(`appearance-${slug(pane)}`, { scroll: TABPANEL, max: 3 });
            const wrapped = await b.eval(WRAPPED);
            c.report(wrapped.length === 0, `Appearance's ${pane}: every hint reads in a line${wrapped.length ? `; wrapped: ${wrapped.join('; ')}` : ''}`);
        }
        await b.eval(`${SETTINGS}.querySelector('[data-back]').click(), true`);
        await waitFor(() => b.eval(`!${APPEARANCE}`), 'back from Appearance');
        c.report((await sections('[role=dialog][aria-label="Settings"]')) === 'Appearance|Chat|Notifications|Voice*|Relay|System', `← goes back to Voice, open before it: ${await sections('[role=dialog][aria-label="Settings"]')}`);
        await b.eval(`${SETTINGS}.querySelector('[data-section=appearance]').click(), true`);
        await waitFor(() => b.eval(`!!${APPEARANCE}`), 'Appearance again');
        await b.key('Escape');
        await waitFor(() => b.eval(`!${APPEARANCE} && !!${SETTINGS}`), 'Escape back from Appearance');
        const escaped = await sections('[role=dialog][aria-label="Settings"]');
        await b.key('Escape');
        await waitFor(async () => !(await b.eval(`!!${SETTINGS}`)), 'the settings closing');
        c.report(escaped === 'Appearance|Chat|Notifications|Voice*|Relay|System', `Escape goes back to Voice too, and again closes the settings: ${escaped}`);

        step("every section of Manage, the owner's");
        await openManage(b, 'General');
        await waitFor(() => b.eval(`!!${MANAGE}?.querySelector('input[type=url]')`), "Manage's General");
        const manage = await b.eval(`[...document.querySelectorAll('[role=dialog][aria-label^="Manage "] nav button')].map((x) => x.textContent.trim())`);
        c.report(manage.join('|') === 'General|Members|Invites|Bans|Audit log|Soundboard|Emoji|Events|Limits',
            `Manage lists what staff manage, the owner's Events and Limits last: ${manage.join(', ')}`);
        for (const name of manage) {
            await openManage(b, name);
            await sleep(900);
            await h.snap(`manage-${slug(name)}`, { scroll: PANEL, max: 4 });
            const wrapped = await b.eval(WRAPPED);
            c.report(wrapped.length === 0, `Manage's ${name}: every hint reads in a line${wrapped.length ? `; wrapped: ${wrapped.join('; ')}` : ''}`);
            frame(`Manage's ${name}`, await b.eval(FRAME));
        }
        step("bob found in Manage's Members, and his row's menu");
        await openManage(b, 'Members');
        const row = (name) => `${MANAGE}?.querySelector('[data-members] [data-member=${JSON.stringify(name)}]')`;
        await waitFor(() => b.eval(`!!${row('bob')} && !!${row('alice')}`), "Members' rows");
        await b.eval(`(() => {
            const box = ${MANAGE}.querySelector('input[data-members-find]');
            Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(box, 'bo');
            box.dispatchEvent(new Event('input', { bubbles: true }));
            return true;
        })()`);
        const narrowed = await waitFor(async () => !(await b.eval(`!!${row('alice')}`)) && b.eval(`!!${row('bob')}`), 'Members narrowed to bob').catch(() => false);
        c.report(narrowed, 'two letters narrow Members to bob');
        await b.eval(`${row('bob')}.querySelector('[data-member-menu]').click(), true`);
        const items = await waitFor(async () => {
            const keys = await b.eval(`[...document.querySelectorAll('[data-menu-item]')].map((i) => i.dataset.menuItem)`);
            return keys.length > 0 && keys;
        }, "bob's row's menu").catch(() => []);
        c.report(items.join() === 'promote,remove', `bob's row opens the menu his card has: ${items.join(', ')}`);
        await h.snap('manage-members-menu');
        await b.key('Escape');
        await waitFor(async () => !(await b.eval(`!!document.querySelector('[data-menu-item]')`)), "the row's menu closing");
        await b.key('Escape');
        await waitFor(async () => !(await b.eval(`!!${MANAGE}`)), 'Manage closing');

        step("her own sections of the den, from the cog on its page");
        await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
        await waitFor(() => b.eval(`!!document.querySelector('button[aria-label="Settings"]')`), "the den's cog");
        await b.eval(`document.querySelector('button[aria-label="Settings"]').click(), true`);
        await waitFor(() => b.eval(`!!${SETTINGS}?.querySelector('nav [data-section=devices]')`), "the den's group");
        const listedSections = await b.eval(`[...document.querySelectorAll('[role=dialog][aria-label="Settings"] nav > *')]
            .map((x) => (x.localName === 'p' ? '[' + x.textContent.trim() + ']' : x.textContent.trim())).join('|')`);
        c.report(/^\[[^\]]+\]\|Devices\|Password and recovery\|Your code\|Your files\|\[App\]\|Appearance\|Chat\|Notifications\|Voice\|Relay\|System$/.test(listedSections),
            `on the den's page the settings list her own sections of it first, under its name, then the app's; the den she hosts has no Leave den: ${listedSections}`);
        for (const name of ['Devices', 'Password and recovery', 'Your code', 'Your files']) {
            await openSettings(b, name);
            await sleep(900);
            await h.snap(`settings-den-${slug(name)}`, { scroll: PANEL, max: 4 });
            const wrapped = await b.eval(WRAPPED);
            c.report(wrapped.length === 0, `${name}: every hint reads in a line${wrapped.length ? `; wrapped: ${wrapped.join('; ')}` : ''}`);
            frame(name, await b.eval(FRAME));
        }
        step('this device renamed from its row in Devices');
        await openSettings(b, 'Devices');
        const button = (text) => `[...${PANEL}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(text)})`;
        await waitFor(() => b.eval(`!!${button('Rename')}`), 'Rename');
        await b.eval(`${button('Rename')}.click(), true`);
        await waitFor(() => b.eval(`!!${PANEL}.querySelector('[data-device-name]')`), 'the name field');
        await b.eval(`(() => {
            const box = ${PANEL}.querySelector('[data-device-name]');
            Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(box, "Alice's desk");
            box.dispatchEvent(new Event('input', { bubbles: true }));
            return true;
        })()`);
        await sleep(200);
        await b.eval(`${PANEL}.querySelector('[data-device-name]').form.querySelector('button[type=submit]').click(), true`);
        const renamed = await waitFor(() => b.eval(`[...${PANEL}.querySelectorAll('li .font-medium')].some((x) => x.textContent === "Alice's desk")`),
            'the new name in Devices').then(() => true, () => false);
        const label = JSON.parse(lib('api', 'alice', 'GET', '/api/settings')).deviceLabel;
        c.report(renamed && /^Alice's desk \(.+\)$/.test(label), `Rename names this device, which Devices reads as the den tells it, what it is kept: ${label}`);
        await h.snap('settings-den-devices-renamed', { scroll: PANEL, max: 2 });
        await closeSettings(b);
        await b.go(`http://localhost:${ALICE.port}/`);

        step('Staff marks');
        lib('api', 'alice', 'POST', `/api/dens/${den}/channels/${general}/messages`, q({ nonce: lib('nonce'), text: "Alice's message, the owner's." }));
        await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
        await waitFor(() => b.eval(`document.body.innerText.includes("Alice's message, the owner's.")`), "alice's message");
        await waitFor(() => b.eval(`document.querySelectorAll('[data-scroller] [data-id]').length >= 5`), "general's messages");
        if (!(await b.eval(`!!document.querySelector('nav[aria-label=Members]')`))) await b.eval(`document.querySelector('button[aria-label=Members]').click(), true`);
        await waitFor(() => b.eval(`!!document.querySelector('nav[aria-label=Members] [data-role-mark=owner]')`), 'the member list');
        const marks = () => b.eval(`document.querySelectorAll('[data-scroller] [data-role-mark=owner]').length`);
        c.report((await marks()) === 0, 'until she turns them on, no crown on her messages, while the member list shows hers');
        await openAppearance(b, 'Messages');
        await waitFor(() => b.eval(`!!document.getElementById('staff-marks')`), 'Staff marks');
        c.report(!(await b.eval(`document.getElementById('staff-marks').checked`)), 'Staff marks starts off');
        await b.eval(`document.getElementById('staff-marks').click(), true`);
        await closeSettings(b);
        const shown = await waitFor(async () => ((await marks()) > 0 ? marks() : null), 'the crowns on her messages').catch(marks);
        c.report(shown > 0 && (await b.eval(`localStorage.getItem('DENS_STAFF_MARKS')`)) === 'on', `turned on, her messages show the crown, ${shown} of them, which the browser keeps`);
        await openAppearance(b, 'Messages');
        await b.eval(`document.getElementById('staff-marks').click(), true`);
        await closeSettings(b);
        c.report(await waitFor(async () => (await marks()) === 0, 'the crowns gone').then(() => true, () => false), 'turned off, they go again');
    }

    if (runs('preview')) {
        step("Appearance's preview: a theme, a name style, Density, the call timer, the sizes and file names, as each is chosen");
        await openAppearance(b, 'Theme');
        await waitFor(() => b.eval(`!!${PREVIEW}?.querySelector('[data-id="preview-1"] [data-name]')`), 'the preview');
        const look = async () => JSON.parse(await b.eval(`JSON.stringify((() => {
            const p = ${PREVIEW}, name = p.querySelector('[data-id="preview-1"] [data-name]');
            const s = getComputedStyle(name);
            const box = (e) => { const r = e.getBoundingClientRect(); return [Math.round(r.width), Math.round(r.height)]; };
            const photo = p.querySelector('[data-preview-photo]');
            return { theme: p.dataset.theme, page: getComputedStyle(p).backgroundColor, font: s.fontFamily.split(',')[0].replace(/["']/g, '').trim(),
                painted: s.backgroundImage !== 'none' || s.color !== getComputedStyle(p).color, lines: p.querySelectorAll('[data-line]').length,
                timer: !!p.querySelector('[data-call-length]'), photo: box(photo), player: box(p.querySelector('[data-preview-player]')), title: photo.getAttribute('title') || null };
        })())`));
        const radio = (name, value) => b.eval(`document.querySelector('input[name=${name}][value=${value}]').click(), true`);
        // A size's slider goes to a stop, as a drag leaves it.
        const slide = (group, size) => b.eval(`(() => { const s = document.querySelector('input[type=range][data-${group}]');
            Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(s, ${['small', 'medium', 'large', 'largest'].indexOf(size)});
            s.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
        await pick(b, 'light');
        const light = await look();
        await pick(b, 'dark');
        const dark = await look();
        c.report(light.theme === 'light' && dark.theme === 'dark' && light.page !== dark.page, `the preview wears Light, then Dark, chosen in the list: ${light.page}, ${dark.page}`);
        await openAppearance(b, 'Messages');
        await radio('name-styles', 'off');
        await sleep(300);
        const plain = await look();
        await radio('name-styles', 'all');
        await sleep(600);
        const styled = await look();
        c.report(/fredoka/i.test(styled.font) && styled.painted && !/fredoka/i.test(plain.font) && !plain.painted,
            `Alice's name in the preview, in her font and colors, then plain with name styles off: ${q(styled)}, ${q(plain)}`);
        await radio('density', 'compact');
        await sleep(300);
        const compact = await look();
        await radio('density', 'default');
        await sleep(300);
        const roomy = await look();
        c.report(compact.lines === 3 && roomy.lines === 0, `Compact makes the preview's messages a line each (${compact.lines} of 3), Default gives them back (${roomy.lines})`);
        await slide('media-size', 'large');
        await slide('player-size', 'largest');
        await sleep(300);
        const large = await look();
        await slide('media-size', 'medium');
        await slide('player-size', 'small');
        await b.eval(`document.getElementById('file-names').click(), true`);
        await sleep(300);
        const unnamed = await look();
        await b.eval(`document.getElementById('file-names').click(), true`);
        c.report(q(roomy.photo) === q([107, 80]) && q(roomy.player) === q([67, 38]) && q(large.photo) === q([160, 120]) && q(large.player) === q([213, 120])
            && roomy.title === 'lake.jpg' && unnamed.title === null,
        `the preview's photo and player at a sixth of their sizes, Medium and Small (${q(roomy.photo)}, ${q(roomy.player)}), Large and Largest (${q(large.photo)}, ${q(large.player)}); the photo's name on hover, and none with File names off`);
        await openAppearance(b, 'Calls');
        await b.eval(`document.getElementById('call-timer').click(), true`);
        await sleep(300);
        const untimed = await look();
        await b.eval(`document.getElementById('call-timer').click(), true`);
        c.report(roomy.timer && !untimed.timer, `the preview's call shows its length, and none with the call timer off`);
        // The preview is a column of its own: the list scrolls beside it.
        const held = JSON.parse(await b.eval(`(async () => {
            const panel = ${LIST}, p = ${PREVIEW};
            const before = Math.round(p.getBoundingClientRect().top);
            panel.scrollTop = panel.scrollHeight;
            await new Promise((r) => setTimeout(r, 300));
            const out = { before, after: Math.round(p.getBoundingClientRect().top), scrolled: panel.scrollTop };
            panel.scrollTop = 0;
            return JSON.stringify(out);
        })()`));
        c.report(held.scrolled > 100 && held.after === held.before, `the preview holds its place while the list scrolls beside it: ${q(held)}`);
        await h.snap('appearance-preview');
        c.report((await b.eval(`localStorage.getItem('DENS_THEME')`)) === null, 'choosing themes for the preview wore none');
        await closeSettings(b);
    }

    if (runs('sizes')) {
        step("Appearance's sizes: photos and videos, and YouTube players");
        // Players are on, as Chat's switch turns them, so the link has its box.
        await b.eval(`localStorage.setItem('DENS_YOUTUBE', '1'), true`);
        await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
        await waitFor(() => b.eval(`!!document.querySelector('[data-id="${photo}"] button[aria-label^="Open "]') && !!document.querySelector('[data-video]')`), "general's photo and player");
        // measure takes the photo's tile and the player's box, each by its
        // width and height, whether the photo itself shows over its preview
        // and has drawn, the player's picture, and the pane's width.
        const measure = () => b.eval(`JSON.stringify((() => {
            const box = (e) => { const r = e.getBoundingClientRect(); return [Math.round(r.width), Math.round(r.height)]; };
            const tile = document.querySelector('[data-id="${photo}"] button[aria-label^="Open "]');
            const sharp = tile.querySelector('[data-sharp]');
            const player = document.querySelector('[data-video]');
            return { tile: box(tile), sharp: sharp ? (sharp.complete && sharp.naturalWidth > 0 ? sharp.naturalWidth : 'loading') : 'none', player: box(player),
                picture: (player.querySelector('img')?.getAttribute('src') || '').split('/').pop(), pane: box(document.querySelector('[data-pane=chan]'))[0],
                header: box(document.querySelector('[data-pane=chan] header').firstElementChild)[0] };
        })())`).then(JSON.parse);
        const sized = async (group, choice) => {
            await openAppearance(b, 'Messages');
            await waitFor(() => b.eval(`!!document.querySelector('[${group}]')`), 'the sizes');
            // The slider goes to the stop, as a drag leaves it.
            await b.eval(`(() => { const s = document.querySelector('input[type=range][${group}]');
                Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(s, ${['Small', 'Medium', 'Large', 'Largest'].indexOf(choice)});
                s.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
            await sleep(200);
            await closeSettings(b);
            await b.eval(`document.querySelector('[data-id="${photo}"]').scrollIntoView({ block: 'center' }), true`);
            await sleep(400);
            return measure();
        };
        const first = await measure();
        c.report(first.pane >= 1300 && first.header >= first.pane - 40, `the pane is wide enough for the check, and its header takes it: ${q(first)}`);
        // The photo is a phone's, portrait, so each box holds it by its height.
        c.report(q(first.tile) === q([360, 480]) && first.sharp === 'none' && q(first.player) === q([400, 225]) && first.picture === 'thumb',
            `until she chooses, a photo shows at Medium, 480 high, as its preview, and a player at Small: ${q(first)}`);
        await openAppearance(b, 'Messages');
        // Each size is a slider of four stops, the one chosen named beside it.
        const choices = JSON.parse(await b.eval(`JSON.stringify(['data-media-size', 'data-player-size'].map((g) => {
            const s = document.querySelector('input[type=range][' + g + ']');
            return [s.min, s.max, s.step, s.getAttribute(g), s.getAttribute('aria-valuetext')].join(' ');
        }))`));
        const width = await b.eval(`!!document.querySelector('[data-width]')`);
        await closeSettings(b);
        c.report(choices[0] === '0 3 1 medium Medium' && choices[1] === '0 3 1 small Small' && !width,
            `Messages offers each four sizes on a slider, and no Width: ${q(choices)}`);
        const large = await sized('data-media-size', 'Large');
        const drawn = await waitFor(async () => { const m = await measure(); return typeof m.sharp === 'number' && m; }, 'the photo itself over its preview');
        c.report(q(large.tile) === q([540, 720]) && drawn.sharp > 640 && q(large.player) === q(first.player),
            `Large shows the photo 720 high, the photo itself drawn over the den's preview, and leaves the player: ${q(drawn)}`);
        await b.shot(`${HOME}/shot-settings-${which}-large.png`);
        const largest = await sized('data-media-size', 'Largest');
        c.report(q(largest.tile) === q([720, 960]), `Largest shows it 960 high: ${q(largest.tile)}`);
        const small = await sized('data-media-size', 'Small');
        c.report(q(small.tile) === q([225, 300]) && small.sharp === 'none', `Small shows it 300 high, as its preview: ${q(small)}`);
        const player = await sized('data-player-size', 'Large');
        c.report(q(player.player) === q([960, 540]) && player.picture === 'large' && q(player.tile) === q(small.tile),
            `a Large player is 960 by 540, its cover the larger picture, and the photo stays: ${q(player)}`);
        const kept = await b.eval(`[localStorage.getItem('DENS_MEDIA_SIZE'), localStorage.getItem('DENS_PLAYER_SIZE')].join(' ')`);
        c.report(kept === 'small large', `the browser keeps both choices: ${kept}`);
        // A narrow pane still holds each: neither runs past it.
        await b.eval(`localStorage.setItem('DENS_MEDIA_SIZE', 'largest'), localStorage.setItem('DENS_PLAYER_SIZE', 'largest'), true`);
        await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
        await waitFor(() => b.eval(`!!document.querySelector('[data-id="${photo}"] button[aria-label^="Open "]')`), 'general again');
        const fits = await b.eval(`JSON.stringify((() => { const s = document.querySelector('[data-scroller]'); return [s.scrollWidth, s.clientWidth]; })())`).then(JSON.parse);
        c.report(fits[0] <= fits[1], `at Largest nothing runs past the list's edge: ${q(fits)}`);
        await b.eval(`localStorage.removeItem('DENS_MEDIA_SIZE'), localStorage.removeItem('DENS_PLAYER_SIZE'), localStorage.removeItem('DENS_YOUTUBE'), true`);
    }

    if (runs('themes')) {
        await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
        await waitFor(() => b.eval(`document.querySelectorAll('[data-scroller] [data-id]').length >= 5`), "general's messages");
        for (const [width, size, layout] of [['a desktop', [1900, 1000], 'three'], ['the middle width', [1100, 900], 'two'], ['a phone', [390, 844], 'phone']]) {
            await resize(b, size, layout === 'phone');
            await themesAt(width, layout);
        }
        await resize(b, [1900, 1000]);
    }

    if (runs('codes')) {
        step('a theme of her own, its code copied as a block');
        await b.go(`http://localhost:${ALICE.port}/`);
        await openAppearance(b, 'Theme');
        await pick(b, 'light');
        await press(b, 'Customize', EDITOR);
        await waitFor(() => b.eval(`!!${EDITOR} && !${EDITOR}.hasAttribute('data-built-in')`), "Light's copy");
        await set(b, '[data-theme-editor] input[data-theme-name]', 'Check theme');
        await color(b, 'base', 'Background', '#f4efe6');
        await color(b, 'text', 'Text', '#2a2118');
        await color(b, 'primary', 'Primary', '#7c3aed');
        await b.eval(`document.querySelector('[data-radius=radiusBox] input[value="1"]').click(), true`);
        await b.eval(`document.querySelector('[data-radius=radiusField] input[value="0"]').click(), true`);
        await set(b, '[data-border]', '2');
        if (!(await b.eval(`document.getElementById('theme-depth').checked`))) await b.eval(`document.getElementById('theme-depth').click(), true`);
        await press(b, 'Save', BAR);
        await press(b, 'Use', EDITOR);
        const edited = await wears(b);
        await b.eval(`(() => { window.copied = []; navigator.clipboard.writeText = async (t) => { window.copied.push(t); }; return true; })()`);
        await press(b, 'Copy code', EDITOR);
        const code = await waitFor(async () => JSON.parse(await b.eval('JSON.stringify(window.copied || [])'))[0], 'the copied code');
        const lines = code.split('\n');
        c.report(lines[0] === '@plugin "daisyui/theme" {' && lines.at(-1) === '}' && lines.length === 34 && lines.includes('  name: "Check theme";')
            && lines.includes('  color-scheme: "light";') && lines.includes('  --color-base-100: #f4efe6;') && lines.includes('  --color-primary: #7c3aed;')
            && lines.includes('  --radius-box: 1rem;') && lines.includes('  --radius-field: 0rem;') && lines.includes('  --border: 2px;') && lines.includes('  --depth: 1;'),
        `Copy code gives the theme as a block in the generator's shape, every variable in it: ${q(code)}`);
        await closeSettings(b);

        step("pasted into bob's browser, and a stranger's blocks read for their colors alone");
        b2 = await launch(which, which === 'chromium' ? 9573 : 9574, { size: [1400, 900], hover: true });
        await pair(b2, BOB);
        await openAppearance(b2, 'Theme');
        const plain = await wears(b2);
        const block = (...ls) => `@plugin "daisyui/theme" {\n${ls.map((l) => `  ${l};`).join('\n')}\n}`;
        const hostile = [
            ['a url where a color goes', block('name: "x"', '--color-base-100: url(https://evil.example/x.png)')],
            ['a color in words', block('--color-primary: red')],
            ['a style after a color', block('--color-primary: #ff0000 ;background:url(https://evil.example/x.png)}').replace(';background', 'x;background')],
            ['a variable it doesn\'t know', block('--fx-noise: url(https://evil.example/n.svg)', 'background-image: url(https://evil.example/z.png)')],
            ['a radius past the range', block('--radius-box: 99rem', '--border: calc(1px)')],
            ['a v1.13.0 code with a url where a color goes', 'dens-theme:1.l.o.url(https://evil.example/x.png).78'],
            ['JSON', '{"name":"x","base":"url(https://evil.example/x.png)"}'],
        ];
        await press(b2, 'Paste a code', LIST);
        const refused = [];
        for (const [what, bad] of hostile) {
            await set(b2, PASTE, bad);
            await press(b2, 'Add', LIST);
            const said = await b2.eval(`${LIST}.querySelector('[role=alert]')?.textContent.trim() || ''`);
            const after = await wears(b2);
            if (said !== "That isn't a theme's code." || after.set !== 0 || after.page !== plain.page || (await ownNames(b2)) !== '') refused.push(`${what}: ${q(said)}, ${q(after.styled)}`);
        }
        c.report(refused.length === 0 && (await b2.eval(`localStorage.getItem('DENS_THEMES')`)) === null,
            `each of a stranger's ${hostile.length} pastes with nothing the page reads is refused, and nothing of them kept or styled${refused.length ? `: ${refused.join('; ')}` : ''}`);
        // A block with hostile lines among its own reads for its own alone.
        const mixed = code.replace('  --color-base-200:', '  --color-accent: url(https://evil.example/x.png);\n  background: url(https://evil.example/y.png);\n  --color-base-200:')
            .replace('  --noise: 0;', '  --noise: 0 ; --border: 9px;');
        await set(b2, PASTE, mixed);
        await press(b2, 'Add', LIST);
        await waitFor(async () => (await ownNames(b2)) === 'Check theme*', 'the mixed block kept, shown and not worn');
        await press(b2, 'Use', EDITOR);
        const fromMixed = await wears(b2);
        c.report(STYLED.test(fromMixed.styled) && !/url|evil|9px/.test(fromMixed.styled) && fromMixed.accent === edited.accent && fromMixed.border === '2px',
            `a block with hostile lines keeps its own values, and nothing hostile reaches the root: ${q(fromMixed.styled)}`);
        await b2.shot(`${HOME}/shot-settings-${which}-refused.png`);
        await press(b2, 'Remove', EDITOR);
        await waitFor(async () => (await ownNames(b2)) === '', 'the mixed theme leaving');
        await press(b2, 'Paste a code', LIST);
        await set(b2, PASTE, `  ${code}\n`);
        await press(b2, 'Add', LIST);
        await waitFor(async () => (await ownNames(b2)) === 'Check theme*', "the pasted theme among bob's");
        await press(b2, 'Use', EDITOR);
        const pasted = await wears(b2);
        c.report(holds(pasted, { theme: 'light', page: '#f4efe6', text: '#2a2118', primary: '#7c3aed', secondary: edited.secondary, accent: edited.accent,
            error: edited.error, base200: edited.base200, base300: edited.base300, radiusField: '0rem', border: '2px', depth: '1' }),
        `bob's page wears alice's theme, variable for variable: ${q(pasted)}`);
        await b2.shot(`${HOME}/shot-settings-${which}-pasted.png`);

        step("a v1.13.0 code reads as it did");
        await press(b2, 'Paste a code', LIST);
        await set(b2, PASTE, OLD_CODE);
        await press(b2, 'Add', LIST);
        await waitFor(async () => (await ownNames(b2)) === 'Check theme+|Plum*', 'the old code\'s theme');
        await press(b2, 'Use', EDITOR);
        const old = await wears(b2);
        c.report(holds(old, { theme: 'dark', page: '#2b1d3a', text: '#eae0f5', primary: '#c084fc', radiusField: '0.25rem' }) && old.set === 23,
            `a v1.13.0 code's theme sets what it set then: ${q(old)}`);
        await closeSettings(b2);

        step('removed while worn');
        await openAppearance(b, 'Theme');
        c.report((await ownNames(b)) === 'Check theme*+', `her theme, worn, shown when Appearance opens: ${await ownNames(b)}`);
        await toEditor(b);
        await press(b, 'Remove', EDITOR);
        await waitFor(async () => (await ownNames(b)) === '', 'the theme leaving the list');
        const deleted = await wears(b);
        c.report(['light', 'dark'].includes(deleted.theme) && deleted.set === 0 && (await b.eval(`localStorage.getItem('DENS_THEME')`)) === null
            && (await b.eval(`localStorage.getItem('DENS_THEMES')`)) === null && (await listed(b)).worn === 'system',
        `removed, the page follows the system again, with nothing left on the root, System marked worn: ${q(deleted)}`);
        await closeSettings(b);

        step('the bars on a green theme of her own');
        await openAppearance(b, 'Theme');
        await press(b, 'New theme', LIST);
        await waitFor(() => b.eval(`!!${EDITOR} && !${EDITOR}.hasAttribute('data-built-in')`), 'the new theme');
        await set(b, '[data-theme-editor] input[data-theme-name]', 'Green');
        if (!(await b.eval(`document.getElementById('theme-dark').checked`))) await b.eval(`document.getElementById('theme-dark').click(), true`);
        await color(b, 'base', 'Background', '#14321f');
        await color(b, 'text', 'Text', '#e2efe6');
        await color(b, 'primary', 'Primary', '#3fae6a');
        await press(b, 'Save', BAR);
        await press(b, 'Use', EDITOR);
        await closeSettings(b);
        await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
        await waitFor(() => b.eval(`!!document.querySelector('textarea[placeholder^="Message "]')`), "general's message bar");
        const bars = JSON.parse(await b.eval(`JSON.stringify((() => { const hex = ${HEX};
            const bar = document.querySelector('textarea[placeholder^="Message "]').closest('.bg-bar');
            return { page: hex(getComputedStyle(document.body).backgroundColor), bar: hex(getComputedStyle(bar).backgroundColor), css: getComputedStyle(bar).backgroundColor }; })())`));
        c.report(bars.page === '#14321f' && bars.bar !== bars.page && apart(hue(bars.bar), hue(bars.page)) <= 5,
            `the message bar keeps the theme's green, a shade lighter: ${bars.bar} (${bars.css}) at ${Math.round(hue(bars.bar))}° on ${bars.page} at ${Math.round(hue(bars.page))}°`);
        await b.shot(`${HOME}/shot-settings-${which}-green.png`);
        await openAppearance(b, 'Theme');
        await press(b, 'Remove', EDITOR);
        await waitFor(async () => (await ownNames(b)) === '', 'the green theme leaving the list');
        await closeSettings(b);
    }

    if (runs('background')) {
        step('a picture behind the chat');
        b2 ||= await launch(which, which === 'chromium' ? 9573 : 9574, { size: [1400, 900], hover: true });
        const general$ = `http://localhost:${ALICE.port}/den/${den}/${general}`;
        const light = async (on) => {
            await openAppearance(on, 'Theme');
            await pick(on, 'light');
            if (await on.eval(`!!${button('Use', EDITOR)}`)) await press(on, 'Use', EDITOR);
            await closeSettings(on);
        };
        await b.go(general$);
        await waitFor(() => b.eval(`document.querySelectorAll('[data-scroller] [data-id]').length >= 5`), "general's messages");
        await light(b);
        await pair(b2, ALICE);
        await b2.go(general$);
        await waitFor(() => b2.eval(`document.querySelectorAll('[data-scroller] [data-id]').length >= 5`), "general's messages in the second browser");
        await light(b2);
        // behind is the list's background as the page has it, and as the
        // screen shows it at a point of the list no message covers.
        const behind = async (on, name) => {
            const at = JSON.parse(await on.eval(`JSON.stringify((() => { const s = document.querySelector('[data-scroller]'); const r = s.getBoundingClientRect();
                const style = getComputedStyle(s); return { image: style.backgroundImage, size: style.backgroundSize, x: Math.round(r.right - 60), y: Math.round(r.bottom - 10),
                    composer: getComputedStyle(document.querySelector('textarea[placeholder^="Message "]').closest('[data-scroller] ~ div')).backgroundImage,
                    list: getComputedStyle(document.querySelector('[data-pane=list]')).backgroundImage }; })())`));
            const file = `${HOME}/shot-settings-${which}-${name}.png`;
            await on.shot(file);
            return { ...at, pixel: pixel(file, at.x, at.y) };
        };
        const none = await behind(b, 'background-none');
        c.report(none.image === 'none' && none.pixel.join() === '255,255,255', `with no background the list is the theme's: ${q(none)}`);
        await openAppearance(b, 'Messages');
        const CARD = `document.querySelector('[data-chat-background]')`;
        await waitFor(() => b.eval(`!!${CARD}`), 'the Chat background card');
        await b.eval(`${CARD}.querySelector('input[type=file]').dataset.check = 'background', true`);
        const gif = `${HOME}/background.gif`;
        fs.writeFileSync(gif, Buffer.from('R0lGODlhAQABAIAAAP8AAAAAACH5BAAAAAAALAAAAAABAAEAAAICRAEAOw==', 'base64'));
        await b.attach('input[data-check=background]', [gif]);
        await waitFor(() => b.eval(`/PNG, a JPEG or a WebP/.test(${CARD}.textContent)`), 'the GIF refused');
        c.report((await behind(b, 'background-gif')).image === 'none', 'a GIF is refused, and nothing shows');
        await b.attach('input[data-check=background]', [png(`${HOME}/background.png`, 64, 64, [255, 0, 0])]);
        await waitFor(() => b.eval(`!!${CARD}.querySelector('[data-background-preview] img')?.naturalWidth`), 'the picture in the card');
        await h.snap('background-card', { scroll: TABPANEL, max: 3 });
        const cardWrapped = await b.eval(WRAPPED);
        c.report(cardWrapped.length === 0, `Chat background: every hint reads in a line${cardWrapped.length ? `; wrapped: ${cardWrapped.join('; ')}` : ''}`);
        c.report((await b.eval(`${CARD}.querySelector('input[type=range]').value`)) === '70' && (await b.eval(`[...${CARD}.querySelectorAll('button')].filter((x) => !x.closest('.tooltip')).map((x) => x.textContent.trim()).join('|')`)) === 'Change image|Remove',
            'the card shows the picture, its dim at 70%, Change image and Remove');
        const kept = await b.eval(`fetch(document.querySelector('[data-background-preview] img').src).then(async (r) => [r.status, r.headers.get('Content-Type'), r.headers.get('Cache-Control'),
            new TextDecoder('latin1').decode(await r.arrayBuffer()).includes('SecretText')].join(' '))`);
        c.report(kept === '200 image/png no-store false', `the service serves it stripped, never cached: ${kept}`);
        await closeSettings(b);
        const half = await behind(b, 'background-70');
        const pink = ([r, g, bl]) => r >= 250 && g > 90 && g < 215 && Math.abs(g - bl) <= 12;
        c.report(/url\("[^"]*\/api\/background\?v=\d+"\)/.test(half.image) && /^cover(, cover)?$/.test(half.size) && pink(half.pixel) && half.composer === 'none' && half.list === 'none',
            `the picture shows behind the list, under the theme's white at the dim, and nowhere else: ${q(half)}`);
        const second = await waitFor(async () => { const s = await behind(b2, 'background-second'); return s.image !== 'none' && s; }, 'the picture in the second browser');
        c.report(second.image === half.image && pink(second.pixel),
            `a second browser paired with her Dens shows the same: ${q(second.pixel)}`);

        step('dimmed, and removed');
        await openAppearance(b, 'Messages');
        await waitFor(() => b.eval(`!!${CARD}?.querySelector('input[type=range]')`), 'the Dim slider');
        await b.eval(`(() => { const r = ${CARD}.querySelector('input[type=range]'); r.dataset.check = 'dim'; return true; })()`);
        await set(b, 'input[data-check=dim]', '0', ['input', 'change']);
        await closeSettings(b);
        const clear = await waitFor(async () => { const s = await behind(b, 'background-0'); return s.pixel.join() === '255,0,0' && s; }, 'the picture undimmed');
        c.report(clear.pixel.join() === '255,0,0', `at 0% the picture shows as it is: ${q(clear.pixel)}`);
        await openAppearance(b, 'Messages');
        await waitFor(() => b.eval(`!!${CARD}?.querySelector('input[type=range]')`), 'the Dim slider');
        await b.eval(`(() => { const r = ${CARD}.querySelector('input[type=range]'); r.dataset.check = 'dim'; return true; })()`);
        await set(b, 'input[data-check=dim]', '90', ['input', 'change']);
        await closeSettings(b);
        const faint = ([r, g, bl]) => r >= 250 && g >= 215 && g < 255 && Math.abs(g - bl) <= 12;
        const dimmed = await waitFor(async () => { const s = await behind(b, 'background-90'); return faint(s.pixel) && s; }, 'the picture dimmed');
        const dimmed2 = await waitFor(async () => { const s = await behind(b2, 'background-second-90'); return faint(s.pixel) && s; }, 'the picture dimmed in the second browser');
        c.report(faint(dimmed.pixel) && faint(dimmed2.pixel), `at 90% it's faint, in both browsers: ${q(dimmed.pixel)}, ${q(dimmed2.pixel)}`);
        await b.go(general$);
        const again = await waitFor(async () => { const s = await behind(b, 'background-reload'); return faint(s.pixel) && s; }, 'the picture after a reload');
        c.report(faint(again.pixel), `a reload shows the picture at its dim: ${q(again.pixel)}`);

        step('Hover');
        // cover is how far a message under the pointer covers the picture,
        // as its row's background's alpha.
        const cover = async (on) => {
            const at = JSON.parse(await on.eval(`(() => { const r = [...document.querySelectorAll('[data-scroller] [data-id]')].at(-1).getBoundingClientRect();
                return JSON.stringify({ x: r.left + r.width / 2, y: r.top + r.height / 2 }); })()`));
            await on.move(at.x, at.y);
            await sleep(300);
            return on.eval(`(() => { const c = getComputedStyle([...document.querySelectorAll('[data-scroller] [data-id]')].at(-1)).backgroundColor;
                const alpha = c.includes('/') ? c.slice(c.lastIndexOf('/') + 1) : c.startsWith('rgba') ? c.slice(c.lastIndexOf(',') + 1) : '1';
                return parseFloat(alpha); })()`);
        };
        c.report(Math.abs((await cover(b)) - 1) < 0.01, 'until she sets it, a message under the pointer covers the picture');
        await openAppearance(b, 'Messages');
        await waitFor(() => b.eval(`!!${CARD}?.querySelector('[data-background-hover]')`), 'the Hover slider');
        c.report((await b.eval(`${CARD}.querySelector('[data-background-hover]').value`)) === '100', 'Hover, under Dim, starts at 100%');
        await b.eval(`${CARD}.querySelector('[data-background-hover]').dataset.check = 'hover', true`);
        await set(b, 'input[data-check=hover]', '40', ['input', 'change']);
        await closeSettings(b);
        const covered = await waitFor(async () => { const v = await cover(b); return Math.abs(v - 0.4) < 0.02 && v; }, 'the hover at 40%').catch(() => cover(b));
        const covered2 = await waitFor(async () => { const v = await cover(b2); return Math.abs(v - 0.4) < 0.02 && v; }, 'the hover at 40% in the second browser').catch(() => cover(b2));
        const keptHover = await b.eval(`fetch('/api/dens').then((r) => r.json()).then((d) => d.background?.hover)`);
        c.report(Math.abs(covered - 0.4) < 0.02 && Math.abs(covered2 - 0.4) < 0.02 && keptHover === 40,
            `at 40% a message under the pointer covers that much of it, in both browsers, the service keeping it: ${covered}, ${covered2}, ${keptHover}`);
        await b.shot(`${HOME}/shot-settings-${which}-hover.png`);
        await openAppearance(b, 'Messages');
        await waitFor(() => b.eval(`!!${CARD}`), 'the Chat background card');
        await press(b, 'Remove', CARD);
        await waitFor(() => b.eval(`!${CARD}.querySelector('[data-background-preview]') && ${button('Choose image', CARD)} && true`), 'the card without a picture');
        await closeSettings(b);
        const gone = await behind(b, 'background-removed');
        const gone2 = await waitFor(async () => { const s = await behind(b2, 'background-second-removed'); return s.image === 'none' && s; }, 'the picture leaving the second browser');
        const status = await b.eval(`fetch('/api/background').then((r) => r.status)`);
        c.report(gone.image === 'none' && gone.pixel.join() === '255,255,255' && gone2.pixel.join() === '255,255,255' && status === 404,
            `Remove takes it from both browsers and from the service: ${q(gone.pixel)}, ${q(gone2.pixel)}, HTTP ${status}`);
    }

    step('the layout audit over every screen');
    audits(which, h.log, (good, what) => c.report(good, what), false);
} catch (e) {
    c.fail(e);
    await b.shot(`${HOME}/shot-settings-${which}-failed.png`).catch(() => {});
    await b2?.shot(`${HOME}/shot-settings-${which}-failed-second.png`).catch(() => {});
} finally {
    await Promise.race([Promise.all([b.close(), b2?.close()]), sleep(5000)]);
    c.finish();
}
