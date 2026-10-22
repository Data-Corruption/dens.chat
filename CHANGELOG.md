# Changelog

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