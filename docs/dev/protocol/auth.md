# Den protocol: sign-in, devices and Relay

Joining, invites, sessions, the password and recovery codes, a member's devices and approving new ones, and Relay's boxes between a member's own devices. The conventions, versions, errors, keys, objects and rate limits every part follows are in [protocol.md](../protocol.md).

## Authentication

Every sign-in starts with a challenge, in which the den proves its identity key and hands out a nonce:

```
POST /api/auth/challenge
{"client_nonce": "<32 bytes>"}

200 {"nonce": "<32 bytes>", "den_key": "<Ed25519 public key>", "den_sig": "<64 bytes>", "url": "https://den.example.com"}
```

- `den_sig = Sign(den_key, "dens-den-v1" ‖ client_nonce ‖ nonce ‖ url)`, where `url` is the den's public URL in UTF-8. It comes last, so it needs no length.
- Before sending anything else, the client checks `SHA-256(den_key) == den_id`, the signature, and that `url` is the address it sent the challenge to. Addresses compare after normalizing: lowercase host, no trailing slash. A client signing in by address, with no invite to pin `den_id`, takes the key the den proves (see [Password and recovery](#password-and-recovery)).
- `nonce` is valid for 60 seconds and one use. The den holds outstanding nonces in memory, with a bounded count.

**Following a move.** A signed `url` other than the address the client reached means the den has moved, or that a server at another address is passing the den's answers along. Either way the client sends nothing more there. It runs the challenge again at `url`, and goes on only if the den proves the same key there and signs that same address. It keeps the new address from then on, and follows one move per challenge: a den whose new address signs yet another is refused.

- A server can pass a den's challenge answer through, but can't change the address signed into it, so a proof, verifier or token never goes to an address the den didn't sign.
- A client that reaches the den its own install hosts over loopback skips the address check: that den signs its public address, and nothing sits between the two.
- A connected client that gets `den.updated` with a new `url` runs the challenge there at once, and moves if the den proves itself there, so its token stops going to the old address. If the new address doesn't answer yet, the client stays where it is until its next challenge sends it on. Either way it keeps, and shows, a new address only once the den has proven itself there.
- A den chooses the addresses its members' clients ask, so a hostile one could turn them into a crowd asking someone else's server, or their own computers' services. A client checks a new address for a den at most once a minute, and refuses a move from an `https` address to a plain `http` one, or from an address elsewhere to one on its own computer. A move it refuses or puts off leaves the den offline at its old address, saying why.
- The den never answers with a redirect, and a client follows none.
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
- `seal_check` identifies the DM seal the joining client holds (see [Private DMs](e2ee.md#private-dms)). A former member who comes back with another seal starts over (see [Starting over](e2ee.md#starting-over)).
- **Owner bootstrap:** creating a den also creates a one-use owner invite, which the owner's own client redeems over loopback. The den makes the first member to redeem it the owner. There is no second path for creating the owner account. If that first join fails, the page can have the install issue a fresh owner invite, but only while the den has no owner. The den's machine also makes one for a den that has an owner, with the local password (see [Invites](#invites)).

### Invites

```
POST   /api/invites          {"expires_in": 86400, "max_uses": 1}   201 {"id", "code", "created_by", "created_at", "expires_at", "max_uses", "uses"}
GET    /api/invites                                                 200 {"invites": [… without code …]}
DELETE /api/invites/{id}                                            204
```

- The client builds the invite string from `den.id`, `code` and `den.url`, and shows it once. The den stores only `SHA-256(code)`, so a code can't be shown again, only revoked.
- `expires_in` is at most 30 days, and a day when left out. `max_uses` is 1 to 100 and defaults to 1.
- Moderators and the owner create, list and revoke invites. The list names who made each (`created_by`).
- An owner invite has the owner role, and only the den's own machine makes one: as the den is created, from the page or with `dens den create`, and with `dens den owner-invite`, which refuses while hosting is off. It lasts an hour and is used once, and doesn't appear in the list. Redeeming it makes the joiner the owner and the owner before them a moderator: the joiner reaches everyone as `member.joined`, as any joiner does, and the owner before them as `member.updated`.

### Sessions

```
POST /api/auth/login
{"key_id": "<32 bytes>", "nonce": "<32 bytes>", "proof": "<64 bytes>"}
200 {"token": "<32 bytes>", "expires_at": …}
```

- The token goes in `Authorization: Bearer <token>` on every other request and on the WebSocket upgrade. The den stores `SHA-256(token)`.
- A token lasts one hour. The client renews it on the open WebSocket (see below), so there are no refresh tokens.
- `POST /api/auth/logout` ends the session. Revoking a key, or banning or kicking a member, ends its sessions and closes their sockets.

### Password and recovery

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

- A password alone doesn't add a device: the sign-in waits for one of the member's other devices to approve it, and `offer` starts the exchange the two run (see [Approving new devices](#approving-new-devices)). A member with no other device gets `403 no_other_device`, and signs in with a recovery code instead.
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
- A wrong password or code gets `403 wrong_password`. It counts against the username's guesses like a sign-in (see [Rate limits](../protocol.md#rate-limits)).

### Devices

```
GET    /api/me/devices            200 {"devices": [device, …], "recovery_codes_left": 9}
DELETE /api/me/devices/{key_id}   204

device = {"key_id", "label", "created_at", "last_seen_at", "current"?}
```

- `label` is the `device_label` the device signed in with, cleaned as a display name is, up to 64 characters. `last_seen_at` is the key's last sign-in or renewal of its session. `current` marks the device making the request.
- Revoking a key ends its sessions and closes its sockets at once, with 4003 `key revoked`. Revoking the current device signs it out. A key that isn't the member's gets `404 not_found`.
- The member's own sessions get `device.removed {"key_id"}`, for a revoked key and for each one a new password, or starting over, signs out. `device.added` and `device.removed` are durable, `device.connected` isn't (see [Approving new devices](#approving-new-devices)), and none goes to anybody else.

## Approving new devices

A password sign-in on a new device waits for one of the member's other devices to approve it. The two run an exchange of one-time keys through the den, and the member types each device's digits into the other. The approving device then hands over the member's DM seal, sealed with the exchange's key, from which the new device makes the member's identity.

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

1. The new device starts the exchange with `ctx = "dens-exchange-device-v1" ‖ den_id ‖ len(username) ‖ username ‖ key_id`, its own key ID, and sends the offer with its sign-in. The den checks the password and holds the request for 10 minutes, in memory only; a member may have 3 waiting, and the den 1,000 in all.
2. The member's sessions get `device.request {device_request}`, and `ready` lists the waiting ones as `device_requests`. The device the member approves from answers, and becomes `answered_by`, which alone may approve.
3. The new device asks after its sign-in with the pending token, which is no session token. The den answers once `version` passes `after`, or after 20 seconds with nothing new. `status` is `waiting`, `answered` (with the answer, to which the new device reveals), `approved`, `refused` or `cancelled`. An unknown or lapsed token gets `401 unauthorized`.
4. Each device shows its half of the check code, the new device, as the starter, the first 16 digits, and the member types each into the other.
5. The approving device, once its member typed the new device's digits, sends `handover`: the seal, sealed with XChaCha20-Poly1305 under `HKDF-SHA-256(exchange key, info = "dens-seal-handover-v1")` and bound to `ctx`, 72 bytes. The den registers the new device, starts its session, tells the member's sessions with `device.added`, and answers the new device with `approved`, `session` (as a recovery returns it) and `handover`.
6. The new device opens the handover only once its member typed the approving device's digits, and keeps the seal only if it matches the session's `seal_check`.
7. As the new device's first socket opens, while its request lasts, the member's sessions get `device.connected {"key_id"}`, unnumbered and kept for no replay, on which the approving device stops showing its digits. A den older than v1.14.0 sends none, and the approving device's member closes them.

- Refusing, from any of the member's devices, keeps the device out. A new password, starting over, or the member leaving cancels what's waiting. Each way, the member's sessions get `device.request_ended {"id", "outcome": "approved" | "refused" | "cancelled"}`. A request that lapses just goes.
- The den enforces approval, but a DM's privacy doesn't rest on it: a device the den lets in on its own has no seal, so it opens no DM, and the identity it publishes isn't the member's, which others see as a change, and in a comparison of codes.

## Relay

A member's installs link across their network, and the den only passes boxes between them (see the design's [Relay](../design/helper.md#relay)).

```json
[{"t": "relay.signal", "d": {"to": "<key id>", "box": "<1,024 bytes>"}}]
{"t": "relay.signal", "d": {"from": "<key id>", "to": "<key id>", "box": "<1,024 bytes>"}}
```

- A client's `relay.signal` goes to the member's other devices, or with `to` to the device with that key ID alone, as an ephemeral `relay.signal` on their sockets, with `from` the key ID of the device it came from and `to` as it was. It never goes back to the sender's device, nor to another member's, and the den keeps nothing of it: a device that comes back gets none it missed.
- The den checks its shape alone: a box of 1,024 bytes, and `to` a key ID or nothing. One that isn't, or past the limit, goes nowhere.
- The box is XChaCha20-Poly1305 under `HKDF-SHA-256(seal, salt = den_id, info = "dens-relay-v1")`, 32 bytes, its nonce first: the note's length in two bytes, the note, and zeros to the box's size, bound to `"dens-relay-box-v1"` ‖ den ID ‖ the member's ID, 8 bytes ‖ `from` ‖ `to`. What the note says is the installs' own. A client closes its connection on a `relay.signal` of the wrong shape, as the den breaking the protocol, and drops one that doesn't open.
- A den that passes boxes says `"relay": true` in its info; a client sends them only to one that does.
- `relay.signal` has a limit of its own, 30 in 10 seconds a member, for all their devices together (see [Rate limits](../protocol.md#rate-limits)). Additive: no version bump.
