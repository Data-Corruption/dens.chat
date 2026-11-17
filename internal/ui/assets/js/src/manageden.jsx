// A den's Manage dialog (M8.3), for what staff manage: its address and Den
// ID, which everyone sees, and the name and address its owner changes;
// invites, bans, the soundboard (M14.3) and emoji (M15.2), for staff, each
// with its limits for the owner; and the owner's events and limits. A
// member's own sections of a den are the settings' (M22.3). The owner's
// edits gather in one draft, saved from one bar.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import {
    Button, Card, CopyButton, ErrorText, Field, PLAIN, StatusText, SubmitButton, TILE, TextInput, Tip, Toggle, day, dayTime, useAction,
} from './components.jsx';
import { MAX_RETENTION, callFields, callLimitsOf, changed, changesOf, draftOf, hasBoard, hasEmoji, hasEvents } from './dendraft.js';
import { ManageEmoji } from './emoji.jsx';
import { onEvent } from './events.js';
import { useLayer, useNarrow } from './nav.js';
import { SectionsFrame, useSectionPage } from './pages.jsx';
import { rank } from './people.jsx';
import { deletesNow, keptFor } from './retention.js';
import { CALL_PRESETS, HEIGHTS, mbps, presetOf, uploadNeeded } from './share.js';
import { ManageSoundboard } from './soundboard.jsx';

// sectionsFor lists the sections of a den's Manage a member sees, each
// [id, name]: everything but General needs the den connected; Invites,
// Bans, the Soundboard and Emoji are staff's, and Events and Limits the
// owner's.
export function sectionsFor(den) {
    const list = [['general', 'General']];
    if (den.state !== 'connected') return list;
    if (rank(den.role) > 0) list.push(['invites', 'Invites'], ['bans', 'Bans'], ['soundboard', 'Soundboard'], ['emoji', 'Emoji']);
    if (den.role === 'owner') list.push(['events', 'Events'], ['limits', 'Limits']);
    return list;
}

// ManageDialog manages one den, at a section. It closes with its ✕,
// Escape, a click outside it, or Back (M13.1), asking first when edits
// aren't saved. On the phone layout it takes the screen, its sections a
// list of pages, and Back and Escape go from a page to the list (M13.3).
export function ManageDialog({ denID, section, onSection, onClose }) {
    const [den, setDen] = useState(null);
    const [view, setView] = useState(null);
    const [base, setBase] = useState(null);
    const [draft, setDraft] = useState(null);
    const [leaving, setLeaving] = useState(false);
    const [saved, setSaved] = useState(false);
    const save = useAction();
    const narrow = useNarrow();

    // The den's status on this computer comes with every change to the
    // list; its state, which the owner's settings need, loads once it's
    // connected.
    useEffect(() => {
        const take = (v) => setDen(v.dens.find((d) => d.den_id === denID) || null);
        api.get('/api/dens').then(take, () => {});
        return onEvent((m) => m.t === 'dens' && take(m.d));
    }, [denID]);
    const connected = den?.state === 'connected';
    const loadView = () => api.get(`/api/dens/${denID}/state`).then((v) => {
        setView(v);
        return v;
    });
    useEffect(() => {
        if (connected) loadView().catch(() => {});
    }, [connected, denID]);
    const owner = den?.role === 'owner';
    // The draft starts from the den as it is, once.
    useEffect(() => {
        if (owner && den && view && !base) {
            const b = draftOf(den, view);
            setBase(b);
            setDraft(b);
        }
    }, [owner, den, view]);
    const dirty = !!(owner && draft && base && changed(draft, base));

    function close() {
        if (dirty) setLeaving(true);
        else onClose();
    }
    const toList = () => onSection('');
    useEffect(() => {
        const escape = (e) => e.key === 'Escape' && (narrow && section ? toList() : close());
        document.addEventListener('keydown', escape);
        return () => document.removeEventListener('keydown', escape);
    });
    // Back with changes unsaved asks, as the ✕ does, and the dialog stays.
    useLayer(() => {
        if (!dirty) return onClose();
        setLeaving(true);
        return false;
    });
    useSectionPage(narrow && !!section, toList);

    function edit(changes) {
        setSaved(false);
        setLeaving(false);
        save.setError('');
        setDraft((d) => ({ ...d, ...changes }));
    }

    function discard() {
        setDraft(base);
        setLeaving(false);
        save.setError('');
    }

    function saveAll() {
        const { body, problem } = changesOf(draft, base);
        if (problem) return save.setError(problem);
        save.run(async () => {
            await api.post(`/api/dens/${denID}/settings`, body);
            // The den has what was sent: the draft is the den now.
            const next = { ...draft, name: draft.name.trim(), url: draft.url.trim() };
            setBase(next);
            setDraft(next);
            setSaved(true);
            setLeaving(false);
            loadView().catch(() => {});
        });
    }

    if (!den) {
        return (
            <SectionsFrame label="Manage" title="Manage" groups={[]} at="loading" narrow={narrow} onPick={() => {}} onList={onClose} onClose={onClose}>
                <span class="loading loading-spinner"></span>
            </SectionsFrame>
        );
    }
    // A den older than events has no limit for them to set.
    const sections = sectionsFor(den).filter(([id]) => id !== 'events' || !view || view.event_limits);
    let at = 'general';
    if (sections.find(([id]) => id === section)) at = section;
    else if (narrow && !section) at = '';
    const ready = (panel) => (draft && view ? panel : <span class="loading loading-spinner"></span>);
    let panel;
    if (at === 'invites') {
        panel = <Invites denID={denID} />;
    } else if (at === 'bans') {
        panel = <Bans denID={denID} role={den.role} />;
    } else if (at === 'soundboard') {
        panel = <ManageSoundboard denID={denID} limits={owner && draft && hasBoard(draft) ? <SoundboardLimits draft={draft} base={base} onEdit={edit} /> : null} />;
    } else if (at === 'emoji') {
        panel = <ManageEmoji denID={denID} limits={owner && draft && hasEmoji(draft) ? <EmojiLimits draft={draft} base={base} onEdit={edit} /> : null} />;
    } else if (at === 'events') {
        panel = ready(draft && hasEvents(draft) && <EventLimits draft={draft} base={base} onEdit={edit} />);
    } else if (at === 'limits') {
        panel = ready(<Limits denID={denID} view={view} draft={draft} base={base} onEdit={edit} />);
    } else {
        panel = <General den={den} draft={owner ? draft : null} base={base} onEdit={edit} />;
    }
    const bar = owner && (dirty || saved || leaving) && (
        <div role="status" class="flex flex-wrap items-center gap-2 border-t border-base-300 bg-base-200 px-4 py-2 max-md:pb-[max(0.5rem,env(safe-area-inset-bottom))]">
            {leaving ? (
                <>
                    <span class="min-w-0 flex-1 text-sm">Discard your unsaved changes?</span>
                    <Button kind="danger" filled onClick={onClose}>Discard</Button>
                    <Button kind="quiet" onClick={() => setLeaving(false)}>Keep editing</Button>
                </>
            ) : dirty ? (
                <>
                    <span class="min-w-0 flex-1 text-sm">{save.error ? <StatusText kind="error" inline>{save.error}</StatusText> : 'You have unsaved changes.'}</span>
                    <Button kind="primary" busy={save.busy} onClick={saveAll}>Save changes</Button>
                    <Button kind="quiet" disabled={save.busy} onClick={discard}>Discard</Button>
                </>
            ) : (
                <StatusText kind="success">Saved.</StatusText>
            )}
        </div>
    );
    return (
        <SectionsFrame label={`Manage ${den.name}`} title={den.name} groups={[['', sections]]} at={at} narrow={narrow} bar={bar}
            onPick={onSection} onList={toList} onClose={close}>
            {panel}
        </SectionsFrame>
    );
}

// Copyable is a value to copy: the den's address or its ID.
function Copyable({ text, mono }) {
    return (
        <div class="flex items-center gap-2">
            <span class={`min-w-0 flex-1 truncate rounded bg-base-100 px-3 py-2 text-sm select-all max-md:bg-base-200 ${mono ? 'font-mono' : ''}`}>{text}</span>
            <CopyButton text={text} />
        </div>
    );
}

// General shows where the den is and its ID, which members compare, and
// lets its owner change its name and public address and test it.
function General({ den, draft, base, onEdit }) {
    const [reached, setReached] = useState(false);
    const check = useAction();
    const moved = draft && draft.url.trim() !== base.url;
    function test() {
        setReached(false);
        check.run(async () => {
            await api.post(`/api/dens/${den.den_id}/check`);
            setReached(true);
        });
    }
    return (
        <>
            {den.state === 'offline' && !den.own && <NewAddress den={den} />}
            <Card>
                {draft ? (
                    <>
                        <Field label="Den name">
                            <TextInput value={draft.name} onInput={(v) => onEdit({ name: v })} required maxlength="32" />
                        </Field>
                        <Field label={<>Public address{moved && <> <Tip id="moved-tip">Members who were offline, and invites already sent, find the new address through the old one.</Tip></>}</>}
                            hint={moved ? 'Keep the old address pointing at the den for a while.' : 'The address members use to connect.'}>
                            <div class="flex gap-2">
                                <TextInput type="url" value={draft.url} onInput={(v) => onEdit({ url: v })} required {...PLAIN} />
                                <Button size="md" busy={check.busy} disabled={moved} title={moved ? 'Save the new address first' : undefined} onClick={test}>Test</Button>
                            </div>
                        </Field>
                        {reached && <StatusText kind="success">This device reached the den at {den.url}, and it proved its identity there.</StatusText>}
                        {check.error && (
                            <>
                                <ErrorText message={check.error} />
                                {den.own && <p class="text-sm text-base-content/70">Your router may not reach itself from inside; test from a phone.</p>}
                            </>
                        )}
                    </>
                ) : (
                    <Field label="Address">
                        <Copyable text={den.url} />
                    </Field>
                )}
                <Field label={<>Den ID <Tip id={`den-id-${den.den_id}`} side="right">The start of the den's identity. Everyone in this den sees the same one.</Tip></>}>
                    <Copyable text={den.fingerprint} mono />
                </Field>
                {den.retention > 0 && <p class="text-sm text-base-content/70">Messages here are deleted after {keptFor(den.retention)}.</p>}
            </Card>
        </>
    );
}

// NewAddress gives a den that can't be reached the new address its owner
// moved it to. The service uses it only if the den there proves it's the
// same one.
function NewAddress({ den }) {
    const [url, setURL] = useState('');
    const act = useAction();
    function submit(e) {
        e.preventDefault();
        act.run(async () => {
            await api.post(`/api/dens/${den.den_id}/address`, { url });
            setURL('');
        });
    }
    return (
        <Card title="Den moved?">
            <form class="flex flex-col gap-2" onSubmit={submit}>
                <p class="text-sm text-base-content/70">Enter the new address the owner gave it.</p>
                <p class="text-sm text-base-content/70">Dens uses it only if the den there proves it's the same one.</p>
                <div class="flex gap-2">
                    <TextInput type="url" value={url} onInput={setURL} placeholder="https://den.example.com" autocomplete="off" required {...PLAIN} />
                    <SubmitButton busy={act.busy} size="md">Use it</SubmitButton>
                </div>
                <ErrorText message={act.error} />
            </form>
        </Card>
    );
}

const EXPIRIES = [
    [3600, '1 hour'],
    [86400, '1 day'],
    [7 * 86400, '7 days'],
    [30 * 86400, '30 days'],
];

// Invites makes invites to the den, shows a new one once to copy, and
// lists the ones still open, to revoke.
function Invites({ denID }) {
    const [expiresIn, setExpiresIn] = useState(86400);
    const [maxUses, setMaxUses] = useState('1');
    const [created, setCreated] = useState(null);
    const [list, setList] = useState(null);
    const create = useAction();
    const manage = useAction();
    const refresh = async () => setList((await api.get(`/api/dens/${denID}/invites`)).invites);
    useEffect(() => {
        manage.run(refresh);
    }, [denID]);

    function submit(e) {
        e.preventDefault();
        create.run(async () => {
            const data = await api.post(`/api/dens/${denID}/invites`, { expires_in: Number(expiresIn), max_uses: Number(maxUses) });
            setCreated(data.invite);
            await refresh();
        });
    }

    return (
        <>
            <Card title="New invite">
                <form class="flex flex-wrap items-end gap-3" onSubmit={submit}>
                    <Field label="Expires after">
                        <select class="select" value={expiresIn} onChange={(e) => setExpiresIn(e.currentTarget.value)}>
                            {EXPIRIES.map(([seconds, label]) => <option key={seconds} value={seconds}>{label}</option>)}
                        </select>
                    </Field>
                    <Field label="Max uses">
                        <input class="input w-24" type="number" inputmode="numeric" min="1" max="100" step="1" value={maxUses}
                            onInput={(e) => setMaxUses(e.currentTarget.value)} required />
                    </Field>
                    <SubmitButton busy={create.busy} size="md" class="mb-1">Create invite</SubmitButton>
                </form>
                <ErrorText message={create.error} />
                {created && (
                    <div class="flex flex-col gap-2">
                        <p class="text-sm">Send this to the people you're inviting. It's shown only once.</p>
                        <Copyable text={created} mono />
                    </div>
                )}
            </Card>
            <Card title={`Active invites${list ? ` (${list.length})` : ''}`}>
                {list === null ? (
                    !manage.error && <span class="loading loading-dots loading-sm"></span>
                ) : list.length === 0 ? (
                    <p class="text-sm text-base-content/70">None.</p>
                ) : (
                    <ul class="flex flex-col divide-y divide-base-300 text-sm">
                        {list.map((inv) => (
                            <li key={inv.id} class="flex items-center gap-2 py-1.5">
                                <span class="min-w-0 flex-1">Used {inv.uses} of {inv.max_uses} · Expires {dayTime(inv.expires_at)}</span>
                                <Button kind="danger" size="xs" disabled={manage.busy} onClick={() => manage.run(async () => {
                                    await api.del(`/api/dens/${denID}/invites/${inv.id}`);
                                    await refresh();
                                })}>
                                    Revoke
                                </Button>
                            </li>
                        ))}
                    </ul>
                )}
                <ErrorText message={manage.error} />
            </Card>
        </>
    );
}

// Bans lists who can't come back, and lifts the bans placed by staff of
// the member's rank or below (M8).
function Bans({ denID, role }) {
    const [list, setList] = useState(null);
    const act = useAction();
    const load = async () => setList((await api.get(`/api/dens/${denID}/bans`)).bans);
    useEffect(() => {
        act.run(load);
    }, [denID]);
    return (
        <Card title={<>Banned{' '}
            <Tip id="bans-tip" side="right">
                A ban signs them out at once and keeps their username out, even with a new invite. Unbanning doesn't bring them back:
                they can join again with an invite.
            </Tip></>}>
            <ErrorText message={act.error} />
            {list === null ? (
                !act.error && <span class="loading loading-dots loading-sm"></span>
            ) : list.length === 0 ? (
                <p class="text-sm text-base-content/70">Nobody is banned.</p>
            ) : (
                <ul class="flex flex-col divide-y divide-base-300">
                    {list.map((b) => (
                        <li key={b.member.id} class="flex items-center gap-2 py-1.5 text-sm">
                            <span class="min-w-0 flex-1 truncate">
                                <span class="font-medium">{b.member.display_name}</span> (@{b.member.username}) · Banned {day(b.banned_at)}
                            </span>
                            {rank(role) >= rank(b.banned_by_role) && (
                                <Button kind="quiet" size="xs" disabled={act.busy} onClick={() => act.run(async () => {
                                    await api.del(`/api/dens/${denID}/bans/${b.member.id}`);
                                    await load();
                                })}>
                                    Unban
                                </Button>
                            )}
                        </li>
                    ))}
                </ul>
            )}
        </Card>
    );
}

// Unit is a number field with its unit inside it, which asks a phone's
// keyboard for digits, and a point where its step has a fraction.
function Unit({ unit, value, onInput, ...rest }) {
    return (
        <label class="input w-full">
            <input type="number" inputmode={Number.isInteger(Number(rest.step)) ? 'numeric' : 'decimal'} class="grow" value={value}
                onInput={(e) => onInput(e.currentTarget.value)} {...rest} />
            <span class="label">{unit}</span>
        </label>
    );
}

// SoundboardLimits are the owner's limits for the den's soundboard, in its
// section: whether it plays at all, how many sounds it keeps, how long one
// may be, and how long a member waits between theirs.
function SoundboardLimits({ draft, base, onEdit }) {
    const on = Number(draft.sounds) > 0;
    const field = (key) => (value) => onEdit({ [key]: value });
    return (
        <Card title="Limits" data-limits-soundboard>
            <Toggle id="allow-soundboard" label="Allow the soundboard" checked={on}
                tip="The den streams each sound to the whole call, so a long one costs its upload as long as it plays."
                onChange={(yes) => onEdit({ sounds: yes ? String(Number(base.sounds) || 50) : '0' })} />
            {on && (
                <div class="grid gap-x-3 sm:grid-cols-3">
                    <Field label="Max sounds">
                        <Unit unit="sounds" min="1" max="500" step="1" value={draft.sounds} onInput={field('sounds')} required />
                    </Field>
                    <Field label="Longest sound">
                        <Unit unit="seconds" min="1" max="300" step="1" value={draft.longest} onInput={field('longest')} required />
                    </Field>
                    <Field label="Wait between sounds">
                        <Unit unit="seconds" min="0" max="600" step="1" value={draft.wait} onInput={field('wait')} required />
                    </Field>
                </div>
            )}
            <p class="text-sm text-base-content/70">Lowering these leaves the sounds already added.</p>
        </Card>
    );
}

// EmojiLimits are the owner's limit for the den's emoji, in their
// section.
function EmojiLimits({ draft, base, onEdit }) {
    const on = Number(draft.emojis) > 0;
    return (
        <Card title="Limits" data-limits-emoji>
            <Toggle id="allow-emoji" label="Allow emoji" checked={on}
                onChange={(yes) => onEdit({ emojis: yes ? String(Number(base.emojis) || 50) : '0' })} />
            {on && (
                <div class="grid gap-x-3 sm:grid-cols-3">
                    <Field label="Max emoji">
                        <Unit unit="emoji" min="1" max="500" step="1" value={draft.emojis} onInput={(v) => onEdit({ emojis: v })} required />
                    </Field>
                </div>
            )}
            <p class="text-sm text-base-content/70">Lowering it or turning emoji off keeps the emoji already added.</p>
        </Card>
    );
}

// EventLimits is the owner's limit for members' events (M17.4): staff
// always make them, and members may too, each this many at a time.
function EventLimits({ draft, base, onEdit }) {
    const on = Number(draft.memberEvents) > 0;
    return (
        <Card title="Who makes events" data-limits-events>
            <Toggle id="member-events" label="Members make events" hint="Staff always can." checked={on} data-member-events
                onChange={(yes) => onEdit({ memberEvents: yes ? String(Number(base.memberEvents) || 1) : '0' })} />
            {on && (
                <div class="grid gap-x-3 sm:grid-cols-3">
                    <Field label="Events per member">
                        <Unit unit="events" min="1" max="10" step="1" value={draft.memberEvents} onInput={(v) => onEdit({ memberEvents: v })} required />
                    </Field>
                </div>
            )}
            <p class="text-sm text-base-content/70">Lowering this leaves the events already made.</p>
        </Card>
    );
}

// RETAIN_DAYS are the periods Auto-delete offers, up to the longest a den
// keeps messages.
export const RETAIN_DAYS = [1, 7, 14, 30, 90, 180, 365, 730, 1825, MAX_RETENTION];

// retainChoices are Auto-delete's periods with their names, and the den's
// own among them where it's none of them.
export function retainChoices(days) {
    const list = RETAIN_DAYS.includes(days) || !Number.isInteger(days) || days < 1 ? RETAIN_DAYS : [...RETAIN_DAYS, days].sort((a, b) => a - b);
    return list.map((d) => [d, d >= 365 && d % 365 === 0 ? (d === 365 ? '1 year' : `${d / 365} years`) : keptFor(d)]);
}

const PRESET_NAMES = { small: 'Small group', large: 'Large group' };

// Limits sets how large a file may be and how much space files take, how
// long the den keeps messages, and its limits for calls, from a preset or
// field by field, with the upload they need as they change.
function Limits({ denID, view, draft, base, onEdit }) {
    const [custom, setCustom] = useState(() => presetOf(callLimitsOf(draft)) === 'custom');
    const [preview, setPreview] = useState(null);
    const rates = view.channels.filter((c) => c.kind === 'voice').map((c) => c.bitrate);
    const voice = rates.length ? Math.max(...rates) : 96000;
    const calls = callLimitsOf(draft);
    const preset = custom ? 'custom' : presetOf(calls);
    const days = Number(draft.days);
    const was = base.retain ? Number(base.days) : 0;
    // The den counts what a new period would delete at once, as the owner
    // picks one.
    useEffect(() => {
        setPreview(null);
        if (!draft.retain || !Number.isInteger(days) || days < 1 || days > MAX_RETENTION || days === was) return undefined;
        let stale = false;
        const timer = setTimeout(() => {
            api.get(`/api/dens/${denID}/retention?days=${days}`).then((p) => !stale && setPreview(p), () => {});
        }, 300);
        return () => {
            stale = true;
            clearTimeout(timer);
        };
    }, [draft.retain, draft.days]);
    const field = (key) => (value) => onEdit({ [key]: value });
    const sharing = Number(draft.shares) > 0;
    const board = hasBoard(draft) && Number(draft.sounds) > 0;
    const presets = [
        ...Object.entries(CALL_PRESETS).map(([name, limits]) => [name, PRESET_NAMES[name], `About ${mbps(uploadNeeded(limits, voice, board))}`, limits]),
        ['custom', 'Custom', 'Set each', null],
    ];
    return (
        <>
            <Card title="Uploads">
                <div class="grid gap-x-3 sm:grid-cols-3">
                    <Field label="Max file size">
                        <Unit unit="MB" min="1" max="1024" step="1" value={draft.file} onInput={field('file')} required />
                    </Field>
                    <Field label="Storage per member">
                        <Unit unit="GB" min="0.01" step="0.01" value={draft.member} onInput={field('member')} required />
                    </Field>
                    <Field label="Total storage">
                        <Unit unit="GB" min="0.01" step="0.01" value={draft.den} onInput={field('den')} required />
                    </Field>
                </div>
            </Card>
            <Card title="Messages">
                <Toggle id="auto-delete" label="Auto-delete messages" hint="In channels and DMs. Members see the period." checked={draft.retain}
                    onChange={(on) => onEdit({ retain: on })} />
                {draft.retain && (
                    <Field label="After">
                        <select class="select w-full sm:w-48" value={draft.days} aria-label="Days to keep messages" onChange={(e) => onEdit({ days: e.currentTarget.value })}>
                            {retainChoices(days).map(([d, label]) => <option key={d} value={String(d)}>{label}</option>)}
                        </select>
                    </Field>
                )}
                {preview && <p class="text-sm">{deletesNow(preview)}</p>}
            </Card>
            <Card title="Calls">
                <p class="text-sm text-base-content/70">Calls use the host's upload. Lower these if they stutter.</p>
                <div role="radiogroup" aria-label="Call limits" class="grid grid-cols-3 gap-2">
                    {presets.map(([name, title, upload, limits]) => (
                        <label key={name} class={TILE} data-preset={name}>
                            <input type="radio" class="sr-only" name="call-preset" checked={preset === name} onChange={() => {
                                setCustom(!limits);
                                if (limits) onEdit(callFields(limits));
                            }} />
                            <span class="text-sm font-medium">{title}</span>
                            <span class="text-xs text-base-content/70">{upload}</span>
                        </label>
                    ))}
                </div>
                {preset === 'custom' && (
                    <div class="grid gap-x-3 sm:grid-cols-2">
                        <Field label="Max people per call">
                            <Unit unit="people" min="2" max="30" step="1" value={draft.members} onInput={field('members')} required />
                        </Field>
                        <Field label="Max people in calls (whole den)">
                            <Unit unit="people" min="2" max="100" step="1" value={draft.callers} onInput={field('callers')} required />
                        </Field>
                        <div class="mt-2 sm:col-span-2">
                            <Toggle id="allow-sharing" label="Allow screen sharing" checked={sharing}
                                onChange={(on) => onEdit({ shares: on ? String(Number(base.shares) || 1) : '0' })} />
                        </div>
                        {sharing && (
                            <>
                                <Field label="Max shares at once">
                                    <Unit unit="shares" min="1" max="10" step="1" value={draft.shares} onInput={field('shares')} required />
                                </Field>
                                <Field label="Max viewers per share">
                                    <Unit unit="viewers" min="1" max="29" step="1" value={draft.viewers} onInput={field('viewers')} required />
                                </Field>
                                <Field label="Max bitrate">
                                    <Unit unit="Mbps" min="0.25" max="50" step="0.25" value={draft.bitrate} onInput={field('bitrate')} required />
                                </Field>
                                <Field label="Max resolution">
                                    <select class="select w-full" value={draft.height} onChange={(e) => onEdit({ height: e.currentTarget.value })}>
                                        {HEIGHTS.map((h) => <option key={h} value={String(h)}>{h === 2160 ? '4K (2160p)' : `${h}p`}</option>)}
                                    </select>
                                </Field>
                                <Field label="Max frame rate">
                                    <Unit unit="fps" min="5" max="60" step="1" value={draft.fps} onInput={field('fps')} required />
                                </Field>
                            </>
                        )}
                    </div>
                )}
                {Object.values(calls).every(Number.isFinite) && (
                    <p class="text-sm">Upload needed at full load: about {mbps(uploadNeeded(calls, voice, board))}</p>
                )}
            </Card>
        </>
    );
}
