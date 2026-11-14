import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { ALWAYS, HoverRow, ON_HOVER, setAnimations } from '../assets/js/src/animations.js';
import { clampOffset, coverScale, sourceRect, zoomAt } from '../assets/js/src/crop.jsx';
import {
    Attachments, attachmentKind, downloadURL, fileWhere, fitBox, formatDuration, formatSize, needsPreview, playbackTime, previewSize, step,
} from '../assets/js/src/files.jsx';

test('sizes read as people write them', () => {
    assert.equal(formatSize(0), '0 B');
    assert.equal(formatSize(1023), '1023 B');
    assert.equal(formatSize(1024), '1 KB');
    assert.equal(formatSize(1536), '1.5 KB');
    assert.equal(formatSize(25 * 1024 * 1024), '25 MB');
    assert.equal(formatSize(2 * 1024 ** 3), '2 GB');
    assert.equal(formatSize(20 * 1024 ** 4), '20480 GB');
});

test('a preview takes its final size from the stated one, never enlarged', () => {
    assert.deepEqual(fitBox(1600, 1200, 400, 300), { width: 400, height: 300 });
    assert.deepEqual(fitBox(1200, 1600, 400, 300), { width: 225, height: 300 });
    assert.deepEqual(fitBox(200, 100, 400, 300), { width: 200, height: 100 });
    assert.deepEqual(fitBox(8000, 1000, 400, 300), { width: 400, height: 50 });
    // A sliver keeps a size that can be clicked; the preview is cropped.
    assert.deepEqual(fitBox(5000, 10, 400, 300), { width: 400, height: 48 });
    assert.deepEqual(fitBox(0, 0, 400, 300), { width: 400, height: 300 });
});

test('durations read as a player shows them', () => {
    assert.equal(formatDuration(0), '0:00');
    assert.equal(formatDuration(4001), '0:04');
    assert.equal(formatDuration(62_999), '1:02');
    assert.equal(formatDuration(3_723_000), '1:02:03');
});

test("a player shows where it is, and the video's length once known", () => {
    assert.equal(playbackTime(0, 4.001), '0:00 / 0:04');
    assert.equal(playbackTime(61.5, 3723), '1:01 / 1:02:03');
    assert.equal(playbackTime(3, 0), '0:03');
    assert.equal(playbackTime(3, NaN), '0:03');
});

test('a message shows each file as what it is', () => {
    const thumb = { width: 640, height: 480 };
    assert.equal(attachmentKind({ type: 'video/mp4', width: 320, height: 568, thumb }), 'video');
    // Without a preview, a video still plays, in a box of its shape.
    assert.equal(attachmentKind({ type: 'video/webm', width: 1920, height: 1080 }), 'video');
    assert.equal(attachmentKind({ type: 'audio/mp4' }), 'audio');
    assert.equal(attachmentKind({ type: 'image/jpeg', width: 1600, height: 1200, thumb }), 'image');
    // An image too large to preview is a file to download.
    assert.equal(attachmentKind({ type: 'image/jpeg', width: 20000, height: 20000 }), 'file');
    assert.equal(attachmentKind({ type: 'video/mp4' }), 'file');
    assert.equal(attachmentKind({ type: 'application/pdf' }), 'file');
});

test('the page draws a preview only for a video without one', () => {
    assert.equal(needsPreview({ type: 'video/webm', width: 1920, height: 1080 }), true);
    assert.equal(needsPreview({ type: 'video/mp4', width: 320, height: 568, thumb: { width: 320, height: 568 } }), false);
    assert.equal(needsPreview({ type: 'audio/ogg' }), false);
    assert.equal(needsPreview({ type: 'image/png', width: 10, height: 10 }), false);
});

test("a drawn preview keeps the video's shape", () => {
    assert.deepEqual(previewSize(3840, 2160, 1280), { w: 1280, h: 720 });
    assert.deepEqual(previewSize(1080, 1920, 1280), { w: 720, h: 1280 });
    assert.deepEqual(previewSize(64, 48, 1280), { w: 64, h: 48 });
    assert.deepEqual(previewSize(0, 0, 1280), { w: 0, h: 0 });
});

test("a download link carries the file's name as data", () => {
    assert.equal(downloadURL('abc', { id: '7', name: 'a b&c=d/e?.pdf' }), '/api/dens/abc/files/7?download=a%20b%26c%3Dd%2Fe%3F.pdf');
});

test('the crop frame stays covered', () => {
    // A 1000x500 image in a 256 square frame just covers it at 0.512.
    const s = coverScale(1000, 500, 256, 256);
    assert.equal(s, 0.512);
    assert.deepEqual(clampOffset({ x: 10, y: 10 }, 1000, 500, 256, 256, s), { x: 0, y: 0 });
    assert.deepEqual(clampOffset({ x: -1000, y: -5 }, 1000, 500, 256, 256, s), { x: 256 - 512, y: 0 });
});

test('zooming keeps the point under the pointer where it is', () => {
    const o = { x: -100, y: -40 };
    const next = zoomAt(o, 1, 2, 128, 128);
    // The image point under (128, 128) was (228, 168) at scale 1.
    assert.deepEqual(next, { x: 128 - 228 * 2, y: 128 - 168 * 2 });
    const r = sourceRect(next, 256, 256, 2);
    assert.equal(r.x + r.w / 2, 228);
    assert.equal(r.y + r.h / 2, 168);
});

test("the viewer moves through a message's images and stops at the ends", () => {
    assert.equal(step(0, 3, 1), 1);
    assert.equal(step(2, 3, 1), 2);
    assert.equal(step(0, 3, -1), 0);
    assert.equal(step(1, 3, -1), 0);
    assert.equal(step(0, 1, 1), 0);
});

test("a member's list of files says what uses each", () => {
    const view = {
        me: { id: '1' },
        members: [{ id: '1', display_name: 'Alice' }, { id: '2', display_name: 'Bob' }],
        channels: [{ id: '10', kind: 'text', name: 'general' }, { id: '11', kind: 'dm', members: ['1', '2'] }],
    };
    assert.equal(fileWhere({ profile: 'avatar' }, view), 'Your profile picture');
    assert.equal(fileWhere({ profile: 'banner' }, view), 'Your profile banner');
    assert.equal(fileWhere({}, view), 'Waiting to be sent');
    assert.equal(fileWhere({ message_id: '5', channel_id: '10' }, view), 'In #general');
    assert.equal(fileWhere({ message_id: '6', channel_id: '11' }, view), 'In your DM with Bob');
    assert.equal(fileWhere({ message_id: '7', channel_id: '12' }, view), "In a channel you can't see");
});

// A GIF in a message plays as Animations says, as every animated picture
// does: under Always as it nears the screen, its file fetched no sooner,
// and under On hover while the pointer is over its message.
test("a message's GIF plays under Always, and under On hover while the pointer is over its message", async () => {
    const kept = new Map();
    globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, v) };
    globalThis.window = { matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }), addEventListener() {}, removeEventListener() {} };
    const gif = { id: 'g1', name: 'dance.gif', type: 'image/gif', size: 4 << 20, width: 480, height: 270, thumb: true, animated: true };
    const settle = () => new Promise((resolve) => setTimeout(resolve, 20));
    const images = (node, out = []) => {
        if (node.localName === 'img') out.push(node);
        for (const c of node.childNodes || []) images(c, out);
        return out;
    };
    const file = (img) => img && !/\/thumb$/.test(img.attributes.src) && !/\bopacity-0\b/.test(img.attributes.class);
    const root = document.createElement('div');
    const show = () => mount(h(HoverRow, { 'data-row': '' }, h(Attachments, { denID: 'D', files: [gif], onOpen() {} })), root);

    setAnimations(ALWAYS);
    show();
    await settle();
    let [preview, playing] = images(root);
    assert.match(preview.attributes.src, /\/thumb$/, 'its preview first');
    assert.equal(playing.attributes.src, '/api/dens/D/files/g1', 'the GIF over it');
    assert.equal(playing.attributes.loading, 'lazy', 'fetched as it nears the screen');
    playing.dispatch('load');
    await settle();
    assert.ok(/\bopacity-0\b/.test(images(root)[0].attributes.class), 'its preview hidden once it plays');

    setAnimations(ON_HOVER);
    await settle();
    assert.deepEqual(images(root).map((i) => i.attributes.src), ['/api/dens/D/files/g1/thumb'], 'still, without the pointer');
    const row = root.find((n) => n.attributes?.['data-row'] !== undefined);
    row.dispatch('pointerenter', { pointerType: 'mouse' });
    await settle();
    [preview, playing] = images(root);
    assert.ok(file(playing), 'playing under the pointer');
    row.dispatch('pointerleave', { pointerType: 'mouse' });
    await settle();
    assert.equal(images(root).length, 1, 'and still again once it leaves');
    row.dispatch('pointerenter', { pointerType: 'touch' });
    await settle();
    assert.equal(images(root).length, 1, "a finger's touch isn't a hover: a tap opens the viewer");
    mount(null, root);
    delete globalThis.localStorage;
    delete globalThis.window;
});
