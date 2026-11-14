// An update's notice, and what installs it: a command for an installed
// Dens; for the Flatpak, a button where Dens may install it or restart
// into it, or the software center; and in the Android app, a button that
// has Android's installer ask the member to confirm it. Home shows it, and
// so does a den, since a member with one den never opens Home; there the
// member may dismiss it, which this browser keeps until the next update.

import { useState } from 'preact/hooks';
import { api } from './api.js';
import { ErrorText, useAction } from './components.jsx';
import { noticeKey, restartDens, updateNotice } from './flatpak.js';

const DISMISSED = 'DENS_UPDATE_DISMISSED';

function dismissedKey() {
    try {
        return localStorage.getItem(DISMISSED) || '';
    } catch {
        // Storage can be unavailable (private windows): it shows again.
        return '';
    }
}

/** UpdateNotice is the notice of an update status names, if any; with
    dismissible, the member may dismiss it until the next one */
export function UpdateNotice({ status, dismissible = false }) {
    const notice = status ? updateNotice(status) : null;
    const key = notice ? noticeKey(status, notice) : '';
    const [dismissed, setDismissed] = useState(dismissedKey);
    const act = useAction();
    if (!notice || (dismissible && dismissed === key)) return null;
    const go = () => act.run(async () => {
        if (notice.action === 'install') await api.post('/api/update');
        else await restartDens(notice.action === 'update' ? '/api/update' : '/api/restart');
    });
    const dismiss = () => {
        try {
            localStorage.setItem(DISMISSED, key);
        } catch {
            // As above: it stays dismissed until the page reloads.
        }
        setDismissed(key);
    };
    return (
        <div role="status" class="alert alert-info flex flex-wrap" data-update-notice>
            <span>
                {notice.text}
                {notice.command && <> <code>{notice.command}</code></>}
            </span>
            <span class="flex gap-2">
                {notice.action && (
                    <button type="button" class="btn btn-sm" disabled={act.busy} onClick={go}>
                        {act.busy && <span class="loading loading-spinner loading-sm"></span>}
                        {act.busy ? (notice.action === 'install' ? 'Opening the installer…' : 'Restarting…') : notice.label}
                    </button>
                )}
                {dismissible && <button type="button" class="btn btn-ghost btn-sm" onClick={dismiss}>Dismiss</button>}
            </span>
            <ErrorText message={act.error} />
        </div>
    );
}
