// The markdown renderer, checked without a browser: components are called
// as functions and the Preact trees they return are inspected directly.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Markdown, Preview, firstLine } from '../assets/js/src/markdown.jsx';
import mentions from '../../denproto/testdata/mentions.json';

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

const render = (source, me = null) => Markdown({ text: source, me });
const mentioned = (tree) => nodes(tree).filter((n) => n.props?.['data-mention']);

test('mentions are highlighted exactly where the den counts them', () => {
    for (const c of mentions.cases) {
        const found = mentioned(render(c.text)).map((n) => n.props['data-mention']);
        assert.deepEqual([...new Set(found)], c.mentions, JSON.stringify(c.text));
    }
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
    const tree = Preview({ text: '\n\n> Visit https://example.com, ||spoiler||\nsecond line' });
    assert.equal(text(tree), 'Visit https://example.com, spoiler');
    for (const n of nodes(tree)) {
        assert.ok(n.type !== 'a' && n.type !== 'button' && typeof n.type !== 'function', `clickable ${String(n.type?.name || n.type)}`);
    }
});

test('a preview with more to show ends in an ellipsis instead of its punctuation', () => {
    assert.equal(text(Preview({ text: 'Everything goes here.\nMore below', more: true })), 'Everything goes here…');
    assert.equal(text(Preview({ text: 'Just one line' })), 'Just one line');
});

test('the first line skips blank lines and quote marks, and stands in for code', () => {
    assert.equal(firstLine('\n  \n> quoted\nnext'), 'quoted');
    assert.equal(firstLine('```js\nlet x;\n```'), '[code]');
    assert.equal(firstLine(''), '');
});
