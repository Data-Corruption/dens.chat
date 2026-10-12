// The browser and system the settings describe (M8.3).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { browserOf, systemOf } from '../assets/js/src/platform.js';

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
