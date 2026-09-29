// The home page: the dens this install has joined, joining another,
// hosting one, and inviting people to a den you own.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import {
    Card, CopyButton, ErrorText, Field, PasswordFields, SubmitButton, TextInput, Waiting, checkPasswords, day, useAction,
    useLater,
} from './components.jsx';

const VERIFIER_HINT =
    "You'll need this password to sign in to the den from a new device. The den never sees it: it gets a value derived from " +
    "it that is different for every den, so reusing a password across dens is safe.";

export function Home({ status, navigate }) {
    const [view, setView] = useState(null);
    const [codes, setCodes] = useState(null);
    // signIn fills the sign-in form for a den this device was signed out of.
    const [signIn, setSignIn] = useState(null);

    useEffect(() => {
        // The list comes over plain HTTP first: the event stream's socket
        // can open late, since Firefox holds back sockets to an address
        // that recently refused them. What the stream sends is newer.
        let fresh = true;
        api.get('/api/dens').then((v) => fresh && setView((cur) => cur || v), () => {});
        const stop = onEvent((message) => {
            if (message.t === 'dens') setView(message.d);
        });
        return () => {
            fresh = false;
            stop();
        };
    }, []);

    if (codes) {
        return <RecoveryCodes denName={codes.name} codes={codes.codes} onDone={() => setCodes(null)} />;
    }
    const joined = (result) => setCodes({ name: result.den.name, codes: result.recovery_codes });
    const showCodes = (name, list) => setCodes({ name, codes: list });

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
                    {view.hosting.enabled && !view.hosting.joined && <HostDen hosting={view.hosting} onJoined={joined} />}
                    <DenList dens={view.dens} navigate={navigate} onCodes={showCodes}
                        onSignIn={(den) => setSignIn({ den: den.url, username: den.username, key: Date.now() })} />
                    <JoinDen onJoined={joined} />
                    <SignInDen key={signIn?.key} prefill={signIn} />
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

function DenList({ dens, navigate, onCodes, onSignIn }) {
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
                {dens.map((den) => <DenItem key={den.den_id} den={den} navigate={navigate} onCodes={onCodes} onSignIn={onSignIn} />)}
            </ul>
        </Card>
    );
}

function DenItem({ den, navigate, onCodes, onSignIn }) {
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
                            onClick={() => forget.run(() => api.del(`/api/dens/${den.den_id}`))}>
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
            {den.error && <p class="text-sm text-warning">{den.error}</p>}
            <ErrorText message={forget.error} />
            {den.state === 'offline' && !den.own && <NewAddress den={den} />}
            {den.state === 'connected' && (
                <>
                    {den.role === 'owner' && <DenSettings den={den} />}
                    {staff && <Invites denID={den.den_id} />}
                    {staff && <Bans denID={den.den_id} />}
                    <Devices den={den} onCodes={onCodes} />
                    {!den.own && <Leave den={den} />}
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
// and changes the den password and recovery codes.
function Devices({ den, onCodes }) {
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
                                        <span class="flex gap-1">
                                            <button type="button" class="btn btn-error btn-xs" disabled={act.busy} onClick={() => signOut(d)}>
                                                {d.current ? 'Sign this device out' : 'Sign it out'}
                                            </button>
                                            <button type="button" class="btn btn-ghost btn-xs" onClick={() => setConfirm('')}>Cancel</button>
                                        </span>
                                    ) : (
                                        <button type="button" class="btn btn-ghost btn-xs text-error" onClick={() => setConfirm(d.key_id)}>Sign out</button>
                                    )}
                                </li>
                            ))}
                        </ul>
                    )}
                    <p class="text-xs text-base-content/60">
                        Signing a device out ends its access at once. Signing in again takes your den password. Every new device shows
                        on your others, so if one appears that isn't yours, change your password below: that signs out every device but
                        this one.
                    </p>
                </div>
                <ChangePassword den={den} onDone={load} />
                {list !== null && <NewCodes den={den} left={list.recovery_codes_left} onCodes={onCodes} onDone={load} />}
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
function Leave({ den }) {
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
                        <button type="button" class="btn btn-error btn-sm" disabled={act.busy} onClick={() => act.run(() => api.post(`/api/dens/${den.den_id}/leave`))}>
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
                {preview.url} proved it holds the identity the invite names. Its owner will be able to read everything you post
                there, including direct messages.
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
function SignInDen({ prefill }) {
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
                recovering
                    ? `Signed in to ${result.den.name}.${signedOut(result.signed_out)} You have ${left} recovery code${left === 1 ? '' : 's'} left; you can make new ones under Devices and password.`
                    : `Signed in to ${result.den.name}.`,
            );
        });
    }

    return (
        <div ref={card}>
            <Card title="Sign in to a den you're in">
                <p class="text-sm text-base-content/70">
                    For a den you joined on another computer, or one that signed this device out. Your other devices will see that
                    this one was added.
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

function RecoveryCodes({ denName, codes, onDone }) {
    const [saved, setSaved] = useState(false);
    const text = codes.join('\n');
    return (
        <Card title={`Save your recovery codes for ${denName}`}>
            <p>
                If you forget your password for this den, one of these codes lets you set a new one. Each works once. They're shown
                only now: store them somewhere safe, away from this computer.
            </p>
            <ol class="grid grid-cols-2 gap-2 font-mono text-sm">
                {codes.map((code) => <li key={code} class="rounded bg-base-100 px-3 py-1">{code}</li>)}
            </ol>
            <div><CopyButton text={text} label="Copy codes" /></div>
            <label class="label gap-2">
                <input type="checkbox" class="checkbox" checked={saved} onChange={(e) => setSaved(e.currentTarget.checked)} />
                I've stored my recovery codes
            </label>
            <div>
                <button type="button" class="btn btn-primary" disabled={!saved} onClick={onDone}>Done</button>
            </div>
        </Card>
    );
}
