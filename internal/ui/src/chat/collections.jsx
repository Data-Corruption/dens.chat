// Collections: channel-scoped shared collections and den-wide personal
// collections. CollectionsRow opens the member's personal collections from the
// channel list; CollectionsPane shows either a text channel's shared
// collections or the member's personal collections as tabs; SavePicker lets
// the member bookmark any message into writable shared or personal collections
// (creating "Saved" lazily on first bookmark or creating a named collection
// inline).

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from '../core/api.js';
import { HoverRow } from '../appearance/animations.js';
import { onEvent } from '../core/events.js';
import { compareIds } from '../core/ids.js';
import { Avatar } from '../ui/avatar.jsx';
import { Actions, Button, Choices, ErrorText, Field, SubmitButton, TextInput, buttonClass, dayTime, useAction } from '../ui/components.jsx';
import { BackIcon, BookmarkIcon, HashIcon, LockIcon, MenuIcon } from '../ui/icons.jsx';
import { Name } from '../appearance/names.jsx';
import { Dialog } from '../manage.jsx';
import { isStaff } from '../people.jsx';
import { LOCKED, partnerOf } from '../private/private.js';
import { Sheet } from '../ui/sheet.jsx';
import { Attachments, Viewer, attachmentKind, placeOf } from './files.jsx';
import { Markdown } from './markdown.jsx';
import { ForwardedLabel } from './msglinks.jsx';
import { Videos } from './youtube.jsx';
import { Games } from './steam.jsx';
import { EventCards } from './denevents.jsx';
import {
    MAX_COLLECTIONS, MAX_COLLECTION_NAME, canRemoveCollectionItem, channelCollections,
    personalCollections, pickerSections,
} from './collections.js';

// wanted tracks a collection requested by the quick switcher so the pane opens
// straight to that tab.
let wanted = null;
let asked = 0;
const watchers = new Set();

export function showCollection(denID, collectionID) {
    wanted = { denID, collectionID, n: ++asked };
    watchers.forEach((fn) => fn());
}

function useWanted(denID) {
    const [focus, setFocus] = useState(null);
    useEffect(() => {
        const take = () => {
            if (wanted?.denID !== denID) return;
            setFocus(wanted);
            wanted = null;
        };
        watchers.add(take);
        take();
        return () => watchers.delete(take);
    }, [denID]);
    return focus;
}

// CollectionsRow sits under Events in the channel list and opens the member's
// personal collections for this den.
export function CollectionsRow({ open, onOpen }) {
    return (
        <HoverRow as="li" class={`relative flex min-h-8 select-none items-center gap-1 rounded pr-1 pointer-coarse:min-h-11 ${open ? 'bg-base-300' : 'hover:bg-base-300/60'}`}
            data-collections-row>
            <button type="button" class="flex min-w-0 flex-1 items-center gap-2 self-stretch rounded px-2 text-left after:absolute after:inset-0 after:rounded"
                aria-label="Collections" onClick={onOpen}>
                <span class="text-base-content/50"><BookmarkIcon /></span>
                <span class={`truncate ${open ? '' : 'text-base-content/85'}`}>Collections</span>
            </button>
        </HoverRow>
    );
}

// CollectionsPane renders either a text channel's shared collections (when
// channel is set) or the member's personal collections across the den.
export function CollectionsPane({ denID, view, channel = null, members, onList, onBack, onProfile, onJump }) {
    const shared = !!channel;
    const staff = isStaff(view.role);
    const gone = view.state === 'removed' || view.state === 'revoked';
    const canManage = !gone && (shared ? staff : true);
    const cols = shared ? channelCollections(view.collections, channel.id) : personalCollections(view.collections);
    const focus = useWanted(denID);
    const [selectedID, setSelectedID] = useState(() => cols[0]?.id || '');
    const [dialog, setDialog] = useState(null);
    const [items, setItems] = useState(null);
    const [loadError, setLoadError] = useState('');
    const [reloadTick, setReloadTick] = useState(0);
    const [viewing, setViewing] = useState(null);
    const removeAct = useAction();

    useEffect(() => {
        if (focus?.collectionID && cols.some((c) => c.id === focus.collectionID)) {
            setSelectedID(focus.collectionID);
        }
    }, [focus, cols.map((c) => c.id).join(',')]);

    const active = cols.find((c) => c.id === selectedID) || cols[0] || null;
    const activeID = active?.id || '';
    const activeCount = active?.count ?? -1;

    useEffect(() => {
        if (!activeID) {
            setItems([]);
            setLoadError('');
            return undefined;
        }
        let live = true;
        setLoadError('');
        api.get(`/api/dens/${denID}/collections/${activeID}/items`).then(
            (res) => {
                if (live) setItems(res.items || []);
            },
            (e) => {
                if (live) {
                    setItems([]);
                    setLoadError(e.message);
                }
            },
        );
        return () => {
            live = false;
        };
    }, [denID, activeID, activeCount, reloadTick]);

    // Keep open collection items in sync with live message/collection events.
    useEffect(() => {
        if (!activeID) return undefined;
        return onEvent((msg) => {
            if (msg.t !== 'den' || msg.d.den !== denID) return;
            if (msg.d.reset) return setReloadTick((n) => n + 1);
            for (const e of msg.d.events || []) {
                if ((e.t === 'collection.item_added' || e.t === 'collection.item_removed') && e.d?.collection_id === activeID) {
                    setReloadTick((n) => n + 1);
                } else if (e.t === 'message.deleted' && e.d?.id) {
                    setItems((cur) => (cur ? cur.filter((it) => it.message.id !== e.d.id) : cur));
                } else if (e.t === 'message.updated' && e.d?.id) {
                    setItems((cur) => (cur ? cur.map((it) => (it.message.id === e.d.id && !(it.message.revision > e.d.revision) ? { ...it, message: e.d } : it)) : cur));
                } else if (e.t === 'messages.expired' && e.d?.through) {
                    setItems((cur) => (cur ? cur.filter((it) => compareIds(it.message.id, e.d.through) > 0) : cur));
                }
            }
        });
    }, [denID, activeID]);

    const channelsById = new Map((view.channels || []).map((c) => [c.id, c]));

    function removeItem(messageID) {
        if (!active) return;
        removeAct.run(async () => {
            await api.del(`/api/dens/${denID}/collections/${active.id}/items/${messageID}`);
            setItems((cur) => (cur ? cur.filter((it) => it.message.id !== messageID) : cur));
        });
    }

    function openViewer(m, file, covered = new Set()) {
        const files = (m.attachments || []).filter((f) => attachmentKind(f) === 'image' && !covered.has(f.id));
        const index = files.findIndex((f) => f.id === file.id);
        if (index >= 0) setViewing({ at: placeOf(m), files, index });
    }

    return (
        <>
            <header class="flex min-h-12 items-center gap-2 border-b border-base-300 px-3 py-2">
                {shared ? (
                    <button type="button" class="btn btn-ghost btn-sm gap-1 px-2" data-collections-back onClick={onBack}>
                        <BackIcon size="h-4 w-4" />
                        <span class="truncate">#{channel.name}</span>
                    </button>
                ) : (
                    <button type="button" class="btn btn-ghost btn-sm btn-square md:hidden" aria-label="Channels" onClick={onList}>
                        <svg viewBox="0 0 16 16" class="h-5 w-5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true">
                            <path d="M2.5 4h11M2.5 8h11M2.5 12h11" />
                        </svg>
                    </button>
                )}
                <span class="text-base-content/50"><BookmarkIcon size="h-5 w-5" /></span>
                <h2 class="min-w-0 flex-1 truncate font-semibold" data-collections-header>
                    Collections
                </h2>
                {canManage && cols.length < MAX_COLLECTIONS && (
                    <Button kind="primary" data-new-collection onClick={() => setDialog({ kind: 'create' })}>+ Collection</Button>
                )}
            </header>

            {cols.length > 0 && (
                <div class="flex items-center gap-2 border-b border-base-300 px-3">
                    <div role="tablist" aria-label="Collections" class="tabs tabs-border min-w-0 flex-1 flex-nowrap overflow-x-auto" data-collections-tabs>
                        {cols.map((c) => {
                            const isSelected = active?.id === c.id;
                            return (
                                <button key={c.id} type="button" role="tab" aria-selected={isSelected} data-collection-tab={c.id}
                                    class={`tab shrink-0 gap-1.5 whitespace-nowrap ${isSelected ? 'tab-active' : ''}`}
                                    onClick={() => setSelectedID(c.id)}>
                                    <span>{c.name}</span>
                                    <span class="badge badge-ghost badge-xs tabular-nums" data-collection-count={c.id}>{c.count}</span>
                                </button>
                            );
                        })}
                    </div>
                </div>
            )}

            {active && (shared || canManage) && (
                <div class="flex flex-wrap items-center gap-2 border-b border-base-300/60 bg-base-200/40 px-4 py-1.5 text-xs text-base-content/70">
                    {shared ? (
                        <span data-collection-who-badge>{active.who_adds === 'everyone' ? 'Everyone can add messages' : 'Staff only can add messages'}</span>
                    ) : (
                        <span>Personal collection</span>
                    )}
                    {canManage && (
                        <span class="ml-auto flex items-center gap-1">
                            <Button kind="quiet" size="xs" data-edit-collection onClick={() => setDialog({ kind: 'edit', col: active })}>
                                {shared ? 'Edit' : 'Rename'}
                            </Button>
                            <Button kind="danger" size="xs" data-delete-collection onClick={() => setDialog({ kind: 'delete', col: active })}>
                                Delete
                            </Button>
                        </span>
                    )}
                </div>
            )}

            <div class="min-h-0 flex-1 overflow-y-auto" data-collections-body>
                <div class="flex max-w-[1400px] flex-col gap-3 p-4">
                    <ErrorText message={loadError || removeAct.error} />
                    {cols.length === 0 ? (
                        <p class="p-3 text-sm text-base-content/70" data-collections-empty>
                            {shared
                                ? (staff ? `No collections in #${channel.name} yet. Create one to gather important messages here.` : `No collections in #${channel.name} yet.`)
                                : 'No personal collections yet. Bookmark any message to save it here, or create a collection.'}
                        </p>
                    ) : items === null ? (
                        <div class="p-4"><span class="loading loading-spinner loading-sm"></span></div>
                    ) : items.length === 0 ? (
                        <p class="p-3 text-sm text-base-content/70" data-collection-items-empty>No messages in this collection yet.</p>
                    ) : (
                        items.map((it) => (
                            <SavedItem key={it.message.id} denID={denID} item={it} col={active} view={view} members={members}
                                channelsById={channelsById} canRemove={!gone && canRemoveCollectionItem(active, it, view.me.id, staff)}
                                removing={removeAct.busy} onRemove={() => removeItem(it.message.id)}
                                onProfile={onProfile} onJump={onJump} onView={openViewer} />
                        ))
                    )}
                </div>
            </div>

            {viewing && (
                <Viewer denID={denID} at={viewing.at} files={viewing.files} index={viewing.index} onClose={() => setViewing(null)}
                    onIndex={(index) => setViewing((v) => v && { ...v, index })} />
            )}
            {dialog?.kind === 'create' && (
                <CollectionDialog denID={denID} channel={channel} onClose={(created) => {
                    setDialog(null);
                    if (created?.id) setSelectedID(created.id);
                }} />
            )}
            {dialog?.kind === 'edit' && (
                <CollectionDialog denID={denID} channel={channel} col={dialog.col} onClose={() => setDialog(null)} />
            )}
            {dialog?.kind === 'delete' && (
                <DeleteCollectionDialog denID={denID} col={dialog.col} onClose={(deleted) => {
                    setDialog(null);
                    if (deleted && dialog.col.id === active?.id) {
                        const next = cols.find((c) => c.id !== dialog.col.id);
                        setSelectedID(next?.id || '');
                    }
                }} />
            )}
        </>
    );
}

function sourceLabel(ch, view, members) {
    if (!ch) return null;
    if (ch.kind === 'text') return { kind: 'text', label: `#${ch.name}` };
    if (ch.kind === 'notes') return { kind: 'notes', label: 'Notes' };
    if (ch.kind === 'dm') {
        const other = members.get(partnerOf(ch, view.me.id));
        return { kind: 'dm', label: other ? `DM with ${other.display_name}` : 'Direct message' };
    }
    return null;
}

function SavedItem({ denID, item, col, view, members, channelsById, canRemove, removing, onRemove, onProfile, onJump, onView }) {
    const m = item.message;
    const author = members.get(m.author_id);
    const adder = members.get(item.added_by);
    const src = !col.channel_id ? sourceLabel(channelsById.get(m.channel_id), view, members) : null;
    return (
        <article class="rounded-box border border-base-300 bg-base-100 p-3 shadow-xs" data-collection-item={m.id}>
            <div class="flex items-start gap-3">
                <button type="button" class="mt-0.5 shrink-0 rounded-full" onClick={(e) => author && onProfile(author, e.currentTarget)}
                    aria-label={`${author ? author.display_name : 'Unknown member'}'s profile`}>
                    <Avatar member={author} size="md" />
                </button>
                <div class="min-w-0 flex-1">
                    <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                        <button type="button" class="font-semibold hover:underline" onClick={(e) => author && onProfile(author, e.currentTarget)}>
                            <Name member={author} />
                        </button>
                        <span class="text-xs text-base-content/50">{dayTime(m.created_at)}</span>
                        {src && (
                            <span class="inline-flex items-center gap-1 rounded bg-base-200 px-1.5 py-0.5 text-xs text-base-content/70" data-item-source>
                                {src.kind === 'text' ? <HashIcon size="h-3 w-3" /> : <LockIcon size="h-3 w-3" />}
                                <span class="truncate">{src.kind === 'text' ? src.label.slice(1) : src.label}</span>
                            </span>
                        )}
                    </div>
                    {m.forwarded && !m.locked && (
                        <p class="mt-0.5 flex flex-wrap cursor-default select-none items-center gap-1 text-xs italic text-base-content/60" data-forwarded>
                            <MenuIcon name="forward" size="h-3 w-3" />
                            <ForwardedLabel source={m.forward_source} />
                        </p>
                    )}
                    {m.locked ? (
                        <p class="mt-1 text-sm italic text-base-content/60">{LOCKED[m.locked] || LOCKED.broken}</p>
                    ) : (
                        <>
                            {m.text?.trim() && (
                                <div class="mt-1 wrap-break-word">
                                    <Markdown text={m.text} me={view.me} everyone={m.everyone} />
                                    {m.edited_at ? <span class="text-xs text-base-content/50"> (edited)</span> : null}
                                </div>
                            )}
                            {m.attachments?.length > 0 && (
                                <Attachments denID={denID} at={placeOf(m)} files={m.attachments} onOpen={(f, covered) => onView(m, f, covered)} />
                            )}
                            {m.text && <Videos text={m.text} />}
                            {m.text && <Games text={m.text} />}
                            {m.text && <EventCards text={m.text} />}
                        </>
                    )}
                    <div class="mt-2 flex flex-wrap items-center gap-2 border-t border-base-300/60 pt-2 text-xs text-base-content/60">
                        <span>
                            {col.channel_id
                                ? `Added by ${adder ? adder.display_name : 'Unknown member'} · ${dayTime(item.added_at)}`
                                : `Saved ${dayTime(item.added_at)}`}
                        </span>
                        <span class="flex items-center gap-1">
                            <Button kind="quiet" size="xs" data-jump-message={m.id} onClick={() => onJump(m.channel_id, m.id)}>
                                Jump to message
                            </Button>
                            {canRemove && (
                                <Button kind="danger" size="xs" disabled={removing} data-remove-item={m.id} onClick={onRemove}>
                                    Remove
                                </Button>
                            )}
                        </span>
                    </div>
                </div>
            </div>
        </article>
    );
}

// CollectionDialog creates or edits a shared or personal collection.
function CollectionDialog({ denID, channel, col = null, onClose }) {
    const shared = !!channel || !!col?.channel_id;
    const [name, setName] = useState(col?.name || '');
    const [whoAdds, setWhoAdds] = useState(col?.who_adds || 'staff');
    const act = useAction();

    function submit(e) {
        e.preventDefault();
        const trimmed = name.trim();
        if (!trimmed) return;
        act.run(async () => {
            if (col) {
                const body = { name: trimmed };
                if (shared) body.who_adds = whoAdds;
                const updated = await api.patch(`/api/dens/${denID}/collections/${col.id}`, body);
                onClose(updated);
            } else if (shared) {
                const created = await api.post(`/api/dens/${denID}/channels/${channel.id}/collections`, { name: trimmed, who_adds: whoAdds });
                onClose(created);
            } else {
                const created = await api.post(`/api/dens/${denID}/collections`, { name: trimmed });
                onClose(created);
            }
        });
    }

    const title = col ? (shared ? 'Edit collection' : 'Rename collection') : (shared ? `New collection in #${channel.name}` : 'New personal collection');
    return (
        <Dialog title={title} onClose={() => onClose(null)}>
            <form class="flex flex-col gap-3" onSubmit={submit}>
                <Field label="Name" id="collection-name">
                    <TextInput id="collection-name" value={name} onInput={setName} maxLength={MAX_COLLECTION_NAME}
                        placeholder={shared ? 'Guides' : 'Saved'} required autoFocus data-collection-name />
                </Field>
                {shared && (
                    <Choices label="Who can add messages" name="who_adds" choice={whoAdds} onPick={setWhoAdds}
                        choices={[['staff', 'Staff only'], ['everyone', 'Everyone']]}
                        tip="Staff only lets moderators and the owner add or remove messages. Everyone lets anyone who sees this channel add messages and remove their own." />
                )}
                <ErrorText message={act.error} />
                <Actions>
                    <SubmitButton busy={act.busy} disabled={!name.trim()} data-collection-submit>{col ? 'Save' : 'Create'}</SubmitButton>
                    <Button kind="quiet" onClick={() => onClose(null)}>Cancel</Button>
                </Actions>
            </form>
        </Dialog>
    );
}

function DeleteCollectionDialog({ denID, col, onClose }) {
    const act = useAction();
    function confirm() {
        act.run(async () => {
            await api.del(`/api/dens/${denID}/collections/${col.id}`);
            onClose(true);
        });
    }
    return (
        <Dialog title={`Delete "${col.name}"?`} onClose={() => onClose(false)}>
            <p class="text-sm">This removes the collection. The messages saved in it stay where they were sent.</p>
            <ErrorText message={act.error} />
            <Actions>
                <Button kind="danger" busy={act.busy} data-confirm-delete-collection onClick={confirm}>Delete</Button>
                <Button kind="quiet" onClick={() => onClose(false)}>Cancel</Button>
            </Actions>
        </Dialog>
    );
}

const SAVE_POPOVER = { width: 288, height: 340 };

// SavePicker opens beside a message's bookmark button on desktop or as a bottom
// sheet on the phone layout. Toggling any checkbox adds or removes the message
// immediately; clicking the virtual "Saved" row or "+ Create" creates the
// personal collection and adds the message in one step.
export function SavePicker({ denID, channelID, messageID, collections, staff, anchor, narrow, onClose }) {
    const [query, setQuery] = useState('');
    const [checked, setChecked] = useState(null);
    const [created, setCreated] = useState([]);
    const [busy, setBusy] = useState('');
    const [error, setError] = useState('');
    const input = useRef(null);

    const [place] = useState(() => {
        if (narrow || !anchor) return null;
        const r = anchor.getBoundingClientRect();
        const vw = document.documentElement.clientWidth;
        const w = Math.min(SAVE_POPOVER.width, vw - 16);
        const h = Math.min(SAVE_POPOVER.height, innerHeight * 0.6);
        const right = Math.max(8, Math.min(vw - r.right, vw - w - 8));
        return r.top - 8 >= h ? { bottom: innerHeight - r.top + 8, right } : { top: Math.max(8, Math.min(r.bottom + 8, innerHeight - h - 8)), right };
    });

    useEffect(() => {
        let live = true;
        api.get(`/api/dens/${denID}/messages/${messageID}/collections`).then(
            (res) => live && setChecked(new Set(res.collection_ids || [])),
            (e) => live && setError(e.message),
        );
        return () => {
            live = false;
        };
    }, [denID, messageID]);

    useEffect(() => {
        if (narrow) return undefined;
        input.current?.focus();
        const away = (e) => {
            if (!e.target.closest?.('[data-save-picker]') && !anchor?.contains(e.target)) onClose();
        };
        const escape = (e) => {
            if (e.key !== 'Escape') return;
            e.preventDefault();
            onClose();
            if (anchor?.isConnected) anchor.focus();
        };
        document.addEventListener('pointerdown', away);
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('pointerdown', away);
            document.removeEventListener('keydown', escape);
        };
    }, [narrow]);

    // Merge collections from view with any freshly created locally before WS catches up.
    const merged = [...(collections || [])];
    for (const c of created) {
        if (!merged.some((x) => x.id === c.id)) merged.push(c);
    }
    const { personal, shared, lazySaved, createName } = pickerSections({ collections: merged, channelID, staff, query });

    async function toggle(col) {
        if (busy || !checked) return;
        const on = !checked.has(col.id);
        setBusy(col.id);
        setError('');
        try {
            if (on) {
                await api.post(`/api/dens/${denID}/collections/${col.id}/items`, { message_id: messageID });
                setChecked((cur) => new Set(cur).add(col.id));
            } else {
                await api.del(`/api/dens/${denID}/collections/${col.id}/items/${messageID}`);
                setChecked((cur) => {
                    const next = new Set(cur);
                    next.delete(col.id);
                    return next;
                });
            }
        } catch (e) {
            setError(e.message);
        } finally {
            setBusy('');
        }
    }

    async function createAndAdd(name, key) {
        if (busy) return;
        setBusy(key);
        setError('');
        try {
            const col = await api.post(`/api/dens/${denID}/collections`, { name });
            await api.post(`/api/dens/${denID}/collections/${col.id}/items`, { message_id: messageID });
            setCreated((cur) => [...cur, { ...col, count: 1 }]);
            setChecked((cur) => new Set(cur || []).add(col.id));
            setQuery('');
        } catch (e) {
            setError(e.message);
        } finally {
            setBusy('');
        }
    }

    const rowClass = 'flex min-h-9 w-full items-center gap-2 rounded px-2 py-1 text-left text-sm hover:bg-base-200 disabled:opacity-50 pointer-coarse:min-h-11';

    const content = (
        <div class="flex flex-col gap-2" data-save-picker>
            <div class={narrow ? 'px-3 pt-1' : ''}>
                <input ref={input} type="search" class="input input-sm w-full" placeholder="Filter or create collection…"
                    aria-label="Filter or create collection" value={query} maxLength={MAX_COLLECTION_NAME}
                    data-save-filter autocomplete="off"
                    onKeyDown={(e) => {
                        if (e.key === 'Enter' && createName && !busy) {
                            e.preventDefault();
                            createAndAdd(createName, 'create');
                            return;
                        }
                        if (narrow && e.key !== 'Escape') e.stopPropagation();
                    }}
                    onInput={(e) => setQuery(e.currentTarget.value)} />
            </div>
            <div class={`flex flex-col gap-2 overflow-y-auto ${narrow ? 'max-h-72 px-3 pb-2' : 'max-h-60'}`}>
                {checked === null && !error ? (
                    <div class="p-2 text-center"><span class="loading loading-dots loading-sm"></span></div>
                ) : (
                    <>
                        {shared.length > 0 && (
                            <div class="flex flex-col gap-0.5">
                                <div class="px-2 text-xs font-semibold uppercase text-base-content/50">This channel</div>
                                {shared.map((c) => (
                                    <button key={c.id} type="button" role="menuitemcheckbox" aria-checked={!!checked?.has(c.id)}
                                        class={rowClass} disabled={!!busy} data-save-option={c.id} onClick={() => toggle(c)}>
                                        <input type="checkbox" class="checkbox checkbox-xs pointer-events-none" checked={!!checked?.has(c.id)} tabIndex={-1} readOnly />
                                        <span class="min-w-0 flex-1 truncate">{c.name}</span>
                                        <span class="text-xs tabular-nums text-base-content/50">{c.count}</span>
                                    </button>
                                ))}
                            </div>
                        )}
                        <div class="flex flex-col gap-0.5">
                            <div class="px-2 text-xs font-semibold uppercase text-base-content/50">Personal</div>
                            {lazySaved && (
                                <button type="button" role="menuitemcheckbox" aria-checked="false"
                                    class={rowClass} disabled={!!busy} data-lazy-saved onClick={() => createAndAdd('Saved', 'lazy')}>
                                    <input type="checkbox" class="checkbox checkbox-xs pointer-events-none" checked={false} tabIndex={-1} readOnly />
                                    <span class="min-w-0 flex-1 truncate">Saved</span>
                                </button>
                            )}
                            {personal.map((c) => (
                                <button key={c.id} type="button" role="menuitemcheckbox" aria-checked={!!checked?.has(c.id)}
                                    class={rowClass} disabled={!!busy} data-save-option={c.id} onClick={() => toggle(c)}>
                                    <input type="checkbox" class="checkbox checkbox-xs pointer-events-none" checked={!!checked?.has(c.id)} tabIndex={-1} readOnly />
                                    <span class="min-w-0 flex-1 truncate">{c.name}</span>
                                    <span class="text-xs tabular-nums text-base-content/50">{c.count}</span>
                                </button>
                            ))}
                            {!lazySaved && personal.length === 0 && !createName && (
                                <p class="px-2 py-1 text-xs text-base-content/60">No matching personal collections.</p>
                            )}
                        </div>
                        {createName && (
                            <button type="button" class={`${buttonClass('quiet', 'sm')} justify-start`} disabled={!!busy}
                                data-save-create onClick={() => createAndAdd(createName, 'create')}>
                                + Create "{createName}"
                            </button>
                        )}
                    </>
                )}
                {error && <ErrorText message={error} />}
            </div>
        </div>
    );

    if (narrow) {
        return <Sheet label="Save to collection" title={<span class="font-semibold">Save to collection</span>} onClose={onClose}>{content}</Sheet>;
    }
    return (
        <div role="dialog" aria-label="Save to collection"
            class="fixed z-40 w-72 rounded-box border border-base-content/10 bg-base-100 p-2 shadow-xl"
            style={place || { right: 16, bottom: 16 }}>
            {content}
        </div>
    );
}
