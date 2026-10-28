// Drives the page in the Android app's WebView over adb, as the browser
// checks drive Chromium: the DevTools protocol on the WebView's socket,
// which adb forwards to a port here, through Node's own WebSocket. A
// development build's WebView takes it (M12). Runs in the Android
// container, beside adb.
//
// Usage: node drive.mjs [-s SERIAL] COMMAND [ARGS]
//   eval EXPR               the expression's value in the page, as JSON
//   api METHOD PATH [JSON]  the page's API, called from the page itself
//   shot FILE               a screenshot of the page, at the phone's size
//   go URL                  load an address in the WebView
//   tap X Y                 a tap, in the page's CSS pixels
//   type TEXT               text, as the keyboard types it
//
// ANDROID_APP names the app, chat.dens.Dens.dev unless it says otherwise.

import { execFileSync } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import fs from 'node:fs';

const ADB = '/opt/android/sdk/platform-tools/adb';
const PORT = 9222;
const APP = process.env.ANDROID_APP || 'chat.dens.Dens.dev';

class Socket {
    constructor(url) {
        this.ws = new WebSocket(url);
        this.id = 0;
        this.waiting = new Map();
        this.events = [];
        this.ws.onmessage = (e) => {
            const m = JSON.parse(e.data);
            if (m.id === undefined) return void this.events.push(m);
            const w = this.waiting.get(m.id);
            if (!w) return;
            this.waiting.delete(m.id);
            if (m.error) w.reject(new Error(JSON.stringify(m.error)));
            else w.resolve(m.result);
        };
    }

    open() {
        return new Promise((resolve, reject) => {
            this.ws.onopen = resolve;
            this.ws.onerror = reject;
        });
    }

    send(method, params = {}) {
        const id = ++this.id;
        this.ws.send(JSON.stringify({ id, method, params }));
        return new Promise((resolve, reject) => this.waiting.set(id, { resolve, reject }));
    }
}

export async function webview(serial) {
    const adb = (...args) => execFileSync(ADB, [...(serial ? ['-s', serial] : []), ...args], { encoding: 'utf8' }).trim();
    const pid = adb('shell', 'pidof', APP).split(/\s+/)[0];
    if (!pid) throw new Error(`${APP} is not running`);
    adb('forward', `tcp:${PORT}`, `localabstract:webview_devtools_remote_${pid}`);
    const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
    const target = list.find((t) => t.type === 'page' && JSON.parse(t.description || '{}').visible !== false) || list.find((t) => t.type === 'page');
    if (!target) throw new Error('no page in the WebView');
    const s = new Socket(target.webSocketDebuggerUrl);
    await s.open();
    await s.send('Page.enable');
    const cdp = (method, params) => s.send(method, params);
    return {
        cdp,
        events: s.events,
        url: target.url,
        async eval(expression, gesture = false) {
            const r = await cdp('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true, userGesture: gesture });
            if (r.exceptionDetails) throw new Error(r.exceptionDetails.text + ' ' + (r.exceptionDetails.exception?.description || ''));
            return r.result.value;
        },
        // api calls the page's own API from the page, so the WebView sends
        // its Origin and the session's cookie, as the page does.
        async api(method, path, body) {
            return this.eval(`fetch(${JSON.stringify(path)}, { method: ${JSON.stringify(method)},
                headers: { 'Content-Type': 'application/json' }, ${body ? `body: ${JSON.stringify(body)},` : ''} })
                .then(async (r) => ({ status: r.status, body: await r.text() }))`);
        },
        async go(url) {
            await cdp('Page.navigate', { url });
            await sleep(1500);
        },
        async shot(file) {
            const r = await cdp('Page.captureScreenshot', { format: 'png' });
            fs.writeFileSync(file, Buffer.from(r.data, 'base64'));
        },
        async tap(x, y) {
            await cdp('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
            await cdp('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
        },
        async type(text) {
            await cdp('Input.insertText', { text });
        },
        // attach gives a file input files from the device's filesystem,
        // which the WebView reads as the app.
        async attach(selector, files) {
            const { root } = await cdp('DOM.getDocument', {});
            const { nodeId } = await cdp('DOM.querySelector', { nodeId: root.nodeId, selector });
            await cdp('DOM.setFileInputFiles', { files, nodeId });
        },
        close() {
            s.ws.close();
        },
    };
}

if (import.meta.url === `file://${process.argv[1]}`) {
    let args = process.argv.slice(2);
    let serial = process.env.ANDROID_SERIAL;
    if (args[0] === '-s') {
        serial = args[1];
        args = args.slice(2);
    }
    const [command, ...rest] = args;
    const b = await webview(serial);
    try {
        switch (command) {
            case 'eval':
                console.log(JSON.stringify(await b.eval(rest[0], true), null, 2));
                break;
            case 'api': {
                const r = await b.api(rest[0], rest[1], rest[2]);
                console.log(r.status, r.body.slice(0, 4000));
                break;
            }
            case 'shot':
                await b.shot(rest[0]);
                break;
            case 'go':
                await b.go(rest[0]);
                break;
            case 'tap':
                await b.tap(Number(rest[0]), Number(rest[1]));
                break;
            case 'type':
                await b.type(rest[0]);
                break;
            default:
                console.error(`no command ${command}`);
                process.exitCode = 2;
        }
    } finally {
        b.close();
    }
}
