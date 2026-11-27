// Custom roles for channel visibility: Manage's Roles section, the
// per-member role picker, and helpers shared with channels, mentions,
// events and profiles.

import { useLayoutEffect, useState } from 'preact/hooks';
import { api } from './core/api.js';
import { Actions, Button, Card, ErrorText, Field, SubmitButton, TextInput, Tip, useAction } from './ui/components.jsx';
import { Dialog } from './manage.jsx';
import { MAX_ROLES, MAX_ROLE_NAME, canSeeChannel, roleFlipChannels, roleNames, roleTipText } from './core/roles.js';

export { MAX_ROLES, MAX_ROLE_NAME, canSeeChannel, roleFlipChannels, roleNames, roleTipText };

function channelTag(c) {
    return c.kind === 'voice' ? c.name : `#${c.name}`;
}

// RolesSection lets staff create, rename and delete custom roles in Manage.
// Deleting a role warns if any channel's only role is this one and will
// flip to staff-only.
export function RolesSection({ denID, view, onChanged }) {
    const roles = view.roles || [];
    const [newName, setNewName] = useState('');
    const [renaming, setRenaming] = useState(null); // { id, name }
    const [deleting, setDeleting] = useState('');
    const create = useAction();
    const edit = useAction();
    const remove = useAction();
    const atCap = roles.length >= MAX_ROLES;

    const activeMembers = (view.members || []).filter((m) => !m.left_at);
    const memberCount = (id) => activeMembers.filter((m) => m.role_ids?.includes(id)).length;
    const channelCount = (id) => (view.channels || []).filter((c) => c.role_ids?.includes(id)).length;

    function addRole(e) {
        e.preventDefault();
        if (atCap || !newName.trim()) return;
        create.run(async () => {
            await api.post(`/api/dens/${denID}/roles`, { name: newName });
            setNewName('');
            await onChanged();
        });
    }

    function saveRename(e) {
        e.preventDefault();
        if (!renaming || !renaming.name.trim()) return;
        edit.run(async () => {
            await api.patch(`/api/dens/${denID}/roles/${renaming.id}`, { name: renaming.name });
            setRenaming(null);
            await onChanged();
        });
    }

    function confirmDelete(id) {
        remove.run(async () => {
            await api.del(`/api/dens/${denID}/roles/${id}`);
            setDeleting('');
            await onChanged();
        });
    }

    return (
        <>
            <Card title={<>Roles{' '}
                <Tip id="roles-tip" side="right">
                    Custom roles control who can see restricted channels. Staff always see every channel.
                </Tip></>} data-roles-create>
                <form class="flex flex-wrap items-end gap-2" onSubmit={addRole}>
                    <div class="min-w-0 flex-1">
                        <Field label={`New role (${roles.length} / ${MAX_ROLES})`}>
                            <TextInput value={newName} onInput={setNewName} placeholder="e.g. Raiders" maxlength={String(MAX_ROLE_NAME)}
                                disabled={atCap || create.busy} required data-role-name />
                        </Field>
                    </div>
                    <SubmitButton busy={create.busy} disabled={atCap || !newName.trim()} size="md" class="mb-1" data-role-add>Add role</SubmitButton>
                </form>
                {atCap && <p class="text-sm text-base-content/70">This den has the maximum of {MAX_ROLES} roles.</p>}
                <ErrorText message={create.error} />
            </Card>
            <Card title={`Roles (${roles.length})`} data-roles-list>
                <ErrorText message={edit.error || remove.error} />
                {roles.length === 0 ? (
                    <p class="text-sm text-base-content/70">No custom roles yet.</p>
                ) : (
                    <ul class="flex flex-col divide-y divide-base-300">
                        {roles.map((r) => {
                            const isRenaming = renaming?.id === r.id;
                            const isDeleting = deleting === r.id;
                            const mc = memberCount(r.id);
                            const cc = channelCount(r.id);
                            const flips = isDeleting ? roleFlipChannels(r.id, view.channels) : [];
                            return (
                                <li key={r.id} class="flex flex-col gap-2 py-2" data-role={r.id}>
                                    {isRenaming ? (
                                        <form class="flex flex-wrap items-center gap-2" onSubmit={saveRename}>
                                            <div class="min-w-0 flex-1">
                                                <TextInput value={renaming.name} onInput={(v) => setRenaming({ ...renaming, name: v })}
                                                    aria-label="Role name" maxlength={String(MAX_ROLE_NAME)} required data-role-rename-input />
                                            </div>
                                            <SubmitButton busy={edit.busy} disabled={!renaming.name.trim()} data-role-rename-save>Save</SubmitButton>
                                            <Button kind="quiet" disabled={edit.busy} onClick={() => setRenaming(null)}>Cancel</Button>
                                        </form>
                                    ) : (
                                        <div class="flex flex-wrap items-center gap-2">
                                            <div class="min-w-0 flex-1">
                                                <p class="break-words text-sm font-medium" data-role-label>{r.name}</p>
                                                <p class="text-xs text-base-content/60">
                                                    {mc} {mc === 1 ? 'member' : 'members'} · {cc} {cc === 1 ? 'channel' : 'channels'}
                                                </p>
                                            </div>
                                            <div class="flex shrink-0 items-center gap-1">
                                                <Button kind="quiet" disabled={edit.busy || remove.busy}
                                                    onClick={() => { setDeleting(''); edit.setError(''); setRenaming({ id: r.id, name: r.name }); }}
                                                    data-role-rename={r.id}>Rename</Button>
                                                <Button kind="danger" disabled={edit.busy || remove.busy}
                                                    onClick={() => { setRenaming(null); remove.setError(''); setDeleting(isDeleting ? '' : r.id); }}
                                                    data-role-delete={r.id}>Delete…</Button>
                                            </div>
                                        </div>
                                    )}
                                    {isDeleting && (
                                        <div class="flex flex-col gap-2 rounded-box bg-base-100 p-3 text-sm" data-role-confirm={r.id}>
                                            {flips.length > 0 && (
                                                <p class="font-medium text-warning" data-role-flip-warning>
                                                    Deleting {r.name} will make {flips.map(channelTag).join(', ')} staff-only.
                                                </p>
                                            )}
                                            <p class="text-base-content/70">
                                                Delete {r.name}? Members with this role will lose access to its channels.
                                            </p>
                                            <Actions>
                                                <Button kind="danger" filled busy={remove.busy} onClick={() => confirmDelete(r.id)} data-role-confirm-delete>
                                                    Delete role
                                                </Button>
                                                <Button kind="quiet" disabled={remove.busy} onClick={() => setDeleting('')}>Cancel</Button>
                                            </Actions>
                                        </div>
                                    )}
                                </li>
                            );
                        })}
                    </ul>
                )}
            </Card>
        </>
    );
}

// MemberRolesDialog sets the custom roles one member holds. When nested
// inside Manage, stopEscape intercepts Escape so Manage stays open.
export function MemberRolesDialog({ denID, member, roles, stopEscape = false, onClose }) {
    const [picked, setPicked] = useState(() => new Set(member.role_ids || []));
    const save = useAction();

    useLayoutEffect(() => {
        if (!stopEscape) return undefined;
        const escape = (e) => {
            if (e.key !== 'Escape') return;
            e.preventDefault();
            e.stopImmediatePropagation();
            onClose(false);
        };
        document.addEventListener('keydown', escape, true);
        return () => document.removeEventListener('keydown', escape, true);
    }, [stopEscape, onClose]);

    function toggle(id) {
        setPicked((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    }

    function submit(e) {
        e.preventDefault();
        save.run(async () => {
            await api.put(`/api/dens/${denID}/members/${member.id}/roles`, { role_ids: [...picked] });
            onClose(true);
        });
    }

    return (
        <Dialog title={`Roles for ${member.display_name}`} onClose={() => onClose(false)}>
            <form class="flex flex-col gap-3" onSubmit={submit} data-member-roles-dialog>
                {roles.length === 0 ? (
                    <p class="text-sm text-base-content/70">No custom roles yet. Create one in Manage → Roles.</p>
                ) : (
                    <div class="flex max-h-64 flex-col gap-1 overflow-y-auto" role="group" aria-label="Roles">
                        {roles.map((r) => (
                            <label key={r.id} class="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-base-200 pointer-coarse:min-h-11">
                                <input type="checkbox" class="checkbox checkbox-sm" checked={picked.has(r.id)}
                                    onChange={() => toggle(r.id)} data-member-role={r.id} />
                                <span class="min-w-0 break-words text-sm">{r.name}</span>
                            </label>
                        ))}
                    </div>
                )}
                <ErrorText message={save.error} />
                <Actions>
                    {roles.length > 0 && <SubmitButton busy={save.busy} data-member-roles-save>Save</SubmitButton>}
                    <Button kind="quiet" onClick={() => onClose(false)}>Cancel</Button>
                </Actions>
            </form>
        </Dialog>
    );
}
