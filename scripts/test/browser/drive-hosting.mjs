// The M9.1 check: alice's instance hosts nothing until she turns hosting
// on in the settings, with her local password, and then home offers to
// create her den, and the setting says where Caddy and the router send it.
// Turned off, once she confirms, her den's row says so and leads back to
// the switch. A port another program holds fails turning it on, naming the
// port and the flag that moves it.
//
//   run.sh hosting chromium|firefox
import net from 'node:net';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, HOME, SETTINGS, checks, launch, lib, openSettings, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const c = checks();
const ok = (what) => c.report(true, what);
const step = (what) => console.error(`.. ${what}`);
const hosting = () => JSON.parse(lib('api', 'alice', 'GET', '/api/hosting'));
const DEN_PORT = 8485;

step(`launching ${which}`);
const b = await launch(which, 9331);
const shot = (name) => b.shot(`${HOME}/shot-hosting-${which}-${name}.png`);
const card = `[...${SETTINGS}.querySelectorAll('section')].find((s) => s.querySelector('#hosting'))`;
const toggle = `document.getElementById('hosting')`;
const button = (scope, text) => `[...${scope}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(text)})`;
const text = (scope) => b.eval(`${scope}.innerText.replace(/\\s+/g, ' ')`);
const closeSettings = () => b.eval(`${SETTINGS}.querySelector('button[aria-label=Close]').click(), true`);
const row = `[...document.querySelectorAll('li')].find((l) => l.innerText.includes('Hosting Den'))`;

// submit types the password into the card's form and turns hosting on.
async function submit(password) {
    await b.eval(`(() => {
        const input = ${card}.querySelector('input[type=password]');
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, ${JSON.stringify(password)});
        input.dispatchEvent(new Event('input', { bubbles: true }));
        return true;
    })()`);
    await b.eval(`${button(card, 'Turn hosting on')}.click(), true`);
}

async function askToTurnOn() {
    await b.eval(`${toggle}.click(), true`);
    await waitFor(() => b.eval(`!!${card}.querySelector('input[type=password]')`), 'the password field');
}

try {
    step('pairing');
    await pair(b, ALICE);
    await waitFor(() => b.eval(`!!${button('document', 'Add a den')}`), 'home');
    if ((await text('document.body')).includes('Create your den')) throw new Error('home offers to create a den with hosting off');
    await b.eval(`${button('document', 'Add a den')}.click(), true`);
    await waitFor(() => b.eval(`!!${button('document', 'Host one on this computer')}`), "Add a den's link to hosting");
    await b.eval(`${button('document', 'Host one on this computer')}.click(), true`);
    await waitFor(() => b.eval(`!!${card}`), 'the hosting card');
    if (await b.eval(`${toggle}.checked`)) throw new Error('the switch shows on');
    ok("with hosting off, home offers no den to create, and Add a den's link opens the switch, off");
    await shot('off');

    step('turning hosting on');
    await askToTurnOn();
    await shot('password');
    await submit('wrong wrong wrong');
    await waitFor(async () => /wrong password/i.test(await text(card)), 'the wrong password refused');
    if (hosting().enabled) throw new Error('hosting turned on with the wrong password');
    ok('the wrong password leaves hosting off, and says so');
    await submit('local password');
    await waitFor(async () => (await text(card)).includes(`127.0.0.1:${DEN_PORT}`), "the card's line on where Caddy sends the den");
    const on = hosting();
    if (!on.enabled || !on.serving || !(await b.eval(`${toggle}.checked`))) throw new Error(`hosting after turning it on: ${JSON.stringify(on)}`);
    ok(`hosting turned on with the password, and the card says: ${(await text(card)).match(/Caddy forwards.*?calls\./)[0]}`);
    await shot('on');
    await closeSettings();
    await waitFor(async () => (await text('document.body')).includes('Create your den'), 'home offering to create the den');
    ok('home offers to create the den');

    step('creating the den');
    lib('api', 'alice', 'POST', '/api/den', JSON.stringify({
        name: 'Hosting Den', url: `http://127.0.0.1:${DEN_PORT}`, username: 'alice', display_name: 'Alice', password: 'den password',
    }));
    await waitFor(() => b.eval(`!!${row} && !${row}.innerText.includes('Hosting is off') && !/Offline|Connecting/.test(${row}.innerText)`), 'the den connected on home');
    ok('the den is on home, connected');

    step('turning hosting off');
    await openSettings(b, 'System');
    await waitFor(() => b.eval(`!!${card}`), 'the hosting card');
    await b.eval(`${toggle}.click(), true`);
    await waitFor(async () => (await text(card)).includes("Members can't reach your den until hosting is back on"), 'the confirmation');
    if (hosting().enabled === false) throw new Error('hosting went off before she confirmed');
    await shot('confirm-off');
    await b.eval(`${button(card, 'Turn hosting off')}.click(), true`);
    await waitFor(async () => !(await b.eval(`${toggle}.checked`)) && !(await text(card)).includes('Caddy forwards'), 'the switch off');
    const off = hosting();
    if (off.enabled || off.serving) throw new Error(`hosting after turning it off: ${JSON.stringify(off)}`);
    ok('hosting turned off once she confirmed');
    await closeSettings();
    await waitFor(() => b.eval(`!!${row} && ${row}.innerText.includes('Hosting is off')`), "the den's row saying hosting is off");
    ok(`the den's row says: ${(await b.eval(`${row}.innerText.replace(/\\s+/g, ' ')`)).match(/Hosting is off.*?back on\./)?.[0]}`);
    await shot('home-off');
    await b.eval(`${button(row, 'Turn hosting on')}.click(), true`);
    await waitFor(() => b.eval(`!!${card}`), 'the hosting card, from the row');
    ok("the row's link opens the switch");

    step('a port another program holds');
    // Her own client tries the den's port again while it's held, so the
    // stand-in closes what it takes, or closing it would wait on that.
    const held = net.createServer((socket) => socket.destroy());
    await new Promise((resolve, reject) => held.once('error', reject).listen(DEN_PORT, '127.0.0.1', resolve));
    await askToTurnOn();
    await submit('local password');
    await waitFor(async () => (await text(card)).includes(`port ${DEN_PORT}`), 'the port named');
    const said = await text(card);
    if (!said.includes('--den-port') || !said.includes('-DenPort') || hosting().enabled) throw new Error(`with the port held, the card says: ${said}`);
    ok(`with the port held, hosting stays off, and the card says: ${said.match(/Hosting can't listen[^]*?Windows\./)?.[0]}`);
    await shot('port-held');
    await new Promise((resolve) => held.close(resolve));
    // The service checks the local password three times at once, then
    // once every 2 seconds, and this is the fourth check.
    await sleep(2500);
    await submit('local password');
    await waitFor(async () => hosting().serving, 'hosting on once the port is free');
    await closeSettings();
    await waitFor(() => b.eval(`!!${row} && !/Hosting is off|Offline|Connecting/.test(${row}.innerText)`), 'the den connected again');
    ok('once the port is free hosting turns on, and the den is connected again');
} catch (e) {
    c.fail(e);
    await shot('failed').catch(() => {});
} finally {
    await b.close();
}
c.finish();
