// The home page, a launcher (M8.3): the dens this install has joined, each
// a row that opens it or its Manage dialog, under a header with Add a den
// and the settings' cog; sign-ins and approvals waiting on this device
// above them; adding a den, from an invite or an address; and setting up
// the den this install hosts, once hosting is on (M9.1), while it has no
// owner (M10). With no den, it's the mark and Add a den alone (M22.4).

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import {
    ADDRESS, Actions, Button, DocsTip, ErrorText, Field, Lead, PLAIN, PasswordFields, StatusText, SubmitButton, TextInput, Tip, Waiting, buttonClass, checkPasswords,
    useAction,
} from './components.jsx';
import { openManage, openSettings } from './dialogs.js';
import { ownRowNote, setupNeeded } from './hosting.js';
import { CogIcon } from './icons.jsx';
import { readPasted } from './join.js';
import { Dialog } from './manage.jsx';
import { KeyNeeded, PendingSignIns, RecoveryCodes, SignInRequests, SignInSteps } from './private.jsx';
import { UpdateNotice } from './update.jsx';

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
            <Lead>
                <section class="card bg-base-200">
                    <div class="card-body">
                        <RecoveryCodes den={codes.den} codes={codes.codes} seal={codes.seal} sealNew={codes.sealNew} onDone={() => setCodes(null)} />
                    </div>
                </section>
            </Lead>
        );
    }
    const joined = (result) => {
        setAdding(null);
        setCodes({ den: result.den, codes: result.recovery_codes, seal: result.seal, sealNew: result.seal_new });
        refresh();
    };
    // A new key, from a reset, shows once like a join's.
    const showKey = (den, seal) => setCodes({ den, codes: [], seal, sealNew: true });
    const recover = (p) => setAdding({ den: p.url, username: p.username, recover: true });
    const connected = view ? view.dens.filter((d) => d.state === 'connected') : [];
    const offerHosting = !!view?.hosting.can_host && !view.hosting.enabled;

    return (
        <>
            <UpdateNotice status={status} />
            {!view ? (
                <Waiting label="Waiting for Dens on this device…" />
            ) : (
                <>
                    <PendingSignIns list={(view.sign_ins || []).filter((p) => p.id !== adding?.pending?.id)} onRecover={recover} />
                    {connected.map((d) => <SignInRequests key={d.den_id} den={d} />)}
                    {connected.filter((d) => !d.seal).map((d) => <KeyNeeded key={d.den_id} den={d} onSeal={(seal) => showKey(d, seal)} />)}
                    {setupNeeded(view.hosting) && <HostDen hosting={view.hosting} onJoined={joined} />}
                    {view.dens.length === 0 && !setupNeeded(view.hosting) ? (
                        <NoDens offerHosting={offerHosting} onAdd={() => setAdding({})} />
                    ) : (
                        <DenList dens={view.dens} hosting={view.hosting} navigate={navigate} onChanged={refresh} onAdd={() => setAdding({})}
                            onSignIn={(den) => setAdding({ den: den.url, username: den.username })} />
                    )}
                </>
            )}
            {adding && view && (
                <AddDen start={adding} signIns={view.sign_ins || []} offerHosting={offerHosting} onJoined={joined}
                    onClose={() => setAdding(null)}
                    onPending={(pending) => setAdding((a) => ({ ...a, pending }))} />
            )}
        </>
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

// SettingsCog opens the settings from Home, where a den's page has the cog
// beside the member's name: a link to where they were a page of their own,
// which the page opens over Home.
function SettingsCog() {
    return (
        <a href="/settings" class={`${buttonClass('quiet')} btn-square`} aria-label="Settings" title="Settings"
            onClick={(e) => {
                e.preventDefault();
                openSettings();
            }}>
            <CogIcon />
        </a>
    );
}

// NoDens is Home with no den: the mark, a line and Add a den, with hosting
// one under it where this install can, and the settings' cog.
function NoDens({ offerHosting, onAdd }) {
    return (
        <Lead>
            <section class="card relative bg-base-200" data-no-dens>
                <div class="card-body items-center gap-4 text-center">
                    <div class="absolute right-3 top-3"><SettingsCog /></div>
                    <p class="text-lg">You're not in a den yet. An invite brings you into one.</p>
                    <Button kind="primary" size="md" onClick={onAdd}>Add a den</Button>
                    {offerHosting && (
                        <button type="button" class="link link-primary min-h-6 text-sm" onClick={() => openSettings('system')}>Host one on this computer</button>
                    )}
                </div>
            </section>
        </Lead>
    );
}

function DenList({ dens, hosting, navigate, onChanged, onSignIn, onAdd }) {
    return (
        <section class="card bg-base-200">
            <div class="card-body gap-3">
                <div class="flex items-center gap-2">
                    <h2 class="card-title flex-1">Your dens</h2>
                    <Button kind="primary" onClick={onAdd}>Add a den</Button>
                    <SettingsCog />
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
// member go, signing in again or forgetting it; on the phone layout they
// sit under the den's name, which keeps the row's width (M13.3). The den
// this computer hosts, with hosting off, says so, with note, and leads to
// the switch (M9.1), or to Hosting once it's exported (M10).
function DenRow({ den, note, navigate, onChanged, onSignIn }) {
    const [dot, label] = note ? ['bg-base-content/40', note.label] : STATES[den.state] || ['bg-base-content/40', den.state];
    const gone = den.state === 'revoked' || den.state === 'removed';
    const forget = useAction();
    const who = gone ? `@${den.username}` : `Signed in as ${den.display_name} · ${ROLES[den.role] || den.role}`;
    return (
        <li class="flex flex-col gap-3 rounded-box bg-base-100 p-3 md:flex-row md:items-center">
            <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                    <Tip id={`den-state-${den.den_id}`} label={label} trigger={<span class={`h-2.5 w-2.5 shrink-0 rounded-full ${dot}`}></span>}>{label}</Tip>
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
                        <button type="button" class="link link-primary" onClick={() => openSettings('system')}>{note.action}</button>
                    </p>
                ) : den.error && <StatusText kind="warning">{den.error}</StatusText>}
                <ErrorText message={forget.error} />
            </div>
            {gone ? (
                <div class="flex flex-wrap justify-end gap-2">
                    {den.state === 'revoked' && <Button onClick={() => onSignIn(den)}>Sign in again</Button>}
                    <Button kind="danger" busy={forget.busy} onClick={() => forget.run(async () => {
                        await api.del(`/api/dens/${den.den_id}`);
                        await onChanged();
                    })}>
                        Remove from this device
                    </Button>
                </div>
            ) : (
                <div class="flex items-center justify-end gap-2">
                    <Button kind="quiet" onClick={() => openManage(den.den_id)}>Manage</Button>
                    <Button size="md" onClick={() => navigate(`/den/${den.den_id}`)}>Open</Button>
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
                <TextInput value={form.username} onInput={(v) => set('username', v)} autocomplete="username" required {...PLAIN} />
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
// record of it. Where this install could host a den and doesn't,
// offerHosting, it leads there too (M13.3).
function AddDen({ start, signIns, offerHosting, onJoined, onClose, onPending }) {
    const again = !!start.den;
    const [step, setStep] = useState(again ? 'signin' : 'paste');
    const [text, setText] = useState('');
    const [den, setDen] = useState(start.den || '');
    const [preview, setPreview] = useState(null);
    const [recovering, setRecovering] = useState(!!start.recover);
    const [form, set, setForm] = useForm({ username: start.username || '', displayName: '', password: '', confirm: '', code: '' });
    const [done, setDone] = useState(null);
    const act = useAction();
    // The request's answer shows the sign-in until the service's list has
    // it, and its leaving the list then ends it.
    const listed = start.pending ? signIns.find((p) => p.id === start.pending.id) : null;
    const seen = useRef('');
    if (listed) seen.current = listed.id;
    const pending = listed || (start.pending && seen.current !== start.pending.id ? start.pending : null);

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
            if (result.pending) return onPending(result.pending);
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
                <Actions><Button kind="quiet" onClick={onClose}>Close</Button></Actions>
            </div>
        );
    } else if (step === 'paste') {
        body = (
            <form class="flex flex-col gap-2" onSubmit={paste}>
                <Field label="Paste an invite or den address">
                    <TextInput value={text} onInput={setText} placeholder="dens1:… or https://den.example.com" autocomplete="off" required autofocus {...ADDRESS} />
                </Field>
                <ErrorText message={act.error} />
                <Actions>
                    <SubmitButton busy={act.busy}>Continue</SubmitButton>
                    <Button kind="quiet" onClick={onClose}>Cancel</Button>
                </Actions>
                {offerHosting && (
                    <p class="mt-2 text-sm text-base-content/70">
                        Want a den of your own?{' '}
                        <button type="button" class="link link-primary" onClick={() => {
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
                <Actions>
                    <Button kind="primary" onClick={() => back('join')}>Join as someone new</Button>
                    <Button onClick={() => back('signin')}>Sign in to my account there</Button>
                    <Button kind="quiet" onClick={() => back('paste')}>Back</Button>
                </Actions>
            </div>
        );
    } else if (step === 'join') {
        title = `Join ${preview.name}`;
        body = (
            <form class="flex flex-col gap-2" onSubmit={join}>
                <AccountFields form={form} set={set} />
                <ErrorText message={act.error} />
                <Actions>
                    <SubmitButton busy={act.busy}>Join</SubmitButton>
                    <Button kind="quiet" disabled={act.busy} onClick={() => back('choose')}>Back</Button>
                </Actions>
            </form>
        );
    } else if (step === 'signin') {
        title = `Sign in to ${preview?.name || den.replace(/^https?:\/\//, '').replace(/^dens1:.*/, 'the den')}`;
        body = (
            <form class="flex flex-col gap-2" onSubmit={signIn}>
                <Field label="Username">
                    <TextInput value={form.username} onInput={(v) => set('username', v)} autocomplete="username" required {...PLAIN} />
                </Field>
                {recovering ? (
                    <>
                        <Field label="Recovery code" hint="One of the codes you saved when you joined. It works once.">
                            <TextInput value={form.code} onInput={(v) => set('code', v)} autocomplete="off" required class="input w-full font-mono" {...PLAIN} />
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
                    <button type="button" class="link link-primary text-sm" onClick={() => {
                        setRecovering(!recovering);
                        act.setError('');
                    }}>
                        {recovering ? 'I know my password' : 'Forgot your password? Use a recovery code'}
                    </button>
                </div>
                <ErrorText message={act.error} />
                <Actions>
                    <SubmitButton busy={act.busy}>Sign in</SubmitButton>
                    {!again && <Button kind="quiet" disabled={act.busy} onClick={() => back(preview ? 'choose' : 'paste')}>Back</Button>}
                </Actions>
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
                <Actions><Button kind="primary" onClick={onClose}>Done</Button></Actions>
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
                    <DocsTip id="docs-host" page="hosting/">The whole setup, step by step: a domain, Caddy and your router.</DocsTip>
                </h2>
                <p class="text-sm text-base-content/70">
                    {hosting.created
                        ? "Your den exists, but its owner account doesn't yet. Create it to manage the den."
                        : 'Hosting is on. Name your den, give the address Caddy serves it at, and create your owner account.'}
                </p>
                {!hosting.serving && hosting.problem && <StatusText kind="warning" role="status">{hosting.problem}</StatusText>}
                <form class="flex flex-col gap-2" onSubmit={submit}>
                    {!hosting.created && (
                        <>
                            <Field label="Den name">
                                <TextInput value={form.name} onInput={(v) => set('name', v)} required maxlength="32" />
                            </Field>
                            <Field label="Public address" hint="The address members use to connect, such as https://den.example.com.">
                                <TextInput type="url" value={form.url} onInput={(v) => set('url', v)} required {...PLAIN} />
                            </Field>
                        </>
                    )}
                    <AccountFields form={form} set={set} />
                    <ErrorText message={error} />
                    <Actions><SubmitButton busy={busy} size="md">{hosting.created ? 'Create owner account' : 'Create den'}</SubmitButton></Actions>
                </form>
            </div>
        </section>
    );
}
