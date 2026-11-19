// Pages of sections (M13.3): the app's settings and a den's Manage, which
// on the phone layout take the screen, their sections a list, each opening
// as a page with ← back, as Android's own settings do, and Back walks out
// of a section, then out of the list. Wider windows keep them as a card
// over the page, their sections in a column beside. Both are one frame
// (M22.3), SectionsFrame.

import { Fragment } from 'preact';
import { useEffect, useRef } from 'preact/hooks';
import { buttonClass, useBackdrop } from './components.jsx';
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
    const button = `${buttonClass('quiet', 'md')} btn-square relative touch-room`;
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

/** SectionsFrame is the settings' and Manage's one frame (M22.3), named
    label for screen readers. On a wide window it's a card over the page,
    headed by title, its sections listed down its left under each group's
    heading, a danger one, as Leave den, at the column's foot, and the
    section at at beside them, bar under it. On the phone layout it takes
    the screen: the list of sections as rows, under title, or a section's
    page under its own name, with ← to the list, onList. groups are
    [heading, [[id, name, danger]...]], a heading of '' showing none. The
    section's groups are cards in both, which a phone shows plain. A full
    section, Appearance, takes the window at any width, its list folded to
    ← back, onList, and lays out what it holds itself. */
export function SectionsFrame({ label, title, groups, at, narrow, full = false, onPick, onList, onClose, bar, children }) {
    const backdrop = useBackdrop(onClose);
    const all = groups.flatMap(([, items]) => items);
    const panel = full ? children : (
        <div data-panel class={`flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4 ${narrow ? `pb-[max(1rem,env(safe-area-inset-bottom))] ${PLAIN_CARDS}` : ''}`}>
            {children}
        </div>
    );
    if (narrow || (full && at)) {
        return (
            <div role="dialog" aria-modal="true" aria-label={label} class="fixed inset-0 z-50 flex flex-col bg-base-100">
                {at ? (
                    <>
                        <PageHeader title={all.find(([id]) => id === at)?.[1] || title} onBack={onList} onClose={narrow ? undefined : onClose} />
                        {panel}
                    </>
                ) : (
                    <>
                        <PageHeader title={title} onClose={onClose} />
                        <SectionList label="Sections" groups={groups} onPick={onPick} />
                    </>
                )}
                {bar}
            </div>
        );
    }
    const item = ([id, name, danger]) => (
        <button key={id} type="button" aria-current={at === id ? 'page' : undefined} data-section={id} onClick={() => onPick(id)}
            class={`${buttonClass(danger ? 'danger' : 'quiet')} shrink-0 justify-start font-normal ${danger ? 'mt-auto' : ''} ${at === id ? 'btn-active' : ''}`}>
            {name}
        </button>
    );
    const shown = groups.filter(([, items]) => items.length);
    return (
        <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" {...backdrop}>
            <div role="dialog" aria-modal="true" aria-label={label} class="card flex h-full max-h-[48rem] w-full max-w-3xl flex-col bg-base-100 shadow-xl">
                <div class="flex items-center gap-2 border-b border-base-300 px-4 py-3">
                    <h2 class="card-title min-w-0 flex-1 truncate">{title}</h2>
                    <button type="button" class={`${buttonClass('quiet')} btn-square`} onClick={onClose} aria-label="Close">✕</button>
                </div>
                <div class="flex min-h-0 flex-1 flex-row">
                    <nav aria-label="Sections" class="flex w-52 shrink-0 flex-col gap-1 overflow-y-auto border-r border-base-300 p-2">
                        {shown.map(([heading, items]) => (
                            <Fragment key={heading || items[0][0]}>
                                {heading && <p class="truncate px-3 pt-2 text-xs font-semibold uppercase text-base-content/60">{heading}</p>}
                                {items.filter(([, , danger]) => !danger).map(item)}
                            </Fragment>
                        ))}
                        {all.filter(([, , danger]) => danger).map(item)}
                    </nav>
                    <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                        {panel}
                        {bar}
                    </div>
                </div>
            </div>
        </div>
    );
}
