// The message box's height: its text's, wrapped lines too, from a
// line's up to half of its pane's room, past which it scrolls.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { boxHeight } from '../assets/js/src/messages.jsx';

test("the message box takes its text's height, from a line's to half of its pane's room", () => {
    assert.equal(boxHeight(45, 800), 48, 'a line, or none, keeps the bar at 73 pixels');
    assert.equal(boxHeight(66, 800), 66, 'a second line, wrapped or broken');
    assert.equal(boxHeight(108.4, 800), 109, 'a line laid out at a fraction is never cut');
    assert.equal(boxHeight(900, 800), 400, 'half the pane, past which it scrolls');
    assert.equal(boxHeight(900, 801), 400);
    assert.equal(boxHeight(300, 360), 180, "less room where a phone's keyboard covers the pane");
    assert.equal(boxHeight(300, 60), 48, 'a line even in a pane too short for one');
});
