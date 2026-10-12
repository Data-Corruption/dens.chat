// The owner's edits to a den in its Manage dialog (M8.3).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { day, dayTime } from '../assets/js/src/components.jsx';
import { callFields, changed, changesOf, draftOf } from '../assets/js/src/dendraft.js';
import { CALL_PRESETS } from '../assets/js/src/share.js';

const MB = 1 << 20;
const GB = 1 << 30;
const den = { name: 'Den', url: 'https://den.example.com' };
const view = {
    limits: { file_size: 100 * MB, member_storage: 2 * GB, den_storage: 50 * GB },
    retention: 0,
    call_limits: CALL_PRESETS.small,
};

test("a draft holds the den's settings as its fields' text", () => {
    const d = draftOf(den, view);
    assert.deepEqual(d, {
        name: 'Den', url: 'https://den.example.com', file: '100', member: '2', den: '50', retain: false, days: '90',
        members: '15', callers: '30', shares: '1', viewers: '8', bitrate: '2', height: '1080', fps: '30',
    });
    assert.equal(changed(d, d), false);
    assert.deepEqual(changesOf(d, d), { body: {}, problem: '' });
});

test('only what changed goes to the den, as one update', () => {
    const base = draftOf(den, view);
    const draft = { ...base, name: ' New name ', retain: true, days: '30', ...callFields(CALL_PRESETS.large) };
    assert.equal(changed(draft, base), true);
    assert.deepEqual(changesOf(draft, base), { body: { name: 'New name', retention: 30, call_limits: CALL_PRESETS.large }, problem: '' });
    const sizes = changesOf({ ...base, file: '200', member: '4' }, base);
    assert.deepEqual(sizes.body, { limits: { file_size: 200 * MB, member_storage: 4 * GB, den_storage: 50 * GB } });
    // Days typed while auto-delete is off change nothing.
    assert.equal(changed({ ...base, days: '7' }, base), false);
});

test('a field out of range stops the save with what to fix', () => {
    const base = draftOf(den, view);
    const problem = (changes) => changesOf({ ...base, ...changes }, base).problem;
    assert.equal(problem({ name: '  ' }), 'The den needs a name.');
    assert.equal(problem({ file: '2000' }), 'Max file size is 1 to 1,024 MB.');
    assert.equal(problem({ member: '0.01' }), 'Storage per member is at least the max file size.');
    assert.equal(problem({ den: '1' }), 'Total storage is at least the storage per member.');
    assert.equal(problem({ retain: true, days: '0' }), 'Messages are kept 1 to 3,650 days.');
    assert.equal(problem({ members: '31' }), 'Max people per call is 2 to 30.');
    assert.equal(problem({ bitrate: '0.1' }), 'Max bitrate is 0.25 to 50 Mbps.');
    assert.equal(problem({ height: '900' }), 'Max resolution is 720p, 1080p, 1440p or 4K.');
    assert.equal(changed({ ...base, file: '2000' }, base), true);
});

test('dates take one form, with the time where it matters', () => {
    const at = Date.UTC(2026, 9, 12, 15, 4);
    assert.equal(day(at, 'en-US'), new Intl.DateTimeFormat('en-US', { dateStyle: 'medium' }).format(at));
    assert.match(day(at, 'en-US'), /^Oct 1[12], 2026$/);
    assert.equal(dayTime(at, 'en-US'), new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeStyle: 'short' }).format(at));
});
