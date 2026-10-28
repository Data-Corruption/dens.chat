import assert from 'node:assert/strict';
import { test } from 'node:test';
import { copyText } from '../assets/js/src/clipboard.js';

// page is a document and navigator with only what copying touches: a
// Clipboard API that copies or refuses, and execCommand('copy'), which
// copies what's selected, or refuses.
function page({ api, command }) {
    const copied = [];
    const body = { children: [], appendChild(n) { this.children.push(n); }, removeChild(n) { this.children.splice(this.children.indexOf(n), 1); } };
    let selected = null;
    let focusedAgain = 0;
    const button = { focus: () => focusedAgain++ };
    const doc = {
        body,
        activeElement: button,
        createElement: () => ({
            style: {},
            attributes: {},
            setAttribute(name, value) { this.attributes[name] = value; },
            select() { selected = this.value; },
            setSelectionRange() {},
        }),
        execCommand(name) {
            assert.equal(name, 'copy');
            if (!command) return false;
            copied.push(selected);
            return true;
        },
    };
    const nav = api === undefined ? {} : {
        clipboard: {
            writeText: async (text) => {
                if (!api) throw new DOMException('Write permission denied.', 'NotAllowedError');
                copied.push(text);
            },
        },
    };
    return { doc, nav, copied, body, focusedAgain: () => focusedAgain };
}

test('the Clipboard API copies where the browser allows it', async () => {
    const p = page({ api: true, command: true });
    await copyText('a link', p.doc, p.nav);
    assert.deepEqual(p.copied, ['a link']);
    assert.equal(p.focusedAgain(), 0, 'nothing was selected');
});

// Android's WebView refuses the Clipboard API even after a tap, and copies
// what's selected (M12).
test('where the Clipboard API is refused, or missing, the old way copies', async () => {
    for (const api of [false, undefined]) {
        const p = page({ api, command: true });
        await copyText('a link', p.doc, p.nav);
        assert.deepEqual(p.copied, ['a link']);
        assert.equal(p.body.children.length, 0, 'the text area went again');
        assert.equal(p.focusedAgain(), 1, 'what had the focus has it again');
    }
});

test('where neither copies, copying says so', async () => {
    const p = page({ api: false, command: false });
    await assert.rejects(copyText('a link', p.doc, p.nav), /didn't let Dens copy/);
    assert.equal(p.body.children.length, 0);
});
