import assert from 'node:assert/strict';
import { test } from 'node:test';
import { avatarColor, avatarLetter } from '../assets/js/src/avatar.jsx';
import { typingLine } from '../assets/js/src/messages.jsx';
import { rank } from '../assets/js/src/people.jsx';

test("an avatar's color depends only on the member's ID", () => {
    assert.equal(avatarColor('1000123'), avatarColor('1000123'));
    const colors = new Set();
    for (let id = 1; id <= 50; id++) {
        const c = avatarColor(String(id));
        assert.match(c, /^bg-[a-z]+-\d00$/);
        colors.add(c);
    }
    assert.ok(colors.size >= 8, `only ${colors.size} colors across 50 members`);
});

test("an avatar's letter is the username's first letter or digit", () => {
    assert.equal(avatarLetter('bob'), 'B');
    assert.equal(avatarLetter('_x1'), 'X');
    assert.equal(avatarLetter('7up'), '7');
    assert.equal(avatarLetter('__'), '?');
    assert.equal(avatarLetter(undefined), '?');
});

test('the typing line names up to three people', () => {
    assert.equal(typingLine([]), '');
    assert.equal(typingLine(['Ann']), 'Ann is typing…');
    assert.equal(typingLine(['Ann', 'Bo']), 'Ann and Bo are typing…');
    assert.equal(typingLine(['Ann', 'Bo', 'Cy']), 'Ann, Bo and Cy are typing…');
    assert.equal(typingLine(['Ann', 'Bo', 'Cy', 'Di']), 'Several people are typing…');
});

test('roles rank as the den ranks them', () => {
    assert.ok(rank('owner') > rank('moderator'));
    assert.ok(rank('moderator') > rank('member'));
    assert.equal(rank(undefined), rank('member'));
});
