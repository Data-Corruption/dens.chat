// The M5.1 check: Alice, in Chromium, owns the den and sets retention in
// its settings; Bob, in Firefox, watches #general, whose messages are three
// days old, vanish without a reload, and #random's unread mark and mention
// go with them.
//
//   run.sh retention
import { execFileSync } from 'node:child_process';
import { ALICE, BOB, HOME, MANAGE, checks, launch, openManage, pair, waitFor } from './browsers.mjs';

const { DEN: den, GENERAL: general } = process.env;
const c = checks();
const ok = (what) => c.report(true, what);

const alice = await launch('chromium', 9301);
const bob = await launch('firefox', 9302);
try {
    await pair(alice, ALICE);
    await pair(bob, BOB);
    // Bob reads #general, three days of history and #random unread.
    await bob.go(`http://localhost:18486/den/${den}/${general}`);
    await waitFor(() => bob.eval(`document.body.innerText.includes('old two, from alice')`), "bob's #general");
    await waitFor(() => bob.eval(`document.body.innerText.includes('This is the start of #general.')`), "#general's start");
    const unread = () => bob.eval(`(() => {
        const name = [...document.querySelectorAll('li button span.truncate')].find((s) => s.textContent === 'random');
        const row = name?.closest('button');
        return JSON.stringify({ bold: !!name?.classList.contains('font-bold'), badge: row?.querySelector('.badge')?.textContent || '' });
    })()`);
    if ((await unread()) !== JSON.stringify({ bold: true, badge: '1' })) throw new Error(`#random before: ${await unread()}`);
    ok("bob sees #general's old messages, and #random unread with a mention");
    await bob.shot(`${HOME}/shot-retention-bob-before.png`);

    // Alice turns auto-delete on in the den's limits, and sees what it deletes.
    await alice.go('http://localhost:18484/');
    await openManage(alice, 'Limits');
    const form = MANAGE;
    await waitFor(() => alice.eval(`!!document.getElementById('auto-delete')`), 'the auto-delete toggle');
    await alice.eval(`document.getElementById('auto-delete').click(), true`);
    await alice.eval(`(() => {
        const input = document.querySelector('input[aria-label="Days to keep messages"]');
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, '1');
        input.dispatchEvent(new Event('input', { bubbles: true }));
        return true;
    })()`);
    const preview = await waitFor(() => alice.eval(`${form}.innerText.match(/Saving this deletes[^.]*\\./)?.[0]`), 'the preview');
    if (preview !== 'Saving this deletes 3 messages now.') throw new Error(`preview: ${preview}`);
    ok(`alice's limits say: ${preview}`);
    await alice.shot(`${HOME}/shot-retention-alice-retention.png`);
    await alice.eval(`[...${form}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Save changes').click(), true`);
    await waitFor(() => alice.eval(`${form}.innerText.includes('Saved.')`), 'saved');
    ok('alice saved auto-delete after 1 day');

    // Bob's open page drops the old messages without a reload.
    await waitFor(() => bob.eval(`!document.body.innerText.includes('old two, from alice') && !document.body.innerText.includes('old one, from bob')`),
        "the old messages leaving bob's page", 15000);
    await waitFor(() => bob.eval(`document.body.innerText.includes('Messages in #general are deleted after 1 day.')`), '#general\'s retention line');
    await waitFor(async () => (await unread()) === JSON.stringify({ bold: false, badge: '' }), "#random's unread mark leaving");
    ok("bob's page dropped the old messages live, says how long #general keeps them, and #random is read");
    await bob.shot(`${HOME}/shot-retention-bob-after.png`);

    // The list still takes new messages.
    execFileSync('bash', ['-c', `cd ${HOME} && curl -sS --fail-with-body -b jar-m2a -c jar-m2a -X POST -H 'Origin: http://127.0.0.1:18484' -H 'Content-Type: application/json' `
        + `--data '{"nonce":"AAAAAAAAAAAAAAAAAAAAAA","text":"fresh, after retention"}' http://127.0.0.1:18484/api/dens/${den}/channels/${general}/messages >/dev/null`]);
    await waitFor(() => bob.eval(`document.body.innerText.includes('fresh, after retention')`), 'a new message on bob\'s page');
    ok('a new message reaches bob after the pass');

    // Bob's Manage dialog names the period.
    await bob.go('http://localhost:18486/');
    await openManage(bob, 'General');
    await waitFor(() => bob.eval(`${MANAGE}.innerText.includes('Messages here are deleted after 1 day.')`), "bob's Manage dialog");
    ok("bob's Manage dialog says how long the den keeps messages");
    await bob.shot(`${HOME}/shot-retention-bob-home.png`);
} catch (e) {
    c.fail(e);
} finally {
    await alice.close();
    await bob.close();
    c.finish();
}
