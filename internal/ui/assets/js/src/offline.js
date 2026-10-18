// While Dens can't reach a den (M6.2), the page reads what this computer
// kept of it, and says so over the messages.

export const OFFLINE_NOTE = "The den is offline. You're reading cached messages.";

// showsOffline says whether a den's messages show the note: the stream
// from Dens on this computer is up, as live says, and the den failed to
// connect. A den connecting, as at every start, shows none, and one that
// signed this device out says so in its own words.
export function showsOffline(live, state) {
    return live && state === 'offline';
}
