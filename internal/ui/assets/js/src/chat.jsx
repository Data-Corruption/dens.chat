// A den's chat: the sidebar of channels and DMs, the open channel, and the
// member list.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { Avatar } from './avatar.jsx';
import { onConnection, onEvent, sendTyping, showChannel } from './events.js';
import { compareIds, unread } from './ids.js';
import { Markdown, Preview, firstLine } from './markdown.jsx';
import { MessagePane } from './messages.jsx';
import { ChannelDialog, GroupDialog } from './manage.jsx';
import { EditProfile, MemberList, ProfileCard, RemoveMember, isStaff } from './people.jsx';

const LAST_CHANNEL = 'DENS_LAST_CHANNEL:';
const MEMBERS_OPEN = 'DENS_MEMBERS_OPEN';
// A typing notice shows this long unless another arrives; senders repeat
// theirs while they type.
const TYPING_SHOWN = 6000;
// Changes to these reload the den's state rather than being applied here.
const STRUCTURAL = new Set([
    'member.joined', 'member.updated', 'member.left', 'channel.created', 'channel.updated', 'channel.deleted',
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

// pick chooses the channel to show: the one in the address, else the last
// one shown in this den, else the first text channel.
function pick(view, denID, channelID) {
    if (!view) return null;
    const shown = view.channels.find((c) => c.id === channelID);
    if (hasMessages(shown)) return shown;
    const last = view.channels.find((c) => c.id === stored(LAST_CHANNEL + denID));
    return hasMessages(last) ? last : view.channels.find((c) => c.kind === 'text') || null;
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

export function Chat({ denID, channelID, navigate }) {
    const [view, setView] = useState(null);
    const [error, setError] = useState('');
    const [dialog, setDialog] = useState(null);
    // On a narrow screen the channel list and the channel take turns.
    const [listOpen, setListOpen] = useState(!channelID);
    const [membersOpen, setMembersOpen] = useState(() => stored(MEMBERS_OPEN) === '1');
    const [online, setOnline] = useState(() => new Set());
    const [typing, setTyping] = useState(() => new Map());
    const [visible, setVisible] = useState(document.visibilityState === 'visible');
    const [live, setLive] = useState(true);
    const reloadTimer = useRef(null);
    useEffect(() => onConnection(setLive), []);

    async function load() {
        try {
            const next = await api.get(`/api/dens/${denID}/state`);
            setView(next);
            setOnline(new Set(next.online));
            setError('');
        } catch (e) {
            setError(e.message);
        }
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
                const status = msg.d.dens.find((d) => d.den_id === denID);
                if (status) setView((v) => (v ? { ...v, state: status.state, error: status.error, name: status.name, role: status.role } : v));
                return;
            }
            if (msg.t !== 'den' || msg.d.den !== denID) return;
            if (msg.d.reset) return reloadSoon();
            for (const e of msg.d.events || []) {
                if (STRUCTURAL.has(e.t)) reloadSoon();
                else if (e.t === 'presence') setOnline((cur) => applyPresence(cur, e.d));
                else if (e.t === 'typing') setTyping((cur) => withTyper(cur, e.d.channel_id, e.d.member_id, Date.now() + TYPING_SHOWN));
                else if (e.t === 'message.created') setTyping((cur) => withTyper(cur, e.d.channel_id, e.d.author_id, 0));
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
    // The den sends typing only where this member is looking.
    const shown = visible && channel ? channel.id : '';
    useEffect(() => showChannel(denID, shown), [denID, shown]);

    if (error && !view) return <div class="p-6"><div role="alert" class="alert alert-error">{error}</div></div>;
    if (!view) return <div class="p-6"><span class="loading loading-spinner"></span></div>;

    if (channel) store(LAST_CHANNEL + denID, channel.id);
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
    // What waits elsewhere, for the channels button on a narrow screen.
    const elsewhere = { mentions: 0, unread: false };
    for (const c of view.channels) {
        const r = reads.get(c.id);
        if (!hasMessages(c) || c.id === channel?.id || !r) continue;
        elsewhere.mentions += r.mention_count;
        elsewhere.unread ||= unread(r);
    }

    function openChannel(id) {
        setListOpen(false);
        navigate(`/den/${denID}/${id}`);
    }

    function toggleMembers() {
        store(MEMBERS_OPEN, membersOpen ? '0' : '1');
        setMembersOpen(!membersOpen);
    }

    async function message(member) {
        const opened = await api.post(`/api/dens/${denID}/dms`, { member_id: member.id });
        await load();
        setDialog(null);
        openChannel(opened.id);
    }

    async function forget() {
        await api.del(`/api/dens/${denID}`);
        navigate('/');
    }

    return (
        <div class="flex h-full min-h-0">
            <aside class={`${listOpen ? 'flex' : 'hidden'} w-full shrink-0 flex-col overflow-y-auto bg-base-200 md:flex md:w-64`}>
                <div class="border-b border-base-300 p-3">
                    <div class="flex items-center justify-between gap-2">
                        <span class="truncate font-semibold">{view.name}</span>
                        <span class={`badge badge-xs ${live && view.state === 'connected' ? 'badge-success' : 'badge-warning'}`} title={live ? view.error || view.state : 'Not in touch with Dens on this computer'}></span>
                    </div>
                    {live && view.state !== 'connected' && <p class="text-xs text-warning">{view.error || 'Reconnecting…'}</p>}
                </div>
                <ChannelList view={view} reads={reads} open={channel?.id} staff={staff} onOpen={openChannel} onDialog={setDialog} />
                <DMList dms={view.channels.filter((c) => c.kind === 'dm')} reads={reads} open={channel?.id} partner={partner} online={online} onOpen={openChannel} />
                <div class="mt-auto flex items-center gap-1 border-t border-base-300 p-2">
                    <button type="button" class="flex min-w-0 flex-1 items-center gap-2 rounded px-1 py-1 text-left hover:bg-base-300/60"
                        onClick={() => setDialog({ kind: 'profile', member: members.get(view.me.id) || view.me })} aria-label="Your profile">
                        <Avatar member={view.me} size="sm" online={live && view.state === 'connected'} />
                        <span class="truncate text-sm">{view.me.display_name}</span>
                    </button>
                    {staff && !gone && (
                        <>
                            <button type="button" class="btn btn-ghost btn-xs" onClick={() => setDialog({ kind: 'channel' })}>+ Channel</button>
                            <button type="button" class="btn btn-ghost btn-xs" onClick={() => setDialog({ kind: 'group' })}>+ Group</button>
                        </>
                    )}
                </div>
            </aside>
            <section class={`${listOpen ? 'hidden' : 'flex'} min-w-0 flex-1 flex-col md:flex`}>
                {gone && (
                    <div role="status" class="alert alert-warning alert-soft m-2 flex flex-wrap">
                        <span>{closed}</span>
                        <button type="button" class="btn btn-sm" onClick={forget}>Remove from this computer</button>
                    </div>
                )}
                {channel ? (
                    <>
                        <ChannelHeader key={channel.id} channel={channel} dm={dm} me={view.me} staff={staff && !gone} elsewhere={elsewhere}
                            online={dm && online.has(dm.id)} membersOpen={membersOpen}
                            onChannels={() => setListOpen(true)} onMembers={toggleMembers}
                            onProfile={(m) => setDialog({ kind: 'profile', member: m })}
                            onSettings={() => setDialog({ kind: 'channel', channel })} />
                        <MessagePane
                            key={`${denID}:${channel.id}`}
                            denID={denID}
                            channel={channel}
                            me={view.me}
                            members={members}
                            readPosition={reads.get(channel.id)?.message_id}
                            role={view.role}
                            dm={dm}
                            typing={typers}
                            closed={closed}
                            onProfile={(m) => setDialog({ kind: 'profile', member: m })}
                            onTyping={() => sendTyping(denID, channel.id)}
                        />
                    </>
                ) : (
                    <div class="flex flex-col items-start gap-2 p-6 text-base-content/70">
                        <button type="button" class="btn btn-ghost btn-sm md:hidden" onClick={() => setListOpen(true)}>Channels</button>
                        <p>{staff ? 'This den has no text channels yet. Create one to start talking.' : 'This den has no text channels yet.'}</p>
                    </div>
                )}
            </section>
            {membersOpen && (
                <aside class="fixed inset-0 z-40 flex flex-col overflow-y-auto bg-base-200 md:static md:z-auto md:w-60 md:shrink-0">
                    <div class="flex items-center justify-between border-b border-base-300 p-3">
                        <span class="font-semibold">Members</span>
                        <button type="button" class="btn btn-ghost btn-sm md:hidden" onClick={toggleMembers} aria-label="Close the member list">✕</button>
                    </div>
                    <MemberList members={view.members} online={online} onProfile={(m) => setDialog({ kind: 'profile', member: m })} />
                </aside>
            )}
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
            {dialog?.kind === 'edit' && <EditProfile denID={denID} me={view.me} onClose={() => setDialog(null)} />}
            {dialog?.kind === 'remove' && <RemoveMember denID={denID} member={dialog.member} onClose={() => setDialog(null)} />}
        </div>
    );
}

function applyReads(view, reads) {
    const changed = new Map(reads.map((r) => [r.channel_id, r]));
    return { ...view, read_states: view.read_states.map((r) => changed.get(r.channel_id) || r) };
}

function ChannelList({ view, reads, open, staff, onOpen, onDialog }) {
    const listed = view.channels.filter((c) => c.kind !== 'dm');
    const ungrouped = listed.filter((c) => !c.group_id).sort((a, b) => a.position - b.position);
    const groups = [...view.groups].sort((a, b) => a.position - b.position);
    const item = (c) => {
        const r = reads.get(c.id);
        const isUnread = c.kind === 'text' && unread(r) && c.id !== open;
        return (
            <li key={c.id}>
                <button
                    type="button"
                    class={`flex w-full items-center gap-2 rounded px-2 py-1 text-left ${c.id === open ? 'bg-base-300' : 'hover:bg-base-300/60'} ${c.kind !== 'text' ? 'opacity-60' : ''}`}
                    onClick={() => c.kind === 'text' && onOpen(c.id)}
                    disabled={c.kind !== 'text'}
                    title={c.kind === 'voice' ? 'Voice channel' : undefined}
                >
                    <span class="text-base-content/50">{c.kind === 'voice' ? '🔊' : '#'}</span>
                    <span class={`truncate ${isUnread ? 'font-bold' : ''}`}>{c.name}</span>
                    {c.staff_only && <span class="text-xs text-base-content/50" title="Staff only">🔒</span>}
                    {r?.mention_count > 0 && c.id !== open && <span class="badge badge-error badge-xs ml-auto">{r.mention_count}</span>}
                </button>
            </li>
        );
    };
    return (
        <nav aria-label="Channels" class="flex flex-col gap-2 p-2">
            <ul class="flex flex-col">{ungrouped.map(item)}</ul>
            {groups.map((g) => (
                <div key={g.id}>
                    <div class="flex cursor-default select-none items-center justify-between px-2 text-xs font-semibold uppercase text-base-content/60">
                        <span class="truncate">{g.name}</span>
                        {staff && (
                            <button type="button" class="btn btn-ghost btn-xs" aria-label={`Group settings for ${g.name}`} onClick={() => onDialog({ kind: 'group', group: g })}>⚙</button>
                        )}
                    </div>
                    <ul class="flex flex-col">
                        {listed.filter((c) => c.group_id === g.id).sort((a, b) => a.position - b.position).map(item)}
                    </ul>
                </div>
            ))}
        </nav>
    );
}

// DMList shows this member's DMs, the most recently active first.
function DMList({ dms, reads, open, partner, online, onOpen }) {
    if (dms.length === 0) return null;
    const sorted = [...dms].sort((a, b) => compareIds(reads.get(b.id)?.last_message_id, reads.get(a.id)?.last_message_id) || compareIds(b.id, a.id));
    return (
        <nav aria-label="Direct messages" class="flex flex-col p-2 pt-0">
            <h3 class="cursor-default select-none px-2 pb-1 text-xs font-semibold uppercase text-base-content/60">Direct messages</h3>
            <ul class="flex flex-col">
                {sorted.map((c) => {
                    const other = partner(c);
                    const r = reads.get(c.id);
                    const isUnread = unread(r) && c.id !== open;
                    return (
                        <li key={c.id}>
                            <button
                                type="button"
                                class={`flex w-full items-center gap-2 rounded px-2 py-1 text-left ${c.id === open ? 'bg-base-300' : 'hover:bg-base-300/60'} ${other?.left_at ? 'opacity-60' : ''}`}
                                onClick={() => onOpen(c.id)}
                            >
                                <Avatar member={other} size="sm" online={other && !other.left_at ? online.has(other.id) : undefined} />
                                <span class={`truncate ${isUnread ? 'font-bold' : ''}`}>{other ? other.display_name : 'Unknown member'}</span>
                                {r?.mention_count > 0 && c.id !== open && <span class="badge badge-error badge-xs ml-auto">{r.mention_count}</span>}
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
// more than fits.
function ChannelHeader({ channel, dm, me, staff, elsewhere, online, membersOpen, onChannels, onMembers, onProfile, onSettings }) {
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
                    {elsewhere.mentions > 0 ? (
                        <span class="badge badge-error badge-xs absolute -right-1 -top-1">{elsewhere.mentions}</span>
                    ) : elsewhere.unread ? (
                        <span class="absolute right-0.5 top-0.5 h-2 w-2 rounded-full bg-base-content"></span>
                    ) : null}
                </button>
                {dm !== null && channel.kind === 'dm' ? (
                    <button type="button" class="flex min-w-0 items-center gap-2 rounded text-left hover:bg-base-200" onClick={() => dm && onProfile(dm)}>
                        <Avatar member={dm} size="sm" online={dm && !dm.left_at ? online : undefined} />
                        <span class="truncate font-semibold">{dm ? dm.display_name : 'Unknown member'}</span>
                        {dm && <span class="hidden truncate text-sm text-base-content/60 sm:inline">@{dm.username}</span>}
                    </button>
                ) : (
                    <span class="shrink-0 font-semibold"># {channel.name}</span>
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
                    {staff && channel.kind === 'text' && (
                        <button type="button" class="btn btn-ghost btn-xs" onClick={onSettings} aria-label="Channel settings">⚙</button>
                    )}
                    <button type="button" class={`btn btn-ghost btn-sm btn-square ${membersOpen ? 'btn-active' : ''}`} onClick={onMembers}
                        aria-label="Members" aria-pressed={membersOpen} title="Members">
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
