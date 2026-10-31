// Files: uploading them, showing them in messages, and the viewer that
// shows an image whole.
//
// Every file comes through the local service, which serves only real
// images, and video and audio in containers Dens writes, as what they are,
// and everything else as a download. A file's name comes from a den and is
// shown as text, never used as anything else.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { APIError, pageHeaders, reloadIfStale } from './api.js';
import { useBackdrop } from './components.jsx';
import { useLayer, useNarrow } from './nav.js';
import { canCopy, followUpload } from './copier.js';
import { dismisses } from './touch.js';
import { fitted, isFitted, maxScale, useZoom } from './zoom.js';

// Previews fit these boxes in the message list; one image gets more room
// than several.
const ONE = { w: 400, h: 300 };
const SEVERAL = { w: 240, h: 180 };
// The smallest side a preview keeps, so a sliver of an image can still be
// clicked; anything past the box is cropped from the preview.
const MIN_SIDE = 48;

export const MAX_ATTACHMENTS = 10;

// formatDuration writes milliseconds as a player shows them: 0:04, 1:02:03.
export function formatDuration(ms) {
    const total = Math.floor(ms / 1000);
    const h = Math.floor(total / 3600);
    const m = Math.floor((total % 3600) / 60);
    const sec = String(total % 60).padStart(2, '0');
    return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${sec}` : `${m}:${sec}`;
}

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
// image's metadata out on the way, and seals a DM's. channelID names the
// channel it's for, empty for a profile's picture, and replaces the file
// it's made to take the place of, if any (M5). send, for a message's file,
// says which version of a photo or video goes, "smaller" or "full", and the
// local service keeps both to compare (M5); progress names the upload for
// the page to follow, which onFollow hears, while a video's copy is made
// (M5.4), and in Chrome and Edge, the page makes the copy itself (M5.5). It
// reports how much of the file went to the local service, from 0 to 1, and
// returns the upload's promise and a way to stop it.
export function upload(denID, channelID, file, name, onProgress, replaces, send, progress, onFollow) {
    const xhr = new XMLHttpRequest();
    const query = new URLSearchParams();
    if (channelID) query.set('channel', channelID);
    if (replaces) query.set('replaces', replaces);
    if (send) query.set('send', send);
    if (progress) {
        query.set('progress', progress);
        if (canCopy()) query.set('copier', 'page');
    }
    let unfollow = () => {};
    const done = new Promise((resolve, reject) => {
        const qs = String(query);
        xhr.open('POST', `/api/dens/${denID}/uploads${qs ? `?${qs}` : ''}`);
        xhr.setRequestHeader('Content-Type', 'application/octet-stream');
        xhr.setRequestHeader('Dens-Filename', encodeURIComponent(name));
        xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
        xhr.onloadend = () => unfollow();
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
        xhr.onerror = () => reject(new APIError("The upload failed. Is Dens still running on this device?", 0));
        xhr.onabort = () => reject(new APIError('Upload stopped.', 0));
        xhr.send(file);
        // Followed from the start: Firefox says the file went only once the
        // whole upload is over.
        if (progress) unfollow = followUpload(denID, progress, onFollow);
    });
    return { done, abort: () => xhr.abort() };
}

// fileWhere says what uses one of a member's files, as their list of them
// shows it (M5): a channel or a DM, a picture on their profile, or nothing
// while it waits to be sent. view is the den as the page has it.
export function fileWhere(f, view) {
    if (f.profile === 'avatar') return 'Your profile picture';
    if (f.profile === 'banner') return 'Your profile banner';
    if (!f.message_id) return 'Waiting to be sent';
    const c = view.channels.find((x) => x.id === f.channel_id);
    if (!c) return "In a channel you can't see";
    if (c.kind !== 'dm') return `In #${c.name}`;
    const other = view.members.find((m) => m.id !== view.me.id && (c.members || []).includes(m.id));
    return `In your DM with ${other?.display_name || 'a former member'}`;
}

// attachmentKind says how a message shows a file: a video it plays, an
// image's preview, audio it plays, or a card to download.
export function attachmentKind(f) {
    if (f.type?.startsWith('video/') && f.width > 0) return 'video';
    if (f.type?.startsWith('audio/')) return 'audio';
    if (f.thumb) return 'image';
    return 'file';
}

// Attachments shows a message's files: images as previews that open the
// viewer, video and audio as players, anything else as a card to download.
export function Attachments({ denID, files, onOpen }) {
    const tiles = files.filter((f) => ['image', 'video'].includes(attachmentKind(f)));
    const others = files.filter((f) => !tiles.includes(f));
    const box = tiles.length > 1 ? SEVERAL : ONE;
    return (
        <div class="mt-1 flex flex-col gap-1">
            {tiles.length > 0 && (
                <div class="flex flex-wrap gap-1">
                    {tiles.map((f) => attachmentKind(f) === 'video'
                        ? <VideoTile key={f.id} denID={denID} file={f} box={box} />
                        : <ImageTile key={f.id} denID={denID} file={f} box={box} onOpen={() => onOpen(f)} />)}
                </div>
            )}
            {others.map((f) => attachmentKind(f) === 'audio'
                ? <AudioCard key={f.id} denID={denID} file={f} />
                : <FileCard key={f.id} denID={denID} file={f} />)}
        </div>
    );
}

// How long a playing video's controls stay up after the pointer last moved,
// about as long as browsers' own.
const CONTROLS_LINGER = 2500;

// playbackTime reads a player's position, and the video's length when
// it's known: 0:01 / 0:04.
export function playbackTime(seconds, total) {
    const at = formatDuration(seconds * 1000);
    return total > 0 ? `${at} / ${formatDuration(total * 1000)}` : at;
}

// Videos play at one volume, which the browser keeps (M3.3): set it on any
// video and every video follows, playing or still to start. Muting stays
// each video's own.
const VIDEO_VOLUME = 'DENS_VIDEO_VOLUME';
const volumeListeners = new Set();
let videoVolume = readVideoVolume();

function readVideoVolume() {
    try {
        const v = Number(localStorage.getItem(VIDEO_VOLUME) ?? '1');
        return Number.isFinite(v) && v >= 0 && v <= 1 ? v : 1;
    } catch {
        // Storage can be unavailable; full volume stands in.
        return 1;
    }
}

function setVideoVolume(v) {
    videoVolume = v;
    try {
        localStorage.setItem(VIDEO_VOLUME, String(v));
    } catch {
        // As above: it holds for this page.
    }
    volumeListeners.forEach((fn) => fn(v));
}

// VideoTile plays a video in the message list, sized before it loads from
// the size the den stated, with its preview as the poster, or a plain box
// of its shape without one. Nothing of the video loads until it plays; the
// player asks for ranges as it goes, and as the member seeks. The controls
// are the page's own, since browsers' own can't be kept up: they stay while
// the video is paused, so it shows where it stopped and how long it is,
// and while it plays they hide once the pointer rests or leaves. Keyboard
// focus inside keeps them up too, but not focus a click gave, as Firefox
// gives the video itself.
function VideoTile({ denID, file, box }) {
    const size = fitBox(file.width, file.height, box.w, box.h);
    const frame = useRef(null);
    const video = useRef(null);
    const linger = useRef(0);
    const [paused, setPaused] = useState(true);
    const [started, setStarted] = useState(false);
    const [time, setTime] = useState(0);
    const [duration, setDuration] = useState((file.duration_ms || 0) / 1000);
    const [muted, setMuted] = useState(false);
    const [volume, setVolume] = useState(videoVolume);
    const [waiting, setWaiting] = useState(false);
    const [failed, setFailed] = useState(null);
    const [stirred, setStirred] = useState(false);
    const [full, setFull] = useState(false);

    useEffect(() => {
        const onFull = () => setFull(document.fullscreenElement === frame.current);
        document.addEventListener('fullscreenchange', onFull);
        return () => {
            document.removeEventListener('fullscreenchange', onFull);
            clearTimeout(linger.current);
        };
    }, []);

    // stir shows the controls for a while after the pointer moves.
    function stir() {
        setStirred(true);
        clearTimeout(linger.current);
        linger.current = setTimeout(() => setStirred(false), CONTROLS_LINGER);
    }
    function rest() {
        clearTimeout(linger.current);
        setStirred(false);
    }
    function toggle() {
        const v = video.current;
        // A play the browser refuses shows through the error event.
        if (v.paused) v.play().catch(() => {});
        else v.pause();
    }
    useEffect(() => {
        const follow = (x) => {
            if (video.current) video.current.volume = x;
        };
        follow(videoVolume);
        volumeListeners.add(follow);
        return () => volumeListeners.delete(follow);
    }, []);
    function toggleMute() {
        const v = video.current;
        v.muted = !v.muted;
        if (!v.muted && v.volume === 0) setVideoVolume(1);
    }
    function setLevel(e) {
        const x = Number(e.currentTarget.value);
        setVideoVolume(x);
        video.current.muted = x === 0;
    }
    function seek(e) {
        const t = Number(e.currentTarget.value);
        video.current.currentTime = t;
        setTime(t);
    }
    function toggleFull() {
        if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
        else frame.current.requestFullscreen().catch(() => {});
    }

    const shown = paused || stirred || failed;
    // Hidden controls still come up for keyboard focus inside.
    const fade = shown ? '' : 'pointer-events-none opacity-0 group-has-[:focus-visible]:pointer-events-auto group-has-[:focus-visible]:opacity-100';
    const silent = muted || volume === 0;
    const button = 'flex h-6 w-6 shrink-0 items-center justify-center rounded hover:bg-white/20';
    return (
        <div ref={frame} onPointerMove={stir} onPointerDown={stir} onPointerLeave={rest}
            class="@container group relative max-w-full overflow-hidden rounded bg-base-300 [&:fullscreen]:rounded-none [&:fullscreen]:bg-black"
            style={full ? undefined : { width: `${size.width}px`, aspectRatio: `${size.width} / ${size.height}` }}>
            <video ref={video} src={fileURL(denID, file.id)} poster={file.thumb ? thumbURL(denID, file.id) : undefined} preload="none"
                title={file.name} aria-label={file.name} class="h-full w-full object-contain" onClick={toggle}
                onPlay={() => { setPaused(false); setStarted(true); }} onPause={() => setPaused(true)}
                onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
                onDurationChange={(e) => Number.isFinite(e.currentTarget.duration) && setDuration(e.currentTarget.duration)}
                onVolumeChange={(e) => { setMuted(e.currentTarget.muted); setVolume(e.currentTarget.volume); }}
                onWaiting={() => setWaiting(true)} onPlaying={() => setWaiting(false)} onCanPlay={() => setWaiting(false)}
                onError={(e) => setFailed(e.currentTarget.error?.code === 2 ? "The video couldn't be loaded." : "This page can't play this video.")} />
            {failed ? (
                <div class="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-black/70 p-2 text-center text-xs text-white">
                    <span>{failed}</span>
                    <a class="btn btn-xs" href={downloadURL(denID, file)} download={file.name}>Download</a>
                </div>
            ) : (
                (!started || (waiting && !paused)) && (
                    <span class="pointer-events-none absolute inset-0 flex items-center justify-center text-white">
                        {waiting && !paused
                            ? <span class="loading loading-spinner loading-md" aria-label="Loading"></span>
                            : <span class="flex h-10 w-10 items-center justify-center rounded-full bg-black/50"><PlayIcon size="h-5 w-5" /></span>}
                    </span>
                )
            )}
            {!failed && (
                <div class={`absolute inset-x-0 bottom-0 flex items-center gap-1 bg-linear-to-t from-black/70 to-transparent px-1 pb-1 pt-5 text-white transition-opacity ${fade}`}>
                    <button type="button" class={button} onClick={toggle} title={paused ? 'Play' : 'Pause'} aria-label={paused ? 'Play' : 'Pause'}>
                        {paused ? <PlayIcon size="h-3.5 w-3.5" /> : <PauseIcon />}
                    </button>
                    <span class="hidden shrink-0 cursor-default select-none text-[11px] tabular-nums @min-[13rem]:inline">{playbackTime(time, duration)}</span>
                    <input type="range" class="range range-md min-w-0 flex-1" min="0" max={duration || 0} step="any" value={time}
                        onInput={seek} disabled={!(duration > 0)} aria-label="Seek" aria-valuetext={playbackTime(time, duration)} />
                    {/* The volume shows above the mute button on hover or
                        keyboard focus, at any width (M3.3). */}
                    <div class="group/volume relative flex shrink-0">
                        <button type="button" class={button} onClick={toggleMute} title={silent ? 'Unmute' : 'Mute'} aria-label={silent ? 'Unmute' : 'Mute'}>
                            <SoundIcon muted={silent} />
                        </button>
                        <div class="absolute bottom-full left-1/2 hidden -translate-x-1/2 pb-1 group-focus-within/volume:flex group-hover/volume:flex">
                            <div class="flex rounded bg-black/70 px-1 py-2">
                                <input type="range" class="h-16 w-6 cursor-pointer accent-white [direction:rtl] [writing-mode:vertical-lr]" min="0" max="1"
                                    step="0.05" value={silent ? 0 : volume} onInput={setLevel} aria-label="Volume" aria-orientation="vertical"
                                    aria-valuetext={`${Math.round((silent ? 0 : volume) * 100)}%`} />
                            </div>
                        </div>
                    </div>
                    {document.fullscreenEnabled && (
                        <button type="button" class={`${button} hidden @min-[9rem]:flex`} onClick={toggleFull}
                            title={full ? 'Exit full screen' : 'Full screen'} aria-label={full ? 'Exit full screen' : 'Full screen'}>
                            <FullIcon full={full} />
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}

function PlayIcon({ size }) {
    return (
        <svg viewBox="0 0 16 16" class={size} fill="currentColor" aria-hidden="true">
            <path d="M4.5 2.8v10.4L13 8z" />
        </svg>
    );
}

function PauseIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-3.5 w-3.5" fill="currentColor" aria-hidden="true">
            <path d="M4 2.5h3v11H4zM9 2.5h3v11H9z" />
        </svg>
    );
}

export function SoundIcon({ muted }) {
    return (
        <svg viewBox="0 0 16 16" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" aria-hidden="true">
            <path d="M2 6h2.5L8 3v10L4.5 10H2z" fill="currentColor" stroke="none" />
            {muted ? <path d="M10.5 6l4 4M14.5 6l-4 4" /> : <path d="M10.5 5.5a3.5 3.5 0 0 1 0 5M12.5 3.5a6.3 6.3 0 0 1 0 9" />}
        </svg>
    );
}

export function FullIcon({ full }) {
    return (
        <svg viewBox="0 0 16 16" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" aria-hidden="true">
            {full
                ? <path d="M6 2.5V6H2.5M10 2.5V6h3.5M6 13.5V10H2.5M10 13.5V10h3.5" />
                : <path d="M2.5 6V2.5H6M13.5 6V2.5H10M2.5 10v3.5H6M13.5 10v3.5H10" />}
        </svg>
    );
}

function AudioCard({ denID, file }) {
    return (
        <div class="flex w-full max-w-sm flex-col gap-2 rounded border border-base-300 bg-base-200 p-2">
            <div class="flex items-center gap-3">
                <div class="min-w-0 flex-1">
                    <p class="truncate text-sm font-medium" title={file.name}>{file.name}</p>
                    <p class="cursor-default select-none text-xs text-base-content/60">
                        {file.duration_ms > 0 && `${formatDuration(file.duration_ms)} · `}{formatSize(file.size)}
                    </p>
                </div>
                <a class="btn btn-ghost btn-sm" href={downloadURL(denID, file)} download={file.name}>Download</a>
            </div>
            <audio src={fileURL(denID, file.id)} controls preload="none" aria-label={file.name} class="w-full" />
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

// step moves from image index of count by delta, stopping at the ends.
export function step(index, count, delta) {
    return Math.min(count - 1, Math.max(0, index + delta));
}

// The room between a message's pictures as a swipe slides them past, in
// pixels, and how long they take to settle once it's let go, in ms.
const GAP = 16;
const SETTLE = 200;

// Viewer shows a message's images whole, over everything (M5): the one at
// index of files, its preview at once and the original as it loads. In a
// wide window it's fitted with room round it, and ‹ and › beside it move
// between them, as do ← and →; on the phone layout it takes the screen,
// the picture its width, and a swipe sideways moves to the next or last,
// and one down closes it (M13.3). Two fingers or a double tap zoom it, as
// do the wheel and a double click, and a finger or the mouse pans it then
// (zoom.js). Its name, size, Download and Close sit in a bar at its foot.
// onIndex hears where to move; Back closes it (M13.1).
export function Viewer({ denID, files, index, onIndex, onClose }) {
    useLayer(onClose);
    const narrow = useNarrow();
    const stageRef = useRef(null);
    const close = useRef(null);
    const [stage, setStage] = useState({ width: 0, height: 0 });
    // slide is where a swipe has the pictures, x across and y down, along
    // the axis it took; settling, once it's let go, with the index it
    // settles on, or -1 to stay.
    const [slide, setSlide] = useState({ x: 0, y: 0, axis: '', settling: false, to: -1 });
    const sliding = useRef(slide);
    sliding.current = slide;
    const settled = useRef(0);
    const at = useRef(index);
    at.current = index;
    useLayoutEffect(() => {
        const el = stageRef.current;
        const measure = () => setStage({ width: el.clientWidth, height: el.clientHeight });
        measure();
        const watch = new ResizeObserver(measure);
        watch.observe(el);
        return () => watch.disconnect();
    }, []);
    useEffect(() => {
        const onKey = (e) => {
            if (e.key === 'Escape') onClose();
            const delta = { ArrowLeft: -1, ArrowRight: 1 }[e.key];
            if (!delta) return;
            e.preventDefault();
            const next = step(at.current, files.length, delta);
            if (next !== at.current) onIndex(next);
        };
        window.addEventListener('keydown', onKey);
        close.current?.focus();
        return () => window.removeEventListener('keydown', onKey);
    }, [files]);
    useEffect(() => () => clearTimeout(settled.current), []);
    const file = files[index];
    const several = files.length > 1;
    // A picture's box: the stage's width on the phone layout, and in a
    // wider window, room beside it for ‹ and ›.
    const boxOf = (f) => fitBox(f.width, f.height, Math.max(64, stage.width - (!narrow && several ? 96 : 0)), Math.max(64, stage.height));
    const box = boxOf(file);
    const motion = typeof matchMedia === 'function' && !matchMedia('(prefers-reduced-motion: reduce)').matches;

    // settle lets the pictures go to x, then moves to the index to, if
    // it's one.
    function settle(x, to) {
        const s = { x, y: 0, axis: '', settling: true, to };
        sliding.current = s;
        setSlide(s);
        clearTimeout(settled.current);
        settled.current = setTimeout(done, motion ? SETTLE + 50 : 0);
    }
    function done() {
        clearTimeout(settled.current);
        const s = sliding.current;
        if (!s.settling) return;
        if (s.to >= 0) onIndex(s.to);
        setSlide({ x: 0, y: 0, axis: '', settling: false, to: -1 });
    }
    const zoom = useZoom({
        box, stage, max: maxScale(file.width, box, window.devicePixelRatio), key: file.id,
        on: {
            drag(dx, dy) {
                const axis = sliding.current.axis || (Math.abs(dx) > Math.abs(dy) ? 'x' : 'y');
                // Past the first or last picture it gives a little.
                const there = files[at.current + (dx < 0 ? 1 : -1)];
                const s = axis === 'x' ? { x: there ? dx : dx / 4, y: 0, axis, settling: false, to: -1 } : { x: 0, y: Math.max(0, dy), axis, settling: false, to: -1 };
                sliding.current = s;
                setSlide(s);
            },
            drop(dx, dy, speed) {
                const axis = sliding.current.axis;
                if (axis === 'y') {
                    if (dismisses(dy, stage.height, speed.y)) onClose();
                    else settle(0, -1);
                    return;
                }
                const dir = dx < 0 ? 1 : -1;
                const to = at.current + dir;
                const far = Math.abs(dx) > stage.width / 5 || (Math.abs(speed.x) > 0.4 && Math.sign(speed.x) === -dir);
                if (axis === 'x' && to >= 0 && to < files.length && far) settle(-dir * (stage.width + GAP), to);
                else settle(0, -1);
            },
            cancel: () => settle(0, -1),
            // A tap beside the picture closes it, as a click beside it does.
            tap(x, y) {
                const v = zoom.view;
                if (x < v.x || y < v.y || x > v.x + box.width * v.scale || y > v.y + box.height * v.scale) onClose();
            },
        },
    });
    const backdrop = useBackdrop(onClose);
    // Full screen it's black, as a phone's photos are, and over a wider
    // window the page shows through a little; a swipe down fades it.
    const dark = narrow ? 1 : 0.85;
    const shade = slide.axis === 'y' ? Math.max(0.3, 1 - slide.y / Math.max(1, stage.height)) * dark : dark;
    const side = 'btn btn-circle btn-ghost absolute top-1/2 z-10 -translate-y-1/2 text-3xl text-white hover:bg-white/15 pointer-coarse:max-md:hidden';
    const keep = { onPointerDown: (e) => e.stopPropagation() };
    // The picture shown, and those beside it, out of sight until a swipe.
    const picture = (d) => {
        const f = files[index + d];
        if (!f) return null;
        const b = d === 0 ? box : boxOf(f);
        const v = d === 0 ? zoom.view : fitted(b, stage);
        const style = { left: `${v.x + d * (stage.width + GAP)}px`, top: `${v.y}px`, width: `${b.width * v.scale}px`, height: `${b.height * v.scale}px` };
        return (
            <div key={f.id} class="absolute overflow-hidden md:rounded" style={style} aria-hidden={d !== 0 || undefined} data-shown={d === 0 || undefined}>
                {f.thumb && <img src={thumbURL(denID, f.id)} alt="" class="absolute inset-0 h-full w-full object-contain" draggable={false} />}
                <img src={fileURL(denID, f.id)} alt={d === 0 ? f.name : ''} class="relative h-full w-full object-contain" draggable={false} />
            </div>
        );
    };
    return (
        <div role="dialog" aria-modal="true" aria-label={file.name} class="fixed inset-0 z-50 flex flex-col md:gap-3 md:p-4"
            style={{ backgroundColor: `rgb(0 0 0 / ${shade})` }} {...backdrop}>
            <div ref={stageRef} data-stage class={`relative min-h-0 w-full flex-1 touch-none select-none overflow-hidden ${isFitted(zoom.view) ? '' : 'cursor-grab active:cursor-grabbing'}`}
                {...zoom.handlers}>
                <div class="absolute inset-0" style={{ transform: slide.x || slide.y ? `translate(${slide.x}px, ${slide.y}px)` : undefined,
                    transition: slide.settling && motion ? `transform ${SETTLE}ms ease-out` : 'none' }}
                    onTransitionEnd={(e) => e.target === e.currentTarget && slide.settling && done()}>
                    {picture(-1)}
                    {picture(0)}
                    {picture(1)}
                </div>
                {several && index > 0 && (
                    <button type="button" class={`${side} left-0`} aria-label="Previous image" {...keep} onClick={() => onIndex(index - 1)}>‹</button>
                )}
                {several && index < files.length - 1 && (
                    <button type="button" class={`${side} right-0`} aria-label="Next image" {...keep} onClick={() => onIndex(index + 1)}>›</button>
                )}
            </div>
            <div class="flex shrink-0 items-center gap-2 px-3 pb-[max(0.5rem,env(safe-area-inset-bottom))] pt-2 text-sm text-white max-md:bg-black/60 md:max-w-full md:justify-center md:gap-3 md:self-center md:p-0">
                {several && <span class="shrink-0 cursor-default select-none text-white/60">{index + 1} of {files.length}</span>}
                <span class="min-w-0 flex-1 truncate md:flex-initial" title={file.name}>{file.name}</span>
                <span class="shrink-0 cursor-default select-none text-white/60">{formatSize(file.size)}</span>
                <a class="btn btn-sm shrink-0" href={downloadURL(denID, file)} download={file.name}>Download</a>
                <button ref={close} type="button" class="btn btn-ghost btn-sm shrink-0 text-white" onClick={onClose}>Close</button>
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

// A video's preview, where Dens can't make one: the media module doesn't
// decode WebM's or AV1's frames, so the page draws the first frame the
// browser plays and sends it, before the message goes. The den makes its own
// preview from that image. Where the browser can't play the video either, it
// goes without, and the message list shows a box of its shape.
const PREVIEW_SIDE = 1280;
const PREVIEW_WAIT = 15000;

// needsPreview says whether an upload is a video still without a preview.
export const needsPreview = (f) => !!f.type?.startsWith('video/') && f.width > 0 && !f.thumb;

// previewSize fits a frame of w×h in maxSide, keeping its shape.
export function previewSize(w, h, maxSide) {
    if (!(w > 0 && h > 0)) return { w: 0, h: 0 };
    const s = Math.min(1, maxSide / Math.max(w, h));
    return { w: Math.max(1, Math.round(w * s)), h: Math.max(1, Math.round(h * s)) };
}

// drawPreview resolves to a JPEG of the first frame of the video at src, or
// null.
function drawPreview(src) {
    return new Promise((resolve) => {
        const video = document.createElement('video');
        const done = (blob) => {
            clearTimeout(timer);
            video.removeAttribute('src');
            video.load();
            resolve(blob);
        };
        const timer = setTimeout(() => done(null), PREVIEW_WAIT);
        video.muted = true;
        video.playsInline = true;
        video.preload = 'auto';
        video.onerror = () => done(null);
        video.onloadeddata = () => {
            const { w, h } = previewSize(video.videoWidth, video.videoHeight, PREVIEW_SIDE);
            if (!w) return done(null);
            const canvas = document.createElement('canvas');
            canvas.width = w;
            canvas.height = h;
            try {
                canvas.getContext('2d').drawImage(video, 0, 0, w, h);
            } catch {
                return done(null);
            }
            canvas.toBlob(done, 'image/jpeg', 0.9);
        };
        video.src = src;
    });
}

// addPreview gives an uploaded video the page's preview, and resolves to
// the upload as it is then: with the preview, or as it was.
export async function addPreview(denID, file) {
    const blob = await drawPreview(fileURL(denID, file.id));
    if (!blob) return file;
    try {
        const res = await fetch(`/api/dens/${denID}/uploads/${file.id}/thumb`, {
            method: 'POST',
            headers: pageHeaders({ 'Content-Type': 'application/octet-stream' }),
            body: blob,
            credentials: 'same-origin',
        });
        if (reloadIfStale(res) || !res.ok) return file;
        const up = await res.json();
        return up?.thumb ? { ...file, thumb: up.thumb } : file;
    } catch {
        return file;
    }
}
