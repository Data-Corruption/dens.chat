// make-sound.mjs FILE SECONDS writes a sound for a den's soundboard (M14.3)
// as the page of staff adding one writes it, Opus in Ogg, here of Opus's
// silence, 20 ms a packet: for the checks that walk the soundboard, or count
// what reaches a call, rather than listen to it.
import fs from 'node:fs';

const [, , file, seconds = '1'] = process.argv;
const SILENCE = [0xf8, 0xff, 0xfe]; // 20 ms of CELT in mono, silent

const table = new Uint32Array(256).map((_, i) => {
    let r = i << 24;
    for (let k = 0; k < 8; k++) r = r & 0x80000000 ? (r << 1) ^ 0x04c11db7 : r << 1;
    return r >>> 0;
});
const crc = (b) => b.reduce((c, x) => ((c << 8) ^ table[((c >>> 24) ^ x) & 0xff]) >>> 0, 0);

function page(packet, flags, granule, number) {
    const out = Buffer.alloc(28 + packet.length);
    out.write('OggS', 0);
    out[5] = flags;
    out.writeUInt32LE(granule, 6);
    out.writeUInt32LE(0x736e6544, 14);
    out.writeUInt32LE(number, 18);
    out[26] = 1;
    out[27] = packet.length;
    Buffer.from(packet).copy(out, 28);
    out.writeUInt32LE(crc(out), 22);
    return out;
}

const head = Buffer.alloc(19);
head.write('OpusHead', 0);
head[8] = 1;
head[9] = 1;
head.writeUInt16LE(312, 10);
head.writeUInt32LE(48000, 12);
const tags = Buffer.concat([Buffer.from('OpusTags'), Buffer.from([4, 0, 0, 0]), Buffer.from('Dens'), Buffer.alloc(4)]);
const n = Math.round(Number(seconds) * 50);
const pages = [page(head, 0x02, 0, 0), page(tags, 0, 0, 1)];
for (let i = 0; i < n; i++) pages.push(page(SILENCE, i === n - 1 ? 0x04 : 0, (i + 1) * 960, i + 2));
fs.writeFileSync(file, Buffer.concat(pages));
