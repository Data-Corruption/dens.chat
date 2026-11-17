// The browser tab's icon: Dens's mark, with a red dot while a DM or a
// mention waits in any den, so one arriving while another tab has the
// focus marks Dens's. The page's shell carries both icons, and the page
// swaps them. The Android app has no tab, and its WebView shows neither.

import { onEvent } from './events.js';

/** waiting says whether any den holds a DM or a mention the member hasn't
    read, from the dens' statuses, whose mentions count every message in a
    DM */
export function waiting(dens) {
    return (dens || []).some((d) => d.mentions > 0);
}

/** markTab sets the tab's icon, link, to its dotted one while on, and
    back to the mark otherwise, where read is the mark's address; it
    touches the link only as it changes, which a browser would fetch again */
export function markTab(link, read, on) {
    const want = on ? link.getAttribute('data-unread') : read;
    if (want && link.getAttribute('href') !== want) link.setAttribute('href', want);
}

/** watchTab keeps the tab's icon to the dens' statuses as they come, and
    returns what stops it, the mark back */
export function watchTab(doc = document) {
    const link = doc.querySelector('link[rel="icon"][data-unread]');
    if (!link) return () => {};
    const read = link.getAttribute('href');
    const stop = onEvent((msg) => msg.t === 'dens' && markTab(link, read, waiting(msg.d.dens)));
    return () => {
        stop();
        markTab(link, read, false);
    };
}
