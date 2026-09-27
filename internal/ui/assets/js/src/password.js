// Local password forms: first setup, and changing it from settings

import { postJSON } from './api.js';
import { blockClicks, unblockClicks, showDialog } from './ui.js';

function showFormError(el, message) {
    if (!el) return;
    el.textContent = message;
    el.classList.toggle('hidden', !message);
}

export function initPasswordSetup() {
    const form = document.getElementById('password-setup-form');
    if (!form) return;
    const error = document.getElementById('password-setup-error');
    form.addEventListener('submit', async (event) => {
        event.preventDefault();
        const password = document.getElementById('password-new').value;
        const confirm = document.getElementById('password-confirm').value;
        if (password !== confirm) {
            showFormError(error, 'The passwords do not match.');
            return;
        }
        showFormError(error, '');
        blockClicks();
        try {
            await postJSON('/api/password', { password });
            window.location.replace('/');
        } catch (e) {
            showFormError(error, e.message);
        } finally {
            unblockClicks();
        }
    });
}

export function initPasswordChange() {
    const form = document.getElementById('password-change-form');
    if (!form) return;
    const error = document.getElementById('password-change-error');
    form.addEventListener('submit', async (event) => {
        event.preventDefault();
        const current = document.getElementById('password-current').value;
        const next = document.getElementById('password-next').value;
        const confirm = document.getElementById('password-next-confirm').value;
        if (next !== confirm) {
            showFormError(error, 'The new passwords do not match.');
            return;
        }
        showFormError(error, '');
        blockClicks();
        try {
            await postJSON('/api/password/change', { current, next });
            form.reset();
            showDialog({ title: 'Password changed', message: 'Use the new password for your next backup or restore.' });
        } catch (e) {
            showFormError(error, e.message);
        } finally {
            unblockClicks();
        }
    });
}
