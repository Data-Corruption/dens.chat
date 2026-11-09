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
// and at the end a mouse's drag in it starts one.
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
    const lock = await bob.eval(`document.querySelector('[data-call-lock]')?.dataset.callLock || ''`);
    c.report(lock === 'sealed', `${aliceBrowser} → ${bobBrowser}: bob dragged onto Games, whose call his page joined, sealed (${lock}) and muted as he was`);
    await bob.shot(`${HOME}/shot-drag-${bobBrowser}-moved.png`);

    // Move to, from his menu in the call, takes him back.
    const menuButton = `${row(games)}.parentElement.querySelector('[data-call-member="${bobID}"] button[aria-label^="Options for"]')`;
    await waitFor(() => alice.eval(`!!${menuButton}`), "bob's row under Games on alice's page");
    await alice.eval(`${menuButton}.click(), true`);
    const target = `[...document.querySelectorAll('[data-move-to] button')].find((b) => b.textContent.trim() === 'Lounge')`;
    await waitFor(() => alice.eval(`!!${target}`), 'Move to in the menu');
    const listed = await alice.eval(`[...document.querySelectorAll('[data-move-to] button')].map((b) => b.textContent.trim()).join(', ')`);
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
    await waitFor(() => alice.eval(`!!document.querySelector('[data-move-to]')`), "Move to in bob's menu");
    const offered = await alice.eval(`[...document.querySelectorAll('[data-call-menu] button')].map((b) => b.textContent.trim()).filter(Boolean).join(', ')`);
    const draggable = await alice.eval(`${bobRow(lounge)}.getAttribute('draggable')`);
    await alice.shot(`${HOME}/shot-drag-${aliceBrowser}-moderator.png`);
    await alice.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await waitFor(() => alice.eval(`${bobRow(lounge)}.getAttribute('draggable') === 'true'`), "bob's row draggable for carol", 5000);
    await dragOnto(alice, bobRow(lounge), row(games), 'middle');
    await waitFor(() => callOf(games).some((m) => m.id === bobID) && callOf(lounge).length === 0, 'bob in Games', 30000);
    await waitFor(() => bob.eval(`!!${row(games)}.querySelector('[data-call-state="connected"]')`), "bob's page connected in Games", 30000);
    c.report(offered.includes('Games') && !/Mute for everyone|Disconnect/.test(offered),
        `${aliceBrowser}: carol, a moderator, is offered ${offered} on bob, a moderator, and drags him onto Games (draggable with the menu open: ${draggable})`);
} catch (e) {
    c.fail(e);
} finally {
    await alice.close();
    await bob.close();
    await sleep(200);
    c.finish();
}
