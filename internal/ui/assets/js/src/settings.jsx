// The app's settings: a dialog over whatever the page shows, so a den and
// a call stay in view (M3.3), in four tabs (M8.3). Appearance holds the
// theme; Chat & media whether photos and videos go compressed (M5), what
// happens to links, and whether YouTube videos play in Dens (M4.1); Voice &
// video the microphone and speaker, noise suppression, how the member
// sends their voice, their shortcuts (M7) and how they share their screen
// (M4.2); System the cache of other dens' files (M6), update checks, the
// local password, logging, signing this browser out, and the version.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { sendSmaller, setSendSmaller } from './compare.jsx';
import { Card, ErrorText, Field, PasswordFields, SubmitButton, TextInput, Tip, checkPasswords, useAction } from './components.jsx';
import { formatSize } from './files.jsx';
import { oldReddit, setOldReddit, setYouTubePlayers, youTubePlayers } from './links.js';
import { here } from './platform.js';
import { HEIGHTS, RATES, shareAudio } from './share.js';
import { cacheSizes, cacheUsage } from './storage.js';
import { THEMES, setTheme, themeChoice, themeName } from './theme.js';
import {
    callLimits, cancelCapture, captureKey, chooseMic, chooseSpeaker, desktopKeys, devices, inCall as callNow, keyLabel, keysElsewhere,
    micProblem, noiseSuppression, onCall, onKeys, onVoiceLevel, setKeys, setRNNoise, setShareSettings, setVoice, shareSettings, startMicTest,
    stopMicTest, voiceSettings,
} from './voice.js';

const openers = new Set();

// openSettings opens the settings at a tab, "appearance", "chat", "voice"
// or "system", from anywhere on the page.
export function openSettings(section = 'appearance') {
    openers.forEach((fn) => fn(section));
}

export function onOpenSettings(fn) {
    openers.add(fn);
    return () => openers.delete(fn);
}

const SECTIONS = [
    ['appearance', 'Appearance'],
    ['chat', 'Chat & media'],
    ['voice', 'Voice & video'],
    ['system', 'System'],
];

// SettingsDialog shows a tab of the settings. It closes with its ✕,
// Escape or a click outside it.
export function SettingsDialog({ section, onSection, onClose, instance, version }) {
    useEffect(() => {
        const escape = (e) => e.key === 'Escape' && onClose();
        document.addEventListener('keydown', escape);
        return () => document.removeEventListener('keydown', escape);
    }, [onClose]);
    let panel;
    if (section === 'chat') panel = <ChatSettings />;
    else if (section === 'voice') panel = <VoiceSettings />;
    else if (section === 'system') panel = <SystemSettings instance={instance} version={version} />;
    else panel = <ThemePicker />;
    return (
        <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={(e) => e.target === e.currentTarget && onClose()}>
            <div role="dialog" aria-modal="true" aria-label="App settings" class="card flex h-full max-h-[48rem] w-full max-w-2xl flex-col bg-base-100 shadow-xl">
                <div class="flex items-center gap-2 px-4 pt-3">
                    <h2 class="card-title flex-1">App settings</h2>
                    <button type="button" class="btn btn-ghost btn-sm" onClick={onClose} aria-label="Close">✕</button>
                </div>
                <div role="tablist" class="tabs tabs-border flex-nowrap overflow-x-auto px-2">
                    {SECTIONS.map(([id, name]) => (
                        <button key={id} type="button" role="tab" aria-selected={section === id}
                            class={`tab whitespace-nowrap ${section === id ? 'tab-active' : ''}`} onClick={() => onSection(id)}>
                            {name}
                        </button>
                    ))}
                </div>
                <div role="tabpanel" class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">{panel}</div>
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
// or Dark, or one of the others, each shown in its colors. It takes hold
// at once, and this browser keeps it.
function ThemePicker() {
    const [choice, setChoice] = useState(themeChoice);
    const other = !['system', 'light', 'dark'].includes(choice);
    const [more, setMore] = useState(other);
    function pick(theme) {
        setTheme(theme);
        setChoice(theme);
    }
    const ring = 'has-[:checked]:ring-2 has-[:checked]:ring-primary has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-primary';
    return (
        <Card title="Theme">
            <div role="radiogroup" aria-label="Theme" class="grid grid-cols-3 gap-2">
                {[['system', 'System'], ['light', 'Light'], ['dark', 'Dark']].map(([theme, name]) => (
                    <label key={theme} class={`flex cursor-pointer flex-col items-center gap-2 rounded-box bg-base-100 px-2 py-3 hover:bg-base-300 ${ring}`}>
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
                        <label key={theme}
                            class={`flex cursor-pointer items-center gap-2 rounded-field px-2 py-1.5 text-sm hover:bg-base-300 has-[:checked]:bg-base-100 has-[:checked]:font-medium ${ring}`}>
                            <input type="radio" name="theme" class="sr-only" value={theme} checked={choice === theme} onChange={() => pick(theme)} />
                            <Swatch theme={theme} />
                            <span class="truncate">{themeName(theme)}</span>
                        </label>
                    ))}
                </div>
            )}
        </Card>
    );
}

// Swatch shows a theme's colors: its text, primary, secondary and accent on
// its background.
function Swatch({ theme, large }) {
    const dot = `${large ? 'size-3' : 'size-1.5'} rounded-full`;
    return (
        <span data-theme={theme} aria-hidden="true"
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
                    hint="Sends faster and saves space. You can still choose the original before sending."
                    tip="A photo goes at most 2,560 pixels across, and a video at most 720p, made on this computer before it's sent, which takes about as long as the video lasts. A file larger than a den allows is compressed either way."
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

// VoiceSettings chooses the microphone and speaker, the noise suppression,
// how the member sends their voice, and their shortcuts. Its meter shows
// what the voice processor hears and whether it sends it: in the call
// while there is one, and otherwise in a test the member starts.
function VoiceSettings() {
    const [v, setV] = useState(voiceSettings);
    const [suppression, setSuppression] = useState(noiseSuppression);
    const [list, setList] = useState(null);
    const [error, setError] = useState('');
    const [report, setReport] = useState(null);
    const [inCall, setInCall] = useState(callNow);
    const [testing, setTesting] = useState(false);
    const [capturing, setCapturing] = useState('');
    const [elsewhere, setElsewhere] = useState(keysElsewhere);
    useEffect(() => onCall((c) => setInCall(!!c.call)), []);
    useEffect(() => onKeys(() => {
        setV(voiceSettings());
        setElsewhere(keysElsewhere());
    }), []);
    useEffect(() => {
        devices().then(setList, () => setError("This browser didn't list its devices."));
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
    async function bind(name) {
        setCapturing(name);
        const key = await captureKey();
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
                <Toggle id="noise" label="Noise suppression" checked={suppression.rnnoise} onChange={pickSuppression}
                    hint="Removes keyboard, fan and background noise."
                    tip="It adds 30 ms of delay and uses a little of this computer's processor. Off, the browser's own suppression takes out only steady noise, such as a fan's." />
                {suppression.processorFailed ? (
                    <p class="text-sm text-warning">
                        This browser couldn't run Dens's voice processing, so its own noise suppression is on, and voice activity sends whatever
                        it hears. Push to talk still works.
                    </p>
                ) : suppression.failed && <p class="text-sm text-warning">Noise suppression couldn't start in this browser, so its own is on.</p>}
            </Card>
            <Card title="Sending your voice">
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
                {v.mode === 'voice' ? (
                    <div class="flex flex-col gap-3 rounded-box bg-base-100 p-3">
                        <Toggle id="auto-sensitivity" label="Automatic sensitivity" checked={auto} disabled={!suppressing}
                            hint={suppressing ? "Detects when you're speaking." : 'Needs noise suppression.'}
                            onChange={(on) => change({ auto: on })} />
                        {!auto && (
                            <label class="flex flex-col gap-1">
                                <span class="text-sm">Sensitivity</span>
                                <input type="range" class="range range-xs w-full" min="-100" max="0" step="1" value={v.threshold}
                                    aria-label="Sensitivity" aria-valuetext={`${v.threshold} dB`} onInput={(e) => change({ threshold: Number(e.currentTarget.value) })} />
                            </label>
                        )}
                    </div>
                ) : (
                    <p class="text-sm text-base-content/70">{talk}</p>
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
            <Card title="Keyboard shortcuts">
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
            </Card>
            <ShareSettings />
            {error && <ErrorText message={error} />}
        </>
    );
}

const KEYS = [
    ['ptt', 'Push to talk'],
    ['toggleMute', 'Toggle mute'],
    ['pushMute', 'Push to mute'],
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
                    <p class={text}>Your desktop runs these in every app, and Dens acts on them during a call. {elsewhere.note}</p>
                    <div class="flex flex-wrap gap-2">
                        {elsewhere.configurable && <button type="button" class="btn btn-sm" onClick={() => onDesktop('configure')}>Change them</button>}
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => onDesktop('stop')}>Stop using them</button>
                    </div>
                </div>
            );
        }
        if (elsewhere.binding) return <p class={text} role="status">Confirm the shortcuts in your desktop's dialog.</p>;
        return (
            <div class="flex flex-col gap-2">
                <p class={text}>Work while Dens is focused. To use them in other apps, set them up with your desktop.</p>
                <div>
                    <button type="button" class="btn btn-sm" disabled={!elsewhere.bindable} onClick={() => onDesktop('bind')}>
                        Set up with your desktop
                    </button>
                </div>
            </div>
        );
    }
    if (elsewhere.helper && elsewhere.covers.length > 0) {
        return <p class={text}>Work in other apps too during a call, such as a game in full screen. {elsewhere.note}</p>;
    }
    return (
        <p class={text}>
            {elsewhere.helper
                ? "Work while Dens is focused. Dens's helper can't read them in other apps on this computer."
                : 'Work while Dens is focused. To use them in other apps, keep the Dens helper running.'}
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

// SystemSettings holds the cache, update checks, the local password,
// logging, signing this browser out, and the version.
function SystemSettings({ instance, version }) {
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
    const command = instance === 'main' ? 'dens open' : `dens open --instance ${instance}`;
    return (
        <>
            <Cache settings={settings} onChange={setSettings} />
            {settings.updatesManaged && (
                <Card title="Updates">
                    <Toggle id="update-checks" label="Check for updates once a day" checked={settings.backgroundUpdateChecks}
                        onChange={(on) => change('backgroundUpdateChecks', on)} />
                    <Toggle id="update-notices" label="Show a notice when an update is available" checked={settings.updateNotifications}
                        onChange={(on) => change('updateNotifications', on)} />
                    <p class="text-sm text-base-content/70">A check asks the release host for the newest version, which sees this computer's IP address.</p>
                </Card>
            )}
            <LocalPassword />
            <Card title="Logging">
                <Field label="Log level">
                    <select class="select" value={settings.logLevel} onChange={(e) => change('logLevel', e.currentTarget.value)}>
                        {settings.logLevels.map((level) => <option key={level} value={level}>{level}</option>)}
                    </select>
                </Field>
            </Card>
            <ErrorText message={save.error} />
            <Card title="This browser">
                <p class="text-sm text-base-content/70">
                    Signing out unpairs this browser. To pair it again, run <code class="rounded bg-base-300 px-1">{command}</code>.
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
                <p class="text-sm text-base-content/70">
                    Dens includes software from other projects, under their own licenses.{' '}
                    <a class="link" href="/licenses" target="_blank" rel="noopener noreferrer">Read them</a>
                </p>
            </Card>
        </>
    );
}

// Cache shows how much of other dens' files this computer keeps, which
// open without downloading again and while their den is offline, up to a
// size the member chooses, and empties it (M6).
function Cache({ settings, onChange }) {
    const save = useAction();
    const reload = async () => onChange(await api.get('/api/settings'));
    return (
        <Card title="Cache">
            <p class="text-sm text-base-content/70">Reduces network use, and lets you view what's cached offline.</p>
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
            <p class="text-sm text-base-content/70">Protects Dens on this computer. It's separate from your den passwords.</p>
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
