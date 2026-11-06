// Events (M17.4), what the page knows of them without a screen: the den's
// limits as the den holds them, the order the pane lists them in, what the
// Events row says, an event's times in the member's time zone, which the
// form takes in and the pane shows, what the form sends, who may change an
// event, and the events the den sends, applied to the view.
//
// The page shows times in the member's own time zone, which the browser
// knows, and sends the den Unix milliseconds. Each helper that reads or
// writes a time of day takes the zone, so the tests can name one.

// EVENTS is the events pane's place in a den's address, beside a channel's
// ID and the Direct messages tab's dms.
export const EVENTS = 'events';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

// The den's limits (denproto's): a name and a place written out of up to
// 100 characters, a description of up to 2,000, a start up to a year ahead,
// or a minute past, which counts as now, and a length of 5 minutes to a
// week.
export const MAX_NAME = 100;
export const MAX_PLACE = 100;
export const MAX_DESCRIPTION = 2000;
export const MAX_AHEAD = 365 * DAY;
export const START_SLACK = MINUTE;
export const MIN_LENGTH = 5 * MINUTE;
export const MAX_LENGTH = 7 * DAY;
// A den holds 50 events that haven't ended, and lets each member who isn't
// staff have up to 10, as its owner sets.
export const MAX_EVENTS = 50;
export const MAX_MEMBER_EVENTS = 10;

// LENGTHS are the lengths the form offers, in minutes, from 5 minutes to a
// week.
export const LENGTHS = [
    [5, '5 minutes'], [15, '15 minutes'], [30, '30 minutes'], [45, '45 minutes'], [60, '1 hour'], [90, '1½ hours'], [120, '2 hours'],
    [180, '3 hours'], [240, '4 hours'], [360, '6 hours'], [480, '8 hours'], [720, '12 hours'], [1440, '1 day'], [2880, '2 days'],
    [4320, '3 days'], [5760, '4 days'], [7200, '5 days'], [8640, '6 days'], [10080, '1 week'],
];

// length counts a name's characters as the den does, emoji as their code
// points.
const length = (s) => [...s].length;

// current are the events not past their end by the clock: a Dens that
// can't reach the den shows what it kept, which the den would have ended.
export function current(events, now) {
    return (events || []).filter((e) => e.ends_at > now);
}

// ordered lists events as the pane shows them: those live first, then the
// rest by their start.
export function ordered(events) {
    return [...(events || [])].sort((a, b) => (b.live ? 1 : 0) - (a.live ? 1 : 0) || a.starts_at - b.starts_at || Number(a.id) - Number(b.id));
}

// rowMark is what the Events row says beside its name: Live while one
// runs, else how many are to come, or nothing.
export function rowMark(events) {
    const list = events || [];
    if (list.some((e) => e.live)) return 'Live';
    const coming = list.filter((e) => !e.live).length;
    return coming ? String(coming) : '';
}

// zoneParts are a time's date and time of day in a time zone.
export function zoneParts(ms, tz) {
    const parts = new Intl.DateTimeFormat('en-US', {
        timeZone: tz, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    }).formatToParts(new Date(ms));
    const get = (type) => Number(parts.find((p) => p.type === type).value);
    return { year: get('year'), month: get('month'), day: get('day'), hour: get('hour'), minute: get('minute') };
}

// offsetAt is how far a time zone's clocks are ahead of UTC at a moment.
function offsetAt(ms, tz) {
    const p = zoneParts(ms, tz);
    return Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute) - Math.floor(ms / MINUTE) * MINUTE;
}

const pad = (n) => String(n).padStart(2, '0');

// inputsAt are a time as the form's date and time fields hold it, in a
// time zone: YYYY-MM-DD and HH:MM.
export function inputsAt(ms, tz) {
    const p = zoneParts(ms, tz);
    return { date: `${p.year}-${pad(p.month)}-${pad(p.day)}`, time: `${pad(p.hour)}:${pad(p.minute)}` };
}

// atZone is the moment a date and time of day name in a time zone, or NaN
// for fields that don't read as one. A time the clocks skip as they go
// forward lands just after the change; one they pass twice, as they go
// back, is the first.
export function atZone(date, time, tz) {
    const d = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date || '');
    const t = /^(\d{2}):(\d{2})$/.exec(time || '');
    if (!d || !t) return NaN;
    const wall = Date.UTC(Number(d[1]), Number(d[2]) - 1, Number(d[3]), Number(t[1]), Number(t[2]));
    const first = wall - offsetAt(wall, tz);
    const second = wall - offsetAt(first, tz);
    // Across a change of the clocks, take the moment that reads back as the
    // fields, or the earlier where both do.
    const reads = (ms) => inputsAt(ms, tz).date === date && inputsAt(ms, tz).time === time;
    if (reads(first) && reads(second)) return Math.min(first, second);
    if (reads(second)) return second;
    if (reads(first)) return first;
    return Math.max(first, second);
}

// dayOf is a moment's date in a time zone, as a number that orders days.
function dayOf(ms, tz) {
    const p = zoneParts(ms, tz);
    return Date.UTC(p.year, p.month - 1, p.day) / DAY;
}

// whenText says when an event is, in a time zone and locale: its start's
// day, Today or Tomorrow where it's one of those, and its time, and its
// end's, with the end's day where it's another; or while it's live, that
// it runs until its end.
export function whenText(e, now, tz, locale) {
    const time = (ms) => new Intl.DateTimeFormat(locale, { timeZone: tz, hour: 'numeric', minute: '2-digit' }).format(new Date(ms));
    const date = (ms) => {
        const days = dayOf(ms, tz) - dayOf(now, tz);
        if (days === 0) return 'Today';
        if (days === 1) return 'Tomorrow';
        const year = zoneParts(ms, tz).year !== zoneParts(now, tz).year;
        return new Intl.DateTimeFormat(locale, { timeZone: tz, weekday: 'short', month: 'short', day: 'numeric', ...(year ? { year: 'numeric' } : {}) })
            .format(new Date(ms));
    };
    const sameDay = dayOf(e.starts_at, tz) === dayOf(e.ends_at, tz);
    const end = sameDay ? time(e.ends_at) : `${date(e.ends_at)}, ${time(e.ends_at)}`;
    if (e.live) return `Live until ${dayOf(e.ends_at, tz) === dayOf(now, tz) ? time(e.ends_at) : end}`;
    return `${date(e.starts_at)}, ${time(e.starts_at)} – ${end}`;
}

// zone is the member's time zone, as the browser has it.
export function zone() {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
}

// newForm is the form for a new event: an hour from now, on the hour, for
// an hour, in the den's first voice channel the member sees, if any.
export function newForm(now, tz, voice) {
    const next = Math.ceil((now + HOUR) / HOUR) * HOUR;
    return { name: '', description: '', ...inputsAt(next, tz), minutes: 60, where: voice ? 'channel' : 'place', channel: voice || '', place: '' };
}

// formOf is the form for changing an event, as it stands.
export function formOf(e, tz) {
    const minutes = Math.round((e.ends_at - e.starts_at) / MINUTE);
    return {
        name: e.name, description: e.description || '', ...inputsAt(e.starts_at, tz), minutes, where: e.channel_id ? 'channel' : 'place',
        channel: e.channel_id || '', place: e.place || '',
    };
}

// lengthsFor are the lengths the form offers an event: all of them for one
// to come, and for one live those that end after now, with its own length
// among them where it's none of the list's.
export function lengthsFor(e, now) {
    let list = LENGTHS;
    if (e?.live) list = list.filter(([m]) => e.starts_at + m * MINUTE > now);
    const own = e ? Math.round((e.ends_at - e.starts_at) / MINUTE) : 0;
    if (own && !list.some(([m]) => m === own)) {
        const label = own % 60 ? `${own} minutes` : own % 1440 ? `${own / 60} hours` : `${own / 1440} days`;
        list = [...list, [own, label]].sort((a, b) => a[0] - b[0]);
    }
    return list;
}

// formProblem is what keeps the form from going, as the den would say it,
// or '': a name, a description and a place within their lengths, a start
// from now to a year ahead, and a length within the limits. A live event's
// start stays where it is, and its end comes after now.
export function formProblem(f, now, tz, e) {
    const name = f.name.trim();
    if (!name) return 'An event needs a name.';
    if (length(name) > MAX_NAME) return `A name is at most ${MAX_NAME} characters.`;
    if (length(f.description.trim()) > MAX_DESCRIPTION) return `A description is at most ${MAX_DESCRIPTION.toLocaleString('en-US')} characters.`;
    if (f.where === 'channel' && !f.channel) return 'Pick a voice channel.';
    if (f.where === 'place' && !f.place.trim()) return 'Say where it is.';
    if (f.where === 'place' && length(f.place.trim()) > MAX_PLACE) return `A place is at most ${MAX_PLACE} characters.`;
    const minutes = f.minutes * MINUTE;
    if (minutes < MIN_LENGTH || minutes > MAX_LENGTH) return 'An event lasts 5 minutes to a week.';
    if (e?.live) return e.starts_at + minutes > now ? '' : 'It would have ended already: End now ends it.';
    const starts = atZone(f.date, f.time, tz);
    if (Number.isNaN(starts)) return 'Pick a day and a time.';
    if (starts < now - START_SLACK) return 'That time has passed.';
    if (starts > now + MAX_AHEAD) return 'An event starts within a year.';
    return '';
}

// eventRequest is what the form sends the den: for a new event all of it,
// and for one it changes only what differs. A live event's start stays.
export function eventRequest(f, tz, e) {
    const starts = e?.live ? e.starts_at : atZone(f.date, f.time, tz);
    const req = {
        name: f.name.trim(), description: f.description.trim(), starts_at: starts, ends_at: starts + f.minutes * MINUTE,
    };
    if (f.where === 'channel') req.channel_id = f.channel;
    else req.place = f.place.trim();
    if (!e) return req;
    const changed = {};
    if (req.name !== e.name) changed.name = req.name;
    if (req.description !== (e.description || '')) changed.description = req.description;
    if (req.starts_at !== e.starts_at) changed.starts_at = req.starts_at;
    if (req.ends_at !== e.ends_at) changed.ends_at = req.ends_at;
    if (req.channel_id && req.channel_id !== e.channel_id) changed.channel_id = req.channel_id;
    if (req.place !== undefined && req.place !== (e.place || '')) changed.place = req.place;
    return changed;
}

const RANK = { member: 0, moderator: 1, owner: 2 };
const staffRole = (role) => role === 'moderator' || role === 'owner';

// canMake says whether a member may make an event in a den, by its limits
// and their role: staff may while the den holds fewer than 50, and a member
// who isn't staff while they have fewer than the owner's limit. A den
// older than events has none.
export function canMake(view) {
    const limits = view?.event_limits;
    if (!limits) return false;
    const events = view.events || [];
    if (events.length >= MAX_EVENTS) return false;
    if (staffRole(view.role)) return true;
    return events.filter((e) => e.made_by === view.me.id).length < limits.member_events;
}

// mayChange says whether a member may change an event, cancel it or end
// it: its maker, and staff above them, as the den's rule for calls and
// messages has it.
export function mayChange(view, e, members) {
    if (e.made_by === view.me.id) return true;
    if (!staffRole(view.role)) return false;
    const maker = members?.get(e.made_by);
    return RANK[maker?.role || 'member'] < RANK[view.role];
}

// withEvent applies one of the den's events about events to a view's list:
// one made, changed or started, as the member's Dens passes it on with
// their own mark; one canceled or ended, which leaves the list; and how
// many are interested, with the member's mark where it's theirs.
export function withEvent(events, t, d) {
    const list = events || [];
    switch (t) {
    case 'event.created':
    case 'event.updated':
    case 'event.started':
        return [...list.filter((e) => e.id !== d.id), d];
    case 'event.canceled':
    case 'event.ended':
        return list.filter((e) => e.id !== d.id);
    case 'event.interest':
        return list.map((e) => (e.id !== d.event_id ? e : { ...e, interested: d.interested, ...(d.mine !== undefined ? { mine: d.mine } : {}) }));
    }
    return list;
}

// interestedText says how many members are interested in an event.
export function interestedText(n) {
    if (!n) return 'Nobody interested yet';
    return n === 1 ? '1 interested' : `${n} interested`;
}
