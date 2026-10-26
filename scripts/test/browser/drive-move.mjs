// The M10 check: alice moves her den to another computer from the
// settings. Move asks her local password, turns hosting off and downloads
// the den's export; Hosting then says when, and her den's row says so and
// leads there. Turning hosting on again says first that another computer
// may host the den by now. Removing her copy is refused while nothing else
// answers as the den. Once carol's instance, standing in for a server,
// restores the export and hosts it from the command line, with the den's
// address forwarded to it, bob reconnects by himself, his next message
// gets an ID past every one the den gave out (M11), and alice's copy
// goes: her den's row is connected at its address, as its owner, and no
// longer hosted here. Once the server is gone for good, Leave den, which
// can't reach the den, removes it from her computer instead (M10).
//
//   run.sh move chromium|firefox
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import { ALICE, HOME, MANAGE, SETTINGS, checks, launch, lib, openManage, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const c = checks();
const ok = (what) => c.report(true, what);
const step = (what) => console.error(`.. ${what}`);
const hosting = () => JSON.parse(lib('api', 'alice', 'GET', '/api/hosting'));
const DOWNLOADS = `${HOME}/downloads`;
const ADDRESS = 'http://127.0.0.1:18490';

step(`launching ${which}`);
const b = await launch(which, 9332, { downloads: DOWNLOADS });
const shot = (name) => b.shot(`${HOME}/shot-move-${which}-${name}.png`);
const card = `[...${SETTINGS}.querySelectorAll('section')].find((s) => s.querySelector('#hosting'))`;
const toggle = `document.getElementById('hosting')`;
const button = (scope, text) => `[...${scope}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(text)})`;
const text = (scope) => b.eval(`${scope}.innerText.replace(/\\s+/g, ' ')`);
const closeSettings = () => b.eval(`${SETTINGS}.querySelector('button[aria-label=Close]').click(), true`);
const row = `[...document.querySelectorAll('li')].find((l) => l.innerText.includes('Moving Den'))`;
const said = async (pattern) => (await text(card)).match(pattern)?.[0];

// password types the local password into the card's form, and submits it
// with the button named.
async function password(submit) {
    await waitFor(() => b.eval(`!!${card}.querySelector('input[type=password]')`), 'the password field');
    await b.eval(`(() => {
        const input = ${card}.querySelector('input[type=password]');
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, 'local password');
        input.dispatchEvent(new Event('input', { bubbles: true }));
        return true;
    })()`);
    await b.eval(`${button(card, submit)}.click(), true`);
}

// dens runs the development binary's command line, as the server's desktop
// user would there.
const dens = (args, input) => execFileSync(`${HOME}/dens-dev`, args, { input }).toString();

try {
    step('pairing');
    await pair(b, ALICE);
    await waitFor(() => b.eval(`!!${row} && ${row}.innerText.includes('Hosted here') && !/Offline|Connecting/.test(${row}.innerText)`),
        'her den on home, connected');

    step('moving the den');
    await openSettings(b, 'System');
    await waitFor(() => b.eval(`!!${card} && !!${button(card, 'Move the den to another computer')}`), "Hosting's Move");
    await b.eval(`${button(card, 'Move the den to another computer')}.click(), true`);
    await waitFor(async () => /About \d/.test(await text(card)), "the export's size");
    const move = await text(card);
    if (!move.includes("This turns hosting off: members can't reach Moving Den until the other computer hosts it.") || !move.includes('not any of your client data')) {
        throw new Error(`Move says: ${move}`);
    }
    ok(`Move says: ${await said(/This turns hosting off.*?client data\./)}`);
    await shot('move');
    await password("Download the den's export");
    const file = await waitFor(() => fs.readdirSync(DOWNLOADS).find((f) => /^dens-m2a-den-\d{8}-\d{6}\.backup$/.test(f)), 'the export downloaded', 30000);
    await waitFor(() => !fs.readdirSync(DOWNLOADS).some((f) => /\.(part|crdownload)$/.test(f)), 'the download to finish', 30000);
    const exported = hosting();
    if (exported.enabled || exported.serving || !exported.exported) throw new Error(`hosting after the move: ${JSON.stringify(exported)}`);
    await waitFor(async () => (await text(card)).includes('You exported Moving Den on'), "the card's line on the export");
    ok(`hosting is off, the export downloaded as ${file}, and the card says: ${await said(/You exported.*?member's does\./)}`);
    await shot('exported');

    step('the row leads to Hosting');
    await closeSettings();
    await waitFor(() => b.eval(`!!${row} && ${row}.innerText.includes('Exported')`), "the den's row saying it was exported");
    ok(`the den's row says: ${(await b.eval(`${row}.innerText.replace(/\\s+/g, ' ')`)).match(/You exported it on.*?this one\./)?.[0]}`);
    await shot('home-exported');
    await b.eval(`${button(row, 'Open Hosting')}.click(), true`);
    await waitFor(() => b.eval(`!!${card}`), 'the hosting card, from the row');
    ok("the row's link opens Hosting");

    step('turning hosting on again asks first');
    await b.eval(`${toggle}.click(), true`);
    await waitFor(async () => (await text(card)).includes('Another computer may host it by now'), 'the warning');
    ok(`turning hosting on says first: ${await said(/You exported Moving Den on.*?points at\./)}`);
    await shot('on-again');
    await b.eval(`${button(card, 'Cancel')}.click(), true`);

    step('removing waits for another computer');
    await waitFor(() => b.eval(`!!${button(card, 'Remove from this computer')}`), 'Remove');
    await b.eval(`${button(card, 'Remove from this computer')}.click(), true`);
    await password('Remove from this computer');
    await waitFor(async () => (await text(card)).includes(`doesn't answer at ${ADDRESS} from this computer`), 'the refusal');
    if (!hosting().den) throw new Error('the refused removal took the den');
    ok(`with nothing else answering as the den, removing is refused: ${await said(/Moving Den doesn't answer.*?forwards there\./)}`);
    await shot('refused');

    step('the server hosts the den');
    const restoredAt = Date.now();
    dens(['-i', 'm2c', 'restore', `${DOWNLOADS}/${file}`, '--password-stdin', '--yes'], 'local password\n');
    lib('start', 'm2c', '--port', '18487', '--den-port', '18488', '--media-udp-port', '17883', '--media-tcp-port', '17884');
    const on = dens(['-i', 'm2c', 'den', 'hosting', 'on', '--password-stdin', '--yes'], 'local password\n');
    if (!on.includes(`Moving Den serves at ${ADDRESS}`)) throw new Error(`hosting on the server said: ${on}`);
    fs.writeFileSync(`${HOME}/forward-to`, '18488\n');
    await waitFor(() => JSON.parse(lib('api', 'bob', 'GET', '/api/dens')).dens[0]?.state === 'connected', 'bob back, on the server', 60000);
    ok("carol's instance restored the export and hosts the den, and bob is back by himself");
    // The restore moved the den's counters on to its time in microseconds,
    // past every ID the den gave out (M11).
    const den = process.env.DEN;
    const general = JSON.parse(lib('api', 'bob', 'GET', `/api/dens/${den}/state`)).channels.find((x) => x.name === 'general').id;
    const after = lib('send', 'bob', general, 'after the move');
    if (BigInt(after) < BigInt(restoredAt) * 1000n) throw new Error(`the first message after the restore has ID ${after}`);
    ok(`the restored den gives a new message an ID past every one it gave out: ${after}`);

    step('removing the copy');
    await password('Remove from this computer');
    await waitFor(async () => (await text(card)).includes('Removed Moving Den from this computer'), 'the removal', 60000);
    const removed = hosting();
    if (removed.den || removed.exported) throw new Error(`hosting after the removal: ${JSON.stringify(removed)}`);
    ok(`the card says: ${await said(/Removed Moving Den.*?now\./)}`);
    await shot('removed');
    await closeSettings();
    await waitFor(() => b.eval(`!!${row} && ${row}.innerText.includes('Owner') && !/Hosted here|Offline|Connecting|Exported/.test(${row}.innerText)`),
        'her den at its address, as the owner');
    ok("her den's row is connected at its address, as its owner, and no longer hosted here");
    await shot('home-moved');

    step('the server goes for good');
    lib('stop', 'm2c');
    await waitFor(() => b.eval(`!!${row} && /Offline|Connecting/.test(${row}.innerText)`), 'her den offline', 60000);
    await openManage(b, 'Leave den');
    await waitFor(async () => (await text(MANAGE)).includes("Dens can't reach this den right now"), "Leave den, for a den Dens can't reach");
    ok(`Leave den says: ${(await text(MANAGE)).match(/Dens can't reach.*?Add a den\./)?.[0]}`);
    await shot('leave-gone');
    await b.eval(`${button(MANAGE, 'Remove from this computer')}.click(), true`);
    await waitFor(() => b.eval(`!${MANAGE} && !${row}`), 'the den off home', 30000);
    if (JSON.parse(lib('api', 'alice', 'GET', '/api/dens')).dens.length !== 0) throw new Error('the den is still listed');
    ok('Remove from this computer took the den whose server is gone off her computer');
    await shot('home-gone');
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    await b.close();
}
c.finish();
