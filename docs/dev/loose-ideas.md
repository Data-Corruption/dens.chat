# Dens — Loose ideas

Ideas and fixes no milestone has taken up yet. A lot of these are still loose, and not all of them may turn out possible or practical. One leaves this list when a milestone's plan in [milestones.md](milestones.md) takes it up. Those weighed and set aside are under Probably not doing, with why.

## Probably M24

First, what M23.1's security checklist found and left, each a den's doing, alone or with a member:

- A group's edits signed for: the author's signature covers who else may edit a message, so an edit, a tick or a reply's quote opens only from its author or someone they named. The names are the den's now, so the den and a member of the group together can show that member's words as another's message, edited. To settle in its plan: what becomes of others' edits to messages sent before it.
- A group's name from its signed changes: the name shown is the one its latest checked naming set, and a key's statement signs the name it carries. A member's Dens shows the sealed name the den sends now, so the den chooses among the names the group has had; it still reads none.
- A hold that outlasts a new device: a device reads the member's record before it meets anyone after a snapshot. One that never kept a member keeps their new identity unheld now, and its next write of the record lifts the hold on the member's other devices, with nobody choosing.
- The checklist's last probes: a test each for a group message's first signer and for fetched keys hashing to the identity kept, and the lines read behind the items ticked on a citation alone.

Then a race in Relay, which its test met once in CI:

- A listening ended by a stale word: listening to a device over a link that's already up ends at once as unshared while the link's last state says the device doesn't share, though the den's list, which Listen checks, already says it does, its state still on the way. To settle in its plan: which word holds while the two disagree, and for how long.

Then what M23 left of M22's review of every screen, and of what it found members would want.

- Pinned messages: a channel's own, pinned by staff and listed from its header, with no cap on how many. A pin is one more of staff's acts for M23's audit log.
- Forward a message to another channel or DM, from its menu, in one step where a pasted link takes several. M23's quick switcher finds where it goes.
- Names for a member's devices, given by them, shown in Devices, Relay and a sign-in's approval.
- A status line under a member's name, set from their own card, cleared after a time they pick, or never.
- Steam's trailers in its cards. Steam gives them only as DASH or HLS, so the first step is a probe: whether a page plays one rendition through Media Source Extensions from segments the service fetches, in Chromium, Firefox and the WebView. The media module isn't the tool; it converts files a job at a time.
- Steam's cards sized with YouTube's players, under one setting for both. A card is 460 pixels wide, its header picture's own width, so a larger one takes a larger picture from Steam, and its rows laid out for each size, with a trailer's place among them once the probe says there's one.

## Big

- Built-in sync-tube.de equivalent. A youtube player with shared controls and a queue. The den keeps the viewers in sync by recording the player state and simulating user clicks in the iframe? Putting it in the share window could work well, like any other stream but It's the iframe with a drawer on it's right that contains the queue. We could limit it to 1 per voice channel and you don't "start" one like sharing, you open that voice channels one. State pauses / persists when no one is viewing it.
- After Relay: its capture of a computer's sound could give a share its sound in Firefox and in Chromium on Linux, once the call's own voices are kept out of it. And what Relay left out: a link over the internet, a microphone relayed, the Android app as a source, and the sound compressed, which is what would bring a phone's 5% of battery an hour down.
- In the mic drop down select on desktop, if a phone is connected over lan, the phone can be selected as a mic? This is a sneaky way to allow high quality desktop relay audio while still "being in a call" from android. They simple join the call on the desktop, select the phone as their mic, then relay desktop audio to their phone. This bypasses the desktop relay audio quality drop when joining a call from android while relaying to it. This assumes there isn't some other small fix for that quality drop in calls in which case a more simple fix works.

## Medium

- Android, staying connected while the phone sleeps (android.md's *A phone sleeps* and *Staying connected*). Go's timers stop while a phone sleeps, so after a drop Dens reconnects only once something else wakes the phone, 4 to 6 minutes later on a desk in the spike, and a notification in that gap shows late. Dens checks the session's renewal against the wall clock each time the den's ping wakes the phone, which ends the hourly drop that made most of the gaps. The fixes left: a dropped connection reconnecting under the shell's wake lock, with an alarm behind it; the app saying so when Android restricts its battery, as Restricted and Samsung's deep sleep stop it; and the install guide's steps, which it has already. Done when a member's phone shows a DM within seconds, eight hours after they last opened Dens, which takes a night of the S24 asleep to test.
- Sharing to Dens from other Android apps: Dens registered for Android's share sheet, opening a page with a search over every channel and DM in every den the member is in, several picked at once, and a text box and a send button at its foot. It takes the shell taking the share's intent, its text, links or files, handing those files to the service, a send that goes to several dens and DMs at once, each DM's sealed with its own key, and a phone to try it on. Members have asked for it.
- Moderation, as a milestone of its own: members blocking other members, and what else members and staff need against one who misbehaves. With it, members' own deletions in the audit log, who, where and when, behind a toggle, weighed against what a delete leaves now: the message's ID and channel, never its author.
- Bookmarks, per member and per den, so a den's bookmarks always resolve against that den. Maybe bookmarks bypass the cache limit cause opening them and missing could result in non contiguous random reads across a channels.
- Search (a different algorithm than Discord's. Exact match + fuzzy? Future AI metadata generation?)
- Onboarding in the page's own first steps: a few screens the first time someone uses Dens, each saying in a line what's unusual about it (the local password and den passwords, recovery codes, the DM recovery key, approving a device, who can read what) and linking the guide, their words tried on people who haven't seen Dens. Until then the page's links to the guides stand in for it ([What the site holds](../README.md#what-the-site-holds)).
- x.com and reddit embeds, as Steam's cards, once either has a source that needs no key.
- TPM binding for the Windows data key, through the Platform Crypto Provider: every Windows 11 machine has TPM 2.0.
- Optional TOTP on the password fallback.
- An optional idle lock.
- Simulcast.
- TURN, should restrictive networks need it.
- A static dictionary for compressing the den socket's small frames, should measurements show live events matter: built from the protocol's shapes, never members' messages, and named in the upgrade so client and den agree on its version.
- AVIF: stripped in place by `internal/media`, keeping the original as sent, with dav1d in the module for previews of AVIF images and AV1 videos.
- Dedup of each member's own channel uploads: their Dens hashes a file once it's stripped, the den keeps the hashes keyed with its data key so its database doesn't show which files it holds, and a file on several messages goes with the last of them.

## Small

- One member on Fedora KDE with Waterfox leaves and rejoins calls without doing anything: flickering in and out, dropping out for a few seconds every 30 to 60 seconds (the timing may be off). The call's logs say which connection breaks first, once the member and the den's owner set them to debug and send them after the next drop (see [What's logged](design/calls.md#whats-logged)); the fix waits for them.
- On the Pixel 9 Pro in the spike, a call held with the screen off sent rougher sound through a quiet spell, until the screen came on (android.md's Calls). The page's notes on a call tell a renderer short of CPU from Opus adapting to mobile data; the fix waits for a Pixel's call with the screen off, and its logs.
- A headset's button as push to talk's key on a phone, through the Android app's shell, which hears it with the screen off: nothing presses the call's button off screen, so a member on push to talk is silent then.
- Select text in a message's sheet, should members miss copying part of a message: a long press opens the sheet in place of the WebView's own selection.
- A first install that rolls back leaves the instance's root and `control/`, with its lock and `maintenance.log`, and on Linux an `/etc/sysusers.d` it made, all from before its journal starts. Whether rollback should take them is open: the log is a failure's record.
- The shortcut helper's X11 path is tested against Xvfb alone. Untried: a real X11 session, and a session manager that runs autostart entries itself, as cinnamon-session does, rather than as systemd units, with `StartHelper`'s check of `DISPLAY` there.
- NixOS's installs are refused with a pointer to the Flatpak's guide, which has no NixOS lines: by NixOS's own docs, Flatpak needs `services.flatpak.enable = true;` and a rebuild, and the app shows in the menu after signing in again. Untried on NixOS.
- Relay's worklet steps between sample rates by linear interpolation with no low-pass, so a call's context below 48 kHz would alias. The rate of a phone's call context hasn't been read.
- Each move of a call's route on the phone leaves a gap of 120 to 700 ms while the voice processor's stream opens on the new device (android.md's Calls). Untried: the processor's context with `sinkId: {type: 'none'}`, off the output device.
- On the phone, a watched share's sound plays through the player's `<video>`, not the call's context, so it may play as media rather than on the call's route, as a Bluetooth headset or the earpiece. Untried on a phone.

## Probably not doing

- A picture behind the whole page, under every pane, sheet and menu: each would go translucent, and every text's contrast would depend on the picture. Dens puts one behind the message list alone, under a scrim the member dims.
- A color picker the page draws, so it looks the same in every browser. The browser's `<input type="color">` with a hex field beside it does, and the hex field is the same everywhere.
- Calls from a DM or a group DM, with a call button in the conversation. Calls stay in voice channels, which are encrypted end to end, until members ask for calls of their own.
- A Double Ratchet for DMs, as Signal's. Its forward secrecy comes from every device forgetting old keys, while Dens opens a DM's whole history from the den on each of the member's devices: it would take keys of each device's own, history handed over at every approval, and a copy of each message for each device. Dens takes Signal's way of checking keys instead, and keeps Dens's history.
- Forward secrecy for disappearing messages: keys of each device's own for them, made at random and changed every day, with a disappearing conversation's key sealed to each of its members' devices rather than their identities, so a den that kept copies of what it should have deleted, and later got a member's DM recovery key or a device of theirs, couldn't open them. A new device would read nothing from before it, and it's a second way of sealing keys beside identities.
- Dens's own F-Droid repository, from which F-Droid 2.0 updates apps by itself, with the app's updater stepping aside when F-Droid installed it. The APK on the release host and the updater already install and update the app with one tap, and a repository would be another signed thing to publish and keep each release, for updates that need no tap.
- UnifiedPush through the den, so a den wakes its members' phones: an additive protocol change, which needs nothing from Google. The foreground service's own connections keep the app connected, for about 1% of the battery a night per den in the spike, and a phone that stops Dens would stop the distributor UnifiedPush needs on the phone too, unless the member exempts it from the battery's limits, as they'd exempt Dens.
- Replacing the web page with a native UI (GPUI, Flutter) or a canvas-rendered UI using HTML-in-Canvas. A native UI gives up the browser's WebRTC stack, hardware codecs, YouTube players, accessibility, and text/IME handling for a much larger maintenance surface; HTML-in-Canvas is Chromium-only (leaving Firefox out) and blocks cross-origin `<iframe>` elements like YouTube embeds from painting into the canvas anyway, while chat is mostly scrolling text and controls that the DOM already handles well.
