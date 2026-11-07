---
title: Android
weight: 4
aliases:
  - /docs/android/
---

Android 12 or later. Dens isn't in Google Play or any other store: you
download it from Dens's release site, and it updates itself from there,
asking you before each update.

{{% steps %}}

### Download it

In your phone's browser, open
[releases.dens.chat/dens.apk](https://releases.dens.chat/dens.apk). The browser
warns that the file might be harmful, as it does for any app from outside a
store. Download it anyway.

### Let the browser install apps

Open the download. The first time, Android says your phone isn't allowed to
install unknown apps from this source. Tap **Settings**, turn on **Allow from
this source**, and go back.

{{% details title="On a newer Galaxy phone" closed="true" %}}

Samsung's **Auto Blocker** blocks it instead. Turn it off in **Settings**,
under **Security and privacy**, then **Auto Blocker**, and leave it off: while
it's on, Dens can't install its updates either.

{{% /details %}}

### Install it

Android asks whether to install Dens: tap **Install**.

{{% details title="If Play Protect warns about it" closed="true" %}}

Play Protect may offer to scan it first, which sends the app's details to
Google. Scanning it is up to you. Scanned, it may say the app may be harmful,
without saying why: to install it anyway, tap the arrow beside the warning,
then **Install anyway**.

{{% /details %}}

### Open Dens

Open Dens from your apps. The first time, it asks you to set a local password.

{{% /steps %}}

## Keeping it connected

Dens keeps your dens connected while it isn't on screen, which is what brings
your messages and notifications while the phone sleeps. Two settings stop it:

- **Battery set to Restricted.** Android cuts Dens off as soon as it leaves
  the screen, and nothing arrives until you open it again. Leave Dens at
  **Optimized** or **Unrestricted**: in **Settings**, **Apps**, **Dens**, then
  **Battery**.
- **Galaxy phones put apps to sleep** that they think you don't use. Put Dens
  among **Never auto sleeping apps**: in **Settings**, **Battery**, then
  **Background usage limits**.

## Updating

When Dens finds an update, it says so in a notification and on Dens's home:
tap either, and Android asks you to confirm. The first time, Android asks you
to let Dens install apps, as it asked of your browser: turn on **Allow from
this source** for Dens.

## Google's developer verification

Dens isn't in Google Play, and its developers haven't registered with Google,
since installing Dens would then depend on Google's say-so. Google is starting
to make phones check that an app from outside its store comes from a developer
who registered with it: in Brazil, Indonesia, Singapore and Thailand since
September 30, 2026, and Google plans it everywhere in 2027.
[Keep Android Open](https://keepandroidopen.org/) campaigns against it: 71
organizations, among them the Tor Project, KDE and LineageOS, signed its open
letter, and it asks developers not to sign up.

On a phone that checks, Android installs Dens only through its **advanced
flow**, which you set up once, in **Developer options**. It makes you wait a
day, then asks how long to allow apps from unverified developers: choose **for
good**, since with 7 days, Dens's updates fail once they run out.

Phones without Google's certification, such as those running GrapheneOS or
LineageOS, check none of this.

## Uninstalling

Uninstall Dens as you would any app. That deletes everything it keeps on the
phone, its keys among them, and Android's backups and its transfers to a new
phone leave Dens's data out.

Your accounts in your dens stay: on a new phone, or a computer, install Dens
and sign in as another device, with your den password or a recovery code
([Usage](../../usage/)).

---

{{% details title="To check the app first" closed="true" %}}

Check it on a computer before you install it. That proves it's the app Dens's
release process built, and not a copy someone swapped in on the way. You need
[cosign](https://docs.sigstore.dev/cosign/system_config/installation/) for it.
Dens checks every update this way by itself.

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
