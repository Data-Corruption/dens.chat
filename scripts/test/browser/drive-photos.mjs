// The M5.3 check: Bob, in the browser named, adds three phone photos,
// which go as smaller copies, and compares them in the viewer: fitted and
// at 100%, flipping with the switch and with Space, dragging, → to the
// next, and sending one full size. Alice, in the other browser, gets them.
// Then the setting, and a photo over a lowered limit.
//
//   run.sh photos chromium|firefox
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
const picture = `(() => { const d = ${dialog}; const box = d && d.querySelector('img')?.parentElement; if (!box) return null;
    const r = box.getBoundingClientRect(); const imgs = [...box.querySelectorAll('img')];
    return { left: r.left, top: r.top, width: r.width, height: r.height, dpr: devicePixelRatio,
        shown: imgs.find((i) => !i.classList.contains('opacity-0'))?.src.split('/').pop(),
        loaded: imgs.every((i) => i.complete && i.naturalWidth > 0), natural: imgs.map((i) => [i.src.split('/').pop(), i.naturalWidth, i.naturalHeight]) }; })()`;
const buttonIn = (label) => `[...${dialog}.querySelectorAll('button')].find((b) => b.textContent.trim().startsWith(${JSON.stringify(label)}))`;
const shot = (who, name) => who.shot(`${HOME}/shot-photos-${bobIn}-${name}.png`);
try {
    await pair(bob, BOB);
    await pair(alice, ALICE);
    await bob.go(`http://localhost:18486/den/${den}/${general}`);
    await waitFor(() => bob.eval(`!!document.querySelector('input[type=file][multiple]')`), "bob's composer");

    // Three phone photos go as smaller copies; each chip says so, with its size.
    await bob.attach('input[type=file][multiple]', [`${HOME}/photos/pixel-2.jpg`, `${HOME}/photos/iphone-xr.heic`, `${HOME}/photos/iphone-5.jpg`]);
    const added = await waitFor(async () => {
        const c = await bob.eval(chips);
        return c.length === 3 && c.every((t) => t.includes('· smaller')) ? c : null;
    }, 'three chips with their copies', 60000);
    ok(`chips: ${added.join(' | ')}`);
    await shot(bob, 'chips');

    // The first chip's size opens the comparison, fitted, on the copy.
    await bob.eval(`document.querySelector('ul[aria-label="Files to send"] li button.link').click(), true`);
    let p = await waitFor(async () => {
        const v = await bob.eval(picture);
        return v?.loaded ? v : null;
    }, 'both versions loaded');
    if (p.shown !== 'smaller' || !(await bob.eval(pressed('Version shown')))?.startsWith('Smaller')) throw new Error(`shown: ${JSON.stringify(p)}`);
    if (Math.abs(p.height - (await bob.eval(`${dialog}.firstElementChild.clientHeight`))) > 1) throw new Error(`fitted at ${p.width} × ${p.height}`);
    const natural = Object.fromEntries(p.natural.map(([w, x, y]) => [w, [x, y]]));
    if (natural.full[0] !== 3024 || natural.full[1] !== 4032 || natural.smaller[0] !== 1920 || natural.smaller[1] !== 2560) {
        throw new Error(`the versions' pixels: ${JSON.stringify(natural)}`);
    }
    if (!(await bob.eval(`${dialog}.innerText.includes('1 of 3') && ${dialog}.innerText.includes('You use')`))) throw new Error('no count or space line');
    ok(`fitted ${Math.round(p.width)} × ${Math.round(p.height)}, the copy 1920 × 2560 and full size 3024 × 4032 loaded`);
    await shot(bob, 'fit');

    // Holding Space shows the other version while it's held, whatever has focus.
    await bob.key(' ', 'down');
    await waitFor(async () => (await bob.eval(picture)).shown === 'full', 'Space held showing full size');
    await bob.key(' ', 'up');
    await waitFor(async () => (await bob.eval(picture)).shown === 'smaller', 'Space let go showing the copy');
    if (!(await bob.eval(`!!${dialog}`))) throw new Error('Space closed the comparison');
    // The switch flips it in place.
    await bob.eval(`${buttonIn('Full size')}.click(), true`);
    await waitFor(async () => (await bob.eval(picture)).shown === 'full', 'the switch showing full size');
    ok('Space and the switch flip between the versions in place');

    // 100% shows each of the photo's pixels on one of the screen's, and drags.
    await bob.eval(`${buttonIn('100%')}.click(), true`);
    p = await waitFor(async () => {
        const v = await bob.eval(picture);
        return Math.abs(v.width - 3024 / v.dpr) < 1 ? v : null;
    }, 'the 100% view');
    if (await bob.eval(pressed('Zoom')) !== '100%' || !(await bob.eval(pressed('Version shown'))).startsWith('Full size')) {
        throw new Error(`pressed: ${await bob.eval(pressed('Zoom'))}, ${await bob.eval(pressed('Version shown'))}`);
    }
    await shot(bob, 'actual');
    const before = p;
    await bob.drag(640, 400, 440, 250);
    p = await waitFor(async () => {
        const v = await bob.eval(picture);
        return Math.abs(v.left - before.left + 200) < 2 && Math.abs(v.top - before.top + 150) < 2 ? v : null;
    }, 'the drag moving the photo');
    ok(`100% is ${Math.round(p.width)} × ${Math.round(p.height)}, and a drag moved it by -200, -150`);
    // A click at 100% fits it again.
    await bob.click(640, 400);
    await waitFor(async () => (await bob.eval(pressed('Zoom'))) === 'Fit', 'a click fitting it again');

    // → goes to the next photo, which starts on the version it goes as.
    await bob.key('ArrowRight');
    await waitFor(() => bob.eval(`${dialog}.innerText.includes('2 of 3') && ${dialog}.innerText.includes('iphone-xr.jpg')`), '→ to the HEIC');
    p = await waitFor(async () => {
        const v = await bob.eval(picture);
        return v?.loaded && v.shown === 'smaller' ? v : null;
    }, "the HEIC's versions");
    ok(`→ moved to 2 of 3: ${JSON.stringify(p.natural)}`);

    // Sending it full size uploads that, and the chip says so.
    await bob.eval(`${buttonIn('Send full size instead')}.click(), true`);
    await waitFor(() => bob.eval(`!!${buttonIn('Send smaller instead')}`), 'the button turning to smaller');
    const after = await waitFor(async () => {
        const c = await bob.eval(chips);
        return c[1].includes('· full size') ? c : null;
    }, 'the chip saying full size');
    await shot(bob, 'switched');
    ok(`sent full size instead: ${after[1]}`);
    await bob.key('Escape');
    await waitFor(() => bob.eval(`!${dialog}`), 'the comparison closed');

    // Sent, alice gets two copies and the full-size HEIC.
    await bob.eval(`(() => { const t = document.querySelector('textarea'); t.focus(); return true; })()`);
    await bob.key('Enter');
    const message = await waitFor(() => {
        const page = curl('alice', 'GET', `/api/dens/${den}/channels/${general}/messages?limit=5`);
        const m = page.messages.find((x) => (x.attachments || []).length === 3);
        return m ? m.attachments.map((f) => [f.name, f.width, f.height, f.size]) : null;
    }, "alice's copy of the message");
    if (message[0][2] !== 2560 || message[1][1] !== 3024 || message[1][2] !== 4032 || message[2][1] !== 2560) {
        throw new Error(`what alice got: ${JSON.stringify(message)}`);
    }
    ok(`alice got ${JSON.stringify(message)}`);
    await alice.go(`http://localhost:18484/den/${den}/${general}`);
    await waitFor(() => alice.eval(`document.querySelectorAll('img[src*="/thumb"]').length >= 3`), "alice's previews");
    await shot(alice, 'received');

    // With copies off, a photo goes full size, and still compares.
    await bob.eval(`document.querySelector('button[aria-label="Settings"]').click(), true`);
    const toggle = `[...document.querySelectorAll('[aria-label=Settings] label')].find((l) => l.textContent.includes('Send photos and videos smaller'))?.querySelector('input')`;
    await waitFor(() => bob.eval(`!!${toggle}`), 'the setting');
    await shot(bob, 'setting');
    await bob.eval(`${toggle}.click(), true`);
    if (await bob.eval(`${toggle}.checked`)) throw new Error('the setting stayed on');
    await bob.eval(`document.querySelector('[aria-label=Settings] button[aria-label=Close]').click(), true`);
    await bob.attach('input[type=file][multiple]', [`${HOME}/photos/iphone-5.jpg`]);
    const off = await waitFor(async () => {
        const c = await bob.eval(chips);
        return c.length === 1 && c[0].includes('· full size') ? c : null;
    }, 'a chip at full size');
    ok(`with copies off: ${off[0]}`);
    if (await bob.eval(`localStorage.getItem('DENS_SEND_SMALLER')`) !== '0') throw new Error('the browser kept no choice');
    await bob.eval(`document.querySelector('button[aria-label="Remove iphone-5.jpg"]').click(), true`);

    // Over a lowered limit, a photo goes as its copy anyway, and can't go full size.
    const v = curl('alice', 'GET', `/api/dens/${den}/state`);
    curl('alice', 'POST', `/api/dens/${den}/settings`, { limits: { ...v.limits, file_size: 1048576 } });
    await waitFor(() => curl('bob', 'GET', `/api/dens/${den}/state`).limits.file_size === 1048576, "bob's view of the limit");
    await bob.attach('input[type=file][multiple]', [`${HOME}/photos/pixel-2.jpg`]);
    const over = await waitFor(async () => {
        const c = await bob.eval(chips);
        return c.length === 1 && c[0].includes('· smaller') && c[0].includes('over this den') ? c : null;
    }, 'a chip over the limit');
    await bob.eval(`document.querySelector('ul[aria-label="Files to send"] li button.link').click(), true`);
    await waitFor(() => bob.eval(`${buttonIn('Send full size instead')}?.disabled && ${dialog}.innerText.includes('over this den')`), 'the button disabled, with why');
    await shot(bob, 'over');
    ok(`over the limit: ${over[0]}`);
} catch (e) {
    c.fail(e);
} finally {
    await bob.close();
    await alice.close();
    c.finish();
}
