// Steam's cards: which links get one, and how a card says what Steam
// told the member's Dens. Checked without a browser.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_GAMES, gamesIn } from '../src/chat/markdown.jsx';
import { onSteamCards, setSteamCards, steamApp, steamCards } from '../src/chat/links.js';
import { count, deckLook, reviewLook, systems, when } from '../src/chat/steam.jsx';

test("a link to a game's store page, its age check or its community hub names its app, and nothing else does", () => {
    const yes = {
        'https://store.steampowered.com/app/1771300/Kingdom_Come_Deliverance_II/': '1771300',
        'https://store.steampowered.com/app/730': '730',
        'https://store.steampowered.com/app/730/?l=german': '730',
        'https://store.steampowered.com/agecheck/app/1091500/': '1091500',
        'https://steamcommunity.com/app/730/discussions/': '730',
        'https://store.steampowered.com/app/4294967295': '4294967295',
    };
    for (const [url, app] of Object.entries(yes)) assert.equal(steamApp(url), app, url);
    for (const url of [
        'http://store.steampowered.com/app/730/',
        'https://store.steampowered.com/app/0730/',
        'https://store.steampowered.com/app/4294967296/',
        'https://store.steampowered.com/sub/54029/',
        'https://store.steampowered.com/bundle/232/',
        'https://store.steampowered.com.evil.example/app/730/',
        'https://evil.example/store.steampowered.com/app/730/',
        'https://store.steampowered.com/app/730x/',
        'steam://store/730',
    ]) assert.equal(steamApp(url), null, url);
});

test('a message gets a card for each game it links, once each and three at most, but none in code or a spoiler', () => {
    const link = (id) => `https://store.steampowered.com/app/${id}/`;
    assert.deepEqual(gamesIn(`try ${link(730)} and ${link(730)}`), ['730']);
    assert.deepEqual(gamesIn(`\`${link(730)}\` ||${link(570)}||`), []);
    assert.deepEqual(gamesIn([1, 2, 3, 4].map(link).join(' ')), ['1', '2', '3']);
    assert.equal(MAX_GAMES, 3);
});

test("Steam's cards are off until the member turns them on, and say so to whoever listens", () => {
    assert.equal(steamCards(), false);
    const heard = [];
    const stop = onSteamCards((on) => heard.push(on));
    setSteamCards(true);
    assert.equal(steamCards(), true);
    stop();
    setSteamCards(false);
    assert.deepEqual(heard, [true]);
});

test("a card names reviews as Steam does, marks them green, amber or red, and shortens a large count", () => {
    assert.deepEqual(reviewLook({ score: 9, percent: 97, count: 288031 }), { name: 'Overwhelmingly Positive', tone: 'text-success' });
    assert.deepEqual(reviewLook({ score: 5 }), { name: 'Mixed', tone: 'text-warning' });
    assert.deepEqual(reviewLook({ score: 2 }), { name: 'Very Negative', tone: 'text-error' });
    for (const bad of [undefined, {}, { score: 0 }, { score: 10 }, { score: '9' }]) assert.equal(reviewLook(bad), null);
    assert.equal(count(7694, 'en-US'), '7,694');
    assert.equal(count(148458, 'en-US'), '148.5K');
});

test('a card lists the systems a game runs on, and Steam Deck by its rating', () => {
    assert.deepEqual(systems({ platforms: { windows: true, linux: true } }), ['Windows', 'Linux']);
    assert.deepEqual(systems({}), []);
    assert.deepEqual(deckLook('verified'), { name: 'Verified', tone: 'text-success' });
    assert.equal(deckLook('playable').name, 'Playable');
    assert.equal(deckLook('nonsense'), null);
});

test("a card's date is a sale's end while it runs, or when the game came out, or that it's coming", () => {
    const now = Date.UTC(2026, 10, 16, 12);
    const at = (y, m, d) => Date.UTC(y, m, d, 12) / 1000;
    assert.equal(when({ price: { discount: 67, ends: at(2026, 10, 23) }, released: at(2025, 1, 4) }, now, 'en-US'), 'Sale ends Nov 23');
    assert.equal(when({ price: { discount: 67, ends: at(2026, 10, 1) }, released: at(2025, 1, 4) }, now, 'en-US'), 'Released Feb 4, 2025', 'a sale that ended');
    assert.equal(when({ released: at(2026, 3, 1) }, now, 'en-US'), 'Released Apr 1');
    assert.equal(when({ coming_soon: true, release_note: 'Q1 2027' }, now, 'en-US'), 'Q1 2027');
    assert.equal(when({ coming_soon: true }, now, 'en-US'), 'Coming soon');
    assert.equal(when({}, now, 'en-US'), '');
});
