// The quick switcher: a box, and under it what switcherList lists, over
// the page, or as a sheet on the phone layout. ↑ and ↓ move through it,
// Enter opens what's marked, a voice channel's joining its call as a
// click on its row does, and a click or a tap opens a row. Ctrl+K opens
// it, and so does the magnifier atop a den's list and in Home's header,
// since a touch screen has no Ctrl. Every name in it is a den's, shown as
// text. Forward picks its place in the same box.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from '../core/api.js';
import { nav, useLayer, useNarrow } from '../core/nav.js';
import { useCoarse } from '../core/touch.js';
import { shownDen, switcherList } from '../core/switcher.js';
import { openCall } from '../voice/voice.js';
import { ErrorText, useBackdrop } from './components.jsx';
import { openSwitcher } from './dialogs.js';
import { HashIcon, MenuIcon, SearchIcon, SpeakerIcon } from './icons.jsx';
import { Handle, useDragDown } from './sheet.jsx';

const LABEL = 'Find a channel, DM or den';

// SwitcherButton is the magnifier that opens the switcher.
export function SwitcherButton({ class: extra = '' }) {
    const touch = useCoarse();
    return (
        <button type="button" class={`btn btn-ghost btn-sm btn-square relative touch-room ${extra}`} aria-label={LABEL} title={touch ? LABEL : `${LABEL} (Ctrl+K)`}
            data-switcher-button onClick={openSwitcher}>
            <SearchIcon />
        </button>
    );
}

// go opens what an entry names.
function go(e) {
    if (e.kind === 'den') nav().open(`/den/${e.den}`);
    else if (e.kind === 'voice') openCall(e.den, e.id, { den: e.denName, channel: e.name });
    else nav().den(e.den, e.id, 'chan');
}

function EntryIcon({ e }) {
    if (e.kind === 'text') return <HashIcon />;
    if (e.kind === 'voice') return <SpeakerIcon size="h-4 w-4" />;
    if (e.kind === 'dm') return <MenuIcon name="profile" />;
    if (e.kind === 'group') return <MenuIcon name="people" />;
    if (e.kind === 'notes') return <MenuIcon name="notes" />;
    return <span class="flex h-4 w-4 items-center justify-center rounded bg-base-300 text-[10px] font-semibold uppercase">{[...e.name][0] || '?'}</span>;
}

export function Switcher({ onClose }) {
    return (
        <PlacePicker label={LABEL} placeholder="Where to?" list={switcherList} data="switcher" onClose={onClose}
            empty={(q) => (q ? 'Nothing matches.' : 'Nothing unread. Type to find a channel, DM or den.')}
            onPick={(e) => {
                onClose();
                go(e);
            }} />
    );
}

// PlacePicker is the switcher's box over what list lists for what's typed,
// as switcherList does; empty says why it lists nothing, and onPick takes
// the row picked. busy is the key of the row being sent, which holds the
// box, and error why the last pick failed. data names the dialog for the
// checks.
export function PlacePicker({ label, placeholder, list: listed, empty, onPick, onClose, data, busy = '', error = '' }) {
    useLayer(onClose);
    const backdrop = useBackdrop(onClose);
    const narrow = useNarrow();
    const drag = useDragDown(onClose, (e) => narrow && !!e.target.closest('[data-sheet-top]'));
    const [query, setQuery] = useState('');
    const [dens, setDens] = useState(null);
    const [views, setViews] = useState(() => new Map());
    const [index, setIndex] = useState(0);
    const box = useRef(null);
    const list = useRef(null);
    const here = shownDen();
    useEffect(() => {
        const opener = document.activeElement;
        box.current?.focus();
        let live = true;
        const keep = (den, view) => live && setViews((m) => new Map(m).set(den, view));
        api.get('/api/dens').then((v) => {
            if (!live) return;
            setDens(v.dens);
            for (const d of v.dens) {
                if (d.den_id === here.den && here.view) keep(d.den_id, here.view);
                else api.get(`/api/dens/${d.den_id}/state`).then((view) => keep(d.den_id, view), () => keep(d.den_id, null));
            }
        }, () => {});
        return () => {
            live = false;
            if (opener?.isConnected) opener.focus({ preventScroll: true });
        };
    }, []);
    const entries = listed(dens || [], views, here.den, query);
    const at = Math.min(index, Math.max(0, entries.length - 1));
    useLayoutEffect(() => {
        list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' });
    }, [at, query]);
    function open(e) {
        if (!busy) onPick(e);
    }
    function keys(e) {
        const n = entries.length;
        if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && n) {
            e.preventDefault();
            setIndex((at + (e.key === 'ArrowDown' ? 1 : -1) + n) % n);
        } else if (e.key === 'Enter' && !e.isComposing) {
            e.preventDefault();
            if (entries[at]) open(entries[at]);
        } else if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            onClose();
        }
    }
    return (
        <div class="fixed inset-0 z-50 flex items-end justify-center bg-black/40 md:items-start md:p-4 md:pt-[12vh]" {...backdrop}>
            <div ref={drag.ref} role="dialog" aria-modal="true" aria-label={label} {...{ [`data-${data}`]: '' }} style={drag.style} {...drag.handlers}
                class="card flex max-h-[calc(100%-1.5rem)] w-full flex-col overflow-hidden bg-base-100 shadow-xl max-md:rounded-b-none md:max-h-[70vh] md:max-w-lg">
                {narrow && <div data-sheet-top class="touch-none"><Handle /></div>}
                <div class="p-3 max-md:pt-1">
                    <label class="input w-full">
                        <SearchIcon />
                        <input ref={box} type="search" role="combobox" aria-expanded="true" aria-controls="switcher-entries" aria-autocomplete="list"
                            aria-activedescendant={entries[at] ? `switch-${at}` : undefined} aria-label={label} placeholder={placeholder} value={query}
                            enterkeyhint="go" autocomplete="off" spellcheck={false} readOnly={!!busy}
                            onInput={(e) => {
                                setQuery(e.currentTarget.value);
                                setIndex(0);
                            }}
                            onKeyDown={keys} />
                    </label>
                </div>
                <ul ref={list} id="switcher-entries" role="listbox" aria-label="Places" class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-2 empty:hidden">
                    {entries.map((e, i) => (
                        <li key={e.key} id={`switch-${i}`} role="option" aria-selected={i === at} data-switch={e.key}
                            class={`flex min-h-9 cursor-pointer items-center gap-2 rounded px-2 pointer-coarse:min-h-11 ${i === at ? 'bg-base-300' : ''}`}
                            onMouseMove={() => i !== at && setIndex(i)}
                            onPointerDown={(ev) => ev.preventDefault()} onMouseDown={(ev) => ev.preventDefault()}
                            onClick={() => open(e)}>
                            <span class="text-base-content/50"><EntryIcon e={e} /></span>
                            <span class={`truncate ${e.unread ? 'font-semibold' : ''}`}>{e.name}</span>
                            {e.detail && <span class="truncate text-sm text-base-content/50">{e.detail}</span>}
                            <span class="ml-auto flex shrink-0 items-center gap-2">
                                {busy === e.key && <span class="loading loading-spinner loading-xs" aria-label="Sending"></span>}
                                {e.kind !== 'den' && e.den !== here.den && <span class="max-w-32 truncate text-xs text-base-content/50">{e.denName}</span>}
                                {e.mentions > 0 ? <span class="badge badge-error badge-xs">{e.mentions}</span>
                                    : e.unread && <span class="h-2 w-2 rounded-full bg-base-content" aria-label="unread"></span>}
                            </span>
                        </li>
                    ))}
                </ul>
                {dens && entries.length === 0 && (
                    <p class="cursor-default select-none px-4 pb-4 text-sm text-base-content/60">{empty(query.trim())}</p>
                )}
                {error && <div class="px-3 pb-3"><ErrorText message={error} /></div>}
            </div>
        </div>
    );
}
