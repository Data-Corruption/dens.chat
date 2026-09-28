// Den IDs are decimal strings that grow with time, too large to trust to
// JavaScript numbers. Without leading zeros, a longer ID is larger.
export function compareIds(a, b) {
    if (!a) return b ? -1 : 0;
    if (!b) return 1;
    if (a.length !== b.length) return a.length - b.length;
    return a < b ? -1 : a > b ? 1 : 0;
}

// newNonce returns 16 random bytes as base64url, the idempotency key that
// keeps a retried send from posting twice.
export function newNonce() {
    const bytes = crypto.getRandomValues(new Uint8Array(16));
    let s = '';
    bytes.forEach((b) => (s += String.fromCharCode(b)));
    return btoa(s).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '');
}

// unread reports whether a channel has messages past the read position.
export function unread(state) {
    return !!state && compareIds(state.last_message_id, state.message_id) > 0;
}
