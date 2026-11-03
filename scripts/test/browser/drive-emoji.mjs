// The M15.2 check of the den's emoji: alice, who owns the den, in Chromium,
// and bob in the browser named. Alice adds a still emoji and an animated
// one in Manage's Emoji section, opened from the den's name: a face larger
// than an emoji, which her page draws at 128 pixels, and a GIF, which her
// service makes 128 pixels. Bob writes the still one by name, picks the
// animated one and Unicode's 👍 in the picker, completes :par, :thu and
// @al, writes one in their DM, and sends a message of emoji alone, as
// alice does one of Unicode's; both pages show each, large where alone,
// and alice's mention highlighted. Bob's picker then leads with what he
// used, in the skin tone he picks; his page plays the animated emoji
// under On hover only while the pointer is over its message. Alice
// renames one, and the messages that wrote its old name show it as text;
// lowers the den's count to what it holds, which takes the Add form away
// and refuses another; and deletes the animated one.
//
//   run.sh emoji chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, SETTINGS, checks, launch, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, DM: dm, ALICE_MSG: aliceMsg } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = ALICE;
const B = BOB;
const q = (s) => JSON.stringify(s);

const MANAGE = `document.querySelector('[role=dialog][aria-label^="Manage "]')`;
const COMPOSER = `document.querySelector('textarea[placeholder^="Message "]')`;
const NAME = `document.querySelector(${q(`input[aria-label="The emoji's name"]`)})`;
const state = async (s) => JSON.parse(await s.b.eval(`(async () => JSON.stringify(await (await fetch('/api/dens/${den}/state')).json()))()`));
const escape = (s) => s.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
// setValue sets a field's value as typing would, for Preact to hear.
const setValue = (field, value) => `(() => { const el = ${field}; const set = Object.getOwnPropertyDescriptor(el.constructor.prototype, 'value').set;
    set.call(el, ${q(value)}); el.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`;
// newest is a channel's newest message, as the den has it.
const newest = (s, channel) => s.b.eval(`fetch('/api/dens/${den}/channels/${channel}/messages?limit=1').then((r) => r.json()).then((p) => p.messages.at(-1) || null)`);
// row is a message's row on a page.
const row = (id) => `document.querySelector('[data-id="${id}"]')`;

// shown says what a message's row shows of the den's emoji: each by name,
// whether its image loaded, its box's size, and what it draws, its file or
// its still preview with the file over it; and whether the row is emoji
// alone, at the size of Unicode's there.
const shown = (s, id) => s.b.eval(`(() => {
    const r = ${row(id)};
    if (!r) return null;
    const emoji = [...r.querySelectorAll('[data-emoji]')].map((e) => {
        const imgs = [...e.querySelectorAll('img')];
        const box = e.getBoundingClientRect();
        return { name: e.dataset.emoji, loaded: imgs.every((i) => i.complete && i.naturalWidth > 0), size: [Math.round(box.width), Math.round(box.height)],
            what: imgs.map((i) => (i.getAttribute('src').endsWith('/thumb') ? 'still' : 'file')).join('+') };
    });
    const md = r.querySelector('.markdown');
    return JSON.stringify({ emoji, big: md?.hasAttribute('data-big') || false, font: md ? getComputedStyle(md).fontSize : '', text: md?.textContent || '',
        mention: [...r.querySelectorAll('[data-mention]')].map((m) => m.dataset.mention) });
})()`).then((v) => (v ? JSON.parse(v) : null));

// sends writes text in the composer, as typing it, and sends it with
// Enter, and returns the message the den then has.
async function sends(s, channel, text) {
    const before = (await newest(s, channel))?.id;
    await s.b.eval(`${COMPOSER}.focus(), true`);
    await s.b.type(text);
    await s.b.key('Enter');
    return waitFor(async () => {
        const m = await newest(s, channel);
        return m && m.id !== before ? m : null;
    }, `${s.name}'s message sent`);
}

// showsOn waits for a page to show a message's emoji as want says.
async function showsOn(s, id, what, ok) {
    await waitFor(() => s.b.eval(`!!${row(id)}`), `the message on ${s.name}'s page`);
    return waitFor(async () => {
        const v = await shown(s, id);
        return v && ok(v) ? v : null;
    }, `${what} on ${s.name}'s page`, 10000).catch(async () => shown(s, id));
}

const allLoaded = (v, names) => v.emoji.length === names.length && v.emoji.every((e, i) => e.name === names[i] && e.loaded);

// addEmoji adds an emoji in Manage's Emoji section from a file, under a
// name, and returns the name the page suggested from the file's.
async function addEmoji(file, name) {
    await A.b.attach('form[data-add-emoji] input[type=file]', [file]);
    const suggested = await waitFor(() => A.b.eval(`${NAME}?.value || ''`), 'the suggested name');
    await A.b.eval(setValue(NAME, name));
    await A.b.eval(`document.querySelector('form[data-add-emoji] button[type=submit]').click(), true`);
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-manage-emoji="${name}"]')`), `:${name}: in Manage`, 30000);
    return suggested;
}

async function openManage(section) {
    if (!(await A.b.eval(`!!${MANAGE}`))) {
        await A.b.eval(`document.querySelector('button[data-den-manage]').click(), true`);
        await waitFor(() => A.b.eval(`!!${MANAGE}`), "alice's Manage");
    }
    const item = `[...${MANAGE}.querySelectorAll('nav button')].find((x) => x.textContent.trim() === ${q(section)})`;
    await waitFor(() => A.b.eval(`!!${item}`), `Manage's ${section}`);
    await A.b.eval(`${item}.click(), true`);
}

async function closeManage() {
    await A.b.eval(`${MANAGE}.querySelector('button[aria-label=Close]').click(), true`);
    await waitFor(async () => !(await A.b.eval(`!!${MANAGE}`)), "alice's Manage closed");
}

// picker opens a member's emoji picker from the composer's smiley, and
// waits for Unicode's to show in it.
async function openPicker(s) {
    if (!(await s.b.eval(`!!document.querySelector('[data-emoji-picker]')`))) {
        await s.b.eval(`document.querySelector('button[data-emoji-button]').click(), true`);
    }
    await waitFor(() => s.b.eval(`!!document.querySelector('[data-emoji-picker] [data-section=g8] [data-emoji-tile]')`), `${s.name}'s picker`);
}

// pick searches the picker and picks a tile.
async function pick(s, query, tile) {
    await openPicker(s);
    await s.b.eval(setValue(`document.querySelector('[data-emoji-picker] input[type=search]')`, query));
    const t = `document.querySelector('[data-emoji-picker] [data-emoji-tile="${tile}"]')`;
    await waitFor(() => s.b.eval(`!!${t}`), `${tile} found in the picker`);
    await s.b.eval(`${t}.click(), true`);
    await waitFor(async () => !(await s.b.eval(`!!document.querySelector('[data-emoji-picker]')`)), 'the picker closed');
}

// completes types text in the composer and waits for the completions to
// offer an item, which it returns with what's marked.
async function completes(s, text, item) {
    await s.b.eval(`${COMPOSER}.focus(), true`);
    await s.b.type(text);
    return waitFor(() => s.b.eval(`(() => {
        const list = document.querySelector('[data-completions]');
        if (!list || !list.querySelector('[data-completion=${q(item)}]')) return null;
        return JSON.stringify({ items: [...list.querySelectorAll('[data-completion]')].map((x) => x.dataset.completion),
            marked: list.querySelector('[aria-selected=true]')?.dataset.completion || '' });
    })()`), `${q(item)} completing ${q(text)}`).then(JSON.parse);
}

const composer = (s) => s.b.eval(`${COMPOSER}.value`);

async function setAnimations(s, value) {
    await openSettings(s.b, 'Appearance');
    await s.b.eval(`document.querySelector('input[name=animations][value=${value}]').click(), true`);
    await escape(s);
    await waitFor(async () => !(await s.b.eval(`!!${SETTINGS}`)), 'the settings closed');
}

try {
    A.b = await launch('chromium', 9581, { hover: true });
    B.b = await launch(which, which === 'chromium' ? 9582 : 9583, { hover: true });
    for (const s of [A, B]) {
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}/${general}`);
        await waitFor(() => s.b.eval(`!!${row(aliceMsg)}`), `${s.name}'s #general`);
    }

    step('alice adds a still emoji and an animated one in Manage');
    await openManage('Emoji');
    const empty = await waitFor(() => A.b.eval(`document.querySelector('[data-emoji-count]')?.textContent || ''`), 'the count');
    report(empty === '0 of 50 emoji.', `Manage's Emoji counts none of the 50 a new den keeps: ${q(empty)}`);
    const suggested = await addEmoji(`${HOME}/Party Face.png`, 'party');
    report(suggested === 'party_face', `the page suggests a name from the file's: ${q(suggested)}`);
    await addEmoji(`${HOME}/dance.gif`, 'dance');
    await A.b.shot(`${HOME}/shot-emoji-manage.png`);
    const kept = (await state(A)).emoji;
    const party = kept.find((e) => e.name === 'party');
    const dance = kept.find((e) => e.name === 'dance');
    report(party && !party.image.animated && party.image.width === 128 && party.image.height === 80,
        `the still one is drawn at an emoji's size, 128 × 80 of 256 × 160: ${q(party)}`);
    report(dance && dance.image.animated && dance.image.width === 128 && dance.image.height === 128,
        `the animated one is a GIF the service made at 128 × 128 of 160 × 160: ${q(dance)}`);
    await closeManage();
    const bobs = await waitFor(async () => {
        const v = await state(B);
        return v.emoji?.length === 2 ? v.emoji : null;
    }, "bob's page to have the den's emoji");
    report(bobs.map((e) => e.name).join(',') === 'dance,party', `bob's page has the den's emoji, by name: ${bobs.map((e) => e.name)}`);

    step('bob writes one by name');
    const byName = await sends(B, general, ':party: by name');
    report(byName.text === ':party: by name', `the den keeps the text as written, which an older page shows as it is: ${q(byName.text)}`);
    for (const s of [A, B]) {
        const v = await showsOn(s, byName.id, ':party:', (x) => allLoaded(x, ['party']));
        report(allLoaded(v, ['party']) && !v.big && v.emoji[0].size[1] <= 24, `${s.name}'s page shows :party: in the line, loaded: ${q(v)}`);
    }

    step('bob picks the animated one and 👍 in the picker');
    await openPicker(B);
    await B.b.shot(`${HOME}/shot-emoji-picker-${which}.png`);
    const sections = await B.b.eval(`[...document.querySelectorAll('[data-emoji-picker] [data-section]')].map((s) => s.getAttribute('aria-label'))`);
    report(sections[0] === 'Most used' && sections[1] === "This den's" && sections[2] === 'Smileys & Emotion' && sections.at(-1) === 'Flags',
        `the picker shows the most used, the den's, then Unicode's by group: ${q(sections)}`);
    await pick(B, 'dance', ':dance:');
    await pick(B, 'thumbs up', '👍');
    const picked = await composer(B);
    report(picked === ':dance:👍', `the picks go in at the caret, the den's by name: ${q(picked)}`);
    await B.b.eval(`${COMPOSER}.focus(), true`);
    await B.b.key('Enter');
    const fromPicker = await waitFor(async () => {
        const m = await newest(B, general);
        return m?.text === ':dance:👍' ? m : null;
    }, 'the picks sent');
    for (const s of [A, B]) {
        const v = await showsOn(s, fromPicker.id, ':dance: and 👍', (x) => allLoaded(x, ['dance']) && x.text.includes('👍'));
        report(allLoaded(v, ['dance']) && v.text.includes('👍') && v.big, `${s.name}'s page shows :dance: and 👍, large alone: ${q(v)}`);
    }

    step('bob completes :par, :thu and @al');
    let offered = await completes(B, 'hi :par', ':party:');
    report(offered.marked === ':party:', `:par offers the den's :party: first, marked: ${q(offered)}`);
    await B.b.shot(`${HOME}/shot-emoji-completion-${which}.png`);
    await B.b.key('Enter');
    offered = await completes(B, ':thu', '👍');
    report(offered.items[0] === '👍', `:thu offers 👍 first: ${q(offered)}`);
    await B.b.eval(`document.querySelector('[data-completion="👍"]').click(), true`);
    offered = await completes(B, '@al', '@alice');
    report(offered.marked === '@alice', `@al offers alice, marked: ${q(offered)}`);
    await B.b.key('Tab');
    const completed = await composer(B);
    report(completed === 'hi :party: 👍 @alice ', `Enter, a click and Tab put each in, with a space: ${q(completed)}`);
    await B.b.key('Enter');
    const viaCompletion = await waitFor(async () => {
        const m = await newest(B, general);
        return m?.text === completed ? m : null;
    }, 'the completed message sent');
    const onAlice = await showsOn(A, viaCompletion.id, 'the completed message', (x) => allLoaded(x, ['party']) && x.mention.includes('alice'));
    report(allLoaded(onAlice, ['party']) && onAlice.mention.includes('alice') && !onAlice.big,
        `alice's page shows the completed message, her mention marked: ${q(onAlice)}`);

    step('bob writes one in their DM');
    await B.b.go(`http://localhost:${B.port}/den/${den}/${dm}`);
    await waitFor(() => B.b.eval(`!!${COMPOSER}`), "bob's DM");
    const inDM = await sends(B, dm, ':party: in a DM');
    await A.b.go(`http://localhost:${A.port}/den/${den}/${dm}`);
    for (const s of [A, B]) {
        const v = await showsOn(s, inDM.id, ':party: in the DM', (x) => allLoaded(x, ['party']));
        report(allLoaded(v, ['party']), `${s.name}'s page shows :party: in the DM, which the den can't read: ${q(v)}`);
    }

    step('messages of emoji alone show them large');
    for (const s of [A, B]) {
        await s.b.go(`http://localhost:${s.port}/den/${den}/${general}`);
        await waitFor(() => s.b.eval(`!!${row(aliceMsg)}`), `${s.name}'s #general again`);
    }
    const alone = await sends(B, general, ':party: :dance:');
    const unicode = await sends(A, general, '👍 🎉');
    for (const s of [A, B]) {
        const v = await showsOn(s, alone.id, 'the emoji alone, large', (x) => allLoaded(x, ['party', 'dance']) && x.big);
        report(v.big && allLoaded(v, ['party', 'dance']) && v.emoji.every((e) => e.size[0] === 48 && e.size[1] === 48),
            `${s.name}'s page shows the den's emoji alone at 48 pixels: ${q(v)}`);
        const u = await showsOn(s, unicode.id, "Unicode's alone, large", (x) => x.big);
        report(u.big && u.font === '48px', `${s.name}'s page shows Unicode's alone at 48 pixels: ${q(u)}`);
    }
    await B.b.shot(`${HOME}/shot-emoji-messages-${which}.png`);

    step("bob's picker leads with what he used, in his skin tone");
    await openPicker(B);
    const most = await B.b.eval(`[...document.querySelectorAll('[data-emoji-picker] [data-section=most] [data-emoji-tile]')].map((t) => t.dataset.emojiTile)`);
    report(most[0] === ':party:' && most.includes('👍') && most.includes(':dance:'),
        `the first block, Most used, leads with :party:, used most, and holds 👍 and :dance:: ${q(most)}`);
    await B.b.eval(`document.querySelector('[data-emoji-picker] [data-skin-tone]').click(), true`);
    await B.b.eval(`document.querySelector('[data-emoji-picker] [data-tone="3"]').click(), true`);
    const toned = await waitFor(() => B.b.eval(`document.querySelector('[data-emoji-picker] [data-section=most] [data-emoji-tile="👍"]')?.textContent === '👍🏽'`),
        'the tone on 👍').catch(() => false);
    const kept3 = await B.b.eval(`localStorage.getItem('DENS_SKIN_TONE')`);
    report(toned && kept3 === '3', `a skin tone shows on 👍, and the browser keeps it: ${kept3}`);
    await B.b.eval(`document.querySelector('[data-emoji-picker] [data-section=most] [data-emoji-tile="👍"]').click(), true`);
    report((await composer(B)) === '👍🏽', `the pick goes in toned: ${q(await composer(B))}`);
    await B.b.eval(setValue(COMPOSER, ''));
    await openPicker(B);
    await B.b.eval(`document.querySelector('[data-emoji-picker] [data-skin-tone]').click(), true`);
    await B.b.eval(`document.querySelector('[data-emoji-picker] [data-tone="0"]').click(), true`);
    await escape(B);

    step("bob's page plays the animated emoji under On hover only over its message");
    await setAnimations(B, 'hover');
    const dancing = (x) => x.emoji.find((e) => e.name === 'dance');
    await B.b.move(5, 5);
    const still = await showsOn(B, fromPicker.id, ':dance: still', (x) => dancing(x)?.what === 'still' && dancing(x).loaded);
    const at = JSON.parse(await B.b.eval(`(() => { const r = ${row(fromPicker.id)}; r.scrollIntoView({ block: 'center' });
        const b = r.getBoundingClientRect(); return JSON.stringify([b.left + b.width / 2, b.top + b.height / 2]); })()`));
    await B.b.move(at[0], at[1]);
    const playing = await showsOn(B, fromPicker.id, ':dance: playing', (x) => dancing(x)?.what === 'still+file' && dancing(x).loaded);
    await B.b.move(5, 5);
    const again = await showsOn(B, fromPicker.id, ':dance: still again', (x) => dancing(x)?.what === 'still');
    report(dancing(still)?.what === 'still' && dancing(playing)?.what === 'still+file' && dancing(again)?.what === 'still',
        `On hover: still, playing under the pointer, still again: ${q([dancing(still), dancing(playing), dancing(again)])}`);
    await setAnimations(B, 'always');
    const always = await showsOn(B, fromPicker.id, ':dance: always', (x) => dancing(x)?.what === 'file');
    report(dancing(always)?.what === 'file', `Always plays it without the pointer: ${q(dancing(always))}`);

    step('alice renames :party:, and the messages that wrote it show its old name as text');
    await openManage('Emoji');
    const partyRow = `document.querySelector('[data-manage-emoji="party"]')`;
    await waitFor(() => A.b.eval(`!!${partyRow}`), ':party: in Manage');
    await A.b.eval(`[...${partyRow}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Rename').click(), true`);
    await A.b.eval(setValue(`${partyRow}.querySelector('input')`, 'partying'));
    await A.b.eval(`[...${partyRow}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Save').click(), true`);
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-manage-emoji="partying"]')`), ':partying: in Manage');
    const renamed = await showsOn(B, byName.id, ':party: as text', (x) => x.emoji.length === 0);
    report(renamed.emoji.length === 0 && renamed.text.includes(':party:'), `bob's page shows :party: as text once it's renamed: ${q(renamed)}`);

    step("alice lowers the den's count to what it holds");
    await openManage('Limits');
    const field = `document.querySelector('[data-limits-emoji] input[type=number]')`;
    await waitFor(() => A.b.eval(`!!${field}`), "Limits' emoji");
    await A.b.eval(setValue(field, '2'));
    const save = `[...${MANAGE}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Save changes')`;
    await waitFor(() => A.b.eval(`!!${save}`), 'the bar');
    await A.b.eval(`${save}.click(), true`);
    await waitFor(() => A.b.eval(`${MANAGE}.textContent.includes('Saved.')`), 'saved');
    await openManage('Emoji');
    const full = await waitFor(async () => {
        const v = JSON.parse(await A.b.eval(`JSON.stringify({ count: document.querySelector('[data-emoji-count]')?.textContent || '',
            form: !!document.querySelector('form[data-add-emoji]') })`));
        return v.count === '2 of 2 emoji.' ? v : null;
    }, 'the count of 2').catch(() => null);
    report(full && !full.form, `Manage's Emoji holds 2 of 2, with no way to add a third: ${q(full)}`);
    const refused = await A.b.eval(`(async () => {
        const img = await (await fetch('/api/dens/${den}/files/${party.image.id}')).blob();
        const up = await (await fetch('/api/dens/${den}/uploads', { method: 'POST', headers: { 'Content-Type': 'application/octet-stream', 'Dens-Filename': 'third.png' }, body: img })).json();
        const res = await fetch('/api/dens/${den}/emoji', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'third', upload: up.id }) });
        return JSON.stringify({ status: res.status, error: (await res.json()).error });
    })()`).then(JSON.parse);
    report(refused.status === 502 && refused.error === 'The den rejected one of the details you entered.', `the den refuses a third: ${q(refused)}`);

    step('alice deletes :dance:');
    const danceRow = `document.querySelector('[data-manage-emoji="dance"]')`;
    await waitFor(() => A.b.eval(`!!${danceRow}`), ':dance: in Manage');
    await A.b.eval(`[...${danceRow}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Delete').click(), true`);
    await A.b.eval(`[...${danceRow}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Delete').click(), true`);
    await waitFor(async () => !(await A.b.eval(`!!${danceRow}`)), ':dance: gone from Manage');
    const gone = await showsOn(B, fromPicker.id, ':dance: as text', (x) => x.emoji.length === 0);
    report(gone.emoji.length === 0 && gone.text.includes(':dance:'), `bob's page shows :dance: as text once it's deleted: ${q(gone)}`);
    const after = await A.b.eval(`document.querySelector('[data-emoji-count]')?.textContent || ''`);
    report(after === '1 of 2 emoji.' && (await A.b.eval(`!!document.querySelector('form[data-add-emoji]')`)), `and Manage can add one again: ${q(after)}`);
    await closeManage();
} catch (e) {
    c.fail(e);
}
await sleep(100);
for (const s of [A, B]) await s.b?.close().catch(() => {});
c.finish();
