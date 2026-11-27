# Changelog

## [v1.17.0] - 2026-11-27

### Added

- Custom roles for channel visibility, once the den is updated: staff create roles in Manage's Roles, assign them to members, and restrict text or voice channels to selected roles. Members without a channel's role can't see its messages, files, calls, events or badges; losing a role disconnects a member from its call, and deleting the last role on a channel turns it staff-only so it never opens to everyone.
- Collections, once the den is updated: bookmark any message (or use Save to collection in its ⋯ menu) to gather it into a shared collection for that text channel or into your own personal collections across the den, including messages from DMs and Notes. Open a channel's collections from its header, your personal collections from Collections in the sidebar, or either from Ctrl+K. Staff manage shared collections and choose whether everyone or staff only can add to them; personal collections are visible only to you, though the den stores their names and which message IDs you saved while DM and Notes text stays encrypted.
- Links back on channel forwards, once the den is updated: forwarding a message from a text channel carries a link back to its channel and message (encrypted inside a DM or Notes), so members who can see the source channel can jump straight to it, even across dens. Forwards from a DM or Notes carry no link, and nobody sees the original author or den address.
- Steam card trailers and sizing: Steam cards have their own size slider under Appearance's Messages, independent of YouTube players, and play a game's primary trailer on click using the same controls and volume as uploaded videos. Stepping through a card's screenshots while a trailer plays keeps it running behind a corner pill. Adult-only cards keep their pictures and trailers covered until you show them.

### Changed

- In a den's left sidebar, the den header and tabs at the top and your profile, call and Relay controls at the bottom stay in place while only the channel or DM list scrolls.

### Fixed

- Since v1.0.0, deleting the newest message in a channel you hadn't opened could leave the channel marked unread until you opened it. Deleting it clears the unread mark at once on an updated den, and opening the channel clears it on older dens too.

## [v1.16.0] - 2026-11-25

### Added

- Forward, in a message's menu: it sends the message, with its photos and files, to a channel or a DM in any den you're in. It shows there as forwarded, with nothing of where it came from.
- A status, in Edit profile once the den is updated: a line under your name that everyone in the den sees, until you empty it or the time you picked passes.
- Rename, beside This device in an updated den's Devices: the name shows in all your dens, in Relay and when the device asks to sign in somewhere new.

### Changed

- The card beside Edit profile's fields, and the small den beside Appearance's editor, have a border marked Preview.

### Removed

- Group DMs. Updating a den deletes them, with their messages and files. Keeping them private from the den, when one of their members could help it, kept opening new holes, and few used them.

### Fixed

- Since v1.9.0, a DM waiting on you after someone's keys changed could stop waiting once another of your devices signed in or saved what it knows, without you choosing Compare or Send anyway. It waits on every device until you choose.
- Since v1.0.0, a den could put a message of its own, unencrypted, into a DM or your notes, and Dens showed it as written by the other member or by you. It shows as a message that can't be read.
- Since v1.0.0, a photo or file in a DM or your notes could open as whatever the den sent in its place, after Dens reconnected or restarted. It opens only once its message has loaded again.
- Since v1.12.0, listening to another device's sound could stop at once, as not shared, just after that device started sharing it.
- Since v1.0.0, a covered spoiler showed the links and mentions inside it, and since v1.10.0 an event's link inside one fetched the event before it was shown.

## [v1.15.0] - 2026-11-22

### Added

- A call has a pane of its own. Joining a voice channel opens it: everyone in the call as a tile that lights while they talk, the shares you watch large on a stage, two side by side, and the call's controls along its foot. Leave for a text channel and the call goes on; click the voice channel to come back to it. On a phone, tap the call's bar.
- Away, in a call: it mutes you and shows z's beside your name to everyone who sees the call, until you unmute or talk. A den that hasn't been updated yet shows you muted.
- A quick switcher: Ctrl+K, or the magnifier atop a den's list and on Home, finds a channel, a DM, a group or a den by a few letters, across every den you're in, with what's unread first.
- Keys for getting around: Alt+↑ and Alt+↓ go to the channel before and after, Esc marks the open one read, and Ctrl+/ lists every key.
- Mark as read, in the menu of a channel, a DM or a group, and Mark all as read, from the den's name, once the den is updated.
- Links to channels: type # and a few letters to complete one, or Copy link from a channel's menu. It shows as the channel's name and opens it.
- A channel's notifications have three levels, from its bell or its menu: All messages, Mentions, as before, and Nothing.
- Do not disturb, from the moon beside the cog and on Home: for an hour, until tomorrow at 8:00, or until you turn it off. Nothing notifies or sounds meanwhile, and unread marks go on as ever. It's this device's alone.
- Members, in Manage, for moderators and the owner: everyone in the den with their role, when they joined and the day they were last seen, found by a few letters, sorted by any column, each with their menu.
- An audit log, in Manage, for moderators and the owner: what staff did and to whom, for 90 days. Removals and bans, roles, invites, channels, a member's message deleted, calls, emoji, sounds, events and the den's settings. Never what a message said, and nothing of DMs. It starts as the den is updated.

### Changed

- A channel's menu is every member's now, from its ⋯, a right click or a long press: Mark as read, Notifications and Copy link, with staff's rows after them.
- A den's moderators and owner can see the day you were last there, and nothing closer than the day.
- The Voice settings open from the call's pane and the settings; the call's block under the channel list no longer has a cog.
- Devices says when each device was last seen, where it said last signed in.

### Fixed

- In Firefox, the microphone's meter in the settings' Voice froze at its last level while you were muted. It falls silent now.
- Since v1.2.0, the Flatpak's Restart, and its restart into an update, could leave Dens closed, on Debian 13 at least, where Flatpak sometimes fails to start an app just as another copy of it closes. Dens now waits until the new one has started before it closes, and starts it again if it didn't.

## [v1.14.1] - 2026-11-21

### Fixed

- Since v1.0.0, a den that set out to read a DM could tell your Dens the DM was an ordinary channel. Your Dens believed it, showed it as a channel, and sent what you wrote there next unencrypted, as it does in channels. Group DMs and notes went the same way. Dens now remembers what each one is for good, and stops talking to a den that says otherwise, which then shows as out of reach. What you sent before stayed encrypted.
- Since v1.9.0, while a DM or a group DM waited on you after someone you'd verified got new keys, an edit to one of your messages there, or a ticked task, could still go out, encrypted to their new keys, before you chose Compare or Send anyway. Nothing goes now until you choose.
- Since v1.10.0, a photo sent as a spoiler that was too large to preview showed uncovered, as a card with its name. It's covered until you click it, as any spoiler is.
- Since v1.0.0, the settings' Voice could open without your shortcuts showing, until one of them changed.

### Changed

- A dialog about someone names them by @username alone: Remove, Ban, and deleting another member's message.

## [v1.14.0] - 2026-11-19

### Added

- Sign in on a new device by scanning. Add device, beside a den on Home, shows a code; on the new device, Add a den's Scan reads it with the camera. Approve it on the first device, and a second code carries the digits. Typing still works, and is all there is where there's no camera. A den that hasn't been updated yet leaves the approval open until you press Done.
- Steam links show as cards: the game's picture and first screenshots, its reviews, who's playing now, the systems it runs on with Steam Deck's rating, and its price where you are, with a sale's cut and end.
- A browser opening Dens for the first time asks about link previews, YouTube's players and Steam's cards, since each shows its site your IP address. A browser that already uses Dens keeps its settings, where Steam's cards start off: turn them on under Chat in the settings.
- Mute, in the menu of someone in a call, silences them for you alone, and Unmute brings them back at the volume they had.
- Dens's tab shows a red dot on its icon while a DM or a mention is unread, in any den.
- A theme of your own holds everything a daisyUI theme does: the status colors, corners for cards, buttons and toggles, the border, depth and noise. Its code is a block of text, and one copied from daisyUI's theme generator pastes in. Codes from before still paste.
- File names on hover, under Appearance's Messages: turn it off to stop a photo's or a video's name showing under the pointer.
- Recovery codes save as a file named for the den, with your DM recovery key in it.
- Formatting help, at the end of the message box's +, lists the formatting and the keys.

### Changed

- Appearance is a page of its own that takes the window: the themes at the left, the one you chose in the middle, with Messages and Calls beside it, and a small den at the right that shows every change as you make it. Choosing a theme only shows it there, and Use puts it on. Customize copies a built-in theme into yours, and a theme's edits wait for Save.
- One look across the page: four kinds of button, a dialog's actions at its foot with the main one first, one slider, a choice of a few as one row of buttons, and the settings and Manage in one frame. The sizes of photos and players are sliders.
- Inside a den, the settings hold your own sections of it: Devices, Password and recovery, Your code and Your files, with Leave den. Manage keeps what staff manage, and opens from the den's name.
- Staff make channels and groups from menus: the den's name, a right click on the channel list, a group's menu, and the + at the end of the tabs. Move up and Move down are in a channel's and a group's menu.
- Menus show an icon on each row, and Move to, for someone in a call, opens the voice channels beside it.
- A message under the pointer shows react, Reply and, on your own, Edit beside its ⋯. Who else can edit a message is in its ⋯.
- The message box grows with what you write, wrapped lines too, up to half the pane, and holds its + and emoji button inside it. Messages end at its top edge.
- A sign-in waiting for your approval opens as a dialog over whatever you're doing, and closes by itself once the new device connects.
- Add a den offers three ways in: Scan, the den's address, with or without https://, or an invite.
- A profile opens as a card beside the name you clicked, without dimming the page. A phone keeps its sheet.
- Reactions take the theme's corners, as its badges do.
- Home with no den yet is a welcome with Add a den, and a call and Relay's bar share one box there.
- A message's menu says Copy message link.

### Fixed

- Since v1.9.0, a DM's first message could show as sent under changed keys when it arrived a moment before its key. Dens fetches the key first.
- Since v1.5.0, on a Linux desktop whose notification service didn't answer, the voice keys in other programs, and later Relay's sound, waited on it for up to two minutes. They no longer wait.

## [v1.13.0] - 2026-11-15

### Added

- Call timer: each call's length shows beside its voice channel, to everyone who sees the channel, counted from the first person in to the last one out. A call that outlasts the channel's longest gets fireworks and a rainbow, and hovering the length names the record to beat. Moderators and the owner reset a channel's longest call from its settings. To hide the timer, turn off Call timer under Appearance's Calls. A den that hasn't been updated yet shows no lengths.
- A notice that an update is ready shows above the channel in a den too, not only on Home, with Dismiss. Home keeps showing it.
- Hover, under Appearance's Chat background: how far the message under your pointer covers your picture, from not at all to fully, as before.
- Staff marks, under Appearance's Messages, shows the crown and the shield beside names in messages and calls.

### Changed

- In a call, an arrow in the call's controls opens the soundboard and the lock that shows the call's encryption and its codes to compare. The lock no longer sits beside the channel's name, and the soundboard no longer has a button of its own there. On a phone, the bar across the top of a channel keeps both.
- The crown and the shield no longer show beside names in messages and calls unless you turn on Staff marks. The member list and profiles always show them.
- A GIF in a message plays where it's posted, as Animations says: as it comes on screen under Always, and while the pointer is over its message under On hover. Before, it played only once opened.
- An event's Interested is the colored button until you press it, then reads ✓ Interested.

### Fixed

- Signing in to your account on a new device showed only three dots until your other device answered. It says it's waiting for your other device, at once.
- Unmuting a screen share's sound set it to full volume, whatever it was before. It returns to where you had it, and the next time you watch that person's share it starts there.
- Since v1.9.0, in a run of disappearing messages from one person, only the first showed its clock, so messages sent after the time was changed looked as if they'd go with the rest. A run ends where the time changes, and each shows its own.
- Since v1.8.0, an animated picture with a clear background, as a profile picture, a banner, an emoji or a badge, showed its still first frame behind it while it played.
- The message bar and a call's controls showed in a tint that didn't match the theme in the newest Chromium browsers, seen in Edge's preview channel. They keep the theme's color in every browser.
- Since v1.11.0, a name styled in Bangers lost the edge of its last letter.

## [v1.12.0] - 2026-11-13

### Added

- Relay, a new section in the settings, links your own devices on one network: those signed in to your account in the same den. The den only introduces them, and what they relay goes straight between them, encrypted. The guide's Relay page has the setups.
- Keybind relay: push to talk and your other voice keys work from whichever computer you're at, in the call another of your devices holds. On a phone, the call's button does it, under that device's name.
- Audio relay: turn on Share this computer's sound, then press Listen beside that computer on another of your devices to hear everything it plays. A phone plays it as media, so it reaches the headphones paired to the phone, with the screen off too.
- On Android 17, Dens asks to reach devices on your network the first time you turn on Keybind relay or press Listen.

### Changed

- The Flatpak asks for one more permission, to play and record sound, which Audio relay reads your computer's sound through. Dens can't install this update itself: update through your software center, which marks it "Requires additional permissions", or with `flatpak update`.

## [v1.11.0] - 2026-11-10

### Added

- Name styles: give your name in a den a font, a color or a gradient, where you edit your profile. Everyone there sees it in the member list, on your messages, in calls and on your profile. A color too dark or too pale for someone else's theme shows a little lighter or darker for them, so it always reads. To see names in their colors alone, or plain, set Name styles under Appearance. A den that hasn't been updated yet doesn't offer them.
- Themes of your own: under Appearance's themes, New theme makes one from a background, a text color and a primary color, with square or round corners, and the page changes as you pick. Copy code turns it into a line of text to send someone, who adds it with Paste a code. Your themes stay in the browser you made them in.
- Chat background: a picture of yours behind your messages, as dim as you set it. Your Dens keeps it, so every browser you open Dens in shows it, and nobody else sees it.
- On a computer, choose how large photos, videos and YouTube players show, each up to Largest, under Appearance's Messages. Photos and videos start a size up from before, and a large photo shows sharp, not as its small preview stretched.
- In a call, turn anyone up to 200% from their ⋯. Past 100% a loud voice may crackle.
- Push to talk's Release delay, under Voice, keeps you heard for up to two seconds after you let go of the key.
- A Toggle deafen shortcut, under Voice, which works from other programs as the other shortcuts do.
- Right-click a message, or a member in the list or a call, for its menu. On a link, a picture or selected text, right-click stays the browser's.
- YouTube Shorts play in a portrait player.

### Changed

- The settings list their sections down the side, as Manage does: Appearance, Chat, Notifications, Voice and System. Their explanations are a line each, with the rest behind an i or in the guide.
- Moderators move other moderators between calls. Muting for everyone and disconnecting still take a higher rank.
- A name too long for its row in the member list or a call scrolls to its end while the pointer rests on it.
- A member you turned down no longer shows a percentage beside their name in a call; the slider under their ⋯ holds it.
- The den's name at the top of its channel list has no arrow beside it. Clicking it still opens Manage.

### Fixed

- Since v1.10.0, a moderator or the owner dragging a call member's volume slider dragged the member's row along, menu and all. The slider keeps its drag now.
- The crown and the shield sat at different heights beside names in the member list, calls, profiles and messages. They stand at one height now.
- The quote above a reply took a click anywhere across the chat, jumping to the message it quotes. Only the quote itself does now.

## [v1.10.1] - 2026-11-08

### Fixed

- Since v1.9.0, a shared screen's sound from Chrome, Edge or any other Chromium-based browser went out unencrypted: the den received it in the clear and could have listened, while nobody watching heard a thing, so shares had no sound. Dens encrypts it now, as it does voices and screens. If a browser ever sends part of a call unencrypted anyway, Dens stops it within about a second and says why: a share's sound is turned off while the share goes on, a screen share ends, or your voice leaves the call. Someone sharing from v1.9.0 or v1.10.0 still sends their share's sound unencrypted, and nobody hears it, until they update.
- On Windows, installing and updating Dens, which run as administrator, wrote two files in your user's temporary folder first, the installer `dens update` downloads and the task that starts Dens's helper when you sign in, where another program of yours could have swapped them before they were used. They go in a folder only administrators can change now.

### Changed

- On Windows, the notice that an update is ready gives the line that installs it, `irm 'https://releases.dens.chat/install.ps1' | iex`, which runs in an ordinary terminal and asks Windows for permission itself.
- The app's links to the guides go to their new places: joining a den is under Usage, and hosting, on your computer or a server, under Hosting. Links from older versions still land.

## [v1.10.0] - 2026-11-07

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

### Fixed

- On Windows, a command that talks to Dens's service, like `dens backup`, could stop partway with "Overlapped I/O event is not in a signaled state". Dens handed Windows memory that Go could move while Windows still wrote into it; it doesn't now.

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