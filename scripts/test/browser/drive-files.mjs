// The M5.2 check: Bob, in Chromium, manages his files from home: the list,
// a swap, a delete and Open, and drops an upload from the composer. Alice,
// in Firefox, moves through a message's images in the viewer.
//
//   run.sh files
import { ALICE, BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const { DEN: den, GENERAL: general, THREE: three, CLIP: clip } = process.env;
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
    const section = `[...document.querySelectorAll('details')].find((d) => d.querySelector('summary')?.textContent.trim() === 'Files')`;
    const rows = `[...${section}.querySelectorAll('li')].map((li) => li.innerText.replace(/\\s+/g, ' ').trim())`;
    await waitFor(() => bob.eval(`!!${section}`), "bob's Files section");
    await bob.eval(`${section}.querySelector('summary').click(), true`);
    const listed = await waitFor(async () => {
        const r = await bob.eval(rows);
        return r.length === 4 ? r : null;
    }, 'the list');
    if (!listed[0].startsWith('clip.mp4') || !listed[0].includes('In #general') || !listed[0].includes('a clip')) throw new Error(`first row: ${listed[0]}`);
    if (!(await bob.eval(`${section}.innerText.includes('Your files take')`))) throw new Error('no space line');
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
} catch (e) {
    c.fail(e);
} finally {
    await bob.close();
    await alice.close();
    c.finish();
}
