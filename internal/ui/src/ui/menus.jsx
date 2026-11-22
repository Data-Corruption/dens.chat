// Right-click menus. On a desktop, a right click on a message, or
// on a member's row in the member list or a call, opens that row's menu
// where the pointer is: what its ⋯ holds, or for a member in the list,
// what their card does. Over a link, a picture, a video, a player, a field
// or selected text, the browser's own menu opens, so copying and saving
// them keep working. Touch screens keep their long presses and sheets.
//
// Every menu draws its items here, each with its icon: as rows of a
// dropdown or of a menu at the pointer, or as a phone's sheet's rows; an
// item with items of its own opens them beside it, or in a sheet in its
// place.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { clipOf } from './components.jsx';
import { BackIcon, MenuIcon, OnwardIcon } from './icons.jsx';
import { useNarrow } from '../core/nav.js';
import { Sheet, SheetRow } from './sheet.jsx';

// MENU is every menu's surface: a step off the page's color, with
// an edge and a shadow, so it reads as a layer over a list or a message
// on every theme.
export const MENU = 'menu flex-nowrap rounded-box border border-base-content/10 bg-base-200 p-1 shadow-lg';

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

/** menuKeys moves the focus through list's own items, not a flyout's, by
    the arrow keys, Home and End; Escape runs close, and Tab tabbed */
export function menuKeys(e, list, close, tabbed) {
    const all = [...list.querySelectorAll('[role^="menuitem"]:not(:disabled)')].filter((el) => el.closest('[role="menu"]') === list);
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
        tabbed();
    }
}

/** MenuItems are a menu's rows: items, each a key, a label, an icon of
    icons.jsx's MENU_ICONS, a run, and maybe danger or disabled, or items
    of its own, which open beside it, or checked, for one of a few choices,
    true for the one that holds. picked closes the menu as an item
    runs, the run getting what picked returned. Rows the arrow keys move
    through are out of the Tab order, but in a menu that opens with the
    focus elsewhere, tabbable. */
export function MenuItems({ items, picked, tabbable = false }) {
    const [sub, setSub] = useState('');
    const tab = tabbable ? undefined : -1;
    return items.map((a) => (a.items ? (
        <Flyout key={a.key} item={a} open={sub === a.key} onOpen={(on) => setSub(on ? a.key : '')} picked={picked} tab={tab} />
    ) : (
        <li key={a.key} role="none">
            <button type="button" role={a.checked === undefined ? 'menuitem' : 'menuitemradio'} aria-checked={a.checked} tabIndex={tab}
                disabled={a.disabled} data-menu-item={a.key} class={`pointer-coarse:min-h-11 ${a.danger ? 'text-error' : ''}`}
                onClick={() => a.run(picked())}>
                <MenuIcon name={a.icon} />
                {a.checked === undefined ? a.label : <><span class="flex-1">{a.label}</span><Check on={a.checked} /></>}
            </button>
        </li>
    )));
}

// Check marks the choice that holds among a menu's few.
function Check({ on }) {
    return <span class={`h-3.5 w-3.5 ${on ? '' : 'invisible'}`}><MenuIcon name="read" size="h-3.5 w-3.5" /></span>;
}

/** flyoutAt is where a flyout of size opens beside its item's box, row,
    in a window of width and height: to its right, or to its left where
    only that side has room, its top a step above the item's, and up
    from there as far as it takes to end inside the window */
export function flyoutAt(row, size, width, height, gap = 4) {
    const left = row.right + size.width > width - gap && row.left - size.width >= gap;
    const top = Math.max(gap, Math.min(row.top - gap, height - gap - size.height));
    return { left: left ? row.left - size.width : row.right, top, toLeft: left };
}

// Flyout is an item whose items open beside it: on a hover by a mouse, a
// click, Enter or the right arrow, and close on its leaving, the left
// arrow or Escape, the focus back on the item. It's fixed to the window,
// since the menus it opens from sit in lists that scroll and would clip
// it, and its box reaches back over the gap to the item, so the pointer
// crossing it doesn't close it.
function Flyout({ item, open, onOpen, picked, tab }) {
    const row = useRef(null);
    const box = useRef(null);
    const list = useRef(null);
    const keyed = useRef(false);
    useLayoutEffect(() => {
        if (!open || !box.current) return;
        const r = row.current.getBoundingClientRect();
        const at = flyoutAt(r, { width: box.current.offsetWidth, height: box.current.offsetHeight }, document.documentElement.clientWidth, innerHeight);
        const style = box.current.style;
        style.left = `${Math.round(at.left)}px`;
        style.top = `${Math.round(at.top)}px`;
        style.paddingLeft = at.toLeft ? '0' : '4px';
        style.paddingRight = at.toLeft ? '4px' : '0';
        if (keyed.current) list.current.querySelector('[role^="menuitem"]:not(:disabled)')?.focus();
    }, [open]);
    function back() {
        onOpen(false);
        row.current?.querySelector('[role^="menuitem"]')?.focus();
    }
    return (
        <li ref={row} role="none"
            onPointerEnter={(e) => e.pointerType === 'mouse' && onOpen(true)}
            onPointerLeave={(e) => e.pointerType === 'mouse' && onOpen(false)}>
            <button type="button" role="menuitem" tabIndex={tab} aria-haspopup="menu" aria-expanded={open} data-menu-item={item.key}
                class={`pointer-coarse:min-h-11 ${open ? 'bg-base-content/10' : ''}`}
                onClick={() => {
                    keyed.current = false;
                    onOpen(!open);
                }}
                onKeyDown={(e) => {
                    if (e.key !== 'ArrowRight' && e.key !== 'Enter' && e.key !== ' ') return;
                    e.preventDefault();
                    e.stopPropagation();
                    keyed.current = true;
                    onOpen(true);
                }}>
                <MenuIcon name={item.icon} />
                <span class="flex-1">{item.label}</span>
                <OnwardIcon size="h-3.5 w-3.5" />
            </button>
            {open && (
                <div ref={box} class="fixed left-0 top-0 z-30">
                    <ul ref={list} role="menu" aria-label={item.label} data-flyout={item.key}
                        class={`${MENU} max-h-80 w-52 overflow-y-auto`}
                        onKeyDown={(e) => {
                            e.stopPropagation();
                            if (e.key === 'ArrowLeft') {
                                e.preventDefault();
                                back();
                                return;
                            }
                            menuKeys(e, list.current, back, () => onOpen(false));
                        }}>
                        <MenuItems items={item.items} picked={picked} tabbable={tab === undefined} />
                    </ul>
                </div>
            )}
        </li>
    );
}

/** SheetItems are a menu's items as a sheet's rows, each with its icon;
    an item with items of its own opens them in the sheet in its place,
    under a row back. picked closes the sheet as an item runs. */
export function SheetItems({ items, picked }) {
    const [sub, setSub] = useState(null);
    if (sub) {
        return (
            <>
                <SheetRow onClick={() => setSub(null)}>
                    <BackIcon size="h-4 w-4" />
                    <span class="font-medium">{sub.label}</span>
                </SheetRow>
                <div role="group" aria-label={sub.label} data-flyout={sub.key} class="flex flex-col">
                    <SheetItems items={sub.items} picked={picked} />
                </div>
            </>
        );
    }
    return items.map((a) => (
        <SheetRow key={a.key} danger={a.danger} disabled={a.disabled} checked={a.checked} item={a.key} onClick={() => (a.items ? setSub(a) : a.run(picked()))}>
            <MenuIcon name={a.icon} />
            <span class="flex-1">{a.label}</span>
            {a.items && <OnwardIcon size="h-3.5 w-3.5" />}
            {a.checked !== undefined && <Check on={a.checked} />}
        </SheetRow>
    ));
}

/** PointerMenu is a menu opened at the point at, for a row that has no ⋯
    to open it from: items, as MenuItems takes them. Arrow keys move
    through it, and Escape, Tab, a pick or a press elsewhere closes it;
    Escape and a pick give the focus back to back, the row */
export function PointerMenu({ at, label, items, back, onClose }) {
    const list = useRef(null);
    usePlaced(at, list);
    const closing = useRef(onClose);
    closing.current = onClose;
    useEffect(() => {
        list.current?.querySelector('[role^="menuitem"]:not(:disabled)')?.focus();
        const away = (e) => !list.current?.contains(e.target) && closing.current();
        document.addEventListener('pointerdown', away);
        return () => document.removeEventListener('pointerdown', away);
    }, []);
    function close() {
        onClose();
        back?.focus();
    }
    return (
        <ul ref={list} role="menu" aria-label={label} onKeyDown={(e) => menuKeys(e, list.current, close, onClose)} data-pointer-menu
            class={`${MENU} absolute left-0 top-0 z-20 w-52`}>
            <MenuItems items={items} picked={close} />
        </ul>
    );
}

/** RowMenu is a row's menu wherever it opened: on the phone layout,
    narrow, a sheet titled title; elsewhere at the point at, or under the
    ⋯ that opened it, which back names */
export function RowMenu({ at, label, title, items, back, onClose }) {
    const narrow = useNarrow();
    if (narrow) {
        return (
            <Sheet label={label} title={title && <span class="font-semibold">{title}</span>} onClose={onClose}>
                <SheetItems items={items} picked={onClose} />
            </Sheet>
        );
    }
    return <PointerMenu at={at} label={label} items={items} back={back} onClose={onClose} />;
}

/** underOf is where a menu its ⋯, el, opened goes: under it, from its
    left, as a point PointerMenu takes */
export function underOf(el) {
    const r = el.getBoundingClientRect();
    return { x: r.left, y: r.bottom + 2 };
}

/** MenuButton is a button and the menu it opens: under it, or over it
    where the dialog or list it's in has no room below, from its right
    edge or its left as align says; on the phone layout, narrow, a sheet.
    button holds the button's class, label, title, data and children, and
    box the class of the box round it and the menu. */
export function MenuButton({ label, items, button, align = 'right', title, box: boxClass = '' }) {
    const narrow = useNarrow();
    const [open, setOpen] = useState(false);
    const [up, setUp] = useState(false);
    const box = useRef(null);
    const opener = useRef(null);
    const list = useRef(null);
    const dropdown = open && !narrow;
    useLayoutEffect(() => {
        if (!dropdown || !list.current) return;
        const clip = clipOf(box.current);
        const at = box.current.getBoundingClientRect();
        const below = clip.bottom - at.bottom;
        setUp(below < list.current.offsetHeight + 8 && at.top - clip.top > below);
    }, [dropdown]);
    useEffect(() => {
        if (!dropdown) return undefined;
        list.current?.querySelector('[role^="menuitem"]:not(:disabled)')?.focus();
        const away = (e) => !box.current?.contains(e.target) && setOpen(false);
        document.addEventListener('pointerdown', away);
        return () => document.removeEventListener('pointerdown', away);
    }, [dropdown]);
    // close gives the focus back to the button, unless what was picked
    // takes it.
    function close() {
        setOpen(false);
        opener.current?.focus();
        return opener.current;
    }
    const { children, class: style, ...rest } = button;
    return (
        <div ref={box} class={`relative flex ${boxClass}`}>
            <button ref={opener} type="button" class={style} aria-label={label} aria-haspopup="menu" aria-expanded={open} {...rest}
                onClick={() => setOpen(!open)}
                onKeyDown={(e) => {
                    if (e.key !== 'ArrowDown' || open) return;
                    e.preventDefault();
                    setOpen(true);
                }}>
                {children}
            </button>
            {dropdown && (
                <ul ref={list} role="menu" aria-label={label} onKeyDown={(e) => menuKeys(e, list.current, close, () => setOpen(false))}
                    class={`${MENU} absolute z-20 w-52 font-normal normal-case ${align === 'left' ? 'left-0' : 'right-0'} ${up ? 'bottom-full mb-1' : 'top-full mt-1'}`}>
                    <MenuItems items={items} picked={close} />
                </ul>
            )}
            {open && narrow && (
                <Sheet label={label} title={title && <span class="font-semibold">{title}</span>} onClose={() => setOpen(false)}>
                    <SheetItems items={items} picked={() => {
                        setOpen(false);
                        return opener.current;
                    }} />
                </Sheet>
            )}
        </div>
    );
}
