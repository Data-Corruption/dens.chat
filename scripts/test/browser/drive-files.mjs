// The M5.2 check: Bob, in Chromium, manages his files from home: the list,
// a swap, a delete and Open, and drops an upload from the composer. Alice,
// in Firefox, moves through a message's images in the viewer. From M17.2,
// spoilers: bob sends a photo as one in #general, which alice's page shows
// covered until she clicks it, and alice one in their DM, which bob's does,
// each page clicking the other's.
//
//   run.sh files
import { ALICE, BOB, HOME, MANAGE, checks, launch, lib, openManage, pair, waitFor } from './browsers.mjs';

const { DEN: den, GENERAL: general, THREE: three, CLIP: clip, DM: dm } = process.env;
const c = checks();
const ok = (what) => c.report(true, what);
const api = (who, path) => JSON.parse(lib('api', who, 'GET', path));
const files = (who, message) => {
    const page = api(who, `/api/dens/${den}/channels/${general}/messages?around=${message}&limit=1`);
    const m = page.messages.find((x) => x.id === message);
    return m ? (m.attachments || []).map((f) => f.name) : null;
};
const used = () => api('bob', `/api/dens/${den}/storage`).used;

// Bob's Chromium hands this driver its file choosers, and takes each step
// as from a click, which a file chooser needs.
const chromium = await launch('chromium', 9311, { size: [1280, 900] });
await chromium.cdp('Page.setInterceptFileChooserDialog', { enabled: true });
const bob = {
    ...chromium,
    eval: (expression) => chromium.eval(expression, true),
    // choose answers the next file chooser the page opens with path.
    async choose(path) {
        const seen = chromium.events.length;
        const opened = await waitFor(() => chromium.events.slice(seen).find((e) => e.method === 'Page.fileChooserOpened'), 'a file chooser');
        await chromium.cdp('DOM.setFileInputFiles', { files: [path], backendNodeId: opened.params.backendNodeId });
    },
};
const alice = await launch('firefox', 9312, { size: [1280, 900] });
try {
    await pair(bob, BOB);
    await pair(alice, ALICE);
    // The list, largest first, says what uses each file.
    const section = MANAGE;
    const rows = `[...${section}.querySelectorAll('li')].map((li) => li.innerText.replace(/\\s+/g, ' ').trim())`;
    await openManage(bob, 'Your files');
    const listed = await waitFor(async () => {
        const r = await bob.eval(rows);
        return r.length === 4 ? r : null;
    }, 'the list');
    if (!listed[0].startsWith('clip.mp4') || !listed[0].includes('In #general') || !listed[0].includes('a clip')) throw new Error(`first row: ${listed[0]}`);
    if (!(await bob.eval(`/of [\\d.]+ [KMG]?B used/.test(${section}.innerText)`))) throw new Error('no space line');
    ok(`bob's list starts with: ${listed[0]}`);
    await bob.eval(`${section}.scrollIntoView(), true`);
    await bob.shot(`${HOME}/shot-files-files-list.png`);

    // A swap puts the new file in the old one's place, for everyone.
    const button = (row, label) => `[...${section}.querySelectorAll('li')].find((li) => li.innerText.includes(${JSON.stringify(row)}))`
        + `.querySelectorAll('button').values().find((b) => b.textContent.trim() === ${JSON.stringify(label)})`;
    const choosing = bob.choose(`${HOME}/photo.jpg`);
    await bob.eval(`${button('clip.mp4', 'Swap')}.click(), true`);
    await choosing;
    await waitFor(async () => !(await bob.eval(rows)).some((r) => r.startsWith('clip.mp4')), 'the clip leaving the list');
    await waitFor(() => JSON.stringify(files('alice', clip)) === JSON.stringify(['photo.jpg']), "alice's copy of the swapped message");
    ok("the swap reached alice's den: the clip's message now holds photo.jpg");

    // A delete takes a file off its message, which keeps its text.
    await bob.eval(`${button('two.jpg', 'Delete')}.click(), true`);
    await waitFor(() => bob.eval(`${section}.innerText.includes('Delete it for good?')`), 'the confirmation');
    await bob.shot(`${HOME}/shot-files-files-confirm.png`);
    await bob.eval(`${button('two.jpg', 'Delete')}.click(), true`);
    await waitFor(async () => !(await bob.eval(rows)).some((r) => r.startsWith('two.jpg')), 'two.jpg leaving the list');
    await waitFor(() => JSON.stringify(files('alice', three)) === JSON.stringify(['one.jpg', 'three.jpg']), "alice's copy of the three photos");
    ok('the delete took two.jpg off its message for alice');

    // Open shows a file's message in its channel.
    await bob.eval(`${button('one.jpg', 'Open')}.click(), true`);
    await waitFor(() => bob.eval(`location.pathname === '/den/${den}/${general}' && document.body.innerText.includes('three photos')`), 'the message opened');
    ok('Open showed the message in #general');

    // The composer's ✕ drops an upload that's up, and frees its space.
    const before = used();
    await bob.attach('input[type=file][multiple]', [`${HOME}/clip.mp4`]);
    await waitFor(() => bob.eval(`!!document.querySelector('button[aria-label="Remove clip.mp4"]') && !document.querySelector('form progress, li progress')`), 'the upload done');
    await waitFor(() => used() > before, 'the upload counted');
    await bob.eval(`document.querySelector('button[aria-label="Remove clip.mp4"]').click(), true`);
    await waitFor(() => used() === before, 'the space freed');
    ok('the composer dropped the upload taken off it, and its space came back');

    // Alice moves through the message's images in the viewer.
    await alice.go(`http://localhost:18484/den/${den}/${general}`);
    await waitFor(() => alice.eval(`!!document.querySelector('button[aria-label="Open one.jpg"]')`), "alice's tiles");
    await alice.eval(`document.querySelector('button[aria-label="Open one.jpg"]').click(), true`);
    const viewer = `document.querySelector('[role=dialog]')?.innerText.match(/\\d+ of \\d+/)?.[0]`;
    await waitFor(async () => (await alice.eval(viewer)) === '1 of 2', 'the viewer at 1 of 2');
    if (await alice.eval(`!!document.querySelector('[aria-label="Previous image"]')`)) throw new Error('a previous button at the start');
    await alice.eval(`window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' })), true`);
    await waitFor(async () => (await alice.eval(viewer)) === '2 of 2', '→ to 2 of 2');
    if (await alice.eval(`!!document.querySelector('[aria-label="Next image"]')`)) throw new Error('a next button at the end');
    await alice.shot(`${HOME}/shot-files-viewer.png`);
    await alice.eval(`document.querySelector('[aria-label="Previous image"]').click(), true`);
    await waitFor(async () => (await alice.eval(viewer)) === '1 of 2', '‹ back to 1 of 2');
    await alice.eval(`window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })), true`);
    await waitFor(() => alice.eval(`!document.querySelector('[role=dialog]')`), 'the viewer closed');
    ok('alice moved through the images with → and ‹, which stop at the ends');

    // Spoilers (M17.2): each sends a photo as one, which the other's page
    // shows covered, naming nothing of it, until it's clicked.
    const spoil = async (from, to, channel, name) => {
        await from.attach('input[type=file][multiple]', [`${HOME}/photo.jpg`]);
        const toggle = `document.querySelector('[data-spoiler-toggle]')`;
        await waitFor(() => from.eval(`!!${toggle} && !document.querySelector('form progress, li progress')`), `${from.name}'s upload done`);
        await from.eval(`${toggle}.click(), true`);
        await waitFor(() => from.eval(`${toggle}.getAttribute('aria-pressed') === 'true'`), `${from.name}'s Spoiler on`);
        await from.shot(`${HOME}/shot-files-spoiler-${from.name}-chip.png`);
        await from.eval(`(() => { const t = document.querySelector('textarea'); t.focus(); return true; })()`);
        await from.key('Enter');
        const sent = await waitFor(() => {
            const page = api(name, `/api/dens/${den}/channels/${channel}/messages?limit=5`);
            return page.messages.find((x) => (x.attachments || []).some((f) => f.spoiler));
        }, `${name}'s copy of the spoiler`);
        const message = `document.querySelector('[data-id="${sent.id}"]')`;
        await waitFor(() => to.eval(`!!${message}?.querySelector('[data-spoiler]')`), `the spoiler covered on ${to.name}'s page`);
        const cover = await to.eval(`(() => { const c = ${message}.querySelector('[data-spoiler]'); return JSON.stringify({ text: c.textContent.trim(), named: c.outerHTML.includes('photo.jpg'), open: !!${message}.querySelector('button[aria-label^="Open"]') }); })()`);
        const shown = JSON.parse(cover);
        if (shown.text !== 'Spoiler' || shown.named || shown.open) throw new Error(`the cover: ${cover}`);
        await to.shot(`${HOME}/shot-files-spoiler-${to.name}-covered.png`);
        await to.eval(`${message}.querySelector('[data-spoiler]').click(), true`);
        await waitFor(() => to.eval(`!${message}.querySelector('[data-spoiler]') && !!${message}.querySelector('button[aria-label="Open photo.jpg"]')`), `the spoiler shown on ${to.name}'s page`);
        await to.shot(`${HOME}/shot-files-spoiler-${to.name}-shown.png`);
        return sent;
    };
    await bob.go(`http://localhost:18486/den/${den}/${general}`);
    await waitFor(() => bob.eval(`!!document.querySelector('textarea')`), "bob's composer");
    await spoil(bob, alice, general, 'alice');
    ok("bob's photo, sent as a spoiler in #general, showed covered on alice's page until she clicked it");
    for (const [b, port] of [[alice, 18484], [bob, 18486]]) {
        await b.go(`http://localhost:${port}/den/${den}/${dm}`);
        await waitFor(() => b.eval(`!!document.querySelector('textarea')`), `${b.name}'s DM`);
    }
    await spoil(alice, bob, dm, 'bob');
    ok("alice's photo, sent as a spoiler in their DM, showed covered on bob's page until he clicked it");
} catch (e) {
    c.fail(e);
} finally {
    await bob.close();
    await alice.close();
    c.finish();
}
