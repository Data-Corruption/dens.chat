// The phone check (M13): every screen the browser checks reach, at 390 by
// 844 pixels, in Chromium as a phone (a mobile viewport, twice the pixels,
// and a touch screen), in Firefox as a touch screen (its pointer prefs),
// and in Firefox with a mouse, as a narrow desktop window; Firefox's
// viewport goes to 390 through BiDi, since it keeps a window at least 500
// wide. Each screen's picture and its layout's audit land in
// ~/shot-phone/<browser>, and the check fails on a control under 24
// pixels, and on a touch screen on a list's row under 44 or a control only
// a hover shows (M13.2). Then, as a member would, getting around (M13.1):
// swipes between the panes, by touch drags or a touchpad's sideways
// scroll, taps, and the browser's Back, through the settings, the
// members, the channel, the list and Home; and touch (M13.2): a message's
// actions by a long press, or the ⋯ with a mouse, in a sheet closed by a
// swipe down and by Back, three lines written and sent, by the send button
// with a finger, and a tip opened and closed by taps; and pages (M13.3):
// the settings and Manage as lists of sections, each a page Back leaves,
// the viewer swiped to a message's second photo, zoomed by a double tap,
// and by two fingers or the wheel, and closed by Back, and a dialog as a
// sheet, swiped down; and the call (M13.4): its bar across the top of the
// channel's pane, the call's button held and tapped on a touch screen, as
// its role in the settings says, and absent with a mouse, a member's
// volume dragged in a sheet, a share docked above the channel, full
// screen and back, and the call left from its bar; and staff's long press
// on a channel's or a group's row for its settings, on a touch screen.
//
//   node drive-phone.mjs [chromium|firefox|firefox-mouse]... [-- PART...]
//
// The parts are member, around, touch, pages, call, owner and before; all
// by default.
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, CAROL, HOME, checks, launch, lib, pair, token } from './browsers.mjs';
import { audits, callScreens, gettingAround, helpers, memberScreens, ownerScreens, pages, staffPress, touching, waitFor } from './screens.mjs';

const args = process.argv.slice(2);
const cut = args.indexOf('--');
const modes = (cut < 0 ? args : args.slice(0, cut)).filter(Boolean);
const parts = cut < 0 ? [] : args.slice(cut + 1);
const want = (p) => parts.length === 0 || parts.includes(p);
const { DEN: den, GENERAL: general, RANDOM_ID: random, LOUNGE: lounge, DM: dm, DM_CHECK: dm2 } = process.env;
const SIZE = [390, 844];
const { report, fail, finish } = checks();

// browser launches one of the modes at 390 by 844.
async function browser(mode, port, opts = {}) {
    const name = mode === 'chromium' ? 'chromium' : 'firefox';
    const touch = mode !== 'firefox-mouse';
    const prefs = touch && name === 'firefox' ? { 'ui.primaryPointerCapabilities': 1, 'ui.allPointerCapabilities': 1, 'dom.w3c_touch_events.enabled': 1 } : {};
    const b = await launch(name, port, { size: SIZE, hover: !touch, prefs, ...opts });
    if (name === 'chromium') {
        await b.cdp('Emulation.setDeviceMetricsOverride', { width: SIZE[0], height: SIZE[1], deviceScaleFactor: 2, mobile: true });
        await b.touch(true);
    } else {
        await b.bidi('browsingContext.setViewport', { context: b.context, viewport: { width: SIZE[0], height: SIZE[1] } });
    }
    return b;
}

// driver is what a member does on mode's screen, for gettingAround: a
// swipe starts across the panes' headers, clear of anything else that
// scrolls sideways.
function driver(mode, b, h) {
    const center = async (x) => {
        if (Array.isArray(x)) return x;
        const r = await h.ev(`(() => { const e = ${x}; if (!e) return null; let r = e.getBoundingClientRect();
            if (r.top < 0 || r.bottom > innerHeight) { e.scrollIntoView({ block: 'nearest', inline: 'nearest' }); r = e.getBoundingClientRect(); }
            return [r.left + r.width / 2, r.top + r.height / 2]; })()`);
        if (!r) throw new Error(`nothing to tap: ${x}`);
        return r;
    };
    const y = 24;
    const settle = () => sleep(900);
    if (mode === 'chromium') {
        return {
            name: 'Chromium, by touch',
            // A finger's drag, as touch events a frame apart: headless
            // Chromium's synthesized touch gestures don't scroll.
            async swipe(dir) {
                const [from, to] = dir === 'right' ? [60, 360] : [330, 30];
                await b.cdp('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: from, y }] });
                for (let i = 1; i <= 10; i++) {
                    await b.cdp('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: from + ((to - from) * i) / 10, y }] });
                    await sleep(16);
                }
                await b.cdp('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
                await settle();
            },
            async tap(x) {
                const [cx, cy] = await center(x);
                await b.tap(cx, cy);
                await settle();
            },
            async back() {
                const { currentIndex, entries } = await b.cdp('Page.getNavigationHistory');
                await b.cdp('Page.navigateToHistoryEntry', { entryId: entries[currentIndex - 1].id });
                await settle();
            },
            touch: true,
            async press(x) {
                const [cx, cy] = await center(x);
                await b.press(cx, cy);
                await settle();
            },
            async drag([x1, y1], [x2, y2]) {
                await b.cdp('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: x1, y: y1 }] });
                for (let i = 1; i <= 10; i++) {
                    await b.cdp('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x1 + ((x2 - x1) * i) / 10, y: y1 + ((y2 - y1) * i) / 10 }] });
                    await sleep(16);
                }
                await b.cdp('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
                await settle();
            },
            type: (text) => b.type(text),
            enter: () => b.key('Enter'),
            async double([x, y]) {
                await b.tap(x, y);
                await sleep(80);
                await b.tap(x, y);
                await settle();
            },
            // A finger put down and held, then lifted, or its touch
            // cancelled, as the browser cancels one it takes over.
            down: ([x, y]) => b.cdp('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] }),
            up: () => b.cdp('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] }),
            cancel: () => b.cdp('Input.dispatchTouchEvent', { type: 'touchCancel', touchPoints: [] }),
            // Two fingers, about the point at, from gap apart to gap to.
            async pinch([cx, cy], from, to) {
                const at = (gap) => [{ x: cx - gap / 2, y: cy, id: 0 }, { x: cx + gap / 2, y: cy, id: 1 }];
                await b.cdp('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: at(from) });
                for (let i = 1; i <= 10; i++) {
                    await b.cdp('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: at(from + ((to - from) * i) / 10) });
                    await sleep(16);
                }
                await b.cdp('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
                await settle();
            },
        };
    }
    const traverse = async () => {
        await b.bidi('browsingContext.traverseHistory', { context: b.context, delta: -1 });
        await settle();
    };
    // A touchpad's sideways swipe, as the wheel's sideways scroll.
    const wheel = async (dir) => {
        await b.bidi('input.performActions', { context: b.context, actions: [{ type: 'wheel', id: 'wheel',
            actions: [{ type: 'scroll', x: 195, y, deltaX: dir === 'right' ? -300 : 300, deltaY: 0 }] }] });
        await settle();
    };
    const row = `document.querySelector('[data-pane=chan]').parentElement.scrollLeft`;
    if (mode === 'firefox') {
        let untouched = false;
        return {
            name: 'Firefox, by touch',
            // Firefox's driven touch may not scroll, as it doesn't reach its
            // scrolling as a finger's does: then the row scrolls as from a
            // touchpad, and the walk says so once.
            async swipe(dir) {
                const [from, to] = dir === 'right' ? [60, 360] : [330, 30];
                const before = untouched ? null : await h.ev(row);
                if (!untouched) {
                    await b.bidi('input.performActions', { context: b.context, actions: [{ type: 'pointer', id: 'finger', parameters: { pointerType: 'touch' },
                        actions: [{ type: 'pointerMove', x: from, y }, { type: 'pointerDown', button: 0 }, { type: 'pointerMove', x: to, y, duration: 250 },
                            { type: 'pointerUp', button: 0 }] }] });
                    await settle();
                    if (Math.abs((await h.ev(row)) - before) > 2) return;
                    untouched = true;
                    console.log("     firefox: a driven touch drag doesn't scroll the panes; a sideways scroll stands in for it");
                }
                await wheel(dir);
            },
            async tap(x) {
                const [cx, cy] = await center(x);
                await b.tap(cx, cy);
                await settle();
            },
            back: traverse,
            touch: true,
            async press(x) {
                const [cx, cy] = await center(x);
                await b.press(cx, cy);
                await settle();
            },
            async drag([x1, y1], [x2, y2]) {
                await b.bidi('input.performActions', { context: b.context, actions: [{ type: 'pointer', id: 'finger', parameters: { pointerType: 'touch' },
                    actions: [{ type: 'pointerMove', x: Math.round(x1), y: Math.round(y1) }, { type: 'pointerDown', button: 0 },
                        { type: 'pointerMove', x: Math.round(x2), y: Math.round(y2), duration: 250 }, { type: 'pointerUp', button: 0 }] }] });
                await settle();
            },
            type: (text) => b.type(text),
            enter: () => b.key('Enter'),
            async double([x, y]) {
                await b.tap(x, y);
                await sleep(80);
                await b.tap(x, y);
                await settle();
            },
            // BiDi keeps a finger down between its actions; it has no way
            // to cancel one.
            down: ([x, y]) => b.bidi('input.performActions', { context: b.context, actions: [{ type: 'pointer', id: 'finger', parameters: { pointerType: 'touch' },
                actions: [{ type: 'pointerMove', x: Math.round(x), y: Math.round(y) }, { type: 'pointerDown', button: 0 }] }] }),
            up: () => b.bidi('input.performActions', { context: b.context, actions: [{ type: 'pointer', id: 'finger', parameters: { pointerType: 'touch' },
                actions: [{ type: 'pointerUp', button: 0 }] }] }),
            async pinch([cx, cy], from, to) {
                await b.bidi('input.performActions', { context: b.context, actions: [-1, 1].map((side, i) => ({ type: 'pointer', id: `finger${i}`,
                    parameters: { pointerType: 'touch' }, actions: [{ type: 'pointerMove', x: Math.round(cx + (side * from) / 2), y: Math.round(cy) },
                        { type: 'pointerDown', button: 0 }, { type: 'pointerMove', x: Math.round(cx + (side * to) / 2), y: Math.round(cy), duration: 300 },
                        { type: 'pointerUp', button: 0 }] })) });
                await settle();
            },
        };
    }
    return {
        name: 'Firefox, with a mouse',
        swipe: wheel,
        async tap(x) {
            const [cx, cy] = await center(x);
            await b.click(cx, cy);
            await settle();
        },
        back: traverse,
        touch: false,
        async hover(x) {
            const [cx, cy] = await center(x);
            await b.move(cx, cy);
            await sleep(300);
        },
        async drag([x1, y1], [x2, y2]) {
            await b.drag(x1, y1, x2, y2);
            await settle();
        },
        type: (text) => b.type(text),
        enter: () => b.key('Enter'),
        shiftEnter: () => b.chord(['Shift'], 'Enter'),
        async double([x, y]) {
            await b.click(x, y);
            await sleep(80);
            await b.click(x, y);
            await settle();
        },
        async wheel([x, y], dy) {
            await b.bidi('input.performActions', { context: b.context, actions: [{ type: 'wheel', id: 'wheel',
                actions: [{ type: 'scroll', x: Math.round(x), y: Math.round(y), deltaX: 0, deltaY: dy }] }] });
            await settle();
        },
    };
}

const ctx = { den, general, random, dm, dm2, lounge, photo: `${HOME}/photos/pixel-2.jpg` };
let port = 9490;
for (const mode of modes.length ? modes : ['chromium', 'firefox', 'firefox-mouse']) {
    const out = `${HOME}/shot-phone/${mode}`;
    fs.rmSync(out, { recursive: true, force: true });
    fs.mkdirSync(out, { recursive: true });
    const log = [];
    if (want('member') || want('around') || want('touch') || want('pages') || want('call')) {
        const b = await browser(mode, port++, { media: mode === 'chromium' ? true : 'pulse', screen: mode === 'chromium' });
        try {
            await pair(b, BOB);
            const h = helpers(b, `http://localhost:${BOB.port}`, out, 'bob-');
            if (want('member')) await memberScreens(h, ctx);
            if (want('around')) await gettingAround(h, ctx, driver(mode, b, h), report).catch(fail);
            if (want('touch')) await touching(h, ctx, driver(mode, b, h), report).catch(fail);
            if (want('pages')) await pages(h, ctx, driver(mode, b, h), report).catch(fail);
            if (want('call')) {
                // Carol, the voice probe, hears bob and shares, for the player.
                const jar = fs.readFileSync(`${HOME}/jar-${CAROL.instance}`, 'utf8').split('\n').map((l) => l.split('\t'));
                const cookie = jar.find((f) => f[5]?.startsWith('dens_session_'));
                const bobID = (await b.eval(`fetch('/api/dens/${den}/state').then((r) => r.json()).then((v) => v.me.id)`));
                const probe = spawn(`${HOME}/voice-probe`, ['-test.run', '^TestVoiceProbe$', '-test.count=1'], { stdio: 'ignore', env: { ...process.env,
                    DENS_VOICE_PROBE: `http://127.0.0.1:${CAROL.port}`, DENS_PROBE_COOKIE: `${cookie[5]}=${cookie[6]}`, DENS_PROBE_DEN: den,
                    DENS_PROBE_CHANNEL: lounge, DENS_PROBE_HEAR: bobID, DENS_PROBE_NETWORK: 'udp', DENS_PROBE_THEN: 'share', DENS_PROBE_STAY: '90' } });
                try {
                    await callScreens(h, ctx, driver(mode, b, h), report, { share: true, own: mode === 'chromium' });
                } finally {
                    probe.kill();
                }
            }
            log.push(...h.log);
        } catch (e) {
            fail(e);
        } finally {
            await b.close();
        }
    }
    if (want('owner')) {
        const b = await browser(mode, port++);
        try {
            await pair(b, ALICE);
            const h = helpers(b, `http://localhost:${ALICE.port}`, out, 'alice-');
            await ownerScreens(h, ctx);
            const d = driver(mode, b, h);
            if (d.touch) await staffPress(h, ctx, d, report).catch(fail);
            log.push(...h.log);
        } catch (e) {
            fail(e);
        } finally {
            await b.close();
        }
    }
    if (want('before')) {
        // Dave's install, new for each browser, joins with an invite of its
        // own, as a member of its own.
        lib('stop', 'm2d');
        fs.rmSync(`${HOME}/.local/share/dens-dev/m2d`, { recursive: true, force: true });
        fs.rmSync(`${HOME}/jar-m2d`, { force: true });
        lib('start', 'm2d', '--port', '18489');
        const invite = lib('invite');
        const b = await browser(mode, port++);
        try {
            const origin = 'http://localhost:18489';
            const h = helpers(b, origin, out, 'dave-');
            await h.step('unpaired', async () => {
                await b.go(`${origin}/`);
                await h.snap('unpaired');
            });
            await h.step('set-password', async () => {
                await b.go(`${origin}/#token=${token('m2d')}`);
                await h.wait(`document.querySelector('input[type=password]')`, 'the local password');
                await h.snap('set-password');
                await h.ev(`(() => { const s = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
                    for (const i of document.querySelectorAll('input[type=password]')) { s.call(i, 'local password'); i.dispatchEvent(new Event('input', { bubbles: true })); }
                    return true; })()`);
                await h.click(h.byText('button', 'Set password'));
                await h.wait(h.byText('button', 'Add a den'), 'home', 15000);
            });
            await h.step('join', async () => {
                await h.click(h.byText('button', 'Add a den'));
                await h.ev(`(() => { const t = document.querySelector('[role=dialog] input'); const s = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
                    s.call(t, ${JSON.stringify(invite)}); t.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
                await h.snap('add-pasted');
                await h.click(h.byText('[role=dialog] button', 'Continue'));
                await h.wait(h.byText('[role=dialog] button', 'Join as someone new'), 'the den', 15000);
                await h.snap('add-choose');
                await h.click(h.byText('[role=dialog] button', 'Join as someone new'));
                await h.snap('add-join-form', { scroll: `document.querySelector('[role=dialog] .overflow-y-auto, [role=dialog]')`, max: 3 });
                await h.ev(`(() => { const s = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
                    const set = (i, v) => { s.call(i, v); i.dispatchEvent(new Event('input', { bubbles: true })); };
                    const [user, name] = [...document.querySelectorAll('[role=dialog] input:not([type=password])')];
                    set(user, ${JSON.stringify(`dave_${mode.replace('-', '_')}`)}); set(name, 'Dave');
                    for (const i of document.querySelectorAll('[role=dialog] input[type=password]')) set(i, 'dave den password');
                    return true; })()`);
                await h.click(`[...document.querySelectorAll('[role=dialog] button[type=submit]')].find((b) => b.textContent.trim() === 'Join')`);
                await waitFor(() => h.has(`document.body.textContent.includes('recovery')`), 'the recovery codes', 30000);
                await h.snap('recovery-codes', { scroll: `document.querySelector('main')`, max: 4 });
            });
            log.push(...h.log);
        } catch (e) {
            fail(e);
        } finally {
            await b.close();
        }
    }
    fs.writeFileSync(`${out}/audits.json`, JSON.stringify(log, null, 1));
    audits(mode, log, report, mode !== 'firefox-mouse');
}
finish();
