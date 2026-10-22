// What the page says of what differs in the Flatpak (M9.3): the update
// notice, which follows Flatpak's update monitor there, and where the key
// it remembers is kept.

import { api } from './api.js';

// updateNotice is home's notice of an update, or null: the release host's
// for an installed Dens, with the command that installs it, or Flatpak's,
// with what the member can do about it: restart into an update something
// installed, update from Dens where Flatpak lets it, or update in the
// software center.
export function updateNotice(status) {
    if (status.updateVersion) {
        return { text: `Dens ${status.updateVersion} is available. To update, run:`, command: status.updateCommand };
    }
    const u = status.flatpakUpdate;
    if (!u) return null;
    if (u.installed) return { text: 'An update to Dens is installed.', action: 'restart', label: 'Restart Dens' };
    if (u.can_install) return { text: 'An update to Dens is available.', action: 'update', label: 'Update' };
    const why = /permission/i.test(u.refused || '') ? ', since it asks for a new permission' : '';
    return { text: `An update to Dens is available. Update it in your software center${why}.` };
}

// rememberNote is the line under Remember on this computer: where the key
// is kept, why in a file where the keyring couldn't take it, or that the
// keyring is asked.
export function rememberNote(r) {
    if (r.asking) return { text: 'Asking your keyring…' };
    if (!r.on) return { text: 'Dens asks for your local password each time it starts.' };
    if (r.where === 'keyring') return { text: 'Your keyring keeps it, and opens Dens when you sign in.' };
    return {
        text: "It's kept in a file in Dens's own folder, which keeps it from other apps, though not from programs you run.",
        problem: r.problem || '',
    };
}

// restartDens asks the service to restart, or to update and restart, at
// path, then waits for it to go and for its successor to answer, and loads
// the page again from it, which may start locked.
export async function restartDens(path) {
    await api.post(path);
    const answers = async () => {
        try {
            await api.get('/api/status');
            return true;
        } catch (e) {
            return e.status === 401 || e.status === 423;
        }
    };
    const pause = () => new Promise((resolve) => setTimeout(resolve, 500));
    for (let i = 0; i < 20 && (await answers()); i++) await pause();
    for (let i = 0; i < 120 && !(await answers()); i++) await pause();
    window.location.reload();
}
