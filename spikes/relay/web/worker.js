// Holds the probe's socket off the page's thread: packets go to the
// worklet's port as they come, and the worklet's counts go back up.
let port, ws, batched = false;

function connect(url) {
  ws = new WebSocket(url);
  ws.binaryType = 'arraybuffer';
  ws.onopen = () => { while (waiting.length) ws.send(JSON.stringify(waiting.shift())); };
  ws.onmessage = (m) => {
    if (typeof m.data === 'string') return;
    if (!batched) { port.postMessage(m.data, [m.data]); return; }
    // Several packets to a message, each under its length.
    const view = new DataView(m.data);
    for (let at = 0; at + 4 <= m.data.byteLength;) {
      const n = view.getUint32(at, true);
      const pkt = m.data.slice(at + 4, at + 4 + n);
      port.postMessage(pkt, [pkt]);
      at += 4 + n;
    }
  };
  ws.onclose = () => setTimeout(() => connect(url), 1000);
}

// What's said before the socket opens waits for it.
const waiting = [];

function send(o) {
  if (ws && ws.readyState === 1) ws.send(JSON.stringify(o));
  else if (waiting.length < 50) waiting.push(o);
}

onmessage = (e) => {
  if (e.data.url) {
    port = e.data.port;
    batched = !!e.data.batched;
    port.onmessage = (m) => send(m.data);
    connect(e.data.url);
  } else if (e.data.report) {
    send(e.data.report);
  }
};
