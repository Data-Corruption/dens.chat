// A sealed call's frames (M16.2), in the worker the page runs them
// through: RFC 9605's own cases, both ways, for the header, the key and
// salt a key ID derives, and a frame under AES_128_GCM_SHA256_128; then
// the worker's rules: a track's key, a counter seen again, a key it lacks,
// a frame in the clear and the soundboard's, and changing keys; and from
// M18 what it counts on each track, and why it drops each frame.

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import {
    DROPS, Frames, KEEP_OLD, LACK_EVERY, SCREEN, SOUND, SWITCH_AFTER, TRACKS, VOICE, WINDOW, decodeHeader, deriveKey, deriveSecrets, encodeHeader,
    freshCounts, open, seal, transform,
} from '../assets/js/sframe.worker.js';

const rfc = JSON.parse(readFileSync('internal/ui/test/testdata/rfc9605.json', 'utf8'));
const hex = (s) => Uint8Array.from(s.match(/../g) || [], (b) => parseInt(b, 16));
const toHex = (b) => [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
const text = (s) => new TextEncoder().encode(s);
const str = (b) => new TextDecoder().decode(b);

test("RFC 9605's headers, written and read", () => {
    assert.equal(rfc.headers.length, 289);
    for (const c of rfc.headers) {
        const kid = BigInt(c.kid);
        const ctr = BigInt(c.ctr);
        assert.equal(toHex(encodeHeader(kid, ctr)), c.header, `kid ${c.kid}, ctr ${c.ctr}`);
        // With the frame after it, which isn't the header's.
        const h = decodeHeader(hex(`${c.header}ff`));
        assert.deepEqual([h.kid, h.ctr, h.length], [kid, ctr, c.header.length / 2], c.header);
    }
    // Numbers write as BigInts do.
    assert.equal(toHex(encodeHeader(0x123, 0x4567)), '9901234567');
    // A header cut short, or not in the fewest bytes, isn't one.
    for (const bad of ['', '90', '8000ff', '0800', '0807', '9900']) assert.equal(decodeHeader(hex(bad)), null, bad);
});

test("RFC 9605's frame under AES_128_GCM_SHA256_128, both ways", async () => {
    const c = rfc.sframe;
    assert.equal(c.cipher_suite, '0x0004');
    const kid = BigInt(c.kid);
    const ctr = BigInt(c.ctr);
    const secrets = await deriveSecrets(hex(c.base_key), kid);
    assert.equal(toHex(secrets.key), c.sframe_key);
    assert.equal(toHex(secrets.salt), c.sframe_salt);
    const k = await deriveKey(hex(c.base_key), kid);
    assert.equal(toHex(await seal(k, kid, ctr, hex(c.pt), hex(c.metadata))), c.ct);
    const frame = hex(c.ct);
    const h = decodeHeader(frame);
    assert.equal(toHex(frame.subarray(0, h.length)) + c.metadata, c.aad);
    assert.equal(toHex(await open(k, frame, h, hex(c.metadata))), c.pt);
    // Without the metadata it was sealed with, or changed, it doesn't open.
    await assert.rejects(open(k, frame, h));
    frame[frame.length - 1] ^= 1;
    await assert.rejects(open(k, frame, h, hex(c.metadata)));
});

const KEY_A = Uint8Array.from({ length: 16 }, (_, i) => i);
const KEY_A2 = Uint8Array.from({ length: 16 }, (_, i) => 50 + i);
const KEY_B = Uint8Array.from({ length: 16 }, (_, i) => 100 + i);

// pair is alice's page and bob's, on a clock of the test's: bob holds
// alice's key, as member 1's.
function pair(clock) {
    const now = () => clock.t;
    const alice = new Frames(now);
    const bob = new Frames(now);
    alice.setKeys({ own: { n: 7, key: KEY_A }, members: {} });
    bob.setKeys({ own: { n: 9, key: KEY_B }, members: { 1: { n: 7, key: KEY_A } } });
    return { alice, bob };
}

const voiceOf = (member) => ({ member, track: VOICE });

// counted is what the worker counted, with what's still 0 left out: by
// track, then by what it did, or why it dropped a frame.
function counted(frames) {
    const out = {};
    for (const [name, value] of Object.entries(frames.counts)) {
        if (typeof value === 'number') {
            if (value) out[name] = value;
            continue;
        }
        const track = {};
        for (const [what, n] of Object.entries(value)) {
            if (typeof n === 'number') {
                if (n) track[what] = n;
            } else {
                const why = Object.fromEntries(Object.entries(n).filter(([, d]) => d));
                if (Object.keys(why).length) track[what] = why;
            }
        }
        if (Object.keys(track).length) out[name] = track;
    }
    return out;
}

test('a frame sealed by one page opens on another, on its track alone, and once', async () => {
    const { alice, bob } = pair({ t: 0 });
    const first = await alice.sealFrame(VOICE, text('hello'));
    const h = decodeHeader(first);
    assert.deepEqual([Number(h.kid), h.ctr], [7 * 4 + VOICE, 0n]);
    assert.equal(str(await bob.openFrame(voiceOf('1'), first)), 'hello');
    assert.equal(await bob.openFrame(voiceOf('1'), first), null, 'a counter its key had');
    const next = await alice.sealFrame(VOICE, text('again'));
    assert.equal(await bob.openFrame({ member: '1', track: SCREEN }, next), null, "on the screen's track");
    assert.equal(await bob.openFrame(voiceOf('2'), next), null, "as another member's");
    assert.equal(str(await bob.openFrame(voiceOf('1'), next)), 'again');
    // Frames out of order open, each once; one older than the window
    // doesn't.
    const frames = [];
    for (let i = 0; i < WINDOW + 3; i++) frames.push(await alice.sealFrame(VOICE, text(`f${i}`)));
    assert.equal(str(await bob.openFrame(voiceOf('1'), frames[5])), 'f5');
    assert.equal(str(await bob.openFrame(voiceOf('1'), frames[4])), 'f4');
    assert.equal(await bob.openFrame(voiceOf('1'), frames[5]), null);
    assert.equal(str(await bob.openFrame(voiceOf('1'), frames.at(-1))), `f${WINDOW + 2}`);
    assert.equal(await bob.openFrame(voiceOf('1'), frames[0]), null, 'a frame older than the window');
    // Each counted on its track, and each dropped one by why: a counter
    // its key had, a key bob lacks for the member he was told, and the
    // voice's frame on the screen's receiver.
    assert.deepEqual(counted(alice), { voice: { seen: WINDOW + 5, sealed: WINDOW + 5 } });
    assert.deepEqual(counted(bob), { voice: { opened: 5, dropped: { lacking: 1, replay: 3 } }, screen: { dropped: { track: 1 } } });
});

test("each of a member's tracks goes under a key of its own", async () => {
    const { alice, bob } = pair({ t: 0 });
    const sealed = [];
    for (const track of [VOICE, SCREEN, SOUND]) sealed.push(await alice.sealFrame(track, text('the same frame')));
    // Each starts its own counter, under a key ID of its own, and the same
    // frame seals to something else on each.
    assert.deepEqual(sealed.map((f) => [Number(decodeHeader(f).kid), decodeHeader(f).ctr]), [[28, 0n], [29, 0n], [30, 0n]]);
    const bodies = new Set(sealed.map((f) => toHex(f.subarray(decodeHeader(f).length))));
    assert.equal(bodies.size, 3);
    for (const [i, track] of [VOICE, SCREEN, SOUND].entries()) {
        assert.equal(str(await bob.openFrame({ member: '1', track }, sealed[i])), 'the same frame');
    }
    assert.deepEqual(counted(alice), { voice: { seen: 1, sealed: 1 }, screen: { seen: 1, sealed: 1 }, sound: { seen: 1, sealed: 1 } });
    assert.deepEqual(counted(bob), { voice: { opened: 1 }, screen: { opened: 1 }, sound: { opened: 1 } });
});

test("a frame in the clear, a key the page lacks, the soundboard's, and a page with no key", async () => {
    const clock = { t: 0 };
    const { bob } = pair(clock);
    const asked = [];
    bob.onLack = (member) => asked.push(member);
    // An Opus frame as a browser sends one in the clear, short and long.
    const clear = Uint8Array.from([0xf8, 0xff, 0xfe, 0x00, 0x01]);
    const long = Uint8Array.from({ length: 120 }, (_, i) => (i === 0 ? 0x48 : i));
    assert.equal(await bob.openFrame(voiceOf('1'), clear), null);
    assert.equal(await bob.openFrame(voiceOf('1'), long), null);
    // A frame under a key bob lacks: dropped, and he asks for the call's
    // keys again, at most once a second for its member.
    const carol = new Frames(() => clock.t);
    carol.setKeys({ own: { n: 3, key: KEY_B } });
    const theirs = await carol.sealFrame(VOICE, text('hi'));
    assert.equal(await bob.openFrame(voiceOf('3'), theirs), null);
    assert.equal(await bob.openFrame(voiceOf('3'), theirs), null);
    assert.deepEqual(asked, ['3']);
    clock.t += LACK_EVERY;
    await bob.openFrame(voiceOf('3'), theirs);
    assert.deepEqual(asked, ['3', '3']);
    // The soundboard's frames pass as they are, whatever they hold; a
    // receiver the page never named drops its frames.
    assert.equal(await bob.openFrame({ member: '', track: VOICE }, clear), clear);
    assert.equal(await bob.openFrame(undefined, theirs), null);
    // A frame in the clear on the share's sound, as Chromium sent one
    // around its seal (M18), counts there.
    assert.equal(await bob.openFrame({ member: '1', track: SOUND }, Uint8Array.from([0xfc, 0xff, 0xfe, 0x01, 0x02])), null);
    assert.deepEqual(counted(bob), { unnamed: 1, voice: { dropped: { clear: 2, lacking: 3 } }, sound: { dropped: { clear: 1 } } });
    // A page without a key of its own yet sends nothing, but its sender's
    // frames reached the worker.
    const keyless = new Frames(() => clock.t);
    assert.equal(await keyless.sealFrame(VOICE, text('x')), null);
    assert.deepEqual(counted(keyless), { voice: { seen: 1 } });
});

test('what the worker counts: every track, and every reason it drops a frame', () => {
    assert.deepEqual(TRACKS, ['voice', 'screen', 'sound']);
    assert.deepEqual([VOICE, SCREEN, SOUND].map((t) => TRACKS[t]), ['voice', 'screen', 'sound']);
    const fresh = freshCounts();
    assert.deepEqual(Object.keys(fresh), ['unnamed', ...TRACKS]);
    for (const name of TRACKS) assert.deepEqual(fresh[name], { seen: 0, sealed: 0, opened: 0, empty: 0, dropped: Object.fromEntries(DROPS.map((d) => [d, 0])) });
    assert.deepEqual(DROPS, ['clear', 'lacking', 'track', 'replay', 'unopened']);
});

test('an empty frame, as DTX leaves between its packets of silence, goes on empty, and nothing seals it', async () => {
    const { alice, bob } = pair({ t: 0 });
    const empty = new Uint8Array(0);
    assert.equal(await alice.sealFrame(VOICE, empty), empty);
    // The worker saw it, which says the sender's transform runs (M18).
    assert.deepEqual(counted(alice), { voice: { seen: 1 } });
    // It took no counter: the next frame is the key's first.
    assert.equal(decodeHeader(await alice.sealFrame(VOICE, text('a'))).ctr, 0n);
    // An empty frame reaching a member's track, as the browser sends a few
    // of, opens to nothing, and drops as empty, not in the clear.
    assert.equal(await bob.openFrame(voiceOf('1'), empty), null);
    assert.deepEqual(counted(bob), { voice: { empty: 1 } });
});

test("changing keys: a page sends under its new key a second after it comes, and opens a member's last for ten seconds more", async () => {
    const clock = { t: 0 };
    const { alice, bob } = pair(clock);
    alice.setKeys({ own: { n: 8, key: KEY_A2 } });
    const old = [await alice.sealFrame(VOICE, text('old 1')), await alice.sealFrame(VOICE, text('old 2'))];
    assert.equal(Number(decodeHeader(old[1]).kid) >> 2, 7, 'the last key for a second more');
    clock.t += SWITCH_AFTER;
    const fresh = await alice.sealFrame(VOICE, text('new'));
    assert.deepEqual([Number(decodeHeader(fresh).kid) >> 2, decodeHeader(fresh).ctr], [8, 0n]);
    // Bob gets alice's new key: frames under either open.
    bob.setKeys({ own: { n: 9, key: KEY_B }, members: { 1: { n: 8, key: KEY_A2 } } });
    assert.equal(str(await bob.openFrame(voiceOf('1'), fresh)), 'new');
    assert.equal(str(await bob.openFrame(voiceOf('1'), old[0])), 'old 1');
    // The same keys again change nothing.
    bob.setKeys({ own: { n: 9, key: KEY_B }, members: { 1: { n: 8, key: KEY_A2 } } });
    clock.t += KEEP_OLD;
    assert.equal(await bob.openFrame(voiceOf('1'), old[1]), null, 'the last key, ten seconds on');
    const later = await alice.sealFrame(VOICE, text('later'));
    assert.equal(str(await bob.openFrame(voiceOf('1'), later)), 'later');
    // A frame changed on the way doesn't open.
    const changed = await alice.sealFrame(VOICE, text('changed'));
    changed[changed.length - 1] ^= 1;
    const before = bob.counts.voice.dropped.unopened;
    assert.equal(await bob.openFrame(voiceOf('1'), changed), null);
    assert.equal(bob.counts.voice.dropped.unopened, before + 1);
    // A member the keys leave out, as one who left, opens ten seconds
    // more.
    bob.setKeys({ own: { n: 9, key: KEY_B }, members: {} });
    assert.equal(str(await bob.openFrame(voiceOf('1'), await alice.sealFrame(VOICE, text('going')))), 'going');
    clock.t += KEEP_OLD;
    assert.equal(await bob.openFrame(voiceOf('1'), await alice.sealFrame(VOICE, text('gone'))), null);
    // Ending the call forgets every key, and what it counted.
    alice.reset();
    assert.equal(await alice.sealFrame(VOICE, text('x')), null);
    assert.deepEqual(counted(alice), { voice: { seen: 1 } });
});

test('a transform passes what it seals or opens on, and drops the rest without stopping', async () => {
    const { alice, bob } = pair({ t: 0 });
    // A transformer as the browser gives the worker one, its frames'
    // data as ArrayBuffers.
    const run = async (frames, targets, options, inputs) => {
        const out = [];
        let feed;
        const readable = new ReadableStream({ start: (c) => (feed = c) });
        const writable = new WritableStream({ write: (frame) => out.push(new Uint8Array(frame.data)) });
        transform(frames, targets, { options, readable, writable });
        for (const data of inputs) feed.enqueue({ data: data.slice().buffer });
        feed.close();
        await new Promise((r) => setTimeout(r, 50));
        return out;
    };
    const sent = await run(alice, new Map(), { side: 'send', track: VOICE }, [text('one'), text('two')]);
    assert.equal(sent.length, 2);
    const targets = new Map([[4, voiceOf('1')], [5, { member: '', track: VOICE }]]);
    const got = await run(bob, targets, { side: 'receive', id: 4 }, [sent[0], text('in the clear'), sent[1]]);
    assert.deepEqual(got.map(str), ['one', 'two']);
    const board = await run(bob, targets, { side: 'receive', id: 5 }, [text('a sound')]);
    assert.deepEqual(board.map(str), ['a sound']);
});
