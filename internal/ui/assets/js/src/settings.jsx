// The settings: a dialog over whatever the page shows, so a den and a call
// stay in view (M3.3). Voice holds the microphone, the speaker and how the
// member sends their voice; General holds the theme, update checks, the
// local password, logging, where Reddit links open, and signing this
// browser out.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { Card, ErrorText, Field, PasswordFields, SubmitButton, TextInput, checkPasswords, useAction } from './components.jsx';
import { oldReddit, setOldReddit } from './links.js';
import { THEMES, setTheme, themeChoice, themeName } from './theme.js';
import {
    cancelCapture, captureKey, chooseMic, chooseSpeaker, devices, inCall as callNow, keyLabel, micProblem, noiseSuppression, onCall, onVoiceLevel,
    setRNNoise, setVoice, startMicTest, stopMicTest, voiceSettings,
} from './voice.js';

const openers = new Set();

// openSettings opens the settings at a section, "voice" or "general", from
// anywhere on the page.
export function openSettings(section = 'general') {
    openers.forEach((fn) => fn(section));
}

export function onOpenSettings(fn) {
    openers.add(fn);
    return () => openers.delete(fn);
}

const SECTIONS = [
    ['general', 'General'],
    ['voice', 'Voice'],
];

// SettingsDialog shows a section of the settings. It closes with its ✕,
// Escape or a click outside it.
export function SettingsDialog({ section, onSection, onClose }) {
    useEffect(() => {
        const escape = (e) => e.key === 'Escape' && onClose();
        document.addEventListener('keydown', escape);
        return () => document.removeEventListener('keydown', escape);
    }, [onClose]);
    return (
        <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={(e) => e.target === e.currentTarget && onClose()}>
            <div role="dialog" aria-modal="true" aria-label="Settings" class="card flex h-full max-h-[48rem] w-full max-w-2xl flex-col bg-base-100 shadow-xl">
                <div class="flex items-center gap-2 px-4 pt-3">
                    <h2 class="card-title flex-1">Settings</h2>
                    <button type="button" class="btn btn-ghost btn-sm" onClick={onClose} aria-label="Close">✕</button>
                </div>
                <div role="tablist" class="tabs tabs-border px-2">
                    {SECTIONS.map(([id, name]) => (
                        <button key={id} type="button" role="tab" aria-selected={section === id} class={`tab ${section === id ? 'tab-active' : ''}`}
                            onClick={() => onSection(id)}>
                            {name}
                        </button>
                    ))}
                </div>
                <div role="tabpanel" class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">
                    {section === 'voice' ? <VoiceSettings /> : <General />}
                </div>
            </div>
        </div>
    );
}

// VoiceSettings chooses the microphone and speaker, the noise suppression,
// how the member sends their voice, and their keys. Its meter shows what
// the voice processor hears and whether it sends it: in the call while
// there is one, and otherwise in a test the member starts.
function VoiceSettings() {
    const [v, setV] = useState(voiceSettings);
    const [suppression, setSuppression] = useState(noiseSuppression);
    const [list, setList] = useState(null);
    const [error, setError] = useState('');
    const [report, setReport] = useState(null);
    const [inCall, setInCall] = useState(callNow);
    const [testing, setTesting] = useState(false);
    const [capturing, setCapturing] = useState('');
    useEffect(() => onCall((c) => setInCall(!!c.call)), []);
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
    function pickRNNoise(on) {
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
    async function bind(name) {
        setCapturing(name);
        const key = await captureKey();
        setCapturing('');
        if (key) change({ keys: { ...v.keys, [name]: key } });
    }
    const rnnoise = suppression.rnnoise && !suppression.failed && !suppression.processorFailed;
    const auto = v.mode === 'voice' && v.auto && rnnoise;
    return (
        <>
            {!list ? (
                <Card title="Microphone and speaker">
                    <span class="loading loading-dots loading-sm"></span>
                </Card>
            ) : (
                <Card title="Microphone and speaker">
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
                </Card>
            )}
            <Card title="Noise suppression">
                <label class="label items-start gap-3 whitespace-normal">
                    <input type="checkbox" class="toggle mt-0.5" checked={suppression.rnnoise} onChange={(e) => pickRNNoise(e.currentTarget.checked)} />
                    <span>
                        RNNoise
                        <span class="block text-sm text-base-content/70">
                            Takes out keyboards, fans, hum and other noise around you, and keeps voices. It adds 30 ms of delay and uses a little of
                            this computer's processor. Off, the browser's own suppression takes out only steady noise, such as a fan.
                        </span>
                    </span>
                </label>
                {suppression.processorFailed ? (
                    <p class="text-sm text-warning">
                        This browser couldn't run Dens's voice processing, so the browser's own suppression is on and voice activity sends
                        everything. Push to talk still works.
                    </p>
                ) : suppression.failed && <p class="text-sm text-warning">RNNoise couldn't start in this browser, so the browser's own suppression is on.</p>}
            </Card>
            <Card title="Sending your voice">
                <div class="flex flex-wrap gap-4">
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
                    <>
                        <label class="label items-start gap-3 whitespace-normal">
                            <input type="checkbox" class="toggle toggle-sm mt-0.5" checked={v.auto} disabled={!rnnoise}
                                onChange={(e) => change({ auto: e.currentTarget.checked })} />
                            <span>
                                Automatically
                                <span class="block text-sm text-base-content/70">
                                    {rnnoise
                                        ? 'Sends whatever RNNoise takes for speech, however loud.'
                                        : 'Needs RNNoise. Without it, your voice goes when it’s louder than the level below.'}
                                </span>
                            </span>
                        </label>
                        <div class={`flex flex-col gap-1 ${auto ? 'opacity-50' : ''}`}>
                            <span class="text-sm">Level {auto ? '' : `(${v.threshold} dB)`}</span>
                            <Meter report={report} threshold={auto ? null : v.threshold} />
                            <input type="range" class="range range-xs w-full" min="-100" max="0" step="1" value={v.threshold} disabled={auto}
                                aria-label="Voice activity level" aria-valuetext={`${v.threshold} dB`}
                                onInput={(e) => change({ threshold: Number(e.currentTarget.value) })} />
                        </div>
                    </>
                ) : (
                    <div class="flex flex-col gap-1">
                        <p class="text-sm text-base-content/70">
                            {v.keys.ptt ? `Hold ${keyLabel(v.keys.ptt)} to send your voice.` : 'Set a push-to-talk key below, or nothing is sent.'}
                        </p>
                        <Meter report={report} threshold={null} />
                    </div>
                )}
                <p class="text-sm text-base-content/70">
                    The bar is your microphone, green while your voice is sent.{' '}
                    {inCall ? 'It shows the call.' : 'Test your microphone to see it.'}
                </p>
                {!inCall && (
                    <div>
                        <button type="button" class="btn btn-sm" onClick={test}>{testing ? 'Stop the test' : 'Test my microphone'}</button>
                    </div>
                )}
            </Card>
            <Card title="Keys">
                {KEYS.map(([name, title, hint]) => (
                    <div key={name} class="flex flex-wrap items-center gap-2">
                        <div class="min-w-40 flex-1">
                            <p>{title}</p>
                            <p class="text-sm text-base-content/70">{hint}</p>
                        </div>
                        {capturing === name ? (
                            <span class="text-sm">Press a key. Escape cancels.</span>
                        ) : v.keys[name] ? (
                            <kbd class="kbd kbd-sm">{keyLabel(v.keys[name])}</kbd>
                        ) : (
                            <span class="text-sm text-base-content/50">Not set</span>
                        )}
                        <button type="button" class="btn btn-sm" disabled={!!capturing} onClick={() => bind(name)}>{v.keys[name] ? 'Change' : 'Set'}</button>
                        {v.keys[name] && (
                            <button type="button" class="btn btn-ghost btn-sm" disabled={!!capturing} onClick={() => change({ keys: { ...v.keys, [name]: undefined } })}>
                                Clear
                            </button>
                        )}
                    </div>
                ))}
                <p class="text-sm text-base-content/70">
                    Keys work while Dens is the window and tab you're in, as any web page's do. A key you're holding when you switch away counts as
                    let go.
                </p>
            </Card>
            {error && <ErrorText message={error} />}
        </>
    );
}

const KEYS = [
    ['ptt', 'Push to talk', 'Sends your voice while you hold it.'],
    ['toggleMute', 'Toggle mute', 'Mutes or unmutes you.'],
    ['pushMute', 'Push to mute', 'Mutes you while you hold it.'],
];

// Meter shows the voice processor's level, green while it sends, against
// the voice activity level when there is one.
function Meter({ report, threshold }) {
    const at = (db) => `${Math.max(0, Math.min(100, db + 100))}%`;
    return (
        <div class="relative h-2 w-full overflow-hidden rounded bg-base-300" aria-hidden="true">
            <div class={`h-full ${report?.open ? 'bg-success' : 'bg-base-content/40'}`} style={{ width: report ? at(report.level) : '0%' }}></div>
            {threshold !== null && <div class="absolute inset-y-0 w-0.5 bg-warning" style={{ left: at(threshold) }}></div>}
        </div>
    );
}

function General() {
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
            <ThemePicker />
            {settings.updatesManaged && (
                <Card title="Updates">
                    <label class="label gap-3">
                        <input
                            type="checkbox"
                            class="toggle"
                            checked={settings.backgroundUpdateChecks}
                            onChange={(e) => change('backgroundUpdateChecks', e.currentTarget.checked)}
                        />
                        Check for updates once a day
                    </label>
                    <label class="label gap-3">
                        <input
                            type="checkbox"
                            class="toggle"
                            checked={settings.updateNotifications}
                            onChange={(e) => change('updateNotifications', e.currentTarget.checked)}
                        />
                        Show a notice when an update is available
                    </label>
                    <p class="text-sm text-base-content/70">
                        A check only asks the release host for the latest version number, which shows it this computer's IP address.
                    </p>
                </Card>
            )}
            <ChangePassword />
            <Card title="Logging">
                <Field label="Log level">
                    <select class="select" value={settings.logLevel} onChange={(e) => change('logLevel', e.currentTarget.value)}>
                        {settings.logLevels.map((level) => <option key={level} value={level}>{level}</option>)}
                    </select>
                </Field>
            </Card>
            <ErrorText message={save.error} />
            <Links />
            <Card title="This browser">
                <p>Signing out unpairs this browser. Run dens open again to pair it.</p>
                <div>
                    <button
                        type="button"
                        class="btn btn-outline"
                        onClick={() => save.run(async () => {
                            await api.post('/api/logout');
                            window.location.replace('/');
                        })}
                    >
                        Sign out
                    </button>
                </div>
            </Card>
            <p class="text-sm text-base-content/60">
                Dens includes software from other projects, under their own licenses.{' '}
                <a class="link" href="/licenses" target="_blank" rel="noopener noreferrer">Read them</a>
            </p>
        </>
    );
}

// ThemePicker chooses the page's theme from every one it has, each shown
// in its colors, or the device's light or dark; it takes hold at once, and
// this browser keeps it.
function ThemePicker() {
    const [choice, setChoice] = useState(themeChoice);
    function pick(theme) {
        setTheme(theme);
        setChoice(theme);
    }
    return (
        <Card title="Theme">
            <div role="radiogroup" aria-label="Theme" class="grid grid-cols-[repeat(auto-fill,minmax(9.5rem,1fr))] gap-1">
                {['system', ...THEMES].map((theme) => (
                    <label key={theme}
                        class="flex cursor-pointer items-center gap-2 rounded-field px-2 py-1.5 text-sm hover:bg-base-200 has-[:checked]:bg-base-200 has-[:checked]:font-medium has-[:checked]:ring-1 has-[:checked]:ring-primary has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-primary">
                        <input type="radio" name="theme" class="sr-only" value={theme} checked={choice === theme} onChange={() => pick(theme)} />
                        {theme === 'system' ? <SystemSwatch /> : <Swatch theme={theme} />}
                        <span class="truncate">{theme === 'system' ? 'System' : themeName(theme)}</span>
                    </label>
                ))}
            </div>
            <p class="text-sm text-base-content/70">System follows this device's light or dark setting, with Light and Dark.</p>
        </Card>
    );
}

// Swatch shows a theme's colors: its text, primary, secondary and accent on
// its background.
function Swatch({ theme }) {
    return (
        <span data-theme={theme} aria-hidden="true" class="grid shrink-0 grid-cols-2 gap-0.5 rounded-md border border-base-content/10 bg-base-100 p-1">
            <span class="size-1.5 rounded-full bg-base-content" />
            <span class="size-1.5 rounded-full bg-primary" />
            <span class="size-1.5 rounded-full bg-secondary" />
            <span class="size-1.5 rounded-full bg-accent" />
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

// Links says what happens to links that are sent, and chooses where this
// browser opens Reddit's.
function Links() {
    const [old, setOld] = useState(oldReddit);
    return (
        <Card title="Links">
            <p class="text-sm text-base-content/70">
                When a link is sent, Dens takes out common tracking parameters, and shortens Reddit, YouTube, X and Amazon links to the
                post, video or product they point to.
            </p>
            <label class="label gap-3">
                <input
                    type="checkbox"
                    class="toggle"
                    checked={old}
                    onChange={(e) => {
                        setOldReddit(e.currentTarget.checked);
                        setOld(e.currentTarget.checked);
                    }}
                />
                Open Reddit links on old.reddit.com
            </label>
        </Card>
    );
}

function ChangePassword() {
    const [current, setCurrent] = useState('');
    const [next, setNext] = useState('');
    const [confirm, setConfirm] = useState('');
    const [done, setDone] = useState(false);
    const { busy, error, setError, run } = useAction();

    function submit(e) {
        e.preventDefault();
        setDone(false);
        const problem = checkPasswords(next, confirm);
        if (problem) return setError(problem);
        run(async () => {
            await api.post('/api/password/change', { current, next });
            setCurrent('');
            setNext('');
            setConfirm('');
            setDone(true);
        });
    }

    return (
        <Card title="Local password">
            <p class="text-sm text-base-content/70">
                It protects Dens on this computer: its backups, and the keys it holds for every den. It isn't a den password, which
                signs you in to one den on a new device.
            </p>
            <form class="flex flex-col gap-2" onSubmit={submit}>
                <Field label="Current password">
                    <TextInput type="password" value={current} onInput={setCurrent} autocomplete="current-password" required />
                </Field>
                <PasswordFields password={next} setPassword={setNext} confirm={confirm} setConfirm={setConfirm} />
                <ErrorText message={error} />
                {done && <p class="text-success">Password changed.</p>}
                <SubmitButton busy={busy}>Change password</SubmitButton>
            </form>
        </Card>
    );
}
