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

// isPrivate reports whether a channel's messages are sealed by its
// members' Dens: a DM's, a group DM's (M16.3), or the member's notes
// (M17.3), all of which the Direct messages tab lists.
export function isPrivate(channel) {
    return channel?.kind === 'dm' || channel?.kind === 'group_dm' || channel?.kind === 'notes';
}

// isNotes reports whether a channel is the member's notes (M17.3).
export function isNotes(channel) {
    return channel?.kind === 'notes';
}

// ownNotes is the member's notes in a den, once the den made them as they
// first opened them, or null (M17.3).
export function ownNotes(view) {
    return (view?.channels || []).find(isNotes) || null;
}

// notesGate says what stands in the notes' composer's place (M17.3):
// no_seal, on a device without the member's DM recovery key, which their
// key comes from; or '' when they take notes.
export function notesGate(view) {
    return view.seal ? '' : 'no_seal';
}

// groupTitle is what a group DM is called (M16.3): its name, or its other
// members' names as its changes make them, the first two and how many more
// past three.
export function groupTitle(group, members, me) {
    if (group?.name) return group.name;
    const names = (group?.members || []).filter((id) => id !== me).map((id) => members.get(id)?.display_name || 'Unknown member');
    if (names.length === 0) return 'Just you';
    if (names.length <= 3) return names.join(', ');
    return `${names.slice(0, 2).join(', ')} and ${names.length - 2} others`;
}

// groupGate says what stands in a group's composer's place, or '' when it
// takes messages (M16.3): no_seal, on a device without the member's DM
// recovery key; checking, while the den holds changes this Dens hasn't
// checked yet; broken, once one doesn't check; alone, with nobody else
// left in it; update, while a member's Dens publishes no identity, being
// older; and held, once a member's keys changed since this member
// verified them. member is whom update and held name.
export function groupGate(view, channel) {
    const g = view.group_dms?.[channel.id];
    const me = view.me?.id;
    if (!view.seal) return { gate: 'no_seal' };
    if (g?.broken) return { gate: 'broken' };
    if (!g || g.syncing || !g.members.includes(me)) return { gate: 'checking' };
    const others = g.members.filter((id) => id !== me);
    if (others.length === 0) return { gate: 'alone' };
    const byID = new Map((view.members || []).map((m) => [m.id, m]));
    const old = others.find((id) => !byID.get(id)?.identity);
    if (old) return { gate: 'update', member: old };
    const held = others.find((id) => view.identities?.[id]?.held);
    if (held) return { gate: 'held', member: held };
    return { gate: '' };
}

// changeText says what a change to a group did, as its line in the group
// shows it (M16.3): who made it, You for this member, whom it took in or
// out, and a name where the change carries one this Dens could open.
export function changeText(change, nameOf, me) {
    const who = (id, start) => (id === me ? (start ? 'You' : 'you') : nameOf(id));
    const by = change.by ? who(change.by, true) : '';
    const them = (change.subjects || []).map((id) => who(id, false));
    const list = them.length <= 1 ? them.join('') : `${them.slice(0, -1).join(', ')} and ${them[them.length - 1]}`;
    const named = typeof change.name === 'string';
    switch (change.kind) {
        case 'made':
            return named && change.name ? `${by} made the group “${change.name}”.` : `${by} made the group.`;
        case 'added':
            return `${by} added ${list}.`;
        case 'removed':
            return `${by} removed ${list}.`;
        case 'left':
            return `${by} left the group.`;
        case 'departed':
            return `${them.length ? list[0].toUpperCase() + list.slice(1) : 'Someone'} left the den.`;
        case 'named':
            if (!named) return `${by} renamed the group.`;
            return change.name ? `${by} named the group “${change.name}”.` : `${by} took the group's name away.`;
    }
    return '';
}

// sameDigits reports whether two strings of digits match once spaces and
// dashes are gone, for telling a member they typed their own code.
export function sameDigits(a, b) {
    const clean = (s) => s.replace(/[\s-]/g, '');
    return clean(a) !== '' && clean(a) === clean(b);
}

// EXPIRY_TIMES are the times a DM's or a group's messages disappear after
// they're sent (M16.4), in seconds, Off first: an hour, a day, a week and
// four weeks, as the den takes them.
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

// expiryText says who last set a DM's or a group's time, when, and to
// what, as its line in the conversation shows it (M16.4): you for this
// member. The day stays said once the messages around the line are gone.
export function expiryText(after, set, nameOf, me) {
    const lead = `On ${when(set.at)}, ${set.by === me ? 'you' : nameOf(set.by)}`;
    if (!after) return `${lead} turned off disappearing messages.`;
    return `${lead} set messages to disappear ${afterText(after)} after they're sent.`;
}

// dueBy lists the messages whose time to disappear came by now (M16.4).
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
