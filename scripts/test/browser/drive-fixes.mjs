// The M11.1 check of the small fixes, as alice in the browser named: a
// dialog that stays open when a selection ends outside it; a key's capture
// that a click elsewhere cancels; the Den tab leaving a DM for the den's
// last text channel, and the Direct messages tab opening the DM last open,
// with its member's profile beside it, or none; the New divider only for
// what was unread when a channel opened; the message box's focus; Enter in
// a code block; the composer's + bordered as its box is; folding a channel
// group; and a video whose sound starts late playing it in time.
//
//   run.sh fixes chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, HOME, SETTINGS, checks, launch, lib, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, CLIPS: clips, DM: dm } = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);

// media lets Firefox play sound without asking, for the clip's step.
const b = await launch(which, which === 'chromium' ? 9531 : 9532, { media: true });
const shot = (name) => b.shot(`${HOME}/shot-fixes-${which}-${name}.png`);
const at = (sel) => b.eval(`(() => { const el = ${sel}; if (!el) return ''; el.scrollIntoView({ block: 'center' }); const r = el.getBoundingClientRect(); return JSON.stringify([r.x + r.width / 2, r.y + r.height / 2]); })()`).then((r) => r && JSON.parse(r));
const clickOn = async (sel, what) => { const p = await waitFor(() => at(sel), what); await b.click(p[0], p[1]); };
const button = (text) => `[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(text)} || x.getAttribute('aria-label') === ${JSON.stringify(text)})`;
const has = (text) => b.eval(`document.body.innerText.includes(${JSON.stringify(text)})`);
const open = (channel) => b.go(`http://localhost:${ALICE.port}/den/${den}/${channel}`);
const rows = () => b.eval(`document.querySelectorAll('[data-id]').length`);
const send = (channel, text) => lib('send', 'bob', channel, text);
const BOX = `document.querySelector('textarea[placeholder="Message #general"]')`;
const row = (title) => `[...document.querySelectorAll('div')].find((d) => d.firstElementChild?.tagName === 'SPAN' && d.firstElementChild.textContent === ${JSON.stringify(title)})`;
const GROUP = `[...document.querySelectorAll('nav[aria-label=Channels] button[aria-expanded]')].find((x) => x.textContent.includes('Games'))`;
const listed = (name) => b.eval(`[...document.querySelectorAll('nav[aria-label=Channels] li')].some((li) => li.textContent.includes(${JSON.stringify(name)}))`);
const may = (p) => p.catch(() => false);

try {
    await pair(b, ALICE);

    step('a dialog stays open when a selection ends outside it');
    await open(general);
    await waitFor(async () => (await rows()) >= 3, "general's messages");
    // The ⋯ shows on hover, which a pointer's arrival doesn't give it in
    // time for the press, so it's clicked as a script would.
    await waitFor(() => b.eval(`!!${button('Settings for general')}`), "general's ⋯");
    await b.eval(`${button('Settings for general')}.click(), true`);
    const DIALOG = `document.querySelector('[role=dialog][aria-label="#general"]')`;
    await waitFor(() => b.eval(`!!${DIALOG}`), "general's settings dialog");
    const name = await at(`${DIALOG}.querySelector('input')`);
    await b.drag(name[0], name[1], 8, 8);
    await sleep(300);
    const stayed = await b.eval(`!!${DIALOG}`);
    await b.click(8, 8);
    const closed = await may(waitFor(async () => !(await b.eval(`!!${DIALOG}`)), 'the dialog closing on a click outside', 3000));
    c.report(stayed && closed, `a selection let go outside the channel's dialog left it open (${stayed}), and a click outside closed it (${closed})`);

    step("a click elsewhere ends a key's capture");
    await openSettings(b, 'Voice & video');
    await clickOn(`${row('Push to talk')}?.querySelector('button')`, "push to talk's field");
    await waitFor(() => has("Press a key, or a mouse's side button"), 'the capture prompt');
    const TITLE = `[...document.querySelectorAll('h2')].find((h) => h.textContent === 'Keyboard shortcuts')`;
    await clickOn(TITLE, "the shortcuts card's title");
    const ended = await may(waitFor(async () => !(await has("Press a key, or a mouse's side button")), 'the prompt going', 3000));
    const free = await b.eval(`!${row('Toggle mute')}?.querySelector('button').disabled`);
    await b.key('v');
    await sleep(500);
    const unbound = await b.eval(`!${row('Push to talk')}?.querySelector('kbd')`);
    c.report(ended && free && unbound, `the click ended the capture (${ended}), freed the other keys (${free}), and the next key bound nothing (${unbound})`);
    const title = await at(TITLE);
    await b.drag(title[0], title[1], 8, 8);
    await sleep(300);
    c.report(await b.eval(`!!${SETTINGS}`), 'a selection let go outside the settings left them open');
    await b.key('Escape');
    await waitFor(async () => !(await b.eval(`!!${SETTINGS}`)), 'the settings closing');

    step('the Den tab leaves a DM for the den');
    await open(dm);
    await waitFor(() => b.eval(`document.querySelector('[role=tab][aria-selected=true]')?.textContent.trim().startsWith('Direct messages')`), 'the DM open');
    await clickOn(`[...document.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim().startsWith('Den'))`, 'the Den tab');
    const back = await may(waitFor(() => b.eval(`location.pathname === '/den/${den}/${general}'`), 'general open again', 5000));
    c.report(back, "the Den tab opened general, the den's last text channel, from the DM");

    step('the Direct messages tab opens the DM last open, or none');
    const TAB = (name) => `[...document.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim().startsWith(${JSON.stringify(name)}))`;
    const SIDE = `[...document.querySelectorAll('aside')].find((a) => ['Profile', 'Members'].includes(a.querySelector('span.font-semibold')?.textContent))`;
    await clickOn(TAB('Direct messages'), 'the Direct messages tab');
    const last = await may(waitFor(() => b.eval(`location.pathname === '/den/${den}/${dm}'`), 'the DM last open', 5000));
    const profile = await may(waitFor(() => b.eval(`(() => { const s = ${SIDE}; return !!s && s.querySelector('span.font-semibold').textContent === 'Profile' && s.innerText.includes('@bob'); })()`), "bob's profile beside the DM", 5000));
    const toggle = await b.eval(`!!document.querySelector('header button[aria-label="Profile"]')`);
    await shot('dm');
    await b.eval(`document.querySelector('button[aria-label^="Close the DM with"]').click(), true`);
    const noDM = await may(waitFor(() => b.eval(`location.pathname === '/den/${den}/dms'`), 'no DM open', 5000));
    const blank = noDM && !(await b.eval(`!!document.querySelector('textarea') || !!${SIDE}`));
    await open('dms');
    const stays = await may(waitFor(() => b.eval(`${TAB('Direct messages')}?.getAttribute('aria-selected') === 'true' && !document.querySelector('textarea')`), 'the tab, blank, after a reload', 5000));
    await clickOn(TAB('Den'), 'the Den tab');
    const den2 = await may(waitFor(() => b.eval(`location.pathname === '/den/${den}/${general}'`), 'general from no DM', 5000));
    await clickOn(TAB('Direct messages'), 'the Direct messages tab again');
    const closed2 = await may(waitFor(() => b.eval(`location.pathname === '/den/${den}/dms' && !document.querySelector('textarea')`), 'none, with the DM closed', 5000));
    await clickOn(TAB('Den'), 'the Den tab again');
    await waitFor(() => b.eval(`location.pathname === '/den/${den}/${general}'`), 'general again');
    c.report(last && profile && toggle && noDM && blank && stays && den2 && closed2,
        `the tab opened the DM last open (${last}) with bob's profile in the members' place (${profile}, toggled as Profile: ${toggle}); closing it left none open (${noDM}), a blank panel (${blank}), still after a reload (${stays}); Den opened general again (${den2}), and the tab then opened none, the DM closed (${closed2})`);

    step('the New divider marks only what was unread when a channel opened');
    await waitFor(async () => (await rows()) >= 3, "general's messages");
    // The read position moves once the newest message has been on screen,
    // at most every two seconds.
    await sleep(3000);
    await open(general);
    await waitFor(async () => (await rows()) >= 3, "general's messages again");
    let before = await rows();
    await send(general, "Bob's message 4");
    await waitFor(async () => (await rows()) > before, "bob's fourth message", 10000);
    await sleep(500);
    const NEW = `document.querySelector('.divider-error')`;
    const none = !(await b.eval(`!!${NEW}`));
    await sleep(2500);
    await open(clips);
    await waitFor(() => b.eval(`!!document.querySelector('textarea[placeholder="Message #clips"]')`), 'clips');
    await send(general, "Bob's message 5");
    await send(general, "Bob's message 6");
    await sleep(1000);
    await open(general);
    await waitFor(async () => (await rows()) >= 6, "general's six messages");
    const message = (n) => `[...document.querySelectorAll('[data-id]')].find((r) => r.textContent.includes("Bob's message ${n}"))`;
    const placed = await may(waitFor(() => b.eval(`(() => {
        const d = ${NEW}, four = ${message(4)}, five = ${message(5)};
        return !!(d && four && five && four.compareDocumentPosition(d) & 4 && d.compareDocumentPosition(five) & 4);
    })()`), 'the New divider between messages 4 and 5', 5000));
    c.report(none && placed, `a message arriving in the open channel got no divider (${none}), and opening it with two unread put one before them (${placed})`);

    step("the composer's + is bordered as its box is");
    await b.eval(`document.activeElement?.blur(), true`);
    const look = (sel) => b.eval(`(() => { const s = getComputedStyle(${sel}); return [s.borderTopWidth, s.borderTopColor, s.boxShadow].join(' | '); })()`);
    const plus = await look(`document.querySelector('button[aria-label="Add to this message"]')`);
    const box = await look(BOX);
    c.report(plus === box, `the + matches the box's border and shading: ${plus} / ${box}`);

    step('Enter in a code block');
    const border = () => b.eval(`getComputedStyle(${BOX}).borderTopColor`);
    const idle = await border();
    await b.eval(`${BOX}.focus(), true`);
    const ring = await b.eval(`getComputedStyle(${BOX}).outlineStyle`);
    const focused = await border();
    await shot('focus');
    c.report(ring === 'none' && focused !== idle, `the focused box has no ring around it (${ring}), and its border darkens (${idle} to ${focused})`);
    before = await rows();
    await b.type('```');
    await b.key('Enter');
    await b.type('x = 1');
    await b.key('Enter');
    await sleep(300);
    const held = await b.eval(`${BOX}.value`);
    const unsent = (await rows()) === before;
    await b.type('```');
    await b.key('Enter');
    await waitFor(async () => (await rows()) > before && !(await b.eval(`${BOX}.value`)), 'the code block sent');
    const block = await b.eval(`[...document.querySelectorAll('[data-id] pre')].some((p) => p.textContent === 'x = 1')`);
    before = await rows();
    await b.type('```');
    await b.key('Enter');
    await b.type('y');
    await b.chord(['Control'], 'Enter');
    const anywhere = await may(waitFor(async () => (await rows()) > before && !(await b.eval(`${BOX}.value`)), 'Ctrl+Enter sending from inside a block', 5000));
    c.report(unsent && held === '```\nx = 1\n' && block && anywhere,
        `Enter in a block started new lines (${JSON.stringify(held)}, nothing sent: ${unsent}), sent after the closing fence (${block}), and Ctrl+Enter sent from inside one (${anywhere})`);

    step('folding a group');
    await clickOn(GROUP, 'the Games group');
    const hidden = await may(waitFor(async () => !(await listed('clips')) && !(await listed('Arcade')), "Games' channels hidden"));
    await send(clips, 'a clip for @alice');
    const badge = await may(waitFor(() => b.eval(`${GROUP}?.querySelector('.badge')?.textContent === '1'`), "the mention on Games' row", 10000));
    await shot('folded');
    await open(general);
    const kept = await may(waitFor(() => b.eval(`${GROUP}?.getAttribute('aria-expanded') === 'false'`), 'Games still folded after a reload'));
    await open(clips);
    const shown = await may(waitFor(() => listed('clips'), 'the open channel in its folded group'));
    await clickOn(GROUP, 'the Games group again');
    const unfolded = await may(waitFor(async () => (await listed('clips')) && (await listed('Arcade')), "Games' channels back"));
    c.report(hidden && badge && kept && shown && unfolded,
        `folding Games hid its channels (${hidden}), its row showed a mention in them (${badge}), it stayed folded after a reload (${kept}), kept the open channel in sight (${shown}), and unfolded (${unfolded})`);

    step('a video whose sound starts late plays it in time');
    // A second's clip whose sound starts 27 ms in: Firefox played the
    // sound of such a video, as Dens wrote it before, seconds late.
    const upload = lib('upload', 'bob', `${HOME}/videos/late-sound.mp4`, 'late-sound.mp4');
    lib('send', 'bob', general, 'late sound', upload);
    await open(general);
    const clip = `[...document.querySelectorAll('[data-id]')].find((r) => r.textContent.includes('late sound'))?.querySelector('video')`;
    // The list's videos load once played (preload none).
    await waitFor(() => b.eval(`!!(${clip})`), 'the clip', 20000);
    const timing = JSON.parse(await b.eval(`(async () => {
        const v = ${clip};
        v.muted = false;
        const ctx = new AudioContext();
        await ctx.resume();
        const an = ctx.createAnalyser();
        ctx.createMediaElementSource(v).connect(an);
        an.connect(ctx.destination);
        const buf = new Float32Array(an.fftSize);
        await v.play();
        let onset = null;
        for (const start = performance.now(); !v.ended && performance.now() - start < 8000;) {
            an.getFloatTimeDomainData(buf);
            if (onset === null && buf.some((x) => Math.abs(x) > 0.01)) onset = v.currentTime;
            await new Promise((r) => setTimeout(r, 20));
        }
        return JSON.stringify({ duration: v.duration, onset });
    })()`, true));
    c.report(timing.duration < 1.2 && timing.onset !== null && timing.onset < 0.3,
        `the clip lasts ${timing.duration.toFixed(2)} s and its sound starts ${timing.onset?.toFixed(2)} s in`);
    await shot('done');
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    await Promise.race([b.close(), sleep(5000)]);
    c.finish();
}
