# Den protocol: members, channels and events

Channels and groups, members and roles, leaving and bans, DMs, presence, and events with their cards and badges. The conventions, versions, errors, keys, objects and rate limits every part follows are in [protocol.md](../protocol.md).

## Channels and groups

```
channel = {"id", "group_id", "name", "description"?, "kind": "text" | "voice", "position", "staff_only", "bitrate"?}
group   = {"id", "name", "position"}
```

- `group_id` is null for a channel outside any group.
- A channel's `kind` is its ID's for good: a den gives no ID out twice, and a client that held or opened an ID as one kind, deleted since or not, takes it as another as the den breaking the protocol, since a DM taken for a text channel would have its next message go unsealed. An older den's `group_dm` channel keeps its kind too, though a client drops it unread (see [Group DMs on an older den](e2ee.md#group-dms-on-an-older-den)).
- Names are 1 to 32 characters, with the same stripping as display names. A text channel's `description` is the markdown subset, up to 4,000 characters. Voice channels have none.
- A voice channel's `bitrate` is what its call's Opus may send, in bits a second: 16000 to 128000, in steps of 8000, and 96000 unless set when it's created or changed. Text channels have none. A change reaches the call in progress with each member's next offer (see [Offers and answers](calls.md#offers-and-answers)).
- A den has at most 500 channels and 100 groups.
- `ready` carries the channels and groups the member can see. Changes arrive as `channel.created {channel}`, `channel.updated {channel}` and `channel.deleted {id}`, and the same for `group.*`.
- Management is `POST /api/channels`, `PATCH /api/channels/{id}` and `DELETE /api/channels/{id}`, and the same for `/api/groups`. Requests carry the object's fields, and a `PATCH` changes only the ones it names. `"group_id": ""` moves a channel out of its group. Moderators and the owner manage them.
- Deleting a channel deletes its messages. Deleting a group keeps its channels, which move to the end of the ungrouped list.

**Staff-only channels.** Members who can't see a channel get none of its events, and reorder lists leave it out, so the positions they see can have gaps. When a channel becomes staff-only or stops being so, those members get a fresh `ready`.

**Ordering.** Positions are dense (0, 1, 2, …) among the channels of one group, among ungrouped channels, and among groups.

- A create or edit that gives `position` inserts there, shifting the siblings at and after it down by one. Leaving it out appends to the end. Moving a channel to another group (`group_id` with or without `position`) closes the gap it leaves.
- The den renumbers in the same transaction, and sends the result as one event per affected list: `channels.reordered {group_id, channel_ids}` (`group_id` is null for ungrouped channels), or `groups.reordered {group_ids}`, in the same frame as the change itself.
- A reorder list also says which group each listed channel is in. Deleting a group sends `group.deleted`, then `channels.reordered` for the ungrouped list with the group's channels at its end, then `groups.reordered`.

## Members and roles

Moderators and the owner are **staff**. Staff manage channels, groups and invites, see staff-only channels, and delete the messages of, remove and ban members of a lower rank: moderators act on members, and the owner on anyone else. The owner alone changes the den and roles.

```
GET   /api/members/{id}                                     200 member, with "bio" and "badges"
PATCH /api/me              {"display_name"?, "bio"?, "name_style"?}  200 member, with "bio"
PUT   /api/me/status       {"status", "until"?}             200 member
PATCH /api/members/{id}    {"role": "member" | "moderator"} 200 member          (owner only)
PUT   /api/me/badges/{id}  {"hidden": bool}                 204
```

- Any member may fetch anyone who has been in the den. `bio` is the markdown subset, up to 300 characters and 1,200 bytes, and an empty one clears it. It comes only in these two responses, never in `ready` or events, which keeps both small, as do the badges the member earned, in the first alone (see [Cards and badges](#cards-and-badges)).
- `name_style` is how the member's name shows: `font`, one of `pacifico`, `fredoka`, `zilla-slab`, `space-mono`, `orbitron`, `pixelify-sans`, `bangers` and `cinzel`; `color`, `#rrggbb` in lowercase; and `color2`, the same, only with `color`, for a gradient from the name's start to its end. Each may be left out, and `{}` clears the style. Anything else is `400 invalid_field`. A member's record carries it wherever the member goes, in `ready` and events too, and leaves it out where there's none. A client takes a font it doesn't know, of lowercase letters, digits and hyphens, up to 32, as none, so a later den may add one; a color that isn't six hex digits makes the member malformed. A den that keeps styles says so in its info, `name_styles`; an older one ignores the field.
- `status` is a line of the member's, up to 80 characters, cleaned as a display name is but keeping each of Unicode's emoji whole, and shown as plain text; `until` is when it ends, in Unix milliseconds, after now, or left out for none. An empty `status` clears it, and takes no `until`. A member's record carries both wherever it goes, in `ready` and events too, as `status` and `status_until`, and the den leaves out one whose end has passed; a client stops showing it at its end by its own clock, and the den's hourly pass removes it, telling nobody. A client takes a record whose status doesn't clean as malformed. It's within the member's writes, and a den that keeps statuses says so in its info, `status`; an older one has no such route.
- A profile, status or role change reaches everyone as `member.updated {member}`; a client showing a bio fetches it again.
- A role change makes the member's sockets start over with a fresh `ready`, since what they can see changed, and their older resume points resync.
- `PUT /api/me/badges/{id}` hides a badge the member earned from their profile card, named by its event's ID, or with `"hidden": false` shows it again. It answers 204 whether or not that changed anything, and tells nobody. A badge the member didn't earn, or whose event they don't see, as a staff-only event's once they aren't staff, is `404 not_found` (see [Cards and badges](#cards-and-badges)).

### Leaving, removal and bans

```
POST   /api/me/leave                                                                204
POST   /api/members/{id}/remove   {"ban"?, "delete_messages"?, "revoke_invite"?}    204
GET    /api/bans                                                                     200 {"bans": [{"member", "banned_at", "banned_by"?}]}
DELETE /api/bans/{id}                                                                204
```

- A member who leaves or is removed keeps their record, with `left_at` set, so their messages keep a name. They lose their role, devices and sessions, recovery codes, read state, the invites they made, the pictures on their profile and the uploads they hadn't used. Their sockets close with 4003, and everyone gets `member.left {id, left_at}`.
- The owner can't leave or be removed.
- `ban` keeps the username out: joining with it is refused with `403 banned`. `delete_messages` also deletes the member's messages from that many seconds back, one of 0, 3600, 86400 or 604800, in DMs too, with a `message.deleted` for each to whoever can see it. `revoke_invite` revokes the invite they joined with, if it can still be used. Someone who already left can still be banned.
- Staff list and lift bans. Lifting one takes staff of the rank of whoever placed it, or higher, so only the owner lifts the owner's. Lifting one doesn't bring the member back.
- **Coming back.** A former member who isn't banned can join again with a new invite, as themselves: their username with the right password (the verifier) reclaims their record, history and first join time, with a new device and recovery codes. A wrong password gets `username_taken`, the same as a name in use, so an invite can't reveal who used to be here.
- The den stores no IP addresses, and a client makes a new key for each join, so a ban can't stop someone who joins under a new name with a valid invite. Invites are the real door.

### Staff's tools

```
GET /api/members/seen          200 {"seen": [{"member_id", "day"}]}
GET /api/audit?before={id}?    200 {"lines": [line], "next"?}
line = {"id", "at", "kind", "by"?, "member_id"?, "channel_id"?, "channel"?, "ref"?, "name"?,
        "changes"?: [{"field", "from"?, "to"?}], "ban"?, "delete_messages"?, "revoke_invite"?, "emoji"?, "owner"?}
```

- Both are staff's, and a member gets `403 forbidden`; a den says it has them in its info, `members_seen` and `audit_log`.
- `seen` lists each member in the den who has a device, once, by the latest time any of their devices signed in or renewed its session, as the start of that day in UTC, in milliseconds.
- The log answers newest first, at most 50 lines a page; `next` is the line to give as `before` for the page after, until the last. A `before` that isn't an ID is `400 invalid_field`. A line goes 90 days after its `at`, and isn't read after that.
- A line is one of staff's acts: `kind`, who acted (`by`, left out for the den's machine, as through its owner invite), on whom (`member_id`), the channel by ID and its name as it was, the ID of what else it was on (`ref`: an invite, a group, an emoji, a sound, an event or a message) and its name as it was, and what changed, each value as text, a description's with neither. A removal says whether it banned, the seconds of messages it deleted and whether it revoked the member's invite; a reactions' line names the Unicode `emoji`, or a den emoji by `name`; an event's change lists its name, place, channel and times, and its description, banner or badge with no values; a call's line holds the channel the member was in, where they were in one; and an owner invite's role change says who took over (`owner`). The kinds are `member.removed`, `ban.lifted`, `role.changed`, `invite.created`, `invite.revoked`, `channel.created`, `channel.updated`, `channel.deleted`, `group.created`, `group.updated`, `group.deleted`, `message.deleted`, `reactions.cleared`, `call.disconnected`, `call.muted`, `call.unmuted`, `call.moved`, `record.reset`, `emoji.added`, `emoji.renamed`, `emoji.deleted`, `sound.added`, `sound.renamed`, `sound.deleted`, `event.updated`, `event.canceled`, `event.ended` and `den.updated`. A later den may add kinds and fields, which a client shows as it can.
- A client checks each line: its IDs, a kind of up to 32 of a-z and `_` in dotted parts, a field of up to 32 of the same, names up to 100 characters and values up to 200, each as cleaning leaves an event's name, emoji whole, at most 32 changes, a window of 0 or more, and an emoji from the list. One that fails refuses the whole page.

### DMs

```
POST /api/dms              {"member_id"}   200 channel
POST /api/dms/{id}/close                   204
```

- A DM is a channel of kind `dm` between two members: `{"id", "kind": "dm", "members": [lower ID, higher ID], "group_id": null, "name": "", "position": 0, "staff_only": false}`. Each pair has one; `POST /api/dms` returns it, creating it on first use and sending `channel.created` to both.
- Only its two members see it, in `ready`, events, history and sends. Staff don't see or moderate DMs, and the den's owner can't read them either: their messages and files are end-to-end encrypted (see [Private DMs](e2ee.md#private-dms)).
- Messages, edits, deletes, replies and read state work as in channels, their text sealed. Every message counts toward the other member's unread `mention_count`.
- DMs aren't managed through `/api/channels`, and don't count toward the 500-channel limit.
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

## Events

A den's events: each a name, a description, an optional banner, a start, an end and a place, which members mark Interested, and which the den starts and ends at their times (see the design's [Events](../design/members.md#events)).

```
event        = {"id", "name", "description"?, "banner"?: image, "starts_at", "ends_at", "channel_id"? | "place"?, "made_by",
                "live"?: true, "interested", "mine"?: bool, "badge"?: {"title", "image": image}, "ended"?: true}
event_limits = {"member_events"}

POST   /api/events                {"name", "description"?, "banner"?, "starts_at", "ends_at", "channel_id"? | "place"?,
                                   "badge"?, "badge_title"?}                                               201 event
GET    /api/events/{id}                                                                                  200 event
PATCH  /api/events/{id}           {"name"?, "description"?, "banner"?, "starts_at"?, "ends_at"?, "channel_id"? | "place"?,
                                   "badge"?, "badge_title"?}                                               200 event
DELETE /api/events/{id}                                                                                  204   (to come)
POST   /api/events/{id}/end                                                                              204   (live)
PUT    /api/events/{id}/interest                                                                         200 {"event_id", "interested", "mine"}
DELETE /api/events/{id}/interest                                                                         200 {"event_id", "interested", "mine"}
```

- An event has a `name`, 1 to 100 characters with something visible, cleaned as a poll's choice is, each of Unicode's emoji kept whole; a `description` of up to 2,000 characters in the markdown subset, links cleaned as a message's are; `starts_at` and `ends_at`, Unix milliseconds; and one place: `channel_id`, a voice channel the member sees, or `place`, a line of text cleaned as its name is, up to 100 characters. Its start is at most a year ahead and no earlier than a minute ago, which counts as now, and it lasts 5 minutes to a week; a development instance's den takes lengths from a second, for the browser checks. `banner` names an upload of the member's own, waiting to be used, a still image or an animated GIF three times as wide as it's high, as a profile's banner is (see [Pictures on a profile](files.md#pictures-on-a-profile)), or `""` takes it off; the banner it replaces is deleted. Anything else, both places or neither, gets `400 invalid_field`. `made_by` is its maker; `live` says it started; `interested` is how many of those who see it marked it; and `mine`, which only `ready`, the answers and the events to the member's own sessions carry, is whether the member did.
- Staff make events. A member who isn't staff makes them while `event_limits.member_events`, 0 to 10, is above how many of theirs haven't ended; at 0, the default, events are staff's, and making one gets `403 forbidden`, as does one past the member's limit, or the den's 50 that haven't ended. The owner changes it with `PATCH /api/den`; a lower limit leaves the events already made. The den sends `event_limits` at 0 too, so a client tells events from an older den, which sends none.
- `PATCH` changes what it carries. Its maker changes an event, and staff those of the members they outrank, as calls and messages have it; anyone else gets `403 forbidden`. A live event's `starts_at` stays where it was, or `400 invalid_field`, and its `ends_at` is now or later, within its length's limits. `DELETE` cancels an event to come, with its banner; a live one gets `403 forbidden`, and is ended with `/end`, which ends it now. `/end` on an event to come gets `403 forbidden`, and on one that ended already `204` with nothing changed. An event the member doesn't see gets `404 not_found` from all of them, and one that ended from the rest.
- `/interest` marks the member Interested, or with `DELETE` takes it back, in an event to come or live they see, and answers with how many are and their mark. The same mark again changes nothing and tells nobody.
- An event in a staff-only channel is staff's alone: in `ready`, its events, its banner and its routes, and its count holds staff alone. It follows its channel, and moves with staff who move it: as either changes who sees it, members who aren't staff start over from a fresh `ready`, as a channel's visibility has them do. A deleted voice channel's events to come go, and its live one ends.
- The den's events, each to everyone who sees the event: `event.created {event}` and `event.updated {event}`, without `mine`; `event.canceled {"id"}`; `event.ended {"id"}`; and `event.interest {"event_id", "interested"}`, with `mine` to the member's own sessions as their mark changes. As an event starts, the den sends `event.started {event}` to the members interested in it, an audience fixed as it's sent, which a socket that resumes soon after still hears from the ring, and `event.updated {event}`, live, to the rest who see it; a client whose socket wasn't connected then, and resumes past the ring, or starts from `ready`, hears no `event.started`. As it ends, at its end or early, the den forgets who was interested. An event that passed whole while the den was stopped ends without starting.
- A member's events to come go as they leave the den, or are removed, with their banners, and their marks on the rest, whose counts everyone who sees them hears; their live events go on to their end.
- A client checks each event as it arrives, refusing a den whose events don't fit: its IDs, its name and place as cleaning leaves them, one place, its end after its start and within a week of it, its banner's size, and how many are interested; and at most 50 of them in `ready`. It keeps the events with the den's state, so they show while the den is away.
- Making, changing, canceling and ending an event, and marking Interested, count toward the member's other writes. An older client ignores the events and their routes; an older den sends no `event_limits`, and a client offers no events there.

### Cards and badges

```
badge         = {"event_id", "title", "image": image, "event", "starts_at", "earned_at", "hidden"?}   in GET /api/members/{id}'s "badges"
```

- `GET /api/events/{id}` answers with one of the den's events the member sees, to come, live or ended, for a link's card: with `mine`, and for one that ended `ended`, nobody interested and no mark. An event the member doesn't see, or that's gone, gets `404 not_found`. A client answers a card from the events it keeps where it can, so the den hears of an event to come only as `ready` and its events tell it, and asks for the rest.
- An ended event stays on the den, with its banner, out of `ready`, for its cards and its badge. Its place keeps who saw it: one whose voice channel was deleted has no place, and stays staff's alone if the channel was. A client takes an ended event only from this route.
- `badge` and `badge_title`, which staff alone send, attach a badge to an event in a voice channel or change it; anyone else sending them gets `403 forbidden`. `badge` names an upload of the staff member's own, waiting to be used, a still image or an animated GIF at most 128 pixels on each side and 256 KiB, or 512 KiB animated, as an emoji's image is (see [Emoji](messages.md#emoji)), or `""` takes the badge off; `badge_title` is 1 to 40 characters, cleaned as an event's name is. Attaching one takes both; changing one, either. An event with a badge stays in a voice channel: moving it to a `place` without taking the badge off in the same request, or attaching one to an event somewhere else, gets `400 invalid_field`. A picture replaced, or a badge taken off, goes, and so do the badges of events canceled, or that go with a member who leaves or a channel deleted. An ended event's badge stays as it was.
- A badge's picture is one of the den's named files, of kind `badge`, so it stays the den's whoever added it, and opens with `GET /api/files/{id}` to everyone who sees its event.
- A member earns an event's badge by a minute at a stretch in its call while it's live, their call held or not (see [Riding out a dropped connection](calls.md#riding-out-a-dropped-connection)), counting from the event's start for one in the call before it; once each, and nothing for a stretch that reaches its minute only after the event ends. A development instance's den takes three seconds, for the browser checks. The den tells nobody: the badge shows on the member's profile.
- `GET /api/members/{id}` carries the badges the member earned whose events the asker sees, latest first, up to 500, each with its event's ID and name and its start, and when they earned it. A client checks each: its IDs, its title and the event's name as cleaning leaves them, a picture of an emoji's size, its times, and each event once.
- A badge its member hid is left out of everyone's answer but the member's own, where it carries `"hidden": true`; a client takes `hidden` on the member's own profile alone, and a profile card shows only the badges without it. Hiding one keeps a card tidy and no more: the den keeps the award as it was.
