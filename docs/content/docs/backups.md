---
title: Backups and moving
weight: 5
---

Everything Dens keeps is on your computer: the keys to your accounts, your
settings, and the den, if you host one. A backup keeps it safe, and it's how
you move to another computer. To move a den on its own, use its
[export](#moving-a-den) instead.

## Making a backup

In Dens's settings, under System, choose **Back up**. It asks for your local
password, says about how large the backup will be, and your browser downloads
it: one file, named like `dens-main-20261012-153000.backup`. The file is locked
with your local password, so keep the two apart.

Or run this as yourself, not as admin, which writes the file into the folder you
ran it from (`--output` picks another place):

```sh
dens backup
```

It says how large the backup will be, and stops before it starts if the folder
hasn't room for it.

A backup that stops partway is never kept as finished: `dens backup` says it
stopped, and your browser marks the download failed. Make it again.

In the Flatpak, `dens backup` writes into Dens's own folder,
`~/.var/app/chat.dens.Dens`, so the settings' **Back up** is the easier way.

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

Restoring needs free space on the disk Dens is on: about the backup's size, and
a gigabyte to spare. It checks before it starts, and if the disk still runs
short, it stops and says how much more it needs.

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
{{< tab name="Flatpak" >}}
As yourself, not as admin, handing the file to the Flatpak:

```sh
flatpak run --command=dens --file-forwarding chat.dens.Dens restore @@ dens-main-20261012-153000.backup @@
```

Dens starts locked afterwards, until the backup's local password opens it, and
it no longer remembers its key, so tick **Remember on this computer** again if
you want it to.
{{< /tab >}}
{{< /tabs >}}

### Open Dens

Run `dens open`, or in the Flatpak pick Dens from your apps menu. Browsers sign
in again after a restore.

{{% /steps %}}

If the backup has a den, it comes back with hosting off, so no copy of a den
goes online by itself. Set up Caddy and the router for the new computer, point
the domain at it, and turn hosting on in **Settings**, under **System**
([Host a den](../host/)). Members' Dens find the den again by themselves.

## Moving a den

A backup moves everything on your computer at once. To move a den alone, to
a server or to someone else's computer, use its **export**: it only holds the
den, with its members, messages and files, and none of your client data, the
dens you've joined, your keys there and your settings, which stay where they
are. In **Settings**, under **System**, choose **Move the den to another
computer**, or run `dens den export` with hosting off.

The other computer restores the export as it would a backup, and turns
hosting on. Once it serves the den, **Remove from this computer** deletes the
copy on yours, and your Dens reaches the den at its address like any
member's. [Host on a server](../server/) goes through it step by step.

A restore replaces everything on the install it goes into, so restore an
export where Dens holds nothing else yet, as on a new server. On a computer
whose Dens is in use, someone's who's in the den already, say, give the den
an install of its own: a second instance, as the end of
[Host a den](../host/#what-hosting-costs-you) describes, and restore into
that one with `--instance`.

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

To move the den to their computer as well, give them its
[export](#moving-a-den), and remove your copy once they host it. If they
use Dens on that computer already, they restore it into a second instance,
which keeps their own accounts as they are.

An owner invite is also the way back if you've locked yourself out of the
owner's account, say by signing out your only device with no recovery codes
left: join with it under a new username.
