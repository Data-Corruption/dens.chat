import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { EXPIRY_TIMES, afterText, dueBy, expiryText, nextDue, placeAt, shortAfter } from '../assets/js/src/private.js';
import { ExpiryBadge, ExpiryLine, ExpirySetting } from '../assets/js/src/disappearing.jsx';

const names = new Map([['1', 'Alice'], ['2', 'Bob']]);
const nameOf = (id) => names.get(id) || 'Unknown member';

// findAll lists the elements at or under node that test accepts.
function findAll(node, test, out = []) {
    if (node.nodeType === 1 && test(node)) out.push(node);
    for (const c of node.childNodes || []) findAll(c, test, out);
    return out;
}

test("a time says how long it is, in words and in a header's few letters", () => {
    assert.deepEqual(EXPIRY_TIMES.slice(1).map(afterText), ['1 hour', '1 day', '1 week', '4 weeks']);
    assert.deepEqual(EXPIRY_TIMES.slice(1).map(shortAfter), ['1h', '1d', '1w', '4w']);
    // The times of seconds a development instance's den takes.
    assert.equal(afterText(1), '1 second');
    assert.equal(afterText(10), '10 seconds');
    assert.equal(afterText(90), '90 seconds');
    assert.equal(afterText(120), '2 minutes');
    assert.equal(shortAfter(10), '10s');
});

test("a time's line says who set it, when, and to what, at the time they set it", () => {
    const at = Date.UTC(2026, 10, 5, 12);
    const on = `On ${new Date(at).toLocaleDateString(undefined, { dateStyle: 'medium' })}`;
    assert.equal(expiryText(86400, { by: '2', at }, nameOf, '1'), `${on}, Bob set messages to disappear 1 day after they're sent.`);
    assert.equal(expiryText(3600, { by: '1', at }, nameOf, '1'), `${on}, you set messages to disappear 1 hour after they're sent.`);
    assert.equal(expiryText(0, { by: '2', at }, nameOf, '1'), `${on}, Bob turned off disappearing messages.`);
    assert.equal(expiryText(10, { by: '9', at }, nameOf, '1'), `${on}, Unknown member set messages to disappear 10 seconds after they're sent.`);

    // Before the first message sent after it, or at the end.
    const messages = [{ id: '10', created_at: 100 }, { id: '11', created_at: 300 }];
    assert.deepEqual([...placeAt(messages, [{ by: '2', at: 200 }], (x) => x.at, {}).before.keys()], ['11']);
    assert.deepEqual(placeAt(messages, [{ by: '2', at: 400 }], (x) => x.at, {}).after, [{ by: '2', at: 400 }]);
    assert.deepEqual(placeAt(messages, [{ by: '2', at: 400 }], (x) => x.at, { hasNewer: true }).after, []);

    const root = document.createElement('div');
    mount(h(ExpiryLine, { channel: { id: '7', kind: 'dm', expires_after: 604800, expires_set: { by: '2', at } }, nameOf, me: '1' }), root);
    assert.equal(root.textContent, `${on}, Bob set messages to disappear 1 week after they're sent.`);
    mount(null, root);
});

test('messages whose time came are due, and the next of the rest goes at its time', () => {
    const messages = [{ id: '1' }, { id: '2', expires_at: 1000 }, { id: '3', expires_at: 3000 }, { id: '4', expires_at: 2000 }];
    assert.deepEqual(dueBy(messages, 999).map((m) => m.id), []);
    assert.deepEqual(dueBy(messages, 2000).map((m) => m.id), ['2', '4']);
    assert.equal(nextDue(messages, 999), 1000);
    assert.equal(nextDue(messages, 2000), 3000);
    assert.equal(nextDue(messages, 3000), 0);
});

// A window for the confirm dialog's place in the history, as the nav
// tests' is.
globalThis.window = {
    location: { pathname: '/den/d/7', hash: '' },
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    addEventListener() {},
    removeEventListener() {},
    history: {
        state: null,
        pushState(state) {
            this.state = state;
        },
        replaceState(state) {
            this.state = state;
        },
        go() {},
    },
};

// service answers the page's requests as the local service would, and
// records them with their bodies.
function service() {
    const asked = [];
    globalThis.fetch = async (path, opts = {}) => {
        asked.push({ method: opts.method || 'GET', path, body: opts.body ? JSON.parse(opts.body) : undefined });
        return { ok: true, status: 204, headers: { get: () => null }, text: async () => '' };
    };
    return asked;
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 5));

test("a DM's time is set beside it, once the member confirms it, and the header says it", async () => {
    const asked = service();
    const root = document.createElement('div');
    const options = () => findAll(root, (n) => n.localName === 'option').map((o) => o.textContent);
    const select = () => findAll(root, (n) => n.localName === 'select')[0];
    const dialog = () => findAll(root, (n) => n.attributes?.role === 'dialog')[0];
    const button = (text) => findAll(root, (n) => n.localName === 'button' && n.textContent.trim() === text)[0];
    const pick = (value) => select().dispatch('change', { currentTarget: { value: String(value) } });
    mount(h(ExpirySetting, { denID: 'd', channel: { id: '7', kind: 'dm' } }), root);
    assert.deepEqual(options(), ['Off', '1 hour', '1 day', '1 week', '4 weeks']);
    assert.ok(!/disappear/.test(findAll(root, (n) => n.localName === 'p').map((p) => p.textContent).join('')), 'no words under the list');

    // A pick asks first, with the way to the guide, and Cancel keeps it as it was.
    pick(86400);
    await settle();
    assert.equal(dialog()?.attributes['aria-label'], 'Turn on disappearing messages?');
    assert.match(dialog().textContent, /New messages disappear for everyone 1 day after they're sent\./);
    const guide = findAll(dialog(), (n) => n.attributes?.['data-docs-link'] !== undefined)[0];
    assert.equal(guide.attributes.href, 'https://dens.chat/docs/join/#disappearing-messages');
    assert.equal(guide.attributes.target, '_blank');
    button('Cancel').dispatch('click', {});
    await settle();
    assert.equal(dialog(), undefined);
    assert.equal(asked.length, 0);
    assert.equal(String(select().value ?? select().attributes.value), '0');

    // Confirmed, it goes to the den.
    pick(86400);
    await settle();
    button('Turn on').dispatch('click', {});
    for (let i = 0; i < 50 && asked.length === 0; i++) await settle();
    assert.deepEqual(asked, [{ method: 'PUT', path: '/api/dens/d/dms/7/expiry', body: { expires_after: 86400 } }]);
    await settle();
    assert.equal(dialog(), undefined);

    // Off, from a time, and a change from one time to another, say so.
    mount(h(ExpirySetting, { denID: 'd', channel: { id: '7', kind: 'group_dm', expires_after: 10, expires_set: { by: '2', at: 5 } } }), root);
    assert.deepEqual(options(), ['Off', '10 seconds', '1 hour', '1 day', '1 week', '4 weeks']);
    pick(0);
    await settle();
    assert.equal(dialog()?.attributes['aria-label'], 'Turn off disappearing messages?');
    assert.match(dialog().textContent, /New messages stay\./);
    button('Cancel').dispatch('click', {});
    await settle();
    pick(3600);
    await settle();
    assert.equal(dialog()?.attributes['aria-label'], 'Change disappearing messages?');
    button('Cancel').dispatch('click', {});
    mount(null, root);

    mount(h(ExpiryBadge, { after: 2419200, onClick() {} }), root);
    const badge = findAll(root, (n) => n.localName === 'button')[0];
    assert.equal(badge.textContent, '4w');
    assert.equal(badge.attributes['aria-label'], "Messages disappear 4 weeks after they're sent: change it");
    mount(null, root);
});
