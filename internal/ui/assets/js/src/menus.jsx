// Right-click menus. On a desktop, a right click on a message, or
// on a member's row in the member list or a call, opens that row's menu
// where the pointer is: what its ⋯ holds, or for a member in the list,
// what their card does. Over a link, a picture, a video, a player, a field
// or selected text, the browser's own menu opens, so copying and saving
// them keep working. Touch screens keep their long presses and sheets.

import { useEffect, useLayoutEffect, useRef } from 'preact/hooks';
import { clipOf } from './components.jsx';

// What a right click leaves to the browser: links, players and fields,
// a YouTube cover among the players. Pictures are left to it too, but for
// a member's and an emoji, which stand for the row's member and the
// message's words.
const FIELDS = new Set(['video', 'audio', 'iframe', 'input', 'textarea', 'select']);

function browsers(el) {
    return FIELDS.has(el.localName) || (el.localName === 'a' && el.hasAttribute('href')) || !!el.isContentEditable || el.hasAttribute('data-video');
}

/** ownsMenu says whether a right click on target, in row, opens the page's
    menu rather than the browser's: not over a link, a picture, a video, a
    player or a field, nor while text in row is selected */
export function ownsMenu(target, selection, row) {
    let picture = false;
    for (let el = target; el?.nodeType === 1; el = el.parentNode) {
        if (browsers(el)) return false;
        if (el.localName === 'img') picture = true;
        else if (el.hasAttribute('data-avatar') || el.hasAttribute('data-emoji')) picture = false;
        if (el === row) break;
    }
    if (picture) return false;
    return !(selection && !selection.isCollapsed && selection.toString().trim() && selection.containsNode(row, true));
}

/** rightClick is where a right click on row opens its menu, as a point in
    the window, or null for one whose point lies outside the row, as the
    keyboard's menu key can send: that menu opens where its ⋯'s would */
export function rightClick(e, row) {
    const r = row.getBoundingClientRect();
    const inside = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
    return inside ? { x: e.clientX, y: e.clientY } : null;
}

/** placeMenu is where a menu of size opens at the point at, in the
    window's terms: below and right of it, or above or left of it where
    clip, the part of the window it shows in, has no room, but never past
    clip's top or left edge */
export function placeMenu(at, size, clip, gap = 4) {
    let left = at.x;
    let top = at.y;
    if (left + size.width > clip.right - gap) left = at.x - size.width;
    if (top + size.height > clip.bottom - gap) top = at.y - size.height;
    return { left: Math.max(clip.left + gap, left), top: Math.max(clip.top + gap, top) };
}

/** usePlaced puts a menu opened at a point, at, where placeMenu says, in
    its positioned box: on the menu's own style, as it's laid out and
    before it's painted, so it shows nowhere else and takes no second
    render. A menu its ⋯ opened, at null, stays where its classes put it. */
export function usePlaced(at, menu) {
    useLayoutEffect(() => {
        const el = menu.current;
        if (!at || !el?.offsetParent) return;
        const clip = clipOf(el);
        // Where it first lay may have run past its list's edge, giving the
        // list scroll bars that moved the menu's box; placed, it no longer
        // does, so it's placed again against where its box is then.
        for (let i = 0; i < 2; i++) {
            const box = el.offsetParent;
            const b = box.getBoundingClientRect();
            const p = placeMenu(at, { width: el.offsetWidth, height: el.offsetHeight }, clip);
            el.style.left = `${Math.round(p.left - b.left - box.clientLeft + box.scrollLeft)}px`;
            el.style.top = `${Math.round(p.top - b.top - box.clientTop + box.scrollTop)}px`;
        }
    }, [at]);
}

/** PointerMenu is a menu opened at the point at, for a row that has no ⋯
    to open it from: items, each a key, a label, a run, and maybe danger
    or disabled. Arrow keys move through it, and Escape, Tab, a pick or a
    press elsewhere closes it; Escape and a pick give the focus back to
    back, the row */
export function PointerMenu({ at, label, items, back, onClose }) {
    const list = useRef(null);
    usePlaced(at, list);
    const closing = useRef(onClose);
    closing.current = onClose;
    useEffect(() => {
        list.current?.querySelector('[role="menuitem"]:not(:disabled)')?.focus();
        const away = (e) => !list.current?.contains(e.target) && closing.current();
        document.addEventListener('pointerdown', away);
        return () => document.removeEventListener('pointerdown', away);
    }, []);
    function close() {
        onClose();
        back?.focus();
    }
    function keys(e) {
        const all = [...list.current.querySelectorAll('[role="menuitem"]:not(:disabled)')];
        const at = all.indexOf(document.activeElement);
        const to = { ArrowDown: at + 1, ArrowUp: at - 1, Home: 0, End: all.length - 1 }[e.key];
        if (to !== undefined && all.length) {
            e.preventDefault();
            all[(to + all.length) % all.length].focus();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            close();
        } else if (e.key === 'Tab') {
            onClose();
        }
    }
    return (
        <ul ref={list} role="menu" aria-label={label} onKeyDown={keys} data-pointer-menu
            class="menu absolute left-0 top-0 z-20 w-52 rounded-box bg-base-100 p-1 shadow">
            {items.map((a) => (
                <li key={a.key} role="none">
                    <button type="button" role="menuitem" tabIndex={-1} disabled={a.disabled} class={a.danger ? 'text-error' : ''}
                        onClick={() => {
                            close();
                            a.run();
                        }}>
                        {a.label}
                    </button>
                </li>
            ))}
        </ul>
    );
}
