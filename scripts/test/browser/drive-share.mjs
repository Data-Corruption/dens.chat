// The M4.2 check: Chromium (alice, the owner) shares its fake screen, and
// Firefox (bob), headed on Xvfb, the X screen; each watches the other's in
// the player. Then the share's limits, a staff mute, and a voice channel's
// bitrate. From M18 it measures what arrives of alice's share's sound:
// bob's Firefox and carol's Chromium each decode it, samples with energy in
// them, their workers opening its frames and dropping none, as alice's
// seals them; and with a preload that sets the sound's transform a task
// late, as the page did before M18, alice's page stops the sound within a
// second and says so.
//
//   run.sh share
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, CAROL, HOME, MANAGE, SETTINGS, checks, launch, openManage, openSettings, pair, waitFor } from './browsers.mjs';

const { DEN: den } = process.env;
// The preload keeps each page's peer connections and workers, what it
// asked of getDisplayMedia, when a track went on each sender and the
// voice.share messages it sent; and while __lateSound holds, it sets the
// share's sound's transform a task after the page did.
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
    const W = window.Worker;
    window.__workers = [];
    window.Worker = class extends W { constructor(...a) { super(...a); window.__workers.push(this); } };
    const ST = window.RTCRtpScriptTransform;
    const sender = window.RTCRtpSender?.prototype;
    if (ST && sender) {
        window.RTCRtpScriptTransform = class extends ST { constructor(w, o, ...r) { super(w, o, ...r); this.__options = o; } };
        const transform = Object.getOwnPropertyDescriptor(sender, 'transform');
        Object.defineProperty(sender, 'transform', {
            configurable: true,
            get() { return this.__late || transform.get.call(this); },
            set(t) {
                if (window.__lateSound && t?.__options?.side === 'send' && t.__options.track === 2) {
                    this.__late = t;
                    setTimeout(() => transform.set.call(this, t), 0);
                    return;
                }
                transform.set.call(this, t);
            },
        });
        const replace = sender.replaceTrack;
        window.__attached = [];
        sender.replaceTrack = function (track) {
            const p = replace.call(this, track);
            p.then(() => track && window.__attached.push({ kind: track.kind, track: this.transform?.__options?.track, at: performance.now() }), () => {});
            return p;
        };
    }
    const send = WebSocket.prototype.send;
    window.__shares = [];
    WebSocket.prototype.send = function (data) {
        try { const m = JSON.parse(data); if (m.t === 'voice.share') window.__shares.push({ ...m.d, at: performance.now() }); } catch {}
        return send.call(this, data);
    };
}`;
const c = checks();
const report = c.report;
const A = ALICE;
const B = BOB;
const C = CAROL;
// COUNTS is what a page's worker counted of its call's frames, as it says.
const COUNTS = `(async () => {
    const w = (window.__workers || []).at(-1);
    if (!w) return 'null';
    return new Promise((resolve) => {
        const heard = (e) => { if (e.data?.t === 'counts') { w.removeEventListener('message', heard); resolve(JSON.stringify(e.data)); } };
        w.addEventListener('message', heard);
        w.postMessage({ t: 'counts' });
        setTimeout(() => resolve('null'), 1500);
    });
})()`;
const counts = async (s) => JSON.parse(await s.b.eval(COUNTS));
const droppedOf = (track) => Object.values(track?.dropped || {}).reduce((n, x) => n + x, 0);
// SOUND_RX is what a watcher's receiver of the share's sound in its player
// received and decoded: its samples, those concealed in place of frames
// that never came, and their energy.
const SOUND_RX = `(async () => {
    const v = document.querySelector('section[aria-label="Screen shares"] video:not([aria-label="Your screen"])');
    const track = v?.srcObject?.getAudioTracks()[0];
    const pc = (window.__pcs || []).at(-1);
    const t = track && pc?.getTransceivers().find((x) => x.receiver.track === track);
    if (!t) return JSON.stringify({ found: false });
    const out = { found: true, mid: t.mid, muted: v.muted, volume: v.volume };
    (await t.receiver.getStats()).forEach((s) => {
        if (s.type !== 'inbound-rtp') return;
        Object.assign(out, { packets: s.packetsReceived || 0, samples: s.totalSamplesReceived ?? s.jitterBufferEmittedCount ?? 0,
            concealed: s.concealedSamples || 0, energy: s.totalAudioEnergy || 0 });
    });
    return JSON.stringify(out);
})()`;
const soundRx = async (s) => JSON.parse(await s.b.eval(SOUND_RX));
const STATE = (s) => `(async () => JSON.stringify(await (await fetch('/api/dens/${den}/state')).json()))()`;
const VIDEO = `(async () => {
    const pc = window.__pcs.at(-1); if (!pc) return '{}';
    const out = { tx: 0, frames: 0, sent: 0, rx: 0, decoded: 0, w: 0, h: 0, fps: 0, audioRx: 0 };
    (await pc.getStats()).forEach((s) => {
        if (s.type === 'outbound-rtp' && s.kind === 'video') { out.frames += s.framesEncoded || 0; out.sent += s.bytesSent || 0; out.limit = s.qualityLimitationReason; out.encW = s.frameWidth; out.encH = s.frameHeight; }
        if (s.type === 'inbound-rtp' && s.kind === 'video') { out.decoded += s.framesDecoded || 0; out.rx += s.bytesReceived || 0; out.w = s.frameWidth || out.w; out.h = s.frameHeight || out.h; out.fps = s.framesPerSecond || out.fps; }
    });
    return JSON.stringify(out);
})()`;
const video = async (s) => JSON.parse(await s.b.eval(VIDEO));
const PLAYER = `document.querySelector('section[aria-label="Screen shares"]')`;
const text = (s) => s.b.eval('document.body.innerText');
const clickButton = (s, label, gesture = false) => s.b.eval(`(() => { const b = [...document.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(label)} || x.getAttribute('aria-label') === ${JSON.stringify(label)}); if (!b) return 'missing'; if (b.disabled) return 'disabled: ' + b.title; b.click(); return 'clicked'; })()`, gesture);
const setLimits = (changes) => A.b.eval(`(async () => {
    const v = await (await fetch('/api/dens/${den}/state')).json();
    const r = await fetch('/api/dens/${den}/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ call_limits: { ...v.call_limits, ...${JSON.stringify(changes)} } }) });
    return r.status;
})()`);

// The owner's den as the checks start: one share at 2 Mbps, the Lounge at
// 96 kbps, and a text channel in a group.
const prepare = `(async () => {
    const post = (path, body, method = 'POST') => fetch('/api/dens/${den}' + path, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const v = await (await fetch('/api/dens/${den}/state')).json();
    await post('/settings', { call_limits: { ...v.call_limits, shares: 1, share_bitrate: 2000000 } });
    const lounge = v.channels.find((c) => c.name === 'Lounge');
    await post('/channels/' + lounge.id, { bitrate: 96000 }, 'PATCH');
    let group = v.groups.find((g) => g.name === 'Chat');
    if (!group) group = await (await post('/groups', { name: 'Chat' })).json();
    if (!v.channels.some((c) => c.name === 'general')) await post('/channels', { name: 'general', group_id: group.id });
    return true;
})()`;

try {
    A.b = await launch('chromium', 9433, { media: true, screen: true, preload: PRELOAD });
    B.b = await launch('firefox', 9434, { media: 'pulse', screen: true, preload: PRELOAD, size: [1400, 860] });
    C.b = await launch('chromium', 9435, { media: true, preload: PRELOAD });
    for (const s of [A, B, C]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), `${s.name} voice channel`);
        if (s === A) {
            await s.b.eval(prepare);
            await sleep(1000);
        }
        s.me = JSON.parse(await s.b.eval(STATE(s))).me.id;
        await s.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    }
    for (const s of [A, B, C]) await waitFor(async () => (await s.b.eval(`!!document.querySelector('button[aria-label="Share your screen"]')`)) && (await s.b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`)), `${s.name} in the call`, 30000);
    // The share button sits in the call's row, just before mute, after the
    // soundboard where the member has one (M14.3).
    const row = await A.b.eval(`(() => { const b = document.querySelector('button[aria-label="Share your screen"]'); const all = [...b.parentElement.children].map((x) => x.getAttribute('aria-label') || x.querySelector('button')?.getAttribute('aria-label') || x.tagName); return JSON.stringify(all); })()`);
    const order = JSON.parse(row);
    const at = order.indexOf('Share your screen');
    report(at > 0 && order[at + 1] === 'Mute', `both in the call; the share button sits just before mute: ${row}`);

    // The channel list: a ⋯ for each channel and group, for the owner, and no cog in the header.
    await waitFor(() => A.b.eval(`!!document.querySelector('button[aria-label="Settings for general"]')`), 'the text channel', 5000).catch(() => {});
    const menus = await A.b.eval(`JSON.stringify({ lounge: !!document.querySelector('button[aria-label="Settings for Lounge"]'), general: !!document.querySelector('button[aria-label="Settings for general"]'), group: !!document.querySelector('button[aria-label="Settings for the group Chat"]'), cog: !!document.querySelector('header button[aria-label="Channel settings"]') })`);
    const bobMenus = await B.b.eval(`!!document.querySelector('button[aria-label="Settings for Lounge"]')`);
    report(menus === JSON.stringify({ lounge: true, general: true, group: true, cog: false }) && !bobMenus, `the owner has a ⋯ per channel and group and no header cog, a member none: ${menus}, member ${bobMenus}`);

    // Alice shares; nobody watches yet, so her encoder waits.
    report(await clickButton(A, 'Share your screen', true) === 'clicked', 'alice clicks Share your screen');
    await waitFor(async () => (await A.b.eval(`!!document.querySelector('[aria-label="Nobody is watching yet"]')`)), 'alice live', 15000);
    const asked = JSON.parse(await A.b.eval('window.__gdm'));
    report(asked.selfBrowserSurface === 'exclude' && asked.audio?.restrictOwnAudio === true && asked.video?.frameRate?.max === 30, `the browser was asked for ${JSON.stringify(asked)}`);
    const own = await waitFor(() => A.b.eval(`(() => { const v = document.querySelector('section[aria-label="Screen shares"] video[aria-label="Your screen"]'); return v && v.videoWidth > 0 ? JSON.stringify({ w: v.videoWidth, muted: v.muted, title: document.querySelector('section[aria-label="Screen shares"] span').textContent }) : ''; })()`), "alice's preview", 10000).catch((e) => e.message);
    report(own.startsWith('{') && JSON.parse(own).muted && JSON.parse(own).title === 'Your screen', `alice's own screen opens in her player, muted: ${own}`);
    const pressed = await A.b.eval(`document.querySelector('button[aria-label="Stop sharing your screen"]')?.getAttribute('aria-pressed')`);
    report(pressed === 'true', `the share button shows pressed while she shares: ${pressed}`);
    await A.b.shot(`${HOME}/shot-share-alice-preview.png`);
    // Its ✕ hides it, and she goes on sharing; the row's button, and her own LIVE mark, bring it back.
    await A.b.eval(`document.querySelector('button[aria-label="Hide your screen"]').click(), true`);
    await sleep(300);
    const hidden = !(await A.b.eval(`!!document.querySelector('section[aria-label="Screen shares"]')`)) && (await A.b.eval(`!!document.querySelector('[aria-label="Nobody is watching yet"]')`));
    await A.b.eval(`document.querySelector('button[aria-label="Show your screen in the player"]').click(), true`);
    await sleep(300);
    const back1 = await A.b.eval(`!!document.querySelector('video[aria-label="Your screen"]')`);
    await A.b.eval(`[...document.querySelectorAll('ul[aria-label="In the call"] button')].find((b) => b.textContent.trim() === 'LIVE').click(), true`);
    await sleep(300);
    const gone2 = !(await A.b.eval(`!!document.querySelector('video[aria-label="Your screen"]')`));
    await A.b.eval(`[...document.querySelectorAll('ul[aria-label="In the call"] button')].find((b) => b.textContent.trim() === 'LIVE').click(), true`);
    await sleep(300);
    const back2 = await A.b.eval(`!!document.querySelector('video[aria-label="Your screen"]')`);
    report(hidden && back1 && gone2 && back2, `the preview hides with its ✕ while the share goes on (${hidden}), and the row's button (${back1}) and her LIVE mark (${gone2}, ${back2}) toggle it`);
    await A.b.eval(`document.querySelector('button[aria-label="Hide your screen"]').click(), true`);
    const p0 = await video(A); await sleep(1500); const p1 = await video(A);
    report(p1.frames === p0.frames && p1.sent === p0.sent, `alice's encoder waits while nobody watches: frames ${p0.frames} -> ${p1.frames}`);
    const live = await waitFor(() => B.b.eval(`(() => { const b = [...document.querySelectorAll('button')].find((x) => x.textContent.trim() === 'LIVE'); return b ? b.getAttribute('aria-label') : ''; })()`), 'bob sees LIVE');
    report(live === "Watch Alice's screen", `bob sees alice's LIVE mark: ${live}`);
    const blocked = await clickButton(B, 'Share your screen');
    report(blocked.startsWith('disabled:') && blocked.includes('one share at a time'), `bob's share button waits while the den has its one share: ${blocked}`);
    await A.b.shot(`${HOME}/shot-share-alice-live.png`);

    // Bob watches: the player comes, plays, and alice's encoder starts.
    const t0 = Date.now();
    await B.b.eval(`[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === 'LIVE').click(), true`, true);
    await waitFor(() => B.b.eval(`!!${PLAYER}`), 'the player');
    const playing = await waitFor(() => B.b.eval(`(() => { const v = ${PLAYER}?.querySelector('video'); return v && v.videoWidth > 0 ? v.videoWidth + 'x' + v.videoHeight : ''; })()`), 'bob sees alice', 15000).catch((e) => e.message);
    report(/^\d+x\d+$/.test(playing), `bob's player plays alice's screen at ${playing}, ${Date.now() - t0} ms after the click`);
    const seenBy = await waitFor(() => A.b.eval(`document.querySelector('[aria-label="Watching: Bob"]')?.textContent || ''`), 'alice sees bob watching', 10000).catch((e) => e.message);
    report(seenBy === '1', `alice's row counts Bob watching: ${seenBy}`);
    await sleep(3000);
    const v1 = await video(B); const e1 = await video(A);
    report(v1.decoded > 10 && v1.w * v1.h <= 1920 * 1080 && v1.fps <= 31, `bob decodes alice's share: ${JSON.stringify(v1)}; alice encodes ${JSON.stringify(e1)}`);
    await B.b.shot(`${HOME}/shot-share-bob-player.png`);

    // What arrives of the share's sound (M18): carol's Chromium watches
    // too, and both watchers decode it, samples with energy in them, while
    // their workers open its frames and drop none, as alice's seals them.
    await waitFor(() => C.b.eval(`[...document.querySelectorAll('button')].some((x) => x.textContent.trim() === 'LIVE')`), 'carol sees LIVE');
    await C.b.eval(`[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === 'LIVE').click(), true`, true);
    await waitFor(() => C.b.eval(`(() => { const v = ${PLAYER}?.querySelector('video'); return v && v.videoWidth > 0; })()`), 'carol sees alice', 15000);
    await sleep(3000);
    const watchers = [[B, 'Firefox'], [C, 'Chromium']];
    const heard0 = await Promise.all(watchers.map(([s]) => soundRx(s)));
    const opened0 = await Promise.all(watchers.map(([s]) => counts(s)));
    const sealed0 = await counts(A);
    await sleep(4000);
    const heard1 = await Promise.all(watchers.map(([s]) => soundRx(s)));
    const opened1 = await Promise.all(watchers.map(([s]) => counts(s)));
    const sealed1 = await counts(A);
    const sealedRate = Math.round((sealed1.sound.sealed - sealed0.sound.sealed) / 4);
    report(sealedRate >= 40, `alice's worker seals her share's sound: ${sealedRate} frames a second, ${JSON.stringify(sealed1.sound)}`);
    for (const [i, [s, browser]] of watchers.entries()) {
        const [h0, h1, o0, o1] = [heard0[i], heard1[i], opened0[i], opened1[i]];
        if (!h0.found || !h1.found) {
            report(false, `${s.name}'s ${browser} has no share's sound in its player: ${JSON.stringify(h1)}, its worker ${JSON.stringify(o1?.sound)}`);
            continue;
        }
        const decoded = h1.samples - h1.concealed - (h0.samples - h0.concealed);
        const opened = Math.round((o1.sound.opened - o0.sound.opened) / 4);
        report(h1.energy > h0.energy && decoded > 4 * 48000 * 0.8 && opened >= 40 && droppedOf(o1.sound) === 0,
            `${s.name}'s ${browser} decodes the share's sound, ${decoded} samples in 4 s with energy ${h0.energy.toFixed(3)} -> ${h1.energy.toFixed(3)} (${JSON.stringify(h1)}), its worker opening ${opened} of its frames a second and dropping none: ${JSON.stringify(o1.sound)}`);
    }
    await C.b.eval(`document.querySelector('button[aria-label="Leave the call"]').click(), true`);
    await waitFor(() => A.b.eval(`document.querySelector('[aria-label="Watching: Bob"]')?.textContent || ''`), 'carol gone', 10000).catch(() => {});

    // The player drags by its bar, and keeps its place across a reload.
    const before = JSON.parse(await B.b.eval(`JSON.stringify(${PLAYER}.getBoundingClientRect())`));
    await B.b.drag(before.x + 40, before.y + 12, before.x - 260, before.y - 188);
    await sleep(300);
    const after = JSON.parse(await B.b.eval(`JSON.stringify(${PLAYER}.getBoundingClientRect())`));
    const kept = await B.b.eval(`localStorage.getItem('DENS_PLAYER')`);
    report(Math.round(before.x - after.x) === 300 && Math.round(before.y - after.y) === 200 && kept && JSON.parse(kept).x === Math.round(after.x),
        `the player moved by its bar from ${Math.round(before.x)},${Math.round(before.y)} to ${Math.round(after.x)},${Math.round(after.y)}, kept ${kept}`);
    // A corner resizes it.
    const atCorner = await B.b.eval(`(() => { const e = document.elementFromPoint(${Math.round(after.x + after.width - 3)}, ${Math.round(after.y + after.height - 3)}); return e ? e.tagName + ' ' + e.className : 'none'; })()`);
    console.log('     at the corner:', atCorner);
    await B.b.drag(after.x + after.width - 3, after.y + after.height - 3, after.x + after.width + 117, after.y + after.height + 57);
    await sleep(300);
    const bigger = JSON.parse(await B.b.eval(`JSON.stringify(${PLAYER}.getBoundingClientRect())`));
    report(Math.round(bigger.width - after.width) === 120 && Math.round(bigger.height - after.height) === 60, `the corner resized it to ${Math.round(bigger.width)}×${Math.round(bigger.height)}`);

    // The browser's own stop ends alice's share, and bob's player closes.
    await A.b.eval(`window.__display.getVideoTracks()[0].dispatchEvent(new Event('ended')), true`);
    await waitFor(async () => !(await B.b.eval(`!!${PLAYER}`)), 'the player to close', 10000).catch(() => {});
    report(!(await B.b.eval(`!!${PLAYER}`)) && (await A.b.eval(`!!document.querySelector('button[aria-label="Share your screen"]')`)), "stopping from the browser ends alice's share and bob's player");

    // Firefox shares the X screen, without sound; alice watches it.
    report(await setLimits({ shares: 2 }) === 200, 'the owner allows two shares');
    await sleep(500);
    report(await clickButton(B, 'Share your screen', true) === 'clicked', 'bob clicks Share your screen');
    await waitFor(async () => (await B.b.eval(`!!document.querySelector('[aria-label="Nobody is watching yet"]')`)), 'bob live', 15000);
    const quiet = await B.b.eval(`!!document.querySelector('[aria-label="no sound"]')`);
    report(quiet, "bob's Firefox share says it has no sound");
    await waitFor(() => A.b.eval(`[...document.querySelectorAll('button')].some((x) => x.textContent.trim() === 'LIVE')`), 'alice sees LIVE');
    await A.b.eval(`[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === 'LIVE').click(), true`, true);
    const firefoxShare = await waitFor(() => A.b.eval(`(() => { const v = ${PLAYER}?.querySelector('video'); return v && v.videoWidth > 0 ? v.videoWidth + 'x' + v.videoHeight : ''; })()`), 'alice sees bob', 20000).catch((e) => e.message);
    report(/^\d+x\d+$/.test(firefoxShare), `alice's player plays bob's Firefox share at ${firefoxShare}`);
    await sleep(2000);
    await A.b.shot(`${HOME}/shot-share-alice-player.png`);
    await B.b.shot(`${HOME}/shot-share-bob-live.png`);

    // A staff mute ends bob's share, and alice's player closes.
    const muted = await A.b.eval(`(async () => (await fetch('/api/dens/${den}/members/${B.me}/voice-mute', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{"muted":true}' })).status)()`);
    await waitFor(async () => (await text(B)).includes('Staff muted you, which ended your share.'), "bob's notice", 10000).catch(() => {});
    report(muted === 204 && (await text(B)).includes('Staff muted you, which ended your share.') && !(await A.b.eval(`!!${PLAYER}`)), `a staff mute ends bob's share (${muted}), and alice's player`);
    await A.b.eval(`fetch('/api/dens/${den}/members/${B.me}/voice-mute', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{"muted":false}' }).then((r) => r.status)`);

    // A low share bitrate holds alice's Chromium to it.
    report(await setLimits({ share_bitrate: 250000 }) === 200, 'the owner sets 250 kbps');
    await sleep(500);
    await clickButton(A, 'Share your screen', true);
    await waitFor(async () => (await A.b.eval(`!!document.querySelector('[aria-label="Nobody is watching yet"]')`)), 'alice live again', 15000);
    await waitFor(() => B.b.eval(`[...document.querySelectorAll('button')].some((x) => x.textContent.trim() === 'LIVE')`), 'bob sees LIVE again');
    await B.b.eval(`[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === 'LIVE').click(), true`, true);
    await waitFor(() => B.b.eval(`(() => { const v = ${PLAYER}?.querySelector('video'); return v && v.videoWidth > 0; })()`), 'bob sees alice again', 15000).catch(() => {});
    await sleep(3000);
    const c0 = await video(A); await sleep(4000); const c1 = await video(A);
    const kbps = Math.round(((c1.sent - c0.sent) * 8) / 4 / 1000);
    const sdp = await A.b.eval(`window.__pcs.at(-1).remoteDescription.sdp`);
    const bandwidth = sdp.split('\r\n').filter((l) => l.startsWith('b='));
    const max = await A.b.eval(`(() => { const pc = window.__pcs.at(-1); const t = pc.getTransceivers().find((x) => x.sender.track?.kind === 'video'); return JSON.stringify(t?.sender.getParameters().encodings); })()`);
    report(kbps > 20 && kbps <= 275 && bandwidth.some((l) => l === 'b=TIAS:250000' || l === 'b=AS:250') && JSON.parse(max)[0].maxBitrate === 250000,
        `alice sends ${kbps} kbps against a 250 kbps cap; her offer has ${JSON.stringify(bandwidth)}; her sender ${max}`);

    // A voice channel's ⋯ opens its settings, and a new bitrate reaches the call.
    await A.b.eval(`document.querySelector('button[aria-label="Settings for Lounge"]').click(), true`);
    await waitFor(() => A.b.eval(`!!document.querySelector('input[aria-label="Voice quality"]')`), 'the voice channel dialog');
    await A.b.shot(`${HOME}/shot-share-channel-dialog.png`);
    await A.b.eval(`(() => { const i = document.querySelector('input[aria-label="Voice quality"]'); i.value = '48000'; i.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
    await sleep(200);
    await clickButton(A, 'Save');
    const rates = await waitFor(async () => {
        const got = await Promise.all([A, B].map((s) => s.b.eval(`(window.__pcs.at(-1).remoteDescription.sdp.match(/maxaveragebitrate=48000/g) || []).length`)));
        return got.every((n) => n > 0) ? JSON.stringify(got) : '';
    }, 'the call at 48 kbps', 15000).catch((e) => e.message);
    report(rates.startsWith('['), `both members' next offers ask for 48 kbps: ${rates}`);

    // A browser that sends the share's sound around its seal (M18): alice
    // stops sharing, leaves, and joins again, so her share's sections come
    // afresh, with a preload that sets the sound's transform a task late, as
    // the page did; her page stops the sound within a second of its track
    // going on, and says so, while the share goes on without it.
    await clickButton(A, 'Stop sharing your screen');
    await waitFor(async () => A.b.eval(`!!document.querySelector('button[aria-label="Share your screen"]')`), 'alice stopped sharing', 10000);
    await A.b.eval(`document.querySelector('button[aria-label="Leave the call"]').click(), true`);
    await waitFor(() => A.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), 'alice out of the call', 10000);
    await A.b.eval(`window.__lateSound = true, window.__attached.length = 0, window.__shares.length = 0, true`);
    await A.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    await waitFor(async () => (await A.b.eval(`!!document.querySelector('button[aria-label="Share your screen"]')`)) && (await A.b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`)), 'alice in the call again', 30000);
    report(await clickButton(A, 'Share your screen', true) === 'clicked', 'alice shares again, her sound sealed a task late');
    const UNSEALED = "This browser sent your share's sound unencrypted, so it was turned off.";
    const said = await waitFor(async () => (await text(A)).includes(UNSEALED), "alice's notice", 15000).catch((e) => e.message);
    const timing = JSON.parse(await A.b.eval(`(() => {
        const on = window.__attached.find((a) => a.kind === 'audio' && a.track === 2);
        const off = window.__shares.find((m) => m.on && m.sound === false);
        return JSON.stringify({ on: on?.at, off: off?.at, shares: window.__shares.map((m) => ({ on: m.on, sound: m.sound })) });
    })()`));
    const within = timing.on && timing.off ? Math.round(timing.off - timing.on) : -1;
    const late = await A.b.eval(`(() => { const t = window.__pcs.at(-1).getTransceivers().find((x) => x.sender.transform?.__options?.track === 2); return JSON.stringify({ track: t?.sender.track?.kind || null, live: !!document.querySelector('button[aria-label="Stop sharing your screen"]'), mark: !!document.querySelector('[aria-label="no sound"]') }); })()`);
    const denSays = await waitFor(async () => {
        const v = JSON.parse(await A.b.eval(STATE(A)));
        const me = v.calls?.flatMap((x) => x.members).find((m) => m.id === A.me);
        return me?.sharing && !me.sound ? JSON.stringify(me) : '';
    }, "the den's word that alice's share has no sound", 10000).catch((e) => e.message);
    report(said === true && within >= 0 && within <= 1500 && JSON.parse(late).track === null && JSON.parse(late).live && JSON.parse(late).mark && denSays.startsWith('{'),
        `alice's page stopped the sound sent around its seal ${within} ms after it went on, and says so (${said}); the share goes on without it: ${late}; the den: ${denSays}; ${JSON.stringify(timing.shares)}`);
    const warned = await waitFor(async () => (await A.b.eval(`fetch('/api/logs').then((r) => r.text())`)).match(/its browser sent its share's sound unsealed, \d+ packets, so it stopped the sound/)?.[0] || '', "alice's log", 10000).catch((e) => e.message);
    report(warned.startsWith('its browser'), `alice's log notes it: ${warned}`);
    await A.b.shot(`${HOME}/shot-share-alice-unsealed.png`);
    await A.b.eval(`window.__lateSound = false, true`);
    await clickButton(A, 'Stop sharing your screen');

    // The settings' card, and the den's call limits in its Manage dialog.
    await openSettings(A.b, 'Voice');
    await waitFor(async () => (await text(A)).includes('Screen share'), 'the share card');
    await A.b.eval(`[...${SETTINGS}.querySelectorAll('h2')].find((h) => h.textContent === 'Screen share').scrollIntoView(), true`);
    await A.b.shot(`${HOME}/shot-share-settings.png`);
    await A.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await A.b.go(`http://localhost:${A.port}/`);
    await openManage(A.b, 'Limits');
    const cost = await waitFor(() => A.b.eval(`${MANAGE}?.innerText.match(/Upload needed at full load: [^\\n]*/)?.[0] || ''`), 'the upload line', 10000).catch((e) => e.message);
    report(cost.includes('Mbps'), `the den's limits say: ${cost}`);
    await A.b.eval(`[...${MANAGE}.querySelectorAll('h4')].find((h) => h.textContent === 'Calls')?.scrollIntoView(), true`);
    await A.b.shot(`${HOME}/shot-share-den-settings.png`);
} catch (e) {
    c.fail(e);
    for (const s of [A, B, C]) {
        try {
            await s.b?.shot(`${HOME}/shot-share-${s.name}-fail.png`);
        } catch {}
    }
} finally {
    for (const s of [A, B, C]) await s.b?.close().catch(() => {});
    c.finish();
}
