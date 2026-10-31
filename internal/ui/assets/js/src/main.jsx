// Entry point. The theme applies immediately, before the first paint; the
// app renders once the document has loaded.

import { render } from 'preact';
import { setPageVersion } from './api.js';
import { initTheme, tellBars } from './theme.js';
import { App } from './app.jsx';

initTheme();

document.addEventListener('DOMContentLoaded', () => {
    tellBars();
    const root = document.getElementById('app');
    setPageVersion(root.dataset.version || '');
    render(<App instance={root.dataset.instance || 'main'} version={root.dataset.version || ''} pair={root.dataset.pair || ''}
        pairLink={root.dataset.pairLink || ''} />, root);
});
