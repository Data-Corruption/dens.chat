// Links to events (M17.5): reading one, what its chip reads, what its card
// shows, and where a click leads, or why it can't.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { eventLink, eventLinkTo, messageLink } from '../assets/js/src/links.js';
import { EVENT_ENDED, EVENT_GONE, EventLink, eventChipLabel, followEvent, linkedEvent } from '../assets/js/src/eventlinks.jsx';
import { notJoined } from '../assets/js/src/msglinks.jsx';
import { eventsIn, MAX_EVENT_CARDS } from '../assets/js/src/markdown.jsx';
import { Badges } from '../assets/js/src/people.jsx';

const HOME = 'TqFAWIFQdzzjqs54au739ASc4QD6ZJyU-73blg8dqUI';
const OTHER = '2SmKENGwc1g33EvYXaxkGw887yekfl1TpU8vP1svz_o';
const ELSEWHERE = 'ext2Puj2LriOR0KnYPkS0LGbzViyuUiZl4S6zBWn9Nc';
const NOW = Date.UTC(2026, 10, 6, 18, 30);
const HOUR = 3_600_000;

function find(node, match) {
    if (match(node)) return node;
    for (const child of node.childNodes || []) {
        const found = find(child, match);
        if (found) return found;
    }
    return null;
}

function findAll(node, match, out = []) {
    if (match(node)) out.push(node);
    for (const child of node.childNodes || []) findAll(child, match, out);
    return out;
}

test("a link to an event is a den's address with the den and the event in the fragment", () => {
    assert.deepEqual(eventLink(`https://den.example.com/#e/${HOME}/12`), { den: HOME, host: 'den.example.com', event: '12' });
    assert.equal(eventLink(`https://Den.Example.com#e/${HOME}/12`).host, 'den.example.com');
    assert.equal(eventLink(`http://127.0.0.1:8485/#e/${HOME}/7`).host, '127.0.0.1:8485');
    assert.equal(eventLink(`https://den.example.com/#e/${HOME}/9223372036854775807`).event, '9223372036854775807');
    for (const other of [
        `https://den.example.com/x#e/${HOME}/12`, `https://den.example.com/?q=1#e/${HOME}/12`, `https://den.example.com/#e/${HOME}/012`,
        `https://den.example.com/#e/${HOME}/12/`, `https://den.example.com/#e/${HOME}`, `https://den.example.com/#e/${HOME}/1a`,
        `https://u@den.example.com/#e/${HOME}/12`, `https://den.example.com/#e/${HOME}/9223372036854775808`, `ftp://den.example.com/#e/${HOME}/12`,
        `https://den.example.com/#m/${HOME}/12`, `https://den.example.com/#e/${HOME.slice(1)}/12`, `https://den.example.com/#e/${HOME}x/12`,
    ]) {
        assert.equal(eventLink(other), null, other);
    }
    const link = eventLinkTo('https://den.example.com', HOME, '12');
    assert.equal(link, `https://den.example.com/#e/${HOME}/12`);
    assert.equal(eventLink(link).event, '12');
    assert.equal(messageLink(link), null, 'an event link is no message link');
});

const view = {
    me: { id: '1' },
    channels: [{ id: '42', kind: 'voice', name: 'lounge' }],
    events: [{ id: '12', name: 'Game night', channel_id: '42', starts_at: NOW + HOUR, ends_at: NOW + 2 * HOUR, interested: 2, mine: true }],
};
const dens = [
    { den_id: HOME, name: 'Home', url: 'https://den.example.com' },
    { den_id: OTHER, name: 'Other', url: 'https://other.example' },
];
const links = { denID: HOME, view, dens, openEvent: (den, id) => opened.push(`${den}/${id}`) };
let opened = [];

test("a link's event comes from the open den's state, else as its Dens read it, and one past its end by the clock shows as ended", () => {
    const read = new Map();
    const lookup = (den, id) => read.get(`${den}/${id}`);
    const at = (url) => linkedEvent(eventLink(url), links, NOW, lookup);
    assert.equal(at(`https://den.example.com/#e/${HOME}/12`).event.name, 'Game night');
    assert.equal(at(`https://den.example.com/#e/${HOME}/13`).state, 'loading');
    assert.equal(at(`https://elsewhere.example/#e/${ELSEWHERE}/1`).state, 'unjoined');
    read.set(`${HOME}/13`, { state: 'shown', event: { id: '13', name: 'Over', starts_at: NOW - 2 * HOUR, ends_at: NOW - HOUR, ended: true } });
    read.set(`${OTHER}/5`, { state: 'shown', event: { id: '5', name: 'Quiz', starts_at: NOW - 2 * HOUR, ends_at: NOW - HOUR } });
    read.set(`${HOME}/14`, { state: 'gone', event: null });
    assert.equal(at(`https://den.example.com/#e/${HOME}/13`).state, 'ended');
    assert.equal(at(`https://other.example/#e/${OTHER}/5`).state, 'ended', 'kept while the den was away, past its end');
    assert.equal(at(`https://den.example.com/#e/${HOME}/14`).state, 'gone');

    const label = (url) => eventChipLabel(eventLink(url), at(url), HOME);
    assert.equal(label(`https://den.example.com/#e/${HOME}/12`), 'Game night');
    assert.equal(label(`https://old.example/#e/${HOME}/12`), 'Game night', 'by its den, wherever the den was');
    assert.equal(label(`https://other.example/#e/${OTHER}/5`), 'Other › Quiz');
    assert.equal(label(`https://den.example.com/#e/${HOME}/14`), 'an event');
    assert.equal(label(`https://den.example.com/#e/${HOME}/15`), '…');
    assert.equal(label(`https://elsewhere.example/#e/${ELSEWHERE}/1`), 'elsewhere.example');

    opened = [];
    const follow = (url) => followEvent(eventLink(url), at(url), links);
    assert.equal(follow(`https://den.example.com/#e/${HOME}/12`), '');
    assert.deepEqual(opened, [`${HOME}/12`]);
    assert.equal(follow(`https://den.example.com/#e/${HOME}/13`), EVENT_ENDED);
    assert.equal(follow(`https://den.example.com/#e/${HOME}/14`), EVENT_GONE);
    assert.equal(follow(`https://elsewhere.example/#e/${ELSEWHERE}/1`), notJoined(eventLink(`https://elsewhere.example/#e/${ELSEWHERE}/1`)));
    assert.deepEqual(opened, [`${HOME}/12`]);
});

test("a message's event cards are its links' events, each once, none in code or a spoiler, and three at most", () => {
    const url = (id) => `https://den.example.com/#e/${HOME}/${id}`;
    assert.deepEqual(eventsIn(`come ${url(1)} and ${url(1)}, or ${url(2)}`).map((e) => e.event), ['1', '2']);
    assert.deepEqual(eventsIn(`\`${url(1)}\` ||${url(2)}|| ${url(3)}`).map((e) => e.event), ['3']);
    assert.equal(eventsIn([1, 2, 3, 4, 5].map(url).join(' ')).length, MAX_EVENT_CARDS);
    assert.deepEqual(eventsIn(`https://den.example.com/#m/${HOME}/41/90210`), []);
});

test("outside a den's page an event link is an ordinary link", () => {
    const root = document.createElement('div');
    const url = `https://den.example.com/#e/${HOME}/12`;
    mount(h(EventLink, { url, link: eventLink(url) }), root);
    const a = find(root, (n) => n.localName === 'a');
    assert.equal(a.attributes.href, url);
    assert.equal(a.textContent, url);
    mount(null, root);
});

test("a profile's badges are a row of pictures, each with a tip saying its title, event and day", () => {
    const root = document.createElement('div');
    const badges = [
        { event_id: '12', title: 'Night owl', image: { id: '30', width: 128, height: 128 }, event: 'Game night', starts_at: Date.UTC(2026, 10, 6, 12), earned_at: 2 },
        { event_id: '9', title: 'Was there', image: { id: '31', width: 128, height: 128, animated: true }, event: 'Quiz', starts_at: Date.UTC(2026, 9, 1, 12), earned_at: 1 },
    ];
    mount(h(Badges, { denID: HOME, badges }), root);
    const items = findAll(root, (n) => n.attributes?.['data-badge'] !== undefined);
    assert.deepEqual(items.map((n) => n.attributes['data-badge']), ['12', '9']);
    const tips = findAll(root, (n) => n.attributes?.role === 'tooltip').map((n) => n.textContent);
    assert.equal(tips.length, 2);
    assert.match(tips[0], /^Night owlGame night, Nov 6, 2026$/);
    assert.match(tips[1], /^Was thereQuiz, Oct 1, 2026$/);
    const imgs = findAll(root, (n) => n.localName === 'img').map((n) => n.attributes.src);
    assert.ok(imgs.includes(`/api/dens/${HOME}/files/30`), imgs.join(' '));
    mount(null, root);
});
