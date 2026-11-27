// Unread state on message deletion: live clearing via applyReads, and
// markRead's fallback on older dens that omit last_message_id on delete.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { unread } from '../src/core/ids.js';

// Extend dom.js just enough to mount MessagePane and observe its PUT /read
// calls without a real browser or live socket.
Object.assign(Object.getPrototypeOf(globalThis.document.createElement('div')), { scrollHeight: 0, scrollTop: 0, clientHeight: 0 });
globalThis.document.visibilityState = 'visible';
globalThis.document.addEventListener = () => {};
globalThis.document.removeEventListener = () => {};
globalThis.requestAnimationFrame = (fn) => setTimeout(fn, 0);
globalThis.cancelAnimationFrame = (id) => clearTimeout(id);
globalThis.window = {
    location: { protocol: 'http:', host: 'localhost:18484', pathname: '/', search: '', hash: '', reload: () => {} },
    addEventListener: () => {},
    removeEventListener: () => {},
    matchMedia: () => ({ matches: false, addEventListener: () => {}, removeEventListener: () => {} }),
    requestAnimationFrame: globalThis.requestAnimationFrame,
    cancelAnimationFrame: globalThis.cancelAnimationFrame,
};
globalThis.WebSocket = class {
    static CONNECTING = 0;
    static OPEN = 1;
    constructor() {
        this.readyState = 1;
        this.listeners = {};
    }
    addEventListener(type, fn) {
        this.listeners[type] = fn;
        if (type === 'open') queueMicrotask(() => fn());
    }
    send() {}
    close() {}
};

const { applyReads } = await import('../src/chat/chat.jsx');
const { MessagePane, readTarget } = await import('../src/chat/messages.jsx');

test('readTarget clears stale last_message_id on older dens while guarding unloaded or scrolled views', () => {
    const loaded = (ids, extra = {}) => ({ loaded: true, hasNewer: false, messages: ids.map((id) => ({ id })), ...extra });

    // Older den: newest message 11 was deleted leaving read message 10.
    assert.equal(readTarget(loaded(['10']), '10', '11'), '11');
    // Older den: only message 1 was deleted leaving an empty channel.
    assert.equal(readTarget(loaded([]), '', '1'), '1');
    // Older den: newest 12 was deleted leaving unread 11 past read 10.
    assert.equal(readTarget(loaded(['10', '11']), '10', '12'), '12');

    // Live message newer than lastMessage prop advances to the loaded tail.
    assert.equal(readTarget(loaded(['10', '11']), '10', '10'), '11');
    // IDs compare numerically, not lexicographically.
    assert.equal(readTarget(loaded(['9']), '9', '10'), '10');

    // Already read, empty and read, unloaded, or scrolled away from tail: nothing to send.
    assert.equal(readTarget(loaded(['10']), '10', '10'), '');
    assert.equal(readTarget(loaded([]), '', ''), '');
    assert.equal(readTarget(loaded([]), '', undefined), '');
    assert.equal(readTarget({ loaded: false, hasNewer: false, messages: [] }, '', '11'), '');
    assert.equal(readTarget(loaded(['5'], { hasNewer: true }), '5', '11'), '');
});

test('applyReads clears unread immediately when read_state.updated carries a reverted or empty last_message_id', () => {
    const view = {
        read_states: [
            { channel_id: '1', last_message_id: '11', message_id: '10', mention_count: 0 },
            { channel_id: '2', last_message_id: '1', mention_count: 0 },
            { channel_id: '3', last_message_id: '12', message_id: '10', mention_count: 0 },
        ],
    };
    const byId = (v) => new Map(v.read_states.map((r) => [r.channel_id, r]));
    assert.equal(unread(byId(view).get('1')), true);
    assert.equal(unread(byId(view).get('2')), true);
    assert.equal(unread(byId(view).get('3')), true);

    const updated = applyReads(view, [
        { channel_id: '1', last_message_id: '10', message_id: '10', mention_count: 0 },
        { channel_id: '2', mention_count: 0 },
        { channel_id: '3', last_message_id: '11', message_id: '10', mention_count: 0 },
    ]);
    assert.equal(unread(byId(updated).get('1')), false, 'reverted to read position');
    assert.equal(unread(byId(updated).get('2')), false, 'emptied channel');
    assert.equal(unread(byId(updated).get('3')), true, 'still unread past 10');
});

// Mount MessagePane against a stubbed older den whose history no longer holds
// lastMessage, and verify unmount flushes PUT /read with lastMessage.
async function openAndMeasureRead({ channel, readPosition, lastMessage, history }) {
    const puts = [];
    const origFetch = globalThis.fetch;
    globalThis.fetch = async (path, opts = {}) => {
        if (opts.method === 'PUT' && path.endsWith('/read')) {
            puts.push({ path, body: JSON.parse(opts.body) });
            return { ok: true, status: 204, headers: { get: () => null }, text: async () => '' };
        }
        return {
            ok: true,
            status: 200,
            headers: { get: () => null },
            text: async () => JSON.stringify({ messages: history, has_older: false, has_newer: false }),
        };
    };
    const root = document.createElement('div');
    const me = { id: '1', username: 'alice', display_name: 'Alice' };
    const bob = { id: '2', username: 'bob', display_name: 'Bob' };
    const members = new Map([[me.id, me], [bob.id, bob]]);
    try {
        mount(h(MessagePane, {
            denID: 'den1',
            denURL: 'https://den.example',
            channel,
            me,
            members,
            readPosition,
            lastMessage,
            role: 'member',
            dm: channel.kind === 'dm' ? bob : null,
            typing: [],
            limits: { file_size: 1024 },
            retention: 0,
            offline: false,
            onProfile: () => {},
            onTyping: () => {},
        }), root);
        // Wait for Preact effects + history fetch + layout effect (checkEdges -> markRead).
        await new Promise((r) => setTimeout(r, 200));
        mount(null, root);
        await new Promise((r) => setTimeout(r, 20));
    } finally {
        globalThis.fetch = origFetch;
    }
    return puts;
}

test('MessagePane markRead fallback clears unread on older dens for deleted newest message, emptied channel, and DM', async () => {
    const olderMsg = { id: '10', channel_id: '5', author_id: '2', created_at: 1700000000000, revision: 1, text: 'kept' };
    const nonEmpty = await openAndMeasureRead({
        channel: { id: '5', kind: 'text', name: 'general' },
        readPosition: '10',
        lastMessage: '11',
        history: [olderMsg],
    });
    assert.deepEqual(nonEmpty, [{ path: '/api/dens/den1/channels/5/read', body: { message_id: '11' } }]);

    const emptied = await openAndMeasureRead({
        channel: { id: '6', kind: 'text', name: 'empty' },
        readPosition: '',
        lastMessage: '1',
        history: [],
    });
    assert.deepEqual(emptied, [{ path: '/api/dens/den1/channels/6/read', body: { message_id: '1' } }]);

    const dmEmptied = await openAndMeasureRead({
        channel: { id: '7', kind: 'dm', members: ['1', '2'] },
        readPosition: '',
        lastMessage: '21',
        history: [],
    });
    assert.deepEqual(dmEmptied, [{ path: '/api/dens/den1/channels/7/read', body: { message_id: '21' } }]);
});
