// A call in the den's voice channel, Lounge, between the app's page and a
// desktop's (M12): each side joins, and every 5 seconds prints a line of
// JSON saying when, what it sends and hears, and what its page does:
// whether it shows, whether its browser froze or thawed it, its audio
// contexts and the elements that play the members.
//
//   node call.mjs desk SECONDS [quiet FROM TO]
//       in dens-browsers' browser directory, beside browsers.mjs: Chromium
//       as desk, the den's owner, with a fake microphone that speaks, muted
//       from FROM to TO seconds into the call
//   node call.mjs phone SECONDS [SERIAL]
//       in the Android container: the app's page, over adb
//   node call.mjs watch SECONDS [SERIAL]
//       the app's page, in a call its member joined by hand
//   node call.mjs judge PHONE DESK OFF ON
//       the Android e2e's verdict on the two sides' lines, the phone's
//       screen off from OFF to ON, in milliseconds since the epoch
//
// It finds the page's peer connection, elements and contexts in its heap,
// so it can start while a call goes on.
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';

const [, , side, secondsArg, ...rest] = process.argv;
const seconds = Number(secondsArg || 60);

// judge says whether the phone's page held its call through the screen
// being off, in the e2e's call: the desk quiet for most of it, which is
// when Chromium would freeze a page it hadn't been told shows, then
// speaking while the screen is still off.
if (side === 'judge') {
    const lines = (file) => fs.readFileSync(file, 'utf8').split('\n').filter((l) => l.startsWith('{')).map((l) => JSON.parse(l)).filter((x) => x.at);
    const [phone, desk] = [lines(secondsArg), lines(rest[0])];
    const [off, on] = [Number(rest[1]), Number(rest[2])];
    const within = (xs, from, to) => xs.filter((x) => x.at >= from && x.at <= to);
    const p = within(phone, off + 5000, on);
    const d = within(desk, off + 5000, on);
    let ok = true;
    const report = (good, what) => {
        ok &&= !!good;
        console.log(`${good ? 'OK  ' : 'BAD '} ${what}`);
    };
    const rising = (xs, k) => xs.every((x, i) => i === 0 || x[k] > xs[i - 1][k]);
    report(p.length >= 20, `the phone's page sampled ${p.length} times off screen`);
    report(p.every((x) => x.visible === 'visible' && x.frozen === 0 && x.state === 'connected'),
        `held off screen: shown, never frozen, connected (${[...new Set(p.map((x) => `${x.visible}/${x.frozen}/${x.state}`))].join(', ')})`);
    report(rising(p, 'tx'), `sending throughout: ${p.map((x) => x.tx).join(' ')}`);
    report(p.at(-1)?.rx > p[0]?.rx && rising(d, 'rx'), `each side receiving throughout: phone ${p[0]?.rx}→${p.at(-1)?.rx}, desk ${d[0]?.rx}→${d.at(-1)?.rx}`);
    report(p.every((x) => x.autoplay === 'true' && x.playing?.length > 0 && x.playing.every((e) => e === 'muted') && x.contexts?.every((c) => c.startsWith('running'))),
        `the voices through the call's context, their elements muted and playing: ${[...new Set(p.map((x) => `${x.playing?.join('+')} ${x.contexts?.join('+')}`))].join(', ')}`);
    const spoke = d.find((x, i) => i > 0 && d[i - 1].muted && !x.muted);
    const heard = spoke && within(phone, spoke.at, on).find((x) => x.speaking > 0);
    report(heard, `the phone heard the desk off screen once it spoke again${spoke ? `, ${Math.round((on - spoke.at) / 1000)} s before the screen came on` : ''}`);
    process.exit(ok ? 0 : 1);
}

// The page's objects of a kind, by their prototype's name, and fn's value
// over them, as an array.
async function over(page, proto, fn) {
    const group = 'call';
    try {
        const { result } = await page.cdp('Runtime.evaluate', { expression: `${proto}.prototype`, objectGroup: group });
        const { objects } = await page.cdp('Runtime.queryObjects', { prototypeObjectId: result.objectId, objectGroup: group });
        const r = await page.cdp('Runtime.callFunctionOn', { objectId: objects.objectId, functionDeclaration: fn, awaitPromise: true, returnByValue: true });
        if (r.exceptionDetails) throw new Error(`${proto}: ${r.exceptionDetails.exception?.description || r.exceptionDetails.text}`);
        return r.result.value;
    } finally {
        await page.cdp('Runtime.releaseObjectGroup', { objectGroup: group }).catch(() => {});
    }
}

const PEER = `async function () {
    const pc = this.find((p) => p.connectionState !== 'closed');
    if (!pc) return null;
    const stats = await pc.getStats();
    let rx = 0, tx = 0, level = 0, lost = 0, jitter = 0, pair = '';
    stats.forEach((s) => {
        if (s.type === 'inbound-rtp' && s.kind === 'audio') {
            rx += s.packetsReceived || 0; lost += s.packetsLost || 0; jitter = Math.max(jitter, s.jitter || 0);
            level = Math.max(level, s.audioLevel || 0);
        }
        if (s.type === 'outbound-rtp' && s.kind === 'audio') tx += s.packetsSent || 0;
        if (s.type === 'candidate-pair' && s.state === 'succeeded' && (s.nominated || s.selected)) {
            const r = stats.get(s.remoteCandidateId);
            if (r) pair = r.protocol + ' ' + r.candidateType;
        }
    });
    return { state: pc.connectionState, rx, tx, lost, jitterMs: Math.round(jitter * 1000), level: Math.round(level * 1000) / 1000, pair };
}`;
// The heap holds the elements' own prototypes too, whose srcObject throws.
const ELEMENTS = `function () {
    const playing = (el) => { try { return !!el.srcObject; } catch { return false; } };
    return this.filter(playing).map((el) => (el.paused ? 'paused' : el.muted ? 'muted' : 'playing'));
}`;
const CONTEXTS = `function () {
    return this.map((c) => c.state + ' ' + c.sampleRate);
}`;
// What the page says of itself, and the freezes and thaws counted since
// the driver started watching it.
const PAGE = `(() => {
    if (!window.__callWatch) {
        const w = (window.__callWatch = { frozen: 0, resumed: 0 });
        document.addEventListener('freeze', () => w.frozen++);
        document.addEventListener('resume', () => w.resumed++);
    }
    return {
        members: [...document.querySelectorAll('ul[aria-label="In the call"] li')].map((e) => e.textContent.trim()),
        speaking: [...document.querySelectorAll('ul[aria-label="In the call"] li')].filter((e) => e.textContent.includes('speaking')).length,
        muted: !!document.querySelector('button[aria-label="Unmute"]'),
        visible: document.visibilityState,
        autoplay: document.getElementById('app')?.dataset.autoplay,
        ...window.__callWatch,
    };
})()`;

async function sample(page) {
    const [page_, peer, playing, contexts] = await Promise.all([
        page.eval(PAGE), over(page, 'RTCPeerConnection', PEER), over(page, 'HTMLMediaElement', ELEMENTS), over(page, 'AudioContext', CONTEXTS),
    ]);
    return { ...page_, ...(peer || { state: 'none' }), playing, contexts };
}

// Joining is two steps: the click's own, which asks for the microphone, has
// the WebView collect a promise the driver awaits across it.
const DEN = `(async () => {
    const den = (await (await fetch('/api/dens')).json()).dens[0].den_id;
    if (!location.pathname.startsWith('/den/')) { history.pushState(null, '', '/den/' + den); dispatchEvent(new PopStateEvent('popstate')); }
    for (let i = 0; i < 40 && !document.querySelector('button[title^="Join the call"]'); i++) await new Promise((r) => setTimeout(r, 250));
    return !!document.querySelector('button[title^="Join the call"]');
})()`;
const JOIN = `(() => { const b = document.querySelector('button[title^="Join the call"]'); b?.click(); return !!b; })()`;
const MUTE = (on) => `(() => { const b = document.querySelector('button[aria-label="${on ? 'Mute' : 'Unmute'}"]'); b?.click(); return !!b; })()`;
const LEAVE = `(() => { const b = document.querySelector('button[aria-label="Leave the call"]'); b?.click(); return !!b; })()`;

let page;
let close;
let quiet = null;
if (side === 'desk') {
    const { launch, token, waitFor } = await import('./browsers.mjs');
    page = await launch('chromium', 9445, { media: true });
    close = () => page.close();
    await page.go(`http://127.0.0.1:18584/#token=${token('sden')}`);
    await waitFor(() => page.eval(`!!document.querySelector('a[href="/settings"]')`), 'desk paired');
    if (rest[0] === 'quiet') quiet = { from: Number(rest[1]), to: Number(rest[2]) };
} else if (side === 'phone' || side === 'watch') {
    const { webview } = await import('./drive.mjs');
    page = await webview(rest[0] || process.env.ANDROID_SERIAL || 'emulator-5554');
    close = () => page.close();
} else {
    console.error('usage: call.mjs desk|phone|watch SECONDS');
    process.exit(2);
}
if (side !== 'watch') console.log(JSON.stringify({ side, joined: (await page.eval(DEN)) && (await page.eval(JOIN, true)) }));
const start = Date.now();
let muted = false;
while (Date.now() - start < seconds * 1000) {
    await sleep(5000);
    const t = Math.round((Date.now() - start) / 1000);
    if (quiet) {
        const want = t >= quiet.from && t < quiet.to;
        if (want !== muted && (await page.eval(MUTE(want), true))) muted = want;
    }
    const s = await sample(page).catch((e) => ({ error: e.message }));
    console.log(JSON.stringify({ side, t, at: Date.now(), ...s }));
}
if (side !== 'watch') console.log(JSON.stringify({ side, left: await page.eval(LEAVE, true) }));
await close();
