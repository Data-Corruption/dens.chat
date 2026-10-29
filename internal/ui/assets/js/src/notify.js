// Notifications (M12.3). The service decides what notifies the member and
// where it shows, since a page may be hidden, frozen or closed. A page
// shows those the service gives it through the browser's notifications,
// which the settings ask the browser for as a kind is turned on, and a
// click brings its tab forward at the message. The page tells the service
// whether it's on screen and whether the browser lets it notify, and the
// service takes a notification back, as when the member reads its
// channel, wherever it shows. Android's WebView has no notifications: the
// app's shell posts them there.

import { onEvent, setPageState } from './events.js';

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
// a click on it brings the page forward and opens the message, through
// open. It returns the browser's notification, or null where the browser
// won't show it.
export function show(n, open, win = globalThis) {
    const N = win.Notification;
    if (permission(win) !== 'granted') return null;
    shown.get(n.key)?.close();
    let note;
    try {
        note = new N(heading(n), { body: n.text, tag: n.key });
    } catch {
        // Chromium on a phone shows them only through a service worker.
        return null;
    }
    // The browser brings the tab forward for a click it isn't kept from,
    // and the page asks it to as well.
    note.onclick = () => {
        win.focus?.();
        note.close();
        open(n.den, n.channel, n.message);
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
// clicked one's message through open(den, channel, message), and keeps the
// service told what the page is.
export function startNotifications(open) {
    if (started) return;
    started = true;
    onEvent((msg) => {
        if (msg.t === 'notifications') {
            status = msg.d;
            listeners.forEach((fn) => fn(status));
        } else if (msg.t === 'notify') {
            show(msg.d, open);
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
// and its channels'.
export function mutedIn(st, denID) {
    const m = st?.muted?.[denID];
    return { den: !!m?.den, channels: new Set(m?.channels || []) };
}

// notifying is whether the member turned any kind on.
export function notifying(st) {
    return !!(st?.dms || st?.mentions);
}

// whereTheyShow says where the member's notifications show, for the
// settings: through this browser, the desktop's helper with no tab open,
// or Android, by what each allows. helper is the keys' status from the
// service, which says whether a helper that shows notifications runs.
export function whereTheyShow(st, browser, helper) {
    if (st?.shell) {
        return st.shell.allowed
            ? 'Android shows them, unless Dens is open on that channel.'
            : "Android doesn't let Dens show notifications. Allow them for Dens in Android's settings, under Apps.";
    }
    const here = {
        granted: 'This browser shows them while a Dens tab is open, unless it shows that channel.',
        denied: "This browser blocks Dens's notifications: allow them for this site in its settings.",
        default: 'This browser asks whether to show them once you turn one on.',
        unsupported: "This browser can't show notifications.",
    }[browser];
    const away = helper?.helper && helper.notifies
        ? 'With no Dens tab open, your desktop shows them.'
        : "With no Dens tab open, your desktop shows them while Dens's helper runs.";
    return `${here} ${away}`;
}
