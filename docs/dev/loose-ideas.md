# Dens — Loose ideas

Ideas and fixes no milestone has taken up yet. A lot of these are still loose, and not all of them may turn out possible or practical. One leaves this list when a milestone's plan in [milestones.md](milestones.md) takes it up. Those weighed and set aside are under Probably not doing, with why.

## Big

- Android build, best effort feature parity. Target text and voice call support, streaming a nice to have. M12 built the app and M13 its phone layout; staying connected while the phone sleeps is under Medium; more in [android.md](android.md).
- End-to-end encrypted group DMs, then end-to-end encrypted calls with SFrame, one to one and in groups, piggybacking off the keys used for DMs? A call button in that person's or group's DM channel? Beside a group DM, the panel that shows a DM's other member (M11) would list the group's members.
- Post quantum double ratchet for forward secrecy? Maybe kinda copy Signal but force the side channel check? I need to learn more about this first.

## Medium

- Android, staying connected while the phone sleeps (android.md's *A phone sleeps* and *Staying connected*). Go's timers stop while a phone sleeps, so after a drop Dens reconnects only once something else wakes the phone, 4 to 6 minutes later on a desk in the spike, and a notification in that gap shows late. M14.1 checks the session's renewal against the wall clock each time the den's ping wakes the phone, which ends the hourly drop that made most of the gaps. The fixes left: a dropped connection reconnecting under the shell's wake lock, with an alarm behind it; the app saying so when Android restricts its battery, as Restricted and Samsung's deep sleep stop it; and the install guide's steps, which it has already. Done when a member's phone shows a DM within seconds, eight hours after they last opened Dens, which takes a night of the S24 asleep to test.
- Sharing to Dens from other Android apps: Dens registered for Android's share sheet, opening a page with a search over every channel and DM in every den the member is in, several picked at once, and a text box and a send button at its foot. It takes the shell taking the share's intent, its text, links or files, handing those files to the service, a send that goes to several dens and DMs at once, each DM's sealed with its own key, and a phone to try it on. Members asked for it during M15.
- Bookmarks, per member and per den, so a den's bookmarks always resolve against that den. Maybe bookmarks bypass the cache limit cause opening them and missing could result in non contiguous random reads across a channels.
- Search (a different algorithm than Discord's. Exact match + fuzzy? Future AI metadata generation?)
- Custom themes, image backgrounds, etc.
- Marking uploaded media as a spoiler.
- Onboarding in the page's own first steps, with simple, careful explanations tried on people who haven't seen Dens.
- x.com and reddit embeds
- TPM binding for the Windows data key.
- Optional TOTP on the password fallback.
- An optional idle lock.
- Simulcast.
- TURN, should restrictive networks need it.
- A static dictionary for compressing the den socket's small frames, should measurements show live events matter: built from the protocol's shapes, never members' messages, and named in the upgrade so client and den agree on its version.
- AVIF: stripped in place by `internal/media`, keeping the original as sent, with dav1d in the module for previews of AVIF images and AV1 videos.
- Dedup of each member's own channel uploads: their Dens hashes a file once it's stripped, the den keeps the hashes keyed with its data key so its database doesn't show which files it holds, and a file on several messages goes with the last of them.

## Small

- One member on Fedora KDE with Waterfox leaves and rejoins calls without doing anything: flickering in and out, dropping out for a few seconds every 30 to 60 seconds (the timing may be off). M11's logs say which connection breaks first, once the member and the den's owner set them to debug and send them after the next drop (see design.md's What's logged); the fix waits for them.
- In the profile edit dialog, let members pick their name's color, including start-to-end gradients. It waits for custom themes, so both cover it.
- On the Pixel 9 Pro in the spike, a call held with the screen off sent rougher sound through a quiet spell, until the screen came on (android.md's Calls). The page's notes on a call tell a renderer short of CPU from Opus adapting to mobile data; the fix waits for a Pixel's call with the screen off, and its logs.
- A headset's button as push to talk's key on a phone, through the Android app's shell, which hears it with the screen off: nothing presses the call's button off screen, so a member on push to talk is silent then (M13.4).
- Select text in a message's sheet, should members miss copying part of a message: a long press opens the sheet in place of the WebView's own selection (M13.2).

## Probably not doing

- Dens's own F-Droid repository, from which F-Droid 2.0 updates apps by itself, with the app's updater stepping aside when F-Droid installed it. The APK on the release host and the updater already install and update the app with one tap, and a repository would be another signed thing to publish and keep each release, for updates that need no tap.
- UnifiedPush through the den, so a den wakes its members' phones: an additive protocol change, which needs nothing from Google. The foreground service's own connections keep the app connected, for about 1% of the battery a night per den in the spike, and a phone that stops Dens would stop the distributor UnifiedPush needs on the phone too, unless the member exempts it from the battery's limits, as they'd exempt Dens.
