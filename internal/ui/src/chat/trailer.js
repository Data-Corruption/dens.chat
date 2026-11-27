// Steam trailer playback over Media Source Extensions: prefers H.264+AAC
// with AV1 fallback, picks 480p on Small/phone and best <= 720p on larger
// cards, opens separate video and audio SourceBuffers via a MediaSource
// object URL, streams chunks progressively ahead of playback, and cleans up
// all fetches, listeners and object URLs on stop.

import { pageHeaders, reloadIfStale } from '../core/api.js';

export const H264_TYPE = 'video/mp4; codecs="avc1.640029"';
export const AV1_TYPE = 'video/mp4; codecs="av01.0.05M.08"';
export const AAC_TYPE = 'audio/mp4; codecs="mp4a.40.2"';

// Look ahead ~12 s (~4 chunks) so playback stays smooth without downloading
// the entire trailer up front.
const LOOKAHEAD_SECS = 12;

export function trackMime(track) {
    return `${track.mime}; codecs="${track.codecs}"`;
}

function defaultSupported(mime) {
    return typeof MediaSource !== 'undefined' && typeof MediaSource.isTypeSupported === 'function' && MediaSource.isTypeSupported(mime);
}

/** preferredEncodings orders DASH encodings to try: h264 first when H.264
    and AAC are supported, otherwise av1 first. */
export function preferredEncodings(isSupported = defaultSupported) {
    if (isSupported(H264_TYPE) && isSupported(AAC_TYPE)) return ['h264', 'av1'];
    if (isSupported(AV1_TYPE)) return ['av1', 'h264'];
    return ['h264', 'av1'];
}

/** pickVideoTrack selects the best supported video rendition for size:
    closest <= 480p on Small (and phone), closest <= 720p on Medium+, or
    the smallest supported rendition if all exceed the cap. */
export function pickVideoTrack(tracks, size, isSupported = defaultSupported) {
    const supported = (tracks || []).filter((t) => t && t.id && t.mime && t.codecs && isSupported(trackMime(t)));
    if (!supported.length) return null;
    const cap = size === 'small' ? 480 : 720;
    const sorted = [...supported].sort((a, b) => (a.height || 0) - (b.height || 0));
    let best = null;
    for (const t of sorted) {
        if ((t.height || 0) <= cap) best = t;
    }
    return best || sorted[0];
}

function appendTo(sb, buf, signal) {
    return new Promise((resolve, reject) => {
        if (signal.aborted) {
            reject(new DOMException('Aborted', 'AbortError'));
            return;
        }
        const finish = () => {
            sb.removeEventListener('updateend', onEnd);
            sb.removeEventListener('error', onErr);
            signal.removeEventListener('abort', onAbort);
        };
        const onEnd = () => {
            finish();
            resolve();
        };
        const onErr = () => {
            finish();
            reject(new Error('SourceBuffer append failed'));
        };
        const onAbort = () => {
            finish();
            reject(new DOMException('Aborted', 'AbortError'));
        };
        sb.addEventListener('updateend', onEnd);
        sb.addEventListener('error', onErr);
        signal.addEventListener('abort', onAbort);
        try {
            sb.appendBuffer(buf);
        } catch (err) {
            finish();
            reject(err);
        }
    });
}

function removeFrom(sb, start, end, signal) {
    return new Promise((resolve, reject) => {
        if (signal.aborted || end <= start) {
            resolve();
            return;
        }
        const finish = () => {
            sb.removeEventListener('updateend', onEnd);
            sb.removeEventListener('error', onErr);
            signal.removeEventListener('abort', onAbort);
        };
        const onEnd = () => {
            finish();
            resolve();
        };
        const onErr = () => {
            finish();
            reject(new Error('SourceBuffer remove failed'));
        };
        const onAbort = () => {
            finish();
            reject(new DOMException('Aborted', 'AbortError'));
        };
        sb.addEventListener('updateend', onEnd);
        sb.addEventListener('error', onErr);
        signal.addEventListener('abort', onAbort);
        try {
            sb.remove(start, end);
        } catch (err) {
            finish();
            reject(err);
        }
    });
}

/** startTrailer fetches the primary trailer manifest for app, wires a dual
    SourceBuffer MediaSource to video, and streams chunks ahead of playback.
    Returns a stop() function that aborts fetches and revokes the object URL. */
export function startTrailer({
    app,
    size,
    video,
    onReady,
    onError,
    fetchImpl = globalThis.fetch,
    MediaSourceCtor = globalThis.MediaSource,
    URLObj = globalThis.URL,
    isSupported,
}) {
    const checkType = isSupported || ((mime) => typeof MediaSourceCtor === 'function' && typeof MediaSourceCtor.isTypeSupported === 'function' && MediaSourceCtor.isTypeSupported(mime));
    const ac = new AbortController();
    let stopped = false;
    let ms = null;
    let objectURL = null;
    let vSB = null;
    let aSB = null;
    let triggerPump = null;

    function stop() {
        if (stopped) return;
        stopped = true;
        ac.abort();
        if (triggerPump && video) {
            video.removeEventListener('timeupdate', triggerPump);
            video.removeEventListener('seeking', triggerPump);
            video.removeEventListener('waiting', triggerPump);
        }
        if (video) {
            try {
                video.pause?.();
            } catch {
                // Ignore media teardown errors.
            }
        }
        if (ms && ms.readyState === 'open') {
            for (const sb of [vSB, aSB]) {
                if (sb) {
                    try {
                        sb.abort();
                    } catch {
                        // Ignore abort errors during cleanup.
                    }
                }
            }
        }
        if (video) {
            try {
                video.removeAttribute?.('src');
                video.load?.();
            } catch {
                // Ignore DOM teardown errors.
            }
        }
        if (objectURL) {
            const url = objectURL;
            objectURL = null;
            try {
                URLObj.revokeObjectURL(url);
            } catch {
                // Ignore revoke errors.
            }
        }
    }

    async function fetchRaw(path) {
        const res = await fetchImpl(path, {
            headers: pageHeaders(),
            credentials: 'same-origin',
            signal: ac.signal,
        });
        if (res.headers && reloadIfStale(res)) return null;
        if (!res.ok) {
            const err = new Error(`Trailer request failed (HTTP ${res.status}).`);
            err.status = res.status;
            throw err;
        }
        return res;
    }

    async function loadManifest() {
        const encodings = preferredEncodings(checkType);
        let lastErr = null;
        for (const enc of encodings) {
            let res;
            try {
                res = await fetchRaw(`/api/steam/${app}/trailer/${enc}`);
            } catch (err) {
                if (err.name === 'AbortError' || stopped) throw err;
                lastErr = err;
                if (err.status === 404) continue;
                continue;
            }
            if (!res) return null;
            const manifest = await res.json();
            const videoTrack = pickVideoTrack(manifest.video, size, checkType);
            if (!videoTrack) {
                lastErr = new Error('No supported video track.');
                continue;
            }
            const audioTrack = manifest.audio && checkType(trackMime(manifest.audio)) ? manifest.audio : null;
            return { enc, manifest, videoTrack, audioTrack };
        }
        throw lastErr || new Error("Trailer couldn't be played.");
    }

    (async () => {
        try {
            if (typeof MediaSourceCtor !== 'function') {
                throw new Error('MediaSource is unavailable.');
            }
            const chosen = await loadManifest();
            if (!chosen || stopped) return;
            const { enc, manifest, videoTrack, audioTrack } = chosen;

            ms = new MediaSourceCtor();
            objectURL = URLObj.createObjectURL(ms);
            const opened = new Promise((resolve, reject) => {
                const onOpen = () => {
                    ms.removeEventListener('sourceopen', onOpen);
                    ac.signal.removeEventListener('abort', onAbort);
                    resolve();
                };
                const onAbort = () => {
                    ms.removeEventListener('sourceopen', onOpen);
                    reject(new DOMException('Aborted', 'AbortError'));
                };
                ms.addEventListener('sourceopen', onOpen);
                ac.signal.addEventListener('abort', onAbort);
            });
            video.src = objectURL;
            await opened;
            if (stopped) return;

            if (manifest.duration > 0 && Number.isFinite(manifest.duration)) {
                try {
                    ms.duration = manifest.duration;
                } catch {
                    // Some engines adjust duration after init segments arrive.
                }
            }

            vSB = ms.addSourceBuffer(trackMime(videoTrack));
            aSB = audioTrack ? ms.addSourceBuffer(trackMime(audioTrack)) : null;

            const base = `/api/steam/${app}/trailer/${enc}`;
            const fetchBytes = async (path) => {
                const res = await fetchRaw(path);
                return res ? res.arrayBuffer() : null;
            };

            const [vInit, aInit] = await Promise.all([
                fetchBytes(`${base}/${videoTrack.id}/init`),
                aSB ? fetchBytes(`${base}/${audioTrack.id}/init`) : Promise.resolve(null),
            ]);
            if (stopped || !vInit) return;
            await appendTo(vSB, vInit, ac.signal);
            if (stopped) return;
            if (aSB && aInit) {
                await appendTo(aSB, aInit, ac.signal);
                if (stopped) return;
            }

            let totalVideo = videoTrack.segments || 0;
            let totalAudio = audioTrack ? audioTrack.segments || 0 : 0;
            const segDur = videoTrack.segment_duration > 0 ? videoTrack.segment_duration : 3;
            const loadedVideo = new Set();
            const loadedAudio = new Set();
            let started = false;
            let pumping = false;
            let pumpAgain = false;

            async function appendSafe(sb, buf, loadedSet, seg) {
                try {
                    await appendTo(sb, buf, ac.signal);
                } catch (err) {
                    if (err.name !== 'QuotaExceededError' || stopped) throw err;
                    const cutoff = Math.max(0, (video.currentTime || 0) - 30);
                    if (cutoff <= 0) throw err;
                    await removeFrom(sb, 0, cutoff, ac.signal);
                    const dropThrough = Math.floor(cutoff / segDur);
                    for (let i = 1; i <= dropThrough; i++) loadedSet.delete(i);
                    await appendTo(sb, buf, ac.signal);
                }
                loadedSet.add(seg);
            }

            async function pump() {
                if (stopped) return;
                if (pumping) {
                    pumpAgain = true;
                    return;
                }
                pumping = true;
                try {
                    do {
                        pumpAgain = false;
                        const t = Math.max(0, video.currentTime || 0);
                        const cur = Math.min(totalVideo, Math.floor(t / segDur) + 1);
                        const end = Math.min(totalVideo, Math.max(cur, Math.ceil((t + LOOKAHEAD_SECS) / segDur)));
                        let nextSeg = 0;
                        for (let s = cur; s <= end; s++) {
                            const needV = !loadedVideo.has(s);
                            const needA = !!aSB && s <= totalAudio && !loadedAudio.has(s);
                            if (needV || needA) {
                                nextSeg = s;
                                break;
                            }
                        }
                        if (nextSeg === 0) {
                            const doneV = totalVideo > 0 && loadedVideo.has(totalVideo);
                            const doneA = !aSB || totalAudio === 0 || loadedAudio.has(totalAudio);
                            if (doneV && doneA && ms.readyState === 'open' && !vSB.updating && (!aSB || !aSB.updating)) {
                                try {
                                    ms.endOfStream();
                                } catch {
                                    // Ignore if already ended or closed.
                                }
                            }
                            continue;
                        }

                        const needV = !loadedVideo.has(nextSeg);
                        const needA = !!aSB && nextSeg <= totalAudio && !loadedAudio.has(nextSeg);
                        let vBuf = null;
                        let aBuf = null;
                        try {
                            [vBuf, aBuf] = await Promise.all([
                                needV ? fetchBytes(`${base}/${videoTrack.id}/${nextSeg}`) : Promise.resolve(null),
                                needA ? fetchBytes(`${base}/${audioTrack.id}/${nextSeg}`) : Promise.resolve(null),
                            ]);
                        } catch (err) {
                            if (err.status === 404 && nextSeg === totalVideo && nextSeg > 1) {
                                // Duration rounding on the MPD can overshoot by one chunk.
                                totalVideo = nextSeg - 1;
                                if (totalAudio >= nextSeg) totalAudio = nextSeg - 1;
                                pumpAgain = true;
                                continue;
                            }
                            throw err;
                        }
                        if (stopped) return;
                        if (vBuf) await appendSafe(vSB, vBuf, loadedVideo, nextSeg);
                        if (stopped) return;
                        if (aSB && aBuf) await appendSafe(aSB, aBuf, loadedAudio, nextSeg);
                        if (stopped) return;

                        if (!started) {
                            started = true;
                            onReady?.();
                            // If transient user activation expired during slow
                            // buffering, retry muted rather than stalling paused.
                            const p = video.play?.();
                            if (p && typeof p.catch === 'function') {
                                p.catch((err) => {
                                    if (!stopped && err?.name === 'NotAllowedError' && !video.muted) {
                                        video.muted = true;
                                        video.play?.()?.catch?.(() => {});
                                    }
                                });
                            }
                        }
                        pumpAgain = true;
                    } while (pumpAgain && !stopped);
                } catch (err) {
                    if (!stopped && err.name !== 'AbortError') {
                        onError?.(err);
                    }
                } finally {
                    pumping = false;
                }
            }

            triggerPump = () => {
                pump();
            };
            video.addEventListener('timeupdate', triggerPump);
            video.addEventListener('seeking', triggerPump);
            video.addEventListener('waiting', triggerPump);
            await pump();
        } catch (err) {
            if (!stopped && err.name !== 'AbortError') {
                onError?.(err);
            }
        }
    })();

    return stop;
}
