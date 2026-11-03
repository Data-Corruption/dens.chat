// Polls (M15.4), checked without a browser: the composer's draft and what
// it sends, how each rule shows a poll to a member who voted and one who
// didn't, before its end and after it, the events applied, a load's
// replay, and the words the poll says itself in.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { replayed } from '../assets/js/src/paging.js';
import {
    DURATIONS, draftProblem, newDraft, pollRequest, pollView, polled, rulesOf, rulesTip, shareOf, timeLeft, votesLine, whoVoted, withPoll,
} from '../assets/js/src/polls.js';

const poll = (rest = {}) => ({ choices: ['Tacos', 'Pho'], ends_at: 10_000_000, show_voters: 'never', show_totals: 'live', voted: 0, ...rest });
const message = (p) => ({ id: '5', channel_id: '2', author_id: '1', revision: 1, text: 'Lunch?', poll: p });

test('a draft starts with two empty choices, a day long, anonymous, its totals as votes come in', () => {
    const d = newDraft();
    assert.deepEqual(d, { choices: ['', ''], multiple: false, hours: 24, show_voters: 'never', show_totals: 'live' });
    assert.deepEqual(DURATIONS.map((x) => x.hours), [1, 4, 8, 24, 72, 168, 336]);
});

test("a draft goes with a question, two choices or more, none too long and no two alike, and sends what's filled in", () => {
    const d = { ...newDraft(), choices: ['  Tacos   al pastor ', '', 'Pho', ''] };
    assert.equal(draftProblem(d, ''), 'Write the question in the message box.');
    assert.equal(draftProblem(d, 'Lunch?'), '');
    assert.equal(draftProblem({ ...d, choices: ['Tacos', ' '] }, 'Lunch?'), 'A poll needs two choices or more.');
    assert.equal(draftProblem({ ...d, choices: ['Tacos', 'tacos '] }, 'Lunch?'), 'Two choices are the same.');
    assert.equal(draftProblem({ ...d, choices: ['a', 'é'.repeat(81)] }, 'Lunch?'), 'A choice is at most 80 characters.');
    assert.equal(draftProblem({ ...d, choices: ['a', '👩‍💻'.repeat(20)] }, 'Lunch?'), '', 'emoji count as the den counts them');
    assert.deepEqual(pollRequest(d), { choices: ['Tacos al pastor', 'Pho'], hours: 24, show_voters: 'never', show_totals: 'live' });
    assert.equal(pollRequest({ ...d, multiple: true }).multiple, true);
});

test('each rule shows the totals and voters as the den sends them, and says when a vote may change', () => {
    const now = 1000;
    const before = pollView(poll({ show_totals: 'voted' }), now);
    assert.deepEqual(before, { ended: false, voted: false, totals: false, voters: false, canVote: true, final: false });
    const voted = pollView(poll({ show_totals: 'voted', mine: [1], totals: [0, 1], voted: 1 }), now);
    assert.deepEqual(voted, { ended: false, voted: true, totals: true, voters: false, canVote: false, final: true }, 'after voting, a vote is final');
    const live = pollView(poll({ mine: [0], totals: [1, 0], voted: 1, voters: [['3'], []], show_voters: 'always' }), now);
    assert.deepEqual(live, { ended: false, voted: true, totals: true, voters: true, canVote: true, final: false }, 'as votes come in, it changes');
    assert.equal(pollView(poll(), 10_000_000).ended, true, 'past its time, it reads as ended before the den says so');
    assert.equal(pollView(poll({ ended: true, ends_at: 99_000_000 }), now).canVote, false);
});

test('a choice shows its share of those who voted, and the poll how many did, how long it has left and its rules', () => {
    const p = poll({ totals: [2, 1], voted: 3, multiple: true });
    assert.deepEqual([shareOf(p, 0), shareOf(p, 1)], [67, 33]);
    assert.equal(shareOf(poll(), 0), 0);
    assert.deepEqual([votesLine(poll()), votesLine(poll({ voted: 1 })), votesLine(p)], ['No votes yet', '1 vote', '3 votes']);
    const end = 100 * 3600_000;
    const left = (ms) => timeLeft({ ends_at: end }, end - ms);
    assert.deepEqual([left(30_000), left(59 * 60_000), left(90 * 60_000), left(47 * 3600_000), left(73 * 3600_000), left(0)],
        ['1 minute left', '59 minutes left', '1 hour left', '47 hours left', '3 days left', 'Ended']);
    assert.equal(timeLeft({ ended: true, ends_at: end }, 0), 'Ended');
    assert.deepEqual([rulesOf(poll()), rulesOf(poll({ show_voters: 'end' })), rulesOf(poll({ show_voters: 'always', show_totals: 'voted' }))],
        ['Anonymous', 'Votes show when it ends', 'Votes show · Results after you vote, votes final']);
    assert.equal(rulesTip(poll()), 'Nobody sees who voted for what, though the den keeps each vote to count it. Totals show as votes come in.');
    assert.match(rulesTip(poll({ show_totals: 'voted', multiple: true })), /final.*more than one/);
});

test("a choice's voters, where they show, say who: You first", () => {
    const p = poll({ show_voters: 'always', totals: [2, 0], voted: 2, voters: [['4', '3'], []] });
    const names = { 3: 'Bob', 4: 'Carol' };
    assert.equal(whoVoted(p, 0, '3', (id) => names[id]), 'You and Carol');
    assert.equal(whoVoted(p, 1, '3', (id) => names[id]), 'Nobody yet');
});

test("a poll's events apply to it: totals that fit with their voters, the member's own votes, and the end, after which nothing changes", () => {
    let m = message(poll({ show_totals: 'voted', show_voters: 'always' }));
    m = polled(m, 'poll.updated', { message_id: '5', voted: 2 });
    assert.equal(m.poll.voted, 2);
    assert.equal(m.poll.totals, undefined, "how many voted, to one who hasn't");
    assert.equal(polled(m, 'poll.updated', { message_id: '6', voted: 9 }), m);
    m = polled(m, 'poll.updated', { message_id: '5', voted: 3, totals: [2, 1], voters: [['3', '4'], ['5']], mine: [1] });
    assert.deepEqual([m.poll.totals, m.poll.voters, m.poll.mine], [[2, 1], [['3', '4'], ['5']], [1]]);
    m = polled(m, 'poll.updated', { message_id: '5', voted: 4 });
    assert.deepEqual([m.poll.voted, m.poll.totals, m.poll.mine], [4, [2, 1], [1]], 'one without totals leaves them');
    assert.deepEqual(polled(m, 'poll.updated', { message_id: '5', voted: 4, totals: [1, 1, 1] }).poll.totals, [2, 1], "totals that don't fit go");
    m = polled(m, 'poll.updated', { message_id: '5', voted: 3, totals: [2, 0], voters: [['3', '4'], []], mine: [] });
    assert.equal(m.poll.mine, undefined, 'a vote taken back');
    m = polled(m, 'poll.ended', { message_id: '5', ends_at: 5000, voted: 3, totals: [3, 0], voters: [['3', '4', '5'], []] });
    assert.deepEqual([m.poll.ended, m.poll.ends_at, m.poll.totals], [true, 5000, [3, 0]]);
    assert.equal(polled(m, 'poll.updated', { message_id: '5', voted: 9, totals: [9, 0] }), m, 'nothing changes an ended poll');
    assert.equal(polled(message(undefined), 'poll.ended', { message_id: '5', ends_at: 1, voted: 0, totals: [0, 0] }).poll, undefined);
});

test("a load takes again the poll's events that came while it was out, the newest last", () => {
    const list = { messages: [message(poll({ voted: 1, totals: [1, 0] }))], hasOlder: false, hasNewer: false, loaded: true };
    const events = [
        { t: 'poll.updated', d: { message_id: '5', voted: 1, totals: [1, 0] } },
        { t: 'poll.updated', d: { message_id: '5', voted: 2, totals: [1, 1] } },
    ];
    assert.deepEqual(replayed(list, events).messages[0].poll.totals, [1, 1]);
    assert.equal(withPoll(list, 'poll.updated', { message_id: '9', voted: 3 }), list, 'an event for none of the run changes nothing');
});
