// Small shared pieces. Everything shown here goes through Preact's text
// rendering; nothing sets HTML from a string.

import { createContext } from 'preact';
import { useContext, useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { useOver } from './animations.js';
import { onBackground } from './background.js';
import { copyText } from './clipboard.js';
import { InfoIcon, StatusIcon } from './icons.jsx';
import { nameText, useNameLook } from './names.jsx';
import { onNotifications } from './notify.js';
import { markURL } from './platform.js';
import { hexColor } from './themes.js';
import { tipShift } from './touch.js';

// Mark is Dens's mark, where a screen says it's Dens: the first run's, and
// Home with no den.
export function Mark({ size = 'h-14 w-14' }) {
    const src = markURL();
    return src ? <img src={src} alt="Dens" class={`${size} shrink-0 rounded-box`} draggable={false} data-mark /> : null;
}

// Lead is a screen with one thing to do, as the first run's and an empty
// Home: the mark over it, in the middle of the window.
export function Lead({ children }) {
    return (
        <div class="my-auto flex w-full flex-col items-center gap-6 py-6" data-lead>
            <Mark />
            <div class="flex w-full flex-col gap-4">{children}</div>
        </div>
    );
}

export function Card({ title, children, class: extra = '', ...rest }) {
    return (
        <section class={`card bg-base-200 ${extra}`} {...rest}>
            <div class="card-body gap-3">
                {title && <h2 class="card-title">{title}</h2>}
                {children}
            </div>
        </section>
    );
}

// The page's four kinds of button (M22.3), each with one job, so a look
// means the same wherever it shows. Spelled out so Tailwind keeps every
// class.
const KINDS = { primary: 'btn-primary', secondary: 'btn-soft', quiet: 'btn-ghost', danger: 'btn-ghost text-error' };
const BUTTON_SIZES = { xs: 'btn-xs', sm: 'btn-sm', md: '', lg: 'btn-lg' };

/** buttonClass is a button's classes by its kind and size, for what can't
    be a Button, as a link or a menu's own button. A danger button filled
    is the one that confirms in a dialog; a danger button anywhere else is
    red words. */
export function buttonClass(kind = 'secondary', size = 'sm', filled = false) {
    const look = kind === 'danger' && filled ? 'btn-error' : KINDS[kind] || KINDS.secondary;
    return `btn ${look} ${BUTTON_SIZES[size] ?? ''}`.trim();
}

/** Button is a button of one of the page's kinds: primary, the one action
    a view or a dialog leads with; secondary, any other that stands alone;
    quiet, a row's or a toolbar's; and danger. busy shows a spinner and
    holds it until the action ends. */
export function Button({ kind, size, filled, busy = false, type = 'button', class: extra = '', disabled, children, ...rest }) {
    return (
        <button type={type} class={`${buttonClass(kind, size, filled)} ${extra}`.trim()} disabled={disabled || busy} {...rest}>
            {busy && <span class="loading loading-spinner loading-sm"></span>}
            {children}
        </button>
    );
}

// TILE is a choice of a few shown as a row of equal tiles, its radio
// hidden inside it, ringed while chosen: a theme, or a den's call limits.
export const TILE = 'flex cursor-pointer flex-col items-center gap-1 rounded-box bg-base-100 px-2 py-3 text-center hover:bg-base-300 max-md:bg-base-200 has-[:checked]:ring-2 has-[:checked]:ring-primary has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-primary';

// InSheet says a dialog shows as a phone's sheet, where its actions share
// its width.
export const InSheet = createContext(false);

/** Actions are a dialog's buttons, or a step's within a section: a row at
    its foot, its primary first, at the left, then Cancel or Back; in a
    phone's sheet they share its width. */
export function Actions({ class: extra = '', children }) {
    const sheet = useContext(InSheet);
    return <div data-actions class={`flex flex-wrap items-center gap-2 ${sheet ? '*:flex-1' : ''} ${extra}`}>{children}</div>;
}

// Where a tip opens, spelled out so Tailwind keeps every class. Below,
// centered on the i, unless the i sits near a panel's left edge, which
// would cut a centered tip off: then to its right, from the i's top down,
// so one by a panel's top edge isn't cut off either. One that would still
// run past the window's edge, or a list's around it, moves in from it, and
// one with no room below opens above.
const SIDES = {
    bottom: 'tooltip-bottom',
    right: 'tooltip-right [--tt-inset:0_auto] [--tt-trans:0]',
    top: 'tooltip-top',
};

// clipOf is the part of the window that the boxes around el leave showing:
// the window, less any box around it that clips what runs past its edges,
// as a list that scrolls does, which would cut a tip or a menu in it off.
export function clipOf(el) {
    const out = { left: 0, top: 0, right: document.documentElement.clientWidth, bottom: innerHeight };
    for (let p = el.parentElement; p; p = p.parentElement) {
        const s = getComputedStyle(p);
        if (s.overflowX === 'visible' && s.overflowY === 'visible') continue;
        const r = p.getBoundingClientRect();
        if (s.overflowX !== 'visible') {
            out.left = Math.max(out.left, r.left);
            out.right = Math.min(out.right, r.right);
        }
        if (s.overflowY !== 'visible') {
            out.top = Math.max(out.top, r.top);
            out.bottom = Math.min(out.bottom, r.bottom);
        }
    }
    return out;
}

// useTip opens a tip on hover by a mouse, on keyboard focus, and on a tap,
// which a touch screen has instead of hover (M13.2): a tap's tip stays
// until the next tap, on it or anywhere else, or Escape. side is where it
// opens if there's room; its place moves to stay within the window, and
// within a list around it that would clip it, which it wraps to fit. A
// reaction's chip opens its own on a long press (M15.3).
export function useTip(side) {
    const wrap = useRef(null);
    const content = useRef(null);
    const [hover, setHover] = useState(false);
    const [focus, setFocus] = useState(false);
    const [pinned, setPinned] = useState(false);
    const [place, setPlace] = useState({ side, shift: 0 });
    const open = hover || focus || pinned;
    useEffect(() => {
        if (!pinned) return undefined;
        const away = (e) => !wrap.current?.contains(e.target) && setPinned(false);
        const escape = (e) => e.key === 'Escape' && setPinned(false);
        document.addEventListener('pointerdown', away, true);
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('pointerdown', away, true);
            document.removeEventListener('keydown', escape);
        };
    }, [pinned]);
    // Where the tip goes is reckoned from its i, which stays put, rather
    // than from the tip, whose move to its last place may still be under
    // way, so it settles at once.
    useLayoutEffect(() => {
        if (!open) {
            if (place.side !== side || place.shift) setPlace({ side, shift: 0 });
            return;
        }
        const el = content.current;
        const at = wrap.current?.getBoundingClientRect();
        if (!el || !at) return;
        const bounds = clipOf(wrap.current);
        const room = Math.max(0, Math.round(bounds.right - bounds.left - 16));
        const w = Math.min(el.offsetWidth, room);
        const h = el.offsetHeight;
        let next = place.side;
        if (next === 'right' && at.right + 8 + w > bounds.right - 8) next = 'bottom';
        if (next === 'bottom' && at.bottom + 8 + h > bounds.bottom - 8 && at.top - 8 - h > bounds.top + 8) next = 'top';
        const left = at.left + at.width / 2 - w / 2;
        const shift = next === 'right' ? 0 : Math.round(tipShift({ left, right: left + w, width: w }, bounds));
        if (next !== place.side || shift !== place.shift || room !== place.room) setPlace({ side: next, shift, room });
    }, [open, place]);
    return {
        open,
        pinned,
        setPinned,
        wrapProps: {
            ref: wrap,
            class: `tooltip ${SIDES[place.side]} ${open ? 'tooltip-open' : ''}`,
            onPointerEnter: (e) => e.pointerType === 'mouse' && setHover(true),
            onPointerLeave: (e) => e.pointerType === 'mouse' && setHover(false),
        },
        contentProps: {
            ref: content,
            hidden: !open,
            style: {
                ...(place.shift ? { '--tt-trans': `calc(-50% + ${Math.round(place.shift)}px)` } : {}),
                ...(place.room ? { maxWidth: `min(18rem, ${place.room}px)` } : {}),
            },
        },
        triggerProps: {
            onFocus: (e) => setFocus(e.currentTarget.matches?.(':focus-visible') ?? false),
            onBlur: () => setFocus(false),
        },
    };
}

// A tip fades in, without sliding from where it would have run past the
// window's edge.
export const TIP = 'tooltip-content max-w-72 rounded-lg px-3 py-2 text-left text-sm font-normal normal-case [transition-property:opacity]';

// The i, or what stands in its place, is at least 24 pixels each way, and
// takes room around it on a touch screen.
const TRIGGER = 'relative inline-flex min-h-6 min-w-6 items-center justify-center rounded p-1 text-base-content/60 hover:text-base-content touch-room';

// Tip is an i beside a label that says more: the detail a one-line hint
// leaves out (M8.3), on hover or focus, or a tap (M13.2). trigger stands in
// the i's place, as a mark whose meaning a title held, and label names it.
// wrap is how its box sits in a line of text, or in a row of flex boxes.
export function Tip({ id, side = 'bottom', label = 'More about this', trigger, wrap = 'align-middle', children }) {
    const tip = useTip(side);
    return (
        <span {...tip.wrapProps} class={`${tip.wrapProps.class} ${wrap}`}>
            <span id={id} role="tooltip" class={TIP} {...tip.contentProps}>
                {children}
            </span>
            <button type="button" class={`${TRIGGER} cursor-help`} aria-label={label} aria-describedby={id} {...tip.triggerProps}
                onClick={() => tip.setPinned(!tip.pinned)}>
                {trigger || <InfoIcon />}
            </button>
        </span>
    );
}

// HoverTip is a mark whose tip shows while a mouse is over it (M17.6), for
// a mark inside a row or a tile that is itself a button, which a second
// button can't sit in, and where a tap belongs to what it's in: a role's
// mark in the member list, whose card says the same at a tap. label, if
// given, is read with what it's in, as the mark's words; the tip itself is
// for the eye alone.
export function HoverTip({ side = 'bottom', label, tip: content, children }) {
    const tip = useTip(side);
    return (
        <span {...tip.wrapProps} class={`${tip.wrapProps.class} inline-flex align-middle`}>
            <span class={TIP} aria-hidden="true" {...tip.contentProps}>{content}</span>
            {label && <span class="sr-only">{label}</span>}
            <span class="inline-flex" aria-hidden="true">{children}</span>
        </span>
    );
}

// Choices is a setting of a few, in a row under its label, and tip, if
// given, says more beside the label.
export function Choices({ label, name, data, choice, choices, onPick, tip }) {
    return (
        <div class="flex flex-col gap-1">
            <span class="flex items-center text-sm">
                {label}
                {tip && <Tip id={`${name}-tip`} label={`About ${label}`} wrap="-my-1 inline-flex">{tip}</Tip>}
            </span>
            <div role="radiogroup" aria-label={label} class="flex flex-wrap gap-x-6 gap-y-2" {...{ [data]: true }}>
                {choices.map(([value, text]) => (
                    <label key={value} class="flex min-h-6 cursor-pointer items-center gap-2">
                        <input type="radio" class="radio radio-sm" name={name} value={value} checked={choice === value} onChange={() => onPick(value)} />
                        <span>{text}</span>
                    </label>
                ))}
            </div>
        </div>
    );
}

// ColorField takes a color: the browser's picker, and beside it the
// color's hex, the same in every browser, which takes a typed or pasted
// one once it reads as a color.
export function ColorField({ label, value, onChange }) {
    const [typed, setTyped] = useState(value);
    useEffect(() => {
        setTyped((t) => (hexColor(t) === value ? t : value));
    }, [value]);
    return (
        <div class="flex items-center gap-3" data-color={label}>
            <span class="min-w-0 flex-1 text-sm">{label}</span>
            <input type="color" class="h-8 w-12 shrink-0 cursor-pointer rounded-field border border-base-content/20 bg-transparent p-0.5"
                aria-label={`${label}, picked`} value={value} onInput={(e) => onChange(e.currentTarget.value.toLowerCase())} />
            <input type="text" class="input input-sm w-24 shrink-0 font-mono" aria-label={`${label}, as hex`} value={typed} maxlength="7" {...PLAIN}
                onInput={(e) => {
                    const text = e.currentTarget.value;
                    setTyped(text);
                    const hex = hexColor(text);
                    if (hex && hex !== value) onChange(hex);
                }}
                onBlur={() => setTyped(value)} />
        </div>
    );
}

// A long name scrolls at this many pixels a second.
const NAME_SPEED = 40;

/** nameScroll is how far a name full pixels wide scrolls in a box shown
    pixels wide to show its end, and how long that takes, in ms; null for a
    name that fits */
export function nameScroll(full, shown) {
    const by = Math.ceil(full - shown);
    return by > 1 ? { by, ms: Math.round((by / NAME_SPEED) * 1000) } : null;
}

/** LongName is a name cut to its row's width, which scrolls to its end
    while a mouse rests on the row, a HoverRow, and back as it leaves: the
    row measures how far, and the stylesheet moves it. A touch screen has
    no hover, so there it stays cut. With its member, it shows as they
    styled it. */
export function LongName({ name, member }) {
    const over = useOver();
    const look = useNameLook(member);
    const box = useRef(null);
    const [scroll, setScroll] = useState(null);
    useLayoutEffect(() => {
        const el = box.current;
        setScroll(over && el ? nameScroll(el.scrollWidth, el.clientWidth) : null);
    }, [over, name, look]);
    const style = scroll ? { ...look.style, '--name-by': `${scroll.by}px`, '--name-ms': `${scroll.ms}ms` } : look.style;
    return (
        <span ref={box} class={`name-scroll truncate ${look.class}`} data-scrolling={scroll ? '' : undefined} style={style} data-name>
            {nameText(name, look)}
        </span>
    );
}

// The public guides, which explain what few apps ask people to understand.
export const DOCS = 'https://dens.chat/docs/';

// DocsTip is a Tip whose i opens the guide to its subject, in a new tab:
// the gist on hover, the whole story a click away. It stands where those
// things come up (v1's onboarding), and opens where a Tip would. A tap's
// first opens the tip, and a second the guide (M13.2).
export function DocsTip({ id, page, side = 'bottom', children }) {
    const tip = useTip(side);
    const touched = useRef(false);
    return (
        <span {...tip.wrapProps} class={`${tip.wrapProps.class} align-middle font-normal`}>
            <span id={id} role="tooltip" class={TIP} {...tip.contentProps}>
                {children}
                <span class="mt-1 block text-xs opacity-70">{tip.pinned ? 'Tap the i again to open the guide on dens.chat.' : 'Opens the guide on dens.chat.'}</span>
            </span>
            <a href={DOCS + page} target="_blank" rel="noopener noreferrer" class={TRIGGER} aria-label="Open the guide" aria-describedby={id}
                {...tip.triggerProps}
                onPointerDown={(e) => {
                    touched.current = e.pointerType === 'touch' || e.pointerType === 'pen';
                }}
                onClick={(e) => {
                    const first = touched.current && !tip.pinned;
                    touched.current = false;
                    if (first) e.preventDefault();
                    tip.setPinned(first);
                }}>
                <InfoIcon />
            </a>
        </span>
    );
}

// DocsLink opens the guide to its subject in a new tab, where a dialog
// needs nothing more than the way there: label says where it leads, as a
// link of words or, with icon, the i alone, named by label.
export function DocsLink({ page, label, icon = false }) {
    const props = { href: DOCS + page, target: '_blank', rel: 'noopener noreferrer' };
    if (icon) {
        return (
            <a {...props} class={`${TRIGGER} align-middle`} aria-label={label} title={label} data-docs-link>
                <InfoIcon />
            </a>
        );
    }
    return <a {...props} class="link link-primary" data-docs-link>{label}</a>;
}

// Field is a control under its label, in the one style a label takes
// (M22.3): regular weight, in the text's color, above its control, and a
// hint under it if it needs one.
export function Field({ label, hint, children }) {
    return (
        <fieldset class="fieldset">
            <legend class="fieldset-legend text-sm font-normal">{label}</legend>
            {children}
            {hint && <p class="text-sm text-base-content/70">{hint}</p>}
        </fieldset>
    );
}

// Toggle is a setting that's on or off, one size wherever it shows
// (M22.3): its switch, its label beside it, a line on what it does if it
// needs one, and a tip for the rest.
export function Toggle({ id, label, hint, tip, checked, disabled, onChange, ...rest }) {
    return (
        <div class="flex items-start gap-3">
            <input id={id} type="checkbox" class="toggle mt-0.5 shrink-0" checked={checked} disabled={disabled}
                onChange={(e) => onChange(e.currentTarget.checked)} {...rest} />
            <div class="min-w-0">
                <label for={id} class={disabled ? '' : 'cursor-pointer'}>{label}</label>
                {tip && <> <Tip id={`${id}-tip`}>{tip}</Tip></>}
                {hint && <p class="text-sm text-base-content/70">{hint}</p>}
            </div>
        </div>
    );
}

/** Slider is the page's one slider (M22.3): a thin track filled in the
    theme's primary color up to its value, a small thumb, and a box tall
    enough for a finger. Chromium draws the fill from --fill alone, which
    runs to the thumb's middle. */
export function Slider({ value, min = 0, max = 100, class: extra = '', style, ...rest }) {
    const at = Number(max) > Number(min) ? (Number(value) - Number(min)) / (Number(max) - Number(min)) : 0;
    const fill = Math.min(1, Math.max(0, at));
    return (
        <input type="range" class={`slider ${extra}`} min={min} max={max} value={value}
            style={{ '--fill': `calc(0.5rem + (100% - 1rem) * ${fill.toFixed(4)})`, ...style }} {...rest} />
    );
}

/** FilePick takes a file through the page's own button (M22.3), the same
    in every browser, the chosen one's name beside it; inputProps reach
    the browser's input, hidden behind it. */
export function FilePick({ label, accept, file, onPick, disabled, inputProps = {} }) {
    const input = useRef(null);
    return (
        <div class="flex min-w-0 items-center gap-3">
            <Button disabled={disabled} class="shrink-0" onClick={() => input.current?.click()}>{label}</Button>
            {file && <span class="min-w-0 truncate text-sm" data-picked>{file.name}</span>}
            <input ref={input} type="file" class="hidden" accept={accept} {...inputProps} onChange={(e) => {
                const picked = e.currentTarget.files?.[0];
                e.currentTarget.value = '';
                if (picked) onPick(picked);
            }} />
        </div>
    );
}

// What a phone's keyboard offers a field (M13.2): a username, an invite, a
// code or a key takes no capitals or corrections, PLAIN, and an address
// asks for the keyboard for addresses too, ADDRESS.
export const PLAIN = { autocapitalize: 'none', autocorrect: 'off', spellcheck: false };
export const ADDRESS = { ...PLAIN, inputmode: 'url' };

export function TextInput({ value, onInput, type = 'text', ...rest }) {
    return <input class="input w-full" type={type} value={value} onInput={(e) => onInput(e.currentTarget.value)} {...rest} />;
}

// useLater reports whether active has stayed true for ms, so a wait that
// ends quickly never flashes a spinner.
export function useLater(active, ms) {
    const [later, setLater] = useState(false);
    useEffect(() => {
        setLater(false);
        if (!active) return undefined;
        const timer = setTimeout(() => setLater(true), ms);
        return () => clearTimeout(timer);
    }, [active, ms]);
    return active && later;
}

// Dates take one form everywhere (M8.3), the medium one in the reader's
// locale, as Oct 12, 2026; locale is for the tests.

// day shows a time as a date.
export function day(ms, locale) {
    return new Date(ms).toLocaleDateString(locale, { dateStyle: 'medium' });
}

// dayTime shows a time as a date and the time of day, where the hour
// matters, as when an invite expires.
export function dayTime(ms, locale) {
    return new Date(ms).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
}

// clock shows the time of day alone, as when a wait ends.
export function clock(ms, locale) {
    return new Date(ms).toLocaleTimeString(locale, { timeStyle: 'short' });
}

// Waiting is a spinner that says what it waits for once it has taken a
// while, so a slow start doesn't look like a hang.
export function Waiting({ label }) {
    const slow = useLater(true, 1500);
    return (
        <div class="flex items-center gap-2 text-sm text-base-content/70" role="status">
            <span class="loading loading-spinner"></span>
            {slow && <span>{label}</span>}
        </div>
    );
}

// NOTICE_LOOKS are a status's colors: its edge and its icon's, while its
// words keep the text's color, which reads on every theme.
const NOTICE_LOOKS = {
    info: ['border-info', 'text-info'],
    success: ['border-success', 'text-success'],
    warning: ['border-warning', 'text-warning'],
    error: ['border-error', 'text-error'],
};

/** Notice is status in words (M22.3): what happened, or what waits, in the
    text's color, its state an icon and an edge in the state's color. role
    is the status's own unless given, an alert for an error. */
export function Notice({ kind = 'info', role, class: extra = '', children, ...rest }) {
    const [edge, mark] = NOTICE_LOOKS[kind] || NOTICE_LOOKS.info;
    return (
        <div role={role ?? (kind === 'error' ? 'alert' : 'status')} data-notice={kind}
            class={`flex items-start gap-2 rounded-box border-l-4 bg-base-content/5 px-3 py-2 text-sm ${edge} ${extra}`} {...rest}>
            <span class={`mt-0.5 ${mark}`}><StatusIcon kind={kind} /></span>
            <div class="flex min-w-0 flex-1 flex-col items-start gap-2">{children}</div>
        </div>
    );
}

/** StatusText is a line of status beside what it's about, as "Saved." is
    beside Save: its words in the text's color, its state an icon in the
    state's color; inline, it sits in a line of other things. */
export function StatusText({ kind = 'info', inline = false, size = 'sm', class: extra = '', children, ...rest }) {
    const [, mark] = NOTICE_LOOKS[kind] || NOTICE_LOOKS.info;
    const Tag = inline ? 'span' : 'p';
    return (
        <Tag data-status={kind} class={`${inline ? 'inline-flex' : 'flex'} items-center gap-1.5 ${size === 'xs' ? 'text-xs' : 'text-sm'} ${extra}`} {...rest}>
            <span class={mark}><StatusIcon kind={kind} size={size === 'xs' ? 'h-3 w-3' : 'h-3.5 w-3.5'} /></span>
            <span class="min-w-0">{children}</span>
        </Tag>
    );
}

export function ErrorText({ message }) {
    if (!message) return null;
    return <Notice kind="error">{message}</Notice>;
}

export function SubmitButton({ busy, kind = 'primary', children, ...rest }) {
    return <Button type="submit" kind={kind} busy={busy} {...rest}>{children}</Button>;
}

// useBackdrop gives a dialog's backdrop what closes the dialog on a click
// outside it. A press that starts inside the dialog, as when selecting
// text, and is let go outside it ends in a click on the backdrop too, so a
// click counts only when its press didn't start inside (M11).
export function useBackdrop(onClose) {
    const inside = useRef(false);
    return {
        onPointerDown: (e) => {
            inside.current = e.target !== e.currentTarget;
        },
        onClick: (e) => {
            const pressedInside = inside.current;
            inside.current = false;
            if (e.target === e.currentTarget && !pressedInside) onClose();
        },
    };
}

// useAction runs an async action once at a time, keeping its error.
export function useAction() {
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    async function run(fn) {
        if (busy) return;
        setBusy(true);
        setError('');
        try {
            await fn();
        } catch (e) {
            setError(e.message || String(e));
        } finally {
            setBusy(false);
        }
    }
    return { busy, error, setError, run };
}

// CopyButton copies text, and says for a moment whether it could. Where
// the browser lets nothing copy, the text stays there to select.
export function CopyButton({ text, label = 'Copy' }) {
    const [said, setSaid] = useState('');
    const timer = useRef(null);
    useEffect(() => () => clearTimeout(timer.current), []);
    async function copy() {
        let result = 'Copied';
        try {
            await copyText(text);
        } catch {
            result = "Couldn't copy";
        }
        setSaid(result);
        clearTimeout(timer.current);
        timer.current = setTimeout(() => setSaid(''), 2000);
    }
    return (
        <Button class="shrink-0" onClick={copy}>
            {said || label}
        </Button>
    );
}

// PasswordFields asks for a new password twice.
export function PasswordFields({ password, setPassword, confirm, setConfirm, hint }) {
    return (
        <>
            <Field label="Password (at least 8 characters)" hint={hint}>
                <TextInput type="password" value={password} onInput={setPassword} autocomplete="new-password" required minlength="8" />
            </Field>
            <Field label="Confirm password">
                <TextInput type="password" value={confirm} onInput={setConfirm} autocomplete="new-password" required minlength="8" />
            </Field>
        </>
    );
}

export function checkPasswords(password, confirm) {
    if (password.length < 8) return 'The password needs at least 8 characters.';
    if (password !== confirm) return "The passwords don't match.";
    return '';
}

// useNotifications is the service's status of notifications (M12.3): the
// member's settings, by kind, with what they muted, and on Android, the
// shell's word; null until it comes.
export function useNotifications() {
    const [status, setStatus] = useState(null);
    useEffect(() => onNotifications(setStatus), []);
    return status;
}

// useBackground is the picture behind the chat, its version and dim, or
// null for none.
export function useBackground() {
    const [background, setBackground] = useState(null);
    useEffect(() => onBackground(setBackground), []);
    return background;
}
