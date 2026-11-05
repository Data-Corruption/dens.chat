// What the page works out about private DMs (M1.7) without drawing it:
// where a DM's keys changed, what stands in its composer's place while it
// can't take messages, and what to say about a message that can't be
// opened. The keys themselves never reach the page.

import { compareIds } from './ids.js';

// LOCKED says why a DM message can't be opened, by the reason the local
// service gives.
export const LOCKED = {
    no_seal: "This device can't open this message without your DM seal.",
    unchecked: 'This was sent before you compared check codes in this DM, so it can\'t be opened.',
    lost: "This was sealed before you started over, so it can't be opened any more.",
    broken: "This message doesn't open: it was changed after it was sent.",
    keys: "This was sealed with keys your Dens doesn't keep for this DM's members, so it isn't shown. Someone may be in the middle.",
};

// keyChanges finds where a DM's messages move from one key to the next,
// and says why, where the change has words: who started over or started
// the check again before M16.1, and when the two checked the new key. From
// M16.1 a DM's keys change as identities do, which their own lines say
// (keysChangedAt), so only this member's own start over has one here. It
// returns, by the ID of the first message under a new key, what its
// divider says.
export function keyChanges(messages, keys, names, me) {
    const byID = new Map(keys.map((k) => [k.id, k]));
    const out = new Map();
    let prev = null;
    for (const m of messages) {
        if (!m.key_id) continue;
        if (prev && prev.key_id !== m.key_id && compareIds(m.key_id, prev.key_id) > 0) {
            const text = dividerText(byID.get(prev.key_id), byID.get(m.key_id), names, me);
            if (text) out.set(m.id, text);
        }
        prev = m;
    }
    return out;
}

function when(ms) {
    return new Date(ms).toLocaleDateString(undefined, { dateStyle: 'medium' });
}

// dividerText says why a DM's key changed from old to next, or nothing
// where a line of its own says it.
export function dividerText(old, next, names, me) {
    const who = (id) => names(id);
    if (old?.retired === 'started_over' && old.retired_by === me && (old.signed || next?.signed)) {
        return `You started over on ${when(old.retired_at)}.`;
    }
    if (old?.signed) return '';
    if (next?.signed) {
        // From before M16.1 to a key sealed to identities.
        if (old?.retired === 'started_over') return `${who(old.retired_by)} started over on ${when(old.retired_at)}.`;
        return 'The keys changed.';
    }
    let why = 'The keys changed';
    if (old?.retired === 'started_over') why = `${who(old.retired_by)} started over on ${when(old.retired_at)}`;
    else if (old?.retired === 'restarted') why = `${who(old.retired_by)} started the check again on ${when(old.retired_at)}`;
    const checks = next?.checks || [];
    if (checks.length === 2) {
        const last = Math.max(...checks.map((c) => c.at));
        return `${why}. You checked codes again on ${when(last)}.`;
    }
    return `${why}.`;
}

// keysChangedAt places the lines that say a member's keys changed (M16.1),
// at the times their Dens saw it: each before the first message sent
// after it, and those after every message loaded at the list's end once
// it holds the newest. One from before the first message loaded waits
// until the list reaches the start. It returns the times by the ID of the
// message each goes before, and those at the end.
export function keysChangedAt(messages, changes, { hasOlder = false, hasNewer = false } = {}) {
    const before = new Map();
    const after = [];
    const first = messages[0]?.created_at;
    for (const at of [...(changes || [])].sort((a, b) => a - b)) {
        if (hasOlder && first !== undefined && at < first) continue;
        const m = messages.find((x) => x.created_at > at);
        if (m) {
            if (!before.has(m.id)) before.set(m.id, []);
            before.get(m.id).push(at);
        } else if (!hasNewer) {
            after.push(at);
        }
    }
    return { before, after };
}

// dmGate says what stands in a DM's composer's place, or '' when it takes
// messages: no_seal, on a device without the member's DM recovery key;
// den, on a den older than M16.1, which keeps no identities, without a key
// both members checked before it; update, while the other member's Dens
// publishes no identity, being older; and held, once the other's keys
// changed since this member verified them. A key both checked before
// M16.1 goes on as it did. A DM whose other member left is closed apart.
export function dmGate(view, channel) {
    const me = view.me?.id;
    const other = (channel.members || []).find((id) => id !== me);
    if (!view.seal) return 'no_seal';
    const live = (view.dm_keys || []).find((k) => k.channel_id === channel.id && !k.retired);
    if (live && !live.signed && (live.checks || []).length === 2) return '';
    if (!view.keeps_identities) return 'den';
    if (!(view.members || []).find((m) => m.id === other)?.identity) return 'update';
    if (view.identities?.[other]?.held) return 'held';
    return '';
}

// partnerOf is the other member of a DM.
export function partnerOf(channel, me) {
    return (channel?.members || []).find((id) => id !== me) || '';
}

// sameDigits reports whether two strings of digits match once spaces and
// dashes are gone, for telling a member they typed their own code.
export function sameDigits(a, b) {
    const clean = (s) => s.replace(/[\s-]/g, '');
    return clean(a) !== '' && clean(a) === clean(b);
}
