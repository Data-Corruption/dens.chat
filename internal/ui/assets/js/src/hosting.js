// What the settings say of hosting the den on this computer (M9.1), and
// of moving it to another (M10), from what the service reports of it.

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
// send it. A form that hosts no den, as the Flatpak (M9.3), has no switch,
// only the line on what hosting needs.
export function hostingNote(h) {
    if (h.can_host === false) {
        return { cantHost: 'Hosting a den needs Dens installed on Linux or Windows, where it keeps running when nobody is signed in.' };
    }
    if (h.enabled && !h.serving) return { problem: h.problem || "The den hasn't started yet." };
    if (h.serving) return { address: `127.0.0.1:${h.den_port}`, udp: h.media_udp_port, tcp: h.media_tcp_port };
    return null;
}

// exportedAt shows when the den was exported, as the page shows a time
// whose hour matters; locale is for the tests.
export function exportedAt(ms, locale) {
    return new Date(ms).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
}

// moving is what the settings offer of moving the den to another computer
// (M10), or null before there's a den: its export, and once it's
// exported, when, with removing this computer's copy, which the service
// lets go once another computer serves the den.
export function moving(h, locale) {
    if (h.can_host === false || !h.den) return null;
    if (!h.exported) return { den: h.den };
    return { den: h.den, exported: exportedAt(h.exported, locale), url: h.url };
}

// hostingOnWarning is what turning hosting on says first of a den exported
// since it was last on, which another computer may host by now, or null.
export function hostingOnWarning(h, locale) {
    if (!h.exported) return null;
    return `You exported ${h.den} on ${exportedAt(h.exported, locale)}. Another computer may host it by now, ` +
        'and members would then reach whichever one its address points at.';
}

// setupNeeded says whether home offers to set the den up, while hosting is
// on: to create it, or to finish, while it has no owner. A den moved here
// has one already, so a server holding an export offers nothing.
export function setupNeeded(hosting) {
    return !!hosting.enabled && !hosting.owned;
}

// ownRowNote is what home says of the den this computer holds, on its row,
// while hosting is off, or null: that members can't reach it, leading to
// the switch, or once it's exported, that this copy goes once another
// computer hosts it, leading to Hosting (M10).
export function ownRowNote(den, hosting, locale) {
    if (!den.own || hosting.enabled) return null;
    if (hosting.exported) {
        return {
            label: 'Exported',
            text: `You exported it on ${exportedAt(hosting.exported, locale)}. Once another computer hosts it, remove it from this one.`,
            action: 'Open Hosting',
        };
    }
    return { label: 'Hosting is off', text: "Members can't reach it until hosting is back on.", action: 'Turn hosting on' };
}
