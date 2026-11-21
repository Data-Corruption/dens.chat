// The den's soundboard: its button among the call's controls,
// which opens it, up from the list's block, where it's among the rest of
// the controls, and down from Home's box, and on the phone layout
// as a sheet from the screen's foot; and its section in Manage, where
// staff add sounds, and hear, rename or delete each.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from '../core/api.js';
import { Actions, Button, Card, ErrorText, Field, FilePick, Notice, Slider, SubmitButton, TextInput, buttonClass, useAction } from '../ui/components.jsx';
import { openManage } from '../ui/dialogs.js';
import { onEvent } from '../core/events.js';
import { upload } from '../chat/files.jsx';
import { CogIcon, PlayIcon, SoundboardIcon } from '../ui/icons.jsx';
import { useNarrow } from '../core/nav.js';
import { rank } from '../people.jsx';
import { usePopup } from '../ui/popup.js';
import { Sheet } from '../ui/sheet.jsx';
import { boardWhy, matching, nameOf, prepareSound, seconds } from './soundboard.js';
import { boardVolume, clearBoardNotice, playBoardSound, readSoundLimits, setBoardVolume, speakerID } from './voice.js';

// shows says whether the soundboard's button shows in a call: the den has
// a soundboard, on, and sounds on it, or the member is staff, who add them.
export function shows(call, den) {
    const limits = den?.soundLimits;
    if (!call || !den || den.den !== call.den || !limits || limits.sounds === 0) return false;
    return den.sounds.length > 0 || rank(den.role) > 0;
}

// WIDTH is the popup's width, in pixels.
const WIDTH = 288;

// SoundboardButton opens the soundboard of the den of the page's call from
// Home's box, down, and from the phone's bar. where says which, for the
// checks.
export function SoundboardButton({ call, den, board, sounding, where }) {
    const [open, setOpen] = useState(false);
    const anchor = useRef(null);
    if (!shows(call, den)) return null;
    return (
        <span ref={anchor} class="relative inline-flex">
            <SoundboardKey den={den} open={open} where={where} onClick={(empty) => empty || setOpen(!open)} />
            {open && <Soundboard anchor={anchor} call={call} den={den} board={board} sounding={sounding} where={where} onClose={() => setOpen(false)} />}
        </span>
    );
}

// SoundboardKey is the soundboard's button, lit while it's open. With no
// sounds in the den, staff's opens on adding them, in Manage, and onClick
// hears of it too. many makes it a finger's size on a touch screen, for
// one among others.
export function SoundboardKey({ den, open, where, many, onClick }) {
    const empty = den.sounds.length === 0;
    const label = empty ? 'Add sounds to the soundboard' : 'Soundboard';
    // The call's pane labels its foot's buttons, as its FootButton does.
    const look = where === 'pane' ? 'btn btn-ghost h-auto min-h-12 min-w-16 flex-col gap-0.5 px-2 py-1 text-xs font-normal'
        : `${buttonClass('quiet')} btn-square ${many ? 'pointer-coarse:btn-md' : ''}`;
    return (
        <button type="button" class={`${look} ${open ? 'text-success' : ''}`} aria-label={label}
            title={label} aria-haspopup="dialog" aria-expanded={!!open} data-soundboard-button={where}
            onClick={() => {
                if (empty) openManage(den.den, 'soundboard');
                onClick(empty);
            }}>
            <SoundboardIcon />
            {where === 'pane' && <span aria-hidden="true">Soundboard</span>}
        </button>
    );
}

// Soundboard is the soundboard, open: by anchor, which holds its button
// and it, up and to the right, or down from Home's box, and on the phone
// layout as a sheet from the screen's foot. What it had to say goes as it
// closes.
export function Soundboard({ anchor, call, den, board, sounding, where, onClose }) {
    const narrow = useNarrow();
    const place = usePopup(!narrow, anchor, { down: where === 'box', width: WIDTH, onClose });
    useEffect(() => clearBoardNotice, []);
    const body = <Board den={den} board={board} sounding={sounding} deafened={call.deafened} sheet={narrow} onClose={onClose} />;
    if (narrow) return <Sheet label="Soundboard" title={<span class="font-semibold">Soundboard</span>} onClose={onClose}>{body}</Sheet>;
    return place && (
        <div role="dialog" aria-label="Soundboard" data-soundboard={where} class="fixed z-40 rounded-box border border-base-300 bg-base-100 shadow-xl"
            style={{ ...place, width: `${WIDTH}px` }}>
            {body}
        </div>
    );
}

// Board is the soundboard: a search, with a cog beside it for staff that
// opens the sounds in Manage, the member's volume for it, 0 muting it, and
// the den's sounds as tiles by name, a tap on one playing it for the call,
// unless a sound plays or the member must wait, which it says.
function Board({ den, board, sounding, deafened, sheet, onClose }) {
    const [query, setQuery] = useState('');
    const [volume, setVolume] = useState(() => Math.round(boardVolume() * 100));
    const [now, setNow] = useState(() => Date.now());
    // The wait counts down while it lasts.
    useEffect(() => {
        if (board.waitUntil <= now) return undefined;
        const timer = setTimeout(() => setNow(Date.now()), 250);
        return () => clearTimeout(timer);
    }, [board.waitUntil, now]);
    useEffect(() => setNow(Date.now()), [board.waitUntil]);
    const why = boardWhy({ sounding, waitUntil: board.waitUntil, deafened }, now);
    const shown = matching(den.sounds, query);
    const staff = rank(den.role) > 0;
    const tile = `${buttonClass('secondary')} h-auto min-h-8 justify-start gap-1 py-1 text-left font-normal pointer-coarse:min-h-11`;
    return (
        <div class={`flex flex-col gap-2 ${sheet ? 'px-4 pb-2' : 'p-3'}`} data-board>
            <div class="flex items-center gap-1">
                <input type="search" class="input input-sm min-w-0 flex-1 pointer-coarse:input-md" placeholder="Search sounds" aria-label="Search sounds"
                    value={query} onInput={(e) => setQuery(e.currentTarget.value)} />
                {staff && (
                    <button type="button" class={`${buttonClass('quiet')} btn-square pointer-coarse:btn-md`} aria-label="Manage sounds" title="Manage sounds"
                        onClick={() => {
                            onClose();
                            openManage(den.den, 'soundboard');
                        }}>
                        <CogIcon />
                    </button>
                )}
            </div>
            <label class={`flex items-center gap-2 ${sheet ? 'h-12 text-sm' : 'text-xs'}`} title="How loud the soundboard plays for you">
                <span>Volume</span>
                <Slider class="min-w-0 flex-1" min="0" max="100" step="5" value={volume}
                    aria-label="Soundboard volume" aria-valuetext={volume === 0 ? 'muted' : `${volume}%`} data-board-volume
                    onInput={(e) => {
                        const v = Number(e.currentTarget.value);
                        setVolume(v);
                        setBoardVolume(v / 100);
                    }} />
                <span class="w-12 text-right tabular-nums">{volume === 0 ? 'Muted' : `${volume}%`}</span>
            </label>
            {why && <p role="status" class="text-xs text-base-content/70" data-board-why>{why}</p>}
            {board.notice && <Notice kind="warning" class="text-xs" data-board-notice>{board.notice}</Notice>}
            {shown.length === 0 ? (
                <p class="text-sm text-base-content/70">No sound matches.</p>
            ) : (
                <div class={`grid grid-cols-2 gap-1 overflow-y-auto ${sheet ? '' : 'max-h-64'}`}>
                    {shown.map((s) => (
                        <button key={s.id} type="button" role={sheet ? 'menuitem' : undefined} class={tile} disabled={!!why}
                            title={why || `Play ${s.name} for the call`} aria-label={`Play ${s.name}`} data-sound-tile={s.id}
                            onClick={() => playBoardSound(s.id)}>
                            <span class="min-w-0 flex-1 truncate">{s.name}</span>
                            <span class="shrink-0 text-xs text-base-content/50">{seconds(s.duration_ms)}</span>
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}

// ManageSoundboard is the soundboard's section in Manage, for staff: the
// den's sounds, with how many it keeps and how long each may be, a sound
// added by a file and a name, prepared by this page, and each sound to
// hear, rename or delete; and limits, the owner's limits for it, if given.
export function ManageSoundboard({ denID, limits: owners }) {
    const [view, setView] = useState(null);
    const act = useAction();
    const load = () => api.get(`/api/dens/${denID}/state`).then(setView);
    useEffect(() => {
        act.run(load);
        // Sounds others add, rename and delete show as they do.
        return onEvent((m) => {
            if (m.t === 'den' && m.d.den === denID && (m.d.reset || (m.d.events || []).some((e) => e.t.startsWith('sound.') || e.t === 'den.updated'))) {
                load().catch(() => {});
            }
        });
    }, [denID]);
    if (!view) return <>{act.error ? <ErrorText message={act.error} /> : <span class="loading loading-spinner"></span>}</>;
    const limits = readSoundLimits(view.sound_limits);
    const names = new Map((view.members || []).map((m) => [m.id, m.display_name]));
    const sounds = matching(view.sounds, '');
    return (
        <>
            <Card title="Sounds">
                <p class="text-sm text-base-content/70">Sounds anyone in a call can play for everyone in it.</p>
                {!limits ? (
                    <p class="text-sm">This den runs a version of Dens without a soundboard.</p>
                ) : (
                    <>
                        {limits.sounds === 0 ? (
                            <Notice kind="warning">The soundboard is off: nobody can play its sounds.</Notice>
                        ) : (
                            <p class="text-sm" data-board-count>
                                {sounds.length} of {limits.sounds} sounds, each up to {seconds(limits.longest * 1000)}.
                            </p>
                        )}
                        {limits.sounds > 0 && sounds.length < limits.sounds && <AddSound denID={denID} limits={limits} onAdded={() => load().catch(() => {})} />}
                    </>
                )}
                {sounds.length > 0 && (
                    <ul class="flex flex-col divide-y divide-base-300" aria-label="The den's sounds">
                        {sounds.map((s) => <SoundRow key={s.id} denID={denID} sound={s} by={names.get(s.added_by)} onChange={() => load().catch(() => {})} />)}
                    </ul>
                )}
            </Card>
            {owners}
        </>
    );
}

// AddSound adds a sound: a file of any kind the browser plays, a video's
// sound too, which this page prepares, and its name, suggested from the
// file's.
function AddSound({ denID, limits, onAdded }) {
    const [file, setFile] = useState(null);
    const [name, setName] = useState('');
    const [stage, setStage] = useState('');
    const add = useAction();
    function submit(e) {
        e.preventDefault();
        add.run(async () => {
            try {
                setStage('Preparing the sound…');
                const prepared = await prepareSound(file, limits.longest);
                setStage('Sending it…');
                const up = await upload(denID, '', prepared.file, prepared.file.name).done;
                await api.post(`/api/dens/${denID}/soundboard`, { name: name.trim() || nameOf(file.name), upload: up.id });
            } finally {
                setStage('');
            }
            setFile(null);
            setName('');
            onAdded();
        });
    }
    return (
        <form class="flex flex-col gap-2 rounded-box bg-base-100 p-3 max-md:bg-base-200" onSubmit={submit} data-add-sound>
            <p class="text-sm font-medium">Add a sound</p>
            <FilePick label="Choose a file" accept="audio/*,video/*" file={file} inputProps={{ 'aria-label': "The sound's file" }}
                onPick={(f) => {
                    setFile(f);
                    if (!name.trim()) setName(nameOf(f.name));
                    add.setError('');
                }} />
            <Field label="Name">
                <TextInput value={name} onInput={setName} maxlength="32" required placeholder="Air horn" aria-label="The sound's name" />
            </Field>
            <Actions>
                <SubmitButton busy={add.busy} disabled={!file}>Add</SubmitButton>
                {stage && <span role="status" class="text-sm text-base-content/70">{stage}</span>}
            </Actions>
            <ErrorText message={add.error} />
        </form>
    );
}

// SoundRow is one of the den's sounds: ▶ to hear it here, its name, length
// and who added it, and its renaming and deleting, which asks first.
function SoundRow({ denID, sound, by, onChange }) {
    const [renaming, setRenaming] = useState(false);
    const [name, setName] = useState(sound.name);
    const [deleting, setDeleting] = useState(false);
    const [audio, setAudio] = useState(null);
    const act = useAction();
    useEffect(() => () => audio?.pause(), [audio]);
    function hear() {
        if (audio) {
            audio.pause();
            setAudio(null);
            return;
        }
        const a = new Audio(`/api/dens/${denID}/files/${sound.file_id}`);
        const speaker = speakerID();
        if (speaker && a.setSinkId) a.setSinkId(speaker).catch(() => {});
        a.onended = () => setAudio(null);
        a.play().catch(() => setAudio(null));
        setAudio(a);
    }
    function rename(e) {
        e.preventDefault();
        act.run(async () => {
            await api.patch(`/api/dens/${denID}/soundboard/${sound.id}`, { name });
            setRenaming(false);
            onChange();
        });
    }
    return (
        <li class="flex flex-col gap-1 py-1.5" data-manage-sound={sound.id}>
            <div class="flex items-center gap-2 text-sm">
                <button type="button" class={`${buttonClass('quiet')} btn-square ${audio ? 'text-success' : ''}`} aria-pressed={!!audio}
                    aria-label={audio ? `Stop ${sound.name}` : `Hear ${sound.name}`} title={audio ? 'Stop' : 'Hear it'} onClick={hear}>
                    <PlayIcon />
                </button>
                {renaming ? (
                    <form class="flex min-w-0 flex-1 items-center gap-1" onSubmit={rename}>
                        <input class="input input-sm min-w-0 flex-1" value={name} maxlength="32" required aria-label={`New name for ${sound.name}`}
                            onInput={(e) => setName(e.currentTarget.value)} />
                        <SubmitButton busy={act.busy}>Save</SubmitButton>
                        <Button kind="quiet" onClick={() => {
                            setRenaming(false);
                            setName(sound.name);
                            act.setError('');
                        }}>Cancel</Button>
                    </form>
                ) : (
                    <>
                        <span class="min-w-0 flex-1">
                            <span class="block truncate font-medium">{sound.name}</span>
                            <span class="block truncate text-xs text-base-content/60">{seconds(sound.duration_ms)}{by ? ` · added by ${by}` : ''}</span>
                        </span>
                        {deleting ? (
                            <>
                                <Actions>
                                    <Button kind="danger" filled busy={act.busy} onClick={() => act.run(async () => {
                                        await api.del(`/api/dens/${denID}/soundboard/${sound.id}`);
                                        onChange();
                                    })}>Delete</Button>
                                    <Button kind="quiet" onClick={() => {
                                        setDeleting(false);
                                        act.setError('');
                                    }}>Keep</Button>
                                </Actions>
                            </>
                        ) : (
                            <>
                                <Button kind="quiet" onClick={() => {
                                    setRenaming(true);
                                    act.setError('');
                                }}>Rename</Button>
                                <Button kind="danger" onClick={() => {
                                    setDeleting(true);
                                    act.setError('');
                                }}>Delete</Button>
                            </>
                        )}
                    </>
                )}
            </div>
            <ErrorText message={act.error} />
        </li>
    );
}
