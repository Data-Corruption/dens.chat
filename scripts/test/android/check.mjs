// The Android e2e's steps in the app's page (M12.1), a command each, which
// the harness runs in the Android container, beside adb: what a member
// does, done as they do it, through the page.
//
//   node check.mjs SERIAL paired          the page shows, paired: its API answers it
//   node check.mjs SERIAL general         opens the den's general channel
//   node check.mjs SERIAL say TEXT        says TEXT there, typed into the composer and sent with Enter
//   node check.mjs SERIAL heard TEXT      waits for a message saying TEXT there
//   node check.mjs SERIAL photo FILE      sends the photo at FILE, a path the app reads, as its smaller copy
//   node check.mjs SERIAL download        saves the newest photo there, from the viewer's Download
//   node check.mjs SERIAL at DEN/CHANNEL/MESSAGE   waits for the page open at a message, as a
//                                         notification's tap opens it (M12.3)
//
// Each prints what it found, or fails saying what it waited for.
import { setTimeout as sleep } from 'node:timers/promises';
import { webview } from './drive.mjs';

const [, , serial, command, arg] = process.argv;
const b = await webview(serial);

async function waitFor(fn, what, ms = 30000) {
    const end = Date.now() + ms;
    for (;;) {
        const v = await fn().catch(() => null);
        if (v) return v;
        if (Date.now() > end) throw new Error(`waited ${ms / 1000} s for ${what}`);
        await sleep(250);
    }
}

// The messages there, as their text, and the newest's ID. The page shows
// only a long channel's newest stretch, and the e2e's den keeps its channel
// from run to run, so a message sent may push an old one out of the page.
const messages = `[...document.querySelectorAll('[data-id]')].map((m) => m.innerText.replace(/\\s+/g, ' ').trim())`;
const newest = `[...document.querySelectorAll('[data-id]')].at(-1)?.dataset.id ?? ''`;
const chips = `[...document.querySelectorAll('ul[aria-label="Files to send"] li')].map((li) => li.innerText.replace(/\\s+/g, ' ').trim())`;
const composer = `document.querySelector('textarea[placeholder^="Message #"]')`;

async function enter() {
    for (const type of ['keyDown', 'keyUp']) {
        await b.cdp('Input.dispatchKeyEvent', { type, key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13, ...(type === 'keyDown' ? { text: '\r' } : {}) });
    }
}

try {
    switch (command) {
        case 'paired': {
            const status = await waitFor(() => b.eval(`fetch('/api/status').then((r) => r.status)`).then((s) => s === 200 && s), 'the page to be paired');
            console.log(`paired: the page at ${new URL(b.url).origin} answers ${status}`);
            break;
        }
        case 'general': {
            const path = await b.eval(`(async () => {
                const den = (await (await fetch('/api/dens')).json()).dens[0].den_id;
                const state = await (await fetch('/api/dens/' + den + '/state')).json();
                return '/den/' + den + '/' + state.channels.find((c) => c.name === 'general').id;
            })()`);
            await b.eval(`history.pushState(null, '', ${JSON.stringify(path)}), dispatchEvent(new PopStateEvent('popstate')), true`);
            await waitFor(() => b.eval(`!!${composer}`), "general's composer");
            console.log('general: open');
            break;
        }
        case 'say': {
            await b.eval(`${composer}.focus(), true`);
            await b.type(arg);
            await enter();
            await waitFor(async () => (await b.eval(messages)).some((m) => m.includes(arg)), `"${arg}" in the channel`);
            console.log(`say: "${arg}" went`);
            break;
        }
        case 'heard': {
            await waitFor(async () => (await b.eval(messages)).some((m) => m.includes(arg)), `"${arg}" to arrive`);
            console.log(`heard: "${arg}"`);
            break;
        }
        case 'photo': {
            const before = await b.eval(newest);
            await b.attach('input[type=file][multiple]', [arg]);
            // The page strips the photo, and makes its smaller copy, before
            // the chip says what goes.
            const chip = await waitFor(async () => {
                const c = await b.eval(chips);
                return c.length === 1 && c[0].includes('· smaller') ? c[0] : null;
            }, 'the photo to settle on its smaller copy', 60000);
            await b.eval(`${composer}.focus(), true`);
            await enter();
            await waitFor(async () => (await b.eval(chips)).length === 0 && (await b.eval(newest)) !== before, 'the photo to go');
            const shown = await waitFor(() => b.eval(`[...document.querySelectorAll('[data-id]')].at(-1)?.querySelector('img')?.complete`), 'the photo in the channel');
            console.log(`photo: went as "${chip}", shown ${shown}`);
            break;
        }
        case 'download': {
            await b.eval(`[...document.querySelectorAll('[data-id]')].at(-1).querySelector('img').click(), true`);
            const link = await waitFor(() => b.eval(`(() => {
                const a = [...document.querySelectorAll('[role=dialog] a[download]')].find((a) => a.textContent.trim() === 'Download');
                return a && a.getAttribute('download');
            })()`), "the viewer's Download");
            await b.eval(`[...document.querySelectorAll('[role=dialog] a[download]')].find((a) => a.textContent.trim() === 'Download').click(), true`);
            await sleep(500);
            await b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
            console.log(`download: asked for ${link}`);
            break;
        }
        case 'at': {
            const [den, channel, message] = arg.split('/');
            await waitFor(() => b.eval(`location.pathname === '/den/${den}/${channel}' && location.hash === '' &&
                !!document.querySelector('[data-id="${message}"]')`), `the page at message ${message}`);
            console.log(`at: the page shows the DM at message ${message}`);
            break;
        }
        default:
            console.error(`no command ${command}`);
            process.exitCode = 2;
    }
} catch (e) {
    console.error(`${command}: ${e.message}`);
    process.exitCode = 1;
} finally {
    b.close();
}
