// The Android e2e's phone walk (M13): the page in the app's WebView at 390
// CSS pixels, through drive.mjs, as the phone check walks the browsers
// (scripts/test/browser/screens.mjs), with what only a phone has: Android's
// own taps and swipes (adb shell input), Back by KEYCODE_BACK, and Back at
// Home leaving the app. Runs in the Android container, beside adb, with
// screens.mjs beside it.
//
//   node walk.mjs SERIAL OUT CTX [PART...]
//
// CTX is a JSON file of the den's IDs: den, general and random, a text
// channel besides. PART is around, getting around from Home as M13.1's
// done-when has it, or screens, every member's screen with its picture and
// audit; around unless given.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { webview } from './drive.mjs';
import { gettingAround, helpers, memberScreens, waitFor } from './screens.mjs';

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
            const r = Array.isArray(x) ? x : await h.ev(`(() => { const e = ${x}; if (!e) return null; let r = e.getBoundingClientRect();
                if (r.top < 0 || r.bottom > innerHeight) { e.scrollIntoView({ block: 'nearest', inline: 'nearest' }); r = e.getBoundingClientRect(); }
                return [r.left + r.width / 2, r.top + r.height / 2]; })()`);
            if (!r) throw new Error(`nothing to tap: ${x}`);
            const [tx, ty] = toScreen(r[0], r[1]);
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
    };
    if (want('around')) {
        await h.wait(`[...document.querySelectorAll('h2')].find((e) => e.textContent.trim() === 'Your dens')`, 'Home');
        await gettingAround(h, ctx, d, report).catch((e) => report(false, e.message));
    }
    if (want('screens')) {
        // The member's own message, newest, whose menu holds everything.
        const sent = await b.api('POST', `/api/dens/${ctx.den}/channels/${ctx.general}/messages`,
            JSON.stringify({ nonce: Buffer.from(crypto.getRandomValues(new Uint8Array(16))).toString('base64url'), text: 'the walk was here', attachments: [] }));
        report(sent.status === 200, `the member says something to walk from: ${sent.status}`);
        await memberScreens(h, ctx);
    }
    // Nothing makes the page itself scroll sideways on a den's panes
    // (M13.1); the audits' other findings wait for M13.2 and M13.3.
    for (const shot of h.log) {
        if (shot.failed) report(false, `${shot.name} wasn't reached: ${shot.failed}`);
        else if (/^(den-list|channel|members|dm|around)/.test(shot.name)) report(!shot.audit?.sideways, `${shot.name}: the page doesn't scroll sideways`);
    }
    fs.writeFileSync(`${out}/audits-${parts.join('-') || 'around'}.json`, JSON.stringify(h.log, null, 1));
} finally {
    b.close();
}
process.exit(ok ? 0 : 1);
