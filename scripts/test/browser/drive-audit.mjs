// The audit check, of staff's tools, as alice in Chromium and carol, then
// bob, in the browser named: alice finds carol in Manage's Members by a
// few letters and promotes her from the row's menu, then removes dave
// from his, and deletes bob's message and the channel doomed; carol,
// a moderator now, reads each line in Manage's Audit log, with who did
// it and to whom and never the message's words, and finds dave there
// though he left; and bob, a member, has no Members or Audit log in his
// Manage, and the den refuses him the log.
//
//   run.sh audit chromium|firefox
import { ALICE, BOB, CAROL, HOME, MANAGE, checks, launch, lib, menuRow, openManage, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, DOOMED: doomed, WORDS: words } = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);

const alice = await launch('chromium', 9541);
let carol = null;
let bob = null;
const ROW = (name) => `${MANAGE}?.querySelector('[data-members] [data-member=${JSON.stringify(name)}]')`;
const FIND = `${MANAGE}?.querySelector('input[data-members-find]')`;
const LINES = (on) => on.eval(`[...document.querySelectorAll('[data-audit] [data-audit-line]')].map((l) => l.firstElementChild.firstElementChild.textContent)`);
// find types a few letters into Members' box, as keys would.
const find = async (on, letters) => {
    await on.eval(`(() => {
        const box = ${FIND};
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(box, ${JSON.stringify(letters)});
        box.dispatchEvent(new Event('input', { bubbles: true }));
        return true;
    })()`);
};
const rows = (on) => on.eval(`[...${MANAGE}.querySelectorAll('[data-members] [data-member]')].map((r) => r.dataset.member)`);

try {
    await pair(alice, ALICE);
    step("alice finds carol in Manage's Members and promotes her from the row's menu");
    await openManage(alice, 'Members');
    await waitFor(() => alice.eval(`!!${ROW('carol')}`), "carol's row");
    await find(alice, 'CAR');
    const found = await waitFor(async () => {
        const r = await rows(alice);
        return r.length === 1 && r;
    }, 'Members narrowed to carol');
    c.report(found[0] === 'carol', `a few letters narrow Members to carol (${JSON.stringify(found)})`);
    const seen = await alice.eval(`${ROW('carol')}.textContent`);
    c.report(/Member/.test(seen) && /\d{4}/.test(seen), `carol's row says her role, when she joined and when she was last seen: ${seen}`);
    await alice.eval(`${ROW('carol')}.querySelector('[data-member-menu]').click(), true`);
    await waitFor(() => alice.eval(`!!${menuRow('promote')}`), "the row's Promote to moderator");
    await alice.shot(`${HOME}/shot-audit-alice-menu.png`);
    await alice.eval(`${menuRow('promote')}.click(), true`);
    await waitFor(() => alice.eval(`${ROW('carol')}?.textContent.includes('Moderator')`), 'carol a moderator in Members');
    c.report(true, "alice promoted carol from her row's menu");

    step('alice removes dave from his row, after Escape leaves it once');
    await find(alice, 'dav');
    await waitFor(() => alice.eval(`!!${ROW('dave')}`), "dave's row");
    const removal = `document.querySelector('[role=dialog][aria-label="Remove @dave"]')`;
    const remove = `[...(${removal}?.querySelectorAll('button') || [])].find((b) => b.textContent.trim() === 'Remove')`;
    const openRemoval = async () => {
        await alice.eval(`${ROW('dave')}.querySelector('[data-member-menu]').click(), true`);
        await waitFor(() => alice.eval(`!!${menuRow('remove')}`), "the row's Remove from den…");
        await alice.eval(`${menuRow('remove')}.click(), true`);
        await waitFor(() => alice.eval(`!!${remove}`), 'the removal dialog');
    };
    await openRemoval();
    await alice.shot(`${HOME}/shot-audit-alice-removal.png`);
    const over = await alice.eval(`(() => {
        const r = ${remove}.getBoundingClientRect();
        return ${removal}.contains(document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2));
    })()`);
    c.report(over, 'the removal opens over Manage, its Remove reached');
    await alice.key('Escape');
    const left = await waitFor(async () => !(await alice.eval(`!!${removal}`)) && alice.eval(`!!${MANAGE}`), 'Escape closing the removal alone', 5000)
        .catch(() => false);
    c.report(left, `Escape closes the removal and leaves Manage open (Manage open: ${await alice.eval(`!!${MANAGE}`)})`);
    if (!(await alice.eval(`!!${MANAGE}`))) await openManage(alice, 'Members');
    if (!(await alice.eval(`!!${ROW('dave')}`))) await find(alice, 'dav');
    await openRemoval();
    await alice.eval(`${remove}.click(), true`);
    await waitFor(async () => !(await alice.eval(`!!${ROW('dave')}`)) && !(await alice.eval(`!!document.querySelector('[aria-label="Remove @dave"]')`)),
        'dave gone from Members');
    c.report(await alice.eval(`!!${MANAGE}`), 'dave is gone from Members, and Manage stays open');
    await alice.shot(`${HOME}/shot-audit-alice-members.png`);

    step("alice deletes bob's message and the channel doomed");
    lib('api', 'alice', 'DELETE', `/api/dens/${den}/messages/${words}`);
    lib('api', 'alice', 'DELETE', `/api/dens/${den}/channels/${doomed}`);

    step("carol reads each line in Manage's Audit log");
    carol = await launch(which, which === 'chromium' ? 9542 : 9543);
    await pair(carol, CAROL);
    await openManage(carol, 'Audit log');
    const want = ['@alice deleted #doomed', "@alice deleted a message of @bob's in #general", '@alice removed @dave', '@alice made @carol a moderator'];
    const lines = await waitFor(async () => {
        const l = await LINES(carol);
        return l.length >= 4 && l;
    }, 'the log\'s lines', 10000).catch(() => []);
    c.report(JSON.stringify(lines.slice(0, 4)) === JSON.stringify(want), `carol reads alice's acts, newest first: ${JSON.stringify(lines.slice(0, 4))}`);
    c.report(lines.includes('@alice made an invite'), 'the log holds the invites alice made as members joined');
    const text = await carol.eval(`${MANAGE}.innerText`);
    c.report(!text.includes('never in the log'), "the log holds nothing of bob's message");
    await carol.shot(`${HOME}/shot-audit-carol-log.png`);

    step('bob, a member, has neither section, and the den refuses him the log');
    await carol.close();
    carol = null;
    bob = await launch(which, which === 'chromium' ? 9544 : 9545);
    await pair(bob, BOB);
    await bob.eval(`[...document.querySelectorAll('button')].find((x) => x.textContent.trim() === 'Manage').click(), true`);
    const sections = await waitFor(async () => {
        const s = await bob.eval(`[...document.querySelectorAll('[role=dialog] nav button')].map((b) => b.textContent.trim())`);
        return s.length > 0 && s;
    }, "bob's Manage");
    c.report(!sections.includes('Members') && !sections.includes('Audit log'), `bob's Manage lists ${JSON.stringify(sections)}`);
    let refused = '';
    try {
        lib('api', 'bob', 'GET', `/api/dens/${den}/audit`);
    } catch (e) {
        refused = String(e.stdout || e.message);
    }
    c.report(/not allowed/.test(refused), `the den refuses bob the log: ${refused.trim().slice(0, 120)}`);
} catch (e) {
    c.fail(e);
} finally {
    await alice.close();
    await carol?.close();
    await bob?.close();
    c.finish();
}
