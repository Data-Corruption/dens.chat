// Desk in the Android walk's call (M13.4): Chromium in the browser checks'
// container as the den's owner, its fake microphone speaking, in Lounge,
// answering the walk in the Android container over HTTP, on PORT at this
// container's address, with what desk's page shows of the call:
//
//   GET  /call   who is in it, by name, and whether each rings or is muted
//   POST /share  shares desk's screen, its fake capture
//   POST /quit   hangs up, and ends
//
//   node desk.mjs PORT [SECONDS]
//
// in dens-browsers' browser directory, beside browsers.mjs. It ends by
// itself after SECONDS, 600 unless given.
import http from 'node:http';
import { launch, token, waitFor } from './browsers.mjs';

const [, , portArg, secondsArg] = process.argv;
const port = Number(portArg);
const page = await launch('chromium', 9446, { media: true, screen: true });
const quit = async (status) => {
    await page.close();
    process.exit(status);
};
setTimeout(() => quit(0), Number(secondsArg || 600) * 1000).unref();

try {
    await page.go(`http://127.0.0.1:18584/#token=${token('sden')}`);
    await waitFor(() => page.eval(`!!document.querySelector('a[href="/settings"]')`), 'desk paired');
    await page.eval(`(async () => {
        const den = (await (await fetch('/api/dens')).json()).dens[0].den_id;
        history.pushState(null, '', '/den/' + den);
        dispatchEvent(new PopStateEvent('popstate'));
        return true;
    })()`);
    await waitFor(() => page.eval(`!!document.querySelector('button[title^="Join the call"]')`), "desk's Lounge");
    await page.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    await waitFor(() => page.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), 'desk in the call', 30000);
} catch (e) {
    console.error(e.message);
    await quit(1);
}

// CALL is the call as desk's list shows it: each member's name, whether
// they ring, as for push to talk held, and whether they're muted, by marks
// any of Dens's pages since M11 make, as desk's den, made once, may still
// serve an older one.
const CALL = `[...document.querySelectorAll('ul[aria-label="In the call"] li')].map((li) => ({
    name: li.querySelector('button span.truncate')?.textContent.trim() || '',
    rings: li.textContent.includes('speaking'),
    muted: !!li.querySelector('[aria-label="muted"]'),
}))`;
const SHARE = `(() => { const b = document.querySelector('button[aria-label="Share your screen"]'); if (!b) return 'missing';
    if (b.disabled) return 'disabled: ' + b.title; b.click(); return 'clicked'; })()`;
// SHAREABLE is desk's share button allowing a share, as it doesn't until
// desk's page knows the den's limits; the click itself goes as from a
// click of desk's own, which the browser's picker needs.
const SHAREABLE = `(() => { const b = document.querySelector('button[aria-label="Share your screen"]'); return !!b && !b.disabled; })()`;

http.createServer(async (req, res) => {
    const answer = (status, body) => {
        res.writeHead(status, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(body));
    };
    try {
        if (req.method === 'GET' && req.url === '/call') return answer(200, { members: await page.eval(CALL) });
        if (req.method === 'POST' && req.url === '/share') {
            await waitFor(() => page.eval(SHAREABLE), "desk's share button", 15000).catch(() => {});
            return answer(200, { share: await page.eval(SHARE, true) });
        }
        if (req.method === 'POST' && req.url === '/quit') {
            answer(200, { left: await page.eval(`(() => { const b = document.querySelector('button[aria-label="Leave the call"]'); b?.click(); return !!b; })()`) });
            return setTimeout(() => quit(0), 500);
        }
        answer(404, { error: 'no such thing' });
    } catch (e) {
        answer(500, { error: e.message });
    }
}).listen(port, '0.0.0.0', () => console.log(`desk in the call, answering on ${port}`));
