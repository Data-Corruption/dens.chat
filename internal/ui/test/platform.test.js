// The browser and system the settings describe (M8.3).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { browserOf, canCall, canPopOut, canShare, systemOf } from '../assets/js/src/platform.js';

const AGENTS = {
    chromeWindows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
    edgeWindows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0',
    firefoxLinux: 'Mozilla/5.0 (X11; Linux x86_64; rv:143.0) Gecko/20100101 Firefox/143.0',
    waterfoxWindows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:128.0) Gecko/20100101 Firefox/128.0',
    chromiumLinux: 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
    chromeMac: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
};

test('Firefox and its forks are Firefox, and the rest are taken for Chromium', () => {
    assert.equal(browserOf(AGENTS.firefoxLinux), 'firefox');
    assert.equal(browserOf(AGENTS.waterfoxWindows), 'firefox');
    assert.equal(browserOf(AGENTS.chromeWindows), 'chromium');
    assert.equal(browserOf(AGENTS.edgeWindows), 'chromium');
    assert.equal(browserOf(''), 'chromium');
});

test('the system comes from the user agent', () => {
    assert.equal(systemOf(AGENTS.chromeWindows), 'windows');
    assert.equal(systemOf(AGENTS.waterfoxWindows), 'windows');
    assert.equal(systemOf(AGENTS.firefoxLinux), 'linux');
    assert.equal(systemOf(AGENTS.chromiumLinux), 'linux');
    assert.equal(systemOf(AGENTS.chromeMac), 'mac');
    assert.equal(systemOf(''), 'other');
});

// What a browser has decides what the page offers (M9.1), as its user
// agent can't: Android's WebView says it's Linux.
test('calls need WebRTC, and sharing a screen getDisplayMedia too', () => {
    const pc = function RTCPeerConnection() {};
    const desktop = { RTCPeerConnection: pc, navigator: { mediaDevices: { getUserMedia() {}, getDisplayMedia() {} } } };
    // Android's WebView has WebRTC, but no getDisplayMedia.
    const webView = { RTCPeerConnection: pc, navigator: { mediaDevices: { getUserMedia() {} } } };
    // GNOME Web has had no WebRTC since WebKitGTK 2.54, so neither, and a
    // page outside a secure context has no mediaDevices.
    const gnomeWeb = { navigator: { mediaDevices: { getUserMedia() {}, getDisplayMedia() {} } } };
    const insecure = { RTCPeerConnection: pc, navigator: {} };
    assert.deepEqual([canCall(desktop), canShare(desktop)], [true, true]);
    assert.deepEqual([canCall(webView), canShare(webView)], [true, false]);
    assert.deepEqual([canCall(gnomeWeb), canShare(gnomeWeb)], [false, false]);
    assert.deepEqual([canCall(insecure), canShare(insecure)], [true, false]);
    assert.deepEqual([canCall({}), canShare({})], [false, false]);
});

// A share's window of its own is for a desktop's monitors (M11).
test("a share opens in a window of its own where the browser opens one that moves between monitors", () => {
    const pc = function RTCPeerConnection() {};
    const desktop = { RTCPeerConnection: pc, navigator: { mediaDevices: { getUserMedia() {}, getDisplayMedia() {} } }, open() {} };
    const webView = { RTCPeerConnection: pc, navigator: { mediaDevices: { getUserMedia() {} } }, open() {} };
    assert.equal(canPopOut(desktop), true);
    assert.equal(canPopOut(webView), false, "Android's WebView");
    assert.equal(canPopOut({ ...desktop, open: undefined }), false);
});
