// The M2 and M3 checks: two members in a call, in the browsers named,
// alice's first. Each hears the other, at the channel's bitrate; speaking
// rings; volume; the settings' Voice section and RNNoise; how a member
// sends, by voice activity or push to talk; staff mute and disconnect; a
// call that rides out its member's service restarting and its UDP path
// breaking; deafen; and the page around the call: the composer, the
// member's panel, the themes, and the home page. From M16.2 the call is
// sealed end to end: each page seals its microphone and opens every
// member's track, so each hears the other only by opening their frames;
// the call's lock opens the members' codes; a third member joining and
// leaving, the voice probe as carol, changes both pages' keys; and a page
// denied a member's keys, by a development build's hook, receives their
// packets and hears nothing.
//
//   run.sh calls chromium|firefox chromium|firefox
import { execFileSync, spawn } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import fs from 'node:fs';
import { HOME, checks, launch, lib, token, waitFor } from './browsers.mjs';

const PRELOAD = `() => {
    const Orig = window.RTCPeerConnection;
    if (!Orig || window.__pcs) return;
    window.__pcs = [];
    window.RTCPeerConnection = class extends Orig {
        constructor(...args) { super(...args); window.__pcs.push(this); }
    };
    const Au = window.Audio;
    window.__audios = [];
    window.Audio = function (...args) { const el = new Au(...args); window.__audios.push(el); return el; };
    const md = navigator.mediaDevices;
    if (md?.getUserMedia) {
        const gum = md.getUserMedia.bind(md);
        window.__gum = [];
        md.getUserMedia = (c) => {
            window.__gum.push(JSON.stringify(c));
            const wait = window.__gumDelay || 0;
            return wait ? new Promise((r) => setTimeout(r, wait)).then(() => gum(c)) : gum(c);
        };
    }
    const AC = window.AudioContext;
    window.__acs = [];
    window.AudioContext = class extends AC {
        constructor(...args) { super(...args); window.__acs.push(this); }
    };
}`;

const STATS = `(async () => {
    const status = [...document.querySelectorAll('[role=status]')].map((e) => e.textContent.trim()).join(' | ');
    const members = [...document.querySelectorAll('ul[aria-label="In the call"] li')].map((e) => e.textContent.trim());
    const pc = (window.__pcs || []).at(-1);
    if (!pc) return JSON.stringify({ status, members, pc: false });
    const stats = await pc.getStats();
    let rx = 0, tx = 0, level = 0, pair = '';
    stats.forEach((s) => {
        if (s.type === 'inbound-rtp' && s.kind === 'audio') { rx += s.packetsReceived || 0; level = Math.max(level, s.audioLevel || 0); }
        if (s.type === 'outbound-rtp' && s.kind === 'audio') tx += s.packetsSent || 0;
        if (s.type === 'candidate-pair' && s.state === 'succeeded' && (s.nominated || s.selected)) {
            const r = stats.get(s.remoteCandidateId);
            if (r) pair = r.protocol + ' ' + r.candidateType + ' port ' + r.port;
        }
    });
    return JSON.stringify({ status, members, state: pc.connectionState, rx, tx, level: Math.round(level * 1000) / 1000, pair });
})()`;

// M3: speaking rings, RNNoise, staff mute and disconnect, and a call that
// rides out its member's service restarting.
const [, , aName, bName] = process.argv;
const { DEN: den, LOUNGE: loungeID } = process.env;
const sides = [
    { browser: aName, instance: 'm2a', port: 18484, debug: 9333 },
    { browser: bName, instance: 'm2b', port: 18486, debug: 9334 },
];
const [A, B] = sides;
const RX = `(async () => {
    const pc = (window.__pcs || []).at(-1);
    if (!pc) return -1;
    let rx = 0;
    (await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'audio') rx += s.packetsReceived || 0; });
    return rx;
})()`;
const TEXT = `document.body.innerText`;
const rx = async (s) => s.b.eval(RX);
const click = (s, selector) => s.b.eval(`(() => { const el = ${selector}; if (!el) return false; el.click(); return true; })()`);
const byText = (text) => `[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === ${JSON.stringify(text)})`;
const opened = [];
const c = checks();
const report = c.report;
async function flowing(s, what, ms = 2000) {
    const before = await rx(s);
    await sleep(ms);
    const after = await rx(s);
    return { moving: after > before, before, after, what };
}
try {
    for (const s of sides) {
        s.b = await launch(s.browser, s.debug, { media: s.browser === 'firefox' ? 'pulse' : true, preload: PRELOAD });
        opened.push(s.b);
        await s.b.go(`http://127.0.0.1:${s.port}/#token=${token(s.instance)}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('a[href="/settings"]')`), `${s.browser} paired`);
        await s.b.go(`http://127.0.0.1:${s.port}/den/${den}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), `${s.browser} voice channel`);
        await s.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`);
    }
    for (const s of sides) await waitFor(async () => (await s.b.eval(STATS)).includes('Connected'), `${s.browser} in the call`, 30000);
    await sleep(3000);
    for (const s of sides) {
        const st = JSON.parse(await s.b.eval(STATS));
        report(st.state === 'connected' && st.rx > 50 && st.tx > 50 && st.members.length === 2, `${s.browser} in the call: ${JSON.stringify(st)}`);
    }

    // Every member's encoder runs at 96 kbps: the first to join too, whose
    // re-offer Pion would build from their browser's answer. The
    // soundboard's section (M14.3) carries what the den sends, as a share's
    // sound does, so it's left out.
    const ENC = `(async () => {
        const pc = window.__pcs.at(-1);
        const voices = (pc.remoteDescription?.sdp || '').split('\\r\\nm=').slice(1).filter((m) => m.startsWith('audio') && !m.includes('a=msid:soundboard '));
        const fmtps = voices.map((m) => m.split('\\r\\n').find((l) => l.startsWith('a=fmtp:111')) || '');
        let target = 0;
        (await pc.getStats()).forEach((s) => { if (s.type === 'outbound-rtp' && s.kind === 'audio') target = s.targetBitrate || target; });
        return JSON.stringify({ sections: fmtps.length, asks: fmtps.length > 0 && fmtps.every((l) => l.includes('maxaveragebitrate=96000')), target });
    })()`;
    for (const s of sides) {
        const e = JSON.parse(await s.b.eval(ENC));
        report(e.sections === 2 && e.asks && (s.browser !== 'chromium' || e.target === 96000), `${s.browser} asked for 96 kbps in every section: ${JSON.stringify(e)}`);
    }
    const CONTEXTS0 = `JSON.stringify(window.__acs.map((c) => c.sampleRate + ' ' + c.state))`;
    // Speaking: each side's list rings the other while their fake
    // microphone sounds.
    for (const [s, other] of [[A, 'Bob'], [B, 'Alice']]) {
        const seen = await waitFor(() => s.b.eval(`[...document.querySelectorAll('ul[aria-label="In the call"] li')].some((li) => li.textContent.includes(${JSON.stringify(other)}) && li.textContent.includes('speaking'))`), `${s.browser} sees ${other} speaking`, 20000).catch((e) => e.message);
        report(seen === true, `${s.browser} rings ${other} while they speak${seen === true ? '' : ': ' + seen + ' contexts ' + await s.b.eval(CONTEXTS0)}`);
    }
    for (const s of sides) {
        const ring = await s.b.eval(`[...document.querySelectorAll('ul[aria-label="In the call"] span.ring-2')].map((e) => { const r = e.getBoundingClientRect(); return Math.round(r.width) + 'x' + Math.round(r.height); }).join(' ')`);
        // A ring stands 3px out from its avatar, so rows 6px or more apart
        // keep theirs from touching.
        const gaps = JSON.parse(await s.b.eval(`JSON.stringify([...document.querySelectorAll('ul[aria-label="In the call"] span.ring-2')].map((e) => e.getBoundingClientRect()).map((r, i, all) => i ? Math.round(r.top - all[i - 1].bottom) : null).slice(1))`));
        report(ring.split(' ').every((wh) => { const [w, h] = wh.split('x'); return w === h && Number(w) > 0; }) && gaps.every((g) => g >= 6),
            `${s.browser}'s rings are round and apart: ${ring}, gaps ${JSON.stringify(gaps)}`);
        await s.b.shot(`${HOME}/shot-calls-${s.browser}-call.png`).catch((e) => console.log('shot', e.message));
    }

    // Sealed (M16.2): each page's microphone went on under Encoded
    // Transform, and every receiver's frames go through the worker, so the
    // rings above came from frames each page opened; the call's lock says
    // so, beside the call's channel.
    const TRANSFORMS = `(() => {
        const ts = window.__pcs.at(-1).getTransceivers();
        const receiving = ts.slice(1).filter((t) => t.receiver.track && /recv/.test(t.currentDirection || ''));
        return JSON.stringify({ voice: ts[0].sender.transform instanceof RTCRtpScriptTransform, receivers: receiving.length,
            opened: receiving.every((t) => t.receiver.transform instanceof RTCRtpScriptTransform),
            lock: document.querySelector('[data-call-lock]')?.dataset.callLock || '' });
    })()`;
    for (const s of sides) {
        const x = JSON.parse(await s.b.eval(TRANSFORMS));
        report(x.voice && x.receivers >= 1 && x.opened && x.lock === 'sealed', `${s.browser} seals its microphone and opens every member's track: ${JSON.stringify(x)}`);
    }
    const logOf = (s) => s.b.eval(`fetch('/api/logs').then((r) => r.text())`);
    const madeKeys = (log) => [...log.matchAll(/: made key (\S+), sealed to (\d+) of its (\d+) other members/g)].map((m) => ({ kid: m[1], to: Number(m[2]), of: Number(m[3]) }));
    const ids = {};
    for (const [s, who] of [[A, 'alice'], [B, 'bob']]) ids[who] = await s.b.eval(`(async () => (await (await fetch('/api/dens/${den}/state')).json()).me.id)()`);
    const keysBefore = { alice: madeKeys(await logOf(A)), bob: madeKeys(await logOf(B)) };
    // Carol, the voice probe, joins, hears alice, waits for the call's
    // keys, among them alice's, stays a few seconds, and leaves.
    const carol = { instance: 'm2c', port: 18487 };
    const jar = fs.readFileSync(`${HOME}/jar-${carol.instance}`, 'utf8').split('\n').map((l) => l.split('\t')).find((f) => f[5]?.startsWith('dens_session_'));
    let probeOut = '';
    const probe = spawn(`${HOME}/voice-probe`, ['-test.run', '^TestVoiceProbe$', '-test.v', '-test.count=1'], { env: { ...process.env,
        DENS_VOICE_PROBE: `http://127.0.0.1:${carol.port}`, DENS_PROBE_COOKIE: `${jar[5]}=${jar[6]}`, DENS_PROBE_DEN: den, DENS_PROBE_CHANNEL: loungeID,
        DENS_PROBE_HEAR: ids.alice, DENS_PROBE_NETWORK: 'udp', DENS_PROBE_STAY: '6' } });
    probe.stdout.on('data', (d) => (probeOut += d));
    probe.stderr.on('data', (d) => (probeOut += d));
    const joinedThree = await waitFor(async () => JSON.parse(await A.b.eval(STATS)).members.length === 3, 'carol in the call', 60000).catch((e) => e.message);
    const probeCode = await new Promise((resolve) => probe.on('exit', resolve));
    const leftThree = await waitFor(async () => JSON.parse(await A.b.eval(STATS)).members.length === 2, 'carol gone', 20000).catch((e) => e.message);
    await sleep(2000);
    const keysAfter = { alice: madeKeys(await logOf(A)), bob: madeKeys(await logOf(B)) };
    for (const who of ['alice', 'bob']) {
        const fresh = keysAfter[who].slice(keysBefore[who].length);
        // A key for the three of them as carol joined, and one for the two
        // as she left, each new.
        const kids = new Set(fresh.map((k) => k.kid));
        report(joinedThree === true && leftThree === true && probeCode === 0 && fresh.some((k) => k.of === 2 && k.to === 2) && fresh.at(-1)?.of === 1 &&
            kids.size === fresh.length && !keysBefore[who].some((k) => kids.has(k.kid)),
            `${who}'s key changed as carol joined and as she left: ${JSON.stringify(fresh)} (carol in ${joinedThree}, gone ${leftThree}, probe ${probeCode})`);
    }
    const passed = probeOut.includes("the call's keys: this member's numbered");
    report(passed, `the services passed their keys through the den, which carol's probe got with alice's: ${passed ? 'yes' : probeOut.slice(-600)}`);

    // The call's lock opens the member's code to read out, and each other
    // member to compare with in place.
    const LOCK_DIALOG = `document.querySelector('[data-call-lock-dialog]')`;
    await B.b.eval(`document.querySelector('[data-call-lock]').click(), true`);
    const lockShown = await waitFor(() => B.b.eval(`(() => {
        const d = ${LOCK_DIALOG};
        if (!d || !/[0-9]{4} [0-9]{4} [0-9]{4} [0-9]{4} [0-9]{4}/.test(d.textContent)) return false;
        const row = [...d.querySelectorAll('li')].find((li) => li.textContent.includes('Alice'));
        return JSON.stringify({ sealed: d.dataset.callLockDialog, compare: !!row && [...row.querySelectorAll('button')].some((b) => b.textContent.trim() === 'Compare') });
    })()`), `${B.browser} shows the call's lock`, 10000).then(JSON.parse, (e) => e.message);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-lock.png`).catch((e) => console.log('shot', e.message));
    await B.b.eval(`[...${LOCK_DIALOG}.querySelectorAll('li button')].find((b) => b.textContent.trim() === 'Compare').click(), true`);
    const comparing = await waitFor(() => B.b.eval(`!!document.querySelector('form[data-codes]')`), `${B.browser} compares with Alice`, 10000).catch((e) => e.message);
    await B.b.eval(`document.querySelector('[role=dialog] button[aria-label=Close]').click(), true`);
    report(lockShown?.sealed === 'sealed' && lockShown.compare && comparing === true, `${B.browser}'s call lock: ${JSON.stringify(lockShown)}, comparing ${comparing}`);

    // Denied alice's keys, by the development build's hook, bob's page
    // receives her packets and opens none of her frames once her last key
    // passes, so nothing of her rings; given them again, it hears her.
    const withhold = (on) => B.b.eval(`fetch('/api/dens/${den}/members/${ids.alice}/withhold', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ on: ${on} }) }).then((r) => r.status)`);
    const ALICE_RINGS = `[...document.querySelectorAll('ul[aria-label="In the call"] li')].some((li) => li.textContent.includes('Alice') && li.textContent.includes('speaking'))`;
    const denied = await withhold(true);
    await sleep(12000);
    let rang = false;
    const deniedFlow = await flowing(B, 'bob receives alice while denied her keys', 1000);
    for (let i = 0; i < 20; i++) {
        rang ||= await B.b.eval(ALICE_RINGS);
        await sleep(200);
    }
    const given = await withhold(false);
    const heardAgain = await waitFor(() => B.b.eval(ALICE_RINGS), `${B.browser} hears Alice again`, 20000).catch((e) => e.message);
    report(denied === 204 && given === 204 && deniedFlow.moving && !rang && heardAgain === true,
        `${B.browser} denied Alice's keys receives her packets ${deniedFlow.before}->${deniedFlow.after}, rang for her ${rang}; given them again, hears her ${heardAgain} (${denied}, ${given})`);
    const notes = await logOf(B);
    const counted = [...notes.matchAll(/frames sealed (\d+), opened (\d+), dropped (\d+)/g)].map((m) => m.slice(1).map(Number));
    report(counted.length > 0 && counted.at(-1)[0] > 0 && counted.at(-1)[1] > 0 && counted.at(-1)[2] > 0,
        `${B.browser}'s notes count its frames, the denied ones dropped: ${JSON.stringify(counted.at(-1))}`);

    // Volume: Bob turns Alice down on his side. Her audio element follows,
    // the list shows the level, and the browser keeps it.
    const aliceID = await A.b.eval(`(async () => (await (await fetch('/api/dens/${den}/state')).json()).me.id)()`);
    const VOLUME = (id) => `(() => { const a = window.__audios.filter((x) => x.srcObject && x.srcObject.id === ${JSON.stringify(aliceID)}).at(-1); return a ? a.volume : -1; })()`;
    const ROW = `(() => { const li = [...document.querySelectorAll('ul[aria-label="In the call"] li')].find((x) => x.textContent.includes('Alice')); const mark = li && li.querySelector('button[aria-label^="Plays at"], button[aria-label="Muted for you"]'); return mark ? mark.getAttribute('aria-label') + ': ' + mark.textContent : ''; })()`;
    const KEPT = `localStorage.getItem('DENS_VOLUMES')`;
    const slide = (pct) => B.b.eval(`(() => {
        if (!document.querySelector('input[aria-label="Volume for Alice"]')) document.querySelector('button[aria-label="Options for Alice in the call"]').click();
        return new Promise((resolve) => setTimeout(() => {
            const input = document.querySelector('input[aria-label="Volume for Alice"]');
            if (!input) return resolve(false);
            input.value = '${pct}';
            input.dispatchEvent(new Event('input', { bubbles: true }));
            resolve(true);
        }, 200));
    })()`);
    const full = await B.b.eval(VOLUME());
    await slide(30);
    await sleep(300);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-menu.png`).catch((e) => console.log('shot', e.message));
    const thirty = { volume: await B.b.eval(VOLUME()), row: await B.b.eval(ROW), kept: await B.b.eval(KEPT) };
    await slide(0);
    await sleep(300);
    const zero = { volume: await B.b.eval(VOLUME()), row: await B.b.eval(ROW), kept: await B.b.eval(KEPT) };
    report(full === 1 && thirty.volume === 0.3 && thirty.row === 'Plays at 30% for you: 30%' && thirty.kept === JSON.stringify({ [`${den}:${aliceID}`]: 0.3 })
        && zero.volume === 0 && zero.row === 'Muted for you: 0%' && zero.kept === JSON.stringify({ [`${den}:${aliceID}`]: 0 }),
        `${B.browser} turns Alice down: ${full} -> ${JSON.stringify(thirty)} -> ${JSON.stringify(zero)}`);

    // A name in the call opens its profile; the menu closes on a click
    // elsewhere, and on Escape. The volume step left it open.
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await sleep(200);
    await B.b.eval(`[...document.querySelectorAll('ul[aria-label="In the call"] li button')].find((b) => !b.hasAttribute('data-call-menu') && b.textContent.includes('Alice')).click(), true`);
    const card = await waitFor(() => B.b.eval(`document.querySelector('[role=dialog][aria-label=Profile]')?.textContent.includes('@alice') || false`), `${B.browser} shows Alice's profile`, 5000).catch((e) => e.message);
    await B.b.eval(`document.querySelector('[role=dialog][aria-label=Profile] button[aria-label=Close]')?.click(), true`);
    const OPEN = `!!document.querySelector('input[aria-label="Volume for Alice"]')`;
    await B.b.eval(`document.querySelector('button[aria-label="Options for Alice in the call"]').click(), true`);
    await sleep(200);
    const menuOpened = await B.b.eval(OPEN);
    await B.b.eval(`document.querySelector('main, nav, body').dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })), true`);
    await sleep(200);
    const closedByClick = !(await B.b.eval(OPEN));
    await B.b.eval(`document.querySelector('button[aria-label="Options for Alice in the call"]').click(), true`);
    await sleep(200);
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await sleep(200);
    const closedByEscape = !(await B.b.eval(OPEN));
    report(card === true && menuOpened && closedByClick && closedByEscape, `${B.browser}: profile from the call ${card}, menu opened ${menuOpened}, closed by a click ${closedByClick}, by Escape ${closedByEscape}`);

    // The settings open as a dialog over the den; their Voice section holds
    // RNNoise and how the member sends.
    const SETTINGS = `document.querySelector('[role=dialog][aria-label="App settings"]')`;
    const inSection = (title, path) => `document.evaluate("//section[.//h2[text()='${title}']]${path}", document, null, 9, null).singleNodeValue`;
    const openVoice = async (s) => {
        await s.b.eval(`document.querySelector('button[aria-label="App settings"]').click(), true`);
        await waitFor(() => s.b.eval(`!!${SETTINGS}`), `${s.browser} settings`);
        await s.b.eval(`[...${SETTINGS}.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim() === 'Voice & video').click(), true`);
        await waitFor(() => s.b.eval(`!!document.getElementById('noise')`), `${s.browser} voice settings`);
    };
    const closeSettings = async (s, how) => {
        const act = {
            escape: `document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))`,
            outside: `${SETTINGS}.parentElement.click()`,
            close: `${SETTINGS}.querySelector('button[aria-label=Close]').click()`,
        }[how];
        await s.b.eval(`(${act}, true)`);
        return waitFor(() => s.b.eval(`!${SETTINGS}`), `${s.browser} settings closed by ${how}`, 3000).then(() => true, () => false);
    };

    // Noise suppression runs from the start on both sides, with the browser's own
    // suppressor off; B turns it off mid-call, and on again, the call going
    // on throughout.
    const SENT = `(() => { const t = window.__pcs.at(-1).getTransceivers()[0].sender.track; return t ? t.id + ' ' + t.readyState : 'none'; })()`;
    const CONTEXTS = `JSON.stringify(window.__acs.map((c) => c.sampleRate + ' ' + c.state))`;
    const NS = `JSON.parse((window.__gum || []).at(-1) || '{}').audio?.noiseSuppression`;
    const RNNOISE = `document.getElementById('noise')`;
    const atStart = await Promise.all(sides.map(async (s) => JSON.parse(await s.b.eval(CONTEXTS)).includes('48000 running') && (await s.b.eval(NS)) === false));
    const sent0 = await B.b.eval(SENT);
    await openVoice(B);
    await sleep(1500);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-settings.png`).catch((e) => console.log('shot', e.message));
    await B.b.eval(`(${SETTINGS}.querySelector('[role=tabpanel]').scrollTop = 10000, true)`);
    await sleep(300);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-settings-keys.png`).catch((e) => console.log('shot', e.message));
    await B.b.eval(`(${SETTINGS}.querySelector('[role=tabpanel]').scrollTop = 0, true)`);
    const checked0 = await B.b.eval(`${RNNOISE}.checked`);
    const turn = async () => {
        await B.b.eval(`${RNNOISE}.click(), true`);
        await sleep(3000);
        return {
            kept: (await B.b.eval(`localStorage.getItem('DENS_RNNOISE')`)) ?? null,
            sent: await B.b.eval(SENT),
            browserSuppression: await B.b.eval(NS),
            failed: await B.b.eval(`document.body.innerText.includes("couldn't start") || document.body.innerText.includes("couldn't run")`),
            through: await flowing(A, 'alice hears bob'),
        };
    };
    const off = await turn();
    const on = await turn();
    const escaped = await closeSettings(B, 'escape');
    report(atStart.every(Boolean) && checked0 === true && off.kept === '0' && off.sent !== sent0 && off.browserSuppression === true && off.through.moving
        && on.kept === null && on.sent !== off.sent && on.sent.endsWith(' live') && on.browserSuppression === false && !on.failed && on.through.moving && escaped,
        `RNNoise from the start ${JSON.stringify(atStart)}, ${B.browser} turned it off ${JSON.stringify(off)} and on ${JSON.stringify(on)}, settings closed by Escape ${escaped}`);

    // Sending: B's voice goes by voice activity, automatic from the start.
    // A level nothing reaches holds it all back, one everything passes lets
    // it through, and push to talk sends only while its key is held, and
    // nothing once the page loses focus mid-push. Held back, DTX sends
    // almost nothing, sealed too: the empty frames it leaves between its
    // packets of silence go on empty, unsent (M16.2).
    const RATE = `(async () => {
        const read = async () => {
            let b = 0;
            (await window.__pcs.at(-1).getStats()).forEach((s) => { if (s.type === 'outbound-rtp' && s.kind === 'audio') b += s.bytesSent || 0; });
            return [b, performance.now()];
        };
        const [b0, t0] = await read();
        await new Promise((r) => setTimeout(r, 2000));
        const [b1, t1] = await read();
        return Math.round(((b1 - b0) * 80) / (t1 - t0)) / 10;
    })()`;
    const key = (type, target) => B.b.eval(`(${target}.dispatchEvent(new KeyboardEvent('${type}', { code: 'KeyV', key: 'v', bubbles: true })), true)`);
    const auto = await B.b.eval(RATE);
    await openVoice(B);
    const LEVEL = `document.querySelector('input[aria-label="Sensitivity"]')`;
    const setLevel = (db) => B.b.eval(`(() => { const r = ${LEVEL}; r.value = '${db}'; r.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
    // The slider shows once automatic sensitivity is off.
    await B.b.eval(`document.getElementById('auto-sensitivity').click(), true`);
    await waitFor(() => B.b.eval(`!!${LEVEL}`), `${B.browser} the sensitivity slider`);
    await setLevel(0);
    await sleep(500);
    const shut = await B.b.eval(RATE);
    await setLevel(-100);
    await sleep(500);
    const open = await B.b.eval(RATE);
    await B.b.eval(`${inSection('Sending your voice', "//label[contains(., 'Push to talk')]//input")}.click(), true`);
    await sleep(200);
    const warned = await B.b.eval(`document.body.innerText.includes('Push to talk has no key')`);
    await B.b.eval(`${inSection('Keyboard shortcuts', "//div[span[text()='Push to talk']]//button")}.click(), true`);
    await sleep(200);
    await key('keydown', 'window');
    await sleep(300);
    // The service keeps the keys (M7.1).
    const bound = await B.b.eval(`fetch('/api/settings').then((r) => r.json()).then((s) => s.voiceKeys?.ptt?.code === 'KeyV')`);
    const outside = await closeSettings(B, 'outside');
    const up = await B.b.eval(RATE);
    await key('keydown', 'document.body');
    const held = await B.b.eval(RATE);
    await key('keyup', 'document.body');
    // What was on its way when the push ended, and Opus's lead into its
    // silence, take a few hundred milliseconds.
    await sleep(800);
    const letGo = await B.b.eval(RATE);
    await key('keydown', 'document.body');
    await sleep(300);
    await B.b.eval(`(window.dispatchEvent(new Event('blur')), true)`);
    await sleep(800);
    const blurred = await B.b.eval(RATE);
    report(auto > 16 && shut < 3 && open > 16 && warned && bound && outside && up < 3 && held > 16 && letGo < 3 && blurred < 3,
        `${B.browser} sends: automatic ${auto} kbps, level shut ${shut}, level open ${open}, push to talk up ${up}, held ${held}, let go ${letGo}, after losing focus ${blurred} (warned ${warned}, bound ${bound}, closed by a click outside ${outside})`);
    // Back to automatic voice activity, for what follows.
    await openVoice(B);
    await B.b.eval(`${inSection('Sending your voice', "//label[contains(., 'Voice activity')]//input")}.click(), true`);
    await sleep(200);
    await B.b.eval(`document.getElementById('auto-sensitivity').click(), true`);
    await closeSettings(B, 'close');

    // Staff mute: the owner mutes Bob, whose packets stop reaching her.
    await click(A, `document.querySelector('button[aria-label="Options for Bob in the call"]')`);
    await click(A, byText('Mute for everyone'));
    const barMuted = await waitFor(() => B.b.eval(`document.body.innerText.includes('Muted by staff')`), `${B.browser} told of the staff mute`, 10000).catch((e) => e.message);
    const listMuted = await waitFor(() => A.b.eval(`!!document.querySelector('[aria-label="muted by staff"]')`), `${A.browser} marks the staff mute`, 10000).catch((e) => e.message);
    await sleep(1000);
    const quiet = await flowing(A, 'alice hears muted bob');
    report(barMuted === true && listMuted === true && !quiet.moving, `staff mute: ${B.browser} told ${barMuted}, ${A.browser} marks ${listMuted}, packets ${quiet.before}->${quiet.after}`);
    await click(A, `document.querySelector('button[aria-label="Options for Bob in the call"]')`);
    await click(A, byText('Unmute for everyone'));
    await waitFor(() => B.b.eval(`!document.body.innerText.includes('Muted by staff')`), `${B.browser} unmuted`, 10000);
    await sleep(1000);
    const back = await flowing(A, 'alice hears bob again');
    report(back.moving, `unmuted: packets ${back.before}->${back.after}`);

    // Bob's service restarts mid-call: the page holds the call, its media
    // flowing, and takes it back on the same connection.
    const pcsBefore = await B.b.eval(`window.__pcs.length`);
    const pid = fs.readFileSync(`${HOME}/m2b.pid`, 'utf8').trim();
    if (fs.readFileSync(`/proc/${pid}/comm`, 'utf8').trim() !== 'dens-dev') throw new Error(`m2b.pid names ${pid}, not dens-dev`);
    execFileSync('kill', [pid]);
    const during = await flowing(A, 'alice hears bob while his service is down', 2500);
    await waitFor(() => !fs.existsSync(`/proc/${pid}`), 'bob\'s service to stop', 10000);
    lib('start', 'm2b', '--port', String(B.port));
    const resumed = await waitFor(async () => {
        const st = JSON.parse(await B.b.eval(STATS));
        return st.status.includes('Connected') && st.state === 'connected' ? st : null;
    }, `${B.browser} back in the call`, 45000).catch((e) => e.message);
    const pcsAfter = await B.b.eval(`window.__pcs.length`);
    const membersA = JSON.parse(await A.b.eval(STATS)).members;
    report(typeof resumed === 'object' && pcsAfter === pcsBefore && during.moving && membersA.length === 2,
        `service restart: media during ${during.before}->${during.after}, same connection ${pcsAfter === pcsBefore}, ${JSON.stringify(resumed)}, ${A.browser} lists ${JSON.stringify(membersA)}`);

    // The browsers' path to the den's UDP port breaks, as on another
    // network: each page asks for an ICE restart, and its call moves to the
    // den's TCP port on the same connection.
    const PATH = `(async () => {
        const pc = window.__pcs.at(-1);
        const stats = await pc.getStats();
        let id = '';
        stats.forEach((s) => { if (s.type === 'transport' && s.selectedCandidatePairId) id = s.selectedCandidatePairId; });
        if (!id) stats.forEach((s) => { if (s.type === 'candidate-pair' && s.selected) id = s.id; });
        const pair = stats.get(id);
        const remote = pair && stats.get(pair.remoteCandidateId);
        return JSON.stringify({ state: pc.connectionState, path: remote ? remote.protocol + ' ' + remote.port : '', pcs: window.__pcs.length, status: [...document.querySelectorAll('[role=status]')].map((e) => e.textContent.trim()).join(' | ') });
    })()`;
    const before = await Promise.all(sides.map(async (s) => JSON.parse(await s.b.eval(PATH))));
    const rules = [['--dport', '17881'], ['--sport', '17881']];
    for (const r of rules) execFileSync('sudo', ['-n', '/usr/sbin/iptables', '-I', 'INPUT', '-p', 'udp', ...r, '-j', 'DROP']);
    let moved;
    try {
        moved = await Promise.all(sides.map((s) => waitFor(async () => {
            const st = JSON.parse(await s.b.eval(PATH));
            return st.state === 'connected' && st.path.startsWith('tcp') ? st : null;
        }, `${s.browser} on TCP`, 45000).catch((e) => e.message)));
    } finally {
        for (const r of rules) execFileSync('sudo', ['-n', '/usr/sbin/iptables', '-D', 'INPUT', '-p', 'udp', ...r, '-j', 'DROP']);
    }
    for (const [i, s] of sides.entries()) {
        const other = sides[1 - i];
        const hearing = await flowing(s, `${s.browser} hears ${other.browser} over TCP`);
        const m = moved[i];
        report(typeof m === 'object' && m.pcs === before[i].pcs && hearing.moving,
            `UDP blocked: ${s.browser} went from ${before[i].path} to ${typeof m === 'object' ? m.path : m}, same connection ${typeof m === 'object' && m.pcs === before[i].pcs}, receives ${hearing.before}->${hearing.after}`);
    }

    // Staff disconnect: Bob's call ends with its reason.
    await click(A, `document.querySelector('button[aria-label="Options for Bob in the call"]')`);
    await click(A, byText('Disconnect from the call'));
    const told = await waitFor(() => B.b.eval(`document.body.innerText.includes('You were disconnected from the call')`), `${B.browser} told it was disconnected`, 10000).catch((e) => e.message);
    const alone = await waitFor(async () => (JSON.parse(await A.b.eval(STATS)).members.length === 1), `${A.browser} lists alice alone`, 10000).catch((e) => e.message);
    report(told === true && alone === true, `staff disconnect: ${B.browser} told ${told}, ${A.browser} alone ${alone}`);

    // Bob's next call, after a reload: Alice plays at the volume he chose,
    // and full again once he slides it back, which the browser then forgets.
    await B.b.go(`http://127.0.0.1:${B.port}/den/${den}`);
    await waitFor(() => B.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), `${B.browser} voice channel again`);
    await B.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`);
    const again = await waitFor(async () => { const v = await B.b.eval(VOLUME()); return v >= 0 ? { v } : null; }, `${B.browser} hears Alice again`, 30000).catch((e) => e.message);
    await B.b.eval(`document.querySelector('button[aria-label="Options for Alice in the call"]') && true`);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-mark.png`).catch((e) => console.log('shot', e.message));
    await slide(100);
    await sleep(300);
    const reset = { volume: await B.b.eval(VOLUME()), row: await B.b.eval(ROW), kept: await B.b.eval(KEPT) };
    report(again?.v === 0 && reset.volume === 1 && reset.row === '' && reset.kept == null,
        `${B.browser} keeps Alice's volume into the next call: ${JSON.stringify(again)}, then ${JSON.stringify(reset)}`);

    // Deafen: Bob hears nothing of the call and is muted, which Alice sees
    // as his deafened mark, beside his muted one (M11); hearing again puts
    // his microphone back.
    const PANEL_BUTTON = (label) => `document.querySelector('button[aria-label="${label}"]')`;
    const DEAFENED_MARK = `[...document.querySelectorAll('ul[aria-label="In the call"] li')].some((li) => li.textContent.includes('Bob') && li.querySelector('[aria-label=deafened]') && li.querySelector('[aria-label=muted]'))`;
    await waitFor(() => B.b.eval(`!!${PANEL_BUTTON('Deafen')}`), `${B.browser} deafen button`);
    await B.b.eval(`(${PANEL_BUTTON('Deafen')}.click(), true)`);
    await sleep(800);
    const deaf = {
        silent: await B.b.eval(`window.__audios.filter((a) => a.srcObject).every((a) => a.muted)`),
        muted: await B.b.eval(`${PANEL_BUTTON('Unmute')}?.getAttribute('aria-pressed') === 'true'`),
        marked: await waitFor(() => A.b.eval(DEAFENED_MARK), `${A.browser} sees Bob deafened`, 5000).catch(() => false),
        kept: (await B.b.eval(`localStorage.getItem('DENS_DEAFENED')`)) ?? null,
    };
    await B.b.eval(`(${PANEL_BUTTON('Undeafen')}.click(), true)`);
    await sleep(800);
    const hearing = {
        audible: await B.b.eval(`window.__audios.filter((a) => a.srcObject).every((a) => !a.muted)`),
        unmuted: await B.b.eval(`${PANEL_BUTTON('Mute')}?.getAttribute('aria-pressed') === 'false'`),
        unmarked: await waitFor(async () => !(await A.b.eval(DEAFENED_MARK)), `${A.browser} sees Bob hearing`, 5000).catch(() => false),
    };
    report(deaf.silent && deaf.muted && deaf.marked === true && deaf.kept === '1' && hearing.audible && hearing.unmuted && hearing.unmarked === true,
        `${B.browser} deafened ${JSON.stringify(deaf)}, then heard again ${JSON.stringify(hearing)}`);

    // Polish: the composer's + menu and its guide to writing a message, and
    // the video player's volume, in a text channel B posts a phone video to.
    const channelNamed = (s, name) => s.b.eval(`(async () => ((await (await fetch('/api/dens/${den}/state')).json()).channels.find((c) => c.name === '${name}') || {}).id || '')()`);
    if (!(await channelNamed(A, 'general'))) {
        await A.b.eval(`(async () => (await fetch('/api/dens/${den}/channels', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'general' }) })).status)()`);
    }
    const general = await waitFor(() => channelNamed(B, 'general'), `${B.browser} sees the text channel`, 10000);
    await B.b.go(`http://127.0.0.1:${B.port}/den/${den}/${general}`);
    // The member's panel lines up with the message bar beside it, and keeps
    // its large picture in a call, whose controls sit in a row of their own
    // above it, in the bar's shade, a little lighter than the page.
    const LINE_UP = `(() => {
        const profile = document.querySelector('button[aria-label="Edit your profile"]');
        const panel = profile.parentElement.getBoundingClientRect();
        const bar = document.querySelector('textarea').closest('.border-t').getBoundingClientRect();
        return JSON.stringify({ panel: Math.round(panel.top) + '+' + Math.round(panel.height), bar: Math.round(bar.top) + '+' + Math.round(bar.height),
            avatar: Math.round(profile.firstElementChild.getBoundingClientRect().width) });
    })()`;
    await waitFor(() => B.b.eval(`!!document.querySelector('textarea')`), `${B.browser} message box`);
    const outOfCall = JSON.parse(await B.b.eval(LINE_UP));
    await B.b.eval(`(document.querySelector('button[title^="Join the call"]').click(), true)`);
    await waitFor(async () => (await B.b.eval(STATS)).includes('Connected'), `${B.browser} back in the call`, 30000);
    await sleep(500);
    const inCall = JSON.parse(await B.b.eval(LINE_UP));
    const loungeName = await B.b.eval(`(async () => ((await (await fetch('/api/dens/${den}/state')).json()).channels.find((c) => c.kind === 'voice') || {}).name)()`);
    const lounge = 'Connected to ' + loungeName;
    const BLOCK = `document.querySelector('button[aria-label="Leave the call"]').closest('.bg-bar')`;
    const row = JSON.parse(await B.b.eval(`(() => {
        const block = ${BLOCK};
        const buttons = [...block.querySelectorAll('button')].filter((x) => !x.closest('.tooltip'));
        const panel = document.querySelector('button[aria-label="Edit your profile"]').parentElement;
        const composer = document.querySelector('textarea').closest('.border-t');
        const bg = (e) => getComputedStyle(e).backgroundColor;
        const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
        const light = (c) => { ctx.clearRect(0, 0, 1, 1); ctx.fillStyle = c; ctx.fillRect(0, 0, 1, 1); const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data; return r + g + b; };
        const b = block.getBoundingClientRect();
        // The buttons are spread evenly; how the call stands is said to a
        // screen reader alone while it's connected here, and shows on the
        // channel's row (M14.3).
        const items = buttons.map((x) => x.getBoundingClientRect());
        const gaps = items.slice(1).map((r, i) => Math.round(r.left - items[i].right));
        const status = block.querySelector('[role=status]');
        return JSON.stringify({
            labels: buttons.map((x) => x.getAttribute('aria-label')).join('|'),
            gaps: gaps.join(','),
            even: Math.max(...gaps) - Math.min(...gaps) <= 1 && Math.min(...gaps) >= 8,
            lines: new Set(buttons.map((x) => Math.round(x.getBoundingClientRect().top))).size,
            height: Math.round(b.height),
            above: Math.round(panel.getBoundingClientRect().top - b.bottom),
            status: status.textContent,
            said: status.classList.contains('sr-only'),
            line: !!block.querySelector('[data-call-status]'),
            shade: bg(block) === bg(composer) && bg(panel) === 'rgba(0, 0, 0, 0)',
            standsOut: light(bg(block)) - light(bg(panel.closest('aside'))),
            lighter: light(bg(composer)) - light(getComputedStyle(document.documentElement).backgroundColor),
        });
    })()`));
    // The channel's row shows how the call stands on its speaker, green once
    // connected, and says so in its label (M14.3).
    const speaker = JSON.parse(await B.b.eval(`(() => {
        const s = document.querySelector('[data-call-state]');
        return JSON.stringify({ state: s?.dataset.callState, green: !!s?.classList.contains('text-success'), label: s?.closest('button')?.getAttribute('aria-label') });
    })()`));
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-panel.png`).catch((e) => console.log('shot', e.message));
    await B.b.move(5, 5);
    await B.b.eval(`(document.querySelector('button[aria-label="Edit your profile"]').click(), true)`);
    const EDIT = `document.querySelector('[role=dialog][aria-label="Your profile in this den"]')`;
    const edit = await waitFor(() => B.b.eval(`!!${EDIT}`), `${B.browser} edits the profile`, 3000).catch(() => false);
    await B.b.eval(`(${EDIT}?.querySelector('button[aria-label="Close"]').click(), true)`);
    await B.b.eval(`(document.querySelector('button[aria-label="Edit your profile"]').parentElement.querySelector('[aria-label="App settings"]').click(), true)`);
    const cog = await waitFor(() => B.b.eval(`${SETTINGS}?.querySelector('[role=tab][aria-selected=true]')?.textContent.trim() || false`), `${B.browser} settings from the panel`, 3000).catch(() => false);
    await closeSettings(B, 'close');
    // The den's header: home at the left of its name, and no status while
    // it's connected; no header above the page.
    const head = JSON.parse(await B.b.eval(`(() => {
        const aside = document.querySelector('aside');
        const home = aside.querySelector('a[aria-label=Home]');
        return JSON.stringify({ home: !!home && home.getAttribute('href') === '/', homeFirst: home?.nextElementSibling?.textContent.trim(), dot: !!aside.querySelector('.badge-xs.badge-warning, .badge-xs.badge-success'), header: !!document.querySelector('header.navbar') });
    })()`));
    report(outOfCall.panel === outOfCall.bar && inCall.panel === inCall.bar && inCall.avatar === outOfCall.avatar && outOfCall.avatar >= 32
        && row.labels === 'Share your screen|Mute|Deafen|Voice settings|Leave the call' && row.even && row.lines === 1 && row.height <= 48 && row.above === 0
        && row.status === lounge && row.said && !row.line && row.shade && row.standsOut >= 15
        && speaker.state === 'connected' && speaker.green && speaker.label === `${loungeName}, connected` && edit === true && cog === 'Appearance'
        && head.home && !head.dot && !head.header,
        `${B.browser} panel and bar line up: out of a call ${JSON.stringify(outOfCall)}, in one ${JSON.stringify(inCall)}; the call's row ${JSON.stringify(row)}, the channel's speaker ${JSON.stringify(speaker)}; the picture edits ${edit}, the cog opens ${cog}; the header ${JSON.stringify(head)}`);

    // Every theme, with its colors, in the settings' Appearance tab: System,
    // Light and Dark, and the rest under More themes; a pick takes hold at
    // once, and System follows the device again.
    const PICKER = `${SETTINGS}?.querySelector('[role=tabpanel]')`;
    const MORE = `${SETTINGS}?.querySelector('[role=radiogroup][aria-label="More themes"]')`;
    const openGeneral = async () => {
        if (!(await B.b.eval(`!!${SETTINGS}`))) await B.b.eval(`(document.querySelector('button[aria-label="App settings"]').click(), true)`);
        await waitFor(() => B.b.eval(`!!${SETTINGS}`), `${B.browser} settings`);
        await B.b.eval(`([...${SETTINGS}.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim() === 'Appearance').click(), true)`);
        await waitFor(() => B.b.eval(`!!${SETTINGS}?.querySelector('[role=radiogroup][aria-label=Theme]')`), `${B.browser} theme picker`);
        if (!(await B.b.eval(`!!${MORE}`))) {
            await B.b.eval(`([...${SETTINGS}.querySelectorAll('button')].find((b) => b.textContent.startsWith('More themes')).click(), true)`);
        }
        await waitFor(() => B.b.eval(`!!${MORE}`), `${B.browser} more themes`);
    };
    const pickTheme = async (name) => {
        await openGeneral();
        await B.b.eval(`([...${PICKER}.querySelectorAll('[role=radiogroup] label')].find((l) => l.textContent.trim() === '${name}').querySelector('input').click(), true)`);
        await sleep(300);
    };
    await openGeneral();
    const menu = JSON.parse(await B.b.eval(`(() => {
        const menu = ${PICKER};
        // The themes' groups, not Appearance's others, as Animations (M15.1).
        const items = [...menu.querySelectorAll('[role=radiogroup][aria-label=Theme] label, [role=radiogroup][aria-label="More themes"] label')];
        const primaries = new Set(items.slice(1).map((i) => getComputedStyle(i.querySelector('[data-theme] > :nth-child(2)')).backgroundColor));
        // Role marks (M17.6), in their color, in every theme, against WCAG's
        // 3:1 for a graphic, on the member list and on a card.
        const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
        const lum = (...layers) => {
            ctx.clearRect(0, 0, 1, 1);
            for (const c of layers) { ctx.fillStyle = c; ctx.fillRect(0, 0, 1, 1); }
            const [r, g, b] = [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3).map((v) => { v /= 255; return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; });
            return 0.2126 * r + 0.7152 * g + 0.0722 * b;
        };
        const contrast = (x, y) => { const [a, b] = [x, y].sort((p, q) => q - p); return (a + 0.05) / (b + 0.05); };
        const marks = [];
        for (const theme of [...menu.querySelectorAll('[data-theme]')].map((s) => s.dataset.theme)) {
            const host = document.createElement('div');
            host.dataset.theme = theme;
            for (const ground of ['bg-base-200', 'bg-base-100']) {
                const box = document.createElement('div');
                box.className = ground;
                const mark = document.createElement('span');
                mark.className = 'text-base-content/80';
                box.append(mark);
                host.append(box);
            }
            document.body.append(host);
            for (const box of host.children) {
                const ground = getComputedStyle(box).backgroundColor;
                marks.push({ at: theme + '/' + box.className, c: contrast(lum(ground), lum(ground, getComputedStyle(box.firstChild).color)) });
            }
            host.remove();
        }
        marks.sort((a, b) => a.c - b.c);
        return JSON.stringify({ count: items.length, first: items[0].textContent.trim(),
            checked: items.filter((i) => i.querySelector('input').checked).map((i) => i.textContent.trim()).join('|'),
            primaries: primaries.size, marks: marks.length, worstMark: marks[0].at + ' ' + marks[0].c.toFixed(2) });
    })()`));
    await pickTheme('Dracula');
    const dracula = { theme: await B.b.eval(`document.documentElement.dataset.theme`), kept: await B.b.eval(`localStorage.getItem('DENS_THEME')`), open: await B.b.eval(`!!${SETTINGS}`) };
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-themes.png`).catch((e) => console.log('shot', e.message));
    await pickTheme('Dark');
    await closeSettings(B, 'close');
    await sleep(300);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-panel-dark.png`).catch((e) => console.log('shot', e.message));
    // How much lighter the bar is than the page, and the call's block than
    // the channel list around it.
    const SHADES = `(() => {
        const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
        const light = (e) => { ctx.clearRect(0, 0, 1, 1); ctx.fillStyle = getComputedStyle(e).backgroundColor; ctx.fillRect(0, 0, 1, 1); const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data; return r + g + b; };
        const block = document.querySelector('button[aria-label="Leave the call"]').closest('.bg-bar');
        return JSON.stringify({ bar: light(document.querySelector('textarea').closest('.border-t')) - light(document.documentElement), block: light(block) - light(block.closest('aside')) });
    })()`;
    const dark = JSON.parse(await B.b.eval(SHADES));
    // On a white page there's no lighter: the bar is as light, and the
    // block still stands out from the darker channel list.
    await pickTheme('Light');
    await closeSettings(B, 'close');
    const white = JSON.parse(await B.b.eval(SHADES));
    await pickTheme('System');
    await closeSettings(B, 'close');
    const system = {
        theme: await B.b.eval(`document.documentElement.dataset.theme`),
        kept: await B.b.eval(`localStorage.getItem('DENS_THEME')`),
        dark: await B.b.eval(`matchMedia('(prefers-color-scheme: dark)').matches`),
    };
    // The member list's role marks, for a look in Light and in Winter.
    const MEMBERS = `document.querySelector('button[aria-label="Members"]')`;
    if ((await B.b.eval(`${MEMBERS}?.getAttribute('aria-pressed')`)) === 'false') await B.b.eval(`(${MEMBERS}.click(), true)`);
    await sleep(800);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-members.png`).catch((e) => console.log('shot', e.message));
    await pickTheme('Winter');
    await closeSettings(B, 'close');
    await sleep(300);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-members-winter.png`).catch((e) => console.log('shot', e.message));
    await pickTheme('System');
    await closeSettings(B, 'close');
    await B.b.eval(`(${MEMBERS}.click(), true)`);
    await sleep(300);
    report(menu.count === 36 && menu.first === 'System' && menu.checked === 'System' && menu.primaries >= 25
        && menu.marks === 70 && parseFloat(menu.worstMark.split(' ')[1]) >= 3
        && dracula.theme === 'dracula' && dracula.kept === 'dracula' && dracula.open && dark.bar >= 15 && dark.block >= 15 && white.bar === 0 && white.block >= 15
        && system.theme === (system.dark ? 'dark' : 'light') && system.kept == null,
        `${B.browser} themes: picker ${JSON.stringify(menu)}, Dracula ${JSON.stringify(dracula)}, lighter in Dark ${JSON.stringify(dark)} and Light ${JSON.stringify(white)}, System ${JSON.stringify(system)}`);

    await B.b.eval(`(document.querySelector('button[aria-label="Leave the call"]').click(), true)`);
    await B.b.eval(`(document.querySelector('button[aria-label="App settings"]').click(), true)`);
    await waitFor(() => B.b.eval(`!!${SETTINGS}`), `${B.browser} settings`);
    const tabs = await B.b.eval(`[...${SETTINGS}.querySelectorAll('[role=tab]')].map((t) => t.textContent.trim()).join('|')`);
    await closeSettings(B, 'close');
    report(tabs === 'Appearance|Chat & media|Notifications & sounds|Voice & video|System', `${B.browser} settings tabs: ${tabs}`);

    // A join that waits on the microphone, as while the browser asks for
    // it, says so; the voice processor's context comes from the click.
    await B.b.eval(`(window.__gumDelay = 3000, true)`);
    await B.b.eval(`(document.querySelector('button[title^="Join the call"]').click(), true)`);
    await sleep(2000);
    const waiting = JSON.parse(await B.b.eval(`JSON.stringify({ line: document.body.innerText.includes('Waiting for the microphone'), warm: window.__acs.some((c) => c.sampleRate === 48000 && c.state !== 'closed') })`));
    await waitFor(async () => (await B.b.eval(STATS)).includes('Connected to'), `${B.browser} in the call after the wait`, 30000);
    await sleep(500);
    const waited = await B.b.eval(`document.body.innerText.includes('Waiting for the microphone')`);
    await B.b.eval(`(window.__gumDelay = 0, document.querySelector('button[aria-label="Leave the call"]').click(), true)`);
    report(waiting.line && waiting.warm && !waited, `${B.browser} waits on the microphone: ${JSON.stringify(waiting)}, line gone once in the call ${!waited}`);

    // A file dropped on the messages leaves the box ready for Enter.
    await B.b.eval(`(document.activeElement?.blur(), true)`);
    const dropped = await B.b.eval(`(() => {
        const dt = new DataTransfer();
        dt.items.add(new File([new Uint8Array([104, 105])], 'note.txt', { type: 'text/plain' }));
        document.querySelector('textarea').closest('.flex-col').querySelector('.overflow-y-auto').dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true, cancelable: true }));
        return new Promise((r) => setTimeout(() => r(document.activeElement?.tagName || ''), 300));
    })()`);
    await B.b.eval(`(document.querySelector('ul[aria-label="Files to send"] button')?.click(), true)`);
    report(dropped === 'TEXTAREA', `${B.browser}: after a file is dropped, focus is on ${dropped}`);

    const ADD = `document.querySelector('button[aria-label="Add to this message"]')`;
    const MENU = `document.querySelector('[data-compose-menu] [role=menu]')`;
    await waitFor(() => B.b.eval(`!!${ADD}`), `${B.browser} composer`);
    await B.b.eval(`(${ADD}.click(), true)`);
    await sleep(200);
    const items = await B.b.eval(`[...(${MENU}?.querySelectorAll('[role=menuitem]') || [])].map((b) => b.textContent.trim()).join('|')`);
    await B.b.eval(`(document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true)`);
    await sleep(200);
    const escClosed = !(await B.b.eval(`!!${MENU}`));
    await B.b.eval(`(${ADD}.click(), true)`);
    await sleep(200);
    await B.b.eval(`(document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })), true)`);
    await sleep(200);
    const clickClosed = !(await B.b.eval(`!!${MENU}`));
    const caption = await B.b.eval(`document.body.innerText.includes('Enter to send')`);
    const bordered = JSON.parse(await B.b.eval(`(() => {
        document.activeElement?.blur();
        const look = (e) => { const s = getComputedStyle(e); return [s.borderTopColor, s.borderTopWidth, s.borderTopLeftRadius].join(' '); };
        const plus = look(${ADD}), box = look(document.querySelector('textarea'));
        return JSON.stringify({ plus, same: plus === box });
    })()`));
    // The guide's ? sits inside the message box, at its right.
    const inside = await B.b.eval(`(() => { const q = document.querySelector('button[aria-label="How to write a message"]').getBoundingClientRect(); const t = document.querySelector('textarea').getBoundingClientRect(); return q.left > t.left && q.right <= t.right && q.top >= t.top && q.bottom <= t.bottom; })()`);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-composer.png`).catch((e) => console.log('shot', e.message));
    const GUIDE = `document.querySelector('[role=dialog][aria-label="Writing a message"]')`;
    await B.b.eval(`(document.querySelector('button[aria-label="How to write a message"]').click(), true)`);
    const guide = await waitFor(() => B.b.eval(`${GUIDE}?.innerText.includes('Formatting') || false`), `${B.browser} guide`, 3000).catch(() => false);
    await sleep(300);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-guide.png`).catch((e) => console.log('shot', e.message));
    await B.b.eval(`(document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true)`);
    await sleep(200);
    const guideClosed = !(await B.b.eval(`!!${GUIDE}`));
    // A channel's + makes a poll too (M15.4).
    report(items === 'Attach files|Let others edit|Poll' && escClosed && clickClosed && !caption && inside && guide === true && guideClosed && bordered.same,
        `${B.browser} composer: + bordered as the box ${JSON.stringify(bordered)}, menu ${items}, closed by Escape ${escClosed} and a click ${clickClosed}, old caption ${caption}, ? inside the box ${inside}, guide ${guide}, closed ${guideClosed}`);

    // Who's typing floats above the message box, which keeps its place.
    const boxTop = () => B.b.eval(`Math.round(document.querySelector('textarea').getBoundingClientRect().top)`);
    const top0 = await boxTop();
    await A.b.go(`http://127.0.0.1:${A.port}/den/${den}/${general}`);
    await waitFor(() => A.b.eval(`!!document.querySelector('textarea')`), `${A.browser} composer`);
    await A.b.eval(`(() => { const t = document.querySelector('textarea'); t.focus(); t.value = 'hello'; t.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
    const typing = await waitFor(() => B.b.eval(`(() => {
        const line = [...document.querySelectorAll('[aria-live=polite]')].find((e) => e.textContent.includes('typing'));
        if (!line) return false;
        const composer = document.querySelector('textarea').closest('.border-t').getBoundingClientRect();
        return line.getBoundingClientRect().bottom <= composer.top + 1 ? line.textContent : false;
    })()`), `${B.browser} sees Alice typing`, 10000).catch((e) => e.message);
    const top1 = await boxTop();
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-typing.png`).catch((e) => console.log('shot', e.message));
    report(typeof typing === 'string' && typing.includes('Alice') && top0 === top1, `${B.browser}: "${typing}" above the composer, which stayed put: ${top0} -> ${top1}`);

    const clip = fs.readFileSync(`${HOME}/clip.mp4`).toString('base64');
    const posted = await B.b.eval(`(async () => {
        const bytes = Uint8Array.from(atob('${clip}'), (c) => c.charCodeAt(0));
        const up = await fetch('/api/dens/${den}/uploads', { method: 'POST', headers: { 'Dens-Filename': 'clip.mp4', 'Content-Type': 'application/octet-stream' }, body: bytes });
        if (!up.ok) return 'upload ' + up.status + ' ' + (await up.text());
        const file = await up.json();
        const nonce = btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(16)))).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '');
        const m = await fetch('/api/dens/${den}/channels/${general}/messages', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ nonce, text: '', attachments: [file.id] }) });
        return m.ok ? 'ok' : 'message ' + m.status;
    })()`);
    await waitFor(() => B.b.eval(`!!document.querySelector('video')`), `${B.browser} shows the video`, 20000).catch(() => {});
    await sleep(500);
    const player = JSON.parse(await B.b.eval(`(() => {
        const v = [...document.querySelectorAll('video')].at(-1);
        if (!v) return '{}';
        const frame = v.parentElement;
        const r = frame.getBoundingClientRect();
        const badges = [...frame.querySelectorAll('.badge')].map((b) => b.textContent).join('|');
        const mute = frame.querySelector('button[aria-label="Mute"], button[aria-label="Unmute"]');
        const before = getComputedStyle(frame.querySelector('input[aria-label="Volume"]').parentElement.parentElement).display;
        mute.focus();
        const after = getComputedStyle(frame.querySelector('input[aria-label="Volume"]').parentElement.parentElement).display;
        frame.scrollIntoView();
        return JSON.stringify({ size: Math.round(r.width) + 'x' + Math.round(r.height), badges, before, after, focused: document.activeElement === mute, windowFocused: document.hasFocus() });
    })()`));
    await sleep(300);
    await B.b.shot(`${HOME}/shot-calls-${B.browser}-video.png`).catch((e) => console.log('shot', e.message));
    // Firefox applies :focus-within only while its window has focus, which
    // a headless one never does; Chromium checks the popover.
    const popover = player.after === 'flex' || (B.browser === 'firefox' && !player.windowFocused);
    report(posted === 'ok' && player.badges === '' && player.before === 'none' && popover,
        `${B.browser} video: posted ${posted}, ${JSON.stringify(player)}${player.after !== 'flex' ? ' (popover unchecked: the window has no focus)' : ''}`);

    // One video's volume is every video's, and the browser keeps it.
    const shared = JSON.parse(await B.b.eval(`(() => {
        const sliders = [...document.querySelectorAll('input[aria-label="Volume"]')];
        const last = sliders.at(-1);
        last.value = '0.5';
        last.dispatchEvent(new Event('input', { bubbles: true }));
        const volumes = [...document.querySelectorAll('video')].map((v) => v.volume);
        const kept = localStorage.getItem('DENS_VIDEO_VOLUME');
        last.value = '1';
        last.dispatchEvent(new Event('input', { bubbles: true }));
        return JSON.stringify({ videos: volumes.length, volumes: [...new Set(volumes)], kept });
    })()`));
    report(shared.videos >= 1 && shared.volumes.length === 1 && shared.volumes[0] === 0.5 && shared.kept === '0.5',
        `${B.browser} video volume shared: ${JSON.stringify(shared)}`);
    await B.b.go(`http://127.0.0.1:${B.port}/`);
    await waitFor(() => B.b.eval(`!!document.querySelector('a[aria-label="App settings"]')`), `${B.browser} home`);
    const home = JSON.parse(await B.b.eval(`(() => {
        const cog = document.querySelector('a[aria-label="App settings"]');
        const r = cog.getBoundingClientRect();
        return JSON.stringify({ fixed: getComputedStyle(cog).position, left: Math.round(r.left), fromBottom: Math.round(innerHeight - r.bottom), header: !!document.querySelector('header.navbar'), title: document.title, label: cog.textContent.trim() });
    })()`));
    await B.b.eval(`(document.querySelector('a[aria-label="App settings"]').click(), true)`);
    const homeSettings = await waitFor(() => B.b.eval(`!!${SETTINGS}`), `${B.browser} settings from home`, 3000).catch(() => false);
    // The version and instance moved from home's foot to the settings' About.
    await B.b.eval(`([...${SETTINGS}.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim() === 'System').click(), true)`);
    const about = await waitFor(() => B.b.eval(`[...${SETTINGS}.querySelectorAll('section')].find((x) => x.querySelector('h2')?.textContent === 'About')?.innerText || ''`), `${B.browser} About`, 5000).catch(() => '');
    await closeSettings(B, 'close');
    report(home.fixed === 'fixed' && home.left <= 24 && home.fromBottom <= 24 && !home.header && home.title === 'Dens (m2b)' && home.label === 'App settings'
        && about.includes('instance m2b') && homeSettings === true,
        `${B.browser} home: ${JSON.stringify(home)}, settings ${homeSettings}, about ${JSON.stringify(about)}`);
} catch (e) {
    c.fail(e);
    for (const s of sides) if (s.b) console.log(`  ${s.browser}:`, await s.b.eval(STATS).catch((x) => x.message));
} finally {
    for (const b of opened) await b.close();
    c.finish();
}
