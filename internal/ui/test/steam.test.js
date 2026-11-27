// Steam's cards: which links get one, and how a card says what Steam
// told the member's Dens. Checked without a browser.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { MAX_GAMES, gamesIn } from '../src/chat/markdown.jsx';
import { onSteamCards, setSteamCards, setYouTubePlayers, steamApp, steamCards } from '../src/chat/links.js';
import { claimPlay, isPlaying, onPlayChange, releasePlay } from '../src/chat/playback.js';
import { Games, count, deckLook, reviewLook, systems, when } from '../src/chat/steam.jsx';
import { AAC_TYPE, AV1_TYPE, H264_TYPE, pickVideoTrack, preferredEncodings, startTrailer } from '../src/chat/trailer.js';
import { Videos } from '../src/chat/youtube.jsx';

test("a link to a game's store page, its age check or its community hub names its app, and nothing else does", () => {
    const yes = {
        'https://store.steampowered.com/app/1771300/Kingdom_Come_Deliverance_II/': '1771300',
        'https://store.steampowered.com/app/730': '730',
        'https://store.steampowered.com/app/730/?l=german': '730',
        'https://store.steampowered.com/agecheck/app/1091500/': '1091500',
        'https://steamcommunity.com/app/730/discussions/': '730',
        'https://store.steampowered.com/app/4294967295': '4294967295',
    };
    for (const [url, app] of Object.entries(yes)) assert.equal(steamApp(url), app, url);
    for (const url of [
        'http://store.steampowered.com/app/730/',
        'https://store.steampowered.com/app/0730/',
        'https://store.steampowered.com/app/4294967296/',
        'https://store.steampowered.com/sub/54029/',
        'https://store.steampowered.com/bundle/232/',
        'https://store.steampowered.com.evil.example/app/730/',
        'https://evil.example/store.steampowered.com/app/730/',
        'https://store.steampowered.com/app/730x/',
        'steam://store/730',
    ]) assert.equal(steamApp(url), null, url);
});

test('a message gets a card for each game it links, once each and three at most, but none in code or a spoiler', () => {
    const link = (id) => `https://store.steampowered.com/app/${id}/`;
    assert.deepEqual(gamesIn(`try ${link(730)} and ${link(730)}`), ['730']);
    assert.deepEqual(gamesIn(`\`${link(730)}\` ||${link(570)}||`), []);
    assert.deepEqual(gamesIn([1, 2, 3, 4].map(link).join(' ')), ['1', '2', '3']);
    assert.equal(MAX_GAMES, 3);
});

test("Steam's cards are off until the member turns them on, and say so to whoever listens", () => {
    assert.equal(steamCards(), false);
    const heard = [];
    const stop = onSteamCards((on) => heard.push(on));
    setSteamCards(true);
    assert.equal(steamCards(), true);
    stop();
    setSteamCards(false);
    assert.deepEqual(heard, [true]);
});

test("a card names reviews as Steam does, marks them green, amber or red, and shortens a large count", () => {
    assert.deepEqual(reviewLook({ score: 9, percent: 97, count: 288031 }), { name: 'Overwhelmingly Positive', tone: 'text-success' });
    assert.deepEqual(reviewLook({ score: 5 }), { name: 'Mixed', tone: 'text-warning' });
    assert.deepEqual(reviewLook({ score: 2 }), { name: 'Very Negative', tone: 'text-error' });
    for (const bad of [undefined, {}, { score: 0 }, { score: 10 }, { score: '9' }]) assert.equal(reviewLook(bad), null);
    assert.equal(count(7694, 'en-US'), '7,694');
    assert.equal(count(148458, 'en-US'), '148.5K');
});

test('a card lists the systems a game runs on, and Steam Deck by its rating', () => {
    assert.deepEqual(systems({ platforms: { windows: true, linux: true } }), ['Windows', 'Linux']);
    assert.deepEqual(systems({}), []);
    assert.deepEqual(deckLook('verified'), { name: 'Verified', tone: 'text-success' });
    assert.equal(deckLook('playable').name, 'Playable');
    assert.equal(deckLook('nonsense'), null);
});

test("a card's date is a sale's end while it runs, or when the game came out, or that it's coming", () => {
    const now = Date.UTC(2026, 10, 16, 12);
    const at = (y, m, d) => Date.UTC(y, m, d, 12) / 1000;
    assert.equal(when({ price: { discount: 67, ends: at(2026, 10, 23) }, released: at(2025, 1, 4) }, now, 'en-US'), 'Sale ends Nov 23');
    assert.equal(when({ price: { discount: 67, ends: at(2026, 10, 1) }, released: at(2025, 1, 4) }, now, 'en-US'), 'Released Feb 4, 2025', 'a sale that ended');
    assert.equal(when({ released: at(2026, 3, 1) }, now, 'en-US'), 'Released Apr 1');
    assert.equal(when({ coming_soon: true, release_note: 'Q1 2027' }, now, 'en-US'), 'Q1 2027');
    assert.equal(when({ coming_soon: true }, now, 'en-US'), 'Coming soon');
    assert.equal(when({}, now, 'en-US'), '');
});

test('trailer codec preference prefers H.264+AAC and falls back to AV1 when rejected', () => {
    assert.deepEqual(preferredEncodings((m) => m === H264_TYPE || m === AAC_TYPE), ['h264', 'av1']);
    assert.deepEqual(preferredEncodings((m) => m === AV1_TYPE || m === AAC_TYPE), ['av1', 'h264']);
    assert.deepEqual(preferredEncodings((m) => m === H264_TYPE), ['h264', 'av1']);
});

test('rendition selection picks 480p on Small, best <= 720p on Medium+, and smallest if all exceed the cap', () => {
    const tracks = [
        { id: '0', width: 640, height: 360, mime: 'video/mp4', codecs: 'avc1.640029' },
        { id: '1', width: 854, height: 480, mime: 'video/mp4', codecs: 'avc1.640029' },
        { id: '2', width: 1280, height: 720, mime: 'video/mp4', codecs: 'avc1.640029' },
        { id: '3', width: 1920, height: 1080, mime: 'video/mp4', codecs: 'avc1.640029' },
    ];
    const ok = () => true;
    assert.equal(pickVideoTrack(tracks, 'small', ok).id, '1');
    assert.equal(pickVideoTrack(tracks, 'medium', ok).id, '2');
    assert.equal(pickVideoTrack(tracks, 'large', ok).id, '2');
    assert.equal(pickVideoTrack(tracks, 'largest', ok).id, '2');

    const older = [
        { id: '0', width: 700, height: 394, mime: 'video/mp4', codecs: 'avc1.640029' },
        { id: '1', width: 938, height: 528, mime: 'video/mp4', codecs: 'avc1.640029' },
    ];
    assert.equal(pickVideoTrack(older, 'small', ok).id, '0');
    assert.equal(pickVideoTrack(older, 'medium', ok).id, '1');

    const hiOnly = [
        { id: '2', width: 1280, height: 720, mime: 'video/mp4', codecs: 'avc1.640029' },
        { id: '3', width: 1920, height: 1080, mime: 'video/mp4', codecs: 'avc1.640029' },
    ];
    assert.equal(pickVideoTrack(hiOnly, 'small', ok).id, '2');
    assert.equal(pickVideoTrack(tracks, 'small', () => false), null);
});

// Fake EventTarget-like emitter for MSE unit tests.
class Emitter {
    constructor() {
        this._ls = new Map();
    }
    addEventListener(type, fn) {
        if (!this._ls.has(type)) this._ls.set(type, new Set());
        this._ls.get(type).add(fn);
    }
    removeEventListener(type, fn) {
        this._ls.get(type)?.delete(fn);
    }
    emit(type, ev = {}) {
        for (const fn of [...(this._ls.get(type) || [])]) fn(ev);
    }
}

class FakeSourceBuffer extends Emitter {
    constructor(mime) {
        super();
        this.mime = mime;
        this.appends = [];
        this.updating = false;
        this.aborted = false;
    }
    appendBuffer(buf) {
        this.updating = true;
        this.appends.push(new Uint8Array(buf));
        queueMicrotask(() => {
            this.updating = false;
            this.emit('updateend');
        });
    }
    abort() {
        this.aborted = true;
    }
}

class FakeMediaSource extends Emitter {
    static isTypeSupported(mime) {
        return mime.startsWith('video/mp4') || mime.startsWith('audio/mp4');
    }
    constructor() {
        super();
        this.readyState = 'closed';
        this.buffers = [];
        this.ended = false;
        this.duration = NaN;
    }
    addSourceBuffer(mime) {
        const sb = new FakeSourceBuffer(mime);
        this.buffers.push(sb);
        return sb;
    }
    endOfStream() {
        this.ended = true;
        this.readyState = 'ended';
    }
}

test('dual-SourceBuffer MSE controller falls back to AV1, streams lookahead progressively, seeks, ends stream, and revokes object URL on stop', async () => {
    const requested = [];
    const revoked = [];
    let msInstance = null;
    class FakeVideo extends Emitter {
        constructor() {
            super();
            this.currentTime = 0;
            this.paused = false;
            this.played = 0;
            this.loaded = 0;
            this._src = '';
        }
        set src(v) {
            this._src = v;
            queueMicrotask(() => {
                msInstance.readyState = 'open';
                msInstance.emit('sourceopen');
            });
        }
        get src() {
            return this._src;
        }
        play() {
            this.played++;
            return Promise.resolve();
        }
        pause() {
            this.paused = true;
        }
        removeAttribute(name) {
            if (name === 'src') this._src = '';
        }
        load() {
            this.loaded++;
        }
    }
    const video = new FakeVideo();

    class TestMS extends FakeMediaSource {
        constructor() {
            super();
            msInstance = this;
        }
    }

    const URLObj = {
        createObjectURL: () => 'blob:http://localhost/trailer-1',
        revokeObjectURL: (u) => revoked.push(u),
    };

    const manifestAV1 = {
        encoding: 'av1',
        duration: 30,
        video: [
            { id: '0', width: 854, height: 480, mime: 'video/mp4', codecs: 'av01.0.05M.08', segments: 10, segment_duration: 3 },
            { id: '2', width: 1280, height: 720, mime: 'video/mp4', codecs: 'av01.0.05M.08', segments: 10, segment_duration: 3 },
        ],
        audio: { id: '4', mime: 'audio/mp4', codecs: 'mp4a.40.2', segments: 10, segment_duration: 3 },
    };

    const fetchImpl = async (path, opts) => {
        requested.push({ path, aborted: opts.signal.aborted });
        if (path.endsWith('/trailer/h264')) {
            return { ok: false, status: 404, headers: new Headers() };
        }
        if (path.endsWith('/trailer/av1')) {
            return { ok: true, status: 200, headers: new Headers(), json: async () => manifestAV1 };
        }
        return { ok: true, status: 200, headers: new Headers(), arrayBuffer: async () => new Uint8Array([1, 2, 3, 4]).buffer };
    };

    let ready = 0;
    const errors = [];
    const stop = startTrailer({
        app: '1771300',
        size: 'small',
        video,
        onReady: () => ready++,
        onError: (e) => errors.push(e),
        fetchImpl,
        MediaSourceCtor: TestMS,
        URLObj,
    });

    await new Promise((r) => setTimeout(r, 80));
    assert.deepEqual(errors, []);
    assert.equal(ready, 1);
    assert.equal(video.played, 1);
    assert.equal(msInstance.buffers.length, 2, 'opened video and audio SourceBuffers');
    assert.equal(msInstance.buffers[0].mime, 'video/mp4; codecs="av01.0.05M.08"');
    assert.equal(msInstance.buffers[1].mime, 'audio/mp4; codecs="mp4a.40.2"');
    // At t=0 with 12 s lookahead, only chunks 1..4 of 10 are fetched (plus init), using 480p rep 0.
    const paths = requested.map((r) => r.path);
    assert.ok(paths.includes('/api/steam/1771300/trailer/h264'), 'tried h264 first');
    assert.ok(paths.includes('/api/steam/1771300/trailer/av1'), 'fell back to av1 on 404');
    assert.ok(paths.includes('/api/steam/1771300/trailer/av1/0/init'), 'fetched 480p video init on small');
    assert.ok(paths.includes('/api/steam/1771300/trailer/av1/4/init'), 'fetched audio init');
    assert.ok(paths.includes('/api/steam/1771300/trailer/av1/0/4'), 'fetched lookahead chunk 4');
    assert.ok(!paths.includes('/api/steam/1771300/trailer/av1/0/5'), 'did not prefetch chunk 5 at t=0');
    assert.equal(msInstance.ended, false, 'stream stays open until final chunk');

    // Seeking to near the end fetches chunks 9 and 10 and calls endOfStream().
    video.currentTime = 24;
    video.emit('seeking');
    await new Promise((r) => setTimeout(r, 80));
    const afterSeek = requested.map((r) => r.path);
    assert.ok(afterSeek.includes('/api/steam/1771300/trailer/av1/0/9'));
    assert.ok(afterSeek.includes('/api/steam/1771300/trailer/av1/0/10'));
    assert.ok(afterSeek.includes('/api/steam/1771300/trailer/av1/4/10'));
    assert.equal(msInstance.ended, true, 'endOfStream called after final chunk');

    stop();
    assert.deepEqual(revoked, ['blob:http://localhost/trailer-1'], 'object URL revoked on stop');
    assert.equal(video.src, '');
    assert.equal(video.paused, true);
    assert.equal(video.loaded, 1);
});

test('one-at-a-time playback lock coordinates YouTube players and Steam trailers', async () => {
    const a = {};
    const b = {};
    const changes = [];
    const unsub = onPlayChange((cur) => changes.push(cur));
    try {
        claimPlay(a);
        assert.equal(isPlaying(a), true);
        assert.equal(isPlaying(b), false);
        claimPlay(b);
        assert.equal(isPlaying(a), false);
        assert.equal(isPlaying(b), true);
        releasePlay(a);
        assert.equal(isPlaying(b), true, 'releasing another token leaves active player');
        releasePlay(b);
        assert.equal(isPlaying(b), false);
        assert.deepEqual(changes, [a, b, null]);
    } finally {
        unsub();
    }

    // Component-level check: starting a YouTube player stops a Steam trailer and vice versa.
    const origWindow = globalThis.window;
    const origIO = globalThis.IntersectionObserver;
    const origFetch = globalThis.fetch;
    const origMS = globalThis.MediaSource;
    globalThis.MediaSource = FakeMediaSource;
    globalThis.window = {
        matchMedia: () => ({ matches: true, addEventListener() {}, removeEventListener() {} }),
        addEventListener() {},
        removeEventListener() {},
        location: { reload() {} },
    };
    globalThis.IntersectionObserver = class {
        constructor(cb) {
            this.cb = cb;
        }
        observe() {
            queueMicrotask(() => this.cb([{ isIntersecting: true }]));
        }
        disconnect() {}
    };
    globalThis.fetch = async (path) => {
        if (path === '/api/youtube/dQw4w9WgXcQ') {
            return { ok: true, status: 200, headers: new Headers(), text: async () => JSON.stringify({ title: 'Song', channel: 'Ch', playable: true }) };
        }
        if (path === '/api/steam/1771300') {
            return { ok: true, status: 200, headers: new Headers(), text: async () => JSON.stringify({ name: 'KCD2', trailer: true, shots: 2, platforms: { windows: true } }) };
        }
        if (path === '/api/steam/1091500') {
            return { ok: true, status: 200, headers: new Headers(), text: async () => JSON.stringify({ name: 'Adult Game', adult: true, trailer: true, platforms: { windows: true } }) };
        }
        // Hold trailer manifest pending so Steam stays in playing state until interrupted.
        return new Promise(() => {});
    };

    const root = document.createElement('div');
    const settle = () => new Promise((r) => setTimeout(r, 130));
    setSteamCards(true);
    setYouTubePlayers(true);
    try {
        mount(h('div', null,
            h(Videos, { text: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ' }),
            h(Games, { text: 'https://store.steampowered.com/app/1771300/ https://store.steampowered.com/app/1091500/' }),
        ), root);
        await settle();

        // Adult card hides its trailer play button behind the cover until clicked.
        const adultCard = root.find((n) => n.attributes['data-steam'] === '1091500');
        assert.ok(adultCard.find((n) => 'data-steam-cover' in n.attributes), 'adult cover shown');
        assert.equal(adultCard.find((n) => 'data-steam-play' in n.attributes), null, 'play button hidden behind adult cover');

        // Play button only appears on slide 0, not on screenshots.
        const steamCard = root.find((n) => n.attributes['data-steam'] === '1771300');
        assert.ok(steamCard.find((n) => 'data-steam-play' in n.attributes), 'play button shown on header slide');
        steamCard.find((n) => n.attributes['aria-label'] === 'Next picture').dispatch('click');
        await settle();
        assert.equal(steamCard.find((n) => 'data-steam-play' in n.attributes), null, 'play button hidden on screenshots');
        steamCard.find((n) => n.attributes['aria-label'] === 'Previous picture').dispatch('click');
        await settle();

        // Clicking Steam play mounts <video> with Dens custom video controls;
        // stepping to a screenshot keeps <video> mounted and shows the return pill.
        steamCard.find((n) => 'data-steam-play' in n.attributes).dispatch('click');
        await settle();
        assert.ok(steamCard.find((n) => 'data-steam-video' in n.attributes), 'Steam trailer started');
        assert.ok(steamCard.find((n) => 'data-video-controls' in n.attributes), 'Steam trailer uses custom video controls');
        steamCard.find((n) => n.attributes['aria-label'] === 'Next picture').dispatch('click');
        await settle();
        assert.ok(steamCard.find((n) => 'data-steam-video' in n.attributes), 'Steam trailer stays mounted while browsing screenshots');
        assert.ok(steamCard.find((n) => 'data-steam-playing-pill' in n.attributes), 'playing pill shown on screenshots');
        assert.ok(steamCard.find((n) => n.localName === 'img'), 'screenshot shown while trailer plays');
        steamCard.find((n) => 'data-steam-playing-pill' in n.attributes).find((n) => n.localName === 'button').dispatch('click');
        await settle();
        assert.equal(steamCard.find((n) => 'data-steam-playing-pill' in n.attributes), null, 'pill jumps back to trailer slide');

        const ytBox = root.find((n) => n.attributes['data-video'] === 'dQw4w9WgXcQ');
        ytBox.find((n) => n.localName === 'button').dispatch('click');
        await settle();
        assert.ok(ytBox.find((n) => n.localName === 'iframe'), 'YouTube iframe started');
        assert.equal(steamCard.find((n) => 'data-steam-video' in n.attributes), null, 'Steam trailer stopped by YouTube');

        // Starting Steam trailer again stops YouTube back to its cover.
        steamCard.find((n) => 'data-steam-play' in n.attributes).dispatch('click');
        await settle();
        assert.equal(ytBox.find((n) => n.localName === 'iframe'), null, 'YouTube stopped by Steam trailer');
        assert.ok(steamCard.find((n) => 'data-steam-video' in n.attributes), 'Steam trailer playing again');
    } finally {
        mount(null, root);
        setSteamCards(false);
        setYouTubePlayers(false);
        globalThis.window = origWindow;
        globalThis.IntersectionObserver = origIO;
        globalThis.fetch = origFetch;
        globalThis.MediaSource = origMS;
    }
});

test('startTrailer retries muted when unmuted autoplay is blocked', async () => {
    const manifest = {
        encoding: 'h264',
        duration: 3,
        video: [{ id: '0', width: 640, height: 480, mime: 'video/mp4', codecs: 'avc1.640029', segments: 1, segment_duration: 3 }],
    };
    const fetchImpl = async (path) => {
        if (path.endsWith('/trailer/h264')) {
            return { ok: true, status: 200, headers: new Headers(), json: async () => manifest };
        }
        return { ok: true, status: 200, headers: new Headers(), arrayBuffer: async () => new Uint8Array([0, 0, 0, 12]).buffer };
    };
    class FakeSB extends EventTarget {
        constructor() {
            super();
            this.updating = false;
        }
        appendBuffer() {
            queueMicrotask(() => this.dispatchEvent(new Event('updateend')));
        }
        abort() {}
    }
    class FakeMS extends EventTarget {
        constructor() {
            super();
            this.readyState = 'closed';
        }
        addSourceBuffer() {
            return new FakeSB();
        }
        endOfStream() {
            this.readyState = 'ended';
        }
    }
    FakeMS.isTypeSupported = () => true;
    let calls = 0;
    const video = new EventTarget();
    video.currentTime = 0;
    video.muted = false;
    video.pause = () => {};
    video.removeAttribute = () => {};
    video.load = () => {};
    video.play = () => {
        calls++;
        if (calls === 1) return Promise.reject(new DOMException('blocked', 'NotAllowedError'));
        return Promise.resolve();
    };
    Object.defineProperty(video, 'src', {
        set() {
            queueMicrotask(() => {
                msRef.readyState = 'open';
                msRef.dispatchEvent(new Event('sourceopen'));
            });
        },
    });
    let msRef;
    const URLObj = {
        createObjectURL(ms) {
            msRef = ms;
            return 'blob:fake-muted-retry';
        },
        revokeObjectURL() {},
    };
    const stop = startTrailer({ app: '1771300', size: 'small', video, fetchImpl, MediaSourceCtor: FakeMS, URLObj });
    await new Promise((r) => setTimeout(r, 40));
    assert.equal(calls, 2);
    assert.equal(video.muted, true);
    stop();
});
