// The M8 check of call answers: Bob, in the browser named, joins Lounge,
// and carol, the voice probe, joins and leaves, so his browser answers the
// den's offers again once it has gathered, when a browser names its
// address in an answer's connection lines. The den is behind
// den-proxy.mjs, which logs every answer that reaches it: none may name an
// address, though the browser's own did. Firefox names its host
// candidates, as it does once a page may use the microphone, rather than
// mDNS names.
//
//   run.sh answers chromium|firefox
import { spawn } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import fs from 'node:fs';
import { BOB, CAROL, HOME, checks, launch, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, LOUNGE: lounge } = process.env;
const c = checks();
const PRELOAD = `() => {
    const set = RTCPeerConnection.prototype.setLocalDescription;
    window.__answers = [];
    RTCPeerConnection.prototype.setLocalDescription = async function (...a) {
        const r = await set.apply(this, a);
        if (this.localDescription?.type === 'answer') window.__answers.push(this.localDescription.sdp);
        return r;
    };
}`;

// addresses lists what an SDP's connection, RTCP, origin and candidate
// lines name, but the unspecified addresses.
function addresses(sdp) {
    const out = [];
    for (const line of sdp.split('\r\n')) {
        const f = line.split(' ');
        if (line.startsWith('c=') || line.startsWith('o=')) out.push(f.at(-1));
        else if (line.startsWith('a=rtcp:') && f.length === 4) out.push(f[3]);
        else if (line.startsWith('a=candidate:')) out.push(f[4]);
    }
    return out.filter((a) => a !== '0.0.0.0' && a !== '::');
}

const b = await launch(which, 9611, { media: which === 'firefox' ? 'pulse' : true, preload: PRELOAD,
    prefs: { 'media.peerconnection.ice.obfuscate_host_addresses': false } });
let probe;
try {
    await pair(b, BOB);
    await b.go(`http://localhost:${BOB.port}/den/${den}/${lounge}`);
    await waitFor(() => b.eval(`!!document.querySelector('button[title^="Join the call"]')`), 'the voice channel');
    await b.eval(`document.querySelector('button[title^="Join the call"]').click(), true`, true);
    await waitFor(() => b.eval(`[...document.querySelectorAll('[role=status]')].some((e) => e.textContent.includes('Connected'))`), 'bob in the call', 30000);
    const bob = await b.eval(`fetch('/api/dens/${den}/state').then((r) => r.json()).then((s) => s.me.id)`);

    // Carol joins, hears bob, and leaves: two offers more for him.
    const cookie = fs.readFileSync(`${HOME}/jar-${CAROL.instance}`, 'utf8').split('\n').map((l) => l.split('\t')).find((f) => f[5]?.startsWith('dens_session_'));
    let out = '';
    probe = spawn(`${HOME}/voice-probe`, ['-test.run', '^TestVoiceProbe$', '-test.v', '-test.count=1'], { env: { ...process.env,
        DENS_VOICE_PROBE: `http://127.0.0.1:${CAROL.port}`, DENS_PROBE_COOKIE: `${cookie[5]}=${cookie[6]}`, DENS_PROBE_DEN: den,
        DENS_PROBE_CHANNEL: lounge, DENS_PROBE_HEAR: bob, DENS_PROBE_NETWORK: 'udp', DENS_PROBE_STAY: '3' } });
    probe.stdout.on('data', (d) => (out += d));
    probe.stderr.on('data', (d) => (out += d));
    const code = await new Promise((resolve) => probe.on('exit', resolve));
    c.report(code === 0, `carol joined, heard bob and left${code === 0 ? '' : `: ${out.slice(-400)}`}`);
    await waitFor(async () => (await b.eval('window.__answers.length')) >= 3, "bob's browser answering again after carol left", 15000);
    await sleep(1000);

    // What the browser made: its connection lines name its address once
    // it has gathered.
    const own = await b.eval('window.__answers');
    const named = own.filter((sdp) => sdp.split('\r\n').some((l) => l.startsWith('c=') && addresses(l).length > 0));
    c.report(named.length > 0, `${which} named its address in the connection lines of ${named.length} of its ${own.length} answers`);
    const reached = fs.readFileSync(`${HOME}/answers.jsonl`, 'utf8').trim().split('\n').filter(Boolean).map((l) => JSON.parse(l).sdp);
    const leaked = reached.filter((sdp) => addresses(sdp).length > 0);
    c.report(reached.length >= own.length && leaked.length === 0,
        `${reached.length} answers reached the den, ${leaked.length} naming an address${leaked.length ? `: ${JSON.stringify(leaked.map(addresses))}` : ''}`);
    const lines = [...new Set(reached.flatMap((sdp) => sdp.split('\r\n').filter((l) => l.startsWith('c=') || l.startsWith('a=rtcp:'))))];
    console.log(`     the c= and a=rtcp: lines the den got: ${lines.join(' | ')}`);
} catch (e) {
    c.fail(e);
} finally {
    probe?.kill();
    await b.close();
    c.finish();
}
