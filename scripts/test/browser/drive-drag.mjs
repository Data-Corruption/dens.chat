// The M17.2 drag check, in the browsers named, alice's first: alice, who
// owns the den, drags a channel into the channels in no group and into
// another group, and a group above another, which bob's den state follows;
// then, with bob in Lounge's call in his browser, muted, she drags his row
// onto Games, and his page joins its call, sealed and still muted, which
// ends the one in Lounge; and Move to in his menu in the call takes him
// back. From M19.3, with bob and carol moderators, carol, on alice's
// browser, moves bob: his menu offers her Move to and neither Mute for
// everyone nor Disconnect, and she drags him onto Games. Chromium drags as
// a mouse does, the drag handed over through the DevTools protocol, which
// a headless Chromium needs; Firefox's drag under WebDriver starts but
// never ends, so its drops are the page's own events, dispatched in order,
// and at the end a mouse's drag in it starts one. From M22.2, alice makes
// a channel and a group from each menu with the mouse, a right click on
// the list's empty space among them, a channel in a group from the group's
// menu, and one from the den's name by the keyboard alone, and moves a
// channel down and back up from its menu; and Move to opens its channels
// beside the menu.
//
//   run.sh drag chromium|firefox chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, CAROL, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , aliceBrowser = 'chromium', bobBrowser = 'firefox'] = process.argv;
const { DEN: den, LOUNGE: lounge, GENERAL: general, GAMES: games, TALK: talk, PLAY: play, NEWS: news } = process.env;
const c = checks();
const ok = (what) => c.report(true, what);
const state = () => JSON.parse(lib('api', 'bob', 'GET', `/api/dens/${den}/state`));
const channel = (id) => state().channels.find((x) => x.id === id);
const callOf = (id) => (state().calls || []).find((x) => x.channel_id === id)?.members || [];
const row = (id) => `document.querySelector('[data-channel-row="${id}"]')`;
const heading = (id) => `document.querySelector('[data-group-row="${id}"]')`;

// dragOnto drags the element from selects onto the one to selects, near
// its top, its middle or its foot (where), as each browser can.
async function dragOnto(b, from, to, where) {
    const point = await b.eval(`(() => {
        const f = ${from}.getBoundingClientRect(), t = ${to}.getBoundingClientRect();
        const y = ${JSON.stringify(where)} === 'top' ? t.top + 3 : ${JSON.stringify(where)} === 'foot' ? t.bottom - 3 : t.top + t.height / 2;
        return JSON.stringify({ fx: f.left + f.width / 3, fy: f.top + f.height / 2, tx: t.left + t.width / 3, ty: y });
    })()`);
    const { fx, fy, tx, ty } = JSON.parse(point);
    if (b.name === 'chromium') {
        await b.cdp('Input.setInterceptDrags', { enabled: true });
        const mouse = (type, x, y, extra = {}) => b.cdp('Input.dispatchMouseEvent', { type, x, y, ...extra });
        const seen = b.events.length;
        await mouse('mouseMoved', fx, fy);
        await mouse('mousePressed', fx, fy, { button: 'left', clickCount: 1 });
        await mouse('mouseMoved', fx, fy + 6, { button: 'left', buttons: 1 });
        await mouse('mouseMoved', (fx + tx) / 2, (fy + ty) / 2, { button: 'left', buttons: 1 });
        const drag = await waitFor(() => b.events.slice(seen).find((e) => e.method === 'Input.dragIntercepted'), 'the drag to start', 5000);
        const data = drag.params.data;
        await b.cdp('Input.dispatchDragEvent', { type: 'dragEnter', x: tx, y: ty, data });
        await b.cdp('Input.dispatchDragEvent', { type: 'dragOver', x: tx, y: ty, data });
        await b.cdp('Input.dispatchDragEvent', { type: 'drop', x: tx, y: ty, data });
        await mouse('mouseReleased', tx, ty, { button: 'left', clickCount: 1 });
        await b.cdp('Input.setInterceptDrags', { enabled: false });
        return;
    }
    const dropped = await b.eval(`(() => {
        const from = ${from}, to = ${to};
        const dt = new DataTransfer();
        const fire = (el, type, x, y) => el.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt, clientX: x, clientY: y }));
        fire(from, 'dragstart', ${fx}, ${fy});
        fire(to, 'dragenter', ${tx}, ${ty});
        fire(to, 'dragover', ${tx}, ${ty});
        const took = !fire(to, 'drop', ${tx}, ${ty});
        fire(from, 'dragend', ${tx}, ${ty});
        return took;
    })()`);
    if (!dropped) throw new Error('the page took no drop');
}

// hoverOver moves the mouse over what x names, where its menu's flyout
// opens.
async function hoverOver(b, x) {
    const p = JSON.parse(await b.eval(`(() => { const r = ${x}.getBoundingClientRect(); return JSON.stringify({ x: r.left + r.width / 2, y: r.top + r.height / 2 }); })()`));
    await b.move(p.x, p.y);
}

// MENU_ROW is a row of the menu open, by its key.
const MENU_ROW = (key) => `document.querySelector('[data-menu-item=${JSON.stringify(key)}]')`;
const DIALOG = (title) => `document.querySelector('[role=dialog][aria-label=${JSON.stringify(title)}]')`;

// makeFromMenus has alice make channels and groups from each menu with the
// mouse: a right click on the list's empty space, the den's name, the Den
// tab's +, and a group's ⋯, whose channel lands in it; one from the den's
// name by the keyboard alone; then move one down and back up from its
// menu, and take them all away.
async function makeFromMenus(b) {
    const tag = Date.now().toString(36).slice(-4);
    const made = [];
    // name types into the dialog that opened, title, and makes it.
    async function name(title, text) {
        await waitFor(() => b.eval(`!!${DIALOG(title)}`), `the ${title} dialog`, 5000);
        await b.eval(`${DIALOG(title)}.querySelector('input').focus(), true`);
        await b.type(text);
        await b.key('Enter');
        await waitFor(async () => !(await b.eval(`!!${DIALOG(title)}`)), `${text} made`, 8000);
        made.push(text);
    }
    async function pick(open, key, title, text) {
        await open();
        await waitFor(() => b.eval(`!!${MENU_ROW(key)}`), `the menu's ${key}`, 5000);
        const p = JSON.parse(await b.eval(`(() => { const r = ${MENU_ROW(key)}.getBoundingClientRect(); return JSON.stringify({ x: r.left + 20, y: r.top + r.height / 2 }); })()`));
        await b.click(p.x, p.y);
        await name(title, text);
    }
    const at = async (x) => JSON.parse(await b.eval(`(() => { const r = ${x}.getBoundingClientRect(); return JSON.stringify({ x: r.left + r.width / 2, y: r.top + r.height / 2 }); })()`));
    const clickOn = async (x, button = 'left') => {
        const p = await at(x);
        await b.click(p.x, p.y, button);
    };
    // The list's empty space, under its last row.
    const space = async () => {
        const p = JSON.parse(await b.eval(`(() => { const n = document.querySelector('nav[aria-label=Channels]').getBoundingClientRect();
            const rows = [...document.querySelectorAll('nav[aria-label=Channels] [data-channel-row], nav[aria-label=Channels] [data-group-row]')];
            const last = Math.max(...rows.map((r) => r.getBoundingClientRect().bottom));
            return JSON.stringify({ x: n.left + n.width / 2, y: Math.min(n.bottom - 10, last + 40) }); })()`));
        await b.click(p.x, p.y, 'right');
    };
    const NAME = `document.querySelector('[data-den-menu]')`;
    const PLUS = `document.querySelector('[data-tab-add=den]')`;
    await pick(space, 'channel', 'New channel', `space-${tag}`);
    await pick(space, 'group', 'New group', `Space ${tag}`);
    await pick(() => clickOn(NAME), 'channel', 'New channel', `name-${tag}`);
    await pick(() => clickOn(NAME), 'group', 'New group', `Name ${tag}`);
    await pick(() => clickOn(PLUS), 'channel', 'New channel', `plus-${tag}`);
    await pick(() => clickOn(PLUS), 'group', 'New group', `Plus ${tag}`);
    await pick(() => clickOn(heading(talk), 'right'), 'channel', 'New channel', `talk-${tag}`);
    // The keyboard: the den's name, its menu by the down arrow, then down
    // to New channel.
    await b.eval(`${NAME}.focus(), true`);
    await b.key('ArrowDown');
    await waitFor(() => b.eval(`document.activeElement?.dataset.menuItem === 'manage'`), "the den's menu, by the keyboard", 5000);
    await b.key('ArrowDown');
    await b.key('Enter');
    await name('New channel', `keys-${tag}`);
    const v = state();
    const ch = (n) => v.channels.find((x) => x.name === n);
    c.report(['space', 'name', 'plus', 'keys'].every((w) => ch(`${w}-${tag}`) && !ch(`${w}-${tag}`).group_id),
        `${b.name}: a channel from the list's right click, the den's name, the Den tab's + and the den's name by the keyboard`);
    c.report(['Space', 'Name', 'Plus'].every((w) => v.groups.some((g) => g.name === `${w} ${tag}`)), `${b.name}: a group from each of the first three`);
    c.report(ch(`talk-${tag}`)?.group_id === talk, `${b.name}: a channel from Talk's menu, by its right click, lands in Talk`);
    await b.shot(`${HOME}/shot-drag-${b.name}-made.png`);
    // Move down, then up, from the channel's ⋯ by the keyboard.
    const id = ch(`keys-${tag}`).id;
    const place = () => channel(id).position;
    const before = place();
    const dots = `document.querySelector('button[aria-label=${JSON.stringify(`Options for keys-${tag}`)}]')`;
    // The ⋯ shows once its row has the focus, and Tab reaches it from the
    // row's channel.
    const toDots = async () => {
        await waitFor(() => b.eval(`!!${dots}`), "the new channel's row", 8000);
        await b.eval(`${dots}.closest('[data-channel-row]').querySelector('button').focus(), true`);
        await b.key('Tab');
        await waitFor(() => b.eval(`document.activeElement === ${dots}`), "the channel's ⋯, by Tab", 5000);
    };
    await toDots();
    await b.key('Enter');
    await waitFor(() => b.eval(`document.activeElement?.dataset.menuItem === 'edit'`), "the channel's menu, by the keyboard", 5000);
    const up = await b.eval(`${MENU_ROW('up')}.disabled`);
    await b.key('ArrowDown');
    await b.key('Enter');
    const moved = await waitFor(() => place() === before - 1, 'the channel moved up', 8000).then(() => true, () => false);
    // The page has the move once the menu offers Move down.
    await waitFor(async () => {
        await toDots();
        await b.key('Enter');
        await waitFor(() => b.eval(`document.activeElement?.dataset.menuItem === 'edit'`), "the channel's menu again", 5000);
        if (await b.eval(`!${MENU_ROW('down')}.disabled`)) return true;
        await b.key('Escape');
        return false;
    }, 'Move down offered', 8000);
    await b.key('ArrowDown');
    await b.key('ArrowDown');
    await b.key('Enter');
    const back = await waitFor(() => place() === before, 'the channel moved back down', 8000).then(() => true, () => false);
    c.report(!up && moved && back, `${b.name}: Move up and Move down in a channel's menu, by the keyboard (up disabled: ${up}, moved: ${moved}, back: ${back})`);
    for (const x of state().channels.filter((y) => y.name.endsWith(tag))) lib('api', 'alice', 'DELETE', `/api/dens/${den}/channels/${x.id}`);
    for (const g of state().groups.filter((y) => y.name.endsWith(tag))) lib('api', 'alice', 'DELETE', `/api/dens/${den}/groups/${g.id}`);
    await waitFor(async () => !(await b.eval(`[...document.querySelectorAll('nav[aria-label=Channels] [data-channel-row]')].some((r) => r.textContent.includes(${JSON.stringify(tag)}))`)),
        'what was made gone', 8000);
}

const alice = await launch(aliceBrowser, 9351, { size: [1400, 900], hover: true });
const bob = await launch(bobBrowser, 9352, { size: [1400, 900], media: bobBrowser === 'firefox' ? 'pulse' : true });
try {
    await pair(alice, ALICE);
    await pair(bob, BOB);
    for (const b of [alice, bob]) {
        await b.go(`http://localhost:${b === alice ? ALICE.port : BOB.port}/den/${den}`);
        await waitFor(() => b.eval(`!!${row(news)}`), `${b.name}'s channel list`);
    }

    // A channel into the channels in no group, below general.
    await dragOnto(alice, row(news), row(general), 'foot');
    await waitFor(() => !channel(news).group_id && channel(news).position === channel(general).position + 1, 'news in no group, after general');
    ok(`${aliceBrowser}: news dragged out of Talk, below general`);
    // Into another group, onto its heading, which puts it first.
    await waitFor(() => alice.eval(`!!${heading(play)} && !${heading(talk)}?.parentElement.querySelector('[data-channel-row="${news}"]')`), 'news gone from Talk on the page');
    await dragOnto(alice, row(news), heading(play), 'middle');
    await waitFor(() => channel(news).group_id === play && channel(news).position === 0, 'news first in Play');
    // The list shows it as the den has it: news first under Play's heading.
    const under = (group) => `[...${heading(group)}.parentElement.querySelectorAll('[data-channel-row]')].map((r) => r.dataset.channelRow).join(' ')`;
    await waitFor(async () => (await alice.eval(under(play))).startsWith(news), "news first under Play on alice's page");
    ok(`${aliceBrowser}: news dragged onto Play's heading, first in it`);
    // A group above another.
    await dragOnto(alice, heading(play), heading(talk), 'top');
    await waitFor(() => state().groups.find((g) => g.id === play).position === 0, 'Play above Talk');
    const order = `[...document.querySelectorAll('[data-group-row]')].map((g) => g.dataset.groupRow).join(' ')`;
    await waitFor(async () => (await alice.eval(order)) === `${play} ${talk}`, "Play above Talk on alice's page");
    ok(`${aliceBrowser}: Play dragged above Talk`);
    await alice.shot(`${HOME}/shot-drag-${aliceBrowser}-list.png`);

    // Channels and groups from each menu, by the mouse and the keyboard.
    await makeFromMenus(alice);

    // Bob joins Lounge's call and mutes; alice drags him onto Games.
    const bobID = state().me.id;
    await bob.eval(`${row(lounge)}.querySelector('button[title^="Join the call"]').click(), true`, true);
    await waitFor(() => bob.eval(`!!${row(lounge)}.querySelector('[data-call-state="connected"]')`), "bob's call in Lounge", 30000);
    await bob.eval(`document.querySelector('button[aria-label="Mute"]').click(), true`);
    await waitFor(() => callOf(lounge).some((m) => m.id === bobID && m.muted), 'bob muted in Lounge');
    const member = `${row(lounge)}.parentElement.querySelector('[data-call-member="${bobID}"]')`;
    await waitFor(() => alice.eval(`!!${member}`), "bob's row under Lounge on alice's page");
    await dragOnto(alice, member, row(games), 'middle');
    await waitFor(() => callOf(games).some((m) => m.id === bobID && m.muted) && callOf(lounge).length === 0, 'bob in Games, muted, and Lounge empty', 30000);
    await waitFor(() => bob.eval(`!!${row(games)}.querySelector('[data-call-state="connected"]')`), "bob's page connected in Games", 30000);
    // The lock is among the rest of the call's controls, behind the block's arrow.
    await bob.eval(`document.querySelector('[data-call-more-button]').click(), true`);
    const lock = await waitFor(() => bob.eval(`document.querySelector('[data-call-more] [data-call-lock]')?.dataset.callLock || ''`), "bob's lock", 5000).catch(() => '');
    await bob.eval(`document.querySelector('[data-call-more-button]').click(), true`);
    c.report(lock === 'sealed', `${aliceBrowser} → ${bobBrowser}: bob dragged onto Games, whose call his page joined, sealed (${lock}) and muted as he was`);
    await bob.shot(`${HOME}/shot-drag-${bobBrowser}-moved.png`);

    // Move to, from his menu in the call, takes him back.
    const menuButton = `${row(games)}.parentElement.querySelector('[data-call-member="${bobID}"] button[aria-label^="Options for"]')`;
    await waitFor(() => alice.eval(`!!${menuButton}`), "bob's row under Games on alice's page");
    await alice.eval(`${menuButton}.click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-call-menu] [data-menu-item=move]')`), 'Move to in the menu');
    await hoverOver(alice, `document.querySelector('[data-call-menu] [data-menu-item=move]')`);
    const target = `[...document.querySelectorAll('[data-flyout=move] [role=menuitem]')].find((b) => b.textContent.trim() === 'Lounge')`;
    await waitFor(() => alice.eval(`!!${target}`), "Move to's channels beside the menu");
    const listed = await alice.eval(`[...document.querySelectorAll('[data-flyout=move] [role=menuitem]')].map((b) => b.textContent.trim()).join(', ')`);
    const beside = await alice.eval(`document.querySelector('[data-flyout=move]').getBoundingClientRect().left >= document.querySelector('[data-call-menu]').getBoundingClientRect().right - 1`);
    c.report(beside, `${aliceBrowser}: Move to opens its channels to the menu's right`);
    await alice.shot(`${HOME}/shot-drag-${aliceBrowser}-move-to.png`);
    await alice.eval(`${target}.click(), true`);
    await waitFor(() => callOf(lounge).some((m) => m.id === bobID && m.muted) && callOf(games).length === 0, 'bob back in Lounge', 30000);
    ok(`${aliceBrowser}: Move to (${listed}) took bob back to Lounge`);

    if (aliceBrowser === 'firefox') {
        // A mouse's drag of a row starts a drag, which the list shows by
        // making room in its channels in no group.
        const pt = JSON.parse(await alice.eval(`(() => { const r = ${row(general)}.getBoundingClientRect(); return JSON.stringify({ x: r.left + r.width / 3, y: r.top + r.height / 2 }); })()`));
        await alice.drag(pt.x, pt.y, pt.x, pt.y + 60);
        const started = await waitFor(() => alice.eval(`document.querySelector('[data-ungrouped]').className.includes('min-h-4')`), 'the drag to start', 5000).catch(() => false);
        c.report(started === true, 'firefox: a mouse dragging a row starts its drag');
    }

    // Bob and carol made moderators, carol moves bob, on alice's browser:
    // his menu offers her Move to, and none of what takes outranking him.
    const carolID = state().members.find((m) => m.username === 'carol').id;
    for (const id of [bobID, carolID]) lib('api', 'alice', 'PUT', `/api/dens/${den}/members/${id}/role`, '{"role":"moderator"}');
    await waitFor(() => state().members.filter((m) => m.role === 'moderator').length === 2, 'bob and carol moderators');
    await pair(alice, CAROL);
    await alice.go(`http://localhost:${CAROL.port}/den/${den}`);
    const bobRow = (channel) => `${row(channel)}?.parentElement.querySelector('[data-call-member="${bobID}"]')`;
    await waitFor(() => alice.eval(`!!${bobRow(lounge)}`), "bob's row under Lounge on carol's page");
    await alice.eval(`${bobRow(lounge)}.querySelector('button[aria-label^="Options for"]').click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-call-menu] [data-menu-item=move]')`), "Move to in bob's menu");
    await alice.eval(`document.querySelector('[data-call-menu] [data-menu-item=move]').click(), true`);
    await waitFor(() => alice.eval(`!!document.querySelector('[data-flyout=move]')`), "Move to's channels");
    const offered = await alice.eval(`[...document.querySelectorAll('[data-call-menu] [role=menuitem]')].map((b) => b.textContent.trim()).filter(Boolean).join(', ')`);
    const draggable = await alice.eval(`${bobRow(lounge)}.getAttribute('draggable')`);
    await alice.shot(`${HOME}/shot-drag-${aliceBrowser}-moderator.png`);
    await alice.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await waitFor(() => alice.eval(`${bobRow(lounge)}.getAttribute('draggable') === 'true'`), "bob's row draggable for carol", 5000);
    await dragOnto(alice, bobRow(lounge), row(games), 'middle');
    await waitFor(() => callOf(games).some((m) => m.id === bobID) && callOf(lounge).length === 0, 'bob in Games', 30000);
    await waitFor(() => bob.eval(`!!${row(games)}.querySelector('[data-call-state="connected"]')`), "bob's page connected in Games", 30000);
    c.report(/^Mute, Move to, .*Games/.test(offered) && !/Mute for everyone|Disconnect/.test(offered),
        `${aliceBrowser}: carol, a moderator, is offered ${offered} on bob, a moderator, and drags him onto Games (draggable with the menu open: ${draggable})`);
} catch (e) {
    c.fail(e);
} finally {
    await alice.close();
    await bob.close();
    await sleep(200);
    c.finish();
}
