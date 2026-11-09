// The app's settings: a dialog over whatever the page shows, so a den and
// a call stay in view, its sections listed down the side as Manage's are,
// or on the phone layout a list of them, each a page. Appearance holds the
// theme, the member's own among them, the picture behind the chat, how
// messages show and the verified mark; Chat compressing photos
// and videos and what happens to links; Notifications what notifies the
// member and the page's sounds; Voice the microphone and speaker, noise
// suppression, how the member sends their voice, their shortcuts and their
// screen share; and System hosting a den, the cache, updates, backups, the
// local password, the logs, signing this browser out, and the version.

import { useEffect, useRef, useState } from 'preact/hooks';
import { ALWAYS, ON_HOVER, setAnimations, useAnimations } from './animations.js';
import { api } from './api.js';
import { sendSmaller, setSendSmaller } from './compare.jsx';
import { DIM_MOST, DIM_STEP, KINDS, backgroundURL, checkBackground, removeBackground, setBackground, setDim, slideDim } from './background.js';
import {
    Card, CopyButton, DOCS, DocsTip, ErrorText, Field, PLAIN, PasswordFields, SubmitButton, TextInput, Tip, checkPasswords, useAction, useBackdrop,
    useBackground, useNotifications,
} from './components.jsx';
import { formatSize } from './files.jsx';
import { rememberNote } from './flatpak.js';
import { hostingAsk, hostingNote, hostingOnWarning, moving } from './hosting.js';
import { oldReddit, setOldReddit, setYouTubePlayers, youTubePlayers } from './links.js';
import { PlayIcon } from './icons.jsx';
import { useLayer, useNarrow } from './nav.js';
import { askToNotify, notifying, permission, tell, whereTheyShow } from './notify.js';
import { NO_CALLS, canCall, canShare, hasKeys, here, inApp } from './platform.js';
import { HEIGHTS, RATES, shareAudio } from './share.js';
import { GROUPS, SOUNDS, checkSound, playSound, soundSettings, uploadSound } from './sounds.js';
import { cacheSizes, cacheUsage } from './storage.js';
import { PLAIN_CARDS, PageHeader, SectionList, useSectionPage } from './pages.jsx';
import { THEMES, forgetTheme, keepTheme, ownChoice, ownThemes, previewTheme, setTheme, shownTheme, themeChoice, themeName } from './theme.js';
import { CORNER_NAMES, MOST_THEMES, NAME_MOST, PICKED, cleanName, hexColor, readCode, themeCode, variables } from './themes.js';
import { useCoarse, useHoldsButton } from './touch.js';
import { DENSITY_COMPACT, DENSITY_DEFAULT, setDensity, useDensity } from './density.js';
import { WIDTH_FULL, WIDTH_NARROW, WIDTH_WIDE, setWidth, useWidth } from './width.js';
import { MARKS_ALWAYS, MARKS_HOVER, MARKS_NEVER, setVerifiedMarks, useVerifiedMarks } from './verified.js';
import {
    RELEASE_MOST, RELEASE_STEP, callLimits, callVolume, cancelCapture, captureKey, chooseMic, chooseSpeaker, desktopKeys, devices,
    inCall as callNow, keyLabel, keysElsewhere, micProblem, noiseSuppression, onCall, onKeys, onVoiceLevel, setCallVolume, setKeys, setRNNoise,
    setShareSettings, setVoice, shareSettings, startMicTest, stopMicTest, voiceSettings,
} from './voice.js';

const openers = new Set();

// openSettings opens the settings at a section, "appearance", "chat",
// "notifications", "voice" or "system", from anywhere on the page; with
// none, at the first, or on the phone layout at the list of them.
export function openSettings(section = '') {
    openers.forEach((fn) => fn(section));
}

export function onOpenSettings(fn) {
    openers.add(fn);
    return () => openers.delete(fn);
}

// SECTIONS are the settings' sections, by the id openSettings takes and
// the name the list shows.
export const SECTIONS = [
    ['appearance', 'Appearance'],
    ['chat', 'Chat'],
    ['notifications', 'Notifications'],
    ['voice', 'Voice'],
    ['system', 'System'],
];

// SettingsDialog shows a section of the settings. It closes with its ✕,
// Escape, a click outside it, or Back. On the phone layout it takes the
// screen, its sections a list of pages, and Back and Escape go from a page
// to the list.
export function SettingsDialog({ section, onSection, onClose, instance, version, pair }) {
    useLayer(onClose);
    const narrow = useNarrow();
    useSectionPage(narrow && !!section, () => onSection(''));
    const at = section || (narrow ? '' : 'appearance');
    useEffect(() => {
        const escape = (e) => e.key === 'Escape' && (narrow && section ? onSection('') : onClose());
        document.addEventListener('keydown', escape);
        return () => document.removeEventListener('keydown', escape);
    }, [onClose, narrow, section]);
    let panel;
    if (at === 'chat') panel = <ChatSettings />;
    else if (at === 'notifications') panel = <><NotificationSettings /><SoundSettings /></>;
    else if (at === 'voice') panel = <VoiceSettings />;
    else if (at === 'system') panel = <SystemSettings instance={instance} version={version} pair={pair} />;
    else panel = <><ThemePicker /><ChatBackground /><MessagesPicker /><AnimationsPicker /><VerifiedMarksPicker /></>;
    const backdrop = useBackdrop(onClose);
    if (narrow) {
        return (
            <div role="dialog" aria-modal="true" aria-label="App settings" class="fixed inset-0 z-50 flex flex-col bg-base-100">
                {at ? (
                    <>
                        <PageHeader title={SECTIONS.find(([id]) => id === at)?.[1]} onBack={() => onSection('')} />
                        <div data-panel class={`flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4 pb-[max(1rem,env(safe-area-inset-bottom))] ${PLAIN_CARDS}`}>{panel}</div>
                    </>
                ) : (
                    <>
                        <PageHeader title="App settings" onClose={onClose} />
                        <SectionList label="Settings" groups={[['', SECTIONS]]} onPick={onSection} />
                    </>
                )}
            </div>
        );
    }
    return (
        <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" {...backdrop}>
            <div role="dialog" aria-modal="true" aria-label="App settings" class="card flex h-full max-h-[48rem] w-full max-w-3xl flex-col bg-base-100 shadow-xl">
                <div class="flex items-center gap-2 border-b border-base-300 px-4 py-3">
                    <h2 class="card-title min-w-0 flex-1 truncate">App settings</h2>
                    <button type="button" class="btn btn-ghost btn-sm" onClick={onClose} aria-label="Close">✕</button>
                </div>
                <div class="flex min-h-0 flex-1 flex-row">
                    <nav aria-label="Sections" class="flex w-52 shrink-0 flex-col gap-1 border-r border-base-300 p-2">
                        {SECTIONS.map(([id, name]) => (
                            <button key={id} type="button" aria-current={at === id ? 'page' : undefined}
                                class={`btn btn-ghost btn-sm shrink-0 justify-start font-normal ${at === id ? 'btn-active' : ''}`} onClick={() => onSection(id)}>
                                {name}
                            </button>
                        ))}
                    </nav>
                    <div data-panel class="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-y-auto p-4">{panel}</div>
                </div>
            </div>
        </div>
    );
}

// Toggle is a setting that's on or off: its label, a line on what it
// does, and a tip for the rest.
function Toggle({ id, label, hint, tip, checked, disabled, onChange }) {
    return (
        <div class="flex items-start gap-3">
            <input id={id} type="checkbox" class="toggle mt-0.5 shrink-0" checked={checked} disabled={disabled}
                onChange={(e) => onChange(e.currentTarget.checked)} />
            <div class="min-w-0">
                <label for={id} class={disabled ? '' : 'cursor-pointer'}>{label}</label>
                {tip && <> <Tip id={`${id}-tip`}>{tip}</Tip></>}
                {hint && <p class="text-sm text-base-content/70">{hint}</p>}
            </div>
        </div>
    );
}

// ThemePicker chooses the page's theme: the device's light or dark, Light
// or Dark, one of the others, or one of the member's own, each shown in
// its colors. It takes hold at once, and this browser keeps it. Under
// them the member makes a theme, edits or deletes the one chosen, copies
// its code for another member, and pastes one.
function ThemePicker() {
    const [choice, setChoice] = useState(themeChoice);
    const [own, setOwn] = useState(ownThemes);
    const other = THEMES.includes(choice) && !['light', 'dark'].includes(choice);
    const [more, setMore] = useState(other);
    // editing is the theme in the editor while it's open, as it started,
    // with its id, '' for a new one; pasting, the field for a code.
    const [editing, setEditing] = useState(null);
    const [pasting, setPasting] = useState(false);
    const [code, setCode] = useState('');
    const [error, setError] = useState('');
    const chosen = own.find((t) => ownChoice(t.id) === choice);
    const full = own.length >= MOST_THEMES;
    function pick(theme) {
        setTheme(theme);
        setChoice(theme);
    }
    function reread() {
        setOwn(ownThemes());
        setChoice(themeChoice());
        setEditing(null);
        setPasting(false);
        setError('');
    }
    // kept takes a theme the member made or pasted, and chooses it.
    function kept(theme, id) {
        const now = keepTheme(theme, id);
        if (!now) return setError("That theme couldn't be kept.");
        setTheme(ownChoice(now));
        reread();
    }
    function paste(e) {
        e.preventDefault();
        const theme = readCode(code);
        if (!theme) return setError("That isn't a theme's code.");
        setCode('');
        kept(theme);
    }
    const ring = 'has-[:checked]:ring-2 has-[:checked]:ring-primary has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-primary';
    const tile = `flex cursor-pointer items-center gap-2 rounded-field px-2 py-1.5 text-sm hover:bg-base-300 has-[:checked]:bg-base-100 has-[:checked]:font-medium max-md:has-[:checked]:bg-base-200 ${ring}`;
    return (
        <Card title="Theme">
            <div role="radiogroup" aria-label="Theme" class="grid grid-cols-3 gap-2">
                {[['system', 'System'], ['light', 'Light'], ['dark', 'Dark']].map(([theme, name]) => (
                    <label key={theme} class={`flex cursor-pointer flex-col items-center gap-2 rounded-box bg-base-100 px-2 py-3 hover:bg-base-300 max-md:bg-base-200 ${ring}`}>
                        <input type="radio" name="theme" class="sr-only" value={theme} checked={choice === theme} onChange={() => pick(theme)} />
                        {theme === 'system' ? <SystemSwatch large /> : <Swatch theme={theme} large />}
                        <span class="text-sm font-medium">{name}</span>
                    </label>
                ))}
            </div>
            <div>
                <button type="button" class="btn btn-ghost btn-sm" aria-expanded={more} onClick={() => setMore(!more)}>
                    {other ? `More themes: ${themeName(choice)}` : 'More themes'}
                    <svg viewBox="0 0 16 16" class={`h-3 w-3 transition-transform ${more ? 'rotate-180' : ''}`} fill="none" stroke="currentColor"
                        stroke-width="2" aria-hidden="true">
                        <path d="M4 6l4 4 4-4" />
                    </svg>
                </button>
            </div>
            {more && (
                <div role="radiogroup" aria-label="More themes" class="grid grid-cols-[repeat(auto-fill,minmax(9.5rem,1fr))] gap-1">
                    {THEMES.filter((t) => t !== 'light' && t !== 'dark').map((theme) => (
                        <label key={theme} class={tile}>
                            <input type="radio" name="theme" class="sr-only" value={theme} checked={choice === theme} onChange={() => pick(theme)} />
                            <Swatch theme={theme} />
                            <span class="truncate">{themeName(theme)}</span>
                        </label>
                    ))}
                </div>
            )}
            <div class="flex flex-col gap-3" data-own-themes>
                <h3 class="text-sm font-semibold">Your themes</h3>
                {own.length > 0 && (
                    <div role="radiogroup" aria-label="Your themes" class="grid grid-cols-[repeat(auto-fill,minmax(9.5rem,1fr))] gap-1">
                        {own.map((theme) => (
                            <label key={theme.id} class={tile}>
                                <input type="radio" name="theme" class="sr-only" value={ownChoice(theme.id)} checked={choice === ownChoice(theme.id)}
                                    onChange={() => pick(ownChoice(theme.id))} />
                                <Swatch own={theme} />
                                <span class="truncate">{theme.name}</span>
                            </label>
                        ))}
                    </div>
                )}
                {editing ? (
                    <ThemeEditor start={editing.start} onSave={(theme) => kept(theme, editing.id)} onCancel={reread} />
                ) : pasting ? (
                    <form class="flex flex-wrap gap-2" onSubmit={paste}>
                        <input class="input input-sm min-w-40 flex-1 font-mono" aria-label="A theme's code" placeholder="dens-theme:…" value={code}
                            onInput={(e) => setCode(e.currentTarget.value)} required autofocus {...PLAIN} />
                        <button type="submit" class="btn btn-primary btn-sm">Add</button>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={reread}>Cancel</button>
                    </form>
                ) : (
                    <>
                        {chosen && (
                            <div class="flex flex-wrap gap-2">
                                <button type="button" class="btn btn-sm" onClick={() => setEditing({ id: chosen.id, start: chosen })}>Edit</button>
                                <CopyButton text={themeCode(chosen)} label="Copy code" />
                                <button type="button" class="btn btn-ghost btn-sm" onClick={() => {
                                    forgetTheme(chosen.id);
                                    reread();
                                }}>
                                    Delete
                                </button>
                            </div>
                        )}
                        <div class="flex flex-wrap gap-2">
                            <button type="button" class="btn btn-sm" disabled={full} onClick={() => setEditing({ id: '', start: shownTheme('My theme') })}>
                                New theme
                            </button>
                            <button type="button" class="btn btn-sm" disabled={full} onClick={() => setPasting(true)}>Paste a code</button>
                        </div>
                    </>
                )}
                {full && !editing && <p class="text-sm text-base-content/70">You have {MOST_THEMES} themes. Delete one to add another.</p>}
                <ErrorText message={error} />
            </div>
        </Card>
    );
}

// COLOR_NAMES are what the editor calls the colors a member picks.
const COLOR_NAMES = { base: 'Background', text: 'Text', primary: 'Primary', secondary: 'Secondary', accent: 'Accent' };
const CORNER_LABELS = { square: 'Square', rounded: 'Rounded', round: 'Round' };

// ThemeEditor makes or changes a theme of the member's own: its name,
// whether it stands on Light or Dark, its five colors and its corners. The
// page wears the theme as it's made, and takes the chosen one back when
// the editor goes, saved or not.
function ThemeEditor({ start, onSave, onCancel }) {
    const [draft, setDraft] = useState(start);
    const [error, setError] = useState('');
    const form = useRef(null);
    useEffect(() => {
        previewTheme(draft);
    }, [draft]);
    // It opens under the card's themes, so it's brought into view.
    useEffect(() => {
        form.current?.scrollIntoView?.({ block: 'nearest' });
        return () => previewTheme(null);
    }, []);
    const change = (changes) => setDraft((d) => ({ ...d, ...changes }));
    function save(e) {
        e.preventDefault();
        const name = cleanName(draft.name);
        if (!name) return setError(`A theme's name is up to ${NAME_MOST} characters.`);
        onSave({ ...draft, name });
    }
    return (
        <form ref={form} class="flex flex-col gap-3 rounded-box bg-base-100 p-3 max-md:bg-base-200" data-theme-editor onSubmit={save}>
            <label class="flex flex-col gap-1">
                <span class="text-sm">Name</span>
                <TextInput value={draft.name} onInput={(name) => change({ name })} maxlength={NAME_MOST * 2} required />
            </label>
            <Choices label="Based on" name="theme-base" data="data-theme-base" choice={draft.dark ? 'dark' : 'light'} onPick={(v) => change({ dark: v === 'dark' })}
                choices={[['light', 'Light'], ['dark', 'Dark']]} />
            <div role="group" aria-label="Colors" class="flex flex-col gap-2">
                {PICKED.map((color) => (
                    <ColorField key={color} label={COLOR_NAMES[color]} value={draft[color]} onChange={(hex) => change({ [color]: hex })} />
                ))}
            </div>
            <Choices label="Corners" name="theme-corners" data="data-theme-corners" choice={draft.corners} onPick={(corners) => change({ corners })}
                choices={CORNER_NAMES.map((c) => [c, CORNER_LABELS[c]])} />
            <div class="flex gap-2">
                <button type="submit" class="btn btn-primary btn-sm">Save</button>
                <button type="button" class="btn btn-ghost btn-sm" onClick={onCancel}>Cancel</button>
            </div>
            <ErrorText message={error} />
        </form>
    );
}

// ColorField takes a color: the browser's picker, and beside it the
// color's hex, the same in every browser, which takes a typed or pasted
// one once it reads as a color.
function ColorField({ label, value, onChange }) {
    const [typed, setTyped] = useState(value);
    useEffect(() => {
        setTyped((t) => (hexColor(t) === value ? t : value));
    }, [value]);
    return (
        <div class="flex items-center gap-3" data-color={label}>
            <span class="min-w-0 flex-1 text-sm">{label}</span>
            <input type="color" class="h-8 w-12 shrink-0 cursor-pointer rounded-field border border-base-content/20 bg-transparent p-0.5"
                aria-label={`${label}, picked`} value={value} onInput={(e) => onChange(e.currentTarget.value.toLowerCase())} />
            <input type="text" class="input input-sm w-24 shrink-0 font-mono" aria-label={`${label}, as hex`} value={typed} maxlength="7" {...PLAIN}
                onInput={(e) => {
                    const text = e.currentTarget.value;
                    setTyped(text);
                    const hex = hexColor(text);
                    if (hex && hex !== value) onChange(hex);
                }}
                onBlur={() => setTyped(value)} />
        </div>
    );
}

// ChatBackground sets the picture behind the member's messages: Choose
// image, for a PNG, a JPEG or a WebP of theirs, Dim, how far the theme's
// background covers it, and Remove. The service keeps both, so every
// browser paired with it shows them.
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
        <Card title="Chat background">
            <p class="text-sm text-base-content/70">A picture behind your messages. Only you see it.</p>
            {background && (
                <div class="relative h-24 w-40 overflow-hidden rounded-box border border-base-content/10" data-background-preview aria-hidden="true">
                    <img src={backgroundURL(background.version)} alt="" class="size-full object-cover" />
                    <div class="absolute inset-0 bg-base-100" style={{ opacity: background.dim / 100 }}></div>
                </div>
            )}
            <div class="flex flex-wrap gap-2">
                <button type="button" class="btn btn-sm" disabled={act.busy} onClick={() => file.current?.click()}>
                    {act.busy && <span class="loading loading-spinner loading-sm"></span>}
                    {background ? 'Change image' : 'Choose image'}
                </button>
                {background && <button type="button" class="btn btn-ghost btn-sm" disabled={act.busy} onClick={() => act.run(removeBackground)}>Remove</button>}
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
                        <input type="range" class="range range-md min-w-0 flex-1" min="0" max={DIM_MOST} step={DIM_STEP} value={background.dim}
                            aria-label="Dim" aria-valuetext={`${background.dim}%`}
                            onInput={(e) => slideDim(Number(e.currentTarget.value))}
                            onChange={(e) => setDim(Number(e.currentTarget.value)).catch((err) => act.setError(err.message))} />
                        <span class="w-10 text-right text-sm tabular-nums">{background.dim}%</span>
                    </span>
                </label>
            )}
            <ErrorText message={act.error} />
        </Card>
    );
}

// MessagesPicker chooses how messages show: their Density, Default or
// Compact, a line each of the time, the name and the text; and their
// Width, Full, Wide or Narrow, how far across the pane they run. This
// browser keeps both, as it keeps the theme.
function MessagesPicker() {
    const density = useDensity();
    const width = useWidth();
    return (
        <Card title="Messages">
            <Choices label="Density" name="density" data="data-density" choice={density} onPick={setDensity}
                choices={[[DENSITY_DEFAULT, 'Default'], [DENSITY_COMPACT, 'Compact']]} />
            <Choices label="Width" name="width" data="data-width" choice={width} onPick={setWidth}
                choices={[[WIDTH_FULL, 'Full'], [WIDTH_WIDE, 'Wide'], [WIDTH_NARROW, 'Narrow']]} />
        </Card>
    );
}

// Choices is a setting of a few, in a row under its label.
function Choices({ label, name, data, choice, choices, onPick }) {
    return (
        <div class="flex flex-col gap-1">
            <span class="text-sm">{label}</span>
            <div role="radiogroup" aria-label={label} class="flex flex-wrap gap-x-6 gap-y-2" {...{ [data]: true }}>
                {choices.map(([value, text]) => (
                    <label key={value} class="flex min-h-6 cursor-pointer items-center gap-2">
                        <input type="radio" class="radio radio-sm" name={name} value={value} checked={choice === value} onChange={() => onPick(value)} />
                        <span>{text}</span>
                    </label>
                ))}
            </div>
        </div>
    );
}

// AnimationsPicker chooses when animated pictures and the den's emoji
// play: always, or while the pointer is over their row, which the system's
// asking for less motion makes the default. This browser keeps it, as it
// keeps the theme.
function AnimationsPicker() {
    const choice = useAnimations();
    return (
        <Card title="Animations">
            <div role="radiogroup" aria-label="Animations" class="flex flex-col gap-3">
                {[
                    [ALWAYS, 'Always', 'Animated pictures and emoji play wherever they show.', ''],
                    [ON_HOVER, 'On hover', 'They play while the pointer is over their message or member.', 'And in a profile you open. On a touch screen, only in profiles.'],
                ].map(([value, label, hint, tip]) => (
                    <div key={value} class="flex flex-col gap-0.5">
                        <div class="flex items-center gap-1">
                            <label class="flex min-h-6 cursor-pointer items-center gap-3">
                                <input type="radio" class="radio radio-sm" name="animations" value={value} checked={choice === value}
                                    onChange={() => setAnimations(value)} />
                                <span>{label}</span>
                            </label>
                            {tip && <Tip id={`animations-${value}-tip`}>{tip}</Tip>}
                        </div>
                        {/* Under the label: the radio is 20 pixels, and its gap 12. */}
                        <p class="pl-8 text-sm text-base-content/70">{hint}</p>
                    </div>
                ))}
            </div>
        </Card>
    );
}

// VerifiedMarksPicker chooses whether the check beside a member this
// member compared codes with shows beside their name: always, on hover or
// never, and on a touch screen, which has no hover, shown or not. This
// browser keeps it, as it keeps the theme.
function VerifiedMarksPicker() {
    const touch = useCoarse();
    const choice = useVerifiedMarks();
    const choices = touch ? [[MARKS_ALWAYS, 'Yes'], [MARKS_NEVER, 'No']] : [[MARKS_ALWAYS, 'Always'], [MARKS_HOVER, 'On hover'], [MARKS_NEVER, 'No']];
    return (
        <Card title="Verified mark">
            <p class="text-sm text-base-content/70">Beside people you've compared codes with.</p>
            <div role="radiogroup" aria-label="Verified mark" class="flex flex-wrap gap-x-6 gap-y-2" data-verified-marks>
                {choices.map(([value, label]) => (
                    <label key={value} class="flex min-h-6 cursor-pointer items-center gap-2">
                        <input type="radio" class="radio radio-sm" name="verified-marks" value={value} checked={choice === value}
                            onChange={() => setVerifiedMarks(value)} />
                        <span>{label}</span>
                    </label>
                ))}
            </div>
        </Card>
    );
}

// Swatch shows a theme's colors: its text, primary, secondary and accent on
// its background; one of the member's own by its variables, over Light or
// Dark.
function Swatch({ theme, own, large }) {
    const dot = `${large ? 'size-3' : 'size-1.5'} rounded-full`;
    return (
        <span data-theme={own ? (own.dark ? 'dark' : 'light') : theme} style={own ? variables(own) : undefined} aria-hidden="true"
            class={`grid shrink-0 grid-cols-2 rounded-md border border-base-content/10 bg-base-100 ${large ? 'gap-1 p-2' : 'gap-0.5 p-1'}`}>
            <span class={`${dot} bg-base-content`} />
            <span class={`${dot} bg-primary`} />
            <span class={`${dot} bg-secondary`} />
            <span class={`${dot} bg-accent`} />
        </span>
    );
}

function SystemSwatch({ large }) {
    return (
        <span aria-hidden="true" class={`flex shrink-0 rounded-md border border-base-content/10 ${large ? 'p-2' : 'p-1'}`}>
            <svg viewBox="0 0 16 16" class={large ? 'size-7' : 'size-3.5'}>
                <circle cx="8" cy="8" r="6.5" fill="none" stroke="currentColor" stroke-width="1.5" />
                <path d="M8 1.5a6.5 6.5 0 0 1 0 13z" fill="currentColor" />
            </svg>
        </span>
    );
}

// ChatSettings holds whether this browser sends photos and videos
// compressed, and what happens to links.
function ChatSettings() {
    const [compress, setCompress] = useState(sendSmaller);
    const [old, setOld] = useState(oldReddit);
    const [players, setPlayers] = useState(youTubePlayers);
    return (
        <>
            <Card title="Photos and videos">
                <Toggle id="compress" label="Compress photos and videos" checked={compress}
                    hint="Sends faster and saves space. You can still send the original."
                    tip="A photo goes at most 2,560 pixels across, and a video at most 720p, made on this device before it's sent, which takes about as long as the video lasts. A file larger than a den allows is compressed either way."
                    onChange={(on) => {
                        setSendSmaller(on);
                        setCompress(on);
                    }} />
            </Card>
            <Card title="Links">
                <p class="text-sm text-base-content/70">
                    Dens removes tracking parameters and shortens links.{' '}
                    <Tip id="links-tip">
                        Common tracking parameters come off every link, and Reddit, YouTube, X and Amazon links shorten to the post, video or
                        product they point to.
                    </Tip>
                </p>
                <Toggle id="old-reddit" label="Open Reddit links on old.reddit.com" checked={old} onChange={(on) => {
                    setOldReddit(on);
                    setOld(on);
                }} />
                <Toggle id="youtube" label="Play YouTube videos in Dens" checked={players}
                    hint="YouTube will see your IP address when previews load." onChange={(on) => {
                        setYouTubePlayers(on);
                        setPlayers(on);
                    }} />
            </Card>
        </>
    );
}

// NotificationSettings turns each kind of notification on or off, and the
// message's start in them (M12.3). The service keeps them, so every browser
// paired with it shares them; turning a kind on asks this browser to let
// the page notify, where it hasn't asked, and the card says where they
// show, by what this browser, the desktop's helper and Android allow.
function NotificationSettings() {
    const status = useNotifications();
    const [browser, setBrowser] = useState(permission);
    const [elsewhere, setElsewhere] = useState(keysElsewhere);
    const act = useAction();
    // What came between the first render and this effect counts too.
    useEffect(() => {
        const update = () => setElsewhere(keysElsewhere());
        update();
        return onKeys(update);
    }, []);
    function change(kind, on) {
        // The browser asks only in answer to the click, for a kind of its
        // own: @everyone is among mentions (M15.4).
        const asked = on && kind !== 'hideText' && kind !== 'everyone' ? askToNotify() : null;
        act.run(async () => {
            if (asked) {
                setBrowser(await asked);
                tell();
            }
            await api.post('/api/settings', { notifications: { [kind]: on } });
        });
    }
    if (!status) return null;
    return (
        <Card title="Notifications">
            <Toggle id="notify-dms" label="Direct messages" checked={!!status.dms} disabled={act.busy} onChange={(on) => change('dms', on)} />
            <Toggle id="notify-mentions" label="Mentions" hint="Messages in channels that mention your username."
                checked={!!status.mentions} disabled={act.busy} onChange={(on) => change('mentions', on)} />
            <div class="pl-6">
                <Toggle id="notify-everyone" label="@everyone" hint="Staff's @everyone, in channels you can see."
                    tip="Off, it notifies nothing and plays no sound, but still counts among your mentions."
                    checked={!status.everyoneOff} disabled={act.busy || !status.mentions} onChange={(on) => change('everyone', on)} />
            </div>
            <Toggle id="notify-events" label="Events" hint="Events you're interested in, as they start."
                checked={!status.eventsOff} disabled={act.busy} onChange={(on) => change('events', on)} />
            <Toggle id="notify-text" label="Show the message" hint="The start of each message, in its notification."
                checked={!status.hideText} disabled={act.busy || !notifying(status)} onChange={(on) => change('hideText', !on)} />
            {notifying(status) && whereTheyShow(status, browser, elsewhere).map((line) => <p key={line} class="text-sm text-base-content/70">{line}</p>)}
            <p class="text-sm text-base-content/70">Mute a den or a channel from the bell beside its name.</p>
            <ErrorText message={act.error} />
        </Card>
    );
}

// SoundSettings holds the page's sounds (M14.2), grouped Messages and
// Calls: each with its switch, ▶ to hear it, its volume, and Replace…, for
// a file of the member's own, or once replaced, Reset, which brings its
// default back. The service keeps them, so every browser paired with it
// shares them, as it does the notifications'. A file must play here, for
// at most 10 seconds, which the page checks before sending it.
function SoundSettings() {
    const status = useNotifications();
    const act = useAction();
    // sliding holds a slider's volume while it moves, until the service
    // has it.
    const [sliding, setSliding] = useState({});
    useEffect(() => {
        setSliding((m) => {
            const next = Object.fromEntries(Object.entries(m).filter(([name, v]) => soundSettings(status, name).volume !== v));
            return Object.keys(next).length === Object.keys(m).length ? m : next;
        });
    }, [status]);
    if (!status) return null;
    const keep = (name, changes) => api.post('/api/settings', { sounds: { [name]: changes } }).catch((e) => act.setError(e.message));
    function replace(name, label, file) {
        act.run(async () => {
            const why = await checkSound(file);
            if (why) throw new Error(`${label}: ${why}`);
            await uploadSound(name, file);
        });
    }
    const message = (
        <>
            Plays once for a DM or a mention, unless read or muted.{' '}
            <DocsTip id="docs-sounds" page="usage/#notifications">
                {status.shell
                    ? "Where a notification shows instead, Android plays its own sound, set in Android's settings for Dens."
                    : 'Where your desktop shows a notification instead, its own sound plays.'}
            </DocsTip>
        </>
    );
    return (
        <Card title="Sounds">
            {GROUPS.map(([group, names]) => (
                <div key={group} role="group" aria-label={group} class="flex flex-col gap-3">
                    <h3 class="text-sm font-semibold">{group}</h3>
                    {names.map((name) => {
                        const label = SOUNDS.find(([n]) => n === name)[1];
                        const s = soundSettings(status, name);
                        return (
                            <SoundRow key={name} name={name} label={label} s={s} volume={sliding[name] ?? s.volume} busy={act.busy}
                                onOn={(on) => keep(name, { off: !on })}
                                onMove={(volume) => setSliding((m) => ({ ...m, [name]: volume }))}
                                onVolume={(volume) => {
                                    keep(name, { volume });
                                    playSound(name, { preview: true, volume });
                                }}
                                onReplace={(file) => replace(name, label, file)}
                                onReset={() => act.run(() => api.del(`/api/sounds/${name}`))} />
                        );
                    })}
                    <p class="text-sm text-base-content/70">{group === 'Messages' ? message : "Deafened, you hear only your microphone's."}</p>
                </div>
            ))}
            <ErrorText message={act.error} />
        </Card>
    );
}

// SoundRow is one of the page's sounds in the settings: its switch, its
// name, ▶, Replace… or Reset, and under them its volume.
function SoundRow({ name, label, s, volume, busy, onOn, onMove, onVolume, onReplace, onReset }) {
    const file = useRef(null);
    const id = `sound-${name}`;
    return (
        <div class="flex flex-col gap-1" data-sound={name}>
            <div class="flex items-center gap-3">
                <input id={id} type="checkbox" class="toggle shrink-0" checked={s.on} onChange={(e) => onOn(e.currentTarget.checked)} />
                <label for={id} class="min-w-0 flex-1 cursor-pointer">
                    {label}
                    {s.own > 0 && <span class="text-sm text-base-content/60"> · your file</span>}
                </label>
                <button type="button" class="btn btn-ghost btn-sm btn-square" aria-label={`Play ${label}`} title={`Play ${label}`}
                    onClick={() => playSound(name, { preview: true, volume })}>
                    <PlayIcon />
                </button>
                {s.own > 0 ? (
                    <button type="button" class="btn btn-ghost btn-sm min-w-24" aria-label={`Reset ${label} to Dens's sound`} disabled={busy} onClick={onReset}>
                        Reset
                    </button>
                ) : (
                    <button type="button" class="btn btn-ghost btn-sm min-w-24" aria-label={`Replace ${label} with a file of yours`} disabled={busy}
                        onClick={() => file.current?.click()}>
                        Replace…
                    </button>
                )}
                <input ref={file} type="file" accept="audio/*" class="hidden" onChange={(e) => {
                    const picked = e.currentTarget.files?.[0];
                    e.currentTarget.value = '';
                    if (picked) onReplace(picked);
                }} />
            </div>
            {/* Under the name: the switch is 40 pixels, and its gap 12. */}
            <div class="flex items-center gap-3 pl-13">
                <input type="range" class="range range-md min-w-0 flex-1" min="0" max="100" step="5" value={volume} disabled={!s.on}
                    aria-label={`${label} volume`} aria-valuetext={`${volume}%`}
                    onInput={(e) => onMove(Number(e.currentTarget.value))} onChange={(e) => onVolume(Number(e.currentTarget.value))} />
                <span class="w-10 text-right text-sm tabular-nums">{volume}%</span>
            </div>
        </div>
    );
}

// VoiceSettings chooses the microphone and speaker, how loud calls play
// (M12), the noise suppression, how the member sends their voice, their
// shortcuts, and how they share their screen. Its meter shows what the
// voice processor hears and whether it sends it: in the call while there
// is one, and otherwise in a test the member starts. What the page can't
// use leaves it, as on a phone (M13.3): the shortcuts where the service
// says there are no keys, and sharing where the browser can't share. Where
// the page has the call's button, its role takes the place of how the
// member sends (M13.4).
function VoiceSettings() {
    const [v, setV] = useState(voiceSettings);
    const holds = useHoldsButton();
    const [loud, setLoud] = useState(callVolume);
    const [suppression, setSuppression] = useState(noiseSuppression);
    const [list, setList] = useState(null);
    const [error, setError] = useState('');
    const [report, setReport] = useState(null);
    const [inCall, setInCall] = useState(callNow);
    const [testing, setTesting] = useState(false);
    const [capturing, setCapturing] = useState('');
    const captures = useRef(0);
    const [elsewhere, setElsewhere] = useState(keysElsewhere);
    useEffect(() => onCall((c) => setInCall(!!c.call)), []);
    useEffect(() => onKeys(() => {
        setV(voiceSettings());
        setElsewhere(keysElsewhere());
    }), []);
    useEffect(() => {
        devices().then(setList, () => setError("Dens couldn't list the microphones and speakers."));
        const off = onVoiceLevel(setReport);
        return () => {
            off();
            stopMicTest();
            cancelCapture();
        };
    }, []);
    function change(changes) {
        setVoice(changes);
        setV(voiceSettings());
    }
    function pickMic(id) {
        setList({ ...list, mic: id });
        setError('');
        chooseMic(id).catch((e) => setError(micProblem(e)));
    }
    function pickSpeaker(id) {
        setList({ ...list, speaker: id });
        chooseSpeaker(id);
    }
    function pickLoudness(volume) {
        setCallVolume(volume);
        setLoud(callVolume());
    }
    function pickSuppression(on) {
        setSuppression({ rnnoise: on, failed: false, processorFailed: false });
        setError('');
        setRNNoise(on).then(() => setSuppression(noiseSuppression()), (e) => setError(micProblem(e)));
    }
    async function test() {
        setError('');
        if (testing) {
            stopMicTest();
            setTesting(false);
            setReport(null);
            return;
        }
        setTesting(true);
        try {
            await startMicTest();
        } catch (e) {
            setTesting(false);
            setError(micProblem(e));
        }
    }
    async function saveKeys(keys) {
        setError('');
        try {
            await setKeys(keys);
            setV(voiceSettings());
        } catch (e) {
            setError(e.message || "The shortcuts weren't saved.");
        }
    }
    async function desktop(what) {
        setError('');
        try {
            await desktopKeys(what);
        } catch (e) {
            setError(e.message || "Your desktop wasn't asked.");
        }
    }
    // bind waits for a key for an action. Starting again cancels the wait
    // before, whose end mustn't clear the one that replaced it.
    async function bind(name) {
        const run = ++captures.current;
        setCapturing(name);
        const key = await captureKey();
        if (run !== captures.current) return;
        setCapturing('');
        if (key) await saveKeys({ ...v.keys, [name]: key });
    }
    const suppressing = suppression.rnnoise && !suppression.failed && !suppression.processorFailed;
    const auto = v.mode === 'voice' && v.auto && suppressing;
    const desktopHolds = (name) => elsewhere.confirms && elsewhere.covers.includes(name);
    let talk = 'Set a push-to-talk shortcut below.';
    if (desktopHolds('ptt')) talk = 'Hold the push-to-talk shortcut your desktop runs to talk.';
    else if (v.keys.ptt) talk = `Hold ${keyLabel(v.keys.ptt)} to talk.`;
    return (
        <>
            {!canCall() && <p role="status" class="text-sm text-warning">{NO_CALLS}</p>}
            <Card title="Microphone and speaker">
                {!list ? (
                    <span class="loading loading-dots loading-sm"></span>
                ) : (
                    <div class="grid gap-x-3 sm:grid-cols-2">
                        <Field label="Microphone">
                            <select class="select w-full" value={list.mic} onChange={(e) => pickMic(e.currentTarget.value)}>
                                <option value="">Default</option>
                                {list.inputs.map((d) => <option key={d.id} value={d.id}>{d.label}</option>)}
                            </select>
                        </Field>
                        {list.outputs && (
                            <Field label="Speaker">
                                <select class="select w-full" value={list.speaker} onChange={(e) => pickSpeaker(e.currentTarget.value)}>
                                    <option value="">Default</option>
                                    {list.outputs.map((d) => <option key={d.id} value={d.id}>{d.label}</option>)}
                                </select>
                            </Field>
                        )}
                    </div>
                )}
                <Field label="Call volume" hint="Everyone in the call, before their own volumes.">
                    <div class="flex items-center gap-3">
                        <input type="range" class="range range-md min-w-0 flex-1" min="0" max="100" step="5" value={Math.round(loud * 100)}
                            aria-label="Call volume" aria-valuetext={`${Math.round(loud * 100)}%`}
                            onInput={(e) => pickLoudness(Number(e.currentTarget.value) / 100)} />
                        <span class="w-10 text-right text-sm tabular-nums">{Math.round(loud * 100)}%</span>
                    </div>
                </Field>
                <Toggle id="noise" label="Noise suppression" checked={suppression.rnnoise} onChange={pickSuppression}
                    hint="Removes keyboard, fan and background noise."
                    tip="It adds 30 ms of delay and uses a little of this device's processor. Off, the built-in suppression takes out only steady noise, such as a fan's." />
                {suppression.processorFailed ? (
                    <p class="text-sm text-warning">Voice processing can't run here; voice activity sends everything.</p>
                ) : suppression.failed && <p class="text-sm text-warning">Noise suppression couldn't start; the built-in one is on.</p>}
            </Card>
            <Card title={holds ? "The call's button" : 'Sending your voice'}>
                {holds ? (
                    <div role="radiogroup" aria-label="The call's button" class="flex flex-wrap gap-4">
                        {ROLES.map(([role, title]) => (
                            <label key={role} class="label gap-2">
                                <input type="radio" class="radio radio-sm" name="button" value={role} checked={v.button === role}
                                    onChange={() => change({ button: role })} />
                                {title}
                            </label>
                        ))}
                    </div>
                ) : (
                    <div role="radiogroup" aria-label="Sending your voice" class="flex flex-wrap gap-4">
                        <label class="label gap-2">
                            <input type="radio" class="radio radio-sm" name="sending" checked={v.mode === 'voice'} onChange={() => change({ mode: 'voice' })} />
                            Voice activity
                        </label>
                        <label class="label gap-2">
                            <input type="radio" class="radio radio-sm" name="sending" checked={v.mode === 'ptt'} onChange={() => change({ mode: 'ptt' })} />
                            Push to talk
                        </label>
                    </div>
                )}
                {v.mode === 'voice' ? (
                    <div class="flex flex-col gap-3 rounded-box bg-base-100 p-3 max-md:bg-base-200">
                        <Toggle id="auto-sensitivity" label="Automatic sensitivity" checked={auto} disabled={!suppressing}
                            hint={suppressing ? "Detects when you're speaking." : 'Needs noise suppression.'}
                            onChange={(on) => change({ auto: on })} />
                        {!auto && (
                            <label class="flex flex-col gap-1">
                                <span class="text-sm">Sensitivity</span>
                                <input type="range" class="range range-md w-full" min="-100" max="0" step="1" value={v.threshold}
                                    aria-label="Sensitivity" aria-valuetext={`${v.threshold} dB`} onInput={(e) => change({ threshold: Number(e.currentTarget.value) })} />
                            </label>
                        )}
                    </div>
                ) : (
                    <div class="flex flex-col gap-3 rounded-box bg-base-100 p-3 max-md:bg-base-200">
                        {!holds && <p class="text-sm text-base-content/70">{talk}</p>}
                        <label class="flex flex-col gap-1">
                            <span class="text-sm">Release delay</span>
                            <span class="flex items-center gap-3">
                                <input type="range" class="range range-md min-w-0 flex-1" min="0" max={RELEASE_MOST} step={RELEASE_STEP}
                                    value={v.releaseDelay} aria-label="Release delay" aria-valuetext={`${v.releaseDelay} ms`}
                                    onInput={(e) => change({ releaseDelay: Number(e.currentTarget.value) })} />
                                <span class="w-16 text-right text-sm tabular-nums">{v.releaseDelay} ms</span>
                            </span>
                        </label>
                    </div>
                )}
                <div class="flex items-center gap-3">
                    <div class="min-w-0 flex-1">
                        <Meter report={report} threshold={v.mode === 'voice' && !auto ? v.threshold : null} />
                    </div>
                    {!inCall && (
                        <button type="button" class="btn btn-sm shrink-0" onClick={test}>{testing ? 'Stop test' : 'Test microphone'}</button>
                    )}
                </div>
            </Card>
            {hasKeys() && <Card title="Keyboard shortcuts">
                {KEYS.map(([name, title]) => (
                    <div key={name} class="flex flex-wrap items-center gap-2">
                        <span class="min-w-32 flex-1">{title}</span>
                        {desktopHolds(name) ? (
                            <span class="flex items-center gap-1.5">
                                <kbd class="kbd kbd-sm">{elsewhere.names[name]}</kbd>
                                <WorksElsewhere id={`elsewhere-${name}`} />
                            </span>
                        ) : (
                            <span class="flex items-center gap-1.5">
                                {elsewhere.covers.includes(name) && v.keys[name] && <WorksElsewhere id={`elsewhere-${name}`} />}
                                <button type="button" class="btn btn-sm min-w-28 font-normal" disabled={!!capturing && capturing !== name}
                                    aria-label={`${title}: ${v.keys[name] ? keyLabel(v.keys[name]) : 'not set'}. Record a shortcut`}
                                    onClick={() => bind(name)}>
                                    {capturing === name ? (
                                        'Press a key…'
                                    ) : v.keys[name] ? (
                                        <kbd class="kbd kbd-sm">{keyLabel(v.keys[name])}</kbd>
                                    ) : (
                                        <span class="text-base-content/60">Not set</span>
                                    )}
                                </button>
                                {v.keys[name] && (
                                    <button type="button" class="btn btn-ghost btn-sm" disabled={!!capturing}
                                        onClick={() => saveKeys({ ...v.keys, [name]: undefined })}>
                                        Clear
                                    </button>
                                )}
                            </span>
                        )}
                    </div>
                ))}
                {capturing && <p class="text-sm text-base-content/70">Press a key, or a mouse's side button. Escape cancels.</p>}
                <KeysNote elsewhere={elsewhere} onDesktop={desktop} />
            </Card>}
            {canShare() && <ShareSettings />}
            {error && <ErrorText message={error} />}
        </>
    );
}

const KEYS = [
    ['ptt', 'Push to talk'],
    ['toggleMute', 'Toggle mute'],
    ['pushMute', 'Push to mute'],
    ['toggleDeafen', 'Toggle deafen'],
];

// ROLES are what the call's button may do (M13.4), its default first.
const ROLES = [
    ['toggleMute', 'Toggle mute'],
    ['pushMute', 'Push to mute'],
    ['ptt', 'Push to talk'],
];

// WorksElsewhere marks a shortcut the helper reads in other apps during a
// call, and says so on hover or focus.
function WorksElsewhere({ id }) {
    return (
        <span class="tooltip">
            <span id={id} role="tooltip" class="tooltip-content rounded-lg px-3 py-2 text-left">
                Works in other apps too, during a call.
            </span>
            <span tabIndex={0} role="img" aria-label="Works in other apps" aria-describedby={id} class="flex cursor-help text-success">
                <svg viewBox="0 0 16 16" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
                    stroke-linejoin="round" aria-hidden="true">
                    <circle cx="8" cy="8" r="6.5" />
                    <path d="M5.2 8.3l1.9 1.9 3.8-4.1" />
                </svg>
            </span>
        </span>
    );
}

// KeysNote says in a line where the shortcuts work: in other apps while
// the helper reads them, or the desktop runs them, and otherwise while
// Dens has focus. Where the desktop holds them (M7.2), the member sets
// them up with it, changes them in its settings, or stops.
function KeysNote({ elsewhere, onDesktop }) {
    const text = 'text-sm text-base-content/70';
    if (elsewhere.helper && elsewhere.confirms) {
        if (elsewhere.covers.length > 0) {
            return (
                <div class="flex flex-col gap-2">
                    <p class={text}>Your desktop runs these in every app; Dens acts on them in a call.</p>
                    {elsewhere.note && <p class={text}>{elsewhere.note}</p>}
                    <div class="flex flex-wrap gap-2">
                        {elsewhere.configurable && <button type="button" class="btn btn-sm" onClick={() => onDesktop('configure')}>Change them</button>}
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => onDesktop('stop')}>Stop using them</button>
                    </div>
                </div>
            );
        }
        if (elsewhere.binding) return <p class={text} role="status">Confirm the shortcuts in your desktop's dialog.</p>;
        // The desktop binds once a session, and this one has none of them
        // now, as when they were taken out in its settings (M11).
        if (!elsewhere.bindable) {
            return (
                <div class="flex flex-col gap-2">
                    <p class={text}>None are set on your desktop, so they work while Dens is focused.</p>
                    {elsewhere.note && <p class={text}>{elsewhere.note}</p>}
                    <div class="flex flex-wrap gap-2">
                        {elsewhere.configurable && <button type="button" class="btn btn-sm" onClick={() => onDesktop('configure')}>Change them</button>}
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => onDesktop('stop')}>Start over</button>
                    </div>
                </div>
            );
        }
        return (
            <div class="flex flex-col gap-2">
                <p class={text}>Work while Dens is focused. Set them up to work in every app.</p>
                <div>
                    <button type="button" class="btn btn-sm" onClick={() => onDesktop('bind')}>
                        Set up with your desktop
                    </button>
                </div>
            </div>
        );
    }
    if (elsewhere.helper && elsewhere.covers.length > 0) {
        return (
            <>
                <p class={text}>Work in every app during a call, a full-screen game included.</p>
                {elsewhere.note && <p class={text}>{elsewhere.note}</p>}
            </>
        );
    }
    return (
        <p class={text}>
            {elsewhere.helper
                ? "Work while Dens is focused; the helper can't read them here."
                : 'Work while Dens is focused, or in every app with the helper.'}
        </p>
    );
}

// Meter shows the voice processor's level, green while it sends, against
// the sensitivity when there is one.
function Meter({ report, threshold }) {
    const at = (db) => `${Math.max(0, Math.min(100, db + 100))}%`;
    return (
        <div class="relative h-2 w-full overflow-hidden rounded bg-base-300" aria-hidden="true">
            <div class={`h-full ${report?.open ? 'bg-success' : 'bg-base-content/40'}`} style={{ width: report ? at(report.level) : '0%' }}></div>
            {threshold !== null && <div class="absolute inset-y-0 w-0.5 bg-warning" style={{ left: at(threshold) }}></div>}
        </div>
    );
}

// ShareSettings chooses how the member shares their screen (M4.2): its
// resolution and frame rate, whether it keeps detail or motion, and
// whether it takes its audio along, which only some browsers can. A den's
// lower limits show while a call in that den holds them to them.
function ShareSettings() {
    const [s, setS] = useState(shareSettings);
    const [limits, setLimits] = useState(callLimits);
    useEffect(() => onCall(() => setLimits(callLimits())), []);
    const { browser, system } = here();
    const audio = shareAudio(browser, system);
    function change(changes) {
        setShareSettings(changes);
        setS(shareSettings());
    }
    const lower = limits && (limits.share_height < s.height || limits.share_fps < s.fps);
    const resolution = (h) => (h === 2160 ? '4K (2160p)' : `${h}p`);
    return (
        <Card title="Screen share">
            <div class="grid gap-x-3 sm:grid-cols-2">
                <Field label="Resolution">
                    <select class="select w-full" value={s.height} onChange={(e) => change({ height: Number(e.currentTarget.value) })}>
                        {HEIGHTS.map((h) => <option key={h} value={h}>{resolution(h)}</option>)}
                    </select>
                </Field>
                <Field label="Frame rate">
                    <select class="select w-full" value={s.fps} onChange={(e) => change({ fps: Number(e.currentTarget.value) })}>
                        {RATES.map((r) => <option key={r} value={r}>{r} fps</option>)}
                    </select>
                </Field>
            </div>
            {lower && (
                <p class="text-sm text-base-content/70">
                    This call's den allows up to {resolution(limits.share_height)} at {limits.share_fps} fps.
                </p>
            )}
            <div role="radiogroup" aria-label="Optimize for" class="flex flex-wrap items-center gap-4">
                <span>Optimize for</span>
                <label class="label gap-2">
                    <input type="radio" class="radio radio-sm" name="hint" checked={s.hint === 'detail'} onChange={() => change({ hint: 'detail' })} />
                    Clarity
                </label>
                <label class="label gap-2">
                    <input type="radio" class="radio radio-sm" name="hint" checked={s.hint === 'motion'} onChange={() => change({ hint: 'motion' })} />
                    Smoothness
                </label>
            </div>
            <Toggle id="share-audio" label="Share audio" hint={audio.text} checked={audio.supported && s.sound} disabled={!audio.supported}
                onChange={(on) => change({ sound: on })} />
        </Card>
    );
}

// SystemSettings holds hosting, the cache, update checks, the local
// password, the logs, signing this browser out, and the version.
function SystemSettings({ instance, version, pair }) {
    const [settings, setSettings] = useState(null);
    const load = useAction();
    const save = useAction();

    useEffect(() => {
        load.run(async () => setSettings(await api.get('/api/settings')));
    }, []);

    function change(key, value) {
        save.run(async () => {
            await api.post('/api/settings', { [key]: value });
            setSettings((s) => ({ ...s, [key]: value }));
        });
    }

    if (!settings) {
        return load.error ? <ErrorText message={load.error} /> : <span class="loading loading-spinner"></span>;
    }
    return (
        <>
            {settings.remember && <OnThisComputer settings={settings} onChange={setSettings} />}
            <Hosting />
            <Cache settings={settings} onChange={setSettings} />
            {settings.updatesManaged && (
                <Card title="Updates">
                    <Toggle id="update-checks" label="Check for updates once a day" checked={settings.backgroundUpdateChecks}
                        onChange={(on) => change('backgroundUpdateChecks', on)} />
                    <Toggle id="update-notices" label="Show a notice when an update is available" checked={settings.updateNotifications}
                        onChange={(on) => change('updateNotifications', on)} />
                    <p class="text-sm text-base-content/70">
                        {settings.updatesDownload ? "Each check shows the release host this phone's IP address." : "Each check shows the release host this computer's IP address."}
                        {settings.updatesDownload && (
                            <>
                                {' '}
                                <Tip id="updates-tip">Dens downloads a newer version, checks that Dens's release process signed it, and Android asks you before installing it.</Tip>
                            </>
                        )}
                    </p>
                    {settings.updatesDownload && <CheckNow />}
                </Card>
            )}
            {settings.updatesThroughFlatpak && (
                <Card title="Updates">
                    <Toggle id="update-notices" label="Show a notice when an update is available" checked={settings.updateNotifications}
                        onChange={(on) => change('updateNotifications', on)} />
                    <p class="text-sm text-base-content/70">Flatpak updates Dens with your other apps.</p>
                </Card>
            )}
            <BackUp />
            <LocalPassword />
            <Card title={<>Logs <DocsTip id="docs-logs" page="usage/#sending-logs" side="right">Which level to set, and what the logs hold.</DocsTip></>}>
                <Field label="Log level">
                    <select class="select" value={settings.logLevel} onChange={(e) => change('logLevel', e.currentTarget.value)}>
                        {settings.logLevels.map((level) => <option key={level} value={level}>{level}</option>)}
                    </select>
                </Field>
                {/* The service keeps the logs where an installed Dens's
                    desktop user can't read them, so the page downloads
                    them. */}
                <p class="text-sm text-base-content/70">What Dens did and when, with no messages, keys or addresses.</p>
                <div>
                    <a class="btn btn-outline btn-sm" href="/api/logs" download>Download the newest logs</a>
                </div>
            </Card>
            <ErrorText message={save.error} />
            <Card title={inApp() ? 'This page' : 'This browser'}>
                <p class="text-sm text-base-content/70">{inApp() ? 'Signing out unpairs this page.' : 'Signing out unpairs this browser.'}</p>
                <p class="text-sm text-base-content/70">
                    {inApp() ? 'Dens pairs it again as you open it from your apps.'
                        : pair
                            ? <>To pair it again, run <code class="rounded bg-base-300 px-1">{pair}</code>.</>
                            : 'To pair it again, open Dens from your apps menu.'}
                </p>
                <div>
                    <button type="button" class="btn btn-outline btn-sm" onClick={() => save.run(async () => {
                        await api.post('/api/logout');
                        window.location.replace('/');
                    })}>
                        Sign out
                    </button>
                </div>
            </Card>
            <Card title="About">
                <p class="text-sm">
                    Dens {version}
                    {instance !== 'main' && `, instance ${instance}`}
                </p>
                <p class="text-sm">
                    Guides to installing, joining and hosting:{' '}
                    <a class="link link-primary" href={DOCS} target="_blank" rel="noopener noreferrer">dens.chat/docs</a>
                </p>
                <p class="text-sm text-base-content/70">
                    Includes other projects' software.{' '}
                    <a class="link link-primary" href="/licenses" target="_blank" rel="noopener noreferrer">Their licenses</a>
                </p>
            </Card>
        </>
    );
}

// CheckNow checks for an update at once, in the Android app (M17.2),
// whatever the daily checks are set to: the service asks the release host,
// and downloads and checks a newer version before it answers, which Install
// then hands to Android's installer.
function CheckNow() {
    const check = useAction();
    const install = useAction();
    const [found, setFound] = useState(null);
    const run = () => check.run(async () => {
        setFound(null);
        setFound(await api.post('/api/update/check'));
    });
    return (
        <div class="flex flex-col gap-2" data-check-now>
            <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
                <button type="button" class="btn btn-outline btn-sm" disabled={check.busy} onClick={run}>
                    {check.busy && <span class="loading loading-spinner loading-sm"></span>}
                    {check.busy ? 'Checking…' : 'Check now'}
                </button>
                {found && !found.update && <span class="text-sm">Dens is up to date.</span>}
                {found?.update && (
                    <>
                        {/* One string, which a screen reader reads as one. */}
                        <span class="text-sm">{`Dens ${found.update} is ready to install.`}</span>
                        <button type="button" class="btn btn-primary btn-sm" disabled={install.busy}
                            onClick={() => install.run(() => api.post('/api/update'))}>
                            {install.busy ? 'Opening the installer…' : 'Install'}
                        </button>
                    </>
                )}
            </div>
            <ErrorText message={check.error || install.error} />
        </div>
    );
}

// OnThisComputer is what only the Flatpak has (M9.3): remembering its key,
// so it starts unlocked, which turning on takes the local password for,
// and which the keyring may take a while to answer; starting at sign-in
// once it remembers; and quitting, which no service manager does for it.
function OnThisComputer({ settings, onChange }) {
    const [asking, setAsking] = useState(false);
    const [password, setPassword] = useState('');
    const [quit, setQuit] = useState(false);
    const act = useAction();
    const remember = settings.remember;
    const reload = async () => onChange(await api.get('/api/settings'));
    useEffect(() => {
        if (!remember.asking) return undefined;
        const timer = setInterval(() => reload().catch(() => {}), 1500);
        return () => clearInterval(timer);
    }, [remember.asking]);

    function change(on) {
        act.setError('');
        setPassword('');
        if (on) return setAsking(true);
        act.run(async () => {
            await api.post('/api/remember', { on: false });
            await reload();
        });
    }
    function turnOn(e) {
        e.preventDefault();
        act.run(async () => {
            await api.post('/api/remember', { on: true, password });
            setAsking(false);
            await reload();
        });
    }

    if (quit) {
        return (
            <Card title="Dens has quit">
                <p class="text-sm">Open it again from your apps menu.</p>
            </Card>
        );
    }
    const note = rememberNote(remember);
    return (
        <Card title="On this computer">
            <Toggle id="remember" label="Remember on this computer" checked={asking || remember.on || remember.asking}
                disabled={act.busy || remember.asking} onChange={change}
                hint="Opens without your local password, and at sign-in." tip="Programs you run on this computer could open Dens too." />
            {asking ? (
                <form class="flex flex-col gap-2" onSubmit={turnOn}>
                    <Field label="Local password" hint="Your Dens password on this device, not a den password.">
                        <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required autofocus />
                    </Field>
                    <div class="flex gap-2">
                        <SubmitButton busy={act.busy}>Remember</SubmitButton>
                        <button type="button" class="btn btn-ghost" disabled={act.busy} onClick={() => setAsking(false)}>Cancel</button>
                    </div>
                </form>
            ) : (
                <>
                    <p class="text-sm text-base-content/70" role={remember.asking ? 'status' : undefined}>{note.text}</p>
                    {note.problem && <p class="text-sm text-warning">{note.problem}</p>}
                </>
            )}
            {remember.on && !asking && (
                <>
                    <Toggle id="start-at-sign-in" label="Start Dens when you sign in" checked={settings.startAtSignIn.on} disabled={act.busy}
                        onChange={(on) => act.run(async () => {
                            const startAtSignIn = await api.post('/api/start-at-sign-in', { on });
                            onChange({ ...settings, startAtSignIn });
                        })} />
                    {settings.startAtSignIn.manual && (
                        <>
                            <p class="text-sm text-base-content/70">Your desktop can't start Dens itself.</p>
                            <p class="text-sm text-base-content/70">
                                Have it run this when you sign in: <code class="rounded bg-base-300 px-1">{settings.startAtSignIn.manual}</code>
                            </p>
                        </>
                    )}
                </>
            )}
            <div>
                <button type="button" class="btn btn-outline btn-sm" disabled={act.busy} onClick={() => act.run(async () => {
                    await api.post('/api/quit');
                    setQuit(true);
                })}>
                    Quit Dens
                </button>
            </div>
            <ErrorText message={act.error} />
        </Card>
    );
}

// BackUp downloads a backup, as dens backup writes it, after the local
// password, which protects it (M9.3), saying first about how large it will
// be (M10).
function BackUp() {
    const [asking, setAsking] = useState(false);
    const [password, setPassword] = useState('');
    const [done, setDone] = useState(false);
    const [size, setSize] = useState(null);
    const act = useAction();

    function ask() {
        setDone(false);
        setSize(null);
        setAsking(true);
        api.get('/api/backup').then((r) => setSize(r.size), () => {});
    }

    function submit(e) {
        e.preventDefault();
        act.run(async () => {
            const { url } = await api.post('/api/backup', { password });
            download(url);
            setPassword('');
            setAsking(false);
            setDone(true);
        });
    }

    return (
        <Card title={<>Backup <DocsTip id="docs-backups" page="backups/" side="right">What a backup holds, and restoring one here or on another computer.</DocsTip></>}>
            <p class="text-sm text-base-content/70">Your dens, keys, settings and a hosted den. Not the cache.</p>
            {asking ? (
                <form class="flex flex-col gap-2" onSubmit={submit}>
                    <Field label="Local password" hint="Your Dens password on this device, not a den password.">
                        <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required autofocus />
                    </Field>
                    <div class="flex flex-wrap items-center gap-2">
                        <SubmitButton busy={act.busy}>Download the backup</SubmitButton>
                        <button type="button" class="btn btn-ghost" disabled={act.busy} onClick={() => setAsking(false)}>Cancel</button>
                        {size != null && <span class="text-sm text-base-content/70">About {formatSize(size)}</span>}
                    </div>
                </form>
            ) : (
                <div class="flex flex-wrap items-center gap-3">
                    <button type="button" class="btn btn-sm" onClick={ask}>
                        Back up
                    </button>
                    {done && <span class="text-sm text-success" role="status">Your browser is downloading the backup.</span>}
                </div>
            )}
            <ErrorText message={act.error} />
        </Card>
    );
}

// Hosting turns hosting a den on this computer on and off (M9.1). Turning
// it on opens ports to the internet, so it takes the local password, and
// for a den exported since, says first that another computer may host it
// by now (M10); turning it off asks first, since members can't reach the
// den until it's back on. While it's on, it says where Caddy and the
// router send the den. Once there's a den, it moves it to another
// computer, with its export, and, once that computer serves it, removes
// it from this one (M10).
function Hosting() {
    const [hosting, setHosting] = useState(null);
    // asking is what waits on the member: 'on' for the password, 'off' for
    // the confirmation, and 'move' and 'remove' for the password the
    // export and removing the den take.
    const [asking, setAsking] = useState('');
    const [password, setPassword] = useState('');
    const [size, setSize] = useState(null);
    const [removed, setRemoved] = useState(null);
    const act = useAction();
    useEffect(() => {
        act.run(async () => setHosting(await api.get('/api/hosting')));
    }, []);

    function begin(what) {
        act.setError('');
        setPassword('');
        setRemoved(null);
        setAsking(what);
    }
    function ask(on) {
        begin(hostingAsk(hosting.enabled, on));
    }
    function turnOn(e) {
        e.preventDefault();
        act.run(async () => {
            setHosting(await api.post('/api/hosting', { on: true, password }));
            setPassword('');
            setAsking('');
        });
    }
    function turnOff() {
        act.run(async () => {
            setHosting(await api.post('/api/hosting', { on: false }));
            setAsking('');
        });
    }
    function askMove() {
        begin('move');
        setSize(null);
        api.get('/api/backup').then((r) => setSize(r.size), () => {});
    }
    function move(e) {
        e.preventDefault();
        act.run(async () => {
            const { url } = await api.post('/api/den/export', { password });
            download(url);
            setPassword('');
            setAsking('');
            // The export is recorded as its download starts.
            for (let i = 0; i < 20; i++) {
                const h = await api.get('/api/hosting');
                setHosting(h);
                if (h.exported) break;
                await new Promise((resolve) => setTimeout(resolve, 500));
            }
        });
    }
    function remove(e) {
        e.preventDefault();
        const den = { name: hosting.den, url: hosting.url };
        act.run(async () => {
            setHosting(await api.post('/api/den/remove', { password }));
            setPassword('');
            setAsking('');
            setRemoved(den);
        });
    }

    const title = (
        <>
            Hosting
            <DocsTip id="docs-hosting" page="hosting/" side="right">Everything a den needs, step by step: a domain, Caddy and your router.</DocsTip>
        </>
    );
    if (!hosting) {
        return <Card title={title}>{act.error ? <ErrorText message={act.error} /> : <span class="loading loading-dots loading-sm"></span>}</Card>;
    }
    if (hostingNote(hosting)?.cantHost) {
        return (
            <Card title={title}>
                <p class="text-sm text-base-content/70">{hostingNote(hosting).cantHost}</p>
            </Card>
        );
    }
    const switching = asking === 'on' || asking === 'off';
    const on = switching ? asking === 'on' : hosting.enabled;
    const note = switching ? null : hostingNote(hosting);
    const moves = switching ? null : moving(hosting);
    const warning = hostingOnWarning(hosting);
    const passwordField = (
        <Field label="Local password" hint="Your Dens password on this device, not a den password.">
            <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required autofocus />
        </Field>
    );
    const cancel = <button type="button" class="btn btn-ghost" disabled={act.busy} onClick={() => begin('')}>Cancel</button>;
    return (
        <Card title={title}>
            <Toggle id="hosting" label="Host a den on this computer" checked={on} disabled={act.busy} onChange={ask}
                hint="Needs this computer on, a domain, and Caddy in front." />
            {asking === 'on' && (
                <form class="flex flex-col gap-2" onSubmit={turnOn}>
                    {warning && <p class="text-sm text-warning">{warning}</p>}
                    <p class="text-sm text-base-content/70">Opening ports to the internet takes your local password.</p>
                    {passwordField}
                    <div class="flex gap-2">
                        <SubmitButton busy={act.busy}>Turn hosting on</SubmitButton>
                        <button type="button" class="btn btn-ghost" disabled={act.busy} onClick={() => ask(hosting.enabled)}>Cancel</button>
                    </div>
                </form>
            )}
            {asking === 'off' && (
                <div class="flex flex-col gap-2">
                    <p class="text-sm">Members can't reach your den until hosting is back on.</p>
                    <p class="text-sm text-base-content/70">Everything in it stays.</p>
                    <div class="flex gap-2">
                        <button type="button" class="btn btn-error" disabled={act.busy} onClick={turnOff}>
                            {act.busy && <span class="loading loading-spinner loading-sm"></span>}
                            Turn hosting off
                        </button>
                        <button type="button" class="btn btn-ghost" disabled={act.busy} onClick={() => ask(hosting.enabled)}>Cancel</button>
                    </div>
                </div>
            )}
            {note?.problem && <p role="status" class="text-sm text-warning">{note.problem}</p>}
            {note?.address && (
                <>
                    <p class="text-sm text-base-content/70">
                        Caddy forwards your den's address to <code class="rounded bg-base-300 px-1">{note.address}</code>.
                    </p>
                    <p class="text-sm text-base-content/70">Your router forwards UDP {note.udp} and TCP {note.tcp} here, for calls.</p>
                </>
            )}
            {moves && (
                <div class="flex flex-col gap-2 border-t border-base-300 pt-3">
                    {moves.exported && (
                        <>
                            <p class="text-sm">
                                You exported {moves.den} on {moves.exported}.{' '}
                                <DocsTip id="docs-server" page="hosting/#on-a-server">Restoring the den on a server, hosting it there, and removing it from this computer.</DocsTip>
                            </p>
                            {asking !== 'remove' && <p class="text-sm text-base-content/70">Once the other computer hosts it, remove it from this one.</p>}
                        </>
                    )}
                    {asking === 'move' && (
                        <form class="flex flex-col gap-2" onSubmit={move}>
                            {hosting.enabled && <p class="text-sm">This turns hosting off.</p>}
                            <p class="text-sm">Members can't reach {moves.den} until the other computer hosts it.</p>
                            <p class="text-sm text-base-content/70">The file holds the den alone, none of your own data.</p>
                            {passwordField}
                            <div class="flex flex-wrap items-center gap-2">
                                <SubmitButton busy={act.busy}>Download the den's export</SubmitButton>
                                {cancel}
                                {size != null && <span class="text-sm text-base-content/70">About {formatSize(size)}</span>}
                            </div>
                        </form>
                    )}
                    {asking === 'remove' && (
                        <form class="flex flex-col gap-2" onSubmit={remove}>
                            <p class="text-sm">Deletes this computer's copy of {moves.den}, its messages and files.</p>
                            <p class="text-sm text-base-content/70">Dens first checks that another computer serves it at {moves.url}.</p>
                            <p class="text-sm text-base-content/70">Your account stays; Dens here reaches the den at its address.</p>
                            {passwordField}
                            <div class="flex gap-2">
                                <SubmitButton busy={act.busy} class="btn-error">Remove from this computer</SubmitButton>
                                {cancel}
                            </div>
                        </form>
                    )}
                    {!asking && (
                        <div class="flex flex-wrap gap-2">
                            {moves.exported && <button type="button" class="btn btn-sm" onClick={() => begin('remove')}>Remove from this computer</button>}
                            <button type="button" class="btn btn-sm" onClick={askMove}>Move the den to another computer</button>
                        </div>
                    )}
                </div>
            )}
            {removed && (
                <p role="status" class="text-sm text-success">
                    Removed {removed.name} from this computer. Dens reaches it at {removed.url} now.
                </p>
            )}
            <ErrorText message={act.error} />
        </Card>
    );
}

// download has the browser download what url serves, as it does any
// download.
function download(url) {
    const link = document.createElement('a');
    link.href = url;
    link.download = '';
    document.body.append(link);
    link.click();
    link.remove();
}

// Cache shows how much of other dens' files this computer keeps, which
// open without downloading again and while their den is offline, up to a
// size the member chooses, and empties it (M6).
function Cache({ settings, onChange }) {
    const save = useAction();
    const reload = async () => onChange(await api.get('/api/settings'));
    return (
        <Card title="Cache">
            <p class="text-sm text-base-content/70">Other dens' files and messages, kept for offline reading.</p>
            <p class="text-sm">{cacheUsage(settings.cacheUsed, settings.cacheLimit)}</p>
            <Field label="Keep up to">
                <select class="select" value={settings.cacheLimit} disabled={save.busy} onChange={(e) => {
                    const limit = Number(e.currentTarget.value);
                    save.run(async () => {
                        await api.post('/api/settings', { cacheLimit: limit });
                        await reload();
                    });
                }}>
                    {cacheSizes(settings.cacheLimit).map((n) => <option key={n} value={n}>{formatSize(n)}</option>)}
                </select>
            </Field>
            <div>
                <button type="button" class="btn btn-outline btn-sm" disabled={save.busy || !settings.cacheUsed} onClick={() => save.run(async () => {
                    await api.post('/api/cache/clear');
                    await reload();
                })}>
                    Clear cache
                </button>
            </div>
            <ErrorText message={save.error} />
        </Card>
    );
}

// LocalPassword changes the local password, behind a button, since it's
// rarely changed.
function LocalPassword() {
    const [open, setOpen] = useState(false);
    const [current, setCurrent] = useState('');
    const [next, setNext] = useState('');
    const [confirm, setConfirm] = useState('');
    const [done, setDone] = useState(false);
    const { busy, error, setError, run } = useAction();

    function close() {
        setOpen(false);
        setCurrent('');
        setNext('');
        setConfirm('');
        setError('');
    }

    function submit(e) {
        e.preventDefault();
        const problem = checkPasswords(next, confirm);
        if (problem) return setError(problem);
        run(async () => {
            await api.post('/api/password/change', { current, next });
            close();
            setDone(true);
        });
    }

    return (
        <Card title="Local password">
            <p class="text-sm text-base-content/70">Protects Dens on this device. Not a den password.</p>
            {open ? (
                <form class="flex flex-col gap-2" onSubmit={submit}>
                    <Field label="Current password">
                        <TextInput type="password" value={current} onInput={setCurrent} autocomplete="current-password" required />
                    </Field>
                    <PasswordFields password={next} setPassword={setNext} confirm={confirm} setConfirm={setConfirm} />
                    <ErrorText message={error} />
                    <div class="flex gap-2">
                        <SubmitButton busy={busy}>Change password</SubmitButton>
                        <button type="button" class="btn btn-ghost" onClick={close}>Cancel</button>
                    </div>
                </form>
            ) : (
                <div class="flex flex-wrap items-center gap-3">
                    <button type="button" class="btn btn-sm" onClick={() => {
                        setDone(false);
                        setOpen(true);
                    }}>
                        Change password
                    </button>
                    {done && <span class="text-sm text-success">Password changed.</span>}
                </div>
            )}
        </Card>
    );
}
