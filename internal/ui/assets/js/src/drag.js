// Staff dragging channels and groups into order, and members from one call
// to another, on a desktop (M17.2), by HTML's drag and drop, as the message
// pane takes files. A channel's drop sends what Move up, Move down and
// Group send from its settings, its place in its list, which the den
// already takes; a group's, its place among the groups; and a member's,
// the voice channel to move their call to, which Move to in their menu in
// the call sends too, for touch screens and keyboards.

// The drags' types, which a page reads before the drop; their data is the
// channel's, the group's or the member's ID.
export const DRAG_CHANNEL = 'application/x-dens-channel';
export const DRAG_GROUP = 'application/x-dens-group';
export const DRAG_MEMBER = 'application/x-dens-member';

// dragged says which of these a drag carries, from its types: "channel",
// "group" or "member", or "" for anything else, as files.
export function dragged(e) {
    const types = [...(e.dataTransfer?.types || [])];
    if (types.includes(DRAG_CHANNEL)) return 'channel';
    if (types.includes(DRAG_GROUP)) return 'group';
    if (types.includes(DRAG_MEMBER)) return 'member';
    return '';
}

const byPosition = (a, b) => a.position - b.position;
const listedKind = (c) => c.kind === 'text' || c.kind === 'voice';

// siblings is a channel list in order: a group's, or with '' the channels
// in none. DMs and group DMs are in neither.
function siblings(view, group) {
    return view.channels.filter((c) => listedKind(c) && (c.group_id || '') === group).sort(byPosition);
}

// channelDrop is what a channel dropped in the list sends the den: its
// group, '' for none, where that changes, and its place among the group's
// other channels. It lands beside near, after it if after, or with no near
// at the group's start. A drop where it is already sends nothing (null).
export function channelDrop(view, id, { group, near = '', after = false }) {
    const moving = view.channels.find((c) => c.id === id && listedKind(c));
    if (!moving) return null;
    const from = moving.group_id || '';
    const others = siblings(view, group).filter((c) => c.id !== id);
    let at = 0;
    if (near) {
        const i = others.findIndex((c) => c.id === near);
        if (i < 0) return null;
        at = after ? i + 1 : i;
    }
    if (from === group) {
        return siblings(view, from).findIndex((c) => c.id === id) === at ? null : { position: at };
    }
    return { position: at, group_id: group };
}

// groupDrop is what a group dropped beside another, near, sends the den:
// its place among the groups, after near if after, or nothing where it is
// already.
export function groupDrop(view, id, { near, after = false }) {
    const groups = [...view.groups].sort(byPosition);
    const current = groups.findIndex((g) => g.id === id);
    const others = groups.filter((g) => g.id !== id);
    const i = others.findIndex((g) => g.id === near);
    if (current < 0 || i < 0) return null;
    const at = after ? i + 1 : i;
    return at === current ? null : { position: at };
}

// moveTargets are the voice channels staff may move a member's call to,
// in the list's order: every one but the call's own, from, and a
// staff-only one only for a member who's staff, since the den moves a
// member only where they could join.
export function moveTargets(view, member, from) {
    const staff = member?.role === 'owner' || member?.role === 'moderator';
    const groups = [...view.groups].sort(byPosition);
    return [siblings(view, ''), ...groups.map((g) => siblings(view, g.id))]
        .flat()
        .filter((c) => c.kind === 'voice' && c.id !== from && (staff || !c.staff_only));
}
