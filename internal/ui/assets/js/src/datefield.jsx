// A day and a time as fields of the page's own (M22.3), the same in every
// browser, where a browser's date and time inputs each look their own: the
// day a field that opens a month under it, the time a list of quarter
// hours. Their values are the form's: YYYY-MM-DD and HH:MM.

import { useEffect, useRef, useState } from 'preact/hooks';
import { Button, day as mediumDay } from './components.jsx';
import { CalendarIcon } from './icons.jsx';

const pad = (n) => String(n).padStart(2, '0');

/** parseDay reads a YYYY-MM-DD day as { y, m, d }, or null */
export function parseDay(s) {
    const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s || '');
    if (!match) return null;
    const [y, m, d] = match.slice(1).map(Number);
    const at = new Date(Date.UTC(y, m - 1, d));
    return at.getUTCMonth() === m - 1 && at.getUTCDate() === d ? { y, m, d } : null;
}

/** dayString writes a day as YYYY-MM-DD, from a year, a month from 1 and a
    day of the month, rolling over as Date does */
export function dayString(y, m, d) {
    const at = new Date(Date.UTC(y, m - 1, d));
    return `${at.getUTCFullYear()}-${pad(at.getUTCMonth() + 1)}-${pad(at.getUTCDate())}`;
}

/** addDays is a day n days on, or back for a negative n */
export function addDays(s, n) {
    const p = parseDay(s);
    return p ? dayString(p.y, p.m, p.d + n) : s;
}

/** addMonths is a day n months on, its day of the month held to the
    month's last, as Jan 31 a month on is Feb 28 */
export function addMonths(s, n) {
    const p = parseDay(s);
    if (!p) return s;
    const last = new Date(Date.UTC(p.y, p.m - 1 + n + 1, 0)).getUTCDate();
    return dayString(p.y, p.m + n, Math.min(p.d, last));
}

/** weekday is a day's place in the week, 0 for Sunday */
export function weekday(s) {
    const p = parseDay(s);
    return p ? new Date(Date.UTC(p.y, p.m - 1, p.d)).getUTCDay() : 0;
}

// Regions whose week starts on Sunday, for a browser whose Intl doesn't
// say, as Firefox's doesn't.
const SUNDAY_FIRST = new Set(['US', 'CA', 'MX', 'BR', 'JP', 'KR', 'TW', 'HK', 'PH', 'IL', 'IN', 'ZA']);

/** firstWeekday is the day a locale's week starts on, 0 for Sunday: from
    Intl where it says, and otherwise from the locale's region, Monday
    for most */
export function firstWeekday(locale) {
    try {
        const loc = new Intl.Locale(locale || Intl.DateTimeFormat().resolvedOptions().locale);
        const info = loc.getWeekInfo?.() ?? loc.weekInfo;
        if (info?.firstDay) return info.firstDay % 7;
        const region = loc.maximize().region;
        return SUNDAY_FIRST.has(region) ? 0 : 1;
    } catch {
        return 1;
    }
}

/** monthGrid is a month's weeks, each seven days from first, the days
    before and after the month filling its first and last week: each
    { day, d, inMonth } */
export function monthGrid(y, m, first) {
    const start = dayString(y, m, 1);
    const lead = (weekday(start) - first + 7) % 7;
    const days = new Date(Date.UTC(y, m, 0)).getUTCDate();
    const weeks = [];
    for (let i = -lead; i < days || weeks.at(-1)?.length < 7; i++) {
        if ((i + lead) % 7 === 0) weeks.push([]);
        const day = addDays(start, i);
        weeks.at(-1).push({ day, d: parseDay(day).d, inMonth: i >= 0 && i < days });
    }
    return weeks;
}

/** weekdayNames are the week's days from first, each [narrow, long], in a
    locale's words */
export function weekdayNames(locale, first) {
    const narrow = new Intl.DateTimeFormat(locale, { weekday: 'narrow', timeZone: 'UTC' });
    const long = new Intl.DateTimeFormat(locale, { weekday: 'long', timeZone: 'UTC' });
    // Jan 1, 2023 was a Sunday.
    return Array.from({ length: 7 }, (_, i) => {
        const at = new Date(Date.UTC(2023, 0, 1 + ((first + i) % 7)));
        return [narrow.format(at), long.format(at)];
    });
}

/** timeSteps are the times a TimeField offers, every step minutes of the
    day, each [HH:MM, its label in a locale's short form], with value among
    them where it's off the steps */
export function timeSteps(value, locale, step = 15) {
    const label = new Intl.DateTimeFormat(locale, { hour: 'numeric', minute: '2-digit', timeZone: 'UTC' });
    const at = (h, m) => [`${pad(h)}:${pad(m)}`, label.format(new Date(Date.UTC(2020, 0, 1, h, m)))];
    const list = [];
    for (let t = 0; t < 24 * 60; t += step) list.push(at(Math.floor(t / 60), t % 60));
    const own = /^(\d{2}):(\d{2})$/.exec(value || '');
    if (own && Number(own[1]) < 24 && Number(own[2]) < 60 && !list.some(([v]) => v === value)) {
        list.push(at(Number(own[1]), Number(own[2])));
        list.sort((a, b) => a[0].localeCompare(b[0]));
    }
    return list;
}

// today is this computer's day.
function today() {
    const now = new Date();
    return dayString(now.getFullYear(), now.getMonth() + 1, now.getDate());
}

// shownDay is a day in the page's one form for dates, the reader's medium
// one.
function shownDay(s) {
    const p = parseDay(s);
    return p ? mediumDay(new Date(p.y, p.m - 1, p.d).getTime()) : '';
}

// within holds a day to min and max, either of which may be ''.
const within = (s, min, max) => (min && s < min ? min : max && s > max ? max : s);

/** DateField is a day as the page draws it: a field showing it in the
    reader's medium form, which opens its month under it. It stands in a
    row that wraps, as beside a TimeField: the month takes a line of its
    own under the row. Days before min or after max can't be picked. rest
    reaches the field's button. */
export function DateField({ value, onChange, min = '', max = '', disabled = false, label = 'Day', class: extra = '', ...rest }) {
    const [open, setOpen] = useState(false);
    const [focus, setFocus] = useState(value || today());
    const field = useRef(null);
    const grid = useRef(null);
    // keyed says the focus moved by a key, or the month opened, so the
    // day it's on takes the browser's focus.
    const keyed = useRef(false);
    useEffect(() => {
        if (disabled) setOpen(false);
    }, [disabled]);
    useEffect(() => {
        if (!open || !keyed.current) return;
        keyed.current = false;
        grid.current?.querySelector(`[data-day="${focus}"]`)?.focus();
    }, [open, focus]);
    const p = parseDay(focus) || parseDay(today());
    const locale = Intl.DateTimeFormat().resolvedOptions().locale;
    const first = firstWeekday(locale);
    const now = today();
    function toggle() {
        if (!open) {
            setFocus(within(value || now, min, max));
            keyed.current = true;
        }
        setOpen(!open);
    }
    function pick(s) {
        onChange(s);
        setOpen(false);
        field.current?.focus();
    }
    function move(to) {
        keyed.current = true;
        setFocus(within(to, min, max));
    }
    function keys(e) {
        const step = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }[e.key];
        if (step) move(addDays(focus, step));
        else if (e.key === 'PageUp') move(addMonths(focus, -1));
        else if (e.key === 'PageDown') move(addMonths(focus, 1));
        else if (e.key === 'Home') move(addDays(focus, -((weekday(focus) - first + 7) % 7)));
        else if (e.key === 'End') move(addDays(focus, 6 - ((weekday(focus) - first + 7) % 7)));
        else if (e.key === 'Escape') {
            setOpen(false);
            field.current?.focus();
        } else return;
        e.preventDefault();
        e.stopPropagation();
    }
    const title = new Intl.DateTimeFormat(locale, { month: 'long', year: 'numeric', timeZone: 'UTC' }).format(new Date(Date.UTC(p.y, p.m - 1, 1)));
    const shown = shownDay(value);
    return (
        <>
            <button ref={field} type="button" class={`input cursor-pointer justify-between gap-2 text-left ${extra}`} disabled={disabled}
                aria-label={`${label}: ${shown || 'none'}`} aria-expanded={open} onClick={toggle} {...rest}>
                <span class="truncate">{shown}</span>
                <span class="text-base-content/60"><CalendarIcon size="h-4 w-4" /></span>
            </button>
            {open && (
                <div class="order-last basis-full rounded-box border border-base-300 p-2" role="group" aria-label={`${label}: ${title}`} data-calendar>
                    <div class="flex items-center gap-1">
                        <Button kind="quiet" class="btn-square" aria-label="The month before" disabled={!!min && dayString(p.y, p.m, 0) < min}
                            onClick={() => setFocus(within(addMonths(focus, -1), min, max))}>‹</Button>
                        <span class="flex-1 text-center text-sm font-medium" aria-live="polite">{title}</span>
                        <Button kind="quiet" class="btn-square" aria-label="The month after" disabled={!!max && dayString(p.y, p.m + 1, 1) > max}
                            onClick={() => setFocus(within(addMonths(focus, 1), min, max))}>›</Button>
                    </div>
                    <table ref={grid} role="grid" class="w-full table-fixed text-center text-sm" onKeyDown={keys}>
                        <thead>
                            <tr>
                                {weekdayNames(locale, first).map(([narrow, long]) => (
                                    <th key={long} scope="col" abbr={long} class="py-1 text-xs font-normal text-base-content/60">{narrow}</th>
                                ))}
                            </tr>
                        </thead>
                        <tbody>
                            {monthGrid(p.y, p.m, first).map((week) => (
                                <tr key={week[0].day}>
                                    {week.map((c) => {
                                        const chosen = c.day === value;
                                        const out = (min && c.day < min) || (max && c.day > max);
                                        return (
                                            <td key={c.day} class="p-0.5">
                                                <button type="button" data-day={c.day} tabIndex={c.day === focus ? 0 : -1} disabled={!!out}
                                                    aria-label={shownDay(c.day)} aria-selected={chosen} aria-current={c.day === now ? 'date' : undefined}
                                                    class={`h-9 w-full rounded-field tabular-nums pointer-coarse:h-11 disabled:cursor-default disabled:opacity-30 ${
                                                        chosen ? 'bg-primary text-primary-content' : 'hover:bg-base-200'
                                                    } ${c.inMonth ? '' : 'text-base-content/40'} ${c.day === now && !chosen ? 'font-semibold underline underline-offset-4' : ''}`}
                                                    onFocus={() => c.day !== focus && setFocus(c.day)} onClick={() => pick(c.day)}>
                                                    {c.d}
                                                </button>
                                            </td>
                                        );
                                    })}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </>
    );
}

/** TimeField is a time of day as a list of quarter hours, in the reader's
    short form, a value off the quarters kept as one of its own. rest
    reaches the select. */
export function TimeField({ value, onChange, label = 'Time', class: extra = '', ...rest }) {
    const locale = Intl.DateTimeFormat().resolvedOptions().locale;
    return (
        <select class={`select ${extra}`} value={value} aria-label={label} onChange={(e) => onChange(e.currentTarget.value)} {...rest}>
            {timeSteps(value, locale).map(([v, text]) => <option key={v} value={v}>{text}</option>)}
        </select>
    );
}
