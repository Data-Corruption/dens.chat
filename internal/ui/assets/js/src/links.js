// How the page shows links (M3). The den, and a DM's sender, write every
// link to a Reddit post as https://www.reddit.com/comments/…, so showing
// it on old.reddit.com, for a browser that prefers it, takes only another
// host. Every YouTube link is written as https://www.youtube.com/watch?v=…,
// which is all a browser that turned YouTube's players on (M4.1) needs to
// find the video to play.

const OLD_REDDIT = 'DENS_OLD_REDDIT';
const REDDIT_POST = 'https://www.reddit.com/comments/';

let old = null;

// oldReddit reports whether this browser shows Reddit links on
// old.reddit.com.
export function oldReddit() {
    if (old === null) {
        try {
            old = localStorage.getItem(OLD_REDDIT) === '1';
        } catch {
            // Storage can be unavailable; links stay as they were sent.
            old = false;
        }
    }
    return old;
}

export function setOldReddit(on) {
    old = on;
    try {
        if (on) localStorage.setItem(OLD_REDDIT, '1');
        else localStorage.removeItem(OLD_REDDIT);
    } catch {
        // As above; the choice lasts until the page reloads.
    }
}

const PLAYERS = 'DENS_YOUTUBE';
const playersListeners = new Set();
let players = null;

// youTubePlayers reports whether this browser plays YouTube videos where
// they're linked (M4.1). It's off until the member turns it on, since it
// means YouTube hears from them.
export function youTubePlayers() {
    if (players === null) {
        try {
            players = localStorage.getItem(PLAYERS) === '1';
        } catch {
            // Storage can be unavailable; links stay links.
            players = false;
        }
    }
    return players;
}

export function setYouTubePlayers(on) {
    players = on;
    try {
        if (on) localStorage.setItem(PLAYERS, '1');
        else localStorage.removeItem(PLAYERS);
    } catch {
        // As above; the choice lasts until the page reloads.
    }
    playersListeners.forEach((fn) => fn(on));
}

// onYouTubePlayers calls fn with the choice whenever it changes, until the
// function it returns is called.
export function onYouTubePlayers(fn) {
    playersListeners.add(fn);
    return () => playersListeners.delete(fn);
}

// The one form the den writes a YouTube link in: the video's ID, and its
// start time if it has one. Anything else, which a hostile den or sender
// could send, isn't played.
const WATCH = /^https:\/\/www\.youtube\.com\/watch\?v=([A-Za-z0-9_-]{11})(?:&t=([0-9hms]{1,16}))?$/;

// videoOf is the video a link plays, as { id, start } with start in
// seconds, or null for a link that isn't in the den's form.
export function videoOf(url) {
    const m = WATCH.exec(url);
    if (!m) return null;
    const start = m[2] === undefined ? 0 : startSeconds(m[2]);
    return start === null ? null : { id: m[1], start };
}

// startSeconds reads a start time as denproto's startTime accepts one:
// seconds, with or without an s, or hours, minutes and seconds like
// 1h2m3s, each optional but in that order. Anything else is null, and a
// time too large to count starts at the beginning.
export function startSeconds(t) {
    if (!t || t.length > 16) return null;
    const plain = /^(\d+)s?$/.exec(t);
    const hms = plain ? null : /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(t);
    if (!plain && !hms) return null;
    const n = plain ? Number(plain[1]) : Number(hms[1] || 0) * 3600 + Number(hms[2] || 0) * 60 + Number(hms[3] || 0);
    return Number.isSafeInteger(n) ? n : 0;
}

// shownLink is a link as this browser shows it.
export function shownLink(url, preferOld = oldReddit()) {
    return preferOld && url.startsWith(REDDIT_POST) ? 'https://old.reddit.com/' + url.slice('https://www.reddit.com/'.length) : url;
}

// trimLink drops what most likely ends a sentence rather than a link:
// punctuation at the end, and a ) there that closes no ( in the link, so a
// link in parentheses loses the closing one but a Wikipedia article's
// title keeps its own. denproto's trimLink is the same rule.
export function trimLink(url) {
    for (;;) {
        const last = url[url.length - 1];
        if ('.,;:!?]'.includes(last)) url = url.slice(0, -1);
        else if (last === ')' && count(url, ')') > count(url, '(')) url = url.slice(0, -1);
        else return url;
    }
}

function count(s, c) {
    return s.split(c).length - 1;
}
