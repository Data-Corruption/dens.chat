import assert from 'node:assert/strict';
import { test } from 'node:test';
import { labelParts } from '../assets/js/src/manageden.jsx';
import labelCases from '../../denclient/testdata/labels.json';

// A device's label shows as its name, with what's in the parentheses at its
// end, a computer's system or a phone's model, under it, as Dens makes
// them (M13.3); the Go tests read the same cases.
test('a device shows by its name, with what it is under it', () => {
    for (const c of labelCases.cases) {
        assert.deepEqual(labelParts(c.label), { name: c.name, detail: c.detail }, c.label);
    }
    assert.deepEqual(labelParts(''), { name: '', detail: '' });
    assert.deepEqual(labelParts(undefined), { name: '', detail: '' });
});
