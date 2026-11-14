---
title: Flatpak
weight: 3
---

The Flatpak runs as you, and only while you're signed in; it can't host a den,
and it's less isolated than the direct install:
[Privacy](../../privacy/#linux-and-windows) has the details.

[Install Dens](flatpak+https://releases.dens.chat/flatpak/chat.dens.Dens.flatpakref):
your software center opens and offers to install it.

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

Then pick Dens from your apps menu. Tick **Remember on this computer** as you
set your local password, and Dens starts by itself when you sign in.

{{% details title="On KDE without a wallet" closed="true" %}}

KDE asks you to make one the first time you tick Remember: pick **Classic**,
with the password you sign in with. The default, GPG, needs a GPG key of your
own.

{{% /details %}}

## Updating

Your software center updates Dens along with your other apps, and Dens offers
to restart into each update. An update that asks for a new permission waits
for you: version 1.12.0 asks to play and record sound, for
[Relay](../../relay/), so update to it through your software center or with
`flatpak update`.

## Uninstalling

This deletes Dens and everything it keeps, so make a [backup](../../backups/)
first if you might want it back. Then run:

```sh
flatpak uninstall --delete-data chat.dens.Dens
```

---

{{% details title="Its commands" closed="true" %}}

Dens's commands are inside the Flatpak. To run them as `dens`, add this to
your shell's config:

```sh
alias dens='flatpak run --command=dens chat.dens.Dens'
```

{{% /details %}}

{{% details title="Which browser works best" closed="true" %}}

[Ungoogled Chromium](https://github.com/ungoogled-software/ungoogled-chromium):
Dens has had the fewest problems in it, its screen shares can carry a tab's
sound, which Firefox and the browsers built on it can't, and it makes the
smaller copies of videos you send several times faster.

Waterfox can show a blank page for a link another program opens, as Dens
opens itself. If it does, click the address bar and press Enter: the link
still works.

{{% /details %}}
