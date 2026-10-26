// Links to messages (M11): reading one, what its chip reads, and where a
// click leads, or why it can't, against a fake local service.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { linkTo, messageLink } from '../assets/js/src/links.js';
import { CHANNEL_GONE, LinkContext, MESSAGE_GONE, MessageLink, chipLabel, follow, notJoined, place } from '../assets/js/src/msglinks.jsx';

// Dens' IDs, 32 bytes in base64url.
const HOME = 'TqFAWIFQdzzjqs54au739ASc4QD6ZJyU-73blg8dqUI';
const OTHER = '2SmKENGwc1g33EvYXaxkGw887yekfl1TpU8vP1svz_o';
const ELSEWHERE = 'ext2Puj2LriOR0KnYPkS0LGbzViyuUiZl4S6zBWn9Nc';

test("a link to a message is a den's address with the den, channel and message in the fragment", () => {
    assert.deepEqual(messageLink(`https://den.example.com/#m/${HOME}/41/90210`),
        { den: HOME, host: 'den.example.com', channel: '41', message: '90210' });
    assert.equal(messageLink(`https://Den.Example.com#m/${HOME}/41/90210`).host, 'den.example.com');
    assert.equal(messageLink(`https://den.example.com:8443/#m/${HOME}/41/90210`).host, 'den.example.com:8443');
    assert.equal(messageLink(`http://127.0.0.1:8485/#m/${HOME}/7/8`).host, '127.0.0.1:8485');
    assert.equal(messageLink(`https://den.example.com/#m/${HOME}/9223372036854775807/1`).channel, '9223372036854775807');
    for (const other of [
        `https://den.example.com/x#m/${HOME}/41/90210`, `https://den.example.com/?q=1#m/${HOME}/41/90210`, `https://den.example.com/#m/${HOME}/041/90210`,
        `https://den.example.com/#m/${HOME}/41/90210/`, `https://den.example.com/#m/${HOME}/41`, `https://den.example.com/#m/${HOME}/41/9a`,
        `https://u@den.example.com/#m/${HOME}/41/90210`, `https://den.example.com/#m/${HOME}/9223372036854775808/1`,
        `https://den.example.com/#m/${HOME}/1/12345678901234567890`, `ftp://den.example.com/#m/${HOME}/41/90210`, `https://den.example.com/#x/${HOME}/41/90210`,
        'https://den.example.com/#m/41/90210', `https://den.example.com/#m/${HOME.slice(1)}/41/90210`, `https://den.example.com/#m/${HOME}x/41/90210`,
        `https://den.example.com/#m/${HOME.slice(1)}=/41/90210`,
    ]) {
        assert.equal(messageLink(other), null, other);
    }
    const link = linkTo('https://den.example.com', HOME, '41', '90210');
    assert.equal(link, `https://den.example.com/#m/${HOME}/41/90210`);
    assert.equal(messageLink(link).message, '90210');
});

const here = {
    me: { id: '1' },
    members: [{ id: '1', username: 'alice' }, { id: '2', username: 'bob' }],
    channels: [
        { id: '41', kind: 'text', name: 'general' },
        { id: '42', kind: 'voice', name: 'lounge' },
        { id: '90', kind: 'dm', members: ['1', '2'] },
    ],
};
const there = { me: { id: '7' }, members: [], channels: [{ id: '5', kind: 'text', name: 'random' }] };
const dens = [
    { den_id: HOME, name: 'Home', url: 'https://den.example.com' },
    { den_id: OTHER, name: 'Other', url: 'https://other.example' },
];
const links = { denID: HOME, view: here, dens };

const label = (url, viewOf = () => there) => chipLabel(messageLink(url), place(messageLink(url), links, viewOf), HOME);

test("a link's chip names its channel, and the den when it's another", () => {
    assert.equal(label(`https://den.example.com/#m/${HOME}/41/1`), '#general');
    assert.equal(label(`https://den.example.com/#m/${HOME}/90/1`), '@bob');
    assert.equal(label(`https://other.example/#m/${OTHER}/5/1`), 'Other › #random');
    // A channel gone and one out of sight look alike, and a voice channel
    // holds no messages.
    assert.equal(label(`https://den.example.com/#m/${HOME}/99/1`), '#unknown');
    assert.equal(label(`https://den.example.com/#m/${HOME}/42/1`), '#unknown');
    assert.equal(label(`https://other.example/#m/${OTHER}/6/1`), 'Other › #unknown');
    assert.equal(label(`https://other.example/#m/${OTHER}/5/1`, () => null), 'Other › …', "while the other den's state is on its way");
    assert.equal(label(`https://other.example/#m/${OTHER}/5/1`, () => false), 'Other', "when it couldn't be fetched");
    assert.equal(label(`https://elsewhere.example/#m/${ELSEWHERE}/5/1`), 'elsewhere.example', "a den the member hasn't joined");
    // A link opens by the den's ID: one made before its den moved still
    // does, and one to an address another den took doesn't open there.
    assert.equal(label(`https://old.example/#m/${HOME}/41/1`), '#general');
    assert.equal(label(`https://den.example.com/#m/${ELSEWHERE}/41/1`), 'den.example.com');
    // The den open is known by its state before the list of dens comes.
    const early = { denID: HOME, view: { ...here, url: 'https://den.example.com', name: 'Home' }, dens: [] };
    const link = messageLink(`https://den.example.com/#m/${HOME}/41/1`);
    assert.equal(chipLabel(link, place(link, early), HOME), '#general');
});

// service answers the page's requests as the local service would, from
// answers by path, and records them; a function answers with what it
// returns, or fails by throwing.
function service(answers) {
    const asked = [];
    globalThis.fetch = async (path) => {
        asked.push(path);
        let status = 200;
        let body = answers[path];
        if (typeof body === 'function') {
            try {
                body = body();
            } catch (e) {
                status = 503;
                body = { error: e.message };
            }
        }
        if (body === undefined) [status, body] = [404, { error: 'no such thing' }];
        return { ok: status < 400, status, headers: { get: () => null }, text: async () => JSON.stringify(body) };
    };
    return asked;
}

function opener() {
    const opened = [];
    return { opened, links: { ...links, open: (...args) => opened.push(args) } };
}

const around = (den, channel, message) => `/api/dens/${den}/channels/${channel}/messages?around=${message}&limit=1`;

test("a link opens its message, or says why it can't", async () => {
    const { opened, links: following } = opener();
    service({
        [around(HOME, '41', '100')]: { messages: [{ id: '100' }] },
        // Around a message that's gone, the page starts at the next.
        [around(HOME, '41', '101')]: { messages: [{ id: '102' }] },
        [around(HOME, '41', '103')]: { messages: [] },
        [`/api/dens/${OTHER}/state`]: there,
        [around(OTHER, '5', '200')]: { messages: [{ id: '200' }] },
    });
    const open = (url) => follow(messageLink(url), following);
    assert.equal(await open(`https://den.example.com/#m/${HOME}/41/100`), '');
    assert.equal(await open(`https://den.example.com/#m/${HOME}/41/101`), MESSAGE_GONE);
    assert.equal(await open(`https://den.example.com/#m/${HOME}/41/103`), MESSAGE_GONE);
    assert.equal(await open(`https://den.example.com/#m/${HOME}/42/1`), CHANNEL_GONE);
    assert.equal(await open(`https://other.example/#m/${OTHER}/6/200`), CHANNEL_GONE);
    assert.equal(await open(`https://other.example/#m/${OTHER}/5/200`), '');
    assert.equal(await open(`https://old.example/#m/${HOME}/41/100`), '', 'made before the den moved');
    const away = messageLink(`https://elsewhere.example/#m/${ELSEWHERE}/5/200`);
    assert.equal(await follow(away, following), notJoined(away));
    assert.equal(notJoined(away), "You haven't joined the den at elsewhere.example.");
    const taken = messageLink(`https://den.example.com/#m/${ELSEWHERE}/41/100`);
    assert.equal(await follow(taken, following), "You haven't joined the den at den.example.com.", 'an address another den took');
    assert.deepEqual(opened, [[HOME, '41', '100'], [OTHER, '5', '200'], [HOME, '41', '100']]);
});

test("a link into a den that's offline opens the channel, which says why the message isn't there", async () => {
    const { opened, links: following } = opener();
    service({
        [around(HOME, '41', '100')]: () => {
            throw new Error("This den is offline, and this computer hasn't kept these messages.");
        },
    });
    assert.equal(await follow(messageLink(`https://den.example.com/#m/${HOME}/41/100`), following), '');
    assert.deepEqual(opened, [[HOME, '41', '100']]);
});

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

test("a chip opens where it points on a click, and says why when it can't", async () => {
    const { opened, links: following } = opener();
    service({ [around(HOME, '41', '100')]: { messages: [{ id: '100' }] }, [around(HOME, '41', '101')]: { messages: [] } });
    const root = document.createElement('div');
    const chip = (url) => h(MessageLink, { url, link: messageLink(url) });
    const [first, second] = [`https://den.example.com/#m/${HOME}/41/100`, `https://den.example.com/#m/${HOME}/41/101`];
    mount(h(LinkContext.Provider, { value: following }, chip(first), chip(second)), root);
    const [open, gone] = root.childNodes;
    assert.equal(open.localName, 'a');
    assert.equal(open.attributes.href, first);
    assert.equal(open.attributes.title, first, 'the address shows on hover');
    assert.equal(open.textContent, '#general');
    let prevented = 0;
    const click = { preventDefault: () => prevented++ };
    open.dispatch('click', click);
    gone.dispatch('click', click);
    // A click with Ctrl opens the address in a new tab, as the browser would.
    open.dispatch('click', { ...click, ctrlKey: true });
    await tick();
    await tick();
    assert.equal(prevented, 2);
    assert.deepEqual(opened, [[HOME, '41', '100']]);
    assert.equal(root.find((n) => n.attributes.role === 'status')?.textContent, MESSAGE_GONE);
    mount(null, root);
});

test('outside a den, a link to a message is an ordinary link', () => {
    const root = document.createElement('div');
    const url = `https://den.example.com/#m/${HOME}/41/100`;
    mount(h(MessageLink, { url, link: messageLink(url) }), root);
    const a = root.childNodes[0];
    assert.equal(a.attributes.target, '_blank');
    assert.equal(a.textContent, url);
    mount(null, root);
});
