// The local event stream, shared by every view: one WebSocket to the
// service, which pushes den statuses and den events. The page sends on it
// too: the channel it shows in each den, and typing. The service is on
// this computer, so a lost stream retries quickly; after a reconnect,
// views reload, since events may have been missed while it was down.

const MIN_DELAY = 250;
const MAX_DELAY = 3000;

const listeners = new Set();
const connectionListeners = new Set();
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
    connect();
}

function connect() {
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    socket = new WebSocket(`${scheme}//${window.location.host}/api/events`);
    socket.addEventListener('open', () => {
        delay = MIN_DELAY;
        focus.forEach((channel, den) => send({ t: 'focus', d: { den, channel } }));
        if (down) {
            down = false;
            connectionListeners.forEach((fn) => fn(true));
        }
        if (everConnected) dispatch({ t: 'reconnected' });
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
        if (!down) {
            down = true;
            connectionListeners.forEach((fn) => fn(false));
        }
        retry = setTimeout(() => {
            retry = null;
            connect();
        }, delay * (0.5 + Math.random() / 2));
        delay = Math.min(delay * 2, MAX_DELAY);
    });
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

// onConnection calls fn with whether the stream is up, now and on every
// change.
export function onConnection(fn) {
    start();
    connectionListeners.add(fn);
    fn(!down);
    return () => connectionListeners.delete(fn);
}
