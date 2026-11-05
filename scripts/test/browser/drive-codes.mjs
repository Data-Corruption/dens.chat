// The M16.1 check of identities and codes: alice, who owns the den, in
// Chromium, and bob in the browser named. Their DM opens at once, its
// composer showing on both pages with nothing to type, and both write at
// the same moment, each reading both. Each compares the other's code: from
// the lock in the DM's header, and from the profile beside the DM, own
// digits and wrong ones refused, a match marking the other verified with a
// green circled check. Bob's comparison reaches his laptop within the time
// a development instance writes his record in, its hour shortened. Bob
// starts over: alice's page shows his keys changed, his mark gone, and the
// DM holds, Compare and Send anyway in its composer's place, until she
// sends anyway; and comparing his new code marks him again.
//
//   run.sh codes chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, DM: dm, ALICE_ID: aliceID, BOB_ID: bobID } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;
const q = (s) => JSON.stringify(s);
const COMPOSER = `document.querySelector('textarea[placeholder^="Message "]')`;
const CODES = `document.querySelector('[data-codes]')`;
const DIALOG = `document.querySelector('[role=dialog][aria-label^="Compare codes"]')`;
const LOCK = `document.querySelector('button[aria-label="End-to-end encrypted: compare codes"]')`;
const HOLD = `document.querySelector('[data-hold]')`;
const LINE = `document.querySelector('[data-keys-changed]')`;
const button = (scope, text) => `[...${scope}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${q(text)})`;
// setValue sets a field's value as typing would, for Preact to hear.
const setValue = (field, value) => `(() => { const el = ${field}; const set = Object.getOwnPropertyDescriptor(el.constructor.prototype, 'value').set;
    set.call(el, ${q(value)}); el.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`;
const text = (s) => s.b.eval('document.body.innerText');
const LOCKED = /can't open this message|doesn't open|isn't shown|can't be opened/;

// verified says whether a page marks the DM's other member verified in
// its header.
const verified = (s, name) => s.b.eval(`!!document.querySelector('header [aria-label="You verified ${name}"]')`);

// sends writes text in the composer, as typing it, and sends it with
// Enter.
async function sends(s, line) {
    await s.b.eval(`${COMPOSER}.focus(), true`);
    await s.b.type(line);
    await s.b.key('Enter');
}

// own opens comparing codes, and reads out the member's own code.
async function own(s) {
    await waitFor(() => s.b.eval(`!!${CODES}`), `${s.name}'s codes`);
    return s.b.eval(`${CODES}.querySelector('.font-mono').textContent.trim()`);
}

// compares types digits as the other read them out, and says what the
// dialog said.
async function compares(s, digits, want) {
    await s.b.eval(setValue(`${CODES}.querySelector('input')`, digits));
    await s.b.eval(`${button(CODES, 'Compare')}.click(), true`);
    return waitFor(async () => {
        const t = await s.b.eval(`${CODES}?.textContent || ''`);
        return want.test(t) ? t : null;
    }, `${s.name}'s comparison`, 10000).catch(() => s.b.eval(`${CODES}?.textContent || ''`));
}

const close = (s) => s.b.eval(`document.querySelector('[role=dialog] button[aria-label="Close"]').click(), true`);

try {
    A.b = await launch('chromium', 9591, { hover: true });
    B.b = await launch(which, which === 'chromium' ? 9592 : 9593, { hover: true });
    for (const s of [A, B]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}/${dm}`);
    }

    step('the DM opens at once on both pages');
    for (const s of [A, B]) {
        const open = await waitFor(() => s.b.eval(`!!${COMPOSER} && !${HOLD}`), `${s.name}'s composer`).catch(() => false);
        const t = await text(s);
        report(open && !/check code/i.test(t), `${s.name}'s DM shows its composer at once, with no digits to type`);
    }

    step('both write at the same moment');
    await Promise.all([sends(A, 'hi from alice'), sends(B, 'hi from bob')]);
    for (const s of [A, B]) {
        const t = await waitFor(async () => {
            const t = await text(s);
            return t.includes('hi from alice') && t.includes('hi from bob') ? t : null;
        }, `both messages on ${s.name}'s page`).catch(() => text(s));
        report(t.includes('hi from alice') && t.includes('hi from bob') && !LOCKED.test(t), `${s.name} reads both messages, written at once`);
    }

    step('alice compares from the lock, bob from the profile beside the DM');
    await A.b.eval(`${LOCK}.click(), true`);
    const aliceCode = await own(A);
    await B.b.eval(`document.querySelector('button[aria-label="Profile"]').click(), true`);
    await waitFor(() => B.b.eval(`!!${button('document', 'Compare codes')}`), "Compare codes in bob's profile pane");
    await B.b.eval(`${button('document', 'Compare codes')}.click(), true`);
    const bobCode = await own(B);
    report(/^\d{4}( \d{4}){4}$/.test(aliceCode) && /^\d{4}( \d{4}){4}$/.test(bobCode) && aliceCode !== bobCode,
        `each page shows its member's own code, 20 digits in groups of four: ${aliceCode} and ${bobCode}`);
    report(!(await text(A)).includes(bobCode) && !(await text(B)).includes(aliceCode), "neither page shows the other member's code");
    await A.b.shot(`${HOME}/shot-codes-dialog-alice.png`);
    await B.b.shot(`${HOME}/shot-codes-dialog-bob-${which}.png`);

    let said = await compares(A, aliceCode, /your own digits/);
    report(/your own digits/.test(said), `alice's own digits are refused: ${q(said.slice(0, 80))}`);
    said = await compares(B, '1111 2222 3333 4444 5555', /don't match/);
    report(/don't match/.test(said) && !(await B.b.eval(`!!${CODES}.querySelector('[data-verified]')`)), "wrong digits don't match, and mark nothing");
    said = await compares(A, bobCode.replaceAll(' ', '-'), /The codes match/);
    report(/The codes match: you verified Bob/.test(said), 'alice types bob\'s code, with dashes, and it matches');
    said = await compares(B, aliceCode, /The codes match/);
    report(/The codes match: you verified Alice/.test(said), "bob types alice's code, and it matches");
    for (const s of [A, B]) await close(s);
    report(await waitFor(() => verified(A, 'Bob'), "bob's mark on alice's page").catch(() => false), "a green circled check beside bob's name on alice's page");
    report(await waitFor(() => verified(B, 'Alice'), "alice's mark on bob's page").catch(() => false), "a green circled check beside alice's name on bob's page");
    await A.b.shot(`${HOME}/shot-codes-verified-alice.png`);

    step("bob's comparison reaches his laptop");
    const start = Date.now();
    const reached = await waitFor(() => {
        const state = JSON.parse(lib('api', 'laptop', 'GET', `/api/dens/${den}/state`));
        return state.identities?.[aliceID]?.verified;
    }, "alice verified on bob's laptop", 90000).catch(() => false);
    report(reached, `alice shows verified on bob's laptop ${Math.round((Date.now() - start) / 1000)} seconds later, within the shortened hour`);

    step('bob starts over: his keys change, and the DM holds on alice\'s page');
    lib('api', 'bob', 'POST', `/api/dens/${den}/seal/start-over`, '{"password":"bob password"}');
    const line = await waitFor(() => A.b.eval(`${LINE}?.textContent || ''`), "the line where bob's keys changed").catch(() => '');
    report(/Bob's keys changed/.test(line) && line.includes('Compare'), `alice's DM shows where bob's keys changed, with Compare: ${q(line)}`);
    report(await waitFor(async () => !(await verified(A, 'Bob')), "bob's mark gone").catch(() => false), "bob's mark is gone from alice's page");
    const held = await waitFor(() => A.b.eval(`!!${HOLD} && !${COMPOSER}`), 'the hold').catch(() => false);
    const hold = await A.b.eval(`${HOLD}?.textContent || ''`);
    report(held && /keys changed since you compared/.test(hold) && hold.includes('Compare') && hold.includes('Send anyway'),
        `alice's DM holds, Compare and Send anyway in its composer's place: ${q(hold.slice(0, 90))}`);
    await A.b.shot(`${HOME}/shot-codes-hold-alice.png`);

    step('alice sends anyway, and bob reads it');
    await A.b.eval(`${button(HOLD, 'Send anyway')}.click(), true`);
    report(await waitFor(() => A.b.eval(`!!${COMPOSER} && !${HOLD}`), 'the composer back').catch(() => false), 'Send anyway gives the DM its composer back');
    await sends(A, 'sent anyway');
    const got = await waitFor(async () => ((await text(B)).includes('sent anyway') ? true : null), "bob reading alice's message").catch(() => false);
    report(got, 'bob reads what alice sent under his new keys');

    step("alice compares bob's new code from the line");
    await B.b.go(`http://localhost:${B.port}/den/${den}/${dm}`);
    await waitFor(() => B.b.eval(`!!${LOCK}`), "bob's DM");
    await B.b.eval(`${LOCK}.click(), true`);
    const fresh = await own(B);
    report(fresh !== bobCode, `bob's code changed with his keys: ${fresh}`);
    await close(B);
    await A.b.eval(`${button(LINE, 'Compare')}.click(), true`);
    await own(A);
    said = await compares(A, fresh, /The codes match/);
    await close(A);
    report(/The codes match/.test(said) && (await waitFor(() => verified(A, 'Bob'), "bob's mark again").catch(() => false)),
        "comparing bob's new code marks him again");
} catch (e) {
    c.fail(e);
} finally {
    for (const s of [A, B]) await s.b?.close().catch(() => {});
    await sleep(100);
}
c.finish();
