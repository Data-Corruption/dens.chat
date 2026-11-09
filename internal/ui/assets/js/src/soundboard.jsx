// The den's soundboard (M14.3): its button among the call's controls,
// which opens it, up from the list's block and down from Home's box, and
// on the phone layout as a sheet from the screen's foot; and its section
// in Manage, where staff add sounds, and hear, rename or delete each.

import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { ErrorText, Field, TextInput, useAction } from './components.jsx';
import { openManage } from './dialogs.js';
import { onEvent } from './events.js';
import { upload } from './files.jsx';
import { CogIcon, PlayIcon, SoundboardIcon } from './icons.jsx';
import { useNarrow } from './nav.js';
import { rank } from './people.jsx';
import { Sheet } from './sheet.jsx';
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

// popupAt places the popup by its button's place on screen: up and to the
// right of it, or down, kept within the window. It stands over the page,
// past the list's edge, which would clip it within the list.
export function popupAt(rect, down, vw, vh) {
    const left = Math.max(8, Math.min(rect.left, vw - WIDTH - 8));
    return down ? { left, top: rect.bottom + 8 } : { left, bottom: vh - rect.top + 8 };
}

// SoundboardButton opens the soundboard of the den of the page's call:
// from the list's block, up and to the right; from Home's box, down; and on
// the phone layout as a sheet. With no sounds in the den, staff's opens on
// adding them, in Manage. where says which, for the checks.
export function SoundboardButton({ call, den, board, sounding, where }) {
    const [open, setOpen] = useState(false);
    const [place, setPlace] = useState(null);
    const narrow = useNarrow();
    const anchor = useRef(null);
    // The popup follows its button as the page scrolls or the window
    // changes size.
    useLayoutEffect(() => {
        if (!open || narrow) return undefined;
        const follow = () => anchor.current && setPlace(popupAt(anchor.current.getBoundingClientRect(), where === 'box', window.innerWidth, window.innerHeight));
        follow();
        window.addEventListener('resize', follow);
        document.addEventListener('scroll', follow, true);
        return () => {
            window.removeEventListener('resize', follow);
            document.removeEventListener('scroll', follow, true);
        };
    }, [open, narrow, where]);
    // The popup closes on a press anywhere else, or on Escape; the sheet
    // closes itself.
    useEffect(() => {
        if (!open || narrow) return undefined;
        const away = (e) => {
            if (!anchor.current?.contains(e.target)) setOpen(false);
        };
        const escape = (e) => e.key === 'Escape' && setOpen(false);
        document.addEventListener('pointerdown', away);
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('pointerdown', away);
            document.removeEventListener('keydown', escape);
        };
    }, [open, narrow]);
    if (!shows(call, den)) return null;
    const close = () => {
        setOpen(false);
        clearBoardNotice();
    };
    const body = <Board den={den} board={board} sounding={sounding} deafened={call.deafened} sheet={narrow} onClose={close} />;
    const empty = den.sounds.length === 0;
    const click = () => {
        if (empty) openManage(den.den, 'soundboard');
        else if (open) close();
        else setOpen(true);
    };
    return (
        <span ref={anchor} class="relative inline-flex">
            <button type="button" class={`btn btn-ghost btn-sm btn-square ${open ? 'text-success' : ''}`} aria-label={empty ? 'Add sounds to the soundboard' : 'Soundboard'}
                title={empty ? 'Add sounds to the soundboard' : 'Soundboard'} aria-haspopup="dialog" aria-expanded={open} data-soundboard-button={where} onClick={click}>
                <SoundboardIcon />
            </button>
            {open && narrow && (
                <Sheet label="Soundboard" title={<span class="font-semibold">Soundboard</span>} onClose={close}>{body}</Sheet>
            )}
            {open && !narrow && place && (
                <div role="dialog" aria-label="Soundboard" data-soundboard={where} class="fixed z-40 rounded-box border border-base-300 bg-base-100 shadow-xl"
                    style={{ ...place, width: `${WIDTH}px` }}>
                    {body}
                </div>
            )}
        </span>
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
    const tile = 'btn btn-sm h-auto min-h-8 justify-start gap-1 py-1 text-left font-normal pointer-coarse:min-h-11';
    return (
        <div class={`flex flex-col gap-2 ${sheet ? 'px-4 pb-2' : 'p-3'}`} data-board>
            <div class="flex items-center gap-1">
                <input type="search" class="input input-sm min-w-0 flex-1 pointer-coarse:input-md" placeholder="Search sounds" aria-label="Search sounds"
                    value={query} onInput={(e) => setQuery(e.currentTarget.value)} />
                {staff && (
                    <button type="button" class="btn btn-ghost btn-sm btn-square pointer-coarse:btn-md" aria-label="Manage sounds" title="Manage sounds"
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
                <input type="range" class={`range min-w-0 flex-1 ${sheet ? 'range-xl' : 'range-md'}`} min="0" max="100" step="5" value={volume}
                    aria-label="Soundboard volume" aria-valuetext={volume === 0 ? 'muted' : `${volume}%`} data-board-volume
                    onInput={(e) => {
                        const v = Number(e.currentTarget.value);
                        setVolume(v);
                        setBoardVolume(v / 100);
                    }} />
                <span class="w-12 text-right tabular-nums">{volume === 0 ? 'Muted' : `${volume}%`}</span>
            </label>
            {why && <p role="status" class="text-xs text-base-content/70" data-board-why>{why}</p>}
            {board.notice && <p role="status" class="text-xs text-warning" data-board-notice>{board.notice}</p>}
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
// den's limits for it as they apply, a sound added by a file and a name,
// prepared by this page, and the den's sounds, each to hear, rename or
// delete. owner says whether the member sets the limits, in Limits.
export function ManageSoundboard({ denID, owner, onLimits }) {
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
            <h3 class="text-lg font-semibold">Soundboard</h3>
            <p class="text-sm text-base-content/70">Sounds anyone in a call can play for everyone in it.</p>
            {!limits ? (
                <p class="text-sm">This den runs a version of Dens without a soundboard.</p>
            ) : (
                <>
                    {limits.sounds === 0 ? (
                        <p role="status" class="text-sm text-warning">
                            The soundboard is off: nobody can play its sounds.
                            {owner && <> <button type="button" class="link link-primary" onClick={onLimits}>Turn it on in Limits</button></>}
                        </p>
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
    const [picker, setPicker] = useState(0);
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
            setPicker((n) => n + 1);
            onAdded();
        });
    }
    return (
        <form class="flex flex-col gap-2 rounded-box bg-base-200 p-3" onSubmit={submit} data-add-sound>
            <p class="font-medium">Add a sound</p>
            <input key={picker} type="file" class="file-input w-full" accept="audio/*,video/*" aria-label="The sound's file"
                onChange={(e) => {
                    const f = e.currentTarget.files?.[0] || null;
                    setFile(f);
                    if (f && !name.trim()) setName(nameOf(f.name));
                    add.setError('');
                }} />
            <Field label="Name">
                <TextInput value={name} onInput={setName} maxlength="32" required placeholder="Air horn" aria-label="The sound's name" />
            </Field>
            <div class="flex flex-wrap items-center gap-2">
                <button type="submit" class="btn btn-primary btn-sm" disabled={!file || add.busy}>
                    {add.busy && <span class="loading loading-spinner loading-sm"></span>}
                    Add
                </button>
                {stage && <span role="status" class="text-sm text-base-content/70">{stage}</span>}
            </div>
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
                <button type="button" class={`btn btn-ghost btn-sm btn-square ${audio ? 'text-success' : ''}`} aria-pressed={!!audio}
                    aria-label={audio ? `Stop ${sound.name}` : `Hear ${sound.name}`} title={audio ? 'Stop' : 'Hear it'} onClick={hear}>
                    <PlayIcon />
                </button>
                {renaming ? (
                    <form class="flex min-w-0 flex-1 items-center gap-1" onSubmit={rename}>
                        <input class="input input-sm min-w-0 flex-1" value={name} maxlength="32" required aria-label={`New name for ${sound.name}`}
                            onInput={(e) => setName(e.currentTarget.value)} />
                        <button type="submit" class="btn btn-primary btn-sm" disabled={act.busy}>Save</button>
                        <button type="button" class="btn btn-ghost btn-sm" onClick={() => {
                            setRenaming(false);
                            setName(sound.name);
                            act.setError('');
                        }}>Cancel</button>
                    </form>
                ) : (
                    <>
                        <span class="min-w-0 flex-1">
                            <span class="block truncate font-medium">{sound.name}</span>
                            <span class="block truncate text-xs text-base-content/60">{seconds(sound.duration_ms)}{by ? ` · added by ${by}` : ''}</span>
                        </span>
                        {deleting ? (
                            <>
                                <button type="button" class="btn btn-error btn-sm" disabled={act.busy} onClick={() => act.run(async () => {
                                    await api.del(`/api/dens/${denID}/soundboard/${sound.id}`);
                                    onChange();
                                })}>Delete</button>
                                <button type="button" class="btn btn-ghost btn-sm" onClick={() => {
                                    setDeleting(false);
                                    act.setError('');
                                }}>Keep</button>
                            </>
                        ) : (
                            <>
                                <button type="button" class="btn btn-ghost btn-sm" onClick={() => {
                                    setRenaming(true);
                                    act.setError('');
                                }}>Rename</button>
                                <button type="button" class="btn btn-ghost btn-sm text-error" onClick={() => {
                                    setDeleting(true);
                                    act.setError('');
                                }}>Delete</button>
                            </>
                        )}
                    </>
                )}
            </div>
            <ErrorText message={act.error} />
        </li>
    );
}
