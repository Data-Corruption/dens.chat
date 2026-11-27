// One embedded video plays at a time across the message list, whether a
// YouTube player or a Steam trailer. Each player holds a token of its own,
// so another card or cover for the same video stays put.

let active = null;
const listeners = new Set();

function notify() {
    listeners.forEach((fn) => fn(active));
}

/** claimPlay makes token the one active player, stopping any other. */
export function claimPlay(token) {
    if (active === token) return;
    active = token;
    notify();
}

/** releasePlay clears the active player when token still holds it. */
export function releasePlay(token) {
    if (active !== token) return;
    active = null;
    notify();
}

/** isPlaying reports whether token is the one active player. */
export function isPlaying(token) {
    return active === token;
}

/** onPlayChange calls fn whenever the active player changes; returns stop. */
export function onPlayChange(fn) {
    listeners.add(fn);
    return () => listeners.delete(fn);
}
