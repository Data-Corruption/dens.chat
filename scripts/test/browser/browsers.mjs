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
    v: ['KeyV', 86, 'v'],
};

async function chromium(port, { media, screen, autoplay, preload, size = [1400, 900] }) {
    const profile = `/tmp/browsers-chromium-${port}`;
    fs.rmSync(profile, { recursive: true, force: true });
    const args = ['--headless=new', '--no-sandbox', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
        `--window-size=${size[0]},${size[1]}`, '--no-first-run', '--no-default-browser-check',
        `--autoplay-policy=${autoplay === 'gesture' ? 'document-user-activation-required' : 'no-user-gesture-required'}`];
    if (media || screen) args.push('--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream');
    if (media) args.push(`--use-file-for-fake-audio-capture=${HOME}/speech.wav`);
    if (screen) args.push('--auto-select-desktop-capture-source=Entire screen');
    spawn('/usr/bin/chromium', [...args, 'about:blank'], { stdio: 'ignore', detached: true }).unref();
    const target = await waitFor(async () => (await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()).find((t) => t.type === 'page'), 'chromium');
    const version = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
    const browser = new Socket(version.webSocketDebuggerUrl);
    await browser.open();
    const { sessionId } = await browser.send('Target.attachToTarget', { targetId: target.id, flatten: true });
    const cdp = (method, params = {}, session = sessionId) => browser.send(method, params, session ? { sessionId: session } : {});
    await cdp('Page.enable');
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
    return {
        name: 'chromium',
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
    v: 'v',
};
const FIREFOX_BUTTONS = { left: 0, back: 3 };

async function firefox(port, { media, screen, preload, prefs = {}, size = [1400, 900] }) {
    const profile = `/tmp/browsers-firefox-${port}`;
    fs.rmSync(profile, { recursive: true, force: true });
    fs.mkdirSync(profile, { recursive: true });
    const all = { 'browser.shell.checkDefaultBrowser': false, 'datareporting.policy.dataSubmissionEnabled': false };
    if (media || screen) {
        Object.assign(all, { 'media.navigator.permission.disabled': true, 'media.navigator.streams.fake': media !== 'pulse' && !screen,
            'media.autoplay.default': 0, 'media.autoplay.block-webaudio': false });
    }
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
        drag: (x1, y1, x2, y2) =>
            pointer([{ type: 'pointerMove', x: Math.round(x1), y: Math.round(y1) }, { type: 'pointerDown', button: 0 },
                { type: 'pointerMove', x: Math.round(x2), y: Math.round(y2), duration: 200 }, { type: 'pointerUp', button: 0 }]),
        async key(key, type) {
            const actions = [];
            if (type !== 'up') actions.push({ type: 'keyDown', value: FIREFOX_KEYS[key] });
            if (type !== 'down') actions.push({ type: 'keyUp', value: FIREFOX_KEYS[key] });
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
