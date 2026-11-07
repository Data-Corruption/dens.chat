---
title: Windows
weight: 1
---

Windows 11, version 24H2 or later. Open **Terminal** and run:

```powershell
irm 'https://releases.dens.chat/install.ps1' | iex
```

{{% details title="Why it asks for admin rights" closed="true" %}}

The installer and updater need admin rights to give Dens an account of its
own, so the programs you run can't read its memory or its keys. They're only
needed during setup: Dens itself never runs with them.
[Privacy](../../privacy/#linux-and-windows) has more.

{{% /details %}}

## Updating

Re-run the install command to update.

## Uninstalling

This deletes Dens's data on this computer, a den it hosts included, so make a
[backup](../../backups/) first if you might want it back. Right-click the
Start button, choose **Terminal (Admin)**, and run:

```powershell
dens uninstall
```

To stop Dens without removing it, run `dens service stop` there instead. It
stays stopped until the computer restarts.

---

{{% details title="Which browser works best" closed="true" %}}

[Ungoogled Chromium](https://github.com/ungoogled-software/ungoogled-chromium):
Dens has had the fewest problems in it, its screen shares can carry a tab's
sound, which Firefox and the browsers built on it can't, and it makes the
smaller copies of videos you send several times faster.

Waterfox can show a blank page for a link another program opens, as Dens
opens itself. If it does, click the address bar and press Enter: the link
still works.

{{% /details %}}

{{% details title="To check the installer first, or give it options" closed="true" %}}

Its signature proves it's the installer Dens's release process built, and not
a copy someone swapped in on the way; the installer then checks everything it
downloads the same way. You need
[cosign](https://docs.sigstore.dev/cosign/system_config/installation/).
Right-click the Start button, choose **Terminal (Admin)**, and run:

```powershell
cd $HOME\Downloads
Invoke-WebRequest 'https://releases.dens.chat/install.ps1' -OutFile install.ps1
Invoke-WebRequest 'https://releases.dens.chat/install.ps1.cosign.bundle' -OutFile install.ps1.cosign.bundle
cosign verify-blob install.ps1 `
  --bundle install.ps1.cosign.bundle `
  --certificate-identity 'https://github.com/Data-Corruption/dens.chat/.github/workflows/release.yml@refs/heads/main' `
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

It prints `Verified OK`. Anything else means don't run it. Then run it in the
same terminal, with any options after it, such as `-DryRun` to see what it
would change, or `-DenPort` and a port to move the den:

```powershell
powershell -ExecutionPolicy Bypass -File install.ps1
```

{{% /details %}}

{{% details title="If Windows blocks it" closed="true" %}}

That's **Smart App Control**, which blocks programs whose signature Windows
doesn't recognize, Dens among them, and makes no exception for one app. To
install Dens, turn it off: in **Windows Security**, open **App & browser
control**, then **Smart App Control settings**, and choose **Off**. On some
computers, turning it back on later means reinstalling Windows.

{{% /details %}}
