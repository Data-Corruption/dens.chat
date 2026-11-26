// Custom role helpers shared across channels, mentions, voice moves,
// events, profiles and Manage.

export const MAX_ROLES = 50;
export const MAX_ROLE_NAME = 32;

/** roleNames resolves role IDs against the den's visible roles, sorted by
    name. */
export function roleNames(roleIDs, roles) {
    if (!roleIDs?.length || !roles?.length) return [];
    const byID = new Map(roles.map((r) => [r.id, r.name]));
    return roleIDs.map((id) => byID.get(id)).filter(Boolean).sort((a, b) => a.localeCompare(b));
}

/** roleTipText explains who can see a role-restricted channel. */
export function roleTipText(roleIDs, roles) {
    const names = roleNames(roleIDs, roles);
    if (names.length === 0) return 'Restricted by role: staff and members with a required role can see it.';
    return `Restricted to ${names.join(', ')} (and staff).`;
}

/** roleFlipChannels returns the channels whose only role is roleID, which
    the den will flip to staff-only if that role is deleted. */
export function roleFlipChannels(roleID, channels) {
    return (channels || []).filter((c) => c.role_ids?.length === 1 && c.role_ids[0] === roleID);
}

/** canSeeChannel reports whether member can see a text or voice channel. */
export function canSeeChannel(channel, member) {
    if (!channel || !member) return false;
    if (member.role === 'owner' || member.role === 'moderator') return true;
    if (channel.staff_only) return false;
    if (!channel.role_ids?.length) return true;
    const held = member.role_ids || [];
    return held.some((id) => channel.role_ids.includes(id));
}
