// When the page says a den is offline over its messages (M6.2).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { showsOffline } from '../assets/js/src/offline.js';

test('the note shows only while the den failed to connect', () => {
    assert.equal(showsOffline(true, 'offline'), true);
    for (const state of ['connected', 'connecting', 'revoked', 'removed']) assert.equal(showsOffline(true, state), false, state);
    // Without the stream from Dens on this computer, the page says that
    // instead.
    assert.equal(showsOffline(false, 'offline'), false);
});
