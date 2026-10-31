// The home page, a launcher (M8.3): the dens this install has joined, each
// a row that opens it or its Manage dialog; sign-ins and approvals waiting
// on this device above them; adding a den, from an invite or an address;
// and setting up the den this install hosts, once hosting is on (M9.1),
// while it has no owner (M10).

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import { DocsTip, ErrorText, Field, PasswordFields, SubmitButton, TextInput, Waiting, checkPasswords, useAction } from './components.jsx';
import { openManage } from './dialogs.js';
import { restartDens, updateNotice } from './flatpak.js';
import { ownRowNote, setupNeeded } from './hosting.js';
import { readPasted } from './join.js';
import { Dialog } from './manage.jsx';
import { KeyNeeded, PendingSignIns, RecoveryCodes, SignInRequests, SignInSteps } from './private.jsx';
import { openSettings } from './settings.jsx';

const PASSWORD_HINT = "You'll need it to sign in on a new device. The den never sees it, so reusing one across dens is safe.";

export function Home({ status, navigate }) {
    const [view, setView] = useState(null);
    const [codes, setCodes] = useState(null);
    // adding is what the Add a den dialog starts from while it's open: a
    // den to sign in to again, or a sign-in to finish with a recovery code,
    // and the sign-in it follows once one waits for approval.
    const [adding, setAdding] = useState(null);

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
        return (
            <section class="card bg-base-200">
                <div class="card-body">
                    <RecoveryCodes denName={codes.name} codes={codes.codes} seal={codes.seal} sealNew={codes.sealNew} onDone={() => setCodes(null)} />
                </div>
            </section>
        );
    }
    const joined = (result) => {
        setAdding(null);
        setCodes({ name: result.den.name, codes: result.recovery_codes, seal: result.seal, sealNew: result.seal_new });
        refresh();
    };
    // A new key, from a reset, shows once like a join's.
    const showKey = (name, seal) => setCodes({ name, codes: [], seal, sealNew: true });
    const recover = (p) => setAdding({ den: p.url, username: p.username, recover: true });
    const connected = view ? view.dens.filter((d) => d.state === 'connected') : [];

    return (
        <>
            <UpdateNotice status={status} />
            {!view ? (
                <Waiting label="Waiting for Dens on this computer…" />
            ) : (
                <>
                    <PendingSignIns list={(view.sign_ins || []).filter((p) => p.id !== adding?.pending)} onRecover={recover} />
                    {connected.map((d) => <SignInRequests key={d.den_id} den={d} />)}
                    {connected.filter((d) => !d.seal).map((d) => <KeyNeeded key={d.den_id} den={d} onSeal={(seal) => showKey(d.name, seal)} />)}
                    {setupNeeded(view.hosting) && <HostDen hosting={view.hosting} onJoined={joined} />}
                    <DenList dens={view.dens} hosting={view.hosting} navigate={navigate} onChanged={refresh} onAdd={() => setAdding({})}
                        onSignIn={(den) => setAdding({ den: den.url, username: den.username })} />
                </>
            )}
            {adding && view && (
                <AddDen start={adding} signIns={view.sign_ins || []} hosting={view.hosting.enabled} onJoined={joined} onClose={() => setAdding(null)}
                    onPending={(id) => setAdding((a) => ({ ...a, pending: id }))} />
            )}
        </>
    );
}

// UpdateNotice says an update is there, and what installs it: a command for
// an installed Dens; for the Flatpak, a button where Dens may install it or
// restart into it, or the software center (M9.3); and in the Android app, a
// button that has Android's installer ask the member to confirm it, over
// the page (M12.4).
function UpdateNotice({ status }) {
    const notice = updateNotice(status);
    const act = useAction();
    if (!notice) return null;
    const go = () => act.run(async () => {
        if (notice.action === 'install') await api.post('/api/update');
        else await restartDens(notice.action === 'update' ? '/api/update' : '/api/restart');
    });
    return (
        <div role="status" class="alert alert-info flex flex-wrap">
            <span>
                {notice.text}
                {notice.command && <> <code>{notice.command}</code></>}
            </span>
            {notice.action && (
                <button type="button" class="btn btn-sm" disabled={act.busy} onClick={go}>
                    {act.busy && <span class="loading loading-spinner loading-sm"></span>}
                    {act.busy ? (notice.action === 'install' ? 'Opening the installer…' : 'Restarting…') : notice.label}
                </button>
            )}
            <ErrorText message={act.error} />
        </div>
    );
}

// STATES are the dot and the words for a den's state on this computer.
const STATES = {
    connected: ['bg-success', 'Connected'],
    connecting: ['bg-info', 'Connecting'],
    offline: ['bg-warning', 'Offline'],
    revoked: ['bg-error', 'Signed out'],
    removed: ['bg-error', 'Not a member'],
};

const ROLES = { owner: 'Owner', moderator: 'Moderator', member: 'Member' };

function DenList({ dens, hosting, navigate, onChanged, onSignIn, onAdd }) {
    return (
        <section class="card bg-base-200">
            <div class="card-body gap-3">
                <div class="flex items-center gap-2">
                    <h2 class="card-title flex-1">Your dens</h2>
                    <button type="button" class="btn btn-sm" onClick={onAdd}>Add a den</button>
                </div>
                {dens.length === 0 ? (
                    <p>You haven't joined any dens yet. Add one with an invite.</p>
                ) : (
                    <ul class="flex flex-col gap-2">
                        {dens.map((den) => (
                            <DenRow key={den.den_id} den={den} note={ownRowNote(den, hosting)} navigate={navigate} onChanged={onChanged}
                                onSignIn={onSignIn} />
                        ))}
                    </ul>
                )}
            </div>
        </section>
    );
}

// DenRow is one den in the launcher: a dot for its state, named in words
// when it isn't connected, what waits there (M13.1), who the member is
// there, and Open and Manage, or, for a den that let this device or its
// member go, signing in again or forgetting it. The den this computer
// hosts, with hosting off, says so, with note, and leads to the switch
// (M9.1), or to Hosting once it's exported (M10).
function DenRow({ den, note, navigate, onChanged, onSignIn }) {
    const [dot, label] = note ? ['bg-base-content/40', note.label] : STATES[den.state] || ['bg-base-content/40', den.state];
    const gone = den.state === 'revoked' || den.state === 'removed';
    const forget = useAction();
    const who = gone ? `@${den.username}` : `Signed in as ${den.display_name} · ${ROLES[den.role] || den.role}`;
    return (
        <li class="flex flex-wrap items-center gap-3 rounded-box bg-base-100 p-3">
            <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                    <span role="img" aria-label={label} title={label} class={`h-2.5 w-2.5 shrink-0 rounded-full ${dot}`}></span>
                    <span class="truncate text-lg font-semibold">{den.name}</span>
                    <DenMarks den={den} />
                    {den.state !== 'connected' && <span class="shrink-0 text-sm text-base-content/60">{label}</span>}
                </div>
                <p class="truncate text-sm text-base-content/70">
                    {who}
                    {den.own && ' · Hosted here'}
                </p>
                {note ? (
                    <p class="text-sm text-base-content/70">
                        {note.text}{' '}
                        <button type="button" class="link" onClick={() => openSettings('system')}>{note.action}</button>
                    </p>
                ) : den.error && <p class="text-sm text-warning">{den.error}</p>}
                <ErrorText message={forget.error} />
            </div>
            {gone ? (
                <div class="flex flex-wrap gap-2">
                    {den.state === 'revoked' && <button type="button" class="btn btn-primary btn-sm" onClick={() => onSignIn(den)}>Sign in again</button>}
                    <button type="button" class="btn btn-sm" disabled={forget.busy} onClick={() => forget.run(async () => {
                        await api.del(`/api/dens/${den.den_id}`);
                        await onChanged();
                    })}>
                        Remove from this computer
                    </button>
                </div>
            ) : (
                <div class="flex items-center gap-2">
                    <button type="button" class="btn btn-ghost btn-sm" onClick={() => openManage(den.den_id)}>Manage</button>
                    <button type="button" class="btn btn-primary" onClick={() => navigate(`/den/${den.den_id}`)}>Open</button>
                </div>
            )}
        </li>
    );
}

// DenMarks marks a den's row with its mentions, counted, or a dot for its
// unread messages, as its channel list marks a channel.
function DenMarks({ den }) {
    if (den.mentions > 0) {
        const words = den.mentions === 1 ? '1 mention' : `${den.mentions} mentions`;
        return <span class="badge badge-error badge-sm shrink-0" aria-label={words} title={words}>{den.mentions}</span>;
    }
    if (den.unread > 0) return <span role="img" class="h-2 w-2 shrink-0 rounded-full bg-base-content" aria-label="Unread" title="Unread"></span>;
    return null;
}

function useForm(initial) {
    const [form, setForm] = useState(initial);
    return [form, (key, value) => setForm((f) => ({ ...f, [key]: value })), setForm];
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
            <PasswordFields password={form.password} setPassword={(v) => set('password', v)} confirm={form.confirm}
                setConfirm={(v) => set('confirm', v)} hint={PASSWORD_HINT} />
        </>
    );
}

// AddDen adds a den from one pasted thing (M8.3): an invite, which shows
// its den and then joins it as someone new or signs in to an account
// there, or a den's address, which signs in. A sign-in that waits for
// another device's approval goes on a step at a time, from the service's
// record of it.
function AddDen({ start, signIns, hosting, onJoined, onClose, onPending }) {
    const again = !!start.den;
    const [step, setStep] = useState(again ? 'signin' : 'paste');
    const [text, setText] = useState('');
    const [den, setDen] = useState(start.den || '');
    const [preview, setPreview] = useState(null);
    const [recovering, setRecovering] = useState(!!start.recover);
    const [form, set, setForm] = useForm({ username: start.username || '', displayName: '', password: '', confirm: '', code: '' });
    const [done, setDone] = useState(null);
    const act = useAction();
    const pending = start.pending ? signIns.find((p) => p.id === start.pending) : null;

    function paste(e) {
        e.preventDefault();
        const pasted = readPasted(text);
        if (!pasted.kind) {
            return act.setError("That isn't an invite or a den's address. Invites start with dens1:, and addresses look like https://den.example.com.");
        }
        setDen(pasted.value);
        if (pasted.kind === 'address') return setStep('signin');
        act.run(async () => {
            const data = await api.post('/api/dens/preview', { invite: pasted.value });
            setPreview(data.den);
            setStep('choose');
        });
    }

    function join(e) {
        e.preventDefault();
        const problem = checkPasswords(form.password, form.confirm);
        if (problem) return act.setError(problem);
        act.run(async () => {
            onJoined(await api.post('/api/dens/join', { invite: den, username: form.username, display_name: form.displayName, password: form.password }));
        });
    }

    function signIn(e) {
        e.preventDefault();
        if (recovering) {
            const problem = checkPasswords(form.password, form.confirm);
            if (problem) return act.setError(problem);
        }
        act.run(async () => {
            const result = await api.post('/api/dens/signin', {
                den, username: form.username, password: form.password, recovery_code: recovering ? form.code : '',
            });
            setForm((f) => ({ ...f, password: '', confirm: '', code: '' }));
            if (result.pending) return onPending(result.pending.id);
            setDone({ name: result.den.name, left: result.recovery_codes_left, signedOut: result.signed_out, key: result.den.seal });
            setStep('done');
        });
    }

    function back(to) {
        act.setError('');
        setStep(to);
    }

    let title = 'Add a den';
    let body;
    if (start.pending) {
        title = 'Signing in';
        body = pending ? (
            <SignInSteps p={pending} onDone={onClose} onRecover={() => {
                onPending(null);
                setRecovering(true);
                setStep('signin');
            }} />
        ) : (
            <div class="flex flex-col gap-2">
                <span class="loading loading-dots loading-sm"></span>
                <div><button type="button" class="btn btn-ghost btn-sm" onClick={onClose}>Close</button></div>
            </div>
        );
    } else if (step === 'paste') {
        body = (
            <form class="flex flex-col gap-2" onSubmit={paste}>
                <Field label="Paste an invite or den address">
                    <TextInput value={text} onInput={setText} placeholder="dens1:… or https://den.example.com" autocomplete="off" required autofocus />
                </Field>
                <ErrorText message={act.error} />
                <div><SubmitButton busy={act.busy}>Continue</SubmitButton></div>
                {!hosting && (
                    <p class="mt-2 text-sm text-base-content/70">
                        Want a den of your own?{' '}
                        <button type="button" class="link" onClick={() => {
                            onClose();
                            openSettings('system');
                        }}>
                            Host one on this computer
                        </button>
                    </p>
                )}
            </form>
        );
    } else if (step === 'choose') {
        title = `Join ${preview.name}`;
        body = (
            <div class="flex flex-col gap-3">
                <p class="text-sm text-base-content/70">The den at {preview.url} proved it's the one this invite names.</p>
                <div class="flex flex-col gap-2">
                    <button type="button" class="btn btn-primary" onClick={() => back('join')}>Join as someone new</button>
                    <button type="button" class="btn" onClick={() => back('signin')}>Sign in to my account there</button>
                </div>
                <div><button type="button" class="btn btn-ghost btn-sm" onClick={() => back('paste')}>Back</button></div>
            </div>
        );
    } else if (step === 'join') {
        title = `Join ${preview.name}`;
        body = (
            <form class="flex flex-col gap-2" onSubmit={join}>
                <AccountFields form={form} set={set} />
                <ErrorText message={act.error} />
                <div class="flex gap-2">
                    <SubmitButton busy={act.busy}>Join</SubmitButton>
                    <button type="button" class="btn btn-ghost" disabled={act.busy} onClick={() => back('choose')}>Back</button>
                </div>
            </form>
        );
    } else if (step === 'signin') {
        title = `Sign in to ${preview?.name || den.replace(/^https?:\/\//, '').replace(/^dens1:.*/, 'the den')}`;
        body = (
            <form class="flex flex-col gap-2" onSubmit={signIn}>
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
                <div>
                    <button type="button" class="link text-sm" onClick={() => {
                        setRecovering(!recovering);
                        act.setError('');
                    }}>
                        {recovering ? 'I know my password' : 'Forgot your password? Use a recovery code'}
                    </button>
                </div>
                <ErrorText message={act.error} />
                <div class="flex gap-2">
                    <SubmitButton busy={act.busy}>Sign in</SubmitButton>
                    {!again && <button type="button" class="btn btn-ghost" disabled={act.busy} onClick={() => back(preview ? 'choose' : 'paste')}>Back</button>}
                </div>
            </form>
        );
    } else if (step === 'done') {
        title = 'Signed in';
        const left = done.left === 1 ? '1 recovery code left' : `${done.left} recovery codes left`;
        const others = !done.signedOut ? '' : done.signedOut === 1 ? ' 1 other device was signed out.' : ` ${done.signedOut} other devices were signed out.`;
        body = (
            <div class="flex flex-col gap-3">
                <p>
                    You're signed in to {done.name}, with {left}.{others}
                </p>
                {!done.key && <p class="text-sm text-base-content/70">To read your direct messages here, enter your DM recovery key: the home page asks for it.</p>}
                <div><button type="button" class="btn btn-primary" onClick={onClose}>Done</button></div>
            </div>
        );
    }
    return <Dialog title={title} onClose={onClose}>{body}</Dialog>;
}

function HostDen({ hosting, onJoined }) {
    const [form, set] = useForm({ username: '', displayName: '', password: '', confirm: '', name: '', url: 'https://' });
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
        <section class="card bg-base-200">
            <div class="card-body gap-3">
                <h2 class="card-title">
                    {hosting.created ? `Finish setting up ${hosting.name}` : 'Create your den'}
                    <DocsTip id="docs-host" page="host/">The whole setup, step by step: a domain, Caddy and your router.</DocsTip>
                </h2>
                <p class="text-sm text-base-content/70">
                    {hosting.created
                        ? "Your den exists, but its owner account doesn't yet. Create it to manage the den."
                        : 'Hosting is on. Name your den, give the address Caddy serves it at, and create your owner account.'}
                </p>
                {!hosting.serving && hosting.problem && <p role="status" class="text-sm text-warning">{hosting.problem}</p>}
                <form class="flex flex-col gap-2" onSubmit={submit}>
                    {!hosting.created && (
                        <>
                            <Field label="Den name">
                                <TextInput value={form.name} onInput={(v) => set('name', v)} required maxlength="32" />
                            </Field>
                            <Field label="Public address" hint="The address members use to connect, such as https://den.example.com.">
                                <TextInput type="url" value={form.url} onInput={(v) => set('url', v)} required />
                            </Field>
                        </>
                    )}
                    <AccountFields form={form} set={set} />
                    <ErrorText message={error} />
                    <div><SubmitButton busy={busy}>{hosting.created ? 'Create owner account' : 'Create den'}</SubmitButton></div>
                </form>
            </div>
        </section>
    );
}
