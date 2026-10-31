// The Android e2e's phone walk (M13): the page in the app's WebView at 390
// CSS pixels, through drive.mjs, as the phone check walks the browsers
// (scripts/test/browser/screens.mjs), with what only a phone has: Android's
// own taps, swipes and long presses (adb shell input), Back by
// KEYCODE_BACK, Back at Home leaving the app, and the keyboard, which the
// e2e has show beside the emulator's own (show_ime_with_hard_keyboard).
// Runs in the Android container, beside adb, with screens.mjs beside it.
//
//   node walk.mjs SERIAL OUT CTX [PART...]
//
// CTX is a JSON file of the den's IDs: den, general and random, a text
// channel besides; for first, the invite, the member's username and
// display name, the passwords, and the phone's name and model. PART is
// first, the page's first screens (M13.3): the local password set and the
// den joined through the page's forms, by Android's taps and keyboard, to
// the recovery codes and Home, where Manage lists the phone among the
// member's devices by its name, with its model under it; around, getting around from Home as M13.1's done-when has it;
// touch, M13.2's: a long press, a sheet's swipe down and Back, three
// lines sent by the send button with the keyboard up, and a tip's tap;
// pages, M13.3's: the settings and Manage as lists of pages that Back
// walks out of, the phone's settings without keyboard shortcuts or screen
// share, the viewer swiped through a message's photos, zoomed by a double
// tap and closed by Back, a dialog swiped down, and the system bars
// taking the page's background as its theme changes; call, M13.4's: in
// a call with desk, whose desk.mjs answers at CTX's desk, from a text
// channel and without going to the list, the call's bar and button, desk's
// share docked above the channel, full screen and back, a tap on the
// button muting and unmuting, then, with its role push to talk, held
// through Android's own touch while desk sees the member ring, with no
// long press's buzz, and let go as the notification shade comes down
// mid-press, and the call left from its bar; or screens, every member's screen with its picture and its
// audit, which fails as the phone check's does on a touch screen. around
// unless given.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { webview } from './drive.mjs';
import { CALL, LIST_END, audits, barPlaced, buttonPlaced, dockedPlaced, gettingAround, helpers, listEnds, memberScreens, pages, touching, waitFor } from './screens.mjs';

const ADB = '/opt/android/sdk/platform-tools/adb';
const APP = process.env.ANDROID_APP || 'chat.dens.Dens.dev';
const [, , serial, out, ctxFile, ...parts] = process.argv;
// PREFIX names the walk's pictures, as a second walk at another font scale
// keeps its own.
const PREFIX = process.env.WALK_PREFIX || 'phone-';
const ctx = JSON.parse(fs.readFileSync(ctxFile, 'utf8'));
const want = (p) => (parts.length ? parts : ['around']).includes(p);
const adb = (...args) => execFileSync(ADB, ['-s', serial, ...args], { encoding: 'utf8' }).trim();
fs.mkdirSync(out, { recursive: true });

let ok = true;
const report = (good, what) => {
    ok &&= !!good;
    console.log(`${good ? 'OK  ' : 'BAD '} ${what}`);
};

// front is whether the app's activity is on top.
const front = () => new RegExp(`topResumedActivity=[^\\n]*${APP.replaceAll('.', '\\.')}/`).test(adb('shell', 'dumpsys', 'activity', 'activities'));

// The WebView's place on the screen, in the device's pixels, and the
// page's pixels to the device's: what turns a spot on the page into one
// Android's input taps.
function webViewBounds() {
    const dump = adb('exec-out', 'uiautomator', 'dump', '/dev/tty');
    const m = dump.match(/class="android\.webkit\.WebView"[^>]*bounds="\[(\d+),(\d+)\]\[(\d+),(\d+)\]"/);
    if (!m) throw new Error('no WebView on the screen');
    return { left: Number(m[1]), top: Number(m[2]), right: Number(m[3]), bottom: Number(m[4]) };
}

// first sets the local password and joins the den through the page's
// forms, as a member does on a new phone: each field tapped, and typed
// into by Android's keyboard, and each button tapped, with a picture and
// an audit of each screen.
async function first(h, d, c) {
    const field = (sel, n = 0) => `[...document.querySelectorAll(${JSON.stringify(sel)})][${n}]`;
    const button = (text) => `[...document.querySelectorAll('button')].find((e) => e.textContent.trim() === ${JSON.stringify(text)})`;
    const fill = async (x, text) => {
        await d.tap(x);
        await d.type(text);
    };
    const reached = (x, what, ms = 15000) => h.wait(x, what, ms).then(() => true, () => false);
    const step = async (name, fn) => {
        try {
            await fn();
        } catch (e) {
            report(false, `${name}: ${e.message}`);
            await h.snap(`${name}-failed`).catch(() => {});
            throw e;
        }
    };
    await step('set-password', async () => {
        if (!(await reached(button('Set password'), "the local password's form", 30000))) throw new Error('no form to set the local password');
        await h.ev('document.activeElement?.blur(), true');
        await h.snap('set-password');
        await fill(field('input[type=password]', 0), c.local);
        await fill(field('input[type=password]', 1), c.local);
        await h.snap('set-password-typed');
        await d.tap(button('Set password'));
        report(await reached(button('Add a den'), 'Home'), 'the local password, set through its form, opens Home');
    });
    await step('join', async () => {
        await h.snap('home-empty');
        await d.tap(button('Add a den'));
        await h.wait(`document.querySelector('[role=dialog] input')`, 'Add a den');
        await h.snap('add-den');
        await fill(`document.querySelector('[role=dialog] input')`, c.invite);
        await d.tap(`[...document.querySelectorAll('[role=dialog] button')].find((e) => e.textContent.trim() === 'Continue')`);
        if (!(await reached(button('Join as someone new'), 'the den the invite names'))) throw new Error("the invite didn't show its den");
        await h.ev('document.activeElement?.blur(), true');
        await h.snap('add-den-choose');
        await d.tap(button('Join as someone new'));
        await h.wait(`document.querySelector('[role=dialog] input[autocomplete=username]')`, 'the form to join');
        await fill(`document.querySelector('[role=dialog] input[autocomplete=username]')`, c.member);
        await fill(field('[role=dialog] input[maxlength="32"]'), c.name);
        await fill(field('[role=dialog] input[type=password]', 0), c.password);
        await fill(field('[role=dialog] input[type=password]', 1), c.password);
        await h.ev('document.activeElement?.blur(), true');
        await h.snap('add-den-join', { scroll: `document.querySelector('[role=dialog]')`, max: 3 });
        await d.tap(`[...document.querySelectorAll('[role=dialog] button[type=submit]')].find((e) => e.textContent.trim() === 'Join')`);
        if (!(await reached(`[...document.querySelectorAll('h3')].find((e) => e.textContent.includes('Save your'))`, 'the recovery codes', 30000))) {
            throw new Error('joining showed no recovery codes');
        }
        await h.snap('recovery-codes', { scroll: `document.querySelector('main')`, max: 3 });
        const codes = await h.ev(`(() => { const ol = document.querySelector('main ol'); const r = ol?.getBoundingClientRect();
            return ol && { n: ol.children.length, columns: new Set([...ol.children].map((li) => Math.round(li.getBoundingClientRect().left))).size,
                whole: [...ol.children].every((li) => li.getClientRects().length === 1 && li.scrollWidth <= li.clientWidth), width: Math.round(r.width) }; })()`);
        report(codes?.n > 0 && codes.columns === 1 && codes.whole, `the recovery codes stand in one column, each whole: ${JSON.stringify(codes)}`);
        await d.tap(`[...document.querySelectorAll('main label')].find((e) => e.textContent.includes("I've stored"))`);
        await d.tap(`[...document.querySelectorAll('main button')].find((e) => e.textContent.trim() === 'Done')`);
        const joined = await reached(`[...document.querySelectorAll('main li')].find((e) => [...e.querySelectorAll('button')].some((b) => b.textContent.trim() === 'Open'))`, 'the den on Home');
        report(joined, "the den, joined through the page's forms, is on Home");
        await h.snap('home-joined');
    });
    await step('devices', async () => {
        await d.tap(button('Manage'));
        await d.tap(`document.querySelector('[role=dialog] [data-section=devices]')`);
        const device = await waitFor(() => h.ev(`(() => {
            const li = [...document.querySelectorAll('[role=dialog] [data-panel] li')].find((l) => l.textContent.includes('This device'));
            if (!li) return null;
            const under = [...li.querySelectorAll('span.block')].map((e) => e.textContent.trim()).filter((t) => !t.startsWith('Added'));
            return { name: li.querySelector('.font-medium')?.textContent.trim(), under };
        })()`), 'this device in Manage', 10000).catch(() => null);
        report(device?.name === c.device && device.under.length === 1 && device.under[0] === c.model,
            `the den lists this phone by its name, with its model under it: ${JSON.stringify(device)}`);
        await h.snap('manage-devices');
        await d.back();
        await d.back();
    });
}

// hex writes red, green and blue bytes as #rrggbb.
const hex = (rgb) => `#${[...rgb].map((v) => v.toString(16).padStart(2, '0')).join('')}`;

// bars checks the status and navigation bars take the page's background,
// with the theme the system's, then Dark, then Light, each picked on the
// settings' Appearance page, then the system's again (M13.3).
async function bars(h, d) {
    const background = () => h.ev(`(() => { const c = document.createElement('canvas').getContext('2d');
        c.fillStyle = getComputedStyle(document.body).backgroundColor; c.fillRect(0, 0, 1, 1);
        return [...c.getImageData(0, 0, 1, 1).data.slice(0, 3)]; })()`);
    const near = (a, b) => [1, 3, 5].every((i) => Math.abs(parseInt(a.slice(i, i + 2), 16) - parseInt(b.slice(i, i + 2), 16)) <= 3);
    const painted = async (theme) => {
        const want = hex(await background());
        const got = await waitFor(() => {
            const b = d.bars();
            return near(b.top, want) && near(b.bottom, want) && b;
        }, `the bars in ${want}`, 6000).catch(() => d.bars());
        report(near(got.top, want) && near(got.bottom, want), `with the ${theme} theme, the bars take the page's background, ${want}: ${JSON.stringify(got)}`);
    };
    const pick = async (theme) => {
        await d.tap(`document.querySelector('[role=dialog] input[name=theme][value=${theme}]').closest('label')`);
        await sleep(800);
    };
    await h.go('/');
    await h.wait(`[...document.querySelectorAll('h2')].find((e) => e.textContent.trim() === 'Your dens')`, 'Home');
    await painted("system's");
    await d.tap(`document.querySelector('a[aria-label="App settings"]')`);
    await d.tap(`document.querySelector('[role=dialog] [data-section=appearance]')`);
    for (const theme of ['dark', 'light']) {
        await pick(theme);
        await painted(theme);
    }
    await pick('system');
    await d.back();
    await d.back();
}

// callWalk walks M13.4's done-when, in a call with desk, the den's owner,
// whose desk.mjs answers at c.desk with what its page shows of the call,
// and c.name the member's name there: the member joins Lounge from the
// list, as desk shares, taps desk's LIVE there and goes to general, and
// from there on, never back to the list, finds the call's bar under its
// header, desk's share docked under the bar and the call's button above
// the composer; plays the share full screen with a tap, which the shell
// shows, and Back docks it again; mutes and unmutes with a tap on the
// button, which desk sees; sets its role to push to talk in the settings;
// holds it through Android's own touch, past a long press, while desk
// sees the member ring, and not before or after; holds it again as the
// notification shade comes down, which lets it go; and leaves from the
// bar.
async function callWalk(h, d, c) {
    const desk = async (path, method = 'GET') => (await fetch(c.desk + path, { method })).json();
    const onDesk = async () => (await desk('/call')).members.find((m) => m.name === c.name);
    const BUTTON = `document.querySelector('[data-call-button]')`;
    const inList = (name) => `[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === ${JSON.stringify(name)})`;
    const LIVE = `[...document.querySelectorAll('ul[aria-label="In the call"] button')].find((b) => b.textContent.trim() === 'LIVE')`;
    const call = () => h.ev(CALL);
    const step = async (name, fn) => {
        try {
            await fn();
        } catch (e) {
            report(false, `${name}: ${e.message}`);
            await h.snap(`${name}-failed`).catch(() => {});
        }
    };
    const said = (line) => d.shellLog().split('\n').filter((l) => l.includes(line)).length;
    await step('call-join', async () => {
        report((await desk('/share', 'POST')).share === 'clicked', 'desk shares its screen in Lounge');
        await h.go('/');
        await h.wait(`[...document.querySelectorAll('h2')].find((e) => e.textContent.trim() === 'Your dens')`, 'Home');
        await d.tap(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Open')`);
        if (!(await h.has(inList('Lounge')))) await d.tap(`[...document.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim() === 'Den')`);
        await d.tap(inList('Lounge'));
        await h.wait(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`, 'the call', 30000);
        await waitFor(onDesk, 'desk to see the member in the call', 15000);
        report(!(await h.has(`document.querySelector('button[aria-label="Share your screen"]')`)), "the WebView's call has no share button");
        await h.snap('call-list');
    });
    await step('call-docked', async () => {
        await h.wait(LIVE, "desk's share", 30000);
        await d.tap(LIVE);
        await d.tap(inList('general'));
        const at = await waitFor(async () => {
            const x = await call();
            return x.video?.w > 0 && x.video.playing && x;
        }, "desk's share docked and playing", 20000).catch(call);
        await h.snap('call-docked');
        report(barPlaced(at) && at.bar_text === 'Connected to Lounge', `from general, the call's bar shows under its header: ${JSON.stringify({ bar: at.bar, header: at.header?.bottom, text: at.bar_text })}`);
        report(buttonPlaced(at) && at.button.role === 'toggleMute' && at.button.label === 'Mute',
            `the call's button, 64 pixels at the screen's foot on the right, above the composer, toggles mute, unmuted: ${JSON.stringify({ button: at.button, composer: at.composer?.top })}`);
        report(dockedPlaced(at), `desk's share docks above the channel, under the bar, at the screen's width and the share's shape: ${JSON.stringify({ docked: at.docked, video: at.video })}`);
        const end = await h.ev(LIST_END);
        report(listEnds(end, at), `the list ends at its newest message, ${end} pixels above its foot, and nothing else scrolls: ${JSON.stringify({ row: at.rowScrolls, page: at.pageScrolls })}`);
    });
    await step('call-full', async () => {
        const [shown, hidden] = [said('page: full screen'), said('page: out of full screen')];
        await d.tap(`document.querySelector('section[data-docked] video')`);
        const full = await waitFor(async () => (await call()).full && said('page: full screen') > shown, 'full screen through the shell', 8000).catch(() => false);
        await h.snap('call-full');
        await d.back();
        const back = await waitFor(async () => {
            const x = await call();
            return !x.full && dockedPlaced(x) && said('page: out of full screen') > hidden && x;
        }, 'docked again', 8000).catch(() => false);
        report(full && back, `a tap plays the share full screen, which the shell shows (${!!full}), and Back docks it again (${!!back})`);
    });
    await step('call-mute', async () => {
        await d.tap(BUTTON);
        const muted = await waitFor(async () => (await onDesk())?.muted, 'desk to see the member muted', 8000).catch(() => false);
        const at = await call();
        await h.snap('call-muted');
        report(muted && at.button.label === 'Unmute' && at.button.struck, `a tap on the call's button mutes, struck through, and desk sees it: ${JSON.stringify(at.button)}`);
        await d.tap(BUTTON);
        const unmuted = await waitFor(async () => (await onDesk())?.muted === false, 'desk to see the member unmuted', 8000).catch(() => false);
        report(unmuted && (await call()).button.label === 'Mute', 'another unmutes, which desk sees');
    });
    await step('call-role', async () => {
        // The settings open by script, from the list that stays off screen.
        await h.click(`document.querySelector('[data-pane=list] button[aria-label="App settings"]')`, 'the settings');
        await d.tap(`document.querySelector('[role=dialog] [data-section=voice]')`);
        const group = `document.querySelector('[role=radiogroup][aria-label="The call\\'s button"]')`;
        const roles = await h.ev(`[...(${group}?.querySelectorAll('label') || [])].map((l) => l.textContent.trim() + (l.querySelector('input').checked ? ' (chosen)' : ''))`);
        const sending = await h.has(`document.querySelector('[role=radiogroup][aria-label="Sending your voice"]')`);
        report(roles.join(', ') === 'Toggle mute (chosen), Push to mute, Push to talk' && !sending, `the button's role takes Sending your voice's place: ${roles.join(', ')}`);
        await d.tap(`[...${group}.querySelectorAll('label')].find((l) => l.textContent.trim() === 'Push to talk')`);
        await h.snap('call-role');
        await d.back();
        await d.back();
        const ptt = await waitFor(async () => (await call()).button?.role === 'ptt' && !(await h.has(`document.querySelector('[role=dialog]')`)), 'push to talk', 8000).catch(() => false);
        report(ptt, 'the call\'s button now pushes to talk, and Back left the settings for general');
    });
    await step('call-hold', async () => {
        const before = await waitFor(async () => (await onDesk())?.rings === false && (await onDesk()), 'the member quiet on desk', 8000).catch(() => null);
        const buzzed = d.buzzes();
        await d.down(BUTTON);
        const rang = await waitFor(async () => (await onDesk())?.rings, 'desk to see the member ring', 8000).catch(() => false);
        await sleep(2000);
        const still = (await onDesk())?.rings;
        const at = await call();
        await h.snap('call-held');
        await d.up();
        const quiet = await waitFor(async () => (await onDesk())?.rings === false, 'desk to see the member let go', 8000).catch(() => false);
        report(!!before && rang && still && quiet && at.button.pressed === 'true' && at.button.talking,
            `held through Android's own touch, past a long press, the call's button talks, in the ring's color, and desk sees the member ring only while it's held: ${JSON.stringify({ before: before?.rings, rang, still, quiet, button: at.button })}`);
        const buzzes = d.buzzes() - buzzed;
        report(buzzes === 0, `and the phone buzzes for no long press as it's held, the browser making no gesture of it: ${buzzes} buzzes`);
    });
    await step('call-shade', async () => {
        await d.down(BUTTON);
        const rang = await waitFor(async () => (await onDesk())?.rings, 'desk to see the member ring', 8000).catch(() => false);
        d.shade(true);
        const quiet = await waitFor(async () => (await onDesk())?.rings === false, 'desk to see the member let go', 8000).catch(() => false);
        const at = await call();
        await h.snap('call-shade');
        d.shade(false);
        await sleep(800);
        await d.up();
        report(rang && quiet && at.button.pressed === 'false', `held as the notification shade comes down, the call's button lets go, which desk sees: ${JSON.stringify({ rang, quiet, button: at.button })}`);
    });
    await step('call-leave', async () => {
        await d.tap(`document.querySelector('[data-call-bar] button[aria-label="Leave the call"]')`);
        const ended = await waitFor(async () => !(await h.has(`document.querySelector('[data-call-bar]')`)) && !(await h.has(BUTTON)), 'the call to end', 10000)
            .catch(() => false);
        const gone = await waitFor(async () => !(await onDesk()), "desk to see the member go", 10000).catch(() => false);
        await h.snap('call-left');
        report(ended && gone, 'the bar\'s leave ends the call, its bar and button go, and desk sees the member leave');
    });
}

const b = await webview(serial);
// The page's own screenshots go through the DevTools protocol; Android's,
// of the whole screen, show what the member sees.
b.shot = async (file) => fs.writeFileSync(file, execFileSync(ADB, ['-s', serial, 'exec-out', 'screencap', '-p'], { maxBuffer: 64 << 20 }));
try {
    const origin = new URL(b.url).origin;
    const h = helpers(b, origin, out, PREFIX);
    const view = webViewBounds();
    const dpr = await b.eval('devicePixelRatio');
    const width = await b.eval('document.documentElement.clientWidth');
    report(width === 390, `the page is ${width} CSS pixels wide, at ${dpr} device pixels each`);
    const toScreen = (x, y) => [Math.round(view.left + x * dpr), Math.round(view.top + y * dpr)];
    // A swipe starts a little in from the screen's edges, which Android's
    // gesture navigation takes for Back, across the panes' headers.
    let finger = [0, 0];
    const d = {
        name: 'the WebView, by Android\'s input',
        fromHome: true,
        async swipe(dir) {
            const [x1, x2] = dir === 'right' ? [70, 330] : [330, 70];
            const [ax, ay] = toScreen(x1, 24);
            const [bx] = toScreen(x2, 24);
            adb('shell', 'input', 'swipe', String(ax), String(ay), String(bx), String(ay), '250');
            await sleep(900);
        },
        async tap(x) {
            // Only what's off screen scrolls into view: scrolling what's
            // shown can move the row of panes.
            const [tx, ty] = toScreen(...(await spot(x)));
            adb('shell', 'input', 'tap', String(tx), String(ty));
            await sleep(900);
        },
        async back() {
            adb('shell', 'input', 'keyevent', 'KEYCODE_BACK');
            await sleep(900);
        },
        // Back at Home, the history's root, leaves the app.
        async out() {
            adb('shell', 'input', 'keyevent', 'KEYCODE_BACK');
            const gone = await waitFor(() => !front(), 'the app to leave', 5000).catch(() => false);
            report(gone, 'the WebView, by Android\'s input: Back at Home leaves the app');
        },
        touch: true,
        // A long press is a swipe that stays where it starts.
        async press(x) {
            const [tx, ty] = toScreen(...(await spot(x)));
            adb('shell', 'input', 'swipe', String(tx), String(ty), String(tx), String(ty), '800');
            await sleep(900);
        },
        async drag(from, to) {
            const [ax, ay] = toScreen(...from);
            const [bx, by] = toScreen(...to);
            adb('shell', 'input', 'swipe', String(ax), String(ay), String(bx), String(by), '300');
            await sleep(900);
        },
        async type(text) {
            adb('shell', 'input', 'text', text.replaceAll(' ', '%s'));
            await sleep(300);
        },
        async enter() {
            adb('shell', 'input', 'keyevent', 'KEYCODE_ENTER');
            await sleep(300);
        },
        // A double tap is two taps from one of adb's shells, some 30 ms
        // apart, where two shells' round trips could leave more between
        // them than a double tap allows.
        async double(x) {
            const [tx, ty] = toScreen(...(await spot(x)));
            adb('shell', `cmd input tap ${tx} ${ty}; cmd input tap ${tx} ${ty}`);
            await sleep(900);
        },
        // A finger put down and held through Android's own touch, at the
        // middle of what x names, then lifted where it went down (M13.4).
        async down(x) {
            finger = toScreen(...(await spot(x)));
            adb('shell', 'input', 'motionevent', 'DOWN', String(finger[0]), String(finger[1]));
            await sleep(300);
        },
        async up() {
            adb('shell', 'input', 'motionevent', 'UP', String(finger[0]), String(finger[1]));
            await sleep(300);
        },
        // Android's notification shade, pulled down or put back.
        shade(on) {
            adb('shell', 'cmd', 'statusbar', on ? 'expand-notifications' : 'collapse');
        },
        // The shell's log, which says what the app's shell did.
        shellLog: () => adb('shell', 'run-as', APP, 'cat', 'files/shell/shell.log'),
        // buzzes counts the long presses Android has buzzed for the app, as
        // its vibrator's recent vibrations list them.
        buzzes: () => adb('shell', 'dumpsys', 'vibrator_manager').split('\n')
            .filter((l) => l.includes(APP) && l.includes('performHapticFeedback(constant=0)')).length,
        phone: true,
        // bars is the color Android draws the status bar and the navigation
        // bar in, a pixel of each from its own picture of the screen: the
        // status bar's middle, between the clock and the icons, and the
        // navigation bar's corner, beside the gesture handle.
        bars() {
            const raw = execFileSync(ADB, ['-s', serial, 'exec-out', 'screencap'], { maxBuffer: 64 << 20 });
            const w = raw.readUInt32LE(0);
            const ht = raw.readUInt32LE(4);
            const head = raw.length - w * ht * 4;
            const px = (x, y) => hex(raw.subarray(head + (y * w + x) * 4, head + (y * w + x) * 4 + 3));
            return { top: px(Math.round(w / 2), 4), bottom: px(4, ht - 4) };
        },
        // The keyboard is up once the shell has made the WebView shorter,
        // to the room the keyboard leaves; then the composer shows whole
        // above it, and the newest message, holding text if given, above
        // that.
        async keyboard(text) {
            const up = await waitFor(async () => (await b.eval('innerHeight')) < tall - 100, 'the keyboard', 10000).then(() => true, () => false);
            await sleep(800);
            const at = await b.eval(`(() => {
                const c = document.querySelector('textarea[placeholder^="Message "]').getBoundingClientRect();
                const list = document.querySelector('[data-scroller]').getBoundingClientRect();
                const row = [...document.querySelectorAll('[data-scroller] [data-id]')].at(-1);
                const m = row.getBoundingClientRect();
                return { height: innerHeight, composer: [Math.round(c.top), Math.round(c.bottom)], newest: [Math.round(m.top), Math.round(m.bottom)],
                    list: [Math.round(list.top), Math.round(list.bottom)], text: row.innerText };
            })()`);
            const sight = at.composer[0] >= 0 && at.composer[1] <= at.height && at.newest[1] <= at.list[1] + 1 && at.newest[1] > at.list[0];
            const holds = !text || at.text.includes(text);
            return { ok: up && sight && holds, said: `the page ${at.height} of ${tall} pixels tall, the composer at ${at.composer}, the newest message at ${at.newest} in the list at ${at.list}${text ? `, holding ${JSON.stringify(text)} (${holds})` : ''}` };
        },
    };
    // spot is where on the page the expression x names, at its middle, as
    // tap finds it.
    const spot = async (x) => {
        if (Array.isArray(x)) return x;
        const r = await h.ev(`(() => { const e = ${x}; if (!e) return null; let r = e.getBoundingClientRect();
            if (r.top < 0 || r.bottom > innerHeight) { e.scrollIntoView({ block: 'nearest', inline: 'nearest' }); r = e.getBoundingClientRect(); }
            return [r.left + r.width / 2, r.top + r.height / 2]; })()`);
        if (!r) throw new Error(`nothing there: ${x}`);
        return r;
    };
    const tall = await b.eval('innerHeight');
    if (want('around')) {
        await h.wait(`[...document.querySelectorAll('h2')].find((e) => e.textContent.trim() === 'Your dens')`, 'Home');
        await gettingAround(h, ctx, d, report).catch((e) => report(false, e.message));
    }
    if (want('touch')) {
        await h.wait(`[...document.querySelectorAll('h2')].find((e) => e.textContent.trim() === 'Your dens')`, 'Home');
        await touching(h, ctx, d, report).catch((e) => report(false, e.message));
    }
    if (want('first')) {
        await first(h, d, ctx);
    }
    if (want('pages')) {
        await pages(h, ctx, d, report).catch((e) => report(false, e.message));
        await bars(h, d).catch((e) => report(false, e.message));
    }
    if (want('call')) {
        try {
            await callWalk(h, d, ctx);
        } finally {
            await fetch(`${ctx.desk}/quit`, { method: 'POST' }).catch(() => {});
        }
    }
    if (want('screens')) {
        // The member's own message, newest, whose menu holds everything.
        const sent = await b.api('POST', `/api/dens/${ctx.den}/channels/${ctx.general}/messages`,
            JSON.stringify({ nonce: Buffer.from(crypto.getRandomValues(new Uint8Array(16))).toString('base64url'), text: 'the walk was here', attachments: [] }));
        report(sent.status === 200, `the member says something to walk from: ${sent.status}`);
        await memberScreens(h, ctx);
    }
    audits('the WebView', h.log, report, true);
    fs.writeFileSync(`${out}/audits-${parts.join('-') || 'around'}.json`, JSON.stringify(h.log, null, 1));
} finally {
    b.close();
}
process.exit(ok ? 0 : 1);
