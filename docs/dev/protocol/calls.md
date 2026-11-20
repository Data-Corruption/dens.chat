## Voice (M2)

A call is a session between one member's browser and the den, in a voice channel. The den runs the media side. The client relays the signaling below between its socket to the den and its member's browser, and decides where the browser sends media (see the design doc's Voice and screen share).

```
call = {"channel_id", "members": [{"id", "muted"?, "deafened"? (M3.3), "staff_muted"? (M3),
                                   "sharing"? (M4.2), "sound"? (M4.2), "watching"? (M4.2)}, …],
        "elapsed_ms"? (M21.2), "record_ms"? (M21.2)}
```

Client frames, with `resume` and `voice.restart` from M3, `voice.share` and `voice.watch` from M4.2, `voice.talk` from M11, `voice.sound` from M14.3, and `sealed` and `voice.key` from M16.2:

```json
[{"t": "voice.join", "d": {"channel_id": "41", "muted": false, "deafened": false, "sealed": true}}]
[{"t": "voice.join", "d": {"channel_id": "41", "resume": true, "sealed": true}}]
[{"t": "voice.answer", "d": {"version": 1, "sdp": "v=0…"}}]
[{"t": "voice.mute", "d": {"muted": true, "deafened": true}}]
[{"t": "voice.restart", "d": {}}]
[{"t": "voice.share", "d": {"on": true, "sound": true}}]
[{"t": "voice.watch", "d": {"member_id": "7", "on": true}}]
[{"t": "voice.talk", "d": {"on": true}}]
[{"t": "voice.sound", "d": {"sound_id": "3"}}]
[{"t": "voice.key", "d": {"channel_id": "41", "kid": "…", "commit": "…", "members": [{"member_id": "7", "identity": "…"}],
                          "maker": {"kem_hash": "…", "sig": "…"}, "signature": "…", "copies": [{"member_id": "7", "copy": "…"}]}}]
[{"t": "voice.leave", "d": {}}]
```

Den events, all ephemeral, with `voice.resumed` from M3, `voice.refused` from M4.2, `voice.talking` from M11, `voice.sounding` from M14.3, `voice.key` from M16.2 and `voice.move` from M17.2:

```json
{"t": "voice.offer", "d": {"channel_id": "41", "version": 1, "sdp": "v=0…", "udp_port": 7881, "tcp_port": 7882}}
{"t": "voice.resumed", "d": {"channel_id": "41"}}
{"t": "voice.refused", "d": {"channel_id": "41", "what": "watch", "member_id": "7", "reason": "full"}}
{"t": "voice.talking", "d": {"channel_id": "41", "members": ["7"]}}
{"t": "voice.sounding", "d": {"channel_id": "41", "member_id": "7"}}
{"t": "voice.key", "d": {"channel_id": "41", "member_id": "7", "kid": "…", "commit": "…", "members": […], "maker": {…}, "signature": "…", "copy": "…"}}
{"t": "voice.move", "d": {"channel_id": "41", "to": "42"}}
{"t": "voice.ended", "d": {"channel_id": "41", "reason": "moved"}}
{"t": "voice.state", "d": {"calls": [call, …]}}
```

### Joining and leaving

- `voice.join` names a voice channel the member can see. The den starts the member's call on this socket and sends it `voice.offer`, and ends any other call of the member's, on any device, with `voice.ended` and `moved`. `muted` and `deafened` (M3.3) are optional and set the member's marks from the start. With `"resume": true` (M3), the join takes back a call the den holds for this device instead of starting one (see [Riding out a dropped connection](#riding-out-a-dropped-connection-m3)).
- A refused join gets `voice.ended` with `not_found` (no voice channel the member can see has that ID), `full` (the call holds as many members as the den allows, or the den's calls do; see [Limits for calls](#limits-for-calls-m42)), `rate_limited`, `failed` when the den couldn't set the call up, or from M16.2 `update_needed`, for a join that doesn't say `"sealed": true` (see [Sealed calls](#sealed-calls-m162)). An older client takes `update_needed` for `failed`.
- `voice.leave` ends the member's call. When the socket a call started on closes, the den holds the call (see below), unless the den closed that socket for good with 4003 or after a logout. So removals, bans, revoked devices, new passwords, starting over, leaving and logging out end calls at once, and a den restart ends every call.
- The den ends a call with `forbidden` when its member can no longer see the channel, as after a role change or the channel becoming staff-only; with `deleted` when the channel goes; with `failed` when its connection doesn't come up or breaks (below); and with `disconnected_by_staff` when staff disconnect its member (M3).

### Offers and answers

- The den makes every offer, and the client only answers. An offer is `a=ice-lite`, one bundle, Opus for audio and, from M4.2, VP9 for video. The den receives on up to three sections (`recvonly` at the den): the first, the member's microphone; from the member's first share in the call, a video section for their screen; and from their first share with sound, an audio section for that sound. Those two stay for the rest of the call, for later shares. Each other section sends the member something (`sendonly`): another member's audio, with that member's ID as its stream ID (`msid`); a share they watch (M4.2), its video and, if it has sound, its sound, both with `screen-<member ID>` as their stream ID; and from M14.3, from the first offer of every call, the call's soundboard, an audio section with `soundboard` as its stream ID, which the den plays its sounds on (see [The soundboard](#the-soundboard-m143)).
- Every Opus format in a voice section carries `a=fmtp:111 minptime=10;useinbandfec=1;usedtx=1;maxaveragebitrate=<the channel's bitrate>`, which asks the browser to send at up to the voice channel's `bitrate` (M3, M4.2) with in-band error correction, and to send almost nothing while there's nothing to hear (discontinuous transmission, M3.3). A share's sound carries `minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1;maxaveragebitrate=128000` instead (M4.2), and so does the soundboard (M14.3). A re-offer carries them too, whatever the answer said.
- The screen section asks for no more than the den's `share_bitrate` (M4.2), as `b=AS` in kilobits and `b=TIAS` in bits a second, since browsers honor one or the other.
- An offer carries no candidates. The client writes the den's: each address the den's name resolves to, or for a den on its own machine, the machine's addresses on its other interfaces, each with `udp_port` over UDP and `tcp_port` over TCP (`tcptype passive`). The den answers connectivity checks from any address, so it needs no candidates from the client, and the client sends none.
- `voice.answer` answers the offer with that `version`, which counts up from 1 in each call. The den keeps one offer outstanding per call, and sends the next only after its answer. An answer to any other version is ignored.
- When a member joins or leaves a call, each other member in it gets a new offer, which adds their section or retires it (`inactive`). So does a member who starts or stops watching a share, for its sections, and each member of a call whose channel's bitrate changes (M4.2). A later section can take back a retired one of its kind.
- An offer not answered within 15 seconds, a connection not up within 30 seconds of its answer, a connection that fails, or an answer that doesn't fit its offer ends the call with `failed`.
- Offers and answers are each at most 32 KiB. The client passes its browser only offers of audio and video, with audio only as Opus and, from M4.2, video only as VP9 or AV1, every format named, and any other offer fails the call. The den offers VP9; AV1 passes so a later den can offer it without breaking older clients.

### Media

- The den takes only Opus from each member's microphone, on its own section, and forwards each packet's payload to the others under its own headers, without header extensions. It forwards at most 256 kbps and 500 packets a second from each member and drops the rest.
- From a share (M4.2), the den takes only VP9 on the member's screen section, and only Opus on its sound section, and only while they share, and forwards them to the members watching it in the same way. It forwards a share's video at up to its `share_bitrate` and a quarter more, with bursts of up to a second of that or 512 KiB, whichever is more, and its sound within a voice's limits, and drops the rest.
- A viewer's picture loss indication (PLI) or full intra request (FIR) goes to the sharer as a PLI for their screen, and the den sends one as each viewer's section opens, and when a share's packets start again after a pause of half a second or more (M4.2). It sends a share at most one every 500 ms; a request inside that time gets one at its end.
- `udp_port` and `tcp_port` are the den's media ports. Packets that name no call are dropped, and a TCP connection that doesn't name one within 5 seconds is closed. The TCP port holds at most 128 connections at once.

### Who's in a call

- `voice.state` gives the members of each call that changed, to everyone who can see its channel; a call that emptied has none. `muted` is the member's own mark, set with `voice.mute` or at join, which the den passes on but doesn't enforce, and so is `deafened` (M3.3), which says the member hears nothing of the call either; a deafened member is muted too. `staff_muted` (M3) is staff's, which the den does enforce (see [Staff in calls](#staff-in-calls-m3)). `sharing` (M4.2) says the member is sharing their screen, and `sound` that the share has sound; `watching` lists the members whose shares they watch (see [Screen share](#screen-share-m42)).
- `ready` carries `calls`, those the member can see that have someone in them. Calls aren't replayed, so after `resumed` the den sends `voice.state` with `"full": true`, which replaces every call the client held.
- From M21.2 every call the den sends carries `elapsed_ms`, how long it has gone on in milliseconds, from its first member's join, held calls counting, and `record_ms`, its channel's longest call as the call began, absent for none (see [Call records](#call-records-m212)). A client turns `elapsed_ms` into a start on its own clock as the frame arrives, and refuses either below 0 or past a hundred years. An older den sends neither, and a client shows its calls without a length. Additive: no version bump.

### Riding out a dropped connection (M3)

A call's socket can close while its media still flows, or come back after its media broke. Neither ends the call by itself.

- When a call's socket closes, the den holds the call for 30 seconds, unless the den closed the socket for good: with 4003, or after a logout. The member stays in the call with their media, and the den sends no offers. An offer still out waits, without its deadline.
- `voice.join` with `"resume": true` takes back a call the den holds in that channel for the socket's device, the device key its session signed in with, whose socket has closed or is older than this one. The den moves the call to this socket and answers `voice.resumed`. Then it sends the offer that was out again, under its version, which a client that already answered it answers again with the same answer; or it makes a new offer if the call's sections changed meanwhile. The client keeps its peer connection. A resume doesn't count toward joining's rate limit.
- A resume never starts a call. If the device has no call to take back, the den answers `voice.ended`: with the reason its call ended while held, if that was within the last 10 minutes, or `failed` otherwise. After `failed`, the client may join again.
- A held call ends like any other call: when the member joins another, leaves the den or is removed, when its device is signed out, when staff disconnect them, or when they can no longer see the channel. It ends with `failed` once its hold runs out.
- `voice.restart` asks the den to restart ICE, as when the client's media connection broke. The den's next offer carries new ICE credentials: at once, or as soon as the outstanding offer is answered. The client writes the den's addresses into that offer afresh. DTLS carries on, so the call keeps its sections and keys. A restart before the call's first answer is ignored, and a restart counts toward the member's other writes.
- A call whose media stops answering connectivity checks for 30 seconds ends with `failed`, held or not.

### Push to talk (M11)

A member who sends by push to talk rings for everyone in their call while they hold its key, whatever their microphone hears (see the design doc's Voice controls).

- `voice.talk` with `"on": true` says the member holds push to talk in the call this socket holds, and `"on": false` that they let go. A frame from a socket that holds no call is ignored.
- `voice.talking` lists the members of the call who hold push to talk, in the order they joined, and goes to the sockets that hold the call's members' calls alone, as only their pages ring for speech. Each replaces the last. A socket gets it as its call starts and as it takes a held call back, and whenever the list changes.
- The den lights a member's ring at most five times a second; a press past that, or while the member is muted or staff muted, lights nothing, and a release always puts a lit ring out. Muting, a staff mute, leaving the call or moving to another, the call ending and the socket closing, which holds the call, put it out too; a client that takes a held call back says again whether its member holds the key.

### Staff in calls (M3)

```
POST /api/members/{id}/disconnect                        204
POST /api/members/{id}/voice-mute  {"muted": true}       204
POST /api/members/{id}/move        {"channel_id": "42"}  204   (M17.2)
```

- Staff disconnect and mute members of a lower rank, as they remove them: moderators act on members, and the owner on anyone else. Anyone else gets `403 forbidden`, and an ID that isn't a member in the den gets `404 not_found`, as for a move.
- Disconnecting ends the member's call, held or not, with `voice.ended` and `disconnected_by_staff`. It leaves a member in no call as they are. They can join again at once.
- A staff mute makes the den forward nothing from the member, in their call and in any they join, until staff lift it with `"muted": false`, the member leaves the den, or the den restarts. It ends their share, and they can't share while it lasts (M4.2). It can be set whether or not they're in a call. Calls show it as `"staff_muted": true` on the member, beside their own `muted`.
- Moving (M17.2) moves a member's call to another voice channel. Staff move anyone but the owner and themselves, a moderator moving another moderator too (M19.3); anyone else gets `403 forbidden`. The den checks what joining does: a channel that isn't a voice channel the member can see gets `404 not_found`, and one whose call holds the den's `members` already `409 call_full`. A call held while its socket is away has nothing to tell, and gets `409 call_held`. Otherwise the den sends `voice.move {"channel_id", "to"}` to the socket holding the call, the call's channel and the one to join, and answers 204; the member's client joins `to` as if they'd picked it, with `voice.join`, sealed, muted and deafened as they were, which ends the old call. That join, within 30 seconds, doesn't count toward the member's limit on joining. A member in no call, or in that channel's already, stays as they are, with 204. A den that moves members says `"move_members": true` in its info, so a client offers it only then; a client older than it ignores `voice.move`, and stays where it is.

### Screen share (M4.2)

A member in a call shares their screen, a window or a tab with the members of the call who choose to watch it (see the design doc's Screen share).

- `voice.share` with `"on": true` starts a share in the call this socket holds, and `sound` says whether the client will send its sound. The den adds whichever of the screen and sound sections the member's offers don't have yet, and sends a new offer if it added one, then marks the member `sharing` in `voice.state`. The client sends its screen on the den's video section, and its sound on the den's second `recvonly` audio section. `"on": false` stops the share: the den forwards nothing more from it, and each viewer's next offer retires its sections. A frame from a socket that holds no call is ignored.
- A share is refused with `voice.refused`, `"what": "share"`, and a reason: `off` when the den's `shares` is 0, `full` when that many members are sharing already, `staff_muted` while staff mute the member, or `rate_limited`.
- A share ends when its member stops it, leaves the call or moves to another, when their call ends, and when staff mute them; `voice.state` then shows them without `sharing`. Lowering a limit ends no share.
- `voice.watch` with `"on": true` watches the share of `member_id`, who must be sharing in the same call; the den adds the share's sections to the member's next offer and lists the sharer in their `watching`. `"on": false` stops watching. Leaving the call, or the share ending, stops it too, and a member doesn't watch their own share.
- A watch is refused with `voice.refused`, `"what": "watch"`, the `member_id`, and a reason: `full` when the share has `share_viewers` viewers already or the member watches four shares, `not_sharing` when that member isn't sharing in this call, or `rate_limited`.
- Sharing and watching count toward the member's other writes.

### Limits for calls (M4.2)

```
call_limits = {"members", "callers", "shares", "share_viewers", "share_bitrate", "share_height", "share_fps"}
```

- `members` is how many members one call holds, 2 to 30, and `callers` how many may be in calls across the den, 2 to 100. A new den allows 15 and 30. A call holds at most 30 so that its offers fit in 32 KiB.
- `shares` is how many members may share at once across the den, 0 to 10, where 0 allows none, and `share_viewers` how many may watch one share, 1 to 29. A new den allows 1 and 8.
- `share_bitrate` is the most a share's video may send, in bits a second, 250,000 to 50,000,000; a new den allows 2,000,000. The den asks for no more in its offers and holds shares to it (see [Media](#media)).
- `share_height` is a share's size, as the height of a 16:9 picture: 720, 1080, 1440 or 2160. A share may have as many pixels as that picture, whatever its shape. `share_fps` is its frame rate, 5 to 60. A new den allows 1080 and 30. The client keeps its member's shares to them, since the den can't see either.
- The owner changes them with `PATCH /api/den`, all together. A lower limit applies to what starts afterwards: nobody leaves a call or a share for it.
- Clients check `call_limits` as they arrive, as they check `limits`, and refuse a den whose limits are out of range.

### Sealed calls (M16.2)

A den with M16.2 seals every call end to end: each member's page seals its frames with SFrame (RFC 9605, AES_128_GCM_SHA256_128) under a key of the member's own for the call, which their Dens makes, seals to each other member's identity and sends as `voice.key` (see the design doc's End-to-end encryption, Calls). The den forwards frames it can't open.

- The den's info, in `ready` and `den.updated`, says `"sealed_calls": true`; an older den says nothing, and its calls go unsealed. A den that seals calls refuses a `voice.join` without `"sealed": true` with `voice.ended` and `update_needed`: an older page would send in the clear, which every other page drops. A resume takes back only a call the den holds, every one of which was joined sealed. A page that joined while its den didn't seal, which ended the call as it updated, joins again sealed.
- `voice.key` from a member's Dens carries their key for the call this socket holds, in `channel_id`: its random 16-byte ID (`kid`), a commitment to it (`commit`, SHA-256 of `"dens-call-key-commit-v1"`, the ID and the key), each member it was sealed to with the hash of the identity it was sealed to, in the order of their IDs and its maker not among them, its maker's identity as the hash of its X-Wing key and its Ed25519 key (`maker`), the maker's signature over `"dens-call-key-v1"` ‖ den ID ‖ channel ‖ kid ‖ commit ‖ maker ‖ how many members ‖ each member and identity hash, and a copy for each member named, 1,152 bytes of HPKE (X-Wing, HKDF-SHA-256, ChaCha20-Poly1305), bound to `"dens-call-key-copy-v1"`, the den's ID, the channel, the key's ID and the member. The den checks its shape alone, and that the socket holds a call in that channel: one that doesn't, or past the limit, goes nowhere.
- The den passes each member the copy it holds for them alone, as `voice.key` with the maker's `member_id`, the statement, the signature and `copy`, on the socket that holds their call, while both are in it. It keeps each member's latest while their call lasts, and passes each a member holds a copy for to them as their call starts, as when they move it to another device, or as they take a held call back.
- A client takes another's key only once its statement names the member's own identity, the signature checks against the identity it kept for the maker, and its copy opens to the key `commit` names; it closes the connection, as the den breaking the protocol, on a `voice.key` of the wrong shape, and refuses one that fails a check. A key's number in its frames' SFrame headers is its ID's first two bytes; each track's key ID is four times the number and the track's, 0 the voice, 1 the screen and 2 the share's sound.
- `voice.key` has a limit of its own, 30 in 10 seconds a member (see [Rate limits](#rate-limits)). Additive: no version bump.

### The soundboard (M14.3)

Staff add sounds to the den's soundboard, and a member in a call plays one for everyone in it, on the call's soundboard track (see the design doc's The soundboard).

```
sound        = {"id", "name", "file_id", "duration_ms", "added_by", "added_at"}
sound_limits = {"sounds", "longest", "wait"}

POST   /api/soundboard        {"name", "file_id"}   201 sound        (staff only)
PATCH  /api/soundboard/{id}   {"name"}              200 sound        (staff only)
DELETE /api/soundboard/{id}                         204              (staff only)
```

- A sound is one of the den's named files: its name, 1 to 32 characters cleaned as display names are and unique in the den in any case; its file, an upload; its length in milliseconds, as it plays; and who added it and when. `ready` carries every sound, by name, and each change reaches every member as `sound.created {sound}`, `sound.updated {sound}` or `sound.deleted {"id"}`. A sound's file opens with `GET /api/files/{id}` for staff alone, and anyone else gets `404 not_found`; members never need it, since the den plays it.
- `POST /api/soundboard` takes an upload of the staff member's own, waiting to be used, and makes it the sound's file, which then counts toward the den's space, not theirs, isn't among their files, and stays until staff delete the sound: retention, the sweep of waiting uploads and the uploader leaving don't remove it, and no message, picture or replacement takes it. The upload must be Opus in Ogg (`audio/ogg`), one stream in mono or stereo, no longer than `longest` seconds, give or take its last packet, and no larger than its length at 64 kbps makes, with a quarter more and 4 KiB; the den reads every page of it to check. A name another sound has, any other upload, or a soundboard holding `sounds` sounds already gets `400 invalid_field`, as does a soundboard that's off; anyone but staff gets `403 forbidden`.
- `PATCH` renames a sound, under the same rule for names; `DELETE` deletes it with its file. A sound that's playing plays to its end.
- `voice.sound` plays `sound_id` in the call this socket holds. The den checks that the soundboard is on, the member isn't staff muted, no sound plays in the call, and the member's wait has passed, `wait` seconds from the end of their last sound; then it sends the sound's Opus packets, as its file holds them, on the call's soundboard section, at their pace, by each packet's table of contents. One plays in a call at a time. A frame from a socket that holds no call is ignored.
- A sound is refused with `voice.refused`, `"what": "sound"`, the `sound_id`, and a reason: `off` when the den's `sounds` is 0, `staff_muted` while staff mute the member, `playing` while a sound plays in the call, `wait` before the member's wait has passed, `not_found` for a sound the den doesn't have, `rate_limited`, or `failed`. A client passes a sound's reasons on only for a sound, and calls any other a failure.
- A sound ends at its end, or early as its player leaves the call or moves to another, as their call ends, and as staff mute them; nothing else stops it.
- `voice.sounding` names the member whose sound plays in the call, or with no `member_id` none, and goes to the sockets that hold the call's members' calls alone. Each replaces the last. A socket gets it as a sound starts and ends, as its call starts and as it takes a held call back.
- `sound_limits`: `sounds` is how many sounds the den keeps, 0 to 500, where 0 turns the soundboard off and keeps its sounds; `longest` is the longest a sound may be, 1 to 300 seconds; and `wait` how long a member waits after their sound ends before playing another, 0 to 600 seconds. A new den allows 50, 10 and 5. The owner changes them with `PATCH /api/den`, all together. A lower limit applies to sounds added afterwards: the sounds already added stay.
- An older den sends no `sound_limits`, no `sounds` and no soundboard section, and clients show no soundboard for it; clients check `sound_limits` as they arrive, as they check `call_limits`, and refuse a den whose limits are out of range. An older client plays the soundboard's section as it would a member's voice.
- Playing a sound, and adding, renaming and deleting one, count toward the member's other writes.

### Call records (M21.2)

A voice channel keeps its longest call, which each call's `record_ms` gives as the call began, and which staff read and reset.

```
GET    /api/channels/{id}/record   200 {"record_ms"}   (staff only)
DELETE /api/channels/{id}/record   204                 (staff only)
```

- A call that ends longer than the record becomes it, the den's stop ending it too, and the den writes a call's length at each whole minute past the record it kept, so a den that stops without ending its calls loses a minute at most. `record_ms` is 0 while the channel has none.
- Anyone but staff gets `403 forbidden`, and a channel that isn't a voice channel the staff member sees `404 not_found`.
- `DELETE` clears the record. A call going on then gets `voice.state` with no `record_ms`, and writes no record as it ends; the next call to end sets one. Resetting counts toward the member's other writes.
