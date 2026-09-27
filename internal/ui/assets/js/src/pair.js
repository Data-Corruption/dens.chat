// Pairing
// dens open opens this page with a one-time token in the URL fragment. The
// fragment never reaches the server in the request line; the page sends it
// once, removes it from the address bar and history, and reloads paired.

import { postJSON } from './api.js';

function tokenFromFragment() {
    const hash = window.location.hash;
    if (!hash.startsWith('#')) return '';
    return new URLSearchParams(hash.slice(1)).get('token') || '';
}

export async function initPairing() {
    const token = tokenFromFragment();
    if (!token) return;
    // Drop the token from the address bar and history before anything else.
    history.replaceState(null, '', window.location.pathname);

    const status = document.getElementById('pair-status');
    if (status) {
        status.textContent = 'Pairing this browser...';
        status.className = 'alert';
    }
    try {
        await postJSON('/api/pair', { token });
        window.location.replace('/');
    } catch (e) {
        if (status) {
            status.textContent = e.message;
            status.className = 'alert alert-error';
        }
    }
}
