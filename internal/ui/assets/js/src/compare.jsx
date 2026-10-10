// Smaller copies (M5): the setting, and the viewer's comparison of a
// photo's two versions before it's sent.
//
// A photo added to a message goes as a smaller copy unless the member sends
// it full size. The local service makes both versions as the photo is
// added, uploads the one to send, and keeps both until the message goes, so
// the comparison shows them from there, and switching uploads the other.

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
// limit: an image, by the type the browser gives it, or by its name for
// the photo formats some browsers don't name.
export function mayGoSmaller(file) {
    return /^image\//.test(file.type || '') || /\.(heic|heif|hif|tiff?|psd|jp2|j2k|jpx)$/i.test(file.name || '');
}

// versionURL is where the local service serves a version of a photo
// waiting to be sent.
export const versionURL = (denID, uploadID, which) => `/api/dens/${denID}/uploads/${uploadID}/versions/${which}`;

// comparable are the files on the message being written that have two
// versions to compare.
export const comparable = (files) => files.filter((f) => f.result?.versions);

// describe labels a version with its pixels and size: 2560 × 1920 · 820 KB.
export const describe = (v) => `${v.width} × ${v.height} · ${formatSize(v.size)}`;

// sentLabel names the version a photo goes as, beside its size on its
// chip.
export const sentLabel = (versions) => (versions.sent === 'smaller' ? 'smaller' : 'full size');

// other is the version a photo doesn't go as.
export const other = (which) => (which === 'smaller' ? 'full' : 'smaller');

// switchLabel is the comparison's button, which sends the other version.
export const switchLabel = (versions) => (versions.sent === 'smaller' ? 'Send full size instead' : 'Send smaller instead');

// switchBlocked says why a photo can't go full size: it's over the den's
// limit, which limits names.
export function switchBlocked(versions, limits) {
    if (versions.sent !== 'smaller' || versions.full.fits) return '';
    return limits?.file_size ? `Its full size is over this den's ${formatSize(limits.file_size)} limit.` : "Its full size is over this den's limit.";
}

// othersToSwitch are the message's other photos the same choice switches
// too: those not going as that version already, and, to full size, only
// those within the den's limit.
export function othersToSwitch(files, key, to) {
    return comparable(files).filter((f) => f.key !== key && !f.switching && f.result.versions.sent !== to &&
        (to !== 'full' || f.result.versions.full.fits));
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

// How far a press may move and still count as a click, not a drag.
const CLICK_SLOP = 4;

// Compare shows a photo waiting to be sent in its two versions, so the
// member picks one: fitted to the window, as the viewer shows everyone, or
// at 100%, where they drag to look around, with the smaller copy scaled up
// to match, as someone who downloads it would see it. The switch flips
// between them in place, and holding Space shows the other one while it's
// held. files are the message's photos with two versions, and ← and → move
// between them; onSwitch sends the photos given as the version given.
export function Compare({ denID, files, index, limits, onIndex, onSwitch, onClose }) {
    const entry = files[index];
    const versions = entry.result.versions;
    const [shown, setShown] = useState(versions.sent);
    const [held, setHeld] = useState(false);
    const [actual, setActual] = useState(false);
    const [pan, setPan] = useState({ x: 0, y: 0 });
    const [all, setAll] = useState(false);
    const [storage, setStorage] = useState(null);
    const [view, setView] = useState({ width: 0, height: 0 });
    const [loaded, setLoaded] = useState({});
    const stage = useRef(null);
    const drag = useRef(null);
    const close = useRef(null);
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
        const space = (e) => e.key === ' ' && !(e.target instanceof HTMLInputElement);
        const onDown = (e) => {
            if (e.key === 'Escape') onClose();
            if (space(e)) {
                e.preventDefault();
                if (!e.repeat) setHeld(true);
                return;
            }
            const delta = { ArrowLeft: -1, ArrowRight: 1 }[e.key];
            if (!delta) return;
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

    // Another photo starts as the version it goes as, from the middle.
    useEffect(() => {
        setShown(entry.result.versions.sent);
        setPan((p) => (actual ? panTo(0.5, 0.5, actualSize(entry.result.versions.full, window.devicePixelRatio), view) : p));
    }, [entry.key]);
    // A switched photo is a new upload, whose versions load again.
    useEffect(() => setLoaded({}), [entry.result.id]);

    const full = versions.full;
    const visible = held ? other(shown) : shown;
    const fitted = fitBox(full.width, full.height, Math.max(64, view.width), Math.max(64, view.height));
    const box = actual ? actualSize(full, window.devicePixelRatio) : fitted;
    const offset = actual ? clampPan(pan, box, view) : { x: (view.width - box.width) / 2, y: (view.height - box.height) / 2 };

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
        // A click on the fitted photo looks at that spot at 100%, and one
        // at 100% fits it again. One beside it closes, as in the viewer.
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
    return (
        <div role="dialog" aria-modal="true" aria-label={`Compare the two versions of ${entry.result.name}`}
            class="fixed inset-0 z-50 flex flex-col gap-3 bg-black/85 p-4" onClick={(e) => e.target === e.currentTarget && onClose()}>
            <div ref={stage} class={`relative min-h-0 flex-1 overflow-hidden ${actual ? 'cursor-grab active:cursor-grabbing' : ''}`}
                onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={() => { drag.current = null; }}>
                <div key={entry.result.id} class={`absolute select-none overflow-hidden rounded ${actual ? '' : 'cursor-zoom-in'}`}
                    style={{ left: `${offset.x}px`, top: `${offset.y}px`, width: `${box.width}px`, height: `${box.height}px` }}>
                    {['smaller', 'full'].map((which) => (
                        <img key={which} src={versionURL(denID, entry.result.id, which)} alt={visible === which ? `${entry.result.name}, ${which === 'full' ? 'full size' : 'smaller copy'}` : ''}
                            draggable={false} onLoad={() => setLoaded((l) => ({ ...l, [which]: true }))}
                            class={`absolute inset-0 h-full w-full object-fill ${visible === which ? '' : 'opacity-0'}`} />
                    ))}
                    {!loaded[visible] && (
                        <span class="absolute inset-0 flex items-center justify-center text-white">
                            <span class="loading loading-spinner loading-md" aria-label="Loading"></span>
                        </span>
                    )}
                </div>
                {several && index > 0 && (
                    <button type="button" class={`${side} left-0`} aria-label="Previous photo" onPointerDown={(e) => e.stopPropagation()}
                        onClick={() => onIndex(index - 1)}>‹</button>
                )}
                {several && index < files.length - 1 && (
                    <button type="button" class={`${side} right-0`} aria-label="Next photo" onPointerDown={(e) => e.stopPropagation()}
                        onClick={() => onIndex(index + 1)}>›</button>
                )}
            </div>
            <div class="flex flex-col items-center gap-2 text-sm text-white">
                <div class="flex max-w-full flex-wrap items-center justify-center gap-2">
                    {several && <span class="shrink-0 cursor-default select-none text-white/60">{index + 1} of {files.length}</span>}
                    <span class="max-w-60 truncate" title={entry.result.name}>{entry.result.name}</span>
                    <div role="group" aria-label="Version shown" class="join">
                        {['smaller', 'full'].map((which) => (
                            <button key={which} type="button" class={toggle(visible === which)} aria-pressed={visible === which}
                                onClick={() => setShown(which)}>
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
                    <span class="cursor-default select-none text-white/60">Hold Space to see the other one. {usage(storage, limits)}</span>
                    <button type="button" class="btn btn-primary btn-sm" disabled={!!blocked || entry.switching}
                        onClick={() => onSwitch([entry, ...(all ? others : [])], to)}>
                        {entry.switching && <span class="loading loading-spinner loading-xs"></span>}
                        {switchLabel(versions)}
                    </button>
                    {others.length > 0 && !blocked && (
                        <label class="label cursor-pointer gap-2 text-white">
                            <input type="checkbox" class="checkbox checkbox-sm border-white/60" checked={all} onChange={(e) => setAll(e.currentTarget.checked)} />
                            {others.length === 1 ? 'And the other photo' : `And the other ${others.length} photos`}
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
