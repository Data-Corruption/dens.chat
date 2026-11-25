// The quick switcher's list: the channels and DMs of every den this
// install is in, and the dens, matched as completion matches a
// member, by the start of a name or of a word in it. What has something
// unread comes first, the open den's before the others', and before
// anything is typed the list is the unread alone. The open den's come from
// its page's view, which showDen keeps here, and another den's from its
// state on this computer, so only what the member sees is listed. Forward
// lists the places a message can go from the same states.

import { unread } from './ids.js';
import { nameRank } from '../chat/emoji.js';
import { inOrder } from '../chat/lists.js';
import { ownNotes } from '../private/private.js';

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
// channels in the list's order, and its DMs. Each holds where it opens, its name and the words that match it, and what waits in it.
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
            out.push({ ...base, kind: 'dm', id: c.id, name, detail: other ? `@${other.username}` : '', words: other ? [name, other.username] : [name], ...waits(c),
                left: !other || !!other.left_at });
        }
    }
    return out;
}

// placesOf lists where a message can be forwarded in a den connected now:
// its text channels, where the den keeps a forward's mark, and where this
// device holds the member's seal, their DMs with members still in the den
// and their notes, whose ID is empty until the den makes them.
function placesOf(den, view) {
    if (view?.state !== 'connected') return [];
    const out = entriesOf(den, view).filter((e) => e.kind === 'text' ? !!view.forward : view.seal && e.kind === 'dm' && !e.left);
    if (view.seal && view.has_notes) out.push({ den: den.den_id, denName: den.name, kind: 'notes', id: ownNotes(view)?.id || '', name: 'Notes', words: ['Notes'] });
    return out;
}

// listed is what a list holds for query: dens as /api/dens lists them, of
// a den's entries by its view, views their states by den ID, null for one
// that couldn't be fetched, and here the den the page shows. shown says
// which show before anything is typed, and first which go first. Each
// entry has a key unique in the list.
function listed(dens, of, views, here, query, shown, first) {
    const q = query.trim().toLowerCase();
    const all = [];
    dens.forEach((d, at) => of(d, views.get(d.den_id)).forEach((e, order) => all.push({ ...e, key: `${e.den}:${e.kind}:${e.id}`, at, order })));
    const matched = all
        .filter((e) => q || shown(e))
        .map((e) => ({ e, rank: q ? Math.min(...e.words.map((w) => nameRank(w, q)).map((r) => (r < 0 ? Infinity : r))) : 0 }))
        .filter((x) => x.rank !== Infinity);
    const before = (b) => (b ? 0 : 1);
    matched.sort((a, b) => before(first(a.e)) - before(first(b.e)) || before(a.e.den === here) - before(b.e.den === here) ||
        a.rank - b.rank || a.e.at - b.e.at || a.e.order - b.e.order);
    return matched.slice(0, MOST).map((x) => x.e);
}

// switcherList is what the switcher lists for query: what has something
// unread first, and before anything is typed nothing else.
export function switcherList(dens, views, here, query) {
    return listed(dens, entriesOf, views, here, query, (e) => e.kind !== 'den' && e.unread, (e) => e.unread);
}

// forwardList is where Forward offers to send a message for query: every
// place, the open den's first.
export function forwardList(dens, views, here, query) {
    return listed(dens, placesOf, views, here, query, () => true, () => false);
}
