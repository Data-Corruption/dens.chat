// Events (M17.4): the Events row at the top of a den's channel list, the
// pane it opens, which lists the den's events live first and then by their
// start, each as a card, and the form that makes or changes one. A member
// marks Interested on a card, which the den counts for everyone and tells
// them alone as the event starts; its maker, and staff above them, change
// it, cancel it while it's to come, and end it early while it's live, or
// move its end. What they are, without a screen, lives in denevents.js.
//
// From M17.5 an event's Copy link gives a link that shows the event's card
// under the message it's in, Interested and all, and staff attach a badge
// to an event in a voice channel, which the card shows.

import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { HoverRow } from './animations.js';
import { copyText } from './clipboard.js';
import { ErrorText, Field, SubmitButton, TextInput, Tip, useAction, useNotifications } from './components.jsx';
import { CropDialog } from './crop.jsx';
import {
    MAX_BADGE_TITLE, badgeProblem, badgeRequest, canMake, cardWhen, current, eventRequest, formOf, formProblem, interestedText, lengthsFor,
    mayChange, newForm, ordered, rowMark, whenText, willHaveBadge, zone,
} from './denevents.js';
import { useLinkedEvent, markRead } from './eventlinks.jsx';
import { upload, uploadAnimated } from './files.jsx';
import { ShieldIcon, SpeakerIcon } from './icons.jsx';
import { eventLinkTo } from './links.js';
import { Dialog } from './manage.jsx';
import { Markdown, eventsIn } from './markdown.jsx';
import { BadgePicture, Banner, isStaff } from './people.jsx';
import { NO_CALLS, canCall } from './platform.js';
import { joinCall } from './voice.js';
import { askToNotify, permission, tell } from './notify.js';

// wanted is the event a notification's tap, or an address, asks its den's
// pane to bring into sight, numbered so the same one asked again shows
// again; watchers are the panes open, which take it.
let wanted = null;
let asked = 0;
const watchers = new Set();

// showEvent has the events pane of a den bring an event into sight, now or
// as it next opens.
export function showEvent(denID, eventID) {
    wanted = { denID, eventID, n: ++asked };
    watchers.forEach((fn) => fn());
}

// useWanted is the event a den's pane is asked to bring into sight.
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

// EventsIcon marks the den's events: a calendar's page.
export function EventsIcon({ size = 'h-4 w-4' }) {
    return (
        <svg viewBox="0 0 16 16" class={size} fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <rect x="2.5" y="3.5" width="11" height="10" rx="1.5" />
            <path d="M2.5 6.5h11M5.5 2v3M10.5 2v3" />
        </svg>
    );
}

function PlaceIcon() {
    return (
        <svg viewBox="0 0 16 16" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M8 14.5s4.5-4.2 4.5-8a4.5 4.5 0 0 0-9 0c0 3.8 4.5 8 4.5 8z" />
            <circle cx="8" cy="6.5" r="1.5" />
        </svg>
    );
}

// EventsRow is the den's events at the top of its channel list: Live while
// one runs, else how many are to come; open says the pane shows them.
export function EventsRow({ events, open, onOpen }) {
    const mark = rowMark(current(events, Date.now()));
    return (
        <HoverRow as="li" class={`relative flex min-h-8 select-none items-center gap-1 rounded pr-1 pointer-coarse:min-h-11 ${open ? 'bg-base-300' : 'hover:bg-base-300/60'}`}
            data-events-row>
            <button type="button" class="flex min-w-0 flex-1 items-center gap-2 self-stretch rounded px-2 text-left after:absolute after:inset-0 after:rounded"
                aria-label={mark === 'Live' ? 'Events, one live now' : mark ? `Events, ${mark} to come` : 'Events'} onClick={onOpen}>
                <span class="text-base-content/50"><EventsIcon /></span>
                <span class={`truncate ${open ? '' : 'text-base-content/85'}`}>Events</span>
            </button>
            {mark === 'Live' && <span class="badge badge-success badge-xs" data-events-live>Live</span>}
            {mark && mark !== 'Live' && <span class="badge badge-ghost badge-xs" data-events-count>{mark}</span>}
        </HoverRow>
    );
}

// EventsPane lists the den's events in the channel's place: those live
// first, then the rest by their start, each a card, with + Event for a
// member who may make one. It brings into sight the event a notification's
// tap asks for.
export function EventsPane({ denID, view, members, onList, onProfile }) {
    const [dialog, setDialog] = useState(null);
    const focus = useWanted(denID);
    const [now, setNow] = useState(Date.now);
    const tz = zone();
    // The pane's times say Today and Live by the clock, which moves on.
    useEffect(() => {
        const tick = setInterval(() => setNow(Date.now()), 30_000);
        return () => clearInterval(tick);
    }, []);
    const events = ordered(current(view.events, now));
    const voice = view.channels.filter((c) => c.kind === 'voice');
    const making = canMake(view) && view.state !== 'removed' && view.state !== 'revoked';
    return (
        <>
            <header class="flex min-h-12 items-center gap-2 border-b border-base-300 px-3 py-2">
                <button type="button" class="btn btn-ghost btn-sm btn-square md:hidden" aria-label="Channels" onClick={onList}>
                    <svg viewBox="0 0 16 16" class="h-5 w-5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true">
                        <path d="M2.5 4h11M2.5 8h11M2.5 12h11" />
                    </svg>
                </button>
                <span class="text-base-content/50"><EventsIcon size="h-5 w-5" /></span>
                <h2 class="min-w-0 flex-1 truncate font-semibold" data-events-header>Events</h2>
                {making && (
                    <button type="button" class="btn btn-sm btn-primary" data-new-event onClick={() => setDialog({ kind: 'make' })}>+ Event</button>
                )}
            </header>
            <div class="min-h-0 flex-1 overflow-y-auto">
                <div class="mx-auto flex max-w-2xl flex-col gap-3 p-3">
                    {events.length === 0 && <p class="p-3 text-base-content/70">No events to come.</p>}
                    {events.map((e) => (
                        <EventCard key={e.id} denID={denID} view={view} e={e} members={members} now={now} tz={tz}
                            focused={focus?.eventID === e.id ? focus.n : 0}
                            voice={voice} onProfile={onProfile} onDialog={setDialog} />
                    ))}
                </div>
            </div>
            {dialog?.kind === 'make' && <EventDialog denID={denID} view={view} onClose={() => setDialog(null)} />}
            {dialog?.kind === 'edit' && <EventDialog denID={denID} view={view} event={dialog.event} onClose={() => setDialog(null)} />}
            {dialog?.kind === 'cancel' && <EndDialog denID={denID} event={dialog.event} cancel onClose={() => setDialog(null)} />}
            {dialog?.kind === 'end' && <EndDialog denID={denID} event={dialog.event} onClose={() => setDialog(null)} />}
        </>
    );
}

// EventCard shows an event: its banner, name and whether it's live, when
// and where, its description, who made it, how many are interested, and
// the member's own mark; and to whoever may change it, Edit, and Cancel or
// End now.
function EventCard({ denID, view, e, members, now, tz, focused, voice, onProfile, onDialog }) {
    const ref = useRef(null);
    const mark = useAction();
    const linking = useAction();
    const [copied, setCopied] = useState(false);
    const copiedTimer = useRef(null);
    useEffect(() => () => clearTimeout(copiedTimer.current), []);
    // Copy link gives the event's link, which shows its card where it's
    // posted (M17.5).
    function copyLink() {
        linking.run(async () => {
            await copyText(eventLinkTo(view.url, denID, e.id));
            setCopied(true);
            clearTimeout(copiedTimer.current);
            copiedTimer.current = setTimeout(() => setCopied(false), 1500);
        });
    }
    const channel = e.channel_id ? voice.find((c) => c.id === e.channel_id) : null;
    const maker = members.get(e.made_by);
    // A den that no longer takes this device takes no mark or change.
    const gone = view.state === 'removed' || view.state === 'revoked';
    const changes = mayChange(view, e, members) && !gone;
    const notifications = useNotifications();
    const [lit, setLit] = useState(false);
    useEffect(() => {
        if (!focused || !ref.current) return undefined;
        ref.current.scrollIntoView({ block: 'center' });
        setLit(true);
        const off = setTimeout(() => setLit(false), 2000);
        return () => clearTimeout(off);
    }, [focused]);
    function interested(on) {
        askOnInterest(on, notifications);
        mark.run(() => (on ? api.put : api.del)(`/api/dens/${denID}/events/${e.id}/interest`));
    }
    return (
        <article ref={ref} class={`card overflow-hidden border border-base-300 bg-base-100 ${lit ? 'ring-2 ring-primary' : ''}`} data-event={e.id}
            aria-label={e.name}>
            {e.banner?.id && <Banner denID={denID} image={e.banner} />}
            <div class="card-body gap-2 p-4">
                <div class="flex flex-wrap items-center gap-2">
                    <h3 class="min-w-0 break-words text-lg font-semibold">{e.name}</h3>
                    {e.live && <span class="badge badge-success badge-sm" data-live>Live</span>}
                </div>
                <p class="text-sm text-base-content/70" data-when>{whenText(e, now, tz)}</p>
                <div class="flex flex-wrap items-center gap-2 text-sm">
                    {e.channel_id ? (
                        <>
                            <span class="flex min-w-0 items-center gap-1"><span class="text-base-content/50"><SpeakerIcon size="h-4 w-4" /></span>
                                <span class="truncate">{channel?.name || 'A voice channel'}</span></span>
                            {channel?.staff_only && (
                                <Tip id={`event-staff-${e.id}`} label="Staff only" trigger={<ShieldIcon />}>
                                    Staff only: only moderators and the owner can see it.
                                </Tip>
                            )}
                            {e.live && channel && (
                                <button type="button" class="btn btn-xs" disabled={!canCall()} title={canCall() ? undefined : NO_CALLS}
                                    onClick={() => joinCall(denID, channel.id, { den: view.name, channel: channel.name })}>Join</button>
                            )}
                        </>
                    ) : (
                        <span class="flex min-w-0 items-center gap-1"><span class="text-base-content/50"><PlaceIcon /></span>
                            <span class="break-words">{e.place}</span></span>
                    )}
                </div>
                {e.description && <div class="text-sm" data-description><Markdown text={e.description} me={view.me} /></div>}
                {e.badge && <EventBadge denID={denID} badge={e.badge} id={`event-badge-${e.id}`} />}
                <p class="text-xs text-base-content/60">
                    By{' '}
                    {maker ? <button type="button" class="link-hover" onClick={() => onProfile(maker)}>{maker.display_name}</button> : 'a former member'}
                </p>
                <div class="flex flex-wrap items-center gap-2">
                    <button type="button" class={`btn btn-sm ${e.mine ? 'btn-primary' : ''}`} aria-pressed={!!e.mine} disabled={mark.busy || gone}
                        data-interested onClick={() => interested(!e.mine)}>
                        {e.mine ? '★ Interested' : '☆ Interested'}
                    </button>
                    <span class="text-sm text-base-content/70" data-interest-count>{interestedText(e.interested)}</span>
                    <span class="ml-auto flex flex-wrap justify-end gap-1">
                        {view.url && (
                            <button type="button" class="btn btn-ghost btn-sm" data-copy-event-link onClick={copyLink}>{copied ? 'Copied' : 'Copy link'}</button>
                        )}
                        {changes && <button type="button" class="btn btn-ghost btn-sm" onClick={() => onDialog({ kind: 'edit', event: e })}>Edit</button>}
                        {changes && (e.live ? (
                            <button type="button" class="btn btn-ghost btn-sm" onClick={() => onDialog({ kind: 'end', event: e })}>End now</button>
                        ) : (
                            <button type="button" class="btn btn-ghost btn-sm text-error" onClick={() => onDialog({ kind: 'cancel', event: e })}>Cancel…</button>
                        ))}
                    </span>
                </div>
                <ErrorText message={mark.error || linking.error} />
            </div>
        </article>
    );
}

// askOnInterest asks the browser to show notifications as a member marks
// Interested, where it hasn't asked and events notify, since a browser asks
// only in answer to a click.
function askOnInterest(on, notifications) {
    if (on && notifications && !notifications.eventsOff && permission() === 'default') askToNotify().then(() => tell());
}

// EventBadge shows an event's badge on its card (M17.5): its picture and
// title, and how it's earned.
function EventBadge({ denID, badge, id }) {
    return (
        <div class="flex min-w-0 items-center gap-2 text-sm" data-event-badge>
            <BadgePicture denID={denID} image={badge.image} />
            <span class="min-w-0 break-words">{badge.title}</span>
            <Tip id={id} label="About the badge">A minute in its call while it's live earns it.</Tip>
        </div>
    );
}

// EventCards shows the cards of the events a message's links point to,
// after its files (M17.5).
export function EventCards({ text }) {
    const linked = useMemo(() => eventsIn(text), [text]);
    if (!linked.length) return null;
    return (
        <div class="mt-1 flex flex-col gap-1">
            {linked.map((link) => <EventLinkCard key={`${link.den}/${link.event}`} link={link} />)}
        </div>
    );
}

// EventLinkCard is the card of an event a link points to (M17.5): its
// banner, its name, which opens it in its den's events, Live or Ended,
// when, in the member's time zone, where, its badge, and, until it ends,
// how many are interested, with Interested. A link to an event in a den the
// member hasn't joined, or one they can't see, gets no card.
function EventLinkCard({ link }) {
    const { links, den, event, state } = useLinkedEvent(link);
    const mark = useAction();
    const notifications = useNotifications();
    const [now, setNow] = useState(Date.now);
    useEffect(() => {
        const tick = setInterval(() => setNow(Date.now()), 30_000);
        return () => clearInterval(tick);
    }, []);
    if (!links || !event || (state !== 'shown' && state !== 'ended')) return null;
    const ended = state === 'ended';
    const here = den.den_id === links.denID;
    const view = here ? links.view : null;
    const channel = event.channel_id && view ? view.channels.find((c) => c.id === event.channel_id) : null;
    const gone = view && (view.state === 'removed' || view.state === 'revoked');
    function interested(on) {
        askOnInterest(on, notifications);
        mark.run(async () => markRead(den.den_id, event.id, await (on ? api.put : api.del)(`/api/dens/${den.den_id}/events/${event.id}/interest`)));
    }
    return (
        <article class="card max-w-md overflow-hidden border border-base-300 bg-base-100" data-event-card={event.id} aria-label={event.name}>
            {event.banner?.id && <Banner denID={den.den_id} image={event.banner} />}
            <div class="card-body gap-1 p-3">
                {!here && <p class="truncate text-xs text-base-content/60">{den.name}</p>}
                <div class="flex flex-wrap items-center gap-2">
                    {ended ? (
                        <h4 class="min-w-0 break-words font-semibold">{event.name}</h4>
                    ) : (
                        <button type="button" class="link-hover min-h-6 min-w-0 break-words text-left font-semibold" onClick={() => links.openEvent(den.den_id, event.id)}>
                            {event.name}
                        </button>
                    )}
                    {event.live && !ended && <span class="badge badge-success badge-sm" data-live>Live</span>}
                    {ended && <span class="badge badge-ghost badge-sm" data-ended>Ended</span>}
                </div>
                <p class="text-sm text-base-content/70" data-when>{cardWhen(event, ended, now, zone())}</p>
                {(event.channel_id || event.place) && (
                    <p class="flex min-w-0 items-center gap-1 text-sm">
                        <span class="text-base-content/50">{event.channel_id ? <SpeakerIcon size="h-4 w-4" /> : <PlaceIcon />}</span>
                        <span class={event.channel_id ? 'truncate' : 'break-words'}>{event.channel_id ? channel?.name || 'A voice channel' : event.place}</span>
                    </p>
                )}
                {event.badge && <EventBadge denID={den.den_id} badge={event.badge} id={`card-badge-${den.den_id}-${event.id}`} />}
                {!ended && (
                    <div class="flex flex-wrap items-center gap-2">
                        <button type="button" class={`btn btn-sm ${event.mine ? 'btn-primary' : ''}`} aria-pressed={!!event.mine} disabled={mark.busy || gone}
                            data-interested onClick={() => interested(!event.mine)}>
                            {event.mine ? '★ Interested' : '☆ Interested'}
                        </button>
                        <span class="text-sm text-base-content/70" data-interest-count>{interestedText(event.interested)}</span>
                    </div>
                )}
                <ErrorText message={mark.error} />
            </div>
        </article>
    );
}

// EndDialog asks before an event is canceled, which deletes it, or ended
// now.
function EndDialog({ denID, event, cancel, onClose }) {
    const act = useAction();
    function go() {
        act.run(async () => {
            if (cancel) await api.del(`/api/dens/${denID}/events/${event.id}`);
            else await api.post(`/api/dens/${denID}/events/${event.id}/end`);
            onClose();
        });
    }
    return (
        <Dialog title={cancel ? 'Cancel the event?' : 'End the event now?'} onClose={onClose}>
            <p class="break-words">{cancel ? `${event.name} goes from the den's events.` : `${event.name} ends for everyone now.`}</p>
            <ErrorText message={act.error} />
            <div class="flex justify-end gap-2">
                <button type="button" class="btn btn-ghost" onClick={onClose}>Keep it</button>
                <button type="button" class={`btn ${cancel ? 'btn-error' : 'btn-primary'}`} disabled={act.busy} onClick={go}>
                    {act.busy && <span class="loading loading-spinner loading-sm"></span>}
                    {cancel ? 'Cancel the event' : 'End now'}
                </button>
            </div>
        </Dialog>
    );
}

// EventDialog makes an event, or changes one: its name, description and
// banner, cut three times as wide as it's high; its start, in the member's
// time zone, which a live event keeps; how long it lasts; and where it is,
// one of the den's voice channels or a line of text. Staff attach a badge
// to one in a voice channel (M17.5): a square picture, which an emoji's
// size takes, and a title.
export function EventDialog({ denID, view, event, onClose }) {
    const tz = zone();
    const voice = view.channels.filter((c) => c.kind === 'voice');
    const [form, setForm] = useState(() => (event ? formOf(event, tz) : newForm(Date.now(), tz, voice[0]?.id)));
    // banner is the banner the event will have: as it is, a new upload, or
    // none; undefined leaves it as it is. badge is its badge's picture, the
    // same way.
    const [banner, setBanner] = useState(undefined);
    const [badge, setBadge] = useState(undefined);
    const [cropping, setCropping] = useState(null);
    const save = useAction();
    const picture = useAction();
    const pick = useRef(null);
    const pickBadge = useRef(null);
    const set = (field) => (value) => setForm((f) => ({ ...f, [field]: value }));
    const live = !!event?.live;
    const staff = isStaff(view.role);
    const shown = banner === undefined ? event?.banner : banner;
    const shownBadge = willHaveBadge(form, event, badge) ? (badge === undefined ? event?.badge?.image : badge) : null;

    // cropped takes a picture the crop dialog framed: the banner's, or the
    // badge's, which goes as an emoji's would (M17.5).
    function cropped(blob, rect) {
        const forBadge = cropping.shape === 'badge';
        picture.run(async () => {
            const name = `${forBadge ? 'badge' : 'banner'}.${blob.type === 'image/webp' ? 'webp' : 'png'}`;
            const up = rect ? await uploadAnimated(denID, forBadge ? 'emoji' : 'banner', blob, rect) : await upload(denID, '', blob, name).done;
            const image = { id: up.id, width: up.width, height: up.height, animated: up.animated };
            if (forBadge) setBadge(image);
            else setBanner(image);
            setCropping(null);
        });
    }

    function submit(e) {
        e.preventDefault();
        save.run(async () => {
            const why = formProblem(form, Date.now(), tz, event) || (staff ? badgeProblem(form, event, badge) : '');
            if (why) throw new Error(why);
            const req = eventRequest(form, tz, event);
            if (banner !== undefined) req.banner = banner?.id || '';
            if (staff) Object.assign(req, badgeRequest(form, event, badge));
            if (event) await api.patch(`/api/dens/${denID}/events/${event.id}`, req);
            else await api.post(`/api/dens/${denID}/events`, req);
            onClose();
        });
    }

    return (
        <Dialog title={event ? 'Edit the event' : 'New event'} onClose={onClose}>
            <form class="flex flex-col gap-2" onSubmit={submit} data-event-form>
                <Field label="Name">
                    <TextInput value={form.name} onInput={set('name')} required data-event-name />
                </Field>
                <Field label="Description" hint="Optional. The same formatting as messages works here.">
                    <textarea class="textarea w-full" rows="3" value={form.description} onInput={(e) => set('description')(e.currentTarget.value)}
                        data-event-description></textarea>
                </Field>
                <fieldset class="fieldset">
                    <legend class="fieldset-legend">Banner</legend>
                    {shown?.id && <Banner denID={denID} image={shown} />}
                    <div class="flex flex-wrap gap-2">
                        <button type="button" class="btn btn-sm" disabled={picture.busy} onClick={() => pick.current?.click()}>
                            {shown?.id ? 'Change' : 'Add a banner'}
                        </button>
                        {shown?.id && <button type="button" class="btn btn-ghost btn-sm" disabled={picture.busy} onClick={() => setBanner(null)}>Remove</button>}
                    </div>
                    <input ref={pick} type="file" accept="image/*" class="hidden" data-picture="event-banner" onChange={(e) => {
                        const file = e.currentTarget.files[0];
                        e.currentTarget.value = '';
                        if (file) {
                            picture.setError('');
                            setCropping({ file, shape: 'event' });
                        }
                    }} />
                    {!cropping && <ErrorText message={picture.error} />}
                </fieldset>
                <Field label="Starts" hint={live ? 'It started; its start stays.' : 'In your time zone.'}>
                    <div class="flex flex-wrap gap-2">
                        <input type="date" class="input min-w-0 flex-1 basis-36" value={form.date} disabled={live} required onInput={(e) => set('date')(e.currentTarget.value)}
                            aria-label="Day" data-event-date />
                        <input type="time" class="input min-w-0 flex-1 basis-36" value={form.time} disabled={live} required onInput={(e) => set('time')(e.currentTarget.value)}
                            aria-label="Time" data-event-time />
                    </div>
                </Field>
                <Field label="Lasts">
                    <select class="select w-full" value={form.minutes} onChange={(e) => set('minutes')(Number(e.currentTarget.value))} data-event-length>
                        {lengthsFor(event, Date.now()).map(([m, label]) => <option key={m} value={m}>{label}</option>)}
                    </select>
                </Field>
                <fieldset class="fieldset">
                    <legend class="fieldset-legend">Where</legend>
                    <div class="flex flex-wrap gap-x-4 gap-y-1">
                        <label class="flex min-h-8 cursor-pointer items-center gap-2 pointer-coarse:min-h-11">
                            <input type="radio" class="radio radio-sm" name="where" checked={form.where === 'channel'} disabled={voice.length === 0}
                                onChange={() => set('where')('channel')} />
                            A voice channel
                        </label>
                        <label class="flex min-h-8 cursor-pointer items-center gap-2 pointer-coarse:min-h-11">
                            <input type="radio" class="radio radio-sm" name="where" checked={form.where === 'place'} onChange={() => set('where')('place')} />
                            Somewhere else
                        </label>
                    </div>
                    {form.where === 'channel' ? (
                        <select class="select w-full" value={form.channel} onChange={(e) => set('channel')(e.currentTarget.value)} aria-label="Voice channel"
                            data-event-channel>
                            {voice.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
                        </select>
                    ) : (
                        <TextInput value={form.place} onInput={set('place')} placeholder="Where it is" aria-label="Place" data-event-place />
                    )}
                </fieldset>
                {staff && form.where === 'channel' && (
                    <fieldset class="fieldset" data-event-badge-form>
                        <legend class="fieldset-legend">Badge</legend>
                        {shownBadge?.id ? (
                            <div class="flex items-center gap-2">
                                <BadgePicture denID={denID} image={shownBadge} size="h-12 w-12" />
                                <TextInput value={form.badgeTitle} onInput={set('badgeTitle')} placeholder="Its title" aria-label="The badge's title"
                                    data-badge-title />
                            </div>
                        ) : (
                            <p class="label whitespace-normal">For whoever's in its call a minute while it's live.</p>
                        )}
                        <div class="flex flex-wrap gap-2">
                            <button type="button" class="btn btn-sm" disabled={picture.busy} data-add-badge onClick={() => pickBadge.current?.click()}>
                                {shownBadge?.id ? 'Change picture' : 'Add a badge'}
                            </button>
                            {shownBadge?.id && (
                                <button type="button" class="btn btn-ghost btn-sm" disabled={picture.busy} onClick={() => setBadge(null)}>Remove</button>
                            )}
                        </div>
                        <input ref={pickBadge} type="file" accept="image/*" class="hidden" data-picture="event-badge" onChange={(e) => {
                            const file = e.currentTarget.files[0];
                            e.currentTarget.value = '';
                            if (file) {
                                picture.setError('');
                                setCropping({ file, shape: 'badge' });
                            }
                        }} />
                        {!cropping && <ErrorText message={picture.error} />}
                    </fieldset>
                )}
                <ErrorText message={save.error} />
                <div>
                    <SubmitButton busy={save.busy || picture.busy}>{event ? 'Save' : 'Make the event'}</SubmitButton>
                </div>
            </form>
            {cropping && (
                <CropDialog file={cropping.file} shape={cropping.shape} busy={picture.busy} error={picture.error} onCrop={cropped}
                    onClose={() => setCropping(null)} />
            )}
        </Dialog>
    );
}
