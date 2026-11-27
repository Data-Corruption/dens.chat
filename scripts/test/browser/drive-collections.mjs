// The collections check, as alice in Chromium and bob in the browser named:
// - alice creates shared channel collections in #general (staff-only Guides
//   and everyone-writable Tips), adds a message via the bookmark picker,
//   edits/deletes a shared collection, removes bob's added item, and checks
//   all 4 collection audit kinds in Manage → Audit log;
// - bob verifies Guides (staff-only) is absent from his Save picker while Tips
//   (everyone) appears, adds his message to Tips, and checks ← #general back;
// - bob uses lazy "Saved" one-click bookmarking + filter/create ("Recipes"),
//   saves channel, role-gated #raids, DM-with-photo, and Notes messages into
//   Saved, and opens them (including decoded DM photo) in personal Collections;
// - alice revokes Raiders from bob: bob's Saved count drops and the #raids
//   message disappears; alice re-grants Raiders and both return;
// - bob opens personal ("Recipes") and shared ("Tips") collections from Ctrl+K;
// - bob checks 390×844 phone layout: bottom-sheet Save picker, sidebar
//   Collections row, channel collections Back trail, and zero horizontal overflow.
//
//   run.sh collections chromium|firefox
import { ALICE, BOB, HOME, MANAGE, checks, launch, manageFromName, openManage, pair, waitFor } from './browsers.mjs';

const [, , which = 'chromium'] = process.argv;
const {
    DEN: den, GENERAL: general, RAIDS: raids, RAIDS_ROLE: raidsRole, BOB_ID: bobID,
    DM: dm, NOTES: notes, GEN_MSG_ALICE: genMsgAlice, GEN_MSG_BOB: genMsgBob,
    RAID_MSG: raidMsg, DM_MSG: dmMsg, NOTE_MSG: noteMsg,
} = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);

const alice = await launch('chromium', 9571);
let bob = null;

const setInput = async (on, selector, value) => {
    await on.eval(`(() => {
        const el = ${selector};
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, ${JSON.stringify(value)});
        el.dispatchEvent(new Event('input', { bubbles: true }));
        return true;
    })()`);
};

const openSavePicker = async (on, messageID) => {
    const sel = `[data-id=${JSON.stringify(messageID)}] [data-save-button]`;
    await waitFor(() => on.eval(`!!document.querySelector(${JSON.stringify(sel)})`), `save button for ${messageID}`);
    await on.eval(`document.querySelector(${JSON.stringify(sel)}).click(), true`);
    await waitFor(() => on.eval(`!!document.querySelector('[data-save-picker]')`), 'save picker open');
};

try {
    await pair(alice, ALICE);
    await alice.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-channel-collections]')`), 'alice channel collections button');

    step('alice creates shared collections in #general (Guides staff-only, Tips everyone, Temp to delete)');
    await alice.eval(`document.querySelector('[data-channel-collections]').click(), true`);
    await waitFor(() => alice.eval(`location.pathname.endsWith('/channels/${general}/collections') && !!document.querySelector('[data-new-collection]')`), 'alice #general collections pane');

    const createShared = async (name, whoAdds = 'staff') => {
        await alice.eval(`document.querySelector('[data-new-collection]').click(), true`);
        await waitFor(() => alice.eval(`!!document.querySelector('[data-collection-name]')`), 'collection dialog');
        await setInput(alice, `document.querySelector('[data-collection-name]')`, name);
        if (whoAdds === 'everyone') {
            await alice.eval(`document.querySelector('input[name="who_adds"][value="everyone"]').click(), true`);
        }
        await alice.eval(`document.querySelector('[data-collection-submit]').click(), true`);
        await waitFor(() => alice.eval(`[...document.querySelectorAll('[data-collection-tab]')].some((t) => t.textContent.includes(${JSON.stringify(name)}))`), `tab ${name}`);
    };
    await createShared('Guides', 'staff');
    await createShared('Tips', 'everyone');
    await createShared('Temp', 'staff');
    c.report(true, 'alice created Guides (staff), Tips (everyone), and Temp shared collections');

    // Delete Temp so collection.deleted is audited too.
    await alice.eval(`document.querySelector('[data-delete-collection]').click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-confirm-delete-collection]')`), 'delete confirm dialog');
    await alice.eval(`document.querySelector('[data-confirm-delete-collection]').click(), true`);
    await waitFor(() => alice.eval(`![...document.querySelectorAll('[data-collection-tab]')].some((t) => t.textContent.includes('Temp'))`), 'Temp deleted');

    // Return to #general via ← #general.
    await alice.eval(`document.querySelector('[data-collections-back]').click(), true`);
    await waitFor(() => alice.eval(`location.pathname.endsWith('/${general}')`), 'alice back in #general');
    c.report(true, '← #general returns to #general');

    // Alice saves her message into Guides.
    await openSavePicker(alice, genMsgAlice);
    const guidesID = await waitFor(() => alice.eval(`[...document.querySelectorAll('[data-save-option]')].find((b) => b.textContent.includes('Guides'))?.dataset.saveOption || ''`), 'Guides option');
    const tipsID = await alice.eval(`[...document.querySelectorAll('[data-save-option]')].find((b) => b.textContent.includes('Tips'))?.dataset.saveOption || ''`);
    await alice.eval(`document.querySelector('[data-save-option=${JSON.stringify(guidesID)}]').click(), true`);
    await waitFor(() => alice.eval(`document.querySelector('[data-save-option=${JSON.stringify(guidesID)}]')?.getAttribute('aria-checked') === 'true'`), 'Guides checked');
    await alice.key('Escape');
    await waitFor(() => alice.eval(`!document.querySelector('[data-save-picker]')`), 'picker closed');

    step('bob checks who_adds enforcement, adds to Tips, and uses lazy Saved + filter/create');
    bob = await launch(which, which === 'chromium' ? 9572 : 9573);
    await pair(bob, BOB);
    await bob.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
    await waitFor(() => bob.eval(`!!document.querySelector('[data-id=${JSON.stringify(genMsgBob)}]')`), 'bob sees messages in #general');

    await openSavePicker(bob, genMsgBob);
    const bobSeesTipsOnly = await waitFor(() => bob.eval(`(() => {
        const opts = [...document.querySelectorAll('[data-save-option]')].map((b) => b.dataset.saveOption);
        return opts.includes(${JSON.stringify(tipsID)}) && !opts.includes(${JSON.stringify(guidesID)}) && !!document.querySelector('[data-lazy-saved]');
    })()`), 'bob sees Tips, not Guides, and sees lazy Saved');
    c.report(bobSeesTipsOnly, 'bob sees Tips (everyone), not Guides (staff), and sees virtual Saved row');

    // Bob adds his message to Tips AND clicks lazy Saved in one go.
    await bob.eval(`document.querySelector('[data-save-option=${JSON.stringify(tipsID)}]').click(), true`);
    await waitFor(() => bob.eval(`document.querySelector('[data-save-option=${JSON.stringify(tipsID)}]')?.getAttribute('aria-checked') === 'true'`), 'Tips checked by bob');
    await bob.eval(`document.querySelector('[data-lazy-saved]').click(), true`);
    const savedID = await waitFor(() => bob.eval(`[...document.querySelectorAll('[data-save-option]')].find((b) => b.textContent.includes('Saved') && b.getAttribute('aria-checked') === 'true')?.dataset.saveOption || ''`), 'lazy Saved created and checked');
    c.report(!!savedID, 'lazy Saved created and checked in one click');

    // Filter/create inline: "Recipes".
    await setInput(bob, `document.querySelector('[data-save-filter]')`, 'Recipes');
    await waitFor(() => bob.eval(`!!document.querySelector('[data-save-create]')`), '+ Create "Recipes"');
    await bob.eval(`document.querySelector('[data-save-create]').click(), true`);
    const recipesID = await waitFor(() => bob.eval(`[...document.querySelectorAll('[data-save-option]')].find((b) => b.textContent.includes('Recipes') && b.getAttribute('aria-checked') === 'true')?.dataset.saveOption || ''`), 'Recipes created and checked');
    c.report(!!recipesID, 'filter/create created Recipes and added message');
    await bob.key('Escape');
    await waitFor(() => bob.eval(`!document.querySelector('[data-save-picker]')`), 'bob picker closed');

    step('bob saves messages from #raids, DM with photo, and Notes into Saved');
    for (const [chID, msgID] of [[raids, raidMsg], [dm, dmMsg], [notes, noteMsg]]) {
        await bob.go(`http://localhost:${BOB.port}/den/${den}/${chID}`);
        await waitFor(() => bob.eval(`!!document.querySelector('[data-id=${JSON.stringify(msgID)}]')`), `message ${msgID} in ${chID}`);
        await openSavePicker(bob, msgID);
        await waitFor(() => bob.eval(`!!document.querySelector('[data-save-option=${JSON.stringify(savedID)}]')`), 'Saved option');
        await bob.eval(`document.querySelector('[data-save-option=${JSON.stringify(savedID)}]').click(), true`);
        await waitFor(() => bob.eval(`document.querySelector('[data-save-option=${JSON.stringify(savedID)}]')?.getAttribute('aria-checked') === 'true'`), `saved ${msgID}`);
        await bob.key('Escape');
        await waitFor(() => bob.eval(`!document.querySelector('[data-save-picker]')`), 'picker closed');
    }

    // Bob switches back to the Den tab, opens personal Collections via the sidebar row, and checks all 4 items + decoded DM photo.
    await bob.eval(`[...document.querySelectorAll('[role=tab]')].find((b) => b.textContent.trim().startsWith('Den')).click(), true`);
    await waitFor(() => bob.eval(`!!document.querySelector('[data-collections-row] button')`), 'Collections row in Den tab');
    await bob.eval(`document.querySelector('[data-collections-row] button').click(), true`);
    await waitFor(() => bob.eval(`location.pathname.endsWith('/collections') && document.querySelectorAll('[data-collection-item]').length === 4`), '4 items in Saved');
    const dmPhotoLoaded = await waitFor(() => bob.eval(`(() => {
        const item = document.querySelector('[data-collection-item=${JSON.stringify(dmMsg)}]');
        const img = item?.querySelector('img');
        return !!item && item.textContent.includes('DM beach photo') && !!img && img.complete && img.naturalWidth > 0;
    })()`), 'DM photo decoded and rendered in Saved');
    const noteDecoded = await bob.eval(`document.querySelector('[data-collection-item=${JSON.stringify(noteMsg)}]')?.textContent.includes('Bob private note')`);
    c.report(dmPhotoLoaded && noteDecoded, 'personal Saved renders channel, #raids, decoded DM photo, and Notes messages');
    await bob.shot(`${HOME}/shot-collections-personal.png`);

    step('role revocation hides #raids item and drops count; re-granting restores both');
    await alice.eval(`fetch('/api/dens/${den}/members/${bobID}/roles', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ role_ids: [] }) }).then((r) => r.ok)`);
    const hiddenOnRevoke = await waitFor(() => bob.eval(`(() => {
        const count = document.querySelector('[data-collection-count=${JSON.stringify(savedID)}]')?.textContent.trim();
        return count === '3' && !document.querySelector('[data-collection-item=${JSON.stringify(raidMsg)}]') && document.querySelectorAll('[data-collection-item]').length === 3;
    })()`), 'Saved dropped to 3 on role loss');
    c.report(hiddenOnRevoke, 'revoking Raiders hides #raids item and drops count to 3');

    await alice.eval(`fetch('/api/dens/${den}/members/${bobID}/roles', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ role_ids: [${JSON.stringify(raidsRole)}] }) }).then((r) => r.ok)`);
    const restoredOnGrant = await waitFor(() => bob.eval(`(() => {
        const count = document.querySelector('[data-collection-count=${JSON.stringify(savedID)}]')?.textContent.trim();
        return count === '4' && !!document.querySelector('[data-collection-item=${JSON.stringify(raidMsg)}]') && document.querySelectorAll('[data-collection-item]').length === 4;
    })()`), 'Saved restored to 4 on role regain');
    c.report(restoredOnGrant, 're-granting Raiders restores #raids item and count to 4');

    step('alice renames Guides, removes bob item from Tips, and verifies audit log');
    await alice.eval(`document.querySelector('[data-channel-collections]').click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-collection-tab=${JSON.stringify(guidesID)}]')`), 'Guides tab');
    await alice.eval(`document.querySelector('[data-collection-tab=${JSON.stringify(guidesID)}]').click(), true`);
    await alice.eval(`document.querySelector('[data-edit-collection]').click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-collection-name]')`), 'edit dialog');
    await setInput(alice, `document.querySelector('[data-collection-name]')`, 'Raid Guides');
    await alice.eval(`document.querySelector('input[name="who_adds"][value="everyone"]').click(), true`);
    await alice.eval(`document.querySelector('[data-collection-submit]').click(), true`);
    await waitFor(() => alice.eval(`document.querySelector('[data-collection-tab=${JSON.stringify(guidesID)}]')?.textContent.includes('Raid Guides')`), 'Guides renamed');

    await alice.eval(`document.querySelector('[data-collection-tab=${JSON.stringify(tipsID)}]').click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-remove-item=${JSON.stringify(genMsgBob)}]')`), 'bob item in Tips');
    await alice.eval(`document.querySelector('[data-remove-item=${JSON.stringify(genMsgBob)}]').click(), true`);
    await waitFor(() => alice.eval(`!document.querySelector('[data-collection-item=${JSON.stringify(genMsgBob)}]')`), 'bob item removed by alice');
    await alice.shot(`${HOME}/shot-collections-shared.png`);

    await manageFromName(alice);
    await waitFor(() => alice.eval(`!!${MANAGE}`), 'Manage dialog');
    await openManage(alice, 'Audit log');
    const auditOK = await waitFor(() => alice.eval(`(() => {
        const t = ${MANAGE}?.querySelector('[data-audit]')?.textContent || '';
        return t.includes('made the collection Guides in #general')
            && t.includes('changed the collection Raid Guides in #general')
            && t.includes('deleted the collection Temp in #general')
            && t.includes('removed a message added by @bob from Tips in #general');
    })()`), '4 collection audit lines');
    c.report(auditOK, 'Audit log records collection.created, updated, deleted, and item_removed');

    step('bob uses Ctrl+K switcher to jump to personal Recipes and shared Tips');
    await bob.eval(`document.querySelector('[data-switcher-button]').click(), true`);
    await waitFor(() => bob.eval(`!!document.querySelector('[data-switcher] input')`), 'switcher open');
    await setInput(bob, `document.querySelector('[data-switcher] input')`, 'Recipes');
    await waitFor(() => bob.eval(`!!document.querySelector('[data-switcher] [role=option]')`), 'Recipes option');
    await bob.eval(`document.querySelector('[data-switcher] [role=option]').click(), true`);
    const switchedPersonal = await waitFor(() => bob.eval(`location.pathname.endsWith('/collections') && document.querySelector('[data-collection-tab=${JSON.stringify(recipesID)}]')?.getAttribute('aria-selected') === 'true'`), 'switcher opened Recipes');
    c.report(switchedPersonal, 'Ctrl+K switcher opens personal collection tab');

    await bob.eval(`document.querySelector('[data-switcher-button]').click(), true`);
    await waitFor(() => bob.eval(`!!document.querySelector('[data-switcher] input')`), 'switcher open again');
    await setInput(bob, `document.querySelector('[data-switcher] input')`, 'Tips');
    await waitFor(() => bob.eval(`!!document.querySelector('[data-switcher] [role=option]')`), 'Tips option');
    await bob.eval(`document.querySelector('[data-switcher] [role=option]').click(), true`);
    const switchedShared = await waitFor(() => bob.eval(`location.pathname.endsWith('/channels/${general}/collections') && document.querySelector('[data-collection-tab=${JSON.stringify(tipsID)}]')?.getAttribute('aria-selected') === 'true'`), 'switcher opened Tips');
    c.report(switchedShared, 'Ctrl+K switcher opens shared channel collection tab');

    step('390×844 phone layout: bottom-sheet Save picker, Back trails, zero horizontal overflow');
    if (which === 'chromium') {
        await bob.cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 2, mobile: true });
        await waitFor(() => bob.eval(`window.innerWidth <= 400`), 'phone viewport');
        await bob.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
        await waitFor(() => bob.eval(`!!document.querySelector('[data-channel-collections]')`), 'phone channel collections button');
        await bob.eval(`document.querySelector('[data-channel-collections]').click(), true`);
        await waitFor(() => bob.eval(`location.pathname.endsWith('/channels/${general}/collections')`), 'phone channel collections open');
        // Back from channel collections returns to #general.
        await bob.eval('(setTimeout(() => history.back()), true)');
        const backToChannel = await waitFor(() => bob.eval(`location.pathname.endsWith('/${general}')`), 'phone Back to #general');
        c.report(backToChannel, 'phone Back from channel collections returns to #general');

        // Open Save picker via message actions -> bottom sheet.
        await waitFor(() => bob.eval(`!!document.querySelector('[data-id=${JSON.stringify(genMsgAlice)}] button[aria-label="Message actions"]')`), 'phone message row');
        await bob.eval(`document.querySelector('[data-id=${JSON.stringify(genMsgAlice)}] button[aria-label="Message actions"]').click(), true`);
        await waitFor(() => bob.eval(`!!document.querySelector('[data-sheet] [data-menu-item="save"]')`), 'Save to collection in phone sheet');
        await bob.eval(`document.querySelector('[data-sheet] [data-menu-item="save"]').click(), true`);
        const sheetOpen = await waitFor(() => bob.eval(`!!document.querySelector('[data-sheet] [data-save-picker]')`), 'phone bottom-sheet Save picker');
        const noOverflow = await bob.eval(`document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1`);
        c.report(sheetOpen && noOverflow, 'phone Save picker opens as bottom sheet with zero horizontal overflow');
        await bob.shot(`${HOME}/shot-collections-phone.png`);
    }
} finally {
    await bob?.close();
    await alice?.close();
}

c.finish();
