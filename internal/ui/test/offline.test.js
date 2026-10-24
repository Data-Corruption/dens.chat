// When the page says a den is offline over its messages (M6.2).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { leaveOffer, showsOffline } from '../assets/js/src/offline.js';

test('the note shows only while the den failed to connect', () => {
    assert.equal(showsOffline(true, 'offline'), true);
    for (const state of ['connected', 'connecting', 'revoked', 'removed']) assert.equal(showsOffline(true, state), false, state);
    // Without the stream from Dens on this computer, the page says that
    // instead.
    assert.equal(showsOffline(false, 'offline'), false);
});

test("Leave den leaves a den that answers, and removes one Dens can't reach from this computer (M10)", () => {
    assert.equal(leaveOffer({ state: 'connected' }), 'leave');
    for (const state of ['offline', 'connecting']) assert.equal(leaveOffer({ state }), 'unreachable', state);
    for (const state of ['revoked', 'removed']) assert.equal(leaveOffer({ state }), 'gone', state);
    // The den this computer hosts goes only by moving it.
    assert.equal(leaveOffer({ state: 'offline', own: true }), null);
});
