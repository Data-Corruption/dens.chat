// The browsers the container's checks drive: Chromium over the DevTools
// protocol and Firefox over WebDriver BiDi, through Node's own WebSocket,
// with nothing installed from npm. Each launch has a profile of its own,
// which close removes. See setup.sh for the container.

import { execFileSync, spawn } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import fs from 'node:fs';
import net from 'node:net';
import zlib from 'node:zlib';

// HOME is the tester's home in the container: the binaries, the media the
// checks send, and the screenshots they take are there.
export const HOME = process.env.HOME;

// The development instances the checks use, as lib.sh starts them: m2a
// hosts the den, and the others are members.
export const ALICE = { name: 'alice', instance: 'm2a', port: 18484 };
export const BOB = { name: 'bob', instance: 'm2b', port: 18486 };
export const CAROL = { name: 'carol', instance: 'm2c', port: 18487 };

export async function waitFor(fn, what, ms = 20000) {
    const end = Date.now() + ms;
    let last;
    while (Date.now() < end) {
        try {
            last = await fn();
            if (last) return last;
        } catch (e) {
            last = e.message;
        }
        await sleep(250);
    }
    throw new Error(`timed out waiting for ${what}: ${typeof last === 'string' ? last : JSON.stringify(last)}`);
}

// The app's settings (M8.3): the dialog, a section of it by name in the
// list down its side (M19.1), and opening it at a section from the cog on
// either page, as expressions for a page's eval and a step that waits.
export const SETTINGS = `document.querySelector('[role=dialog][aria-label="Settings"]')`;
export const settingsTab = (name) => `[...document.querySelectorAll('[role=dialog][aria-label="Settings"] nav button')].find((t) => t.textContent.trim() === ${JSON.stringify(name)})`;
export async function openSettings(b, tab) {
    if (tab === 'Appearance') return openAppearance(b);
    if (!(await b.eval(`!!${SETTINGS}`))) await b.eval(`document.querySelector('button[aria-label="Settings"], a[aria-label="Settings"]').click(), true`);
    // Appearance takes the window, its list folded to ← (M22.8).
    if (await b.eval(`!!${APPEARANCE}`)) await b.eval(`${SETTINGS}.querySelector('[data-back]').click(), true`);
    await waitFor(() => b.eval(`!!${settingsTab(tab)}`), 'the settings');
    await b.eval(`${settingsTab(tab)}.click(), true`);
    await waitFor(() => b.eval(`${settingsTab(tab)}.getAttribute('aria-current') === 'page'`), `the settings' ${tab} section`);
}

// Appearance (M22.8), a page of the settings that takes the window:
// openAppearance opens it at a tab of its middle column, Theme, Messages
// or Calls, which the phone layout shows under the foot's Editor.
export const APPEARANCE = `document.querySelector('[role=dialog][aria-label="Settings"] [data-appearance]')`;
export const paneTab = (pane) => `document.querySelector('[data-pane-tab=${pane.toLowerCase()}]')`;
export async function openAppearance(b, pane = 'Theme') {
    if (!(await b.eval(`!!${SETTINGS}`))) await b.eval(`document.querySelector('button[aria-label="Settings"], a[aria-label="Settings"]').click(), true`);
    if (!(await b.eval(`!!${APPEARANCE}`))) {
        const row = `${SETTINGS}?.querySelector('[data-section=appearance]')`;
        // A phone shows another section's page over the list.
        if (!(await b.eval(`!!${row}`)) && (await b.eval(`!!${SETTINGS}?.querySelector('[data-back]')`))) await b.eval(`${SETTINGS}.querySelector('[data-back]').click(), true`);
        await waitFor(() => b.eval(`!!${row}`), 'Appearance in the settings');
        await b.eval(`${row}.click(), true`);
        await waitFor(() => b.eval(`!!${APPEARANCE}`), 'Appearance');
    }
    if (!(await b.eval(`!!${paneTab(pane)}`))) await b.eval(`document.querySelector('[data-foot-tab=editor]')?.click(), true`);
    await waitFor(() => b.eval(`!!${paneTab(pane)}`), "Appearance's tabs");
    await b.eval(`${paneTab(pane)}.click(), true`);
    await waitFor(() => b.eval(`${paneTab(pane)}.getAttribute('aria-selected') === 'true'`), `Appearance's ${pane}`);
}

// closeSettings closes the settings by Escape, which goes from Appearance
// to the section open before it, and from there closes them.
export async function closeSettings(b) {
    for (let i = 0; i < 3 && (await b.eval(`!!${SETTINGS}`)); i++) {
        await b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
        await sleep(300);
    }
    await waitFor(async () => !(await b.eval(`!!${SETTINGS}`)), 'the settings closed');
}

// picture is a screenshot's pixels, a PNG of 8 bits a channel as the
// browsers take them: at(x, y) is one, as [r, g, b].
export function picture(file) {
    const data = fs.readFileSync(file);
    const w = data.readUInt32BE(16), h = data.readUInt32BE(20);
    const n = { 2: 3, 6: 4 }[data[25]];
    if (data[24] !== 8 || !n || data[28] !== 0) throw new Error(`a screenshot of another kind: ${data[24]} bits, color type ${data[25]}`);
    const idat = [];
    for (let at = 8; at < data.length;) {
        const len = data.readUInt32BE(at);
        if (data.toString('latin1', at + 4, at + 8) === 'IDAT') idat.push(data.subarray(at + 8, at + 8 + len));
        at += 12 + len;
    }
    const raw = zlib.inflateSync(Buffer.concat(idat));
    const stride = w * n;
    const rows = [];
    let prev = Buffer.alloc(stride);
    for (let row = 0; row < h; row++) {
        const filter = raw[row * (stride + 1)];
        const line = Buffer.from(raw.subarray(row * (stride + 1) + 1, (row + 1) * (stride + 1)));
        for (let i = 0; i < stride; i++) {
            const a = i >= n ? line[i - n] : 0;
            const up = prev[i];
            const cc = i >= n ? prev[i - n] : 0;
            const p = a + up - cc;
            const paeth = Math.abs(p - a) <= Math.abs(p - up) && Math.abs(p - a) <= Math.abs(p - cc) ? a : Math.abs(p - up) <= Math.abs(p - cc) ? up : cc;
            line[i] = (line[i] + [0, a, up, (a + up) >> 1, paeth][filter]) & 255;
        }
        rows.push(line);
        prev = line;
    }
    return { w, h, at: (x, y) => [...rows[y].subarray(x * n, x * n + 3)] };
}

// CLOSE_CARD closes a profile card, as an expression for a page's eval:
// beside its click on a wide window it has no ✕, and Escape closes it
// (M22.4); as a phone's sheet, its ✕.
export const CLOSE_CARD = `(document.querySelector('[role=dialog][aria-label="Profile"] button[aria-label="Close"]')?.click()
    ?? document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true)`;

// openYours opens one of a member's own sections of a den (M22.3): its
// page, if the browser isn't on it, then the cog's settings, which list
// them first.
export async function openYours(b, port, den, section) {
    if (!(await b.eval(`location.pathname.startsWith(${JSON.stringify(`/den/${den}`)})`))) await b.go(`http://localhost:${port}/den/${den}`);
    await waitFor(() => b.eval(`!!document.querySelector('button[aria-label="Settings"]')`), "the den's cog");
    await openSettings(b, section);
}

// openManage opens the Manage dialog of the first den on home's list
// (M8.3) at a section, from the home page.
export const MANAGE = `document.querySelector('[role=dialog][aria-label^="Manage "]')`;
export async function openManage(b, section) {
    const manage = `[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === 'Manage')`;
    if (!(await b.eval(`!!${MANAGE}`))) {
        await waitFor(() => b.eval(`!!${manage}`), 'the Manage button');
        await b.eval(`${manage}.click(), true`);
    }
    const item = `[...document.querySelectorAll('[role=dialog] nav button')].find((x) => x.textContent.trim() === ${JSON.stringify(section)})`;
    await waitFor(() => b.eval(`!!${item}`), `Manage's ${section}`);
    await b.eval(`${item}.click(), true`);
}

// The den's name opens its menu, whose first row is Manage, and a
// channel's or a group's ⋯ its menu; a menu's rows are named by their
// keys. manageFromName opens Manage from the name, and fromMenu picks a
// row from the menu a button opens, as expressions for a page's eval.
export const DEN_NAME = `document.querySelector('[data-den-menu]')`;
export const menuRow = (key) => `document.querySelector('[data-menu-item=${JSON.stringify(key)}]')`;
export async function fromMenu(b, opener, key) {
    await waitFor(() => b.eval(`!!${opener}`), `the menu's button for ${key}`);
    await b.eval(`${opener}.click(), true`);
    await waitFor(() => b.eval(`!!${menuRow(key)}`), `the menu's ${key}`);
    await b.eval(`${menuRow(key)}.click(), true`);
}
export const manageFromName = (b) => fromMenu(b, DEN_NAME, 'manage');
// rowMenu is the ⋯ of a channel's row, by its name, or a group's.
export const rowMenu = (name) => `document.querySelector('button[aria-label=${JSON.stringify(`Options for ${name}`)}]')`;

// token is a pairing token for an instance's page.
export const token = (instance) =>
    execFileSync(`${HOME}/dens-dev`, ['-i', instance, 'open', '--print']).toString().trim().split('#token=')[1];

// lib runs a function of lib.sh, as the prep scripts do, such as api,
// stop or start, and returns what it prints.
export const lib = (...args) =>
    execFileSync('bash', ['-c', 'source "$0" && "$@"', new URL('./lib.sh', import.meta.url).pathname, ...args]).toString().trim();

// pair pairs a browser with a member's instance, at its page's home.
export async function pair(b, who, previews = false) {
    await b.go(`http://localhost:${who.port}/#token=${token(who.instance)}`);
    await paired(b, `${who.name} paired`, previews);
}

// paired waits for a page just paired to reach its home, answering Link
// previews as the page asks a browser new to Dens (M22.7): off, as they
// were before it asked, unless previews.
export const PREVIEWS = `document.querySelector('[data-link-previews]')`;
export async function paired(b, what, previews = false) {
    const home = `!!document.querySelector('a[href="/settings"]')`;
    await waitFor(() => b.eval(`${home} || !!${PREVIEWS}`), what);
    if (!(await b.eval(`!!${PREVIEWS}`))) return;
    if (!previews) await b.eval(`document.getElementById('link-previews').click(), true`);
    await b.eval(`[...document.querySelectorAll('button[type=submit]')].find((x) => x.textContent.trim() === 'Continue').click(), true`);
    await waitFor(() => b.eval(home), what);
}

// helper stands in for the shortcut helper (dens shortcuts, M7) on an
// instance's control socket: it sends what a driver gives it, and keeps
// what the service sends, the latest state among it.
export function helper(instance = 'm2a') {
    const c = net.createConnection(`${process.env.XDG_RUNTIME_DIR}/dens-dev/${instance}/control.sock`);
    let buf = '';
    const lines = [];
    c.on('data', (d) => {
        buf += d;
        for (let i; (i = buf.indexOf('\n')) >= 0; buf = buf.slice(i + 1)) lines.push(JSON.parse(buf.slice(0, i)));
    });
    c.write(JSON.stringify({ op: 'shortcuts' }) + '\n');
    const send = (t, d) => c.write(JSON.stringify(d === undefined ? { t } : { t, d }) + '\n');
    const ping = setInterval(() => send('ping'), 10000);
    return {
        lines,
        send,
        state: () => [...lines].reverse().find((m) => m.t === 'state')?.d,
        close() {
            clearInterval(ping);
            c.destroy();
        },
    };
}

// checks collects a driver's results: report prints each, and finish
// exits with whether all held.
export function checks() {
    let ok = true;
    return {
        report(good, what) {
            ok &&= !!good;
            console.log(`${good ? 'OK  ' : 'BAD '} ${what}`);
        },
        fail(e) {
            ok = false;
            console.log(`BAD  ${e.stack || e.message}`);
        },
        finish() {
            console.log(ok ? 'ALL OK' : 'SOME BAD');
            process.exit(ok ? 0 : 1);
        },
    };
}

class Socket {
    constructor(url) {
        this.ws = new WebSocket(url);
        this.id = 0;
        this.waiting = new Map();
        this.events = [];
        this.ws.onmessage = (e) => {
            const m = JSON.parse(e.data);
            if (m.id === undefined || m.type === 'event') return void this.events.push(m);
            const w = this.waiting.get(m.id);
            if (!w) return;
            this.waiting.delete(m.id);
            if (m.error || m.type === 'error') w.reject(new Error(JSON.stringify(m.error || m.message || m)));
            else w.resolve(m.result);
        };
    }

    open() {
        return new Promise((resolve, reject) => {
            this.ws.onopen = resolve;
            this.ws.onerror = reject;
        });
    }

    send(method, params = {}, extra = {}) {
        const id = ++this.id;
        this.ws.send(JSON.stringify({ id, method, params, ...extra }));
        return new Promise((resolve, reject) => this.waiting.set(id, { resolve, reject }));
    }
}

// launch starts a browser, 'chromium' or 'firefox', driven on port. Options:
//   media: a fake microphone. Chromium plays speech.wav into it, and Firefox
//     makes its own, or, with 'pulse', takes PulseAudio's, as from a null
//     sink's monitor.
//   screen: something to share. Chromium shares its fake screen; Firefox
//     runs headed on Xvfb's display :99 and shares that.
//   autoplay: 'gesture' makes Chromium play sound only after a click, as a
//     browser does by default; otherwise it plays without one.
//   preload: a function's source, run in each document before the page.
//   prefs: more Firefox prefs, as { name: value }.
//   size: the window, [1400, 900] unless given.
//   downloads: a directory downloads go to, without asking (M10).
//   hover: a pointer that hovers, as a desktop's mouse. Headless, both
//     browsers say they have no pointer, so what shows on hover never
//     shows (M11). Chromium's touch emulation forgets it.
//   camera: a Y4M file Chromium's camera plays, again and again, read
//     each time the page opens the camera (M22.6).
// A launched browser's windows, windowEval, closeWindow and windowShot reach
// the windows its page opens, as a share's own (M11): how many there are,
// and the newest.
export function launch(name, port, opts = {}) {
    return name === 'chromium' ? chromium(port, opts) : firefox(port, opts);
}

const CHROMIUM_KEYS = {
    ' ': ['Space', 32, ' '],
    ArrowRight: ['ArrowRight', 39],
    ArrowLeft: ['ArrowLeft', 37],
    ArrowUp: ['ArrowUp', 38],
    ArrowDown: ['ArrowDown', 40],
    Escape: ['Escape', 27],
    Enter: ['Enter', 13, '\r'],
    Tab: ['Tab', 9],
    PageUp: ['PageUp', 33],
    PageDown: ['PageDown', 34],
    Home: ['Home', 36],
    End: ['End', 35],
    v: ['KeyV', 86, 'v'],
};

async function chromium(port, { media, screen, autoplay, preload, size = [1400, 900], downloads, hover, camera }) {
    const profile = `/tmp/browsers-chromium-${port}`;
    fs.rmSync(profile, { recursive: true, force: true });
    const args = ['--headless=new', '--no-sandbox', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
        `--window-size=${size[0]},${size[1]}`, '--no-first-run', '--no-default-browser-check',
        `--autoplay-policy=${autoplay === 'gesture' ? 'document-user-activation-required' : 'no-user-gesture-required'}`];
    if (media || screen) args.push('--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream');
    if (media) args.push(`--use-file-for-fake-audio-capture=${HOME}/speech.wav`);
    if (screen) args.push('--auto-select-desktop-capture-source=Entire screen');
    if (camera) args.push('--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream', `--use-file-for-fake-video-capture=${camera}`);
    if (hover) args.push('--blink-settings=primaryPointerType=4,availablePointerTypes=4,primaryHoverType=2,availableHoverTypes=2');
    spawn('/usr/bin/chromium', [...args, 'about:blank'], { stdio: 'ignore', detached: true }).unref();
    const target = await waitFor(async () => (await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()).find((t) => t.type === 'page'), 'chromium');
    const version = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
    const browser = new Socket(version.webSocketDebuggerUrl);
    await browser.open();
    const { sessionId } = await browser.send('Target.attachToTarget', { targetId: target.id, flatten: true });
    const cdp = (method, params = {}, session = sessionId) => browser.send(method, params, session ? { sessionId: session } : {});
    await cdp('Page.enable');
    if (downloads) await cdp('Browser.setDownloadBehavior', { behavior: 'allow', downloadPath: downloads }, null);
    if (preload) await cdp('Page.addScriptToEvaluateOnNewDocument', { source: `(${preload})()` });
    // Frames attach as they load, so frameEval can find them.
    await cdp('Target.setAutoAttach', { autoAttach: true, waitForDebuggerOnStart: false, flatten: true });
    const frame = async (match) => {
        const { targetInfos } = await cdp('Target.getTargets', {}, null);
        const t = targetInfos.filter((x) => x.type === 'iframe' && x.url.includes(match)).at(-1);
        const attached = t && browser.events.find((e) => e.method === 'Target.attachedToTarget' && e.params.targetInfo.targetId === t.targetId);
        return attached?.params.sessionId;
    };
    const mouse = (type, x, y, extra = {}) => cdp('Input.dispatchMouseEvent', { type, x, y, ...extra });
    const answers = new Map();
    let answering = false;
    // The page's own windows are the pages besides it, each attached once.
    const windows = async () => (await cdp('Target.getTargets', {}, null)).targetInfos.filter((t) => t.type === 'page' && t.targetId !== target.id);
    const sessions = new Map();
    const newest = async () => {
        const t = (await windows()).at(-1);
        if (!t) return null;
        if (!sessions.has(t.targetId)) sessions.set(t.targetId, (await browser.send('Target.attachToTarget', { targetId: t.targetId, flatten: true })).sessionId);
        return { target: t.targetId, session: sessions.get(t.targetId) };
    };
    return {
        name: 'chromium',
        windows: async () => (await windows()).length,
        async windowEval(expression) {
            const w = await newest();
            if (!w) return null;
            const r = await cdp('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true }, w.session);
            if (r.exceptionDetails) throw new Error(r.exceptionDetails.text + ' ' + (r.exceptionDetails.exception?.description || ''));
            return r.result.value;
        },
        async closeWindow() {
            const w = await newest();
            if (w) await cdp('Target.closeTarget', { targetId: w.target }, null);
        },
        async windowShot(file) {
            const w = await newest();
            const r = await cdp('Page.captureScreenshot', { format: 'png' }, w.session);
            fs.writeFileSync(file, Buffer.from(r.data, 'base64'));
        },
        cdp,
        events: browser.events,
        async go(url) {
            await cdp('Page.navigate', { url });
            await sleep(1500);
        },
        // eval runs an expression in the page and returns its value, which
        // must be JSON's; gesture runs it as from a click. A template
        // literal building the expression drops a regex's backslashes (\d
        // becomes d), so write [0-9]; and text and selectors go in through
        // JSON.stringify, which an apostrophe can't end.
        async eval(expression, gesture = false) {
            const r = await cdp('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true, userGesture: gesture });
            if (r.exceptionDetails) throw new Error(r.exceptionDetails.text + ' ' + (r.exceptionDetails.exception?.description || ''));
            return r.result.value;
        },
        // frameEval runs an expression in the last frame whose address
        // includes match, or returns null if there's none.
        async frameEval(match, expression) {
            const session = await frame(match);
            if (!session) return null;
            return (await cdp('Runtime.evaluate', { expression, returnByValue: true }, session)).result.value;
        },
        frame,
        async click(x, y, button = 'left') {
            await mouse('mouseMoved', x, y);
            await mouse('mousePressed', x, y, { button, clickCount: 1 });
            await mouse('mouseReleased', x, y, { button, clickCount: 1 });
        },
        // move moves the pointer there, hovering what's under it (M11).
        move: (x, y) => mouse('mouseMoved', x, y),
        // touch has the page take this browser for a touch screen's, or
        // not, as its pointer and hover media say (M11); tap then taps.
        async touch(on) {
            await cdp('Emulation.setTouchEmulationEnabled', on ? { enabled: true, maxTouchPoints: 1 } : { enabled: false });
        },
        async tap(x, y) {
            await cdp('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
            await cdp('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
        },
        // press holds a finger there for ms, a long press (M13.2).
        async press(x, y, ms = 800) {
            await cdp('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
            await sleep(ms);
            await cdp('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
        },
        async drag(x1, y1, x2, y2) {
            await mouse('mouseMoved', x1, y1);
            await mouse('mousePressed', x1, y1, { button: 'left', clickCount: 1 });
            for (let i = 1; i <= 5; i++) await mouse('mouseMoved', x1 + ((x2 - x1) * i) / 5, y1 + ((y2 - y1) * i) / 5, { button: 'left', buttons: 1 });
            await mouse('mouseReleased', x2, y2, { button: 'left', clickCount: 1 });
        },
        // key presses a key, or with type 'down' or 'up' only that half.
        async key(key, type) {
            const [code, vk, text] = CHROMIUM_KEYS[key];
            const down = { type: text ? 'keyDown' : 'rawKeyDown', key, code, windowsVirtualKeyCode: vk, ...(text ? { text } : {}) };
            if (type !== 'up') await cdp('Input.dispatchKeyEvent', down);
            if (type !== 'down') await cdp('Input.dispatchKeyEvent', { type: 'keyUp', key, code, windowsVirtualKeyCode: vk });
        },
        // chord presses a key while modifiers ('Control', 'Shift', 'Alt',
        // 'Meta') are held (M11).
        async chord(mods, key) {
            const bits = { Alt: 1, Control: 2, Meta: 4, Shift: 8 };
            const modifiers = mods.reduce((m, k) => m | bits[k], 0);
            const [code, vk] = CHROMIUM_KEYS[key];
            await cdp('Input.dispatchKeyEvent', { type: 'rawKeyDown', key, code, windowsVirtualKeyCode: vk, modifiers });
            await cdp('Input.dispatchKeyEvent', { type: 'keyUp', key, code, windowsVirtualKeyCode: vk, modifiers });
        },
        // type types text into what has focus, as keys would.
        async type(text) {
            await cdp('Input.insertText', { text });
        },
        async attach(selector, files) {
            const { root } = await cdp('DOM.getDocument', {});
            const { nodeId } = await cdp('DOM.querySelector', { nodeId: root.nodeId, selector });
            await cdp('DOM.setFileInputFiles', { files, nodeId });
        },
        async focus() {
            await cdp('Page.bringToFront');
        },
        // answer has the page's requests for a path get this answer, as
        // well as those set before: JSON unless type says otherwise, and
        // body a string or a Buffer; intercepted lists the requests caught.
        intercepted: [],
        async answer(path, status, body, type = 'application/json') {
            answers.set(path, { status, body, type });
            await cdp('Fetch.enable', { patterns: [...answers.keys()].map((p) => ({ urlPattern: `*${p}`, requestStage: 'Request' })) });
            if (answering) return;
            answering = true;
            (async () => {
                for (;;) {
                    const ev = browser.events.find((e) => e.method === 'Fetch.requestPaused' && !e.handled);
                    if (ev) {
                        ev.handled = true;
                        this.intercepted.push(ev.params.request.url);
                        const [, a] = [...answers].find(([p]) => ev.params.request.url.endsWith(p)) || [];
                        await (a
                            ? cdp('Fetch.fulfillRequest', { requestId: ev.params.requestId, responseCode: a.status,
                                responseHeaders: [{ name: 'Content-Type', value: a.type }], body: Buffer.from(a.body).toString('base64') })
                            : cdp('Fetch.continueRequest', { requestId: ev.params.requestId })).catch(() => {});
                    }
                    await sleep(100);
                }
            })();
        },
        async shot(file, format = 'png') {
            const r = await cdp('Page.captureScreenshot', { format });
            fs.writeFileSync(file, Buffer.from(r.data, 'base64'));
        },
        async close() {
            await cdp('Browser.close', {}, null).catch(() => {});
            await sleep(1000);
            fs.rmSync(profile, { recursive: true, force: true });
        },
    };
}

// The keys' values in WebDriver: a character, or a code point in Unicode's
// private use area for the others.
const FIREFOX_KEYS = {
    ' ': ' ',
    ArrowRight: '',
    ArrowLeft: '',
    ArrowUp: '',
    ArrowDown: '',
    Escape: '',
    Enter: '',
    Tab: '',
    PageUp: '',
    PageDown: '',
    Home: '\uE011',
    End: '\uE010',
    v: 'v',
};
const FIREFOX_BUTTONS = { left: 0, right: 2, back: 3 };
const FIREFOX_MODIFIERS = { Control: '\uE009', Shift: '\uE008', Alt: '\uE00A', Meta: '\uE03D' };

async function firefox(port, { media, screen, preload, prefs = {}, size = [1400, 900], downloads, hover }) {
    const profile = `/tmp/browsers-firefox-${port}`;
    fs.rmSync(profile, { recursive: true, force: true });
    fs.mkdirSync(profile, { recursive: true });
    const all = { 'browser.shell.checkDefaultBrowser': false, 'datareporting.policy.dataSubmissionEnabled': false };
    if (media || screen) {
        Object.assign(all, { 'media.navigator.permission.disabled': true, 'media.navigator.streams.fake': media !== 'pulse' && !screen,
            'media.autoplay.default': 0, 'media.autoplay.block-webaudio': false });
    }
    if (downloads) {
        Object.assign(all, { 'browser.download.dir': downloads, 'browser.download.folderList': 2, 'browser.download.useDownloadDir': true,
            'browser.download.always_ask_before_handling_new_types': false, 'browser.helperApps.neverAsk.saveToDisk': 'application/octet-stream',
            'browser.download.start_downloads_in_tmp_dir': false });
    }
    // A fine pointer that hovers, in Firefox's bits: fine 2, hover 4.
    if (hover) Object.assign(all, { 'ui.primaryPointerCapabilities': 6, 'ui.allPointerCapabilities': 6 });
    Object.assign(all, prefs);
    fs.writeFileSync(`${profile}/user.js`, Object.entries(all).map(([k, v]) => `user_pref(${JSON.stringify(k)}, ${JSON.stringify(v)});`).join('\n'));
    const args = [`--remote-debugging-port=${port}`, '--profile', profile, '--no-remote', `--width=${size[0]}`, `--height=${size[1]}`];
    if (!screen) args.unshift('--headless');
    spawn('/usr/bin/firefox-esr', args, { stdio: 'ignore', detached: true, env: { ...process.env, DISPLAY: ':99' } }).unref();
    const bidi = await waitFor(async () => {
        const s = new Socket(`ws://127.0.0.1:${port}/session`);
        await s.open();
        return s;
    }, 'firefox');
    await bidi.send('session.new', { capabilities: {} });
    if (preload) await bidi.send('script.addPreloadScript', { functionDeclaration: preload });
    const { context } = await bidi.send('browsingContext.create', { type: 'tab' });
    // The paths answer has set, and what each gets.
    const firefoxAnswers = new Map();
    // The page's own windows are the top-level contexts that came after it.
    const before = new Set((await bidi.send('browsingContext.getTree', {})).contexts.map((x) => x.context));
    const windows = async () => (await bidi.send('browsingContext.getTree', {})).contexts.filter((x) => !before.has(x.context));
    const evaluate = async (expression, target, extra = {}) => {
        const r = await bidi.send('script.evaluate', { expression, target: { context: target }, awaitPromise: true, ...extra });
        if (r.type === 'exception') throw new Error(JSON.stringify(r.exceptionDetails?.text || r));
        return r.result;
    };
    // BiDi hands back its own form of a value, so results cross as JSON.
    const json = async (expression, target, extra) => {
        const r = await evaluate(`(async () => JSON.stringify(await (${expression})))()`, target, extra);
        return r.value === undefined ? undefined : JSON.parse(r.value);
    };
    const pointer = (actions) => bidi.send('input.performActions', { context, actions: [{ type: 'pointer', id: 'mouse', actions }] });
    const frame = async (match) => {
        const { contexts } = await bidi.send('browsingContext.getTree', { root: context });
        const found = [];
        const walk = (c) => {
            if (c.url.includes(match) && c.context !== context) found.push(c.context);
            (c.children || []).forEach(walk);
        };
        contexts.forEach(walk);
        return found.at(-1);
    };
    return {
        name: 'firefox',
        bidi: (method, params) => bidi.send(method, params),
        context,
        windows: async () => (await windows()).length,
        async windowEval(expression) {
            const w = (await windows()).at(-1);
            return w ? json(expression, w.context) : null;
        },
        async closeWindow() {
            const w = (await windows()).at(-1);
            if (w) await bidi.send('browsingContext.close', { context: w.context });
        },
        async windowShot(file) {
            const w = (await windows()).at(-1);
            const r = await bidi.send('browsingContext.captureScreenshot', { context: w.context });
            fs.writeFileSync(file, Buffer.from(r.data, 'base64'));
        },
        async go(url) {
            await bidi.send('browsingContext.navigate', { context, url, wait: 'complete' });
            await sleep(1500);
        },
        eval: (expression, gesture = false) => json(expression, context, { userActivation: gesture }),
        async frameEval(match, expression) {
            const c = await frame(match);
            return c ? json(expression, c) : null;
        },
        frame,
        click: (x, y, button = 'left') =>
            pointer([{ type: 'pointerMove', x: Math.round(x), y: Math.round(y) }, { type: 'pointerDown', button: FIREFOX_BUTTONS[button] },
                { type: 'pointerUp', button: FIREFOX_BUTTONS[button] }]),
        move: (x, y) => pointer([{ type: 'pointerMove', x: Math.round(x), y: Math.round(y) }]),
        // Firefox takes itself for a touch screen's from prefs, at launch:
        // ui.primaryPointerCapabilities and ui.allPointerCapabilities at 1,
        // a coarse pointer that can't hover.
        tap: (x, y) => bidi.send('input.performActions', { context, actions: [{ type: 'pointer', id: 'finger', parameters: { pointerType: 'touch' },
            actions: [{ type: 'pointerMove', x: Math.round(x), y: Math.round(y) }, { type: 'pointerDown', button: 0 }, { type: 'pointerUp', button: 0 }] }] }),
        press: (x, y, ms = 800) => bidi.send('input.performActions', { context, actions: [{ type: 'pointer', id: 'finger', parameters: { pointerType: 'touch' },
            actions: [{ type: 'pointerMove', x: Math.round(x), y: Math.round(y) }, { type: 'pointerDown', button: 0 }, { type: 'pause', duration: ms },
                { type: 'pointerUp', button: 0 }] }] }),
        drag: (x1, y1, x2, y2) =>
            pointer([{ type: 'pointerMove', x: Math.round(x1), y: Math.round(y1) }, { type: 'pointerDown', button: 0 },
                { type: 'pointerMove', x: Math.round(x2), y: Math.round(y2), duration: 200 }, { type: 'pointerUp', button: 0 }]),
        async key(key, type) {
            const actions = [];
            if (type !== 'up') actions.push({ type: 'keyDown', value: FIREFOX_KEYS[key] });
            if (type !== 'down') actions.push({ type: 'keyUp', value: FIREFOX_KEYS[key] });
            await bidi.send('input.performActions', { context, actions: [{ type: 'key', id: 'keyboard', actions }] });
        },
        async chord(mods, key) {
            const actions = [
                ...mods.map((m) => ({ type: 'keyDown', value: FIREFOX_MODIFIERS[m] })),
                { type: 'keyDown', value: FIREFOX_KEYS[key] }, { type: 'keyUp', value: FIREFOX_KEYS[key] },
                ...mods.map((m) => ({ type: 'keyUp', value: FIREFOX_MODIFIERS[m] })),
            ];
            await bidi.send('input.performActions', { context, actions: [{ type: 'key', id: 'keyboard', actions }] });
        },
        async type(text) {
            const actions = [...text].flatMap((c) => [{ type: 'keyDown', value: c }, { type: 'keyUp', value: c }]);
            await bidi.send('input.performActions', { context, actions: [{ type: 'key', id: 'keyboard', actions }] });
        },
        // answer has the page's requests for a path get this answer, as
        // Chromium's does, through BiDi's network intercepts.
        intercepted: [],
        async answer(path, status, body, type = 'application/json') {
            if (!firefoxAnswers.size) {
                await bidi.send('session.subscribe', { events: ['network.beforeRequestSent'], contexts: [context] });
                const intercepted = this.intercepted;
                (async () => {
                    for (;;) {
                        const ev = bidi.events.find((e) => e.method === 'network.beforeRequestSent' && !e.handled);
                        if (ev) {
                            ev.handled = true;
                            if (ev.params.isBlocked) {
                                const url = ev.params.request.url;
                                intercepted.push(url);
                                const [, a] = [...firefoxAnswers].find(([p]) => new URL(url).pathname === p) || [];
                                const request = ev.params.request.request;
                                await (a ? bidi.send('network.provideResponse', { request, statusCode: a.status,
                                    headers: [{ name: 'Content-Type', value: { type: 'string', value: a.type } }],
                                    body: { type: 'base64', value: Buffer.from(a.body).toString('base64') } })
                                    : bidi.send('network.continueRequest', { request })).catch(() => {});
                            }
                        }
                        await sleep(50);
                    }
                })();
            }
            if (!firefoxAnswers.has(path)) {
                await bidi.send('network.addIntercept', { phases: ['beforeRequestSent'], contexts: [context], urlPatterns: [{ type: 'pattern', pathname: path }] });
            }
            firefoxAnswers.set(path, { status, body, type });
        },
        async attach(selector, files) {
            const node = await evaluate(`document.querySelector(${JSON.stringify(selector)})`, context, { resultOwnership: 'root' });
            await bidi.send('input.setFiles', { context, element: { sharedId: node.sharedId }, files });
        },
        // Headless Firefox never has its window's focus, so a check of
        // focus runs in Chromium.
        async focus() {},
        async shot(file) {
            const r = await bidi.send('browsingContext.captureScreenshot', { context });
            fs.writeFileSync(file, Buffer.from(r.data, 'base64'));
        },
        async close() {
            await bidi.send('browser.close', {}).catch(() => {});
            await sleep(1500);
            fs.rmSync(profile, { recursive: true, force: true });
        },
    };
}
