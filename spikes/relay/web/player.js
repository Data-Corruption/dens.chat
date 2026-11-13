// The player: an audio worklet fed the probe's packets. With via=worker,
// the default, a worker holds the socket and hands each packet straight to
// the worklet, so the page's own thread carries none of the sound; with
// via=main the page's thread does, as the simplest player would.
const q = new URLSearchParams(location.search);
const name = q.get('name') || 'page';
const via = q.get('via') || 'worker';
const out = document.getElementById('out');
let ctx, node, worker, socket, timers = [], made = 0, report = () => {};
const seen = {};

function show(m) {
  seen[m.kind] = m;
  out.textContent = Object.values(seen).map((v) => JSON.stringify(v)).join('\n');
}

// stop ends the player, so another can be made: question 9 makes one
// before the microphone opens and one after.
async function stop() {
  timers.forEach(clearInterval);
  timers = [];
  if (worker) worker.terminate();
  worker = null;
  if (socket) { socket.onclose = null; socket.close(); }
  socket = null;
  if (ctx) await ctx.close();
  ctx = null;
}

async function start() {
  document.getElementById('start').disabled = true;
  made++;
  const info = await (await fetch('/info')).json();
  const target = Number(q.get('buffer') || info.buffer);
  ctx = new AudioContext({ sampleRate: info.rate, latencyHint: 'interactive' });
  await ctx.audioWorklet.addModule('worklet.js');
  node = new AudioWorkletNode(ctx, 'relay', {
    numberOfInputs: 0, outputChannelCount: [2], processorOptions: { target, adapt: q.get('adapt') !== '0' },
  });
  node.connect(ctx.destination);
  const batch = via === 'worker' ? Number(q.get('batch') || 0) : 0;
  const url = `ws://${location.host}/ws?name=${encodeURIComponent(name)}` + (batch > 1 ? `&batch=${batch}` : '');
  if (via === 'worker') {
    const pipe = new MessageChannel();
    node.port.postMessage({ port: pipe.port2 }, [pipe.port2]);
    worker = new Worker('worker.js');
    worker.postMessage({ url, port: pipe.port1, batched: batch > 1 }, [pipe.port1]);
    report = (m) => worker.postMessage({ report: m });
    node.port.onmessage = (e) => show(e.data);
  } else {
    let ws;
    const connect = () => {
      ws = socket = new WebSocket(url);
      ws.binaryType = 'arraybuffer';
      ws.onmessage = (e) => { if (typeof e.data !== 'string') node.port.postMessage(e.data, [e.data]); };
      ws.onclose = () => setTimeout(connect, 1000);
    };
    connect();
    report = (m) => { if (ws.readyState === 1) ws.send(JSON.stringify(m)); };
    node.port.onmessage = (e) => { show(e.data); report(e.data); };
  }
  await ctx.resume();
  const state = (why) => {
    if (!ctx) return;
    const m = {
      kind: 'ctx', why, made, state: ctx.state, rate: ctx.sampleRate, target, via,
      base: Math.round(ctx.baseLatency * 1e4) / 10, output: Math.round((ctx.outputLatency || 0) * 1e4) / 10,
      visible: document.visibilityState, focus: document.hasFocus(),
    };
    show(m);
    report(m);
  };
  state('started');
  timers.push(setInterval(() => state('tick'), 10000));
  ctx.onstatechange = () => state('statechange');
  for (const ev of ['visibilitychange', 'freeze', 'resume']) document.addEventListener(ev, () => state(ev));
  // A page's timers slow when it's hidden: how late this one fires says how much.
  let last = performance.now(), worst = 0;
  timers.push(setInterval(() => { const t = performance.now(); worst = Math.max(worst, t - last - 100); last = t; }, 100));
  timers.push(setInterval(() => { report({ kind: 'timer', lateMax: Math.round(worst) }); worst = 0; }, 10000));
}

// call=N walks question 9 by itself, N seconds a step: the player as it
// is, made before any microphone; the microphone opened, as a call opens
// it; the player made again, after the microphone; the microphone closed;
// and the player made once more.
async function call(step) {
  const wait = () => new Promise((r) => setTimeout(r, step * 1000));
  const say = (what, extra = {}) => { const m = { kind: 'call', what, ...extra }; show(m); report(m); };
  await wait();
  let mic;
  try {
    mic = await navigator.mediaDevices.getUserMedia({ audio: true });
    say('microphone open, the player made before it');
  } catch (e) {
    say('no microphone', { error: String(e) });
    return;
  }
  await wait();
  await again();
  say('the player made again, after the microphone');
  await wait();
  mic.getTracks().forEach((t) => t.stop());
  say('microphone closed, the player made during the call');
  await wait();
  await again();
  say('the player made again, after the call');
}

// again makes the player anew. Chromium keeps a context's output stream
// for some seconds after it closes and hands it to the next context, as
// it was opened; only after that does a new context get a stream of its
// own, opened as things are now.
async function again() {
  await stop();
  await new Promise((r) => setTimeout(r, 8000));
  await start();
}

document.getElementById('start').onclick = start;
if (q.get('auto')) {
  start().then(() => { if (q.get('call')) return call(Number(q.get('call'))); })
    .catch((e) => { out.textContent = String(e); document.getElementById('start').disabled = false; });
}
