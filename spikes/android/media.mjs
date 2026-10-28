// Photos and videos through the page as a member sends them: each file,
// from the device's filesystem, attached to the composer of the den's
// general channel, timed until its chip settles on what goes (the smaller
// copy and its size, or the reason there's none). Runs in the spike's
// container, beside drive.mjs.
//
//   node media.mjs SERIAL FILE...
//   node media.mjs SERIAL watch    the chips as the member attaches files by hand
import { setTimeout as sleep } from 'node:timers/promises';
import { webview } from './drive.mjs';

const [, , serial, ...files] = process.argv;
const chips = `[...document.querySelectorAll('ul[aria-label="Files to send"] li')].map((li) => li.innerText.replace(/\\s+/g, ' ').trim())`;
if (files[0] === 'watch') {
    // A line with the time whenever the chips change. The page closing, or
    // a reply that never comes as the WebView goes, starts it looking again.
    let last = '';
    for (;;) {
        let w;
        try {
            w = await webview(serial);
            for (;;) {
                const c = JSON.stringify(await Promise.race([w.eval(chips), sleep(10000).then(() => { throw new Error('no reply'); })]));
                if (c !== last) console.log(new Date().toISOString(), c);
                last = c;
                await sleep(1000);
            }
        } catch {
            w?.close();
            await sleep(5000);
        }
    }
}
const b = await webview(serial);
const general = await b.eval(`(async () => {
    const den = (await (await fetch('/api/dens')).json()).dens[0].den_id;
    const state = await (await fetch('/api/dens/' + den + '/state')).json();
    return '/den/' + den + '/' + state.channels.find((c) => c.name === 'general').id;
})()`);
await b.eval(`history.pushState(null, '', ${JSON.stringify(general)}), dispatchEvent(new PopStateEvent('popstate')), true`);
await sleep(2000);
for (const file of files) {
    const start = Date.now();
    await b.attach('input[type=file][multiple]', [file]);
    let last = '';
    let steady = 0;
    for (;;) {
        await sleep(1000);
        const c = (await b.eval(chips)).at(-1) || '';
        if (c !== last) {
            console.log(`${((Date.now() - start) / 1000).toFixed(0)}s ${file.split('/').pop()}: ${c}`);
            last = c;
            steady = 0;
        } else if (++steady >= 8 || Date.now() - start > 15 * 60 * 1000) {
            break;
        }
    }
    // Removed again, so the next goes alone.
    await b.eval(`[...document.querySelectorAll('ul[aria-label="Files to send"] li button')].at(-1)?.click(), true`);
    await sleep(500);
}
b.close();
