// The owner's channel and group settings. Changes go to the den, which
// announces them as events; the sidebar updates from those.

import { useEffect, useState } from 'preact/hooks';
import { api } from './core/api.js';
import { callLength } from './voice/calltimer.js';
import { Actions, Button, ErrorText, Field, InSheet, Slider, SubmitButton, TextInput, Tip, Toggle, useAction, useBackdrop } from './ui/components.jsx';
import { useLayer, useNarrow } from './core/nav.js';
import { Handle, useDragDown } from './ui/sheet.jsx';

// WIDTHS are a dialog's widest on a wide window, spelled out so Tailwind
// keeps every class.
const WIDTHS = { false: 'md:max-w-md', true: 'md:max-w-3xl' };

// Dialog frames a dialog over the page, which Back closes, as do its ✕ and
// a click outside it. On the phone layout it's a sheet from the
// screen's foot, as tall as it needs up to the screen's, its title and ✕
// staying at its top as the rest scrolls, and a swipe down from there
// closes it too. A bare one, a profile card, shows no
// title, which still names it for screen readers: what it shows runs to
// its edges, the ✕ standing over its top right, and on the phone layout
// the handle over its top, both staying there as it scrolls; a swipe down
// closes it from the handle, or from what it shows marked as its top. One
// with a foot keeps it at its bottom as the rest scrolls, as Edit
// profile's Save; a wide one takes room for two columns.
export function Dialog({ title, onClose, bare = false, wide = false, foot, children }) {
    useLayer(onClose);
    const backdrop = useBackdrop(onClose);
    const narrow = useNarrow();
    const drag = useDragDown(onClose, (e) => narrow && !!e.target.closest('[data-sheet-top]') && !e.target.closest('button'));
    return (
        <InSheet.Provider value={narrow}>
            <div class="fixed inset-0 z-50 flex items-end justify-center bg-black/40 md:items-center md:p-4" {...backdrop}>
                <div ref={drag.ref} role="dialog" aria-modal="true" aria-label={title} style={drag.style} {...drag.handlers}
                    class={`card max-h-[calc(100%-1.5rem)] w-full overscroll-contain bg-base-100 shadow-xl max-md:rounded-b-none md:max-h-full ${WIDTHS[wide] ?? WIDTHS.true} ${foot ? 'flex flex-col overflow-hidden' : 'overflow-y-auto max-md:pb-[env(safe-area-inset-bottom)]'}`}>
                    {bare ? (
                        <>
                            <div class="pointer-events-none sticky top-0 z-10 h-0">
                                <div data-sheet-top class="pointer-events-auto absolute left-1/2 top-0 h-7 w-24 -translate-x-1/2 touch-none md:hidden">
                                    <div class="flex justify-center pt-2" aria-hidden="true">
                                        <span class="h-1 w-10 rounded-full bg-base-100/90 shadow-[0_0_0_1px] shadow-base-content/25"></span>
                                    </div>
                                </div>
                                <button type="button" class="btn btn-ghost btn-circle btn-sm pointer-events-auto absolute right-2 top-2 bg-base-100/85 shadow-sm hover:bg-base-100"
                                    onClick={onClose} aria-label="Close">✕</button>
                            </div>
                            {children}
                        </>
                    ) : (
                        <>
                            <div class={foot ? 'min-h-0 flex-1 overflow-y-auto overscroll-contain' : 'contents'}>
                                <div class="card-body gap-3 max-md:px-4 max-md:pt-0">
                                    <div data-sheet-top class="sticky top-0 z-10 -mx-1 bg-base-100 px-1 max-md:touch-none max-md:pb-1 md:static">
                                        <div class="md:hidden"><Handle /></div>
                                        <div class="flex items-center justify-between gap-2">
                                            <h2 class="card-title min-w-0">{title}</h2>
                                            <button type="button" class="btn btn-ghost btn-sm btn-square shrink-0" onClick={onClose} aria-label="Close">✕</button>
                                        </div>
                                    </div>
                                    {children}
                                </div>
                            </div>
                            {foot && (
                                <div class="shrink-0 border-t border-base-300 px-6 py-3 max-md:px-4 max-md:pb-[max(0.75rem,env(safe-area-inset-bottom))]" data-dialog-foot>
                                    {foot}
                                </div>
                            )}
                        </>
                    )}
                </div>
            </div>
        </InSheet.Provider>
    );
}

// Voice channels' bitrates, in bits a second.
const BITRATE = { min: 16000, max: 128000, step: 8000, default: 96000 };

// ChannelDialog creates a channel, in the group inGroup if given, or
// changes or deletes one, a voice channel's bitrate included. Moving one
// is its menu's, and the drag's.
export function ChannelDialog({ denID, view, channel, group: inGroup, onClose }) {
    const editing = !!channel;
    const [name, setName] = useState(channel?.name || '');
    const [kind, setKind] = useState(channel?.kind || 'text');
    const [description, setDescription] = useState(channel?.description || '');
    const [bitrate, setBitrate] = useState(channel?.bitrate || BITRATE.default);
    const [group, setGroup] = useState(channel?.group_id || inGroup || '');
    const [staffOnly, setStaffOnly] = useState(channel?.staff_only || false);
    const save = useAction();
    const remove = useAction();
    const [confirmDelete, setConfirmDelete] = useState(false);

    function submit(e) {
        e.preventDefault();
        save.run(async () => {
            if (!editing) {
                await api.post(`/api/dens/${denID}/channels`, {
                    name, kind, description: kind === 'text' ? description : undefined, group_id: group || undefined, staff_only: staffOnly,
                    bitrate: kind === 'voice' ? bitrate : undefined,
                });
            } else {
                const body = {};
                if (name !== channel.name) body.name = name;
                if (description !== (channel.description || '')) body.description = description;
                if (group !== (channel.group_id || '')) body.group_id = group;
                if (staffOnly !== channel.staff_only) body.staff_only = staffOnly;
                if (kind === 'voice' && bitrate !== channel.bitrate) body.bitrate = bitrate;
                await api.patch(`/api/dens/${denID}/channels/${channel.id}`, body);
            }
            onClose();
        });
    }

    return (
        <Dialog title={editing ? (channel.kind === 'voice' ? `Voice channel ${channel.name}` : `#${channel.name}`) : 'New channel'} onClose={onClose}>
            <form class="flex flex-col gap-2" onSubmit={submit}>
                <Field label="Name">
                    <TextInput value={name} onInput={setName} required maxlength="32" />
                </Field>
                {!editing && (
                    <Field label="Kind">
                        <select class="select w-full" value={kind} onChange={(e) => setKind(e.currentTarget.value)}>
                            <option value="text">Text</option>
                            <option value="voice">Voice</option>
                        </select>
                    </Field>
                )}
                {kind === 'text' && (
                    <Field label="Description" hint="Shown at the top of the channel, formatted as messages are.">
                        <textarea class="textarea w-full" rows="4" value={description} onInput={(e) => setDescription(e.currentTarget.value)} maxlength="4000"></textarea>
                    </Field>
                )}
                {kind === 'voice' && (
                    <Field label={<>Voice quality: {bitrate / 1000} kbps <Tip id="bitrate-tip">Each member who talks sends it to everyone else in the call. A change reaches a call in progress at once.</Tip></>}
                        hint="Higher sounds better and costs more of the den's upload.">
                        <Slider class="w-full" min={BITRATE.min} max={BITRATE.max} step={BITRATE.step} value={bitrate}
                            onInput={(e) => setBitrate(Number(e.currentTarget.value))} aria-label="Voice quality" aria-valuetext={`${bitrate / 1000} kbps`} />
                    </Field>
                )}
                <Field label="Group">
                    <select class="select w-full" value={group} onChange={(e) => setGroup(e.currentTarget.value)}>
                        <option value="">No group</option>
                        {view.groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                    </select>
                </Field>
                <Toggle id="staff-only" label="Staff only" hint="Only moderators and the owner can see it." checked={staffOnly} onChange={setStaffOnly} />
                <ErrorText message={save.error} />
                <Actions>
                    <SubmitButton busy={save.busy}>{editing ? 'Save' : 'Create channel'}</SubmitButton>
                    <Button kind="quiet" onClick={onClose}>Cancel</Button>
                </Actions>
            </form>
            {editing && channel.kind === 'voice' && <CallRecord denID={denID} channel={channel} />}
            {editing && (
                <div class="border-t border-base-300 pt-3">
                    {!confirmDelete ? (
                        <Button kind="danger" onClick={() => setConfirmDelete(true)}>Delete channel</Button>
                    ) : (
                        <div class="flex flex-col gap-2">
                            <p class="text-sm">Deletes #{channel.name} and every message in it, for everyone, for good.</p>
                            <Actions>
                                <Button kind="danger" filled busy={remove.busy} onClick={() => remove.run(async () => {
                                    await api.del(`/api/dens/${denID}/channels/${channel.id}`);
                                    onClose();
                                })}>
                                    Delete it
                                </Button>
                                <Button kind="quiet" onClick={() => setConfirmDelete(false)}>Cancel</Button>
                            </Actions>
                            <ErrorText message={remove.error} />
                        </div>
                    )}
                </div>
            )}
        </Dialog>
    );
}

// CallRecord shows staff a voice channel's longest call, with
// Reset, which asks first, since every member's timer counts against it.
function CallRecord({ denID, channel }) {
    const path = `/api/dens/${denID}/channels/${channel.id}/record`;
    const [record, setRecord] = useState(null);
    const [confirming, setConfirming] = useState(false);
    const load = useAction();
    const reset = useAction();
    useEffect(() => {
        load.run(async () => setRecord((await api.get(path)).record_ms));
    }, [path]);
    return (
        <div class="flex flex-col gap-2 border-t border-base-300 pt-3" data-call-record>
            <div class="flex min-h-8 items-center justify-between gap-2">
                <span class="text-sm">
                    Longest call:{' '}
                    {record === null ? <span class="loading loading-dots loading-xs align-middle"></span>
                        : <span class="tabular-nums" data-call-record-ms={record}>{record > 0 ? callLength(record) : 'none yet'}</span>}
                </span>
                {record > 0 && !confirming && <Button onClick={() => setConfirming(true)}>Reset</Button>}
            </div>
            {confirming && (
                <div class="flex flex-col gap-2">
                    <p class="text-sm">Clears the longest call for everyone. A call going on now won't set the next one.</p>
                    <Actions>
                        <Button kind="danger" filled busy={reset.busy} onClick={() => reset.run(async () => {
                            await api.del(path);
                            setRecord(0);
                            setConfirming(false);
                        })}>
                            Reset it
                        </Button>
                        <Button kind="quiet" onClick={() => setConfirming(false)}>Cancel</Button>
                    </Actions>
                </div>
            )}
            <ErrorText message={load.error || reset.error} />
        </div>
    );
}

// GroupDialog creates a group, or renames or deletes one; moving one is
// its menu's, and the drag's.
export function GroupDialog({ denID, view, group, onClose }) {
    const editing = !!group;
    const [name, setName] = useState(group?.name || '');
    const save = useAction();

    function submit(e) {
        e.preventDefault();
        save.run(async () => {
            if (editing) await api.patch(`/api/dens/${denID}/groups/${group.id}`, { name });
            else await api.post(`/api/dens/${denID}/groups`, { name });
            onClose();
        });
    }

    function act(fn) {
        save.run(async () => {
            await fn();
            onClose();
        });
    }

    return (
        <Dialog title={editing ? group.name : 'New group'} onClose={onClose}>
            <form class="flex flex-col gap-2" onSubmit={submit}>
                <Field label="Name">
                    <TextInput value={name} onInput={setName} required maxlength="32" />
                </Field>
                <ErrorText message={save.error} />
                <Actions>
                    <SubmitButton busy={save.busy}>{editing ? 'Save' : 'Create group'}</SubmitButton>
                    <Button kind="quiet" onClick={onClose}>Cancel</Button>
                </Actions>
            </form>
            {editing && (
                <div class="border-t border-base-300 pt-3">
                    <p class="mb-2 text-sm text-base-content/70">Its channels stay, moving to the ungrouped list.</p>
                    <Button kind="danger" busy={save.busy} onClick={() => act(() => api.del(`/api/dens/${denID}/groups/${group.id}`))}>
                        Delete group
                    </Button>
                </div>
            )}
        </Dialog>
    );
}
