// The home page: the dens this install has joined, joining another,
// hosting one, and inviting people to a den you own.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import {
    Card, CopyButton, ErrorText, Field, PasswordFields, SubmitButton, TextInput, Waiting, checkPasswords, day, useAction,
    useLater,
} from './components.jsx';
import { PendingSignIns, SealNeeded, SealSection, SealShown, SignInRequests } from './private.jsx';
import { progressKey, progressLabel, sendSmaller } from './compare.jsx';
import { addPreview, fileWhere, formatSize, needsPreview, thumbURL, upload } from './files.jsx';
import { openAt } from './messages.jsx';
import { deletesNow, keptFor } from './retention.js';
import { HEIGHTS, mbps, shareCost } from './share.js';

const VERIFIER_HINT =
    "You'll need this password to sign in to the den from a new device. The den never sees it: it gets a value derived from " +
    "it that is different for every den, so reusing a password across dens is safe.";

export function Home({ status, navigate }) {
    const [view, setView] = useState(null);
    const [codes, setCodes] = useState(null);
    // signIn fills the sign-in form for a den this device was signed out of.
    const [signIn, setSignIn] = useState(null);

    // show keeps the newest list, whether it came from the stream or a fetch.
    const show = (v) => setView((cur) => (!cur || v.epoch !== cur.epoch || v.version >= cur.version ? v : cur));
    // refresh fetches the list after an action here changed it, rather than
    // wait for the stream, which a browser can hold back.
    const refresh = () => api.get('/api/dens').then(show, () => {});

    useEffect(() => {
        // The list comes over plain HTTP first: the event stream's socket
        // can open late, since Firefox holds back sockets to an address
        // that recently refused them.
        let fresh = true;
        api.get('/api/dens').then((v) => fresh && show(v), () => {});
        const stop = onEvent((message) => {
            if (message.t === 'dens') show(message.d);
        });
        return () => {
            fresh = false;
            stop();
        };
    }, []);

    if (codes) {
        return <RecoveryCodes denName={codes.name} codes={codes.codes} seal={codes.seal} sealNew={codes.sealNew} onDone={() => setCodes(null)} />;
    }
    const joined = (result) => {
        setCodes({ name: result.den.name, codes: result.recovery_codes, seal: result.seal, sealNew: result.seal_new });
        refresh();
    };
    const showCodes = (name, list) => setCodes({ name, codes: list });
    // A new seal, from starting over, shows once like a join's.
    const showSeal = (name, seal) => setCodes({ name, codes: [], seal, sealNew: true });

    return (
        <>
            {status.updateVersion && (
                <div role="status" class="alert alert-info">
                    <span>
                        Dens {status.updateVersion} is available. To update, run: <code>{status.updateCommand}</code>
                    </span>
                </div>
            )}
            {!view ? (
                <Waiting label="Waiting for Dens on this computer…" />
            ) : (
                <>
                    <PendingSignIns list={view.sign_ins} />
                    {view.hosting.enabled && !view.hosting.joined && <HostDen hosting={view.hosting} onJoined={joined} />}
                    <DenList dens={view.dens} navigate={navigate} onCodes={showCodes} onSeal={showSeal} onChanged={refresh}
                        onSignIn={(den) => setSignIn({ den: den.url, username: den.username, key: Date.now() })} />
                    <JoinDen onJoined={joined} />
                    <SignInDen key={signIn?.key} prefill={signIn} onDone={refresh} />
                </>
            )}
        </>
    );
}

const STATE_BADGES = {
    connected: ['badge-success', 'Connected'],
    connecting: ['badge-info', 'Connecting'],
    offline: ['badge-warning', 'Offline'],
    revoked: ['badge-error', 'Signed out'],
    removed: ['badge-error', 'Not a member'],
};

const ROLES = { owner: 'the owner', moderator: 'a moderator', member: 'a member' };

function DenList({ dens, navigate, onCodes, onSeal, onChanged, onSignIn }) {
    if (dens.length === 0) {
        return (
            <Card title="Your dens">
                <p>You haven't joined any dens yet. Paste an invite below to join one.</p>
            </Card>
        );
    }
    return (
        <Card title="Your dens">
            <ul class="flex flex-col gap-3">
                {dens.map((den) => <DenItem key={den.den_id} den={den} navigate={navigate} onCodes={onCodes} onSeal={onSeal} onChanged={onChanged} onSignIn={onSignIn} />)}
            </ul>
        </Card>
    );
}

function DenItem({ den, navigate, onCodes, onSeal, onChanged, onSignIn }) {
    const [badge, label] = STATE_BADGES[den.state] || ['badge-ghost', den.state];
    const gone = den.state === 'revoked' || den.state === 'removed';
    const staff = den.role === 'owner' || den.role === 'moderator';
    const forget = useAction();
    return (
        <li class="rounded-box bg-base-100 p-4">
            <div class="flex flex-wrap items-center gap-2">
                <span class="text-lg font-semibold">{den.name}</span>
                <span class={`badge ${badge}`}>{label}</span>
                {den.own && <span class="badge badge-outline">Hosted here</span>}
                {gone ? (
                    <div class="ml-auto flex gap-2">
                        {den.state === 'revoked' && (
                            <button type="button" class="btn btn-primary btn-sm" onClick={() => onSignIn(den)}>Sign in again</button>
                        )}
                        <button type="button" class="btn btn-sm" disabled={forget.busy}
                            onClick={() => forget.run(async () => {
                                await api.del(`/api/dens/${den.den_id}`);
                                await onChanged();
                            })}>
                            Remove from this computer
                        </button>
                    </div>
                ) : (
                    <button type="button" class="btn btn-primary btn-sm ml-auto" onClick={() => navigate(`/den/${den.den_id}`)}>
                        Open
                    </button>
                )}
            </div>
            <p class="text-sm text-base-content/70">
                {den.url} · you're <span class="font-medium">{den.display_name}</span> (@{den.username}), {ROLES[den.role] || den.role}
            </p>
            <p class="text-xs text-base-content/60" title="The start of the den's identity. Everyone in this den sees the same one.">
                Den ID <span class="font-mono">{den.fingerprint}</span>
            </p>
            {den.retention > 0 && (
                <p class="text-xs text-base-content/60">Messages here, and their files, are deleted after {keptFor(den.retention)}.</p>
            )}
            {den.error && <p class="text-sm text-warning">{den.error}</p>}
            <ErrorText message={forget.error} />
            {den.state === 'offline' && !den.own && <NewAddress den={den} onChanged={onChanged} />}
            {den.state === 'connected' && (
                <>
                    <SignInRequests den={den} />
                    {!den.seal && <SealNeeded den={den} onSeal={(seal) => onSeal(den.name, seal)} />}
                    {den.role === 'owner' && <DenSettings den={den} />}
                    {staff && <Invites denID={den.den_id} />}
                    {staff && <Bans denID={den.den_id} />}
                    <Files den={den} navigate={navigate} />
                    <Devices den={den} onCodes={onCodes} onSeal={onSeal} />
                    {!den.own && <Leave den={den} onChanged={onChanged} />}
                </>
            )}
        </li>
    );
}

// Bans lists who can't come back, and lifts bans.
function Bans({ denID }) {
    const [open, setOpen] = useState(false);
    const [list, setList] = useState(null);
    const act = useAction();

    function toggle(e) {
        setOpen(e.currentTarget.open);
        if (e.currentTarget.open) act.run(async () => setList((await api.get(`/api/dens/${denID}/bans`)).bans));
    }

    return (
        <details class="collapse-arrow collapse mt-2 bg-base-200" onToggle={toggle}>
            <summary class="collapse-title cursor-pointer select-none font-medium">Bans</summary>
            <div class="collapse-content flex flex-col gap-2">
                <ErrorText message={act.error} />
                {!open ? null : list === null ? (
                    <span class="loading loading-dots loading-sm"></span>
                ) : list.length === 0 ? (
                    <p class="text-sm text-base-content/70">Nobody is banned.</p>
                ) : (
                    <ul class="flex flex-col gap-1">
                        {list.map((b) => (
                            <li key={b.member.id} class="flex items-center gap-2 text-sm">
                                <span class="min-w-0 flex-1 truncate">
                                    <span class="font-medium">{b.member.display_name}</span> @{b.member.username}, since {new Date(b.banned_at).toLocaleDateString()}
                                </span>
                                <button type="button" class="btn btn-ghost btn-xs" disabled={act.busy}
                                    onClick={() => act.run(async () => {
                                        await api.del(`/api/dens/${denID}/bans/${b.member.id}`);
                                        setList((await api.get(`/api/dens/${denID}/bans`)).bans);
                                    })}>
                                    Lift ban
                                </button>
                            </li>
                        ))}
                    </ul>
                )}
                <p class="text-xs text-base-content/60">Lifting a ban doesn't bring anyone back; they can join again with a new invite.</p>
            </div>
        </details>
    );
}

// Devices lists this member's devices on a den and signs any of them out,
// changes the den password and recovery codes, and shows the DM seal.
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

    // Closing the section forgets what it showed; opening loads it afresh.
    function toggle(e) {
        setConfirm('');
        act.setError('');
        if (e.currentTarget.open) act.run(load);
        else setShown(null);
    }

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
                // A photo or video swapped in goes smaller like any other,
                // unless the member turned copies off (M5).
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
        <details class="collapse-arrow collapse mt-2 bg-base-200" onToggle={toggle}>
            <summary class="collapse-title cursor-pointer select-none font-medium">Files</summary>
            <div class="collapse-content flex flex-col gap-3">
                <ErrorText message={act.error} />
                {shown === null ? (
                    slow && <span class="loading loading-dots loading-sm"></span>
                ) : (
                    <>
                        <div class="flex flex-col gap-1 text-sm">
                            <p>Your files take {formatSize(shown.storage.used)} of the {formatSize(limit)} you have here.</p>
                            <progress class="progress progress-primary w-full" value={shown.storage.used} max={limit}></progress>
                            <p class="text-xs text-base-content/60">
                                Everyone's files take {formatSize(shown.storage.den_used)} of the den's {formatSize(shown.view.limits.den_storage)}.
                            </p>
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
            </div>
        </details>
    );
}

// FileRow is one of a member's files in their list: its preview, name,
// size and what uses it, and what they can do with it. A file being
// swapped shows how far its replacement is, and how far a video's copy.
function FileRow({ denID, f, view, confirming, busy, progress, onOpen, onSwap, onConfirm, onCancel, onRemove }) {
    const following = progress?.following;
    const counting = progressLabel(following);
    const onMessage = !!f.message_id;
    const what = f.profile ? 'Take it off your profile?' : onMessage ? "Delete it for good? It comes off its message for everyone." : 'Drop this upload?';
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
                    <progress class="progress progress-primary w-24" value={Math.round((counting ? following.done : progress.sent) * 100)}
                        max="100"></progress>
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

function Devices({ den, onCodes, onSeal }) {
    const [open, setOpen] = useState(false);
    const [list, setList] = useState(null);
    const [confirm, setConfirm] = useState('');
    const act = useAction();
    const load = async () => setList(await api.get(`/api/dens/${den.den_id}/devices`));
    // The list usually arrives at once; the dots show only if it's slow.
    const slow = useLater(open && list === null && !act.error, 1000);

    // Closing the section forgets what it showed; opening loads it afresh.
    function toggle(e) {
        setOpen(e.currentTarget.open);
        setConfirm('');
        act.setError('');
        if (e.currentTarget.open) act.run(load);
        else setList(null);
    }

    function signOut(d) {
        act.run(async () => {
            await api.del(`/api/dens/${den.den_id}/devices/${d.key_id}`);
            setConfirm('');
            if (!d.current) await load();
        });
    }

    return (
        <details class="collapse-arrow collapse mt-2 bg-base-200" onToggle={toggle}>
            <summary class="collapse-title cursor-pointer select-none font-medium">Devices and password</summary>
            <div class="collapse-content flex flex-col gap-4">
                <div class="flex flex-col gap-2">
                    <ErrorText message={act.error} />
                    {list === null ? (
                        slow && <span class="loading loading-dots loading-sm"></span>
                    ) : (
                        <ul class="flex flex-col gap-2">
                            {list.devices.map((d) => (
                                <li key={d.key_id} class="flex flex-wrap items-center gap-2 text-sm">
                                    <span class="min-w-0 flex-1">
                                        <span class="font-medium">{d.label}</span>
                                        {d.current && <span class="badge badge-soft badge-success badge-sm ml-2">This device</span>}
                                        <span class="block text-xs text-base-content/60">Added {day(d.created_at)} · last signed in {day(d.last_seen_at)}</span>
                                    </span>
                                    {confirm === d.key_id ? (
                                        <span class="flex flex-col items-end gap-1">
                                            {list.devices.length === 1 && (
                                                <span class="max-w-xs text-right text-xs text-warning">
                                                    It's your only device here. Signing in again then takes a recovery code, and your DM
                                                    seal to read your direct messages.
                                                </span>
                                            )}
                                            <span class="flex gap-1">
                                                <button type="button" class="btn btn-error btn-xs" disabled={act.busy} onClick={() => signOut(d)}>
                                                    {d.current ? 'Sign this device out' : 'Sign it out'}
                                                </button>
                                                <button type="button" class="btn btn-ghost btn-xs" onClick={() => setConfirm('')}>Cancel</button>
                                            </span>
                                        </span>
                                    ) : (
                                        <button type="button" class="btn btn-ghost btn-xs text-error" onClick={() => setConfirm(d.key_id)}>Sign out</button>
                                    )}
                                </li>
                            ))}
                        </ul>
                    )}
                    <p class="text-xs text-base-content/60">
                        Signing a device out ends its access at once. Signing in again takes your den password, and one of your other
                        devices to approve it. If a sign-in you didn't make asks for approval, refuse it and change your password
                        below: that signs out every device but this one.
                    </p>
                </div>
                <ChangePassword den={den} onDone={load} />
                {list !== null && <NewCodes den={den} left={list.recovery_codes_left} onCodes={onCodes} onDone={load} />}
                {den.seal && <SealSection den={den} onSeal={(seal) => onSeal(den.name, seal)} />}
            </div>
        </details>
    );
}

// signedOut says how many of the member's other devices a new den password
// signed out.
function signedOut(n) {
    if (!n) return '';
    return n === 1 ? ' 1 other device was signed out.' : ` ${n} other devices were signed out.`;
}

// LocalPasswordHelp names the local password, with a tooltip that tells it
// apart from a den password.
function LocalPasswordHelp({ id }) {
    return (
        <span class="tooltip">
            <span id={id} role="tooltip" class="tooltip-content rounded-lg px-3 py-2 text-left">
                Your local password protects Dens on this computer: its backups, and the keys it holds for every den. A den
                password signs you in to one den on a new device, and that den never sees it.
            </span>
            <button type="button" class="cursor-help underline decoration-dotted" aria-describedby={id}>local password</button>
        </span>
    );
}

// ChangePassword sets the member's den password, with the current one or a
// recovery code, which signs out their other devices.
function ChangePassword({ den, onDone }) {
    const [form, set, setForm] = useForm({ current: '', code: '', password: '', confirm: '' });
    const [withCode, setWithCode] = useState(false);
    const [saved, setSaved] = useState('');
    const act = useAction();

    function submit(e) {
        e.preventDefault();
        setSaved('');
        const problem = checkPasswords(form.password, form.confirm);
        if (problem) return act.setError(problem);
        act.run(async () => {
            const result = await api.post(`/api/dens/${den.den_id}/password`, {
                current: withCode ? '' : form.current, recovery_code: withCode ? form.code : '', password: form.password,
            });
            setForm({ current: '', code: '', password: '', confirm: '' });
            setSaved(`Password changed.${signedOut(result.signed_out)}`);
            await onDone();
        });
    }

    return (
        <form class="flex flex-col gap-2 border-t border-base-300 pt-3" onSubmit={submit}>
            <h3 class="font-medium">Change your den password</h3>
            <p class="text-xs text-base-content/60">
                Your den password signs you in to {den.name} on a new device. Changing it signs out every other device you have
                there, and each signs in again with the new one.
            </p>
            <p class="text-xs text-base-content/60">
                It's separate from your <LocalPasswordHelp id={`local-password-${den.den_id}`} />.
            </p>
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
            {saved && <p class="text-sm text-success">{saved}</p>}
            <div class="flex flex-wrap items-center gap-2">
                <SubmitButton busy={act.busy}>Change password</SubmitButton>
                <button type="button" class="btn btn-ghost btn-sm" onClick={() => setWithCode(!withCode)}>
                    {withCode ? 'Use my current password' : 'Use a recovery code instead'}
                </button>
            </div>
        </form>
    );
}

// NewCodes replaces the member's recovery codes, which takes their den
// password.
function NewCodes({ den, left, onCodes, onDone }) {
    const [asking, setAsking] = useState(false);
    const [password, setPassword] = useState('');
    const act = useAction();

    function submit(e) {
        e.preventDefault();
        act.run(async () => {
            const result = await api.post(`/api/dens/${den.den_id}/recovery-codes`, { password });
            setPassword('');
            setAsking(false);
            await onDone();
            onCodes(den.name, result.recovery_codes);
        });
    }

    return (
        <form class="flex flex-col gap-2 border-t border-base-300 pt-3" onSubmit={submit}>
            <h3 class="font-medium">Recovery codes</h3>
            <p class={`text-sm ${left <= 3 ? 'text-warning' : 'text-base-content/70'}`}>
                {left === 1 ? 'You have 1 recovery code left.' : `You have ${left} recovery codes left.`} New codes replace all the old
                ones.
            </p>
            {asking && (
                <Field label="Den password">
                    <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required />
                </Field>
            )}
            <ErrorText message={act.error} />
            <div class="flex gap-2">
                {asking ? (
                    <>
                        <SubmitButton busy={act.busy}>Make new codes</SubmitButton>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => setAsking(false)}>Cancel</button>
                    </>
                ) : (
                    <button type="button" class="btn btn-sm" onClick={() => setAsking(true)}>Make new codes</button>
                )}
            </div>
        </form>
    );
}

// NewAddress gives a den that can't be reached the new address its owner
// moved it to. The service uses it only if the den there proves it's the
// same one.
function NewAddress({ den, onChanged }) {
    const [url, setURL] = useState('');
    const act = useAction();

    function submit(e) {
        e.preventDefault();
        act.run(async () => {
            await api.post(`/api/dens/${den.den_id}/address`, { url });
            setURL('');
            await onChanged();
        });
    }

    return (
        <details class="collapse-arrow collapse mt-2 bg-base-200">
            <summary class="collapse-title cursor-pointer select-none font-medium">Den moved?</summary>
            <form class="collapse-content flex flex-col gap-2" onSubmit={submit}>
                <p class="text-xs text-base-content/60">
                    If the owner gave {den.name} a new address, enter it here. Dens only uses it if the den there proves it's the
                    same one.
                </p>
                <Field label="New address">
                    <TextInput value={url} onInput={setURL} placeholder="https://den.example.com" autocomplete="off" required />
                </Field>
                <ErrorText message={act.error} />
                <SubmitButton busy={act.busy}>Use this address</SubmitButton>
            </form>
        </details>
    );
}

// Leave takes this member out of a den and forgets it on this computer.
function Leave({ den, onChanged }) {
    const [confirming, setConfirming] = useState(false);
    const act = useAction();
    return (
        <div class="mt-2">
            {!confirming ? (
                <button type="button" class="btn btn-ghost btn-xs text-error" onClick={() => setConfirming(true)}>Leave den</button>
            ) : (
                <div class="flex flex-col gap-2 rounded bg-base-200 p-3 text-sm">
                    <p>
                        Leave {den.name}? You're signed out on every device, and your messages stay. You can come back as @{den.username} if
                        someone invites you again.
                    </p>
                    <ErrorText message={act.error} />
                    <div class="flex gap-2">
                        <button type="button" class="btn btn-error btn-sm" disabled={act.busy} onClick={() => act.run(async () => {
                            await api.post(`/api/dens/${den.den_id}/leave`);
                            await onChanged();
                        })}>
                            Leave
                        </button>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => setConfirming(false)}>Cancel</button>
                    </div>
                </div>
            )}
        </div>
    );
}

// DenSettings changes a den's name and public address, and tests whether
// the address reaches the den from this computer.
function DenSettings({ den }) {
    const [name, setName] = useState(den.name);
    const [url, setURL] = useState(den.url);
    const [saved, setSaved] = useState(false);
    const [reached, setReached] = useState(false);
    // Each opening loads the limits afresh.
    const [opened, setOpened] = useState(0);
    const save = useAction();
    const check = useAction();

    function submit(e) {
        e.preventDefault();
        setSaved(false);
        setReached(false);
        const body = {};
        if (name !== den.name) body.name = name;
        if (url !== den.url) body.url = url;
        save.run(async () => {
            await api.post(`/api/dens/${den.den_id}/settings`, body);
            setSaved(true);
        });
    }

    function test() {
        setReached(false);
        check.run(async () => {
            await api.post(`/api/dens/${den.den_id}/check`);
            setReached(true);
        });
    }

    // Closing the section drops unsaved edits and old results.
    function toggle(e) {
        if (e.currentTarget.open) return setOpened((n) => n + 1);
        setName(den.name);
        setURL(den.url);
        setSaved(false);
        setReached(false);
        save.setError('');
        check.setError('');
    }

    return (
        <details class="collapse-arrow collapse mt-2 bg-base-200" onToggle={toggle}>
            <summary class="collapse-title cursor-pointer select-none font-medium">Den settings</summary>
            <div class="collapse-content flex flex-col gap-3">
                <form class="flex flex-col gap-2" onSubmit={submit}>
                    <Field label="Den name">
                        <TextInput value={name} onInput={setName} required maxlength="32" />
                    </Field>
                    <Field
                        label="Public address"
                        hint="Where members reach the den. After changing it, keep the old address pointing at the den for a while: members who were offline, and invites already sent, find the new one through it."
                    >
                        <TextInput type="url" value={url} onInput={setURL} required />
                    </Field>
                    <ErrorText message={save.error} />
                    {saved && <p class="text-sm text-success">Saved.</p>}
                    <div>
                        <SubmitButton busy={save.busy}>Save</SubmitButton>
                    </div>
                </form>
                <div class="flex flex-col gap-2">
                    <div>
                        <button type="button" class="btn btn-sm" onClick={test} disabled={check.busy}>
                            {check.busy && <span class="loading loading-spinner loading-sm"></span>}
                            Test the address
                        </button>
                    </div>
                    {reached && (
                        <p class="text-sm text-success">This computer reached the den at {den.url}, and it proved its identity there.</p>
                    )}
                    {check.error && (
                        <>
                            <ErrorText message={check.error} />
                            {den.own && (
                                <p class="text-sm text-base-content/70">
                                    Some routers can't reach their own public address from inside the network, so members may
                                    still get through. If you're unsure, test from a phone or another network.
                                </p>
                            )}
                        </>
                    )}
                </div>
                <UploadLimits key={opened} denID={den.den_id} />
                <Retention key={`retention-${opened}`} denID={den.den_id} />
                <CallLimits key={`calls-${opened}`} denID={den.den_id} />
            </div>
        </details>
    );
}

const MB = 1 << 20;
const GB = 1 << 30;

// UploadLimits sets how large a file may be, and how much space each
// member's files and the whole den's may take.
function UploadLimits({ denID }) {
    const [limits, setLimits] = useState(null);
    const [saved, setSaved] = useState(false);
    const save = useAction();
    useEffect(() => {
        api.get(`/api/dens/${denID}/state`).then((v) => setLimits({
            file: String(Math.round(v.limits.file_size / MB)),
            member: String(+(v.limits.member_storage / GB).toFixed(2)),
            den: String(+(v.limits.den_storage / GB).toFixed(2)),
        }), (e) => save.setError(e.message));
    }, [denID]);
    if (!limits) return save.error ? <ErrorText message={save.error} /> : null;
    const set = (key) => (value) => {
        setSaved(false);
        setLimits({ ...limits, [key]: value });
    };

    function submit(e) {
        e.preventDefault();
        setSaved(false);
        save.run(async () => {
            await api.post(`/api/dens/${denID}/settings`, {
                limits: {
                    file_size: Math.round(Number(limits.file) * MB),
                    member_storage: Math.round(Number(limits.member) * GB),
                    den_storage: Math.round(Number(limits.den) * GB),
                },
            });
            setSaved(true);
        });
    }

    return (
        <form class="flex flex-col gap-2 border-t border-base-300 pt-3" onSubmit={submit}>
            <h3 class="font-medium">Uploads</h3>
            <p class="text-sm text-base-content/70">
                Files are stored encrypted on this den's computer. Dens also stops taking uploads when its disk has less than 1 GB free.
            </p>
            <div class="grid gap-2 sm:grid-cols-3">
                <Field label="Largest file (MB)">
                    <TextInput type="number" min="1" max="1024" step="1" value={limits.file} onInput={set('file')} required />
                </Field>
                <Field label="Space per member (GB)">
                    <TextInput type="number" min="0.01" step="0.01" value={limits.member} onInput={set('member')} required />
                </Field>
                <Field label="Space for the den (GB)">
                    <TextInput type="number" min="0.01" step="0.01" value={limits.den} onInput={set('den')} required />
                </Field>
            </div>
            <ErrorText message={save.error} />
            {saved && <p class="text-sm text-success">Saved.</p>}
            <div>
                <SubmitButton busy={save.busy}>Save upload limits</SubmitButton>
            </div>
        </form>
    );
}

// MAX_RETENTION is the longest a den keeps messages, in days.
const MAX_RETENTION = 3650;

// Retention sets how long the den keeps messages (M5), or that it keeps
// them all, and says what a new period would delete at once.
function Retention({ denID }) {
    const [current, setCurrent] = useState(null);
    const [on, setOn] = useState(false);
    const [days, setDays] = useState('30');
    const [preview, setPreview] = useState(null);
    const [saved, setSaved] = useState(false);
    const save = useAction();
    useEffect(() => {
        api.get(`/api/dens/${denID}/state`).then((v) => {
            setCurrent(v.retention);
            setOn(v.retention > 0);
            if (v.retention > 0) setDays(String(v.retention));
        }, (e) => save.setError(e.message));
    }, [denID]);
    const n = Number(days);
    const valid = Number.isInteger(n) && n >= 1 && n <= MAX_RETENTION;
    // The den counts what a period would delete as the owner types one.
    useEffect(() => {
        setPreview(null);
        if (!on || !valid || n === current) return undefined;
        let stale = false;
        const timer = setTimeout(() => {
            api.get(`/api/dens/${denID}/retention?days=${n}`).then((p) => !stale && setPreview(p), () => {});
        }, 300);
        return () => {
            stale = true;
            clearTimeout(timer);
        };
    }, [on, days, current]);
    if (current === null) return save.error ? <ErrorText message={save.error} /> : null;

    function submit(e) {
        e.preventDefault();
        setSaved(false);
        const retention = on ? n : 0;
        save.run(async () => {
            await api.post(`/api/dens/${denID}/settings`, { retention });
            setCurrent(retention);
            setSaved(true);
        });
    }

    return (
        <form class="flex flex-col gap-2 border-t border-base-300 pt-3" onSubmit={submit}>
            <h3 class="font-medium">Retention</h3>
            <p class="text-sm text-base-content/70">
                The den can delete messages, and their files, once they're older than a number of days, in channels and DMs alike. Members
                see how long messages are kept. Backups keep what the den held when they were made.
            </p>
            <label class="label gap-2">
                <input type="checkbox" class="toggle" checked={on} onChange={(e) => {
                    setSaved(false);
                    setOn(e.currentTarget.checked);
                }} />
                Delete old messages
            </label>
            {on && (
                <Field label="Keep messages for (days)">
                    <TextInput type="number" min="1" max={String(MAX_RETENTION)} step="1" value={days} required onInput={(v) => {
                        setSaved(false);
                        setDays(v);
                    }} />
                </Field>
            )}
            {preview && <p class="text-sm">{deletesNow(preview)}</p>}
            <ErrorText message={save.error} />
            {saved && <p class="text-sm text-success">Saved.</p>}
            <div>
                <SubmitButton busy={save.busy}>Save retention</SubmitButton>
            </div>
        </form>
    );
}

// CallLimits sets the den's limits for calls and screen shares (M4.2), and
// says what they cost this computer's upload at most.
function CallLimits({ denID }) {
    const [limits, setLimits] = useState(null);
    const [voice, setVoice] = useState(96000);
    const [saved, setSaved] = useState(false);
    const save = useAction();
    useEffect(() => {
        api.get(`/api/dens/${denID}/state`).then((v) => {
            const l = v.call_limits;
            setLimits({
                members: String(l.members), callers: String(l.callers), shares: String(l.shares), viewers: String(l.share_viewers),
                bitrate: String(l.share_bitrate / 1e6), height: String(l.share_height), fps: String(l.share_fps),
            });
            const rates = v.channels.filter((c) => c.kind === 'voice').map((c) => c.bitrate);
            if (rates.length) setVoice(Math.max(...rates));
        }, (e) => save.setError(e.message));
    }, [denID]);
    if (!limits) return save.error ? <ErrorText message={save.error} /> : null;
    const set = (key) => (value) => {
        setSaved(false);
        setLimits({ ...limits, [key]: value });
    };
    const asked = {
        members: Number(limits.members), callers: Number(limits.callers), shares: Number(limits.shares), share_viewers: Number(limits.viewers),
        share_bitrate: Math.round(Number(limits.bitrate) * 1e6), share_height: Number(limits.height), share_fps: Number(limits.fps),
    };
    const cost = shareCost(asked, voice);
    const known = Object.values(asked).every(Number.isFinite);

    function submit(e) {
        e.preventDefault();
        setSaved(false);
        save.run(async () => {
            await api.post(`/api/dens/${denID}/settings`, { call_limits: asked });
            setSaved(true);
        });
    }

    return (
        <form class="flex flex-col gap-2 border-t border-base-300 pt-3" onSubmit={submit}>
            <h3 class="font-medium">Calls and screen shares</h3>
            <p class="text-sm text-base-content/70">
                Calls go through this computer: each member's voice goes out to everyone else in their call, and each share to everyone watching
                it, so they use this computer's upload. Set these to what your connection can send.
            </p>
            <div class="grid gap-x-3 sm:grid-cols-2">
                <Field label="Members in one call">
                    <TextInput type="number" min="2" max="30" step="1" value={limits.members} onInput={set('members')} required />
                </Field>
                <Field label="Members in calls across the den">
                    <TextInput type="number" min="2" max="100" step="1" value={limits.callers} onInput={set('callers')} required />
                </Field>
                <Field label="Shares at once" hint="0 turns screen sharing off.">
                    <TextInput type="number" min="0" max="10" step="1" value={limits.shares} onInput={set('shares')} required />
                </Field>
                <Field label="Viewers of one share">
                    <TextInput type="number" min="1" max="29" step="1" value={limits.viewers} onInput={set('viewers')} required />
                </Field>
                <Field label="A share's bitrate (Mbps)">
                    <TextInput type="number" min="0.25" max="50" step="0.25" value={limits.bitrate} onInput={set('bitrate')} required />
                </Field>
                <Field label="A share's size">
                    <select class="select w-full" value={limits.height} onChange={(e) => set('height')(e.currentTarget.value)}>
                        {HEIGHTS.map((h) => <option key={h} value={String(h)}>{h === 2160 ? '4K (2160p)' : `${h}p`}</option>)}
                    </select>
                </Field>
                <Field label="A share's frame rate">
                    <TextInput type="number" min="5" max="60" step="1" value={limits.fps} onInput={set('fps')} required />
                </Field>
            </div>
            {known && (
                <p class="text-sm">
                    At most, shares take {mbps(cost.video)} of this computer's upload, and a full call's voice about {mbps(cost.voice)} while two
                    people talk.
                </p>
            )}
            <ErrorText message={save.error} />
            {saved && <p class="text-sm text-success">Saved.</p>}
            <div>
                <SubmitButton busy={save.busy}>Save call limits</SubmitButton>
            </div>
        </form>
    );
}

const EXPIRIES = [
    [3600, '1 hour'],
    [86400, '1 day'],
    [7 * 86400, '7 days'],
    [30 * 86400, '30 days'],
];

function Invites({ denID }) {
    const [expiresIn, setExpiresIn] = useState(86400);
    const [maxUses, setMaxUses] = useState(1);
    const [created, setCreated] = useState(null);
    const [list, setList] = useState(null);
    const create = useAction();
    const manage = useAction();

    async function refresh() {
        const data = await api.get(`/api/dens/${denID}/invites`);
        setList(data.invites);
    }

    function submit(e) {
        e.preventDefault();
        create.run(async () => {
            const data = await api.post(`/api/dens/${denID}/invites`, { expires_in: Number(expiresIn), max_uses: Number(maxUses) });
            setCreated(data.invite);
            if (list) await refresh();
        });
    }

    // Closing the section clears the invite shown once, so it doesn't sit
    // on the page.
    function toggle(e) {
        if (e.currentTarget.open) return;
        setCreated(null);
        setList(null);
        create.setError('');
        manage.setError('');
    }

    return (
        <details class="collapse-arrow collapse mt-2 bg-base-200" onToggle={toggle}>
            <summary class="collapse-title cursor-pointer select-none font-medium">Invite people</summary>
            <div class="collapse-content flex flex-col gap-3">
                <form class="flex flex-wrap items-end gap-3" onSubmit={submit}>
                    <Field label="Expires after">
                        <select class="select" value={expiresIn} onChange={(e) => setExpiresIn(e.currentTarget.value)}>
                            {EXPIRIES.map(([seconds, label]) => <option key={seconds} value={seconds}>{label}</option>)}
                        </select>
                    </Field>
                    <Field label="Uses">
                        <select class="select" value={maxUses} onChange={(e) => setMaxUses(e.currentTarget.value)}>
                            {[1, 5, 25, 100].map((n) => <option key={n} value={n}>{n}</option>)}
                        </select>
                    </Field>
                    <SubmitButton busy={create.busy}>Create invite</SubmitButton>
                </form>
                <ErrorText message={create.error} />
                {created && (
                    <div class="flex flex-col gap-2">
                        <p class="text-sm">
                            Send this invite to the people you're inviting. It's shown only once, and anyone who has it can join
                            until it expires or is used up.
                        </p>
                        <textarea class="textarea w-full font-mono text-xs" rows="3" readonly value={created}></textarea>
                        <div><CopyButton text={created} label="Copy invite" /></div>
                    </div>
                )}
                <div>
                    {list === null ? (
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => manage.run(refresh)} disabled={manage.busy}>
                            Show open invites
                        </button>
                    ) : list.length === 0 ? (
                        <p class="text-sm text-base-content/70">No open invites.</p>
                    ) : (
                        <ul class="flex flex-col gap-1 text-sm">
                            {list.map((inv) => (
                                <li key={inv.id} class="flex items-center gap-2">
                                    <span>
                                        Used {inv.uses} of {inv.max_uses}, expires {new Date(inv.expires_at).toLocaleString()}
                                    </span>
                                    <button
                                        type="button"
                                        class="btn btn-ghost btn-xs"
                                        disabled={manage.busy}
                                        onClick={() => manage.run(async () => {
                                            await api.del(`/api/dens/${denID}/invites/${inv.id}`);
                                            await refresh();
                                        })}
                                    >
                                        Revoke
                                    </button>
                                </li>
                            ))}
                        </ul>
                    )}
                    <ErrorText message={manage.error} />
                </div>
            </div>
        </details>
    );
}

// AccountFields asks for the member's name on a den and their password there.
function AccountFields({ form, set }) {
    return (
        <>
            <Field label="Username" hint="2 to 32 letters, digits and _. It can't change later.">
                <TextInput value={form.username} onInput={(v) => set('username', v)} autocomplete="username" required />
            </Field>
            <Field label="Display name" hint="What others see. You can change it later.">
                <TextInput value={form.displayName} onInput={(v) => set('displayName', v)} required maxlength="32" />
            </Field>
            <PasswordFields
                password={form.password}
                setPassword={(v) => set('password', v)}
                confirm={form.confirm}
                setConfirm={(v) => set('confirm', v)}
                hint={VERIFIER_HINT}
            />
        </>
    );
}

function useForm(initial) {
    const [form, setForm] = useState(initial);
    return [form, (key, value) => setForm((f) => ({ ...f, [key]: value })), setForm];
}

const EMPTY_ACCOUNT = { username: '', displayName: '', password: '', confirm: '' };

function JoinDen({ onJoined }) {
    const [invite, setInvite] = useState('');
    const [preview, setPreview] = useState(null);
    const [form, set, setForm] = useForm(EMPTY_ACCOUNT);
    const { busy, error, setError, run } = useAction();

    function check(e) {
        e.preventDefault();
        run(async () => {
            const data = await api.post('/api/dens/preview', { invite });
            setPreview(data.den);
        });
    }

    // back starts over, from an empty invite.
    function back() {
        setPreview(null);
        setInvite('');
        setForm(EMPTY_ACCOUNT);
        setError('');
    }

    function join(e) {
        e.preventDefault();
        const problem = checkPasswords(form.password, form.confirm);
        if (problem) return setError(problem);
        run(async () => {
            const result = await api.post('/api/dens/join', {
                invite, username: form.username, display_name: form.displayName, password: form.password,
            });
            setInvite('');
            setPreview(null);
            setForm(EMPTY_ACCOUNT);
            onJoined(result);
        });
    }

    if (!preview) {
        return (
            <Card title="Join a den">
                <form class="flex flex-col gap-2" onSubmit={check}>
                    <Field label="Invite" hint="Invites start with dens1: and come from someone in the den.">
                        <textarea
                            class="textarea w-full font-mono text-xs"
                            rows="3"
                            value={invite}
                            onInput={(e) => setInvite(e.currentTarget.value)}
                            required
                        ></textarea>
                    </Field>
                    <ErrorText message={error} />
                    <SubmitButton busy={busy}>Continue</SubmitButton>
                </form>
            </Card>
        );
    }
    return (
        <Card title={`Join ${preview.name}`}>
            <p class="text-sm text-base-content/70">
                {preview.url} proved it holds the identity the invite names. Its owner will be able to read what you post in its
                channels. Direct messages are end-to-end encrypted, so they can't read those, though they see who talks to whom.
            </p>
            <form class="flex flex-col gap-2" onSubmit={join}>
                <AccountFields form={form} set={set} />
                <ErrorText message={error} />
                <div class="flex gap-2">
                    <SubmitButton busy={busy}>Join</SubmitButton>
                    <button type="button" class="btn btn-ghost" onClick={back} disabled={busy}>
                        Back
                    </button>
                </div>
            </form>
        </Card>
    );
}

// SignInDen signs this computer in to a den the member is in already: with
// the den password, or with a recovery code that sets a new one.
function SignInDen({ prefill, onDone }) {
    const empty = { den: prefill?.den || '', username: prefill?.username || '', password: '', code: '', confirm: '' };
    const [form, set, setForm] = useForm(empty);
    const [recovering, setRecovering] = useState(false);
    const [done, setDone] = useState('');
    const act = useAction();
    const card = useRef(null);
    useEffect(() => {
        if (prefill) card.current?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }, []);

    function submit(e) {
        e.preventDefault();
        setDone('');
        if (recovering) {
            const problem = checkPasswords(form.password, form.confirm);
            if (problem) return act.setError(problem);
        }
        act.run(async () => {
            const result = await api.post('/api/dens/signin', {
                den: form.den, username: form.username, password: form.password, recovery_code: recovering ? form.code : '',
            });
            setForm({ ...empty, den: '', username: '' });
            setRecovering(false);
            const left = result.recovery_codes_left;
            setDone(
                result.pending
                    ? 'Almost there: approve this sign-in on one of your other devices, as shown at the top of this page.'
                    : `Signed in to ${result.den.name}.${signedOut(result.signed_out)} You have ${left} recovery code${left === 1 ? '' : 's'} left; you can make new ones under Devices and password. To read your direct messages here, type your DM seal where the den is listed.`,
            );
            await onDone();
        });
    }

    return (
        <div ref={card}>
            <Card title="Sign in to a den you're in">
                <p class="text-sm text-base-content/70">
                    For a den you joined on another computer, or one that signed this device out. With your password, one of your other
                    devices there approves this one and passes it your DM seal. With no other device left, use a recovery code, then
                    type your DM seal.
                </p>
                <form class="flex flex-col gap-2" onSubmit={submit}>
                    <Field
                        label="Invite or den address"
                        hint="Any invite from the den works, even a used one: it names the den exactly. An address like https://den.example.com works too; the den ID it shows afterwards should match another member's."
                    >
                        <TextInput value={form.den} onInput={(v) => set('den', v)} required />
                    </Field>
                    <Field label="Username">
                        <TextInput value={form.username} onInput={(v) => set('username', v)} autocomplete="username" required />
                    </Field>
                    {recovering ? (
                        <>
                            <Field label="Recovery code" hint="One of the codes you saved when you joined. It works once.">
                                <TextInput value={form.code} onInput={(v) => set('code', v)} autocomplete="off" required class="input w-full font-mono" />
                            </Field>
                            <PasswordFields password={form.password} setPassword={(v) => set('password', v)} confirm={form.confirm}
                                setConfirm={(v) => set('confirm', v)} hint="Your new password for this den." />
                        </>
                    ) : (
                        <Field label="Den password">
                            <TextInput type="password" value={form.password} onInput={(v) => set('password', v)} autocomplete="current-password" required />
                        </Field>
                    )}
                    <ErrorText message={act.error} />
                    {done && <p class="text-sm text-success">{done}</p>}
                    <div class="flex flex-wrap items-center gap-2">
                        <SubmitButton busy={act.busy}>{recovering ? 'Recover and sign in' : 'Sign in'}</SubmitButton>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => {
                            setRecovering(!recovering);
                            act.setError('');
                        }}>
                            {recovering ? 'I know my password' : 'Forgot your password?'}
                        </button>
                    </div>
                </form>
            </Card>
        </div>
    );
}

function HostDen({ hosting, onJoined }) {
    const [form, set] = useForm({ ...EMPTY_ACCOUNT, name: '', url: 'https://' });
    const { busy, error, setError, run } = useAction();

    function submit(e) {
        e.preventDefault();
        const problem = checkPasswords(form.password, form.confirm);
        if (problem) return setError(problem);
        run(async () => {
            const result = await api.post('/api/den', {
                name: form.name, url: form.url, username: form.username, display_name: form.displayName, password: form.password,
            });
            onJoined(result);
        });
    }

    return (
        <Card title={hosting.created ? `Finish setting up ${hosting.name}` : 'Create your den'}>
            <p class="text-sm text-base-content/70">
                {hosting.created
                    ? "Your den exists, but its owner account doesn't yet. Create it to manage the den."
                    : 'This install hosts a den. Name it, give the address Caddy serves it at, and create your owner account.'}
            </p>
            <form class="flex flex-col gap-2" onSubmit={submit}>
                {!hosting.created && (
                    <>
                        <Field label="Den name">
                            <TextInput value={form.name} onInput={(v) => set('name', v)} required maxlength="32" />
                        </Field>
                        <Field
                            label="Public address"
                            hint="Where members reach the den through Caddy, such as https://den.example.com. You can test and change it later in the den's settings."
                        >
                            <TextInput type="url" value={form.url} onInput={(v) => set('url', v)} required />
                        </Field>
                    </>
                )}
                <AccountFields form={form} set={set} />
                <ErrorText message={error} />
                <SubmitButton busy={busy}>{hosting.created ? 'Create owner account' : 'Create den'}</SubmitButton>
            </form>
        </Card>
    );
}

// RecoveryCodes shows a den's recovery codes once, and the DM seal beside
// them when there is one to keep; after starting over, the seal alone.
function RecoveryCodes({ denName, codes, seal, sealNew, onDone }) {
    const [saved, setSaved] = useState(false);
    const text = codes.join('\n');
    const what = codes.length && seal ? 'recovery codes and DM seal' : codes.length ? 'recovery codes' : 'DM seal';
    return (
        <Card title={`Save your ${what} for ${denName}`}>
            {codes.length > 0 && (
                <>
                    <p>
                        If you forget your password for this den, one of these codes lets you set a new one. Each works once. They're
                        shown only now: store them somewhere safe, away from this computer.
                    </p>
                    <ol class="grid grid-cols-2 gap-2 font-mono text-sm">
                        {codes.map((code) => <li key={code} class="rounded bg-base-100 px-3 py-1">{code}</li>)}
                    </ol>
                    <div><CopyButton text={text} label="Copy codes" /></div>
                </>
            )}
            {seal && <SealShown seal={seal} fresh={sealNew} />}
            <label class="label gap-2">
                <input type="checkbox" class="checkbox" checked={saved} onChange={(e) => setSaved(e.currentTarget.checked)} />
                I've stored my {what}
            </label>
            <div>
                <button type="button" class="btn btn-primary" disabled={!saved} onClick={onDone}>Done</button>
            </div>
        </Card>
    );
}
