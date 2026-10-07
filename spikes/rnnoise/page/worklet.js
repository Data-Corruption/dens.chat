// RNNoise in an AudioWorklet: the audio thread hands the processor 128
// samples at a time, RNNoise takes 480 (10 ms at 48 kHz), so the processor
// gathers frames and plays each one back once it's cleaned. The output
// starts a frame behind, which keeps it from running dry between frames.
//
// processorOptions: module (a compiled WebAssembly.Module) or bytes (an
// ArrayBuffer to compile here); bypass, to time the same path without
// RNNoise.

const FRAME = 480;
// RNNoise works on samples at 16-bit scale.
const SCALE = 32768;

class RNNoiseProcessor extends AudioWorkletProcessor {
    constructor(options) {
        super();
        const { module, bytes, bypass } = options.processorOptions || {};
        this.bypass = !!bypass;
        this.frames = 0;
        this.speech = 0;
        // in gathers input until a frame is full; out holds cleaned audio,
        // primed with one frame of silence.
        this.in = new Float32Array(FRAME);
        this.inLength = 0;
        this.out = new Float32Array(FRAME * 2 + 128);
        this.outStart = 0;
        this.outLength = FRAME;
        if (!this.bypass) {
            const compiled = module || new WebAssembly.Module(bytes);
            const instance = new WebAssembly.Instance(compiled, {});
            const x = instance.exports;
            x._initialize();
            this.x = x;
            this.state = x.rnnoise_create(0);
            this.inPtr = x.malloc(FRAME * 4);
            this.outPtr = x.malloc(FRAME * 4);
        }
        this.port.onmessage = (e) => {
            if (e.data === 'stats') this.port.postMessage({ frames: this.frames, speech: this.speech });
        };
    }

    process(inputs, outputs) {
        const input = inputs[0]?.[0];
        const output = outputs[0]?.[0];
        if (!output) return true;
        const n = output.length;
        if (input) {
            for (let i = 0; i < input.length; i++) {
                this.in[this.inLength++] = input[i];
                if (this.inLength === FRAME) {
                    this.frame();
                    this.inLength = 0;
                }
            }
        }
        for (let i = 0; i < n; i++) {
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

    // frame cleans the gathered frame and queues it for output.
    frame() {
        let cleaned = this.in;
        if (!this.bypass) {
            // The module's memory doesn't grow while it runs, but a view made
            // before a growth would read a detached buffer, so it's made here.
            const heap = new Float32Array(this.x.memory.buffer);
            const at = this.inPtr / 4;
            for (let i = 0; i < FRAME; i++) heap[at + i] = this.in[i] * SCALE;
            this.speech = this.x.rnnoise_process_frame(this.state, this.outPtr, this.inPtr);
            cleaned = heap.subarray(this.outPtr / 4, this.outPtr / 4 + FRAME);
        }
        const end = this.out.length;
        let w = (this.outStart + this.outLength) % end;
        for (let i = 0; i < FRAME; i++) {
            this.out[w] = this.bypass ? cleaned[i] : cleaned[i] / SCALE;
            w = (w + 1) % end;
        }
        this.outLength += FRAME;
        this.frames++;
    }
}

registerProcessor('rnnoise', RNNoiseProcessor);
