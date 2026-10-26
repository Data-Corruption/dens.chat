// The M11.3 check: bob in the browser named, alice in Chromium, in a call,
// both sending by push to talk. Held, push to talk rings for the call's
// other member, in silence too, as the den passes it on, and never while
// muted; a share bob watches opens in a window of its own, plays there with
// its controls and his voice keys, and comes back to the player as the
// window closes, by the player's button, and as watching or the share
// stops; and the logs download from the settings with what the call
// noted, on bob's side and the den's.
//
//   run.sh talk chromium|firefox
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, SETTINGS, checks, launch, openSettings, pair, waitFor } from './browsers.mjs';

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
const DOWNLOADS = `${HOME}/downloads`;
const c = checks();
const report = c.report;
const A = ALICE;
const B = BOB;
A.display = 'Alice';
B.display = 'Bob';
const PLAYER = `document.querySelector('section[aria-label="Screen shares"]')`;
// rings says whether a page's call list rings a member, by name.
const rings = (s, name) => s.b.eval(`[...document.querySelectorAll('ul[aria-label="In the call"] li')].some((li) => li.textContent.includes(${JSON.stringify(name)}) && li.textContent.includes('speaking'))`);
const KEY = (type) => `(document.body.dispatchEvent(new KeyboardEvent('${type}', { code: 'KeyV', key: 'v', bubbles: true })), true)`;
const button = (label) => `[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(label)} || x.getAttribute('aria-label') === ${JSON.stringify(label)})`;
const click = (s, label, gesture = false) => s.b.eval(`(() => { const b = ${button(label)}; if (!b) return 'missing'; if (b.disabled) return 'disabled: ' + b.title; b.click(); return 'clicked'; })()`, gesture);
// A video plays at its size, as a page or a window shows it.
const PLAYS = (video) => `(() => { const v = ${video}; return v && v.videoWidth > 0 && !v.paused ? v.videoWidth + 'x' + v.videoHeight : ''; })()`;
const IN_PLAYER = PLAYS(`${PLAYER}?.querySelector('video')`);
const IN_WINDOW = PLAYS(`document.querySelector('video')`);
const LIVE = `[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === 'LIVE')`;
const sized = (x) => /^\d+x\d+$/.test(x);
// silence quiets a page's microphone, so its own meter hears nothing, and
// has it send nothing, so the other's doesn't either; or brings both back.
const silence = (s, on) => s.b.eval(on
    ? `(async () => {
        window.__mics.forEach((m) => m.getAudioTracks().forEach((t) => (t.enabled = false)));
        const t = window.__pcs.at(-1).getTransceivers()[0].sender;
        window.__voice = t.track;
        await t.replaceTrack(null);
        return true;
    })()`
    : `(async () => {
        window.__mics.forEach((m) => m.getAudioTracks().forEach((t) => (t.enabled = true)));
        await window.__pcs.at(-1).getTransceivers()[0].sender.replaceTrack(window.__voice);
        return true;
    })()`);

try {
    fs.rmSync(DOWNLOADS, { recursive: true, force: true });
    fs.mkdirSync(DOWNLOADS, { recursive: true });
    A.b = await launch('chromium', 9533, { media: true, screen: true, preload: PRELOAD });
    B.b = await launch(which, 9534, { media: which === 'firefox' ? 'pulse' : true, preload: PRELOAD, downloads: DOWNLOADS, hover: true });
    for (const s of [A, B]) {
        await pair(s.b, s);
        // Both send by push to talk, on V.
        const keys = await s.b.eval(`(async () => {
            localStorage.setItem('DENS_VOICE', JSON.stringify({ mode: 'ptt', auto: true, threshold: -50 }));
            const r = await fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ voiceKeys: { ptt: { code: 'KeyV', label: 'V' } } }) });
            return r.status;
        })()`);
        if (keys !== 200) throw new Error(`${s.name}'s keys: ${keys}`);
        await s.b.go(`http://localhost:${s.port}/den/${den}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), `${s.name} voice channel`);
        await s.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    }
    for (const s of [A, B]) {
        await waitFor(() => s.b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), `${s.name} in the call`, 30000);
    }
    await waitFor(async () => (await A.b.eval(`document.querySelectorAll('ul[aria-label="In the call"] li').length`)) === 2, 'both in the call');

    // Push to talk in silence: neither page sends a sound, so only the den's
    // word rings one for the other.
    for (const s of [A, B]) await silence(s, true);
    await sleep(1500);
    for (const [s, o] of [[B, A], [A, B]]) {
        const before = await rings(o, s.display);
        await s.b.eval(KEY('keydown'));
        const own = await waitFor(() => rings(s, s.display), `${s.name}'s own ring`, 1000).catch(() => false);
        const t0 = Date.now();
        const lit = await waitFor(() => rings(o, s.display), `${o.name}'s ring for ${s.name}`, 5000).catch(() => false);
        const after = Date.now() - t0;
        await sleep(1500);
        const held = await rings(o, s.display);
        await s.b.eval(KEY('keyup'));
        const ownOut = await waitFor(async () => !(await rings(s, s.display)), `${s.name}'s own ring to go out`, 1000).catch(() => false);
        const out = await waitFor(async () => !(await rings(o, s.display)), `${s.name}'s ring to go out`, 5000).catch(() => false);
        report(!before && own === true && lit === true && held && out === true && ownOut === true,
            `${s.name} holds push to talk in silence: rings on their page at once (${own}), on ${o.name}'s ${after} ms later (${lit}) and on (${held}), and goes out as they let go (${out}, ${ownOut})`);
        if (s === B) await A.b.shot(`${HOME}/shot-talk-${which}-ring.png`).catch(() => {});
    }
    // Muted, it rings nowhere.
    report(await click(B, 'Mute') === 'clicked', 'bob mutes');
    await sleep(500);
    await B.b.eval(KEY('keydown'));
    await sleep(2000);
    const muted = [await rings(A, 'Bob'), await rings(B, 'Bob')];
    await B.b.eval(KEY('keyup'));
    await click(B, 'Unmute');
    report(muted.every((r) => !r), `muted, bob's push to talk rings on neither page: ${JSON.stringify(muted)}`);
    for (const s of [A, B]) await silence(s, false);

    // Alice shares, and bob watches.
    report(await click(A, 'Share your screen', true) === 'clicked', 'alice shares her screen');
    await waitFor(() => A.b.eval(`!!document.querySelector('[aria-label="Nobody is watching yet"]')`), 'alice live', 15000);
    const watch = async () => {
        await waitFor(() => B.b.eval(`!!${LIVE}`), 'bob sees LIVE');
        await B.b.eval(`${LIVE}.click(), true`, true);
        return waitFor(() => B.b.eval(IN_PLAYER), "alice's screen in bob's player", 15000).catch((e) => e.message);
    };
    const first = await watch();
    report(sized(first), `bob's player plays alice's screen at ${first}`);

    // Its own window plays it, with its controls, and the player says where
    // it went.
    const OPEN = "Open Alice's screen in a window of its own";
    const windows = (n) => waitFor(async () => (await B.b.windows()) === n, `${n} windows`, 10000).then(() => true, () => false);
    report(await click(B, OPEN, true) === 'clicked', 'bob opens the share in a window of its own');
    const opened = await windows(1);
    const there = await waitFor(() => B.b.windowEval(IN_WINDOW), "alice's screen in the window", 15000).catch((e) => e.message);
    // The page's stylesheet loads in the window as in the page.
    await waitFor(() => B.b.windowEval(`getComputedStyle(document.body).backgroundColor === 'rgb(0, 0, 0)'`), "the window's style", 5000).catch(() => {});
    const look = JSON.parse(await B.b.windowEval(`JSON.stringify({
        title: document.title, theme: document.documentElement.getAttribute('data-theme'), black: getComputedStyle(document.body).backgroundColor,
        buttons: [...document.querySelectorAll('button')].map((b) => b.getAttribute('aria-label') || b.textContent.trim()),
        fills: Math.round(document.querySelector('video').getBoundingClientRect().width) === window.innerWidth,
    })`));
    const away = JSON.parse(await B.b.eval(`JSON.stringify({ said: ${PLAYER}?.innerText.includes("Alice's screen is in a window of its own"), back: !!${button('Bring it back')}, video: !!${PLAYER}?.querySelector('video') })`));
    report(opened && sized(there) && look.title === "Alice's screen" && look.theme && look.black === 'rgb(0, 0, 0)' && look.fills &&
        look.buttons.includes("Stop watching Alice's screen") && !look.buttons.includes(OPEN) && away.said && away.back && !away.video,
        `the window plays alice's screen at ${there}: ${JSON.stringify(look)}; the player keeps its place ${JSON.stringify(away)}`);
    await B.b.windowShot(`${HOME}/shot-talk-${which}-window.png`).catch((e) => console.log('shot', e.message));
    await B.b.shot(`${HOME}/shot-talk-${which}-away.png`);
    // His voice keys work while it has focus.
    await B.b.windowEval(KEY('keydown'));
    const keyed = await waitFor(() => rings(A, 'Bob'), "bob's ring from his window's key", 5000).catch(() => false);
    await B.b.windowEval(KEY('keyup'));
    const unkeyed = await waitFor(async () => !(await rings(A, 'Bob')), "bob's ring out", 5000).catch(() => false);
    report(keyed === true && unkeyed === true, `push to talk from the window rings bob for alice (${keyed}), and lets go (${unkeyed})`);

    // Closing the window brings the share back to the player, and so does
    // the player's button.
    await B.b.closeWindow();
    const closedBack = await waitFor(() => B.b.eval(IN_PLAYER), 'the share back in the player', 10000).catch((e) => e.message);
    report(sized(closedBack) && await windows(0), `closing the window brings the share back to the player: ${closedBack}`);
    await click(B, OPEN, true);
    await windows(1);
    await waitFor(() => B.b.eval(`!!${button('Bring it back')}`), 'its place in the player', 5000);
    await click(B, 'Bring it back');
    const broughtBack = await waitFor(() => B.b.eval(IN_PLAYER), 'the share back again', 10000).catch((e) => e.message);
    report(await windows(0) && sized(broughtBack), `Bring it back closes the window, and the player plays the share: ${broughtBack}`);

    // Stopping watching in the window closes it, and the player; the share
    // ending closes it too.
    await click(B, OPEN, true);
    await windows(1);
    await waitFor(() => B.b.windowEval(`!!document.querySelector('button[aria-label="Stop watching Alice\\'s screen"]')`), "the window's controls", 10000);
    await B.b.windowEval(`document.querySelector('button[aria-label="Stop watching Alice\\'s screen"]').click(), true`);
    const stopped = await windows(0) && !(await B.b.eval(`!!${PLAYER}`));
    report(stopped, `stopping watching in the window closes it, and the player: ${stopped}`);
    const again = await watch();
    await click(B, OPEN, true);
    const reopened = await windows(1);
    report(await click(A, 'Stop sharing your screen') === 'clicked', 'alice stops sharing');
    const ended = await windows(0);
    report(sized(again) && reopened && ended, `the share ending closes its window: watched again at ${again}, opened ${reopened}, closed ${ended}`);

    // Bob's logs download from the settings, and say what the call did on
    // his side, naming no address.
    await openSettings(B.b, 'System');
    const LINK = `${SETTINGS}.querySelector('a[href="/api/logs"]')`;
    await waitFor(() => B.b.eval(`!!${LINK}`), 'the download');
    const card = await B.b.eval(`${LINK}.closest('section').innerText.replace(/\\s+/g, ' ')`);
    await B.b.eval(`${LINK}.scrollIntoView(), true`);
    await B.b.shot(`${HOME}/shot-talk-${which}-logging.png`);
    await B.b.eval(`${LINK}.click(), true`, true);
    const file = await waitFor(() => fs.readdirSync(DOWNLOADS).find((f) => /^dens-m2b-\d{8}-\d{6}-logs\.txt$/.test(f)), 'the logs downloaded', 20000);
    await waitFor(() => !fs.readdirSync(DOWNLOADS).some((f) => /\.(part|crdownload)$/.test(f)), 'the download to finish', 20000);
    const logs = fs.readFileSync(`${DOWNLOADS}/${file}`, 'utf8');
    const ours = ["'s stream opened", 'asked to join it', 'offer 1 came', 'answer 1 went', 'its connection is connected, its ICE', 'over udp, from a host candidate'];
    const missing = ours.filter((x) => !logs.includes(x));
    const address = logs.match(/\b(?:\d{1,3}\.){3}\d{1,3}\b/)?.[0];
    report(card.includes('They hold no messages, keys or addresses') && !missing.length && !address,
        `bob downloads ${file}, ${logs.length} bytes, with his call's signaling and notes (missing ${JSON.stringify(missing)}) and no address (${address}); the card says: ${card}`);
    // The den's, alice's install's, have the call's ICE and DTLS.
    const denLogs = await A.b.eval(`fetch('/api/logs').then((r) => r.text())`);
    const theirs = ['The call of member', ': ICE connected', ': DTLS connected', 'its path is udp', 'answered after'];
    const denMissing = theirs.filter((x) => !denLogs.includes(x));
    report(!denMissing.length, `the den's logs follow the call's ICE and DTLS, ${denLogs.length} bytes (missing ${JSON.stringify(denMissing)})`);
} catch (e) {
    c.fail(e);
    for (const s of [A, B]) {
        try {
            await s.b?.shot(`${HOME}/shot-talk-${which}-${s.name}-fail.png`);
        } catch {}
    }
} finally {
    for (const s of [A, B]) await s.b?.close().catch(() => {});
    c.finish();
}
