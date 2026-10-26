// Links to messages (M11). In a message, a link to one shows as a chip
// naming where it points, and a click opens the den and the channel at
// the message, as a reply's quote jumps to its original. When it can't,
// the chip says why: the member hasn't joined that den; the channel is
// gone or out of their sight, which look alike, so a staff-only channel
// stays hidden; or the message is gone.

import { createContext } from 'preact';
import { useContext, useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import { ICON } from './icons.jsx';

// LinkContext is what a den's page gives the links on it: the den open
// (denID, and view, its state), the dens joined (dens, as /api/dens lists
// them), and open(denID, channelID, messageID), which shows a message.
export const LinkContext = createContext(null);

export const CHANNEL_GONE = "That channel is gone, or you can't see it.";
export const MESSAGE_GONE = 'That message is gone: it was deleted, or the den no longer keeps it.';

export function notJoined(link) {
    return `You haven't joined the den at ${link.host}.`;
}

// Other dens' states, which name the channels their links point to: each
// fetched once a link points into it, and again once its channels or
// members change.
const others = new Map();
const listeners = new Set();
const NAMES = new Set(['den.updated', 'channel.created', 'channel.updated', 'channel.deleted', 'member.joined', 'member.updated', 'member.left']);
let watching = false;

function changed() {
    listeners.forEach((fn) => fn());
}

function watch() {
    if (watching) return;
    watching = true;
    onEvent((msg) => {
        if (msg.t === 'reconnected' && others.size) {
            others.clear();
            changed();
        } else if (msg.t === 'den' && others.has(msg.d.den) && (msg.d.reset || (msg.d.events || []).some((e) => NAMES.has(e.t)))) {
            others.delete(msg.d.den);
            changed();
        }
    });
}

// fetchOther fetches another den's state for its links' chips, unless
// it's fetched or on its way.
function fetchOther(denID) {
    if (others.has(denID)) return;
    watch();
    const entry = { view: null, failed: false };
    others.set(denID, entry);
    api.get(`/api/dens/${denID}/state`).then(
        (view) => {
            entry.view = view;
        },
        () => {
            entry.failed = true;
        },
    ).finally(() => others.get(denID) === entry && changed());
}

// otherView is another den's state as last fetched: null while it's on
// its way, and false when it couldn't be.
function otherView(denID) {
    const entry = others.get(denID);
    return entry?.failed ? false : entry?.view || null;
}

// denOf is the joined den a link points into, by its ID, or null. The
// den open is known by its own state too, before the list of dens comes.
function denOf(link, links) {
    const { view } = links;
    return links.dens.find((d) => d.den_id === link.den) || (link.den === links.denID && view ? { den_id: links.denID, name: view.name, url: view.url } : null);
}

// place is where a link to a message points, as far as this page knows:
// the den, if the member joined it; its state, null while that's fetched
// and false when it couldn't be; and the channel, a text channel or DM
// they can see.
export function place(link, links, viewOf = otherView) {
    const den = denOf(link, links);
    if (!den) return { den: null, view: null, channel: null };
    const view = den.den_id === links.denID ? links.view : viewOf(den.den_id);
    const channel = (view && view.channels.find((c) => c.id === link.channel && (c.kind === 'text' || c.kind === 'dm'))) || null;
    return { den, view, channel };
}

// chipLabel is what a link's chip reads: its channel, as #name, or as
// @username for a DM, after the den's name when it's another den's;
// #unknown for a channel gone or out of sight; and the host of a den the
// member hasn't joined.
export function chipLabel(link, { den, view, channel }, hereID) {
    if (!den) return link.host;
    let where = '';
    if (view === null) where = '…';
    else if (view && !channel) where = '#unknown';
    else if (channel?.kind === 'dm') where = `@${partner(view, channel)?.username || 'unknown'}`;
    else if (channel) where = `#${channel.name}`;
    if (den.den_id === hereID) return where;
    return where ? `${den.name} › ${where}` : den.name;
}

function partner(view, channel) {
    const id = channel.members.find((m) => m !== view.me.id);
    return view.members.find((m) => m.id === id);
}

// follow opens the message a link points to, and returns '' once it has,
// or why it can't. Another den's state is fetched afresh, as its channels
// may have changed since its chip was named.
export async function follow(link, links) {
    const den = denOf(link, links);
    if (!den) return notJoined(link);
    const view = den.den_id === links.denID ? links.view : await api.get(`/api/dens/${den.den_id}/state`);
    const { channel } = place(link, { ...links, view, denID: den.den_id });
    if (!channel) return CHANNEL_GONE;
    try {
        const page = await api.get(`/api/dens/${den.den_id}/channels/${channel.id}/messages?around=${link.message}&limit=1`);
        // A page around a message that's gone starts at the one after it.
        if (page.messages[0]?.id !== link.message) return MESSAGE_GONE;
    } catch {
        // A den that's offline, at a page this computer hasn't kept, opens
        // the channel as it would anyway, and the channel says why the
        // message isn't there.
    }
    links.open(den.den_id, channel.id, link.message);
    return '';
}

// MessageLink is a link to a message, as a chip naming where it points,
// with the address on hover. Outside a den's page, as in the guide to
// writing, it's an ordinary link. Opening it in a new tab, as with Ctrl,
// opens the den's address in the browser, which says what it is.
export function MessageLink({ url, link }) {
    const links = useContext(LinkContext);
    const [, setVersion] = useState(0);
    const [refusal, setRefusal] = useState('');
    const [busy, setBusy] = useState(false);
    const timer = useRef(null);
    const at = links ? place(link, links) : null;
    const otherDen = at?.den && at.den.den_id !== links.denID ? at.den.den_id : '';
    useEffect(() => {
        const fn = () => setVersion((n) => n + 1);
        listeners.add(fn);
        return () => {
            listeners.delete(fn);
            clearTimeout(timer.current);
        };
    }, []);
    useEffect(() => {
        if (otherDen) fetchOther(otherDen);
    }, [otherDen]);
    if (!links) {
        return <a href={url} target="_blank" rel="noopener noreferrer nofollow" class="link link-primary break-all">{url}</a>;
    }
    async function click(e) {
        if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
        e.preventDefault();
        if (busy) return;
        setBusy(true);
        let why = '';
        try {
            why = await follow(link, links);
        } catch (err) {
            why = err.message;
        }
        setBusy(false);
        setRefusal(why);
        clearTimeout(timer.current);
        if (why) timer.current = setTimeout(() => setRefusal(''), 6000);
    }
    return (
        <>
            {/* The chip keeps to one line, cut short when it's wider. */}
            <a href={url} title={url} aria-busy={busy} onClick={click}
                class={`inline-flex max-w-full items-center gap-1 rounded bg-primary/15 px-1 align-bottom font-medium text-primary hover:bg-primary/25 ${busy ? 'cursor-wait opacity-70' : ''}`}>
                <span class="truncate">{chipLabel(link, at, links.denID)}</span>
                <svg class="h-3.5 w-3.5 shrink-0" {...ICON}>
                    <path d="M3 3h10a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H7.5L4.5 13.5V11H3a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z" />
                </svg>
            </a>
            {refusal ? <span role="status" class="ml-1 text-xs text-error">{refusal}</span> : null}
        </>
    );
}
