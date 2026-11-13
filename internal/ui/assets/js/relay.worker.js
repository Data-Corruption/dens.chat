// Holds the socket of a page's listening to another of the member's
// devices' sound (M20.2), off the page's thread: the service sends the
// packets several to a message, each under its length, 4 bytes
// little-endian, and the worker hands each to the player's worklet, on
// the port the page gives it. As the listening ends the service says why,
// which the worker tells the page, as it does the socket closing.
//
// The page posts {url, port} to start, and {stop} to close the socket.

// split cuts a message into its packets.
export function split(buf) {
    const view = new DataView(buf);
    const out = [];
    for (let at = 0; at + 4 <= buf.byteLength; ) {
        const n = view.getUint32(at, true);
        if (at + 4 + n > buf.byteLength) break;
        out.push(buf.slice(at + 4, at + 4 + n));
        at += 4 + n;
    }
    return out;
}

if (typeof WorkerGlobalScope !== 'undefined' && globalThis instanceof WorkerGlobalScope) {
    let socket = null;
    let port = null;
    globalThis.onmessage = (e) => {
        const m = e.data || {};
        if (m.url) {
            port = m.port;
            socket = new WebSocket(m.url);
            socket.binaryType = 'arraybuffer';
            socket.onmessage = (msg) => {
                if (typeof msg.data === 'string') {
                    let said = null;
                    try {
                        said = JSON.parse(msg.data);
                    } catch {
                        return;
                    }
                    if (said?.t === 'ended') postMessage({ t: 'ended', why: said.why || '' });
                    return;
                }
                for (const pkt of split(msg.data)) port.postMessage(pkt, [pkt]);
            };
            socket.onclose = () => postMessage({ t: 'closed' });
        } else if (m.stop && socket) {
            socket.onclose = null;
            socket.close();
        }
    };
}
