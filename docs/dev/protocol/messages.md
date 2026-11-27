# Den protocol: messages

Messages and their links, history, sending and editing, read state, shared messages, emoji, reactions and polls. The conventions, versions, errors, keys, objects and rate limits every part follows are in [protocol.md](../protocol.md).

## Messages

```
message        = {"id", "channel_id", "author_id", "created_at", "revision", "edited_at"?, "edited_by"?,
                  "text", "reply_to"?, "reply"?, "nonce"?, "attachments"?, "editors"?,
                  "sealed"?, "key_id"?, "reactions"?, "dm_reactions"?, "poll"?, "everyone"?, "expires_at"?,
                  "forwarded"?, "forward_source"?}
forward_source = {"den_id", "channel_id", "message_id"}
```

A DM's message carries its text and files `sealed` instead, with the DM key `key_id` names; its `text` is empty, and it lists no `attachments` (see [Private DMs](e2ee.md#private-dms)). A channel's message carries its `reactions`, and a DM's its members' sets of them sealed, as `dm_reactions` (see [Reactions](#reactions)). A channel's message may carry a `poll`, as the member it's sent to may see it, and `everyone: true` where its @everyone is staff's (see [Polls](#polls)). A DM's message sent under a time carries when it goes, `expires_at` (see [Disappearing messages](sync.md#disappearing-messages)). A channel's message its author forwarded carries `forwarded: true` and, when forwarded from a text channel, `forward_source` naming its 32-byte `den_id`, `channel_id` and `message_id` (no URL or hostname); a sealed place's mark and source go inside the seal (see [DM messages](e2ee.md#dm-messages)).

`text` is at most 4,000 characters and 16 KiB, and not only spaces, unless the message has files, when it may be empty. It is the markdown subset in the design's [Text and replies](../design/messages.md#text-and-replies), which the client parses and never treats as HTML. The den stores it as sent, except for its links, which it rewrites before checking the limits (see [Links](#links)).

`revision` starts at 1 and increases with every edit. `edited_by` is the member who made the latest edit, which differs from the author on shared messages.

`reply_to` names the message this one replies to, which is in the same channel. `reply` previews it as `{"author_id", "text"}`: its author and the start of its text, from the first character that isn't a space, at most 100 characters. The text is empty when the original has only files. A client can show the quote without loading a message that may be thousands back. The den builds the preview from the original as it is whenever it sends the reply, and leaves it out once the original is deleted; clients update the previews they hold from the original's `message.updated` and `message.deleted`. In a DM, the den can't read the original, so `reply` carries it whole, for the client to open and quote: `{"author_id", "text": "", "sealed", "key_id", "nonce", "revision"}`.

**Mentions.** `@` then 2 to 32 letters, digits or `_`, matching a username without regard to case. It counts at the start of a line, or after a character that is neither one of those nor `@`, when none of them follows. It doesn't count inside code (a ``` block, or a span from one backtick to the next with something between) or inside a link (`http://` or `https://` at the start or after a character that can't be part of a name, running up to a space or one of `<`, `>`, `"`, `'` and the backtick). The den counts by this rule (`denproto.Mentions`), and the page highlights by the same one; both are tested against the cases in `internal/denproto/testdata/mentions.json`. A mention adds to the unread mention count of the member it names, if they can see the channel and aren't the author. An edit recounts. Staff's `@everyone`, the moderators' and the owner's, mentions every member who can see the channel, and marks the message `everyone`; anyone else's mentions a member named `everyone`, who may have joined before v1.8.0 reserved the name (see [Polls](#polls)).

### Links

The den rewrites the links in a message's text when it's sent or edited, and in a channel's description or a member's bio when it's set, then checks the text's limits. A DM's text is rewritten the same way by its sender's client, before it's sealed. Both follow one rule (`denproto.CleanLinks`), tested against the cases in `internal/denproto/testdata/links.json`. The page's renderer is tested against the same cases, to check that it ends each link where the rule does.

- A link is one the page renders as a link: `http://` or `https://`, at the start or after a character that can't be part of a name, running up to a space or one of `<`, `>`, `"`, `'` and the backtick, less any `.`, `,`, `;`, `:`, `!`, `?` and `]` at its end, and any `)` there that closes no `(` in the link. It isn't in code or part of a mention, and inside bold, italics, strikethrough or a spoiler it ends where they do. The page's renderer (`markdown.jsx`) decides each of these, and `denproto` follows it step by step.
- Hosts match exactly and in lowercase, as a URL parser reads them, so a port or a trailing dot makes another host. A link with a user name or password before its host stays as it is, and so does one that doesn't parse.
- Links to these sites, in these forms, are written in one form, which keeps only what identifies the post or video:

| Site | Forms | Kept | Written as |
| --- | --- | --- | --- |
| YouTube | `/watch?v=ID`, `/shorts/ID`, `/live/ID` and `/embed/ID` on `youtube.com`, `www.youtube.com` and `m.youtube.com`; `youtu.be/ID`; `/embed/ID` on `youtube-nocookie.com` and `www.youtube-nocookie.com` | The video ID, 11 of `A-Z`, `a-z`, `0-9`, `-` and `_`. The start time `t`, or `start` on an embed: digits, optionally followed by `s`, or a time like `1h2m3s` | `https://www.youtube.com/watch?v=ID`, then `&t=` and the start time if there is one; a Short, from `/shorts/ID`, as `https://www.youtube.com/shorts/ID`, with no start time |
| Reddit | `/comments/POST` on `reddit.com` and its `www.`, `old.`, `new.`, `np.` and `m.` hosts, also after `/r/NAME`, `/u/NAME` or `/user/NAME`, optionally followed by a title, which may be empty, and then a comment ID; `/gallery/POST` on the same hosts; `redd.it/POST`. Each may end in a `/` | The post ID and the comment ID, each 1 to 16 of `a-z` and `0-9` | `https://www.reddit.com/comments/POST`, or `https://www.reddit.com/comments/POST/comment/COMMENT` |
| X | `/NAME/status/ID`, `/i/status/ID` and `/i/web/status/ID`, optionally followed by more of a path, on `x.com`, `twitter.com` and their `www.` and `mobile.` hosts | The status ID, 1 to 20 digits | `https://x.com/i/status/ID` |
| Amazon | `/dp/ID`, also after a product name (`/NAME/dp/ID`), `/gp/product/ID` and `/gp/aw/d/ID`, optionally followed by more of a path, on Amazon's stores and their `www.` and `smile.` hosts: `amazon.` followed by `com`, `ca`, `com.mx`, `com.br`, `co.uk`, `ie`, `de`, `fr`, `it`, `es`, `nl`, `se`, `pl`, `com.be`, `com.tr`, `ae`, `sa`, `eg`, `in`, `co.jp`, `sg`, `com.au`, `co.za` or `cn` | The product ID (ASIN), 10 of `A-Z` and `0-9` | `https://www.STORE/dp/ID`, where `STORE` is the store the link was on |

- Every other link keeps its form, and loses its tracking parameters:
  - On any site: `fbclid`, `gclid`, `dclid`, `gbraid`, `wbraid`, `msclkid`, `twclid`, `ttclid`, `yclid`, `igshid`, `igsh`, `mc_cid`, `mc_eid`, `_hsenc`, `_hsmi`, `mkt_tok`, and any whose name starts with `utm_`.
  - On `youtube.com`, `youtu.be`, `youtube-nocookie.com` and their subdomains, such as `music.youtube.com`: `si`, `pp` and `feature`.
  - On `x.com`, `twitter.com` and their subdomains: `s`, `t`, `ref_src` and `ref_url`.
  - On `reddit.com`, its subdomains and `redd.it`: `share_id`.
  - On `steampowered.com`, `steamcommunity.com` and their subdomains: `snr`.
  - On Amazon's stores and their `www.` and `smile.` hosts: `ref`, `ref_`, `qid`, `sr`, `crid`, `sprefix`, `keywords`, `content-id`, `_encoding`, `tag`, `linkCode`, `linkId`, `ascsubtag`, `creativeASIN`, `creative`, `camp`, and any whose name starts with `pd_rd_` or `pf_rd_`. Path segments starting with `ref=` come out of the link too.

  Everything else in the link stays as it was, the remaining parameters' order and spelling included. A link left with no parameters loses its `?` too, and empty ones between `&`s go.
- A link stays as it is if the page would find another link once it's rewritten, as when what's left ends in punctuation the page leaves out of a link: `https://example.com/a.?utm_source=x` keeps its tracking rather than become a link to `https://example.com/a`.
- A Reddit share link (`/r/NAME/s/CODE`) loses only its tracking parameters. Its code is Reddit's own record of who shared the link, and only Reddit can turn it into a post. Amazon's short links, on `amzn.to` and `a.co`, are left the same way.
- Rewriting a text a second time changes nothing.
- The page shows a link written as `https://www.reddit.com/comments/…` on `old.reddit.com` in a browser that prefers it.

### Links to messages

```
https://den.example.com/#m/<den ID>/41/90210    the den's address, then #m/<den ID>/<channel>/<message>
GET /                                           200 text/plain: a line saying the address is a den's
```

- A link to a message is the den's address, as its invites and challenge sign it, followed by `/#m/`, `den_id` in base64url, `/`, the channel's ID, `/` and the message's ID. A client reads only that form: nothing between the address and the fragment but the `/`, which it may leave out, and IDs as the den writes them. It opens a link in the den it joined with that ID, wherever that den is now, so a link outlives a move, and never opens in another den that takes the address; the address names a den the client hasn't joined, and is what a browser opens.
- A browser never sends a fragment, so a link opened outside a client tells the den nothing of which message. The den answers `GET /` with a line of plain text saying it's a den on Dens, whose links open in Dens.
- Rewriting links leaves the fragment as it is (see [Links](#links)), so a link to a message reads the same in a channel and in a DM.
- Before opening one, the client checks that the channel is a text channel or DM the member can see, and that the message is there: `around` it with a `limit` of 1 answers it, or the message after it once it's gone (see [History](#history)).
- A link to an event is the same with `/#e/`, `den_id`, `/` and the event's ID: `https://den.example.com/#e/<den ID>/12`. A client reads it as it reads a link to a message, and shows the event's card (see [Cards and badges](members.md#cards-and-badges)).
- A link to a channel is the same with `/#c/`, `den_id`, `/` and the channel's ID: `https://den.example.com/#c/<den ID>/41`. A client opens it as a link to a message, at the channel's newest message, where it's a text channel, a DM or the member's notes they can see; a voice channel's opens nothing.

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
POST   /api/channels/{id}/messages   {"nonce": "<16 bytes>", "text", "reply_to"?, "forwarded"?, "forward_source"?}   201 message
PATCH  /api/messages/{id}            {"revision", "text", "attachments"?}                                            200 message
DELETE /api/messages/{id}                                                                                            204
```

- `nonce` makes a send idempotent: for 10 minutes, a retry with the same member and nonce returns the original message instead of posting again. The resulting `message.created` event carries the nonce, so the sender's other devices can match their pending copy. The nonce appears only there and in the send response, never in history, except on a DM's messages, whose sealed text is bound to it.
- Sending moves the author's read position to their new message.
- An edit names the `revision` it was made against. If the message has changed since, the den refuses it with `409 edit_conflict` and returns the current message beside the error, as `{"error": {…}, "message": message}`. The client shows it with the member's draft kept to reapply. Two devices, or two co-editors, never silently overwrite each other.
- `forwarded: true` marks a forward, in a channel, where the den's info says `forward`: the den refuses it (`400 invalid_field`) on a sealed message, or with a poll, `reply_to` or `editors`, and refuses an edit or a tick of one with `403 forbidden`, but its author's edit that keeps its text, names no `editors` and lists only files already on it, which takes the others off while preserving `forward_source`. Where the den's info says `forward_links`, a channel forward may also carry `forward_source` (`{"den_id", "channel_id", "message_id"}`) as the sender's claim; the den checks only its shape (never whether the message exists or matches) and refuses `forward_source` (`400 invalid_field`) on a sealed message, without `forwarded: true`, or with a malformed 32-byte `den_id` or non-positive IDs. Clients omit `forward_source` when forwarding to a channel on a den without `forward_links`, and reject a sealed message carrying `forward_source` outside its seal.
- The author edits a message, and so do the members they named as its editors (see [Shared messages](#shared-messages)). The author deletes it, and so does staff of a higher rank than the author: moderators delete members' messages, and the owner anyone's. In a DM only the author deletes a message.
- Events: `message.created {message}`, `message.updated {message}`, `message.deleted {id, channel_id, files?, last_message_id?}`. A deleted message is gone for good, and so are its files, which `files` lists: clients drop them from memory, and later from their cache. `last_message_id` is the newest message left in the channel, or `""` once none is left, so clients clear an unread mark when a channel's newest message is deleted; older dens omit it, and clients opening a channel whose `last_message_id` still sits past its newest remaining message send that ID to clear it. An edit that takes files off a message lists them the same way, in `files` beside the message's own fields (see [Attachments](files.md#attachments)).
- Each member a deleted message counted for, in a DM the other member and in a channel each it mentioned, gets `read_state.updated` after its `message.deleted`: their `mention_count` without it, and where it was all that lay past their read position, the position moved past it, so older clients that don't read `last_message_id` on `message.deleted` still clear a DM whose last message went unread.

### Read state

```
PUT  /api/channels/{id}/read   {"message_id"}   204
POST /api/read                                  204
```

- The position only moves forward: a lower `message_id` is ignored.
- `POST /api/read` moves the member's position in every channel and DM they see, and in their notes, to its newest message, in one write, where `read_all` is in the den's info. It counts as one write against the member's limit.
- Clients send it once a message has been on screen, at most once every few seconds per channel.
- The den tells all the member's sessions with `read_state.updated {channel_id, message_id, mention_count}`, one for each channel whose position moved.
- `ready` carries `read_states`: for each channel, `channel_id`, its last message's ID as `last_message_id`, the member's read position as `message_id`, their unread mention count as `mention_count`, and for a DM, whether they closed it, as `closed`.

## Shared messages

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

## Emoji

Staff add emoji to the den, which members write by name in the markdown subset, as `:name:` (see the design's [Emoji](../design/messages.md#emoji)).

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

## Reactions

A reaction is a member's emoji on a message: one of Unicode's, from the list (`denproto.IsEmoji`), or one of the den's (see the design's [Reactions](../design/messages.md#reactions)).

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
- Each change records its message in the change log, its revision as it was (see [What changed while away](sync.md#what-changed-while-away)). Reactions mention nobody and notify nobody.

**A DM's.** Each member's reactions on a DM's message are one set, which their client seals: `{"emoji": [...]}` as JSON, each one of Unicode's emoji from the list or a name one of the den's emoji may have, as a DM's text writes them, each once, at most 20, padded with spaces to 1,024 bytes, and sealed with XChaCha20-Poly1305 under the DM's live key, bound to `"dens-dm-reactions-v1" ‖ den_id ‖ channel ‖ message ‖ member ‖ key_id ‖ revision`, as nonce ‖ ciphertext, 1,064 bytes.

- `PUT /api/messages/{id}/reactions/sealed` sets the member's set: `revision` is the revision of their set it was made against, 0 while they have none, and `sealed` is sealed as the revision after it. `key_id` must name the DM's live key, and one the member checked, as for a message, and `sealed` must be 1,064 bytes; otherwise `400 invalid_field`. A set made against another revision gets `409 edit_conflict` with the member's set as it is, `{"error": {…}, "reactions": sealed_reactions}`, without `reactions` while they have none, and the client makes it again on that one. Once the other member has left, `403 forbidden`.
- The den answers with the set as it keeps it, and sends it to both members as `dm.reactions {"message_id", "channel_id", "member_id", "key_id", "revision", "sealed"}`; a DM's message carries its members' sets as `dm_reactions`, a set a member, in the order of their IDs. A client keeps the newer of a member's sets by `revision`, and opening one keeps only emoji from the list and names a den's emoji may have, and drops anything else; one that doesn't open shows none.
- A set stays once made, empty or not, and goes with its message. The den sees that a member's reactions changed, and when, never what they are.

**Older ones.** An older page shows no reactions. An older den sends none and takes none; a client offers none for a den with no `emoji_limits`, since reactions came with emoji.

## Polls

A channel's message may be a poll, whose question its text is (see the design's [Polls](../design/messages.md#polls)). A poll as the den sends it is as the member it's sent to may see it, by its rules: in history, in what changed while away, in `message.created`, and in its events.

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
- Each vote and each end records its message in the change log, its revision as it was (see [What changed while away](sync.md#what-changed-while-away)). A poll goes with its message, and its votes with it.
- **@everyone.** Staff's `@everyone` in a channel, found by the mention rule, mentions each member who can see the channel but the author, as a mention by name does, and marks the message `everyone`, a client's sign to count it as a mention of its member and highlight it. It counts while the text holds it and staff put it there: an edit by anyone that keeps a marked one keeps the mark, and one that adds it marks the message only where staff make it. Anyone else's mentions the member named `everyone`, if the den has one; nobody new takes that username, which `/api/join` answers as `409 username_taken`.
- An older page shows a poll's question alone, and an @everyone as a mention of nobody, though the den counts it among the member's mentions. An older den takes no poll: a client offers none where the den sends no `emoji_limits`, which came with them.

## Collections

A den that supports collections sets `collections: true` in its `Den` object and includes `collections` (all personal collections of the member and all shared collections in text channels they currently see, with visible item counts) in `ready`.

### Objects and limits

- **Collection**: `id`, `channel_id` (present for shared channel collections, omitted for personal), `name` (1–50 runes, emoji kept whole, unique case-insensitively per channel or per member), `who_adds` (`"everyone"` or `"staff"` on shared, omitted on personal), `count` (visible non-expired item count, `0..200`), `created_by` (omitted on personal), `created_at` (unix milliseconds).
- **CollectionItem**: `message` (full `Message`, sealed as usual for DMs and Notes), `added_by`, `added_at` (unix milliseconds).
- Limits: at most 20 shared collections per text channel, 20 personal collections per member per den, and 200 items per collection. Name collisions return `409 collection_name_taken`.

### Endpoints

All mutating routes count toward the member's write rate limit.

- `GET /api/collections` — returns `{collections}` with fresh visible item counts.
- `POST /api/collections` — body `{name}`; creates a personal collection (`201`). Rejects `who_adds`.
- `POST /api/channels/{id}/collections` — staff only on text channels; body `{name, who_adds?}` (`who_adds` defaults to `"staff"`); creates a shared collection (`201`).
- `PATCH /api/collections/{id}` — body `{name?, who_adds?}`; staff on shared collections, owner on personal (personal rejects `who_adds`). Returns the updated `Collection` (`200`).
- `DELETE /api/collections/{id}` — staff on shared collections, owner on personal (`204`). Does not delete messages.
- `GET /api/collections/{id}/items` (optional `?before_at=<ms>&before_id=<message_id>`, both required together) — returns `{collection, items, has_more?, next_before_at?, next_before_id?}` ordered newest-added first (`added_at DESC, message_id DESC`), filtered through current channel/DM/Notes visibility and disappearing-message expiry (`200`), gzip-compressed when the client sends `Accept-Encoding: gzip`. Each page stops once adding another item would pass history's 896 KiB JSON budget (always returning at least one item), setting `has_more: true` with `next_before_at` and `next_before_id` from its last item so clients fetch remaining pages until `has_more` is false while `collection.count` reports the total visible count. Non-owners of personal collections and members who cannot see a shared collection's channel receive `404 not_found`.
- `POST /api/collections/{id}/items` — body `{message_id}`; idempotent if already present; returns `CollectionItemEvent` (`200`). Shared collections accept only messages from their own channel and enforce `who_adds`; personal collections accept any non-expired message in any channel, DM or Notes the owner sees.
- `DELETE /api/collections/{id}/items/{message_id}` — removes an item and returns `CollectionItemEvent` (`200`). On shared collections, allowed for whoever added the item or for staff; staff removing another member's item writes a `collection.item_removed` audit line.
- `GET /api/messages/{id}/collections` — returns `{collection_ids}` naming the caller's personal collections and that message's channel's shared collections that currently hold message `{id}` (`200`), or `404 not_found` if the message does not exist, has expired or is not visible.

### Events and audit kinds

- Events (sent to the channel's audience for shared collections and to the owner alone for personal collections): `collection.created` (`Collection`), `collection.updated` (`Collection`), `collection.deleted` (`{id, channel_id?}`), `collection.item_added` and `collection.item_removed` (`{collection_id, channel_id?, message_id, added_by?, added_at?, count}`).
- Staff audit kinds (shared collections only): `collection.created`, `collection.updated` (changes for `name` and `who_adds`), `collection.deleted`, `collection.item_removed` (only when staff removes an item added by another member).
