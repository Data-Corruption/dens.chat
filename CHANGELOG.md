# Changelog

## [v1.10.0] - 2026-11-06

### Added

- Events, at the top of a den's channel list: a game night, a watch party or a meeting, each at a time and in a voice channel or anywhere else, with a description and a banner if you like. Mark ☆ Interested, and Dens tells you as it starts, on your phone too, even with its screen off; it shows Live until it ends, and Join takes you into its call. Moderators and the owner make events, and in Limits the owner can let everyone make a few. Its maker and the staff above them change it, cancel it before it starts, or end it early.
- An event's Copy link gives a link that shows the event's card wherever you send it, in a channel or a DM, with Interested on it. Once the event is over, the card says it ended.
- Badges: moderators and the owner can give an event in a voice channel a badge, a small picture and a title. Be in its call for a minute while it's on, and the badge shows on your profile, with what it was for on hover or a tap. The den keeps that you earned it, and when, not who else was in the call. Hide any of yours from your profile with its eye, where you edit your profile.
- Notes to self, at the top of your DMs and on your own profile: a conversation with yourself in each den, with files, replies and edits, end-to-end encrypted so only your own devices read it.
- Compact messages, under Appearance's Message density: each message a line of its time, name and text, on a phone too.
- Moderators and the owner drag channels and groups into order, and a member in a call onto another voice channel, which moves them there; Move to in a call member's ⋯ does the same by touch or keyboard.
- Send a photo or video as a spoiler: it shows covered until someone clicks or taps it, in a channel or a DM.
- Check now, in the Android app's Updates settings, looks for a new version at once.

### Changed

- Profiles look like cards now: your banner across the top, your picture over its edge, your name under it, and your badges beside your name. A crown marks the den's owner and a shield a moderator, on profiles and in the member list, where colored labels were. One button stands out, Message, or Edit profile on your own, and the rest is under ⋯.
- Make moderator and Remove moderator are Promote to moderator and Demote to member, under ⋯ on someone's profile.
- A device whose Dens hasn't been updated yet doesn't show your notes to self until it is.

## [v1.9.0] - 2026-11-05

### Added

- Codes, to be sure nobody sits between you and the people you DM. Everyone has one in each den, 20 digits: compare yours with theirs, in person or on a call, from the lock in your DM with them, their profile, or a call's lock. A check then shows beside their name, on your other devices too, and the den never learns whether you compared; under Appearance, Verified status says whether it shows in lists always, only under the pointer, or not at all. If someone's keys change, as when they reset their DM recovery key, the DM says so, and one where you'd compared waits until you compare again or choose Send anyway.
- Calls are end-to-end encrypted: voices, screens and a shared screen's sound are sealed in your browser or the app before they leave, so the den carries them without hearing or seeing them. The soundboard still plays. A lock beside the call says so, and lists who's in it, to compare codes with.
- Group DMs, of three to ten people, end-to-end encrypted as DMs are: from + New group above your DMs, or Add people on the profile beside a DM. Give one a name if you like; only its members ever see it. Anyone in it can add people, rename it or leave, and whoever made it can remove people. Each change is signed by whoever made it, so the den can't add anyone, and someone added reads the group from when they joined.
- Disappearing messages: anyone in a DM or a group DM can have its messages disappear 1 hour, 1 day, 1 week or 4 weeks after they're sent. When its time comes, a message is gone from the den and from everyone's devices, even ones that are offline.

### Changed

- A new DM opens at once, with no check digits to type. DMs where you both typed them go on as before.
- In a den running v1.9.0, calls need v1.9.0 too, since an older Dens can't encrypt them. On Android, joining one needs Android System WebView 141 or newer; the app says so, with a link to update it.
- A device whose Dens hasn't been updated yet doesn't show your group DMs until it is.
- Staff-only channels show a shield, since a lock now means end-to-end encryption.

### Fixed

- A DM whose unread messages were deleted stayed marked unread until Dens next reconnected, and a deleted message that mentioned you still counted as an unread mention. Both clear as the message goes now.
- The Direct messages tab showed + Channel and + Group too. They're in the Den tab alone now.

## [v1.8.0] - 2026-11-03

### Added

- Animated pictures: a GIF or WebP that moves, as your picture or your profile's banner, framed as a still one is, with your own Dens making it small enough to send. Under Appearance, Animations plays them always, or only while the pointer is over them, and in a profile you open; that's the default where your system asks for less motion.
- A den's own emoji. Moderators and the owner add them in the den's Manage, under Emoji, from a picture, a photo or a GIF or WebP that moves, and everyone writes them by name, like `:party_parrot:`, in channels and DMs. A message of nothing but emoji shows them large. In Limits, the owner sets how many the den keeps, 50 at first and up to 500, or turns them off. Your Dens keeps a copy of every one, so a den doesn't learn which ones you see.
- An emoji picker in the message box, with the ones you use most first, the den's own, then the usual ones by group, a search and a skin tone. Typing a colon and two letters, like `:th`, lists the emoji that match, and `@` lists the people who can see the channel.
- Reactions, in channels and DMs: from the smiley beside a message's ⋯, its menu's Add reaction, or on a phone a long press, with the six you use most at the top. A reaction's chip says who reacted. A DM's reactions are end-to-end encrypted, like its messages, and moderators can take an emoji's reactions off a message in a channel.
- Polls in channels, from the + beside the message box: the message is the question, with 2 to 10 choices, one or several picks, an hour to two weeks to run, totals that show as votes come in or only once you've voted, which makes your vote final, and voting that's anonymous, anonymous until the poll ends, or not. A poll can't be edited, and its author can end it early.
- @everyone, for moderators and the owner, which lets everyone who can see the channel know, as a mention does. Turn it off for yourself under Mentions in Notifications & sounds; a muted den or channel silences it too.

### Changed

- A channel or DM with messages you haven't read is bold, with a small mark at its left edge, and the ones you've read are a little fainter, so they're easier to tell apart in every theme and on a phone.
- A video's seek bar is thinner, with a smaller handle.
- On a phone, a menu's title stays at its top as a long one scrolls, so the emoji picker's search stays in reach.

## [v1.7.0] - 2026-11-02

### Added

- Sounds: a short one for a DM or a mention, and in a call as someone joins or leaves, you included, as someone starts or stops sharing their screen, and as your microphone turns on or off. Turn each off, set how loud it is, or swap in a file of your own, under Sounds in the settings' new Notifications & sounds tab. A message makes one sound, with its notification; turning that kind of notification off silences it too, and a muted den or channel, or the channel on your screen, makes none.
- A soundboard in each den. Moderators and the owner add sounds in the den's Manage, which the den's name at the top of its channel list now opens: any audio your browser plays, or a video's sound, each made about as loud as the rest. Anyone in a call plays one for everyone from the call's controls, or from the bar at the top of a channel on a phone, and their ring lights up while it plays. One plays at a time, you wait a few seconds after yours, and a slider sets how loud the soundboard is for you, down to silent. In Limits, the owner sets how many sounds the den keeps, how long each may be, up to 5 minutes, and how long members wait between them.
- On a YouTube player's cover, the title links to the video and the channel's name to the channel; a click anywhere else still plays it.

### Changed

- Links show by their color, and are underlined only under the pointer or the keyboard's focus.
- A call's state shows on its channel's speaker in the list, green once connected and yellow while connecting. The call's controls hold the soundboard where the speaker was, and say how the call stands only while it isn't connected. On a phone, the call's bar gains mute and the soundboard.
- While you're deafened, no one's ring lights up for you, and the soundboard waits until you undeafen.
- Notifications moved from Chat & media to Notifications & sounds.

### Fixed

- A phone asleep for over an hour dropped its connection to each den and signed in again, since Dens renewed its session by a clock that stops while the phone sleeps. It renews in time now.
- A thin video, like two phone videos side by side in one message, hid its full screen button. The button always shows now, and full screen has the rest of the controls.
- The note on a small mark, like a muted DM's bell, could be cut off at the edge of the channel list. It stays inside the list now.
- On Linux with a recent systemd, as Arch Linux has now, sending a video, an audio file, or a photo that goes as a smaller copy or in another format failed, saying the den couldn't be reached, and a den hosted there turned videos and audio away. The sandbox Dens runs in refused how it lowered its media worker's priority; it asks in a way the sandbox allows now.

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