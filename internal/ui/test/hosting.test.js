// What the settings say of hosting (M9.1).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { hostingAsk, hostingNote } from '../assets/js/src/hosting.js';

const ports = { den_port: 8485, media_udp_port: 7881, media_tcp_port: 7882 };

test('turning hosting on takes the password, turning it off a confirmation, and leaving it asks nothing', () => {
    assert.equal(hostingAsk(false, true), 'on');
    assert.equal(hostingAsk(true, false), 'off');
    assert.equal(hostingAsk(false, false), '');
    assert.equal(hostingAsk(true, true), '');
});

test("the switch says nothing while off, where the den is sent while on, and why it isn't when it can't start", () => {
    assert.equal(hostingNote({ enabled: false, serving: false, ...ports }), null);
    assert.deepEqual(hostingNote({ enabled: true, serving: true, ...ports }), { address: '127.0.0.1:8485', udp: 7881, tcp: 7882 });
    const held = "Hosting can't listen on port 7881 (UDP): another program holds it, or the system keeps it.";
    assert.deepEqual(hostingNote({ enabled: true, serving: false, problem: held, ...ports }), { problem: held });
    assert.match(hostingNote({ enabled: true, serving: false, ...ports }).problem, /hasn't started/);
});

test('a form that hosts no den, as the Flatpak, says what hosting needs, whatever else it says', () => {
    assert.match(hostingNote({ can_host: false, enabled: false, serving: false, ...ports }).cantHost, /installed on Linux or Windows/);
    assert.equal(hostingNote({ can_host: true, enabled: false, serving: false, ...ports }), null);
});
