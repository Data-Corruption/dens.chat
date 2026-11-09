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

import { useContext, useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { HoverRow } from './animations.js';
import { onEvent } from './events.js';
import { Avatar } from './avatar.jsx';
import { backgroundStyle } from './background.js';
import { copyText } from './clipboard.js';
import { ErrorText, PLAIN, Tip, dayTime, useAction, useBackground } from './components.jsx';
import { Compare, comparable, mayGoSmaller, progressKey, progressLabel, sendSmaller, sentLabel } from './compare.jsx';
import { EmojiContext } from './denemoji.jsx';
import { DENSITY_COMPACT, useDensity } from './density.js';
import { useWidthClass } from './width.js';
import { completeEmoji, completeMention, completionAt, denKey, emojiList, inTone, keyOf, loadEmoji, noteSent, noteUses, skinTone, unicodeKey } from './emoji.js';
import { Completions, EmojiPicker } from './emoji.jsx';
import {
    Attachments, MAX_ATTACHMENTS, Thumb, Viewer, addPreview, attachmentKind, formatSize, isImageFile, needsPreview, thumbURL, upload,
} from './files.jsx';
import { compareIds, newNonce } from './ids.js';
import { linkTo } from './links.js';
import { OFFLINE_NOTE } from './offline.js';
import {
    PAGE, REACTIONS, SCREENS, WINDOW, atTail, farIds, fromPage, mergeIn, pageSize, replayed, withDelete, withExpired, withNewer, withOlder, withUpdate,
} from './paging.js';
import { reactionOf, room, withReaction } from './reactions.js';
import { QuickReactions, ReactionPicker, Reactions, RemoveReactions, useChips } from './reactions.jsx';
import { keptFor } from './retention.js';
import { Markdown, Preview, sends } from './markdown.jsx';
import { Dialog } from './manage.jsx';
import { ownsMenu, rightClick, usePlaced } from './menus.jsx';
import { useNarrow } from './nav.js';
import { RoleMark, isStaff, rank } from './people.jsx';
import { POLLS, draftProblem, newDraft, pollRequest, withPoll } from './polls.js';
import { Poll, PollEditor } from './polls.jsx';
import { LOCKED, dueBy, isPrivate, keyChanges, nextDue, placeAt } from './private.js';
import { DMGate, KeyDivider, KeysChanged } from './private.jsx';
import { GroupGate, GroupLine } from './groups.jsx';
import { ExpiryLine } from './disappearing.jsx';
import { Sheet, SheetRow } from './sheet.jsx';
import { createPress, useCoarse } from './touch.js';
import { SmileyIcon, TimerIcon } from './icons.jsx';
import { setFoot } from './voice.jsx';
import { Videos } from './youtube.jsx';
import { EventCards } from './denevents.jsx';

const EDGE = 600; // px from an end at which the next page loads
const READ_EVERY = 2000; // ms between read position updates
const GROUP_GAP = 5 * 60 * 1000;
// DUE_WAIT is the longest the list waits to look again for messages whose
// time to disappear came (M16.4), well within what a timer holds.
const DUE_WAIT = 24 * 60 * 60 * 1000;

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

// pendingJump is a message to show once its channel opens, which the list
// of a member's files asks for (M5), and a link to a message (M11): the
// pane that opens on its channel jumps there rather than to the newest.
let pendingJump = null;
// jumpers are the panes open, each of which jumps to a message of its own
// channel where it is.
const jumpers = new Set();

// openAt has the pane showing a den's channel jump to a message, or the
// next one that opens on it.
export function openAt(denID, channelID, messageID) {
    for (const jump of jumpers) {
        if (jump(denID, channelID, messageID)) return;
    }
    pendingJump = { denID, channelID, messageID };
}

// continues says whether a message shows under the name of the one before
// it, as a run's later message: by the same author, on the same day,
// within GROUP_GAP and not a reply; in Compact never, as each message
// shows its own name (M17.2).
export function continues(prev, m, line) {
    return !line && !!prev && prev.author_id === m.author_id && !m.reply_to && m.created_at - prev.created_at < GROUP_GAP &&
        new Date(prev.created_at).toDateString() === new Date(m.created_at).toDateString();
}

// viewed is what the viewer shows from a message: its images, and which,
// but for spoilers still covered (M17.2).
export function viewed(m, file, covered = new Set()) {
    const files = (m.attachments || []).filter((f) => attachmentKind(f) === 'image' && !covered.has(f.id));
    const index = files.findIndex((f) => f.id === file.id);
    return index < 0 ? null : { message: m.id, files, index };
}

// dividerAt is where a channel's New divider goes, as the channel opens:
// after the read position when messages came after it, and nowhere when
// none did, so messages that arrive while it's open get none (M11).
export function dividerAt(readPosition, lastMessage) {
    return readPosition && compareIds(lastMessage, readPosition) > 0 ? readPosition : '';
}

// MessagePane shows a channel or DM. dm is the other member of a DM, and
// keys the DM's keys; typing lists who is typing here; closed, when set,
// says why nothing can be sent; limits are the den's upload limits, and
// retention the days it keeps a message, 0 for ever (M5). lastMessage is
// the channel's newest, as its read state has it, and denURL the den's
// address, which links to its messages start with (M11).
export function MessagePane({ denID, denURL, channel, me, members, readPosition, lastMessage, role, dm, group, groupTitle, keys, gate = '', changes, identities,
    typing, closed, limits, retention, offline, spoilers, onProfile, onCompare, onTyping }) {
    const [list, setList] = useState({ messages: [], hasOlder: false, hasNewer: false, loaded: false });
    const [pending, setPending] = useState([]);
    const [highlight, setHighlight] = useState(null);
    const [newCount, setNewCount] = useState(0);
    const [replyTo, setReplyTo] = useState(null);
    // menu is the message whose ⋯ menu is open, with where a right click
    // opened it, if one did, and deleting the one whose delete
    // waits on the member's word (M11).
    const [menu, setMenu] = useState(null);
    const [deleting, setDeleting] = useState(null);
    // reacting is the message whose picker is open to react to it, with
    // what opened it, and removing the one whose reactions staff take off
    // (M15.3).
    const [reacting, setReacting] = useState(null);
    const [removing, setRemoving] = useState(null);
    // A den older than reactions, which came with its emoji, takes none,
    // nor polls nor @everyone, which came with them (M15.4). Polls go in
    // channels, and staff send @everyone there.
    const emoji = useContext(EmojiContext);
    // The member's notes (M17.3) are theirs alone: nobody reacts to them,
    // writes in them or shares their editing, and nothing in them goes as
    // a spoiler.
    const notes = channel.kind === 'notes';
    const reactions = !!emoji?.known && !notes;
    const polls = reactions && channel.kind === 'text';
    // A DM's, a group's (M16.3) and the notes' messages are sealed by their
    // members.
    const sealed = isPrivate(channel);
    // Compact shows each message as a line (M17.2).
    const line = useDensity() === DENSITY_COMPACT;
    const width = useWidthClass();
    const background = useBackground();
    // A photo or video goes as a spoiler in a DM, inside its seal, and in a
    // channel where the den keeps which files are (M17.2).
    const canSpoil = !notes && (sealed || !!spoilers);
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
    const [divider] = useState(() => dividerAt(readPosition, lastMessage));
    // A finger works the messages by long presses, and on the phone
    // layout their actions open as a sheet (M13.2).
    const narrow = useNarrow();
    const touch = useCoarse();
    // The call's button floats above the pane's foot, where the composer is
    // (M13.4).
    const foot = useRef(null);
    const scroller = useRef(null);
    const content = useRef(null);
    const anchor = useRef(null);
    const loading = useRef({ older: false, newer: false });
    const atBottom = useRef(true);
    const lastTop = useRef(0);
    const listRef = useRef(list);
    listRef.current = list;
    // While the den is offline (M6.2), the note over the messages says why
    // the pages beyond what this computer kept don't load.
    const offlineRef = useRef(offline);
    offlineRef.current = offline;
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
    const candidates = notes ? [] : [...members.values()]
        .filter((x) => x.id !== me.id && !x.left_at && (dm ? x.id === dm.id : group ? group.members.includes(x.id) : !channel.staff_only || isStaff(x.role)))
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
            // The list scrolls itself, not what holds it: scrollIntoView
            // can move the phone layout's row of panes too, in Firefox.
            const el = box.querySelector(`[data-id="${a.id || a.jump}"]`);
            const centered = el && a.jump ? (box.clientHeight - el.offsetHeight) / 2 : a.offset;
            if (el) box.scrollTop += el.getBoundingClientRect().top - box.getBoundingClientRect().top - centered;
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
            if (!offlineRef.current) setError(e.message);
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
            if (!offlineRef.current) setError(e.message);
        } finally {
            loading.current.newer = false;
        }
    }

    // jumpTo shows a message, loading the page around it. A pane that
    // opens on one it can't load, as from a den that's offline, opens at
    // the newest instead, saying why.
    async function jumpTo(id, opening) {
        let next = null;
        if (!listRef.current.messages.some((m) => m.id === id)) {
            try {
                const got = await fetchPage(`${base}?around=${id}&limit=${PAGE}`, true);
                if (!got) return;
                next = replayed(fromPage(got.page), got.since);
            } catch (e) {
                if (opening) await loadNewest();
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

    // The pane's foot says how tall it is, for the call's button above it.
    useLayoutEffect(() => {
        const el = foot.current;
        if (!el || typeof ResizeObserver === 'undefined') return undefined;
        const observer = new ResizeObserver(() => setFoot(el.offsetHeight));
        observer.observe(el);
        setFoot(el.offsetHeight);
        return () => {
            observer.disconnect();
            setFoot(0);
        };
    }, []);

    useEffect(() => {
        const jump = pendingJump?.denID === denID && pendingJump.channelID === channel.id ? pendingJump.messageID : '';
        pendingJump = null;
        if (jump) jumpTo(jump, true);
        else loadNewest();
        const here = (d, c, id) => d === denID && c === channel.id && (jumpTo(id), true);
        jumpers.add(here);
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
            jumpers.delete(here);
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
        // As a DM's key comes or changes, what it sealed may open.
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
            gone([d.id]);
        } else if (REACTIONS.has(e.t)) {
            holdView();
            setList((cur) => withReaction(cur, e.t, d));
        } else if (POLLS.has(e.t)) {
            holdView();
            setList((cur) => withPoll(cur, e.t, d));
        }
    }

    // gone takes deleted messages out of the list, with the file the viewer
    // shows, the reply being written and the edit under way, if they were
    // among them, and their quotes out of the replies to them.
    function gone(ids) {
        const out = new Set(ids);
        setViewing((v) => (v && out.has(v.message) ? null : v));
        setDeleting((m) => (m && out.has(m.id) ? null : m));
        setReacting((r) => (r && out.has(r.id) ? null : r));
        setRemoving((id) => (out.has(id) ? null : id));
        setReplyTo((r) => (r && out.has(r.id) ? null : r));
        setEditing((id) => (out.has(id) ? null : id));
        setMenu((open) => (open && out.has(open.id) ? null : open));
        setList((cur) => ids.reduce((l, id) => withDelete(l, { id, channel_id: channel.id }), cur));
    }

    // A message that disappears leaves the list as its time comes, by this
    // computer's clock, as the den and the local service take it too
    // (M16.4): a page doesn't wait on them to stop showing it, as while
    // the den is offline.
    const [looked, setLooked] = useState(0);
    useEffect(() => {
        const now = Date.now();
        const due = dueBy(list.messages, now);
        const next = nextDue(list.messages, now);
        if (!due.length && !next) return undefined;
        const timer = setTimeout(() => {
            const ids = dueBy(listRef.current.messages, Date.now()).map((m) => m.id);
            if (ids.length) gone(ids);
            else setLooked((n) => n + 1);
        }, due.length ? 0 : Math.min(next - now, DUE_WAIT));
        return () => clearTimeout(timer);
    }, [list.messages, looked]);

    // holdView keeps what the member reads where it is as a message above
    // it changes size, as a reaction's row comes or goes, or wraps (M15.3).
    // At the newest, the list stays there by itself.
    function holdView() {
        if (!atBottom.current || listRef.current.hasNewer) captureAnchor();
    }

    // react adds the member's reaction to a message, or takes it back, and
    // shows it at once: the den's word on it comes as an event, which
    // changes nothing more. An emoji added counts toward their most used.
    async function react(m, item, on) {
        setError('');
        try {
            const res = await api.post(`/api/dens/${denID}/messages/${m.id}/reactions?channel=${channel.id}`, { ...item, on });
            holdView();
            if (res.set) setList((cur) => withReaction(cur, 'dm.reactions', { message_id: m.id, ...res.set }));
            else setList((cur) => withReaction(cur, on ? 'reaction.added' : 'reaction.removed', { message_id: m.id, member_id: me.id, ...item }));
        } catch (e) {
            setError(e.message);
            return;
        }
        if (!on) return;
        loadEmoji().then((l) => {
            const den = item.emoji_id || (item.name && emoji?.byName.get(item.name)?.id);
            const key = den ? denKey(denID, den) : item.emoji ? keyOf({ unicode: item.emoji }, denID, null, l) : null;
            if (key) noteUses([key]);
        }, () => {});
    }

    // pickReaction reacts with what the picker picked, where the message
    // has room for another emoji.
    function pickReaction(id, item, chips) {
        const m = listRef.current.messages.find((x) => x.id === id);
        if (!m) return;
        const key = item.emoji ? `u:${item.emoji}` : `d:${item.emoji_id}`;
        const name = item.emoji_id && emoji?.all.find((e) => e.id === item.emoji_id)?.name;
        if (!room(chips, key) && !chips.some((c) => c.name && c.name === name)) {
            setError('A message holds at most 20 different emoji.');
            return;
        }
        react(m, item, true);
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
        setDeleting((m) => (m && gone(m.id) ? null : m));
        setList((cur) => withExpired(cur, through));
        // A run that lost every message it held starts again from the newest.
        if (l.hasNewer && l.messages.length && l.messages.every((m) => gone(m.id))) loadNewest();
    }

    // addFiles starts uploading files the member picked, dropped or pasted,
    // up to what a message holds. Uploads go ahead while they write. A
    // photo or video goes as a smaller copy unless the member turned copies
    // off, and one over the den's limit may still go as its copy (M5). Each
    // upload has a key the chip follows it by while a video's copy is made
    // (M5.4).
    function addFiles(list) {
        const picked = [...list];
        const room = MAX_ATTACHMENTS - filesRef.current.length;
        if (picked.length > room) setError(`A message holds at most ${MAX_ATTACHMENTS} files.`);
        const added = picked.slice(0, Math.max(0, room)).map((file) => {
            const entry = {
                key: nextKey++, file, name: file.name || 'pasted image.png', size: file.size, progress: 0, progressKey: progressKey(),
            };
            if (limits?.file_size && file.size > limits.file_size && !mayGoSmaller(file)) {
                entry.error = `This file is larger than this den allows (${formatSize(limits.file_size)}).`;
            }
            return entry;
        });
        setFiles((cur) => [...cur, ...added]);
        const send = sendSmaller() ? 'smaller' : 'full';
        for (const entry of added) {
            if (entry.error) continue;
            const up = upload(denID, channel.id, entry.file, entry.name, (progress) => updateFile(entry.key, { progress }), '', send,
                entry.progressKey, (following) => updateFile(entry.key, { following }));
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

    async function send(text, poll, pendingEntry) {
        if (listRef.current.hasNewer) loadNewest();
        const entry = pendingEntry || {
            nonce: newNonce(), text, reply_to: replyTo?.id || '', attachments: files.map((f) => f.result), editors, poll, failed: false,
            spoilers: canSpoil ? files.filter((f) => f.spoiler && f.result).map((f) => f.result.id) : [],
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
                poll: entry.poll || undefined,
                spoilers: entry.spoilers?.length ? entry.spoilers : undefined,
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

    // A DM takes messages unless something stands in its composer's
    // place (M16.1), and a group as well (M16.3), whose gate names whom it
    // waits on.
    const gated = group ? gate.gate : gate;
    const canSend = !sealed || gated === '';
    const byId = new Map(list.messages.map((m) => [m.id, m]));
    const rows = [];
    let prev = null;
    // In a DM, dividers mark where its keys changed before M16.1, and
    // lines where the other member's keys changed since.
    const dividers = sealed && keys ? keyChanges(list.messages, keys, (id) => (id === me.id ? 'You' : name(members.get(id))), me.id) : new Map();
    // In a group, lines where its members changed, and where each one's
    // keys did since this member joined it (M16.3).
    const joinedAt = group?.changes?.[0]?.at || 0;
    const keyLines = group
        ? group.members.filter((id) => id !== me.id)
            .flatMap((id) => (identities?.[id]?.changes || []).filter((at) => at >= joinedAt).map((at) => ({ at, member: id })))
        : (dm ? changes || [] : []).map((at) => ({ at, member: dm?.id }));
    const changed = sealed ? placeAt(list.messages, keyLines, (x) => x.at, list) : { before: new Map(), after: [] };
    const changeLine = ({ at, member }) => {
        const who = members.get(member);
        return <KeysChanged key={`changed-${member}-${at}`} name={who ? who.display_name : 'The other member'} at={at} onCompare={() => who && onCompare?.(who)} />;
    };
    const groupLines = group ? placeAt(list.messages, group.changes || [], (c) => c.at, list) : { before: new Map(), after: [] };
    const groupLine = (c) => <GroupLine key={`group-${c.n}`} change={c} me={me.id} nameOf={(id) => name(members.get(id))} />;
    // In a DM or a group, the line that says who last set its time, and
    // when (M16.4).
    const expiry = sealed && channel.expires_set ? placeAt(list.messages, [channel.expires_set], (x) => x.at, list) : { before: new Map(), after: [] };
    const expiryLine = (x) => <ExpiryLine key={`expiry-${x.at}`} channel={channel} me={me.id} nameOf={(id) => name(members.get(id))} />;
    for (const m of list.messages) {
        const day = new Date(m.created_at).toDateString();
        if (!prev || new Date(prev.created_at).toDateString() !== day) {
            rows.push(<DayDivider key={`day-${m.id}`} time={m.created_at} />);
        }
        if (dividers.has(m.id)) rows.push(<KeyDivider key={`keys-${m.id}`} text={dividers.get(m.id)} />);
        for (const x of changed.before.get(m.id) || []) rows.push(changeLine(x));
        for (const c of groupLines.before.get(m.id) || []) rows.push(groupLine(c));
        for (const x of expiry.before.get(m.id) || []) rows.push(expiryLine(x));
        if (divider && compareIds(m.id, divider) > 0 && (!prev || compareIds(prev.id, divider) <= 0) && m.author_id !== me.id) {
            rows.push(<div key="unread" class="divider divider-error my-0 cursor-default select-none text-xs text-error">New</div>);
        }
        const compact = continues(prev, m, line);
        // The loaded copy is newer than the den's preview, if there is one.
        const replied = m.reply_to ? byId.get(m.reply_to) || m.reply : null;
        rows.push(
            <MessageRow
                key={m.id}
                m={m}
                compact={compact}
                line={line}
                author={members.get(m.author_id)}
                replied={replied}
                repliedAuthor={replied ? members.get(replied.author_id) : null}
                me={me}
                highlighted={highlight === m.id}
                editing={editing === m.id}
                canReply={!closed && canSend}
                canEdit={!m.locked && !m.poll && (m.author_id === me.id || (m.editors || []).includes(me.id))}
                canDelete={m.author_id === me.id || (!sealed && rank(role) > rank(members.get(m.author_id)?.role))}
                link={denURL ? linkTo(denURL, denID, channel.id, m.id) : ''}
                menuOpen={menu?.id === m.id}
                menuAt={menu?.id === m.id ? menu.at : null}
                narrow={narrow}
                touch={touch}
                editorNames={(m.editors || []).map((id) => name(members.get(id)))}
                candidates={candidates}
                canReact={reactions && !m.locked && !closed && canSend}
                canRemoveReactions={!sealed && isStaff(role) &&
                    (m.reactions || []).some((r) => r.members.some((id) => rank(members.get(id)?.role) < rank(role)))}
                nameOf={(id) => name(members.get(id))}
                onReact={(item, on) => react(m, item, on)}
                onReactPick={(anchor) => setReacting({ id: m.id, anchor })}
                onRemoveReactions={() => setRemoving(m.id)}
                onUpdated={(u) => setList((cur) => withUpdate(cur, u, true))}
                onPolled={(t, d) => {
                    holdView();
                    setList((cur) => withPoll(cur, t, d));
                }}
                onProfile={onProfile}
                onMenu={(open, at) => setMenu(open ? { id: m.id, at } : null)}
                onReply={() => setReplyTo(m)}
                onEdit={() => setEditing(m.id)}
                onEditDone={() => setEditing(null)}
                onDelete={() => setDeleting(m)}
                onJump={jumpTo}
                onView={(f, covered) => setViewing(viewed(m, f, covered))}
                onToggle={toggled}
                denID={denID}
                channelID={channel.id}
            />,
        );
        prev = m;
    }
    rows.push(...changed.after.map(changeLine));
    rows.push(...groupLines.after.map(groupLine));
    rows.push(...expiry.after.map(expiryLine));

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
            {offline && (
                <div role="status" class="cursor-default select-none border-b border-warning/40 bg-warning/10 px-4 py-1 text-sm">
                    {OFFLINE_NOTE}
                </div>
            )}
            {/* The room at the bottom is where who's typing shows, over the list.
                Appearance's Width holds the content, not the scroller, so the
                scrollbar stays at the pane's edge. */}
            <div ref={scroller} data-scroller class="min-h-0 flex-1 overflow-y-auto px-4 pb-6 pt-2" style={backgroundStyle(background)} onScroll={checkEdges}>
                <div ref={content} class={width}>
                    {list.loaded && !list.hasOlder && (
                        <div class="cursor-default select-none py-6 text-center text-sm text-base-content/60">
                            {notes ? (
                                <>
                                    <p>{retention ? `Your notes are deleted after ${keptFor(retention)}.` : 'This is the start of your notes.'}</p>
                                    <p class="text-xs">End-to-end encrypted: only your devices can read them.</p>
                                </>
                            ) : group ? (
                                <>
                                    <p>{retention ? `Messages in ${groupTitle} are deleted after ${keptFor(retention)}.` : `This is the start of ${groupTitle}, as you can read it.`}</p>
                                    <p class="text-xs">End-to-end encrypted: only the group's members can read it, each from when they joined.</p>
                                </>
                            ) : dm ? (
                                <>
                                    <p>
                                        {retention
                                            ? `Messages with ${dm.display_name} are deleted after ${keptFor(retention)}.`
                                            : `This is the start of your conversation with ${dm.display_name}.`}
                                    </p>
                                    <p class="text-xs">
                                        End-to-end encrypted: only the two of you can read it.{' '}
                                        <button type="button" class="link link-primary" onClick={() => onCompare?.(dm)}>Compare codes</button>
                                    </p>
                                </>
                            ) : (
                                <p>
                                    {retention
                                        ? `Messages in #${channel.name} are deleted after ${keptFor(retention)}.`
                                        : `This is the start of #${channel.name}.`}
                                </p>
                            )}
                        </div>
                    )}
                    {list.hasOlder && !offline && <div class="py-3 text-center"><span class="loading loading-dots loading-sm"></span></div>}
                    {!list.loaded && !error && <span class="loading loading-spinner"></span>}
                    {rows}
                    {!list.hasNewer && pending.map((p) => <PendingRow key={p.nonce} p={p} me={me} line={line} onToggle={toggled} onRetry={() => send(p.text, p.poll, p)} onDiscard={() => setPending((ps) => ps.filter((x) => x.nonce !== p.nonce))} />)}
                    {list.hasNewer && <div class="py-3 text-center"><span class="loading loading-dots loading-sm"></span></div>}
                </div>
            </div>
            {/* The pane's foot, the composer among it, which the call's button
                floats above (M13.4). */}
            <div ref={foot} class={width}>
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
                {!closed && (channel.kind === 'dm' || notes) && gate !== '' && (
                    <DMGate denID={denID} gate={gate} dm={dm} notes={notes} onCompare={() => dm && onCompare?.(dm)} />
                )}
                {!closed && group && gated !== '' && (
                    <GroupGate denID={denID} gate={gated} member={members.get(gate.member)} onCompare={(m) => onCompare?.(m)} />
                )}
                {closed ? (
                    <div class="cursor-default select-none border-t border-base-300 px-4 py-3 text-sm text-base-content/60">{closed}</div>
                ) : !canSend ? null : (
                    <Composer
                        denID={denID}
                        limits={limits}
                        placeholder={notes ? 'Write a note' : dm ? `Message @${dm.username}` : group ? `Message ${groupTitle}` : `Message #${channel.name}`}
                        replyTo={replyTo}
                        replyAuthor={replyTo ? members.get(replyTo.author_id) : null}
                        typing={typing}
                        files={files}
                        onAddFiles={addFiles}
                        onRemoveFile={removeFile}
                        canSpoil={canSpoil}
                        onSpoiler={(key, on) => updateFile(key, { spoiler: on })}
                        onCompare={setComparing}
                        onCancelReply={() => setReplyTo(null)}
                        candidates={candidates}
                        mentionable={sealed ? null : candidates}
                        polls={polls}
                        everyone={polls && isStaff(role)}
                        shares={!notes}
                        editors={editors}
                        onEditors={setEditors}
                        onSend={(text, poll) => send(text, poll)}
                        onEditLast={editLast}
                        onTyping={onTyping}
                        narrow={narrow}
                        touch={touch}
                    />
                )}
            </div>
            {viewing && (
                <Viewer denID={denID} files={viewing.files} index={viewing.index} onClose={() => setViewing(null)}
                    onIndex={(index) => setViewing((v) => v && { ...v, index })} />
            )}
            {deleting && (
                <DeleteDialog denID={denID} m={deleting} author={members.get(deleting.author_id)} me={me} onClose={() => setDeleting(null)} />
            )}
            {reacting && byId.has(reacting.id) && (
                <ReactingPicker m={byId.get(reacting.id)} me={me} anchor={reacting.anchor} sheet={narrow}
                    onPick={(item, chips) => pickReaction(reacting.id, item, chips)} onClose={() => setReacting(null)} />
            )}
            {removing && byId.has(removing) && (
                <RemoveReactions denID={denID} m={byId.get(removing)} me={me} myRank={rank(role)} rankOf={(id) => rank(members.get(id)?.role)}
                    nameOf={(id) => name(members.get(id))} onClose={() => setRemoving(null)} />
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

// MessageRow is a message in the list, with its ⋯ menu of what the member
// may do with it: link is the link to it, for Copy link (M11). A finger's
// long press opens the menu in the ⋯'s place, which a touch screen has
// none of, and on the phone layout, narrow, it's a sheet (M13.2); a mouse's
// right click opens it at the pointer, menuAt. compact is a run's
// later message, under its first's name; line is Compact's row, each a
// line of its own (M17.2).
export function MessageRow({ m, compact, line, author, replied, repliedAuthor, me, highlighted, editing, canReply, canEdit, canDelete, link, menuOpen, menuAt, narrow, touch, editorNames, candidates, canReact, canRemoveReactions, nameOf, onReact, onReactPick, onRemoveReactions, onUpdated, onPolled, onMenu, onReply, onEdit, onEditDone, onDelete, onJump, onView, onProfile, onToggle, denID, channelID }) {
    const [error, setError] = useState('');
    // copied says for a moment what the member just copied.
    const [copied, setCopied] = useState('');
    const copiedTimer = useRef(null);
    const opening = useRef(onMenu);
    opening.current = onMenu;
    const press = useRef(null);
    press.current ||= createPress({ onLong: () => opening.current(true) });
    useEffect(() => () => {
        clearTimeout(copiedTimer.current);
        press.current.cancel();
    }, []);
    async function copy(what, done) {
        setError('');
        try {
            await copyText(what);
        } catch (e) {
            setError(e.message);
            return;
        }
        setCopied(done);
        clearTimeout(copiedTimer.current);
        copiedTimer.current = setTimeout(() => setCopied(''), 1500);
    }
    // Add reaction opens the picker beside the ⋯ it was picked from, or as
    // a sheet (M15.3).
    const actions = [
        canReply && { label: 'Reply', run: onReply },
        canReact && { label: 'Add reaction', run: (from) => onReactPick(from) },
        canEdit && { label: 'Edit', run: onEdit },
        !m.locked && m.text.trim() && { label: 'Copy text', run: () => copy(m.text, 'Text copied') },
        link && { label: 'Copy link', run: () => copy(link, 'Link copied') },
        canRemoveReactions && { label: 'Remove reactions', run: onRemoveReactions },
        canDelete && { label: 'Delete', run: onDelete, danger: true },
    ].filter(Boolean);
    const { chips } = useChips(m, me.id);
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
    // The long press takes the place of the browser's own selection and
    // menu on a message, and a click it's followed by goes nowhere.
    const pressing = touch && !editing && actions.length > 0;
    const presses = pressing ? {
        onPointerDown: (e) => press.current.down(e),
        onPointerMove: (e) => press.current.move(e),
        onPointerUp: () => press.current.up(),
        onPointerCancel: () => press.current.cancel(),
        onContextMenu: (e) => e.preventDefault(),
    } : !touch && !editing && actions.length > 0 ? {
        onContextMenu: (e) => {
            if (!ownsMenu(e.target, getSelection(), e.currentTarget)) return;
            e.preventDefault();
            onMenu(true, narrow ? null : rightClick(e, e.currentTarget));
        },
    } : {};
    // Staff's mark centers on the name, whose box keeps the line's
    // baseline.
    const authorName = (
        <span class="flex items-center gap-1">
            <button type="button" class="font-semibold hover:underline" onClick={() => author && onProfile(author)}>{name(author)}</button>
            <RoleMark role={author?.role} />
        </span>
    );
    // In Compact, each message is a line of its time, its author's name and
    // its text (M17.2): the name floats at the text's start, so the text
    // wraps under it, and the rest goes below, from the name's start.
    const lead = line ? (
        <span class="float-left mr-2 flex items-baseline gap-1">
            {authorName}
            {m.expires_at ? <Disappears at={m.expires_at} /> : null}
        </span>
    ) : null;
    let body = lead;
    if (m.locked) {
        body = <div>{lead}<p class="cursor-default select-none text-sm italic text-base-content/60">{LOCKED[m.locked] || LOCKED.broken}</p></div>;
    } else if (editing) {
        body = (
            <>
                {lead}
                <div class={line ? 'clear-both' : ''}>
                    <EditBox m={m} denID={denID} channelID={channelID} mine={m.author_id === me.id} candidates={candidates} touch={touch} onDone={onEditDone} />
                </div>
            </>
        );
    } else if (m.text.trim() || m.edited_at) {
        body = (
            <Fold onToggle={onToggle}>
                {/* A quote beside the name starts past it, its bar too. */}
                <div class={`wrap-break-word ${line ? '[&_blockquote]:flow-root' : ''}`}>
                    {lead}
                    {m.text.trim() && <Markdown text={m.text} me={me} everyone={m.everyone} onTask={canEdit ? tick : undefined} big />}
                    {m.edited_at ? <span class="cursor-default select-none text-xs text-base-content/50" title={new Date(m.edited_at).toLocaleString()}> (edited)</span> : null}
                    {editorNames.length > 0 && (
                        <span class="cursor-default select-none text-xs text-base-content/50" title={`${joinNames(editorNames)} can edit this too`}> (shared)</span>
                    )}
                </div>
            </Fold>
        );
    }
    const below = (
        <>
            {m.poll && !m.locked && !editing && <Poll m={m} me={me} denID={denID} nameOf={nameOf} onPolled={onPolled} />}
            {m.attachments?.length > 0 && <Attachments denID={denID} files={m.attachments} onOpen={onView} />}
            {!m.locked && !editing && m.text && <Videos text={m.text} />}
            {!m.locked && !editing && m.text && <EventCards text={m.text} />}
            <Reactions m={m} me={me} nameOf={nameOf} canReact={canReact} onToggle={(c, on) => onReact(reactionOf(c), on)} onAdd={onReactPick} />
            {error && <p class="text-xs text-error">{error}</p>}
        </>
    );
    // Its pictures, and that of the message it replies to, play under the
    // pointer (M15.1).
    return (
        <HoverRow
            data-id={m.id}
            data-line={line ? '' : undefined}
            class={`group relative rounded px-2 ${line ? 'py-0.5' : compact ? 'py-0' : 'mt-2 py-0.5'} ${highlighted ? 'bg-warning/20' : menuOpen ? 'bg-base-200' : 'hover:bg-base-200'} ${pressing ? 'select-none' : ''}`}
            {...presses}
            onClickCapture={(e) => {
                if (!press.current.click()) return;
                e.preventDefault();
                e.stopPropagation();
            }}
        >
            {/* In Compact the quote sits over the name, past the time. */}
            {m.reply_to && replied && (
                <button type="button" class={`flex w-full min-w-0 items-center gap-1 text-left text-xs text-base-content/60 hover:text-base-content ${line ? 'pl-14' : 'pl-2'}`}
                    onClick={() => onJump(m.reply_to)}>
                    <Spine />
                    {!line && <Avatar member={repliedAuthor} size="sm" />}
                    <span class="shrink-0 font-medium">{name(repliedAuthor)}</span>
                    <span class="truncate"><Quoted text={replied.text} locked={replied.locked} me={me} /></span>
                </button>
            )}
            {m.reply_to && !replied && (
                <div class={`flex items-center gap-1 text-xs italic text-base-content/50 ${line ? 'pl-14' : 'pl-2'}`}>
                    <Spine />
                    The original message was deleted.
                </div>
            )}
            <div class={`flex ${line ? 'gap-2' : 'gap-3'}`}>
                {line ? (
                    <span class="w-14 shrink-0 cursor-default select-none whitespace-nowrap text-xs leading-6 tabular-nums text-base-content/50"
                        title={new Date(m.created_at).toLocaleString()}>
                        {clock(m.created_at)}
                    </span>
                ) : (
                    <div class="relative w-9 shrink-0">
                        {compact ? (
                            // Out of the flow, so the time never makes a row taller.
                            // A touch screen has it in the long press's sheet.
                            <span class="invisible absolute -inset-x-1 top-1 cursor-default select-none whitespace-nowrap text-center text-[10px] text-base-content/50 group-hover:visible pointer-coarse:hidden">
                                {clock(m.created_at)}
                            </span>
                        ) : (
                            <button type="button" class="mt-0.5 rounded-full" onClick={() => author && onProfile(author)} aria-label={`${name(author)}'s profile`}>
                                <Avatar member={author} />
                            </button>
                        )}
                    </div>
                )}
                <div class="min-w-0 flex-1">
                    {!line && !compact && (
                        <div class="flex items-baseline gap-2">
                            {authorName}
                            <span class="text-xs text-base-content/50" title={new Date(m.created_at).toLocaleString()}>{clock(m.created_at)}</span>
                            {m.expires_at ? <Disappears at={m.expires_at} /> : null}
                        </div>
                    )}
                    {body}
                    {line ? <div class="clear-both">{below}</div> : below}
                </div>
            </div>
            {!editing && actions.length > 0 && (
                <MessageMenu open={menuOpen} at={menuAt} narrow={narrow} onOpen={onMenu} actions={actions} copied={copied} onReact={canReact ? onReactPick : null} />
            )}
            {!editing && menuOpen && narrow && actions.length > 0 && (
                <Sheet label="Message actions" title={<MessageHeading m={m} author={author} editorNames={editorNames} />} onClose={() => onMenu(false)}>
                    {canReact && (
                        <QuickReactions denID={denID} chips={chips}
                            onReact={(item, on) => {
                                onMenu(false);
                                onReact(item, on);
                            }}
                            onMore={() => {
                                onMenu(false);
                                onReactPick(null);
                            }} />
                    )}
                    {actions.filter((a) => a.label !== 'Add reaction').map((a) => (
                        <SheetRow key={a.label} danger={a.danger} onClick={() => {
                            onMenu(false);
                            a.run();
                        }}>
                            {a.label}
                        </SheetRow>
                    ))}
                </Sheet>
            )}
        </HoverRow>
    );
}

// ReactingPicker opens the picker to react to a message, and hands what's
// picked on with the message's chips, which say whether it has room for
// another emoji (M15.3).
function ReactingPicker({ m, me, anchor, sheet, onPick, onClose }) {
    const { chips } = useChips(m, me.id);
    return <ReactionPicker anchor={anchor} sheet={sheet} onPick={(item) => onPick(item, chips)} onClose={onClose} />;
}

// MessageHeading heads a message's sheet with what a hover shows of it
// elsewhere: its author and time in full, when it was edited, and who
// else can edit it.
function MessageHeading({ m, author, editorNames }) {
    return (
        <>
            <p class="truncate"><span class="font-semibold">{name(author)}</span> <span class="text-base-content/60">{dayTime(m.created_at)}</span></p>
            {m.edited_at ? <p class="text-xs text-base-content/60">Edited {dayTime(m.edited_at)}</p> : null}
            {m.expires_at ? <p class="text-xs text-base-content/60">Disappears {dayTime(m.expires_at)}</p> : null}
            {editorNames.length > 0 && <p class="text-xs text-base-content/60">{joinNames(editorNames)} can edit this too</p>}
        </>
    );
}

// Disappears marks a message that disappears (M16.4), and says when.
function Disappears({ at }) {
    const when = `Disappears ${new Date(at).toLocaleString()}`;
    return (
        <span class="self-center text-base-content/50" role="img" title={when} aria-label={when} data-disappears>
            <TimerIcon size="h-3 w-3" />
        </span>
    );
}

// MessageMenu is a message's ⋯, which shows on hover or focus, for a mouse
// and the keyboard, and the menu of what the member may do with the
// message (M11), which a touch screen's long press opens without it
// (M13.2), and a right click at the pointer, at. Under the ⋯, the
// menu opens toward whichever side of the list has room for it. Arrow keys
// move through it, and Escape, Tab or a press elsewhere closes it. On the
// phone layout, narrow, the ⋯ opens the message's sheet instead. copied
// says what the member just copied.
function MessageMenu({ open, at, narrow, onOpen, actions, copied, onReact }) {
    const box = useRef(null);
    const button = useRef(null);
    const list = useRef(null);
    const [up, setUp] = useState(false);
    const dropdown = open && !narrow;
    usePlaced(dropdown ? at : null, list);
    useLayoutEffect(() => {
        if (!dropdown || !list.current) return;
        const view = box.current.closest('[data-scroller]')?.getBoundingClientRect();
        const at = box.current.getBoundingClientRect();
        const below = view ? view.bottom - at.bottom : Infinity;
        setUp(below < list.current.offsetHeight + 8 && at.top - view.top > below);
    }, [dropdown]);
    useEffect(() => {
        if (!dropdown) return undefined;
        list.current?.querySelector('[role="menuitem"]')?.focus();
        const away = (e) => !box.current?.contains(e.target) && onOpen(false);
        document.addEventListener('pointerdown', away);
        return () => document.removeEventListener('pointerdown', away);
    }, [dropdown]);
    function toggle() {
        onOpen(!open);
    }
    // close gives the focus back to the ⋯, unless what was picked takes it.
    function close() {
        onOpen(false);
        button.current?.focus();
    }
    function keys(e) {
        const items = [...list.current.querySelectorAll('[role="menuitem"]')];
        const at = items.indexOf(document.activeElement);
        const to = { ArrowDown: at + 1, ArrowUp: at - 1, Home: 0, End: items.length - 1 }[e.key];
        if (to !== undefined) {
            e.preventDefault();
            items[(to + items.length) % items.length].focus();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            close();
        } else if (e.key === 'Tab') {
            onOpen(false);
        }
    }
    const hidden = 'pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-focus-within:opacity-100';
    // An open menu stands over the rows below it, and their ⋯.
    return (
        <div ref={box} class={`absolute -top-3 right-2 flex items-center gap-1 ${dropdown ? 'z-20' : 'z-10'} ${dropdown || copied ? '' : hidden}`}>
            {copied && <span role="status" class="cursor-default select-none rounded bg-base-100 px-1.5 py-0.5 text-xs shadow">{copied}</span>}
            {onReact && (
                <button type="button" class="btn btn-ghost btn-xs btn-square bg-base-100 shadow pointer-coarse:hidden" aria-label="Add a reaction"
                    title="Add a reaction" data-react-button onClick={(e) => onReact(e.currentTarget)}>
                    <SmileyIcon size="h-4 w-4" />
                </button>
            )}
            <button ref={button} type="button" class="btn btn-ghost btn-xs btn-square bg-base-100 shadow pointer-coarse:hidden"
                aria-label="Message actions" title="More" aria-haspopup="menu" aria-expanded={open} onClick={toggle}
                onKeyDown={(e) => {
                    if (e.key !== 'ArrowDown' || open) return;
                    e.preventDefault();
                    toggle();
                }}>
                ⋯
            </button>
            {dropdown && (
                <ul ref={list} role="menu" aria-label="Message actions" onKeyDown={keys}
                    class={`menu absolute z-20 w-44 rounded-box bg-base-100 p-1 shadow ${at ? 'left-0 top-0' : up ? 'right-0 bottom-full mb-1' : 'right-0 top-full mt-1'}`}>
                    {actions.map((a) => (
                        <li key={a.label} role="none">
                            <button type="button" role="menuitem" tabIndex={-1} class={`pointer-coarse:min-h-11 ${a.danger ? 'text-error' : ''}`}
                                onClick={() => {
                                    close();
                                    a.run(button.current);
                                }}>
                                {a.label}
                            </button>
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}

// DeleteDialog asks before a message is deleted, which is for everyone and
// for good, with its files (M11). Staff deleting another member's message
// see whose it is. Closing it gives the focus back to the ⋯ that opened it.
function DeleteDialog({ denID, m, author, me, onClose }) {
    const act = useAction();
    const confirm = useRef(null);
    useEffect(() => {
        const opener = document.activeElement;
        confirm.current?.focus();
        const escape = (e) => e.key === 'Escape' && onClose();
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('keydown', escape);
            if (opener?.isConnected) opener.focus();
        };
    }, []);
    const files = m.attachments?.length ? ', with its files' : '';
    const whose = m.author_id === me.id ? '' : `This is ${name(author)}'s message${author ? `, @${author.username}` : ''}. `;
    function remove() {
        act.run(async () => {
            await api.del(`/api/dens/${denID}/messages/${m.id}`);
            onClose();
        });
    }
    return (
        <Dialog title="Delete this message?" onClose={onClose}>
            <HoverRow class="flex min-w-0 cursor-default select-none items-center gap-2 rounded-box border border-base-300 p-2 text-sm">
                <Avatar member={author} size="sm" />
                <span class="shrink-0 font-medium">{name(author)}</span>
                <span class="min-w-0 truncate text-base-content/70"><Quoted text={m.text} locked={m.locked} me={me} /></span>
            </HoverRow>
            <p class="text-sm">{whose}It goes for everyone{files}, and can't be brought back.</p>
            <ErrorText message={act.error} />
            <div class="flex gap-2">
                <button ref={confirm} type="button" class="btn btn-error btn-sm" disabled={act.busy} onClick={remove}>
                    {act.busy && <span class="loading loading-spinner loading-sm"></span>}
                    Delete
                </button>
                <button type="button" class="btn btn-ghost btn-sm" onClick={onClose}>Cancel</button>
            </div>
        </Dialog>
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
    // below it, so the message comes back into view before the next frame,
    // its list scrolling alone, as a jump to a message does.
    useLayoutEffect(() => {
        if (!folding.current) return;
        folding.current = false;
        const el = box.current;
        const list = el?.closest('[data-scroller]');
        if (!list) return;
        const r = el.getBoundingClientRect();
        const view = list.getBoundingClientRect();
        if (r.top < view.top) list.scrollTop += r.top - view.top;
        else if (r.bottom > view.bottom) list.scrollTop += Math.min(r.bottom - view.bottom, r.top - view.top);
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

function PendingRow({ p, me, line, onToggle, onRetry, onDiscard }) {
    const status = <span class="cursor-default select-none text-xs">{p.failed ? 'Not sent' : 'Sending…'}</span>;
    // In Compact, a line as the sent ones are (M17.2).
    const lead = line ? <span class="float-left mr-2 flex items-baseline gap-2"><span class="font-semibold">{me.display_name}</span>{status}</span> : null;
    const below = (
        <>
            {p.attachments?.length > 0 && (
                <ul class="text-xs text-base-content/70">
                    {p.attachments.map((f) => <li key={f.id} class="truncate">{f.name} · {formatSize(f.size)}</li>)}
                </ul>
            )}
            {p.poll && (
                <ul class="text-xs text-base-content/70" aria-label="Choices">
                    {p.poll.choices.map((c, i) => <li key={i} class="truncate">○ {c}</li>)}
                </ul>
            )}
            {p.failed && (
                <div class="flex items-center gap-2 text-xs text-error">
                    <span>{p.error}</span>
                    <button type="button" class="btn btn-ghost btn-xs" onClick={onRetry}>Retry</button>
                    <button type="button" class="btn btn-ghost btn-xs" onClick={onDiscard}>Discard</button>
                </div>
            )}
        </>
    );
    return (
        <HoverRow class={`flex rounded px-2 py-0.5 opacity-60 ${line ? 'gap-2' : 'mt-2 gap-3'}`}>
            <div class={`shrink-0 ${line ? 'w-14' : 'w-9'}`}>
                {!line && <Avatar member={me} />}
            </div>
            <div class="min-w-0 flex-1">
                {!line && (
                    <div class="flex items-baseline gap-2">
                        <span class="font-semibold">{me.display_name}</span>
                        {status}
                    </div>
                )}
                {p.text.trim() ? (
                    <Fold onToggle={onToggle}>
                        <div>
                            {lead}
                            <Markdown text={p.text} me={me} big />
                        </div>
                    </Fold>
                ) : lead}
                {line ? <div class="clear-both">{below}</div> : below}
            </div>
        </HoverRow>
    );
}

// EditBox edits a message against the revision it started from. If the
// message changed meanwhile, it shows the newer text and keeps the draft.
// Its author also changes who else may edit it. With a finger, touch, ↵
// starts a line, and Save saves (M13.2).
function EditBox({ m, denID, channelID, mine, candidates, touch, onDone }) {
    const [text, setText] = useState(m.text);
    const [editors, setEditors] = useState(m.editors || []);
    const [sharing, setSharing] = useState(false);
    const [base, setBase] = useState(m);
    const [conflict, setConflict] = useState(null);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    // The box takes the focus with the caret at the end, however the edit
    // started: ↑ in the composer, or Edit in the message's menu.
    const box = useRef(null);
    useEffect(() => {
        const el = box.current;
        el?.focus();
        el?.setSelectionRange(el.value.length, el.value.length);
    }, []);
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
                ref={box}
                class="textarea focus-border w-full"
                rows={Math.min(8, text.split('\n').length + 1)}
                enterkeyhint={touch ? 'enter' : 'done'}
                value={text}
                onInput={(e) => setText(e.currentTarget.value)}
                onKeyDown={(e) => {
                    if (e.key === 'Escape') onDone();
                    if (sends(e, touch)) {
                        e.preventDefault();
                        save();
                    }
                }}
            ></textarea>
            {mine && (sharing ? (
                <div class="flex flex-col gap-1 rounded border border-base-300 p-2">
                    <span class="text-xs text-base-content/70">Who else can edit this message and tick its boxes?</span>
                    <EditorList candidates={candidates} selected={editors} onChange={setEditors} />
                </div>
            ) : (
                <div class="flex items-center gap-2 text-xs text-base-content/70">
                    <span>{editors.length ? `${joinNames(names)} can edit this too.` : 'Only you can edit this.'}</span>
                    <button type="button" class="btn btn-ghost btn-xs pointer-coarse:btn-sm" onClick={() => setSharing(true)}>Change</button>
                </div>
            ))}
            {error && <p class="text-xs text-error">{error}</p>}
            <div class="flex gap-2 text-xs">
                <button type="button" class="btn btn-primary btn-xs pointer-coarse:btn-sm" onClick={save} disabled={busy}>Save</button>
                <button type="button" class="btn btn-ghost btn-xs pointer-coarse:btn-sm" onClick={onDone}>Cancel</button>
                {!touch && <span class="cursor-default select-none self-center text-base-content/50">Enter to save, Escape to cancel</span>}
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
                <input class="input input-sm w-full" placeholder="Find someone" value={filter} onInput={(e) => setFilter(e.currentTarget.value)} {...PLAIN} />
            )}
            <ul class="flex max-h-32 flex-wrap gap-x-4 gap-y-1 overflow-y-auto" aria-label="Members who can edit">
                {shown.map((c) => {
                    const on = selected.includes(c.id);
                    return (
                        <li key={c.id}>
                            <label class="flex min-h-6 cursor-pointer items-center gap-1.5 text-sm pointer-coarse:min-h-11 pointer-coarse:gap-2">
                                <input type="checkbox" class="checkbox checkbox-xs pointer-coarse:checkbox-sm" checked={on} disabled={!on && full}
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

// Composer writes a message: its text, its files, a reply and who else may
// edit it. A smiley in the box's corner opens the emoji picker (M15.2),
// and typing a colon and two characters, or in a channel an @, where a
// name or a mention may start, lists what completes it: mentionable are
// the members a mention may name, none in a DM, and with everyone, staff's
// @everyone first (M15.4). With polls, the + makes the message a poll,
// whose question is its text (M15.4).
function Composer({ denID, limits, placeholder, replyTo, replyAuthor, typing, files, candidates, mentionable, everyone, polls, shares = true, editors, onEditors, onAddFiles, onRemoveFile, canSpoil, onSpoiler, onCompare, onCancelReply, onSend, onEditLast, onTyping, narrow, touch }) {
    const [text, setText] = useState('');
    // poll is the poll being written, which goes without files or other
    // editors, and problem what kept it from going.
    const [poll, setPoll] = useState(null);
    const [problem, setProblem] = useState('');
    const emoji = useContext(EmojiContext);
    const den = emoji?.on ? emoji.list : [];
    // picking says the emoji picker is open; completing is what the caret
    // completes, with what it offers and the one marked, and dismissed the
    // text Escape closed it on, which stays closed until the text changes.
    const [picking, setPicking] = useState(false);
    const [completing, setCompleting] = useState(null);
    const dismissed = useRef(null);
    // picked counts the emoji put in by the picker and completion since the
    // last message went, which count as they go in, not again as it goes.
    const picked = useRef(new Map());
    // caret is where the caret goes once the text that put it there shows,
    // and refocus has the box take the focus back once the picker's sheet,
    // which gives it back to its opener, has closed.
    const caret = useRef(null);
    const refocus = useRef(false);
    const [sharing, setSharing] = useState(false);
    // The + menu, and the guide to writing a message (M3.3).
    const [adding, setAdding] = useState(false);
    const [guide, setGuide] = useState(false);
    // focused says the box has the focus, which on a touch screen brings
    // its keyboard up.
    const [focused, setFocused] = useState(false);
    useEffect(() => {
        if (!picking || narrow) return undefined;
        const away = (e) => {
            if (!e.target.closest?.('[data-emoji-picker]') && !e.target.closest?.('[data-emoji-button]')) setPicking(false);
        };
        const escape = (e) => {
            if (e.key === 'Escape') {
                setPicking(false);
                box.current?.focus();
            }
        };
        document.addEventListener('pointerdown', away);
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('pointerdown', away);
            document.removeEventListener('keydown', escape);
        };
    }, [picking, narrow]);
    useEffect(() => {
        if (!adding || narrow) return undefined;
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
    }, [adding, narrow]);
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
    const something = !!text.trim() || files.length > 0;
    function submit() {
        if (!something || length > MAX_TEXT || uploading || failed) return;
        const held = poll && draftProblem(poll, text);
        if (held) {
            setProblem(held);
            return;
        }
        onSend(text, poll ? pollRequest(poll) : undefined);
        setPoll(null);
        setProblem('');
        // The emoji it holds count toward the member's most used, once the
        // list knows Unicode's (M15.2).
        const sent = text;
        const counted = picked.current;
        picked.current = new Map();
        const byName = emoji?.on ? emoji.byName : null;
        loadEmoji().then((l) => noteSent(sent, denID, byName, counted, Date.now(), l), () => {});
        setText('');
        setCompleting(null);
        setSharing(false);
        lastTyping.current = 0;
    }
    function input(value, at) {
        setText(value);
        complete(value, at);
        const now = Date.now();
        if (value.trim() && now - lastTyping.current > TYPING_EVERY) {
            lastTyping.current = now;
            onTyping();
        }
    }
    // complete finds what the caret at at completes in value, and what that
    // offers, which needs the list of Unicode's emoji for a name.
    function complete(value, at) {
        const found = at == null ? null : completionAt(value, at, mentionable);
        if (!found || dismissed.current === value) {
            setCompleting(null);
            return;
        }
        dismissed.current = null;
        let items;
        if (found.kind === 'member') {
            items = completeMention(found.query, mentionable, everyone);
        } else {
            const l = emojiList();
            if (!l) loadEmoji().then(() => box.current && complete(box.current.value, box.current.selectionStart), () => {});
            items = completeEmoji(found.query, den, l);
        }
        setCompleting(items.length ? { ...found, items, index: 0 } : null);
    }
    // putIn puts an emoji or a mention in place of from to to in the text,
    // and the caret after it, keeping the box's focus.
    function putIn(from, to, what) {
        const value = text.slice(0, from) + what + text.slice(to);
        caret.current = from + what.length;
        setText(value);
        setCompleting(null);
        box.current?.focus();
    }
    // used counts a use of an emoji the picker or completion put in.
    function used(item) {
        const key = item.den ? denKey(denID, item.den.id) : unicodeKey(item.unicode.emoji);
        noteUses([key]);
        picked.current.set(key, (picked.current.get(key) || 0) + 1);
    }
    // completeWith puts in what a completion offers: a mention or an emoji,
    // and a space after it.
    function completeWith(item) {
        const c = completing;
        if (!c) return;
        const end = c.start + 1 + c.query.length;
        if (item.everyone) return putIn(c.start, end, '@everyone ');
        if (item.member) return putIn(c.start, end, `@${item.member.username} `);
        used(item);
        putIn(c.start, end, `${item.den ? `:${item.den.name}:` : inTone(item.unicode, skinTone())} `);
    }
    // pickEmoji puts an emoji picked in the picker in at the caret.
    function pickEmoji(item, what) {
        used(item);
        const el = box.current;
        refocus.current = true;
        putIn(el ? el.selectionStart : text.length, el ? el.selectionEnd : text.length, what);
    }
    useLayoutEffect(() => {
        if (caret.current == null || !box.current) return;
        box.current.setSelectionRange(caret.current, caret.current);
        caret.current = null;
    }, [text]);
    useEffect(() => {
        if (picking || !refocus.current) return;
        refocus.current = false;
        box.current?.focus();
    }, [picking]);
    function attach() {
        setAdding(false);
        picker.current?.click();
    }
    function share() {
        setAdding(false);
        setSharing(true);
    }
    function startPoll() {
        setAdding(false);
        setPoll(newDraft());
        setProblem('');
        box.current?.focus();
    }
    const typers = typingLine(typing.map((m) => m.display_name));
    // While a touch screen's keyboard is up, the box stops growing sooner,
    // so the list keeps room above it.
    const lines = Math.min(touch && focused ? 5 : 8, text.split('\n').length);
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
            {/* What stands above the box scrolls within a cap on a touch
                screen, so its keyboard leaves the list room. */}
            <div class="pointer-coarse:max-h-[30vh] pointer-coarse:overflow-y-auto">
                {replyTo && (
                    <div class="mb-1 flex cursor-default select-none items-center gap-2 text-xs text-base-content/70">
                        <span class="min-w-0 truncate">
                            Replying to <span class="font-medium">{name(replyAuthor)}</span>: <Quoted text={replyTo.text} locked={replyTo.locked} />
                        </span>
                        <button type="button" class="btn btn-ghost btn-xs relative touch-room" onClick={onCancelReply} aria-label="Cancel reply">✕</button>
                    </div>
                )}
                {files.length > 0 && (
                    <ul class="mb-2 flex flex-wrap gap-2" aria-label="Files to send">
                        {files.map((f) => (
                            <FileChip key={f.key} denID={denID} f={f} limits={limits} canSpoil={canSpoil} onSpoiler={(on) => onSpoiler(f.key, on)}
                                onRemove={() => onRemoveFile(f.key)} onCompare={() => onCompare(f.key)} />
                        ))}
                    </ul>
                )}
                {poll && (
                    <PollEditor draft={poll} problem={problem} onRemove={() => setPoll(null)} onChange={(d) => {
                        setPoll(d);
                        setProblem('');
                    }} />
                )}
                {sharing ? (
                    <div class="mb-2 flex flex-col gap-1 rounded border border-base-300 p-2">
                        <div class="flex items-center justify-between gap-2 text-xs text-base-content/70">
                            <span>Who else can edit this message? They can also tick its boxes: lines that start with [ ].</span>
                            <button type="button" class="btn btn-ghost btn-xs relative touch-room" onClick={() => setSharing(false)}>Done</button>
                        </div>
                        <EditorList candidates={candidates} selected={editors} onChange={onEditors} />
                    </div>
                ) : editors.length > 0 && (
                    <div class="mb-1 flex cursor-default select-none items-center gap-2 text-xs text-base-content/70">
                        <span class="min-w-0 truncate">{joinNames(editors.map((id) => name(candidates.find((c) => c.id === id))))} can edit this too</span>
                        <button type="button" class="btn btn-ghost btn-xs relative touch-room" onClick={() => setSharing(true)}>Change</button>
                    </div>
                )}
            </div>
            <div class="flex items-end gap-2">
                <div class="relative" data-compose-menu>
                    <button type="button" class={`btn btn-ghost btn-field btn-square h-12 w-12 ${adding ? 'btn-active' : ''}`} aria-label="Add to this message"
                        title="Add to this message" aria-haspopup="menu" aria-expanded={adding} onClick={() => setAdding(!adding)}>
                        <svg viewBox="0 0 16 16" class="h-5 w-5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true">
                            <path d="M8 3v10M3 8h10" />
                        </svg>
                    </button>
                    {adding && !narrow && (
                        <ul role="menu" class="menu absolute bottom-full left-0 z-20 mb-1 w-56 rounded-box bg-base-100 p-1 shadow">
                            <li role="none">
                                <button type="button" role="menuitem" class="pointer-coarse:min-h-11" disabled={files.length >= MAX_ATTACHMENTS || !!poll} onClick={attach}>
                                    <AttachIcon />
                                    Attach files
                                </button>
                            </li>
                            {shares && (
                                <li role="none">
                                    <button type="button" role="menuitem" class={`pointer-coarse:min-h-11 ${editors.length ? 'text-primary' : ''}`} disabled={!!poll} onClick={share}>
                                        <EditorsIcon />
                                        Let others edit
                                    </button>
                                </li>
                            )}
                            {polls && (
                                <li role="none">
                                    <button type="button" role="menuitem" class={`pointer-coarse:min-h-11 ${poll ? 'text-primary' : ''}`}
                                        disabled={files.length > 0 || editors.length > 0 || !!poll} onClick={startPoll}>
                                        <PollIcon />
                                        Poll
                                    </button>
                                </li>
                            )}
                        </ul>
                    )}
                </div>
                {adding && narrow && (
                    <Sheet label="Add to this message" onClose={() => setAdding(false)}>
                        <SheetRow disabled={files.length >= MAX_ATTACHMENTS || !!poll} onClick={attach}>
                            <AttachIcon />
                            Attach files
                        </SheetRow>
                        {shares && (
                            <SheetRow disabled={!!poll} onClick={share}>
                                <EditorsIcon />
                                <span class={editors.length ? 'text-primary' : ''}>Let others edit</span>
                            </SheetRow>
                        )}
                        {polls && (
                            <SheetRow disabled={files.length > 0 || editors.length > 0 || !!poll} onClick={startPoll}>
                                <PollIcon />
                                <span class={poll ? 'text-primary' : ''}>Poll</span>
                            </SheetRow>
                        )}
                    </Sheet>
                )}
                <input ref={picker} type="file" multiple class="hidden" onChange={(e) => {
                    onAddFiles(e.currentTarget.files);
                    e.currentTarget.value = '';
                }} />
                <div class="relative min-w-0 flex-1">
                    {completing && (
                        <Completions items={completing.items} index={completing.index} tone={skinTone()} onPick={completeWith}
                            onIndex={(index) => setCompleting((c) => c && { ...c, index })} />
                    )}
                    {picking && (
                        <EmojiPicker emoji={emoji} sheet={narrow} onPick={pickEmoji} onClose={() => setPicking(false)} />
                    )}
                    <textarea
                        ref={box}
                        class="textarea focus-border h-auto min-h-12 w-full resize-none bg-transparent py-3 pr-16"
                        rows={lines}
                        placeholder={poll ? 'Ask your question' : placeholder}
                        enterkeyhint={touch ? 'enter' : 'send'}
                        value={text}
                        onFocus={() => setFocused(true)}
                        onBlur={() => {
                            setFocused(false);
                            setCompleting(null);
                        }}
                        onClick={(e) => complete(e.currentTarget.value, e.currentTarget.selectionStart)}
                        onKeyUp={(e) => {
                            if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) complete(e.currentTarget.value, e.currentTarget.selectionStart);
                        }}
                        onPaste={(e) => {
                            if (e.clipboardData?.files?.length) {
                                e.preventDefault();
                                onAddFiles(e.clipboardData.files);
                            }
                        }}
                        onInput={(e) => input(e.currentTarget.value, e.currentTarget.selectionStart)}
                        onKeyDown={(e) => {
                            // An open completion takes the arrows, Enter and Tab,
                            // and Escape closes it until the text changes.
                            if (completing && !e.isComposing) {
                                const n = completing.items.length;
                                if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                                    e.preventDefault();
                                    const step = e.key === 'ArrowDown' ? 1 : -1;
                                    setCompleting((c) => c && { ...c, index: (c.index + step + n) % n });
                                    return;
                                }
                                if ((e.key === 'Enter' && !e.shiftKey) || e.key === 'Tab') {
                                    e.preventDefault();
                                    completeWith(completing.items[completing.index]);
                                    return;
                                }
                                if (e.key === 'Escape') {
                                    e.preventDefault();
                                    e.stopPropagation();
                                    dismissed.current = text;
                                    setCompleting(null);
                                    return;
                                }
                            }
                            if (sends(e, touch)) {
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
                    {/* The emoji picker and the guide to writing sit in the box's
                        corner, by the last line. */}
                    <button type="button" data-emoji-button
                        class={`absolute bottom-3 right-9 flex h-6 w-6 items-center justify-center rounded-full hover:text-base-content touch-room ${picking ? 'text-base-content' : 'text-base-content/50'}`}
                        aria-label="Emoji" title="Emoji" aria-haspopup="dialog" aria-expanded={picking}
                        onClick={() => {
                            setCompleting(null);
                            setPicking(!picking);
                        }}>
                        <SmileyIcon size="h-5 w-5" />
                    </button>
                    <button type="button"
                        class="absolute bottom-3 right-2 flex h-6 w-6 items-center justify-center rounded-full text-base-content/50 hover:text-base-content touch-room"
                        aria-label="How to write a message" title="How to write a message" onClick={() => setGuide(true)}>
                        <span class="flex h-4 w-4 items-center justify-center rounded-full border border-current text-[10px] leading-none" aria-hidden="true">?</span>
                    </button>
                </div>
                {/* A touch screen's keyboard has no Shift, so ↵ starts a line
                    and this sends; pressing it leaves the keyboard up. */}
                {touch && something && (
                    <button type="button" class="btn btn-primary btn-square h-12 w-12" aria-label="Send"
                        disabled={length > MAX_TEXT || uploading || failed}
                        onPointerDown={(e) => e.preventDefault()} onMouseDown={(e) => e.preventDefault()} onClick={submit}>
                        <SendIcon />
                    </button>
                )}
            </div>
            {guide && <WritingGuide touch={touch} onClose={() => setGuide(false)} />}
        </div>
    );
}

function AttachIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-4 w-4 shrink-0" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
            <path d="M13.5 7.5l-5.8 5.8a3.5 3.5 0 01-5-5l6-6a2.3 2.3 0 013.3 3.3l-6 6a1.2 1.2 0 01-1.7-1.7l5.5-5.5" />
        </svg>
    );
}

// PollIcon is three bars of a poll's choices, of different lengths.
function PollIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-4 w-4 shrink-0" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true">
            <path d="M2.5 4h7M2.5 8h11M2.5 12h4.5" />
        </svg>
    );
}

function EditorsIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-4 w-4 shrink-0" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
            <circle cx="6" cy="5" r="2.5" />
            <path d="M1.5 14c0-2.5 2-4.5 4.5-4.5s4.5 2 4.5 4.5" />
            <circle cx="11.5" cy="5.5" r="2" />
            <path d="M11 9.6c2 .2 3.5 1.9 3.5 4" />
        </svg>
    );
}

// SendIcon is a paper plane, flying right.
function SendIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-5 w-5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" aria-hidden="true">
            <path d="M2 2.5l12 5.5-12 5.5 2-5.5z" />
            <path d="M4 8h5" stroke-linecap="round" />
        </svg>
    );
}

// WritingGuide explains how a message is written: sending, the markdown
// subset with each example rendered as the list shows it, mentions, links
// and links to messages, task lines, editing together and files. A touch
// screen gets its own words for sending, and for the menu its long press
// opens (M13.2).
function WritingGuide({ touch, onClose }) {
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
                    {touch ? (
                        <p>The button beside the box sends, once there's something to send, and ↵ on the keyboard starts a new line.</p>
                    ) : (
                        <>
                            <p>
                                {kbd('Enter')} sends. {kbd('Shift')}+{kbd('Enter')} starts a new line, as {kbd('Enter')} does inside a code block,
                                and {kbd('Ctrl')}+{kbd('Enter')} sends from anywhere.
                            </p>
                            <p>In an empty box, {kbd('↑')} edits your last message, and {kbd('Esc')} drops a reply you started.</p>
                        </>
                    )}
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
                    <h3 class="font-semibold">Emoji</h3>
                    <p>
                        The smiley in the box's corner picks one, with the ones you use most first. The den's own emoji go in by name, as :name:,
                        which everyone sees as the emoji, and as text inside code or a link.
                    </p>
                    <p>
                        A colon and two letters, as :th, lists the emoji that match, and in a channel @ lists who can see it.{' '}
                        {touch ? 'Tap one to put it in.' : <>{kbd('↑')} and {kbd('↓')} choose, {kbd('Enter')} or {kbd('Tab')} puts it in, and {kbd('Esc')} closes the list.</>}
                    </p>
                    <p>A message of nothing but emoji, ten at most, shows them large.</p>
                </section>
                <section class="flex flex-col gap-1">
                    <h3 class="font-semibold">Mentions and links</h3>
                    <p>@ and a username lets them know, if they can see the channel. Inside code or a link it's only text.</p>
                    <p>
                        Staff, the moderators and the owner, write @everyone to let everyone who can see the channel know. Anyone can turn
                        those off in the settings, under Notifications, or mute the channel.
                    </p>
                    <p>
                        A link shows where it goes, as you paste it; there's no way to give one a label. When it's sent, Dens takes its tracking
                        out, and writes YouTube, Reddit, X and Amazon links in a short form.
                    </p>
                    <p>
                        Copy link, in {touch ? 'the menu a long press on a message opens' : "a message's ⋯ menu"}, copies a link to that message.
                        Pasted in a message, in this den or another, it shows the channel it's in, and opens the message for anyone who can see it.
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
                        their location and camera details before they leave this device.
                    </p>
                    <p>Let others edit: up to {MAX_EDITORS} people who can see the channel can edit the message too, but not delete it.</p>
                    <p>
                        Poll, in a channel: the message is the question, with 2 to 10 choices. You pick how long it runs, whether anyone sees
                        who voted for what, and whether the totals show as votes come in or only to those who voted, whose votes are then
                        final. A poll can't be edited once it's sent, but you can end it early.
                    </p>
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
// computer, and says what that can be on hover, focus or a tap.
function MetadataRemoved({ id }) {
    return (
        <Tip id={id} label="Metadata removed" trigger={
            <svg viewBox="0 0 16 16" class="h-3.5 w-3.5 text-success" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
                stroke-linejoin="round" aria-hidden="true">
                <circle cx="8" cy="8" r="6.5" />
                <path d="M5.2 8.3l1.9 1.9 3.8-4.1" />
            </svg>
        }>
            Metadata was removed from this file. That can include where and when it was made, and the phone or camera that made it.
        </Tip>
    );
}

// spoilable is a file waiting to go that may go as a spoiler: a photo or a
// video (M17.2).
export function spoilable(f) {
    const type = f.result?.type || f.file?.type || '';
    return type.startsWith('image/') || type.startsWith('video/');
}

// FileChip is a file waiting to go with the message: its preview or icon,
// how far its upload is, the size it will send, whether its metadata came
// out, and whether a photo browsers can't show was turned into one they
// can, which then shows the preview the upload has. A photo or video with
// two versions says which one it goes as, and its size opens the
// comparison (M5). While a video's copy is made, by the local service or
// this browser, it says how far that is, as the upload's socket tells it
// (following), and offers to send the video full size instead when the den
// takes it (M5.4, M5.5). A photo or video has Spoiler, where the message
// may carry one, canSpoil, which blurs its preview here as it'll show
// covered (M17.2).
function FileChip({ denID, f, limits, canSpoil, onSpoiler, onRemove, onCompare }) {
    const done = !!f.result;
    const name = f.result?.name || f.name;
    const versions = f.result?.versions;
    // Once the file is with the local service, it may still be making a
    // smaller copy, or uploading, or switching versions.
    const busy = !done ? f.progress >= 1 : f.switching;
    const following = !done && !f.error ? f.following : null;
    const counting = progressLabel(following);
    const [askedFull, setAskedFull] = useState(false);
    const [fullError, setFullError] = useState('');
    const fullFits = !limits?.file_size || f.size <= limits.file_size;
    function sendFull() {
        setAskedFull(true);
        setFullError('');
        api.post(`/api/dens/${denID}/progress/${f.progressKey}/full`, {}).catch((e) => {
            setAskedFull(false);
            setFullError(e.message);
        });
    }
    return (
        <li class={`flex w-60 max-w-full items-center gap-2 rounded border bg-base-200 p-1.5 ${f.error ? 'border-error' : 'border-base-300'}`}>
            <span class={`shrink-0 overflow-hidden rounded ${f.spoiler ? '[&>*]:blur-sm' : ''}`}>
                {isImageFile(f.file) ? (
                    <Thumb file={f.file} size={40} />
                ) : f.result?.thumb ? (
                    <img src={thumbURL(denID, f.result.id)} alt="" class="h-10 w-10 shrink-0 rounded bg-base-300 object-cover" draggable={false} />
                ) : (
                    <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded bg-base-300 text-xs">FILE</span>
                )}
            </span>
            <div class="min-w-0 flex-1 text-xs">
                <p class="truncate font-medium" title={name}>{name}</p>
                {f.error ? (
                    <p class="text-error" role="alert">{f.error}</p>
                ) : done ? (
                    <>
                        <p class="flex items-center gap-1 text-base-content/60">
                            {versions ? (
                                <button type="button" class="link inline-flex min-h-6 cursor-pointer items-center whitespace-nowrap text-base-content/60" onClick={onCompare}
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
                ) : busy && counting ? (
                    <>
                        <p class="truncate text-base-content/60">{counting}</p>
                        <progress class="progress progress-primary h-1.5 w-full" value={Math.round(following.done * 100)} max="100"></progress>
                        {following.stage === 'copying' && fullFits && !askedFull && (
                            <button type="button" class="link min-h-6 cursor-pointer text-base-content/60" onClick={sendFull}>
                                Send full size instead
                            </button>
                        )}
                        {fullError && <p class="text-error" role="alert">{fullError}</p>}
                    </>
                ) : busy ? (
                    <progress class="progress progress-primary h-1.5 w-full"></progress>
                ) : (
                    <progress class="progress progress-primary h-1.5 w-full" value={Math.round(f.progress * 100)} max="100"></progress>
                )}
                {canSpoil && !f.error && spoilable(f) && (
                    <button type="button" class={`btn btn-xs mt-0.5 ${f.spoiler ? 'btn-active' : 'btn-ghost'}`} aria-pressed={!!f.spoiler}
                        title="Covered until it's clicked" onClick={() => onSpoiler(!f.spoiler)} data-spoiler-toggle>
                        Spoiler
                    </button>
                )}
            </div>
            <button type="button" class="btn btn-ghost btn-xs btn-square relative touch-room" onClick={onRemove} aria-label={`Remove ${f.name}`}>✕</button>
        </li>
    );
}
