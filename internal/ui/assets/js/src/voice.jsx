// A call's controls, which follow the call from den to den: in a block at
// the foot of a den's channel list, and boxed on the other pages; and who
// is in each voice channel's call, with who is speaking, how loud each
// plays here, and staff's controls (M3).

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { Avatar } from './avatar.jsx';
import { rank } from './people.jsx';
import { openSettings } from './settings.jsx';
import { byName, clearNotice, leaveCall, onCall, setDeafened, setMuted, setVolume, voiceSettings, volumeIn } from './voice.js';

// useCall is the page's call, why the last one ended, who is speaking in
// it, and how loud each member plays here.
export function useCall() {
    const [state, setState] = useState({ call: null, notice: null, speaking: new Set(), volumes: {} });
    useEffect(() => onCall(setState), []);
    return state;
}

// STATUS says how the call stands, and where.
const STATUS = {
    joining: (where) => `Joining ${where}…`,
    connecting: (where) => `Connecting to ${where}…`,
    connected: (where) => `Connected to ${where}`,
    reconnecting: (where) => `Reconnecting to ${where}…`,
};

// notices are what the member should know of their call: why the last one
// ended, that it waits for the microphone, that staff muted them in this
// den's, or that push to talk has no key yet.
function notices(call, notice, denID, staffMuted) {
    const ptt = voiceSettings();
    const lines = [];
    if (!call && notice) {
        lines.push(
            <div key="notice" role="status" class="flex items-start gap-2">
                <span class="min-w-0 flex-1 text-warning">{notice.text}</span>
                <button type="button" class="btn btn-ghost btn-xs" aria-label="Dismiss" onClick={clearNotice}>✕</button>
            </div>,
        );
    }
    if (call?.waitingForMic) lines.push(<p key="mic">Waiting for the microphone. If your browser asks for it, allow it.</p>);
    if (call && staffMuted && call.den === denID) lines.push(<p key="staff" class="text-warning">Muted by staff: nobody hears you</p>);
    if (call && ptt.mode === 'ptt' && !ptt.keys.ptt) {
        lines.push(
            <button key="ptt" type="button" class="link text-left text-warning" onClick={() => openSettings('voice')}>
                Push to talk has no key: set one
            </button>,
        );
    }
    return lines;
}

// CallBlock holds the page's call at the foot of a den's channel list,
// above the member's panel and in the message bar's shade: its notices,
// then its controls in a row. Screen sharing's controls will be a row of
// their own, above the call's.
export function CallBlock({ denID, staffMuted }) {
    const { call, notice } = useCall();
    const lines = notices(call, notice, denID, staffMuted);
    if (!call && !lines.length) return null;
    return (
        <div class="flex flex-col gap-1 border-t border-base-300 bg-bar px-2 py-1.5">
            {lines.length > 0 && <div class="flex flex-col gap-1 px-1 py-0.5 text-xs">{lines}</div>}
            {call && <CallControls denID={denID} call={call} />}
        </div>
    );
}

// CallBar is the call, and its notices, boxed on the pages other than a
// den's, whose channel lists hold them at their foot.
export function CallBar() {
    const { call, notice } = useCall();
    const lines = notices(call, notice);
    if (!call && !lines.length) return null;
    return (
        <div class="flex flex-col gap-2 rounded-box bg-base-200 p-2">
            {lines.length > 0 && <div class="flex flex-col gap-1 text-xs">{lines}</div>}
            {call && <CallControls call={call} labeled />}
        </div>
    );
}

// CallControls are the call, in this den or another, in a row: a speaker
// for whether it's connected, which says where on hover, and the buttons to
// mute, deafen, open the voice settings and leave, spread evenly. labeled
// spells out where beside the speaker, for pages with room for it.
function CallControls({ denID, call, labeled }) {
    const where = call.den === denID ? call.label.channel : `${call.label.channel} in ${call.label.den}`;
    const said = STATUS[call.status](where);
    const color = call.status === 'connected' ? 'text-success' : 'text-warning';
    const buttons = (
        <>
            <IconButton label={call.muted ? 'Unmute' : 'Mute'} pressed={call.muted} danger={call.muted} onClick={() => setMuted(!call.muted)}>
                <MicIcon off={call.muted} size="h-[18px] w-[18px]" />
            </IconButton>
            <IconButton label={call.deafened ? 'Undeafen' : 'Deafen'} pressed={call.deafened} danger={call.deafened}
                onClick={() => setDeafened(!call.deafened)}>
                <HeadphonesIcon off={call.deafened} size="h-[18px] w-[18px]" />
            </IconButton>
            <IconButton label="Voice settings" onClick={() => openSettings('voice')}>
                <CogIcon />
            </IconButton>
            <IconButton label="Leave the call" danger onClick={leaveCall}>
                <LeaveIcon />
            </IconButton>
        </>
    );
    if (labeled) {
        return (
            <div class="flex items-center justify-between gap-1">
                <span class={`flex min-w-0 items-center gap-2 px-1.5 ${color}`}>
                    <SpeakerIcon />
                    <span role="status" class="truncate text-xs text-base-content/80">{said}</span>
                </span>
                <span class="flex shrink-0 items-center gap-1">{buttons}</span>
            </div>
        );
    }
    return (
        <div class="flex items-center justify-between">
            <span class={`group relative flex h-8 w-8 shrink-0 items-center justify-center ${color}`}>
                <SpeakerIcon />
                <span role="status" class="sr-only">{said}</span>
                <span aria-hidden="true" class="pointer-events-none absolute bottom-full left-0 z-20 mb-2 hidden w-max max-w-52 rounded-lg bg-neutral px-2 py-1 text-xs text-neutral-content shadow group-hover:block">
                    {said}
                </span>
            </span>
            {buttons}
        </div>
    );
}

function IconButton({ label, pressed, danger, onClick, children }) {
    return (
        <button type="button" class={`btn btn-ghost btn-sm btn-square ${danger ? 'text-error' : ''}`} aria-label={label} title={label}
            aria-pressed={pressed} onClick={onClick}>
            {children}
        </button>
    );
}

const ICON = { viewBox: '0 0 16 16', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.5', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true' };

export function MicIcon({ off, size = 'h-4 w-4' }) {
    return (
        <svg class={size} {...ICON}>
            <rect x="5.75" y="1.5" width="4.5" height="8" rx="2.25" />
            <path d="M3.5 7.5a4.5 4.5 0 0 0 9 0M8 12v2.5" />
            {off && <path d="M2 2l12 12" />}
        </svg>
    );
}

export function HeadphonesIcon({ off, size = 'h-4 w-4' }) {
    return (
        <svg class={size} {...ICON}>
            <path d="M2.5 11.5V8a5.5 5.5 0 0 1 11 0v3.5" />
            <rect x="2" y="9.5" width="3" height="5" rx="1" />
            <rect x="11" y="9.5" width="3" height="5" rx="1" />
            {off && <path d="M2 2l12 12" />}
        </svg>
    );
}

// CogIcon is an eight-toothed cog, for settings.
export function CogIcon({ size = 'h-[18px] w-[18px]' }) {
    return (
        <svg class={size} {...ICON} stroke-width="1.3">
            <path d="M13.06 6.43 L14.90 6.82 L14.90 9.18 L13.06 9.57 L12.69 10.47 L13.72 12.04 L12.04 13.72 L10.47 12.69 L9.57 13.06 L9.18 14.90 L6.82 14.90 L6.43 13.06 L5.53 12.69 L3.96 13.72 L2.28 12.04 L3.31 10.47 L2.94 9.57 L1.10 9.18 L1.10 6.82 L2.94 6.43 L3.31 5.53 L2.28 3.96 L3.96 2.28 L5.53 3.31 L6.43 2.94 L6.82 1.10 L9.18 1.10 L9.57 2.94 L10.47 3.31 L12.04 2.28 L13.72 3.96 L12.69 5.53Z" />
            <circle cx="8" cy="8" r="2.2" />
        </svg>
    );
}

function SpeakerIcon() {
    return (
        <svg class="h-[18px] w-[18px] shrink-0" {...ICON}>
            <path d="M2.5 6v4h2.5l3.5 3V3L5 6z" />
            <path d="M11 5.75a3.25 3.25 0 0 1 0 4.5M12.75 3.75a6 6 0 0 1 0 8.5" />
        </svg>
    );
}

function LeaveIcon() {
    return (
        <svg class="h-[18px] w-[18px]" viewBox="0 0 16 16" aria-hidden="true">
            <path fill="currentColor" d="M1.5 9.4c3.6-3.4 9.4-3.4 13 0l-1.3 1.9-2.6-.8V8.9a8.4 8.4 0 0 0-5.2 0v1.6l-2.6.8z" />
        </svg>
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
            {/* Rings stand 3px out from each avatar, so rows keep 8px apart. */}
            <ul class="mb-2 ml-8 mt-1 flex flex-col gap-2" aria-label="In the call">
                {byName(call.members, members).map((m) => {
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
                            {m.deafened ? (
                                <span class="text-base-content/60" title="Deafened: hears nothing of the call" aria-label="deafened"><HeadphonesIcon off size="h-3.5 w-3.5" /></span>
                            ) : m.muted && (
                                <span class="text-base-content/60" title="Muted" aria-label="muted"><MicIcon off size="h-3.5 w-3.5" /></span>
                            )}
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
