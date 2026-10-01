// What the page works out about private DMs (M1.7) without drawing it:
// where a DM's keys changed, and what to say about a message that can't be
// opened. The keys themselves never reach the page.

import { compareIds } from './ids.js';

// LOCKED says why a DM message can't be opened, by the reason the local
// service gives.
export const LOCKED = {
    no_seal: "This device can't open this message without your DM seal.",
    unchecked: 'Compare check codes to read this.',
    lost: "This was sealed before you started over, so it can't be opened any more.",
    broken: "This message doesn't open: it was changed after it was sent.",
};

// keyChanges finds where a DM's messages move from one key to the next,
// and says why: who started over or started the check again, and when the
// two checked the new key. It returns, by the ID of the first message
// under a new key, what its divider says.
export function keyChanges(messages, keys, names) {
    const byID = new Map(keys.map((k) => [k.id, k]));
    const out = new Map();
    let prev = null;
    for (const m of messages) {
        if (!m.key_id) continue;
        if (prev && prev.key_id !== m.key_id && compareIds(m.key_id, prev.key_id) > 0) {
            out.set(m.id, dividerText(byID.get(prev.key_id), byID.get(m.key_id), names));
        }
        prev = m;
    }
    return out;
}

function when(ms) {
    return new Date(ms).toLocaleDateString(undefined, { dateStyle: 'medium' });
}

// dividerText says why a DM's key changed from old to next.
export function dividerText(old, next, names) {
    const who = (id) => names(id);
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

// dmKeyState sums up a DM's live key for the channel list: whether this
// member still has to compare codes there.
export function needsCheck(keys, channelID, me) {
    const live = keys.find((k) => k.channel_id === channelID && !k.retired);
    if (!live) return false;
    return live.stage === 'revealed' && !(live.checks || []).some((c) => c.member_id === me);
}

// sameDigits reports whether two strings of digits match once spaces and
// dashes are gone, for telling a member they typed their own half.
export function sameDigits(a, b) {
    const clean = (s) => s.replace(/[\s-]/g, '');
    return clean(a) !== '' && clean(a) === clean(b);
}
