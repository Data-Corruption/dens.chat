// Main Entry Point
// Initializes every module; controls are wired with event listeners (no inline handlers)

import { initTheme, setupThemeToggle } from './theme.js';
import { initPairing } from './pair.js';
import { initPasswordSetup, initPasswordChange } from './password.js';
import { initSettings } from './settings.js';

// Apply the theme immediately, before the page renders.
initTheme();

document.addEventListener('DOMContentLoaded', () => {
    setupThemeToggle();
    initPairing();
    initPasswordSetup();
    initPasswordChange();
    initSettings();
});
