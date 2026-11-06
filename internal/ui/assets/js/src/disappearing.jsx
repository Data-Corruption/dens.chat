// Disappearing messages (M16.4): what the page shows of a DM's or a
// group's time, beside it and in it. Any of its members sets how long its
// messages stay after they're sent, for those sent from then on; the den
// deletes each as its time comes, and every member's Dens what it holds.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { DocsLink, ErrorText, useAction } from './components.jsx';
import { TimerIcon } from './icons.jsx';
import { Dialog } from './manage.jsx';
import { EXPIRY_TIMES, afterText, expiryText, shortAfter } from './private.js';

// ExpirySetting sets a DM's or a group's time, beside it, where its other
// member's profile or its members show; a pick waits on the member's word,
// since it applies to everyone in it. A time the list lacks, as a
// development instance's den takes for the browser checks, shows as it is.
export function ExpirySetting({ denID, channel }) {
    const after = channel.expires_after || 0;
    // asking is the time picked, while the member says whether to set it;
    // set, the one set, until the den's word on it comes as the DM's or
    // group's next state.
    const [asking, setAsking] = useState(null);
    const [set, setSet] = useState(null);
    useEffect(() => setSet(null), [after, channel.expires_set?.at]);
    const times = EXPIRY_TIMES.includes(after) ? EXPIRY_TIMES : [...EXPIRY_TIMES, after].sort((a, b) => a - b);
    const shown = asking ?? set ?? after;
    return (
        <>
            <label class="flex flex-col gap-1" data-expiry-setting>
                <span class="flex items-center gap-1 text-sm font-medium"><TimerIcon /> Disappearing messages</span>
                <select class="select select-sm w-full" value={shown}
                    onChange={(e) => {
                        const value = Number(e.currentTarget.value);
                        if (value !== (set ?? after)) setAsking(value);
                    }}>
                    {times.map((t) => <option key={t} value={t}>{t ? afterText(t) : 'Off'}</option>)}
                </select>
            </label>
            {asking !== null && (
                <ExpiryConfirm denID={denID} channel={channel} from={set ?? after} to={asking}
                    onDone={() => {
                        setSet(asking);
                        setAsking(null);
                    }}
                    onClose={() => setAsking(null)} />
            )}
        </>
    );
}

// ExpiryConfirm asks before a DM's or a group's time changes, saying what
// the new one does, with the way to the guide.
export function ExpiryConfirm({ denID, channel, from, to, onDone, onClose }) {
    const act = useAction();
    const confirm = useRef(null);
    useEffect(() => confirm.current?.focus?.(), []);
    const [title, action] = !to ? ['Turn off disappearing messages?', 'Turn off']
        : !from ? ['Turn on disappearing messages?', 'Turn on'] : ['Change disappearing messages?', 'Change'];
    return (
        <Dialog title={title} onClose={onClose}>
            <p class="text-sm" data-expiry-confirm>
                {to ? `New messages disappear for everyone ${afterText(to)} after they're sent.` : 'New messages stay.'}{' '}
                <DocsLink page="join/#disappearing-messages" label="How it works" />
            </p>
            <ErrorText message={act.error} />
            <div class="flex gap-2">
                <button ref={confirm} type="button" class="btn btn-primary btn-sm" disabled={act.busy}
                    onClick={() => act.run(async () => {
                        await api.put(`/api/dens/${denID}/dms/${channel.id}/expiry`, { expires_after: to });
                        onDone();
                    })}>
                    {act.busy && <span class="loading loading-spinner loading-sm"></span>}
                    {action}
                </button>
                <button type="button" class="btn btn-ghost btn-sm" onClick={onClose}>Cancel</button>
            </div>
        </Dialog>
    );
}

// ExpiryLine says who last set a DM's or a group's time, as a line among
// its messages, at the time they set it.
export function ExpiryLine({ channel, nameOf, me }) {
    return (
        <div class="divider my-1 h-auto cursor-default select-none text-xs text-base-content/60" data-expiry-line>
            <span class="flex max-w-[80%] shrink-0 items-center justify-center gap-1 whitespace-normal text-center">
                <TimerIcon size="h-3 w-3" />
                {expiryText(channel.expires_after || 0, channel.expires_set, nameOf, me)}
            </span>
        </div>
    );
}

// ExpiryBadge says in a DM's or a group's header that its messages
// disappear, and after how long, and opens what's beside it, where the
// time is set.
export function ExpiryBadge({ after, onClick }) {
    const said = `Messages disappear ${afterText(after)} after they're sent`;
    return (
        <button type="button" class="btn btn-ghost btn-xs shrink-0 gap-1 font-normal text-base-content/60" onClick={onClick} title={said}
            aria-label={`${said}: change it`} data-expiry-badge>
            <TimerIcon size="h-3 w-3" />
            {shortAfter(after)}
        </button>
    );
}
