// What the settings say of hosting (M9.1).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { hostingAsk, hostingNote, hostingOnWarning, moving, ownRowNote, setupNeeded } from '../assets/js/src/hosting.js';

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

// A den this computer holds, in its settings (M10).
const den = { can_host: true, den: 'Home Den', url: 'https://den.example.com', ...ports };
const exported = Date.UTC(2026, 9, 23, 15, 4);

test('a den moves to another computer from its settings, and once exported says when, with its removal', () => {
    assert.equal(moving({ can_host: true, enabled: false, ...ports }), null);
    assert.equal(moving({ ...den, can_host: false }), null);
    assert.deepEqual(moving({ ...den, enabled: true }), { den: 'Home Den' });
    const after = moving({ ...den, enabled: false, exported }, 'en-US');
    assert.equal(after.den, 'Home Den');
    assert.equal(after.url, 'https://den.example.com');
    assert.match(after.exported, /Oct 23, 2026/);
});

test('turning hosting on after an export says another computer may host the den by now', () => {
    assert.equal(hostingOnWarning({ ...den, enabled: false }), null);
    const warning = hostingOnWarning({ ...den, enabled: false, exported }, 'en-US');
    assert.match(warning, /^You exported Home Den on Oct 23, 2026/);
    assert.match(warning, /Another computer may host it by now/);
    assert.match(warning, /whichever one its address points at/);
});

test('home offers to set the den up while hosting is on and it has no owner, which a den moved here has', () => {
    assert.equal(setupNeeded({ enabled: true, created: false, owned: false }), true);
    assert.equal(setupNeeded({ enabled: true, created: true, owned: false }), true);
    assert.equal(setupNeeded({ enabled: true, created: true, owned: true }), false);
    assert.equal(setupNeeded({ enabled: false, created: true, owned: false }), false);
});

test("the den's row says hosting is off, leading to the switch, and once exported, leads to Hosting", () => {
    const own = { own: true, state: 'offline' };
    assert.equal(ownRowNote({ own: false, state: 'connected' }, { enabled: false }), null);
    assert.equal(ownRowNote(own, { enabled: true }), null);
    assert.deepEqual(ownRowNote(own, { enabled: false }),
        { label: 'Hosting is off', text: "Members can't reach it until hosting is back on.", action: 'Turn hosting on' });
    const note = ownRowNote(own, { enabled: false, exported }, 'en-US');
    assert.equal(note.label, 'Exported');
    assert.match(note.text, /^You exported it on Oct 23, 2026.*Once another computer hosts it, remove it from this one\.$/);
    assert.equal(note.action, 'Open Hosting');
});
