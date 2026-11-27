// The roles check, of custom channel roles, as alice in Chromium and bob in
// the browser named:
// - alice creates Raiders and Secret in Manage → Roles, renames Raiders to
//   Vanguard, and restricts #raids and Raid Voice to Vanguard;
// - bob sees neither channel nor either role until alice assigns Vanguard and
//   Secret to him from Manage → Members → Roles… (Escape leaving Manage open);
// - bob sees #raids, Raid Voice, and Vanguard (never Secret), and @ completion
//   in #raids suggests @alice, not @carol;
// - bob joins Raid Voice, then alice deletes Vanguard after seeing the
//   fail-closed warning naming #raids and Raid Voice; bob is kicked at once
//   and loses #raids, which flips to staff-only on alice's page;
// - alice verifies role lines in the Audit log and checks Manage → Roles at
//   390×844 phone width with no horizontal overflow.
//
//   run.sh roles chromium|firefox
import { ALICE, BOB, HOME, MANAGE, checks, launch, manageFromName, menuRow, openManage, pair, waitFor } from './browsers.mjs';

const [, , which = 'chromium'] = process.argv;
const { DEN: den, GENERAL: general, RAIDS: raids, RAID_VOICE: raidVoice } = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);

const alice = await launch('chromium', 9561);
let bob = null;

const setInput = async (on, selector, value) => {
    await on.eval(`(() => {
        const el = ${selector};
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, ${JSON.stringify(value)});
        el.dispatchEvent(new Event('input', { bubbles: true }));
        return true;
    })()`);
};

try {
    await pair(alice, ALICE);

    step('alice creates Raiders and Secret in Manage → Roles and renames Raiders to Vanguard');
    await openManage(alice, 'Roles');
    await waitFor(() => alice.eval(`!!${MANAGE}?.querySelector('[data-roles-create]')`), 'Manage Roles section');

    const createRole = async (name) => {
        await setInput(alice, `${MANAGE}.querySelector('[data-role-name]')`, name);
        await alice.eval(`${MANAGE}.querySelector('[data-role-add]').click(), true`);
        await waitFor(() => alice.eval(`[...${MANAGE}.querySelectorAll('[data-role-label]')].some((n) => n.textContent.trim() === ${JSON.stringify(name)})`), `role ${name}`);
    };
    await createRole('Raiders');
    await createRole('Secret');
    c.report(true, 'alice created Raiders and Secret');

    // Duplicate name (case-insensitive) shows an error.
    await setInput(alice, `${MANAGE}.querySelector('[data-role-name]')`, 'raiders');
    await alice.eval(`${MANAGE}.querySelector('[data-role-add]').click(), true`);
    const dupErr = await waitFor(() => alice.eval(`${MANAGE}.querySelector('[data-roles-create]')?.textContent.includes('Another role')`), 'duplicate role error', 5000).catch(() => false);
    c.report(dupErr, 'duplicate role name rejected with clear message');

    // Rename Raiders -> Vanguard.
    const raidersID = await alice.eval(`[...${MANAGE}.querySelectorAll('[data-role]')].find((li) => li.querySelector('[data-role-label]')?.textContent.trim() === 'Raiders')?.dataset.role`);
    const secretID = await alice.eval(`[...${MANAGE}.querySelectorAll('[data-role]')].find((li) => li.querySelector('[data-role-label]')?.textContent.trim() === 'Secret')?.dataset.role`);
    await alice.eval(`${MANAGE}.querySelector('[data-role-rename=${JSON.stringify(raidersID)}]').click(), true`);
    await waitFor(() => alice.eval(`!!${MANAGE}.querySelector('[data-role-rename-input]')`), 'rename input');
    await setInput(alice, `${MANAGE}.querySelector('[data-role-rename-input]')`, 'Vanguard');
    await alice.eval(`${MANAGE}.querySelector('[data-role-rename-save]').click(), true`);
    await waitFor(() => alice.eval(`[...${MANAGE}.querySelectorAll('[data-role-label]')].some((n) => n.textContent.trim() === 'Vanguard')`), 'Vanguard role');
    c.report(true, 'alice renamed Raiders to Vanguard');
    await alice.shot(`${HOME}/shot-roles-manage.png`);

    // Close Manage and open the den.
    await alice.key('Escape');
    await waitFor(() => alice.eval(`!${MANAGE}`), 'Manage closed');
    await alice.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-channel-row=${JSON.stringify(raids)}]')`), '#raids row');

    step('alice restricts #raids and Raid Voice to Vanguard via ChannelDialog');
    const restrictChannel = async (channelID) => {
        await alice.eval(`document.querySelector('[data-channel-row=${JSON.stringify(channelID)}] button[aria-label^="Options for"]').click(), true`);
        await waitFor(() => alice.eval(`!!${menuRow('edit')}`), 'Edit channel menu item');
        await alice.eval(`${menuRow('edit')}.click(), true`);
        await waitFor(() => alice.eval(`!!document.querySelector('[data-channel-visibility]')`), 'visibility choices');
        await alice.eval(`document.querySelector('[data-channel-visibility] input[value="roles"]').click(), true`);
        await waitFor(() => alice.eval(`!!document.querySelector('[data-channel-role=${JSON.stringify(raidersID)}]')`), 'Vanguard checkbox');
        await alice.eval(`document.querySelector('[data-channel-role=${JSON.stringify(raidersID)}]').click(), true`);
        await alice.eval(`[...document.querySelectorAll('[role=dialog] button[type=submit]')].at(-1).click(), true`);
        await waitFor(() => alice.eval(`!document.querySelector('[data-channel-visibility]')`), 'ChannelDialog closed');
    };
    await restrictChannel(raids);
    await restrictChannel(raidVoice);
    const tagged = await waitFor(() => alice.eval(`!!document.querySelector('[data-channel-row=${JSON.stringify(raids)}] button[aria-label="Role-restricted"]')`), 'role indicator on #raids');
    c.report(tagged, '#raids shows role-restricted indicator');

    step('bob sees neither restricted channel nor either role before assignment');
    bob = await launch(which, which === 'chromium' ? 9562 : 9563, { media: true });
    await pair(bob, BOB);
    await bob.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
    await waitFor(() => bob.eval(`!!document.querySelector('[data-channel-row]')`), "bob's channels loaded");
    const bobHidden = await bob.eval(`!document.querySelector('[data-channel-row=${JSON.stringify(raids)}]') && !document.querySelector('[data-channel-row=${JSON.stringify(raidVoice)}]')`);
    c.report(bobHidden, 'bob cannot see #raids or Raid Voice before role assignment');

    step('alice assigns Vanguard and Secret to bob from Manage → Members');
    await manageFromName(alice);
    await waitFor(() => alice.eval(`!!${MANAGE}`), 'Manage dialog');
    await openManage(alice, 'Members');
    const bobRow = `${MANAGE}.querySelector('[data-members] [data-member="bob"]')`;
    await waitFor(() => alice.eval(`!!${bobRow}`), "bob's row in Members");
    await alice.eval(`${bobRow}.querySelector('[data-member-menu]').click(), true`);
    await waitFor(() => alice.eval(`!!${menuRow('roles')}`), 'Roles… menu item');
    await alice.eval(`${menuRow('roles')}.click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-member-roles-dialog]')`), 'MemberRolesDialog');
    // Escape closes MemberRolesDialog alone, leaving Manage open.
    await alice.key('Escape');
    const escLeftManage = await waitFor(() => alice.eval(`!document.querySelector('[data-member-roles-dialog]') && !!${MANAGE}`), 'Escape kept Manage open', 5000).catch(() => false);
    c.report(escLeftManage, 'Escape closes MemberRolesDialog and leaves Manage open');

    await alice.eval(`${bobRow}.querySelector('[data-member-menu]').click(), true`);
    await waitFor(() => alice.eval(`!!${menuRow('roles')}`), 'Roles… menu item again');
    await alice.eval(`${menuRow('roles')}.click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-member-roles-dialog]')`), 'MemberRolesDialog again');
    await alice.eval(`document.querySelector('[data-member-role=${JSON.stringify(raidersID)}]').click(), true`);
    await alice.eval(`document.querySelector('[data-member-role=${JSON.stringify(secretID)}]').click(), true`);
    await alice.eval(`document.querySelector('[data-member-roles-save]').click(), true`);
    await waitFor(() => alice.eval(`!document.querySelector('[data-member-roles-dialog]')`), 'MemberRolesDialog saved');

    step('bob gains #raids and Raid Voice, sees Vanguard (never Secret), and @ completion excludes carol');
    await waitFor(() => bob.eval(`!!document.querySelector('[data-channel-row=${JSON.stringify(raids)}]')`), 'bob sees #raids');
    await waitFor(() => bob.eval(`!!document.querySelector('[data-channel-row=${JSON.stringify(raidVoice)}]')`), 'bob sees Raid Voice');
    await bob.eval(`document.querySelector('[data-channel-row=${JSON.stringify(raids)}] button').click(), true`);
    await waitFor(() => bob.eval(`!!document.querySelector('textarea')`), '#raids composer');
    // Type @ to trigger mention completion.
    await bob.eval(`document.querySelector('textarea').focus(), true`);
    await bob.type('@');
    const mentionText = await waitFor(() => bob.eval(`document.querySelector('[data-completions]')?.textContent || ''`), 'mention completion list', 5000).catch(() => '');
    c.report(mentionText.includes('alice') && !mentionText.includes('carol'), `@ completion in #raids lists alice and excludes carol (${JSON.stringify(mentionText)})`);

    // Check hidden role Secret does not appear anywhere in bob's view state.
    const bobNoSecret = await bob.eval(`(async () => {
        const dens = await (await fetch('/api/dens')).json();
        const st = await (await fetch('/api/dens/' + dens.dens[0].den_id + '/state')).json();
        return st.roles.length === 1 && st.roles[0].name === 'Vanguard' && !JSON.stringify(st).includes('Secret');
    })()`);
    c.report(bobNoSecret, 'bob sees Vanguard and never learns Secret role name or membership');
    await bob.shot(`${HOME}/shot-roles-bob.png`);

    step('bob joins Raid Voice; alice deletes Vanguard with fail-closed warning and kicks bob');
    await bob.eval(`document.querySelector('[data-channel-row=${JSON.stringify(raidVoice)}] button').click(), true`, true);
    await waitFor(() => bob.eval(`!!document.querySelector('[data-call-pane=${JSON.stringify(raidVoice)}]')`), 'bob in Raid Voice');

    await alice.eval(`[...${MANAGE}.querySelectorAll('nav button')].find((b) => b.textContent.trim() === 'Roles').click(), true`);
    await waitFor(() => alice.eval(`!!${MANAGE}.querySelector('[data-role-delete=${JSON.stringify(raidersID)}]')`), 'delete Vanguard button');
    await alice.eval(`${MANAGE}.querySelector('[data-role-delete=${JSON.stringify(raidersID)}]').click(), true`);
    const warn = await waitFor(() => alice.eval(`${MANAGE}.querySelector('[data-role-flip-warning]')?.textContent || ''`), 'fail-closed warning');
    c.report(warn.includes('#raids') && warn.includes('Raid Voice'), `fail-closed delete warning lists #raids and Raid Voice (${warn})`);
    await alice.shot(`${HOME}/shot-roles-delete-warn.png`);

    await alice.eval(`${MANAGE}.querySelector('[data-role-confirm-delete]').click(), true`);
    await waitFor(() => alice.eval(`!${MANAGE}.querySelector('[data-role=${JSON.stringify(raidersID)}]')`), 'Vanguard deleted');

    // Bob is kicked from Raid Voice and loses #raids immediately.
    const bobKicked = await waitFor(() => bob.eval(`!document.querySelector('[data-call-pane=${JSON.stringify(raidVoice)}]') && !document.querySelector('[data-channel-row=${JSON.stringify(raids)}]')`), 'bob kicked and #raids gone', 10000).catch(() => false);
    c.report(bobKicked, 'bob kicked from Raid Voice and loses #raids on role delete');

    step('alice sees role entries in Audit log and #raids flipped to staff-only');
    await alice.eval(`[...${MANAGE}.querySelectorAll('nav button')].find((b) => b.textContent.trim() === 'Audit log').click(), true`);
    const auditText = await waitFor(() => alice.eval(`(() => {
        const el = ${MANAGE}.querySelector('[data-audit]');
        return el && el.textContent.includes('deleted the role Vanguard') ? el.textContent : '';
    })()`), 'role audit lines', 10000).catch(() => '');
    c.report(auditText.includes('deleted the role Vanguard') && auditText.includes('Changed to staff only'), 'Audit log records role.deleted and flipped channels');
    c.report(auditText.includes("changed @bob's roles") && auditText.includes('renamed the role Raiders to Vanguard'), 'Audit log records member.roles and role.renamed');

    // Check 390×844 phone width layout on Manage → Roles (zero horizontal overflow).
    await alice.eval(`[...${MANAGE}.querySelectorAll('nav button')].find((b) => b.textContent.trim() === 'Roles').click(), true`);
    await alice.cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 2, mobile: true });
    await waitFor(() => alice.eval(`window.innerWidth <= 400`), 'phone viewport');
    const noOverflow = await alice.eval(`document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1`);
    c.report(noOverflow, 'Manage → Roles fits 390px phone viewport without horizontal overflow');
    await alice.shot(`${HOME}/shot-roles-phone.png`);
} finally {
    await bob?.close();
    await alice?.close();
}

c.finish();
