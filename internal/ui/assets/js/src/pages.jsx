// Pages of sections (M13.3): the app's settings and a den's Manage, which
// on the phone layout take the screen, their sections a list, each opening
// as a page with ← back, as Android's own settings do, and Back walks out
// of a section, then out of the list. Wider windows keep them as a card
// over the page, their sections as tabs or a column beside.

import { useEffect, useRef } from 'preact/hooks';
import { BackIcon, OnwardIcon } from './icons.jsx';
import { nav } from './nav.js';

// useSectionPage gives a section's page an entry in the history while
// shown says it shows, so Back goes to the list with back. Its frame's
// own entry comes first, so a frame calls it after its useLayer.
export function useSectionPage(shown, back) {
    const ref = useRef(back);
    ref.current = back;
    useEffect(() => {
        if (!shown) return undefined;
        const id = nav().openLayer(() => ref.current());
        return () => nav().closeLayer(id);
    }, [shown]);
}

// PageHeader heads a page: its title, with ← where it goes back to a list,
// or ✕ where it closes.
export function PageHeader({ title, onBack, onClose, children }) {
    const button = 'btn btn-ghost btn-square relative touch-room';
    return (
        <div class="flex min-h-14 shrink-0 items-center gap-1 border-b border-base-300 px-2">
            {onBack && (
                <button type="button" class={button} aria-label="Back" data-back onClick={onBack}>
                    <BackIcon />
                </button>
            )}
            <h2 class={`min-w-0 flex-1 truncate text-lg font-semibold ${onBack ? '' : 'pl-2'}`}>{title}</h2>
            {children}
            {onClose && (
                <button type="button" class={button} aria-label="Close" onClick={onClose}>✕</button>
            )}
        </div>
    );
}

// SectionList lists the sections to open, in groups, each [heading,
// [[id, name, danger]...]], a heading of '' showing none, as rows a finger
// fits; onPick opens the one picked.
export function SectionList({ label, groups, onPick }) {
    return (
        <nav aria-label={label} class="flex min-h-0 flex-1 flex-col overflow-y-auto pb-4">
            {groups.filter(([, items]) => items.length).map(([heading, items]) => (
                <div key={heading || items[0][0]} class="flex flex-col">
                    {heading ? <h3 class="px-4 pb-1 pt-4 text-xs font-semibold uppercase text-base-content/60">{heading}</h3> : <div class="pt-3"></div>}
                    <ul class="flex flex-col">
                        {items.map(([id, name, danger]) => (
                            <li key={id}>
                                <button type="button" data-section={id} onClick={() => onPick(id)}
                                    class={`flex min-h-12 w-full items-center gap-3 px-4 text-left hover:bg-base-200 focus-visible:bg-base-200 focus-visible:outline-none active:bg-base-300 ${danger ? 'text-error' : ''}`}>
                                    <span class="min-w-0 flex-1">{name}</span>
                                    {!danger && <span class="text-base-content/50"><OnwardIcon /></span>}
                                </button>
                            </li>
                        ))}
                    </ul>
                </div>
            ))}
        </nav>
    );
}

// PLAIN_CARDS, on a page's panel, has the cards in it lose their own
// background and padding on the phone layout, so their text takes the
// screen's width but the panel's margin, and a line parts each from the
// last.
export const PLAIN_CARDS = 'max-md:[&>.card]:rounded-none max-md:[&>.card]:bg-transparent max-md:[&>.card>.card-body]:p-0 max-md:[&>.card+.card]:border-t max-md:[&>.card+.card]:border-base-300 max-md:[&>.card+.card]:pt-4';
