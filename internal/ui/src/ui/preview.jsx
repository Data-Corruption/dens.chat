// Appearance's preview: a small den in the theme shown,
// drawn by the page's own components on a box that wears the theme alone,
// so the page keeps its own until the member uses it. It shows every other
// Appearance setting as it's chosen: the chat's background, Density,
// styled names, staff's marks, the verified mark, the call timer, the
// sizes of photos and players, at a sixth of theirs, and file names on
// hover.

import { useMemo, useState } from 'preact/hooks';
import { HoverRow } from '../appearance/animations.js';
import { Avatar } from './avatar.jsx';
import { backgroundStyle } from '../appearance/background.js';
import { CallLength } from '../voice/calltimer.jsx';
import { ChannelHeader, ROW, ROW_BUTTON, UnreadPill, readStyle, rowShade } from '../chat/chat.jsx';
import { Button, LongName, Notice, Slider, Toggle, useBackground } from './components.jsx';
import { DENSITY_COMPACT, useDensity } from '../appearance/density.js';
import { useFileNames } from '../appearance/filenames.js';
import { HashIcon, SpeakerIcon } from './icons.jsx';
import { Composer, MessageRow, continues } from '../chat/messages.jsx';
import { ShownColors } from '../appearance/names.jsx';
import { useNarrow } from '../core/nav.js';
import { RoleMark } from '../people.jsx';
import { VerifiedMark } from '../private/private.jsx';
import { MEDIA, PLAYERS, mediaBox, useShownSize } from '../appearance/sizes.js';
import { useStaffMarks } from './staffmarks.js';
import { hexOf, variables } from '../appearance/themes.js';
import { CallControls } from '../voice/voice.jsx';

const MINUTE = 60 * 1000;

// The preview's members: the owner, whose name is styled, a member, and
// the member looking, who reacted.
const ALICE = { id: '901', username: 'alice', display_name: 'Alice', role: 'owner', name_style: { color: '#ec4899', color2: '#f59e0b', font: 'fredoka' } };
const BOB = { id: '902', username: 'bob', display_name: 'Bob', role: 'member' };
const YOU = { id: '903', username: 'you', display_name: 'You', role: 'member' };
const MEMBERS = new Map([ALICE, BOB, YOU].map((m) => [m.id, m]));
const nameOf = (id) => MEMBERS.get(id)?.display_name || 'Someone';

// PHOTO is the preview's photo, a landscape drawn here, as the page takes
// no picture from anywhere else.
const PHOTO = `data:image/svg+xml,${encodeURIComponent(
    "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 160 120'><defs><linearGradient id='s' x1='0' y1='0' x2='0' y2='1'>" +
    "<stop offset='0' stop-color='#7dd3fc'/><stop offset='1' stop-color='#fde68a'/></linearGradient></defs>" +
    "<rect width='160' height='120' fill='url(#s)'/><circle cx='118' cy='38' r='14' fill='#fbbf24'/>" +
    "<path d='M0 92 40 58 70 82 104 50 160 96V120H0z' fill='#4d7c0f'/><path d='M0 104q50-16 90-2t70-2v20H0z' fill='#365314'/></svg>",
)}`;

/** sampleMessages are the preview's messages, sent a few minutes before at */
export function sampleMessages(at) {
    return [
        { id: 'preview-1', author_id: ALICE.id, created_at: at - 4 * MINUTE, text: 'Anyone up for a game tonight?' },
        {
            id: 'preview-2', author_id: ALICE.id, created_at: at - 4 * MINUTE + 20 * 1000, text: 'The **Lounge** is open.',
            dm_reactions: [{ member_id: BOB.id, emoji: ['🎉'] }, { member_id: YOU.id, emoji: ['🎉', '👍'] }],
        },
        { id: 'preview-3', author_id: BOB.id, created_at: at - 2 * MINUTE, text: "I'm in. Here's where we left off:", photo: true },
    ];
}

const none = () => {};

// sampleCall is the preview's call, on its Lounge, a while before at.
const sampleCall = (at) => ({ channel_id: 'preview-lounge', started_at: at - 12 * MINUTE - 34 * 1000, record_ms: 3 * 60 * MINUTE });

// SCALE is how much smaller than a message's the preview draws its photo
// and its player.
const SCALE = 6;

// ChatSample is the preview's messages, on the member's chat background.
// They take no pointer, so nothing in them acts.
function ChatSample({ at }) {
    const narrow = useNarrow();
    const line = useDensity() === DENSITY_COMPACT;
    const marks = useStaffMarks();
    const background = useBackground();
    const named = useFileNames();
    const photo = mediaBox(useShownSize(MEDIA));
    const player = mediaBox(useShownSize(PLAYERS));
    const messages = sampleMessages(at);
    return (
        <div class="pointer-events-none select-none px-2 pb-2 pt-1" style={backgroundStyle(background)} inert>
            {messages.map((m, i) => (
                <MessageRow key={m.id} m={m} compact={continues(messages[i - 1], m, line)} line={line} marks={marks} author={MEMBERS.get(m.author_id)}
                    me={YOU} editing={false} canReply={false} canEdit={false} canDelete={false} link="" menuOpen={false} menuAt={null}
                    narrow={narrow} touch={false} editorNames={[]} candidates={[]} canReact={false} canRemoveReactions={false} nameOf={nameOf}
                    onReact={none} onReactPick={none} onRemoveReactions={none} onUpdated={none} onPolled={none} onMenu={none} onReply={none}
                    onEdit={none} onEditDone={none} onDelete={none} onJump={none} onView={none} onProfile={none} onToggle={none}
                    denID="" channelID="preview">
                    {m.photo && (
                        <div class="mt-1 flex flex-col items-start gap-1">
                            <div class="overflow-hidden rounded bg-base-300" style={{ width: `${photo.w / SCALE}px`, height: `${photo.h / SCALE}px` }}
                                title={named ? 'lake.jpg' : undefined} data-preview-photo>
                                <img src={PHOTO} alt="" class="size-full object-cover" />
                            </div>
                            <div class="relative overflow-hidden rounded bg-black" style={{ width: `${player.w / SCALE}px`, height: `${(player.w * 9) / 16 / SCALE}px` }}
                                data-preview-player>
                                <span class="absolute left-1/2 top-1/2 flex h-2/5 w-1/3 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-md bg-[#f00]">
                                    <svg viewBox="0 0 24 24" class="size-2/3 fill-white" aria-hidden="true"><path d="M8 5v14l11-7z" /></svg>
                                </span>
                            </div>
                        </div>
                    )}
                </MessageRow>
            ))}
        </div>
    );
}

// CallSample is the preview's call as the member's on its channel's row:
// its length, and its one member under it, whose verified mark shows on
// hover over them.
function CallSample({ at }) {
    const marks = useStaffMarks();
    return (
        <>
            <div class="relative flex min-h-7 items-center gap-2 rounded bg-base-300 px-2">
                <span class="text-success"><SpeakerIcon size="h-4 w-4" /></span>
                <span class="min-w-0 flex-1 truncate">Lounge</span>
                <CallLength call={sampleCall(at)} />
            </div>
            <HoverRow class="ml-8 flex min-w-0 items-center gap-1.5 text-base-content/80" data-preview-member>
                <Avatar member={ALICE} size="sm" />
                <LongName name={ALICE.display_name} member={ALICE} />
                {marks && <RoleMark role={ALICE.role} />}
                <VerifiedMark id="preview-verified" name={ALICE.display_name} />
            </HoverRow>
        </>
    );
}

// The den preview's channels: the one open, one unread, and one with
// mentions.
const CHANNELS = [
    { id: 'general', name: 'general', open: true },
    { id: 'news', name: 'news', unread: true },
    { id: 'games', name: 'games', unread: true, mentions: 2 },
];

// The den preview's call, as the page's own holds it, connected and
// sealed, and its den as the call's controls read it.
const IN_CALL = { den: 'preview', channel: 'preview-lounge', label: { den: 'Den', channel: 'Lounge' }, status: 'connected', muted: false, deafened: false, sealed: true };
const IN_DEN = { den: 'preview', me: YOU.id, role: 'member', calls: [], limits: null, sounds: [], soundLimits: null };

// channelRow is one of the den preview's channels as the list draws it.
const channelRow = (c) => (
    <div class={`${ROW} ${rowShade(c.open)}`} data-preview-channel={c.id}>
        <span class={ROW_BUTTON}>
            <span class="text-base-content/50"><HashIcon /></span>
            <span class={`truncate ${readStyle(c.unread, c.open)}`}>{c.name}</span>
        </span>
        {c.unread && <UnreadPill />}
        {c.mentions > 0 && <span class="badge badge-error badge-xs ml-1">{c.mentions}</span>}
    </div>
);

/** DenPreview is a small den in a theme, which the box it's drawn on
    wears alone: one of the built-in ones by its name, builtIn, or one of
    the member's own by its variables. It holds its channel list, with an
    unread channel and a mention, the page's call and its controls; the
    channel open, its header, the chat and the composer; and under them a
    button of each kind, a notice of each status, a toggle and a slider.
    Nothing in it acts. Narrower than a list beside a chat takes, the list
    stands above the chat. */
export function DenPreview({ theme, builtIn = '' }) {
    const vars = useMemo(() => variables(theme), [theme]);
    const colors = useMemo(() => ({
        backs: ['--color-base-100', '--color-base-200', '--color-base-300'].map((v) => hexOf(vars[v])),
        text: hexOf(vars['--color-base-content']),
    }), [vars]);
    const narrow = useNarrow();
    const [at] = useState(() => Date.now());
    return (
        <div data-theme={builtIn || (theme.dark ? 'dark' : 'light')} style={builtIn ? {} : vars} inert role="group" aria-label="Preview" data-theme-preview
            class="flex select-none flex-col overflow-hidden rounded-box border border-base-300 text-sm">
            <ShownColors.Provider value={colors}>
                <div class="grid @xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
                    <div class="flex min-h-0 flex-col bg-base-200 @max-xl:border-b @max-xl:border-base-300" data-preview-list>
                        <div class="truncate border-b border-base-300 p-3 font-semibold">Den</div>
                        <ul class="flex flex-1 flex-col p-2">
                            {CHANNELS.map((c) => <li key={c.id}>{channelRow(c)}</li>)}
                            <li class="flex flex-col gap-1"><CallSample at={at} /></li>
                        </ul>
                        <div class="border-t border-base-300 bg-bar px-2 py-1.5">
                            <CallControls denID="preview" call={IN_CALL} share={null} den={IN_DEN} sounding="" board={null} />
                        </div>
                    </div>
                    <div class="flex min-h-0 min-w-0 flex-col">
                        <ChannelHeader channel={{ id: 'preview', kind: 'text', name: 'general', description: 'Say hi' }} dm={null} me={YOU}
                            elsewhere={{ mentions: 0, unread: false }} verified={false} online={false} membersOpen={false} muted={null} level="mentions"
                            onCompare={none} onMute={none} onLevel={none} onChannels={none} onMembers={none} onProfile={none} />
                        <div class="min-h-0 flex-1 overflow-hidden"><ChatSample at={at} /></div>
                        <Composer denID="preview" limits={null} placeholder="Message #general" typing={[]} files={[]} candidates={[]} mentionable={null}
                            everyone={false} polls={false} shares={false} editors={[]} onEditors={none} onAddFiles={none} onRemoveFile={none} canSpoil={false}
                            onSpoiler={none} onCompare={none} onCancelReply={none} onSend={none} onEditLast={none} onTyping={none} narrow={narrow} touch={false} />
                    </div>
                </div>
                <div class="flex flex-col gap-3 border-t border-base-300 p-3" data-preview-controls>
                    <div class="flex flex-wrap gap-2">
                        <Button kind="primary">Join</Button>
                        <Button>Invite</Button>
                        <Button kind="quiet">Later</Button>
                        <Button kind="danger">Leave</Button>
                    </div>
                    <div class="grid gap-2 @md:grid-cols-2">
                        <Notice kind="info">Update ready.</Notice>
                        <Notice kind="success">Saved.</Notice>
                        <Notice kind="warning">Reconnecting…</Notice>
                        <Notice kind="error">Couldn't send.</Notice>
                    </div>
                    <div class="flex items-center gap-4">
                        <Toggle id="preview-toggle" label="On" checked onChange={none} />
                        <Slider value={60} aria-label="Volume" class="min-w-0 flex-1" onInput={none} />
                    </div>
                </div>
            </ShownColors.Provider>
        </div>
    );
}
