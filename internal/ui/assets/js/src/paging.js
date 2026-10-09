// How a channel view's run of messages changes: pages merge in, events
// apply, and the far end is cut, so the run stays bounded however tall its
// messages are. It's kept apart from the list so the page's tests can
// check it without a browser.

import { compareIds } from './ids.js';

// WINDOW is the most messages a run holds, and PAGE the most a load
// fetches. SCREENS is how far, in screens of the list, the run reaches
// beyond what's shown before its far end is cut: a run of photos is far
// taller than one of text, and the count alone would keep 200 of them.
// A page from history asks for about PAGE_SCREENS screens of messages,
// and at least MIN_PAGE, so a run of photos still moves along.
export const WINDOW = 200;
export const PAGE = 50;
export const SCREENS = 8;
export const PAGE_SCREENS = 3;
export const MIN_PAGE = 10;

// pageSize is how many messages a page asks for to fill about PAGE_SCREENS
// screens, given how tall the rows it continues from are on average: PAGE
// for short messages, fewer for tall ones, never under MIN_PAGE. Without
// rows to go by, it's PAGE.
export function pageSize(rowHeight, screen) {
    if (!(rowHeight > 0) || !(screen > 0)) return PAGE;
    return Math.max(MIN_PAGE, Math.min(PAGE, Math.round((PAGE_SCREENS * screen) / rowHeight)));
}

// fromPage is the run a page makes on its own.
export function fromPage(page) {
    return { messages: page.messages, hasOlder: page.has_older, hasNewer: page.has_newer, loaded: true };
}

export function mergeIn(messages, incoming) {
    const seen = new Set(messages.map((m) => m.id));
    const out = messages.concat(incoming.filter((m) => !seen.has(m.id)));
    out.sort((a, b) => compareIds(a.id, b.id));
    return out;
}

// withUpdate puts a changed message into the run. A tick's answer can
// arrive after a newer update, so the higher revision wins.
export function withUpdate(list, d) {
    const held = list.messages.find((m) => m.id === d.id);
    if (held && held.revision > d.revision) return list;
    const reply = { author_id: d.author_id, text: d.text, locked: !!d.locked };
    return { ...list, messages: list.messages.map((m) => (m.id === d.id ? d : m.reply_to === d.id ? { ...m, reply } : m)) };
}

// withDelete takes a deleted message out of the run, and the quote out of
// any reply to it.
export function withDelete(list, d) {
    return {
        ...list,
        messages: list.messages.filter((m) => m.id !== d.id).map((m) => (m.reply_to === d.id ? { ...m, reply: undefined } : m)),
    };
}

// withExpired takes out of the run what a retention pass deleted: every
// message up to through, in every channel, and the quotes of them in
// replies (M5). Passes delete the oldest first, so a run that lost any
// holds the oldest message left.
export function withExpired(list, through) {
    const messages = list.messages
        .filter((m) => compareIds(m.id, through) > 0)
        .map((m) => (m.reply_to && compareIds(m.reply_to, through) <= 0 ? { ...m, reply: undefined } : m));
    return { ...list, messages, hasOlder: list.hasOlder && messages.length === list.messages.length };
}

// withOlder adds a page from before the run, then cuts the run from below
// (see farIds) and down to WINDOW from its newest end. A run cut there no
// longer holds the newest message.
export function withOlder(list, page, below) {
    const merged = mergeIn(list.messages, page.messages);
    let messages = cutBelow(merged, below);
    if (messages.length > WINDOW) messages = messages.slice(0, WINDOW);
    return { ...list, messages, hasOlder: page.has_older, hasNewer: list.hasNewer || messages.length < merged.length };
}

// withNewer adds a page from after the run, then cuts the run from above
// and down to WINDOW from its oldest end.
export function withNewer(list, page, above) {
    const merged = mergeIn(list.messages, page.messages);
    let messages = cutAbove(merged, above);
    if (messages.length > WINDOW) messages = messages.slice(messages.length - WINDOW);
    return { ...list, messages, hasOlder: list.hasOlder || messages.length < merged.length, hasNewer: page.has_newer };
}

// atTail adds a new message to a run the view follows at its newest end,
// cut as withNewer cuts.
export function atTail(list, d, above) {
    return withNewer(list, { messages: [d], has_newer: list.hasNewer }, above);
}

// replayed applies again, on top of what a load fetched, the events that
// came while it was out: the service may have taken the page before them.
// Applying one twice changes nothing, so the run they already reached
// takes them as well. A new message joins only a run that holds the
// newest.
export function replayed(list, events) {
    for (const e of events) {
        if (e.t === 'message.created' && !list.hasNewer) list = { ...list, messages: mergeIn(list.messages, [e.d]) };
        else if (e.t === 'message.updated') list = withUpdate(list, e.d);
        else if (e.t === 'message.deleted') list = withDelete(list, e.d);
        else if (e.t === 'messages.expired') list = withExpired(list, e.d.through);
    }
    return list;
}

// farIds finds where a run reaches more than reach px beyond what's shown:
// above, the newest message that lies wholly further than that above the
// view, and below, the oldest wholly further below it, or null. rows are
// the run's messages in order as { id, top, bottom }, laid out in the same
// coordinates as the view's top and bottom.
export function farIds(rows, top, bottom, reach) {
    let above = null;
    let below = null;
    for (const r of rows) {
        if (r.bottom >= top - reach) break;
        above = r.id;
    }
    for (let i = rows.length - 1; i >= 0; i--) {
        if (rows[i].top <= bottom + reach) break;
        below = rows[i].id;
    }
    return { above, below };
}

// cutAbove drops the messages up to id and id itself, and cutBelow id and
// those after it. A null id cuts nothing.
export function cutAbove(messages, id) {
    return id ? messages.filter((m) => compareIds(m.id, id) > 0) : messages;
}

export function cutBelow(messages, id) {
    return id ? messages.filter((m) => compareIds(m.id, id) < 0) : messages;
}
