---
title: Linux by the Flatpak
linkTitle: The Flatpak
weight: 3
---

On the Linux distributions the installer doesn't support, such as NixOS and
SteamOS, Dens comes as a Flatpak. It joins dens like any install, and you use
it in your browser, like a website.

## What it needs

- Flatpak, which most distributions come with.
- An up-to-date browser: Firefox, Chrome, Edge, or one built on them.

The Flatpak runs as you, in your session, instead of as a service with an
account of its own. It's only up while you're signed in, so it can't host a
den, and programs you run can read what it keeps:
[Privacy](../../privacy/#linux-and-windows) has the details.

{{% details title="Which browser works best" closed="true" %}}

[Ungoogled Chromium](https://github.com/ungoogled-software/ungoogled-chromium):
Dens has had the fewest problems in it, its screen shares can carry a tab's
sound, which Firefox and the browsers built on it can't, and it makes the
smaller copies of videos you send several times faster.

{{% /details %}}

## Install it

{{% steps %}}

### Install the Flatpak

[Install Dens](flatpak+https://releases.dens.chat/flatpak/chat.dens.Dens.flatpakref).
Your software center opens and offers to install it.

{{% details title="If nothing opens" closed="true" %}}

[Download the file](https://releases.dens.chat/flatpak/chat.dens.Dens.flatpakref)
and open it, or run:

```sh
flatpak install https://releases.dens.chat/flatpak/chat.dens.Dens.flatpakref
```

{{% /details %}}

{{% details title="What your software center may ask or say" closed="true" %}}

It adds Flathub, where the runtime Dens runs on comes from, if your computer
doesn't have it yet. A software center that installs for everyone on the
computer, as Fedora's does, asks for your admin password.

GNOME Software calls Dens potentially unsafe because it can use X11, the older
way of drawing windows. Dens draws none, but push to talk needs X11 when
you're signed in to an X11 session.

{{% /details %}}

### Open Dens

Pick Dens from your apps menu. The first time, it asks you to set a **local
password**, which you'll need to make a backup or to see your keys.

Tick **Remember on this computer** and Dens starts by itself when you sign in.
Without it, Dens asks for the password each time it starts.

{{% details title="What Remember does" closed="true" %}}

Your desktop's keyring keeps a key that opens Dens. On KDE without a wallet,
KDE asks you to make one the first time: pick **Classic**, with the password
you sign in with. The default, GPG, needs a GPG key of your own.

The local password protects Dens on this computer, and never leaves it. It's
separate from the passwords you'll pick for each den.

{{% /details %}}

{{% /steps %}}

Next: [join a den](../../join/) with your invite.

## Its commands

Dens's commands are inside the Flatpak. To run them as `dens`, add this to
your shell's config:

```sh
alias dens='flatpak run --command=dens chat.dens.Dens'
```

## Updating

Your software center updates Dens along with your other apps. Once an update
is in, Dens offers to restart into it.

{{% details title="Why not the link again" closed="true" %}}

You don't need it: the software center already knows where Dens comes from.
KDE's Discover can't open the link once Dens is installed anyway.

{{% /details %}}

## Removing it

This deletes Dens and everything it keeps, so make a [backup](../../backups/)
first if you might want it back. Then run:

```sh
flatpak uninstall --delete-data chat.dens.Dens
```
