// Runs the RNNoise spike's page headless in the Windows browsers, from WSL,
// and prints what each one measured. The page posts its results to the
// spike's server; this launches each browser on the page, with a throwaway
// profile and a fake microphone, waits for the results, and closes it
// through its debugging protocol, never by killing processes, since the
// desktop user's own browsers may be running.
//
// Usage, with the server running (spikes/rnnoise/serve):
//   tools/node spikes/rnnoise/drive.mjs [chrome] [edge] [firefox] [waterfox] [--strict]
// --strict loads the page under today's CSP, which should refuse to compile.

import { execFileSync, spawn } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';

const SERVER = 'http://127.0.0.1:38590';
const BROWSERS = {
    chrome: { exe: '/mnt/c/Program Files/Google/Chrome/Application/chrome.exe', kind: 'cdp', port: 9351 },
    edge: { exe: '/mnt/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', kind: 'cdp', port: 9352 },
    firefox: { exe: '/mnt/c/Program Files/Mozilla Firefox/firefox.exe', kind: 'bidi', port: 9353 },
    waterfox: { exe: '/mnt/c/Program Files/Waterfox/waterfox.exe', kind: 'bidi', port: 9354 },
};
const FIREFOX_PREFS = {
    'media.navigator.streams.fake': true,
    'media.navigator.permission.disabled': true,
    'media.autoplay.default': 0,
    'browser.shell.checkDefaultBrowser': false,
    'browser.aboutwelcome.enabled': false,
    'browser.startup.homepage_override.mstone': '"ignore"',
    'datareporting.policy.dataSubmissionEnabled': false,
    'toolkit.telemetry.reportingpolicy.firstRun': false,
};

const args = process.argv.slice(2);
const strict = args.includes('--strict');
// --listen has the first browser save recordings to listen to.
let listen = args.includes('--listen');
const names = args.filter((a) => !a.startsWith('--'));
const winTemp = execFileSync('powershell.exe', ['-NoProfile', '-Command', '[IO.Path]::GetTempPath()']).toString().trim();
const wsl = (path) => execFileSync('wslpath', ['-u', path]).toString().trim();

async function results(label, timeoutMs) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        const res = await fetch(`${SERVER}/results?label=${encodeURIComponent(label)}`).catch(() => null);
        if (res?.ok) return res.json();
        await sleep(1000);
    }
    return null;
}

// message sends one command over a debugging socket and waits for its reply.
function message(url, commands) {
    return new Promise((resolve, reject) => {
        const ws = new WebSocket(url);
        let id = 0;
        const next = () => {
            if (id === commands.length) {
                ws.close();
                resolve();
                return;
            }
            const [method, params] = commands[id];
            ws.send(JSON.stringify({ id: ++id, method, params: params || {} }));
        };
        ws.onopen = next;
        ws.onmessage = (e) => {
            const reply = JSON.parse(e.data);
            if (reply.id === id) next();
        };
        ws.onerror = () => reject(new Error(`can't reach ${url}`));
        ws.onclose = () => resolve();
    });
}

async function close(browser) {
    if (browser.kind === 'cdp') {
        const version = await (await fetch(`http://127.0.0.1:${browser.port}/json/version`)).json();
        await message(version.webSocketDebuggerUrl, [['Browser.close']]);
    } else {
        await message(`ws://127.0.0.1:${browser.port}/session`, [['session.new', { capabilities: {} }], ['browser.close']]);
    }
}

async function run(name) {
    const browser = BROWSERS[name];
    const label = `${name}${strict ? '-strict' : ''}-${Date.now()}`;
    const profile = `${winTemp}rnnoise-spike-${name}`;
    const local = wsl(profile);
    rmSync(local, { recursive: true, force: true });
    mkdirSync(local, { recursive: true });
    const url = `${SERVER}/${strict ? 'strict/' : ''}?label=${label}${listen ? '&listen=1' : ''}`;
    listen = false;
    let argv;
    if (browser.kind === 'cdp') {
        argv = ['--headless=new', `--remote-debugging-port=${browser.port}`, `--user-data-dir=${profile}`,
            '--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream',
            '--autoplay-policy=no-user-gesture-required', '--no-first-run', '--no-default-browser-check', url];
    } else {
        writeFileSync(`${local}/user.js`, Object.entries(FIREFOX_PREFS).map(([k, v]) => `user_pref("${k}", ${v});`).join('\n') + '\n');
        argv = ['--headless', `--remote-debugging-port=${browser.port}`, '--profile', profile, '--no-remote', url];
    }
    const child = spawn(browser.exe, argv, { stdio: 'ignore' });
    const exited = new Promise((resolve) => child.on('exit', resolve));
    const got = await results(label, strict ? 60_000 : 10 * 60_000);
    try {
        await close(browser);
    } catch (e) {
        console.error(`${name}: closing: ${e.message}`);
    }
    await Promise.race([exited, sleep(20_000)]);
    for (let i = 0; i < 10; i++) {
        try {
            rmSync(local, { recursive: true, force: true });
            break;
        } catch {
            await sleep(1000);
        }
    }
    return got;
}

for (const name of names.length ? names : Object.keys(BROWSERS)) {
    const got = await run(name);
    console.log(`== ${name}${strict ? ' (strict CSP)' : ''}`);
    if (!got) {
        console.log('no results');
        continue;
    }
    console.log(got.userAgent);
    for (const s of got.steps) console.log(JSON.stringify(s));
}
