// A den's chat: the sidebar of channels and DMs, the open channel, and the
// member list. Below 768 pixels they're three panes in a row, one on screen
// at a time (M13.1): the list, the channel, and the members or a DM's
// profile, which the browser's own scrolling swipes between, snapping to
// each, and the page's history walks.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { HoverRow } from './animations.js';
import { Avatar, DenContext } from './avatar.jsx';
import { EmojiContext, useDenEmoji } from './denemoji.jsx';
import { onConnection, onEvent, sendTyping, showChannel } from './events.js';
import { openManage } from './dialogs.js';
import { Tip, useNotifications } from './components.jsx';
import { BellIcon, CogIcon, HashIcon, ICON, LockIcon, ShieldIcon, SpeakerIcon } from './icons.jsx';
import { compareIds, unread } from './ids.js';
import { Markdown, Preview, firstLine } from './markdown.jsx';
import { MessagePane, openAt } from './messages.jsx';
import { LinkContext } from './msglinks.jsx';
import { Name } from './names.jsx';
import { nav, useNarrow } from './nav.js';
import { mutedIn, notifying, withdraw } from './notify.js';
import { showsOffline } from './offline.js';
import { ChannelDialog, GroupDialog } from './manage.jsx';
import { EditProfile, MemberList, ProfileCard, ProfilePanel, RemoveMember, isStaff } from './people.jsx';
import { NO_CALLS, canCall } from './platform.js';
import { dmGate, groupGate, groupTitle, isNotes, isPrivate, notesGate, ownNotes, partnerOf } from './private.js';
import { CodesDialog, SignInRequests, VerifiedMark } from './private.jsx';
import { GroupIcon, GroupPanel, MAX_MEMBERS, PickPeople, RenameGroup } from './groups.jsx';
import { ExpiryBadge } from './disappearing.jsx';
import { NotesIcon, NotesRow } from './notes.jsx';
import { EVENTS, withEvent } from './denevents.js';
import { EventsPane, EventsRow, showEvent } from './denevents.jsx';
import { createPress, useCoarse } from './touch.js';
import { DockedPlayer } from './player.jsx';
import { openSettings } from './settings.jsx';
import { applyCalls, joinCall } from './voice.js';
import { CallLock } from './calllock.jsx';
import { DRAG_CHANNEL, DRAG_GROUP, DRAG_MEMBER, channelDrop, groupDrop, moveTargets } from './drag.js';
import { CallBar, CallBlock, CallMembers, callState, useCall, useCallButton } from './voice.jsx';

const LAST_CHANNEL = 'DENS_LAST_CHANNEL:';
const MEMBERS_OPEN = 'DENS_MEMBERS_OPEN';
const LIST_TAB = 'DENS_LIST_TAB:';
// The text channel last open in each den, which its Den tab opens, and the
// DM, which its Direct messages tab opens (M11).
const LAST_TEXT = 'DENS_LAST_TEXT:';
const LAST_DM = 'DENS_LAST_DM:';
// The groups folded in each den's channel list (M11).
const FOLDED = 'DENS_FOLDED:';
// The address's place for a channel holds this for the Direct messages tab
// with no DM open, as when there are none (M11).
export const NO_DM = 'dms';
// What the den's state dot says, as Home's rows name each state, where the
// state has no error to say why (M13.2).
const STATE_WORDS = { connecting: 'Connecting', offline: 'Offline', revoked: 'Signed out', removed: 'Not a member' };
// A typing notice shows this long unless another arrives; senders repeat
// theirs while they type.
const TYPING_SHOWN = 6000;
// Changes to these reload the den's state rather than being applied here,
// the den's emoji among them (M15.2).
const STRUCTURAL = new Set([
    'den.updated', 'member.joined', 'member.updated', 'member.left', 'channel.created', 'channel.updated', 'channel.deleted',
    'channels.reordered', 'group.created', 'group.updated', 'group.deleted', 'groups.reordered',
    'emoji.created', 'emoji.updated', 'emoji.deleted',
]);

function stored(key) {
    try {
        return localStorage.getItem(key) || '';
    } catch {
        // Storage can be unavailable; defaults stand in.
        return '';
    }
}

function store(key, value) {
    try {
        localStorage.setItem(key, value);
    } catch {
        // As above.
    }
}

const hasMessages = (c) => c && (c.kind === 'text' || isPrivate(c));

// closedDMs is the set of DMs this member closed.
function closedDMs(view) {
    return new Set(view.read_states.filter((r) => r.closed).map((r) => r.channel_id));
}

// listed arranges a den's channels, DMs and group DMs aside, as its
// channel list shows them: those in no group, then each group's, each by
// position.
export function listed(view) {
    const byPosition = (a, b) => a.position - b.position;
    const channels = view.channels.filter((c) => !isPrivate(c));
    return {
        ungrouped: channels.filter((c) => !c.group_id).sort(byPosition),
        groups: [...view.groups].sort(byPosition).map((group) => ({ group, channels: channels.filter((c) => c.group_id === group.id).sort(byPosition) })),
    };
}

// denChannel is the text channel the Den tab opens (M11): the one last
// open in the den, unless it's gone or out of sight, else the list's first.
export function denChannel(view, lastID) {
    const { ungrouped, groups } = listed(view);
    const text = [...ungrouped, ...groups.flatMap((g) => g.channels)].filter((c) => c.kind === 'text');
    return text.find((c) => c.id === lastID) || text[0] || null;
}

// recentDMs is a den's DMs and group DMs (M16.3) as their list shows
// them, the most recently active first: one closed since keeps out until
// it has a new message, unless it's the one open. The member's notes keep
// a place of their own, above them (M17.3).
export function recentDMs(view, reads, open) {
    return view.channels
        .filter((c) => isPrivate(c) && !isNotes(c) && (!reads.get(c.id)?.closed || c.id === open))
        .sort((a, b) => compareIds(reads.get(b.id)?.last_message_id, reads.get(a.id)?.last_message_id) || compareIds(b.id, a.id));
}

// lastDM is the DM the Direct messages tab opens (M11): the one last open
// in the den, unless it's gone or was closed since. Otherwise none, rather
// than one the member didn't pick, whom they might write to unawares.
export function lastDM(view, reads, lastID) {
    const c = view.channels.find((x) => isPrivate(x) && x.id === lastID);
    return c && !reads.get(c.id)?.closed ? c : null;
}

// pick chooses the channel to show: the one in the address, else the last
// one shown in this den unless it's a DM since closed, else the first text
// channel. The Direct messages tab with no DM open shows none, as does a
// den last left there, whose list the phone layout shows at its own
// address.
function pick(view, denID, channelID) {
    if (!view || channelID === NO_DM || channelID === EVENTS) return null;
    const shown = view.channels.find((c) => c.id === channelID);
    if (hasMessages(shown)) return shown;
    const lastID = stored(LAST_CHANNEL + denID);
    if (!channelID && lastID === NO_DM) return null;
    const last = view.channels.find((c) => c.id === lastID);
    if (hasMessages(last) && !closedDMs(view).has(last.id)) return last;
    return denChannel(view, '');
}

// readFolded reads the groups folded in a den's channel list, as the
// browser keeps them.
export function readFolded(text) {
    try {
        const ids = JSON.parse(text || '[]');
        return new Set(Array.isArray(ids) ? ids.filter((id) => typeof id === 'string') : []);
    } catch {
        return new Set();
    }
}

// foldedShows is what a folded group still shows of its channels: the one
// that's open, and voice channels with a call in them.
export function foldedShows(channels, open, calls) {
    return channels.filter((c) => c.id === open || (c.kind === 'voice' && calls.get(c.id)?.members.length > 0));
}

// hiddenMarks is what a folded group's row says of the channels it hides:
// whether any is unread, and how many mentions they hold.
export function hiddenMarks(hidden, reads) {
    let mentions = 0;
    let any = false;
    for (const c of hidden) {
        const r = reads.get(c.id);
        if (c.kind !== 'text' || !r) continue;
        mentions += r.mention_count;
        any ||= unread(r);
    }
    return { unread: any, mentions };
}

// withStatus takes in the den's status on this computer.
function withStatus(view, status) {
    return { ...view, state: status.state, error: status.error, name: status.name, role: status.role, url: status.url };
}

function applyPresence(online, p) {
    const next = new Set(p.full ? [] : online);
    (p.online || []).forEach((id) => next.add(id));
    (p.offline || []).forEach((id) => next.delete(id));
    return next;
}

// withTyper records that a member is typing in a channel until a time, or,
// with until 0, that they stopped.
function withTyper(typing, channel, member, until) {
    const next = new Map(typing);
    const typers = new Map(next.get(channel) || []);
    if (until) typers.set(member, until);
    else typers.delete(member);
    if (typers.size) next.set(channel, typers);
    else next.delete(channel);
    return next;
}

function expireTypers(typing, now) {
    let changed = false;
    const next = new Map();
    for (const [channel, typers] of typing) {
        const kept = new Map([...typers].filter(([, until]) => until > now));
        changed ||= kept.size !== typers.size;
        if (kept.size) next.set(channel, kept);
    }
    return changed ? next : typing;
}

// pane is the pane the page's history shows on the phone layout: list,
// chan or side.
export function Chat({ denID, channelID, pane, navigate }) {
    const [view, setView] = useState(null);
    const [error, setError] = useState('');
    const [dialog, setDialog] = useState(null);
    const narrow = useNarrow();
    // In a wider window the member list starts open where it fits beside
    // the channel, until the member closes it (M8.3); on the phone layout
    // it's a pane of its own.
    const [membersOpen, setMembersOpen] = useState(() => {
        const kept = stored(MEMBERS_OPEN);
        return kept ? kept === '1' : window.matchMedia?.('(min-width: 768px)').matches ?? false;
    });
    const [online, setOnline] = useState(() => new Set());
    const [typing, setTyping] = useState(() => new Map());
    const [visible, setVisible] = useState(document.visibilityState === 'visible');
    const [live, setLive] = useState(true);
    const { call: myCall } = useCall();
    // The call's button floats in the screen's corner (M13.4), where the
    // phone layout's list keeps clear of it.
    const button = useCallButton() && narrow;
    // What the member muted here, which the bells show once they turned
    // notifications on (M12.3).
    const notifications = useNotifications();
    const muted = mutedIn(notifications, denID);
    const bells = notifying(notifications);
    // newDevice is a device that just signed in to this member's account.
    const [newDevice, setNewDevice] = useState(null);
    // status is the den's status on this computer: its sign-ins waiting
    // for approval, and whether this device holds the DM seal. dens are
    // every den's, which links to messages find theirs among (M11).
    const [status, setStatus] = useState(null);
    const [dens, setDens] = useState([]);
    // tab is the list the sidebar shows: the den's channels, or DMs.
    const [tab, setTab] = useState(() => stored(LIST_TAB + denID) || 'den');
    // showing changes as the phone layout's list shows another channel
    // behind it, which pick reads from the browser.
    const [, setShowing] = useState('');
    const reloadTimer = useRef(null);
    // loads counts the view's reloads, so one that lands after a newer one
    // is dropped. caught holds, for each reload out, the events that came
    // while it was: the service may have taken the reload's snapshot before
    // them, so they go again on top of it.
    const loads = useRef(0);
    const caught = useRef(new Set());
    useEffect(() => onConnection(setLive), []);

    async function load() {
        const n = ++loads.current;
        const since = [];
        caught.current.add(since);
        try {
            const next = await api.get(`/api/dens/${denID}/state`);
            if (n === loads.current) {
                let view = next;
                let present = new Set(next.online);
                for (const e of since) {
                    if (e.t === 'voice.state') view = { ...view, calls: applyCalls(view.calls, e.d) };
                    else if (e.t === 'presence') present = applyPresence(present, e.d);
                    else if (e.t === 'dm.key') view = withKey(view, e.d);
                    else if (e.t === 'identities') view = { ...view, identities: e.d };
                    else if (e.t === 'groups') view = { ...view, group_dms: e.d };
                    else if (e.t === 'dens') view = withStatus(view, e.d);
                    else if (e.t.startsWith('event.')) view = { ...view, events: withEvent(view.events, e.t, e.d) };
                }
                setView(view);
                setOnline(present);
                setError('');
            }
        } catch (e) {
            if (n === loads.current) setError(e.message);
        } finally {
            caught.current.delete(since);
        }
        api.get('/api/dens').then((v) => {
            setDens(v.dens);
            setStatus(v.dens.find((d) => d.den_id === denID) || null);
        }, () => {});
    }

    // catchUp keeps an event for the reloads that are out.
    function catchUp(e) {
        caught.current.forEach((since) => since.push(e));
    }

    function reloadSoon() {
        clearTimeout(reloadTimer.current);
        reloadTimer.current = setTimeout(load, 150);
    }

    useEffect(() => {
        load();
        const stop = onEvent((msg) => {
            if (msg.t === 'reconnected') return reloadSoon();
            if (msg.t === 'dens') {
                setDens(msg.d.dens);
                const status = msg.d.dens.find((d) => d.den_id === denID);
                if (status) {
                    catchUp({ t: 'dens', d: status });
                    setView((v) => (v ? withStatus(v, status) : v));
                    setStatus(status);
                }
                return;
            }
            if (msg.t !== 'den' || msg.d.den !== denID) return;
            if (msg.d.reset) return reloadSoon();
            for (const e of msg.d.events || []) {
                if (e.t === 'voice.state' || e.t === 'presence' || e.t === 'dm.key' || e.t === 'identities' || e.t === 'groups' || e.t.startsWith('event.')) catchUp(e);
                if (STRUCTURAL.has(e.t)) reloadSoon();
                else if (e.t === 'presence') setOnline((cur) => applyPresence(cur, e.d));
                else if (e.t === 'voice.state') setView((v) => (v ? { ...v, calls: applyCalls(v.calls, e.d) } : v));
                else if (e.t === 'typing') setTyping((cur) => withTyper(cur, e.d.channel_id, e.d.member_id, Date.now() + TYPING_SHOWN));
                else if (e.t === 'message.created') setTyping((cur) => withTyper(cur, e.d.channel_id, e.d.author_id, 0));
                else if (e.t === 'device.added') setNewDevice(e.d);
                else if (e.t === 'dm.key') setView((v) => (v ? withKey(v, e.d) : v));
                // What this member's Dens kept of identities here (M16.1).
                else if (e.t === 'identities') setView((v) => (v ? { ...v, identities: e.d } : v));
                // The member's groups, as their changes make them (M16.3).
                else if (e.t === 'groups') setView((v) => (v ? { ...v, group_dms: e.d } : v));
                // The den's events (M17.4).
                else if (e.t.startsWith('event.')) setView((v) => (v ? { ...v, events: withEvent(v.events, e.t, e.d) } : v));
            }
            // The local service counts unread messages and mentions.
            if (msg.d.reads) setView((v) => (v ? applyReads(v, msg.d.reads) : v));
        });
        const onVisible = () => setVisible(document.visibilityState === 'visible');
        document.addEventListener('visibilitychange', onVisible);
        return () => {
            stop();
            document.removeEventListener('visibilitychange', onVisible);
            clearTimeout(reloadTimer.current);
            showChannel(denID, '');
        };
    }, [denID]);

    const anyTyping = typing.size > 0;
    useEffect(() => {
        if (!anyTyping) return;
        const tick = setInterval(() => setTyping((cur) => expireTypers(cur, Date.now())), 1000);
        return () => clearInterval(tick);
    }, [anyTyping]);

    const channel = pick(view, denID, channelID);
    // noDM is the Direct messages tab open with no DM, as the address says,
    // or the phone layout's list, at its own address, left it.
    const noDM = channelID === NO_DM || (!channelID && !!view && stored(LAST_CHANNEL + denID) === NO_DM);
    // The den's events show in the channel's place (M17.4), from the Den
    // tab's list, while the den has them.
    const eventsOpen = channelID === EVENTS && !!view?.event_limits;
    // The den sends typing only where this member is looking, and the
    // service notifies them of nothing there (M12.3); this page takes back
    // its own notification of what it shows, which the service may have
    // lost track of, as when the page's stream reconnected.
    const shown = visible && channel ? channel.id : '';
    useEffect(() => {
        showChannel(denID, shown);
        if (shown) withdraw([`${denID}/${shown}`]);
    }, [denID, shown]);
    // Opening a channel or a DM shows the list it's in.
    const openKind = channel?.kind;
    useEffect(() => {
        if (openKind) setTab(isPrivate({ kind: openKind }) ? 'dms' : 'den');
        else if (noDM) setTab('dms');
        else if (eventsOpen) setTab('den');
    }, [channel?.id, channelID, noDM, eventsOpen]);
    // shownID is the channel shown, for the page's history: its ID, NO_DM,
    // EVENTS, or '' for none.
    const shownID = channel ? channel.id : noDM ? NO_DM : eventsOpen ? EVENTS : '';
    // The member's notes (M17.3), which have nobody else to show beside
    // them.
    const notes = isNotes(channel);
    // The members' place: the member list, or beside a DM the other
    // member's profile (M11); with no DM open, the notes or the events,
    // there's none.
    const hasSide = !noDM && !notes && !eventsOpen;
    // at is the pane on screen on the phone layout, '' in a wider window.
    const at = !narrow ? '' : pane === 'side' && hasSide ? 'side' : pane === 'list' || (!pane && !channelID) ? 'list' : 'chan';
    const toPane = (next) => nav().den(denID, shownID, next);
    // A swipe that settles on another pane goes there in the history too.
    const row = usePaneRow(at, !!view, (near) => near !== at && toPane(near));
    // The den's emoji, for every text the den's page shows (M15.2).
    const emoji = useDenEmoji(denID, view?.emoji, view?.emoji_limits);

    if (error && !view) return <div class="p-6"><div role="alert" class="alert alert-error">{error}</div></div>;
    if (!view) return <div class="p-6"><span class="loading loading-spinner"></span></div>;

    if (channel) store(LAST_CHANNEL + denID, channel.id);
    if (channel?.kind === 'text') store(LAST_TEXT + denID, channel.id);
    if (isPrivate(channel)) store(LAST_DM + denID, channel.id);
    const members = new Map(view.members.map((m) => [m.id, m]));
    const reads = new Map(view.read_states.map((r) => [r.channel_id, r]));
    const staff = isStaff(view.role);
    const partner = (c) => members.get(c.members.find((id) => id !== view.me.id));
    const dm = channel?.kind === 'dm' ? partner(channel) : null;
    // A group DM, as its changes make it (M16.3).
    const group = channel?.kind === 'group_dm' ? view.group_dms?.[channel.id] || { members: [], changes: [], syncing: true } : null;
    const title = (c) => groupTitle(view.group_dms?.[c.id], members, view.me.id);
    const gone = view.state === 'removed' || view.state === 'revoked';
    let closed = '';
    if (gone) closed = view.error || 'This den no longer accepts this device.';
    else if (dm?.left_at) closed = `${dm.display_name} is no longer in this den.`;
    const typers = channel
        ? [...(typing.get(channel.id)?.keys() || [])].filter((id) => id !== view.me.id).map((id) => members.get(id)).filter(Boolean)
        : [];
    // What waits elsewhere, for the channels button on the phone layout.
    const elsewhere = { mentions: 0, unread: false };
    for (const c of view.channels) {
        const r = reads.get(c.id);
        if (!hasMessages(c) || c.id === channel?.id || !r) continue;
        elsewhere.mentions += r.mention_count;
        elsewhere.unread ||= unread(r);
    }

    function openChannel(id) {
        nav().den(denID, id, 'chan');
    }

    // showInList shows a channel behind the phone layout's list, which
    // keeps its own address, as the list would have it in a wider window.
    function showInList(id) {
        store(LAST_CHANNEL + denID, id);
        setShowing(id);
    }

    // pickTab shows a tab's list, and opens what's in it, so a den channel
    // never sits open beside the DM list, nor a DM beside the channels:
    // nobody writes in a channel thinking it's a DM (M11). Each opens what
    // was last open in it, so going to the den to copy something and back
    // returns to the same DM; Direct messages opens none when there's no
    // DM to return to. The den's events, open in the Den tab, stay open as
    // it's picked again (M17.4). The phone layout's list stays on screen,
    // with what the tab opens behind it.
    function pickTab(next) {
        store(LIST_TAB + denID, next);
        setTab(next);
        let to = null;
        if (next === 'den' && (isPrivate(channel) || (!channel && !eventsOpen))) to = denChannel(view, stored(LAST_TEXT + denID))?.id || '';
        else if (next === 'dms' && !isPrivate(channel)) to = lastDM(view, reads, stored(LAST_DM + denID))?.id || NO_DM;
        if (to === null) return;
        if (at === 'list') showInList(to);
        else nav().den(denID, to, 'chan');
    }

    function toggleMembers() {
        if (narrow) return toPane(at === 'side' ? 'chan' : 'side');
        store(MEMBERS_OPEN, membersOpen ? '0' : '1');
        setMembersOpen(!membersOpen);
    }

    async function message(member) {
        const opened = await api.post(`/api/dens/${denID}/dms`, { member_id: member.id });
        await load();
        setDialog(null);
        openChannel(opened.id);
    }

    // openNotes opens the member's notes, which the den makes the first
    // time (M17.3).
    async function openNotes() {
        let mine = ownNotes(view);
        if (!mine) {
            mine = await api.post(`/api/dens/${denID}/notes`);
            await load();
        }
        setDialog(null);
        openChannel(mine.id);
    }

    // makeGroup makes a group DM of this member and those picked, and opens
    // it (M16.3).
    async function makeGroup(picked, name) {
        const made = await api.post(`/api/dens/${denID}/group-dms`, { members: picked, name });
        await load();
        setDialog(null);
        openChannel(made.id);
    }

    // closeDM closes a DM; closing the one open opens no other in its place,
    // as the tab wouldn't (M11).
    async function closeDM(c) {
        await api.post(`/api/dens/${denID}/dms/${c.id}/close`);
        if (c.id !== channel?.id) return;
        if (at === 'list') showInList(NO_DM);
        else nav().den(denID, NO_DM, 'chan');
    }

    async function forget() {
        await api.del(`/api/dens/${denID}`);
        navigate('/');
    }

    // mute mutes the den's notifications, or a channel's, or unmutes them;
    // the service's status says so to every page.
    function mute(channel, on) {
        api.post(`/api/dens/${denID}/notifications`, { channel, muted: on }).catch(() => {});
    }

    // A link to a message opens it here, or in the den it points into (M11),
    // and a link to an event opens it in its den's events (M17.5).
    const links = {
        denID, view, dens,
        open: (den, channelID, messageID) => {
            openAt(den, channelID, messageID);
            navigate(`/den/${den}/${channelID}`);
        },
        openEvent: (den, eventID) => {
            showEvent(den, eventID);
            navigate(`/den/${den}/${EVENTS}`);
        },
    };
    // On the phone layout the call shows across the top of the channel's
    // pane, under its header, with the shares the member watches docked
    // under it (M13.4); a wider window has them beside the channel.
    const callTop = narrow && (
        <>
            <CallBar denID={denID} />
            <DockedPlayer />
        </>
    );
    // What waits in the other dens, for the list's Home button.
    const away = { mentions: 0, unread: false };
    for (const d of dens) {
        if (d.den_id === denID) continue;
        away.mentions += d.mentions || 0;
        away.unread ||= d.unread > 0;
    }

    return (
        <DenContext.Provider value={denID}>
            <EmojiContext.Provider value={emoji}>
            <LinkContext.Provider value={links}>
                {/* On the phone layout a row that scrolls sideways, snapping to
                    each pane; the side panes take 85% of it, so the channel shows
                    at their edge, dimmed, and a tap there goes back to it. Panes
                    off screen are inert. */}
                <div ref={row} class="relative flex h-full min-h-0 snap-x snap-mandatory overflow-x-auto overscroll-x-contain [scrollbar-width:none] md:snap-none md:overflow-x-visible [&::-webkit-scrollbar]:hidden"
                    onClick={(e) => at && at !== 'chan' && e.target === e.currentTarget && toPane('chan')}>
                    <aside data-pane="list" inert={!!at && at !== 'list'} class="flex w-[85%] shrink-0 snap-start flex-col overflow-y-auto bg-base-200 md:w-64">
                        <div class="border-b border-base-300 p-3">
                            <div class="flex items-center gap-2">
                                <a href="/" class="btn btn-ghost btn-sm btn-square relative -my-1 -ml-1" title="Home"
                                    aria-label={away.mentions ? `Home, ${away.mentions} unread mentions in other dens` : away.unread ? 'Home, unread in other dens' : 'Home'}
                                    onClick={(e) => { e.preventDefault(); navigate('/'); }}>
                                    <HomeIcon />
                                    <Marks mentions={away.mentions} unread={away.unread} />
                                </a>
                                {/* The den's name opens its Manage, as a server's name
                                    opens its menu elsewhere; on the phone layout, at
                                    the list of its sections (M14.3). */}
                                <button type="button" class="-my-1 flex min-h-8 min-w-0 flex-1 cursor-pointer items-center rounded px-1 text-left font-semibold hover:bg-base-300/60"
                                    aria-label={`Manage ${view.name}`} title="Manage this den" data-den-manage onClick={() => openManage(denID)}>
                                    <span class="truncate">{view.name}</span>
                                </button>
                                {bells && (
                                    <button type="button" class="btn btn-ghost btn-xs btn-square relative -my-1 touch-room" aria-pressed={muted.den}
                                        aria-label={muted.den ? `Unmute ${view.name}` : `Mute ${view.name}`}
                                        title={muted.den ? "Unmute the den's notifications" : "Mute the den's notifications"}
                                        onClick={() => mute('', !muted.den)}>
                                        <BellIcon off={muted.den} />
                                    </button>
                                )}
                                {!(live && view.state === 'connected') && (
                                    <Tip id={`state-${denID}`} label="The den's connection" trigger={<span class="badge badge-xs badge-warning"></span>}>
                                        {live ? view.error || STATE_WORDS[view.state] || view.state : 'Not in touch with Dens on this device.'}
                                    </Tip>
                                )}
                            </div>
                            {live && view.state !== 'connected' && <p class="text-xs text-warning">{view.error || 'Reconnecting…'}</p>}
                        </div>
                        <ListTabs tab={tab} onTab={pickTab} view={view} reads={reads} open={channel?.id} />
                        {tab === 'dms' ? (
                            <>
                                {view.has_group_dms && !gone && (
                                    <div class="flex px-2 pt-2">
                                        <button type="button" class="btn btn-ghost btn-xs" data-new-group onClick={() => setDialog({ kind: 'new-group' })}>+ New group</button>
                                    </div>
                                )}
                                <DMList dms={recentDMs(view, reads, channel?.id)} reads={reads} open={channel?.id} partner={partner} online={online}
                                    identities={view.identities || {}} muted={bells ? muted.channels : null} title={title} onOpen={openChannel} onClose={closeDM}
                                    notes={view.has_notes && (ownNotes(view) || !gone) ? { open: notes } : null} onNotes={openNotes} />
                            </>
                        ) : (
                            <ChannelList denID={denID} view={view} reads={reads} members={members} open={channel?.id} staff={staff} gone={gone}
                                muted={bells ? muted.channels : null} onOpen={openChannel} onDialog={setDialog}
                                events={view.event_limits ? { open: eventsOpen, onOpen: () => openChannel(EVENTS) } : null} />
                        )}
                        <div class="mt-auto">
                            {/* Room for the call's button over the list's end, so
                                the last row clears it. */}
                            {button && <div class="h-11" aria-hidden="true"></div>}
                            {/* Channels and their groups are the Den tab's. */}
                            {staff && !gone && tab !== 'dms' && (
                                <div class="flex gap-1 px-2 pb-1">
                                    <button type="button" class="btn btn-ghost btn-xs" onClick={() => setDialog({ kind: 'channel' })}>+ Channel</button>
                                    <button type="button" class="btn btn-ghost btn-xs" onClick={() => setDialog({ kind: 'group' })}>+ Group</button>
                                </div>
                            )}
                            <CallBlock denID={denID} staffMuted={staffMuted(view, myCall)} roomy={button} />
                            <UserPanel me={view.me} online={live && view.state === 'connected'} onEdit={() => setDialog({ kind: 'edit' })} />
                        </div>
                    </aside>
                    <section data-pane="chan" inert={!!at && at !== 'chan'}
                        class="relative flex w-full min-w-0 shrink-0 snap-start snap-always flex-col md:w-auto md:flex-1 md:shrink">
                        {gone && (
                            <div role="status" class="alert alert-warning alert-soft m-2 flex flex-wrap">
                                <span>{closed}</span>
                                {view.state === 'revoked' && (
                                    <button type="button" class="btn btn-primary btn-sm" onClick={() => navigate('/')}>Sign in again</button>
                                )}
                                <button type="button" class="btn btn-sm" onClick={forget}>Remove from this device</button>
                            </div>
                        )}
                        {status && !gone && <div class="mx-2"><SignInRequests den={status} /></div>}
                        {newDevice && !gone && (
                            <div role="status" class="alert alert-info alert-soft m-2 flex flex-wrap">
                                <span>
                                    A new device signed in to your account here: <span class="font-medium">{newDevice.label}</span>. If it wasn't
                                    you, change your den password, which signs out every other device.
                                </span>
                                <span class="flex gap-2">
                                    <button type="button" class="btn btn-sm" onClick={() => openManage(denID, 'password')}>Change password</button>
                                    <button type="button" class="btn btn-ghost btn-sm" onClick={() => setNewDevice(null)}>Dismiss</button>
                                </span>
                            </div>
                        )}
                        {channel ? (
                            <>
                                <ChannelHeader key={channel.id} channel={channel} dm={dm} groupTitle={group ? title(channel) : ''} me={view.me} elsewhere={elsewhere}
                                    verified={!!(dm && view.identities?.[dm.id]?.verified)} onCompare={() => dm && setDialog({ kind: 'codes', member: dm })}
                                    online={dm && online.has(dm.id)} membersOpen={!at && membersOpen}
                                    muted={bells ? muted.channels.has(channel.id) : null} onMute={(on) => mute(channel.id, on)}
                                    onChannels={() => toPane('list')} onMembers={toggleMembers}
                                    onProfile={(m) => setDialog({ kind: 'profile', member: m })} />
                                {callTop}
                                <MessagePane
                                    key={`${denID}:${channel.id}`}
                                    denID={denID}
                                    denURL={view.url}
                                    channel={channel}
                                    me={view.me}
                                    members={members}
                                    readPosition={reads.get(channel.id)?.message_id}
                                    lastMessage={reads.get(channel.id)?.last_message_id}
                                    role={view.role}
                                    dm={dm}
                                    group={group}
                                    groupTitle={group ? title(channel) : ''}
                                    keys={isPrivate(channel) ? (view.dm_keys || []).filter((k) => k.channel_id === channel.id) : undefined}
                                    gate={channel.kind === 'dm' ? dmGate(view, channel) : group ? groupGate(view, channel) : notes ? notesGate(view) : ''}
                                    changes={channel.kind === 'dm' ? view.identities?.[partnerOf(channel, view.me.id)]?.changes : undefined}
                                    identities={view.identities}
                                    onCompare={(m) => setDialog({ kind: 'codes', member: m })}
                                    typing={typers}
                                    closed={closed}
                                    limits={view.limits}
                                    retention={view.retention}
                                    offline={showsOffline(live, view.state)}
                                    spoilers={!!view.spoilers}
                                    onProfile={(m) => setDialog({ kind: 'profile', member: m })}
                                    onTyping={notes ? () => {} : () => sendTyping(denID, channel.id)}
                                />
                            </>
                        ) : noDM ? (
                            <>
                                {callTop}
                                <div class="flex flex-col items-start p-6">
                                    <button type="button" class="btn btn-ghost btn-sm md:hidden" onClick={() => toPane('list')}>Direct messages</button>
                                </div>
                            </>
                        ) : eventsOpen ? (
                            <>
                                {callTop}
                                <EventsPane denID={denID} view={view} members={members} onList={() => toPane('list')}
                                    onProfile={(m) => setDialog({ kind: 'profile', member: m })} />
                            </>
                        ) : (
                            <>
                                {callTop}
                                <div class="flex flex-col items-start gap-2 p-6 text-base-content/70">
                                    <button type="button" class="btn btn-ghost btn-sm md:hidden" onClick={() => toPane('list')}>Channels</button>
                                    <p>{staff ? 'This den has no text channels yet. Create one to start talking.' : 'This den has no text channels yet.'}</p>
                                </div>
                            </>
                        )}
                        {at && at !== 'chan' && <div class="pointer-events-none absolute inset-0 z-30 bg-black/40"></div>}
                    </section>
                    {/* Beside a DM, the member list's place shows the other member's
                        profile (M11); with no DM open, it shows nothing. */}
                    {hasSide && (at || membersOpen) && (
                        <aside data-pane="side" inert={!!at && at !== 'side'} class="flex w-[85%] shrink-0 snap-end flex-col overflow-y-auto bg-base-200 md:w-60">
                            <div class="flex items-center justify-between border-b border-base-300 p-3">
                                <span class="font-semibold">{channel?.kind === 'dm' ? 'Profile' : group ? 'Group' : 'Members'}</span>
                                <button type="button" class="btn btn-ghost btn-sm md:hidden" onClick={() => toPane('chan')}
                                    aria-label={channel?.kind === 'dm' ? 'Close the profile' : group ? "Close the group's members" : 'Close the member list'}>✕</button>
                            </div>
                            {channel?.kind === 'dm' ? (
                                dm && <ProfilePanel key={dm.id} denID={denID} member={dm} me={view.me} online={online.has(dm.id)}
                                    kept={view.keeps_identities ? view.identities?.[dm.id] || {} : null} onCompare={() => setDialog({ kind: 'codes', member: dm })}
                                    onAddPeople={view.has_group_dms && !dm.left_at && !gone ? () => setDialog({ kind: 'new-group', with: dm.id }) : null}
                                    expiry={view.disappearing && !dm.left_at && !gone ? channel : null} />
                            ) : group ? (
                                <GroupPanel key={channel.id} denID={denID} view={view} channel={channel} group={group} title={title(channel)} members={members}
                                    online={online} onCompare={(m) => setDialog({ kind: 'codes', member: m })} onProfile={(m) => setDialog({ kind: 'profile', member: m })}
                                    onAdd={() => setDialog({ kind: 'add-to-group' })} onRename={() => setDialog({ kind: 'rename-group' })}
                                    expiry={view.disappearing && !gone ? channel : null} />
                            ) : (
                                <MemberList denID={denID} members={view.members} online={online} onProfile={(m) => setDialog({ kind: 'profile', member: m })}
                                    card={{
                                        me: view.me,
                                        role: gone ? 'member' : view.role,
                                        identities: view.keeps_identities ? view.identities || {} : null,
                                        onMessage: message,
                                        onNotes: view.has_notes && !gone ? openNotes : null,
                                        onCompare: (m) => setDialog({ kind: 'codes', member: m }),
                                        onEdit: () => setDialog({ kind: 'edit' }),
                                        onRemove: (m) => setDialog({ kind: 'remove', member: m }),
                                    }} />
                            )}
                        </aside>
                    )}
                </div>
                {dialog?.kind === 'channel' && <ChannelDialog denID={denID} view={view} channel={dialog.channel} onClose={() => setDialog(null)} />}
                {dialog?.kind === 'group' && <GroupDialog denID={denID} view={view} group={dialog.group} onClose={() => setDialog(null)} />}
                {dialog?.kind === 'profile' && (
                    <ProfileCard
                        denID={denID}
                        member={members.get(dialog.member.id) || dialog.member}
                        me={view.me}
                        role={gone ? 'member' : view.role}
                        online={online.has(dialog.member.id)}
                        onClose={() => setDialog(null)}
                        onMessage={message}
                        onNotes={view.has_notes && !gone ? openNotes : null}
                        kept={view.keeps_identities ? view.identities?.[dialog.member.id] || {} : null}
                        onCompare={(m) => setDialog({ kind: 'codes', member: m })}
                        onEdit={() => setDialog({ kind: 'edit' })}
                        onRemove={(m) => setDialog({ kind: 'remove', member: m })}
                    />
                )}
                {dialog?.kind === 'codes' && <CodesDialog denID={denID} member={members.get(dialog.member.id) || dialog.member} onClose={() => setDialog(null)} />}
                {dialog?.kind === 'edit' && <EditProfile denID={denID} me={members.get(view.me.id) || view.me} limits={view.limits} nameStyles={!!view.name_styles}
                    onClose={() => setDialog(null)} />}
                {dialog?.kind === 'remove' && <RemoveMember denID={denID} member={dialog.member} onClose={() => setDialog(null)} />}
                {/* Group DMs (M16.3): making one, from the DM list or a DM's
                    profile, which leaves the DM as it is; adding to one; and
                    naming it. */}
                {dialog?.kind === 'new-group' && (
                    <PickPeople view={view} title="New group" action="Make the group" named picked={dialog.with ? [dialog.with] : []}
                        min={2} max={MAX_MEMBERS - 1} onPick={makeGroup} onClose={() => setDialog(null)} />
                )}
                {dialog?.kind === 'add-to-group' && group && (
                    <PickPeople view={view} title="Add people" action="Add" leaveOut={group.members} max={MAX_MEMBERS - group.members.length}
                        onPick={async (picked) => {
                            await api.post(`/api/dens/${denID}/group-dms/${channel.id}/members`, { members: picked });
                            setDialog(null);
                        }} onClose={() => setDialog(null)} />
                )}
                {dialog?.kind === 'rename-group' && group && (
                    <RenameGroup denID={denID} channel={channel} name={group.name} onClose={() => setDialog(null)} />
                )}
            </LinkContext.Provider>
            </EmojiContext.Provider>
        </DenContext.Provider>
    );
}

// withKey puts a DM key's change into the view.
function withKey(view, key) {
    const keys = (view.dm_keys || []).filter((k) => k.id !== key.id);
    return { ...view, dm_keys: [...keys, key] };
}

// ListTabs switches the sidebar between the den's channels and DMs, and
// shows whether the other has anything waiting: a channel or DM unread,
// which nothing in the member's own notes is (M17.3).
function ListTabs({ tab, onTab, view, reads, open }) {
    const waits = (c) => unread(reads.get(c.id));
    const waiting = (dms) => view.channels.some((c) => isPrivate(c) === dms && !isNotes(c) && c.id !== open && hasMessages(c) && waits(c) && !reads.get(c.id)?.closed);
    const tabs = [['den', 'Den', waiting(false)], ['dms', 'Direct messages', waiting(true)]];
    return (
        <div role="tablist" class="tabs tabs-border px-2 pt-1">
            {tabs.map(([id, label, dot]) => (
                <button key={id} type="button" role="tab" aria-selected={tab === id} class={`tab gap-1 ${tab === id ? 'tab-active' : ''}`} onClick={() => onTab(id)}>
                    {label}
                    {dot && tab !== id && <span class="h-2 w-2 rounded-full bg-base-content" aria-label="unread"></span>}
                </button>
            ))}
        </div>
    );
}

// UserPanel is the member's own corner at the foot of the channel list, as
// tall as the message bar beside it, which it lines up with: their picture
// and name, which open their profile to edit, and the cog for the
// settings. It keeps the list's color, so the call's lighter block above it
// shows at a glance.
function UserPanel({ me, online, onEdit }) {
    return (
        <div class="flex h-[73px] items-center gap-1 border-t border-base-300 px-2">
            <HoverRow as="button" type="button" class="flex min-w-0 flex-1 items-center gap-3 rounded px-1 py-1 text-left hover:bg-base-300/60"
                onClick={onEdit} aria-label="Edit your profile" title="Edit your profile">
                <Avatar member={me} size="md" online={online} />
                <span class="min-w-0 leading-tight">
                    <Name member={me} class="block truncate text-sm font-medium" />
                    <span class="block truncate text-xs text-base-content/60">@{me.username}</span>
                </span>
            </HoverRow>
            <button type="button" class="btn btn-ghost btn-sm btn-square" aria-label="App settings" title="App settings" onClick={() => openSettings()}>
                <CogIcon />
            </button>
        </div>
    );
}

// snapLeft is where the phone layout's row scrolls to show a pane: a side
// pane at its edge, the channel whole.
function snapLeft(row, pane) {
    const el = row.querySelector(`:scope > [data-pane="${pane}"]`);
    if (!el) return 0;
    return pane === 'side' ? el.offsetLeft + el.offsetWidth - row.clientWidth : el.offsetLeft;
}

// usePaneRow keeps the phone layout's row of panes at the pane at, which
// the history shows, sliding there once it's placed; and hears where a
// swipe settles, as the browser snaps it to a pane, through onSwipe. ready
// says the row is up.
function usePaneRow(at, ready, onSwipe) {
    const row = useRef(null);
    const placed = useRef(false);
    // heading is the pane the page is scrolling the row to: until it gets
    // there, or the member takes over, the row settling elsewhere is the
    // page's own doing, as a snap after it settles late, not a swipe.
    const heading = useRef('');
    const swiped = useRef(onSwipe);
    swiped.current = onSwipe;
    useLayoutEffect(() => {
        const r = row.current;
        if (!r || !at) {
            placed.current = false;
            return;
        }
        const left = snapLeft(r, at);
        if (Math.abs(r.scrollLeft - left) >= 2) {
            heading.current = at;
            r.scrollTo({ left, behavior: placed.current ? 'smooth' : 'instant' });
        }
        placed.current = true;
    }, [at, ready]);
    useEffect(() => {
        const r = row.current;
        if (!r || !at) return undefined;
        // Midway, as when a swipe lets go between panes, isn't settled.
        const settle = () => {
            const near = ['list', 'chan', 'side'].find((p) => r.querySelector(`:scope > [data-pane="${p}"]`) && Math.abs(r.scrollLeft - snapLeft(r, p)) < 2);
            if (heading.current) {
                if (near === heading.current) heading.current = '';
            } else if (near) {
                swiped.current(near);
            }
        };
        // WebViews before 114 have no scrollend: a pause in scrolling stands
        // in for it.
        let timer = 0;
        const later = () => {
            clearTimeout(timer);
            timer = setTimeout(settle, 150);
        };
        const event = 'onscrollend' in r ? 'scrollend' : 'scroll';
        const heard = event === 'scrollend' ? settle : later;
        const takeOver = () => {
            heading.current = '';
        };
        r.addEventListener(event, heard);
        for (const input of ['pointerdown', 'touchstart', 'wheel']) r.addEventListener(input, takeOver, { passive: true });
        return () => {
            clearTimeout(timer);
            r.removeEventListener(event, heard);
            for (const input of ['pointerdown', 'touchstart', 'wheel']) r.removeEventListener(input, takeOver);
        };
    }, [!!at, ready]);
    return row;
}

// rowLabel names a list's row by its name and what its marks say, when
// they say anything.
function rowLabel(name, ...marks) {
    const said = marks.filter(Boolean);
    return said.length ? [name, ...said].join(', ') : undefined;
}

// What a channel's or a DM's marks mean, which their tips say.
const STAFF_ONLY = 'Staff only: only moderators and the owner can see it.';
const MUTED = "Muted: its messages don't notify you.";

// readStyle is how a row's name shows whether it waits to be read: bold at
// full strength, while one read is a little fainter, unless it's open or
// its call is the member's.
function readStyle(unread, here) {
    if (unread) return 'font-bold text-base-content';
    return here ? '' : 'text-base-content/85';
}

// UnreadPill marks a row with messages unread at its left edge, which tells
// it from one read in any theme, at a glance on a phone too.
function UnreadPill() {
    return <span class="pointer-events-none absolute left-0 top-1/2 h-2 w-1 -translate-y-1/2 rounded-r-full bg-base-content" data-unread aria-hidden="true"></span>;
}

// Marks says on a button what waits behind it: how many mentions, or a dot
// for unread messages.
function Marks({ mentions, unread }) {
    if (mentions > 0) return <span class="badge badge-error badge-xs absolute -right-1 -top-1">{mentions}</span>;
    if (unread) return <span class="absolute right-0.5 top-0.5 h-2 w-2 rounded-full bg-base-content"></span>;
    return null;
}

// ChevronIcon points down beside an unfolded group, and right beside a
// folded one.
function ChevronIcon({ folded }) {
    return (
        <svg class={`h-3 w-3 shrink-0 ${folded ? '-rotate-90' : ''}`} {...ICON}>
            <path d="M4 6l4 4 4-4" />
        </svg>
    );
}

function HomeIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-[18px] w-[18px]" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
            aria-hidden="true">
            <path d="M2 7.5 8 2.5l6 5" />
            <path d="M3.5 6.5v7h3.25v-4h2.5v4h3.25v-7" />
        </svg>
    );
}

// staffMuted says whether staff muted this member in their call in this
// den, which a staff mute outlasts.
export function staffMuted(view, call) {
    if (!call || !view?.me) return false;
    const c = (view.calls || []).find((x) => x.channel_id === call.channel);
    return !!c?.members.find((m) => m.id === view.me.id)?.staff_muted;
}

function applyReads(view, reads) {
    const changed = new Map(reads.map((r) => [r.channel_id, r]));
    return { ...view, read_states: view.read_states.map((r) => changed.get(r.channel_id) || r) };
}

// muted holds the channels the member muted, or is null while no kind of
// notification is on (M12.3).
// events, while the den has them, puts its events' row at the list's top
// (M17.4), open while their pane shows, which events.onOpen opens.
function ChannelList({ denID, view, reads, members, open, staff, gone, muted, onOpen, onDialog, events }) {
    const { ungrouped, groups } = listed(view);
    const calls = new Map((view.calls || []).map((c) => [c.channel_id, c]));
    const [folded, setFolded] = useState(() => readFolded(stored(FOLDED + denID)));
    function fold(id) {
        const next = new Set(folded);
        if (!next.delete(id)) next.add(id);
        store(FOLDED + denID, JSON.stringify([...next]));
        setFolded(next);
    }
    const { call, speaking, sounding, volumes, watched, share, den } = useCall();
    const watching = new Set(watched.map((w) => w.member));
    // Staff reach each channel's and group's settings from a ⋯ beside it
    // (M4.2), which shows on hover or focus. It shows once its row has the
    // focus, since Tab passes over what's hidden (M11). A touch screen has
    // no ⋯ there: a long press on the row opens its settings, as one on a
    // message opens its actions (M13.4).
    const settings = 'btn btn-ghost btn-xs btn-square invisible group-hover:visible group-focus-within:visible pointer-coarse:hidden touch-room';
    const touch = useCoarse();
    const pressed = useRef(null);
    const press = useRef(null);
    press.current ||= createPress({ onLong: () => pressed.current && onDialog(pressed.current) });
    // The click a long press may be followed by goes nowhere: not to the
    // row, nor to the settings it opened under the finger, which aren't
    // the row's, and whose backdrop would take it for a tap outside.
    useEffect(() => {
        const swallow = (e) => {
            if (!press.current.click()) return;
            e.preventDefault();
            e.stopPropagation();
        };
        document.addEventListener('click', swallow, true);
        return () => {
            document.removeEventListener('click', swallow, true);
            press.current.cancel();
        };
    }, []);
    // presses has a row's long press open what dialog says, in place of the
    // browser's own selection and menu.
    const presses = (dialog) => (touch && staff && !gone ? {
        onPointerDown: (e) => {
            pressed.current = dialog;
            press.current.down(e);
        },
        onPointerMove: (e) => press.current.move(e),
        onPointerUp: () => press.current.up(),
        onPointerCancel: () => press.current.cancel(),
        onContextMenu: (e) => e.preventDefault(),
    } : {});
    // With a mouse, staff drag channels and groups into order, and members
    // from one call onto another voice channel (M17.2); touch screens and
    // keyboards keep the settings' Move up and Move down, and Move to in
    // the member's menu. held is what this list's drag carries, which only
    // a drag it started does, and drop where it would land.
    const drags = staff && !gone && !touch;
    const moves = staff && !gone && !!view.move_members;
    const held = useRef(null);
    const [drop, setDrop] = useState(null);
    const [dragging, setDragging] = useState('');
    const [dragError, setDragError] = useState('');
    function lift(e, type, what, id, from = '') {
        e.dataTransfer.setData(type, id);
        e.dataTransfer.effectAllowed = 'move';
        held.current = { what, id, from };
        setDragging(what);
        setDragError('');
    }
    function letGo() {
        held.current = null;
        setDrop(null);
        setDragging('');
    }
    function land(e, send) {
        e.preventDefault();
        e.stopPropagation();
        letGo();
        if (!send) return;
        send().catch((err) => setDragError(err.message));
    }
    // over takes a drag over a row where it can land, there, and with half
    // says whether it's past the row's middle.
    function over(e, key, half) {
        e.preventDefault();
        e.stopPropagation();
        e.dataTransfer.dropEffect = 'move';
        const r = e.currentTarget.getBoundingClientRect();
        const after = half ? e.clientY > r.top + r.height / 2 : undefined;
        setDrop((d) => (d?.key === key && d.after === after ? d : { key, after }));
    }
    const leave = (key) => (e) => !e.currentTarget.contains(e.relatedTarget) && setDrop((d) => (d?.key === key ? null : d));
    const pastMiddle = (e) => {
        const r = e.currentTarget.getBoundingClientRect();
        return e.clientY > r.top + r.height / 2;
    };
    const moveTo = (member, channel) => api.post(`/api/dens/${denID}/members/${member}/move`, { channel_id: channel });
    // A member's drag lands on a voice channel Move to would list for them.
    const takes = (h, c) => h?.what === 'member' && moveTargets(view, members.get(h.id), h.from).some((t) => t.id === c.id);
    // A channel's row takes a channel dropped beside it, and a voice
    // channel's a member dropped on it, from another call.
    const rowDrag = (c) => (drags ? {
        draggable: true,
        onDragStart: (e) => lift(e, DRAG_CHANNEL, 'channel', c.id),
        onDragEnd: letGo,
        onDragOver: (e) => {
            const h = held.current;
            if (h?.what === 'channel' && h.id !== c.id) over(e, c.id, true);
            else if (takes(h, c)) over(e, c.id, false);
        },
        onDragLeave: leave(c.id),
        onDrop: (e) => {
            const h = held.current;
            if (h?.what === 'channel') {
                const body = channelDrop(view, h.id, { group: c.group_id || '', near: c.id, after: pastMiddle(e) });
                land(e, body && (() => api.patch(`/api/dens/${denID}/channels/${h.id}`, body)));
            } else if (takes(h, c)) {
                land(e, () => moveTo(h.id, c.id));
            }
        },
    } : {});
    // A group's heading takes a group dropped beside it, and a channel
    // dropped on it, which goes first in the group.
    const headDrag = (g) => (drags ? {
        draggable: true,
        onDragStart: (e) => lift(e, DRAG_GROUP, 'group', g.id),
        onDragEnd: letGo,
        onDragOver: (e) => {
            const h = held.current;
            if (h?.what === 'group' && h.id !== g.id) over(e, `group-${g.id}`, true);
            else if (h?.what === 'channel') over(e, `group-${g.id}`, false);
        },
        onDragLeave: leave(`group-${g.id}`),
        onDrop: (e) => {
            const h = held.current;
            const body = h?.what === 'group' ? groupDrop(view, h.id, { near: g.id, after: pastMiddle(e) })
                : h?.what === 'channel' ? channelDrop(view, h.id, { group: g.id }) : null;
            const path = h?.what === 'group' ? 'groups' : 'channels';
            land(e, body && (() => api.patch(`/api/dens/${denID}/${path}/${h.id}`, body)));
        },
    } : {});
    // The channels in no group take a channel dropped past their last.
    const looseDrag = drags ? {
        onDragOver: (e) => held.current?.what === 'channel' && over(e, 'ungrouped', false),
        onDragLeave: leave('ungrouped'),
        onDrop: (e) => {
            const h = held.current;
            if (h?.what !== 'channel') return;
            const last = ungrouped.filter((c) => c.id !== h.id).at(-1);
            const body = channelDrop(view, h.id, last ? { group: '', near: last.id, after: true } : { group: '' });
            land(e, body && (() => api.patch(`/api/dens/${denID}/channels/${h.id}`, body)));
        },
    } : {};
    // dropMark marks where a drop would land: a line above or below a row,
    // or a ring round what it'd go into.
    const dropMark = (key) => {
        if (drop?.key !== key) return '';
        if (drop.after === undefined) return 'ring-2 ring-inset ring-primary';
        return drop.after ? 'shadow-[inset_0_-2px_0_0_var(--color-primary)]' : 'shadow-[inset_0_2px_0_0_var(--color-primary)]';
    };
    const item = (c) => {
        const r = reads.get(c.id);
        const isUnread = c.kind === 'text' && unread(r) && c.id !== open;
        const voice = c.kind === 'voice';
        const inCall = voice && call?.den === denID && call.channel === c.id;
        // The page's call shows how it stands on its channel's speaker:
        // green once connected, and yellow until then (M14.3).
        const state = inCall ? callState(call.status) : '';
        const label = { den: view.name, channel: c.name };
        const managed = staff && !gone;
        const isMuted = !!muted?.has(c.id);
        const mentions = c.id !== open ? r?.mention_count || 0 : 0;
        return (
            <li key={c.id}>
                {/* The row's button reaches across it, under its marks, whose
                    tips say on a tap what they mean (M13.2). */}
                <div class={`group relative flex min-h-8 select-none items-center gap-0.5 rounded pr-1 pointer-coarse:min-h-11 ${managed ? 'pr-8 pointer-coarse:pr-1' : ''} ${c.id === open || inCall ? 'bg-base-300' : 'hover:bg-base-300/60'} ${dropMark(c.id)}`}
                    data-channel-row={c.id} {...presses({ kind: 'channel', channel: c })} {...rowDrag(c)}>
                    <button
                        type="button"
                        class="flex min-w-0 flex-1 items-center gap-2 self-stretch rounded px-2 text-left after:absolute after:inset-0 after:rounded"
                        onClick={() => (voice ? joinCall(denID, c.id, label) : onOpen(c.id))}
                        disabled={voice && gone}
                        title={voice ? (canCall() ? 'Join the call' : NO_CALLS) : undefined}
                        aria-label={rowLabel(c.name, state, c.staff_only && 'staff only', isMuted && 'muted', mentions && `${mentions} mentions`)}
                    >
                        <span class={!inCall ? 'text-base-content/50' : state === 'connected' ? 'text-success' : 'text-warning'} data-call-state={state || undefined}>
                            {voice ? <SpeakerIcon size="h-4 w-4" /> : <HashIcon />}
                        </span>
                        <span class={`truncate ${readStyle(isUnread, c.id === open || inCall)}`}>{c.name}</span>
                    </button>
                    {/* The call's lock, beside how it stands (M16.2). */}
                    {inCall && <CallLock call={call} den={den} />}
                    {isUnread && <UnreadPill />}
                    {c.staff_only && <Tip id={`staff-${c.id}`} label="Staff only" trigger={<ShieldIcon />}>{STAFF_ONLY}</Tip>}
                    {isMuted && <Tip id={`muted-${c.id}`} label="Muted" trigger={<BellIcon off size="h-3.5 w-3.5" />}>{MUTED}</Tip>}
                    {mentions > 0 && <span class="badge badge-error badge-xs ml-1">{mentions}</span>}
                    {managed && (
                        <button type="button" class={`${settings} absolute right-1 top-1/2 -translate-y-1/2`} title="Channel settings"
                            aria-label={`Settings for ${c.name}`} onClick={() => onDialog({ kind: 'channel', channel: c })}>
                            ⋯
                        </button>
                    )}
                </div>
                {voice && (
                    <CallMembers denID={denID} call={calls.get(c.id)} members={members} me={view.me} speaking={inCall ? speaking : null}
                        sounding={inCall ? sounding : ''} deafened={inCall && !!call?.deafened} volumes={volumes} onProfile={(m) => onDialog({ kind: 'profile', member: m })} inCall={inCall} label={label}
                        watched={watching} previewing={!!share?.preview} identities={view.keeps_identities ? view.identities : null} since={call?.since}
                        moving={moves ? {
                            targets: (member) => moveTargets(view, member, c.id),
                            lift: drags ? (e, m) => lift(e, DRAG_MEMBER, 'member', m.id, c.id) : null,
                            letGo,
                        } : null} />
                )}
            </li>
        );
    };
    return (
        <nav aria-label="Channels" class="flex flex-col gap-2 p-2">
            {events && (
                <ul class="flex flex-col">
                    <EventsRow events={view.events} open={events.open} onOpen={events.onOpen} />
                </ul>
            )}
            <ul class={`flex flex-col rounded ${dragging === 'channel' ? 'min-h-4' : ''} ${dropMark('ungrouped')}`} data-ungrouped {...looseDrag}>{ungrouped.map(item)}</ul>
            {groups.map(({ group: g, channels }) => {
                // A folded group keeps what's open or busy in sight, and its
                // row says what waits in the rest (M11).
                const isFolded = folded.has(g.id);
                const shown = isFolded ? foldedShows(channels, open, calls) : channels;
                const marks = isFolded ? hiddenMarks(channels.filter((c) => !shown.includes(c)), reads) : null;
                return (
                    <div key={g.id}>
                        <div class={`group flex min-h-6 select-none items-center justify-between gap-1 rounded px-2 text-xs font-semibold uppercase text-base-content/60 pointer-coarse:min-h-11 ${dropMark(`group-${g.id}`)}`}
                            data-group-row={g.id} {...presses({ kind: 'group', group: g })} {...headDrag(g)}>
                            <button type="button" class="flex min-w-0 flex-1 cursor-pointer items-center gap-1 self-stretch text-left uppercase hover:text-base-content"
                                aria-expanded={!isFolded} onClick={() => fold(g.id)}>
                                <ChevronIcon folded={isFolded} />
                                <span class="truncate">{g.name}</span>
                                {marks?.mentions > 0 && <span class="badge badge-error badge-xs font-normal normal-case">{marks.mentions}</span>}
                                {marks?.unread && !marks.mentions && <span class="h-2 w-2 shrink-0 rounded-full bg-base-content" aria-label="unread"></span>}
                            </button>
                            {staff && !gone && (
                                <button type="button" class={`${settings} relative font-normal normal-case`} aria-label={`Settings for the group ${g.name}`} title="Group settings"
                                    onClick={() => onDialog({ kind: 'group', group: g })}>
                                    ⋯
                                </button>
                            )}
                        </div>
                        <ul class="flex flex-col">{shown.map(item)}</ul>
                    </div>
                );
            })}
            {dragError && <p class="px-2 text-xs text-error" role="alert">{dragError}</p>}
        </nav>
    );
}

// DMList shows this member's DMs, the most recently active first. A closed
// one stays out of it until it has a new message, unless it's open. A DM
// waiting for this member to compare codes says so.
// dms are as recentDMs lists them.
// muted holds the DMs the member muted, or is null while no kind of
// notification is on (M12.3).
// notes, while the den has notes for this Dens, puts the member's at the
// list's top (M17.3), open while they show, which onNotes opens.
function DMList({ dms, notes, reads, open, partner, online, identities, muted, title, onOpen, onClose, onNotes }) {
    const notesRow = notes && <NotesRow open={notes.open} onOpen={onNotes} />;
    if (dms.length === 0) {
        return (
            <>
                {notesRow && (
                    <nav aria-label="Direct messages" class="flex flex-col p-2 pb-0">
                        <ul class="flex flex-col">{notesRow}</ul>
                    </nav>
                )}
                <p class="cursor-default select-none p-4 text-sm text-base-content/60">
                    No direct messages yet. Open someone's profile to message them; it's end-to-end encrypted.
                </p>
            </>
        );
    }
    return (
        <nav aria-label="Direct messages" class="flex flex-col p-2">
            <ul class="flex flex-col">
                {notesRow}
                {dms.map((c) => {
                    if (c.kind === 'group_dm') {
                        return <GroupRow key={c.id} c={c} r={reads.get(c.id)} open={open} title={title(c)} muted={muted} onOpen={onOpen} onClose={onClose} />;
                    }
                    const other = partner(c);
                    const r = reads.get(c.id);
                    const isUnread = unread(r) && c.id !== open;
                    const isMuted = !!muted?.has(c.id);
                    const verified = !!(other && identities[other.id]?.verified);
                    const mentions = c.id !== open ? r?.mention_count || 0 : 0;
                    const who = other ? other.display_name : 'Unknown member';
                    return (
                        <HoverRow as="li" key={c.id} class={`group relative flex min-h-8 items-center gap-0.5 rounded pr-8 pointer-coarse:min-h-11 ${c.id === open ? 'bg-base-300' : 'hover:bg-base-300/60'}`}>
                            <button
                                type="button"
                                class={`flex min-w-0 flex-1 items-center gap-2 self-stretch rounded px-2 text-left after:absolute after:inset-0 after:rounded ${other?.left_at ? 'opacity-60' : ''}`}
                                onClick={() => onOpen(c.id)}
                                aria-label={rowLabel(who, verified && 'verified', isMuted && 'muted', mentions && `${mentions} mentions`)}
                            >
                                <Avatar member={other} size="sm" online={other && !other.left_at ? online.has(other.id) : undefined} />
                                <Name member={other} class={`truncate ${readStyle(isUnread, c.id === open)}`} />
                            </button>
                            {isUnread && <UnreadPill />}
                            {isMuted && <Tip id={`muted-${c.id}`} label="Muted" trigger={<BellIcon off size="h-3.5 w-3.5" />}>{MUTED}</Tip>}
                            {verified && <VerifiedMark id={`verified-${c.id}`} name={who} />}
                            {mentions > 0 && <span class="badge badge-error badge-xs ml-1">{mentions}</span>}
                            {/* Touch screens have no hover, so there the X always shows. */}
                            <button
                                type="button"
                                class="btn btn-ghost btn-xs btn-square absolute right-1 top-1/2 -translate-y-1/2 touch-room md:pointer-fine:invisible md:pointer-fine:group-hover:visible md:pointer-fine:focus:visible"
                                aria-label={`Close the DM with ${other ? other.display_name : 'this member'}`}
                                title="Close"
                                onClick={() => onClose(c)}
                            >
                                ✕
                            </button>
                        </HoverRow>
                    );
                })}
            </ul>
        </nav>
    );
}

// GroupRow is a group DM in the DM list (M16.3): its name, or its other
// members', and what waits in it.
function GroupRow({ c, r, open, title, muted, onOpen, onClose }) {
    const isUnread = unread(r) && c.id !== open;
    const isMuted = !!muted?.has(c.id);
    const mentions = c.id !== open ? r?.mention_count || 0 : 0;
    return (
        <HoverRow as="li" class={`group relative flex min-h-8 items-center gap-0.5 rounded pr-8 pointer-coarse:min-h-11 ${c.id === open ? 'bg-base-300' : 'hover:bg-base-300/60'}`}
            data-group-row={c.id}>
            <button type="button" class="flex min-w-0 flex-1 items-center gap-2 self-stretch rounded px-2 text-left after:absolute after:inset-0 after:rounded"
                onClick={() => onOpen(c.id)} aria-label={rowLabel(title, 'group', isMuted && 'muted', mentions && `${mentions} unread`)}>
                <GroupIcon size="h-6 w-6" />
                <span class={`truncate ${readStyle(isUnread, c.id === open)}`}>{title}</span>
            </button>
            {isUnread && <UnreadPill />}
            {isMuted && <Tip id={`muted-${c.id}`} label="Muted" trigger={<BellIcon off size="h-3.5 w-3.5" />}>{MUTED}</Tip>}
            {mentions > 0 && <span class="badge badge-error badge-xs ml-1">{mentions}</span>}
            <button type="button"
                class="btn btn-ghost btn-xs btn-square absolute right-1 top-1/2 -translate-y-1/2 touch-room md:pointer-fine:invisible md:pointer-fine:group-hover:visible md:pointer-fine:focus:visible"
                aria-label={`Close ${title}`} title="Close" onClick={() => onClose(c)}>
                ✕
            </button>
        </HoverRow>
    );
}

function PeopleIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
            <circle cx="6" cy="5" r="2.5" />
            <path d="M1.5 13.5c0-2.5 2-4 4.5-4s4.5 1.5 4.5 4" />
            <path d="M10.5 3a2.5 2.5 0 010 5M12 9.5c1.5.5 2.5 2 2.5 4" />
        </svg>
    );
}

// ChannelHeader names the channel or the DM's other member. A channel's
// description shows its first line, which opens the rest when there is
// more than fits. Its bell mutes the channel's notifications, once the
// member turned some on (M12.3): muted is null until then.
function ChannelHeader({ channel, dm, groupTitle: grouped, me, elsewhere, verified, onCompare, online, membersOpen, muted, onMute, onChannels, onMembers, onProfile }) {
    const [expanded, setExpanded] = useState(false);
    const line = useRef(null);
    const clipped = useClipped(line, channel.description);
    const hasMore = !!channel.description && channel.description.trim() !== firstLine(channel.description).trim();
    const expandable = hasMore || clipped || expanded;
    return (
        <header class="border-b border-base-300 px-4 py-2">
            <div class="flex min-w-0 items-center gap-2">
                <button
                    type="button"
                    class="btn btn-ghost btn-sm btn-square relative md:hidden"
                    onClick={onChannels}
                    aria-label={elsewhere.mentions ? `Channels, ${elsewhere.mentions} unread mentions` : 'Channels'}
                >
                    <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                        <path d="M2 4h12M2 8h12M2 12h12" />
                    </svg>
                    <Marks mentions={elsewhere.mentions} unread={elsewhere.unread} />
                </button>
                {dm !== null && channel.kind === 'dm' ? (
                    <>
                        {/* The member and their verified mark are one row, which
                            the mark shows on hover over (M16.4). */}
                        <HoverRow as="span" class="flex min-w-0 items-center gap-1">
                            <button type="button" class="flex min-w-0 items-center gap-2 rounded text-left hover:bg-base-200" onClick={() => dm && onProfile(dm)}>
                                <Avatar member={dm} size="sm" online={dm && !dm.left_at ? online : undefined} />
                                <Name member={dm} class="truncate font-semibold" />
                                {dm && <span class="hidden truncate text-sm text-base-content/60 sm:inline">@{dm.username}</span>}
                            </button>
                            {verified && <VerifiedMark id={`header-verified-${channel.id}`} name={dm.display_name} />}
                        </HoverRow>
                        {/* The lock says the DM is end-to-end encrypted, and
                            opens comparing codes (M16.1). */}
                        <button type="button" class="btn btn-ghost btn-xs shrink-0 gap-1 font-normal text-base-content/60" onClick={onCompare}
                            title="End-to-end encrypted: only the two of you can read it. Compare codes to check nobody sits between you."
                            aria-label="End-to-end encrypted: compare codes">
                            <LockIcon size="h-3 w-3" />
                            Encrypted
                        </button>
                        {channel.expires_after > 0 && <ExpiryBadge after={channel.expires_after} onClick={onMembers} />}
                    </>
                ) : channel.kind === 'notes' ? (
                    <>
                        <span class="flex min-w-0 items-center gap-2" data-notes-header>
                            <NotesIcon />
                            <span class="truncate font-semibold">Notes</span>
                        </span>
                        {/* Only the member's devices open them (M17.3). */}
                        <Tip id={`header-notes-${channel.id}`} label="Encrypted" trigger={
                            <span class="flex shrink-0 items-center gap-1 text-xs text-base-content/60"><LockIcon size="h-3 w-3" />Encrypted</span>
                        }>
                            End-to-end encrypted: only your devices can read your notes.
                        </Tip>
                    </>
                ) : channel.kind === 'group_dm' ? (
                    <>
                        <button type="button" class="flex min-w-0 items-center gap-2 rounded text-left hover:bg-base-200" onClick={onMembers}>
                            <GroupIcon size="h-6 w-6" />
                            <span class="truncate font-semibold" data-group-header>{grouped}</span>
                        </button>
                        {/* The lock says the group is end-to-end encrypted, and
                            opens its members, where each compares codes (M16.3). */}
                        <button type="button" class="btn btn-ghost btn-xs shrink-0 gap-1 font-normal text-base-content/60" onClick={onMembers}
                            title="End-to-end encrypted: only the group's members can read it. Compare codes with each to check nobody sits between you."
                            aria-label="End-to-end encrypted: the group's members">
                            <LockIcon size="h-3 w-3" />
                            Encrypted
                        </button>
                        {channel.expires_after > 0 && <ExpiryBadge after={channel.expires_after} onClick={onMembers} />}
                    </>
                ) : (
                    <span class="flex shrink-0 items-center gap-1 font-semibold">
                        <span class="text-base-content/50"><HashIcon /></span>
                        {channel.name}
                        {channel.staff_only && <Tip id={`header-staff-${channel.id}`} label="Staff only" trigger={<ShieldIcon />}>{STAFF_ONLY}</Tip>}
                    </span>
                )}
                {channel.description && (
                    <button
                        type="button"
                        class="flex min-h-8 min-w-0 items-center gap-1 rounded px-1 text-left text-sm text-base-content/60 enabled:hover:bg-base-200 enabled:hover:text-base-content disabled:cursor-default"
                        disabled={!expandable}
                        aria-expanded={expandable ? expanded : undefined}
                        title={expandable ? (expanded ? 'Hide the description' : 'Show the whole description') : undefined}
                        onClick={() => setExpanded(!expanded)}
                    >
                        <span ref={line} class="truncate">
                            <Preview text={channel.description} me={me} more={hasMore} />
                        </span>
                        {expandable && (
                            <svg viewBox="0 0 16 16" class={`h-3 w-3 shrink-0 transition-transform ${expanded ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
                                <path d="M4 6l4 4 4-4" />
                            </svg>
                        )}
                    </button>
                )}
                <div class="ml-auto flex shrink-0 items-center gap-1">
                    {muted !== null && channel.kind !== 'voice' && channel.kind !== 'notes' && (
                        <button type="button" class="btn btn-ghost btn-sm btn-square" aria-pressed={muted} onClick={() => onMute(!muted)}
                            aria-label={`${muted ? 'Unmute' : 'Mute'} ${channel.kind === 'dm' ? `the DM with ${dm ? dm.display_name : 'this member'}` : channel.kind === 'group_dm' ? grouped : `#${channel.name}`}`}
                            title={muted ? 'Unmute its notifications' : 'Mute its notifications'}>
                            <BellIcon off={muted} />
                        </button>
                    )}
                    {channel.kind !== 'notes' && (
                        <button type="button" class={`btn btn-ghost btn-sm btn-square ${membersOpen ? 'btn-active' : ''}`} onClick={onMembers}
                            aria-label={channel.kind === 'dm' ? 'Profile' : channel.kind === 'group_dm' ? "The group's members" : 'Members'} aria-pressed={membersOpen}
                            title={channel.kind === 'dm' ? 'Profile' : 'Members'}>
                            <PeopleIcon />
                        </button>
                    )}
                </div>
            </div>
            {expanded && channel.description && (
                <div class="mt-1 max-h-64 overflow-y-auto rounded bg-base-200 p-2 text-sm">
                    <Markdown text={channel.description} me={me} />
                </div>
            )}
        </header>
    );
}

// useClipped reports whether an element's text runs past its width, so a
// long first line counts as more to show.
function useClipped(ref, text) {
    const [clipped, setClipped] = useState(false);
    useEffect(() => {
        const el = ref.current;
        if (!el || typeof ResizeObserver === 'undefined') return;
        const check = () => setClipped(el.scrollWidth > el.clientWidth + 1);
        const observer = new ResizeObserver(check);
        observer.observe(el);
        check();
        return () => observer.disconnect();
    }, [text]);
    return clipped;
}
