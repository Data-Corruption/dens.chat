// The local event stream, shared by every view: one WebSocket to the
// service, which pushes den statuses and den events. The service is on this
// computer, so a lost stream retries quickly; after a reconnect, views
// reload, since events may have been missed while it was down.

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
