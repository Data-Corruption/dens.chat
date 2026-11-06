---
title: Windows
weight: 1
---

Dens runs as a small background service that starts with Windows, and you
use it in your browser, like a website.

## What it needs

- Windows 11, version 24H2 or later, Home or Pro.
- An up-to-date browser: Firefox, Chrome, Edge, or one built on them.
- Admin rights, once, to install it.

{{% details title="Which browser works best" closed="true" %}}

[Ungoogled Chromium](https://github.com/ungoogled-software/ungoogled-chromium):
Dens has had the fewest problems in it, its screen shares can carry a tab's
sound, which Firefox and the browsers built on it can't, and it makes the
smaller copies of videos you send several times faster.

{{% /details %}}

{{% details title="Why it needs admin rights" closed="true" %}}

They're how Dens gets a locked-down account of its own to run as, so Dens
itself never runs as admin. Linux locks it down more tightly than Windows can;
[Privacy](../../privacy/#linux-and-windows) has the details.

{{% /details %}}

## Install it

{{% steps %}}

### Download the installer

Right-click the Start button, choose **Terminal (Admin)**, and run:

```powershell
cd $HOME\Downloads
Invoke-WebRequest 'https://releases.dens.chat/install.ps1' -OutFile install.ps1
```

{{% details title="To check it wasn't tampered with (optional)" closed="true" %}}

Its signature proves it's the installer Dens's release process built, and not
a copy someone swapped in on the way; the installer then checks everything it
downloads the same way. You need
[cosign](https://docs.sigstore.dev/cosign/system_config/installation/).

```powershell
Invoke-WebRequest 'https://releases.dens.chat/install.ps1.cosign.bundle' -OutFile install.ps1.cosign.bundle
cosign verify-blob install.ps1 `
  --bundle install.ps1.cosign.bundle `
  --certificate-identity 'https://github.com/Data-Corruption/dens.chat/.github/workflows/release.yml@refs/heads/main' `
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

It prints `Verified OK`. Anything else means don't run it.

{{% /details %}}

### Run it

In the same terminal:

```powershell
powershell -ExecutionPolicy Bypass -File install.ps1
```

### Open Dens

In a new, ordinary terminal, not an admin one, run this, or pick Dens from
the Start menu:

```powershell
dens open
```

The first time, Dens asks you to set a **local password**. You'll need it to
make a backup or to see your keys.

{{% details title="More on opening it" closed="true" %}}

`dens open` opens Dens in your default browser. Add `--print` and it prints
the link instead, for whichever browser you like. The link works once, within
two minutes.

Waterfox can show a blank page for a link another program opens. If it does,
click the address bar and press Enter: the link still works.

Dens lives at `http://localhost:8484`, and your browser stays signed in to it
for 30 days after you last used it. After that, or in another browser, run
`dens open` again.

The local password protects Dens on this computer, and never leaves it. It's
separate from the passwords you'll pick for each den.

{{% /details %}}

{{% /steps %}}

Next: [join a den](../../join/) with your invite, or [host one](../../host/).

## Updating

Dens shows a notice when there's a new version, and never installs it by
itself. In an admin terminal, run:

```powershell
dens update
```

{{% details title="How Dens finds updates" closed="true" %}}

Dens asks its release site once a day for the latest version number. You can
turn that off in **Settings**, under **System**. Running the installer again
updates Dens too, and keeps your data.

{{% /details %}}

## Removing it

This deletes Dens's data on this computer, a den it hosts included, so make a
[backup](../../backups/) first if you might want it back. In an admin
terminal, run:

```powershell
dens uninstall
```

It lists what it removes, then asks you to type `perm delete data`. Anything
else cancels.

{{% details title="To stop Dens without removing it" closed="true" %}}

Run `dens service stop` in an admin terminal. It stays stopped until the
computer restarts.

{{% /details %}}
