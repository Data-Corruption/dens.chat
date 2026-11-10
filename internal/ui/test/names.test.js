// Name styles, checked without a browser: a style read as the den
// checks it, by the cases they share; a color moved until it reads on a
// theme, its hue kept; the reader's three choices; the fonts given to the
// browser from the shell's list alone; and a name wearing its style where
// the page shows it, and in Edit profile, which saves it.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import cases from '../../denproto/testdata/namestyles.json';
import { LongName } from '../assets/js/src/components.jsx';
import { MessageRow } from '../assets/js/src/messages.jsx';
import {
    FONTS, READS, STYLES_ALL, STYLES_COLORS, STYLES_OFF, cleanStyle, fontFamily, nameLook, nameStyles, readable, setNameStyles,
} from '../assets/js/src/names.js';
import { Name, nameText } from '../assets/js/src/names.jsx';
import { EditProfile } from '../assets/js/src/people.jsx';
import { setTheme } from '../assets/js/src/theme.js';
import { contrast, oklch } from '../assets/js/src/themes.js';

// A page to read a theme from: a root with the theme's variables, and a
// canvas that reads a hex color back as itself.
const THEMES = {
    dark: { '--color-base-100': '#1d232a', '--color-base-200': '#191e24', '--color-base-300': '#15191e', '--color-base-content': '#ecf9ff' },
    light: { '--color-base-100': '#ffffff', '--color-base-200': '#f8f8f8', '--color-base-300': '#eeeeee', '--color-base-content': '#18181b' },
};
const root = { attributes: {}, setAttribute(k, v) { this.attributes[k] = v; }, style: { setProperty() {}, removeProperty() {} } };
const element = document.createElement;
const added = [];
Object.assign(document, {
    documentElement: root,
    fonts: { add: (face) => added.push(face) },
    getElementById: () => ({
        dataset: {
            fonts: JSON.stringify({
                pacifico: '/assets/fonts/pacifico.0a1b2c3d.woff2', orbitron: '/assets/fonts/orbitron.4e5f6a7b.woff2',
                cinzel: 'https://evil.example/cinzel.woff2', bangers: '/assets/fonts/x.woff2) format("woff2"), url(https://evil.example/x', 'comic-sans': '/assets/fonts/comic.woff2',
            }),
        },
    }),
    createElement: (name) => (name !== 'canvas' ? element(name) : {
        getContext: () => {
            let color = '';
            return {
                set fillStyle(c) { if (/^#[0-9a-f]{6}$/.test(c)) color = c; },
                get fillStyle() { return color; },
                fillRect() {},
                getImageData: () => ({ data: [1, 3, 5].map((i) => parseInt(color.slice(i, i + 2), 16)) }),
            };
        },
    }),
});
globalThis.FontFace = class {
    constructor(family, source, descriptors) {
        Object.assign(this, { family, source, descriptors });
    }
};
globalThis.getComputedStyle = () => ({ getPropertyValue: (v) => THEMES[root.attributes['data-theme']]?.[v] || '' });
globalThis.window = {
    location: { pathname: '/den/d', hash: '' },
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    addEventListener() {},
    removeEventListener() {},
    history: { state: null, pushState() {}, replaceState() {}, back() {}, go() {} },
};

const DARK = { backs: Object.values(THEMES.dark).slice(0, 3), text: THEMES.dark['--color-base-content'] };
const LIGHT = { backs: Object.values(THEMES.light).slice(0, 3), text: THEMES.light['--color-base-content'] };
const hue = (hex) => oklch(hex)[2];
const settle = () => new Promise((r) => setTimeout(r, 0));
// until waits for what a render's effects bring, which run a moment after it.
async function until(ok) {
    for (let i = 0; i < 200 && !ok(); i++) await settle();
}

function all(node, match, found = []) {
    if (node.nodeType === 1 && match(node)) found.push(node);
    for (const child of node.childNodes || []) all(child, match, found);
    return found;
}
const names = (node) => all(node, (n) => 'data-name' in n.attributes);

test('a style is read as the den checks it, by the cases they share', () => {
    assert.deepEqual(FONTS.map(([name]) => name), cases.fonts, "the picker's fonts are the den's, in its order");
    for (const c of cases.cases) {
        const want = 'shown' in c ? c.shown : c.style;
        assert.deepEqual(cleanStyle(c.style), want, JSON.stringify(c.style));
        assert.ok(c.sent || cleanStyle(c.style) === null, `${JSON.stringify(c.style)} isn't a den's to send, and shows as nothing`);
    }
    for (const bad of [null, undefined, 'pacifico', 7, [], { font: 7 }, { color: ['#aa0000'] }, { font: null }, { color: '#aa0000', color2: 9 }]) {
        assert.equal(cleanStyle(bad), null, JSON.stringify(bad));
    }
    assert.deepEqual(cleanStyle({ font: 'cinzel', color: '#aa0000', background: 'url(x)', '--color-base-100': 'red' }), { font: 'cinzel', color: '#aa0000' });
});

test('a color that reads on the theme is left alone, and one that does not moves toward the text until it does, its hue kept', () => {
    for (const [color, theme] of [['#ff6b6b', DARK], ['#4f8ff7', DARK], ['#8b0000', LIGHT], ['#1d4ed8', LIGHT]]) {
        assert.equal(readable(color, theme.backs, theme.text), color, `${color} reads as it is`);
    }
    // A dark red over a dark theme shows as a lighter red.
    const red = '#5c0a0a';
    assert.ok(contrast(red, DARK.backs[0]) < READS);
    const lifted = readable(red, DARK.backs, DARK.text);
    assert.match(lifted, /^#[0-9a-f]{6}$/);
    assert.ok(DARK.backs.every((b) => contrast(lifted, b) >= READS), `${lifted} reads on each of the theme's backgrounds`);
    assert.ok(oklch(lifted)[0] > oklch(red)[0], 'lighter');
    assert.ok(Math.abs(hue(lifted) - hue(red)) < 0.06, `still red: ${lifted}`);
    assert.ok(contrast(lifted, DARK.backs[2]) < READS + 0.6, `and no lighter than it must be: ${lifted}`);
    // A pale yellow over a light theme shows as a darker one.
    const yellow = '#fff3a0';
    const lowered = readable(yellow, LIGHT.backs, LIGHT.text);
    assert.ok(LIGHT.backs.every((b) => contrast(lowered, b) >= READS) && oklch(lowered)[0] < oklch(yellow)[0], lowered);
    assert.ok(Math.abs(hue(lowered) - hue(yellow)) < 0.06, `still yellow: ${lowered}`);
    // The theme's own background color, with no hue to keep, still reads.
    for (const theme of [DARK, LIGHT]) {
        const moved = readable(theme.backs[0], theme.backs, theme.text);
        assert.ok(theme.backs.every((b) => contrast(moved, b) >= READS), moved);
    }
});

test("a name's look is its font and its colors as the theme shows them, each end of a gradient moved alone", () => {
    const style = { font: 'pacifico', color: '#5c0a0a', color2: '#ff6b6b' };
    const look = nameLook(style, STYLES_ALL, DARK);
    assert.equal(look.font, 'pacifico');
    assert.deepEqual(look.colors, [readable('#5c0a0a', DARK.backs, DARK.text), '#ff6b6b'], 'the end that reads stays');
    assert.deepEqual(nameLook(style, STYLES_ALL, null).colors, ['#5c0a0a', '#ff6b6b'], 'as picked where the page reads no theme');
    assert.deepEqual(nameLook(style, STYLES_ALL, LIGHT).colors, ['#5c0a0a', readable('#ff6b6b', LIGHT.backs, LIGHT.text)]);
    assert.deepEqual(nameLook({ color: '#ff6b6b' }, STYLES_ALL, DARK), { font: '', colors: ['#ff6b6b'] });
    assert.equal(nameLook(undefined, STYLES_ALL, DARK), null);
    assert.equal(nameLook({ color: 'red' }, STYLES_ALL, DARK), null);
});

test('the reader chooses colors and fonts, colors only, or off, which the browser keeps', () => {
    const style = { font: 'pacifico', color: '#ff6b6b' };
    assert.equal(nameStyles(), STYLES_ALL, 'colors and fonts until they choose');
    assert.deepEqual(nameLook(style, nameStyles(), DARK), { font: 'pacifico', colors: ['#ff6b6b'] });
    setNameStyles(STYLES_COLORS);
    assert.equal(nameStyles(), STYLES_COLORS);
    assert.deepEqual(nameLook(style, nameStyles(), DARK), { font: '', colors: ['#ff6b6b'] });
    assert.equal(nameLook({ font: 'pacifico' }, nameStyles(), DARK), null, 'a font alone shows plain');
    setNameStyles(STYLES_OFF);
    assert.equal(nameStyles(), STYLES_OFF);
    assert.equal(nameLook(style, nameStyles(), DARK), null);
    setNameStyles('anything else');
    assert.equal(nameStyles(), STYLES_ALL);
});

test('the browser is given the fonts the shell lists at the assets, and no other address', () => {
    assert.match(fontFamily('pacifico'), /^"Dens name pacifico", var\(--default-font-family, /);
    assert.ok(fontFamily('orbitron'));
    for (const missing of ['cinzel', 'bangers', 'comic-sans', 'fredoka', 'toString']) assert.equal(fontFamily(missing), '', missing);
    assert.deepEqual(added.map((f) => [f.family, f.source]), [
        ['Dens name pacifico', 'url(/assets/fonts/pacifico.0a1b2c3d.woff2) format("woff2")'],
        ['Dens name orbitron', 'url(/assets/fonts/orbitron.4e5f6a7b.woff2) format("woff2")'],
    ]);
    assert.ok(added.every((f) => f.descriptors.weight === '100 900' && f.descriptors.unicodeRange.startsWith('U+0021-00FF')), 'one weight for all, Latin alone, without the space');
});

test('a name wears its style: a font, a color the theme shows, or a gradient cut to its text', async () => {
    setTheme('dark');
    const page = document.createElement('div');
    const bob = { id: '7', username: 'bob', display_name: 'Bob', name_style: { font: 'pacifico', color: '#5c0a0a' } };
    mount(h(Name, { member: bob, class: 'truncate' }), page);
    await until(() => names(page)[0].style.color !== '#5c0a0a');
    const [name] = names(page);
    assert.equal(name.textContent, 'Bob');
    assert.equal(name.attributes.class, 'truncate');
    assert.deepEqual({ ...name.style }, { fontFamily: fontFamily('pacifico'), paddingBlock: '0.3em', marginBlock: '-0.3em', color: readable('#5c0a0a', DARK.backs, DARK.text) });

    // Another theme shows the color as picked, where it reads there.
    setTheme('light');
    await until(() => names(page)[0].style.color === '#5c0a0a');
    assert.equal(names(page)[0].style.color, '#5c0a0a');

    // A gradient is the element's background, and its emoji keep their colors.
    mount(h(Name, { member: { ...bob, display_name: 'Bob 🦊🇨🇦 fox', name_style: { color: '#8b0000', color2: '#1d4ed8' } } }), page);
    await settle();
    const [gradient] = names(page);
    assert.equal(gradient.attributes.class, 'bg-clip-text');
    assert.deepEqual({ ...gradient.style }, { fontFamily: '', paddingBlock: '', marginBlock: '', backgroundImage: 'linear-gradient(90deg, #8b0000, #1d4ed8)', color: 'transparent' });
    assert.equal(gradient.textContent, 'Bob 🦊🇨🇦 fox');
    assert.deepEqual(all(gradient, (n) => n.localName === 'span' && n !== gradient).map((s) => [s.textContent, s.attributes.class]), [['🦊🇨🇦', 'text-base-content']]);
    assert.equal(nameText('Bob', { gradient: true }), 'Bob');

    // Off shows it plain, a member the page doesn't know has no style, and
    // a style that isn't one shows as none.
    setNameStyles(STYLES_OFF);
    await until(() => !names(page)[0].style.backgroundImage);
    assert.deepEqual({ ...names(page)[0].style }, { fontFamily: '', paddingBlock: '', marginBlock: '', backgroundImage: '', color: '' });
    assert.equal(names(page)[0].attributes.class, undefined);
    setNameStyles(STYLES_ALL);
    mount(h(Name, { member: null }), page);
    assert.equal(names(page)[0].textContent, 'Unknown member');
    mount(h(Name, { member: { ...bob, name_style: { color: 'url(https://evil.example/x.png)' } } }), page);
    await settle();
    assert.ok(!names(page)[0].style.color && !names(page)[0].style.backgroundImage);
    mount(null, page);
});

test('the member list, a call and a message show a name as its member styled it', async () => {
    setTheme('dark');
    const page = document.createElement('div');
    const bob = { id: '7', username: 'bob', display_name: 'Bob', name_style: { font: 'orbitron', color: '#ff6b6b' } };
    mount(h(LongName, { name: bob.display_name, member: bob }), page);
    await settle();
    assert.deepEqual({ ...names(page)[0].style }, { fontFamily: fontFamily('orbitron'), paddingBlock: '0.3em', marginBlock: '-0.3em', color: '#ff6b6b' });
    assert.match(names(page)[0].attributes.class, /\bname-scroll truncate\b/);

    const me = { id: '3', username: 'alice', display_name: 'Alice' };
    const none = () => {};
    const m = { id: '10', channel_id: '2', author_id: '7', created_at: Date.UTC(2026, 10, 9, 12), revision: 1, text: 'hello', reply_to: '9' };
    const replied = { id: '9', channel_id: '2', author_id: '3', created_at: m.created_at - 1000, revision: 1, text: 'hi' };
    for (const line of [false, true]) {
        mount(h(MessageRow, {
            m, line, author: bob, replied, repliedAuthor: { ...me, name_style: { color: '#4f8ff7' } }, me, editorNames: [], candidates: [], nameOf: () => '', denID: 'd', channelID: '2',
            onReact: none, onReactPick: none, onRemoveReactions: none, onUpdated: none, onPolled: none, onMenu: none, onReply: none, onEdit: none,
            onEditDone: none, onDelete: none, onJump: none, onView: none, onProfile: none, onToggle: none,
        }), page);
        await settle();
        const shown = names(page).map((n) => [n.textContent, n.style.color, n.style.fontFamily || '']);
        assert.deepEqual(shown, [['Alice', '#4f8ff7', ''], ['Bob', '#ff6b6b', fontFamily('orbitron')]], line ? 'in Compact' : 'by default');
    }
    mount(null, page);
});

test('Edit profile takes a font and colors, shows the name as it will be, and saves them with the profile', async () => {
    const asked = [];
    globalThis.fetch = async (path, opts = {}) => {
        asked.push({ method: opts.method || 'GET', path, body: opts.body ? JSON.parse(opts.body) : undefined });
        const body = path.endsWith('/members/1') ? { bio: 'Hi', badges: [] } : path.endsWith('/storage') ? { used: 0 } : { id: '1' };
        return { ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify(body) };
    };
    const alice = { id: '1', username: 'alice', display_name: 'Alice', role: 'member', name_style: { color: '#ff6b6b' } };
    // The preview shows the name as this theme does, a blue lightened to read on Dark.
    setTheme('dark');
    const shown = `linear-gradient(90deg, #ff6b6b, ${readable('#1d4ed8', DARK.backs, DARK.text)})`;
    assert.notEqual(readable('#1d4ed8', DARK.backs, DARK.text), '#1d4ed8');
    const page = document.createElement('div');
    const fields = () => all(page, (n) => 'data-name-style' in n.attributes)[0];
    const preview = () => names(all(page, (n) => 'data-name-preview' in n.attributes)[0])[0];
    const radio = (name, value) => all(fields(), (n) => n.localName === 'input' && n.attributes.name === name && n.attributes.value === value)[0];
    const hexes = () => all(fields(), (n) => n.localName === 'input' && /as hex$/.test(n.attributes['aria-label'] || '')).map((n) => [n.attributes['aria-label'], n.attributes.value]);
    const checked = (name, value) => 'checked' in radio(name, value).attributes;

    // A den older than name styles offers none.
    mount(h(EditProfile, { denID: 'd', me: alice, limits: null, onClose() {} }), page);
    await settle();
    assert.equal(fields(), undefined);
    mount(null, page);

    let closed = false;
    mount(h(EditProfile, { denID: 'd', me: alice, limits: null, nameStyles: true, onClose() { closed = true; } }), page);
    for (let i = 0; i < 50 && !all(page, (n) => n.localName === 'textarea' && !n.disabled).length; i++) await settle();
    assert.deepEqual(all(fields(), (n) => n.localName === 'input' && n.attributes.name === 'name-font').map((n) => n.attributes.value), ['', ...cases.fonts]);
    assert.ok(checked('name-color', 'solid') && checked('name-font', ''), 'it opens at her style');
    assert.deepEqual(hexes(), [['Color, as hex', '#ff6b6b']]);
    assert.equal(preview().style.color, '#ff6b6b');

    radio('name-font', 'pacifico').dispatch('change');
    await settle();
    radio('name-color', 'gradient').dispatch('change');
    await settle();
    assert.ok(checked('name-color', 'gradient') && checked('name-font', 'pacifico'));
    assert.deepEqual(hexes().map(([label]) => label), ['From, as hex', 'To, as hex']);
    const to = all(fields(), (n) => n.attributes['aria-label'] === 'To, as hex')[0];
    to.dispatch('input', { currentTarget: Object.assign(to, { value: '1D4ED8' }) });
    await until(() => preview().style.backgroundImage === shown);
    assert.equal(preview().style.backgroundImage, shown);
    assert.equal(preview().style.fontFamily, fontFamily('pacifico'));

    // Her own Name styles, off, leaves the preview as others see it.
    setNameStyles(STYLES_OFF);
    for (let i = 0; i < 60; i++) await settle();
    assert.equal(preview().style.backgroundImage, shown);
    setNameStyles(STYLES_ALL);

    asked.length = 0;
    all(page, (n) => n.localName === 'form')[0].dispatch('Submit', { preventDefault() {} });
    for (let i = 0; i < 50 && !closed; i++) await settle();
    assert.deepEqual(asked, [{ method: 'PATCH', path: '/api/dens/d/me', body: { display_name: 'Alice', bio: 'Hi', name_style: { color: '#ff6b6b', font: 'pacifico', color2: '#1d4ed8' } } }]);

    // Default clears the colors, and the font goes with its tile.
    closed = false;
    mount(null, page);
    mount(h(EditProfile, { denID: 'd', me: { ...alice, name_style: { font: 'cinzel', color: '#ff6b6b', color2: '#1d4ed8' } }, limits: null, nameStyles: true, onClose() { closed = true; } }), page);
    for (let i = 0; i < 50 && !all(page, (n) => n.localName === 'textarea' && !n.disabled).length; i++) await settle();
    radio('name-color', 'none').dispatch('change');
    await settle();
    radio('name-font', '').dispatch('change');
    await settle();
    assert.deepEqual(hexes(), []);
    asked.length = 0;
    all(page, (n) => n.localName === 'form')[0].dispatch('Submit', { preventDefault() {} });
    for (let i = 0; i < 50 && !closed; i++) await settle();
    assert.deepEqual(asked.at(-1).body, { display_name: 'Alice', bio: 'Hi', name_style: {} });
    mount(null, page);
});
