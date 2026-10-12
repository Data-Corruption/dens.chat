// What someone pastes to add a den (M8.3): an invite, which names its den
// and lets them join it or sign in to an account there, or a den's
// address, which signs in.

// readPasted reads pasted text as an invite or a den's address, or as
// neither. A bare host name is taken for an https address, as dens are
// served.
export function readPasted(text) {
    const value = text.trim();
    if (value.startsWith('dens1:')) return { kind: 'invite', value };
    if (/^https?:\/\/[^\s/]+\/?$/i.test(value)) return { kind: 'address', value };
    if (/^[a-z0-9-]+(\.[a-z0-9-]+)+(:\d+)?\/?$/i.test(value)) return { kind: 'address', value: `https://${value}` };
    return { kind: '', value };
}
