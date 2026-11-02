// The den's soundboard (M14.3): sounds staff add, which a member in a call
// plays for everyone in it. The den streams a sound into the call on a
// track of the call's own, which every page plays as it plays a voice, so
// members never fetch the files; this page only prepares the sounds staff
// add, and picks one to play.
//
// The den sends a sound's Opus packets as its file holds them, since it has
// no audio encoder, so the page of the staff member adding one prepares it
// before it goes up: it decodes the file, any audio the browser plays, a
// video's sound too; evens its loudness to one level, its peaks under full
// scale; encodes it as Opus at 64 kbps with WebCodecs; and writes it in
// Ogg, as below. The one level takes the place of a volume for each sound,
// and the one bitrate makes a file's size follow its length.

// RATE is the rate Opus counts samples at, and the page decodes a sound at.
export const RATE = 48000;
// BITRATE is the rate the den takes a sound at, in bits a second.
export const BITRATE = 64000;
// TARGET is the loudness every sound is evened to, in LUFS, about a call's
// voices, and CEILING the highest a peak may reach, in dBFS.
export const TARGET = -20;
export const CEILING = -1;
// SERIAL names the one stream in a sound's Ogg file.
const SERIAL = 0x736e6544;
const VENDOR = 'Dens';

export const NO_ENCODER = "This browser can't prepare sounds for the soundboard. Add them in Chrome, Edge or Firefox 130 or later.";

// opusSamples is how many samples at RATE an Opus packet holds, by its
// table of contents (RFC 6716, 3.1), or 0 for one that isn't a packet.
export function opusSamples(p) {
    if (!p?.length) return 0;
    const config = p[0] >> 3;
    let frame;
    if (config < 12) frame = [480, 960, 1920, 2880][config & 3];
    else if (config < 16) frame = [480, 960][config & 1];
    else frame = [120, 240, 480, 960][config & 3];
    let frames = 1;
    const code = p[0] & 3;
    if (code === 1 || code === 2) frames = 2;
    else if (code === 3) {
        if (p.length < 2) return 0;
        frames = p[1] & 0x3f;
    }
    const n = frames * frame;
    return frames > 0 && n <= 5760 ? n : 0;
}

// Ogg's CRC-32: polynomial 0x04c11db7, shifted left, from 0.
const CRC = (() => {
    const t = new Uint32Array(256);
    for (let i = 0; i < 256; i++) {
        let r = i << 24;
        for (let k = 0; k < 8; k++) r = r & 0x80000000 ? (r << 1) ^ 0x04c11db7 : r << 1;
        t[i] = r >>> 0;
    }
    return t;
})();

function crc(bytes) {
    let c = 0;
    for (const b of bytes) c = ((c << 8) ^ CRC[((c >>> 24) ^ b) & 0xff]) >>> 0;
    return c;
}

// page is one Ogg page holding one packet whole: its flags, the granule
// position its packet ends at, and its number.
function page(packet, flags, granule, serial, number) {
    if (packet.length >= 255 * 255) throw new Error('an Opus packet too large for a page');
    const lacing = [];
    for (let n = packet.length; ; n -= 255) {
        lacing.push(Math.min(n, 255));
        if (n < 255) break;
    }
    const out = new Uint8Array(27 + lacing.length + packet.length);
    const v = new DataView(out.buffer);
    out.set([0x4f, 0x67, 0x67, 0x53], 0); // OggS
    out[5] = flags;
    v.setUint32(6, granule % 0x100000000, true);
    v.setUint32(10, Math.floor(granule / 0x100000000), true);
    v.setUint32(14, serial, true);
    v.setUint32(18, number, true);
    out[26] = lacing.length;
    out.set(lacing, 27);
    out.set(packet, 27 + lacing.length);
    v.setUint32(22, crc(out), true);
    return out;
}

// oggOpus writes a stream of Opus in Ogg (RFC 7845): its head and its tags,
// each on a page of its own, then a page for each packet. Each page's
// granule position is the samples up to its packet's end, and the last's
// ends the stream at samples, the sound's length after the pre-skip, which
// trims the padding the encoder put in its last packet.
export function oggOpus({ channels, preSkip, samples, packets, serial = SERIAL }) {
    const head = new Uint8Array(19);
    const h = new DataView(head.buffer);
    head.set(new TextEncoder().encode('OpusHead'), 0);
    head[8] = 1;
    head[9] = channels;
    h.setUint16(10, preSkip, true);
    h.setUint32(12, RATE, true);
    const vendor = new TextEncoder().encode(VENDOR);
    const tags = new Uint8Array(16 + vendor.length);
    const t = new DataView(tags.buffer);
    tags.set(new TextEncoder().encode('OpusTags'), 0);
    t.setUint32(8, vendor.length, true);
    tags.set(vendor, 12);
    const pages = [page(head, 0x02, 0, serial, 0), page(tags, packets.length ? 0 : 0x04, 0, serial, 1)];
    let granule = 0;
    packets.forEach((p, i) => {
        granule += opusSamples(p);
        const last = i === packets.length - 1;
        pages.push(page(p, last ? 0x04 : 0, last ? Math.min(granule, preSkip + samples) : granule, serial, i + 2));
    });
    const out = new Uint8Array(pages.reduce((n, p) => n + p.length, 0));
    let at = 0;
    for (const p of pages) {
        out.set(p, at);
        at += p.length;
    }
    return out;
}

// Loudness, as ITU-R BS.1770 measures it, at RATE: each channel through
// its K-weighting, a shelf then a high-pass, and the mean of its square
// over blocks of 400 ms, a block every 100 ms, with the blocks under -70
// LUFS left out, and then those 10 LU under the rest.
const SHELF = { b: [1.53512485958697, -2.69169618940638, 1.19839281085285], a: [-1.69065929318241, 0.73248077421585] };
const HIGHPASS = { b: [1, -2, 1], a: [-1.99004745483398, 0.99007225036621] };

function biquad(x, { b, a }) {
    const y = new Float64Array(x.length);
    let x1 = 0;
    let x2 = 0;
    let y1 = 0;
    let y2 = 0;
    for (let i = 0; i < x.length; i++) {
        const v = b[0] * x[i] + b[1] * x1 + b[2] * x2 - a[0] * y1 - a[1] * y2;
        x2 = x1;
        x1 = x[i];
        y2 = y1;
        y1 = v;
        y[i] = v;
    }
    return y;
}

const lufsOf = (meanSquare) => -0.691 + 10 * Math.log10(meanSquare);

// loudness is a sound's integrated loudness in LUFS, from its channels at
// RATE, or -Infinity for silence. A sound shorter than a block is one.
export function loudness(channels) {
    const weighted = channels.map((c) => biquad(biquad(c, SHELF), HIGHPASS));
    const length = channels[0]?.length || 0;
    const block = Math.round(RATE * 0.4);
    const step = Math.round(RATE * 0.1);
    const blocks = [];
    const measure = (from, to) => {
        let sum = 0;
        for (const w of weighted) {
            let s = 0;
            for (let i = from; i < to; i++) s += w[i] * w[i];
            sum += s / (to - from);
        }
        blocks.push(sum);
    };
    if (length === 0) return -Infinity;
    if (length < block) measure(0, length);
    else for (let from = 0; from + block <= length; from += step) measure(from, from + block);
    const loud = blocks.filter((z) => z > 0 && lufsOf(z) > -70);
    if (!loud.length) return -Infinity;
    const gate = lufsOf(loud.reduce((s, z) => s + z, 0) / loud.length) - 10;
    const kept = loud.filter((z) => lufsOf(z) > gate);
    return lufsOf(kept.reduce((s, z) => s + z, 0) / kept.length);
}

// peakOf is the largest sample of a sound's channels, as a fraction of
// full scale.
export function peakOf(channels) {
    let peak = 0;
    for (const c of channels) for (const x of c) peak = Math.max(peak, Math.abs(x));
    return peak;
}

// evenGain is the gain that brings a sound of a loudness and a peak to
// TARGET, held down so its peak stays under CEILING.
export function evenGain(lufs, peak) {
    if (!Number.isFinite(lufs) || !(peak > 0)) return 1;
    const gain = 10 ** ((TARGET - lufs) / 20);
    return Math.min(gain, 10 ** (CEILING / 20) / peak);
}

// even brings a sound's channels to TARGET in place, and returns the gain
// it took, or 0 for silence, which there's nothing to make of.
export function even(channels) {
    const lufs = loudness(channels);
    if (!Number.isFinite(lufs)) return 0;
    const gain = evenGain(lufs, peakOf(channels));
    for (const c of channels) for (let i = 0; i < c.length; i++) c[i] *= gain;
    return gain;
}

// fold mixes a sound's channels into the one or two the call plays. 5.1
// and 7.1 fold as Web Audio mixes 5.1 for speakers: the centre into both
// sides and each surround into its own, at -3 dB, the LFE left out, so a
// film's dialogue stays in the middle. A file names no layout, so any other
// count of more than two puts its extra channels into both sides alike.
export function fold(channels) {
    if (channels.length <= 2) return channels.map((c) => Float32Array.from(c));
    const sides = [Float32Array.from(channels[0]), Float32Array.from(channels[1])];
    const add = (into, from) => {
        for (let i = 0; i < into.length; i++) into[i] += from[i] * Math.SQRT1_2;
    };
    const surround = channels.length === 6 || channels.length === 8;
    for (let c = 2; c < channels.length; c++) {
        if (surround && c === 3) continue;
        if (surround && c > 3) add(sides[c % 2], channels[c]);
        else for (const side of sides) add(side, channels[c]);
    }
    return sides;
}

// seconds says a sound's length as the soundboard does: to a tenth of a
// second under a minute, and as minutes and seconds past it.
export function seconds(ms) {
    if (ms < 60000) return `${(Math.ceil(ms / 100) / 10).toFixed(1).replace(/\.0$/, '')} s`;
    const s = Math.round(ms / 1000);
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

// tooLong says why a sound of ms is longer than a den whose longest is
// longest seconds takes, or '' when it isn't.
export function tooLong(ms, longest) {
    if (ms <= longest * 1000) return '';
    return `This sound lasts ${seconds(ms)}, and this den's sounds are at most ${seconds(longest * 1000)}.`;
}

// preSkipOf reads the pre-skip from an Opus head an encoder gives as its
// decoder's description, or says 312, libopus's at RATE.
export function preSkipOf(description) {
    const b = description ? new Uint8Array(ArrayBuffer.isView(description) ? description.buffer.slice(description.byteOffset, description.byteOffset + description.byteLength) : description) : null;
    if (b?.length >= 19 && new TextDecoder().decode(b.subarray(0, 8)) === 'OpusHead') return b[10] | (b[11] << 8);
    return 312;
}

// encoderConfig is the Opus encoder's configuration for channels: at a
// constant bitrate where the browser has one, which makes the size follow
// the length exactly, and otherwise its own.
async function encoderConfig(channels) {
    const base = { codec: 'opus', sampleRate: RATE, numberOfChannels: channels, bitrate: BITRATE };
    for (const config of [{ ...base, bitrateMode: 'constant' }, base]) {
        try {
            if ((await AudioEncoder.isConfigSupported(config)).supported) return config;
        } catch {
            // A member this browser doesn't take; the next is plainer.
        }
    }
    return null;
}

// prepareSound prepares a file as a sound for a den whose longest is
// longest seconds: decoded at RATE, in mono or stereo, evened, encoded and
// written in Ogg. It resolves to the file, and its length in milliseconds,
// or throws what's wrong, for the member.
export async function prepareSound(file, longest) {
    if (typeof AudioEncoder !== 'function' || typeof OfflineAudioContext !== 'function') throw new Error(NO_ENCODER);
    let decoded;
    try {
        // A context's decoding resamples to its rate.
        decoded = await new OfflineAudioContext(1, 1, RATE).decodeAudioData(await file.arrayBuffer());
    } catch {
        throw new Error("This browser can't play this file's sound, so it can't be a sound on the soundboard.");
    }
    const ms = (decoded.length / RATE) * 1000;
    const long = tooLong(ms, longest);
    if (long) throw new Error(long);
    const channels = fold(Array.from({ length: decoded.numberOfChannels }, (_, c) => decoded.getChannelData(c)));
    const count = channels.length;
    if (!even(channels)) throw new Error('This file is silent.');
    const config = await encoderConfig(count);
    if (!config) throw new Error(NO_ENCODER);
    const packets = [];
    let preSkip = 312;
    let failed = null;
    const encoder = new AudioEncoder({
        output: (chunk, meta) => {
            const p = new Uint8Array(chunk.byteLength);
            chunk.copyTo(p);
            packets.push(p);
            if (meta?.decoderConfig?.description) preSkip = preSkipOf(meta.decoderConfig.description);
        },
        error: (e) => {
            failed = e;
        },
    });
    encoder.configure(config);
    const frames = RATE / 10;
    for (let at = 0; at < decoded.length; at += frames) {
        const n = Math.min(frames, decoded.length - at);
        const planar = new Float32Array(n * count);
        for (let c = 0; c < count; c++) planar.set(channels[c].subarray(at, at + n), c * n);
        const data = new AudioData({ format: 'f32-planar', sampleRate: RATE, numberOfFrames: n, numberOfChannels: count, timestamp: Math.round((at / RATE) * 1e6), data: planar });
        encoder.encode(data);
        data.close();
    }
    await encoder.flush();
    encoder.close();
    if (failed || !packets.length) throw new Error("This browser couldn't encode the sound.");
    const name = file.name.replace(/\.[^.]*$/, '') || 'sound';
    const ogg = oggOpus({ channels: count, preSkip, samples: decoded.length, packets });
    return { file: new File([ogg], `${name}.ogg`, { type: 'audio/ogg' }), ms };
}

// nameOf suggests a sound's name from its file's: without its extension,
// its underscores as spaces, at most 32 characters.
export function nameOf(filename) {
    const name = (filename || '').replace(/\.[^.]*$/, '').replace(/[_]+/g, ' ').replace(/\s+/g, ' ').trim();
    return [...name].slice(0, 32).join('').trim();
}

// matching lists a den's sounds whose names hold what the member searched
// for, in any case, by name.
export function matching(sounds, query) {
    const q = query.trim().toLocaleLowerCase();
    return [...(sounds || [])]
        .filter((s) => !q || s.name.toLocaleLowerCase().includes(q))
        .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id, undefined, { numeric: true }));
}

// boardWhy says why a member can't play a sound now, or '' when they can:
// they're deafened, since there's no playing what they wouldn't hear, as
// there's no talking without hearing; a sound plays in the call; or their
// wait after their last hasn't passed, as of now. The den checks again all
// the same, but for deafening, which is the page's.
export function boardWhy(board, now) {
    if (board.deafened) return 'Undeafen to play sounds.';
    if (board.sounding) return 'A sound is playing.';
    if (board.waitUntil > now) return `Wait ${Math.ceil((board.waitUntil - now) / 1000)} s before your next sound.`;
    return '';
}

// soundRefusal says why the den refused a sound, for the member.
export function soundRefusal(reason, limits) {
    switch (reason) {
        case 'playing':
            return 'A sound is playing already.';
        case 'wait':
            return limits?.wait ? `Wait ${limits.wait} s after your last sound.` : 'Wait a moment after your last sound.';
        case 'off':
            return 'The soundboard is off in this den.';
        case 'staff_muted':
            return "Staff muted you, so you can't play sounds.";
        case 'not_found':
            return "That sound isn't on the soundboard any more.";
        case 'rate_limited':
            return "You're doing that too fast. Wait a moment and try again.";
    }
    return "The den couldn't play that sound.";
}
