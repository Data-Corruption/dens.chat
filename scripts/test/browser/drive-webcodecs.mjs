// The M5.5 check: Bob, in the browser named, adds videos. In Chromium, the
// page makes an H.264 video's copy itself, up to 1080p, where the module
// goes to 720p; declines an iPhone's HEVC, which it can't decode without
// graphics for it, so the module makes that copy; and makes a WebM's VP9
// copy, which the module can't. In Firefox, the module makes the copies,
// and the WebM goes full size. Alice, in the other browser, plays what
// arrives.
//
//   run.sh webcodecs chromium|firefox
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
const shot = (who, name) => who.shot(`${HOME}/shot-webcodecs-${bobIn}-${name}.png`);
const page = bobIn === 'chromium';

// add adds a video, waits for its chip to settle, and returns what the
// comparison says of it, or the chip alone without versions.
async function add(file, waitMs) {
    await bob.attach('input[type=file][multiple]', [`${HOME}/videos/${file}`]);
    const t0 = Date.now();
    const seen = new Set();
    const chip = await waitFor(async () => {
        const c = await bob.eval(chips);
        if (c.length !== 1) return null;
        const m = c[0].match(/Smaller copy · \d+%|Sending · \d+%/);
        if (m) seen.add(m[0].split(' · ')[0]);
        return /\d+(\.\d+)? (KB|MB) · (smaller|full size)|\d+(\.\d+)? (KB|MB)(?! ·)/.test(c[0]) && !/Smaller copy|Sending/.test(c[0]) ? c[0] : null;
    }, `${file}'s chip`, waitMs);
    const took = ((Date.now() - t0) / 1000).toFixed(1);
    let label = '';
    if (chip.includes('· smaller') || chip.includes('· full size')) {
        await bob.eval(`document.querySelector('ul[aria-label="Files to send"] li button.link').click(), true`);
        label = await waitFor(() => bob.eval(`${dialog}?.innerText.replace(/\\s+/g, ' ')`), 'the comparison');
        await bob.key('Escape');
        await waitFor(() => bob.eval(`!${dialog}`), 'the comparison closed');
    }
    return { chip, label, took, seen: [...seen] };
}

async function sendIt() {
    await bob.eval(`(() => { const t = document.querySelector('textarea'); t.focus(); return true; })()`);
    await bob.key('Enter');
    await waitFor(async () => (await bob.eval(chips)).length === 0, 'the message sent');
}

try {
    await pair(bob, BOB);
    await pair(alice, ALICE);
    await bob.go(`http://localhost:18486/den/${den}/${general}`);
    await waitFor(() => bob.eval(`!!document.querySelector('input[type=file][multiple]')`), "bob's composer");

    // An iPhone's H.264: Chromium's own copy keeps its 980 × 1308, which
    // fits 1080p; the module's fits 720p.
    let r = await add('iphone-x-h264.mov', 120000);
    const want = page ? 'Smaller 980 × 1308' : 'Smaller 960 × 1280';
    if (!r.label.includes(want)) throw new Error(`the H.264 copy: ${r.label}`);
    ok(`H.264, copied by ${page ? 'the page' : 'the module'} in ${r.took} s: ${r.chip} — ${r.label.match(/Smaller [^·]+·[^·]+·[^·]+/)[0]} (saw ${r.seen.join(', ')})`);
    await shot(bob, 'h264');
    await sendIt();
    const copy = await waitFor(() => {
        const p = curl('alice', 'GET', `/api/dens/${den}/channels/${general}/messages?limit=5`);
        const m = p.messages.find((x) => (x.attachments || []).some((f) => f.name === 'iphone-x-h264.mov'));
        return m ? m.attachments[0] : null;
    }, "alice's copy of the H.264");
    await alice.go(`http://localhost:18484/den/${den}/${general}`);
    const playing = await waitFor(() => alice.eval(`(async () => { const v = [...document.querySelectorAll('video')].pop(); if (!v) return null;
        v.muted = true; await v.play().catch(() => {}); await new Promise((r) => setTimeout(r, 800));
        return v.currentTime > 0.2 && v.videoWidth > 0 ? [v.videoWidth, v.videoHeight, +v.currentTime.toFixed(2)] : null; })()`), "alice's copy playing", 30000);
    ok(`alice plays it: ${JSON.stringify(playing)}, ${copy.width} × ${copy.height}, ${copy.size} bytes, preview ${!!copy.thumb}`);

    // An iPhone's HEVC: no decoder for it here, so the module makes the copy.
    r = await add('iphone-12-pro-hevc.mov', 300000);
    if (!r.label.includes('Smaller 720 × 1280')) throw new Error(`the HEVC copy: ${r.label}`);
    ok(`HEVC, copied by the module in ${r.took} s: ${r.label.match(/Smaller [^·]+·[^·]+·[^·]+/)[0]}`);
    await bob.eval(`document.querySelector('ul[aria-label="Files to send"] li button[aria-label^="Remove"]').click(), true`);

    // A WebM's VP9, which the module doesn't decode: Chromium copies it,
    // and in Firefox it goes full size.
    r = await add('vp9.webm', 120000);
    if (page ? !r.label.includes('Smaller 640 × 360') : r.label !== '' || !/4\d\d KB/.test(r.chip)) throw new Error(`the WebM: ${r.chip} ${r.label}`);
    ok(page ? `VP9, copied by the page: ${r.label.match(/Smaller [^·]+·[^·]+·[^·]+/)[0]}` : `VP9 goes full size: ${r.chip}`);
    await shot(bob, 'vp9');
    await sendIt();
    await alice.go(`http://localhost:18484/den/${den}/${general}`);
    const webm = await waitFor(() => alice.eval(`(async () => { const v = [...document.querySelectorAll('video')].pop(); if (!v) return null;
        v.muted = true; await v.play().catch(() => {}); await new Promise((r) => setTimeout(r, 800));
        return v.currentTime > 0.2 && v.videoWidth > 0 ? [v.videoWidth, v.videoHeight] : null; })()`), "alice's WebM playing", 30000);
    ok(`alice plays the WebM's: ${JSON.stringify(webm)}`);
} catch (e) {
    c.fail(e);
} finally {
    await bob.close();
    await alice.close();
    c.finish();
}
