// What someone pastes to add a den (M8.3).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readPasted } from '../assets/js/src/join.js';

test('an invite is an invite, whatever space is around it', () => {
    assert.deepEqual(readPasted('  dens1:AbC-_123\n'), { kind: 'invite', value: 'dens1:AbC-_123' });
});

test("a den's address is an address, and a bare host one served over https", () => {
    assert.deepEqual(readPasted('https://den.example.com'), { kind: 'address', value: 'https://den.example.com' });
    assert.deepEqual(readPasted('https://den.example.com/'), { kind: 'address', value: 'https://den.example.com/' });
    assert.deepEqual(readPasted('http://127.0.0.1:18585'), { kind: 'address', value: 'http://127.0.0.1:18585' });
    assert.deepEqual(readPasted('den.example.com'), { kind: 'address', value: 'https://den.example.com' });
    assert.deepEqual(readPasted('den.example.com:8443'), { kind: 'address', value: 'https://den.example.com:8443' });
});

test('anything else is neither', () => {
    for (const text of ['', 'hello', 'dens2:abc', 'https://den.example.com/path', 'not a host.', 'ftp://den.example.com']) {
        assert.equal(readPasted(text).kind, '', text);
    }
});
