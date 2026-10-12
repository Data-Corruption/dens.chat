// Opening a den's Manage dialog from anywhere on the page (M8.3): home's
// rows, and the alerts on a den's page that point at what's in it. The
// app shows the dialog; this keeps its openers out of the modules the
// dialog itself uses.

const openers = new Set();

// openManage opens a den's Manage dialog at a section, or at its first.
export function openManage(denID, section = '') {
    openers.forEach((fn) => fn({ denID, section }));
}

export function onOpenManage(fn) {
    openers.add(fn);
    return () => openers.delete(fn);
}
