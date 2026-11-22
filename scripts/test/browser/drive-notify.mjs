// The notify check, of notifications, as alice in the browser named: DMs
// turned on in the settings, which the browser lets the page show; a DM
// from bob while her tab is behind another shows through the browser, with
// who sent it, the den and its start, a spoiler hidden, and its click
// opens the DM at the message; read on another device, it's taken back; on
// screen in the DM, nothing shows; and muted from the DM's bell, nothing
// shows. With mentions turned on, staff's @everyone in a channel shows, as
// bob, a moderator, writes it; the @everyone switch under Mentions,
// on by default, silences it, though it still counts among her mentions;
// and on again, the channel at Nothing from its bell silences it too.
// With mentions off, #general at All messages from its menu shows a
// message that mentions nobody, a level for the DM is refused, and back at
// Mentions from its bell #general shows nothing. Do not disturb from the moon in her panel lights it, for an
// hour; with an end a few seconds off, a DM shows nothing but the tab's
// dot, and once the end passes the next DM shows and the moon goes out.
// Then with no tab open, Dens's helper, dens shortcuts, shows a DM
// through the desktop's notification service, dunst here, as Dens's, and
// its click opens the page at the message, paired as dens open pairs it;
// read elsewhere, it's taken back from the desktop too. Then the
// tab's icon: the mark, then with its dot while the DM waits behind
// another tab, and the mark again once it's read.
//
//   run.sh notify chromium|firefox
import { execFileSync, spawn } from 'node:child_process';
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, HOME, checks, launch, lib, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, DM: dm, GENERAL: general } = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);
const BUS = { ...process.env, DBUS_SESSION_BUS_ADDRESS: `unix:path=${process.env.XDG_RUNTIME_DIR}/bus` };
const SHOWN = `${HOME}/notify-shown.txt`;
const OPENED = `${HOME}/notify-opened.txt`;

// The page's notifications, as it asks the browser for them: each one kept
// for the check, and its closing noted.
const RECORD = `() => {
    const Real = window.Notification;
    if (typeof Real !== 'function') return;
    window.notes = [];
    window.Notification = class extends Real {
        constructor(title, opts) {
            super(title, opts);
            this.shut = false;
            window.notes.push(this);
        }

        close() {
            this.shut = true;
            super.close();
        }
    };
}`;

const b = await launch(which, which === 'chromium' ? 9541 : 9542, { preload: RECORD, prefs: { 'permissions.default.desktop-notification': 1 } });
const shot = (name) => b.shot(`${HOME}/shot-notify-${which}-${name}.png`);
const notes = () => b.eval(`(window.notes || []).map((n) => ({ title: n.title, body: n.body, tag: n.tag, shut: n.shut }))`);
const has = (text) => b.eval(`document.body.innerText.includes(${JSON.stringify(text)})`);
const lit = (id) => b.eval(`!!document.querySelector('[data-id="${id}"]')?.className.includes('bg-warning/20')`);
const send = (text) => lib('send', 'bob', dm, text);
const read = (id) => lib('api', 'alice', 'PUT', `/api/dens/${den}/channels/${dm}/read`, JSON.stringify({ message_id: id }));
const settings = () => JSON.parse(lib('api', 'alice', 'GET', '/api/settings')).notifications;
const lastLine = (file) => (fs.existsSync(file) ? fs.readFileSync(file, 'utf8').trim().split('\n').at(-1) : '');
const displayed = () => Number(execFileSync('dunstctl', ['count', 'displayed'], { env: BUS }).toString().trim());
const may = (p) => p.catch(() => false);
const howMany = async () => (await notes()).length;
// newest waits for the notification after the first count shown, and
// gives it.
const newest = (count, what) => may(waitFor(async () => {
    const n = await notes();
    return n.length === count + 1 && n[count];
}, what));
// level picks a level from the open channel's bell.
const BELL = `document.querySelector('[data-levels]')`;
async function level(key) {
    await waitFor(() => b.eval(`!!${BELL}`), "the channel's bell");
    await b.eval(`${BELL}.click(), true`);
    await waitFor(() => b.eval(`!!document.querySelector('[data-menu-item=${key}]')`), `the bell's ${key}`);
    await b.eval(`document.querySelector('[data-menu-item=${key}]').click(), true`);
}

// hide puts another tab in front of alice's, and front brings hers back.
let other = null;
async function hide() {
    if (which === 'chromium') {
        other = (await b.cdp('Target.createTarget', { url: 'about:blank' }, null)).targetId;
        await b.cdp('Target.activateTarget', { targetId: other }, null);
    } else {
        other = (await b.bidi('browsingContext.create', { type: 'tab' })).context;
        await b.bidi('browsingContext.activate', { context: other });
    }
    await waitFor(() => b.eval(`document.visibilityState === 'hidden'`), "alice's tab hidden");
    // The page tells the service.
    await sleep(500);
}
async function front() {
    if (which === 'chromium') {
        await b.cdp('Target.closeTarget', { targetId: other }, null);
        await b.cdp('Page.bringToFront');
    } else {
        await b.bidi('browsingContext.close', { context: other });
        await b.bidi('browsingContext.activate', { context: b.context });
    }
    await waitFor(() => b.eval(`document.visibilityState === 'visible'`), "alice's tab in front");
    await sleep(500);
}

let helper = null;
try {
    step('pairing alice, and turning DMs on in the settings');
    await pair(b, ALICE);
    if (which === 'chromium') await b.cdp('Browser.grantPermissions', { permissions: ['notifications'], origin: 'http://localhost:18484' }, null);
    await b.go(`http://localhost:18484/den/${den}/${general}`);
    await waitFor(() => b.eval(`!!document.querySelector('textarea')`), 'general');
    await openSettings(b, 'Notifications');
    await waitFor(() => b.eval(`!!document.getElementById('notify-dms')`), 'the notifications card');
    await b.eval(`document.getElementById('notify-dms').click(), true`, true);
    await waitFor(() => settings().dms, 'DMs on, kept by the service');
    // The card says only what calls for a line: nothing, with the
    // browser letting the page notify.
    await sleep(500);
    const explained = await b.eval(`['asks to show them', 'A Dens tab shows them', 'Mute a den'].filter((x) => document.querySelector('[role=dialog]').innerText.includes(x))`);
    await shot('settings');
    c.report(explained.length === 0, `DMs turned on in the settings, kept by the service, the card explaining nothing${explained.length ? `; it says: ${explained.join(', ')}` : ''}`);
    await b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);

    step("a DM from bob, with alice's tab behind another");
    const ICON = `(() => { const l = document.querySelector('link[rel=icon]'); return l.getAttribute('href') === l.dataset.unread ? 'dotted' : 'mark'; })()`;
    const plain = await b.eval(ICON);
    await hide();
    const first = send('a DM for alice, with ||a secret||');
    const dotted = await may(waitFor(async () => (await b.eval(ICON)) === 'dotted', 'the dot on the tab', 10000));
    c.report(plain === 'mark' && dotted, `the tab's icon is the mark, then takes its dot as the DM waits behind another tab (${plain}, ${dotted})`);
    const [one] = await waitFor(async () => {
        const n = await notes();
        return n.length > 0 && n;
    }, 'the notification');
    c.report(one.title === 'Bob · Voice Den' && one.body === 'a DM for alice, with (spoiler)' && one.tag === `${den}/${dm}`,
        `the notification shows who sent it, the den, and the start without the spoiler: ${JSON.stringify(one)}`);

    step('its click');
    await b.eval(`window.notes[0].dispatchEvent(new Event('click')), true`);
    const opened = await may(waitFor(() => b.eval(`location.pathname === '/den/${den}/${dm}'`), 'the DM open', 10000));
    const litFirst = await may(waitFor(() => lit(first), 'the message lit', 5000));
    const shut = (await notes())[0].shut;
    c.report(opened && litFirst && shut, `its click opened the DM (${opened}) at the message (${litFirst}), and closed the notification (${shut})`);
    await front();
    const undotted = await may(waitFor(async () => (await b.eval(ICON)) === 'mark', 'the dot gone', 10000));
    c.report(undotted, "read, the tab's icon is the mark again");
    await shot('opened');

    step('read on another device, it goes');
    await hide();
    const second = send('another DM');
    await waitFor(async () => (await notes()).length === 2, 'the second notification');
    read(second);
    const taken = await may(waitFor(async () => (await notes())[1].shut, 'the notification taken back', 10000));
    c.report(taken, 'read on another device, the notification was taken back');
    await front();

    step('on screen, in the DM, nothing shows');
    send('while she reads');
    await sleep(3000);
    const quiet = (await notes()).length === 2;
    c.report(quiet, `with the DM on screen, no notification showed (${(await notes()).length} in all)`);

    step('muted from its bell, nothing shows');
    await b.eval(`document.querySelector('button[aria-label="Mute the DM with Bob"]').click(), true`);
    await waitFor(() => settings().muted?.[den]?.channels?.includes(dm), 'the DM muted');
    await waitFor(() => b.eval(`!!document.querySelector('button[aria-label="Unmute the DM with Bob"]')`), 'the bell struck');
    await hide();
    send('while muted');
    await sleep(3000);
    const mutedQuiet = (await notes()).length === 2;
    await front();
    await b.eval(`document.querySelector('button[aria-label="Unmute the DM with Bob"]').click(), true`);
    const unmuted = await may(waitFor(() => !settings().muted?.[den], 'the DM unmuted', 5000));
    c.report(mutedQuiet && unmuted, `with the DM muted from its bell, no notification showed (${mutedQuiet}), and it unmuted (${unmuted})`);

    step("staff's @everyone in a channel, with mentions on");
    await openSettings(b, 'Notifications');
    await waitFor(() => b.eval(`!!document.getElementById('notify-everyone')`), 'the @everyone switch');
    const under = await b.eval(`(() => { const t = document.getElementById('notify-everyone'); return t.checked && t.disabled; })()`);
    await b.eval(`document.getElementById('notify-mentions').click(), true`, true);
    await waitFor(() => settings().mentions, 'mentions on, kept by the service');
    const on = await may(waitFor(() => b.eval(`(() => { const t = document.getElementById('notify-everyone'); return t.checked && !t.disabled; })()`),
        '@everyone on with mentions', 5000));
    await shot('everyone-settings');
    c.report(under && on, `@everyone is on by default, under Mentions, which turning on lets it notify (${under}, ${on})`);
    await b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    const count = () => JSON.parse(lib('api', 'alice', 'GET', `/api/dens/${den}/state`)).read_states.find((r) => r.channel_id === general)?.mention_count || 0;
    const shout = (text) => lib('send', 'bob', general, text);
    const counted = count();
    await hide();
    shout('@everyone the den moves tonight');
    const everyone = await may(waitFor(async () => {
        const n = await notes();
        return n.length === 3 && n[2];
    }, 'the @everyone notification'));
    c.report(everyone && everyone.title === 'Bob in #general · Voice Den' && everyone.body === '@everyone the den moves tonight' && everyone.tag === `${den}/${general}`,
        `staff's @everyone shows, as a mention in #general: ${JSON.stringify(everyone)}`);
    await front();

    step('@everyone switched off, nothing shows, and it still counts');
    await openSettings(b, 'Notifications');
    await waitFor(() => b.eval(`!!document.getElementById('notify-everyone')`), 'the @everyone switch');
    await b.eval(`document.getElementById('notify-everyone').click(), true`, true);
    await waitFor(() => settings().everyoneOff, '@everyone off, kept by the service');
    await b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await hide();
    shout('@everyone, silenced');
    await sleep(3000);
    const silenced = (await notes()).length === 3;
    const counts = await may(waitFor(() => count() === counted + 2, 'the mentions counted', 5000));
    c.report(silenced && counts, `with @everyone off, nothing showed (${silenced}), and both counted among her mentions (${counted} to ${count()})`);
    await front();

    step('@everyone on again, its channel at Nothing from its bell, nothing shows');
    await openSettings(b, 'Notifications');
    await waitFor(() => b.eval(`!!document.getElementById('notify-everyone')`), 'the @everyone switch');
    await b.eval(`document.getElementById('notify-everyone').click(), true`, true);
    await waitFor(() => !settings().everyoneOff, '@everyone on again');
    await b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await b.go(`http://localhost:18484/den/${den}/${general}`);
    await level('nothing');
    await waitFor(() => settings().muted?.[den]?.channels?.includes(general), '#general at Nothing');
    // A page loaded anew keeps what it notified of from then on.
    await b.go(`http://localhost:18484/den/${den}/${dm}`);
    await waitFor(() => b.eval(`!!document.querySelector('textarea')`), 'the DM');
    const before = (await notes()).length;
    await hide();
    shout('@everyone, muted');
    await sleep(3000);
    const muted = (await notes()).length === before;
    await front();
    await b.go(`http://localhost:18484/den/${den}/${general}`);
    const struck = await may(waitFor(() => b.eval(`${BELL}?.dataset.levels === 'nothing'`), "#general's struck bell", 5000));
    await level('mentions');
    const unmutedGeneral = await may(waitFor(() => !settings().muted?.[den], '#general at Mentions', 5000));
    c.report(muted && struck && unmutedGeneral,
        `with #general at Nothing, its @everyone showed nothing (${muted}), its bell struck (${struck}), and it went back to Mentions (${unmutedGeneral})`);

    step('#general at All messages from its menu, with mentions off');
    lib('api', 'alice', 'POST', '/api/settings', JSON.stringify({ notifications: { mentions: false } }));
    await waitFor(() => !settings().mentions, 'mentions off');
    await b.eval(`document.querySelector('[data-channel-row="${general}"] button[aria-label="Options for general"]').click(), true`);
    await waitFor(() => b.eval(`!!document.querySelector('[data-menu-item=notifications]')`), "#general's menu");
    const rows = await b.eval(`[...document.querySelectorAll('[role=menu][aria-label="Options for general"] [role=menuitem]')].map((x) => x.textContent.trim()).join('|')`);
    await b.eval(`document.querySelector('[data-menu-item=notifications]').click(), true`);
    const LEVELS = '[data-flyout=notifications] [role=menuitemradio]';
    await waitFor(() => b.eval(`document.querySelectorAll('${LEVELS}').length === 3`), 'its levels beside it');
    const levels = await b.eval(`[...document.querySelectorAll('${LEVELS}')].map((x) => x.textContent.trim() + (x.getAttribute('aria-checked') === 'true' ? '*' : '')).join('|')`);
    await shot('levels');
    await b.eval(`document.querySelector('[data-flyout=notifications] [data-menu-item=all]').click(), true`);
    await waitFor(() => settings().muted?.[den]?.all?.includes(general), '#general at All messages');
    const ringing = await may(waitFor(() => b.eval(`!!document.querySelector('[data-channel-row="${general}"] button[aria-label*="all messages"]')`),
        'the ringing bell on its row', 5000));
    await b.go(`http://localhost:18484/den/${den}/${dm}`);
    await waitFor(() => b.eval(`!!document.querySelector('textarea')`), 'the DM');
    let had = await howMany();
    await hide();
    const toNoOne = shout('no one in particular');
    const all = await newest(had, 'the message at All messages');
    c.report(rows === 'Mark as read|Notifications|Copy link|Edit channel|Move up|Move down' && levels === 'All messages|Mentions*|Nothing' && ringing &&
        all?.title === 'Bob in #general · Voice Den' && all?.body === 'no one in particular',
        `#general's menu (${rows}) opens its levels (${levels}); at All messages its row rings (${ringing}), and a message to no one shows, with mentions off: ${JSON.stringify(all)}`);
    await front();
    // Read while the page that shows it lives, it goes, which a page
    // loaded anew couldn't do, Firefox keeping it on the desktop.
    lib('api', 'alice', 'PUT', `/api/dens/${den}/channels/${general}/read`, JSON.stringify({ message_id: toNoOne }));
    await waitFor(async () => (await notes())[had].shut, '#general read');

    step("a DM's bell stays a mute: the service refuses it a level");
    let refused = false;
    try {
        lib('api', 'alice', 'POST', `/api/dens/${den}/notifications`, JSON.stringify({ channel: dm, level: 'all' }));
    } catch {
        refused = true;
    }
    const kept = settings().muted?.[den];
    c.report(refused && !kept?.all?.includes(dm), `a level for the DM was refused (${refused}), the levels left as they were: ${JSON.stringify(kept)}`);

    step('back at Mentions from its bell, a message to no one shows nothing');
    await b.go(`http://localhost:18484/den/${den}/${general}`);
    await level('mentions');
    await waitFor(() => !settings().muted?.[den], '#general at Mentions');
    await b.go(`http://localhost:18484/den/${den}/${dm}`);
    await waitFor(() => b.eval(`!!document.querySelector('textarea')`), 'the DM');
    had = await howMany();
    await hide();
    shout('no one, at Mentions');
    await sleep(3000);
    const atMentions = (await howMany()) === had;
    await front();
    c.report(atMentions, 'back at Mentions, with mentions off, a message to no one showed nothing');

    step('Do not disturb from the moon, for an hour');
    await b.go(`http://localhost:18484/den/${den}/${general}`);
    const MOON = `document.querySelector('[data-quiet]')`;
    await waitFor(() => b.eval(`!!${MOON}`), 'the moon');
    const unlit = await b.eval(`${MOON}.dataset.quiet`);
    await b.eval(`${MOON}.click(), true`);
    await waitFor(() => b.eval(`!!document.querySelector('[data-menu-item=hour]')`), "the moon's ends");
    const ends = await b.eval(`[...document.querySelectorAll('[role=menu][aria-label="Do not disturb"] [role=menuitem]')].map((x) => x.textContent.trim()).join('|')`);
    await shot('moon-ends');
    await b.eval(`document.querySelector('[data-menu-item=hour]').click(), true`);
    await waitFor(() => settings().dnd, 'Do not disturb on');
    const hour = Date.parse(settings().dndUntil) - Date.now();
    const moonLit = await may(waitFor(() => b.eval(`${MOON}.dataset.quiet === 'on' && ${MOON}.title.startsWith('Do not disturb until ')`), 'the moon lit', 5000));
    const tip = await b.eval(`${MOON}.title`);
    await shot('moon');
    c.report(unlit === 'off' && ends === 'For an hour|Until tomorrow|Until I turn it off' && hour > 3590000 && hour <= 3600000 && moonLit,
        `the moon (${unlit}) opens its ends (${ends}); For an hour ends in ${Math.round(hour / 1000)} s, and lights it: "${tip}"`);

    step('Do not disturb until a few seconds off: a DM shows nothing until its end');
    const end = new Date(Date.now() + 8000);
    lib('api', 'alice', 'POST', '/api/settings', JSON.stringify({ notifications: { dnd: true, dndUntil: end.toISOString() } }));
    await waitFor(() => Date.parse(settings().dndUntil) === end.getTime(), 'the end a few seconds off');
    had = await howMany();
    await hide();
    send('held under Do not disturb');
    const dottedHeld = await may(waitFor(async () => (await b.eval(ICON)) === 'dotted', 'the dot on the tab', 5000));
    await sleep(2000);
    const held = (await howMany()) === had;
    await sleep(Math.max(0, end.getTime() - Date.now()) + 1000);
    const after = send('after Do not disturb');
    const past = await newest(had, 'the DM after its end');
    await front();
    const out = await may(waitFor(() => b.eval(`${MOON}.dataset.quiet === 'off'`), 'the moon out', 5000));
    c.report(dottedHeld && held && past?.body === 'after Do not disturb' && out,
        `under Do not disturb a DM took the tab's dot (${dottedHeld}) and showed nothing (${held}); past its end the next showed alone (${JSON.stringify(past)}), and the moon went out (${out})`);
    // Read, it goes, so the helper's are the desktop's only notifications,
    // Firefox showing a page's there too.
    read(after);
    await waitFor(async () => (await notes())[had].shut, 'the DM read');

    step("Dens's helper, with no tab open");
    helper = spawn(`${HOME}/dens-dev`, ['shortcuts'], {
        env: { ...BUS, PATH: `${HOME}/notify-bin:${process.env.PATH}` },
        stdio: ['ignore', 'ignore', fs.openSync(`${HOME}/helper.log`, 'w')],
    });
    await openSettings(b, 'Notifications');
    const desktop = await may(waitFor(() => has('With no Dens tab open, your desktop shows them.'), 'the helper showing notifications', 20000));
    const said = await b.eval(`[...document.querySelectorAll('p')].find((p) => p.textContent.includes('With no Dens tab open'))?.textContent || ''`);
    await shot('helper-settings');
    c.report(desktop, `the settings say the desktop shows them with no tab open, once Dens's helper runs: ${said}`);
    await b.go('about:blank');
    await sleep(1000);
    const third = send('a DM with no tab open');
    const line = await may(waitFor(() => lastLine(SHOWN), 'the desktop showing it'));
    c.report(line === 'Dens|Bob · Voice Den|a DM with no tab open|chat.dens.Dens', `the desktop showed it as Dens's: ${line}`);

    step("the desktop's notification, clicked");
    execFileSync('dunstctl', ['action', '0'], { env: BUS });
    const url = await may(waitFor(() => lastLine(OPENED), 'the helper opening the page'));
    const named = typeof url === 'string' && url.startsWith(`http://localhost:18484/den/${den}/${dm}#token=`) && url.endsWith(`&message=${third}`);
    let atMessage = false;
    if (named) {
        await b.go(url);
        atMessage = await may(waitFor(async () => (await b.eval(`location.pathname === '/den/${den}/${dm}' && location.hash === ''`)) && lit(third),
            'the page at the message', 15000));
    }
    await shot('helper-opened');
    c.report(named && atMessage, `its click opened the page at the message (${atMessage}), paired: ${String(url).replace(/token=[^&]*/, 'token=…')}`);

    step("read elsewhere, it goes from the desktop");
    await b.go('about:blank');
    await sleep(1000);
    const fourth = send('one more');
    await waitFor(() => displayed() === 1, 'the desktop showing it');
    read(fourth);
    const gone = await may(waitFor(() => displayed() === 0, "the desktop's notification taken back", 10000));
    c.report(gone, "read elsewhere, the desktop's notification was taken back");
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    helper?.kill();
    await b.close();
}
c.finish();
