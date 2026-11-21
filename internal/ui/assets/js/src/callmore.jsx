// The rest of the call's controls: the block's row holds what a
// call needs at hand, and an arrow at its start opens the others as a grid
// over it: the soundboard, where the den has one, and the call's lock.
// What one of them opens stands by the arrow, which closes it again.

import { useRef, useState } from 'preact/hooks';
import { CallLockDialog, LockKey } from './calllock.jsx';
import { ChevronIcon } from './icons.jsx';
import { usePopup } from './popup.js';
import { Soundboard, SoundboardKey, shows } from './soundboard.jsx';

// ACROSS is how many buttons the grid holds across, and WIDTH the most it
// takes of the window, in pixels, with a finger's buttons.
const ACROSS = 4;
const WIDTH = 190;

export function CallMore({ call, den, board, sounding }) {
    // open is what the arrow has open: 'more', its grid, or what a button
    // of the grid opened in its place, 'soundboard' or 'lock'.
    const [open, setOpen] = useState(null);
    const anchor = useRef(null);
    const close = () => setOpen(null);
    const place = usePopup(open === 'more', anchor, { width: WIDTH, onClose: close });
    const sounds = shows(call, den);
    if (!sounds && call.sealed === undefined) return null;
    // A call the den doesn't seal says so on the arrow, as its lock does.
    const color = open ? 'text-success' : call.sealed === false ? 'text-warning' : '';
    return (
        <span ref={anchor} class="relative inline-flex">
            <button type="button" class={`btn btn-ghost btn-sm btn-square ${color}`} aria-label="More" title="More" aria-haspopup="true"
                aria-expanded={!!open} data-call-more-button onClick={() => setOpen(open ? null : 'more')}>
                <ChevronIcon down={!!open} />
            </button>
            {place && (
                <div role="group" aria-label="More call controls" data-call-more class="fixed z-40 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-xl" style={place}>
                    <div class="flex flex-wrap gap-1" style={{ maxWidth: `calc(${ACROSS} * 2.5rem + ${ACROSS - 1} * 0.25rem)` }}>
                        {sounds && <SoundboardKey den={den} where="block" many onClick={(empty) => setOpen(empty ? null : 'soundboard')} />}
                        <LockKey call={call} many onClick={() => setOpen('lock')} />
                    </div>
                </div>
            )}
            {open === 'soundboard' && sounds && <Soundboard anchor={anchor} call={call} den={den} board={board} sounding={sounding} where="block" onClose={close} />}
            {open === 'lock' && <CallLockDialog call={call} den={den} onClose={close} />}
        </span>
    );
}
