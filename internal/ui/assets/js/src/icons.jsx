// The page's drawn icons that more than one place shows, in the one style
// they all share: 16 units square and stroked at 1.5, so a channel's kind,
// a lock and the call's controls look alike wherever they sit (M8.3).

export const ICON = { viewBox: '0 0 16 16', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.5', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true' };

// HashIcon marks a text channel.
export function HashIcon({ size = 'h-4 w-4' }) {
    return (
        <svg class={`${size} shrink-0`} {...ICON}>
            <path d="M6.5 2.5 5 13.5M11 2.5 9.5 13.5M2.75 5.75h11M2.25 10.25h11" />
        </svg>
    );
}

// SpeakerIcon marks a voice channel, and the call's connection in its
// controls.
export function SpeakerIcon({ size = 'h-[18px] w-[18px]' }) {
    return (
        <svg class={`${size} shrink-0`} {...ICON}>
            <path d="M2.5 6v4h2.5l3.5 3V3L5 6z" />
            <path d="M11 5.75a3.25 3.25 0 0 1 0 4.5M12.75 3.75a6 6 0 0 1 0 8.5" />
        </svg>
    );
}

// LockIcon marks what not everyone sees: a staff-only channel, and a DM,
// which is end-to-end encrypted.
export function LockIcon({ size = 'h-3.5 w-3.5' }) {
    return (
        <svg class={`${size} shrink-0`} {...ICON}>
            <rect x="3" y="7" width="10" height="7" rx="1.5" />
            <path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" />
        </svg>
    );
}

// InfoIcon is an i in a circle, for a tip that says more than its label.
export function InfoIcon({ size = 'h-3.5 w-3.5' }) {
    return (
        <svg class={`${size} shrink-0`} {...ICON}>
            <circle cx="8" cy="8" r="6.5" />
            <path d="M8 7.25v4" />
            <circle cx="8" cy="4.75" r="0.4" fill="currentColor" />
        </svg>
    );
}
