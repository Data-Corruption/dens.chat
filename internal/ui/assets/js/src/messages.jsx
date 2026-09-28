// A channel's message list and composer.
//
// The list holds one contiguous run of at most WINDOW messages, never the
// whole channel. Scrolling near either end loads the next page and trims
// the far end, so memory stays flat however far back a member scrolls.
// While the run holds the newest message the list is attached to the live
// tail and new messages append; jumping to an old message (a reply's
// quote) loads the page around it and detaches, and new messages then only
// update a "jump to present" bar.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import { Avatar } from './avatar.jsx';
import { compareIds, newNonce } from './ids.js';
import { Markdown, Preview } from './markdown.jsx';
import { rank } from './people.jsx';

const WINDOW = 200;
const PAGE = 50;
const EDGE = 600; // px from an end at which the next page loads
const READ_EVERY = 2000; // ms between read position updates
const GROUP_GAP = 5 * 60 * 1000;

function mergeIn(messages, incoming) {
    const seen = new Set(messages.map((m) => m.id));
    const out = messages.concat(incoming.filter((m) => !seen.has(m.id)));
    out.sort((a, b) => compareIds(a.id, b.id));
    return out;
}

// MessagePane shows a channel or DM. dm is the other member of a DM;
// typing lists who is typing here; closed, when set, says why nothing can
// be sent.
export function MessagePane({ denID, channel, me, members, readPosition, role, dm, typing, closed, onProfile, onTyping }) {
    const [list, setList] = useState({ messages: [], hasOlder: false, hasNewer: false, loaded: false });
    const [pending, setPending] = useState([]);
    const [highlight, setHighlight] = useState(null);
    const [newCount, setNewCount] = useState(0);
    const [replyTo, setReplyTo] = useState(null);
    const [editing, setEditing] = useState(null);
    const [error, setError] = useState('');
    const [divider] = useState(readPosition || '');
    const scroller = useRef(null);
    const content = useRef(null);
    const anchor = useRef(null);
    const loading = useRef({ older: false, newer: false });
    const atBottom = useRef(true);
    const lastTop = useRef(0);
    const listRef = useRef(list);
    listRef.current = list;
    const readUpTo = useRef(readPosition || '');
    const readTimer = useRef(null);
    const unsentRead = useRef('');
    const highlightTimer = useRef(null);
    const base = `/api/dens/${denID}/channels/${channel.id}/messages`;

    // captureAnchor remembers where the topmost visible message sits, so
    // content added or trimmed around it doesn't move what's being read.
    function captureAnchor() {
        const box = scroller.current;
        if (!box) return;
        const top = box.getBoundingClientRect().top;
        for (const el of box.querySelectorAll('[data-id]')) {
            const rect = el.getBoundingClientRect();
            if (rect.bottom > top) {
                anchor.current = { id: el.dataset.id, offset: rect.top - top };
                return;
            }
        }
    }

    useLayoutEffect(() => {
        const box = scroller.current;
        const a = anchor.current;
        anchor.current = null;
        if (!box || !list.loaded) return;
        if (a?.bottom) {
            box.scrollTop = box.scrollHeight;
        } else if (a) {
            const el = box.querySelector(`[data-id="${a.id || a.jump}"]`);
            if (el && a.jump) el.scrollIntoView({ block: 'center' });
            else if (el) box.scrollTop += el.getBoundingClientRect().top - box.getBoundingClientRect().top - a.offset;
        }
        // A list too short to scroll gets no scroll events.
        checkEdges();
    }, [list]);

    async function loadNewest() {
        try {
            const page = await api.get(`${base}?limit=${PAGE}`);
            anchor.current = { bottom: true };
            setList({ messages: page.messages, hasOlder: page.has_older, hasNewer: page.has_newer, loaded: true });
            setNewCount(0);
            setError('');
        } catch (e) {
            setError(e.message);
        }
    }

    async function loadOlder() {
        const cur = listRef.current;
        if (loading.current.older || !cur.hasOlder || !cur.messages.length) return;
        loading.current.older = true;
        try {
            const page = await api.get(`${base}?before=${cur.messages[0].id}&limit=${PAGE}`);
            captureAnchor();
            setList((l) => {
                let messages = mergeIn(l.messages, page.messages);
                let hasNewer = l.hasNewer;
                if (messages.length > WINDOW) {
                    messages = messages.slice(0, WINDOW);
                    hasNewer = true;
                }
                return { ...l, messages, hasOlder: page.has_older, hasNewer };
            });
        } catch (e) {
            setError(e.message);
        } finally {
            loading.current.older = false;
        }
    }

    async function loadNewer() {
        const cur = listRef.current;
        if (loading.current.newer || !cur.hasNewer || !cur.messages.length) return;
        loading.current.newer = true;
        try {
            const page = await api.get(`${base}?after=${cur.messages[cur.messages.length - 1].id}&limit=${PAGE}`);
            captureAnchor();
            setList((l) => {
                let messages = mergeIn(l.messages, page.messages);
                let hasOlder = l.hasOlder;
                if (messages.length > WINDOW) {
                    messages = messages.slice(messages.length - WINDOW);
                    hasOlder = true;
                }
                return { ...l, messages, hasOlder, hasNewer: page.has_newer };
            });
            if (!page.has_newer) setNewCount(0);
        } catch (e) {
            setError(e.message);
        } finally {
            loading.current.newer = false;
        }
    }

    async function jumpTo(id) {
        let page = null;
        if (!listRef.current.messages.some((m) => m.id === id)) {
            try {
                page = await api.get(`${base}?around=${id}&limit=${PAGE}`);
            } catch (e) {
                setError(e.message);
                return;
            }
        }
        anchor.current = { jump: id };
        if (page) setList({ messages: page.messages, hasOlder: page.has_older, hasNewer: page.has_newer, loaded: true });
        else setList((l) => ({ ...l }));
        setHighlight(id);
        clearTimeout(highlightTimer.current);
        highlightTimer.current = setTimeout(() => setHighlight(null), 2500);
    }

    // markRead moves the read position to the newest message once it's on
    // screen. Updates go out at most every READ_EVERY, so a busy channel
    // costs the den one small write every few seconds, not one per message.
    function markRead() {
        const l = listRef.current;
        if (l.hasNewer || !atBottom.current || document.visibilityState !== 'visible' || !l.messages.length) return;
        const last = l.messages[l.messages.length - 1].id;
        if (compareIds(last, readUpTo.current) <= 0) return;
        unsentRead.current = last;
        if (!readTimer.current) readTimer.current = setTimeout(sendRead, READ_EVERY);
    }

    function sendRead() {
        readTimer.current = null;
        const id = unsentRead.current;
        if (compareIds(id, readUpTo.current) <= 0) return;
        readUpTo.current = id;
        api.put(`/api/dens/${denID}/channels/${channel.id}/read`, { message_id: id }).catch(() => {});
    }

    function checkEdges() {
        const box = scroller.current;
        if (!box) return;
        const fromBottom = box.scrollHeight - box.scrollTop - box.clientHeight;
        // A resize can fire a scroll event without moving anything, before
        // the resize observer pins the list; only a move lets go of the end.
        if (box.scrollTop !== lastTop.current || fromBottom < 80) atBottom.current = fromBottom < 80;
        lastTop.current = box.scrollTop;
        if (box.scrollTop < EDGE) loadOlder();
        if (fromBottom < EDGE) loadNewer();
        if (atBottom.current && !listRef.current.hasNewer) {
            setNewCount(0);
            markRead();
        }
    }

    // Keep the newest message in view when the list or its box changes
    // size: a growing composer, a rotated phone, text rewrapping.
    useEffect(() => {
        const box = scroller.current;
        if (!box || typeof ResizeObserver === 'undefined') return;
        const observer = new ResizeObserver(() => {
            if (!atBottom.current || listRef.current.hasNewer) return;
            box.scrollTop = box.scrollHeight;
            // The scroll event this causes can arrive after more layout
            // changes; it isn't the member moving away from the end.
            lastTop.current = box.scrollTop;
        });
        observer.observe(box);
        observer.observe(content.current);
        return () => observer.disconnect();
    }, []);

    useEffect(() => {
        loadNewest();
        const onVisible = () => markRead();
        document.addEventListener('visibilitychange', onVisible);
        const stop = onEvent((msg) => {
            if (msg.t === 'reconnected' || (msg.t === 'den' && msg.d.den === denID && msg.d.reset)) {
                loadNewest();
                return;
            }
            if (msg.t !== 'den' || msg.d.den !== denID || !msg.d.events) return;
            for (const e of msg.d.events) handleEvent(e);
        });
        return () => {
            stop();
            document.removeEventListener('visibilitychange', onVisible);
            // Leaving the channel sends what was read without waiting.
            clearTimeout(readTimer.current);
            sendRead();
            clearTimeout(highlightTimer.current);
        };
    }, []);

    function handleEvent(e) {
        const d = e.d;
        if (!d || d.channel_id !== channel.id) return;
        if (e.t === 'message.created') {
            if (d.nonce) setPending((p) => p.filter((x) => x.nonce !== d.nonce));
            const mine = d.author_id === me.id;
            // The den moves the read position past a member's own message.
            if (mine && compareIds(d.id, readUpTo.current) > 0) readUpTo.current = d.id;
            const l = listRef.current;
            if (l.hasNewer) {
                if (mine) loadNewest();
                else setNewCount((n) => n + 1);
                return;
            }
            const follow = atBottom.current || mine;
            anchor.current = follow ? { bottom: true } : null;
            if (!follow) setNewCount((n) => n + 1);
            setList((cur) => {
                let messages = mergeIn(cur.messages, [d]);
                let hasOlder = cur.hasOlder;
                if (messages.length > WINDOW && follow) {
                    messages = messages.slice(messages.length - WINDOW);
                    hasOlder = true;
                }
                return { ...cur, messages, hasOlder };
            });
            if (follow) setTimeout(markRead, 0);
        } else if (e.t === 'message.updated') {
            const reply = { author_id: d.author_id, text: d.text };
            setList((cur) => ({
                ...cur,
                messages: cur.messages.map((m) => (m.id === d.id ? d : m.reply_to === d.id ? { ...m, reply } : m)),
            }));
        } else if (e.t === 'message.deleted') {
            setList((cur) => ({
                ...cur,
                messages: cur.messages.filter((m) => m.id !== d.id).map((m) => (m.reply_to === d.id ? { ...m, reply: undefined } : m)),
            }));
        }
    }

    async function send(text, pendingEntry) {
        if (listRef.current.hasNewer) loadNewest();
        const entry = pendingEntry || { nonce: newNonce(), text, reply_to: replyTo?.id || '', failed: false };
        setPending((p) => [...p.filter((x) => x.nonce !== entry.nonce), { ...entry, failed: false }]);
        if (!pendingEntry) setReplyTo(null);
        anchor.current = { bottom: true };
        try {
            const m = await api.post(base, { nonce: entry.nonce, text: entry.text, reply_to: entry.reply_to || undefined });
            setPending((p) => p.filter((x) => x.nonce !== entry.nonce));
            if (compareIds(m.id, readUpTo.current) > 0) readUpTo.current = m.id;
            if (!listRef.current.hasNewer) {
                anchor.current = { bottom: true };
                setList((cur) => ({ ...cur, messages: mergeIn(cur.messages, [m]) }));
            } else {
                loadNewest();
            }
        } catch (e) {
            setPending((p) => p.map((x) => (x.nonce === entry.nonce ? { ...x, failed: true, error: e.message } : x)));
        }
    }

    function editLast() {
        const mine = listRef.current.messages.filter((m) => m.author_id === me.id);
        if (mine.length) setEditing(mine[mine.length - 1].id);
    }

    const byId = new Map(list.messages.map((m) => [m.id, m]));
    const rows = [];
    let prev = null;
    for (const m of list.messages) {
        const day = new Date(m.created_at).toDateString();
        if (!prev || new Date(prev.created_at).toDateString() !== day) {
            rows.push(<DayDivider key={`day-${m.id}`} time={m.created_at} />);
        }
        if (divider && compareIds(m.id, divider) > 0 && (!prev || compareIds(prev.id, divider) <= 0) && m.author_id !== me.id) {
            rows.push(<div key="unread" class="divider divider-error my-0 cursor-default select-none text-xs text-error">New</div>);
        }
        const compact = prev && prev.author_id === m.author_id && !m.reply_to && m.created_at - prev.created_at < GROUP_GAP &&
            new Date(prev.created_at).toDateString() === day;
        // The loaded copy is newer than the den's preview, if there is one.
        const replied = m.reply_to ? byId.get(m.reply_to) || m.reply : null;
        rows.push(
            <MessageRow
                key={m.id}
                m={m}
                compact={compact}
                author={members.get(m.author_id)}
                replied={replied}
                repliedAuthor={replied ? members.get(replied.author_id) : null}
                me={me}
                highlighted={highlight === m.id}
                editing={editing === m.id}
                canDelete={m.author_id === me.id || rank(role) > rank(members.get(m.author_id)?.role)}
                onProfile={onProfile}
                onReply={() => setReplyTo(m)}
                onEdit={() => setEditing(m.id)}
                onEditDone={() => setEditing(null)}
                onJump={jumpTo}
                denID={denID}
            />,
        );
        prev = m;
    }

    return (
        <div class="flex min-h-0 flex-1 flex-col">
            <div ref={scroller} class="min-h-0 flex-1 overflow-y-auto px-4 py-2" onScroll={checkEdges}>
                <div ref={content}>
                    {list.loaded && !list.hasOlder && (
                        <div class="cursor-default select-none py-6 text-center text-sm text-base-content/60">
                            {dm ? (
                                <>
                                    <p>This is the start of your conversation with {dm.display_name}.</p>
                                    <p class="text-xs">Only the two of you see it here, but the den's owner can read everything on the den, this included.</p>
                                </>
                            ) : (
                                `This is the start of #${channel.name}.`
                            )}
                        </div>
                    )}
                    {list.hasOlder && <div class="py-3 text-center"><span class="loading loading-dots loading-sm"></span></div>}
                    {!list.loaded && !error && <span class="loading loading-spinner"></span>}
                    {rows}
                    {!list.hasNewer && pending.map((p) => <PendingRow key={p.nonce} p={p} me={me} onRetry={() => send(p.text, p)} onDiscard={() => setPending((ps) => ps.filter((x) => x.nonce !== p.nonce))} />)}
                    {list.hasNewer && <div class="py-3 text-center"><span class="loading loading-dots loading-sm"></span></div>}
                </div>
            </div>
            {(list.hasNewer || newCount > 0) && (
                <div class="flex cursor-default select-none items-center justify-between bg-primary px-4 py-1 text-sm text-primary-content">
                    <span>
                        {newCount > 0 ? `${newCount} new message${newCount === 1 ? '' : 's'}` : "You're viewing older messages"}
                    </span>
                    <button type="button" class="btn btn-ghost btn-xs" onClick={() => (list.hasNewer ? loadNewest() : jumpToBottom())}>
                        Jump to present
                    </button>
                </div>
            )}
            {error && <div role="alert" class="alert alert-error alert-soft mx-4 my-1 py-1 text-sm">{error}</div>}
            {closed ? (
                <div class="cursor-default select-none border-t border-base-300 px-4 py-3 text-sm text-base-content/60">{closed}</div>
            ) : (
                <Composer
                    placeholder={dm ? `Message @${dm.username}` : `Message #${channel.name}`}
                    replyTo={replyTo}
                    replyAuthor={replyTo ? members.get(replyTo.author_id) : null}
                    typing={typing}
                    onCancelReply={() => setReplyTo(null)}
                    onSend={(text) => send(text)}
                    onEditLast={editLast}
                    onTyping={onTyping}
                />
            )}
        </div>
    );

    function jumpToBottom() {
        anchor.current = { bottom: true };
        setNewCount(0);
        setList((l) => ({ ...l }));
    }
}

function DayDivider({ time }) {
    return <div class="divider my-1 cursor-default select-none text-xs text-base-content/50">{new Date(time).toLocaleDateString(undefined, { dateStyle: 'medium' })}</div>;
}

function name(member) {
    return member ? member.display_name : 'Unknown member';
}

function clock(ms) {
    return new Date(ms).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

function MessageRow({ m, compact, author, replied, repliedAuthor, me, highlighted, editing, canDelete, onReply, onEdit, onEditDone, onJump, onProfile, denID }) {
    const [confirming, setConfirming] = useState(false);
    const [error, setError] = useState('');
    async function remove() {
        try {
            await api.del(`/api/dens/${denID}/messages/${m.id}`);
        } catch (e) {
            setError(e.message);
        }
        setConfirming(false);
    }
    return (
        <div
            data-id={m.id}
            class={`group relative rounded px-2 ${compact ? 'py-0' : 'mt-2 py-0.5'} ${highlighted ? 'bg-warning/20' : 'hover:bg-base-200'}`}
        >
            {m.reply_to && replied && (
                <button type="button" class="flex w-full min-w-0 items-center gap-1 pl-2 text-left text-xs text-base-content/60 hover:text-base-content" onClick={() => onJump(m.reply_to)}>
                    <Spine />
                    <Avatar member={repliedAuthor} size="sm" />
                    <span class="shrink-0 font-medium">{name(repliedAuthor)}</span>
                    <span class="truncate"><Preview text={replied.text} me={me} /></span>
                </button>
            )}
            {m.reply_to && !replied && (
                <div class="flex items-center gap-1 pl-2 text-xs italic text-base-content/50">
                    <Spine />
                    The original message was deleted.
                </div>
            )}
            <div class="flex gap-3">
                <div class="relative w-9 shrink-0">
                    {compact ? (
                        // Out of the flow, so the time never makes a row taller.
                        <span class="invisible absolute -inset-x-1 top-1 cursor-default select-none whitespace-nowrap text-center text-[10px] text-base-content/50 group-hover:visible">
                            {clock(m.created_at)}
                        </span>
                    ) : (
                        <button type="button" class="mt-0.5 rounded-full" onClick={() => author && onProfile(author)} aria-label={`${name(author)}'s profile`}>
                            <Avatar member={author} />
                        </button>
                    )}
                </div>
                <div class="min-w-0 flex-1">
                    {!compact && (
                        <div class="flex items-baseline gap-2">
                            <button type="button" class="font-semibold hover:underline" onClick={() => author && onProfile(author)}>{name(author)}</button>
                            <span class="text-xs text-base-content/50" title={new Date(m.created_at).toLocaleString()}>{clock(m.created_at)}</span>
                        </div>
                    )}
                    {editing ? (
                        <EditBox m={m} denID={denID} onDone={onEditDone} />
                    ) : (
                        <div class="break-words">
                            <Markdown text={m.text} me={me} />
                            {m.edited_at ? <span class="cursor-default select-none text-xs text-base-content/50" title={new Date(m.edited_at).toLocaleString()}> (edited)</span> : null}
                        </div>
                    )}
                    {error && <p class="text-xs text-error">{error}</p>}
                </div>
            </div>
            {!editing && (
                <div class="absolute -top-3 right-2 hidden gap-1 rounded bg-base-100 shadow group-hover:flex">
                    <button type="button" class="btn btn-ghost btn-xs" onClick={onReply}>Reply</button>
                    {m.author_id === me.id && <button type="button" class="btn btn-ghost btn-xs" onClick={onEdit}>Edit</button>}
                    {canDelete && !confirming && <button type="button" class="btn btn-ghost btn-xs text-error" onClick={() => setConfirming(true)}>Delete</button>}
                    {confirming && (
                        <>
                            <button type="button" class="btn btn-error btn-xs" onClick={remove}>Delete for everyone</button>
                            <button type="button" class="btn btn-ghost btn-xs" onClick={() => setConfirming(false)}>Cancel</button>
                        </>
                    )}
                </div>
            )}
        </div>
    );
}

// Spine is the corner that joins a reply to the message it quotes.
function Spine() {
    return <span aria-hidden="true" class="ml-2 mt-1.5 h-2 w-5 shrink-0 self-start rounded-tl border-l-2 border-t-2 border-base-content/30"></span>;
}

function PendingRow({ p, me, onRetry, onDiscard }) {
    return (
        <div class="mt-2 flex gap-3 rounded px-2 py-0.5 opacity-60">
            <div class="w-9 shrink-0">
                <Avatar member={me} />
            </div>
            <div class="min-w-0 flex-1">
                <div class="flex items-baseline gap-2">
                    <span class="font-semibold">{me.display_name}</span>
                    <span class="cursor-default select-none text-xs">{p.failed ? 'Not sent' : 'Sending…'}</span>
                </div>
                <Markdown text={p.text} me={me} />
                {p.failed && (
                    <div class="flex items-center gap-2 text-xs text-error">
                        <span>{p.error}</span>
                        <button type="button" class="btn btn-ghost btn-xs" onClick={onRetry}>Retry</button>
                        <button type="button" class="btn btn-ghost btn-xs" onClick={onDiscard}>Discard</button>
                    </div>
                )}
            </div>
        </div>
    );
}

// EditBox edits a message against the revision it started from. If the
// message changed meanwhile, it shows the newer text and keeps the draft.
function EditBox({ m, denID, onDone }) {
    const [text, setText] = useState(m.text);
    const [base, setBase] = useState(m);
    const [conflict, setConflict] = useState(null);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    async function save() {
        if (text === base.text) return onDone();
        setBusy(true);
        setError('');
        try {
            await api.patch(`/api/dens/${denID}/messages/${m.id}`, { revision: base.revision, text });
            onDone();
        } catch (e) {
            if (e.status === 409 && e.data?.message) {
                setConflict(e.data.message);
                setBase(e.data.message);
            } else {
                setError(e.message);
            }
        } finally {
            setBusy(false);
        }
    }
    return (
        <div class="flex flex-col gap-1 py-1">
            {conflict && (
                <div class="rounded bg-warning/20 p-2 text-sm">
                    <p class="font-medium">This message changed while you were editing. It now reads:</p>
                    <Markdown text={conflict.text} />
                    <p class="text-xs">Your draft is below; saving replaces the newer text.</p>
                </div>
            )}
            <textarea
                class="textarea w-full"
                rows={Math.min(8, text.split('\n').length + 1)}
                value={text}
                onInput={(e) => setText(e.currentTarget.value)}
                onKeyDown={(e) => {
                    if (e.key === 'Escape') onDone();
                    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
                        e.preventDefault();
                        save();
                    }
                }}
                autofocus
            ></textarea>
            {error && <p class="text-xs text-error">{error}</p>}
            <div class="flex gap-2 text-xs">
                <button type="button" class="btn btn-primary btn-xs" onClick={save} disabled={busy}>Save</button>
                <button type="button" class="btn btn-ghost btn-xs" onClick={onDone}>Cancel</button>
                <span class="cursor-default select-none self-center text-base-content/50">Enter to save, Escape to cancel</span>
            </div>
        </div>
    );
}

const MAX_TEXT = 4000;

// typingLine says who is typing, by display name.
export function typingLine(names) {
    if (names.length === 0) return '';
    if (names.length === 1) return `${names[0]} is typing…`;
    if (names.length === 2) return `${names[0]} and ${names[1]} are typing…`;
    if (names.length === 3) return `${names[0]}, ${names[1]} and ${names[2]} are typing…`;
    return 'Several people are typing…';
}

// Typing notices go out at most this often while someone types; the den
// shows them for a little longer than that.
const TYPING_EVERY = 3000;

function Composer({ placeholder, replyTo, replyAuthor, typing, onCancelReply, onSend, onEditLast, onTyping }) {
    const [text, setText] = useState('');
    const box = useRef(null);
    const lastTyping = useRef(0);
    useEffect(() => box.current?.focus(), [replyTo]);
    const length = [...text].length;
    function submit() {
        if (!text.trim() || length > MAX_TEXT) return;
        onSend(text);
        setText('');
        lastTyping.current = 0;
    }
    function input(value) {
        setText(value);
        const now = Date.now();
        if (value.trim() && now - lastTyping.current > TYPING_EVERY) {
            lastTyping.current = now;
            onTyping();
        }
    }
    const typers = typingLine(typing.map((m) => m.display_name));
    return (
        <div class="border-t border-base-300 px-4 py-2">
            {replyTo && (
                <div class="mb-1 flex cursor-default select-none items-center gap-2 text-xs text-base-content/70">
                    <span class="min-w-0 truncate">
                        Replying to <span class="font-medium">{name(replyAuthor)}</span>: <Preview text={replyTo.text} />
                    </span>
                    <button type="button" class="btn btn-ghost btn-xs" onClick={onCancelReply} aria-label="Cancel reply">✕</button>
                </div>
            )}
            <textarea
                ref={box}
                class="textarea h-auto min-h-0 w-full resize-none"
                rows={Math.min(8, text.split('\n').length)}
                placeholder={placeholder}
                value={text}
                onInput={(e) => input(e.currentTarget.value)}
                onKeyDown={(e) => {
                    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
                        e.preventDefault();
                        submit();
                    } else if (e.key === 'Escape' && replyTo) {
                        onCancelReply();
                    } else if (e.key === 'ArrowUp' && !text) {
                        e.preventDefault();
                        onEditLast();
                    }
                }}
            ></textarea>
            <div class="flex cursor-default select-none justify-between text-xs text-base-content/50">
                {typers ? <span class="truncate font-medium text-base-content/70" aria-live="polite">{typers}</span> : <span>Enter to send, Shift+Enter for a new line</span>}
                {length > MAX_TEXT - 500 && <span class={length > MAX_TEXT ? 'text-error' : ''}>{length} / {MAX_TEXT}</span>}
            </div>
        </div>
    );
}
