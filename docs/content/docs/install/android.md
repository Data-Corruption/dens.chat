---
title: Android
weight: 4
aliases:
  - /docs/android/
---

Android 12 or later. Dens isn't in Google Play: you download the app from
Dens's release site, and it updates from there, asking you each time.

## Your phone will warn you

Installing Dens brings up to three warnings: one from your browser, one from
Android, and one from Play Protect, which may say the app is harmful. They
appear because Dens comes from outside a store and its developers haven't
registered with Google.

We haven't registered on purpose. A registered app installs only as long as
Google keeps its developer on the list, and Dens is built so that it doesn't
need a company's permission to run.
[Keep Android Open](https://keepandroidopen.org/) makes the wider case.

What you have in place of Google's word: Dens's
[source is public](https://github.com/Data-Corruption/dens.chat), each release
is built from it on GitHub and signed there, and Dens checks that signature on
every update. You can [check it yourself](#check-the-app-yourself) before you
install.

## Install

{{% steps %}}

### Download it

In your phone's browser, open
[releases.dens.chat/dens.apk](https://releases.dens.chat/dens.apk). The browser
warns that the file might be harmful, as it does for any app that isn't from a
store. Download it anyway.

### Let the browser install apps

Open the download. Android says your browser isn't allowed to install apps:
tap **Settings**, turn on **Allow from this source**, and go back.

On a Galaxy phone, **Auto Blocker** may block it instead. Turn it off in
**Settings**, **Security and privacy**, **Auto Blocker**, and leave it off, or
it blocks Dens's updates too.

### Install it

Tap **Install**.

Play Protect may offer to scan the app first, which sends its details to
Google. That's up to you. After a scan it may say the app may be harmful: tap
the arrow beside the warning, then **Install anyway**.

Play Protect doesn't say why, and we don't know for sure. A test build that
asks for the same permissions passed, so our best guess is that Dens's name
and signing key are new to Google.

### Open Dens

The first time, it asks you to set a local password.

{{% /steps %}}

## Keep it connected

Dens stays connected to your dens while it's off screen, which is how messages
and notifications reach you. Two settings stop that:

- **Battery set to Restricted.** Nothing arrives until you open Dens again.
  Leave it at **Optimized** or **Unrestricted**, in **Settings**, **Apps**,
  **Dens**, **Battery**.
- **Galaxy phones put apps to sleep.** Add Dens to **Never auto sleeping
  apps**, in **Settings**, **Battery**, **Background usage limits**.

## Updates

Dens tells you about an update in a notification and on its home. Tap either,
and Android asks you to confirm. The first time, Android also asks you to let
Dens install apps, as it asked of your browser, and Play Protect may warn
again.

## If Android refuses to install it

Google is starting to make phones refuse apps from developers who haven't
registered with it: in Brazil, Indonesia, Singapore and Thailand since
September 30, 2026, and everywhere in 2027, by its plan.

On such a phone, Dens installs only through Android's **advanced flow**, which
you set up once in **Developer options**. It makes you wait a day, then asks
how long to allow apps from unverified developers. Choose **for good**: with 7
days, Dens's updates fail once they run out.

Phones without Google's certification, such as those running GrapheneOS or
LineageOS, don't check.

## Uninstalling

Uninstalling Dens deletes everything it keeps on the phone, its keys among
them. Android's backups and its transfers to a new phone leave Dens's data
out.

Your accounts in your dens stay. On a new phone or a computer, install Dens
and sign in as another device, with your den password or a recovery code
([Usage](../../usage/)).

## Check the app yourself

To prove the download is the app Dens's release process built, check it on a
computer first. Dens checks every update the same way.

{{% details title="With cosign" closed="true" %}}

You need
[cosign](https://docs.sigstore.dev/cosign/system_config/installation/).

```sh
version=$(curl -fsSL https://releases.dens.chat/version)
curl -fsSLO "https://releases.dens.chat/releases/$version/checksums.txt"
curl -fsSLO "https://releases.dens.chat/releases/$version/checksums.txt.cosign.bundle"
curl -fsSLO https://releases.dens.chat/dens.apk
cosign verify-blob checksums.txt \
  --bundle checksums.txt.cosign.bundle \
  --certificate-identity https://github.com/Data-Corruption/dens.chat/.github/workflows/release.yml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum dens.apk
grep android-arm64.apk checksums.txt
```

cosign prints `Verified OK`, and the last two lines start with the same
checksum. If they don't, a new version may have come out in between: run it
again. Then copy that `dens.apk` to your phone and open it there.

{{% /details %}}
