// A member's name as they styled it, wherever it's theirs: the
// member list, messages and the quotes of replies, a call's rows, the DM
// list and a DM's header, and the profile card. names.js reads the style
// and says how it shows on this theme, to this reader; here it becomes a
// class and a style the page sets through the CSSOM, as it sets a theme's.

import { createContext } from 'preact';
import { useContext, useEffect, useMemo, useState } from 'preact/hooks';
import { STYLES_ALL, fontFamily, fontRoom, nameLook, useNameStyles } from './names.js';
import { onTheme, themeColors } from './theme.js';

/** ShownColors are the colors a theme other than the page's gives what's
    inside it, as themeColors has them, for names to read against: the
    theme editor's preview wears the theme being made. */
export const ShownColors = createContext(null);

function useThemeColors() {
    const shown = useContext(ShownColors);
    const [colors, setColors] = useState(themeColors);
    useEffect(() => {
        const update = () => setColors(themeColors());
        update();
        return onTheme(update);
    }, []);
    return shown || colors;
}

const PLAIN = { class: '', style: undefined, gradient: false };

// dress is a look as the element holding the name wears it. A gradient is
// the element's background, cut to its text.
function dress(look) {
    if (!look) return PLAIN;
    const style = {};
    const family = look.font && fontFamily(look.font);
    let [before, after] = [0, 0];
    if (family) {
        style.fontFamily = family;
        // A face's tall and deep letters reach past the line, and some past
        // the name's ends, where a row that cuts long names short would cut
        // them, and a gradient wouldn't reach: the box grows to hold them,
        // and takes no more room for it.
        [before, after] = fontRoom(look.font);
        style.paddingBlock = '0.3em';
        style.marginBlock = '-0.3em';
        style.paddingInline = `${before}em ${after}em`;
        style.marginInline = `-${before}em -${after}em`;
    }
    const [from, to] = look.colors;
    if (to) {
        // The box's room past the name keeps its ends' colors, so the
        // gradient runs across the name alone.
        style.backgroundImage = family ? `linear-gradient(90deg, ${from} ${before}em, ${to} calc(100% - ${after}em))` : `linear-gradient(90deg, ${from}, ${to})`;
        style.color = 'transparent';
    } else if (from) {
        style.color = from;
    }
    return { class: to ? 'bg-clip-text' : '', style, gradient: !!to };
}

/** useNameLook is what the element holding a member's name wears: a class,
    a style, and whether it's a gradient, whose text goes through
    nameText. always shows the style whatever the reader chose, as the
    member's own preview does. */
export function useNameLook(member, always = false) {
    const choice = useNameStyles();
    const theme = useThemeColors();
    const s = member?.name_style;
    return useMemo(() => dress(nameLook(s, always ? STYLES_ALL : choice, theme)), [s?.font, s?.color, s?.color2, always, choice, theme]);
}

// Emoji in a row: a gradient shows through a name's transparent text, and
// would show through its emoji's shapes too.
const EMOJI = /(?:(?:\p{Emoji_Presentation}|\p{Extended_Pictographic}\p{Variation_Selector})(?:\p{Variation_Selector}|\p{Emoji_Modifier}|\p{Join_Control}\p{Extended_Pictographic}\p{Variation_Selector}?)*)+/gu;

/** nameText is a name's text for an element wearing a gradient: its emoji
    in spans of the theme's own color, so they keep theirs */
export function nameText(name, look) {
    if (!look.gradient) return name;
    const parts = [];
    let at = 0;
    for (const m of name.matchAll(EMOJI)) {
        if (m.index > at) parts.push(name.slice(at, m.index));
        parts.push(<span key={m.index} class="text-base-content">{m[0]}</span>);
        at = m.index + m[0].length;
    }
    if (at === 0) return name;
    if (at < name.length) parts.push(name.slice(at));
    return parts;
}

/** Name is a member's name as they styled it, or fallback for a member the
    page doesn't know */
export function Name({ member, fallback = 'Unknown member', always = false, class: cls = '' }) {
    const look = useNameLook(member, always);
    return (
        <span class={`${cls} ${look.class}`.trim() || undefined} style={look.style} data-name>
            {nameText(member ? member.display_name : fallback, look)}
        </span>
    );
}
