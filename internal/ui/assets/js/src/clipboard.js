// Copying to the clipboard: through the Clipboard API where the browser
// allows it, and otherwise the old way, which Android's WebView allows
// while it refuses the API, even after a tap (M12).

// copyText puts text on the clipboard, or throws saying it couldn't. doc
// and nav are the page's, or stand-ins in the tests.
export async function copyText(text, doc = globalThis.document, nav = globalThis.navigator) {
    try {
        await nav.clipboard.writeText(text);
        return;
    } catch {
        // Refused, or no Clipboard API at all: the old way may still work.
    }
    // A selected text area copies, read-only so a phone shows no keyboard
    // for it, out of sight, and gone again at once.
    const area = doc.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.setAttribute('aria-hidden', 'true');
    area.style.position = 'fixed';
    area.style.top = '0';
    area.style.opacity = '0';
    const focused = doc.activeElement;
    doc.body.appendChild(area);
    let copied = false;
    try {
        area.select();
        area.setSelectionRange(0, text.length);
        copied = doc.execCommand('copy');
    } catch {
        copied = false;
    } finally {
        doc.body.removeChild(area);
        focused?.focus?.();
    }
    if (!copied) throw new Error("The browser didn't let Dens copy that.");
}
