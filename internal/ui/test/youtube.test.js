// YouTube's players (M4.1): which links get covers, what plays, and the
// setting. Checked without a browser, as markdown.test.js checks the
// renderer.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_VIDEOS, parse, videosIn } from '../assets/js/src/markdown.jsx';
import { onYouTubePlayers, setYouTubePlayers, startSeconds, videoOf, youTubePlayers } from '../assets/js/src/links.js';
import { Cover, embedURL, pictureURL, playerBox, watchURL } from '../assets/js/src/youtube.jsx';
import linkCases from '../../denproto/testdata/links.json';

const ID = 'dQw4w9WgXcQ';
const watch = (id = ID, t) => `https://www.youtube.com/watch?v=${id}${t === undefined ? '' : `&t=${t}`}`;

// nodes lists every element in a tree, depth first, and text is what one
// reads as, as markdown.test.js has them.
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

function text(tree) {
    if (tree == null || typeof tree === 'boolean') return '';
    if (typeof tree !== 'object') return String(tree);
    if (Array.isArray(tree)) return tree.map(text).join('');
    return text(tree.props?.children);
}

// linksIn lists the links the renderer makes of a text, in order.
function linksIn(text) {
    const out = [];
    const walk = (n) => {
        if (n == null || typeof n !== 'object') return;
        if (Array.isArray(n)) return n.forEach(walk);
        if (n.type === 'a') out.push(n.props.href);
        walk(n.props?.children);
    };
    walk(parse(text, {}));
    return out;
}

test("a cover plays exactly the links the den writes in YouTube's form, wherever the renderer finds them", () => {
    let youTube = 0;
    for (const c of linkCases.cases) {
        const written = c.clean ?? c.text;
        // A spoiler's links are links, but get no cover.
        if (written.includes('||')) continue;
        const want = [];
        for (const link of linksIn(written)) {
            const m = /^https:\/\/www\.youtube\.com\/(?:watch\?v=|shorts\/)([A-Za-z0-9_-]{11})(?:&|$)/.exec(link);
            if (m && !want.includes(m[1])) want.push(m[1]);
        }
        youTube += want.length;
        assert.deepEqual(videosIn(written).map((v) => v.id), want.slice(0, MAX_VIDEOS), JSON.stringify(written));
    }
    // The cases hold plenty of YouTube links, so the check above checked
    // something.
    assert.ok(youTube >= 20, `only ${youTube} YouTube links in links.json`);
});

test('every start time the den keeps is read as the seconds it means', () => {
    for (const c of linkCases.cases) {
        for (const [, t] of (c.clean ?? '').matchAll(/youtube\.com\/watch\?v=[\w-]{11}&t=(\w+)/g)) {
            assert.notEqual(videoOf(watch(ID, t)), null, t);
        }
    }
    for (const [t, seconds] of [['0', 0], ['42', 42], ['90s', 90], ['1m30s', 90], ['2m', 120], ['1h', 3600], ['1h2m3s', 3723], ['1h30s', 3630], ['3m0s', 180]]) {
        assert.equal(startSeconds(t), seconds, t);
        assert.deepEqual(videoOf(watch(ID, t)), { id: ID, start: seconds }, t);
    }
    for (const t of ['', 's', 'h', '1m2h', '1h2m3', '1.5', '-1', '1d', '1H', '90ss', '12345678901234567']) {
        assert.equal(startSeconds(t), null, t);
    }
    // Too large to count: the video starts at the beginning.
    assert.equal(startSeconds('9999999999999999'), 0);
});

test("a link in any other form, as a hostile den or sender could send, gets no cover", () => {
    assert.deepEqual(videoOf(watch()), { id: ID, start: 0 });
    for (const link of [
        `https://youtu.be/${ID}`,
        `https://m.youtube.com/watch?v=${ID}`,
        `https://youtube.com/watch?v=${ID}`,
        `http://www.youtube.com/watch?v=${ID}`,
        `https://WWW.YOUTUBE.COM/watch?v=${ID}`,
        `https://www.youtube.com:443/watch?v=${ID}`,
        `https://www.youtube.com./watch?v=${ID}`,
        `https://www.youtube.com.example.org/watch?v=${ID}`,
        `https://user@www.youtube.com/watch?v=${ID}`,
        `https://www.youtube.com/watch?v=${ID}&list=PLx1`,
        `https://www.youtube.com/watch?t=42&v=${ID}`,
        `https://www.youtube.com/watch?v=${ID}&t=42&t=43`,
        `https://www.youtube.com/watch?v=${ID}&t=bad`,
        `https://www.youtube.com/watch?v=${ID}#t=42`,
        `https://www.youtube.com/watch?v=${ID}/`,
        `https://www.youtube.com/watch?v=dQw4w9WgXc`,
        `https://www.youtube.com/watch?v=dQw4w9WgXcQQ`,
        `https://www.youtube.com/watch?v=dQw4w9WgX%51`,
        `https://www.youtube.com/watch?v=dQw4w9Wg"><`,
        `https://www.youtube.com/embed/${ID}`,
        `https://www.youtube-nocookie.com/embed/${ID}`,
    ]) {
        assert.equal(videoOf(link), null, link);
    }
});

test('covers skip code and spoilers, come once per video, and stop at three', () => {
    const other = (n) => watch(`video${n}abcde`);
    for (const [text, want] of [
        [`look ${watch(ID, '1m30s')} and ${watch()}`, [{ id: ID, start: 90 }]],
        [`||${watch()}||`, []],
        [`a ||spoiled ${watch()} here|| b`, []],
        [`**||${watch()}||**`, []],
        [`\`${watch()}\``, []],
        [`\`\`\`\n${watch()}\n\`\`\``, []],
        [`> ${watch()}`, [{ id: ID, start: 0 }]],
        [`[ ] ${watch()}`, [{ id: ID, start: 0 }]],
        [`**${watch()}**`, [{ id: ID, start: 0 }]],
        [`see ${watch(ID, 42)}.`, [{ id: ID, start: 42 }]],
        [`x${watch()}`, []],
        [`${other(1)} ${other(2)} ${other(3)} ${other(4)}`, [1, 2, 3].map((n) => ({ id: `video${n}abcde`, start: 0 }))],
    ]) {
        assert.deepEqual(videosIn(text), want, text);
    }
});

test("a Short plays in a portrait box, as tall as another's is wide, with its own picture", () => {
    const short = `https://www.youtube.com/shorts/${ID}`;
    const video = videoOf(short);
    assert.deepEqual(video, { id: ID, start: 0, short: true });
    assert.equal(playerBox(video), 'aspect-[9/16] w-[225px]');
    assert.equal(playerBox(videoOf(watch())), 'aspect-video w-[400px]');
    assert.equal(pictureURL(video), `/api/youtube/${ID}/portrait`);
    assert.equal(pictureURL(videoOf(watch())), `/api/youtube/${ID}/thumb`);
    // Its links go to it as a Short, and its player is any video's.
    assert.equal(watchURL(video), short);
    assert.equal(embedURL(video), `https://www.youtube-nocookie.com/embed/${ID}?autoplay=1`);
    assert.deepEqual(videosIn(`look ${short} and ${watch()}`), [video]);
    // The den writes a Short in one form; any other gets no cover.
    for (const link of [`https://youtube.com/shorts/${ID}`, `https://m.youtube.com/shorts/${ID}`, `${short}?t=5`, `${short}/`,
        `https://www.youtube.com/shorts/dQw4w9WgXc`, `https://www.youtube.com/shorts/${ID}#t=1`]) {
        assert.equal(videoOf(link), null, link);
    }
});

test("the player's address is built from the checked ID and start time alone", () => {
    assert.equal(embedURL({ id: ID, start: 0 }), `https://www.youtube-nocookie.com/embed/${ID}?autoplay=1`);
    assert.equal(embedURL({ id: ID, start: 90 }), `https://www.youtube-nocookie.com/embed/${ID}?autoplay=1&start=90`);
    assert.equal(watchURL({ id: ID, start: 0 }), watch());
    assert.equal(watchURL({ id: ID, start: 90 }), watch(ID, 90));
    assert.deepEqual(videoOf(watchURL({ id: ID, start: 3723 })), { id: ID, start: 3723 });
});

test('players stay off until the member turns them on, and the page hears when they do', () => {
    assert.equal(youTubePlayers(), false);
    const heard = [];
    const stop = onYouTubePlayers((on) => heard.push(on));
    try {
        setYouTubePlayers(true);
        assert.equal(youTubePlayers(), true);
        setYouTubePlayers(false);
        assert.equal(youTubePlayers(), false);
    } finally {
        stop();
    }
    setYouTubePlayers(true);
    setYouTubePlayers(false);
    assert.deepEqual(heard, [true, false]);
});

// A cover's title and channel (M14.1).
const RICK = { found: true, playable: true, title: 'Never Gonna Give You Up', channel: 'Rick Astley', channel_url: 'https://www.youtube.com/@RickAstleyYT' };

test("a cover's title links to the video and its channel's name to the channel, laid over the button that plays it", () => {
    let played = 0;
    const tree = Cover({ video: { id: ID, start: 42 }, about: RICK, onPlay: () => played++ });
    const all = nodes(tree);
    const links = all.filter((n) => n.type === 'a');
    assert.deepEqual(links.map((a) => [a.props.href, text(a)]), [[watch(ID, 42), RICK.title], [RICK.channel_url, RICK.channel]]);
    for (const a of links) {
        assert.equal(a.props.target, '_blank', a.props.href);
        assert.equal(a.props.rel, 'noopener noreferrer nofollow', a.props.href);
        assert.ok(a.props.class.split(' ').includes('pointer-events-auto'), `${a.props.href} takes the pointer`);
    }
    // A link in the button would play the video as it opened: the words
    // lie beside it, in a layer the pointer passes through but for them.
    const button = all.find((n) => n.type === 'button');
    assert.equal(nodes(button).filter((n) => n.type === 'a').length, 0);
    const layer = all.find((n) => n.type === 'div');
    assert.ok(layer.props.class.split(' ').includes('pointer-events-none'));
    assert.equal(button.props['aria-label'], `Play ${RICK.title}`);
    button.props.onClick();
    assert.equal(played, 1);
    // The play button lights under the pointer alone, not with the hover of
    // the message's row, which is a group of its own.
    const classes = all.flatMap((n) => (typeof n.props?.class === 'string' ? n.props.class.split(' ') : []));
    assert.ok(classes.includes('group/play') && classes.includes('group-hover/play:bg-[#f00]'));
    assert.deepEqual(classes.filter((c) => c === 'group' || /^group-[a-z-]+:/.test(c)), []);
});

test("a channel's name stays text unless its address is on YouTube's own site", () => {
    for (const channel_url of [undefined, '', 'https://example.org/@RickAstleyYT', 'javascript:alert(1)', 'http://www.youtube.com/@RickAstleyYT',
        'https://www.youtube.com.example.org/@RickAstleyYT', 'https://www.youtube.com@example.org/', 'https://m.youtube.com/@RickAstleyYT', 42]) {
        const all = nodes(Cover({ video: { id: ID, start: 0 }, about: { ...RICK, channel_url }, onPlay() {} }));
        assert.deepEqual(all.filter((n) => n.type === 'a').map((a) => a.props.href), [watch()], String(channel_url));
        assert.ok(all.some((n) => n.type === 'span' && text(n) === RICK.channel), String(channel_url));
    }
    // Without a title, there's nothing to lay over the picture.
    const untitled = nodes(Cover({ video: { id: ID, start: 0 }, about: { ...RICK, title: '' }, onPlay() {} }));
    assert.equal(untitled.filter((n) => n.type === 'a').length, 0);
    assert.equal(text(untitled.find((n) => n.type === 'button')), '');
});
