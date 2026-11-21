// The call timer's marks: a call's length at its channel's row's
// right, its tip naming the channel's record, the rainbow while the call
// beats it, and fireworks over the row as it passes. The stylesheet draws
// both, and holds them still where less motion is asked for.

import { Tip } from '../ui/components.jsx';
import { callLength, sparks, useCallClock, useCallTimer } from './calltimer.js';

// CallLength is a call's length for its channel's row, or for the call's
// pane's header, as where says, where the member's Dens gives one and they
// didn't turn the timer off. What it sits in places the fireworks.
export function CallLength({ call, where = '' }) {
    const on = useCallTimer();
    if (!on || !call?.started_at) return null;
    return <Length call={call} where={where} />;
}

function Length({ call, where }) {
    const clock = useCallClock(call);
    const record = call.record_ms > 0 ? `Longest call: ${callLength(call.record_ms)}` : 'No longest call yet';
    const figures = (
        <span class={`text-xs tabular-nums ${clock.beating ? 'call-rainbow font-semibold' : ''}`} data-call-length={call.channel_id}
            data-beating={clock.beating || undefined}>
            {clock.text}
        </span>
    );
    return (
        <>
            <Tip id={`${where}call-length-${call.channel_id}`} label={`Call length ${clock.text}`} trigger={figures}>{record}</Tip>
            {clock.burst > 0 && <Fireworks key={clock.burst} />}
        </>
    );
}

// Fireworks are three bursts of sparks over the row, for a couple of
// seconds; the hook that shows them takes them away.
function Fireworks() {
    return (
        <span class="call-fireworks" data-fireworks aria-hidden="true">
            {sparks().map((s, i) => (
                <span key={i} class="call-spark" style={{ left: `${s.x * 100}%`, '--dx': `${s.dx}px`, '--dy': `${s.dy}px`, '--spark': s.color, animationDelay: `${s.delay}ms` }} />
            ))}
        </span>
    );
}
