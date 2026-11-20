// The steam check, of Steam's cards, as bob in the browser named, against a
// stand-in for Steam: the page's own requests for a card and its pictures
// answered in the browser, with what bob's Dens would give, since the
// service's side is checked against Steam's recorded answers in Go. Alice
// posts links to games; bob sees links alone and asks nothing until he
// turns the cards on in Chat. Then each game linked gets a card, once and
// three to a message at most, none for a link in code or a spoiler: a sale
// with its cut, its prices and its end, reviews and who's playing, its
// systems and Steam Deck; a free game; one coming; one Steam doesn't
// have; and one for adults only, its pictures covered and unasked until
// bob shows them. A card keeps its size from before it loads, its arrows
// step through the screenshots, each asked for as it's shown, and its name
// links to the store.
//
//   run.sh steam chromium|firefox
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, SETTINGS, checks, launch, lib, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general } = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);
const q = JSON.stringify;
const tag = Date.now().toString(36);
const photo = fs.readFileSync(`${HOME}/photo.jpg`);
const day = 24 * 3600;
const now = Math.floor(Date.now() / 1000);

// What bob's Dens says of each game, as the service writes its cards.
const CARDS = {
    1771300: {
        name: 'Kingdom Come: Deliverance II', kind: 'game', developer: 'Warhorse Studios', description: 'An action RPG set in medieval Bohemia.',
        tags: ['Story Rich', 'Open World', 'RPG'], reviews: { score: 8, percent: 93, count: 148458 }, players: 7694,
        platforms: { windows: true, mac: false, linux: true }, deck: 'verified', released: now - 300 * day,
        price: { final: '$19.79', original: '$59.99', discount: 67, ends: now + 6 * day }, shots: 3,
    },
    730: {
        name: 'Counter-Strike 2', kind: 'game', developer: 'Valve', description: 'The next chapter in the CS story.', tags: ['FPS', 'Shooter'],
        reviews: { score: 6, percent: 86, count: 8400000 }, players: 1204331, platforms: { windows: true, mac: false, linux: true }, deck: 'playable',
        released: now - 4000 * day, free: true,
    },
    5503390: { name: "Blamcon's Light Gun Arcade", platforms: { windows: true, mac: false, linux: false }, coming_soon: true, release_note: 'Coming soon' },
    1091500: {
        name: 'Adults Only Game', platforms: { windows: true, mac: false, linux: false }, adult: true, shots: 2, released: now - 900 * day,
        price: { final: '$59.99' },
    },
};
const link = (id, slug = '') => `https://store.steampowered.com/app/${id}/${slug}`;

const b = await launch(which, which === 'chromium' ? 9581 : 9582, { size: [1400, 900], hover: true });
const shot = (name) => b.shot(`${HOME}/shot-steam-${which}-${name}.png`).catch((e) => console.log('shot', e.message));
const MESSAGE = (label) => `[...document.querySelectorAll('[data-scroller] [data-id]')].filter((r) => r.textContent.includes(${q(label)})).at(-1)`;
const CARD = (label, app) => `${MESSAGE(label)}?.querySelector('[data-steam="${app}"]')`;
const cards = (label) => b.eval(`[...(${MESSAGE(label)}?.querySelectorAll('[data-steam]') || [])].map((x) => x.dataset.steam).join(' ')`);
const asked = () => b.intercepted.filter((u) => u.includes('/api/steam/'));
try {
    await pair(b, BOB);
    for (const [id, card] of Object.entries(CARDS)) await b.answer(`/api/steam/${id}`, 200, q(card));
    await b.answer('/api/steam/99999999', 404, q({ error: "Steam doesn't have this app." }));
    for (const id of ['1771300', '730', '1091500']) {
        await b.answer(`/api/steam/${id}/header`, 200, photo, 'image/jpeg');
        for (const n of [1, 2, 3]) await b.answer(`/api/steam/${id}/shots/${n}`, 200, photo, 'image/jpeg');
    }
    await b.answer('/api/steam/5503390/header', 200, photo, 'image/jpeg');

    step('links alone, and nothing asked, until bob turns the cards on');
    const first = `sale ${tag}`;
    lib('send', 'alice', general, `${first}: ${link(1771300, 'Kingdom_Come_Deliverance_II/?snr=1_7_15__13')} and again ${link(1771300)}, free ${link(730)}, \`${link(570)}\` ||${link(440)}||`);
    lib('send', 'alice', general, `more ${tag}: ${link(5503390)} ${link(99999999)} https://steamcommunity.com/app/1091500/discussions/`);
    lib('send', 'alice', general, `four ${tag}: ${[1771300, 730, 5503390, 99999999].map((id) => link(id)).join(' ')}`);
    await b.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
    await waitFor(() => b.eval(`!!${MESSAGE(`four ${tag}`)}`), "alice's messages");
    await sleep(1500);
    c.report((await cards(first)) === '' && asked().length === 0, `with the cards off, links stay links, and the page asks nothing of Steam (${asked().length} asks)`);

    await openSettings(b, 'Chat');
    await waitFor(() => b.eval(`!!${SETTINGS}.querySelector('input#steam')`), 'the Steam switch');
    const off = await b.eval(`!${SETTINGS}.querySelector('input#steam').checked`);
    await b.eval(`${SETTINGS}.querySelector('input#steam').click(), true`);
    await b.eval(`${SETTINGS}.querySelector('button[aria-label=Close]').click(), true`);
    c.report(off && (await b.eval(`localStorage.getItem('DENS_STEAM')`)) === '1', 'the switch starts off, and on, the browser keeps it');

    step('each game once, three to a message, none in code or a spoiler');
    await waitFor(async () => (await cards(first)) !== '', 'the cards');
    const each = { first: await cards(first), more: await cards(`more ${tag}`), four: await cards(`four ${tag}`) };
    c.report(each.first === '1771300 730' && each.more === '5503390 99999999 1091500' && each.four === '1771300 730 5503390',
        `cards for each game once, three at most, none in code or a spoiler: ${q(each)}`);

    step('a sale: its card, which kept its size as it loaded');
    // A card asks once it's on screen.
    const show = (label, app) => b.eval(`${CARD(label, app)}.scrollIntoView({ block: 'center' }), true`);
    await show(first, 1771300);
    const box = (label, app) => b.eval(`(() => { const r = ${CARD(label, app)}.getBoundingClientRect(); return Math.round(r.width) + 'x' + Math.round(r.height); })()`);
    await waitFor(() => b.eval(`${CARD(first, 1771300)}?.dataset.state === 'found'`), "the sale's card", 10000);
    await waitFor(() => b.eval(`${CARD(first, 1771300)}.querySelector('img')?.naturalWidth > 0`), "its picture", 10000);
    const loaded = await box(first, 1771300);
    const sale = JSON.parse(await b.eval(`(() => { const k = ${CARD(first, 1771300)}, t = (s) => k.querySelector(s)?.textContent.trim() || '';
        return JSON.stringify({ name: t('[data-steam-name]'), href: k.querySelector('[data-steam-name]').getAttribute('href'), by: t('[data-steam-by]'),
            reviews: t('[data-steam-reviews]'), players: t('[data-steam-players]'), systems: t('[data-steam-systems]'), price: t('[data-steam-price]'), when: t('[data-steam-when]') }); })()`));
    c.report(sale.name === CARDS[1771300].name && sale.href === 'https://store.steampowered.com/app/1771300/' && sale.by === 'Warhorse Studios · Story Rich · Open World · RPG'
        && /^Very Positive\s*93% of 148\.5K$/.test(sale.reviews) && sale.players === '7,694 playing' && /^Windows · Linux\s*·\s*Steam Deck\s*Verified$/.test(sale.systems)
        && /^-67%\s*\$59\.99\s*\$19\.79$/.test(sale.price) && /^Sale ends /.test(sale.when), `the sale's card says it all: ${q(sale)}`);
    // Its size, from a fresh card before its answer comes, against the one
    // loaded.
    const sizes = JSON.parse(await b.eval(`(() => { const k = ${CARD(first, 1771300)}; const copy = k.cloneNode(false); copy.dataset.state = 'loading';
        return JSON.stringify({ width: k.offsetWidth, height: k.offsetHeight }); })()`));
    c.report(loaded === '460x' + sizes.height && sizes.width === 460, `the card is 460 pixels wide, its picture and rows held before they load: ${loaded}`);
    await b.eval(`${CARD(first, 1771300)}.scrollIntoView({ block: 'center' }), true`);
    await sleep(400);
    await shot('sale');

    step('its arrows step through the screenshots, each asked for as it shows');
    const before = asked().filter((u) => u.includes('/shots/')).length;
    const pictures = [];
    for (let i = 0; i < 4; i++) {
        await b.eval(`${CARD(first, 1771300)}.querySelector('button[aria-label="Next picture"]').click(), true`);
        await sleep(400);
        pictures.push(await b.eval(`${CARD(first, 1771300)}.querySelector('img')?.getAttribute('src') || ''`));
    }
    const shots = asked().filter((u) => u.includes('/shots/')).length - before;
    c.report(q(pictures) === q(['/api/steam/1771300/shots/1', '/api/steam/1771300/shots/2', '/api/steam/1771300/shots/3', '/api/steam/1771300/header'])
        && shots >= 3 && (await box(first, 1771300)) === loaded, `the arrows step through three screenshots and back, ${shots} asked, the card's size kept: ${q(pictures)}`);

    step('a free game, one coming, and one Steam lacks');
    for (const [label, app] of [[first, 730], [`more ${tag}`, 5503390], [`more ${tag}`, 99999999]]) {
        await show(label, app);
        await sleep(300);
    }
    await waitFor(() => b.eval(`${CARD(first, 730)}?.dataset.state === 'found' && ${CARD(`more ${tag}`, 99999999)}?.dataset.state === 'missing'`), 'the other cards', 10000);
    const free = await b.eval(`${CARD(first, 730)}.querySelector('[data-steam-price]').textContent.trim() + ' | ' + ${CARD(first, 730)}.querySelector('[data-steam-players]').textContent.trim() + ' | ' + ${CARD(first, 730)}.querySelector('[data-steam-deck]').textContent.trim()`);
    const coming = await b.eval(`(${CARD(`more ${tag}`, 5503390)}.querySelector('[data-steam-price]')?.textContent || '') + ' | ' + ${CARD(`more ${tag}`, 5503390)}.querySelector('[data-steam-when]').textContent.trim()`);
    const missing = await b.eval(`${CARD(`more ${tag}`, 99999999)}.textContent`);
    c.report(/^Free to play \| 1\.2M playing \| · ?Steam Deck\s*Playable$/.test(free) && coming === ' | Coming soon' && missing.includes("Steam doesn't have this game."),
        `free: ${free}; coming: ${coming}; Steam lacks one: ${missing.includes("Steam doesn't have this game.")}`);

    step("one for adults only: covered, its pictures unasked until bob shows them");
    await show(`more ${tag}`, 1091500);
    await waitFor(() => b.eval(`${CARD(`more ${tag}`, 1091500)}?.dataset.state === 'found'`), 'the adult card', 10000);
    const covered = await b.eval(`!!${CARD(`more ${tag}`, 1091500)}.querySelector('[data-steam-cover]') && !${CARD(`more ${tag}`, 1091500)}.querySelector('img')`);
    const unasked = !asked().some((u) => u.includes('/api/steam/1091500/'));
    await b.eval(`${CARD(`more ${tag}`, 1091500)}.scrollIntoView({ block: 'center' }), true`);
    await sleep(300);
    await shot('adult');
    await b.eval(`${CARD(`more ${tag}`, 1091500)}.querySelector('[data-steam-cover]').click(), true`);
    const shown = await waitFor(() => b.eval(`${CARD(`more ${tag}`, 1091500)}.querySelector('img')?.naturalWidth > 0`), 'its picture shown', 10000).then(() => true, () => false);
    c.report(covered && unasked && shown, `the adult card covers its pictures and asks for none (${covered}, ${unasked}) until bob shows them (${shown})`);
    await b.eval(`${MESSAGE(`more ${tag}`)}.scrollIntoView({ block: 'center' }), true`);
    await sleep(300);
    await shot('more');
} catch (e) {
    c.fail(e);
} finally {
    await b.close();
    await sleep(200);
    c.finish();
}
