// A sealed call's frames (M16.2), in a worker of the page's own: SFrame
// (RFC 9605), in its AES-128-GCM suite, which WebRTC's Encoded Transform
// (RTCRtpScriptTransform) hands each frame the page's encoders make before
// it's sent, and each it receives before it's decoded. The den forwards
// what it can't open, so it hears and sees nothing of the call.
//
// Each member's frames are sealed under a key of their own for the call,
// which their Dens makes and seals to the call's other members, and the
// page gets the call's keys whole as one changes. A key goes by its number
// in a frame's header, and each of its member's tracks by a key ID of its
// own from it, four times the number and the track's: 0 the voice, 1 the
// screen, 2 the share's sound. SFrame derives each key ID's AES key and
// salt from the member's key with it, so no two tracks share a counter.
//
// The page sends under its own new key SWITCH_AFTER after it gets it,
// while the call's other members' Dens pass it to their pages, and opens
// a member's last key's frames for KEEP_OLD after their new one comes. A
// frame that doesn't open, names a counter its key has had, names a key
// the page lacks, or comes in the clear on a member's track, is dropped;
// one under a key it lacks has it ask for the call's keys again. The
// soundboard is the den's own track, which plays in the clear.
//
// The worker counts what it does on each of the tracks (M18): the frames
// the page's sender hands it, empty ones too, which tell the page its
// sender's transform runs, those it seals, those it opens, those that
// reach a receiver empty, and those it drops, by why; and frames on a
// receiver the page hasn't named yet.
//
// The page names each receiver's track to the worker by an ID of its own,
// as its stream changes ({t: 'target'}), posts the call's keys ({t:
// 'keys'}), asks for the frames counted ({t: 'counts'}), and ends the call
// ({t: 'end'}), which forgets its keys. The worker asks for the keys again
// ({t: 'lack'}) and answers with the counts.

// SUITE is AES_128_GCM_SHA256_128, RFC 9605's 0x0004: a key of 16 bytes,
// a nonce of 12, and a tag of 16.
export const SUITE = 0x0004;
const NK = 16;
const NN = 12;

// The tracks of a member's, as key IDs name them, and as the counts do.
export const VOICE = 0;
export const SCREEN = 1;
export const SOUND = 2;
export const TRACKS = ['voice', 'screen', 'sound'];

// Why the worker drops a frame it receives, as its counts name it: it
// comes in the clear, as a frame that doesn't start with a header it can
// read; under a key the page lacks; under another track's key ID than the
// page named its receiver's; at a counter its key had; or it doesn't open.
export const DROPS = ['clear', 'lacking', 'track', 'replay', 'unopened'];

// freshCounts are a call's counts before any frame: for each track, the
// frames the page's sender handed the worker, those it sealed and opened,
// those that reached a receiver empty, and those it dropped, by why; and
// those on a receiver the page hadn't named.
export function freshCounts() {
    const counts = { unnamed: 0 };
    for (const name of TRACKS) counts[name] = { seen: 0, sealed: 0, opened: 0, empty: 0, dropped: Object.fromEntries(DROPS.map((d) => [d, 0])) };
    return counts;
}

export const SWITCH_AFTER = 1000;
export const KEEP_OLD = 10 * 1000;
// A counter this far below the newest its key opened counts as one it had.
export const WINDOW = 1024;
// The worker asks for the keys again at most this often for a member.
export const LACK_EVERY = 1000;
// The highest key ID a key's number makes: two bytes of number, and the
// track's two bits.
const MAX_KID = 0xffff * 4 + 3;

const encoder = new TextEncoder();

function concat(...parts) {
    const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
    let at = 0;
    for (const p of parts) {
        out.set(p, at);
        at += p.length;
    }
    return out;
}

function same(a, b) {
    if (a.length !== b.length) return false;
    let diff = 0;
    for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
    return diff === 0;
}

// bytesOf writes a value in the fewest big-endian bytes.
function bytesOf(v) {
    const out = [];
    for (; v > 0n; v >>= 8n) out.unshift(Number(v & 0xffn));
    return out;
}

// encodeHeader writes a frame's header for its key ID and counter, each a
// number or a BigInt: a value under 8 in the config byte, and any other in
// the fewest bytes after it, the key ID's first.
export function encodeHeader(kid, ctr) {
    const k = BigInt(kid);
    const c = BigInt(ctr);
    const kb = k < 8n ? [] : bytesOf(k);
    const cb = c < 8n ? [] : bytesOf(c);
    const config = (k < 8n ? Number(k) << 4 : 0x80 | ((kb.length - 1) << 4)) | (c < 8n ? Number(c) : 0x08 | (cb.length - 1));
    return Uint8Array.from([config, ...kb, ...cb]);
}

// decodeHeader reads a frame's header: its key ID and counter, as BigInts,
// and how long it is; or null for bytes that don't start with one written
// in the fewest bytes.
export function decodeHeader(b) {
    if (!b.length) return null;
    let at = 1;
    const read = (extended, field) => {
        if (!extended) return BigInt(field);
        const len = field + 1;
        if (at + len > b.length || b[at] === 0) return null;
        let v = 0n;
        for (let i = 0; i < len; i++) v = (v << 8n) | BigInt(b[at + i]);
        at += len;
        return v < 8n ? null : v;
    };
    const kid = read(b[0] & 0x80, (b[0] >> 4) & 0x07);
    if (kid === null) return null;
    const ctr = read(b[0] & 0x08, b[0] & 0x07);
    if (ctr === null) return null;
    return { kid, ctr, length: at };
}

// label is SFrame's for a key ID's key or salt: "SFrame 1.0 Secret key "
// or "... salt ", then the key ID in 8 bytes and the suite in 2.
function label(word, kid) {
    const prefix = encoder.encode(`SFrame 1.0 Secret ${word} `);
    const out = new Uint8Array(prefix.length + 10);
    out.set(prefix);
    const v = new DataView(out.buffer);
    v.setBigUint64(prefix.length, BigInt(kid));
    v.setUint16(prefix.length + 8, SUITE);
    return out;
}

// deriveSecrets derives a key ID's AES key and salt from a base key, as
// RFC 9605 does: HKDF-SHA-256, with an empty salt and SFrame's labels.
export async function deriveSecrets(baseKey, kid) {
    const ikm = await crypto.subtle.importKey('raw', baseKey, 'HKDF', false, ['deriveBits']);
    const bits = async (word, n) =>
        new Uint8Array(await crypto.subtle.deriveBits({ name: 'HKDF', hash: 'SHA-256', salt: new Uint8Array(0), info: label(word, kid) }, ikm, n * 8));
    const [key, salt] = await Promise.all([bits('key', NK), bits('salt', NN)]);
    return { key, salt };
}

// deriveKey derives what seals and opens a key ID's frames.
export async function deriveKey(baseKey, kid) {
    const { key, salt } = await deriveSecrets(baseKey, kid);
    const aes = await crypto.subtle.importKey('raw', key, 'AES-GCM', false, ['encrypt', 'decrypt']);
    key.fill(0);
    return { key: aes, salt };
}

// nonceFor is the salt with the counter, in 12 big-endian bytes, laid over
// it.
function nonceFor(salt, ctr) {
    const c = new Uint8Array(NN);
    new DataView(c.buffer).setBigUint64(4, BigInt(ctr));
    for (let i = 0; i < NN; i++) c[i] ^= salt[i];
    return c;
}

// seal seals a frame under a key ID's key, at a counter, with its header
// and the metadata as the AEAD's data: the header, then the ciphertext.
export async function seal(k, kid, ctr, plaintext, metadata = new Uint8Array(0)) {
    const header = encodeHeader(kid, ctr);
    const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: nonceFor(k.salt, ctr), additionalData: concat(header, metadata), tagLength: 128 }, k.key,
        plaintext);
    return concat(header, new Uint8Array(ct));
}

// open opens a sealed frame whose header was read, or throws.
export async function open(k, frame, h, metadata = new Uint8Array(0)) {
    const header = frame.subarray(0, h.length);
    const pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: nonceFor(k.salt, h.ctr), additionalData: concat(header, metadata), tagLength: 128 }, k.key,
        frame.subarray(h.length));
    return new Uint8Array(pt);
}

// keyEntry is a member's key as the worker holds it: its number and bytes,
// each track's derived key once used, and for another member's, each
// track's counters seen, and until when it opens frames.
function keyEntry(n, key) {
    return { n, base: Uint8Array.from(key), derived: new Map(), counters: [0, 0, 0], seen: new Map(), until: Infinity };
}

function derived(entry, track) {
    let d = entry.derived.get(track);
    if (!d) {
        d = deriveKey(entry.base, entry.n * 4 + track);
        entry.derived.set(track, d);
    }
    return d;
}

// The counters a key opened on a track: the newest, and which of the
// WINDOW below it.
function seenWindow(entry, track) {
    let w = entry.seen.get(track);
    if (!w) {
        w = { max: -1, bits: new Uint8Array(WINDOW) };
        entry.seen.set(track, w);
    }
    return w;
}

function seenBefore(w, ctr) {
    if (ctr > w.max) return false;
    return w.max - ctr >= WINDOW || w.bits[ctr % WINDOW] === 1;
}

function markSeen(w, ctr) {
    if (ctr > w.max) {
        if (ctr - w.max >= WINDOW) w.bits.fill(0);
        else for (let i = w.max + 1; i < ctr; i++) w.bits[i % WINDOW] = 0;
        w.max = ctr;
    }
    w.bits[ctr % WINDOW] = 1;
}

// Frames holds a call's keys and seals and opens its frames. now is the
// clock its waits go by.
export class Frames {
    constructor(now = () => performance.now()) {
        this.now = now;
        this.onLack = () => {};
        this.reset();
    }

    // reset forgets the call: its keys, its counters and what it counted.
    reset() {
        this.own = null;
        this.next = null;
        this.members = new Map();
        this.lacking = new Map();
        this.counts = freshCounts();
    }

    // setKeys takes the call's keys, whole: the member's own, which frames
    // go under SWITCH_AFTER from now where one went before, and each other
    // member's, whose last opens frames for KEEP_OLD more, as does the key
    // of one the set leaves out.
    setKeys({ own, members = {} }) {
        const now = this.now();
        const is = (entry, k) => entry && entry.n === k.n && same(entry.base, k.key);
        if (own && !is(this.own, own) && !is(this.next?.entry, own)) {
            const entry = keyEntry(own.n, own.key);
            if (this.own) this.next = { entry, at: now + SWITCH_AFTER };
            else this.own = entry;
        }
        for (const [id, k] of Object.entries(members)) {
            const list = this.members.get(id) || [];
            if (list.length && is(list[0], k)) continue;
            for (const old of list) old.until = Math.min(old.until, now + KEEP_OLD);
            list.unshift(keyEntry(k.n, k.key));
            this.members.set(id, list);
        }
        for (const [id, list] of this.members) {
            if (!(id in members)) for (const old of list) old.until = Math.min(old.until, now + KEEP_OLD);
        }
        this.prune(now);
    }

    prune(now) {
        for (const [id, list] of this.members) {
            const kept = list.filter((e) => e.until > now);
            if (kept.length) this.members.set(id, kept);
            else this.members.delete(id);
        }
    }

    // sealFrame seals a frame of one of the member's tracks under their key,
    // or returns null where they have none yet, which sends nothing. A
    // frame with nothing in it, as Opus's DTX hands the transform between
    // its packets of silence, goes on empty, which the browser sends no
    // packet for, as without a transform: sealed, it would cost every 20 ms
    // of silence a packet of SFrame's header and tag.
    async sealFrame(track, data) {
        const counted = this.counts[TRACKS[track]];
        if (!counted) return null;
        counted.seen++;
        if (!data.length) return data;
        if (this.next && this.now() >= this.next.at) {
            this.own = this.next.entry;
            this.next = null;
        }
        const own = this.own;
        if (!own) return null;
        // Taken before anything waits, so no two frames share a counter.
        const ctr = own.counters[track]++;
        const k = await derived(own, track);
        const out = await seal(k, own.n * 4 + track, ctr, data);
        counted.sealed++;
        return out;
    }

    // openFrame opens a frame that reached a receiver, for the member and
    // track the page named it, or returns null to drop it. A receiver of
    // the soundboard, whose member is '', passes its frames as they are. A
    // frame with nothing in it, as a sender's DTX passes on and the browser
    // sends a few of, holds nothing to open, and drops as empty rather than
    // in the clear.
    async openFrame(target, data) {
        if (target?.member === '') return data;
        const counted = this.counts[TRACKS[target?.track]];
        if (!counted) {
            this.counts.unnamed++;
            return null;
        }
        if (!data.length) {
            counted.empty++;
            return null;
        }
        const drop = (why) => {
            counted.dropped[why]++;
            return null;
        };
        const h = decodeHeader(data);
        if (!h || h.kid > BigInt(MAX_KID) || h.ctr > BigInt(Number.MAX_SAFE_INTEGER)) return drop('clear');
        const kid = Number(h.kid);
        const ctr = Number(h.ctr);
        if ((kid & 3) !== target.track) return drop('track');
        const now = this.now();
        this.prune(now);
        const entry = (this.members.get(target.member) || []).find((e) => e.n === kid >> 2);
        if (!entry) {
            this.lack(target.member, now);
            return drop('lacking');
        }
        const w = seenWindow(entry, target.track);
        if (seenBefore(w, ctr)) return drop('replay');
        try {
            const out = await open(await derived(entry, target.track), data, { ...h, ctr });
            markSeen(w, ctr);
            counted.opened++;
            return out;
        } catch {
            return drop('unopened');
        }
    }

    // lack asks for the call's keys again, at most every LACK_EVERY for a
    // member.
    lack(member, now) {
        if (now - (this.lacking.get(member) ?? -Infinity) < LACK_EVERY) return;
        this.lacking.set(member, now);
        this.onLack(member);
    }
}

// transformer runs one sender's or receiver's frames through frames: the
// page names a sender's track in its options, and a receiver by the ID it
// names its target by in targets. A frame that throws is dropped, so the
// stream goes on.
export function transform(frames, targets, transformer) {
    const o = transformer.options || {};
    const work = o.side === 'send' ? (data) => frames.sealFrame(o.track, data) : (data) => frames.openFrame(targets.get(o.id), data);
    transformer.readable
        .pipeThrough(
            new TransformStream({
                async transform(frame, out) {
                    let result = null;
                    try {
                        result = await work(new Uint8Array(frame.data));
                    } catch {
                        result = null;
                    }
                    if (!result) return;
                    if (result.buffer !== frame.data) frame.data = result.byteLength === result.buffer.byteLength ? result.buffer : result.slice().buffer;
                    out.enqueue(frame);
                },
            }),
        )
        .pipeTo(transformer.writable)
        .catch(() => {});
}

// In a worker, the page's messages and transforms reach the call's frames.
if (typeof WorkerGlobalScope !== 'undefined' && globalThis instanceof WorkerGlobalScope) {
    const frames = new Frames();
    const targets = new Map();
    frames.onLack = (member) => postMessage({ t: 'lack', member });
    globalThis.onmessage = (e) => {
        const m = e.data || {};
        if (m.t === 'keys') frames.setKeys(m);
        else if (m.t === 'target') targets.set(m.id, { member: m.member, track: m.track });
        else if (m.t === 'counts') postMessage({ t: 'counts', ...frames.counts });
        else if (m.t === 'end') {
            frames.reset();
            targets.clear();
        }
    };
    globalThis.onrtctransform = (e) => transform(frames, targets, e.transformer);
}
