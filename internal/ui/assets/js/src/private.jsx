// Private DMs and device approval (M1.7) on the page: comparing codes
// with a member (M16.1), approving a new device, as a dialog with a code
// to scan (M22.6), and the DM seal, which the page calls the DM recovery
// key (M8.3). The keys stay in the local
// service; the page only shows digits, and passes on what the member types.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import {
    Actions, Button, CopyButton, DocsLink, DocsTip, ErrorText, Field, Notice, PLAIN, StatusText, SubmitButton, TextInput, Tip, Waiting, clock, day, useAction,
} from './components.jsx';
import { useOver } from './animations.js';
import { MARKS_HOVER, MARKS_NEVER, useVerifiedMarks } from './verified.js';
import { openSettings } from './dialogs.js';
import { Dialog } from './manage.jsx';
import { canSaveFile } from './platform.js';
import { recoveryFileName, recoveryText, sameDigits } from './private.js';
import { copyText } from './clipboard.js';
import { QRCode, Scanner, useCamera } from './scan.jsx';
import { readSignInCode } from './signincode.js';

// Digits shows the digits a screen reads out, large and in groups, a size
// smaller on a phone, where a code's five groups would wrap.
export function Digits({ value }) {
    return <p class="select-all font-mono text-2xl tracking-wider max-[25rem]:text-xl" aria-label={value.replace(/ /g, ', ')}>{value}</p>;
}

// DigitsInput takes the digits the other side reads out.
function DigitsInput({ value, onInput, label, placeholder = '0000 0000 0000 0000' }) {
    return (
        <Field label={label}>
            <TextInput value={value} onInput={onInput} inputmode="numeric" autocomplete="off" placeholder={placeholder}
                class="input w-full font-mono text-lg tracking-wider" required />
        </Field>
    );
}

// VerifiedMark is the circled check beside a member this member verified
// by comparing codes (M16.1), which says so in its tip. Beside a name it
// shows as Appearance's Verified marks says: always, while the pointer is
// over its row, or never, and on a touch screen, shown or not (M16.4);
// always shows it regardless, as where comparing is the subject.
export function VerifiedMark({ id, name, always = false }) {
    const marks = useVerifiedMarks();
    const over = useOver();
    if (!always && marks === MARKS_NEVER) return null;
    const hidden = !always && marks === MARKS_HOVER && !over;
    return (
        <span class={hidden ? 'invisible' : ''} data-verified-mark>
            <Tip id={id} label={`You verified ${name}`} trigger={<CheckIcon />}>
                Verified: you compared codes with {name}.
            </Tip>
        </span>
    );
}

// CheckIcon is a check in a circle, as verified members have.
function CheckIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
            stroke-linejoin="round" aria-hidden="true">
            <circle cx="8" cy="8" r="6.5" />
            <path d="M5.2 8.3l1.9 1.9 3.8-4.1" />
        </svg>
    );
}

// CodesDialog compares codes with a member (M16.1). It doesn't name them,
// whose name could read as part of its words.
export function CodesDialog({ denID, member, onClose }) {
    return (
        <Dialog title="Compare codes" onClose={onClose}>
            <Codes denID={denID} member={member} />
        </Dialog>
    );
}

// Codes is comparing codes with a member: this member's own, to read out,
// and a box for the other's, typed as they read it out, so a check can't
// pass by copying one's own screen. Comparing asks nothing of the den: the
// local service makes the code from the identity it kept.
export function Codes({ denID, member }) {
    const [view, setView] = useState(null);
    const [error, setError] = useState('');
    const [digits, setDigits] = useState('');
    const [matched, setMatched] = useState(false);
    const act = useAction();
    useEffect(() => {
        api.get(`/api/dens/${denID}/members/${member.id}/code`).then(setView, (e) => setError(e.message));
    }, [denID, member.id]);

    function compare(e) {
        e.preventDefault();
        if (view && sameDigits(digits, view.mine)) {
            return act.setError('Those are your own digits. Type theirs.');
        }
        act.run(async () => {
            const next = await api.post(`/api/dens/${denID}/members/${member.id}/compare`, { digits });
            setView((v) => ({ ...v, ...next }));
            setDigits('');
            setMatched(true);
        });
    }

    return (
        <>
            {!view ? (
                error ? <ErrorText message={error} /> : <span class="loading loading-dots loading-sm"></span>
            ) : (
                <form class="flex flex-col gap-3" onSubmit={compare} data-codes>
                    {view.verified && (
                        <StatusText kind="success" role="status" data-verified>{matched ? 'The codes match.' : 'Verified.'}</StatusText>
                    )}
                    {view.held && <StatusText kind="warning">Their keys changed since you compared codes.</StatusText>}
                    <p class="text-sm">
                        Read out your code, and type theirs as they read it out.{' '}
                        <DocsLink icon page="usage/#comparing-codes" label="Where to compare codes, and why" />
                    </p>
                    <div>
                        <p class="text-xs text-base-content/60">Your code</p>
                        <Digits value={view.mine} />
                    </div>
                    <DigitsInput value={digits} onInput={setDigits} label="Their code" placeholder="0000 0000 0000 0000 0000" />
                    <ErrorText message={act.error} />
                    <Actions><SubmitButton busy={act.busy}>Compare</SubmitButton></Actions>
                </form>
            )}
        </>
    );
}

// KeysChanged is the line in a DM where the other member's keys changed
// (M16.1), with Compare.
export function KeysChanged({ name, at, onCompare }) {
    return (
        <div class="divider divider-warning my-1 h-auto cursor-default select-none text-xs" data-keys-changed>
            <span class="flex max-w-[80%] shrink-0 flex-wrap items-center justify-center gap-x-2 whitespace-normal text-center">
                <span>{name}'s keys changed{at ? ` on ${day(at)}` : ''}.</span>
                <Button kind="quiet" size="xs" onClick={onCompare}>Compare</Button>
            </span>
        </div>
    );
}

// DMGate stands in a DM's composer's place while it can't take messages
// (M16.1): this device lacks the member's DM recovery key; the den, or the
// other member's Dens, runs a version too old to seal the DM's new keys;
// or the other's keys changed since this member verified them, which
// holds the DM until they compare again or send anyway.
export function DMGate({ denID, gate, dm, notes = false, onCompare }) {
    const act = useAction();
    const name = dm ? dm.display_name : 'the other member';
    let body;
    if (gate === 'no_seal') {
        // The member's notes (M17.3) open with a key their recovery key makes.
        body = (
            <div class="flex flex-wrap items-center gap-2">
                <p class="min-w-0 flex-1">
                    {notes
                        ? "This device doesn't have your DM recovery key for this den, so it can't read or write your notes here."
                        : "This device doesn't have your DM recovery key for this den, so it can't read or send direct messages here."}
                </p>
                <Button onClick={() => openSettings('password', denID)}>Enter it</Button>
            </div>
        );
    } else if (gate === 'den') {
        body = <p class="text-base-content/70">This den runs an older Dens, which can't set up this DM's encryption. Its owner can update it.</p>;
    } else if (gate === 'update') {
        body = <p class="text-base-content/70">{name}'s Dens needs an update before you can write here.</p>;
    } else if (gate === 'held') {
        body = (
            <div class="flex flex-col gap-2" data-hold>
                <p>{name}'s keys changed since you compared codes.</p>
                <ErrorText message={act.error} />
                <Actions>
                    <Button kind="primary" onClick={onCompare}>Compare</Button>
                    <Button busy={act.busy} onClick={() => act.run(() => api.post(`/api/dens/${denID}/members/${dm.id}/send-anyway`))}>
                        Send anyway
                    </Button>
                </Actions>
            </div>
        );
    }
    return <div class="border-t border-base-300 px-4 py-3 text-sm">{body}</div>;
}

// Requests the member closed before approving them, by ID, which wait in
// a line above Home and their den's channel to open again (M22.6).
const hidden = new Set();
const hiddenWatchers = new Set();

function setHidden(id, on) {
    if (on) hidden.add(id);
    else hidden.delete(id);
    for (const f of hiddenWatchers) f();
}

function useHidden() {
    const [, bump] = useState(0);
    useEffect(() => {
        const f = () => bump((n) => n + 1);
        hiddenWatchers.add(f);
        return () => hiddenWatchers.delete(f);
    }, []);
    return hidden;
}

// useDensView is the dens' status on this computer, the newest of the
// list fetched and the stream's.
function useDensView() {
    const [view, setView] = useState(null);
    useEffect(() => {
        let live = true;
        const show = (v) => setView((cur) => (!cur || v.epoch !== cur.epoch || v.version >= cur.version ? v : cur));
        api.get('/api/dens').then((v) => live && show(v), () => {});
        const stop = onEvent((m) => m.t === 'dens' && show(m.d));
        return () => {
            live = false;
            stop();
        };
    }, []);
    return view;
}

/** Approvals opens each sign-in on a new device that waits for this
    member's approval, in any den, as a dialog over whatever is on screen,
    a sheet on a phone (M22.6), one at a time, oldest first. One closed
    before it's approved waits in a line above Home and its den's channel,
    HiddenRequests, to open again; one approved closes as its new device
    connects, or as the member closes it, which ends its digits. */
export function Approvals() {
    const view = useDensView();
    const shut = useHidden();
    const [refused, setRefused] = useState(null);
    if (refused) return <RefusedDialog den={refused} onClose={() => setRefused(null)} />;
    const waiting = [];
    for (const den of view?.dens || []) {
        if (den.state !== 'connected') continue;
        for (const r of den.requests || []) if (!shut.has(r.id)) waiting.push({ den, r });
    }
    waiting.sort((a, b) => a.r.requested_at - b.r.requested_at);
    const first = waiting[0];
    if (!first) return null;
    return <ApprovalDialog key={first.r.id} den={first.den} r={first.r} onRefused={() => setRefused(first.den)} />;
}

/** HiddenRequests is a line above Home and a den's channel for each
    sign-in waiting on this member there whose dialog they closed, which
    opens it again (M22.6). */
export function HiddenRequests({ den }) {
    const shut = useHidden();
    return (den.requests || []).filter((r) => shut.has(r.id)).map((r) => (
        <Notice key={r.id} kind="info" data-request-hidden>
            <span>A new device asks to sign in to {den.name} as you: <span class="font-medium">{r.label}</span>.</span>
            <Actions><Button onClick={() => setHidden(r.id, false)}>Show</Button></Actions>
        </Notice>
    ));
}

// ApprovalDialog is one new device's request to sign in as this member:
// who asks, It's mine or Refuse, then the digits each device scans or
// types from the other, this one's under their code. This device answers,
// the member types the new device's digits here, and only then does this
// device hand over the member's DM recovery key.
function ApprovalDialog({ den, r, onRefused }) {
    const [digits, setDigits] = useState('');
    const act = useAction();
    const base = `/api/dens/${den.den_id}/requests`;
    const close = () => (r.approved ? act.run(() => api.del(`${base}/${r.id}`)) : setHidden(r.id, true));
    const refuse = (
        <Button kind="danger" disabled={act.busy} onClick={() => act.run(async () => {
            await api.post(`${base}/${r.id}/refuse`);
            onRefused();
        })}>
            Refuse
        </Button>
    );
    let step;
    if (r.approved) {
        step = (
            <>
                <StatusText kind="success">Approved.</StatusText>
                <p>If the new device still asks for this one's digits, scan this code or type them there.</p>
                <DigitsShown den={den} r={r} />
                <ErrorText message={act.error} />
                <Actions><Button kind="primary" busy={act.busy} onClick={close}>Done</Button></Actions>
            </>
        );
    } else if (r.elsewhere) {
        step = (
            <>
                <p>Another of your devices is approving it.</p>
                <ErrorText message={act.error} />
                <Actions>{refuse}</Actions>
            </>
        );
    } else if (r.lost) {
        step = (
            <>
                <p>Dens on this device restarted while you were approving it, which lost its part of the check. Refuse it, and sign in again on the new device.</p>
                <ErrorText message={act.error} />
                <Actions>{refuse}</Actions>
            </>
        );
    } else if (!r.here) {
        step = (
            <>
                <p>If it isn't yours, refuse it and change your den password.</p>
                <ErrorText message={act.error} />
                <Actions>
                    <Button kind="primary" busy={act.busy} onClick={() => act.run(() => api.post(`${base}/${r.id}/answer`))}>It's mine</Button>
                    {refuse}
                </Actions>
            </>
        );
    } else if (!r.half) {
        step = <Waiting label="Waiting for the new device…" />;
    } else {
        step = (
            <form class="flex flex-col gap-3" onSubmit={(e) => {
                e.preventDefault();
                act.run(async () => {
                    await api.post(`${base}/${r.id}/approve`, { digits });
                    setDigits('');
                });
            }}>
                <p>On the new device, scan this code or type these digits.</p>
                <DigitsShown den={den} r={r} />
                <DigitsInput value={digits} onInput={setDigits} label="Then type the digits the new device shows" />
                <ErrorText message={act.error} />
                <Actions>
                    <SubmitButton busy={act.busy}>Approve</SubmitButton>
                    {refuse}
                </Actions>
            </form>
        );
    }
    return (
        <Dialog title="Sign-in request" onClose={close}>
            <div class="flex flex-col gap-3" data-approval>
                <p>
                    A new device asks to sign in to {den.name} as you: <span class="font-medium" data-request-label>{r.label}</span>.{' '}
                    <DocsTip id={`docs-approve-${r.id}`} page="usage/#signing-in-from-another-computer">
                        Why your devices approve each other, and what to do about one that isn't yours.</DocsTip>
                </p>
                {step}
                <p class="text-xs text-base-content/60">It waits until {clock(r.expires_at)}.</p>
            </div>
        </Dialog>
    );
}

// DigitsShown is the digits this device shows for a sign-in it answered,
// under their code, for the new device to scan (M22.6).
function DigitsShown({ den, r }) {
    const [code, setCode] = useState(null);
    useEffect(() => {
        let live = true;
        api.get(`/api/dens/${den.den_id}/requests/${r.id}/code`).then((c) => live && setCode(c), () => {});
        return () => {
            live = false;
        };
    }, [den.den_id, r.id, r.half]);
    return (
        <div class="flex flex-col items-center gap-2" data-digits-shown>
            {code && <QRCode code={code} label="These digits as a code, for the new device to scan" />}
            <Digits value={r.half} />
        </div>
    );
}

// RefusedDialog follows a refused sign-in: whoever asked has the member's
// den password.
function RefusedDialog({ den, onClose }) {
    return (
        <Dialog title="Sign-in refused" onClose={onClose}>
            <p>Whoever asked has your den password, so change it: that signs out every other device.</p>
            <Actions>
                <Button kind="primary" onClick={() => {
                    onClose();
                    openSettings('password', den.den_id);
                }}>Change password</Button>
                <Button kind="quiet" onClick={onClose}>Not now</Button>
            </Actions>
        </Dialog>
    );
}

const SIGN_IN_ENDED = {
    refused: 'Your other device refused this sign-in.',
    cancelled: 'This sign-in was cancelled: your den password or DM recovery key changed on another device meanwhile.',
    expired: 'None of your other devices approved this sign-in in time. Try again when one of them is at hand.',
};

// SignInSteps shows a sign-in on this device at the step it's at (M8.3):
// waiting for another device to approve it, comparing digits with it,
// each scanning or typing the other's (M22.6), then signed in, or why it
// ended. The service holds the sign-in, so a reload comes back to its
// step. title heads it where nothing else does: in Add a den, the
// dialog's title is the den's. onRecover gives up waiting for another
// device, to sign in with a recovery code instead.
export function SignInSteps({ p, title, onRecover, onDone }) {
    const [digits, setDigits] = useState('');
    const [scanning, setScanning] = useState(false);
    const [scanNote, setScanNote] = useState('');
    const camera = useCamera();
    const act = useAction();
    const dismiss = () => act.run(async () => {
        await api.del(`/api/dens/signin/${p.id}`);
        onDone?.();
    });
    const going = p.stage === 'waiting' || p.stage === 'check' || p.stage === 'approved';
    // Check and Cancel share a row, as a dialog's actions do.
    const checking = (p.stage === 'check' || p.stage === 'approved') && !p.checked;
    const check = (typed) => act.run(async () => {
        await api.post(`/api/dens/signin/${p.id}/check`, { digits: typed });
        setDigits('');
    });
    // scanned checks the digits a code holds, or says what it is and goes
    // on scanning.
    const scanned = (text) => {
        const code = readSignInCode(text);
        if (code.kind !== 'digits') {
            setScanNote(code.kind === 'device' ? "That's Add device's code. Scan the one beside your other device's digits." : "That code isn't one from Dens.");
            return false;
        }
        setScanNote('');
        setScanning(false);
        check(code.digits);
        return true;
    };
    const cancel = (
        <Button kind="quiet" disabled={act.busy} onClick={dismiss}>Cancel</Button>
    );
    const docs = going && (
        <DocsTip id={`docs-signin-${p.id}`} page="usage/#signing-in-from-another-computer">Why a new device needs one of your others to let it in.</DocsTip>
    );
    return (
        <div class="flex flex-col gap-3" data-signin-steps>
            {title && <h3 class="text-lg font-semibold">{title}</h3>}
            {p.stage === 'waiting' && (
                <>
                    <p role="status" class="flex items-center gap-2 font-medium">
                        <span class="loading loading-spinner loading-sm"></span>
                        Waiting for your other device
                        {docs}
                    </p>
                    <p>
                        Open Dens on one of your other devices in this den, as @{p.username}: it asks whether this one is yours. It waits until{' '}
                        {clock(p.expires_at)}.
                    </p>
                    {onRecover && (
                        <div>
                            <button type="button" class="link link-primary text-sm" disabled={act.busy} onClick={() => act.run(async () => {
                                await api.del(`/api/dens/signin/${p.id}`);
                                onRecover(p);
                            })}>
                                No other device? Use a recovery code
                            </button>
                        </div>
                    )}
                </>
            )}
            {(p.stage === 'check' || p.stage === 'approved') && (
                <form class="flex flex-col gap-3" onSubmit={(e) => {
                    e.preventDefault();
                    check(digits);
                }}>
                    <p>
                        {p.checked ? "You checked your other device's digits. Now type this device's digits into it:" : "Type these digits into your other device. They stay here until you're signed in."}
                        {' '}{docs}
                    </p>
                    <Digits value={p.half} />
                    {checking && (
                        scanning ? (
                            <>
                                <Scanner onCode={scanned} />
                                <p class="text-center text-sm">Point the camera at the code above your other device's digits.</p>
                                {scanNote && <StatusText kind="warning" role="status">{scanNote}</StatusText>}
                            </>
                        ) : (
                            <>
                                {camera && (
                                    <div>
                                        <Button size="md" onClick={() => {
                                            act.setError('');
                                            setScanning(true);
                                        }} data-scan>Scan its code</Button>
                                    </div>
                                )}
                                <DigitsInput value={digits} onInput={setDigits} label={camera ? 'Or type the digits your other device shows' : 'Then type the digits your other device shows'} />
                            </>
                        )
                    )}
                    {p.stage === 'approved' && !p.checked && (
                        <p class="text-sm text-base-content/70">Your other device approved; its digits here finish signing in.</p>
                    )}
                    <p class="text-xs text-base-content/60">The digits only match between your two devices if nobody is in between.</p>
                    {checking && (
                        <>
                            <ErrorText message={act.error} />
                            <Actions>
                                {scanning
                                    ? <Button onClick={() => setScanning(false)}>Type them instead</Button>
                                    : <SubmitButton busy={act.busy}>Check</SubmitButton>}
                                {cancel}
                            </Actions>
                        </>
                    )}
                </form>
            )}
            {p.stage === 'done' && <StatusText kind="success">You're signed in, and your DMs are readable here too.</StatusText>}
            {SIGN_IN_ENDED[p.stage] && <p>{SIGN_IN_ENDED[p.stage]}</p>}
            {p.stage === 'failed' && <ErrorText message={p.error || 'Signing in failed.'} />}
            {!checking && (
                <>
                    <ErrorText message={act.error} />
                    <Actions>{going ? cancel : <Button kind="primary" disabled={act.busy} onClick={dismiss}>OK</Button>}</Actions>
                </>
            )}
        </div>
    );
}

// PendingSignIns shows the sign-ins on this device that the Add a den
// dialog isn't showing, as after a reload, each at its step, under its
// den's address.
export function PendingSignIns({ list, onRecover }) {
    if (!list?.length) return null;
    return list.map((p) => (
        <section key={p.id} class="card bg-base-200">
            <div class="card-body">
                <SignInSteps p={p} title={p.url.replace(/^https?:\/\//, '')} onRecover={onRecover} />
            </div>
        </section>
    ));
}

const KEY_WHY = 'Reads your DMs if you lose every device. Keep it secret.';

// KeyShown shows a DM recovery key once, to keep, beside a den's recovery
// codes or after a reset, which save it with them.
function KeyShown({ seal, fresh }) {
    return (
        <div class="flex flex-col gap-2">
            <h4 class="font-semibold">DM recovery key</h4>
            <p class="text-sm">{KEY_WHY}{!fresh && " It's the one your other dens on this device use."}</p>
            <p class="select-all break-all rounded bg-base-100 px-3 py-2 font-mono text-sm" data-seal>{seal}</p>
        </div>
    );
}

// saveText has the browser save text as a file named name.
function saveText(text, name) {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = name;
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// RecoveryCodes shows a den's recovery codes once, and the DM recovery key
// beside them when there is one to keep; after a reset, the key alone.
// They save as one file, named for the den, or copy as its text, and
// either lets Done through. den is its status: its name, address and the
// member's username there, which the file names too. Android's app has
// no file to save, so it copies.
export function RecoveryCodes({ den, codes, seal, sealNew, onDone }) {
    const [kept, setKept] = useState('');
    const [problem, setProblem] = useState('');
    const text = recoveryText({ den, codes, seal });
    function said(what) {
        setKept(what);
        setProblem('');
    }
    async function copy() {
        try {
            await copyText(text);
            said('copied');
        } catch (e) {
            setProblem(e.message);
        }
    }
    return (
        <div class="flex flex-col gap-3">
            <div>
                <h3 class="text-lg font-semibold">
                    {codes.length ? 'Save your recovery codes' : 'Save your DM recovery key'}{' '}
                    <DocsTip id="docs-keys" page="usage/#your-passwords-and-keys">What each of these is for, and how to keep them.</DocsTip>
                </h3>
                <p class="text-sm text-base-content/70">{den.name} · shown only now</p>
            </div>
            {codes.length > 0 && (
                <>
                    <p class="text-sm">Each code signs you in once if you forget your password here.</p>
                    <ol class="grid gap-2 font-mono text-sm sm:grid-cols-2">
                        {codes.map((code) => <li key={code} class="whitespace-nowrap rounded bg-base-100 px-3 py-1">{code}</li>)}
                    </ol>
                </>
            )}
            {seal && <KeyShown seal={seal} fresh={sealNew} />}
            <div class="flex flex-wrap items-center gap-2" data-recovery-save>
                {canSaveFile() && (
                    <Button onClick={() => {
                        saveText(text, recoveryFileName(den.name));
                        said('saved');
                    }}>{kept === 'saved' ? 'Saved' : 'Save as a file'}</Button>
                )}
                <Button onClick={copy}>{kept === 'copied' ? 'Copied' : 'Copy'}</Button>
            </div>
            <ErrorText message={problem} />
            <Actions>
                <Button kind="primary" disabled={!kept} title={kept ? undefined : 'Save or copy them first'} onClick={onDone}>Done</Button>
            </Actions>
        </div>
    );
}

// KeyNeeded offers a device with no DM recovery key for a den, as after a
// recovery code, to enter the key or reset it.
export function KeyNeeded({ den, onSeal }) {
    const [mode, setMode] = useState('');
    const [seal, setSeal] = useState('');
    const act = useAction();
    return (
        <Notice kind="warning">
            <p>This device doesn't have your DM recovery key for {den.name}, so it can't read or send your direct messages there.</p>
            {mode === '' && (
                <Actions>
                    <Button kind="primary" onClick={() => setMode('type')}>Enter my key</Button>
                    <Button onClick={() => setMode('over')}>I've lost it</Button>
                </Actions>
            )}
            {mode === 'type' && (
                <form class="flex flex-col gap-2 self-stretch" onSubmit={(e) => {
                    e.preventDefault();
                    act.run(async () => {
                        await api.post(`/api/dens/${den.den_id}/seal`, { seal });
                        setSeal('');
                        setMode('');
                    });
                }}>
                    <Field label="DM recovery key" hint="The one you saved when you joined, in groups of four. Case doesn't matter.">
                        <textarea class="textarea w-full font-mono" rows="2" value={seal} onInput={(e) => setSeal(e.currentTarget.value)} required {...PLAIN}></textarea>
                    </Field>
                    <ErrorText message={act.error} />
                    <Actions>
                        <SubmitButton busy={act.busy}>Use this key</SubmitButton>
                        <Button kind="quiet" onClick={() => setMode('')}>Back</Button>
                    </Actions>
                </form>
            )}
            {mode === 'over' && <ResetKey den={den} onDone={onSeal} onBack={() => setMode('')} />}
        </Notice>
    );
}

// ResetKey gives the member a new DM recovery key at a den, with their den
// password, once they've read what that does, and shows it once.
export function ResetKey({ den, onDone, onBack }) {
    const [password, setPassword] = useState('');
    const act = useAction();
    return (
        <form class="flex flex-col gap-2 self-stretch" onSubmit={(e) => {
            e.preventDefault();
            act.run(async () => {
                const result = await api.post(`/api/dens/${den.den_id}/seal/start-over`, { password });
                setPassword('');
                onDone(result.seal);
            });
        }}>
            <p class="text-sm">
                Resetting makes a new DM recovery key, for a lost key or a stolen device that holds it. Your DMs in {den.name} before now
                become unreadable to you, though not to the people you talked with, and your code changes, so they see your keys changed and
                anyone who compared codes with you does again. Your other devices there are signed out, since they hold the old key.
            </p>
            <Field label="Den password">
                <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required />
            </Field>
            <ErrorText message={act.error} />
            <Actions>
                <SubmitButton busy={act.busy} kind="danger" filled>Reset key</SubmitButton>
                {onBack && <Button kind="quiet" onClick={onBack}>Cancel</Button>}
            </Actions>
        </form>
    );
}

// DMKey shows a den's DM recovery key after the local password, and resets
// it, in the den's Manage dialog; on a device without the key, it offers
// to enter it.
export function DMKey({ den, onSeal }) {
    const [shown, setShown] = useState('');
    const [asking, setAsking] = useState('');
    const [password, setPassword] = useState('');
    const act = useAction();
    if (!den.seal) return <KeyNeeded den={den} onSeal={onSeal} />;
    return (
        <div class="flex flex-col gap-2">
            <p class="text-sm text-base-content/70">{KEY_WHY}</p>
            {shown ? (
                <>
                    <p class="select-all rounded bg-base-100 px-3 py-2 font-mono text-sm">{shown}</p>
                    <div class="flex flex-wrap gap-2">
                        <CopyButton text={shown} label="Copy key" />
                        <Button kind="quiet" onClick={() => setShown('')}>Hide</Button>
                    </div>
                </>
            ) : asking === 'show' ? (
                <form class="flex flex-col gap-2" onSubmit={(e) => {
                    e.preventDefault();
                    act.run(async () => {
                        const result = await api.post(`/api/dens/${den.den_id}/seal/show`, { password });
                        setPassword('');
                        setAsking('');
                        setShown(result.seal);
                    });
                }}>
                    <Field label="Local password" hint="Your Dens password on this device, not a den password.">
                        <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required />
                    </Field>
                    <ErrorText message={act.error} />
                    <Actions>
                        <SubmitButton busy={act.busy}>Show key</SubmitButton>
                        <Button kind="quiet" onClick={() => setAsking('')}>Cancel</Button>
                    </Actions>
                </form>
            ) : asking === 'reset' ? (
                <ResetKey den={den} onDone={(seal) => {
                    setAsking('');
                    onSeal(seal);
                }} onBack={() => setAsking('')} />
            ) : (
                <div class="flex flex-wrap gap-2">
                    <Button onClick={() => setAsking('show')}>Show key</Button>
                    <Button kind="danger" onClick={() => setAsking('reset')}>Reset key</Button>
                </div>
            )}
        </div>
    );
}

// keyDivider draws where a DM's keys changed.
// DaisyUI's divider keeps its words on one line, which a key's change has
// too many of for a phone, so they wrap, within most of the width, and the
// lines beside them take the rest (M13.4).
export function KeyDivider({ text }) {
    return (
        <div class="divider divider-warning my-1 h-auto cursor-default select-none text-xs">
            <span class="max-w-[80%] shrink-0 whitespace-normal text-center">{text}</span>
        </div>
    );
}
