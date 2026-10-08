// How a channel view's run of messages changes: pages merge in, and events
// apply. It's kept apart from the list so the page's tests can check it
// without a browser.

import { compareIds } from './ids.js';

// WINDOW is the most messages a run holds, and PAGE how many a load
// fetches.
export const WINDOW = 200;
export const PAGE = 50;

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
    }
    return list;
}
