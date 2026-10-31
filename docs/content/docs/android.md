---
title: Android
weight: 2
---

Dens for Android runs on your phone: chat, DMs and calls, which keep going
with the screen off. It's Dens itself, running on the phone as it runs on a
computer, so the phone keeps its own keys. It can't host a den.

It needs Android 12 or later. It isn't in Google Play or any other store: you
download it from Dens's release site, and it updates itself from there,
asking you before each update.

## Install it

{{% steps %}}

### Download it

In your phone's browser, open
[releases.dens.chat/dens.apk](https://releases.dens.chat/dens.apk). The browser
warns that the file might be harmful, as it does for any app from outside a
store. Download it anyway.

{{% details title="To verify it wasn't tampered with" closed="true" %}}

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

### Let the browser install apps

Open the download. The first time, Android says your phone isn't allowed to
install unknown apps from this source. Tap **Settings**, turn on **Allow from
this source**, and go back.

On a newer Galaxy phone, Samsung's **Auto Blocker** blocks it instead. Turn it
off in **Settings**, under **Security and privacy**, then **Auto Blocker**, and
leave it off: while it's on, Dens can't install its updates either.

### Install it

Android asks whether to install Dens: tap **Install**. Play Protect may offer
to scan it first, which sends the app's details to Google. Scanning it is up
to you. Scanned, it may say the app may be harmful, without saying why: to
install it anyway, tap the arrow beside the warning, then **Install anyway**.

### Open Dens

Open Dens from your apps. The first time, it asks you to set a local password,
as any install does. Then [join a den](../join/) with your invite, or sign in
to one you're in already, as another device.

{{% /steps %}}

## Getting around

A den on your phone is three screens side by side: its channel list, the
channel you're reading, and its members. Swipe sideways to move between them.
Start a swipe a little in from the edge of the screen: from the very edge,
Android takes it for Back. The buttons at the top of the channel go there
too: ☰ for the list, and the people button for the members.

Back closes what's open, like the settings or a photo, and otherwise goes up
a step: from the members to the channel, from the channel to the list, and
from the list to Dens's home, which lists your dens. Back at home leaves
Dens, which keeps running: your messages and notifications still arrive, and
a call goes on.

Dens's home shows what's waiting in each den: a red number for mentions of
you and messages in your DMs, and a dot for anything else unread.

Hold your finger on a message to reply to it, edit it, copy it or delete it.
In the message box, ↵ starts a new line, and the paper plane beside the box
sends. Where you see an ⓘ, or a small mark you're not sure of, tap it to see
what it means, and tap anywhere to put it away.

## Keeping it connected

Dens keeps your dens connected while it isn't on screen, which is what brings
your messages and notifications while the phone sleeps. A notification says
it's running, and Android may tuck that away. It costs about 1% of the battery
a night for each den, in our tests.

Two settings stop it:

- **Battery set to Restricted.** Android cuts Dens off as soon as it leaves
  the screen, and nothing arrives until you open it again. Leave Dens at
  **Optimized** or **Unrestricted**: in **Settings**, **Apps**, **Dens**, then
  **Battery**.
- **Galaxy phones put apps to sleep** that they think you don't use. Put Dens
  among **Never auto sleeping apps**: in **Settings**, **Battery**, then
  **Background usage limits**.

For notifications of DMs and mentions, turn them on in Dens's settings
([Notifications](../join/#notifications)).

## Updating

Dens checks for an update once a day. When it finds one, it downloads it,
about 25 MB on whatever connection the phone has, checks that Dens's release
process signed it, and says so in a notification and on Dens's home. Tap
either, and Android asks you to confirm. The first time, Android asks you to
let Dens install apps, as it asked of your browser: turn on **Allow from this
source** for Dens. Play Protect may warn about an update as it did about the
install: tap the arrow beside the warning, then **Install anyway**.

You can turn the checks off in **Settings**, under **System**.

## Google's developer verification

Google is starting to make phones check that an app installed from outside its
store comes from a developer who registered with Google: in Brazil, Indonesia,
Singapore and Thailand since September 30, 2026, and Google plans it
everywhere in 2027.
Dens's developers haven't registered, since that would make installing Dens
depend on Google's say-so. On a phone that checks, Android installs Dens only
through its **advanced flow**, which you set up once, in **Developer options**:
it makes you wait a day, then lets you allow apps from unverified developers
for 7 days or for good. Choose **for good**: with 7 days, Dens's updates fail
once they run out, and its notification says where to turn the flow back on.

Phones without Google's certification, such as those running GrapheneOS or
LineageOS, check none of this.

## Removing it

Uninstall Dens as you would any app. That deletes everything it keeps on the
phone, its keys among them, and Android's backups and its transfers to a new
phone leave Dens's data out. Your accounts in your dens stay: on a new phone,
or a computer, install Dens and sign in as another device, with your den
password or a recovery code ([Join a den](../join/)).
