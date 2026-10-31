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
// channel besides. PART is around, getting around from Home as M13.1's
// done-when has it; touch, M13.2's: a long press, a sheet's swipe down
// and Back, three lines sent by the send button with the keyboard up, and
// a tip's tap; or screens, every member's screen with its picture and its
// audit, which fails as the phone check's does on a touch screen. around
// unless given.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { webview } from './drive.mjs';
import { audits, gettingAround, helpers, memberScreens, touching, waitFor } from './screens.mjs';

const ADB = '/opt/android/sdk/platform-tools/adb';
const APP = process.env.ANDROID_APP || 'chat.dens.Dens.dev';
const [, , serial, out, ctxFile, ...parts] = process.argv;
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

const b = await webview(serial);
// The page's own screenshots go through the DevTools protocol; Android's,
// of the whole screen, show what the member sees.
b.shot = async (file) => fs.writeFileSync(file, execFileSync(ADB, ['-s', serial, 'exec-out', 'screencap', '-p'], { maxBuffer: 64 << 20 }));
try {
    const origin = new URL(b.url).origin;
    const h = helpers(b, origin, out, 'phone-');
    const view = webViewBounds();
    const dpr = await b.eval('devicePixelRatio');
    const width = await b.eval('document.documentElement.clientWidth');
    report(width === 390, `the page is ${width} CSS pixels wide, at ${dpr} device pixels each`);
    const toScreen = (x, y) => [Math.round(view.left + x * dpr), Math.round(view.top + y * dpr)];
    // A swipe starts a little in from the screen's edges, which Android's
    // gesture navigation takes for Back, across the panes' headers.
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
