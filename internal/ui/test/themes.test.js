// A theme of the member's own, checked without a browser: what derives
// from its colors, the variables it sets, its code there and back, the
// codes refused, and what the browser kept read again.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    CODE, MOST_THEMES, VARIABLES, cleanName, cleanTheme, contrast, cornersOf, deeper, fromOklch, hexColor, oklch, readCode, readThemes, textOn,
    themeCode, variables,
} from '../assets/js/src/themes.js';

const DUSK = {
    name: 'Dusk', dark: true, corners: 'rounded', base: '#1d232a', text: '#a6adbb', primary: '#605dff', secondary: '#f43098', accent: '#00d3bb',
    neutral: '#09090b', info: '#00bafe', success: '#00d390', warning: '#fcb700', error: '#ff627d',
};
const HEX = /^#[0-9a-f]{6}$/;
const lightness = (hex) => oklch(hex)[0];

test('a color goes to lightness, chroma and hue and back', () => {
    for (const color of ['#000000', '#ffffff', '#1d232a', '#605dff', '#f43098', '#fcb700', '#00d390']) {
        assert.equal(fromOklch(...oklch(color)), color);
    }
    assert.ok(Math.abs(lightness('#ffffff') - 1) < 0.001);
    assert.ok(lightness('#000000') < 0.001);
    // A color sRGB can't show keeps its hue and loses chroma.
    const vivid = fromOklch(0.7, 0.4, oklch('#ff0000')[2]);
    assert.match(vivid, HEX);
    assert.ok(Math.abs(oklch(vivid)[2] - oklch('#ff0000')[2]) < 0.05);
});

test('the contrast ratio reads known pairs', () => {
    assert.equal(Math.round(contrast('#000000', '#ffffff') * 100) / 100, 21);
    assert.equal(contrast('#ffffff', '#000000'), contrast('#000000', '#ffffff'));
    assert.equal(contrast('#605dff', '#605dff'), 1);
    assert.equal(Math.round(contrast('#777777', '#ffffff') * 100) / 100, 4.48);
    assert.equal(Math.round(contrast('#ff0000', '#ffffff') * 100) / 100, 4);
});

test('the deeper backgrounds step darker, or lighter under one too dark to', () => {
    for (const base of ['#ffffff', '#1d232a', '#f2e8d5', '#2b3440']) {
        const [two, three] = deeper(base);
        assert.ok(lightness(two) < lightness(base) && lightness(three) < lightness(two), `${base}: ${two}, ${three}`);
        assert.ok(lightness(base) - lightness(three) < 0.06, `${base} steps a little`);
    }
    for (const base of ['#000000', '#0a0a0a', '#120c1c']) {
        const [two, three] = deeper(base);
        assert.ok(lightness(two) > lightness(base) && lightness(three) > lightness(two), `${base}: ${two}, ${three}`);
        assert.notEqual(two, base);
        assert.notEqual(three, two);
    }
});

test("each color's own text is light or dark, whichever reads better on it", () => {
    for (const color of ['#605dff', '#09090b', '#7a1f1f', '#0000ff']) {
        assert.ok(lightness(textOn(color)) > 0.9, `${color} carries light text`);
    }
    for (const color of ['#fcb700', '#00d3bb', '#ffffff', '#00d390']) {
        assert.ok(lightness(textOn(color)) < 0.3, `${color} carries dark text`);
    }
    for (const color of Object.values(DUSK).filter((v) => HEX.test(v))) {
        assert.ok(contrast(textOn(color), color) >= 4, `${color}'s text reads on it`);
    }
});

test("a theme sets DaisyUI's variables, each a color it wrote out or one of its corners' radii", () => {
    const vars = variables(DUSK);
    assert.deepEqual(Object.keys(vars).sort(), [...VARIABLES].sort());
    assert.equal(vars['--color-base-100'], DUSK.base);
    assert.equal(vars['--color-base-content'], DUSK.text);
    assert.deepEqual([vars['--color-base-200'], vars['--color-base-300']], deeper(DUSK.base));
    for (const name of ['primary', 'secondary', 'accent', 'neutral', 'info', 'success', 'warning', 'error']) {
        assert.equal(vars[`--color-${name}`], DUSK[name]);
        assert.equal(vars[`--color-${name}-content`], textOn(DUSK[name]));
    }
    for (const [name, value] of Object.entries(vars)) {
        assert.match(value, name.startsWith('--color-') ? HEX : /^[0-9.]+rem$/, name);
    }
    assert.equal(variables({ ...DUSK, corners: 'square' })['--radius-box'], '0rem');
    assert.equal(variables({ ...DUSK, corners: 'round' })['--radius-field'], '2rem');
    assert.deepEqual([cornersOf(0), cornersOf(0.5), cornersOf(1), cornersOf(NaN)], ['square', 'rounded', 'round', 'square']);
});

test("a theme's code holds it whole, and reads back the same", () => {
    for (const theme of [DUSK, { ...DUSK, name: 'Été 🌆 at the lake', dark: false, corners: 'round' }, { ...DUSK, name: 'x'.repeat(40), corners: 'square' }]) {
        const code = themeCode(theme);
        assert.ok(code.startsWith(CODE));
        assert.match(code, /^[a-z0-9:.-]+$/, 'nothing in a code means anything to markdown');
        assert.deepEqual(readCode(code), theme);
        assert.deepEqual(readCode(`  ${code.toUpperCase()}\n`), theme, 'as pasted, with space around it, in capitals');
    }
});

test('a code with anything but a theme in it is refused', () => {
    const code = themeCode(DUSK);
    const [version, dark, corners, colors, name] = code.slice(CODE.length).split('.');
    const made = (...parts) => CODE + parts.join('.');
    assert.deepEqual(readCode(made(version, dark, corners, colors, name)), DUSK);
    const hexOf = (text) => [...new TextEncoder().encode(text)].map((b) => b.toString(16).padStart(2, '0')).join('');
    for (const [what, bad] of [
        ['nothing', ''],
        ['no prefix', code.slice(CODE.length)],
        ["another prefix's", `dens-invite:${code.slice(CODE.length)}`],
        ['a later version', made('2', dark, corners, colors, name)],
        ['a field more', made(version, dark, corners, colors, name, '00')],
        ['a field fewer', made(version, dark, corners, colors)],
        ['a third scheme', made(version, 'x', corners, colors, name)],
        ['corners it has no name for', made(version, dark, 'q', colors, name)],
        ["a value that isn't a color", made(version, dark, corners, `${colors.slice(0, 54)}zzzzzz`, name)],
        ['a color in words', made(version, dark, corners, 'red', name)],
        ['a color more', made(version, dark, corners, `${colors}ffffff`, name)],
        ['a url where a color goes', made(version, dark, corners, `${colors.slice(0, 30)}url(https://evil.example/x.png)`, name)],
        ['no name', made(version, dark, corners, colors, '')],
        ['a name of 41 characters', made(version, dark, corners, colors, hexOf('x'.repeat(41)))],
        ['a name of half a byte', made(version, dark, corners, colors, `${name}4`)],
        ["a name that isn't UTF-8", made(version, dark, corners, colors, 'ff')],
        ['a name of spaces', made(version, dark, corners, colors, hexOf('   '))],
        ['a name with a line break', made(version, dark, corners, colors, hexOf('two\nlines'))],
        ['a name that turns its text around', made(version, dark, corners, colors, hexOf(`abc${String.fromCodePoint(0x202e)}def`))],
        ['JSON', `${CODE}{"name":"x"}`],
    ]) {
        assert.equal(readCode(bad), null, what);
    }
});

test('a theme is only the fields a theme has, each checked', () => {
    assert.deepEqual(cleanTheme({ ...DUSK, id: 'abc', font: 'Comic Sans', '--color-base-100': 'url(x)' }), DUSK);
    for (const bad of [
        null, 'a theme', [], { ...DUSK, name: '' }, { ...DUSK, name: 7 }, { ...DUSK, dark: 'yes' }, { ...DUSK, corners: 'pointy' },
        { ...DUSK, corners: 'toString' }, { ...DUSK, base: 'red' }, { ...DUSK, primary: '#FFF' }, { ...DUSK, error: undefined },
        { ...DUSK, accent: '#12345g' }, { ...DUSK, info: 'var(--color-primary)' },
    ]) {
        assert.equal(cleanTheme(bad), null, JSON.stringify(bad));
    }
    assert.equal(cleanName('  Dusk  '), 'Dusk');
    assert.equal(cleanName('🌆'.repeat(40)), '🌆'.repeat(40));
    assert.equal(cleanName('x'.repeat(41)), '');
});

test('a typed color reads with or without its #, and in three digits', () => {
    assert.equal(hexColor('#1D232A'), '#1d232a');
    assert.equal(hexColor(' 1d232a '), '#1d232a');
    assert.equal(hexColor('#abc'), '#aabbcc');
    for (const bad of ['', '#12', '#12345', '#1234567', 'red', 'rgb(1,2,3)', '#12345g']) assert.equal(hexColor(bad), '', bad);
});

test('what the browser kept reads as themes, and what else is there is left out', () => {
    const kept = [{ id: 'aaaaaaaa', ...DUSK }, { id: 'bbbbbbbb', ...DUSK, name: 'Dawn', dark: false }];
    assert.deepEqual(readThemes(JSON.stringify(kept)), kept);
    assert.deepEqual(readThemes(null), []);
    assert.deepEqual(readThemes('not json'), []);
    assert.deepEqual(readThemes('{"id":"aaaaaaaa"}'), []);
    const mixed = [kept[0], { ...kept[1], id: 'aaaaaaaa' }, { ...kept[1], id: '../../etc' }, { id: 'cccccccc', ...DUSK, base: 'red' }, 'a string', kept[1]];
    assert.deepEqual(readThemes(JSON.stringify(mixed)), kept, 'an id twice, an id that is none, and a color that is none are left out');
    const many = Array.from({ length: MOST_THEMES + 5 }, (_, i) => ({ id: `theme${String(i).padStart(3, '0')}`, ...DUSK }));
    assert.equal(readThemes(JSON.stringify(many)).length, MOST_THEMES);
});
