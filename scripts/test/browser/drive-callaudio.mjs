// The M12.2 check: alice in the first browser named and bob in the second,
// in a call, each page playing it as Android's app has it play: its call's
// audio contexts made once the microphone is open, and the voices through
// the call's context, as the page does where the service says audio may
// start without a click. Each hears the other there, at the level chosen
// for them, under the call's volume from the settings, and not at all
// while deafened. A page Chromium froze plays
// its call again as it thaws, and so does a page that hears resume with
// its elements paused, in either browser; and the call's notes in the log
// say what each page sent a second, what its encoder aimed at, and how far
// its voice processor ran behind.
//
//   run.sh callaudio chromium|firefox chromium|firefox
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, checks, launch, openSettings, pair, waitFor } from './browsers.mjs';

const [, , aName, bName] = process.argv;
const { DEN: den } = process.env;
// What each page asks of the browser, in order, and what each of its audio
// contexts plays, heard through an analyser as its speakers would hear it.
const PRELOAD = `() => {
    if (window.__acs) return;
    window.__order = [];
    window.__acs = [];
    window.__resumed = 0;
    document.addEventListener('resume', () => window.__resumed++);
    const AC = window.AudioContext;
    window.AudioContext = class extends AC {
        constructor(...a) {
            super(...a);
            this.__out = this.createAnalyser();
            window.__acs.push(this);
            window.__order.push('context');
        }
    };
    const connect = AudioNode.prototype.connect;
    AudioNode.prototype.connect = function (to, ...rest) {
        const r = connect.call(this, to, ...rest);
        if (to === this.context?.destination && this.context.__out) connect.call(this, this.context.__out);
        return r;
    };
    const md = navigator.mediaDevices;
    const gum = md.getUserMedia.bind(md);
    md.getUserMedia = (c) => gum(c).then((s) => (window.__order.push('microphone'), s));
    const Au = window.Audio;
    window.__audios = [];
    window.Audio = function (...a) { const el = new Au(...a); window.__audios.push(el); return el; };
}`;
// LOUDEST is the loudest the page's contexts played over two seconds, in
// dBFS.
const LOUDEST = `(async () => {
    let most = -120;
    for (let i = 0; i < 20; i++) {
        for (const c of window.__acs.filter((x) => x.state === 'running')) {
            const b = new Float32Array(c.__out.fftSize);
            c.__out.getFloatTimeDomainData(b);
            let s = 0;
            for (const x of b) s += x * x;
            most = Math.max(most, 10 * Math.log10(Math.max(s / b.length, 1e-12)));
        }
        await new Promise((r) => setTimeout(r, 100));
    }
    return Math.round(most);
})()`;
const ELEMENTS = `JSON.stringify(window.__audios.filter((a) => a.srcObject).map((a) => (a.paused ? 'paused' : a.muted ? 'muted' : 'playing')))`;
const button = (label) => `[...document.querySelectorAll('button')].find((x) => x.getAttribute('aria-label') === ${JSON.stringify(label)})`;
const sides = [
    { ...ALICE, browser: aName, debug: 9333, other: 'Bob' },
    { ...BOB, browser: bName, debug: 9334, other: 'Alice' },
];
const [A, B] = sides;
const c = checks();
const report = c.report;
const opened = [];
try {
    for (const s of sides) {
        s.b = await launch(s.browser, s.debug, { media: s.browser === 'firefox' ? 'pulse' : true, preload: PRELOAD });
        opened.push(s.b);
        await pair(s.b, s);
        await s.b.go(`http://localhost:${s.port}/den/${den}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('button[title^="Join the call"]')`), `${s.name}'s voice channel`);
        // As the service says to the app's page (M12).
        await s.b.eval(`(document.getElementById('app').dataset.autoplay = 'true', true)`);
        await s.b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    }
    for (const s of sides) await waitFor(() => s.b.eval(`document.body.innerText.includes('Connected')`), `${s.name} in the call`, 30000);
    await sleep(3000);

    for (const s of sides) {
        const order = await s.b.eval(`JSON.stringify(window.__order)`);
        const contexts = await s.b.eval(`JSON.stringify(window.__acs.map((x) => x.state + ' ' + x.sampleRate))`);
        const o = JSON.parse(order);
        report(o[0] === 'microphone' && o.filter((x) => x === 'context').length === 2 && JSON.parse(contexts).every((x) => x.startsWith('running')),
            `${s.name} in ${s.browser} made the call's contexts once the microphone was open: ${order}, ${contexts}`);
        const heard = await waitFor(async () => {
            const db = await s.b.eval(LOUDEST);
            return db > -50 ? db : null;
        }, `${s.name} to hear ${s.other} through the call's context`, 20000).catch((e) => e.message);
        const els = await s.b.eval(ELEMENTS);
        report(typeof heard === 'number' && els === '["muted"]', `${s.name} hears ${s.other} through the call's context at ${heard} dBFS, ${s.other}'s element muted and playing: ${els}`);
    }

    // Bob turns Alice down to nothing and back, and deafens: her voice in
    // his call's context follows.
    const slide = (pct) => B.b.eval(`(() => {
        if (!document.querySelector('input[aria-label="Volume for Alice"]')) document.querySelector('button[aria-label="Options for Alice in the call"]').click();
        return new Promise((resolve) => setTimeout(() => {
            const input = document.querySelector('input[aria-label="Volume for Alice"]');
            if (!input) return resolve(false);
            input.value = '${pct}';
            input.dispatchEvent(new Event('input', { bubbles: true }));
            resolve(true);
        }, 200));
    })()`);
    await slide(0);
    await sleep(500);
    const down = await B.b.eval(LOUDEST);
    await slide(100);
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    await sleep(500);
    const up = await B.b.eval(LOUDEST);
    await B.b.eval(`${button('Deafen')}.click(), true`);
    await sleep(500);
    const deaf = await B.b.eval(LOUDEST);
    await B.b.eval(`${button('Undeafen')}.click(), true`);
    await B.b.eval(`${button('Unmute')}?.click(), true`);
    await sleep(500);
    const hearing = await B.b.eval(LOUDEST);
    report(down < -90 && up > -50 && deaf < -90 && hearing > -50,
        `${B.browser}'s Alice at 0% plays ${down} dBFS, at 100% ${up}, deafened ${deaf}, and hearing again ${hearing}`);

    // The call's volume in the settings turns everyone down at once, and
    // the browser keeps it.
    await openSettings(B.b, 'Voice & video');
    const loudness = async (pct) => {
        await B.b.eval(`(() => {
            const r = document.querySelector('input[aria-label="Call volume"]');
            r.value = '${pct}';
            r.dispatchEvent(new Event('input', { bubbles: true }));
            return true;
        })()`);
        await sleep(300);
        return B.b.eval(LOUDEST);
    };
    const whole = await loudness(100);
    const half = await loudness(50);
    const kept = await B.b.eval(`localStorage.getItem('DENS_CALL_VOLUME')`);
    const none = await loudness(0);
    const again = await loudness(100);
    await B.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    report(half < whole - 3 && kept === '0.5' && none < -90 && again > -50,
        `${B.browser}'s call volume plays Alice at ${whole} dBFS at 100%, ${half} at 50% (kept ${kept}), ${none} at 0%, and ${again} at 100% again`);

    // A page whose elements a freeze paused plays them again as it hears
    // resume; Chromium freezes and thaws a page itself too.
    for (const s of sides) {
        await s.b.eval(`(window.__audios.forEach((a) => a.pause()), true)`);
        await s.b.eval(`(document.dispatchEvent(new Event('resume')), true)`);
        await sleep(500);
        const els = await s.b.eval(ELEMENTS);
        let thawed = 'not frozen: Firefox freezes no page';
        if (s.browser === 'chromium') {
            await s.b.cdp('Page.setWebLifecycleState', { state: 'frozen' });
            await sleep(3000);
            await s.b.cdp('Page.setWebLifecycleState', { state: 'active' });
            await sleep(1000);
            thawed = `frozen and thawed, it heard resume ${await s.b.eval('window.__resumed')} times, its elements ${await s.b.eval(ELEMENTS)}`;
        }
        const db = await s.b.eval(LOUDEST);
        report(els === '["muted"]' && db > -50 && (s.browser !== 'chromium' || thawed.includes('["muted"]')),
            `${s.name} in ${s.browser} played its call again on resume: ${els}, ${thawed}, hearing ${s.other} at ${db} dBFS`);
    }

    // The notes summing the call up every 10 s say what each page sent a
    // second, what its encoder aimed at, where its browser says, and how
    // far its voice processor ran behind: as far as Chromium froze it, which
    // rendered nothing the while, and hardly at all in Firefox.
    await sleep(11000);
    for (const s of sides) {
        const logs = await s.b.eval(`fetch('/api/logs').then((r) => r.text())`);
        const stats = logs.split('\n').filter((l) => l.includes('; sending ')).at(-1) || '';
        const rate = Number(stats.match(/sending (\d+) kbit\/s/)?.[1] ?? -1);
        const aim = stats.match(/of the (\d+) its encoder aims at/)?.[1];
        const behind = Number(stats.match(/its voice processor (\d+) ms behind/)?.[1] ?? -1);
        const froze = logs.includes('its browser froze it');
        const thawed = logs.includes('its browser thawed it, and it plays the call again');
        const chromium = s.browser === 'chromium';
        report(rate > 8 && (chromium ? behind > 2500 && behind < 3500 && aim === '96' && froze && thawed : behind >= 0 && behind < 250 && !aim && thawed),
            `${s.name}'s notes: sending ${rate} kbit/s, aiming at ${aim ?? 'nothing said'}, ${behind} ms behind, froze ${froze}, thawed ${thawed}`);
    }
} catch (e) {
    c.fail(e);
} finally {
    for (const b of opened) await b.close().catch(() => {});
}
c.finish();
