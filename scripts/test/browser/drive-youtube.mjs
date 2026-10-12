// The M4.1 check: Alice posts YouTube links; Bob turns players on, plays
// them, and checks what the page does, in Chromium and Firefox, with each
// browser's own autoplay rules. YouTube is on the internet, so this check
// needs it.
//
//   run.sh youtube chromium|firefox chromium|firefox   (alice's, then bob's)
import { setTimeout as sleep } from 'node:timers/promises';
import crypto from 'node:crypto';
import { HOME, SETTINGS, checks, launch, openSettings, token, waitFor } from './browsers.mjs';

const [, , aName, bName] = process.argv;
const { DEN: den, GENERAL: general, OTHER: other, DM: dm } = process.env;
const RICK = 'dQw4w9WgXcQ';
const ZOO = 'jNQXAC9IVRw';
const tag = Date.now().toString(36);

// Counts the page's requests about videos, as covers make them.
const PRELOAD = `() => {
    if (!location.origin.startsWith('http://localhost')) return;
    window.__yt = 0;
    window.__errors = [];
    addEventListener('error', (e) => window.__errors.push(String(e.message) + ' @' + (e.filename || '').split('/').pop() + ':' + e.lineno));
    addEventListener('unhandledrejection', (e) => window.__errors.push('rejection: ' + String(e.reason && (e.reason.stack || e.reason))));
    new PerformanceObserver((list) => {
        for (const e of list.getEntries()) if (e.name.includes('/api/youtube/')) window.__yt++;
    }).observe({ type: 'resource', buffered: true });
}`;

// What YouTube's player says inside its frame.
const STATE = `JSON.stringify((() => {
    const v = document.querySelector('video');
    const err = document.querySelector('.ytp-error');
    return { error: err ? err.innerText.replace(/\\s+/g, ' ').trim().slice(0, 120) : null, ad: !!document.querySelector('.ad-showing'),
        time: v ? Math.round(v.currentTime * 10) / 10 : null, paused: v ? v.paused : null, muted: v ? v.muted : null };
})())`;

// popup has YouTube's player open a page, as its links do, and reports
// whether it opened cut off from the page.
async function popup(b, match) {
    const open = `window.open('https://www.youtube.com/robots.txt', '_blank'), true`;
    if (b.name === 'chromium') {
        await b.cdp('Runtime.evaluate', { expression: open, userGesture: true }, await b.frame(match));
        await sleep(3000);
        const { targetInfos } = await b.cdp('Target.getTargets', {}, null);
        const popups = targetInfos.filter((t) => t.type === 'page' && t.url.includes('robots.txt'));
        for (const p of popups) await b.cdp('Target.closeTarget', { targetId: p.targetId }, null);
        return popups.length === 1 && popups[0].canAccessOpener === false ? 'opened, and cut off from the page' : JSON.stringify(popups);
    }
    await b.bidi('script.evaluate', { expression: open, target: { context: await b.frame(match) }, awaitPromise: true, userActivation: true });
    await sleep(3000);
    const { contexts } = await b.bidi('browsingContext.getTree', {});
    const popups = contexts.filter((c) => c.url.includes('robots.txt'));
    const cut = [];
    for (const p of popups) {
        cut.push((await b.bidi('script.evaluate', { expression: 'String(window.opener === null)', target: { context: p.context }, awaitPromise: true })).result.value);
        await b.bidi('browsingContext.close', { context: p.context });
    }
    return popups.length === 1 && cut[0] === 'true' ? 'opened, and cut off from the page' : JSON.stringify({ popups, cut });
}

const c = checks();
const report = c.report;
const sides = [
    { browser: aName, instance: 'm2a', port: 18484, debug: 9441 },
    { browser: bName, instance: 'm2b', port: 18486, debug: 9442 },
];
const [A, B] = sides;
const opened = [];

// The cover of a video in the message that carries this run's label.
const COVER = (label, id) => `[...document.querySelectorAll('[data-id]')].filter((r) => r.textContent.includes(${JSON.stringify(label)})).at(-1)?.querySelector('[data-video="${id}"]')`;
const post = (s, channel, text) => s.b.eval(`(async () => (await fetch('/api/dens/${den}/channels/${channel}/messages', { method: 'POST',
    headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ nonce: ${JSON.stringify(crypto.randomBytes(16).toString('base64url'))}, text: ${JSON.stringify(text)} }) })).status)()`);

async function setPlayers(s, on) {
    await openSettings(s.b, 'Chat & media');
    const BOX = `${SETTINGS}.querySelector('input#youtube')`;
    await waitFor(() => s.b.eval(`!!${BOX}`), `${s.browser} settings`);
    await s.b.eval(`(() => { const box = ${BOX}; if (box.checked !== ${on}) box.click(); return true; })()`);
    await s.b.eval(`${SETTINGS}.querySelector('button[aria-label=Close]').click(), true`);
}

// playCover clicks a cover as a member would, and waits for the player.
async function playCover(s, label, id) {
    const at = JSON.parse(await s.b.eval(`(() => { const b = ${COVER(label, id)}.querySelector('button'); b.scrollIntoView({ block: 'center' });
        const r = b.getBoundingClientRect(); return JSON.stringify({ x: r.left + r.width / 2, y: r.top + r.height / 2 }); })()`));
    await sleep(300);
    await s.b.click(at.x, at.y);
    return JSON.parse(await waitFor(() => s.b.eval(`(() => { const f = ${COVER(label, id)}?.querySelector('iframe'); return f ? JSON.stringify({ src: f.src,
        sandbox: f.getAttribute('sandbox'), referrer: f.getAttribute('referrerpolicy'), allow: f.getAttribute('allow') }) : false; })()`), `${s.browser} player for ${id}`));
}

async function playing(s, id, from) {
    const st = await waitFor(async () => {
        const v = JSON.parse((await s.b.frameEval(id, STATE)) || 'null');
        return v && (v.error || v.ad || (v.time !== null && v.time > from + 1)) ? v : false;
    }, `${s.browser} plays ${id}`, 25000).catch((e) => ({ timeout: e.message }));
    const good = !st.timeout && !st.error && (st.ad || (st.paused === false && st.muted === false && st.time >= from));
    report(good, `${s.browser} plays ${id}${from ? ` from ${from} s` : ''}, with sound: ${JSON.stringify(st)}`);
}

try {
    for (const s of sides) {
        s.b = await launch(s.browser, s.debug, { autoplay: 'gesture', preload: PRELOAD, size: [1280, 900] });
        opened.push(s.b);
        // An old link at 127.0.0.1 ends at localhost, paired by its token.
        await s.b.go(`http://127.0.0.1:${s.port}/#token=${token(s.instance)}`);
        const where = await waitFor(async () => {
            const origin = await s.b.eval('location.origin');
            return origin === `http://localhost:${s.port}` && (await s.b.eval(`!!document.querySelector('a[href="/settings"]')`)) && origin;
        }, `${s.browser} paired`).catch((e) => e.message);
        report(where === `http://localhost:${s.port}`, `${s.browser}: a link to 127.0.0.1 paired the page at ${where}`);
    }

    await B.b.go(`http://localhost:${B.port}/den/${den}/${general}`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-id]') || !!document.querySelector('textarea')`), `${B.browser} general`);

    // Off by default: links stay links, and nothing asks about videos.
    const rick = `${tag}-rick`;
    await post(A, general, `${rick} https://youtu.be/${RICK}?si=abc&t=42 ok`);
    await waitFor(() => B.b.eval(`!!document.querySelector('a[href="https://www.youtube.com/watch?v=${RICK}&t=42"]')`), `${B.browser} sees the link`);
    await sleep(1500);
    const off = await B.b.eval(`JSON.stringify({ covers: document.querySelectorAll('[data-video]').length, asked: window.__yt })`);
    report(off === '{"covers":0,"asked":0}', `${B.browser} with players off: ${off}`);

    // Turned on in the settings, without a reload.
    await setPlayers(B, true);
    const cover = await waitFor(() => B.b.eval(`(() => { const c = ${COVER(rick, RICK)}; if (!c) return false; const img = c.querySelector('img');
        const r = c.getBoundingClientRect(); const t = c.textContent;
        return t.includes('Rick Astley') && img?.complete && img.naturalWidth > 0
            ? JSON.stringify({ title: t.trim().slice(0, 60), box: Math.round(r.width) + 'x' + Math.round(r.height), picture: img.naturalWidth + 'x' + img.naturalHeight }) : false; })()`),
    `${B.browser} cover`).catch((e) => e.message);
    report(cover.includes('"box":"400x225"') && cover.includes('"picture":"480x360"'), `${B.browser} cover: ${cover}`);
    await B.b.eval(`${COVER(rick, RICK)}.scrollIntoView({ block: 'center' }), true`);
    await B.b.shot(`${HOME}/shot-youtube-${B.browser}-cover.png`);

    const frame = await playCover(B, rick, RICK);
    report(frame.src === `https://www.youtube-nocookie.com/embed/${RICK}?autoplay=1&start=42` && frame.referrer === 'strict-origin-when-cross-origin' &&
        frame.sandbox === 'allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox' &&
        frame.allow === 'autoplay; encrypted-media; picture-in-picture; fullscreen', `${B.browser} player: ${JSON.stringify(frame)}`);
    await playing(B, RICK, 42);
    await B.b.shot(`${HOME}/shot-youtube-${B.browser}-playing.png`);
    report((await popup(B.b, RICK)) === 'opened, and cut off from the page', `${B.browser}: YouTube's popup can't reach the page`);

    // A second video stops the first.
    const zoo = `${tag}-zoo`;
    await post(A, general, `${zoo} https://www.youtube.com/watch?v=${ZOO}`);
    await waitFor(() => B.b.eval(`!!${COVER(zoo, ZOO)}?.querySelector('button')`), `${B.browser} second cover`);
    await playCover(B, zoo, ZOO);
    const one = await B.b.eval(`JSON.stringify({ first: !!${COVER(rick, RICK)}.querySelector('iframe'), second: !!${COVER(zoo, ZOO)}.querySelector('iframe'), frames: document.querySelectorAll('iframe').length })`);
    report(one === '{"first":false,"second":true,"frames":1}', `${B.browser}: starting another stops the first: ${one}`);
    await playing(B, ZOO, 0);

    // Scrolled out of view, it keeps playing.
    for (let i = 0; i < 30; i++) await post(A, general, `${tag} filler ${i}\nsecond line\nthird line`);
    await waitFor(() => B.b.eval(`document.body.textContent.includes('${tag} filler 29')`), `${B.browser} filler`);
    const away = await B.b.eval(`(() => { const c = ${COVER(zoo, ZOO)}; let list = c.parentElement;
        while (list && !['auto', 'scroll'].includes(getComputedStyle(list).overflowY)) list = list.parentElement;
        list.scrollTop = list.scrollHeight; return 'scrolled'; })()`);
    await sleep(1000);
    const out = await B.b.eval(`(() => { const c = ${COVER(zoo, ZOO)}; let list = c.parentElement;
        while (list && !['auto', 'scroll'].includes(getComputedStyle(list).overflowY)) list = list.parentElement;
        const r = c.getBoundingClientRect(), v = list.getBoundingClientRect();
        return JSON.stringify({ hidden: r.bottom < v.top || r.top > v.bottom, frame: !!c.querySelector('iframe') }); })()`);
    const t1 = JSON.parse((await B.b.frameEval(ZOO, STATE)) || '{}');
    await sleep(2500);
    const t2 = JSON.parse((await B.b.frameEval(ZOO, STATE)) || '{}');
    report(away === 'scrolled' && out === '{"hidden":true,"frame":true}' && t2.paused === false && t2.time > t1.time,
        `${B.browser}: scrolled out of view, it plays on: ${out}, ${t1.time} s then ${t2.time} s`);

    // Another channel stops it, and coming back finds covers.
    // A channel's # is an icon (M8.3), so its button reads its name alone.
    const channel = (name) => `[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === '${name}')`;
    await B.b.eval(`${channel('other')}.click(), true`);
    await waitFor(() => B.b.eval(`location.pathname.endsWith('/${other}')`), `${B.browser} other channel`);
    await sleep(800);
    const elsewhere = await B.b.eval(`document.querySelectorAll('iframe').length`);
    await B.b.eval(`${channel('general')}.click(), true`);
    await waitFor(() => B.b.eval(`!!${COVER(zoo, ZOO)}`), `${B.browser} back in general`);
    const back = await B.b.eval(`JSON.stringify({ frames: document.querySelectorAll('iframe').length, play: !!${COVER(zoo, ZOO)}.querySelector('button') })`);
    report(elsewhere === 0 && back === '{"frames":0,"play":true}', `${B.browser}: another channel stops it (${elsewhere} frames), and back it's a cover: ${back}`);

    // In a DM, sealed end to end.
    const dmLabel = `${tag}-dm`;
    await post(A, dm, `${dmLabel} https://www.youtube.com/watch?v=${RICK}&t=1m30s`);
    await B.b.eval(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Direct messages').click(), true`);
    await waitFor(() => B.b.eval(`!![...document.querySelectorAll('nav[aria-label="Direct messages"] button')].find((b) => b.textContent.includes('Alice'))`), `${B.browser} DM list`);
    await B.b.eval(`[...document.querySelectorAll('nav[aria-label="Direct messages"] button')].find((b) => b.textContent.includes('Alice')).click(), true`);
    const dmCover = await waitFor(() => B.b.eval(`(() => { const c = ${COVER(dmLabel, RICK)}; return c && c.textContent.includes('Rick Astley') ? 'cover' : false; })()`), `${B.browser} DM cover`).catch((e) => e.message);
    report(dmCover === 'cover', `${B.browser}: a DM's link gets a cover: ${dmCover}`);
    if (dmCover === 'cover') {
        const f = await playCover(B, dmLabel, RICK);
        report(f.src.endsWith('&start=90'), `${B.browser}: the DM's video starts at 1m30s: ${f.src}`);
        await playing(B, RICK, 90);
    }

    // What a cover says when YouTube won't play a video here, or has none.
    if (B.b.answer) {
        const [refused, missing] = ['9bZkp7q19f0', 'aaaaaaaaaaa'];
        await B.b.answer(`/api/youtube/${refused}`, 200, '{"playable":false}');
        await B.b.answer(`/api/youtube/${missing}`, 404, '{"error":"YouTube doesn\'t have this video."}');
        const label = `${tag}-notices`;
        await B.b.eval(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Den').click(), true`);
        await B.b.eval(`[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === 'general').click(), true`);
        await waitFor(() => B.b.eval(`document.body.textContent.includes('${tag} filler 29')`), `${B.browser} general loaded`);
        console.log('posted notices:', await post(A, general, `${label} https://www.youtube.com/watch?v=${refused} https://www.youtube.com/watch?v=${missing}`));
        const notices = await waitFor(() => B.b.eval(`(() => { const a = ${COVER(label, refused)}, b = ${COVER(label, missing)};
            if (!a?.textContent.includes('only on YouTube') || !b?.textContent.includes("doesn't have")) return false;
            const link = a.querySelector('a');
            return JSON.stringify({ refused: a.textContent.trim(), link: link.href, rel: link.rel, target: link.target, play: !!a.querySelector('button'), missing: b.textContent.trim() }); })()`),
        `${B.browser} notices`).catch(async (e) => e.message + ' | page: ' + await B.b.eval(`JSON.stringify({ path: location.pathname,
            posted: ${JSON.stringify(label)}, rows: [...document.querySelectorAll('[data-id]')].filter((r) => r.textContent.includes(${JSON.stringify(label)})).map((r) => r.innerHTML.slice(0, 1500)),
            last: [...document.querySelectorAll('[data-id]')].at(-1)?.textContent.slice(0, 200), asked: window.__yt, errors: window.__errors })`) + ' | intercepted: ' + JSON.stringify(B.b.intercepted));
        report(notices.includes(`"link":"https://www.youtube.com/watch?v=${refused}"`) && notices.includes('"rel":"noopener noreferrer nofollow"') && notices.includes('"play":false'),
            `${B.browser}: covers for a video that plays only on YouTube, and for one it doesn't have: ${notices}`);
        await B.b.eval(`${COVER(label, refused)}.scrollIntoView({ block: 'center' }), true`);
        await B.b.shot(`${HOME}/shot-youtube-${B.browser}-notices.png`);
    }

    // The page frames nothing else.
    const csp = await B.b.eval(`new Promise((resolve) => {
        const f = document.createElement('iframe');
        document.addEventListener('securitypolicyviolation', (e) => { f.remove(); resolve((e.effectiveDirective || e.violatedDirective) + ' ' + e.blockedURI); }, { once: true });
        f.src = 'https://example.com/'; document.body.append(f);
        setTimeout(() => { f.remove(); resolve('allowed'); }, 4000); })`);
    report(csp.startsWith('frame-src'), `${B.browser}: a frame from elsewhere is refused: ${csp}`);

    // Off again: covers and players go.
    await setPlayers(B, false);
    await sleep(800);
    const gone = await B.b.eval(`JSON.stringify({ covers: document.querySelectorAll('[data-video]').length, frames: document.querySelectorAll('iframe').length })`);
    report(gone === '{"covers":0,"frames":0}', `${B.browser} with players off again: ${gone}`);
    // From a DM back to a channel, as the notices step did, a message
    // posted as the channel opens shows (#17).
    for (const wait of [0, 3000]) {
        await B.b.eval(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Direct messages').click(), true`);
        await waitFor(() => B.b.eval(`!![...document.querySelectorAll('nav[aria-label="Direct messages"] button')].find((b) => b.textContent.includes('Alice'))`), 'DM list');
        await B.b.eval(`[...document.querySelectorAll('nav[aria-label="Direct messages"] button')].find((b) => b.textContent.includes('Alice')).click(), true`);
        await sleep(2000);
        await B.b.eval(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Den').click(), true`);
        await B.b.eval(`[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === 'general').click(), true`);
        await sleep(wait);
        const race = `${tag}-dmrace-${wait}`;
        await post(A, general, race);
        const seen = await waitFor(() => B.b.eval(`document.body.textContent.includes('${race}')`), 'race', 10000).then(() => true, () => false);
        const where = await B.b.eval('location.pathname');
        report(seen, `${B.browser}: from a DM to #general, a message posted after ${wait} ms shows (at ${where})`);
    }
    // And one posted as a channel opens from another.
    for (const [from, to] of [['other', general], ['general', other]]) {
        const target = to === general ? 'general' : 'other';
        await B.b.eval(`[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === '${from}').click(), true`);
        await sleep(1500);
        await B.b.eval(`[...document.querySelectorAll('nav[aria-label=Channels] button')].find((b) => b.textContent.trim() === '${target}').click(), true`);
        const race = `${tag}-race-${target}`;
        await post(A, to, race);
        const seen = await waitFor(() => B.b.eval(`document.body.textContent.includes('${race}')`), 'race', 10000).then(() => true, () => false);
        await B.b.go(`http://localhost:${B.port}/den/${den}/${to}`);
        const afterReload = await waitFor(() => B.b.eval(`document.body.textContent.includes('${race}')`), 'race after reload', 10000).then(() => true, () => false);
        report(seen && afterReload, `${B.browser}: a message posted as #${target} opens shows (${seen}), and after a reload (${afterReload})`);
    }
} catch (e) {
    c.fail(e);
} finally {
    for (const b of opened) await b.close().catch(() => {});
    c.finish();
}
