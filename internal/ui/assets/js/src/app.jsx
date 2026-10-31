// The app: pairing, the first local password, unlocking the Flatpak
// (M9.3), and the pages (home and a den's), chosen from what /api/status
// says about this browser, with the app's settings and a den's Manage
// dialog over them (M8.3). A den's page reaches home and the settings from
// its channel list; home has a button of its own for the settings. Which
// page shows, and which of a den's panes, is the page's history's (M13.1).

import { useCallback, useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { onConnection, onUnpaired } from './events.js';
import { Card, DocsTip, ErrorText, Field, PasswordFields, SubmitButton, TextInput, checkPasswords, useAction } from './components.jsx';
import { onOpenManage } from './dialogs.js';
import { Home } from './home.jsx';
import { ManageDialog } from './manageden.jsx';
import { SettingsDialog, onOpenSettings, openSettings } from './settings.jsx';
import { Chat } from './chat.jsx';
import { openAt } from './messages.jsx';
import { chatRoute, nav, paneOf, pathOf, useTrail } from './nav.js';
import { startNotifications } from './notify.js';
import { SharePlayer } from './player.jsx';
import { CallBar, CogIcon } from './voice.jsx';

// dens open opens the page with a one-time token in the URL fragment,
// which never reaches the server in the request line, and a notification's
// click with the message to open there too (M12.3), and the Android app's
// shell, on a page that shows, where to open it (M13.1). The page reads
// them before its history starts, which takes them out of the address.
function readFragment(loc = window.location) {
    if (!loc.hash.startsWith('#')) return { token: '', message: '', open: '' };
    const params = new URLSearchParams(loc.hash.slice(1));
    const message = /^[0-9]+$/.test(params.get('message') || '') ? params.get('message') : '';
    const open = chatRoute(params.get('open') || '') ? params.get('open') : '';
    return { token: params.get('token') || '', message, open };
}

// openThere opens a fragment's message: in the channel it names, or the
// channel the address names, which it opens.
function openThere({ message, open }) {
    const to = open || window.location.pathname;
    if (open) nav().open(open);
    const at = chatRoute(to);
    if (message && at?.channelID && at.channelID !== 'dms') openAt(at.denID, at.channelID, message);
}

// takeSettingsPath opens the settings over the home page for /settings,
// where they were a page of their own.
function takeSettingsPath() {
    if (window.location.pathname !== '/settings') return null;
    history.replaceState(null, '', '/');
    return 'appearance';
}

export function App({ instance, version, pair, pairLink }) {
    const [state, setState] = useState({ phase: 'loading' });
    const [settings, setSettings] = useState(takeSettingsPath);
    // Read before the history starts, which useTrail does.
    const [first] = useState(readFragment);
    const trail = useTrail();
    const [live, setLive] = useState(true);
    const [manage, setManage] = useState(null);
    useEffect(() => onOpenSettings(setSettings), []);
    useEffect(() => onOpenManage(setManage), []);
    const closeSettings = useCallback(() => setSettings(null), []);

    async function load() {
        try {
            const status = await api.get('/api/status');
            setState({ phase: 'ready', status });
        } catch (e) {
            if (e.status === 401) setState({ phase: 'unpaired' });
            else setState({ phase: 'failed', error: e.message });
        }
    }

    useEffect(() => {
        const { token } = first;
        openThere(first);
        (async () => {
            if (token) {
                try {
                    await api.post('/api/pair', { token });
                } catch (e) {
                    setState({ phase: 'unpaired', error: e.message });
                    return;
                }
            }
            await load();
        })();
        // A pairing link pasted into a tab already on this page changes only
        // the fragment, which doesn't load the page again, as does the
        // Android app's shell opening a notification's message on the page
        // it shows.
        const onHash = async () => {
            const fragment = readFragment();
            const next = fragment.token;
            if (fragment.open || fragment.message) openThere(fragment);
            if (!next) return;
            // A token works once: the reload mustn't find it again.
            history.replaceState(history.state, '', window.location.pathname);
            try {
                await api.post('/api/pair', { token: next });
                window.location.reload();
            } catch (e) {
                setState({ phase: 'unpaired', error: e.message });
            }
        };
        window.addEventListener('hashchange', onHash);
        const stopUnpaired = onUnpaired(() => setState({ phase: 'unpaired' }));
        return () => {
            window.removeEventListener('hashchange', onHash);
            stopUnpaired();
        };
    }, []);

    // navigate goes to the screen an address names, as a link to it does.
    const navigate = (to) => nav().open(to);

    const ready = state.phase === 'ready';
    const locked = ready && state.status.locked;
    // open is Dens set up and unlocked, which everything but this page's
    // first screens needs.
    const passwordSet = ready && state.status.passwordSet;
    const open = passwordSet && !locked;
    const chat = open ? chatRoute(pathOf(trail)) : null;
    // The event stream needs a paired browser, so it starts only then, and
    // with it the notifications this page shows (M12.3), whose click opens
    // their message.
    useEffect(() => (open ? onConnection(setLive) : undefined), [open]);
    useEffect(() => {
        if (!open) return;
        startNotifications((den, channel, message) => {
            openAt(den, channel, message);
            navigate(`/den/${den}/${channel}`);
        });
    }, [open]);
    // An update installed meanwhile, as in the software center, shows once
    // the member is back on the page, and within a minute while they stay
    // (M9.3): the service asks the newest build installed as the page asks
    // for its status (M9.4).
    useEffect(() => {
        if (!open) return undefined;
        let last = Date.now();
        const refresh = () => {
            if (document.visibilityState !== 'visible' || Date.now() - last < 10000) return;
            last = Date.now();
            api.get('/api/status').then((status) => setState({ phase: 'ready', status }), () => {});
        };
        document.addEventListener('visibilitychange', refresh);
        const timer = setInterval(refresh, 60000);
        return () => {
            document.removeEventListener('visibilitychange', refresh);
            clearInterval(timer);
        };
    }, [open]);
    // The keyring may open a locked Flatpak while the page waits for the
    // password.
    useEffect(() => {
        if (!locked) return undefined;
        const timer = setInterval(async () => {
            try {
                const status = await api.get('/api/status');
                if (!status.locked) setState({ phase: 'ready', status });
            } catch {
                // The next try says.
            }
        }, 2000);
        return () => clearInterval(timer);
    }, [locked]);
    let page;
    if (state.phase === 'loading') {
        page = <span class="loading loading-spinner"></span>;
    } else if (state.phase === 'failed') {
        page = <ErrorText message={state.error} />;
    } else if (state.phase === 'unpaired') {
        page = <Unpaired pair={pair} pairLink={pairLink} error={state.error} />;
    } else if (!passwordSet) {
        page = <SetPassword canRemember={'remember' in state.status} onDone={load} />;
    } else if (locked) {
        page = <Unlock remembered={!!state.status.remember?.on} onDone={load} />;
    } else {
        page = <Home status={state.status} navigate={navigate} />;
    }

    return (
        <div class="flex h-screen flex-col">
            {open && !live && (
                <div role="status" class="alert alert-warning alert-soft shrink-0 rounded-none">
                    <span>Lost touch with the Dens service on this computer; reconnecting…</span>
                </div>
            )}
            {chat ? (
                <div class="min-h-0 flex-1">
                    <Chat key={chat.denID} denID={chat.denID} channelID={chat.channelID} pane={paneOf(trail)} navigate={navigate} />
                </div>
            ) : (
                <main class="min-h-0 flex-1 overflow-y-auto">
                    <div class="mx-auto flex max-w-2xl flex-col gap-4 p-6 pb-20">
                        {open && <CallBar />}
                        {page}
                    </div>
                    {open && (
                        <a href="/settings" class="btn fixed bottom-4 left-4 shadow-sm" aria-label="App settings"
                            onClick={(e) => { e.preventDefault(); openSettings('appearance'); }}>
                            <CogIcon size="h-5 w-5" />
                            App settings
                        </a>
                    )}
                </main>
            )}
            {/* The shares this member watches play over every page (M4.2). */}
            {open && <SharePlayer />}
            {open && settings && (
                <SettingsDialog section={settings} onSection={setSettings} onClose={closeSettings} instance={instance} version={version}
                    pair={pair} />
            )}
            {open && manage && (
                <ManageDialog key={manage.denID} denID={manage.denID} section={manage.section} navigate={navigate}
                    onSection={(section) => setManage((m) => ({ ...m, section }))} onClose={() => setManage(null)} />
            )}
        </div>
    );
}

// Unpaired says how to pair this browser: with dens open, or, in the
// Flatpak, which has no command for it, by opening Dens from the apps
// menu, either of which opens the default browser; and in another, with the
// link dens open --print prints. The Android app, with neither, pairs its
// own page as it opens it (M12).
function Unpaired({ pair, pairLink, error }) {
    return (
        <Card title="Pair this browser">
            {pair ? (
                <>
                    <p>This page only works in a browser paired with Dens on this computer. Run this in a terminal:</p>
                    <pre class="rounded bg-base-300 p-3"><code>{pair}</code></pre>
                    <p class="text-sm text-base-content/70">It opens this page again in your default browser, with a one-time link.</p>
                </>
            ) : pairLink ? (
                <>
                    <p>This page only works in a browser paired with Dens on this computer. Open Dens from your apps menu.</p>
                    <p class="text-sm text-base-content/70">It opens this page again in your default browser, with a one-time link.</p>
                </>
            ) : (
                <p>This page isn't paired with Dens anymore. Close Dens, then open it again from your apps, which pairs it.</p>
            )}
            {pairLink && (
                <>
                    <p>In another browser, open the link this prints:</p>
                    <pre class="rounded bg-base-300 p-3"><code>{pairLink}</code></pre>
                </>
            )}
            <ErrorText message={error} />
        </Card>
    );
}

// Remember is the choice the Flatpak offers with the local password
// (M9.3): to keep a copy of its key that opens Dens without the password,
// so it starts unlocked, and at sign-in.
function Remember({ checked, onChange }) {
    return (
        <label class="label cursor-pointer justify-start gap-2 whitespace-normal">
            <input type="checkbox" class="checkbox checkbox-sm" checked={checked} onChange={(e) => onChange(e.currentTarget.checked)} />
            <span class="text-sm text-base-content">
                Remember on this computer
                <span class="block text-xs text-base-content/70">
                    Dens opens without your local password, and when you sign in. Programs you run could open it too.
                </span>
            </span>
        </label>
    );
}

// Unlock opens the Flatpak, which starts locked unless it remembers its
// key (M9.3), with the local password.
function Unlock({ remembered, onDone }) {
    const [password, setPassword] = useState('');
    const [remember, setRemember] = useState(remembered);
    const { busy, error, run } = useAction();

    function submit(e) {
        e.preventDefault();
        run(async () => {
            await api.post('/api/unlock', { password, remember });
            await onDone();
        });
    }

    return (
        <Card title="Unlock Dens">
            <p>Dens is locked on this computer. Your local password opens it, and your dens connect once it's open.</p>
            <form class="flex flex-col gap-2" onSubmit={submit}>
                <Field label="Local password" hint="The one that protects Dens on this computer, not a den password.">
                    <TextInput type="password" value={password} onInput={setPassword} autocomplete="current-password" required autofocus />
                </Field>
                <Remember checked={remember} onChange={setRemember} />
                <ErrorText message={error} />
                <SubmitButton busy={busy}>Unlock</SubmitButton>
            </form>
        </Card>
    );
}

function SetPassword({ canRemember, onDone }) {
    const [password, setPassword] = useState('');
    const [confirm, setConfirm] = useState('');
    const [remember, setRemember] = useState(false);
    const { busy, error, setError, run } = useAction();

    function submit(e) {
        e.preventDefault();
        const problem = checkPasswords(password, confirm);
        if (problem) return setError(problem);
        run(async () => {
            await api.post('/api/password', { password, remember });
            await onDone();
        });
    }

    return (
        <Card title={<>Set a local password <DocsTip id="docs-local-password" page="join/#your-passwords-and-keys">
            How it differs from your den passwords, and what each one is for.</DocsTip></>}>
            <p>
                The local password protects your backups and your keys. You'll need it to make a backup, restore one on another
                machine, or view your keys.
            </p>
            <p class="text-sm text-base-content/70">
                Nobody can recover it for you. If you lose both this password and this computer, your local data is gone; your den
                accounts can still be recovered with their recovery codes.
            </p>
            <form class="flex flex-col gap-2" onSubmit={submit}>
                <PasswordFields password={password} setPassword={setPassword} confirm={confirm} setConfirm={setConfirm} />
                {canRemember && <Remember checked={remember} onChange={setRemember} />}
                <ErrorText message={error} />
                <SubmitButton busy={busy}>Set password</SubmitButton>
            </form>
        </Card>
    );
}
