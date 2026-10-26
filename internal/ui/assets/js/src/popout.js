// Shares in windows of their own (M11): a share the member watches opens in
// a blank window of the page's own origin, which the page fills with the
// share and its controls, and which they can move to another monitor,
// resize and make full screen. The window plays the stream the page
// already receives, so it needs no sign-in and no connection of its own.
// Closing it brings the share back to the player; stopping watching,
// leaving the call or the share ending closes it, and so does closing or
// reloading the page.

import { listenIn } from './voice.js';

// windows holds each open window, by the member whose share it plays.
const windows = new Map();
const listeners = new Set();
let closesWithPage = false;

// SIZE is a new window's, which fits a 16:9 picture under its title bar.
const SIZE = 'width=960,height=580';

// poppedOut lists the members whose shares play in windows of their own,
// with their windows.
export function poppedOut() {
    return new Map([...windows].map(([member, w]) => [member, w.win]));
}

// onPopOut calls fn as a window opens or closes; it returns a function
// that stops.
export function onPopOut(fn) {
    listeners.add(fn);
    return () => listeners.delete(fn);
}

function changed() {
    listeners.forEach((fn) => fn());
}

// popOut opens a member's share in a window of its own, named for label, or
// brings its window to the front, and reports whether the browser opened
// one: a browser that blocks pop-ups gives none.
export function popOut(member, label) {
    const open = windows.get(member);
    if (open) {
        open.win.focus();
        return true;
    }
    const win = window.open('', '', `popup,${SIZE}`);
    if (!win) return false;
    dress(win.document, `${label}'s screen`);
    const w = { win, unlisten: listenIn(win) };
    windows.set(member, w);
    // The window closing brings the share back. A browser may not say so as
    // it closes, so the page also looks.
    const back = () => {
        if (windows.get(member) !== w) return;
        windows.delete(member);
        forget(w);
        changed();
    };
    win.addEventListener('pagehide', back);
    w.looking = setInterval(() => win.closed && back(), 1000);
    if (!closesWithPage) {
        closesWithPage = true;
        window.addEventListener('pagehide', () => [...windows.keys()].forEach(popIn));
    }
    changed();
    return true;
}

// popIn closes a member's share's window, if one is open, which brings the
// share back to the player.
export function popIn(member) {
    const w = windows.get(member);
    if (!w) return;
    windows.delete(member);
    forget(w);
    try {
        w.win.close();
    } catch {
        // Closed already.
    }
    changed();
}

function forget(w) {
    clearInterval(w.looking);
    try {
        w.unlisten();
    } catch {
        // A window that closed takes its listeners with it.
    }
}

// dress gives a share's window the page's look: its title, stylesheet and
// theme, and a black page the share fills.
function dress(doc, title) {
    doc.title = title;
    for (const link of document.querySelectorAll('link[rel="stylesheet"]')) {
        const copy = doc.createElement('link');
        copy.rel = 'stylesheet';
        copy.href = link.href;
        doc.head.append(copy);
    }
    const theme = document.documentElement.getAttribute('data-theme');
    if (theme) doc.documentElement.setAttribute('data-theme', theme);
    doc.documentElement.lang = document.documentElement.lang;
    doc.body.className = 'm-0 h-screen overflow-hidden bg-black';
}
