// The browsers the container's checks drive: Chromium over the DevTools
// protocol and Firefox over WebDriver BiDi, through Node's own WebSocket,
// with nothing installed from npm. Each launch has a profile of its own,
// which close removes. See setup.sh for the container.

import { execFileSync, spawn } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import fs from 'node:fs';
import net from 'node:net';

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

// The app's settings (M8.3): the dialog, a tab of it by name, and opening
// it at a tab from the cog on either page, as expressions for a page's
// eval and a step that waits.
export const SETTINGS = `document.querySelector('[role=dialog][aria-label="App settings"]')`;
export const settingsTab = (name) => `[...document.querySelectorAll('[role=dialog][aria-label="App settings"] [role=tab]')].find((t) => t.textContent.trim() === ${JSON.stringify(name)})`;
export async function openSettings(b, tab) {
    if (!(await b.eval(`!!${SETTINGS}`))) await b.eval(`document.querySelector('button[aria-label="App settings"], a[aria-label="App settings"]').click(), true`);
    await waitFor(() => b.eval(`!!${settingsTab(tab)}`), 'the settings');
    await b.eval(`${settingsTab(tab)}.click(), true`);
    await waitFor(() => b.eval(`${settingsTab(tab)}.getAttribute('aria-selected') === 'true'`), `the settings' ${tab} tab`);
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

// token is a pairing token for an instance's page.
export const token = (instance) =>
    execFileSync(`${HOME}/dens-dev`, ['-i', instance, 'open', '--print']).toString().trim().split('#token=')[1];

// lib runs a function of lib.sh, as the prep scripts do, such as api,
// stop or start, and returns what it prints.
export const lib = (...args) =>
    execFileSync('bash', ['-c', 'source "$0" && "$@"', new URL('./lib.sh', import.meta.url).pathname, ...args]).toString().trim();

// pair pairs a browser with a member's instance, at its page's home.
export async function pair(b, who) {
    await b.go(`http://localhost:${who.port}/#token=${token(who.instance)}`);
    await waitFor(() => b.eval(`!!document.querySelector('a[href="/settings"]')`), `${who.name} paired`);
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

async function chromium(port, { media, screen, autoplay, preload, size = [1400, 900], downloads, hover }) {
    const profile = `/tmp/browsers-chromium-${port}`;
    fs.rmSync(profile, { recursive: true, force: true });
    const args = ['--headless=new', '--no-sandbox', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
        `--window-size=${size[0]},${size[1]}`, '--no-first-run', '--no-default-browser-check',
        `--autoplay-policy=${autoplay === 'gesture' ? 'document-user-activation-required' : 'no-user-gesture-required'}`];
    if (media || screen) args.push('--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream');
    if (media) args.push(`--use-file-for-fake-audio-capture=${HOME}/speech.wav`);
    if (screen) args.push('--auto-select-desktop-capture-source=Entire screen');
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
        // must be JSON's; gesture runs it as from a click.
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
        // well as those set before; intercepted lists the requests caught.
        intercepted: [],
        async answer(path, status, body) {
            answers.set(path, { status, body });
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
                                responseHeaders: [{ name: 'Content-Type', value: 'application/json' }], body: Buffer.from(a.body).toString('base64') })
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
const FIREFOX_BUTTONS = { left: 0, back: 3 };
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
        async attach(selector, files) {
            const node = await evaluate(`document.querySelector(${JSON.stringify(selector)})`, context, { resultOwnership: 'root' });
            await bidi.send('input.setFiles', { context, element: { sharedId: node.sharedId }, files });
        },
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
