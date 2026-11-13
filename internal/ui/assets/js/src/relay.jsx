// Relay's bar (M20.2), where the call's sits: in the block at the foot of
// a den's channel list, boxed on the other pages, and across the top of
// the channel's pane on the phone layout. It shows on both ends of a
// computer's sound: on the page that listens to it, how its listening
// stands, and on the computer whose sound it is, the device it goes to,
// each with Stop; and why the page's last listening ended.

import { useEffect, useState } from 'preact/hooks';
import { SpeakerIcon } from './icons.jsx';
import { clearNotice, onRelay, stopListening, stopSharing } from './relay.js';

// LISTENING says how the page's listening stands, by the device's name.
const LISTENING = {
    connecting: (name) => `Connecting to ${name}…`,
    playing: (name) => `Listening to ${name}`,
    waiting: (name) => `Waiting for ${name}…`,
};

/** useRelay is Relay's sound in the page, kept current: with sharing,
    this computer's too, as the service says */
export function useRelay(sharing = false) {
    const [state, setState] = useState(null);
    useEffect(() => onRelay(setState, sharing), []);
    return state;
}

// LOOKS are the bar's, where it sits: in the list's block, boxed, or
// across the channel's pane.
const LOOKS = {
    block: 'flex flex-col gap-1 border-t border-base-300 bg-bar px-2 py-1.5',
    box: 'flex flex-col gap-1 rounded-box bg-base-200 p-2',
    bar: 'flex flex-col border-b border-base-300 bg-bar px-2',
};

export function RelayBar({ look = 'block' }) {
    const s = useRelay(true);
    if (!s || (!s.listening && !s.sharing && !s.notice)) return null;
    const row = look === 'bar' ? 'min-h-10' : 'min-h-8';
    return (
        <div class={LOOKS[look]} data-relay-bar>
            {s.listening && (
                <Row row={row} status={s.listening.status} label={LISTENING[s.listening.status](s.listening.name)} onStop={stopListening}
                    stop="Stop listening" />
            )}
            {s.sharing && (
                <Row row={row} status="sharing" label={`Sharing this computer's sound with ${s.sharing.name}`} problem={s.problem}
                    onStop={stopSharing} stop="Stop sharing" />
            )}
            {s.notice && (
                <div role="status" class="flex items-start gap-2 px-1 text-xs" data-relay-notice>
                    <span class="min-w-0 flex-1 text-warning">{s.notice}</span>
                    <button type="button" class="btn btn-ghost btn-xs" aria-label="Dismiss" onClick={clearNotice}>✕</button>
                </div>
            )}
        </div>
    );
}

function Row({ row, status, label, problem, onStop, stop }) {
    const color = status === 'playing' || status === 'sharing' ? 'text-success' : 'text-warning';
    return (
        <div class={`flex items-center gap-2 ${row}`} data-relay={status}>
            <span class={`shrink-0 ${color}`}><SpeakerIcon size="h-4 w-4" /></span>
            <span class="min-w-0 flex-1">
                <span role="status" class="block truncate text-sm">{label}</span>
                {problem && <span class="block text-xs text-warning">{problem[0].toUpperCase() + problem.slice(1)}</span>}
            </span>
            <button type="button" class="btn btn-ghost btn-xs shrink-0" aria-label={stop} onClick={onStop}>Stop</button>
        </div>
    );
}
