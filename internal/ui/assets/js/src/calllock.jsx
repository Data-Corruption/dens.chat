// The call's lock (M16.2): beside how a call stands, it says whether the
// den seals its calls end to end, and opens the member's code to read out,
// with each other member of the call, their mark where the member compared
// codes with them, or Compare, which compares from there; a comparison
// holds in every call, DM and group with that member after. The call's
// list marks each member verified, and one whose keys changed.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { Avatar } from './avatar.jsx';
import { DocsLink, ErrorText, Tip, day } from './components.jsx';
import { LockIcon } from './icons.jsx';
import { Dialog } from './manage.jsx';
import { Codes, Digits, VerifiedMark } from './private.jsx';

// keysChanged says whether a member's keys changed in a way the call's
// list shows: since this member compared codes with them, which holds
// their DMs too, or since the call began.
export function keysChanged(kept, since) {
    return !!kept && (!!kept.held || (kept.changes || []).some((at) => at >= since));
}

// CallLock is the call's lock: a button that opens what it says, closed
// where the den seals its calls, open where it's older and doesn't.
export function CallLock({ call, den }) {
    const [open, setOpen] = useState(false);
    // Until the page knows whether the den seals its calls, it says nothing.
    if (!call || call.sealed === undefined) return null;
    const sealed = call.sealed;
    const label = sealed ? 'The call is end-to-end encrypted: compare codes' : "The call isn't end-to-end encrypted: this den runs an older Dens";
    return (
        <>
            <button type="button" class={`btn btn-ghost btn-xs btn-square relative shrink-0 touch-room ${sealed ? 'text-base-content/60' : 'text-warning'}`}
                aria-label={label} title={label} data-call-lock={sealed ? 'sealed' : 'open'} onClick={() => setOpen(true)}>
                <LockIcon open={!sealed} />
            </button>
            {open && <CallLockDialog call={call} den={den} onClose={() => setOpen(false)} />}
        </>
    );
}

// CallLockDialog is what the call's lock opens: the member's own code and
// the call's other members, each to compare with in place.
export function CallLockDialog({ call, den, onClose }) {
    const [comparing, setComparing] = useState(null);
    const [mine, setMine] = useState('');
    const [error, setError] = useState('');
    const me = den?.me;
    useEffect(() => {
        if (!call.sealed || !me) return;
        api.get(`/api/dens/${call.den}/members/${me}/code`).then((v) => setMine(v.mine), (e) => setError(e.message));
    }, [call.den, call.sealed, me]);
    if (!call.sealed) {
        return (
            <Dialog title="Not end-to-end encrypted" onClose={onClose}>
                <p class="text-sm" data-call-lock-dialog="open">
                    This den runs an older Dens, so its calls aren't end-to-end encrypted: the den can hear and see them. Its owner can update it.
                </p>
            </Dialog>
        );
    }
    if (comparing) {
        return (
            <Dialog title="Compare codes" onClose={onClose}>
                <div>
                    <button type="button" class="btn btn-ghost btn-sm -ml-2" onClick={() => setComparing(null)}>← The call</button>
                </div>
                <Codes denID={call.den} member={comparing} />
            </Dialog>
        );
    }
    const others = (den?.calls.find((c) => c.channel_id === call.channel)?.members || []).filter((m) => m.id !== me);
    const name = (id) => den?.names.get(id) || 'Unknown member';
    return (
        <Dialog title="End-to-end encrypted" onClose={onClose}>
            <div class="flex flex-col gap-3" data-call-lock-dialog="sealed">
                <p class="text-sm">
                    Only the call's members can hear and see it.{' '}
                    <DocsLink icon page="usage/#comparing-codes" label="Where to compare codes, and why" />
                </p>
                <div>
                    <p class="text-xs text-base-content/60">Your code</p>
                    {mine ? <Digits value={mine} /> : error ? <ErrorText message={error} /> : <span class="loading loading-dots loading-sm"></span>}
                </div>
                {others.length > 0 ? (
                    <ul class="flex flex-col gap-1" aria-label="In the call">
                        {others.map((m) => {
                            const kept = den?.identities?.[m.id];
                            const member = { id: m.id, display_name: name(m.id) };
                            return (
                                <li key={m.id} class="flex min-h-10 items-center gap-2">
                                    <Avatar member={member} size="sm" />
                                    <span class="min-w-0 flex-1 truncate">{member.display_name}</span>
                                    {keysChanged(kept, call.since) && <KeysChangedMark id={`lock-changed-${m.id}`} name={member.display_name} kept={kept} />}
                                    {kept?.verified ? (
                                        <span class="flex items-center gap-1 text-sm text-base-content/70">
                                            <VerifiedMark always id={`lock-verified-${m.id}`} name={member.display_name} />
                                            Verified
                                        </span>
                                    ) : (
                                        <button type="button" class="btn btn-sm" onClick={() => setComparing(member)}>Compare</button>
                                    )}
                                </li>
                            );
                        })}
                    </ul>
                ) : (
                    <p class="text-sm text-base-content/70">Nobody else is in the call yet.</p>
                )}
            </div>
        </Dialog>
    );
}

// KeysChangedMark marks a member of the call whose keys changed, as
// keysChanged says, which says when in its tip.
export function KeysChangedMark({ id, name, kept }) {
    const at = (kept?.changes || []).at(-1);
    return (
        <Tip id={id} label={`${name}'s keys changed`} trigger={
            <svg viewBox="0 0 16 16" class="h-3.5 w-3.5 shrink-0 text-warning" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
                stroke-linejoin="round" aria-hidden="true">
                <path d="M8 1.75l6.5 12H1.5z" />
                <path d="M8 6.5v3.25" />
                <circle cx="8" cy="12" r="0.4" fill="currentColor" />
            </svg>
        }>
            {name}'s keys changed{at ? ` on ${day(at)}` : ''}{kept?.held ? ', since you compared codes' : ''}.
        </Tip>
    );
}
