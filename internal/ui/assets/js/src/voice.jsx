// A call's controls: the bar at the foot of the channel list, which follows
// the call from den to den, and who is in each voice channel's call.

import { useEffect, useState } from 'preact/hooks';
import { Avatar } from './avatar.jsx';
import { chooseMic, chooseSpeaker, clearNotice, devices, leaveCall, micProblem, onCall, setMuted } from './voice.js';

// useCall is the page's call, and why the last one ended.
export function useCall() {
    const [state, setState] = useState({ call: null, notice: null });
    useEffect(() => onCall(setState), []);
    return state;
}

const STATUS = {
    joining: 'Joining…',
    connecting: 'Connecting…',
    connected: 'In the call',
    reconnecting: 'Reconnecting…',
};

// CallBar shows the page's call, in this den or another, or why it ended:
// at the foot of a den's channel list, or boxed on the other pages.
export function CallBar({ denID, boxed }) {
    const { call, notice } = useCall();
    const [settings, setSettings] = useState(false);
    const frame = boxed ? 'rounded-box bg-base-200 p-2' : 'border-t border-base-300 p-2';
    if (!call) {
        if (!notice) return null;
        return (
            <div role="status" class={`flex items-start gap-2 text-sm ${frame}`}>
                <span class="min-w-0 flex-1 text-warning">{notice.text}</span>
                <button type="button" class="btn btn-ghost btn-xs" aria-label="Dismiss" onClick={clearNotice}>✕</button>
            </div>
        );
    }
    const where = call.den === denID ? call.label.channel : `${call.label.channel} in ${call.label.den}`;
    return (
        <div class={frame}>
            <div class="flex items-center gap-1">
                <span class={`mx-1 h-2 w-2 shrink-0 rounded-full ${call.status === 'connected' ? 'bg-success' : 'bg-warning'}`} aria-hidden="true"></span>
                <div class="min-w-0 flex-1 leading-tight">
                    <div class="text-xs font-semibold" role="status">{STATUS[call.status]}</div>
                    <div class="truncate text-xs text-base-content/60">{where}</div>
                </div>
                <span class="tooltip tooltip-top before:max-w-48" data-tip="Calls aren't end-to-end encrypted: the den can hear them.">
                    <span class="cursor-default select-none px-1 text-xs text-base-content/50" aria-label="Calls aren't end-to-end encrypted: the den can hear them.">ⓘ</span>
                </span>
                <button type="button" class={`btn btn-ghost btn-xs ${call.muted ? 'text-error' : ''}`} aria-pressed={call.muted}
                    aria-label={call.muted ? 'Unmute' : 'Mute'} title={call.muted ? 'Unmute' : 'Mute'} onClick={() => setMuted(!call.muted)}>
                    {call.muted ? '🔇' : '🎙'}
                </button>
                <button type="button" class={`btn btn-ghost btn-xs ${settings ? 'btn-active' : ''}`} aria-expanded={settings}
                    aria-label="Microphone and speaker" title="Microphone and speaker" onClick={() => setSettings(!settings)}>⚙</button>
                <button type="button" class="btn btn-ghost btn-xs text-error" onClick={leaveCall}>Leave</button>
            </div>
            {settings && <DevicePicker />}
        </div>
    );
}

// DevicePicker chooses the microphone and, where the browser can, the
// speaker.
function DevicePicker() {
    const [list, setList] = useState(null);
    const [error, setError] = useState('');
    useEffect(() => {
        devices().then(setList, () => setError("This browser didn't list its devices."));
    }, []);
    if (!list) return error ? <p class="mt-2 text-xs text-error">{error}</p> : null;
    function pickMic(id) {
        setList({ ...list, mic: id });
        setError('');
        chooseMic(id).catch((e) => setError(micProblem(e)));
    }
    function pickSpeaker(id) {
        setList({ ...list, speaker: id });
        chooseSpeaker(id);
    }
    return (
        <div class="mt-2 flex flex-col gap-2 text-xs">
            <label class="flex flex-col gap-1">
                <span>Microphone</span>
                <select class="select select-xs w-full" value={list.mic} onChange={(e) => pickMic(e.currentTarget.value)}>
                    <option value="">Default</option>
                    {list.inputs.map((d) => <option key={d.id} value={d.id}>{d.label}</option>)}
                </select>
            </label>
            {list.outputs && (
                <label class="flex flex-col gap-1">
                    <span>Speaker</span>
                    <select class="select select-xs w-full" value={list.speaker} onChange={(e) => pickSpeaker(e.currentTarget.value)}>
                        <option value="">Default</option>
                        {list.outputs.map((d) => <option key={d.id} value={d.id}>{d.label}</option>)}
                    </select>
                </label>
            )}
            {error && <p class="text-error">{error}</p>}
        </div>
    );
}

// CallMembers lists who is in a voice channel's call, under the channel.
export function CallMembers({ call, members }) {
    if (!call?.members?.length) return null;
    return (
        <ul class="mb-1 ml-8 flex flex-col gap-0.5" aria-label="In the call">
            {call.members.map((m) => {
                const member = members.get(m.id);
                return (
                    <li key={m.id} class="flex min-w-0 items-center gap-1.5 text-sm text-base-content/80">
                        <Avatar member={member || { id: m.id }} size="sm" />
                        <span class="truncate">{member?.display_name || 'Unknown member'}</span>
                        {m.muted && <span class="text-xs" title="Muted" aria-label="muted">🔇</span>}
                    </li>
                );
            })}
        </ul>
    );
}
