// The M20 check: bob in the browser named, in a call on his first
// install, sending by push to talk, and alice in Chromium in the call too.
// His laptop's install, with Keybind relay on in both, links to his first
// through the den's introduction, and its helper, a stand-in, hears a call
// is on. Its push to talk lights bob's ring on alice's page, its toggle
// mute mutes him once, and the ring goes out within half a second of the
// laptop's install stopping, and of it killed, which says nothing. Bob's
// settings list the laptop as linked. From M20.2 the laptop, started
// again with its own helper, the real one, shares the container's sound,
// a tone, and bob's first page listens from his settings: the worklet's
// own counts say the packets came, at the tone's level, and it never ran
// dry, in front, going behind another tab or there; the laptop's page
// shows the bar on its end; a second page of bob's first install takes
// over, the first told; and the laptop's Stop ends it, the second told.
// Then the den's database and both of bob's installs' logs hold none of
// the container's addresses.
//
//   run.sh relay chromium|firefox
import { execFileSync, spawn } from 'node:child_process';
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, SETTINGS, checks, helper, launch, lib, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den } = process.env;
// The browsers play into "speakers", and the laptop's helper reads the
// default output, the tone's: nothing they play is read again.
process.env.PULSE_SINK = 'speakers';
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
    // What Relay's player counts, each report as its worklet posts it.
    const Node = window.AudioWorkletNode;
    window.__relay = [];
    if (Node) {
        window.AudioWorkletNode = class extends Node {
            constructor(ctx, name, o) {
                super(ctx, name, o);
                if (name !== 'relay') return;
                const mine = [];
                window.__relay.push(mine);
                this.port.addEventListener('message', (e) => mine.push(e.data));
            }
        };
    }
}`;
const LAPTOP = { name: 'laptop', instance: 'm2e', port: 18490 };
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;
// The laptop's page, and a second page of bob's first install.
const C = {};
const D = {};
let real = null;

// hide puts another tab in front of a page's, and front brings it back.
let other = null;
async function hide(b) {
    if (b.name === 'chromium') {
        other = (await b.cdp('Target.createTarget', { url: 'about:blank' }, null)).targetId;
        await b.cdp('Target.activateTarget', { targetId: other }, null);
    } else {
        other = (await b.bidi('browsingContext.create', { type: 'tab' })).context;
        await b.bidi('browsingContext.activate', { context: other });
    }
    await waitFor(() => b.eval(`document.visibilityState === 'hidden'`), 'the tab hidden');
}
async function front(b) {
    if (b.name === 'chromium') {
        await b.cdp('Target.closeTarget', { targetId: other }, null);
        await b.cdp('Page.bringToFront');
    } else {
        await b.bidi('browsingContext.close', { context: other });
    }
    await waitFor(() => b.eval(`document.visibilityState === 'visible'`), 'the tab shown');
}
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
    const launched = Date.now();
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

    step("the laptop sharing the container's sound, through its own helper");
    lib('start', 'm2e', '--port', '18490');
    real = spawn(`${HOME}/dens-dev`, ['shortcuts'], { cwd: HOME, stdio: ['ignore', 'ignore', fs.openSync(`${HOME}/helper.log`, 'w')] });
    C.b = await launch('chromium', 9547, {});
    await pair(C.b, LAPTOP);
    await openSettings(B.b, 'Relay');
    // Bob's first install lists one device, the laptop.
    const listenButton = `${SETTINGS}.querySelector('[data-device] [data-listen]')`;
    const listenable = await waitFor(() => B.b.eval(`${listenButton}?.textContent.trim() === 'Listen'`), "Listen beside bob's laptop", 30000)
        .catch(() => false);
    report(listenable === true, "bob's Relay settings show Listen beside the laptop, which shares its sound");
    // Firefox, half a minute after it starts, holds a worker's messages for
    // some 80 ms, once, at work of its own: a member's has long been
    // running, so the listening waits that out.
    await sleep(Math.max(0, 45000 - (Date.now() - launched)));
    await B.b.eval(`${listenButton}.click(), true`, true);
    const counts = (b) => b.eval(`JSON.stringify((window.__relay.at(-1) || []).at(-1) || null)`).then(JSON.parse);
    const started = Date.now();
    const first = await waitFor(async () => {
        const r = await counts(B.b);
        return r?.playing && r.packets > 50 ? r : null;
    }, "bob's first page playing the laptop's sound", 30000).catch(() => null);
    report(!!first, `bob's first page plays the laptop's sound ${Date.now() - started} ms after Listen: ${JSON.stringify(first)}`);
    await B.b.shot(`${HOME}/shot-relay-${which}-listening.png`).catch(() => {});
    const bar = (b) => b.eval(`[...document.querySelectorAll('[data-relay-bar] [data-relay]')].map((r) => r.dataset.relay + ': ' + r.textContent.trim()).join(' | ')`);
    const barB = await waitFor(async () => ((await bar(B.b)).includes('playing') ? bar(B.b) : ''), "bob's bar", 10000).catch(() => '');
    report(barB.includes('Listening to') && barB.includes('Stop'), `bob's bar says he listens, with Stop: ${barB}`);

    // Ten seconds in front, then as the tab goes behind another and ten
    // seconds there: what came, at the tone's level (a sine at half of full
    // scale, -9.0 dB), never dry. The worklet reports every two seconds, so
    // each count is read once a report has what came before it.
    await sleep(2500);
    const settle = await counts(B.b);
    await sleep(10000);
    const shown = await counts(B.b);
    const tone = -9.03;
    report(shown && shown.packets > settle.packets + 1500 && shown.dry === settle.dry && Math.abs(shown.level - tone) < 1.5 && shown.lost === settle.lost,
        `in front, the worklet counts ${shown?.packets - settle?.packets} packets in 10 s, none lost (${shown?.lost - settle?.lost}), at ${shown?.level} dB, `
        + `never dry (${shown?.dry - settle?.dry}; ${settle?.dry} while it started), its buffer ${shown?.least} to ${shown?.depth} ms of ${shown?.target}`);
    await hide(B.b);
    await sleep(12500);
    const hidden = await counts(B.b);
    await front(B.b);
    report(hidden && hidden.packets > shown.packets + 1900 && hidden.dry === shown.dry && Math.abs(hidden.level - tone) < 1.5,
        `going behind another tab and there, ${hidden?.packets - shown?.packets} packets in 12 s at ${hidden?.level} dB, never dry (${hidden?.dry - shown?.dry}), `
        + `its buffer ${hidden?.least} to ${hidden?.depth} ms`);

    step("the laptop's end");
    const barC = await waitFor(async () => {
        const t = await bar(C.b);
        return t.includes('sharing') ? t : '';
    }, "the laptop's bar", 15000).catch(() => '');
    report(barC.includes("Sharing this computer's sound with") && barC.includes('Stop'), `the laptop's page says it shares its sound, with Stop: ${barC}`);
    await C.b.shot(`${HOME}/shot-relay-${which}-sharing.png`).catch(() => {});

    step("a second page of bob's first install takes over");
    D.b = await launch('chromium', 9548, { preload: PRELOAD });
    await pair(D.b, BOB);
    await openSettings(D.b, 'Relay');
    await waitFor(() => D.b.eval(`${listenButton}?.textContent.trim() === 'Listen'`), "Listen on the second page", 30000);
    await D.b.eval(`${listenButton}.click(), true`, true);
    const second = await waitFor(async () => {
        const r = await counts(D.b);
        return r?.playing && r.packets > 50 ? r : null;
    }, 'the second page playing', 30000).catch(() => null);
    const notice = await waitFor(() => B.b.eval(`document.querySelector('[data-relay-notice]')?.textContent.trim() || ''`), "the first page told", 10000)
        .catch(() => '');
    const stopped = await B.b.eval(`!document.querySelector('[data-relay="playing"]')`);
    report(!!second && notice.includes('took over') && stopped, `the second page plays (${!!second}), and the first stops, told: ${notice}`);

    step("the laptop's Stop");
    await C.b.eval(`document.querySelector('[data-relay="sharing"] button').click(), true`);
    const ended = await waitFor(() => D.b.eval(`document.querySelector('[data-relay-notice]')?.textContent.trim() || ''`), 'the second page told', 10000)
        .catch(() => '');
    const gone = await waitFor(async () => !(await bar(C.b)).includes('sharing'), "the laptop's bar gone", 10000).catch(() => false);
    report(ended.includes('stopped sharing') && gone === true, `the laptop's Stop ends it: the second page told "${ended}", the laptop's bar gone (${gone})`);

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
    real?.kill();
    for (const s of [A, B, C, D]) await Promise.race([s.b?.close(), sleep(5000)]);
    c.finish();
}
