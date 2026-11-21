// The page's own day and time fields, checked without a browser:
// a month's weeks from the locale's first day, moving by days and months,
// the week's first day by locale, and the quarter hours a time offers.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    addDays, addMonths, dayString, firstWeekday, monthGrid, parseDay, timeSteps, weekday, weekdayNames,
} from '../src/ui/datefield.jsx';

test('a day reads and writes as YYYY-MM-DD, and one that never was reads as none', () => {
    assert.deepEqual(parseDay('2026-11-16'), { y: 2026, m: 11, d: 16 });
    for (const bad of ['', '2026-02-30', '2026-13-01', '16/11/2026', '2026-1-1']) assert.equal(parseDay(bad), null, bad);
    assert.equal(dayString(2026, 12, 32), '2027-01-01');
    assert.equal(dayString(2026, 3, 0), '2026-02-28');
});

test('days and months move across the ends of months and years, a month on holding to its last day', () => {
    assert.equal(addDays('2026-12-31', 1), '2027-01-01');
    assert.equal(addDays('2026-03-01', -1), '2026-02-28');
    assert.equal(addDays('2028-03-01', -1), '2028-02-29');
    assert.equal(addMonths('2026-01-31', 1), '2026-02-28');
    assert.equal(addMonths('2026-03-31', -1), '2026-02-28');
    assert.equal(addMonths('2026-11-16', 2), '2027-01-16');
    assert.equal(weekday('2026-11-16'), 1, 'a Monday');
});

test("a month's weeks start on the locale's first day, filled from the months around it", () => {
    // November 2026 starts on a Sunday and has 30 days.
    const sunday = monthGrid(2026, 11, 0);
    assert.equal(sunday.length, 5);
    assert.ok(sunday.every((w) => w.length === 7));
    assert.deepEqual(sunday[0][0], { day: '2026-11-01', d: 1, inMonth: true });
    assert.deepEqual(sunday.at(-1).map((c) => c.d), [29, 30, 1, 2, 3, 4, 5]);
    assert.deepEqual(sunday.at(-1).map((c) => c.inMonth), [true, true, false, false, false, false, false]);
    const monday = monthGrid(2026, 11, 1);
    assert.equal(monday.length, 6);
    assert.deepEqual(monday[0].map((c) => c.day), ['2026-10-26', '2026-10-27', '2026-10-28', '2026-10-29', '2026-10-30', '2026-10-31', '2026-11-01']);
    assert.deepEqual(monday.at(-1).map((c) => c.d), [30, 1, 2, 3, 4, 5, 6]);
    // February 2027 starts on a Monday and fills four weeks exactly.
    const exact = monthGrid(2027, 2, 1);
    assert.equal(exact.length, 4);
    assert.ok(exact.flat().every((c) => c.inMonth));
    for (const [y, m] of [[2026, 1], [2026, 2], [2028, 2], [2026, 8], [2026, 12]]) {
        for (const first of [0, 1, 6]) {
            const weeks = monthGrid(y, m, first);
            const inside = weeks.flat().filter((c) => c.inMonth);
            assert.equal(inside.length, new Date(Date.UTC(y, m, 0)).getUTCDate(), `${y}-${m} from ${first}`);
            assert.ok(weeks.every((w) => w.length === 7 && weekday(w[0].day) === first), `${y}-${m} from ${first}`);
        }
    }
});

test("the week starts on Sunday in the US and on Monday in Britain and Germany, and its days read in the locale's words", () => {
    assert.equal(firstWeekday('en-US'), 0);
    assert.equal(firstWeekday('en-GB'), 1);
    assert.equal(firstWeekday('de-DE'), 1);
    const names = weekdayNames('en-US', 0);
    assert.deepEqual(names.map(([, long]) => long), ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']);
    assert.deepEqual(weekdayNames('en-GB', 1)[0], ['M', 'Monday']);
});

test('a time offers each quarter hour in the short form, keeping a time off the quarters as one of its own', () => {
    const steps = timeSteps('19:00', 'en-US');
    assert.equal(steps.length, 96);
    assert.equal(steps[0][0], '00:00');
    assert.match(steps[0][1], /^12:00\sAM$/);
    assert.equal(steps[77][0], '19:15');
    assert.match(steps[76][1], /^7:00\sPM$/);
    const off = timeSteps('19:09', 'en-US');
    assert.equal(off.length, 97);
    const at = off.findIndex(([v]) => v === '19:09');
    assert.deepEqual([off[at - 1][0], off[at + 1][0]], ['19:00', '19:15']);
    assert.match(off[at][1], /^7:09\sPM$/);
    assert.match(timeSteps('', 'en-GB')[76][1], /^19:00$/);
    assert.equal(timeSteps('25:00', 'en-US').length, 96);
});
