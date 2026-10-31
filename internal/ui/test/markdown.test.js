// The markdown renderer, checked without a browser: components are called
// as functions and the Preact trees they return are inspected directly.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, options, render as mount } from 'preact';
import { Markdown, firstLine, inCodeBlock, parse, preview, sends } from '../assets/js/src/markdown.jsx';
import { setOldReddit, shownLink, trimLink } from '../assets/js/src/links.js';
import { MessageLink } from '../assets/js/src/msglinks.jsx';
import mentions from '../../denproto/testdata/mentions.json';
import taskCases from '../../denproto/testdata/tasks.json';
import linkCases from '../../denproto/testdata/links.json';

// nodes lists every element in a tree, depth first. A component, such as
// the spoiler, is listed as itself, and its children are walked.
function nodes(tree) {
    const out = [];
    const walk = (n) => {
        if (n == null || typeof n !== 'object') return;
        if (Array.isArray(n)) return n.forEach(walk);
        out.push(n);
        walk(n.props?.children);
    };
    walk(tree);
    return out;
}

// text is what a tree reads as: its strings, with line breaks.
function text(tree) {
    let out = '';
    const walk = (n) => {
        if (n == null || typeof n === 'boolean') return;
        if (typeof n !== 'object') {
            out += n;
            return;
        }
        if (Array.isArray(n)) return n.forEach(walk);
        if (n.type === 'br') out += '\n';
        walk(n.props?.children);
    };
    walk(tree);
    return out;
}

const render = (source, me = null) => parse(source, { username: me?.username });
const mentioned = (tree) => nodes(tree).filter((n) => n.props?.['data-mention']);

test('mentions are highlighted exactly where the den counts them', () => {
    for (const c of mentions.cases) {
        const found = mentioned(render(c.text)).map((n) => n.props['data-mention']);
        assert.deepEqual([...new Set(found)], c.mentions, JSON.stringify(c.text));
    }
});

// A link to a message is a chip (M11), a link all the same.
const linksIn = (tree) => nodes(tree).filter((n) => n.type === 'a' || n.type === MessageLink).map((n) => n.props.href || n.props.url);

test('links end exactly where the den finds them to rewrite', () => {
    for (const c of linkCases.cases) {
        assert.deepEqual(linksIn(render(c.text)), c.links, JSON.stringify(c.text));
    }
});

test("a link keeps a ) that closes its own (, and loses one that closes the sentence's", () => {
    assert.equal(trimLink('https://en.wikipedia.org/wiki/Go_(programming_language)'), 'https://en.wikipedia.org/wiki/Go_(programming_language)');
    assert.equal(trimLink('https://en.wikipedia.org/wiki/Go_(programming_language)).'), 'https://en.wikipedia.org/wiki/Go_(programming_language)');
    assert.equal(trimLink('https://x.com/a)'), 'https://x.com/a');
    assert.equal(trimLink('https://x.com/a?!]'), 'https://x.com/a');
});

test('Reddit links show on old.reddit.com in a browser that prefers it', () => {
    const post = 'https://www.reddit.com/comments/1abc2de/comment/k9xyz12';
    assert.equal(shownLink(post, true), 'https://old.reddit.com/comments/1abc2de/comment/k9xyz12');
    assert.equal(shownLink(post, false), post);
    for (const other of ['https://www.reddit.com/r/golang/s/AbCdEf123', 'https://www.reddit.com.example.org/comments/1abc2de', 'https://example.com/comments/1']) {
        assert.equal(shownLink(other, true), other);
    }
    setOldReddit(true);
    try {
        assert.deepEqual(linksIn(render(`see ${post}.`)), ['https://old.reddit.com/comments/1abc2de/comment/k9xyz12']);
        assert.equal(text(render(`see ${post}.`)), 'see https://old.reddit.com/comments/1abc2de/comment/k9xyz12.');
    } finally {
        setOldReddit(false);
    }
});

const tasksIn = (tree) => nodes(tree).filter((n) => n.type?.name === 'Task');

test('task lines are checkboxes exactly where the den finds them, numbered in order', () => {
    for (const c of taskCases.cases) {
        const found = tasksIn(render(c.text));
        assert.deepEqual(found.map((n) => ({ checked: n.props.checked, text: n.props.text })), c.tasks, JSON.stringify(c.text));
        assert.deepEqual(found.map((n) => n.props.n), found.map((_, i) => i));
    }
});

test('tasks tick only for a reader who may edit the message', () => {
    assert.equal(tasksIn(render('[ ] milk'))[0].props.onTask, undefined);
    const onTask = () => Promise.resolve();
    assert.equal(tasksIn(parse('[ ] milk', { onTask }))[0].props.onTask, onTask);
});

test("a mention of the reader looks different from anyone else's", () => {
    const [mine, theirs] = mentioned(render('@alice and @bob', { username: 'alice' }));
    assert.notEqual(mine.props.class, theirs.props.class);
});

// What the renderer may produce; a component is the spoiler.
const ELEMENTS = new Set(['div', 'p', 'br', 'blockquote', 'pre', 'code', 'strong', 'em', 'del', 'span', 'a']);

test('nothing a member writes becomes markup', () => {
    const hostile = '<img src=x onerror=alert(1)> <script>alert(1)</script> &lt;b&gt; <a href="javascript:alert(1)">x</a>';
    const tree = render(hostile);
    for (const n of nodes(tree)) {
        assert.ok(typeof n.type === 'function' || ELEMENTS.has(n.type), `unexpected element ${String(n.type)}`);
        assert.equal(n.props?.dangerouslySetInnerHTML, undefined);
    }
    assert.equal(text(tree), hostile);
    assert.equal(nodes(tree).filter((n) => n.type === 'a').length, 0);
});

test('links are bare http(s) addresses that show where they go and open safely', () => {
    const tree = render('see https://example.com/path?q=1. or http://example.org, not javascript:alert(1) or ftp://x.y');
    const links = nodes(tree).filter((n) => n.type === 'a');
    assert.deepEqual(links.map((a) => a.props.href), ['https://example.com/path?q=1', 'http://example.org']);
    for (const a of links) {
        assert.equal(text(a), a.props.href);
        assert.equal(a.props.target, '_blank');
        assert.deepEqual(a.props.rel.split(' ').sort(), ['nofollow', 'noopener', 'noreferrer']);
    }
});

test("a link to a message is a chip, and only where it's one", () => {
    const den = 'TqFAWIFQdzzjqs54au739ASc4QD6ZJyU-73blg8dqUI';
    const url = `https://den.example.com/#m/${den}/41/90210`;
    const tree = render(`in ${url}, not https://den.example.com/x#m/${den}/41/90210 or \`https://den.example.com/#m/${den}/1/2\``);
    const [chip, ...rest] = nodes(tree).filter((n) => n.type === MessageLink);
    assert.equal(rest.length, 0);
    assert.equal(chip.props.url, url);
    assert.deepEqual(chip.props.link, { den, host: 'den.example.com', channel: '41', message: '90210' });
    assert.deepEqual(nodes(tree).filter((n) => n.type === 'a').map((a) => a.props.href), [`https://den.example.com/x#m/${den}/41/90210`]);
    // A preview holds nothing to click.
    assert.equal(text(preview(url)), url);
    assert.equal(nodes(preview(url)).filter((n) => n.type === MessageLink).length, 0);
});

test('code keeps its text literal', () => {
    const tree = render('`**not bold** @alice`\n```\n# not a heading\n*still literal*\n```');
    assert.deepEqual(nodes(tree).filter((n) => n.type === 'code').map(text), ['**not bold** @alice', '# not a heading\n*still literal*']);
    assert.equal(nodes(tree).filter((n) => n.type === 'strong' || n.type === 'em').length, 0);
    assert.equal(mentioned(tree).length, 0);
});

test('formatting, quotes and spoilers', () => {
    const tree = render('**bold** *it* _it_ ~~gone~~ ||secret||\n> quoted **line**');
    const types = nodes(tree).map((n) => (typeof n.type === 'function' ? n.type.name : n.type));
    for (const t of ['strong', 'em', 'del', 'Spoiler', 'blockquote']) assert.ok(types.includes(t), t);
    assert.equal(text(nodes(tree).find((n) => n.type === 'blockquote')), 'quoted line');
});

test('previews are one line with nothing in them to click', () => {
    const tree = preview('\n\n> Visit https://example.com, ||spoiler||\nsecond line');
    assert.equal(text(tree), 'Visit https://example.com, spoiler');
    for (const n of nodes(tree)) {
        assert.ok(n.type !== 'a' && n.type !== 'button' && typeof n.type !== 'function', `clickable ${String(n.type?.name || n.type)}`);
    }
});

test('a preview with more to show ends in an ellipsis instead of its punctuation', () => {
    assert.equal(text(preview('Everything goes here.\nMore below', null, true)), 'Everything goes here…');
    assert.equal(text(preview('Just one line')), 'Just one line');
});

test('the first line skips blank lines and quote marks, and stands in for code', () => {
    assert.equal(firstLine('\n  \n> quoted\nnext'), 'quoted');
    assert.equal(firstLine('```js\nlet x;\n```'), '[code]');
    assert.equal(firstLine('[x] done\n[ ] not yet'), '☑ done');
    assert.equal(firstLine(''), '');
});

// Hostile lines of 16,000 characters, four times a message's limit: openers
// that never close, links that can't start after a name, and a link ending
// in parentheses. Each took the old parser between 7 and 700 ms; in time
// proportional to its length, each takes about a millisecond.
const HOSTILE = {
    'unclosed *': '* ',
    'unclosed **': '** ',
    'unclosed _': '_ ',
    'unclosed ||': '|| ',
    'every opener': '*_~~|| ',
    'code and openers': '*`',
    'links after a name': 'ahttp://',
    'mentions too long': '@' + 'a'.repeat(40) + ' ',
};

test('hostile text parses in time proportional to its length', () => {
    const texts = Object.entries(HOSTILE).map(([name, unit]) => [name, unit.repeat(Math.ceil(16000 / unit.length)).slice(0, 16000)]);
    texts.push(['trailing parentheses', 'http://a' + ')'.repeat(16000)]);
    for (const [name, line] of texts) {
        for (const shape of [line, `> ${line}`, `[ ] ${line}`]) {
            parse(shape, {});
            const took = Math.min(...[0, 1, 2].map(() => {
                const start = performance.now();
                parse(shape, {});
                preview(shape);
                return performance.now() - start;
            }));
            assert.ok(took < 50, `${name}: ${took.toFixed(1)} ms`);
        }
    }
});

test("a row's parse is kept across renders that don't change its text", () => {
    const root = document.createElement('div');
    const text = '**hi** @alice, see https://example.com\n[ ] milk\n> quoted _too_';
    const row = (props) => mount(h(Markdown, { text, me: { username: 'alice' }, onTask: () => Promise.resolve(), ...props }), root);
    row();
    const shown = root.textContent;
    let made = 0;
    const before = options.vnode;
    options.vnode = (v) => {
        made++;
        before?.(v);
    };
    try {
        // As while the member types: the row renders again, with a new
        // handler and the same reader.
        row({ me: { username: 'alice' } });
        assert.ok(made <= 3, `a render made ${made} nodes`);
        assert.equal(root.textContent, shown);
        made = 0;
        row({ text: `${text}!` });
        assert.ok(made > 10, 'a new text is parsed again');
    } finally {
        options.vnode = before;
    }
});

test("a tick reaches the row's current handler", () => {
    const root = document.createElement('div');
    const ticks = [];
    const handler = (name) => (n, checked, text) => {
        ticks.push([name, n, checked, text]);
        return Promise.resolve();
    };
    mount(h(Markdown, { text: '[ ] milk', onTask: handler('first') }), root);
    mount(h(Markdown, { text: '[ ] milk', onTask: handler('current') }), root);
    root.find((n) => n.localName === 'input').dispatch('change', { currentTarget: { checked: true } });
    assert.deepEqual(ticks, [['current', 0, true, 'milk']]);
    mount(h(Markdown, { text: '[ ] milk' }), root);
    assert.equal(root.find((n) => n.localName === 'input').attributes.disabled, 'true');
});

test('a caret is in a code block between a fence and the next', () => {
    const at = (marked) => inCodeBlock(marked.replace('|', ''), marked.indexOf('|'));
    assert.equal(at('hello|'), false);
    assert.equal(at('```|'), true, 'just after an opening fence');
    assert.equal(at('```js|'), true, 'after a language');
    assert.equal(at('```\nlet x = 1|'), true);
    assert.equal(at('```\nlet x = 1\n```|'), false, 'just after the closing fence');
    assert.equal(at('```\na\n```\nafter|'), false);
    assert.equal(at('```\na|\n```'), true, 'inside a closed block');
    assert.equal(at('  ```\nindented fence|'), true, 'a fence after spaces, as parse reads it');
    assert.equal(at('see ``` here|'), false, 'three backticks inside a line open nothing');
    assert.equal(at('``|`'), false);
});

test('Enter sends outside a code block, Shift+Enter never does, and Ctrl+Enter always does', () => {
    const key = (marked, mods = {}) => {
        const value = marked.replace('|', '');
        return sends({ key: 'Enter', isComposing: false, currentTarget: { value, selectionStart: marked.indexOf('|') }, ...mods });
    };
    assert.equal(key('hi|'), true);
    assert.equal(key('hi|', { shiftKey: true }), false);
    assert.equal(key('```\ncode|'), false);
    assert.equal(key('```\ncode|', { ctrlKey: true }), true);
    assert.equal(key('```\ncode\n```|'), true);
    assert.equal(key('hi|', { isComposing: true }), false, 'an input method composing keeps Enter');
    assert.equal(sends({ key: 'a', currentTarget: { value: '', selectionStart: 0 } }), false);
});

test("with a finger, whose keyboard has no Shift, ↵ starts a line, and only Ctrl+Enter sends", () => {
    const key = (marked, mods = {}) => {
        const value = marked.replace('|', '');
        return sends({ key: 'Enter', isComposing: false, currentTarget: { value, selectionStart: marked.indexOf('|') }, ...mods }, true);
    };
    assert.equal(key('hi|'), false);
    assert.equal(key('```\ncode\n```|'), false);
    assert.equal(key('hi|', { ctrlKey: true }), true, "a tablet's keyboard");
    assert.equal(key('hi|', { metaKey: true }), true);
});
