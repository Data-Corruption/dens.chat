// Cropping a picture for a profile: the member drags the image around a
// frame and zooms it, and the page draws what the frame shows into a new
// image. Drawing it anew leaves every bit of the original's metadata
// behind; the local service checks it on the way to the den all the same.

import { useEffect, useRef, useState } from 'preact/hooks';
import { Dialog } from './manage.jsx';

// The frame each kind of picture is cut to, as it shows here and as it's
// saved.
export const SHAPES = {
    avatar: { frame: [256, 256], out: [512, 512], round: true, title: 'Your picture' },
    banner: { frame: [384, 128], out: [1200, 400], round: false, title: 'Your banner' },
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

// CropDialog lets the member frame a picked image, and hands back the
// framed part as a new image file.
export function CropDialog({ file, shape, busy, error, onCrop, onClose }) {
    const spec = SHAPES[shape];
    const [fw, fh] = spec.frame;
    const canvas = useRef(null);
    const drag = useRef(null);
    const [bitmap, setBitmap] = useState(null);
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
                        <input type="range" class="range range-sm flex-1" min="1" max={MAX_ZOOM} step="0.01" value={zoom}
                            disabled={!bitmap} onInput={(e) => zoomTo(Number(e.currentTarget.value))} />
                    </label>
                    <p class="cursor-default select-none text-xs text-base-content/60">Drag the picture to choose what shows.</p>
                </div>
            )}
            {error && <p role="alert" class="text-sm text-error">{error}</p>}
            <div class="flex gap-2">
                <button type="button" class="btn btn-primary btn-sm" disabled={!bitmap || busy} onClick={save}>
                    {busy && <span class="loading loading-spinner loading-sm"></span>}
                    Save
                </button>
                <button type="button" class="btn btn-ghost btn-sm" onClick={onClose}>Cancel</button>
            </div>
        </Dialog>
    );
}
