// The call's pane, apart from its component so the tests can check it: how
// its tiles and its stage lay out, and whether it shows, which the
// players follow, showing nothing while it does.

import { useEffect, useState } from 'preact/hooks';
import { tileLayout } from './player.js';

// GAP is the room between tiles. A tile is 4:3, at most TILE_MOST wide, so
// one member alone isn't a wall, and at least TILE_LEAST, past which the
// tiles scroll. Under a stage they take a row ROW tall, in ROW_ROOM, which
// leaves room for its scroll bar.
export const GAP = 8;
const RATIO = 4 / 3;
const TILE_MOST = 320;
const TILE_LEAST = 96;
export const ROW = 104;
export const ROW_ROOM = ROW + 12;

// paneLayout lays out a call's pane, width by height: count tiles, the
// members' and the shares not watched, and staged, the shares on the
// stage. Without a stage the tiles take the pane, in the columns that make
// them largest; with one they take a row along its foot, which scrolls
// sideways, and the stage the rest, laid out as the player lays its shares.
export function paneLayout(count, staged, width, height) {
    if (staged > 0) {
        const stageHeight = Math.max(0, height - ROW_ROOM - GAP);
        return {
            stage: { ...tileLayout(staged, width, stageHeight), height: stageHeight },
            tiles: { row: true, w: Math.round(ROW * RATIO), h: ROW },
        };
    }
    let best = { cols: 1, w: 0 };
    for (let cols = 1; cols <= count; cols++) {
        const rows = Math.ceil(count / cols);
        const w = Math.min(TILE_MOST, (width - GAP * (cols - 1)) / cols, ((height - GAP * (rows - 1)) / rows) * RATIO);
        // Of two as large, the wider, which keeps them in sight together.
        if (w >= best.w) best = { cols, w };
    }
    // Too many for the pane: as many across as fit, and the rest scroll.
    if (count > 0 && best.w < TILE_LEAST) {
        const cols = Math.max(1, Math.floor((width + GAP) / (TILE_LEAST + GAP)));
        best = { cols, w: Math.max(TILE_LEAST, (width - GAP * (cols - 1)) / cols) };
    }
    const w = Math.floor(best.w);
    return { stage: null, tiles: { row: false, cols: best.cols, w, h: Math.floor(w / RATIO) } };
}

// shown is whether the call's pane shows, for the players.
let shown = false;
const listeners = new Set();

export function setPaneShown(on) {
    if (on === shown) return;
    shown = on;
    listeners.forEach((fn) => fn(on));
}

export function usePaneShown() {
    const [on, setOn] = useState(shown);
    useEffect(() => {
        setOn(shown);
        listeners.add(setOn);
        return () => listeners.delete(setOn);
    }, []);
    return on;
}
