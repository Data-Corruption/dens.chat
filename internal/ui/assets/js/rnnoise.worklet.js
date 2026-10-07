// RNNoise in an AudioWorklet (M3): between the microphone and the call, for
// a member who turns it on. The audio thread hands the processor 128
// samples at a time and RNNoise takes 480 (10 ms at 48 kHz), so the
// processor gathers frames and plays each back once it's cleaned. Its
// output starts a frame behind, which keeps it from running dry: frames of
// 480 and blocks of 128 line up only every 1,920 samples, and a lead of 448
// is the least that works. With RNNoise's own delay, a call hears the
// member 30 ms later than without it.
//
// processorOptions.module is the compiled module (wasm/rnnoise.wasm), which
// the page compiles under its CSP and posts here. It imports nothing.

const FRAME = 480;
// RNNoise works on samples at 16-bit scale.
const SCALE = 32768;

class RNNoiseProcessor extends AudioWorkletProcessor {
    constructor(options) {
        super();
        const instance = new WebAssembly.Instance(options.processorOptions.module, {});
        const x = instance.exports;
        x._initialize();
        this.x = x;
        this.state = x.rnnoise_create(0);
        this.inPtr = x.malloc(FRAME * 4);
        this.outPtr = x.malloc(FRAME * 4);
        this.in = new Float32Array(FRAME);
        this.inLength = 0;
        this.out = new Float32Array(FRAME * 2 + 128);
        this.outStart = 0;
        this.outLength = FRAME;
    }

    process(inputs, outputs) {
        const input = inputs[0]?.[0];
        const output = outputs[0]?.[0];
        if (!output) return true;
        if (input) {
            for (let i = 0; i < input.length; i++) {
                this.in[this.inLength++] = input[i];
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

    // frame cleans the gathered frame and queues it.
    frame() {
        // A view made before the module's memory grew would read a detached
        // buffer; RNNoise allocates nothing per frame, but it's cheap to be
        // sure.
        const heap = new Float32Array(this.x.memory.buffer);
        const at = this.inPtr / 4;
        for (let i = 0; i < FRAME; i++) heap[at + i] = this.in[i] * SCALE;
        this.x.rnnoise_process_frame(this.state, this.outPtr, this.inPtr);
        const from = this.outPtr / 4;
        let w = (this.outStart + this.outLength) % this.out.length;
        for (let i = 0; i < FRAME; i++) {
            this.out[w] = heap[from + i] / SCALE;
            w = (w + 1) % this.out.length;
        }
        this.outLength += FRAME;
    }
}

registerProcessor('rnnoise', RNNoiseProcessor);
