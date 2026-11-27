import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    COLLECTIONS, MAX_COLLECTIONS, canRemoveCollectionItem, channelCollectionsOf,
    channelCollectionsPath, pickerSections, withCollection, writableChannelCollections,
} from '../src/chat/collections.js';

test('channel collections route helpers round-trip valid channel IDs', () => {
    assert.equal(COLLECTIONS, 'collections');
    assert.equal(channelCollectionsPath('42'), 'channels/42/collections');
    assert.equal(channelCollectionsOf('channels/42/collections'), '42');
    assert.equal(channelCollectionsOf('collections'), '');
    assert.equal(channelCollectionsOf('channels/abc/collections'), '');
});

test('withCollection keeps creation order and applies live updates', () => {
    let list = [];
    list = withCollection(list, 'collection.created', { id: '2', name: 'Second', count: 0, created_at: 200 });
    list = withCollection(list, 'collection.created', { id: '1', channel_id: '10', name: 'First', who_adds: 'staff', count: 1, created_at: 100 });
    assert.deepEqual(list.map((c) => c.id), ['1', '2']);

    list = withCollection(list, 'collection.updated', { id: '1', channel_id: '10', name: 'Guides', who_adds: 'everyone', count: 1, created_at: 100 });
    assert.equal(list[0].name, 'Guides');
    assert.equal(list[0].who_adds, 'everyone');

    list = withCollection(list, 'collection.item_added', { collection_id: '1', channel_id: '10', message_id: '9', count: 2 });
    assert.equal(list[0].count, 2);

    list = withCollection(list, 'collection.item_removed', { collection_id: '1', channel_id: '10', message_id: '9', count: 1 });
    assert.equal(list[0].count, 1);

    list = withCollection(list, 'channel.deleted', { id: '10' });
    assert.deepEqual(list.map((c) => c.id), ['2'], 'channel delete drops its shared collections, keeping personal');

    list = withCollection(list, 'collection.deleted', { id: '2' });
    assert.deepEqual(list, []);
});

test('writableChannelCollections and canRemoveCollectionItem enforce permissions', () => {
    const cols = [
        { id: '1', channel_id: '10', name: 'Staff Col', who_adds: 'staff' },
        { id: '2', channel_id: '10', name: 'Open Col', who_adds: 'everyone' },
        { id: '3', channel_id: '11', name: 'Other Chan', who_adds: 'everyone' },
        { id: '4', name: 'Saved' },
    ];
    assert.deepEqual(writableChannelCollections(cols, '10', false).map((c) => c.id), ['2']);
    assert.deepEqual(writableChannelCollections(cols, '10', true).map((c) => c.id), ['1', '2']);
    assert.deepEqual(writableChannelCollections(cols, '', true), []);

    assert.equal(canRemoveCollectionItem(cols[3], { added_by: '99' }, '2', false), true, 'owner removes any personal item');
    assert.equal(canRemoveCollectionItem(cols[1], { added_by: '2' }, '2', false), true, 'adder removes own shared item');
    assert.equal(canRemoveCollectionItem(cols[1], { added_by: '1' }, '2', false), false, 'member cannot remove others');
    assert.equal(canRemoveCollectionItem(cols[1], { added_by: '1' }, '2', true), true, 'staff removes any shared item');
});

test('pickerSections offers lazy Saved when empty and quick-create for new names', () => {
    const shared = [
        { id: '1', channel_id: '10', name: 'Guides', who_adds: 'everyone' },
        { id: '2', channel_id: '10', name: 'Staff Pins', who_adds: 'staff' },
    ];
    // Zero personal collections -> virtual Saved shown.
    const empty = pickerSections({ collections: shared, channelID: '10', staff: false, query: '' });
    assert.equal(empty.lazySaved, true);
    assert.deepEqual(empty.shared.map((c) => c.id), ['1']);
    assert.equal(empty.createName, '');

    // Typing "Saved" matches virtual Saved without duplicating "+ Create".
    const typedSaved = pickerSections({ collections: shared, channelID: '10', staff: false, query: 'Saved' });
    assert.equal(typedSaved.lazySaved, true);
    assert.equal(typedSaved.createName, '');

    // Typing a new name hides virtual Saved and offers "+ Create".
    const typedNew = pickerSections({ collections: shared, channelID: '10', staff: false, query: '  Recipes  ' });
    assert.equal(typedNew.lazySaved, false);
    assert.equal(typedNew.createName, 'Recipes');

    // Once personal collections exist, virtual Saved disappears and duplicate names (any case) suppress create.
    const withPersonal = [...shared, { id: '9', name: 'Saved' }];
    const existing = pickerSections({ collections: withPersonal, channelID: '10', staff: true, query: 'saved' });
    assert.equal(existing.lazySaved, false);
    assert.deepEqual(existing.personal.map((c) => c.id), ['9']);
    assert.equal(existing.createName, '');

    // At 20 personal collections, quick-create is suppressed.
    const full = Array.from({ length: MAX_COLLECTIONS }, (_, i) => ({ id: String(i + 1), name: `P${i + 1}` }));
    const atCap = pickerSections({ collections: full, channelID: '', staff: false, query: 'One More' });
    assert.equal(atCap.createName, '');
});
