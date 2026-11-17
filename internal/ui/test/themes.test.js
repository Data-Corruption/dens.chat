// A theme of the member's own, checked without a browser: what derives
// from its colors, the variables it sets, its block there and back,
// DaisyUI's generator's block read, what a block holds that the page
// can't read left out, v1.13.0's codes and kept themes read as they were,
// and what the browser kept read again.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    CODE, COLORS, DAISY, FIELDS, MOST_THEMES, SHOWN, VARIABLES, cleanName, cleanTheme, contrast, deeper, editTheme, fromOklch, hexColor, hexOf, lowContrast,
    oklch, readCode, readColor, readThemes, readValue, textOn, themeCode, variables,
} from '../assets/js/src/themes.js';
import released from './testdata/themes-v1.13.0.json';

const DUSK = {
    name: 'Dusk', dark: true, base: '#1d232a', text: '#a6adbb', primary: '#605dff', secondary: '#f43098', accent: '#00d3bb',
    neutral: '#09090b', info: '#00bafe', success: '#00d390', warning: '#fcb700', error: '#ff627d', radiusSelector: 0.5, radiusField: 0.25, radiusBox: 0.5,
};
const HEX = /^#[0-9a-f]{6}$/;
const lightness = (hex) => oklch(hex)[0];

// GENERATOR is a block as DaisyUI's theme generator copies it (its
// generateCSS): a line before it, each line of it, a quoted color-scheme,
// and every value as the generator writes it.
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
    for (const color of COLORS.map((c) => DUSK[c])) {
        assert.ok(contrast(textOn(color), color) >= 4, `${color}'s text reads on it`);
    }
});

test('a color reads as hex or oklch(), and is written out again by the page', () => {
    for (const [given, kept] of [
        ['#1D232A', '#1d232a'],
        [' #abc ', '#aabbcc'],
        ['oklch(97.788% 0.004 56.375)', 'oklch(97.788% 0.004 56.375)'],
        ['OKLCH( 97.788%   .004 56.375 )', 'oklch(97.788% 0.004 56.375)'],
        ['oklch(0.55 0.3 240)', 'oklch(55% 0.3 240)'],
        ['oklch(55% 75% 240deg)', 'oklch(55% 0.3 240)'],
        ['oklch(55.12345% 0.123456 -30)', 'oklch(55.123% 0.123 330)'],
        ['oklch(100% 0 720)', 'oklch(100% 0 0)'],
    ]) {
        assert.equal(readColor(given), kept, given);
        assert.equal(readColor(kept), kept, `${kept} reads as itself`);
    }
    for (const bad of [
        '', 'red', '#12', '#12345', '#1234567', '#12345678', '#12345g', 'rgb(1,2,3)', 'hsl(1 2% 3%)', 'oklch(50% 0.1 20 / 50%)', 'oklch(50% 0.1)',
        'oklch(101% 0.1 20)', 'oklch(-1% 0.1 20)', 'oklch(50% 0.6 20)', 'oklch(none 0.1 20)', 'oklch(50% 0.1 20) url(x)', 'var(--color-primary)',
        'oklch(50% 0.1 20);background:url(https://evil.example/x.png)', 'oklch(1e2% 0 0)', 7, null,
    ]) {
        assert.equal(readColor(bad), '', String(bad));
    }
    assert.equal(hexOf('#605dff'), '#605dff');
    assert.equal(hexOf(readColor(`oklch(${oklch('#605dff')[0] * 100}% ${oklch('#605dff')[1]} ${(oklch('#605dff')[2] * 180) / Math.PI})`)), '#605dff');
});

test('a number reads in its unit, within the generator\'s range', () => {
    assert.equal(readValue('radiusField', '0.75rem'), 0.75);
    assert.equal(readValue('radiusField', '0'), 0);
    assert.equal(readValue('radiusField', '.5rem'), 0.5);
    assert.equal(readValue('sizeField', '0.21875rem'), 0.21875);
    assert.equal(readValue('border', '1.5px'), 1.5);
    assert.equal(readValue('depth', '1'), 1);
    assert.equal(readValue('noise', '0'), 0);
    for (const [field, bad] of [
        ['radiusField', '3rem'], ['radiusField', '8px'], ['radiusField', '0.5'], ['radiusField', '-1rem'], ['radiusField', 'calc(1rem)'],
        ['sizeField', '1rem'], ['sizeField', '0.1rem'], ['border', '1rem'], ['border', '3px'], ['border', '1'], ['depth', '2'], ['noise', '1px'],
        ['radiusBox', '1rem;color:red'], ['radiusBox', ''],
    ]) {
        assert.equal(readValue(field, bad), undefined, `${field}: ${bad}`);
    }
});

test("a theme sets DaisyUI's variables, each a value it wrote out, deriving what it doesn't hold", () => {
    const vars = variables(DUSK);
    const derived = ['--color-base-200', '--color-base-300', ...Object.keys(vars).filter((v) => v.endsWith('-content') && v !== '--color-base-content')];
    assert.deepEqual(Object.keys(vars).sort(), [...COLORS.map((c) => FIELDS[c]), ...derived, '--radius-selector', '--radius-field', '--radius-box'].sort());
    assert.equal(vars['--color-base-100'], DUSK.base);
    assert.equal(vars['--color-base-content'], DUSK.text);
    assert.deepEqual([vars['--color-base-200'], vars['--color-base-300']], deeper(DUSK.base));
    for (const name of ['primary', 'secondary', 'accent', 'neutral', 'info', 'success', 'warning', 'error']) {
        assert.equal(vars[`--color-${name}`], DUSK[name]);
        assert.equal(vars[`--color-${name}-content`], textOn(DUSK[name]));
    }
    assert.deepEqual([vars['--radius-selector'], vars['--radius-field'], vars['--radius-box']], ['0.5rem', '0.25rem', '0.5rem']);
    // What a theme holds, it sets as it holds it.
    const held = { ...DUSK, base200: 'oklch(20% 0.02 250)', primaryContent: '#ffffff', sizeField: 0.28125, border: 2, depth: 0, noise: 1 };
    const all = variables(held);
    assert.equal(all['--color-base-200'], held.base200);
    assert.equal(all['--color-base-300'], deeper(DUSK.base)[1]);
    assert.equal(all['--color-primary-content'], '#ffffff');
    assert.deepEqual([all['--size-field'], all['--border'], all['--depth'], all['--noise'], all['--size-selector']], ['0.28125rem', '2px', '0', '1', undefined]);
    assert.ok(Object.keys(all).every((v) => VARIABLES.includes(v)));
    assert.equal(VARIABLES.length, 28);
});

test('a block holds a theme whole, and reads back as the same look', () => {
    for (const theme of [
        DUSK,
        { ...DUSK, name: 'Été 🌆 "at" the \\ lake', dark: false, radiusField: 2 },
        { ...DUSK, name: 'x'.repeat(40), base: 'oklch(25.33% 0.016 252.42)', base300: '#101010', errorContent: 'oklch(14.79% 0.038 27.33)', sizeSelector: 0.3125, noise: 1 },
    ]) {
        const code = themeCode(theme);
        assert.ok(code.startsWith('@plugin "daisyui/theme" {\n'), code);
        assert.ok(code.endsWith('\n}'));
        assert.equal(code.split('\n').length, 5 + VARIABLES.length + 1, 'a line for each variable');
        const back = readCode(code);
        assert.equal(back.name, theme.name);
        assert.equal(back.dark, theme.dark);
        // Every variable is in the block, so the theme read holds them all,
        // derived or not, and sets each as the one it came from showed it.
        assert.deepEqual(variables(back), { ...variables(theme), ...Object.fromEntries(['sizeSelector', 'sizeField', 'border', 'depth', 'noise']
            .filter((f) => theme[f] === undefined).map((f) => [FIELDS[f], `${SHOWN[f]}${{ border: 'px', depth: '', noise: '' }[f] ?? 'rem'}`])) });
        assert.deepEqual(readCode(themeCode(back)), back, 'and again, the same');
    }
});

test("DaisyUI's generator's block reads as it shows there", () => {
    const theme = readCode(GENERATOR);
    assert.equal(theme.name, 'mytheme');
    assert.equal(theme.dark, false);
    const vars = variables(theme);
    for (const line of GENERATOR.split('\n').filter((l) => l.trim().startsWith('--'))) {
        const [name, value] = line.trim().slice(0, -1).split(': ');
        assert.equal(vars[name], value, name);
    }
    assert.equal(Object.keys(vars).length, VARIABLES.length);
    // As pasted from a message: on one line, or without its braces.
    assert.deepEqual(readCode(GENERATOR.replace(/\n/g, ' ')), theme);
    const inside = GENERATOR.slice(GENERATOR.indexOf('{') + 1, GENERATOR.lastIndexOf('}'));
    assert.deepEqual(readCode(inside), theme);
    assert.deepEqual(readCode(`/* mine */ [data-theme="mytheme"] {${inside}}`), theme);
});

test('what a block holds that the page reads is kept, and the rest left out', () => {
    const block = (...lines) => `@plugin "daisyui/theme" {\n${lines.map((l) => `  ${l};`).join('\n')}\n}`;
    // Light's colors for what a light block leaves out, Dark's for a dark one.
    const some = readCode(block('name: "some"', 'color-scheme: light', '--color-primary: #ff0000'));
    assert.deepEqual({ ...some }, { name: 'some', dark: false, ...DAISY.light, primary: '#ff0000', radiusSelector: 0.5, radiusField: 0.25, radiusBox: 0.5 });
    assert.equal(readCode(block('color-scheme: "dark"', '--color-error: #ff0000')).base, DAISY.dark.base);
    // A dark background with no scheme named is Dark.
    assert.equal(readCode(block('--color-base-100: #101418')).dark, true);
    assert.equal(readCode(block('--color-base-100: #fafafa')).dark, false);
    // Each value the page can't read is left out, and the theme reads still.
    const hostile = readCode(block(
        'name: "hostile"',
        '--color-base-100: url(https://evil.example/x.png)',
        '--color-primary: red',
        '--color-secondary: oklch(50% 0.1 20) !important',
        '--color-accent: var(--color-error)',
        '--color-info: expression(alert(1))',
        '--color-error: #00ff00',
        '--radius-box: 9rem',
        '--border: 1px; background: url(https://evil.example/y.png)',
        '--depth: calc(1)',
        '--noise: 1',
        '--color-base-200: #12345',
        'background-image: url(https://evil.example/z.png)',
        '--fx-noise: url(https://evil.example/n.svg)',
    ));
    assert.deepEqual({ ...hostile }, { name: 'hostile', dark: false, ...DAISY.light, error: '#00ff00', radiusSelector: 0.5, radiusField: 0.25, radiusBox: 0.5, border: 1, noise: 1 });
    const set = Object.values(variables(hostile)).join(' ');
    assert.ok(!/url|evil|var\(|expression|important|;/.test(set), set);
    // A name that isn't one is replaced; one quoted with escapes reads.
    assert.equal(readCode(block('name: "two\\nlines"', '--color-primary: #ff0000')).name, 'Pasted theme');
    assert.equal(readCode(block(`name: "${'x'.repeat(41)}"`, '--color-primary: #ff0000')).name, 'Pasted theme');
    assert.equal(readCode(block('name: "a \\"b\\" ; } c"', '--color-primary: #ff0000')).name, 'a "b" ; } c');
    assert.equal(readCode(block("name: 'single'", '--color-primary: #ff0000')).name, 'single');
    for (const [what, bad] of [
        ['nothing', ''],
        ['words', 'a theme, honest'],
        ['no value the page reads', block('name: "x"', '--color-primary: red', '--radius-box: 9rem')],
        ['no known key', block('--color-primary-light: #ff0000', 'color: #ff0000')],
        ['too long', block('--color-primary: #ff0000', `name: "${'x'.repeat(9000)}"`)],
        ['JSON', '{"name":"x","base":"#ffffff"}'],
    ]) {
        assert.equal(readCode(bad), null, what);
    }
});

test('a theme v1.13.0 kept reads and looks as it did', () => {
    const themes = readThemes(released.stored);
    assert.equal(themes.length, released.themes.length);
    for (const [i, theme] of themes.entries()) {
        assert.equal(theme.id, released.themes[i].id);
        assert.deepEqual(variables(theme), released.themes[i].variables, theme.name);
        // Kept again, it holds the same look.
        assert.deepEqual(variables(cleanTheme(JSON.parse(JSON.stringify(theme)))), released.themes[i].variables);
    }
});

test("a v1.13.0 code reads as the theme it held, and one that isn't refused", () => {
    const themes = readThemes(released.stored);
    for (const [i, { code, variables: was }] of released.themes.entries()) {
        const theme = readCode(code);
        assert.deepEqual(theme, (({ id, ...t }) => t)(themes[i]));
        assert.deepEqual(variables(theme), was);
        assert.deepEqual(readCode(`  ${code.toUpperCase()}\n`), theme, 'as pasted, with space around it, in capitals');
    }
    const code = released.themes[0].code;
    const [version, dark, corners, colors, name] = code.slice(CODE.length).split('.');
    const made = (...parts) => CODE + parts.join('.');
    const hexOfText = (text) => [...new TextEncoder().encode(text)].map((b) => b.toString(16).padStart(2, '0')).join('');
    for (const [what, bad] of [
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
        ['a name of 41 characters', made(version, dark, corners, colors, hexOfText('x'.repeat(41)))],
        ['a name of half a byte', made(version, dark, corners, colors, `${name}4`)],
        ["a name that isn't UTF-8", made(version, dark, corners, colors, 'ff')],
        ['a name of spaces', made(version, dark, corners, colors, hexOfText('   '))],
        ['a name with a line break', made(version, dark, corners, colors, hexOfText('two\nlines'))],
        ['a name that turns its text around', made(version, dark, corners, colors, hexOfText(`abc${String.fromCodePoint(0x202e)}def`))],
        ['JSON', `${CODE}{"name":"x"}`],
    ]) {
        assert.equal(readCode(bad), null, what);
    }
});

test('a theme is only the fields a theme has, each as the page writes it', () => {
    assert.deepEqual(cleanTheme({ ...DUSK, id: 'abc', font: 'Comic Sans', '--color-base-100': 'url(x)' }), DUSK);
    const { radiusSelector, radiusField, radiusBox, ...cornerless } = DUSK;
    assert.deepEqual(cleanTheme({ ...cornerless, corners: 'round' }), { ...cornerless, radiusSelector: 1, radiusField: 2, radiusBox: 1 });
    assert.deepEqual(cleanTheme({ ...DUSK, corners: 'square' }), DUSK, 'radii it holds outrank corners named');
    for (const bad of [
        null, 'a theme', [], { ...DUSK, name: '' }, { ...DUSK, name: 7 }, { ...DUSK, dark: 'yes' }, cornerless, { ...cornerless, corners: 'pointy' },
        { ...cornerless, corners: 'toString' }, { ...DUSK, base: 'red' }, { ...DUSK, primary: '#FFF' }, { ...DUSK, error: undefined },
        { ...DUSK, accent: '#12345g' }, { ...DUSK, info: 'var(--color-primary)' }, { ...DUSK, base200: '' }, { ...DUSK, base200: 'OKLCH(50% 0.1 20)' },
        { ...DUSK, primaryContent: 'oklch(50% 0.1 20 / 0.5)' }, { ...DUSK, radiusBox: '1rem' }, { ...DUSK, radiusBox: 3 }, { ...DUSK, border: 0.25 },
        { ...DUSK, depth: true }, { ...DUSK, sizeField: 0.123456 },
    ]) {
        assert.equal(cleanTheme(bad), null, JSON.stringify(bad));
    }
    assert.equal(cleanName('  Dusk  '), 'Dusk');
    assert.equal(cleanName('🌆'.repeat(40)), '🌆'.repeat(40));
    assert.equal(cleanName('x'.repeat(41)), '');
});

test("the editor's changes take what derived from the color changed away", () => {
    const held = { ...DUSK, base200: '#111111', base300: '#222222', primaryContent: '#ffffff', errorContent: '#000000' };
    const based = editTheme(held, { base: '#303030' });
    assert.equal(based.base, '#303030');
    assert.ok(!('base200' in based) && !('base300' in based));
    assert.equal(based.primaryContent, '#ffffff');
    const primary = editTheme(held, { primary: '#ff0000' });
    assert.ok(!('primaryContent' in primary));
    assert.equal(primary.base200, '#111111');
    assert.equal(primary.errorContent, '#000000');
    assert.deepEqual(editTheme(held, { base: held.base, name: 'New' }), { ...held, name: 'New' }, 'the same color changes nothing');
    assert.equal(held.base200, '#111111', 'the theme it started from stays as it was');
});

test('a color that reads poorly on the background says by how much', () => {
    assert.equal(lowContrast(DUSK, 'primary'), 0);
    assert.ok(lowContrast({ ...DUSK, text: '#3a4048' }, 'text') > 1);
    assert.equal(lowContrast({ ...DUSK, text: '#ffffff' }, 'text'), 0);
    const near = lowContrast({ ...DUSK, primary: '#2a3038' }, 'primary');
    assert.ok(near > 1 && near < 3, String(near));
    // A color on the edge: 4 to 1 reads as a color but not as the text.
    const gray = { ...DUSK, base: '#ffffff', primary: '#ff0000', text: '#ff0000' };
    assert.equal(lowContrast(gray, 'primary'), 0);
    assert.ok(lowContrast(gray, 'text') > 3.9);
    assert.equal(lowContrast({ ...DUSK, base: 'oklch(100% 0 0)', text: 'oklch(21% 0.006 285.885)' }, 'text'), 0);
    // A surface, under its own text, and the background itself aren't measured.
    for (const field of ['base', 'neutral', 'secondary', 'accent']) assert.equal(lowContrast({ ...DUSK, neutral: DUSK.base, secondary: DUSK.base, accent: DUSK.base }, field), 0, field);
});

test('a typed color reads with or without its #, and in three digits', () => {
    assert.equal(hexColor('#1D232A'), '#1d232a');
    assert.equal(hexColor(' 1d232a '), '#1d232a');
    assert.equal(hexColor('#abc'), '#aabbcc');
    for (const bad of ['', '#12', '#12345', '#1234567', 'red', 'rgb(1,2,3)', '#12345g']) assert.equal(hexColor(bad), '', bad);
});

test('what the browser kept reads as themes, and what else is there is left out', () => {
    const kept = [{ id: 'aaaaaaaa', ...DUSK }, { id: 'bbbbbbbb', ...DUSK, name: 'Dawn', dark: false, base: 'oklch(98% 0.01 80)', border: 2 }];
    assert.deepEqual(readThemes(JSON.stringify(kept)), kept);
    assert.deepEqual(readThemes(null), []);
    assert.deepEqual(readThemes('not json'), []);
    assert.deepEqual(readThemes('{"id":"aaaaaaaa"}'), []);
    const mixed = [kept[0], { ...kept[1], id: 'aaaaaaaa' }, { ...kept[1], id: '../../etc' }, { id: 'cccccccc', ...DUSK, base: 'red' }, 'a string', kept[1]];
    assert.deepEqual(readThemes(JSON.stringify(mixed)), kept, 'an id twice, an id that is none, and a color that is none are left out');
    const many = Array.from({ length: MOST_THEMES + 5 }, (_, i) => ({ id: `theme${String(i).padStart(3, '0')}`, ...DUSK }));
    assert.equal(readThemes(JSON.stringify(many)).length, MOST_THEMES);
});
