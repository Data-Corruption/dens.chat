// The M12.3 check of notifications, as alice in the browser named: DMs
// turned on in the settings, which the browser lets the page show; a DM
// from bob while her tab is behind another shows through the browser, with
// who sent it, the den and its start, a spoiler hidden, and its click
// opens the DM at the message; read on another device, it's taken back; on
// screen in the DM, nothing shows; and muted from the DM's bell, nothing
// shows. Then with no tab open, Dens's helper, dens shortcuts, shows a DM
// through the desktop's notification service, dunst here, as Dens's, and
// its click opens the page at the message, paired as dens open pairs it;
// read elsewhere, it's taken back from the desktop too.
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
    await openSettings(b, 'Chat & media');
    await waitFor(() => b.eval(`!!document.getElementById('notify-dms')`), 'the notifications card');
    await b.eval(`document.getElementById('notify-dms').click(), true`, true);
    await waitFor(() => settings().dms, 'DMs on, kept by the service');
    const where = await may(waitFor(() => has('This browser shows them while a Dens tab is open'), 'where they show', 5000));
    await shot('settings');
    c.report(where, 'DMs turned on in the settings, kept by the service, which say the browser shows them');
    await b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);

    step("a DM from bob, with alice's tab behind another");
    await hide();
    const first = send('a DM for alice, with ||a secret||');
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

    step("Dens's helper, with no tab open");
    helper = spawn(`${HOME}/dens-dev`, ['shortcuts'], {
        env: { ...BUS, PATH: `${HOME}/notify-bin:${process.env.PATH}` },
        stdio: ['ignore', 'ignore', fs.openSync(`${HOME}/helper.log`, 'w')],
    });
    await openSettings(b, 'Chat & media');
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
