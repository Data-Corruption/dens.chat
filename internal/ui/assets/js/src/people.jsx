// Members: the member list, profile cards, editing your own profile, and
// staff removing someone.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { HoverRow, Shown, usePicture } from './animations.js';
import { Avatar } from './avatar.jsx';
import { ErrorText, Field, SubmitButton, TextInput, day, useAction, useLater } from './components.jsx';
import { CropDialog } from './crop.jsx';
import { fileURL, formatSize, thumbURL, upload, uploadAnimated } from './files.jsx';
import { Dialog } from './manage.jsx';
import { Markdown } from './markdown.jsx';
import { VerifiedMark } from './private.jsx';
import { ExpirySetting } from './disappearing.jsx';

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
    return <span class={`badge badge-role ${role === 'owner' ? 'badge-warning' : 'badge-info'} ${size}`}>{role === 'owner' ? 'Owner' : 'Moderator'}</span>;
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
                            <HoverRow
                                as="button"
                                type="button"
                                class={`flex w-full items-center gap-2 rounded px-2 py-1 text-left hover:bg-base-300/60 pointer-coarse:min-h-11 ${dim ? 'opacity-60' : ''}`}
                                onClick={() => onProfile(m)}
                            >
                                <Avatar member={m} size="sm" online={online.has(m.id)} />
                                <span class="min-w-0 flex-1 truncate">{m.display_name}</span>
                                <RoleBadge role={m.role} size="badge-xs" />
                            </HoverRow>
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

// Profile shows a member: their banner and picture, their names and role,
// how long they've been in the den, and their bio. In a panel the bio runs
// its full length, which the panel scrolls. Its pictures play while it
// shows, since the member opened it (M15.1).
function Profile({ denID, member, me, online, panel = false, verified = false }) {
    const [bio, setBio] = useState(null);
    const [error, setError] = useState('');
    // A bio usually arrives at once; the dots show only if it's slow.
    const slow = useLater(bio === null && !error, 1000);
    useEffect(() => {
        api.get(`/api/dens/${denID}/members/${member.id}`).then((p) => setBio(p.bio || ''), (e) => setError(e.message));
    }, [member.id, member.display_name]);
    const left = !!member.left_at;
    return (
        <Shown>
            {member.banner?.id && <Banner denID={denID} image={member.banner} />}
            <div class="flex items-center gap-4">
                <Avatar member={member} size="lg" online={left ? undefined : online} />
                <div class="flex min-w-0 flex-col items-start gap-1">
                    <p class="flex max-w-full items-center gap-1 text-lg font-semibold">
                        <span class="truncate">{member.display_name}</span>
                        {verified && <VerifiedMark id={`profile-verified-${member.id}${panel ? '-panel' : ''}`} name={member.display_name} />}
                    </p>
                    <p class="max-w-full truncate text-sm text-base-content/70">@{member.username}</p>
                    <RoleBadge role={member.role} />
                </div>
            </div>
            <p class="text-xs text-base-content/60">{left ? `No longer in this den since ${day(member.left_at)}` : `Member since ${day(member.joined_at)}`}</p>
            {slow && <span class="loading loading-dots loading-sm"></span>}
            {bio && (
                <div class={`rounded p-2 text-sm ${panel ? 'bg-base-100' : 'max-h-48 overflow-y-auto bg-base-200'}`}>
                    <Markdown text={bio} me={me} />
                </div>
            )}
            <ErrorText message={error} />
        </Shown>
    );
}

// ProfilePanel shows the other member of a DM beside it, in the member
// list's place (M11), and from M16.1 whether this member verified them,
// with Compare codes. kept is what this member's Dens kept of their
// identity, null on a den that keeps none. expiry is the DM, whose time
// either member sets (M16.4), where the den keeps one.
export function ProfilePanel({ denID, member, me, online, kept, onCompare, onAddPeople, expiry }) {
    return (
        <div class="flex flex-col gap-3 p-3">
            <Profile denID={denID} member={member} me={me} online={online} panel verified={!!kept?.verified} />
            <CompareCodes member={member} kept={kept} onCompare={onCompare} />
            {/* A group of the two and those added, which leaves the DM as it
                is (M16.3). */}
            {onAddPeople && (
                <div>
                    <button type="button" class="btn btn-sm" data-add-people onClick={onAddPeople}>Add people</button>
                </div>
            )}
            {expiry && <ExpirySetting denID={denID} channel={expiry} />}
        </div>
    );
}

// CompareCodes offers to compare codes with a member whose Dens publishes
// an identity (M16.1), saying where their keys changed since this member
// verified them.
function CompareCodes({ member, kept, onCompare }) {
    if (!kept || !member.identity || member.left_at) return null;
    return (
        <div class="flex flex-col items-start gap-1">
            {kept.held && <p class="text-sm text-warning">Their keys changed since you compared codes.</p>}
            <button type="button" class="btn btn-sm" onClick={() => onCompare(member)}>{kept.verified ? 'Compare codes again' : 'Compare codes'}</button>
        </div>
    );
}

// ProfileCard shows a member, and what the viewer can do with them.
// onNotes, on the member's own card, opens their notes (M17.3), while the
// den has them for this Dens.
export function ProfileCard({ denID, member, me, role, online, kept, onClose, onMessage, onNotes, onCompare, onEdit, onRemove }) {
    const act = useAction();
    const self = member.id === me.id;
    const left = !!member.left_at;
    const canManage = !self && rank(role) > rank(member.role);

    function setRole(next) {
        act.run(() => api.put(`/api/dens/${denID}/members/${member.id}/role`, { role: next }));
    }

    return (
        <Dialog title="Profile" onClose={onClose}>
            <Profile denID={denID} member={member} me={me} online={online} verified={!self && !!kept?.verified} />
            <ErrorText message={act.error} />
            <div class="flex flex-wrap gap-2">
                {self && <button type="button" class="btn btn-sm" onClick={onEdit}>Edit profile</button>}
                {self && onNotes && <button type="button" class="btn btn-sm" data-own-notes onClick={onNotes}>Notes</button>}
                {!self && !left && <button type="button" class="btn btn-primary btn-sm" onClick={() => onMessage(member)}>Message</button>}
                {!self && kept && member.identity && !left && (
                    <button type="button" class="btn btn-sm" onClick={() => onCompare(member)}>{kept.verified ? 'Compare codes again' : 'Compare codes'}</button>
                )}
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

// Banner shows the wide picture on a profile, or an event's (M17.4), three
// times as wide as it is high, at its final size before it loads. An
// animated one plays as an avatar does (M15.1), which in a profile is
// while it shows.
export function Banner({ denID, image }) {
    const how = usePicture(!!image.animated);
    const cover = 'h-full w-full select-none object-cover';
    return (
        <div class="relative aspect-[3/1] w-full overflow-hidden rounded bg-base-300">
            <img src={how === 'file' ? fileURL(denID, image.id) : thumbURL(denID, image.id)} alt="" draggable={false} class={cover} />
            {how === 'hover' && <img src={fileURL(denID, image.id)} alt="" draggable={false} class={`absolute inset-0 ${cover}`} />}
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
    // den deletes the one it replaces, which the upload names, so a member
    // at their limit can still change it (M5). An animated one goes as it
    // is, with the part framed, for the local service to make (M15.1).
    function savePicture(blob, rect) {
        const { shape } = cropping;
        picture.run(async () => {
            const name = `${shape}.${blob.type === 'image/webp' ? 'webp' : 'png'}`;
            const up = rect
                ? await uploadAnimated(denID, shape, blob, rect, me[shape]?.id)
                : await upload(denID, '', blob, name, undefined, me[shape]?.id).done;
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
            <Shown>
                <p class="text-sm text-base-content/70">Each den has its own profile.</p>
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
                        <input ref={pickAvatar} type="file" accept="image/*" class="hidden" data-picture="avatar" onChange={pick('avatar')} />
                        <input ref={pickBanner} type="file" accept="image/*" class="hidden" data-picture="banner" onChange={pick('banner')} />
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
            </Shown>
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
                <Field label="Delete their messages from" hint="Their direct messages too, which go unread.">
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
