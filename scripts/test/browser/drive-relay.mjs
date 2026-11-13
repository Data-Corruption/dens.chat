// The M20.1 check: bob in the browser named, in a call on his first
// install, sending by push to talk, and alice in Chromium in the call too.
// His laptop's install, with Keybind relay on in both, links to his first
// through the den's introduction, and its helper, a stand-in, hears a call
// is on. Its push to talk lights bob's ring on alice's page, its toggle
// mute mutes him once, and the ring goes out within half a second of the
// laptop's install stopping, and of it killed, which says nothing. Bob's
// settings list the laptop as linked; and
// the den's database and both of bob's installs' logs hold none of the
// container's addresses.
//
//   run.sh relay chromium|firefox
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, SETTINGS, checks, helper, launch, lib, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den } = process.env;
const PRELOAD = `() => {
    const Orig = window.RTCPeerConnection;
    if (!Orig || window.__pcs) return;
    window.__pcs = [];
    window.RTCPeerConnection = class extends Orig { constructor(...a) { super(...a); window.__pcs.push(this); } };
    const md = navigator.mediaDevices;
    window.__mics = [];
    if (md?.getUserMedia) {
        const gum = md.getUserMedia.bind(md);
        md.getUserMedia = (c) => gum(c).then((s) => (window.__mics.push(s), s));
    }
}`;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;
// rings says whether a page's call list rings a member, by name.
const rings = (s, name) => s.b.eval(`[...document.querySelectorAll('ul[aria-label="In the call"] li')].some((li) => li.textContent.includes(${JSON.stringify(name)}) && li.textContent.includes('speaking'))`);
const muted = () => B.b.eval(`!!document.querySelector('button[aria-label="Unmute"]')`);
// silence has a page send nothing, so only the den's word of push to talk
// rings it on the other's.
const silence = (s) => s.b.eval(`(async () => {
    window.__mics.forEach((m) => m.getAudioTracks().forEach((t) => (t.enabled = false)));
    await window.__pcs.at(-1).getTransceivers()[0].sender.replaceTrack(null);
    return true;
})()`);
let h = null;

try {
    A.b = await launch('chromium', 9545, { media: true, preload: PRELOAD });
    B.b = await launch(which, 9546, { media: which === 'firefox' ? 'pulse' : true, preload: PRELOAD, hover: true });
    for (const s of [A, B]) {
        await pair(s.b, s);
        if (s === B) await s.b.eval(`localStorage.setItem('DENS_VOICE', JSON.stringify({ mode: 'ptt' })), true`);
        await s.b.go(`http://localhost:${s.port}/den/${den}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), `${s.name}'s voice channel`);
        await s.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    }
    for (const s of [A, B]) {
        await waitFor(() => s.b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), `${s.name} in the call`, 30000);
    }
    await waitFor(async () => (await A.b.eval(`document.querySelectorAll('ul[aria-label="In the call"] li').length`)) === 2, 'both in the call');
    for (const s of [A, B]) await silence(s);

    step("the laptop's install linking to bob's first, which holds the call");
    h = helper('m2e');
    const linkedIn = Date.now();
    const told = await waitFor(() => h.state()?.call, "the laptop's helper hearing a call is on", 30000).catch(() => false);
    report(told === true, `the laptop's helper hears a call is on, ${Date.now() - linkedIn} ms after it connected, once its install linked to bob's first`);
    h.send('source', { covers: ['ptt', 'toggleMute'] });

    step("the laptop's push to talk");
    await sleep(1500);
    const before = await rings(A, 'Bob');
    h.send('press', { action: 'ptt', down: true });
    const t0 = Date.now();
    const lit = await waitFor(() => rings(A, 'Bob'), "bob's ring on alice's page", 5000).catch(() => false);
    const litAfter = Date.now() - t0;
    await sleep(1000);
    const held = await rings(A, 'Bob');
    h.send('press', { action: 'ptt', down: false });
    const out = await waitFor(async () => !(await rings(A, 'Bob')), "bob's ring going out", 5000).catch(() => false);
    report(!before && lit === true && held && out === true,
        `the laptop's push to talk lights bob's ring on alice's page ${litAfter} ms after its press (${lit}), held (${held}), and out as it lets go (${out})`);
    await A.b.shot(`${HOME}/shot-relay-${which}-ring.png`).catch(() => {});

    step("the laptop's toggle mute");
    h.send('press', { action: 'toggleMute', down: true });
    h.send('press', { action: 'toggleMute', down: false });
    const mutedOnce = await waitFor(muted, 'bob muted', 5000).catch(() => false);
    await sleep(1000);
    const stillMuted = await muted();
    h.send('press', { action: 'toggleMute', down: true });
    h.send('press', { action: 'toggleMute', down: false });
    const unmuted = await waitFor(async () => !(await muted()), 'bob unmuted', 5000).catch(() => false);
    report(mutedOnce === true && stillMuted && unmuted === true, `the laptop's toggle mute mutes bob once (${mutedOnce}, still muted a second on: ${stillMuted}), and again unmutes him (${unmuted})`);

    step("bob's settings");
    await openSettings(B.b, 'Relay');
    const listed = await waitFor(() => B.b.eval(`(() => {
        const sw = ${SETTINGS}.querySelector('#relay-keys');
        const rows = [...${SETTINGS}.querySelectorAll('[data-device]')].map((li) => li.textContent);
        return sw && sw.checked && rows.length === 1 && rows[0].includes('Linked') ? rows[0] : '';
    })()`), "the laptop in bob's Relay settings, linked", 15000).catch(() => '');
    report(!!listed, `bob's Relay settings have Keybind relay on, and list the laptop as linked: ${JSON.stringify(listed)}`);
    await B.b.shot(`${HOME}/shot-relay-${which}-settings.png`).catch(() => {});
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);

    // The laptop's install stopping, as a service stops, which closes the
    // link at once; then, started again and linked again, killed, which
    // closes nothing, so bob's install lets go 300 ms after its last word.
    for (const [how, signal] of [['stopped', 'SIGTERM'], ['killed', 'SIGKILL']]) {
        step(`the laptop's install ${how} while it holds push to talk`);
        if (signal === 'SIGKILL') {
            lib('start', 'm2e', '--port', '18490');
            h = helper('m2e');
            await waitFor(() => h.state()?.call, "the laptop's helper hearing a call is on again", 30000);
            h.send('source', { covers: ['ptt', 'toggleMute'] });
        }
        h.send('press', { action: 'ptt', down: true });
        const relit = await waitFor(() => rings(A, 'Bob'), "bob's ring again", 5000).catch(() => false);
        await sleep(500);
        const pid = fs.readFileSync(`${HOME}/m2e.pid`, 'utf8').trim();
        const stopped = Date.now();
        process.kill(Number(pid), signal);
        let goneAfter = -1;
        while (Date.now() - stopped < 5000) {
            if (!(await rings(A, 'Bob'))) {
                goneAfter = Date.now() - stopped;
                break;
            }
            await sleep(20);
        }
        report(relit === true && goneAfter >= 0 && goneAfter <= 500, `bob's ring goes out ${goneAfter} ms after the laptop's install was ${how}, within half a second`);
        h.close();
        h = null;
    }

    step('addresses');
    const addrs = execFileSync('hostname', ['-I']).toString().trim().split(/\s+/).filter((a) => /^\d+\.\d+\.\d+\.\d+$/.test(a));
    const data = `${HOME}/.local/share/dens-dev`;
    const places = [`${data}/m2a/data/db`, `${data}/m2b/data/logs`, `${data}/m2e/data/logs`, `${HOME}/m2b.log`, `${HOME}/m2e.log`];
    const missing = places.filter((p) => !fs.existsSync(p));
    const found = [];
    for (const a of missing.length ? [] : addrs) {
        try {
            found.push(...execFileSync('grep', ['-rlaF', a, ...places]).toString().trim().split('\n').filter(Boolean));
        } catch (e) {
            if (e.status !== 1) throw e;
        }
    }
    // The logs searched are the ones that note the link.
    let noted = false;
    try {
        noted = execFileSync('grep', ['-rlF', 'linked in', `${data}/m2b/data/logs`, `${HOME}/m2b.log`]).toString().trim() !== '';
    } catch (e) {
        if (e.status !== 1) throw e;
    }
    report(addrs.length > 0 && missing.length === 0 && found.length === 0 && noted,
        `the den's database and bob's installs' logs, which note the link (${noted}), hold none of ${JSON.stringify(addrs)}${missing.length ? `; missing: ${missing.join(', ')}` : ''}${found.length ? `: found in ${found.join(', ')}` : ''}`);
} catch (e) {
    c.fail(e);
    for (const s of [A, B]) await s.b?.shot(`${HOME}/shot-relay-${which}-failed-${s.name}.png`).catch(() => {});
} finally {
    h?.close();
    for (const s of [A, B]) await Promise.race([s.b?.close(), sleep(5000)]);
    c.finish();
}
