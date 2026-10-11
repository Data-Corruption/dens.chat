// How the settings show the client's cache (M6).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { CACHE_SIZES, cacheSizes, cacheUsage } from '../assets/js/src/storage.js';
import { formatSize } from '../assets/js/src/files.jsx';

test('the sizes to choose from run from 512 MiB to 50 GiB, 2 GiB among them', () => {
    assert.deepEqual(CACHE_SIZES.map(formatSize), ['512 MB', '1 GB', '2 GB', '5 GB', '10 GB', '20 GB', '50 GB']);
    assert.ok(CACHE_SIZES.includes(2 * 1024 ** 3));
});

test("a size that isn't one of them shows among them", () => {
    assert.equal(cacheSizes(2 * 1024 ** 3), CACHE_SIZES);
    assert.equal(cacheSizes(0), CACHE_SIZES);
    assert.deepEqual(cacheSizes(3 * 1024 ** 3).map(formatSize), ['512 MB', '1 GB', '2 GB', '3 GB', '5 GB', '10 GB', '20 GB', '50 GB']);
});

test('the cache says how much it holds', () => {
    assert.equal(cacheUsage(0, 2 * 1024 ** 3), 'It holds nothing now.');
    assert.equal(cacheUsage(1536 * 1024 ** 2, 2 * 1024 ** 3), 'It holds 1.5 GB of 2 GB now.');
});
