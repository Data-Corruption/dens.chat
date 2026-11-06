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

{{% details title="To check it wasn't tampered with (optional)" closed="true" %}}

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

Open Dens from your apps. The first time, it asks you to set a local password,
as any install does. Then [join a den](../join/) with your invite, or sign in
to one you're in already, as another device.

{{% /steps %}}

## Getting around

A den on your phone is three screens side by side: its channel list, the
channel you're reading, and its members. Swipe sideways to move between them,
starting a little in from the edge of the screen: from the very edge, Android
takes it for Back. Hold your finger on a message to reply to it, edit it, copy
it or delete it.

{{% details title="Back, buttons and Dens's home" closed="true" %}}

The buttons at the top of the channel go to the other screens too: ☰ for the
list, and the people button for the members.

Back closes what's open, like the settings or a photo, and otherwise goes up
a step: from the members to the channel, from the channel to the list, and
from the list to Dens's home, which lists your dens. Back at home leaves
Dens, which keeps running: your messages and notifications still arrive, and
a call goes on.

Dens's home shows what's waiting in each den: a red number for mentions of
you and messages in your DMs, and a dot for anything else unread.

{{% /details %}}

{{% details title="Messages, photos and the settings" closed="true" %}}

If you're a moderator or the owner, hold your finger on a channel, or a group,
for its settings. In the message box, ↵ starts a new line, and the paper plane
beside the box sends. Where you see an ⓘ, or a small mark you're not sure of,
tap it to see what it means, and tap anywhere to put it away.

Tap a photo to see it full screen. Swipe sideways for the message's other
photos, and down to close it. Spread two fingers, or tap twice, to zoom in,
and drag to look around; tap twice again to see it whole.

The settings, and each den's Manage, list their sections: tap one to open
it, and Back goes back to the list. Smaller panels, like a channel's
settings, slide up from the bottom of the screen; swipe one down to close it.

{{% /details %}}

## Calls

Tap a voice channel in a den's list to join its call. A bar across the top of
the channel shows the call, and the round button at the bottom right, by your
thumb, mutes and unmutes you: it's struck through in red while you're muted,
and green while you're talking.

Encrypted calls need Android System WebView 141 or later. On an older one,
Dens says so as you join, with a link to update it.

{{% details title="The call's bar" closed="true" %}}

The lock says the call is end-to-end encrypted, and opens your code to compare
with the others in it; the pads open the den's soundboard, where a tap plays a
sound for the call; the microphone mutes you; the headphones stop you hearing
the call; the red phone leaves it; and a tap on the rest of the bar goes to the
channel list, where the call's other buttons and the people in it are.

{{% /details %}}

{{% details title="Holding the button to talk" closed="true" %}}

To hold the round button to talk instead, or to hold it to mute, pick **Push
to talk** or **Push to mute** under **The call's button** in Dens's settings,
**Voice & video**. With push to talk nobody hears you unless you're holding
it, so with the screen off they hear nothing.

{{% /details %}}

{{% details title="Watching someone's screen" closed="true" %}}

When someone shares their screen, tap **LIVE** beside their name in the
channel list to watch. Their screen shows across the top of the channel,
under the call's bar. Tap it to watch full screen, and turn the phone
sideways for more room; Back, or another tap, ends full screen. The ✕ stops
watching. If you watch more than one, the count on it, like **1/2**, goes to
the next. Dens on a phone can't share its own screen.

{{% /details %}}

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

For notifications of DMs and mentions, turn them on in Dens's settings
([Notifications and sounds](../join/#notifications-and-sounds)).

{{% details title="What it costs" closed="true" %}}

A notification says Dens is running, and Android may tuck that away. Staying
connected costs about 1% of the battery a night for each den, in our tests.

{{% /details %}}

## Updating

Dens checks for an update once a day. When it finds one, it says so in a
notification and on Dens's home: tap either, and Android asks you to confirm.

{{% details title="More on updates" closed="true" %}}

Before it tells you, Dens downloads the update, about 25 MB on whatever
connection the phone has, and checks that Dens's release process signed it.

The first time, Android asks you to let Dens install apps, as it asked of your
browser: turn on **Allow from this source** for Dens. Play Protect may warn
about an update as it did about the install: tap the arrow beside the warning,
then **Install anyway**.

You can turn the checks off in **Settings**, under **System**.

{{% /details %}}

## Google's developer verification

On a phone that checks Google's developer verification, Android installs Dens
only through its **advanced flow**, which you set up once, in **Developer
options**. When it asks how long to allow apps from unverified developers,
choose **for good**: with 7 days, Dens's updates fail once they run out.

{{% details title="Why, and which phones check" closed="true" %}}

Google is starting to make phones check that an app installed from outside its
store comes from a developer who registered with Google: in Brazil, Indonesia,
Singapore and Thailand since September 30, 2026, and Google plans it
everywhere in 2027. Dens's developers haven't registered, since that would
make installing Dens depend on Google's say-so.

The advanced flow makes you wait a day, then lets you allow apps from
unverified developers for 7 days or for good. If the 7 days run out, Dens's
notification says where to turn the flow back on.

Phones without Google's certification, such as those running GrapheneOS or
LineageOS, check none of this.

{{% /details %}}

## Removing it

Uninstall Dens as you would any app. That deletes everything it keeps on the
phone, its keys among them, and Android's backups and its transfers to a new
phone leave Dens's data out.

Your accounts in your dens stay: on a new phone, or a computer, install Dens
and sign in as another device, with your den password or a recovery code
([Join a den](../join/)).
