# Dens — Loose ideas

Ideas and fixes no milestone has taken up yet. A lot of these are still loose, and not all of them may turn out possible or practical. One leaves this list when a milestone's plan in [milestones.md](milestones.md) takes it up.

## Big

- Android build, best effort feature parity. Target text and voice call support, streaming a nice to have. M12 to M14 hold it; more in [android.md](android.md).
- End-to-end encrypted group DMs, then end-to-end encrypted calls with SFrame, one to one and in groups, piggybacking off the keys used for DMs? A call button in that person's or group's DM channel? Beside a group DM, the panel that shows a DM's other member (M11) would list the group's members.

## Medium

- Bookmarks, per member and per den, so a den's bookmarks always resolve against that den.
- Soundboard.
- Events, a calendar above the channels. Each has a name, description, optional banner image, time, length, place (a voice channel), etc. You can link it in a channel, and others can click "Interested" and get a notification when it starts. Maybe even the option to make each a badge event: attending gets you a badge on your den account. The badge could just be a little image uploaded when the event is created, like uploading custom emojis.
- Emojis and reactions (images and GIFs).
- Search (a different algorithm than Discord's. Exact match + fuzzy? Future AI metadata generation?)
- Custom themes, image backgrounds, etc.
- Zoom when viewing images and GIFs.
- Avatar GIFs, with a setting to only play them on hover, off by default?
- Sound effects on call actions (join, leave, push to talk, push to talk release, etc.). Allow setting custom sounds for the actions. This and the soundboard will share a lot of backend, I'm guessing.
- Marking uploaded media as a spoiler.
- Onboarding in the page's own first steps, with simple, careful explanations tried on people who haven't seen Dens.
- Polls from the composer's + menu.
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
- In the profile edit dialog, let members pick their name's color, including start-to-end gradients. It waits for custom themes, after the phone layout, so both cover it.
