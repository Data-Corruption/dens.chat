// Plays the probe's packets: a ring of the source's frames, held at a
// target depth by playing a hair faster or slower, so the source's clock
// and this output's never drift into a gap or a pile.
const HEADER = 24;
const RING = 1 << 18; // frames, over five seconds at 48 kHz

class Relay extends AudioWorkletProcessor {
  constructor(options) {
    super();
    this.floor = options.processorOptions.target; // milliseconds: the least it keeps
    this.adapt = options.processorOptions.adapt !== false;
    this.target = this.floor;
    this.gaps = []; // the longest wait between calls in each of the last reports
    this.left = new Float32Array(RING);
    this.right = new Float32Array(RING);
    this.written = 0; // frames put in, ever
    this.read = 0; // frames taken out, ever, with a fraction
    this.rate = sampleRate;
    this.playing = false;
    this.smooth = 0;
    this.feed = null;
    this.reset();
    this.totals = { underruns: 0, skips: 0, lost: 0, stale: 0, packets: 0 };
    this.port.onmessage = (e) => {
      if (e.data.port) {
        this.feed = e.data.port;
        this.feed.onmessage = (m) => this.packet(m.data);
      } else {
        this.packet(e.data);
      }
    };
  }

  reset() {
    this.since = currentTime;
    this.min = Infinity;
    this.max = 0;
    this.sum = 0;
    this.blocks = 0;
    this.steer = 0;
    this.silent = 0;
    this.callGap = 0;
    this.arriveGap = 0;
  }

  packet(buf) {
    const view = new DataView(buf);
    const seq = view.getUint32(0, true);
    const rate = view.getUint32(4, true);
    const at = Date.now();
    if (this.arrived) this.arriveGap = Math.max(this.arriveGap, at - this.arrived);
    this.arrived = at;
    if (rate !== this.rate) {
      this.rate = rate;
      this.read = this.written;
      this.playing = false;
    }
    if (this.seq !== undefined) {
      const ahead = (seq - this.seq) | 0;
      if (ahead <= 0) {
        this.totals.stale++;
        return;
      }
      if (ahead > 1) {
        // Silence where the lost packets were, so what follows keeps its place.
        this.totals.lost += ahead - 1;
        const frames = Math.min((ahead - 1) * (this.size || 240), sampleRate / 2);
        for (let i = 0; i < frames; i++) {
          const at = (this.written + i) & (RING - 1);
          this.left[at] = 0;
          this.right[at] = 0;
        }
        this.written += frames;
      }
    }
    this.seq = seq;
    const samples = new Int16Array(buf, HEADER);
    const frames = samples.length >> 1;
    this.size = frames;
    for (let i = 0; i < frames; i++) {
      const at = (this.written + i) & (RING - 1);
      this.left[at] = samples[2 * i] / 32768;
      this.right[at] = samples[2 * i + 1] / 32768;
    }
    this.written += frames;
    this.totals.packets++;
  }

  process(inputs, outputs) {
    const left = outputs[0][0];
    const right = outputs[0][1] || left;
    const n = left.length;
    const wall = Date.now();
    if (this.called) this.callGap = Math.max(this.callGap, wall - this.called);
    this.called = wall;

    const target = (this.target * this.rate) / 1000;
    let depth = this.written - this.read;
    this.min = Math.min(this.min, depth);
    this.max = Math.max(this.max, depth);
    this.sum += depth;
    this.blocks++;

    if (!this.playing && depth >= target) {
      this.playing = true;
      this.smooth = depth;
    }
    if (this.playing && depth > target + this.rate * 0.15) {
      // A pile, as after a stall: skip to the target rather than play it late.
      this.read = this.written - target;
      depth = target;
      this.smooth = depth;
      this.totals.skips++;
    }
    if (this.playing) {
      this.smooth += (depth - this.smooth) * 0.002;
      const off = ((this.smooth - target) / this.rate) * 1000; // milliseconds from the target
      const steer = Math.max(-0.005, Math.min(0.005, off * 0.0002));
      const step = (this.rate / sampleRate) * (1 + steer);
      if (depth < n * step + 2) {
        this.playing = false;
        this.totals.underruns++;
      } else {
        this.steer += steer;
        for (let i = 0; i < n; i++) {
          const at = this.read + i * step;
          const whole = Math.floor(at);
          const part = at - whole;
          const a = whole & (RING - 1);
          const b = (whole + 1) & (RING - 1);
          left[i] = this.left[a] + (this.left[b] - this.left[a]) * part;
          right[i] = this.right[a] + (this.right[b] - this.right[a]) * part;
        }
        this.read += n * step;
      }
    }
    if (!this.playing) this.silent += n;

    if (currentTime - this.since >= 2) {
      const toMs = (frames) => Math.round((frames / this.rate) * 10000) / 10;
      const m = {
        kind: 'play', t: Math.round(currentTime * 10) / 10, playing: this.playing,
        min: toMs(this.min), avg: toMs(this.sum / this.blocks), max: toMs(this.max),
        steer: Math.round((this.steer / this.blocks) * 1e6), silent: toMs((this.silent * this.rate) / sampleRate),
        callGap: this.callGap, arriveGap: this.arriveGap, target: this.target, ...this.totals, src: this.rate, ctx: sampleRate,
      };
      this.port.postMessage(m);
      if (this.feed) this.feed.postMessage(m);
      // An output that asks in long strides, as a phone's or an emulator's
      // may, empties any buffer shorter than its stride: keep one and a
      // half of the longest lately, and 10 ms.
      this.gaps.push(this.callGap);
      if (this.gaps.length > 10) this.gaps.shift();
      if (this.adapt) this.target = Math.min(500, Math.max(this.floor, Math.round(1.5 * Math.max(...this.gaps) + 10)));
      this.reset();
    }
    return true;
  }
}

registerProcessor('relay', Relay);
