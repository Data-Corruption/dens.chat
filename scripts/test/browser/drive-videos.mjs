// The M5.4 check: Bob, in the browser named, adds an iPhone's H.264 video,
// whose chip follows its copy as the module makes it, and compares the two
// versions playing in place: play, Space, the switch, seeking, both at the
// same moment. Alice, in the other browser, gets the copy, which plays.
// Then an iPhone's HEVC video, whose full size neither browser here plays,
// and a third sent full size while its copy is made. Then the setting's
// name.
//
//   run.sh videos chromium|firefox
import { ALICE, BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , bobIn] = process.argv;
const aliceIn = bobIn === 'chromium' ? 'firefox' : 'chromium';
const { DEN: den, GENERAL: general } = process.env;
const c = checks();
const ok = (what) => c.report(true, `${bobIn}: ${what}`);
const curl = (who, method, path, body) => JSON.parse(lib('api', who, method, path, ...(body ? [JSON.stringify(body)] : [])) || 'null');

const bob = await launch(bobIn, 9331, { size: [1280, 900] });
const alice = await launch(aliceIn, 9332, { size: [1280, 900] });
const chips = `[...document.querySelectorAll('ul[aria-label="Files to send"] li')].map((li) => li.innerText.replace(/\\s+/g, ' ').trim())`;
const dialog = `document.querySelector('[role=dialog][aria-label^="Compare"]')`;
const pressed = (group) => `${dialog}.querySelector('[aria-label="${group}"] [aria-pressed=true]')?.textContent.trim()`;
const players = `(() => { const d = ${dialog}; if (!d) return null; const vs = [...d.querySelectorAll('video')];
    return vs.map((v) => ({ which: v.src.split('/').pop(), shown: !v.classList.contains('opacity-0'), ready: v.readyState, paused: v.paused,
        time: v.currentTime, muted: v.muted, w: v.videoWidth, h: v.videoHeight, error: v.error ? v.error.code : 0 })); })()`;
const buttonIn = (label) => `[...${dialog}.querySelectorAll('button')].find((b) => b.textContent.trim().startsWith(${JSON.stringify(label)}) || b.getAttribute('aria-label') === ${JSON.stringify(label)})`;
const shot = (who, name) => who.shot(`${HOME}/shot-videos-${bobIn}-${name}.png`);
const one = (list, which) => list.find((p) => p.which === which);
const send = async () => {
    await bob.eval(`(() => { const t = document.querySelector('textarea'); t.focus(); return true; })()`);
    await bob.key('Enter');
};
try {
    await pair(bob, BOB);
    await pair(alice, ALICE);
    await bob.go(`http://localhost:18486/den/${den}/${general}`);
    await waitFor(() => bob.eval(`!!document.querySelector('input[type=file][multiple]')`), "bob's composer");

    // An iPhone's video: the chip follows its copy, then says it goes smaller.
    await bob.attach('input[type=file][multiple]', [`${HOME}/videos/iphone-x-h264.mov`]);
    const making = await waitFor(async () => {
        const c = await bob.eval(chips);
        return c.length === 1 && /Smaller copy · \d+%/.test(c[0]) ? c[0] : null;
    }, "the chip following the copy", 60000);
    ok(`while it's made: ${making}`);
    await shot(bob, 'making');
    const made = await waitFor(async () => {
        const c = await bob.eval(chips);
        return c.length === 1 && c[0].includes('· smaller') ? c[0] : null;
    }, 'the chip with its copy', 120000);
    ok(`made: ${made}`);
    await shot(bob, 'chip');

    // Its size opens the comparison: both versions load, the copy shows.
    await bob.eval(`document.querySelector('ul[aria-label="Files to send"] li button.link').click(), true`);
    let v = await waitFor(async () => {
        const p = await bob.eval(players);
        return p?.length === 2 && p.every((x) => x.ready >= 2) ? p : null;
    }, 'both versions loaded', 30000);
    if (!one(v, 'smaller').shown || one(v, 'full').shown || one(v, 'smaller').muted || !one(v, 'full').muted) throw new Error(`shown: ${JSON.stringify(v)}`);
    const label = await bob.eval(`${dialog}.innerText.replace(/\\s+/g, ' ')`);
    // 1440 × 1080 cropped to 1308 × 980 as its container asks, turned a
    // quarter, at about 24 frames a second. Chromium makes its own copy,
    // which fits 1080p, and the module's fits 720p (M5.5).
    const small = bobIn === 'chromium' ? 'Smaller 980 × 1308 · 24 fps' : 'Smaller 960 × 1280 · 24 fps';
    if (!label.includes(small) || !/Full size 980 × 1308 · 24 fps/.test(label)) throw new Error(`labels: ${label}`);
    ok(`both load: ${JSON.stringify(v.map((x) => [x.which, x.w, x.h]))}`);
    await shot(bob, 'compare');

    // Play runs the copy; Space shows the full size at the same moment, still playing.
    await bob.eval(`${buttonIn('Play')}.click(), true`);
    await waitFor(async () => {
        const p = await bob.eval(players);
        return !one(p, 'smaller').paused && one(p, 'smaller').time > 0.3;
    }, 'the copy playing');
    await bob.key(' ', 'down');
    v = await waitFor(async () => {
        const p = await bob.eval(players);
        return one(p, 'full').shown && !one(p, 'full').paused ? p : null;
    }, 'Space showing the full size playing');
    await bob.key(' ', 'up');
    await waitFor(async () => one(await bob.eval(players), 'smaller').shown, 'Space let go');
    // Paused, both stand at the same moment, so the switch shows the same frame.
    await bob.eval(`${buttonIn('Pause')}.click(), true`);
    v = await waitFor(async () => {
        const p = await bob.eval(players);
        return p.every((x) => x.paused) && Math.abs(one(p, 'smaller').time - one(p, 'full').time) < 0.05 ? p : null;
    }, 'both paused at the same moment');
    ok(`paused at ${one(v, 'smaller').time.toFixed(2)} s and ${one(v, 'full').time.toFixed(2)} s`);
    // Seeking moves both.
    await bob.eval(`(() => { const r = ${dialog}.querySelector('input[type=range]'); r.value = '1.5'; r.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
    v = await waitFor(async () => {
        const p = await bob.eval(players);
        return p.every((x) => Math.abs(x.time - 1.5) < 0.05) ? p : null;
    }, 'a seek moving both');
    await bob.eval(`${buttonIn('Full size')}.click(), true`);
    await waitFor(async () => one(await bob.eval(players), 'full').shown, 'the switch showing full size');
    await shot(bob, 'full');
    ok('play, Space, pause, seek and the switch keep both versions at one moment');
    await bob.key('Escape');
    await waitFor(() => bob.eval(`!${dialog}`), 'the comparison closed');

    // Sent, alice gets the copy, and it plays.
    await send();
    const copy = await waitFor(() => {
        const page = curl('alice', 'GET', `/api/dens/${den}/channels/${general}/messages?limit=5`);
        const m = page.messages.find((x) => (x.attachments || []).some((f) => f.name === 'iphone-x-h264.mov'));
        return m ? m.attachments[0] : null;
    }, "alice's copy of the message");
    const wide = bobIn === 'chromium' ? 980 : 960;
    if (copy.type !== 'video/mp4' || copy.width !== wide || !copy.thumb) throw new Error(`alice got ${JSON.stringify(copy)}`);
    await alice.go(`http://localhost:18484/den/${den}/${general}`);
    const playing = await waitFor(() => alice.eval(`(async () => { const v = [...document.querySelectorAll('video')].pop(); if (!v) return null;
        v.muted = true; await v.play().catch(() => {}); await new Promise((r) => setTimeout(r, 800));
        return v.currentTime > 0.2 && v.videoWidth > 0 ? [v.videoWidth, v.videoHeight, +v.currentTime.toFixed(2)] : null; })()`), "alice's copy playing", 30000);
    ok(`alice's copy plays: ${JSON.stringify(playing)}, ${copy.size} bytes`);
    await shot(alice, 'received');

    // An iPhone's HEVC video: where the browser can't decode its full size,
    // as Chromium without graphics for it, the copy shows alone, saying why;
    // Firefox on Linux decodes HEVC with the system's FFmpeg, and shows both.
    await bob.attach('input[type=file][multiple]', [`${HOME}/videos/iphone-12-pro-hevc.mov`]);
    await waitFor(async () => {
        const c = await bob.eval(chips);
        return c.length === 1 && c[0].includes('· smaller') ? c[0] : null;
    }, 'the HEVC video with its copy', 240000);
    await bob.eval(`document.querySelector('ul[aria-label="Files to send"] li button.link').click(), true`);
    v = await waitFor(async () => {
        const p = await bob.eval(players);
        const full = p && one(p, 'full');
        return p && one(p, 'smaller').ready >= 2 && (full.error || full.ready >= 2) ? p : null;
    }, "the HEVC video's versions loading", 30000);
    if (one(v, 'full').error || one(v, 'full').w === 0) {
        await waitFor(() => bob.eval(`${dialog}?.innerText.includes("can't play the full size") && ${buttonIn('Full size')}.disabled`), 'the full size refused, with why', 10000);
        if (!one(await bob.eval(players), 'smaller').shown) throw new Error('the copy is not what shows');
        ok(`HEVC: this browser can't play the full size, so the copy shows alone, ${one(v, 'smaller').w} × ${one(v, 'smaller').h}`);
    } else {
        if (await bob.eval(`${dialog}.innerText.includes("can't play") || ${buttonIn('Full size')}.disabled`)) throw new Error('a full size that plays, refused');
        ok(`HEVC: this browser plays the full size too, ${one(v, 'full').w} × ${one(v, 'full').h}, beside the copy, ${one(v, 'smaller').w} × ${one(v, 'smaller').h}`);
    }
    await shot(bob, 'hevc');
    await bob.key('Escape');
    await bob.eval(`document.querySelector('ul[aria-label="Files to send"] li button[aria-label^="Remove"]').click(), true`);

    // The HEVC goes full size instead of waiting for its copy, which the
    // module makes in either browser here, once the den takes its 33 MB.
    const limits = curl('alice', 'GET', `/api/dens/${den}/state`).limits;
    curl('alice', 'POST', `/api/dens/${den}/settings`, { limits: { ...limits, file_size: 64 << 20 } });
    await waitFor(() => curl('bob', 'GET', `/api/dens/${den}/state`).limits.file_size === 64 << 20, "bob's view of the limit");
    await bob.attach('input[type=file][multiple]', [`${HOME}/videos/iphone-12-pro-hevc.mov`]);
    await waitFor(() => bob.eval(`(() => { const b = [...document.querySelectorAll('ul[aria-label="Files to send"] li button')]
        .find((x) => x.textContent.includes('Send full size instead')); if (b) b.click(); return !!b; })()`), 'the offer to send it full size', 60000);
    const full = await waitFor(async () => {
        const c = await bob.eval(chips);
        return c.length === 1 && /33 MB/.test(c[0]) && !c[0].includes('smaller') ? c[0] : null;
    }, 'the chip at full size', 120000);
    ok(`sent full size instead: ${full}`);
    await bob.eval(`document.querySelector('ul[aria-label="Files to send"] li button[aria-label^="Remove"]').click(), true`);
    curl('alice', 'POST', `/api/dens/${den}/settings`, { limits });

    // The setting names videos too.
    await bob.eval(`document.querySelector('button[aria-label="Settings"]').click(), true`);
    await waitFor(() => bob.eval(`[...document.querySelectorAll('[aria-label=Settings] label')].some((l) => l.textContent.includes('Send photos and videos smaller'))`), 'the setting');
    await shot(bob, 'setting');
    ok('the setting reads "Send photos and videos smaller"');
} catch (e) {
    c.fail(e);
} finally {
    await bob.close();
    await alice.close();
    c.finish();
}
