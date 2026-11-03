// Makes an image for the den's emoji (M15.2), as staff would add one: a
// PNG of a face, a yellow circle on a transparent ground, of any size,
// which the page draws at an emoji's; or, named .gif, an animated GIF of
// a dot crossing a square, over six frames, looping forever.
//
//   node make-emoji.mjs FILE WIDTH HEIGHT
import fs from 'node:fs';
import zlib from 'node:zlib';

const [, , file, width, height] = process.argv;
const w = Number(width);
const h = Number(height);
if (!file || !(w > 0) || !(h > 0)) {
    console.error('usage: node make-emoji.mjs FILE WIDTH HEIGHT');
    process.exit(2);
}

// face is the face's color at (x, y), as RGBA: the circle, two eyes and a
// mouth, and outside it nothing.
function face(x, y) {
    const r = Math.min(w, h) / 2 - 1;
    const dx = x + 0.5 - w / 2;
    const dy = y + 0.5 - h / 2;
    if (dx * dx + dy * dy > r * r) return [0, 0, 0, 0];
    const eye = (ex) => (dx - ex * r) ** 2 + (dy + 0.3 * r) ** 2 < (0.12 * r) ** 2;
    const mouth = dy > 0.25 * r && dy < 0.38 * r && Math.abs(dx) < 0.45 * r;
    return eye(-0.35) || eye(0.35) || mouth ? [60, 40, 20, 255] : [250, 200, 40, 255];
}

function png() {
    const chunk = (type, data) => {
        const b = Buffer.alloc(12 + data.length);
        b.writeUInt32BE(data.length, 0);
        b.write(type, 4, 'ascii');
        data.copy(b, 8);
        b.writeUInt32BE(zlib.crc32(b.subarray(4, 8 + data.length)), 8 + data.length);
        return b;
    };
    const head = Buffer.alloc(13);
    head.writeUInt32BE(w, 0);
    head.writeUInt32BE(h, 4);
    head[8] = 8; // bits per channel
    head[9] = 6; // RGBA
    const rows = Buffer.alloc((w * 4 + 1) * h);
    for (let y = 0; y < h; y++) {
        for (let x = 0; x < w; x++) Buffer.from(face(x, y)).copy(rows, y * (w * 4 + 1) + 1 + x * 4);
    }
    return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', head), chunk('IDAT', zlib.deflateSync(rows)), chunk('IEND', Buffer.alloc(0))]);
}

// lzw writes indexes as GIF's LZW in codes of nine bits, cleared often
// enough that its table never needs a tenth.
function lzw(indexes) {
    const out = [];
    let cur = 0;
    let n = 0;
    const put = (code) => {
        for (let i = 0; i < 9; i++) {
            cur |= ((code >> i) & 1) << n;
            if (++n === 8) {
                out.push(cur);
                cur = n = 0;
            }
        }
    };
    put(256);
    indexes.forEach((v, i) => {
        if (i > 0 && i % 200 === 0) put(256);
        put(v);
    });
    put(257);
    if (n) out.push(cur);
    return out;
}

function gif() {
    const b = [...Buffer.from('GIF89a'), w & 0xff, w >> 8, h & 0xff, h >> 8, 0xf7, 0, 0];
    const colors = [[90, 30, 150], [250, 200, 40]];
    for (let i = 0; i < 256; i++) b.push(...(colors[i] || [0, 0, 0]));
    b.push(0x21, 0xff, 11, ...Buffer.from('NETSCAPE2.0'), 3, 1, 0, 0, 0);
    const frames = 6;
    for (let f = 0; f < frames; f++) {
        const cx = ((f + 0.5) * w) / frames;
        const pix = [];
        for (let y = 0; y < h; y++) {
            for (let x = 0; x < w; x++) pix.push((x - cx) ** 2 + (y - h / 2) ** 2 < (Math.min(w, h) / 6) ** 2 ? 1 : 0);
        }
        b.push(0x21, 0xf9, 4, 1 << 2, 12, 0, 0, 0, 0x2c, 0, 0, 0, 0, w & 0xff, w >> 8, h & 0xff, h >> 8, 0, 8);
        const data = lzw(pix);
        for (let i = 0; i < data.length; i += 255) {
            const part = data.slice(i, i + 255);
            b.push(part.length, ...part);
        }
        b.push(0);
    }
    b.push(0x3b);
    return Buffer.from(b);
}

fs.writeFileSync(file, file.endsWith('.gif') ? gif() : png());
