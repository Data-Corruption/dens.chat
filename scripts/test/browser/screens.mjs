// The phone check's screens (M13), which the browsers' driver
// (drive-phone.mjs) and the Android e2e's walk (scripts/test/android/
// walk.mjs) share: each step brings the page to a screen, and snap takes
// its picture with an audit of its layout beside it. Steps reach screens by
// script, as the browser checks do, so a screen shows whatever covers its
// buttons; what a finger can do is getting around's, below, through the
// driver's own swipes and Back.
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
// of its own, the controls smaller than 24 pixels, the least a finger
// should be given, or 44, a comfortable one, and the controls hidden until
// a hover. Panes off screen are inert, and left out.
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
    const small = [], tight = [], hoverOnly = [];
    const targets = document.querySelectorAll('button, a[href], input:not([type=hidden]), select, textarea, [role=tab], [role=menuitem], summary, label:has(input)');
    for (const e of targets) {
        if (!shown(e) || e.closest('[aria-hidden=true], [inert]')) continue;
        const r = e.getBoundingClientRect();
        if (r.bottom < 0 || r.top > vh || r.right < 0 || r.left > vw) continue;
        const s = getComputedStyle(e);
        if (s.opacity === '0' || s.pointerEvents === 'none') { hoverOnly.push(desc(e)); continue; }
        if (e.tagName === 'INPUT' && ['checkbox', 'radio'].includes(e.type) && e.closest('label')) continue;
        const min = Math.min(r.width, r.height);
        if (min < 24) small.push(desc(e) + ' ' + Math.round(r.width) + '×' + Math.round(r.height));
        else if (min < 44) tight.push(desc(e));
    }
    for (const e of document.querySelectorAll('button')) {
        if (e.getClientRects().length && getComputedStyle(e).visibility === 'hidden' && !e.closest('[aria-hidden=true], [inert]')) hoverOnly.push(desc(e));
    }
    return {
        vw, vh, sideways: Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) > vw + 1,
        over: over.map((o) => o.what), small, tight: tight.length, hoverOnly,
    };
})()`;

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
const PANEL = `document.querySelector('[role=dialog][aria-label="App settings"] [role=tabpanel]')`;
const MANAGE = `document.querySelector('[role=dialog][aria-label^="Manage "]')`;
const MANAGE_PANEL = `document.querySelector('[role=dialog][aria-label^="Manage "] .overflow-y-auto:not(nav)')`;
const SCROLLER = `document.querySelector('[data-scroller]')`;
const COMPOSER = `document.querySelector('textarea[placeholder^="Message "]')`;
const HOME = `[...document.querySelectorAll('h2')].find((e) => e.textContent.trim() === 'Your dens')`;

async function openSettings(h) {
    if (await h.has(SETTINGS)) return;
    await h.click(`document.querySelector('a[aria-label="App settings"], button[aria-label="App settings"]')`, 'the settings cog');
    await h.wait(SETTINGS, 'the settings');
}

async function settingsTab(h, name) {
    await openSettings(h);
    await h.click(`[...document.querySelectorAll('[role=dialog][aria-label="App settings"] [role=tab]')].find((t) => t.textContent.trim() === ${JSON.stringify(name)})`, `the ${name} tab`);
}

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
        const sections = await h.ev(`[...document.querySelectorAll('[role=dialog] nav button')].map((b) => b.textContent.trim())`);
        for (const s of sections) {
            await h.click(h.byText('[role=dialog] nav button', s));
            await sleep(500);
            await h.snap(`manage-${s.toLowerCase().replace(/[^a-z]+/g, '-')}`, { scroll: MANAGE_PANEL, max: 3 });
        }
    });
    await h.step('den-list', async () => {
        await h.go(`/den/${den}`);
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
        await h.snap('guide', { scroll: `[...document.querySelectorAll('[role=dialog] *')].find((e) => e.scrollHeight > e.clientHeight + 4 && getComputedStyle(e).overflowY !== 'visible')`, max: 4 });
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
        const sections = await h.ev(`[...document.querySelectorAll('[role=dialog] nav button')].map((b) => b.textContent.trim())`);
        for (const s of sections) {
            if (s === 'Leave den') continue;
            await h.click(h.byText('[role=dialog] nav button', s));
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

// callScreens joins the den's voice channel from its list, and looks at
// the call on the list, in a channel and at Home, and at a share.
export async function callScreens(h, ctx, { share } = {}) {
    const { den, general } = ctx;
    await h.step('call', async () => {
        await h.go(`/den/${den}`);
        await h.click(h.byText('[role=tab]', 'Den'));
        await h.click(`[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === 'Lounge')`, 'Lounge');
        await waitFor(() => h.ev(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), 'connected', 30000);
        await sleep(1500);
        await h.snap('call-list');
    });
    await h.step('call-menu', async () => {
        await h.click(`document.querySelector('button[data-call-menu]')`, "a member's ⋯ in the call");
        await h.snap('call-member-menu');
        await h.escape();
    });
    // The page moves within itself, by its own buttons, which keeps the call.
    await h.step('call-channel', async () => {
        await h.click(`[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === 'general')`, 'general');
        await h.wait(COMPOSER, 'the composer');
        await h.snap('call-in-channel');
    });
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
            await sleep(2500);
            await h.snap('share-player');
        });
    }
    await h.step('leave', async () => {
        await h.click(`document.querySelector('button[aria-label="Leave the call"], button[aria-label^="Leave"]')`, 'leave');
    });
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
