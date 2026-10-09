// The owner's channel and group settings. Changes go to the den, which
// announces them as events; the sidebar updates from those.

import { useState } from 'preact/hooks';
import { api } from './api.js';
import { ErrorText, Field, SubmitButton, TextInput, useAction } from './components.jsx';

export function Dialog({ title, onClose, children }) {
    return (
        <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={(e) => e.target === e.currentTarget && onClose()}>
            <div role="dialog" aria-label={title} class="card max-h-full w-full max-w-md overflow-y-auto bg-base-100 shadow-xl">
                <div class="card-body gap-3">
                    <div class="flex items-center justify-between">
                        <h2 class="card-title">{title}</h2>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={onClose} aria-label="Close">✕</button>
                    </div>
                    {children}
                </div>
            </div>
        </div>
    );
}

// Voice channels' bitrates, in bits a second (M4.2).
const BITRATE = { min: 16000, max: 128000, step: 8000, default: 96000 };

// ChannelDialog creates a channel, or changes or deletes one, a voice
// channel's bitrate included.
export function ChannelDialog({ denID, view, channel, onClose }) {
    const editing = !!channel;
    const [name, setName] = useState(channel?.name || '');
    const [kind, setKind] = useState(channel?.kind || 'text');
    const [description, setDescription] = useState(channel?.description || '');
    const [bitrate, setBitrate] = useState(channel?.bitrate || BITRATE.default);
    const [group, setGroup] = useState(channel?.group_id || '');
    const [staffOnly, setStaffOnly] = useState(channel?.staff_only || false);
    const save = useAction();
    const remove = useAction();
    const [confirmDelete, setConfirmDelete] = useState(false);
    const siblings = view.channels.filter((c) => (c.group_id || '') === (channel?.group_id || ''));

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

    function move(delta) {
        save.run(async () => {
            await api.patch(`/api/dens/${denID}/channels/${channel.id}`, { position: Math.max(0, channel.position + delta) });
            onClose();
        });
    }

    return (
        <Dialog title={editing ? (channel.kind === 'voice' ? `🔊 ${channel.name}` : `#${channel.name}`) : 'New channel'} onClose={onClose}>
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
                    <Field label="Description" hint="Shown at the top of the channel. The same formatting as messages works here.">
                        <textarea class="textarea w-full" rows="4" value={description} onInput={(e) => setDescription(e.currentTarget.value)} maxlength="4000"></textarea>
                    </Field>
                )}
                {kind === 'voice' && (
                    <Field label={`Voice quality: ${bitrate / 1000} kbps`}
                        hint="The most each voice in the call sends. Higher sounds better, and costs more of the den's upload: each member who talks sends it to everyone else in the call. A change reaches a call in progress at once.">
                        <input type="range" class="range range-sm w-full" min={BITRATE.min} max={BITRATE.max} step={BITRATE.step} value={bitrate}
                            onInput={(e) => setBitrate(Number(e.currentTarget.value))} aria-label="Voice quality" aria-valuetext={`${bitrate / 1000} kbps`} />
                    </Field>
                )}
                <Field label="Group">
                    <select class="select w-full" value={group} onChange={(e) => setGroup(e.currentTarget.value)}>
                        <option value="">No group</option>
                        {view.groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                    </select>
                </Field>
                <label class="label gap-2">
                    <input type="checkbox" class="toggle" checked={staffOnly} onChange={(e) => setStaffOnly(e.currentTarget.checked)} />
                    Staff only: only moderators and the owner can see it
                </label>
                <ErrorText message={save.error} />
                <div class="flex flex-wrap gap-2">
                    <SubmitButton busy={save.busy}>{editing ? 'Save' : 'Create channel'}</SubmitButton>
                    {editing && (
                        <>
                            <button type="button" class="btn" disabled={save.busy || channel.position === 0} onClick={() => move(-1)}>Move up</button>
                            <button type="button" class="btn" disabled={save.busy || channel.position >= siblings.length - 1} onClick={() => move(1)}>Move down</button>
                        </>
                    )}
                </div>
            </form>
            {editing && (
                <div class="border-t border-base-300 pt-3">
                    {!confirmDelete ? (
                        <button type="button" class="btn btn-outline btn-error btn-sm" onClick={() => setConfirmDelete(true)}>Delete channel</button>
                    ) : (
                        <div class="flex flex-col gap-2">
                            <p class="text-sm">This deletes #{channel.name} and every message in it, for everyone. It can't be undone.</p>
                            <div class="flex gap-2">
                                <button
                                    type="button"
                                    class="btn btn-error btn-sm"
                                    disabled={remove.busy}
                                    onClick={() => remove.run(async () => {
                                        await api.del(`/api/dens/${denID}/channels/${channel.id}`);
                                        onClose();
                                    })}
                                >
                                    Delete it
                                </button>
                                <button type="button" class="btn btn-ghost btn-sm" onClick={() => setConfirmDelete(false)}>Cancel</button>
                            </div>
                            <ErrorText message={remove.error} />
                        </div>
                    )}
                </div>
            )}
        </Dialog>
    );
}

// GroupDialog creates a group, or renames, moves or deletes one.
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
                <div class="flex flex-wrap gap-2">
                    <SubmitButton busy={save.busy}>{editing ? 'Save' : 'Create group'}</SubmitButton>
                    {editing && (
                        <>
                            <button type="button" class="btn" disabled={save.busy || group.position === 0}
                                onClick={() => act(() => api.patch(`/api/dens/${denID}/groups/${group.id}`, { position: group.position - 1 }))}>
                                Move up
                            </button>
                            <button type="button" class="btn" disabled={save.busy || group.position >= view.groups.length - 1}
                                onClick={() => act(() => api.patch(`/api/dens/${denID}/groups/${group.id}`, { position: group.position + 1 }))}>
                                Move down
                            </button>
                        </>
                    )}
                </div>
            </form>
            {editing && (
                <div class="border-t border-base-300 pt-3">
                    <p class="mb-2 text-sm text-base-content/70">Deleting the group keeps its channels; they move to the ungrouped list.</p>
                    <button type="button" class="btn btn-outline btn-error btn-sm" disabled={save.busy}
                        onClick={() => act(() => api.del(`/api/dens/${denID}/groups/${group.id}`))}>
                        Delete group
                    </button>
                </div>
            )}
        </Dialog>
    );
}
