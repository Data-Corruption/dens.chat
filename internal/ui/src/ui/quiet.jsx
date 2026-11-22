// Do not disturb's moon, beside the settings' cog in the member's panel
// and in Home's header. It opens Do not disturb's ends, with Turn off
// while it's on, when the moon is lit and its tip says until when.

import { useEffect, useState } from 'preact/hooks';
import { useNotifications } from './components.jsx';
import { MoonIcon } from './icons.jsx';
import { MenuButton } from './menus.jsx';
import { QUIET_ENDS, quiet, setQuiet, untilWords } from '../core/notify.js';

/** useQuiet is the notifications' status and how Do not disturb stands by
    it, read again as its end passes and as the page comes back on
    screen, since the service sends nothing at the end */
export function useQuiet() {
    const status = useNotifications();
    const [, setTick] = useState(0);
    const q = quiet(status);
    useEffect(() => {
        const again = () => setTick((n) => n + 1);
        document.addEventListener('visibilitychange', again);
        // A timer longer than 2^31 milliseconds fires at once.
        const timer = q.until !== null ? setTimeout(again, Math.min(q.until - Date.now() + 100, 2 ** 31 - 1)) : null;
        return () => {
            document.removeEventListener('visibilitychange', again);
            clearTimeout(timer);
        };
    }, [q.on, q.until]);
    return { status, ...q };
}

/** QuietButton is the moon, its button's class style */
export function QuietButton({ class: style = 'btn btn-ghost btn-sm btn-square' }) {
    const { status, on, until } = useQuiet();
    if (!status) return null;
    const items = [...(on ? [{ key: 'none', label: 'Turn off', icon: 'bell' }] : []), ...QUIET_ENDS]
        .map((e) => ({ ...e, run: () => setQuiet(e.key === 'none' ? null : e.key).catch(() => {}) }));
    const words = on ? `Do not disturb ${untilWords(until)}` : 'Do not disturb';
    return (
        <MenuButton label={words} title="Do not disturb" items={items}
            button={{ class: `${style} ${on ? 'text-warning' : ''}`, title: words, 'data-quiet': on ? 'on' : 'off', children: <MoonIcon on={on} /> }} />
    );
}
