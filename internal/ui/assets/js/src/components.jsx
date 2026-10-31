// Small shared pieces. Everything shown here goes through Preact's text
// rendering; nothing sets HTML from a string.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { copyText } from './clipboard.js';
import { InfoIcon } from './icons.jsx';
import { onNotifications } from './notify.js';
import { tipShift } from './touch.js';

export function Card({ title, children, class: extra = '' }) {
    return (
        <section class={`card bg-base-200 ${extra}`}>
            <div class="card-body gap-3">
                {title && <h2 class="card-title">{title}</h2>}
                {children}
            </div>
        </section>
    );
}

// Where a tip opens, spelled out so Tailwind keeps every class. Below,
// centered on the i, unless the i sits near a panel's left edge, which
// would cut a centered tip off: then to its right, from the i's top down,
// so one by a panel's top edge isn't cut off either. One that would still
// run past the window's edge moves in from it, and one with no room below
// opens above.
const SIDES = {
    bottom: 'tooltip-bottom',
    right: 'tooltip-right [--tt-inset:0_auto] [--tt-trans:0]',
    top: 'tooltip-top',
};

// useTip opens a tip on hover by a mouse, on keyboard focus, and on a tap,
// which a touch screen has instead of hover (M13.2): a tap's tip stays
// until the next tap, on it or anywhere else, or Escape. side is where it
// opens if there's room; its place moves to stay within the window.
function useTip(side) {
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
        const w = el.offsetWidth;
        const h = el.offsetHeight;
        const vw = document.documentElement.clientWidth;
        let next = place.side;
        if (next === 'right' && at.right + 8 + w > vw - 8) next = 'bottom';
        if (next === 'bottom' && at.bottom + 8 + h > innerHeight - 8 && at.top - 8 - h > 8) next = 'top';
        const left = at.left + at.width / 2 - w / 2;
        const shift = next === 'right' ? 0 : Math.round(tipShift({ left, right: left + w, width: w }, vw));
        if (next !== place.side || shift !== place.shift) setPlace({ side: next, shift });
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
            style: place.shift ? { '--tt-trans': `calc(-50% + ${Math.round(place.shift)}px)` } : undefined,
        },
        triggerProps: {
            onFocus: (e) => setFocus(e.currentTarget.matches?.(':focus-visible') ?? false),
            onBlur: () => setFocus(false),
        },
    };
}

// A tip fades in, without sliding from where it would have run past the
// window's edge.
const TIP = 'tooltip-content max-w-72 rounded-lg px-3 py-2 text-left text-sm font-normal normal-case [transition-property:opacity]';

// The i, or what stands in its place, is at least 24 pixels each way, and
// takes room around it on a touch screen.
const TRIGGER = 'relative inline-flex min-h-6 min-w-6 items-center justify-center rounded p-1 text-base-content/60 hover:text-base-content touch-room';

// Tip is an i beside a label that says more: the detail a one-line hint
// leaves out (M8.3), on hover or focus, or a tap (M13.2). trigger stands in
// the i's place, as a mark whose meaning a title held, and label names it.
export function Tip({ id, side = 'bottom', label = 'More about this', trigger, children }) {
    const tip = useTip(side);
    return (
        <span {...tip.wrapProps} class={`${tip.wrapProps.class} align-middle`}>
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

export function Field({ label, hint, children }) {
    return (
        <fieldset class="fieldset">
            <legend class="fieldset-legend">{label}</legend>
            {children}
            {hint && <p class="label whitespace-normal">{hint}</p>}
        </fieldset>
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

export function ErrorText({ message }) {
    if (!message) return null;
    return (
        <div role="alert" class="alert alert-error alert-soft">
            <span>{message}</span>
        </div>
    );
}

export function SubmitButton({ busy, children, class: extra = 'btn-primary' }) {
    return (
        <button type="submit" class={`btn ${extra}`} disabled={busy}>
            {busy && <span class="loading loading-spinner loading-sm"></span>}
            {children}
        </button>
    );
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
        <button type="button" class="btn btn-sm" onClick={copy}>
            {said || label}
        </button>
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
