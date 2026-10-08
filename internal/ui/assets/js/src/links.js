// How the page shows links (M3). The den, and a DM's sender, write every
// link to a Reddit post as https://www.reddit.com/comments/…, so showing
// it on old.reddit.com, for a browser that prefers it, takes only another
// host.

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
