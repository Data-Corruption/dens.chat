// Events (M17.4), checked without a browser: an event's times in the
// member's time zone, across zones and the clocks' changes, as the form
// takes them in and the pane shows them; the form's limits and what it
// sends; who may make and change one; the den's events applied to the
// view; the Events row's mark; the owner's limit in Manage; and where the
// page opens one.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import {
    EVENTS, LENGTHS, MAX_EVENTS, atZone, canMake, current, eventRequest, formOf, formProblem, inputsAt, interestedText, lengthsFor, mayChange,
    newForm, ordered, rowMark, whenText, withEvent, zoneParts,
} from '../assets/js/src/denevents.js';
import { EventsRow } from '../assets/js/src/denevents.jsx';
import { changesOf, draftOf } from '../assets/js/src/dendraft.js';
import { fileWhere } from '../assets/js/src/files.jsx';
import { chatRoute } from '../assets/js/src/nav.js';
import { notifying } from '../assets/js/src/notify.js';

function find(node, match) {
    if (match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}

const marked = (attr) => (n) => n.attributes?.[attr] !== undefined;

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
// Friday 6 November 2026, 18:30 UTC.
const NOW = Date.UTC(2026, 10, 6, 18, 30);

test("a moment reads as each zone's date and time of day, and the form's fields name the same moment back", () => {
    for (const [tz, date, time] of [
        ['UTC', '2026-11-06', '18:30'],
        ['America/New_York', '2026-11-06', '13:30'],
        ['America/St_Johns', '2026-11-06', '15:00'],
        ['Asia/Kolkata', '2026-11-07', '00:00'],
        ['Pacific/Chatham', '2026-11-07', '08:15'],
        ['Pacific/Kiritimati', '2026-11-07', '08:30'],
        ['Pacific/Pago_Pago', '2026-11-06', '07:30'],
    ]) {
        assert.deepEqual(inputsAt(NOW, tz), { date, time }, tz);
        assert.equal(atZone(date, time, tz), NOW, `${tz} back`);
    }
    assert.deepEqual(zoneParts(NOW, 'Asia/Kolkata'), { year: 2026, month: 11, day: 7, hour: 0, minute: 0 });
    assert.ok(Number.isNaN(atZone('2026-11-6', '18:30', 'UTC')), 'a date not in the field’s form');
    assert.ok(Number.isNaN(atZone('2026-11-06', '', 'UTC')), 'no time');
});

test('across a change of the clocks, a skipped time lands past the change, and a time passed twice is the first', () => {
    const ny = 'America/New_York';
    // 8 March 2026: 02:00 EST jumps to 03:00 EDT, so 02:30 never shows.
    const skipped = atZone('2026-03-08', '02:30', ny);
    assert.deepEqual(inputsAt(skipped, ny), { date: '2026-03-08', time: '03:30' });
    assert.equal(atZone('2026-03-08', '03:30', ny), Date.UTC(2026, 2, 8, 7, 30));
    // 1 November 2026: 02:00 EDT goes back to 01:00 EST, so 01:30 shows
    // twice; the first is EDT's.
    assert.equal(atZone('2026-11-01', '01:30', ny), Date.UTC(2026, 10, 1, 5, 30));
    // The day after, a day's length on: EST's 13:30 is 18:30 UTC.
    assert.equal(atZone('2026-11-02', '13:30', ny), Date.UTC(2026, 10, 2, 18, 30));
});

test("when says the event's day, Today or Tomorrow, its time and its end, in the member's zone, and Live until its end", () => {
    const at = (h, m = 0) => Date.UTC(2026, 10, 6, h, m);
    const e = (starts, ends, live) => ({ starts_at: starts, ends_at: ends, live });
    assert.equal(whenText(e(at(20), at(22)), NOW, 'UTC', 'en-US'), 'Today, 8:00 PM – 10:00 PM');
    assert.equal(whenText(e(at(20), at(22)), NOW, 'America/New_York', 'en-US'), 'Today, 3:00 PM – 5:00 PM');
    // In Kolkata it's past midnight already, so it's today there too.
    assert.equal(whenText(e(at(20), at(22)), NOW, 'Asia/Kolkata', 'en-US'), 'Today, 1:30 AM – 3:30 AM');
    assert.equal(whenText(e(at(20) + DAY, at(22) + DAY), NOW, 'UTC', 'en-US'), 'Tomorrow, 8:00 PM – 10:00 PM');
    assert.equal(whenText(e(at(20) + 3 * DAY, at(20) + 4 * DAY), NOW, 'UTC', 'en-US'), 'Mon, Nov 9, 8:00 PM – Tue, Nov 10, 8:00 PM');
    assert.equal(whenText(e(at(20) + 60 * DAY, at(21) + 60 * DAY), NOW, 'UTC', 'en-US'), 'Tue, Jan 5, 2027, 8:00 PM – 9:00 PM');
    assert.equal(whenText(e(at(18), at(21), true), NOW, 'UTC', 'en-US'), 'Live until 9:00 PM');
    assert.equal(whenText(e(at(18), at(21) + DAY, true), NOW, 'UTC', 'en-US'), 'Live until Tomorrow, 9:00 PM');
    assert.equal(whenText(e(at(20), at(22)), NOW, 'UTC', 'en-GB'), 'Today, 20:00 – 22:00');
});

test('a new form starts on the next whole hour after an hour from now, for an hour, in the first voice channel', () => {
    assert.deepEqual(newForm(NOW, 'UTC', '7'), { name: '', description: '', date: '2026-11-06', time: '20:00', minutes: 60, where: 'channel', channel: '7', place: '' });
    assert.equal(newForm(NOW, 'Asia/Kolkata', '').time, '01:30');
    assert.equal(newForm(NOW, 'UTC', '').where, 'place', 'with no voice channel, somewhere else');
    const e = { name: 'Quiz', starts_at: Date.UTC(2026, 10, 7, 19), ends_at: Date.UTC(2026, 10, 7, 21, 30), place: 'Pub' };
    assert.deepEqual(formOf(e, 'UTC'), { name: 'Quiz', description: '', date: '2026-11-07', time: '19:00', minutes: 150, where: 'place', channel: '', place: 'Pub' });
});

test("the form keeps to the den's limits: a name, a place, a start from now to a year on, and 5 minutes to a week", () => {
    const f = (rest) => ({ ...newForm(NOW, 'UTC', '7'), name: 'Game night', ...rest });
    const problem = (rest, e) => formProblem(f(rest), NOW, 'UTC', e);
    assert.equal(problem({}), '');
    assert.equal(problem({ name: '  ' }), 'An event needs a name.');
    assert.equal(problem({ name: 'x'.repeat(101) }), 'A name is at most 100 characters.');
    assert.equal(problem({ name: '🧑‍🚀'.repeat(25) }), '', 'emoji count as the den counts them');
    assert.equal(problem({ description: 'é'.repeat(2001) }), 'A description is at most 2,000 characters.');
    assert.equal(problem({ where: 'channel', channel: '' }), 'Pick a voice channel.');
    assert.equal(problem({ where: 'place', place: ' ' }), 'Say where it is.');
    assert.equal(problem({ where: 'place', place: 'x'.repeat(101) }), 'A place is at most 100 characters.');
    assert.equal(problem({ minutes: 4 }), 'An event lasts 5 minutes to a week.');
    assert.equal(problem({ minutes: 7 * 24 * 60 + 1 }), 'An event lasts 5 minutes to a week.');
    assert.equal(problem({ date: '2026-11-06', time: '18:00' }), 'That time has passed.');
    assert.equal(problem({ date: '2026-11-06', time: '18:30' }), '', 'this minute starts it now');
    assert.equal(problem({ date: '2027-11-07', time: '18:30' }), 'An event starts within a year.');
    assert.equal(problem({ date: '' }), 'Pick a day and a time.');
    // A live event keeps its start, and its end comes after now.
    const live = { live: true, starts_at: NOW - 2 * HOUR, ends_at: NOW + HOUR };
    assert.equal(problem({ minutes: 60, date: '2020-01-01' }, live), 'It would have ended already: End now ends it.');
    assert.equal(problem({ minutes: 180 }, live), '');
});

test('the form sends a new event whole, and of one it changes only what differs; a live one keeps its start', () => {
    const f = { ...newForm(NOW, 'UTC', '7'), name: ' Game night ', description: ' bring *snacks* ' };
    const starts = Date.UTC(2026, 10, 6, 20);
    assert.deepEqual(eventRequest(f, 'UTC'), { name: 'Game night', description: 'bring *snacks*', starts_at: starts, ends_at: starts + HOUR, channel_id: '7' });
    const e = { id: '4', name: 'Game night', description: 'bring *snacks*', starts_at: starts, ends_at: starts + HOUR, channel_id: '7' };
    assert.deepEqual(eventRequest(formOf(e, 'UTC'), 'UTC', e), {});
    assert.deepEqual(eventRequest({ ...formOf(e, 'UTC'), where: 'place', place: 'Pub' }, 'UTC', e), { place: 'Pub' });
    assert.deepEqual(eventRequest({ ...formOf(e, 'UTC'), minutes: 120, time: '21:00' }, 'UTC', e), { starts_at: starts + HOUR, ends_at: starts + 3 * HOUR });
    const live = { ...e, live: true };
    assert.deepEqual(eventRequest({ ...formOf(live, 'UTC'), minutes: 120, time: '23:00' }, 'UTC', live), { ends_at: starts + 2 * HOUR });
});

test('a live event is offered the lengths that end after now, and its own among them', () => {
    const live = { live: true, starts_at: NOW - 100 * MINUTE, ends_at: NOW + 50 * MINUTE };
    const offered = lengthsFor(live, NOW).map(([m]) => m);
    assert.ok(offered.every((m) => m > 100), `${offered}`);
    assert.ok(offered.includes(150), 'its own length, 2½ hours');
    assert.equal(lengthsFor(live, NOW).find(([m]) => m === 150)[1], '150 minutes');
    assert.deepEqual(lengthsFor(null, NOW), LENGTHS);
    assert.deepEqual(LENGTHS.map(([m]) => m).filter((m) => m < 5 || m > 7 * 24 * 60), []);
});

test('staff make events while the den holds fewer than 50, and a member as many as the owner lets each', () => {
    const view = (role, limit, events = []) => ({ role, me: { id: '2' }, event_limits: limit === null ? undefined : { member_events: limit }, events });
    const mine = (n) => Array.from({ length: n }, (_, i) => ({ id: String(i + 1), made_by: '2' }));
    assert.equal(canMake(view('owner', 0)), true);
    assert.equal(canMake(view('moderator', 0)), true);
    assert.equal(canMake(view('member', 0)), false);
    assert.equal(canMake(view('member', 2, mine(1))), true);
    assert.equal(canMake(view('member', 2, mine(2))), false);
    assert.equal(canMake(view('owner', 0, Array.from({ length: MAX_EVENTS }, (_, i) => ({ id: String(i + 1), made_by: '9' })))), false);
    assert.equal(canMake(view('owner', null)), false, 'a den older than events has none');
});

test('an event is changed by its maker, and by staff above them', () => {
    const members = new Map([['1', { id: '1', role: 'owner' }], ['2', { id: '2', role: 'moderator' }], ['3', { id: '3', role: 'member' }]]);
    const as = (id) => ({ me: { id }, role: members.get(id).role });
    const by = (id) => ({ made_by: id });
    assert.equal(mayChange(as('3'), by('3'), members), true);
    assert.equal(mayChange(as('3'), by('2'), members), false);
    assert.equal(mayChange(as('2'), by('3'), members), true);
    assert.equal(mayChange(as('2'), by('1'), members), false);
    assert.equal(mayChange(as('1'), by('2'), members), true);
    assert.equal(mayChange(as('2'), by('9'), members), true, 'a maker who left is a member');
});

test("the den's events change the view's list: made, changed and started in place, canceled and ended out, and how many are interested", () => {
    const a = { id: '1', name: 'A', starts_at: 2, interested: 0, mine: false };
    const b = { id: '2', name: 'B', starts_at: 1, interested: 1, mine: true };
    let list = withEvent([], 'event.created', a);
    list = withEvent(list, 'event.created', b);
    assert.deepEqual(ordered(list).map((e) => e.id), ['2', '1']);
    list = withEvent(list, 'event.started', { ...a, live: true });
    assert.deepEqual(ordered(list).map((e) => e.id), ['1', '2'], 'live first');
    list = withEvent(list, 'event.interest', { event_id: '1', interested: 3 });
    assert.equal(list.find((e) => e.id === '1').interested, 3);
    assert.equal(list.find((e) => e.id === '1').mine, false, 'a count to everyone leaves the mark');
    list = withEvent(list, 'event.interest', { event_id: '1', interested: 4, mine: true });
    assert.equal(list.find((e) => e.id === '1').mine, true);
    // Kept while the den was away, one past its end by the clock leaves the
    // list all the same.
    assert.deepEqual(current([{ id: '1', ends_at: 10 }, { id: '2', ends_at: 30 }], 20).map((e) => e.id), ['2']);
    list = withEvent(list, 'event.ended', { id: '1' });
    list = withEvent(list, 'event.canceled', { id: '2' });
    assert.deepEqual(list, []);
    assert.deepEqual(withEvent(undefined, 'event.updated', a), [a]);
});

test('the Events row says Live while one runs, else how many are to come', () => {
    assert.equal(rowMark([]), '');
    assert.equal(rowMark([{ id: '1' }, { id: '2' }]), '2');
    assert.equal(rowMark([{ id: '1' }, { id: '2', live: true }]), 'Live');
    const root = document.createElement('div');
    let opened = 0;
    const later = Date.now() + DAY;
    mount(h('ul', null, h(EventsRow, { events: [{ id: '1', live: true, ends_at: later }], open: false, onOpen: () => opened++ })), root);
    assert.equal(find(root, marked('data-events-live'))?.textContent, 'Live');
    const row = find(root, (n) => n.localName === 'button');
    assert.equal(row.attributes['aria-label'], 'Events, one live now');
    row.dispatch('click', { target: row });
    assert.equal(opened, 1);
    mount(h('ul', null, h(EventsRow, { events: [{ id: '1', ends_at: later }, { id: '2', ends_at: later }, { id: '3', ends_at: 1 }], open: true, onOpen: () => {} })), root);
    assert.equal(find(root, marked('data-events-count'))?.textContent, '2');
    assert.match(find(root, marked('data-events-row')).attributes.class, /bg-base-300/);
    mount(null, root);
    assert.equal(interestedText(0), 'Nobody interested yet');
    assert.equal(interestedText(1), '1 interested');
    assert.equal(interestedText(12), '12 interested');
});

test("the owner's limit on members' events goes with Manage's other limits", () => {
    const den = { name: 'Den', url: 'https://den.example' };
    const view = {
        limits: { file_size: 1 << 20, member_storage: 1 << 30, den_storage: 1 << 30 }, retention: 0,
        call_limits: { members: 5, callers: 10, shares: 1, share_viewers: 4, share_bitrate: 1e6, share_height: 720, share_fps: 30 },
        event_limits: { member_events: 0 },
    };
    const base = draftOf(den, view);
    assert.equal(base.memberEvents, '0');
    assert.deepEqual(changesOf({ ...base, memberEvents: '3' }, base).body, { event_limits: { member_events: 3 } });
    assert.equal(changesOf({ ...base, memberEvents: '11' }, base).problem, 'Events each member may have is 1 to 10.');
    assert.equal('memberEvents' in draftOf(den, { ...view, event_limits: undefined }), false, 'a den older than events has no limit');
});

test("the page opens a den's events at their address, and notifies of them by default, and names an event's banner", () => {
    assert.deepEqual(chatRoute(`/den/abc/${EVENTS}`), { denID: 'abc', channelID: 'events' });
    assert.equal(notifying({}), true, 'events notify unless turned off');
    assert.equal(notifying({ eventsOff: true }), false);
    assert.equal(notifying({ eventsOff: true, dms: true }), true);
    assert.equal(notifying(null), false);
    const view = { me: { id: '1' }, members: [], channels: [], events: [{ id: '4', name: 'Game night' }] };
    assert.equal(fileWhere({ event: '4' }, view), 'The banner of Game night');
    assert.equal(fileWhere({ event: '5' }, view), "An event's banner");
});
