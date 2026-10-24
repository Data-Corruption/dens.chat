// A den's Manage dialog (M8.3), in two parts by who they're for. This den:
// its address and Den ID, which everyone sees, and the name and address
// its owner changes; invites and bans, for staff; and the owner's limits.
// You in this den: the member's devices, their den password, recovery
// codes and DM recovery key, and their files. The owner's edits gather in
// one draft, saved from one bar.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { progressKey, progressLabel, sendSmaller } from './compare.jsx';
import { CopyButton, DocsTip, ErrorText, Field, PasswordFields, SubmitButton, TextInput, Tip, checkPasswords, day, dayTime, useAction, useLater } from './components.jsx';
import { MAX_RETENTION, callFields, callLimitsOf, changed, changesOf, draftOf } from './dendraft.js';
import { onEvent } from './events.js';
import { addPreview, fileWhere, formatSize, needsPreview, thumbURL, upload } from './files.jsx';
import { openAt } from './messages.jsx';
import { leaveOffer } from './offline.js';
import { rank } from './people.jsx';
import { DMKey, RecoveryCodes } from './private.jsx';
import { deletesNow, keptFor } from './retention.js';
import { CALL_PRESETS, HEIGHTS, mbps, presetOf, uploadNeeded } from './share.js';

// sectionsFor lists the sections a member sees for a den, each with the
// part it's in: everything but General needs the den connected.
export function sectionsFor(den) {
    const list = [['general', 'General', 'den']];
    if (den.state !== 'connected') return list;
    if (rank(den.role) > 0) list.push(['invites', 'Invites', 'den'], ['bans', 'Bans', 'den']);
    if (den.role === 'owner') list.push(['limits', 'Limits', 'den']);
    list.push(['devices', 'Devices', 'you'], ['password', 'Password and recovery', 'you'], ['files', 'Your files', 'you']);
    return list;
}

// ManageDialog manages one den, at a section. It closes with its ✕,
// Escape or a click outside it, asking first when edits aren't saved.
export function ManageDialog({ denID, section, onSection, onClose, navigate }) {
    const [den, setDen] = useState(null);
    const [view, setView] = useState(null);
    const [base, setBase] = useState(null);
    const [draft, setDraft] = useState(null);
    const [shown, setShown] = useState(null);
    const [leaving, setLeaving] = useState(false);
    const [saved, setSaved] = useState(false);
    const save = useAction();

    // The den's status on this computer comes with every change to the
    // list; its state, which the owner's settings and the files need,
    // loads once it's connected.
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
    useEffect(() => {
        const escape = (e) => e.key === 'Escape' && close();
        document.addEventListener('keydown', escape);
        return () => document.removeEventListener('keydown', escape);
    });

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
            <Frame title="Manage" onClose={onClose}>
                <div class="p-4"><span class="loading loading-spinner"></span></div>
            </Frame>
        );
    }
    const sections = sectionsFor(den);
    const at = sections.find(([id]) => id === section) ? section : section === 'leave' && !den.own ? 'leave' : 'general';
    const showCodes = (codes) => setShown({ codes, seal: '', sealNew: false });
    const showKey = (seal) => setShown({ codes: [], seal, sealNew: true });
    let panel;
    if (shown) {
        panel = <RecoveryCodes denName={den.name} codes={shown.codes} seal={shown.seal} sealNew={shown.sealNew} onDone={() => setShown(null)} />;
    } else if (at === 'invites') {
        panel = <Invites denID={denID} />;
    } else if (at === 'bans') {
        panel = <Bans denID={denID} role={den.role} />;
    } else if (at === 'limits') {
        panel = draft && view ? <Limits denID={denID} view={view} draft={draft} base={base} onEdit={edit} /> : <span class="loading loading-spinner"></span>;
    } else if (at === 'devices') {
        panel = <Devices den={den} />;
    } else if (at === 'password') {
        panel = <PasswordAndRecovery den={den} onCodes={showCodes} onKey={showKey} />;
    } else if (at === 'files') {
        panel = <Files den={den} navigate={(to) => {
            onClose();
            navigate(to);
        }} />;
    } else if (at === 'leave') {
        panel = <Leave den={den} onLeft={() => {
            onClose();
            navigate('/');
        }} />;
    } else {
        panel = <General den={den} draft={owner ? draft : null} base={base} onEdit={edit} />;
    }
    const item = ([id, label]) => (
        <button key={id} type="button" aria-current={at === id ? 'page' : undefined}
            class={`btn btn-ghost btn-sm shrink-0 justify-start font-normal ${at === id ? 'btn-active' : ''}`}
            onClick={() => {
                setShown(null);
                onSection(id);
            }}>
            {label}
        </button>
    );
    const group = (name) => <p class="hidden px-3 pt-2 text-xs font-semibold uppercase text-base-content/60 md:block">{name}</p>;
    const yours = sections.filter(([, , part]) => part === 'you');
    return (
        <Frame title={den.name} onClose={close}>
            <div class="flex min-h-0 flex-1 flex-col md:flex-row">
                <nav aria-label="Sections" class="flex shrink-0 gap-1 overflow-x-auto border-b border-base-300 p-2 md:w-52 md:flex-col md:border-b-0 md:border-r">
                    {group('This den')}
                    {sections.filter(([, , part]) => part === 'den').map(item)}
                    {yours.length > 0 && group('You in this den')}
                    {yours.map(item)}
                    {!den.own && (
                        <button type="button" class={`btn btn-ghost btn-sm shrink-0 justify-start font-normal text-error md:mt-auto ${at === 'leave' ? 'btn-active' : ''}`}
                            onClick={() => onSection('leave')}>
                            Leave den
                        </button>
                    )}
                </nav>
                <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                    <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">{panel}</div>
                    {owner && (dirty || saved || leaving) && (
                        <div role="status" class="flex flex-wrap items-center gap-2 border-t border-base-300 bg-base-200 px-4 py-2">
                            {leaving ? (
                                <>
                                    <span class="min-w-0 flex-1 text-sm">Discard your unsaved changes?</span>
                                    <button type="button" class="btn btn-ghost btn-sm" onClick={() => setLeaving(false)}>Keep editing</button>
                                    <button type="button" class="btn btn-error btn-sm" onClick={onClose}>Discard</button>
                                </>
                            ) : dirty ? (
                                <>
                                    <span class="min-w-0 flex-1 text-sm">{save.error ? <span class="text-error">{save.error}</span> : 'You have unsaved changes.'}</span>
                                    <button type="button" class="btn btn-ghost btn-sm" disabled={save.busy} onClick={discard}>Discard</button>
                                    <button type="button" class="btn btn-primary btn-sm" disabled={save.busy} onClick={saveAll}>
                                        {save.busy && <span class="loading loading-spinner loading-sm"></span>}
                                        Save changes
                                    </button>
                                </>
                            ) : (
                                <span class="text-sm text-success">Saved.</span>
                            )}
                        </div>
                    )}
                </div>
            </div>
        </Frame>
    );
}

function Frame({ title, onClose, children }) {
    return (
        <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={(e) => e.target === e.currentTarget && onClose()}>
            <div role="dialog" aria-modal="true" aria-label={`Manage ${title}`} class="card flex h-full max-h-[48rem] w-full max-w-3xl flex-col bg-base-100 shadow-xl">
                <div class="flex items-center gap-2 border-b border-base-300 px-4 py-3">
                    <h2 class="card-title min-w-0 flex-1 truncate">{title}</h2>
                    <button type="button" class="btn btn-ghost btn-sm" onClick={onClose} aria-label="Close">✕</button>
                </div>
                {children}
            </div>
        </div>
    );
}

function Heading({ children }) {
    return <h3 class="text-lg font-semibold">{children}</h3>;
}

// Copyable is a value to copy: the den's address or its ID.
function Copyable({ text, mono }) {
    return (
        <div class="flex items-center gap-2">
            <span class={`min-w-0 flex-1 truncate rounded bg-base-200 px-3 py-2 text-sm select-all ${mono ? 'font-mono' : ''}`}>{text}</span>
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
            <Heading>General</Heading>
            {den.state === 'offline' && !den.own && <NewAddress den={den} />}
            {draft ? (
                <>
                    <Field label="Den name">
                        <TextInput value={draft.name} onInput={(v) => onEdit({ name: v })} required maxlength="32" />
                    </Field>
                    <Field label="Public address"
                        hint={moved
                            ? 'Keep the old address pointing at the den for a while: members who were offline, and invites already sent, find the new one through it.'
                            : 'The address members use to connect.'}>
                        <div class="flex gap-2">
                            <TextInput type="url" value={draft.url} onInput={(v) => onEdit({ url: v })} required />
                            <button type="button" class="btn" onClick={test} disabled={check.busy || moved} title={moved ? 'Save the new address first' : undefined}>
                                {check.busy && <span class="loading loading-spinner loading-sm"></span>}
                                Test
                            </button>
                        </div>
                    </Field>
                    {reached && <p class="text-sm text-success">This computer reached the den at {den.url}, and it proved its identity there.</p>}
                    {check.error && (
                        <>
                            <ErrorText message={check.error} />
                            {den.own && (
                                <p class="text-sm text-base-content/70">
                                    Some routers can't reach their own public address from inside the network, so members may still get
                                    through. If you're unsure, test from a phone or another network.
                                </p>
                            )}
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
        <form class="flex flex-col gap-2 rounded-box bg-base-200 p-3" onSubmit={submit}>
            <p class="font-medium">Den moved?</p>
            <p class="text-sm text-base-content/70">If the owner gave {den.name} a new address, enter it. Dens only uses it if the den there proves it's the same one.</p>
            <div class="flex gap-2">
                <TextInput value={url} onInput={setURL} placeholder="https://den.example.com" autocomplete="off" required />
                <SubmitButton busy={act.busy}>Use it</SubmitButton>
            </div>
            <ErrorText message={act.error} />
        </form>
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
            <Heading>Invites</Heading>
            <form class="flex flex-wrap items-end gap-3" onSubmit={submit}>
                <Field label="Expires after">
                    <select class="select" value={expiresIn} onChange={(e) => setExpiresIn(e.currentTarget.value)}>
                        {EXPIRIES.map(([seconds, label]) => <option key={seconds} value={seconds}>{label}</option>)}
                    </select>
                </Field>
                <Field label="Max uses">
                    <input class="input w-24" type="number" min="1" max="100" step="1" value={maxUses} onInput={(e) => setMaxUses(e.currentTarget.value)}
                        required />
                </Field>
                <SubmitButton busy={create.busy}>Create invite</SubmitButton>
            </form>
            <ErrorText message={create.error} />
            {created && (
                <div class="flex flex-col gap-2">
                    <p class="text-sm">Send this to the people you're inviting. It's shown only once.</p>
                    <Copyable text={created} mono />
                </div>
            )}
            <div class="flex flex-col gap-2">
                <h4 class="font-medium">Active invites{list ? ` (${list.length})` : ''}</h4>
                {list === null ? (
                    !manage.error && <span class="loading loading-dots loading-sm"></span>
                ) : list.length === 0 ? (
                    <p class="text-sm text-base-content/70">None.</p>
                ) : (
                    <ul class="flex flex-col divide-y divide-base-300 text-sm">
                        {list.map((inv) => (
                            <li key={inv.id} class="flex items-center gap-2 py-1.5">
                                <span class="min-w-0 flex-1">Used {inv.uses} of {inv.max_uses} · Expires {dayTime(inv.expires_at)}</span>
                                <button type="button" class="btn btn-ghost btn-xs" disabled={manage.busy} onClick={() => manage.run(async () => {
                                    await api.del(`/api/dens/${denID}/invites/${inv.id}`);
                                    await refresh();
                                })}>
                                    Revoke
                                </button>
                            </li>
                        ))}
                    </ul>
                )}
                <ErrorText message={manage.error} />
            </div>
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
        <>
            <Heading>
                Bans{' '}
                <Tip id="bans-tip" side="right">
                    A ban signs them out at once and keeps their username out, even with a new invite. Unbanning doesn't bring them back:
                    they can join again with an invite.
                </Tip>
            </Heading>
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
                                <button type="button" class="btn btn-ghost btn-xs" disabled={act.busy} onClick={() => act.run(async () => {
                                    await api.del(`/api/dens/${denID}/bans/${b.member.id}`);
                                    await load();
                                })}>
                                    Unban
                                </button>
                            )}
                        </li>
                    ))}
                </ul>
            )}
        </>
    );
}

// Unit is a number field with its unit inside it.
function Unit({ unit, value, onInput, ...rest }) {
    return (
        <label class="input w-full">
            <input type="number" class="grow" value={value} onInput={(e) => onInput(e.currentTarget.value)} {...rest} />
            <span class="label">{unit}</span>
        </label>
    );
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
    // types one.
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
    return (
        <>
            <Heading>Limits</Heading>
            <section class="flex flex-col gap-2">
                <h4 class="font-medium">Uploads</h4>
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
            </section>
            <section class="flex flex-col gap-2 border-t border-base-300 pt-3">
                <h4 class="font-medium">Messages</h4>
                <div class="flex flex-wrap items-center gap-2">
                    <input id="auto-delete" type="checkbox" class="toggle" checked={draft.retain} onChange={(e) => onEdit({ retain: e.currentTarget.checked })} />
                    <label for="auto-delete" class="cursor-pointer">Auto-delete messages after</label>
                    <input type="number" class="input input-sm w-20" min="1" max={String(MAX_RETENTION)} step="1" value={draft.days}
                        disabled={!draft.retain} aria-label="Days to keep messages" onInput={(e) => onEdit({ days: e.currentTarget.value })} />
                    <span>days</span>
                </div>
                <p class="text-sm text-base-content/70">Applies to channels and DMs. Members can see this.</p>
                {preview && <p class="text-sm">{deletesNow(preview)}</p>}
            </section>
            <section class="flex flex-col gap-2 border-t border-base-300 pt-3">
                <h4 class="font-medium">Calls</h4>
                <p class="text-sm text-base-content/70">Calls and screen shares pass through this computer's network. Lower these if calls stutter.</p>
                <div role="radiogroup" aria-label="Call limits" class="flex flex-wrap gap-4">
                    {Object.entries(CALL_PRESETS).map(([name, limits]) => (
                        <label key={name} class="label gap-2">
                            <input type="radio" class="radio radio-sm" name="call-preset" checked={preset === name} onChange={() => {
                                setCustom(false);
                                onEdit(callFields(limits));
                            }} />
                            {PRESET_NAMES[name]} (about {mbps(uploadNeeded(limits, voice))})
                        </label>
                    ))}
                    <label class="label gap-2">
                        <input type="radio" class="radio radio-sm" name="call-preset" checked={preset === 'custom'} onChange={() => setCustom(true)} />
                        Custom
                    </label>
                </div>
                {preset === 'custom' && (
                    <div class="grid gap-x-3 sm:grid-cols-2">
                        <Field label="Max people per call">
                            <Unit unit="people" min="2" max="30" step="1" value={draft.members} onInput={field('members')} required />
                        </Field>
                        <Field label="Max people in calls (whole den)">
                            <Unit unit="people" min="2" max="100" step="1" value={draft.callers} onInput={field('callers')} required />
                        </Field>
                        <div class="sm:col-span-2">
                            <label class="label mt-2 gap-2">
                                <input type="checkbox" class="toggle toggle-sm" checked={sharing}
                                    onChange={(e) => onEdit({ shares: e.currentTarget.checked ? String(Number(base.shares) || 1) : '0' })} />
                                Allow screen sharing
                            </label>
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
                    <p class="text-sm">Upload needed at full load: about {mbps(uploadNeeded(calls, voice))}</p>
                )}
            </section>
        </>
    );
}

// Devices lists this member's devices in the den and signs any of them
// out, after saying what that does.
function Devices({ den }) {
    const [list, setList] = useState(null);
    const [confirm, setConfirm] = useState('');
    const act = useAction();
    const load = async () => setList(await api.get(`/api/dens/${den.den_id}/devices`));
    const slow = useLater(list === null && !act.error, 1000);
    useEffect(() => {
        act.run(load);
    }, [den.den_id]);

    function signOut(d) {
        act.run(async () => {
            await api.del(`/api/dens/${den.den_id}/devices/${d.key_id}`);
            setConfirm('');
            if (!d.current) await load();
        });
    }

    return (
        <>
            <Heading>Devices</Heading>
            <ErrorText message={act.error} />
            {list === null ? (
                slow && <span class="loading loading-dots loading-sm"></span>
            ) : (
                <ul class="flex flex-col divide-y divide-base-300">
                    {list.devices.map((d) => (
                        <li key={d.key_id} class="flex flex-col gap-2 py-2 text-sm">
                            <div class="flex flex-wrap items-center gap-2">
                                <span class="min-w-0 flex-1">
                                    <span class="font-medium">{d.label}</span>
                                    {d.current && <span class="badge badge-soft badge-success badge-sm ml-2">This device</span>}
                                    <span class="block text-xs text-base-content/60">Added {day(d.created_at)} · last signed in {day(d.last_seen_at)}</span>
                                </span>
                                {confirm !== d.key_id && (
                                    <button type="button" class="btn btn-ghost btn-xs text-error" onClick={() => setConfirm(d.key_id)}>Sign out</button>
                                )}
                            </div>
                            {confirm === d.key_id && (
                                <div class="flex flex-col gap-2 rounded-box bg-base-200 p-3">
                                    <p>
                                        {d.current ? 'Sign this device out?' : `Sign out ${d.label}?`} It loses access at once. Signing in again takes your
                                        den password and approval from another of your devices.
                                    </p>
                                    {list.devices.length === 1 && (
                                        <p class="text-warning">
                                            It's your only device here, so signing in again takes a recovery code, and your DM recovery key to read
                                            your direct messages.
                                        </p>
                                    )}
                                    <div class="flex gap-2">
                                        <button type="button" class="btn btn-error btn-sm" disabled={act.busy} onClick={() => signOut(d)}>Sign out</button>
                                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => setConfirm('')}>Cancel</button>
                                    </div>
                                </div>
                            )}
                        </li>
                    ))}
                </ul>
            )}
        </>
    );
}

// signedOut says how many of the member's other devices a new den password
// signed out.
function signedOut(n) {
    if (!n) return '';
    return n === 1 ? ' 1 other device was signed out.' : ` ${n} other devices were signed out.`;
}

// PasswordAndRecovery changes the member's den password, makes new
// recovery codes, and shows or resets their DM recovery key.
function PasswordAndRecovery({ den, onCodes, onKey }) {
    const [left, setLeft] = useState(null);
    const load = () => api.get(`/api/dens/${den.den_id}/devices`).then((d) => setLeft(d.recovery_codes_left), () => {});
    useEffect(() => {
        load();
    }, [den.den_id]);
    return (
        <>
            <Heading>
                Password and recovery{' '}
                <DocsTip id={`docs-recovery-${den.den_id}`} page="join/#your-passwords-and-keys">
                    What your den password, recovery codes and DM recovery key are each for.</DocsTip>
            </Heading>
            <section class="flex flex-col gap-2">
                <h4 class="font-medium">Den password</h4>
                <ChangePassword den={den} onDone={load} />
            </section>
            <section class="flex flex-col gap-2 border-t border-base-300 pt-3">
                <h4 class="font-medium">Recovery codes</h4>
                {left !== null && <NewCodes den={den} left={left} onCodes={(codes) => {
                    load();
                    onCodes(codes);
                }} />}
            </section>
            <section class="flex flex-col gap-2 border-t border-base-300 pt-3">
                <h4 class="font-medium">DM recovery key</h4>
                <DMKey den={den} onSeal={onKey} />
            </section>
        </>
    );
}

function useForm(initial) {
    const [form, setForm] = useState(initial);
    return [form, (key, value) => setForm((f) => ({ ...f, [key]: value })), setForm];
}

// ChangePassword sets the member's den password, with the current one or a
// recovery code, which signs out their other devices.
function ChangePassword({ den, onDone }) {
    const empty = { current: '', code: '', password: '', confirm: '' };
    const [open, setOpen] = useState(false);
    const [form, set, setForm] = useForm(empty);
    const [withCode, setWithCode] = useState(false);
    const [saved, setSaved] = useState('');
    const act = useAction();

    function close() {
        setOpen(false);
        setForm(empty);
        setWithCode(false);
        act.setError('');
    }

    function submit(e) {
        e.preventDefault();
        const problem = checkPasswords(form.password, form.confirm);
        if (problem) return act.setError(problem);
        act.run(async () => {
            const result = await api.post(`/api/dens/${den.den_id}/password`, {
                current: withCode ? '' : form.current, recovery_code: withCode ? form.code : '', password: form.password,
            });
            close();
            setSaved(`Password changed.${signedOut(result.signed_out)}`);
            await onDone();
        });
    }

    if (!open) {
        return (
            <div class="flex flex-wrap items-center gap-3">
                <button type="button" class="btn btn-sm" onClick={() => {
                    setSaved('');
                    setOpen(true);
                }}>
                    Change password
                </button>
                {saved && <span class="text-sm text-success">{saved}</span>}
            </div>
        );
    }
    return (
        <form class="flex flex-col gap-2" onSubmit={submit}>
            <p class="text-sm text-base-content/70">Signs out your other devices.</p>
            {withCode ? (
                <Field label="Recovery code" hint="One of the codes you saved. It works once.">
                    <TextInput value={form.code} onInput={(v) => set('code', v)} autocomplete="off" required class="input w-full font-mono" />
                </Field>
            ) : (
                <Field label="Current password">
                    <TextInput type="password" value={form.current} onInput={(v) => set('current', v)} autocomplete="current-password" required />
                </Field>
            )}
            <PasswordFields password={form.password} setPassword={(v) => set('password', v)} confirm={form.confirm} setConfirm={(v) => set('confirm', v)} />
            <ErrorText message={act.error} />
            <div class="flex flex-wrap items-center gap-2">
                <SubmitButton busy={act.busy}>Change password</SubmitButton>
                <button type="button" class="btn btn-ghost btn-sm" onClick={() => setWithCode(!withCode)}>
                    {withCode ? 'Use my current password' : 'Use a recovery code instead'}
                </button>
                <button type="button" class="btn btn-ghost btn-sm" onClick={close}>Cancel</button>
            </div>
        </form>
    );
}

// NewCodes replaces the member's recovery codes, after saying the old ones
// stop working, which takes their den password.
function NewCodes({ den, left, onCodes }) {
    const [asking, setAsking] = useState(false);
    const [password, setPassword] = useState('');
    const act = useAction();

    function submit(e) {
        e.preventDefault();
        act.run(async () => {
            const result = await api.post(`/api/dens/${den.den_id}/recovery-codes`, { password });
            setPassword('');
            setAsking(false);
            onCodes(result.recovery_codes);
        });
    }

    return (
        <form class="flex flex-col gap-2" onSubmit={submit}>
            <p class={`text-sm ${left <= 3 ? 'text-warning' : 'text-base-content/70'}`}>
                {left === 1 ? '1 code left.' : `${left} codes left.`} Use one to sign in if you forget your password.
            </p>
            {asking ? (
                <>
                    <p class="text-sm">New codes replace all the old ones, which stop working.</p>
                    <Field label="Den password">
                        <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required />
                    </Field>
                    <ErrorText message={act.error} />
                    <div class="flex gap-2">
                        <SubmitButton busy={act.busy}>Generate new codes</SubmitButton>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => setAsking(false)}>Cancel</button>
                    </div>
                </>
            ) : (
                <div><button type="button" class="btn btn-sm" onClick={() => setAsking(true)}>Generate new codes</button></div>
            )}
        </form>
    );
}

// Files lists this member's files in a den, largest first, with what uses
// each, and deletes or swaps them (M5): a member at their limit frees
// space here. A swap is a new upload that takes the old file's place.
function Files({ den, navigate }) {
    const [shown, setShown] = useState(null);
    const [confirm, setConfirm] = useState('');
    const [progress, setProgress] = useState({});
    const act = useAction();
    const pick = useRef(null);
    const swapping = useRef(null);
    const base = `/api/dens/${den.den_id}`;
    const slow = useLater(shown === null && act.busy, 1000);

    async function load() {
        const [view, storage, page] = await Promise.all([api.get(`${base}/state`), api.get(`${base}/storage`), api.get(`${base}/files`)]);
        setShown({ view, storage, files: page.files, next: page.next || '' });
    }
    useEffect(() => {
        act.run(load);
    }, [den.den_id]);

    function more() {
        act.run(async () => {
            const page = await api.get(`${base}/files?after=${encodeURIComponent(shown.next)}`);
            setShown((s) => ({ ...s, files: [...s.files, ...page.files], next: page.next || '' }));
        });
    }

    function open(f) {
        openAt(den.den_id, f.channel_id, f.message_id);
        navigate(`/den/${den.den_id}/${f.channel_id}`);
    }

    function remove(f) {
        act.run(async () => {
            if (f.profile) await api.patch(`${base}/me`, { [f.profile]: '' });
            else if (!f.message_id) await api.del(`${base}/uploads/${f.id}`);
            else await api.post(`${base}/messages/${f.message_id}/files/${f.id}/remove?channel=${f.channel_id}`);
            setConfirm('');
            await load();
        });
    }

    function startSwap(f) {
        swapping.current = f;
        pick.current.value = '';
        pick.current.click();
    }

    function swapPicked(e) {
        const file = e.currentTarget.files?.[0];
        const f = swapping.current;
        if (!file || !f) return;
        const dm = shown.view.channels.find((c) => c.id === f.channel_id)?.kind === 'dm';
        // The row follows the upload while a video's copy is made (M5.4).
        const key = progressKey();
        const track = (sent) => setProgress((cur) => ({ ...cur, [f.id]: { ...cur[f.id], sent } }));
        const follow = (following) => setProgress((cur) => ({ ...cur, [f.id]: { ...cur[f.id], following } }));
        track(0);
        act.run(async () => {
            try {
                // A photo or video swapped in goes compressed like any
                // other, unless the member turned that off (M5).
                const send = sendSmaller() ? 'smaller' : 'full';
                let up = await upload(den.den_id, dm ? f.channel_id : '', file, file.name || 'file', track, f.id, send, key, follow).done;
                if (needsPreview(up)) up = await addPreview(den.den_id, up);
                await api.post(`${base}/messages/${f.message_id}/files/${f.id}/swap?channel=${f.channel_id}`, { upload: up.id });
                await load();
            } finally {
                setProgress((cur) => {
                    const next = { ...cur };
                    delete next[f.id];
                    return next;
                });
            }
        });
    }

    const limit = shown?.view.limits.member_storage || 0;
    return (
        <>
            <Heading>Your files</Heading>
            <ErrorText message={act.error} />
            {shown === null ? (
                slow && <span class="loading loading-dots loading-sm"></span>
            ) : (
                <>
                    <div class="flex flex-col gap-1 text-sm">
                        <p>{formatSize(shown.storage.used)} of {formatSize(limit)} used</p>
                        <progress class="progress progress-primary w-full" value={shown.storage.used} max={limit}></progress>
                        {den.role === 'owner' && (
                            <p class="text-xs text-base-content/60">
                                The den's files take {formatSize(shown.storage.den_used)} of {formatSize(shown.view.limits.den_storage)}.
                            </p>
                        )}
                    </div>
                    {shown.files.length === 0 ? (
                        <p class="text-sm text-base-content/70">You have no files here.</p>
                    ) : (
                        <ul class="flex flex-col divide-y divide-base-300">
                            {shown.files.map((f) => (
                                <FileRow key={f.id} denID={den.den_id} f={f} view={shown.view} confirming={confirm === f.id} busy={act.busy}
                                    progress={progress[f.id]} onOpen={() => open(f)} onSwap={() => startSwap(f)}
                                    onConfirm={() => setConfirm(f.id)} onCancel={() => setConfirm('')} onRemove={() => remove(f)} />
                            ))}
                        </ul>
                    )}
                    {shown.next && (
                        <div>
                            <button type="button" class="btn btn-sm" disabled={act.busy} onClick={more}>Show more</button>
                        </div>
                    )}
                </>
            )}
            <input ref={pick} type="file" class="hidden" onChange={swapPicked} />
        </>
    );
}

// FileRow is one of a member's files in their list: its preview, name,
// size and what uses it, and what they can do with it. A file being
// swapped shows how far its replacement is, and how far a video's copy.
function FileRow({ denID, f, view, confirming, busy, progress, onOpen, onSwap, onConfirm, onCancel, onRemove }) {
    const following = progress?.following;
    const counting = progressLabel(following);
    const onMessage = !!f.message_id;
    const what = f.profile ? 'Take it off your profile?' : onMessage ? 'Delete it for good? It comes off its message for everyone.' : 'Drop this upload?';
    const action = f.profile ? 'Remove' : onMessage ? 'Delete' : 'Drop';
    return (
        <li class="flex flex-wrap items-center gap-2 py-2">
            {f.thumb && !f.locked ? (
                <img src={thumbURL(denID, f.id)} alt="" class="h-10 w-10 shrink-0 rounded bg-base-300 object-cover" loading="lazy" draggable={false} />
            ) : (
                <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded bg-base-300 text-xs">FILE</span>
            )}
            <span class="min-w-0 flex-1 text-sm">
                <span class="block truncate font-medium" title={f.name}>{f.locked ? "A file in a DM this device can't open" : f.name}</span>
                <span class="block truncate text-xs text-base-content/60">
                    {formatSize(f.size)} · {fileWhere(f, view)} · {day(f.created_at)}
                </span>
                {f.excerpt && <span class="block truncate text-xs text-base-content/50">{f.excerpt}</span>}
            </span>
            {progress !== undefined ? (
                <span class="flex flex-col items-end gap-0.5">
                    {counting && <span class="text-xs text-base-content/60">{counting}</span>}
                    <progress class="progress progress-primary w-24" value={Math.round((counting ? following.done : progress.sent) * 100)} max="100"></progress>
                </span>
            ) : confirming ? (
                <span class="flex flex-col items-end gap-1">
                    <span class="max-w-xs text-right text-xs">{what}</span>
                    <span class="flex gap-1">
                        <button type="button" class="btn btn-error btn-xs" disabled={busy} onClick={onRemove}>{action}</button>
                        <button type="button" class="btn btn-ghost btn-xs" onClick={onCancel}>Cancel</button>
                    </span>
                </span>
            ) : (
                <span class="flex gap-1">
                    {onMessage && <button type="button" class="btn btn-ghost btn-xs" onClick={onOpen}>Open</button>}
                    {onMessage && !f.locked && <button type="button" class="btn btn-ghost btn-xs" disabled={busy} onClick={onSwap}>Swap</button>}
                    {!f.locked && <button type="button" class="btn btn-ghost btn-xs text-error" onClick={onConfirm}>{action}</button>}
                </span>
            )}
        </li>
    );
}

// Leave takes this member out of a den and forgets it on this computer.
// A den Dens can't reach, as one whose server is gone for good, can't hear
// of a leave, so it's removed from this computer alone, as one that let
// this device go is (M10).
function Leave({ den, onLeft }) {
    const act = useAction();
    const offer = leaveOffer(den);
    if (offer !== 'leave') {
        return (
            <>
                <Heading>Leave {den.name}</Heading>
                <p>
                    {offer === 'unreachable'
                        ? "Dens can't reach this den right now, so it can't take you out of it. If it's gone for good, remove it from this computer instead."
                        : den.error || 'This den no longer accepts this device.'}
                </p>
                <p>
                    That deletes your keys for the den on this computer, and the messages and files it kept.
                    {offer === 'unreachable' && ' Your account stays in the den, so if it comes back, you can sign in again from Add a den.'}
                </p>
                <ErrorText message={act.error} />
                <div>
                    <button type="button" class="btn btn-error" disabled={act.busy} onClick={() => act.run(async () => {
                        await api.del(`/api/dens/${den.den_id}`);
                        onLeft();
                    })}>
                        Remove from this computer
                    </button>
                </div>
            </>
        );
    }
    return (
        <>
            <Heading>Leave {den.name}</Heading>
            <p>
                You're signed out on every device, and your messages stay. You can come back as @{den.username} if someone invites you
                again.
            </p>
            <ErrorText message={act.error} />
            <div>
                <button type="button" class="btn btn-error" disabled={act.busy} onClick={() => act.run(async () => {
                    await api.post(`/api/dens/${den.den_id}/leave`);
                    onLeft();
                })}>
                    Leave den
                </button>
            </div>
        </>
    );
}
