import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { avatarColor, avatarLetter } from '../assets/js/src/avatar.jsx';
import { typingLine } from '../assets/js/src/messages.jsx';
import { EditProfile, MemberList, ProfileCard, cardMenu, rank, roleMark, shownBadges } from '../assets/js/src/people.jsx';

test("an avatar's color depends only on the member's ID", () => {
    assert.equal(avatarColor('1000123'), avatarColor('1000123'));
    const colors = new Set();
    for (let id = 1; id <= 50; id++) {
        const c = avatarColor(String(id));
        assert.match(c, /^bg-[a-z]+-\d00$/);
        colors.add(c);
    }
    assert.ok(colors.size >= 8, `only ${colors.size} colors across 50 members`);
});

test("an avatar's letter is the username's first letter or digit", () => {
    assert.equal(avatarLetter('bob'), 'B');
    assert.equal(avatarLetter('_x1'), 'X');
    assert.equal(avatarLetter('7up'), '7');
    assert.equal(avatarLetter('__'), '?');
    assert.equal(avatarLetter(undefined), '?');
});

test('the typing line names up to three people', () => {
    assert.equal(typingLine([]), '');
    assert.equal(typingLine(['Ann']), 'Ann is typing…');
    assert.equal(typingLine(['Ann', 'Bo']), 'Ann and Bo are typing…');
    assert.equal(typingLine(['Ann', 'Bo', 'Cy']), 'Ann, Bo and Cy are typing…');
    assert.equal(typingLine(['Ann', 'Bo', 'Cy', 'Di']), 'Several people are typing…');
});

test('roles rank as the den ranks them', () => {
    assert.ok(rank('owner') > rank('moderator'));
    assert.ok(rank('moderator') > rank('member'));
    assert.equal(rank(undefined), rank('member'));
});

// The profile card as a card (M17.6): its role marks, its buttons and ⋯,
// and the badges its member shows, which Edit profile turns by their eyes.

// A window for the card's place in the history, as the nav tests' is.
globalThis.window = {
    location: { pathname: '/den/d', hash: '' },
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

const findAll = (node, match, out = []) => {
    if (match(node)) out.push(node);
    for (const child of node.childNodes || []) findAll(child, match, out);
    return out;
};
const byAttr = (name, value) => (n) => n.attributes?.[name] !== undefined && (value === undefined || n.attributes[name] === String(value));
const buttonNamed = (text) => (n) => n.localName === 'button' && (n.textContent.trim() === text || n.attributes?.['aria-label'] === text);
const settle = () => new Promise((resolve) => setTimeout(resolve, 5));

const alice = { id: '1', display_name: 'Alice', username: 'alice', role: 'owner', joined_at: 1 };
const bob = { id: '2', display_name: 'Bob', username: 'bob', role: 'member', joined_at: 1, identity: 'x' };

function card(props) {
    return h(ProfileCard, { denID: 'd', me: alice, role: 'owner', online: true, kept: null,
        onClose() {}, onMessage() {}, onNotes: null, onCompare() {}, onEdit() {}, onRemove() {}, ...props });
}

test("a staff member's name has a crown or a shield with its tip, on the card and in the member list, and nobody has a pill", () => {
    assert.equal(roleMark('owner'), 'Owner');
    assert.equal(roleMark('moderator'), 'Moderator');
    assert.equal(roleMark('member'), '');
    const root = document.createElement('div');
    for (const [member, mark] of [[alice, 'owner'], [{ ...bob, role: 'moderator' }, 'moderator'], [bob, null]]) {
        mount(card({ member }), root);
        const marks = findAll(root, byAttr('data-role-mark'));
        if (!mark) {
            assert.equal(marks.length, 0, "a member's card has no mark");
            continue;
        }
        assert.equal(marks.length, 1);
        assert.equal(marks[0].attributes['data-role-mark'], mark);
        assert.ok(findAll(marks[0], buttonNamed(roleMark(mark))).length === 1, `the ${mark}'s mark opens its tip`);
    }
    mount(h(MemberList, { members: [alice, { ...bob, role: 'moderator' }, { ...bob, id: '3', display_name: 'Cy', username: 'cy' }], online: new Set(['1']), onProfile() {} }), root);
    const rows = findAll(root, (n) => n.localName === 'button');
    assert.equal(rows.length, 3, 'a row is one button, holding no other');
    // Online first, then by name; each mark's words read with its row.
    const said = (m) => findAll(m, (n) => n.attributes?.class === 'sr-only').map((n) => n.textContent).join();
    const words = rows.map((r) => findAll(r, byAttr('data-role-mark')).map((m) => `${m.attributes['data-role-mark']}:${said(m)}`).join());
    assert.deepEqual(words, ['owner:Owner', 'moderator:Moderator', '']);
    assert.equal(findAll(root, (n) => /\bbadge\b/.test(n.attributes?.class || '')).length, 0, 'no pill');
    mount(null, root);
});

test("a card's ⋯ holds what the viewer may do beyond the button that stands out, by role, and one's own card has none", () => {
    const keys = (args) => cardMenu(args).map((i) => i.label).join(' | ');
    const kept = { verified: false };
    // The owner, on a member, a moderator, and one who left.
    assert.equal(keys({ self: false, role: 'owner', member: bob, kept }), 'Compare codes | Promote to moderator | Remove from den…');
    assert.equal(keys({ self: false, role: 'owner', member: { ...bob, role: 'moderator' }, kept: { verified: true } }),
        'Compare codes again | Demote to member | Remove from den…');
    assert.equal(keys({ self: false, role: 'owner', member: { ...bob, left_at: 9 }, kept }), 'Ban…');
    // A moderator acts on members alone, and nobody on the owner.
    assert.equal(keys({ self: false, role: 'moderator', member: bob, kept: null }), 'Remove from den…');
    assert.equal(keys({ self: false, role: 'moderator', member: { ...bob, role: 'moderator' }, kept: null }), '');
    assert.equal(keys({ self: false, role: 'moderator', member: alice, kept }), '');
    // A member compares codes where their Dens keeps identities, and with
    // a member who publishes one.
    assert.equal(keys({ self: false, role: 'member', member: bob, kept }), 'Compare codes');
    assert.equal(keys({ self: false, role: 'member', member: { ...bob, identity: undefined }, kept }), '');
    assert.equal(keys({ self: true, role: 'owner', member: alice, kept }), '');
    assert.deepEqual(cardMenu({ self: false, role: 'owner', member: bob, kept: null }).map((i) => !!i.danger), [false, true]);
});

test("a card shows Message, or one's own Edit profile and Notes, standing out, with the rest under ⋯ and no title", () => {
    const root = document.createElement('div');
    const primary = (text) => findAll(root, buttonNamed(text)).some((b) => /\bbtn-primary\b/.test(b.attributes.class));
    const more = () => findAll(root, buttonNamed('More actions')).length;
    mount(card({ member: alice, onNotes() {} }), root);
    assert.ok(primary('Edit profile') && findAll(root, buttonNamed('Notes')).length === 1, 'Edit profile stands out, Notes beside it');
    assert.equal(more(), 0, "one's own card has no ⋯");
    assert.equal(findAll(root, buttonNamed('Message')).length, 0);
    const dialog = findAll(root, byAttr('role', 'dialog'))[0];
    assert.equal(dialog.attributes['aria-label'], 'Profile', 'named for screen readers');
    assert.equal(findAll(root, (n) => n.localName === 'h2').length, 0, 'with no title shown');

    mount(card({ member: bob, kept: {} }), root);
    assert.ok(primary('Message'), 'Message stands out');
    assert.equal(more(), 1, 'the rest under ⋯');
    for (const hidden of ['Compare codes', 'Promote to moderator', 'Make moderator', 'Remove from den…']) {
        assert.equal(findAll(root, buttonNamed(hidden)).length, 0, `${hidden} waits under ⋯`);
    }
    // One who left gets no message; the owner can still ban them, and a
    // member can do nothing with them.
    mount(card({ member: { ...bob, left_at: 9 } }), root);
    assert.ok(!findAll(root, buttonNamed('Message')).length && more() === 1);
    mount(card({ member: { ...bob, left_at: 9 }, role: 'member', me: { ...bob, id: '3' } }), root);
    assert.ok(!findAll(root, buttonNamed('Message')).length && more() === 0);
    mount(null, root);
});

test("a card shows the badges its member shows, and Edit profile turns each by its eye, at once", async () => {
    const badge = (id, title, hidden) => ({ event_id: id, title, image: { id: `9${id}`, width: 128, height: 128 }, event: 'Game night',
        starts_at: Date.UTC(2026, 10, 6, 20), earned_at: Date.UTC(2026, 10, 6, 20, 5), ...(hidden ? { hidden: true } : {}) });
    const asked = [];
    globalThis.fetch = async (path, opts = {}) => {
        asked.push({ method: opts.method || 'GET', path, body: opts.body ? JSON.parse(opts.body) : undefined });
        const body = path === '/api/dens/d/members/1'
            ? { ...alice, bio: 'Hi', badges: [badge('41', 'Was there'), badge('42', 'Stayed late', true)] }
            : path === '/api/dens/d/storage' ? { used: 0 } : null;
        return { ok: true, status: body ? 200 : 204, headers: { get: () => null }, text: async () => (body ? JSON.stringify(body) : '') };
    };
    const root = document.createElement('div');
    mount(card({ member: alice }), root);
    for (let i = 0; i < 50 && !findAll(root, byAttr('data-badge')).length; i++) await settle();
    assert.deepEqual(findAll(root, byAttr('data-badge')).map((b) => b.attributes['data-badge']), ['41'], 'her own card leaves the hidden one out too');
    assert.ok(findAll(root, (n) => n.localName === 'p' && n.textContent === 'Badges').length === 1, 'under Badges');
    assert.deepEqual(shownBadges([badge('1', 'a'), badge('2', 'b', true)]).map((b) => b.event_id), ['1']);

    mount(h(EditProfile, { denID: 'd', me: alice, limits: null, onClose() {} }), root);
    const toggle = (id) => findAll(root, byAttr('data-badge-toggle', id))[0];
    for (let i = 0; i < 50 && !toggle('42'); i++) await settle();
    const state = (id) => [toggle(id).attributes['aria-pressed'], findAll(toggle(id), byAttr('data-eye'))[0].attributes['data-eye']].join(' ');
    assert.equal(state('41'), 'true on', 'a shown badge, its eye open');
    assert.equal(state('42'), 'false off', 'a hidden one, its eye struck');
    assert.equal(findAll(toggle('42'), (n) => n.localName === 'path' && n.attributes.d === 'M2.5 2.5l11 11').length, 1);
    asked.length = 0;
    toggle('42').dispatch('click', {});
    for (let i = 0; i < 50 && state('42') !== 'true on'; i++) await settle();
    assert.deepEqual(asked, [{ method: 'PUT', path: '/api/dens/d/me/badges/42', body: { hidden: false } }]);
    assert.equal(state('42'), 'true on');
    toggle('41').dispatch('click', {});
    for (let i = 0; i < 50 && state('41') !== 'false off'; i++) await settle();
    assert.deepEqual(asked.at(-1), { method: 'PUT', path: '/api/dens/d/me/badges/41', body: { hidden: true } });
    mount(null, root);
});
