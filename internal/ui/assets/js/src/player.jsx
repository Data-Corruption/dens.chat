// The player that floats over the page and plays the shares this member
// watches (M4.2), and their own while they share, as a preview: they drag
// it by its bar and resize it from its corners, and the browser keeps
// where it was and its size. It holds every share side by side; clicking
// one makes it fill the player, while the others go on out of sight, and
// clicking it again puts it back. From M11 a share they watch can play in
// a window of its own, while the player keeps its place. From M13.4 the
// phone layout docks it instead, across the top of the channel's pane.

import { render } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { FullIcon, SoundIcon } from './files.jsx';
import { holdLayout, useNarrow } from './nav.js';
import { canPopOut } from './platform.js';
import { clampBox, defaultBox, dockedShown, nextShown, readPlayer, resizeBox, tileLayout } from './player.js';
import { onPopOut, popIn, popOut, poppedOut } from './popout.js';
import { closePlayer, setPreview, setShareVolume, speakerID, unwatchShare } from './voice.js';
import { useCall } from './voice.jsx';

const PLAYER = 'DENS_PLAYER';
// The bar's height, which the shares' room leaves out.
const BAR = 32;

function viewport() {
    return { w: window.innerWidth, h: window.innerHeight };
}

function keptBox() {
    let kept = null;
    try {
        kept = readPlayer(localStorage.getItem(PLAYER));
    } catch {
        // Storage can be unavailable; the player starts in its corner.
    }
    return clampBox(kept || defaultBox(viewport()), viewport());
}

function keepBox(box) {
    try {
        localStorage.setItem(PLAYER, JSON.stringify(box));
    } catch {
        // As above.
    }
}

// SELF is the key of the member's own screen among the player's tiles.
const SELF = 'self';

// tilesOf are the shares the player shows: those the member watches, and
// their own screen while they share it and show it. Their own plays from
// the capture, always silent, since they hear its sound already.
function tilesOf(watched, share) {
    const own = share?.preview && share.stream ? [{ member: SELF, label: 'Your screen', stream: share.stream, volume: 0, self: true }] : [];
    return [...watched, ...own];
}

// SharePlayer is the floating player, over every page in a wider window.
// It stays on the phone layout, showing nothing, for the shares' own
// windows, which it closes as their shares stop, and with the page.
export function SharePlayer() {
    const { call, watched, share } = useCall();
    const narrow = useNarrow();
    const [box, setBox] = useState(keptBox);
    const [focus, setFocus] = useState('');
    const [windows, setWindows] = useState(poppedOut);
    const drag = useRef(null);
    useEffect(() => {
        const fit = () => setBox((b) => clampBox(b, viewport()));
        window.addEventListener('resize', fit);
        return () => window.removeEventListener('resize', fit);
    }, []);
    useEffect(() => onPopOut(() => setWindows(poppedOut())), []);
    // A share's window closes once the member stops watching it, as when
    // they leave the call or the share ends, and with the player.
    useEffect(() => {
        for (const member of windows.keys()) if (!watched.some((w) => w.member === member)) popIn(member);
    });
    useEffect(() => () => [...poppedOut().keys()].forEach(popIn), []);
    const tiles = tilesOf(watched, share);
    const own = tiles.filter((w) => w.self);
    if (narrow || !tiles.length) return null;
    const focused = tiles.some((w) => w.member === focus) ? focus : '';
    const shown = focused ? tiles.filter((w) => w.member === focused) : tiles;
    const { cols, rows } = tileLayout(shown.length, box.w, box.h - BAR);

    // A drag moves the player by its bar, or resizes it from a corner,
    // from where the pointer went down.
    function start(e, corner) {
        if (e.button !== 0) return;
        e.preventDefault();
        e.currentTarget.setPointerCapture(e.pointerId);
        drag.current = { corner, x: e.clientX, y: e.clientY, box };
    }
    function move(e) {
        const d = drag.current;
        if (!d) return;
        const dx = e.clientX - d.x;
        const dy = e.clientY - d.y;
        setBox(d.corner ? resizeBox(d.box, d.corner, dx, dy, viewport()) : clampBox({ ...d.box, x: d.box.x + dx, y: d.box.y + dy }, viewport()));
    }
    function end() {
        if (!drag.current) return;
        drag.current = null;
        setBox((b) => {
            keepBox(b);
            return b;
        });
    }
    const handlers = { onPointerMove: move, onPointerUp: end, onPointerCancel: end };
    const hidden = tiles.length - shown.length;
    const name = (w) => (w.self ? 'Your screen' : `Watching ${w.label}`);
    const title = shown.length === 1 ? name(shown[0])
        : `Watching ${watched.length === 1 ? watched[0].label : `${watched.length} shares`}${own.length ? ', and your screen' : ''}`;
    return (
        <section aria-label="Screen shares" class="fixed z-40 rounded-box border border-base-300 bg-base-100 shadow-xl"
            style={{ left: `${box.x}px`, top: `${box.y}px`, width: `${box.w}px`, height: `${box.h}px` }}>
            {/* The corners round off what's inside, but not the handles,
                which sit over them. */}
            <div class="flex h-full flex-col overflow-hidden rounded-box">
                <div class="flex shrink-0 cursor-move touch-none select-none items-center gap-2 border-b border-base-300 bg-bar pl-3 pr-1"
                    style={{ height: `${BAR}px` }} onPointerDown={(e) => start(e, '')} {...handlers}>
                    <span class="min-w-0 flex-1 truncate text-sm font-medium">{title}</span>
                    {hidden > 0 && <span class="shrink-0 text-xs text-base-content/60">{hidden} more out of sight</span>}
                    <button type="button" class="btn btn-ghost btn-xs btn-square" title="Close the player"
                        aria-label="Close the player: stop watching every share, and hide your own" onPointerDown={(e) => e.stopPropagation()}
                        onClick={closePlayer}>✕</button>
                </div>
                <div class="grid min-h-0 flex-1 gap-px bg-base-300"
                    style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`, gridTemplateRows: `repeat(${rows}, minmax(0, 1fr))` }}>
                    {tiles.map((w) => (windows.has(w.member) ? (
                        <Away key={w.member} w={w} win={windows.get(w.member)} deafened={!!call?.deafened} hidden={!!focused && w.member !== focused} />
                    ) : (
                        <Tile key={w.member} w={w} deafened={!!call?.deafened} hidden={!!focused && w.member !== focused} many={tiles.length > 1}
                            focused={w.member === focused} onFocus={() => setFocus(w.member === focused ? '' : w.member)} />
                    )))}
                </div>
            </div>
            {['nw', 'ne', 'sw', 'se'].map((corner) => (
                <span key={corner} aria-hidden="true" onPointerDown={(e) => start(e, corner)} {...handlers}
                    class={`absolute z-10 h-3.5 w-3.5 touch-none ${corner.includes('n') ? '-top-px' : '-bottom-px'} ${corner.includes('w') ? '-left-px' : '-right-px'} ${corner === 'nw' || corner === 'se' ? 'cursor-nwse-resize' : 'cursor-nesw-resize'}`} />
            ))}
        </section>
    );
}

// DockedPlayer is the player on the phone layout (M13.4): across the top of
// the channel's pane, under the call's bar, or under Home's call box, at
// the screen's width and the share's shape, up to half the screen's
// height. It shows one share at a time, the one the member last began to
// watch, with a button to the next; a tap on its picture plays it full
// screen, which the Android app's shell shows (onShowCustomView), and its
// ✕ stops watching it. A share in a window of its own holds its place, as
// in the floating player, which keeps the windows.
export function DockedPlayer() {
    const { call, watched, share } = useCall();
    const [shown, setShown] = useState('');
    const [sizes, setSizes] = useState({});
    const [windows, setWindows] = useState(poppedOut);
    const known = useRef([]);
    useEffect(() => onPopOut(() => setWindows(poppedOut())), []);
    const tiles = tilesOf(watched, share);
    const keys = tiles.map((w) => w.member);
    const at = dockedShown(keys, known.current, shown);
    useEffect(() => {
        known.current = keys;
        if (at !== shown) setShown(at);
    }, [keys.join(' ')]);
    const w = tiles.find((t) => t.member === at);
    if (!w) return null;
    const [width, height] = sizes[w.member] || [16, 9];
    const place = tiles.length > 1 ? `${keys.indexOf(at) + 1}/${tiles.length}` : '';
    return (
        <section aria-label="Screen shares" data-docked class="w-full shrink-0 bg-black" style={{ aspectRatio: `${width} / ${height}`, maxHeight: '50vh' }}>
            {windows.has(w.member) ? (
                <Away key={w.member} w={w} win={windows.get(w.member)} deafened={!!call?.deafened} />
            ) : (
                <Tile key={w.member} w={w} deafened={!!call?.deafened} docked place={place} onNext={() => setShown(nextShown(keys, at))}
                    onSize={(x, y) => x > 0 && y > 0 && setSizes((s) => ({ ...s, [w.member]: [x, y] }))} />
            )}
        </section>
    );
}

// Away holds the place of a share that plays in a window of its own (M11),
// saying so, with a button that brings it back, and fills that window from
// the page's own render: Preact makes the window's elements, which its
// document takes in.
function Away({ w, win, deafened, hidden }) {
    useEffect(() => {
        if (win.closed) return;
        try {
            render(<Tile w={w} deafened={deafened} doc={win.document} alone />, win.document.body);
        } catch {
            // The window is closing; the page hears so soon.
        }
    });
    useEffect(() => () => {
        try {
            render(null, win.document.body);
        } catch {
            // As above.
        }
    }, [win]);
    return (
        <div class={`flex h-full min-h-0 min-w-0 flex-col items-center justify-center gap-2 bg-black p-2 text-center text-xs text-white/70 ${hidden ? 'hidden' : ''}`}>
            <span>{w.label}'s screen is in a window of its own</span>
            <button type="button" class="btn btn-xs" onClick={() => popIn(w.member)}>Bring it back</button>
        </div>
    );
}

// Tile plays one share: its picture, its sound at the member's level here,
// and controls of the page's own that show on hover or focus. The member's
// own share shows without sound, and its ✕ hides it, while they go on
// sharing. doc is the document the tile shows in: the page's, or a share's
// own window's, where alone has it fill the window (M11). docked has it
// fill the docked player, where a tap on the picture plays it full screen
// and, of several, place says which it is, with a button to the next; and
// it says its share's size through onSize (M13.4).
function Tile({ w, deafened, hidden, many, focused, onFocus, doc = document, alone, docked, place, onNext, onSize }) {
    const frame = useRef(null);
    const video = useRef(null);
    const [playing, setPlaying] = useState(false);
    const [full, setFull] = useState(false);
    const [blocked, setBlocked] = useState(false);
    const [popped, setPopped] = useState('');
    useEffect(() => {
        const el = video.current;
        el.srcObject = w.stream || null;
        setPlaying(false);
        // A window of its own may not play with sound until it's clicked.
        if (w.stream) el.play().then(() => setBlocked(false), (e) => setBlocked(alone && e?.name === 'NotAllowedError'));
    }, [w.stream]);
    useEffect(() => {
        const el = video.current;
        el.volume = w.volume;
        el.muted = w.self || deafened || w.volume === 0;
    }, [w.volume, w.self, deafened]);
    useEffect(() => {
        const id = speakerID();
        if (id && video.current.setSinkId) video.current.setSinkId(id).catch(() => {});
        const onFull = () => setFull(doc.fullscreenElement === frame.current);
        doc.addEventListener('fullscreenchange', onFull);
        return () => doc.removeEventListener('fullscreenchange', onFull);
    }, []);
    // Why a window didn't open shows for a while.
    useEffect(() => {
        if (!popped) return undefined;
        const t = setTimeout(() => setPopped(''), 6000);
        return () => clearTimeout(t);
    }, [popped]);
    // The page's layout holds while its share plays full screen (M13.4).
    function toggleFull() {
        if (doc.fullscreenElement) {
            doc.exitFullscreen().catch(() => {});
            return;
        }
        if (doc === document) holdLayout(true);
        frame.current.requestFullscreen().catch(() => doc === document && holdLayout(false));
    }
    function own() {
        if (!popOut(w.member, w.label)) setPopped("This browser didn't open the window: allow pop-ups for Dens, and try again.");
    }
    function play() {
        video.current.play().then(() => setBlocked(false), () => {});
    }
    const silent = deafened || w.volume === 0;
    const button = 'flex h-6 w-6 shrink-0 items-center justify-center rounded hover:bg-white/20';
    const sized = (e) => onSize?.(e.currentTarget.videoWidth, e.currentTarget.videoHeight);
    return (
        <div ref={frame} class={`group relative min-h-0 min-w-0 bg-black ${hidden ? 'hidden' : ''} ${focused ? 'col-span-full row-span-full' : ''} ${alone ? 'h-screen w-screen' : ''} ${docked ? 'h-full w-full' : ''}`}>
            {/* A click makes one share of several fill the player, and
                another puts it back; docked, it plays it full screen. */}
            <video ref={video} class={`h-full w-full object-contain ${many || docked ? 'cursor-pointer' : ''}`} autoplay playsinline muted={w.self}
                aria-label={w.self ? w.label : `${w.label}'s screen`} onPlaying={() => setPlaying(true)} onLoadedMetadata={sized} onResize={sized}
                onClick={() => (docked ? toggleFull() : many && onFocus())} />
            {!playing && !blocked && (
                <span class="pointer-events-none absolute inset-0 flex items-center justify-center gap-2 p-2 text-center text-xs text-white/70">
                    <span class="loading loading-spinner loading-xs"></span>
                    {w.self ? 'Waiting for your screen' : `Waiting for ${w.label}'s screen`}
                </span>
            )}
            {blocked && (
                <span class="absolute inset-0 flex items-center justify-center p-2">
                    <button type="button" class="btn btn-sm" onClick={play}>Play {w.label}'s screen</button>
                </span>
            )}
            {popped && <span role="status" class="absolute inset-x-0 top-0 bg-black/70 p-2 text-center text-xs text-white">{popped}</span>}
            {/* A touch screen, which has no hover, always shows them (M13.2). */}
            <div class="absolute inset-x-0 bottom-0 flex items-center gap-1 bg-linear-to-t from-black/70 to-transparent px-1 pb-1 pt-5 text-white opacity-0 transition-opacity group-hover:opacity-100 group-has-[:focus-visible]:opacity-100 pointer-coarse:opacity-100">
                <span class="min-w-0 flex-1 truncate px-1 text-xs">{w.label}</span>
                <div class={`group/volume relative shrink-0 ${w.self ? 'hidden' : 'flex'}`}>
                    <button type="button" class={button} onClick={() => setShareVolume(w.member, w.volume === 0 ? 1 : 0)} disabled={deafened}
                        title={deafened ? 'Deafened: you hear nothing of the call' : silent ? 'Unmute' : 'Mute'} aria-label={silent ? 'Unmute' : 'Mute'}>
                        <SoundIcon muted={silent} />
                    </button>
                    <div class="absolute bottom-full left-1/2 hidden -translate-x-1/2 pb-1 group-focus-within/volume:flex group-hover/volume:flex">
                        <div class="flex rounded bg-black/70 px-1 py-2">
                            <input type="range" class="h-16 w-6 cursor-pointer accent-white [direction:rtl] [writing-mode:vertical-lr]" min="0" max="1"
                                step="0.05" value={w.volume} onInput={(e) => setShareVolume(w.member, Number(e.currentTarget.value))}
                                aria-label={`Volume of ${w.label}'s share`} aria-orientation="vertical" aria-valuetext={`${Math.round(w.volume * 100)}%`} />
                        </div>
                    </div>
                </div>
                {!w.self && !alone && canPopOut() && (
                    <button type="button" class={button} onClick={own} title="Open in a window of its own"
                        aria-label={`Open ${w.label}'s screen in a window of its own`}>
                        <WindowIcon />
                    </button>
                )}
                {place && (
                    <button type="button" class={`${button} w-auto gap-0.5 px-1.5 text-xs tabular-nums`} onClick={onNext}
                        aria-label={`The next share: this is ${place.replace('/', ' of ')}`}>
                        {place} <span aria-hidden="true">›</span>
                    </button>
                )}
                {doc.fullscreenEnabled && (
                    <button type="button" class={button} onClick={toggleFull} title={full ? 'Exit full screen' : 'Full screen'}
                        aria-label={full ? 'Exit full screen' : 'Full screen'}>
                        <FullIcon full={full} />
                    </button>
                )}
                {w.self ? (
                    <button type="button" class={button} onClick={() => setPreview(false)} title="Hide your screen. You go on sharing it."
                        aria-label="Hide your screen">
                        ✕
                    </button>
                ) : (
                    <button type="button" class={button} onClick={() => unwatchShare(w.member)} title="Stop watching"
                        aria-label={`Stop watching ${w.label}'s screen`}>
                        ✕
                    </button>
                )}
            </div>
        </div>
    );
}

// WindowIcon is a window with an arrow going out of it, for a share's own.
function WindowIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" aria-hidden="true">
            <path d="M7 3H3.5A1 1 0 0 0 2.5 4v8.5a1 1 0 0 0 1 1H12a1 1 0 0 0 1-1V9" />
            <path d="M9.5 2.5h4v4M13.5 2.5 8 8" />
        </svg>
    );
}
