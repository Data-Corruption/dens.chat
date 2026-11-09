// The picture behind the chat: one of the member's own, which the service
// keeps, with how far the theme's background covers it, the dim, so every
// page paired with the install shows the same. The service names it in
// the dens view, by its version, and the page draws it behind the message
// list and nowhere else.

import { APIError, api, pageHeaders, reloadIfStale } from './api.js';
import { onEvent } from './events.js';

/** The largest file a background takes, which the service checks too, and
    the kinds it takes */
export const MAX_FILE = 10 << 20;
export const KINDS = ['image/png', 'image/jpeg', 'image/webp'];
/** The dim's most, in percent, its step on the slider, and what it is
    until the member sets it, as the service has it */
export const DIM_MOST = 90;
export const DIM_STEP = 5;
const DIM_UNSET = 70;

const listeners = new Set();
let started = false;
// heard is the background as the service last named it, and sliding the
// dim the member is setting, until the service has it.
let heard = null;
let sliding = null;

/** readBackground is the background a dens view names, its version and
    dim, or null for none */
export function readBackground(raw) {
    const version = Number(raw?.version);
    const dim = Number(raw?.dim);
    if (!Number.isInteger(version) || version <= 0) return null;
    return { version, dim: Number.isInteger(dim) && dim >= 0 && dim <= DIM_MOST ? dim : DIM_UNSET };
}

function shown() {
    return heard && sliding !== null ? { ...heard, dim: sliding } : heard;
}

function hear(raw) {
    const before = shown();
    heard = readBackground(raw);
    if (!heard || heard.dim === sliding) sliding = null;
    const now = shown();
    if (before?.version !== now?.version || before?.dim !== now?.dim) listeners.forEach((fn) => fn(now));
}

/** startBackground follows the background as the dens view names it */
export function startBackground() {
    if (started) return;
    started = true;
    onEvent((msg) => msg.t === 'dens' && hear(msg.d?.background));
}

/** onBackground calls fn with the background, now and as it changes; it
    returns a function that stops */
export function onBackground(fn) {
    listeners.add(fn);
    fn(shown());
    return () => listeners.delete(fn);
}

/** backgroundURL is where a background's file is, by its version */
export function backgroundURL(version) {
    return `/api/background?v=${version}`;
}

/** backgroundStyle is the style that draws a background behind a list:
    the picture, filling it, under the theme's background at the dim */
export function backgroundStyle(bg) {
    if (!bg) return undefined;
    const scrim = `color-mix(in oklab, var(--color-base-100) ${bg.dim}%, transparent)`;
    return {
        backgroundImage: `linear-gradient(${scrim}, ${scrim}), url("${backgroundURL(bg.version)}")`,
        backgroundSize: 'cover',
        backgroundPosition: 'center',
    };
}

/** checkBackground says why a file can't be the background, or '' */
export function checkBackground(file) {
    if (!KINDS.includes(file.type)) return 'A background is a PNG, a JPEG or a WebP.';
    if (file.size > MAX_FILE) return 'A background is at most 10 MB.';
    return '';
}

/** setBackground sends a file to be the background */
export async function setBackground(file) {
    const res = await fetch('/api/background', {
        method: 'POST',
        headers: pageHeaders({ 'Content-Type': 'application/octet-stream' }),
        body: file,
        credentials: 'same-origin',
    });
    if (reloadIfStale(res)) return new Promise(() => {});
    let data = null;
    try {
        data = await res.json();
    } catch {
        // An answer that isn't JSON says only its status, below.
    }
    if (!res.ok) throw new APIError(data?.error || `The picture wasn't kept (HTTP ${res.status}).`, res.status);
    hear(data?.background);
    return data;
}

/** removeBackground takes the background away */
export async function removeBackground() {
    hear((await api.del('/api/background'))?.background);
}

/** slideDim shows a dim while the member moves its slider */
export function slideDim(dim) {
    const before = shown();
    sliding = dim;
    if (before && before.dim !== dim) listeners.forEach((fn) => fn(shown()));
}

/** setDim has the service keep the dim */
export async function setDim(dim) {
    slideDim(dim);
    try {
        await api.post('/api/settings', { backgroundDim: dim });
    } catch (e) {
        sliding = null;
        listeners.forEach((fn) => fn(shown()));
        throw e;
    }
}
