// The M14.2 check of the page's sounds: bob in the browser named, alice,
// the den's owner, in Chromium. Each page's sounds are recorded as it plays
// them, by a preload, with the file each comes from and how loud, since a
// headless browser plays to no speaker. In a call: bob's own joining;
// alice leaving and joining, and her share starting and ending; bob's
// microphone closing and opening by the call's controls, deafening, which
// silences the call's other sounds, the settings' push to talk, and its
// key; then in the settings' Notifications & sounds, a sound heard by its
// ▶, replaced with a file of bob's, a file too long refused, one quieted,
// one off and one reset, each as the call next plays it; and bob leaving.
// Then a DM from alice: its sound once with the browser's popup, which
// shows silently; alone with DMs off; and none muted, or with the DM on
// screen.
//
//   run.sh sounds chromium|firefox
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, SETTINGS, checks, launch, lib, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, DM: dm } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;

// The page's sounds as it plays them, by name, whether from the member's
// own file, how loud in percent, and whether the browser played it; and
// its notifications, with whether each was asked to be silent.
const RECORD = `() => {
    window.__sounds = [];
    window.__notes = [];
    const sound = (src) => {
        const u = new URL(src, location.href);
        if (u.pathname.startsWith('/assets/sounds/')) return { name: u.pathname.slice(15).split('.')[0], own: false };
        if (u.pathname.startsWith('/api/sounds/')) return { name: u.pathname.slice(12), own: true };
        return null;
    };
    const play = HTMLMediaElement.prototype.play;
    HTMLMediaElement.prototype.play = function () {
        const p = play.call(this);
        const s = this.src ? sound(this.src) : null;
        if (s) {
            const rec = { ...s, volume: Math.round(this.volume * 100) };
            window.__sounds.push(rec);
            p.then(() => (rec.played = true), (e) => (rec.error = e.name));
        }
        return p;
    };
    const Real = window.Notification;
    if (typeof Real === 'function') {
        window.Notification = class extends Real {
            constructor(title, opts) {
                super(title, opts);
                window.__notes.push({ title, silent: !!opts?.silent });
            }
        };
    }
}`;

// wav writes a WAV file of a tone, seconds long, for bob's own sound.
function wav(file, seconds) {
    const rate = 8000;
    const n = Math.round(rate * seconds);
    const b = Buffer.alloc(44 + n * 2);
    b.write('RIFF', 0);
    b.writeUInt32LE(36 + n * 2, 4);
    b.write('WAVEfmt ', 8);
    b.writeUInt32LE(16, 16);
    b.writeUInt16LE(1, 20);
    b.writeUInt16LE(1, 22);
    b.writeUInt32LE(rate, 24);
    b.writeUInt32LE(rate * 2, 28);
    b.writeUInt16LE(2, 32);
    b.writeUInt16LE(16, 34);
    b.write('data', 36);
    b.writeUInt32LE(n * 2, 40);
    for (let i = 0; i < n; i++) b.writeInt16LE(Math.round(Math.sin((2 * Math.PI * 660 * i) / rate) * 8000), 44 + i * 2);
    fs.writeFileSync(file, b);
    return file;
}

const button = (label) => `[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(label)} || x.getAttribute('aria-label') === ${JSON.stringify(label)})`;
const click = (s, label, gesture = false) => s.b.eval(`(() => { const b = ${button(label)}; if (!b) return 'missing'; if (b.disabled) return 'disabled'; b.click(); return 'clicked'; })()`, gesture);
const KEY = (type) => `(document.body.dispatchEvent(new KeyboardEvent('${type}', { code: 'KeyV', key: 'v', bubbles: true })), true)`;
const connected = (s) => waitFor(() => s.b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), `${s.name} in the call`, 30000);
const notes = () => B.b.eval('window.__notes');
const settings = () => JSON.parse(lib('api', 'bob', 'GET', '/api/settings')).notifications;

// mark is how many sounds a page has played; since waits for those after
// it to number want's, then a moment more for any extra, and returns them.
const mark = async (s) => s.b.eval('window.__sounds.length');
async function since(s, from, want, ms = 10000) {
    const all = () => s.b.eval(`window.__sounds.slice(${from})`);
    await waitFor(async () => (await all()).length >= want.length, `${s.name}'s sounds ${want.join(', ') || '(none)'}`, ms).catch(() => {});
    await sleep(want.length ? 800 : 2500);
    return all();
}
const names = (list) => list.map((x) => x.name).join(', ');
const played = (list) => list.every((x) => x.played && !x.error);
// expect has a page play want, by name, after what happened since from.
async function expect(s, from, want, what, also = () => true) {
    const got = await since(s, from, want);
    report(names(got) === want.join(', ') && played(got) && also(got), `${what}: ${JSON.stringify(got)}`);
    return got;
}

// alice's leaving and joining the call, as bob hears it.
async function aliceLeaves() {
    await click(A, 'Leave the call');
    await waitFor(async () => !(await A.b.eval(`!!${button('Leave the call')}`)), 'alice out of the call');
}
async function aliceJoins() {
    await A.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    await connected(A);
}
// row is an expression for one of the settings' sounds, by name.
const row = (name) => `document.querySelector('[data-sound=${name}]')`;
const own = () => settings().own || {};
// soundsTab opens bob's settings at Notifications & sounds, once its cards
// show, which wait for the service's status.
async function soundsTab() {
    await openSettings(B.b, 'Notifications & sounds');
    await waitFor(() => B.b.eval(`!!${row('micOff')} && !!document.getElementById('notify-dms')`), 'the sounds card');
}

try {
    A.b = await launch('chromium', 9551, { media: true, screen: true, preload: RECORD });
    B.b = await launch(which, which === 'chromium' ? 9552 : 9553, {
        media: which === 'firefox' ? 'pulse' : true, preload: RECORD, hover: true, prefs: { 'permissions.default.desktop-notification': 1 },
    });
    for (const s of [A, B]) {
        await pair(s.b, s);
        // Push to talk is V, for when bob sends by it.
        const keys = await s.b.eval(`(async () => (await fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ voiceKeys: { ptt: { code: 'KeyV', label: 'V' } } }) })).status)()`);
        if (keys !== 200) throw new Error(`${s.name}'s keys: ${keys}`);
    }
    if (which === 'chromium') await B.b.cdp('Browser.grantPermissions', { permissions: ['notifications'], origin: 'http://localhost:18486' }, null);

    step('alice, then bob, joins the call');
    for (const s of [A, B]) {
        await s.b.go(`http://localhost:${s.port}/den/${den}/${general}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), `${s.name}'s voice channel`);
    }
    await aliceJoins();
    let a = await mark(A);
    let b = await mark(B);
    await B.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    await connected(B);
    await expect(B, b, ['joined'], 'bob joining plays Joined for him, from its default file, at its default volume',
        ([x]) => !x.own && x.volume === 35);
    await expect(A, a, ['joined'], 'and alice hears him join');

    step('alice leaves and joins again, and shares her screen');
    b = await mark(B);
    await aliceLeaves();
    await expect(B, b, ['left'], 'alice leaving plays Left for bob');
    b = await mark(B);
    await aliceJoins();
    await expect(B, b, ['joined'], 'alice joining again plays Joined');
    b = await mark(B);
    if ((await click(A, 'Share your screen', true)) !== 'clicked') throw new Error("alice's share button");
    await expect(B, b, ['shareStarted'], 'her share starting plays Share started');
    b = await mark(B);
    await click(A, 'Stop sharing your screen');
    await expect(B, b, ['shareEnded'], 'and ending, Share ended');

    step("bob's microphone, by the call's controls, deafening, push to talk and its key");
    b = await mark(B);
    await click(B, 'Mute');
    await expect(B, b, ['micOff'], 'muting plays Microphone off');
    b = await mark(B);
    await click(B, 'Unmute');
    await expect(B, b, ['micOn'], 'unmuting, Microphone on');
    b = await mark(B);
    await click(B, 'Deafen');
    await expect(B, b, ['micOff'], 'deafening closes the microphone, which plays Microphone off');
    b = await mark(B);
    await aliceLeaves();
    await aliceJoins();
    await expect(B, b, [], 'deafened, alice leaving and joining play nothing');
    b = await mark(B);
    await click(B, 'Undeafen');
    await expect(B, b, ['micOn'], 'hearing again opens it, Microphone on');
    await openSettings(B.b, 'Voice & video');
    b = await mark(B);
    await B.b.eval(`[...document.querySelectorAll('[role=radiogroup][aria-label="Sending your voice"] label')].find((l) => l.textContent.trim() === 'Push to talk').click(), true`);
    await expect(B, b, ['micOff'], 'push to talk closes it until held');
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await waitFor(async () => !(await B.b.eval(`!!${SETTINGS}`)), 'the settings closed');
    b = await mark(B);
    await B.b.eval(KEY('keydown'));
    await sleep(600);
    await B.b.eval(KEY('keyup'));
    await expect(B, b, ['micOn', 'micOff'], 'holding V plays Microphone on, and letting go Microphone off');

    step("the settings' sounds");
    await soundsTab();
    const card = await B.b.eval(`({
        notifications: !!document.getElementById('notify-dms'),
        rows: [...document.querySelectorAll('[data-sound]')].map((r) => r.dataset.sound + ' ' + r.querySelector('input[type=range]').value),
        groups: [...document.querySelectorAll('[role=dialog] [role=group]')].map((g) => g.getAttribute('aria-label')),
    })`);
    report(card.notifications && card.groups.join() === 'Messages,Calls' && card.rows.join() === 'message 60,joined 35,left 30,shareStarted 50,shareEnded 40,micOn 65,micOff 85',
        `Notifications & sounds holds the notifications and the sounds, grouped, each at its default volume: ${JSON.stringify(card)}`);
    await B.b.shot(`${HOME}/shot-sounds-${which}-settings.png`);
    b = await mark(B);
    await B.b.eval(`${row('shareStarted')}.querySelector('button[aria-label="Play Share started"]').click(), true`);
    await expect(B, b, ['shareStarted'], "Share started's ▶ plays it", ([x]) => x.volume === 50);

    const long = wav(`${HOME}/sound-long.wav`, 12);
    await B.b.attach('[data-sound=left] input[type=file]', [long]);
    const refused = await waitFor(() => B.b.eval(`document.querySelector('[role=dialog] [role=alert]')?.textContent || ''`), 'the refusal', 10000).catch(() => '');
    report(/Left: That sound lasts 12 seconds, and one plays for at most 10\./.test(refused) && !own().left, `a twelve-second file is refused, before it goes: ${refused}`);

    await B.b.attach('[data-sound=joined] input[type=file]', [wav(`${HOME}/sound-own.wav`, 0.5)]);
    const replaced = await waitFor(() => B.b.eval(`!!${row('joined')}.querySelector('button[aria-label^="Reset Joined"]')`), 'Joined replaced', 10000).catch(() => false);
    report(replaced && own().joined > 0, `bob replaces Joined with a file of his, which the service keeps: ${JSON.stringify(own())}`);
    await B.b.eval(`(() => { const r = ${row('left')}.querySelector('input[type=range]'); r.value = '10';
        r.dispatchEvent(new Event('input', { bubbles: true })); r.dispatchEvent(new Event('change', { bubbles: true })); return true; })()`);
    const quiet = await waitFor(() => settings().sounds?.left?.volume === 10, 'Left at 10%', 10000).then(() => true, () => false);
    report(quiet, 'and turns Left down to 10%, which the service keeps');
    await B.b.shot(`${HOME}/shot-sounds-${which}-own.png`);
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    b = await mark(B);
    await aliceLeaves();
    await aliceJoins();
    await expect(B, b, ['left', 'joined'], 'alice leaving plays Left at 10%, and joining plays Joined from his own file',
        ([l, j]) => l.volume === 10 && !l.own && j.own && j.volume === 35);

    await soundsTab();
    await B.b.eval(`document.getElementById('sound-joined').click(), true`);
    await waitFor(() => settings().sounds?.joined?.off, 'Joined off');
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    b = await mark(B);
    await aliceLeaves();
    await aliceJoins();
    await expect(B, b, ['left'], 'with Joined off, alice joining plays nothing');

    await soundsTab();
    await B.b.eval(`${row('joined')}.querySelector('button[aria-label^="Reset Joined"]').click(), true`);
    await waitFor(() => !own().joined, 'Joined reset');
    await B.b.eval(`document.getElementById('sound-joined').click(), true`);
    await waitFor(() => !settings().sounds?.joined, 'Joined on again');
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    b = await mark(B);
    await aliceLeaves();
    await aliceJoins();
    await expect(B, b, ['left', 'joined'], 'reset and on again, Joined plays its default file', ([, j]) => !j.own);

    b = await mark(B);
    await click(B, 'Leave the call');
    await expect(B, b, ['left'], 'bob leaving plays Left for him, at his 10%', ([x]) => x.volume === 10);

    step("a DM's sound");
    await soundsTab();
    await B.b.eval(`document.getElementById('notify-dms').click(), true`, true);
    await waitFor(() => settings().dms, 'DMs on');
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await sleep(500);
    b = await mark(B);
    lib('send', 'alice', dm, 'a DM for bob');
    await expect(B, b, ['message'], "a DM plays Message once, with the browser's popup");
    const popups = await notes();
    report(popups.length === 1 && popups[0].silent, `the popup shows silently, so the page's sound is the one: ${JSON.stringify(popups)}`);

    await soundsTab();
    await B.b.eval(`document.getElementById('notify-dms').click(), true`);
    await waitFor(() => !settings().dms, 'DMs off');
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await sleep(500);
    b = await mark(B);
    lib('send', 'alice', dm, 'another, with DMs off');
    await expect(B, b, ['message'], 'with DMs off, a DM plays Message alone');
    report((await notes()).length === 1, 'and shows no popup');

    lib('api', 'bob', 'POST', `/api/dens/${den}/notifications`, JSON.stringify({ channel: dm, muted: true }));
    b = await mark(B);
    lib('send', 'alice', dm, 'while muted');
    await expect(B, b, [], 'the DM muted, it plays nothing');
    lib('api', 'bob', 'POST', `/api/dens/${den}/notifications`, JSON.stringify({ channel: dm, muted: false }));

    await B.b.go(`http://localhost:18486/den/${den}/${dm}`);
    await waitFor(() => B.b.eval(`!!document.querySelector('textarea')`), 'the DM open');
    await sleep(1000);
    b = await mark(B);
    lib('send', 'alice', dm, 'while bob reads it');
    await expect(B, b, [], 'with the DM on screen, it plays nothing');
    await B.b.shot(`${HOME}/shot-sounds-${which}-dm.png`);
} catch (e) {
    c.fail(e);
    await B.b?.shot(`${HOME}/shot-sounds-${which}-failed.png`).catch(() => {});
} finally {
    await A.b?.close();
    await B.b?.close();
}
c.finish();
