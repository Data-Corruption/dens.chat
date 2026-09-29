// A member's avatar: the picture on their profile, or the default, a
// colored circle with the first letter or digit of their username. The
// color comes from the member's ID, so every device shows the same one
// without the den storing or sending anything.

import { createContext } from 'preact';
import { useContext, useState } from 'preact/hooks';

// DenContext names the den a view shows, so pictures can be fetched from
// it wherever they appear.
export const DenContext = createContext('');

// Colors that keep white text readable in both themes. Written out in
// full so Tailwind finds them.
const COLORS = [
    'bg-red-600', 'bg-orange-700', 'bg-amber-700', 'bg-green-700', 'bg-emerald-700', 'bg-teal-700',
    'bg-cyan-700', 'bg-sky-700', 'bg-blue-600', 'bg-indigo-600', 'bg-violet-600', 'bg-fuchsia-700',
];

// avatarColor picks a member's color from their ID (FNV-1a).
export function avatarColor(id) {
    let h = 0x811c9dc5;
    for (const c of String(id)) {
        h ^= c.charCodeAt(0);
        h = Math.imul(h, 0x01000193) >>> 0;
    }
    return COLORS[h % COLORS.length];
}

// avatarLetter is the first letter or digit of a username, capitalized.
export function avatarLetter(username) {
    const m = /[a-z0-9]/i.exec(username || '');
    return m ? m[0].toUpperCase() : '?';
}

const SIZES = {
    sm: 'h-6 w-6 text-xs',
    md: 'h-9 w-9 text-sm',
    lg: 'h-18 w-18 text-3xl',
};

const DOTS = {
    sm: 'h-2 w-2 ring-1',
    md: 'h-3 w-3 ring-2',
    lg: 'h-4 w-4 ring-4',
};

// Avatar shows a member's picture. online, when given, adds a presence dot.
// A picture that fails to load gives way to the default.
export function Avatar({ member, size = 'md', online }) {
    const denID = useContext(DenContext);
    const [failed, setFailed] = useState('');
    const picture = denID && member?.avatar?.id && member.avatar.id !== failed ? member.avatar.id : '';
    return (
        <span class={`relative inline-flex shrink-0 ${SIZES[size]}`} aria-hidden="true">
            {picture ? (
                <img src={`/api/dens/${denID}/files/${picture}`} alt="" draggable={false} onError={() => setFailed(picture)}
                    class="h-full w-full select-none rounded-full bg-base-300 object-cover" />
            ) : (
                <span class={`flex h-full w-full select-none items-center justify-center rounded-full font-semibold text-white ${avatarColor(member?.id)}`}>
                    {avatarLetter(member?.username)}
                </span>
            )}
            {online !== undefined && (
                <span class={`absolute -bottom-0.5 -right-0.5 rounded-full ring-base-200 ${DOTS[size]} ${online ? 'bg-success' : 'bg-base-content/30'}`}></span>
            )}
        </span>
    );
}
