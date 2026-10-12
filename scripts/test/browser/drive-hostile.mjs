// The M8 check of hostile text: a channel of 200 messages, each 4,000
// characters of markdown made to be slow to parse, as any member could
// send. Bob, in the browser named, scrolls up through it while alice, in
// the other, types in it, and his page takes her typing notices without
// stalling: no frame of his waits long on the page's work.
//
//   run.sh hostile chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, checks, launch, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, HOSTILE: hostile } = process.env;
const c = checks();
const LIST = `document.querySelector('[data-id]')?.closest('.overflow-y-auto')`;
// Frames: the gap before each, from requestAnimationFrame.
const MONITOR = `(() => {
    window.__gaps = [];
    let last = performance.now();
    const tick = (t) => { window.__gaps.push(t - last); last = t; if (!window.__stop) requestAnimationFrame(tick); };
    requestAnimationFrame(tick);
    return true;
})()`;

const bob = await launch(which, 9621);
const alice = await launch(which === 'chromium' ? 'firefox' : 'chromium', 9622);
try {
    await pair(bob, BOB);
    await pair(alice, ALICE);
    const start = Date.now();
    await bob.go(`http://localhost:${BOB.port}/den/${den}/${hostile}`);
    await waitFor(async () => (await bob.eval(`document.querySelectorAll('[data-id]').length`)) > 5, "bob's channel", 60000);
    c.report(true, `bob's page shows the channel, ${Date.now() - start} ms after it opened`);
    await alice.go(`http://localhost:${ALICE.port}/den/${den}/${hostile}`);
    await waitFor(() => alice.eval(`!!document.querySelector('textarea')`), "alice's composer");
    await alice.eval(`document.querySelector('textarea').focus(), true`);

    await bob.eval(MONITOR);
    // The list holds a window of the channel, so how far it reached shows
    // in the oldest message it holds.
    const OLDEST = `[...document.querySelectorAll('[data-id]')].map((r) => r.dataset.id).sort((a, b) => a.length - b.length || (a < b ? -1 : 1))[0]`;
    const oldest0 = await bob.eval(OLDEST);
    let typing = false;
    for (let i = 0; i < 80; i++) {
        await bob.eval(`(() => { const l = ${LIST}; l.scrollTop = Math.max(0, l.scrollTop - 900); return true; })()`);
        if (i % 6 === 0) await alice.type('x');
        typing ||= await bob.eval(`document.body.innerText.includes('Alice is typing')`);
        await sleep(150);
    }
    await bob.eval(`window.__stop = true`);
    const gaps = await bob.eval(`window.__gaps`);
    const oldest = await bob.eval(OLDEST);
    const slow = gaps.filter((g) => g > 200).length;
    const worst = Math.round(Math.max(...gaps));
    c.report(typing, `alice's typing notices reached bob's page as he scrolled`);
    c.report(BigInt(oldest) < BigInt(oldest0), `scrolling up loaded older messages: the oldest shown went from ${oldest0} to ${oldest}`);
    c.report(gaps.length > 100 && worst < 400,
        `bob's page didn't stall: ${gaps.length} frames, the longest ${worst} ms, ${slow} over 200 ms`);
    await bob.shot(`${HOME}/shot-hostile-${which}.png`);
} catch (e) {
    c.fail(e);
    await bob.shot(`${HOME}/shot-hostile-${which}-failed.png`).catch(() => {});
} finally {
    await bob.close();
    await alice.close();
    c.finish();
}
