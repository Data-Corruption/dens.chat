// A den's chat: the sidebar of channels and DMs, the open channel, and the
// member list. Below 768 pixels they're three panes in a row, one on screen
// at a time (M13.1): the list, the channel, and the members or a DM's
// profile, which the browser's own scrolling swipes between, snapping to
// each, and the page's history walks.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { Avatar, DenContext } from './avatar.jsx';
import { onConnection, onEvent, sendTyping, showChannel } from './events.js';
import { openManage } from './dialogs.js';
import { useNotifications } from './components.jsx';
import { BellIcon, HashIcon, ICON, LockIcon, SpeakerIcon } from './icons.jsx';
import { compareIds, unread } from './ids.js';
import { Markdown, Preview, firstLine } from './markdown.jsx';
import { MessagePane, openAt } from './messages.jsx';
import { LinkContext } from './msglinks.jsx';
import { nav, useNarrow } from './nav.js';
import { mutedIn, notifying, withdraw } from './notify.js';
import { showsOffline } from './offline.js';
import { ChannelDialog, GroupDialog } from './manage.jsx';
import { EditProfile, MemberList, ProfileCard, ProfilePanel, RemoveMember, isStaff } from './people.jsx';
import { NO_CALLS, canCall } from './platform.js';
import { needsCheck } from './private.js';
import { SignInRequests } from './private.jsx';
import { openSettings } from './settings.jsx';
import { applyCalls, joinCall } from './voice.js';
import { CallBlock, CallMembers, CogIcon, useCall } from './voice.jsx';

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
// A typing notice shows this long unless another arrives; senders repeat
// theirs while they type.
const TYPING_SHOWN = 6000;
// Changes to these reload the den's state rather than being applied here.
const STRUCTURAL = new Set([
    'den.updated', 'member.joined', 'member.updated', 'member.left', 'channel.created', 'channel.updated', 'channel.deleted',
    'channels.reordered', 'group.created', 'group.updated', 'group.deleted', 'groups.reordered',
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

const hasMessages = (c) => c && (c.kind === 'text' || c.kind === 'dm');

// closedDMs is the set of DMs this member closed.
function closedDMs(view) {
    return new Set(view.read_states.filter((r) => r.closed).map((r) => r.channel_id));
}

// listed arranges a den's channels, DMs aside, as its channel list shows
// them: those in no group, then each group's, each by position.
export function listed(view) {
    const byPosition = (a, b) => a.position - b.position;
    const channels = view.channels.filter((c) => c.kind !== 'dm');
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

// recentDMs is a den's DMs as their list shows them, the most recently
// active first: a DM closed since keeps out until it has a new message,
// unless it's the one open.
export function recentDMs(view, reads, open) {
    return view.channels
        .filter((c) => c.kind === 'dm' && (!reads.get(c.id)?.closed || c.id === open))
        .sort((a, b) => compareIds(reads.get(b.id)?.last_message_id, reads.get(a.id)?.last_message_id) || compareIds(b.id, a.id));
}

// lastDM is the DM the Direct messages tab opens (M11): the one last open
// in the den, unless it's gone or was closed since. Otherwise none, rather
// than one the member didn't pick, whom they might write to unawares.
export function lastDM(view, reads, lastID) {
    const c = view.channels.find((x) => x.kind === 'dm' && x.id === lastID);
    return c && !reads.get(c.id)?.closed ? c : null;
}

// pick chooses the channel to show: the one in the address, else the last
// one shown in this den unless it's a DM since closed, else the first text
// channel. The Direct messages tab with no DM open shows none, as does a
// den last left there, whose list the phone layout shows at its own
// address.
function pick(view, denID, channelID) {
    if (!view || channelID === NO_DM) return null;
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
                    else if (e.t === 'dens') view = withStatus(view, e.d);
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
                if (e.t === 'voice.state' || e.t === 'presence' || e.t === 'dm.key') catchUp(e);
                if (STRUCTURAL.has(e.t)) reloadSoon();
                else if (e.t === 'presence') setOnline((cur) => applyPresence(cur, e.d));
                else if (e.t === 'voice.state') setView((v) => (v ? { ...v, calls: applyCalls(v.calls, e.d) } : v));
                else if (e.t === 'typing') setTyping((cur) => withTyper(cur, e.d.channel_id, e.d.member_id, Date.now() + TYPING_SHOWN));
                else if (e.t === 'message.created') setTyping((cur) => withTyper(cur, e.d.channel_id, e.d.author_id, 0));
                else if (e.t === 'device.added') setNewDevice(e.d);
                else if (e.t === 'dm.key') setView((v) => (v ? withKey(v, e.d) : v));
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
        if (openKind) setTab(openKind === 'dm' ? 'dms' : 'den');
        else if (noDM) setTab('dms');
    }, [channel?.id, channelID, noDM]);
    // shownID is the channel shown, for the page's history: its ID, NO_DM,
    // or '' for none.
    const shownID = channel ? channel.id : noDM ? NO_DM : '';
    // The members' place: the member list, or beside a DM the other
    // member's profile (M11); with no DM open, there's none.
    const hasSide = !noDM;
    // at is the pane on screen on the phone layout, '' in a wider window.
    const at = !narrow ? '' : pane === 'side' && hasSide ? 'side' : pane === 'list' || (!pane && !channelID) ? 'list' : 'chan';
    const toPane = (next) => nav().den(denID, shownID, next);
    // A swipe that settles on another pane goes there in the history too.
    const row = usePaneRow(at, !!view, (near) => near !== at && toPane(near));

    if (error && !view) return <div class="p-6"><div role="alert" class="alert alert-error">{error}</div></div>;
    if (!view) return <div class="p-6"><span class="loading loading-spinner"></span></div>;

    if (channel) store(LAST_CHANNEL + denID, channel.id);
    if (channel?.kind === 'text') store(LAST_TEXT + denID, channel.id);
    if (channel?.kind === 'dm') store(LAST_DM + denID, channel.id);
    const members = new Map(view.members.map((m) => [m.id, m]));
    const reads = new Map(view.read_states.map((r) => [r.channel_id, r]));
    const staff = isStaff(view.role);
    const partner = (c) => members.get(c.members.find((id) => id !== view.me.id));
    const dm = channel?.kind === 'dm' ? partner(channel) : null;
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
    // DM to return to. The phone layout's list stays on screen, with what
    // the tab opens behind it.
    function pickTab(next) {
        store(LIST_TAB + denID, next);
        setTab(next);
        let to = null;
        if (next === 'den' && (channel?.kind === 'dm' || !channel)) to = denChannel(view, stored(LAST_TEXT + denID))?.id || '';
        else if (next === 'dms' && channel?.kind !== 'dm') to = lastDM(view, reads, stored(LAST_DM + denID))?.id || NO_DM;
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

    // A link to a message opens it here, or in the den it points into (M11).
    const links = {
        denID, view, dens,
        open: (den, channelID, messageID) => {
            openAt(den, channelID, messageID);
            navigate(`/den/${den}/${channelID}`);
        },
    };
    // What waits in the other dens, for the list's Home button.
    const away = { mentions: 0, unread: false };
    for (const d of dens) {
        if (d.den_id === denID) continue;
        away.mentions += d.mentions || 0;
        away.unread ||= d.unread > 0;
    }

    return (
        <DenContext.Provider value={denID}>
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
                                <span class="min-w-0 flex-1 truncate font-semibold">{view.name}</span>
                                {bells && (
                                    <button type="button" class="btn btn-ghost btn-xs btn-square -my-1" aria-pressed={muted.den}
                                        aria-label={muted.den ? `Unmute ${view.name}` : `Mute ${view.name}`}
                                        title={muted.den ? "Unmute the den's notifications" : "Mute the den's notifications"}
                                        onClick={() => mute('', !muted.den)}>
                                        <BellIcon off={muted.den} />
                                    </button>
                                )}
                                {!(live && view.state === 'connected') && (
                                    <span class="badge badge-xs badge-warning" title={live ? view.error || view.state : 'Not in touch with Dens on this computer'}></span>
                                )}
                            </div>
                            {live && view.state !== 'connected' && <p class="text-xs text-warning">{view.error || 'Reconnecting…'}</p>}
                        </div>
                        <ListTabs tab={tab} onTab={pickTab} view={view} reads={reads} open={channel?.id} />
                        {tab === 'dms' ? (
                            <DMList dms={recentDMs(view, reads, channel?.id)} reads={reads} open={channel?.id} partner={partner} online={online}
                                keys={view.dm_keys || []} me={view.me.id} muted={bells ? muted.channels : null} onOpen={openChannel} onClose={closeDM} />
                        ) : (
                            <ChannelList denID={denID} view={view} reads={reads} members={members} open={channel?.id} staff={staff} gone={gone}
                                muted={bells ? muted.channels : null} onOpen={openChannel} onDialog={setDialog} />
                        )}
                        <div class="mt-auto">
                            {staff && !gone && (
                                <div class="flex gap-1 px-2 pb-1">
                                    <button type="button" class="btn btn-ghost btn-xs" onClick={() => setDialog({ kind: 'channel' })}>+ Channel</button>
                                    <button type="button" class="btn btn-ghost btn-xs" onClick={() => setDialog({ kind: 'group' })}>+ Group</button>
                                </div>
                            )}
                            <CallBlock denID={denID} staffMuted={staffMuted(view, myCall)} />
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
                                <button type="button" class="btn btn-sm" onClick={forget}>Remove from this computer</button>
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
                                <ChannelHeader key={channel.id} channel={channel} dm={dm} me={view.me} elsewhere={elsewhere}
                                    online={dm && online.has(dm.id)} membersOpen={!at && membersOpen}
                                    muted={bells ? muted.channels.has(channel.id) : null} onMute={(on) => mute(channel.id, on)}
                                    onChannels={() => toPane('list')} onMembers={toggleMembers}
                                    onProfile={(m) => setDialog({ kind: 'profile', member: m })} />
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
                                    keys={channel.kind === 'dm' ? (view.dm_keys || []).filter((k) => k.channel_id === channel.id) : undefined}
                                    typing={typers}
                                    closed={closed}
                                    limits={view.limits}
                                    retention={view.retention}
                                    offline={showsOffline(live, view.state)}
                                    onProfile={(m) => setDialog({ kind: 'profile', member: m })}
                                    onTyping={() => sendTyping(denID, channel.id)}
                                />
                            </>
                        ) : noDM ? (
                            <div class="flex flex-col items-start p-6">
                                <button type="button" class="btn btn-ghost btn-sm md:hidden" onClick={() => toPane('list')}>Direct messages</button>
                            </div>
                        ) : (
                            <div class="flex flex-col items-start gap-2 p-6 text-base-content/70">
                                <button type="button" class="btn btn-ghost btn-sm md:hidden" onClick={() => toPane('list')}>Channels</button>
                                <p>{staff ? 'This den has no text channels yet. Create one to start talking.' : 'This den has no text channels yet.'}</p>
                            </div>
                        )}
                        {at && at !== 'chan' && <div class="pointer-events-none absolute inset-0 z-30 bg-black/40"></div>}
                    </section>
                    {/* Beside a DM, the member list's place shows the other member's
                        profile (M11); with no DM open, it shows nothing. */}
                    {hasSide && (at || membersOpen) && (
                        <aside data-pane="side" inert={!!at && at !== 'side'} class="flex w-[85%] shrink-0 snap-end flex-col overflow-y-auto bg-base-200 md:w-60">
                            <div class="flex items-center justify-between border-b border-base-300 p-3">
                                <span class="font-semibold">{channel?.kind === 'dm' ? 'Profile' : 'Members'}</span>
                                <button type="button" class="btn btn-ghost btn-sm md:hidden" onClick={() => toPane('chan')}
                                    aria-label={channel?.kind === 'dm' ? 'Close the profile' : 'Close the member list'}>✕</button>
                            </div>
                            {channel?.kind === 'dm' ? (
                                dm && <ProfilePanel key={dm.id} denID={denID} member={dm} me={view.me} online={online.has(dm.id)} />
                            ) : (
                                <MemberList members={view.members} online={online} onProfile={(m) => setDialog({ kind: 'profile', member: m })} />
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
                        onEdit={() => setDialog({ kind: 'edit' })}
                        onRemove={(m) => setDialog({ kind: 'remove', member: m })}
                    />
                )}
                {dialog?.kind === 'edit' && <EditProfile denID={denID} me={members.get(view.me.id) || view.me} limits={view.limits} onClose={() => setDialog(null)} />}
                {dialog?.kind === 'remove' && <RemoveMember denID={denID} member={dialog.member} onClose={() => setDialog(null)} />}
            </LinkContext.Provider>
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
// or a DM whose check codes this member hasn't compared, as one someone
// just started (M8.3).
function ListTabs({ tab, onTab, view, reads, open }) {
    const keys = view.dm_keys || [];
    const waits = (c) => (unread(reads.get(c.id)) || (c.kind === 'dm' && needsCheck(keys, c.id, view.me.id)));
    const waiting = (dms) => view.channels.some((c) => (c.kind === 'dm') === dms && c.id !== open && hasMessages(c) && waits(c) && !reads.get(c.id)?.closed);
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
            <button type="button" class="flex min-w-0 flex-1 items-center gap-3 rounded px-1 py-1 text-left hover:bg-base-300/60" onClick={onEdit}
                aria-label="Edit your profile" title="Edit your profile">
                <Avatar member={me} size="md" online={online} />
                <span class="min-w-0 leading-tight">
                    <span class="block truncate text-sm font-medium">{me.display_name}</span>
                    <span class="block truncate text-xs text-base-content/60">@{me.username}</span>
                </span>
            </button>
            <button type="button" class="btn btn-ghost btn-sm btn-square" aria-label="App settings" title="App settings" onClick={() => openSettings('appearance')}>
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
function ChannelList({ denID, view, reads, members, open, staff, gone, muted, onOpen, onDialog }) {
    const { ungrouped, groups } = listed(view);
    const calls = new Map((view.calls || []).map((c) => [c.channel_id, c]));
    const [folded, setFolded] = useState(() => readFolded(stored(FOLDED + denID)));
    function fold(id) {
        const next = new Set(folded);
        if (!next.delete(id)) next.add(id);
        store(FOLDED + denID, JSON.stringify([...next]));
        setFolded(next);
    }
    const { call, speaking, volumes, watched, share } = useCall();
    const watching = new Set(watched.map((w) => w.member));
    // Staff reach each channel's and group's settings from a ⋯ beside it
    // (M4.2), which shows on hover or focus, and always on touch screens,
    // which have no hover. It shows once its row has the focus, since Tab
    // passes over what's hidden (M11).
    const settings = 'btn btn-ghost btn-xs btn-square invisible group-hover:visible group-focus-within:visible pointer-coarse:visible';
    const item = (c) => {
        const r = reads.get(c.id);
        const isUnread = c.kind === 'text' && unread(r) && c.id !== open;
        const voice = c.kind === 'voice';
        const inCall = voice && call?.den === denID && call.channel === c.id;
        const label = { den: view.name, channel: c.name };
        const managed = staff && !gone;
        return (
            <li key={c.id}>
                <div class="group relative">
                    <button
                        type="button"
                        class={`flex w-full items-center gap-2 rounded px-2 py-1 text-left ${managed ? 'pr-8' : ''} ${c.id === open || inCall ? 'bg-base-300' : 'hover:bg-base-300/60'}`}
                        onClick={() => (voice ? joinCall(denID, c.id, label) : onOpen(c.id))}
                        disabled={voice && gone}
                        title={voice ? (canCall() ? 'Join the call' : NO_CALLS) : undefined}
                    >
                        <span class={inCall ? 'text-success' : 'text-base-content/50'}>{voice ? <SpeakerIcon size="h-4 w-4" /> : <HashIcon />}</span>
                        <span class={`truncate ${isUnread ? 'font-bold' : ''}`}>{c.name}</span>
                        {c.staff_only && <span class="text-base-content/50" title="Staff only" aria-label="staff only"><LockIcon /></span>}
                        {muted?.has(c.id) && <span class="text-base-content/50" title="Muted" aria-label="muted"><BellIcon off size="h-3.5 w-3.5" /></span>}
                        {r?.mention_count > 0 && c.id !== open && <span class="badge badge-error badge-xs ml-auto">{r.mention_count}</span>}
                    </button>
                    {managed && (
                        <button type="button" class={`${settings} absolute right-1 top-1/2 -translate-y-1/2`} title="Channel settings"
                            aria-label={`Settings for ${c.name}`} onClick={() => onDialog({ kind: 'channel', channel: c })}>
                            ⋯
                        </button>
                    )}
                </div>
                {voice && (
                    <CallMembers denID={denID} call={calls.get(c.id)} members={members} me={view.me} speaking={inCall ? speaking : null}
                        volumes={volumes} onProfile={(m) => onDialog({ kind: 'profile', member: m })} inCall={inCall} label={label}
                        watched={watching} previewing={!!share?.preview} />
                )}
            </li>
        );
    };
    return (
        <nav aria-label="Channels" class="flex flex-col gap-2 p-2">
            <ul class="flex flex-col">{ungrouped.map(item)}</ul>
            {groups.map(({ group: g, channels }) => {
                // A folded group keeps what's open or busy in sight, and its
                // row says what waits in the rest (M11).
                const isFolded = folded.has(g.id);
                const shown = isFolded ? foldedShows(channels, open, calls) : channels;
                const marks = isFolded ? hiddenMarks(channels.filter((c) => !shown.includes(c)), reads) : null;
                return (
                    <div key={g.id}>
                        <div class="group flex h-6 select-none items-center justify-between gap-1 px-2 text-xs font-semibold uppercase text-base-content/60">
                            <button type="button" class="flex min-w-0 flex-1 cursor-pointer items-center gap-1 text-left uppercase hover:text-base-content"
                                aria-expanded={!isFolded} onClick={() => fold(g.id)}>
                                <ChevronIcon folded={isFolded} />
                                <span class="truncate">{g.name}</span>
                                {marks?.mentions > 0 && <span class="badge badge-error badge-xs font-normal normal-case">{marks.mentions}</span>}
                                {marks?.unread && !marks.mentions && <span class="h-2 w-2 shrink-0 rounded-full bg-base-content" aria-label="unread"></span>}
                            </button>
                            {staff && !gone && (
                                <button type="button" class={`${settings} font-normal normal-case`} aria-label={`Settings for the group ${g.name}`} title="Group settings"
                                    onClick={() => onDialog({ kind: 'group', group: g })}>
                                    ⋯
                                </button>
                            )}
                        </div>
                        <ul class="flex flex-col">{shown.map(item)}</ul>
                    </div>
                );
            })}
        </nav>
    );
}

// DMList shows this member's DMs, the most recently active first. A closed
// one stays out of it until it has a new message, unless it's open. A DM
// waiting for this member to compare codes says so.
// dms are as recentDMs lists them.
// muted holds the DMs the member muted, or is null while no kind of
// notification is on (M12.3).
function DMList({ dms, reads, open, partner, online, keys, me, muted, onOpen, onClose }) {
    if (dms.length === 0) {
        return (
            <p class="cursor-default select-none p-4 text-sm text-base-content/60">
                No direct messages yet. Open someone's profile to message them; it's end-to-end encrypted.
            </p>
        );
    }
    return (
        <nav aria-label="Direct messages" class="flex flex-col p-2">
            <ul class="flex flex-col">
                {dms.map((c) => {
                    const other = partner(c);
                    const r = reads.get(c.id);
                    const isUnread = unread(r) && c.id !== open;
                    return (
                        <li key={c.id} class="group relative">
                            <button
                                type="button"
                                class={`flex w-full items-center gap-2 rounded px-2 py-1 pr-8 text-left ${c.id === open ? 'bg-base-300' : 'hover:bg-base-300/60'} ${other?.left_at ? 'opacity-60' : ''}`}
                                onClick={() => onOpen(c.id)}
                            >
                                <Avatar member={other} size="sm" online={other && !other.left_at ? online.has(other.id) : undefined} />
                                <span class={`truncate ${isUnread ? 'font-bold' : ''}`}>{other ? other.display_name : 'Unknown member'}</span>
                                {muted?.has(c.id) && <span class="text-base-content/50" title="Muted" aria-label="muted"><BellIcon off size="h-3.5 w-3.5" /></span>}
                                {needsCheck(keys, c.id, me) && <span class="badge badge-warning badge-xs ml-auto" title="Compare check codes">Check</span>}
                                {r?.mention_count > 0 && c.id !== open && <span class="badge badge-error badge-xs ml-auto">{r.mention_count}</span>}
                            </button>
                            {/* Touch screens have no hover, so there the X always shows. */}
                            <button
                                type="button"
                                class="btn btn-ghost btn-xs btn-square absolute right-1 top-1/2 -translate-y-1/2 md:invisible md:group-hover:visible md:focus:visible"
                                aria-label={`Close the DM with ${other ? other.display_name : 'this member'}`}
                                title="Close"
                                onClick={() => onClose(c)}
                            >
                                ✕
                            </button>
                        </li>
                    );
                })}
            </ul>
        </nav>
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
function ChannelHeader({ channel, dm, me, elsewhere, online, membersOpen, muted, onMute, onChannels, onMembers, onProfile }) {
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
                    <button type="button" class="flex min-w-0 items-center gap-2 rounded text-left hover:bg-base-200" onClick={() => dm && onProfile(dm)}>
                        <Avatar member={dm} size="sm" online={dm && !dm.left_at ? online : undefined} />
                        <span class="truncate font-semibold">{dm ? dm.display_name : 'Unknown member'}</span>
                        {dm && <span class="hidden truncate text-sm text-base-content/60 sm:inline">@{dm.username}</span>}
                        <span class="flex shrink-0 items-center gap-1 text-xs text-base-content/50" title="End-to-end encrypted: only the two of you can read it">
                            <LockIcon size="h-3 w-3" />
                            Encrypted
                        </span>
                    </button>
                ) : (
                    <span class="flex shrink-0 items-center gap-1 font-semibold">
                        <span class="text-base-content/50"><HashIcon /></span>
                        {channel.name}
                    </span>
                )}
                {channel.description && (
                    <button
                        type="button"
                        class="flex min-w-0 items-center gap-1 rounded px-1 text-left text-sm text-base-content/60 enabled:hover:bg-base-200 enabled:hover:text-base-content disabled:cursor-default"
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
                    {muted !== null && channel.kind !== 'voice' && (
                        <button type="button" class="btn btn-ghost btn-sm btn-square" aria-pressed={muted} onClick={() => onMute(!muted)}
                            aria-label={`${muted ? 'Unmute' : 'Mute'} ${channel.kind === 'dm' ? `the DM with ${dm ? dm.display_name : 'this member'}` : `#${channel.name}`}`}
                            title={muted ? 'Unmute its notifications' : 'Mute its notifications'}>
                            <BellIcon off={muted} />
                        </button>
                    )}
                    <button type="button" class={`btn btn-ghost btn-sm btn-square ${membersOpen ? 'btn-active' : ''}`} onClick={onMembers}
                        aria-label={channel.kind === 'dm' ? 'Profile' : 'Members'} aria-pressed={membersOpen} title={channel.kind === 'dm' ? 'Profile' : 'Members'}>
                        <PeopleIcon />
                    </button>
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
