// Right-click menus, checked without a browser: whose menu a right
// click opens, the page's or the browser's; where a right click from the
// keyboard opens it; and where a menu opened at the pointer goes.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { placeMenu, ownsMenu, rightClick } from '../assets/js/src/menus.jsx';

// el makes an element of the DOM stand-in, with attributes and children.
function el(name, attrs = {}, ...children) {
    const node = document.createElement(name);
    for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
    for (const c of children) node.appendChild(c);
    return node;
}

// selection stands in for the window's: text selected, or none, and
// whether it reaches into a row.
const selection = (text, rows = []) => ({ isCollapsed: !text, toString: () => text, containsNode: (row) => rows.includes(row) });
const none = selection('');

test("a right click on a message or a member opens the page's menu, and on a link, a picture, a video, a player or a field the browser's", () => {
    const text = el('p');
    const link = el('a', { href: 'https://example.com/' }, el('span'));
    const photo = el('button', {}, el('img'));
    const avatar = el('span', { 'data-avatar': '' }, el('img'));
    const emoji = el('span', { 'data-emoji': 'party' }, el('img'));
    const video = el('video');
    const cover = el('div', { 'data-video': 'dQw4w9WgXcQ' }, el('button', {}, el('span')));
    const field = el('textarea');
    const name = el('button', {}, el('span'));
    const message = el('div', { 'data-id': '10' }, text, link, photo, avatar, emoji, video, cover, field, name);
    const own = (target) => ownsMenu(target, none, message);
    assert.equal(own(message), true, 'the row');
    assert.equal(own(text), true, "the message's text");
    assert.equal(own(name.firstChild), true, "its author's name");
    assert.equal(own(avatar.firstChild), true, "its author's picture");
    assert.equal(own(emoji.firstChild), true, 'an emoji in it');
    assert.equal(own(link.firstChild), false, 'a link');
    assert.equal(own(photo.firstChild), false, 'a photo');
    assert.equal(own(video), false, 'a video');
    assert.equal(own(cover.firstChild.firstChild), false, "a YouTube cover's button");
    assert.equal(own(field), false, 'the message box');
    assert.equal(own(Object.assign(el('div'), { isContentEditable: true })), false, 'anything editable');
    // A member's row in a list is a button with their picture in it.
    const member = el('button', {}, el('span', { 'data-avatar': '' }, el('img')), el('span'));
    assert.equal(ownsMenu(member.firstChild.firstChild, none, member), true, "a member's picture in the list");
    // A link around the row is the page's, not the row's.
    el('a', { href: '/den' }, member);
    assert.equal(ownsMenu(member.lastChild, none, member), true, 'what is outside the row stays out');
});

test("selected text in a message keeps the browser's menu, to copy it, and text selected elsewhere doesn't", () => {
    const text = el('p');
    const message = el('div', {}, text);
    const other = el('div');
    assert.equal(ownsMenu(text, selection('a few words', [message]), message), false, 'text selected in it');
    assert.equal(ownsMenu(text, selection('a few words', [other]), message), true, 'text selected in another message');
    assert.equal(ownsMenu(text, selection(' \n', [message]), message), true, 'a selection of nothing but spaces');
    assert.equal(ownsMenu(text, none, message), true);
});

test('a right click opens its menu at the pointer, and one whose point is outside its row where its ⋯ would', () => {
    const row = { getBoundingClientRect: () => ({ left: 100, top: 200, right: 400, bottom: 240 }) };
    assert.deepEqual(rightClick({ clientX: 150, clientY: 210 }, row), { x: 150, y: 210 });
    // The keyboard's menu key can send its event at the window's corner.
    assert.equal(rightClick({ clientX: 0, clientY: 0 }, row), null);
    assert.equal(rightClick({ clientX: 150, clientY: 260 }, row), null);
});

test("a menu opens below and right of the pointer, and turns where the list it's in has no room", () => {
    const clip = { left: 0, top: 50, right: 800, bottom: 600 };
    const size = { width: 200, height: 150 };
    assert.deepEqual(placeMenu({ x: 100, y: 100 }, size, clip), { left: 100, top: 100 }, 'room below and right');
    assert.deepEqual(placeMenu({ x: 700, y: 100 }, size, clip), { left: 500, top: 100 }, 'left of it, at the right edge');
    assert.deepEqual(placeMenu({ x: 100, y: 500 }, size, clip), { left: 100, top: 350 }, 'above it, at the foot');
    assert.deepEqual(placeMenu({ x: 700, y: 500 }, size, clip), { left: 500, top: 350 }, 'both, in the corner');
    // A list too small for it holds it at its top and left edges.
    assert.deepEqual(placeMenu({ x: 150, y: 120 }, size, { left: 100, top: 100, right: 300, bottom: 200 }), { left: 104, top: 104 });
});
