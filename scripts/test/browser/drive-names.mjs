// The M19.5 check of name styles, in the browser named: alice picks a
// font and a gradient in Edit profile, the fonts as tiles in their own
// faces and the name over them as it will show, and saves; bob's page, on
// Dark, shows her name in them in the member list, a message, a reply's
// quote, a call, the DM list, a DM's header and her profile card, the
// gradient's dark end lightened until it reads against Dark's backgrounds
// and its hue kept, by the page's styles and by the screen's own pixels,
// with no row a pixel taller for it, and only her font fetched; on Light,
// chosen while the page shows, the pale end darkened instead; carol's
// Cyrillic name draws in the page's font, her font never fetched; bob's
// Name styles at Colors only drops the fonts, and at Off shows every name
// plain, which a reload keeps; and alice clears her style.
//
//   run.sh names chromium|firefox
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import zlib from 'node:zlib';
import { ALICE, BOB, HOME, SETTINGS, checks, launch, lib, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which = 'chromium'] = process.argv;
const { DEN: den, GENERAL: general, LOUNGE: lounge, DM: dm, FIRST: first, SECOND: second, REPLY: reply } = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);
const q = JSON.stringify;

// The colors alice picks: a blue too dark for Dark, and a yellow too pale
// for Light.
const FROM = '#0b1f5c';
const TO = '#ffe066';
const READS = 3;

const state = () => JSON.parse(lib('api', 'bob', 'GET', `/api/dens/${den}/state`));
const styleOf = (username) => state().members.find((m) => m.username === username)?.name_style;

// Colors, as the page's own check does: WCAG's contrast, and a hue in
// degrees.
const rgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
const lum = (hex) => {
    const [r, g, b] = rgb(hex).map((v) => v / 255).map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const contrast = (a, b) => (Math.max(lum(a), lum(b)) + 0.05) / (Math.min(lum(a), lum(b)) + 0.05);
const hueOf = ([r, g, b]) => {
    const max = Math.max(r, g, b), d = max - Math.min(r, g, b);
    if (!d) return NaN;
    const h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
    return (h * 60 + 360) % 360;
};
const apart = (a, b) => Math.abs(((a - b + 540) % 360) - 180);

// picture is a screenshot's pixels, a PNG of 8 bits a channel as the
// browsers take them: at(x, y) is one, as [r, g, b].
function picture(file) {
    const data = fs.readFileSync(file);
    const w = data.readUInt32BE(16), h = data.readUInt32BE(20);
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
    const rows = [];
    let prev = Buffer.alloc(stride);
    for (let row = 0; row < h; row++) {
        const filter = raw[row * (stride + 1)];
        const line = Buffer.from(raw.subarray(row * (stride + 1) + 1, (row + 1) * (stride + 1)));
        for (let i = 0; i < stride; i++) {
            const a = i >= n ? line[i - n] : 0;
            const up = prev[i];
            const cc = i >= n ? prev[i - n] : 0;
            const p = a + up - cc;
            const paeth = Math.abs(p - a) <= Math.abs(p - up) && Math.abs(p - a) <= Math.abs(p - cc) ? a : Math.abs(p - up) <= Math.abs(p - cc) ? up : cc;
            line[i] = (line[i] + [0, a, up, (a + up) >> 1, paeth][filter]) & 255;
        }
        rows.push(line);
        prev = line;
    }
    return { w, h, at: (x, y) => [...rows[y].subarray(x * n, x * n + 3)] };
}

// vivid is the most colorful pixel of a box of a picture, which a name's
// glyphs hold where it's colored.
function vivid(pic, x0, y0, x1, y1) {
    let best = [0, 0, 0], most = -1;
    for (let y = Math.max(0, Math.floor(y0)); y < Math.min(pic.h, Math.ceil(y1)); y++) {
        for (let x = Math.max(0, Math.floor(x0)); x < Math.min(pic.w, Math.ceil(x1)); x++) {
            const p = pic.at(x, y);
            const sat = Math.max(...p) - Math.min(...p);
            if (sat > most) [best, most] = [p, sat];
        }
    }
    return best;
}

const HEX = `((c) => { const x = document.createElement('canvas').getContext('2d', { willReadFrequently: true }); x.fillStyle = c; x.fillRect(0, 0, 1, 1);
    const d = x.getImageData(0, 0, 1, 1).data; return '#' + [d[0], d[1], d[2]].map((v) => v.toString(16).padStart(2, '0')).join(''); })`;
// look is how a name's element shows: its font, its color, a gradient's
// ends, its box, and how wide its text would run in the page's own font.
const look = (on, el) => on.eval(`(() => {
    const el = ${el};
    if (!el) return '';
    el.scrollIntoView({ block: 'nearest' });
    const hex = ${HEX};
    const s = getComputedStyle(el);
    const probe = document.createElement('span');
    probe.textContent = el.textContent;
    probe.style.cssText = 'position:absolute;visibility:hidden;white-space:nowrap';
    el.parentElement.appendChild(probe);
    probe.style.fontWeight = s.fontWeight;
    probe.style.fontSize = s.fontSize;
    const plain = probe.getBoundingClientRect().width;
    probe.style.fontFamily = s.fontFamily;
    const own = probe.getBoundingClientRect().width;
    probe.remove();
    const r = el.getBoundingClientRect();
    return JSON.stringify({ text: el.textContent, font: s.fontFamily.split(',')[0].replace(/"/g, ''), color: s.color.replace(/ /g, '') === 'rgba(0,0,0,0)' ? 'transparent' : hex(s.color),
        ends: (s.backgroundImage.match(/rgba?\\([^)]*\\)/g) || []).map(hex), clip: s.backgroundClip || s.webkitBackgroundClip || '', x: r.x, y: r.y, w: r.width, h: r.height,
        plain: Math.round(plain * 10) / 10, own: Math.round(own * 10) / 10 });
})()`).then((r) => (r ? JSON.parse(r) : null));
const THEME = `JSON.stringify((() => { const hex = ${HEX}; const s = getComputedStyle(document.documentElement);
    return { backs: ['--color-base-100', '--color-base-200', '--color-base-300'].map((v) => hex(s.getPropertyValue(v))), text: hex(s.getPropertyValue('--color-base-content')) }; })())`;
// fetched are the names' fonts a page's browser asked for: it's given all
// eight, and loads one only as a name on screen uses it.
const fetched = (on) => on.eval(`JSON.stringify([...document.fonts].filter((f) => f.family.includes('Dens name') && f.status !== 'unloaded')
    .map((f) => f.family.replace(/"/g, '').slice('Dens name '.length)))`).then(JSON.parse);
const named = (scope, text) => `[...document.querySelectorAll(${q(`${scope} [data-name]`)})].find((n) => n.textContent === ${q(text)})`;
// set types into a field the page controls, as a member's typing would.
const set = (on, selector, value) => on.eval(`(() => {
    const el = document.querySelector(${q(selector)});
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, ${q(value)});
    el.dispatchEvent(new Event('input', { bubbles: true }));
    return true;
})()`);
const PROFILE = `document.querySelector('[role=dialog][aria-label="Your profile in this den"]')`;
const FIELDS = `document.querySelector('[data-name-style]')`;
const PREVIEW = `document.querySelector('[data-name-preview] [data-name]')`;

const base = which === 'chromium' ? 9581 : 9583;
const alice = await launch(which, base, { size: [1400, 900], hover: true, media: true });
const bob = await launch(which, base + 1, { size: [1400, 900], hover: true });
const shot = (on, name) => on.shot(`${HOME}/shot-names-${which}-${name}.png`);

async function editProfile() {
    await alice.eval(`document.querySelector('button[aria-label="Edit your profile"]').click(), true`);
    await waitFor(() => alice.eval(`!!${FIELDS}`), "Edit profile's Name style");
}
async function save() {
    await alice.eval(`${PROFILE}.querySelector('button[type=submit]').click(), true`);
    await waitFor(async () => !(await alice.eval(`!!${PROFILE}`)), 'the profile saved, its dialog closing');
}

try {
    await pair(alice, ALICE);
    await pair(bob, BOB);
    await bob.eval(`localStorage.setItem('DENS_THEME', 'dark'), true`);
    await alice.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
    await bob.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
    await waitFor(() => bob.eval(`document.querySelectorAll('[data-scroller] [data-id]').length >= 4 && !!${named('nav[aria-label=Members]', 'Alice')}`), "general on bob's page");
    const aliceID = state().members.find((m) => m.username === 'alice').id;
    const bobsFonts = [];

    step('before any style');
    const LIST = named('nav[aria-label=Members]', 'Alice');
    const plainList = await look(bob, LIST);
    const plainRow = await bob.eval(`${LIST}.closest('li').getBoundingClientRect().height`);
    const heights = () => bob.eval(`JSON.stringify([${q(first)}, ${q(second)}].map((id) => document.querySelector('[data-id="' + id + '"]').getBoundingClientRect().height))`).then(JSON.parse);
    const plainHeights = await heights();
    c.report(plainList.ends.length === 0 && !plainList.font.startsWith('Dens name') && plainHeights[0] === plainHeights[1],
        `alice's name starts plain, her message as tall as bob's: ${q(plainList)}, ${q(plainHeights)}`);

    step('alice picks a font and a gradient in Edit profile');
    await waitFor(() => alice.eval(`!!document.querySelector('button[aria-label="Edit your profile"]')`), "alice's own bar");
    await editProfile();
    const tiles = JSON.parse(await alice.eval(`JSON.stringify([...${FIELDS}.querySelectorAll('[data-name-fonts] label')].map((l) =>
        [l.textContent.trim(), getComputedStyle(l.querySelector('span')).fontFamily.split(',')[0].replace(/"/g, ''), l.querySelector('input').value]))`));
    c.report(tiles.length === 9 && tiles[0][0] === 'Default' && !tiles[0][1].startsWith('Dens name') && tiles.slice(1).every(([, family, id]) => family === `Dens name ${id}`),
        `the fonts are tiles, Default and eight, each in its own face: ${q(tiles.map(([label, family]) => `${label}: ${family}`))}`);
    await waitFor(() => alice.eval(`[...document.fonts].filter((f) => f.family.includes('Dens name') && f.status === 'loaded').length === 8`), "the eight faces loaded for the picker's tiles");
    await alice.eval(`${FIELDS}.querySelector('input[name=name-font][value=pacifico]').click(), true`);
    await alice.eval(`${FIELDS}.querySelector('[data-name-color] input[value=gradient]').click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-name-style] [data-color="To"] input[type=text]')`), 'the two colors');
    await set(alice, '[data-name-style] [data-color="From"] input[type=text]', FROM);
    await set(alice, '[data-name-style] [data-color="To"] input[type=text]', TO.slice(1).toUpperCase());
    await sleep(300);
    const aliceTheme = JSON.parse(await alice.eval(THEME));
    const preview = await look(alice, PREVIEW);
    c.report(preview.text === 'Alice' && preview.font === 'Dens name pacifico' && preview.ends.length === 2 && preview.color === 'transparent' && preview.clip === 'text'
        && preview.ends.every((e) => aliceTheme.backs.every((bk) => contrast(e, bk) >= READS - 0.05)),
    `the name shows over the fields as it will on her theme, in Pacifico, a gradient whose ends read there: ${q(preview)} on ${q(aliceTheme.backs)}`);
    const hints = JSON.parse(await alice.eval(`JSON.stringify([...${FIELDS}.querySelectorAll('p')].map((p) =>
        [p.textContent.trim(), Math.round(p.getBoundingClientRect().height / (parseFloat(getComputedStyle(p).lineHeight) || 16))]))`));
    c.report(hints.length === 2 && hints.every(([, lines]) => lines === 1), `its two hints read in a line each: ${q(hints)}`);
    await shot(alice, 'editor');
    await save();
    await waitFor(() => q(styleOf('alice')) === q({ font: 'pacifico', color: FROM, color2: TO }), "alice's style in bob's den state");
    c.report(true, `saved, the den carries her style with her: ${q(styleOf('alice'))}`);
    const own = await waitFor(async () => {
        const l = await look(alice, `document.querySelector('button[aria-label="Edit your profile"] [data-name]')`);
        return l?.font === 'Dens name pacifico' && l.ends.length === 2 && l;
    }, "alice's own bar wearing it");
    c.report(true, `her own bar shows it: ${q(own.ends)}`);

    step("bob's page on Dark shows it wherever her name is");
    const dark = JSON.parse(await bob.eval(THEME));
    const worn = (l) => l && l.text === 'Alice' && l.font === 'Dens name pacifico' && l.ends.length === 2 && l.color === 'transparent' && l.clip === 'text';
    const inList = await waitFor(async () => { const l = await look(bob, LIST); return worn(l) && l; }, "alice's styled name in bob's member list");
    await waitFor(() => bob.eval(`document.fonts.check('16px "Dens name pacifico"') && [...document.fonts].some((f) => f.family.includes('pacifico') && f.status === 'loaded')`), 'Pacifico loaded on bob\'s page');
    const [from, to] = inList.ends;
    c.report(to === TO && from !== FROM && lum(from) > lum(FROM) && dark.backs.every((bk) => contrast(from, bk) >= READS - 0.05) && contrast(FROM, dark.backs[0]) < READS
        && apart(hueOf(rgb(from)), hueOf(rgb(FROM))) < 15 && contrast(from, dark.backs[2]) < READS + 0.7,
    `on Dark the blue shows lightened until it reads, no further, still blue, and the yellow as picked: ${from} (${dark.backs.map((bk) => contrast(from, bk).toFixed(2))}) and ${to}, from ${FROM} (${contrast(FROM, dark.backs[0]).toFixed(2)})`);

    // A call's row: alice joins Lounge.
    await alice.eval(`document.querySelector('[data-channel-row="${lounge}"] button[title^="Join the call"]').click(), true`, true);
    await waitFor(() => bob.eval(`!!document.querySelector('[data-call-member="${aliceID}"] [data-name]')`), "alice's row in Lounge's call on bob's page", 30000);
    // Her card, from the member list.
    await bob.eval(`${LIST}.closest('button').click(), true`);
    await waitFor(() => bob.eval(`!!document.querySelector('[data-profile] [data-name]')`), "alice's profile card");
    const places = {
        'the member list': LIST,
        'her message': `document.querySelector('[data-id="${first}"] button [data-name]')`,
        "a reply's quote": `document.querySelector('[data-id="${reply}"] > button [data-name]')`,
        "the call's row": `document.querySelector('[data-call-member="${aliceID}"] [data-name]')`,
        'her profile card': `document.querySelector('[data-profile] [data-name]')`,
    };
    const seen = {};
    for (const [where, el] of Object.entries(places)) seen[where] = await look(bob, el);
    await shot(bob, 'dark-card');
    await bob.eval(`document.querySelector('[data-profile]').closest('[role=dialog]').querySelector('button[aria-label="Close"]').click(), true`);
    await waitFor(async () => !(await bob.eval(`!!document.querySelector('[data-profile]')`)), 'the card closing');
    await bob.go(`http://localhost:${BOB.port}/den/${den}/${dm}`);
    await waitFor(() => bob.eval(`!!${named('nav[aria-label="Direct messages"]', 'Alice')} && !!document.querySelector('header [data-name]')`), "the DM on bob's page");
    seen['the DM list'] = await look(bob, named('nav[aria-label="Direct messages"]', 'Alice'));
    seen["the DM's header"] = await look(bob, `document.querySelector('header [data-name]')`);
    await shot(bob, 'dark-dm');
    const off = Object.entries(seen).filter(([, l]) => !worn(l) || l.ends[0] !== from || l.ends[1] !== to || Math.abs(l.own - l.plain) < 1).map(([where, l]) => `${where}: ${q(l)}`);
    c.report(off.length === 0, `her name wears the font and the same two colors in ${Object.keys(seen).join(', ')}${off.length ? `; not in ${off.join('; ')}` : ''}`);

    step('by the screen, and no row taller for it');
    await bob.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
    await waitFor(async () => worn(await look(bob, places['her message'])), "general again on bob's page");
    await sleep(500);
    await shot(bob, 'dark');
    const pic = picture(`${HOME}/shot-names-${which}-dark.png`);
    const drawn = [];
    for (const where of ['the member list', 'her message', "the call's row"]) {
        const l = await look(bob, places[where]);
        const left = vivid(pic, l.x, l.y, l.x + l.w * 0.3, l.y + l.h);
        const right = vivid(pic, l.x + l.w * 0.7, l.y, l.x + l.w, l.y + l.h);
        const good = apart(hueOf(left), hueOf(rgb(from))) < 30 && apart(hueOf(right), hueOf(rgb(to))) < 30;
        drawn.push(`${good ? '' : 'NOT '}${where}: ${q(left)} to ${q(right)}`);
    }
    c.report(drawn.every((d) => !d.startsWith('NOT')), `the screen's pixels run from the blue at each name's start to the yellow at its end: ${drawn.join('; ')}`);
    const styledRow = await bob.eval(`${LIST}.closest('li').getBoundingClientRect().height`);
    const styledHeights = await heights();
    c.report(styledRow === plainRow && q(styledHeights) === q(plainHeights) && styledHeights[0] === styledHeights[1],
        `her row in the list and her message stand as tall as before, and as bob's: ${styledRow} and ${q(styledHeights)}, from ${plainRow} and ${q(plainHeights)}`);
    // The quote over a reply is as wide as its words, not the pane.
    const quote = JSON.parse(await bob.eval(`JSON.stringify((() => { const q = document.querySelector('[data-id="${reply}"] > button');
        return [Math.round(q.getBoundingClientRect().width), Math.round(q.scrollWidth), Math.round(document.querySelector('[data-pane=chan]').getBoundingClientRect().width)]; })())`));
    c.report(quote[0] < 320 && quote[0] >= quote[1] && quote[2] > 800, `a reply's quote takes its words' width, ${quote[0]} of the pane's ${quote[2]}`);
    bobsFonts.push(...await fetched(bob));
    c.report(q(bobsFonts) === q(['pacifico']), `bob's browser fetched her font alone: ${q(bobsFonts)}`);

    step("carol's Cyrillic name draws in the page's font");
    const carol = await look(bob, named('nav[aria-label=Members]', 'Карина'));
    c.report(carol.font === 'Dens name bangers' && carol.color !== 'transparent' && carol.ends.length === 0 && Math.abs(carol.own - carol.plain) < 0.2
        && dark.backs.every((bk) => contrast(carol.color, bk) >= READS - 0.05) && !(await fetched(bob)).includes('bangers'),
    `her name takes her color and names her font, but runs as wide as in the page's own, the font never fetched: ${q(carol)}`);

    step('on Light, chosen while the page shows');
    await openSettings(bob, 'Appearance');
    await bob.eval(`${SETTINGS}.querySelector('input[name=theme][value=light]').click(), true`);
    const lit = await waitFor(async () => { const l = await look(bob, LIST); return l.ends[0] === FROM && l; }, "alice's name on Light, without a reload");
    const light = JSON.parse(await bob.eval(THEME));
    c.report(lit.ends[0] === FROM && lit.ends[1] !== TO && lum(lit.ends[1]) < lum(TO) && light.backs.every((bk) => contrast(lit.ends[1], bk) >= READS - 0.05)
        && apart(hueOf(rgb(lit.ends[1])), hueOf(rgb(TO))) < 15 && q(light.backs) !== q(dark.backs),
    `on Light the blue shows as picked and the yellow darkened until it reads, still yellow: ${lit.ends[0]} and ${lit.ends[1]} (${light.backs.map((bk) => contrast(lit.ends[1], bk).toFixed(2))})`);
    await shot(bob, 'light');

    step("bob's Name styles: Colors only, then Off");
    const pick = (value) => bob.eval(`${SETTINGS}.querySelector('[data-name-styles] input[value=${value}]').click(), true`);
    c.report(await bob.eval(`${SETTINGS}.querySelector('[data-name-styles] input[value=all]').checked`), 'Name styles starts at Colors and fonts');
    await pick('colors');
    const colors = await waitFor(async () => { const l = await look(bob, LIST); return !l.font.startsWith('Dens name') && l; }, 'the fonts going');
    const carolColors = await look(bob, named('nav[aria-label=Members]', 'Карина'));
    c.report(q(colors.ends) === q(lit.ends) && colors.clip === 'text' && Math.abs(colors.own - colors.plain) < 0.2 && !carolColors.font.startsWith('Dens name') && carolColors.color !== plainList.color,
        `at Colors only her name keeps its gradient in the page's font, and carol's its color: ${q(colors)}`);
    await pick('off');
    const plain = await waitFor(async () => { const l = await look(bob, LIST); return l.ends.length === 0 && l; }, 'the colors going');
    const bobsOwn = await look(bob, named('nav[aria-label=Members]', 'Bob'));
    const carolOff = await look(bob, named('nav[aria-label=Members]', 'Карина'));
    c.report(plain.color === bobsOwn.color && plain.color !== 'transparent' && !plain.font.startsWith('Dens name') && plain.clip !== 'text' && carolOff.color === bobsOwn.color,
        `at Off every name shows as bob's own does: ${q(plain)}`);
    await bob.key('Escape');
    await bob.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
    const kept = await waitFor(() => look(bob, places['her message']), "general after a reload");
    c.report(kept.ends.length === 0 && !kept.font.startsWith('Dens name') && (await bob.eval(`localStorage.getItem('DENS_NAME_STYLES')`)) === 'off',
        `a reload keeps Off: ${q(kept)}`);
    await openSettings(bob, 'Appearance');
    await pick('all');
    await waitFor(async () => worn(await look(bob, LIST)), 'the style back at Colors and fonts');
    await bob.key('Escape');

    step('alice clears her style');
    await editProfile();
    c.report(await alice.eval(`${FIELDS}.querySelector('input[name=name-font][value=pacifico]').checked && ${FIELDS}.querySelector('[data-name-color] input[value=gradient]').checked
        && document.querySelector('[data-name-style] [data-color="From"] input[type=text]').value === ${q(FROM)}`), 'Edit profile opens at her style');
    await alice.eval(`${FIELDS}.querySelector('input[name=name-font][value=""]').click(), true`);
    await alice.eval(`${FIELDS}.querySelector('[data-name-color] input[value=none]').click(), true`);
    await sleep(200);
    await save();
    await waitFor(() => styleOf('alice') === undefined, "alice's style gone from bob's den state");
    const cleared = await waitFor(async () => { const l = await look(bob, LIST); return l.ends.length === 0 && !l.font.startsWith('Dens name') && l; }, "alice's name plain again on bob's page");
    c.report(cleared.color === (await look(bob, named('nav[aria-label=Members]', 'Bob'))).color, `cleared, her record carries no style and her name shows plain: ${q(cleared)}`);
} catch (e) {
    c.fail(e);
    await shot(alice, 'failed-alice').catch(() => {});
    await shot(bob, 'failed-bob').catch(() => {});
} finally {
    await alice.close();
    await bob.close();
}
c.finish();
