// A call's controls: the bar at the foot of the channel list, which follows
// the call from den to den, and who is in each voice channel's call, with
// who is speaking, how loud each plays here, and staff's controls (M3).

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { Avatar } from './avatar.jsx';
import { rank } from './people.jsx';
import { chooseMic, chooseSpeaker, clearNotice, devices, leaveCall, micProblem, noiseSuppression, onCall, setMuted, setRNNoise, setVolume, volumeIn } from './voice.js';

// useCall is the page's call, why the last one ended, who is speaking in
// it, and how loud each member plays here.
export function useCall() {
    const [state, setState] = useState({ call: null, notice: null, speaking: new Set(), volumes: {} });
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
// staffMuted says staff muted the member in this den's call.
export function CallBar({ denID, boxed, staffMuted }) {
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
                    {staffMuted && call.den === denID && <div class="text-xs text-warning">Muted by staff: nobody hears you</div>}
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
// speaker, and the noise suppression.
function DevicePicker() {
    const [list, setList] = useState(null);
    const [error, setError] = useState('');
    const [suppression, setSuppression] = useState(noiseSuppression);
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
    function pickRNNoise(on) {
        setSuppression({ rnnoise: on, failed: false });
        setError('');
        setRNNoise(on).then(() => setSuppression(noiseSuppression()), (e) => setError(micProblem(e)));
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
            <label class="flex items-start gap-2">
                <input type="checkbox" class="toggle toggle-xs mt-0.5" checked={suppression.rnnoise} onChange={(e) => pickRNNoise(e.currentTarget.checked)} />
                <span>
                    Stronger noise suppression
                    <span class="block text-base-content/60">
                        RNNoise takes out keyboards and other noise the browser leaves in. It adds 30 ms of delay and uses a little more of this
                        computer's processor.
                    </span>
                </span>
            </label>
            {suppression.failed && <p class="text-warning">RNNoise couldn't start in this browser, so the browser's own suppression is on.</p>}
            {error && <p class="text-error">{error}</p>}
        </div>
    );
}

// CallMembers lists who is in a voice channel's call, under the channel.
// In the page's own call, a ring marks whoever is speaking: speaking holds
// member IDs, and "self" for this page's member, me. A member's name opens
// their profile through onProfile. Everyone else's row has a menu that sets
// how loud they play here, from volumes, and gives staff a way to mute or
// disconnect those they rank above.
export function CallMembers({ denID, call, members, me, speaking, volumes, onProfile }) {
    const [menu, setMenu] = useState('');
    const [error, setError] = useState('');
    // An open menu closes on a click anywhere else, or on Escape.
    useEffect(() => {
        if (!menu) return undefined;
        const away = (e) => {
            if (!e.target.closest?.('[data-call-menu]')) setMenu('');
        };
        const escape = (e) => {
            if (e.key === 'Escape') setMenu('');
        };
        document.addEventListener('pointerdown', away);
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('pointerdown', away);
            document.removeEventListener('keydown', escape);
        };
    }, [menu]);
    if (!call?.members?.length) return null;
    async function act(id, what, body) {
        setError('');
        setMenu('');
        try {
            await api.post(`/api/dens/${denID}/members/${id}/${what}`, body);
        } catch (e) {
            setError(e.message);
        }
    }
    return (
        <>
            <ul class="mb-1 ml-8 flex flex-col gap-0.5" aria-label="In the call">
                {call.members.map((m) => {
                    const member = members.get(m.id);
                    const name = member?.display_name || 'Unknown member';
                    const heard = !m.muted && !m.staff_muted;
                    const talking = heard && speaking && speaking.has(m.id === me?.id ? 'self' : m.id);
                    // Everyone else's row has a menu, for their volume here,
                    // and for staff's actions when the member here outranks them.
                    const other = me && m.id !== me.id;
                    const moderate = me && member && rank(me.role) > rank(member.role);
                    const pct = Math.round(volumeIn(volumes || {}, denID, m.id) * 100);
                    return (
                        <li key={m.id} class="group relative flex min-w-0 items-center gap-1.5 text-sm text-base-content/80">
                            <button type="button" class="flex min-w-0 items-center gap-1.5 rounded pr-1 text-left hover:text-base-content disabled:cursor-default"
                                disabled={!member} onClick={() => onProfile?.(member)}>
                                {/* A block of the avatar's own size, so the ring is round. */}
                                <span class={`inline-flex shrink-0 rounded-full ring-2 ring-offset-1 ring-offset-base-200 ${talking ? 'ring-success' : 'ring-transparent'}`}>
                                    <Avatar member={member || { id: m.id }} size="sm" />
                                </span>
                                <span class="truncate">{name}</span>
                            </button>
                            {talking && <span class="sr-only">speaking</span>}
                            {m.muted && <span class="text-xs" title="Muted" aria-label="muted">🔇</span>}
                            {m.staff_muted && (
                                <span class="text-xs text-warning" title="Muted by staff: the den forwards nothing from them" aria-label="muted by staff">
                                    🔇 staff
                                </span>
                            )}
                            {other && pct < 100 && (
                                <span class="text-xs text-base-content/50" title={pct === 0 ? 'Muted for you' : `Plays at ${pct}% for you`}>{pct}%</span>
                            )}
                            {other && (
                                <button type="button" class="btn btn-ghost btn-xs ml-auto opacity-0 focus:opacity-100 group-hover:opacity-100" data-call-menu
                                    aria-label={`Options for ${name} in the call`} aria-expanded={menu === m.id}
                                    onClick={() => setMenu(menu === m.id ? '' : m.id)}>⋯</button>
                            )}
                            {other && menu === m.id && (
                                <div class="absolute right-0 top-full z-10 flex w-52 flex-col gap-1 rounded-box bg-base-100 p-2 shadow" data-call-menu>
                                    <label class="flex items-center gap-2 text-xs" title="How loud they play for you">
                                        <span>Volume</span>
                                        <input type="range" class="range range-xs min-w-0 flex-1" min="0" max="100" step="5" value={pct}
                                            aria-label={`Volume for ${name}`} aria-valuetext={`${pct}%`}
                                            onInput={(e) => setVolume(denID, m.id, Number(e.currentTarget.value) / 100)} />
                                        <span class="w-9 text-right tabular-nums">{pct}%</span>
                                    </label>
                                    {moderate && (
                                        <>
                                            <button type="button" class="btn btn-ghost btn-xs justify-start"
                                                onClick={() => act(m.id, 'voice-mute', { muted: !m.staff_muted })}>
                                                {m.staff_muted ? 'Unmute for everyone' : 'Mute for everyone'}
                                            </button>
                                            <button type="button" class="btn btn-ghost btn-xs justify-start text-error" onClick={() => act(m.id, 'disconnect')}>
                                                Disconnect from the call
                                            </button>
                                        </>
                                    )}
                                </div>
                            )}
                        </li>
                    );
                })}
            </ul>
            {error && <p class="ml-8 text-xs text-error">{error}</p>}
        </>
    );
}
