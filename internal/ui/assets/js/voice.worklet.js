// The voice processor (M3), in an AudioWorklet between the microphone and
// the call: RNNoise, unless the member turned it off, and then the gate
// that decides what's sent (M3.3).
//
// The audio thread hands the processor 128 samples at a time, and RNNoise
// and the gate work on frames of 480 (10 ms at 48 kHz), so the processor
// gathers frames and plays each back once it's done. Its output starts a frame behind, which
// keeps it from running dry: frames of 480 and blocks of 128 line up only
// every 1,920 samples, and a lead of 448 is the least that works. With
// RNNoise's own delay, a call hears the member 30 ms later than without it.
//
// A microphone may come with two channels or more, as some USB mics and
// laptops' microphone arrays do, and the processor takes their mix, as the
// browser would: the call carries one voice, in one channel.
//
// processorOptions.module is RNNoise's compiled module (wasm/rnnoise.wasm),
// which the page compiles under its CSP and posts here; it imports nothing.
// Without it the processor gates alone. processorOptions.gate, and later
// messages of {gate}, set the gate; the processor posts {level, speech,
// open} every 50 ms, for the settings' meter.

const FRAME = 480;
// RNNoise works on samples at 16-bit scale.
const SCALE = 32768;
// Reports go every 5 frames.
const REPORT = 5;

// The gate holds open for 30 frames (300 ms) after the last that passed,
// so words' ends and the gaps between them go too, and fades in over 2.5 ms
// and out over 20, so it never clicks.
const HOLD = 30;
const FADE_IN = 120;
const FADE_OUT = 960;
// RNNoise's estimate that a frame is speech, at or above which the
// automatic gate passes it.
const SPEECH = 0.6;

// Gate decides frame by frame what's sent: everything ("open"), nothing
// ("closed", as push to talk is while its key is up), what RNNoise takes
// for speech ("auto"), or what's at or above a level in dBFS ("level").
export class Gate {
    constructor() {
        this.mode = 'open';
        this.threshold = -50;
        this.held = 0;
        this.gain = 0;
    }

    set({ mode, threshold }) {
        if (['open', 'closed', 'auto', 'level'].includes(mode)) this.mode = mode;
        if (Number.isFinite(threshold)) this.threshold = threshold;
    }

    // decide takes a frame's level and RNNoise's estimate that it's speech,
    // or -1 without RNNoise, and says whether the frame goes.
    decide(level, speech) {
        let pass;
        switch (this.mode) {
            case 'open':
                return true;
            case 'closed':
                this.held = 0;
                return false;
            case 'auto':
                pass = speech >= SPEECH;
                break;
            default:
                pass = level >= this.threshold;
        }
        if (pass) this.held = HOLD;
        else if (this.held > 0) this.held--;
        return this.held > 0;
    }

    // fade scales a frame's samples on their way open or shut.
    fade(samples, open) {
        for (let i = 0; i < samples.length; i++) {
            this.gain = open ? Math.min(1, this.gain + 1 / FADE_IN) : Math.max(0, this.gain - 1 / FADE_OUT);
            samples[i] *= this.gain;
        }
    }
}

// levelOf is a frame's level in dBFS.
export function levelOf(samples) {
    let s = 0;
    for (let i = 0; i < samples.length; i++) s += samples[i] * samples[i];
    return 10 * Math.log10(Math.max(s / samples.length, 1e-12));
}

class VoiceProcessor extends AudioWorkletProcessor {
    constructor(options) {
        super();
        const { module, gate } = options.processorOptions || {};
        if (module) {
            const instance = new WebAssembly.Instance(module, {});
            const x = instance.exports;
            x._initialize();
            this.x = x;
            this.state = x.rnnoise_create(0);
            this.inPtr = x.malloc(FRAME * 4);
            this.outPtr = x.malloc(FRAME * 4);
        }
        this.gate = new Gate();
        if (gate) this.gate.set(gate);
        this.port.onmessage = (e) => {
            if (e.data?.gate) this.gate.set(e.data.gate);
        };
        this.in = new Float32Array(FRAME);
        this.inLength = 0;
        this.done = new Float32Array(FRAME);
        this.out = new Float32Array(FRAME * 2 + 128);
        this.outStart = 0;
        this.outLength = FRAME;
        this.frames = 0;
        this.loudest = -120;
        this.likeliest = -1;
        this.opened = false;
    }

    process(inputs, outputs) {
        const input = inputs[0];
        const output = outputs[0]?.[0];
        if (!output) return true;
        if (input?.length) {
            const channels = input.length;
            for (let i = 0; i < input[0].length; i++) {
                let x = input[0][i];
                for (let c = 1; c < channels; c++) x += input[c][i];
                this.in[this.inLength++] = x / channels;
                if (this.inLength === FRAME) {
                    this.frame();
                    this.inLength = 0;
                }
            }
        }
        for (let i = 0; i < output.length; i++) {
            if (this.outLength > 0) {
                output[i] = this.out[this.outStart];
                this.outStart = (this.outStart + 1) % this.out.length;
                this.outLength--;
            } else {
                output[i] = 0;
            }
        }
        return true;
    }

    // frame cleans the gathered frame, gates it and queues it.
    frame() {
        let speech = -1;
        if (this.x) {
            // A view made before the module's memory grew would read a
            // detached buffer; RNNoise allocates nothing per frame, but it's
            // cheap to be sure.
            const heap = new Float32Array(this.x.memory.buffer);
            const at = this.inPtr / 4;
            for (let i = 0; i < FRAME; i++) heap[at + i] = this.in[i] * SCALE;
            speech = this.x.rnnoise_process_frame(this.state, this.outPtr, this.inPtr);
            const from = this.outPtr / 4;
            for (let i = 0; i < FRAME; i++) this.done[i] = heap[from + i] / SCALE;
        } else {
            this.done.set(this.in);
        }
        const level = levelOf(this.done);
        const open = this.gate.decide(level, speech);
        this.gate.fade(this.done, open);
        let w = (this.outStart + this.outLength) % this.out.length;
        for (let i = 0; i < FRAME; i++) {
            this.out[w] = this.done[i];
            w = (w + 1) % this.out.length;
        }
        this.outLength += FRAME;
        this.loudest = Math.max(this.loudest, level);
        this.likeliest = Math.max(this.likeliest, speech);
        this.opened ||= open;
        if (++this.frames % REPORT === 0) {
            this.port.postMessage({ level: this.loudest, speech: this.likeliest, open: this.opened });
            this.loudest = -120;
            this.likeliest = -1;
            this.opened = false;
        }
    }
}

registerProcessor('voice', VoiceProcessor);
