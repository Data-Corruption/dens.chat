// A den's emoji in text (M15.2): a name the den has, written :name:, shows
// its image in a square box of the line's height, which the image fills as
// it loads, so the list never moves; large in a message of emoji alone. An
// animated one plays as Appearance's Animations says (M15.1): always, or
// while the pointer is over its row.

import { createContext } from 'preact';
import { useContext, useMemo } from 'preact/hooks';
import { usePicture } from './animations.js';

// EmojiContext holds the den's emoji for the text a view shows: the den's
// ID, which their files come from, and the emoji by name, while emoji are
// on; all, every emoji the den has, on or off, whose scores stay. A den
// older than emoji, or one with them off, shows none, and :name: stays the
// text it is.
export const EmojiContext = createContext(null);

/** useDenEmoji makes the context's value for a den's view, kept while
    its emoji and their limit stay the same, so a text's parse, which
    depends on them, holds through the view's other changes */
export function useDenEmoji(denID, emoji, limits) {
    const on = !!limits && limits.emojis > 0;
    const all = emoji || [];
    const key = all.map((e) => `${e.id}:${e.name}:${e.image.id}:${e.image.animated ? 1 : 0}`).join(',');
    return useMemo(() => {
        const list = on ? all : [];
        return { denID, list, all, byName: new Map(list.map((e) => [e.name, e])), on, known: !!limits };
    }, [denID, key, on, !!limits]);
}

/** emojiFile is where the page gets an emoji's image, or its preview */
export function emojiFile(denID, emoji, still) {
    return `/api/dens/${denID}/files/${emoji.image.id}${still ? '/thumb' : ''}`;
}

/** DenEmoji shows one of the den's emoji: inline, in the line's box, or
    large, 48 pixels, or at size, a class of the caller's */
export function DenEmoji({ emoji, big, size }) {
    const ctx = useContext(EmojiContext);
    const how = usePicture(!!emoji.image.animated);
    if (!ctx) return `:${emoji.name}:`;
    const box = size || (big ? 'h-12 w-12' : 'h-[1lh] w-[1lh]');
    const fit = 'h-full w-full select-none object-contain';
    // The image's words are its name, so copying text with it in copies
    // :name:, and a picture that doesn't load reads as it was written.
    return (
        <span class={`relative inline-block shrink-0 align-top ${box}`} title={`:${emoji.name}:`} data-emoji={emoji.name}>
            <img src={emojiFile(ctx.denID, emoji, how !== 'file')} alt={`:${emoji.name}:`} draggable={false} class={fit} />
            {how === 'hover' && <img src={emojiFile(ctx.denID, emoji, false)} alt="" draggable={false} class={`absolute inset-0 ${fit}`} />}
        </span>
    );
}
