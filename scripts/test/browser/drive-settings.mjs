// The M19.1 check of the settings, as alice, the den's owner, in the
// browser named at a desktop's width: every section of the settings and
// of Manage opened from the list down the side, each with the layout
// audit, and every hint in it read as one line, a hint that wrapped
// failing it, Voice with push to talk's release delay too (M19.3); then
// Appearance's Width: Wide and Narrow holding general's header, list and
// composer to 1,200 and 900 pixels, centered in the pane, and Full letting
// them go.
//
//   run.sh settings chromium|firefox
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, HOME, MANAGE, SETTINGS, checks, launch, openManage, openSettings, pair, waitFor } from './browsers.mjs';
import { audits, helpers } from './screens.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general } = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);

// Wide enough for Wide's 1,200 pixels to bind inside the channel's pane.
const b = await launch(which, which === 'chromium' ? 9571 : 9572, { size: [1900, 1000], hover: true });
const out = `${HOME}/shot-settings/${which}`;
fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });
const h = helpers(b, `http://localhost:${ALICE.port}`, out);
const PANEL = `document.querySelector('[role=dialog] [data-panel]')`;

// WRAPPED names the hints shown in the open dialog that run past a line:
// each paragraph of the panel, measured against its own line height.
const WRAPPED = `(() => {
    const out = [];
    for (const p of document.querySelectorAll('[role=dialog] [data-panel] p')) {
        if (p.closest('[role=tooltip]')) continue;
        const s = getComputedStyle(p);
        if (s.display === 'none' || !p.getClientRects().length) continue;
        const lh = parseFloat(s.lineHeight) || parseFloat(s.fontSize) * 1.5;
        const lines = p.getBoundingClientRect().height / lh;
        if (lines > 1.5) out.push(JSON.stringify(p.textContent.trim().slice(0, 70)) + ' (' + Math.round(lines) + ' lines)');
    }
    return out;
})()`;
const sections = (frame) => b.eval(`[...document.querySelectorAll('${frame} nav button')].map((x) => x.textContent.trim() + (x.getAttribute('aria-current') === 'page' ? '*' : '')).join('|')`);
const slug = (s) => s.toLowerCase().replace(/[^a-z]+/g, '-');

try {
    await pair(b, ALICE);
    await b.go(`http://localhost:${ALICE.port}/`);

    step('every section of the settings, from the list down the side');
    await openSettings(b, 'Appearance');
    c.report((await sections('[role=dialog][aria-label="App settings"]')) === 'Appearance*|Chat|Notifications|Voice|System',
        `the settings list their sections down the side, open at Appearance: ${await sections('[role=dialog][aria-label="App settings"]')}`);
    const ready = {
        Appearance: `document.querySelector('[data-width]')`,
        Chat: `document.getElementById('compress')`,
        Notifications: `document.getElementById('notify-dms') && document.querySelector('[data-sound]')`,
        Voice: `document.getElementById('noise') && document.querySelector('select')`,
        System: `document.querySelector('a[href="/api/logs"]') && document.getElementById('hosting')`,
    };
    for (const name of ['Appearance', 'Chat', 'Notifications', 'Voice', 'System']) {
        await openSettings(b, name);
        await waitFor(() => b.eval(`!!(${ready[name]})`), `${name}'s cards`);
        await sleep(400);
        await h.snap(`settings-${slug(name)}`, { scroll: PANEL, max: 6 });
        const wrapped = await b.eval(WRAPPED);
        c.report(wrapped.length === 0, `${name}: every hint reads in a line${wrapped.length ? `; wrapped: ${wrapped.join('; ')}` : ''}`);
    }
    // Push to talk shows its release delay in Voice, which the audit and
    // the hints' measure take too.
    await openSettings(b, 'Voice');
    await b.eval(`[...document.querySelectorAll('[role=radiogroup][aria-label="Sending your voice"] label')].find((l) => l.textContent.trim() === 'Push to talk').querySelector('input').click(), true`);
    await waitFor(() => b.eval(`!!document.querySelector('input[aria-label="Release delay"]')`), 'the release delay');
    await sleep(300);
    await h.snap('settings-voice-ptt', { scroll: PANEL, max: 6 });
    const pttWrapped = await b.eval(WRAPPED);
    c.report(pttWrapped.length === 0, `Voice with push to talk: every hint reads in a line${pttWrapped.length ? `; wrapped: ${pttWrapped.join('; ')}` : ''}`);
    await b.eval(`[...document.querySelectorAll('[role=radiogroup][aria-label="Sending your voice"] label')].find((l) => l.textContent.trim() === 'Voice activity').querySelector('input').click(), true`);
    await b.key('Escape');
    await waitFor(async () => !(await b.eval(`!!${SETTINGS}`)), 'the settings closing');

    step("every section of Manage, the owner's");
    await openManage(b, 'General');
    await waitFor(() => b.eval(`!!${MANAGE}?.querySelector('input[type=url]')`), "Manage's General");
    const manage = await b.eval(`[...document.querySelectorAll('[role=dialog][aria-label^="Manage "] nav button')].map((x) => x.textContent.trim())`);
    c.report(manage.join('|') === 'General|Invites|Bans|Soundboard|Emoji|Limits|Devices|Password and recovery|Your code|Your files',
        `Manage lists the owner's sections: ${manage.join(', ')}`);
    for (const name of manage) {
        await openManage(b, name);
        await sleep(900);
        await h.snap(`manage-${slug(name)}`, { scroll: PANEL, max: 4 });
        const wrapped = await b.eval(WRAPPED);
        c.report(wrapped.length === 0, `Manage's ${name}: every hint reads in a line${wrapped.length ? `; wrapped: ${wrapped.join('; ')}` : ''}`);
    }
    await b.key('Escape');
    await waitFor(async () => !(await b.eval(`!!${MANAGE}`)), 'Manage closing');

    step('the layout audit over every screen');
    audits(which, h.log, (good, what) => c.report(good, what), false);

    step("Appearance's Width");
    await b.go(`http://localhost:${ALICE.port}/den/${den}/${general}`);
    await waitFor(() => b.eval(`document.querySelectorAll('[data-scroller] [data-id]').length >= 5`), "general's messages");
    // measure takes the pane, and the header's, the list's and the foot's
    // content, each by its left edge and width.
    const measure = () => b.eval(`(() => {
        const box = (e) => { const r = e.getBoundingClientRect(); return [Math.round(r.left), Math.round(r.width)]; };
        const pane = document.querySelector('[data-pane=chan]');
        const scroller = document.querySelector('[data-scroller]');
        const header = pane.querySelector('header');
        return { pane: box(pane), header: box(header.firstElementChild), list: box(scroller.firstElementChild), scroller: box(scroller),
            foot: box(document.querySelector('textarea[placeholder^="Message "]').closest('[data-scroller] ~ div')) };
    })()`);
    const pick = async (choice) => {
        await openSettings(b, 'Appearance');
        await waitFor(() => b.eval(`!!document.querySelector('[data-width]')`), 'the Width choices');
        await b.eval(`[...document.querySelectorAll('[data-width] label')].find((l) => l.textContent.trim() === ${JSON.stringify(choice)}).querySelector('input').click(), true`);
        await sleep(200);
        await b.key('Escape');
        await waitFor(async () => !(await b.eval(`!!${SETTINGS}`)), 'the settings closing');
        await sleep(300);
        return measure();
    };
    const centered = (m, part, max) => {
        const [left, width] = m[part];
        const [paneLeft, paneWidth] = m.pane;
        const inset = (paneWidth - width) / 2;
        return width <= max && width >= max - 2 && Math.abs(left - (paneLeft + inset)) <= 17;
    };
    const full = await pick('Full');
    const wide = await pick('Wide');
    const narrow = await pick('Narrow');
    await b.shot(`${HOME}/shot-settings-${which}-narrow.png`);
    const back = await pick('Full');
    c.report(full.pane[1] >= 1300, `the pane is wide enough for the check: ${full.pane[1]} pixels`);
    c.report(full.list[1] >= full.pane[1] - 40 && full.header[1] >= full.pane[1] - 40 && full.foot[1] >= full.pane[1] - 2,
        `Full lets the header, the list and the foot take the pane: ${JSON.stringify(full)}`);
    c.report(centered(wide, 'list', 1200) && centered(wide, 'header', 1200) && centered(wide, 'foot', 1200),
        `Wide holds the header, the list and the foot to 1,200 pixels, centered: ${JSON.stringify(wide)}`);
    c.report(centered(narrow, 'list', 900) && centered(narrow, 'header', 900) && centered(narrow, 'foot', 900),
        `Narrow holds them to 900, centered: ${JSON.stringify(narrow)}`);
    c.report(narrow.scroller[1] === full.scroller[1], `the scroller keeps the pane's width, its scrollbar at the edge: ${narrow.scroller[1]} of ${full.scroller[1]}`);
    c.report(back.list[1] === full.list[1], `Full again lets the list go: ${back.list[1]}`);
    const kept = await b.eval(`localStorage.getItem('DENS_WIDTH')`);
    c.report(kept === 'full', `the browser keeps the choice: ${kept}`);
} catch (e) {
    c.fail(e);
    await b.shot(`${HOME}/shot-settings-${which}-failed.png`).catch(() => {});
} finally {
    await Promise.race([b.close(), sleep(5000)]);
    c.finish();
}
