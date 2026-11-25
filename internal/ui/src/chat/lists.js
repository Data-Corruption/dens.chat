// How a den's channel list orders what it shows, which the switcher and
// the keys that walk the list order the same way.

import { isPrivate } from '../private/private.js';

// listed arranges a den's channels, DMs aside, as its channel list shows
// them: those in no group, then each group's, each by
// position.
export function listed(view) {
    const byPosition = (a, b) => a.position - b.position;
    const channels = view.channels.filter((c) => !isPrivate(c));
    return {
        ungrouped: channels.filter((c) => !c.group_id).sort(byPosition),
        groups: [...view.groups].sort(byPosition).map((group) => ({ group, channels: channels.filter((c) => c.group_id === group.id).sort(byPosition) })),
    };
}

// inOrder is a den's text and voice channels in the list's order.
export function inOrder(view) {
    const { ungrouped, groups } = listed(view);
    return [...ungrouped, ...groups.flatMap((g) => g.channels)];
}
