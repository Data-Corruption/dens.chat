// Reactions on the page (M15.3): under a message, after its files and
// covers, a row of chips, each emoji with how many reacted with it, marked
// where the member did, which a click adds the member's to or takes it
// back from, and whose tip says who: on hover, on focus, or a finger's long
// press; and a + at the row's end for the picker. The picker opens beside
// what opened it, the row's +, a message's smiley or its menu's Add
// reaction, and as a sheet on the phone layout. On a touch screen the long
// press's sheet opens with the quick reactions at its top; and staff take
// an emoji's reactions off a channel's message from its menu.

import { useContext, useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { ErrorText, TIP, useAction, useTip } from './components.jsx';
import { DenEmoji, EmojiContext } from './denemoji.jsx';
import { emojiList, inTone, loadEmoji, quickRow, skinTone } from './emoji.js';
import { EmojiPicker } from './emoji.jsx';
import { SmileyIcon } from './icons.jsx';
import { Dialog } from './manage.jsx';
import { chipsOf, reactionKey, reactionOf, whoReacted } from './reactions.js';
import { createPress } from './touch.js';

/** useChips lists a message's chips, the den's emoji by ID going with the
    den's own, for the views that show them */
export function useChips(m, me) {
    const ctx = useContext(EmojiContext);
    const all = useMemo(() => (ctx ? new Map(ctx.all.map((e) => [e.id, e])) : null), [ctx]);
    return { chips: chipsOf(m, me, all), all, ctx };
}

// shownEmoji is what a chip or a quick reaction shows: one of Unicode's,
// one of the den's as its image while emoji are on, or its name as text,
// as a message shows a name the den hasn't.
function shownEmoji(item, ctx, all, size) {
    if (item.emoji) return <span class="font-emoji leading-none">{item.emoji}</span>;
    const den = item.emoji_id ? all?.get(item.emoji_id) : ctx?.on ? ctx.byName.get(item.name) : null;
    if (den && ctx?.on) return <DenEmoji emoji={den} size={size} />;
    return <span class="text-xs">:{den?.name || item.name}:</span>;
}

// nameOfChip is how a chip's tip names its emoji.
function nameOfChip(chip, all) {
    if (chip.emoji) return chip.emoji;
    return `:${chip.emoji_id ? all?.get(chip.emoji_id)?.name : chip.name}:`;
}

/** Reactions is a message's row of chips, none until its first reaction.
    nameOf names a member; canReact says whether the member may react, and
    onToggle(chip, on) and onAdd(button) react, the latter opening the
    picker beside the row's + */
export function Reactions({ m, me, nameOf, canReact, onToggle, onAdd }) {
    const { chips, all, ctx } = useChips(m, me.id);
    if (!chips.length) return null;
    return (
        <div role="group" aria-label="Reactions" class="mt-1 flex flex-wrap items-center gap-1" data-reactions>
            {chips.map((c) => (
                <Chip key={c.key} chip={c} label={nameOfChip(c, all)} who={whoReacted(c.members, me.id, nameOf)} canReact={canReact}
                    onToggle={onToggle}>
                    {shownEmoji(c, ctx, all, 'h-[1.125rem] w-[1.125rem]')}
                </Chip>
            ))}
            {canReact && (
                <button type="button" class="inline-flex h-7 min-w-7 items-center justify-center rounded-full border border-base-300 px-1.5 text-base-content/60 hover:border-base-content/40 hover:text-base-content"
                    aria-label="Add a reaction" title="Add a reaction" data-add-reaction onClick={(e) => onAdd(e.currentTarget)}>
                    <SmileyIcon size="h-4 w-4" />
                    <span class="-ml-px text-xs leading-none" aria-hidden="true">+</span>
                </button>
            )}
        </div>
    );
}

// Chip is one emoji's reactions: a click adds the member's or takes it
// back; its tip says who, on hover or focus, or a finger's long press,
// which a finger's tap doesn't open, since the tap reacts.
function Chip({ chip, label, who, canReact, onToggle, children }) {
    const tip = useTip('bottom');
    const press = useRef(null);
    press.current ||= createPress({ onLong: () => tip.setPinned(true) });
    // A finger's press, whose long press takes the browser's own menu's
    // place, as on a message (M13.2).
    const finger = useRef(false);
    useEffect(() => () => press.current.cancel(), []);
    const said = `${who} reacted with ${label}`;
    return (
        <span {...tip.wrapProps} class={tip.wrapProps.class}>
            <span role="tooltip" class={TIP} {...tip.contentProps}>{said}</span>
            <button type="button" aria-pressed={chip.mine} aria-label={`${label}, ${chip.members.length}: ${said}`} aria-disabled={!canReact || undefined}
                data-reaction={chip.key} data-count={chip.members.length}
                class={`inline-flex h-7 min-w-7 select-none items-center gap-1 rounded-full border px-2 text-sm ${chip.mine ? 'border-primary bg-primary/15' : 'border-base-300 bg-base-200/60'} ${canReact ? 'hover:border-base-content/40' : 'cursor-default'}`}
                {...tip.triggerProps}
                onPointerDown={(e) => {
                    // The chip's long press is its own, not its message's,
                    // which would open the message's sheet over the tip.
                    e.stopPropagation();
                    finger.current = e.pointerType !== 'mouse';
                    if (finger.current) press.current.down(e);
                }}
                onPointerMove={(e) => press.current.move(e)}
                onPointerUp={() => press.current.up()}
                onPointerCancel={() => press.current.cancel()}
                onContextMenu={(e) => finger.current && e.preventDefault()}
                onClick={() => {
                    if (press.current.click() || !canReact) return;
                    onToggle(chip, !chip.mine);
                }}>
                {children}
                <span class="text-xs tabular-nums">{chip.members.length}</span>
            </button>
        </span>
    );
}

// PICKER is the picker's size beside what opened it, as emoji.jsx draws it.
const PICKER = { width: 352, height: 384 };

/** ReactionPicker opens the emoji picker to react to a message: beside
    what opened it, above where there's room and below otherwise, within
    the window; and on the phone layout as a sheet. A press elsewhere or
    Escape closes it, Escape giving the focus back to what opened it. */
export function ReactionPicker({ anchor, sheet, onPick, onClose }) {
    const emoji = useContext(EmojiContext);
    const [place] = useState(() => {
        if (sheet || !anchor) return null;
        const r = anchor.getBoundingClientRect();
        const vw = document.documentElement.clientWidth;
        const w = Math.min(PICKER.width, vw - 32);
        const h = Math.min(PICKER.height, innerHeight * 0.6);
        const right = Math.max(8, Math.min(vw - r.right, vw - w - 8));
        return r.top - 8 >= h ? { bottom: innerHeight - r.top + 8, right } : { top: Math.max(8, Math.min(r.bottom + 8, innerHeight - h - 8)), right };
    });
    useEffect(() => {
        if (sheet) return undefined;
        const away = (e) => {
            if (!e.target.closest?.('[data-emoji-picker]') && !anchor?.contains(e.target)) onClose();
        };
        const escape = (e) => {
            if (e.key !== 'Escape') return;
            e.preventDefault();
            onClose();
            if (anchor?.isConnected) anchor.focus();
        };
        document.addEventListener('pointerdown', away);
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('pointerdown', away);
            document.removeEventListener('keydown', escape);
        };
    }, [sheet]);
    return <EmojiPicker emoji={emoji} sheet={sheet} place={place} label="Add a reaction" onPick={(item, what) => onPick(item.den ? { emoji_id: item.den.id } : { emoji: what })} onClose={onClose} />;
}

/** QuickReactions is the row atop a message's sheet on a touch screen:
    the member's six most used, filled out with 👍 ❤️ 😂 😮 😢 🎉 while
    they've used fewer, each marked where they reacted with it, and a + for
    the picker */
export function QuickReactions({ denID, chips, onReact, onMore }) {
    const ctx = useContext(EmojiContext);
    const [list, setList] = useState(emojiList);
    useEffect(() => {
        if (!list) loadEmoji().then(setList, () => {});
    }, []);
    const all = useMemo(() => (ctx ? new Map(ctx.all.map((e) => [e.id, e])) : null), [ctx]);
    const tone = skinTone();
    const row = quickRow(denID, ctx?.all || [], Date.now(), list, !!ctx?.on);
    const cell = 'btn btn-ghost h-12 min-h-12 w-12 shrink-0 p-0 text-[1.75rem]';
    return (
        <div role="group" aria-label="Quick reactions" class="flex items-center justify-between gap-1 px-3 pb-2" data-quick-reactions>
            {row.map((x) => {
                const item = x.den ? { emoji_id: x.den.id } : { emoji: inTone(x.unicode, tone) };
                const mine = chips.some((c) => c.mine && (c.key === reactionKey(item) || (x.den && c.name === x.den.name)));
                return (
                    <button key={x.den ? `d${x.den.id}` : x.unicode.emoji} type="button" role="menuitem" class={`${cell} font-emoji ${mine ? 'btn-active' : ''}`}
                        aria-label={x.den ? `:${x.den.name}:` : x.unicode.name} aria-pressed={mine} data-quick-reaction={item.emoji || `:${x.den.name}:`}
                        onClick={() => onReact(item, !mine)}>
                        {shownEmoji(item, ctx, all, 'h-8 w-8')}
                    </button>
                );
            })}
            <button type="button" role="menuitem" class={`${cell} text-base-content/70`} aria-label="More reactions" data-more-reactions onClick={onMore}>
                <SmileyIcon size="h-6 w-6" />
                <span class="-ml-1 text-base leading-none" aria-hidden="true">+</span>
            </button>
        </div>
    );
}

/** RemoveReactions takes an emoji's reactions off a channel's message, at
    staff's word: those of the members they outrank, as their other powers
    reach, which each emoji's button counts. rankOf ranks a member by ID. */
export function RemoveReactions({ denID, m, me, myRank, rankOf, nameOf, onClose }) {
    const { chips, all, ctx } = useChips(m, me.id);
    const act = useAction();
    return (
        <Dialog title="Remove reactions" onClose={onClose}>
            <p class="text-sm">Take an emoji's reactions off this message, for everyone: those of the members you outrank.</p>
            {chips.length === 0 && <p class="text-sm text-base-content/70">No reactions are left on it.</p>}
            <ul class="flex flex-col gap-2">
                {chips.map((c) => {
                    const lower = c.members.filter((id) => rankOf(id) < myRank).length;
                    return (
                        <li key={c.key} class="flex items-center gap-2" data-remove-reaction={c.key}>
                            <span class="inline-flex h-7 min-w-7 shrink-0 items-center justify-center rounded-full border border-base-300 px-2 text-base">
                                {shownEmoji(c, ctx, all, 'h-5 w-5')}
                            </span>
                            <span class="min-w-0 flex-1 text-sm text-base-content/80">{whoReacted(c.members, me.id, nameOf)}</span>
                            <button type="button" class="btn btn-sm shrink-0" disabled={!lower || act.busy}
                                onClick={() => act.run(() => api.post(`/api/dens/${denID}/messages/${m.id}/reactions/clear`, reactionOf(c)))}>
                                {lower ? `Remove ${lower}` : 'None to remove'}
                            </button>
                        </li>
                    );
                })}
            </ul>
            <ErrorText message={act.error} />
        </Dialog>
    );
}
