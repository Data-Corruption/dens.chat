// The site check (M17.1): the public site's install guide as a reader meets
// it, at 390 by 844 as a phone, in Chromium (a mobile viewport and a touch
// screen) and Firefox (a touch screen), and at a desktop's 1400 by 900 with
// a mouse, in both. The choice at /docs/install/ shows its four cards, and
// each opens its platform's page, which holds that platform's steps and
// none of another's, its folds closed until one is opened; the link older
// releases give for the Flatpak lands on the choice, the Flatpak's card in
// view, and Android's old page leads to its new one; every link on the
// pages walked, and every link into the guides that the app and its
// releases carry, or that the other guides hold, reaches a page holding its
// anchor; and each screen of every page, its folds closed and open, holds
// to the phone check's audits, with nothing making the page scroll
// sideways. The pictures and audits land in ~/shot-site/<mode>.
//
//   node drive-site.mjs [chromium|firefox|chromium-desktop|firefox-desktop]...
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';
import { HOME, checks, launch } from './browsers.mjs';
import { audits, helpers, waitFor } from './screens.mjs';

const SITE = process.env.SITE;
const modes = process.argv.slice(2).filter(Boolean);
const { report, fail, finish } = checks();

// The cards on the choice, in order, and where each goes.
const CARDS = [
    ['Windows', '/docs/install/windows/'],
    ['Linux', '/docs/install/linux/'],
    ['Flatpak (client only)', '/docs/install/flatpak/'],
    ['Android (client only)', '/docs/install/android/'],
];
// Each platform's page holds its own steps, and none of another's: what
// marks each platform's steps.
const MARKS = {
    windows: ["irm 'https://releases.dens.chat/install.ps1' | iex", 'Terminal (Admin)'],
    linux: ['install.sh', 'sudo sh install.sh'],
    flatpak: ['chat.dens.Dens.flatpakref', 'flatpak uninstall'],
    android: ['dens.apk', 'Allow from this source'],
};
const PAGES = {
    windows: '/docs/install/windows/',
    linux: '/docs/install/linux/',
    flatpak: '/docs/install/flatpak/',
    android: '/docs/install/android/',
};
// The guides the app links to: components.jsx's DOCS and each DocsTip's and
// DocsLink's page, and the installer's and restore's links in
// internal/install, as this tree and earlier releases carry them: a page
// that moved answers at its old path with a redirect that keeps the anchor
// (layouts/alias.html), which the check follows. A released Dens's link to
// the Flatpak's old section is checked on its own.
const LINKED = [
    '/docs/', '/docs/install/', '/docs/install/flatpak/', '/docs/hosting/', '/docs/hosting/#on-a-server', '/docs/backups/',
    '/docs/backups/#moving-a-den', '/docs/usage/#your-passwords-and-keys', '/docs/usage/#comparing-codes',
    '/docs/usage/#signing-in-from-another-computer', '/docs/usage/#group-dms', '/docs/usage/#disappearing-messages',
    '/docs/host/', '/docs/server/', '/docs/join/#your-passwords-and-keys', '/docs/join/#comparing-codes',
    '/docs/join/#signing-in-from-another-computer', '/docs/join/#group-dms', '/docs/join/#disappearing-messages',
];
// The guides besides the install guide, whose links the check follows too.
const GUIDES = ['/docs/', '/docs/usage/', '/docs/hosting/', '/docs/backups/', '/docs/privacy/'];

// browser launches a mode: a phone's 390 by 844, by touch, or a desktop's
// 1400 by 900, with a mouse.
async function browser(mode, port) {
    const name = mode.startsWith('chromium') ? 'chromium' : 'firefox';
    if (mode.endsWith('-desktop')) return launch(name, port, { size: [1400, 900], hover: true });
    const size = [390, 844];
    const prefs = name === 'firefox' ? { 'ui.primaryPointerCapabilities': 1, 'ui.allPointerCapabilities': 1, 'dom.w3c_touch_events.enabled': 1 } : {};
    const b = await launch(name, port, { size, prefs });
    if (name === 'chromium') {
        await b.cdp('Emulation.setDeviceMetricsOverride', { width: size[0], height: size[1], deviceScaleFactor: 2, mobile: true });
        await b.touch(true);
    } else {
        await b.bidi('browsingContext.setViewport', { context: b.context, viewport: { width: size[0], height: size[1] } });
    }
    return b;
}

// LINKS checks the links it's given, or every link on the page into the
// site: each reaches a page, through a moved page's redirect, and the page
// holds the link's anchor.
const LINKS = (list) => `(async () => {
    const urls = ${list ? JSON.stringify(list) + '.map((p) => location.origin + p)'
        : `[...new Set([...document.querySelectorAll('a[href]')].map((a) => a.href).filter((u) => u.startsWith(location.origin + '/')))]`};
    const pages = new Map(), bad = [];
    const load = async (page) => {
        const r = await fetch(page);
        if (!r.ok) return { status: r.status };
        const doc = new DOMParser().parseFromString(await r.text(), 'text/html');
        const refresh = doc.querySelector('meta[http-equiv="refresh"]')?.content.match(/url=(.*)$/i);
        return refresh ? load(new URL(refresh[1], page).href) : { doc };
    };
    for (const u of urls) {
        const url = new URL(u), page = url.origin + url.pathname;
        if (!pages.has(page)) {
            const r = await load(page);
            pages.set(page, r.doc || null);
            if (!r.doc) bad.push(url.pathname + ': ' + r.status);
        }
        const doc = pages.get(page);
        if (doc && url.hash.length > 1 && !doc.getElementById(decodeURIComponent(url.hash.slice(1)))) bad.push(url.pathname + url.hash + ': no such anchor');
    }
    return { count: urls.length, bad };
})()`;

const CONTENT = `document.getElementById('content')`;
const FOLDS = `[...document.querySelectorAll('#content details')]`;

// walk takes mode's browser through the guide.
async function walk(mode, b, h) {
    // A page's screens, a screen at a time, as a reader scrolls it.
    const screens = async (name) => {
        await h.ev(`document.documentElement.style.scrollBehavior = 'auto', scrollTo(0, 0), true`);
        await h.snap(name, { scroll: 'document.scrollingElement', max: 12 });
    };
    const links = async (where, list) => {
        const r = await h.ev(LINKS(list));
        report(r.count > 0 && !r.bad.length, `${mode}: ${where}: ${r.count} links reach their pages and anchors${r.bad.length ? `, but ${r.bad.join('; ')}` : ''}`);
    };

    await h.step('choice', async () => {
        await h.go('/docs/install/');
        await h.wait(`document.querySelectorAll('#content .hextra-card').length`, 'the choice');
        const cards = await h.ev(`[...document.querySelectorAll('#content .hextra-card')].map((c) => [c.querySelector('.hextra-card-icon').textContent.trim(), new URL(c.href).pathname])`);
        report(JSON.stringify(cards) === JSON.stringify(CARDS),
            `${mode}: the choice's cards are ${CARDS.map(([t]) => t).join(', ')}, each to its page: ${JSON.stringify(cards)}`);
        await screens('choice');
        await links('the choice');
    });

    for (const [i, [title, path]] of CARDS.entries()) {
        const page = Object.keys(PAGES).find((k) => PAGES[k] === path);
        await h.step(page, async () => {
            await h.go('/docs/install/');
            await h.wait(`document.querySelectorAll('#content .hextra-card').length === 4`, 'the choice');
            await h.ev(`document.querySelectorAll('#content .hextra-card')[${i}].click(), true`);
            await waitFor(() => h.ev(`location.pathname === ${JSON.stringify(path)} && document.readyState === 'complete'`), `${title}'s page`);
            await sleep(500);
            const text = (await h.ev(`${CONTENT}.textContent`)).replace(/\s+/g, ' ');
            const own = MARKS[page].filter((m) => text.includes(m));
            const others = Object.entries(MARKS).filter(([k]) => k !== page).flatMap(([, m]) => m).filter((m) => text.includes(m));
            report(own.length === MARKS[page].length && !others.length,
                `${mode}: ${title}'s card opens its page, with its own steps (${own.join(', ')}) and none of another's${others.length ? `, but ${others.join(', ')}` : ''}`);
            const folds = await h.ev(`${FOLDS}.map((d) => d.open)`);
            report(folds.length > 0 && folds.every((open) => !open), `${mode}: ${title}'s ${folds.length} folds start closed`);
            await screens(page);
            // A fold opens from its title, as a reader opens it.
            await h.ev(`${FOLDS}[0].querySelector('summary').click(), true`);
            report(await h.ev(`${FOLDS}[0].open`), `${mode}: ${title}'s first fold opens from its title`);
            await h.ev(`${FOLDS}.forEach((d) => { d.open = true; }), true`);
            await screens(`${page}-open`);
            await links(`${title}'s page`);
        });
    }

    await h.step('old-flatpak-link', async () => {
        await h.go('/docs/install/#the-flatpak');
        await sleep(500);
        const r = await h.ev(`(() => {
            const card = [...document.querySelectorAll('#content .hextra-card')].find((c) => c.textContent.includes('Flatpak'));
            const box = card?.getBoundingClientRect();
            return { path: location.pathname, inView: !!box && box.top >= 0 && box.bottom <= innerHeight };
        })()`);
        report(r.path === '/docs/install/' && r.inView, `${mode}: a released Dens's link to the Flatpak lands on the choice, the Flatpak's card in view: ${JSON.stringify(r)}`);
        await h.snap('old-flatpak-link');
    });

    // Android's page was /docs/android/ until it joined the install guide.
    await h.step('old-android-link', async () => {
        await h.go('/docs/android/');
        await waitFor(() => h.ev(`location.pathname === '/docs/install/android/' && document.readyState === 'complete'`), "Android's page");
        report(true, `${mode}: Android's old page leads to its new one`);
    });

    await h.step('linked', async () => {
        await h.go('/docs/');
        await links('the app\'s links into the guides', LINKED);
    });

    // The other guides' links, which the install guide's moves could break.
    for (const path of GUIDES) {
        await h.step(`links${path}`, async () => {
            await h.go(path);
            await links(path);
        });
    }
}

let port = 9530;
for (const mode of modes.length ? modes : ['chromium', 'firefox', 'chromium-desktop', 'firefox-desktop']) {
    const out = `${HOME}/shot-site/${mode}`;
    fs.rmSync(out, { recursive: true, force: true });
    fs.mkdirSync(out, { recursive: true });
    const b = await browser(mode, port++);
    try {
        const h = helpers(b, SITE, out);
        await walk(mode, b, h);
        fs.writeFileSync(`${out}/audits.json`, JSON.stringify(h.log, null, 1));
        const touch = !mode.endsWith('-desktop');
        audits(mode, h.log, report, touch);
        const sideways = h.log.filter((s) => s.audit?.sideways).map((s) => s.name);
        report(!sideways.length, `${mode}: no page scrolls sideways${sideways.length ? `, but ${sideways.join(', ')} do` : ''}`);
    } catch (e) {
        fail(e);
    } finally {
        await b.close();
    }
}
finish();
