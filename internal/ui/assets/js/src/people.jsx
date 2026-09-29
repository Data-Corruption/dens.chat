// Members: the member list, profile cards, editing your own profile, and
// staff removing someone.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { Avatar } from './avatar.jsx';
import { ErrorText, Field, SubmitButton, TextInput, day, useAction, useLater } from './components.jsx';
import { CropDialog } from './crop.jsx';
import { fileURL, formatSize, upload } from './files.jsx';
import { Dialog } from './manage.jsx';
import { Markdown } from './markdown.jsx';

const MAX_BIO = 300;

// rank orders roles as the den does: staff act only on those below them.
export function rank(role) {
    return role === 'owner' ? 2 : role === 'moderator' ? 1 : 0;
}

export function isStaff(role) {
    return rank(role) > 0;
}

function RoleBadge({ role, size = 'badge-sm' }) {
    if (!isStaff(role)) return null;
    return <span class={`badge badge-soft ${role === 'owner' ? 'badge-warning' : 'badge-info'} ${size}`}>{role === 'owner' ? 'Owner' : 'Moderator'}</span>;
}

const byName = (a, b) => a.display_name.localeCompare(b.display_name);

// MemberList shows who's in the den, online first.
export function MemberList({ members, online, onProfile }) {
    const here = members.filter((m) => !m.left_at);
    const on = here.filter((m) => online.has(m.id)).sort(byName);
    const off = here.filter((m) => !online.has(m.id)).sort(byName);
    const section = (title, list, dim) =>
        list.length > 0 && (
            <div>
                <h3 class="cursor-default select-none px-2 pt-2 text-xs font-semibold uppercase text-base-content/60">
                    {title} — {list.length}
                </h3>
                <ul>
                    {list.map((m) => (
                        <li key={m.id}>
                            <button
                                type="button"
                                class={`flex w-full items-center gap-2 rounded px-2 py-1 text-left hover:bg-base-300/60 ${dim ? 'opacity-60' : ''}`}
                                onClick={() => onProfile(m)}
                            >
                                <Avatar member={m} size="sm" online={online.has(m.id)} />
                                <span class="min-w-0 flex-1 truncate">{m.display_name}</span>
                                <RoleBadge role={m.role} size="badge-xs" />
                            </button>
                        </li>
                    ))}
                </ul>
            </div>
        );
    return (
        <nav aria-label="Members" class="flex flex-col gap-2 p-2">
            {section('Online', on, false)}
            {section('Offline', off, true)}
        </nav>
    );
}

// ProfileCard shows a member, and what the viewer can do with them.
export function ProfileCard({ denID, member, me, role, online, onClose, onMessage, onEdit, onRemove }) {
    const [bio, setBio] = useState(null);
    const [error, setError] = useState('');
    const act = useAction();
    // A bio usually arrives at once; the dots show only if it's slow.
    const slow = useLater(bio === null && !error, 1000);
    useEffect(() => {
        api.get(`/api/dens/${denID}/members/${member.id}`).then((p) => setBio(p.bio || ''), (e) => setError(e.message));
    }, [member.id, member.display_name]);
    const self = member.id === me.id;
    const left = !!member.left_at;
    const canManage = !self && rank(role) > rank(member.role);

    function setRole(next) {
        act.run(() => api.put(`/api/dens/${denID}/members/${member.id}/role`, { role: next }));
    }

    return (
        <Dialog title="Profile" onClose={onClose}>
            {member.banner?.id && <Banner denID={denID} image={member.banner} />}
            <div class="flex items-center gap-4">
                <Avatar member={member} size="lg" online={left ? undefined : online} />
                <div class="flex min-w-0 flex-col items-start gap-1">
                    <p class="max-w-full truncate text-lg font-semibold">{member.display_name}</p>
                    <p class="max-w-full truncate text-sm text-base-content/70">@{member.username}</p>
                    <RoleBadge role={member.role} />
                </div>
            </div>
            <p class="text-xs text-base-content/60">{left ? `No longer in this den since ${day(member.left_at)}` : `Member since ${day(member.joined_at)}`}</p>
            {slow && <span class="loading loading-dots loading-sm"></span>}
            {bio && (
                <div class="max-h-48 overflow-y-auto rounded bg-base-200 p-2 text-sm">
                    <Markdown text={bio} me={me} />
                </div>
            )}
            <ErrorText message={error || act.error} />
            <div class="flex flex-wrap gap-2">
                {self && <button type="button" class="btn btn-sm" onClick={onEdit}>Edit profile</button>}
                {!self && !left && <button type="button" class="btn btn-primary btn-sm" onClick={() => onMessage(member)}>Message</button>}
                {role === 'owner' && !self && !left && member.role === 'member' && (
                    <button type="button" class="btn btn-sm" disabled={act.busy} onClick={() => setRole('moderator')}>Make moderator</button>
                )}
                {role === 'owner' && !self && !left && member.role === 'moderator' && (
                    <button type="button" class="btn btn-sm" disabled={act.busy} onClick={() => setRole('member')}>Remove moderator</button>
                )}
                {canManage && (
                    <button type="button" class="btn btn-outline btn-error btn-sm" onClick={() => onRemove(member)}>
                        {left ? 'Ban…' : 'Remove from den…'}
                    </button>
                )}
            </div>
        </Dialog>
    );
}

// Banner shows the wide picture on a profile, three times as wide as it
// is high, at its final size before it loads.
function Banner({ denID, image }) {
    return (
        <div class="aspect-[3/1] w-full overflow-hidden rounded bg-base-300">
            <img src={fileURL(denID, image.id)} alt="" draggable={false} class="h-full w-full select-none object-cover" />
        </div>
    );
}

// EditProfile changes the member's own display name, bio and pictures in
// this den.
export function EditProfile({ denID, me, limits, onClose }) {
    const [name, setName] = useState(me.display_name);
    const [bio, setBio] = useState(null);
    const [cropping, setCropping] = useState(null);
    const [storage, setStorage] = useState(null);
    const save = useAction();
    const picture = useAction();
    const pickAvatar = useRef(null);
    const pickBanner = useRef(null);
    useEffect(() => {
        api.get(`/api/dens/${denID}/members/${me.id}`).then((p) => setBio(p.bio || ''), () => setBio(''));
        api.get(`/api/dens/${denID}/storage`).then(setStorage, () => {});
    }, [me.id]);
    const length = [...(bio || '')].length;

    function pick(shape) {
        return (e) => {
            const file = e.currentTarget.files[0];
            e.currentTarget.value = '';
            if (file) {
                picture.setError('');
                setCropping({ shape, file });
            }
        };
    }

    // savePicture uploads a cropped picture and puts it on the profile; the
    // den deletes the one it replaces.
    function savePicture(blob) {
        const { shape } = cropping;
        picture.run(async () => {
            const up = await upload(denID, blob, `${shape}.${blob.type === 'image/webp' ? 'webp' : 'png'}`).done;
            await api.patch(`/api/dens/${denID}/me`, { [shape]: up.id });
            setCropping(null);
        });
    }

    function removePicture(shape) {
        picture.run(() => api.patch(`/api/dens/${denID}/me`, { [shape]: '' }));
    }

    function submit(e) {
        e.preventDefault();
        save.run(async () => {
            await api.patch(`/api/dens/${denID}/me`, { display_name: name, bio });
            onClose();
        });
    }

    return (
        <Dialog title="Your profile in this den" onClose={onClose}>
            <p class="text-sm text-base-content/70">Each den has its own profile. Everyone in this den can see it.</p>
            <form class="flex flex-col gap-2" onSubmit={submit}>
                <fieldset class="fieldset">
                    <legend class="fieldset-legend">Picture</legend>
                    <div class="flex flex-wrap items-center gap-2">
                        <Avatar member={me} size="lg" />
                        <button type="button" class="btn btn-sm" disabled={picture.busy} onClick={() => pickAvatar.current?.click()}>
                            {me.avatar?.id ? 'Change' : 'Add a picture'}
                        </button>
                        {me.avatar?.id && <button type="button" class="btn btn-ghost btn-sm" disabled={picture.busy} onClick={() => removePicture('avatar')}>Remove</button>}
                    </div>
                </fieldset>
                <fieldset class="fieldset">
                    <legend class="fieldset-legend">Banner</legend>
                    {me.banner?.id ? <Banner denID={denID} image={me.banner} /> : <p class="label whitespace-normal">A wide picture across the top of your profile.</p>}
                    <div class="flex flex-wrap gap-2">
                        <button type="button" class="btn btn-sm" disabled={picture.busy} onClick={() => pickBanner.current?.click()}>
                            {me.banner?.id ? 'Change' : 'Add a banner'}
                        </button>
                        {me.banner?.id && <button type="button" class="btn btn-ghost btn-sm" disabled={picture.busy} onClick={() => removePicture('banner')}>Remove</button>}
                    </div>
                    <input ref={pickAvatar} type="file" accept="image/*" class="hidden" onChange={pick('avatar')} />
                    <input ref={pickBanner} type="file" accept="image/*" class="hidden" onChange={pick('banner')} />
                    {!cropping && <ErrorText message={picture.error} />}
                </fieldset>
                <Field label="Display name">
                    <TextInput value={name} onInput={setName} required maxlength="32" />
                </Field>
                <Field label="About you" hint={`Up to ${MAX_BIO} characters. The same formatting as messages works here.`}>
                    {/* The form shows at once; only the bio waits for the den. */}
                    <textarea class="textarea w-full" rows="4" value={bio ?? ''} disabled={bio === null}
                        placeholder={bio === null ? 'Loading…' : ''} onInput={(e) => setBio(e.currentTarget.value)}></textarea>
                </Field>
                {length > MAX_BIO - 50 && (
                    <p class={`cursor-default select-none text-right text-xs ${length > MAX_BIO ? 'text-error' : 'text-base-content/60'}`}>
                        {length} / {MAX_BIO}
                    </p>
                )}
                <ErrorText message={save.error} />
                <div>
                    <SubmitButton busy={save.busy}>Save</SubmitButton>
                </div>
                {storage && limits?.member_storage > 0 && (
                    <p class="cursor-default select-none text-xs text-base-content/60">
                        Your files here take {formatSize(storage.used)} of the {formatSize(limits.member_storage)} each member can use.
                    </p>
                )}
            </form>
            {cropping && (
                <CropDialog file={cropping.file} shape={cropping.shape} busy={picture.busy} error={picture.error}
                    onCrop={savePicture} onClose={() => setCropping(null)} />
            )}
        </Dialog>
    );
}

const WINDOWS = [
    [0, 'Nothing'],
    [3600, 'The last hour'],
    [86400, 'The last day'],
    [7 * 86400, 'The last week'],
];

// RemoveMember takes someone out of the den, and can ban them. Someone who
// already left can still be banned.
export function RemoveMember({ denID, member, onClose }) {
    const left = !!member.left_at;
    const [ban, setBan] = useState(left);
    const [span, setSpan] = useState(0);
    const [revoke, setRevoke] = useState(left);
    const act = useAction();

    function submit(e) {
        e.preventDefault();
        act.run(async () => {
            await api.post(`/api/dens/${denID}/members/${member.id}/remove`, { ban, delete_messages: span, revoke_invite: revoke });
            onClose();
        });
    }

    return (
        <Dialog title={left || ban ? `Ban @${member.username}` : `Remove @${member.username}`} onClose={onClose}>
            <form class="flex flex-col gap-3" onSubmit={submit}>
                <p class="text-sm">
                    {left
                        ? 'They already left. A ban keeps their username from coming back.'
                        : `${member.display_name} is signed out of this den at once, on every device. Their messages stay unless you delete them below.`}
                </p>
                {!left && (
                    <label class="label gap-2 whitespace-normal">
                        <input type="checkbox" class="checkbox checkbox-sm" checked={ban} onChange={(e) => {
                            setBan(e.currentTarget.checked);
                            setRevoke(e.currentTarget.checked);
                        }} />
                        Ban: don't let @{member.username} back in
                    </label>
                )}
                <Field label="Delete their messages from" hint="Direct messages aren't touched.">
                    <select class="select w-full" value={span} onChange={(e) => setSpan(Number(e.currentTarget.value))}>
                        {WINDOWS.map(([seconds, label]) => <option key={seconds} value={seconds}>{label}</option>)}
                    </select>
                </Field>
                <label class="label gap-2 whitespace-normal">
                    <input type="checkbox" class="checkbox checkbox-sm" checked={revoke} onChange={(e) => setRevoke(e.currentTarget.checked)} />
                    Revoke the invite they joined with, if others can still use it
                </label>
                <p class="text-xs text-base-content/70">
                    {ban
                        ? 'A ban keeps this username out, but anyone with a valid invite can still join under a new name, so share invites with care.'
                        : 'They can come back if someone invites them again.'}
                </p>
                <ErrorText message={act.error} />
                <div class="flex gap-2">
                    <button type="submit" class="btn btn-error btn-sm" disabled={act.busy}>
                        {act.busy && <span class="loading loading-spinner loading-sm"></span>}
                        {ban ? 'Ban' : 'Remove'}
                    </button>
                    <button type="button" class="btn btn-ghost btn-sm" onClick={onClose}>Cancel</button>
                </div>
            </form>
        </Dialog>
    );
}
