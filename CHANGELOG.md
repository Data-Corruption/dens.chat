# Changelog

## [v1.2.0] - 2026-10-23

### Added

- A Flatpak, for the Linux distributions the installer doesn't support, such as NixOS and SteamOS. Install it from the link in the install guide, and it updates through your software center. It joins dens but can't host one, since it only runs while you're signed in.
- The Flatpak asks for your local password each time it starts, unless you tick Remember on this computer: then your keyring keeps a key that opens it, and it starts by itself when you sign in.
- Back up in the settings, under System, downloads a backup in your browser, after your local password.

### Changed

- An open Dens tab reloads itself once Dens updates or restarts, instead of staying on the old page.
- A browser that isn't paired yet shows `dens open --print` too, which prints a link to open Dens in whichever browser you like.
- The installer's refusal on NixOS and SteamOS points to the Flatpak.

## [v1.1.0] - 2026-10-21

### Added

- Hosting is a switch in the settings, under System: turn it on with the local password to host a den on this computer, and off again, once you confirm, to take it offline. An install that hosted a den keeps hosting it after the update.
- On Windows, every install adds the firewall rules for the media ports, for `dens.exe` alone, so turning hosting on needs no installer.
- Calls and screen share show only in browsers that can make them, and the page says why where they can't.

### Changed

- The installer's `--den` option (`-Den` on Windows) is gone, and refused with a pointer to the setting. The port options stay.
- A restored backup starts with hosting off, as it starts with no browser paired.
- Release downloads are about a third smaller.

## [v1.0.0] - 2026-10-13

### Added

- Initial release.