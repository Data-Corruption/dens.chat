// Copies the page makes (M5.5). In Chrome and Edge, the page makes a
// video's smaller copy itself with WebCodecs, often on the graphics card
// and many times faster than the local service's media module. The service
// offers the copy on the socket the page follows the upload by: what
// decoding the video's packets takes, and how to encode the copy. The page
// takes the packets a window at a time, decodes them, keeps at most the
// frames a second the copy has, crops each as the video's container asks,
// and hands it to the encoder at its own size, which scales it: drawing it
// through a canvas first cost the spike 15 to 20 points of quality. It
// sends the copy's AV1 packets back as it goes. A browser that can't decode
// the video or encode the copy declines, and one that fails says so: the
// service then has its module make the copy.
//
// Packets go both ways as records, little-endian: the packet's size (u32),
// its flags (u32, 1 for a keyframe), and its time and duration in
// microseconds (i64 each), then the packet.

const HEADER = 24;
// How much of the video's packets the page asks for at a time, and how
// much of the copy's it sends at a time, or after how long.
const WINDOW = 4 << 20;
const BATCH = 512 << 10;
const BATCH_WAIT = 1000;
// How much the decoder and encoder may hold before the page waits.
const QUEUE = 8;
// How much may wait to go out on the socket before the page waits.
const SOCKET_BACKLOG = 8 << 20;

// canCopy says whether this browser makes videos' copies itself: Chrome,
// Edge and others built on Chromium, whose WebCodecs decode in hardware
// where it can and encode AV1 within a fifth of its bitrate. Firefox's
// encoders ran slower than the module in the video spike, and two to four
// times over their bitrate, so it leaves copies to the module.
export function canCopy(nav = globalThis.navigator, scope = globalThis) {
    return typeof scope.VideoEncoder === 'function' && typeof scope.VideoDecoder === 'function' &&
        !!nav?.userAgentData?.brands?.some((b) => b.brand === 'Chromium');
}

// readRecords reads a message of whole records.
export function readRecords(buffer) {
    const view = new DataView(buffer);
    const out = [];
    for (let off = 0; off + HEADER <= buffer.byteLength;) {
        const size = view.getUint32(off, true);
        if (off + HEADER + size > buffer.byteLength) throw new Error('a record cut short');
        out.push({
            key: (view.getUint32(off + 4, true) & 1) === 1,
            pts: Number(view.getBigInt64(off + 8, true)),
            duration: Number(view.getBigInt64(off + 16, true)),
            data: new Uint8Array(buffer, off + HEADER, size),
        });
        off += HEADER + size;
    }
    return out;
}

// writeRecord writes a packet of the copy as a record.
export function writeRecord(key, pts, duration, size, copyTo) {
    const out = new Uint8Array(HEADER + size);
    const view = new DataView(out.buffer);
    view.setUint32(0, size, true);
    view.setUint32(4, key ? 1 : 0, true);
    view.setBigInt64(8, BigInt(Math.round(pts)), true);
    view.setBigInt64(16, BigInt(Math.max(0, Math.round(duration))), true);
    copyTo(out.subarray(HEADER));
    return out;
}

// keeper keeps at most fps frames a second, from startUS on, as the module
// does: a frame goes once its time reaches the next slot, a 1/fps second
// after the last one kept, give or take a quarter of a slot.
export function keeper(fps, startUS) {
    const slot = 1e6 / fps;
    let next = null;
    return (ts) => {
        if (ts < startUS || (next !== null && ts < next - slot / 4)) return false;
        next = ts + slot;
        return true;
    };
}

// cropRect is the part of a decoded frame the copy shows, its container's
// crop applied to what the stream itself shows (crop is top, bottom, left
// and right), or null when there's none, or none that leaves anything.
export function cropRect(visible, crop) {
    const [top, bottom, left, right] = crop || [];
    if (!top && !bottom && !left && !right) return null;
    const rect = { x: visible.x + left, y: visible.y + top, width: visible.width - left - right, height: visible.height - top - bottom };
    return rect.width > 0 && rect.height > 0 ? rect : null;
}

// decoderConfig and encoderConfig are WebCodecs' configurations from the
// service's offer.
export function decoderConfig(decoding) {
    const config = { codec: decoding.codec, codedWidth: decoding.coded_width, codedHeight: decoding.coded_height, hardwareAcceleration: 'no-preference' };
    if (decoding.description) config.description = Uint8Array.from(atob(decoding.description), (c) => c.charCodeAt(0));
    return config;
}

export function encoderConfig(encoding) {
    return {
        codec: encoding.codec, width: encoding.width, height: encoding.height, bitrate: encoding.bitrate, framerate: encoding.fps,
        bitrateMode: 'variable', latencyMode: 'quality', hardwareAcceleration: 'no-preference',
    };
}

// supported says whether this browser decodes the video and encodes its
// copy as offered, and if not, why.
export async function supported(offer, scope = globalThis) {
    const check = async (codec, config) => {
        try {
            return (await codec.isConfigSupported(config)).supported;
        } catch {
            return false;
        }
    };
    if (!(await check(scope.VideoDecoder, decoderConfig(offer.decoding)))) return `it can't decode ${offer.decoding.codec}`;
    if (!(await check(scope.VideoEncoder, encoderConfig(offer.encoding)))) return `it can't encode ${offer.encoding.codec}`;
    return '';
}

// makeCopy makes the copy the service offered on socket, a WebSocket whose
// binary messages come to it through the returned packets, and the end of
// them through end; stop ends it early, as a withdrawn offer does.
export function makeCopy(socket, offer) {
    const id = offer.id;
    const send = (msg) => socket.readyState === WebSocket.OPEN && socket.send(JSON.stringify({ ...msg, offer: id }));
    // queued is what arrived and waits to be decoded, and asked what was
    // asked for and hasn't arrived: the page asks for more once both run
    // low, so it holds about a window at most.
    const queue = [];
    let queued = 0;
    let asked = 0;
    let ended = false;
    let stopped = false;
    let wake = null;
    const nudge = () => {
        const w = wake;
        wake = null;
        w?.();
    };
    const wait = (ms) => new Promise((resolve) => {
        wake = resolve;
        setTimeout(nudge, ms);
    });

    let decoder = null;
    let encoder = null;
    let more = () => {};
    let failure = '';
    const fail = (why) => {
        if (stopped) return;
        failure = failure || why;
        stopped = true;
        nudge();
    };
    const close = () => {
        for (const codec of [decoder, encoder]) {
            if (codec && codec.state !== 'closed') codec.close();
        }
    };

    // The copy's records go out in batches.
    let batch = [];
    let batchBytes = 0;
    let lastSent = performance.now();
    let packets = 0;
    const flush = () => {
        if (!batchBytes) return;
        const out = new Uint8Array(batchBytes);
        let at = 0;
        for (const r of batch) {
            out.set(r, at);
            at += r.length;
        }
        socket.send(out);
        batch = [];
        batchBytes = 0;
        lastSent = performance.now();
    };

    async function run() {
        const why = await supported(offer);
        if (why) {
            send({ t: 'decline', reason: `This browser can't make the copy: ${why}.` });
            return;
        }
        send({ t: 'accept' });
        const keep = keeper(offer.encoding.fps, offer.encoding.start_us);
        const slot = 1e6 / offer.encoding.fps;
        let lastKey = null;
        encoder = new VideoEncoder({
            output: (chunk) => {
                const r = writeRecord(chunk.type === 'key', chunk.timestamp, chunk.duration ?? slot, chunk.byteLength, (dst) => chunk.copyTo(dst));
                batch.push(r);
                batchBytes += r.length;
                packets++;
                if (batchBytes >= BATCH || performance.now() - lastSent > BATCH_WAIT) flush();
            },
            error: (e) => fail(`the encoder failed: ${e.message}`),
        });
        encoder.configure(encoderConfig(offer.encoding));
        decoder = new VideoDecoder({
            output: (frame) => {
                try {
                    if (stopped || !keep(frame.timestamp)) return;
                    const rect = cropRect(frame.visibleRect, offer.decoding.crop);
                    const shown = rect ? new VideoFrame(frame, { visibleRect: rect }) : frame;
                    try {
                        const key = lastKey === null || frame.timestamp - lastKey >= offer.encoding.keyframe_us;
                        if (key) lastKey = frame.timestamp;
                        encoder.encode(shown, { keyFrame: key });
                    } finally {
                        if (shown !== frame) shown.close();
                    }
                } catch (e) {
                    fail(`a frame didn't encode: ${e.message}`);
                } finally {
                    frame.close();
                }
            },
            error: (e) => fail(`the decoder failed: ${e.message}`),
        });
        decoder.configure(decoderConfig(offer.decoding));
        decoder.addEventListener('dequeue', nudge);
        encoder.addEventListener('dequeue', nudge);

        const ask = () => {
            if (ended || asked > WINDOW / 2 || queued > WINDOW / 2) return;
            send({ t: 'more', bytes: WINDOW });
            asked += WINDOW;
        };
        more = ask;
        ask();
        while (!stopped) {
            if (decoder.decodeQueueSize > QUEUE || encoder.encodeQueueSize > QUEUE || socket.bufferedAmount > SOCKET_BACKLOG) {
                await wait(50);
                continue;
            }
            const r = queue.shift();
            if (!r) {
                if (ended) break;
                await wait(250);
                if (performance.now() - lastSent > BATCH_WAIT) flush();
                continue;
            }
            queued -= HEADER + r.data.length;
            decoder.decode(new EncodedVideoChunk({ type: r.key ? 'key' : 'delta', timestamp: r.pts, duration: r.duration, data: r.data }));
            ask();
        }
        if (stopped) return;
        await decoder.flush();
        await encoder.flush();
        if (stopped) return;
        flush();
        send({ t: 'copied', packets });
    }

    run().catch((e) => fail(e.message)).finally(() => {
        close();
        if (failure && socket.readyState === WebSocket.OPEN) send({ t: 'fail', reason: failure });
    });

    return {
        packets(buffer) {
            if (stopped) return;
            try {
                for (const r of readRecords(buffer)) {
                    queue.push(r);
                    queued += HEADER + r.data.length;
                }
            } catch (e) {
                fail(e.message);
            }
            // A record larger than what was left of the window comes whole.
            asked = Math.max(0, asked - buffer.byteLength);
            more();
            nudge();
        },
        end() {
            ended = true;
            nudge();
        },
        stop() {
            stopped = true;
            nudge();
        },
    };
}

// followUpload follows an upload over a socket of its own: how far the
// local service has come with it, which onFollow hears, and in Chrome and
// Edge, the copy of a video it offers the page to make (M5.4, M5.5). A
// socket, unlike a request, doesn't wait behind the browser's few
// connections to the service, which uploads hold while copies are made. It
// returns how to stop following.
export function followUpload(denID, key, onFollow) {
    const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
    const url = `${scheme}://${location.host}/api/dens/${denID}/progress/${key}`;
    let socket = null;
    let copy = null;
    let stopped = false;
    let retry = null;
    const open = () => {
        socket = new WebSocket(url);
        socket.binaryType = 'arraybuffer';
        socket.onmessage = (e) => {
            if (typeof e.data !== 'string') {
                copy?.packets(e.data);
                return;
            }
            let msg;
            try {
                msg = JSON.parse(e.data);
            } catch {
                return;
            }
            if (msg.t === 'progress') onFollow?.({ stage: msg.stage, done: msg.done || 0 });
            else if (msg.t === 'offer' && msg.copy) {
                copy?.stop();
                copy = makeCopy(socket, msg.copy);
            } else if (msg.t === 'packets-end') copy?.end();
            else if (msg.t === 'cancel') {
                copy?.stop();
                copy = null;
            }
        };
        // A socket the service closed before the upload reached it, as one
        // waiting behind other requests does, opens again.
        socket.onclose = () => {
            copy?.stop();
            copy = null;
            if (!stopped) retry = setTimeout(open, 1000);
        };
    };
    open();
    return () => {
        stopped = true;
        clearTimeout(retry);
        copy?.stop();
        socket.close();
    };
}
