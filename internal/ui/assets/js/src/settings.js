// Settings page controls

import { postJSON } from './api.js';
import { handleSelect, handleToggle } from './forms.js';
import { showError } from './ui.js';

export function initSettings() {
    handleSelect('log-level', '/api/settings', 'logLevel');
    handleToggle('background-update-checks', '/api/settings', 'backgroundUpdateChecks');
    handleToggle('update-notifications', '/api/settings', 'updateNotifications');

    document.getElementById('logout')?.addEventListener('click', async () => {
        try {
            await postJSON('/api/logout', {});
            window.location.replace('/');
        } catch (e) {
            showError(e.message);
        }
    });
}
