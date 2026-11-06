// The M17.4 check of events: alice, who hosts the den, and bob, a member,
// each in the browser named. Each page's Den tab starts its list with
// Events, which opens the den's events in the channel's place; alice's has
// + Event, bob's, a member's while events are staff's, none. Alice makes
// an event in Lounge through the form, with a description in the markdown
// subset, which reaches bob's page; bob marks Interested, which alice's
// page counts as he does; an event alice makes in Backstage, staff's
// alone, never reaches bob. Alice lets members make events in Manage's
// Limits, and bob makes one somewhere else through his form, his + Event
// gone at his limit, and cancels it. With bob's tab behind another, the event
// starts, moved to start in seconds as a development instance's den
// takes: his page shows the browser's notification of it, naming it and
// its channel, and its click opens his events at it. Both pages show it
// Live, the row too; alice moves its end from the form, whose start stays
// as it was, then ends it now, which takes it off both pages and bob's
// notification back. An event of seconds, which nobody marked, ends by the
// den's timer, notifying nobody. The den's database holds no event's name
// in the clear, and nobody's interest once they end.
//
//   run.sh events chromium|firefox
import { execFileSync } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import { ALICE, BOB, HOME, checks, launch, lib, pair, waitFor } from './browsers.mjs';

const [, , which] = process.argv;
const { DEN: den, GENERAL: general, LOUNGE: lounge, BACKSTAGE: backstage } = process.env;
const c = checks();
const report = c.report;
const step = (what) => console.error(`.. ${what}`);
const A = { ...ALICE };
const B = { ...BOB };
const q = (s) => JSON.stringify(s);
const api = (who, method, path, body) => {
    const args = ['api', who, method, path];
    if (body !== undefined) args.push(JSON.stringify(body));
    return lib(...args);
};
const state = (who) => JSON.parse(api(who, 'GET', `/api/dens/${den}/state`));
const eventsOf = (who) => state(who).events || [];
// count asks the den's database how many rows a query counts.
const count = (query, ...args) => Number(execFileSync('python3', ['-c',
    'import sqlite3, sys; db = sqlite3.connect(sys.argv[1]); print(db.execute(sys.argv[2], sys.argv[3:]).fetchone()[0])',
    `${HOME}/.local/share/dens-dev/m2a/data/db/app.db`, query, ...args]).toString().trim());
const may = (p) => p.then(() => true, () => false);

// The page's notifications, as it asks the browser for them: each one kept
// for the check, and its closing noted.
const RECORD = `() => {
    const Real = window.Notification;
    if (typeof Real !== 'function') return;
    window.notes = [];
    window.Notification = class extends Real {
        constructor(title, opts) {
            super(title, opts);
            this.shut = false;
            window.notes.push(this);
        }

        close() {
            this.shut = true;
            super.close();
        }
    };
}`;
const notes = () => B.b.eval(`(window.notes || []).map((n) => ({ title: n.title, body: n.body, tag: n.tag, shut: n.shut }))`);

const ROW = `document.querySelector('[data-events-row] button')`;
const card = (name) => `[...document.querySelectorAll('[data-event]')].find((x) => x.querySelector('h3')?.textContent === ${q(name)})`;
const cardText = (s, name) => s.b.eval(`${card(name)}?.innerText || ''`);
const button = (scope, text) => `[...${scope}.querySelectorAll('button')].find((x) => x.textContent.trim() === ${q(text)})`;
const DIALOG = `document.querySelector('[role=dialog]')`;

// openEvents opens a page's events from the row atop its Den tab.
async function openEvents(s) {
    await waitFor(() => s.b.eval(`!!${ROW}`), `the Events row on ${s.name}'s page`);
    await s.b.eval(`${ROW}.click(), true`);
    await waitFor(() => s.b.eval(`location.pathname === '/den/${den}/events' && !!document.querySelector('[data-events-header]')`),
        `the events on ${s.name}'s page`);
}

// fill sets a form's field as typing would, which Preact hears.
const fill = (s, selector, value) => s.b.eval(`(() => {
    const el = document.querySelector(${q(selector)});
    const set = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), 'value').set;
    set.call(el, ${q(value)});
    el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }));
    return true;
})()`);

// hide puts another tab in front of bob's, and front brings his back.
let other = null;
async function hide() {
    if (which === 'chromium') {
        other = (await B.b.cdp('Target.createTarget', { url: 'about:blank' }, null)).targetId;
        await B.b.cdp('Target.activateTarget', { targetId: other }, null);
    } else {
        other = (await B.b.bidi('browsingContext.create', { type: 'tab' })).context;
        await B.b.bidi('browsingContext.activate', { context: other });
    }
    await waitFor(() => B.b.eval(`document.visibilityState === 'hidden'`), "bob's tab hidden");
    await sleep(500);
}
async function front() {
    if (which === 'chromium') {
        await B.b.cdp('Target.closeTarget', { targetId: other }, null);
        await B.b.cdp('Page.bringToFront');
    } else {
        await B.b.bidi('browsingContext.close', { context: other });
        await B.b.bidi('browsingContext.activate', { context: B.b.context });
    }
    await waitFor(() => B.b.eval(`document.visibilityState === 'visible'`), "bob's tab in front");
    await sleep(500);
}

try {
    A.b = await launch(which, which === 'chromium' ? 9611 : 9612, { hover: true });
    B.b = await launch(which, which === 'chromium' ? 9613 : 9614, { hover: true, preload: RECORD, prefs: { 'permissions.default.desktop-notification': 1 } });
    for (const s of [A, B]) {
        await pair(s.b, s);
        if (which === 'chromium') await s.b.cdp('Browser.grantPermissions', { permissions: ['notifications'], origin: `http://localhost:${s.port}` }, null);
        await s.b.go(`http://localhost:${s.port}/den/${den}/${general}`);
        await waitFor(() => s.b.eval(`!!document.querySelector('textarea[placeholder="Message #general"]')`), `#general on ${s.name}'s page`);
    }

    step('each page lists the events atop the Den tab, which open in the channel’s place');
    for (const s of [A, B]) {
        report(await s.b.eval(`!!document.querySelector('nav[aria-label="Channels"] ul:first-child [data-events-row]')`),
            `${s.name}'s Den tab starts with Events`);
        await openEvents(s);
        report(/No events to come\./.test(await s.b.eval('document.body.innerText')), `${s.name}'s events say none are to come`);
    }
    report(await A.b.eval(`!!document.querySelector('[data-new-event]')`) && !(await B.b.eval(`!!document.querySelector('[data-new-event]')`)),
        'alice, the owner, has + Event, and bob, a member while events are staff’s, has none');
    await A.b.shot(`${HOME}/shot-events-none-${which}.png`);

    step('alice makes one through the form');
    await A.b.eval(`document.querySelector('[data-new-event]').click(), true`);
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-event-form]')`), 'the event form');
    await fill(A, '[data-event-name]', 'Game night');
    await fill(A, '[data-event-description]', 'Bring **snacks**');
    await fill(A, '[data-event-channel]', lounge);
    await fill(A, '[data-event-length]', '120');
    const offered = JSON.parse(await A.b.eval(`JSON.stringify([...document.querySelectorAll('[data-event-channel] option')].map((o) => o.textContent))`));
    report(offered.join() === 'Lounge,Backstage', `alice's form offers the den's voice channels she sees: ${q(offered)}`);
    await A.b.shot(`${HOME}/shot-events-form-${which}.png`);
    await A.b.eval(`${button('document.querySelector("[data-event-form]")', 'Make the event')}.click(), true`);
    await waitFor(() => A.b.eval(`!${DIALOG} && !!${card('Game night')}`), "the event on alice's page");
    const night = eventsOf('alice').find((e) => e.name === 'Game night');
    report(night && night.channel_id === lounge && night.ends_at - night.starts_at === 2 * 3600_000 && night.description === 'Bring **snacks**' && !night.live,
        `the den made the event as the form said: ${q(night)}`);
    report(await may(waitFor(() => cardText(B, 'Game night').then((t) => /Lounge/.test(t) && /Nobody interested yet/.test(t)), "the event on bob's page")),
        "bob's page shows it, in Lounge, nobody interested yet");
    report(await B.b.eval(`${card('Game night')}.querySelector('[data-description] strong')?.textContent === 'snacks'`),
        'its description shows in the markdown subset');
    report(await B.b.eval(`document.querySelector('[data-events-count]')?.textContent === '1'`), "bob's Events row says 1 to come");

    step('bob marks Interested, which alice’s page counts');
    await B.b.eval(`${card('Game night')}.querySelector('[data-interested]').click(), true`);
    report(await may(waitFor(() => B.b.eval(`${card('Game night')}.querySelector('[data-interested]').getAttribute('aria-pressed') === 'true'`),
        "bob's mark")), "bob's Interested shows pressed");
    report(await may(waitFor(() => cardText(A, 'Game night').then((t) => /1 interested/.test(t)), "alice's count")),
        "alice's page counts 1 interested, as bob marked it");

    step('an event in Backstage, staff’s alone, never reaches bob');
    const at = Date.now();
    api('alice', 'POST', `/api/dens/${den}/events`, { name: 'Staff meeting', starts_at: at + 3600_000, ends_at: at + 7200_000, channel_id: backstage });
    await waitFor(() => A.b.eval(`!!${card('Staff meeting')}`), "the staff's event on alice's page");
    await sleep(1000);
    report(!eventsOf('bob').some((e) => e.name === 'Staff meeting') && !(await B.b.eval(`!!${card('Staff meeting')}`)) &&
        (await B.b.eval(`document.querySelector('[data-events-count]')?.textContent === '1'`)),
    "bob's Dens and page have nothing of the staff's event");

    step('alice lets members make events, and bob makes one, then cancels it');
    const MANAGE = `document.querySelector('[role=dialog][aria-label^="Manage "]')`;
    await A.b.eval(`document.querySelector('button[data-den-manage]').click(), true`);
    await waitFor(() => A.b.eval(`!!${MANAGE}`), "alice's Manage");
    const limits = `[...${MANAGE}.querySelectorAll('nav button')].find((x) => x.textContent.trim() === 'Limits')`;
    await waitFor(() => A.b.eval(`!!${limits}`), "Manage's Limits");
    await A.b.eval(`${limits}.click(), true`);
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-member-events]')`), "Limits' events");
    await A.b.eval(`document.querySelector('[data-member-events]').click(), true`);
    const save = `[...${MANAGE}.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Save changes')`;
    await waitFor(() => A.b.eval(`!!${save}`), 'the bar');
    await A.b.eval(`${save}.click(), true`);
    await waitFor(() => A.b.eval(`${MANAGE}.textContent.includes('Saved.')`), 'saved');
    await A.b.eval(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), true`);
    report(state('bob').event_limits?.member_events === 1, "alice's Limits let each member make one event");
    report(await may(waitFor(() => B.b.eval(`!!document.querySelector('[data-new-event]')`), "bob's + Event")), 'bob’s page now has + Event');
    await B.b.eval(`document.querySelector('[data-new-event]').click(), true`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-event-form]')`), "bob's event form");
    await fill(B, '[data-event-name]', 'Quiz');
    await B.b.eval(`[...document.querySelectorAll('[data-event-form] input[type=radio]')][1].click(), true`);
    await waitFor(() => B.b.eval(`!!document.querySelector('[data-event-place]')`), 'the place written out');
    await fill(B, '[data-event-place]', 'The back room');
    await B.b.eval(`${button('document.querySelector("[data-event-form]")', 'Make the event')}.click(), true`);
    report(await may(waitFor(() => A.b.eval(`!!${card('Quiz')}`), "bob's event on alice's page")) && /The back room/.test(await cardText(A, 'Quiz')),
        "bob's event reached alice's page, its place written out");
    report(await may(waitFor(() => B.b.eval(`!!${card('Quiz')} && !document.querySelector('[data-new-event]')`), "bob's + Event gone")),
        "at his limit, bob's page has no + Event");
    await B.b.eval(`${button(card('Quiz'), 'Cancel…')}.click(), true`);
    await waitFor(() => B.b.eval(`!!${DIALOG} && !!${button(DIALOG, 'Cancel the event')}`), 'the Cancel dialog');
    await B.b.eval(`${button(DIALOG, 'Cancel the event')}.click(), true`);
    for (const s of [A, B]) {
        report(await may(waitFor(() => s.b.eval(`!${card('Quiz')}`), `Quiz gone from ${s.name}'s page`)), `bob's canceled event left ${s.name}'s page`);
    }

    step('with bob on #general, his tab behind another, the event starts');
    await B.b.eval(`document.querySelector('[data-channel-row="${general}"] button').click(), true`);
    await waitFor(() => B.b.eval(`location.pathname === '/den/${den}/${general}' && !!document.querySelector('textarea[placeholder="Message #general"]')`),
        "#general on bob's page");
    const starts = Date.now() + 8000;
    api('alice', 'PATCH', `/api/dens/${den}/events/${night.id}`, { starts_at: starts, ends_at: starts + 3600_000 });
    await hide();
    const [shown] = await waitFor(async () => {
        const n = await notes();
        return n.length > 0 && n;
    }, 'the event’s notification', 30000);
    report(shown.title === 'Game night · Voice Den' && shown.body === 'Live now in Lounge' && shown.tag === `${den}/event-${night.id}`,
        `bob's page showed the start's notification, naming the event and its channel: ${q(shown)}`);
    await B.b.eval(`window.notes[0].dispatchEvent(new Event('click')), true`);
    report(await may(waitFor(() => B.b.eval(`location.pathname === '/den/${den}/events' && !!${card('Game night')}?.className.includes('ring-2')`),
        'the event opened at it', 10000)), "its click opened bob's events at it, lit");
    await front();

    step('both pages show it Live, the row too');
    for (const s of [A, B]) {
        report(await may(waitFor(() => s.b.eval(`!!${card('Game night')}?.querySelector('[data-live]') && !!document.querySelector('[data-events-live]')`),
            `Live on ${s.name}'s page`)), `${s.name}'s page shows the event Live, and its row Live`);
    }
    report(/^Live until /.test(await B.b.eval(`${card('Game night')}.querySelector('[data-when]').textContent`)), 'its time says it runs until its end');
    await B.b.shot(`${HOME}/shot-events-live-${which}.png`);

    step('alice moves its end from the form, its start staying');
    await A.b.eval(`${button(card('Game night'), 'Edit')}.click(), true`);
    await waitFor(() => A.b.eval(`!!document.querySelector('[data-event-form]')`), 'the form for the live event');
    report(await A.b.eval(`document.querySelector('[data-event-date]').disabled && document.querySelector('[data-event-time]').disabled`),
        "the live event's start can't change in the form");
    await fill(A, '[data-event-length]', '180');
    await A.b.eval(`${button('document.querySelector("[data-event-form]")', 'Save')}.click(), true`);
    await waitFor(() => A.b.eval(`!${DIALOG}`), 'the form saved');
    const moved = eventsOf('bob').find((e) => e.id === night.id);
    report(moved?.live && moved.starts_at === starts && moved.ends_at === starts + 3 * 3600_000, `its end moved, its start the same: ${q(moved)}`);

    step('alice ends it now');
    await A.b.eval(`${button(card('Game night'), 'End now')}.click(), true`);
    await waitFor(() => A.b.eval(`!!${DIALOG} && !!${button(DIALOG, 'End now')}`), 'the End now dialog');
    await A.b.eval(`${button(DIALOG, 'End now')}.click(), true`);
    for (const s of [A, B]) {
        report(await may(waitFor(() => s.b.eval(`!${card('Game night')}`), `the event gone from ${s.name}'s page`)), `the ended event left ${s.name}'s page`);
    }
    report(await may(waitFor(async () => (await notes())[0]?.shut, 'the notification taken back', 10000)), "bob's notification of it was taken back");
    report(!(await B.b.eval(`!!document.querySelector('[data-events-live]') || !!document.querySelector('[data-events-count]')`)),
        "bob's Events row says nothing now");

    step('an event of seconds ends by the den’s timer, notifying nobody');
    const flash = Date.now() + 3000;
    api('alice', 'POST', `/api/dens/${den}/events`, { name: 'Flash', starts_at: flash, ends_at: flash + 6000, place: 'The back room' });
    report(await may(waitFor(() => B.b.eval(`!!${card('Flash')}?.querySelector('[data-live]')`), 'Flash live on bob’s page', 15000)),
        "the short event went live on bob's page, in a place written out");
    report(await may(waitFor(() => B.b.eval(`!${card('Flash')}`), 'Flash gone from bob’s page', 15000)), 'and left it at its end');
    report((await notes()).length === 1, 'nobody interested, it notified nobody');

    step("the den's database");
    report(count(`SELECT count(*) FROM den_events WHERE instr(details, CAST(? AS BLOB)) > 0`, 'Game night') === 0 && count(`SELECT count(*) FROM den_events`) === 3,
        "the den's database holds the events, none of their names in the clear");
    report(count(`SELECT count(*) FROM den_event_interest`) === 0, 'and nobody’s interest in those that ended');
    await A.b.shot(`${HOME}/shot-events-alice-${which}.png`);
} catch (e) {
    c.fail(e);
} finally {
    await A.b?.close();
    await B.b?.close();
}
c.finish();
