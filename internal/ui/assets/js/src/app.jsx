// The app: pairing, the first local password, and the pages (home and a
// den's), chosen from what /api/status says about this browser, with the
// settings over them. A den's page reaches home and the settings from its
// channel list; home has a cog of its own for the settings.

import { useCallback, useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { onConnection, onUnpaired } from './events.js';
import { Card, ErrorText, PasswordFields, SubmitButton, checkPasswords, useAction } from './components.jsx';
import { Home } from './home.jsx';
import { SettingsDialog, onOpenSettings, openSettings } from './settings.jsx';
import { Chat } from './chat.jsx';
import { SharePlayer } from './player.jsx';
import { CallBar, CogIcon } from './voice.jsx';

// chatRoute reads /den/<den>[/<channel>] from a path.
function chatRoute(path) {
    const m = path.match(/^\/den\/([A-Za-z0-9_-]+)(?:\/([0-9]+))?\/?$/);
    return m ? { denID: m[1], channelID: m[2] || '' } : null;
}

// dens open opens the page with a one-time token in the URL fragment,
// which never reaches the server in the request line. The page takes it,
// removes it from the address bar and history, and redeems it once.
function takePairingToken() {
    const hash = window.location.hash;
    if (!hash.startsWith('#')) return '';
    const token = new URLSearchParams(hash.slice(1)).get('token') || '';
    if (token) history.replaceState(null, '', window.location.pathname);
    return token;
}

// takeSettingsPath opens the settings over the home page for /settings,
// where they were a page of their own.
function takeSettingsPath() {
    if (window.location.pathname !== '/settings') return null;
    history.replaceState(null, '', '/');
    return 'general';
}

export function App({ instance, version }) {
    const [state, setState] = useState({ phase: 'loading' });
    const [settings, setSettings] = useState(takeSettingsPath);
    const [path, setPath] = useState(window.location.pathname);
    const [live, setLive] = useState(true);
    useEffect(() => onOpenSettings(setSettings), []);
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
        const token = takePairingToken();
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
        const onPop = () => setPath(window.location.pathname);
        window.addEventListener('popstate', onPop);
        // A pairing link pasted into a tab already on this page changes only
        // the fragment, which doesn't load the page again.
        const onHash = async () => {
            const next = takePairingToken();
            if (!next) return;
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
            window.removeEventListener('popstate', onPop);
            window.removeEventListener('hashchange', onHash);
            stopUnpaired();
        };
    }, []);

    function navigate(to) {
        if (to === window.location.pathname) return;
        history.pushState(null, '', to);
        setPath(to);
    }

    const passwordSet = state.phase === 'ready' && state.status.passwordSet;
    const chat = passwordSet ? chatRoute(path) : null;
    // The event stream needs a paired browser, so it starts only then.
    useEffect(() => (passwordSet ? onConnection(setLive) : undefined), [passwordSet]);
    let page;
    if (state.phase === 'loading') {
        page = <span class="loading loading-spinner"></span>;
    } else if (state.phase === 'failed') {
        page = <ErrorText message={state.error} />;
    } else if (state.phase === 'unpaired') {
        page = <Unpaired instance={instance} error={state.error} />;
    } else if (!passwordSet) {
        page = <SetPassword onDone={load} />;
    } else {
        page = <Home status={state.status} navigate={navigate} />;
    }

    return (
        <div class="flex h-screen flex-col">
            {passwordSet && !live && (
                <div role="status" class="alert alert-warning alert-soft shrink-0 rounded-none">
                    <span>Lost touch with the Dens service on this computer; reconnecting…</span>
                </div>
            )}
            {chat ? (
                <div class="min-h-0 flex-1">
                    <Chat key={chat.denID} denID={chat.denID} channelID={chat.channelID} navigate={navigate} />
                </div>
            ) : (
                <main class="min-h-0 flex-1 overflow-y-auto">
                    <div class="mx-auto flex max-w-2xl flex-col gap-4 p-6 pb-20">
                        {passwordSet && <CallBar />}
                        {page}
                        {version && (
                            <p class="text-center text-xs text-base-content/50">
                                Dens {version}
                                {instance !== 'main' && `, instance ${instance}`}
                            </p>
                        )}
                    </div>
                    {passwordSet && (
                        <a href="/settings" class="btn btn-square fixed bottom-4 left-4 shadow-sm" aria-label="Settings" title="Settings"
                            onClick={(e) => { e.preventDefault(); openSettings('general'); }}>
                            <CogIcon size="h-5 w-5" />
                        </a>
                    )}
                </main>
            )}
            {/* The shares this member watches play over every page (M4.2). */}
            {passwordSet && <SharePlayer />}
            {passwordSet && settings && <SettingsDialog section={settings} onSection={setSettings} onClose={closeSettings} />}
        </div>
    );
}

function Unpaired({ instance, error }) {
    const command = instance === 'main' ? 'dens open' : `dens open --instance ${instance}`;
    return (
        <Card title="Pair this browser">
            <p>This page only works in a browser paired with Dens on this computer. Run this in a terminal:</p>
            <pre class="rounded bg-base-300 p-3"><code>{command}</code></pre>
            <p class="text-sm text-base-content/70">It opens this page again with a one-time link.</p>
            <ErrorText message={error} />
        </Card>
    );
}

function SetPassword({ onDone }) {
    const [password, setPassword] = useState('');
    const [confirm, setConfirm] = useState('');
    const { busy, error, setError, run } = useAction();

    function submit(e) {
        e.preventDefault();
        const problem = checkPasswords(password, confirm);
        if (problem) return setError(problem);
        run(async () => {
            await api.post('/api/password', { password });
            await onDone();
        });
    }

    return (
        <Card title="Set a local password">
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
                <ErrorText message={error} />
                <SubmitButton busy={busy}>Set password</SubmitButton>
            </form>
        </Card>
    );
}
