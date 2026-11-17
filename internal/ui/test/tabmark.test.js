// The browser tab's icon: a red dot while a DM or a mention waits in any
// den, checked without a browser.

import './dom.js';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { markTab, waiting } from '../assets/js/src/tabmark.js';

test("the tab's icon takes its dot while any den holds a DM or a mention unread, and loses it once none does", () => {
    const read = 'data:image/svg+xml,mark';
    const link = document.createElement('link');
    link.setAttribute('rel', 'icon');
    link.setAttribute('href', read);
    link.setAttribute('data-unread', 'data:image/svg+xml,dotted');
    const dens = (...marks) => marks.map(([mentions, unread], i) => ({ den_id: `d${i}`, mentions, unread }));
    const shown = (list) => {
        markTab(link, read, waiting(list));
        return link.getAttribute('href');
    };
    assert.equal(shown(dens([0, 0], [0, 0])), read, 'nothing waits');
    assert.equal(shown(dens([0, 3], [0, 1])), read, "a channel's messages alone, unread, don't mark it");
    assert.equal(shown(dens([0, 0], [1, 1])), 'data:image/svg+xml,dotted', 'a DM in the second den, which its status counts among mentions');
    assert.equal(shown(dens([2, 2], [1, 1])), 'data:image/svg+xml,dotted', 'mentions in both');
    assert.equal(shown(dens([0, 0], [0, 0])), read, 'read, it goes');
    assert.equal(waiting(undefined), false);
});
