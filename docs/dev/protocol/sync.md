# Den protocol: the socket and sync

The WebSocket, what changed while a client was away, and how messages leave: retention and disappearing messages. The conventions, versions, errors, keys, objects and rate limits every part follows are in [protocol.md](../protocol.md).

## The WebSocket

```
GET /api/ws?resume=<epoch>.<seq>&members=<sync epoch>.<version>
Authorization: Bearer <token>
Dens-Protocol: 3
```

`resume` is left out on a first connection, and `members` by a client that kept no list of members (see [What changed while away](#what-changed-while-away)). Frames are JSON text, each an array of one or more events, of at most 1 MiB from the den and 64 KiB from the client:

```json
[{"t": "message.created", "seq": 5021, "d": {…}}, {"t": "read_state.updated", "seq": 5022, "d": {…}}]
```

The den keeps each frame under 1 MiB: events queued behind a large one go in the next frame.

Client and den negotiate permessage-deflate without context takeover: either side may compress a frame of more than 512 bytes, each on its own, as `ready` and replays are. The limits apply to a frame as it inflates.

- **Durable events** record a change a member can see, and carry `seq`. `seq` increases strictly within a den epoch. It has holes, because a member only receives the events they're allowed to see.
- **Ephemeral events** (presence, typing, `auth.renewed`) carry no `seq`, and are never replayed.
- Events apply in order: across frames, and within a frame's array.

### Starting and resuming

The den's first frame is one of:

- `ready`: a full snapshot. It says the client's state is gone or never existed, and replaces everything the client held for this den. `d` holds `epoch`, `seq` (the resume point), `den`, `me`, `members`, `channels` (the member's DMs among them), `groups`, `read_states`, `online`, `seal_check`, `dm_keys` (the keys of the member's DMs, without their exchanges), `device_requests` (sign-ins waiting for their approval), `calls` (see [Voice](calls.md#voice)), `sync` (see [What changed while away](#what-changed-while-away)), `sounds`, the den's soundboard's, by name (see [The soundboard](calls.md#the-soundboard)), `emoji`, the den's emoji, by name (see [Emoji](messages.md#emoji)), `signed_keys`, the keys of the member's DMs sealed to identities, each with their own copy, which a den older than v1.9.0 doesn't send, as a client tells it keeps no identities (see [Private DMs](e2ee.md#private-dms)), and `events`, the den's events to come and live that the member sees, each with their own mark (see [Events](members.md#events)).
- `resumed`, followed by every durable event the member can see after the given `seq`, then the live stream.

The den resumes when `epoch` matches its current process and every event after `seq` is still in its ring. The ring holds the events of the last 10 minutes, up to 50,000 of them and 64 MiB. Otherwise it sends `ready`.

The den may also send `ready` mid-stream when what the member can see changes, such as a role change or a channel becoming staff-only. The client handles it the same way: drop and replace. A member whose view changed that way while they were offline gets `ready` on reconnect too, rather than a resume that skips the change.

The client remembers `epoch` and the `seq` of the last durable event it applied, and sends them on every reconnect. After a `ready`, it keeps nothing from before but what it cached, which it checks against what changed while it was away, and refetches only what's on screen.

### What changed while away

A client that caches what a den sends asks, after each `ready`, what changed in it meanwhile. The den keeps a log of its changes, by ID only, for 30 days.

```
sync = {"epoch": "<16 bytes>", "version": 1300, "floor": 1000, "expired"?: "<message id>", "members_since"?: 1200}

GET /api/changes?since=<version>&upto=<message id>
200 {"version": 1300, "messages": [message, …], "deleted": [{"id", "channel_id"}, …], "more": false}
```

- `version` counts the den's changes, in the order they happened: each message edited (its text, files or editors, or a tick), deleted, by a member or as its time came, or whose reactions or poll's votes changed, or whose poll ended, its revision as it was; and each member whose record changed (joining, leaving or removed, a name, a picture or a role). `epoch` names the den's run of versions; a den restored from a backup starts a new one, since the backup can give a new message an ID a client holds for another. `floor` is the oldest version the den still has the changes since, which moves up as it forgets changes older than 30 days. `expired` is the last message retention deleted, once it has deleted any (see [Retention](#retention)).
- A client keeps what it cached from an earlier `ready` only when its version is from the same `epoch`, no older than `floor` and no newer than `version`; otherwise it drops it. It takes the `version` of a `ready` that finds it holding nothing.
- **Members since.** A client that kept the list of members from a `ready` names the epoch and version of that `ready` on the socket's upgrade, as `members`. When the den can still say what changed since, `ready` carries only the members whose records changed after that version, and says so with `sync.members_since`, the version they changed after; the client changes its list by them and takes the new `version` for its list. Otherwise `ready` carries everyone, without `members_since`. A `ready` the den sends mid-stream goes by the same cursor, which the socket's own events kept current.
- `GET /api/changes` returns the messages edited or deleted after `since`, in channels the member can see now, and with IDs up to `upto` when it's given: the newest message the client holds. An edited message comes as history has it now, and a deleted one as its ID and channel. A change in a channel that's gone is left out, since the channel is missing from `ready` and the client drops it whole.
- New messages aren't changes: a client fetches them as history. Retention and deleted channels aren't in the log either: `expired` and the channel list say what they took.
- A page holds at most 1,000 changes, within about 900 KiB. `more` says the log goes on past its `version`, and the client asks again from there; the last page's `version` is the client's from then on.
- `since` older than `floor`, or newer than the den's version, gets `410 changes_gone`, and the client drops what it holds for the den.
- Responses are gzip-compressed when the client sends `Accept-Encoding: gzip`.
- A client connected throughout asks once a day, which keeps its version above the floor. Changes it already applied as events apply again harmlessly.

### Client frames

The client sends the same array shape: `auth.renew`, `focus` and `typing` (see [Presence and typing](members.md#presence-and-typing)), the `voice.*` frames (see [Voice](calls.md#voice)), and `relay.signal` (see [Relay](auth.md#relay)).

```json
[{"t": "auth.renew", "d": {"nonce": "…", "proof": "…"}}]
```

To renew, the client gets a nonce from `/api/auth/challenge` and sends `auth.renew` within the token's last 10 minutes. The den extends the same token by one hour, marks the device seen then, and answers with an ephemeral `auth.renewed {expires_at}`. If a token expires anyway, the den closes the socket with 4001.

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
| 1007 | A frame from the client that doesn't decode | Reconnect with backoff and resume |
| 1011 | The den couldn't make the snapshot for `ready` | Reconnect with backoff and resume |
| 1012 | Den restarting | Reconnect after a random 0.5 to 5 seconds, and resume (it will get `ready`) |
| 4001 | Session expired | Log in again, then reconnect and resume |
| 4003 | Key revoked, or the member left, was removed or was banned; the reason says which: `key revoked`, `password changed` (a new password signed the device out), `started over` (a new DM seal did), `left`, `removed` or `banned` | Stop. Tell the member, and don't reconnect. After `key revoked`, `password changed` or `started over`, offer to sign in again |
| 4008 | Too slow | Reconnect and resume |
| 4029 | Rate limited. The den sends none: it refuses a socket past its limits with `429 rate_limited` before the upgrade | Reconnect after a random 10 to 30 seconds |

Any other failure reconnects with exponential backoff and full jitter, from 0.5 seconds up to 30, resetting after a minute connected. A stopping den closes its listener before sending 1012, so no client reconnects to the old process.

## Retention

```
GET /api/den/retention?days=N   200 {"messages", "bytes"}   (owner only)
messages.expired {"through": "<message id>"}
```

- With a retention period, the den deletes the messages sent more than that many days ago, in channels and DMs, with their files, as it deletes any message, within the hour after they pass it. Edits don't change a message's age, and neither do replies to it. A message that disappears goes at its own time, or with retention's pass where that comes first.
- It deletes the oldest first, in the order they were sent, so after a pass every message up to the last one it deleted is gone, in every channel. It sends `messages.expired` naming that message, and no `message.deleted` for each. Clients drop every message with an ID up to `through`, the quotes of them in replies, and the files on them; a channel whose last message is among them holds none, so nothing in it is unread. `ready` names the last message retention deleted as `sync.expired`, for a client that was away.
- The owner sets the period with `PATCH /api/den`: 1 to 3,650 days, or 0 to turn it off. A change starts a pass at once. `GET /api/den/retention` says what a period of `days` would delete now, for the settings to show before a change: how many messages, and the bytes their files take.

## Disappearing messages

```
channel = {…, "expires_after"?: <seconds>, "expires_set"?: {"by", "at"}}   a DM's
message = {…, "expires_at"?}

PUT /api/dms/{id}/expiry   {"expires_after"}   200 channel
```

- Either member of a DM sets how long its messages stay after they're sent: `expires_after` is 3600, 86400, 604800 or 2419200 seconds, an hour, a day, a week or four weeks, or 0, Off, which a new one starts at and its channel then leaves out. A development instance's den also takes any whole number of seconds up to 2419200, for the browser checks, and a client takes any such time from a den. `expires_set` is who last set it and when, once anyone has, Off included, for the line that says so.
- Another time gets `400 invalid_field`, as does a channel that isn't a DM; a DM whose other member left gets `403 forbidden`. The same time again changes nothing and tells nobody. A change reaches the DM's two members as `channel.updated {channel}`, and `ready` carries it with the channel. Setting a time counts toward the member's other writes.
- A message sent while its channel has a time carries `expires_at`: its `created_at` plus the time, by the den's clock. An edit leaves it. A client refuses a message whose `expires_at` isn't after its `created_at`, or that isn't sealed.
- The den deletes each as `expires_at` comes, as its author's delete would: with its files and reactions, `message.deleted {id, channel_id, files?}` to whoever can see it, and the change log; and as it starts, what came due while it was stopped. Each member it counted for hears `read_state.updated`, as for any delete (see [Sending, editing, deleting](messages.md#sending-editing-deleting)).
- A client drops what it holds past `expires_at`, by its own clock, whether it can reach the den or not, and leaves it out of history. An older client, which knows no `expires_at`, drops it as the den deletes it. A client offers the setting only on a den whose info says `disappearing`.
