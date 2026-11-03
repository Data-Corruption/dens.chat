// Emoji (M15.2), checked without a browser: the rule that finds the den's
// emoji in text, as the renderer shows it; a message of emoji alone; the
// picker's search and skin tone; the member's most used; and completion.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { DenEmoji } from '../assets/js/src/denemoji.jsx';
import {
    HALF_LIFE, KEEP, MAX_BIG, QUICK, TONES, cleanName, completeEmoji, completeMember, completionAt, denKey, emojiAlone, emojiIn, emojiList,
    inTone, mostUsed, nameAt, nameFromFile, noteSent, noteUses, quickRow, scoreOf, searchDen, searchUnicode, setEmojiList, setSkinTone,
    skinTone, unicodeKey, validName,
} from '../assets/js/src/emoji.js';
import { parse, preview } from '../assets/js/src/markdown.jsx';
import list from '../assets/emoji.json';

setEmojiList(list);

// storage stands in for the browser's.
function storage() {
    const kept = new Map();
    globalThis.localStorage = { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, String(v)) };
    return kept;
}

// The den's emoji, as its state names them.
const emojiOf = (id, name, animated = false) => ({ id, name, image: { id: `9${id}`, width: 64, height: 64, animated }, added_by: '1', added_at: 1 });
const PARTY = emojiOf('1', 'party');
const WAVE = emojiOf('2', 'wave', true);
const DEN = [PARTY, WAVE];
const byName = new Map(DEN.map((e) => [e.name, e]));

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

const shown = (tree) => nodes(tree).filter((n) => n.type === DenEmoji).map((n) => n.props.emoji.name);
const render = (text, big = false) => parse(text, { emoji: byName, big });

// The rule, against cases of its own: a name the den has, between colons
// that no letter, digit or _ touches from outside, never in code or a link.
const RULE = [
    [':party:', ['party']],
    ['I am :party: today', ['party']],
    [':PARTY: in capitals', ['party']],
    [':party::wave:', ['party', 'wave']],
    [':party: :wave: :party:', ['party', 'wave', 'party']],
    ['10:30:45', []],
    ['at 10:party:30', []],
    ['a:party:', []],
    [':party:a', []],
    [':party:wave:', []],
    ['::party::', ['party']],
    [':p:', []],
    [`:${'x'.repeat(33)}:`, []],
    [':nothere:', []],
    ['`:party:`', []],
    ['```\n:party:\n```', []],
    ['https://example.com/:party:', []],
    ['see https://example.com/a :party:', ['party']],
    ['**:party:**', ['party']],
    ['_:party:_', ['party']],
    ['||:party:||', ['party']],
    ['> :wave:', ['wave']],
    ['[ ] :party: task', ['party']],
];

test('the den\'s emoji show where the rule finds a name the den has, and nowhere else', () => {
    for (const [text, want] of RULE) {
        assert.deepEqual(shown(render(text)), want, JSON.stringify(text));
    }
    // A text with none of the den's names, or a den with none, shows text.
    assert.deepEqual(shown(parse(':party:', {})), []);
    assert.deepEqual(nodes(parse(':nothere:', { emoji: byName })).length, 2, 'the paragraph and its text');
});

test('nameAt reads a name lowercased, where its second colon ends', () => {
    assert.deepEqual(nameAt(':Party_Time:', 0), { name: 'party_time', end: 12 });
    assert.equal(nameAt('x:party:', 1), null);
    assert.deepEqual(nameAt('_:party:_', 1, 8, 1), { name: 'party', end: 8 }, "a span's start isn't the letter before it");
});

test("a reply's quote shows the den's emoji, and a spoiler hides them until it opens", () => {
    assert.deepEqual(shown(preview(':wave: hello', '', false, byName)), ['wave']);
    const spoiler = nodes(render('||:party:||')).find((n) => n.props?.children && nodes(n.props.children).some((c) => c.type === DenEmoji));
    assert.ok(spoiler, 'the spoiler holds the emoji');
    const quoted = nodes(preview('||:party:||', '', false, byName)).find((n) => typeof n.props?.class === 'string' && n.props.class.includes('[&_img]:invisible'));
    assert.ok(quoted, "the quote's spoiler hides its images");
});

test('a message of emoji alone, ten at most, shows them large', () => {
    for (const [text, n] of [
        ['👍', 1], ['👍 👍', 2], ['👍👍👍', 3], ['  😀\n😃 ', 2], [':party:', 1], [':party: 👍 :wave:', 3], [':party::wave:', 2],
        ['👍🏽', 1], ['🧑‍🤝‍🧑', 1], ['🇺🇸', 1], ['#️⃣', 1], ['👍'.repeat(MAX_BIG), MAX_BIG],
    ]) {
        assert.equal(emojiAlone(text, byName), n, JSON.stringify(text));
    }
    for (const text of ['', ' ', 'hi 👍', '👍 hi', ':nothere:', '1', '#', '👍'.repeat(MAX_BIG + 1), ':party: x', '`👍`', '**👍**']) {
        assert.equal(emojiAlone(text, byName), 0, JSON.stringify(text));
    }
    assert.equal(emojiAlone(':party:', null), 0, "a den's name with emoji off is text");
    const big = render(':party: 👍', true);
    assert.ok(big.props.class.includes('text-5xl'), 'Unicode\'s large');
    assert.ok(nodes(big).some((n) => n.type === DenEmoji && n.props.big), "the den's large");
});

test('emojiIn finds the emoji a message holds, outside code and links', () => {
    assert.deepEqual(emojiIn('hi 👍🏽 :party: :nothere: `😀` https://x.com/😀 ❤️', byName), [{ unicode: '👍🏽' }, { den: 'party' }, { unicode: '❤️' }]);
    assert.deepEqual(emojiIn('```\n😀\n```\n😃', byName), [{ unicode: '😃' }]);
});

test("the picker's search finds names and keywords, best first, and the den's by name", () => {
    assert.equal(searchUnicode('thumbs up')[0].emoji, '👍');
    assert.equal(searchUnicode('Thumbs')[0].emoji, '👍', 'any case');
    assert.ok(searchUnicode('+1').some((e) => e.emoji === '👍'), 'by keyword');
    assert.equal(searchUnicode('grinning face')[0].emoji, '😀', 'its name exactly before the rest');
    assert.ok(searchUnicode('heart').length > 10);
    assert.deepEqual(searchUnicode('zzzzqq'), []);
    assert.deepEqual(searchUnicode(''), []);
    assert.deepEqual(searchDen('par', DEN).map((e) => e.name), ['party']);
    assert.deepEqual(searchDen('_', DEN), []);
    const many = [emojiOf('3', 'party_parrot'), emojiOf('4', 'parrot'), PARTY];
    assert.deepEqual(searchDen('parrot', many).map((e) => e.name), ['parrot', 'party_parrot']);
    assert.deepEqual(searchDen('party parrot', many).map((e) => e.name), ['party_parrot']);
});

test('the skin tone, which the browser keeps, takes those emoji that take one', () => {
    const kept = storage();
    assert.equal(skinTone(), 0);
    setSkinTone(3);
    assert.equal(kept.get('DENS_SKIN_TONE'), '3');
    assert.equal(skinTone(), 3);
    const thumbs = emojiList().byEmoji.get('👍');
    const grin = emojiList().byEmoji.get('😀');
    assert.equal(inTone(thumbs, 3), '👍🏽');
    assert.equal(inTone(thumbs, 0), '👍');
    assert.equal(inTone(grin, 3), '😀');
    assert.equal(inTone(emojiList().byEmoji.get('🧑‍🤝‍🧑'), 5), '🧑🏿‍🤝‍🧑🏿', "two people, both of the tone");
    kept.set('DENS_SKIN_TONE', '9');
    assert.equal(skinTone(), 0, 'a tone the page never wrote');
    assert.equal(TONES.length, 6);
    delete globalThis.localStorage;
});

test('a use adds one, and every score halves in two weeks', () => {
    const kept = storage();
    const t = 1_000_000_000_000;
    noteUses([unicodeKey('👍')], t);
    noteUses([unicodeKey('👍')], t);
    assert.equal(scoreOf(JSON.parse(kept.get('DENS_EMOJI_USES'))['u:👍'], t), 2);
    assert.equal(scoreOf(JSON.parse(kept.get('DENS_EMOJI_USES'))['u:👍'], t + HALF_LIFE), 1);
    noteUses([unicodeKey('👍')], t + HALF_LIFE);
    assert.equal(scoreOf(JSON.parse(kept.get('DENS_EMOJI_USES'))['u:👍'], t + HALF_LIFE), 2, 'halved, then one more');
    // A favorite stays above one used more once, long ago.
    noteUses([unicodeKey('😀'), unicodeKey('😀'), unicodeKey('😀')], t - 8 * HALF_LIFE);
    assert.deepEqual(mostUsed('d1', [], 2, t + HALF_LIFE).map((x) => x.unicode.emoji), ['👍', '😀']);
    delete globalThis.localStorage;
});

test('each kind of use counts, once: a pick, a completion, and what a message sends, a keyboard\'s among it', () => {
    storage();
    const t = 1_000_000_000_000;
    // The picker and completion count as they put emoji in, and the
    // message they went in with counts the rest.
    noteUses([unicodeKey('😀')], t);
    noteUses([denKey('d1', PARTY.id)], t);
    const picked = new Map([[unicodeKey('😀'), 1], [denKey('d1', PARTY.id), 1]]);
    noteSent('😀 :party: 🎉 👍🏽 :wave:', 'd1', byName, picked, t);
    const scores = new Map(mostUsed('d1', DEN, 10, t).map((x) => [x.den ? `:${x.den.name}:` : x.unicode.emoji, x.score]));
    assert.deepEqual(Object.fromEntries(scores), { '😀': 1, ':party:': 1, '🎉': 1, '👍': 1, ':wave:': 1 }, 'a tone counts as its emoji');
    // An emoji the list doesn't have counts as nothing.
    noteSent('🫪', 'd1', byName, new Map(), t);
    assert.equal(mostUsed('d1', DEN, 10, t).length, 5);
    delete globalThis.localStorage;
});

test("a den's emoji counts in its den alone, and goes once it's deleted", () => {
    const kept = storage();
    const t = 1_000_000_000_000;
    noteUses([denKey('d1', PARTY.id), denKey('d2', PARTY.id)], t);
    assert.deepEqual(mostUsed('d1', DEN, 10, t).map((x) => x.den?.name), ['party']);
    assert.deepEqual(mostUsed('d3', DEN, 10, t), []);
    // Deleted in d1, its score there goes; d2's stays.
    assert.deepEqual(mostUsed('d1', [WAVE], 10, t), []);
    const left = Object.keys(JSON.parse(kept.get('DENS_EMOJI_USES')));
    assert.deepEqual(left, [denKey('d2', PARTY.id)]);
    delete globalThis.localStorage;
});

test('the browser keeps a few hundred scores, the lowest dropping first', () => {
    const kept = storage();
    const t = 1_000_000_000_000;
    const all = emojiList().emoji.slice(0, KEEP + 10).map((e) => unicodeKey(e.emoji));
    noteUses(all.slice(0, 1), t);
    noteUses(all.slice(0, 1), t);
    noteUses(all, t);
    const scores = JSON.parse(kept.get('DENS_EMOJI_USES'));
    assert.equal(Object.keys(scores).length, KEEP);
    assert.ok(all[0] in scores, 'the highest stays');
    delete globalThis.localStorage;
});

test('the quick reactions are the six most used, filled out with the defaults', () => {
    storage();
    const t = 1_000_000_000_000;
    const shownAs = (row) => row.map((x) => (x.den ? `:${x.den.name}:` : x.unicode.emoji));
    assert.deepEqual(shownAs(quickRow('d1', DEN, t)), QUICK);
    noteUses([unicodeKey('🔥'), denKey('d1', WAVE.id), denKey('d1', WAVE.id), unicodeKey('👍')], t);
    const row = shownAs(quickRow('d1', DEN, t));
    assert.equal(row[0], ':wave:', 'the most used first');
    assert.deepEqual(new Set(row.slice(1, 3)), new Set(['🔥', '👍']));
    assert.deepEqual(row.slice(3), ['❤️', '😂', '😮'], "then the defaults it lacks, in order");
    delete globalThis.localStorage;
});

test('a colon and two characters complete an emoji where one may start, and an @ a member in a channel', () => {
    const members = [{ id: '2', username: 'bob', display_name: 'Bobby Tables' }, { id: '3', username: 'carol', display_name: 'Carol B' }];
    const at = (text, m = members) => completionAt(text, text.length, m);
    assert.deepEqual(at(':th'), { kind: 'emoji', start: 0, query: 'th' });
    assert.deepEqual(at('hi :par'), { kind: 'emoji', start: 3, query: 'par' });
    assert.deepEqual(at(':party::wa'), { kind: 'emoji', start: 7, query: 'wa' });
    for (const text of [':t', 'a:th', '10:30', '`:th', '```\n:th', 'see https://x.com/:th', ':' + 'x'.repeat(33)]) {
        assert.equal(at(text), null, JSON.stringify(text));
    }
    assert.deepEqual(at('@'), { kind: 'member', start: 0, query: '' });
    assert.deepEqual(at('hi @bo'), { kind: 'member', start: 3, query: 'bo' });
    for (const text of ['a@bo', '@@bo', 'mail me at x@bo', '`@bo', 'https://x.com/@bo']) {
        assert.equal(at(text), null, JSON.stringify(text));
    }
    assert.equal(at('@bo', null), null, 'a DM completes no mention');
    // The caret in the middle completes what's before it.
    assert.deepEqual(completionAt(':thumbs here', 4, members), { kind: 'emoji', start: 0, query: 'thu' });
    assert.deepEqual(completeMember('b', members).map((m) => m.username), ['bob', 'carol'], 'by username, then a word of the display name');
    assert.deepEqual(completeMember('', members).map((m) => m.username), ['bob', 'carol']);
    assert.deepEqual(completeMember('tab', members).map((m) => m.username), ['bob']);
});

test("completing an emoji offers the den's that match, then Unicode's", () => {
    const offered = completeEmoji('wa', DEN);
    assert.deepEqual(offered[0], { den: WAVE });
    assert.ok(offered.slice(1).every((x) => x.unicode));
    assert.ok(offered.some((x) => x.unicode?.emoji === '👋'), 'waving hand');
    assert.ok(offered.length <= 8);
    assert.deepEqual(completeEmoji('thumbs_up', [])[0], { unicode: emojiList().byEmoji.get('👍') }, '_ for a space');
});

test("a name from a file's, and what staff type, as the den takes them", () => {
    assert.equal(nameFromFile('Party Parrot.gif'), 'party_parrot');
    assert.equal(nameFromFile('cat-jam (1).webp'), 'cat_jam_1');
    assert.equal(nameFromFile('x.png'), 'x_emoji');
    assert.equal(nameFromFile('ほげ.png'), 'emoji');
    assert.equal(nameFromFile(`${'a'.repeat(40)}.png`).length, 32);
    assert.equal(cleanName('  :Party: '), 'party');
    assert.ok(validName('party_2'));
    assert.ok(!validName('p') && !validName('par-ty') && !validName('x'.repeat(33)));
});
