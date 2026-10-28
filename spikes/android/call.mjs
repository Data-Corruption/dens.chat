// A call between the spike's WebView and a desktop's Chromium, each
// joining the den's voice channel with a fake microphone that speaks, and
// each saying every 5 seconds what it sends and hears: what the calls
// check sees, for the phone.
//
//   node call.mjs desk SECONDS    in dens-browsers' browser directory: Chromium, as desk
//   node call.mjs phone SECONDS   in the spike's container: the WebView, over adb
//   node call.mjs watch SECONDS   the WebView, which the member joins by hand
//
// The PRELOAD and STATS here are the calls check's (drive-calls.mjs).
import { setTimeout as sleep } from 'node:timers/promises';

const PRELOAD = `() => {
    const Orig = window.RTCPeerConnection;
    if (!Orig || window.__pcs) return;
    window.__pcs = [];
    window.RTCPeerConnection = class extends Orig {
        constructor(...args) { super(...args); window.__pcs.push(this); }
    };
}`;

const STATS = `(async () => {
    const members = [...document.querySelectorAll('ul[aria-label="In the call"] li')].map((e) => e.textContent.trim());
    const pc = (window.__pcs || []).at(-1);
    if (!pc) return { members, pc: false };
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
            if (r) pair = r.protocol + ' ' + r.candidateType + ' ' + r.address + ':' + r.port;
        }
    });
    return { members, state: pc.connectionState, rx, tx, lost, jitterMs: Math.round(jitter * 1000), level: Math.round(level * 1000) / 1000, pair };
})()`;

const JOIN = `(async () => {
    const den = (await (await fetch('/api/dens')).json()).dens[0].den_id;
    if (!location.pathname.startsWith('/den/')) { history.pushState(null, '', '/den/' + den); dispatchEvent(new PopStateEvent('popstate')); }
    for (let i = 0; i < 40 && !document.querySelector('button[title^="Join the call"]'); i++) await new Promise((r) => setTimeout(r, 250));
    const b = document.querySelector('button[title^="Join the call"]');
    if (!b) return 'no join button';
    b.click();
    return 'joined ' + den;
})()`;

const [, , side, secondsArg] = process.argv;
const seconds = Number(secondsArg || 60);
let page;
if (side === 'desk') {
    const { launch, token, waitFor } = await import('./browsers.mjs');
    page = await launch('chromium', 9445, { media: true, preload: PRELOAD });
    await page.go(`http://127.0.0.1:18584/#token=${token('sden')}`);
    await waitFor(() => page.eval(`!!document.querySelector('a[href="/settings"]')`), 'desk paired');
} else {
    const { webview } = await import('./drive.mjs');
    page = await webview(process.env.ANDROID_SERIAL || 'emulator-5554');
    await page.cdp('Page.addScriptToEvaluateOnNewDocument', { source: `(${PRELOAD})()` });
    await page.cdp('Page.reload');
    await sleep(4000);
}
if (side !== 'watch') console.log(side, await page.eval(JOIN, true));
const start = Date.now();
while (Date.now() - start < seconds * 1000) {
    await sleep(5000);
    const s = await page.eval(STATS).catch((e) => ({ error: e.message }));
    console.log(side, `${Math.round((Date.now() - start) / 1000)}s`, JSON.stringify(s));
}
if (side === 'desk') await page.close();
else page.close();
