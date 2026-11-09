import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { avatarColor, avatarLetter } from '../assets/js/src/avatar.jsx';
import { MessageRow, typingLine } from '../assets/js/src/messages.jsx';
import { EditProfile, MARK, MemberList, ProfileCard, cardMenu, memberMenu, rank, roleMark, shownBadges } from '../assets/js/src/people.jsx';
import { CallMembers } from '../assets/js/src/voice.jsx';

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

// A message's row, as density.test.js renders one.
function messageRow(author, line) {
    const none = () => {};
    const m = { id: '10', channel_id: '2', author_id: author.id, created_at: Date.UTC(2026, 10, 6, 12, 34), revision: 1, text: 'hi' };
    return h(MessageRow, {
        m, author, me: { ...bob, id: '3' }, line, canReply: false, canEdit: false, canDelete: false, link: '', editorNames: [], candidates: [],
        canReact: false, nameOf: () => '', onReact: none, onReactPick: none, onRemoveReactions: none, onUpdated: none, onPolled: none, onMenu: none,
        onReply: none, onEdit: none, onEditDone: none, onDelete: none, onJump: none, onView: none, onProfile: none, onToggle: none, denID: 'd', channelID: '2',
    });
}

// A voice channel's call, as the channel list renders one, of alice and
// bob, seen by me.
function callRows(me, { volumes = {}, moving = null } = {}) {
    const members = new Map([alice, bob].map((m) => [m.id, m]));
    return h(CallMembers, { denID: 'd', call: { channel_id: '5', members: [{ id: alice.id }, { id: bob.id }] }, members, me, speaking: new Set(),
        sounding: '', deafened: false, volumes, onProfile() {}, inCall: false, label: { den: 'Den', channel: 'Lounge' }, moving });
}

test("a crown or a shield is one mark, centered on the name wherever it stands: the list, a call, a card and a message's name", () => {
    const root = document.createElement('div');
    const moderator = { ...bob, role: 'moderator' };
    const places = {
        list: h(MemberList, { members: [alice, moderator], online: new Set(['1']), onProfile() {} }),
        call: callRows({ ...moderator, id: '3' }),
        card: card({ member: alice }),
        message: messageRow(alice, false),
        compact: messageRow(alice, true),
    };
    for (const [place, tree] of Object.entries(places)) {
        mount(tree, root);
        const marks = findAll(root, byAttr('data-role-mark'));
        assert.ok(marks.length > 0, `${place} has its mark`);
        for (const mark of marks) {
            assert.equal(mark.attributes.class, 'inline-flex shrink-0', `${place}: the one mark`);
            const icon = findAll(mark, (n) => n.attributes?.class === `inline-flex ${MARK}`);
            assert.equal(icon.length, 1, `${place}: its icon, in the marks' color`);
            assert.equal(findAll(icon[0], (n) => n.localName === 'svg').length, 1);
            // Every box from the mark to its icon is a flex box, so no line
            // of text sets its height, and the row centers it on the name.
            for (let n = icon[0].parentNode; n !== mark; n = n.parentNode) assert.match(n.attributes.class, /\b(inline-)?flex\b/, `${place}: ${n.localName}`);
            assert.match(mark.parentNode.attributes.class, /\bflex\b.*\bitems-center\b|\bitems-center\b.*\bflex\b/, `${place}: centered on its row`);
        }
    }
    mount(null, root);
});

test("a right click on a member in the list offers their profile, what their card does, and its ⋯", () => {
    const keys = (args) => memberMenu(args).map((i) => i.label).join(' | ');
    const kept = { verified: false };
    assert.equal(keys({ self: false, role: 'owner', member: bob, kept, notes: true }), 'Profile | Message | Compare codes | Promote to moderator | Remove from den…');
    assert.equal(keys({ self: true, role: 'owner', member: alice, kept, notes: true }), 'Profile | Edit profile | Notes');
    assert.equal(keys({ self: true, role: 'owner', member: alice, kept, notes: false }), 'Profile | Edit profile');
    assert.equal(keys({ self: false, role: 'member', member: { ...bob, identity: undefined }, kept: null }), 'Profile | Message');
    assert.equal(keys({ self: false, role: 'owner', member: { ...bob, left_at: 9 }, kept }), 'Profile | Ban…');
});

test("a call's row drags for staff but while its menu is open, whose slider would drag it along, and shows no volume of its own", async () => {
    const root = document.createElement('div');
    mount(callRows(alice, { volumes: { 'd:2': 0.3 }, moving: { targets: () => [], lift() {}, letGo() {} } }), root);
    const row = () => findAll(root, byAttr('data-call-member', bob.id))[0];
    assert.equal(row().attributes.draggable, 'true', 'the owner drags a member');
    assert.equal(findAll(root, byAttr('data-call-member', alice.id))[0].attributes.draggable, undefined, 'and not herself');
    assert.doesNotMatch(row().textContent, /%/, 'its volume is in its menu alone');
    findAll(row(), buttonNamed('Options for Bob in the call'))[0].dispatch('click', {});
    await settle();
    assert.equal(findAll(root, (n) => n.localName === 'input' && n.attributes.type === 'range').length, 1, 'its menu, with the slider');
    assert.match(row().textContent, /30%/, "the slider's figure");
    assert.equal(row().attributes.draggable, undefined, 'a press in its menu drags nothing');
    findAll(row(), buttonNamed('Options for Bob in the call'))[0].dispatch('click', {});
    await settle();
    assert.equal(row().attributes.draggable, 'true', 'closed, it drags again');
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
