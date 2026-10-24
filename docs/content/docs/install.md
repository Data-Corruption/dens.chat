---
title: Install
weight: 1
---

Dens runs as a small background service on your computer, and you use it in
your browser like a website. You install it once. After that it starts with
your computer, and it keeps running while you're logged out.

## What it runs on

- **Windows 11**, version 24H2 or later, Home or Pro.
- **Linux**: Fedora (Workstation and its spins), Bazzite, Debian 12 or later,
  Ubuntu 24.04 or later and the distributions built on it, such as Linux Mint
  and Pop!\_OS, Arch, and CachyOS. On the others, such as NixOS and SteamOS,
  use [the Flatpak](#the-flatpak), which joins dens but can't host one.
- **An up-to-date browser**: Firefox, Chrome, Edge, or one built on them.

Linux keeps Dens more tightly locked down than Windows can; [Privacy](../privacy/#linux-and-windows)
has the details.

## Install it

Installing needs admin rights, once. They're how Dens gets a locked-down
account of its own to run as, so Dens itself never runs as admin.

{{% steps %}}

### Download the installer

{{< tabs >}}
{{< tab name="Linux" >}}
The installer needs `curl`. Ubuntu doesn't include it: `sudo apt install curl`.

```sh
curl -fsSLO https://releases.dens.chat/install.sh
```
{{< /tab >}}
{{< tab name="Windows" >}}
Open an admin terminal: right-click the Start button and choose **Terminal (Admin)**.

```powershell
cd $HOME\Downloads
Invoke-WebRequest 'https://releases.dens.chat/install.ps1' -OutFile install.ps1
```
{{< /tab >}}
{{< /tabs >}}

{{% details title="To verify it wasn't tampered with" closed="true" %}}

Check its signature before you run it. That proves it's the installer Dens's
release process built, and not a copy someone swapped in on the way; the
installer then checks everything it downloads the same way. You need
[cosign](https://docs.sigstore.dev/cosign/system_config/installation/) for it.

{{< tabs >}}
{{< tab name="Linux" >}}
```sh
curl -fsSLO https://releases.dens.chat/install.sh.cosign.bundle
cosign verify-blob install.sh \
  --bundle install.sh.cosign.bundle \
  --certificate-identity https://github.com/Data-Corruption/dens.chat/.github/workflows/release.yml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```
{{< /tab >}}
{{< tab name="Windows" >}}
```powershell
Invoke-WebRequest 'https://releases.dens.chat/install.ps1.cosign.bundle' -OutFile install.ps1.cosign.bundle
cosign verify-blob install.ps1 `
  --bundle install.ps1.cosign.bundle `
  --certificate-identity 'https://github.com/Data-Corruption/dens.chat/.github/workflows/release.yml@refs/heads/main' `
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```
{{< /tab >}}
{{< /tabs >}}

It prints `Verified OK`. Anything else means don't run it.

{{% /details %}}

### Run it

{{< tabs >}}
{{< tab name="Linux" >}}
```sh
sudo sh install.sh
```

Running the installer again later keeps your data, and it's another way to
update, too.
{{< /tab >}}
{{< tab name="Windows" >}}
```powershell
powershell -ExecutionPolicy Bypass -File install.ps1
```

Running the installer again later keeps your data, and it's another way to
update, too.
{{< /tab >}}
{{< /tabs >}}

### Open Dens

As yourself, not as admin. On Windows, that means a new, ordinary terminal.

```sh
dens open
```

This opens Dens in your default browser. Add `--print` and it prints the link
instead, for you to open in whichever browser you like. The link works once,
within two minutes.

Waterfox on Windows can show a blank page for a link another program opens.
If it does, click the address bar and press Enter: the link is still there,
and still works.

On Linux you can also pick Dens from your apps menu.

{{% /steps %}}

The first time, Dens asks you to set a **local password**. It protects Dens on
this computer: you'll need it to make a backup or to see your keys. It's
separate from the passwords you'll pick for each den.

Dens lives at `http://localhost:8484`, and your browser stays signed in to it
for 30 days after you last used it. After that, or in another browser, run
`dens open` again.

Next: [join a den](../join/) with your invite, or [host one](../host/). Any
install can host a den: you turn hosting on in Dens's settings when you want
to.

## Updating

When there's a new version, Dens shows a notice with the command. Updates
never install themselves.

{{< tabs >}}
{{< tab name="Linux" >}}
```sh
sudo dens update
```
{{< /tab >}}
{{< tab name="Windows" >}}
In an admin terminal:

```powershell
dens update
```
{{< /tab >}}
{{< /tabs >}}

To look for updates, Dens asks its release site once a day for the latest
version number. You can turn that off in **Settings**, under **System**.

## Removing it

This deletes Dens's data on this computer, including any den it hosts, so make
a [backup](../backups/) first if you might want it back. It shows what it
removes, then asks you to type `perm delete data`, and anything else cancels.
To stop Dens without deleting anything, until the computer restarts, run
`sudo dens service stop` on Linux, or `dens service stop` in an admin
terminal on Windows.

{{< tabs >}}
{{< tab name="Linux" >}}
```sh
sudo dens uninstall
```
{{< /tab >}}
{{< tab name="Windows" >}}
In an admin terminal:

```powershell
dens uninstall
```
{{< /tab >}}
{{< /tabs >}}

## The Flatpak

On the Linux distributions the installer doesn't support, Dens comes as a
Flatpak. It joins dens like any install, but it runs as you, in your session,
instead of as a service with an account of its own. So it's only up while
you're signed in, and it can't host a den, which has to keep running while
nobody is.

{{% steps %}}

### Install it

[Install Dens](flatpak+https://releases.dens.chat/flatpak/chat.dens.Dens.flatpakref).
Your software center opens and offers to install it. If nothing opens,
[download the file](https://releases.dens.chat/flatpak/chat.dens.Dens.flatpakref)
and open it, or run:

```sh
flatpak install https://releases.dens.chat/flatpak/chat.dens.Dens.flatpakref
```

It also adds Flathub, where the runtime Dens runs on comes from, if your
computer doesn't have it yet. A software center that installs for everyone on
the computer, as Fedora's does, asks for your admin password. GNOME Software calls Dens potentially
unsafe because it can use X11, the older way of drawing windows: Dens draws
none, but push to talk needs X11 when you're signed in to an X11 session.

### Open Dens

Pick Dens from your apps menu. The first time, it asks you to set a local
password, as any install does.

{{% /steps %}}

The Flatpak asks for your local password each time it starts, unless you tick
**Remember on this computer**. Then your desktop's keyring keeps a key that
opens it, and Dens starts by itself when you sign in. On KDE without a wallet,
KDE asks you to make one the first time: pick **Classic**, with the password
you sign in with. The default, GPG, needs a GPG key of your own.

Who can read what Dens keeps differs from the installed Dens; [Privacy](../privacy/#linux-and-windows)
has the details.

Dens's commands are inside the Flatpak. To run them as `dens`, add this to
your shell's config:

```sh
alias dens='flatpak run --command=dens chat.dens.Dens'
```

### Updating and removing it

Your software center updates Dens along with your other apps, so you don't
need the link again; KDE's Discover can't open it once Dens is installed
anyway. Once an update is in, Dens offers to restart into it.

To remove it, with everything it keeps, make a [backup](../backups/) first if
you might want it back, then run:

```sh
flatpak uninstall --delete-data chat.dens.Dens
```
