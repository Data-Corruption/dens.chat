// Events (M17.4): the Events row at the top of a den's channel list, the
// pane it opens, which lists the den's events live first and then by their
// start, each as a card, and the form that makes or changes one. A member
// marks Interested on a card, which the den counts for everyone and tells
// them alone as the event starts; its maker, and staff above them, change
// it, cancel it while it's to come, and end it early while it's live, or
// move its end. What they are, without a screen, lives in denevents.js.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { HoverRow } from './animations.js';
import { ErrorText, Field, SubmitButton, TextInput, Tip, useAction, useNotifications } from './components.jsx';
import { CropDialog } from './crop.jsx';
import {
    canMake, current, eventRequest, formOf, formProblem, interestedText, lengthsFor, mayChange, newForm, ordered, rowMark, whenText, zone,
} from './denevents.js';
import { upload, uploadAnimated } from './files.jsx';
import { ShieldIcon, SpeakerIcon } from './icons.jsx';
import { Dialog } from './manage.jsx';
import { Markdown } from './markdown.jsx';
import { Banner } from './people.jsx';
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
    // Marking Interested asks the browser to show notifications, where it
    // hasn't asked and events notify, since a browser asks only in answer
    // to a click.
    function interested(on) {
        if (on && notifications && !notifications.eventsOff && permission() === 'default') askToNotify().then(() => tell());
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
                    {changes && (
                        <span class="ml-auto flex gap-1">
                            <button type="button" class="btn btn-ghost btn-sm" onClick={() => onDialog({ kind: 'edit', event: e })}>Edit</button>
                            {e.live ? (
                                <button type="button" class="btn btn-ghost btn-sm" onClick={() => onDialog({ kind: 'end', event: e })}>End now</button>
                            ) : (
                                <button type="button" class="btn btn-ghost btn-sm text-error" onClick={() => onDialog({ kind: 'cancel', event: e })}>Cancel…</button>
                            )}
                        </span>
                    )}
                </div>
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
// one of the den's voice channels or a line of text.
export function EventDialog({ denID, view, event, onClose }) {
    const tz = zone();
    const voice = view.channels.filter((c) => c.kind === 'voice');
    const [form, setForm] = useState(() => (event ? formOf(event, tz) : newForm(Date.now(), tz, voice[0]?.id)));
    // banner is the banner the event will have: as it is, a new upload, or
    // none; undefined leaves it as it is.
    const [banner, setBanner] = useState(undefined);
    const [cropping, setCropping] = useState(null);
    const save = useAction();
    const picture = useAction();
    const pick = useRef(null);
    const set = (field) => (value) => setForm((f) => ({ ...f, [field]: value }));
    const live = !!event?.live;
    const shown = banner === undefined ? event?.banner : banner;

    function cropped(blob, rect) {
        picture.run(async () => {
            const up = rect ? await uploadAnimated(denID, 'banner', blob, rect) : await upload(denID, '', blob, `banner.${blob.type === 'image/webp' ? 'webp' : 'png'}`).done;
            setBanner({ id: up.id, width: up.width, height: up.height, animated: up.animated });
            setCropping(null);
        });
    }

    function submit(e) {
        e.preventDefault();
        save.run(async () => {
            const why = formProblem(form, Date.now(), tz, event);
            if (why) throw new Error(why);
            const req = eventRequest(form, tz, event);
            if (banner !== undefined) req.banner = banner?.id || '';
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
                            setCropping(file);
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
                <ErrorText message={save.error} />
                <div>
                    <SubmitButton busy={save.busy || picture.busy}>{event ? 'Save' : 'Make the event'}</SubmitButton>
                </div>
            </form>
            {cropping && (
                <CropDialog file={cropping} shape="event" busy={picture.busy} error={picture.error} onCrop={cropped} onClose={() => setCropping(null)} />
            )}
        </Dialog>
    );
}
