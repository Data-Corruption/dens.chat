import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { h, render as mount } from 'preact';
import { HOME, createNav, denTrail, holdLayout, paneOf, pathOf, plan, readTrail, trailFor, useNarrow } from '../assets/js/src/nav.js';

const list = (den) => ({ k: 'list', den });
const chan = (den, ch) => ({ k: 'chan', den, ch });
const side = (den, ch) => ({ k: 'side', den, ch });

test('an address opens Home, or a channel with Home and its den beneath it on the phone layout', () => {
    assert.deepEqual(trailFor('/', true), [HOME]);
    assert.deepEqual(trailFor('/settings', true), [HOME]);
    assert.deepEqual(trailFor('/den/abc', true), [HOME, list('abc')]);
    assert.deepEqual(trailFor('/den/abc/12', true), [HOME, list('abc'), chan('abc', '12')]);
    assert.deepEqual(trailFor('/den/abc/dms', true), [HOME, list('abc'), chan('abc', 'dms')]);
    assert.deepEqual(trailFor('/den/abc', false), [HOME, chan('abc', '')], 'a wider window shows a den as one level');
    assert.deepEqual(trailFor('/den/abc/12', false), [HOME, chan('abc', '12')]);
    assert.deepEqual(denTrail('abc', '12', 'side', true), [HOME, list('abc'), chan('abc', '12'), side('abc', '12')]);
    assert.deepEqual(denTrail('abc', '12', 'side', false), [HOME, chan('abc', '12')]);
});

test("a trail's address is its screen's, under any layers, and its pane the den's", () => {
    assert.equal(pathOf([HOME]), '/');
    assert.equal(pathOf([HOME, list('abc')]), '/den/abc', "the list keeps the den's own address");
    assert.equal(pathOf([HOME, list('abc'), chan('abc', '12')]), '/den/abc/12');
    assert.equal(pathOf([HOME, chan('abc', '')]), '/den/abc');
    assert.equal(pathOf([HOME, list('abc'), chan('abc', '12'), side('abc', '12'), { k: 'layer', id: 3 }]), '/den/abc/12');
    assert.equal(pathOf([HOME, { k: 'layer', id: 1 }]), '/');
    assert.equal(paneOf([HOME, list('abc'), chan('abc', '12'), side('abc', '12'), { k: 'layer', id: 3 }]), 'side');
    assert.equal(paneOf([HOME, list('abc')]), 'list');
    assert.equal(paneOf([HOME, { k: 'layer', id: 1 }]), '');
});

test('a move goes back to where two trails part, then replaces and pushes', () => {
    const at = [HOME, list('a'), chan('a', '1')];
    assert.deepEqual(plan(at, [...at, side('a', '1')]), { back: 0, keep: 3, replace: false }, 'deeper: push');
    assert.deepEqual(plan(at, [HOME, list('a')]), { back: 1, keep: 2, replace: false }, 'up: back');
    assert.deepEqual(plan(at, [HOME, list('a'), chan('a', '2')]), { back: 0, keep: 2, replace: true }, 'beside: replace');
    assert.deepEqual(plan([...at, side('a', '1')], [HOME, list('a'), chan('a', '2')]), { back: 1, keep: 2, replace: true });
    assert.deepEqual(plan(at, [HOME, list('b'), chan('b', '5')]), { back: 1, keep: 1, replace: true }, 'another den');
    assert.deepEqual(plan(at, [HOME]), { back: 2, keep: 1, replace: false }, 'Home');
    assert.deepEqual(plan(at, at), { back: 0, keep: 3, replace: false }, 'there already');
    assert.deepEqual(plan([...at, { k: 'layer', id: 4 }], [HOME, list('a'), chan('a', '2')]), { back: 1, keep: 2, replace: true },
        'a move drops the layers over it');
});

test("an entry's state reads as a trail only as the page writes it", () => {
    assert.deepEqual(readTrail({ trail: [HOME, list('a')] }), [HOME, list('a')]);
    assert.equal(readTrail(null), null);
    assert.equal(readTrail({}), null);
    assert.equal(readTrail({ trail: [] }), null);
    assert.equal(readTrail({ trail: [list('a')] }), null, 'Home is the root');
    assert.equal(readTrail({ trail: [HOME, { k: 'chan', den: 'a' }] }), null);
    assert.equal(readTrail({ trail: [HOME, { k: 'layer', id: 'x' }] }), null);
    assert.equal(readTrail({ trail: [HOME, { k: 'other' }] }), null);
});

// fakeWindow stands in for a browser's window: its history holds entries
// and moves through them a task later, as a browser's does, telling the
// page with popstate.
function fakeWindow(path, { wide = false, state = null } = {}) {
    const listeners = {};
    const win = {
        location: { pathname: path, hash: '' },
        matchMedia: () => ({ matches: wide }),
        addEventListener: (type, fn) => {
            (listeners[type] ||= []).push(fn);
        },
        entries: [{ state, path }],
        at: 0,
    };
    const set = (i) => {
        win.at = i;
        const [p, hash] = win.entries[i].path.split('#');
        win.location.pathname = p;
        win.location.hash = hash ? `#${hash}` : '';
    };
    win.history = {
        get state() {
            return win.entries[win.at].state;
        },
        pushState(s, _, p) {
            win.entries.splice(win.at + 1, Infinity, { state: s, path: p });
            set(win.at + 1);
        },
        replaceState(s, _, p) {
            win.entries[win.at] = { state: s, path: p };
            set(win.at);
        },
        go(delta) {
            setTimeout(() => {
                const to = win.at + delta;
                if (to < 0 || to >= win.entries.length) return;
                set(to);
                (listeners.popstate || []).forEach((fn) => fn({ state: win.entries[to].state }));
            }, 0);
        },
    };
    // back is the browser's Back, as a member presses it.
    win.back = () => win.history.go(-1);
    // load is a fragment's load on the page, as the Android app's shell
    // makes one: a new entry, then popstate.
    win.load = (fragment) => {
        win.entries.splice(win.at + 1, Infinity, { state: null, path: `${win.location.pathname}#${fragment}` });
        set(win.at + 1);
        (listeners.popstate || []).forEach((fn) => fn({ state: null }));
    };
    win.paths = () => win.entries.map((e) => e.path);
    return win;
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 20));

test('a page opened at a channel on the phone layout puts Home and the list beneath it, and Back walks up them', async () => {
    const win = fakeWindow('/den/a/1');
    const nav = createNav(win);
    nav.start();
    assert.deepEqual(win.paths(), ['/', '/den/a', '/den/a/1']);
    assert.equal(win.at, 2);
    assert.equal(paneOf(nav.trail), 'chan');
    nav.den('a', '1', 'side');
    await settle();
    assert.deepEqual(win.paths(), ['/', '/den/a', '/den/a/1', '/den/a/1']);
    assert.equal(paneOf(nav.trail), 'side');
    const seen = [];
    nav.watch((t) => seen.push(`${paneOf(t) || 'home'} ${pathOf(t)}`));
    for (let i = 0; i < 3; i++) {
        win.back();
        await settle();
    }
    assert.deepEqual(seen, ['chan /den/a/1', 'list /den/a', 'home /']);
    assert.equal(win.at, 0, 'Home is the root');
});

test('a channel opened from the list goes above it, another opened from a channel takes its place, and Home goes back to the root', async () => {
    const win = fakeWindow('/');
    const nav = createNav(win);
    nav.start();
    nav.open('/den/a');
    await settle();
    assert.deepEqual(win.paths(), ['/', '/den/a']);
    nav.den('a', '1', 'chan');
    await settle();
    nav.den('a', '2', 'chan');
    await settle();
    assert.deepEqual(win.paths(), ['/', '/den/a', '/den/a/2']);
    nav.den('a', '2', 'side');
    await settle();
    nav.den('a', '3', 'chan');
    await settle();
    assert.deepEqual(win.paths().slice(0, win.at + 1), ['/', '/den/a', '/den/a/3'], 'from the members, back to the channel, then beside it');
    nav.home();
    await settle();
    assert.equal(win.at, 0);
    assert.deepEqual(nav.trail, [HOME]);
});

test('a link into another den goes back to its list and opens the channel above it', async () => {
    const win = fakeWindow('/den/a/1');
    const nav = createNav(win);
    nav.start();
    nav.open('/den/b/7');
    await settle();
    assert.deepEqual(win.paths(), ['/', '/den/b', '/den/b/7']);
    assert.deepEqual(nav.trail, [HOME, list('b'), chan('b', '7')]);
});

test('in a wider window a den is one level, and its channels take turns in it', async () => {
    const win = fakeWindow('/', { wide: true });
    const nav = createNav(win);
    nav.start();
    nav.open('/den/a');
    await settle();
    nav.den('a', '1', 'chan');
    await settle();
    nav.den('a', '2', 'chan');
    await settle();
    assert.deepEqual(win.paths(), ['/', '/den/a/2']);
    win.back();
    await settle();
    assert.deepEqual(nav.trail, [HOME]);
});

test('a swipe up a level goes back where the entry beneath is that level, and replaces where it is not', async () => {
    const win = fakeWindow('/den/a/1', { wide: true });
    const nav = createNav(win);
    nav.start();
    assert.deepEqual(win.paths(), ['/', '/den/a/1']);
    // The window narrows: the den's one level shows its channel, and the
    // list goes in its place, as nothing beneath it is a list.
    win.matchMedia = () => ({ matches: false });
    nav.den('a', '1', 'list');
    await settle();
    assert.deepEqual(win.paths(), ['/', '/den/a']);
    nav.den('a', '1', 'chan');
    await settle();
    nav.den('a', '1', 'list');
    await settle();
    assert.equal(win.at, 1, 'back to the list beneath');
    assert.deepEqual(win.paths(), ['/', '/den/a', '/den/a/1'], 'Forward returns to the channel');
});

test("Back closes a dialog, and the dialog's own closing takes its entry back", async () => {
    const win = fakeWindow('/den/a/1');
    const nav = createNav(win);
    nav.start();
    let closed = 0;
    nav.openLayer(() => closed++);
    await settle();
    assert.equal(win.entries.length, 4);
    assert.equal(win.location.pathname, '/den/a/1', 'a dialog keeps the address under it');
    win.back();
    await settle();
    assert.equal(closed, 1);
    assert.equal(paneOf(nav.trail), 'chan');
    assert.equal(win.at, 2);

    const id = nav.openLayer(() => closed++);
    await settle();
    assert.equal(win.at, 3);
    nav.closeLayer(id);
    await settle();
    assert.equal(win.at, 2, 'its entry taken back');
    assert.equal(closed, 1, 'not closed twice');
});

test('a dialog that refuses Back, as Manage with changes unsaved, stays with its entry', async () => {
    const win = fakeWindow('/');
    const nav = createNav(win);
    nav.start();
    let asked = 0;
    nav.openLayer(() => {
        asked++;
        return false;
    });
    await settle();
    win.back();
    await settle();
    assert.equal(asked, 1);
    assert.equal(win.at, 1, 'its entry back on');
    assert.equal(nav.trail.at(-1).k, 'layer');
});

test('a move from under a dialog closes it first, and one dialog in place of another takes its place', async () => {
    const win = fakeWindow('/den/a/1');
    const nav = createNav(win);
    nav.start();
    let closed = 0;
    const profile = nav.openLayer(() => closed++);
    await settle();
    // The profile's Message: the dialog closes as the DM opens.
    nav.closeLayer(profile);
    nav.den('a', '9', 'chan');
    await settle();
    assert.deepEqual(win.paths().slice(0, win.at + 1), ['/', '/den/a', '/den/a/9']);
    assert.equal(closed, 0, 'closed by the page, not by Back');

    const first = nav.openLayer(() => {});
    await settle();
    nav.closeLayer(first);
    nav.openLayer(() => {});
    await settle();
    assert.equal(win.at, 3);
    assert.equal(nav.trail.length, 4);
    assert.equal(nav.trail.at(-1).k, 'layer');
});

test('a reload keeps the trail, and drops the dialogs whose entries it finds', async () => {
    const trail = [HOME, list('a'), chan('a', '1'), side('a', '1'), { k: 'layer', id: 3 }];
    const win = fakeWindow('/den/a/1', { state: { trail } });
    // The entries beneath, as the page wrote them before the reload.
    win.entries = [1, 2, 3, 4, 5].map((n) => ({ state: { trail: trail.slice(0, n) }, path: pathOf(trail.slice(0, n)) }));
    win.at = 4;
    const nav = createNav(win);
    nav.start();
    await settle();
    assert.equal(win.at, 3);
    assert.deepEqual(nav.trail, trail.slice(0, 4));
});

test('Forward into a closed dialog steps back out of it', async () => {
    const win = fakeWindow('/');
    const nav = createNav(win);
    nav.start();
    nav.openLayer(() => {});
    await settle();
    win.back();
    await settle();
    win.history.go(1);
    await settle();
    await settle();
    assert.equal(win.at, 0);
    assert.deepEqual(nav.trail, [HOME]);
});

test("a fragment's load on the page, as the Android app's shell opens a notification, goes where it says", async () => {
    const win = fakeWindow('/den/a/1');
    const nav = createNav(win);
    nav.start();
    win.load('open=/den/b/7&message=8');
    assert.equal(paneOf(nav.trail), 'chan', 'the page stays until the hashchange says where');
    nav.open('/den/b/7');
    await settle();
    assert.deepEqual(win.paths().slice(0, win.at + 1), ['/', '/den/b', '/den/b/7']);
    assert.equal(win.location.hash, '');
});

test('an entry from before the page kept trails shows its address', async () => {
    const win = fakeWindow('/');
    const nav = createNav(win);
    nav.start();
    win.entries.unshift({ state: null, path: '/den/a/1' });
    win.at = 1;
    win.back();
    await settle();
    assert.deepEqual(nav.trail, [HOME, list('a'), chan('a', '1')]);
    assert.deepEqual(readTrail(win.entries[0].state), nav.trail, 'and keeps its trail from then on');
});

// Something the page plays full screen takes the screen's size, as a phone
// turned sideways does, and the layout holds under it (M13.4), so the
// share docked on the phone layout isn't taken away as it goes full
// screen.
test('the layout follows the window, but holds while the page plays something full screen', async () => {
    let wide = false;
    const changes = new Set();
    globalThis.window = { matchMedia: () => ({ get matches() { return wide; }, addEventListener: (_, fn) => changes.add(fn), removeEventListener: (_, fn) => changes.delete(fn) }) };
    const settle = () => new Promise((resolve) => setTimeout(resolve, 150));
    const resize = async (to) => {
        wide = to;
        changes.forEach((fn) => fn());
        await settle();
    };
    let narrow = null;
    function Layout() {
        narrow = useNarrow();
        return null;
    }
    mount(h(Layout), document.createElement('div'));
    await settle();
    assert.equal(narrow, true);
    await resize(true);
    assert.equal(narrow, false, 'a wider window');
    await resize(false);
    assert.equal(narrow, true);
    holdLayout(true);
    await resize(true);
    assert.equal(narrow, true, 'held, as full screen widens the window');
    holdLayout(false);
    await settle();
    assert.equal(narrow, false, 'back from full screen, the layout fits the window it finds');
    await resize(false);
    assert.equal(narrow, true);
    delete globalThis.window;
});
