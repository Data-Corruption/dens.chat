// While Dens can't reach a den (M6.2), the page reads what this computer
// kept of it, and says so over the messages; and Leave den, which needs
// the den, removes it from this computer instead (M10).

export const OFFLINE_NOTE = "The den is offline. You're reading cached messages.";

// showsOffline says whether a den's messages show the note: the stream
// from Dens on this computer is up, as live says, and the den failed to
// connect. A den connecting, as at every start, shows none, and one that
// signed this device out says so in its own words.
export function showsOffline(live, state) {
    return live && state === 'offline';
}

// leaveOffer is what Manage's Leave den offers for a den: leaving it,
// while it answers ('leave'); removing it from this computer while Dens
// can't reach it, as when its server is gone for good, since the den
// can't hear of a leave then ('unreachable'); and removing it once it no
// longer accepts this device ('gone'). The den this computer hosts has no
// Leave den.
export function leaveOffer(den) {
    if (den.own) return null;
    if (den.state === 'connected') return 'leave';
    if (den.state === 'revoked' || den.state === 'removed') return 'gone';
    return 'unreachable';
}
