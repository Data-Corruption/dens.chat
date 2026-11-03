// The M15.1 check of animated pictures: alice, who owns the den, in
// Chromium, and bob in the browser named, whose system asks for less
// motion. Bob's Animations starts at On hover and alice's at Always. Each
// crops an animated GIF and an animated WebP, made here, into a picture and
// a banner, bob the GIF as his picture and alice the WebP; the crop dialog
// says each moves, and the den keeps animated GIFs of the parts framed.
// With both in Lounge's call, alice's page, set to On hover, shows bob's
// picture still in his message's row, his member's, his DM's and his
// call's, plays it in each while the pointer is over it alone, and in his
// profile while it's open, and a finger's tap plays nothing; bob's page
// does the same with alice's, and once set to Always plays every row's.
//
//   run.sh pictures chromium|firefox
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, SETTINGS, checks, launch, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, ALICE_MSG: aliceMsg, BOB_MSG: bobMsg } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;
const q = (s) => JSON.stringify(s);

// lzw writes indexes as GIF's LZW in codes of nine bits, cleared often
// enough that its table never needs a tenth.
function lzw(indexes) {
    const out = [];
    let cur = 0;
    let n = 0;
    const put = (code) => {
        for (let i = 0; i < 9; i++) {
            cur |= ((code >> i) & 1) << n;
            if (++n === 8) {
                out.push(cur);
                cur = n = 0;
            }
        }
    };
    put(256);
    indexes.forEach((v, i) => {
        if (i > 0 && i % 200 === 0) put(256);
        put(v);
    });
    put(257);
    if (n) out.push(cur);
    return out;
}

// gif writes an animated GIF of 320 × 320: a square crossing a blue ground
// over eight frames, a tenth of a second each, looping forever.
function gif(file) {
    const w = 320;
    const h = 320;
    const b = [...Buffer.from('GIF89a'), w & 0xff, w >> 8, h & 0xff, h >> 8, 0xf7, 0, 0];
    const colors = [[20, 40, 120], [250, 200, 40], [200, 60, 60]];
    for (let i = 0; i < 256; i++) b.push(...(colors[i] || [0, 0, 0]));
    b.push(0x21, 0xff, 11, ...Buffer.from('NETSCAPE2.0'), 3, 1, 0, 0, 0);
    for (let f = 0; f < 8; f++) {
        const pix = [];
        for (let y = 0; y < h; y++) {
            for (let x = 0; x < w; x++) pix.push(x >= 20 + 30 * f && x < 100 + 30 * f && y >= 120 && y < 200 ? 1 : y < 10 * (f + 1) ? 2 : 0);
        }
        b.push(0x21, 0xf9, 4, 1 << 2, 10, 0, 0, 0, 0x2c, 0, 0, 0, 0, w & 0xff, w >> 8, h & 0xff, h >> 8, 0, 8);
        const data = lzw(pix);
        for (let i = 0; i < data.length; i += 255) {
            const part = data.slice(i, i + 255);
            b.push(part.length, ...part);
        }
        b.push(0);
    }
    b.push(0x3b);
    fs.writeFileSync(file, Buffer.from(b));
    return file;
}

// vp8l encodes a frame losslessly, as plainly as VP8L allows: each
// channel's value in eight bits of its own.
function vp8l(w, h, px) {
    const bytes = [];
    let cur = 0;
    let n = 0;
    const put = (v, bits) => {
        for (let i = 0; i < bits; i++) {
            cur |= ((v >> i) & 1) << n;
            if (++n === 8) {
                bytes.push(cur);
                cur = n = 0;
            }
        }
    };
    const reversed = (v) => {
        let r = 0;
        for (let i = 0; i < 8; i++) r |= ((v >> i) & 1) << (7 - i);
        return r;
    };
    put(0x2f, 8);
    put(w - 1, 14);
    put(h - 1, 14);
    put(1, 1);
    put(0, 3);
    put(0, 3); // no transform, cache or meta codes
    for (const size of [280, 256, 256, 256]) {
        put(0, 1);
        put(8, 4);
        for (const sym of [17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8]) put(sym === 0 || sym === 8 ? 1 : 0, 3);
        put(0, 1);
        for (let s = 0; s < size; s++) put(s < 256 ? 1 : 0, 1);
    }
    put(0b0001, 4); // distances: a simple code of one symbol
    for (let y = 0; y < h; y++) {
        for (let x = 0; x < w; x++) {
            const [r, g, bl, a] = px(x, y);
            for (const v of [g, r, bl, a]) put(reversed(v), 8);
        }
    }
    if (n) bytes.push(cur);
    return Buffer.from(bytes);
}

function chunk(id, data) {
    const b = Buffer.alloc(8 + data.length + (data.length & 1));
    b.write(id, 0, 'ascii');
    b.writeUInt32LE(data.length, 4);
    data.copy(b, 8);
    return b;
}

// webp writes an animated WebP of 480 × 240: a white square crossing a
// gradient over six frames of 120 ms, looping forever.
function webp(file) {
    const w = 480;
    const h = 240;
    const vp8x = Buffer.alloc(10);
    vp8x[0] = 0x12; // animation and alpha
    vp8x.writeUIntLE(w - 1, 4, 3);
    vp8x.writeUIntLE(h - 1, 7, 3);
    const parts = [Buffer.from('WEBP'), chunk('VP8X', vp8x), chunk('ANIM', Buffer.alloc(6))];
    for (let f = 0; f < 6; f++) {
        const head = Buffer.alloc(16);
        head.writeUIntLE(w - 1, 6, 3);
        head.writeUIntLE(h - 1, 9, 3);
        head.writeUIntLE(120, 12, 3);
        head[15] = 2; // in place, not blended
        const px = (x, y) => (x >= 40 + 70 * f && x < 120 + 70 * f && y >= 80 && y < 160 ? [255, 255, 255, 255] : [x >> 1, y, 128, 255]);
        parts.push(chunk('ANMF', Buffer.concat([head, chunk('VP8L', vp8l(w, h, px))])));
    }
    const body = Buffer.concat(parts);
    const riff = Buffer.alloc(8);
    riff.write('RIFF', 0);
    riff.writeUInt32LE(body.length, 4);
    fs.writeFileSync(file, Buffer.concat([riff, body]));
    return file;
}

const state = async (s) => JSON.parse(await s.b.eval(`(async () => JSON.stringify(await (await fetch('/api/dens/${den}/state')).json()))()`));
const meIn = async (s) => (await state(s)).me;
const dialog = (title) => `document.querySelector('[role=dialog][aria-label=${q(title)}]')`;
const escape = (s) => s.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
const close = async (s, title) => {
    await s.b.eval(`${dialog(title)}.querySelector('button[aria-label=Close]').click(), true`);
    await waitFor(async () => !(await s.b.eval(`!!${dialog(title)}`)), `${s.name}'s ${title} closed`);
};

// crop picks a file as a profile's picture or banner, has the crop dialog
// save it as it frames it, and returns what the dialog said of it and the
// picture the den then keeps.
async function crop(s, shape, file) {
    if (!(await s.b.eval(`!!${dialog('Your profile in this den')}`))) {
        await s.b.eval(`document.querySelector('button[aria-label="Edit your profile"]').click(), true`);
        await waitFor(() => s.b.eval(`!!${dialog('Your profile in this den')}`), `${s.name}'s profile`);
    }
    const title = shape === 'avatar' ? 'Your picture' : 'Your banner';
    await s.b.attach(`input[data-picture=${shape}]`, [file]);
    const save = `[...${dialog(title)}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Save')`;
    await waitFor(() => s.b.eval(`!!${dialog(title)} && !${save}.disabled && ${dialog(title)}.textContent.includes('It moves')`), `${s.name}'s ${shape} framed`);
    const said = await s.b.eval(`${dialog(title)}.textContent.match(/It moves[^.]*\\./)?.[0] || ''`);
    if (s === B) await s.b.shot(`${HOME}/shot-pictures-crop-${which}-${shape}.png`);
    await s.b.eval(`${save}.click(), true`);
    await waitFor(() => s.b.eval(`!${dialog(title)}`), `${s.name}'s ${shape} saved`, 30000);
    const pic = await waitFor(async () => {
        const me = await meIn(s);
        return me[shape]?.animated ? me[shape] : null;
    }, `${s.name}'s animated ${shape}`);
    return { said, pic };
}

async function setAnimations(s, value) {
    await openSettings(s.b, 'Appearance');
    await s.b.eval(`document.querySelector('input[name=animations][value=${value}]').click(), true`);
    await escape(s);
    await waitFor(async () => !(await s.b.eval(`!!${SETTINGS}`)), 'the settings closed');
}

// The rows a page shows another member in, as expressions for its element.
const rows = (name, msg) => ({
    message: `document.querySelector('[data-id="${msg}"]')`,
    member: `[...document.querySelectorAll('nav[aria-label=Members] button')].find((x) => x.textContent.includes(${q(name)}))`,
    call: `[...document.querySelectorAll('ul[aria-label="In the call"] li')].find((x) => x.textContent.includes(${q(name)}))`,
    dm: `[...document.querySelectorAll('nav[aria-label="Direct messages"] li')].find((x) => x.textContent.includes(${q(name)}))`,
});

// shows says what a row's picture shows: the preview, the picture, or the
// preview with the picture over it, and whether each loaded.
const shows = (s, row) => s.b.eval(`(() => {
    const imgs = [...(${row})?.querySelector('span[aria-hidden=true]')?.querySelectorAll('img') || []];
    const what = imgs.map((i) => (i.getAttribute('src').endsWith('/thumb') ? 'still' : 'file')).join('+');
    return JSON.stringify({ what, loaded: imgs.every((i) => i.complete && i.naturalWidth > 0) });
})()`).then(JSON.parse);

async function center(s, row) {
    return JSON.parse(await s.b.eval(`(() => {
        const el = ${row};
        el.scrollIntoView({ block: 'center' });
        const r = el.getBoundingClientRect();
        return JSON.stringify({ x: r.left + r.width / 2, y: r.top + r.height / 2 });
    })()`));
}

const tab = (s, label) => s.b.eval(`[...document.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim() === ${q(label)}).click(), true`);
// back leaves the DMs' tab for the den's, which opens #general again.
async function back(s) {
    await tab(s, 'Den');
    await waitFor(() => s.b.eval(`!!document.querySelector('[data-id="${bobMsg}"]')`), `${s.name}'s #general again`);
}

// playsOnHover checks that a page plays a member's picture in each of their
// rows while the pointer is over that row alone.
async function playsOnHover(s, name, msg) {
    for (const [kind, row] of Object.entries(rows(name, msg))) {
        if (kind === 'dm') await tab(s, 'Direct messages');
        await waitFor(() => s.b.eval(`!!${row}`), `${name}'s ${kind} row on ${s.name}'s page`);
        await s.b.move(5, 5);
        const before = await waitFor(async () => {
            const v = await shows(s, row);
            return v.what === 'still' && v.loaded ? v : null;
        }, `${name}'s ${kind} row still`, 5000).catch(() => null);
        const at = await center(s, row);
        await s.b.move(at.x, at.y);
        const over = await waitFor(async () => {
            const v = await shows(s, row);
            return v.what === 'still+file' && v.loaded ? v : null;
        }, `${name}'s ${kind} row playing`, 5000).catch(() => null);
        await s.b.move(5, 5);
        const after = await waitFor(async () => {
            const v = await shows(s, row);
            return v.what === 'still' ? v : null;
        }, `${name}'s ${kind} row still again`, 5000).catch(() => null);
        report(before && over && after, `${s.name}'s page, On hover: ${name}'s ${kind} row still, playing under the pointer, still again: ` +
            JSON.stringify({ before, over, after }));
        if (kind === 'dm') await back(s);
    }
}

try {
    const animation = gif(`${HOME}/moving.gif`);
    const sticker = webp(`${HOME}/moving.webp`);
    A.b = await launch('chromium', 9571, { media: true, hover: true });
    B.b = await launch(which, which === 'chromium' ? 9572 : 9573, {
        media: which === 'firefox' ? 'pulse' : true, hover: true, prefs: { 'ui.prefersReducedMotion': 1 },
    });
    if (which === 'chromium') await B.b.cdp('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-reduced-motion', value: 'reduce' }] });
    for (const s of [A, B]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}/${general}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('[data-id="${bobMsg}"]')`), `${s.name}'s #general`);
    }

    step("each page's Animations to start");
    for (const [s, want] of [[A, 'always'], [B, 'hover']]) {
        await openSettings(s.b, 'Appearance');
        const checked = await s.b.eval(`document.querySelector('input[name=animations]:checked')?.value || ''`);
        report(checked === want, `${s.name}'s Animations starts at ${want}${s === B ? ', the system asking for less motion' : ''}: ${checked}`);
        await escape(s);
        await waitFor(async () => !(await s.b.eval(`!!${SETTINGS}`)), 'the settings closed');
    }

    step('each crops an animated GIF and an animated WebP');
    for (const [s, picture, banner, want] of [
        [B, animation, sticker, { avatar: [256, 256], banner: [480, 160] }],
        [A, sticker, animation, { avatar: [240, 240], banner: [318, 106] }],
    ]) {
        for (const [shape, file] of [['avatar', picture], ['banner', banner]]) {
            const { said, pic } = await crop(s, shape, file);
            report(said && pic.width === want[shape][0] && pic.height === want[shape][1],
                `${s.name}'s ${shape} from ${file.split('/').pop()}: the dialog said "${said}", and the den keeps ${JSON.stringify(pic)}`);
        }
        await close(s, 'Your profile in this den');
    }
    const bob = (await state(A)).members.find((m) => m.username === 'bob');
    report(bob?.avatar?.animated && bob?.banner?.animated, `alice's page has bob's pictures as animated: ${JSON.stringify(bob)}`);

    step('both join the call');
    for (const s of [A, B]) {
        await waitFor(() => s.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), `${s.name}'s voice channel`);
        await s.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
        await waitFor(() => s.b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), `${s.name} in the call`, 30000);
    }
    for (const s of [A, B]) {
        if (!(await s.b.eval(`!!document.querySelector('nav[aria-label=Members]')`))) {
            await s.b.eval(`document.querySelector('button[aria-label=Members]').click(), true`);
        }
    }

    step("alice's page, On hover, shows bob's rows");
    await setAnimations(A, 'hover');
    await playsOnHover(A, 'Bob', bobMsg);
    // A profile, opened, plays its pictures.
    await A.b.eval(`[...document.querySelector('[data-id="${bobMsg}"]').querySelectorAll('button')].find((x) => x.textContent.trim() === 'Bob').click(), true`);
    await waitFor(() => A.b.eval(`!!${dialog('Profile')}`), "bob's profile");
    const profile = await waitFor(async () => {
        const v = JSON.parse(await A.b.eval(`JSON.stringify([...${dialog('Profile')}.querySelectorAll('img')].map((i) => ({
            still: i.getAttribute('src').endsWith('/thumb'), loaded: i.complete && i.naturalWidth > 0 })))`));
        return v.length === 2 && v.every((i) => !i.still && i.loaded) ? v : null;
    }, "bob's profile playing", 5000).catch(() => null);
    await A.b.shot(`${HOME}/shot-pictures-profile.png`);
    report(profile, `alice's page plays bob's picture and banner in his profile, open: ${JSON.stringify(profile)}`);
    await close(A, 'Profile');
    // A finger's tap is no hover.
    await A.b.touch(true);
    await sleep(500);
    const row = rows('Bob', bobMsg).message;
    const at = await center(A, row);
    await A.b.tap(at.x, at.y);
    await sleep(500);
    const tapped = await shows(A, row);
    report(tapped.what === 'still', `a tap on bob's message plays nothing: ${JSON.stringify(tapped)}`);
    await A.b.touch(false);

    step("bob's page, On hover, shows alice's rows, then Always");
    await playsOnHover(B, 'Alice', aliceMsg);
    await setAnimations(B, 'always');
    for (const [kind, r] of Object.entries(rows('Alice', aliceMsg))) {
        if (kind === 'dm') await tab(B, 'Direct messages');
        await B.b.move(5, 5);
        const v = await waitFor(async () => {
            const got = await shows(B, r);
            return got.what === 'file' && got.loaded ? got : null;
        }, `alice's ${kind} row playing`, 5000).catch(() => null);
        report(v, `bob's page, Always: alice's ${kind} row plays without the pointer: ${JSON.stringify(v)}`);
        if (kind === 'dm') await back(B);
    }
} catch (e) {
    c.fail(e);
}
for (const s of [A, B]) await s.b?.close().catch(() => {});
c.finish();
