// Cropping a picture for a profile: the member drags the image around a
// frame and zooms it, and the page draws what the frame shows into a new
// image. Drawing it anew leaves every bit of the original's metadata
// behind; the local service checks it on the way to the den all the same.
// An animated GIF or WebP is framed by its first frame, and goes as it is,
// with the part framed, for the local service to make each frame again,
// which a canvas can't (M15.1).

import { useEffect, useRef, useState } from 'preact/hooks';
import { Dialog } from './manage.jsx';

// The frame each kind of picture is cut to, as it shows here and as it's
// saved, whether it's square, and an animated one at most, with the most it
// may take.
export const SHAPES = {
    avatar: { frame: [256, 256], out: [512, 512], round: true, square: true, title: 'Your picture', moving: '256 × 256 pixels and 2 MB' },
    banner: { frame: [384, 128], out: [1200, 400], round: false, title: 'Your banner', moving: '600 × 200 pixels and 4 MB' },
    // An event's banner (M17.4), cut as a profile's is.
    event: { frame: [384, 128], out: [1200, 400], round: false, title: "The event's banner", moving: '600 × 200 pixels and 4 MB' },
    // A badge's picture (M17.5), square at an emoji's size, which an animated
    // one is made at too.
    badge: { frame: [256, 256], out: [128, 128], round: false, square: true, title: "The badge's picture", moving: '128 × 128 pixels and 512 KB' },
};
const MAX_ZOOM = 5;

// coverScale is the scale at which an image just covers the frame.
export function coverScale(iw, ih, fw, fh) {
    return Math.max(fw / iw, fh / ih);
}

// clampOffset keeps an image covering the frame: o is where its top left
// corner sits, relative to the frame's, which it may not pass inside.
export function clampOffset(o, iw, ih, fw, fh, scale) {
    return {
        x: Math.min(0, Math.max(fw - iw * scale, o.x)),
        y: Math.min(0, Math.max(fh - ih * scale, o.y)),
    };
}

// zoomAt moves an image as its scale goes from one value to another, so
// the point (px, py) of the frame stays over the same spot of the image.
export function zoomAt(o, from, to, px, py) {
    return { x: px - ((px - o.x) * to) / from, y: py - ((py - o.y) * to) / from };
}

// sourceRect is the part of the image the frame shows, in its pixels.
export function sourceRect(o, fw, fh, scale) {
    return { x: -o.x / scale, y: -o.y / scale, w: fw / scale, h: fh / scale };
}

// wholeRect rounds the part of an image of iw × ih the frame shows to
// whole pixels on the image, square where the picture is, for an animated
// one's crop.
export function wholeRect(r, iw, ih, square) {
    let w = Math.max(1, Math.min(iw, Math.round(r.w)));
    let h = Math.max(1, Math.min(ih, Math.round(r.h)));
    if (square) w = h = Math.min(w, h);
    return { x: Math.min(Math.max(0, Math.round(r.x)), iw - w), y: Math.min(Math.max(0, Math.round(r.y)), ih - h), w, h };
}

// animatedImage says whether a file is an animated GIF, of more than one
// frame, or an animated WebP, by its header, as the local service reads
// them. An animated PNG, rare among pictures, frames as a still of its
// first frame.
export function animatedImage(b) {
    const has = (at, s) => [...s].every((c, i) => b[at + i] === c.charCodeAt(0));
    if (has(0, 'RIFF') && has(8, 'WEBP')) return has(12, 'VP8X') && b.length > 20 && (b[20] & 2) !== 0;
    if (!has(0, 'GIF87a') && !has(0, 'GIF89a') || b.length < 13) return false;
    let p = 13 + (b[10] & 0x80 ? 3 << ((b[10] & 7) + 1) : 0);
    // Sub-blocks of data run to the one of no length.
    const blocks = () => {
        while (p < b.length && b[p] !== 0) p += b[p] + 1;
        p++;
    };
    let frames = 0;
    while (p < b.length) {
        if (b[p] === 0x21) {
            p += 2;
            blocks();
        } else if (b[p] === 0x2c && p + 10 < b.length) {
            const flags = b[p + 9];
            p += 11 + (flags & 0x80 ? 3 << ((flags & 7) + 1) : 0);
            blocks();
            if (p <= b.length && ++frames > 1) return true;
        } else {
            return false;
        }
    }
    return false;
}

// CropDialog lets the member frame a picked image, and hands back the
// framed part as a new image file, or an animated one as it is, with the
// part framed in its pixels.
export function CropDialog({ file, shape, busy, error, onCrop, onClose }) {
    const spec = SHAPES[shape];
    const [fw, fh] = spec.frame;
    const canvas = useRef(null);
    const drag = useRef(null);
    const [bitmap, setBitmap] = useState(null);
    // Whether the file is animated, null until its bytes are read.
    const [animated, setAnimated] = useState(null);
    const [failed, setFailed] = useState('');
    const [zoom, setZoom] = useState(1);
    const [offset, setOffset] = useState({ x: 0, y: 0 });

    useEffect(() => {
        let b = null;
        createImageBitmap(file).then(
            (loaded) => {
                b = loaded;
                const s = coverScale(loaded.width, loaded.height, fw, fh);
                setOffset({ x: (fw - loaded.width * s) / 2, y: (fh - loaded.height * s) / 2 });
                setBitmap(loaded);
            },
            () => setFailed("Your browser can't read this image. Try a JPEG, PNG, GIF or WebP."),
        );
        setAnimated(null);
        file.arrayBuffer().then((data) => setAnimated(animatedImage(new Uint8Array(data))), () => setAnimated(false));
        return () => b?.close();
    }, [file]);

    const base = bitmap ? coverScale(bitmap.width, bitmap.height, fw, fh) : 1;
    const scale = base * zoom;

    useEffect(() => {
        const c = canvas.current;
        if (!c || !bitmap) return;
        const dpr = window.devicePixelRatio || 1;
        c.width = Math.round(fw * dpr);
        c.height = Math.round(fh * dpr);
        const ctx = c.getContext('2d');
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        ctx.imageSmoothingQuality = 'high';
        ctx.clearRect(0, 0, fw, fh);
        ctx.drawImage(bitmap, offset.x, offset.y, bitmap.width * scale, bitmap.height * scale);
        if (spec.round) {
            // Dim what the circle leaves out.
            ctx.fillStyle = 'rgba(0, 0, 0, 0.5)';
            ctx.beginPath();
            ctx.rect(0, 0, fw, fh);
            ctx.arc(fw / 2, fh / 2, fw / 2, 0, Math.PI * 2, true);
            ctx.fill();
        }
    }, [bitmap, offset, scale]);

    function place(o, s = scale) {
        setOffset(clampOffset(o, bitmap.width, bitmap.height, fw, fh, s));
    }

    function zoomTo(next, px = fw / 2, py = fh / 2) {
        if (!bitmap) return;
        next = Math.min(MAX_ZOOM, Math.max(1, next));
        const to = base * next;
        setZoom(next);
        place(zoomAt(offset, scale, to, px, py), to);
    }

    function pointerDown(e) {
        if (!bitmap) return;
        try {
            // Keeps the drag going when the pointer leaves the frame.
            e.currentTarget.setPointerCapture(e.pointerId);
        } catch {
            // A pointer already gone can't be captured; the drag still works inside.
        }
        drag.current = { x: e.clientX, y: e.clientY, o: offset };
    }

    function pointerMove(e) {
        const d = drag.current;
        if (d) place({ x: d.o.x + e.clientX - d.x, y: d.o.y + e.clientY - d.y });
    }

    function wheel(e) {
        e.preventDefault();
        const rect = e.currentTarget.getBoundingClientRect();
        zoomTo(zoom * Math.exp(-e.deltaY * 0.002), ((e.clientX - rect.left) * fw) / rect.width, ((e.clientY - rect.top) * fh) / rect.height);
    }

    function key(e) {
        const step = e.shiftKey ? 40 : 10;
        const moves = { ArrowLeft: [step, 0], ArrowRight: [-step, 0], ArrowUp: [0, step], ArrowDown: [0, -step] };
        if (moves[e.key]) {
            e.preventDefault();
            place({ x: offset.x + moves[e.key][0], y: offset.y + moves[e.key][1] });
        } else if (e.key === '+' || e.key === '=') {
            zoomTo(zoom * 1.1);
        } else if (e.key === '-') {
            zoomTo(zoom / 1.1);
        }
    }

    function save() {
        if (animated) {
            onCrop(file, wholeRect(sourceRect(offset, fw, fh, scale), bitmap.width, bitmap.height, !!spec.square));
            return;
        }
        const [ow, oh] = spec.out;
        const out = document.createElement('canvas');
        out.width = ow;
        out.height = oh;
        const ctx = out.getContext('2d');
        ctx.imageSmoothingQuality = 'high';
        const r = sourceRect(offset, fw, fh, scale);
        ctx.drawImage(bitmap, r.x, r.y, r.w, r.h, 0, 0, ow, oh);
        // WebP keeps transparency and stays small; a browser that can't
        // write it gives PNG.
        out.toBlob((blob) => blob && onCrop(blob), 'image/webp', 0.9);
    }

    return (
        <Dialog title={spec.title} onClose={onClose}>
            {failed ? (
                <p role="alert" class="text-sm text-error">{failed}</p>
            ) : (
                <div class="flex flex-col items-center gap-3">
                    <canvas
                        ref={canvas}
                        tabIndex={0}
                        class="max-w-full cursor-grab touch-none select-none rounded bg-base-300 active:cursor-grabbing"
                        style={{ width: `${fw}px`, aspectRatio: `${fw} / ${fh}` }}
                        aria-label="Drag to move the picture, scroll or use + and - to zoom"
                        onPointerDown={pointerDown}
                        onPointerMove={pointerMove}
                        onPointerUp={() => (drag.current = null)}
                        onPointerCancel={() => (drag.current = null)}
                        onWheel={wheel}
                        onKeyDown={key}
                    ></canvas>
                    <label class="flex w-full max-w-xs items-center gap-2 text-sm">
                        <span class="cursor-default select-none">Zoom</span>
                        <input type="range" class="range range-md flex-1" min="1" max={MAX_ZOOM} step="0.01" value={zoom}
                            disabled={!bitmap} onInput={(e) => zoomTo(Number(e.currentTarget.value))} />
                    </label>
                    <p class="cursor-default select-none text-xs text-base-content/60">Drag the picture to choose what shows.</p>
                    {animated && (
                        <p class="text-xs text-base-content/60">It moves, and stays moving, as a GIF of at most {spec.moving}.</p>
                    )}
                </div>
            )}
            {error && <p role="alert" class="text-sm text-error">{error}</p>}
            <div class="flex gap-2">
                <button type="button" class="btn btn-primary btn-sm" disabled={!bitmap || animated === null || busy} onClick={save}>
                    {busy && <span class="loading loading-spinner loading-sm"></span>}
                    Save
                </button>
                <button type="button" class="btn btn-ghost btn-sm" onClick={onClose}>Cancel</button>
            </div>
        </Dialog>
    );
}
