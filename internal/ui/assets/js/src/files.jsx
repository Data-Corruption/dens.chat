// Files: uploading them, showing them in messages, and the viewer that
// shows an image whole.
//
// Every file comes through the local service, which serves only real
// images as images and everything else as a download. A file's name comes
// from a den and is shown as text, never used as anything else.

import { useEffect, useRef, useState } from 'preact/hooks';
import { APIError } from './api.js';

// Previews fit these boxes in the message list; one image gets more room
// than several.
const ONE = { w: 400, h: 300 };
const SEVERAL = { w: 240, h: 180 };
// The smallest side a preview keeps, so a sliver of an image can still be
// clicked; anything past the box is cropped from the preview.
const MIN_SIDE = 48;

export const MAX_ATTACHMENTS = 10;

export function formatSize(bytes) {
    if (bytes < 1024) return `${bytes} B`;
    const units = ['KB', 'MB', 'GB'];
    let v = bytes / 1024;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v < 10 ? v.toFixed(1).replace(/\.0$/, '') : Math.round(v)} ${units[i]}`;
}

// fitBox scales an image of w×h to fit in maxW×maxH without enlarging it,
// keeping each side at least MIN_SIDE.
export function fitBox(w, h, maxW, maxH) {
    if (!(w > 0 && h > 0)) return { width: maxW, height: maxH };
    const s = Math.min(1, maxW / w, maxH / h);
    return {
        width: Math.min(maxW, Math.max(MIN_SIDE, Math.round(w * s))),
        height: Math.min(maxH, Math.max(MIN_SIDE, Math.round(h * s))),
    };
}

export const fileURL = (denID, id) => `/api/dens/${denID}/files/${id}`;
export const thumbURL = (denID, id) => `/api/dens/${denID}/files/${id}/thumb`;
export const downloadURL = (denID, f) => `/api/dens/${denID}/files/${f.id}?download=${encodeURIComponent(f.name)}`;

// upload sends a file to a den through the local service, which takes an
// image's metadata out on the way. It reports progress from 0 to 1, and
// returns the upload's promise and a way to stop it.
export function upload(denID, file, name, onProgress) {
    const xhr = new XMLHttpRequest();
    const done = new Promise((resolve, reject) => {
        xhr.open('POST', `/api/dens/${denID}/uploads`);
        xhr.setRequestHeader('Content-Type', 'application/octet-stream');
        xhr.setRequestHeader('Dens-Filename', encodeURIComponent(name));
        xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
        xhr.onload = () => {
            let data = null;
            try {
                data = JSON.parse(xhr.responseText);
            } catch {
                // An error that isn't JSON is reported below.
            }
            if (xhr.status >= 200 && xhr.status < 300 && data) resolve(data);
            else reject(new APIError(data?.error || `The upload failed (HTTP ${xhr.status}).`, xhr.status));
        };
        xhr.onerror = () => reject(new APIError("The upload failed. Is Dens still running on this computer?", 0));
        xhr.onabort = () => reject(new APIError('Upload stopped.', 0));
        xhr.send(file);
    });
    return { done, abort: () => xhr.abort() };
}

// Attachments shows a message's files: images as previews that open the
// viewer, anything else as a card to download.
export function Attachments({ denID, files, onOpen }) {
    const images = files.filter((f) => f.thumb);
    const others = files.filter((f) => !f.thumb);
    const box = images.length > 1 ? SEVERAL : ONE;
    return (
        <div class="mt-1 flex flex-col gap-1">
            {images.length > 0 && (
                <div class="flex flex-wrap gap-1">
                    {images.map((f) => <ImageTile key={f.id} denID={denID} file={f} box={box} onOpen={() => onOpen(f)} />)}
                </div>
            )}
            {others.map((f) => <FileCard key={f.id} denID={denID} file={f} />)}
        </div>
    );
}

// ImageTile takes its final size before the preview loads, from the size
// the den stated, so nothing below it moves when it arrives.
function ImageTile({ denID, file, box, onOpen }) {
    const size = fitBox(file.width, file.height, box.w, box.h);
    return (
        <button
            type="button"
            class="relative block max-w-full overflow-hidden rounded bg-base-300"
            style={{ width: `${size.width}px`, aspectRatio: `${size.width} / ${size.height}` }}
            onClick={onOpen}
            aria-label={`Open ${file.name}`}
            title={file.name}
        >
            <img src={thumbURL(denID, file.id)} alt="" width={size.width} height={size.height} loading="lazy" draggable={false}
                class="h-full w-full object-cover" />
            {file.animated && <span class="badge badge-neutral badge-sm absolute bottom-1 left-1 select-none opacity-90">GIF</span>}
        </button>
    );
}

function FileCard({ denID, file }) {
    return (
        <div class="flex w-full max-w-sm items-center gap-3 rounded border border-base-300 bg-base-200 p-2">
            <FileIcon />
            <div class="min-w-0 flex-1">
                <p class="truncate text-sm font-medium" title={file.name}>{file.name}</p>
                <p class="cursor-default select-none text-xs text-base-content/60">
                    {formatSize(file.size)}
                    {file.width > 0 && !file.thumb && ' · too large to preview'}
                </p>
            </div>
            <a class="btn btn-ghost btn-sm" href={downloadURL(denID, file)} download={file.name}>Download</a>
        </div>
    );
}

function FileIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-6 w-6 shrink-0 text-base-content/60" fill="none" stroke="currentColor" stroke-width="1.2" aria-hidden="true">
            <path d="M4 1.5h5.5L13 5v9.5H4z" />
            <path d="M9.5 1.5V5H13" />
        </svg>
    );
}

// Viewer shows an image whole, over everything, fitted to the window. The
// preview shows at once and the original replaces it as it loads.
export function Viewer({ denID, file, onClose }) {
    const [win, setWin] = useState({ w: window.innerWidth, h: window.innerHeight });
    const close = useRef(null);
    useEffect(() => {
        const onKey = (e) => e.key === 'Escape' && onClose();
        const onResize = () => setWin({ w: window.innerWidth, h: window.innerHeight });
        window.addEventListener('keydown', onKey);
        window.addEventListener('resize', onResize);
        close.current?.focus();
        return () => {
            window.removeEventListener('keydown', onKey);
            window.removeEventListener('resize', onResize);
        };
    }, []);
    const size = fitBox(file.width, file.height, Math.max(64, win.w - 32), Math.max(64, win.h - 112));
    const box = { width: `${size.width}px`, height: `${size.height}px` };
    return (
        <div role="dialog" aria-label={file.name} class="fixed inset-0 z-50 flex flex-col items-center justify-center gap-3 bg-black/85 p-4"
            onClick={(e) => e.target === e.currentTarget && onClose()}>
            <div class="relative overflow-hidden rounded" style={box}>
                {file.thumb && <img src={thumbURL(denID, file.id)} alt="" class="absolute inset-0 h-full w-full object-contain" draggable={false} />}
                <img src={fileURL(denID, file.id)} alt={file.name} width={size.width} height={size.height} class="relative h-full w-full object-contain" />
            </div>
            <div class="flex max-w-full items-center gap-3 text-sm text-white">
                <span class="truncate" title={file.name}>{file.name}</span>
                <span class="shrink-0 cursor-default select-none text-white/60">{formatSize(file.size)}</span>
                <a class="btn btn-sm" href={downloadURL(denID, file)} download={file.name}>Download</a>
                <button ref={close} type="button" class="btn btn-ghost btn-sm text-white" onClick={onClose}>Close</button>
            </div>
        </div>
    );
}

// Thumb draws a picked image small, for the composer: the file is still on
// this computer, and drawing it needs no address the page's policy would
// refuse.
export function Thumb({ file, size = 56 }) {
    const canvas = useRef(null);
    const [failed, setFailed] = useState(false);
    useEffect(() => {
        let bitmap = null;
        let stop = false;
        createImageBitmap(file).then(
            (b) => {
                bitmap = b;
                const c = canvas.current;
                if (stop || !c) return;
                const dpr = window.devicePixelRatio || 1;
                c.width = c.height = Math.round(size * dpr);
                const scale = Math.max(c.width / b.width, c.height / b.height);
                const ctx = c.getContext('2d');
                ctx.drawImage(b, (c.width - b.width * scale) / 2, (c.height - b.height * scale) / 2, b.width * scale, b.height * scale);
            },
            () => setFailed(true),
        );
        return () => {
            stop = true;
            bitmap?.close();
        };
    }, [file]);
    if (failed) return <FileIcon />;
    return <canvas ref={canvas} class="shrink-0 rounded bg-base-300" style={{ width: `${size}px`, height: `${size}px` }} aria-hidden="true"></canvas>;
}

export const isImageFile = (file) => /^image\/(jpeg|png|gif|webp)$/.test(file.type);
