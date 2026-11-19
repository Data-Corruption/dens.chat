// Link previews at the first run (M22.7): a browser new to Dens is asked,
// one switch on for both, opening into one each once off, and keeps its
// answer.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { ALL_PREVIEWS, LinkPreviews, previewsKept } from '../assets/js/src/linkpreviews.jsx';

// A new browser's storage, which the page reads as it loads: links.js is
// imported only once it's in place.
const kept = new Map();
globalThis.localStorage = {
    get length() {
        return kept.size;
    },
    key: (i) => [...kept.keys()][i] ?? null,
    getItem: (k) => kept.get(k) ?? null,
    setItem: (k, v) => kept.set(k, String(v)),
    removeItem: (k) => kept.delete(k),
};
const links = await import('../assets/js/src/links.js');

const storage = (entries) => ({ length: entries.length, key: (i) => entries[i]?.[0] ?? null });

test('a browser is new to Dens while its storage holds nothing of Dens', () => {
    assert.equal(links.newToDens(storage([])), true);
    assert.equal(links.newToDens(storage([['other', '1']])), true, "another page's on the same origin");
    assert.equal(links.newToDens(storage([['other', '1'], ['DENS_THEME', 'dark']])), false);
    assert.equal(links.newToDens(storage([['DENS_LAST_CHANNEL:abc', '4']])), false, 'a channel last opened');
    assert.equal(links.newToDens(storage([['DENS_YOUTUBE', '0'], ['DENS_STEAM', '0']])), false, 'an answer, off');
});

test('Link previews is one switch, on, which opens into a switch for each once off', () => {
    const root = document.createElement('div');
    let choice = ALL_PREVIEWS;
    const show = () => mount(h(LinkPreviews, { choice, onChange: (c) => { choice = c; show(); } }), root);
    const box = (id) => root.find((n) => n.attributes.id === id);
    const on = (id) => 'checked' in box(id).attributes;
    show();
    assert.ok(on('link-previews'));
    assert.equal(box('link-previews-youtube'), null);
    assert.deepEqual(previewsKept(choice), { youtube: true, steam: true });

    box('link-previews').dispatch('change', { currentTarget: { checked: false } });
    assert.ok(!on('link-previews'));
    assert.ok(!on('link-previews-youtube') && !on('link-previews-steam'), 'each starts off');
    assert.deepEqual(previewsKept(choice), { youtube: false, steam: false });

    box('link-previews-youtube').dispatch('change', { currentTarget: { checked: true } });
    assert.ok(on('link-previews-youtube') && !on('link-previews-steam'));
    assert.deepEqual(previewsKept(choice), { youtube: true, steam: false });

    box('link-previews').dispatch('change', { currentTarget: { checked: true } });
    assert.equal(box('link-previews-steam'), null, 'on again, it closes');
    assert.deepEqual(previewsKept(choice), { youtube: true, steam: true });
    mount(null, root);
});

test('a new browser is asked until it answers, and keeps the answer, off as well as on', () => {
    assert.equal(links.asksLinkPreviews(), true);
    assert.equal(links.youTubePlayers(), false, 'off while it waits');
    links.setLinkPreviews({ youtube: true, steam: false });
    assert.equal(links.asksLinkPreviews(), false);
    assert.equal(links.youTubePlayers(), true);
    assert.equal(links.steamCards(), false);
    assert.deepEqual([kept.get('DENS_YOUTUBE'), kept.get('DENS_STEAM')], ['1', '0']);
    // Off is kept as an answer, so the browser isn't new again.
    links.setYouTubePlayers(false);
    assert.equal(kept.get('DENS_YOUTUBE'), '0');
    assert.equal(links.newToDens(globalThis.localStorage), false);
});
