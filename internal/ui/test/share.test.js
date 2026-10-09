import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    MAX_WATCHING, SHARE_DEFAULTS, displayOptions, fitPixels, mbps, readShare, receivingSections, refusalText, screenOwner, shareBlocked, shareCost,
    shareQuality, sharersIn, sharingAcross, watchersOf,
} from '../assets/js/src/share.js';

const limits = { members: 15, callers: 30, shares: 1, share_viewers: 8, share_bitrate: 2_000_000, share_height: 1080, share_fps: 30 };

test("the browser's kept choices are read, with defaults for anything else", () => {
    assert.deepEqual(readShare(''), SHARE_DEFAULTS);
    assert.deepEqual(readShare('not json'), SHARE_DEFAULTS);
    assert.deepEqual(readShare('[1]'), SHARE_DEFAULTS);
    assert.deepEqual(readShare(JSON.stringify({ height: 1440, fps: 60, hint: 'motion', sound: false })),
        { height: 1440, fps: 60, hint: 'motion', sound: false });
    assert.deepEqual(readShare(JSON.stringify({ height: 1000, fps: 24, hint: 'text', sound: 'yes' })), SHARE_DEFAULTS);
});

test("a share goes at the member's choices, held to the den's limits", () => {
    assert.deepEqual(shareQuality(limits, { height: 1440, fps: 60 }), { height: 1080, fps: 30, bitrate: 2_000_000 });
    assert.deepEqual(shareQuality({ ...limits, share_height: 2160, share_fps: 60 }, { height: 720, fps: 15 }), { height: 720, fps: 15, bitrate: 2_000_000 });
});

test("a share keeps its shape and no more pixels than the den's size allows", () => {
    assert.deepEqual(fitPixels(1920, 1080, 1080), { width: 1920, height: 1080, scale: 1 });
    assert.deepEqual(fitPixels(1280, 720, 1080), { width: 1280, height: 720, scale: 1 });
    // A 4K screen comes down to 1080p.
    const uhd = fitPixels(3840, 2160, 1080);
    assert.equal(uhd.scale, 2);
    assert.deepEqual([uhd.width, uhd.height], [1920, 1080]);
    // An ultrawide one keeps its shape, with as many pixels as 1080p.
    const wide = fitPixels(3440, 1440, 1080);
    assert.ok(wide.width * wide.height <= 1920 * 1080, `${wide.width}×${wide.height}`);
    assert.ok(Math.abs(wide.width / wide.height - 3440 / 1440) < 0.01);
    assert.ok(wide.width > 1920, 'an ultrawide share comes out wider than 1920');
    // So does a window standing on end.
    const tall = fitPixels(1440, 2560, 720);
    assert.ok(tall.width * tall.height <= 1280 * 720);
    assert.ok(tall.height > 720);
    assert.equal(tall.width % 2, 0);
    assert.equal(tall.height % 2, 0);
    // A size the browser hasn't told yet is left alone.
    assert.equal(fitPixels(undefined, undefined, 1080).scale, 1);
});

test('the browser is asked for the frame rate, and for sound only when wanted, without the call', () => {
    const withSound = displayOptions({ height: 1080, fps: 30 }, true);
    assert.deepEqual(withSound.video, { frameRate: { ideal: 30, max: 30 } });
    assert.equal(withSound.audio.restrictOwnAudio, true);
    assert.equal(withSound.audio.echoCancellation, false);
    assert.equal(withSound.selfBrowserSurface, 'exclude');
    const silent = displayOptions({ height: 720, fps: 15 }, false);
    assert.equal(silent.audio, false);
    assert.equal(silent.systemAudio, 'exclude');
});

// An offer the way the den writes one: the microphone's section first,
// then others' voices, the member's screen and its sound, and a share
// they watch.
const sdp = [
    'v=0', 'o=- 1 2 IN IP4 0.0.0.0', 's=-', 't=0 0',
    'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:0', 'a=rtpmap:111 opus/48000/2', 'a=recvonly',
    'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:1', 'a=msid:7 audio', 'a=sendonly',
    'm=video 9 UDP/TLS/RTP/SAVPF 98', 'a=mid:2', 'a=rtpmap:98 VP9/90000', 'a=recvonly',
    'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:3', 'a=recvonly',
    'm=video 9 UDP/TLS/RTP/SAVPF 98', 'a=mid:4', 'a=msid:screen-7 screen', 'a=sendonly',
    '',
].join('\r\n');

test('the screen and its sound go on the sections the den receives them on, past the microphone', () => {
    assert.deepEqual(receivingSections(sdp), { screen: '2', sound: '3' });
    const first = sdp.split('m=video')[0];
    assert.deepEqual(receivingSections(first), { screen: null, sound: null });
    const noSound = sdp.replace('a=mid:3\r\na=recvonly', 'a=mid:3\r\na=inactive');
    assert.deepEqual(receivingSections(noSound), { screen: '2', sound: null });
});

test("a share's stream names its member, and nothing else does", () => {
    assert.equal(screenOwner('screen-7'), '7');
    assert.equal(screenOwner('screen-1234567890'), '1234567890');
    for (const id of ['7', 'screen-', 'screen-07', 'screen-7 ', 'xscreen-7', 'screen-a']) assert.equal(screenOwner(id), '', id);
});

test('who shares, and who watches whom', () => {
    const call = { channel_id: '41', members: [{ id: '7', sharing: true, sound: true }, { id: '8', watching: ['7'] }, { id: '9', watching: ['7'] }] };
    assert.deepEqual(sharersIn(call), ['7']);
    assert.deepEqual(watchersOf(call, '7'), ['8', '9']);
    assert.deepEqual(watchersOf(call, '8'), []);
    assert.deepEqual(watchersOf(undefined, '7'), []);
    assert.equal(sharingAcross([call, { channel_id: '42', members: [{ id: '3', sharing: true }] }]), 2);
});

test("a share the den wouldn't take is refused before the member picks one", () => {
    const calls = [{ channel_id: '41', members: [{ id: '7', sharing: true }] }];
    assert.match(shareBlocked(limits, calls), /one share at a time/);
    assert.equal(shareBlocked({ ...limits, shares: 2 }, calls), '');
    assert.match(shareBlocked({ ...limits, shares: 0 }, []), /doesn't allow/);
    assert.equal(shareBlocked(null, calls), '');
});

test('each refusal says why', () => {
    for (const reason of ['off', 'full', 'staff_muted', 'rate_limited']) {
        assert.notEqual(refusalText('share', reason, limits), "The den couldn't start your share.", reason);
    }
    assert.match(refusalText('watch', 'full', limits), /\(8\)/);
    assert.match(refusalText('watch', 'full', limits), new RegExp(String(MAX_WATCHING)));
    assert.equal(refusalText('watch', 'not_sharing'), 'That share has ended.');
    assert.equal(refusalText('watch', 'something new'), "The den couldn't show you that share.");
});

test("the limits' cost at most", () => {
    const cost = shareCost(limits, 96000);
    assert.equal(Math.round(cost.video), 16_800_000);
    assert.equal(Math.round(cost.voice), Math.round(2 * 14 * 96000 * 1.2));
    assert.equal(mbps(cost.video), '17 Mbps');
    assert.equal(mbps(3_225_600), '3.2 Mbps');
});
