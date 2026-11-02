// Desk in the Android walk's call (M13.4): Chromium in the browser checks'
// container as the den's owner, its fake microphone speaking, in Lounge,
// answering the walk in the Android container over HTTP, on PORT at this
// container's address, with what desk's page shows of the call:
//
//   GET  /call    who is in it, by name, and whether each rings or is muted
//   POST /share   shares desk's screen, its fake capture
//   POST /rejoin  leaves the call and joins it again, for its sounds (M14.2)
//   POST /mute    mutes desk, so its ring lights for its sound alone (M14.3)
//   POST /sound?name=NAME  plays one of the den's sounds from desk's soundboard
//   GET  /board   what desk's page receives on the call's soundboard
//   POST /quit    hangs up, and ends
//
//   node desk.mjs PORT [SECONDS]
//
// in dens-browsers' browser directory, beside browsers.mjs. It ends by
// itself after SECONDS, 600 unless given.
import http from 'node:http';
import { setTimeout as sleep } from 'node:timers/promises';
import { launch, token, waitFor } from './browsers.mjs';

const [, , portArg, secondsArg] = process.argv;
const port = Number(portArg);
// Each peer connection's tracks, by the stream they come in (M14.3).
const RECORD = `() => {
    window.__tracks = [];
    const Orig = window.RTCPeerConnection;
    window.RTCPeerConnection = class extends Orig {
        constructor(...args) {
            super(...args);
            this.addEventListener('track', (e) => window.__tracks.push({ pc: this, stream: e.streams[0]?.id, track: e.track }));
        }
    };
}`;
const page = await launch('chromium', 9446, { media: true, screen: true, preload: RECORD });
const quit = async (status) => {
    await page.close();
    process.exit(status);
};
setTimeout(() => quit(0), Number(secondsArg || 600) * 1000).unref();

try {
    await page.go(`http://127.0.0.1:18584/#token=${token('sden')}`);
    await waitFor(() => page.eval(`!!document.querySelector('a[href="/settings"]')`), 'desk paired');
    await page.eval(`(async () => {
        const den = (await (await fetch('/api/dens')).json()).dens[0].den_id;
        history.pushState(null, '', '/den/' + den);
        dispatchEvent(new PopStateEvent('popstate'));
        return true;
    })()`);
    await waitFor(() => page.eval(`!!document.querySelector('button[title^="Join the call"]')`), "desk's Lounge");
    await join();
} catch (e) {
    console.error(e.message);
    await quit(1);
}

// join joins Lounge's call, from a click of desk's own.
async function join() {
    await page.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    await waitFor(() => page.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), 'desk in the call', 30000);
}

// CALL is the call as desk's list shows it: each member's name, whether
// they ring, as for push to talk held, and whether they're muted, by marks
// any of Dens's pages since M11 make, as desk's den, made once, may still
// serve an older one.
const CALL = `[...document.querySelectorAll('ul[aria-label="In the call"] li')].map((li) => ({
    name: li.querySelector('button span.truncate')?.textContent.trim() || '',
    rings: li.textContent.includes('speaking'),
    muted: !!li.querySelector('[aria-label="muted"]'),
}))`;
const SHARE = `(() => { const b = document.querySelector('button[aria-label="Share your screen"]'); if (!b) return 'missing';
    if (b.disabled) return 'disabled: ' + b.title; b.click(); return 'clicked'; })()`;
// BOARD is what desk's page receives on the call's soundboard: its packets.
const BOARD = `(async () => {
    const t = window.__tracks.filter((x) => x.stream === 'soundboard' && x.pc.connectionState !== 'closed').at(-1);
    if (!t) return { packets: 0 };
    const stats = [...(await t.pc.getStats(t.track)).values()].find((x) => x.type === 'inbound-rtp') || {};
    return { packets: stats.packetsReceived || 0 };
})()`;
// SHAREABLE is desk's share button allowing a share, as it doesn't until
// desk's page knows the den's limits; the click itself goes as from a
// click of desk's own, which the browser's picker needs.
const SHAREABLE = `(() => { const b = document.querySelector('button[aria-label="Share your screen"]'); return !!b && !b.disabled; })()`;

http.createServer(async (req, res) => {
    const answer = (status, body) => {
        res.writeHead(status, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(body));
    };
    try {
        if (req.method === 'GET' && req.url === '/call') return answer(200, { members: await page.eval(CALL) });
        if (req.method === 'POST' && req.url === '/share') {
            await waitFor(() => page.eval(SHAREABLE), "desk's share button", 15000).catch(() => {});
            return answer(200, { share: await page.eval(SHARE, true) });
        }
        if (req.method === 'POST' && req.url === '/rejoin') {
            const LEAVE = `document.querySelector('button[aria-label="Leave the call"]')`;
            await page.eval(`${LEAVE}.click(), true`);
            await waitFor(async () => !(await page.eval(`!!${LEAVE}`)), 'desk out of the call', 10000);
            await sleep(1500);
            await join();
            return answer(200, { rejoined: true });
        }
        if (req.method === 'POST' && req.url === '/mute') {
            const MUTE = `document.querySelector('button[aria-label="Mute"]')`;
            await page.eval(`(${MUTE}?.click(), true)`);
            await waitFor(() => page.eval(`!!document.querySelector('button[aria-label="Unmute"]')`), 'desk muted', 5000);
            return answer(200, { muted: true });
        }
        if (req.method === 'POST' && req.url.startsWith('/sound?')) {
            const name = new URL(req.url, 'http://desk').searchParams.get('name');
            await page.eval(`document.querySelector('[data-soundboard-button=block]').click(), true`);
            const TILE = `[...document.querySelectorAll('[data-soundboard] [data-sound-tile]')].find((t) => t.querySelector('span').textContent === ${JSON.stringify(name)})`;
            await waitFor(() => page.eval(`!!${TILE}`), `desk's tile ${name}`, 5000);
            await page.eval(`(${TILE}.click(), true)`, true);
            await page.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
            return answer(200, { played: name });
        }
        if (req.method === 'GET' && req.url === '/board') return answer(200, await page.eval(BOARD));
        if (req.method === 'POST' && req.url === '/quit') {
            answer(200, { left: await page.eval(`(() => { const b = document.querySelector('button[aria-label="Leave the call"]'); b?.click(); return !!b; })()`) });
            return setTimeout(() => quit(0), 500);
        }
        answer(404, { error: 'no such thing' });
    } catch (e) {
        answer(500, { error: e.message });
    }
}).listen(port, '0.0.0.0', () => console.log(`desk in the call, answering on ${port}`));
