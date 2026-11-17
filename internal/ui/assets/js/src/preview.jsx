// Appearance's preview: a sample of the chat atop Appearance, drawn by the
// components the chat draws with, on the member's theme and chat
// background, so each change shows as it's made: the theme and the
// background, Density, styled names, staff's marks, the verified mark and
// the call timer. Photos' and players' sizes stay out of it, since at
// theirs a photo would fill it. It holds its place while the settings
// under it scroll; on the phone layout it scrolls with them, since held
// it would leave them a third of the screen.

import { useState } from 'preact/hooks';
import { HoverRow } from './animations.js';
import { Avatar } from './avatar.jsx';
import { backgroundStyle } from './background.js';
import { CallLength } from './calltimer.jsx';
import { LongName, useBackground } from './components.jsx';
import { DENSITY_COMPACT, useDensity } from './density.js';
import { SpeakerIcon } from './icons.jsx';
import { MessageRow, continues } from './messages.jsx';
import { useNarrow } from './nav.js';
import { RoleMark } from './people.jsx';
import { VerifiedMark } from './private.jsx';
import { useStaffMarks } from './staffmarks.js';

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

/** AppearancePreview is the sample of the chat atop Appearance */
export function AppearancePreview() {
    const narrow = useNarrow();
    const line = useDensity() === DENSITY_COMPACT;
    const marks = useStaffMarks();
    const background = useBackground();
    const [at] = useState(() => Date.now());
    const messages = sampleMessages(at);
    const call = { channel_id: 'preview', started_at: at - 12 * MINUTE - 34 * 1000, record_ms: 3 * 60 * MINUTE };
    // Held, it covers the panel's padding above it, so the settings
    // scroll out of sight under it.
    const held = narrow ? '' : 'sticky top-0 z-10 -mx-4 -mt-4 bg-base-100 px-4 pb-1 pt-4';
    return (
        <div class={held} data-appearance-preview data-density={line ? 'compact' : 'default'}>
            <div class="overflow-hidden rounded-box border border-base-300 bg-base-100" role="group" aria-label="Preview">
                {/* Its messages take no pointer, so nothing in them acts; the
                    call's row below does, for the verified mark's hover. */}
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
                <div class="flex flex-col gap-1 border-t border-base-300 bg-base-200 px-2 py-1.5 text-sm">
                    <div class="relative flex min-h-7 items-center gap-2 rounded bg-base-300 px-2">
                        <span class="text-success"><SpeakerIcon size="h-4 w-4" /></span>
                        <span class="min-w-0 flex-1 truncate">Lounge</span>
                        <CallLength call={call} />
                    </div>
                    <HoverRow class="ml-8 flex min-w-0 items-center gap-1.5 text-base-content/80" data-preview-member>
                        <Avatar member={ALICE} size="sm" />
                        <LongName name={ALICE.display_name} member={ALICE} />
                        {marks && <RoleMark role={ALICE.role} />}
                        <VerifiedMark id="preview-verified" name={ALICE.display_name} />
                    </HoverRow>
                </div>
            </div>
        </div>
    );
}
