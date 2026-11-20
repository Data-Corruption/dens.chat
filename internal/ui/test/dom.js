// A DOM just large enough for Preact to render into, so a test can render a
// component with hooks, and render it again, without a browser: elements
// and text, their attributes, and the listeners Preact adds, which
// dispatch calls as the browser would.
//
// What it leaves out, a test makes up for. With no requestAnimationFrame,
// Preact runs effects about 100 ms after a render, so a test waits for what
// an effect does. A component that reaches for window, as useLayer and
// nav() do, needs a globalThis.window of the test's. textContent holds
// hidden text too, with no CSS to hide it. A test that leaves a socket or a
// timer running never ends, and Node has a WebSocket of its own, through
// which the page's event stream reconnects for ever, so a test that mounts
// the page stubs WebSocket, location and history.

class Node {
    constructor(nodeType, localName = null) {
        this.nodeType = nodeType;
        this.localName = localName;
        this.childNodes = [];
        this.parentNode = null;
        this.attributes = {};
        this.listeners = {};
        // Preact sets a custom property, as --name-by, through setProperty,
        // and the page takes a theme's off through removeProperty.
        this.style = Object.defineProperties({}, {
            setProperty: { value(name, value) { this[name] = value; } },
            removeProperty: { value(name) { delete this[name]; } },
        });
    }

    get firstChild() {
        return this.childNodes[0] ?? null;
    }

    get nextSibling() {
        const siblings = this.parentNode?.childNodes;
        return siblings?.[siblings.indexOf(this) + 1] ?? null;
    }

    get textContent() {
        return this.nodeType === 3 ? this.data : this.childNodes.map((c) => c.textContent).join('');
    }

    insertBefore(node, before) {
        node.parentNode?.removeChild(node);
        const at = before ? this.childNodes.indexOf(before) : -1;
        this.childNodes.splice(at < 0 ? this.childNodes.length : at, 0, node);
        node.parentNode = this;
        return node;
    }

    appendChild(node) {
        return this.insertBefore(node, null);
    }

    remove() {
        this.parentNode?.removeChild(this);
    }

    removeChild(node) {
        this.childNodes.splice(this.childNodes.indexOf(node), 1);
        node.parentNode = null;
        return node;
    }

    setAttribute(name, value) {
        this.attributes[name] = String(value);
    }

    getAttribute(name) {
        return name in this.attributes ? this.attributes[name] : null;
    }

    removeAttribute(name) {
        delete this.attributes[name];
    }

    hasAttribute(name) {
        return name in this.attributes;
    }

    addEventListener(type, listener) {
        this.listeners[type] = listener;
    }

    removeEventListener(type) {
        delete this.listeners[type];
    }

    // dispatch calls the listener for type with an event made of init.
    dispatch(type, init = {}) {
        return this.listeners[type].call(this, { type, currentTarget: this, ...init });
    }

    // find returns the first element at or under this one that test
    // accepts, depth first.
    find(test) {
        if (this.nodeType === 1 && test(this)) return this;
        for (const child of this.childNodes) {
            const found = child.find(test);
            if (found) return found;
        }
        return null;
    }

    // A menu's flyout places itself by its item's box, which a page
    // without a layout has none of.
    getBoundingClientRect() {
        return { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 };
    }

    // A sheet's effect looks for a row to focus, and may run after its
    // test ended; it finds none.
    querySelector() {
        return null;
    }

    querySelectorAll() {
        return [];
    }

    // The composer measures itself against the pane it's in, by its
    // selector, which no element matches here, and takes the focus.
    closest() {
        return null;
    }

    focus() {}
}

// Preact names a listener as the element names the event's handler, so
// these make it name them in lower case, as it does in a browser; one left
// out is named as the component writes it, onSubmit's as Submit.
for (const handler of ['onchange', 'onclick', 'oninput', 'onkeydown', 'onload', 'onpointerdown', 'onpointerup', 'onpointerenter', 'onpointerleave']) Node.prototype[handler] = null;

globalThis.document = {
    documentElement: new Node(1, 'html'),
    createElement: (name) => new Node(1, name),
    createElementNS: (namespace, name) => Object.assign(new Node(1, name), { namespaceURI: namespace }),
    createTextNode: (data) => Object.assign(new Node(3), { data: String(data) }),
};
