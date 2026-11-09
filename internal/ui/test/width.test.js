// Appearance's Width (M19.1), checked without a browser: the browser's
// choice, Full until the member makes one, and the classes each choice
// puts on the header's, the list's and the foot's content.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { WIDTH_FULL, WIDTH_NARROW, WIDTH_WIDE, setWidth, width, widthClass } from '../assets/js/src/width.js';

test('messages run Full until the member picks Wide or Narrow, which the browser keeps', () => {
    assert.equal(width(), WIDTH_FULL);
    setWidth(WIDTH_NARROW);
    assert.equal(width(), WIDTH_NARROW);
    setWidth(WIDTH_WIDE);
    assert.equal(width(), WIDTH_WIDE);
    setWidth('anything else');
    assert.equal(width(), WIDTH_FULL);
});

test('Wide and Narrow center the content at 1,200 or 900 pixels, and Full adds nothing', () => {
    assert.equal(widthClass(WIDTH_FULL), '');
    assert.match(widthClass(WIDTH_WIDE), /\bmx-auto\b/);
    assert.match(widthClass(WIDTH_WIDE), /\bmax-w-\[1200px\]/);
    assert.match(widthClass(WIDTH_NARROW), /\bmx-auto\b/);
    assert.match(widthClass(WIDTH_NARROW), /\bmax-w-\[900px\]/);
    assert.equal(widthClass('anything else'), '');
});
