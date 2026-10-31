// The page's history (M13.1): Back, a phone's or a browser's, closes what's
// on top, and otherwise goes up a level. Each entry the page makes holds its
// trail, the screens from Home to the one it shows: Home, the history's
// root; a den's list; one of its channels; the members beside it; and the
// dialogs and the viewer over them, its layers. A move goes from the trail
// shown to the one it asks for: back through the history to where the two
// part, then forward a step at a time, so the history holds the trail
// shown, and Back walks up it.
//
// The phone layout's panes, below 768 pixels, are each a level: a den's
// list, its channel, and the members beside it, or a DM's profile. A wider
// window shows them together, so a den is one level there, its channel's.

import { useEffect, useRef, useState } from 'preact/hooks';

// A screen is one of { k: 'home' }, { k: 'list', den }, { k: 'chan', den,
// ch }, { k: 'side', den, ch }, { k: 'layer', id }, or { k: 'raw', path },
// an entry the page didn't make, as a fragment's load makes one. A
// channel's ch is its ID, '' for the one the den picks, or NO_DM.
export const HOME = { k: 'home' };

// Tailwind's md, where the page lays a den's panes side by side.
const WIDE = '(min-width: 48rem)';

// chatRoute reads /den/<den>[/<channel>] from a path, where the channel
// may be dms, the Direct messages tab with no DM open (M11).
export function chatRoute(path) {
    const m = path.match(/^\/den\/([A-Za-z0-9_-]+)(?:\/([0-9]+|dms))?\/?$/);
    return m ? { denID: m[1], channelID: m[2] || '' } : null;
}

// pathOf is the address of a trail's screen: a layer keeps the one under
// it.
export function pathOf(trail) {
    for (let i = trail.length - 1; i >= 0; i--) {
        const s = trail[i];
        if (s.k === 'home') return '/';
        if (s.k === 'raw') return s.path;
        if (s.k === 'list') return `/den/${s.den}`;
        if (s.k === 'chan' || s.k === 'side') return `/den/${s.den}${s.ch ? `/${s.ch}` : ''}`;
    }
    return '/';
}

// paneOf is the den's pane a trail shows, under any layers: list, chan or
// side, or '' away from a den.
export function paneOf(trail) {
    for (let i = trail.length - 1; i >= 0; i--) {
        const k = trail[i].k;
        if (k === 'list' || k === 'chan' || k === 'side') return k;
        if (k === 'home') return '';
    }
    return '';
}

// denTrail is the trail to a den's pane, with the channel ch shown: on the
// phone layout the list, the channel above it and the members above that;
// in a wider window the den alone.
export function denTrail(den, ch, pane, narrow) {
    if (!narrow) return [HOME, { k: 'chan', den, ch }];
    const trail = [HOME, { k: 'list', den }];
    if (pane === 'list') return trail;
    trail.push({ k: 'chan', den, ch });
    if (pane === 'side') trail.push({ k: 'side', den, ch });
    return trail;
}

// trailFor is the trail to the screen an address names, as a page opened
// there shows it: Home, and a channel with Home and its den's list beneath.
export function trailFor(path, narrow) {
    const at = chatRoute(path);
    if (!at) return [HOME];
    return denTrail(at.denID, at.channelID, at.channelID ? 'chan' : 'list', narrow);
}

function same(a, b) {
    if (a.k !== b.k) return false;
    if (a.k === 'home') return true;
    if (a.k === 'list') return a.den === b.den;
    if (a.k === 'chan' || a.k === 'side') return a.den === b.den && a.ch === b.ch;
    if (a.k === 'layer') return a.id === b.id;
    return false;
}

// plan is how the history goes from the trail cur to target: back over the
// entries past where the two part, to keep of them; then, unless target
// ends there, replace the entry at keep with target's, and push the rest.
export function plan(cur, target) {
    let keep = 0;
    while (keep < cur.length && keep < target.length && same(cur[keep], target[keep])) keep++;
    if (keep === cur.length) return { back: 0, keep, replace: false };
    if (keep === target.length) return { back: cur.length - keep, keep, replace: false };
    return { back: cur.length - 1 - keep, keep, replace: true };
}

const KINDS = new Set(['home', 'list', 'chan', 'side', 'layer', 'raw']);
const isString = (v) => typeof v === 'string';

// readTrail is the trail an entry's state holds, or null for one the page
// didn't make.
export function readTrail(state) {
    const t = state?.trail;
    if (!Array.isArray(t) || t.length === 0 || t[0]?.k !== 'home') return null;
    for (const s of t) {
        if (!s || !KINDS.has(s.k)) return null;
        if ((s.k === 'list' || s.k === 'chan' || s.k === 'side') && !(isString(s.den) && (s.k === 'list' || isString(s.ch)))) return null;
        if (s.k === 'layer' && !Number.isInteger(s.id)) return null;
        if (s.k === 'raw' && !isString(s.path)) return null;
    }
    return t;
}

// A fragment the page reads, which a link from dens open or the Android
// app's shell carries (see app.jsx).
const READ = /^#(?:.*&)?(?:token|message|open)=/;

// createNav keeps the history of the window win.
export function createNav(win) {
    const h = win.history;
    // cur is the current entry's trail, and shown the one the page shows,
    // which a move's steps through the history leave until it's done.
    let cur = [HOME];
    let shown = cur;
    const watchers = new Set();
    const moves = [];
    let moving = false;
    // landed hears the popstate a move's step back causes.
    let landed = null;
    // The layers open, each with what closes it when the history drops it.
    const layers = new Map();
    let layerIDs = 0;

    const narrow = () => !win.matchMedia?.(WIDE).matches;

    function write(push, trail) {
        h[push ? 'pushState' : 'replaceState']({ trail }, '', pathOf(trail));
    }

    // show has the page show cur, closing the layers it no longer holds. A
    // layer that won't close, as Manage with changes unsaved, goes back on.
    function show() {
        const before = shown;
        shown = cur;
        for (const s of before) {
            if (s.k !== 'layer' || cur.some((t) => same(t, s))) continue;
            const back = layers.get(s.id);
            layers.delete(s.id);
            if (back?.() === false) {
                layers.set(s.id, back);
                move((c) => [...c, s]);
            }
        }
        watchers.forEach((fn) => fn(shown));
    }

    function traverse(delta) {
        return new Promise((resolve) => {
            // A step that never lands, as past the history's start, ends
            // the wait all the same.
            const timer = setTimeout(() => {
                landed = null;
                resolve();
            }, 3000);
            landed = () => {
                clearTimeout(timer);
                resolve();
            };
            h.go(delta);
        });
    }

    async function drain() {
        if (moving) return;
        moving = true;
        try {
            while (moves.length) {
                const target = moves.shift()(cur);
                if (!target) continue;
                let p = plan(cur, target);
                if (p.back) {
                    await traverse(-p.back);
                    p = plan(cur, target);
                    // An entry the trail misnamed: this one becomes target.
                    if (p.back) p = { back: 0, keep: target.length - 1, replace: true };
                }
                if (p.replace) write(false, target.slice(0, p.keep + 1));
                for (let i = p.keep + (p.replace ? 2 : 1); i <= target.length; i++) write(true, target.slice(0, i));
                cur = target;
                show();
            }
        } finally {
            moving = false;
        }
    }

    // move goes to the trail fn makes of the trail then current, in turn
    // after the moves before it; fn returns null to stay.
    function move(fn) {
        moves.push(fn);
        drain();
    }

    // stale drops the layers on top whose dialogs are gone, as a reload or
    // Forward finds them.
    const stale = (c) => {
        let end = c.length;
        while (end > 1 && c[end - 1].k === 'layer' && !layers.has(c[end - 1].id)) end--;
        return end === c.length ? null : c.slice(0, end);
    };

    win.addEventListener('popstate', (e) => {
        const t = readTrail(e.state);
        if (landed) {
            cur = t || cur;
            const done = landed;
            landed = null;
            done();
            return;
        }
        if (t) {
            cur = t;
        } else if (READ.test(win.location.hash)) {
            // A fragment's load sits on the entry before it, and the
            // hashchange after says where it goes.
            cur = [...cur, { k: 'raw', path: win.location.pathname }];
        } else {
            // An entry from before the page kept trails.
            cur = trailFor(win.location.pathname, narrow());
            write(false, cur);
        }
        show();
        move(stale);
    });

    return {
        narrow,
        get trail() {
            return shown;
        },
        // start takes the trail of the entry the page opened at, from a
        // reload, or else makes Home and what's beneath its screen.
        start() {
            const t = readTrail(h.state);
            if (t && pathOf(t) === win.location.pathname) {
                cur = t;
            } else {
                cur = trailFor(win.location.pathname, narrow());
                write(false, cur.slice(0, 1));
                for (let i = 2; i <= cur.length; i++) write(true, cur.slice(0, i));
            }
            shown = cur;
            move(stale);
        },
        watch(fn) {
            watchers.add(fn);
            return () => watchers.delete(fn);
        },
        // open goes to the screen an address names, as a link to it does.
        open: (path) => move(() => trailFor(path, narrow())),
        // den goes to a den's pane, list, chan or side, showing ch.
        den: (den, ch, pane) => move(() => denTrail(den, ch, pane, narrow())),
        home: () => move(() => [HOME]),
        // openLayer puts a dialog over what's shown, and returns its ID;
        // back closes it once the history drops it, and returns false to
        // stay open.
        openLayer(back) {
            const id = ++layerIDs;
            layers.set(id, back);
            move((c) => [...c, { k: 'layer', id }]);
            return id;
        },
        // closeLayer takes back the entry of a dialog the page closed.
        closeLayer(id) {
            layers.delete(id);
            move((c) => {
                const at = c.findIndex((s) => s.k === 'layer' && s.id === id);
                return at < 0 ? null : c.slice(0, at);
            });
        },
    };
}

let page = null;

// nav is the page's history, started once.
export function nav() {
    if (!page) {
        page = createNav(window);
        page.start();
    }
    return page;
}

// useTrail is the trail the page shows, kept current.
export function useTrail() {
    const [trail, setTrail] = useState(() => nav().trail);
    useEffect(() => {
        setTrail(nav().trail);
        return nav().watch(setTrail);
    }, []);
    return trail;
}

// useLayer gives a dialog an entry in the history while it's open, so
// Back closes it with onBack, and its closing by the page takes the entry
// back. onBack returns false to stay open.
export function useLayer(onBack) {
    const back = useRef(onBack);
    back.current = onBack;
    useEffect(() => {
        const id = nav().openLayer(() => back.current());
        return () => nav().closeLayer(id);
    }, []);
}

// The layout holds while something of the page's plays full screen, from
// the moment it asks (M13.4): the window takes the screen's size, as a
// phone turned sideways does, and the layout changing under what plays
// would take it away, the share docked on the phone layout among them.
// The window may take its new size before the browser says what's full
// screen, so the page says as it asks, through holdLayout, and the hold
// ends as nothing is full screen.
let layoutHeld = false;
const layoutWatchers = new Set();

export function holdLayout(on) {
    layoutHeld = on;
    if (!on) layoutWatchers.forEach((fn) => fn());
}

globalThis.document?.addEventListener?.('fullscreenchange', () => {
    if (layoutHeld && !document.fullscreenElement) holdLayout(false);
});

// useNarrow says whether the window has the phone layout, kept current,
// but while the layout holds.
export function useNarrow() {
    const [narrow, setNarrow] = useState(() => typeof window !== 'undefined' && !window.matchMedia?.(WIDE).matches);
    useEffect(() => {
        const q = window.matchMedia?.(WIDE);
        if (!q) return undefined;
        const change = () => !layoutHeld && setNarrow(!q.matches);
        change();
        q.addEventListener('change', change);
        layoutWatchers.add(change);
        return () => {
            q.removeEventListener('change', change);
            layoutWatchers.delete(change);
        };
    }, []);
    return narrow;
}
