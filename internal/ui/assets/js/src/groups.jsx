// Group DMs (M16.3): what the page shows of a group, beside it and in it,
// and the dialogs that make one, add people to it and name it. The
// group's members are what its changes make them, which the local service
// checks: someone the den lists without a member's signed add shows
// nowhere here.

import { useState } from 'preact/hooks';
import { api } from './api.js';
import { HoverRow } from './animations.js';
import { Avatar } from './avatar.jsx';
import { DocsLink, ErrorText, Field, SubmitButton, TextInput, useAction } from './components.jsx';
import { LockIcon } from './icons.jsx';
import { Dialog } from './manage.jsx';
import { changeText } from './private.js';
import { VerifiedMark } from './private.jsx';
import { ExpirySetting } from './disappearing.jsx';

// MAX_MEMBERS is how many a group holds, its maker among them.
export const MAX_MEMBERS = 10;

// GroupIcon stands in a group's avatar.
export function GroupIcon({ size = 'h-8 w-8' }) {
    return (
        <span class={`flex shrink-0 items-center justify-center rounded-full bg-base-300 text-base-content/70 ${size}`} aria-hidden="true">
            <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.5">
                <circle cx="6" cy="5" r="2.5" />
                <path d="M1.5 13.5c0-2.5 2-4 4.5-4s4.5 1.5 4.5 4" />
                <path d="M10.5 3a2.5 2.5 0 010 5M12 9.5c1.5.5 2.5 2 2.5 4" />
            </svg>
        </span>
    );
}

// GroupPanel shows a group beside it, where a DM shows its other member's
// profile: its name, which any member changes, its members in the order
// they joined, each with their mark or Compare, and Remove for the member
// who removes others, the first of them; then Add people and Leave, and
// its time, which any member sets (M16.4), where expiry gives the group.
export function GroupPanel({ denID, view, channel, group, title, members, online, onCompare, onProfile, onAdd, onRename, expiry }) {
    const act = useAction();
    const me = view.me.id;
    const list = group?.members || [];
    const remover = list[0];
    const full = list.length >= MAX_MEMBERS;
    return (
        <div class="flex flex-col gap-3 p-3">
            <div class="flex items-start gap-2">
                <GroupIcon />
                <div class="min-w-0 flex-1">
                    <p class="break-words font-semibold" data-group-title>{title}</p>
                    <p class="text-xs text-base-content/60">{list.length} member{list.length === 1 ? '' : 's'}</p>
                </div>
                <button type="button" class="btn btn-ghost btn-xs" onClick={onRename} disabled={!!group?.broken}>Rename</button>
            </div>
            <p class="flex items-start gap-1 text-xs text-base-content/60">
                <span class="mt-0.5"><LockIcon size="h-3 w-3" /></span>
                <span>End-to-end encrypted: only its members can read it. <DocsLink icon page="join/#group-dms" label="How group DMs work" /></span>
            </p>
            {group?.broken && <p role="alert" class="text-sm text-error">{BROKEN}</p>}
            <ul class="flex flex-col" aria-label="The group's members" data-group-members>
                {list.map((id) => {
                    const m = members.get(id);
                    const self = id === me;
                    const kept = view.identities?.[id];
                    const name = m ? m.display_name : 'Unknown member';
                    return (
                        <HoverRow as="li" key={id} class="flex items-center gap-1" data-member={id}>
                            <button type="button" class="flex min-w-0 flex-1 items-center gap-2 rounded px-1 py-1 text-left hover:bg-base-300/60 pointer-coarse:min-h-11"
                                onClick={() => m && onProfile(m)}>
                                <Avatar member={m} size="sm" online={m && !m.left_at ? online.has(id) : undefined} />
                                <span class="min-w-0 truncate">{self ? `${name} (you)` : name}</span>
                                {id === remover && <span class="badge badge-ghost badge-xs shrink-0" title="Removes members: they made the group, or have been in it longest">Removes</span>}
                            </button>
                            {!self && kept?.verified && <VerifiedMark id={`group-verified-${channel.id}-${id}`} name={name} />}
                            {!self && m?.identity && !kept?.verified && (
                                <button type="button" class="btn btn-ghost btn-xs" onClick={() => onCompare(m)}>Compare</button>
                            )}
                            {me === remover && !self && !group?.broken && (
                                <button type="button" class="btn btn-ghost btn-xs text-error" aria-label={`Remove ${name} from the group`} disabled={act.busy}
                                    onClick={() => act.run(() => api.del(`/api/dens/${denID}/group-dms/${channel.id}/members/${id}`))}>
                                    Remove
                                </button>
                            )}
                        </HoverRow>
                    );
                })}
            </ul>
            <ErrorText message={act.error} />
            <div class="flex flex-wrap gap-2">
                <button type="button" class="btn btn-sm" disabled={full || !!group?.broken} title={full ? 'A group holds at most ten members' : undefined} onClick={onAdd}>
                    Add people
                </button>
                <button type="button" class="btn btn-outline btn-error btn-sm" disabled={act.busy}
                    onClick={() => act.run(() => api.post(`/api/dens/${denID}/group-dms/${channel.id}/leave`))}>
                    Leave
                </button>
            </div>
            {expiry && !group?.broken && <ExpirySetting denID={denID} channel={expiry} />}
        </div>
    );
}

export const BROKEN = "This group's changes don't check out, so someone may be in the middle. Nothing more is sent here.";

// PickPeople picks members of the den, to make a group with or to add to
// one: those still in the den, less those it leaves out, at most max. One
// whose Dens publishes no identity, being older, can't be picked yet. A
// new group may have a name.
export function PickPeople({ view, title, action, leaveOut = [], picked: start = [], max, min = 1, named = false, onPick, onClose }) {
    const act = useAction();
    const [picked, setPicked] = useState(start);
    const [query, setQuery] = useState('');
    const [name, setName] = useState('');
    const q = query.trim().toLowerCase();
    const shown = view.members
        .filter((m) => !m.left_at && m.id !== view.me.id && !leaveOut.includes(m.id))
        .filter((m) => !q || m.display_name.toLowerCase().includes(q) || m.username.includes(q))
        .sort((a, b) => a.display_name.localeCompare(b.display_name));
    function toggle(id) {
        setPicked((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : cur.length >= max ? cur : [...cur, id]));
    }
    return (
        <Dialog title={title} onClose={onClose}>
            <form class="flex flex-col gap-3" onSubmit={(e) => {
                e.preventDefault();
                act.run(() => onPick(picked, name.trim()));
            }}>
                {named && (
                    <Field label="Name, if you'd like one">
                        <TextInput value={name} onInput={setName} maxLength={32} placeholder="Named by its members" />
                    </Field>
                )}
                <input type="search" class="input input-sm w-full" placeholder="Search members" aria-label="Search members" value={query}
                    onInput={(e) => setQuery(e.currentTarget.value)} />
                <p class="text-xs text-base-content/60">{picked.length} picked, {max} at most</p>
                <ul class="flex max-h-72 flex-col overflow-y-auto" aria-label="Members to pick">
                    {shown.map((m) => {
                        const old = !m.identity;
                        return (
                            <li key={m.id}>
                                <label class={`flex items-center gap-2 rounded px-1 py-1 hover:bg-base-200 pointer-coarse:min-h-11 ${old ? 'opacity-60' : 'cursor-pointer'}`}>
                                    <input type="checkbox" class="checkbox checkbox-sm" checked={picked.includes(m.id)} disabled={old || (!picked.includes(m.id) && picked.length >= max)}
                                        onChange={() => toggle(m.id)} />
                                    <Avatar member={m} size="sm" />
                                    <span class="min-w-0 flex-1 truncate">{m.display_name}</span>
                                    {old && <span class="text-xs text-base-content/60">Needs an update</span>}
                                </label>
                            </li>
                        );
                    })}
                    {shown.length === 0 && <li class="p-2 text-sm text-base-content/60">Nobody to pick.</li>}
                </ul>
                <ErrorText message={act.error} />
                <div class="flex justify-end">
                    <button type="submit" class="btn btn-primary btn-sm" disabled={act.busy || picked.length < min}>
                        {act.busy && <span class="loading loading-spinner loading-sm"></span>}
                        {action}
                    </button>
                </div>
            </form>
        </Dialog>
    );
}

// RenameGroup names a group, or with nothing takes its name away.
export function RenameGroup({ denID, channel, name, onClose }) {
    const act = useAction();
    const [value, setValue] = useState(name || '');
    return (
        <Dialog title="Rename the group" onClose={onClose}>
            <form class="flex flex-col gap-3" onSubmit={(e) => {
                e.preventDefault();
                act.run(async () => {
                    await api.put(`/api/dens/${denID}/group-dms/${channel.id}/name`, { name: value.trim() });
                    onClose();
                });
            }}>
                <Field label="Name">
                    <TextInput value={value} onInput={setValue} maxLength={32} placeholder="Named by its members" />
                </Field>
                <p class="text-xs text-base-content/60">Its members see the name; the den sees it only sealed.</p>
                <ErrorText message={act.error} />
                <div class="flex justify-end">
                    <SubmitButton busy={act.busy} class="btn-primary btn-sm">Save</SubmitButton>
                </div>
            </form>
        </Dialog>
    );
}

// GroupLine is a change to a group, as a line among its messages.
export function GroupLine({ change, nameOf, me }) {
    return (
        <div class="divider my-1 h-auto cursor-default select-none text-xs text-base-content/60" data-group-change={change.kind}>
            <span class="max-w-[80%] shrink-0 whitespace-normal text-center">{changeText(change, nameOf, me)}</span>
        </div>
    );
}

// GroupGate stands in a group's composer's place while it can't take
// messages, as DMGate does a DM's.
export function GroupGate({ denID, gate, member, onCompare }) {
    const act = useAction();
    const name = member ? member.display_name : 'A member';
    let body;
    if (gate === 'checking') body = <p class="text-base-content/70">Checking the group's changes…</p>;
    else if (gate === 'broken') body = <p class="text-error">{BROKEN}</p>;
    else if (gate === 'alone') body = <p class="text-base-content/70">Nobody else is in this group. Add people to write here.</p>;
    else if (gate === 'update') body = <p class="text-base-content/70">{name}'s Dens needs an update before you can write here.</p>;
    else if (gate === 'held') {
        body = (
            <div class="flex flex-col gap-2" data-hold>
                <p>{name}'s keys changed since you compared codes. Compare again to check it's really them, or send anyway if you know why they changed.</p>
                <ErrorText message={act.error} />
                <div class="flex flex-wrap gap-2">
                    <button type="button" class="btn btn-primary btn-sm" onClick={() => member && onCompare(member)}>Compare</button>
                    <button type="button" class="btn btn-sm" disabled={act.busy || !member}
                        onClick={() => act.run(() => api.post(`/api/dens/${denID}/members/${member.id}/send-anyway`))}>
                        Send anyway
                    </button>
                </div>
            </div>
        );
    }
    return <div class="border-t border-base-300 px-4 py-3 text-sm" data-group-gate={gate}>{body}</div>;
}
