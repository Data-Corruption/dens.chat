// YouTube's players (M4.1): which links get covers, what plays, and the
// setting. Checked without a browser, as markdown.test.js checks the
// renderer.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_VIDEOS, Markdown, videosIn } from '../assets/js/src/markdown.jsx';
import { onYouTubePlayers, setYouTubePlayers, startSeconds, videoOf, youTubePlayers } from '../assets/js/src/links.js';
import { embedURL, watchURL } from '../assets/js/src/youtube.jsx';
import linkCases from '../../denproto/testdata/links.json';

const ID = 'dQw4w9WgXcQ';
const watch = (id = ID, t) => `https://www.youtube.com/watch?v=${id}${t === undefined ? '' : `&t=${t}`}`;

// linksIn lists the links the renderer makes of a text, in order.
function linksIn(text) {
    const out = [];
    const walk = (n) => {
        if (n == null || typeof n !== 'object') return;
        if (Array.isArray(n)) return n.forEach(walk);
        if (n.type === 'a') out.push(n.props.href);
        walk(n.props?.children);
    };
    walk(Markdown({ text }));
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
            const m = /^https:\/\/www\.youtube\.com\/watch\?v=([^&]+)/.exec(link);
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
