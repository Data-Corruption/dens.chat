// The check of a sign-in on a new device (M21.1, M22.6): bob opens Add
// device on his first device, at a phone's width, and his laptop reads its
// code, in Chromium by scanning it, its camera shown the picture of it on
// his first device's screen, and in Firefox, which has no camera, by
// typing what it shows; then his den password. The laptop says at once
// that it waits for his other device, whose request opens as a dialog in
// Add device's place. It's mine; his first device types the laptop's
// digits and approves; the laptop scans his first device's digits from
// their code, or types them; and once it connects, his first device's
// dialog closes by itself, and his Devices mark the laptop as new.
//
//   run.sh signin chromium|firefox
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { BOB, HOME, checks, launch, pair, picture, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, DEN_URL: denURL } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const q = (s) => JSON.stringify(s);
const B = BOB;
const L = { name: 'laptop', instance: 'm2e', port: 18490 };
const scans = which === 'chromium';
const CAMERA = `${HOME}/signin-camera.y4m`;
const DIALOG = `document.querySelector('[role=dialog]')`;
const APPROVAL = `document.querySelector('[role=dialog] [data-approval]')`;
const button = (scope, text) => `[...${scope}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${q(text)})`;
// setValue sets a field's value as typing would, for Preact to hear.
const setValue = (field, value) => `(() => { const el = ${field}; const set = Object.getOwnPropertyDescriptor(el.constructor.prototype, 'value').set;
    set.call(el, ${q(value)}); el.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`;
const field = (scope, label) => `[...${scope}.querySelectorAll('fieldset')].find((f) => f.querySelector('legend')?.textContent.includes(${q(label)}))?.querySelector('input')`;
const DIGITS = /^\d{4}( \d{4}){3}$/;

// feed shows the laptop's camera what bob's first device shows at
// selector: a screenshot of it, at its size on his screen, on a dim desk
// a little off the frame's middle, which Chromium plays as its camera
// from a file, frame after frame, as a phone held to a screen sees it.
async function feed(selector) {
    const box = await B.b.eval(`(() => { const r = document.querySelector(${q(selector)}).getBoundingClientRect();
        return [r.left, r.top, r.width, r.height].map((v) => Math.round(v * devicePixelRatio)); })()`);
    const shot = `${HOME}/shot-signin-camera.png`;
    await B.b.shot(shot);
    const pic = picture(shot);
    const W = 640;
    const H = 480;
    const y = Buffer.alloc(W * H, 70);
    // The code at twice its size on bob's screen, as a camera near it.
    const scale = Math.min(2, 400 / box[3]);
    const left = Math.round(W / 2 - (box[2] * scale) / 2 + 30);
    const top = Math.round(H / 2 - (box[3] * scale) / 2 - 10);
    for (let py = 0; py < Math.round(box[3] * scale); py++) {
        for (let px = 0; px < Math.round(box[2] * scale); px++) {
            const [r, g, b] = pic.at(Math.min(pic.w - 1, box[0] + Math.floor(px / scale)), Math.min(pic.h - 1, box[1] + Math.floor(py / scale)));
            const fy = top + py;
            const fx = left + px;
            if (fx >= 0 && fy >= 0 && fx < W && fy < H) y[fy * W + fx] = Math.round(0.299 * r + 0.587 * g + 0.114 * b);
        }
    }
    const chroma = Buffer.alloc((W / 2) * (H / 2) * 2, 128);
    const frame = Buffer.concat([Buffer.from('FRAME\n'), y, chroma]);
    fs.writeFileSync(CAMERA, Buffer.concat([Buffer.from(`YUV4MPEG2 W${W} H${H} F10:1 Ip A1:1 C420jpeg\n`), frame, frame]));
}

try {
    // Chromium reads its camera's file as it starts, so it starts with
    // one: a gray frame, until bob shows a code.
    fs.writeFileSync(CAMERA, Buffer.concat([Buffer.from('YUV4MPEG2 W640 H480 F10:1 Ip A1:1 C420jpeg\nFRAME\n'), Buffer.alloc(640 * 480, 70), Buffer.alloc(640 * 240, 128)]));
    B.b = await launch(which, which === 'chromium' ? 9611 : 9612, { size: [390, 844] });
    L.b = await launch(which, which === 'chromium' ? 9613 : 9614, { hover: true, camera: scans ? CAMERA : undefined });
    await pair(B.b, B);
    await pair(L.b, L);

    step('bob opens Add device on his first device');
    await B.b.go(`http://localhost:${B.port}/`);
    await waitFor(() => B.b.eval(`!!${button('document', 'Add device')}`), 'Add device on his den');
    await B.b.eval(`${button('document', 'Add device')}.click(), true`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[role=dialog] [data-qr] path')`), "Add device's code");
    const shown = await B.b.eval(`[document.querySelector('[data-code-url]').textContent, document.querySelector('[data-code-username]').textContent]`);
    report(shown[0] === denURL && shown[1] === 'bob', `Add device shows a code, with what it holds beside it: ${shown.join(' as ')}`);
    await B.b.shot(`${HOME}/shot-signin-add-device-${which}.png`);

    step(scans ? 'the laptop scans it' : 'the laptop, with no camera, types what it shows');
    await waitFor(() => L.b.eval(`!!${button('document', 'Add a den')}`), 'Add a den');
    await L.b.eval(`${button('document', 'Add a den')}.click(), true`);
    await waitFor(() => L.b.eval(`!!${DIALOG}?.querySelector('[data-way=address] input')`), 'the dialog');
    if (scans) {
        await feed('[role=dialog] [data-qr]');
        await waitFor(() => L.b.eval(`!!${button(DIALOG, 'Scan a code')}`), 'Scan');
        await L.b.eval(`${button(DIALOG, 'Scan a code')}.click(), true`);
        const read = await waitFor(() => L.b.eval(`!!${field(DIALOG, 'Den password')}`), 'the code read', 20000).catch(() => false);
        const form = read && await L.b.eval(`[${field(DIALOG, 'Username')}.value, document.querySelector('[data-den-address]')?.textContent]`);
        report(read && form[0] === 'bob' && form[1] === denURL, `the laptop's camera reads the code, which signs in at ${form?.[1]} as ${form?.[0]}`);
    } else {
        await sleep(500);
        report(!(await L.b.eval(`!!${button(DIALOG, 'Scan a code')}`)), 'with no camera, Add a den offers no Scan');
        await L.b.eval(setValue(`${DIALOG}.querySelector('[data-way=address] input')`, shown[0]));
        await L.b.eval(`${DIALOG}.querySelector('[data-way=address]').requestSubmit(), true`);
        await waitFor(() => L.b.eval(`!!${field(DIALOG, 'Den password')}`), 'the sign-in form');
        await L.b.eval(setValue(field(DIALOG, 'Username'), shown[1]));
    }
    report(await L.b.eval(`${DIALOG}.querySelector('h2').textContent.trim() === ${q(new URL(denURL).host)}`), "the sign-in titled with the den's host, its address under it");
    await L.b.shot(`${HOME}/shot-signin-form-${which}.png`);
    await L.b.eval(setValue(field(DIALOG, 'Den password'), 'bob password'));
    const asked = Date.now();
    await L.b.eval(`${DIALOG}.querySelector('form').requestSubmit(), true`);
    const waiting = await waitFor(() => L.b.eval(`${DIALOG}?.innerText.includes('Waiting for your other device') && !!${DIALOG}.querySelector('.loading-spinner')`),
        'the waiting step', 5000).catch(() => false);
    report(waiting, `the laptop reads that it waits for his other device, with a spinner, ${Date.now() - asked} ms after asking`);
    await L.b.shot(`${HOME}/shot-signin-waiting-${which}.png`);

    step("bob's first device: the request as a dialog in Add device's place");
    const opened = await waitFor(() => B.b.eval(`!!${APPROVAL} && document.querySelectorAll('[role=dialog]').length === 1`), 'the request').catch(() => false);
    const sheet = opened && await B.b.eval(`(() => { const r = ${DIALOG}.getBoundingClientRect(); return Math.abs(r.bottom - innerHeight) < 2 && r.width === innerWidth; })()`);
    const dialogs = await B.b.eval(`[...document.querySelectorAll('[role=dialog]')].map((d) => d.getAttribute('aria-label')).join(', ') || 'none'`);
    report(opened && sheet, `the request opens as a sheet at the foot of the screen, Add device closed: dialogs ${dialogs}`);
    if (!opened) {
        await B.b.shot(`${HOME}/shot-signin-request-${which}.png`);
        throw new Error(`no request to approve; the page reads: ${await B.b.eval(`document.body.innerText.slice(0, 300)`)}`);
    }
    report(await B.b.eval(`${APPROVAL}.innerText.includes('Voice Den') && !!${button(APPROVAL, "It's mine")} && !!${button(APPROVAL, 'Refuse')}`),
        "it names the den, with It's mine and Refuse");
    await B.b.shot(`${HOME}/shot-signin-request-${which}.png`);

    step('each shows digits');
    await B.b.eval(`${button(APPROVAL, "It's mine")}.click(), true`);
    const theirs = await waitFor(() => L.b.eval(`${DIALOG}?.querySelector('[data-signin-steps] .font-mono')?.textContent || ''`), "the laptop's digits");
    const mine = await waitFor(() => B.b.eval(`${APPROVAL}?.querySelector('[data-digits-shown] .font-mono')?.textContent || ''`), "the first device's digits");
    report(DIGITS.test(theirs) && DIGITS.test(mine) && theirs !== mine, `each device shows digits of its own: ${theirs} and ${mine}`);
    const coded = await waitFor(() => B.b.eval(`!!${APPROVAL}?.querySelector('[data-digits-shown] [data-qr] path')`), "the digits' code").catch(() => false);
    report(coded, "bob's first device shows its digits under their code");
    report(await L.b.eval(`!!${button(DIALOG, 'Scan its code')}`) === scans, scans ? 'the laptop offers to scan them' : 'the laptop offers no scan');
    await B.b.shot(`${HOME}/shot-signin-digits-${which}.png`);

    step("bob approves with the laptop's digits");
    await B.b.eval(setValue(field(APPROVAL, 'Then type the digits the new device shows'), theirs));
    await B.b.eval(`${button(APPROVAL, 'Approve')}.click(), true`);
    const approved = await waitFor(() => B.b.eval(`${APPROVAL}?.innerText.includes('Approved.') && !!${APPROVAL}.querySelector('[data-qr] path')`), 'approved').catch(() => false);
    report(approved, 'his first device says Approved, its digits and their code still shown for the laptop');

    step(scans ? "the laptop scans his first device's digits" : 'the laptop types them');
    if (scans) {
        await feed('[role=dialog] [data-digits-shown] [data-qr]');
        await L.b.eval(`${button(DIALOG, 'Scan its code')}.click(), true`);
    } else {
        await L.b.eval(setValue(field(DIALOG, 'digits your other device shows'), mine));
        await L.b.eval(`${button(DIALOG, 'Check')}.click(), true`);
    }
    const done = await waitFor(() => L.b.eval(`${DIALOG}?.innerText.includes("You're signed in")`), 'signed in', 20000).catch(() => false);
    report(done && await L.b.eval(`${DIALOG}.querySelector('h2').textContent.trim() === 'Voice Den'`), "the laptop is signed in, its DMs readable there, under the den's name");
    await L.b.shot(`${HOME}/shot-signin-done-${which}.png`);

    step("bob's first device's dialog closes as the laptop connects");
    const closed = await waitFor(() => B.b.eval(`!${DIALOG}`), 'the dialog closed', 10000).catch(() => false);
    report(closed, 'the dialog closes by itself');
    // At a phone's width the cog is the list pane's, and the settings a
    // list of pages.
    const cog = `document.querySelector('[data-pane=list] button[aria-label="Settings"]')`;
    const devices = `document.querySelector('[role=dialog][aria-label="Settings"] [data-section=devices]')`;
    await B.b.go(`http://localhost:${B.port}/den/${den}`);
    await waitFor(() => B.b.eval(`!!${cog}`), "the den's cog");
    await B.b.eval(`${cog}.click(), true`);
    await waitFor(() => B.b.eval(`!!${devices}`), "the den's sections");
    await B.b.eval(`(${devices}.closest('button') || ${devices}.querySelector('button') || ${devices}).click(), true`);
    const marked = await waitFor(() => B.b.eval(`[...document.querySelectorAll('li')].some((li) => li.textContent.includes('Linux') && !!li.querySelector('[data-new-device]'))`),
        'the laptop marked').catch(() => false);
    report(marked, 'his Devices mark the laptop as new');
    await B.b.shot(`${HOME}/shot-signin-devices-${which}.png`);

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
