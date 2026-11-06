// Notes to self (M17.3): the member's own conversation in each den, which
// only their devices read. The Direct messages tab keeps their row at its
// top, and the member's own profile card opens them too; nothing in them
// waits to be read or notifies, since everything in them is the member's,
// and they don't close. What they are lives in private.js beside the DMs'.

import { HoverRow } from './animations.js';

// NotesIcon marks the member's notes: a page with lines on it, round as a
// DM's picture is, so the rows line up.
export function NotesIcon({ size = 'h-6 w-6' }) {
    return (
        <span class={`flex shrink-0 items-center justify-center rounded-full bg-base-300 text-base-content/70 ${size}`} aria-hidden="true">
            <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M3.5 2h6l3 3v9h-9z" />
                <path d="M9.5 2v3h3M6 8.5h4M6 11h4" />
            </svg>
        </span>
    );
}

// NotesRow is the member's notes at the top of the DM list: open says
// they're what the channel pane shows.
export function NotesRow({ open, onOpen }) {
    return (
        <HoverRow as="li" class={`relative flex min-h-8 items-center rounded pointer-coarse:min-h-11 ${open ? 'bg-base-300' : 'hover:bg-base-300/60'}`}
            data-notes-row>
            <button type="button" class="flex min-w-0 flex-1 items-center gap-2 self-stretch rounded px-2 text-left after:absolute after:inset-0 after:rounded"
                onClick={onOpen}>
                <NotesIcon />
                <span class={`truncate ${open ? '' : 'text-base-content/85'}`}>Notes</span>
            </button>
        </HoverRow>
    );
}
