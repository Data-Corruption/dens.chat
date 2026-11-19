// Reading QR codes (M22.6), for the sign-in codes a new device scans with
// its camera: a frame's pixels in, the code's text out, or null. It reads
// what Dens makes, versions 1 to 10, held up to a camera on a screen. A
// code is whatever someone shows the camera, so every step is bounded and
// refuses what doesn't fit, and its text is only ever read as a sign-in
// code (signincode.js).

// MAX_VERSION is the largest code read, denclient.MaxCodeVersion.
export const MAX_VERSION = 10;

// Each version's error correction, by level L, M, Q, H: the check bytes in
// each block, then the blocks, as [how many, data bytes in each].
const BLOCKS = [
    null,
    [[7, [1, 19]], [10, [1, 16]], [13, [1, 13]], [17, [1, 9]]],
    [[10, [1, 34]], [16, [1, 28]], [22, [1, 22]], [28, [1, 16]]],
    [[15, [1, 55]], [26, [1, 44]], [18, [2, 17]], [22, [2, 13]]],
    [[20, [1, 80]], [18, [2, 32]], [26, [2, 24]], [16, [4, 9]]],
    [[26, [1, 108]], [24, [2, 43]], [18, [2, 15], [2, 16]], [22, [2, 11], [2, 12]]],
    [[18, [2, 68]], [16, [4, 27]], [24, [4, 19]], [28, [4, 15]]],
    [[20, [2, 78]], [18, [4, 31]], [18, [2, 14], [4, 15]], [26, [4, 13], [1, 14]]],
    [[24, [2, 97]], [22, [2, 38], [2, 39]], [22, [4, 18], [2, 19]], [26, [4, 14], [2, 15]]],
    [[30, [2, 116]], [22, [3, 36], [2, 37]], [20, [4, 16], [4, 17]], [24, [4, 12], [4, 13]]],
    [[18, [2, 68], [2, 69]], [26, [4, 43], [1, 44]], [24, [6, 19], [2, 20]], [28, [6, 15], [2, 16]]],
];

// Where each version's alignment patterns center, across and down.
const ALIGN = [null, [], [6, 18], [6, 22], [6, 26], [6, 30], [6, 34], [6, 22, 38], [6, 24, 42], [6, 26, 46], [6, 28, 50]];

// The format's two bits for a level, in the order of BLOCKS.
const LEVELS = { 1: 0, 0: 1, 3: 2, 2: 3 };

// bch is a code's check bits: data shifted past them, less its remainder
// by the generator.
function bch(data, bits, generator) {
    let value = data << bits;
    const top = 31 - Math.clz32(generator);
    for (let i = 31 - Math.clz32(value); i >= top; i--) {
        if (value & (1 << i)) value ^= generator << (i - top);
    }
    return (data << bits) | value;
}

// Every format and version information a code can hold, to read one by
// the nearest: three wrong bits of either are fixed.
const FORMATS = Array.from({ length: 32 }, (_, d) => bch(d, 10, 0x537) ^ 0x5412);
const VERSIONS = Array.from({ length: MAX_VERSION + 1 }, (_, v) => (v >= 7 ? bch(v, 12, 0x1f25) : -1));

function ones(x) {
    let n = 0;
    for (; x; x &= x - 1) n++;
    return n;
}

// nearest is the index of the entry in table nearest to any of the
// values read, within three bits, or -1.
function nearest(table, ...values) {
    let best = -1;
    let distance = 4;
    table.forEach((entry, i) => {
        if (entry < 0) return;
        for (const v of values) {
            const d = ones(entry ^ v);
            if (d < distance) [best, distance] = [i, d];
        }
    });
    return best;
}

// GF(256) by the polynomial QR codes use, with exp doubled so a product's
// logs need no reduction.
const EXP = new Uint8Array(512);
const LOG = new Uint8Array(256);
for (let i = 0, x = 1; i < 255; i++) {
    EXP[i] = x;
    LOG[x] = i;
    x <<= 1;
    if (x & 0x100) x ^= 0x11d;
}
for (let i = 255; i < 512; i++) EXP[i] = EXP[i - 255];

const mul = (a, b) => (a && b ? EXP[LOG[a] + LOG[b]] : 0);
const div = (a, b) => (a ? EXP[LOG[a] + 255 - LOG[b]] : 0);

// evaluate is a polynomial, its lowest term first, at x.
function evaluate(poly, x) {
    let y = 0;
    for (let i = poly.length - 1; i >= 0; i--) y = mul(y, x) ^ poly[i];
    return y;
}

// correct fixes a block, its first byte the highest term, in place, with
// its last ecc bytes the checks: Berlekamp and Massey find where the
// errors are, and Forney by how much. It says whether the block is whole.
export function correct(block, ecc) {
    const n = block.length;
    const syndromes = [];
    let clean = true;
    for (let i = 0; i < ecc; i++) {
        let s = 0;
        for (let j = 0; j < n; j++) s = mul(s, EXP[i]) ^ block[j];
        syndromes.push(s);
        clean &&= s === 0;
    }
    if (clean) return true;
    let locator = [1];
    let previous = [1];
    let length = 0;
    let shift = 1;
    let last = 1;
    for (let i = 0; i < ecc; i++) {
        let d = syndromes[i];
        for (let j = 1; j <= length; j++) d ^= mul(locator[j] || 0, syndromes[i - j]);
        if (d === 0) {
            shift++;
            continue;
        }
        const next = locator.slice();
        const scale = div(d, last);
        for (let j = 0; j < previous.length; j++) next[j + shift] = (next[j + shift] || 0) ^ mul(scale, previous[j]);
        if (2 * length <= i) {
            previous = locator;
            length = i + 1 - length;
            last = d;
            shift = 1;
        } else {
            shift++;
        }
        locator = next;
    }
    if (length > ecc / 2) return false;
    locator = Array.from({ length: length + 1 }, (_, i) => locator[i] || 0);
    // The evaluator: the syndromes times the locator, below x^ecc.
    const evaluator = new Array(ecc).fill(0);
    for (let i = 0; i < ecc; i++) {
        for (let j = 0; j <= i && j < locator.length; j++) evaluator[i] ^= mul(syndromes[i - j], locator[j]);
    }
    const derivative = locator.map((c, i) => (i % 2 === 1 ? c : 0)).slice(1);
    let found = 0;
    for (let degree = 0; degree < n; degree++) {
        const inverse = EXP[(255 - degree) % 255];
        if (evaluate(locator, inverse) !== 0) continue;
        const slope = evaluate(derivative, inverse);
        if (slope === 0) return false;
        block[n - 1 - degree] ^= mul(EXP[degree], div(evaluate(evaluator, inverse), slope));
        found++;
    }
    if (found !== length) return false;
    for (let i = 0; i < ecc; i++) {
        let s = 0;
        for (let j = 0; j < n; j++) s = mul(s, EXP[i]) ^ block[j];
        if (s !== 0) return false;
    }
    return true;
}

// MASKS say which modules a mask flips, by row and column.
const MASKS = [
    (i, j) => (i + j) % 2 === 0,
    (i) => i % 2 === 0,
    (i, j) => j % 3 === 0,
    (i, j) => (i + j) % 3 === 0,
    (i, j) => (Math.floor(i / 2) + Math.floor(j / 3)) % 2 === 0,
    (i, j) => ((i * j) % 2) + ((i * j) % 3) === 0,
    (i, j) => (((i * j) % 2) + ((i * j) % 3)) % 2 === 0,
    (i, j) => (((i + j) % 2) + ((i * j) % 3)) % 2 === 0,
];

// functionModules marks a version's modules that hold no data: the
// finders with their separators and the format beside them, the timing
// lines, the alignment patterns and the version information.
function functionModules(version) {
    const size = 17 + 4 * version;
    const marked = new Uint8Array(size * size);
    const region = (left, top, width, height) => {
        for (let y = top; y < top + height; y++) marked.fill(1, y * size + left, y * size + left + width);
    };
    region(0, 0, 9, 9);
    region(size - 8, 0, 8, 9);
    region(0, size - 8, 9, 8);
    const centers = ALIGN[version];
    const last = centers.length - 1;
    centers.forEach((x, i) => centers.forEach((y, j) => {
        if ((i === 0 && j === 0) || (i === 0 && j === last) || (i === last && j === 0)) return;
        region(x - 2, y - 2, 5, 5);
    }));
    region(6, 9, 1, size - 17);
    region(9, 6, size - 17, 1);
    if (version >= 7) {
        region(size - 11, 0, 3, 6);
        region(0, size - 11, 6, 3);
    }
    return marked;
}

// Bits reads a byte string a few bits at a time.
class Bits {
    constructor(bytes) {
        this.bytes = bytes;
        this.at = 0;
    }

    get left() {
        return this.bytes.length * 8 - this.at;
    }

    read(n) {
        if (n > this.left) throw new Error('past the end');
        let v = 0;
        for (let i = 0; i < n; i++, this.at++) v = (v << 1) | ((this.bytes[this.at >> 3] >> (7 - (this.at & 7))) & 1);
        return v;
    }
}

const ALPHANUMERIC = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:';

// segments reads a code's data as text: numeric, alphanumeric and byte
// segments, the bytes UTF-8 together. Any other kind is refused, since
// Dens makes none.
function segments(data, version) {
    const bits = new Bits(data);
    const wide = version >= 10 ? 1 : 0;
    const out = [];
    while (bits.left >= 4) {
        const mode = bits.read(4);
        if (mode === 0) break;
        if (mode === 1) {
            let count = bits.read(wide ? 12 : 10);
            for (; count >= 3; count -= 3) {
                const v = bits.read(10);
                if (v > 999) return null;
                out.push(...String(v).padStart(3, '0').split('').map((c) => c.charCodeAt(0)));
            }
            if (count > 0) {
                const v = bits.read(count === 2 ? 7 : 4);
                if (v >= 10 ** count) return null;
                out.push(...String(v).padStart(count, '0').split('').map((c) => c.charCodeAt(0)));
            }
        } else if (mode === 2) {
            let count = bits.read(wide ? 11 : 9);
            for (; count >= 2; count -= 2) {
                const v = bits.read(11);
                if (v >= 45 * 45) return null;
                out.push(ALPHANUMERIC.charCodeAt(Math.floor(v / 45)), ALPHANUMERIC.charCodeAt(v % 45));
            }
            if (count === 1) {
                const v = bits.read(6);
                if (v >= 45) return null;
                out.push(ALPHANUMERIC.charCodeAt(v));
            }
        } else if (mode === 4) {
            const count = bits.read(wide ? 16 : 8);
            if (count * 8 > bits.left) return null;
            for (let i = 0; i < count; i++) out.push(bits.read(8));
        } else {
            return null;
        }
    }
    try {
        return new TextDecoder('utf-8', { fatal: true }).decode(new Uint8Array(out));
    } catch {
        return null;
    }
}

// decodeMatrix reads a code from its modules, dark(x, y) for each, size
// a side: its format and version, its data unmasked and corrected, then
// its text, or null.
export function decodeMatrix(dark, size) {
    const version = (size - 17) / 4;
    if (!Number.isInteger(version) || version < 1 || version > MAX_VERSION) return null;
    const bit = (x, y) => (dark(x, y) ? 1 : 0);
    let first = 0;
    let second = 0;
    for (let i = 0; i <= 5; i++) first = (first << 1) | bit(i, 8);
    first = (((((first << 1) | bit(7, 8)) << 1) | bit(8, 8)) << 1) | bit(8, 7);
    for (let j = 5; j >= 0; j--) first = (first << 1) | bit(8, j);
    for (let j = size - 1; j >= size - 7; j--) second = (second << 1) | bit(8, j);
    for (let i = size - 8; i < size; i++) second = (second << 1) | bit(i, 8);
    const format = nearest(FORMATS, first, second);
    if (format < 0) return null;
    const level = LEVELS[format >> 3];
    const mask = MASKS[format & 7];
    if (version >= 7) {
        let a = 0;
        let b = 0;
        for (let j = 5; j >= 0; j--) for (let i = size - 9; i >= size - 11; i--) a = (a << 1) | bit(i, j);
        for (let i = 5; i >= 0; i--) for (let j = size - 9; j >= size - 11; j--) b = (b << 1) | bit(i, j);
        const read = nearest(VERSIONS, a, b);
        if (read >= 0 && read !== version) return null;
    }
    const marked = functionModules(version);
    const [ecc, ...groups] = BLOCKS[version][level];
    const total = groups.reduce((n, [count, data]) => n + count * (data + ecc), 0);
    const raw = new Uint8Array(total);
    let n = 0;
    let byte = 0;
    let bits = 0;
    let up = true;
    for (let j = size - 1; j > 0; j -= 2) {
        if (j === 6) j--;
        for (let count = 0; count < size; count++) {
            const y = up ? size - 1 - count : count;
            for (let col = 0; col < 2; col++) {
                const x = j - col;
                if (marked[y * size + x]) continue;
                byte = (byte << 1) | (bit(x, y) ^ (mask(y, x) ? 1 : 0));
                if (++bits === 8) {
                    if (n < total) raw[n] = byte;
                    n++;
                    byte = 0;
                    bits = 0;
                }
            }
        }
        up = !up;
    }
    if (n !== total) return null;
    // The blocks' bytes are interleaved: each one's first, then each one's
    // second, the longer blocks' last data byte after the shorter ones'.
    const blocks = [];
    for (const [count, data] of groups) for (let i = 0; i < count; i++) blocks.push({ data, bytes: new Uint8Array(data + ecc) });
    const shortest = blocks[0].data;
    let at = 0;
    for (let i = 0; i < shortest; i++) for (const b of blocks) b.bytes[i] = raw[at++];
    for (const b of blocks) if (b.data > shortest) b.bytes[shortest] = raw[at++];
    for (let i = 0; i < ecc; i++) for (const b of blocks) b.bytes[b.data + i] = raw[at++];
    const data = [];
    for (const b of blocks) {
        if (!correct(b.bytes, ecc)) return null;
        data.push(...b.bytes.subarray(0, b.data));
    }
    try {
        return segments(new Uint8Array(data), version);
    } catch {
        return null;
    }
}

// luminance is a frame's RGBA pixels as gray.
function luminance(rgba, width, height) {
    const out = new Uint8Array(width * height);
    for (let i = 0, j = 0; i < out.length; i++, j += 4) out[i] = (rgba[j] * 77 + rgba[j + 1] * 150 + rgba[j + 2] * 29) >> 8;
    return out;
}

// binarize marks a frame's dark pixels against the light around them, in
// blocks of 8 pixels each compared with the 5 by 5 blocks about it, which
// holds under a screen's glare and the shadow across it. A block with
// little contrast takes its neighbors' measure, as a code's quiet zone
// does.
function binarize(gray, width, height) {
    const B = 8;
    const bw = Math.ceil(width / B);
    const bh = Math.ceil(height / B);
    const points = new Float32Array(bw * bh);
    for (let by = 0; by < bh; by++) {
        for (let bx = 0; bx < bw; bx++) {
            let sum = 0;
            let count = 0;
            let low = 255;
            let high = 0;
            for (let y = by * B; y < Math.min(height, by * B + B); y++) {
                for (let x = bx * B; x < Math.min(width, bx * B + B); x++) {
                    const v = gray[y * width + x];
                    sum += v;
                    count++;
                    if (v < low) low = v;
                    if (v > high) high = v;
                }
            }
            let point = sum / count;
            if (high - low <= 24) {
                point = low / 2;
                if (bx > 0 && by > 0) {
                    const around = (points[(by - 1) * bw + bx] + 2 * points[by * bw + bx - 1] + points[(by - 1) * bw + bx - 1]) / 4;
                    if (low < around) point = around;
                }
            }
            points[by * bw + bx] = point;
        }
    }
    const dark = new Uint8Array(width * height);
    for (let by = 0; by < bh; by++) {
        for (let bx = 0; bx < bw; bx++) {
            let sum = 0;
            let count = 0;
            for (let y = Math.max(0, by - 2); y <= Math.min(bh - 1, by + 2); y++) {
                for (let x = Math.max(0, bx - 2); x <= Math.min(bw - 1, bx + 2); x++) {
                    sum += points[y * bw + x];
                    count++;
                }
            }
            const threshold = sum / count;
            for (let y = by * B; y < Math.min(height, by * B + B); y++) {
                for (let x = bx * B; x < Math.min(width, bx * B + B); x++) dark[y * width + x] = gray[y * width + x] <= threshold ? 1 : 0;
            }
        }
    }
    return dark;
}

// crossed says whether five runs read 1:1:3:1:1, a finder across its
// middle, within half a module each.
function crossed(runs) {
    const total = runs[0] + runs[1] + runs[2] + runs[3] + runs[4];
    if (total < 7) return false;
    const module = total / 7;
    const slack = module / 2;
    return Math.abs(runs[0] - module) < slack && Math.abs(runs[1] - module) < slack && Math.abs(runs[2] - 3 * module) < 3 * slack &&
        Math.abs(runs[3] - module) < slack && Math.abs(runs[4] - module) < slack;
}

// Finders look for the code's three corners: rows across the frame, each
// run of 1:1:3:1:1 checked down and across its middle, and those found
// again near one another taken together.
class Finders {
    constructor(dark, width, height) {
        this.dark = dark;
        this.width = width;
        this.height = height;
        this.found = [];
    }

    at(x, y) {
        return this.dark[y * this.width + x] === 1;
    }

    // line counts the five runs through x, y along dx, dy, out from the
    // middle run each way, none past most, and returns the middle of the
    // middle run along the line and the five runs, or null.
    line(x, y, dx, dy, most) {
        const runs = [0, 0, 0, 0, 0];
        const inside = (i) => {
            const px = x + dx * i;
            const py = y + dy * i;
            return px >= 0 && py >= 0 && px < this.width && py < this.height;
        };
        const dark = (i) => this.at(x + dx * i, y + dy * i);
        let i = 0;
        while (inside(i) && dark(i)) {
            runs[2]++;
            i--;
        }
        if (!inside(i)) return null;
        while (inside(i) && !dark(i) && runs[1] <= most) {
            runs[1]++;
            i--;
        }
        if (!inside(i) || runs[1] > most) return null;
        while (inside(i) && dark(i) && runs[0] <= most) {
            runs[0]++;
            i--;
        }
        if (runs[0] > most) return null;
        i = 1;
        while (inside(i) && dark(i)) {
            runs[2]++;
            i++;
        }
        if (!inside(i)) return null;
        while (inside(i) && !dark(i) && runs[3] <= most) {
            runs[3]++;
            i++;
        }
        if (!inside(i) || runs[3] > most) return null;
        while (inside(i) && dark(i) && runs[4] <= most) {
            runs[4]++;
            i++;
        }
        if (runs[4] > most) return null;
        if (!crossed(runs)) return null;
        return { center: i - runs[4] - runs[3] - runs[2] / 2, runs };
    }

    // check takes a row's 1:1:3:1:1 ending at x, and keeps the finder it
    // is if the runs down and across its middle agree.
    check(runs, x, y) {
        const total = runs.reduce((a, b) => a + b, 0);
        const cx = Math.floor(x - runs[4] - runs[3] - runs[2] / 2);
        const down = this.line(cx, y, 0, 1, runs[2]);
        if (!down || 5 * Math.abs(down.runs.reduce((a, b) => a + b, 0) - total) >= 2 * total) return;
        const cy = Math.floor(y + down.center);
        const across = this.line(cx, cy, 1, 0, runs[2]);
        if (!across) return;
        const downTotal = down.runs.reduce((a, b) => a + b, 0);
        const acrossTotal = across.runs.reduce((a, b) => a + b, 0);
        if (5 * Math.abs(acrossTotal - total) >= 2 * total) return;
        const fx = cx + across.center;
        const fy = cy;
        const module = (downTotal + acrossTotal) / 14;
        for (const f of this.found) {
            if (Math.abs(f.x - fx) <= module && Math.abs(f.y - fy) <= module && Math.abs(f.module - module) <= Math.max(1, f.module)) {
                const n = f.count + 1;
                f.x = (f.x * f.count + fx) / n;
                f.y = (f.y * f.count + fy) / n;
                f.module = (f.module * f.count + module) / n;
                f.count = n;
                return;
            }
        }
        if (this.found.length < 64) this.found.push({ x: fx, y: fy, module, count: 1 });
    }

    scan() {
        const step = Math.max(2, Math.floor((3 * this.height) / (4 * (17 + 4 * MAX_VERSION + 8))));
        for (let y = step - 1; y < this.height; y += step) {
            const runs = [0, 0, 0, 0, 0];
            let state = 0;
            for (let x = 0; x < this.width; x++) {
                const dark = this.at(x, y);
                if (dark === (state % 2 === 0)) {
                    runs[state]++;
                    continue;
                }
                if (state === 4) {
                    if (crossed(runs)) this.check(runs, x, y);
                    runs[0] = runs[2];
                    runs[1] = runs[3];
                    runs[2] = runs[4];
                    runs[3] = 1;
                    runs[4] = 0;
                    state = 3;
                } else if (state === 0 && !dark && runs[0] === 0) {
                    continue;
                } else {
                    runs[++state]++;
                }
            }
            if (state === 4 && crossed(runs)) this.check(runs, this.width, y);
        }
        return this.found;
    }
}

const distance = (a, b) => Math.hypot(a.x - b.x, a.y - b.y);

// corners picks the likeliest threes among the finders found, best first:
// alike in size, and at the corners of a right angle with equal legs. In
// each, the first is the top left, then the top right, then the bottom
// left, as the code reads.
function corners(found) {
    let pool = found.filter((f) => f.count >= 2);
    if (pool.length < 3) pool = found;
    pool = pool.slice().sort((a, b) => b.count - a.count).slice(0, 12);
    const out = [];
    for (let i = 0; i < pool.length; i++) {
        for (let j = i + 1; j < pool.length; j++) {
            for (let k = j + 1; k < pool.length; k++) {
                const [a, b, c] = [pool[i], pool[j], pool[k]];
                const sizes = [a.module, b.module, c.module];
                const big = Math.max(...sizes);
                if (Math.min(...sizes) < big / 2) continue;
                // The top left is opposite the longest side.
                const sides = [[distance(b, c), a, b, c], [distance(a, c), b, a, c], [distance(a, b), c, a, b]].sort((x, y) => y[0] - x[0]);
                const [hyp, corner, p, q] = sides[0];
                const legP = distance(corner, p);
                const legQ = distance(corner, q);
                if (legP < 7 * big || legQ < 7 * big) continue;
                const legs = Math.abs(legP - legQ) / Math.max(legP, legQ);
                const right = Math.abs(hyp - Math.hypot(legP, legQ)) / hyp;
                const alike = (big - Math.min(...sizes)) / big;
                const score = legs + right + alike / 2;
                if (legs > 0.3 || right > 0.15) continue;
                // Clockwise, in the frame's coordinates, from the top
                // right to the bottom left.
                const turn = (p.x - corner.x) * (q.y - corner.y) - (p.y - corner.y) * (q.x - corner.x);
                out.push({ score, tl: corner, tr: turn > 0 ? p : q, bl: turn > 0 ? q : p });
            }
        }
    }
    return out.sort((x, y) => x.score - y.score).slice(0, 4);
}

// perspective is the projective map that takes four points to four
// others, each given as [x0, y0, ..., x3, y3] going round a square from
// its top left, as a function of x and y.
export function perspective(from, to) {
    const square = (q) => {
        const [x0, y0, x1, y1, x2, y2, x3, y3] = q;
        const dx3 = x0 - x1 + x2 - x3;
        const dy3 = y0 - y1 + y2 - y3;
        if (dx3 === 0 && dy3 === 0) return [x1 - x0, y1 - y0, 0, x2 - x1, y2 - y1, 0, x0, y0, 1];
        const dx1 = x1 - x2;
        const dx2 = x3 - x2;
        const dy1 = y1 - y2;
        const dy2 = y3 - y2;
        const d = dx1 * dy2 - dx2 * dy1;
        const a13 = (dx3 * dy2 - dx2 * dy3) / d;
        const a23 = (dx1 * dy3 - dx3 * dy1) / d;
        return [x1 - x0 + a13 * x1, y1 - y0 + a13 * y1, a13, x3 - x0 + a23 * x3, y3 - y0 + a23 * y3, a23, x0, y0, 1];
    };
    // A point maps as the row x, y, 1 times a matrix, here row by row:
    // from the source to the square by the adjugate of the square's map to
    // it, then from the square to the destination.
    const [a, b, c, d, e, f, g, h, i] = square(from);
    const inv = [e * i - f * h, c * h - b * i, b * f - c * e, f * g - d * i, a * i - c * g, c * d - a * f, d * h - e * g, b * g - a * h, a * e - b * d];
    const out = square(to);
    const m = [];
    for (let r = 0; r < 3; r++) {
        for (let k = 0; k < 3; k++) m.push(inv[r * 3] * out[k] + inv[r * 3 + 1] * out[3 + k] + inv[r * 3 + 2] * out[6 + k]);
    }
    return (x, y) => {
        const w = m[2] * x + m[5] * y + m[8];
        return [(m[0] * x + m[3] * y + m[6]) / w, (m[1] * x + m[4] * y + m[7]) / w];
    };
}

// alignment looks for the bottom right alignment pattern near where the
// finders put it: a dark module in a light ring, about a module each,
// across and down; those found, nearest first. A tilted code puts it
// farther off, where one of the others can be nearer.
function alignment(finders, ex, ey, module, reach) {
    const { width, height } = finders;
    const left = Math.max(0, Math.floor(ex - reach));
    const right = Math.min(width - 1, Math.ceil(ex + reach));
    const top = Math.max(0, Math.floor(ey - reach));
    const bottom = Math.min(height - 1, Math.ceil(ey + reach));
    const fits = (runs) => runs.every((r) => Math.abs(r - module) < module / 2);
    const found = [];
    // runsAt counts the light, dark and light runs through x, y along dx,
    // dy, none past twice a module, and the middle of the dark one.
    const runsAt = (x, y, dx, dy) => {
        if (!finders.at(x, y)) return null;
        let i = 0;
        const runs = [0, 0, 0];
        const ok = (k) => x + dx * k >= 0 && y + dy * k >= 0 && x + dx * k < width && y + dy * k < height;
        while (ok(i) && finders.at(x + dx * i, y + dy * i) && runs[1] <= 2 * module) {
            runs[1]++;
            i--;
        }
        while (ok(i) && !finders.at(x + dx * i, y + dy * i) && runs[0] <= 2 * module) {
            runs[0]++;
            i--;
        }
        if (!ok(i)) return null;
        i = 1;
        while (ok(i) && finders.at(x + dx * i, y + dy * i) && runs[1] <= 2 * module) {
            runs[1]++;
            i++;
        }
        const end = i;
        while (ok(i) && !finders.at(x + dx * i, y + dy * i) && runs[2] <= 2 * module) {
            runs[2]++;
            i++;
        }
        if (!ok(i) || !fits(runs)) return null;
        return end - runs[1] / 2;
    };
    // A third of a module apart still meets the pattern's middle module,
    // and keeps a stranger's large finders from making the search long.
    const step = Math.max(1, Math.floor(module / 3));
    for (let y = top; y <= bottom; y += step) {
        for (let x = left; x <= right; x += step) {
            const ax = runsAt(x, y, 1, 0);
            if (ax === null) continue;
            const cx = Math.round(x + ax - 0.5);
            const ay = runsAt(cx, y, 0, 1);
            if (ay === null) continue;
            const point = { x: x + ax, y: y + ay };
            if (!found.some((f) => Math.hypot(f.x - point.x, f.y - point.y) < module)) {
                found.push({ ...point, d: Math.hypot(point.x - ex, point.y - ey) });
                if (found.length >= 32) break;
            }
            x += Math.max(1, Math.floor(module));
        }
    }
    return found.sort((a, b) => a.d - b.d);
}

// sample reads a code of size modules from the frame, through the map
// from the code's corners to where they were found: each module by the
// most of the pixels about its middle, where a module is three pixels or
// more, so one pixel's noise doesn't flip it.
function sample(finders, size, tl, tr, bl, br, inset, module) {
    const far = size - 3.5;
    const map = perspective([3.5, 3.5, far, 3.5, far - inset, far - inset, 3.5, far], [tl.x, tl.y, tr.x, tr.y, br.x, br.y, bl.x, bl.y]);
    const { width, height, dark } = finders;
    const reach = module >= 3 ? 1 : 0;
    const bits = new Uint8Array(size * size);
    for (let y = 0; y < size; y++) {
        for (let x = 0; x < size; x++) {
            const [px, py] = map(x + 0.5, y + 0.5);
            const ix = Math.floor(px);
            const iy = Math.floor(py);
            if (!(ix >= reach && iy >= reach && ix < width - reach && iy < height - reach)) return null;
            let votes = 0;
            for (let dy = -reach; dy <= reach; dy++) for (let dx = -reach; dx <= reach; dx++) votes += dark[(iy + dy) * width + ix + dx];
            bits[y * size + x] = 2 * votes > (2 * reach + 1) ** 2 ? 1 : 0;
        }
    }
    return bits;
}

// runTo measures from the middle of a finder toward a point: the dark
// middle, the light ring and the dark ring, three and a half modules
// along any line, or NaN where it doesn't find them.
function runTo(finders, fromX, fromY, toX, toY) {
    const steep = Math.abs(toY - fromY) > Math.abs(toX - fromX);
    if (steep) [fromX, fromY, toX, toY] = [fromY, fromX, toY, toX];
    const dx = Math.abs(toX - fromX);
    const dy = Math.abs(toY - fromY);
    const xstep = fromX < toX ? 1 : -1;
    const ystep = fromY < toY ? 1 : -1;
    let error = -dx / 2;
    let state = 0;
    for (let x = fromX, y = fromY; x !== toX + xstep; x += xstep) {
        const [px, py] = steep ? [y, x] : [x, y];
        if (px < 0 || py < 0 || px >= finders.width || py >= finders.height) break;
        if ((state === 1) === finders.at(px, py)) {
            if (state === 2) return Math.hypot(x - fromX, y - fromY);
            state++;
        }
        error += dy;
        if (error > 0) {
            if (y === toY) break;
            y += ystep;
            error -= dx;
        }
    }
    return NaN;
}

// runBoth is runTo both ways along the line through a finder's middle:
// seven modules.
function runBoth(finders, a, b) {
    const ax = Math.floor(a.x);
    const ay = Math.floor(a.y);
    const bx = Math.floor(b.x);
    const by = Math.floor(b.y);
    return runTo(finders, ax, ay, bx, by) + runTo(finders, ax, ay, 2 * ax - bx, 2 * ay - by) - 1;
}

// moduleBetween is a module's size along the line between two finders,
// measured at each, as a turned or tilted code has it.
function moduleBetween(finders, a, b) {
    const one = runBoth(finders, a, b);
    const two = runBoth(finders, b, a);
    if (Number.isNaN(one)) return two / 7;
    if (Number.isNaN(two)) return one / 7;
    return (one + two) / 14;
}

// read tries a three of finders as a code's corners: each size its spacing
// allows, the alignment pattern found or, if not, where it should be.
function read(finders, { tl, tr, bl }) {
    let module = (moduleBetween(finders, tl, tr) + moduleBetween(finders, tl, bl)) / 2;
    if (!(module > 0)) module = (tl.module + tr.module + bl.module) / 3;
    const across = Math.round(distance(tl, tr) / module);
    const down = Math.round(distance(tl, bl) / module);
    const near = Math.round((across + down) / 2) + 7;
    const sizes = [];
    for (const size of [near, near - 1, near + 1, near - 2, near + 2, near - 3, near + 3]) {
        if ((size - 17) % 4 === 0 && size >= 21 && size <= 17 + 4 * MAX_VERSION && !sizes.includes(size)) sizes.push(size);
    }
    for (const size of sizes) {
        // A code larger than the frame twice over is no code in it.
        if (size * module > 2 * Math.max(finders.width, finders.height)) continue;
        const corner = { x: tr.x - tl.x + bl.x, y: tr.y - tl.y + bl.y };
        const tries = [];
        if (size > 21) {
            const toward = 1 - 3 / (size - 7);
            const ex = tl.x + toward * (corner.x - tl.x);
            const ey = tl.y + toward * (corner.y - tl.y);
            for (const found of alignment(finders, ex, ey, module, 12 * module).slice(0, 4)) tries.push([found, 3]);
            tries.push([{ x: ex, y: ey }, 3]);
        }
        tries.push([corner, 0]);
        for (const [br, inset] of tries) {
            const bits = sample(finders, size, tl, tr, bl, br, inset, module);
            if (!bits) continue;
            const text = decodeMatrix((x, y) => bits[y * size + x] === 1, size);
            if (text !== null) return text;
        }
    }
    return null;
}

// readQR reads a QR code in a frame, { data, width, height } with data
// its RGBA pixels as a canvas gives them: its text, or null for none.
export function readQR({ data, width, height }) {
    if (!(width >= 21 && height >= 21) || data.length < width * height * 4) return null;
    const finders = new Finders(binarize(luminance(data, width, height), width, height), width, height);
    for (const three of corners(finders.scan())) {
        const text = read(finders, three);
        if (text !== null) return text;
    }
    return null;
}
