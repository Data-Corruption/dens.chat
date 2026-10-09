// A call's controls, which follow the call from den to den: in a block at
// the foot of a den's channel list, and boxed on the other pages; and who
// is in each voice channel's call, with who is speaking, how loud each
// plays here, and staff's controls (M3). From M4.2 the call's block has a
// row for sharing the member's screen, and the list marks who shares,
// which a click watches.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { Avatar } from './avatar.jsx';
import { rank } from './people.jsx';
import { openSettings } from './settings.jsx';
import { shareBlocked, watchersOf } from './share.js';
import {
    byName, clearNotice, clearShareNotice, joinAndWatch, leaveCall, onCall, setDeafened, setMuted, setPreview, setVolume, startShare, stopShare,
    unwatchShare, voiceSettings, volumeIn, watchShare,
} from './voice.js';

// useCall is the page's call, why the last one ended, who is speaking in
// it, how loud each member plays here, and from M4.2 its share, the shares
// it watches, and what it knows of the call's den.
export function useCall() {
    const [state, setState] = useState({ call: null, notice: null, speaking: new Set(), volumes: {}, share: null, shareNotice: null, watched: [], den: null });
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
function notices(call, notice, denID, staffMuted, shareNotice) {
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
    if (call && shareNotice) {
        lines.push(
            <div key="share" role="status" class="flex items-start gap-2">
                <span class="min-w-0 flex-1 text-warning">{shareNotice.text}</span>
                <button type="button" class="btn btn-ghost btn-xs" aria-label="Dismiss" onClick={clearShareNotice}>✕</button>
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
// then, while the member shares their screen, the share's row (M4.2), then
// its controls in a row.
export function CallBlock({ denID, staffMuted }) {
    const { call, notice, share, shareNotice, den } = useCall();
    const lines = notices(call, notice, denID, staffMuted, shareNotice);
    if (!call && !lines.length) return null;
    return (
        <div class="flex flex-col gap-1 border-t border-base-300 bg-bar px-2 py-1.5">
            {lines.length > 0 && <div class="flex flex-col gap-1 px-1 py-0.5 text-xs">{lines}</div>}
            {call && share && <ShareRow call={call} share={share} den={den} />}
            {call && <CallControls denID={denID} call={call} share={share} den={den} />}
        </div>
    );
}

// CallBar is the call, and its notices, boxed on the pages other than a
// den's, whose channel lists hold them at their foot.
export function CallBar() {
    const { call, notice, share, shareNotice, den } = useCall();
    const lines = notices(call, notice, undefined, false, shareNotice);
    if (!call && !lines.length) return null;
    return (
        <div class="flex flex-col gap-2 rounded-box bg-base-200 p-2">
            {lines.length > 0 && <div class="flex flex-col gap-1 text-xs">{lines}</div>}
            {call && share && <ShareRow call={call} share={share} den={den} />}
            {call && <CallControls call={call} share={share} den={den} labeled />}
        </div>
    );
}

// ShareRow is the member's share's row, while they share (M4.2): while it
// starts, what it waits for; and while it's live, who watches, whether it
// has sound, a button that shows their own screen in the player or hides
// it, and a button to stop.
function ShareRow({ call, share, den }) {
    if (share.status !== 'live') {
        return (
            <div class="flex h-8 items-center gap-2 px-3 text-sm text-base-content/70" role="status">
                <span class="loading loading-spinner loading-xs"></span>
                {share.status === 'picking' ? 'Choosing what to share' : 'Starting your share'}
            </div>
        );
    }
    const mine = den?.calls.find((c) => c.channel_id === call.channel);
    const viewers = watchersOf(mine, den?.me).map((id) => den.names.get(id) || 'Someone');
    const soundless = share.wanted
        ? "This share has no sound: this browser didn't share any. Chromium-based browsers share a tab's sound, and on Windows the whole system's."
        : 'This share has no sound: sharing sound is off in the Voice settings.';
    // The sidebar is narrow, so who watches is a count, with their names on
    // hover.
    const watching = viewers.length ? `Watching: ${viewers.join(', ')}` : 'Nobody is watching yet';
    return (
        <div class="flex h-8 items-center gap-2 pl-2 pr-1 text-sm">
            <span class="badge badge-error badge-sm shrink-0 font-semibold">LIVE</span>
            <span class="flex items-center gap-1 text-base-content/70" title={watching} role="status" aria-label={watching}>
                <EyeIcon />
                <span class="tabular-nums">{viewers.length}</span>
            </span>
            <span class="flex-1"></span>
            {!share.sound && (
                <span class="shrink-0 text-base-content/60" title={soundless} aria-label="no sound"><SoundOffIcon /></span>
            )}
            <button type="button" class={`btn btn-ghost btn-xs btn-square shrink-0 ${share.preview ? 'text-success' : ''}`} aria-pressed={share.preview}
                title={share.preview ? 'Hide your screen from the player' : 'Show your screen in the player'}
                aria-label={share.preview ? 'Hide your screen from the player' : 'Show your screen in the player'}
                onClick={() => setPreview(!share.preview)}>
                <PreviewIcon />
            </button>
            <button type="button" class="btn btn-error btn-xs shrink-0" onClick={stopShare}>Stop</button>
        </div>
    );
}

// shareWhy says why the member can't start a share now, or '' when they
// can: the den allows none, as many share as it allows, or staff muted
// them. The den checks again all the same.
function shareWhy(call, den) {
    if (!den) return 'Waiting for the den';
    const mine = den.calls.find((c) => c.channel_id === call.channel);
    if (mine?.members.some((m) => m.id === den.me && m.staff_muted)) return "Staff muted you, so you can't share your screen.";
    return shareBlocked(den.limits, den.calls);
}

// ShareButton starts a share, between the call's speaker and its mute
// button, or stops the one going on, when it shows pressed.
function ShareButton({ call, share, den }) {
    if (share) {
        return (
            <IconButton label="Stop sharing your screen" pressed onClick={stopShare}>
                <ScreenIcon on />
            </IconButton>
        );
    }
    const why = call.status === 'joining' ? 'Joining the call' : shareWhy(call, den);
    return (
        <IconButton label="Share your screen" disabled={!!why}
            title={why || "Share your screen, a window or a tab with the call. Shares aren't end-to-end encrypted: the den can see them."}
            onClick={startShare}>
            <ScreenIcon />
        </IconButton>
    );
}

// CallControls are the call, in this den or another, in a row: a speaker
// for whether it's connected, which says where on hover, and the buttons to
// mute, deafen, open the voice settings and leave, spread evenly. labeled
// spells out where beside the speaker, for pages with room for it.
function CallControls({ denID, call, share, den, labeled }) {
    const where = call.den === denID ? call.label.channel : `${call.label.channel} in ${call.label.den}`;
    const said = STATUS[call.status](where);
    const color = call.status === 'connected' ? 'text-success' : 'text-warning';
    const buttons = (
        <>
            {labeled && <ShareButton call={call} share={share} den={den} />}
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
            <ShareButton call={call} share={share} den={den} />
            {buttons}
        </div>
    );
}

// LiveMark marks a member who shares their screen, with who watches it on
// hover. In the page's own call it watches the share, or stops watching it;
// elsewhere it joins the call and watches. The member's own mark shows
// their screen in the player, or hides it, while this page shares it;
// previewing says whether it shows.
function LiveMark({ denID, call, member, name, mine, inCall, label, watching, members, previewing }) {
    const viewers = watchersOf(call, member.id).map((id) => members.get(id)?.display_name || 'Someone');
    const seen = viewers.length ? ` Watching: ${viewers.join(', ')}.` : '';
    const style = `badge badge-xs shrink-0 font-semibold ${watching || (mine && previewing) ? 'badge-error' : 'badge-error badge-outline'}`;
    if (mine && !inCall) return <span class={style} title={`You're sharing your screen.${seen}`}>LIVE</span>;
    if (mine) {
        const what = previewing ? 'Hide your screen from the player' : 'Show your screen in the player';
        return (
            <button type="button" class={`${style} cursor-pointer`} title={`You're sharing your screen. ${what}.${seen}`} aria-label={what}
                aria-pressed={previewing} onClick={() => setPreview(!previewing)}>
                LIVE
            </button>
        );
    }
    const what = !inCall ? `Join the call and watch ${name}'s screen` : watching ? `Stop watching ${name}'s screen` : `Watch ${name}'s screen`;
    function act() {
        if (!inCall) joinAndWatch(denID, call.channel_id, label, member.id, name);
        else if (watching) unwatchShare(member.id);
        else watchShare(member.id, name);
    }
    return (
        <button type="button" class={`${style} cursor-pointer`} title={`${what}.${seen}`} aria-label={what} aria-pressed={inCall ? watching : undefined}
            onClick={act}>
            LIVE
        </button>
    );
}

// IconButton is one of the call's buttons. A disabled one says why on
// hover, through title.
function IconButton({ label, pressed, danger, disabled, title, onClick, children }) {
    return (
        <button type="button" class={`btn btn-ghost btn-sm btn-square ${danger ? 'text-error' : ''} ${pressed && !danger ? 'text-success' : ''}`}
            aria-label={label} title={title || label} aria-pressed={pressed} disabled={disabled} onClick={onClick}>
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

// ScreenIcon is a monitor, with an arrow going out of it while on: the
// member is sharing.
function ScreenIcon({ on }) {
    return (
        <svg class="h-[18px] w-[18px] shrink-0" {...ICON}>
            <rect x="1.5" y="2.5" width="13" height="8.5" rx="1" />
            <path d="M5.5 14h5M8 11v3" />
            {on && <path d="M8 8.5V5M6.25 6.5L8 4.75l1.75 1.75" />}
        </svg>
    );
}

// PreviewIcon is a picture with a smaller one in its corner, for the
// member's own screen in the player.
function PreviewIcon() {
    return (
        <svg class="h-4 w-4" {...ICON}>
            <rect x="1.5" y="3" width="13" height="10" rx="1" />
            <rect x="8" y="8" width="5" height="3.5" rx="0.5" fill="currentColor" stroke="none" />
        </svg>
    );
}

function EyeIcon() {
    return (
        <svg class="h-4 w-4" {...ICON}>
            <path d="M1.5 8s2.5-4.5 6.5-4.5S14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" />
            <circle cx="8" cy="8" r="2" />
        </svg>
    );
}

function SoundOffIcon() {
    return (
        <svg class="h-4 w-4" {...ICON}>
            <path d="M2.5 6v4h2.5l3.5 3V3L5 6z" />
            <path d="M11 6l3.5 4M14.5 6L11 10" />
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
// disconnect those they rank above. A member who shares their screen has a
// LIVE mark (M4.2), which watches or stops watching in the page's own call,
// inCall, and elsewhere joins the call as label names it and watches;
// watched holds the members whose shares the page watches.
export function CallMembers({ denID, call, members, me, speaking, volumes, onProfile, inCall, label, watched, previewing }) {
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
                            {m.sharing && (
                                <LiveMark denID={denID} call={call} member={m} name={name} mine={m.id === me?.id} inCall={inCall} label={label}
                                    watching={!!watched?.has(m.id)} members={members} previewing={previewing} />
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
