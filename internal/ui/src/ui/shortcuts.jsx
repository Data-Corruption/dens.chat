// Keyboard shortcuts: the page's keys by where they work, and the voice
// keys as the member bound them, opened by Ctrl+/ and from Formatting
// help. A page key a voice key took says so, since the voice key takes
// the press. A touch screen has no keys, so nothing offers it there.

import { useEffect, useState } from 'preact/hooks';
import { Dialog } from '../manage.jsx';
import { PAGE_KEYS, takenBy } from '../core/keys.js';
import { KEY_NAMES, onKeys, voiceSettings } from '../voice/voice.js';
import { Button } from './components.jsx';
import { openSettings } from './dialogs.js';

// Keys draws a press, each key in its box: an array of keys held together,
// or text, as a completion's.
export function Keys({ keys }) {
    if (typeof keys === 'string') return keys;
    return keys.map((k, i) => <span key={k}>{i > 0 && '+'}<kbd class="kbd kbd-xs">{k}</kbd></span>);
}

// COMPOSER_KEYS are the message box's keys.
export const COMPOSER_KEYS = [
    [['Enter'], 'Send'],
    [['Shift', 'Enter'], 'Start a new line'],
    [['Ctrl', 'Enter'], 'Send from inside a code block'],
    [['↑'], 'Edit your last message, in an empty box'],
    [['Esc'], 'Drop the reply you started'],
    [':th', 'Emoji that match, @ members to mention, and # channels to link'],
    [['Tab'], 'Put in the emoji, member or channel marked'],
];

const EDIT_KEYS = [
    [['Enter'], 'Save'],
    [['Shift', 'Enter'], 'Start a new line'],
    [['Esc'], 'Cancel'],
];

const VIEWER_KEYS = [
    [['←'], 'The file before'],
    [['→'], 'The file after'],
    [['Esc'], 'Close'],
];

// KeyTable is a section's keys, each with what it does.
export function KeyTable({ label, rows }) {
    return (
        <table class="table table-sm" aria-label={label}>
            <tbody>
                {rows.map(([keys, what]) => (
                    <tr key={what}>
                        <td class="w-36 whitespace-nowrap"><Keys keys={keys} /></td>
                        <td>{what}</td>
                    </tr>
                ))}
            </tbody>
        </table>
    );
}

function Section({ title, children }) {
    return (
        <section class="flex flex-col gap-1">
            <h3 class="font-semibold">{title}</h3>
            {children}
        </section>
    );
}

export function ShortcutsDialog({ onClose }) {
    const [keys, setKeys] = useState(() => voiceSettings().keys);
    useEffect(() => onKeys(() => setKeys(voiceSettings().keys)), []);
    useEffect(() => {
        const escape = (e) => e.key === 'Escape' && onClose();
        document.addEventListener('keydown', escape);
        return () => document.removeEventListener('keydown', escape);
    }, [onClose]);
    const around = PAGE_KEYS.map((k) => {
        const by = takenBy(k, keys);
        return [k.keys, by ? `${k.what}. Your ${KEY_NAMES[by]} key takes it.` : k.what];
    });
    const held = (b) => [b.ctrl && 'Ctrl', b.alt && 'Alt', b.shift && 'Shift', b.meta && 'Meta', b.label].filter(Boolean);
    const bound = Object.keys(KEY_NAMES).filter((a) => keys[a]).map((a) => [held(keys[a]), KEY_NAMES[a]]);
    return (
        <Dialog title="Keyboard shortcuts" onClose={onClose}>
            <div class="flex flex-col gap-4 text-sm" data-shortcuts>
                <Section title="Getting around"><KeyTable label="Getting around" rows={around} /></Section>
                <Section title="The message box"><KeyTable label="The message box" rows={COMPOSER_KEYS} /></Section>
                <Section title="Editing a message"><KeyTable label="Editing a message" rows={EDIT_KEYS} /></Section>
                <Section title="Photos and videos"><KeyTable label="Photos and videos" rows={VIEWER_KEYS} /></Section>
                <Section title="Voice">
                    {bound.length > 0 ? <KeyTable label="Voice" rows={bound} /> : <p class="text-base-content/70">None set.</p>}
                    <div><Button kind="secondary" onClick={() => {
                        onClose();
                        openSettings('voice');
                    }}>Voice settings</Button></div>
                </Section>
            </div>
        </Dialog>
    );
}
