// The M16.4 check of disappearing messages: alice, who hosts the den, in
// Chromium, and bob in the browser named; carol through her page's API.
// Bob sets the DM's time from the profile beside it, which both pages show
// as a line and in the header. Under a time of seconds, which a
// development instance's den takes, a message alice writes shows its mark,
// and goes from both pages, from bob's cache and from the den's database
// as its time passes, into the den's change log. Alice sets a group's time
// from its members, and bob's message there goes from both pages too. And
// with the den stopped, a message goes from bob's page and his cache all
// the same.
//
//   run.sh disappearing chromium|firefox
import { execFileSync } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, DM: dm, BOB_ID: bobID, CAROL_ID: carolID } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;
const q = (s) => JSON.stringify(s);
// SECONDS is the time of seconds the check's messages go after.
const SECONDS = 12;
const COMPOSER = `document.querySelector('textarea[placeholder^="Message "]')`;
const lines = (s) => s.b.eval(`[...document.querySelectorAll('[data-expiry-line]')].map((x) => x.textContent.trim())`);
const badge = (s) => s.b.eval(`document.querySelector('[data-expiry-badge]')?.textContent.trim() || ''`);
const text = (s) => s.b.eval('document.body.innerText');
// marked says whether a message's row on a page shows it disappears.
const marked = (s, words) => s.b.eval(`[...document.querySelectorAll('[data-id]')].some((r) => r.textContent.includes(${q(words)}) && !!r.querySelector('[data-disappears]'))`);
const api = (who, method, path, body) => {
    const args = ['api', who, method, path];
    if (body !== undefined) args.push(JSON.stringify(body));
    return lib(...args);
};
const history = (who, channel) => JSON.parse(api(who, 'GET', `/api/dens/${den}/channels/${channel}/messages`)).messages;
const nonce = () => lib('nonce');
// count asks an instance's database how many rows a query counts.
const count = (path, query, ...args) => Number(execFileSync('python3', ['-c',
    'import sqlite3, sys; db = sqlite3.connect(sys.argv[1]); print(db.execute(sys.argv[2], sys.argv[3:]).fetchone()[0])',
    `${HOME}/.local/share/dens-dev/${path}`, query, ...args]).toString().trim());
const denDB = 'm2a/data/db/app.db';
const bobCache = 'm2b/data/cache/cache.db';

// lineShown waits for a line in the conversation on a page, which starts
// with the day it was set.
async function lineShown(s, line) {
    return waitFor(async () => (await lines(s)).some((l) => /^On .+, /.test(l) && l.endsWith(line)), `${q(line)} on ${s.name}'s page`, 15000)
        .then(() => true, () => false);
}

// gone waits for a message to leave a page.
async function gone(s, words, ms = (SECONDS + 15) * 1000) {
    return waitFor(async () => !(await text(s)).includes(words), `${q(words)} gone from ${s.name}'s page`, ms).then(() => true, () => false);
}

// beside opens what's beside a DM or a group, by its header's button,
// unless it shows already, as at a desktop's width.
async function beside(s, label) {
    if (!(await s.b.eval(`!!document.querySelector('[data-expiry-setting]')`))) {
        await s.b.eval(`document.querySelector(${q(`button[aria-label="${label}"]`)}).click(), true`);
    }
}

// choose picks a time in the setting beside a DM or a group, and confirms
// it in the dialog that asks first, whose words it returns, with where its
// link leads.
async function choose(s, value, confirm) {
    const select = `document.querySelector('[data-expiry-setting] select')`;
    await waitFor(() => s.b.eval(`!!${select} && !${select}.disabled`), `the time's setting on ${s.name}'s page`);
    await s.b.eval(`(() => { const el = ${select}; el.value = ${q(String(value))}; el.dispatchEvent(new Event('change', { bubbles: true })); return true; })()`);
    const asked = `document.querySelector('[data-expiry-confirm]')`;
    await waitFor(() => s.b.eval(`!!${asked}`), `the dialog asking first on ${s.name}'s page`);
    const said = await s.b.eval(`${asked}.textContent + ' | ' + ${asked}.querySelector('a').href + ' ' + ${asked}.querySelector('a').target`);
    await s.b.eval(`[...document.querySelectorAll('[role=dialog] button')].find((b) => b.textContent.trim() === ${q(confirm)}).click(), true`);
    await waitFor(() => s.b.eval(`!${asked}`), `the dialog gone on ${s.name}'s page`);
    return said;
}

async function sends(s, line) {
    await waitFor(() => s.b.eval(`!!${COMPOSER}`), `${s.name}'s composer`);
    await s.b.eval(`${COMPOSER}.focus(), true`);
    await s.b.type(line);
    await s.b.key('Enter');
}

try {
    A.b = await launch('chromium', 9591, { hover: true });
    B.b = await launch(which, which === 'chromium' ? 9592 : 9593, { hover: true });
    for (const s of [A, B]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}/${dm}`);
        await waitFor(() => s.b.eval(`!!${COMPOSER}`), `${s.name}'s DM`);
    }

    // The DM tab offers a group, and none of the den tab's buttons, to its
    // owner too.
    const plus = await A.b.eval(`JSON.stringify([...document.querySelectorAll('[data-pane=list] button')].map((b) => b.textContent.trim()).filter((t) => t.startsWith('+')))`);
    report(plus === '["+ New group"]', `alice's DM tab offers + New group alone: ${plus}`);

    step("bob sets the DM's time from the profile beside it");
    await beside(B, 'Profile');
    const asked = await choose(B, 3600, 'Turn on');
    report(/^New messages disappear for everyone 1 hour after they're sent\. How it works \| https:\/\/dens\.chat\/docs\/join\/#disappearing-messages _blank$/.test(asked),
        `bob's pick asks first, linking to the guide: ${asked}`);
    report(await lineShown(A, "Bob set messages to disappear 1 hour after they're sent."), "alice's page shows the line where bob set the DM's time");
    report(await lineShown(B, "you set messages to disappear 1 hour after they're sent."), "bob's page says he set it");
    report(await waitFor(async () => (await badge(A)) === '1h', "the time in alice's header").then(() => true, () => false),
        "alice's header says the DM's messages go after an hour");
    await sleep(300);
    await B.b.shot(`${HOME}/shot-disappearing-setting-bob-${which}.png`);

    step(`a time of ${SECONDS} seconds, and a message alice writes under it`);
    api('bob', 'PUT', `/api/dens/${den}/dms/${dm}/expiry`, { expires_after: SECONDS });
    report(await lineShown(A, `Bob set messages to disappear ${SECONDS} seconds after they're sent.`), "alice's page shows the time of seconds");
    await sends(A, 'gone in seconds');
    const shown = await Promise.all([A, B].map((s) => waitFor(() => marked(s, 'gone in seconds'), `the marked message on ${s.name}'s page`)
        .then(() => true, () => false)));
    report(shown.every(Boolean), 'both pages show the message, marked as one that disappears');
    const sent = history('alice', dm).find((m) => m.text === 'gone in seconds');
    report(!!sent?.expires_at, `the message goes at ${sent?.expires_at}`);
    report(await waitFor(() => count(bobCache, 'SELECT count(*) FROM messages WHERE id = ? AND expires IS NOT NULL', sent.id) === 1, "the message in bob's cache")
        .then(() => true, () => false), "bob's cache holds the message, with when it goes");
    await A.b.shot(`${HOME}/shot-disappearing-dm-alice.png`);
    const went = await Promise.all([A, B].map((s) => gone(s, 'gone in seconds')));
    report(went.every(Boolean), 'the message leaves both pages as its time passes');
    report(await waitFor(() => count(bobCache, 'SELECT count(*) FROM messages WHERE id = ?', sent.id) === 0, "the message gone from bob's cache")
        .then(() => true, () => false), "bob's cache no longer holds it");
    report(count(denDB, 'SELECT count(*) FROM den_messages WHERE id = ?', sent.id) === 0 &&
        count(denDB, `SELECT count(*) FROM den_changes WHERE kind = 'message' AND ref = ?`, sent.id) === 1,
    "the den's database no longer holds it, and its change log says it went");

    step("alice sets a group's time from its members");
    const g = JSON.parse(api('alice', 'POST', `/api/dens/${den}/group-dms`, { members: [bobID, carolID] })).id;
    for (const s of [A, B]) {
        await waitFor(() => s.b.eval(`!!document.querySelector('[data-group-row=${q(g)}]')`), `the group on ${s.name}'s page`);
        await s.b.go(`http://localhost:${s.port}/den/${den}/${g}`);
        await waitFor(() => s.b.eval(`!!${COMPOSER}`), `the group's composer on ${s.name}'s page`);
    }
    await beside(A, "The group's members");
    await choose(A, 86400, 'Turn on');
    report(await lineShown(B, "Alice set messages to disappear 1 day after they're sent."), "bob's page shows the line where alice set the group's time");
    report(await waitFor(async () => (await badge(B)) === '1d', "the time in bob's header").then(() => true, () => false),
        "bob's header says the group's messages go after a day");
    await A.b.shot(`${HOME}/shot-disappearing-group-alice.png`);
    api('alice', 'PUT', `/api/dens/${den}/dms/${g}/expiry`, { expires_after: SECONDS });
    await lineShown(B, `Alice set messages to disappear ${SECONDS} seconds after they're sent.`);
    await sends(B, 'the group forgets this');
    report((await Promise.all([A, B].map((s) => waitFor(() => marked(s, 'the group forgets this'), `the group's message on ${s.name}'s page`)
        .then(() => true, () => false)))).every(Boolean), "both pages show bob's message in the group, marked");
    report((await Promise.all([A, B].map((s) => gone(s, 'the group forgets this')))).every(Boolean), "the group's message leaves both pages as its time passes");

    step('with the den stopped, a message goes from bob\'s page and cache all the same');
    await B.b.go(`http://localhost:${B.port}/den/${den}/${dm}`);
    await waitFor(() => B.b.eval(`!!${COMPOSER}`), "bob's DM again");
    api('alice', 'POST', `/api/dens/${den}/channels/${dm}/messages`, { nonce: nonce(), text: 'gone while the den is away' });
    await waitFor(() => marked(B, 'gone while the den is away'), "the message on bob's page");
    const away = history('bob', dm).find((m) => m.text === 'gone while the den is away');
    lib('stop', 'm2a');
    report(count(bobCache, 'SELECT count(*) FROM messages WHERE id = ?', away.id) === 1, "bob's cache holds the message as the den stops");
    report(await gone(B, 'gone while the den is away'), "the message leaves bob's page with the den away");
    report(await waitFor(() => count(bobCache, 'SELECT count(*) FROM messages WHERE id = ?', away.id) === 0, "the message gone from bob's cache offline")
        .then(() => true, () => false), "bob's cache lets it go with the den away");
    await B.b.shot(`${HOME}/shot-disappearing-offline-bob-${which}.png`);
} catch (e) {
    c.fail(e);
} finally {
    for (const s of [A, B]) await s.b?.close().catch(() => {});
    await sleep(100);
}
c.finish();
