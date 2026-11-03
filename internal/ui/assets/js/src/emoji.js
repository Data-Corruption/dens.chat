// Emoji (M15.2): Unicode's, from the list scripts/emoji.sh writes, which
// the page fetches once a picker or a completion first needs it, and the
// den's own, which staff add and members write by name, as :name:. This is
// what the picker, completion and the markdown subset share: the rule that
// finds a name in text, a message of emoji alone, search, the skin tone,
// the member's most used, and where a completion starts. Emoji draw in the
// system's font, as the rest of the text does.

// NAME_CHAR is what a den's emoji's name, and a username, are made of.
const NAME_CHAR = /[A-Za-z0-9_]/;

// fenced says whether at in text is inside a code block, as the markdown
// subset reads them: a line starting with three backticks opens one and
// the next closes it.
function fenced(text, at) {
    let open = false;
    for (const line of text.slice(0, at).split('\n')) {
        if (line.trim().startsWith('```')) open = !open;
    }
    return open;
}

// nameAt finds a den's emoji written by name at i in line, before end: a
// colon, 2 to 32 of a-z, 0-9 and _ in any case, and a colon, the first not
// after a letter, digit or _ and the second not before one, so a time such
// as 10:30:45 holds none. The markdown subset reads a span inside its
// delimiters from start to end, so _:name:_ is an emoji in italics. It
// returns the name, lowercased, and where the second colon ends, or null;
// whether the den has the name is the caller's to ask.
export function nameAt(line, i, end = line.length, start = 0) {
    if (line[i] !== ':' || (i > start && NAME_CHAR.test(line[i - 1]))) return null;
    let j = i + 1;
    while (j < end && j - i <= 33 && NAME_CHAR.test(line[j])) j++;
    const length = j - i - 1;
    if (j >= end || line[j] !== ':' || length < 2 || length > 32) return null;
    if (j + 1 < end && NAME_CHAR.test(line[j + 1])) return null;
    return { name: line.slice(i + 1, j).toLowerCase(), end: j + 1 };
}

// A Unicode emoji, as the RGI set names them, in full: the browser knows
// them, so finding one needs no list.
const UNICODE = new RegExp('\\p{RGI_Emoji}', 'vy');
const SPACE = /\s/;

// MAX_BIG is how many emoji a message of nothing else holds at most and
// still shows them large.
export const MAX_BIG = 10;

// emojiAlone says how many emoji a text is, when it's nothing else: 1 to
// MAX_BIG of Unicode's, and of the den's that names holds, with nothing
// but spaces between them. Otherwise it's 0, and the text shows them at
// its size.
export function emojiAlone(text, names) {
    let count = 0;
    for (let i = 0; i < text.length;) {
        if (SPACE.test(text[i])) {
            i++;
            continue;
        }
        const named = nameAt(text, i);
        if (named && names?.has(named.name)) {
            i = named.end;
        } else {
            UNICODE.lastIndex = i;
            const m = UNICODE.exec(text);
            if (!m) return 0;
            i += m[0].length;
        }
        if (++count > MAX_BIG) return 0;
    }
    return count;
}

// emojiIn lists the emoji a text holds, in order: Unicode's, each as it's
// written, and the den's that names holds, by name. Code doesn't hold
// any, as the markdown subset shows it, nor do links.
export function emojiIn(text, names) {
    const out = [];
    let block = false;
    for (const line of text.split('\n')) {
        if (line.trim().startsWith('```')) {
            block = !block;
            continue;
        }
        if (block) continue;
        let code = false;
        for (let i = 0; i < line.length;) {
            if (line[i] === '`') {
                code = !code;
                i++;
                continue;
            }
            if (!code && (line.startsWith('http://', i) || line.startsWith('https://', i)) && (i === 0 || !NAME_CHAR.test(line[i - 1]))) {
                while (i < line.length && !/[\s<>"'`]/.test(line[i])) i++;
                continue;
            }
            if (!code) {
                const named = nameAt(line, i);
                if (named && names?.has(named.name)) {
                    out.push({ den: named.name });
                    i = named.end;
                    continue;
                }
                UNICODE.lastIndex = i;
                const m = UNICODE.exec(line);
                if (m) {
                    out.push({ unicode: m[0] });
                    i += m[0].length;
                    continue;
                }
            }
            i += line.codePointAt(i) > 0xffff ? 2 : 1;
        }
    }
    return out;
}

// The list of Unicode's emoji -------------------------------------------------

let list = null;
let loading = null;

// prepare makes the list the page fetched ready to use: each emoji with
// its name and keywords as words to search, its group, and its forms in
// the skin tones, and every form found back to its emoji.
export function prepare(data) {
    const emoji = data.emoji.map(([e, name, group, keywords, tones], i) => ({
        emoji: e, name, group, index: i, tones: tones || null,
        keywords: keywords ? keywords.split('|') : [],
        words: words(`${name} ${keywords.replaceAll('|', ' ')}`),
        nameWords: words(name),
    }));
    const byEmoji = new Map();
    for (const e of emoji) {
        byEmoji.set(e.emoji, e);
        for (const t of e.tones || []) byEmoji.set(t, e);
    }
    return { version: data.version, groups: data.groups, emoji, byEmoji };
}

// words splits a name or keywords into the words a search starts, lowercased.
function words(s) {
    return s.toLowerCase().split(/[\s:,_“”"().!-]+/).filter(Boolean);
}

// setEmojiList takes the list as the page would fetch it, for tests.
export function setEmojiList(data) {
    list = data ? prepare(data) : null;
    loading = null;
}

/** emojiList is the list, once fetched, or null */
export function emojiList() {
    return list;
}

/** loadEmoji fetches the list, once, from where the page's shell says */
export function loadEmoji() {
    if (list) return Promise.resolve(list);
    loading ||= (async () => {
        const url = document.getElementById('app')?.dataset.emoji;
        if (!url) throw new Error('The page has no list of emoji.');
        const res = await fetch(url, { credentials: 'same-origin' });
        if (!res.ok) throw new Error(`The list of emoji didn't load (HTTP ${res.status}).`);
        list = prepare(await res.json());
        return list;
    })().catch((e) => {
        loading = null;
        throw e;
    });
    return loading;
}

// Search ----------------------------------------------------------------------

// queryWords splits what a member searches for into words, as names split.
function queryWords(q) {
    return words(q.replaceAll('_', ' '));
}

// rankOf says how well words match an emoji's, lower being better: its
// name exactly, its name's start, the start of a word of its name, the
// start of a keyword, or -1 for no match, which every word must start.
function rankOf(q, name, nameWords, all) {
    if (!q.length || !q.every((w) => all.some((x) => x.startsWith(w)))) return -1;
    const joined = q.join(' ');
    if (name === joined) return 0;
    if (name.startsWith(joined)) return 1;
    if (q.every((w) => nameWords.some((x) => x.startsWith(w)))) return 2;
    return 3;
}

// searchDen lists the den's emoji whose names match a search, best first,
// and by name.
export function searchDen(query, denEmoji) {
    const q = queryWords(query);
    return denEmoji
        .map((e) => {
            const parts = words(e.name);
            return { e, rank: rankOf(q, e.name.replaceAll('_', ' '), parts, parts) };
        })
        .filter((x) => x.rank >= 0)
        .sort((a, b) => a.rank - b.rank || a.e.name.localeCompare(b.e.name))
        .map((x) => x.e);
}

// searchUnicode lists Unicode's emoji whose names and keywords match a
// search, best first, and in the list's order.
export function searchUnicode(query, l = list) {
    if (!l) return [];
    const q = queryWords(query);
    return l.emoji
        .map((e) => ({ e, rank: rankOf(q, e.name.toLowerCase(), e.nameWords, e.words) }))
        .filter((x) => x.rank >= 0)
        .sort((a, b) => a.rank - b.rank || a.e.index - b.e.index)
        .map((x) => x.e);
}

// Skin tone -------------------------------------------------------------------

const TONE = 'DENS_SKIN_TONE';
// TONES are the choices, none, then light to dark, each shown by a hand.
export const TONES = ['✋', '✋🏻', '✋🏼', '✋🏽', '✋🏾', '✋🏿'];
export const TONE_NAMES = ['No skin tone', 'Light skin tone', 'Medium-light skin tone', 'Medium skin tone', 'Medium-dark skin tone', 'Dark skin tone'];

/** skinTone is the skin tone the member chose, 0 for none, which the
    browser keeps */
export function skinTone() {
    try {
        const n = Number(localStorage.getItem(TONE));
        return Number.isInteger(n) && n >= 0 && n < TONES.length ? n : 0;
    } catch {
        // Storage can be unavailable (private windows).
        return 0;
    }
}

export function setSkinTone(n) {
    try {
        localStorage.setItem(TONE, String(n));
    } catch {
        // As above.
    }
}

// inTone is an emoji in a skin tone, where it takes one.
export function inTone(e, tone) {
    return tone > 0 && e.tones ? e.tones[tone - 1] : e.emoji;
}

// Most used -------------------------------------------------------------------

const USES = 'DENS_EMOJI_USES';
// HALF_LIFE is how long a score takes to halve, so a favorite stays at the
// top while one used once fades.
export const HALF_LIFE = 14 * 24 * 60 * 60 * 1000;
// KEEP is how many scores the browser keeps, the lowest dropping first.
export const KEEP = 300;
// QUICK are the quick reactions while the member has used fewer (M15.3).
export const QUICK = ['👍', '❤️', '😂', '😮', '😢', '🎉'];

// A use is keyed by the emoji: Unicode's as itself without a skin tone,
// which the member's choice puts back, and a den's by the den and its ID,
// so it counts in its den alone and keeps its score through a rename.
export const unicodeKey = (e) => `u:${e}`;
export const denKey = (denID, id) => `d:${denID}:${id}`;

// readUses reads the scores, each a score as of a time.
function readUses() {
    try {
        const kept = JSON.parse(localStorage.getItem(USES) || '{}');
        return kept && typeof kept === 'object' ? kept : {};
    } catch {
        return {};
    }
}

function writeUses(uses) {
    try {
        localStorage.setItem(USES, JSON.stringify(uses));
    } catch {
        // Storage can be unavailable; the scores last as long as the page.
    }
}

// scoreOf is a score as of now: halved for each HALF_LIFE since it was set.
export function scoreOf(kept, now) {
    if (!Array.isArray(kept) || !Number.isFinite(kept[0]) || !Number.isFinite(kept[1])) return 0;
    return kept[0] * 2 ** (-Math.max(0, now - kept[1]) / HALF_LIFE);
}

/** noteUses counts a use of each emoji, by key: one each */
export function noteUses(keys, now = Date.now()) {
    if (!keys.length) return;
    const uses = readUses();
    for (const k of keys) uses[k] = [scoreOf(uses[k], now) + 1, now];
    const all = Object.entries(uses);
    if (all.length > KEEP) {
        all.sort((a, b) => scoreOf(b[1], now) - scoreOf(a[1], now));
        writeUses(Object.fromEntries(all.slice(0, KEEP)));
        return;
    }
    writeUses(uses);
}

// keyOf is the key an emoji in text counts under: a den's by its ID, and
// Unicode's without its skin tone, where the list has it.
export function keyOf(found, denID, byName, l = list) {
    if (found.den) {
        const e = byName?.get(found.den);
        return e ? denKey(denID, e.id) : null;
    }
    const e = l?.byEmoji.get(found.unicode);
    return e ? unicodeKey(e.emoji) : null;
}

/** noteSent counts the emoji in a message the member sent, but those
    already counted as they went in by the picker or completion, each of
    which takes one from picked */
export function noteSent(text, denID, byName, picked, now = Date.now(), l = list) {
    const left = new Map(picked);
    const keys = [];
    for (const found of emojiIn(text, byName)) {
        const k = keyOf(found, denID, byName, l);
        if (!k) continue;
        if (left.get(k) > 0) {
            left.set(k, left.get(k) - 1);
            continue;
        }
        keys.push(k);
    }
    noteUses(keys, now);
}

// mostUsed lists the emoji the member uses most in a den, at most limit,
// as scores say: Unicode's, and the den's in that den alone. A score of a
// den's emoji it no longer has goes.
export function mostUsed(denID, denEmoji, limit, now = Date.now(), l = list) {
    const uses = readUses();
    const ids = new Map(denEmoji.map((e) => [e.id, e]));
    const prefix = `d:${denID}:`;
    let pruned = false;
    const found = [];
    for (const [k, kept] of Object.entries(uses)) {
        if (k.startsWith(prefix)) {
            const e = ids.get(k.slice(prefix.length));
            if (!e) {
                delete uses[k];
                pruned = true;
                continue;
            }
            found.push({ score: scoreOf(kept, now), den: e });
        } else if (k.startsWith('u:')) {
            const e = l?.byEmoji.get(k.slice(2));
            if (e) found.push({ score: scoreOf(kept, now), unicode: e });
        }
    }
    if (pruned) writeUses(uses);
    return found.sort((a, b) => b.score - a.score).slice(0, limit);
}

/** quickRow is the quick reactions (M15.3): the member's six most used,
    filled out with QUICK while they've used fewer. denEmoji are all the
    den's emoji, whose scores stay while emoji are off, when on is false
    and the row leaves them out. */
export function quickRow(denID, denEmoji, now = Date.now(), l = list, on = true) {
    const row = mostUsed(denID, denEmoji, KEEP, now, l).filter((x) => on || !x.den).slice(0, QUICK.length);
    for (const q of QUICK) {
        if (row.length >= QUICK.length) break;
        const e = l?.byEmoji.get(q);
        if (e && !row.some((x) => x.unicode === e)) row.push({ score: 0, unicode: e });
    }
    return row;
}

// Completion ------------------------------------------------------------------

/** completionAt says what the member is completing with the caret at
    caret in text: an emoji, after a colon and two characters, where one
    may start, or with members, a member, after an @ where a mention may
    start. Nothing completes in code or in a link. It returns its kind,
    where its colon or @ is, and what follows it, or null. */
export function completionAt(text, caret, members) {
    let i = caret;
    while (i > 0 && caret - i <= 32 && NAME_CHAR.test(text[i - 1])) i--;
    const at = i - 1;
    if (at < 0 || caret - i > 32) return null;
    const mark = text[at];
    const before = at > 0 ? text[at - 1] : '';
    const query = text.slice(i, caret);
    let kind = null;
    if (mark === ':' && !NAME_CHAR.test(before) && query.length >= 2) kind = 'emoji';
    else if (mark === '@' && members && !NAME_CHAR.test(before) && before !== '@') kind = 'member';
    if (!kind || fenced(text, caret)) return null;
    const lineStart = text.lastIndexOf('\n', at) + 1;
    const line = text.slice(lineStart, at);
    if ((line.match(/`/g) || []).length % 2 === 1) return null;
    // The word it's in, back to the last space, isn't a link.
    const word = line.slice(Math.max(line.lastIndexOf(' '), line.lastIndexOf('\t')) + 1);
    if (/^https?:\/\//.test(word)) return null;
    return { kind, start: at, query };
}

// MAX_COMPLETIONS is how many a completion offers at once.
export const MAX_COMPLETIONS = 8;

/** completeEmoji lists what completing an emoji offers: the den's that
    match, then Unicode's */
export function completeEmoji(query, denEmoji, l = list) {
    const den = searchDen(query, denEmoji).map((e) => ({ den: e }));
    const unicode = searchUnicode(query, l).map((e) => ({ unicode: e }));
    return [...den, ...unicode].slice(0, MAX_COMPLETIONS);
}

/** completeMember lists the members a mention offers, by username, then
    by a word of their display name */
export function completeMember(query, members) {
    const q = query.toLowerCase();
    const rank = (m) => {
        if (m.username.startsWith(q)) return 0;
        if (m.display_name.toLowerCase().split(/\s+/).some((w) => w.startsWith(q))) return 1;
        return -1;
    };
    return members
        .map((m) => ({ m, r: rank(m) }))
        .filter((x) => x.r >= 0)
        .sort((a, b) => a.r - b.r || a.m.username.localeCompare(b.m.username))
        .slice(0, MAX_COMPLETIONS)
        .map((x) => x.m);
}

// Names from files ------------------------------------------------------------

/** nameFromFile suggests an emoji's name from its file's: lowercased, the
    rest of what a name can't hold as _, and at most 32 */
export function nameFromFile(file) {
    const base = (file || '').replace(/\.[^.]*$/, '').toLowerCase();
    let name = base.replace(/[^a-z0-9_]+/g, '_').replace(/_+/g, '_').replace(/^_|_$/g, '').slice(0, 32).replace(/_$/, '');
    if (name.length < 2) name = name ? `${name}_emoji` : 'emoji';
    return name;
}

/** cleanName makes what staff typed a name as the den takes it: trimmed,
    lowercased, without colons around it */
export function cleanName(s) {
    return s.trim().replace(/^:+|:+$/g, '').toLowerCase();
}

/** validName says whether a name is one the den takes */
export function validName(s) {
    return /^[a-z0-9_]{2,32}$/.test(s);
}
