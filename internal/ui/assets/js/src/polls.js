// Polls (M15.4) on the page: the draft the composer writes, which goes
// with the message whose text is its question; and a poll as the member
// sees it, by its rules, which the den holds to: who voted for what shows
// never, once it ends, or with the totals; and the totals show as votes
// come in, or to each member once they've voted, whose vote is then final.
// The events that change a poll apply to it here, the totals and voters
// where they come, and the member's own votes. It's kept apart from the
// components so the page's tests check it without a browser.

import { whoReacted } from './reactions.js';

export const MIN_CHOICES = 2;
export const MAX_CHOICES = 10;
// MAX_CHOICE is a choice's length, in characters as the den counts them.
export const MAX_CHOICE = 80;

// DURATIONS are how long a poll runs, as the composer offers them, a day
// first chosen.
export const DURATIONS = [
    { hours: 1, label: '1 hour' },
    { hours: 4, label: '4 hours' },
    { hours: 8, label: '8 hours' },
    { hours: 24, label: '1 day' },
    { hours: 72, label: '3 days' },
    { hours: 168, label: '1 week' },
    { hours: 336, label: '2 weeks' },
];

// VOTERS are when who voted for what shows, as the composer asks whether
// voting is anonymous: yes, never shown; during the poll, shown once it
// ends; or no, shown with the totals. TOTALS are when the totals show.
export const VOTERS = [
    { value: 'never', label: 'Yes' },
    { value: 'end', label: 'During the poll' },
    { value: 'always', label: 'No' },
];
export const TOTALS = [
    { value: 'live', label: 'As votes come in' },
    { value: 'voted', label: 'After voting' },
];

/** newDraft is a poll as the composer starts one */
export function newDraft() {
    return { choices: ['', ''], multiple: false, hours: 24, show_voters: 'never', show_totals: 'live' };
}

// cleanChoice is a choice as it goes: its spaces folded and trimmed.
const cleanChoice = (c) => c.trim().replace(/\s+/g, ' ');

/** draftProblem says what keeps a draft from going with its question, or
    '' when nothing does: a question, two choices or more, each at most
    MAX_CHOICE characters, and no two alike */
export function draftProblem(draft, question) {
    if (!question.trim()) return 'Write the question in the message box.';
    const choices = draft.choices.map(cleanChoice).filter(Boolean);
    if (choices.length < MIN_CHOICES) return 'A poll needs two choices or more.';
    if (choices.some((c) => [...c].length > MAX_CHOICE)) return `A choice is at most ${MAX_CHOICE} characters.`;
    if (new Set(choices.map((c) => c.toLowerCase())).size < choices.length) return 'Two choices are the same.';
    return '';
}

/** pollRequest is what a draft sends: its choices, the empty ones left
    out, and its rules */
export function pollRequest(draft) {
    const req = { choices: draft.choices.map(cleanChoice).filter(Boolean), hours: draft.hours, show_voters: draft.show_voters, show_totals: draft.show_totals };
    if (draft.multiple) req.multiple = true;
    return req;
}

/** pollView is what a member sees of a poll at now: whether it ended, by
    the den's word or its time; whether they voted; whether the totals and
    who voted for what show; whether they may vote, or vote again, which
    they may until it ends where the totals show as votes come in; and
    whether their vote is final */
export function pollView(p, now) {
    const ended = !!p.ended || now >= p.ends_at;
    const voted = (p.mine || []).length > 0;
    const live = p.show_totals === 'live';
    return { ended, voted, totals: !!p.totals, voters: !!p.voters, canVote: !ended && (!voted || live), final: voted && !live };
}

/** shareOf is a choice's share of those who voted, in percent */
export function shareOf(p, i) {
    return p.totals && p.voted ? Math.round((p.totals[i] * 100) / p.voted) : 0;
}

/** votesLine says how many voted */
export function votesLine(p) {
    if (!p.voted) return 'No votes yet';
    return p.voted === 1 ? '1 vote' : `${p.voted} votes`;
}

/** timeLeft says how long a poll has left at now, or that it ended */
export function timeLeft(p, now) {
    if (p.ended || now >= p.ends_at) return 'Ended';
    const minutes = Math.ceil((p.ends_at - now) / 60000);
    if (minutes < 60) return minutes === 1 ? '1 minute left' : `${minutes} minutes left`;
    const hours = Math.floor(minutes / 60);
    if (hours < 48) return hours === 1 ? '1 hour left' : `${hours} hours left`;
    return `${Math.floor(hours / 24)} days left`;
}

/** rulesOf says a poll's rules in a few words */
export function rulesOf(p) {
    const voters = { never: 'Anonymous', end: 'Votes show when it ends', always: 'Votes show' }[p.show_voters] || '';
    return p.show_totals === 'voted' ? `${voters} · Results after you vote, votes final` : voters;
}

/** rulesTip says a poll's rules in a sentence or two, for its tip */
export function rulesTip(p) {
    const voters = {
        never: 'Nobody sees who voted for what, though the den keeps each vote to count it.',
        end: 'Who voted for what shows once the poll ends.',
        always: 'Everyone sees who voted for what.',
    }[p.show_voters] || '';
    const totals = p.show_totals === 'voted' ? 'Totals show once you vote, and your vote is final.' : 'Totals show as votes come in.';
    return `${voters} ${totals}${p.multiple ? ' You can pick more than one.' : ''}`;
}

/** whoVoted says who picked a choice, for its count's tip, You first */
export function whoVoted(p, i, me, nameOf) {
    const members = p.voters?.[i] || [];
    return members.length ? whoReacted(members, me, nameOf) : 'Nobody yet';
}

/** polled applies a poll's event to a message, and returns it changed,
    or the same message where the event changes nothing: an update's totals
    replace those held, with its voters, where it carries totals that fit
    the poll, and one to the member's own sessions carries their votes; an
    end brings the final totals, after which no update changes it */
export function polled(m, t, d) {
    const p = m.poll;
    if (!p || d.message_id !== m.id) return m;
    const fits = (totals) => Array.isArray(totals) && totals.length === p.choices.length;
    if (t === 'poll.updated') {
        if (p.ended) return m;
        const next = { ...p, voted: d.voted };
        if (fits(d.totals)) {
            next.totals = d.totals;
            next.voters = d.voters;
        }
        if (d.mine) next.mine = d.mine.length ? d.mine : undefined;
        return { ...m, poll: next };
    }
    if (t === 'poll.ended' && fits(d.totals)) {
        return { ...m, poll: { ...p, ended: true, ends_at: d.ends_at, voted: d.voted, totals: d.totals, voters: d.voters } };
    }
    return m;
}

/** withPoll applies a poll's event to the run's message it's on */
export function withPoll(list, t, d) {
    let changed = false;
    const messages = list.messages.map((m) => {
        if (m.id !== d.message_id) return m;
        const n = polled(m, t, d);
        changed ||= n !== m;
        return n;
    });
    return changed ? { ...list, messages } : list;
}

// POLLS are the events that change a poll.
export const POLLS = new Set(['poll.updated', 'poll.ended']);
