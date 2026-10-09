// How the page words a den's retention period (M5).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { deletesNow, keptFor } from '../assets/js/src/retention.js';

test('a period reads in days', () => {
    assert.equal(keptFor(1), '1 day');
    assert.equal(keptFor(30), '30 days');
    assert.equal(keptFor(3650), '3,650 days');
});

test('the settings say what saving a period deletes at once', () => {
    assert.equal(deletesNow({ messages: 0, bytes: 0 }), 'No messages are that old yet, so saving deletes nothing now.');
    assert.equal(deletesNow({ messages: 1, bytes: 0 }), 'Saving this deletes 1 message now.');
    assert.equal(deletesNow({ messages: 1234, bytes: 5 * 1024 * 1024 }), 'Saving this deletes 1,234 messages now, and 5 MB of files.');
});
