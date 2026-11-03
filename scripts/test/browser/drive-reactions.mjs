// The M15.3 check of reactions: alice, who owns the den, in Chromium, and
// bob in the browser named. Bob reacts to alice's message from the smiley
// beside its ⋯, which shows as the ⋯ does, under the pointer, and alice
// from its menu's Add reaction, each through the picker, one of Unicode's
// emoji and one of the den's; each page shows the chips, its own marked;
// a click on a chip adds the member's or takes it back; the row's + opens
// the picker; a chip's tip says who, You first; and the menu, by keyboard,
// opens the picker in its search, where Enter picks the first found and
// Escape gives the focus back to the ⋯. In their DM, which the
// den can't read, each reacts, the den's emoji going by name, and the other
// sees it. Alice, staff, takes bob's reaction off from the menu's Remove
// reactions. With bob reading in the middle of #general, alice's reactions
// on the old message above his view, enough to wrap its row onto another,
// leave his view where it was; and at the newest, one on the newest keeps
// it in view.
//
//   run.sh reactions chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, checks, launch, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, DM: dm, OLD: old, MSG: msg, DM_MSG: dmMsg } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;
const q = (s) => JSON.stringify(s);
const row = (id) => `document.querySelector('[data-id="${id}"]')`;
const PICKER = `document.querySelector('[data-emoji-picker][aria-label="Add a reaction"]')`;
const SCROLLER = `document.querySelector('[data-scroller]')`;

// chips lists what a message's row shows of its reactions: each chip's
// emoji, its count, whether it's the member's, and whether it shows an
// image, loaded.
const chips = (s, id) => s.b.eval(`(() => { const r = ${row(id)}; if (!r) return null;
    return JSON.stringify([...r.querySelectorAll('[data-reaction]')].map((c) => { const imgs = [...c.querySelectorAll('img')];
        return { key: c.dataset.reaction, count: Number(c.dataset.count), mine: c.getAttribute('aria-pressed') === 'true',
            img: imgs.length > 0 && imgs.every((i) => i.complete && i.naturalWidth > 0) }; })); })()`).then((v) => (v ? JSON.parse(v) : null));
const said = (list) => (list || []).map((x) => `${x.key}=${x.count}${x.mine ? '*' : ''}`).join(' ');

// shows waits for a page to show a message's chips as want says.
function shows(s, id, want, what) {
    return waitFor(async () => {
        const v = await chips(s, id);
        return v && said(v) === want ? v : null;
    }, `${what} on ${s.name}'s page`, 10000).catch(() => chips(s, id));
}

// at is where on the page an element's middle is, scrolled into view.
const at = async (s, expr) => JSON.parse(await s.b.eval(`(() => { const e = ${expr}; e.scrollIntoView({ block: 'nearest' });
    const r = e.getBoundingClientRect(); return JSON.stringify([r.left + r.width / 2, r.top + r.height / 2]); })()`));

async function clickOn(s, expr) {
    const [x, y] = await at(s, expr);
    await s.b.click(x, y);
}

// pick searches the reaction picker and picks a tile.
async function pick(s, query, tile) {
    await waitFor(() => s.b.eval(`!!${PICKER}?.querySelector('[data-section=g8] [data-emoji-tile]')`), `${s.name}'s picker`);
    await s.b.eval(`(() => { const el = ${PICKER}.querySelector('input[type=search]');
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, ${q(query)});
        el.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
    const t = `${PICKER}.querySelector('[data-emoji-tile="${tile}"]')`;
    await waitFor(() => s.b.eval(`!!${t}`), `${tile} in the picker`);
    await clickOn(s, t);
    await waitFor(async () => !(await s.b.eval(`!!${PICKER}`)), 'the picker closed');
}

// menu opens a message's ⋯ menu under the pointer and picks an item.
async function menu(s, id, item) {
    await clickOn(s, row(id));
    await clickOn(s, `${row(id)}.querySelector('button[aria-label="Message actions"]')`);
    const it = `[...${row(id)}.querySelectorAll('[role=menuitem]')].find((x) => x.textContent.trim() === ${q(item)})`;
    await waitFor(() => s.b.eval(`!!${it}`), `${item} in the menu`);
    await clickOn(s, it);
}

// tip says what a chip's tip says, once the pointer is over it.
async function tip(s, id, key) {
    const chip = `${row(id)}.querySelector('[data-reaction="${key}"]')`;
    const [x, y] = await at(s, chip);
    await s.b.move(x, y);
    const text = await waitFor(() => s.b.eval(`(() => { const t = ${chip}.parentElement.querySelector('[role=tooltip]');
        return t && !t.hidden ? t.textContent : ''; })()`), `${key}'s tip`, 5000).catch(() => '');
    await s.b.move(5, 5);
    return text;
}

// react reacts as a member's page does, through its service's API.
const react = (s, channel, id, body) => s.b.eval(`fetch('/api/dens/${den}/messages/${id}/reactions?channel=${channel}', { method: 'POST',
    headers: { 'Content-Type': 'application/json' }, body: ${q(JSON.stringify(body))} }).then((r) => r.status)`);

const alerts = (s) => s.b.eval(`[...document.querySelectorAll('[role=alert]')].map((a) => a.textContent.trim()).filter(Boolean)`);

try {
    A.b = await launch('chromium', 9591, { hover: true });
    B.b = await launch(which, which === 'chromium' ? 9592 : 9593, { hover: true });
    for (const s of [A, B]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}/${general}`);
        await waitFor(() => s.b.eval(`!!${row(msg)}`), `${s.name}'s #general`);
    }
    const party = await A.b.eval(`fetch('/api/dens/${den}/state').then((r) => r.json()).then((s) => s.emoji.find((e) => e.name === 'party').id)`);
    const P = `d:${party}`;

    step("bob reacts from the smiley beside alice's message's ⋯");
    await B.b.move(...(await at(B, row(msg))));
    const smiley = `${row(msg)}.querySelector('[data-react-button]')`;
    const visible = await waitFor(() => B.b.eval(`getComputedStyle(${smiley}.parentElement).opacity === '1'`), 'the smiley under the pointer').catch(() => false);
    report(visible, 'the smiley shows beside the ⋯ under the pointer');
    await clickOn(B, smiley);
    const placed = JSON.parse(await waitFor(() => B.b.eval(`(() => { const p = ${PICKER}; if (!p) return null; const r = p.getBoundingClientRect();
        return JSON.stringify({ top: Math.round(r.top), bottom: Math.round(r.bottom), left: Math.round(r.left), right: Math.round(r.right),
            vw: document.documentElement.clientWidth, vh: innerHeight, search: document.activeElement === p.querySelector('input[type=search]') }); })()`), 'the picker'));
    report(placed.top >= 0 && placed.bottom <= placed.vh && placed.left >= 0 && placed.right <= placed.vw && placed.search,
        `the picker opens within the window, beside the smiley, in its search: ${q(placed)}`);
    await B.b.shot(`${HOME}/shot-reactions-picker-${which}.png`);
    await pick(B, 'thumbs up', '👍');
    let v = await shows(B, msg, 'u:👍=1*', '👍');
    report(said(v) === 'u:👍=1*', `bob's page shows his 👍, marked: ${said(v)}`);
    v = await shows(A, msg, 'u:👍=1', '👍');
    report(said(v) === 'u:👍=1', `alice's page shows bob's 👍, not marked: ${said(v)}`);

    step("alice reacts with the den's :party: from the menu's Add reaction");
    await menu(A, msg, 'Add reaction');
    await pick(A, 'party', ':party:');
    for (const [s, want] of [[A, `u:👍=1 ${P}=1*`], [B, `u:👍=1* ${P}=1`]]) {
        v = await shows(s, msg, want, ':party:');
        report(said(v) === want && v[1]?.img, `${s.name}'s page shows :party: as the den's image: ${q(v)}`);
    }

    step("alice adds hers to bob's 👍 with a click on its chip, and bob's + opens the picker");
    await clickOn(A, `${row(msg)}.querySelector('[data-reaction="u:👍"]')`);
    v = await shows(B, msg, `u:👍=2* ${P}=1`, "alice's 👍");
    report(said(v) === `u:👍=2* ${P}=1`, `a click on a chip adds alice's to it, which bob's page counts: ${said(v)}`);
    await clickOn(B, `${row(msg)}.querySelector('[data-add-reaction]')`);
    await pick(B, 'party popper', '🎉');
    v = await shows(A, msg, `u:👍=2* ${P}=1* u:🎉=1`, '🎉');
    report(said(v) === `u:👍=2* ${P}=1* u:🎉=1`, `the row's + opens the picker, and bob's 🎉 joins the row: ${said(v)}`);
    await B.b.shot(`${HOME}/shot-reactions-row-${which}.png`);

    step("the chips' tips say who, You first");
    const tips = { bob: await tip(B, msg, 'u:👍'), alice: await tip(A, msg, P) };
    report(tips.bob === 'You and Alice reacted with 👍' && tips.alice === 'You reacted with :party:', `the tips: ${q(tips)}`);

    step('bob takes his 👍 back with a click');
    await clickOn(B, `${row(msg)}.querySelector('[data-reaction="u:👍"]')`);
    for (const [s, want] of [[B, `u:👍=1 ${P}=1 u:🎉=1*`], [A, `u:👍=1* ${P}=1* u:🎉=1`]]) {
        v = await shows(s, msg, want, 'his 👍 taken back');
        report(said(v) === want, `${s.name}'s page counts it taken back: ${said(v)}`);
    }

    step('the menu by keyboard: Add reaction, Escape, and Enter in the search');
    const dots = `${row(msg)}.querySelector('button[aria-label="Message actions"]')`;
    const focusedOn = () => A.b.eval(`document.activeElement?.getAttribute('aria-label') || document.activeElement?.textContent.trim() || ''`);
    const keys = async (...ks) => {
        for (const k of ks) {
            await A.b.key(k);
            await sleep(150);
        }
    };
    await A.b.eval(`${dots}.focus(), true`);
    await keys('Enter', 'ArrowDown');
    const second = await focusedOn();
    await keys('Enter');
    const searching = await waitFor(() => A.b.eval(`document.activeElement === ${PICKER}?.querySelector('input[type=search]')`), 'the search').catch(() => false);
    await keys('Escape');
    const back = await waitFor(async () => !(await A.b.eval(`!!${PICKER}`)) && (await focusedOn()) === 'Message actions', 'the focus back on the ⋯').catch(() => false);
    await keys('Enter', 'ArrowDown', 'Enter');
    await waitFor(() => A.b.eval(`document.activeElement === ${PICKER}?.querySelector('input[type=search]')`), 'the search again');
    await A.b.type('red heart');
    await keys('Enter');
    v = await shows(A, msg, `u:👍=1* ${P}=1* u:🎉=1 u:❤️=1*`, '❤️ by keyboard');
    report(second === 'Add reaction' && searching && back && said(v) === `u:👍=1* ${P}=1* u:🎉=1 u:❤️=1*`,
        `the menu's second item is ${second}, which opens the picker in its search (${searching}); Escape gives the focus back to the ⋯ (${back}); and Enter picks the first found: ${said(v)}`);
    await clickOn(A, `${row(msg)}.querySelector('[data-reaction="u:❤️"]')`);
    await shows(A, msg, `u:👍=1* ${P}=1* u:🎉=1`, '❤️ taken back');

    step("alice, staff, takes bob's 🎉 off from the menu's Remove reactions");
    await menu(A, msg, 'Remove reactions');
    const dialog = `document.querySelector('[role=dialog][aria-label="Remove reactions"]')`;
    await waitFor(() => A.b.eval(`!!${dialog}`), 'the dialog');
    const buttons = await A.b.eval(`[...${dialog}.querySelectorAll('[data-remove-reaction]')].map((r) => r.dataset.removeReaction + ':' + r.querySelector('button').textContent.trim())`);
    report(q(buttons) === q([`u:👍:None to remove`, `${P}:None to remove`, 'u:🎉:Remove 1']), `it offers what she outranks: ${q(buttons)}`);
    await A.b.shot(`${HOME}/shot-reactions-remove-${which}.png`);
    await clickOn(A, `${dialog}.querySelector('[data-remove-reaction="u:🎉"] button')`);
    v = await shows(B, msg, `u:👍=1 ${P}=1`, '🎉 taken off');
    report(said(v) === `u:👍=1 ${P}=1`, `bob's page loses his 🎉: ${said(v)}`);
    await A.b.eval(`${dialog}.querySelector('button[aria-label=Close]').click(), true`);

    step('in their DM, each reacts, and the other sees it');
    for (const s of [A, B]) {
        await s.b.go(`http://localhost:${s.port}/den/${den}/${dm}`);
        await waitFor(() => s.b.eval(`!!${row(dmMsg)}`), `${s.name}'s DM`);
    }
    await B.b.move(...(await at(B, row(dmMsg))));
    await clickOn(B, `${row(dmMsg)}.querySelector('[data-react-button]')`);
    await pick(B, 'tears of joy', '😂');
    v = await shows(A, dmMsg, 'u:😂=1', "bob's 😂");
    report(said(v) === 'u:😂=1', `alice's page opens bob's sealed set: ${said(v)}`);
    await menu(A, dmMsg, 'Add reaction');
    await pick(A, 'party', ':party:');
    for (const [s, want] of [[B, 'n:party=1 u:😂=1*'], [A, 'n:party=1* u:😂=1']]) {
        v = await shows(s, dmMsg, want, 'the DM\'s reactions');
        report(said(v) === want && v[0]?.img, `${s.name}'s page shows the DM's reactions, the den's emoji by name, as its image: ${q(v)}`);
    }
    await clickOn(B, `${row(dmMsg)}.querySelector('[data-reaction="u:😂"]')`);
    v = await shows(A, dmMsg, 'n:party=1*', "bob's 😂 taken back");
    report(said(v) === 'n:party=1*', `bob takes his back in the DM: ${said(v)}`);

    step('reactions above what bob reads leave his view where it is');
    for (const s of [A, B]) {
        await s.b.go(`http://localhost:${s.port}/den/${den}/${general}`);
        await waitFor(() => s.b.eval(`!!${row(old)}`), `${s.name}'s #general again`);
    }
    await sleep(800);
    // The old message just above the view.
    await B.b.eval(`(() => { const box = ${SCROLLER}; const r = ${row(old)}.getBoundingClientRect();
        box.scrollTop += r.bottom - box.getBoundingClientRect().top + 40; return true; })()`);
    await sleep(800);
    const reading = `(() => { const box = ${SCROLLER}; const top = box.getBoundingClientRect().top;
        const first = [...box.querySelectorAll('[data-id]')].find((e) => e.getBoundingClientRect().top >= top);
        const o = ${row(old)}.getBoundingClientRect();
        return JSON.stringify({ id: first.dataset.id, at: Math.round(first.getBoundingClientRect().top - top), old: Math.round(o.height), above: o.bottom <= top }); })()`;
    const before = JSON.parse(await B.b.eval(reading));
    const twenty = ['😀', '😃', '😄', '😁', '😆', '😅', '🤣', '😂', '🙂', '🙃', '🫠', '😉', '😊', '😇', '🥰', '😍', '🤩', '😘', '😗', '😚'];
    for (const e of twenty) await react(A, general, old, { emoji: e, on: true });
    await waitFor(async () => (await chips(B, old))?.length === 20, "the old message's twenty on bob's page");
    await sleep(500);
    const after = JSON.parse(await B.b.eval(reading));
    const rows = await B.b.eval(`new Set([...${row(old)}.querySelectorAll('[data-reaction]')].map((c) => Math.round(c.getBoundingClientRect().top))).size`);
    report(before.above && after.id === before.id && Math.abs(after.at - before.at) <= 1 && after.old > before.old && rows >= 2,
        `twenty reactions wrap the old message's row onto ${rows} lines above bob's view, which stays where it was: ${q({ before, after })}`);
    // A twenty-first is refused.
    const refused = await react(A, general, old, { emoji: '😙', on: true });
    report(refused === 502, `a twenty-first emoji is refused: ${refused}`);

    step('at the newest, a reaction on the newest keeps it in view');
    await B.b.eval(`(() => { const box = ${SCROLLER}; box.scrollTop = box.scrollHeight; return true; })()`);
    await sleep(800);
    await react(A, general, msg, { emoji: '🎉', on: true });
    await shows(B, msg, `u:👍=1 ${P}=1 u:🎉=1`, "alice's 🎉");
    await sleep(500);
    const end = JSON.parse(await B.b.eval(`(() => { const box = ${SCROLLER}; const r = ${row(msg)}.getBoundingClientRect();
        return JSON.stringify({ fromBottom: Math.round(box.scrollHeight - box.scrollTop - box.clientHeight), shown: r.bottom <= box.getBoundingClientRect().bottom + 1 }); })()`));
    report(end.fromBottom <= 2 && end.shown, `bob's view stays at the newest, the row in sight: ${q(end)}`);
    await B.b.shot(`${HOME}/shot-reactions-held-${which}.png`);

    for (const s of [A, B]) {
        const a = await alerts(s);
        report(a.length === 0, `${s.name}'s page shows no error: ${q(a)}`);
    }
} catch (e) {
    c.fail(e);
}
await sleep(100);
for (const s of [A, B]) await s.b?.close().catch(() => {});
c.finish();
