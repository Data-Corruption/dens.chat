// The call's pane: the voice channel of the page's call, in the main pane.
// Its header says where the call is, how it stands while it isn't
// connected, its lock and its length; each member is a tile that rings as
// they talk, with their marks and their menu; a share is a tile marked
// LIVE until the member watches it, which puts it on the stage, the
// members' tiles in a row under it; and the call's controls stand along
// its foot, labeled.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { Avatar } from '../ui/avatar.jsx';
import { CallLength } from './calltimer.jsx';
import { CallLock } from './calllock.jsx';
import { StatusText } from '../ui/components.jsx';
import { Name } from '../appearance/names.jsx';
import { AwayIcon, CogIcon, ShieldIcon, SpeakerIcon } from '../ui/icons.jsx';
import { ownsMenu, rightClick, underOf } from '../ui/menus.jsx';
import { useNarrow } from '../core/nav.js';
import { RoleMark } from '../people.jsx';
import { canShare } from '../core/platform.js';
import { openSettings } from '../ui/dialogs.js';
import { onPopOut, poppedOut } from './popout.js';
import { GAP, ROW_ROOM, paneLayout } from './pane.js';
import { PoppedOut, Tile, tilesOf } from './player.jsx';
import { SoundboardButton } from './soundboard.jsx';
import { useStaffMarks } from '../ui/staffmarks.js';
import { createPress, useCoarse, useHoldsButton } from '../core/touch.js';
import {
    HeadphonesIcon, LeaveIcon, MemberMarks, MicIcon, ScreenIcon, notices, setFoot, shareWhy, useCall, useMemberMenu,
} from './voice.jsx';
import { byName, leaveCall, rings, setAway, setDeafened, setMuted, setPreview, startShare, stopShare, watchShare } from './voice.js';

// WORDS says how a call stands while it isn't connected, in the header.
const WORDS = { joining: 'Joining…', connecting: 'Connecting…', reconnecting: 'Reconnecting…' };

// CallPane is the pane of the page's call, in channel: view is the den's,
// members its members by ID, and staffMuted whether staff muted this
// member. lead is what starts the header, the phone layout's way to the
// list. A member's name opens their profile through onProfile; moving
// gives staff Move to in a member's menu, as the list's rows do.
export function CallPane(props) {
    const state = useCall();
    if (!state.call) return null;
    return <Pane {...props} {...state} />;
}

// Pane is the call's pane while the page's call is known, so what measures
// it finds it from its first render.
function Pane({ denID, view, members, channel, staffMuted, lead, onProfile, moving, call, notice, speaking, sounding, volumes, share, shareNotice, watched, den, board }) {
    const narrow = useNarrow();
    const holds = useHoldsButton();
    const room = useRef(null);
    const foot = useRef(null);
    const [size, setSize] = useState({ w: 0, h: 0 });
    const [focus, setFocus] = useState('');
    const [windows, setWindows] = useState(poppedOut);
    const me = view.me;
    const menu = useMemberMenu({ denID, members, me, volumes, moving });
    useLayoutEffect(() => {
        const el = room.current;
        // Less the room's padding, 8 pixels a side.
        const measure = () => setSize({ w: Math.max(0, el.clientWidth - 16), h: Math.max(0, el.clientHeight - 16) });
        measure();
        const watch = new ResizeObserver(measure);
        watch.observe(el);
        return () => watch.disconnect();
    }, []);
    // The call's button floats above the foot, as above the composer.
    useEffect(() => {
        const el = foot.current;
        const watch = new ResizeObserver(() => setFoot(el.offsetHeight));
        watch.observe(el);
        setFoot(el.offsetHeight);
        return () => {
            watch.disconnect();
            setFoot(0);
        };
    }, []);
    useEffect(() => onPopOut(() => setWindows(poppedOut())), []);
    const mine = (view.calls || []).find((c) => c.channel_id === channel.id);
    const inIt = byName(mine?.members || [], members);
    // The stage holds the shares watched, and the member's own while they
    // show it; the rest are tiles, a share's beside its member's.
    const stage = tilesOf(watched, share);
    const staged = new Set(stage.map((w) => (w.self ? me.id : w.member)));
    const shares = inIt.filter((m) => m.sharing && !staged.has(m.id) && (m.id !== me.id || share?.status === 'live'));
    const layout = paneLayout(inIt.length + shares.length, stage.length, size.w, size.h);
    const focused = stage.some((w) => w.member === focus) ? focus : '';
    // A tile's menu opens at the pane's level, placed within the window, so
    // the row of tiles under a stage, which scrolls, doesn't clip it.
    const menuOf = menu.menu && inIt.find((m) => m.id === menu.menu.id && m.id !== me.id);
    const lines = narrow ? notices(call, notice, denID, staffMuted, shareNotice, holds) : [];
    const box = { width: `${layout.tiles.w}px`, height: `${layout.tiles.h}px` };
    const tiles = (
        <>
            {inIt.map((m) => (
                <MemberTile key={m.id} m={m} member={members.get(m.id)} me={me} box={box} big={layout.tiles.h >= 132}
                    talking={rings(m, me.id, speaking, sounding, !!call.deafened)} identities={view.keeps_identities ? view.identities : null}
                    since={call.since} menu={menu} onProfile={onProfile} />
            ))}
            {shares.map((m) => <ShareTile key={`share-${m.id}`} m={m} name={members.get(m.id)?.display_name || 'Someone'} mine={m.id === me.id} box={box} />)}
        </>
    );
    return (
        <section class="relative flex min-h-0 flex-1 flex-col" aria-label={`${channel.name}, the call`} data-call-pane={channel.id}>
            <header class="flex min-h-12 items-center gap-2 border-b border-base-300 px-4 py-2">
                {lead}
                <span class="flex min-w-0 items-center gap-1 font-semibold">
                    <span class={call.status === 'connected' ? 'text-success' : 'text-warning'}><SpeakerIcon size="h-4 w-4" /></span>
                    <span class="truncate">{channel.name}</span>
                    {channel.staff_only && <ShieldIcon />}
                </span>
                {WORDS[call.status] && <span role="status" class="shrink-0 text-sm text-warning" data-call-pane-status>{WORDS[call.status]}</span>}
                <CallLock call={call} den={den} />
                <span class="flex-1"></span>
                <span class="relative shrink-0"><CallLength call={mine} where="pane-" /></span>
            </header>
            <div ref={room} class={`min-h-0 flex-1 p-2 ${layout.stage ? 'flex flex-col overflow-hidden' : 'overflow-y-auto'}`}>
                {layout.stage && (
                    <div class="grid shrink-0 gap-1 overflow-hidden rounded-box bg-base-300" data-call-stage
                        style={{ height: `${layout.stage.height}px`, gridTemplateColumns: `repeat(${layout.stage.cols}, minmax(0, 1fr))`, gridTemplateRows: `repeat(${layout.stage.rows}, minmax(0, 1fr))` }}>
                        {stage.map((w) => (windows.has(w.member) ? (
                            <PoppedOut key={w.member} w={w} win={windows.get(w.member)} deafened={!!call.deafened} hidden={!!focused && w.member !== focused} fill={false} />
                        ) : (
                            <Tile key={w.member} w={w} deafened={!!call.deafened} hidden={!!focused && w.member !== focused} many={stage.length > 1}
                                focused={w.member === focused} onFocus={() => setFocus(w.member === focused ? '' : w.member)} />
                        )))}
                    </div>
                )}
                {layout.stage ? (
                    <div class="flex shrink-0 items-start overflow-x-auto"
                        style={{ gap: `${GAP}px`, marginTop: `${GAP}px`, height: `${ROW_ROOM}px`, justifyContent: 'safe center' }}
                        data-call-tiles>
                        {tiles}
                    </div>
                ) : (
                    <div class="grid content-center justify-center" style={{ gap: `${GAP}px`, minHeight: '100%', gridTemplateColumns: `repeat(${layout.tiles.cols}, ${layout.tiles.w}px)` }}
                        data-call-tiles>
                        {tiles}
                    </div>
                )}
            </div>
            {menuOf && menu.render(menuOf)}
            {menu.error && <StatusText kind="error" role="alert" class="px-4">{menu.error}</StatusText>}
            <div ref={foot} class="shrink-0 border-t border-base-300 bg-bar">
                {lines.length > 0 && <div class="flex flex-col gap-1 px-4 pt-2 text-xs">{lines}</div>}
                <Foot call={call} share={share} den={den} board={board} sounding={sounding} />
            </div>
        </section>
    );
}

// MemberTile is a member of the call: their picture, ringed while they
// talk, their name in its style, and their marks. Everyone else's has a ⋯
// for their menu, which a right click opens at the pointer, and a long
// press on a touch screen; the pane draws the menu.
function MemberTile({ m, member, me, box, big, talking, identities, since, menu, onProfile }) {
    const touch = useCoarse();
    const marks = useStaffMarks();
    const press = useRef(null);
    const other = m.id !== me.id;
    press.current ||= createPress({ onLong: (e) => menu.setMenu({ id: m.id, at: { x: e.clientX, y: e.clientY } }) });
    // The click a long press may be followed by goes nowhere.
    useEffect(() => {
        const swallow = (e) => {
            if (!press.current.click()) return;
            e.preventDefault();
            e.stopPropagation();
        };
        document.addEventListener('click', swallow, true);
        return () => {
            document.removeEventListener('click', swallow, true);
            press.current.cancel();
        };
    }, []);
    const name = member?.display_name || 'Unknown member';
    const open = menu.menu?.id === m.id;
    const opens = !other ? {} : touch ? {
        onPointerDown: (e) => press.current.down(e),
        onPointerMove: (e) => press.current.move(e),
        onPointerUp: () => press.current.up(),
        onPointerCancel: () => press.current.cancel(),
        onContextMenu: (e) => e.preventDefault(),
    } : {
        onContextMenu: (e) => {
            if (!ownsMenu(e.target, getSelection(), e.currentTarget)) return;
            e.preventDefault();
            menu.setMenu({ id: m.id, at: rightClick(e, e.currentTarget) || underOf(e.currentTarget) });
        },
    };
    return (
        <div class="group relative flex shrink-0 flex-col items-center justify-center gap-1.5 rounded-box bg-base-200 p-2 text-sm" style={box}
            data-call-tile={m.id} data-talking={talking || undefined} {...opens}>
            <button type="button" class="flex min-w-0 max-w-full flex-col items-center gap-1.5 rounded disabled:cursor-default" disabled={!member}
                onClick={(e) => onProfile?.(member, e.currentTarget)}>
                <span class={`inline-flex shrink-0 rounded-full ring-offset-base-200 ${big ? 'ring-4 ring-offset-2' : 'ring-2 ring-offset-1'} ${talking ? 'ring-success' : 'ring-transparent'}`}>
                    <Avatar member={member || { id: m.id }} size={big ? 'lg' : 'md'} />
                </span>
                <span class="flex min-w-0 max-w-full items-center gap-1">
                    <Name member={member} class="truncate font-medium" />
                    {marks && <RoleMark role={member?.role} />}
                </span>
            </button>
            {talking && <span class="sr-only">speaking</span>}
            <span class="flex min-h-4 items-center gap-1 text-base-content/70">
                <MemberMarks m={m} name={name} other={other} identities={identities} since={since} where="tile-" />
            </span>
            {other && (
                <button type="button" class="btn btn-ghost btn-xs absolute right-1 top-1 opacity-0 touch-room focus:opacity-100 group-hover:opacity-100 pointer-coarse:opacity-100"
                    data-call-menu aria-label={`Options for ${name} in the call`} aria-haspopup="menu" aria-expanded={open}
                    onClick={(e) => menu.setMenu(open ? null : { id: m.id, at: underOf(e.currentTarget) })}>⋯</button>
            )}
        </div>
    );
}

// ShareTile is a share nobody here watches yet, marked LIVE: a click
// watches it, which counts against its viewers and the den's upload, or
// for the member's own, shows it.
function ShareTile({ m, name, mine, box }) {
    const what = mine ? 'Show your screen' : `Watch ${name}'s screen`;
    return (
        <button type="button" class="flex shrink-0 flex-col items-center justify-center gap-1.5 rounded-box bg-neutral p-2 text-sm text-neutral-content hover:brightness-110"
            style={box} aria-label={what} title={what} data-share-tile={m.id} onClick={() => (mine ? setPreview(true) : watchShare(m.id, name))}>
            <span class="badge badge-error badge-sm font-semibold">LIVE</span>
            <span class="max-w-full truncate">{mine ? 'Your screen' : `${name}'s screen`}</span>
            <span class="text-xs opacity-70">{mine ? 'Show' : 'Watch'}</span>
        </button>
    );
}

// Foot holds the call's controls, labeled: share, where the browser can,
// mute, deafen, Away, where the den keeps it, the soundboard, where the
// den has one, the voice settings and leave.
function Foot({ call, share, den, board, sounding }) {
    const shareWait = call.status === 'joining' ? 'Joining the call' : shareWhy(call, den);
    return (
        <div class="flex flex-wrap items-center justify-center gap-1 px-2 py-2" role="group" aria-label="The call's controls" data-call-foot>
            {canShare() && (share ? (
                <FootButton label="Stop sharing" aria-label="Stop sharing your screen" pressed onClick={stopShare}><ScreenIcon on /></FootButton>
            ) : (
                <FootButton label="Share" aria-label="Share your screen" title={shareWait || 'Share your screen, a window or a tab with the call'}
                    disabled={!!shareWait} onClick={startShare}>
                    <ScreenIcon />
                </FootButton>
            ))}
            <FootButton label={call.muted ? 'Unmute' : 'Mute'} pressed={call.muted} danger={call.muted} onClick={() => setMuted(!call.muted)}>
                <MicIcon off={call.muted} size="h-[18px] w-[18px]" />
            </FootButton>
            <FootButton label={call.deafened ? 'Undeafen' : 'Deafen'} pressed={call.deafened} danger={call.deafened} onClick={() => setDeafened(!call.deafened)}>
                <HeadphonesIcon off={call.deafened} size="h-[18px] w-[18px]" />
            </FootButton>
            {den?.away && (
                <FootButton label="Away" pressed={!!call.away} title={call.away ? "You're away. Click when you're back." : 'Mutes you, and shows you away to the call'}
                    data-away-button="pane" onClick={() => setAway(!call.away)}>
                    <AwayIcon />
                </FootButton>
            )}
            <SoundboardButton call={call} den={den} board={board} sounding={sounding} where="pane" />
            <FootButton label="Voice settings" onClick={() => openSettings('voice')}><CogIcon /></FootButton>
            <FootButton label="Leave" aria-label="Leave the call" danger onClick={leaveCall}><LeaveIcon /></FootButton>
        </div>
    );
}

// FootButton is one of the foot's buttons: its icon over its label, and
// named as the block's button that does the same.
export function FootButton({ label, title, pressed, danger, disabled, onClick, children, ...rest }) {
    const color = danger ? 'text-error' : pressed ? 'text-success' : '';
    const name = rest['aria-label'] || label;
    return (
        <button type="button" class={`btn btn-ghost h-auto min-h-12 min-w-16 flex-col gap-0.5 px-2 py-1 text-xs font-normal ${color}`}
            aria-pressed={pressed === undefined ? undefined : !!pressed} title={title || name} disabled={disabled} onClick={onClick} {...rest} aria-label={name}>
            {children}
            <span aria-hidden="true">{label}</span>
        </button>
    );
}
