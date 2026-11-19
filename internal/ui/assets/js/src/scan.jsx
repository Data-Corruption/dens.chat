// Scanning sign-in codes (M22.6): whether there's a camera, the camera
// read for a code by the page's own reader, and a code the service made
// drawn as squares.

import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { ErrorText } from './components.jsx';
import { inApp } from './platform.js';
import { readQR } from './qr.js';
import { codePath } from './signincode.js';

// FRAME is the longest side a camera's frame is read at: scaling one down
// smooths its noise, and keeps a phone's reading quick.
const FRAME = 640;

// EVERY is how often a frame is read, in ms.
const EVERY = 150;

/** useCamera says whether the browser has a camera to scan with, null
    until it knows. A browser lists its cameras before it's let use one,
    unnamed. */
export function useCamera() {
    const [has, setHas] = useState(null);
    useEffect(() => {
        let live = true;
        const media = typeof navigator === 'undefined' ? null : navigator.mediaDevices;
        if (typeof media?.getUserMedia !== 'function' || typeof media.enumerateDevices !== 'function') {
            setHas(false);
            return undefined;
        }
        media.enumerateDevices().then((list) => live && setHas(list.some((d) => d.kind === 'videoinput')), () => live && setHas(false));
        return () => {
            live = false;
        };
    }, []);
    return has;
}

/** cameraProblem says why the camera didn't open, and where a refused one
    is allowed, as micProblem does for the microphone. */
export function cameraProblem(e, app = inApp()) {
    switch (e?.name) {
        case 'NotAllowedError':
            return app
                ? "Scanning needs your camera. Allow it for Dens in Android's settings, under Apps."
                : "Scanning needs your camera. Allow it for this page in your browser's site settings.";
        case 'NotFoundError':
            return 'No camera was found.';
        case 'NotReadableError':
            return 'Your camera is in use by another program, or it could not be opened.';
    }
    return "Your camera couldn't be opened.";
}

/** Scanner shows the camera, the back one where there are two, and reads
    its frames for a code. onCode gets each code's text as it's read, and
    returns true to stop. The camera stops as the scanner goes. */
export function Scanner({ onCode }) {
    const video = useRef(null);
    const take = useRef(onCode);
    take.current = onCode;
    const [problem, setProblem] = useState('');
    useEffect(() => {
        let stream = null;
        let timer = 0;
        let stopped = false;
        const canvas = document.createElement('canvas');
        const context = canvas.getContext('2d', { willReadFrequently: true });
        const read = () => {
            if (stopped) return;
            const v = video.current;
            if (v && v.readyState >= 2 && v.videoWidth > 0) {
                const scale = Math.min(1, FRAME / Math.max(v.videoWidth, v.videoHeight));
                const width = Math.round(v.videoWidth * scale);
                const height = Math.round(v.videoHeight * scale);
                if (canvas.width !== width || canvas.height !== height) {
                    canvas.width = width;
                    canvas.height = height;
                }
                context.drawImage(v, 0, 0, width, height);
                const text = readQR(context.getImageData(0, 0, width, height));
                if (text !== null && take.current(text)) return;
            }
            timer = setTimeout(read, EVERY);
        };
        if (typeof navigator.mediaDevices?.getUserMedia !== 'function') {
            setProblem(cameraProblem(null));
            return undefined;
        }
        navigator.mediaDevices.getUserMedia({ video: { facingMode: { ideal: 'environment' }, width: { ideal: 1280 }, height: { ideal: 720 } }, audio: false })
            .then((s) => {
                if (stopped || !video.current) {
                    s.getTracks().forEach((t) => t.stop());
                    return;
                }
                stream = s;
                video.current.srcObject = s;
                video.current.play().catch(() => {});
                read();
            }, (e) => !stopped && setProblem(cameraProblem(e)));
        return () => {
            stopped = true;
            clearTimeout(timer);
            stream?.getTracks().forEach((t) => t.stop());
        };
    }, []);
    if (problem) return <ErrorText message={problem} />;
    return (
        <div class="relative mx-auto aspect-square w-full max-w-72 overflow-hidden rounded-box bg-black" data-scanner>
            <video ref={video} class="h-full w-full object-cover" muted playsinline autoplay aria-label="Camera"></video>
            <div class="pointer-events-none absolute inset-[15%] rounded-box border-2 border-white/80" aria-hidden="true"></div>
        </div>
    );
}

/** QRCode draws a code the service made, black on white with its quiet
    zone of four modules, whatever the theme, as readers expect one. */
export function QRCode({ code, label }) {
    const path = useMemo(() => codePath(code), [code]);
    if (!path) return null;
    const side = code.size + 8;
    return (
        <svg viewBox={`-4 -4 ${side} ${side}`} role="img" aria-label={label} shape-rendering="crispEdges"
            class="mx-auto block aspect-square w-full max-w-64 rounded-box bg-white" data-qr>
            <path d={path} fill="#000" />
        </svg>
    );
}
