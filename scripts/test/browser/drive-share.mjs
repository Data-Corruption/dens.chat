// The M4.2 check: Chromium (alice, the owner) shares its fake screen, and
// Firefox (bob), headed on Xvfb, the X screen; each watches the other's in
// the player. Then the share's limits, a staff mute, and a voice channel's
// bitrate.
//
//   run.sh share
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, checks, launch, pair, waitFor } from './browsers.mjs';

const { DEN: den } = process.env;
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
    for (const s of [A, B]) {
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
    for (const s of [A, B]) await waitFor(async () => (await s.b.eval(`!!document.querySelector('button[aria-label="Share your screen"]')`)) && (await s.b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`)), `${s.name} in the call`, 30000);
    // The share button sits in the call's row, between the speaker and mute.
    const row = await A.b.eval(`(() => { const b = document.querySelector('button[aria-label="Share your screen"]'); const all = [...b.parentElement.children].map((x) => x.getAttribute('aria-label') || x.querySelector('[role=status]')?.textContent || x.tagName); return JSON.stringify(all); })()`);
    const order = JSON.parse(row);
    report(order.indexOf('Share your screen') === 1 && order[2] === 'Mute', `both in the call; the share button sits between the speaker and mute: ${row}`);

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
    const sound = await B.b.eval(`(() => { const v = ${PLAYER}.querySelector('video'); return JSON.stringify({ audio: v.srcObject?.getAudioTracks().length, video: v.srcObject?.getVideoTracks().length, muted: v.muted, volume: v.volume }); })()`);
    report(JSON.parse(sound).audio === 1, `the share's sound comes with it: ${sound}`);
    await B.b.shot(`${HOME}/shot-share-bob-player.png`);

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

    // The settings' card, and the den's call limits on the home page.
    await A.b.eval(`document.querySelector('[aria-label=Settings]').click(), true`);
    await waitFor(() => A.b.eval(`!!document.querySelector('[role=dialog][aria-label=Settings]')`), 'settings');
    await A.b.eval(`[...document.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim() === 'Voice').click(), true`);
    await waitFor(async () => (await text(A)).includes('Sharing your screen'), 'the share card');
    await A.b.eval(`[...document.querySelectorAll('h2')].find((h) => h.textContent === 'Sharing your screen').scrollIntoView(), true`);
    await A.b.shot(`${HOME}/shot-share-settings.png`);
    await A.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await A.b.go(`http://localhost:${A.port}/`);
    await waitFor(() => A.b.eval(`[...document.querySelectorAll('summary')].some((s) => s.textContent.includes('Den settings'))`), 'den settings');
    await A.b.eval(`(() => { const d = [...document.querySelectorAll('summary')].find((s) => s.textContent.includes('Den settings')).parentElement; d.open = true; d.dispatchEvent(new Event('toggle')); return true; })()`);
    const cost = await waitFor(async () => (await text(A)).match(/At most, shares take [^\n]*/)?.[0] || '', 'the cost line', 10000).catch((e) => e.message);
    report(cost.includes('0.3 Mbps') || cost.includes('Mbps'), `the den's settings say: ${cost}`);
    await A.b.eval(`[...document.querySelectorAll('h3')].find((h) => h.textContent.includes('Calls and screen shares'))?.scrollIntoView(), true`);
    await A.b.shot(`${HOME}/shot-share-den-settings.png`);
} catch (e) {
    c.fail(e);
    for (const s of [A, B]) {
        try {
            await s.b?.shot(`${HOME}/shot-share-${s.name}-fail.png`);
        } catch {}
    }
} finally {
    for (const s of [A, B]) await s.b?.close().catch(() => {});
    c.finish();
}
