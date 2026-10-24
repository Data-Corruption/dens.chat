// Stands in for Caddy and the router's forward in the move check (M10):
// the den's address, which reaches the den listener on the port FILE
// names, read again for each connection, so moving the forward to another
// computer's den is writing its port there.
//
//   ./node browser/forward.mjs LISTEN_PORT FILE

import fs from 'node:fs';
import net from 'node:net';

const [, , listen, file] = process.argv;

net.createServer((client) => {
    const den = net.connect(Number(fs.readFileSync(file, 'utf8').trim()), '127.0.0.1');
    const close = () => {
        client.destroy();
        den.destroy();
    };
    for (const end of [client, den]) {
        end.on('error', close);
        end.on('close', close);
    }
    client.pipe(den).pipe(client);
}).listen(Number(listen), '127.0.0.1');
