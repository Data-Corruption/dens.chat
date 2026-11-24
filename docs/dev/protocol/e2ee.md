# Den protocol: end-to-end encryption

Private DMs, identities, DM keys, the member's record, group DMs and notes to self. The conventions, versions, errors, keys, objects and rate limits every part follows are in [protocol.md](../protocol.md).

## Private DMs

A DM's messages and files are end-to-end encrypted: its members' clients seal them with the DM's key before they reach the den, and open them after. A DM's key is sealed to its members' identities, and signed by its maker's; the den keeps each member's identity, relays the keys and keeps each member's record, and never holds anything that opens them. The cryptography is in `internal/denproto/e2e.go` and `identity.go`, which both sides share.

In this section, `len(x)` is x's length as an 8-byte big-endian number, and IDs join as 8-byte big-endian numbers too.

### Identities

```
identity = {"kem": "<1,216 bytes>", "sig": "<32 bytes>"}

PUT /api/me/identity                    identity   200 member
GET /api/identities?members=<id>,<id>…             200 {"identities": [{"member_id", "kem", "sig"}, …]}
```

- A member's client publishes their identity (see [Identity and keys](../protocol.md#identity-and-keys)) as it signs in, where the member's record has no `identity` or another; a client without the seal publishes nothing. The den checks the keys are of their kinds. A change reaches everyone as `member.updated`, and a client that kept the list of members gets it with its next `ready`.
- Any member fetches identities, up to 50 at once, as a client needs them; members without one are left out.
- Leaving the den takes the member's identity, and starting over does too (see [Starting over](#starting-over)).
- A client keeps the identity the den gives for each member the first time it needs it, and checks each it meets after against what it kept; trusting the den is the client's, as is noticing a change.

### DM keys

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
- The den takes a key only when `members` are the DM's two, each with the identity they publish now, the signature checks against the maker's, and there's a copy for each. A DM has one live key: a legacy key both members checked stays (see [Legacy DM keys](#legacy-dm-keys)), and so does a signed key naming the identities its members publish now, so a second gets `409 key_exists`, and its client takes the live one; a signed key that names an identity no longer published retires as `restarted`, by the maker of the key that replaced it.
- Each member sees their own copy alone: in `ready`'s `signed_keys`, and as `dm.signed_key {signed_key}` as a key is made or retired. A retired key takes no new messages, and stays so what it sealed still opens.
- Signed keys never come among `dm_keys`, `dm.key` or `GET /api/dms/{id}/keys`, whose copies a client older than v1.9.0 checks to be its own 72 bytes and whose malformed events close its connection.

### Legacy DM keys

```
dm_key = {"id", "channel_id", "started_by", "started_at", "stage": "offered" | "answered" | "revealed",
          "checks"?: [{"member_id", "at"}], "retired_at"?, "retired_by"?, "retired"?: "started_over" | "restarted",
          "sealed"?, "exchange"?: {"offer", "answer"?, "reveal"?, "state"?}}

GET  /api/dms/{id}/keys                                    200 {"keys": [dm_key, …]}
POST /api/dms/{id}/keys   {"offer", "state", "restart"?}   400 invalid_field
```

A legacy key is one Dens older than v1.9.0 made by an exchange of one-time keys between a DM's two members' clients, the one [Approving new devices](auth.md#approving-new-devices) runs, with `ctx = "dens-exchange-dm-v1" ‖ den_id ‖ channel ‖ starter ‖ other`: the starter's offer, the other's answer and the starter's reveal, each side's `state` waiting on the den sealed with its member's seal, bound to `"dens-exchange-state-v1" ‖ den_id ‖ channel ‖ member ‖ commit ‖ side`, where `commit` is the offer's commitment and `side` one byte, 1 for the starter. Its key, `HKDF-SHA-256(secret, salt = transcript, info = "dens-exchange-key-v1")`, opened the DM once each member typed the other's half of its check code, and stored their copy:

- `sealed` is the member's copy of the key: XChaCha20-Poly1305 under `HKDF-SHA-256(seal, salt = den_id, info = "dens-dm-seal-v1")`, bound to `"dens-dm-key-v1" ‖ den_id ‖ channel ‖ key_id ‖ member`, as nonce ‖ ciphertext, 72 bytes. `checks` records who stored theirs.
- A key both members checked stays live, both sending and reading under it, until one of them starts over.
- The den starts no exchange: `POST /api/dms/{id}/keys` gets `400 invalid_field`. An exchange still running as the den updated to v1.9.0 or later, or a key one member alone checked, retired as its migration ran, as `restarted` by whoever started it.
- `ready` carries `dm_keys` without exchanges, and each change reaches the DM's two members as `dm.key {dm_key}`, each with their own copy.

### The member's record

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
PATCH /api/messages/{id}           {"revision", "sealed", "key_id", "editors"?, "unedited"?, "attachments"?}  200 message
```

- `sealed` is `{"text", "files"?, "forwarded"?}` as JSON, sealed with XChaCha20-Poly1305 under the DM key and bound to `"dens-dm-message-v1" ‖ den_id ‖ channel ‖ author ‖ len(nonce) ‖ nonce ‖ key_id ‖ revision`, as nonce ‖ ciphertext, at most 64 KiB. `text` stays empty. The sealed text follows a message's rules, and lists at most 10 files.
- `key_id` must name the DM's live key, and one its sender holds a copy of: a signed key's members do, and a legacy key's once they checked it; otherwise `400 invalid_field`.
- A DM's reactions are each member's set, which their client seals as it seals a message (see [Reactions](messages.md#reactions)).
- An edit seals the message again at the next revision, under the DM's live key, which may be newer than the one it was sent with. The den can't compare texts, so `"unedited": true` marks an edit that only ticks a task or changes the editors, and doesn't mark the message edited. A DM's tasks tick this way: `POST /api/messages/{id}/tasks/{n}` refuses a DM's message.
- `forwarded: true` marks a forward, which no client edits or ticks, and a client takes the mark from the sealed text alone; an older client reads past it, and its edit drops it.
- The rest works as in channels: ordering, history, revisions and edit conflicts, deletes, read state, and unread counts, where every message counts for the other member.
- Binding the author, nonce, key and revision keeps the den from moving a sealed text to another message, or passing an old revision off as a new one. It can still hold a message back, or show one twice.

### DM files

```
POST /api/uploads/sealed   (body: the sealed file)   201 {"id", "name": "", "type": "application/octet-stream", "size", "sealed": true}
```

- A DM's file goes up sealed by the sender's client, which first takes an image's metadata out and makes its preview, as the den does for a channel's. Each file gets a random key of its own. The file and its preview are sealed with it as streams, in the format files at rest use, bound to `"dens-dm-file-v1" ‖ den_id ‖ channel ‖ "file"` or `"thumb"`, and go up as two blobs.
- The sender's client strips a DM's video and audio, and makes a video's preview, as a den does for a channel's. For a video the module can't decode, the sender's page draws the preview, and the client seals and uploads it as it does its own.
- The message's sealed text lists each file: `{"id", "key", "name", "type", "size", "width"?, "height"?, "animated"?, "duration_ms"?, "thumb"?: {"id", "width", "height"}, "spoiler"?}`. Its `attachments` name every blob, each file's and its preview's, at most 20. A file sent as a spoiler says so here alone, and a file swapped into its place is one too.
- A DM's edit can change its files as a channel's does: the sealed text lists the files afterward, and `attachments` names every blob, as a send's does. Blobs it leaves out are deleted.
- A sealed file answers byte ranges like any other, and a client opens the chunks a range covers with the file's key, so a DM's video seeks the same way.
- A DM's message takes only sealed uploads, and a channel's none. The den counts a sealed blob against the upload limits, and serves it like any file, with no preview of its own.

### Group DMs

```
channel = {"id", "kind": "group_dm", "members": [member id, …], "changes": n, "sealed_name"?: {"kid", "sealed", "sig"?}, "group_id": null, "name": "", "position": 0, "staff_only": false}
change  = {"id", "made_at", "n", "prev", "kind": "made" | "added" | "removed" | "left" | "departed" | "named", "by"?, "subjects"?: [member id, …],
           "after": [member id, …], "name"?: {"kid", "sealed"}, "signer"?: {"kem_hash", "sig"}, "signature"?}

POST /api/group-dms                                                       201 {"id"}
GET  /api/group-dms/{id}/changes?after=<n>                                200 {"changes": [change, …], "more"}
POST /api/group-dms/{id}/changes  {"change", "key"?: signed key request}  200 change
```

A group DM is a DM of three to ten members, at version 2 of the protocol and up. Its channel lists its members as the den holds them, in the order they joined, the number of its latest change, and its name sealed; a client knows its members from its changes alone.

- **A change** names its number `n`, from 1, `prev`, the hash of the change before it (32 zero bytes for the first), its `kind`, its maker `by`, the members it took in or out `subjects`, and the group's members `after` it, in the order they joined. Its maker's identity signs `"dens-group-change-v1" ‖ den_id ‖ channel ‖ n ‖ prev ‖ kind ‖ by ‖ len(subjects) ‖ subject… ‖ len(after) ‖ member… ‖ name`, where `kind` is one byte, 1 to 6 in the order above, and `name` is 1 ‖ kid ‖ SHA-256(sealed), or 0 with none; `signer` is the maker's identity as a signed key's `maker` carries it, which hashes to their identity. A change's hash is `SHA-256("dens-group-change-hash-v1" ‖ what it signs)`.
- **The rules,** which the den checks, as every member's client does: `made` is the first change, its maker first in `after`, the others its `subjects`, three to ten in all; `added`, by a member, takes in `subjects`, none of them members, at the end of `after`, ten at most; `removed`, by the first of the members in the order they joined, takes out one other; `left` takes out its maker; `departed`, the den's, unsigned, takes out one who left the den; and `named`, by a member, carries the new name, sealed under the live key. Each follows the one before, by `n` and `prev`, and `after` is what it makes of the members before it. The den takes a change only from its maker, signed by the identity they publish now; one made against an earlier change gets `409 edit_conflict`, and the client makes it again on the group as it is.
- **Making one.** `POST /api/group-dms` reserves a group for the session's member, which nobody sees until its first change, `made`, makes it: every statement binds the den's ID for the group. A member who reserved one and didn't make it gets the same one again; one reserved an hour ago goes as the next is reserved, or as its maker leaves the den. A member is in at most 100 group DMs; making one, and each change, count toward the member's other writes.
- **Keys.** A change that makes a membership, `made`, `added` or `removed`, carries its key, as `POST /api/dms/{id}/signed-keys` makes one, for the members `after` it, each with the identity they publish now; `left` retires the live key, as `departed` does, and the first member's client to send makes the next, through `POST /api/dms/{id}/signed-keys`, which takes a group's key for its members as they are, retiring a live key that names the members or identities as they were. A group's key may carry the group's name sealed under it, `name`, its `kid` the key's, with `sig`, its maker's identity's signature over `"dens-group-key-name-v1" ‖ den_id ‖ channel ‖ kid ‖ SHA-256(sealed)`: the den keeps it as the group's, with the signature, which it refuses (`400 invalid_field`) where it doesn't check against the maker's published identity. One with no `sig`, from an older client, it keeps unsigned. A change's name carries no `sig`, which its own signature covers, and the den keeps a naming's with none. A member gets a group's keys they hold a copy of, in `ready` and as `dm.signed_key`, while they're in it.
- **The name** is `{"len(name)" as 2 bytes ‖ name ‖ zeros to 160 bytes}` sealed with XChaCha20-Poly1305 under the group key whose `kid` it names, bound to `"dens-group-name-v1" ‖ den_id ‖ channel ‖ kid`, as nonce ‖ ciphertext, 200 bytes. An empty name is none. A client opens it with the key whose `kid` it names, and cleans it as a display name. It shows the name of the group's latest change it checked that carries one, `made` or `named`; a client whose member joined since the last naming shows the channel's `sealed_name` only under a key it holds, where `sig` checks against the key's `maker`.
- **Members' changes.** A group changes at most 1,000 times: the den refuses the next change with `400 invalid_field`, and a client refuses a group that claims more. `GET /api/group-dms/{id}/changes` returns a page of the group's changes after `after`, at most 200, from the change that took the member in, which names the group's members then. A client takes a page from before `after` + 1 as the den breaking the protocol, and a group listed again from a change no later than the latest it checked only where its changes from there reach that one, the same. `group.change {"channel_id", "change"}` goes to the members after each change; a member taken in gets `channel.created`, one taken out `channel.deleted`, and the rest `channel.updated`. A member removed, gone or leaving loses the group, its history, keys and changes.
- **Messages** are sealed as a DM's, under the group's live key, with `"dens-group-message-v1"` in its binding's place, and the sealed text starts with its signer's ID as 8 bytes, the hash of their identity's X-Wing key, their Ed25519 key, and their signature over `"dens-group-signed-v1" ‖ binding ‖ signer ‖ SHA-256(text)`; the signer is the author at the first revision, and whoever edited or ticked it after. The author names who else may: the sealed text's `grant`, `{"editors": [member id, …], "revision", "signer": {"kem_hash", "sig"}, "signature"}`, is their identity's signature over `"dens-group-editors-v1" ‖ den_id ‖ channel ‖ author ‖ len(nonce) ‖ nonce ‖ revision ‖ len(editors) ‖ editor…`, at most nine editors, the author not among them, at the revision the author named them, which each later revision copies until the author gives another; a forward carries none. A client opens a revision signed by another than its author only where the grant names them and checks against an identity it kept for the author, at a revision no later than the message's; the den's `editors` and `edited_by` say nothing of a group's. One with no grant, as an older client seals every edit, opens on its author's devices alone, which may keep the revision their page showed, sealing it again as theirs with no grant and `"editors": []`. A member's set of reactions is sealed as a DM's, with `"dens-group-reactions-v1"` in its binding's place, its 1,024 bytes the same header, signed over `"dens-group-reacted-v1" ‖ binding ‖ member ‖ SHA-256(set)`, then the set padded to the rest; a message carries at most 20 members' sets. A client opens one only where its signature checks against the identity its key names for the signer.
- **The rest** works as in a DM: history from the member's joining on, sends, edits, deletes by their author alone, read state, closing, and unread counts, where every message counts for each other member. No polls, and no @everyone.

### Starting over

```
POST /api/me/seal   {"verifier", "seal_check"}   200 {"signed_out": 1}
```

- A member who has lost their seal and every device that held it, or wants to shut out a stolen device that holds it, starts over with a new seal. It takes the password, so a stolen session can't.
- The live keys of their DMs retire (`"retired": "started_over"`), and their copies of every DM key, and their exchange states, go: nothing opens them any more. What those keys sealed stays unreadable to them, while the other members keep their copies. Their identity and record go too, so others see their identity change as their client publishes the new seal's; each DM gets a new key, sealed to the new identity, with its next message.
- Every other device of theirs is signed out with 4003 `started over`, since it holds the old seal, and their sign-ins waiting for approval are cancelled.
- A former member who joins again with another seal than the one they left with starts over the same way.

## Notes to self

```
channel = {"id", "kind": "notes", "members": [member id], "group_id": null, "name": "", "position": 0, "staff_only": false}

POST /api/notes   200 channel
```

A member's notes are a channel of their own in the den, which only they see, at version 3 of the protocol and up. Their clients seal its messages as a DM's, under a key the den never holds.

- `POST /api/notes` answers with the member's notes, making them the first time, which sends `channel.created` to the member; after that it answers with the same channel. A member has one in each den. It counts toward the member's other writes.
- **The key** is `HKDF-SHA-256(seal, salt = den_id, info = "dens-notes-v1")`, 32 bytes, which each client holding the member's seal makes as it needs it and never sends.
- **Messages** go as a DM's (see [DM messages](#dm-messages)), sealed under the notes' key, with `key_id` the notes' channel ID, which the den takes in place of a DM key's and requires, or answers `400 invalid_field`. Their files go as a DM's (see [DM files](#dm-files)), each sealed under a key of its own that the sealed text carries.
- Replies, edits, deletes, ticks as edits, and read state work as in a DM, and sending a note reads the notes up to it; nothing in them counts toward an unread count. The den refuses reactions, polls, other editors, disappearing times and closing (`400 invalid_field`), and a spoiler outside the seal, as in a DM.
- The den's retention deletes notes as any message, and their files count toward the member's upload limits.
- A member who leaves the den, or is removed, takes their notes with them: the den deletes the channel, with its messages and files.
- A member who starts over (see [Starting over](#starting-over)) has a new seal, and so a new key: the notes from before no longer open, and a client shows them as lost, for the member to delete.
- At version 1 or 2 the den sends nothing of notes, in `ready`, events, history or the change log, and `POST /api/notes` answers `404 not_found`, since an older client closes its connection on a channel of a kind it doesn't know.
