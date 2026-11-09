---
title: Privacy
weight: 5
---

Dens keeps your conversations away from big platforms, data brokers and ad
profiles, and lets them fade instead of piling up forever. It's a closed door,
not a bunker. This page says plainly who can read what, and what Dens doesn't
protect you from.

## Who can read what

| What | Who can read or see it |
| --- | --- |
| Messages and files in a channel | Members who can see the channel, and the den |
| Staff-only channels | Moderators, the owner, and the den |
| Calls and screen shares | Only everyone in the call: they're end-to-end encrypted. The den carries them but can't hear or see them; it sees who's in a call, and when. On a den running an older Dens, calls aren't, and the den could listen in or watch: the lock beside the call says which |
| Who plays which sound on the soundboard | Everyone in the call hears it and sees who played it, and the den knows who played which, as it knows who's in a call |
| DMs, and the photos and files in them | Only the two of you: they're end-to-end encrypted |
| Group DMs, their photos and files, and their names | Only their members, each from when they joined: they're end-to-end encrypted. The den sees who's in each group, and who changed it, but can't add anyone |
| Your notes to self, and the files in them | Only your own devices: they're end-to-end encrypted. The den sees that you write in them, and when |
| Reactions | In a channel, its members and the den. In a DM or a group DM, only its members, like its messages; the den sees only that someone's reactions changed, and when |
| Who voted for what in a poll | The den, always: it counts each vote. Members, as the poll's author chose: never, once it ends, or always |
| Who's interested in an event | The den, until the event ends, to tell them as it starts. Members see only how many |
| The badges you earn at events | Everyone who can see the event, on your profile, and the den. It keeps when you earned each, not who else was in the call |
| Who DMs whom, who's in which group DMs, when, and how much | The den |
| How long a DM's or a group DM's messages last before they disappear | Its members, and the den, which deletes them when their time comes |
| Your IP address | Each den you're connected to, while you're connected. Other members never see it, even in calls |
| Your profile | Members of that den. You have a separate one in each den, and no den can link it to your others |
| Your email or phone number | Nobody: Dens never asks for them |

How DMs stay between the two of you, and the code you compare to be sure of it,
is in [Usage](../usage/#direct-messages). Calls use the same codes: the
lock beside a call shows yours, and each member's, to compare with them.
The den decides who's in a call, so only the people listed there can hear
it.

## What leaves your computer

- **To each den you're in:** what you send, and your IP address while you're
  connected. Photos and videos lose their location and camera details on your
  computer before they're sent, so the den never gets them. Other files, such
  as documents and archives, go as they are, with whatever they carry inside: a
  PDF can hold its author's name, for one.
- **To Dens's release site:** once a day, a request for the latest version
  number, which shows the site your IP address. Turn it off in **Settings**,
  under **System**. The Flatpak doesn't ask: Flatpak itself checks Dens's
  repository there for updates, every 30 minutes while Dens runs, and your
  software center does too, on its own schedule.
- **To YouTube**, only if you turn on YouTube videos in the settings: your IP
  address for each video whose cover shows, and which videos you play.

That's all. There's no analytics or telemetry.

## Deleting

Deleting a message deletes it for real: from the den, and from members'
caches once they next connect. A den can also be set to delete messages after a
number of days, which its members see in its settings. Leaving a den deletes
your notes there. A backup keeps what it held until the backup itself is
deleted.

In a DM or a group DM, anyone in it can have its messages disappear an hour, a
day, a week or four weeks after they're sent. Each is then deleted as if its
sender had deleted it: from the den, and from every member's devices, even
those offline at the time. The den knows the time, since it does the deleting,
and still sees that the messages were sent, and when.

## What Dens doesn't protect you from

- **The den**, which can read channels and their files, could listen to calls,
  and sees who DMs whom and when. Join dens run by people you trust, or host
  your own.
- **A tampered den, with people who never compare codes.** It could hand each
  of you keys of its own and read your DMs. Comparing codes, even once, shows
  it: see [Usage](../usage/#comparing-codes).
- **Members** screenshotting or repeating what you say.
- **Whoever sees your screen.** A notification shows the start of a message, a
  DM's included. Turn off **Show the message**, in the settings under
  **Notifications**, and it says only who sent one.
- **A compromised computer**, yours or the den's.
- **Traffic analysis.** A den sees its members' IP addresses while they're
  connected.
- **A targeted investigation or a state.** Dens isn't built for that.

## Linux and Windows

Dens runs on both, but Linux locks it down further.

- On Linux, the service runs in a sandbox that limits what it can read and
  which parts of the system it can use. On Windows, it can only write where it
  should, but it can read anything an ordinary account can.
- Dens keeps its data encrypted on disk, with a key the service unlocks as it
  starts. On Linux that key is bound to the computer's TPM, or to a key only
  root can read. On Windows, only file permissions protect it, so anything
  running as admin can read it.
- Dens's Windows files aren't signed with a certificate Windows recognizes.
  The signature check in [Install](../install/windows/) is what proves they're
  Dens's.
- [The Flatpak](../install/flatpak/) runs as you, not as an account of
  its own, so programs you run can read its files, and the key it keeps if you
  tick **Remember on this computer**. Without that key, its data opens only
  with your local password. Other Flatpak apps can't reach any of it.

## Why bother

Being able to say dumb things, joke, vent and make art without a permanent,
searchable record is part of being human. Dens is built for that kind of
everyday privacy.

For more of it: use Linux rather than Windows, a privacy-minded browser such as
Ungoogled Chromium, and keep details that identify you out of what you post. If
your safety depends on it, use tools built for higher stakes, such as
[Signal](https://signal.org/) and [Qubes OS](https://www.qubes-os.org/), and
know that none of them is bulletproof either.
