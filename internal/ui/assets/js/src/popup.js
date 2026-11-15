// A popup by its button: placed over the page, so nothing its button sits
// in clips it, and closed by a press anywhere else, or by Escape. The
// soundboard opens as one, and the rest of the call's controls.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';

// popupAt places a popup of a width by its button's place on screen: up
// and to the right of it, or down, kept within the window.
export function popupAt(rect, down, vw, vh, width) {
    const left = Math.max(8, Math.min(rect.left, vw - width - 8));
    return down ? { left, top: rect.bottom + 8 } : { left, bottom: vh - rect.top + 8 };
}

/** usePopup is where a popup stands while it's open, null until it's
    placed: by anchor, which holds its button and the popup itself, and
    which it follows as the page scrolls or the window changes size.
    onClose closes it, for a press outside anchor, or Escape. */
export function usePopup(open, anchor, { down = false, width, onClose }) {
    const [place, setPlace] = useState(null);
    useLayoutEffect(() => {
        if (!open) return undefined;
        const follow = () => anchor.current && setPlace(popupAt(anchor.current.getBoundingClientRect(), down, window.innerWidth, window.innerHeight, width));
        follow();
        window.addEventListener('resize', follow);
        document.addEventListener('scroll', follow, true);
        return () => {
            window.removeEventListener('resize', follow);
            document.removeEventListener('scroll', follow, true);
        };
    }, [open, down, width]);
    const close = useRef(onClose);
    close.current = onClose;
    useEffect(() => {
        if (!open) return undefined;
        const away = (e) => {
            if (!anchor.current?.contains(e.target)) close.current();
        };
        const escape = (e) => e.key === 'Escape' && close.current();
        document.addEventListener('pointerdown', away);
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('pointerdown', away);
            document.removeEventListener('keydown', escape);
        };
    }, [open]);
    return open ? place : null;
}
