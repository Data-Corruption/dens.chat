// The markdown subset messages use: **bold**, *italic* or _italic_,
// ~~strikethrough~~, `inline code`, ``` code blocks ```, > quotes,
// ||spoilers||, @mentions, bare http(s) links, and task lines that start
// with [ ] or [x]. There are no [label](url) links, so a link always shows
// where it goes.
//
// Every message comes from a den, and dens are other people's servers. The
// renderer builds Preact nodes whose text is always text: nothing here
// turns a string into markup, and a link's address is only ever an
// http(s) URL the parser matched. Any member can send any text, so a line
// takes time in proportion to its length, whatever its delimiters (M8).

import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { shownLink, trimLink, videoOf } from './links.js';

const NAME_CHAR = /[A-Za-z0-9_]/;
const SPACE = /\s/;
// What ends a link: a space, or one of <>"'`.
const ENDS_LINK = /[\s<>"'`]/;
// A task line, as denproto.Tasks finds one outside code blocks.
const TASK_RE = /^\[( |x)\] /;

// Markdown renders text as blocks: code blocks, quotes, task lines and
// paragraphs. With onTask, the reader may tick tasks: onTask(n, checked,
// text) returns a promise that settles once the den has the tick.
//
// It parses a text once (M8): the parse is kept while the text, the
// reader's username and whether tasks tick stay the same, so a row that
// renders again, as every row does while the member types, gives Preact
// the same tree, which it skips. A tick goes through a ref, so it reaches
// the row's current onTask.
export function Markdown({ text, me, onTask }) {
    const tick = useRef(onTask);
    tick.current = onTask;
    const ticks = !!onTask;
    const username = me?.username;
    return useMemo(() => parse(text, { username, onTask: ticks ? (...a) => tick.current(...a) : undefined }), [text, username, ticks]);
}

// parse is what Markdown renders: a text's blocks in their container. ctx
// holds the reader's username, whose mentions look different, and onTask.
export function parse(text, ctx) {
    return <div class="markdown">{blocks(text, ctx)}</div>;
}

// Preview renders a text's first line inline, for places that are already
// a control, like a reply's quote: nothing in it is a link or a button.
// With more, the line ends in an ellipsis in place of its punctuation. It
// keeps its parse as Markdown does.
export function Preview({ text, me, more }) {
    const username = me?.username;
    return useMemo(() => preview(text, username, more), [text, username, more]);
}

// preview is what Preview renders.
export function preview(text, username, more) {
    let line = firstLine(text);
    if (more) line = line.trimEnd().replace(/[.,;:]+$/, '');
    return <span>{inline(line, marksOf(line), { username, preview: true }, 'p', 0, line.length)}{more && '…'}</span>;
}

// MAX_VIDEOS is how many YouTube covers a message gets at most (M4.1).
export const MAX_VIDEOS = 3;

// videosIn lists the YouTube videos a message's covers play (M4.1): those
// its links point to in the one form the den writes, found exactly as the
// renderer finds links, so none in code, and none in a spoiler, whose
// cover would show what it hides. Each video comes once, at its first
// link's start time.
export function videosIn(text) {
    const ctx = { videos: [] };
    blocks(text, ctx);
    const seen = new Set();
    return ctx.videos.filter((v) => !seen.has(v.id) && seen.add(v.id)).slice(0, MAX_VIDEOS);
}

function blocks(text, ctx) {
    const out = [];
    const lines = text.split('\n');
    let para = [];
    let quote = [];
    let tasks = 0;
    const flushPara = () => {
        if (para.length) out.push(<p key={out.length}>{joinLines(para, ctx)}</p>);
        para = [];
    };
    const flushQuote = () => {
        if (quote.length) {
            out.push(
                <blockquote key={out.length} class="border-l-4 border-base-content/30 pl-3 text-base-content/80">
                    {joinLines(quote, ctx)}
                </blockquote>,
            );
        }
        quote = [];
    };
    for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        if (line.trim().startsWith('```')) {
            flushPara();
            flushQuote();
            const code = [];
            i++;
            while (i < lines.length && !lines[i].trim().startsWith('```')) code.push(lines[i++]);
            out.push(
                <pre key={out.length} class="my-1 overflow-x-auto rounded bg-base-300 p-2 text-sm">
                    <code>{code.join('\n')}</code>
                </pre>,
            );
            continue;
        }
        const box = TASK_RE.exec(line);
        if (box) {
            flushPara();
            flushQuote();
            const label = line.slice(box[0].length);
            out.push(
                <Task key={out.length} n={tasks++} checked={box[1] === 'x'} text={label} onTask={ctx.onTask}>
                    {inline(label, marksOf(label), ctx, `t${out.length}`, 0, label.length)}
                </Task>,
            );
            continue;
        }
        if (line.startsWith('>')) {
            flushPara();
            quote.push(line.replace(/^> ?/, ''));
            continue;
        }
        flushQuote();
        para.push(line);
    }
    flushPara();
    flushQuote();
    return out;
}

function joinLines(lines, ctx) {
    const out = [];
    lines.forEach((line, i) => {
        if (i > 0) out.push(<br key={`br${i}`} />);
        out.push(...inline(line, marksOf(line), ctx, `l${i}`, 0, line.length));
    });
    return out;
}

// The delimiters, in the order inline tries them, and what each makes.
const DELIMITERS = ['**', '~~', '||', '*', '_'];
const MAKE = [
    (c, k) => <strong key={k}>{c}</strong>,
    (c, k) => <del key={k}>{c}</del>,
    (c, k, ctx) => (ctx.preview ? <span key={k} class="rounded bg-base-content/40 px-0.5 text-transparent select-none">{c}</span> : <Spoiler key={k}>{c}</Spoiler>),
    (c, k) => <em key={k}>{c}</em>,
    (c, k) => <em key={k}>{c}</em>,
];

// marksOf finds what a line's inline syntax needs, so the line takes time
// in proportion to its length, whatever its delimiters (M8): code[i] is
// the backtick that closes a code span opening at i, found in one pass,
// and closer(marks, d, i) where delimiter d first closes at or after i, or
// -1, from a table found in one pass the first time a d opens. A code span
// runs from a backtick to the next one with something in between, and a
// delimiter closes after something other than a space, outside code
// spans. A line without backticks has no code.
function marksOf(line) {
    const marks = { line, code: null, inCode: null, next: [] };
    for (let i = line.indexOf('`'); i >= 0;) {
        const end = line.indexOf('`', i + 1);
        if (end < 0) break;
        if (end === i + 1) {
            i = end;
            continue;
        }
        marks.code ??= new Int32Array(line.length);
        marks.inCode ??= new Uint8Array(line.length);
        marks.code[i] = end;
        marks.inCode.fill(1, i, end + 1);
        i = line.indexOf('`', end + 1);
    }
    return marks;
}

function closer(marks, d, i) {
    if (!marks.next[d]) {
        const { line, inCode } = marks;
        const delim = DELIMITERS[d];
        const at = new Int32Array(line.length + 1);
        at[line.length] = -1;
        for (let j = line.length - 1; j >= 0; j--) {
            at[j] = j > 0 && !inCode?.[j] && line.startsWith(delim, j) && !SPACE.test(line[j - 1]) ? j : at[j + 1];
        }
        marks.next[d] = at;
    }
    return marks.next[d][i];
}

// linkEnd is where a link that starts at i ends, before end, or -1 if none
// does: http:// or https://, then anything up to a space or one of <>"'`.
function linkEnd(line, i, end) {
    let j = line.startsWith('https://', i) ? i + 8 : line.startsWith('http://', i) ? i + 7 : -1;
    if (j < 0 || j >= end) return -1;
    const start = j;
    while (j < end && !ENDS_LINK.test(line[j])) j++;
    return j > start ? j : -1;
}

// inline renders the inline syntax of line from start to end, with the
// marks marksOf found for it. Code spans are kept literal; everything else
// can nest. Links and mentions start where denproto.Mentions says they do,
// nested or not: after a character that can't be in a name.
function inline(line, marks, ctx, key, start, end) {
    const out = [];
    let plain = '';
    let n = 0;
    const flush = () => {
        if (plain) out.push(plain);
        plain = '';
    };
    for (let i = start; i < end;) {
        const c = line[i];
        const before = i === 0 ? '' : line[i - 1];
        if (c === '`' && marks.code?.[i] > i) {
            const close = marks.code[i];
            flush();
            out.push(<code key={`${key}-${n++}`} class="rounded bg-base-300 px-1 text-sm">{line.slice(i + 1, close)}</code>);
            i = close + 1;
            continue;
        }
        if (c === 'h' && !NAME_CHAR.test(before)) {
            const stop = linkEnd(line, i, end);
            if (stop > 0) {
                const url = trimLink(line.slice(i, stop));
                const shown = shownLink(url);
                const video = ctx.videos && !ctx.spoiler && videoOf(url);
                if (video) ctx.videos.push(video);
                flush();
                out.push(
                    ctx.preview ? (
                        <span key={`${key}-${n++}`} class="text-primary">{shown}</span>
                    ) : (
                        <a key={`${key}-${n++}`} href={shown} target="_blank" rel="noopener noreferrer nofollow" class="link link-primary break-all">
                            {shown}
                        </a>
                    ),
                );
                i += url.length;
                continue;
            }
        }
        if (c === '@' && !NAME_CHAR.test(before) && before !== '@') {
            let j = i + 1;
            while (j < end && NAME_CHAR.test(line[j])) j++;
            const name = line.slice(i + 1, j);
            if (name.length >= 2 && name.length <= 32) {
                flush();
                const self = ctx.username && name.toLowerCase() === ctx.username;
                out.push(
                    <span key={`${key}-${n++}`} data-mention={name.toLowerCase()} class={`rounded px-0.5 font-medium ${self ? 'bg-warning/30' : 'bg-primary/15 text-primary'}`}>
                        @{name}
                    </span>,
                );
                i = j;
                continue;
            }
        }
        const d = DELIMITERS.findIndex((x) => i + x.length <= end && line.startsWith(x, i));
        if (d >= 0) {
            const from = i + DELIMITERS[d].length;
            const close = from < end ? closer(marks, d, from) : -1;
            if (close > from && close + DELIMITERS[d].length <= end) {
                flush();
                const inner = DELIMITERS[d] === '||' ? { ...ctx, spoiler: true } : ctx;
                out.push(MAKE[d](inline(line, marks, inner, `${key}-${n}`, from, close), `${key}-${n++}`, ctx));
                i = close + DELIMITERS[d].length;
                continue;
            }
        }
        plain += c;
        i++;
    }
    flush();
    return out;
}

// Task is a task line's checkbox. A tick shows at once, and holds until
// the message catches up with it, or undoes itself if the den refused it.
// Only the box ticks, so a link in the task's text opens without ticking.
function Task({ n, checked, text, onTask, children }) {
    const [want, setWant] = useState(null);
    useEffect(() => setWant(null), [checked, text]);
    const shown = want ?? checked;
    return (
        <div class="flex items-start gap-2">
            <input
                type="checkbox"
                class="checkbox checkbox-sm mt-0.5"
                checked={shown}
                disabled={!onTask}
                aria-label={text || 'Task'}
                onChange={(e) => {
                    const next = e.currentTarget.checked;
                    setWant(next);
                    onTask(n, next, text).catch(() => setWant(null));
                }}
            />
            <span class={shown ? 'text-base-content/60 line-through' : ''}>{children}</span>
        </div>
    );
}

function Spoiler({ children }) {
    const [shown, setShown] = useState(false);
    return (
        <span
            role="button"
            tabIndex={0}
            class={`rounded px-0.5 ${shown ? 'bg-base-300' : 'cursor-pointer bg-base-content text-transparent select-none'}`}
            onClick={() => setShown(true)}
            onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && setShown(true)}
            title={shown ? undefined : 'Spoiler: click to show'}
        >
            {children}
        </span>
    );
}

// firstLine is a text's first non-empty line, for previews.
export function firstLine(text) {
    return (text.split('\n').find((l) => l.trim()) || '')
        .replace(/^> ?/, '')
        .replace(/^```.*/, '[code]')
        .replace(/^\[ \] /, '☐ ')
        .replace(/^\[x\] /, '☑ ');
}
