// A DOM just large enough for Preact to render into, so a test can render a
// component with hooks, and render it again, without a browser: elements
// and text, their attributes, and the listeners Preact adds, which
// dispatch calls as the browser would.

class Node {
    constructor(nodeType, localName = null) {
        this.nodeType = nodeType;
        this.localName = localName;
        this.childNodes = [];
        this.parentNode = null;
        this.attributes = {};
        this.listeners = {};
        this.style = {};
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

    removeChild(node) {
        this.childNodes.splice(this.childNodes.indexOf(node), 1);
        node.parentNode = null;
        return node;
    }

    setAttribute(name, value) {
        this.attributes[name] = String(value);
    }

    removeAttribute(name) {
        delete this.attributes[name];
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
}

// Preact names a listener as the element names the event's handler, so
// these make it name them in lower case, as it does in a browser.
for (const handler of ['onchange', 'onclick', 'oninput', 'onkeydown', 'onpointerdown']) Node.prototype[handler] = null;

globalThis.document = {
    createElement: (name) => new Node(1, name),
    createElementNS: (namespace, name) => Object.assign(new Node(1, name), { namespaceURI: namespace }),
    createTextNode: (data) => Object.assign(new Node(3), { data: String(data) }),
};
