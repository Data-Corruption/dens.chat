// Reactions (M15.3) on the page: the chips under a message, each emoji with
// how many reacted with it, marked where the member did; the events that
// change them, each applied to its emoji's set, so applying one twice
// changes nothing; a DM's sets, each member's, kept by their revision; and
// the tip that says who. It's kept apart from the components so the page's
// tests check it without a browser.

import { validName } from './emoji.js';
import { compareIds } from './ids.js';

// MAX_EMOJI is how many different emoji a message holds, and NAMES how
// many names a chip's tip lists before saying how many more.
export const MAX_EMOJI = 20;
export const NAMES = 15;

// reactionKey names an emoji among a message's reactions: one of
// Unicode's as itself, one of the den's by its ID, and in a DM's set by
// its name, as a DM's text writes it.
export function reactionKey(r) {
    if (r.emoji) return `u:${r.emoji}`;
    if (r.emoji_id) return `d:${r.emoji_id}`;
    return `n:${r.name}`;
}

// reactionOf is what reacting with a chip's emoji, or a picked one, sends:
// one of Unicode's, one of the den's by ID, or a name a DM's set holds.
export function reactionOf(r) {
    if (r.emoji) return { emoji: r.emoji };
    if (r.emoji_id) return { emoji_id: r.emoji_id };
    return { name: r.name };
}

/** chipsOf lists a message's reactions as its row shows them, in order: a
    channel's as the den keeps them, each emoji with its members in the
    order they reacted, and a DM's from its members' sets, the lower ID's
    first, each emoji once. A den's emoji the den no longer has goes, where
    denEmoji, the den's by ID, says; one a DM's set holds by name stays by
    its name. Each says whether me reacted with it. */
export function chipsOf(m, me, denEmoji) {
    const out = [];
    if (m.dm_reactions) {
        const at = new Map();
        const sets = [...m.dm_reactions].sort((a, b) => compareIds(a.member_id, b.member_id));
        for (const s of sets) {
            for (const e of s.emoji || []) {
                const item = validName(e) ? { name: e } : { emoji: e };
                const key = reactionKey(item);
                let chip = at.get(key);
                if (!chip) {
                    chip = { key, ...item, members: [] };
                    at.set(key, chip);
                    out.push(chip);
                }
                if (!chip.members.includes(s.member_id)) chip.members.push(s.member_id);
            }
        }
    } else {
        for (const r of m.reactions || []) {
            if (!r.members?.length || (r.emoji_id && denEmoji && !denEmoji.has(r.emoji_id))) continue;
            out.push({ key: reactionKey(r), ...reactionOf(r), members: r.members });
        }
    }
    for (const c of out) c.mine = c.members.includes(me);
    return out;
}

/** room says whether a message whose row shows chips takes a reaction
    with the emoji key names: one it has already, or a new one while it
    holds fewer than MAX_EMOJI */
export function room(chips, key) {
    return chips.length < MAX_EMOJI || chips.some((c) => c.key === key);
}

/** reacted applies a reaction's event to a message, and returns it
    changed, or the same message where the event changes nothing: a
    channel's reaction added or taken back, or staff's clearing, applied to
    its emoji's set; or a DM's member's set, where it's newer than the one
    the message holds */
export function reacted(m, t, d) {
    if (t === 'dm.reactions') {
        const sets = m.dm_reactions || [];
        const held = sets.find((s) => s.member_id === d.member_id);
        if (held && held.revision >= d.revision) return m;
        const set = { member_id: d.member_id, revision: d.revision, emoji: d.emoji || [] };
        return { ...m, dm_reactions: [...sets.filter((s) => s.member_id !== d.member_id), set] };
    }
    if (t !== 'reaction.added' && t !== 'reaction.removed' && t !== 'reaction.cleared') return m;
    const members = t === 'reaction.cleared' ? d.members || [] : [d.member_id];
    const key = reactionKey(d);
    const list = m.reactions || [];
    const i = list.findIndex((r) => reactionKey(r) === key);
    if (t === 'reaction.added') {
        if (i < 0) {
            if (list.length >= MAX_EMOJI) return m;
            return { ...m, reactions: [...list, { ...reactionOf(d), members }] };
        }
        const fresh = members.filter((id) => !list[i].members.includes(id));
        if (!fresh.length) return m;
        return { ...m, reactions: list.map((r, j) => (j === i ? { ...r, members: [...r.members, ...fresh] } : r)) };
    }
    if (i < 0) return m;
    const kept = list[i].members.filter((id) => !members.includes(id));
    if (kept.length === list[i].members.length) return m;
    const reactions = kept.length ? list.map((r, j) => (j === i ? { ...r, members: kept } : r)) : list.filter((_, j) => j !== i);
    return { ...m, reactions };
}

/** withReaction applies a reaction's event to the run's message it's on */
export function withReaction(list, t, d) {
    let changed = false;
    const messages = list.messages.map((m) => {
        if (m.id !== d.message_id) return m;
        const n = reacted(m, t, d);
        changed ||= n !== m;
        return n;
    });
    return changed ? { ...list, messages } : list;
}

/** keptReactions is what a message from the den, d, keeps of the copy the
    run holds: a DM's sets each the newer, by revision; and where d answers
    the member's own edit or tick, at the revision held, the reactions held,
    which events may have changed since the den answered. An event at that
    revision brings the den's reactions, as after being away. */
export function keptReactions(held, d, response) {
    let next = d;
    if (held.dm_reactions || d.dm_reactions) {
        const sets = new Map();
        for (const s of [...(d.dm_reactions || []), ...(held.dm_reactions || [])]) {
            const o = sets.get(s.member_id);
            if (!o || s.revision > o.revision) sets.set(s.member_id, s);
        }
        next = { ...next, dm_reactions: [...sets.values()] };
    }
    if (response && held.revision === d.revision) next = { ...next, reactions: held.reactions };
    return next;
}

/** whoReacted says who reacted with an emoji, for its chip's tip: You
    first, then names, up to NAMES, then how many more */
export function whoReacted(members, me, nameOf) {
    const names = members.includes(me) ? ['You'] : [];
    for (const id of members) if (id !== me) names.push(nameOf(id));
    if (names.length > NAMES) return `${names.slice(0, NAMES).join(', ')} and ${names.length - NAMES} more`;
    return names.length < 3 ? names.join(' and ') : `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
}
