# Den protocol

How a client talks to a den: HTTP requests for commands and history, and one WebSocket for live events. The design doc explains the reasons; this is the reference to implement and review against. It covers M1.1 (join, sessions, the WebSocket), M1.2 (channels and messages), M1.3 (members, roles, DMs, presence and typing), M1.4 (files), M1.5 (new devices and recovery), M1.6 (shared messages), M1.7 (private DMs and approving new devices), M2 (voice), M3 (links, calls that ride out a dropped connection, and staff in calls), M4.2 (screen share and the limits for calls), M5 (retention and managing files), M6 (compressed frames, what changed while away and the members changed since) and M11 (links to messages) in full. The sections for later steps are outlines, filled in with their step.

Terms used below:

- **Client:** the local Dens service acting for its desktop user. Browsers never talk to a den. The browser-facing API is internal to the binary and not part of this protocol.
- **Den:** the den listener of the install hosting the den, reached through Caddy at `https://<domain>`, or at `http://127.0.0.1:<port>` by the owner's own client and other instances on the same machine.
- **Member:** an account on a den. A member has one or more **devices**, each holding its own Ed25519 key.

## Conventions

- Bodies are JSON in UTF-8. Receivers ignore unknown fields and unknown event types, so adding either never bumps the protocol version. They still check every known field's type and limits, and reject a body that fails.
- IDs are decimal strings (`"1000123"`). They come from the den, increase with time within a den, and are never reused, even after a restore from a backup, which moves the den's counters on to the restore's time in microseconds (M11). The den's own ID, `den_id`, is another kind (see [Identity and keys](#identity-and-keys)).
- Times are integers: milliseconds since the Unix epoch, in the den's clock.
- Keys, nonces, signatures and tokens are base64url without padding.
- Every request and response is at most 1 MiB, except uploads and files (see [Files](#files-m14)).
- `‖` means byte concatenation. Every field it joins has a fixed length, except a URL at the end, so the result is unambiguous.

## Versioning

The client sends `Dens-Protocol: <n>` on every request and on the WebSocket upgrade, where `n` is the highest version it speaks. The den replies with `Dens-Protocol-Range: <min>-<max>`.

- If `n` falls in the range, the request proceeds at version `n`.
- If `n` is above the range, the client retries once at the den's `max` if its own minimum allows it, and goes at it from then on, until a response's range says the den speaks a newer one, as once it updates. A socket keeps the version its upgrade went at, so a client whose upgrade's answer shows another version they share opens the socket again at it, once.
- Otherwise the den answers `426` with the error code `protocol_unsupported`, and the client shows "this den needs a newer or older Dens" with both ranges.

This document describes version 3, which M17.3 brought with notes to self (see [Notes to self](#notes-to-self-m173)), after version 2, which M16.3 brought with group DMs (see [Group DMs](#group-dms-m163)). A den serves 1 to 3 (`Dens-Protocol-Range: 1-3`): to a request or socket at version 1, it sends nothing of group DMs or notes, and at version 2 nothing of notes, in `ready`, events or answers, and their routes answer `404 not_found`, since an older client takes a channel of more than two members, or of a kind it doesn't know, or a message with more than two members' sets of reactions, for the den breaking the protocol. Everything else is as at version 1.

## Errors

Failures use HTTP status codes with a body of `{"error": {"code": "…", "message": "…"}}`. `code` is stable and meant for programs. `message` is English for logs and never shown verbatim to members, since it comes from the den. Nor is `code`, unless it's one of those below: a client shows any other only as the den refusing, with the HTTP status (M8).

| Status | Codes |
| --- | --- |
| 400 | `malformed`, `invalid_field` |
| 401 | `unauthorized` (missing, unknown or expired token), `bad_signature`, `bad_nonce` |
| 403 | `forbidden`, `banned`, `key_revoked`, `wrong_password` (a signed-in member's password or recovery code), `no_other_device` (a password sign-in with no other device to approve it) |
| 404 | `not_found` |
| 409 | `username_taken` (checked only after the invite, so only invite holders can test names), `edit_conflict` (an edit, a tick, or from M15.3 a DM's set of reactions, made against another revision, and from M16.1 a member's record written over another), `key_exists` (a DM has a live key both its members can use, or a sign-in waiting for approval isn't at that step), `sound_name_taken` (another of the den's sounds has the name, in any case; M14.3), `emoji_name_taken` (another of the den's emoji has the name; M15.2), `call_held` (a move of a call held while its socket is away; M17.2), `call_full` (a move to a call that holds as many members as the den allows; M17.2) |
| 410 | `invite_invalid` (unknown, expired or used; one code so invites can't be probed), `changes_gone` (a version the den no longer has the changes since; M6) |
| 413 | `too_large` |
| 415 | `unsupported_type` |
| 426 | `protocol_unsupported` |
| 503 | `den_not_created` (the install hosts a den, but it hasn't been set up yet) |
| 429 | `rate_limited`, with a `Retry-After` header in seconds |
| 507 | `quota_exceeded` (the member's space), `den_full` (the den's space, or its disk) |

Login and recovery failures use `401 unauthorized` whether the username exists or not.

## Identity and keys

- **Den identity key:** Ed25519, generated when the den is created. `den_id` is `SHA-256(public key)`, 32 bytes.
- **Device key:** Ed25519, one per device per den, generated by the client. `key_id` is `SHA-256(public key)`, 32 bytes.
- **Invite string:** `dens1:` followed by base64url of `den_id (32) ‖ code (16) ‖ den URL (UTF-8, the rest)`. The client pins `den_id` and refuses a den that can't prove it.
- **Password verifier:** `Argon2id(password, salt = SHA-256("dens-den-password-v1" ‖ den_id ‖ username))`, with t=3, m=64 MiB, p=4 and a 32-byte output. `username` is lowercase ASCII, per the name rules in the design doc. The den stores `SHA-256(verifier)` and compares in constant time.
- **Recovery codes:** 10 per member, each 10 random bytes (80 bits) shown as 16 base32 characters in four groups, such as `K7QM-2XRD-9HVT-LC4P`. The den stores `SHA-256` of each. A code works once.
- **Den ID as shown:** the first 10 bytes of `den_id`, written like a recovery code. Members compare it to check they reached the same den, as after signing in by address.
- **DM seal (M1.7):** 32 random bytes a member's clients hold and never send to a den. `seal_check = HMAC-SHA-256(key = HKDF-SHA-256(seal, salt = den_id, info = "dens-dm-seal-v1"), "dens-dm-seal-check-v1")` identifies it at one den without revealing it. Members see it written like a recovery code, in 13 groups of four.
- **Identity (M16.1):** two key pairs made from the DM seal for one den. The HPKE key is X-Wing's (`MLKEM768X25519`, KEM 0x647a): `DeriveKeyPair(HKDF-SHA-256(seal, salt = den_id, info = "dens-identity-kem-v1"), 32 bytes)`, a 1,216-byte public key. The signing key is Ed25519's, whose seed is `HKDF-SHA-256(seal, salt = den_id, info = "dens-identity-sig-v1")`, 32 bytes. `identity = SHA-256("dens-identity-v1" ‖ SHA-256(kem) ‖ sig)` names it.
- **Call key (M16.2):** 16 random bytes, SFrame's base key for AES_128_GCM_SHA256_128, with a random 16-byte ID. Its copy is HPKE as an identity's DM key copy is, with `info = "dens-call-key-copy-v1" ‖ den_id ‖ channel ‖ kid ‖ member`, 1,152 bytes; its commitment `SHA-256("dens-call-key-commit-v1" ‖ kid ‖ key)`. SFrame derives each track's AES key and salt from it with RFC 9605's labels, under the key ID `4 × n + track`, where `n` is the ID's first two bytes, big-endian.
- **Code (M16.1):** a member's, 20 digits. `h = SHA-512("dens-code-v1" ‖ den_id ‖ member ‖ identity)`, then 5,200 times `h = SHA-512(h ‖ identity)`; the first 20 bytes, as four big-endian 40-bit numbers, each modulo 100,000, make five digits each. Members see it in groups of four. A client compares it with nothing from the den but the identity's hash.

## Objects

```
den    = {"id": "<den_id>", "name", "url", "limits"?, "call_limits"? (M4.2), "retention"? (M5), "sound_limits"? (M14.3),
          "emoji_limits"? (M15.2), "sealed_calls"? (M16.2), "disappearing"? (M16.4), "spoilers"? (M17.2), "move_members"? (M17.2),
          "event_limits"? (M17.4)}
member = {"id", "username", "display_name", "role": "member" | "moderator" | "owner", "joined_at", "left_at"?,
          "avatar"?, "banner"?, "identity"? (M16.1)}
image  = {"id", "width", "height", "animated"? (M15.1)}
```

`url` is the den's public URL, which the owner sets when creating the den. Clients build invite strings from it, and the den signs it into every challenge answer (see [Authentication](#authentication)). Den names follow the display name rules, up to 32 characters. `limits` are the den's upload limits (see [Files](#files-m14)), `call_limits` its limits for calls and screen shares (see [Limits for calls](#limits-for-calls-m42)), `retention` its retention period in days, when it has one (see [Retention](#retention-m5)), `sound_limits` its soundboard's (see [The soundboard](#the-soundboard-m143)), `emoji_limits` its limit for emoji (see [Emoji](#emoji-m152)), `sealed_calls` that it seals its calls end to end (see [Sealed calls](#sealed-calls-m162)), `disappearing` that it deletes a DM's and a group's messages at the time their members set (see [Disappearing messages](#disappearing-messages-m164)), `spoilers` that it keeps which of a channel's files went as spoilers (see [Attachments](#attachments)), `move_members` that staff move members from one call to another (see [Staff in calls](#staff-in-calls-m3)), and `event_limits` its limit for members' events, which says it has events (see [Events](#events-m174)); they come in `ready` and `den.updated`, and not in a join preview. `avatar` and `banner` are the pictures on a member's profile (M1.4), each an `image`: its upload, its size, and `animated` when it's an animated GIF (M15.1). `identity` is the identity the member's client published, by its 32-byte hash (M16.1; see [Private DMs](#private-dms-m17)), until they leave or start over.

```
PATCH /api/den   {"name"?, "url"?, "limits"?, "call_limits"?, "retention"?, "sound_limits"?, "emoji_limits"?, "event_limits"?}   200 den   (owner only)
```

A change reaches every member as a `den.updated {den}` event.

`ready` carries `members`: everyone who has been in the den, so clients can name authors and resolve mentions; from M6.2, to a client that kept the list, only those whose records changed since (see [What changed while away](#what-changed-while-away-m6)). A member who left or was removed keeps their record with `left_at` set. A new or returning member reaches everyone as `member.joined {member}`; see [Members and roles](#members-and-roles-m13) for the rest.

## Authentication

Every sign-in starts with a challenge, in which the den proves its identity key and hands out a nonce:

```
POST /api/auth/challenge
{"client_nonce": "<32 bytes>"}

200 {"nonce": "<32 bytes>", "den_key": "<Ed25519 public key>", "den_sig": "<64 bytes>", "url": "https://den.example.com"}
```

- `den_sig = Sign(den_key, "dens-den-v1" ‖ client_nonce ‖ nonce ‖ url)`, where `url` is the den's public URL in UTF-8. It comes last, so it needs no length.
- Before sending anything else, the client checks `SHA-256(den_key) == den_id`, the signature, and that `url` is the address it sent the challenge to. Addresses compare after normalizing: lowercase host, no trailing slash. A client signing in by address, with no invite to pin `den_id`, takes the key the den proves (see [Password and recovery](#password-and-recovery-m15)).
- `nonce` is valid for 60 seconds and one use. The den holds outstanding nonces in memory, with a bounded count.

**Following a move.** A signed `url` other than the address the client reached means the den has moved, or that a server at another address is passing the den's answers along. Either way the client sends nothing more there. It runs the challenge again at `url`, and goes on only if the den proves the same key there and signs that same address. It keeps the new address from then on, and follows one move per challenge: a den whose new address signs yet another is refused.

- A server can pass a den's challenge answer through, but can't change the address signed into it, so a proof, verifier or token never goes to an address the den didn't sign.
- A client that reaches the den its own install hosts over loopback skips the address check: that den signs its public address, and nothing sits between the two.
- A connected client that gets `den.updated` with a new `url` runs the challenge there at once, and moves if the den proves itself there, so its token stops going to the old address. If the new address doesn't answer yet, the client stays where it is until its next challenge sends it on. Either way it keeps, and shows, a new address only once the den has proven itself there.
- A den chooses the addresses its members' clients ask, so a hostile one could turn them into a crowd asking someone else's server, or their own computers' services (M8). A client checks a new address for a den at most once a minute, and refuses a move from an `https` address to a plain `http` one, or from an address elsewhere to one on its own computer. A move it refuses or puts off leaves the den offline at its old address, saying why.
- The den never answers with a redirect, and a client follows none (M8).
- A den that moved keeps answering members at its old address only while the owner keeps that address pointing at it. A member whose install was away longer enters the new address by hand, where the same checks apply.

Requests that use a nonce prove the device key with:

```
proof = Sign(device_key, "dens-auth-v1" ‖ den_id ‖ key_id ‖ nonce)
```

The context string and `den_id` keep a proof from being replayed at another den, and the single-use nonce keeps it from being replayed at this one.

### Join

```
POST /api/join/preview
{"invite": "<code>"}
200 {"den": {"id", "name"}}

POST /api/join
{"invite": "<code>", "username": "alice", "display_name": "Alice",
 "verifier": "<32 bytes>", "public_key": "<32 bytes>", "device_label": "Pearl (Windows)",
 "nonce": "<32 bytes>", "proof": "<64 bytes>", "seal_check": "<32 bytes>"}
201 {"member": {…}, "token": "<32 bytes>", "expires_at": 1759000000000,
     "recovery_codes": ["ABCD-EFGH-…", …]}
```

- The client runs the challenge before preview, so a member never sees a name from a den that failed its identity check.
- The invite is consumed only by a successful join.
- `seal_check` identifies the DM seal the joining client holds (see [Private DMs](#private-dms-m17)). A former member who comes back with another seal starts over (see [Starting over](#starting-over)).
- **Owner bootstrap:** creating a den also creates a one-use owner invite, which the owner's own client redeems over loopback. The den makes the first member to redeem it the owner. There is no second path for creating the owner account. If that first join fails, the page can have the install issue a fresh owner invite, but only while the den has no owner. From M8 the den's machine also makes one for a den that has an owner, with the local password (see [Invites](#invites)).

### Invites

```
POST   /api/invites          {"expires_in": 86400, "max_uses": 1}   201 {"id", "code", "created_by", "expires_at", "max_uses", "uses"}
GET    /api/invites                                                 200 {"invites": [… without code …]}
DELETE /api/invites/{id}                                            204
```

- The client builds the invite string from `den.id`, `code` and `den.url`, and shows it once. The den stores only `SHA-256(code)`, so a code can't be shown again, only revoked.
- `expires_in` is at most 30 days. `max_uses` is 1 to 100 and defaults to 1.
- Moderators and the owner create, list and revoke invites. The list names who made each (`created_by`).
- An owner invite has the owner role, and only the den's own machine makes one: as the den is created, from the page or with `dens den create` (M10), and with `dens den owner-invite` (M8), which refuses while hosting is off (M9.1). It lasts an hour and is used once, and doesn't appear in the list. Redeeming it makes the joiner the owner and the owner before them a moderator: the joiner reaches everyone as `member.joined`, as any joiner does, and the owner before them as `member.updated`.

### Sessions

```
POST /api/auth/login
{"key_id": "<32 bytes>", "nonce": "<32 bytes>", "proof": "<64 bytes>"}
200 {"token": "<32 bytes>", "expires_at": …}
```

- The token goes in `Authorization: Bearer <token>` on every other request and on the WebSocket upgrade. The den stores `SHA-256(token)`.
- A token lasts one hour. The client renews it on the open WebSocket (see below), so there are no refresh tokens.
- `POST /api/auth/logout` ends the session. Revoking a key, or banning or kicking a member, ends its sessions and closes their sockets.

### Password and recovery (M1.5)

A member signs in on a device new to the den with their den password, or with a recovery code if they forgot it. Both start with a challenge, and prove the new device's key over its nonce, as join does.

```
POST /api/auth/password
{"username": "alice", "verifier": "<32 bytes>", "public_key": "<32 bytes>", "device_label": "Pearl (Windows)",
 "nonce": "<32 bytes>", "proof": "<64 bytes>", "offer": {…}}
202 {"pending_token": "<32 bytes>", "expires_at": …}

POST /api/auth/recover
{"username", "recovery_code": "K7QM-2XRD-9HVT-LC4P", "new_verifier", "public_key", "device_label", "nonce", "proof"}
200 {"token": "<32 bytes>", "expires_at": …, "den": {…}, "member": {…}, "recovery_codes_left": 9, "signed_out": 1,
     "seal_check": "<32 bytes>"}
```

- A password alone doesn't add a device: the sign-in waits for one of the member's other devices to approve it, and `offer` starts the exchange the two run (see [Approving new devices](#approving-new-devices-m17)). A member with no other device gets `403 no_other_device`, and signs in with a recovery code instead.
- Recovery needs no approval. It registers the new key under its label and starts its session, as an approved sign-in does, and the den tells the member's other sessions with `device.added {device}`, so a sign-in the member didn't make shows on the devices they have.
- `seal_check` identifies the member's DM seal, which a device signed in with a recovery code doesn't have: the member types the one they saved, which the client checks against it, or starts over.
- Recovery spends the code and makes `new_verifier` the password, so the old one stops working. The den takes a code in any case, with or without its dashes.
- A new password, set by recovery or by a change, signs out every other device of the member, since anyone who had the old password may have signed one in with it. The den deletes their keys and sessions and closes their sockets with 4003 `password changed`, and `signed_out` says how many there were. It also cancels the member's sign-ins waiting for approval.
- Only a member in the den now signs in this way. One who left or was removed comes back with an invite; a banned one can't.
- Every failure is the same `401 unauthorized`: an unknown username, a wrong password or code, or a member who left.
- The client lowercases the username before deriving the verifier, as at join, so the member can type it in any case.

Signed in, a member changes their password or replaces their recovery codes:

```
POST /api/me/password
{"verifier": "<32 bytes>", "new_verifier": "<32 bytes>"}  or  {"recovery_code", "new_verifier"}
200 {"recovery_codes_left": 9, "signed_out": 1}

POST /api/me/recovery-codes
{"verifier": "<32 bytes>"}
200 {"recovery_codes": ["ABCD-EFGH-…", …]}
```

- A password change takes the current password's verifier or a recovery code, which it spends, but not both. The device making it stays signed in, and every other one is signed out.
- New codes take the password, so a stolen session can't make codes to keep the account with. They replace every old code, and the client shows them once.
- A wrong password or code gets `403 wrong_password`. It counts against the username's guesses like a sign-in (see [Rate limits](#rate-limits)).

### Devices (M1.5)

```
GET    /api/me/devices            200 {"devices": [device, …], "recovery_codes_left": 9}
DELETE /api/me/devices/{key_id}   204

device = {"key_id", "label", "created_at", "last_seen_at", "current"?}
```

- `last_seen_at` is the key's last sign-in. `current` marks the device making the request.
- Revoking a key ends its sessions and closes its sockets at once, with 4003 `key revoked`. Revoking the current device signs it out. A key that isn't the member's gets `404 not_found`.
- The member's own sessions get `device.removed {"key_id"}`, for a revoked key and for each one a new password, or starting over, signs out. The `device.*` events are durable, and go to nobody else.

## The WebSocket

```
GET /api/ws?resume=<epoch>.<seq>&members=<sync epoch>.<version>
Authorization: Bearer <token>
Dens-Protocol: 2
```

`resume` is left out on a first connection, and `members` by a client that kept no list of members (M6.2; see [What changed while away](#what-changed-while-away-m6)). Frames are JSON text, each an array of one or more events, of at most 1 MiB from the den and 64 KiB from the client:

```json
[{"t": "message.created", "seq": 5021, "d": {…}}, {"t": "read_state.updated", "seq": 5022, "d": {…}}]
```

The den keeps each frame under 1 MiB: events queued behind a large one go in the next frame.

From M6, client and den negotiate permessage-deflate without context takeover: either side may compress a frame of more than 512 bytes, each on its own, as `ready` and replays are. The limits apply to a frame as it inflates.

- **Durable events** record a change a member can see, and carry `seq`. `seq` increases strictly within a den epoch. It has holes, because a member only receives the events they're allowed to see.
- **Ephemeral events** (presence, typing, `auth.renewed`) carry no `seq`, and are never replayed.
- Events apply in order: across frames, and within a frame's array.

### Starting and resuming

The den's first frame is one of:

- `ready`: a full snapshot. It says the client's state is gone or never existed, and replaces everything the client held for this den. `d` holds `epoch`, `seq` (the resume point), `den`, `me`, `members`, `channels` (the member's DMs among them, and from M16.3, at version 2, their group DMs), `groups`, `read_states` and `online`; from M1.7, `seal_check`, `dm_keys` (the keys of the member's DMs, without their exchanges) and `device_requests` (sign-ins waiting for their approval); from M2, `calls` (see [Voice](#voice-m2)); from M6, `sync` (see [What changed while away](#what-changed-while-away-m6)); from M14.3, `sounds`, the den's soundboard's, by name (see [The soundboard](#the-soundboard-m143)); and from M15.2, `emoji`, the den's emoji, by name (see [Emoji](#emoji-m152)); and from M16.1, `signed_keys`, the keys of the member's DMs sealed to identities, each with their own copy, which a den older than M16.1 doesn't send at all, as a client tells it keeps no identities (see [Private DMs](#private-dms-m17)); and from M17.4, `events`, the den's events to come and live that the member sees, each with their own mark (see [Events](#events-m174)).
- `resumed`, followed by every durable event the member can see after the given `seq`, then the live stream.

The den resumes when `epoch` matches its current process and every event after `seq` is still in its ring. The ring holds the last 10 minutes or 50,000 events, whichever is less. Otherwise it sends `ready`.

The den may also send `ready` mid-stream when what the member can see changes, such as a role change or a channel becoming staff-only. The client handles it the same way: drop and replace. A member whose view changed that way while they were offline gets `ready` on reconnect too, rather than a resume that skips the change.

The client remembers `epoch` and the `seq` of the last durable event it applied, and sends them on every reconnect. After a `ready`, it keeps nothing from before but what it cached, which it checks against what changed while it was away (M6), and refetches only what's on screen.

### What changed while away (M6)

A client that caches what a den sends asks, after each `ready`, what changed in it meanwhile. The den keeps a log of its changes, by ID only, for 30 days.

```
sync = {"epoch": "<16 bytes>", "version": 1300, "floor": 1000, "expired"?: "<message id>", "members_since"?: 1200}

GET /api/changes?since=<version>&upto=<message id>
200 {"version": 1300, "messages": [message, …], "deleted": [{"id", "channel_id"}, …], "more": false}
```

- `version` counts the den's changes: each message edited (its text, files or editors, or a tick) or deleted, from M16.4 as its time came too, or from M15.3 whose reactions changed, or from M15.4 whose poll's votes changed or that ended, its revision as it was, and from M6.2 each member whose record changed (joining, leaving or removed, a name, a picture or a role), in the order they happened. `epoch` names the den's run of versions; a den restored from a backup starts a new one, since the backup can give a new message an ID a client holds for another. `floor` is the oldest version the den still has the changes since, which moves up as it forgets changes older than 30 days. `expired` is the last message retention deleted, once it has deleted any (see [Retention](#retention-m5)).
- A client keeps what it cached from an earlier `ready` only when its version is from the same `epoch`, no older than `floor` and no newer than `version`; otherwise it drops it. It takes the `version` of a `ready` that finds it holding nothing.
- **Members since (M6.2).** A client that kept the list of members from a `ready` names the epoch and version of that `ready` on the socket's upgrade, as `members`. When the den can still say what changed since, `ready` carries only the members whose records changed after that version, and says so with `sync.members_since`, the version they changed after; the client changes its list by them and takes the new `version` for its list. Otherwise `ready` carries everyone, without `members_since`. A `ready` the den sends mid-stream goes by the same cursor, which the socket's own events kept current.
- `GET /api/changes` returns the messages edited or deleted after `since`, in channels the member can see now, and with IDs up to `upto` when it's given: the newest message the client holds. An edited message comes as history has it now, and a deleted one as its ID and channel. A change in a channel that's gone is left out, since the channel is missing from `ready` and the client drops it whole.
- New messages aren't changes: a client fetches them as history. Retention and deleted channels aren't in the log either: `expired` and the channel list say what they took.
- A page holds at most 1,000 changes, within about 900 KiB. `more` says the log goes on past its `version`, and the client asks again from there; the last page's `version` is the client's from then on.
- `since` older than `floor`, or newer than the den's version, gets `410 changes_gone`, and the client drops what it holds for the den.
- Responses are gzip-compressed when the client sends `Accept-Encoding: gzip`.
- A client connected throughout asks once a day, which keeps its version above the floor. Changes it already applied as events apply again harmlessly.

### Client frames

The client sends the same array shape: `auth.renew`, `focus` and `typing` (see [Presence and typing](#presence-and-typing)), and from M2 the `voice.*` frames (see [Voice](#voice-m2)).

```json
[{"t": "auth.renew", "d": {"nonce": "…", "proof": "…"}}]
```

To renew, the client gets a nonce from `/api/auth/challenge` and sends `auth.renew` within the token's last 10 minutes. The den extends the same token by one hour and answers with an ephemeral `auth.renewed {expires_at}`. If a token expires anyway, the den closes the socket with 4001.

### Liveness

Each side checks the other with WebSocket pings, since a dead connection can look open for a long time:

- The den pings every 30 seconds, and drops a connection that doesn't answer within 15.
- The client pings every 20 seconds, and reconnects if the den doesn't answer within 10.
- Each connection has a bounded send queue. A client that falls behind is closed with 4008 and resumes on reconnect.
- Events queued for a connection go out together, up to 256 per frame, so a busy den sends fewer, larger frames.

### Close codes

| Code | Meaning | Client does |
| --- | --- | --- |
| 1000 | Normal | Nothing |
| 1001 | Going away (a proxy reload or restart) | Reconnect with backoff and resume |
| 1012 | Den restarting | Reconnect after a random 0.5 to 5 seconds, and resume (it will get `ready`) |
| 4001 | Session expired | Log in again, then reconnect and resume |
| 4003 | Key revoked, or the member left, was removed or was banned; the reason says which: `key revoked`, `password changed` (a new password signed the device out), `started over` (a new DM seal did), `left`, `removed` or `banned` | Stop. Tell the member, and don't reconnect. After `key revoked`, `password changed` or `started over`, offer to sign in again |
| 4008 | Too slow | Reconnect and resume |
| 4029 | Rate limited | Reconnect after the `Retry-After` the close reason carries |

Any other failure reconnects with exponential backoff and full jitter, from 0.5 seconds up to 30, resetting after a minute connected. A stopping den closes its listener before sending 1012, so no client reconnects to the old process.

## Channels and groups (M1.2)

```
channel = {"id", "group_id", "name", "description"?, "kind": "text" | "voice", "position", "staff_only", "bitrate"? (M4.2)}
group   = {"id", "name", "position"}
```

- `group_id` is null for a channel outside any group.
- Names are 1 to 32 characters, with the same stripping as display names. A text channel's `description` is the markdown subset, up to 4,000 characters. Voice channels have none.
- A voice channel's `bitrate` is what its call's Opus may send, in bits a second (M4.2): 16000 to 128000, in steps of 8000, and 96000 unless set when it's created or changed. Text channels have none. A change reaches the call in progress with each member's next offer (see [Offers and answers](#offers-and-answers)).
- A den has at most 500 channels and 100 groups.
- `ready` carries the channels and groups the member can see. Changes arrive as `channel.created {channel}`, `channel.updated {channel}` and `channel.deleted {id}`, and the same for `group.*`.
- Management is `POST /api/channels`, `PATCH /api/channels/{id}` and `DELETE /api/channels/{id}`, and the same for `/api/groups`. Requests carry the object's fields, and a `PATCH` changes only the ones it names. `"group_id": ""` moves a channel out of its group. Moderators and the owner manage them.
- Deleting a channel deletes its messages. Deleting a group keeps its channels, which move to the end of the ungrouped list.

**Staff-only channels.** Members who can't see a channel get none of its events, and reorder lists leave it out, so the positions they see can have gaps. When a channel becomes staff-only or stops being so, those members get a fresh `ready`.

**Ordering.** Positions are dense (0, 1, 2, …) among the channels of one group, among ungrouped channels, and among groups.

- A create or edit that gives `position` inserts there, shifting the siblings at and after it down by one. Leaving it out appends to the end. Moving a channel to another group (`group_id` with or without `position`) closes the gap it leaves.
- The den renumbers in the same transaction, and sends the result as one event per affected list: `channels.reordered {group_id, channel_ids}` (`group_id` is null for ungrouped channels), or `groups.reordered {group_ids}`, in the same frame as the change itself.
- A reorder list also says which group each listed channel is in. Deleting a group sends `group.deleted`, then `channels.reordered` for the ungrouped list with the group's channels at its end, then `groups.reordered`.

## Messages (M1.2)

```
message = {"id", "channel_id", "author_id", "created_at", "revision", "edited_at"?, "edited_by"?,
           "text", "reply_to"?, "reply"?, "nonce"?, "attachments"? (M1.4), "editors"? (M1.6),
           "sealed"?, "key_id"? (M1.7), "reactions"?, "dm_reactions"? (M15.3), "poll"?, "everyone"? (M15.4), "expires_at"? (M16.4)}
```

A DM's message carries its text and files `sealed` instead, with the DM key `key_id` names; its `text` is empty, and it lists no `attachments` (see [Private DMs](#private-dms-m17)). A channel's message carries its `reactions`, and a DM's its members' sets of them sealed, as `dm_reactions` (see [Reactions](#reactions-m153)). A channel's message may carry a `poll`, as the member it's sent to may see it, and `everyone: true` where its @everyone is staff's (see [Polls](#polls-m154)). A DM's or a group's message sent under a time carries when it goes, `expires_at` (see [Disappearing messages](#disappearing-messages-m164)).

`text` is at most 4,000 characters and 16 KiB, and not only spaces, unless the message has files (M1.4), when it may be empty. It is the markdown subset from the design doc, which the client parses and never treats as HTML. The den stores it as sent, except for its links, which it rewrites before checking the limits (see [Links](#links-m3)).

`revision` starts at 1 and increases with every edit. `edited_by` is the member who made the latest edit, which differs from the author on shared messages.

`reply_to` names the message this one replies to, which is in the same channel. `reply` previews it as `{"author_id", "text"}`: its author and the start of its text, from the first character that isn't a space, at most 100 characters. The text is empty when the original has only files. A client can show the quote without loading a message that may be thousands back. The den builds the preview from the original as it is whenever it sends the reply, and leaves it out once the original is deleted; clients update the previews they hold from the original's `message.updated` and `message.deleted`. In a DM, the den can't read the original, so `reply` carries it whole, for the client to open and quote: `{"author_id", "text": "", "sealed", "key_id", "nonce", "revision"}`.

**Mentions.** `@` then 2 to 32 letters, digits or `_`, matching a username without regard to case. It counts at the start of a line, or after a character that is neither one of those nor `@`, when none of them follows. It doesn't count inside code (a ``` block, or a span from one backtick to the next with something between) or inside a link (`http://` or `https://` at the start or after a character that can't be part of a name, running up to a space or one of `<`, `>`, `"`, `'` and the backtick). The den counts by this rule (`denproto.Mentions`), and the page highlights by the same one; both are tested against the cases in `internal/denproto/testdata/mentions.json`. A mention adds to the unread mention count of the member it names, if they can see the channel and aren't the author. An edit recounts. Staff's `@everyone`, the moderators' and the owner's, mentions every member who can see the channel, and marks the message `everyone`; anyone else's mentions a member named `everyone`, whom a den may have from before (M15.4; see [Polls](#polls-m154)).

### Links (M3)

The den rewrites the links in a message's text when it's sent or edited, and in a channel's description or a member's bio when it's set, then checks the text's limits. A DM's text is rewritten the same way by its sender's client, before it's sealed. Both follow one rule (`denproto.CleanLinks`), tested against the cases in `internal/denproto/testdata/links.json`. The page's renderer is tested against the same cases, to check that it ends each link where the rule does.

- A link is one the page renders as a link: `http://` or `https://`, at the start or after a character that can't be part of a name, running up to a space or one of `<`, `>`, `"`, `'` and the backtick, less any `.`, `,`, `;`, `:`, `!`, `?` and `]` at its end, and any `)` there that closes no `(` in the link. It isn't in code or part of a mention, and inside bold, italics, strikethrough or a spoiler it ends where they do. The page's renderer (`markdown.jsx`) decides each of these, and `denproto` follows it step by step.
- Hosts match exactly and in lowercase, as a URL parser reads them, so a port or a trailing dot makes another host. A link with a user name or password before its host stays as it is, and so does one that doesn't parse.
- Links to these sites, in these forms, are written in one form, which keeps only what identifies the post or video:

| Site | Forms | Kept | Written as |
| --- | --- | --- | --- |
| YouTube | `/watch?v=ID`, `/shorts/ID`, `/live/ID` and `/embed/ID` on `youtube.com`, `www.youtube.com` and `m.youtube.com`; `youtu.be/ID`; `/embed/ID` on `youtube-nocookie.com` and `www.youtube-nocookie.com` | The video ID, 11 of `A-Z`, `a-z`, `0-9`, `-` and `_`. The start time `t`, or `start` on an embed: digits, optionally followed by `s`, or a time like `1h2m3s` | `https://www.youtube.com/watch?v=ID`, then `&t=` and the start time if there is one; a Short, from `/shorts/ID`, as `https://www.youtube.com/shorts/ID`, with no start time (M19.2) |
| Reddit | `/comments/POST` on `reddit.com` and its `www.`, `old.`, `new.`, `np.` and `m.` hosts, also after `/r/NAME`, `/u/NAME` or `/user/NAME`, optionally followed by a title, which may be empty, and then a comment ID; `/gallery/POST` on the same hosts; `redd.it/POST`. Each may end in a `/` | The post ID and the comment ID, each 1 to 16 of `a-z` and `0-9` | `https://www.reddit.com/comments/POST`, or `https://www.reddit.com/comments/POST/comment/COMMENT` |
| X | `/NAME/status/ID`, `/i/status/ID` and `/i/web/status/ID`, optionally followed by more of a path, on `x.com`, `twitter.com` and their `www.` and `mobile.` hosts | The status ID, 1 to 20 digits | `https://x.com/i/status/ID` |
| Amazon | `/dp/ID`, also after a product name (`/NAME/dp/ID`), `/gp/product/ID` and `/gp/aw/d/ID`, optionally followed by more of a path, on Amazon's stores and their `www.` and `smile.` hosts: `amazon.` followed by `com`, `ca`, `com.mx`, `com.br`, `co.uk`, `ie`, `de`, `fr`, `it`, `es`, `nl`, `se`, `pl`, `com.be`, `com.tr`, `ae`, `sa`, `eg`, `in`, `co.jp`, `sg`, `com.au`, `co.za` or `cn` | The product ID (ASIN), 10 of `A-Z` and `0-9` | `https://www.STORE/dp/ID`, where `STORE` is the store the link was on |

- Every other link keeps its form, and loses its tracking parameters:
  - On any site: `fbclid`, `gclid`, `dclid`, `gbraid`, `wbraid`, `msclkid`, `twclid`, `ttclid`, `yclid`, `igshid`, `igsh`, `mc_cid`, `mc_eid`, `_hsenc`, `_hsmi`, `mkt_tok`, and any whose name starts with `utm_`.
  - On `youtube.com`, `youtu.be`, `youtube-nocookie.com` and their subdomains, such as `music.youtube.com`: `si`, `pp` and `feature`.
  - On `x.com`, `twitter.com` and their subdomains: `s`, `t`, `ref_src` and `ref_url`.
  - On `reddit.com`, its subdomains and `redd.it`: `share_id`.
  - On Amazon's stores and their `www.` and `smile.` hosts: `ref`, `ref_`, `qid`, `sr`, `crid`, `sprefix`, `keywords`, `content-id`, `_encoding`, `tag`, `linkCode`, `linkId`, `ascsubtag`, `creativeASIN`, `creative`, `camp`, and any whose name starts with `pd_rd_` or `pf_rd_`. Path segments starting with `ref=` come out of the link too.

  Everything else in the link stays as it was, the remaining parameters' order and spelling included. A link left with no parameters loses its `?` too, and empty ones between `&`s go.
- A link stays as it is if the page would find another link once it's rewritten, as when what's left ends in punctuation the page leaves out of a link: `https://example.com/a.?utm_source=x` keeps its tracking rather than become a link to `https://example.com/a`.
- A Reddit share link (`/r/NAME/s/CODE`) loses only its tracking parameters. Its code is Reddit's own record of who shared the link, and only Reddit can turn it into a post. Amazon's short links, on `amzn.to` and `a.co`, are left the same way.
- Rewriting a text a second time changes nothing.
- The page shows a link written as `https://www.reddit.com/comments/…` on `old.reddit.com` in a browser that prefers it.

### Links to messages (M11)

```
https://den.example.com/#m/<den ID>/41/90210    the den's address, then #m/<den ID>/<channel>/<message>
GET /                                           200 text/plain: a line saying the address is a den's
```

- A link to a message is the den's address, as its invites and challenge sign it, followed by `/#m/`, `den_id` in base64url, `/`, the channel's ID, `/` and the message's ID. A client reads only that form: nothing between the address and the fragment but the `/`, which it may leave out, and IDs as the den writes them. It opens a link in the den it joined with that ID, wherever that den is now, so a link outlives a move, and never opens in another den that takes the address; the address names a den the client hasn't joined, and is what a browser opens.
- A browser never sends a fragment, so a link opened outside a client tells the den nothing of which message. The den answers `GET /` with a line of plain text saying it's a den on Dens, whose links open in Dens.
- Rewriting links leaves the fragment as it is (see [Links](#links-m3)), so a link to a message reads the same in a channel and in a DM.
- Before opening one, the client checks that the channel is a text channel or DM the member can see, and that the message is there: `around` it with a `limit` of 1 answers it, or the message after it once it's gone (see [History](#history)).
- A link to an event (M17.5) is the same with `/#e/`, `den_id`, `/` and the event's ID: `https://den.example.com/#e/<den ID>/12`. A client reads it as it reads a link to a message, and shows the event's card (see [Cards and badges](#cards-and-badges-m175)).

### History

```
GET /api/channels/{id}/messages?limit=50                 the newest page
GET /api/channels/{id}/messages?before=<id>&limit=50     older than id
GET /api/channels/{id}/messages?after=<id>&limit=50      newer than id
GET /api/channels/{id}/messages?around=<id>&limit=50     id and about half each side

200 {"messages": [… ascending by id …], "has_older": true, "has_newer": false}
```

- `limit` is 1 to 100. A page stops short of it when its messages would pass what a response holds, about 900 KiB of them, keeping those nearest the anchor; `has_older` and `has_newer` say more are there.
- The anchor `id` doesn't have to exist: `around` a deleted message returns its neighbours.
- `has_newer: false` means the page reaches the newest message, and the view can attach to the live tail.
- Responses are gzip-compressed when the client sends `Accept-Encoding: gzip`.

### Sending, editing, deleting

```
POST   /api/channels/{id}/messages   {"nonce": "<16 bytes>", "text", "reply_to"?}   201 message
PATCH  /api/messages/{id}            {"revision", "text", "attachments"? (M5)}       200 message
DELETE /api/messages/{id}                                                            204
```

- `nonce` makes a send idempotent: for 10 minutes, a retry with the same member and nonce returns the original message instead of posting again. The resulting `message.created` event carries the nonce, so the sender's other devices can match their pending copy. The nonce appears only there and in the send response, never in history, except on a DM's messages, whose sealed text is bound to it.
- Sending moves the author's read position to their new message.
- An edit names the `revision` it was made against. If the message has changed since, the den refuses it with `409 edit_conflict` and returns the current message beside the error, as `{"error": {…}, "message": message}`. The client shows it with the member's draft kept to reapply. Two devices, or two co-editors, never silently overwrite each other.
- The author edits a message, and so do the members they named as its editors (see [Shared messages](#shared-messages-m16)). The author deletes it, and so does staff of a higher rank than the author: moderators delete members' messages, and the owner anyone's. In a DM only the author deletes a message (M8).
- Events: `message.created {message}`, `message.updated {message}`, `message.deleted {id, channel_id, files?}`. A deleted message is gone for good, and so are its files, which `files` lists: clients drop them from memory, and later from their cache. An edit that takes files off a message lists them the same way, in `files` beside the message's own fields (M5; see [Attachments](#attachments)).
- Each member a deleted message counted for, in a DM or a group each other member and in a channel each it mentioned, gets `read_state.updated` after its `message.deleted`: their `mention_count` without it, and where it was all that lay past their read position, the position moved past it (M16.4). A client learns a channel's newest message only as messages come, so a DM whose last message went unread would otherwise stay unread until the next `ready`.

### Retention (M5)

```
GET /api/den/retention?days=N   200 {"messages", "bytes"}   (owner only)
messages.expired {"through": "<message id>"}
```

- With a retention period, the den deletes the messages sent more than that many days ago, in channels and DMs, with their files, as it deletes any message, within the hour after they pass it. Edits don't change a message's age, and neither do replies to it. A message that disappears (M16.4) goes at its own time, or with retention's pass where that comes first.
- It deletes the oldest first, in the order they were sent, so after a pass every message up to the last one it deleted is gone, in every channel. It sends `messages.expired` naming that message, and no `message.deleted` for each. Clients drop every message with an ID up to `through`, the quotes of them in replies, and the files on them; a channel whose last message is among them holds none, so nothing in it is unread. `ready` names the last message retention deleted as `sync.expired` (M6), for a client that was away.
- The owner sets the period with `PATCH /api/den`: 1 to 3,650 days, or 0 to turn it off. A change starts a pass at once. `GET /api/den/retention` says what a period of `days` would delete now, for the settings to show before a change: how many messages, and the bytes their files take.

### Disappearing messages (M16.4)

```
channel = {…, "expires_after"?: <seconds>, "expires_set"?: {"by", "at"}}   a DM's or a group DM's
message = {…, "expires_at"?}

PUT /api/dms/{id}/expiry   {"expires_after"}   200 channel
```

- Any member of a DM or a group DM sets how long its messages stay after they're sent: `expires_after` is 3600, 86400, 604800 or 2419200 seconds, an hour, a day, a week or four weeks, or 0, Off, which a new one starts at and its channel then leaves out. A development instance's den also takes any whole number of seconds up to 2419200, for the browser checks, and a client takes any such time from a den. `expires_set` is who last set it and when, once anyone has, Off included, for the line that says so.
- Another time gets `400 invalid_field`, as does a channel that isn't a DM or a group DM; a DM whose other member left gets `403 forbidden`, and a group at version 1 `404 not_found`. The same time again changes nothing and tells nobody. A change reaches the DM's two members, or the group's, as `channel.updated {channel}`, and `ready` carries it with the channel. Setting a time counts toward the member's other writes.
- A message sent while its channel has a time carries `expires_at`: its `created_at` plus the time, by the den's clock. An edit leaves it. A client refuses a message whose `expires_at` isn't after its `created_at`, or that isn't a DM's or a group's.
- The den deletes each as `expires_at` comes, as its author's delete would: with its files and reactions, `message.deleted {id, channel_id, files?}` to whoever can see it, and the change log; and as it starts, what came due while it was stopped. Each member it counted for hears `read_state.updated`, as for any delete (see [Sending, editing, deleting](#sending-editing-deleting)).
- A client drops what it holds past `expires_at`, by its own clock, whether it can reach the den or not, and leaves it out of history. An older client, which knows no `expires_at`, drops it as the den deletes it. A client offers the setting only on a den whose info says `disappearing`.

### Read state

```
PUT /api/channels/{id}/read   {"message_id"}   204
```

- The position only moves forward: a lower `message_id` is ignored.
- Clients send it once a message has been on screen, at most once every few seconds per channel.
- The den tells all the member's sessions with `read_state.updated {channel_id, message_id, mention_count}`.
- `ready` carries `read_states`: each channel's last message ID, the member's read position and their unread mention count, and for a DM, whether they closed it (M1.3).

## Members and roles (M1.3)

Moderators and the owner are **staff**. Staff manage channels, groups and invites, see staff-only channels, and delete the messages of, remove and ban members of a lower rank: moderators act on members, and the owner on anyone else. The owner alone changes the den and roles.

```
GET   /api/members/{id}                                     200 member, with "bio" and "badges" (M17.5)
PATCH /api/me              {"display_name"?, "bio"?}        200 member, with "bio"
PATCH /api/members/{id}    {"role": "member" | "moderator"} 200 member          (owner only)
PUT   /api/me/badges/{id}  {"hidden": bool}                 204                 (M17.6)
```

- Any member may fetch anyone who has been in the den. `bio` is the markdown subset, up to 300 characters, and an empty one clears it. It comes only in these two responses, never in `ready` or events, which keeps both small, as do the badges the member earned, in the first alone (see [Cards and badges](#cards-and-badges-m175)).
- A profile or role change reaches everyone as `member.updated {member}`; a client showing a bio fetches it again.
- A role change makes the member's sockets start over with a fresh `ready`, since what they can see changed, and their older resume points resync.
- `PUT /api/me/badges/{id}` hides a badge the member earned from their profile card, named by its event's ID, or with `"hidden": false` shows it again (M17.6). It answers 204 whether or not that changed anything, and tells nobody. A badge the member didn't earn, or whose event they don't see, as a staff-only event's once they aren't staff, is `404 not_found` (see [Cards and badges](#cards-and-badges-m175)).

### Leaving, removal and bans

```
POST   /api/me/leave                                                                204
POST   /api/members/{id}/remove   {"ban"?, "delete_messages"?, "revoke_invite"?}    204
GET    /api/bans                                                                     200 {"bans": [{"member", "banned_at", "banned_by"?}]}
DELETE /api/bans/{id}                                                                204
```

- A member who leaves or is removed keeps their record, with `left_at` set, so their messages keep a name. They lose their role, devices and sessions, recovery codes, read state, the invites they made, the pictures on their profile and the uploads they hadn't used. Their sockets close with 4003, and everyone gets `member.left {id, left_at}`.
- The owner can't leave or be removed.
- `ban` keeps the username out: joining with it is refused with `403 banned`. `delete_messages` also deletes the member's messages from that many seconds back, one of 0, 3600, 86400 or 604800, in DMs too from M8, with a `message.deleted` for each to whoever can see it. `revoke_invite` revokes the invite they joined with, if it can still be used. Someone who already left can still be banned.
- Staff list and lift bans. Lifting one takes staff of the rank of whoever placed it, or higher, so only the owner lifts the owner's (M8). Lifting one doesn't bring the member back.
- **Coming back.** A former member who isn't banned can join again with a new invite, as themselves: their username with the right password (the verifier) reclaims their record, history and first join time, with a new device and recovery codes. A wrong password gets `username_taken`, the same as a name in use, so an invite can't reveal who used to be here.
- The den stores no IP addresses, and a client makes a new key for each join, so a ban can't stop someone who joins under a new name with a valid invite. Invites are the real door.

### DMs

```
POST /api/dms              {"member_id"}   200 channel
POST /api/dms/{id}/close                   204
```

- A DM is a channel of kind `dm` between two members: `{"id", "kind": "dm", "members": [lower ID, higher ID], "group_id": null, "name": "", "position": 0, "staff_only": false}`. Each pair has one; `POST /api/dms` returns it, creating it on first use and sending `channel.created` to both.
- Only its two members see it, in `ready`, events, history and sends. Staff don't see or moderate DMs, and from M1.7 the den's owner can't read them either: their messages and files are end-to-end encrypted (see [Private DMs](#private-dms-m17)).
- Messages, edits, deletes, replies and read state work as in channels, their text sealed. Every message counts toward the other member's unread `mention_count`.
- DMs aren't managed through `/api/channels`, and don't count toward the 500-channel limit. Nor are group DMs, which `POST /api/dms/{id}/close` closes as it does a DM (see [Group DMs](#group-dms-m163)).
- A DM whose other member left keeps its history, but nothing more can be sent (`403 forbidden`), and no new DM starts with a former member.
- Closing a DM takes it out of the member's list on all their devices: their read state for it gets `"closed": true`, sent as `read_state.updated`. A new message in it, from either member, or opening it again with `POST /api/dms`, reopens it the same way.

### Presence and typing

A member is online while they have an open socket. `ready` carries `online`, a list of member IDs. Changes go out as ephemeral `presence {online?, offline?}` events, gathered into batches about every 1.5 seconds. Presence isn't replayed, so after `resumed` the den sends `presence {online, full: true}`, which replaces the client's set.

```json
[{"t": "focus", "d": {"channels": ["12", "40"]}}]
[{"t": "typing", "d": {"channel_id": "12"}}]
```

- `focus` names the channels the client is showing, at most 10; the den keeps those the member can see. It forgets them with each connection and each `ready`, so the client sends `focus` again after either. Focus counts toward the member's limit for other writes, and the den drops one past it.
- `typing` counts only in a channel the connection focuses. The den passes it on as an ephemeral `typing {channel_id, member_id}` to the other connections focused there, at most once every 2 seconds per member and channel, dropping the rest.
- Clients show a typing notice for about 6 seconds, and clear it when that member's message arrives.

## Files (M1.4, M1.8)

```
file  = {"id", "name", "type", "size", "width"?, "height"?, "animated"?, "duration_ms"?, "thumb"?: {"width", "height"}, "spoiler"? (M17.2)}
image = {"id", "width", "height"}
```

A file is an upload: an attachment on a message, or a picture on a profile. `type` is what the den found the file to be from its bytes, never what its name says. `width` and `height` are set for images and videos, as they display, so a client lays out the space one takes before it loads. `duration_ms` is a video's or audio's length when it's known, at most a week. `thumb` is there when the file has a preview.

### Uploading

```
POST /api/uploads      (body: the file's bytes)      201 file
Content-Type: application/octet-stream
Dens-Filename: <the file's name, UTF-8, percent-encoded>
```

- `Content-Length`, when given, lets the den refuse a file over the size limit before reading it. Without it the den reads up to the limit and refuses the file once it passes it.
- The den reads the file's first bytes to decide what it is:
  - JPEG, PNG, GIF and WebP are images. They get their size as they display (after a JPEG's orientation), `animated` when they have more than one frame, and a preview: a JPEG, or a PNG when it has transparency, fitting a 640 × 640 square, never enlarged, upright. An image too large to decode within the den's memory budget, 256 MiB, goes without one and downloads like any other file.
  - Video and audio in MP4 and QuickTime, Matroska and WebM, MP3, AAC, FLAC, Ogg and WAV go through the den's media module (see [Video and audio](#video-and-audio)).
  - HEIF (not AVIF), TIFF (not camera raw), JPEG 2000 and Photoshop photos are refused with `415 unsupported_type`: clients send them as the JPEG or PNG they turn them into.
  - AVIF, JPEG XL, camera raw, and video and audio in other containers (AVI, ASF, FLV, MPEG program and transport streams, AIFF, AMR, MIDI, RealMedia) are refused with `415 unsupported_type`, and a message naming the kind, rather than stored with metadata nobody took out. So are video and audio at a den without the media module.
  - Anything else is a file to download, with the media type the den sniffed, for clients to pick an icon.
- The name is cleaned: control, bidi and zero-width characters are dropped, `/` and `\` become `_`, spaces and dots are trimmed from both ends, and it is cut to 255 bytes without splitting a character. An empty name becomes `file`.
- An upload waits an hour for a message or a profile to use it, and is then deleted. Until it's used, only the member who uploaded it can fetch it.
- Uploads are limited per member (see [Rate limits](#rate-limits)).

### Metadata

Clients take metadata out of images before uploading, so a den never receives it, and the den checks: an image that still has any is refused with `400 invalid_field`. The den re-encodes nothing; a client's smaller copy of a photo (M5) is a new image, checked like any other. What an image keeps:

| Kind | Kept | Everything else goes, including |
| --- | --- | --- |
| JPEG | Frame headers, tables, scans, the JFIF header with no thumbnail, the ICC profile, Adobe's color transform (12 bytes), and the orientation | EXIF and its GPS, XMP, IPTC, comments, other application segments, and everything after the image's end, where the Multi-Picture Format stores extra images such as gain maps |
| PNG | `IHDR`, `PLTE`, `IDAT`, `IEND`, `tRNS`, `cHRM`, `gAMA`, `iCCP`, `sBIT`, `sRGB`, `cICP`, `mDCV`, `cLLI`, `bKGD`, `hIST`, `pHYs`, and the animation chunks `acTL`, `fcTL` and `fdAT` | `eXIf` (with its orientation, which browsers don't agree on), text chunks, time stamps, private chunks, anything after `IEND`. An unknown critical chunk refuses the file. |
| GIF | Images, their graphic control extensions, and the `NETSCAPE2.0` or `ANIMEXTS1.0` loop extension | Comments, plain text blocks, other application extensions (XMP among them), anything after the trailer |
| WebP | `VP8X` with its EXIF and XMP flags cleared, `VP8 `, `VP8L`, `ALPH`, `ANIM`, `ANMF` (keeping only `ALPH`, `VP8 ` and `VP8L` inside) and `ICCP` | `EXIF`, `XMP `, unknown chunks, anything past the RIFF size |

A JPEG that isn't upright keeps its orientation as the smallest EXIF block that holds it, and the den accepts it only in exactly this form: `Exif\0\0`, then a big-endian TIFF header (`MM`, 42, the directory at 8), one directory with one entry, orientation (`0x0112`, type SHORT, count 1, the value 2 to 8), and no next directory, 32 bytes in all. An upright JPEG keeps no EXIF.

### Video and audio

Clients strip video and audio before uploading, with the same media module dens run: it copies the file's video and audio streams into a new container and nothing else (see the design doc's ffmpeg section). A den can't check a container the way it checks an image, so it strips the file again and stores its own copy:

- `type` is what the copy plays as, which its container decides:
  - MP4, QuickTime and AAC become MP4: `video/mp4`, or `audio/mp4` without video.
  - Matroska and WebM become WebM when WebM takes their codecs (`video/webm`, `audio/webm`), else MP4 when it does, else stay Matroska (`video/x-matroska`, `audio/x-matroska`).
  - MP3, FLAC, Ogg and WAV stay as they are: `audio/mpeg`, `audio/flac`, `audio/ogg` and `audio/wav`.
- `size` is the copy's, which must fit the file size limit too.
- `width` and `height` are a video's as it displays: cropped as its container asks, stretched by its pixels' aspect ratio, and turned.
- A video's preview is its first frame, made like an image's. The module decodes H.264 and HEVC, but not VP8, VP9 or AV1, so such a video arrives without `thumb`, and its uploader's client may send one (below).
- A file the module can't read gets `400 invalid_field`, as a damaged image does.

```
POST /api/uploads/{id}/thumb   (body: a JPEG or PNG)   200 file
```

- The image is the video's first frame as the uploader's browser plays it, drawn at the video's size or smaller, at most 16 MiB, and stripped like any image. It must have the video's shape, give or take a pixel of rounding on each side.
- Only the uploader may send one, while the upload waits to be used, for a video without a preview. The den makes its own preview from the image and keeps nothing else of it; the preview counts against the member's space.

### Attachments

```
POST   /api/channels/{id}/messages   {"nonce", "text", "reply_to"?, "attachments"?: ["<upload id>", …], "spoilers"?: ["<upload id>", …] (M17.2)}
PATCH  /api/messages/{id}            {"revision", "text", "attachments"?: ["<file or upload id>", …]}   (M5)
DELETE /api/uploads/{id}             204                                                         (M5)
GET    /api/files/{id}               200 the file's bytes
GET    /api/files/{id}/thumb         200 the preview's bytes
GET    /api/me/storage               200 {"used", "den_used"}
GET    /api/me/files?after=<cursor>  200 {"files": [file, …], "messages"?: [message, …], "next"?}  (M5)
```

- A message holds at most 10 files: the author's own uploads, not yet used. `message.attachments` lists them in the order sent.
- `spoilers` (M17.2) names which of them go as spoilers, each one of the message's uploads, once; anything else gets `400 invalid_field`. The den keeps it on the file, which says `"spoiler": true` to everyone who sees it, and clients show it covered until it's clicked. Once sent it stays: an edit keeps it, and an upload that takes a file's place (`Dens-Replaces`) is a spoiler if the file was. A DM's spoilers go inside its sealed text, never to the den: a DM's send naming any gets `400 invalid_field`. A den older than them, which doesn't say `spoilers` in its info, ignores the field, so a client offers spoilers in a channel only where the den says it keeps them; an older client shows a spoiler uncovered.
- An edit changes the text. From M5, the author's edit can change the files too: `attachments` lists them afterward, in order, as files the message already has and the author's waiting uploads. Files it leaves out are deleted, and `message.updated` lists them in `files`. A change of files marks the message edited. An edit can't leave a message with neither text nor files; a client deletes the message instead.
- An upload made to take a file's place names it, as `Dens-Replaces: <file id>` (M5): one of the member's files, on a message or their profile. The den counts the upload against the member's space less that file's, and it can only take that file's place, in an edit that drops the file or as a picture replacing it. A file has one replacement waiting at most; a newer one drops the older.
- `DELETE /api/uploads/{id}` drops one of the member's uploads waiting to be used, and frees its space at once (M5).
- `GET /api/me/files` lists the member's files on the den, largest first by the space each takes with its preview, at most 50 at a time, `next` continuing the list (M5). Each is a `file` with `created_at`, and `message_id` and `channel_id`, or `"profile": "avatar"` or `"banner"`, or neither while it waits to be used. A channel's file has `excerpt`, the start of its message's text, as a reply's preview has. A DM's blobs come as sealed files, previews among them, and the page's `messages` carry the DM messages they're on, sealed, for the client to open and name them; a page ends early rather than carry more than 600 KiB of them.
- A file is served as `application/octet-stream` with `Content-Disposition: attachment`. The den never states a type a browser would act on; clients check the bytes before showing anything as an image, video or audio.
- A file answers byte ranges (`Range: bytes=N-`, `206 Partial Content`), and the den decrypts only the chunks a range covers, so a player seeks without fetching the whole file. Its `ETag` is its ID, quoted, since its bytes never change.
- A DM's files are sealed uploads, which its message lists only inside its sealed text (see [DM files](#dm-files)).
- A member may fetch a file when they can see the message it's on, any member may fetch a picture on a profile, and only its uploader may fetch an upload waiting to be used. Anyone else gets `404 not_found`.
- A file's bytes never change under its ID, so clients may keep a copy until an event says it's gone: `message.deleted` lists a message's files, a profile's replaced picture drops out of `member.updated`, `member.left` takes a member's pictures with it, `channel.deleted` takes the channel's, and `messages.expired` those of the messages it ends. After a `ready`, the changes since the client's version say which messages lost files or went (M6; see [What changed while away](#what-changed-while-away-m6)).
- `GET /api/me/storage` says how much space the member's files take and how much everyone's do, in bytes, previews included.

### Pictures on a profile

```
PATCH /api/me   {"avatar"?: "<upload id>" | "", "banner"?: "<upload id>" | ""}   200 member, with "bio"
```

- An avatar is a square still image, 64 to 1,024 pixels a side. A banner is a still image 300 to 3,000 pixels wide and three times as wide as it is high, give or take 3%. Clients crop pictures to these shapes before uploading them.
- Either may be an animated GIF instead (M15.1): an avatar then at most 256 pixels a side and 2 MiB, and a banner at most 600 pixels wide and 4 MiB. An animated picture of any other kind, such as an animated WebP or PNG, is refused; a member's own service makes its GIF.
- `""` takes a picture off. The picture a change replaces is deleted.
- A member who leaves or is removed loses their pictures and the uploads they hadn't used.

### Limits

```
limits = {"file_size", "member_storage", "den_storage"}   (bytes)
```

- A new den allows files of 25 MiB, 2 GiB of files per member and 20 GiB for the whole den. The owner changes them with `PATCH /api/den`: a file limit of 1 MiB to 1 GiB, room for at least one file per member, and for at least one member in the den.
- A file over the size limit gets `413 too_large`. One that would take a member past their space gets `507 quota_exceeded`; past the den's space, or with less than 1 GiB left free on the den's disk, `507 den_full`. Space counts files and their previews.

## Shared messages (M1.6)

```
POST  /api/channels/{id}/messages    {…, "editors": ["<member id>", …]}   201 message
PATCH /api/messages/{id}             {"revision", "text", "editors"?}     200 message
POST  /api/messages/{id}/tasks/{n}   {"checked": true, "text": "milk"}    200 message
```

- `editors` names up to 20 other members who may edit the message and tick its tasks. Each must be in the den and able to see the channel: in a staff-only channel only staff, and in a DM only its other member. The den lists them in the order of their IDs.
- Only the author changes `editors`, with a `PATCH` that sets it; `[]` removes them all. A `PATCH` that changes only `editors` doesn't mark the message edited.
- An editor edits the text as the author does, against a revision, and becomes `edited_by`. Editors don't delete a message.

**Tasks.** A task is a line that starts with `[ ] ` or `[x] `, outside a ``` block; its text is the rest of the line. A message's tasks are numbered from 0 in order. The den finds them by this rule (`denproto.Tasks`), and the page renders them by the same one; both are tested against the cases in `internal/denproto/testdata/tasks.json`.

- A tick sets task `n` to `checked`, as a change of its own: it needs no `revision`, and bumps it, so ticks on different tasks at once never conflict.
- `text` is the task's text as the member saw it. If task `n` no longer has that text, as after an edit moved the lines, the den refuses the tick with `409 edit_conflict` and the current message, as for an edit, rather than tick another line. Ticking a task to the state it's in changes nothing.
- A tick doesn't mark the message edited, and leaves its mentions as they were.
- The den makes one change to a message at a time, and sends each `message.updated` in the order of its revisions. A tick's answer can still reach a client after a newer event, so clients keep the higher revision.

## Private DMs (M1.7)

A DM's messages and files are end-to-end encrypted: its members' clients seal them with the DM's key before they reach the den, and open them after. From M16.1 a DM's key is sealed to its members' identities, and signed by its maker's; the den keeps each member's identity, relays the keys and keeps each member's record, and never holds anything that opens them. The cryptography is in `internal/denproto/e2e.go` and `identity.go`, which both sides share.

In this section, `len(x)` is x's length as an 8-byte big-endian number, and IDs join as 8-byte big-endian numbers too.

### Identities (M16.1)

```
identity = {"kem": "<1,216 bytes>", "sig": "<32 bytes>"}

PUT /api/me/identity                    identity   200 member
GET /api/identities?members=<id>,<id>…             200 {"identities": [{"member_id", "kem", "sig"}, …]}
```

- A member's client publishes their identity (see [Identity and keys](#identity-and-keys)) as it signs in, where the member's record has no `identity` or another; a client without the seal publishes nothing. The den checks the keys are of their kinds. A change reaches everyone as `member.updated`, and a client that kept the list of members gets it with its next `ready` (M6.2).
- Any member fetches identities, up to 50 at once, as a client needs them; members without one are left out.
- Leaving the den takes the member's identity, and starting over does too (see [Starting over](#starting-over)).
- A client keeps the identity the den gives for each member the first time it needs it, and checks each it meets after against what it kept; trusting the den is the client's, as is noticing a change.

### DM keys (M16.1)

```
signed_key = {"id", "channel_id", "made_by", "made_at", "kid", "commit", "members": [{"member_id", "identity"}, …],
              "maker": {"kem_hash", "sig"}, "signature", "copy"?, "retired_at"?, "retired_by"?, "retired"?: "started_over" | "restarted"}

GET  /api/dms/{id}/signed-keys                                                            200 {"keys": [signed_key, …]}
POST /api/dms/{id}/signed-keys  {"kid", "commit", "members", "signature", "copies": [{"member_id", "copy"}, …]}  201 signed_key
```

A DM's key is 32 random bytes its maker's client makes as the DM first needs one, with a random 16-byte `kid`:

- `copy` is the key sealed to a member's identity: HPKE base mode with X-Wing, HKDF-SHA-256 and ChaCha20-Poly1305, `info = "dens-key-copy-v1" ‖ den_id ‖ channel ‖ kid ‖ member`, as the encapsulated key ‖ ciphertext, 1,168 bytes.
- `commit = SHA-256("dens-key-commit-v1" ‖ kid ‖ key)`. Anyone can seal a copy to an identity, so a client takes its copy only when it opens to what this commits to.
- `signature` signs `"dens-signed-key-v1" ‖ den_id ‖ channel ‖ kid ‖ commit ‖ made_by ‖ len(members) ‖ (member ‖ identity)…`, with `members` in the order of their IDs, by the maker's identity.
- `maker` is the maker's identity as the den held it when the key was made: `kem_hash`, SHA-256 of its X-Wing key, and `sig`, its Ed25519 key, which hash to the identity `members` names for the maker. A client checks the signature with it, and the identity it names against those it kept for the maker, its own being the member's: for sending, the one kept now; for reading, any it kept, so a key from before an identity changed still opens what it sealed.
- The den takes a key only when `members` are the DM's two, each with the identity they publish now, the signature checks against the maker's, and there's a copy for each. A DM has one live key: a key from before M16.1 both members checked stays, and so does a signed key naming the identities its members publish now, so a second gets `409 key_exists`, and its client takes the live one; a signed key that names an identity no longer published retires as `restarted`, by the maker of the key that replaced it.
- Each member sees their own copy alone: in `ready`'s `signed_keys`, and as `dm.signed_key {signed_key}` as a key is made or retired. A retired key takes no new messages, and stays so what it sealed still opens.
- Signed keys never come among `dm_keys`, `dm.key` or `GET /api/dms/{id}/keys`, whose copies a client from before M16.1 checks to be its own 72 bytes and whose malformed events close its connection.

### DM keys from before M16.1

```
dm_key = {"id", "channel_id", "started_by", "started_at", "stage": "offered" | "answered" | "revealed",
          "checks"?: [{"member_id", "at"}], "retired_at"?, "retired_by"?, "retired"?: "started_over" | "restarted",
          "sealed"?, "exchange"?: {"offer", "answer"?, "reveal"?, "state"?}}

GET  /api/dms/{id}/keys                                    200 {"keys": [dm_key, …]}
POST /api/dms/{id}/keys   {"offer", "state", "restart"?}   400 invalid_field
```

Before M16.1 a DM's key came from an exchange of one-time keys between its two members' clients, the one [Approving new devices](#approving-new-devices-m17) runs, with `ctx = "dens-exchange-dm-v1" ‖ den_id ‖ channel ‖ starter ‖ other`: the starter's offer, the other's answer and the starter's reveal, each side's `state` waiting on the den sealed with its member's seal. Its key, `HKDF-SHA-256(secret, salt = transcript, info = "dens-exchange-key-v1")`, opened the DM once each member typed the other's half of its check code, and stored their copy:

- `sealed` is the member's copy of the key: XChaCha20-Poly1305 under `HKDF-SHA-256(seal, salt = den_id, info = "dens-dm-seal-v1")`, bound to `"dens-dm-key-v1" ‖ den_id ‖ channel ‖ key_id ‖ member`, as nonce ‖ ciphertext, 72 bytes. `checks` records who stored theirs.
- A key both members checked stays live, both sending and reading under it, until one of them starts over.
- The den starts no exchange from M16.1 on: `POST /api/dms/{id}/keys` gets `400 invalid_field`. An exchange still running at the update, or a key one member alone checked, retired as M16.1's migration ran, as `restarted` by whoever started it.
- `ready` carries `dm_keys` without exchanges, and each change reaches the DM's two members as `dm.key {dm_key}`, each with their own copy.

### The member's record (M16.1)

```
GET /api/me/record                            200 {"revision", "sealed"?}
PUT /api/me/record  {"revision", "sealed"}    200 {"revision"}
```

- The record is what the member's clients kept of others' identities, and whom they verified, sealed by each client with their seal: XChaCha20-Poly1305 under `HKDF-SHA-256(seal, salt = den_id, info = "dens-dm-seal-v1")`, bound to `"dens-record-v1" ‖ den_id ‖ member ‖ revision`, over its length as 4 bytes ‖ its JSON ‖ zeros to 16, 64 or 256 KiB, as nonce ‖ ciphertext. What it holds is the clients' own.
- `revision` is 0, with nothing sealed, before the first write. A write names the revision it was merged from, and the den keeps it as the next; one written over another gets `409 edit_conflict`, and the client reads, merges and writes again. The den checks only the sealed size.
- The member's sessions hear of each revision as `record.updated {"revision"}`, so their other clients read it at once.
- Each connected client writes it every hour whether it changed or not, so the den learns nothing from when, or from its size.
- Leaving the den takes the record, and starting over does too, since the old seal sealed it.

### DM messages

```
POST  /api/channels/{id}/messages  {"nonce", "sealed", "key_id", "reply_to"?, "attachments"?, "editors"?}  201 message
PATCH /api/messages/{id}           {"revision", "sealed", "key_id", "editors"?, "unedited"?, "attachments"? (M5)}  200 message
```

- `sealed` is `{"text", "files"?}` as JSON, sealed with XChaCha20-Poly1305 under the DM key and bound to `"dens-dm-message-v1" ‖ den_id ‖ channel ‖ author ‖ len(nonce) ‖ nonce ‖ key_id ‖ revision`, as nonce ‖ ciphertext, at most 64 KiB. `text` stays empty. The sealed text follows a message's rules, and lists at most 10 files.
- `key_id` must name the DM's live key, and one its sender holds a copy of: a signed key's members do, and a key from before M16.1's once they checked it; otherwise `400 invalid_field`.
- A DM's reactions are each member's set, which their client seals as it seals a message (see [Reactions](#reactions-m153)).
- An edit seals the message again at the next revision, under the DM's live key, which may be newer than the one it was sent with. The den can't compare texts, so `"unedited": true` marks an edit that only ticks a task or changes the editors, and doesn't mark the message edited. A DM's tasks tick this way: `POST /api/messages/{id}/tasks/{n}` refuses a DM's message.
- The rest works as in channels: ordering, history, revisions and edit conflicts, deletes, read state, and unread counts, where every message counts for the other member.
- Binding the author, nonce, key and revision keeps the den from moving a sealed text to another message, or passing an old revision off as a new one. It can still hold a message back, or show one twice.

### DM files

```
POST /api/uploads/sealed   (body: the sealed file)   201 {"id", "name": "", "type": "application/octet-stream", "size", "sealed": true}
```

- A DM's file goes up sealed by the sender's client, which first takes an image's metadata out and makes its preview, as the den does for a channel's. Each file gets a random key of its own. The file and its preview are sealed with it as streams, in the format files at rest use, bound to `"dens-dm-file-v1" ‖ den_id ‖ channel ‖ "file"` or `"thumb"`, and go up as two blobs.
- The sender's client strips a DM's video and audio, and makes a video's preview, as a den does for a channel's. For a video the module can't decode, the sender's page draws the preview, and the client seals and uploads it as it does its own.
- The message's sealed text lists each file: `{"id", "key", "name", "type", "size", "width"?, "height"?, "animated"?, "duration_ms"?, "thumb"?: {"id", "width", "height"}, "spoiler"? (M17.2)}`. Its `attachments` name every blob, each file's and its preview's, at most 20. A file sent as a spoiler says so here alone, and a file swapped into its place is one too.
- From M5, a DM's edit can change its files as a channel's does: the sealed text lists the files afterward, and `attachments` names every blob, as a send's does. Blobs it leaves out are deleted.
- A sealed file answers byte ranges like any other, and a client opens the chunks a range covers with the file's key, so a DM's video seeks the same way.
- A DM's message takes only sealed uploads, and a channel's none. The den counts a sealed blob against the upload limits, and serves it like any file, with no preview of its own.

### Group DMs (M16.3)

```
channel = {"id", "kind": "group_dm", "members": [member id, …], "changes": n, "sealed_name"?: {"kid", "sealed"}, "group_id": null, "name": "", "position": 0, "staff_only": false}
change  = {"id", "made_at", "n", "prev", "kind": "made" | "added" | "removed" | "left" | "departed" | "named", "by"?, "subjects"?: [member id, …],
           "after": [member id, …], "name"?: {"kid", "sealed"}, "signer"?: {"kem_hash", "sig"}, "signature"?}

POST /api/group-dms                                                       201 {"id"}
GET  /api/group-dms/{id}/changes?after=<n>                                200 {"changes": [change, …], "more"}
POST /api/group-dms/{id}/changes  {"change", "key"?: signed key request}  200 change
```

A group DM is a DM of three to ten members, at version 2 of the protocol alone. Its channel lists its members as the den holds them, in the order they joined, the number of its latest change, and its name sealed; a client knows its members from its changes alone.

- **A change** names its number `n`, from 1, `prev`, the hash of the change before it (32 zero bytes for the first), its `kind`, its maker `by`, the members it took in or out `subjects`, and the group's members `after` it, in the order they joined. Its maker's identity signs `"dens-group-change-v1" ‖ den_id ‖ channel ‖ n ‖ prev ‖ kind ‖ by ‖ len(subjects) ‖ subject… ‖ len(after) ‖ member… ‖ name`, where `kind` is one byte, 1 to 6 in the order above, and `name` is 1 ‖ kid ‖ SHA-256(sealed), or 0 with none; `signer` is the maker's identity as a signed key's `maker` carries it, which hashes to their identity. A change's hash is `SHA-256("dens-group-change-hash-v1" ‖ what it signs)`.
- **The rules,** which the den checks, as every member's client does: `made` is the first change, its maker first in `after`, the others its `subjects`, three to ten in all; `added`, by a member, takes in `subjects`, none of them members, at the end of `after`, ten at most; `removed`, by the first of the members in the order they joined, takes out one other; `left` takes out its maker; `departed`, the den's, unsigned, takes out one who left the den; and `named`, by a member, carries the new name, sealed under the live key. Each follows the one before, by `n` and `prev`, and `after` is what it makes of the members before it. The den takes a change only from its maker, signed by the identity they publish now; one made against an earlier change gets `409 edit_conflict`, and the client makes it again on the group as it is.
- **Making one.** `POST /api/group-dms` reserves a group for the session's member, which nobody sees until its first change, `made`, makes it: every statement binds the den's ID for the group. A member who reserved one and didn't make it gets the same one again; one reserved an hour ago goes as the next is reserved, or as its maker leaves the den. A member is in at most 100 group DMs; making one, and each change, count toward the member's other writes.
- **Keys.** A change that makes a membership, `made`, `added` or `removed`, carries its key, as `POST /api/dms/{id}/signed-keys` makes one, for the members `after` it, each with the identity they publish now; `left` retires the live key, as `departed` does, and the first member's client to send makes the next, through `POST /api/dms/{id}/signed-keys`, which takes a group's key for its members as they are, retiring a live key that names the members or identities as they were. A group's key may carry the group's name sealed under it, `name`, its `kid` the key's: the den keeps it as the group's. A member gets a group's keys they hold a copy of, in `ready` and as `dm.signed_key`, while they're in it.
- **The name** is `{"len(name)" as 2 bytes ‖ name ‖ zeros to 160 bytes}` sealed with XChaCha20-Poly1305 under the group key whose `kid` it names, bound to `"dens-group-name-v1" ‖ den_id ‖ channel ‖ kid`, as nonce ‖ ciphertext, 200 bytes. An empty name is none. A client opens it with the key whose `kid` it names, and cleans it as a display name.
- **Members' changes.** `GET /api/group-dms/{id}/changes` returns a page of the group's changes after `after`, at most 200, from the change that took the member in, which names the group's members then. `group.change {"channel_id", "change"}` goes to the members after each change; a member taken in gets `channel.created`, one taken out `channel.deleted`, and the rest `channel.updated`. A member removed, gone or leaving loses the group, its history, keys and changes.
- **Messages** are sealed as a DM's, under the group's live key, with `"dens-group-message-v1"` in its binding's place, and the sealed text starts with its signer's ID as 8 bytes, the hash of their identity's X-Wing key, their Ed25519 key, and their signature over `"dens-group-signed-v1" ‖ binding ‖ signer ‖ SHA-256(text)`; the signer is the author at the first revision, and whoever edited or ticked it after. A member's set of reactions is sealed as a DM's, with `"dens-group-reactions-v1"` in its binding's place, its 1,024 bytes the same header, signed over `"dens-group-reacted-v1" ‖ binding ‖ member ‖ SHA-256(set)`, then the set padded to the rest; a message carries at most 20 members' sets. A client opens one only where its signature checks against the identity its key names for the signer.
- **The rest** works as in a DM: history from the member's joining on, sends, edits, deletes by their author alone, read state, closing, and unread counts, where every message counts for each other member. No polls, and no @everyone.

### Starting over

```
POST /api/me/seal   {"verifier", "seal_check"}   200 {"signed_out": 1}
```

- A member who has lost their seal and every device that held it, or wants to shut out a stolen device that holds it, starts over with a new seal. It takes the password, so a stolen session can't.
- The live keys of their DMs retire (`"retired": "started_over"`), and their copies of every DM key, and their exchange states, go: nothing opens them any more. What those keys sealed stays unreadable to them, while the other members keep their copies. Their identity and record go too, so others see their identity change as their client publishes the new seal's (M16.1); each DM gets a new key, sealed to the new identity, with its next message.
- Every other device of theirs is signed out with 4003 `started over`, since it holds the old seal, and their sign-ins waiting for approval are cancelled.
- A former member who joins again with another seal than the one they left with starts over the same way.

## Notes to self (M17.3)

```
channel = {"id", "kind": "notes", "members": [member id], "group_id": null, "name": "", "position": 0, "staff_only": false}

POST /api/notes   200 channel
```

A member's notes are a channel of their own in the den, which only they see, at version 3 of the protocol alone. Their clients seal its messages as a DM's, under a key the den never holds.

- `POST /api/notes` answers with the member's notes, making them the first time, which sends `channel.created` to the member; after that it answers with the same channel. A member has one in each den. It counts toward the member's other writes.
- **The key** is `HKDF-SHA-256(seal, salt = den_id, info = "dens-notes-v1")`, 32 bytes, which each client holding the member's seal makes as it needs it and never sends.
- **Messages** go as a DM's (see [DM messages](#dm-messages)), sealed under the notes' key, with `key_id` the notes' channel ID, which the den takes in place of a DM key's and requires, or answers `400 invalid_field`. Their files go as a DM's (see [DM files](#dm-files)), each sealed under a key of its own that the sealed text carries.
- Replies, edits, deletes, ticks as edits, and read state work as in a DM, and sending a note reads the notes up to it; nothing in them counts toward an unread count. The den refuses reactions, polls, other editors, disappearing times and closing (`400 invalid_field`), and a spoiler outside the seal, as in a DM.
- The den's retention deletes notes as any message, and their files count toward the member's upload limits.
- A member who leaves the den, or is removed, takes their notes with them: the den deletes the channel, with its messages and files.
- A member who starts over (see [Starting over](#starting-over)) has a new seal, and so a new key: the notes from before no longer open, and a client shows them as lost, for the member to delete.
- At version 1 or 2 the den sends nothing of notes, in `ready`, events, history or the change log, and `POST /api/notes` answers `404 not_found`, since an older client closes its connection on a channel of a kind it doesn't know.

## Approving new devices (M1.7)

A password sign-in on a new device waits for one of the member's other devices to approve it. The two run an exchange of one-time keys through the den, and the member types each device's digits into the other. The approving device then hands over the member's DM seal, sealed with the exchange's key, from which the new device makes the member's identity (M16.1).

The exchange has three messages, each sent when its device is online:

1. **Offer**, from the new device: `{"ek", "commit"}`, a fresh ML-KEM-768 encapsulation key, and `commit = SHA-256("dens-exchange-commit-v1" ‖ len(ctx) ‖ ctx ‖ x ‖ nonce)` over a fresh X25519 public key `x` and 32 random bytes.
2. **Answer**, from the approving device: `{"x", "nonce", "ct"}`, its own fresh X25519 key and 32 random bytes, and a ciphertext encapsulated to `ek`.
3. **Reveal**, from the new device: `{"x", "nonce"}`, which must open `commit`.

Both sides hash the whole exchange, `transcript = SHA-256(len(ctx) ‖ ctx ‖ ek ‖ commit ‖ answer's x ‖ answer's nonce ‖ ct ‖ reveal's x ‖ reveal's nonce)`, and derive from `secret = X25519 shared secret ‖ ML-KEM shared secret` the exchange's key, `HKDF-SHA-256(secret, salt = transcript, info = "dens-exchange-key-v1")`, 32 bytes, and its check code, 16 bytes with `info = "dens-exchange-check-v1"`, read as two big-endian 64-bit numbers, each modulo 10^16, and written as 32 digits. Everything that feeds the code is fixed before its sender sees the other side's part, so a den in the middle, running one exchange with each device, gets one guess at 32 digits.

```
POST /api/auth/password                  {…, "offer"}             202 {"pending_token", "expires_at"}
GET  /api/auth/pending?after=<version>   Bearer <pending token>   200 {"version", "status", "answer"?, "session"?, "handover"?}
POST /api/auth/pending/reveal            Bearer <pending token>   {"reveal"}   204

POST /api/me/device-requests/{id}/answer    {"answer"}     200 device_request
POST /api/me/device-requests/{id}/approve   {"handover"}   204
POST /api/me/device-requests/{id}/refuse                   204

device_request = {"id", "key_id", "label", "requested_at", "expires_at", "offer", "answer"?, "answered_by"?, "reveal"?}
```

1. The new device starts the exchange with `ctx = "dens-exchange-device-v1" ‖ den_id ‖ len(username) ‖ username ‖ key_id`, its own key ID, and sends the offer with its sign-in. The den checks the password and holds the request for 10 minutes, in memory only; a member may have 3 waiting.
2. The member's sessions get `device.request {device_request}`, and `ready` lists the waiting ones as `device_requests`. The device the member approves from answers, and becomes `answered_by`, which alone may approve.
3. The new device asks after its sign-in with the pending token, which is no session token. The den answers once `version` passes `after`, or after 20 seconds with nothing new. `status` is `waiting`, `answered` (with the answer, to which the new device reveals), `approved`, `refused` or `cancelled`. An unknown or lapsed token gets `401 unauthorized`.
4. Each device shows its half of the check code, the new device, as the starter, the first 16 digits, and the member types each into the other.
5. The approving device, once its member typed the new device's digits, sends `handover`: the seal, sealed with XChaCha20-Poly1305 under `HKDF-SHA-256(exchange key, info = "dens-seal-handover-v1")` and bound to `ctx`, 72 bytes. The den registers the new device, starts its session, tells the member's sessions with `device.added`, and answers the new device with `approved`, `session` (as a recovery returns it) and `handover`.
6. The new device opens the handover only once its member typed the approving device's digits, and keeps the seal only if it matches the session's `seal_check`.

- Refusing, from any of the member's devices, keeps the device out. A new password, starting over, or the member leaving cancels what's waiting. Each way, the member's sessions get `device.request_ended {"id", "outcome": "approved" | "refused" | "cancelled"}`. A request that lapses just goes.
- The den enforces approval, but a DM's privacy doesn't rest on it: a device the den lets in on its own has no seal, so it opens no DM, and the identity it publishes isn't the member's, which others see as a change, and in a comparison of codes (M16.1).

## Voice (M2)

A call is a session between one member's browser and the den, in a voice channel. The den runs the media side. The client relays the signaling below between its socket to the den and its member's browser, and decides where the browser sends media (see the design doc's Voice and screen share).

```
call = {"channel_id", "members": [{"id", "muted"?, "deafened"? (M3.3), "staff_muted"? (M3),
                                   "sharing"? (M4.2), "sound"? (M4.2), "watching"? (M4.2)}, …]}
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

- Staff disconnect and mute members of a lower rank, as they remove them: moderators act on members, and the owner on anyone else. Anyone else gets `403 forbidden`, and an ID that isn't a member in the den gets `404 not_found`.
- Disconnecting ends the member's call, held or not, with `voice.ended` and `disconnected_by_staff`. It leaves a member in no call as they are. They can join again at once.
- A staff mute makes the den forward nothing from the member, in their call and in any they join, until staff lift it with `"muted": false`, the member leaves the den, or the den restarts. It ends their share, and they can't share while it lasts (M4.2). It can be set whether or not they're in a call. Calls show it as `"staff_muted": true` on the member, beside their own `muted`.
- Moving (M17.2) moves a member's call to another voice channel, with the rank rule disconnecting takes. The den checks what joining does: a channel that isn't a voice channel the member can see gets `404 not_found`, and one whose call holds the den's `members` already `409 call_full`. A call held while its socket is away has nothing to tell, and gets `409 call_held`. Otherwise the den sends `voice.move {"channel_id", "to"}` to the socket holding the call, the call's channel and the one to join, and answers 204; the member's client joins `to` as if they'd picked it, with `voice.join`, sealed, muted and deafened as they were, which ends the old call. That join, within 30 seconds, doesn't count toward the member's limit on joining. A member in no call, or in that channel's already, stays as they are, with 204. A den that moves members says `"move_members": true` in its info, so a client offers it only then; a client older than it ignores `voice.move`, and stays where it is.

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

## Emoji (M15.2)

Staff add emoji to the den, which members write by name in the markdown subset, as `:name:` (see the design doc's Emoji).

```
emoji        = {"id", "name", "image": image, "added_by", "added_at"}
emoji_limits = {"emojis"}

POST   /api/emoji        {"name", "file_id"}   201 emoji        (staff only)
PATCH  /api/emoji/{id}   {"name"}              200 emoji        (staff only)
DELETE /api/emoji/{id}                         204              (staff only)
```

- An emoji is one of the den's named files: its name, 2 to 32 of `a-z`, `0-9` and `_`, lowercased on entry and unique among the den's emoji; its `image`, an upload, with its size and `animated` when it's an animated GIF; and who added it and when. `ready` carries every emoji, by name, and each change reaches every member as `emoji.created {emoji}`, `emoji.updated {emoji}` or `emoji.deleted {"id"}`. An emoji's image, and its preview, open with `GET /api/files/{id}` for every member, since every page draws it.
- `POST /api/emoji` takes an upload of the staff member's own, waiting to be used, and makes it the emoji's image, which then counts toward the den's space, not theirs, isn't among their files, and stays until staff delete the emoji: retention, the sweep of waiting uploads and the uploader leaving don't remove it, and no message, picture or replacement takes it. The upload must be a still PNG, JPEG, GIF or WebP, or an animated GIF, at most 128 pixels each way, and at most 256 KiB, or 512 KiB animated. A name another emoji has gets `409 emoji_name_taken`; any other upload, a den holding `emojis` emoji already, or emoji off gets `400 invalid_field`; anyone but staff gets `403 forbidden`.
- `PATCH` renames an emoji, under the same rule for names; `DELETE` deletes it with its image. The den never reads messages for emoji, so the messages that wrote its old name keep the text, which pages show as text, or as another emoji given the name.
- `emoji_limits`: `emojis` is how many emoji the den keeps, 0 to 500, where 0 turns emoji off and keeps them: no emoji is added, and pages show their names as text. A new den keeps 50. The owner changes it with `PATCH /api/den`. A lower count applies to emoji added afterwards: the emoji already added stay. The den sends it at 0 too, so a client tells emoji turned off from an older den.
- An older den sends no `emoji_limits` and no `emoji`, and clients show none of its emoji and offer no way to add one; clients check `emoji_limits` and every emoji as they arrive, and refuse a den whose are out of range. An older client shows `:name:` as the text it is.
- Adding, renaming and deleting an emoji count toward the member's other writes.

## Reactions (M15.3)

A reaction is a member's emoji on a message: one of Unicode's, from the list (`denproto.IsEmoji`), or one of the den's (see the design doc's Reactions).

```
reaction        = {"emoji"?: "<one of Unicode's>", "emoji_id"?: "<one of the den's>", "members": [member id, …]}
sealed_reactions = {"member_id", "key_id", "revision", "sealed"}

POST /api/messages/{id}/reactions          {"emoji"? | "emoji_id"?}               204
POST /api/messages/{id}/reactions/remove   {"emoji"? | "emoji_id"?}               204
POST /api/messages/{id}/reactions/clear    {"emoji"? | "emoji_id"?}               204   (staff only)
PUT  /api/messages/{id}/reactions/sealed   {"revision", "key_id", "sealed"}       200 sealed_reactions
```

- A reaction names one emoji, `emoji` or `emoji_id`, never both. A channel's message carries its `reactions`: each emoji with the members who reacted with it, in the order each emoji came and each member reacted, in history, in what changed while away, and in `message.updated`; a new message has none. It holds at most 20 different emoji and 10,000 reactions in all; a client refuses a message from the den with more.
- `POST /api/messages/{id}/reactions` adds the member's reaction to a message in a text channel they can see: one of Unicode's emoji, fully qualified, as the list has it, or one of the den's by ID while its `emoji_limits` aren't 0. Anything else gets `400 invalid_field`, as does a 21st different emoji, a message at 10,000, or a DM's message; a message they can't see gets `404 not_found`. `/remove` takes it back. Reacting again, or taking back one that isn't there, changes nothing and tells nobody.
- `/clear`, staff's, takes one emoji's reactions off a channel's message: those of the members of a lower rank than theirs, as their other powers reach. Anyone else gets `403 forbidden`, and so does a DM's message, whose reactions are its members' alone.
- `reaction.added` and `reaction.removed` carry `{"message_id", "channel_id", "member_id", "emoji"? | "emoji_id"?}`, one member's one emoji, and `reaction.cleared` carries `{"message_id", "channel_id", "emoji"? | "emoji_id"?, "members"}`, whose reactions went; each goes to those who can see the channel. A client applies each to its emoji's set on the message, so applying one twice changes nothing.
- A reaction with one of the den's emoji goes as the emoji is deleted: the den tells of it by `emoji.deleted` alone, by which a client drops its chips, and records the messages it was on in the change log. A ban with `delete_messages` takes the reactions the member added in that time too, in channels, each as `reaction.removed`.
- Each change records its message in the change log, its revision as it was (see [What changed while away](#what-changed-while-away-m6)). Reactions mention nobody and notify nobody.

**A DM's.** Each member's reactions on a DM's message are one set, which their client seals: `{"emoji": [...]}` as JSON, each one of Unicode's emoji from the list or a name one of the den's emoji may have, as a DM's text writes them, each once, at most 20, padded with spaces to 1,024 bytes, and sealed with XChaCha20-Poly1305 under the DM's live key, bound to `"dens-dm-reactions-v1" ‖ den_id ‖ channel ‖ message ‖ member ‖ key_id ‖ revision`, as nonce ‖ ciphertext, 1,064 bytes.

- `PUT /api/messages/{id}/reactions/sealed` sets the member's set: `revision` is the revision of their set it was made against, 0 while they have none, and `sealed` is sealed as the revision after it. `key_id` must name the DM's live key, and one the member checked, as for a message, and `sealed` must be 1,064 bytes; otherwise `400 invalid_field`. A set made against another revision gets `409 edit_conflict` with the member's set as it is, `{"error": {…}, "reactions": sealed_reactions}`, without `reactions` while they have none, and the client makes it again on that one. Once the other member has left, `403 forbidden`.
- The den answers with the set as it keeps it, and sends it to both members as `dm.reactions {"message_id", "channel_id", "member_id", "key_id", "revision", "sealed"}`; a DM's message carries its members' sets as `dm_reactions`, a set a member, in the order of their IDs. A client keeps the newer of a member's sets by `revision`, and opening one keeps only emoji from the list and names a den's emoji may have, and drops anything else; one that doesn't open shows none.
- A set stays once made, empty or not, and goes with its message. The den sees that a member's reactions changed, and when, never what they are.

**Older ones.** An older page shows no reactions. An older den sends none and takes none; a client offers none for a den with no `emoji_limits`, since reactions came with emoji.

## Polls (M15.4)

A channel's message may be a poll, whose question its text is (see the design doc's Polls). A poll as the den sends it is as the member it's sent to may see it, by its rules: in history, in what changed while away, in `message.created`, and in its events.

```
poll         = {"choices": ["…", …], "multiple"?: true, "ends_at", "show_voters": "never" | "end" | "always",
                "show_totals": "live" | "voted", "ended"?: true, "voted", "totals"?: [n, …], "voters"?: [[member id, …], …],
                "mine"?: [choice, …]}
poll_request = {"choices": ["…", …], "multiple"?: true, "hours", "show_voters", "show_totals"}

POST /api/channels/{id}/messages     {"nonce", "text", "poll": poll_request, "reply_to"?}   201 message
POST /api/messages/{id}/poll/vote    {"choices": [choice, …]}                              200 poll
POST /api/messages/{id}/poll/end                                                           200 poll   (its author only)
```

- A poll goes with a message in a text channel, which has a question, and no `attachments` or `editors`; in a DM, or with either, `400 invalid_field`. It has 2 to 10 `choices`, each 1 to 80 characters with something visible, which the den cleans as display names are, keeping each of Unicode's emoji whole, joiners and tags and all, no two the same in any case; `hours`, how long it runs, is 1 to 336; `multiple` lets a member pick more than one; and its two rules are fixed once it's posted: `show_voters`, when who voted for what shows, `never`, at its `end`, or `always`, with the totals; and `show_totals`, when the totals show, `live`, as votes come in, or once the member `voted`, whose vote is then final. Anything else gets `400 invalid_field`. The den keeps the choices as cleaned, which the message shows.
- `ends_at` is when it ends, and `ended` says it has, at its time or early by its author, who moves `ends_at` to then; one past `ends_at` is ended, though the den's word on it may still be coming. `voted` is how many members voted, which everyone sees. `totals`, each choice's count, come once the member may see them: always under `live`, under `voted` once they voted, and to everyone once it ended. `voters`, each choice's members in the order they voted, come with the totals under `always`, and to everyone once it ended under `end`; never under `never`. `mine` are the member's own votes, by choice, while they have any.
- `/vote` sets the member's vote: the choices they pick, by their place in `choices`, each once, one unless `multiple`; none takes it back. The poll must be open, or `403 forbidden`; under `voted`, a member who voted gets `403 forbidden`, their vote final. The same vote again changes nothing and tells nobody. The answer is the poll as the member now sees it. `/end` is its author's alone, anyone else getting `403 forbidden`; one that ended stays as it is. A message without a poll gets `400 invalid_field` from either, and one the member can't see `404 not_found`. A poll can't be edited: `PATCH /api/messages/{id}` and its ticks get `403 forbidden`.
- `poll.updated {"message_id", "channel_id", "voted", "totals"?, "voters"?, "mine"?}` carries how a poll stands, as its audience may see it: to the member's own sessions as their vote changes, with `mine`, their votes, an empty list once they take it back; and in batches every second or two, without `mine`, to those who see the channel under `live`, and under `voted` with the totals to the poll's voters and with only `voted` to the rest. A client sets `voted`; where an update carries totals, the totals and the voters that come with them; and where it carries `mine`, the member's own votes; it keeps the rest as it holds them, and applies none to a poll that ended.
- `poll.ended {"message_id", "channel_id", "ends_at", "voted", "totals", "voters"?}` carries the final totals, and who voted for what under `end` and `always`, to everyone who sees the channel, as the poll ends.
- A client checks a poll and its events as they arrive: its choices clean, its tally in range, each count at most `voted` and the voters it names as many, and its member's votes its own choices; one that doesn't fit is refused, and an event whose tally doesn't fit the poll's choices changes nothing.
- Each vote and each end records its message in the change log, its revision as it was (see [What changed while away](#what-changed-while-away-m6)). A poll goes with its message, and its votes with it.
- **@everyone.** Staff's `@everyone` in a channel, found by the mention rule, mentions each member who can see the channel but the author, as a mention by name does, and marks the message `everyone`, a client's sign to count it as a mention of its member and highlight it. It counts while the text holds it and staff put it there: an edit by anyone that keeps a marked one keeps the mark, and one that adds it marks the message only where staff make it. Anyone else's mentions the member named `everyone`, if the den has one; nobody new takes that username, which `/api/join` answers as `409 username_taken`.
- An older page shows a poll's question alone, and an @everyone as a mention of nobody, though the den counts it among the member's mentions. An older den takes no poll: a client offers none where the den sends no `emoji_limits`, which came with them.

## Events (M17.4)

A den's events: each a name, a description, an optional banner, a start, an end and a place, which members mark Interested, and which the den starts and ends at their times (see the design doc's Events).

```
event        = {"id", "name", "description"?, "banner"?: image, "starts_at", "ends_at", "channel_id"? | "place"?, "made_by",
                "live"?: true, "interested", "mine"?: bool, "badge"?: {"title", "image": image}, "ended"?: true}
event_limits = {"member_events"}

POST   /api/events                {"name", "description"?, "banner"?, "starts_at", "ends_at", "channel_id"? | "place"?,
                                   "badge"?, "badge_title"?}                                               201 event
GET    /api/events/{id}                                                                                  200 event   (M17.5)
PATCH  /api/events/{id}           {"name"?, "description"?, "banner"?, "starts_at"?, "ends_at"?, "channel_id"? | "place"?,
                                   "badge"?, "badge_title"?}                                               200 event
DELETE /api/events/{id}                                                                                  204   (to come)
POST   /api/events/{id}/end                                                                              204   (live)
PUT    /api/events/{id}/interest                                                                         200 {"event_id", "interested", "mine"}
DELETE /api/events/{id}/interest                                                                         200 {"event_id", "interested", "mine"}
```

- An event has a `name`, 1 to 100 characters with something visible, cleaned as a poll's choice is, each of Unicode's emoji kept whole; a `description` of up to 2,000 characters in the markdown subset, links cleaned as a message's are; `starts_at` and `ends_at`, Unix milliseconds; and one place: `channel_id`, a voice channel the member sees, or `place`, a line of text cleaned as its name is, up to 100 characters. Its start is at most a year ahead and no earlier than a minute ago, which counts as now, and it lasts 5 minutes to a week; a development instance's den takes lengths from a second, for the browser checks. `banner` names an upload of the member's own, waiting to be used, a still image or an animated GIF three times as wide as it's high, as a profile's banner is (see [Pictures on a profile](#pictures-on-a-profile)), or `""` takes it off; the banner it replaces is deleted. Anything else, both places or neither, gets `400 invalid_field`. `made_by` is its maker; `live` says it started; `interested` is how many of those who see it marked it; and `mine`, which only `ready`, the answers and the events to the member's own sessions carry, is whether the member did.
- Staff make events. A member who isn't staff makes them while `event_limits.member_events`, 0 to 10, is above how many of theirs haven't ended; at 0, the default, events are staff's, and making one gets `403 forbidden`, as does one past the member's limit, or the den's 50 that haven't ended. The owner changes it with `PATCH /api/den`; a lower limit leaves the events already made. The den sends `event_limits` at 0 too, so a client tells events from an older den, which sends none.
- `PATCH` changes what it carries. Its maker changes an event, and staff those of the members they outrank, as calls and messages have it; anyone else gets `403 forbidden`. A live event's `starts_at` stays where it was, or `400 invalid_field`, and its `ends_at` is now or later, within its length's limits. `DELETE` cancels an event to come, with its banner; a live one gets `403 forbidden`, and is ended with `/end`, which ends it now. `/end` on an event to come gets `403 forbidden`, and on one that ended already `204` with nothing changed. An event the member doesn't see gets `404 not_found` from all of them, and one that ended from the rest.
- `/interest` marks the member Interested, or with `DELETE` takes it back, in an event to come or live they see, and answers with how many are and their mark. The same mark again changes nothing and tells nobody.
- An event in a staff-only channel is staff's alone: in `ready`, its events, its banner and its routes, and its count holds staff alone. It follows its channel, and moves with staff who move it: as either changes who sees it, members who aren't staff start over from a fresh `ready`, as a channel's visibility has them do. A deleted voice channel's events to come go, and its live one ends.
- The den's events, each to everyone who sees the event: `event.created {event}` and `event.updated {event}`, without `mine`; `event.canceled {"id"}`; `event.ended {"id"}`; and `event.interest {"event_id", "interested"}`, with `mine` to the member's own sessions as their mark changes. As an event starts, the den sends `event.started {event}` to the members interested in it, an audience fixed as it's sent, which a socket that resumes soon after still hears from the ring, and `event.updated {event}`, live, to the rest who see it; a client whose socket wasn't connected then, and resumes past the ring, or starts from `ready`, hears no `event.started`. As it ends, at its end or early, the den forgets who was interested. An event that passed whole while the den was stopped ends without starting.
- A member's events to come go as they leave the den, or are removed, with their banners, and their marks on the rest, whose counts everyone who sees them hears; their live events go on to their end.
- A client checks each event as it arrives, refusing a den whose events don't fit: its IDs, its name and place as cleaning leaves them, one place, its end after its start and within a week of it, its banner's size, and how many are interested; and at most 50 of them in `ready`. It keeps the events with the den's state, so they show while the den is away.
- Making, changing, canceling and ending an event, and marking Interested, count toward the member's other writes. An older client ignores the events and their routes; an older den sends no `event_limits`, and a client offers no events there.

### Cards and badges (M17.5)

```
badge         = {"event_id", "title", "image": image, "event", "starts_at", "earned_at", "hidden"? (M17.6)}   in GET /api/members/{id}'s "badges"
```

- `GET /api/events/{id}` answers with one of the den's events the member sees, to come, live or ended, for a link's card: with `mine`, and for one that ended `ended`, nobody interested and no mark. An event the member doesn't see, or that's gone, gets `404 not_found`. A client answers a card from the events it keeps where it can, so the den hears of an event to come only as `ready` and its events tell it, and asks for the rest.
- An ended event stays on the den, with its banner, out of `ready`, for its cards and its badge. Its place keeps who saw it: one whose voice channel was deleted has no place, and stays staff's alone if the channel was. A client takes an ended event only from this route.
- `badge` and `badge_title`, which staff alone send, attach a badge to an event in a voice channel or change it; anyone else sending them gets `403 forbidden`. `badge` names an upload of the staff member's own, waiting to be used, a still image or an animated GIF at most 128 pixels on each side and 256 KiB, or 512 KiB animated, as an emoji's image is (see [Emoji](#emoji-m152)), or `""` takes the badge off; `badge_title` is 1 to 40 characters, cleaned as an event's name is. Attaching one takes both; changing one, either. An event with a badge stays in a voice channel: moving it to a `place` without taking the badge off in the same request, or attaching one to an event somewhere else, gets `400 invalid_field`. A picture replaced, or a badge taken off, goes, and so do the badges of events canceled, or that go with a member who leaves or a channel deleted. An ended event's badge stays as it was.
- A badge's picture is one of the den's named files, of kind `badge`, so it stays the den's whoever added it, and opens with `GET /api/files/{id}` to everyone who sees its event.
- A member earns an event's badge by a minute at a stretch in its call while it's live, their call held or not (see [Riding out a dropped connection](#riding-out-a-dropped-connection-m3)), counting from the event's start for one in the call before it; once each, and nothing for a stretch that reaches its minute only after the event ends. A development instance's den takes three seconds, for the browser checks. The den tells nobody: the badge shows on the member's profile.
- `GET /api/members/{id}` carries the badges the member earned whose events the asker sees, latest first, up to 500, each with its event's ID and name and its start, and when they earned it. A client checks each: its IDs, its title and the event's name as cleaning leaves them, a picture of an emoji's size, its times, and each event once.
- A badge its member hid (M17.6) is left out of everyone's answer but the member's own, where it carries `"hidden": true`; a client takes `hidden` on the member's own profile alone, and a profile card shows only the badges without it. Hiding one keeps a card tidy and no more: the den keeps the award as it was.

## Rate limits

The den limits per IP, taken from `X-Forwarded-For` only on connections from loopback, and per member. Limits live in memory only. Starting values, to tune with real use:

| What | Limit |
| --- | --- |
| Challenges | 30 per minute per IP |
| Join and join preview | 10 per hour per IP |
| Sign-in with a device key | 30 per minute per IP |
| Password sign-in and recovery | 10, then 1 every 6 minutes, per IP and per username. A signed-in member's password checks count against the username's |
| A new device asking after its sign-in, and revealing | 30 per minute per IP |
| Sending messages | 5 per 5 seconds per member, per channel |
| A member's messages, across every channel and DM (M8) | 10, then 1 every half second; and 256 KiB of text, a poll's choices among it (M15.4), then 16 KiB a second |
| Typing notices | 1 per 2 seconds per member, per channel; the rest are dropped |
| Uploads, and a video's preview | 20, then 1 every 3 seconds per member; an upload that sends nothing for a minute is dropped |
| A request's body, other than an upload's | A minute to arrive in full (M8) |
| Joining a call, but for the join a move asked for (M17.2) | 10 per minute per member |
| Other writes, including mute, ICE restarts, sharing and watching, staff disconnecting, muting and from M17.2 moving, from M14.3 playing a sound and adding, renaming and deleting one, from M15.2 adding, renaming and deleting an emoji, from M15.3 adding a reaction, taking one back, staff taking an emoji's off, and setting a DM's set, from M15.4 voting and ending a poll, from M16.1 publishing an identity, making a DM's key and writing the record, from M16.4 setting a DM's or a group's time, from M17.4 making, changing, canceling and ending an event, and marking Interested, and from M17.6 hiding or showing a badge | 30 per 10 seconds per member |
| Lighting the push-to-talk ring (M11) | 5 per second per member; a release always passes |
| A member's keys for calls (M16.2) | 30 per 10 seconds per member; the rest are dropped |
| WebSocket connections | 5 open per member, 20 upgrades per minute per IP |
