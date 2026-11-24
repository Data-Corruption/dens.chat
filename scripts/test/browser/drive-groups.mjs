// The groups check, of group DMs: alice, who owns the den, in Chromium, and
// bob in the browser named; carol, on the last release's Dens, and dave
// through their pages' API. Alice makes a group of bob, carol and herself
// from the DM tab's +, naming it, and each change shows in it as a line:
// bob writes and sends a photo, which alice reads, and carol, through her
// Dens; bob renames it, which reaches the den only sealed; bob adds dave
// from the group's members, who reads from his joining on, and the name
// bob's key carries; alice lets bob edit a list, whose tick on his page and
// edit dave reads; carol ticks a list alice let her edit, as her older Dens
// seals it, which waits on bob's page until alice keeps it on hers; alice,
// who made it, removes bob, whose page loses it and reads nothing written
// after; carol leaves through her Dens, and alice from the group's members.
//
//   run.sh groups chromium|firefox
import { execFileSync } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, BOB_ID: bobID, CAROL_ID: carolID, DAVE_ID: daveID } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;
const q = (s) => JSON.stringify(s);
const COMPOSER = `document.querySelector('textarea[placeholder^="Message "]')`;
const HEADER = `document.querySelector('[data-group-header]')`;
const PANEL = `document.querySelector('[data-group-members]')`;
const dialog = (title) => `document.querySelector('[role=dialog][aria-label=${q(title)}]')`;
const button = (scope, text) => `[...${scope}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${q(text)})`;
const lines = (s) => s.b.eval(`[...document.querySelectorAll('[data-group-change]')].map((x) => x.textContent.trim())`);
const text = (s) => s.b.eval('document.body.innerText');
// setValue sets a field's value as typing would, for Preact to hear.
const setValue = (field, value) => `(() => { const el = ${field}; const set = Object.getOwnPropertyDescriptor(el.constructor.prototype, 'value').set;
    set.call(el, ${q(value)}); el.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`;
const api = (who, method, path, body) => {
    const args = ['api', who, method, path];
    if (body !== undefined) args.push(JSON.stringify(body));
    return lib(...args);
};
const state = (who) => JSON.parse(api(who, 'GET', `/api/dens/${den}/state`));
const history = (who, g) => JSON.parse(api(who, 'GET', `/api/dens/${den}/channels/${g}/messages`)).messages;
const nonce = () => lib('nonce');

// lineShown waits for a line in the group on a page.
async function lineShown(s, line) {
    return waitFor(async () => (await lines(s)).includes(line), `${q(line)} on ${s.name}'s page`, 15000).then(() => true, () => false);
}

// pick ticks members by name in a dialog of people to pick.
async function pick(s, title, names) {
    for (const name of names) {
        const box = `[...${dialog(title)}.querySelectorAll('label')].find((l) => l.textContent.includes(${q(name)}))?.querySelector('input')`;
        await waitFor(() => s.b.eval(`!!${box} && !${box}.disabled`), `${name} to pick on ${s.name}'s page`);
        await s.b.eval(`${box}.click(), true`);
    }
}

// openPanel opens the group's members beside it.
async function openPanel(s) {
    if (!(await s.b.eval(`!!${PANEL}`))) await s.b.eval(`document.querySelector('button[aria-label="The group\\'s members"]').click(), true`);
    await waitFor(() => s.b.eval(`!!${PANEL}`), `the group's members on ${s.name}'s page`);
}

async function sends(s, line) {
    await waitFor(() => s.b.eval(`!!${COMPOSER}`), `${s.name}'s composer`);
    await s.b.eval(`${COMPOSER}.focus(), true`);
    await s.b.type(line);
    await s.b.key('Enter');
}

try {
    A.b = await launch('chromium', 9591, { hover: true });
    B.b = await launch(which, which === 'chromium' ? 9592 : 9593, { hover: true });
    for (const s of [A, B]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}/dms`);
    }

    step('alice makes a group from the DM list, and names it');
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-tab-add=dms]')`), "alice's + for a new group");
    await A.b.eval(`document.querySelector('[data-tab-add=dms]').click(), true`);
    await waitFor(() => A.b.eval(`!!${dialog('New group')}`), 'the New group dialog');
    await pick(A, 'New group', ['Bob', 'Carol']);
    await A.b.eval(setValue(`${dialog('New group')}.querySelector('fieldset input')`, 'Weekend'));
    const ready = await waitFor(() => A.b.eval(`[...${dialog('New group')}.querySelectorAll('input[type=checkbox]')].filter((x) => x.checked).length === 2 &&
        !${button(dialog('New group'), 'Make the group')}.disabled`), 'two picked').then(() => true, () => false);
    report(ready, 'the New group dialog shows bob and carol ticked, and Make the group ready');
    await sleep(300);
    await A.b.shot(`${HOME}/shot-groups-new.png`);
    await A.b.eval(`${button(dialog('New group'), 'Make the group')}.click(), true`);
    const made = await waitFor(async () => ((await A.b.eval(`${HEADER}?.textContent || ''`)) === 'Weekend' ? true : null), "the group's header on alice's page").catch(() => false);
    report(made, 'alice\'s page opens the group, "Weekend", as she makes it');
    const g = Object.keys(state('alice').group_dms || {})[0] || '';
    report(!!g && (await lineShown(A, 'You made the group “Weekend”.')), "alice's page shows the line where she made it");

    step("bob's page lists the group, and opens it");
    const row = `document.querySelector('[data-group-row=${q(g)}]')`;
    const listed = await waitFor(() => B.b.eval(`${row}?.textContent.includes('Weekend')`), "the group in bob's DM list").catch(() => false);
    report(listed, "bob's DM list shows the group by its name");
    await B.b.eval(`${row}.querySelector('button').click(), true`);
    report(await waitFor(async () => (await B.b.eval(`${HEADER}?.textContent || ''`)) === 'Weekend', "the group on bob's page").catch(() => false),
        "bob's page opens the group, its name in its header");
    report(await lineShown(B, 'Alice made the group “Weekend”.'), "bob's page shows the line where alice made it");

    step('bob writes, and sends a photo');
    await sends(B, 'hello group');
    report(await waitFor(async () => (await text(A)).includes('hello group'), "bob's message on alice's page").then(() => true, () => false),
        "alice's page reads bob's message");
    report(await waitFor(() => history('carol', g).some((m) => m.text === 'hello group' && !m.locked), "bob's message in carol's Dens").then(() => true, () => false),
        "carol's Dens opens bob's message, checking his signature inside it");
    const photo = execFileSync('bash', ['-c', `source "$0" && DEN=${den} upload bob "$HOME/photo.jpg" photo.jpg ${g}`,
        new URL('./lib.sh', import.meta.url).pathname]).toString().trim();
    api('bob', 'POST', `/api/dens/${den}/channels/${g}/messages`, { nonce: nonce(), text: '', attachments: [photo] });
    report(await waitFor(() => A.b.eval(`[...document.querySelectorAll('img')].some((i) => i.naturalWidth > 0 && i.src.includes('/files/${photo}/'))`),
        "bob's photo on alice's page").then(() => true, () => false), "alice's page shows the photo bob sent, its preview opened for the group");

    step('bob renames the group');
    await openPanel(B);
    await B.b.eval(`${button(PANEL + '.parentElement.parentElement', 'Rename')}.click(), true`);
    await waitFor(() => B.b.eval(`!!${dialog('Rename the group')}`), 'the Rename dialog');
    await B.b.eval(setValue(`${dialog('Rename the group')}.querySelector('input')`, 'Long weekend'));
    await B.b.eval(`${button(dialog('Rename the group'), 'Save')}.click(), true`);
    report(await waitFor(async () => (await A.b.eval(`${HEADER}?.textContent || ''`)) === 'Long weekend', "the new name on alice's page").then(() => true, () => false),
        "alice's page shows the group's new name");
    report(await lineShown(A, 'Bob named the group “Long weekend”.'), "alice's page shows bob renaming it");
    // The name reaches the den only sealed: its database holds neither name.
    const data = `${HOME}/.local/share/dens-dev/m2a`;
    const plain = execFileSync('bash', ['-c', `grep -rlaE 'Long weekend|Weekend' "${data}" --include='*.db*' --include='dens*' 2>/dev/null || true`]).toString().trim();
    report(plain === '', `the den's database holds the group's name only sealed${plain ? `: found in ${plain}` : ''}`);

    step('bob adds dave from the group\'s members');
    await openPanel(B);
    await B.b.eval(`${button(PANEL + '.parentElement', 'Add people')}.click(), true`);
    await waitFor(() => B.b.eval(`!!${dialog('Add people')}`), 'the Add people dialog');
    await pick(B, 'Add people', ['Dave']);
    await B.b.eval(`${button(dialog('Add people'), 'Add')}.click(), true`);
    report(await lineShown(A, 'Bob added Dave.'), "alice's page shows bob adding dave");
    report(await waitFor(() => (state('dave').group_dms?.[g]?.members || []).includes(daveID), "the group in dave's Dens").then(() => true, () => false),
        "dave's Dens knows the group, him in it, from bob's signed add");
    report(!history('dave', g).some((m) => m.text === 'hello group'), 'dave reads nothing from before he joined');
    report(await waitFor(() => state('dave').group_dms?.[g]?.name === 'Long weekend', "the group's name in dave's Dens").then(() => true, () => false),
        "dave's Dens names the group as bob's key carries the name, signed by bob, though he joined after the rename");
    api('carol', 'POST', `/api/dens/${den}/channels/${g}/messages`, { nonce: nonce(), text: 'welcome, dave' });
    report(await waitFor(() => history('dave', g).some((m) => m.text === 'welcome, dave' && !m.locked), "carol's message in dave's Dens").then(() => true, () => false),
        'dave reads what carol wrote after he joined');
    await B.b.shot(`${HOME}/shot-groups-members-bob-${which}.png`);
    // A member's row is their picture and name, the one who made the group
    // marked under theirs; comparing codes is each row's ⋯.
    const rows = JSON.parse(await B.b.eval(`JSON.stringify([...${PANEL}.querySelectorAll('[data-member]')].map((li) => ({
        name: li.querySelector('[data-member-name]')?.textContent.trim(), under: li.querySelector('[data-remover]')?.textContent.trim() || '',
        buttons: [...li.querySelectorAll('button')].map((b) => b.getAttribute('aria-label') || 'row'),
        clipped: [...li.querySelectorAll('[data-member-name]')].some((n) => n.scrollWidth > n.clientWidth) })))`));
    report(rows.length === 4 && rows[0].name === 'Alice' && rows[0].under === 'Made the group' && rows.slice(1).every((r) => r.under === '')
        && rows.every((r) => !r.clipped) && rows.find((r) => r.name === 'Bob (you)')?.buttons.join() === 'row'
        && rows.filter((r) => r.name !== 'Bob (you)').every((r) => r.buttons.join() === `row,Options for ${r.name}`),
    `each of the group's rows is a picture and a whole name, alice's marked Made the group, and the others' a ⋯: ${q(rows)}`);
    await B.b.eval(`${PANEL}.querySelector('button[aria-label="Options for Carol"]').click(), true`);
    const carolItems = await waitFor(() => B.b.eval(`[...document.querySelectorAll('[role=menu][aria-label="Options for Carol"] [role=menuitem]')].map((i) => i.textContent.trim()).join('|')`),
        "carol's menu on bob's page", 3000).catch(() => '');
    report(carolItems === 'Compare codes', `carol's ⋯ on bob's page compares codes, and removes nobody: ${q(carolItems)}`);
    await B.b.eval(`document.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })), true`);

    step("alice lets bob edit a list, whose tick on his page and edit dave reads");
    const list = JSON.parse(api('alice', 'POST', `/api/dens/${den}/channels/${g}/messages`, { nonce: nonce(), text: '[ ] snacks\n[ ] drinks', editors: [bobID] }));
    const listBox = `document.querySelector('[data-id=${q(list.id)}] input[type=checkbox][aria-label="snacks"]')`;
    await waitFor(() => B.b.eval(`!!${listBox} && !${listBox}.disabled`), "the list's boxes on bob's page");
    await B.b.eval(`${listBox}.click(), true`);
    const reads = (text) => waitFor(() => history('dave', g).some((m) => m.id === list.id && m.text === text && !m.locked && (m.editors || []).join() === bobID),
        `${q(text)} in dave's Dens`).then(() => true, () => false);
    report(await reads('[x] snacks\n[ ] drinks'), "dave's Dens reads bob's tick as alice's list, bob named to edit it by her signature");
    api('bob', 'PATCH', `/api/dens/${den}/messages/${list.id}?channel=${g}`, { revision: 2, text: '[x] snacks\n[ ] drinks\n[ ] ice' });
    report(await reads('[x] snacks\n[ ] drinks\n[ ] ice'), "dave's Dens reads bob's edit after his tick");
    report(await waitFor(() => A.b.eval(`!!document.querySelector('[data-id=${q(list.id)}] input[type=checkbox][aria-label="ice"]')`), "bob's edit on alice's page")
        .then(() => true, () => false), "alice's page shows bob's edit");

    step("carol, on the last release, ticks a list alice let her edit, which waits for alice's Keep");
    const shared = JSON.parse(api('alice', 'POST', `/api/dens/${den}/channels/${g}/messages`, { nonce: nonce(), text: '[ ] cake\n[ ] candles', editors: [carolID] }));
    const sharedRow = `document.querySelector('[data-id=${q(shared.id)}]')`;
    await waitFor(() => history('carol', g).some((m) => m.id === shared.id && !m.locked), "alice's list in carol's Dens");
    api('carol', 'POST', `/api/dens/${den}/messages/${shared.id}/tasks/1?channel=${g}`, { checked: true, text: 'candles' });
    report(await waitFor(() => B.b.eval(`${sharedRow}?.textContent.includes('waits for them')`), "carol's tick on bob's page").then(() => true, () => false),
        "bob's page shows carol's tick as waiting for alice, saying nothing of it");
    report(history('dave', g).some((m) => m.id === shared.id && m.locked === 'author' && m.text === ''), "dave's Dens holds carol's tick as waiting, unopened");
    const waiting = `${sharedRow}?.querySelector('[data-waiting]')`;
    const said = await waitFor(() => A.b.eval(`${waiting}?.textContent || ''`), "carol's tick on alice's page").catch(() => '');
    report(said.includes('@carol changed this') && said.includes('candles'), `alice's page shows who changed her list, and how it reads: ${q(said)}`);
    await A.b.shot(`${HOME}/shot-groups-waiting-alice-${which}.png`);
    await A.b.eval(`${button(sharedRow + ".querySelector('[data-waiting]')", 'Keep')}.click(), true`);
    report(await waitFor(() => B.b.eval(`!!${sharedRow}?.querySelector('input[type=checkbox][aria-label="candles"]')?.checked`), "the kept list on bob's page")
        .then(() => true, () => false), "bob's page reads the list alice kept, candles ticked");
    report(await waitFor(() => history('dave', g).some((m) => m.id === shared.id && !m.locked && m.text === '[ ] cake\n[x] candles' && !(m.editors || []).length),
        "the kept list in dave's Dens").then(() => true, () => false), "dave's Dens reads it as alice's, with nobody else to edit it");

    step('alice, who made the group, removes bob');
    await openPanel(A);
    const more = `${PANEL}.querySelector('button[aria-label="Options for Bob"]')`;
    await waitFor(() => A.b.eval(`!!${more}`), "bob's ⋯ on alice's page");
    await A.b.eval(`${more}.click(), true`);
    const remove = `[...document.querySelectorAll('[role=menu][aria-label="Options for Bob"] [role=menuitem]')].find((i) => i.textContent.trim() === 'Remove from the group')`;
    await waitFor(() => A.b.eval(`!!${remove}`), "Remove in bob's menu on alice's page");
    await A.b.eval(`${remove}.click(), true`);
    report(await lineShown(A, 'You removed Bob.'), "alice's page shows she removed bob");
    report(await waitFor(() => B.b.eval(`!${row}`), "the group gone from bob's page").then(() => true, () => false), "bob's page no longer lists the group");
    await sends(A, 'after bob');
    await sleep(2000);
    report(!(await text(B)).includes('after bob'), "bob's page reads nothing written after he was removed");
    let refused = false;
    try {
        history('bob', g);
    } catch {
        refused = true;
    }
    report(refused, "bob's Dens can't fetch the group's messages after he was removed");
    await B.b.shot(`${HOME}/shot-groups-removed-bob-${which}.png`);

    step('carol leaves through her Dens, and alice from the group');
    api('carol', 'POST', `/api/dens/${den}/group-dms/${g}/leave`);
    report(await lineShown(A, 'Carol left the group.'), "alice's page shows carol leaving");
    await A.b.shot(`${HOME}/shot-groups-lines-alice.png`);
    await openPanel(A);
    await A.b.eval(`${button(PANEL + '.parentElement', 'Leave')}.click(), true`);
    report(await waitFor(() => A.b.eval(`!${row}`), "the group gone from alice's page").then(() => true, () => false), "alice's page no longer lists the group once she leaves");
    report((state('dave').group_dms?.[g]?.members || []).join() === daveID, "dave's Dens holds the group with him alone in it");
} catch (e) {
    c.fail(e);
} finally {
    for (const s of [A, B]) await s.b?.close().catch(() => {});
    await sleep(100);
}
c.finish();
