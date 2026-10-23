// What the page says of what differs in the Flatpak (M9.3).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { rememberNote, updateNotice } from '../assets/js/src/flatpak.js';

test("an installed Dens's notice gives the update command, and no update gives none", () => {
    assert.equal(updateNotice({}), null);
    assert.deepEqual(updateNotice({ updateVersion: 'v1.2.0', updateCommand: 'sudo dens update' }),
        { text: 'Dens v1.2.0 is available. To update, run:', command: 'sudo dens update' });
});

test("the Flatpak's notice restarts into an installed update, updates where Flatpak lets Dens, and sends to the software center otherwise", () => {
    assert.equal(updateNotice({ flatpakUpdate: { available: false, installed: true } }).action, 'restart');
    // The version, where Dens asked the newest build installed (M9.4).
    assert.equal(updateNotice({ flatpakUpdate: { installed: true, version: 'v1.2.1' } }).text, 'Dens v1.2.1 is installed.');
    assert.equal(updateNotice({ flatpakUpdate: { installed: true } }).text, 'An update to Dens is installed.');
    assert.equal(updateNotice({ flatpakUpdate: { available: true, installed: true, can_install: true } }).action, 'restart');
    assert.equal(updateNotice({ flatpakUpdate: { available: true, can_install: true } }).action, 'update');
    const gnome = updateNotice({ flatpakUpdate: { available: true, can_install: false } });
    assert.equal(gnome.action, undefined);
    assert.match(gnome.text, /software center\.$/);
    const permission = updateNotice({ flatpakUpdate: { available: true, refused: 'Self update not supported, new version requires new permissions' } });
    assert.match(permission.text, /new permission\.$/);
});

test('Remember says where the key is kept, and why in a file', () => {
    assert.match(rememberNote({ on: false }).text, /each time it starts/);
    assert.match(rememberNote({ on: true, where: 'keyring' }).text, /keyring keeps it/);
    const file = rememberNote({ on: true, where: 'file', problem: 'Your desktop has no keyring Dens can reach.' });
    assert.match(file.text, /file/);
    assert.equal(file.problem, 'Your desktop has no keyring Dens can reach.');
    assert.match(rememberNote({ on: false, asking: true }).text, /Asking your keyring/);
});
