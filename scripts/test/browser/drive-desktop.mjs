// The M7.2 check of the page: a stand-in helper plays a Wayland desktop
// that owns the keys. The keys card offers to set them up with it, says to
// confirm them while its dialog is up, shows its names once they're bound,
// asks it to show its settings, and stops using it; in a call, its presses
// open the push-to-talk gate.
//
//   run.sh desktop chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, HOME, checks, helper, launch, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, LOUNGE: lounge } = process.env;
const c = checks();
const ok = (what) => c.report(true, what);
const step = (what) => console.error(`.. ${what}`);

const b = await launch(which, which === 'chromium' ? 9523 : 9524, { media: true });
const shot = (name) => b.shot(`${HOME}/shot-desktop-${which}-${name}.png`);
const rect = (sel) => b.eval(`(() => { const el = ${sel}; if (!el) return ''; el.scrollIntoView({ block: 'center' }); const r = el.getBoundingClientRect(); return JSON.stringify([r.x + r.width / 2, r.y + r.height / 2]); })()`).then((r) => r && JSON.parse(r));
const button = (text) => `[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(text)} || x.getAttribute('aria-label') === ${JSON.stringify(text)})`;
const row = (title) => `[...document.querySelectorAll('div')].find((d) => d.firstElementChild?.firstElementChild?.textContent === ${JSON.stringify(title)})`;
const clickOn = async (sel, what) => { const at = await waitFor(() => rect(sel), what); await b.click(at[0], at[1]); };
const keyIn = (title) => b.eval(`${row(title)}?.querySelector('kbd')?.textContent || ''`);
const marked = (title) => b.eval(`!!${row(title)}?.querySelector('[aria-label="Works in other programs"]')`);
const has = (text) => b.eval(`document.body.innerText.includes(${JSON.stringify(text)})`);
const gateOpen = () => b.eval(`!!document.querySelector('div.relative.h-2 > div.bg-success')`);
const asked = (h, what) => waitFor(() => h.lines.some((m) => m.t === 'desktop' && m.d?.do === what), `the helper asked to ${what}`, 5000);
let h = null;

try {
    step('pairing alice, push to talk chosen, a key set');
    await pair(b, ALICE);
    await b.eval(`localStorage.setItem('DENS_VOICE', JSON.stringify({ mode: 'ptt' })), true`);
    await b.go(`http://localhost:18484/den/${den}/${lounge}`);
    h = helper();
    await waitFor(() => h.state(), "the service's state for the helper");
    // A Wayland desktop with the portal, not set up.
    h.send('source', { covers: [], confirms: true, bindable: true, configurable: true });
    await clickOn(button('Settings'), 'the settings button');
    await clickOn(button('Voice'), 'the voice tab');
    await clickOn(`${row('Push to talk')}?.querySelector('button')`, "push to talk's Set");
    await waitFor(() => has("Press a key or a mouse's side button"), 'the capture prompt');
    await b.key('v', 'down');
    await b.key('v', 'up');
    await waitFor(async () => (await keyIn('Push to talk')) === 'V', 'V bound');
    await waitFor(() => b.eval(`!!${button('Set up with your desktop')}`), 'the set-up button');
    ok('the keys card offers to set the keys up with the desktop');
    await shot('offer');

    step('setting up');
    await clickOn(button('Set up with your desktop'), 'the set-up button');
    await asked(h, 'bind');
    h.send('source', { covers: [], confirms: true, binding: true, configurable: true });
    await waitFor(() => has("Confirm the keys in your desktop's dialog."), 'the confirm note');
    ok("the page says to confirm in the desktop's dialog while it's up");
    h.send('source', { covers: ['ptt', 'toggleMute'], confirms: true, configurable: true, names: { ptt: 'Press F8', toggleMute: 'Press F9' } });
    h.send('bound');
    await waitFor(async () => (await keyIn('Push to talk')) === 'Press F8' && (await keyIn('Toggle mute')) === 'Press F9', "the desktop's names");
    await waitFor(async () => (await marked('Push to talk')) && (await marked('Toggle mute')), 'the marks');
    if (await b.eval(`!!${row('Push to talk')}?.querySelector('button')`)) throw new Error("push to talk's row still offers Change");
    await waitFor(() => has('Your desktop runs these keys in every program'), 'the note once set up');
    await waitFor(() => h.state()?.desktop, 'the service keeping that the member set it up');
    ok("the desktop's names show with the marks, and the service keeps the member's choice");
    await shot('bound');
    await clickOn(button('Change them'), 'Change them');
    await asked(h, 'configure');
    ok("Change them asks the desktop to show its settings");
    await b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);

    step('a call');
    await clickOn(`document.querySelector('button[title^="Join the call"]')`, 'the join button');
    await waitFor(() => h.state()?.call, 'the helper hearing of the call');
    await clickOn(`document.querySelector('button[aria-label="Voice settings"]')`, "the call's voice settings");
    await waitFor(() => b.eval(`!!document.querySelector('div.relative.h-2 > div')`), 'the meter');
    await sleep(800);
    if (await gateOpen()) throw new Error('the gate is open before any press');
    h.send('press', { action: 'ptt', down: true });
    await waitFor(gateOpen, "the gate opening for the desktop's press", 5000);
    h.send('press', { action: 'ptt', down: false });
    await waitFor(async () => !(await gateOpen()), 'the gate closing', 5000);
    ok("the desktop's push to talk opens and closes the gate");

    step('stopping');
    await clickOn(button('Stop using them'), 'Stop using them');
    await asked(h, 'stop');
    await waitFor(() => h.state() && !h.state().desktop, "the service dropping the member's choice");
    h.send('source', { covers: [], confirms: true, bindable: true, configurable: true });
    await waitFor(() => b.eval(`!!${button('Set up with your desktop')}`), 'the set-up button again');
    await waitFor(async () => (await keyIn('Push to talk')) === 'V', "the page's own key again");
    ok("stopping drops the member's choice, and the card offers set-up again");
    await shot('done');
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    h?.close();
    await Promise.race([b.close(), sleep(5000)]);
    c.finish();
}
