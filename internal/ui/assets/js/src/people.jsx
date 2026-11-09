// Members: the member list, profile cards, editing your own profile, and
// staff removing someone.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { HoverRow, Shown, usePicture } from './animations.js';
import { Avatar } from './avatar.jsx';
import { ErrorText, Field, HoverTip, LongName, SubmitButton, TextInput, Tip, day, useAction, useLater } from './components.jsx';
import { CropDialog } from './crop.jsx';
import { fileURL, formatSize, thumbURL, upload, uploadAnimated } from './files.jsx';
import { CalendarIcon, CrownIcon, EyeIcon, ShieldIcon } from './icons.jsx';
import { Dialog } from './manage.jsx';
import { Markdown } from './markdown.jsx';
import { PointerMenu, ownsMenu, rightClick } from './menus.jsx';
import { useNarrow } from './nav.js';
import { VerifiedMark } from './private.jsx';
import { ExpirySetting } from './disappearing.jsx';
import { badgeTip, zone } from './denevents.js';
import { Sheet, SheetRow } from './sheet.jsx';
import { useCoarse } from './touch.js';

const MAX_BIO = 300;

// rank orders roles as the den does: staff act only on those below them.
export function rank(role) {
    return role === 'owner' ? 2 : role === 'moderator' ? 1 : 0;
}

export function isStaff(role) {
    return rank(role) > 0;
}

/** roleMark names the mark beside a staff member's name (M17.6): Owner, a
    crown, or Moderator, a shield; a member has none */
export function roleMark(role) {
    return role === 'owner' ? 'Owner' : role === 'moderator' ? 'Moderator' : '';
}

// MARK is a role mark's color: darker than the icons' 60% of the text's,
// which falls under a graphic's 3:1 against the member list in some
// themes, Retro's among them.
export const MARK = 'text-base-content/80';

/** RoleMark is a staff member's mark beside their name, in place of a
    role's pill (M17.6). Its boxes are flex boxes down to the icon, so no
    line of text sets its height, and each row centers it on the name.
    With id, its tip opens on a tap too, as on the card; without, the tip
    is hover's alone and its words are read with the row's, where the row
    or the name is a button. */
export function RoleMark({ role, id }) {
    const label = roleMark(role);
    if (!label) return null;
    const icon = <span class={`inline-flex ${MARK}`}>{role === 'owner' ? <CrownIcon size="h-4 w-4" /> : <ShieldIcon size="h-4 w-4" />}</span>;
    return (
        <span class="inline-flex shrink-0" data-role-mark={role}>
            {id ? <Tip id={id} label={label} trigger={icon} wrap="inline-flex">{label}</Tip> : <HoverTip label={label} tip={label}>{icon}</HoverTip>}
        </span>
    );
}

/** shownBadges are the badges a profile card shows: all but those its
    member hid, which their own copy of it alone carries (M17.6) */
export function shownBadges(badges) {
    return badges.filter((b) => !b.hidden);
}

/** cardMenu lists what a profile card's ⋯ offers (M17.6), each by key,
    label and whether it's a danger: comparing codes with a member whose
    Dens publishes an identity, where this member's keeps them; the
    owner's promoting a member or demoting a moderator; and removing
    someone of a lower rank, or banning one who left. One's own card offers
    none of them */
export function cardMenu({ self, role, member, kept }) {
    if (self) return [];
    const left = !!member.left_at;
    const out = [];
    if (kept && member.identity && !left) out.push({ key: 'compare', label: kept.verified ? 'Compare codes again' : 'Compare codes' });
    if (role === 'owner' && !left && member.role === 'member') out.push({ key: 'promote', label: 'Promote to moderator' });
    if (role === 'owner' && !left && member.role === 'moderator') out.push({ key: 'demote', label: 'Demote to member' });
    if (rank(role) > rank(member.role)) out.push({ key: 'remove', label: left ? 'Ban…' : 'Remove from den…', danger: true });
    return out;
}

/** memberMenu lists what a right click on a member's row in the list
    offers: their profile, then what their card does, Message, or
    on one's own Edit profile and Notes, and what its ⋯ holds */
export function memberMenu({ self, role, member, kept, notes }) {
    const left = !!member.left_at;
    return [
        { key: 'profile', label: 'Profile' },
        self && { key: 'edit', label: 'Edit profile' },
        self && notes && { key: 'notes', label: 'Notes' },
        !self && !left && { key: 'message', label: 'Message' },
        ...cardMenu({ self, role, member, kept }),
    ].filter(Boolean);
}

const byName = (a, b) => a.display_name.localeCompare(b.display_name);

// MemberList shows who's in the den, online first. With a mouse, a right
// click on a row opens what the member's card does, at the pointer, or as
// a sheet on the phone layout: card holds the viewer, their role, the
// identities their Dens keeps, and what the card's buttons do.
export function MemberList({ denID, members, online, card, onProfile }) {
    const narrow = useNarrow();
    const touch = useCoarse();
    const act = useAction();
    const [menu, setMenu] = useState(null);
    const here = members.filter((m) => !m.left_at);
    const on = here.filter((m) => online.has(m.id)).sort(byName);
    const off = here.filter((m) => !online.has(m.id)).sort(byName);
    function items(m) {
        const setRole = (next) => act.run(() => api.put(`/api/dens/${denID}/members/${m.id}/role`, { role: next }));
        const run = {
            profile: () => onProfile(m),
            edit: card.onEdit,
            notes: () => act.run(card.onNotes),
            message: () => act.run(() => card.onMessage(m)),
            compare: () => card.onCompare(m),
            promote: () => setRole('moderator'),
            demote: () => setRole('member'),
            remove: () => card.onRemove(m),
        };
        const self = m.id === card.me.id;
        const kept = card.identities ? card.identities[m.id] || {} : null;
        return memberMenu({ self, role: card.role, member: m, kept, notes: !!card.onNotes })
            .map((item) => ({ ...item, disabled: act.busy && ['message', 'notes', 'promote', 'demote'].includes(item.key), run: run[item.key] }));
    }
    const rightClicks = (m) => (card && !touch ? {
        onContextMenu: (e) => {
            const row = e.currentTarget;
            if (!ownsMenu(e.target, getSelection(), row)) return;
            e.preventDefault();
            const r = row.getBoundingClientRect();
            setMenu({ id: m.id, at: rightClick(e, row) || { x: r.left + 8, y: r.bottom }, row });
        },
    } : {});
    const opened = menu && here.find((m) => m.id === menu.id);
    const section = (title, list, dim) =>
        list.length > 0 && (
            <div>
                <h3 class="cursor-default select-none px-2 pt-2 text-xs font-semibold uppercase text-base-content/60">
                    {title} — {list.length}
                </h3>
                <ul>
                    {list.map((m) => (
                        <li key={m.id} class="relative">
                            <HoverRow
                                as="button"
                                type="button"
                                class={`flex w-full items-center gap-2 rounded px-2 py-1 text-left hover:bg-base-300/60 pointer-coarse:min-h-11 ${dim ? 'opacity-60' : ''} ${menu?.id === m.id ? 'bg-base-300/60' : ''}`}
                                onClick={() => onProfile(m)}
                                {...rightClicks(m)}
                            >
                                <Avatar member={m} size="sm" online={online.has(m.id)} />
                                <span class="flex min-w-0 flex-1 items-center gap-1">
                                    <LongName name={m.display_name} />
                                    <RoleMark role={m.role} />
                                </span>
                            </HoverRow>
                            {opened?.id === m.id && !narrow && (
                                <PointerMenu at={menu.at} label={`Options for ${m.display_name}`} items={items(m)} back={menu.row} onClose={() => setMenu(null)} />
                            )}
                        </li>
                    ))}
                </ul>
            </div>
        );
    return (
        <nav aria-label="Members" class="flex flex-col gap-2 p-2">
            {act.error && <p role="alert" class="px-2 text-xs text-error">{act.error}</p>}
            {section('Online', on, false)}
            {section('Offline', off, true)}
            {opened && narrow && (
                <Sheet label={`Options for ${opened.display_name}`} title={<span class="font-semibold">{opened.display_name}</span>} onClose={() => setMenu(null)}>
                    {items(opened).map((a) => (
                        <SheetRow key={a.key} danger={a.danger} disabled={a.disabled} onClick={() => {
                            setMenu(null);
                            a.run();
                        }}>
                            {a.label}
                        </SheetRow>
                    ))}
                </Sheet>
            )}
        </nav>
    );
}

// Profile shows a member as a card (M17.6): their banner across its top,
// to its edges, or a band of the theme's quiet color where they have none;
// their picture across the banner's foot, ringed in the card's own color,
// so the two read as one, with actions to its right; under it their
// names, with a crown or a shield for staff, and the badges they show,
// beside the names on a card wide enough and under them on a narrow one;
// then, under a thin line, their bio as text and how long they've been in
// the den. The bio and the badges come as the den sends them with the
// card (M17.5). In a panel the bio runs its full length, which the panel
// scrolls. Its pictures play while it shows, since the member opened it
// (M15.1).
function Profile({ denID, member, me, online, panel = false, verified = false, actions, children }) {
    const [bio, setBio] = useState(null);
    const [badges, setBadges] = useState([]);
    const [error, setError] = useState('');
    // A bio usually arrives at once; the dots show only if it's slow.
    const slow = useLater(bio === null && !error, 1000);
    useEffect(() => {
        api.get(`/api/dens/${denID}/members/${member.id}`).then((p) => {
            setBio(p.bio || '');
            setBadges(p.badges || []);
        }, (e) => setError(e.message));
    }, [member.id, member.display_name]);
    const left = !!member.left_at;
    const shown = shownBadges(badges);
    const ring = panel ? 'bg-base-200' : 'bg-base-100';
    const at = panel ? '-panel' : '';
    return (
        <Shown>
            <div class="@container flex flex-col" data-profile>
                {/* In a card, a sheet on the phone layout, the banner drags it
                    down; beside a DM it's the pane's, which swipes. */}
                <div data-sheet-top={panel ? undefined : ''} class={panel ? '' : 'max-md:touch-none'}>
                    {member.banner?.id ? <Banner denID={denID} image={member.banner} edge /> : <div class="h-16 bg-base-300 @min-[20rem]:h-20" data-no-banner></div>}
                </div>
                <div class={`flex flex-col gap-3 ${panel ? 'px-3 pb-3' : 'px-4 pb-4 md:px-6 md:pb-6'}`}>
                    <div class="-mt-10 flex items-end justify-between gap-2">
                        {/* Positioned, as the banner is, so its ring paints over it. */}
                        <span class={`relative shrink-0 rounded-full p-1 ${ring}`}>
                            <Avatar member={member} size="lg" online={left ? undefined : online} dotRing={panel ? 'ring-base-200' : 'ring-base-100'} />
                        </span>
                        {actions && <div class="flex min-w-0 flex-wrap justify-end gap-2" data-profile-actions>{actions}</div>}
                    </div>
                    <div class="flex flex-col gap-3 @min-[26rem]:flex-row @min-[26rem]:items-start @min-[26rem]:justify-between">
                        <div class="flex min-w-0 flex-col items-start gap-0.5 @min-[26rem]:flex-1">
                            <p class="flex max-w-full items-center gap-0.5 text-lg font-semibold">
                                <span class="truncate">{member.display_name}</span>
                                <RoleMark role={member.role} id={`profile-role-${member.id}${at}`} />
                                {verified && <VerifiedMark id={`profile-verified-${member.id}${at}`} name={member.display_name} />}
                            </p>
                            <p class="max-w-full truncate text-sm text-base-content/70">@{member.username}</p>
                        </div>
                        {shown.length > 0 && (
                            <div class="flex min-w-0 flex-col gap-1 @min-[26rem]:max-w-[55%]">
                                <p class="cursor-default select-none text-xs font-semibold uppercase text-base-content/60">Badges</p>
                                <Badges denID={denID} badges={shown} panel={panel} />
                            </div>
                        )}
                    </div>
                    <div class="flex flex-col gap-2 border-t border-base-300 pt-3">
                        {slow && <span class="loading loading-dots loading-sm"></span>}
                        {bio && (
                            <div class={`break-words text-sm ${panel ? '' : 'max-h-48 overflow-y-auto'}`} data-bio>
                                <Markdown text={bio} me={me} />
                            </div>
                        )}
                        <p class="flex cursor-default select-none items-center gap-1.5 text-xs text-base-content/60" data-member-since>
                            <CalendarIcon />
                            {left ? `No longer in this den since ${day(member.left_at)}` : `Member since ${day(member.joined_at)}`}
                        </p>
                    </div>
                    <ErrorText message={error} />
                    {children}
                </div>
            </div>
        </Shown>
    );
}

// ProfilePanel shows the other member of a DM beside it, in the member
// list's place (M11), as the profile card shows them (M17.6), and from
// M16.1 whether this member verified them, with Compare codes. kept is
// what this member's Dens kept of their identity, null on a den that keeps
// none. expiry is the DM, whose time either member sets (M16.4), where the
// den keeps one.
export function ProfilePanel({ denID, member, me, online, kept, onCompare, onAddPeople, expiry }) {
    return (
        <div class="flex flex-col">
            <Profile denID={denID} member={member} me={me} online={online} panel verified={!!kept?.verified}>
                <CompareCodes member={member} kept={kept} onCompare={onCompare} />
                {/* A group of the two and those added, which leaves the DM as it
                    is (M16.3). */}
                {onAddPeople && (
                    <div>
                        <button type="button" class="btn btn-sm" data-add-people onClick={onAddPeople}>Add people</button>
                    </div>
                )}
                {expiry && <ExpirySetting denID={denID} channel={expiry} />}
            </Profile>
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

// ProfileCard shows a member, and what the viewer can do with them, with
// one button standing out (M17.6): Message, or on the member's own card
// Edit profile, with Notes beside it, which open their notes (M17.3) while
// the den has them for this Dens. The rest is under ⋯.
export function ProfileCard({ denID, member, me, role, online, kept, onClose, onMessage, onNotes, onCompare, onEdit, onRemove }) {
    const act = useAction();
    const self = member.id === me.id;
    const left = !!member.left_at;

    function setRole(next) {
        act.run(() => api.put(`/api/dens/${denID}/members/${member.id}/role`, { role: next }));
    }

    const run = {
        compare: () => onCompare(member),
        promote: () => setRole('moderator'),
        demote: () => setRole('member'),
        remove: () => onRemove(member),
    };
    const more = cardMenu({ self, role, member, kept }).map((item) => ({ ...item, disabled: act.busy && item.key !== 'compare', run: run[item.key] }));
    const actions = self ? (
        <>
            <button type="button" class="btn btn-primary btn-sm" onClick={onEdit}>Edit profile</button>
            {onNotes && <button type="button" class="btn btn-sm" data-own-notes onClick={onNotes}>Notes</button>}
        </>
    ) : (
        (!left || more.length > 0) && (
            <>
                {!left && <button type="button" class="btn btn-primary btn-sm" onClick={() => onMessage(member)}>Message</button>}
                {more.length > 0 && <MoreMenu items={more} />}
            </>
        )
    );

    return (
        <Dialog title="Profile" bare onClose={onClose}>
            <Profile denID={denID} member={member} me={me} online={online} verified={!self && !!kept?.verified} actions={actions}>
                <ErrorText message={act.error} />
            </Profile>
        </Dialog>
    );
}

// MoreMenu is a profile card's ⋯ and the menu of the rest of what the
// member may do (M17.6), as a message's is: under the ⋯, or over it where
// the card has no room below, arrow keys moving through it, and Escape, Tab
// or a press elsewhere closing it; on the phone layout, narrow, a sheet.
// Each item has a label, a run, and maybe danger or disabled.
function MoreMenu({ items }) {
    const narrow = useNarrow();
    const [open, setOpen] = useState(false);
    const [up, setUp] = useState(false);
    const box = useRef(null);
    const button = useRef(null);
    const list = useRef(null);
    const dropdown = open && !narrow;
    useLayoutEffect(() => {
        if (!dropdown || !list.current) return;
        const view = box.current.closest('[role=dialog]')?.getBoundingClientRect();
        const at = box.current.getBoundingClientRect();
        const below = view ? view.bottom - at.bottom : Infinity;
        setUp(below < list.current.offsetHeight + 8 && at.top - view.top > below);
    }, [dropdown]);
    useEffect(() => {
        if (!dropdown) return undefined;
        list.current?.querySelector('[role="menuitem"]:not(:disabled)')?.focus();
        const away = (e) => !box.current?.contains(e.target) && setOpen(false);
        document.addEventListener('pointerdown', away);
        return () => document.removeEventListener('pointerdown', away);
    }, [dropdown]);
    // close gives the focus back to the ⋯, unless what was picked takes it.
    function close() {
        setOpen(false);
        button.current?.focus();
    }
    function keys(e) {
        const all = [...list.current.querySelectorAll('[role="menuitem"]:not(:disabled)')];
        const at = all.indexOf(document.activeElement);
        const to = { ArrowDown: at + 1, ArrowUp: at - 1, Home: 0, End: all.length - 1 }[e.key];
        if (to !== undefined && all.length) {
            e.preventDefault();
            all[(to + all.length) % all.length].focus();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            close();
        } else if (e.key === 'Tab') {
            setOpen(false);
        }
    }
    return (
        <div ref={box} class="relative">
            <button ref={button} type="button" class="btn btn-sm btn-square" aria-label="More actions" title="More" aria-haspopup="menu"
                aria-expanded={open} data-profile-more onClick={() => setOpen(!open)}
                onKeyDown={(e) => {
                    if (e.key !== 'ArrowDown' || open) return;
                    e.preventDefault();
                    setOpen(true);
                }}>
                ⋯
            </button>
            {dropdown && (
                <ul ref={list} role="menu" aria-label="More actions" onKeyDown={keys}
                    class={`menu absolute right-0 z-20 w-52 rounded-box bg-base-100 p-1 shadow ${up ? 'bottom-full mb-1' : 'top-full mt-1'}`}>
                    {items.map((a) => (
                        <li key={a.key} role="none">
                            <button type="button" role="menuitem" tabIndex={-1} disabled={a.disabled} class={`pointer-coarse:min-h-11 ${a.danger ? 'text-error' : ''}`}
                                onClick={() => {
                                    close();
                                    a.run();
                                }}>
                                {a.label}
                            </button>
                        </li>
                    ))}
                </ul>
            )}
            {open && narrow && (
                <Sheet label="More actions" onClose={() => setOpen(false)}>
                    {items.map((a) => (
                        <SheetRow key={a.key} danger={a.danger} disabled={a.disabled} onClick={() => {
                            setOpen(false);
                            a.run();
                        }}>
                            {a.label}
                        </SheetRow>
                    ))}
                </Sheet>
            )}
        </div>
    );
}

// Badges shows the badges a member earned and shows, latest first,
// wrapping into as many rows as they take, each with a tip saying what
// it's called, and what event it was for and when (M17.5). Each takes a
// finger's 44 pixels on a touch screen.
export function Badges({ denID, badges, panel = false }) {
    const tz = zone();
    return (
        <ul class="flex flex-wrap gap-1" aria-label="Badges" data-badges>
            {badges.map((b) => {
                const tip = badgeTip(b, tz);
                return (
                    <li key={b.event_id} class="flex" data-badge={b.event_id}>
                        <Tip id={`badge-${b.event_id}${panel ? '-panel' : ''}`} side="top" label={`Badge: ${b.title}`}
                            trigger={<BadgePicture denID={denID} image={b.image} size="h-8 w-8 pointer-coarse:h-9 pointer-coarse:w-9" />}>
                            <span class="block break-words font-semibold">{tip.title}</span>
                            <span class="block break-words text-xs opacity-80">{tip.line}</span>
                        </Tip>
                    </li>
                );
            })}
        </ul>
    );
}

// BadgePicture shows a badge's picture, square, at a size of the caller's.
// An animated one plays as an emoji does (M15.1).
export function BadgePicture({ denID, image, size = 'h-8 w-8' }) {
    const how = usePicture(!!image.animated);
    const fit = 'h-full w-full select-none rounded object-cover';
    return (
        <span class={`relative inline-block shrink-0 ${size}`}>
            <img src={how === 'file' ? fileURL(denID, image.id) : thumbURL(denID, image.id)} alt="" draggable={false} class={fit} />
            {how === 'hover' && <img src={fileURL(denID, image.id)} alt="" draggable={false} class={`absolute inset-0 ${fit}`} />}
        </span>
    );
}

// BadgeToggle is one of the member's badges in Edit profile (M17.6): its
// picture, with an eye at its top right, open while the badge shows on
// their card and struck while it's hidden, which a click or a tap turns.
// Its tip, on a mouse's hover, says what it is.
export function BadgeToggle({ denID, badge, busy, onToggle }) {
    const tip = badgeTip(badge, zone());
    return (
        <button type="button" class={`relative rounded-box p-1.5 hover:bg-base-200 ${badge.hidden ? 'opacity-60' : ''}`} aria-pressed={!badge.hidden}
            aria-label={`Show ${badge.title} on your profile`} disabled={busy} data-badge-toggle={badge.event_id} onClick={() => onToggle(!badge.hidden)}>
            <HoverTip side="top" tip={<><span class="block break-words font-semibold">{tip.title}</span><span class="block break-words text-xs opacity-80">{tip.line}</span></>}>
                <BadgePicture denID={denID} image={badge.image} size="h-10 w-10" />
            </HoverTip>
            <span class="absolute right-0 top-0 rounded-full bg-base-100 p-0.5 text-base-content/80 shadow" data-eye={badge.hidden ? 'off' : 'on'}>
                <EyeIcon off={badge.hidden} />
            </span>
        </button>
    );
}

// Banner shows the wide picture on a profile, or an event's (M17.4), three
// times as wide as it is high, at its final size before it loads. An
// animated one plays as an avatar does (M15.1), which in a profile is
// while it shows. One to its box's edges, edge, takes the box's corners.
export function Banner({ denID, image, edge = false }) {
    const how = usePicture(!!image.animated);
    const cover = 'h-full w-full select-none object-cover';
    return (
        <div class={`relative aspect-[3/1] w-full overflow-hidden bg-base-300 ${edge ? '' : 'rounded'}`}>
            <img src={how === 'file' ? fileURL(denID, image.id) : thumbURL(denID, image.id)} alt="" draggable={false} class={cover} />
            {how === 'hover' && <img src={fileURL(denID, image.id)} alt="" draggable={false} class={`absolute inset-0 ${cover}`} />}
        </div>
    );
}

// EditProfile changes the member's own display name, bio and pictures in
// this den, and which of their badges their card shows (M17.6), which
// takes at once, as a picture does.
export function EditProfile({ denID, me, limits, onClose }) {
    const [name, setName] = useState(me.display_name);
    const [bio, setBio] = useState(null);
    const [badges, setBadges] = useState([]);
    const [cropping, setCropping] = useState(null);
    const [storage, setStorage] = useState(null);
    const save = useAction();
    const picture = useAction();
    const turn = useAction();
    const pickAvatar = useRef(null);
    const pickBanner = useRef(null);
    useEffect(() => {
        api.get(`/api/dens/${denID}/members/${me.id}`).then((p) => {
            setBio(p.bio || '');
            setBadges(p.badges || []);
        }, () => setBio(''));
        api.get(`/api/dens/${denID}/storage`).then(setStorage, () => {});
    }, [me.id]);

    // toggle hides a badge from the member's card, or shows it again.
    function toggle(badge, hidden) {
        turn.run(async () => {
            await api.put(`/api/dens/${denID}/me/badges/${badge.event_id}`, { hidden });
            setBadges((list) => list.map((b) => (b.event_id === badge.event_id ? { ...b, hidden } : b)));
        });
    }
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
                    {badges.length > 0 && (
                        <fieldset class="fieldset">
                            <legend class="fieldset-legend">Badges</legend>
                            <ul class="flex flex-wrap gap-1" data-edit-badges>
                                {badges.map((b) => (
                                    <li key={b.event_id} class="flex">
                                        <BadgeToggle denID={denID} badge={b} busy={turn.busy} onToggle={(hidden) => toggle(b, hidden)} />
                                    </li>
                                ))}
                            </ul>
                            <p class="label whitespace-normal">Your profile shows those with the eye open.</p>
                            <ErrorText message={turn.error} />
                        </fieldset>
                    )}
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
