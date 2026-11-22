// Notifications. The service decides what notifies the member and
// where it shows, since a page may be hidden, frozen or closed. A page
// shows those the service gives it through the browser's notifications,
// which the settings ask the browser for as a kind is turned on, and a
// click brings its tab forward at the message. The page tells the service
// whether it's on screen and whether the browser lets it notify, and the
// service takes a notification back, as when the member reads its
// channel, wherever it shows. Android's WebView has no notifications: the
// app's shell posts them there.
//
// A text channel's level, All messages, Mentions or Nothing, and Do not
// disturb, which holds every notification until its end, are the
// service's too; the page sets them and reads them from the status.
//
// The service places the message's sound too, so a message
// makes one: a page plays it with the notification it shows, which it asks
// the browser to show silently, and alone where the service says, as when
// the member turned the kind off. It also carries the sounds' settings in
// the status, which the page's sounds play by.

import { api } from './api.js';
import { onEvent, setPageState } from './events.js';
import { heardSounds, playSound } from '../voice/sounds.js';

// The latest status the service sent: the member's settings, and on
// Android, the shell's word on whether Android lets the app post them.
let status = null;
const listeners = new Set();
// What this page shows, by key: one notification per channel.
const shown = new Map();
let started = false;

// permission is what the browser lets the page do with notifications:
// granted, denied, default (it hasn't asked), or unsupported, as in
// Android's WebView. win is the page's window, or a stand-in in the tests.
export function permission(win = globalThis) {
    return typeof win.Notification === 'function' ? win.Notification.permission : 'unsupported';
}

// askToNotify asks the browser to let the page notify, where it hasn't
// asked yet, and returns what it lets the page do. A browser asks only in
// answer to a click, so the click that turns a kind on calls it at once.
export async function askToNotify(win = globalThis) {
    const N = win.Notification;
    if (typeof N !== 'function') return 'unsupported';
    if (N.permission !== 'default') return N.permission;
    try {
        return await N.requestPermission();
    } catch {
        return N.permission;
    }
}

// heading is a notification's first line: who sent it, where for a
// mention, and the den's name.
export function heading(n) {
    return n.denName ? `${n.title} · ${n.denName}` : n.title;
}

// show shows a notification through the browser, replacing its channel's;
// a click on it brings the page forward and opens the message, or the
// event, through open, which it hands the notification. It shows
// silently, since the page plays the message's sound: Chromium
// and, from 132, Firefox keep the system's own quiet. It returns the
// browser's notification, or null where the browser won't show it.
export function show(n, open, win = globalThis) {
    const N = win.Notification;
    if (permission(win) !== 'granted') return null;
    shown.get(n.key)?.close();
    let note;
    try {
        note = new N(heading(n), { body: n.text, tag: n.key, silent: true });
    } catch {
        // Chromium on a phone shows them only through a service worker.
        return null;
    }
    // The browser brings the tab forward for a click it isn't kept from,
    // and the page asks it to as well.
    note.onclick = () => {
        win.focus?.();
        note.close();
        open(n);
    };
    note.onclose = () => {
        if (shown.get(n.key) === note) shown.delete(n.key);
    };
    shown.set(n.key, note);
    return note;
}

// withdraw takes back what this page shows of keys.
export function withdraw(keys) {
    for (const key of keys) {
        const note = shown.get(key);
        shown.delete(key);
        note?.close();
    }
}

// tell says to the service what this page is now: on screen or not, and
// whether the browser lets it notify.
export function tell(doc = globalThis.document, win = globalThis) {
    setPageState(doc.visibilityState === 'visible', permission(win) === 'granted');
}

// startNotifications shows what the service gives this page, opening a
// clicked one's message or event through open(notification), and keeps the
// service told what the page is.
export function startNotifications(open) {
    if (started) return;
    started = true;
    onEvent((msg) => {
        if (msg.t === 'notifications') {
            status = msg.d;
            heardSounds(status);
            listeners.forEach((fn) => fn(status));
        } else if (msg.t === 'notify') {
            // The sound plays whether or not the browser shows it.
            show(msg.d, open);
            playSound('message');
        } else if (msg.t === 'sound') {
            playSound('message');
        } else if (msg.t === 'unnotify') {
            withdraw(msg.d.keys || []);
        }
    });
    tell();
    document.addEventListener('visibilitychange', () => tell());
    // The member may change what the browser allows in its settings.
    navigator.permissions?.query({ name: 'notifications' }).then((p) => {
        p.onchange = () => tell();
    }, () => {});
}

// onNotifications calls fn with the service's status of notifications, now
// if it came and whenever it changes; it returns a function that stops.
export function onNotifications(fn) {
    listeners.add(fn);
    if (status) fn(status);
    return () => listeners.delete(fn);
}

// mutedIn is what the member muted in a den, by the status: the whole den,
// and its channels at Nothing; and its channels at All messages.
export function mutedIn(st, denID) {
    const m = st?.muted?.[denID];
    return { den: !!m?.den, channels: new Set(m?.channels || []), all: new Set(m?.all || []) };
}

// levelOf is a text channel's level: all, mentions or nothing.
export function levelOf(st, denID, channelID) {
    const m = st?.muted?.[denID];
    if (m?.channels?.includes(channelID)) return 'nothing';
    return m?.all?.includes(channelID) ? 'all' : 'mentions';
}

// LEVELS are a text channel's levels, as its menu and its header's bell
// list them.
const LEVELS = [
    { key: 'all', label: 'All messages', icon: 'bellring' },
    { key: 'mentions', label: 'Mentions', icon: 'at' },
    { key: 'nothing', label: 'Nothing', icon: 'belloff' },
];

// levelMenu lists the levels, level checked, as MenuItems takes them.
export function levelMenu(level) {
    return LEVELS.map((l) => ({ ...l, checked: l.key === level }));
}

// setLevel sets a text channel's level. All messages asks the browser to
// let the page notify, where it hasn't asked, which it does only in
// answer to the click, so the click calls this at once; the level goes
// without waiting, since a prompt may stay open.
export function setLevel(denID, channel, level) {
    if (level === 'all') askToNotify().then(() => tell());
    return api.post(`/api/dens/${denID}/notifications`, { channel, level });
}

// notifying is whether anything notifies: DMs or mentions, which the
// member turns on, a channel at All messages, or events, on unless they
// turn them off.
export function notifying(st) {
    return !!(st && (st.dms || st.mentions || !st.eventsOff || Object.values(st.muted || {}).some((m) => m.all?.length)));
}

// quiet is how Do not disturb stands at now, by the status: on, and its
// end, null for none. An end gone by is off: the service sends nothing as
// it passes, and reads it as off from then on.
export function quiet(st, now = Date.now()) {
    const until = st?.dndUntil ? Date.parse(st.dndUntil) : null;
    if (!st?.dnd || (until !== null && until <= now)) return { on: false, until: null };
    return { on: true, until };
}

// QUIET_ENDS are Do not disturb's ends, as the moon lists them.
export const QUIET_ENDS = [
    { key: 'hour', label: 'For an hour', icon: 'clock' },
    { key: 'tomorrow', label: 'Until tomorrow', icon: 'sunrise' },
    { key: 'manual', label: 'Until I turn it off', icon: 'moon' },
];

// quietEnd is when an end of QUIET_ENDS falls, from now: an hour on, the
// next 8:00 by the device's clock, or null for none.
export function quietEnd(key, now = new Date()) {
    if (key === 'hour') return new Date(now.getTime() + 60 * 60 * 1000);
    if (key !== 'tomorrow') return null;
    const at = new Date(now);
    at.setHours(8, 0, 0, 0);
    if (at <= now) at.setDate(at.getDate() + 1);
    return at;
}

// setQuiet turns Do not disturb on until an end of QUIET_ENDS, sent as
// the time it falls, or with key null turns it off.
export function setQuiet(key) {
    if (!key) return api.post('/api/settings', { notifications: { dnd: false } });
    const until = quietEnd(key);
    return api.post('/api/settings', { notifications: until ? { dnd: true, dndUntil: until.toISOString() } : { dnd: true } });
}

// untilWords says until when Do not disturb holds, from now: a time
// today, tomorrow's with the word, or another day's with its name.
export function untilWords(until, now = Date.now()) {
    if (until === null) return 'until you turn it off';
    const end = new Date(until);
    const time = end.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    const day = new Date(now);
    if (end.toDateString() === day.toDateString()) return `until ${time}`;
    day.setDate(day.getDate() + 1);
    if (end.toDateString() === day.toDateString()) return `until tomorrow, ${time}`;
    return `until ${end.toLocaleDateString([], { weekday: 'long' })}, ${time}`;
}

// whereTheyShow is what the settings say of where the member's
// notifications show, each a [kind, line], only what calls for one:
// a browser or Android that won't show them, and the desktop's helper
// showing them with no tab open. helper is the keys' status from the
// service, which says whether a helper that shows notifications runs.
export function whereTheyShow(st, browser, helper) {
    if (st?.shell) return st.shell.allowed ? [] : [['warning', "Android doesn't let Dens show notifications. Allow them in Android's settings, under Apps."]];
    const lines = [];
    if (browser === 'denied') lines.push(['warning', 'This browser blocks them; allow them in its site settings.']);
    if (browser === 'unsupported') lines.push(['warning', "This browser can't show notifications."]);
    if (helper?.helper && helper.notifies) lines.push(['success', 'With no Dens tab open, your desktop shows them.']);
    return lines;
}
