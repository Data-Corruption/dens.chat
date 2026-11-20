// Opening a den's Manage dialog from anywhere on the page: home's
// rows, and the alerts on a den's page that point at what's in it; and the
// settings, which hold a den's sections of the member's own. The
// app shows the dialogs; this keeps their openers out of the modules the
// dialogs themselves use.

const openers = new Set();
const settingsOpeners = new Set();

// openSettings opens the settings at a section, "appearance", "chat",
// "notifications", "voice", "relay" or "system", from anywhere on the
// page; with none, at the first, or on the phone layout at the list of
// them. With denID, they hold that den's sections of the member's own
// too, "devices", "password", "code", "files" and "leave", as they do on
// its page.
export function openSettings(section = '', denID = '') {
    settingsOpeners.forEach((fn) => fn({ section, denID }));
}

export function onOpenSettings(fn) {
    settingsOpeners.add(fn);
    return () => settingsOpeners.delete(fn);
}

// openManage opens a den's Manage dialog at a section, or at its first.
export function openManage(denID, section = '') {
    openers.forEach((fn) => fn({ denID, section }));
}

export function onOpenManage(fn) {
    openers.add(fn);
    return () => openers.delete(fn);
}
