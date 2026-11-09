// The M19.1 check of the settings, as alice, the den's owner, in the
// browser named at a desktop's width: every section of the settings and
// of Manage opened from the list down the side, each with the layout
// audit, and every hint in it read as one line, a hint that wrapped
// failing it, Voice with push to talk's release delay too (M19.3); then
// Appearance's Width: Wide and Narrow holding general's header, list and
// composer to 1,200 and 900 pixels, centered in the pane, and Full letting
// them go. From M19.4, a theme of her own: the editor starting from the
// theme the page shows, the page wearing each color as it's typed or
// picked, Cancel putting the one before back, the theme saved, chosen and
// kept across a reload, edited, its code copied and pasted into bob's
// browser, which then wears the same, a stranger's codes refused there
// with nothing reaching a style, and the theme deleted; and a picture
// behind the chat: a PNG set, found behind general's list under the dim,
// by the screen's own pixels, in a second browser paired with her Dens
// too, dimmed further, a GIF refused, and removed from both.
//
//   run.sh settings chromium|firefox [PART...]
//
// PART is sections, width, themes or background; all unless given.
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import zlib from 'node:zlib';
import { ALICE, BOB, HOME, MANAGE, SETTINGS, checks, launch, openManage, openSettings, pair, waitFor } from './browsers.mjs';
import { audits, helpers } from './screens.mjs';

const [, , which, ...parts] = process.argv;
const runs = (part) => parts.length === 0 || parts.includes(part);
const { DEN: den, GENERAL: general } = process.env;
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
        if (p.closest('[role=tooltip]')) continue;
        const s = getComputedStyle(p);
        if (s.display === 'none' || !p.getClientRects().length) continue;
        const lh = parseFloat(s.lineHeight) || parseFloat(s.fontSize) * 1.5;
        const lines = p.getBoundingClientRect().height / lh;
        if (lines > 1.5) out.push(JSON.stringify(p.textContent.trim().slice(0, 70)) + ' (' + Math.round(lines) + ' lines)');
    }
    return out;
})()`;
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
        set: document.documentElement.style.length, styled: document.documentElement.getAttribute('style') || '' };
})())`;
const wears = async (on) => JSON.parse(await on.eval(WEARS));
const q = JSON.stringify;
// set types into a field the page controls, as a member's typing or
// picking would: the value, then the event Preact listens for.
const set = (on, selector, value, events = ['input']) => on.eval(`(() => {
    const el = document.querySelector(${q(selector)});
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, ${q(value)});
    for (const e of ${q(events)}) el.dispatchEvent(new Event(e, { bubbles: true }));
    return true;
})()`);
const OWN = `document.querySelector('[data-own-themes]')`;
const EDITOR = `document.querySelector('[data-theme-editor]')`;
const button = (text, within = OWN) => `[...${within}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${q(text)})`;
const press = async (on, text, within) => {
    await waitFor(() => on.eval(`!!${button(text, within)} && !${button(text, within)}.disabled`), `the ${text} button`);
    await on.eval(`${button(text, within)}.click(), true`);
    await sleep(250);
};
const choose = (on, group, text) => on.eval(`[...document.querySelectorAll('[${group}] label')].find((l) => l.textContent.trim() === ${q(text)}).querySelector('input').click(), true`);
const field = (label, kind) => `[data-color="${label}"] input[type=${kind}]`;
const ownNames = (on) => on.eval(`[...document.querySelectorAll('[role=radiogroup][aria-label="Your themes"] label')].map((l) => l.textContent.trim() + (l.querySelector('input').checked ? '*' : '')).join('|')`);
const closeSettings = async (on) => {
    await on.key('Escape');
    await waitFor(async () => !(await on.eval(`!!${SETTINGS}`)), 'the settings closing');
};
let b2 = null;

try {
    await pair(b, ALICE);
    await b.go(`http://localhost:${ALICE.port}/`);

    if (runs('sections')) {
        step('every section of the settings, from the list down the side');
        await openSettings(b, 'Appearance');
        c.report((await sections('[role=dialog][aria-label="App settings"]')) === 'Appearance*|Chat|Notifications|Voice|System',
            `the settings list their sections down the side, open at Appearance: ${await sections('[role=dialog][aria-label="App settings"]')}`);
        const ready = {
            Appearance: `document.querySelector('[data-width]')`,
            Chat: `document.getElementById('compress')`,
            Notifications: `document.getElementById('notify-dms') && document.querySelector('[data-sound]')`,
            Voice: `document.getElementById('noise') && document.querySelector('select')`,
            System: `document.querySelector('a[href="/api/logs"]') && document.getElementById('hosting')`,
        };
        for (const name of ['Appearance', 'Chat', 'Notifications', 'Voice', 'System']) {
            await openSettings(b, name);
            await waitFor(() => b.eval(`!!(${ready[name]})`), `${name}'s cards`);
            await sleep(400);
            await h.snap(`settings-${slug(name)}`, { scroll: PANEL, max: 6 });
            const wrapped = await b.eval(WRAPPED);
            c.report(wrapped.length === 0, `${name}: every hint reads in a line${wrapped.length ? `; wrapped: ${wrapped.join('; ')}` : ''}`);
        }
        // Push to talk shows its release delay in Voice, which the audit and
        // the hints' measure take too.
        await openSettings(b, 'Voice');
        await b.eval(`[...document.querySelectorAll('[role=radiogroup][aria-label="Sending your voice"] label')].find((l) => l.textContent.trim() === 'Push to talk').querySelector('input').click(), true`);
        await waitFor(() => b.eval(`!!document.querySelector('input[aria-label="Release delay"]')`), 'the release delay');
        await sleep(300);
        await h.snap('settings-voice-ptt', { scroll: PANEL, max: 6 });
        const pttWrapped = await b.eval(WRAPPED);
        c.report(pttWrapped.length === 0, `Voice with push to talk: every hint reads in a line${pttWrapped.length ? `; wrapped: ${pttWrapped.join('; ')}` : ''}`);
        await b.eval(`[...document.querySelectorAll('[role=radiogroup][aria-label="Sending your voice"] label')].find((l) => l.textContent.trim() === 'Voice activity').querySelector('input').click(), true`);
        await b.key('Escape');
        await waitFor(async () => !(await b.eval(`!!${SETTINGS}`)), 'the settings closing');

        step("every section of Manage, the owner's");
        await openManage(b, 'General');
        await waitFor(() => b.eval(`!!${MANAGE}?.querySelector('input[type=url]')`), "Manage's General");
        const manage = await b.eval(`[...document.querySelectorAll('[role=dialog][aria-label^="Manage "] nav button')].map((x) => x.textContent.trim())`);
        c.report(manage.join('|') === 'General|Invites|Bans|Soundboard|Emoji|Limits|Devices|Password and recovery|Your code|Your files',
            `Manage lists the owner's sections: ${manage.join(', ')}`);
        for (const name of manage) {
            await openManage(b, name);
            await sleep(900);
            await h.snap(`manage-${slug(name)}`, { scroll: PANEL, max: 4 });
            const wrapped = await b.eval(WRAPPED);
            c.report(wrapped.length === 0, `Manage's ${name}: every hint reads in a line${wrapped.length ? `; wrapped: ${wrapped.join('; ')}` : ''}`);
        }
        await b.key('Escape');
        await waitFor(async () => !(await b.eval(`!!${MANAGE}`)), 'Manage closing');

    }

    if (runs('width')) {
        step("Appearance's Width");
        await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
        await waitFor(() => b.eval(`document.querySelectorAll('[data-scroller] [data-id]').length >= 5`), "general's messages");
        // measure takes the pane, and the header's, the list's and the foot's
        // content, each by its left edge and width.
        const measure = () => b.eval(`(() => {
            const box = (e) => { const r = e.getBoundingClientRect(); return [Math.round(r.left), Math.round(r.width)]; };
            const pane = document.querySelector('[data-pane=chan]');
            const scroller = document.querySelector('[data-scroller]');
            const header = pane.querySelector('header');
            return { pane: box(pane), header: box(header.firstElementChild), list: box(scroller.firstElementChild), scroller: box(scroller),
                foot: box(document.querySelector('textarea[placeholder^="Message "]').closest('[data-scroller] ~ div')) };
        })()`);
        const pick = async (choice) => {
            await openSettings(b, 'Appearance');
            await waitFor(() => b.eval(`!!document.querySelector('[data-width]')`), 'the Width choices');
            await b.eval(`[...document.querySelectorAll('[data-width] label')].find((l) => l.textContent.trim() === ${JSON.stringify(choice)}).querySelector('input').click(), true`);
            await sleep(200);
            await b.key('Escape');
            await waitFor(async () => !(await b.eval(`!!${SETTINGS}`)), 'the settings closing');
            await sleep(300);
            return measure();
        };
        const centered = (m, part, max) => {
            const [left, width] = m[part];
            const [paneLeft, paneWidth] = m.pane;
            const inset = (paneWidth - width) / 2;
            return width <= max && width >= max - 2 && Math.abs(left - (paneLeft + inset)) <= 17;
        };
        const full = await pick('Full');
        const wide = await pick('Wide');
        const narrow = await pick('Narrow');
        await b.shot(`${HOME}/shot-settings-${which}-narrow.png`);
        const back = await pick('Full');
        c.report(full.pane[1] >= 1300, `the pane is wide enough for the check: ${full.pane[1]} pixels`);
        c.report(full.list[1] >= full.pane[1] - 40 && full.header[1] >= full.pane[1] - 40 && full.foot[1] >= full.pane[1] - 2,
            `Full lets the header, the list and the foot take the pane: ${JSON.stringify(full)}`);
        c.report(centered(wide, 'list', 1200) && centered(wide, 'header', 1200) && centered(wide, 'foot', 1200),
            `Wide holds the header, the list and the foot to 1,200 pixels, centered: ${JSON.stringify(wide)}`);
        c.report(centered(narrow, 'list', 900) && centered(narrow, 'header', 900) && centered(narrow, 'foot', 900),
            `Narrow holds them to 900, centered: ${JSON.stringify(narrow)}`);
        c.report(narrow.scroller[1] === full.scroller[1], `the scroller keeps the pane's width, its scrollbar at the edge: ${narrow.scroller[1]} of ${full.scroller[1]}`);
        c.report(back.list[1] === full.list[1], `Full again lets the list go: ${back.list[1]}`);
        const kept = await b.eval(`localStorage.getItem('DENS_WIDTH')`);
        c.report(kept === 'full', `the browser keeps the choice: ${kept}`);
    }

    if (runs('themes')) {
        step('a theme of her own: the editor, and Cancel');
        await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
        await waitFor(() => b.eval(`document.querySelectorAll('[data-scroller] [data-id]').length >= 5`), "general's messages");
        await openSettings(b, 'Appearance');
        await waitFor(() => b.eval(`!!${OWN}`), 'Your themes');
        // Dracula, so the editor has colors other than Light's or Dark's to
        // start from.
        const DRACULA = `document.querySelector('[role=radiogroup][aria-label="More themes"] input[value=dracula]')`;
        if (!(await b.eval(`!!${DRACULA}`))) await press(b, 'More themes', SETTINGS);
        await waitFor(() => b.eval(`!!${DRACULA}`), 'the other themes');
        await b.eval(`${DRACULA}.click(), true`);
        await sleep(300);
        const dracula = await wears(b);
        c.report(dracula.theme === 'dracula' && dracula.set === 0, `Dracula sets nothing on the root: ${q(dracula.styled)}`);
        await press(b, 'New theme');
        await waitFor(() => b.eval(`!!${EDITOR}`), 'the editor');
        const started = JSON.parse(await b.eval(`JSON.stringify({
            name: ${EDITOR}.querySelector('input[type=text]').value,
            colors: [...${EDITOR}.querySelectorAll('[data-color]')].map((r) => [r.dataset.color, r.querySelector('input[type=color]').value, r.querySelector('input[type=text]').value]),
            base: ${EDITOR}.querySelector('[data-theme-base] input:checked').value, corners: ${EDITOR}.querySelector('[data-theme-corners] input:checked').value })`));
        const starts = Object.fromEntries(started.colors.map(([label, picked]) => [label, picked]));
        c.report(started.colors.map(([label]) => label).join('|') === 'Background|Text|Primary|Secondary|Accent' && started.colors.every(([, picked, typed]) => picked === typed),
            `the editor asks for five colors, each picked and as hex: ${q(started.colors)}`);
        c.report(starts.Background === dracula.base && starts.Text === dracula.text && starts.Primary === dracula.primary && starts.Secondary === dracula.secondary
            && starts.Accent === dracula.accent && started.base === 'dark' && started.corners === 'rounded',
        `it starts from the theme the page shows, Dracula: ${q(started)} of ${q(dracula)}`);
        const before = await wears(b);
        c.report(before.page === dracula.page && before.error === dracula.error && before.theme === 'dark',
            `opened, the page looks as it did, over Dark, the error color carried: ${q(before)}`);
        await set(b, field('Background', 'text'), '#102030');
        await sleep(250);
        const typed = await wears(b);
        c.report(typed.page === '#102030' && typed.base === '#102030' && typed.base200 !== '#102030' && typed.base300 !== typed.base200
            && (await b.eval(`document.querySelector(${q(field('Background', 'color'))}).value`)) === '#102030',
        `a background typed as hex is the page's at once, the deeper two derived, the picker following: ${q(typed)}`);
        await press(b, 'Cancel', EDITOR);
        await waitFor(async () => !(await b.eval(`!!${EDITOR}`)), 'the editor closing');
        const canceled = await wears(b);
        c.report(canceled.theme === 'dracula' && canceled.page === dracula.page && canceled.set === 0 && (await ownNames(b)) === ''
            && (await b.eval(`localStorage.getItem('DENS_THEMES')`)) === null,
        `Cancel puts Dracula back and keeps nothing: ${q(canceled)}`);

        step('the theme made, saved and chosen');
        await press(b, 'New theme');
        await waitFor(() => b.eval(`!!${EDITOR}`), 'the editor');
        await set(b, '[data-theme-editor] label input[type=text]', 'Check theme');
        await choose(b, 'data-theme-base', 'Light');
        await set(b, field('Background', 'text'), '#f4efe6');
        await set(b, field('Text', 'text'), '2a2118');
        await set(b, field('Primary', 'color'), '#c2410c');
        await set(b, field('Secondary', 'text'), '#0af');
        await set(b, field('Accent', 'color'), '#22cc88');
        await choose(b, 'data-theme-corners', 'Square');
        await sleep(300);
        const made = await wears(b);
        const wanted = { theme: 'light', page: '#f4efe6', text: '#2a2118', base: '#f4efe6', primary: '#c2410c', secondary: '#00aaff', accent: '#22cc88', button: '#c2410c', radius: '0px' };
        const holds = (got, want) => Object.entries(want).every(([k, v]) => got[k] === v);
        c.report(holds(made, wanted) && made.error === dracula.error,
            `the page wears the theme as it's made, every color and the corners, its Save button among them: ${q(made)}`);
        c.report(/^(--[a-z0-9-]+: (#[0-9a-f]{6}|[0-9.]+rem); ?)+$/.test(made.styled),
            `the root's style holds variables alone, each a hex color or a radius: ${q(made.styled)}`);
        await b.shot(`${HOME}/shot-settings-${which}-editor.png`);
        await h.snap('theme-editor', { scroll: PANEL, max: 3 });
        const editorWrapped = await b.eval(WRAPPED);
        c.report(editorWrapped.length === 0, `the editor: every hint reads in a line${editorWrapped.length ? `; wrapped: ${editorWrapped.join('; ')}` : ''}`);
        await press(b, 'Save', EDITOR);
        await waitFor(async () => !(await b.eval(`!!${EDITOR}`)), 'the editor closing');
        const saved = await wears(b);
        const stored = await b.eval(`localStorage.getItem('DENS_THEME')`);
        c.report(holds(saved, { ...wanted, button: saved.button, radius: saved.radius }) && (await ownNames(b)) === 'Check theme*' && /^custom:[a-z0-9]{8}$/.test(stored),
            `saved, it's chosen among Your themes, which the browser keeps: ${await ownNames(b)}, ${stored}, ${q(saved)}`);
        const swatch = await b.eval(`(() => { const hex = ${HEX}; const s = document.querySelector('[role=radiogroup][aria-label="Your themes"] [data-theme]');
            return [hex(getComputedStyle(s).backgroundColor), ...[...s.children].map((d) => hex(getComputedStyle(d).backgroundColor))].join(' '); })()`);
        c.report(swatch === '#f4efe6 #2a2118 #c2410c #00aaff #22cc88', `its swatch shows its colors: ${swatch}`);
        await h.snap('theme-own', { scroll: PANEL, max: 3 });
        await closeSettings(b);
        await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
        await waitFor(() => b.eval(`document.querySelectorAll('[data-scroller] [data-id]').length >= 5`), "general's messages");
        const reloaded = await wears(b);
        c.report(holds(reloaded, { theme: 'light', page: '#f4efe6', text: '#2a2118', primary: '#c2410c' }), `a reload wears it from the start: ${q(reloaded)}`);
        await b.shot(`${HOME}/shot-settings-${which}-theme.png`);

        step('edited, and its code copied');
        await openSettings(b, 'Appearance');
        await waitFor(() => b.eval(`!!${OWN}`), 'Your themes');
        await press(b, 'Edit');
        await waitFor(() => b.eval(`!!${EDITOR}`), 'the editor');
        c.report((await b.eval(`${EDITOR}.querySelector('input[type=text]').value`)) === 'Check theme'
            && (await b.eval(`document.querySelector(${q(field('Primary', 'text'))}).value`)) === '#c2410c', 'Edit opens the editor at the theme');
        await set(b, field('Primary', 'text'), '#7c3aed');
        await choose(b, 'data-theme-corners', 'Round');
        await press(b, 'Save', EDITOR);
        await waitFor(async () => !(await b.eval(`!!${EDITOR}`)), 'the editor closing');
        const edited = await wears(b);
        c.report(edited.primary === '#7c3aed' && edited.page === '#f4efe6' && (await ownNames(b)) === 'Check theme*', `the edit took the theme's place: ${q(edited)}`);
        await b.eval(`(() => { window.copied = []; navigator.clipboard.writeText = async (t) => { window.copied.push(t); }; return true; })()`);
        await press(b, 'Copy code');
        const code = await waitFor(async () => JSON.parse(await b.eval('JSON.stringify(window.copied || [])'))[0], 'the copied code');
        c.report(/^dens-theme:1\.l\.o\.f4efe62a21187c3aed00aaff22cc88[0-9a-f]{30}\.[0-9a-f]+$/.test(code), `Copy code gives the theme as a code: ${code}`);
        await closeSettings(b);

        step("pasted into bob's browser, and a stranger's codes refused");
        b2 = await launch(which, which === 'chromium' ? 9573 : 9574, { size: [1400, 900], hover: true });
        await pair(b2, BOB);
        await openSettings(b2, 'Appearance');
        await waitFor(() => b2.eval(`!!${OWN}`), "bob's themes");
        const plain = await wears(b2);
        const hostile = [
            ['a url where a color goes', code.replace(/\.[0-9a-f]{60}\./, `.${code.split('.')[3].slice(0, 30)}url(https://evil.example/x.png)).`)],
            ['a color in words', code.replace(/\.[0-9a-f]{60}\./, '.red.')],
            ['a field more', `${code}.2f2a`],
            ['a later version', code.replace(':1.', ':2.')],
            ['a name of 41 characters', `${code.split('.').slice(0, 4).join('.')}.${'78'.repeat(41)}`],
            ["a name that isn't text", `${code.split('.').slice(0, 4).join('.')}.ff`],
            ['a style', 'dens-theme:1.l.o.;background:url(https://evil.example/x.png).78'],
            ['JSON', 'dens-theme:{"name":"x","base":"url(https://evil.example/x.png)"}'],
        ];
        await press(b2, 'Paste a code');
        const refused = [];
        for (const [what, bad] of hostile) {
            await set(b2, '[data-own-themes] form input', bad);
            await press(b2, 'Add');
            const said = await b2.eval(`${OWN}.querySelector('[role=alert], .text-error')?.textContent.trim() || ''`);
            const after = await wears(b2);
            if (said !== "That isn't a theme's code." || after.set !== 0 || after.page !== plain.page || (await ownNames(b2)) !== '') refused.push(`${what}: ${q(said)}, ${q(after.styled)}`);
        }
        c.report(refused.length === 0 && (await b2.eval(`localStorage.getItem('DENS_THEMES')`)) === null,
            `each of a stranger's ${hostile.length} codes is refused, and nothing of them kept or styled${refused.length ? `: ${refused.join('; ')}` : ''}`);
        await b2.shot(`${HOME}/shot-settings-${which}-refused.png`);
        await set(b2, '[data-own-themes] form input', `  ${code}\n`);
        await press(b2, 'Add');
        await waitFor(async () => (await ownNames(b2)) === 'Check theme*', "the pasted theme among bob's");
        const pasted = await wears(b2);
        c.report(holds(pasted, { theme: 'light', page: '#f4efe6', text: '#2a2118', primary: '#7c3aed', secondary: '#00aaff', accent: '#22cc88', error: edited.error })
            && pasted.base200 === edited.base200 && pasted.base300 === edited.base300,
        `bob's page wears alice's theme, color for color: ${q(pasted)}`);
        await b2.shot(`${HOME}/shot-settings-${which}-pasted.png`);

        step('deleted');
        await openSettings(b, 'Appearance');
        await waitFor(() => b.eval(`!!${OWN}`), 'Your themes');
        await press(b, 'Delete');
        await waitFor(async () => (await ownNames(b)) === '', 'the theme leaving the list');
        const deleted = await wears(b);
        c.report(['light', 'dark'].includes(deleted.theme) && deleted.set === 0 && (await b.eval(`localStorage.getItem('DENS_THEME')`)) === null
            && (await b.eval(`localStorage.getItem('DENS_THEMES')`)) === null,
        `deleted, the page follows the system again, with nothing left on the root: ${q(deleted)}`);
        await closeSettings(b);
    }

    if (runs('background')) {
        step('a picture behind the chat');
        b2 ||= await launch(which, which === 'chromium' ? 9573 : 9574, { size: [1400, 900], hover: true });
        const general$ = `http://localhost:${ALICE.port}/den/${den}/${general}`;
        const light = async (on) => {
            await openSettings(on, 'Appearance');
            await on.eval(`document.querySelector('[role=radiogroup][aria-label=Theme] input[value=light]').click(), true`);
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
        await openSettings(b, 'Appearance');
        const CARD = `[...document.querySelectorAll('[role=dialog] section')].find((s) => s.querySelector('h2')?.textContent.trim() === 'Chat background')`;
        await waitFor(() => b.eval(`!!${CARD}`), 'the Chat background card');
        await b.eval(`${CARD}.querySelector('input[type=file]').dataset.check = 'background', true`);
        const gif = `${HOME}/background.gif`;
        fs.writeFileSync(gif, Buffer.from('R0lGODlhAQABAIAAAP8AAAAAACH5BAAAAAAALAAAAAABAAEAAAICRAEAOw==', 'base64'));
        await b.attach('input[data-check=background]', [gif]);
        await waitFor(() => b.eval(`/PNG, a JPEG or a WebP/.test(${CARD}.textContent)`), 'the GIF refused');
        c.report((await behind(b, 'background-gif')).image === 'none', 'a GIF is refused, and nothing shows');
        await b.attach('input[data-check=background]', [png(`${HOME}/background.png`, 64, 64, [255, 0, 0])]);
        await waitFor(() => b.eval(`!!${CARD}.querySelector('[data-background-preview] img')?.naturalWidth`), 'the picture in the card');
        await h.snap('background-card', { scroll: PANEL, max: 3 });
        const cardWrapped = await b.eval(WRAPPED);
        c.report(cardWrapped.length === 0, `Chat background: every hint reads in a line${cardWrapped.length ? `; wrapped: ${cardWrapped.join('; ')}` : ''}`);
        c.report((await b.eval(`${CARD}.querySelector('input[type=range]').value`)) === '70' && (await b.eval(`[...${CARD}.querySelectorAll('button')].map((x) => x.textContent.trim()).join('|')`)) === 'Change image|Remove',
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
        await openSettings(b, 'Appearance');
        await waitFor(() => b.eval(`!!${CARD}?.querySelector('input[type=range]')`), 'the Dim slider');
        await b.eval(`(() => { const r = ${CARD}.querySelector('input[type=range]'); r.dataset.check = 'dim'; return true; })()`);
        await set(b, 'input[data-check=dim]', '0', ['input', 'change']);
        await closeSettings(b);
        const clear = await waitFor(async () => { const s = await behind(b, 'background-0'); return s.pixel.join() === '255,0,0' && s; }, 'the picture undimmed');
        c.report(clear.pixel.join() === '255,0,0', `at 0% the picture shows as it is: ${q(clear.pixel)}`);
        await openSettings(b, 'Appearance');
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
        await openSettings(b, 'Appearance');
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
