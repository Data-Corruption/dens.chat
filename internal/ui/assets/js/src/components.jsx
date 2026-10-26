// Small shared pieces. Everything shown here goes through Preact's text
// rendering; nothing sets HTML from a string.

import { useEffect, useRef, useState } from 'preact/hooks';
import { InfoIcon } from './icons.jsx';

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
// so one by a panel's top edge isn't cut off either.
const SIDES = { bottom: 'tooltip-bottom', right: 'tooltip-right [--tt-inset:0_auto] [--tt-trans:0]' };

// Tip is an i beside a label that says more on hover or focus: the detail
// a one-line hint leaves out (M8.3).
export function Tip({ id, side = 'bottom', children }) {
    return (
        <span class={`tooltip ${SIDES[side]} align-middle`}>
            <span id={id} role="tooltip" class="tooltip-content max-w-72 rounded-lg px-3 py-2 text-left text-sm font-normal">
                {children}
            </span>
            <button type="button" class="inline-flex cursor-help p-0.5 text-base-content/60 hover:text-base-content" aria-label="More about this"
                aria-describedby={id}>
                <InfoIcon />
            </button>
        </span>
    );
}

// The public guides, which explain what few apps ask people to understand.
export const DOCS = 'https://dens.chat/docs/';

// DocsTip is a Tip whose i opens the guide to its subject, in a new tab:
// the gist on hover, the whole story a click away. It stands where those
// things come up (v1's onboarding), and opens where a Tip would.
export function DocsTip({ id, page, side = 'bottom', children }) {
    return (
        <span class={`tooltip ${SIDES[side]} align-middle font-normal`}>
            <span id={id} role="tooltip" class="tooltip-content max-w-72 rounded-lg px-3 py-2 text-left text-sm font-normal">
                {children}
                <span class="mt-1 block text-xs opacity-70">Opens the guide on dens.chat.</span>
            </span>
            <a href={DOCS + page} target="_blank" rel="noopener noreferrer" class="inline-flex p-0.5 text-base-content/60 hover:text-base-content"
                aria-label="Open the guide" aria-describedby={id}>
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

export function CopyButton({ text, label = 'Copy' }) {
    const [copied, setCopied] = useState(false);
    async function copy() {
        try {
            await navigator.clipboard.writeText(text);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        } catch {
            // Clipboard access can be refused; the text stays selectable.
        }
    }
    return (
        <button type="button" class="btn btn-sm" onClick={copy}>
            {copied ? 'Copied' : label}
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
