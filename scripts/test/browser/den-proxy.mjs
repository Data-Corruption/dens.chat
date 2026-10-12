// Stands between the members' clients and the den, as Caddy would, and
// writes the SDP of each call answer a client sends the den to a log, one
// JSON line each: what the den learns of a member's computer from their
// answers (M8). The answers check reads the log.
//
//   ./node browser/den-proxy.mjs LISTEN_PORT DEN_PORT LOG
//
// A client's bytes are HTTP requests, one after another on a connection,
// until one upgrades it to the den's WebSocket, whose frames it masks and
// may compress with permessage-deflate, without context takeover, as the
// den asks.

import fs from 'node:fs';
import net from 'node:net';
import zlib from 'node:zlib';

const [, , listen, den, log] = process.argv;

// reader reads a client's side of a connection: requests, then frames.
function reader(onMessage) {
    let buf = Buffer.alloc(0);
    let ws = false;
    let parts = [];
    let compressed = false;
    const http = () => {
        const end = buf.indexOf('\r\n\r\n');
        if (end < 0) return false;
        const head = buf.subarray(0, end).toString('latin1');
        const header = (name) => head.match(new RegExp(`^${name}:\\s*(.*)$`, 'im'))?.[1].trim();
        let at = end + 4;
        if (/^upgrade:\s*websocket/im.test(head)) {
            buf = buf.subarray(at);
            ws = true;
            return true;
        }
        if (/chunked/i.test(header('transfer-encoding') || '')) {
            for (;;) {
                const line = buf.indexOf('\r\n', at);
                if (line < 0) return false;
                const size = parseInt(buf.subarray(at, line).toString('latin1'), 16);
                if (buf.length < line + 2 + size + 2) return false;
                at = line + 2 + size + 2;
                if (size === 0) break;
            }
        } else {
            const length = Number(header('content-length') || 0);
            if (buf.length < at + length) return false;
            at += length;
        }
        buf = buf.subarray(at);
        return true;
    };
    const frame = () => {
        if (buf.length < 2) return false;
        const fin = buf[0] & 0x80;
        const opcode = buf[0] & 0x0f;
        let length = buf[1] & 0x7f;
        let at = 2;
        if (length === 126) {
            if (buf.length < 4) return false;
            length = buf.readUInt16BE(2);
            at = 4;
        } else if (length === 127) {
            if (buf.length < 10) return false;
            length = Number(buf.readBigUInt64BE(2));
            at = 10;
        }
        const masked = buf[1] & 0x80;
        const key = masked ? buf.subarray(at, at + 4) : null;
        if (masked) at += 4;
        if (buf.length < at + length) return false;
        const payload = Buffer.from(buf.subarray(at, at + length));
        if (key) for (let i = 0; i < payload.length; i++) payload[i] ^= key[i % 4];
        buf = buf.subarray(at + length);
        // Control frames, as pings, carry nothing the check reads.
        if (opcode >= 8) return true;
        return { fin, payload };
    };
    return (chunk) => {
        buf = Buffer.concat([buf, chunk]);
        for (;;) {
            if (!ws) {
                if (!http()) return;
                continue;
            }
            if (buf.length < 2) return;
            const rsv1 = buf[0] & 0x40;
            const opcode = buf[0] & 0x0f;
            const f = frame();
            if (!f) return;
            if (f === true) continue;
            if (opcode !== 0) compressed = !!rsv1;
            parts.push(f.payload);
            if (!f.fin) continue;
            let message = Buffer.concat(parts);
            parts = [];
            try {
                // Each message ends at a sync flush, not the stream's end.
                if (compressed) {
                    message = zlib.inflateRawSync(Buffer.concat([message, Buffer.from([0, 0, 0xff, 0xff])]), { finishFlush: zlib.constants.Z_SYNC_FLUSH });
                }
                onMessage(message);
            } catch (e) {
                console.error(`a message that doesn't read: ${e.message}`);
            }
        }
    };
}

net.createServer((client) => {
    const upstream = net.connect(Number(den), '127.0.0.1');
    const read = reader((message) => {
        let events;
        try {
            events = JSON.parse(message.toString('utf8'));
        } catch {
            return;
        }
        for (const e of Array.isArray(events) ? events : []) {
            if (e.t === 'voice.answer' && typeof e.d?.sdp === 'string') fs.appendFileSync(log, JSON.stringify({ at: Date.now(), sdp: e.d.sdp }) + '\n');
        }
    });
    client.on('data', (chunk) => {
        upstream.write(chunk);
        read(chunk);
    });
    upstream.pipe(client);
    const end = () => {
        client.destroy();
        upstream.destroy();
    };
    for (const s of [client, upstream]) {
        s.on('error', end);
        s.on('close', end);
    }
}).listen(Number(listen), '127.0.0.1');
