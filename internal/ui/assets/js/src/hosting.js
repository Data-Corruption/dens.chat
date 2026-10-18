// What the settings say of hosting the den on this computer (M9.1), from
// what the service reports of it.

// hostingAsk is what moving the switch to on asks of the member: the local
// password to turn hosting on ('on'), a confirmation to turn it off
// ('off'), or nothing where hosting already is ('').
export function hostingAsk(enabled, on) {
    if (on === enabled) return '';
    return on ? 'on' : 'off';
}

// hostingNote is the line under the switch, or null while hosting is off:
// why the den doesn't serve while hosting is on, as when another program
// holds one of its ports, or, while it serves, where Caddy and the router
// send it.
export function hostingNote(h) {
    if (h.enabled && !h.serving) return { problem: h.problem || "The den hasn't started yet." };
    if (h.serving) return { address: `127.0.0.1:${h.den_port}`, udp: h.media_udp_port, tcp: h.media_tcp_port };
    return null;
}
