// The call timer (M21.2): each call's length on its channel's row, from
// when the call began on this computer's clock, which the member's Dens
// gives as started_at; a moving rainbow while it's longer than its
// channel's record as it began, record_ms, and fireworks as it passes it.
// Appearance's Call timer, on until the member turns it off, hides all of
// it; the browser keeps the choice, as it keeps Density.

import { useEffect, useState } from 'preact/hooks';

const KEY = 'DENS_CALL_TIMER';
const OFF = 'off';

const listeners = new Set();
// chosen is the member's choice on this page, which lasts until it reloads
// where storage is unavailable.
let chosen = null;

/** callTimer says whether calls' lengths show */
export function callTimer() {
    let stored = chosen;
    try {
        stored = localStorage.getItem(KEY) ?? chosen;
    } catch {
        // Storage can be unavailable (private windows).
    }
    return stored !== OFF;
}

export function setCallTimer(on) {
    chosen = on ? '' : OFF;
    try {
        if (on) localStorage.removeItem(KEY);
        else localStorage.setItem(KEY, OFF);
    } catch {
        // As above.
    }
    listeners.forEach((fn) => fn());
}

/** useCallTimer is the choice, kept current */
export function useCallTimer() {
    const [on, setOn] = useState(callTimer);
    useEffect(() => {
        const update = () => setOn(callTimer());
        update();
        listeners.add(update);
        return () => listeners.delete(update);
    }, []);
    return on;
}

/** callLength writes a call's length: m:ss, then h:mm:ss from an hour */
export function callLength(ms) {
    const total = Math.max(0, Math.floor(ms / 1000));
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    const seconds = String(total % 60).padStart(2, '0');
    return hours ? `${hours}:${String(minutes).padStart(2, '0')}:${seconds}` : `${minutes}:${seconds}`;
}

/** beating says whether a call shows as the rainbow at now: longer than
    its channel's record as it began, where there was one to beat */
export function beating(call, now) {
    return call.record_ms > 0 && now - call.started_at > call.record_ms;
}

/** crossed says whether a call passed its record between a look at
    before and one at now: a page that first looks once it's past, with
    before null, saw no crossing */
export function crossed(call, before, now) {
    return before !== null && !beating(call, before) && beating(call, now);
}

// TICK is how often the page's one timer reads the clock for calls'
// lengths, often enough that a length shows its second within a quarter
// of one.
const TICK = 250;
const ticking = new Set();
let ticker = null;

/** onTick calls fn with the time at each tick of the page's one timer,
    which runs while anything listens, and returns what stops it */
export function onTick(fn) {
    ticking.add(fn);
    ticker ??= setInterval(() => {
        const now = Date.now();
        ticking.forEach((f) => f(now));
    }, TICK);
    return () => {
        ticking.delete(fn);
        if (ticking.size === 0) {
            clearInterval(ticker);
            ticker = null;
        }
    };
}

// FIREWORKS is how long the fireworks play, a little past the last
// burst's sparks.
export const FIREWORKS = 2200;

/** useCallClock is a call's length as it shows, kept current by the page's
    one timer: its text, whether it beats the record, and while fireworks
    play, when they began. A page that starts showing a call past its record
    shows the rainbow alone. */
export function useCallClock(call) {
    const started = call.started_at;
    const record = call.record_ms || 0;
    const [shown, setShown] = useState(() => ({ text: callLength(Date.now() - started), beating: beating(call, Date.now()), burst: 0 }));
    useEffect(() => {
        const timed = { started_at: started, record_ms: record };
        let before = null;
        let ends = null;
        const look = (now) => {
            // A record that changed starts the look over, without fireworks.
            const first = before === null;
            const burst = crossed(timed, before, now);
            before = now;
            const text = callLength(now - started);
            const beats = beating(timed, now);
            setShown((cur) => {
                const at = burst ? now : first ? 0 : cur.burst;
                return cur.text === text && cur.beating === beats && cur.burst === at ? cur : { text, beating: beats, burst: at };
            });
            if (burst) {
                clearTimeout(ends);
                ends = setTimeout(() => setShown((cur) => ({ ...cur, burst: 0 })), FIREWORKS);
            }
        };
        look(Date.now());
        const stop = onTick(look);
        return () => {
            stop();
            clearTimeout(ends);
        };
    }, [started, record]);
    return shown;
}

// SPARKS are each burst's sparks, and BURSTS where along the row each
// bursts, as a fraction of its width, and when.
const SPARKS = 14;
const BURSTS = [[0.25, 0], [0.7, 450], [0.45, 900]];
const COLORS = ['#ff5f5f', '#ffb347', '#ffe066', '#6be675', '#5cc8ff', '#9b7bff', '#ff6bd6'];

/** sparks lays out the fireworks: each spark's place along the row, its
    way out as --dx and --dy in pixels, its color and its delay */
export function sparks() {
    const out = [];
    BURSTS.forEach(([x, delay], b) => {
        for (let i = 0; i < SPARKS; i++) {
            const angle = (2 * Math.PI * i) / SPARKS + b;
            const reach = 20 + ((i * 7 + b * 5) % 13);
            out.push({ x, dx: Math.round(Math.cos(angle) * reach), dy: Math.round(Math.sin(angle) * reach), color: COLORS[(i + b * 3) % COLORS.length], delay });
        }
    });
    return out;
}
