// Appearance as a page, checked without a browser on a wide
// window: a theme chosen in the list shows in the editor and the preview
// and isn't worn until Use, which the list then marks; Customize copies a
// built-in theme into the member's own; a theme's edits wait for Save, and
// leaving with them unsaved asks first; and on a phone, each column is a
// tab along the foot.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { AppearancePage } from '../src/appearance/appearance.jsx';
import { builtInTheme, forgetTheme, keepTheme, ownChoice, ownThemes, setTheme, themeChoice } from '../src/appearance/theme.js';
import { FIELDS, cleanTheme, variables } from '../src/appearance/themes.js';

// What DaisyUI's stylesheet gives a box wearing each theme, as its
// computed style reads: Light, Dark and Dracula.
const NUMBERS = { '--radius-selector': '.5rem', '--radius-field': '.25rem', '--radius-box': '.5rem', '--size-selector': '.25rem', '--size-field': '.25rem', '--border': '1px', '--depth': '1', '--noise': '0' };
const colors = (list) => Object.fromEntries(Object.values(FIELDS).filter((v) => v.startsWith('--color-')).map((v, i) => [v, list[i % list.length]]));
const STYLES = {
    light: { scheme: 'light', vars: { ...colors(['oklch(100% 0 0)', 'oklch(98% 0 0)', 'oklch(95% 0 0)', 'oklch(21% .006 285.885)', 'oklch(45% .24 277.023)']), ...NUMBERS } },
    dark: { scheme: 'dark', vars: { ...colors(['oklch(25.33% .016 252.42)', 'oklch(23.26% .014 253.1)', 'oklch(21.15% .012 254.09)', 'oklch(97.807% .029 256.847)', 'oklch(58% .233 277.117)']), ...NUMBERS } },
    dracula: {
        scheme: 'dark',
        vars: {
            ...colors(['oklch(28.822% .022 277.508)', 'oklch(26.805% .02 277.508)', 'oklch(24.787% .019 277.508)', 'oklch(97.747% .007 106.545)', 'oklch(75.461% .183 346.812)']),
            ...NUMBERS, '--radius-selector': '1rem', '--radius-field': '.5rem', '--radius-box': '1rem', '--depth': '0',
        },
    },
};

// The window is wide and has a mouse, as the page's three columns want,
// unless phone says it's a phone's.
let phone = false;
globalThis.window = {
    matchMedia: (q) => ({ matches: !phone && q.includes('min-width'), addEventListener() {}, removeEventListener() {} }), addEventListener() {}, removeEventListener() {},
    history: { state: null, pushState() {}, replaceState() {}, go() {} }, location: { protocol: 'http:', host: 'localhost', pathname: '/', href: 'http://localhost/' },
};
document.documentElement = document.createElement('html');
document.body = document.createElement('body');
document.addEventListener ||= () => {};
document.removeEventListener ||= () => {};
globalThis.getComputedStyle = (el) => {
    const s = STYLES[el.getAttribute?.('data-theme')] || STYLES.light;
    return { colorScheme: s.scheme, getPropertyValue: (v) => el.style?.[v] ?? s.vars[v] ?? '' };
};

function all(node, match, found = []) {
    if (node.nodeType === 1 && match(node)) found.push(node);
    for (const child of node.childNodes || []) all(child, match, found);
    return found;
}
const one = (node, match) => all(node, match)[0];
const has = (name) => (n) => name in n.attributes;
const named = (tag, text) => (n) => n.localName === tag && n.textContent.trim() === text;
// Without requestAnimationFrame, Preact runs effects up to 100 ms on.
const settle = () => new Promise((r) => setTimeout(r, 120));

// open mounts the page, with the guard the settings hold, and reads what
// it shows: the rows of the list, the one checked and the one worn, the
// editor's name and its buttons, and the theme the preview wears.
function open() {
    const root = document.createElement('div');
    const guard = { current: null };
    mount(h(AppearancePage, { guard }), root);
    const read = () => {
        const rows = all(root, has('data-theme-row'));
        const editor = one(root, has('data-theme-editor'));
        const nameField = editor && one(editor, (n) => n.localName === 'input' && 'data-theme-name' in n.attributes);
        return {
            rows: rows.map((r) => r.attributes['data-theme-row']),
            picked: rows.filter((r) => 'checked' in one(r, (n) => n.localName === 'input').attributes || one(r, (n) => n.localName === 'input').checked)
                .map((r) => r.attributes['data-theme-row']),
            worn: rows.filter((r) => 'data-worn' in r.attributes).map((r) => r.attributes['data-theme-row']),
            name: nameField ? nameField.value ?? nameField.attributes.value : one(root, has('data-theme-name'))?.textContent.trim(),
            builtIn: !!editor && 'data-built-in' in editor.attributes,
            use: !!one(root, named('button', 'Use')),
            inUse: !!one(root, has('data-worn-mark')),
            preview: one(root, has('data-theme-preview'))?.attributes['data-theme'],
            previewStyle: { ...one(root, has('data-theme-preview'))?.style },
            bar: one(root, has('data-theme-bar'))?.textContent.trim() || '',
        };
    };
    const press = async (text) => {
        const b = one(root, named('button', text));
        assert.ok(b, `a ${text} button`);
        b.dispatch('click');
        await settle();
    };
    const choose = async (value) => {
        const row = one(root, (n) => n.attributes['data-theme-row'] === value);
        one(row, (n) => n.localName === 'input').dispatch('change');
        await settle();
    };
    return { root, guard, read, press, choose, close: () => mount(null, root) };
}

const wears = () => ({ theme: document.documentElement.attributes['data-theme'], vars: { ...document.documentElement.style } });

test('a theme chosen in the list shows in the editor and the preview alone, and Use wears it, the list marking it', async () => {
    setTheme('system');
    const page = open();
    try {
        let seen = page.read();
        assert.deepEqual(seen.rows.slice(0, 4), ['system', 'light', 'dark', 'abyss'], 'System, Light and Dark first, then the rest');
        assert.equal(seen.rows.length, 36);
        assert.deepEqual([seen.picked, seen.worn, seen.inUse], [['system'], ['system'], true], 'it opens at the theme worn');
        await page.choose('dracula');
        seen = page.read();
        assert.deepEqual([seen.picked, seen.worn], [['dracula'], ['system']], 'Dracula chosen, System still worn');
        assert.deepEqual([seen.name, seen.builtIn, seen.use, seen.inUse], ['Dracula', true, true, false], 'the editor shows Dracula, with Use');
        assert.equal(seen.preview, 'dracula', 'the preview wears Dracula by its name');
        assert.deepEqual(seen.previewStyle, {}, 'and sets no variables for it');
        assert.equal(wears().theme, 'light', 'the page keeps its own');
        assert.equal(themeChoice(), 'system');
        await page.press('Use');
        seen = page.read();
        assert.deepEqual([wears().theme, themeChoice()], ['dracula', 'dracula'], 'Use wears it');
        assert.deepEqual([seen.worn, seen.use, seen.inUse], [['dracula'], false, true], 'and the list marks it');
    } finally {
        page.close();
        setTheme('system');
    }
});

test("Customize copies a built-in theme into the member's own, shown and not worn", async () => {
    setTheme('system');
    const page = open();
    try {
        await page.choose('dracula');
        await page.press('Customize');
        const own = ownThemes();
        assert.equal(own.length, 1);
        const { id, ...copy } = own[0];
        assert.deepEqual(copy, builtInTheme('dracula'), "the copy holds Dracula's every variable, by its name");
        assert.equal(copy.name, 'Dracula');
        assert.equal(copy.radiusBox, 1);
        const seen = page.read();
        assert.deepEqual([seen.picked, seen.worn, seen.name, seen.builtIn], [[ownChoice(id)], ['system'], 'Dracula', false], 'shown in the editor, as the member\'s own');
        assert.equal(seen.rows[0], ownChoice(id), 'first in the list, under My themes');
        assert.equal(seen.preview, 'dark', "the preview wears its variables over Dark");
        assert.deepEqual(seen.previewStyle, variables(cleanTheme(copy)));
        assert.equal(wears().theme, 'light', 'the page keeps its own');
        forgetTheme(id);
    } finally {
        page.close();
        setTheme('system');
    }
});

test("a theme's edits wait for Save, and leaving with them unsaved asks first", async () => {
    const id = keepTheme({ ...builtInTheme('dracula'), name: 'Mine' });
    setTheme(ownChoice(id));
    const page = open();
    try {
        await settle();
        assert.equal(page.read().name, 'Mine');
        assert.equal(page.guard.current(() => {}), false, 'with nothing unsaved, leaving asks nothing');
        const tile = one(page.root, (n) => n.attributes['data-color-tile'] === 'primary');
        tile.dispatch('click');
        await settle();
        const field = one(page.root, (n) => n.localName === 'input' && n.attributes['aria-label'] === 'Primary, as text');
        field.value = '#c2410c';
        field.dispatch('input');
        await settle();
        let seen = page.read();
        assert.equal(seen.previewStyle['--color-primary'], '#c2410c', 'the preview shows the edit');
        assert.notEqual(wears().vars['--color-primary'], '#c2410c', 'the page, wearing the theme, waits for Save');
        assert.match(seen.bar, /unsaved changes/);
        assert.equal(seen.use, false);
        let left = false;
        assert.equal(page.guard.current(() => {
            left = true;
        }), true, 'leaving asks');
        await settle();
        assert.match(page.read().bar, /^Discard your unsaved changes\?/);
        await page.press('Keep editing');
        assert.equal(left, false);
        await page.press('Save');
        seen = page.read();
        assert.equal(wears().vars['--color-primary'], '#c2410c', 'saved, the page wears the edit');
        assert.equal(ownThemes()[0].primary, '#c2410c');
        assert.equal(seen.bar, '');
        field.value = '#7c3aed';
        field.dispatch('input');
        await settle();
        page.guard.current(() => {
            left = true;
        });
        await settle();
        await page.press('Discard');
        assert.equal(left, true, 'Discard leaves');
        assert.equal(ownThemes()[0].primary, '#c2410c', 'keeping nothing');
    } finally {
        page.close();
        forgetTheme(id);
        setTheme('system');
    }
});

test('on a phone each column is a tab along the foot, the themes first', async () => {
    phone = true;
    setTheme('system');
    const page = open();
    try {
        const tabs = () => all(page.root, has('data-foot-tab')).map((t) => t.attributes['data-foot-tab'] + (t.attributes['aria-selected'] === 'true' ? '*' : ''));
        assert.deepEqual(tabs(), ['themes*', 'editor', 'preview']);
        assert.ok(page.read().rows.length > 0 && !page.read().preview && page.read().name === undefined, 'the list alone');
        one(page.root, (n) => n.attributes['data-foot-tab'] === 'editor').dispatch('click');
        await settle();
        assert.deepEqual(tabs(), ['themes', 'editor*', 'preview']);
        assert.deepEqual(all(page.root, has('data-pane-tab')).map((t) => t.textContent.trim()), ['Theme', 'Messages', 'Calls']);
        assert.equal(page.read().name, 'System');
        one(page.root, (n) => n.attributes['data-foot-tab'] === 'preview').dispatch('click');
        await settle();
        assert.ok(page.read().preview && !page.read().rows.length, 'the preview alone');
    } finally {
        page.close();
        phone = false;
    }
});
