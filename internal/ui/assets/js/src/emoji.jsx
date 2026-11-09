// Emoji on the page (M15.2): the picker beside the composer, which opens
// above it, and on the phone layout as a sheet from the screen's foot, as
// the page's menus are there; the list completing a name or a mention
// above the box; and Manage's Emoji section, where staff add the den's
// emoji, and rename and delete each.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { HoverRow } from './animations.js';
import { api } from './api.js';
import { Avatar } from './avatar.jsx';
import { ErrorText, Field, TextInput, useAction } from './components.jsx';
import { animatedImage } from './crop.jsx';
import { DenEmoji, EmojiContext } from './denemoji.jsx';
import {
    TONES, TONE_NAMES, cleanName, emojiList, inTone, loadEmoji, mostUsed, nameFromFile, searchDen, searchUnicode, setSkinTone, skinTone, validName,
} from './emoji.js';
import { onEvent } from './events.js';
import { upload, uploadAnimated } from './files.jsx';
import { Sheet } from './sheet.jsx';

// COLUMNS is how many emoji a row of the picker holds, and MOST_ROWS how
// many rows the most used take at most.
const COLUMNS = 9;
const MOST_ROWS = 4;

// A group's button shows one of its emoji.
const GROUP_MARKS = ['😀', '👋', '🐻', '🍔', '✈️', '⚽', '💡', '💕', '🏁'];

// pickOf is what a pick puts in the text: a Unicode emoji as itself, in the
// member's skin tone where it takes one, and the den's by name.
export function pickOf(item, tone) {
    return item.den ? `:${item.den.name}:` : inTone(item.unicode, tone);
}

// nameOfItem is what a tile's tip says.
function nameOfItem(item) {
    return item.den ? `:${item.den.name}:` : item.unicode.name;
}

// EmojiPicker is the picker: a search over names and keywords; the member's
// most used, up to four rows, then the den's emoji, by name, then
// Unicode's, by group, which the buttons at its top go to; and the skin
// tone, which the browser keeps, for those that take one. onPick hears the
// emoji picked, which onPick's caller puts in. As a sheet, its search and
// its buttons stay at the sheet's top while the emoji scroll. It opens
// above the composer's box, or where place, a fixed place in the window,
// says, beside a message to react to it (M15.3); label names it.
export function EmojiPicker({ emoji, onPick, onClose, sheet, place, label = 'Emoji' }) {
    // The list, where the page fetched it already, shows from the start.
    const [list, setList] = useState(emojiList);
    const [failed, setFailed] = useState('');
    const [query, setQuery] = useState('');
    const [tone, setTone] = useState(skinTone);
    const [tones, setTones] = useState(false);
    const [named, setNamed] = useState('');
    const search = useRef(null);
    const grid = useRef(null);
    useEffect(() => {
        let gone = false;
        loadEmoji().then((l) => !gone && setList(l), (e) => !gone && setFailed(e.message));
        // A finger's keyboard would cover the emoji, so only a mouse's
        // picker starts in its search.
        if (!sheet) search.current?.focus();
        return () => {
            gone = true;
        };
    }, []);
    const den = emoji?.on ? emoji.list : [];
    const denID = emoji?.denID || '';
    const sections = [];
    const q = query.trim();
    if (q) {
        sections.push({ key: 'found', label: 'Found', items: [...searchDen(q, den).map((e) => ({ den: e })), ...searchUnicode(q, list).map((e) => ({ unicode: e }))] });
    } else {
        // Scores go with the den's emoji they're of, not while emoji are off.
        const most = mostUsed(denID, emoji?.all || [], COLUMNS * MOST_ROWS, Date.now(), list)
            .filter((x) => !x.den || emoji?.on)
            .map((x) => (x.den ? { den: x.den } : { unicode: x.unicode }));
        if (most.length) sections.push({ key: 'most', label: 'Most used', items: most });
        if (den.length) sections.push({ key: 'den', label: "This den's", items: den.map((e) => ({ den: e })) });
        list?.groups.forEach((g, i) => sections.push({ key: `g${i}`, label: g, items: list.emoji.filter((e) => e.group === i).map((e) => ({ unicode: e })) }));
    }
    function pick(item, keep) {
        onPick(item, pickOf(item, tone));
        if (!keep) onClose();
    }
    // goTo scrolls a section to the top of what scrolls: the grid, or as a
    // sheet the sheet, below its top, which stays.
    function goTo(key) {
        const el = grid.current?.querySelector(`[data-section="${key}"]`);
        const scroller = sheet ? el?.closest('[data-sheet]') : grid.current;
        if (!el || !scroller) return;
        const cover = sheet ? scroller.querySelector('[data-sheet-top]')?.getBoundingClientRect().height || 0 : 0;
        scroller.scrollTop += el.getBoundingClientRect().top - scroller.getBoundingClientRect().top - cover;
    }
    function chooseTone(n) {
        setTone(n);
        setSkinTone(n);
        setTones(false);
    }
    const tile = `flex items-center justify-center rounded font-emoji hover:bg-base-200 focus-visible:bg-base-200 focus-visible:outline-none ${sheet ? 'h-11 text-[1.75rem]' : 'h-9 text-2xl'}`;
    const top = (
        <div class={`flex flex-col gap-2 ${sheet ? '' : 'px-3 pt-3'}`}>
            <div class="flex items-center gap-1">
                <input ref={search} type="search" class="input input-sm min-w-0 flex-1 pointer-coarse:input-md" placeholder="Search emoji" aria-label="Search emoji"
                    value={query} onInput={(e) => setQuery(e.currentTarget.value)}
                    onKeyDown={(e) => {
                        const first = sections[0]?.items[0];
                        if (e.key === 'Enter' && q && first) {
                            e.preventDefault();
                            pick(first, e.shiftKey);
                        }
                    }} />
                <span class="relative">
                    <button type="button" class="btn btn-ghost btn-sm btn-square font-emoji text-xl pointer-coarse:btn-md" aria-label={`Skin tone: ${TONE_NAMES[tone]}`}
                        title="Skin tone" aria-expanded={tones} data-skin-tone={tone} onClick={() => setTones(!tones)}>
                        {TONES[tone]}
                    </button>
                    {tones && (
                        <span role="group" aria-label="Skin tones" class="absolute right-0 top-full z-10 mt-1 flex rounded-box border border-base-300 bg-base-100 p-1 shadow">
                            {TONES.map((t, i) => (
                                <button key={t} type="button" class={`btn btn-ghost btn-sm btn-square font-emoji text-xl pointer-coarse:btn-md ${i === tone ? 'btn-active' : ''}`}
                                    aria-label={TONE_NAMES[i]} title={TONE_NAMES[i]} aria-pressed={i === tone} data-tone={i} onClick={() => chooseTone(i)}>
                                    {t}
                                </button>
                            ))}
                        </span>
                    )}
                </span>
            </div>
            {!q && list && (
                <div class="flex justify-between" role="group" aria-label="Go to">
                    {sections.map((s) => (
                        <button key={s.key} type="button" class="btn btn-ghost btn-xs btn-square font-emoji text-base pointer-coarse:btn-sm" aria-label={s.label} title={s.label}
                            onClick={() => goTo(s.key)}>
                            {s.key === 'most' ? '🕘' : s.key === 'den' ? <DenEmoji emoji={den[0]} size="h-4 w-4" /> : GROUP_MARKS[Number(s.key.slice(1))] || '•'}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
    const body = (
        <>
            <div ref={grid} class={sheet ? 'px-2 pb-2' : 'min-h-0 flex-1 overflow-y-auto px-2'} data-emoji-grid>
                {failed && <p role="alert" class="p-2 text-sm text-error">{failed}</p>}
                {!list && !failed && <div class="p-4 text-center"><span class="loading loading-spinner"></span></div>}
                {list && sections.length === 0 && <p class="p-2 text-sm text-base-content/70">No emoji matches.</p>}
                {sections.map((s) => (
                    <section key={s.key} data-section={s.key} aria-label={s.label}>
                        <h4 class={`cursor-default select-none bg-base-100 px-1 pb-1 pt-2 text-xs font-semibold text-base-content/60 ${sheet ? '' : 'sticky top-0 z-[1]'}`}>{s.label}</h4>
                        <div class="grid" style={{ gridTemplateColumns: `repeat(${COLUMNS}, minmax(0, 1fr))` }}>
                            {s.items.map((item) => (
                                <HoverRow key={item.den ? `d${item.den.id}` : item.unicode.emoji} as="button" type="button" class={tile}
                                    role={sheet ? 'menuitem' : undefined} aria-label={nameOfItem(item)} title={sheet ? undefined : nameOfItem(item)}
                                    data-emoji-tile={item.den ? `:${item.den.name}:` : item.unicode.emoji}
                                    onMouseEnter={() => setNamed(nameOfItem(item))} onFocus={() => setNamed(nameOfItem(item))}
                                    onClick={(e) => pick(item, e.shiftKey)}>
                                    {item.den ? <DenEmoji emoji={item.den} size={sheet ? 'h-8 w-8' : 'h-7 w-7'} /> : inTone(item.unicode, tone)}
                                </HoverRow>
                            ))}
                        </div>
                    </section>
                ))}
            </div>
            {!sheet && <p class="h-7 truncate border-t border-base-300 px-3 py-1 text-xs text-base-content/70" aria-live="polite">{named}</p>}
        </>
    );
    if (sheet) {
        return (
            <Sheet label={label} title={top} onClose={onClose}>
                {body}
            </Sheet>
        );
    }
    return (
        <div role="dialog" aria-label={label} data-emoji-picker style={place || undefined}
            class={`${place ? 'fixed z-40' : 'absolute bottom-full right-0 z-30 mb-2'} flex h-96 max-h-[60vh] w-[22rem] max-w-[calc(100vw-2rem)] flex-col gap-1 rounded-box border border-base-300 bg-base-100 shadow-xl`}>
            {top}
            {body}
        </div>
    );
}

// Completions lists what completing offers above the composer's box: the
// den's emoji that match, then Unicode's, or members, the one at index
// marked, which Enter or Tab puts in. A press keeps the box's focus, so a
// finger's keyboard stays up.
export function Completions({ items, index, tone, onPick, onIndex }) {
    const list = useRef(null);
    useLayoutEffect(() => {
        list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' });
    }, [index]);
    return (
        <ul ref={list} role="listbox" aria-label="Completions" data-completions
            class="absolute inset-x-0 bottom-full z-30 mb-1 max-h-72 overflow-y-auto rounded-box border border-base-300 bg-base-100 p-1 shadow-lg">
            {items.map((item, i) => (
                <HoverRow key={item.everyone ? 'everyone' : item.member ? item.member.id : item.den ? `d${item.den.id}` : item.unicode.emoji} as="li" role="option"
                    aria-selected={i === index}
                    class={`flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm pointer-coarse:min-h-11 ${i === index ? 'bg-base-200' : ''}`}
                    data-completion={item.everyone ? '@everyone' : item.member ? `@${item.member.username}` : item.den ? `:${item.den.name}:` : item.unicode.emoji}
                    onPointerDown={(e) => e.preventDefault()} onMouseDown={(e) => e.preventDefault()}
                    onMouseEnter={() => onIndex(i)} onClick={() => onPick(item)}>
                    {item.everyone ? (
                        <>
                            <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary/15 text-xs font-semibold text-primary" aria-hidden="true">@</span>
                            <span class="truncate font-medium">@everyone</span>
                            <span class="truncate text-base-content/60">Everyone who can see this channel</span>
                        </>
                    ) : item.member ? (
                        <>
                            <Avatar member={item.member} size="sm" />
                            <span class="truncate font-medium">{item.member.display_name}</span>
                            <span class="truncate text-base-content/60">@{item.member.username}</span>
                        </>
                    ) : item.den ? (
                        <>
                            <DenEmoji emoji={item.den} size="h-6 w-6" />
                            <span class="truncate">:{item.den.name}:</span>
                        </>
                    ) : (
                        <>
                            <span class="w-6 text-center font-emoji text-xl leading-none">{inTone(item.unicode, tone)}</span>
                            <span class="truncate">{item.unicode.name}</span>
                        </>
                    )}
                </HoverRow>
            ))}
        </ul>
    );
}

// MAX_SIDE is the longest side of an emoji's image, in pixels.
const MAX_SIDE = 128;

// stillEmoji draws an image into a PNG of an emoji's size: at most MAX_SIDE
// on its longer side, its shape kept, and never enlarged. Drawing it anew
// leaves its metadata behind, as for a picture.
async function stillEmoji(bitmap) {
    const scale = Math.min(1, MAX_SIDE / Math.max(bitmap.width, bitmap.height));
    const w = Math.max(1, Math.round(bitmap.width * scale));
    const h = Math.max(1, Math.round(bitmap.height * scale));
    const canvas = document.createElement('canvas');
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext('2d');
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(bitmap, 0, 0, w, h);
    return new Promise((resolve, reject) => canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("This image couldn't be drawn."))), 'image/png'));
}

/** prepareEmoji makes a file an emoji's upload: a still image drawn into a
    PNG, or an animated GIF or WebP made into a GIF by the local service,
    each at an emoji's size */
export async function prepareEmoji(denID, file) {
    let bitmap;
    try {
        bitmap = await createImageBitmap(file);
    } catch {
        throw new Error("Your browser can't read this image. Try a PNG, GIF, JPEG or WebP.");
    }
    try {
        if (animatedImage(new Uint8Array(await file.arrayBuffer()))) {
            return await uploadAnimated(denID, 'emoji', file, { x: 0, y: 0, w: bitmap.width, h: bitmap.height });
        }
        const png = await stillEmoji(bitmap);
        return await upload(denID, '', png, 'emoji.png').done;
    } finally {
        bitmap.close?.();
    }
}

// ManageEmoji is the den's Emoji section in Manage, for staff: how many
// emoji the den keeps of how many it allows, an emoji added by a file and
// a name, which the page suggests from the file's, and the den's emoji by
// name, each to rename or delete. owner says whether the member sets the
// count, in Limits.
export function ManageEmoji({ denID, owner, onLimits }) {
    const [view, setView] = useState(null);
    const act = useAction();
    const load = () => api.get(`/api/dens/${denID}/state`).then(setView);
    useEffect(() => {
        act.run(load);
        // Emoji others add, rename and delete show as they do.
        return onEvent((m) => {
            if (m.t === 'den' && m.d.den === denID && (m.d.reset || (m.d.events || []).some((e) => e.t.startsWith('emoji.') || e.t === 'den.updated'))) {
                load().catch(() => {});
            }
        });
    }, [denID]);
    if (!view) return <>{act.error ? <ErrorText message={act.error} /> : <span class="loading loading-spinner"></span>}</>;
    const limits = view.emoji_limits;
    const names = new Map((view.members || []).map((m) => [m.id, m.display_name]));
    const emoji = view.emoji || [];
    // The section shows its emoji whether or not they're on.
    const ctx = { denID, list: emoji, all: emoji, byName: new Map(emoji.map((e) => [e.name, e])), on: true, known: !!limits };
    return (
        <EmojiContext.Provider value={ctx}>
            <h3 class="text-lg font-semibold">Emoji</h3>
            <p class="text-sm text-base-content/70">The den's own emoji, typed as :name: or picked with the rest.</p>
            {!limits ? (
                <p class="text-sm">This den runs a version of Dens without emoji.</p>
            ) : (
                <>
                    {limits.emojis === 0 ? (
                        <p role="status" class="text-sm text-warning">
                            Emoji are off: messages show their names as text.
                            {owner && <> <button type="button" class="link link-primary" onClick={onLimits}>Turn them on in Limits</button></>}
                        </p>
                    ) : (
                        <p class="text-sm" data-emoji-count>{emoji.length} of {limits.emojis} emoji.</p>
                    )}
                    {limits.emojis > 0 && emoji.length < limits.emojis && <AddEmoji denID={denID} taken={ctx.byName} onAdded={() => load().catch(() => {})} />}
                </>
            )}
            {emoji.length > 0 && (
                <>
                    <p class="text-xs text-base-content/60">
                        Messages write an emoji by its name, so renaming or deleting one turns those it was in back into text, and a name given to
                        another emoji shows the new one in them.
                    </p>
                    <ul class="flex flex-col divide-y divide-base-300" aria-label="The den's emoji">
                        {emoji.map((e) => <EmojiRow key={e.id} denID={denID} emoji={e} by={names.get(e.added_by)} onChange={() => load().catch(() => {})} />)}
                    </ul>
                </>
            )}
        </EmojiContext.Provider>
    );
}

// AddEmoji adds an emoji: an image of any kind the browser draws, which
// this page makes an emoji's size, and its name, suggested from the file's.
function AddEmoji({ denID, taken, onAdded }) {
    const [file, setFile] = useState(null);
    const [name, setName] = useState('');
    const [stage, setStage] = useState('');
    const [picker, setPicker] = useState(0);
    const add = useAction();
    const clean = cleanName(name);
    function submit(e) {
        e.preventDefault();
        if (!validName(clean)) return add.setError("A name is 2 to 32 letters, digits and _.");
        if (taken.has(clean)) return add.setError('Another emoji has that name.');
        add.run(async () => {
            try {
                setStage('Preparing the emoji…');
                const up = await prepareEmoji(denID, file);
                setStage('Adding it…');
                await api.post(`/api/dens/${denID}/emoji`, { name: clean, upload: up.id });
            } finally {
                setStage('');
            }
            setFile(null);
            setName('');
            setPicker((n) => n + 1);
            onAdded();
        });
    }
    return (
        <form class="flex flex-col gap-2 rounded-box bg-base-200 p-3" onSubmit={submit} data-add-emoji>
            <p class="font-medium">Add an emoji</p>
            <input key={picker} type="file" class="file-input w-full" accept="image/*" aria-label="The emoji's image"
                onChange={(e) => {
                    const f = e.currentTarget.files?.[0] || null;
                    setFile(f);
                    if (f && !name.trim()) setName(nameFromFile(f.name));
                    add.setError('');
                }} />
            <Field label="Name" hint="Letters, digits and _, written as :name:">
                <TextInput value={name} onInput={setName} maxlength="34" required placeholder="party_parrot" aria-label="The emoji's name"
                    autocapitalize="none" autocorrect="off" spellcheck={false} />
            </Field>
            <div class="flex flex-wrap items-center gap-2">
                <button type="submit" class="btn btn-primary btn-sm" disabled={!file || add.busy}>
                    {add.busy && <span class="loading loading-spinner loading-sm"></span>}
                    Add
                </button>
                {stage && <span role="status" class="text-sm text-base-content/70">{stage}</span>}
            </div>
            <ErrorText message={add.error} />
        </form>
    );
}

// EmojiRow is one of the den's emoji: its image, which plays under the
// pointer where it moves, its name and who added it, and its renaming and
// deleting, which asks first.
function EmojiRow({ denID, emoji, by, onChange }) {
    const [renaming, setRenaming] = useState(false);
    const [name, setName] = useState(emoji.name);
    const [deleting, setDeleting] = useState(false);
    const act = useAction();
    function rename(e) {
        e.preventDefault();
        const clean = cleanName(name);
        if (!validName(clean)) return act.setError("A name is 2 to 32 letters, digits and _.");
        act.run(async () => {
            await api.patch(`/api/dens/${denID}/emoji/${emoji.id}`, { name: clean });
            setRenaming(false);
            onChange();
        });
    }
    return (
        <HoverRow as="li" class="flex flex-col gap-1 py-1.5" data-manage-emoji={emoji.name}>
            <div class="flex items-center gap-2 text-sm">
                <DenEmoji emoji={emoji} size="h-8 w-8" />
                {renaming ? (
                    <form class="flex min-w-0 flex-1 items-center gap-1" onSubmit={rename}>
                        <input class="input input-sm min-w-0 flex-1" value={name} maxlength="34" required aria-label={`New name for :${emoji.name}:`}
                            autocapitalize="none" autocorrect="off" spellcheck={false} onInput={(e) => setName(e.currentTarget.value)} />
                        <button type="submit" class="btn btn-primary btn-sm" disabled={act.busy}>Save</button>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => {
                            setRenaming(false);
                            setName(emoji.name);
                            act.setError('');
                        }}>Cancel</button>
                    </form>
                ) : (
                    <>
                        <span class="min-w-0 flex-1">
                            <span class="block truncate font-medium">:{emoji.name}:</span>
                            <span class="block truncate text-xs text-base-content/60">
                                {emoji.image.animated ? 'Moves' : 'Still'}{by ? ` · added by ${by}` : ''}
                            </span>
                        </span>
                        {deleting ? (
                            <>
                                <button type="button" class="btn btn-error btn-sm" disabled={act.busy} onClick={() => act.run(async () => {
                                    await api.del(`/api/dens/${denID}/emoji/${emoji.id}`);
                                    onChange();
                                })}>Delete</button>
                                <button type="button" class="btn btn-ghost btn-sm" onClick={() => {
                                    setDeleting(false);
                                    act.setError('');
                                }}>Keep</button>
                            </>
                        ) : (
                            <>
                                <button type="button" class="btn btn-ghost btn-sm" onClick={() => {
                                    setRenaming(true);
                                    act.setError('');
                                }}>Rename</button>
                                <button type="button" class="btn btn-ghost btn-sm text-error" onClick={() => {
                                    setDeleting(true);
                                    act.setError('');
                                }}>Delete</button>
                            </>
                        )}
                    </>
                )}
            </div>
            <ErrorText message={act.error} />
        </HoverRow>
    );
}
