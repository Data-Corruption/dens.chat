// Custom channel roles: helpers, Manage Roles section (create, rename,
// fail-closed delete warning, 50-role cap), MemberRolesDialog, and
// ChannelDialog visibility modes (with fallback on older dens).

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { MAX_ROLES, canSeeChannel, roleFlipChannels, roleNames, roleTipText } from '../src/core/roles.js';
import { MemberRolesDialog, RolesSection } from '../src/roles.jsx';
import { ChannelDialog } from '../src/manage.jsx';
import { cardMenu, memberMenu } from '../src/people.jsx';
import { sectionsFor } from '../src/manageden.jsx';

globalThis.window = {
    location: { pathname: '/den/d', hash: '' },
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    addEventListener() {},
    removeEventListener() {},
    history: { state: null, pushState(s) { this.state = s; }, replaceState(s) { this.state = s; }, go() {} },
};

const findAll = (node, match, out = []) => {
    if (match(node)) out.push(node);
    for (const child of node.childNodes || []) findAll(child, match, out);
    return out;
};
const byAttr = (name, value) => (n) => n.attributes?.[name] !== undefined && (value === undefined || n.attributes[name] === String(value));
const settle = () => new Promise((resolve) => setTimeout(resolve, 10));

const roles = [
    { id: '10', name: 'Beta' },
    { id: '11', name: 'Raiders' },
    { id: '12', name: 'VIP' },
];
const channels = [
    { id: '1', kind: 'text', name: 'general', position: 0 },
    { id: '2', kind: 'text', name: 'staff', position: 1, staff_only: true },
    { id: '3', kind: 'text', name: 'raids', position: 2, role_ids: ['11'] },
    { id: '4', kind: 'voice', name: 'Raid Voice', position: 3, role_ids: ['11', '12'] },
];

test('roleNames, roleTipText, roleFlipChannels and canSeeChannel behave consistently', () => {
    assert.deepEqual(roleNames(['12', '10', '999'], roles), ['Beta', 'VIP']);
    assert.equal(roleTipText(['12', '11'], roles), 'Restricted to Raiders, VIP (and staff).');
    assert.equal(roleTipText(['999'], roles), 'Restricted by role: staff and members with a required role can see it.');

    // Only #raids has Raiders as its sole role; Raid Voice also has VIP.
    assert.deepEqual(roleFlipChannels('11', channels).map((c) => c.name), ['raids']);
    assert.deepEqual(roleFlipChannels('12', channels), []);

    const owner = { id: '1', role: 'owner' };
    const mod = { id: '2', role: 'moderator' };
    const bob = { id: '3', role: 'member', role_ids: ['10'] };
    const raider = { id: '4', role: 'member', role_ids: ['11'] };
    const vip = { id: '5', role: 'member', role_ids: ['12'] };
    for (const staff of [owner, mod]) {
        for (const ch of channels) assert.equal(canSeeChannel(ch, staff), true);
    }
    assert.equal(canSeeChannel(channels[0], bob), true);
    assert.equal(canSeeChannel(channels[1], bob), false);
    assert.equal(canSeeChannel(channels[2], bob), false);
    assert.equal(canSeeChannel(channels[2], raider), true);
    assert.equal(canSeeChannel(channels[3], raider), true);
    assert.equal(canSeeChannel(channels[3], vip), true);
    assert.equal(canSeeChannel(channels[2], vip), false);
});

test('cardMenu and memberMenu offer Roles… to staff only when roles are enabled', () => {
    const bob = { id: '2', username: 'bob', display_name: 'Bob', role: 'member', joined_at: 1 };
    const modLabels = cardMenu({ self: false, role: 'moderator', member: bob, kept: null, roles: true }).map((i) => i.label);
    assert.deepEqual(modLabels, ['Roles…', 'Remove from den…']);
    const memberLabels = cardMenu({ self: false, role: 'member', member: bob, kept: null, roles: true }).map((i) => i.label);
    assert.deepEqual(memberLabels, []);
    const leftLabels = cardMenu({ self: false, role: 'owner', member: { ...bob, left_at: 5 }, kept: null, roles: true }).map((i) => i.label);
    assert.deepEqual(leftLabels, ['Ban…']);
    assert.ok(memberMenu({ self: false, role: 'owner', member: bob, kept: null, roles: true }).some((i) => i.key === 'roles'));
    assert.ok(!memberMenu({ self: false, role: 'owner', member: bob, kept: null, roles: false }).some((i) => i.key === 'roles'));
    assert.ok(sectionsFor({ state: 'connected', role: 'moderator' }).some(([id]) => id === 'roles'));
    assert.ok(!sectionsFor({ state: 'connected', role: 'member' }).some(([id]) => id === 'roles'));
});

test('RolesSection shows fail-closed flip warning on delete and enforces 50-role cap', async () => {
    const root = document.createElement('div');
    const calls = [];
    const origFetch = globalThis.fetch;
    globalThis.fetch = async (url, init) => {
        calls.push({ method: init.method, url, body: init.body ? JSON.parse(init.body) : null });
        return { ok: true, status: 204, headers: new Headers(), text: async () => '' };
    };
    try {
        const view = {
            has_roles: true,
            roles,
            channels,
            members: [
                { id: '1', username: 'alice', role: 'owner' },
                { id: '2', username: 'bob', role: 'member', role_ids: ['11'] },
            ],
        };
        mount(h(RolesSection, { denID: 'd', view, onChanged: async () => {} }), root);

        // Deleting Raiders warns that #raids will flip to staff-only, not Raid Voice.
        findAll(root, byAttr('data-role-delete', '11'))[0].dispatch('click', {});
        await settle();
        const warning = findAll(root, byAttr('data-role-flip-warning'))[0];
        assert.ok(warning, 'flip warning is shown for single-role channel');
        assert.match(warning.textContent, /#raids/);
        assert.doesNotMatch(warning.textContent, /Raid Voice/);

        // Confirm delete sends DELETE /api/dens/d/roles/11.
        findAll(root, byAttr('data-role-confirm-delete'))[0].dispatch('click', {});
        await settle();
        assert.deepEqual(calls.at(-1), { method: 'DELETE', url: '/api/dens/d/roles/11', body: null });

        // Deleting VIP shows no flip warning since Raid Voice also has Raiders.
        findAll(root, byAttr('data-role-delete', '12'))[0].dispatch('click', {});
        await settle();
        assert.equal(findAll(root, byAttr('data-role-flip-warning')).length, 0);

        // At MAX_ROLES (50), the create input and button are disabled.
        const fullRoles = Array.from({ length: MAX_ROLES }, (_, i) => ({ id: String(i + 1), name: `Role ${i + 1}` }));
        mount(h(RolesSection, { denID: 'd', view: { ...view, roles: fullRoles }, onChanged: async () => {} }), root);
        const addBtn = findAll(root, byAttr('data-role-add'))[0];
        assert.equal(addBtn.attributes.disabled, 'true');
        assert.match(root.textContent, /maximum of 50 roles/);
    } finally {
        mount(null, root);
        globalThis.fetch = origFetch;
    }
});

test('MemberRolesDialog toggles roles and PUTs member roles', async () => {
    const root = document.createElement('div');
    const calls = [];
    const origFetch = globalThis.fetch;
    globalThis.fetch = async (url, init) => {
        calls.push({ method: init.method, url, body: init.body ? JSON.parse(init.body) : null });
        return { ok: true, status: 204, headers: new Headers(), text: async () => '' };
    };
    let closedWith = null;
    try {
        const bob = { id: '2', username: 'bob', display_name: 'Bob', role: 'member', role_ids: ['10'] };
        mount(h(MemberRolesDialog, { denID: 'd', member: bob, roles, onClose: (v) => { closedWith = v; } }), root);
        // Uncheck Beta (10), check Raiders (11) and VIP (12).
        findAll(root, byAttr('data-member-role', '10'))[0].dispatch('change', {});
        findAll(root, byAttr('data-member-role', '11'))[0].dispatch('change', {});
        findAll(root, byAttr('data-member-role', '12'))[0].dispatch('change', {});
        await settle();
        const form = findAll(root, byAttr('data-member-roles-dialog'))[0];
        form.dispatch('Submit', { preventDefault() {} });
        await settle();
        assert.equal(calls.length, 1);
        assert.equal(calls[0].method, 'PUT');
        assert.equal(calls[0].url, '/api/dens/d/members/2/roles');
        assert.deepEqual([...calls[0].body.role_ids].sort(), ['11', '12']);
        assert.equal(closedWith, true);
    } finally {
        mount(null, root);
        globalThis.fetch = origFetch;
    }
});

test('ChannelDialog shows 3 visibility choices when has_roles is true and falls back to Staff only toggle on older dens', async () => {
    const root = document.createElement('div');
    const calls = [];
    const origFetch = globalThis.fetch;
    globalThis.fetch = async (url, init) => {
        calls.push({ method: init.method, url, body: init.body ? JSON.parse(init.body) : null });
        return { ok: true, status: 200, headers: new Headers(), text: async () => '{"ok":true}' };
    };
    try {
        // Older den: no has_roles -> shows #staff-only toggle, no visibility Choices.
        mount(h(ChannelDialog, { denID: 'd', view: { groups: [], has_roles: false, roles: [] }, onClose() {} }), root);
        assert.equal(findAll(root, byAttr('id', 'staff-only')).length, 1);
        assert.equal(findAll(root, byAttr('data-channel-visibility')).length, 0);
        mount(null, root);

        // Role-enabled den: shows Everyone / Staff only / Selected roles, picks Raiders on create.
        mount(h(ChannelDialog, { denID: 'd', view: { groups: [], has_roles: true, roles }, channel: { id: '3', kind: 'text', name: 'raids', role_ids: ['11'] }, onClose() {} }), root);
        assert.equal(findAll(root, byAttr('data-channel-visibility')).length, 1);
        assert.equal(findAll(root, byAttr('data-channel-roles')).length, 1);
        // Check VIP (12) in addition to Raiders (11) and submit PATCH.
        findAll(root, byAttr('data-channel-role', '12'))[0].dispatch('change', {});
        await settle();
        findAll(root, (n) => n.localName === 'form')[0].dispatch('Submit', { preventDefault() {} });
        await settle();
        assert.equal(calls.length, 1);
        assert.equal(calls[0].method, 'PATCH');
        assert.equal(calls[0].url, '/api/dens/d/channels/3');
        assert.deepEqual(calls[0].body, { role_ids: ['11', '12'] });
    } finally {
        mount(null, root);
        globalThis.fetch = origFetch;
    }
});
