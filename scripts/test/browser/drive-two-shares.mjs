// The second M4.2 check: two shares in bob's player. Alice (Chromium)
// shares her fake screen; carol is the voice probe, sharing Pion's packets,
// which no decoder takes, so her tile waits: enough for the layout and the
// focus.
//
//   run.sh two-shares
import { spawn } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import fs from 'node:fs';
import { ALICE, BOB, CAROL, HOME, checks, launch, pair, waitFor } from './browsers.mjs';

const { DEN: den, LOUNGE: lounge } = process.env;
const PRELOAD = `() => {
    const Orig = window.RTCPeerConnection;
    if (!Orig || window.__pcs) return;
    window.__pcs = [];
    window.RTCPeerConnection = class extends Orig { constructor(...a) { super(...a); window.__pcs.push(this); } };
    const md = navigator.mediaDevices;
    if (md?.getDisplayMedia) {
        const gdm = md.getDisplayMedia.bind(md);
        md.getDisplayMedia = (o) => { window.__gdm = JSON.stringify(o); return gdm(o).then((s) => (window.__display = s, s)); };
    }
}`;
const c = checks();
const report = c.report;
const A = ALICE;
const B = BOB;
const PLAYER = `document.querySelector('section[aria-label="Screen shares"]')`;
const live = (s, name) => s.b.eval(`(() => { const li = [...document.querySelectorAll('ul[aria-label="In the call"] li')].find((x) => x.textContent.includes(${JSON.stringify(name)})); return !!li?.querySelector('button[aria-label*="screen"]'); })()`);
const watch = (s, name) => s.b.eval(`[...document.querySelectorAll('ul[aria-label="In the call"] li')].find((x) => x.textContent.includes(${JSON.stringify(name)})).querySelector('button[aria-label*="screen"]').click(), true`, true);
let probe;
try {
    A.b = await launch('chromium', 9433, { media: true, screen: true, preload: PRELOAD });
    B.b = await launch('firefox', 9434, { media: 'pulse', screen: true, preload: PRELOAD, size: [1400, 860] });
    await pair(A.b, A);
    await A.b.go(`http://localhost:${A.port}/den/${den}`);
    await waitFor(() => A.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), 'alice voice channel');
    await A.b.eval(`(async () => {
        const v = await (await fetch('/api/dens/${den}/state')).json();
        await fetch('/api/dens/${den}/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ call_limits: { ...v.call_limits, shares: 2, share_bitrate: 2000000 } }) });
        return true;
    })()`);
    const alice = JSON.parse(await A.b.eval(`(async () => JSON.stringify(await (await fetch('/api/dens/${den}/state')).json()))()`)).me.id;
    await pair(B.b, B);
    await B.b.go(`http://localhost:${B.port}/den/${den}`);
    await waitFor(() => B.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), 'bob voice channel');
    for (const s of [A, B]) await s.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    for (const s of [A, B]) await waitFor(() => s.b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), `${s.name} in the call`, 30000);

    // Carol joins as the voice probe, hears alice, and shares.
    const cookie = fs.readFileSync(`${HOME}/jar-${CAROL.instance}`, 'utf8').split('\n').map((l) => l.split('\t')).find((f) => f[5]?.startsWith('dens_session_'));
    probe = spawn(`${HOME}/voice-probe`, ['-test.run', '^TestVoiceProbe$', '-test.v', '-test.count=1'], { env: { ...process.env,
        DENS_VOICE_PROBE: `http://127.0.0.1:${CAROL.port}`, DENS_PROBE_COOKIE: `${cookie[5]}=${cookie[6]}`, DENS_PROBE_DEN: den, DENS_PROBE_CHANNEL: lounge,
        DENS_PROBE_HEAR: alice, DENS_PROBE_NETWORK: 'udp', DENS_PROBE_THEN: 'share', DENS_PROBE_STAY: '60' } });
    let probeOut = '';
    probe.stdout.on('data', (d) => { probeOut += d; });
    probe.stderr.on('data', (d) => { probeOut += d; });
    await waitFor(() => live(B, 'Carol'), "carol's LIVE mark", 60000).catch((e) => console.log(e.message, probeOut));

    // Alice shares too; bob watches both.
    await A.b.eval(`document.querySelector('button[aria-label="Share your screen"]').click(), true`, true);
    await waitFor(() => live(B, 'Alice'), "alice's LIVE mark", 15000);
    await watch(B, 'Alice');
    await waitFor(() => B.b.eval(`(() => { const v = ${PLAYER}?.querySelector('video'); return v && v.videoWidth > 0; })()`), "bob sees alice", 15000);
    await watch(B, 'Carol');
    await waitFor(() => B.b.eval(`${PLAYER}?.querySelectorAll('video').length === 2`), 'two tiles', 10000);
    const keyframes = await waitFor(() => probeOut.includes('the den asked for a keyframe') ? 'yes' : '', "carol's keyframe request", 15000).catch((e) => e.message);
    report(keyframes === 'yes', `carol's probe got a keyframe request as bob started watching: ${keyframes}`);
    const layout = await B.b.eval(`(() => { const g = ${PLAYER}.querySelector('.grid'); const r = ${PLAYER}.getBoundingClientRect(); return JSON.stringify({ cols: g.style.gridTemplateColumns, rows: g.style.gridTemplateRows, w: Math.round(r.width), h: Math.round(r.height), title: ${PLAYER}.querySelector('span').textContent }); })()`);
    const l = JSON.parse(layout);
    report(l.title === 'Watching 2 shares' && (l.w >= l.h * 1.2 ? l.cols.startsWith('repeat(2') : l.rows.startsWith('repeat(2')), `two shares lay out in the player: ${layout}`);
    await B.b.shot(`${HOME}/shot-two-shares-two.png`);

    // A click fills the player with one; the other goes out of sight; a
    // second click puts it back.
    await B.b.eval(`[...${PLAYER}.querySelectorAll('video')].find((v) => v.getAttribute('aria-label') === "Alice's screen").click(), true`);
    await sleep(300);
    const focused = await B.b.eval(`JSON.stringify({ title: ${PLAYER}.querySelector('span').textContent, hidden: ${PLAYER}.textContent.includes('1 more out of sight'), shown: [...${PLAYER}.querySelectorAll('video')].filter((v) => v.offsetParent).map((v) => v.getAttribute('aria-label')) })`);
    const f = JSON.parse(focused);
    report(f.title === 'Watching Alice' && f.hidden && f.shown.length === 1 && f.shown[0] === "Alice's screen", `a click fills the player with alice's share: ${focused}`);
    await B.b.shot(`${HOME}/shot-two-shares-focus.png`);
    await B.b.eval(`[...${PLAYER}.querySelectorAll('video')].find((v) => v.getAttribute('aria-label') === "Alice's screen").click(), true`);
    await sleep(300);
    const back = await B.b.eval(`[...${PLAYER}.querySelectorAll('video')].filter((v) => v.offsetParent).length`);
    report(back === 2, `a second click puts both back: ${back} showing`);

    // Bob shares too, while he watches: his own screen waits for him to ask.
    await B.b.eval(`(async () => { const v = await (await fetch('/api/dens/${den}/state')).json(); return true; })()`);
    await A.b.eval(`(async () => {
        const v = await (await fetch('/api/dens/${den}/state')).json();
        await fetch('/api/dens/${den}/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ call_limits: { ...v.call_limits, shares: 3 } }) });
        return true;
    })()`);
    await sleep(500);
    await B.b.eval(`document.querySelector('button[aria-label="Share your screen"]').click(), true`, true);
    await waitFor(() => B.b.eval(`!!document.querySelector('[aria-label="Nobody is watching yet"]')`), 'bob live', 15000);
    const noPreview = await B.b.eval(`${PLAYER}.querySelectorAll('video').length`);
    await B.b.eval(`document.querySelector('button[aria-label="Show your screen in the player"]').click(), true`);
    await sleep(500);
    const three = await B.b.eval(`JSON.stringify({ tiles: ${PLAYER}.querySelectorAll('video').length, title: ${PLAYER}.querySelector('span').textContent })`);
    report(noPreview === 2 && JSON.parse(three).tiles === 3 && JSON.parse(three).title === 'Watching 2 shares, and your screen',
        `bob's own share waits for him while he watches (${noPreview} tiles), then joins them: ${three}`);
    await B.b.shot(`${HOME}/shot-two-shares-three.png`);

    // The bar's ✕ stops watching both, hides bob's own, and the player closes.
    await B.b.eval(`${PLAYER}.querySelector('button[title="Close the player"]').click(), true`);
    await sleep(500);
    report(!(await B.b.eval(`!!${PLAYER}`)) && (await B.b.eval(`!!document.querySelector('[aria-label="Show your screen in the player"]')`)), "the bar's ✕ closes the player, and bob goes on sharing");
} catch (e) {
    c.fail(e);
    try {
        await B.b?.shot(`${HOME}/shot-two-shares-fail.png`);
    } catch {}
} finally {
    probe?.kill();
    for (const s of [A, B]) await s.b?.close().catch(() => {});
    c.finish();
}
