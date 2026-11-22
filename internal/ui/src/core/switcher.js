// The quick switcher's list: the channels, DMs and groups of every den
// this install is in, and the dens, matched as completion matches a
// member, by the start of a name or of a word in it. What has something
// unread comes first, the open den's before the others', and before
// anything is typed the list is the unread alone. The open den's come from
// its page's view, which showDen keeps here, and another den's from its
// state on this computer, so only what the member sees is listed.

import { unread } from './ids.js';
import { nameRank } from '../chat/emoji.js';
import { inOrder } from '../chat/lists.js';
import { groupTitle } from '../private/private.js';

// MOST is how many entries the list shows.
const MOST = 50;

let shown = { den: '', view: null };

// showDen keeps the view of the den the page shows, or forgets it with
// none.
export function showDen(den, view) {
    shown = { den, view };
}

// shownDen is the den the page shows and its view.
export function shownDen() {
    return shown;
}

// entriesOf lists what a den offers: itself, then its text and voice
// channels in the list's order, its DMs, and its groups. Each holds where
// it opens, its name and the words that match it, and what waits in it.
function entriesOf(den, view) {
    const base = { den: den.den_id, denName: den.name };
    const out = [{ ...base, kind: 'den', id: '', name: den.name, words: [den.name], unread: den.unread > 0, mentions: den.mentions || 0 }];
    if (!view) return out;
    const reads = new Map(view.read_states.map((r) => [r.channel_id, r]));
    const members = new Map(view.members.map((m) => [m.id, m]));
    const waits = (c) => ({ unread: unread(reads.get(c.id)), mentions: reads.get(c.id)?.mention_count || 0 });
    for (const c of inOrder(view)) {
        if (c.kind === 'voice') out.push({ ...base, kind: 'voice', id: c.id, name: c.name, words: [c.name], unread: false, mentions: 0 });
        else if (c.kind === 'text') out.push({ ...base, kind: 'text', id: c.id, name: c.name, words: [c.name], ...waits(c) });
    }
    for (const c of view.channels) {
        if (c.kind === 'dm') {
            const other = members.get(c.members.find((id) => id !== view.me.id));
            const name = other?.display_name || 'Unknown member';
            out.push({ ...base, kind: 'dm', id: c.id, name, detail: other ? `@${other.username}` : '', words: other ? [name, other.username] : [name], ...waits(c) });
        } else if (c.kind === 'group_dm') {
            const name = groupTitle(view.group_dms?.[c.id], members, view.me.id);
            out.push({ ...base, kind: 'group', id: c.id, name, words: [name], ...waits(c) });
        }
    }
    return out;
}

// switcherList is what the switcher lists for query: dens as /api/dens
// lists them, views their states by den ID, null for one that couldn't be
// fetched, and here the den the page shows. Each entry has a key unique in
// the list.
export function switcherList(dens, views, here, query) {
    const q = query.trim().toLowerCase();
    const all = [];
    dens.forEach((d, at) => entriesOf(d, views.get(d.den_id)).forEach((e, order) => all.push({ ...e, key: `${e.den}:${e.kind}:${e.id}`, at, order })));
    const matched = all
        .filter((e) => (q ? true : e.kind !== 'den' && e.unread))
        .map((e) => ({ e, rank: q ? Math.min(...e.words.map((w) => nameRank(w, q)).map((r) => (r < 0 ? Infinity : r))) : 0 }))
        .filter((x) => x.rank !== Infinity);
    const first = (b) => (b ? 0 : 1);
    matched.sort((a, b) => first(a.e.unread) - first(b.e.unread) || first(a.e.den === here) - first(b.e.den === here) ||
        a.rank - b.rank || a.e.at - b.e.at || a.e.order - b.e.order);
    return matched.slice(0, MOST).map((x) => x.e);
}
