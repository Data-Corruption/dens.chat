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
- Every request and response is at most 1 MiB, except uploads and files (see [Files](#files-m14-m18)).
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
          "event_limits"? (M17.4), "name_styles"? (M19.5), "relay"? (M20)}
member = {"id", "username", "display_name", "role": "member" | "moderator" | "owner", "joined_at", "left_at"?,
          "avatar"?, "banner"?, "identity"? (M16.1), "name_style"? (M19.5)}
image  = {"id", "width", "height", "animated"? (M15.1)}
name_style = {"font"?, "color"?, "color2"?}
```

`url` is the den's public URL, which the owner sets when creating the den. Clients build invite strings from it, and the den signs it into every challenge answer (see [Authentication](#authentication)). Den names follow the display name rules, up to 32 characters. `limits` are the den's upload limits (see [Files](#files-m14-m18)), `call_limits` its limits for calls and screen shares (see [Limits for calls](#limits-for-calls-m42)), `retention` its retention period in days, when it has one (see [Retention](#retention-m5)), `sound_limits` its soundboard's (see [The soundboard](#the-soundboard-m143)), `emoji_limits` its limit for emoji (see [Emoji](#emoji-m152)), `sealed_calls` that it seals its calls end to end (see [Sealed calls](#sealed-calls-m162)), `disappearing` that it deletes a DM's and a group's messages at the time their members set (see [Disappearing messages](#disappearing-messages-m164)), `spoilers` that it keeps which of a channel's files went as spoilers (see [Attachments](#attachments)), `move_members` that staff move members from one call to another (see [Staff in calls](#staff-in-calls-m3)), `event_limits` its limit for members' events, which says it has events (see [Events](#events-m174)), `name_styles` that it keeps a style for each member's name (see [Members and roles](#members-and-roles-m13)), and `relay` that it passes boxes between a member's devices (see [Relay](#relay-m20)); they come in `ready` and `den.updated`, and not in a join preview. `avatar` and `banner` are the pictures on a member's profile (M1.4), each an `image`: its upload, its size, and `animated` when it's an animated GIF (M15.1). `identity` is the identity the member's client published, by its 32-byte hash (M16.1; see [Private DMs](#private-dms-m17)), until they leave or start over.

```
PATCH /api/den   {"name"?, "url"?, "limits"?, "call_limits"?, "retention"?, "sound_limits"?, "emoji_limits"?, "event_limits"?}   200 den   (owner only)
```

A change reaches every member as a `den.updated {den}` event.

`ready` carries `members`: everyone who has been in the den, so clients can name authors and resolve mentions; from M6.2, to a client that kept the list, only those whose records changed since (see [What changed while away](#what-changed-while-away-m6)). A member who left or was removed keeps their record with `left_at` set. A new or returning member reaches everyone as `member.joined {member}`; see [Members and roles](#members-and-roles-m13) for the rest.

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
| Other writes, including mute, ICE restarts, sharing and watching, staff disconnecting, muting and from M17.2 moving, from M14.3 playing a sound and adding, renaming and deleting one, from M15.2 adding, renaming and deleting an emoji, from M15.3 adding a reaction, taking one back, staff taking an emoji's off, and setting a DM's set, from M15.4 voting and ending a poll, from M16.1 publishing an identity, making a DM's key and writing the record, from M16.4 setting a DM's or a group's time, from M17.4 making, changing, canceling and ending an event, and marking Interested, from M17.6 hiding or showing a badge, and from M21.2 staff resetting a channel's longest call | 30 per 10 seconds per member |
| Lighting the push-to-talk ring (M11) | 5 per second per member; a release always passes |
| A member's keys for calls (M16.2) | 30 per 10 seconds per member; the rest are dropped |
| Boxes between a member's devices (M20) | 30 per 10 seconds per member, all their devices together; the rest are dropped |
| WebSocket connections | 5 open per member, 20 upgrades per minute per IP |
