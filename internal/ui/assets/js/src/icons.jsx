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

// CogIcon is an eight-toothed cog, for settings: the app's, the call's
// voice settings, and the soundboard's sounds in Manage (M14.3).
export function CogIcon({ size = 'h-[18px] w-[18px]' }) {
    return (
        <svg class={size} {...ICON} stroke-width="1.3">
            <path d="M13.06 6.43 L14.90 6.82 L14.90 9.18 L13.06 9.57 L12.69 10.47 L13.72 12.04 L12.04 13.72 L10.47 12.69 L9.57 13.06 L9.18 14.90 L6.82 14.90 L6.43 13.06 L5.53 12.69 L3.96 13.72 L2.28 12.04 L3.31 10.47 L2.94 9.57 L1.10 9.18 L1.10 6.82 L2.94 6.43 L3.31 5.53 L2.28 3.96 L3.96 2.28 L5.53 3.31 L6.43 2.94 L6.82 1.10 L9.18 1.10 L9.57 2.94 L10.47 3.31 L12.04 2.28 L13.72 3.96 L12.69 5.53Z" />
            <circle cx="8" cy="8" r="2.2" />
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

// BellIcon is notifications (M12.3): a den's, a channel's or a DM's bell,
// struck through where the member muted them.
export function BellIcon({ off, size = 'h-4 w-4' }) {
    return (
        <svg class={`${size} shrink-0`} {...ICON}>
            <path d="M4 11.5V7a4 4 0 0 1 8 0v4.5l1 1H3z" />
            <path d="M6.75 14a1.5 1.5 0 0 0 2.5 0" />
            {off && <path d="M2.5 2.5l11 11" />}
        </svg>
    );
}

// BackIcon is ←, a page's way back to the one it opened from (M13.3).
export function BackIcon({ size = 'h-5 w-5' }) {
    return (
        <svg class={`${size} shrink-0`} {...ICON}>
            <path d="M13 8H3M7.5 3.5 3 8l4.5 4.5" />
        </svg>
    );
}

// OnwardIcon is ›, where a row opens a page of its own (M13.3).
export function OnwardIcon({ size = 'h-4 w-4' }) {
    return (
        <svg class={`${size} shrink-0`} {...ICON}>
            <path d="M6 3.5 10.5 8 6 12.5" />
        </svg>
    );
}

// PlayIcon is ▶, filled as a player's is: a video's, and a sound's in the
// settings (M14.2).
export function PlayIcon({ size = 'h-3.5 w-3.5' }) {
    return (
        <svg viewBox="0 0 16 16" class={`${size} shrink-0`} fill="currentColor" aria-hidden="true">
            <path d="M4.5 2.8v10.4L13 8z" />
        </svg>
    );
}

// SoundboardIcon is the soundboard (M14.3): a sampler's pads, one lit.
export function SoundboardIcon({ size = 'h-[18px] w-[18px]' }) {
    return (
        <svg class={`${size} shrink-0`} {...ICON}>
            <rect x="2" y="2" width="5" height="5" rx="1.25" />
            <rect x="9" y="2" width="5" height="5" rx="1.25" />
            <rect x="2" y="9" width="5" height="5" rx="1.25" />
            <rect x="9" y="9" width="5" height="5" rx="1.25" fill="currentColor" />
        </svg>
    );
}

// SmileyIcon is a face, smiling, for emoji (M15.2): the composer's picker.
export function SmileyIcon({ size = 'h-5 w-5' }) {
    return (
        <svg class={`${size} shrink-0`} {...ICON}>
            <circle cx="8" cy="8" r="6.5" />
            <path d="M5.25 9.6a3.25 3.25 0 0 0 5.5 0" />
            <circle cx="5.9" cy="6.3" r="0.5" fill="currentColor" stroke="none" />
            <circle cx="10.1" cy="6.3" r="0.5" fill="currentColor" stroke="none" />
        </svg>
    );
}
