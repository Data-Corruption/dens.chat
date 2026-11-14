// The M21.1 check of a sign-in on a new device: bob's laptop signs in to
// the den from its page with his den password, and says at once that it
// waits for his other device, before that one answers. His first device,
// at a phone's width, shows the request on whichever pane is open: the
// channel's, the list's and the members'. Then each types the other's
// digits, and the laptop is signed in.
//
//   run.sh signin chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { BOB, HOME, checks, launch, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, DEN_URL: denURL, GENERAL: general } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const q = (s) => JSON.stringify(s);
const B = BOB;
const L = { name: 'laptop', instance: 'm2e', port: 18490 };
const DIALOG = `document.querySelector('[role=dialog]')`;
const REQUEST = `[...document.querySelectorAll('[role=status]')].find((x) => x.textContent.includes('A new device asks to sign in'))`;
const button = (scope, text) => `[...${scope}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${q(text)})`;
// setValue sets a field's value as typing would, for Preact to hear.
const setValue = (field, value) => `(() => { const el = ${field}; const set = Object.getOwnPropertyDescriptor(el.constructor.prototype, 'value').set;
    set.call(el, ${q(value)}); el.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`;
const field = (scope, label) => `[...${scope}.querySelectorAll('fieldset')].find((f) => f.querySelector('legend')?.textContent.includes(${q(label)}))?.querySelector('input')`;

// shown says whether an element is on screen and nothing covers its
// middle, as the member would see it.
const shown = (el) => `(() => { const el = ${el}; if (!el) return false; const r = el.getBoundingClientRect();
    if (r.width === 0 || r.bottom <= 0 || r.top >= innerHeight || r.right <= 0 || r.left >= innerWidth) return false;
    const at = document.elementFromPoint(Math.min(innerWidth - 1, r.left + r.width / 2), Math.min(innerHeight - 1, r.top + Math.min(r.height / 2, 20)));
    return !!at && el.contains(at); })()`;

// pane is the pane the phone layout's row has scrolled to: the list and
// the channel snap to its start, and the members to its end.
const PANE = `(() => { const row = document.querySelector('[data-pane=chan]').parentElement.getBoundingClientRect();
    const at = (p) => document.querySelector('[data-pane=' + p + ']')?.getBoundingClientRect();
    const side = at('side');
    if (side && Math.abs(side.right - row.right) < 2) return 'side';
    return ['list', 'chan'].find((p) => Math.abs(at(p).left - row.left) < 2) || ''; })()`;

try {
    B.b = await launch(which, which === 'chromium' ? 9611 : 9612, { size: [390, 844] });
    L.b = await launch(which, which === 'chromium' ? 9613 : 9614, { hover: true });
    await pair(B.b, B);
    await B.b.go(`http://localhost:${B.port}/den/${den}/${general}`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-pane=chan]')`), "bob's den");
    await pair(L.b, L);

    step('the laptop signs in with his den password');
    await waitFor(() => L.b.eval(`!!${button('document', 'Add a den')}`), 'Add a den');
    await L.b.eval(`${button('document', 'Add a den')}.click(), true`);
    await waitFor(() => L.b.eval(`!!${DIALOG}?.querySelector('input')`), 'the dialog');
    await L.b.eval(setValue(`${DIALOG}.querySelector('input')`, denURL));
    await L.b.eval(`${DIALOG}.querySelector('form').requestSubmit(), true`);
    await waitFor(() => L.b.eval(`!!${field(DIALOG, 'Den password')}`), 'the sign-in form');
    await L.b.eval(setValue(field(DIALOG, 'Username'), 'bob'));
    await L.b.eval(setValue(field(DIALOG, 'Den password'), 'bob password'));
    const asked = Date.now();
    await L.b.eval(`${DIALOG}.querySelector('form').requestSubmit(), true`);
    const waiting = await waitFor(() => L.b.eval(`${DIALOG}?.innerText.includes('Waiting for your other device') && !!${DIALOG}.querySelector('.loading-spinner')`),
        'the waiting step', 5000).catch(() => false);
    const took = Date.now() - asked;
    // His first device hasn't answered: it shows no request yet, or one
    // still asking whether it's his.
    const before = await B.b.eval(`!${REQUEST} || !!${button(`(${REQUEST})`, "It's mine")}`);
    report(waiting && before, `the laptop reads that it waits for his other device, with a spinner, ${took} ms after asking, before that device answers`);
    report(await L.b.eval(`${DIALOG}.innerText.includes('Approve this on another device')`), 'under its step\'s title');
    await L.b.shot(`${HOME}/shot-signin-waiting-${which}.png`);

    step("bob's first device shows the request on each pane");
    const request = await waitFor(() => B.b.eval(shown(REQUEST)), 'the request on the channel').catch(() => false);
    report(request && (await B.b.eval(PANE)) === 'chan', "the request shows over the channel's pane");
    await B.b.shot(`${HOME}/shot-signin-chan-${which}.png`);
    await B.b.eval(`document.querySelector('button[aria-label^="Channels"]').click(), true`);
    const onList = await waitFor(async () => (await B.b.eval(PANE)) === 'list', 'the list').catch(() => false);
    report(onList && (await B.b.eval(shown(REQUEST))), "and over the list's");
    await B.b.shot(`${HOME}/shot-signin-list-${which}.png`);
    await B.b.eval(`[...document.querySelectorAll('[data-pane=list] a, [data-pane=list] button')].find((x) => x.textContent.trim() === 'general').click(), true`);
    await waitFor(async () => (await B.b.eval(PANE)) === 'chan', 'the channel again');
    await B.b.eval(`document.querySelector('button[aria-label="Members"]').click(), true`);
    const onSide = await waitFor(async () => (await B.b.eval(PANE)) === 'side', 'the members').catch(() => false);
    report(onSide && (await B.b.eval(shown(REQUEST))), "and over the members'");
    await B.b.shot(`${HOME}/shot-signin-side-${which}.png`);
    await B.b.eval(`document.querySelector('button[aria-label="Close the member list"]').click(), true`);
    await waitFor(async () => (await B.b.eval(PANE)) === 'chan', 'the channel once more');

    step('each types the digits the other shows');
    await B.b.eval(`${button(`(${REQUEST})`, "It's mine")}.click(), true`);
    const theirs = await waitFor(() => L.b.eval(`${DIALOG}?.querySelector('.font-mono.text-2xl')?.textContent || ''`), "the laptop's digits");
    const mine = await waitFor(() => B.b.eval(`(${REQUEST})?.querySelector('.font-mono.text-2xl')?.textContent || ''`), "the first device's digits");
    report(/^\d{4}( \d{4}){3}$/.test(theirs) && /^\d{4}( \d{4}){3}$/.test(mine) && theirs !== mine,
        `each device shows digits of its own: ${theirs} and ${mine}`);
    report(await L.b.eval(`${DIALOG}.innerText.includes('Compare digits with your other device')`), 'the laptop moves on to comparing them');
    await B.b.eval(setValue(field(`(${REQUEST})`, 'Then type the digits the new device shows'), theirs));
    await B.b.eval(`${button(`(${REQUEST})`, 'Approve')}.click(), true`);
    await L.b.eval(setValue(field(DIALOG, 'Then type the digits your other device shows'), mine));
    await L.b.eval(`${button(DIALOG, 'Check')}.click(), true`);
    const done = await waitFor(() => L.b.eval(`${DIALOG}?.innerText.includes("You're signed in to")`), 'signed in', 15000).catch(() => false);
    report(done, 'the laptop is signed in, its DMs readable there');
    await L.b.shot(`${HOME}/shot-signin-done-${which}.png`);
    await L.b.eval(`${button(DIALOG, 'OK')}.click(), true`);
    const listed = await waitFor(() => L.b.eval(`document.body.innerText.includes('Voice Den') && !${DIALOG}`), "the den on the laptop's list").catch(() => false);
    report(listed, "the den is on the laptop's list");
} catch (e) {
    c.fail(e);
} finally {
    for (const s of [B, L]) await s.b?.close().catch(() => {});
    await sleep(100);
}
c.finish();
