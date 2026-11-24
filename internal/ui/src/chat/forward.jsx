// Forward: a message's text and files sent to another place, in its den
// or another this install is in, as the member's own, marked forwarded,
// with nothing of where it came from. The member's Dens reads and sends the
// files, which takes a while for a video.

import { useRef, useState } from 'preact/hooks';
import { api } from '../core/api.js';
import { newNonce } from '../core/ids.js';
import { forwardList } from '../core/switcher.js';
import { PlacePicker } from '../ui/switcher.jsx';

// placeName is how a place reads in the line that says a forward went
// there.
export const placeName = (e) => (e.kind === 'text' ? `#${e.name}` : e.name);

// Forward picks where a message goes, and sends it there; onDone hears
// where, once it went. The member's notes are made as they're picked, if
// they weren't yet.
export function Forward({ denID, channelID, m, onClose, onDone }) {
    const [busy, setBusy] = useState('');
    const [error, setError] = useState('');
    // Each place keeps its nonce, so sending again after a lost answer
    // doesn't post twice.
    const nonces = useRef(new Map());
    async function pick(e) {
        setBusy(e.key);
        setError('');
        try {
            const place = e.id || (await api.post(`/api/dens/${e.den}/notes`)).id;
            if (!nonces.current.has(e.key)) nonces.current.set(e.key, newNonce());
            await api.post(`/api/dens/${denID}/messages/${m.id}/forward?channel=${channelID}`, { den: e.den, channel: place, nonce: nonces.current.get(e.key) });
            onDone(e);
        } catch (err) {
            setError(err.message);
            setBusy('');
        }
    }
    return (
        <PlacePicker label="Forward to" placeholder="Where to?" list={forwardList} data="forward" busy={busy} error={error} onPick={pick} onClose={onClose}
            empty={(q) => (q ? 'Nothing matches.' : 'Nowhere to forward this.')} />
    );
}
