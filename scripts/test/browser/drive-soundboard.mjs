// The M14.3 check of the den's soundboard: alice, who owns the den, in
// Chromium, and bob in the browser named. With no sounds, bob has no
// soundboard and alice's opens on adding them, in Manage, which the den's
// name opens too. Alice adds sounds from an MP3 and a WAV, each prepared by
// her page, and a sound too long is refused; she raises the longest, and
// makes bob a moderator, whose page prepares a video's sound and one of two
// minutes. In the call, bob plays sounds from the call's block, up and to
// the right of it, from Home's box, down from it, and at 390 pixels from
// the phone's bar, as a sheet, beside its mute; each time alice's page
// receives the soundboard's track playing, by its stats and its level, and
// lights his ring, while his tiles wait as it plays and for his wait after.
// The popup is framed, and staff's cog beside its search opens the sounds
// in Manage; deafened, bob's tiles wait, and alice's sound lights no ring
// on his page, as it does on hers. Two sounds asked for at once play one,
// and the den refuses the other.
// Bob joins partway through alice's long sound and receives the rest,
// which ends as she leaves; and his soundboard's slider at 0 mutes it.
//
//   run.sh soundboard chromium|firefox
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, MANAGE, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, BOB_ID: bobID } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;

// Each peer connection's tracks, by the stream they come in, and the
// elements the page plays members in.
const RECORD = `() => {
    window.__tracks = [];
    const Orig = window.RTCPeerConnection;
    window.RTCPeerConnection = class extends Orig {
        constructor(...args) {
            super(...args);
            this.addEventListener('track', (e) => window.__tracks.push({ pc: this, stream: e.streams[0]?.id, track: e.track }));
        }
    };
    const Au = window.Audio;
    window.__audios = [];
    window.Audio = function (...args) { const el = new Au(...args); window.__audios.push(el); return el; };
}`;

// BOARD is what a page receives on the call's soundboard: the packets, the
// energy its level adds up to, and how loud its element plays it.
const BOARD = `(async () => {
    const t = window.__tracks.filter((x) => x.stream === 'soundboard' && x.pc.connectionState !== 'closed').at(-1);
    if (!t) return JSON.stringify({ track: false });
    const stats = [...(await t.pc.getStats(t.track)).values()].find((x) => x.type === 'inbound-rtp') || {};
    const el = window.__audios.findLast((a) => a.srcObject?.id === 'soundboard');
    return JSON.stringify({ track: true, packets: stats.packetsReceived || 0, energy: stats.totalAudioEnergy || 0, level: stats.audioLevel ?? null,
        volume: el ? (el.muted ? 0 : el.volume) : null, played: el ? !el.paused : false });
})()`;
const board = async (s) => JSON.parse(await s.b.eval(BOARD));

// wav writes a WAV file of tones summed, seconds long, at 8 kHz: in one
// channel, or in channel into of 5.1's six (FL, FR, FC, LFE, BL, BR, as
// FFmpeg writes them), the rest silent.
function wav(file, seconds, tones = [440], into = -1) {
    const rate = 8000;
    const channels = into < 0 ? 1 : 6;
    const n = Math.round(rate * seconds);
    const fmt = channels === 1 ? 16 : 40;
    const head = 28 + fmt;
    const b = Buffer.alloc(head + n * 2 * channels);
    b.write('RIFF', 0);
    b.writeUInt32LE(b.length - 8, 4);
    b.write('WAVEfmt ', 8);
    b.writeUInt32LE(fmt, 16);
    b.writeUInt16LE(channels === 1 ? 1 : 0xfffe, 20);
    b.writeUInt16LE(channels, 22);
    b.writeUInt32LE(rate, 24);
    b.writeUInt32LE(rate * 2 * channels, 28);
    b.writeUInt16LE(2 * channels, 32);
    b.writeUInt16LE(16, 34);
    if (channels > 1) {
        // WAVE_FORMAT_EXTENSIBLE: 16 bits, 5.1's speakers, as PCM.
        b.writeUInt16LE(22, 36);
        b.writeUInt16LE(16, 38);
        b.writeUInt32LE(0x3f, 40);
        Buffer.from('0100000000001000800000aa00389b71', 'hex').copy(b, 44);
    }
    b.write('data', 20 + fmt);
    b.writeUInt32LE(n * 2 * channels, 24 + fmt);
    for (let i = 0; i < n; i++) {
        const v = tones.reduce((sum, hz) => sum + Math.sin((2 * Math.PI * hz * i) / rate), 0) / tones.length;
        b.writeInt16LE(Math.round(v * 8000), head + (i * channels + Math.max(into, 0)) * 2);
    }
    fs.writeFileSync(file, b);
    return file;
}

const q = (s) => JSON.stringify(s);
const button = (label) => `[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === ${q(label)} || x.getAttribute('aria-label') === ${q(label)})`;
const click = (s, expr) => s.b.eval(`(() => { const b = ${expr}; if (!b) return 'missing'; if (b.disabled) return 'disabled'; b.click(); return 'clicked'; })()`, true);
const connected = (s) => waitFor(() => s.b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), `${s.name} in the call`, 30000);
const joinLounge = async (s) => {
    await waitFor(() => s.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), `${s.name}'s voice channel`);
    await s.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    await connected(s);
};
const leave = async (s) => {
    await click(s, button('Leave the call'));
    await waitFor(async () => !(await s.b.eval(`!!${button('Leave the call')}`)), `${s.name} out of the call`);
};
// setValue sets a field as typing it does.
const setValue = (s, sel, value) => s.b.eval(`(() => {
    const el = document.querySelector(${q(sel)});
    const proto = el.tagName === 'INPUT' ? HTMLInputElement.prototype : HTMLTextAreaElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, ${q(String(value))});
    el.dispatchEvent(new Event('input', { bubbles: true }));
    return true;
})()`);
const state = async (s) => JSON.parse(await s.b.eval(`(async () => JSON.stringify(await (await fetch('/api/dens/${den}/state')).json()))()`));
const soundNamed = async (s, name) => (await state(s)).sounds.find((x) => x.name === name);
// ring says whether a page lights the ring of the member named in its call.
const ring = (s, name) => s.b.eval(`[...document.querySelectorAll('[aria-label="In the call"] li')].some((li) => li.textContent.includes(${q(name)}) && !!li.querySelector('.ring-success'))`);
const rect = (s, sel) => s.b.eval(`(() => { const r = document.querySelector(${q(sel)})?.getBoundingClientRect(); return r ? JSON.stringify({ left: Math.round(r.left), right: Math.round(r.right), top: Math.round(r.top), bottom: Math.round(r.bottom) }) : 'null'; })()`).then(JSON.parse);

// openSoundboardSection opens Manage's Soundboard, Manage open.
async function soundboardSection(s) {
    const item = `[...document.querySelectorAll('[role=dialog] nav button')].find((x) => x.textContent.trim() === 'Soundboard')`;
    await waitFor(() => s.b.eval(`!!${item}`), `${s.name}'s Manage's Soundboard`);
    await s.b.eval(`${item}.click(), true`);
    await waitFor(() => s.b.eval(`!!document.querySelector('[data-add-sound], [data-board-count]')`), `${s.name}'s Soundboard section`);
}

// addSound adds a sound from a file in Manage's Soundboard, as staff do,
// named name, or as the page suggests from the file when name is empty,
// and returns what the page said, if it refused it.
async function addSound(s, file, name) {
    const count = await s.b.eval(`document.querySelectorAll('[data-manage-sound]').length`);
    await s.b.attach('[data-add-sound] input[type=file]', [file]);
    await waitFor(() => s.b.eval(`!!document.querySelector('[data-add-sound] input[aria-label="The sound\\'s name"]').value`), `${s.name}'s name for ${file}`, 5000);
    if (name) await setValue(s, `[data-add-sound] input[aria-label="The sound's name"]`, name);
    await click(s, `document.querySelector('[data-add-sound] button[type=submit]')`);
    const done = await waitFor(async () => {
        if ((await s.b.eval(`document.querySelectorAll('[data-manage-sound]').length`)) > count) return 'added';
        const busy = await s.b.eval(`!!document.querySelector('[data-add-sound] .loading')`);
        const error = await s.b.eval(`document.querySelector('[data-add-sound] [role=alert]')?.textContent || ''`);
        return !busy && error ? `refused: ${error}` : '';
    }, `${s.name} adding ${file}`, 90000).catch((e) => `timed out: ${e.message}`);
    return done;
}

// played waits for a page to receive a sound on the soundboard, from the
// count of packets before, and returns what it received.
async function received(s, before, packets = 25) {
    return waitFor(async () => {
        const now = await board(s);
        return now.packets >= before.packets + packets ? now : null;
    }, `${s.name} receiving the soundboard`, 10000).catch(() => null);
}

// tile plays a sound from an open soundboard.
const tile = (s, id) => click(s, `document.querySelector('[data-sound-tile="${id}"]')`);

try {
    A.b = await launch('chromium', 9561, { media: true, preload: RECORD, hover: true });
    B.b = await launch(which, which === 'chromium' ? 9562 : 9563, { media: which === 'firefox' ? 'pulse' : true, preload: RECORD, hover: true });
    for (const s of [A, B]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}`);
    }
    const tone = '/home/tester/sounds/tone.mp3';
    const chord = wav(`${HOME}/sounds/chord.wav`, 1, [440, 554, 659]);
    const twelve = wav(`${HOME}/sounds/twelve.wav`, 12, [330]);
    const long = wav(`${HOME}/sounds/two-minutes.wav`, 120, [262, 392]);
    const middle = wav(`${HOME}/sounds/middle.wav`, 1, [440], 2);
    const video = '/home/tester/videos/late-sound.mp4';

    step('both join the call, muted, so their rings light for their sounds alone; with no sounds, bob has no soundboard, and alice\'s opens on adding them');
    await joinLounge(A);
    await joinLounge(B);
    for (const s of [A, B]) {
        await click(s, `document.querySelector('[data-pane=list] button[aria-label=Mute]') || ${button('Mute')}`);
        await waitFor(() => s.b.eval(`!!${button('Unmute')}`), `${s.name} muted`, 5000);
    }
    report(!(await B.b.eval(`!!document.querySelector('[data-soundboard-button]')`)), 'a member has no soundboard in a den with no sounds');
    const empty = await click(A, `document.querySelector('[data-soundboard-button=block]')`);
    const opened = await waitFor(() => A.b.eval(`!!${MANAGE} && !!document.querySelector('[data-add-sound]')`), 'Manage at the soundboard', 5000).catch(() => false);
    report(empty === 'clicked' && opened === true, `alice's soundboard with no sounds opens Manage on adding them: ${empty}, ${opened}`);
    await click(A, `${MANAGE}.querySelector('button[aria-label=Close]')`);
    await waitFor(async () => !(await A.b.eval(`!!${MANAGE}`)), "Manage closed");

    step("Manage, from the den's name: alice adds an MP3 and a WAV; one too long is refused");
    const name = await A.b.eval(`document.querySelector('[data-den-manage]')?.textContent.trim() || ''`);
    await click(A, `document.querySelector('[data-den-manage]')`);
    const general = await waitFor(() => A.b.eval(`${MANAGE}?.querySelector('h3')?.textContent || false`), "Manage from the den's name", 5000).catch(() => false);
    report(name === 'Voice Den' && general === 'General', `the den's name, ${name}, opens its Manage, at ${general}`);
    await soundboardSection(A);
    report(await addSound(A, tone, 'Tone') === 'added', 'alice adds an MP3, which her page prepares');
    report(await addSound(A, chord, 'Chord') === 'added', 'and a WAV');
    const refused = await addSound(A, twelve, '');
    report(/lasts 12 s.*at most 10 s/.test(refused), `a sound longer than the den's longest is refused: ${refused}`);
    const toneSound = await soundNamed(A, 'Tone');
    const chordSound = await soundNamed(A, 'Chord');
    report(Math.abs(toneSound?.duration_ms - 1500) <= 60 && Math.abs(chordSound?.duration_ms - 1000) <= 60,
        `the sounds last what their files did: ${toneSound?.duration_ms} ms and ${chordSound?.duration_ms} ms`);

    step('alice raises the longest to 130 seconds, and makes bob a moderator, who adds a video\'s sound and two minutes');
    const limits = `[...document.querySelectorAll('[role=dialog] nav button')].find((x) => x.textContent.trim() === 'Limits')`;
    await click(A, limits);
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-limits-soundboard]')`), "the Limits' soundboard");
    await setValue(A, '[data-limits-soundboard] input[type=number][max="300"]', 130);
    await click(A, button('Save changes'));
    const saved = await waitFor(async () => (await state(A)).sound_limits?.longest === 130, 'the longest raised', 10000).catch(() => false);
    report(saved === true, 'the owner raises the longest sound in Limits');
    await click(A, `${MANAGE}.querySelector('button[aria-label=Close]')`);
    lib('api', 'alice', 'PUT', `/api/dens/${den}/members/${bobID}/role`, '{"role":"moderator"}');
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-soundboard-button=block]')`), "bob's soundboard", 15000);
    await click(B, `document.querySelector('[data-den-manage]')`);
    await soundboardSection(B);
    report(await addSound(B, video, '') === 'added', `bob adds a video's sound, which his ${which} prepares`);
    report(await addSound(B, long, 'Long') === 'added', 'and one of two minutes');
    const videoSound = await soundNamed(B, 'late-sound');
    const longSound = await soundNamed(B, 'Long');
    report(videoSound && Math.abs(longSound?.duration_ms - 120000) <= 100, `the video's sound is ${videoSound?.duration_ms} ms, and the long one ${longSound?.duration_ms} ms`);
    // A film's 5.1 keeps its dialogue in the middle: a tone in the centre
    // alone comes out in both sides alike.
    report(await addSound(B, middle, 'Middle') === 'added', `and a 5.1 WAV, which his ${which} folds into two`);
    const middleSound = await soundNamed(B, 'Middle');
    const sides = JSON.parse(await B.b.eval(`(async () => {
        const data = await (await fetch('/api/dens/${den}/files/${middleSound?.file_id}')).arrayBuffer();
        const d = await new OfflineAudioContext(1, 1, 48000).decodeAudioData(data);
        const rms = (c) => Math.sqrt(d.getChannelData(c).reduce((s, x) => s + x * x, 0) / d.length);
        return JSON.stringify({ channels: d.numberOfChannels, left: rms(0), right: d.numberOfChannels > 1 ? rms(1) : 0 });
    })()`));
    report(sides.channels === 2 && sides.left > 0.01 && Math.abs(sides.left - sides.right) <= sides.left * 0.05,
        `its centre comes out in both sides alike: ${JSON.stringify(sides)}`);
    lib('api', 'bob', 'DELETE', `/api/dens/${den}/soundboard/${middleSound?.id}`);
    await click(B, `${MANAGE}.querySelector('button[aria-label=Close]')`);
    await waitFor(async () => !(await B.b.eval(`!!${MANAGE}`)), "bob's Manage closed");

    step("bob plays a sound from the call's block; alice receives it and lights his ring");
    await click(B, `document.querySelector('[data-soundboard-button=block]')`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-soundboard=block] [data-sound-tile]')`), "bob's soundboard");
    const at = await rect(B, '[data-soundboard-button=block]');
    const popup = await rect(B, '[data-soundboard=block]');
    report(popup.bottom <= at.top && Math.abs(popup.left - at.left) <= 8, `it opens up and to the right of the block's button: ${JSON.stringify({ at, popup })}`);
    const tiles = await B.b.eval(`[...document.querySelectorAll('[data-soundboard=block] [data-sound-tile]')].map((t) => t.querySelector('span').textContent).join('|')`);
    report(tiles === 'Chord|late-sound|Long|Tone', `its tiles are the den's sounds by name: ${tiles}`);
    await setValue(B, '[data-soundboard=block] input[type=search]', 'TO');
    const found = await B.b.eval(`[...document.querySelectorAll('[data-soundboard=block] [data-sound-tile]')].map((t) => t.querySelector('span').textContent).join('|')`);
    report(found === 'Tone', `a search finds names in any case: ${found}`);
    let before = await board(A);
    await tile(B, toneSound.id);
    const lit = await waitFor(() => ring(A, 'Bob'), "bob's ring at alice's", 5000).catch(() => false);
    const waiting = await waitFor(() => B.b.eval(`document.querySelector('[data-board-why]')?.textContent || false`), "bob's tiles waiting", 3000).catch(() => '');
    const got = await received(A, before);
    report(lit === true && got && got.energy > before.energy, `alice receives the soundboard playing, by its stats and level, and lights bob's ring: ${JSON.stringify({ before, got })}`);
    report(waiting === 'A sound is playing.' && await B.b.eval(`document.querySelector('[data-sound-tile]').disabled`), `bob's tiles wait while it plays: ${waiting}`);
    const out = await waitFor(async () => !(await ring(A, 'Bob')), 'the ring out', 5000).catch(() => false);
    const wait = await waitFor(async () => /^Wait \d s/.test(await B.b.eval(`document.querySelector('[data-board-why]')?.textContent || ''`)), "bob's wait", 3000).catch(() => false);
    report(out === true && wait === true, `the ring goes out as it ends, and bob waits after his sound: ${out}, ${wait}`);
    const free = await waitFor(async () => !(await B.b.eval(`!!document.querySelector('[data-board-why]')`)), "bob's wait over", 8000).catch(() => false);
    report(free === true, "bob's wait passes");

    step("the popup is framed, and staff's cog opens the sounds in Manage; deafened, bob's tiles wait, and alice's sound lights no ring for him");
    const frame = await B.b.eval(`getComputedStyle(document.querySelector('[data-soundboard=block]')).borderTopWidth`);
    await click(B, `document.querySelector('[data-soundboard=block] button[aria-label="Manage sounds"]')`);
    const managed = await waitFor(() => B.b.eval(`!!${MANAGE}?.querySelector('[data-add-sound]')`), "Manage's Soundboard from the cog", 5000).catch(() => false);
    report(frame !== '0px' && managed === true, `the popup is framed (${frame}), and bob's cog beside its search opens Manage's Soundboard`);
    await click(B, `${MANAGE}.querySelector('button[aria-label=Close]')`);
    await waitFor(async () => !(await B.b.eval(`!!${MANAGE}`)), "bob's Manage closed");
    await click(B, `document.querySelector('[data-soundboard-button=block]')`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-soundboard=block] [data-sound-tile]')`), "bob's soundboard again");
    await click(B, `document.querySelector('button[aria-label=Deafen]')`);
    const deaf = await waitFor(() => B.b.eval(`document.querySelector('[data-board-why]')?.textContent || false`), "bob's tiles while deafened", 3000).catch(() => '');
    report(deaf === 'Undeafen to play sounds.' && await B.b.eval(`document.querySelector('[data-sound-tile]').disabled`), `deafened, bob's tiles wait: ${deaf}`);
    await click(A, `document.querySelector('[data-soundboard-button=block]')`);
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-soundboard=block] [data-sound-tile]')`), "alice's soundboard");
    before = await board(B);
    await tile(A, toneSound.id);
    const heard = await received(B, before);
    const herOwn = await ring(A, 'Alice');
    let rang = false;
    for (let i = 0; i < 4 && !rang; i++) {
        rang = await ring(B, 'Alice');
        await sleep(200);
    }
    report(heard && herOwn && !rang, `bob's page, deafened, receives alice's sound but lights no ring, as hers does: ${JSON.stringify({ heard: !!heard, herOwn, rang })}`);
    await click(A, `document.querySelector('[data-soundboard-button=block]')`);
    await click(B, `document.querySelector('button[aria-label=Undeafen]')`);
    // Alice's wait passes before the two ask at once.
    await sleep(6000);

    step('alice and bob ask for sounds at once: one plays, and the den refuses the other');
    await setValue(B, '[data-soundboard=block] input[type=search]', '');
    await click(A, `document.querySelector('[data-soundboard-button=block]')`);
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-soundboard=block] [data-sound-tile]')`), "alice's soundboard");
    // Each page refuses a tap itself once it hears another's sound play,
    // so the two may need a few goes to reach the den together.
    let race = null;
    for (let go = 0; go < 3 && !race?.refused; go++) {
        if (go) await sleep(7000);
        const clicks = await Promise.all([tile(A, chordSound.id), tile(B, chordSound.id)]);
        const notices = await waitFor(async () => {
            const a = await A.b.eval(`document.querySelector('[data-board-notice]')?.textContent || ''`);
            const b = await B.b.eval(`document.querySelector('[data-board-notice]')?.textContent || ''`);
            return a || b ? [a, b] : null;
        }, 'a refusal', 4000).catch(() => null);
        race = { go, clicks, notices, refused: notices?.filter((x) => x === 'A sound is playing already.').length === 1 };
    }
    report(race.refused, `one of them hears the den refuse: ${JSON.stringify(race)}`);
    await click(A, `document.querySelector('[data-soundboard-button=block]')`);
    await sleep(6000);

    step("from Home's box, bob plays one, down from the box");
    await click(B, `document.querySelector('a[aria-label^=Home]')`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-soundboard-button=box]')`), "Home's box");
    await click(B, `document.querySelector('[data-soundboard-button=box]')`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-soundboard=box] [data-sound-tile]')`), "the box's soundboard");
    const boxAt = await rect(B, '[data-soundboard-button=box]');
    const boxPopup = await rect(B, '[data-soundboard=box]');
    report(boxPopup.top >= boxAt.bottom, `it opens down from the box: ${JSON.stringify({ boxAt, boxPopup })}`);
    before = await board(A);
    await tile(B, chordSound.id);
    report(await received(A, before), "alice receives the sound bob plays from Home's box");
    await B.b.close();

    step("at 390 pixels, bob plays one from the phone's bar, as a sheet, beside its mute");
    B.b = await launch(which, which === 'chromium' ? 9562 : 9563, { media: which === 'firefox' ? 'pulse' : true, preload: RECORD, hover: true, size: [390, 844] });
    // A narrow window with a mouse, as the phone check's: Firefox keeps a
    // window at least 500 wide, so its viewport goes to 390 through BiDi.
    if (which === 'chromium') await B.b.cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: false });
    else await B.b.bidi('browsingContext.setViewport', { context: B.b.context, viewport: { width: 390, height: 844 } });
    await pair(B.b, B);
    await B.b.go(`http://localhost:${B.port}/den/${den}`);
    await joinLounge(B);
    await B.b.eval(`document.querySelector('[data-pane=chan]')?.scrollIntoView(), true`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-call-bar]')`), "the call's bar");
    const bar = await B.b.eval(`[...document.querySelector('[data-call-bar]').querySelectorAll('button')].map((b) => b.getAttribute('aria-label') || '').join('|')`);
    report(bar === 'Soundboard|Mute|Deafen|Leave the call' || bar.endsWith('Soundboard|Mute|Deafen|Leave the call'), `the bar holds the soundboard, mute, deafen and leave: ${bar}`);
    await click(B, `document.querySelector('[data-call-bar] button[aria-label=Mute]')`);
    const muted = await waitFor(() => B.b.eval(`document.querySelector('[data-call-bar] button[aria-label=Unmute]')?.getAttribute('aria-pressed') === 'true'`), "bob muted from the bar", 3000).catch(() => false);
    await click(B, `document.querySelector('[data-call-bar] button[aria-label=Unmute]')`);
    report(muted === true, 'the bar mutes, and unmutes');
    await sleep(6000);
    await click(B, `document.querySelector('[data-call-bar] [data-soundboard-button=bar]')`);
    const sheet = await waitFor(() => B.b.eval(`!!document.querySelector('[data-sheet] [data-sound-tile]')`), 'the soundboard\'s sheet', 5000).catch(() => false);
    before = await board(A);
    await tile(B, toneSound.id);
    report(sheet === true && await received(A, before), "the bar's soundboard is a sheet, and alice receives what bob plays from it");
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);

    step("bob joins partway through alice's long sound, receives the rest, which ends as she leaves");
    await sleep(6000);
    await leave(B);
    await click(A, `document.querySelector('[data-soundboard-button=block]')`);
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-soundboard=block] [data-sound-tile]')`), "alice's soundboard");
    await tile(A, longSound.id);
    await sleep(3000);
    await joinLounge(B);
    before = await board(B);
    const rest = await received(B, before, 100);
    const hers = await waitFor(() => ring(B, 'Alice'), "alice's ring at bob's", 5000).catch(() => false);
    report(rest && hers === true, `bob, joining partway, receives the rest and lights alice's ring: ${JSON.stringify(rest)}`);

    step("bob's slider at 0 mutes it");
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-call-bar] [data-soundboard-button=bar]')`), "bob's bar", 5000);
    await click(B, `document.querySelector('[data-call-bar] [data-soundboard-button=bar]')`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-sheet] [data-board-volume]')`), "bob's sheet");
    await setValue(B, '[data-sheet] [data-board-volume]', 0);
    const silent = await waitFor(async () => (await board(B)).volume === 0, "the soundboard muted", 3000).catch(() => false);
    report(silent === true, `bob's soundboard at 0 plays nothing: ${JSON.stringify(await board(B))}`);
    await setValue(B, '[data-sheet] [data-board-volume]', 100);
    report((await board(B)).volume === 1, 'and back at 100 plays it whole');
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    before = await board(B);
    await leave(A);
    const ended = await waitFor(async () => !(await ring(B, 'Alice')), "alice's ring out", 5000).catch(() => false);
    await sleep(1000);
    const after = await board(B);
    await sleep(1000);
    const later = await board(B);
    report(ended === true && later.packets - after.packets <= 2, `the long sound ends as alice leaves: ${JSON.stringify({ before, after, later })}`);
} catch (e) {
    c.fail(e);
}
for (const s of [A, B]) await s.b?.close().catch(() => {});
c.finish();
