// The review's pictures: every screen the phone check reaches and
// those reviewScreens and callReview add, in Chromium at a desktop's 1280
// by 800 with a mouse and at a phone's 390 by 844 by touch, each on Dark
// and on Light, with each screen's audit beside it. Bob's screens, a call
// with carol's probe sharing, alice's as the owner, dave's before a den,
// and bob's laptop signing in while his first device approves. It checks
// nothing but that each screen was reached; the pictures land in
// ~/shot-review/<pass>, for the review to read.
//
//   run.sh review [PASS...] [-- PART...]
//
// PASS is desk-dark, desk-light, phone-dark or phone-light; PART is member,
// more, call, owner, before or signin; all unless given.
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, CAROL, HOME, checks, launch, lib, pair, token } from './browsers.mjs';
import { callReview, helpers, memberScreens, ownerScreens, reviewScreens, waitFor } from './screens.mjs';

const args = process.argv.slice(2);
const cut = args.indexOf('--');
const passes = (cut < 0 ? args : args.slice(0, cut)).filter(Boolean);
const parts = cut < 0 ? [] : args.slice(cut + 1);
const want = (p) => parts.length === 0 || parts.includes(p);
const { DEN: den, DEN_URL: denURL, GENERAL: general, RANDOM_ID: random, LOUNGE: lounge, DM: dm, GROUP: group } = process.env;
const { report, fail, finish } = checks();
const LAPTOP = { name: 'laptop', instance: 'm2e', port: 18490 };

async function browser(pass, port, opts = {}) {
    const phone = pass.startsWith('phone');
    const theme = pass.endsWith('light') ? 'light' : 'dark';
    const size = phone ? [390, 844] : [1280, 800];
    const b = await launch('chromium', port, { size, hover: !phone, preload: `() => { try { localStorage.setItem('DENS_THEME', '${theme}'); } catch {} }`, ...opts });
    await b.cdp('Emulation.setDeviceMetricsOverride', { width: size[0], height: size[1], deviceScaleFactor: phone ? 2 : 1, mobile: phone });
    if (phone) await b.touch(true);
    return b;
}

// fresh starts an install anew, paired with nothing.
function fresh(instance, port) {
    lib('stop', instance);
    fs.rmSync(`${HOME}/.local/share/dens-dev/${instance}`, { recursive: true, force: true });
    fs.rmSync(`${HOME}/jar-${instance}`, { force: true });
    lib('start', instance, '--port', String(port));
}

const setAll = (sel, value) => `(() => { const s = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
    for (const i of document.querySelectorAll(${JSON.stringify(sel)})) { s.call(i, ${JSON.stringify(value)}); i.dispatchEvent(new Event('input', { bubbles: true })); }
    return true; })()`;

let port = 9520;
for (const pass of passes.length ? passes : ['desk-dark', 'desk-light', 'phone-dark', 'phone-light']) {
    const phone = pass.startsWith('phone');
    const out = `${HOME}/shot-review/${pass}`;
    if (!parts.length) fs.rmSync(out, { recursive: true, force: true });
    fs.mkdirSync(out, { recursive: true });
    const log = [];
    const ctx = { den, general, random, dm, group, lounge, photo: `${HOME}/photos/pixel-2.jpg`, eventCard: true };
    const pointer = (b, h) => {
        if (phone) return {};
        const center = (x) => h.ev(`(() => { const e = ${x}; e.scrollIntoView({ block: 'nearest' }); const r = e.getBoundingClientRect(); return [r.left + Math.min(r.width / 2, 60), r.top + r.height / 2]; })()`);
        return {
            async right(x) {
                const [cx, cy] = await center(x);
                await b.click(cx, cy, 'right');
                await sleep(500);
            },
            async hover(x) {
                const [cx, cy] = await center(x);
                await b.move(cx, cy);
                await sleep(500);
            },
        };
    };
    if (want('member') || want('more') || want('call')) {
        const b = await browser(pass, port++, { media: true, screen: true });
        try {
            await pair(b, BOB);
            const h = helpers(b, `http://localhost:${BOB.port}`, out, 'bob-');
            if (want('member')) await memberScreens(h, ctx);
            if (want('more')) await reviewScreens(h, { ...ctx, ...pointer(b, h) });
            if (want('call')) {
                const jar = fs.readFileSync(`${HOME}/jar-${CAROL.instance}`, 'utf8').split('\n').map((l) => l.split('\t'));
                const cookie = jar.find((f) => f[5]?.startsWith('dens_session_'));
                const bobID = await b.eval(`fetch('/api/dens/${den}/state').then((r) => r.json()).then((v) => v.me.id)`);
                const probe = spawn(`${HOME}/voice-probe`, ['-test.run', '^TestVoiceProbe$', '-test.count=1'], { stdio: 'ignore', env: { ...process.env,
                    DENS_VOICE_PROBE: `http://127.0.0.1:${CAROL.port}`, DENS_PROBE_COOKIE: `${cookie[5]}=${cookie[6]}`, DENS_PROBE_DEN: den,
                    DENS_PROBE_CHANNEL: lounge, DENS_PROBE_HEAR: bobID, DENS_PROBE_NETWORK: 'udp', DENS_PROBE_THEN: 'share', DENS_PROBE_STAY: '90' } });
                try {
                    await callReview(h, { ...ctx, share: true, own: true });
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
        const b = await browser(pass, port++);
        try {
            await pair(b, ALICE);
            const h = helpers(b, `http://localhost:${ALICE.port}`, out, 'alice-');
            await ownerScreens(h, ctx);
            const p = pointer(b, h);
            if (p.right) {
                await h.step('owner-right-click', async () => {
                    await h.go(`/den/${den}/${general}`);
                    await sleep(1200);
                    await p.right(`[...document.querySelectorAll('nav[aria-label=Channels] button')].find((x) => x.textContent.trim() === 'random')`);
                    await h.snap('owner-right-click-channel');
                    await h.escape();
                    if (!(await h.has(`document.querySelector('nav[aria-label=Members]')`))) await h.click(h.label('Members'));
                    await p.right(`[...document.querySelectorAll('nav[aria-label=Members] button')].find((x) => x.textContent.includes('Bob'))`);
                    await h.snap('owner-right-click-member');
                    await h.escape();
                    await p.right(`[...document.querySelectorAll('[data-scroller] [data-id]')].find((r) => r.textContent.includes("Bob's last word")).querySelector('p')`);
                    await h.snap('owner-right-click-message');
                    await h.escape();
                });
            }
            await h.step('owner-channel', async () => {
                await h.go(`/den/${den}/${general}`);
                await sleep(1500);
                await h.snap('owner-channel');
            });
            log.push(...h.log);
        } catch (e) {
            fail(e);
        } finally {
            await b.close();
        }
    }
    if (want('before')) {
        fresh('m2d', 18489);
        const invite = lib('invite');
        const b = await browser(pass, port++);
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
                await h.ev(setAll('input[type=password]', 'local password'));
                await h.click(h.byText('button', 'Set password'));
                await h.wait(h.byText('button', 'Add a den'), 'home', 15000);
                await h.snap('home-empty');
            });
            await h.step('join', async () => {
                await h.click(h.byText('button', 'Add a den'));
                await h.snap('add-empty');
                await h.ev(setAll('[role=dialog] [data-way=invite] input', invite));
                await h.snap('add-pasted');
                await h.click(h.byText('[role=dialog] [data-way=invite] button', 'Continue'));
                await h.wait(h.byText('[role=dialog] button', 'Join as someone new'), 'the den', 15000);
                await h.snap('add-choose');
                await h.click(h.byText('[role=dialog] button', 'Join as someone new'));
                await h.snap('add-join-form', { scroll: `document.querySelector('[role=dialog] .overflow-y-auto, [role=dialog]')`, max: 3 });
                await h.ev(`(() => { const s = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
                    const set = (i, v) => { s.call(i, v); i.dispatchEvent(new Event('input', { bubbles: true })); };
                    const [user, name] = [...document.querySelectorAll('[role=dialog] input:not([type=password])')];
                    set(user, ${JSON.stringify(`dave_${pass.replace('-', '_')}`)}); set(name, 'Dave');
                    for (const i of document.querySelectorAll('[role=dialog] input[type=password]')) set(i, 'dave den password');
                    return true; })()`);
                await h.snap('add-join-filled');
                await h.click(`[...document.querySelectorAll('[role=dialog] button[type=submit]')].find((b) => b.textContent.trim() === 'Join')`);
                await waitFor(() => h.has(`document.body.textContent.includes('recovery')`), 'the recovery codes', 30000);
                await h.snap('recovery-codes', { scroll: `document.querySelector('main')`, max: 4 });
            });
            // Whatever the recovery codes lead to, and the den as a new
            // member first sees it.
            await h.step('first-den', async () => {
                const next = `[...document.querySelectorAll('button')].find((b) => /saved|Continue|Done|OK/.test(b.textContent))`;
                if (await h.has(next)) await h.click(next, 'past the recovery codes');
                await sleep(1500);
                await h.snap('after-codes');
                await h.go('/');
                await h.snap('home-one-den');
                await h.go(`/den/${den}`);
                await sleep(2000);
                await h.snap('first-den');
            });
            log.push(...h.log);
        } catch (e) {
            fail(e);
        } finally {
            await b.close();
        }
    }
    if (want('signin')) {
        // Bob's laptop, new, signs in; his first device approves.
        fresh('m2e', LAPTOP.port);
        const B = await browser(pass, port++);
        const L = await browser(pass, port++);
        const DIALOG = `document.querySelector('[role=dialog]')`;
        const REQUEST = `document.querySelector('[role=dialog] [data-approval]')`;
        const button = (scope, text) => `[...${scope}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(text)})`;
        const field = (scope, label) => `[...${scope}.querySelectorAll('fieldset')].find((f) => f.querySelector('legend')?.textContent.includes(${JSON.stringify(label)}))?.querySelector('input')`;
        const set = (f, v) => `(() => { const el = ${f}; Object.getOwnPropertyDescriptor(el.constructor.prototype, 'value').set.call(el, ${JSON.stringify(v)});
            el.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`;
        try {
            await pair(B, BOB);
            const hb = helpers(B, `http://localhost:${BOB.port}`, out, 'signin-bob-');
            const hl = helpers(L, `http://localhost:${LAPTOP.port}`, out, 'signin-laptop-');
            await hb.go('/');
            await hb.wait(button('document', 'Add device'), 'Add device');
            await hb.click(button('document', 'Add device'), 'Add device');
            await hb.wait(`document.querySelector('[role=dialog] [data-qr]')`, "Add device's code");
            await hb.snap('add-device');
            await hl.step('signin', async () => {
                await L.go(`http://localhost:${LAPTOP.port}/#token=${token('m2e')}`);
                await hl.wait(`document.querySelector('input[type=password]')`, 'the local password');
                await hl.ev(setAll('input[type=password]', 'local password'));
                await hl.click(hl.byText('button', 'Set password'));
                await hl.click(button('document', 'Add a den'), 'Add a den');
                await hl.wait(`${DIALOG}?.querySelector('[data-way=address] input')`, 'the dialog');
                await hl.ev(set(`${DIALOG}.querySelector('[data-way=address] input')`, denURL));
                await hl.snap('add-address');
                await hl.ev(`${DIALOG}.querySelector('[data-way=address]').requestSubmit(), true`);
                await hl.wait(field(DIALOG, 'Den password'), 'the sign-in form');
                await hl.snap('form');
                await hl.ev(set(field(DIALOG, 'Username'), 'bob'));
                await hl.ev(set(field(DIALOG, 'Den password'), 'bob password'));
                await hl.ev(`${DIALOG}.querySelector('form').requestSubmit(), true`);
                await hl.wait(`${DIALOG}?.innerText.includes('Waiting for your other device')`, 'the waiting step');
                await hl.snap('waiting');
                await hb.wait(REQUEST, 'the request');
                await hb.snap('request');
                await hb.click(button(`(${REQUEST})`, "It's mine"), "It's mine");
                const theirs = await waitFor(() => hl.ev(`${DIALOG}?.querySelector('[data-signin-steps] .font-mono')?.textContent || ''`), "the laptop's digits");
                const mine = await waitFor(() => hb.ev(`(${REQUEST})?.querySelector('[data-digits-shown] .font-mono')?.textContent || ''`), "the first device's digits");
                await hl.snap('digits');
                await hb.snap('digits');
                await hb.ev(set(field(`(${REQUEST})`, 'Then type the digits the new device shows'), theirs));
                await hb.snap('digits-typed');
                await hb.click(button(`(${REQUEST})`, 'Approve'), 'Approve');
                await hb.wait(`(${REQUEST})?.innerText.includes('Approved.')`, 'approved');
                await hb.snap('approved');
                await hl.ev(set(field(DIALOG, 'digits your other device shows'), mine));
                await hl.click(button(DIALOG, 'Check'), 'Check');
                await hl.wait(`${DIALOG}?.innerText.includes("You're signed in")`, 'signed in', 15000);
                await hl.snap('done');
            });
            log.push(...hb.log, ...hl.log);
        } catch (e) {
            fail(e);
        } finally {
            await B.close();
            await L.close();
        }
    }
    fs.writeFileSync(`${out}/audits-${parts.join('-') || 'all'}.json`, JSON.stringify(log, null, 1));
    for (const shot of log) if (shot.failed) report(false, `${pass}: ${shot.name} wasn't reached: ${shot.failed}`);
    report(true, `${pass}: ${log.filter((s) => !s.failed).length} screens shot`);
}
finish();
