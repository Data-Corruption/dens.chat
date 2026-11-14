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
// mid-press, desk leaving and joining again, whose sounds the page plays
// through the call's context (M14.2), and the call left from its bar;
// reactions, M15.3's: desk's reaction on the member's message, whose
// phone_msg CTX names, shown as a chip, and the member's on desk's,
// desk_msg, from the long press's quick row, which stays for desk to see,
// with the chip's tip by a long press; polls, M15.4's: desk's poll, whose
// desk_poll CTX names, voted on by Android's own touch, its totals and
// voters showing; relay, M20.3's: Keybind relay turned on in the settings,
// which has Android ask to let Dens reach its network, allowed on its
// dialog, and the member's install beside desk's listed as sharing its
// sound, whose Listen plays it across the link, as media, the page held as
// shown and Wi-Fi kept awake, moving into a call's sound as the phone joins
// one, CTX's den's Lounge, and back to media as it leaves, until Stop lets
// them go; or screens, every
// member's screen with its picture and its audit, which fails as the phone
// check's does on a touch screen. around unless given.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { webview } from './drive.mjs';
import {
    CALL, LIST_END, audits, barPlaced, buttonPlaced, dockedPlaced, gettingAround, helpers, listEnds, memberScreens, pages, quickReacting, touching,
    waitFor,
} from './screens.mjs';

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

// OWN_THEME is a theme of a member's own as its code: Plum, dark, on
// #2b1d3a.
const OWN_THEME = 'dens-theme:1.d.r.2b1d3aeae0f5c084fc7dd3fcfbbf2409090b00bafe00d390fcb700ff627d.506c756d';

// hex writes red, green and blue bytes as #rrggbb.
const hex = (rgb) => `#${[...rgb].map((v) => v.toString(16).padStart(2, '0')).join('')}`;

// bars checks the status and navigation bars take the page's background,
// with the theme the system's, then Dark, then Light, each picked on the
// settings' Appearance page, then a theme of the member's own, pasted as
// its code (M19.4), then the system's again (M13.3).
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
    // A theme of the member's own (M19.4), pasted as its code: the WebView
    // wears it, and the bars its background.
    await h.click(h.byText('[data-own-themes] button', 'Paste a code'));
    await h.ev(`(() => { const el = document.querySelector('[data-own-themes] form input');
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, ${JSON.stringify(OWN_THEME)});
        el.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
    await h.click(h.byText('[data-own-themes] button', 'Add'));
    await waitFor(() => h.has(`document.querySelector('[role=radiogroup][aria-label="Your themes"] input:checked')`), 'the pasted theme chosen');
    const wears = hex(await background());
    report(wears === '#2b1d3a', `the page wears a theme pasted as its code, its background ${wears}`);
    await painted("member's own");
    await h.click(h.byText('[data-own-themes] button', 'Delete'));
    await pick('system');
    await d.back();
    await d.back();
}

// styledName checks the WebView draws a name in the font its member
// picked (M19.5): desk's, in general, in Pacifico, once the WebView has
// fetched the face.
async function styledName(h, c) {
    await h.go(`/den/${c.den}/${c.general}`);
    const NAME = `[...document.querySelectorAll('[data-scroller] [data-name]')].find((n) => n.textContent === 'Desk')`;
    await h.wait(NAME, "desk's name in general");
    const drawn = () => h.ev(`(() => { const n = ${NAME}; const f = [...document.fonts].find((x) => x.family.includes('Dens name pacifico'));
        return JSON.stringify({ font: getComputedStyle(n).fontFamily.split(',')[0].replace(/"/g, ''), color: getComputedStyle(n).color, face: f ? f.status : 'none' }); })()`).then(JSON.parse);
    const got = await waitFor(async () => { const g = await drawn(); return g.font === 'Dens name pacifico' && g.face === 'loaded' && g; }, "desk's name in its font", 15000)
        .catch(() => drawn());
    report(got.font === 'Dens name pacifico' && got.face === 'loaded' && got.color !== 'rgba(0, 0, 0, 0)', `the WebView draws desk's name in the font it picked, the face fetched: ${JSON.stringify(got)}`);
    await h.snap('styled-name');
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
// notification shade comes down, which lets it go; hears desk leave and
// join again through the call's context, as Android plays the call's
// voices, which a hook in the page counts (M14.2); hears desk, muted, play
// a sound of the den's soundboard, which the page plays from the call's
// track through the call's context, desk's ring lit while it plays, and
// plays one from the bar's soundboard, its sheet, which desk receives
// (M14.3); and leaves from the bar.
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
    await step('call-sound', async () => {
        // What the page plays, by what each sound lasts: the call's context
        // plays a decoded sound, and an element a file. Each lasts what this
        // WebView decodes it to at the call's rate, the device's, 44.1 or 48
        // kHz, a few milliseconds past what desktop Chromium does: as 0.338 s
        // for Left and 0.295 for Joined.
        const lasts = await h.ev(`(async () => {
            const sounds = JSON.parse(document.getElementById('app').dataset.sounds);
            const out = { left: [], joined: [] };
            for (const name of Object.keys(out)) {
                const data = await (await fetch(sounds[name])).arrayBuffer();
                for (const rate of [44100, 48000]) {
                    out[name].push(Math.round((await new OfflineAudioContext(1, 1, rate).decodeAudioData(data.slice(0))).duration * 1000));
                }
            }
            return out;
        })()`);
        await h.ev(`(() => {
            if (window.__played) return true;
            window.__played = { context: [], element: [] };
            const start = AudioBufferSourceNode.prototype.start;
            AudioBufferSourceNode.prototype.start = function (...a) {
                window.__played.context.push(Math.round((this.buffer?.duration || 0) * 1000));
                return start.apply(this, a);
            };
            const play = HTMLMediaElement.prototype.play;
            HTMLMediaElement.prototype.play = function () {
                if (this.src.includes('/sounds/')) window.__played.element.push(new URL(this.src).pathname);
                return play.call(this);
            };
            return true;
        })()`);
        const before = await h.ev('window.__played.context.length');
        const rejoined = (await desk('/rejoin', 'POST')).rejoined;
        const near = (ms, want) => want.some((w) => Math.abs(ms - w) <= 1);
        const played = await waitFor(async () => {
            const p = await h.ev('window.__played');
            return p.context.length >= before + 2 && p;
        }, "desk's sounds", 15000).catch(() => h.ev('window.__played'));
        const [left, joined] = played.context.slice(before);
        report(rejoined && near(left, lasts.left) && near(joined, lasts.joined) && played.element.length === 0,
            `desk leaving and joining again play Left and Joined through the call's context, and no element plays them: ${JSON.stringify({ played, lasts })}`);
    });
    await step('call-soundboard', async () => {
        report((await desk('/mute', 'POST')).muted, 'desk mutes, so its ring lights for its sound alone');
        const deskRings = `[...document.querySelectorAll('ul[aria-label="In the call"] li')].some((li) => li.textContent.includes('Desk') && li.textContent.includes('speaking'))`;
        await waitFor(async () => !(await h.ev(deskRings)), "desk's ring out", 10000);
        let before = await board(h.b);
        report((await desk('/sound?name=Ping', 'POST')).played === 'Ping', "desk plays Ping from its soundboard");
        const lit = await waitFor(() => h.ev(deskRings), "desk's ring for its sound", 8000).catch(() => false);
        const got = await waitFor(async () => {
            const now = await board(h.b);
            return now.packets >= before.packets + 25 && now;
        }, 'the soundboard on the phone', 10000).catch(() => board(h.b));
        report(lit && got.packets >= before.packets + 25 && got.context, `the phone's page plays desk's sound from the call's track, through the call's context, and lights desk's ring: ${JSON.stringify({ before, got, lit })}`);
        await waitFor(async () => !(await h.ev(deskRings)), "desk's ring out after its sound", 10000);
        await d.tap(`document.querySelector('[data-call-bar] [data-soundboard-button=bar]')`);
        const tile = `[...document.querySelectorAll('[data-sheet] [data-sound-tile]')].find((t) => t.querySelector('span').textContent === 'Ping')`;
        await h.wait(tile, "the bar's soundboard sheet");
        await h.snap('call-soundboard');
        before = await desk('/board');
        await d.tap(tile);
        const heard = await waitFor(async () => {
            const now = await desk('/board');
            return now.packets >= before.packets + 25 && now;
        }, 'desk receiving the phone\'s sound', 10000).catch(() => desk('/board'));
        report(heard.packets >= before.packets + 25, `the phone plays Ping from the bar's soundboard, and desk receives it: ${JSON.stringify({ before, heard })}`);
        if (await h.has(`document.querySelector('[data-sheet]')`)) await d.back();
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

// board is what the page's call receives on the soundboard (M14.3), found
// in its heap, as call.mjs finds its peer: the packets, and whether the
// call's context plays it, its element muted and playing.
async function board(b) {
    const group = 'board';
    const over = async (proto, fn) => {
        const { result } = await b.cdp('Runtime.evaluate', { expression: `${proto}.prototype`, objectGroup: group });
        const { objects } = await b.cdp('Runtime.queryObjects', { prototypeObjectId: result.objectId, objectGroup: group });
        const r = await b.cdp('Runtime.callFunctionOn', { objectId: objects.objectId, functionDeclaration: fn, awaitPromise: true, returnByValue: true });
        if (r.exceptionDetails) throw new Error(`${proto}: ${r.exceptionDetails.exception?.description || r.exceptionDetails.text}`);
        return r.result.value;
    };
    try {
        const packets = await over('RTCPeerConnection', `async function () {
            const pc = this.find((p) => p.connectionState !== 'closed' && p.remoteDescription);
            if (!pc) return 0;
            const mid = pc.remoteDescription.sdp.split('\\r\\nm=').slice(1).map((s) => [/a=mid:(\\S+)/.exec(s)?.[1], s.includes('a=msid:soundboard ')]).find(([, on]) => on)?.[0];
            const t = pc.getTransceivers().find((x) => x.mid === mid);
            if (!t) return 0;
            return [...(await pc.getStats(t.receiver.track)).values()].find((x) => x.type === 'inbound-rtp')?.packetsReceived || 0;
        }`);
        const context = await over('HTMLMediaElement', `function () {
            return this.filter((e) => { try { return e.srcObject?.id === 'soundboard'; } catch { return false; } }).some((e) => e.muted && !e.paused);
        }`);
        return { packets, context };
    } finally {
        await b.cdp('Runtime.releaseObjectGroup', { objectGroup: group }).catch(() => {});
    }
}

const b = await webview(serial);
// reactionsWalk checks desk's reaction on the member's message shows on the
// phone's page as a chip, not the member's, and has the member react to
// desk's from the long press's quick row, by Android's own touch, leaving
// it for desk's page (M15.3).
async function reactionsWalk(h, d, c) {
    await h.go(`/den/${c.den}/${c.general}`);
    const chip = `document.querySelector('[data-scroller] [data-id="${c.phone_msg}"] [data-reaction="u:🎉"]')`;
    const shown = await waitFor(() => h.ev(`(() => { const ch = ${chip}; return ch ? { count: Number(ch.dataset.count), mine: ch.getAttribute('aria-pressed') === 'true' } : null; })()`),
        "desk's 🎉 on the member's message", 15000).catch(() => null);
    report(shown?.count === 1 && !shown.mine, `the WebView, by Android's input: desk's 🎉 shows on the member's message, not marked theirs: ${JSON.stringify(shown)}`);
    await h.snap('reactions-desk');
    const made = await quickReacting(h, c, d, report, { at: c.desk_msg, keep: true });
    if (made) fs.writeFileSync(`${out}/reacted.json`, JSON.stringify(made));
}

// pollsWalk has the member vote on desk's poll by Android's own touch: a
// tap on a choice's row and one on Vote, after which the totals show, with
// their vote marked and desk's beside it, and a tap on its count says who
// (M15.4).
async function pollsWalk(h, d, c) {
    await h.go(`/den/${c.den}/${c.general}`);
    const poll = `document.querySelector('[data-scroller] [data-poll="${c.desk_poll}"]')`;
    const shown = await waitFor(() => h.has(`${poll}?.querySelector('[data-choice="1"] label')`), "desk's poll", 15000).then(() => true, () => false);
    report(shown, "the WebView, by Android's input: desk's poll shows, open to the member's vote");
    await h.snap('poll-desk');
    if (!shown) return;
    await d.tap(`${poll}.querySelector('[data-choice="1"] label')`);
    await d.tap(`[...${poll}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Vote')`);
    const voted = await waitFor(() => h.ev(`(() => { const p = ${poll}; const c = p.querySelector('[data-choice="1"]');
        return c.hasAttribute('data-mine') && c.dataset.count === '1' && p.querySelector('[data-choice="0"]').dataset.count === '1'; })()`),
    'the vote', 15000).then(() => true, () => false);
    report(voted, "the WebView, by Android's input: a tap on a choice and one on Vote vote, and the totals show, the member's marked, desk's beside it");
    await h.snap('poll-voted');
    const count = `${poll}.querySelector('[data-choice="1"] [data-voters]')`;
    if (voted) {
        await d.tap(count);
        const tip = await waitFor(() => h.ev(`(() => { const t = ${count}?.parentElement.querySelector('[role=tooltip]'); return t && !t.hidden ? t.textContent : ''; })()`),
            "the count's tip", 5000).catch(() => '');
        report(tip === 'You', `the WebView, by Android's input: a tap on the count says who voted for it: ${JSON.stringify(tip)}`);
        await h.snap('poll-tip');
    }
}

// relayWalk is Relay on the phone (M20.3), from Home by Android's own taps.
async function relayWalk(h, d, c) {
    const SECTION = `[...document.querySelectorAll('[role=dialog][aria-label="App settings"] [data-section]')].find((b) => b.textContent.trim() === 'Relay')`;
    const KEYS = `document.getElementById('relay-keys')`;
    // The member's one other device, the install beside desk's.
    const ROW = `document.querySelector('[data-device]')`;
    const LISTEN = `document.querySelector('[data-device] [data-listen]')`;
    const lines = (line) => d.shellLog().split('\n').filter((l) => l.includes(line)).length;
    const said = (line) => lines(line) > 0;
    const uid = adb('shell', 'pm', 'list', 'packages', '-U', APP).match(/uid:(\d+)/)?.[1];
    const types = () => adb('shell', 'dumpsys', 'activity', 'services', APP).match(/types=(0x[0-9a-f]+)/)?.[1];
    // The app's players as Android has them: their use and state.
    const players = () => adb('shell', 'dumpsys', 'audio').split('\n').filter((l) => l.includes(`u/pid:${uid}/`) && l.includes('usage='))
        .map((l) => `${l.match(/usage=(\w+)/)?.[1]} ${l.match(/state:(\w+)/)?.[1]}`);
    // Wi-Fi's locks held now, which its dump lists apart from its log.
    const wifiLock = () => {
        const dump = adb('shell', 'dumpsys', 'wifi');
        const from = dump.indexOf('Locks held:', dump.indexOf('Dump of WifiLockManager'));
        return from >= 0 && dump.slice(from).split(/\r?\n\s*\r?\n/)[0].includes('dens:relay');
    };
    await h.go('/');
    await h.wait(`[...document.querySelectorAll('h2')].find((e) => e.textContent.trim() === 'Your dens')`, 'Home');
    await d.tap(`document.querySelector('a[aria-label="App settings"]')`);
    await h.wait(SECTION, "the settings' Relay");
    await d.tap(SECTION);
    await h.wait(KEYS, 'Keybind relay');
    report(!(await h.ev(`${KEYS}.checked`)) && (await h.ev(`[...document.querySelectorAll('[data-panel]')].some((p) => p.textContent.includes("Relay the call's button"))`)),
        "the WebView, by Android's input: the phone's Relay has Keybind relay off, the call's button its keys");
    await h.snap('relay');
    await d.tap(KEYS);
    // Android asks to let Dens reach the network, on a dialog of its own.
    const allow = await waitFor(() => adb('exec-out', 'uiautomator', 'dump', '/dev/tty')
        .match(/resource-id="com\.android\.permissioncontroller:id\/permission_allow_(?:foreground_only_)?button"[^>]*bounds="\[(\d+),(\d+)\]\[(\d+),(\d+)\]"/),
    "Android's dialog", 15000).catch(() => null);
    if (allow) {
        const [x1, y1, x2, y2] = allow.slice(1).map(Number);
        adb('shell', 'input', 'tap', String((x1 + x2) >> 1), String((y1 + y2) >> 1));
    }
    const allowed = await waitFor(() => said('page: Android lets the app reach its network'), 'the shell allowed the network', 10000).catch(() => false);
    report(said('page: asking Android for ACCESS_LOCAL_NETWORK') && !!allow && allowed,
        `the WebView, by Android's input: Keybind relay on has Android ask to let Dens reach the network, which the member allows (${!!allow}, ${allowed})`);
    report(await h.ev(`${KEYS}.checked && !document.querySelector('[data-relay-refused]')`), 'Keybind relay stays on, with nothing refused');

    const row = await waitFor(async () => {
        const t = await h.ev(`${ROW}?.textContent || ''`);
        return t.includes('Sharing its sound') && t;
    }, "the member's install beside desk's", 30000).catch(() => '');
    report(!!row && (await h.has(LISTEN)), `the member's install beside desk's shows among their devices, sharing its sound, with Listen: ${JSON.stringify(row)}`);
    // Each of the player's nodes, by its context: the latency it was opened
    // for, the call's context none of its own, and whether it's closed. The
    // page makes both from the globals, so they're counted from here on.
    await h.ev(`(() => {
        if (window.__relayContexts) return true;
        window.__relayContexts = [];
        const Context = window.AudioContext;
        window.AudioContext = class extends Context {
            constructor(o) {
                super(o);
                this.__hint = o?.latencyHint || 'none';
            }
        };
        const Node = window.AudioWorkletNode;
        window.AudioWorkletNode = class extends Node {
            constructor(ctx, name, o) {
                super(ctx, name, o);
                if (name === 'relay') window.__relayContexts.push(ctx);
            }
        };
        return true;
    })()`);
    const contexts = () => h.ev(`window.__relayContexts.map((c) => c.__hint + ' ' + c.state)`);
    await d.tap(LISTEN);
    const playing = await waitFor(() => h.ev(`!!document.querySelector('[data-relay-bar] [data-relay="playing"]')`), 'the sound playing', 45000).catch(() => false);
    // The list asks again every few seconds.
    const linked = await waitFor(async () => {
        const t = await h.ev(`${ROW}?.textContent || ''`);
        return t.includes('Linked') && t;
    }, 'the install linked', 10000).catch(() => h.ev(`${ROW}?.textContent || ''`));
    const held = said('page: held as shown, for the sound it plays');
    const asMedia = said("service: the page plays another device's sound, as media, Wi-Fi kept awake");
    const type = types();
    const media = await waitFor(() => players().some((p) => p === 'USAGE_MEDIA started'), 'the player as media', 10000).catch(() => false);
    const lock = wifiLock();
    await h.snap('relay-listening');
    report(playing === true && linked.includes('Linked'), `Listen plays its sound across the link from the emulator: ${JSON.stringify({ playing, row: linked })}`);
    report(asMedia && held && type === '0x40000002' && media === true && lock,
        `as media: the service takes media playback's type (${type}), Android plays the page's player as media (${players().join(', ')}), the page held as shown (${held}), Wi-Fi kept awake (${lock})`);

    // A call joined on the phone takes the player into the call's context,
    // which Android plays as a call's on a phone with an echo canceller, as
    // the emulator isn't; and its end gives it a context of its own again,
    // opened for playback, played as media.
    const inList = (name) => `[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === ${JSON.stringify(name)})`;
    const PLAYING = `!!document.querySelector('[data-relay-bar] [data-relay="playing"]')`;
    const started = (use) => players().includes(`${use} started`);
    await d.back();
    await d.back();
    await h.wait(`[...document.querySelectorAll('h2')].find((e) => e.textContent.trim() === 'Your dens')`, 'Home');
    await d.tap(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Open')`);
    if (!(await h.has(inList('Lounge')))) await d.tap(`[...document.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim() === 'Den')`);
    await d.tap(inList('Lounge'));
    await h.wait(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`, 'the call', 30000);
    const moved = (x) => x.join() === 'playback closed,none running';
    const inCall = await waitFor(async () => moved(await contexts()) && (await h.ev(PLAYING)), "the player in the call's context", 20000).catch(() => false);
    report(inCall === true, `in a call on the phone, the player plays in the call's context, its own closed: ${JSON.stringify(await contexts())}; Android has ${players().join(', ')}`);
    await h.snap('relay-call');
    await d.tap(`document.querySelector('button[aria-label="Leave the call"]')`);
    const backed = (x) => x.join() === 'playback closed,none closed,playback running';
    const back = await waitFor(async () => backed(await contexts()) && (await h.ev(PLAYING)) && started('USAGE_MEDIA'), 'the player back in its own', 20000)
        .catch(() => false);
    report(back === true, `the call over, it plays in a context of its own again, as media: ${JSON.stringify(await contexts())}; Android has ${players().join(', ')}`);

    const unheld = lines('page: no longer held');
    await d.tap(`document.querySelector('[data-relay-bar] [data-relay="playing"] button')`);
    const stopped = await waitFor(async () => !(await h.has(`document.querySelector('[data-relay-bar] [data-relay="playing"]')`))
        && said("service: the page stopped playing another device's sound"), 'the sound stopped', 15000).catch(() => false);
    // The microphone's type, and the hold, outlast the call by ten seconds.
    const free = await waitFor(() => (parseInt(types(), 16) & 0x2) === 0 && !wifiLock() && lines('page: no longer held') > unheld, 'all of it let go', 15000)
        .catch(() => false);
    report(stopped === true && free === true, `the bar's Stop lets them go: the service's types ${types()}, without media playback's, Wi-Fi's lock ${wifiLock() ? 'held' : 'let go'}`);
}

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
        await styledName(h, ctx).catch((e) => report(false, e.message));
    }
    if (want('reactions')) {
        await reactionsWalk(h, d, ctx).catch((e) => report(false, e.message));
    }
    if (want('polls')) {
        await pollsWalk(h, d, ctx).catch((e) => report(false, e.message));
    }
    if (want('relay')) {
        await relayWalk(h, d, ctx).catch((e) => report(false, e.message));
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
