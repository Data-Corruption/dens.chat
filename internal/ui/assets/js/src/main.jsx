// Entry point. The theme applies immediately, before the first paint; the
// app renders once the document has loaded.

import { render } from 'preact';
import { initTheme } from './theme.js';
import { App } from './app.jsx';

initTheme();

document.addEventListener('DOMContentLoaded', () => {
    const root = document.getElementById('app');
    render(<App instance={root.dataset.instance || 'main'} version={root.dataset.version || ''} />, root);
});
