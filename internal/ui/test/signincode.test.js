import { test } from 'node:test';
import assert from 'node:assert/strict';
import { codeModules, codePath, readSignInCode } from '../assets/js/src/signincode.js';
import { decodeMatrix } from '../assets/js/src/qr.js';
import cases from '../../denproto/testdata/signincodes.json';
import made from '../../denclient/testdata/qrcodes.json';

test('a scanned code reads as Add device\'s, as digits, or as nothing, as the Go reader reads it', () => {
    assert.ok(cases.cases.length > 50);
    for (const c of cases.cases) {
        const want = c.kind === 'device' ? { kind: 'device', url: c.url, username: c.username }
            : c.kind === 'digits' ? { kind: 'digits', digits: c.digits } : { kind: '' };
        assert.deepEqual(readSignInCode(c.text), want, `${JSON.stringify(c.text)}: ${c.why || c.kind}`);
    }
    for (const odd of [undefined, null, 1234567890123456, {}, ['1234567890123456']]) assert.deepEqual(readSignInCode(odd), { kind: '' });
});

test("the service's codes draw as they read", () => {
    for (const code of made.codes) {
        const dark = codeModules(code);
        assert.ok(dark, `version ${code.version}`);
        // The path's rectangles, filled back in, are the code's modules.
        const filled = new Set();
        for (const [, x, y, run] of codePath(code).matchAll(/M(\d+) (\d+)h(\d+)v1h-\3z/g)) {
            for (let i = 0; i < Number(run); i++) filled.add(`${Number(x) + i},${y}`);
        }
        assert.equal(decodeMatrix((x, y) => filled.has(`${x},${y}`), code.size), code.text);
        for (let y = 0; y < code.size; y++) for (let x = 0; x < code.size; x++) assert.equal(filled.has(`${x},${y}`), dark(x, y));
    }
});

test("a code the service didn't make draws as nothing", () => {
    const code = made.codes[0];
    const bad = [
        null, {}, { size: 25 }, { ...code, size: 61, rows: [] }, { ...code, size: 22 }, { ...code, size: '29' },
        { ...code, rows: code.rows.slice(1) }, { ...code, rows: [...code.rows.slice(1), 'zz'.repeat(4)] },
        { ...code, rows: code.rows.map((r) => r.toUpperCase()) }, { ...code, rows: code.rows.map((r) => r + '00') },
        { ...code, rows: code.rows.map(() => 1) },
    ];
    for (const b of bad) {
        assert.equal(codeModules(b), null, JSON.stringify(b)?.slice(0, 60));
        assert.equal(codePath(b), '');
    }
});
