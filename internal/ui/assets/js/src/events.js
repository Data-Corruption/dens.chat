// The local event stream, shared by every view: one WebSocket to the
// service, which pushes den statuses and den events. The page sends on it
// too: the channel it shows in each den, and typing. The service is on
// this computer, so a lost stream retries quickly; after a reconnect,
// views reload, since events may have been missed while it was down.
//
// While the stream is down, the page asks the service over plain HTTP
// whether it's back, and still knows this browser, before opening another
// socket. Firefox holds back a socket to an address whose last ones failed,
// longer after each failure, up to a minute, so sockets retried while the
// service restarts, or refused because the instance behind this address
// was replaced, would keep this page, and new ones, waiting long after.
// A socket that still hasn't opened after a few seconds counts as down, so
// the page says it's out of touch rather than looking fine while it hears
// nothing, and its views reload once it opens.

const MIN_DELAY = 250;
const MAX_DELAY = 3000;
const OPEN_WAIT = 3000;

const listeners = new Set();
const connectionListeners = new Set();
const unpairedListeners = new Set();
let socket = null;
let started = false;
let down = false;
let everConnected = false;
let delay = MIN_DELAY;
let retry = null;
// The service sends den statuses when the stream opens and when they
// change, so a view that starts listening later gets the latest from here.
let latestDens = null;
// The channel shown in each den, sent again after a reconnect, since the
// service forgets it when the stream closes.
const focus = new Map();

function start() {
    if (started) return;
    started = true;
    connect();
    // A tab coming back into view, or the network returning, is worth a try
    // without waiting out the delay.
    document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && retryNow());
    window.addEventListener('online', retryNow);
}

function retryNow() {
    if (!retry) return;
    clearTimeout(retry);
    retry = null;
    reconnect();
}

// reconnect opens the socket again once the service answers, and tries
// again later if it doesn't. It stops for good once the service no longer
// knows this browser.
async function reconnect() {
    let res = null;
    try {
        res = await fetch('/api/status', { cache: 'no-store' });
    } catch {
        // Not back yet.
    }
    if (res?.status === 401) unpairedListeners.forEach((fn) => fn());
    else if (res?.ok) connect();
    else schedule();
}

function schedule() {
    retry = setTimeout(() => {
        retry = null;
        reconnect();
    }, delay * (0.5 + Math.random() / 2));
    delay = Math.min(delay * 2, MAX_DELAY);
}

function connect() {
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const s = new WebSocket(`${scheme}//${window.location.host}/api/events`);
    socket = s;
    const slow = setTimeout(() => {
        if (socket === s && s.readyState === WebSocket.CONNECTING) goneDown();
    }, OPEN_WAIT);
    socket.addEventListener('open', () => {
        clearTimeout(slow);
        delay = MIN_DELAY;
        focus.forEach((channel, den) => send({ t: 'focus', d: { den, channel } }));
        const missed = everConnected || down;
        if (down) {
            down = false;
            connectionListeners.forEach((fn) => fn(true));
        }
        if (missed) dispatch({ t: 'reconnected' });
        everConnected = true;
    });
    socket.addEventListener('message', (event) => {
        let message;
        try {
            message = JSON.parse(event.data);
        } catch {
            return;
        }
        if (message.t === 'dens') latestDens = message;
        dispatch(message);
    });
    socket.addEventListener('close', () => {
        clearTimeout(slow);
        goneDown();
        schedule();
    });
}

function goneDown() {
    if (down) return;
    down = true;
    connectionListeners.forEach((fn) => fn(false));
}

function dispatch(message) {
    listeners.forEach((fn) => fn(message));
}

// send writes to the service if the stream is up; these messages are
// hints, so one lost while it's down doesn't matter.
function send(message) {
    if (socket && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(message));
}

// showChannel tells the service which channel of a den this page shows,
// '' for none, so typing there reaches this member.
export function showChannel(den, channel) {
    if ((focus.get(den) || '') === channel) return;
    if (channel) focus.set(den, channel);
    else focus.delete(den);
    send({ t: 'focus', d: { den, channel } });
}

// sendTyping tells a den this member is typing in a channel.
export function sendTyping(den, channel) {
    send({ t: 'typing', d: { den, channel } });
}

// sendCall sends a message of this page's call, and reports whether the
// stream was up to take it. Unlike the hints above, a call needs to know:
// the service ends a page's call when its stream closes.
export function sendCall(t, d) {
    if (!socket || socket.readyState !== WebSocket.OPEN) return false;
    socket.send(JSON.stringify({ t, d }));
    return true;
}

// onEvent calls fn with every message from the service, starting with the
// latest den statuses if they already came; it returns a function that
// stops.
export function onEvent(fn) {
    start();
    listeners.add(fn);
    if (latestDens) {
        const replay = latestDens;
        queueMicrotask(() => listeners.has(fn) && fn(replay));
    }
    return () => listeners.delete(fn);
}

// onUnpaired calls fn once the service no longer knows this browser, as
// after its instance was replaced, and the stream has stopped.
export function onUnpaired(fn) {
    unpairedListeners.add(fn);
    return () => unpairedListeners.delete(fn);
}

// onConnection calls fn with whether the stream is up, now and on every
// change.
export function onConnection(fn) {
    start();
    connectionListeners.add(fn);
    fn(!down);
    return () => connectionListeners.delete(fn);
}
