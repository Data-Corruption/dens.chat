// The page's theme, checked without a browser, on a stand-in for the
// page's root: a theme of the member's own kept and chosen, its variables
// set; the editor's theme shown and taken back; one of DaisyUI's taking
// the variables off; a share's window dressed the same; and the most the
// browser keeps.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { dressTheme, forgetTheme, keepTheme, ownChoice, ownThemes, previewTheme, setTheme, themeChoice } from '../assets/js/src/theme.js';
import { MOST_THEMES, VARIABLES, variables } from '../assets/js/src/themes.js';

function root() {
    const props = new Map();
    const attributes = {};
    return {
        props,
        attributes,
        setAttribute: (name, value) => { attributes[name] = value; },
        getAttribute: (name) => attributes[name] ?? null,
        style: {
            setProperty: (name, value) => props.set(name, value),
            removeProperty: (name) => props.delete(name),
            getPropertyValue: (name) => props.get(name) ?? '',
        },
    };
}

const page = root();
globalThis.document = { documentElement: page };
globalThis.window = { matchMedia: () => ({ matches: false }) };

const DUSK = {
    name: 'Dusk', dark: true, corners: 'rounded', base: '#1d232a', text: '#a6adbb', primary: '#605dff', secondary: '#f43098', accent: '#00d3bb',
    neutral: '#09090b', info: '#00bafe', success: '#00d390', warning: '#fcb700', error: '#ff627d',
};
const DAWN = { ...DUSK, name: 'Dawn', dark: false, corners: 'round', base: '#fff8ee', text: '#2a2118' };
const shows = (theme) => assert.deepEqual(Object.fromEntries(page.props), variables(theme));

test("a theme of the member's own is kept, chosen, and set on the root as variables over Light or Dark", () => {
    assert.equal(themeChoice(), 'system');
    const id = keepTheme(DUSK);
    assert.match(id, /^[a-z0-9]{8}$/);
    assert.deepEqual(ownThemes(), [{ id, ...DUSK }]);
    assert.equal(page.props.size, 0, 'kept, not yet chosen');
    setTheme(ownChoice(id));
    assert.equal(themeChoice(), ownChoice(id));
    assert.equal(page.attributes['data-theme'], 'dark');
    shows(DUSK);
});

test('the editor shows its theme as the member makes it, and Cancel puts the one before back', () => {
    previewTheme({ ...DAWN, name: '' });
    assert.equal(page.attributes['data-theme'], 'light');
    shows(DAWN);
    previewTheme(null);
    assert.equal(page.attributes['data-theme'], 'dark');
    shows(DUSK);
    // Over one of DaisyUI's, Cancel leaves no variable behind.
    setTheme('dracula');
    previewTheme(DAWN);
    shows(DAWN);
    previewTheme(null);
    assert.equal(page.attributes['data-theme'], 'dracula');
    assert.equal(page.props.size, 0);
});

test("an edit replaces the theme in place, and a share's window is dressed as the page is", () => {
    const [{ id }] = ownThemes();
    assert.equal(keepTheme({ ...DUSK, primary: '#ff8800' }, id), id);
    assert.deepEqual(ownThemes(), [{ id, ...DUSK, primary: '#ff8800' }]);
    setTheme(ownChoice(id));
    assert.equal(page.props.get('--color-primary'), '#ff8800');
    const other = root();
    dressTheme({ documentElement: other });
    assert.equal(other.attributes['data-theme'], 'dark');
    assert.deepEqual(Object.fromEntries(other.props), Object.fromEntries(page.props));
    assert.equal(other.props.size, VARIABLES.length);
});

test("deleting the chosen theme follows the system again, and a theme that isn't one is never kept", () => {
    const [{ id }] = ownThemes();
    forgetTheme(id);
    assert.deepEqual(ownThemes(), []);
    assert.equal(themeChoice(), 'system');
    assert.equal(page.attributes['data-theme'], 'light');
    assert.equal(page.props.size, 0);
    assert.equal(keepTheme({ ...DUSK, base: 'url(https://evil.example)' }), '');
    assert.equal(keepTheme({ ...DUSK, name: '' }), '');
    assert.deepEqual(ownThemes(), []);
});

test(`the browser keeps ${MOST_THEMES} themes and no more`, () => {
    const ids = Array.from({ length: MOST_THEMES }, (_, i) => keepTheme({ ...DUSK, name: `Theme ${i + 1}` }));
    assert.ok(ids.every(Boolean));
    assert.equal(new Set(ids).size, MOST_THEMES);
    assert.equal(keepTheme(DAWN), '');
    assert.equal(ownThemes().length, MOST_THEMES);
    assert.equal(keepTheme(DAWN, ids[3]), ids[3], 'one of them still changes');
    assert.equal(ownThemes()[3].name, 'Dawn');
});
