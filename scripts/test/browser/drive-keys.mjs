// The M7.1 check: Alice binds push to talk to V and toggle mute to Mouse 4
// in the settings, without the side button taking the page back. A
// stand-in helper on her instance's control socket says it reads both, and
// the keys card marks them; in a call, its presses open her push-to-talk
// gate and toggle her mute, while her own V in the page does nothing. With
// the helper gone, the page's own V and Mouse 4 work again.
//
//   run.sh keys chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, HOME, checks, helper, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, LOUNGE: lounge } = process.env;
const c = checks();
const ok = (what) => c.report(true, what);
const step = (what) => console.error(`.. ${what}`);

const b = await launch(which, which === 'chromium' ? 9521 : 9522, { media: true });
const shot = (name) => b.shot(`${HOME}/shot-keys-${which}-${name}.png`);
const rect = (sel) => b.eval(`(() => { const el = ${sel}; if (!el) return ''; el.scrollIntoView({ block: 'center' }); const r = el.getBoundingClientRect(); return JSON.stringify([r.x + r.width / 2, r.y + r.height / 2]); })()`).then((r) => r && JSON.parse(r));
const button = (text) => `[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(text)} || x.getAttribute('aria-label') === ${JSON.stringify(text)})`;
const row = (title) => `[...document.querySelectorAll('div')].find((d) => d.firstElementChild?.tagName === 'SPAN' && d.firstElementChild.textContent === ${JSON.stringify(title)})`;
const clickOn = async (sel, what) => { const at = await waitFor(() => rect(sel), what); await b.click(at[0], at[1]); };
const keyIn = (title) => b.eval(`${row(title)}?.querySelector('kbd')?.textContent || ''`);
const marked = (title) => b.eval(`!!${row(title)}?.querySelector('[aria-label="Works in other apps"]')`);
const gateOpen = () => b.eval(`!!document.querySelector('div.relative.h-2 > div.bg-success')`);
const muteLabel = () => b.eval(`document.querySelector('button[aria-label="Unmute"]') ? 'muted' : document.querySelector('button[aria-label="Mute"]') ? 'unmuted' : ''`);
const settingsKeys = () => JSON.parse(lib('api', 'alice', 'GET', '/api/settings')).voiceKeys || {};
let h = null;

try {
    step('pairing alice, push to talk chosen');
    await pair(b, ALICE);
    await b.eval(`localStorage.setItem('DENS_VOICE', JSON.stringify({ mode: 'ptt' })), true`);
    await b.go(`http://localhost:18484/den/${den}/${lounge}`);
    const home = await b.eval('location.href');

    step('binding V and Mouse 4 in the settings');
    await clickOn(button('App settings'), 'the settings button');
    await clickOn(button('Voice & video'), 'the voice tab');
    await clickOn(`${row('Push to talk')}?.querySelector('button')`, "push to talk's field");
    await waitFor(() => b.eval(`document.body.innerText.includes("Press a key, or a mouse's side button")`), 'the capture prompt');
    await b.key('v', 'down');
    await b.key('v', 'up');
    await waitFor(async () => (await keyIn('Push to talk')) === 'V', 'V bound');
    await clickOn(`${row('Toggle mute')}?.querySelector('button')`, "toggle mute's field");
    await waitFor(() => b.eval(`document.body.innerText.includes("Press a key, or a mouse's side button")`), 'the capture prompt again');
    const at = await rect(`${row('Toggle mute')}`);
    await b.click(at[0], at[1], 'back');
    await waitFor(async () => (await keyIn('Toggle mute')) === 'Mouse 4', 'Mouse 4 bound');
    await sleep(500);
    const after = await b.eval('location.href');
    if (after !== home) throw new Error(`the side button took the page from ${home} to ${after}`);
    const kept = settingsKeys();
    if (kept.ptt?.code !== 'KeyV' || kept.toggleMute?.code !== 'Mouse4') throw new Error(`the service kept ${JSON.stringify(kept)}`);
    ok('V and Mouse 4 bound, kept by the service, and the side button left the page where it was');
    const unmarked = !(await marked('Push to talk')) && (await b.eval(`document.body.innerText.includes('To use them in other apps, keep the Dens helper running')`));
    if (!unmarked) throw new Error('the shortcuts card claims a helper that isn\'t there');
    await shot('keys-no-helper');

    step('a stand-in helper says it reads both');
    h = helper();
    await waitFor(() => h.state(), "the service's state for the helper");
    h.send('source', { covers: ['ptt', 'toggleMute'], note: 'A test note from the helper.' });
    await waitFor(async () => (await marked('Push to talk')) && (await marked('Toggle mute')) && !(await marked('Push to mute')), 'the marks');
    await waitFor(() => b.eval(`document.body.innerText.includes('Work in other apps too during a call') && document.body.innerText.includes('A test note from the helper.')`), 'the note');
    ok('the shortcuts card marks the shortcuts the helper reads, with its note');
    await shot('keys-helper');
    await b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await clickOn(`document.querySelector('[aria-label="Close"]') || ${button('✕')}`, 'the close button').catch(() => {});

    step('joining Lounge');
    await clickOn(`document.querySelector('button[title^="Join the call"]')`, 'the join button');
    await waitFor(() => h.state()?.call, 'the helper hearing of the call');
    ok('the helper heard the install holds a call');
    await clickOn(`document.querySelector('button[aria-label="Voice settings"]')`, "the call's voice settings");
    await waitFor(() => b.eval(`!!document.querySelector('div.relative.h-2 > div')`), 'the meter');
    await sleep(1000);
    if (await gateOpen()) throw new Error('the gate is open before any press');

    step("the helper's presses");
    h.send('press', { action: 'ptt', down: true });
    await waitFor(gateOpen, 'the gate opening for the helper', 5000);
    h.send('press', { action: 'ptt', down: false });
    await waitFor(async () => !(await gateOpen()), 'the gate closing', 5000);
    ok("the helper's push to talk opens and closes the gate");
    h.send('press', { action: 'toggleMute', down: true });
    h.send('press', { action: 'toggleMute', down: false });
    await waitFor(async () => (await muteLabel()) === 'muted', 'muted', 5000);
    h.send('press', { action: 'toggleMute', down: true });
    h.send('press', { action: 'toggleMute', down: false });
    await waitFor(async () => (await muteLabel()) === 'unmuted', 'unmuted', 5000);
    ok("the helper's toggle mute mutes and unmutes");

    step("the page's own V, which the helper reads");
    await b.focus();
    await b.key('v', 'down');
    await sleep(800);
    const ownOpened = await gateOpen();
    await b.key('v', 'up');
    if (ownOpened) throw new Error("the page acted on its own V while the helper reads it");
    ok("the page leaves the helper's keys to it during the call");

    step('the helper going');
    h.send('press', { action: 'ptt', down: true });
    await waitFor(gateOpen, 'the gate opening again', 5000);
    h.close();
    h = null;
    await waitFor(async () => !(await gateOpen()), 'the gate closing as the helper goes', 5000);
    await waitFor(async () => !(await marked('Push to talk')), 'the marks gone');
    ok("a press held as the helper goes is let go, and the marks go with it");
    await b.key('v', 'down');
    await waitFor(gateOpen, "the page's own V opening the gate", 5000);
    await b.key('v', 'up');
    await waitFor(async () => !(await gateOpen()), "the page's own V closing it", 5000);
    const here = await b.eval('location.href');
    const spot = await rect(`document.querySelector('div.relative.h-2')`);
    await b.click(spot[0], spot[1], 'back');
    await waitFor(async () => (await muteLabel()) === 'muted', 'Mouse 4 muting', 5000);
    await b.click(spot[0], spot[1], 'back');
    await waitFor(async () => (await muteLabel()) === 'unmuted', 'Mouse 4 unmuting', 5000);
    await sleep(500);
    if ((await b.eval('location.href')) !== here) throw new Error('Mouse 4 took the page back');
    ok("without the helper, the page's own V pushes to talk, and Mouse 4 toggles mute without going back");
    await shot('done');
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    h?.close();
    await Promise.race([b.close(), sleep(5000)]);
    c.finish();
}
