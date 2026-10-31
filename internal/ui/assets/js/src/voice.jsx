// A call's controls, which follow the call from den to den: in a block at
// the foot of a den's channel list, and boxed on the other pages; and who
// is in each voice channel's call, with who is speaking, how loud each
// plays here, and staff's controls (M3). From M4.2 the call's block has a
// row for sharing the member's screen, and the list marks who shares,
// which a click watches. From M13.4 the phone layout shows the call in a
// bar across the top of the channel's pane, and a touch screen holds the
// microphone on the call's button, by the thumb.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { Avatar } from './avatar.jsx';
import { Tip } from './components.jsx';
import { ICON, SpeakerIcon } from './icons.jsx';
import { nav, useNarrow } from './nav.js';
import { rank } from './people.jsx';
import { canShare } from './platform.js';
import { openSettings } from './settings.jsx';
import { shareBlocked, watchersOf } from './share.js';
import { Sheet, SheetRow } from './sheet.jsx';
import { createHold, useHoldsButton } from './touch.js';
import {
    byName, clearNotice, clearShareNotice, joinAndWatch, keysElsewhere, leaveCall, onCall, pressButton, releaseButton, rings, setDeafened, setMuted,
    setPreview, setVolume, startShare, stopShare, unwatchShare, voiceSettings, volumeIn, watchShare,
} from './voice.js';

// useCall is the page's call, why the last one ended, who is speaking in
// it, how loud each member plays here, and from M4.2 its share, the shares
// it watches, and what it knows of the call's den.
export function useCall() {
    const [state, setState] = useState({ call: null, notice: null, speaking: new Set(), volumes: {}, share: null, shareNotice: null, watched: [], den: null });
    useEffect(() => onCall(setState), []);
    return state;
}

// useCallButton says whether the call's button shows (M13.4): in a call,
// where the page has the button. It changes only as those do, so what
// leaves room for the button doesn't render as the call's rings change.
export function useCallButton() {
    const holds = useHoldsButton();
    const [inCall, setInCall] = useState(false);
    useEffect(() => onCall((s) => setInCall(!!s.call)), []);
    return holds && inCall;
}

// foot is the height of what stands at the foot of the channel's pane,
// the composer among it, which the call's button floats above (M13.4): the
// channel's pane says it as it changes, and 0 once it's gone.
let foot = 0;
const footListeners = new Set();

export function setFoot(height) {
    if (height === foot) return;
    foot = height;
    footListeners.forEach((fn) => fn(height));
}

function useFoot() {
    const [height, setHeight] = useState(foot);
    useEffect(() => {
        setHeight(foot);
        footListeners.add(setHeight);
        return () => footListeners.delete(setHeight);
    }, []);
    return height;
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
// den's, or that push to talk has no key yet, where the call's button
// isn't there to hold (M13.4).
function notices(call, notice, denID, staffMuted, shareNotice, holds) {
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
    // A key the desktop runs counts as much as one the page knows (M7.2).
    if (call && ptt.mode === 'ptt' && !holds && !ptt.keys.ptt && !keysElsewhere().covers.includes('ptt')) {
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
// its controls in a row. On the phone layout it keeps clear of the call's
// button in the screen's corner, as roomy says (M13.4).
export function CallBlock({ denID, staffMuted, roomy }) {
    const { call, notice, share, shareNotice, den } = useCall();
    const holds = useHoldsButton();
    const lines = notices(call, notice, denID, staffMuted, shareNotice, holds);
    if (!call && !lines.length) return null;
    return (
        <div class={`flex flex-col gap-1 border-t border-base-300 bg-bar py-1.5 pl-2 ${roomy && call ? 'pr-10' : 'pr-2'}`}>
            {lines.length > 0 && <div class="flex flex-col gap-1 px-1 py-0.5 text-xs">{lines}</div>}
            {call && share && <ShareRow call={call} share={share} den={den} />}
            {call && <CallControls denID={denID} call={call} share={share} den={den} />}
        </div>
    );
}

// CallBox is the call, and its notices, boxed on the pages other than a
// den's, whose channel lists hold them at their foot.
export function CallBox() {
    const { call, notice, share, shareNotice, den } = useCall();
    const holds = useHoldsButton();
    const lines = notices(call, notice, undefined, false, shareNotice, holds);
    if (!call && !lines.length) return null;
    return (
        <div class="flex flex-col gap-2 rounded-box bg-base-200 p-2">
            {lines.length > 0 && <div class="flex flex-col gap-1 text-xs">{lines}</div>}
            {call && share && <ShareRow call={call} share={share} den={den} />}
            {call && <CallControls call={call} share={share} den={den} labeled />}
        </div>
    );
}

// CallBar is the call across the top of the channel's pane on the phone
// layout (M13.4), under its header, for 40 pixels while in a call, as a
// wider window shows its block beside the channel: where it is and how it
// stands, deafen and leave. A tap on the rest goes to the list of the
// call's den, where its members and its other controls are.
export function CallBar({ denID }) {
    const { call } = useCall();
    if (!call) return null;
    const where = call.den === denID ? call.label.channel : `${call.label.channel} in ${call.label.den}`;
    const said = STATUS[call.status](where);
    const color = call.status === 'connected' ? 'text-success' : 'text-warning';
    return (
        <div class="flex h-10 shrink-0 items-center gap-1 border-b border-base-300 bg-bar pr-2" data-call-bar>
            <button type="button" class="flex h-full min-w-0 flex-1 items-center gap-2 pl-4 pr-1 text-left" onClick={() => nav().den(call.den, '', 'list')}>
                <span class={`shrink-0 ${color}`}><SpeakerIcon /></span>
                <span role="status" class="truncate text-sm">{said}</span>
                <span class="sr-only">Show the call</span>
            </button>
            <IconButton label={call.deafened ? 'Undeafen' : 'Deafen'} pressed={call.deafened} danger={call.deafened}
                onClick={() => setDeafened(!call.deafened)}>
                <HeadphonesIcon off={call.deafened} size="h-[18px] w-[18px]" />
            </IconButton>
            <IconButton label="Leave the call" danger onClick={leaveCall}>
                <LeaveIcon />
            </IconButton>
        </div>
    );
}

// CallButton is the call's button (M13.4): where the page has it, in a
// call, a round button of 64 pixels at the screen's foot on the right,
// over every pane and above the composer, where a thumb rests. It holds
// the microphone as the member's role for it says: a tap toggles mute, or
// a press mutes while held, or talks while held, ringing for the call as
// push to talk's key does. It shows the microphone's state: talking in the
// ring's color, muted struck through.
export function CallButton() {
    const { call, speaking, den } = useCall();
    const shows = useCallButton();
    const above = useFoot();
    if (!shows || !call) return null;
    const mine = den?.calls.find((c) => c.channel_id === call.channel)?.members.find((m) => m.id === den.me);
    const muted = call.muted || !!mine?.staff_muted;
    return <HeldButton role={voiceSettings().button} muted={muted} talking={!muted && speaking.has('self')}
        bottom={above ? above + 24 : 16} />;
}

// HeldButton is the call's button while it shows. A press lets go however
// it ends, so the microphone never stays open: lifted, taken by the system,
// as Android's back gesture or its notification shade takes it, its
// pointer's capture lost, the page hidden or out of focus, or the button
// gone, as when the call ends. Its pointer's events are all it takes, so
// its touch is the page's alone: the browser makes no gesture of it, as
// the long press a phone answers with a buzz, as if to select something.
function HeldButton({ role, muted, talking, bottom }) {
    const [held, setHeld] = useState(false);
    const [hold] = useState(() => createHold({
        onPress: () => {
            setHeld(true);
            pressButton();
        },
        onRelease: () => {
            setHeld(false);
            releaseButton();
        },
    }));
    useEffect(() => {
        const away = () => hold.away();
        const hidden = () => document.hidden && hold.away();
        window.addEventListener('blur', away);
        document.addEventListener('visibilitychange', hidden);
        return () => {
            window.removeEventListener('blur', away);
            document.removeEventListener('visibilitychange', hidden);
            hold.away();
        };
    }, []);
    const label = role === 'toggleMute' ? (muted ? 'Unmute' : 'Mute') : `${role === 'ptt' ? 'Hold to talk' : 'Hold to mute'}${muted ? ', muted' : ''}`;
    const look = talking ? 'bg-success text-success-content' : muted ? 'bg-neutral text-error' : 'bg-neutral text-neutral-content';
    return (
        <button type="button" data-call-button={role} aria-label={label} aria-pressed={role === 'toggleMute' ? muted : held}
            class={`fixed right-4 z-40 flex h-16 w-16 touch-none select-none items-center justify-center rounded-full shadow-lg transition-colors [-webkit-touch-callout:none] ${look}`}
            style={{ bottom: `${bottom}px` }}
            onPointerDown={hold.down} onPointerUp={hold.up} onPointerCancel={hold.up} onLostPointerCapture={hold.up}
            onTouchStart={(e) => e.preventDefault()} onMouseDown={(e) => e.preventDefault()} onKeyDown={hold.keydown} onKeyUp={hold.keyup}
            onClick={hold.click} onContextMenu={(e) => e.preventDefault()}>
            <MicIcon off={muted} size="h-7 w-7" />
        </button>
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
    // The sidebar is narrow, so who watches is a count, with their names in
    // its tip.
    const watching = viewers.length ? `Watching: ${viewers.join(', ')}` : 'Nobody is watching yet';
    return (
        <div class="flex h-8 items-center gap-2 pl-2 pr-1 text-sm">
            <span class="badge badge-error badge-sm shrink-0 font-semibold">LIVE</span>
            <span role="status" class="sr-only">{watching}</span>
            <Tip id="share-watching" label={watching} trigger={
                <span class="flex items-center gap-1 text-base-content/70">
                    <EyeIcon />
                    <span class="tabular-nums">{viewers.length}</span>
                </span>
            }>
                {watching}
            </Tip>
            <span class="flex-1"></span>
            {!share.sound && <Tip id="share-sound" label="no sound" trigger={<SoundOffIcon />}>{soundless}</Tip>}
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
            title={why || 'Share your screen, a window or a tab with the call'}
            onClick={startShare}>
            <ScreenIcon />
        </IconButton>
    );
}

// CallControls are the call, in this den or another, in a row: a speaker
// for whether it's connected, whose tip says where, and the buttons to
// mute, deafen, open the voice settings and leave, spread evenly. labeled
// spells out where beside the speaker, for pages with room for it. They
// show only what the browser can do: a share where it can share (M13.4).
function CallControls({ denID, call, share, den, labeled }) {
    const where = call.den === denID ? call.label.channel : `${call.label.channel} in ${call.label.den}`;
    const said = STATUS[call.status](where);
    const color = call.status === 'connected' ? 'text-success' : 'text-warning';
    const sharing = canShare() && <ShareButton call={call} share={share} den={den} />;
    const buttons = (
        <>
            {labeled && sharing}
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
            <span class={`flex h-8 w-8 shrink-0 items-center justify-center ${color}`}>
                <span role="status" class="sr-only">{said}</span>
                <Tip id="call-status" side="top" label={said} trigger={<span class={color}><SpeakerIcon /></span>}>{said}</Tip>
            </span>
            {sharing}
            {buttons}
        </div>
    );
}

// LiveMark marks a member who shares their screen, with who watches it in
// its title, or in its tip where it does nothing else. In the page's own
// call it watches the share, or stops watching it; elsewhere it joins the
// call and watches. The member's own mark shows their screen in the
// player, or hides it, while this page shares it; previewing says whether
// it shows.
function LiveMark({ denID, call, member, name, mine, inCall, label, watching, members, previewing }) {
    const viewers = watchersOf(call, member.id).map((id) => members.get(id)?.display_name || 'Someone');
    const seen = viewers.length ? ` Watching: ${viewers.join(', ')}.` : '';
    const style = `badge badge-xs shrink-0 font-semibold ${watching || (mine && previewing) ? 'badge-error' : 'badge-error badge-outline'}`;
    // The mark is small, and its button reaches 24 pixels round it.
    const mark = 'relative inline-flex min-h-6 shrink-0 cursor-pointer items-center touch-room';
    if (mine && !inCall) {
        return (
            <Tip id={`live-${member.id}`} label="You're sharing your screen" trigger={<span class={style}>LIVE</span>}>
                You're sharing your screen.{seen}
            </Tip>
        );
    }
    if (mine) {
        const what = previewing ? 'Hide your screen from the player' : 'Show your screen in the player';
        return (
            <button type="button" class={mark} title={`You're sharing your screen. ${what}.${seen}`} aria-label={what}
                aria-pressed={previewing} onClick={() => setPreview(!previewing)}>
                <span class={style}>LIVE</span>
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
        <button type="button" class={mark} title={`${what}.${seen}`} aria-label={what} aria-pressed={inCall ? watching : undefined}
            onClick={act}>
            <span class={style}>LIVE</span>
        </button>
    );
}

// CallMenuRow is an action in the menu of a member in the call, in a wider
// window; the phone layout's sheet has SheetRows.
function CallMenuRow({ danger, onClick, children }) {
    return (
        <button type="button" class={`btn btn-ghost btn-xs justify-start pointer-coarse:btn-sm ${danger ? 'text-error' : ''}`} onClick={onClick}>{children}</button>
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
// In the page's own call, a ring marks whoever is speaking or holds push to
// talk (M11): speaking holds member IDs, and "self" for this page's member,
// me. A member's name opens
// their profile through onProfile. Everyone else's row has a menu that sets
// how loud they play here, from volumes, and gives staff a way to mute or
// disconnect those they rank above. A member who shares their screen has a
// LIVE mark (M4.2), which watches or stops watching in the page's own call,
// inCall, and elsewhere joins the call as label names it and watches;
// watched holds the members whose shares the page watches.
export function CallMembers({ denID, call, members, me, speaking, volumes, onProfile, inCall, label, watched, previewing }) {
    const [menu, setMenu] = useState('');
    const [error, setError] = useState('');
    const narrow = useNarrow();
    // An open menu closes on a click anywhere else, or on Escape; on the
    // phone layout it's a sheet, which closes itself (M13.2).
    useEffect(() => {
        if (!menu || narrow) return undefined;
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
    }, [menu, narrow]);
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
    // options are what the menu of a member other than this one holds: how
    // loud they play here, on the phone layout's sheet a slider a finger
    // can hold (M13.4), and for staff who outrank them, muting them for
    // everyone and disconnecting them.
    function options(m, name, pct, moderate, Row) {
        return (
            <>
                <label class={`flex items-center ${narrow ? 'h-14 gap-3 px-4 text-sm' : 'min-h-6 gap-2 text-xs'}`} title="How loud they play for you">
                    <span>Volume</span>
                    <input type="range" class={`range min-w-0 flex-1 ${narrow ? 'range-xl' : 'range-md'}`} min="0" max="100" step="5" value={pct}
                        aria-label={`Volume for ${name}`} aria-valuetext={`${pct}%`}
                        onInput={(e) => setVolume(denID, m.id, Number(e.currentTarget.value) / 100)} />
                    <span class="w-9 text-right tabular-nums">{pct}%</span>
                </label>
                {moderate && (
                    <>
                        <Row onClick={() => act(m.id, 'voice-mute', { muted: !m.staff_muted })}>
                            {m.staff_muted ? 'Unmute for everyone' : 'Mute for everyone'}
                        </Row>
                        <Row danger onClick={() => act(m.id, 'disconnect')}>Disconnect from the call</Row>
                    </>
                )}
            </>
        );
    }
    return (
        <>
            {/* Rings stand 3px out from each avatar, so rows keep 8px apart. */}
            <ul class="mb-2 ml-8 mt-1 flex flex-col gap-2 pointer-coarse:gap-0" aria-label="In the call">
                {byName(call.members, members).map((m) => {
                    const member = members.get(m.id);
                    const name = member?.display_name || 'Unknown member';
                    const talking = rings(m, me?.id, speaking);
                    // Everyone else's row has a menu, for their volume here,
                    // and for staff's actions when the member here outranks them.
                    const other = me && m.id !== me.id;
                    const moderate = me && member && rank(me.role) > rank(member.role);
                    const pct = Math.round(volumeIn(volumes || {}, denID, m.id) * 100);
                    return (
                        <li key={m.id} class="group relative flex min-w-0 items-center gap-1 text-sm text-base-content/80 pointer-coarse:min-h-11">
                            <button type="button" class="flex min-w-0 flex-1 items-center gap-1.5 self-stretch rounded pr-1 text-left hover:text-base-content disabled:cursor-default"
                                disabled={!member} onClick={() => onProfile?.(member)}>
                                {/* A block of the avatar's own size, so the ring is round. */}
                                <span class={`inline-flex shrink-0 rounded-full ring-2 ring-offset-1 ring-offset-base-200 ${talking ? 'ring-success' : 'ring-transparent'}`}>
                                    <Avatar member={member || { id: m.id }} size="sm" />
                                </span>
                                <span class="truncate">{name}</span>
                            </button>
                            {talking && <span class="sr-only">speaking</span>}
                            {/* Deafening mutes too, so it shows both (M11). Each mark's
                                tip says what it means (M13.2). */}
                            {(m.muted || m.deafened) && (
                                <Tip id={`muted-${m.id}`} label="muted" trigger={<MicIcon off size="h-3.5 w-3.5" />}>Muted</Tip>
                            )}
                            {m.deafened && (
                                <Tip id={`deafened-${m.id}`} label="deafened" trigger={<HeadphonesIcon off size="h-3.5 w-3.5" />}>
                                    Deafened: hears nothing of the call
                                </Tip>
                            )}
                            {m.staff_muted && (
                                <Tip id={`staff-muted-${m.id}`} label="muted by staff" trigger={
                                    <span class="flex items-center gap-0.5 text-xs text-warning"><MicIcon off size="h-3.5 w-3.5" />staff</span>
                                }>
                                    Muted by staff: nobody hears them
                                </Tip>
                            )}
                            {m.sharing && (
                                <LiveMark denID={denID} call={call} member={m} name={name} mine={m.id === me?.id} inCall={inCall} label={label}
                                    watching={!!watched?.has(m.id)} members={members} previewing={previewing} />
                            )}
                            {other && pct < 100 && (
                                <Tip id={`volume-${m.id}`} label={pct === 0 ? 'Muted for you' : `Plays at ${pct}% for you`}
                                    trigger={<span class="text-xs text-base-content/50">{pct}%</span>}>
                                    {pct === 0 ? 'Muted for you' : `Plays at ${pct}% for you`}
                                </Tip>
                            )}
                            {other && (
                                <button type="button" class="btn btn-ghost btn-xs relative opacity-0 touch-room focus:opacity-100 group-hover:opacity-100 pointer-coarse:opacity-100" data-call-menu
                                    aria-label={`Options for ${name} in the call`} aria-haspopup="menu" aria-expanded={menu === m.id}
                                    onClick={() => setMenu(menu === m.id ? '' : m.id)}>⋯</button>
                            )}
                            {other && menu === m.id && !narrow && (
                                <div class="absolute right-0 top-full z-10 flex w-52 flex-col gap-1 rounded-box bg-base-100 p-2 shadow" data-call-menu>
                                    {options(m, name, pct, moderate, CallMenuRow)}
                                </div>
                            )}
                            {other && menu === m.id && narrow && (
                                <Sheet label={`Options for ${name} in the call`} title={<span class="font-semibold">{name}</span>} onClose={() => setMenu('')}>
                                    {options(m, name, pct, moderate, SheetRow)}
                                </Sheet>
                            )}
                        </li>
                    );
                })}
            </ul>
            {error && <p class="ml-8 text-xs text-error">{error}</p>}
        </>
    );
}
