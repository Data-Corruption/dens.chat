---
title: Privacy
weight: 6
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
| Calls and screen shares | Everyone in the call. The den could listen in or watch, since calls pass through it |
| DMs, and the photos and files in them | Only the two of you: they're end-to-end encrypted |
| Who DMs whom, when, and how much | The den |
| Your IP address | Each den you're connected to, while you're connected. Other members never see it, even in calls |
| Your profile | Members of that den. You have a separate one in each den, and no den can link it to your others |
| Your email or phone number | Nobody: Dens never asks for them |

How DMs stay between the two of you, and the code you compare to be sure of it,
is in [Join a den](../join/#direct-messages).

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
number of days, which its members see in its settings. A backup keeps what it
held until the backup itself is deleted.

## What Dens doesn't protect you from

- **The den**, which can read channels and their files, could listen to calls,
  and sees who DMs whom and when. Join dens run by people you trust, or host
  your own.
- **Members** screenshotting or repeating what you say.
- **Whoever sees your screen.** A notification shows the start of a message, a
  DM's included. Turn off **Show the message**, in the settings under **Chat &
  media**, and it says only who sent one.
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
  The signature check in [Install](../install/) is what proves they're Dens's.
- [The Flatpak](../install/#the-flatpak) runs as you, not as an account of
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
