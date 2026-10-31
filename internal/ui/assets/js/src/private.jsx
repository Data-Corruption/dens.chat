// Private DMs and device approval (M1.7) on the page: comparing a DM's
// check code, approving a new device, and the DM seal, which the page calls
// the DM recovery key (M8.3). The keys stay in the local service; the page
// only shows digits, and passes on what the member types.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import { CopyButton, DocsTip, ErrorText, Field, PLAIN, SubmitButton, TextInput, clock, day, useAction } from './components.jsx';
import { openManage } from './dialogs.js';
import { sameDigits } from './private.js';

// Digits shows the digits a screen reads out, large and in groups.
function Digits({ value }) {
    return <p class="select-all font-mono text-2xl tracking-wider" aria-label={value.replace(/ /g, ', ')}>{value}</p>;
}

// DigitsInput takes the digits the other side reads out.
function DigitsInput({ value, onInput, label }) {
    return (
        <Field label={label}>
            <TextInput value={value} onInput={onInput} inputmode="numeric" autocomplete="off" placeholder="0000 0000 0000 0000"
                class="input w-full font-mono text-lg tracking-wider" required />
        </Field>
    );
}

// WhereToCompare says where a check code is worth comparing, best first,
// and why.
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

// CheckPanel stands in for the composer in a DM while this member can't
// send in it: until its key exists, until both Dens have been online, and
// until this member types the digits the other reads out. After that it
// says whether the other member has checked too.
export function CheckPanel({ denID, channel, dm, keyID }) {
    const [st, setSt] = useState(null);
    const [digits, setDigits] = useState('');
    const [error, setError] = useState('');
    const act = useAction();
    const starting = useRef(false);
    const name = dm ? dm.display_name : 'the other member';

    async function load() {
        try {
            const next = await api.get(`/api/dens/${denID}/dms/${channel.id}/check`);
            setSt(next);
            setError('');
            // A DM with no key to use starts one: the first, or the next
            // after someone started over. If both members' Dens start at
            // once, the den keeps one and the other answers it.
            if (next.stage === 'none' && next.seal && !dm?.left_at && !starting.current) {
                starting.current = true;
                try {
                    await api.post(`/api/dens/${denID}/dms/${channel.id}/key`, { restart: false });
                    setSt(await api.get(`/api/dens/${denID}/dms/${channel.id}/check`));
                } finally {
                    starting.current = false;
                }
            }
        } catch (e) {
            setError(e.message);
        }
    }

    useEffect(() => {
        load();
        return onEvent((msg) => {
            if (msg.t === 'reconnected' || (msg.t === 'den' && msg.d.den === denID && msg.d.reset)) return load();
            if (msg.t !== 'den' || msg.d.den !== denID) return;
            if ((msg.d.events || []).some((e) => e.t === 'dm.key' && e.d.channel_id === channel.id)) load();
        });
    }, [denID, channel.id, keyID]);

    function check(e) {
        e.preventDefault();
        if (st?.half && sameDigits(digits, st.half)) {
            return act.setError(`Those are your own digits. Type the ones ${name} reads out to you.`);
        }
        act.run(async () => {
            await api.post(`/api/dens/${denID}/dms/${channel.id}/check`, { digits });
            setDigits('');
            await load();
        });
    }

    function restart() {
        act.run(async () => {
            await api.post(`/api/dens/${denID}/dms/${channel.id}/key`, { restart: true });
            setDigits('');
            await load();
        });
    }

    let body;
    if (!st) {
        body = error ? <ErrorText message={error} /> : <span class="loading loading-dots loading-sm"></span>;
    } else if (!st.seal) {
        body = (
            <div class="flex flex-wrap items-center gap-2">
                <p class="min-w-0 flex-1">
                    This device doesn't have your DM recovery key for this den, so it can't read or send direct messages here.
                </p>
                <button type="button" class="btn btn-sm" onClick={() => openManage(denID, 'password')}>Enter it</button>
            </div>
        );
    } else if (st.checked) {
        if (st.partner) return null;
        // Whoever checks first still has their digits to read out.
        return (
            <div class="border-t border-base-300 px-4 py-3 text-sm">
                {st.half && (
                    <>
                        <p class="text-base-content/70">{name} still has to type your digits. Read these out to them:</p>
                        <Digits value={st.half} />
                    </>
                )}
                <p class="text-xs text-base-content/60">
                    You typed {name}'s digits, so you can send now. {name} reads what you send once they've typed yours.
                </p>
            </div>
        );
    } else if (st.stage === 'none' || st.stage === 'offered' || st.stage === 'answered') {
        body = (
            <p class="text-base-content/70">
                Setting up this DM's encryption. It needs {name}'s Dens to be online once; then you'll both see digits to
                compare.
            </p>
        );
    } else {
        body = (
            <form class="flex flex-col gap-2" onSubmit={check}>
                <p class="font-medium">
                    Compare check codes with {name} before you talk here.{' '}
                    <DocsTip id={`docs-check-${channel.id}`} page="join/#direct-messages">Why you compare digits, and where it's safe to.</DocsTip>
                </p>
                <p class="text-sm text-base-content/70">Read these digits out to {name}. They stay here until {name} has typed them.</p>
                <Digits value={st.half} />
                <DigitsInput value={digits} onInput={setDigits} label={`Type the digits ${name} reads out to you`} />
                <ErrorText message={act.error} />
                <div class="flex flex-wrap items-center gap-2">
                    <SubmitButton busy={act.busy}>Check</SubmitButton>
                    <button type="button" class="btn btn-ghost btn-sm" disabled={act.busy} onClick={restart}>
                        The digits don't match: start again
                    </button>
                </div>
                <p class="text-xs text-base-content/60">
                    Digits that really don't match mean someone may be in the middle. Start again, and compare somewhere else.
                </p>
                <WhereToCompare name={name} />
            </form>
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
                            <button type="button" class="link text-sm" disabled={act.busy} onClick={() => act.run(async () => {
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
                become unreadable to you, though not to the people you talked with, and each DM needs its check codes compared again. Your
                other devices there are signed out, since they hold the old key.
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
export function KeyDivider({ text }) {
    return <div class="divider divider-warning my-1 cursor-default select-none text-xs text-warning">{text}</div>;
}

// checkedOn says when two members last compared a DM's codes, for its
// header.
export function checkedOn(key) {
    const checks = key?.checks || [];
    return checks.length === 2 ? day(Math.max(...checks.map((c) => c.at))) : '';
}
