// Small shared pieces. Everything shown here goes through Preact's text
// rendering; nothing sets HTML from a string.

import { useEffect, useState } from 'preact/hooks';

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
