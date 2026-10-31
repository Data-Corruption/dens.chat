// The phone check's screens (M13), which the browsers' driver
// (drive-phone.mjs) and the Android e2e's walk (scripts/test/android/
// walk.mjs) share: each step brings the page to a screen, and snap takes
// its picture with an audit of its layout beside it. Steps reach screens by
// script, as the browser checks do, so a screen shows whatever covers its
// buttons; what a finger can do is getting around's and touching's, below,
// through the driver's own swipes, presses, keys and Back.
//
// The screens, by the check that reaches each, so the walk keeps to every
// screen the browser checks cover; a check that adds a screen adds it here:
//   calls, talk, callaudio   a call: its block on the list, a member's menu, the call
//                            from a channel and from Home, a share in the player
//   fixes, messages          a channel, its description, a message's menu, deleting,
//                            editing, replying, the composer's +, letting others edit,
//                            the writing guide
//   photos, videos, files    a photo in the composer, comparing its copies, the viewer,
//                            a video in the list
//   keys, desktop, notify    the settings, each tab
//   hosting, move, cache     the System tab, as an owner
//   retention, files         Manage, each section, as a member and as an owner
//   history, hostile         the den's list, its DMs, a DM, a DM's check codes
//   (M8.3, M11)              Home, adding a den, a member's profile, editing one's own,
//                            removing a member, a channel's and a group's settings
//   (M9, M10)                unpaired, setting the local password, joining a den,
//                            the recovery codes
//   (M13.2)                  a message's sheet, three lines sent, a tip opened by a tap
//   (M13.3)                  the settings' and Manage's lists of sections, the viewer
//                            swiped and zoomed, a dialog as a sheet swiped down
//   (M13.4)                  the call's bar, the call's button muted, its role, held,
//                            a member's sheet, a share docked and full screen
import { setTimeout as sleep } from 'node:timers/promises';
import fs from 'node:fs';

export async function waitFor(fn, what, ms = 15000) {
    const end = Date.now() + ms;
    let last;
    while (Date.now() < end) {
        try {
            last = await fn();
            if (last) return last;
        } catch (e) {
            last = e.message;
        }
        await sleep(250);
    }
    throw new Error(`timed out waiting for ${what}: ${typeof last === 'string' ? last : JSON.stringify(last)}`);
}

// AUDIT measures the screen as a phone would meet it: whether the page
// scrolls sideways, what sticks out past the right edge outside a scroller
// of its own, and of the controls a finger can reach, those under 24
// pixels each way, WCAG 2.2's minimum (small), a list's rows under 44
// pixels tall (rows), those whose room for a finger reaches less than 44
// each way (tight), and those hidden until a hover (hoverOnly); and the
// controls whose middle another control's room for a finger covers
// (covered); and whether a channel's message list scrolls sideways
// (listSideways). Panes off screen are inert, and left out, as are
// controls under a dialog, a link within a line of text, which WCAG leaves
// to the text's size, and a disabled control's hiding. A list's row is a
// menu's item, or a control across most of its list item or its list.
export const AUDIT = `(() => {
    const vw = document.documentElement.clientWidth, vh = innerHeight;
    const shown = (e) => { const s = getComputedStyle(e); return s.display !== 'none' && s.visibility !== 'hidden' && e.getClientRects().length > 0; };
    const desc = (e) => {
        const t = (e.getAttribute('aria-label') || e.textContent || e.getAttribute('placeholder') || '').trim().replace(/\\s+/g, ' ').slice(0, 40);
        return e.tagName.toLowerCase() + (t ? ' "' + t + '"' : '');
    };
    const held = (e) => {
        for (let p = e.parentElement; p && p !== document.body; p = p.parentElement) {
            const s = getComputedStyle(p);
            if (['hidden', 'auto', 'scroll', 'clip'].includes(s.overflowX) && p.getBoundingClientRect().right <= vw + 1) return true;
        }
        return false;
    };
    const over = [];
    for (const e of document.querySelectorAll('body *')) {
        if (!shown(e) || e.closest('[inert]')) continue;
        const r = e.getBoundingClientRect();
        if (r.width === 0 || r.right <= vw + 1 || r.left >= vw) continue;
        if (!held(e) && !over.some((o) => o.el.contains(e))) over.push({ el: e, what: desc(e) + ' → ' + Math.round(r.right) });
    }
    // A channel's message list holds whatever scrolls sideways in a scroller
    // of its own, as code does, so it never scrolls sideways itself (M13.4).
    const listSideways = [...document.querySelectorAll('[data-scroller]')].some((l) => shown(l) && !l.closest('[inert]') && l.scrollWidth > l.clientWidth + 1);
    const faded = (e) => { for (let p = e; p; p = p.parentElement) if (getComputedStyle(p).opacity === '0') return true; return false; };
    const disabled = (e) => e.disabled || e.getAttribute('aria-disabled') === 'true' || !!e.closest('fieldset:disabled');
    const hits = (e, x, y) => { const at = document.elementFromPoint(x, y); return !!at && (at === e || e.contains(at)); };
    // Words that are a link or a button, as a line of text has them, or
    // a box of them in a line of other words.
    const inline = (e) => {
        const display = getComputedStyle(e).display;
        if (!(e.textContent || '').trim() || !display.startsWith('inline')) return false;
        if (display === 'inline') return true;
        let block = e.parentElement;
        while (block && getComputedStyle(block).display.startsWith('inline')) block = block.parentElement;
        return !!block && block.textContent.trim().length > e.textContent.trim().length;
    };
    // A sheet or a dialog opened from a list's item isn't its row.
    const across = (e, r) => {
        if (/^(menuitem|option)/.test(e.getAttribute('role') || '')) return true;
        const layer = e.closest('[role=dialog]');
        for (const sel of ['li', 'nav']) {
            const box = e.parentElement?.closest(sel);
            if (box && !(layer && box.contains(layer)) && r.width >= 0.8 * box.getBoundingClientRect().width) return true;
        }
        return false;
    };
    const small = [], rows = [], tight = [], hoverOnly = [], covered = [];
    const TARGETS = 'button, a[href], input:not([type=hidden]), select, textarea, [role=tab], [role=menuitem], [role=button], summary, label:has(input)';
    const targets = document.querySelectorAll(TARGETS);
    for (const e of targets) {
        if (!shown(e) || e.closest('[aria-hidden=true], [inert]')) continue;
        const r = e.getBoundingClientRect();
        if (r.bottom < 0 || r.top > vh || r.right < 0 || r.left > vw) continue;
        const s = getComputedStyle(e);
        if (faded(e) || s.pointerEvents === 'none') {
            if (!disabled(e)) hoverOnly.push(desc(e));
            continue;
        }
        if (e.tagName === 'INPUT' && ['checkbox', 'radio'].includes(e.type) && e.closest('label')) continue;
        const cx = Math.min(Math.max(r.left + r.width / 2, 0), vw - 1), cy = Math.min(Math.max(r.top + r.height / 2, 0), vh - 1);
        if (!hits(e, cx, cy)) {
            // What covers its middle is a dialog over it, or what it
            // scrolled under, unless it's another control's room for a
            // finger, reaching past that control's own box; a control
            // that's a dialog's, over this one, covers it as the dialog.
            const top = document.elementFromPoint(cx, cy);
            const layer = top?.closest('[role=dialog]');
            if (layer && !layer.contains(e)) continue;
            const over = top?.closest(TARGETS);
            const o = over?.getBoundingClientRect();
            // A point within a pixel of its box is in it, as hit testing
            // rounds a point onto the box's edge.
            if (over && !over.contains(e) && !e.contains(over) && !(cx >= o.left - 1 && cx <= o.right + 1 && cy >= o.top - 1 && cy <= o.bottom + 1)) {
                covered.push(desc(e) + ' under ' + desc(over) + "'s room");
            }
            continue;
        }
        if (inline(e)) continue;
        const size = Math.round(r.width) + '×' + Math.round(r.height);
        if (Math.min(r.width, r.height) < 23.5) small.push(desc(e) + ' ' + size);
        else if (across(e, r) && r.height < 43.5) rows.push(desc(e) + ' ' + size);
        else if (![[cx - 21, cy], [cx + 21, cy], [cx, cy - 21], [cx, cy + 21]].every(([x, y]) => hits(e, x, y))) tight.push(desc(e));
    }
    for (const e of document.querySelectorAll('button')) {
        if (e.getClientRects().length && getComputedStyle(e).visibility === 'hidden' && !disabled(e) && !e.closest('[aria-hidden=true], [inert]')) hoverOnly.push(desc(e));
    }
    return {
        vw, vh, sideways: Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) > vw + 1, listSideways,
        over: over.map((o) => o.what), small, rows, tight: tight.length, hoverOnly, covered,
    };
})()`;

// audits reports the audits' findings, screen by screen, for the browser
// or phone mode names, a touch screen's where touch says. On every screen
// nothing is under 24 pixels, nor under another control (M13.2), nor past
// the right edge outside a scroller of its own (M13.3), and a channel's
// message list never scrolls sideways (M13.4); on a touch screen
// no list's row is under 44, and nothing shows only on hover; and nothing
// makes the page itself scroll sideways on a den's panes (M13.1). The rest
// is noted.
export function audits(mode, log, report, touch) {
    for (const shot of log) {
        if (shot.failed) {
            report(false, `${mode}: ${shot.name} wasn't reached: ${shot.failed}`);
            continue;
        }
        const a = shot.audit;
        if (!a || a.error) continue;
        const bad = [];
        if (a.small.length) bad.push(`under 24: ${a.small.join('; ')}`);
        if (a.covered?.length) bad.push(`covered: ${a.covered.join('; ')}`);
        if (a.over.length) bad.push(`past the edge: ${a.over.slice(0, 3).join('; ')}`);
        if (a.listSideways) bad.push('the message list scrolls sideways');
        if (touch && a.rows.length) bad.push(`rows under 44: ${a.rows.join('; ')}`);
        if (touch && a.hoverOnly.length) bad.push(`on hover only: ${a.hoverOnly.join('; ')}`);
        const panes = /^(\d+-)?(den-list|channel|members|dm|description|video|around|touch)/.test(shot.name);
        if (panes && a.sideways) bad.push('the page scrolls sideways');
        const notes = [];
        if (!panes && a.sideways) notes.push('the page scrolls sideways');
        if (!touch && a.rows.length) notes.push(`${a.rows.length} rows under 44`);
        if (a.tight) notes.push(`${a.tight} reaching under 44`);
        report(!bad.length, `${mode}: ${shot.name}: ${bad.length ? bad.join('; ') : 'fits a finger'}${notes.length ? `; noted: ${notes.join(', ')}` : ''}`);
    }
}

// helpers works a page through the driver b, which has eval, go and shot,
// as browsers.mjs's browsers and drive.mjs's WebView do.
export function helpers(b, origin, out, prefix = '') {
    let n = 0;
    const log = [];
    const ev = (x) => b.eval(x, true);
    const has = (x) => ev(`!!(${x})`);
    const h = {
        b, origin, out, log, ev, has,
        byText: (sel, text) => `[...document.querySelectorAll(${JSON.stringify(sel)})].find((e) => e.textContent.trim() === ${JSON.stringify(text)})`,
        withText: (sel, text) => `[...document.querySelectorAll(${JSON.stringify(sel)})].find((e) => e.textContent.includes(${JSON.stringify(text)}))`,
        label: (l) => `document.querySelector(${JSON.stringify(`[aria-label="${l}"]`)})`,
        wait: (x, what, ms) => waitFor(() => has(x), what || x, ms),
        async click(x, what) {
            await waitFor(() => has(x), what || x);
            await ev(`(${x}).click(), true`);
            await sleep(450);
        },
        async escape() {
            await ev(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
            await sleep(300);
        },
        async go(path) {
            await b.go(origin + path);
            await sleep(800);
        },
        // snap takes the screen's picture and its audit; with scroll, the
        // element it names is scrolled a screen at a time for more, down,
        // or up with up.
        async snap(name, { scroll, up, max = 4 } = {}) {
            const take = async (suffix) => {
                const file = `${out}/${prefix}${String(++n).padStart(2, '0')}-${name}${suffix}`;
                await sleep(350);
                await b.shot(`${file}.png`);
                let audit;
                try {
                    audit = await ev(AUDIT);
                } catch (e) {
                    audit = { error: e.message };
                }
                fs.writeFileSync(`${file}.json`, JSON.stringify(audit, null, 1));
                log.push({ name: name + suffix, audit });
            };
            await take('');
            if (!scroll) return;
            for (let i = 1; i < max; i++) {
                const moved = await ev(`(() => { const e = ${scroll}; if (!e) return false; const before = e.scrollTop;
                    e.scrollTop += ${up ? -1 : 1} * Math.round(e.clientHeight * 0.85); return e.scrollTop !== before; })()`);
                if (!moved) break;
                await take(`-${i + 1}`);
            }
        },
        // step runs a screen's steps; one that fails is kept as such, and
        // the walk goes on.
        async step(name, fn) {
            try {
                await fn();
            } catch (e) {
                log.push({ name, failed: e.message });
                await h.snap(`${name}-failed`).catch(() => {});
            }
        },
    };
    return h;
}

const SETTINGS = `document.querySelector('[role=dialog][aria-label="App settings"]')`;
const PANEL = `document.querySelector('[role=dialog][aria-label="App settings"] [data-panel]')`;
const MANAGE = `document.querySelector('[role=dialog][aria-label^="Manage "]')`;
const MANAGE_PANEL = `document.querySelector('[role=dialog][aria-label^="Manage "] [data-panel]')`;
// A section of the settings or Manage by its name: its tab in a wide
// window, or its button in the column or the list of them (M13.3).
const section = (frame, name) => `[...document.querySelectorAll(${JSON.stringify(`${frame} [role=tab], ${frame} nav button`)})].find((t) => t.textContent.trim() === ${JSON.stringify(name)})`;
const SETTINGS_FRAME = '[role=dialog][aria-label="App settings"]';
const MANAGE_FRAME = '[role=dialog][aria-label^="Manage "]';
const SCROLLER = `document.querySelector('[data-scroller]')`;
const COMPOSER = `document.querySelector('textarea[placeholder^="Message "]')`;
const HOME = `[...document.querySelectorAll('h2')].find((e) => e.textContent.trim() === 'Your dens')`;

async function openSettings(h) {
    if (await h.has(SETTINGS)) return;
    await h.click(`document.querySelector('a[aria-label="App settings"], button[aria-label="App settings"]')`, 'the settings cog');
    await h.wait(SETTINGS, 'the settings');
}

// settingsTab opens a section of the settings by its name: its tab, or on
// the phone layout its page, from the list (M13.3).
async function settingsTab(h, name) {
    await openSettings(h);
    await openSection(h, SETTINGS_FRAME, name);
}

// openSection opens a section of the settings or Manage, framed by frame,
// by its name, going back to the list first from another section's page.
async function openSection(h, frame, name) {
    const back = `document.querySelector(${JSON.stringify(`${frame} [data-back]`)})`;
    if (!(await h.has(section(frame, name))) && (await h.has(back))) await h.click(back, 'back to the list');
    await h.click(section(frame, name), `the ${name} section`);
}

// sections names the sections a frame lists, or tabs, or a column holds.
const sections = (h, frame) => h.ev(`[...document.querySelectorAll(${JSON.stringify(`${frame} [role=tab], ${frame} nav button`)})].map((b) => b.textContent.trim())`);

// memberScreens are the screens every member reaches, in a den with
// general (text, with a description and every kind of message), a DM
// checked (dm) and maybe one waiting for its check (dm2).
export async function memberScreens(h, ctx) {
    const { den, general, dm, dm2 } = ctx;
    await h.step('home', async () => {
        await h.go('/');
        await h.wait(HOME, 'home');
        await h.snap('home', { scroll: `document.querySelector('main')` });
    });
    await h.step('home-add', async () => {
        await h.click(h.byText('button', 'Add a den'));
        await h.snap('home-add');
    });
    await h.step('settings', async () => {
        await h.go('/');
        await openSettings(h);
        if (await h.has(`document.querySelector('${SETTINGS_FRAME} [data-section]')`)) await h.snap('settings-list');
        await settingsTab(h, 'Appearance');
        await h.snap('settings-appearance', { scroll: PANEL });
        await h.click(h.withText('[role=dialog] button', 'More themes'));
        await h.snap('settings-themes', { scroll: PANEL, max: 2 });
        for (const [tab, name] of [['Chat & media', 'chat'], ['Voice & video', 'voice'], ['System', 'system']]) {
            await settingsTab(h, tab);
            await sleep(600);
            await h.snap(`settings-${name}`, { scroll: PANEL, max: 6 });
        }
    });
    await h.step('manage', async () => {
        await h.go('/');
        await h.click(h.byText('button', 'Manage'));
        await h.wait(MANAGE, 'Manage');
        if (await h.has(`document.querySelector('${MANAGE_FRAME} [data-section]')`)) await h.snap('manage-list');
        for (const s of await sections(h, MANAGE_FRAME)) {
            await openSection(h, MANAGE_FRAME, s);
            await sleep(500);
            await h.snap(`manage-${s.toLowerCase().replace(/[^a-z]+/g, '-')}`, { scroll: MANAGE_PANEL, max: 3 });
        }
    });
    await h.step('den-list', async () => {
        await h.go(`/den/${den}`);
        // A den reopens at the tab last open in it, which a DM's is after
        // a walk that ended in one.
        await h.wait(h.byText('[role=tab]', 'Den'), "the list's tabs");
        if (!(await h.has(`document.querySelector('nav[aria-label=Channels]')`))) await h.click(h.byText('[role=tab]', 'Den'));
        await h.wait(`document.querySelector('nav[aria-label=Channels]')`, 'the channel list');
        await h.snap('den-list');
    });
    await h.step('den-list-dms', async () => {
        await h.click(h.byText('[role=tab]', 'Direct messages'));
        await sleep(600);
        await h.snap('den-list-dms');
        await h.click(h.byText('[role=tab]', 'Den'));
    });
    await h.step('channel', async () => {
        await h.go(`/den/${den}/${general}`);
        await h.wait(COMPOSER, 'the composer');
        await sleep(1500);
        await h.snap('channel', { scroll: SCROLLER, up: true, max: 7 });
    });
    await h.step('description', async () => {
        await h.go(`/den/${den}/${general}`);
        await h.wait(COMPOSER, 'the composer');
        await h.click(`document.querySelector('header button[aria-expanded]')`, 'the description');
        await h.snap('description');
    });
    await h.step('message-menu', async () => {
        await h.go(`/den/${den}/${general}`);
        await h.wait(COMPOSER, 'the composer');
        await sleep(1200);
        await h.click(`[...document.querySelectorAll('button[aria-label="Message actions"]')].at(-1)`, 'a ⋯');
        await h.snap('message-menu');
        await h.click(`[...document.querySelectorAll('[role=menuitem]')].find((b) => b.textContent.trim() === 'Delete')`, 'Delete');
        await h.snap('delete-dialog');
        await h.click(h.byText('[role=dialog] button', 'Cancel'));
    });
    await h.step('edit', async () => {
        await h.click(`[...document.querySelectorAll('button[aria-label="Message actions"]')].at(-1)`);
        await h.click(`[...document.querySelectorAll('[role=menuitem]')].find((b) => b.textContent.trim() === 'Edit')`, 'Edit');
        await h.snap('edit');
        await h.escape();
    });
    await h.step('reply', async () => {
        await h.click(`[...document.querySelectorAll('button[aria-label="Message actions"]')].at(-2)`);
        await h.click(`[...document.querySelectorAll('[role=menuitem]')].find((b) => b.textContent.trim() === 'Reply')`, 'Reply');
        await h.snap('reply');
        await h.click(h.label('Cancel reply'));
    });
    await h.step('plus', async () => {
        await h.click(h.label('Add to this message'));
        await h.snap('plus-menu');
        await h.click(`[...document.querySelectorAll('[role=menuitem]')].find((b) => b.textContent.trim() === 'Let others edit')`);
        await h.snap('let-others-edit');
    });
    await h.step('guide', async () => {
        await h.go(`/den/${den}/${general}`);
        await h.click(h.label('How to write a message'));
        await h.snap('guide', { scroll: `document.querySelector('[role=dialog]')`, max: 4 });
        await h.escape();
    });
    if (ctx.photo) {
        await h.step('attach', async () => {
            await h.go(`/den/${den}/${general}`);
            await h.wait(COMPOSER, 'the composer');
            await h.b.attach('input[type=file][multiple]', [ctx.photo]);
            await waitFor(() => h.ev(`[...document.querySelectorAll('ul[aria-label="Files to send"] li')].some((li) => /smaller|full size/.test(li.textContent))`), 'the photo to settle', 60000);
            await h.ev(`document.querySelector('textarea[placeholder^="Message "]').blur(), true`);
            await h.snap('files-chip');
            await h.click(`[...document.querySelectorAll('ul[aria-label="Files to send"] li button')].find((b) => /ompare/.test(b.title + b.getAttribute('aria-label') + b.textContent))`, 'compare');
            await sleep(1500);
            await h.snap('compare');
            await h.escape();
        });
    }
    await h.step('viewer', async () => {
        await h.go(`/den/${den}/${general}`);
        await h.wait(COMPOSER, 'the composer');
        await sleep(1200);
        await h.click(`[...document.querySelectorAll('button[aria-label^="Open "]')].at(-1)`, 'a photo');
        await sleep(1200);
        await h.snap('viewer');
        await h.escape();
    });
    await h.step('video', async () => {
        await h.ev(`[...document.querySelectorAll('[data-id] video')].at(-1)?.scrollIntoView({ block: 'center' }), true`);
        await sleep(800);
        await h.snap('video-in-list');
    });
    await h.step('members', async () => {
        await h.go(`/den/${den}/${general}`);
        await h.click(h.label('Members'));
        await h.snap('members');
        await h.click(`document.querySelector('nav[aria-label=Members] button')`, 'a member');
        await h.snap('profile-card');
        await h.escape();
    });
    await h.step('edit-profile', async () => {
        await h.go(`/den/${den}`);
        await h.click(h.label('Edit your profile'));
        await h.snap('edit-profile', { scroll: `document.querySelector('[role=dialog]')`, max: 3 });
        await h.escape();
    });
    if (dm2) {
        await h.step('dm-check', async () => {
            await h.go(`/den/${den}/${dm2}`);
            await sleep(2500);
            await h.snap('dm-check');
        });
    }
    if (dm) {
        await h.step('dm', async () => {
            await h.go(`/den/${den}/${dm}`);
            await h.wait(COMPOSER, 'the DM');
            await sleep(1200);
            await h.snap('dm');
            await h.click(h.label('Profile'));
            await h.snap('dm-profile');
        });
    }
}

// ownerScreens are those only the den's owner reaches: a channel's and a
// group's settings, a new channel, the list with its ⋯, Manage's every
// section, the System tab with hosting, and removing a member.
export async function ownerScreens(h, ctx) {
    const { den, general } = ctx;
    await h.step('channel-dialog', async () => {
        await h.go(`/den/${den}`);
        await h.click(h.label('Settings for general'));
        await h.snap('channel-dialog', { scroll: `document.querySelector('[role=dialog]')`, max: 3 });
        await h.escape();
    });
    await h.step('group-dialog', async () => {
        await h.click(h.label('Settings for the group Projects'));
        await h.snap('group-dialog');
        await h.escape();
    });
    await h.step('new-channel', async () => {
        await h.click(h.byText('button', '+ Channel'));
        await h.snap('new-channel');
        await h.escape();
    });
    await h.step('owner-list', async () => {
        await h.snap('owner-list', { scroll: `document.querySelector('aside')`, max: 2 });
    });
    await h.step('owner-manage', async () => {
        await h.go('/');
        await h.click(h.byText('button', 'Manage'));
        await h.wait(MANAGE, 'Manage');
        if (await h.has(`document.querySelector('${MANAGE_FRAME} [data-section]')`)) await h.snap('manage-list');
        for (const s of await sections(h, MANAGE_FRAME)) {
            if (s === 'Leave den') continue;
            await openSection(h, MANAGE_FRAME, s);
            await sleep(500);
            await h.snap(`manage-${s.toLowerCase().replace(/[^a-z]+/g, '-')}`, { scroll: MANAGE_PANEL, max: 4 });
        }
        await h.escape();
    });
    await h.step('owner-system', async () => {
        await h.go('/');
        await settingsTab(h, 'System');
        await sleep(800);
        await h.snap('settings-system', { scroll: PANEL, max: 8 });
    });
    await h.step('profile-staff', async () => {
        await h.go(`/den/${den}/${general}`);
        await h.click(h.label('Members'));
        await h.click(`[...document.querySelectorAll('nav[aria-label=Members] button')].find((b) => b.textContent.includes('Bob'))`);
        await h.snap('profile-card-staff');
        await h.click(`[...document.querySelectorAll('[role=dialog] button')].find((b) => /Remove/.test(b.textContent))`);
        await h.snap('remove-member', { scroll: `document.querySelector('[role=dialog]')`, max: 2 });
    });
}

// CALL is what a page shows of its call on the phone layout (M13.4): the
// call's bar, under the channel's header; the call's button, its role, how
// it's pressed and what it says, its size and place, above the pane's foot
// with the composer; and the player docked under the bar, with the share's
// size, and whether it plays full screen.
export const CALL = `(() => {
    const box = (e) => { if (!e) return null; const r = e.getBoundingClientRect();
        return { top: Math.round(r.top), bottom: Math.round(r.bottom), left: Math.round(r.left), right: Math.round(r.right), w: Math.round(r.width), h: Math.round(r.height) }; };
    const pane = document.querySelector('[data-pane=chan]');
    const row = pane?.parentElement;
    const button = document.querySelector('[data-call-button]');
    const video = document.querySelector('section[data-docked] video');
    return {
        vw: document.documentElement.clientWidth, vh: innerHeight,
        rowScrolls: !!row && row.scrollHeight > row.clientHeight + 1, pageScrolls: document.scrollingElement.scrollHeight > innerHeight + 1,
        header: box(pane?.querySelector('header')), bar: box(document.querySelector('[data-call-bar]')),
        bar_text: document.querySelector('[data-call-bar] [role=status]')?.textContent || '',
        button: button && { ...box(button), role: button.dataset.callButton, label: button.getAttribute('aria-label'), pressed: button.getAttribute('aria-pressed'),
            talking: button.className.includes('bg-success'), struck: !!button.querySelector('path[d="M2 2l12 12"]') },
        composer: box(document.querySelector('textarea[placeholder^="Message "]')?.closest('.border-t')),
        docked: box(document.querySelector('section[data-docked]')),
        video: video && { w: video.videoWidth, h: video.videoHeight, playing: !video.paused },
        full: !!document.fullscreenElement,
    };
})()`;

// LIST_END scrolls the channel's message list to its end, and says how far
// above the list's foot its newest message ends: its own room at its end,
// 24 pixels, as the call's button floats over the list rather than making
// it longer (M13.4).
export const LIST_END = `(async () => {
    const s = document.querySelector('[data-pane=chan] [data-scroller]');
    if (!s) return null;
    s.scrollTop = s.scrollHeight;
    await new Promise((r) => setTimeout(r, 300));
    const last = [...s.querySelectorAll('[data-id]')].at(-1);
    return last ? Math.round(s.getBoundingClientRect().bottom - last.getBoundingClientRect().bottom) : null;
})()`;

// listEnds says whether the list ends at its newest message, from LIST_END,
// and in a call nothing else on the page scrolls, from CALL.
export const listEnds = (end, c) => end !== null && end <= 30 && !c.rowScrolls && !c.pageScrolls;

// barPlaced and buttonPlaced say whether CALL found the call's bar across
// the channel's pane under its header, 40 pixels tall, and the call's
// button 64 pixels round, 16 from the right and at least 16 above the
// composer.
export const barPlaced = (c) => !!c.bar && !!c.header && Math.abs(c.bar.top - c.header.bottom) <= 1 && c.bar.h === 40 && c.bar.w === c.vw;
export const buttonPlaced = (c) => !!c.button && c.button.w === 64 && c.button.h === 64 && c.vw - c.button.right === 16 && !!c.composer &&
    c.button.bottom <= c.composer.top - 16;

// dockedPlaced says whether CALL found the share docked under the call's
// bar at the screen's width and the share's shape, or half the screen's
// height where the share's taller, and playing.
export const dockedPlaced = (c) => !!c.docked && !!c.bar && !!c.video?.w && c.video.playing && c.docked.top === c.bar.bottom && c.docked.w === c.vw &&
    Math.abs(c.docked.h - Math.min(c.vw * c.video.h / c.video.w, c.vh / 2)) <= 1.5;

// selfRings says whether a page's own member rings in its call's list.
const selfRings = (den) => `fetch('/api/dens/${den}/state').then((r) => r.json()).then((v) => [...document.querySelectorAll('ul[aria-label="In the call"] li')]
    .some((li) => li.textContent.includes(v.me.display_name) && li.textContent.includes('speaking')))`;
const IN_LIST = (name) => `[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === ${JSON.stringify(name)})`;
const BUTTON = `document.querySelector('[data-call-button]')`;

// callScreens joins the den's voice channel from its list, and looks at
// the call on the list, a member's sheet, in a channel and at Home, and at
// a share, each screen with its picture and its audit. On the phone layout
// (M13.4), through the driver's d, as gettingAround and touching have it,
// the call's bar shows across the top of the channel's pane; where the
// page has the call's button, a touch screen's, d.down(x) and d.up() hold
// it, as Settings' Voice & video sets its role, toggling mute with a tap,
// as the default, then talking while it's held, and letting go of a press
// the browser cancels, by d.cancel(), or the page's losing focus; with a
// mouse, there's none, and Sending your voice keeps its place; a member's
// volume is a slider in a sheet, which a finger drags; the share docks
// above the channel, plays full screen by a tap on it and back, and where
// own says the browser shares its fake screen, that share docks at its
// shape, and a tap goes on to the other; and the call ends from the bar.
export async function callScreens(h, ctx, d, report, { share, own } = {}) {
    const { den } = ctx;
    const did = (good, what) => report(good, `${d.name}: ${what}`);
    const call = () => h.ev(CALL);
    await h.step('call', async () => {
        await h.go(`/den/${den}`);
        await h.click(h.byText('[role=tab]', 'Den'));
        await h.click(IN_LIST('Lounge'), 'Lounge');
        await waitFor(() => h.ev(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), 'connected', 30000);
        await sleep(1500);
        await h.snap('call-list');
        const share = await h.has(`[...document.querySelectorAll('button')].some((b) => b.getAttribute('aria-label') === 'Share your screen')`);
        const can = await h.ev(`typeof navigator.mediaDevices?.getDisplayMedia === 'function'`);
        did(share === can, `the call's block shows a share button only where the browser can share: ${share}`);
    });
    await h.step('call-menu', async () => {
        await h.click(`document.querySelector('button[data-call-menu]')`, "a member's ⋯ in the call");
        const slider = `document.querySelector('[role=dialog][data-sheet] input[type=range]')`;
        await h.wait(slider, "the member's sheet");
        await h.snap('call-member-menu');
        const tall = await h.ev(`Math.round(${slider}.getBoundingClientRect().height)`);
        did(tall >= 32, `a member's volume is a slider ${tall} pixels tall in a sheet`);
        if (d.touch) {
            const [x1, x2, y] = await h.ev(`(() => { const r = ${slider}.getBoundingClientRect(); return [r.right - 8, r.left + r.width * 0.4, r.top + r.height / 2]; })()`);
            await d.drag([x1, y], [x2, y]);
            const value = Number(await h.ev(`${slider}.value`));
            did(value < 100, `a finger drags it, from 100% to ${value}%, and the sheet stays: ${await h.has(slider)}`);
        }
        await d.back();
        await waitFor(async () => !(await h.has(slider)), 'the sheet to close', 5000);
    });
    // The page moves within itself, by its own buttons, which keeps the call.
    await h.step('call-channel', async () => {
        await h.click(IN_LIST('general'), 'general');
        await h.wait(COMPOSER, 'the composer');
        await sleep(600);
        await h.snap('call-in-channel');
        const c = await call();
        did(barPlaced(c) && c.bar_text.includes('Lounge'), `the call's bar shows across the channel's pane, under its header: ${JSON.stringify({ bar: c.bar, header: c.header?.bottom, text: c.bar_text })}`);
        if (d.touch) did(buttonPlaced(c) && c.button.role === 'toggleMute', `the call's button, 64 pixels at the screen's foot on the right, above the composer: ${JSON.stringify({ button: c.button, composer: c.composer?.top })}`);
        else did(!c.button, "with a mouse there's no call's button");
        const end = await h.ev(LIST_END);
        did(listEnds(end, c), `the list ends at its newest message, ${end} pixels above its foot, and nothing else scrolls: ${JSON.stringify({ row: c.rowScrolls, page: c.pageScrolls })}`);
    });
    if (d.touch) {
        await h.step('call-button', async () => {
            // Toggle mute, the default: a tap mutes, and another unmutes.
            await d.tap(BUTTON);
            let c = await call();
            const blockMuted = await h.has(`document.querySelector('[data-pane=list] button[aria-label="Unmute"]')`);
            did(c.button.pressed === 'true' && c.button.label === 'Unmute' && c.button.struck && blockMuted, `a tap on the call's button mutes, struck through: ${JSON.stringify(c.button)}`);
            await h.snap('call-button-muted');
            await d.tap(BUTTON);
            c = await call();
            did(c.button.pressed === 'false' && c.button.label === 'Mute' && !c.button.struck, `another unmutes: ${JSON.stringify(c.button)}`);
            // Push to talk, from the settings, where the button's role takes
            // Sending your voice's place.
            await h.click(`document.querySelector('[data-pane=list] button[aria-label="App settings"]')`, 'the settings');
            await settingsTab(h, 'Voice & video');
            const group = `document.querySelector('[role=radiogroup][aria-label="The call\\'s button"]')`;
            const roles = await h.ev(`[...(${group}?.querySelectorAll('label') || [])].map((l) => [l.textContent.trim(), l.querySelector('input').checked])`);
            const sending = await h.has(`document.querySelector('[role=radiogroup][aria-label="Sending your voice"]')`);
            did(JSON.stringify(roles) === JSON.stringify([['Toggle mute', true], ['Push to mute', false], ['Push to talk', false]]) && !sending,
                `the button's role takes Sending your voice's place, Toggle mute by default: ${JSON.stringify(roles)}`);
            await d.tap(`[...${group}.querySelectorAll('label')].find((l) => l.textContent.trim() === 'Push to talk')`);
            await h.snap('call-button-role');
            await d.back();
            await d.back();
            await waitFor(async () => !(await h.has(SETTINGS)), 'the settings to close', 5000);
            await waitFor(async () => (await call()).button?.role === 'ptt', 'the button to push to talk', 5000);
            await waitFor(async () => !(await h.ev(selfRings(den))), 'quiet until held', 5000);
            const [x, y] = await h.ev(`(() => { const r = ${BUTTON}.getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`);
            await d.down([x, y]);
            const rang = await waitFor(() => h.ev(selfRings(den)), 'the member to ring while held', 5000).catch(() => false);
            await sleep(1200);
            c = await call();
            const still = await h.ev(selfRings(den));
            await h.snap('call-button-held');
            did(rang && still && c.button.pressed === 'true' && c.button.talking, `held, past a long press, the call's button talks, in the ring's color, and the member rings: ${JSON.stringify(c.button)}`);
            await d.up();
            const quiet = await waitFor(async () => !(await h.ev(selfRings(den))) && (await call()).button.pressed === 'false', 'quiet once let go', 5000).catch(() => false);
            did(quiet, 'let go, the member stops ringing');
            // A press the browser takes over lets go, as one the page loses
            // focus during does.
            await d.down([x, y]);
            await waitFor(async () => (await call()).button.pressed === 'true', 'held again', 5000);
            if (d.cancel) await d.cancel();
            else await h.ev(`window.dispatchEvent(new Event('blur')), true`);
            const gone = await waitFor(async () => (await call()).button.pressed === 'false' && !(await h.ev(selfRings(den))), 'let go', 5000).catch(() => false);
            did(gone, `a press ${d.cancel ? 'the browser cancels' : 'held as the page loses focus'} lets go, and the member stops ringing`);
            if (!d.cancel) await d.up();
        });
    }
    await h.step('call-home', async () => {
        await h.click(`document.querySelector('button[aria-label^="Channels"]')`, 'the channels button');
        await h.click(`document.querySelector('a[aria-label^="Home"]')`, 'Home');
        await sleep(800);
        await h.snap('call-home');
    });
    if (share) {
        await h.step('share', async () => {
            await h.click(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Open')`, "the den's Open");
            await waitFor(() => h.has(`[...document.querySelectorAll('ul[aria-label="In the call"] button')].find((b) => b.textContent.trim() === 'LIVE')`), 'a LIVE mark', 60000);
            await h.click(`[...document.querySelectorAll('ul[aria-label="In the call"] button')].find((b) => b.textContent.trim() === 'LIVE')`, 'LIVE');
            await h.click(IN_LIST('general'), 'general');
            // The probe's share is Pion's packets, which show no picture, so
            // the player keeps 16:9 until a share it shows says its size.
            let c = await waitFor(async () => (await call()).docked && call(), 'the share docked', 20000).catch(call);
            did(!!c.docked && c.docked.top === c.bar?.bottom && c.docked.w === c.vw && Math.abs(c.docked.h - (c.vw * 9) / 16) <= 1.5,
                `carol's share docks above the channel, under the call's bar, at the screen's width, 16:9 until it shows a picture: ${JSON.stringify({ docked: c.docked, bar: c.bar?.bottom })}`);
            if (own) {
                // The member shares their own fake screen too, and shows it,
                // which the docked player shows at its shape, as the share
                // just begun, the second of two.
                await h.click(`document.querySelector('[data-pane=list] button[aria-label="Share your screen"]')`, 'share');
                await h.click(`document.querySelector('[data-pane=list] button[aria-label="Show your screen in the player"]')`, 'the preview');
                c = await waitFor(async () => {
                    const x = await call();
                    return x.video?.w > 0 && x;
                }, 'their own screen docked', 20000).catch(call);
                const place = await h.ev(`document.querySelector('section[data-docked] button[aria-label^="The next share"]')?.textContent.trim() || ''`);
                await h.snap('share-docked');
                did(dockedPlaced(c) && place.startsWith('2/2'), `the share just begun docks at the screen's width and its shape, the second of two: ${JSON.stringify({ docked: c.docked, video: c.video, place })}`);
                await d.tap(`document.querySelector('section[data-docked] button[aria-label^="The next share"]')`);
                const next = await h.ev(`document.querySelector('section[data-docked] button[aria-label^="The next share"]')?.textContent.trim() || ''`);
                did(next.startsWith('1/2'), `a tap goes on to the next: ${next}`);
                await d.tap(`document.querySelector('section[data-docked] button[aria-label^="The next share"]')`);
            } else {
                await h.snap('share-docked');
            }
            await d.tap(`document.querySelector('section[data-docked] video')`);
            const full = await waitFor(async () => (await call()).full, 'full screen', 5000).catch(() => false);
            await h.snap('share-full');
            await d.tap(`document.querySelector('section[data-docked] video')`);
            const back = await waitFor(async () => !(await call()).full && (await call()).docked, 'docked again', 5000).catch(() => false);
            did(full && back, `a tap on it plays it full screen (${full}), and another docks it again (${!!back})`);
            if (own) await h.click(`[...document.querySelectorAll('[data-pane=list] button')].find((b) => b.textContent.trim() === 'Stop')`, 'stop sharing');
        });
    }
    await h.step('leave', async () => {
        if (!(await h.has(`document.querySelector('[data-call-bar]')`))) {
            await h.click(IN_LIST('general'), 'general');
        }
        await d.tap(`document.querySelector('[data-call-bar] button[aria-label="Leave the call"]')`);
        const left = await waitFor(async () => !(await h.has(`document.querySelector('[data-call-bar]')`)) && !(await h.has(BUTTON)), 'the call to end', 10000)
            .catch(() => false);
        did(left, 'the call ends from its bar, and its bar and button go');
    });
}

// staffPress walks staff's long press on a touch screen (M13.4): a
// channel's row opens its settings, and a group's the group's, where a
// touch screen shows no ⋯ for them, and a tap on a row still opens its
// channel.
export async function staffPress(h, ctx, d, report) {
    const { den, random } = ctx;
    const did = (good, what) => report(good, `${d.name}: ${what}`);
    const dialog = () => h.ev(`document.querySelector('[role=dialog]')?.getAttribute('aria-label') || ''`);
    await h.go(`/den/${den}`);
    if (!(await h.has(IN_LIST('general')))) await h.click(h.byText('[role=tab]', 'Den'));
    await h.wait(IN_LIST('general'), 'the channel list');
    const dots = await h.ev(`[...document.querySelectorAll('nav[aria-label=Channels] button[aria-label^="Settings for"]')].filter((b) => getComputedStyle(b).display !== 'none').length`);
    did(dots === 0, `staff's channels and groups show no ⋯ on a touch screen (${dots} show)`);
    await d.press(IN_LIST('general'));
    const channel = await waitFor(dialog, "general's settings", 5000).catch(() => '');
    await h.snap('staff-press-channel');
    did(channel === '#general', `a long press on a channel's row opens its settings: ${channel || 'nothing'}`);
    await d.back();
    await waitFor(async () => !(await dialog()), 'the settings to close', 5000);
    await d.press(`[...document.querySelectorAll('nav[aria-label=Channels] button[aria-expanded]')].find((b) => b.textContent.trim().toLowerCase() === 'projects')`);
    const group = await waitFor(dialog, "Projects' settings", 5000).catch(() => '');
    did(group === 'Projects', `and a long press on a group's row opens the group's: ${group || 'nothing'}`);
    await d.back();
    await waitFor(async () => !(await dialog()), 'the settings to close', 5000);
    await d.tap(IN_LIST('random'));
    const opened = await waitFor(async () => (await h.ev('location.pathname')) === `/den/${den}/${random}` && !(await dialog()), 'random', 5000).catch(() => false);
    did(opened, 'a tap on a row still opens its channel');
}

// PANE is what the page shows of a den: the pane on screen, by the panes
// not inert and where each sits, and whether the settings are open.
const PANE = `(() => {
    const row = document.querySelector('[data-pane=chan]')?.parentElement;
    const settings = !!document.querySelector('[role=dialog][aria-label="App settings"]');
    if (!row) return { path: location.pathname, home: !!(${HOME}), settings };
    const vw = document.documentElement.clientWidth;
    const on = [...row.children].filter((e) => e.dataset.pane && !e.inert).map((e) => {
        const r = e.getBoundingClientRect();
        return { pane: e.dataset.pane, whole: r.left >= -1 && r.right <= vw + 1 };
    });
    return { path: location.pathname, pane: on.length === 1 && on[0].whole ? on[0].pane : on.map((p) => p.pane).join('+'), settings };
})()`;

// gettingAround walks M13.1's done-when on the phone layout, by what the
// driver's d does as a member would: d.swipe('right') moves to the pane on
// the left, d.tap(x) taps what the expression x names, or the point x
// as [x, y] in the page's pixels, d.back() is the
// browser's or the phone's Back, and, where d.out says, Back at Home
// leaves the page. From general, opened from Home where d.fromHome says,
// and at its address otherwise, it swipes to the list and back, to the
// members and back; opens random from the list; closes the settings with
// Back; taps the channel at the list's edge; and from the members goes
// back to the channel, the list and Home.
export async function gettingAround(h, ctx, d, report) {
    const { den, general, random } = ctx;
    const at = () => h.ev(PANE);
    const settles = async (want, what) => {
        try {
            const got = await waitFor(async () => {
                const s = await at();
                return Object.entries(want).every(([k, v]) => s[k] === v) && s;
            }, what, 10000);
            report(true, `${d.name}: ${what} (${got.path})`);
        } catch (e) {
            report(false, `${d.name}: ${what}: ${JSON.stringify(await at())}`);
            throw e;
        }
    };
    const inList = (name) => `[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === ${JSON.stringify(name)})`;
    if (d.fromHome) {
        await d.tap(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Open')`);
        await settles({ pane: 'list', path: `/den/${den}` }, "Home's Open shows the den's list");
        // A den reopens at the tab last open in it.
        if (!(await h.has(inList('general')))) await d.tap(`[...document.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim() === 'Den')`);
        await d.tap(inList('general'));
    } else {
        await h.go(`/den/${den}/${general}`);
    }
    await h.wait(COMPOSER, 'the composer');
    await settles({ pane: 'chan', path: `/den/${den}/${general}` }, d.fromHome ? 'a channel tapped in the list opens' : 'a page opened at a channel shows it');
    await d.swipe('right');
    await settles({ pane: 'list', path: `/den/${den}` }, 'a swipe right goes to the list, a level up');
    await h.snap('around-list');
    await d.swipe('left');
    await settles({ pane: 'chan', path: `/den/${den}/${general}` }, 'a swipe left goes back to the channel');
    await d.swipe('left');
    await settles({ pane: 'side', path: `/den/${den}/${general}` }, 'another goes to the members');
    await h.snap('around-members');
    await d.swipe('right');
    await settles({ pane: 'chan' }, 'and a swipe right comes back');
    await d.swipe('right');
    await settles({ pane: 'list' }, 'to the list again');
    await d.tap(inList('random'));
    await settles({ pane: 'chan', path: `/den/${den}/${random}` }, 'a channel tapped in the list opens');
    await d.swipe('right');
    await settles({ pane: 'list' }, 'the list once more');
    await d.tap(`document.querySelector('[data-pane=list] button[aria-label="App settings"]')`);
    await settles({ settings: true, pane: 'list' }, 'the settings open over the list');
    await h.snap('around-settings');
    await d.back();
    await settles({ settings: false, pane: 'list', path: `/den/${den}` }, 'Back closes the settings, and the list stays');
    await d.tap([372, 420]);
    await settles({ pane: 'chan', path: `/den/${den}/${random}` }, "a tap on the channel, dimmed at the list's edge, shows the channel the list had open");
    await d.tap(`document.querySelector('[data-pane=chan] header button[aria-label="Members"]')`);
    await settles({ pane: 'side' }, "the header's people button goes to the members");
    await d.back();
    await settles({ pane: 'chan', path: `/den/${den}/${random}` }, 'Back goes from the members to the channel');
    await d.back();
    await settles({ pane: 'list', path: `/den/${den}` }, 'Back goes from the channel to the list');
    await d.back();
    await settles({ home: true, path: '/' }, 'Back goes from the list to Home');
    if (d.out) await d.out();
}

const SHEET = `document.querySelector('[role=dialog][data-sheet]')`;
const NEWEST = `[...document.querySelectorAll('[data-scroller] [data-id]')].at(-1)`;
const SEND = `document.querySelector('button[aria-label="Send"]')`;

// touching walks M13.2's done-when, by what the driver's d does as a
// member would, on the phone layout of ctx's den at general. With a
// finger, d.touch, d.press(x) holds a finger on what the expression x
// names, a long press; d.drag(from, to) drags between two of the page's
// points, a finger or the mouse; d.type(text) types, d.enter() presses
// the keyboard's ↵, and with a mouse d.hover(x) hovers and d.shiftEnter()
// presses Shift and Enter. d.tap and d.back are gettingAround's.
// d.keyboard(text), where given, waits for a phone's keyboard to come up,
// and says whether the composer and the newest message, holding text if
// given, show above it. It opens a message's actions by a long press, or
// the ⋯ with a mouse, and closes their sheet by a swipe down, and by Back;
// writes three lines and sends them; and opens a tip with a tap, which the
// next closes.
export async function touching(h, ctx, d, report) {
    const { den, general } = ctx;
    const did = (good, what) => report(good, `${d.name}: ${what}`);
    const sheet = () => h.ev(`(() => { const s = ${SHEET}; if (!s) return null;
        return { label: s.getAttribute('aria-label'), rows: [...s.querySelectorAll('[role=menuitem]')].map((r) => r.textContent.trim()),
            title: s.querySelector('[data-sheet-top]')?.innerText.trim() || '', layers: document.querySelectorAll('[role=dialog]').length }; })()`);
    // A mouse finds the ⋯ by hovering its message first.
    const open = async () => {
        if (d.touch) {
            await d.press(NEWEST);
        } else {
            await d.hover(NEWEST);
            await d.tap(`${NEWEST}.querySelector('button[aria-label="Message actions"]')`);
        }
        return waitFor(sheet, 'the sheet', 5000).catch(() => null);
    };
    const closed = () => waitFor(async () => !(await sheet()), 'the sheet gone', 5000).then(() => true, () => false);
    await h.go(`/den/${den}/${general}`);
    await h.wait(COMPOSER, 'the composer');
    await sleep(1500);
    // A finger has no ⋯ on a message, and a mouse has one on each.
    const dots = await h.ev(`[...document.querySelectorAll('[data-scroller] button[aria-label="Message actions"]')].filter((b) => getComputedStyle(b).display !== 'none').length`);
    did(d.touch ? dots === 0 : dots > 0, d.touch ? 'no message shows a ⋯' : 'each message has its ⋯');
    let s = await open();
    did(s?.label === 'Message actions' && s.rows.includes('Reply') && s.rows.includes('Copy link') && s.title && s.layers === 1,
        `${d.touch ? 'a long press' : 'the ⋯'} opens the message's actions as a sheet, headed by its author and time: ${JSON.stringify(s)}`);
    await h.snap('touch-sheet');
    if (s) {
        const top = await h.ev(`(() => { const r = ${SHEET}.getBoundingClientRect(); return [r.left + r.width / 2, r.top + 12]; })()`);
        await d.drag(top, [top[0], top[1] + 260]);
        did(await closed(), 'a swipe down closes it');
    }
    s = await open();
    if (s) {
        await d.back();
        const gone = await closed();
        const path = await h.ev('location.pathname');
        did(gone && path === `/den/${den}/${general}`, `Back closes it, and the channel stays (${path})`);
    } else {
        did(false, "the sheet didn't open again");
    }

    // Each line starts with a digit, which a phone's keyboard doesn't
    // capitalize at a line's start, as it does a word.
    const stamp = Date.now().toString(36);
    const lines = [`1 ${stamp}`, '2 two', '3 three'];
    const newest = () => h.ev(`fetch('/api/dens/${den}/channels/${general}/messages?limit=1').then((r) => r.json()).then((p) => p.messages.at(-1)?.text || '')`);
    const before = await newest();
    await d.tap(COMPOSER);
    if (d.keyboard) {
        const k = await d.keyboard();
        did(k.ok, `the keyboard comes up, and leaves the composer and the newest message in sight: ${k.said}`);
    }
    if (d.touch) {
        await d.type(lines[0]);
        await d.enter();
        await d.type(lines[1]);
        await d.enter();
        await d.type(lines[2]);
        await sleep(500);
        const box = await h.ev(`${COMPOSER}.value`);
        const kept = (await newest()) === before;
        const button = await h.has(SEND);
        did(box === lines.join('\n') && kept && button, `↵ starts a line: the box holds ${JSON.stringify(box)}, nothing sent (${kept}), and the send button shows (${button})`);
        await d.tap(SEND);
    } else {
        await d.type(lines[0]);
        await d.shiftEnter();
        await d.type(lines[1]);
        await d.shiftEnter();
        await d.type(lines[2]);
        const button = await h.has(SEND);
        did(!button, `a mouse has no send button (${!button}), and Shift+Enter starts a line`);
        await d.enter();
    }
    const sent = await waitFor(async () => (await newest()) === lines.join('\n'), 'the three lines sent', 10000).then(() => true, () => false);
    const after = await h.ev(`({ box: ${COMPOSER}.value, focused: document.activeElement === ${COMPOSER} })`);
    did(sent && after.box === '' && (!d.touch || after.focused),
        `${d.touch ? 'the send button' : 'Enter'} sends the three lines (${sent}), leaving the box empty${d.touch ? `, with the keyboard's focus (${after.focused})` : ''}`);
    if (d.keyboard) {
        const k = await d.keyboard(lines[2]);
        did(k.ok, `with the keyboard up, the message sent shows above the composer: ${k.said}`);
    }
    await h.snap('touch-sent');
    // The keyboard goes, since Back would take it before the page.
    await h.ev('document.activeElement?.blur(), true');
    await sleep(800);

    // A tip opens on a tap within the screen, and the next tap closes it.
    await openSettings(h);
    await settingsTab(h, 'Chat & media');
    const TIP = `document.querySelector('[role=dialog][aria-label="App settings"] button[aria-label="More about this"]')`;
    const shows = () => h.ev(`(() => { const t = document.getElementById(${TIP}.getAttribute('aria-describedby'));
        if (!t || t.hidden) return null; const r = t.getBoundingClientRect(); return { left: Math.round(r.left), right: Math.round(r.right), vw: document.documentElement.clientWidth }; })()`);
    await d.tap(TIP);
    const tip = await waitFor(shows, 'the tip', 3000).catch(() => null);
    await h.snap('touch-tip');
    await d.tap(`[...document.querySelectorAll('[role=dialog][aria-label="App settings"] h2')].at(-1)`);
    const shut = await waitFor(async () => !(await shows()), 'the tip closed', 3000).then(() => true, () => false);
    did(tip && tip.left >= 0 && tip.right <= tip.vw && shut, `a tap on an i opens its tip within the screen (${JSON.stringify(tip)}), and the next tap closes it (${shut})`);
    await d.back();
}

// VIEWER is the viewer, as the walk reads it: which picture of how many it
// shows, the stage's width and the shown picture's, whether it's zoomed
// past that, and the address under it.
const VIEWER = `(() => {
    const v = [...document.querySelectorAll('[role=dialog]')].find((d) => d.querySelector('[data-stage]'));
    if (!v) return null;
    const stage = v.querySelector('[data-stage]').getBoundingClientRect();
    const shown = v.querySelector('[data-shown]').getBoundingClientRect();
    const count = [...v.querySelectorAll('span')].map((e) => e.textContent.trim()).find((t) => /^\\d+ of \\d+$/.test(t)) || '';
    return { count, stage: [Math.round(stage.left), Math.round(stage.top), Math.round(stage.width), Math.round(stage.height)],
        shown: [Math.round(shown.left), Math.round(shown.top), Math.round(shown.width), Math.round(shown.height)], path: location.pathname };
})()`;

// pages walks M13.3's done-when on the phone layout, by what the driver's
// d does as a member would; d.tap, d.back and d.drag are gettingAround's
// and touching's, d.double(x) taps twice quickly, or clicks, on what x
// names, d.pinch(at, from, to), where given, spreads two fingers about the
// point at from a gap of from to to, and d.wheel(at, dy), where given,
// turns the wheel there. The settings open as a list of their sections,
// each a page, which Back leaves for the list, and Back leaves the list,
// closing them; where d.phone says, Voice & video holds no keyboard
// shortcuts or screen share, which a browser's hold. Manage goes the same
// way. The viewer, on a message's two photos, moves to the second by a
// swipe, zooms by a double tap, and by two fingers or the wheel, and Back
// closes it; and a dialog, as a sheet, closes by a swipe down its top.
export async function pages(h, ctx, d, report) {
    const { den, general } = ctx;
    const did = (good, what) => report(good, `${d.name}: ${what}`);
    const frame = (f) => `document.querySelector(${JSON.stringify(f)})`;
    const state = (f) => h.ev(`(() => { const s = ${frame(f)}; if (!s) return { open: false, path: location.pathname };
        return { open: true, path: location.pathname, title: s.querySelector('h2')?.textContent.trim(), rows: [...s.querySelectorAll('[data-section]')].map((b) => b.textContent.trim()),
            page: !!s.querySelector('[data-panel]'), cards: [...s.querySelectorAll('[data-panel] .card-title')].map((t) => t.textContent.trim()) }; })()`);
    const until = (f, test, what) => waitFor(async () => { const s = await state(f); return test(s) && s; }, what, 6000).catch(() => null);
    // walk opens each section of a list from it, and Back from each.
    const walk = async (f, name, title, check) => {
        const list = await until(f, (s) => s.open && s.rows.length, `${name}'s list`);
        did(!!list && !list.page, `${name}, opened, list their sections: ${JSON.stringify(list?.rows)}`);
        for (const row of list?.rows || []) {
            await d.tap(`[...document.querySelectorAll('${f} [data-section]')].find((b) => b.textContent.trim() === ${JSON.stringify(row)})`);
            const page = await until(f, (s) => s.page, `${row}'s page`);
            did(!!page && (title === null || page.title === (title || row)), `${row} opens as a page with ← back (${page?.title})`);
            if (page) await check?.(row, page);
            await d.back();
            const back = await until(f, (s) => s.open && !s.page && s.rows.length, 'the list again');
            did(!!back, `Back goes from ${row} to the list`);
        }
        await d.back();
        const closed = await until(f, (s) => !s.open, `${name} closed`);
        did(!!closed && closed.path === '/', `Back from the list closes ${name} (${closed?.path})`);
    };

    await h.go('/');
    await h.wait(HOME, 'home');
    await d.tap(`document.querySelector('a[aria-label="App settings"]')`);
    await walk(SETTINGS_FRAME, 'the settings', '', async (row, page) => {
        if (row !== 'Voice & video') return;
        const keys = page.cards.includes('Keyboard shortcuts');
        const share = page.cards.includes('Screen share');
        did(d.phone ? !keys && !share : keys && share, d.phone
            ? `the phone's Voice & video holds no keyboard shortcuts or screen share (${page.cards.join(', ')})`
            : `a browser's Voice & video holds the keyboard shortcuts and screen share (${page.cards.join(', ')})`);
    });
    await d.tap(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Manage')`);
    // A den's sections are headed by the den's name.
    await walk(MANAGE_FRAME, 'Manage', null);

    await h.go(`/den/${den}/${general}`);
    await h.wait(COMPOSER, 'the composer');
    await sleep(1200);
    const photos = `[...document.querySelectorAll('[data-scroller] [data-id]')].reverse().find((m) => m.querySelectorAll('button[aria-label^="Open "]').length >= 2)`;
    await d.tap(`${photos}.querySelector('button[aria-label^="Open "]')`);
    let v = await waitFor(() => h.ev(VIEWER), 'the viewer', 6000).catch(() => null);
    did(v?.count === '1 of 2' && v.shown[2] <= v.stage[2] + 1 && v.stage[2] >= 389, `the viewer opens full screen on the message's first photo, its width the screen's: ${JSON.stringify(v)}`);
    await h.snap('pages-viewer');
    if (v) {
        const y = v.stage[1] + v.stage[3] / 2;
        await d.drag([330, y], [60, y]);
        v = await waitFor(async () => { const n = await h.ev(VIEWER); return n?.count === '2 of 2' && n; }, 'the second photo', 4000).catch(async () => h.ev(VIEWER));
        did(v?.count === '2 of 2', `a swipe to the left moves to the second photo (${v?.count})`);
        const fit = v.shown[2];
        const middle = `[${v.shown[0] + v.shown[2] / 2}, ${v.shown[1] + v.shown[3] / 2}]`;
        const zoomed = async (what) => {
            const z = await waitFor(async () => { const n = await h.ev(VIEWER); return n && n.shown[2] > fit * 1.5 && n; }, what, 3000).catch(async () => h.ev(VIEWER));
            return { ok: !!z && z.shown[2] > fit * 1.5, z };
        };
        await d.double(JSON.parse(middle));
        let z = await zoomed('a double tap\'s zoom');
        did(z.ok, `${d.touch ? 'a double tap' : 'a double click'} zooms it: ${fit} pixels wide to ${z.z?.shown[2]}`);
        await h.snap('pages-zoomed');
        await d.double(JSON.parse(middle));
        const out = await waitFor(async () => { const n = await h.ev(VIEWER); return n && Math.abs(n.shown[2] - fit) < 2 && n; }, 'fitted again', 3000).catch(() => null);
        did(!!out, `and another fits it again (${out?.shown[2]})`);
        if (d.pinch) {
            await d.pinch(JSON.parse(middle), 60, 240);
            z = await zoomed('a pinch\'s zoom');
            did(z.ok, `two fingers spread zoom it: ${fit} pixels wide to ${z.z?.shown[2]}`);
        } else if (d.wheel) {
            // A turn zooms at most half again, however far it goes.
            await d.wheel(JSON.parse(middle), -400);
            await d.wheel(JSON.parse(middle), -400);
            z = await zoomed('the wheel\'s zoom');
            did(z.ok, `the wheel zooms it: ${fit} pixels wide to ${z.z?.shown[2]}`);
        }
        await d.back();
        const gone = await waitFor(async () => !(await h.ev(VIEWER)), 'the viewer closed', 4000).then(() => true, () => false);
        const path = await h.ev('location.pathname');
        did(gone && path === `/den/${den}/${general}`, `Back closes the viewer, and the channel stays (${path})`);
    }

    // The writing guide, a dialog, is a sheet, which a swipe down its top
    // closes.
    await d.tap(`document.querySelector('[aria-label="How to write a message"]')`);
    const sheet = await waitFor(() => h.ev(`(() => { const s = document.querySelector('[role=dialog][aria-label="Writing a message"]'); if (!s) return null;
        const r = s.getBoundingClientRect(); return [Math.round(r.left), Math.round(r.top), Math.round(r.width), Math.round(r.bottom), innerHeight]; })()`), 'the guide', 4000).catch(() => null);
    did(!!sheet && sheet[0] === 0 && sheet[2] >= 389 && sheet[3] >= sheet[4] - 1, `the writing guide opens as a sheet from the screen's foot: ${JSON.stringify(sheet)}`);
    if (sheet) {
        await d.drag([sheet[2] / 2, sheet[1] + 14], [sheet[2] / 2, sheet[1] + 14 + 0.6 * (sheet[3] - sheet[1])]);
        const shut = await waitFor(async () => !(await h.has(`document.querySelector('[role=dialog][aria-label="Writing a message"]')`)), 'the guide closed', 4000).then(() => true, () => false);
        did(shut, 'a swipe down its top closes it');
    }
}
