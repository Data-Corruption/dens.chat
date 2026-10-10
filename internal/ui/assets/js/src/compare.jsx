// Smaller copies (M5): the setting, the viewer's comparison of a photo's or
// video's two versions before it's sent, and how far a video's copy is.
//
// A photo or video added to a message goes as a smaller copy unless the
// member sends it full size. The local service makes both versions as the
// file is added, uploads the one to send, and keeps both until the message
// goes, so the comparison shows them from there, and switching uploads the
// other. A video's copy takes a while, so the page follows it (M5.4).

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { fitBox, formatSize, step } from './files.jsx';

const SMALLER = 'DENS_SEND_SMALLER';
let smaller = null;

// sendSmaller reports whether this browser sends photos and videos as
// smaller copies, which it does unless the member turned it off.
export function sendSmaller() {
    if (smaller === null) {
        try {
            smaller = localStorage.getItem(SMALLER) !== '0';
        } catch {
            // Storage can be unavailable; copies stay on.
            smaller = true;
        }
    }
    return smaller;
}

export function setSendSmaller(on) {
    smaller = on;
    try {
        if (on) localStorage.removeItem(SMALLER);
        else localStorage.setItem(SMALLER, '0');
    } catch {
        // As above; the choice lasts until the page reloads.
    }
}

// mayGoSmaller says whether a file the member picked may go as a smaller
// copy, which the den then takes even when the file itself is over its
// limit: an image or a video, by the type the browser gives it, or by its
// name for the formats some browsers don't name. The local service says
// when it can't make one.
export function mayGoSmaller(file) {
    return /^(image|video)\//.test(file.type || '') ||
        /\.(heic|heif|hif|tiff?|psd|jp2|j2k|jpx|mov|mp4|m4v|mkv|webm|3gp)$/i.test(file.name || '');
}

// versionURL is where the local service serves a version of a photo or
// video waiting to be sent.
export const versionURL = (denID, uploadID, which) => `/api/dens/${denID}/uploads/${uploadID}/versions/${which}`;

// comparable are the files on the message being written that have two
// versions to compare.
export const comparable = (files) => files.filter((f) => f.result?.versions);

// isVideo says whether a file's versions are a video's.
export const isVideo = (versions) => versions.full.type.startsWith('video/');

// describe labels a version with its pixels, a video's frame rate, and its
// size: 2560 × 1920 · 820 KB, or 1280 × 720 · 30 fps · 3.1 MB.
export const describe = (v) => `${v.width} × ${v.height} · ${v.fps ? `${Math.round(v.fps)} fps · ` : ''}${formatSize(v.size)}`;

// sentLabel names the version a file goes as, beside its size on its chip.
export const sentLabel = (versions) => (versions.sent === 'smaller' ? 'smaller' : 'full size');

// other is the version a file doesn't go as.
export const other = (which) => (which === 'smaller' ? 'full' : 'smaller');

// switchLabel is the comparison's button, which sends the other version.
export const switchLabel = (versions) => (versions.sent === 'smaller' ? 'Send full size instead' : 'Send smaller instead');

// switchBlocked says why a file can't go full size: it's over the den's
// limit, which limits names.
export function switchBlocked(versions, limits) {
    if (versions.sent !== 'smaller' || versions.full.fits) return '';
    return limits?.file_size ? `Its full size is over this den's ${formatSize(limits.file_size)} limit.` : "Its full size is over this den's limit.";
}

// othersToSwitch are the message's other files the same choice switches
// too: those not going as that version already, and, to full size, only
// those within the den's limit.
export function othersToSwitch(files, key, to) {
    return comparable(files).filter((f) => f.key !== key && !f.switching && f.result.versions.sent !== to &&
        (to !== 'full' || f.result.versions.full.fits));
}

// othersLabel names the others the choice can go to: "And the other
// photo", "And the other 2 videos", "And the other 3 files".
export function othersLabel(others) {
    const videos = others.filter((f) => isVideo(f.result.versions)).length;
    const kind = videos === others.length ? 'video' : videos === 0 ? 'photo' : 'file';
    return others.length === 1 ? `And the other ${kind}` : `And the other ${others.length} ${kind}s`;
}

// actualSize is a version's size in CSS pixels at 100%: each of its pixels
// on one of the screen's, whose ratio to CSS pixels is dpr.
export function actualSize(v, dpr) {
    const r = dpr > 0 ? dpr : 1;
    return { width: v.width / r, height: v.height / r };
}

// clampPan keeps a picture of size box, at offset in a view, covering the
// view where it's larger than the view, and centered where it's smaller.
export function clampPan(offset, box, view) {
    const axis = (o, b, v) => (b <= v ? (v - b) / 2 : Math.min(0, Math.max(v - b, o)));
    return { x: axis(offset.x, box.width, view.width), y: axis(offset.y, box.height, view.height) };
}

// panTo centers the view on the point at fractions fx and fy across a
// picture of size box, as far as the picture reaches.
export function panTo(fx, fy, box, view) {
    return clampPan({ x: view.width / 2 - fx * box.width, y: view.height / 2 - fy * box.height }, box, view);
}

// usage says how much of their space the member uses on the den, against
// the den's limit for each member.
export function usage(storage, limits) {
    if (!storage) return '';
    const used = formatSize(storage.used);
    return limits?.member_storage ? `You use ${used} of your ${formatSize(limits.member_storage)} on this den.` : `You use ${used} on this den.`;
}

// A video's copy (M5.4). The page names each upload with a key of its own,
// and follows it by that key, over a socket of its own (copier.js), while
// the copy is made and sent.

// progressKey names an upload for the page to follow it by.
export function progressKey() {
    return Array.from(crypto.getRandomValues(new Uint8Array(12)), (b) => b.toString(16).padStart(2, '0')).join('');
}

// progressLabel says how far an upload has come, once there's something to
// count: a video's copy being made, or the file being sent.
export function progressLabel(p) {
    const pct = `${Math.floor((p?.done || 0) * 100)}%`;
    if (p?.stage === 'copying') return `Smaller copy · ${pct}`;
    if (p?.stage === 'sending') return `Sending · ${pct}`;
    return '';
}

// formatTime writes a player's time: 0:04, 1:02.
function formatTime(seconds) {
    const s = Math.max(0, Math.floor(seconds || 0));
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

// How far a press may move and still count as a click, not a drag.
const CLICK_SLOP = 4;

// Compare shows a photo or video waiting to be sent in its two versions, so
// the member picks one: fitted to the window, as the viewer shows everyone,
// or at 100%, where they drag to look around, with the smaller copy scaled
// up to match, as someone who downloads it would see it. The switch flips
// between them in place, and holding Space shows the other one while it's
// held. A video's two play in place, kept at the same moment, so a flip
// shows the same frame; a version the browser can't play, such as a
// full size in HEVC in Firefox, leaves the other alone, and says why. files
// are the message's files with two versions, and ← and → move between
// them; onSwitch sends the files given as the version given.
export function Compare({ denID, files, index, limits, onIndex, onSwitch, onClose }) {
    const entry = files[index];
    const versions = entry.result.versions;
    const video = isVideo(versions);
    const [shown, setShown] = useState(versions.sent);
    const [held, setHeld] = useState(false);
    const [actual, setActual] = useState(false);
    const [pan, setPan] = useState({ x: 0, y: 0 });
    const [all, setAll] = useState(false);
    const [storage, setStorage] = useState(null);
    const [view, setView] = useState({ width: 0, height: 0 });
    const [loaded, setLoaded] = useState({});
    const [playing, setPlaying] = useState(false);
    const [time, setTime] = useState(0);
    const [length, setLength] = useState(0);
    // The versions the browser can't play: it refuses the file, or plays its
    // sound without a picture, as Chromium does with HEVC it can't decode.
    const [broken, setBroken] = useState({});
    const stage = useRef(null);
    const drag = useRef(null);
    const close = useRef(null);
    const players = useRef({});
    const at = useRef(index);
    at.current = index;

    // The stage's size is known before the first paint, so the photo
    // never shows at a guess.
    useLayoutEffect(() => {
        const box = stage.current;
        const measure = () => setView({ width: box.clientWidth, height: box.clientHeight });
        measure();
        const watch = new ResizeObserver(measure);
        watch.observe(box);
        return () => watch.disconnect();
    }, []);
    useEffect(() => {
        api.get(`/api/dens/${denID}/storage`).then(setStorage, () => {});
        close.current?.focus();
    }, []);

    useEffect(() => {
        // Space flips the versions while it's held, whatever has focus but
        // a checkbox, which it ticks.
        const space = (e) => e.key === ' ' && !(e.target instanceof HTMLInputElement && e.target.type === 'checkbox');
        const onDown = (e) => {
            if (e.key === 'Escape') onClose();
            if (space(e)) {
                e.preventDefault();
                if (!e.repeat) setHeld(true);
                return;
            }
            const delta = { ArrowLeft: -1, ArrowRight: 1 }[e.key];
            if (!delta || e.target instanceof HTMLInputElement) return;
            e.preventDefault();
            const next = step(at.current, files.length, delta);
            if (next !== at.current) onIndex(next);
        };
        const onUp = (e) => {
            if (!space(e)) return;
            e.preventDefault();
            setHeld(false);
        };
        const onBlur = () => setHeld(false);
        window.addEventListener('keydown', onDown, true);
        window.addEventListener('keyup', onUp, true);
        window.addEventListener('blur', onBlur);
        return () => {
            window.removeEventListener('keydown', onDown, true);
            window.removeEventListener('keyup', onUp, true);
            window.removeEventListener('blur', onBlur);
        };
    }, [files, onClose, onIndex]);

    // Another file starts as the version it goes as, from the middle.
    useEffect(() => {
        setShown(entry.result.versions.sent);
        setPan((p) => (actual ? panTo(0.5, 0.5, actualSize(entry.result.versions.full, window.devicePixelRatio), view) : p));
    }, [entry.key]);
    // A switched file is a new upload, whose versions load again.
    useEffect(() => {
        setLoaded({});
        setPlaying(false);
        setTime(0);
        setBroken({});
    }, [entry.result.id]);

    const full = versions.full;
    // A version the browser can't play shows the other instead.
    const flip = held ? other(shown) : shown;
    const visible = broken[flip] && !broken[other(flip)] ? other(flip) : flip;
    const fitted = fitBox(full.width, full.height, Math.max(64, view.width), Math.max(64, view.height));
    const box = actual ? actualSize(full, window.devicePixelRatio) : fitted;
    const offset = actual ? clampPan(pan, box, view) : { x: (view.width - box.width) / 2, y: (view.height - box.height) / 2 };

    // play starts a version. One the comparison pauses before it starts, as
    // a quick flip does, hasn't failed; one the browser won't play has.
    const play = (p) => p.play().catch((e) => e.name !== 'AbortError' && setPlaying(false));

    // The version that comes into view takes up where the other one is, and
    // plays on if it played, before the next key or click is handled.
    useLayoutEffect(() => {
        const now = players.current[visible];
        const before = players.current[other(visible)];
        if (!video || !now) return;
        if (before && Math.abs(now.currentTime - before.currentTime) > 0.01) now.currentTime = before.currentTime;
        if (playing) {
            before?.pause();
            play(now);
        }
    }, [visible]);

    // playing is what the member asked for, whichever version shows.
    function togglePlay() {
        const now = players.current[visible];
        if (!now) return;
        if (!playing) {
            setPlaying(true);
            play(now);
            return;
        }
        for (const p of Object.values(players.current)) p?.pause();
        setPlaying(false);
        // Paused, both show the same moment.
        const before = players.current[other(visible)];
        if (before) before.currentTime = now.currentTime;
    }
    function seek(t) {
        for (const p of Object.values(players.current)) {
            if (p) p.currentTime = t;
        }
        setTime(t);
    }

    function down(e) {
        if (e.button !== 0) return;
        e.currentTarget.setPointerCapture(e.pointerId);
        drag.current = { x: e.clientX, y: e.clientY, pan: offset, moved: false };
    }
    function move(e) {
        const d = drag.current;
        if (!d) return;
        const dx = e.clientX - d.x;
        const dy = e.clientY - d.y;
        d.moved = d.moved || Math.abs(dx) > CLICK_SLOP || Math.abs(dy) > CLICK_SLOP;
        if (actual && d.moved) setPan(clampPan({ x: d.pan.x + dx, y: d.pan.y + dy }, box, view));
    }
    function up(e) {
        const d = drag.current;
        drag.current = null;
        if (!d || d.moved) return;
        // A click on the fitted file looks at that spot at 100%, and one at
        // 100% fits it again. One beside it closes, as in the viewer.
        if (actual) {
            setActual(false);
            return;
        }
        const r = e.currentTarget.getBoundingClientRect();
        const fx = (e.clientX - r.left - offset.x) / box.width;
        const fy = (e.clientY - r.top - offset.y) / box.height;
        if (fx < 0 || fx > 1 || fy < 0 || fy > 1) {
            onClose();
            return;
        }
        setPan(panTo(fx, fy, actualSize(full, window.devicePixelRatio), view));
        setActual(true);
    }
    function showActual(on) {
        if (on && !actual) setPan(panTo(0.5, 0.5, actualSize(full, window.devicePixelRatio), view));
        setActual(on);
    }

    const to = other(versions.sent);
    const blocked = to === 'full' ? switchBlocked(versions, limits) : '';
    const others = othersToSwitch(files, entry.key, to);
    const several = files.length > 1;
    const side = 'btn btn-circle btn-ghost absolute top-1/2 z-10 -translate-y-1/2 text-3xl text-white hover:bg-white/15';
    const toggle = (on) => `btn btn-sm join-item ${on ? 'btn-active' : 'btn-ghost text-white'}`;
    const label = (which) => `${entry.result.name}, ${which === 'full' ? 'full size' : 'smaller copy'}`;
    const layer = (which) => `absolute inset-0 h-full w-full object-fill ${visible === which ? '' : 'opacity-0'}`;
    return (
        <div role="dialog" aria-modal="true" aria-label={`Compare the two versions of ${entry.result.name}`}
            class="fixed inset-0 z-50 flex flex-col gap-3 bg-black/85 p-4" onClick={(e) => e.target === e.currentTarget && onClose()}>
            <div ref={stage} class={`relative min-h-0 flex-1 overflow-hidden ${actual ? 'cursor-grab active:cursor-grabbing' : ''}`}
                onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={() => { drag.current = null; }}>
                <div key={entry.result.id} class={`absolute select-none overflow-hidden rounded ${actual ? '' : 'cursor-zoom-in'}`}
                    style={{ left: `${offset.x}px`, top: `${offset.y}px`, width: `${box.width}px`, height: `${box.height}px` }}>
                    {['smaller', 'full'].map((which) => (video ? (
                        <video key={which} ref={(el) => { players.current[which] = el; }} src={versionURL(denID, entry.result.id, which)}
                            aria-label={visible === which ? label(which) : undefined} preload="auto" playsInline loop
                            muted={visible !== which} class={layer(which)}
                            onLoadedData={(e) => {
                                setLoaded((l) => ({ ...l, [which]: true }));
                                if (!e.currentTarget.videoWidth) setBroken((b) => ({ ...b, [which]: true }));
                            }}
                            onError={() => setBroken((b) => ({ ...b, [which]: true }))}
                            onDurationChange={(e) => which === 'smaller' && setLength(e.currentTarget.duration || 0)}
                            onTimeUpdate={(e) => visible === which && setTime(e.currentTarget.currentTime)}></video>
                    ) : (
                        <img key={which} src={versionURL(denID, entry.result.id, which)} alt={visible === which ? label(which) : ''}
                            draggable={false} onLoad={() => setLoaded((l) => ({ ...l, [which]: true }))} class={layer(which)} />
                    )))}
                    {!loaded[visible] && (
                        <span class="absolute inset-0 flex items-center justify-center text-white">
                            <span class="loading loading-spinner loading-md" aria-label="Loading"></span>
                        </span>
                    )}
                </div>
                {several && index > 0 && (
                    <button type="button" class={`${side} left-0`} aria-label="Previous" onPointerDown={(e) => e.stopPropagation()}
                        onClick={() => onIndex(index - 1)}>‹</button>
                )}
                {several && index < files.length - 1 && (
                    <button type="button" class={`${side} right-0`} aria-label="Next" onPointerDown={(e) => e.stopPropagation()}
                        onClick={() => onIndex(index + 1)}>›</button>
                )}
            </div>
            <div class="flex flex-col items-center gap-2 text-sm text-white">
                {video && (
                    <div class="flex w-full max-w-xl items-center gap-2">
                        <button type="button" class="btn btn-ghost btn-sm btn-square text-white" onClick={togglePlay}
                            aria-label={playing ? 'Pause' : 'Play'}>
                            {playing ? (
                                <svg viewBox="0 0 16 16" class="h-4 w-4" fill="currentColor" aria-hidden="true"><path d="M4 3h3v10H4zM9 3h3v10H9z" /></svg>
                            ) : (
                                <svg viewBox="0 0 16 16" class="h-4 w-4" fill="currentColor" aria-hidden="true"><path d="M4 2.5v11l9-5.5z" /></svg>
                            )}
                        </button>
                        <input type="range" class="range range-xs flex-1" min="0" max={length || 0} step="any" value={time}
                            aria-label="Where in the video" onInput={(e) => seek(Number(e.currentTarget.value))} />
                        <span class="shrink-0 cursor-default select-none tabular-nums text-white/60">{formatTime(time)} / {formatTime(length)}</span>
                    </div>
                )}
                <div class="flex max-w-full flex-wrap items-center justify-center gap-2">
                    {several && <span class="shrink-0 cursor-default select-none text-white/60">{index + 1} of {files.length}</span>}
                    <span class="max-w-60 truncate" title={entry.result.name}>{entry.result.name}</span>
                    <div role="group" aria-label="Version shown" class="join">
                        {['smaller', 'full'].map((which) => (
                            <button key={which} type="button" class={toggle(visible === which)} aria-pressed={visible === which}
                                disabled={!!broken[which]} onClick={() => setShown(which)}>
                                {which === 'smaller' ? 'Smaller' : 'Full size'}
                                <span class="font-normal opacity-70">{describe(versions[which])}</span>
                                {versions.sent === which && <span class="badge badge-primary badge-xs">sending</span>}
                            </button>
                        ))}
                    </div>
                    <div role="group" aria-label="Zoom" class="join">
                        <button type="button" class={toggle(!actual)} aria-pressed={!actual} onClick={() => showActual(false)}>Fit</button>
                        <button type="button" class={toggle(actual)} aria-pressed={actual} onClick={() => showActual(true)}>100%</button>
                    </div>
                </div>
                <div class="flex max-w-full flex-wrap items-center justify-center gap-x-3 gap-y-1">
                    <span class="cursor-default select-none text-white/60">
                        {broken.full && broken.smaller ? "This browser can't play either version."
                            : broken.full ? "This browser can't play the full size, so only the smaller copy shows."
                                : broken.smaller ? "This browser can't play the smaller copy, which is AV1, so only the full size shows."
                                    : 'Hold Space to see the other one.'}{' '}
                        {usage(storage, limits)}
                    </span>
                    <button type="button" class="btn btn-primary btn-sm" disabled={!!blocked || entry.switching}
                        onClick={() => onSwitch([entry, ...(all ? others : [])], to)}>
                        {entry.switching && <span class="loading loading-spinner loading-xs"></span>}
                        {switchLabel(versions)}
                    </button>
                    {others.length > 0 && !blocked && (
                        <label class="label cursor-pointer gap-2 text-white">
                            <input type="checkbox" class="checkbox checkbox-sm border-white/60" checked={all} onChange={(e) => setAll(e.currentTarget.checked)} />
                            {othersLabel(others)}
                        </label>
                    )}
                    <button ref={close} type="button" class="btn btn-ghost btn-sm text-white" onClick={onClose}>Close</button>
                </div>
                {(blocked || entry.switchError) && (
                    <p role="alert" class="text-warning">{entry.switchError || blocked}</p>
                )}
            </div>
        </div>
    );
}
