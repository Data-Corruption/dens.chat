// Private DMs and device approval (M1.7) on the page: comparing codes
// with a member (M16.1), approving a new device, and the DM seal, which the
// page calls the DM recovery key (M8.3). The keys stay in the local
// service; the page only shows digits, and passes on what the member types.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { CopyButton, DocsTip, ErrorText, Field, PLAIN, SubmitButton, TextInput, Tip, clock, day, useAction } from './components.jsx';
import { openManage } from './dialogs.js';
import { Dialog } from './manage.jsx';
import { sameDigits } from './private.js';

// Digits shows the digits a screen reads out, large and in groups.
function Digits({ value }) {
    return <p class="select-all font-mono text-2xl tracking-wider" aria-label={value.replace(/ /g, ', ')}>{value}</p>;
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

// WhereToCompare says where codes are worth comparing, best first, and
// why.
function WhereToCompare({ name }) {
    return (
        <details class="text-sm">
            <summary class="min-h-6 cursor-pointer select-none py-0.5 text-base-content/70">Where should we compare?</summary>
            <div class="mt-1 flex flex-col gap-1 text-base-content/70">
                <p>Best first:</p>
                <ol class="list-decimal pl-6">
                    <li>In person.</li>
                    <li>On a voice or video call, where you know it's really {name}.</li>
                    <li>In another den that one of you owns. Not this one, even if it's yours.</li>
                    <li>On several other services where you know the accounts are really {name}'s.</li>
                    <li>In a den neither of you owns, whose owner could change what you see there.</li>
                </ol>
                <p>
                    Keys pass through this den, so if it were tampered with, it could sit between you and hand each of you keys of
                    its own. The digits only match if nobody did. Whoever tampered with it could also change the digits you send
                    each other here, so this den can't vouch for itself, whoever owns it: compare somewhere it can't reach.
                </p>
                <p>
                    It could also pretend to be {name} somewhere else ("I'm @them over there, let's compare there"), so compare where
                    you know who you're talking to. Hosting your own den narrows that risk, but nothing removes it.
                </p>
            </div>
        </details>
    );
}

// VerifiedMark is the green circled check beside a member this member
// verified by comparing codes (M16.1), which says so in its tip.
export function VerifiedMark({ id, name }) {
    return (
        <Tip id={id} label={`You verified ${name}`} trigger={
            <svg viewBox="0 0 16 16" class="h-3.5 w-3.5 text-success" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
                stroke-linejoin="round" aria-hidden="true">
                <circle cx="8" cy="8" r="6.5" />
                <path d="M5.2 8.3l1.9 1.9 3.8-4.1" />
            </svg>
        }>
            You compared codes with {name}, so nobody sits between you. It goes if their keys change.
        </Tip>
    );
}

// CodesDialog compares codes with a member (M16.1).
export function CodesDialog({ denID, member, onClose }) {
    return (
        <Dialog title={`Compare codes with ${member.display_name}`} onClose={onClose}>
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
    const name = member.display_name;
    useEffect(() => {
        api.get(`/api/dens/${denID}/members/${member.id}/code`).then(setView, (e) => setError(e.message));
    }, [denID, member.id]);

    function compare(e) {
        e.preventDefault();
        if (view && sameDigits(digits, view.mine)) {
            return act.setError(`Those are your own digits. Type the ones ${name} reads out to you.`);
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
                        <p role="status" class="flex items-center gap-1 text-success" data-verified>
                            <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
                                stroke-linejoin="round" aria-hidden="true">
                                <circle cx="8" cy="8" r="6.5" />
                                <path d="M5.2 8.3l1.9 1.9 3.8-4.1" />
                            </svg>
                            {matched ? `The codes match: you verified ${name}.` : `You verified ${name}.`}
                        </p>
                    )}
                    {view.held && (
                        <p class="text-sm text-warning">{name}'s keys changed since you compared codes. Compare again to check it's really them.</p>
                    )}
                    <p class="text-sm">
                        Read your code out to {name}, and type theirs as they read it out. If both match, nobody sits between you.{' '}
                        <DocsTip id={`docs-codes-${member.id}`} page="join/#direct-messages">Why you compare codes, and where it's safe to.</DocsTip>
                    </p>
                    <div>
                        <p class="text-xs text-base-content/60">Your code</p>
                        <Digits value={view.mine} />
                    </div>
                    <DigitsInput value={digits} onInput={setDigits} label={`Type the code ${name} reads out to you`} placeholder="0000 0000 0000 0000 0000" />
                    <ErrorText message={act.error} />
                    <div><SubmitButton busy={act.busy}>Compare</SubmitButton></div>
                    <p class="text-xs text-base-content/60">
                        Digits that really don't match mean someone may be in the middle. Compare somewhere else before you trust this DM.
                    </p>
                    <WhereToCompare name={name} />
                </form>
            )}
        </>
    );
}

// KeysChanged is the line in a DM where the other member's keys changed
// (M16.1), with Compare.
export function KeysChanged({ name, at, onCompare }) {
    return (
        <div class="divider divider-warning my-1 h-auto cursor-default select-none text-xs text-warning" data-keys-changed>
            <span class="flex max-w-[80%] shrink-0 flex-wrap items-center justify-center gap-x-2 whitespace-normal text-center">
                <span>{name}'s keys changed{at ? ` on ${day(at)}` : ''}.</span>
                <button type="button" class="btn btn-ghost btn-xs" onClick={onCompare}>Compare</button>
            </span>
        </div>
    );
}

// DMGate stands in a DM's composer's place while it can't take messages
// (M16.1): this device lacks the member's DM recovery key; the den, or the
// other member's Dens, runs a version too old to seal the DM's new keys;
// or the other's keys changed since this member verified them, which
// holds the DM until they compare again or send anyway.
export function DMGate({ denID, gate, dm, onCompare }) {
    const act = useAction();
    const name = dm ? dm.display_name : 'the other member';
    let body;
    if (gate === 'no_seal') {
        body = (
            <div class="flex flex-wrap items-center gap-2">
                <p class="min-w-0 flex-1">
                    This device doesn't have your DM recovery key for this den, so it can't read or send direct messages here.
                </p>
                <button type="button" class="btn btn-sm" onClick={() => openManage(denID, 'password')}>Enter it</button>
            </div>
        );
    } else if (gate === 'den') {
        body = <p class="text-base-content/70">This den runs an older Dens, which can't set up this DM's encryption. Its owner can update it.</p>;
    } else if (gate === 'update') {
        body = <p class="text-base-content/70">{name}'s Dens needs an update before you can write here.</p>;
    } else if (gate === 'held') {
        body = (
            <div class="flex flex-col gap-2" data-hold>
                <p>
                    {name}'s keys changed since you compared codes. Compare again to check it's really them, or send anyway if you know why they
                    changed.
                </p>
                <ErrorText message={act.error} />
                <div class="flex flex-wrap gap-2">
                    <button type="button" class="btn btn-primary btn-sm" onClick={onCompare}>Compare</button>
                    <button type="button" class="btn btn-sm" disabled={act.busy} onClick={() => act.run(() => api.post(`/api/dens/${denID}/members/${dm.id}/send-anyway`))}>
                        Send anyway
                    </button>
                </div>
            </div>
        );
    }
    return <div class="border-t border-base-300 px-4 py-3 text-sm">{body}</div>;
}

// SignInRequests shows sign-ins on new devices that wait for this member's
// approval on a den, and approves them from here: this device answers, the
// member types the new device's digits here and this device's there, and
// only then does this device hand over the member's DM recovery key.
export function SignInRequests({ den }) {
    const [digits, setDigits] = useState({});
    const [refused, setRefused] = useState(false);
    const act = useAction();
    const requests = den.requests || [];
    if (requests.length === 0) {
        return refused ? (
            <div role="status" class="alert alert-warning alert-soft flex flex-wrap">
                <span>You refused a sign-in. Whoever asked has your den password, so change it: that signs out every other device.</span>
                <span class="flex gap-2">
                    <button type="button" class="btn btn-sm" onClick={() => openManage(den.den_id, 'password')}>Change password</button>
                    <button type="button" class="btn btn-ghost btn-sm" onClick={() => setRefused(false)}>Dismiss</button>
                </span>
            </div>
        ) : null;
    }
    const base = `/api/dens/${den.den_id}/requests`;
    return (
        <div class="flex flex-col gap-2">
            {requests.map((r) => r.approved ? (
                // Approved here first, the new device may still be waiting
                // for its member to type this device's digits.
                <div key={r.id} role="status" class="alert alert-success alert-soft flex flex-col items-stretch gap-2 text-left">
                    <p>
                        You approved <span class="font-medium">{r.label}</span>. If it still asks for this device's digits, type these into
                        it:
                    </p>
                    <Digits value={r.half} />
                    <div>
                        <button type="button" class="btn btn-sm" disabled={act.busy} onClick={() => act.run(() => api.del(`${base}/${r.id}`))}>
                            Done
                        </button>
                    </div>
                </div>
            ) : (
                <div key={r.id} role="status" class="alert alert-info alert-soft flex flex-col items-stretch gap-2 text-left">
                    <p>
                        A new device asks to sign in to {den.name} as you: <span class="font-medium">{r.label}</span>. If it isn't yours,
                        refuse it and change your den password.{' '}
                        <DocsTip id={`docs-approve-${r.id}`} page="join/#signing-in-from-another-computer">
                            Why your devices approve each other, and what to do about one that isn't yours.</DocsTip>
                    </p>
                    {r.elsewhere ? (
                        <p class="text-sm">Another of your devices is approving it.</p>
                    ) : r.lost ? (
                        <p class="text-sm">
                            Dens on this device restarted while you were approving it, which lost its part of the check. Refuse it, and
                            sign in again on the new device.
                        </p>
                    ) : !r.here ? (
                        <p class="text-sm">Is it yours? Then check it's the device in front of you: both screens show digits to compare.</p>
                    ) : !r.half ? (
                        <p class="text-sm">Waiting for the new device…</p>
                    ) : (
                        <form class="flex flex-col gap-2" onSubmit={(e) => {
                            e.preventDefault();
                            act.run(async () => {
                                await api.post(`${base}/${r.id}/approve`, { digits: digits[r.id] || '' });
                                setDigits({ ...digits, [r.id]: '' });
                            });
                        }}>
                            <p class="text-sm">Type these digits into the new device. They stay here after you approve, until you click Done.</p>
                            <Digits value={r.half} />
                            <DigitsInput value={digits[r.id] || ''} onInput={(v) => setDigits({ ...digits, [r.id]: v })}
                                label="Then type the digits the new device shows" />
                            <div><SubmitButton busy={act.busy}>Approve</SubmitButton></div>
                        </form>
                    )}
                    <ErrorText message={act.error} />
                    <div class="flex flex-wrap gap-2">
                        {!r.here && !r.elsewhere && !r.lost && (
                            <button type="button" class="btn btn-primary btn-sm" disabled={act.busy}
                                onClick={() => act.run(() => api.post(`${base}/${r.id}/answer`))}>
                                It's mine
                            </button>
                        )}
                        <button type="button" class="btn btn-sm" disabled={act.busy} onClick={() => act.run(async () => {
                            await api.post(`${base}/${r.id}/refuse`);
                            setRefused(true);
                        })}>
                            Refuse
                        </button>
                    </div>
                    <p class="text-xs text-base-content/60">It waits until {clock(r.expires_at)}.</p>
                </div>
            ))}
        </div>
    );
}

const SIGN_IN_ENDED = {
    refused: 'Your other device refused this sign-in.',
    cancelled: 'This sign-in was cancelled: your den password or DM recovery key changed on another device meanwhile.',
    expired: 'None of your other devices approved this sign-in in time. Try again when one of them is at hand.',
};

// SignInSteps shows a sign-in on this device, as one screen for the step
// it's at (M8.3): waiting for another device to approve it, comparing
// digits with it, then signed in, or why it ended. The service holds the
// sign-in, so a reload comes back to its step. onRecover gives up waiting
// for another device, to sign in with a recovery code instead.
export function SignInSteps({ p, onRecover, onDone }) {
    const [digits, setDigits] = useState('');
    const act = useAction();
    const dismiss = () => act.run(async () => {
        await api.del(`/api/dens/signin/${p.id}`);
        onDone?.();
    });
    const going = p.stage === 'waiting' || p.stage === 'check' || p.stage === 'approved';
    let title = `Signing in to ${p.url} as @${p.username}`;
    if (p.stage === 'waiting') title = 'Approve this on another device';
    else if (p.stage === 'check' || p.stage === 'approved') title = 'Compare digits with your other device';
    else if (p.stage === 'done') title = 'Signed in';
    return (
        <div class="flex flex-col gap-3">
            <h3 class="text-lg font-semibold">
                {title}
                {going && <> <DocsTip id={`docs-signin-${p.id}`} page="join/#signing-in-from-another-computer">
                    Why a new device needs one of your others to let it in.</DocsTip></>}
            </h3>
            {p.stage === 'waiting' && (
                <>
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
                <form class="flex flex-col gap-2" onSubmit={(e) => {
                    e.preventDefault();
                    act.run(async () => {
                        await api.post(`/api/dens/signin/${p.id}/check`, { digits });
                        setDigits('');
                    });
                }}>
                    {p.checked ? (
                        <p>You typed your other device's digits. Now type this device's digits into it:</p>
                    ) : (
                        <p>Type these digits into your other device. They stay here until you're signed in.</p>
                    )}
                    <Digits value={p.half} />
                    {!p.checked && (
                        <>
                            <DigitsInput value={digits} onInput={setDigits} label="Then type the digits your other device shows" />
                            <div><SubmitButton busy={act.busy}>Check</SubmitButton></div>
                        </>
                    )}
                    {p.stage === 'approved' && !p.checked && (
                        <p class="text-sm text-base-content/70">Your other device approved; typing its digits here finishes signing in.</p>
                    )}
                    <p class="text-xs text-base-content/60">The digits only match between your two devices if nobody is in between.</p>
                </form>
            )}
            {p.stage === 'done' && <p class="text-success">You're signed in to {p.url}, and your DMs are readable here too.</p>}
            {SIGN_IN_ENDED[p.stage] && <p>{SIGN_IN_ENDED[p.stage]}</p>}
            {p.stage === 'failed' && <ErrorText message={p.error || 'Signing in failed.'} />}
            <ErrorText message={act.error} />
            <div>
                <button type="button" class={`btn btn-sm ${going ? 'btn-ghost' : 'btn-primary'}`} disabled={act.busy} onClick={dismiss}>
                    {going ? 'Cancel' : 'OK'}
                </button>
            </div>
        </div>
    );
}

// PendingSignIns shows the sign-ins on this device that the Add a den
// dialog isn't showing, as after a reload, each at its step.
export function PendingSignIns({ list, onRecover }) {
    if (!list?.length) return null;
    return list.map((p) => (
        <section key={p.id} class="card bg-base-200">
            <div class="card-body">
                <SignInSteps p={p} onRecover={onRecover} />
            </div>
        </section>
    ));
}

const KEY_WHY = "You'll need this to read your DMs if you lose all your devices. Keep it secret.";

// KeyShown shows a DM recovery key once, to keep, beside a den's recovery
// codes or after a reset.
export function KeyShown({ seal, fresh }) {
    return (
        <div class="flex flex-col gap-2">
            <h3 class="font-semibold">Your DM recovery key</h3>
            <p>{KEY_WHY}</p>
            {!fresh && <p class="text-sm text-base-content/70">It's the same key your other dens on this device use.</p>}
            <p class="select-all rounded bg-base-100 px-3 py-2 font-mono text-sm">{seal}</p>
            <div><CopyButton text={seal} label="Copy key" /></div>
        </div>
    );
}

// RecoveryCodes shows a den's recovery codes once, and the DM recovery key
// beside them when there is one to keep; after a reset, the key alone.
export function RecoveryCodes({ denName, codes, seal, sealNew, onDone }) {
    const [saved, setSaved] = useState(false);
    const what = codes.length && seal ? 'recovery codes and DM recovery key' : codes.length ? 'recovery codes' : 'DM recovery key';
    return (
        <div class="flex flex-col gap-3">
            <h3 class="text-lg font-semibold">
                Save your {what} for {denName}{' '}
                <DocsTip id="docs-keys" page="join/#your-passwords-and-keys">What each of these is for, and how to keep them.</DocsTip>
            </h3>
            {codes.length > 0 && (
                <>
                    <p>
                        If you forget your password for this den, one of these codes lets you set a new one. Each works once. They're
                        shown only now: store them somewhere safe, away from this device.
                    </p>
                    <ol class="grid gap-2 font-mono text-sm sm:grid-cols-2">
                        {codes.map((code) => <li key={code} class="whitespace-nowrap rounded bg-base-100 px-3 py-1">{code}</li>)}
                    </ol>
                    <div><CopyButton text={codes.join('\n')} label="Copy codes" /></div>
                </>
            )}
            {seal && <KeyShown seal={seal} fresh={sealNew} />}
            <label class="label gap-2">
                <input type="checkbox" class="checkbox" checked={saved} onChange={(e) => setSaved(e.currentTarget.checked)} />
                I've stored my {what}
            </label>
            <div>
                <button type="button" class="btn btn-primary" disabled={!saved} onClick={onDone}>Done</button>
            </div>
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
        <div role="status" class="alert alert-warning alert-soft flex flex-col items-stretch gap-2 text-left">
            <p>This device doesn't have your DM recovery key for {den.name}, so it can't read or send your direct messages there.</p>
            {mode === '' && (
                <div class="flex flex-wrap gap-2">
                    <button type="button" class="btn btn-sm btn-primary" onClick={() => setMode('type')}>Enter my key</button>
                    <button type="button" class="btn btn-sm" onClick={() => setMode('over')}>I've lost it</button>
                </div>
            )}
            {mode === 'type' && (
                <form class="flex flex-col gap-2" onSubmit={(e) => {
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
                    <div class="flex gap-2">
                        <SubmitButton busy={act.busy}>Use this key</SubmitButton>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => setMode('')}>Back</button>
                    </div>
                </form>
            )}
            {mode === 'over' && <ResetKey den={den} onDone={onSeal} onBack={() => setMode('')} />}
        </div>
    );
}

// ResetKey gives the member a new DM recovery key at a den, with their den
// password, once they've read what that does, and shows it once.
export function ResetKey({ den, onDone, onBack }) {
    const [password, setPassword] = useState('');
    const act = useAction();
    return (
        <form class="flex flex-col gap-2" onSubmit={(e) => {
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
            <div class="flex gap-2">
                <SubmitButton busy={act.busy} class="btn-error">Reset key</SubmitButton>
                {onBack && <button type="button" class="btn btn-ghost btn-sm" onClick={onBack}>Cancel</button>}
            </div>
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
                    <div class="flex gap-2">
                        <CopyButton text={shown} label="Copy key" />
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => setShown('')}>Hide</button>
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
                    <Field label="Local password" hint="The one that protects Dens on this device, not your den password.">
                        <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required />
                    </Field>
                    <ErrorText message={act.error} />
                    <div class="flex gap-2">
                        <SubmitButton busy={act.busy}>Show key</SubmitButton>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => setAsking('')}>Cancel</button>
                    </div>
                </form>
            ) : asking === 'reset' ? (
                <ResetKey den={den} onDone={(seal) => {
                    setAsking('');
                    onSeal(seal);
                }} onBack={() => setAsking('')} />
            ) : (
                <div class="flex flex-wrap gap-2">
                    <button type="button" class="btn btn-sm" onClick={() => setAsking('show')}>Show key</button>
                    <button type="button" class="btn btn-ghost btn-sm text-error" onClick={() => setAsking('reset')}>Reset key</button>
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
        <div class="divider divider-warning my-1 h-auto cursor-default select-none text-xs text-warning">
            <span class="max-w-[80%] shrink-0 whitespace-normal text-center">{text}</span>
        </div>
    );
}
