// The switcher check, of getting around, as bob in the browser named and
// alice in Chromium: Ctrl+K, pressed as keys, opening the switcher with
// the page keeping the focus, listing the unread with his open den's
// first, and opening a channel in his other den by a few letters and back;
// Alt+↓ and Alt+↑ walking his den's text channels past a voice channel; Esc
// in a channel he's scrolled up in, with a message come since, marking it
// read and going to its newest; Mark all as read from the den's name;
// Copy link in a channel's menu; and a channel's link completed from #,
// sent, drawn as its chip and opened by alice, and one to a staff-only
// channel reading #unknown to bob; and Enter on a voice channel joining its
// call and opening its pane, from a channel, which Back returns to, and
// from Home.
//
//   run.sh switcher chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, PANE, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, SECOND_DEN: den2, GENERAL: general, RANDOM_CHANNEL: random, LONG: long, STAFF: staff, LOBBY: lobby, STAGE: stage } = process.env;
const DEN_URL = 'http://127.0.0.1:18485';
const c = checks();
const step = (what) => console.error(`.. ${what}`);
const may = (p) => p.catch(() => false);

const b = await launch(which, which === 'chromium' ? 9533 : 9534, { hover: true, media: true });
let a = null;
const shot = (name, on = b) => on.shot(`${HOME}/shot-switcher-${which}-${name}.png`);
const SWITCHER = `document.querySelector('[role=dialog][data-switcher]')`;
const BOX = `${SWITCHER}?.querySelector('input')`;
const entries = (on = b) => on.eval(`[...document.querySelectorAll('[data-switch]')].map((e) => e.getAttribute('data-switch') + ' ' + e.getAttribute('aria-selected'))`);
const path = (on = b) => on.eval('location.pathname');
const reads = (on, d) => on.eval(`(async () => {
    const v = await (await fetch('/api/dens/${d}/state')).json();
    return Object.fromEntries(v.read_states.map((r) => [r.channel_id, (r.message_id || '') === (r.last_message_id || '')]));
})()`);
const BOX_TEXT = `document.querySelector('textarea[data-message-box]')`;
const CHIP = (href) => `[...document.querySelectorAll('[data-id] a')].find((x) => x.getAttribute('href') === ${JSON.stringify(href)})`;
const linkTo = (channel) => `${DEN_URL}/#c/${den}/${channel}`;
const open = async (on, who, d, channel) => {
    await on.go(`http://localhost:${who.port}/den/${d}/${channel}`);
    await waitFor(() => on.eval(`document.querySelectorAll('[data-id]').length > 0`), `${d}/${channel}'s messages`);
};
// write types text into the message box, as keys would.
const write = async (on, text) => {
    await on.eval(`${BOX_TEXT}.focus(), true`);
    await on.type(text);
};

try {
    await pair(b, BOB);
    await open(b, BOB, den, general);
    await waitFor(async () => (await reads(b, den))[general], 'general read');

    step('Ctrl+K opens the switcher, the page keeping the focus');
    await b.eval('document.body.focus(), true');
    await b.chord(['Control'], 'k');
    const opened = await may(waitFor(() => b.eval(`!!${SWITCHER}`), 'the switcher', 5000));
    const focus = await b.eval(`JSON.stringify({ page: document.hasFocus(), box: document.activeElement === ${BOX} })`);
    const unread = await may(waitFor(async () => ((await entries()).length >= 3 ? entries() : false), 'the unread listed', 5000));
    await shot('unread');
    const first = (unread || []).map((e) => e.split(' ')[0]);
    c.report(opened && focus === '{"page":true,"box":true}' && first.indexOf(`${den}:text:${random}`) >= 0 &&
        first.indexOf(`${den}:text:${random}`) < first.indexOf(`${den2}:text:${lobby}`) && !first.includes(`${den}:text:${general}`),
    `Ctrl+K opened the switcher (${opened}) with the focus in its box (${focus}), listing the unread, this den's first, general read: ${first.join(', ')}`);

    step('a few letters open a channel in the other den, and back');
    await b.type('lob');
    const matched = await may(waitFor(async () => ((await entries())[0] === `${den2}:text:${lobby} true` ? entries() : false), 'lobby first', 5000));
    await b.key('Enter');
    const there = await may(waitFor(async () => (await path()) === `/den/${den2}/${lobby}` && !(await b.eval(`!!${SWITCHER}`)), 'Second Den\'s lobby', 8000));
    await b.chord(['Control'], 'k');
    await waitFor(() => b.eval(`document.activeElement === ${BOX}`), 'the switcher again', 5000);
    await b.type('gen');
    await may(waitFor(async () => (await entries())[0]?.startsWith(`${den}:text:${general}`), 'general first', 5000));
    await b.key('Enter');
    const back = await may(waitFor(async () => (await path()) === `/den/${den}/${general}`, 'back to general', 8000));
    // Ctrl+K again closes it.
    await b.chord(['Control'], 'k');
    await waitFor(() => b.eval(`!!${SWITCHER}`), 'the switcher', 5000);
    await b.chord(['Control'], 'k');
    const closed = await may(waitFor(async () => !(await b.eval(`!!${SWITCHER}`)), 'the switcher closed', 5000));
    c.report(matched && there && back && closed,
        `"lob" put Second Den's lobby first (${JSON.stringify(matched)}) and Enter opened it (${there}); "gen" took bob back to general (${back}); and Ctrl+K closes it (${closed})`);

    step('Alt+↓ and Alt+↑ walk the text channels, past the voice channel');
    await b.eval('document.activeElement?.blur(), true');
    await b.chord(['Alt'], 'ArrowDown');
    const down = await may(waitFor(async () => (await path()) === `/den/${den}/${random}`, 'random', 5000));
    await b.chord(['Alt'], 'ArrowDown');
    const down2 = await may(waitFor(async () => (await path()) === `/den/${den}/${long}`, 'long', 5000));
    await b.chord(['Alt'], 'ArrowUp');
    await may(waitFor(async () => (await path()) === `/den/${den}/${random}`, 'random again', 5000));
    await b.chord(['Alt'], 'ArrowUp');
    const up = await may(waitFor(async () => (await path()) === `/den/${den}/${general}`, 'general', 5000));
    c.report(down && down2 && up, `Alt+↓ went from general past Stage to random (${down}) and long (${down2}), and Alt+↑ twice back to general (${up})`);

    step('Esc marks a channel read and goes to its newest');
    await open(b, BOB, den, long);
    await may(waitFor(async () => (await reads(b, den))[long], 'long read at its end', 8000));
    await b.eval(`(() => { const s = document.querySelector('[data-scroller]'); s.scrollTop = 0; s.dispatchEvent(new Event('scroll')); return true; })()`);
    await sleep(1000);
    lib('send', 'alice', long, 'Long line new');
    const behind = await may(waitFor(async () => (await b.eval(`document.body.innerText.includes('1 new message')`)) && !(await reads(b, den))[long], 'long unread', 8000));
    // Esc in the box with text in it is the box's; empty, it's the page's.
    await write(b, 'half');
    await b.key('Escape');
    await sleep(800);
    const kept = !(await reads(b, den))[long];
    await b.eval(`(() => { const t = ${BOX_TEXT}; t.value = ''; t.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
    await sleep(300);
    await b.key('Escape');
    const read = await may(waitFor(async () => (await reads(b, den))[long], 'long read', 8000));
    const newest = await may(waitFor(() => b.eval(`(() => {
        const row = [...document.querySelectorAll('[data-id]')].find((r) => r.textContent.includes('Long line new'));
        const s = document.querySelector('[data-scroller]').getBoundingClientRect();
        const r = row?.getBoundingClientRect();
        return !!r && r.top >= s.top && r.bottom <= s.bottom;
    })()`), 'the newest in view', 5000));
    await shot('esc');
    c.report(behind && kept && read && newest,
        `scrolled up, a new message left long unread (${behind}); Esc with text in the box left it so (${kept}), and with the box empty marked it read (${read}) and showed the newest (${newest})`);

    step('Mark all as read');
    lib('send', 'alice', general, 'More in general');
    lib('send', 'alice', random, 'More in random');
    const waiting = await may(waitFor(async () => {
        const r = await reads(b, den);
        return !r[general] && !r[random];
    }, 'general and random unread', 8000));
    await b.eval(`document.querySelector('[data-den-menu]').click(), true`);
    await waitFor(() => b.eval(`!!document.querySelector('[data-menu-item=read]:not(:disabled)')`), 'Mark all as read', 5000);
    await b.eval(`document.querySelector('[data-menu-item=read]').click(), true`);
    const PILLS = `[...document.querySelectorAll('[data-channel-row] [data-unread]')]`;
    const allRead = await may(waitFor(async () => {
        const r = await reads(b, den);
        return r[general] && r[random] && !(await b.eval(`${PILLS}.length`));
    }, 'every channel read', 8000));
    const left = allRead ? '' : JSON.stringify({ reads: await reads(b, den), pills: await b.eval(`${PILLS}.map((p) => p.closest('[data-channel-row]').textContent.trim())`) });
    await b.eval(`document.querySelector('[data-den-menu]').click(), true`);
    const off = await may(waitFor(() => b.eval(`!!document.querySelector('[data-menu-item=read]:disabled')`), 'Mark all as read, off', 3000));
    // Esc closes the menu, once it has the focus, and marks nothing.
    await waitFor(() => b.eval(`!!document.activeElement?.closest('[role=menu]')`), "the den's menu focused", 3000);
    await b.key('Escape');
    await waitFor(async () => !(await b.eval(`!!document.querySelector('[data-menu-item=read]')`)), "the den's menu closed", 3000);
    c.report(waiting && allRead && off, `general and random unread (${waiting}) read at one press of Mark all as read (${allRead}${left && `, ${left}`}), which then shows off (${off})`);

    step("a channel's menu copies its link");
    await b.eval(`(() => { window.copied = []; navigator.clipboard.writeText = async (t) => { window.copied.push(t); }; return true; })()`);
    await b.eval(`document.querySelector('[data-channel-row="${random}"] button[aria-label="Options for random"]').click(), true`);
    await waitFor(() => b.eval(`!!document.querySelector('[data-menu-item=link]')`), "random's menu", 5000);
    const offered = await b.eval(`[...document.querySelectorAll('[role=menu][aria-label="Options for random"] [role=menuitem]')].map((x) => x.textContent.trim()).join('|')`);
    await b.eval(`document.querySelector('[data-menu-item=link]').click(), true`);
    const copied = await may(waitFor(async () => (await b.eval('window.copied'))?.includes(linkTo(random)), 'the link copied', 5000));
    c.report(offered === 'Mark as read|Copy link' && copied, `bob, a member, gets random's menu (${offered}), and Copy link copies ${linkTo(random)} (${copied})`);

    step("a channel's link, completed from #");
    await open(b, BOB, den, general);
    await write(b, 'see #ran');
    const offeredLink = await may(waitFor(() => b.eval(`!!document.querySelector('[data-completion="#random"][aria-selected=true]')`), '#random offered', 5000));
    await b.key('Tab');
    const inBox = await b.eval(`${BOX_TEXT}.value`);
    await b.key('Enter');
    const chip = await may(waitFor(() => b.eval(`${CHIP(linkTo(random))}?.textContent.trim()`), "the link's chip", 8000));
    await shot('chip');
    c.report(offeredLink && inBox === `see ${linkTo(random)} ` && chip === '#random',
        `"#ran" offered random (${offeredLink}), Tab put its link in the box ("${inBox}"), and sent it shows as ${chip}`);

    step('opened by alice, and #unknown to bob for a channel he can\'t see');
    a = await launch('chromium', 9535, { hover: true });
    await pair(a, ALICE);
    await open(a, ALICE, den, general);
    await waitFor(() => a.eval(`!!${CHIP(linkTo(random))}`), "the chip on alice's page");
    await a.eval(`${CHIP(linkTo(random))}.click(), true`);
    const followed = await may(waitFor(async () => (await path(a)) === `/den/${den}/${random}`, 'random on alice\'s page', 8000));
    await open(a, ALICE, den, general);
    await write(a, 'staff: #sta');
    await may(waitFor(() => a.eval(`!!document.querySelector('[data-completion="#staff"]')`), '#staff offered', 5000));
    await a.key('Tab');
    await a.key('Enter');
    const unknown = await may(waitFor(() => b.eval(`${CHIP(linkTo(staff))}?.textContent.trim()`), "the staff link's chip on bob's page", 8000));
    await shot('unknown');
    c.report(followed && unknown === '#unknown', `alice's click on the chip opened random (${followed}), and her link to staff reads ${unknown} to bob`);

    step("a voice channel's Enter joins its call and opens its pane");
    // switchTo opens what a few letters find first, by Ctrl+K and Enter.
    const switchTo = async (letters, key) => {
        await b.eval('document.activeElement?.blur(), true');
        await b.chord(['Control'], 'k');
        await waitFor(() => b.eval(`document.activeElement === ${BOX}`), 'the switcher', 5000);
        await b.type(letters);
        await waitFor(async () => (await entries())[0]?.startsWith(key), `${key} first`, 5000);
        await b.key('Enter');
    };
    const inPane = () => may(waitFor(async () => (await path()) === `/den/${den}/${stage}` && (await b.eval(`!!${PANE}`)), "Stage's pane", 10000));
    await switchTo('sta', `${den}:voice:${stage}`);
    const fromChannel = await inPane();
    await b.eval('(setTimeout(() => history.back()), true)');
    const backTo = await may(waitFor(async () => (await path()) === `/den/${den}/${general}` && !(await b.eval(`!!${PANE}`)), 'general again', 8000));
    const stays = await b.eval(`!!document.querySelector('button[aria-label="Leave the call"]')`);
    await b.eval(`document.querySelector('button[aria-label="Leave the call"]').click(), true`);
    await b.go(`http://localhost:${BOB.port}/`);
    await waitFor(() => b.eval(`!!document.querySelector('[data-switcher-button]')`), 'Home');
    await switchTo('sta', `${den}:voice:${stage}`);
    const fromHome = await inPane();
    await shot('stage');
    await b.eval(`document.querySelector('button[aria-label="Leave the call"]').click(), true`);
    c.report(fromChannel && backTo && stays && fromHome,
        `Enter on Stage joined its call and opened its pane from general (${fromChannel}), Back returned to general (${backTo}) with the call kept (${stays}), and from Home it did the same (${fromHome})`);
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    await Promise.race([b.close(), sleep(5000)]);
    if (a) await Promise.race([a.close(), sleep(5000)]);
    c.finish();
}
