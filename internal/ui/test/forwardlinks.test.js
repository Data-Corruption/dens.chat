// Forward links: display states for forwarded channel sources (same den, cross
// den, hidden/deleted channel, unjoined den, DM/Notes plain) and click jumps
// without querying hidden channels or unjoined dens.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { CHANNEL_GONE, ForwardedLabel, LinkContext, MESSAGE_GONE, followForward, forwardLabel, forwardPlace } from '../src/chat/msglinks.jsx';
import { MessageRow } from '../src/chat/messages.jsx';

globalThis.window = {
    location: { protocol: 'http:', host: 'localhost:8484' },
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    addEventListener() {},
};
document.addEventListener = () => {};
globalThis.WebSocket = class {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = 1;
    addEventListener(type, fn) {
        if (type === 'open') queueMicrotask(fn);
    }
    send() {}
    close() {}
};

// 32-byte den IDs in base64url.
const HOME = 'TqFAWIFQdzzjqs54au739ASc4QD6ZJyU-73blg8dqUI';
const OTHER = '2SmKENGwc1g33EvYXaxkGw887yekfl1TpU8vP1svz_o';
const OTHER_FAIL = '9SmKENGwc1g33EvYXaxkGw887yekfl1TpU8vP1svz_o';
const ELSEWHERE = 'ext2Puj2LriOR0KnYPkS0LGbzViyuUiZl4S6zBWn9Nc';

const here = {
    me: { id: '1' },
    members: [{ id: '1', username: 'alice', display_name: 'Alice' }, { id: '2', username: 'bob', display_name: 'Bob' }],
    channels: [
        { id: '41', kind: 'text', name: 'general' },
        { id: '42', kind: 'voice', name: 'Lounge' },
        { id: '90', kind: 'dm', members: ['1', '2'] },
        { id: '95', kind: 'notes', members: ['1'] },
    ],
};
const there = {
    me: { id: '7' },
    members: [{ id: '7', username: 'alice', display_name: 'Alice' }],
    channels: [
        { id: '5', kind: 'text', name: 'random' },
        { id: '6', kind: 'voice', name: 'Call' },
    ],
};
const dens = [
    { den_id: HOME, name: 'Home Den', url: 'https://home.example' },
    { den_id: OTHER, name: 'Den A', url: 'https://other.example' },
    { den_id: OTHER_FAIL, name: 'Broken Den', url: 'https://broken.example' },
];
const baseLinks = { denID: HOME, view: here, dens };

function service(answers) {
    const asked = [];
    globalThis.fetch = async (path) => {
        asked.push(path);
        let status = 200;
        let body = answers[path];
        if (typeof body === 'function') {
            try {
                body = body();
            } catch (e) {
                status = 503;
                body = { error: e.message };
            }
        }
        if (body === undefined) [status, body] = [404, { error: 'no such thing' }];
        return { ok: status < 400, status, headers: { get: () => null }, text: async () => JSON.stringify(body) };
    };
    return asked;
}

const around = (den, channel, message) => `/api/dens/${den}/channels/${channel}/messages?around=${message}&limit=1`;
const tick = (ms = 20) => new Promise((resolve) => setTimeout(resolve, ms));

test('forwardLabel handles all five display states without exposing unjoined den IDs or non-text channels', () => {
    const label = (source, viewOf = () => there) => forwardLabel(source, forwardPlace(source, baseLinks, viewOf), HOME);

    // 1. DM/Notes forward (no source) -> plain Forwarded
    assert.deepEqual(label(null), { text: 'Forwarded', chip: '' });
    assert.deepEqual(label(undefined), { text: 'Forwarded', chip: '' });

    // 2. Same den, visible text channel -> #general
    assert.deepEqual(label({ den_id: HOME, channel_id: '41', message_id: '100' }), { text: 'Forwarded from ', chip: '#general' });

    // 3. Cross den, visible text channel -> Den A · #random
    assert.deepEqual(label({ den_id: OTHER, channel_id: '5', message_id: '200' }), { text: 'Forwarded from ', chip: 'Den A · #random' });

    // 4. Hidden/deleted/non-text channel or failed state -> Forwarded from another channel
    assert.deepEqual(label({ den_id: HOME, channel_id: '999', message_id: '1' }), { text: 'Forwarded from another channel', chip: '' });
    assert.deepEqual(label({ den_id: HOME, channel_id: '42', message_id: '1' }), { text: 'Forwarded from another channel', chip: '' }, 'voice channel');
    assert.deepEqual(label({ den_id: HOME, channel_id: '90', message_id: '1' }), { text: 'Forwarded from another channel', chip: '' }, 'DM channel');
    assert.deepEqual(label({ den_id: HOME, channel_id: '95', message_id: '1' }), { text: 'Forwarded from another channel', chip: '' }, 'Notes channel');
    assert.deepEqual(label({ den_id: OTHER, channel_id: '777', message_id: '1' }), { text: 'Forwarded from another channel', chip: '' });
    assert.deepEqual(label({ den_id: OTHER, channel_id: '5', message_id: '1' }, () => false), { text: 'Forwarded from another channel', chip: '' }, 'failed state');
    assert.deepEqual(label({ den_id: OTHER, channel_id: '5', message_id: '1' }, () => null), { text: 'Forwarded', chip: '' }, 'loading state');

    // 5. Unjoined den -> Forwarded from another den
    assert.deepEqual(label({ den_id: ELSEWHERE, channel_id: '5', message_id: '200' }), { text: 'Forwarded from another den', chip: '' });
});

test('followForward jumps to visible text channel messages and reports gone messages or vanished channels', async () => {
    const opened = [];
    const links = { ...baseLinks, open: (...args) => opened.push(args) };
    const asked = service({
        [around(HOME, '41', '100')]: { messages: [{ id: '100' }] },
        [around(HOME, '41', '101')]: { messages: [{ id: '102' }] },
        [`/api/dens/${OTHER}/state`]: there,
        [around(OTHER, '5', '200')]: { messages: [{ id: '200' }] },
    });

    assert.equal(await followForward({ den_id: HOME, channel_id: '41', message_id: '100' }, links), '');
    assert.equal(await followForward({ den_id: HOME, channel_id: '41', message_id: '101' }, links), MESSAGE_GONE);
    assert.equal(await followForward({ den_id: HOME, channel_id: '999', message_id: '1' }, links), CHANNEL_GONE);
    assert.equal(await followForward({ den_id: HOME, channel_id: '90', message_id: '1' }, links), CHANNEL_GONE);
    assert.equal(await followForward({ den_id: OTHER, channel_id: '5', message_id: '200' }, links), '');
    assert.equal(await followForward({ den_id: OTHER, channel_id: '777', message_id: '200' }, links), CHANNEL_GONE);
    assert.equal(await followForward({ den_id: ELSEWHERE, channel_id: '5', message_id: '200' }, links), '');
    assert.deepEqual(opened, [[HOME, '41', '100'], [OTHER, '5', '200']]);
    assert.ok(asked.every((p) => !p.includes(ELSEWHERE) && !p.includes('/channels/999/') && !p.includes('/channels/777/') && !p.includes('/channels/90/')),
        `never queries hidden channels or unjoined dens: ${asked}`);
});

test('ForwardedLabel renders chips, handles clicks and gone messages, and never queries for hidden or unjoined sources', async () => {
    const opened = [];
    const links = { ...baseLinks, open: (...args) => opened.push(args) };
    const asked = service({
        [around(HOME, '41', '100')]: { messages: [{ id: '100' }] },
        [around(HOME, '41', '101')]: { messages: [] },
        [`/api/dens/${OTHER}/state`]: there,
        [around(OTHER, '5', '200')]: { messages: [{ id: '200' }] },
        [`/api/dens/${OTHER_FAIL}/state`]: () => {
            throw new Error('offline');
        },
    });

    const root = document.createElement('div');
    const row = (m) => {
        const none = () => {};
        const me = here.members[0];
        return h(MessageRow, {
            m, author: me, me, canReply: true, canEdit: false, canForward: true, canDelete: true, link: '', editorNames: [], candidates: [], canReact: false,
            menuOpen: false, nameOf: () => '', onReact: none, onReactPick: none, onRemoveReactions: none, onUpdated: none, onPolled: none, onMenu: none,
            onReply: none, onEdit: none, onForward: none, onEditDone: none, onDelete: none, onJump: none, onView: none, onProfile: none, onToggle: none,
            denID: HOME, channelID: '41',
        });
    };

    const base = { channel_id: '41', author_id: '1', created_at: Date.UTC(2026, 10, 6, 12), revision: 1, text: 'hello', forwarded: true };
    mount(h(LinkContext.Provider, { value: links },
        row({ ...base, id: '1' }),
        row({ ...base, id: '2', forward_source: { den_id: HOME, channel_id: '41', message_id: '100' } }),
        row({ ...base, id: '3', forward_source: { den_id: HOME, channel_id: '41', message_id: '101' } }),
        row({ ...base, id: '4', forward_source: { den_id: HOME, channel_id: '999', message_id: '1' } }),
        row({ ...base, id: '5', forward_source: { den_id: ELSEWHERE, channel_id: '5', message_id: '200' } }),
        row({ ...base, id: '6', forward_source: { den_id: OTHER, channel_id: '5', message_id: '200' } }),
        row({ ...base, id: '7', forward_source: { den_id: OTHER_FAIL, channel_id: '5', message_id: '200' } }),
    ), root);

    // Wait for Preact effects (no rAF in dom.js -> ~100ms) and fetchOther promises.
    await tick(150);

    const headerOf = (id) => {
        const msg = root.find((n) => n.attributes['data-id'] === id);
        return msg.find((n) => 'data-forwarded' in n.attributes);
    };

    assert.equal(headerOf('1').textContent.trim(), 'Forwarded');
    assert.equal(headerOf('1').find((n) => 'data-forward-link' in n.attributes), null);

    assert.equal(headerOf('2').textContent.trim(), 'Forwarded from #general');
    const sameBtn = headerOf('2').find((n) => 'data-forward-link' in n.attributes);
    assert.equal(sameBtn.localName, 'button');
    assert.equal(sameBtn.textContent, '#general');

    assert.equal(headerOf('4').textContent.trim(), 'Forwarded from another channel');
    assert.equal(headerOf('4').find((n) => 'data-forward-link' in n.attributes), null);

    assert.equal(headerOf('5').textContent.trim(), 'Forwarded from another den');
    assert.equal(headerOf('5').find((n) => 'data-forward-link' in n.attributes), null);
    assert.ok(!headerOf('5').textContent.includes(ELSEWHERE), 'never reveals den ID');

    assert.equal(headerOf('6').textContent.trim(), 'Forwarded from Den A · #random');
    const crossBtn = headerOf('6').find((n) => 'data-forward-link' in n.attributes);
    assert.equal(crossBtn.localName, 'button');
    assert.equal(crossBtn.textContent, 'Den A · #random');

    assert.equal(headerOf('7').textContent.trim(), 'Forwarded from another channel');
    assert.equal(headerOf('7').find((n) => 'data-forward-link' in n.attributes), null);

    // Click same-den, gone-message, and cross-den chips.
    const goneBtn = headerOf('3').find((n) => 'data-forward-link' in n.attributes);
    sameBtn.dispatch('click', { preventDefault() {} });
    goneBtn.dispatch('click', { preventDefault() {} });
    crossBtn.dispatch('click', { preventDefault() {} });
    await tick(50);

    assert.deepEqual(opened, [[HOME, '41', '100'], [OTHER, '5', '200']]);
    assert.equal(headerOf('3').find((n) => n.attributes.role === 'status')?.textContent, MESSAGE_GONE);
    assert.ok(asked.every((p) => !p.includes(ELSEWHERE) && !p.includes('/channels/999/')),
        `never queried unjoined den or hidden channel: ${asked}`);
    mount(null, root);
});
