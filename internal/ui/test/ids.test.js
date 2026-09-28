import assert from 'node:assert/strict';
import { test } from 'node:test';
import { compareIds, newNonce, unread } from '../assets/js/src/ids.js';

test('IDs compare as numbers, even past what JavaScript numbers hold', () => {
    assert.ok(compareIds('9', '10') < 0);
    assert.ok(compareIds('10', '9') > 0);
    assert.ok(compareIds('9007199254740993', '9007199254740992') > 0);
    assert.equal(compareIds('42', '42'), 0);
    assert.ok(compareIds('', '1') < 0);
    assert.ok(compareIds('1', '') > 0);
    assert.equal(compareIds('', ''), 0);
});

test('nonces are 16 random bytes in base64url', () => {
    const seen = new Set();
    for (let i = 0; i < 100; i++) {
        const nonce = newNonce();
        assert.match(nonce, /^[A-Za-z0-9_-]{22}$/);
        assert.equal(Buffer.from(nonce, 'base64url').length, 16);
        seen.add(nonce);
    }
    assert.equal(seen.size, 100);
});

test('a channel is unread past the read position', () => {
    assert.equal(unread({ last_message_id: '5', message_id: '4' }), true);
    assert.equal(unread({ last_message_id: '5', message_id: '5' }), false);
    assert.equal(unread({ last_message_id: '5' }), true);
    assert.equal(unread({}), false);
    assert.equal(unread(undefined), false);
});
