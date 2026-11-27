// Collections logic without DOM: routes, live state updates, permission checks
// and Save picker filtering.

import { compareIds } from '../core/ids.js';

export const COLLECTIONS = 'collections';
export const MAX_COLLECTIONS = 20;
export const MAX_COLLECTION_ITEMS = 200;
export const MAX_COLLECTION_NAME = 50;

export function channelCollectionsPath(channelID) {
    return `channels/${channelID}/collections`;
}

export function channelCollectionsOf(channelID) {
    const m = typeof channelID === 'string' && channelID.match(/^channels\/([0-9]+)\/collections$/);
    return m ? m[1] : '';
}

function sortCollections(list) {
    return [...list].sort((a, b) => (a.created_at - b.created_at) || compareIds(a.id, b.id));
}

// withCollection applies one collection (or channel.deleted) event to
// view.collections, keeping creation order.
export function withCollection(list, type, d) {
    const cur = list || [];
    if (!d) return cur;
    if (type === 'collection.created' || type === 'collection.updated') {
        const rest = cur.filter((c) => c.id !== d.id);
        return sortCollections([...rest, d]);
    }
    if (type === 'collection.deleted') {
        return cur.filter((c) => c.id !== d.id);
    }
    if (type === 'collection.item_added' || type === 'collection.item_removed') {
        return cur.map((c) => (c.id === d.collection_id ? { ...c, count: d.count } : c));
    }
    if (type === 'channel.deleted') {
        return cur.filter((c) => c.channel_id !== d.id);
    }
    return cur;
}

export function personalCollections(list) {
    return (list || []).filter((c) => !c.channel_id);
}

export function channelCollections(list, channelID) {
    return (list || []).filter((c) => c.channel_id === channelID);
}

// writableChannelCollections returns shared collections in channelID the member
// may add messages to: all of them for staff, or those with who_adds === 'everyone'.
export function writableChannelCollections(list, channelID, staff) {
    if (!channelID) return [];
    return channelCollections(list, channelID).filter((c) => staff || c.who_adds === 'everyone');
}

// canRemoveCollectionItem reports whether the member may remove an item from a
// collection: always on personal collections, and on shared collections for
// staff or whoever added the item.
export function canRemoveCollectionItem(col, item, meID, staff) {
    if (!col) return false;
    if (!col.channel_id) return true;
    return !!staff || item?.added_by === meID;
}

// pickerSections splits collections into the Save picker's sections for a
// message in channelID (empty for DMs/Notes) given the current filter text.
// When the member has no personal collections yet, lazySaved offers a virtual
// "Saved" row that creates "Saved" on click; typing an unused name offers
// quick-creating it as a personal collection.
export function pickerSections({ collections = [], channelID = '', staff = false, query = '' }) {
    const trimmed = query.trim();
    const q = trimmed.toLowerCase();
    const allPersonal = personalCollections(collections);
    const allShared = writableChannelCollections(collections, channelID, staff);
    const personal = allPersonal.filter((c) => !q || c.name.toLowerCase().includes(q));
    const shared = allShared.filter((c) => !q || c.name.toLowerCase().includes(q));
    const lazySaved = allPersonal.length === 0 && (!q || 'saved'.includes(q));
    const nameRunes = [...trimmed].length;
    const dupPersonal = allPersonal.some((c) => c.name.toLowerCase() === q);
    const matchesLazy = allPersonal.length === 0 && q === 'saved';
    const createName = nameRunes > 0 && nameRunes <= MAX_COLLECTION_NAME && allPersonal.length < MAX_COLLECTIONS && !dupPersonal && !matchesLazy
        ? trimmed
        : '';
    return { personal, shared, lazySaved, createName };
}
