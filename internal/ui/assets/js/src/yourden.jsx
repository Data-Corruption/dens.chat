// A member's own sections of a den, which the settings hold as that
// den's group, above the app's, on its page or opened for it: their
// devices there, their den password, recovery codes and DM recovery key,
// their code for others to compare, their files, and leaving it.
// Manage keeps what staff manage.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { progressKey, progressLabel, sendSmaller } from './compare.jsx';
import {
    Actions, Button, Card, DocsTip, ErrorText, Field, Notice, PLAIN, PasswordFields, StatusText, SubmitButton, TextInput, checkPasswords, day, useAction,
    useLater,
} from './components.jsx';
import { onEvent } from './events.js';
import { addPreview, fileWhere, formatSize, needsPreview, thumbURL, upload } from './files.jsx';
import { openAt } from './messages.jsx';
import { leaveOffer } from './offline.js';
import { DMKey, RecoveryCodes } from './private.jsx';

// YOUR_SECTIONS are the ids of a member's own sections of a den.
export const YOUR_SECTIONS = ['devices', 'password', 'code', 'files', 'leave'];

// yourSections lists a member's own sections of a den, each [id, name,
// danger]: all but Leave den need the den connected, and the den this
// computer hosts has no Leave den.
export function yourSections(den) {
    const list = [];
    if (den.state === 'connected') list.push(['devices', 'Devices'], ['password', 'Password and recovery'], ['code', 'Your code'], ['files', 'Your files']);
    if (!den.own) list.push(['leave', 'Leave den', true]);
    return list;
}

// useYourDen is a den's status on this computer, kept current from the
// list's changes, and its state once it's connected, for its sections;
// each null until it comes, and both for no den.
export function useYourDen(denID) {
    const [den, setDen] = useState(null);
    const [view, setView] = useState(null);
    useEffect(() => {
        setDen(null);
        setView(null);
        if (!denID) return undefined;
        const take = (v) => setDen(v.dens.find((d) => d.den_id === denID) || null);
        api.get('/api/dens').then(take, () => {});
        return onEvent((m) => m.t === 'dens' && take(m.d));
    }, [denID]);
    const connected = den?.state === 'connected';
    useEffect(() => {
        if (connected) api.get(`/api/dens/${denID}/state`).then(setView, () => {});
    }, [connected, denID]);
    return { den, view };
}

// YourDen shows one of a member's own sections of a den, at, or the codes
// and key they just made until they're saved. onClose closes the settings,
// as opening a file's message and leaving do, and navigate goes there.
export function YourDen({ den, view, at, onClose, navigate }) {
    const [shown, setShown] = useState(null);
    if (shown) {
        return <RecoveryCodes den={den} codes={shown.codes} seal={shown.seal} sealNew={shown.sealNew} onDone={() => setShown(null)} />;
    }
    if (at === 'devices') return <Devices den={den} />;
    if (at === 'password') {
        return <PasswordAndRecovery den={den} onCodes={(codes) => setShown({ codes, seal: '', sealNew: false })}
            onKey={(seal) => setShown({ codes: [], seal, sealNew: true })} />;
    }
    if (at === 'code') return <YourCode den={den} view={view} />;
    if (at === 'files') {
        return <Files den={den} navigate={(to) => {
            onClose();
            navigate(to);
        }} />;
    }
    if (at === 'leave') {
        return <Leave den={den} onLeft={() => {
            onClose();
            navigate('/');
        }} />;
    }
    return null;
}

// labelParts splits a device's label into its name and, from the
// parentheses at its end, what it is: a computer's system or a phone's
// model, which shows under the name. A label with none is all
// name.
export function labelParts(label) {
    const m = /^(.*\S)\s*\(([^()]+)\)$/.exec(label || '');
    return m ? { name: m[1], detail: m[2] } : { name: label || '', detail: '' };
}

// A device added in the last day shows as new.
const NEW_FOR = 24 * 60 * 60 * 1000;

// Devices lists this member's devices in the den, each by its name with
// what it is under it, those just added marked, and signs any of them
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
        <Card title="Devices">
            <ErrorText message={act.error} />
            {list === null ? (
                slow && <span class="loading loading-dots loading-sm"></span>
            ) : (
                <ul class="flex flex-col divide-y divide-base-300">
                    {list.devices.map((d) => {
                        const { name, detail } = labelParts(d.label);
                        return (
                            <li key={d.key_id} class="flex flex-col gap-2 py-2 text-sm">
                                <div class="flex flex-wrap items-center gap-2">
                                    <span class="min-w-0 flex-1">
                                        <span class="font-medium">{name}</span>
                                        {d.current && <span class="badge badge-soft badge-success badge-sm ml-2">This device</span>}
                                        {!d.current && Date.now() - d.created_at < NEW_FOR && <span class="badge badge-soft badge-info badge-sm ml-2" data-new-device>New</span>}
                                        {detail && <span class="block text-base-content/70">{detail}</span>}
                                        <span class="block text-xs text-base-content/60">Added {day(d.created_at)} · last signed in {day(d.last_seen_at)}</span>
                                    </span>
                                    {confirm !== d.key_id && <Button kind="danger" size="xs" onClick={() => setConfirm(d.key_id)}>Sign out</Button>}
                                </div>
                                {confirm === d.key_id && (
                                    <div class="flex flex-col gap-2 rounded-box bg-base-100 p-3 max-md:bg-base-200">
                                        <p>
                                            {d.current ? 'Sign this device out?' : `Sign out ${name}?`} It loses access at once. Signing in again takes your
                                            den password and approval from another of your devices.
                                        </p>
                                        {list.devices.length === 1 && (
                                            <Notice kind="warning">
                                                It's your only device here, so signing in again takes a recovery code, and your DM recovery key to read
                                                your direct messages.
                                            </Notice>
                                        )}
                                        <Actions>
                                            <Button kind="danger" filled busy={act.busy} onClick={() => signOut(d)}>Sign out</Button>
                                            <Button kind="quiet" onClick={() => setConfirm('')}>Cancel</Button>
                                        </Actions>
                                    </div>
                                )}
                            </li>
                        );
                    })}
                </ul>
            )}
        </Card>
    );
}

// signedOut says how many of the member's other devices a new den password
// signed out.
function signedOut(n) {
    if (!n) return '';
    return n === 1 ? ' 1 other device was signed out.' : ` ${n} other devices were signed out.`;
}

// PasswordAndRecovery changes the member's den password, makes new
// recovery codes, and shows or resets their DM recovery key, each a card.
function PasswordAndRecovery({ den, onCodes, onKey }) {
    const [left, setLeft] = useState(null);
    const load = () => api.get(`/api/dens/${den.den_id}/devices`).then((d) => setLeft(d.recovery_codes_left), () => {});
    useEffect(() => {
        load();
    }, [den.den_id]);
    return (
        <>
            <Card title={<>Den password <DocsTip id={`docs-recovery-${den.den_id}`} page="usage/#your-passwords-and-keys">
                What your den password, recovery codes and DM recovery key are each for.</DocsTip></>}>
                <ChangePassword den={den} onDone={load} />
            </Card>
            <Card title="Recovery codes">
                {left !== null && <NewCodes den={den} left={left} onCodes={(codes) => {
                    load();
                    onCodes(codes);
                }} />}
            </Card>
            <Card title="DM recovery key">
                <DMKey den={den} onSeal={onKey} />
            </Card>
        </>
    );
}

// YourCode shows the member's code in this den, which others
// compare with what their Dens shows for them, from a DM, a profile or a
// call, to check nobody sits between them. A phone shows it a size
// smaller, which holds it on one line.
function YourCode({ den, view }) {
    const [code, setCode] = useState('');
    const [error, setError] = useState('');
    const me = view?.me?.id;
    useEffect(() => {
        if (me) api.get(`/api/dens/${den.den_id}/members/${me}/code`).then((v) => setCode(v.mine), (e) => setError(e.message));
    }, [den.den_id, me]);
    return (
        <Card title={<>Your code <DocsTip id={`docs-code-${den.den_id}`} page="usage/#comparing-codes">Why members compare codes, and where it's safe to.</DocsTip></>}>
            {view && !view.keeps_identities ? (
                <p class="text-sm">This den's Dens is older, and has no codes to compare yet.</p>
            ) : (
                <>
                    <p class="text-sm">Read it out when someone asks to compare codes.</p>
                    <p class="text-sm text-base-content/70">It changes only if you reset your DM recovery key.</p>
                    {code ? <p class="select-all font-mono text-xl tracking-wider sm:text-2xl" data-own-code>{code}</p> : <ErrorText message={error} />}
                </>
            )}
        </Card>
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
                <Button onClick={() => {
                    setSaved('');
                    setOpen(true);
                }}>
                    Change password
                </Button>
                {saved && <StatusText kind="success">{saved}</StatusText>}
            </div>
        );
    }
    return (
        <form class="flex flex-col gap-2" onSubmit={submit}>
            <p class="text-sm text-base-content/70">Signs out your other devices.</p>
            {withCode ? (
                <Field label="Recovery code" hint="One of the codes you saved. It works once.">
                    <TextInput value={form.code} onInput={(v) => set('code', v)} autocomplete="off" required class="input w-full font-mono" {...PLAIN} />
                </Field>
            ) : (
                <Field label="Current password">
                    <TextInput type="password" value={form.current} onInput={(v) => set('current', v)} autocomplete="current-password" required />
                </Field>
            )}
            <div>
                <Button kind="quiet" class="-ml-2" onClick={() => setWithCode(!withCode)}>
                    {withCode ? 'Use my current password' : 'Use a recovery code instead'}
                </Button>
            </div>
            <PasswordFields password={form.password} setPassword={(v) => set('password', v)} confirm={form.confirm} setConfirm={(v) => set('confirm', v)} />
            <ErrorText message={act.error} />
            <Actions>
                <SubmitButton busy={act.busy}>Change password</SubmitButton>
                <Button kind="quiet" onClick={close}>Cancel</Button>
            </Actions>
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

    const count = `${left === 1 ? '1 code left' : `${left} codes left`}, to sign in if you forget your password.`;
    return (
        <form class="flex flex-col gap-2" onSubmit={submit}>
            {left <= 3 ? <StatusText kind="warning">{count}</StatusText> : <p class="text-sm text-base-content/70">{count}</p>}
            {asking ? (
                <>
                    <p class="text-sm">New codes replace all the old ones, which stop working.</p>
                    <Field label="Den password">
                        <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required />
                    </Field>
                    <ErrorText message={act.error} />
                    <Actions>
                        <SubmitButton busy={act.busy}>Generate new codes</SubmitButton>
                        <Button kind="quiet" onClick={() => setAsking(false)}>Cancel</Button>
                    </Actions>
                </>
            ) : (
                <div><Button onClick={() => setAsking(true)}>Generate new codes</Button></div>
            )}
        </form>
    );
}

// Files lists this member's files in a den, largest first, with what uses
// each, and deletes or swaps them: a member at their limit frees
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
        const kind = shown.view.channels.find((c) => c.id === f.channel_id)?.kind;
        const dm = kind === 'dm' || kind === 'group_dm' || kind === 'notes';
        // The row follows the upload while a video's copy is made.
        const key = progressKey();
        const track = (sent) => setProgress((cur) => ({ ...cur, [f.id]: { ...cur[f.id], sent } }));
        const follow = (following) => setProgress((cur) => ({ ...cur, [f.id]: { ...cur[f.id], following } }));
        track(0);
        act.run(async () => {
            try {
                // A photo or video swapped in goes compressed like any
                // other, unless the member turned that off.
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
        <Card title="Your files">
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
                            <Button disabled={act.busy} onClick={more}>Show more</Button>
                        </div>
                    )}
                </>
            )}
            <input ref={pick} type="file" class="hidden" onChange={swapPicked} />
        </Card>
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
                        <Button kind="danger" filled size="xs" busy={busy} onClick={onRemove}>{action}</Button>
                        <Button kind="quiet" size="xs" onClick={onCancel}>Cancel</Button>
                    </span>
                </span>
            ) : (
                <span class="flex gap-1">
                    {onMessage && <Button kind="quiet" size="xs" onClick={onOpen}>Open</Button>}
                    {onMessage && !f.locked && <Button kind="quiet" size="xs" disabled={busy} onClick={onSwap}>Swap</Button>}
                    {!f.locked && <Button kind="danger" size="xs" onClick={onConfirm}>{action}</Button>}
                </span>
            )}
        </li>
    );
}

// Leave takes this member out of a den and forgets it on this computer.
// A den Dens can't reach, as one whose server is gone for good, can't hear
// of a leave, so it's removed from this computer alone, as one that let
// this device go is.
function Leave({ den, onLeft }) {
    const act = useAction();
    const offer = leaveOffer(den);
    if (offer !== 'leave') {
        return (
            <Card title={`Leave ${den.name}`}>
                <p class="text-sm">
                    {offer === 'unreachable'
                        ? "Dens can't reach this den right now, so it can't take you out of it. If it's gone for good, remove it from this device instead."
                        : den.error || 'This den no longer accepts this device.'}
                </p>
                <p class="text-sm">
                    That deletes your keys for the den on this device, and the messages and files it kept.
                    {offer === 'unreachable' && ' Your account stays in the den, so if it comes back, you can sign in again from Add a den.'}
                </p>
                <ErrorText message={act.error} />
                <Actions>
                    <Button kind="danger" filled busy={act.busy} onClick={() => act.run(async () => {
                        await api.del(`/api/dens/${den.den_id}`);
                        onLeft();
                    })}>
                        Remove from this device
                    </Button>
                </Actions>
            </Card>
        );
    }
    return (
        <Card title={`Leave ${den.name}`}>
            <p class="text-sm">
                You're signed out on every device, and your messages stay. You can come back as @{den.username} if someone invites you
                again.
            </p>
            <ErrorText message={act.error} />
            <Actions>
                <Button kind="danger" filled busy={act.busy} onClick={() => act.run(async () => {
                    await api.post(`/api/dens/${den.den_id}/leave`);
                    onLeft();
                })}>
                    Leave den
                </Button>
            </Actions>
        </Card>
    );
}
