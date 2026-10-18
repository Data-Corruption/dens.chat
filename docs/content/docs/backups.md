---
title: Backups and moving
weight: 4
---

Everything Dens keeps is on your computer: the keys to your accounts, your
settings, and the den, if you host one. A backup keeps it safe, and it's how
you move to another computer.

## Making a backup

Run this as yourself, not as admin:

```sh
dens backup
```

It asks for your local password, and writes one file into the folder you ran it
from, named like `dens-main-20261012-153000.backup` (`--output` picks another
place). The file is locked with your local password, so keep the two apart.

A backup holds everything except the cache of other dens' messages and files,
which fills up again by itself.
If you host a den, the den is in it too, and a backup keeps every message it
held, even ones auto-delete removes later, until you delete the backup.

## Restoring, or moving to another computer

Backups work between Linux and Windows, either way.

{{% steps %}}

### Install Dens

[Install](../install/) it on the computer you're moving to.

### Restore the backup

Copy the file over, then restore it as admin. It asks for the backup's local
password, and replaces Dens's data on this computer with the backup's.

{{< tabs >}}
{{< tab name="Linux" >}}
```sh
sudo dens restore dens-main-20261012-153000.backup
```
{{< /tab >}}
{{< tab name="Windows" >}}
In an admin terminal:

```powershell
dens restore dens-main-20261012-153000.backup
```
{{< /tab >}}
{{< /tabs >}}

### Open Dens

Run `dens open`. Browsers sign in again after a restore.

{{% /steps %}}

If the backup has a den, it comes back with hosting off, so no copy of a den
goes online by itself. Set up Caddy and the router for the new computer, point
the domain at it, and turn hosting on in **Settings**, under **System**
([Host a den](../host/)). Members' Dens find the den again by themselves.

## Losing your password or your computer

- **Lost the computer**, but you have a backup and its local password: restore
  it on a new one.
- **Lost both** the computer and the local password: what was on it is gone, by
  design. Nobody can open it, including you. Your accounts in dens survive,
  though: on a new install, sign in with your username and a recovery code,
  then your DM recovery key to read your DMs ([Join a den](../join/)).

## Handing a den to someone else

On the den's computer, with hosting on, run:

```sh
dens den owner-invite
```

It asks for your local password, and prints a one-time invite that lasts an
hour. Whoever joins with it becomes the den's owner, and you become a
moderator. The den itself stays on your computer, which still has the last
word: you can make another owner invite whenever you like.

To move the den to their computer as well, they restore a backup of yours. A
backup holds everything on that install, your own accounts and their keys
included, so do that only from an install that hosts the den and nothing else of
yours.

An owner invite is also the way back if you've locked yourself out of the
owner's account, say by signing out your only device with no recovery codes
left: join with it under a new username.
