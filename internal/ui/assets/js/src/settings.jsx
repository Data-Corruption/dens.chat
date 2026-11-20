// The app's settings: a dialog over whatever the page shows, so a den and
// a call stay in view, its sections listed down the side as Manage's are,
// or on the phone layout a list of them, each a page. Appearance is a page
// of its own (see appearance.jsx); Chat holds compressing photos and
// videos and what happens to links; Notifications what notifies the
// member and the page's sounds; Voice the microphone and speaker, noise
// suppression, how the member sends their voice, their shortcuts and their
// screen share; Relay this install's links with the member's other devices,
// and from M20.2 the computer's sound shared and listened to; and
// System hosting a den, the cache, updates, backups, the local
// password, the logs, signing this browser out, and the version.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { AppearancePage } from './appearance.jsx';
import { sendSmaller, setSendSmaller } from './compare.jsx';
import {
    Actions, Button, Card, Choices, DOCS, DocsLink, DocsTip, ErrorText, Field, Notice, PasswordFields, Slider, StatusText,
    SubmitButton, TextInput, Tip, Toggle, buttonClass, checkPasswords, useAction, useNotifications,
} from './components.jsx';
import { formatSize } from './files.jsx';
import { rememberNote } from './flatpak.js';
import { hostingAsk, hostingNote, hostingOnWarning, moving } from './hosting.js';
import { oldReddit, setOldReddit, setSteamCards, setYouTubePlayers, steamCards, youTubePlayers } from './links.js';
import { PlayIcon } from './icons.jsx';
import { useLayer, useNarrow } from './nav.js';
import { askToNotify, notifying, permission, tell, whereTheyShow } from './notify.js';
import { NO_CALLS, canCall, canShare, hasKeys, here, inApp } from './platform.js';
import { HEIGHTS, RATES, shareAudio } from './share.js';
import { GROUPS, SOUNDS, checkSound, playSound, soundSettings, uploadSound } from './sounds.js';
import { cacheSizes, cacheUsage } from './storage.js';
import { SectionsFrame, useSectionPage } from './pages.jsx';
import { MenuButton } from './menus.jsx';
import { YOUR_SECTIONS, YourDen, useYourDen, yourSections } from './yourden.jsx';
import { BUFFER_HIGH, BUFFER_LOW, bufferChoice, listen, setBufferChoice, stopListening } from './relay.js';
import { useRelay } from './relay.jsx';
import { useHoldsButton } from './touch.js';
import {
    RELEASE_MOST, RELEASE_STEP, callLimits, callVolume, cancelCapture, captureKey, chooseMic, chooseSpeaker, desktopKeys, devices,
    inCall as callNow, keyLabel, keysElsewhere, micProblem, noiseSuppression, onCall, onKeys, onVoiceLevel, setCallVolume, setKeys, setRNNoise,
    setShareSettings, setVoice, shareSettings, startMicTest, stopMicTest, voiceSettings,
} from './voice.js';

export { onOpenSettings, openSettings } from './dialogs.js';

// SECTIONS are the settings' sections, by the id openSettings takes and
// the name the list shows. The browser checks name them too, in
// drive-settings.mjs, drive-calls.mjs and screens.mjs.
export const SECTIONS = [
    ['appearance', 'Appearance'],
    ['chat', 'Chat'],
    ['notifications', 'Notifications'],
    ['voice', 'Voice'],
    ['relay', 'Relay'],
    ['system', 'System'],
];

// SettingsDialog shows a section of the settings. It closes with its ✕,
// Escape, a click outside it, or Back. On the phone layout it takes the
// screen, its sections a list of pages, and Back and Escape go from a page
// to the list. Appearance takes the window at any width, and Back
// and Escape go from it to the section open before; on a wide window the
// settings open at Chat, the first section that doesn't. With denID, as on
// a den's page, it holds that den's sections of the member's own as a
// group above the app's.
export function SettingsDialog({ section, denID, onSection, onClose, instance, version, pair, navigate }) {
    const narrow = useNarrow();
    const { den, view } = useYourDen(denID);
    const yours = den ? yourSections(den) : [];
    // A den's section waits for the den, which may not show it after all.
    const waiting = denID && YOUR_SECTIONS.includes(section) && !den;
    let at = narrow ? '' : 'chat';
    if (SECTIONS.some(([id]) => id === section) || yours.some(([id]) => id === section) || waiting) at = section;
    const full = at === 'appearance';
    // before is the section Appearance was opened from, which Back goes
    // to, and guard what asks before leaving Appearance with a theme's
    // edits unsaved.
    const before = useRef('');
    const guard = useRef(null);
    const asked = (action) => guard.current?.(action) || false;
    const close = () => !asked(onClose) && onClose();
    const back = () => onSection(full && !narrow ? before.current : '');
    useLayer(() => {
        if (!asked(onClose)) return onClose();
        return false;
    });
    useSectionPage(narrow ? !!section : full, () => {
        if (!asked(back)) return back();
        return false;
    });
    // One listener reads what Escape does now, so a second press as the
    // page leaves Appearance isn't taken by the first's.
    const escape = useRef(null);
    escape.current = () => {
        if (full || (narrow && section)) !asked(back) && back();
        else close();
    };
    useEffect(() => {
        const listen = (e) => e.key === 'Escape' && escape.current();
        document.addEventListener('keydown', listen);
        return () => document.removeEventListener('keydown', listen);
    }, []);
    function pick(id) {
        if (id === 'appearance' && at !== 'appearance') before.current = at;
        onSection(id);
    }
    let panel;
    if (waiting) panel = <span class="loading loading-spinner"></span>;
    else if (YOUR_SECTIONS.includes(at)) panel = <YourDen key={at} den={den} view={view} at={at} onClose={onClose} navigate={navigate} />;
    else if (at === 'chat') panel = <ChatSettings />;
    else if (at === 'notifications') panel = <><NotificationSettings /><SoundSettings /></>;
    else if (at === 'voice') panel = <VoiceSettings />;
    else if (at === 'relay') panel = <RelaySettings />;
    else if (at === 'system') panel = <SystemSettings instance={instance} version={version} pair={pair} />;
    else if (full) panel = <AppearancePage guard={guard} />;
    const groups = den
        ? [[den.name, yours.filter(([, , danger]) => !danger)], ['App', SECTIONS], ['', yours.filter(([, , danger]) => danger)]]
        : [['', SECTIONS]];
    return (
        <SectionsFrame label="Settings" title="Settings" groups={groups} at={at} narrow={narrow} full={full} onPick={pick}
            onList={() => !asked(back) && back()} onClose={close}>
            {panel}
        </SectionsFrame>
    );
}

// ChatSettings holds whether this browser sends photos and videos
// compressed, and what happens to links.
function ChatSettings() {
    const [compress, setCompress] = useState(sendSmaller);
    const [old, setOld] = useState(oldReddit);
    const [players, setPlayers] = useState(youTubePlayers);
    const [steam, setSteam] = useState(steamCards);
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
                <Toggle id="steam" label="Show Steam games as cards" checked={steam}
                    hint="Steam will see your IP address when cards load." onChange={(on) => {
                        setSteamCards(on);
                        setSteam(on);
                    }} />
            </Card>
        </>
    );
}

// NotificationSettings turns each kind of notification on or off, and the
// message's start in them. The service keeps them, so every browser
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
        // own: @everyone is among mentions.
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
        <Card title={<>Notifications <DocsTip id="docs-notifications" page="usage/#notifications" side="right">
            Where they show, and muting a den or a channel from the bell beside its name.</DocsTip></>}>
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
            {notifying(status) && whereTheyShow(status, browser, elsewhere).map(([kind, line]) => <StatusText key={line} kind={kind}>{line}</StatusText>)}
            <ErrorText message={act.error} />
        </Card>
    );
}

// SoundSettings holds the page's sounds, grouped Messages and
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
                    <p class="text-sm text-base-content/70">{group === 'Messages' ? message : 'While deafened, only the microphone sounds play.'}</p>
                </div>
            ))}
            <ErrorText message={act.error} />
        </Card>
    );
}

// SoundRow is one of the page's sounds in the settings: its switch, its
// name, its volume, ▶, and ⋯ with Replace and, once replaced, Reset, all
// on one line on a wide window; a phone puts the volume on a line of its
// own, under the name.
function SoundRow({ name, label, s, volume, busy, onOn, onMove, onVolume, onReplace, onReset }) {
    const file = useRef(null);
    const narrow = useNarrow();
    const id = `sound-${name}`;
    const items = [
        { key: 'replace', label: 'Replace with a file…', icon: 'replace', disabled: busy, run: () => file.current?.click() },
        ...(s.own > 0 ? [{ key: 'reset', label: "Reset to Dens's sound", icon: 'reset', disabled: busy, run: onReset }] : []),
    ];
    const slider = (
        <span class={`flex min-w-0 items-center gap-3 ${narrow ? 'pl-13' : 'w-36 shrink-0'}`}>
            <Slider class="min-w-0 flex-1" min="0" max="100" step="5" value={volume} disabled={!s.on}
                aria-label={`${label} volume`} aria-valuetext={`${volume}%`}
                onInput={(e) => onMove(Number(e.currentTarget.value))} onChange={(e) => onVolume(Number(e.currentTarget.value))} />
            <span class="w-10 text-right text-sm tabular-nums">{volume}%</span>
        </span>
    );
    return (
        <div class="flex flex-col gap-1" data-sound={name} data-own={s.own > 0 ? '' : undefined}>
            <div class="flex items-center gap-3">
                <input id={id} type="checkbox" class="toggle shrink-0" checked={s.on} onChange={(e) => onOn(e.currentTarget.checked)} />
                <label for={id} class="min-w-0 flex-1 cursor-pointer truncate">
                    {label}
                    {s.own > 0 && <span class="text-sm text-base-content/60"> · your file</span>}
                </label>
                {!narrow && slider}
                <button type="button" class={`${buttonClass('quiet')} btn-square`} aria-label={`Play ${label}`} title={`Play ${label}`}
                    onClick={() => playSound(name, { preview: true, volume })}>
                    <PlayIcon />
                </button>
                <MenuButton label={`More for ${label}`} items={items} button={{ class: `${buttonClass('quiet')} btn-square`, title: 'More', children: '⋯' }} />
                <input ref={file} type="file" accept="audio/*" class="hidden" onChange={(e) => {
                    const picked = e.currentTarget.files?.[0];
                    e.currentTarget.value = '';
                    if (picked) onReplace(picked);
                }} />
            </div>
            {narrow && slider}
        </div>
    );
}

// VoiceSettings chooses the microphone and speaker, how loud calls play,
// the noise suppression, how the member sends their voice, their
// shortcuts, and how they share their screen. Its meter shows what the
// voice processor hears and whether it sends it: in the call while there
// is one, and otherwise in a test the member starts. What the page can't
// use leaves it, as on a phone: the shortcuts where the service
// says there are no keys, and sharing where the browser can't share. Where
// the page has the call's button, its role takes the place of how the
// member sends.
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
            {!canCall() && <Notice kind="warning">{NO_CALLS}</Notice>}
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
                        <Slider class="min-w-0 flex-1" min="0" max="100" step="5" value={Math.round(loud * 100)}
                            aria-label="Call volume" aria-valuetext={`${Math.round(loud * 100)}%`}
                            onInput={(e) => pickLoudness(Number(e.currentTarget.value) / 100)} />
                        <span class="w-10 text-right text-sm tabular-nums">{Math.round(loud * 100)}%</span>
                    </div>
                </Field>
                <Toggle id="noise" label="Noise suppression" checked={suppression.rnnoise} onChange={pickSuppression}
                    hint="Removes keyboard, fan and background noise."
                    tip="It adds 30 ms of delay and uses a little of this device's processor. Off, the built-in suppression takes out only steady noise, such as a fan's." />
                {suppression.processorFailed ? (
                    <StatusText kind="warning">Voice processing can't run here; voice activity sends everything.</StatusText>
                ) : suppression.failed && <StatusText kind="warning">Noise suppression couldn't start; the built-in one is on.</StatusText>}
            </Card>
            <Card title={holds ? "The call's button" : 'Sending your voice'}>
                {holds ? (
                    <Choices name="The call's button" data="data-call-button-role" choice={v.button} onPick={(button) => change({ button })} choices={ROLES} />
                ) : (
                    <Choices name="Sending your voice" data="data-sending" choice={v.mode} onPick={(mode) => change({ mode })}
                        choices={[['voice', 'Voice activity'], ['ptt', 'Push to talk']]} />
                )}
                {v.mode === 'voice' ? (
                    <div class="flex flex-col gap-3 rounded-box bg-base-100 p-3 max-md:bg-base-200">
                        <Toggle id="auto-sensitivity" label="Automatic sensitivity" checked={auto} disabled={!suppressing}
                            hint={suppressing ? "Detects when you're speaking." : 'Needs noise suppression.'}
                            onChange={(on) => change({ auto: on })} />
                        {!auto && (
                            <label class="flex flex-col gap-1">
                                <span class="text-sm">Sensitivity</span>
                                <Slider class="w-full" min="-100" max="0" step="1" value={v.threshold}
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
                                <Slider class="min-w-0 flex-1" min="0" max={RELEASE_MOST} step={RELEASE_STEP}
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
                        <Button class="shrink-0" onClick={test}>{testing ? 'Stop test' : 'Test microphone'}</Button>
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
                                <button type="button" class={`${buttonClass('secondary')} min-w-28 font-normal`} disabled={!!capturing && capturing !== name}
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
                                    <Button kind="quiet" disabled={!!capturing} onClick={() => saveKeys({ ...v.keys, [name]: undefined })}>Clear</Button>
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

// ROLES are what the call's button may do, its default first.
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
// Dens has focus. Where the desktop holds them, the member sets
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
                        {elsewhere.configurable && <Button onClick={() => onDesktop('configure')}>Change them</Button>}
                        <Button kind="quiet" onClick={() => onDesktop('stop')}>Stop using them</Button>
                    </div>
                </div>
            );
        }
        if (elsewhere.binding) return <p class={text} role="status">Confirm the shortcuts in your desktop's dialog.</p>;
        // The desktop binds once a session, and this one has none of them
        // now, as when they were taken out in its settings.
        if (!elsewhere.bindable) {
            return (
                <div class="flex flex-col gap-2">
                    <p class={text}>None are set on your desktop, so they work while Dens is focused.</p>
                    {elsewhere.note && <p class={text}>{elsewhere.note}</p>}
                    <div class="flex flex-wrap gap-2">
                        {elsewhere.configurable && <Button onClick={() => onDesktop('configure')}>Change them</Button>}
                        <Button kind="quiet" onClick={() => onDesktop('stop')}>Start over</Button>
                    </div>
                </div>
            );
        }
        return (
            <div class="flex flex-col gap-2">
                <p class={text}>Work while Dens is focused. Set them up to work in every app.</p>
                <div>
                    <Button onClick={() => onDesktop('bind')}>Set up with your desktop</Button>
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

// ShareSettings chooses how the member shares their screen: its
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
            <Choices label="Optimize for" name="hint" data="data-share-hint" choice={s.hint} onPick={(hint) => change({ hint })}
                choices={[['detail', 'Clarity'], ['motion', 'Smoothness']]} />
            <Toggle id="share-audio" label="Share audio" hint={audio.text} checked={audio.supported && s.sound} disabled={!audio.supported}
                onChange={(on) => change({ sound: on })} />
        </Card>
    );
}

// DEVICES_EVERY is how often an open Relay section asks again which of
// the member's devices are there.
const DEVICES_EVERY = 3000;

// RelaySettings links this install with the member's other devices on one
// network: Keybind relay and, from M20.2, Share this computer's
// sound, whose switches the service keeps, and the buffer of what this
// page plays, which the browser keeps; and the devices that answered,
// each with how its link stands or what it offers, and Listen beside each
// that shares its sound, asked again while the section is open. A phone's
// keys are its call's button, and where the member refused Android's ask
// to let Dens reach the network, the section says so.
export function RelaySettings() {
    const [relay, setRelay] = useState(null);
    const [devices, setDevices] = useState(null);
    const [refused, setRefused] = useState(false);
    const [buffer, setBuffer] = useState(bufferChoice);
    const sound = useRelay();
    const load = useAction();
    const save = useAction();
    useEffect(() => {
        load.run(async () => setRelay((await api.get('/api/settings')).relay));
        let live = true;
        const ask = () => api.get('/api/relay/devices').then((d) => {
            if (!live) return;
            setDevices(d?.devices || []);
            setRefused(!!d?.refused);
        }, () => {});
        ask();
        const timer = setInterval(ask, DEVICES_EVERY);
        return () => {
            live = false;
            clearInterval(timer);
        };
    }, []);
    if (!relay) return load.error ? <ErrorText message={load.error} /> : <span class="loading loading-spinner"></span>;
    const set = (key, on) => save.run(async () => {
        await api.post('/api/settings', { [key]: on });
        setRelay((r) => ({ ...r, [key === 'relayKeys' ? 'keys' : 'sound']: on }));
    });
    return (
        <>
            {refused && (
                <Notice kind="warning" data-relay-refused>Android keeps Dens off your network. Allow it in Android's settings, under Apps.</Notice>
            )}
            {relay.can?.includes('keys') && (
                <Card title="Keybind relay">
                    <Toggle id="relay-keys" label={hasKeys() ? 'Relay voice keys' : "Relay the call's button"}
                        hint={hasKeys() ? 'Keys pressed on one device act in the call another holds.' : 'It acts in the call another of your devices holds.'}
                        checked={!!relay.keys} disabled={save.busy} onChange={(on) => set('relayKeys', on)} />
                    <ErrorText message={save.error} />
                </Card>
            )}
            <Card title="Audio relay">
                {relay.can?.includes('sound') && (
                    <Toggle id="relay-sound" label="Share this computer's sound" checked={!!relay.sound} disabled={save.busy}
                        tip="Muted, it still sends; at volume 0, Windows sends silence."
                        onChange={(on) => set('relaySound', on)} />
                )}
                <Choices label="Buffer" name="relay-buffer" data="data-relay-buffer" choice={buffer} choices={[[BUFFER_LOW, 'Low'], [BUFFER_HIGH, 'High']]}
                    tip="High plays a little later, and rides out a busy Wi-Fi."
                    onPick={(b) => {
                        setBufferChoice(b);
                        setBuffer(b);
                    }} />
            </Card>
            <Card title="Your devices">
                <RelayDevices devices={devices} listening={sound?.listening} />
                <p class="text-sm text-base-content/70">
                    Signed in as you, on this network. <DocsLink page="relay/" label="How Relay works" />
                </p>
            </Card>
        </>
    );
}

// relayState is what a device's row says of it: its link, or what it
// offers.
export function relayState(d) {
    if (d.linked) return 'Linked';
    if (d.apart) return 'Not on this network';
    const offers = [];
    if (d.offers?.includes('keys')) offers.push('Keybind relay');
    if (d.offers?.includes('sound')) offers.push('Sharing its sound');
    return offers.join(', ') || 'Keybind relay off';
}

// RelayDevices lists the member's other devices that answered, with Listen
// beside each that shares its sound, or Stop beside the one this page
// listens to.
function RelayDevices({ devices, listening }) {
    if (!devices) return <span class="loading loading-spinner"></span>;
    if (devices.length === 0) return <p class="text-sm">None of your other devices answered.</p>;
    return (
        <ul aria-label="Your devices" class="flex flex-col gap-1">
            {devices.map((d) => {
                const mine = listening?.id === d.id;
                return (
                    <li key={d.id} data-device={d.id} class="flex min-h-11 items-center gap-3">
                        <span class="min-w-0 flex-1 truncate">{d.name}</span>
                        {d.linked ? <StatusText kind="success" inline class="shrink-0">{relayState(d)}</StatusText>
                            : <span class="shrink-0 text-sm text-base-content/70">{relayState(d)}</span>}
                        {mine ? (
                            <Button class="shrink-0" data-listen={d.id} onClick={stopListening}>Stop</Button>
                        ) : d.offers?.includes('sound') ? (
                            <Button class="shrink-0" data-listen={d.id} onClick={() => listen({ id: d.id, name: d.name })}>Listen</Button>
                        ) : null}
                    </li>
                );
            })}
        </ul>
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
                    <a class={buttonClass('secondary')} href="/api/logs" download>Download the newest logs</a>
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
                    <Button onClick={() => save.run(async () => {
                        await api.post('/api/logout');
                        window.location.replace('/');
                    })}>
                        Sign out
                    </Button>
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

// CheckNow checks for an update at once, in the Android app,
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
                <Button busy={check.busy} onClick={run}>{check.busy ? 'Checking…' : 'Check now'}</Button>
                {found && !found.update && <StatusText kind="success" inline>Dens is up to date.</StatusText>}
                {found?.update && (
                    <>
                        {/* One string, which a screen reader reads as one. */}
                        <span class="text-sm">{`Dens ${found.update} is ready to install.`}</span>
                        <Button kind="primary" disabled={install.busy} onClick={() => install.run(() => api.post('/api/update'))}>
                            {install.busy ? 'Opening the installer…' : 'Install'}
                        </Button>
                    </>
                )}
            </div>
            <ErrorText message={check.error || install.error} />
        </div>
    );
}

// OnThisComputer is what only the Flatpak has: remembering its key,
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
                    <Actions>
                        <SubmitButton busy={act.busy}>Remember</SubmitButton>
                        <Button kind="quiet" disabled={act.busy} onClick={() => setAsking(false)}>Cancel</Button>
                    </Actions>
                </form>
            ) : (
                <>
                    <p class="text-sm text-base-content/70" role={remember.asking ? 'status' : undefined}>{note.text}</p>
                    {note.problem && <StatusText kind="warning">{note.problem}</StatusText>}
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
                <Button disabled={act.busy} onClick={() => act.run(async () => {
                    await api.post('/api/quit');
                    setQuit(true);
                })}>
                    Quit Dens
                </Button>
            </div>
            <ErrorText message={act.error} />
        </Card>
    );
}

// BackUp downloads a backup, as dens backup writes it, after the local
// password, which protects it, saying first about how large it will
// be.
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
                    <Actions>
                        <SubmitButton busy={act.busy}>Download the backup</SubmitButton>
                        <Button kind="quiet" disabled={act.busy} onClick={() => setAsking(false)}>Cancel</Button>
                        {size != null && <span class="text-sm text-base-content/70">About {formatSize(size)}</span>}
                    </Actions>
                </form>
            ) : (
                <div class="flex flex-wrap items-center gap-3">
                    <Button onClick={ask}>Back up</Button>
                    {done && <StatusText kind="success" inline role="status">Your browser is downloading the backup.</StatusText>}
                </div>
            )}
            <ErrorText message={act.error} />
        </Card>
    );
}

// Hosting turns hosting a den on this computer on and off. Turning
// it on opens ports to the internet, so it takes the local password, and
// for a den exported since, says first that another computer may host it
// by now; turning it off asks first, since members can't reach the
// den until it's back on. While it's on, it says where Caddy and the
// router send the den. Once there's a den, it moves it to another
// computer, with its export, and, once that computer serves it, removes
// it from this one.
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
    const cancel = <Button kind="quiet" disabled={act.busy} onClick={() => begin('')}>Cancel</Button>;
    return (
        <Card title={title}>
            <Toggle id="hosting" label="Host a den on this computer" checked={on} disabled={act.busy} onChange={ask}
                hint="Needs this computer on, a domain, and Caddy in front." />
            {asking === 'on' && (
                <form class="flex flex-col gap-2" onSubmit={turnOn}>
                    {warning && <Notice kind="warning">{warning}</Notice>}
                    <p class="text-sm text-base-content/70">Opening ports to the internet takes your local password.</p>
                    {passwordField}
                    <Actions>
                        <SubmitButton busy={act.busy}>Turn hosting on</SubmitButton>
                        <Button kind="quiet" disabled={act.busy} onClick={() => ask(hosting.enabled)}>Cancel</Button>
                    </Actions>
                </form>
            )}
            {asking === 'off' && (
                <div class="flex flex-col gap-2">
                    <p class="text-sm">Members can't reach your den until hosting is back on.</p>
                    <p class="text-sm text-base-content/70">Everything in it stays.</p>
                    <Actions>
                        <Button kind="danger" filled busy={act.busy} onClick={turnOff}>Turn hosting off</Button>
                        <Button kind="quiet" disabled={act.busy} onClick={() => ask(hosting.enabled)}>Cancel</Button>
                    </Actions>
                </div>
            )}
            {note?.problem && <Notice kind="warning">{note.problem}</Notice>}
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
                            <Actions>
                                <SubmitButton busy={act.busy}>Download the den's export</SubmitButton>
                                {cancel}
                                {size != null && <span class="text-sm text-base-content/70">About {formatSize(size)}</span>}
                            </Actions>
                        </form>
                    )}
                    {asking === 'remove' && (
                        <form class="flex flex-col gap-2" onSubmit={remove}>
                            <p class="text-sm">Deletes this computer's copy of {moves.den}, its messages and files.</p>
                            <p class="text-sm text-base-content/70">Dens first checks that another computer serves it at {moves.url}.</p>
                            <p class="text-sm text-base-content/70">Your account stays; Dens here reaches the den at its address.</p>
                            {passwordField}
                            <Actions>
                                <SubmitButton busy={act.busy} kind="danger" filled>Remove from this computer</SubmitButton>
                                {cancel}
                            </Actions>
                        </form>
                    )}
                    {!asking && (
                        <div class="flex flex-wrap gap-2">
                            <Button onClick={askMove}>Move the den to another computer</Button>
                            {moves.exported && <Button kind="danger" onClick={() => begin('remove')}>Remove from this computer</Button>}
                        </div>
                    )}
                </div>
            )}
            {removed && (
                <StatusText kind="success" role="status">Removed {removed.name} from this computer. Dens reaches it at {removed.url} now.</StatusText>
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
// size the member chooses, and empties it.
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
                <Button disabled={save.busy || !settings.cacheUsed} onClick={() => save.run(async () => {
                    await api.post('/api/cache/clear');
                    await reload();
                })}>
                    Clear cache
                </Button>
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
                    <Actions>
                        <SubmitButton busy={busy}>Change password</SubmitButton>
                        <Button kind="quiet" onClick={close}>Cancel</Button>
                    </Actions>
                </form>
            ) : (
                <div class="flex flex-wrap items-center gap-3">
                    <Button onClick={() => {
                        setDone(false);
                        setOpen(true);
                    }}>
                        Change password
                    </Button>
                    {done && <StatusText kind="success" inline>Password changed.</StatusText>}
                </div>
            )}
        </Card>
    );
}
