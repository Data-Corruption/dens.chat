import { test } from 'node:test';
import assert from 'node:assert/strict';
import { MAX_VERSION, correct, decodeMatrix, perspective, readQR } from '../src/ui/qr.js';
import made from '../../denclient/testdata/qrcodes.json';

// modules is a code from the file as dark(x, y), the service's own form.
function modules(code) {
    const bytes = code.rows.map((row) => row.match(/../g).map((h) => parseInt(h, 16)));
    return (x, y) => x >= 0 && y >= 0 && x < code.size && y < code.size && ((bytes[y][x >> 3] >> (7 - (x & 7))) & 1) === 1;
}

// random is a seeded generator, so a failure repeats.
function random(seed) {
    let s = seed >>> 0 || 1;
    return () => {
        s ^= s << 13;
        s ^= s >>> 17;
        s ^= s << 5;
        return (s >>> 0) / 2 ** 32;
    };
}

// dataModule says whether a module of a code holds data: outside the
// finders and the format beside them, the timing lines and, from version
// 7, the version information. Alignment patterns are left in the pool, as
// flipping one costs a codeword at most.
function dataModule(size, x, y) {
    if ((x < 9 && y < 9) || (x >= size - 8 && y < 9) || (x < 9 && y >= size - 8) || x === 6 || y === 6) return false;
    if (size >= 45 && ((x >= size - 11 && y < 6) || (x < 6 && y >= size - 11))) return false;
    return true;
}

// flipped is a code's modules with n of its data modules flipped.
function flipped(code, n, rand) {
    const dark = modules(code);
    const flips = new Set();
    while (flips.size < n) {
        const x = Math.floor(rand() * code.size);
        const y = Math.floor(rand() * code.size);
        if (dataModule(code.size, x, y)) flips.add(y * code.size + x);
    }
    return (x, y) => dark(x, y) !== flips.has(y * code.size + x);
}

test('every code the service makes reads from its modules', () => {
    assert.ok(made.codes.length >= 20);
    for (const code of made.codes) {
        assert.equal(decodeMatrix(modules(code), code.size), code.text, `version ${code.version} ${code.level} mask ${code.mask}`);
    }
    const versions = new Set(made.codes.map((c) => c.version));
    for (let v = 1; v <= MAX_VERSION; v++) assert.ok(versions.has(v), `version ${v} among the codes`);
    assert.deepEqual(new Set(made.codes.map((c) => c.mask)), new Set([0, 1, 2, 3, 4, 5, 6, 7]));
    assert.deepEqual(new Set(made.codes.map((c) => c.level)), new Set(['L', 'M', 'Q', 'H']));
});

test('a code with damaged modules reads, within what its level fixes', () => {
    const rand = random(7);
    for (const code of made.codes.filter((c) => c.level !== 'L' && c.version >= 3)) {
        for (let round = 0; round < 5; round++) {
            assert.equal(decodeMatrix(flipped(code, 6, rand), code.size), code.text, `version ${code.version} ${code.level}`);
        }
    }
    // Three wrong bits in a copy of the format, and the other copy lost.
    const code = made.codes[0];
    const dark = modules(code);
    const bad = new Set(['0,8', '2,8', '8,4']);
    for (let i = code.size - 8; i < code.size; i++) bad.add(`${i},8`);
    assert.equal(decodeMatrix((x, y) => dark(x, y) !== bad.has(`${x},${y}`), code.size), code.text);
});

test("a code too damaged to fix, or no code, reads as nothing and doesn't throw", () => {
    const rand = random(11);
    for (const code of made.codes) {
        const dark = flipped(code, Math.floor(code.size * code.size * 0.3), rand);
        assert.equal(decodeMatrix(dark, code.size), null, `version ${code.version}`);
    }
    for (let size = 21; size <= 17 + 4 * MAX_VERSION; size += 4) {
        for (let round = 0; round < 20; round++) {
            const bits = Array.from({ length: size * size }, () => rand() < 0.5);
            const text = decodeMatrix((x, y) => bits[y * size + x], size);
            assert.ok(text === null || typeof text === 'string');
        }
    }
    for (const size of [17, 20, 22, 61, 177, 0, -3, 21.5]) assert.equal(decodeMatrix(() => true, size), null, `size ${size}`);
});

// rsEncode is Reed and Solomon's encoder, written here apart from the
// reader: the data's check bytes over GF(256), the remainder by the
// generator with roots 2^0 to 2^(ecc-1).
function rsEncode(data, ecc) {
    const exp = [];
    const log = [];
    for (let i = 0, x = 1; i < 255; i++) {
        exp[i] = x;
        log[x] = i;
        x = (x << 1) ^ (x & 0x80 ? 0x11d : 0);
    }
    const mul = (a, b) => (a && b ? exp[(log[a] + log[b]) % 255] : 0);
    let gen = [1];
    for (let i = 0; i < ecc; i++) {
        const next = new Array(gen.length + 1).fill(0);
        gen.forEach((c, j) => {
            next[j] ^= c;
            next[j + 1] ^= mul(c, exp[i]);
        });
        gen = next;
    }
    const rem = [...data, ...new Array(ecc).fill(0)];
    for (let i = 0; i < data.length; i++) {
        const f = rem[i];
        if (f) gen.forEach((c, j) => (rem[i + j] ^= mul(c, f)));
    }
    return [...data, ...rem.slice(data.length)];
}

test('Reed-Solomon fixes up to half its check bytes, and says when it can\'t', () => {
    const rand = random(3);
    for (const [data, ecc] of [[19, 7], [16, 10], [9, 17], [43, 26], [15, 28], [116, 30]]) {
        for (let round = 0; round < 30; round++) {
            const block = rsEncode(Array.from({ length: data }, () => Math.floor(rand() * 256)), ecc);
            const sent = Uint8Array.from(block);
            const errors = Math.floor(rand() * (Math.floor(ecc / 2) + 1));
            const at = new Set();
            while (at.size < errors) at.add(Math.floor(rand() * block.length));
            for (const i of at) sent[i] ^= 1 + Math.floor(rand() * 255);
            assert.ok(correct(sent, ecc), `${errors} errors in ${data}+${ecc}`);
            assert.deepEqual([...sent], block);
        }
        // Past what it fixes, it fixes something whole or says no.
        for (let round = 0; round < 30; round++) {
            const block = rsEncode(Array.from({ length: data }, () => Math.floor(rand() * 256)), ecc);
            const sent = Uint8Array.from(block);
            const at = new Set();
            while (at.size < Math.floor(ecc / 2) + 3) at.add(Math.floor(rand() * block.length));
            for (const i of at) sent[i] ^= 1 + Math.floor(rand() * 255);
            if (correct(sent, ecc)) assert.deepEqual([...rsEncode([...sent.subarray(0, data)], ecc)], [...sent]);
        }
    }
});

test('perspective takes four points to four others', () => {
    const near = (a, b) => a.every((v, i) => Math.abs(v - b[i]) < 1e-6);
    const from = [0, 0, 10, 0, 10, 10, 0, 10];
    const to = [100, 50, 220, 70, 230, 210, 80, 190];
    const map = perspective(from, to);
    for (let i = 0; i < 4; i++) assert.ok(near(map(from[2 * i], from[2 * i + 1]), [to[2 * i], to[2 * i + 1]]), `corner ${i}`);
    const back = perspective(to, from);
    const [x, y] = map(3, 7);
    assert.ok(near(back(x, y), [3, 7]));
    const scale = perspective(from, [0, 0, 20, 0, 20, 20, 0, 20]);
    assert.ok(near(scale(5, 2), [10, 4]));
});

// render draws a code as a camera might see it on a screen: its modules
// and a quiet zone of four on a card of light, corners at quad in a frame
// of width by height, the frame around it background, then blurred, lit
// unevenly and noised. clutter adds dark shapes and a lone finder. shrink
// draws the camera's frame that many times larger, then scales it down to
// the size given, as the page does.
function render(code, { shrink = 1, ...o }) {
    if (shrink === 1) return draw(code, o);
    const big = draw(code, { ...o, width: o.width * shrink, height: o.height * shrink, quad: o.quad.map((v) => v * shrink), fine: 1 });
    const data = new Uint8ClampedArray(o.width * o.height * 4);
    for (let y = 0; y < o.height; y++) {
        for (let x = 0; x < o.width; x++) {
            let sum = 0;
            for (let dy = 0; dy < shrink; dy++) for (let dx = 0; dx < shrink; dx++) sum += big.data[((y * shrink + dy) * big.width + x * shrink + dx) * 4];
            const i = (y * o.width + x) * 4;
            data[i] = data[i + 1] = data[i + 2] = sum / shrink / shrink;
            data[i + 3] = 255;
        }
    }
    return { data, width: o.width, height: o.height };
}

function draw(code, { width = 360, height = 360, quad, blur = 0, noise = 0, dark = 20, light = 235, background = light, ramp = 0, clutter = false, seed = 1, fine = 3 }) {
    const dk = modules(code);
    const n = code.size;
    const q = 4;
    const toModule = perspective(quad, [-q, -q, n + q, -q, n + q, n + q, -q, n + q]);
    let gray = new Float32Array(width * height);
    const shapes = [];
    if (clutter) {
        const r = random(seed + 99);
        for (let i = 0; i < 12; i++) shapes.push([r() * width, r() * height, 4 + r() * 30, 4 + r() * 30]);
    }
    const finderAt = (fx, fy, size, x, y) => {
        const mx = Math.floor((x - fx) / size);
        const my = Math.floor((y - fy) / size);
        if (mx < 0 || my < 0 || mx > 6 || my > 6) return null;
        const ring = Math.min(mx, my, 6 - mx, 6 - my);
        return ring !== 1;
    };
    for (let y = 0; y < height; y++) {
        for (let x = 0; x < width; x++) {
            let sum = 0;
            for (let sy = 0; sy < fine; sy++) {
                for (let sx = 0; sx < fine; sx++) {
                    const px = x + (sx + 0.5) / fine;
                    const py = y + (sy + 0.5) / fine;
                    const [mx, my] = toModule(px, py);
                    let v = background;
                    if (mx >= -q && my >= -q && mx < n + q && my < n + q) v = dk(Math.floor(mx), Math.floor(my)) ? dark : light;
                    else if (clutter) {
                        if (shapes.some(([sx0, sy0, w, h]) => px >= sx0 && py >= sy0 && px < sx0 + w && py < sy0 + h)) v = dark;
                        const lone = finderAt(width - 50, height - 50, 5, px, py);
                        if (lone !== null) v = lone ? dark : light;
                    }
                    sum += v;
                }
            }
            gray[y * width + x] = sum / fine / fine;
        }
    }
    for (let pass = 0; pass < blur; pass++) {
        const next = new Float32Array(gray.length);
        for (let y = 0; y < height; y++) {
            for (let x = 0; x < width; x++) {
                let s = 0;
                let c = 0;
                for (let dy = -1; dy <= 1; dy++) {
                    for (let dx = -1; dx <= 1; dx++) {
                        const xx = x + dx;
                        const yy = y + dy;
                        if (xx >= 0 && yy >= 0 && xx < width && yy < height) {
                            s += gray[yy * width + xx];
                            c++;
                        }
                    }
                }
                next[y * width + x] = s / c;
            }
        }
        gray = next;
    }
    const r = random(seed);
    const data = new Uint8ClampedArray(width * height * 4);
    for (let i = 0; i < gray.length; i++) {
        const lit = gray[i] * (1 - ramp * ((i % width) / width)) + (r() - 0.5) * 2 * noise;
        data[i * 4] = data[i * 4 + 1] = data[i * 4 + 2] = lit;
        data[i * 4 + 3] = 255;
    }
    return { data, width, height };
}

// square is a quad of side s at x, y, turned by angle degrees about its
// middle.
function square(x, y, s, angle = 0) {
    const c = Math.cos((angle * Math.PI) / 180);
    const sn = Math.sin((angle * Math.PI) / 180);
    const mx = x + s / 2;
    const my = y + s / 2;
    return [[x, y], [x + s, y], [x + s, y + s], [x, y + s]].flatMap(([px, py]) => [
        mx + (px - mx) * c - (py - my) * sn,
        my + (px - mx) * sn + (py - my) * c,
    ]);
}

test('a code reads from a frame, as large as a phone holds it', () => {
    for (const code of made.codes) {
        const side = (code.size + 8) * 4;
        const frame = render(code, { width: side + 80, height: side + 60, quad: square(40, 30, side) });
        assert.equal(readQR(frame), code.text, `version ${code.version} ${code.level} mask ${code.mask}`);
    }
});

test('a code reads turned any way, and from three pixels a module', () => {
    const sign = made.codes.filter((c) => c.version <= 3);
    for (const code of [...sign, ...made.codes.filter((c) => c.version >= 7)]) {
        for (const angle of [0, 33, 90, 180, 270, -20]) {
            const side = (code.size + 8) * 4;
            const frame = render(code, { width: side + 120, height: side + 120, quad: square(60, 60, side, angle) });
            assert.equal(readQR(frame), code.text, `version ${code.version}, turned ${angle}`);
        }
    }
    for (const code of made.codes.filter((c) => c.version === MAX_VERSION || c.version === 5)) {
        const side = (code.size + 8) * 3;
        assert.equal(readQR(render(code, { width: side + 40, height: side + 40, quad: square(20, 20, side) })), code.text, `version ${code.version} small`);
    }
});

test('a code reads tilted away from the camera, blurred, dim, noisy and unevenly lit', () => {
    // Version 1 has no alignment pattern to correct a tilt by, so it's
    // tilted less; Dens makes none.
    for (const code of made.codes.filter((c) => c.version <= 3 || c.version === 7)) {
        const s = (code.size + 8) * 5;
        const t = code.version === 1 ? 0.15 : 1;
        // Its top edge farther than its bottom, and a little to the side.
        const far = [70, 40, 70 + s * 0.85, 55, 50 + s, 40 + s, 30, 30 + s * 0.98];
        const keystone = square(50, 40, s).map((v, i) => v + t * (far[i] - v));
        for (let seed = 1; seed <= 4; seed++) {
            const frame = render(code, { width: s + 160, height: s + 120, quad: keystone, blur: 1, noise: 22, dark: 70, light: 190, ramp: 0.35, seed, shrink: 2 });
            assert.equal(readQR(frame), code.text, `version ${code.version} ${code.level}, noise ${seed}`);
        }
    }
});

test('a code reads among other shapes, and on a dark page', () => {
    for (const code of made.codes.slice(0, 3)) {
        const side = (code.size + 8) * 5;
        assert.equal(readQR(render(code, { width: 640, height: 480, quad: square(150, 90, side, 8), clutter: true, seed: 5 })), code.text, 'among shapes');
        assert.equal(readQR(render(code, { width: 480, height: 400, quad: square(90, 60, side), background: 25, light: 240 })), code.text, 'on a dark page');
    }
});

test('a frame of no code, or not a frame, reads as nothing', () => {
    const rand = random(19);
    const frame = (width, height, value) => {
        const data = new Uint8ClampedArray(width * height * 4);
        for (let i = 0; i < data.length; i += 4) {
            data[i] = data[i + 1] = data[i + 2] = value();
            data[i + 3] = 255;
        }
        return { data, width, height };
    };
    for (let round = 0; round < 10; round++) assert.equal(readQR(frame(320, 240, () => rand() * 256)), null, 'noise');
    assert.equal(readQR(frame(320, 240, () => 0)), null, 'black');
    assert.equal(readQR(frame(320, 240, () => 255)), null, 'white');
    assert.equal(readQR(frame(10, 10, () => 0)), null, 'too small');
    assert.equal(readQR({ data: new Uint8ClampedArray(16), width: 100, height: 100 }), null, 'too few pixels');
    assert.equal(readQR({ data: new Uint8ClampedArray(0), width: NaN, height: 100 }), null, 'no size');
    // Three finders, and noise where the rest of a code would be.
    const code = made.codes[0];
    const dark = modules(code);
    const scrambled = { ...code };
    const r = random(23);
    const noisy = (x, y) => (dataModule(code.size, x, y) ? r() < 0.5 : dark(x, y));
    const bits = [];
    for (let y = 0; y < code.size; y++) for (let x = 0; x < code.size; x++) bits.push(noisy(x, y));
    scrambled.rows = [];
    for (let y = 0; y < code.size; y++) {
        let row = '';
        for (let x = 0; x < code.size; x += 8) {
            let b = 0;
            for (let k = 0; k < 8; k++) b = (b << 1) | (x + k < code.size && bits[y * code.size + x + k] ? 1 : 0);
            row += b.toString(16).padStart(2, '0');
        }
        scrambled.rows.push(row);
    }
    const side = (code.size + 8) * 4;
    assert.equal(readQR(render(scrambled, { width: side + 40, height: side + 40, quad: square(20, 20, side) })), null, 'finders and noise');
});

test('three large finders with no code between them read as nothing, and soon', () => {
    const width = 640;
    const height = 480;
    const data = new Uint8ClampedArray(width * height * 4).fill(235);
    const finder = (fx, fy, size) => {
        for (let y = 0; y < 7 * size; y++) {
            for (let x = 0; x < 7 * size; x++) {
                const mx = Math.floor(x / size);
                const my = Math.floor(y / size);
                const ring = Math.min(mx, my, 6 - mx, 6 - my);
                const i = ((fy + y) * width + fx + x) * 4;
                data[i] = data[i + 1] = data[i + 2] = ring === 1 ? 235 : 20;
            }
        }
    };
    for (const [fx, fy] of [[20, 20], [440, 20], [20, 280]]) finder(fx, fy, 25);
    const start = performance.now();
    assert.equal(readQR({ data, width, height }), null);
    const took = performance.now() - start;
    assert.ok(took < 500, `${took.toFixed(1)} ms`);
});

test('reading a phone camera\'s frame takes a fraction of a frame\'s time', () => {
    const code = made.codes[0];
    const side = (code.size + 8) * 6;
    const frame = render(code, { width: 640, height: 480, quad: square(200, 100, side, 5), noise: 10 });
    readQR(frame);
    const start = performance.now();
    for (let i = 0; i < 5; i++) assert.equal(readQR(frame), code.text);
    const each = (performance.now() - start) / 5;
    assert.ok(each < 250, `${each.toFixed(1)} ms a frame`);
});
