// Relay's player (M20.2), in an AudioWorklet: another of the member's
// devices' sound, which the page's worker hands it a packet at a time,
// each its place in the stream, in frames, 4 bytes little-endian, its
// rate, 4 more, then its frames of 16-bit stereo, little-endian.
//
// It keeps a buffer of the source's frames: the member's choice, Low's 40
// ms or High's 150, or one and a half of the longest its output has
// lately waited between calls and 10 ms more where that's more, as a
// phone's output asks in long strides. It plays up to 0.5% fast or slow
// to stay there, since the source's clock and the output's drift apart,
// and skips ahead past a pile, as after a stall. Where the path lost a
// packet it fades out before the gap and in after it; where its buffer
// runs dry it fades out what's left, and in again once the buffer is
// back. Every two seconds it posts what it counted.
//
// processorOptions.buffer is the member's choice, in milliseconds. The
// page posts {port}, the worker's end of a channel, which then carries
// the packets.

// HEADER is a packet's place and rate.
export const HEADER = 8;
// The ring holds over five seconds at 48 kHz.
const RING = 1 << 18;
// FADE is how many of the output's frames a fade takes: 2.5 ms at 48 kHz.
export const FADE = 120;
// The most the player speeds up or slows down, and how much it does for
// each millisecond its smoothed buffer is off its target.
export const MOST_STEER = 0.005;
const STEER_GAIN = 0.0002;
const SMOOTH = 0.002;
// A buffer this far past its target, in seconds, skips back to it; a gap
// this long starts the stream again rather than fill it with silence, as
// does a jump back as long.
const PILE = 0.15;
const MOST_GAP = 0.5;
// The most the target follows a long stride to, in milliseconds, and how
// many reports' strides it follows.
const MOST_TARGET = 500;
const STRIDES = 10;
// REPORT_EVERY is how often, in seconds of the context's time, it reports.
export const REPORT_EVERY = 2;

// Player is the processor's work, as plain code: packet takes a packet,
// render fills a block of the output, given the wall clock in
// milliseconds, and report says what it counted since the last.
export class Player {
    constructor({ buffer, rate }) {
        this.floor = buffer;
        this.target = buffer;
        this.out = rate;
        this.rate = rate;
        this.left = new Float32Array(RING);
        this.right = new Float32Array(RING);
        // Frames written ever, and read, with a fraction.
        this.written = 0;
        this.read = 0;
        this.next = undefined;
        this.playing = false;
        // How far into a fade in the output is, since playing (re)started,
        // and whether the next packet fades in after a gap.
        this.rise = 0;
        this.after = false;
        this.smooth = 0;
        this.strides = [];
        this.counts = { packets: 0, lost: 0, stale: 0, dry: 0, skips: 0 };
        this.period();
    }

    // period starts what a report counts.
    period() {
        this.stride = 0;
        this.least = Infinity;
        this.depths = 0;
        this.blocks = 0;
        this.steered = 0;
        this.squares = 0;
        this.played = 0;
    }

    // restart drops what's buffered, for a stream at rate starting again.
    restart(rate) {
        this.rate = rate;
        this.read = this.written;
        this.next = undefined;
        this.playing = false;
    }

    packet(buf) {
        if (buf.byteLength < HEADER + 4) return;
        const view = new DataView(buf);
        const place = view.getUint32(0, true);
        const rate = view.getUint32(4, true);
        if (rate < 8000 || rate > 192000) return;
        const samples = new Int16Array(buf, HEADER, (buf.byteLength - HEADER) >> 2 << 1);
        const frames = samples.length >> 1;
        if (rate !== this.rate) this.restart(rate);
        if (this.next !== undefined) {
            const gap = (place - this.next) | 0;
            // A jump of half a second either way is a stream started
            // again, as on a link made again, whose places start over.
            if (Math.abs(gap) > rate * MOST_GAP) {
                this.restart(rate);
            } else if (gap < 0) {
                this.counts.stale++;
                return;
            } else if (gap > 0) {
                this.counts.lost += Math.round(gap / frames);
                this.gap(gap);
            }
        }
        const start = this.written;
        for (let i = 0; i < frames; i++) {
            const at = (start + i) & (RING - 1);
            let gain = 1;
            if (this.after && i < FADE) gain = (i + 1) / FADE;
            this.left[at] = (samples[2 * i] / 32768) * gain;
            this.right[at] = (samples[2 * i + 1] / 32768) * gain;
        }
        this.after = false;
        this.written += frames;
        this.next = (place + frames) >>> 0;
        this.counts.packets++;
    }

    // gap fades out what's buffered before a gap the path left, as far as
    // it's not yet played, fills the gap with silence, and has the next
    // packet fade in.
    gap(frames) {
        const from = Math.max(this.written - FADE, Math.ceil(this.read));
        const span = this.written - from;
        for (let i = 0; i < span; i++) {
            const at = (from + i) & (RING - 1);
            const gain = 1 - (i + 1) / span;
            this.left[at] *= gain;
            this.right[at] *= gain;
        }
        for (let i = 0; i < frames; i++) {
            const at = (this.written + i) & (RING - 1);
            this.left[at] = 0;
            this.right[at] = 0;
        }
        this.written += frames;
        this.after = true;
    }

    render(outL, outR, wall) {
        const n = outL.length;
        if (this.called !== undefined) this.stride = Math.max(this.stride, wall - this.called);
        this.called = wall;
        const target = (this.target * this.rate) / 1000;
        let depth = this.written - this.read;
        this.least = Math.min(this.least, depth);
        this.depths += depth;
        this.blocks++;
        if (!this.playing && depth >= target) {
            this.playing = true;
            this.smooth = depth;
            this.rise = 0;
        }
        if (this.playing && depth > target + this.rate * PILE) {
            this.read = this.written - target;
            depth = target;
            this.smooth = depth;
            this.rise = 0;
            this.counts.skips++;
        }
        if (!this.playing) {
            outL.fill(0);
            outR.fill(0);
            return;
        }
        this.smooth += (depth - this.smooth) * SMOOTH;
        const off = ((this.smooth - target) / this.rate) * 1000;
        const steer = Math.max(-MOST_STEER, Math.min(MOST_STEER, off * STEER_GAIN));
        this.steered += steer;
        const step = (this.rate / this.out) * (1 + steer);
        // What's left plays faded out where the buffer runs dry.
        let frames = n;
        const dry = depth < n * step + 2;
        if (dry) frames = Math.max(0, Math.floor((depth - 2) / step));
        for (let i = 0; i < frames; i++) {
            const at = this.read + i * step;
            const whole = Math.floor(at);
            const part = at - whole;
            const a = whole & (RING - 1);
            const b = (whole + 1) & (RING - 1);
            let gain = 1;
            if (this.rise < FADE) gain = ++this.rise / FADE;
            if (dry) gain *= (frames - i) / (frames + 1);
            const l = (this.left[a] + (this.left[b] - this.left[a]) * part) * gain;
            const r = (this.right[a] + (this.right[b] - this.right[a]) * part) * gain;
            outL[i] = l;
            outR[i] = r;
            this.squares += l * l + r * r;
        }
        for (let i = frames; i < n; i++) {
            outL[i] = 0;
            outR[i] = 0;
        }
        this.played += frames;
        this.read += frames * step;
        if (dry) {
            this.playing = false;
            this.counts.dry++;
        }
    }

    // report says what was counted, and moves the target to follow the
    // output's longest strides lately.
    report() {
        const toMs = (frames) => Math.round((frames / this.rate) * 10000) / 10;
        const rms = this.played ? Math.sqrt(this.squares / (2 * this.played)) : 0;
        const r = {
            ...this.counts,
            playing: this.playing,
            target: this.target,
            least: this.blocks ? toMs(Math.max(0, this.least)) : 0,
            depth: this.blocks ? toMs(this.depths / this.blocks) : 0,
            steer: this.blocks ? Math.round((this.steered / this.blocks) * 1e6) : 0,
            level: rms > 0 ? Math.round(200 * Math.log10(rms)) / 10 : null,
            stride: this.stride,
            rate: this.rate,
        };
        this.strides.push(this.stride);
        if (this.strides.length > STRIDES) this.strides.shift();
        this.target = Math.min(MOST_TARGET, Math.max(this.floor, Math.round(1.5 * Math.max(...this.strides) + 10)));
        this.period();
        return r;
    }
}

class RelayProcessor extends AudioWorkletProcessor {
    constructor(options) {
        super();
        this.player = new Player({ buffer: options.processorOptions.buffer, rate: sampleRate });
        this.since = currentTime;
        this.port.onmessage = (e) => {
            if (!e.data?.port) return;
            e.data.port.onmessage = (m) => this.player.packet(m.data);
        };
    }

    process(inputs, outputs) {
        const out = outputs[0];
        this.player.render(out[0], out[1] || out[0], Date.now());
        if (currentTime - this.since >= REPORT_EVERY) {
            this.since = currentTime;
            this.port.postMessage(this.player.report());
        }
        return true;
    }
}

registerProcessor('relay', RelayProcessor);
