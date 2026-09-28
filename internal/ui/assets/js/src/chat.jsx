// A den's chat: the channel sidebar and the open channel.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onEvent } from './events.js';
import { compareIds, unread } from './ids.js';
import { Markdown, Preview, firstLine } from './markdown.jsx';
import { MessagePane } from './messages.jsx';
import { ChannelDialog, GroupDialog } from './manage.jsx';

const LAST_CHANNEL = 'DENS_LAST_CHANNEL:';
// Changes to these reload the den's state rather than being applied here.
const STRUCTURAL = new Set([
    'member.joined', 'channel.created', 'channel.updated', 'channel.deleted', 'channels.reordered',
    'group.created', 'group.updated', 'group.deleted', 'groups.reordered',
]);

function remember(denID, channelID) {
    try {
        localStorage.setItem(LAST_CHANNEL + denID, channelID);
    } catch {
        // Storage can be unavailable; the first channel opens instead.
    }
}

function remembered(denID) {
    try {
        return localStorage.getItem(LAST_CHANNEL + denID) || '';
    } catch {
        return '';
    }
}

// mentions reports whether text mentions username, by the same rule the
// den counts mentions with (see denproto.Mentions).
function mentions(text, username) {
    let inBlock = false;
    for (const line of text.split('\n')) {
        if (line.trim().startsWith('```')) {
            inBlock = !inBlock;
            continue;
        }
        if (inBlock) continue;
        const outside = line.split('`').filter((_, i) => i % 2 === 0).join(' ');
        for (const m of outside.matchAll(/(^|[^A-Za-z0-9_@])@([A-Za-z0-9_]{2,32})(?![A-Za-z0-9_])/g)) {
            if (m[2].toLowerCase() === username) return true;
        }
    }
    return false;
}

export function Chat({ denID, channelID, navigate }) {
    const [view, setView] = useState(null);
    const [error, setError] = useState('');
    const [dialog, setDialog] = useState(null);
    // On a narrow screen the channel list and the channel take turns.
    const [listOpen, setListOpen] = useState(!channelID);
    const reloadTimer = useRef(null);
    const openRef = useRef(channelID);

    async function load() {
        try {
            setView(await api.get(`/api/dens/${denID}/state`));
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
                if (status) setView((v) => (v ? { ...v, state: status.state, error: status.error, name: status.name } : v));
                return;
            }
            if (msg.t !== 'den' || msg.d.den !== denID) return;
            if (msg.d.reset) return reloadSoon();
            for (const e of msg.d.events || []) {
                if (STRUCTURAL.has(e.t)) {
                    reloadSoon();
                } else if (e.t === 'message.created' || e.t === 'read_state.updated') {
                    setView((v) => (v ? applyRead(v, e, openRef.current) : v));
                }
            }
        });
        return () => {
            stop();
            clearTimeout(reloadTimer.current);
        };
    }, [denID]);

    if (error && !view) return <div class="p-6"><div role="alert" class="alert alert-error">{error}</div></div>;
    if (!view) return <div class="p-6"><span class="loading loading-spinner"></span></div>;

    const text = view.channels.filter((c) => c.kind === 'text');
    let channel = view.channels.find((c) => c.id === channelID) || text.find((c) => c.id === remembered(denID)) || text[0];
    if (channel && channel.kind !== 'text') channel = text[0];
    openRef.current = channel?.id;
    if (channel) remember(denID, channel.id);
    const members = new Map(view.members.map((m) => [m.id, m]));
    const reads = new Map(view.read_states.map((r) => [r.channel_id, r]));
    const owner = view.role === 'owner';

    return (
        <div class="flex h-full min-h-0">
            <aside class={`${listOpen ? 'flex' : 'hidden'} w-full shrink-0 flex-col overflow-y-auto bg-base-200 md:flex md:w-64`}>
                <div class="border-b border-base-300 p-3">
                    <div class="flex items-center justify-between gap-2">
                        <span class="truncate font-semibold">{view.name}</span>
                        <span class={`badge badge-xs ${view.state === 'connected' ? 'badge-success' : 'badge-warning'}`} title={view.error || view.state}></span>
                    </div>
                    {view.state !== 'connected' && <p class="text-xs text-warning">{view.error || 'Reconnecting…'}</p>}
                </div>
                <ChannelList view={view} reads={reads} open={channel?.id} owner={owner}
                    onOpen={(id) => {
                        setListOpen(false);
                        navigate(`/den/${denID}/${id}`);
                    }}
                    onDialog={setDialog}
                />
                {owner && (
                    <div class="mt-auto flex gap-1 p-2">
                        <button type="button" class="btn btn-ghost btn-xs" onClick={() => setDialog({ kind: 'channel' })}>+ Channel</button>
                        <button type="button" class="btn btn-ghost btn-xs" onClick={() => setDialog({ kind: 'group' })}>+ Group</button>
                    </div>
                )}
            </aside>
            <section class={`${listOpen ? 'hidden' : 'flex'} min-w-0 flex-1 flex-col md:flex`}>
                {channel ? (
                    <>
                        <ChannelHeader key={channel.id} channel={channel} me={view.me} owner={owner} onChannels={() => setListOpen(true)}
                            onSettings={() => setDialog({ kind: 'channel', channel })} />
                        <MessagePane
                            key={`${denID}:${channel.id}`}
                            denID={denID}
                            channel={channel}
                            me={view.me}
                            members={members}
                            readPosition={reads.get(channel.id)?.message_id}
                            canModerate={owner}
                        />
                    </>
                ) : (
                    <div class="flex flex-col items-start gap-2 p-6 text-base-content/70">
                        <button type="button" class="btn btn-ghost btn-sm md:hidden" onClick={() => setListOpen(true)}>Channels</button>
                        <p>{owner ? 'This den has no text channels yet. Create one to start talking.' : 'This den has no text channels yet.'}</p>
                    </div>
                )}
            </section>
            {dialog?.kind === 'channel' && <ChannelDialog denID={denID} view={view} channel={dialog.channel} onClose={() => setDialog(null)} />}
            {dialog?.kind === 'group' && <GroupDialog denID={denID} view={view} group={dialog.group} onClose={() => setDialog(null)} />}
        </div>
    );
}

// applyRead keeps unread marks and mention counts current between reloads.
function applyRead(view, e, openChannel) {
    const reads = view.read_states.map((r) => {
        if (r.channel_id !== e.d.channel_id) return r;
        if (e.t === 'read_state.updated') return { ...r, message_id: e.d.message_id, mention_count: e.d.mention_count };
        const next = { ...r };
        if (compareIds(e.d.id, next.last_message_id) > 0) next.last_message_id = e.d.id;
        if (e.d.author_id === view.me.id) next.message_id = e.d.id;
        else if (e.d.channel_id !== openChannel && mentions(e.d.text, view.me.username)) next.mention_count++;
        return next;
    });
    return { ...view, read_states: reads };
}

function ChannelList({ view, reads, open, owner, onOpen, onDialog }) {
    const ungrouped = view.channels.filter((c) => !c.group_id).sort((a, b) => a.position - b.position);
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
        <nav class="flex flex-col gap-2 p-2">
            <ul class="flex flex-col">{ungrouped.map(item)}</ul>
            {groups.map((g) => (
                <div key={g.id}>
                    <div class="flex items-center justify-between px-2 text-xs font-semibold uppercase text-base-content/60">
                        <span class="truncate">{g.name}</span>
                        {owner && (
                            <button type="button" class="btn btn-ghost btn-xs" aria-label={`Group settings for ${g.name}`} onClick={() => onDialog({ kind: 'group', group: g })}>⚙</button>
                        )}
                    </div>
                    <ul class="flex flex-col">
                        {view.channels.filter((c) => c.group_id === g.id).sort((a, b) => a.position - b.position).map(item)}
                    </ul>
                </div>
            ))}
        </nav>
    );
}

function ChannelHeader({ channel, me, owner, onChannels, onSettings }) {
    const [expanded, setExpanded] = useState(false);
    const hasMore = channel.description && channel.description.trim() !== firstLine(channel.description).trim();
    return (
        <header class="border-b border-base-300 px-4 py-2">
            <div class="flex min-w-0 items-center gap-2">
                <button type="button" class="btn btn-ghost btn-sm btn-square md:hidden" onClick={onChannels} aria-label="Channels">
                    <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                        <path d="M2 4h12M2 8h12M2 12h12" />
                    </svg>
                </button>
                <span class="shrink-0 font-semibold"># {channel.name}</span>
                {channel.description && !expanded && (
                    <button type="button" class="min-w-0 truncate text-left text-sm text-base-content/60" onClick={() => setExpanded(true)} title={hasMore ? 'Show the full description' : undefined}>
                        <Preview text={channel.description} me={me} />
                    </button>
                )}
                {owner && <button type="button" class="btn btn-ghost btn-xs ml-auto" onClick={onSettings} aria-label="Channel settings">⚙</button>}
            </div>
            {expanded && channel.description && (
                <div class="mt-1 max-h-64 overflow-y-auto rounded bg-base-200 p-2 text-sm">
                    <Markdown text={channel.description} me={me} />
                    <button type="button" class="btn btn-ghost btn-xs" onClick={() => setExpanded(false)}>Collapse</button>
                </div>
            )}
        </header>
    );
}
