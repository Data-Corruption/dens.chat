// The browser and system the page runs in, as the browser describes
// itself, so the settings describe only what applies to them (M8.3).
// Firefox's forks, Waterfox among them, describe themselves as Firefox.
// What a browser can do comes from what it has, not what it says it is
// (M9.1): Android's WebView says it's Linux.

// browserOf names a browser by its user agent: Firefox, or one built on
// Chromium, which the page otherwise takes it for.
export function browserOf(userAgent) {
    return /\bFirefox\//.test(userAgent) ? 'firefox' : 'chromium';
}

// systemOf names the system in a user agent: windows, mac, linux or other.
export function systemOf(userAgent) {
    if (/\bWindows\b/.test(userAgent)) return 'windows';
    if (/\bMac OS X\b|\bMacintosh\b/.test(userAgent)) return 'mac';
    if (/\bLinux\b|\bX11\b/.test(userAgent)) return 'linux';
    return 'other';
}

// here names this page's browser and system.
export function here() {
    const ua = typeof navigator === 'undefined' ? '' : navigator.userAgent;
    return { browser: browserOf(ua), system: systemOf(ua) };
}

// canCall says whether the browser can make calls, which takes WebRTC:
// GNOME Web has had none since WebKitGTK 2.54. win is the page's window,
// or a stand-in in the tests.
export function canCall(win = globalThis) {
    return typeof win.RTCPeerConnection === 'function';
}

// canShare says whether the browser can share a screen in a call, which
// Android's WebView can't.
export function canShare(win = globalThis) {
    return canCall(win) && typeof win.navigator?.mediaDevices?.getDisplayMedia === 'function';
}

// autoplays says whether the page's audio may start without a click, as
// the service says its form lets it: Android's app does (M12). doc is the
// page's document, or a stand-in in the tests.
export function autoplays(doc = globalThis.document) {
    return doc?.getElementById?.('app')?.dataset?.autoplay === 'true';
}

// hasKeys says whether the page has keys its shortcuts may take, as the
// service says its form has them: Android's app has none (M13.3), and its
// settings leave the shortcuts out.
export function hasKeys(doc = globalThis.document) {
    return doc?.getElementById?.('app')?.dataset?.keys !== 'false';
}

// inApp says whether the page shows in Dens's own app rather than a
// browser, as the service says its form has it: Android's app (M13.3). The
// app holds the page's permissions, as for the microphone, which the
// phone's settings allow, and pairs the page as it opens it.
export function inApp(doc = globalThis.document) {
    return doc?.getElementById?.('app')?.dataset?.inApp === 'true';
}

// canPopOut says whether the browser opens a share in a window of its own
// that the member can move to another monitor (M11). Phones' browsers and
// Android's WebView open no such window, and capture no screen either,
// which tells them apart.
export function canPopOut(win = globalThis) {
    return typeof win.open === 'function' && canShare(win);
}

// NO_CALLS and NO_SHARING say so where the page would offer them.
export const NO_CALLS = "This browser can't make calls. Open Dens in Firefox, Chrome or Edge to join one.";
export const NO_SHARING = "This page can't share a screen.";
