// A channel's message list and composer.
//
// The list holds one contiguous run of at most WINDOW messages, never the
// whole channel, and none that lie more than SCREENS screens beyond what's
// shown, so a run of photos holds far fewer than a run of text. Scrolling
// near either end loads the next page and cuts the far end, so memory
// stays flat however far back a member scrolls (see paging.js). While the
// run holds the newest message the list is attached to the live tail and
// new messages append, unless the member is reading far enough up that the
// run is full; jumping to an old message (a reply's quote) loads the page
// around it and detaches, and new messages then only update a "jump to
// present" bar.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import { Avatar } from './avatar.jsx';
import { Compare, comparable, mayGoSmaller, sendSmaller, sentLabel } from './compare.jsx';
import {
    Attachments, MAX_ATTACHMENTS, Thumb, Viewer, addPreview, attachmentKind, formatSize, isImageFile, needsPreview, thumbURL, upload,
} from './files.jsx';
import { compareIds, newNonce } from './ids.js';
import { PAGE, SCREENS, WINDOW, atTail, farIds, fromPage, mergeIn, pageSize, replayed, withDelete, withExpired, withNewer, withOlder, withUpdate } from './paging.js';
import { keptFor } from './retention.js';
import { Markdown, Preview } from './markdown.jsx';
import { Dialog } from './manage.jsx';
import { isStaff, rank } from './people.jsx';
import { LOCKED, keyChanges } from './private.js';
import { CheckPanel, KeyDivider } from './private.jsx';
import { Videos } from './youtube.jsx';

const EDGE = 600; // px from an end at which the next page loads
const READ_EVERY = 2000; // ms between read position updates
const GROUP_GAP = 5 * 60 * 1000;

// MAX_EDITORS is how many others may edit a message with its author.
const MAX_EDITORS = 20;

// FOLD is how tall a message's text shows before it folds, in px: about 16
// lines. Taller text, such as a pasted log or a wall of blank lines, shows
// its top under a fade, and opens on request. The den keeps no limit on
// lines: it can't read a DM, and a hostile sender wouldn't keep one.
const FOLD = 400;

// joinNames lists names in a sentence.
function joinNames(names) {
    return names.length < 3 ? names.join(' and ') : `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
}

let nextKey = 1;

// pendingJump is a message to show once its channel opens, which the home
// page's list of files asks for (M5): the pane that opens on its channel
// jumps there rather than to the newest.
let pendingJump = null;

// openAt has the next pane that opens on a den's channel jump to a message.
export function openAt(denID, channelID, messageID) {
    pendingJump = { denID, channelID, messageID };
}

// viewed is what the viewer shows from a message: its images, and which.
function viewed(m, file) {
    const files = (m.attachments || []).filter((f) => attachmentKind(f) === 'image');
    const index = files.findIndex((f) => f.id === file.id);
    return index < 0 ? null : { message: m.id, files, index };
}

// MessagePane shows a channel or DM. dm is the other member of a DM, and
// keys the DM's keys; typing lists who is typing here; closed, when set,
// says why nothing can be sent; limits are the den's upload limits, and
// retention the days it keeps a message, 0 for ever (M5).
export function MessagePane({ denID, channel, me, members, readPosition, role, dm, keys, typing, closed, limits, retention, onProfile, onTyping }) {
    const [list, setList] = useState({ messages: [], hasOlder: false, hasNewer: false, loaded: false });
    const [pending, setPending] = useState([]);
    const [highlight, setHighlight] = useState(null);
    const [newCount, setNewCount] = useState(0);
    const [replyTo, setReplyTo] = useState(null);
    // editors may edit the message being written, besides its author.
    const [editors, setEditors] = useState([]);
    const [editing, setEditing] = useState(null);
    const [error, setError] = useState('');
    // files are the attachments the member is getting ready to send.
    const [files, setFiles] = useState([]);
    const filesRef = useRef(files);
    filesRef.current = files;
    const [viewing, setViewing] = useState(null);
    // comparing is the key of the file whose two versions the member
    // compares (M5).
    const [comparing, setComparing] = useState(null);
    const [dragging, setDragging] = useState(false);
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
    // loads counts the loads that replace the run, so a page that lands
    // after the run it was for was replaced is dropped. caught holds, for
    // each load out, the events that came while it was.
    const loads = useRef(0);
    const caught = useRef(new Set());
    const base = `/api/dens/${denID}/channels/${channel.id}/messages`;
    // candidates could edit a message here with its author: the members
    // who can see the channel, besides this one.
    const candidates = [...members.values()]
        .filter((x) => x.id !== me.id && !x.left_at && (dm ? x.id === dm.id : !channel.staff_only || isStaff(x.role)))
        .sort((a, b) => name(a).localeCompare(name(b)));

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

    // fetchPage fetches a page, keeping the events that come while it's out
    // for replayed. A page that replaces the run counts as a new load; any
    // page is null once the run it was for was replaced.
    async function fetchPage(path, replaces) {
        const n = replaces ? ++loads.current : loads.current;
        const since = [];
        caught.current.add(since);
        try {
            const page = await api.get(path);
            return n === loads.current ? { page, since } : null;
        } finally {
            caught.current.delete(since);
        }
    }

    // far finds where the run reaches more than SCREENS screens beyond what's
    // shown, by its rows as they're laid out: those are what a growing run
    // gives up first.
    function far() {
        const box = scroller.current;
        if (!box) return { above: null, below: null };
        const view = box.getBoundingClientRect();
        const rows = [...box.querySelectorAll('[data-id]')].map((el) => {
            const r = el.getBoundingClientRect();
            return { id: el.dataset.id, top: r.top, bottom: r.bottom };
        });
        return farIds(rows, view.top, view.bottom, SCREENS * box.clientHeight);
    }

    // nextPage is how many messages the page beyond an end ("older" or
    // "newer") asks for, by how tall the rows nearest that end are.
    function nextPage(end) {
        const box = scroller.current;
        if (!box) return PAGE;
        const rows = [...box.querySelectorAll('[data-id]')];
        const near = end === 'older' ? rows.slice(0, 20) : rows.slice(-20);
        const height = near.reduce((sum, el) => sum + el.getBoundingClientRect().height, 0) / near.length;
        return pageSize(height, box.clientHeight);
    }

    async function loadNewest() {
        try {
            const got = await fetchPage(`${base}?limit=${PAGE}`, true);
            if (!got) return;
            anchor.current = { bottom: true };
            setList(replayed(fromPage(got.page), got.since));
            setNewCount(0);
            setError('');
        } catch (e) {
            setError(e.message);
        }
    }

    // loadOlder adds the page before the run, and cuts what lies far below
    // the view, measured before the page lands: the anchor keeps the view
    // where it is, so what's below stays put.
    async function loadOlder() {
        const cur = listRef.current;
        if (loading.current.older || !cur.hasOlder || !cur.messages.length) return;
        loading.current.older = true;
        try {
            const got = await fetchPage(`${base}?before=${cur.messages[0].id}&limit=${nextPage('older')}`, false);
            if (!got) return;
            const { below } = far();
            captureAnchor();
            setList((l) => replayed(withOlder(l, got.page, below), got.since));
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
            const got = await fetchPage(`${base}?after=${cur.messages[cur.messages.length - 1].id}&limit=${nextPage('newer')}`, false);
            if (!got) return;
            const { above } = far();
            captureAnchor();
            setList((l) => replayed(withNewer(l, got.page, above), got.since));
            if (!got.page.has_newer) setNewCount(0);
        } catch (e) {
            setError(e.message);
        } finally {
            loading.current.newer = false;
        }
    }

    async function jumpTo(id) {
        let next = null;
        if (!listRef.current.messages.some((m) => m.id === id)) {
            try {
                const got = await fetchPage(`${base}?around=${id}&limit=${PAGE}`, true);
                if (!got) return;
                next = replayed(fromPage(got.page), got.since);
            } catch (e) {
                setError(e.message);
                return;
            }
        }
        anchor.current = { jump: id };
        if (next) setList(next);
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

    // toggled hears that a folded message opened or folded back. The member
    // is reading it, so the list stops following the newest until it next
    // sees where the view is: otherwise it would follow the message's change
    // of size to the end.
    function toggled() {
        atBottom.current = false;
        requestAnimationFrame(checkEdges);
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
        const jump = pendingJump?.denID === denID && pendingJump.channelID === channel.id ? pendingJump.messageID : '';
        pendingJump = null;
        if (jump) jumpTo(jump);
        else loadNewest();
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
            filesRef.current.forEach((f) => f.abort?.());
        };
    }, []);

    function handleEvent(e) {
        const d = e.d;
        // A retention pass reaches every channel at once (M5).
        if (e.t === 'messages.expired' && d) return expired(e);
        if (!d || d.channel_id !== channel.id) return;
        caught.current.forEach((since) => since.push(e));
        // Once this member checks a DM's key, what it sealed opens.
        if (e.t === 'dm.key') {
            if (listRef.current.messages.some((m) => m.locked)) loadNewest();
            return;
        }
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
            const { above, below } = far();
            if (!follow && (below || l.messages.length >= WINDOW)) {
                // The member is reading further up a run that's full: rather
                // than grow it, the list lets go of the live tail, as after a
                // jump, and the bar or scrolling down brings it back.
                setList((cur) => ({ ...cur, hasNewer: true }));
                setNewCount((n) => n + 1);
                return;
            }
            anchor.current = follow ? { bottom: true } : null;
            if (!follow) setNewCount((n) => n + 1);
            setList((cur) => (follow ? atTail(cur, d, above) : { ...cur, messages: mergeIn(cur.messages, [d]) }));
            if (follow) setTimeout(markRead, 0);
        } else if (e.t === 'message.updated') {
            // A swap or a delete changes what the viewer may show (M5).
            setViewing((v) => (v && v.message === d.id ? viewed(d, v.files[v.index]) : v));
            setList((cur) => withUpdate(cur, d));
        } else if (e.t === 'message.deleted') {
            setViewing((v) => (v && v.message === d.id ? null : v));
            setList((cur) => withDelete(cur, d));
        }
    }

    // expired drops what a retention pass deleted: every message up to
    // e.d.through, with the file the viewer shows, the reply being written
    // and the edit under way, if they were among them.
    function expired(e) {
        const { through } = e.d;
        caught.current.forEach((since) => since.push(e));
        const gone = (id) => compareIds(id, through) <= 0;
        const l = listRef.current;
        setViewing((v) => (v && gone(v.message) ? null : v));
        setReplyTo((r) => (r && gone(r.id) ? null : r));
        setEditing((id) => (id && gone(id) ? null : id));
        setList((cur) => withExpired(cur, through));
        // A run that lost every message it held starts again from the newest.
        if (l.hasNewer && l.messages.length && l.messages.every((m) => gone(m.id))) loadNewest();
    }

    // addFiles starts uploading files the member picked, dropped or pasted,
    // up to what a message holds. Uploads go ahead while they write. A
    // photo goes as a smaller copy unless the member turned copies off, and
    // one over the den's limit may still go as its copy (M5).
    function addFiles(list) {
        const picked = [...list];
        const room = MAX_ATTACHMENTS - filesRef.current.length;
        if (picked.length > room) setError(`A message holds at most ${MAX_ATTACHMENTS} files.`);
        const added = picked.slice(0, Math.max(0, room)).map((file) => {
            const entry = { key: nextKey++, file, name: file.name || 'pasted image.png', size: file.size, progress: 0 };
            if (limits?.file_size && file.size > limits.file_size && !mayGoSmaller(file)) {
                entry.error = `This file is larger than this den allows (${formatSize(limits.file_size)}).`;
            }
            return entry;
        });
        setFiles((cur) => [...cur, ...added]);
        const send = sendSmaller() ? 'smaller' : 'full';
        for (const entry of added) {
            if (entry.error) continue;
            const up = upload(denID, channel.id, entry.file, entry.name, (progress) => updateFile(entry.key, { progress }), '', send);
            entry.abort = up.abort;
            up.done.then(
                async (result) => {
                    // The message waits for the preview, which the den takes
                    // only while the upload waits to be sent.
                    if (needsPreview(result)) result = await addPreview(denID, result);
                    updateFile(entry.key, { result, progress: 1 });
                },
                (e) => updateFile(entry.key, { error: e.message }),
            );
        }
    }

    function updateFile(key, changes) {
        setFiles((cur) => cur.map((f) => (f.key === key ? { ...f, ...changes } : f)));
    }

    // switchVersions sends photos on the message being written as the
    // other version, to, which the local service uploads, dropping the one
    // waiting (M5). The message waits for it, as for an upload. A photo
    // taken off meanwhile drops what it switched to.
    function switchVersions(entries, to) {
        for (const f of entries) {
            const from = f.result;
            updateFile(f.key, { switching: true, switchError: '' });
            api.post(`/api/dens/${denID}/uploads/${from.id}/switch`, { to }).then(
                (result) => {
                    if (!filesRef.current.some((x) => x.key === f.key)) {
                        api.del(`/api/dens/${denID}/uploads/${result.id}`).catch(() => {});
                        return;
                    }
                    updateFile(f.key, { result, switching: false });
                },
                (e) => updateFile(f.key, { switching: false, switchError: e.message }),
            );
        }
    }

    // removeFile takes a file off the message being written: an upload in
    // progress stops, and one already up is dropped, so its space is free at
    // once (M5).
    function removeFile(key) {
        const f = filesRef.current.find((x) => x.key === key);
        f?.abort?.();
        if (f?.result) api.del(`/api/dens/${denID}/uploads/${f.result.id}`).catch(() => {});
        setFiles((cur) => cur.filter((x) => x.key !== key));
    }

    async function send(text, pendingEntry) {
        if (listRef.current.hasNewer) loadNewest();
        const entry = pendingEntry || {
            nonce: newNonce(), text, reply_to: replyTo?.id || '', attachments: files.map((f) => f.result), editors, failed: false,
        };
        setPending((p) => [...p.filter((x) => x.nonce !== entry.nonce), { ...entry, failed: false }]);
        if (!pendingEntry) {
            setReplyTo(null);
            setFiles([]);
            setEditors([]);
        }
        anchor.current = { bottom: true };
        try {
            const m = await api.post(base, {
                nonce: entry.nonce, text: entry.text, reply_to: entry.reply_to || undefined,
                attachments: entry.attachments.length ? entry.attachments.map((f) => f.id) : undefined,
                editors: entry.editors.length ? entry.editors : undefined,
            });
            setPending((p) => p.filter((x) => x.nonce !== entry.nonce));
            if (compareIds(m.id, readUpTo.current) > 0) readUpTo.current = m.id;
            if (!listRef.current.hasNewer) {
                anchor.current = { bottom: true };
                const { above } = far();
                setList((cur) => atTail(cur, m, above));
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

    // A DM takes messages once this member checked its live key.
    const live = keys?.find((k) => k.channel_id === channel.id && !k.retired);
    const canSend = channel.kind !== 'dm' || (live?.stage === 'revealed' && (live.checks || []).some((c) => c.member_id === me.id));
    const byId = new Map(list.messages.map((m) => [m.id, m]));
    const rows = [];
    let prev = null;
    // In a DM, dividers mark where its keys changed.
    const changes = channel.kind === 'dm' && keys ? keyChanges(list.messages, keys, (id) => (id === me.id ? 'You' : name(members.get(id)))) : new Map();
    for (const m of list.messages) {
        const day = new Date(m.created_at).toDateString();
        if (!prev || new Date(prev.created_at).toDateString() !== day) {
            rows.push(<DayDivider key={`day-${m.id}`} time={m.created_at} />);
        }
        if (changes.has(m.id)) rows.push(<KeyDivider key={`keys-${m.id}`} text={changes.get(m.id)} />);
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
                canEdit={!m.locked && (m.author_id === me.id || (m.editors || []).includes(me.id))}
                canDelete={m.author_id === me.id || rank(role) > rank(members.get(m.author_id)?.role)}
                editorNames={(m.editors || []).map((id) => name(members.get(id)))}
                candidates={candidates}
                onUpdated={(u) => setList((cur) => withUpdate(cur, u))}
                onProfile={onProfile}
                onReply={() => setReplyTo(m)}
                onEdit={() => setEditing(m.id)}
                onEditDone={() => setEditing(null)}
                onJump={jumpTo}
                onView={(f) => setViewing(viewed(m, f))}
                onToggle={toggled}
                denID={denID}
                channelID={channel.id}
            />,
        );
        prev = m;
    }

    // Files dragged over the pane attach to the message being written.
    const withFiles = (e) => !closed && [...(e.dataTransfer?.types || [])].includes('Files');
    return (
        <div
            class="relative flex min-h-0 flex-1 flex-col"
            onDragOver={(e) => {
                if (!withFiles(e)) return;
                e.preventDefault();
                setDragging(true);
            }}
            onDragLeave={(e) => !e.currentTarget.contains(e.relatedTarget) && setDragging(false)}
            onDrop={(e) => {
                if (!withFiles(e)) return;
                e.preventDefault();
                setDragging(false);
                addFiles(e.dataTransfer.files);
            }}
        >
            {dragging && (
                <div class="pointer-events-none absolute inset-2 z-10 flex items-center justify-center rounded-lg border-2 border-dashed border-primary bg-base-100/80 text-lg font-semibold">
                    Drop to attach
                </div>
            )}
            {/* The room at the bottom is where who's typing shows, over the list. */}
            <div ref={scroller} class="min-h-0 flex-1 overflow-y-auto px-4 pb-6 pt-2" onScroll={checkEdges}>
                <div ref={content}>
                    {list.loaded && !list.hasOlder && (
                        <div class="cursor-default select-none py-6 text-center text-sm text-base-content/60">
                            {dm ? (
                                <>
                                    <p>
                                        {retention
                                            ? `Messages with ${dm.display_name} are deleted after ${keptFor(retention)}.`
                                            : `This is the start of your conversation with ${dm.display_name}.`}
                                    </p>
                                    <p class="text-xs">
                                        It's end-to-end encrypted: only the two of you can read it, not the den's owner. The den still
                                        sees who talks to whom, and when.
                                    </p>
                                </>
                            ) : (
                                <>
                                    <p>
                                        {retention
                                            ? `Messages in #${channel.name} are deleted after ${keptFor(retention)}.`
                                            : `This is the start of #${channel.name}.`}
                                    </p>
                                    <p class="text-xs">The den's owner can read what's posted here.</p>
                                </>
                            )}
                        </div>
                    )}
                    {list.hasOlder && <div class="py-3 text-center"><span class="loading loading-dots loading-sm"></span></div>}
                    {!list.loaded && !error && <span class="loading loading-spinner"></span>}
                    {rows}
                    {!list.hasNewer && pending.map((p) => <PendingRow key={p.nonce} p={p} me={me} onToggle={toggled} onRetry={() => send(p.text, p)} onDiscard={() => setPending((ps) => ps.filter((x) => x.nonce !== p.nonce))} />)}
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
            {!closed && channel.kind === 'dm' && (
                <CheckPanel denID={denID} channel={channel} dm={dm} keyID={live?.id || ''} />
            )}
            {closed ? (
                <div class="cursor-default select-none border-t border-base-300 px-4 py-3 text-sm text-base-content/60">{closed}</div>
            ) : !canSend ? null : (
                <Composer
                    denID={denID}
                    placeholder={dm ? `Message @${dm.username}` : `Message #${channel.name}`}
                    replyTo={replyTo}
                    replyAuthor={replyTo ? members.get(replyTo.author_id) : null}
                    typing={typing}
                    files={files}
                    onAddFiles={addFiles}
                    onRemoveFile={removeFile}
                    onCompare={setComparing}
                    onCancelReply={() => setReplyTo(null)}
                    candidates={candidates}
                    editors={editors}
                    onEditors={setEditors}
                    onSend={(text) => send(text)}
                    onEditLast={editLast}
                    onTyping={onTyping}
                />
            )}
            {viewing && (
                <Viewer denID={denID} files={viewing.files} index={viewing.index} onClose={() => setViewing(null)}
                    onIndex={(index) => setViewing((v) => v && { ...v, index })} />
            )}
            {comparing !== null && comparable(files).some((f) => f.key === comparing) && (
                <Compare denID={denID} files={comparable(files)} index={comparable(files).findIndex((f) => f.key === comparing)} limits={limits}
                    onIndex={(i) => setComparing(comparable(filesRef.current)[i]?.key ?? null)} onSwitch={switchVersions}
                    onClose={() => setComparing(null)} />
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

// Quoted shows the first line of a message a reply quotes, that it has
// only files, or that it can't be opened.
function Quoted({ text, locked, me }) {
    if (locked) return <span class="italic">A message this device can't open</span>;
    return text.trim() ? <Preview text={text} me={me} /> : <span class="italic">Attachment</span>;
}

function MessageRow({ m, compact, author, replied, repliedAuthor, me, highlighted, editing, canEdit, canDelete, editorNames, candidates, onUpdated, onReply, onEdit, onEditDone, onJump, onView, onProfile, onToggle, denID, channelID }) {
    const [confirming, setConfirming] = useState(false);
    const [error, setError] = useState('');
    async function tick(n, checked, text) {
        setError('');
        try {
            onUpdated(await api.post(`/api/dens/${denID}/messages/${m.id}/tasks/${n}?channel=${channelID}`, { checked, text }));
        } catch (e) {
            // A list that changed meanwhile shows as it is now.
            if (e.status === 409 && e.data?.message) onUpdated(e.data.message);
            else setError(e.message);
            throw e;
        }
    }
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
                    <span class="truncate"><Quoted text={replied.text} locked={replied.locked} me={me} /></span>
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
                    {m.locked ? (
                        <p class="cursor-default select-none text-sm italic text-base-content/60">{LOCKED[m.locked] || LOCKED.broken}</p>
                    ) : editing ? (
                        <EditBox m={m} denID={denID} channelID={channelID} mine={m.author_id === me.id} candidates={candidates} onDone={onEditDone} />
                    ) : (
                        (m.text.trim() || m.edited_at) && (
                            <Fold onToggle={onToggle}>
                                <div class="wrap-break-word">
                                    {m.text.trim() && <Markdown text={m.text} me={me} onTask={canEdit ? tick : undefined} />}
                                    {m.edited_at ? <span class="cursor-default select-none text-xs text-base-content/50" title={new Date(m.edited_at).toLocaleString()}> (edited)</span> : null}
                                    {editorNames.length > 0 && (
                                        <span class="cursor-default select-none text-xs text-base-content/50" title={`${joinNames(editorNames)} can edit this too`}> (shared)</span>
                                    )}
                                </div>
                            </Fold>
                        )
                    )}
                    {m.attachments?.length > 0 && <Attachments denID={denID} files={m.attachments} onOpen={onView} />}
                    {!m.locked && !editing && m.text && <Videos text={m.text} />}
                    {error && <p class="text-xs text-error">{error}</p>}
                </div>
            </div>
            {!editing && (
                <div class="absolute -top-3 right-2 hidden gap-1 rounded bg-base-100 shadow group-hover:flex">
                    <button type="button" class="btn btn-ghost btn-xs" onClick={onReply}>Reply</button>
                    {canEdit && <button type="button" class="btn btn-ghost btn-xs" onClick={onEdit}>Edit</button>}
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

// Fold holds a message's text to FOLD px when it's taller, under a fade
// with Show more. The text is held from the first paint, so folding never
// moves the list; only the button comes after the text is measured, over
// the fade. onToggle hears that the member opened it or folded it back.
function Fold({ onToggle, children }) {
    const box = useRef(null);
    const content = useRef(null);
    const folding = useRef(false);
    const [over, setOver] = useState(false);
    const [open, setOpen] = useState(false);
    // Folding back from the end of a long message would leave the view far
    // below it, so the message comes back into view before the next frame.
    useLayoutEffect(() => {
        if (!folding.current) return;
        folding.current = false;
        box.current?.scrollIntoView({ block: 'nearest' });
    }, [open]);
    useLayoutEffect(() => {
        const el = content.current;
        const measure = () => setOver(el.offsetHeight > FOLD);
        measure();
        if (typeof ResizeObserver === 'undefined') return undefined;
        // An edit or a narrower window can take the text over the line, or
        // back under it.
        const observer = new ResizeObserver(measure);
        observer.observe(el);
        return () => observer.disconnect();
    }, []);
    const folded = over && !open;
    return (
        <div ref={box} class="relative">
            <div class={`${open ? '' : 'max-h-100 overflow-hidden'} ${folded ? '[mask-image:linear-gradient(to_bottom,black_70%,transparent)]' : ''}`}>
                <div ref={content}>{children}</div>
            </div>
            {folded && (
                <button
                    type="button"
                    class="btn btn-xs absolute bottom-1 left-0 shadow-sm"
                    aria-expanded="false"
                    onClick={() => {
                        onToggle?.();
                        setOpen(true);
                    }}
                >
                    Show more
                </button>
            )}
            {over && open && (
                <button
                    type="button"
                    class="btn btn-xs mt-1"
                    aria-expanded="true"
                    onClick={() => {
                        onToggle?.();
                        folding.current = true;
                        setOpen(false);
                    }}
                >
                    Show less
                </button>
            )}
        </div>
    );
}

// Spine is the corner that joins a reply to the message it quotes.
function Spine() {
    return <span aria-hidden="true" class="ml-2 mt-1.5 h-2 w-5 shrink-0 self-start rounded-tl border-l-2 border-t-2 border-base-content/30"></span>;
}

function PendingRow({ p, me, onToggle, onRetry, onDiscard }) {
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
                {p.text.trim() && (
                    <Fold onToggle={onToggle}>
                        <Markdown text={p.text} me={me} />
                    </Fold>
                )}
                {p.attachments?.length > 0 && (
                    <ul class="text-xs text-base-content/70">
                        {p.attachments.map((f) => <li key={f.id} class="truncate">{f.name} · {formatSize(f.size)}</li>)}
                    </ul>
                )}
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
// Its author also changes who else may edit it.
function EditBox({ m, denID, channelID, mine, candidates, onDone }) {
    const [text, setText] = useState(m.text);
    const [editors, setEditors] = useState(m.editors || []);
    const [sharing, setSharing] = useState(false);
    const [base, setBase] = useState(m);
    const [conflict, setConflict] = useState(null);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const was = base.editors || [];
    const shared = mine && (editors.length !== was.length || editors.some((id) => !was.includes(id)));
    const names = editors.map((id) => name(candidates.find((c) => c.id === id)));
    async function save() {
        if (text === base.text && !shared) return onDone();
        setBusy(true);
        setError('');
        try {
            await api.patch(`/api/dens/${denID}/messages/${m.id}?channel=${channelID}`, { revision: base.revision, text, editors: shared ? editors : undefined });
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
            {mine && (sharing ? (
                <div class="flex flex-col gap-1 rounded border border-base-300 p-2">
                    <span class="text-xs text-base-content/70">Who else can edit this message and tick its boxes?</span>
                    <EditorList candidates={candidates} selected={editors} onChange={setEditors} />
                </div>
            ) : (
                <div class="flex items-center gap-2 text-xs text-base-content/70">
                    <span>{editors.length ? `${joinNames(names)} can edit this too.` : 'Only you can edit this.'}</span>
                    <button type="button" class="btn btn-ghost btn-xs" onClick={() => setSharing(true)}>Change</button>
                </div>
            ))}
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

// EditorList picks, among the members who could, who else may edit a
// message.
function EditorList({ candidates, selected, onChange }) {
    const [filter, setFilter] = useState('');
    const wanted = filter.trim().toLowerCase();
    const shown = candidates.filter((c) => !wanted || `${c.display_name} ${c.username}`.toLowerCase().includes(wanted));
    const full = selected.length >= MAX_EDITORS;
    if (candidates.length === 0) return <p class="text-xs text-base-content/60">Nobody else here can see this channel.</p>;
    return (
        <div class="flex flex-col gap-1">
            {candidates.length > 8 && (
                <input class="input input-sm w-full" placeholder="Find someone" value={filter} onInput={(e) => setFilter(e.currentTarget.value)} />
            )}
            <ul class="flex max-h-32 flex-wrap gap-x-4 gap-y-1 overflow-y-auto" aria-label="Members who can edit">
                {shown.map((c) => {
                    const on = selected.includes(c.id);
                    return (
                        <li key={c.id}>
                            <label class="flex cursor-pointer items-center gap-1.5 text-sm">
                                <input type="checkbox" class="checkbox checkbox-xs" checked={on} disabled={!on && full}
                                    onChange={() => onChange(on ? selected.filter((id) => id !== c.id) : [...selected, c.id])} />
                                {c.display_name}
                            </label>
                        </li>
                    );
                })}
            </ul>
            {full && <p class="text-xs text-base-content/60">At most {MAX_EDITORS} others can edit a message.</p>}
        </div>
    );
}

function Composer({ denID, placeholder, replyTo, replyAuthor, typing, files, candidates, editors, onEditors, onAddFiles, onRemoveFile, onCompare, onCancelReply, onSend, onEditLast, onTyping }) {
    const [text, setText] = useState('');
    const [sharing, setSharing] = useState(false);
    // The + menu, and the guide to writing a message (M3.3).
    const [adding, setAdding] = useState(false);
    const [guide, setGuide] = useState(false);
    useEffect(() => {
        if (!adding) return undefined;
        const away = (e) => {
            if (!e.target.closest?.('[data-compose-menu]')) setAdding(false);
        };
        const escape = (e) => {
            if (e.key === 'Escape') setAdding(false);
        };
        document.addEventListener('pointerdown', away);
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('pointerdown', away);
            document.removeEventListener('keydown', escape);
        };
    }, [adding]);
    const box = useRef(null);
    const picker = useRef(null);
    const lastTyping = useRef(0);
    useEffect(() => box.current?.focus(), [replyTo]);
    // Files added however they came, dropped, pasted or picked, leave the
    // box ready for Enter.
    const fileCount = useRef(files.length);
    useEffect(() => {
        if (files.length > fileCount.current) box.current?.focus();
        fileCount.current = files.length;
    }, [files.length]);
    const length = [...text].length;
    const uploading = files.some((f) => (!f.result && !f.error) || f.switching);
    const failed = files.some((f) => f.error);
    function submit() {
        if ((!text.trim() && files.length === 0) || length > MAX_TEXT || uploading || failed) return;
        onSend(text);
        setText('');
        setSharing(false);
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
    // At 73px, a single line's composer lines up with the member's panel
    // beside it, at the foot of the channel list.
    return (
        <div class="relative border-t border-base-300 bg-bar px-4 py-3">
            {/* Who's typing, how the files are doing and the length float just
                above the composer, so it never jumps as they come and go. The
                line is always here, empty or not, for screen readers. */}
            <div class="pointer-events-none absolute inset-x-4 bottom-full flex cursor-default select-none items-end justify-between gap-2 pb-0.5 text-xs text-base-content/50">
                {typers ? (
                    <span class="truncate rounded bg-base-100 px-1 font-medium text-base-content/70" aria-live="polite">{typers}</span>
                ) : uploading ? (
                    <span class="rounded bg-base-100 px-1" aria-live="polite">Uploading… the message sends once the files are up.</span>
                ) : failed ? (
                    <span class="rounded bg-base-100 px-1 text-error" aria-live="polite">Remove the files that couldn't be sent to send the rest.</span>
                ) : (
                    <span aria-live="polite"></span>
                )}
                {length > MAX_TEXT - 500 && <span class={`rounded bg-base-100 px-1 ${length > MAX_TEXT ? 'text-error' : ''}`}>{length} / {MAX_TEXT}</span>}
            </div>
            {replyTo && (
                <div class="mb-1 flex cursor-default select-none items-center gap-2 text-xs text-base-content/70">
                    <span class="min-w-0 truncate">
                        Replying to <span class="font-medium">{name(replyAuthor)}</span>: <Quoted text={replyTo.text} locked={replyTo.locked} />
                    </span>
                    <button type="button" class="btn btn-ghost btn-xs" onClick={onCancelReply} aria-label="Cancel reply">✕</button>
                </div>
            )}
            {files.length > 0 && (
                <ul class="mb-2 flex flex-wrap gap-2" aria-label="Files to send">
                    {files.map((f) => <FileChip key={f.key} denID={denID} f={f} onRemove={() => onRemoveFile(f.key)} onCompare={() => onCompare(f.key)} />)}
                </ul>
            )}
            {sharing ? (
                <div class="mb-2 flex flex-col gap-1 rounded border border-base-300 p-2">
                    <div class="flex items-center justify-between gap-2 text-xs text-base-content/70">
                        <span>Who else can edit this message? They can also tick its boxes: lines that start with [ ].</span>
                        <button type="button" class="btn btn-ghost btn-xs" onClick={() => setSharing(false)}>Done</button>
                    </div>
                    <EditorList candidates={candidates} selected={editors} onChange={onEditors} />
                </div>
            ) : editors.length > 0 && (
                <div class="mb-1 flex cursor-default select-none items-center gap-2 text-xs text-base-content/70">
                    <span class="min-w-0 truncate">{joinNames(editors.map((id) => name(candidates.find((c) => c.id === id))))} can edit this too</span>
                    <button type="button" class="btn btn-ghost btn-xs" onClick={() => setSharing(true)}>Change</button>
                </div>
            )}
            <div class="flex items-end gap-2">
                <div class="relative" data-compose-menu>
                    <button type="button" class={`btn btn-ghost btn-field btn-square h-12 w-12 ${adding ? 'btn-active' : ''}`} aria-label="Add to this message"
                        title="Add to this message" aria-haspopup="menu" aria-expanded={adding} onClick={() => setAdding(!adding)}>
                        <svg viewBox="0 0 16 16" class="h-5 w-5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true">
                            <path d="M8 3v10M3 8h10" />
                        </svg>
                    </button>
                    {adding && (
                        <ul role="menu" class="menu absolute bottom-full left-0 z-20 mb-1 w-56 rounded-box bg-base-100 p-1 shadow">
                            <li role="none">
                                <button type="button" role="menuitem" disabled={files.length >= MAX_ATTACHMENTS}
                                    onClick={() => { setAdding(false); picker.current?.click(); }}>
                                    <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                                        <path d="M13.5 7.5l-5.8 5.8a3.5 3.5 0 01-5-5l6-6a2.3 2.3 0 013.3 3.3l-6 6a1.2 1.2 0 01-1.7-1.7l5.5-5.5" />
                                    </svg>
                                    Attach files
                                </button>
                            </li>
                            <li role="none">
                                <button type="button" role="menuitem" class={editors.length ? 'text-primary' : ''}
                                    onClick={() => { setAdding(false); setSharing(true); }}>
                                    <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                                        <circle cx="6" cy="5" r="2.5" />
                                        <path d="M1.5 14c0-2.5 2-4.5 4.5-4.5s4.5 2 4.5 4.5" />
                                        <circle cx="11.5" cy="5.5" r="2" />
                                        <path d="M11 9.6c2 .2 3.5 1.9 3.5 4" />
                                    </svg>
                                    Let others edit
                                </button>
                            </li>
                        </ul>
                    )}
                </div>
                <input ref={picker} type="file" multiple class="hidden" onChange={(e) => {
                    onAddFiles(e.currentTarget.files);
                    e.currentTarget.value = '';
                }} />
                <div class="relative min-w-0 flex-1">
                    <textarea
                        ref={box}
                        class="textarea h-auto min-h-12 w-full resize-none bg-transparent py-3 pr-10"
                        rows={Math.min(8, text.split('\n').length)}
                        placeholder={placeholder}
                        value={text}
                        onPaste={(e) => {
                            if (e.clipboardData?.files?.length) {
                                e.preventDefault();
                                onAddFiles(e.clipboardData.files);
                            }
                        }}
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
                    {/* The guide to writing sits in the box's corner, by the last line. */}
                    <button type="button"
                        class="absolute bottom-4 right-3 flex h-4 w-4 items-center justify-center rounded-full border border-current text-[10px] leading-none text-base-content/50 hover:text-base-content"
                        aria-label="How to write a message" title="How to write a message" onClick={() => setGuide(true)}>
                        ?
                    </button>
                </div>
            </div>
            {guide && <WritingGuide onClose={() => setGuide(false)} />}
        </div>
    );
}

// WritingGuide explains how a message is written: sending, the markdown
// subset with each example rendered as the list shows it, mentions, links,
// task lines, editing together and files.
function WritingGuide({ onClose }) {
    useEffect(() => {
        const escape = (e) => e.key === 'Escape' && onClose();
        document.addEventListener('keydown', escape);
        return () => document.removeEventListener('keydown', escape);
    }, [onClose]);
    const kbd = (k) => <kbd class="kbd kbd-xs">{k}</kbd>;
    return (
        <Dialog title="Writing a message" onClose={onClose}>
            <div class="flex flex-col gap-4 text-sm">
                <section class="flex flex-col gap-1">
                    <h3 class="font-semibold">Sending</h3>
                    <p>{kbd('Enter')} sends. {kbd('Shift')}+{kbd('Enter')} starts a new line.</p>
                    <p>In an empty box, {kbd('↑')} edits your last message, and {kbd('Esc')} drops a reply you started.</p>
                    <p>A message holds up to 4,000 characters. One taller than about 16 lines shows folded, and readers open the rest.</p>
                </section>
                <section class="flex flex-col gap-1">
                    <h3 class="font-semibold">Formatting</h3>
                    <table class="table table-sm">
                        <tbody>
                            {GUIDE_FORMATS.map(([source, note]) => (
                                <tr key={source}>
                                    <td class="whitespace-pre-wrap font-mono text-xs">{source}</td>
                                    <td><Markdown text={source} />{note && <p class="text-xs text-base-content/60">{note}</p>}</td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </section>
                <section class="flex flex-col gap-1">
                    <h3 class="font-semibold">Mentions and links</h3>
                    <p>@ and a username lets them know, if they can see the channel. Inside code or a link it's only text.</p>
                    <p>
                        A link shows where it goes, as you paste it; there's no way to give one a label. When it's sent, Dens takes its tracking
                        out, and writes YouTube, Reddit, X and Amazon links in a short form.
                    </p>
                </section>
                <section class="flex flex-col gap-1">
                    <h3 class="font-semibold">Lists to tick</h3>
                    <p>
                        A line that starts with [ ] or [x] is a box to tick. Anyone who may edit the message can tick its boxes, and two people
                        ticking at once both keep their ticks.
                    </p>
                </section>
                <section class="flex flex-col gap-1">
                    <h3 class="font-semibold">With the + button</h3>
                    <p>
                        Attach files: up to {MAX_ATTACHMENTS} to a message, also by pasting them or dropping them on the messages. Photos lose
                        their location and camera details before they leave this computer.
                    </p>
                    <p>Let others edit: up to {MAX_EDITORS} people who can see the channel can edit the message too, but not delete it.</p>
                </section>
            </div>
        </Dialog>
    );
}

const GUIDE_FORMATS = [
    ['**bold**', ''],
    ['*italic* or _italic_', ''],
    ['~~struck through~~', ''],
    ['||spoiler||', 'Hidden until the reader clicks it.'],
    ['`code`', ''],
    ['```\ncode on\nseveral lines\n```', ''],
    ['> a quote', 'A line that starts with >.'],
];

// MetadataRemoved marks a file whose metadata came out before it left this
// computer, and says what that can be on hover or focus.
function MetadataRemoved({ id }) {
    return (
        <span class="tooltip">
            <span id={id} role="tooltip" class="tooltip-content rounded-lg px-3 py-2 text-left">
                Metadata was removed from this file. That can include where and when it was made, and the phone or camera that made it.
            </span>
            <span tabIndex={0} role="img" aria-label="Metadata removed" aria-describedby={id} class="flex cursor-help text-success">
                <svg viewBox="0 0 16 16" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
                    stroke-linejoin="round" aria-hidden="true">
                    <circle cx="8" cy="8" r="6.5" />
                    <path d="M5.2 8.3l1.9 1.9 3.8-4.1" />
                </svg>
            </span>
        </span>
    );
}

// FileChip is a file waiting to go with the message: its preview or icon,
// how far its upload is, the size it will send, whether its metadata came
// out, and whether a photo browsers can't show was turned into one they
// can, which then shows the preview the upload has. A photo with two
// versions says which one it goes as, and its size opens the comparison
// (M5).
function FileChip({ denID, f, onRemove, onCompare }) {
    const done = !!f.result;
    const name = f.result?.name || f.name;
    const versions = f.result?.versions;
    // Once the file is with the local service, it may still be making a
    // smaller copy, or uploading, or switching versions, with nothing to
    // count.
    const busy = !done ? f.progress >= 1 : f.switching;
    return (
        <li class={`flex w-60 max-w-full items-center gap-2 rounded border bg-base-200 p-1.5 ${f.error ? 'border-error' : 'border-base-300'}`}>
            {isImageFile(f.file) ? (
                <Thumb file={f.file} size={40} />
            ) : f.result?.thumb ? (
                <img src={thumbURL(denID, f.result.id)} alt="" class="h-10 w-10 shrink-0 rounded bg-base-300 object-cover" draggable={false} />
            ) : (
                <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded bg-base-300 text-xs">FILE</span>
            )}
            <div class="min-w-0 flex-1 text-xs">
                <p class="truncate font-medium" title={name}>{name}</p>
                {f.error ? (
                    <p class="text-error" role="alert">{f.error}</p>
                ) : done ? (
                    <>
                        <p class="flex items-center gap-1 text-base-content/60">
                            {versions ? (
                                <button type="button" class="link link-hover cursor-pointer whitespace-nowrap" onClick={onCompare}
                                    title={`Compare it with the ${versions.sent === 'smaller' ? 'full size' : 'smaller copy'}`}>
                                    {formatSize(f.result.size)} · {sentLabel(versions)}
                                </button>
                            ) : formatSize(f.result.size)}
                            {f.result.stripped && <MetadataRemoved id={`metadata-${f.key}`} />}
                        </p>
                        {versions?.sent === 'smaller' && !versions.full.fits && (
                            <p class="text-base-content/60">Its full size is over this den's limit</p>
                        )}
                        {f.result.converted && (
                            <p class="text-base-content/60">Sent as {f.result.type === 'image/png' ? 'PNG' : 'JPEG'}, which every browser shows</p>
                        )}
                        {f.switchError && <p class="text-error" role="alert">{f.switchError}</p>}
                        {busy && <progress class="progress progress-primary h-1.5 w-full"></progress>}
                    </>
                ) : busy ? (
                    <progress class="progress progress-primary h-1.5 w-full"></progress>
                ) : (
                    <progress class="progress progress-primary h-1.5 w-full" value={Math.round(f.progress * 100)} max="100"></progress>
                )}
            </div>
            <button type="button" class="btn btn-ghost btn-xs btn-square" onClick={onRemove} aria-label={`Remove ${f.name}`}>✕</button>
        </li>
    );
}
