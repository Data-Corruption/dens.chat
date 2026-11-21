// Appearance: a page of the settings that takes the window, in the
// shape of DaisyUI's theme generator. At the left the themes: New theme
// and Paste a code, the member's own, then the built-in ones. In the
// middle the one chosen, with Messages and Calls in tabs beside its
// controls. At the right a small den in that theme, which shows every
// other Appearance setting too. Choosing a theme shows it there alone, and
// Use wears it. A theme's edits wait for Save; Messages and Calls apply as
// they're changed. Under a large window the themes are a tab beside the
// editor, and on a phone each column is a tab along the foot.

import { Fragment } from 'preact';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { ALWAYS, ON_HOVER, setAnimations, useAnimations } from './animations.js';
import {
    DIM_MOST, DIM_STEP, HOVER_MOST, KINDS, backgroundURL, checkBackground, removeBackground, setBackground, setDim, setHover, slideDim, slideHover,
} from './background.js';
import { setCallTimer, useCallTimer } from '../voice/calltimer.js';
import {
    Actions, Button, Choices, ColorField, CopyButton, ErrorText, Field, PLAIN, Slider, StatusText, Steps, SubmitButton, TextInput, Ticks, Tip, Toggle, useAction,
    useBackground,
} from '../ui/components.jsx';
import { DENSITY_COMPACT, DENSITY_DEFAULT, setDensity, useDensity } from './density.js';
import { setFileNames, useFileNames } from './filenames.js';
import { EyeIcon, MenuIcon, StatusIcon } from '../ui/icons.jsx';
import { STYLES_ALL, STYLES_COLORS, STYLES_OFF, setNameStyles, useNameStyles } from './names.js';
import { useNarrow } from '../core/nav.js';
import { DenPreview } from '../ui/preview.jsx';
import { MEDIA, PLAYERS, SIZES, setSize, useSize } from './sizes.js';
import { setStaffMarks, useStaffMarks } from '../ui/staffmarks.js';
import {
    THEMES, builtInTheme, forgetTheme, keepTheme, onTheme, ownChoice, ownThemes, setTheme, shownTheme, systemTheme, themeChoice, themeName,
} from './theme.js';
import { MOST_THEMES, NAME_MOST, cleanName, editTheme, hexColor, hexOf, lowContrast, numberShown, readCode, readColor, sameTheme, themeCode, variables } from './themes.js';
import { useCoarse } from '../core/touch.js';
import { MARKS_ALWAYS, MARKS_HOVER, MARKS_NEVER, setVerifiedMarks, useVerifiedMarks } from '../core/verified.js';

// THREE is the width from which the page has its three columns.
const THREE = '(min-width: 80rem)';

// useWide says whether the window is at least as wide as query says, kept
// current.
function useWide(query) {
    const [wide, setWide] = useState(() => !!window.matchMedia?.(query).matches);
    useEffect(() => {
        const q = window.matchMedia?.(query);
        if (!q) return undefined;
        const change = () => setWide(q.matches);
        change();
        q.addEventListener('change', change);
        return () => q.removeEventListener('change', change);
    }, [query]);
    return wide;
}

// The built-in themes as the list shows them: System, Light and Dark,
// then the rest.
const FIRST = ['system', 'light', 'dark'];
const BUILT_IN = [...FIRST, ...THEMES.filter((t) => !FIRST.includes(t))];
const OWN = 'custom:';

// The foot's tabs, each a column, and the middle column's.
const FOOT = [['themes', 'Themes'], ['editor', 'Editor'], ['preview', 'Preview']];
const PANES = [['theme', 'Theme'], ['messages', 'Messages'], ['calls', 'Calls']];

/** AppearancePage is the page. guard is a ref the settings hold, which it
    points at what asks before they leave it with a theme's edits unsaved:
    given what leaving does, it returns true while it asks. */
export function AppearancePage({ guard }) {
    const narrow = useNarrow();
    const three = useWide(THREE);
    const layout = narrow ? 'phone' : three ? 'three' : 'two';
    // choice is the theme the page wears, picked the one the page shows,
    // and drafts each theme of the member's own edited and not saved, by
    // its id.
    const [choice, setChoice] = useState(themeChoice);
    const [own, setOwn] = useState(ownThemes);
    const [picked, setPicked] = useState(themeChoice);
    const [drafts, setDrafts] = useState({});
    const [leaving, setLeaving] = useState(null);
    const [tab, setTab] = useState('themes');
    const [pane, setPane] = useState('theme');
    const [error, setError] = useState('');
    const [saveError, setSaveError] = useState('');
    useEffect(() => onTheme(() => {
        setChoice(themeChoice());
        setOwn(ownThemes());
    }), []);

    const saved = own.find((t) => ownChoice(t.id) === picked) || null;
    // A theme of the member's own that's gone shows the one worn.
    const at = picked.startsWith(OWN) && !saved ? choice : picked;
    const mine = (at === picked && saved) || null;
    const dirty = (id) => {
        const kept = own.find((t) => t.id === id);
        return !!(kept && drafts[id] && !sameTheme(drafts[id], kept));
    };
    const edited = !!mine && dirty(mine.id);
    const unsaved = Object.keys(drafts).some(dirty);
    const builtIn = mine ? '' : at === 'system' ? systemTheme() : at;
    const shown = useMemo(() => {
        if (mine) return drafts[mine.id] || mine;
        return builtInTheme(builtIn, at === 'system' ? 'System' : themeName(at));
    }, [mine, drafts, builtIn, at]);
    const full = own.length >= MOST_THEMES;

    // Leaving with edits unsaved asks first, as Manage does.
    const asks = useRef(null);
    asks.current = unsaved ? (action) => {
        setLeaving(() => action);
        return true;
    } : null;
    useEffect(() => {
        if (!guard) return undefined;
        guard.current = (action) => asks.current?.(action) || false;
        return () => {
            guard.current = null;
        };
    }, [guard]);

    function pick(next) {
        setPicked(next);
        setError('');
        setSaveError('');
    }
    // add keeps a theme the member made, pasted or copied, and shows it.
    function add(theme) {
        const id = keepTheme(theme);
        if (!id) return setError("That theme couldn't be kept.");
        pick(ownChoice(id));
        if (layout !== 'three') setTab('editor');
    }
    function change(changes) {
        const id = mine.id;
        setSaveError('');
        setDrafts((d) => ({ ...d, [id]: editTheme(d[id] || mine, changes) }));
    }
    const drop = (id) => setDrafts(({ [id]: _, ...rest }) => rest);
    function save() {
        const name = cleanName(shown.name);
        if (!name) return setSaveError(`A theme's name is up to ${NAME_MOST} characters.`);
        if (!keepTheme({ ...shown, name }, mine.id)) return setSaveError("That theme couldn't be kept.");
        drop(mine.id);
        setSaveError('');
    }
    function remove() {
        drop(mine.id);
        forgetTheme(mine.id);
        pick(themeChoice());
    }
    function discard() {
        const then = leaving;
        setDrafts({});
        setLeaving(null);
        setSaveError('');
        then?.();
    }

    const bar = (edited || leaving) && (
        <div role="status" data-theme-bar class="flex shrink-0 flex-wrap items-center gap-2 border-t border-base-300 bg-base-200 px-4 py-2">
            {leaving ? (
                <>
                    <span class="min-w-0 flex-1 text-sm">Discard your unsaved changes?</span>
                    <Button kind="danger" filled onClick={discard}>Discard</Button>
                    <Button kind="quiet" onClick={() => setLeaving(null)}>Keep editing</Button>
                </>
            ) : (
                <>
                    <span class="min-w-0 flex-1 text-sm">{saveError ? <StatusText kind="error" inline>{saveError}</StatusText> : 'You have unsaved changes.'}</span>
                    <Button kind="primary" onClick={save}>Save</Button>
                    <Button kind="quiet" onClick={() => drop(mine.id)}>Discard</Button>
                </>
            )}
        </div>
    );
    const list = (
        <ThemeList own={own} choice={choice} picked={at} full={full} error={error} unsaved={dirty} onPick={pick}
            onNew={() => add(shownTheme('My theme'))} onPaste={add} />
    );
    const editor = (
        <MiddlePane pane={pane} onPane={setPane}>
            {pane === 'messages' && <MessagesPane />}
            {pane === 'calls' && <CallsPane />}
            {pane === 'theme' && (mine ? (
                <ThemeEditor theme={shown} worn={choice === at} edited={edited} onChange={change} onUse={() => setTheme(at)} onRemove={remove} />
            ) : (
                <BuiltInTheme theme={shown} system={at === 'system'} worn={choice === at} full={full} onUse={() => setTheme(at)}
                    onCustomize={() => add(builtInTheme(builtIn))} />
            ))}
        </MiddlePane>
    );
    const preview = <PreviewColumn theme={shown} builtIn={builtIn} />;
    if (layout === 'three') {
        return (
            <div class="flex min-h-0 flex-1" data-appearance="three">
                <div class="flex w-72 shrink-0 flex-col border-r border-base-300">{list}</div>
                <div class="flex w-[21rem] shrink-0 flex-col border-r border-base-300">
                    {editor}
                    {bar}
                </div>
                {preview}
            </div>
        );
    }
    if (layout === 'two') {
        const left = tab === 'themes' ? 'themes' : 'editor';
        return (
            <div class="flex min-h-0 flex-1" data-appearance="two">
                <div class="flex w-[21rem] shrink-0 flex-col border-r border-base-300">
                    {left === 'themes' ? list : editor}
                    {bar}
                    <FootTabs tabs={FOOT.slice(0, 2)} tab={left} onTab={setTab} />
                </div>
                {preview}
            </div>
        );
    }
    return (
        <div class="flex min-h-0 flex-1 flex-col" data-appearance="phone">
            {tab === 'themes' ? list : tab === 'editor' ? editor : preview}
            {bar}
            <FootTabs tabs={FOOT} tab={tab} onTab={setTab} />
        </div>
    );
}

// FOOT_ICONS are the foot's tabs' icons: the themes as tiles, a brush for
// the editor and an eye for the preview.
const FOOT_ICONS = {
    themes: <MenuIcon name="themes" size="h-5 w-5" />,
    editor: <MenuIcon name="brush" size="h-5 w-5" />,
    preview: <EyeIcon size="h-5 w-5" />,
};

// FootTabs switches the columns where they don't all fit: a tab each, its
// icon over its name, the one shown underlined.
function FootTabs({ tabs, tab, onTab }) {
    return (
        <div role="tablist" aria-label="Columns" class="flex shrink-0 border-t border-base-300 bg-base-100 pb-[env(safe-area-inset-bottom)]" data-foot-tabs>
            {tabs.map(([id, label]) => (
                <button key={id} type="button" role="tab" aria-selected={tab === id} data-foot-tab={id} onClick={() => onTab(id)}
                    class={`flex min-h-14 flex-1 flex-col items-center justify-center gap-0.5 text-xs ${tab === id ? 'font-semibold' : 'text-base-content/60 hover:text-base-content'}`}>
                    {FOOT_ICONS[id]}
                    <span>{label}</span>
                    <span class={`h-0.5 w-8 rounded-full ${tab === id ? 'bg-base-content' : ''}`} aria-hidden="true"></span>
                </button>
            ))}
        </div>
    );
}

// ThemeList lists the themes to show: New theme and Paste a code, which
// add one to the member's own, their own, then the built-in ones, each in
// its colors, the one shown checked and the one worn marked.
function ThemeList({ own, choice, picked, full, error, unsaved, onPick, onNew, onPaste }) {
    const box = useRef(null);
    // The theme shown stays in view, as one just added, first in the list.
    useEffect(() => {
        box.current?.querySelector('[data-theme-row] input:checked')?.closest('[data-theme-row]')?.scrollIntoView?.({ block: 'nearest' });
    }, [picked]);
    const [pasting, setPasting] = useState(false);
    const [code, setCode] = useState('');
    const [refused, setRefused] = useState('');
    function paste(e) {
        e.preventDefault();
        const theme = readCode(code);
        if (!theme) return setRefused("That isn't a theme's code.");
        setCode('');
        setRefused('');
        setPasting(false);
        onPaste(theme);
    }
    const row = (value, name, swatch, mark) => (
        <ThemeRow key={value} value={value} name={name} swatch={swatch} picked={picked === value} worn={choice === value} mark={mark} onPick={onPick} />
    );
    return (
        <div ref={box} data-panel data-themes class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-3">
            <div class="grid grid-cols-2 gap-2 *:whitespace-nowrap">
                <Button disabled={full} onClick={onNew}>New theme</Button>
                <Button disabled={full} onClick={() => setPasting(true)}>Paste a code</Button>
            </div>
            {pasting && (
                <form class="flex flex-col gap-2" onSubmit={paste}>
                    <textarea class="textarea w-full font-mono text-xs" rows="4" aria-label="A theme's code" placeholder={'@plugin "daisyui/theme" { … }'}
                        value={code} onInput={(e) => setCode(e.currentTarget.value)} required autofocus {...PLAIN}></textarea>
                    <Actions>
                        <SubmitButton>Add</SubmitButton>
                        <Button kind="quiet" onClick={() => {
                            setPasting(false);
                            setRefused('');
                        }}>Cancel</Button>
                    </Actions>
                </form>
            )}
            {full && <p class="text-sm text-base-content/70">You have {MOST_THEMES} themes. Remove one to add another.</p>}
            <ErrorText message={refused || error} />
            {own.length > 0 && (
                <ThemeGroup label="My themes">
                    {own.map((t) => row(ownChoice(t.id), t.name, <Swatch own={t} />, unsaved(t.id)))}
                </ThemeGroup>
            )}
            <ThemeGroup label="Built in">
                {BUILT_IN.map((t) => row(t, t === 'system' ? 'System' : themeName(t), t === 'system' ? <SystemSwatch /> : <Swatch theme={t} />))}
            </ThemeGroup>
        </div>
    );
}

function ThemeGroup({ label, children }) {
    return (
        <div class="flex flex-col gap-1">
            <h3 class="px-2 text-sm font-semibold text-base-content/60">{label}</h3>
            <div role="radiogroup" aria-label={label} class="flex flex-col">{children}</div>
        </div>
    );
}

// ThemeRow is a theme in the list: its colors and its name, a dot where
// it has edits unsaved, and a tick where the page wears it.
function ThemeRow({ value, name, swatch, picked, worn, mark, onPick }) {
    return (
        <label data-theme-row={value} data-worn={worn || undefined}
            class="flex min-h-11 cursor-pointer items-center gap-3 rounded-field px-2 hover:bg-base-200 has-[:checked]:bg-base-300 has-[:checked]:font-medium has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-primary md:min-h-9">
            <input type="radio" name="theme" class="sr-only" value={value} checked={picked} onChange={() => onPick(value)} />
            {swatch}
            <span class="min-w-0 flex-1 truncate">{name}</span>
            {mark && <span class="size-2 shrink-0 rounded-full bg-warning" title="Unsaved changes" data-unsaved></span>}
            {worn && (
                <span class="text-success" title="In use">
                    <MenuIcon name="read" />
                    <span class="sr-only">In use</span>
                </span>
            )}
        </label>
    );
}

// MiddlePane is the middle column: its tabs, Theme, Messages and Calls,
// over the one chosen.
function MiddlePane({ pane, onPane, children }) {
    return (
        <div class="flex min-h-0 flex-1 flex-col">
            <div role="tablist" aria-label="Appearance" class="tabs tabs-border shrink-0 px-3 pt-1">
                {PANES.map(([id, label]) => (
                    <button key={id} type="button" role="tab" aria-selected={pane === id} data-pane-tab={id} class={`tab ${pane === id ? 'tab-active' : ''}`}
                        onClick={() => onPane(id)}>
                        {label}
                    </button>
                ))}
            </div>
            <div data-panel role="tabpanel" class="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto p-4">{children}</div>
        </div>
    );
}

// ThemeHead is a theme's name, or the field that names it, in a row with
// Use, as tall as the field, or that the page wears it.
function ThemeHead({ worn, onUse, children }) {
    return (
        <div class="flex items-center gap-3" data-theme-head>
            <div class="min-w-0 flex-1">{children}</div>
            {worn ? (
                <StatusText kind="success" inline data-worn-mark>In use</StatusText>
            ) : onUse && (
                <Button kind="primary" size="md" onClick={onUse}>Use</Button>
            )}
        </div>
    );
}

// BuiltInTheme shows one of the built-in themes: its name, its colors, Use
// and Customize, which copies it into the member's own.
function BuiltInTheme({ theme, system, worn, full, onUse, onCustomize }) {
    return (
        <div class="flex flex-col gap-5" data-theme-editor data-built-in>
            <div class="flex flex-col gap-1">
                <ThemeHead worn={worn} onUse={onUse}>
                    <h3 class="truncate text-lg font-semibold" data-theme-name>{theme.name}</h3>
                </ThemeHead>
                {system && <p class="text-sm text-base-content/70">Light or Dark, as your device is set.</p>}
            </div>
            <ColorGrid theme={theme} />
            <div class="flex flex-col gap-2">
                <div><Button disabled={full} onClick={onCustomize}>Customize</Button></div>
                {full && <p class="text-sm text-base-content/70">You have {MOST_THEMES} themes. Remove one to add another.</p>}
            </div>
        </div>
    );
}

// RADII are the corners by what each rounds, and ROUNDINGS the radii
// offered for each, in rem, as the generator offers them.
const RADII = [['radiusBox', 'Cards'], ['radiusField', 'Buttons'], ['radiusSelector', 'Toggles']];
const ROUNDINGS = [[0, 'Square'], [0.25, 'Slight'], [0.5, 'Rounded'], [1, 'Round'], [2, 'Full']];
// A rounding's tile draws a box's top right corner, as the generator's
// do: the box runs off the tile's left and foot, shaded, and the one
// chosen is lighter with its edge in the primary color.
const CORNER_TILE = 'group relative flex h-11 min-w-0 flex-1 cursor-pointer overflow-hidden rounded-md bg-base-200 has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-primary';
const CORNER = 'absolute -bottom-0.5 -left-0.5 right-2.5 top-2.5 border-2 border-base-content/25 bg-base-300 group-hover:border-base-content/50 peer-checked:border-primary peer-checked:bg-base-100';
// BORDER is the border's width as the slider offers it, in pixels.
const BORDER = { least: 0.5, most: 2, step: 0.5 };

// readTyped reads a color typed in the editor: oklch(), or hex with or
// without its #.
const readTyped = (text) => readColor(text) || hexColor(text);

// offered are the choices of a number, with one a block gave that isn't
// among them as itself.
const offered = (choices, value, unit) => (choices.some(([v]) => v === value) ? choices : [...choices, [value, `${value} ${unit}`]].sort((a, b) => a[0] - b[0]));

// ThemeEditor changes a theme of the member's own: its name, Use, its
// colors, each a tile chosen to change it below them, which says where it
// reads poorly on the background; the corners as tiles, the border's width
// on a slider, depth, noise and Dark; then Copy code and Remove.
function ThemeEditor({ theme, worn, edited, onChange, onUse, onRemove }) {
    const [color, setColor] = useState('base');
    const label = COLOR_NAMES[color];
    const border = numberShown(theme, 'border');
    return (
        <div class="flex flex-col gap-5" data-theme-editor>
            <Field label="Name">
                <ThemeHead worn={worn} onUse={edited ? null : onUse}>
                    <TextInput value={theme.name} onInput={(name) => onChange({ name })} maxlength={NAME_MOST * 2} data-theme-name />
                </ThemeHead>
            </Field>
            <div class="flex flex-col gap-3">
                <ColorGrid theme={theme} chosen={color} onChoose={setColor} />
                <div class="flex flex-col gap-1">
                    <ColorField key={color} long label={label} value={theme[color]} hex={hexOf(theme[color])} read={readTyped}
                        onChange={(c) => onChange({ [color]: c })} />
                    {lowContrast(theme, color) > 0 && <StatusText kind="warning" size="xs">Hard to read on the background</StatusText>}
                </div>
            </div>
            <div role="group" aria-label="Corners" class="flex flex-col gap-3" data-theme-corners>
                <h3 class="text-sm font-semibold">Corners</h3>
                {RADII.map(([field, name]) => (
                    <div key={field} role="radiogroup" aria-label={name} class="flex flex-col gap-1" data-radius={field}>
                        <span class="text-sm">{name}</span>
                        <div class="flex gap-2">
                            {offered(ROUNDINGS, theme[field], 'rem').map(([v, shape]) => (
                                <label key={v} title={shape} class={CORNER_TILE}>
                                    <input type="radio" class="peer sr-only" name={`radius-${field}`} value={String(v)} aria-label={shape} checked={theme[field] === v}
                                        onChange={() => onChange({ [field]: v })} />
                                    <span aria-hidden="true" class={CORNER} style={{ borderTopRightRadius: `${v}rem` }}></span>
                                </label>
                            ))}
                        </div>
                    </div>
                ))}
            </div>
            <div class="flex flex-col gap-1" data-border-field>
                <span class="flex items-center justify-between text-sm">
                    <span>Border</span>
                    <span class="tabular-nums text-base-content/70">{border} px</span>
                </span>
                <Slider class="w-full" min={BORDER.least} max={BORDER.most} step={BORDER.step} value={border} aria-label="Border" aria-valuetext={`${border} px`}
                    data-border onInput={(e) => onChange({ border: Number(e.currentTarget.value) })} />
                <Ticks count={4} />
            </div>
            <div role="group" aria-label="Options" class="flex flex-col gap-3">
                <h3 class="text-sm font-semibold">Options</h3>
                <Toggle id="theme-depth" label="Depth" tip="Buttons and fields raised a little, with a shade." checked={numberShown(theme, 'depth') > 0}
                    onChange={(on) => onChange({ depth: on ? 1 : 0 })} />
                <Toggle id="theme-noise" label="Noise" tip="A grain over buttons, toggles and menus' picks." checked={numberShown(theme, 'noise') > 0}
                    onChange={(on) => onChange({ noise: on ? 1 : 0 })} />
                <Toggle id="theme-dark" label="Dark" tip="Scroll bars and the browser's own controls drawn dark." checked={theme.dark}
                    onChange={(on) => onChange({ dark: on })} />
            </div>
            <div class="flex flex-wrap gap-2 border-t border-base-300 pt-4">
                <CopyButton text={themeCode({ ...theme, name: cleanName(theme.name) || 'My theme' })} label="Copy code" />
                <Button kind="danger" onClick={onRemove}>Remove</Button>
            </div>
        </div>
    );
}

// COLOR_NAMES are the colors a member sets, by what the editor calls them:
// those the page draws with. A theme holds secondary and accent, each
// color's text and the deeper backgrounds without a control of their own.
const COLOR_NAMES = {
    base: 'Background', text: 'Text', primary: 'Primary', neutral: 'Neutral', info: 'Info', success: 'Success', warning: 'Warning', error: 'Error',
};
// The tints' rows, two to a row, each a color and its text.
const TINT_ROWS = [['primary', 'neutral'], ['info', 'success'], ['warning', 'error']];

// ColorGrid shows a theme's colors as tiles, in the generator's rows: the
// background, the two deeper ones and the text on it as an A; then each
// tint beside its text, an A on it, which derives from it. With onChoose,
// each color it sets is a tile that chooses it, the one chosen ringed, and
// one that reads poorly on the background is marked.
function ColorGrid({ theme, chosen, onChoose }) {
    const vars = useMemo(() => variables(theme), [theme]);
    const tile = 'relative flex h-12 items-center justify-center rounded-field border border-base-content/15 text-xl font-semibold';
    const color = (field, look, letter) => {
        const style = { backgroundColor: look[0], color: look[1] };
        const low = lowContrast(theme, field) > 0;
        if (!onChoose) {
            return <span class={tile} style={style} data-color-tile={field} aria-hidden="true">{letter}</span>;
        }
        return (
            <button type="button" class={`${tile} cursor-pointer ${chosen === field ? 'ring-2 ring-base-content ring-offset-2 ring-offset-base-100' : ''}`} style={style}
                aria-label={COLOR_NAMES[field]} aria-pressed={chosen === field} data-color-tile={field} onClick={() => onChoose(field)}>
                {letter}
                {low && (
                    <span class="absolute -right-1.5 -top-1.5 flex rounded-full bg-base-100 p-0.5 text-warning" title="Hard to read on the background" data-low-contrast={COLOR_NAMES[field]}>
                        <StatusIcon kind="warning" size="h-3.5 w-3.5" />
                    </span>
                )}
            </button>
        );
    };
    const shade = (variable) => <span class={tile} style={{ backgroundColor: vars[variable] }} aria-hidden="true"></span>;
    const caption = (text, span = 'col-span-2') => <span class={`${span} -mt-0.5 pb-1 text-xs text-base-content/70`}>{text}</span>;
    return (
        <div role="group" aria-label="Colors" class="grid grid-cols-4 gap-x-2 gap-y-1" data-colors>
            {color('base', [vars['--color-base-100']])}
            {shade('--color-base-200')}
            {shade('--color-base-300')}
            {color('text', [vars['--color-base-100'], vars['--color-base-content']], 'A')}
            {caption('Background', 'col-span-3')}
            {caption('Text', '')}
            {TINT_ROWS.map((pair) => (
                <Fragment key={pair[0]}>
                    {pair.map((t) => (
                        <Fragment key={t}>
                            {color(t, [vars[`--color-${t}`]])}
                            <span class={tile} style={{ backgroundColor: vars[`--color-${t}`], color: vars[`--color-${t}-content`] }} aria-hidden="true">A</span>
                        </Fragment>
                    ))}
                    {pair.map((t) => <Fragment key={`${t}-name`}>{caption(COLOR_NAMES[t])}</Fragment>)}
                </Fragment>
            ))}
        </div>
    );
}

// The sizes' names, as the page shows them.
const SIZE_NAMES = { small: 'Small', medium: 'Medium', large: 'Large', largest: 'Largest' };

// MessagesPane chooses how messages show: their Density; on a wide window,
// how large their photos and videos show, and their YouTube players; how
// the names members styled show; when animated pictures and the den's
// emoji play; whether staff's marks and the verified mark show; but on a
// touch screen, whether a photo's or a video's name shows on hover; and
// the picture behind the chat. This browser keeps each, as it keeps the
// theme, but the picture, which the service keeps.
function MessagesPane() {
    const density = useDensity();
    const marks = useStaffMarks();
    const media = useSize(MEDIA);
    const players = useSize(PLAYERS);
    const styles = useNameStyles();
    const animations = useAnimations();
    const verified = useVerifiedMarks();
    const names = useFileNames();
    const narrow = useNarrow();
    const touch = useCoarse();
    const sizes = SIZES.map((s) => [s, SIZE_NAMES[s]]);
    return (
        <>
            <Choices label="Density" name="density" data="data-density" choice={density} onPick={setDensity}
                choices={[[DENSITY_DEFAULT, 'Default'], [DENSITY_COMPACT, 'Compact']]} />
            {!narrow && (
                <>
                    <Steps label="Photos and videos" data="data-media-size" choice={media} onPick={(s) => setSize(MEDIA, s)} choices={sizes} />
                    <Steps label="YouTube players" data="data-player-size" choice={players} onPick={(s) => setSize(PLAYERS, s)} choices={sizes} />
                </>
            )}
            <Choices label="Name styles" name="name-styles" data="data-name-styles" choice={styles} onPick={setNameStyles}
                tip="The colors and fonts members give their names."
                choices={[[STYLES_ALL, 'Color & font'], [STYLES_COLORS, 'Color'], [STYLES_OFF, 'Off']]} />
            <Choices label="Animations" name="animations" data="data-animations" choice={animations} onPick={setAnimations}
                tip="Animated pictures and emoji. On Hover, they play while the pointer is over their message or member, and in a profile you open; on a touch screen, only in profiles."
                choices={[[ALWAYS, 'Always'], [ON_HOVER, 'Hover']]} />
            <Choices label="Verified mark" name="verified-marks" data="data-verified-marks" choice={verified} onPick={setVerifiedMarks}
                tip="Beside people you've compared codes with."
                choices={touch ? [[MARKS_ALWAYS, 'On'], [MARKS_NEVER, 'Off']] : [[MARKS_ALWAYS, 'Always'], [MARKS_HOVER, 'Hover'], [MARKS_NEVER, 'Off']]} />
            <Toggle id="staff-marks" label="Staff marks" tip="A crown or a shield beside staff's names." checked={marks} onChange={setStaffMarks} />
            {!touch && (
                <Toggle id="file-names" label="File names on hover" tip="A photo's or a video's name, as the pointer rests on it." checked={names}
                    onChange={setFileNames} />
            )}
            <ChatBackground />
        </>
    );
}

// CallsPane chooses whether each call's length shows beside its channel,
// on until the member turns it off.
function CallsPane() {
    const on = useCallTimer();
    return <Toggle id="call-timer" label="Call timer" tip="Each call's length beside its channel." checked={on} onChange={setCallTimer} />;
}

// ChatBackground sets the picture behind the member's messages: Choose
// image, for a PNG, a JPEG or a WebP of theirs, Dim, how far the theme's
// background covers it, Hover, how far a message under the pointer does,
// and Remove. The service keeps them, so every browser paired with it
// shows them.
function ChatBackground() {
    const background = useBackground();
    const file = useRef(null);
    const act = useAction();
    function choose(picked) {
        act.run(async () => {
            const why = checkBackground(picked);
            if (why) throw new Error(why);
            await setBackground(picked);
        });
    }
    return (
        <div class="flex flex-col gap-3 border-t border-base-300 pt-4" data-chat-background>
            <h3 class="flex items-center text-sm font-semibold">
                Chat background
                <Tip id="chat-background-tip" label="About Chat background" wrap="-my-1 inline-flex">A picture behind your messages. Only you see it.</Tip>
            </h3>
            {background && (
                <div class="relative h-24 w-40 overflow-hidden rounded-box border border-base-content/10" data-background-preview aria-hidden="true">
                    <img src={backgroundURL(background.version)} alt="" class="size-full object-cover" />
                    <div class="absolute inset-0 bg-base-100" style={{ opacity: background.dim / 100 }}></div>
                </div>
            )}
            <div class="flex flex-wrap gap-2">
                <Button busy={act.busy} onClick={() => file.current?.click()}>{background ? 'Change image' : 'Choose image'}</Button>
                {background && <Button kind="quiet" disabled={act.busy} onClick={() => act.run(removeBackground)}>Remove</Button>}
                <input ref={file} type="file" accept={KINDS.join(',')} class="hidden" onChange={(e) => {
                    const picked = e.currentTarget.files?.[0];
                    e.currentTarget.value = '';
                    if (picked) choose(picked);
                }} />
            </div>
            {background && (
                <label class="flex flex-col gap-1">
                    <span class="text-sm">Dim</span>
                    <span class="flex items-center gap-3">
                        <Slider class="min-w-0 flex-1" min="0" max={DIM_MOST} step={DIM_STEP} value={background.dim}
                            aria-label="Dim" aria-valuetext={`${background.dim}%`}
                            onInput={(e) => slideDim(Number(e.currentTarget.value))}
                            onChange={(e) => setDim(Number(e.currentTarget.value)).catch((err) => act.setError(err.message))} />
                        <span class="w-10 text-right text-sm tabular-nums">{background.dim}%</span>
                    </span>
                </label>
            )}
            {background && (
                <label class="flex flex-col gap-1">
                    <span class="flex items-center text-sm">
                        Hover
                        <Tip id="background-hover-tip" label="About Hover" wrap="-my-1 inline-flex">How far a message under the pointer covers the picture.</Tip>
                    </span>
                    <span class="flex items-center gap-3">
                        <Slider class="min-w-0 flex-1" min="0" max={HOVER_MOST} step={DIM_STEP} value={background.hover}
                            aria-label="Hover" aria-valuetext={`${background.hover}%`} data-background-hover
                            onInput={(e) => slideHover(Number(e.currentTarget.value))}
                            onChange={(e) => setHover(Number(e.currentTarget.value)).catch((err) => act.setError(err.message))} />
                        <span class="w-10 text-right text-sm tabular-nums">{background.hover}%</span>
                    </span>
                </label>
            )}
            <ErrorText message={act.error} />
        </div>
    );
}

// PreviewColumn holds the den in the theme shown, as wide as the column
// lets it be, its layout by its own width.
function PreviewColumn({ theme, builtIn }) {
    return (
        <div data-panel data-preview-column class="@container min-h-0 min-w-0 flex-1 overflow-y-auto bg-base-200 p-4">
            <div class="mx-auto max-w-5xl">{theme && <DenPreview theme={theme} builtIn={builtIn} />}</div>
        </div>
    );
}

// Swatch shows a theme's colors on its background: DaisyUI's by its text,
// primary, secondary and accent; one of the member's own by the text and
// primary they picked, through its variables over Light or Dark.
function Swatch({ theme, own }) {
    return (
        <span data-theme={own ? (own.dark ? 'dark' : 'light') : theme} style={own ? variables(own) : undefined} aria-hidden="true"
            class="grid shrink-0 grid-cols-2 gap-0.5 rounded-md border border-base-content/10 bg-base-100 p-1">
            {own ? (
                <>
                    <span class="h-3.5 w-1.5 rounded-full bg-base-content" />
                    <span class="h-3.5 w-1.5 rounded-full bg-primary" />
                </>
            ) : (
                <>
                    <span class="size-1.5 rounded-full bg-base-content" />
                    <span class="size-1.5 rounded-full bg-primary" />
                    <span class="size-1.5 rounded-full bg-secondary" />
                    <span class="size-1.5 rounded-full bg-accent" />
                </>
            )}
        </span>
    );
}

function SystemSwatch() {
    return (
        <span aria-hidden="true" class="flex shrink-0 rounded-md border border-base-content/10 p-1">
            <svg viewBox="0 0 16 16" class="size-3.5">
                <circle cx="8" cy="8" r="6.5" fill="none" stroke="currentColor" stroke-width="1.5" />
                <path d="M8 1.5a6.5 6.5 0 0 1 0 13z" fill="currentColor" />
            </svg>
        </span>
    );
}
