// Private DMs and device approval (M1.7) on the page: comparing a DM's
// check code, approving a new device, and the DM seal. The keys stay in
// the local service; the page only shows digits, and passes on what the
// member types.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import { CopyButton, ErrorText, Field, SubmitButton, TextInput, day, useAction } from './components.jsx';
import { sameDigits } from './private.js';

const SEAL_WHY =
    "This protects your end-to-end encrypted direct messages. Keep it secret and don't share it with anyone. You'll need it " +
    'to read your DMs if you lose every device.';

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
            <summary class="cursor-pointer select-none text-base-content/70">Where should we compare?</summary>
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
            <p>
                This device doesn't have your DM seal for this den, so it can't read or send direct messages here. Type it in on the
                home page, or start over there if you've lost it.
            </p>
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
                <p class="font-medium">Compare check codes with {name} before you talk here.</p>
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
// only then does this device hand over the member's DM seal.
export function SignInRequests({ den }) {
    const [digits, setDigits] = useState({});
    const [refused, setRefused] = useState(false);
    const act = useAction();
    const requests = den.requests || [];
    if (requests.length === 0) {
        return refused ? (
            <div role="status" class="alert alert-warning alert-soft mt-2 flex flex-wrap">
                <span>
                    You refused a sign-in. Whoever asked has your den password: change it under Devices and password, which signs
                    out every other device.
                </span>
                <button type="button" class="btn btn-ghost btn-sm" onClick={() => setRefused(false)}>Dismiss</button>
            </div>
        ) : null;
    }
    const base = `/api/dens/${den.den_id}/requests`;
    return (
        <div class="mt-2 flex flex-col gap-2">
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
                        refuse it.
                    </p>
                    {r.elsewhere ? (
                        <p class="text-sm">Another of your devices is approving it.</p>
                    ) : r.lost ? (
                        <p class="text-sm">
                            Dens on this computer restarted while you were approving it, which lost its part of the check. Refuse it, and
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
                    <p class="text-xs text-base-content/60">It waits until {new Date(r.expires_at).toLocaleTimeString()}.</p>
                </div>
            ))}
        </div>
    );
}

const SIGN_IN_ENDED = {
    refused: 'Your other device refused this sign-in.',
    cancelled: 'This sign-in was cancelled: your den password or DM seal changed on another device meanwhile.',
    expired: 'None of your other devices approved this sign-in in time. Try again when one of them is at hand.',
};

// PendingSignIns shows sign-ins on this device that wait for one of the
// member's other devices to approve them.
export function PendingSignIns({ list }) {
    const [digits, setDigits] = useState({});
    const act = useAction();
    if (!list?.length) return null;
    const dismiss = (id) => act.run(() => api.del(`/api/dens/signin/${id}`));
    return list.map((p) => (
        <section key={p.id} class="card bg-base-200">
            <div class="card-body gap-3">
                <h2 class="card-title">Signing in to {p.url} as @{p.username}</h2>
                {p.stage === 'waiting' && (
                    <p>
                        Approve this sign-in on one of your other devices: open Dens there, and it asks whether this device is yours.
                        It waits until {new Date(p.expires_at).toLocaleTimeString()}.
                    </p>
                )}
                {(p.stage === 'check' || p.stage === 'approved') && (
                    <form class="flex flex-col gap-2" onSubmit={(e) => {
                        e.preventDefault();
                        act.run(async () => {
                            await api.post(`/api/dens/signin/${p.id}/check`, { digits: digits[p.id] || '' });
                            setDigits({ ...digits, [p.id]: '' });
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
                                <DigitsInput value={digits[p.id] || ''} onInput={(v) => setDigits({ ...digits, [p.id]: v })}
                                    label="Then type the digits your other device shows" />
                                <div><SubmitButton busy={act.busy}>Check</SubmitButton></div>
                            </>
                        )}
                        {p.stage === 'approved' && !p.checked && (
                            <p class="text-sm text-base-content/70">Your other device approved; typing its digits here finishes signing in.</p>
                        )}
                        <p class="text-xs text-base-content/60">
                            The digits only match between your two devices if nobody is in between. Neither goes on until you've typed
                            each one's digits into the other.
                        </p>
                    </form>
                )}
                {p.stage === 'done' && <p class="text-success">Signed in. Your DMs are readable here too.</p>}
                {SIGN_IN_ENDED[p.stage] && <p>{SIGN_IN_ENDED[p.stage]}</p>}
                {p.stage === 'failed' && <ErrorText message={p.error || 'Signing in failed.'} />}
                <ErrorText message={act.error} />
                <div>
                    <button type="button" class="btn btn-sm" disabled={act.busy} onClick={() => dismiss(p.id)}>
                        {p.stage === 'waiting' || p.stage === 'check' || p.stage === 'approved' ? 'Cancel' : 'OK'}
                    </button>
                </div>
            </div>
        </section>
    ));
}

// SealShown shows a DM seal once, to keep, beside a den's recovery codes
// or after starting over.
export function SealShown({ seal, fresh }) {
    return (
        <div class="flex flex-col gap-2">
            <h3 class="font-semibold">Your DM seal</h3>
            <p>{SEAL_WHY}</p>
            {!fresh && <p class="text-sm text-base-content/70">It's the same seal your other dens on this computer use.</p>}
            <p class="select-all rounded bg-base-100 px-3 py-2 font-mono text-sm">{seal}</p>
            <div><CopyButton text={seal} label="Copy seal" /></div>
        </div>
    );
}

// SealNeeded offers a device with no DM seal for a den, as after a
// recovery code, to type the seal in or start over.
export function SealNeeded({ den, onSeal }) {
    const [mode, setMode] = useState('');
    const [seal, setSeal] = useState('');
    const act = useAction();
    return (
        <div role="status" class="alert alert-warning alert-soft mt-2 flex flex-col items-stretch gap-2 text-left">
            <p>This device doesn't have your DM seal for {den.name}, so it can't read or send your direct messages there.</p>
            {mode === '' && (
                <div class="flex flex-wrap gap-2">
                    <button type="button" class="btn btn-sm btn-primary" onClick={() => setMode('type')}>Type my DM seal</button>
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
                    <Field label="DM seal" hint="The one you saved when you joined, in groups of four. Case doesn't matter.">
                        <textarea class="textarea w-full font-mono" rows="2" value={seal} onInput={(e) => setSeal(e.currentTarget.value)} required></textarea>
                    </Field>
                    <ErrorText message={act.error} />
                    <div class="flex gap-2">
                        <SubmitButton busy={act.busy}>Use this seal</SubmitButton>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => setMode('')}>Back</button>
                    </div>
                </form>
            )}
            {mode === 'over' && (
                <StartOver den={den} onDone={onSeal} onBack={() => setMode('')} />
            )}
        </div>
    );
}

// StartOver gives the member a new DM seal at a den, with their den
// password, and shows it once.
export function StartOver({ den, onDone, onBack }) {
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
                Starting over makes a new DM seal. Your DMs in {den.name} before now become unreadable to you, though not to the people
                you talked with, and each DM needs a new check before anything more is sent in it. Your other devices there are signed
                out, since they hold the old seal.
            </p>
            <Field label="Den password">
                <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required />
            </Field>
            <ErrorText message={act.error} />
            <div class="flex gap-2">
                <SubmitButton busy={act.busy} class="btn-error">Start over</SubmitButton>
                {onBack && <button type="button" class="btn btn-ghost btn-sm" onClick={onBack}>Back</button>}
            </div>
        </form>
    );
}

// SealSection shows a den's DM seal after the local password, and starts
// over, under Devices and password.
export function SealSection({ den, onSeal }) {
    const [shown, setShown] = useState('');
    const [asking, setAsking] = useState('');
    const [password, setPassword] = useState('');
    const act = useAction();
    return (
        <div class="flex flex-col gap-2 border-t border-base-300 pt-3">
            <h3 class="font-medium">DM seal</h3>
            <p class="text-xs text-base-content/60">{SEAL_WHY}</p>
            {shown ? (
                <>
                    <p class="select-all rounded bg-base-100 px-3 py-2 font-mono text-sm">{shown}</p>
                    <div class="flex gap-2">
                        <CopyButton text={shown} label="Copy seal" />
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
                    <Field label="Local password" hint="The one that protects Dens on this computer, not your den password.">
                        <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required />
                    </Field>
                    <ErrorText message={act.error} />
                    <div class="flex gap-2">
                        <SubmitButton busy={act.busy}>Show</SubmitButton>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => setAsking('')}>Cancel</button>
                    </div>
                </form>
            ) : asking === 'over' ? (
                <StartOver den={den} onDone={(seal) => {
                    setAsking('');
                    onSeal(seal);
                }} onBack={() => setAsking('')} />
            ) : (
                <div class="flex flex-wrap gap-2">
                    <button type="button" class="btn btn-sm" onClick={() => setAsking('show')}>Show my DM seal</button>
                    <button type="button" class="btn btn-ghost btn-sm text-error" onClick={() => setAsking('over')}>Start over</button>
                </div>
            )}
            {asking === '' && !shown && (
                <p class="text-xs text-base-content/60">
                    Starting over is for a lost seal, or a stolen device that holds it: a new seal keeps what's written from now on
                    away from it.
                </p>
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
