// The browser and system the page runs in, as the browser describes
// itself, so the settings describe only what applies to them (M8.3).
// Firefox's forks, Waterfox among them, describe themselves as Firefox.

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
