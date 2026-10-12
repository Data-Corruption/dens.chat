// The M6.2 check: Bob reads general, which his install keeps. While his
// service is off, alice edits one message, deletes another and posts; back,
// his page shows all three changes. With alice's den stopped, his page says
// it's offline over the messages, and reads them from his cache, even after
// a reload; once the den is back, the note goes.
//
//   run.sh history chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, EDIT: edit, DELETE: del } = process.env;
const c = checks();
const ok = (what) => c.report(true, what);
const api = (who, method, path, body) => lib('api', who, method, path, ...(body ? [JSON.stringify(body)] : []));
const bobState = () => JSON.parse(api('bob', 'GET', '/api/dens')).dens[0].state;
const nonce = () => Buffer.from(crypto.getRandomValues(new Uint8Array(16))).toString('base64url');

const bob = await launch(which, 9321);
const shot = (name) => bob.shot(`${HOME}/shot-history-${which}-${name}.png`);
const text = `document.querySelector('[data-id]')?.closest('.overflow-y-auto')?.innerText || ''`;
const note = `[...document.querySelectorAll('[role=status]')].map((e) => e.textContent).find((t) => t.includes('The den is offline')) || ''`;

try {
    await pair(bob, BOB);
    await bob.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
    await waitFor(async () => (await bob.eval(text)).includes('to be deleted'), 'general');
    ok('bob reads general');

    // Changes while bob's service is off.
    lib('stop', 'm2b');
    api('alice', 'PATCH', `/api/dens/${den}/messages/${edit}?channel=${general}`, { revision: 1, text: 'edited while away' });
    api('alice', 'DELETE', `/api/dens/${den}/messages/${del}`);
    api('alice', 'POST', `/api/dens/${den}/channels/${general}/messages`, { nonce: nonce(), text: 'new while away' });
    lib('restart_den');
    lib('start', 'm2b', '--port', String(BOB.port));
    await waitFor(() => bobState() === 'connected', 'bob back');
    await waitFor(async () => {
        const t = await bob.eval(text);
        return t.includes('edited while away') && !t.includes('to be deleted') && t.includes('new while away') && !t.includes('to be edited');
    }, 'the three changes on the page');
    ok('the edit, the delete and the new message reached the page');

    // Alice's den stops.
    lib('stop', 'm2a');
    await waitFor(() => bobState() === 'offline', 'bob seeing the den offline');
    const shown = await waitFor(() => bob.eval(note), 'the offline note');
    ok(`the note: ${shown.trim()}`);
    await shot('offline');
    await bob.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
    await waitFor(async () => (await bob.eval(text)).includes('new while away'), 'general from the cache after a reload');
    await waitFor(() => bob.eval(note), 'the note after a reload');
    ok('after a reload, general reads from the cache, under the note');
    await shot('offline-reloaded');
    lib('start_den');
    await waitFor(() => bobState() === 'connected', 'bob reconnected');
    await waitFor(async () => (await bob.eval(note)) === '', 'the note gone');
    ok('the note went once the den was back');
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    await Promise.race([bob.close(), sleep(5000)]);
    c.finish();
}
