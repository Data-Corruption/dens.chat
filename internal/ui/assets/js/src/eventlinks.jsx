// Links to events (M17.5). In a message, a link to an event shows as a
// chip naming it, as a link to a message names its channel, and under the
// message as the event's card (denevents.jsx). Both read the event here:
// one of the open den's events to come from its state, which keeps it
// current, and any other from the member's Dens, which answers from what
// it keeps for an event to come and asks the den for the rest, an ended one
// among them. A click on the chip opens the event in its den's events, or
// says why it can't: the member hasn't joined that den, the event ended, or
// it's gone or out of their sight, which look alike, so a staff-only event
// stays hidden.

import { useContext, useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import { LinkContext, denOf, notJoined } from './msglinks.jsx';

export const EVENT_GONE = "That event is gone, or you can't see it.";
export const EVENT_ENDED = 'That event ended.';

// The events links point to, by den and event, each as last read: on its
// way, read, gone (or out of sight) or not read, as with the den away. Each
// is read once a link to it shows, and again once its den tells of it.
const linked = new Map();
const listeners = new Set();
let watching = false;

const keyOf = (den, event) => `${den}/${event}`;

function changed() {
    listeners.forEach((fn) => fn());
}

// watch has an event read again once its den tells of a change to it, or
// once the page reconnects.
function watch() {
    if (watching) return;
    watching = true;
    onEvent((msg) => {
        if (msg.t === 'reconnected' && linked.size) {
            linked.clear();
            changed();
            return;
        }
        if (msg.t !== 'den') return;
        let dropped = false;
        for (const key of [...linked.keys()]) {
            if (!key.startsWith(`${msg.d.den}/`)) continue;
            const id = key.slice(msg.d.den.length + 1);
            if (msg.d.reset || (msg.d.events || []).some((e) => e.t.startsWith('event.') && (e.d?.id === id || e.d?.event_id === id))) {
                linked.delete(key);
                dropped = true;
            }
        }
        if (dropped) changed();
    });
}

// readEvent reads an event a link points to, unless it's read or on its way.
function readEvent(den, id) {
    const key = keyOf(den, id);
    if (linked.has(key)) return;
    watch();
    const entry = { state: 'loading', event: null };
    linked.set(key, entry);
    api.get(`/api/dens/${den}/events/${id}`).then(
        (e) => {
            entry.state = 'shown';
            entry.event = e;
        },
        (err) => {
            entry.state = err?.status === 404 ? 'gone' : 'failed';
        },
    ).finally(() => linked.get(key) === entry && changed());
}

// markRead keeps the mark a card's Interested made on an event it read, as
// the den answered, until the den's own event has it read again.
export function markRead(den, id, interest) {
    const entry = linked.get(keyOf(den, id));
    if (entry?.event) {
        entry.event = { ...entry.event, interested: interest.interested, mine: interest.mine };
        changed();
    }
}

const cached = (den, id) => linked.get(keyOf(den, id));

// linkedEvent is what a page knows of the event a link points to: the
// den, if the member joined it; the event, as read; and state, one of
// loading, shown, ended, gone and failed. An event of the open den's to
// come or live comes from its state; any other as lookup has it read.
export function linkedEvent(link, links, now = Date.now(), lookup = cached) {
    const den = links && denOf(link, links);
    if (!den) return { den: null, event: null, state: 'unjoined' };
    if (den.den_id === links.denID) {
        const e = (links.view?.events || []).find((x) => x.id === link.event);
        if (e && e.ends_at > now) return { den, event: e, state: 'shown' };
    }
    const entry = lookup(den.den_id, link.event);
    if (!entry || entry.state !== 'shown') return { den, event: null, state: entry?.state || 'loading' };
    // One its Dens kept, which the den would have ended by the clock, shows
    // as ended.
    const e = entry.event;
    return { den, event: e, state: e.ended || e.ends_at <= now ? 'ended' : 'shown' };
}

// useLinkedEvent is linkedEvent for a component, which reads the event
// where it must, and renders again as it's read or changes.
export function useLinkedEvent(link) {
    const links = useContext(LinkContext);
    const [, setVersion] = useState(0);
    useEffect(() => {
        const fn = () => setVersion((n) => n + 1);
        listeners.add(fn);
        return () => listeners.delete(fn);
    }, []);
    const at = linkedEvent(link, links);
    const want = at.den && !at.event && at.state === 'loading' ? at.den.den_id : '';
    useEffect(() => {
        if (want) readEvent(want, link.event);
    }, [want, link.event]);
    return { links, ...at };
}

// eventChipLabel is what an event link's chip reads: the event's name,
// after its den's name when it's another den's; an event gone or out of
// sight as such; and the host of a den the member hasn't joined.
export function eventChipLabel(link, { den, event, state }, hereID) {
    if (!den) return link.host;
    const what = event ? event.name : state === 'loading' ? '…' : 'an event';
    return den.den_id === hereID ? what : `${den.name} › ${what}`;
}

// followEvent opens the event a link points to, and returns '' once it has,
// or why it can't.
export function followEvent(link, at, links) {
    if (!at.den) return notJoined(link);
    if (at.state === 'ended') return EVENT_ENDED;
    if (at.state === 'gone') return EVENT_GONE;
    links.openEvent(at.den.den_id, link.event);
    return '';
}

// EventIcon is a calendar's page, as the Events row has it, at a chip's
// size.
function EventIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-3.5 w-3.5 shrink-0" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
            stroke-linejoin="round" aria-hidden="true">
            <rect x="2.5" y="3.5" width="11" height="10" rx="1.5" />
            <path d="M2.5 6.5h11M5.5 2v3M10.5 2v3" />
        </svg>
    );
}

// EventLink is a link to an event, as a chip naming it, with the address on
// hover. Outside a den's page, as in the guide to writing, it's an ordinary
// link; opening it in a new tab, as with Ctrl, opens the den's address in
// the browser, which says what it is.
export function EventLink({ url, link }) {
    const at = useLinkedEvent(link);
    const { links } = at;
    const [refusal, setRefusal] = useState('');
    useEffect(() => {
        if (!refusal) return undefined;
        const t = setTimeout(() => setRefusal(''), 6000);
        return () => clearTimeout(t);
    }, [refusal]);
    if (!links) {
        return <a href={url} target="_blank" rel="noopener noreferrer nofollow" class="link link-primary break-all">{url}</a>;
    }
    function click(e) {
        if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
        e.preventDefault();
        setRefusal(followEvent(link, at, links));
    }
    return (
        <>
            {/* The chip keeps to one line, cut short when it's wider. */}
            <a href={url} title={url} onClick={click} data-event-link={link.event}
                class="inline-flex max-w-full items-center gap-1 rounded bg-primary/15 px-1 align-bottom font-medium text-primary hover:bg-primary/25">
                <EventIcon />
                <span class="truncate">{eventChipLabel(link, at, links.denID)}</span>
            </a>
            {refusal ? <span role="status" class="ml-1 text-xs text-error">{refusal}</span> : null}
        </>
    );
}
