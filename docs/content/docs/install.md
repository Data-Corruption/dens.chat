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
  and Pop!\_OS, Arch, and CachyOS. Others aren't supported yet.
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

To host a den on this computer, add `--den`. You can also rerun this anytime
and add it then: running the installer again keeps your data, and it's another
way to update, too.
{{< /tab >}}
{{< tab name="Windows" >}}
```powershell
powershell -ExecutionPolicy Bypass -File install.ps1
```

To host a den on this computer, add `-Den`. You can also rerun this anytime
and add it then: running the installer again keeps your data, and it's another
way to update, too.
{{< /tab >}}
{{< /tabs >}}

### Open Dens

As yourself, not as admin. On Windows, that means a new, ordinary terminal.

```sh
dens open
```

On Linux you can also pick Dens from your apps menu.

{{% /steps %}}

The first time, Dens asks you to set a **local password**. It protects Dens on
this computer: you'll need it to make a backup or to see your keys. It's
separate from the passwords you'll pick for each den.

Dens lives at `http://localhost:8484`, and your browser stays signed in to it
for 30 days after you last used it. After that, or in another browser, run
`dens open` again.

Next: [join a den](../join/) with your invite, or [host one](../host/).

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
a [backup](../backups/) first if you might want it back.

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
