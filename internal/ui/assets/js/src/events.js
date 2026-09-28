// The local event stream, shared by every view: one WebSocket to the
// service, which pushes den statuses and den events. It reconnects with
// backoff; after a reconnect, views reload, since events may have been
// missed while it was down.

const listeners = new Set();
const connectionListeners = new Set();
let socket = null;
let started = false;
let connected = false;
let everConnected = false;
let delay = 500;

function start() {
    if (started) return;
    started = true;
    connect();
}

function connect() {
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    socket = new WebSocket(`${scheme}//${window.location.host}/api/events`);
    socket.addEventListener('open', () => {
        delay = 500;
        connected = true;
        connectionListeners.forEach((fn) => fn(true));
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
        dispatch(message);
    });
    socket.addEventListener('close', () => {
        connected = false;
        connectionListeners.forEach((fn) => fn(false));
        setTimeout(connect, delay + Math.random() * delay);
        delay = Math.min(delay * 2, 10000);
    });
}

function dispatch(message) {
    listeners.forEach((fn) => fn(message));
}

// onEvent calls fn with every message from the service; it returns a
// function that stops.
export function onEvent(fn) {
    start();
    listeners.add(fn);
    return () => listeners.delete(fn);
}

// onConnection calls fn with whether the stream is up, now and on every
// change.
export function onConnection(fn) {
    start();
    connectionListeners.add(fn);
    fn(connected || !everConnected);
    return () => connectionListeners.delete(fn);
}
