// Sheets (M13.2): the phone layout's menus, which open from the screen's
// foot over the page, dimmed, with rows a finger fits. A tap above one
// closes it, as do a swipe down, Back, Escape and a row's action. Wider
// windows keep their menus, touch or not.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { useBackdrop } from './components.jsx';
import { useLayer } from './nav.js';
import { SLOP, dismisses } from './touch.js';

// Sheet frames a sheet: label names it, and title, if given, heads it,
// under the handle. Its rows are SheetRows, or anything else as tall. The
// focus starts on its first row, and goes back to where it was once the
// sheet closes.
export function Sheet({ label, title, onClose, children }) {
    useLayer(onClose);
    const backdrop = useBackdrop(onClose);
    const panel = useRef(null);
    const drag = useRef(null);
    const [dy, setDy] = useState(0);
    const [dragging, setDragging] = useState(false);
    // A sheet taller than the room scrolls, and then only its top drags.
    const [scrolls, setScrolls] = useState(false);
    useLayoutEffect(() => {
        const el = panel.current;
        setScrolls(el.scrollHeight > el.clientHeight + 1);
    });
    useEffect(() => {
        const opener = document.activeElement;
        panel.current?.querySelector('[role="menuitem"]:not(:disabled)')?.focus({ preventScroll: true });
        return () => {
            if (opener?.isConnected) opener.focus({ preventScroll: true });
        };
    }, []);
    function keys(e) {
        const items = [...panel.current.querySelectorAll('[role="menuitem"]:not(:disabled)')];
        const at = items.indexOf(document.activeElement);
        const to = { ArrowDown: at + 1, ArrowUp: at - 1, Home: 0, End: items.length - 1 }[e.key];
        if (to !== undefined && items.length) {
            e.preventDefault();
            items[(to + items.length) % items.length].focus();
        } else if (e.key === 'Escape' || e.key === 'Tab') {
            e.preventDefault();
            e.stopPropagation();
            onClose();
        }
    }
    // A press that moves down past the slop drags the sheet with it, and
    // the press's click goes nowhere; let go far enough or fast enough,
    // the sheet closes, and otherwise goes back up. A slider's press is
    // the slider's.
    function down(e) {
        if (e.button !== 0 || e.target.closest('input')) return;
        if (scrolls && !e.target.closest('[data-sheet-top]')) return;
        drag.current = { id: e.pointerId, y: e.clientY, last: e.clientY, at: e.timeStamp, speed: 0, moved: false };
    }
    function move(e) {
        const d = drag.current;
        if (!d || e.pointerId !== d.id) return;
        const by = e.clientY - d.y;
        if (!d.moved) {
            if (by < SLOP) return;
            d.moved = true;
            setDragging(true);
            e.currentTarget.setPointerCapture?.(e.pointerId);
        }
        const dt = e.timeStamp - d.at;
        if (dt > 0) d.speed = (e.clientY - d.last) / dt;
        d.last = e.clientY;
        d.at = e.timeStamp;
        setDy(Math.max(0, by));
    }
    function up(e) {
        const d = drag.current;
        drag.current = null;
        if (!d?.moved) return;
        setDragging(false);
        if (dismisses(e.clientY - d.y, panel.current.offsetHeight, d.speed)) onClose();
        else setDy(0);
    }
    function cancel() {
        drag.current = null;
        setDragging(false);
        setDy(0);
    }
    // Presses inside go no further than the sheet, so the message or list
    // it opened from doesn't take them for its own.
    return (
        <div class="fixed inset-0 z-50 flex flex-col justify-end bg-black/40" {...backdrop}
            onPointerDown={(e) => {
                backdrop.onPointerDown(e);
                e.stopPropagation();
            }}>
            <div ref={panel} role="dialog" aria-modal="true" aria-label={label} data-sheet onKeyDown={keys}
                class={`max-h-[85%] overflow-y-auto overscroll-contain rounded-t-box bg-base-100 pb-[env(safe-area-inset-bottom)] shadow-xl ${scrolls ? 'touch-pan-y' : 'touch-none'}`}
                style={{ transform: dy ? `translateY(${dy}px)` : undefined, transition: dragging ? 'none' : 'transform 150ms ease-out' }}
                onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={cancel}>
                <div data-sheet-top class="touch-none">
                    <div class="flex justify-center pb-1 pt-2" aria-hidden="true">
                        <span class="h-1 w-10 rounded-full bg-base-content/30"></span>
                    </div>
                    {title && <div class="cursor-default select-none px-4 pb-2 text-sm">{title}</div>}
                </div>
                <div role="menu" aria-label={label} class="flex flex-col pb-2">{children}</div>
            </div>
        </div>
    );
}

// SheetRow is a sheet's row, 48 pixels tall, for an action, whose onClick
// closes the sheet as it runs it.
export function SheetRow({ danger, disabled, onClick, children }) {
    return (
        <button type="button" role="menuitem" disabled={disabled} onClick={onClick}
            class={`flex h-12 w-full shrink-0 items-center gap-3 px-4 text-left hover:bg-base-200 focus-visible:bg-base-200 focus-visible:outline-none active:bg-base-300 disabled:opacity-50 ${danger ? 'text-error' : ''}`}>
            {children}
        </button>
    );
}
