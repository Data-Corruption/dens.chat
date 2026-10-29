// The page's notifications (M12.3): what the browser lets it do, asking it
// as a kind is turned on, showing what the service gives the page, a
// click opening the message, taking them back, and what the settings say
// of where they show.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { askToNotify, heading, mutedIn, notifying, permission, show, whereTheyShow, withdraw } from '../assets/js/src/notify.js';

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
    const open = (...at) => opened.push(at);
    const first = show(DM, open, b.win);
    assert.equal(first.title, 'Alice · Den');
    assert.equal(first.body, 'hello');
    assert.equal(first.tag, 'd1/7');
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
    assert.ok(!notifying({ dms: false, mentions: false }));
    assert.ok(!notifying(null));
    const m = mutedIn(st, 'd1');
    assert.ok(m.den && m.channels.has('7') && !m.channels.has('8'));
    const none = mutedIn(st, 'd2');
    assert.ok(!none.den && none.channels.size === 0);
});

test('the settings say where notifications show', () => {
    const on = { dms: true };
    const helper = { helper: true, notifies: true };
    assert.match(whereTheyShow(on, 'granted', helper), /^This browser shows them while a Dens tab is open.*With no Dens tab open, your desktop shows them\.$/);
    assert.match(whereTheyShow(on, 'denied', { helper: false }), /blocks Dens's notifications.*while Dens's helper runs/);
    assert.match(whereTheyShow(on, 'default', helper), /asks whether to show them/);
    assert.match(whereTheyShow(on, 'unsupported', helper), /can't show notifications/);
    assert.match(whereTheyShow({ ...on, shell: { allowed: true } }, 'unsupported', null), /^Android shows them/);
    assert.match(whereTheyShow({ ...on, shell: { allowed: false } }, 'unsupported', null), /^Android doesn't let Dens show notifications/);
});
