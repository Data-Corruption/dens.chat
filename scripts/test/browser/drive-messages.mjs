// The M11.2 check of each message's ⋯ menu and links to messages, as bob
// in the browser named: the ⋯ showing on hover and focus, and its menu by
// keyboard, Reply, Copy text and Copy link, Edit, and Delete through its
// dialog; the menu by touch, through a long press, a touch screen having
// no ⋯ (M13.2); the chips of links to messages, opening one
// in its channel, in another channel, from another den and at an address
// its den left, and saying why one can't open: out of sight, gone, or in
// a den bob hasn't joined, even at his den's address; the
// den's address as a browser sees it; alice, the owner, seeing whose
// message she's deleting; and a link into a den gone offline opening from
// the cache.
//
//   run.sh messages chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, SECOND_DEN: den2, GENERAL: general, RANDOM_CHANNEL: random, LOBBY: lobby, BOB_FIRST: bob1, BOB_SECOND: bob2, RND: rnd } = process.env;
const DEN_URL = 'http://127.0.0.1:18485';
const c = checks();
const step = (what) => console.error(`.. ${what}`);

const b = await launch(which, which === 'chromium' ? 9533 : 9534, { hover: true });
const shot = (name, on = b) => on.shot(`${HOME}/shot-messages-${which}-${name}.png`);
const at = (on, sel) => on.eval(`(() => { const el = ${sel}; if (!el) return ''; el.scrollIntoView({ block: 'center' }); const r = el.getBoundingClientRect(); return JSON.stringify([r.x + r.width / 2, r.y + r.height / 2]); })()`).then((r) => r && JSON.parse(r));
const may = (p) => p.catch(() => false);
const ROW = (text) => `[...document.querySelectorAll('[data-id]')].find((r) => r.textContent.includes(${JSON.stringify(text)}))`;
const DOTS = (text) => `${ROW(text)}?.querySelector('button[aria-label="Message actions"]')`;
const MENU = `document.querySelector('[role=menu][aria-label="Message actions"]')`;
const DIALOG = `document.querySelector('[role=dialog][aria-label="Delete this message?"]')`;
const CHIP = (href) => `document.querySelector('[data-id] a[href="${href}"]')`;
const link = (channel, message) => `${DEN_URL}/#m/${den}/${channel}/${message}`;
const shown = (on, text) => on.eval(`getComputedStyle(${DOTS(text)}.parentElement).opacity`);
const items = (on = b) => on.eval(`[...(${MENU}?.querySelectorAll('[role=menuitem]') || [])].map((x) => x.textContent.trim())`);
const focused = (on = b) => on.eval(`(() => { const el = document.activeElement; return el?.getAttribute('aria-label') || el?.textContent.trim() || el?.tagName; })()`);
const has = (text, on = b) => on.eval(`document.body.innerText.includes(${JSON.stringify(text)})`);
const lit = (on, text) => on.eval(`!!${ROW(text)}?.className.includes('bg-warning/20')`);
// The clipboard copies to a list the check reads, rather than to the
// container's own, which headless browsers may refuse.
const tap = (on) => on.eval(`(() => { window.copied = []; navigator.clipboard.writeText = async (t) => { window.copied.push(t); }; return true; })()`);
const copied = (on = b) => on.eval('window.copied');
const open = async (on, who, d, channel) => {
    await on.go(`http://localhost:${who.port}/den/${d}/${channel}`);
    await waitFor(() => on.eval(`document.querySelectorAll('[data-id]').length > 0`), `${d}/${channel}'s messages`);
    await tap(on);
};
// press presses keys in turn, as a member would.
const press = async (...keys) => {
    for (const k of keys) {
        await b.key(k);
        await sleep(150);
    }
};
const menuWith = (focus) => waitFor(async () => (await items()).length > 0 && (await focused()) === focus, `the menu, at ${focus}`, 5000);

try {
    await pair(b, BOB);
    await open(b, BOB, den, general);

    step('the ⋯ shows on hover and on focus');
    const away = await shown(b, "Bob's first");
    const row = await at(b, ROW("Bob's first"));
    await b.move(row[0], row[1]);
    const hovered = await may(waitFor(async () => (await shown(b, "Bob's first")) === '1', 'the ⋯ on hover', 3000));
    await b.move(5, 5);
    await waitFor(async () => (await shown(b, "Bob's first")) === '0', 'the ⋯ hidden again', 3000);
    await b.eval(`${DOTS("Bob's first")}.focus(), true`);
    const onFocus = (await shown(b, "Bob's first")) === '1';
    c.report(away === '0' && hovered && onFocus, `the ⋯ is hidden (${away}), and shows on hover (${hovered}) and on focus (${onFocus})`);

    step('its menu by keyboard');
    await press('Enter');
    const opened = await may(menuWith('Reply'));
    const all = await items();
    await shot('menu');
    await press('ArrowDown');
    const down = await focused();
    await press('ArrowUp', 'ArrowUp');
    const wrapped = await focused();
    await press('Home');
    const home = await focused();
    await press('End');
    const end = await focused();
    await press('Escape');
    const closed = !(await b.eval(`!!${MENU}`)) && (await focused()) === 'Message actions';
    c.report(opened && JSON.stringify(all) === JSON.stringify(['Reply', 'Edit', 'Copy text', 'Copy link', 'Delete']) &&
        down === 'Edit' && wrapped === 'Delete' && home === 'Reply' && end === 'Delete' && closed,
        `Enter opened ${JSON.stringify(all)} at Reply (${opened}); the arrows moved to ${down} and round to ${wrapped}, Home to ${home}, End to ${end}, and Escape closed it back to the ⋯ (${closed})`);

    step('Reply, Copy text and Copy link');
    await press('ArrowDown');
    await menuWith('Reply');
    await press('Enter');
    const replying = await may(waitFor(async () => (await has('Replying to Bob')) && (await b.eval(`document.activeElement?.tagName === 'TEXTAREA'`)), 'the reply in the composer', 5000));
    await press('Escape');
    await b.eval(`${DOTS("Bob's first")}.focus(), true`);
    await press('Enter');
    await menuWith('Reply');
    await press('ArrowDown', 'ArrowDown', 'Enter');
    const text = await may(waitFor(async () => (await copied())?.includes("Bob's first") && (await has('Text copied')), 'the text copied', 5000));
    await press('Enter');
    await menuWith('Reply');
    await press('End', 'ArrowUp', 'Enter');
    const linked = await may(waitFor(async () => (await copied())?.includes(link(general, bob1)) && (await has('Link copied')), 'the link copied', 5000));
    c.report(replying && text && linked, `Reply started a reply in the composer (${replying}), Copy text copied the text (${text}), and Copy link ${link(general, bob1)} (${linked}): ${JSON.stringify(await copied())}`);

    step('Edit');
    await b.eval(`${DOTS("Bob's first")}.focus(), true`);
    await press('Enter');
    await menuWith('Reply');
    await press('ArrowDown', 'Enter');
    const editing = await may(waitFor(() => b.eval(`(() => { const el = document.activeElement; return el?.tagName === 'TEXTAREA' && el.value === "Bob's first" && el.selectionStart === el.value.length; })()`), 'the edit box, caret at its end', 5000));
    await b.type(' (fixed)');
    await press('Enter');
    const edited = await may(waitFor(() => b.eval(`!!${ROW("Bob's first (fixed)")}?.textContent.includes('(edited)')`), 'the edit saved', 5000));
    c.report(editing && edited, `Edit opened the box with the caret at the end (${editing}), and Enter saved it (${edited})`);

    step('Delete asks first');
    await b.eval(`${DOTS("Bob's third")}.focus(), true`);
    await press('Enter');
    await menuWith('Reply');
    await press('End', 'Enter');
    const asked = await may(waitFor(async () => (await b.eval(`!!${DIALOG}`)) && (await focused()) === 'Delete', 'the dialog, at Delete', 5000));
    const says = await b.eval(`${DIALOG}?.innerText || ''`);
    await shot('dialog');
    await press('Escape');
    const kept = await may(waitFor(async () => !(await b.eval(`!!${DIALOG}`)) && (await focused()) === 'Message actions' && (await b.eval(`!!${ROW("Bob's third")}`)), 'the dialog gone, the message kept', 5000));
    await press('Enter');
    await menuWith('Reply');
    await press('End', 'Enter');
    await waitFor(async () => (await focused()) === 'Delete', 'the dialog again', 5000);
    await press('Enter');
    const deleted = await may(waitFor(async () => !(await b.eval(`!!${ROW("Bob's third")}`)) && !(await b.eval(`!!${DIALOG}`)), 'the message deleted', 5000));
    c.report(asked && says.includes("It goes for everyone, and can't be brought back.") && !says.includes('This is') && kept && deleted,
        `Delete opened its dialog at its button (${asked}), saying ${JSON.stringify(says)}; Escape kept the message (${kept}), and Delete deleted it (${deleted})`);

    step("another member's message, and the menu's side");
    await b.eval(`${DOTS("Alice's first")}.focus(), true`);
    await press('Enter');
    await menuWith('Reply');
    const theirs = await items();
    await press('Escape');
    // The last message's menu opens upward, inside the list.
    const last = `[...document.querySelectorAll('[data-id]')].at(-1)`;
    await b.eval(`${last}.querySelector('button[aria-label="Message actions"]').focus(), true`);
    await press('Enter');
    await menuWith('Reply');
    const inside = await b.eval(`${MENU}.getBoundingClientRect().bottom <= document.querySelector('[data-scroller]').getBoundingClientRect().bottom + 1`);
    await press('Escape');
    c.report(JSON.stringify(theirs) === JSON.stringify(['Reply', 'Copy text', 'Copy link']) && inside,
        `alice's message offers bob ${JSON.stringify(theirs)}, and the last message's menu stays inside the list (${inside})`);

    step('links to messages');
    // Bob's message of links: here, there, staff, gone, away, moved and
    // taken, in order.
    const CHIPS = `[...${ROW(' away ')}.querySelectorAll('a')]`;
    const chip = (i) => `${CHIPS}[${i}]`;
    const chips = await b.eval(`${CHIPS}.map((a) => a.textContent.trim())`);
    const titled = await b.eval(`${chip(0)}.title === ${JSON.stringify(link(general, bob1))}`);
    c.report(JSON.stringify(chips) === JSON.stringify(['#general', '#random', '#unknown', '#general', '127.0.0.1:18499', '#general', '127.0.0.1:18485']) && titled,
        `the chips read ${JSON.stringify(chips)}, with the address on hover (${titled})`);
    const click = async (sel) => {
        const p = await waitFor(() => at(b, sel), sel);
        await b.click(p[0], p[1]);
    };
    const why = (i) => b.eval(`${chip(i)}.nextElementSibling?.getAttribute('role') === 'status' ? ${chip(i)}.nextElementSibling.textContent : ''`);
    const refusals = [];
    for (const i of [2, 3, 4, 6]) {
        await click(chip(i));
        refusals.push(await may(waitFor(() => why(i), `why chip ${i} won't open`, 5000)));
    }
    await shot('chips');
    c.report(JSON.stringify(refusals) === JSON.stringify(["That channel is gone, or you can't see it.", 'That message is gone: it was deleted, or the den no longer keeps it.',
        "You haven't joined the den at 127.0.0.1:18499.", "You haven't joined the den at 127.0.0.1:18485."]), `the others say why they won't open: ${JSON.stringify(refusals)}`);
    await click(chip(0));
    const here = await may(waitFor(() => lit(b, "Bob's first"), 'the message lit in place', 5000));
    // Wait out the light, so the next one is the moved link's.
    await waitFor(async () => !(await lit(b, "Bob's first")), 'the light gone', 5000);
    await click(chip(5));
    const moved = await may(waitFor(() => lit(b, "Bob's first"), 'the message lit from its old address', 5000));
    await click(chip(1));
    const inRandom = await may(waitFor(async () => (await b.eval(`location.pathname`)) === `/den/${den}/${random}`, 'random open', 10000));
    const there = inRandom && (await may(waitFor(() => lit(b, 'In random'), 'the message lit in random', 3000)));
    c.report(here && moved && there, `a link opened its message in the same channel (${here}), from an address the den left (${moved}), and in another channel (${there})`);

    step('a link from another den');
    await open(b, BOB, den2, lobby);
    const label = await waitFor(() => b.eval(`(${CHIP(link(general, bob2))})?.textContent.trim()`), 'the chip into Voice Den', 5000).catch(() => '');
    await click(CHIP(link(general, bob2)));
    const across = await may(waitFor(async () => (await b.eval(`location.pathname`)) === `/den/${den}/${general}` && (await lit(b, "Bob's second")), "bob's second lit in Voice Den", 10000));
    c.report(label === 'Voice Den › #general' && across, `the chip in Second Den reads ${JSON.stringify(label)}, and opened the message in Voice Den (${across})`);

    step("the den's address in a browser");
    await b.go(`${link(general, bob1)}`);
    const line = await b.eval(`document.body.innerText.trim()`);
    c.report(line.startsWith('This is a den: a community on Dens'), `the den's address answers ${JSON.stringify(line)}`);

    step("the owner sees whose message she's deleting");
    await pair(b, ALICE);
    await open(b, ALICE, den, general);
    await b.eval(`${DOTS("Bob's second")}.focus(), true`);
    await press('Enter');
    await menuWith('Reply');
    const hers = await items();
    await press('End', 'Enter');
    await waitFor(() => b.eval(`!!${DIALOG}`), 'the dialog', 5000);
    const whose = await b.eval(`${DIALOG}.innerText`);
    await press('Escape');
    c.report(JSON.stringify(hers) === JSON.stringify(['Reply', 'Copy text', 'Copy link', 'Delete']) && whose.includes("This is Bob's message, @bob. It goes for everyone"),
        `bob's message offers alice ${JSON.stringify(hers)}, and its dialog says ${JSON.stringify(whose)}`);

    step('the menu by a long press');
    let t = b;
    if (which === 'chromium') await b.touch(true);
    else t = await launch('firefox', 9535, { prefs: { 'ui.primaryPointerCapabilities': 1, 'ui.allPointerCapabilities': 1 } });
    try {
        if (t !== b) await pair(t, BOB);
        await open(t, t === b ? ALICE : BOB, den, general);
        const coarse = await t.eval(`matchMedia('(pointer: coarse)').matches && matchMedia('(hover: none)').matches`);
        await t.eval(`document.activeElement?.blur(), true`);
        // A touch screen's messages have no ⋯: a long press opens the menu,
        // which a wide window keeps beside the message (M13.2).
        const none = await t.eval(`getComputedStyle(${DOTS("Alice's first")}).display === 'none'`);
        const p = await waitFor(() => at(t, `${ROW("Alice's first")}.querySelector('.wrap-break-word')`), "alice's first");
        await t.tap(p[0], p[1]);
        await sleep(800);
        const tapped = !(await t.eval(`!!${MENU}`));
        await t.press(p[0], p[1]);
        const menu = await may(waitFor(async () => (await items(t)).includes('Copy text'), 'the menu, by a long press', 5000));
        await shot('touch', t);
        const item = await at(t, `[...${MENU}.querySelectorAll('[role=menuitem]')].find((x) => x.textContent.trim() === 'Copy text')`);
        await t.tap(item[0], item[1]);
        const took = await may(waitFor(async () => (await copied(t))?.includes("Alice's first"), 'the text copied by touch', 5000));
        c.report(coarse && none && tapped && menu && took,
            `on a touch screen (${coarse}) no message shows its ⋯ (${none}), a tap opens nothing (${tapped}), a long press opens the menu (${menu}), and a tap on Copy text copies (${took})`);
    } finally {
        if (t === b) await b.touch(false);
        else await Promise.race([t.close(), sleep(5000)]);
    }

    step("a link into a den that's offline opens from the cache");
    await pair(b, BOB);
    await open(b, BOB, den, general);
    lib('stop', `${HOME}/m2a`);
    await open(b, BOB, den2, lobby);
    await click(CHIP(link(general, bob2)));
    const cached = await may(waitFor(async () => (await b.eval(`location.pathname`)) === `/den/${den}/${general}` && (await lit(b, "Bob's second")), "bob's second lit from the cache", 15000));
    await shot('done');
    c.report(cached, `with Voice Den offline, the link opened bob's second from the cache (${cached})`);
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    await Promise.race([b.close(), sleep(5000)]);
    c.finish();
}
