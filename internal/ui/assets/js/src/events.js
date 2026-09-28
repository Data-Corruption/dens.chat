// The local event stream: the service pushes state changes over a
// WebSocket, and this reconnects with backoff when it drops.

export function subscribeEvents(onMessage, onConnected) {
    let socket = null;
    let stopped = false;
    let delay = 500;
    let timer = null;

    function connect() {
        const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        socket = new WebSocket(`${scheme}//${window.location.host}/api/events`);
        socket.addEventListener('open', () => {
            delay = 500;
            onConnected?.(true);
        });
        socket.addEventListener('message', (event) => {
            let message;
            try {
                message = JSON.parse(event.data);
            } catch {
                return;
            }
            onMessage(message);
        });
        socket.addEventListener('close', () => {
            onConnected?.(false);
            if (stopped) return;
            timer = setTimeout(connect, delay + Math.random() * delay);
            delay = Math.min(delay * 2, 10000);
        });
    }

    connect();
    return () => {
        stopped = true;
        clearTimeout(timer);
        socket?.close();
    };
}
