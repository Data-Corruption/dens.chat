// The home page: the dens this install has joined, joining another,
// hosting one, and inviting people to a den you own.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { onConnection, onEvent } from './events.js';
import {
    Card, CopyButton, ErrorText, Field, PasswordFields, SubmitButton, TextInput, checkPasswords, useAction,
} from './components.jsx';

const VERIFIER_HINT =
    "You'll need this password to sign in to the den from a new device. The den never sees it: it gets a value derived from " +
    "it that is different for every den, so reusing a password across dens is safe.";

export function Home({ status, navigate }) {
    const [view, setView] = useState(null);
    const [live, setLive] = useState(true);
    const [codes, setCodes] = useState(null);

    useEffect(() => {
        const stopEvents = onEvent((message) => {
            if (message.t === 'dens') setView(message.d);
        });
        const stopConnection = onConnection(setLive);
        return () => {
            stopEvents();
            stopConnection();
        };
    }, []);

    if (codes) {
        return <RecoveryCodes denName={codes.name} codes={codes.codes} onDone={() => setCodes(null)} />;
    }
    const joined = (result) => setCodes({ name: result.den.name, codes: result.recovery_codes });

    return (
        <>
            {status.updateVersion && (
                <div role="status" class="alert alert-info">
                    <span>
                        Dens {status.updateVersion} is available. To update, run: <code>{status.updateCommand}</code>
                    </span>
                </div>
            )}
            {!live && (
                <div role="status" class="alert alert-warning alert-soft">
                    <span>Lost touch with the Dens service on this computer; reconnecting…</span>
                </div>
            )}
            {!view ? (
                <span class="loading loading-spinner"></span>
            ) : (
                <>
                    {view.hosting.enabled && !view.hosting.joined && <HostDen hosting={view.hosting} onJoined={joined} />}
                    <DenList dens={view.dens} navigate={navigate} />
                    <JoinDen onJoined={joined} />
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
};

function DenList({ dens, navigate }) {
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
                {dens.map((den) => <DenItem key={den.den_id} den={den} navigate={navigate} />)}
            </ul>
        </Card>
    );
}

function DenItem({ den, navigate }) {
    const [badge, label] = STATE_BADGES[den.state] || ['badge-ghost', den.state];
    return (
        <li class="rounded-box bg-base-100 p-4">
            <div class="flex flex-wrap items-center gap-2">
                <span class="text-lg font-semibold">{den.name}</span>
                <span class={`badge ${badge}`}>{label}</span>
                {den.own && <span class="badge badge-outline">Hosted here</span>}
                {den.state !== 'revoked' && (
                    <button type="button" class="btn btn-primary btn-sm ml-auto" onClick={() => navigate(`/den/${den.den_id}`)}>
                        Open
                    </button>
                )}
            </div>
            <p class="text-sm text-base-content/70">
                {den.url} · you're <span class="font-medium">{den.display_name}</span> (@{den.username}), {den.role}
            </p>
            {den.error && <p class="text-sm text-warning">{den.error}</p>}
            {den.role === 'owner' && den.state === 'connected' && (
                <>
                    <DenSettings den={den} />
                    <Invites denID={den.den_id} />
                </>
            )}
        </li>
    );
}

// DenSettings changes a den's name and public address, and tests whether
// the address reaches the den from this computer.
function DenSettings({ den }) {
    const [name, setName] = useState(den.name);
    const [url, setURL] = useState(den.url);
    const [saved, setSaved] = useState(false);
    const [reached, setReached] = useState(false);
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

    return (
        <details class="collapse-arrow collapse mt-2 bg-base-200">
            <summary class="collapse-title font-medium">Den settings</summary>
            <div class="collapse-content flex flex-col gap-3">
                <form class="flex flex-col gap-2" onSubmit={submit}>
                    <Field label="Den name">
                        <TextInput value={name} onInput={setName} required maxlength="32" />
                    </Field>
                    <Field
                        label="Public address"
                        hint="Where members reach the den. New invites carry it; invites already sent keep the old one."
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
            </div>
        </details>
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

    return (
        <details class="collapse-arrow collapse mt-2 bg-base-200">
            <summary class="collapse-title font-medium">Invite people</summary>
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
                    <button type="button" class="btn btn-ghost" onClick={() => setPreview(null)} disabled={busy}>
                        Back
                    </button>
                </div>
            </form>
        </Card>
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
