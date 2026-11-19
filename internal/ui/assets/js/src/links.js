// How the page shows links (M3). The den, and a DM's sender, write every
// link to a Reddit post as https://www.reddit.com/comments/…, so showing
// it on old.reddit.com, for a browser that prefers it, takes only another
// host. Every YouTube link is written as https://www.youtube.com/watch?v=…,
// or a Short's as https://www.youtube.com/shorts/…, which is all a
// browser that turned YouTube's players on (M4.1) needs to find the video
// to play, and its shape.

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

// YouTube's players and Steam's cards each show their site the member's
// IP address. A browser new to Dens asks whether to show them as it opens
// Dens (M22.7), and keeps the answer, '1' or '0', for each; one that had
// opened Dens before keeps what it had, nothing being off.
const PLAYERS = 'DENS_YOUTUBE';
const STEAM = 'DENS_STEAM';

// newToDens says whether a browser's storage holds nothing of Dens's: no
// setting, and no channel last opened.
export function newToDens(storage) {
    for (let i = 0; i < storage.length; i++) {
        if (storage.key(i)?.startsWith('DENS_')) return false;
    }
    return true;
}

// isNew is read as the page loads, before anything on it is kept.
let isNew = false;
try {
    isNew = newToDens(localStorage);
} catch {
    // Storage can be unavailable; nothing would keep the answer.
}
let answered = false;

// asksLinkPreviews says whether the page asks about link previews: in a
// browser new to Dens, until it answers.
export function asksLinkPreviews() {
    if (!isNew || answered) return false;
    try {
        return localStorage.getItem(PLAYERS) === null && localStorage.getItem(STEAM) === null;
    } catch {
        return false;
    }
}

// setLinkPreviews keeps the answer: whether YouTube's players and Steam's
// cards show.
export function setLinkPreviews({ youtube, steam: cards }) {
    answered = true;
    setYouTubePlayers(youtube);
    setSteamCards(cards);
}

const playersListeners = new Set();
let players = null;

// youTubePlayers reports whether this browser plays YouTube videos where
// they're linked (M4.1).
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
        localStorage.setItem(PLAYERS, on ? '1' : '0');
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

const steamListeners = new Set();
let steam = null;

// steamCards reports whether this browser shows Steam's cards where games
// are linked (M22.2).
export function steamCards() {
    if (steam === null) {
        try {
            steam = localStorage.getItem(STEAM) === '1';
        } catch {
            // Storage can be unavailable; links stay links.
            steam = false;
        }
    }
    return steam;
}

export function setSteamCards(on) {
    steam = on;
    try {
        localStorage.setItem(STEAM, on ? '1' : '0');
    } catch {
        // As above; the choice lasts until the page reloads.
    }
    steamListeners.forEach((fn) => fn(on));
}

// onSteamCards calls fn with the choice whenever it changes, until the
// function it returns is called.
export function onSteamCards(fn) {
    steamListeners.add(fn);
    return () => steamListeners.delete(fn);
}

// A link to a game on Steam: its store page, its age check, or its
// community hub, by the app's ID, with a name or anything else after it.
// The den doesn't shorten Steam's links, so they come as members wrote
// them; the ID is all the page takes from one.
const STEAM_APP = /^https:\/\/(?:store\.steampowered\.com\/(?:agecheck\/)?app|steamcommunity\.com\/app)\/([1-9][0-9]{0,9})(?:[/?#].*)?$/;
const MAX_APP = 4294967295;

// steamApp is the app a link to a game on Steam names, as its ID, or null
// for any other link.
export function steamApp(url) {
    const m = STEAM_APP.exec(url);
    return m && Number(m[1]) <= MAX_APP ? m[1] : null;
}

// The forms the den writes a YouTube link in: the video's ID, and its
// start time if it has one; or a Short's ID. Anything else, which a hostile
// den or sender could send, isn't played.
const WATCH = /^https:\/\/www\.youtube\.com\/watch\?v=([A-Za-z0-9_-]{11})(?:&t=([0-9hms]{1,16}))?$/;
const SHORT = /^https:\/\/www\.youtube\.com\/shorts\/([A-Za-z0-9_-]{11})$/;

// videoOf is the video a link plays, as { id, start } with start in
// seconds, and short for a Short, or null for a link that isn't in the
// den's form.
export function videoOf(url) {
    const short = SHORT.exec(url);
    if (short) return { id: short[1], start: 0, short: true };
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

// A link to a message (M11) is its den's address, then the den's ID and
// the message's channel and ID in the fragment, as Copy link writes it:
// https://den.example.com/#m/<den ID>/41/90210. It opens by the den's ID,
// which the den's identity key proves on every connection, so it still
// opens once the den moves to another address, and never in another den
// that takes the old one. The address
// names a den the member hasn't joined, and opens in a browser, which
// never sends the fragment, so the den learns nothing of which message.
// The IDs are as a den writes them: its own, 32 bytes in base64url, and
// the channel's and message's, 1 to 19 digits without a leading zero, no
// larger than a signed 64-bit integer.
const MESSAGE_LINK = /^https?:\/\/([^/?#@\s]+)\/?#m\/([A-Za-z0-9_-]{43})\/([1-9][0-9]{0,18})\/([1-9][0-9]{0,18})$/;
const MAX_ID = '9223372036854775807';

const fitsID = (id) => id.length < MAX_ID.length || id <= MAX_ID;

// messageLink reads a link to a message as { den, host, channel, message },
// where den is the den's ID, or null for any other link.
export function messageLink(url) {
    const m = MESSAGE_LINK.exec(url);
    if (!m || !fitsID(m[3]) || !fitsID(m[4])) return null;
    return { den: m[2], host: m[1].toLowerCase(), channel: m[3], message: m[4] };
}

// linkTo is the link to a message in the den with ID denID at denURL, as
// Copy link writes it.
export function linkTo(denURL, denID, channelID, messageID) {
    return `${denURL}/#m/${denID}/${channelID}/${messageID}`;
}

// A link to an event (M17.5) is its den's address, then the den's ID and
// the event's in the fragment, as an event's Copy link writes it:
// https://den.example.com/#e/<den ID>/12. It opens by the den's ID, as a
// link to a message does, and the den never learns from the address which
// event it names.
const EVENT_LINK = /^https?:\/\/([^/?#@\s]+)\/?#e\/([A-Za-z0-9_-]{43})\/([1-9][0-9]{0,18})$/;

// eventLink reads a link to an event as { den, host, event }, where den is
// the den's ID, or null for any other link.
export function eventLink(url) {
    const m = EVENT_LINK.exec(url);
    if (!m || !fitsID(m[3])) return null;
    return { den: m[2], host: m[1].toLowerCase(), event: m[3] };
}

// eventLinkTo is the link to an event in the den with ID denID at denURL,
// as Copy link writes it.
export function eventLinkTo(denURL, denID, eventID) {
    return `${denURL}/#e/${denID}/${eventID}`;
}

// shownLink is a link as this browser shows it.
export function shownLink(url, preferOld = oldReddit()) {
    return preferOld && url.startsWith(REDDIT_POST) ? 'https://old.reddit.com/' + url.slice('https://www.reddit.com/'.length) : url;
}

// trimLink drops what most likely ends a sentence rather than a link:
// punctuation at the end, and a ) there that closes no ( in the link, so a
// link in parentheses loses the closing one but a Wikipedia article's
// title keeps its own. denproto's trimLink is the same rule. It counts the
// parentheses once, so a link ending in many takes no longer (M8).
export function trimLink(url) {
    const opens = count(url, '(');
    let closes = count(url, ')');
    let end = url.length;
    for (; end > 0; end--) {
        const last = url[end - 1];
        if ('.,;:!?]'.includes(last)) continue;
        if (last !== ')' || closes <= opens) break;
        closes--;
    }
    return url.slice(0, end);
}

function count(s, c) {
    return s.split(c).length - 1;
}
