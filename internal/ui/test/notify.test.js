// The page's notifications: what the browser lets it do, asking it
// as a kind is turned on, showing what the service gives the page, a
// click opening the message, taking them back, and what the settings say
// of where they show.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { askToNotify, heading, levelMenu, levelOf, mutedIn, notifying, permission, quiet, quietEnd, setLevel, show, untilWords, whereTheyShow, withdraw } from '../src/core/notify.js';

// browser is a window whose Notification starts at permission, answers a
// request with answer, and keeps what it shows.
function browser(permission, answer = permission) {
    const shown = [];
    let asked = 0;
    let focused = 0;
    class Notification {
        static permission = permission;

        static async requestPermission() {
            asked++;
            Notification.permission = answer;
            return answer;
        }

        constructor(title, opts) {
            this.title = title;
            this.body = opts.body;
            this.tag = opts.tag;
            this.silent = opts.silent;
            this.closed = false;
            shown.push(this);
        }

        close() {
            if (this.closed) return;
            this.closed = true;
            this.onclose?.();
        }
    }
    return { win: { Notification, focus: () => focused++ }, shown, asked: () => asked, focused: () => focused };
}

const DM = { key: 'd1/7', den: 'd1', denName: 'Den', channel: '7', message: '10', title: 'Alice', text: 'hello' };

test("what the browser lets the page do, and Android's WebView, which has no notifications", () => {
    assert.equal(permission(browser('granted').win), 'granted');
    assert.equal(permission(browser('denied').win), 'denied');
    assert.equal(permission(browser('default').win), 'default');
    assert.equal(permission({}), 'unsupported');
});

test('turning a kind on asks the browser once, and what it answers stands', async () => {
    const granted = browser('default', 'granted');
    assert.equal(await askToNotify(granted.win), 'granted');
    assert.equal(await askToNotify(granted.win), 'granted');
    assert.equal(granted.asked(), 1, 'a browser that answered is not asked again');

    const refused = browser('default', 'denied');
    assert.equal(await askToNotify(refused.win), 'denied');
    assert.equal(permission(refused.win), 'denied');
    const blocked = browser('denied');
    assert.equal(await askToNotify(blocked.win), 'denied');
    assert.equal(blocked.asked(), 0, 'a browser that blocks them is not asked');
    assert.equal(await askToNotify({}), 'unsupported');
});

test('a notification shows with its den, replaces its channel\'s, and a click opens its message', () => {
    const b = browser('granted');
    const opened = [];
    const open = (n) => opened.push([n.den, n.channel, n.message]);
    const first = show(DM, open, b.win);
    assert.equal(first.title, 'Alice · Den');
    assert.equal(first.body, 'hello');
    assert.equal(first.tag, 'd1/7');
    assert.equal(first.silent, true, 'silent, since the page plays the message\'s sound');
    const newer = show({ ...DM, message: '11', text: 'again' }, open, b.win);
    assert.ok(first.closed, "a channel's newer notification replaces its last");
    let prevented = false;
    newer.onclick({ preventDefault: () => (prevented = true) });
    assert.ok(!prevented, 'the browser brings the tab forward, as it does for a click nothing keeps it from');
    assert.ok(newer.closed);
    assert.equal(b.focused(), 1, 'the page asks it to as well');
    assert.deepEqual(opened, [['d1', '7', '11']]);
});

test('the service takes notifications back, and a browser that blocks them shows none', () => {
    const b = browser('granted');
    const note = show({ ...DM, key: 'd1/8', channel: '8' }, () => {}, b.win);
    withdraw(['d1/8', 'd1/9']);
    assert.ok(note.closed);
    assert.equal(show(DM, () => {}, browser('denied').win), null);
    assert.equal(show(DM, () => {}, {}), null);
    assert.equal(heading({ title: 'Bob in #general', denName: '' }), 'Bob in #general');
});

test('mutes and kinds, as the service says them', () => {
    const st = { dms: false, mentions: true, muted: { d1: { den: true, channels: ['7'] } } };
    assert.ok(notifying(st));
    assert.ok(notifying({ dms: false, mentions: false }), 'events notify unless turned off');
    assert.ok(!notifying({ dms: false, mentions: false, eventsOff: true }));
    assert.ok(!notifying(null));
    const m = mutedIn(st, 'd1');
    assert.ok(m.den && m.channels.has('7') && !m.channels.has('8'));
    const none = mutedIn(st, 'd2');
    assert.ok(!none.den && none.channels.size === 0 && none.all.size === 0);
});

test("a channel's level: Nothing as v1.14.0's mutes kept it, All messages beside it, Mentions until the member chooses", () => {
    const st = { muted: { d1: { channels: ['7'], all: ['9'] } } };
    assert.equal(levelOf(st, 'd1', '7'), 'nothing');
    assert.equal(levelOf(st, 'd1', '9'), 'all');
    assert.equal(levelOf(st, 'd1', '8'), 'mentions');
    assert.equal(levelOf(null, 'd1', '8'), 'mentions');
    assert.ok(mutedIn(st, 'd1').all.has('9'));
    assert.ok(notifying({ eventsOff: true, muted: { d1: { all: ['9'] } } }), 'a channel at All messages notifies with every kind off');
    assert.ok(!notifying({ eventsOff: true, muted: { d1: { channels: ['7'] } } }));
    const items = levelMenu('all');
    assert.deepEqual(items.map((i) => `${i.label}${i.checked ? ' (checked)' : ''}`), ['All messages (checked)', 'Mentions', 'Nothing']);
});

test('All messages asks the browser in the click, and goes without waiting for its answer', async (t) => {
    const sent = [];
    let asked = 0;
    // The browser's prompt stays open, as one a member leaves.
    globalThis.Notification = class { static permission = 'default'; static requestPermission() { asked++; return new Promise(() => {}); } };
    globalThis.fetch = async (path, opts) => {
        sent.push(`${opts.method} ${path} ${opts.body}`);
        return { ok: true, status: 200, headers: { get: () => null }, text: async () => '{}' };
    };
    t.after(() => {
        delete globalThis.Notification;
        delete globalThis.fetch;
    });
    await setLevel('d1', '9', 'all');
    await setLevel('d1', '9', 'nothing');
    assert.equal(asked, 1, 'only All messages asks');
    assert.deepEqual(sent, ['POST /api/dens/d1/notifications {"channel":"9","level":"all"}', 'POST /api/dens/d1/notifications {"channel":"9","level":"nothing"}']);
});

test("Do not disturb's ends, read against the device's clock", () => {
    const at = (h, m = 0) => new Date(2026, 10, 21, h, m);
    assert.equal(quietEnd('hour', at(22, 15)).getTime(), at(23, 15).getTime());
    assert.equal(quietEnd('tomorrow', at(22, 15)).getTime(), new Date(2026, 10, 22, 8).getTime(), 'the next 8:00, tomorrow');
    assert.equal(quietEnd('tomorrow', at(2)).getTime(), at(8).getTime(), 'the next 8:00, past midnight');
    assert.equal(quietEnd('tomorrow', at(8)).getTime(), new Date(2026, 10, 22, 8).getTime());
    assert.equal(quietEnd('manual', at(22)), null);

    const now = at(22).getTime();
    assert.deepEqual(quiet({ dnd: true, dndUntil: at(23).toISOString() }, now), { on: true, until: at(23).getTime() });
    assert.deepEqual(quiet({ dnd: true }, now), { on: true, until: null });
    assert.deepEqual(quiet({ dnd: true, dndUntil: at(21).toISOString() }, now), { on: false, until: null }, 'an end gone by is off');
    assert.deepEqual(quiet({ dndUntil: at(23).toISOString() }, now), { on: false, until: null });
    assert.deepEqual(quiet(null, now), { on: false, until: null });

    assert.match(untilWords(at(23, 30).getTime(), now), /^until 11:30\sPM$|^until 23:30$/);
    assert.match(untilWords(new Date(2026, 10, 22, 8).getTime(), now), /^until tomorrow, 8:00\sAM$|^until tomorrow, 08:00$/);
    assert.equal(untilWords(null, now), 'until you turn it off');
});

test('the settings say where notifications show only what calls for a line', () => {
    const on = { dms: true };
    const helper = { helper: true, notifies: true };
    const said = (...args) => whereTheyShow(...args).map(([kind, line]) => `${kind}: ${line}`);
    assert.deepEqual(said(on, 'granted', helper), ['success: With no Dens tab open, your desktop shows them.']);
    assert.deepEqual(said(on, 'default', { helper: false }), []);
    assert.deepEqual(said(on, 'denied', { helper: false }), ['warning: This browser blocks them; allow them in its site settings.']);
    assert.match(said(on, 'unsupported', helper).join('\n'), /^warning: This browser can't show notifications\.\nsuccess: /);
    assert.deepEqual(said({ ...on, shell: { allowed: true } }, 'unsupported', null), []);
    assert.match(said({ ...on, shell: { allowed: false } }, 'unsupported', null).join(' '), /^warning: Android doesn't let Dens show notifications/);
});
