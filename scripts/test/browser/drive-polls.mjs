// The M15.4 check of polls: alice, who owns the den, in Chromium, and bob
// in the browser named. Alice writes a poll in the composer, its +'s Poll,
// with three choices, its totals after voting and who voted for what once
// it ends; both pages show it with its rules. Bob votes from his page and
// sees the totals, his vote final, while alice's page shows only how many
// voted, until she votes; she ends it early, and both pages show who voted
// for what, in a count's tip. A poll of several choices shows its totals
// and voters as votes come in, and bob changes his vote and takes it back.
// Then each pair of rules, in a poll of its own: what each page receives
// of it, every frame of its stream and every answer, holds the totals and
// who voted for what only once its rules allow, before the end and after.
//
//   run.sh polls chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, ALICE_ID: aliceID, BOB_ID: bobID } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;
const q = (s) => JSON.stringify(s);
const POLL = (id) => `document.querySelector('[data-poll="${id}"]')`;
const COMPOSER = `document.querySelector('[data-scroller] ~ div textarea')`;

// RECORD keeps what the page receives from its service: each frame of its
// stream, and each answer about messages and polls.
const RECORD = `() => {
    window.__got = [];
    const Real = window.WebSocket;
    window.WebSocket = class extends Real {
        constructor(...args) {
            super(...args);
            this.addEventListener('message', (e) => typeof e.data === 'string' && window.__got.push(e.data));
        }
    };
    const fetched = window.fetch;
    window.fetch = async (...args) => {
        const r = await fetched(...args);
        const url = String(args[0]?.url || args[0]);
        if (url.includes('/messages') || url.includes('/poll/')) r.clone().text().then((t) => window.__got.push(t), () => {});
        return r;
    };
}`;

// seen lists what a page received of a poll, in order: each message of
// it, and each event about it, with its tally as it came.
const seen = (s, id) => s.b.eval(`(() => {
    const out = [];
    const visit = (v) => {
        if (!v || typeof v !== 'object') return;
        if (Array.isArray(v)) return v.forEach(visit);
        const p = v.id === ${q(id)} && v.poll ? v.poll : v.message_id === ${q(id)} && 'voted' in v ? v : null;
        if (p) out.push({ voted: p.voted, totals: !!p.totals, voters: !!p.voters, ended: !!p.ended || (v.message_id && 'ends_at' in v) });
        for (const k of Object.keys(v)) visit(v[k]);
    };
    for (const raw of window.__got) {
        try { visit(JSON.parse(raw)); } catch {}
    }
    return JSON.stringify(out);
})()`).then(JSON.parse);

// shows says what a page shows of a poll: each choice's count where the
// totals show, the member's own, how many voted, its time, its rules,
// whether it ended, its buttons and each choice's voters where they show.
const shows = (s, id) => s.b.eval(`(() => { const p = ${POLL(id)}; if (!p) return null;
    const choices = [...p.querySelectorAll('[data-choice]')];
    return JSON.stringify({ counts: choices.map((x) => x.dataset.count ?? null), mine: choices.filter((x) => x.hasAttribute('data-mine')).map((x) => Number(x.dataset.choice)),
        votes: Number(p.querySelector('[data-votes]').dataset.votes), left: p.querySelector('[data-left]').textContent,
        rules: p.querySelector('[data-rules]').textContent, ended: p.hasAttribute('data-ended'), text: p.textContent,
        buttons: [...p.querySelectorAll('button')].map((b) => b.textContent.trim()).filter((t) => t && !t.includes('%')),
        voters: [...p.querySelectorAll('[data-choice]')].map((x) => x.querySelector('[data-voters]')?.dataset.voters ?? null) }); })()`)
    .then((v) => (v ? JSON.parse(v) : null));

// until waits for a page to show a poll as want says of it.
function until(s, id, want, what) {
    return waitFor(async () => {
        const v = await shows(s, id);
        return v && want(v) ? v : null;
    }, `${what} on ${s.name}'s page`, 15000).catch(() => shows(s, id));
}

// at is where on the page an element's middle is, scrolled into view.
const at = async (s, expr) => JSON.parse(await s.b.eval(`(() => { const e = ${expr}; e.scrollIntoView({ block: 'nearest' });
    const r = e.getBoundingClientRect(); return JSON.stringify([r.left + r.width / 2, r.top + r.height / 2]); })()`));

async function clickOn(s, expr) {
    await waitFor(() => s.b.eval(`!!(${expr})`), expr.slice(0, 80));
    const [x, y] = await at(s, expr);
    await s.b.click(x, y);
}

const button = (scope, label) => `[...${scope}.querySelectorAll('button')].find((b) => b.textContent.trim() === ${q(label)})`;
const pick = (s, id, i) => clickOn(s, `${POLL(id)}.querySelector('[data-choice="${i}"] input')`);

// setValue sets a field's value as typing would, for Preact to hear.
const setValue = (field, value) => `(() => { const el = ${field}; const set = Object.getOwnPropertyDescriptor(el.constructor.prototype, 'value').set;
    set.call(el, ${q(value)}); el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true })); return true; })()`;

// tip says what a choice's count's tip says, once the pointer is over it.
async function tip(s, id, i) {
    const count = `${POLL(id)}.querySelector('[data-choice="${i}"] [data-voters]')`;
    const [x, y] = await at(s, count);
    await s.b.move(x, y);
    const text = await waitFor(() => s.b.eval(`(() => { const t = ${count}.parentElement.querySelector('[role=tooltip]');
        return t && !t.hidden ? t.textContent : ''; })()`), `choice ${i}'s tip`, 5000).catch(() => '');
    await s.b.move(5, 5);
    return text;
}

// newest is the channel's newest message, as the den has it.
const newest = (s) => s.b.eval(`fetch('/api/dens/${den}/channels/${general}/messages?limit=1').then((r) => r.json()).then((p) => p.messages.at(-1) || null)`);

// postPoll posts a poll as alice through her service, and returns its ID.
const postPoll = (text, poll) => JSON.parse(lib('api', 'alice', 'POST', `/api/dens/${den}/channels/${general}/messages`,
    JSON.stringify({ nonce: lib('nonce'), text, poll }))).id;

// vote and end ask a member's service as their page does, and return the
// answer's status.
const post = (s, path, body) => s.b.eval(`fetch('/api/dens/${den}/messages/${path}', { method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: ${q(JSON.stringify(body))} }).then((r) => r.status)`);
const vote = (s, id, choices) => post(s, `${id}/poll/vote`, { choices });
const end = (s, id) => post(s, `${id}/poll/end`, {});

try {
    A.b = await launch('chromium', 9601, { hover: true, preload: RECORD });
    B.b = await launch(which, which === 'chromium' ? 9602 : 9603, { hover: true, preload: RECORD });
    for (const s of [A, B]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}/${general}`);
        await waitFor(() => s.b.eval(`!!${COMPOSER}`), `${s.name}'s #general`);
    }

    step('alice writes a poll in the composer');
    await clickOn(A, `document.querySelector('button[aria-label="Add to this message"]')`);
    await clickOn(A, `[...document.querySelectorAll('[role=menuitem]')].find((x) => x.textContent.trim() === 'Poll')`);
    const EDITOR = `document.querySelector('[data-poll-editor]')`;
    await waitFor(() => A.b.eval(`!!${EDITOR}`), 'the poll editor');
    await A.b.eval(setValue(`${EDITOR}.querySelector('[data-choice-input="0"]')`, 'Tacos'));
    await A.b.eval(setValue(`${EDITOR}.querySelector('[data-choice-input="1"]')`, 'Pho'));
    await clickOn(A, button(EDITOR, '+ Add a choice'));
    await waitFor(() => A.b.eval(`!!${EDITOR}.querySelector('[data-choice-input="2"]')`), 'a third choice');
    await A.b.eval(setValue(`${EDITOR}.querySelector('[data-choice-input="2"]')`, 'Pizza'));
    // A select by its label: Runs for, Totals show, Anonymous voting.
    const select = (label) => `[...${EDITOR}.querySelectorAll('label')].find((l) => l.firstElementChild?.textContent === ${q(label)}).querySelector('select')`;
    await A.b.eval(setValue(select('Anonymous voting'), 'end'));
    await A.b.eval(setValue(select('Totals show'), 'voted'));
    const placeholder = await A.b.eval(`${COMPOSER}.placeholder`);
    await A.b.eval(`${COMPOSER}.focus(), true`);
    await A.b.type('Lunch?');
    await A.b.shot(`${HOME}/shot-polls-editor-${which}.png`);
    await A.b.key('Enter');
    const posted = await waitFor(async () => {
        const m = await newest(A);
        return m?.poll ? m : null;
    }, "alice's poll at the den");
    const p1 = posted.id;
    report(posted.text === 'Lunch?' && q(posted.poll.choices) === q(['Tacos', 'Pho', 'Pizza']) && posted.poll.show_voters === 'end' &&
        posted.poll.show_totals === 'voted' && posted.poll.ends_at - posted.created_at === 24 * 3600_000 && placeholder === 'Ask your question',
    `the composer posted the poll as written, a day long: ${q({ text: posted.text, poll: posted.poll, placeholder })}`);
    const fresh = (v) => v.votes === 0 && v.counts.every((x) => x === null) && v.rules === 'Votes show when it ends · Results after you vote, votes final';
    const [freshA, freshB] = [await until(A, p1, fresh, 'the new poll'), await until(B, p1, fresh, 'the new poll')];
    report(freshA && fresh(freshA) && freshB && fresh(freshB) && freshB.left === '24 hours left' && freshA.buttons.includes('End now') && !freshB.buttons.includes('End now'),
        `both pages show it, with its rules and no totals, alice's with End now: ${q({ alice: freshA, bob: freshB })}`);

    step('bob votes, sees the totals, and his vote is final');
    await pick(B, p1, 1);
    await clickOn(B, button(POLL(p1), 'Vote'));
    const bobVoted = await until(B, p1, (v) => q(v.counts) === q(['0', '1', '0']), "bob's vote");
    report(bobVoted && q(bobVoted.mine) === q([1]) && bobVoted.text.includes('Your vote is final.') && !bobVoted.buttons.includes('Change vote'),
        `bob's page shows the totals once he voted, his own marked, his vote final: ${q(bobVoted)}`);
    await B.b.shot(`${HOME}/shot-polls-voted-${which}.png`);
    const aliceCount = await until(A, p1, (v) => v.votes === 1, 'one vote');
    report(aliceCount?.votes === 1 && aliceCount.counts.every((x) => x === null), `alice's page shows how many voted and no totals: ${q(aliceCount)}`);
    const changed = await vote(B, p1, [0]);
    report(changed >= 400, `the den refused bob's change of a final vote: ${changed}`);
    let got = { A: await seen(A, p1), B: await seen(B, p1) };
    report(got.A.length > 0 && !got.A.some((x) => x.totals || x.voters) && !got.B.some((x) => x.voters),
        `alice's page received no totals before she voted, and neither page any voters: ${q(got)}`);

    step('alice votes, and sees the totals');
    await pick(A, p1, 0);
    await clickOn(A, button(POLL(p1), 'Vote'));
    const aliceVoted = await until(A, p1, (v) => q(v.counts) === q(['1', '1', '0']), "alice's vote");
    const bobTwo = await until(B, p1, (v) => q(v.counts) === q(['1', '1', '0']), "alice's vote in the totals");
    report(aliceVoted && q(aliceVoted.mine) === q([0]) && bobTwo?.votes === 2, `both pages show the totals: ${q({ alice: aliceVoted, bob: bobTwo })}`);

    step('alice ends it early, and both pages show who voted for what');
    await clickOn(A, button(POLL(p1), 'End now'));
    await clickOn(A, button(POLL(p1), 'End poll'));
    const ended = (v) => v.ended && v.left === 'Ended' && q(v.voters) === q([aliceID, bobID, '']);
    const [endA, endB] = [await until(A, p1, ended, 'the end'), await until(B, p1, ended, 'the end')];
    report(endA && ended(endA) && endB && ended(endB) && !endA.buttons.includes('End now'), `both pages show it ended, with its voters: ${q({ alice: endA, bob: endB })}`);
    const [tacos, pho] = [await tip(B, p1, 0), await tip(B, p1, 1)];
    report(tacos === 'Alice' && pho === 'You', `a count's tip says who voted for it: ${q([tacos, pho])}`);
    await B.b.shot(`${HOME}/shot-polls-ended-${which}.png`);

    step('a poll of several choices, its totals and voters as votes come in');
    const p2 = postPoll('Which days?', { choices: ['Mon', 'Tue', 'Wed'], multiple: true, hours: 4, show_voters: 'always', show_totals: 'live' });
    await until(B, p2, (v) => v.votes === 0, 'the second poll');
    await pick(B, p2, 0);
    await pick(B, p2, 2);
    await clickOn(B, button(POLL(p2), 'Vote'));
    const two = await until(A, p2, (v) => q(v.counts) === q(['1', '0', '1']), "bob's two choices");
    report(two && q(two.voters) === q([bobID, '', bobID]) && two.rules === 'Votes show', `alice's page shows bob's two choices as they come, with his name: ${q(two)}`);
    await clickOn(B, button(POLL(p2), 'Change vote'));
    await pick(B, p2, 0);
    await pick(B, p2, 2);
    await pick(B, p2, 1);
    await clickOn(B, button(POLL(p2), 'Vote'));
    const moved = await until(A, p2, (v) => q(v.counts) === q(['0', '1', '0']), "bob's changed vote");
    report(moved && q(moved.voters) === q(['', bobID, '']), `bob changed his vote, which alice's page shows: ${q(moved)}`);
    await clickOn(B, button(POLL(p2), 'Remove vote'));
    const none = await until(A, p2, (v) => v.votes === 0, "bob's vote taken back");
    const bobNone = await until(B, p2, (v) => v.mine.length === 0 && v.buttons.includes('Vote'), 'his vote gone');
    report(none?.votes === 0 && q(none.counts) === q(['0', '0', '0']) && bobNone?.buttons.includes('Vote'),
        `bob took his vote back, and both pages show it: ${q({ alice: none, bob: bobNone })}`);

    step('each pair of rules, by what each page receives');
    for (const voters of ['never', 'end', 'always']) {
        for (const totals of ['live', 'voted']) {
            const rule = `${voters}/${totals}`;
            const id = postPoll(`Rules ${rule}?`, { choices: ['yes', 'no'], hours: 1, show_voters: voters, show_totals: totals });
            for (const s of [A, B]) await until(s, id, (v) => v.votes === 0, `the ${rule} poll`);
            const mark = { A: (await seen(A, id)).length, B: (await seen(B, id)).length };
            if ((await vote(B, id, [0])) !== 200) report(false, `${rule}: bob's vote`);
            await waitFor(async () => (await seen(A, id)).some((x) => x.voted === 1), `${rule}: bob's vote reaching alice`, 15000).catch(() => {});
            got = { A: (await seen(A, id)).slice(mark.A), B: (await seen(B, id)).slice(mark.B) };
            const live = totals === 'live';
            const always = voters === 'always';
            report(got.A.length > 0 && got.A.every((x) => x.totals === live && x.voters === (always && live)) &&
                got.B.length > 0 && got.B.every((x) => x.totals && x.voters === always),
            `${rule}, bob voted: alice's page received the totals ${live ? 'as votes came' : 'not at all'}, and the voters ${always && live ? 'with them' : 'not at all'}; bob's both where allowed: ${q(got)}`);
            mark.A += got.A.length;
            mark.B += got.B.length;
            if ((await vote(A, id, [1])) !== 200) report(false, `${rule}: alice's vote`);
            await waitFor(async () => (await seen(B, id)).some((x) => x.voted === 2), `${rule}: alice's vote reaching bob`, 15000).catch(() => {});
            got = { A: (await seen(A, id)).slice(mark.A), B: (await seen(B, id)).slice(mark.B) };
            report(got.A.length > 0 && [...got.A, ...got.B].every((x) => x.totals && x.voters === always),
                `${rule}, both voted: each page received the totals, and the voters ${always ? 'with them' : 'not yet'}: ${q(got)}`);
            mark.A += got.A.length;
            mark.B += got.B.length;
            if ((await end(A, id)) !== 200) report(false, `${rule}: alice's end`);
            for (const s of [A, B]) await waitFor(async () => (await seen(s, id)).some((x) => x.ended), `${rule}: the end on ${s.name}'s page`, 15000).catch(() => {});
            got = { A: (await seen(A, id)).slice(mark.A), B: (await seen(B, id)).slice(mark.B) };
            const ends = [...got.A, ...got.B].filter((x) => x.ended);
            report(ends.length >= 2 && ends.every((x) => x.totals && x.voted === 2 && x.voters === (voters !== 'never')),
                `${rule}, ended: each page received the final totals, and the voters ${voters === 'never' ? 'never' : 'now'}: ${q(got)}`);
        }
    }
    await sleep(200);
    await A.b.shot(`${HOME}/shot-polls-rules-${which}.png`);
} catch (e) {
    c.fail(e);
    for (const s of [A, B]) await s.b?.shot(`${HOME}/shot-polls-failed-${s.name}-${which}.png`).catch(() => {});
} finally {
    for (const s of [A, B]) await s.b?.close();
}
c.finish();
