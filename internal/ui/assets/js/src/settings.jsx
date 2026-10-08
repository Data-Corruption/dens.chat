// The settings page: update checks, the local password, logging, where
// Reddit links open, and signing this browser out.

import { useEffect, useState } from 'preact/hooks';
import { api } from './api.js';
import { Card, ErrorText, Field, PasswordFields, SubmitButton, TextInput, checkPasswords, useAction } from './components.jsx';
import { oldReddit, setOldReddit } from './links.js';

export function Settings() {
    const [settings, setSettings] = useState(null);
    const load = useAction();
    const save = useAction();

    useEffect(() => {
        load.run(async () => setSettings(await api.get('/api/settings')));
    }, []);

    function change(key, value) {
        save.run(async () => {
            await api.post('/api/settings', { [key]: value });
            setSettings((s) => ({ ...s, [key]: value }));
        });
    }

    if (!settings) {
        return load.error ? <ErrorText message={load.error} /> : <span class="loading loading-spinner"></span>;
    }
    return (
        <>
            {settings.updatesManaged && (
                <Card title="Updates">
                    <label class="label gap-3">
                        <input
                            type="checkbox"
                            class="toggle"
                            checked={settings.backgroundUpdateChecks}
                            onChange={(e) => change('backgroundUpdateChecks', e.currentTarget.checked)}
                        />
                        Check for updates once a day
                    </label>
                    <label class="label gap-3">
                        <input
                            type="checkbox"
                            class="toggle"
                            checked={settings.updateNotifications}
                            onChange={(e) => change('updateNotifications', e.currentTarget.checked)}
                        />
                        Show a notice when an update is available
                    </label>
                    <p class="text-sm text-base-content/70">
                        A check only asks the release host for the latest version number, which shows it this computer's IP address.
                    </p>
                </Card>
            )}
            <ChangePassword />
            <Card title="Logging">
                <Field label="Log level">
                    <select class="select" value={settings.logLevel} onChange={(e) => change('logLevel', e.currentTarget.value)}>
                        {settings.logLevels.map((level) => <option key={level} value={level}>{level}</option>)}
                    </select>
                </Field>
            </Card>
            <ErrorText message={save.error} />
            <Links />
            <Card title="This browser">
                <p>Signing out unpairs this browser. Run dens open again to pair it.</p>
                <div>
                    <button
                        type="button"
                        class="btn btn-outline"
                        onClick={() => save.run(async () => {
                            await api.post('/api/logout');
                            window.location.replace('/');
                        })}
                    >
                        Sign out
                    </button>
                </div>
            </Card>
            <p class="text-sm text-base-content/60">
                Dens includes software from other projects, under their own licenses.{' '}
                <a class="link" href="/licenses" target="_blank" rel="noopener noreferrer">Read them</a>
            </p>
        </>
    );
}

// Links says what happens to links that are sent, and chooses where this
// browser opens Reddit's.
function Links() {
    const [old, setOld] = useState(oldReddit);
    return (
        <Card title="Links">
            <p class="text-sm text-base-content/70">
                When a link is sent, Dens takes out common tracking parameters, and shortens Reddit, YouTube and X links to the post or
                video they point to.
            </p>
            <label class="label gap-3">
                <input
                    type="checkbox"
                    class="toggle"
                    checked={old}
                    onChange={(e) => {
                        setOldReddit(e.currentTarget.checked);
                        setOld(e.currentTarget.checked);
                    }}
                />
                Open Reddit links on old.reddit.com
            </label>
        </Card>
    );
}

function ChangePassword() {
    const [current, setCurrent] = useState('');
    const [next, setNext] = useState('');
    const [confirm, setConfirm] = useState('');
    const [done, setDone] = useState(false);
    const { busy, error, setError, run } = useAction();

    function submit(e) {
        e.preventDefault();
        setDone(false);
        const problem = checkPasswords(next, confirm);
        if (problem) return setError(problem);
        run(async () => {
            await api.post('/api/password/change', { current, next });
            setCurrent('');
            setNext('');
            setConfirm('');
            setDone(true);
        });
    }

    return (
        <Card title="Local password">
            <p class="text-sm text-base-content/70">
                It protects Dens on this computer: its backups, and the keys it holds for every den. It isn't a den password, which
                signs you in to one den on a new device.
            </p>
            <form class="flex flex-col gap-2" onSubmit={submit}>
                <Field label="Current password">
                    <TextInput type="password" value={current} onInput={setCurrent} autocomplete="current-password" required />
                </Field>
                <PasswordFields password={next} setPassword={setNext} confirm={confirm} setConfirm={setConfirm} />
                <ErrorText message={error} />
                {done && <p class="text-success">Password changed.</p>}
                <SubmitButton busy={busy}>Change password</SubmitButton>
            </form>
        </Card>
    );
}
