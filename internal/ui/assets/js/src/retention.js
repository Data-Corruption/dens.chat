// How long a den keeps messages (M5), in the words the page uses for it.

import { formatSize } from './files.jsx';

const count = (n, one, many) => (n === 1 ? `1 ${one}` : `${n.toLocaleString('en-US')} ${many}`);

// keptFor says a retention period: "1 day", "30 days".
export function keptFor(days) {
    return count(days, 'day', 'days');
}

// deletesNow says what saving a period would delete at once, from the
// den's preview of it.
export function deletesNow(preview) {
    if (!preview.messages) return 'No messages are that old yet, so saving deletes nothing now.';
    const files = preview.bytes ? `, and ${formatSize(preview.bytes)} of files` : '';
    return `Saving this deletes ${count(preview.messages, 'message', 'messages')} now${files}.`;
}
