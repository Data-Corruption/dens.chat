---
title: Linux
weight: 2
---

Ubuntu doesn't include `curl`: run `sudo apt install curl` first.

```sh
curl -fsSL https://releases.dens.chat/install.sh | sudo sh
```

```sh
dens open
```

{{% details title="Why it asks for sudo" closed="true" %}}

The installer and updater need root to give Dens an account of its own, so
the programs you run can't read its memory or its keys. They're only needed
during setup: Dens itself never runs as root.
[Privacy](../../privacy/#linux-and-windows) has more.

{{% /details %}}

## Updating

```sh
sudo dens update
```

Or re-run the install command.

## Uninstalling

This deletes Dens's data on this computer, a den it hosts included, so make a
[backup](../../backups/) first if you might want it back. Then run:

```sh
sudo dens uninstall
```

To stop Dens without removing it, run `sudo dens service stop`. It stays
stopped until the computer restarts.

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

```sh
curl -fsSLO https://releases.dens.chat/install.sh
curl -fsSLO https://releases.dens.chat/install.sh.cosign.bundle
cosign verify-blob install.sh \
  --bundle install.sh.cosign.bundle \
  --certificate-identity https://github.com/Data-Corruption/dens.chat/.github/workflows/release.yml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

It prints `Verified OK`. Anything else means don't run it. Then run it, with
any options after it, such as `--dry-run` to see what it would change, or
`--den-port` and a port to move the den:

```sh
sudo sh install.sh
```

{{% /details %}}
