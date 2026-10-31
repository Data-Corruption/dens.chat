# Changelog

## [v1.6.0] - 2026-10-31

### Added

- A layout for phones, and for any window narrower than a tablet: a den's channel list, the channel you're reading and its members are three screens side by side, which you swipe between, or reach by the buttons at the top of the channel. Back closes whatever's open, then goes up a step at a time, from the members to the channel, the list and home. Home shows what's waiting in each den: a count of mentions and DMs, or a dot for anything else unread.
- Touch in place of hovering, on any touch screen: hold your finger on a message for its actions, and moderators and the owner on a channel or a group for its settings. Menus slide up from the bottom of a phone's screen, small marks and ⓘ say what they mean when tapped, and in the message box ↵ starts a new line while a button sends.
- On a phone, dialogs slide up from the bottom, and the settings and each den's Manage list their sections, each a page that Back leaves.
- A photo opens full screen on a phone, where you swipe to the message's other photos and down to close it. Zoom in with two fingers or a double tap, or on a computer with a double click or the wheel; comparing a photo's two copies zooms the same way.
- Calls on a phone: a bar across the top of the channel shows the call, with buttons to stop hearing it and to leave, and a tap on it goes to the call's members and its other buttons. A round button by your thumb mutes and unmutes, or works as push to talk or push to mute, as you choose in the settings under Voice & video: green while you talk, struck through in red while you're muted. A screen you watch shows across the top of the channel, and a tap plays it full screen, turning with the phone.
- In the Android app, the status and navigation bars take Dens's colors, and a den lists the phone among your devices by its name and model, from your next join or sign-in there.

### Changed

- Nothing you tap or click is smaller than 24 pixels across, and on a touch screen lists' rows are 44 pixels tall.
- On a touch screen, messages and channels have no ⋯: hold your finger on one instead. A mouse and the keyboard keep it.
- In a wide window, Back from a den goes home, rather than back through the channels you opened in it.
- A phone's settings leave out what it can't do, the keyboard shortcuts and screen share, and its keyboard writes usernames, invites and addresses without capitals or corrections. Dens says "this device" where a phone shows the words too.
- Add a den offers to host one only where Dens can host: not in the Android app or the Flatpak.

### Fixed

- On a phone, Back left the channel on screen and stepped through older pages, and the member list, once opened, stayed open over every channel. Back now moves between them, as above.
- In the Android app, tapping a notification during a call loaded the page again, which ended the call. It opens the message on the page now, and the call goes on.
- Things ran past the right edge of a phone's screen in places, such as the settings' System tab, a channel's settings, the recovery codes, comparing a photo's copies, and a DM's note where its keys changed. They fit now.

## [v1.5.1] - 2026-10-30

### Changed

- The Android guide shows the warning Play Protect may give as you install the app, and how to install it anyway. Dens itself is as it was in v1.5.0.

## [v1.5.0] - 2026-10-29

### Added

- Dens for Android, for phones on Android 12 or later: open releases.dens.chat/dens.apk in your phone's browser. It runs Dens on the phone, which keeps its own keys, and joins dens as any install does: chat, DMs, photos, which go as smaller copies with their location and camera details removed, and calls that keep going with the screen off. A call's notification moves its sound between the loudspeaker, the earpiece and a headset. It updates itself: when a new version is ready, it says so, and Android asks you before it installs it. It can't host a den or share your screen. The new Android guide has the steps, and the settings Galaxy phones need.
- Notifications of DMs and mentions, on every install, once you turn them on in the settings' Chat & media tab: each kind on its own, with the start of the message or without it. The bell beside a den's name mutes the den, and the bell at the top of a channel or DM mutes just that one. With a Dens tab open they show through your browser, and with none, as your desktop's own; clicking one opens the message. Reading a channel on any of your devices clears its notification everywhere.
- Call volume, in the voice settings, turns everyone in a call down at once, under the level you set for each member.
- On Windows, Dens is in the Start menu, which opens it.

### Changed

- A copy button says so when the browser won't copy, where it said nothing.
- At debug, the logs of a call also say what it sent each second, and when the browser froze the page and let it go again.

### Fixed

- A channel whose newest messages had been deleted could show its loading dots for good.
- A call stopped playing the others once the browser had frozen its page and let it go again, as phones do with pages they hide. It plays them again.
- The microphone stayed on if you left a call while the browser was asking for it.

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