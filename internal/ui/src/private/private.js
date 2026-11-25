// What the page works out about private DMs without drawing it:
// where a DM's keys changed, what stands in its composer's place while it
// can't take messages, and what to say about a message that can't be
// opened. The keys themselves never reach the page.

import { compareIds } from '../core/ids.js';

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
// the check again before v1.9.0, and when the two checked the new key. From
// v1.9.0 a DM's keys change as identities do, which their own lines say
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
        // From a legacy key to a key sealed to identities.
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

// keysChangedAt places the lines that say a member's keys changed,
// at the times their Dens saw it: each before the first message sent
// after it, and those after every message loaded at the list's end once
// it holds the newest. One from before the first message loaded waits
// until the list reaches the start. It returns the times by the ID of the
// message each goes before, and those at the end.
export function keysChangedAt(messages, changes, opts) {
    return placeAt(messages, changes || [], (at) => at, opts);
}

// placeAt places lines among the messages loaded by their times, as
// keysChangedAt does, and returns them by the ID of the message each goes
// before, and those at the end.
export function placeAt(messages, items, timeOf, { hasOlder = false, hasNewer = false } = {}) {
    const before = new Map();
    const after = [];
    const first = messages[0]?.created_at;
    for (const item of [...items].sort((a, b) => timeOf(a) - timeOf(b))) {
        const at = timeOf(item);
        if (hasOlder && first !== undefined && at < first) continue;
        const m = messages.find((x) => x.created_at > at);
        if (m) {
            if (!before.has(m.id)) before.set(m.id, []);
            before.get(m.id).push(item);
        } else if (!hasNewer) {
            after.push(item);
        }
    }
    return { before, after };
}

// dmGate says what stands in a DM's composer's place, or '' when it takes
// messages: no_seal, on a device without the member's DM recovery key;
// den, on a den older than v1.9.0, which keeps no identities, without a key
// both members checked before it; update, while the other member's Dens
// publishes no identity, being older; and held, once the other's keys
// changed since this member verified them. A key both checked before
// v1.9.0 goes on as it did. A DM whose other member left is closed apart.
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

// isPrivate reports whether a channel's messages are sealed by its
// members' Dens: a DM's or the member's notes, both of which the Direct
// messages tab lists.
export function isPrivate(channel) {
    return channel?.kind === 'dm' || channel?.kind === 'notes';
}

// isNotes reports whether a channel is the member's notes.
export function isNotes(channel) {
    return channel?.kind === 'notes';
}

// ownNotes is the member's notes in a den, once the den made them as they
// first opened them, or null.
export function ownNotes(view) {
    return (view?.channels || []).find(isNotes) || null;
}

// notesGate says what stands in the notes' composer's place:
// no_seal, on a device without the member's DM recovery key, which their
// key comes from; or '' when they take notes.
export function notesGate(view) {
    return view.seal ? '' : 'no_seal';
}

// sameDigits reports whether two strings of digits match once spaces and
// dashes are gone, for telling a member they typed their own code.
export function sameDigits(a, b) {
    const clean = (s) => s.replace(/[\s-]/g, '');
    return clean(a) !== '' && clean(a) === clean(b);
}

// EXPIRY_TIMES are the times a DM's messages disappear after they're sent,
// in seconds, Off first: an hour, a day, a week and four weeks, as the den
// takes them.
export const EXPIRY_TIMES = [0, 3600, 86400, 604800, 2419200];

const UNITS = [[604800, 'week', 'w'], [86400, 'day', 'd'], [3600, 'hour', 'h'], [60, 'minute', 'm'], [1, 'second', 's']];

// afterText says how long a time of seconds is, in its largest whole unit:
// 1 hour, 4 weeks, or the seconds a development instance's den takes for
// the browser checks.
export function afterText(seconds) {
    const [size, unit] = UNITS.find(([size]) => seconds % size === 0 && seconds >= size) || UNITS[UNITS.length - 1];
    const n = seconds / size;
    return `${n} ${unit}${n === 1 ? '' : 's'}`;
}

// shortAfter says it in a few letters, as a header has room for: 1h, 4w.
export function shortAfter(seconds) {
    const [size, , letter] = UNITS.find(([size]) => seconds % size === 0 && seconds >= size) || UNITS[UNITS.length - 1];
    return `${seconds / size}${letter}`;
}

// expiryText says who last set a DM's time, when, and to what, as its line
// in the conversation shows it: you for this member. The day stays said once the messages around the line are gone.
export function expiryText(after, set, nameOf, me) {
    const lead = `On ${when(set.at)}, ${set.by === me ? 'you' : nameOf(set.by)}`;
    if (!after) return `${lead} turned off disappearing messages.`;
    return `${lead} set messages to disappear ${afterText(after)} after they're sent.`;
}

// dueBy lists the messages whose time to disappear came by now.
export function dueBy(messages, now) {
    return messages.filter((m) => m.expires_at && m.expires_at <= now);
}

// nextDue is when the next of the messages whose time is still to come
// goes, or 0 with none.
export function nextDue(messages, now) {
    let next = 0;
    for (const m of messages) {
        if (m.expires_at > now && (!next || m.expires_at < next)) next = m.expires_at;
    }
    return next;
}

/** recoveryText is what the recovery codes' file and Copy hold: the den,
    the member's username there, the codes and the DM recovery key, as
    many of those as there are */
export function recoveryText({ den, codes, seal }) {
    const lines = [`Den: ${den.name}`];
    if (den.url) lines.push(`Address: ${den.url}`);
    if (den.username) lines.push(`Username: ${den.username}`);
    if (codes.length) lines.push('', 'Recovery codes. Each signs you in once if you forget your den password.', ...codes);
    if (seal) lines.push('', 'DM recovery key. It reads your DMs if you lose every device: keep it secret.', seal);
    return `${lines.join('\n')}\n`;
}

/** recoveryFileName names the recovery codes' file for its den: the den's
    name, less what a file's name can't hold */
export function recoveryFileName(name) {
    // eslint-disable-next-line no-control-regex
    const safe = name.replace(/[\u0000-\u001f\u007f/\\:*?"<>|]/g, ' ').replace(/\s+/g, ' ').replace(/^[ .]+|[ .]+$/g, '');
    return `${safe || 'Den'} recovery codes.txt`;
}
