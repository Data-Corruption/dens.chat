package build

// Mark is Dens's mark, a white D on a magenta tile over a cyan bar, as an
// SVG: the page's icon, and the app's on Linux desktops, where the installer
// puts it beside the .desktop file the shortcut helper needs (M7.2). It's the
// public site's icon too (docs/static/favicon.svg), and the Flatpak's
// (scripts/flatpak/chat.dens.Dens.svg, M9.3), which a test keeps the same.
const Mark = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48"><rect x="2" y="2" width="44" height="44" fill="#E5338A"/><path fill="#FFFFFF" d="M8.62 35V9H23.74Q28.57 9 32.09 10.51Q35.6 12.02 37.49 14.93Q39.38 17.84 39.38 22Q39.38 26.16 37.49 29.07Q35.6 31.98 32.09 33.49Q28.57 35 23.74 35ZM17.95 28.54H23.17Q24.72 28.54 25.93 28.08Q27.14 27.63 28.01 26.82Q28.88 26.01 29.33 24.89Q29.78 23.78 29.78 22.42V21.51Q29.78 20.15 29.33 19.05Q28.88 17.96 28.01 17.14Q27.14 16.33 25.93 15.9Q24.72 15.46 23.17 15.46H17.95Z"/><rect x="2" y="39" width="44" height="7" fill="#2BD2EE"/></svg>`
