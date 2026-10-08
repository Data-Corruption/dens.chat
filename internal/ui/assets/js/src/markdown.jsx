// The markdown subset messages use: **bold**, *italic* or _italic_,
// ~~strikethrough~~, `inline code`, ``` code blocks ```, > quotes,
// ||spoilers||, @mentions, bare http(s) links, and task lines that start
// with [ ] or [x]. There are no [label](url) links, so a link always shows
// where it goes.
//
// Every message comes from a den, and dens are other people's servers. The
// renderer builds Preact nodes whose text is always text: nothing here
// turns a string into markup, and a link's address is only ever an
// http(s) URL the parser matched.

import { useEffect, useState } from 'preact/hooks';
import { shownLink, trimLink, videoOf } from './links.js';

const URL_RE = /https?:\/\/[^\s<>"'`]+/y;
const NAME_CHAR = /[A-Za-z0-9_]/;
// A task line, as denproto.Tasks finds one outside code blocks.
const TASK_RE = /^\[( |x)\] /;

// Markdown renders text as blocks: code blocks, quotes, task lines and
// paragraphs. With onTask, the reader may tick tasks: onTask(n, checked,
// text) returns a promise that settles once the den has the tick.
export function Markdown({ text, me, onMention, onTask }) {
    return <div class="markdown">{blocks(text, { me, onMention, onTask })}</div>;
}

// Preview renders a text's first line inline, for places that are already
// a control, like a reply's quote: nothing in it is a link or a button.
// With more, the line ends in an ellipsis in place of its punctuation.
export function Preview({ text, me, more }) {
    let line = firstLine(text);
    if (more) line = line.trimEnd().replace(/[.,;:]+$/, '');
    return <span>{inline(line, { me, preview: true }, 'p')}{more && '…'}</span>;
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
                    {inline(label, ctx, `t${out.length}`)}
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
        out.push(...inline(line, ctx, `l${i}`));
    });
    return out;
}

const DELIMITERS = [
    ['**', (c, k) => <strong key={k}>{c}</strong>],
    ['~~', (c, k) => <del key={k}>{c}</del>],
    ['||', (c, k, ctx) => (ctx.preview ? <span key={k} class="rounded bg-base-content/40 px-0.5 text-transparent select-none">{c}</span> : <Spoiler key={k}>{c}</Spoiler>)],
    ['*', (c, k) => <em key={k}>{c}</em>],
    ['_', (c, k) => <em key={k}>{c}</em>],
];

// inline renders one line's inline syntax. Code spans are found first and
// kept literal; everything else can nest. prev is the character before
// text in its line, so links and mentions start where denproto.Mentions
// says they do, nested or not.
function inline(text, ctx, key, prev = '') {
    const out = [];
    let plain = '';
    let n = 0;
    const flush = () => {
        if (plain) out.push(plain);
        plain = '';
    };
    for (let i = 0; i < text.length;) {
        const c = text[i];
        const before = i === 0 ? prev : text[i - 1];
        if (c === '`') {
            const end = text.indexOf('`', i + 1);
            if (end > i + 1) {
                flush();
                out.push(<code key={`${key}-${n++}`} class="rounded bg-base-300 px-1 text-sm">{text.slice(i + 1, end)}</code>);
                i = end + 1;
                continue;
            }
        }
        if (c === 'h') {
            URL_RE.lastIndex = i;
            const m = URL_RE.exec(text);
            if (m && !NAME_CHAR.test(before)) {
                const url = trimLink(m[0]);
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
            while (j < text.length && NAME_CHAR.test(text[j])) j++;
            const name = text.slice(i + 1, j);
            if (name.length >= 2 && name.length <= 32) {
                flush();
                const self = ctx.me && name.toLowerCase() === ctx.me.username;
                out.push(
                    <span
                        key={`${key}-${n++}`}
                        data-mention={name.toLowerCase()}
                        class={`rounded px-0.5 font-medium ${self ? 'bg-warning/30' : 'bg-primary/15 text-primary'}`}
                        onClick={ctx.onMention && !ctx.preview ? () => ctx.onMention(name.toLowerCase()) : undefined}
                    >
                        @{name}
                    </span>,
                );
                i = j;
                continue;
            }
        }
        const matched = DELIMITERS.find(([d]) => text.startsWith(d, i));
        if (matched) {
            const [d, make] = matched;
            const end = findClose(text, d, i + d.length);
            if (end > i + d.length) {
                flush();
                const inner = d === '||' ? { ...ctx, spoiler: true } : ctx;
                out.push(make(inline(text.slice(i + d.length, end), inner, `${key}-${n}`, d[d.length - 1]), `${key}-${n++}`, ctx));
                i = end + d.length;
                continue;
            }
        }
        plain += c;
        i++;
    }
    flush();
    return out;
}

// findClose finds the delimiter that closes one opened at start, skipping
// code spans, or returns -1.
function findClose(text, d, start) {
    for (let i = start; i < text.length; i++) {
        if (text[i] === '`') {
            const end = text.indexOf('`', i + 1);
            if (end > i) {
                i = end;
                continue;
            }
        }
        if (text.startsWith(d, i) && !/\s/.test(text[i - 1])) return i;
    }
    return -1;
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
