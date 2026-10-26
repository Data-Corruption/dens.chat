# Changelog

## [v1.4.0] - 2026-10-26

### Added

- Each message's actions are in a ⋯ menu that touch screens and keyboards reach: Reply, Edit, Copy text, Copy link and Delete, each where you may. Arrow keys, Home and End move through it, and Escape closes it.
- Copy link copies a link to the message. Pasted in a message, in this den or another, it shows as a chip naming the channel, and a click opens the den and jumps to the message, or says why it can't: you haven't joined that den, the channel is gone or out of sight, or the message is gone. A link keeps working after its den moves to another address.
- Push to talk lights your ring for everyone in the call while you hold the key, whether or not you're making a sound, as long as you aren't muted.
- A share you watch opens in a window of its own, from the button in its controls, to move to another monitor, resize and make full screen. The player keeps its place, with a button that brings it back, and closing the window brings it back too.
- Download the newest logs, in the settings under System, to send with a problem; on a server, `dens logs`, with `dens logs --level debug` to have them say the most. They hold no messages, keys or addresses. At debug they follow calls closely, to find out why one drops. The install guide's new Sending logs says how.
- Clicking a channel group's name folds it, and clicking again unfolds it. A folded group still shows the channel that's open and any voice channel with a call in it.
- Beside a DM, the member list's place shows the other member's profile.

### Changed

- Deleting a message asks first, in a dialog, and staff deleting someone else's see whose it is.
- Den opens the den's text channel last open, and Direct messages the DM last open, or none: never a DM you didn't pick.
- Enter starts a new line in a code block, and Ctrl+Enter sends from anywhere.
- A dialog closes on a click outside it only when the press started outside it too, so selecting text that ends past its edge leaves it open.
- A click elsewhere cancels the wait for a key in the settings, as Escape does.
- Where Set up with your desktop can't be pressed, the keys' card says why, and what to do.
- The New divider marks only where unread messages started when you opened the channel.
- A deafened member shows as muted too, as deafening mutes them.
- The message box shows its focus by a darker border alone, and the composer's + lines up with it.
- A channel's ⋯ shows once Tab reaches it, and a call member's ⋯ shows on touch screens.
- The den's address answers a browser with a line saying it's a den on Dens, where it answered a 404.
- The install guide recommends Ungoogled Chromium, where Dens has had the fewest problems.

### Fixed

- Firefox played the sound of some videos, such as short Android clips, a couple of seconds after the picture. Videos sent from now on play in step, once the den runs this version.
- After a restore from a backup, a den could give a new message the ID of one it had given out before, which a link elsewhere might name.
- A message opened from Manage den's list of files, in the channel already open, didn't scroll to it.
- A call could fail when its answer reached the den at the moment the answer was due.

## [v1.3.0] - 2026-10-24

### Added

- Move the den to another computer, such as a server: in the settings, under System, in Hosting, or with `dens den export`. The export only holds the den, with its members, messages and files, not any of your client data. Restore it on the other computer with `dens restore`, which refuses one over an install that hosts a den or has joined one.
- Once the other computer hosts the den, Remove from this computer, in Hosting, or `dens den remove`, deletes the copy on yours. Your Dens then reaches the den at its address, as its owner, with your history and DMs.
- Hosting from the command line, for a server with no desktop: `dens den hosting on` and `off`, `dens password` to set or change the local password, and `dens den create` to start a den there, which prints the invite that makes you its owner. The new guide, Host on a server, walks through both ways, and the installer points to it on a computer with no desktop.
- Leave den offers Remove from this computer for a den Dens can't reach, such as one whose server is gone for good.

### Changed

- Backups write several times faster, and restore checks the disk's free space instead of fixed caps: a den over 256 GB restores, and a disk without room is refused before anything is written, saying how much more it needs. `dens backup` checks its folder has room first, and Back up in the settings says about how large the file will be.
- Restore checks the local password before it writes any file, so a mistyped one fails at once.
- `dens uninstall` asks you to type `perm delete data` rather than a y, says what the install holds, and points to `dens service stop` if you only want Dens off for now.

### Fixed

- A backup cut off partway, by an error or a stall, could look finished. Now it fails, from the command line and the page, and leaves no file, and a large one no longer stops at 30 minutes.
- Password prompts in the terminal printed their colon twice.
- The help tips beside Backup and Hosting in the settings were cut off on their left.

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