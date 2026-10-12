// The M6.1 check: Bob sees alice's photo and clip, which his install
// keeps. They open from his cache with alice's den stopped, the photo
// deleted while his service was off leaves his cache, and the settings show
// the cache, change its size and empty it.
//
//   run.sh cache chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, PHOTO: photo, PHOTO_MESSAGE: photoMessage, CLIP: clip } = process.env;
const c = checks();
const ok = (what) => c.report(true, what);
const step = (what) => console.error(`.. ${what}`);
const api = (who, method, path) => lib('api', who, method, path);
const bobDens = () => JSON.parse(api('bob', 'GET', '/api/dens')).dens[0];
const startBob = () => lib('start', 'm2b', '--port', String(BOB.port));

step(`launching ${which}`);
const bob = await launch(which, 9321);
const shot = (name) => bob.shot(`${HOME}/shot-cache-${which}-${name}.png`);
const photoTile = `document.querySelector('button[aria-label="Open photo.jpg"]')`;
const loaded = (img) => `(() => { const i = ${img}; return !!i && i.complete && i.naturalWidth > 0; })()`;
const viewerPhoto = `[...document.querySelectorAll('[role=dialog] img')].find((i) => i.src.endsWith('/files/${photo}'))`;
const seek = (to) => `(async () => {
    const v = document.querySelector('video');
    v.muted = true;
    const within = (p, what) => Promise.race([p, new Promise((_, j) => setTimeout(() => j(new Error('no ' + what)), 15000))]);
    if (v.readyState < 1) {
        // The page's videos load nothing until they play.
        await within(v.play(), 'play');
        v.pause();
    }
    const at = ${to};
    await new Promise((resolve, reject) => {
        v.addEventListener('seeked', resolve, { once: true });
        v.addEventListener('error', () => reject(new Error('the video failed: ' + (v.error && v.error.code))), { once: true });
        setTimeout(() => reject(new Error('no seek to ' + at)), 15000);
        v.currentTime = at;
    });
    await within(new Promise((r) => (v.readyState >= 2 ? r() : v.addEventListener('loadeddata', r, { once: true }))), 'data');
    return v.readyState + ' at ' + v.currentTime.toFixed(1) + ' of ' + v.duration.toFixed(1);
})()`;
const openSettings = async () => {
    await bob.eval(`document.querySelector('button[aria-label="Settings"]').click(), true`);
    await waitFor(() => bob.eval(`[...document.querySelectorAll('[aria-label=Settings] h2')].some((h) => h.textContent === 'Storage')`), 'the storage card');
};
const storage = `[...document.querySelectorAll('[aria-label=Settings] section')].find((s) => s.querySelector('h2')?.textContent === 'Storage')`;
const closeSettings = () => bob.eval(`document.querySelector('[aria-label=Settings] button[aria-label=Close]').click(), true`);
const escape = () => bob.eval(`window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })), true`);

try {
    step('pairing');
    await pair(bob, BOB);
    step('opening general');
    await bob.go(`http://localhost:18486/den/${den}/${general}`);
    step("waiting for the photo's preview");
    await waitFor(() => bob.eval(loaded(`${photoTile}?.querySelector('img')`)), "the photo's preview");
    step('opening the viewer');
    await bob.eval(`${photoTile}.click(), true`);
    await waitFor(() => bob.eval(loaded(viewerPhoto)), 'the photo full size in the viewer');
    await escape();
    step('seeking the clip');
    await waitFor(() => bob.eval(`!!document.querySelector('video')`), 'the clip');
    ok(`the clip seeks: ${await bob.eval(seek('v.duration / 2'))}, and back: ${await bob.eval(seek('0.5'))}`);

    step('the storage card');
    await openSettings();
    const text = await bob.eval(`${storage}.innerText.replace(/\\s+/g, ' ')`);
    if (!/It holds [\d.]+ [KM]B of 2 GB now\./.test(text)) throw new Error(`the storage card says: ${text}`);
    ok(`the storage card: ${text.match(/It holds .*? now\./)[0]}`);
    await shot('storage');
    await bob.eval(`(() => { const s = ${storage}.querySelector('select'); s.value = String(1024 ** 3); s.dispatchEvent(new Event('change', { bubbles: true })); return true; })()`);
    await waitFor(() => bob.eval(`${storage}.innerText.includes('of 1 GB now')`), 'the size changed to 1 GB');
    const limit = JSON.parse(api('bob', 'GET', '/api/settings')).cacheLimit;
    if (limit !== 1024 ** 3) throw new Error(`the service's cache size: ${limit}`);
    ok('the size changed to 1 GB, which the service keeps');
    await closeSettings();

    step("stopping alice's den");
    lib('stop', 'm2a');
    await waitFor(() => bobDens().state !== 'connected', 'bob seeing the den away');
    await bob.eval(`${photoTile}.click(), true`);
    await waitFor(() => bob.eval(loaded(viewerPhoto)), 'the photo full size from the cache');
    await escape();
    ok(`with the den stopped, the photo opens and the clip seeks: ${await bob.eval(seek('v.duration / 2'))}`);
    const range = await bob.eval(`fetch('/api/dens/${den}/files/${clip}', { headers: { Range: 'bytes=100000-199999' } }).then(async (r) => r.status + ' ' + (await r.arrayBuffer()).byteLength)`);
    if (range !== '206 100000') throw new Error(`a range of the clip with the den stopped: ${range}`);
    ok('a range of the clip came from the cache with the den stopped');
    await shot('offline');
    lib('start_den');
    await waitFor(() => bobDens().state === 'connected', 'bob reconnected');

    step("deleting the photo while bob's service is off");
    lib('stop', 'm2b');
    api('alice', 'DELETE', `/api/dens/${den}/messages/${photoMessage}`);
    lib('restart_den');
    startBob();
    await waitFor(() => bobDens().state === 'connected', 'bob back');
    await waitFor(() => bob.eval(`!${photoTile} && !!document.querySelector('video')`), 'the page without the photo');
    // A file the cache held would come back; the den says it's gone.
    await waitFor(() => bob.eval(`fetch('/api/dens/${den}/files/${photo}').then(async (r) => !r.ok && (await r.text()).includes("doesn't exist on the den"))`),
        "the deleted photo out of bob's cache");
    ok("the photo deleted while bob was away left his cache, and the page");

    step('emptying the cache');
    await openSettings();
    await bob.eval(`[...${storage}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Empty').click(), true`);
    await waitFor(() => bob.eval(`${storage}.innerText.includes('It holds nothing now.')`), 'the cache emptied');
    await shot('emptied');
    ok('Empty emptied the cache');
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    await Promise.race([bob.close(), sleep(5000)]);
    c.finish();
}
