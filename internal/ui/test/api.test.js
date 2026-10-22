// The page names its version in every request to the service, and loads
// again when a service of another version refuses it (M9.3).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { pageHeaders, setPageVersion, stale } from '../assets/js/src/api.js';

test("requests name the page's version once it's set", () => {
    assert.deepEqual(pageHeaders({ 'Content-Type': 'application/json' }), { 'Content-Type': 'application/json' });
    setPageVersion('v1.2.0');
    assert.deepEqual(pageHeaders(), { 'Dens-Page-Version': 'v1.2.0' });
    assert.deepEqual(pageHeaders({ 'Content-Type': 'application/json' }),
        { 'Content-Type': 'application/json', 'Dens-Page-Version': 'v1.2.0' });
});

test("only a refusal as another version's page is stale", () => {
    const res = (status, reload) => ({ status, headers: { get: (name) => (name === 'Dens-Reload' ? reload : null) } });
    assert.equal(stale(res(409, 'v1.2.0')), true);
    assert.equal(stale(res(409, null)), false);
    assert.equal(stale(res(200, 'v1.2.0')), false);
});
