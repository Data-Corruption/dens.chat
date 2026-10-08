// What an AudioWorklet's scope gives a processor, for running one in
// Node: its base class, and registerProcessor, which keeps what it's given.

globalThis.AudioWorkletProcessor = class {
    constructor() {
        this.port = { postMessage() {}, onmessage: null };
    }
};
globalThis.registered = {};
globalThis.registerProcessor = (name, processor) => {
    globalThis.registered[name] = processor;
};
