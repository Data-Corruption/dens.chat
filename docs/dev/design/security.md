# Security hardening

Part of [Dens's design](../design.md): the checklist every change is held to, by area.

The localhost page is the most valuable target: an XSS there reaches every joined den, the vault and the local API. Any den member can send you content, so all of it is hostile input.

Before the first release an audit checked every item here against the code (2026-10-11), with a probe where a claim needed one, such as the Windows control pipe closed early, run natively, and the addresses in Chrome's answers, from headless Chrome on Windows; it also measured the markdown parser on hostile text and the snapshot's scan at a million messages. What it found was fixed for v1.0.0. An item a later milestone adds is checked as it's built.

## Processes and listeners

### Client page and listener

- [x] CSP: `default-src 'none'`, with scripts, styles, images, media, fonts, connections and forms limited to `'self'`, images also from `data:`, scripts also compiling WebAssembly (below), no inline scripts, frames only from YouTube's player, `frame-ancestors 'none'` and `base-uri 'none'`.
- [x] Messages rendered by an escaping markdown subset; names and channel descriptions treated as untrusted text.
- [x] Filenames treated as untrusted text, and files shown inline only as the image type the local service found them to be, sandboxed and never cached by the browser.
- [x] Exact `Host` check (`127.0.0.1:<port>`, `[::1]:<port>` or `localhost:<port>`) against DNS rebinding.
- [x] `Origin` check on every write and on the WebSocket upgrade.
- [x] Session cookie `HttpOnly`, `SameSite=Strict`; pairing tokens single use and short-lived.
- [x] Listener bound on both `127.0.0.1` and `::1`.
- [x] Everything from a den is hostile input: the client service checks every den response against the protocol's types and limits before storing or forwarding it.
- [x] A den's error codes reach the page only when they're codes the page knows, and a client follows a den to a new address at most once a minute, one check at a time, never to plain `http` or loopback for a den elsewhere, and follows no redirect.
- [x] The page parses a message in time proportional to its length, and once per text.
- [x] The CSP allows WebAssembly for RNNoise (`'wasm-unsafe-eval'`), but not `eval`.
- [x] Rewritten links stay on their own site: hosts match exactly, as a URL parser reads them, so a lookalike is never rewritten into a real link.
- [x] A link to a message names what this member's Dens knows of where it points, never what its sender wrote, and opens in Dens, never in the browser, unless the member asks the browser to.
- [x] The page's links to the public guides open in a new tab, with no opener and no referrer (v1).
- [x] The page frames nothing but YouTube's player from `https://www.youtube-nocookie.com` (`frame-src`), only for a member with players on who clicked, sandboxed without top navigation, at an address the page built from an ID it checked; it takes no messages from the frame, and its popups can't reach the page.
- [x] The local service fetches nothing from YouTube but the oEmbed and picture addresses it builds from a checked video ID, follows no redirects, caps what it reads, cleans titles as names, and serves a picture only as a JPEG it read and stripped, with a member's file's headers.
- [x] The local service fetches nothing from Steam but the store's, price, players' and tags' addresses it builds from a checked app ID, and pictures from Steam's fixed host at a path checked against that ID; it follows no redirects, caps what it reads, cleans text as names, and serves a picture only as a JPEG it read and stripped, with a member's file's headers; and an app for adults only shows no picture until the member asks.

### Den listener

- [x] Serves only den routes; no client or admin routes compiled into its router.
- [x] Challenge answers sign the den's address, and clients send nothing to an address the den didn't sign, so no relay can pass a sign-in through.
- [x] A password alone doesn't add a device: another of the member's devices approves it, or a recovery code stands in.
- [x] A scanned sign-in code reads as a den's address and a username, or as 16 digits, by one grammar in the service and the page, tested against one file of cases, or as nothing; what reads only fills the fields a member would type, and the page's reader refuses codes past version 10 and segments Dens makes none of.
- [x] A DM's key is sealed to its two members' identities and signed by its maker's, which the den checks before it takes the key and can't open; the den starts none of the exchanges older Dens made keys by, and the DM seal never reaches the den.
- [x] Request size limits, WebSocket message size limits, and rate limits per IP and per member, with one budget for a member's messages across every channel and DM. A request's body arrives within a minute.
- [x] Invite codes: 128-bit random, single use unless staff allow more (at most 100), expiring, stored hashed.
- [x] Bearer tokens only, never cookies, so no web page can make a browser act on a den.
- [x] The den's ring of recent events is held to 64 MiB as well as its count and age.
- [x] Staff powers stop at DMs, and a ban is lifted only by staff of the rank of whoever placed it, or higher.
- [x] Only the desktop user of the den's machine, with the local password, makes an owner invite, which is used once within an hour.

### Control endpoint

- [x] Every connection authorized by OS-reported peer identity against the recorded desktop user.
- [x] The CLI verifies the server's identity before trusting a response.
- [x] Windows pipe: `FILE_FLAG_FIRST_PIPE_INSTANCE`, `PIPE_REJECT_REMOTE_CLIENTS`, one listening instance at all times, DACL granting SYSTEM and the service SID full access and the recorded user only `0x12019b` (never `GENERIC_WRITE`).
- [x] The endpoint keeps answering after a client that opens it and closes at once, and on Windows a service that fails after starting is restarted, as systemd restarts it on Linux.

### Service sandbox (Linux)

- [x] `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp`, `PrivateDevices`, `RemoveIPC`.
- [x] `ProtectKernelTunables`, `ProtectKernelModules`, `ProtectKernelLogs`, `ProtectControlGroups`, `ProtectClock`, `ProtectHostname`, `ProtectProc=invisible`, `ProcSubset=pid`.
- [x] `RestrictNamespaces`, `RestrictRealtime`, `RestrictSUIDSGID`, `LockPersonality`, `MemoryDenyWriteExecute`.
- [x] Empty `CapabilityBoundingSet` and `AmbientCapabilities`, `SystemCallArchitectures=native`, `SystemCallFilter=@system-service`, `SystemCallErrorNumber=EPERM` so a blocked call fails and is logged instead of killing the service.
- [x] `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK`. Netlink is needed for interface enumeration, which Pion's ICE gathering uses; without it `net.Interfaces` fails.
- [x] `LimitCORE=0`, `UMask=0077`, `StateDirectory=dens/%i/data` (mode 0700) and `RuntimeDirectory=dens/%i` (mode 0755, for the socket) as the only writable paths.
- [x] `systemd-analyze security` rates the unit 1.5 ("OK") on every supported distro; the lifecycle e2e fails above that.

### Service sandbox (Windows)

- [x] Virtual account, `SERVICE_SID_TYPE_RESTRICTED`, required privileges limited to `SeChangeNotifyPrivilege`.
- [x] Protected DACL on the state directory and data key blob: SYSTEM, Administrators and the service SID only.
- [x] `dens.exe` excluded from Windows Error Reporting.

### Hosting and the Flatpak

- [x] Nothing of the den listens until the owner turns hosting on, with the local password, and turning it off closes the den listener and the media ports at once.
- [x] The Windows firewall rules admit only `dens.exe`, on the media ports alone.
- [x] The Flatpak keeps no copy of its key that opens without the local password unless the member asks it to remember, and never keeps the password itself. It takes the Secret portal's secret only once it has answered the same twice, and never replaces a remembered copy that doesn't open.
- [x] The Flatpak refuses data a newer version wrote.
- [x] The Flatpak's repository is GPG-signed and built only from release binaries whose cosign signatures CI has checked, and its key reaches only the job that publishes it, from `main`.

### The Android app's updates

- [x] The updater downloads an APK only once the release's checksums check out, against the release workflow's identity and Sigstore's trusted root, as cosign would check them, and offers it only matching them, newer than itself; a build without the identity updates from nothing.
- [x] Android's installer installs it only once the member confirms, and only signed with the release's key; the app asks for `REQUEST_INSTALL_PACKAGES` alone.
- [x] The APK's key reaches only the release job, from `main`, and a release's build refuses an APK that key alone didn't sign. The release pipeline checks each release's signature as the app will before publishing it.

### Logs and data

- [x] No message content, tokens, keys or IPs in logs; log IDs and event types only. That holds for the addresses Go's network errors name too, of dens and of the release host. Pion's own messages, which name addresses, reach the log only in development instances.
- [x] A page's notes on its call reach the log by their own fields alone, words from fixed lists and numbers bounded, at most two a second after a burst of 64; the logs' download is a paired browser's alone.
- [x] SQLite `secure_delete=ON`.
- [x] Signed releases: the installers and `dens update` verify cosign signatures against the release workflow's identity.
- [x] No release older than the installed one installs, and only command-line flags, never the environment, change where releases come from or skip their verification, which a binary the release pipeline built refuses.
- [x] Nothing a crash leaves in the data directory's `tmp` outlives the next start, and a backup holds every file its snapshot names.
- [x] The binary carries the license notices of everything it includes, and CI fails when they're stale.

## The page

### Custom themes

- [x] A theme reaches the page's styles as DaisyUI's variables alone, each a color as `#rrggbb` or `oklch(L% C H)` or a number in its unit and range, written out by the page itself, whether the member made it, the browser kept it or a code held it.
- [x] A theme's block is read value by value, each it knows parsed and written out again, the rest left out; a v1.13.0 code is read whole, by one pattern of hex digits; and its name shows as text.
- [x] The chat's background stays on the member's install: the client router alone serves and takes it, its writes behind the `Origin` check, and it's served as the image it is, sandboxed and never cached.
- [x] The service takes only a still PNG, JPEG or WebP, by its own look at the bytes, of at most 10 MB, strips it, and keeps it sealed with the data key.
- [x] Nothing about the background is logged but that it was set or removed.

### Getting around

- [x] The switcher draws every den's names, its channels' and members', as text, from that den's state on this install, which holds only what the member sees; opening it asks no den anything.
- [x] A channel's link names its den by ID and the channel by ID; its chip's words are the member's own Dens's, and a click opens only a text channel, DM or notes the member sees, never a call, one out of sight reading `#unknown`, as one gone does.
- [x] `POST /api/read` moves the member's position only where they see the channel, by the snapshot's own rule, never backward, in one write that counts toward their writes, and tells their sessions alone.
- [x] A voice key bound over a page key takes its press, so the page acts on no key the member gave a voice action.

## Members

### Name styles

- [x] A name's style reaches the page's styles as a font family from the page's own list and colors of six hex digits alone, checked by the den as it's set, by the client as a den sends it, and by the page as it draws it.
- [x] The fonts are the binary's own, fetched from the page's origin under `font-src 'self'`, and each license is in the notices.
- [x] A color that wouldn't read on the reader's theme is moved until it does, so a name can't hide in its background, and a reader turns styles off.

### Events and badges

- [x] An event's name and place and a badge's title are plain text, cleaned as names are, and its description the markdown subset; a client checks each again as it arrives, with the event's times, banner and badge, and the page shows them as text. The den keeps them sealed with the data key, bound to the event.
- [x] Staff make events, and members only as many to come as the owner allows each, none until they do and 10 at most; a den holds 50 that haven't ended. A maker changes, cancels and ends their own, staff those of members they outrank, and each of those, and marking Interested, counts toward the member's writes.
- [x] An event in a staff-only voice channel reaches staff alone, with its banner and its badge, and its count of interested holds only those who see it. Nobody sees who's interested, `event.started` goes to them alone, and the den forgets them as the event ends.
- [x] An event the member can't see and one that's gone get the same answer, and a card for an event to come that the member's Dens keeps is drawn from what it keeps, without asking the den.
- [x] Only staff attach a badge, change it or take it off, until its event ends; its picture is checked as an emoji's, and a move out of a voice channel is refused unless the badge goes with it.
- [x] Earning a badge writes the award alone, who earned which badge and when, which says they were in its call; the stretches are timed in memory. A member's badges travel with their profile card alone, only those whose events the viewer sees, and one they hid in nobody's copy but their own, which a client checks again.
- [x] Nothing about an event or a badge is logged but IDs and counts.

### Staff's tools

- [x] The audit log and when members were last seen answer staff alone, from the den's routes, and nothing of either goes over the socket; a member gets `403`.
- [x] A line holds no message's text and nothing of a DM, where staff can't act, nor what a member does to their own; whose message staff deleted is the one thing a line keeps that a delete otherwise drops.
- [x] What a line says beyond who and when, names as they were among it, is sealed with the data key, bound to the line, and the line goes 90 days after it's written, by the den's timer.
- [x] A client checks each line's IDs, kind, names, changes and emoji as a den's answer, and the page shows them as text, a kind it doesn't know by its name.
- [x] Last seen is told by its day in UTC, less than presence says, and only of members still in the den.

## Messages

### Emoji

- [x] An emoji's name is the den's rule's, `a-z`, `0-9` and `_`, which a client checks again as it arrives, and the page shows it as text and as its image's words; it builds an emoji's image address from the image's ID alone.
- [x] The den takes as an emoji only a still image every page draws, or an animated GIF, at most 128 pixels each way and 256 or 512 KiB, an upload of staff's own waiting to be used; and refuses one past the owner's count.
- [x] An emoji's image is the den's: deleting the emoji takes it, and retention, the sweep of waiting uploads and its uploader leaving leave it.
- [x] A page shows the den's emoji from its install's cache alone, which fetches all of them ahead of their use, so the den doesn't learn which showed, in a DM or anywhere.
- [x] The list of Unicode's emoji is made from inputs pinned by SHA-256, and CI makes it again and fails on a difference.
- [x] Nothing about an emoji is logged but IDs: who added or deleted which.

### Reactions

- [x] A channel's reaction carries no text: the den takes one of Unicode's emoji from the list, or one of its own emoji by ID while emoji are on, and a client checks each again as it arrives; the page shows a den's emoji by its image's ID alone.
- [x] A DM's set is sealed with the DM key, bound to the den, the DM, the message, the member, the key and its revision, and padded to one length; a client keeps only emoji from the list and names a den's emoji may have, and drops anything else, so a hostile partner or den puts nothing in a chip but an emoji, or a name of `a-z`, `0-9` and `_`, as their DM's text could hold.
- [x] A message holds at most 20 different emoji and 10,000 reactions, and each reaction counts toward its member's writes; staff take only the reactions of the members they outrank, and a DM's are its members' alone.
- [x] A DM's set changes only against the revision it was made against, so two devices never drop each other's reactions.
- [x] Nothing about a reaction is logged but IDs, and only staff's: who took how many off which message.

### Polls and @everyone

- [x] The den sends a poll's totals and who voted for what to nobody sooner than its rules allow: in history and what changed while away, as the member asking may see it, and in its events, each batch to an audience of those who may see it, until the end; and a vote after voting is final.
- [x] A poll's choices are plain text, cleaned as names are, which a client checks again as it arrives, with its tally, and the page shows as text; an event whose tally doesn't fit its poll changes nothing. Its choices are sealed at rest with the data key, as its question is, bound to its message.
- [x] A vote and an early end each count toward the member's writes; only a poll's author ends it, and nobody edits one.
- [x] Only staff's @everyone mentions everyone, and only while staff put it there; nobody new takes the username `everyone`.
- [x] Nothing about a poll is logged but IDs: who ended which early.

### Forwarding

- [x] A forward carries nothing of where it came from, to the den or in the seal: its sender's Dens sends the text and uploads each file again as the member's own, stripped again, and sealed anew under a key of its own where the place is sealed, with no author, channel, den, reply or link.
- [x] A sealed place's mark goes inside the seal, so the den isn't told a message was forwarded: the den refuses one outside the seal there, and a client takes none the den set on a sealed message. What's sealed isn't padded, so a file's sealed size and the text's, and when they came, can still match a message the den holds.
- [x] The place's limits hold before anything goes: a place the sender's Dens doesn't hold as a text channel or a sealed place, whose files would go up unsealed, a sealed one with no key to send under, as a DM that holds, a file over its den's size, sealed where the place is, or past the member's space or the den's, sends nothing, and an upload made before a later failure is dropped.
- [x] A forward is its sender's to delete, not to edit, tick or share, at the den and in the sender's Dens. Its sender takes a file off it, and puts none on it or in another's place. An older Dens of the sender's can still edit a sealed one, which drops the mark.

## Sync

### The client's cache

- [x] Everything cached is sealed with the data key, each message and file chunk bound to its den, its ID and its place; a chunk that doesn't open drops its file, and a cache under another key or of another format is emptied, never read.
- [x] Deleted messages and files leave every client's cache: at once while it's connected, and from the changes since its version when it comes back. The den's change log holds IDs and channels only, for 30 days.
- [x] The den sends a member only changes in channels they can see now, and a client drops a channel's cache when it can no longer see it, and a den's when the member leaves, is removed or banned, or the device is signed out.
- [x] A restored den starts a new sync epoch, and a client never keeps a cache across epochs or from below the floor.
- [x] What a client keeps for good, the kinds of its channels, is kept in the cache again at once as the cache drops, since the den chooses when that is, by a new epoch or by saying its changes are gone, and a client that stopped before the state's next change would otherwise start without them. They outlast a kept state that doesn't check at a start too, which the den can have had a hand in: the rest goes, and they go back in alone.
- [x] A den never gives an ID out twice, even after a restore, which moves its counters on past every ID it gave out, so a link names one message for good.
- [x] Compressed frames are held to the same limits as plain ones, as they inflate, and the cache keeps no file over a quarter of its size.

### Disappearing messages

- [x] A message's time is fixed as it's sent, from its channel's as the den reads it within the send's transaction, and an edit leaves it; a client takes a time only on a sealed message, and only after its sending.
- [x] The den deletes each at its time with `secure_delete` on, its files off the disk and its reactions with it, and as it starts what passed while it was stopped; each member's Dens lets its copies go at their time by its own clock, connected or not, and keeps out a copy past it, and the page drops one as well, neither waiting on the den.
- [x] Setting a time counts toward the member's writes, and is logged by IDs and the time alone.

## Notifications and sounds

### Notifications

- [x] Notifications stay off until the member turns a kind on, and the settings are the service's, which only a paired browser changes.
- [x] A notification's text is plain text from the start of the message: no markup reaches a notification service, whose markup a member's text is escaped for, a spoiler never shows, and control and format characters go; the member can leave the text out.
- [x] Nothing about a notification is logged but its IDs.
- [x] A channel's level, a mute and Do not disturb stay on the install, and the den hears nothing of them; a level or a mute is kept only for a channel the member's view holds, and a level only for a text channel's.
- [x] A click opens only the page, at an address the service builds from IDs the den gave it; the Android shell loads only a path of the page's form, and a pairing link is made at the click, never kept in a notification.

### Sounds

- [x] A member's own sound stays on their install: the client router alone serves and takes it, its writes behind the `Origin` check, and it's served as the audio it is, sandboxed and never cached.
- [x] The service takes only audio, by its own look at the bytes, of at most 1 MiB, and keeps it sealed with the data key, bound to its sound.
- [x] Nothing about a sound is logged but which sound changed.

## Files

### Files and retention

- [x] Smaller copies are made on the sender's machine, a photo's by the module and `internal/media` and a video's by the module, or by the browser and muxed by the module, and the den checks them like any upload: images for metadata, and video stripped again.
- [x] Only a file's uploader deletes or swaps it, and a replacement counts against the space its file frees only to take that file's place.
- [x] Retention deletes for real, in channels and DMs: rows with `secure_delete` on, files off the disk, and clients drop what passed the period.
- [x] An animated picture is made on the member's machine, in Go, and refused past its budget before any of its frames is decoded; the den checks it as any upload, and takes one only as a GIF of a picture's sides and size.

### Spoilers

- [x] In a DM a file's mark goes inside the seal, beside its name, so the den never learns which are spoilers: the den refuses one outside the seal there, and a client one the den set on a sealed file.
- [x] In a channel the den keeps the mark on the file, which an edit can't take off, and a file swapped into a spoiler's place is one too.
- [x] A spoiler shows covered, whatever its kind, naming nothing of the file, its name among it: a photo or a video as its preview blurred, nothing of a video playing before it's shown, and any other file as a card. The viewer leaves it out while it's covered, and a link in a spoiler draws no event's card.
- [x] A covered text spoiler covers what's in it, links, mentions and chips too, whose own colors would show through, and holds it inert, so a link in it takes no focus and shows no address on hover, and an event's chip asks for nothing until it's shown.

### Media

- [x] ffmpeg runs only as the translated module, in a worker process with a memory cap, a deadline and a lower Go stack limit, reaching nothing but the functions Dens gives it.
- [x] The module's memory is a slice with no spare capacity, so bulk memory operations trap past its end.
- [x] Dens's C in the module is fuzzed with damaged files under AddressSanitizer in CI, beside the module.
- [x] Stripped files of every kind Dens takes are checked by a reader independent of FFmpeg, exiftool, in CI.

## End-to-end encryption

### Sealed channels

- [x] A sealed place is a DM or a member's notes, which the den can't read; a place several members read is a channel, which the den reads.
- [x] A channel's ID keeps its kind for good: a client remembers the kind of every channel it held or opened, deleted since or not and across its cache, and takes one that comes as another, relabeled or deleted and made again, in an event or a snapshot, as the den breaking the protocol. What's sent to an ID that was a DM or notes is sealed, or fails, and a message that comes there in the clear shows as one that doesn't open, keeping nothing the den wrote in it; one the client kept as a group DM's, which an older den listed, gets nothing, and everything naming it is dropped unread. A device that never held a channel has nothing to hold it to.

### Identities

- [x] A member's identity comes from their seal by HKDF, salted with the den's ID, under a label for each key, so the den holds nothing that makes it, and a member's identity at one den says nothing of them at another.
- [x] The den takes an identity's keys only as keys of their kinds and sizes, and a client takes a member's keys only where they hash to the identity it kept for them.
- [x] A client keeps each member's identity on first use and checks every one it meets after against it. A change shows wherever that member does and takes their mark, and where they were verified the DM holds: the service seals nothing to them, a message, an edit or a reaction, until the member compares again or sends anyway.
- [x] A hold lifts only by the member's choice, on whichever of their devices: the record keeps the identity they sent anyway at, two devices' records merge held where either holds unless either compared or sent anyway at the identity kept, and the trim keeps a held member as a verified one, so a device that never held, as one signed in since, lifts nothing. A device holds only once it has read a record that does, so one signed in before the holding device's next write, or given an older record by its den, seals to the changed identity meanwhile; an older Dens of the member's holds as it did; and a device offline since it sent anyway at an identity lets go of a later hold at that identity, should the den show it again.
- [x] A code comes from the identity's hash, the member's ID and the den's ID by 5,200 rounds of SHA-512, and is checked against the identity kept, never the den's, in constant time; a member's own digits are refused, and comparing asks nothing of the den.
- [x] The record is sealed with a key from the seal, bound to the den, the member and its revision, its length first and padded to a power of four from 16 KiB, and written each hour from a moment picked at random, whether it changed or not; the den checks its size and revision alone, and tells the member's own sessions of it. Each device keeps its copy sealed with the data key.
- [x] A DM's key is 32 random bytes sealed to each member's identity with HPKE, bound to the den, the DM, its ID and the member. A client uses one only where its maker's identity signs its statement, its maker's keys hash to the identity named for them, it names the identity kept for each member, and the copy opens to the key it commits to.
- [x] The den takes a key only where it names the identities published now and its maker's signature checks, keeps one live key a DM, and gives each member their own copy alone.
- [x] Starting over takes the den password.
- [x] Nothing about identities, codes, the record or keys is logged but IDs: never a key's bytes, an identity's hash, a code's digits or the seal.

### Notes to self

- [x] A member's notes are theirs alone: the den shows the channel to them only, one each, and a client takes only one, naming them.
- [x] Their key comes from the seal by HKDF, salted with the den's ID under a label of its own, made by the member's own Dens; the den holds nothing of it, and takes in notes only sealed messages, each naming the channel as its key.
- [x] A note is sealed as a DM's message is, bound to the den, the channel, its author, its nonce, the key and its revision, and one that doesn't open shows as lost.
- [x] Leaving the den, a removal or a ban deletes a member's notes, with their files.
- [x] A Dens speaking version 1 or 2 of the protocol gets nothing of notes.
- [x] Nothing about notes is logged but that a member opened theirs.

## Calls

### Voice

- [x] Only members who can see a voice channel join its call, and a call ends as soon as that stops.
- [x] The local service writes the den's media addresses into every offer, so a den points a member's browser only where its name resolves, and takes the browser's candidates out of every answer, and the addresses in its connection, RTCP and origin lines, so the den never learns members' local addresses.
- [x] The page's peer connection names no STUN or TURN server, and its `Permissions-Policy` grants the microphone to the page alone.
- [x] The local service passes the page only offers within 32 KiB, of audio and of the video [Screen share](#screen-share) allows, and the den takes only answers that fit its offer, within 32 KiB.
- [x] The den forwards only Opus, under its own headers, at most 256 kbps and 500 packets a second from each member.
- [x] The media ports drop packets for no call, close a TCP connection that doesn't name one within 5 seconds, and hold at most 128 TCP connections.
- [x] Pion's log messages, which name addresses, stay out of release logs.
- [x] Only staff of a higher rank disconnect or mute a member, and the den enforces a mute by forwarding nothing from them.
- [x] A held call resumes only on a socket of the device that held it, within 30 seconds, and a socket the den closes for good ends its call at once.
- [x] A call that ended while held stays ended: its resume gets the reason, never a new call.
- [x] Push to talk's ring reaches the call's members alone, never lights for a member muted or staff muted, and lights at most five times a second.
- [x] A member's Away is their own mark, which everyone who sees the call sees, as their muted one; the den takes it only with that one, enforces neither, and keeps it with the call in memory, never written.
- [x] A den takes no join that doesn't say it seals, and passes each member of a call their copy of another's key alone, while both are in it, from a member's frame for the call their socket holds, at most 30 in 10 seconds a member.
- [x] A member's Dens takes a call key only once its statement names their own identity, its signature checks against the identity kept for its maker, and its copy opens to the key it commits to; copies, commitments and statements have contexts of their own, apart from DM keys'.
- [x] The page seals a sender before its track goes on, so nothing leaves in the clear, sends nothing before it has a key, and drops every frame on a member's track that doesn't open under their key, on its track, once, the soundboard's alone passing in the clear; its tests take RFC 9605's own cases. It seals every sender in the task its offer was applied in, puts a track on no sender sealed later, and stops, within about a second, a sender its browser sends around its seal anyway; its tests take a peer connection that treats a late transform as Chromium does.
- [x] The browser holds a call's keys only while it lasts, and the service and the logs name keys by their IDs alone.

### Screen share

- [x] The page's `Permissions-Policy` grants screen capture to the page alone (`display-capture=(self)`), and the page asks the browser's picker to leave out the Dens tab.
- [x] The local service passes the page audio only as Opus and video only as VP9 or AV1, so a den can't have the browser decode any other codec. The den offers VP9; AV1 passes too, so a later den can offer it without breaking older clients.
- [x] Only members of a call watch its shares, and the den forwards a share only while its member shares, only VP9 from their screen's section, under its own headers, within the den's share bitrate.
- [x] A staff mute ends a member's share, and the owner's limits cap the shares at once, a share's viewers and its bitrate, which the den enforces.
- [x] A share's window of its own is a blank page of the page's origin that the page fills, under the page's CSP, and loads nothing but the page's stylesheet; it closes with the share, the call and the page.

### Soundboard

- [x] A sound's file opens for staff alone, by the den's check of who asks, and members get its name; the page hears a sound only on the call's track.
- [x] The den takes only Opus in Ogg, stripped by the media module as any audio is, which it reads itself, one stream in mono or stereo, every page's checksum checked and every packet at most 4 KiB and 120 ms, within the owner's longest and the size its length allows; and it plays only what it checked, under headers of its own.
- [x] One sound plays in a call at a time, a member's next only after their wait, and playing counts toward their writes; a staff mute ends their sound, and leaving ends it.
- [x] A sound's file is the den's: no message, profile picture or replacement takes it, and deleting a member or a message, retention and the sweep of waiting uploads leave it; a database bug that would delete it fails on its foreign key rather than leave a sound with no file.
- [x] Nothing about a sound is logged but IDs: who added or deleted which, and who played which in which channel.

### Call records

- [x] A call's record keeps of it its length alone, as its voice channel's record, never who was in it or when.
- [x] Only staff read a channel's record and reset it, for a voice channel they see, and a call going on as it's reset writes no record.
- [x] A client takes a call's length and its record only as numbers from 0 to 100 years, and a call's start never steps back.
- [x] A reset is logged by member and channel alone.

## The helper and Relay

### Global shortcuts

- [x] The helper watches only the bound keys, and only while a page is in a call or Relay hands them to one, with no keyboard hook, and sends nothing but the bound actions' presses and releases; neither it nor the service logs them.
- [x] The helper's stream is a control request like any other: it answers only the instance's desktop user, by the identity the system reports.
- [x] The page acts on the helper's keys only in a call it holds, and never while the member types a key that types something.
- [x] Each desktop user's helper task runs as them, with their limited token; nothing of the helper runs elevated.

### Relay

- [x] A box between a member's devices is sealed under a key their seal makes, bound to the den, the member and both devices, and one size; the den passes it to that member's other devices alone and keeps nothing of it.
- [x] A box's note is taken within a minute of its time, and an answer only for the hello whose attempt it names.
- [x] A link's addresses are IPv4 ones on private networks, or loopback in a development instance: a box naming any other is refused, and the link gathers no other address, asks no STUN server and listens on nothing.
- [x] The DTLS certificate a link proves is the one its box named, so only a device holding the seal links.
- [x] Local addresses are never logged, written or given to the page, and Pion's messages are dropped.
- [x] Keys go only to an install holding a call, with Keybind relay on at both ends, and a device quiet for 300 ms holds nothing.
- [x] A computer's sound goes only while its member shares it, to the one device of theirs that asked, over the link's SRTP, and the helper reads it only while one listens.
- [x] Listening starts only at a click on the page that listens, and ends with it; the sound's socket checks the page's `Origin`, as the event stream's does, and nothing of it reaches the den.
- [x] The Android app links only once Android lets it reach its network, which the member allows; the call's button's presses go only to the device holding the member's call, as a helper's do.
