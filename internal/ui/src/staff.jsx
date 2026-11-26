// Staff's tools in a den's Manage: Members, every member in a table with
// when each was last seen, and the audit log of staff's acts, with the
// words each kind of line reads in.

import { useEffect, useState } from 'preact/hooks';
import { api } from './core/api.js';
import { Card, ErrorText, LongName, TextInput, Tip, buttonClass, day, dayTime, useAction } from './ui/components.jsx';
import { Avatar } from './ui/avatar.jsx';
import { MenuButton } from './ui/menus.jsx';
import { useNarrow } from './core/nav.js';
import { nameRank } from './chat/emoji.js';
import { formatSize } from './chat/files.jsx';
import { keptFor } from './core/retention.js';
import { mbps } from './voice/share.js';
import { RemoveMember, cardMenu, isStaff, rank } from './people.jsx';
import { MemberRolesDialog, roleNames } from './roles.jsx';

const ROLES = { owner: 'Owner', moderator: 'Moderator', member: 'Member' };

/** seenDay shows the day a member was last seen, which the den tells by
    its start in UTC, so it reads as that day wherever the page is */
export function seenDay(ms, locale) {
    return new Date(ms).toLocaleDateString(locale, { dateStyle: 'medium', timeZone: 'UTC' });
}

/** matchMember reports whether a member's username or display name
    starts with what was typed, q, or has a word that does, as completion
    matches them */
export function matchMember(m, q) {
    const t = q.trim().toLowerCase();
    return !t || nameRank(m.username, t) >= 0 || nameRank(m.display_name, t) >= 0;
}

// SORTS are the table's columns, each with how it orders two rows: the
// names alphabetically, staff first, and the latest joined or seen first,
// online above every day.
const SORTS = {
    name: ['Name', (a, b) => a.m.display_name.localeCompare(b.m.display_name)],
    role: ['Role', (a, b) => rank(b.m.role) - rank(a.m.role)],
    joined: ['Joined', (a, b) => b.m.joined_at - a.m.joined_at],
    seen: ['Last seen', (a, b) => b.seen - a.seen],
};

/** sortMembers orders rows, each { m, seen }, by a column of SORTS, ties
    going by name */
export function sortMembers(rows, by) {
    const order = SORTS[by][1];
    return [...rows].sort((a, b) => order(a, b) || SORTS.name[1](a, b));
}

// Members lists everyone in the den, narrowed by a few letters and sorted
// by a column's head, with when each was last seen where the den says,
// and on each row the menu a member's card has. On the phone layout a row
// is the names over the rest, and a list picks the order.
export function Members({ denID, view, role, onChanged }) {
    const narrow = useNarrow();
    const [query, setQuery] = useState('');
    const [by, setBy] = useState('name');
    const [seen, setSeen] = useState({});
    const [removing, setRemoving] = useState(null);
    const [editingRoles, setEditingRoles] = useState(null);
    const act = useAction();
    const load = useAction();
    useEffect(() => {
        if (!view.members_seen) return;
        load.run(async () => {
            const { seen: list } = await api.get(`/api/dens/${denID}/seen`);
            setSeen(Object.fromEntries(list.map((s) => [s.member_id, s.day])));
        });
    }, [denID, view.members_seen]);
    // Escape closes the removal alone: heard first, it never reaches
    // Manage's own, wherever the focus is.
    useEffect(() => {
        if (!removing) return undefined;
        const escape = (e) => {
            if (e.key !== 'Escape') return;
            e.stopPropagation();
            setRemoving(null);
        };
        document.addEventListener('keydown', escape, true);
        return () => document.removeEventListener('keydown', escape, true);
    }, [removing]);
    const online = new Set(view.online);
    const rows = view.members.filter((m) => !m.left_at && matchMember(m, query))
        .map((m) => ({ m, seen: online.has(m.id) ? Infinity : seen[m.id] || 0 }));
    const sorted = sortMembers(rows, by);
    // A den older than last seen tells none, so its column goes.
    const columns = Object.entries(SORTS).filter(([key]) => key !== 'seen' || view.members_seen);
    const seenOf = (r) => (r.seen === Infinity ? 'Online' : r.seen ? seenDay(r.seen) : '—');
    function menu(m) {
        const setRole = (next) => act.run(async () => {
            await api.put(`/api/dens/${denID}/members/${m.id}/role`, { role: next });
            await onChanged();
        });
        const run = {
            roles: () => setEditingRoles(m),
            promote: () => setRole('moderator'),
            demote: () => setRole('member'),
            remove: () => setRemoving(m),
        };
        const items = cardMenu({ self: m.id === view.me.id, role, member: m, kept: null, roles: !!view.has_roles })
            .map((item) => ({ ...item, disabled: act.busy, run: run[item.key] }));
        if (m.id === view.me.id && view.has_roles && isStaff(role) && !m.left_at) {
            items.unshift({ key: 'roles', label: 'Roles…', icon: 'roles', disabled: act.busy, run: run.roles });
        }
        return items.length > 0 && (
            <MenuButton label={`Options for ${m.display_name}`} items={items}
                button={{ class: `${buttonClass('quiet')} btn-square btn-sm`, title: 'More', 'data-member-menu': m.username, children: '⋯' }} />
        );
    }
    const roleSummary = (m) => roleNames(m.role_ids, view.roles).join(', ');
    const names = (m) => (
        <span class="flex min-w-0 items-center gap-2">
            <Avatar member={m} size="sm" />
            <span class="flex min-w-0 flex-col">
                <LongName name={m.display_name} member={m} />
                <span class="truncate text-xs text-base-content/60">@{m.username}</span>
            </span>
        </span>
    );
    return (
        <Card title={<>Members{' '}<Tip id="members-tip" side="right">Last seen is the day, in UTC, any of their devices last used the den.</Tip></>}>
            <div class="flex gap-2">
                <TextInput value={query} onInput={setQuery} placeholder="Find a member" aria-label="Find a member" autocomplete="off" data-members-find />
                {narrow && (
                    <select class="select w-36 shrink-0" value={by} aria-label="Sort by" onChange={(e) => setBy(e.currentTarget.value)}>
                        {columns.map(([key, [label]]) => <option key={key} value={key}>{label}</option>)}
                    </select>
                )}
            </div>
            <ErrorText message={act.error || load.error} />
            {sorted.length === 0 ? (
                <p class="text-sm text-base-content/70">Nobody matches.</p>
            ) : narrow ? (
                <ul class="flex flex-col divide-y divide-base-300" data-members>
                    {sorted.map((r) => (
                        <li key={r.m.id} class="flex items-center gap-2 py-2" data-member={r.m.username}>
                            <div class="flex min-w-0 flex-1 flex-col gap-1">
                                {names(r.m)}
                                <span class="text-xs text-base-content/70">
                                    {ROLES[r.m.role]}{roleSummary(r.m) && ` · ${roleSummary(r.m)}`} · Joined {day(r.m.joined_at)}{view.members_seen && ` · ${seenOf(r)}`}
                                </span>
                            </div>
                            {menu(r.m)}
                        </li>
                    ))}
                </ul>
            ) : (
                <table class="table table-sm" data-members>
                    <thead>
                        <tr>
                            {columns.map(([key, [label]]) => (
                                <th key={key} aria-sort={by === key ? (key === 'name' ? 'ascending' : 'descending') : undefined}>
                                    <button type="button" class={`inline-flex min-h-6 cursor-pointer items-center ${by === key ? 'text-base-content' : ''}`} onClick={() => setBy(key)}
                                        data-sort={key}>
                                        {label}{by === key && ' ▾'}
                                    </button>
                                </th>
                            ))}
                            <th><span class="sr-only">Options</span></th>
                        </tr>
                    </thead>
                    <tbody>
                        {sorted.map((r) => (
                            <tr key={r.m.id} data-member={r.m.username}>
                                <td class="max-w-56">{names(r.m)}</td>
                                <td>
                                    <span>{ROLES[r.m.role]}</span>
                                    {roleSummary(r.m) && <div class="text-xs text-base-content/60" data-member-roles>{roleSummary(r.m)}</div>}
                                </td>
                                <td>{day(r.m.joined_at)}</td>
                                {view.members_seen && <td>{seenOf(r)}</td>}
                                <td class="w-10">{menu(r.m)}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            )}
            {removing && <RemoveMember denID={denID} member={removing} onClose={() => {
                setRemoving(null);
                onChanged();
            }} />}
            {editingRoles && <MemberRolesDialog denID={denID} member={editingRoles} roles={view.roles || []} stopEscape onClose={(saved) => {
                setEditingRoles(null);
                if (saved) onChanged();
            }} />}
        </Card>
    );
}

// WINDOWS name the stretches of a member's messages a removal deletes.
const WINDOWS = { 3600: 'hour', 86400: 'day', 604800: '7 days' };

// listed joins phrases as a sentence lists them: "a", "a and b", "a, b
// and c".
function listed(parts) {
    return parts.length < 2 ? parts.join('') : `${parts.slice(0, -1).join(', ')} and ${parts.at(-1)}`;
}

// FIELDS are the settings a line may say changed, each with its label and
// how a value shows, as Manage's own fields name them.
const count = (v) => String(Number(v));
const FIELDS = {
    name: ['Name'],
    description: ['Description'],
    staff_only: ['Staff only', (v) => (v === 'true' ? 'Yes' : 'No')],
    roles: ['Roles'],
    channels_staff_only: ['Changed to staff only'],
    roles_added: ['Added roles'],
    roles_removed: ['Removed roles'],
    bitrate: ['Bitrate', (v) => `${Math.round(Number(v) / 1000)} kbps`],
    group: ['Group'],
    place: ['Place'],
    channel: ['Channel'],
    starts_at: ['Starts', (v, locale) => dayTime(Number(v), locale)],
    ends_at: ['Ends', (v, locale) => dayTime(Number(v), locale)],
    banner: ['Banner'],
    badge: ['Badge'],
    url: ['Public address'],
    file_size: ['Max file size', (v) => formatSize(Number(v))],
    member_storage: ['Storage per member', (v) => formatSize(Number(v))],
    den_storage: ['Total storage', (v) => formatSize(Number(v))],
    call_members: ['Max people per call', count],
    den_callers: ['Max people in calls', count],
    shares: ['Max shares at once', count],
    share_viewers: ['Max viewers per share', count],
    share_bitrate: ['Max bitrate', (v) => mbps(Number(v))],
    share_height: ['Max resolution', (v) => `${Number(v)}p`],
    share_fps: ['Max frame rate', (v) => `${Number(v)} fps`],
    retention: ['Auto-delete messages', (v) => (Number(v) > 0 ? keptFor(Number(v)) : 'Off')],
    sounds: ['Max sounds', count],
    sound_longest: ['Longest sound', (v) => `${Number(v)} seconds`],
    sound_wait: ['Wait between sounds', (v) => `${Number(v)} seconds`],
    emojis: ['Max emoji', count],
    member_events: ['Events per member', count],
};

const SUMMARY_FIELDS = new Set(['channels_staff_only', 'roles_added', 'roles_removed']);

/** changeText says what one change did: "Name: general → chat", or for
    one the den keeps no values of, "Description changed". A setting this
    page doesn't know shows by its own name. The den's settings call their
    name the den's */
export function changeText(c, kind, locale) {
    const [label, show = (v) => v] = FIELDS[c.field] || [c.field];
    const name = kind === 'den.updated' && c.field === 'name' ? 'Den name' : label;
    if (!c.from && !c.to) return `${name} changed`;
    if (SUMMARY_FIELDS.has(c.field) && !c.from && c.to) return `${name}: ${show(c.to, locale)}`;
    const value = (v) => (v ? show(v, locale) : 'none');
    return `${name}: ${value(c.from)} → ${value(c.to)}`;
}

// LISTED are the kinds whose changes show under their sentence; the rest
// say theirs in it.
const LISTED = new Set(['channel.updated', 'group.updated', 'event.updated', 'den.updated', 'role.deleted', 'member.roles']);

/** auditText says a line in words: { text, changes }, its sentence and
    the changes listed under it. nameOf gives a member's @username by ID.
    A kind this page doesn't know shows as itself, its changes listed, and
    so does a line that lacks what its kind's sentence names, which a den
    is free to send */
export function auditText(l, nameOf, locale) {
    const by = l.by ? nameOf(l.by) : 'The den';
    const on = l.member_id ? nameOf(l.member_id) : 'someone';
    const change = (field) => l.changes?.find((c) => c.field === field) || {};
    let lacking = false;
    const need = (v) => {
        lacking ||= !v;
        return v;
    };
    const channel = () => `#${need(l.channel)}`;
    const name = () => need(l.name);
    const known = {
        'member.removed': () => {
            const parts = [`${l.ban ? 'banned' : 'removed'} ${on}`];
            if (l.delete_messages) parts.push(`deleted their messages of the last ${WINDOWS[l.delete_messages] || `${l.delete_messages} seconds`}`);
            if (l.revoke_invite) parts.push('revoked their invite');
            return `${by} ${listed(parts)}`;
        },
        'ban.lifted': () => `${by} lifted ${on}'s ban`,
        'role.changed': () => (l.owner
            ? `An owner invite made ${nameOf(l.owner)} the owner, and ${on} a ${need(change('role').to)}`
            : `${by} made ${on} a ${need(change('role').to)}`),
        'role.created': () => `${by} made the role ${name()}`,
        'role.renamed': () => `${by} renamed the role ${need(change('name').from)} to ${name()}`,
        'role.deleted': () => `${by} deleted the role ${name()}`,
        'member.roles': () => `${by} changed ${on}'s roles`,
        'invite.created': () => `${by} made an invite`,
        'invite.revoked': () => `${by} revoked an invite`,
        'channel.created': () => `${by} made ${channel()}`,
        'channel.updated': () => `${by} changed ${channel()}`,
        'channel.deleted': () => `${by} deleted ${channel()}`,
        'group.created': () => `${by} made the group ${name()}`,
        'group.updated': () => `${by} changed the group ${name()}`,
        'group.deleted': () => `${by} deleted the group ${name()}`,
        'message.deleted': () => `${by} deleted a message of ${on}'s in ${channel()}`,
        'reactions.cleared': () => `${by} took ${l.emoji || `:${name()}:`} off a message of ${on}'s in ${channel()}`,
        // A call's line has no channel where the member was in none, or
        // it went before the line was written.
        'call.disconnected': () => `${by} disconnected ${on} from ${l.channel || 'a call'}`,
        'call.muted': () => `${by} muted ${on} in calls`,
        'call.unmuted': () => `${by} unmuted ${on} in calls`,
        'call.moved': () => (change('channel').to
            ? `${by} moved ${on} from ${need(change('channel').from)} to ${change('channel').to}`
            : `${by} moved ${on} to another call`),
        'record.reset': () => `${by} reset the longest call in ${need(l.channel)}`,
        'emoji.added': () => `${by} added the emoji :${name()}:`,
        'emoji.renamed': () => `${by} renamed the emoji :${need(change('name').from)}: to :${name()}:`,
        'emoji.deleted': () => `${by} deleted the emoji :${name()}:`,
        'sound.added': () => `${by} added the sound ${name()}`,
        'sound.renamed': () => `${by} renamed the sound ${need(change('name').from)} to ${name()}`,
        'sound.deleted': () => `${by} deleted the sound ${name()}`,
        'event.updated': () => `${by} changed ${on}'s event ${name()}`,
        'event.canceled': () => `${by} canceled ${on}'s event ${name()}`,
        'event.ended': () => `${by} ended ${on}'s event ${name()}`,
        'den.updated': () => `${by} changed the den's settings`,
    }[l.kind];
    const sentence = known?.();
    const plain = !known || lacking;
    return {
        text: plain ? `${by}: ${l.kind}` : sentence,
        changes: plain || LISTED.has(l.kind) ? (l.changes || []).map((c) => changeText(c, l.kind, locale)) : [],
    };
}

// AuditLog shows staff's acts, newest first, a page at a time, each in
// words with when, and what it changed under it.
export function AuditLog({ denID, view }) {
    const [lines, setLines] = useState(null);
    const [next, setNext] = useState('');
    const act = useAction();
    const members = new Map(view.members.map((m) => [m.id, m]));
    const nameOf = (id) => (members.has(id) ? `@${members.get(id).username}` : 'someone');
    const more = (before, had) => act.run(async () => {
        const page = await api.get(`/api/dens/${denID}/audit${before ? `?before=${before}` : ''}`);
        setLines([...had, ...page.lines]);
        setNext(page.next || '');
    });
    useEffect(() => {
        more('', []);
    }, [denID]);
    return (
        <Card title={<>Audit log{' '}<Tip id="audit-tip" side="right">What staff did here, for 90 days. Never what a message said, and nothing of DMs.</Tip></>}>
            <ErrorText message={act.error} />
            {lines === null ? (
                !act.error && <span class="loading loading-dots loading-sm"></span>
            ) : lines.length === 0 ? (
                <p class="text-sm text-base-content/70">Nothing yet.</p>
            ) : (
                <ul class="flex flex-col divide-y divide-base-300" data-audit>
                    {lines.map((l) => {
                        const { text, changes } = auditText(l, nameOf);
                        return (
                            <li key={l.id} class="flex flex-col gap-0.5 py-2 text-sm" data-audit-line={l.kind}>
                                <span class="flex flex-col sm:flex-row sm:items-baseline sm:gap-3">
                                    <span class="min-w-0 break-words sm:flex-1">{text}</span>
                                    <span class="shrink-0 text-xs text-base-content/60">{dayTime(l.at)}</span>
                                </span>
                                {changes.length > 0 && (
                                    <ul class="text-xs text-base-content/70">
                                        {changes.map((c, i) => <li key={i} class="break-words">{c}</li>)}
                                    </ul>
                                )}
                            </li>
                        );
                    })}
                </ul>
            )}
            {next && (
                <button type="button" class={`${buttonClass('quiet')} btn-sm self-start`} disabled={act.busy} onClick={() => more(next, lines)}>Show older</button>
            )}
        </Card>
    );
}
