// The geometry of the player shares play in (M4.2), apart from its
// component so the tests can check it: where it sits and how large it is,
// which the browser keeps, kept inside the window; resizing from a corner;
// and how its shares lay out.

// The smallest the player gets, its margin from the window's edges where
// it starts, and the room it leaves below for the message bar.
export const PLAYER_MIN = { w: 240, h: 160 };
const MARGIN = 16;
const BAR_ROOM = 96;

// readPlayer reads the box the browser kept, or null for anything that
// isn't one.
export function readPlayer(text) {
    let kept;
    try {
        kept = JSON.parse(text || 'null');
    } catch {
        return null;
    }
    if (!kept || typeof kept !== 'object') return null;
    const { x, y, w, h } = kept;
    return [x, y, w, h].every(Number.isFinite) ? { x, y, w, h } : null;
}

// defaultBox is where the player starts: at the right, above the message
// bar, at about a third of the window's width, with room for a 16:9
// picture under its bar.
export function defaultBox(view) {
    const w = Math.min(Math.max(PLAYER_MIN.w, Math.round(view.w / 3)), 560, view.w - 2 * MARGIN);
    const h = Math.round((w * 9) / 16) + 32;
    return clampBox({ x: view.w - w - MARGIN, y: view.h - h - BAR_ROOM, w, h }, view);
}

// clampBox keeps a box inside the window: no larger than it, no smaller
// than PLAYER_MIN where the window allows, and with every edge inside.
export function clampBox(box, view) {
    const w = Math.round(Math.min(Math.max(box.w, PLAYER_MIN.w), view.w));
    const h = Math.round(Math.min(Math.max(box.h, PLAYER_MIN.h), view.h));
    const x = Math.round(Math.min(Math.max(box.x, 0), view.w - w));
    const y = Math.round(Math.min(Math.max(box.y, 0), view.h - h));
    return { x, y, w, h };
}

// resizeBox moves a corner of a box ("nw", "ne", "sw" or "se") by dx and
// dy, keeping the opposite corner where it was, within the window.
export function resizeBox(box, corner, dx, dy, view) {
    const left = corner.includes('w');
    const top = corner.includes('n');
    let w = box.w + (left ? -dx : dx);
    let h = box.h + (top ? -dy : dy);
    w = Math.min(Math.max(w, PLAYER_MIN.w), left ? box.x + box.w : view.w - box.x);
    h = Math.min(Math.max(h, PLAYER_MIN.h), top ? box.y + box.h : view.h - box.y);
    const x = left ? box.x + box.w - w : box.x;
    const y = top ? box.y + box.h - h : box.y;
    return clampBox({ x, y, w, h }, view);
}

// tileLayout lays shares out in the player: one fills it, two sit side by
// side in a wide player or one above the other in a tall one, three or
// four take two rows of two, and five, the most with the member's own
// beside the four they may watch, three across two rows, or two across
// three in a tall player.
export function tileLayout(n, width, height) {
    const wide = width >= height * 1.2;
    if (n <= 1) return { cols: 1, rows: 1 };
    if (n === 2) return wide ? { cols: 2, rows: 1 } : { cols: 1, rows: 2 };
    if (n <= 4) return { cols: 2, rows: 2 };
    return wide ? { cols: 3, rows: 2 } : { cols: 2, rows: 3 };
}
