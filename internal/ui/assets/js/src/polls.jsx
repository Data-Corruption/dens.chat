// Polls on the page (M15.4): the editor the composer's + opens above its
// box, where a reply and files stand, whose question is the message's
// text; and a message's poll, under its question, as the member may see it.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { ErrorText, TIP, Tip, useAction, useTip } from './components.jsx';
import { DURATIONS, MAX_CHOICES, TOTALS, VOTERS, pollView, rulesOf, rulesTip, shareOf, timeLeft, votesLine, whoVoted } from './polls.js';

/** PollEditor is the poll being written, whose question the box below it
    holds: its choices, 2 to 10, whether a member may pick more than one, how
    long it runs, and its two rules, fixed once it's posted, as anonymous
    voting is asked: yes, during the poll, or no. problem says what keeps it
    from going. */
export function PollEditor({ draft, problem, onChange, onRemove }) {
    const set = (patch) => onChange({ ...draft, ...patch });
    const added = useRef(false);
    const list = useRef(null);
    // A choice added takes the focus.
    useEffect(() => {
        if (!added.current) return;
        added.current = false;
        list.current?.querySelector('li:last-child input')?.focus();
    }, [draft.choices.length]);
    return (
        <div class="mb-2 flex flex-col gap-2 rounded border border-base-300 p-2" data-poll-editor>
            <ol ref={list} class="flex flex-col gap-1" aria-label="Choices">
                {draft.choices.map((c, i) => (
                    <li key={i} class="flex items-center gap-1">
                        <input type="text" class="input input-sm w-full pointer-coarse:min-h-11" placeholder={`Choice ${i + 1}`} aria-label={`Choice ${i + 1}`} value={c}
                            enterkeyhint="next" data-choice-input={i}
                            onInput={(e) => set({ choices: draft.choices.map((x, j) => (j === i ? e.currentTarget.value : x)) })}
                            onKeyDown={(e) => {
                                // Enter moves on to the next choice, adding one at the end.
                                if (e.key !== 'Enter' || e.isComposing) return;
                                e.preventDefault();
                                const next = e.currentTarget.closest('li')?.nextElementSibling?.querySelector('input');
                                if (next) next.focus();
                                else if (draft.choices.length < MAX_CHOICES) {
                                    added.current = true;
                                    set({ choices: [...draft.choices, ''] });
                                }
                            }} />
                        {draft.choices.length > 2 && (
                            <button type="button" class="btn btn-ghost btn-sm btn-square shrink-0" aria-label={`Remove choice ${i + 1}`}
                                onClick={() => set({ choices: draft.choices.filter((_, j) => j !== i) })}>✕</button>
                        )}
                    </li>
                ))}
            </ol>
            <div class="flex items-center justify-between gap-2">
                {draft.choices.length < MAX_CHOICES ? (
                    <button type="button" class="btn btn-ghost btn-xs" onClick={() => {
                        added.current = true;
                        set({ choices: [...draft.choices, ''] });
                    }}>+ Add a choice</button>
                ) : <span></span>}
                <button type="button" class="btn btn-ghost btn-xs relative touch-room" onClick={onRemove}>Cancel</button>
            </div>
            <label class="flex min-h-6 cursor-pointer items-center gap-2 self-start text-sm">
                <input type="checkbox" class="checkbox checkbox-sm" checked={draft.multiple} onChange={(e) => set({ multiple: e.currentTarget.checked })} />
                Members may pick more than one
            </label>
            <div class="grid grid-cols-1 gap-2 text-xs sm:grid-cols-3">
                <PollSelect label="Runs for" value={draft.hours} options={DURATIONS.map((d) => [d.hours, d.label])}
                    onChange={(v) => set({ hours: Number(v) })} />
                <PollSelect label="Totals show" value={draft.show_totals} options={TOTALS.map((o) => [o.value, o.label])}
                    onChange={(v) => set({ show_totals: v })} />
                <PollSelect label="Anonymous voting" value={draft.show_voters} options={VOTERS.map((o) => [o.value, o.label])}
                    onChange={(v) => set({ show_voters: v })} />
            </div>
            {draft.show_totals === 'voted' && (
                <p class="text-xs text-base-content/70">Each member sees the totals once they vote, and their vote is then final.</p>
            )}
            {problem && <p class="text-xs text-error" role="alert">{problem}</p>}
        </div>
    );
}

function PollSelect({ label, value, options, onChange }) {
    return (
        <label class="flex flex-col gap-0.5">
            <span class="text-base-content/70">{label}</span>
            <select class="select select-sm w-full" value={value} onChange={(e) => onChange(e.currentTarget.value)}>
                {options.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            </select>
        </label>
    );
}

/** Poll is a message's poll as the member sees it, under its question:
    each choice, with a bar of its share once they may see the totals, and
    theirs marked; how many voted; how long it has left, or that it ended;
    and its rules in a few words, with a tip. While they may vote, its
    choices pick and Vote votes; once they've voted, where the totals show
    as votes come in, they change their vote or take it back; and its
    author ends it early. Where the poll shows them, a choice's voters show
    on hover over its count, or a tap. onPolled(t, d) applies what the den
    answers, as its event would. */
export function Poll({ m, me, denID, nameOf, onPolled }) {
    const p = m.poll;
    const [now, setNow] = useState(Date.now);
    useEffect(() => {
        if (p.ended) return undefined;
        setNow(Date.now());
        const id = setInterval(() => setNow(Date.now()), 15000);
        return () => clearInterval(id);
    }, [p.ended, p.ends_at]);
    const v = pollView(p, now);
    // picked is what the member chose before Vote, null while the poll
    // shows their vote; ending asks before an early end.
    const [picked, setPicked] = useState(null);
    const [ending, setEnding] = useState(false);
    const act = useAction();
    const choosing = v.canVote && (!v.voted || picked !== null);
    const chosen = picked || [];
    const mine = p.mine || [];
    const base = `/api/dens/${denID}/messages/${m.id}/poll`;
    function vote(choices) {
        act.run(async () => {
            const got = await api.post(`${base}/vote`, { choices });
            onPolled('poll.updated', { message_id: m.id, voted: got.voted, totals: got.totals, voters: got.voters, mine: got.mine || [] });
            setPicked(null);
        });
    }
    function end() {
        act.run(async () => {
            const got = await api.post(`${base}/end`, {});
            onPolled('poll.ended', { message_id: m.id, ends_at: got.ends_at, voted: got.voted, totals: got.totals, voters: got.voters });
            setEnding(false);
        });
    }
    function pick(i, on) {
        if (p.multiple) setPicked(on ? [...chosen, i].sort((a, b) => a - b) : chosen.filter((x) => x !== i));
        else setPicked([i]);
    }
    return (
        <div class="mt-1 max-w-md rounded-box border border-base-300 p-2" data-poll={m.id} data-ended={v.ended ? '' : undefined}>
            <ul class="flex flex-col gap-1" aria-label="Choices">
                {p.choices.map((c, i) => (
                    <Choice key={i} p={p} i={i} text={c} name={`poll-${m.id}`} choosing={choosing} picked={chosen.includes(i)} mine={mine.includes(i)}
                        disabled={act.busy} me={me.id} nameOf={nameOf} onPick={(on) => pick(i, on)} />
                ))}
            </ul>
            <div class="mt-2 flex flex-col gap-0.5 text-xs text-base-content/60">
                <p>
                    <span data-votes={p.voted}>{votesLine(p)}</span> · <span data-left>{timeLeft(p, now)}</span>
                    {v.final && !v.ended && <> · Your vote is final.</>}
                </p>
                <p>
                    <span data-rules={`${p.show_voters} ${p.show_totals}`}>{rulesOf(p)}</span>{' '}
                    <Tip id={`poll-${m.id}-rules`} label="About this poll's rules">{rulesTip(p)}</Tip>
                </p>
            </div>
            {(choosing || (v.voted && v.canVote) || (m.author_id === me.id && !v.ended)) && (
                <div class="mt-2 flex flex-wrap items-center gap-1">
                    {choosing && (
                        <button type="button" class="btn btn-primary btn-xs" disabled={!chosen.length || act.busy} onClick={() => vote(chosen)}>Vote</button>
                    )}
                    {choosing && v.voted && <button type="button" class="btn btn-ghost btn-xs" onClick={() => setPicked(null)}>Cancel</button>}
                    {!choosing && v.voted && v.canVote && (
                        <>
                            <button type="button" class="btn btn-ghost btn-xs" disabled={act.busy} onClick={() => setPicked(mine)}>Change vote</button>
                            <button type="button" class="btn btn-ghost btn-xs" disabled={act.busy} onClick={() => vote([])}>Remove vote</button>
                        </>
                    )}
                    {m.author_id === me.id && !v.ended && (ending ? (
                        <span class="ml-auto flex items-center gap-1 text-xs">
                            End it now, for everyone?
                            <button type="button" class="btn btn-error btn-xs" disabled={act.busy} onClick={end}>End poll</button>
                            <button type="button" class="btn btn-ghost btn-xs" onClick={() => setEnding(false)}>Keep it</button>
                        </span>
                    ) : (
                        <button type="button" class="btn btn-ghost btn-xs ml-auto" onClick={() => setEnding(true)}>End now</button>
                    ))}
                </div>
            )}
            <ErrorText message={act.error} />
        </div>
    );
}

// Choice is one of a poll's choices: while the member chooses, a radio
// button or a check box, whose label is the whole row, for a finger; its
// bar where the totals show; and its count, and where the poll shows them,
// its voters in the count's tip.
function Choice({ p, i, text, name, choosing, picked, mine, disabled, me, nameOf, onPick }) {
    const shown = !!p.totals;
    const share = shareOf(p, i);
    const Row = choosing ? 'label' : 'div';
    return (
        <li class={`relative rounded border ${mine ? 'border-primary' : 'border-base-300'}`} data-choice={i}
            data-count={shown ? p.totals[i] : undefined} data-mine={mine ? '' : undefined}>
            {/* The bar's own layer rounds it, so the count's tip shows past
                the row. */}
            {shown && (
                <div class="absolute inset-0 overflow-hidden rounded" aria-hidden="true">
                    <div class={`h-full ${mine ? 'bg-primary/25' : 'bg-base-content/10'}`} style={{ width: `${share}%` }}></div>
                </div>
            )}
            <Row class={`relative flex min-h-9 items-center gap-2 px-2 py-1 pointer-coarse:min-h-11 ${choosing ? 'cursor-pointer' : ''}`}>
                {choosing ? (
                    <input type={p.multiple ? 'checkbox' : 'radio'} name={name} class={p.multiple ? 'checkbox checkbox-sm' : 'radio radio-sm'}
                        checked={picked} disabled={disabled} onChange={(e) => onPick(e.currentTarget.checked)} />
                ) : (
                    <span class="flex w-5 shrink-0 justify-center text-primary" aria-label={mine ? 'Your vote' : undefined}>{mine ? '✓' : ''}</span>
                )}
                <span class="min-w-0 flex-1 wrap-break-word text-sm">{text}</span>
                {shown && <Count p={p} i={i} share={share} me={me} nameOf={nameOf} />}
            </Row>
        </li>
    );
}

// Count is a choice's count and share, whose tip, where the poll shows who
// voted for what, says who: on hover or focus, or a tap.
function Count({ p, i, share, me, nameOf }) {
    const tip = useTip('bottom');
    const total = p.totals[i];
    const words = `${total} ${total === 1 ? 'vote' : 'votes'}, ${share}%`;
    if (!p.voters) return <span class="shrink-0 text-xs tabular-nums text-base-content/70" aria-label={words}>{share}% · {total}</span>;
    const who = whoVoted(p, i, me, nameOf);
    return (
        <span {...tip.wrapProps} class={tip.wrapProps.class}>
            <span role="tooltip" class={TIP} {...tip.contentProps}>{who}</span>
            <button type="button" class="shrink-0 rounded px-1 text-xs tabular-nums text-base-content/70 hover:text-base-content" aria-label={`${words}: ${who}`}
                data-voters={(p.voters[i] || []).join(' ')} {...tip.triggerProps}
                onPointerDown={(e) => {
                    // Its press is its own, not the start of its message's
                    // long press, and a tap shows the tip.
                    e.stopPropagation();
                }}
                onClick={() => tip.setPinned(!tip.pinned)}>
                {share}% · {total}
            </button>
        </span>
    );
}
