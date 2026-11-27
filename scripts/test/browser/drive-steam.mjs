// The steam check, of Steam's cards, sizing and primary trailers, as bob in
// the browser named, against a stand-in for Steam: the page's own requests
// for a card, its pictures and its trailer answered in the browser, with
// what bob's Dens would give, since the service's side is checked against
// Steam's recorded answers in Go. Alice posts links to games; bob sees links
// alone and asks nothing until he turns the cards on in Chat. Then each game
// linked gets a card, once and three to a message at most, none for a link
// in code or a spoiler: a sale with its cut, its prices and its end, reviews
// and who's playing, its systems and Steam Deck; a free game; one coming;
// one Steam doesn't have; and one for adults only, its pictures and trailer
// covered and unasked until bob shows them. A card starts at Small (400 px
// stacked), keeps its size from before it loads, resizes independently via
// Appearance's Steam cards setting (Medium 640 side by side; Large 960 and
// Largest 1280 stacked with a compact wide bar below), steps through
// screenshots (with the trailer play button only on the header slide), plays
// its dual-SourceBuffer fMP4 trailer on click with shared custom video
// controls, carousel browsing during playback and zero CSP violations, and
// shares one-at-a-time playback with YouTube players.
//
//   run.sh steam chromium|firefox
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, SETTINGS, checks, closeSettings, launch, lib, openAppearance, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general } = process.env;
const c = checks();
const step = (what) => console.error(`.. ${what}`);
const q = JSON.stringify;
const tag = Date.now().toString(36);
const photo = fs.readFileSync(`${HOME}/photo.jpg`);
const trailer = {
    vInit: fs.readFileSync(`${HOME}/videos/trailer-init-0.m4s`),
    v1: fs.readFileSync(`${HOME}/videos/trailer-chunk-0-1.m4s`),
    v2: fs.readFileSync(`${HOME}/videos/trailer-chunk-0-2.m4s`),
    aInit: fs.readFileSync(`${HOME}/videos/trailer-init-1.m4s`),
    a1: fs.readFileSync(`${HOME}/videos/trailer-chunk-1-1.m4s`),
    a2: fs.readFileSync(`${HOME}/videos/trailer-chunk-1-2.m4s`),
};
const day = 24 * 3600;
const now = Math.floor(Date.now() / 1000);

// Record any CSP violation anywhere in the page during the run.
const PRELOAD = `() => {
    window.__cspViolations = [];
    document.addEventListener('securitypolicyviolation', (e) => {
        window.__cspViolations.push({ directive: e.effectiveDirective, blockedURI: e.blockedURI });
    });
}`;

// What bob's Dens says of each game, as the service writes its cards.
const CARDS = {
    1771300: {
        name: 'Kingdom Come: Deliverance II', kind: 'game', developer: 'Warhorse Studios', description: 'An action RPG set in medieval Bohemia.',
        tags: ['Story Rich', 'Open World', 'RPG'], reviews: { score: 8, percent: 93, count: 148458 }, players: 7694,
        platforms: { windows: true, mac: false, linux: true }, deck: 'verified', released: now - 300 * day,
        price: { final: '$19.79', original: '$59.99', discount: 67, ends: now + 6 * day }, shots: 3, trailer: true,
    },
    730: {
        name: 'Counter-Strike 2', kind: 'game', developer: 'Valve', description: 'The next chapter in the CS story.', tags: ['FPS', 'Shooter'],
        reviews: { score: 6, percent: 86, count: 8400000 }, players: 1204331, platforms: { windows: true, mac: false, linux: true }, deck: 'playable',
        released: now - 4000 * day, free: true,
    },
    5503390: { name: "Blamcon's Light Gun Arcade", platforms: { windows: true, mac: false, linux: false }, coming_soon: true, release_note: 'Coming soon' },
    1091500: {
        name: 'Adults Only Game', platforms: { windows: true, mac: false, linux: false }, adult: true, shots: 2, trailer: true, released: now - 900 * day,
        price: { final: '$59.99' },
    },
};
const TRAILER_MANIFEST = {
    encoding: 'h264',
    duration: 6,
    video: [{ id: '0', width: 320, height: 180, mime: 'video/mp4', codecs: 'avc1.42c00c', segments: 2, segment_duration: 3 }],
    audio: { id: '1', mime: 'audio/mp4', codecs: 'mp4a.40.2', segments: 2, segment_duration: 3 },
};
const link = (id, slug = '') => `https://store.steampowered.com/app/${id}/${slug}`;
const YT_ID = 'dQw4w9WgXcQ';

// Wide enough for Largest (1280 px) inside the channel pane.
const b = await launch(which, which === 'chromium' ? 9581 : 9582, { size: [1900, 1000], hover: true, preload: PRELOAD });
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
    await b.answer('/api/steam/1771300/trailer/h264', 200, q(TRAILER_MANIFEST));
    await b.answer('/api/steam/1771300/trailer/h264/0/init', 200, trailer.vInit, 'video/mp4');
    await b.answer('/api/steam/1771300/trailer/h264/0/1', 200, trailer.v1, 'video/mp4');
    await b.answer('/api/steam/1771300/trailer/h264/0/2', 200, trailer.v2, 'video/mp4');
    await b.answer('/api/steam/1771300/trailer/h264/1/init', 200, trailer.aInit, 'audio/mp4');
    await b.answer('/api/steam/1771300/trailer/h264/1/1', 200, trailer.a1, 'audio/mp4');
    await b.answer('/api/steam/1771300/trailer/h264/1/2', 200, trailer.a2, 'audio/mp4');
    await b.answer(`/api/youtube/${YT_ID}`, 200, q({ title: 'Sample Video', channel: 'Channel', playable: true }));
    await b.answer(`/api/youtube/${YT_ID}/thumb`, 200, photo, 'image/jpeg');

    step('links alone, and nothing asked, until bob turns the cards on');
    const first = `sale ${tag}`;
    lib('send', 'alice', general, `${first}: ${link(1771300, 'Kingdom_Come_Deliverance_II/?snr=1_7_15__13')} and again ${link(1771300)}, free ${link(730)}, \`${link(570)}\` ||${link(440)}||`);
    lib('send', 'alice', general, `more ${tag}: ${link(5503390)} ${link(99999999)} https://steamcommunity.com/app/1091500/discussions/`);
    lib('send', 'alice', general, `four ${tag}: ${[1771300, 730, 5503390, 99999999].map((id) => link(id)).join(' ')}`);
    lib('send', 'alice', general, `yt ${tag}: https://www.youtube.com/watch?v=${YT_ID}`);
    await b.go(`http://localhost:${BOB.port}/den/${den}/${general}`);
    await waitFor(() => b.eval(`!!${MESSAGE(`yt ${tag}`)}`), "alice's messages");
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

    step('a sale: Small (400 px stacked) by default, kept its size as it loaded');
    const show = (label, app) => b.eval(`${CARD(label, app)}.scrollIntoView({ block: 'center' }), true`);
    await show(first, 1771300);
    const geom = (label, app) => b.eval(`JSON.stringify((() => {
        const k = ${CARD(label, app)}, pic = k.querySelector('[data-steam-picture]'), facts = pic.nextElementSibling;
        const r = k.getBoundingClientRect(), pr = pic.getBoundingClientRect(), fr = facts.getBoundingClientRect();
        return { w: Math.round(r.width), h: Math.round(r.height), pw: Math.round(pr.width), ph: Math.round(pr.height),
            stacked: fr.top >= pr.bottom - 1, beside: fr.left >= pr.right - 1 };
    })())`).then(JSON.parse);
    await waitFor(() => b.eval(`${CARD(first, 1771300)}?.dataset.state === 'found'`), "the sale's card", 10000);
    await waitFor(() => b.eval(`${CARD(first, 1771300)}.querySelector('img')?.naturalWidth > 0`), "its picture", 10000);
    const loaded = await geom(first, 1771300);
    const sale = JSON.parse(await b.eval(`(() => { const k = ${CARD(first, 1771300)}, t = (s) => k.querySelector(s)?.textContent.trim() || '';
        return JSON.stringify({ name: t('[data-steam-name]'), href: k.querySelector('[data-steam-name]').getAttribute('href'), by: t('[data-steam-by]'),
            reviews: t('[data-steam-reviews]'), players: t('[data-steam-players]'), systems: t('[data-steam-systems]'), price: t('[data-steam-price]'), when: t('[data-steam-when]') }); })()`));
    c.report(sale.name === CARDS[1771300].name && sale.href === 'https://store.steampowered.com/app/1771300/' && sale.by === 'Warhorse Studios · Story Rich · Open World · RPG'
        && /^Very Positive\s*93% of 148\.5K$/.test(sale.reviews) && sale.players === '7,694 playing' && /^Windows · Linux\s*·\s*Steam Deck\s*Verified$/.test(sale.systems)
        && /^-67%\s*\$59\.99\s*\$19\.79$/.test(sale.price) && /^Sale ends /.test(sale.when), `the sale's card says it all: ${q(sale)}`);
    c.report(loaded.w === 400 && loaded.stacked && !loaded.beside, `Small is 400 pixels wide, stacked: ${q(loaded)}`);
    await shot('sale-small');

    step('its arrows step through the screenshots, each asked for as it shows, play button only on header');
    const before = asked().filter((u) => u.includes('/shots/')).length;
    const pictures = [];
    const playOnSlide = [];
    for (let i = 0; i < 4; i++) {
        await b.eval(`${CARD(first, 1771300)}.querySelector('button[aria-label="Next picture"]').click(), true`);
        await sleep(400);
        pictures.push(await b.eval(`${CARD(first, 1771300)}.querySelector('img')?.getAttribute('src') || ''`));
        playOnSlide.push(await b.eval(`!!${CARD(first, 1771300)}.querySelector('[data-steam-play]')`));
    }
    const shots = asked().filter((u) => u.includes('/shots/')).length - before;
    const afterShots = await geom(first, 1771300);
    c.report(q(pictures) === q(['/api/steam/1771300/shots/1', '/api/steam/1771300/shots/2', '/api/steam/1771300/shots/3', '/api/steam/1771300/header'])
        && q(playOnSlide) === q([false, false, false, true])
        && shots >= 3 && afterShots.w === loaded.w && afterShots.h === loaded.h,
        `the arrows step through three screenshots and back, ${shots} asked, play button header-only (${q(playOnSlide)}), size kept: ${q(pictures)}`);

    step("Appearance's Steam cards setting resizes Medium side by side and Large / Largest stacked with wide bar");
    const slideSteam = async (choice) => {
        await openAppearance(b, 'Messages');
        await waitFor(() => b.eval(`!!document.querySelector('input[type=range][data-steam-size]')`), 'the Steam cards slider');
        await b.eval(`(() => { const s = document.querySelector('input[type=range][data-steam-size]');
            Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(s, ${['small', 'medium', 'large', 'largest'].indexOf(choice)});
            s.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
        await sleep(200);
        await closeSettings(b);
        await show(first, 1771300);
        await sleep(300);
        return geom(first, 1771300);
    };
    const med = await slideSteam('medium');
    await shot('sale-medium');
    const lrg = await slideSteam('large');
    await shot('sale-large');
    const max = await slideSteam('largest');
    const fits = await b.eval(`JSON.stringify((() => { const s = document.querySelector('[data-scroller]'); return [s.scrollWidth, s.clientWidth]; })())`).then(JSON.parse);
    const kept = await b.eval(`JSON.stringify([localStorage.getItem('DENS_STEAM_SIZE'), localStorage.getItem('DENS_PLAYER_SIZE')])`);
    c.report(med.w === 640 && med.beside && lrg.w === 960 && lrg.stacked && !lrg.beside && max.w === 1280 && max.stacked && !max.beside && fits[0] <= fits[1] && kept === '["largest",null]',
        `Medium 640 side by side, Large 960 and Largest 1280 stacked within the pane (${q({ med, lrg, max, fits, kept })})`);
    await slideSteam('medium');

    step('trailer: zero fetches before click, custom video controls, carousel browsing during play, 0 CSP violations, shared lock with YouTube');
    const noTrailerYet = !asked().some((u) => u.includes('/trailer/'));
    const beforePlay = await geom(first, 1771300);
    await b.eval(`${CARD(first, 1771300)}.querySelector('[data-steam-play]').click(), true`, true);
    const playing = await waitFor(() => b.eval(`(() => {
        const v = ${CARD(first, 1771300)}?.querySelector('video[data-steam-video]');
        if (!v || v.videoWidth === 0 || v.currentTime < 0.2) return false;
        return JSON.stringify({ w: v.videoWidth, h: v.videoHeight, t: Number(v.currentTime.toFixed(2)), paused: v.paused, muted: v.muted });
    })()`), 'Steam trailer playing', 15000).then(JSON.parse, () => null);
    const diag = playing ? null : await b.eval(`(() => {
        const k = ${CARD(first, 1771300)}, v = k?.querySelector('video[data-steam-video]');
        return JSON.stringify({ hasVideo: !!v, w: v?.videoWidth, h: v?.videoHeight, t: v?.currentTime, paused: v?.paused, muted: v?.muted, rs: v?.readyState, err: v?.error?.code, msg: k?.querySelector('[data-steam-trailer-error]')?.textContent || '' });
    })()`);
    const duringPlay = await geom(first, 1771300);
    const customControls = await b.eval(`(() => { const k = ${CARD(first, 1771300)}; return !!k?.querySelector('[data-video-controls]') && !k?.querySelector('video[controls]'); })()`);
    const cspViolations = await b.eval(`JSON.stringify(window.__cspViolations || [])`).then(JSON.parse);
    await shot('trailer-playing');
    c.report(noTrailerYet && playing && playing.w === 320 && playing.h === 180 && playing.t >= 0.2 && customControls
        && duringPlay.w === beforePlay.w && duringPlay.h === beforePlay.h && cspViolations.length === 0,
        `zero trailer asks before click (${noTrailerYet}), trailer played (${q(playing || diag)}), custom controls (${customControls}), box unchanged (${q(duringPlay)}), 0 CSP violations (${q(cspViolations)})`);

    // Stepping through screenshots while playing keeps the trailer running behind a return pill.
    await b.eval(`${CARD(first, 1771300)}.querySelector('button[aria-label="Next picture"]').click(), true`);
    await sleep(300);
    const browsed = JSON.parse(await b.eval(`(() => {
        const k = ${CARD(first, 1771300)};
        return JSON.stringify({
            shot: k?.querySelector('img')?.getAttribute('src') || '',
            hasVideo: !!k?.querySelector('video[data-steam-video]'),
            pill: k?.querySelector('[data-steam-playing-pill]')?.textContent.trim() || '',
            playBtn: !!k?.querySelector('[data-steam-play]'),
        });
    })()`));
    await b.eval(`${CARD(first, 1771300)}.querySelector('[data-steam-playing-pill] button').click(), true`);
    await sleep(200);
    const returned = await b.eval(`!!${CARD(first, 1771300)}.querySelector('video[data-steam-video]:not(.hidden)') && !${CARD(first, 1771300)}.querySelector('[data-steam-playing-pill]')`);
    c.report(browsed.shot === '/api/steam/1771300/shots/1' && browsed.hasVideo && browsed.pill.includes('Trailer') && !browsed.playBtn && returned,
        `carousel steps to screenshots during trailer (${q(browsed)}) and pill returns (${returned})`);

    // Turn on YouTube players and start a YouTube player: Steam trailer stops.
    await b.eval(`localStorage.setItem('DENS_YOUTUBE', '1'), true`);
    await openSettings(b, 'Chat');
    await waitFor(() => b.eval(`!!${SETTINGS}.querySelector('input#youtube')`), 'the YouTube switch');
    await b.eval(`(() => { const el = ${SETTINGS}.querySelector('input#youtube'); if (!el.checked) el.click(); return true; })()`);
    await b.eval(`${SETTINGS}.querySelector('button[aria-label=Close]').click(), true`);
    const YT = `${MESSAGE(`yt ${tag}`)}?.querySelector('[data-video="${YT_ID}"]')`;
    await waitFor(() => b.eval(`!!${YT}?.querySelector('button')`), 'the YouTube cover', 10000);
    await b.eval(`${YT}.querySelector('button').click(), true`);
    const ytTookOver = await waitFor(() => b.eval(`!!${YT}.querySelector('iframe') && !${CARD(first, 1771300)}.querySelector('[data-steam-video]')`), 'YouTube stopping Steam trailer', 10000).then(() => true, () => false);
    // Starting Steam trailer again stops YouTube back to its cover.
    await show(first, 1771300);
    await b.eval(`${CARD(first, 1771300)}.querySelector('[data-steam-play]').click(), true`);
    const steamTookOver = await waitFor(() => b.eval(`!!${CARD(first, 1771300)}.querySelector('[data-steam-video]') && !${YT}.querySelector('iframe')`), 'Steam trailer stopping YouTube', 10000).then(() => true, () => false);
    await b.eval(`${CARD(first, 1771300)}.querySelector('[data-steam-close]').click(), true`);
    const closed = await waitFor(() => b.eval(`!${CARD(first, 1771300)}.querySelector('[data-steam-video]') && !!${CARD(first, 1771300)}.querySelector('img')`), 'trailer closed back to picture', 5000).then(() => true, () => false);
    c.report(ytTookOver && steamTookOver && closed, `one-at-a-time lock with YouTube (${ytTookOver}, ${steamTookOver}) and close returns to picture (${closed})`);

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

    step("one for adults only: covered, its pictures and trailer unasked until bob shows them");
    await show(`more ${tag}`, 1091500);
    await waitFor(() => b.eval(`${CARD(`more ${tag}`, 1091500)}?.dataset.state === 'found'`), 'the adult card', 10000);
    const covered = await b.eval(`!!${CARD(`more ${tag}`, 1091500)}.querySelector('[data-steam-cover]') && !${CARD(`more ${tag}`, 1091500)}.querySelector('img') && !${CARD(`more ${tag}`, 1091500)}.querySelector('[data-steam-play]')`);
    const unasked = !asked().some((u) => u.includes('/api/steam/1091500/'));
    await b.eval(`${CARD(`more ${tag}`, 1091500)}.scrollIntoView({ block: 'center' }), true`);
    await sleep(300);
    await shot('adult');
    await b.eval(`${CARD(`more ${tag}`, 1091500)}.querySelector('[data-steam-cover]').click(), true`);
    const shown = await waitFor(() => b.eval(`${CARD(`more ${tag}`, 1091500)}.querySelector('img')?.naturalWidth > 0 && !!${CARD(`more ${tag}`, 1091500)}.querySelector('[data-steam-play]')`), 'its picture and play button shown', 10000).then(() => true, () => false);
    c.report(covered && unasked && shown, `the adult card covers its pictures and trailer and asks for none (${covered}, ${unasked}) until bob shows them (${shown})`);
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
