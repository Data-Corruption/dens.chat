// The markdown subset messages use: **bold**, *italic* or _italic_,
// ~~strikethrough~~, `inline code`, ``` code blocks ```, > quotes,
// ||spoilers||, @mentions and bare http(s) links. There are no [label](url)
// links, so a link always shows where it goes.
//
// Every message comes from a den, and dens are other people's servers. The
// renderer builds Preact nodes whose text is always text: nothing here
// turns a string into markup, and a link's address is only ever an
// http(s) URL the parser matched.

import { useState } from 'preact/hooks';

const URL_RE = /https?:\/\/[^\s<>"'`]+/y;
const NAME_CHAR = /[A-Za-z0-9_]/;

// Markdown renders text as blocks: code blocks, quotes and paragraphs.
export function Markdown({ text, me, onMention }) {
    return <div class="markdown">{blocks(text, { me, onMention })}</div>;
}

// Preview renders a text's first line inline, for places that are already
// a control, like a reply's quote: nothing in it is a link or a button.
// With more, the line ends in an ellipsis in place of its punctuation.
export function Preview({ text, me, more }) {
    let line = firstLine(text);
    if (more) line = line.trimEnd().replace(/[.,;:]+$/, '');
    return <span>{inline(line, { me, preview: true }, 'p')}{more && '…'}</span>;
}

function blocks(text, ctx) {
    const out = [];
    const lines = text.split('\n');
    let para = [];
    let quote = [];
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
                // Trailing punctuation usually ends the sentence, not the URL.
                const url = m[0].replace(/[.,;:!?)\]]+$/, '');
                flush();
                out.push(
                    ctx.preview ? (
                        <span key={`${key}-${n++}`} class="text-primary">{url}</span>
                    ) : (
                        <a key={`${key}-${n++}`} href={url} target="_blank" rel="noopener noreferrer nofollow" class="link link-primary break-all">
                            {url}
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
                out.push(make(inline(text.slice(i + d.length, end), ctx, `${key}-${n}`, d[d.length - 1]), `${key}-${n++}`, ctx));
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
    return (text.split('\n').find((l) => l.trim()) || '').replace(/^> ?/, '').replace(/^```.*/, '[code]');
}
