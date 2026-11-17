// Appearance's preview: a sample of the chat atop Appearance, drawn by the
// components the chat draws with, on the member's theme and chat
// background, so each change shows as it's made: the theme and the
// background, Density, styled names, staff's marks, the verified mark and
// the call timer. Photos' and players' sizes stay out of it, since at
// theirs a photo would fill it. It holds its place while the settings
// under it scroll; on the phone layout it scrolls with them, since held
// it would leave them a third of the screen.
//
// And the theme editor's: a small den in the theme being made, drawn by
// the same components on a box that wears the theme's variables alone, so
// the page keeps its own theme until the member saves it.

import { useMemo, useState } from 'preact/hooks';
import { HoverRow } from './animations.js';
import { Avatar } from './avatar.jsx';
import { backgroundStyle } from './background.js';
import { CallLength } from './calltimer.jsx';
import { ChannelHeader, ROW, ROW_BUTTON, UnreadPill, readStyle, rowShade } from './chat.jsx';
import { Button, LongName, Notice, Slider, Toggle, useBackground } from './components.jsx';
import { DENSITY_COMPACT, useDensity } from './density.js';
import { HashIcon, SpeakerIcon } from './icons.jsx';
import { Composer, MessageRow, continues } from './messages.jsx';
import { ShownColors } from './names.jsx';
import { useNarrow } from './nav.js';
import { RoleMark } from './people.jsx';
import { VerifiedMark } from './private.jsx';
import { useStaffMarks } from './staffmarks.js';
import { hexOf, variables } from './themes.js';
import { CallControls } from './voice.jsx';

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

// ChatSample is the preview's messages, on the member's chat background,
// or with few its first two. They take no pointer, so nothing in them acts.
function ChatSample({ at, few = false }) {
    const narrow = useNarrow();
    const line = useDensity() === DENSITY_COMPACT;
    const marks = useStaffMarks();
    const background = useBackground();
    const messages = sampleMessages(at).slice(0, few ? 2 : undefined);
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
                        <div class="mt-1 h-[72px] w-24 overflow-hidden rounded bg-base-300" data-preview-photo>
                            <img src={PHOTO} alt="" class="size-full object-cover" />
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

/** AppearancePreview is the sample of the chat atop Appearance */
export function AppearancePreview() {
    const narrow = useNarrow();
    const line = useDensity() === DENSITY_COMPACT;
    const [at] = useState(() => Date.now());
    // Held, it covers the panel's padding above it, so the settings
    // scroll out of sight under it.
    const held = narrow ? '' : 'sticky top-0 z-10 -mx-4 -mt-4 bg-base-100 px-4 pb-1 pt-4';
    return (
        <div class={held} data-appearance-preview data-density={line ? 'compact' : 'default'}>
            <div class="overflow-hidden rounded-box border border-base-300 bg-base-100" role="group" aria-label="Preview">
                <ChatSample at={at} />
                <div class="flex flex-col gap-1 border-t border-base-300 bg-base-200 px-2 py-1.5 text-sm">
                    <CallSample at={at} />
                </div>
            </div>
        </div>
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

/** DenPreview is a small den in theme, which the box it's drawn on wears
    alone: its channel list, with an unread channel and a mention, the
    page's call and its controls; the channel open, its header, Appearance's
    sample of the chat and the composer; and under them a button of each
    kind, a notice of each status, a toggle and a slider. Nothing in it
    acts. On the phone layout, where it stays in view above the editor's
    controls, it's cut to what they change: a channel's row on the list's
    background, two messages, the buttons and the notices. */
export function DenPreview({ theme }) {
    const vars = useMemo(() => variables(theme), [theme]);
    const colors = useMemo(() => ({
        backs: ['--color-base-100', '--color-base-200', '--color-base-300'].map((v) => hexOf(vars[v])),
        text: hexOf(vars['--color-base-content']),
    }), [vars]);
    const narrow = useNarrow();
    const [at] = useState(() => Date.now());
    return (
        <div data-theme={theme.dark ? 'dark' : 'light'} style={vars} inert role="group" aria-label="Preview" data-theme-preview
            class="flex select-none flex-col overflow-hidden rounded-box border border-base-300 text-sm">
            <ShownColors.Provider value={colors}>
                {narrow ? (
                    <>
                        <div class="bg-base-200 px-2 py-1" data-preview-list>{channelRow(CHANNELS[2])}</div>
                        <ChatSample at={at} few />
                    </>
                ) : (
                <div class="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
                    <div class="flex min-h-0 flex-col bg-base-200" data-preview-list>
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
                            elsewhere={{ mentions: 0, unread: false }} verified={false} online={false} membersOpen={false} muted={false}
                            onCompare={none} onMute={none} onChannels={none} onMembers={none} onProfile={none} />
                        <div class="min-h-0 flex-1 overflow-hidden"><ChatSample at={at} /></div>
                        <Composer denID="preview" limits={null} placeholder="Message #general" typing={[]} files={[]} candidates={[]} mentionable={null}
                            everyone={false} polls={false} shares={false} editors={[]} onEditors={none} onAddFiles={none} onRemoveFile={none} canSpoil={false}
                            onSpoiler={none} onCompare={none} onCancelReply={none} onSend={none} onEditLast={none} onTyping={none} narrow={narrow} touch={false} />
                    </div>
                </div>
                )}
                <div class={`flex flex-col border-t border-base-300 ${narrow ? 'gap-2 p-2' : 'gap-3 p-3'}`} data-preview-controls>
                    <div class="flex flex-wrap gap-2">
                        <Button kind="primary">Join</Button>
                        <Button>Invite</Button>
                        <Button kind="quiet">Later</Button>
                        <Button kind="danger">Leave</Button>
                    </div>
                    <div class="grid grid-cols-2 gap-2">
                        <Notice kind="info">Update ready.</Notice>
                        <Notice kind="success">Saved.</Notice>
                        <Notice kind="warning">Reconnecting…</Notice>
                        <Notice kind="error">Couldn't send.</Notice>
                    </div>
                    <div class={narrow ? 'hidden' : 'flex items-center gap-4'}>
                        <Toggle id="preview-toggle" label="On" checked onChange={none} />
                        <Slider value={60} aria-label="Volume" class="min-w-0 flex-1" onInput={none} />
                    </div>
                </div>
            </ShownColors.Provider>
        </div>
    );
}
