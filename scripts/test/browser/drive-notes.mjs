// The M17.3 check of notes to self: bob's phone, his first device, in the
// browser named, and his laptop in Chromium; alice through her page's API.
// Bob opens his notes from the top of his Direct messages tab, which the
// den makes as he first does, their header saying they're encrypted, with
// no members or bell beside it; he writes one with a photo, attached in
// the composer, whose + offers no one else's editing, nor a poll, and whose
// photo no Spoiler. His laptop opens them from his own profile card's
// Notes, reads the note and its photo, opened from its seal, and writes
// back, which his phone shows with nothing marked unread. The den's
// database holds neither text in the clear, and alice's Dens has nothing
// of them.
//
//   run.sh notes chromium|firefox
import { execFileSync } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import { BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, BOB_ID: bobID } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const PHONE = { ...BOB };
const LAPTOP = { name: "bob's laptop", instance: 'm2e', port: 18490 };
const q = (s) => JSON.stringify(s);
const COMPOSER = `document.querySelector('textarea[placeholder="Write a note"]')`;
const ROW = `document.querySelector('[data-notes-row] button')`;
const HEADER = `document.querySelector('[data-notes-header]')`;
const text = (s) => s.b.eval('document.body.innerText');
const api = (who, method, path, body) => {
    const args = ['api', who, method, path];
    if (body !== undefined) args.push(JSON.stringify(body));
    return lib(...args);
};
const state = (who) => JSON.parse(api(who, 'GET', `/api/dens/${den}/state`));
// count asks an instance's database how many rows a query counts.
const count = (path, query, ...args) => Number(execFileSync('python3', ['-c',
    'import sqlite3, sys; db = sqlite3.connect(sys.argv[1]); print(db.execute(sys.argv[2], sys.argv[3:]).fetchone()[0])',
    `${HOME}/.local/share/dens-dev/${path}`, query, ...args]).toString().trim());
const denDB = 'm2a/data/db/app.db';

// dms shows a page's Direct messages tab.
async function dms(s) {
    await s.b.eval(`[...document.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim().startsWith('Direct messages')).click(), true`);
    await waitFor(() => s.b.eval(`!!${ROW}`), `the notes' row on ${s.name}'s page`);
}

// writes types a note and sends it with Enter.
async function writes(s, line) {
    await waitFor(() => s.b.eval(`!!${COMPOSER}`), `${s.name}'s notes' composer`);
    await s.b.eval(`${COMPOSER}.focus(), true`);
    await s.b.type(line);
    await s.b.key('Enter');
}

// shows waits for a note's text on a page.
const shows = (s, words) => waitFor(async () => (await text(s)).includes(words), `${q(words)} on ${s.name}'s page`).then(() => true, () => false);

try {
    PHONE.b = await launch(which, which === 'chromium' ? 9592 : 9593, { hover: true });
    LAPTOP.b = await launch('chromium', 9591, { hover: true });
    for (const s of [PHONE, LAPTOP]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}/${general}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('textarea[placeholder="Message #general"]')`), `#general on ${s.name}'s page`);
    }

    step('bob opens his notes from the top of his Direct messages tab');
    report(state('bob').channels.every((x) => x.kind !== 'notes'), 'the den has made no notes before bob opens them');
    await dms(PHONE);
    report(await PHONE.b.eval(`!!document.querySelector('nav[aria-label="Direct messages"] li')?.hasAttribute('data-notes-row')`),
        "the notes are the first row of bob's Direct messages tab");
    await PHONE.b.eval(`${ROW}.click(), true`);
    await waitFor(() => PHONE.b.eval(`!!${COMPOSER} && !!${HEADER}`), "bob's notes open");
    const notes = state('bob').channels.find((x) => x.kind === 'notes');
    report(notes && notes.members.length === 1 && notes.members[0] === bobID, `the den made bob's notes, naming him alone: ${q(notes)}`);
    const header = JSON.parse(await PHONE.b.eval(`JSON.stringify({ text: document.querySelector('header').innerText,
        people: !!document.querySelector('header button[aria-label="Members"], header button[aria-label="Profile"]'),
        bell: !!document.querySelector('header button[aria-label^="Mute"], header button[aria-label^="Unmute"]') })`));
    report(/Notes/.test(header.text) && /Encrypted/.test(header.text) && !header.people && !header.bell,
        `the notes' header says they're encrypted, with no members or bell: ${q(header)}`);
    report(/This is the start of your notes\.\s*End-to-end encrypted: only your devices can read them\./.test(await text(PHONE)),
        'the notes start by saying only his devices read them');

    step('bob writes a note with a photo');
    await PHONE.b.eval(`document.querySelector('button[aria-label="Add to this message"]').click(), true`);
    await waitFor(() => PHONE.b.eval(`!!document.querySelector('[role=menu]')`), 'the composer\'s + menu');
    const offered = await PHONE.b.eval(`JSON.stringify([...document.querySelectorAll('[role=menu] [role=menuitem]')].map((x) => x.textContent.trim()))`);
    report(offered === '["Attach files"]', `the composer's + offers files alone: ${offered}`);
    await PHONE.b.eval(`document.querySelector('button[aria-label="Add to this message"]').click(), true`);
    await PHONE.b.attach('input[type=file][multiple]', [`${HOME}/photo.jpg`]);
    await waitFor(() => PHONE.b.eval(`!!document.querySelector('button[aria-label="Remove photo.jpg"]') && !document.querySelector('form progress, li progress')`),
        "the photo ready in bob's composer");
    report(!(await PHONE.b.eval(`!!document.querySelector('[data-spoiler-toggle]')`)), 'the photo waiting in the notes offers no Spoiler');
    await writes(PHONE, 'milk, eggs');
    report(await shows(PHONE, 'milk, eggs'), "the note shows on bob's phone");
    report(await waitFor(() => PHONE.b.eval(`[...document.querySelectorAll('img')].some((i) => i.naturalWidth > 0 && i.src.includes('/files/'))`), 'the photo on the phone')
        .then(() => true, () => false), "the note's photo shows on bob's phone");
    await sleep(300);
    await PHONE.b.shot(`${HOME}/shot-notes-phone-${which}.png`);

    step('his laptop opens them from his own profile card');
    await waitFor(() => LAPTOP.b.eval(`[...document.querySelectorAll('nav[aria-label="Members"] button')].some((x) => x.textContent.includes('Bob'))`),
        "bob in the laptop's member list");
    await LAPTOP.b.eval(`[...document.querySelectorAll('nav[aria-label="Members"] button')].find((x) => x.textContent.includes('Bob')).click(), true`);
    await waitFor(() => LAPTOP.b.eval(`!!document.querySelector('[data-own-notes]')`), "Notes on bob's own profile card");
    await LAPTOP.b.shot(`${HOME}/shot-notes-card.png`);
    await LAPTOP.b.eval(`document.querySelector('[data-own-notes]').click(), true`);
    await waitFor(() => LAPTOP.b.eval(`!!${COMPOSER} && !!${HEADER} && !document.querySelector('[role=dialog]')`), "the notes open on bob's laptop");
    report(await shows(LAPTOP, 'milk, eggs'), "bob's laptop reads the note his phone wrote");
    report(await waitFor(() => LAPTOP.b.eval(`[...document.querySelectorAll('img')].some((i) => i.naturalWidth > 0 && i.src.includes('/files/'))`), 'the photo on the laptop')
        .then(() => true, () => false), "bob's laptop shows the note's photo, opened from its seal");
    report(await LAPTOP.b.eval(`document.querySelector('[data-notes-row]')?.className.includes('bg-base-300')`), "the laptop's DM list marks the notes open");
    await writes(LAPTOP, 'and bread');
    report(await shows(PHONE, 'and bread'), "bob's phone reads the note his laptop wrote");
    await sleep(500);
    const marks = JSON.parse(await PHONE.b.eval(`JSON.stringify({ pill: !!document.querySelector('[data-notes-row] [data-unread]'),
        bold: document.querySelector('[data-notes-row] span.truncate')?.className.includes('font-bold'),
        dot: [...document.querySelectorAll('[role=tab]')].some((t) => !!t.querySelector('[aria-label=unread]')) })`));
    report(!marks.pill && !marks.bold && !marks.dot, `nothing on bob's phone marks the laptop's note unread: ${q(marks)}`);
    const status = JSON.parse(api('bob', 'GET', '/api/dens')).dens.find((d) => d.den_id === den);
    report(!status.unread && !status.mentions, `bob's Dens counts nothing unread: ${status.unread || 0} unread, ${status.mentions || 0} mentions`);
    await LAPTOP.b.shot(`${HOME}/shot-notes-laptop.png`);

    step("the den's database holds neither note in the clear, and alice's Dens nothing of them");
    report(count(denDB, 'SELECT count(*) FROM den_messages WHERE channel_id = ?', notes.id) === 2 &&
        count(denDB, `SELECT count(*) FROM den_messages WHERE channel_id = ? AND (instr(text, CAST('milk' AS BLOB)) > 0 OR instr(text, CAST('bread' AS BLOB)) > 0)`, notes.id) === 0,
    "the den's database holds the two notes, neither one's text in the clear");
    report(count(denDB, 'SELECT count(*) FROM den_files f JOIN den_messages m ON m.id = f.message_id WHERE m.channel_id = ? AND f.sealed = 1', notes.id) >= 1,
        "the den holds the note's photo as a sealed blob alone");
    report(!state('alice').channels.some((x) => x.id === notes.id), "alice's Dens has nothing of bob's notes");
    let read = true;
    try {
        api('alice', 'GET', `/api/dens/${den}/channels/${notes.id}/messages`);
    } catch {
        read = false;
    }
    report(!read, "alice's Dens can't read bob's notes");
} catch (e) {
    c.fail(e);
} finally {
    for (const s of [PHONE, LAPTOP]) await s.b?.close().catch(() => {});
    await sleep(100);
}
c.finish();
