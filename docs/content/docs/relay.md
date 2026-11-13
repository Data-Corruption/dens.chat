---
title: Relay
weight: 3
---

Relay makes your own devices on one network work together. Push to talk from
whichever keyboard you're at, into the call another of your devices holds, or
from your phone's call button. Or play one computer's sound on another
device, like the headphones paired to your phone.

## What it does

- **Keybind relay**: your voice keys, push to talk, toggle mute, push to mute
  and toggle deafen, act in the call another of your devices holds. On a
  phone, the call's button does it, under the name of the device in the call.
- **Audio relay**: everything a computer plays, played on another of your
  devices, one at a time. The other device listens from its settings. It
  plays as media on a phone, at the media volume, with its screen off too.

Turn them on in **Settings**, under **Relay**. Keybind relay goes on at both
ends: the device in the call and the one you press keys on. **Share this
computer's sound** goes on at the computer whose sound you want, and then
**Listen** beside it on the other device starts playing it. A bar on both
devices shows while it plays, with **Stop**.

## Two setups

**Your computer's sound in your phone's headphones.** Turn on **Share this
computer's sound** on the computer, and press **Listen** beside it on the
phone. To hear it only on the phone, mute the computer rather than turning
it down: Windows and most Linux desktops keep sending while muted, but
Windows sends silence at volume 0.

**Two computers behind a KVM, in a call on one.** Turn on Keybind relay on
both, and on your phone if you'd like its button too. Whichever keyboard
you're at reaches the call. To hear the call on the computer you sit at,
share the other's sound and listen to it there.

## One call at a time

You're in a call on one device at a time. Join from another, and the call
moves there, and the keys of your other devices follow it.

<figure style="display: flex; flex-wrap: wrap; justify-content: center; gap: 1.5rem 2rem; margin: 1.5rem 0">
<svg viewBox="0 0 300 214" style="width: min(100%, 15.5rem)" role="img" aria-label="In the call on the desktop: the laptop's keys and the phone's button reach the desktop.">
  <defs>
    <marker id="relay-arrow-a" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
      <path d="M0 0 L10 5 L0 10 z" fill="currentColor"/>
    </marker>
  </defs>
  <g fill="none" stroke="currentColor" stroke-width="1.5">
    <rect x="90" y="34" width="120" height="50" rx="8" stroke-width="3" fill="currentColor" fill-opacity="0.08"/>
    <rect x="6" y="156" width="120" height="50" rx="8"/>
    <rect x="174" y="156" width="120" height="50" rx="8"/>
    <line x1="66" y1="154" x2="118" y2="90" marker-end="url(#relay-arrow-a)"/>
    <line x1="234" y1="154" x2="182" y2="90" marker-end="url(#relay-arrow-a)"/>
  </g>
  <g fill="currentColor" font-size="15" text-anchor="middle">
    <text x="150" y="18" font-weight="600">In the call on your desktop</text>
    <text x="150" y="57" font-weight="600">Desktop</text>
    <text x="150" y="75" font-size="13" fill-opacity="0.75">in the call</text>
    <text x="66" y="186">Laptop</text>
    <text x="234" y="186">Phone</text>
    <text x="84" y="126" text-anchor="end" font-size="13">keys</text>
    <text x="216" y="126" text-anchor="start" font-size="13">button</text>
  </g>
</svg>
<svg viewBox="0 0 300 214" style="width: min(100%, 15.5rem)" role="img" aria-label="Then joined from the laptop: the call is on the laptop, and the desktop's keys and the phone's button reach it.">
  <defs>
    <marker id="relay-arrow-b" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
      <path d="M0 0 L10 5 L0 10 z" fill="currentColor"/>
    </marker>
  </defs>
  <g fill="none" stroke="currentColor" stroke-width="1.5">
    <rect x="90" y="34" width="120" height="50" rx="8" stroke-width="3" fill="currentColor" fill-opacity="0.08"/>
    <rect x="6" y="156" width="120" height="50" rx="8"/>
    <rect x="174" y="156" width="120" height="50" rx="8"/>
    <line x1="66" y1="154" x2="118" y2="90" marker-end="url(#relay-arrow-b)"/>
    <line x1="234" y1="154" x2="182" y2="90" marker-end="url(#relay-arrow-b)"/>
  </g>
  <g fill="currentColor" font-size="15" text-anchor="middle">
    <text x="150" y="18" font-weight="600">Then joined from the laptop</text>
    <text x="150" y="57" font-weight="600">Laptop</text>
    <text x="150" y="75" font-size="13" fill-opacity="0.75">in the call</text>
    <text x="66" y="186">Desktop</text>
    <text x="234" y="186">Phone</text>
    <text x="84" y="126" text-anchor="end" font-size="13">keys</text>
    <text x="216" y="126" text-anchor="start" font-size="13">button</text>
  </g>
</svg>
</figure>

## What you need

- Your devices on the same network, at home or at work. Relay doesn't cross
  the internet, or from one network to another: a virtual machine behind a
  network of its own counts as another.
- Each device signed in to the same account in a den running Dens 1.12.0 or
  later, and able to read your DMs there: one you signed in to with a
  recovery code needs your DM recovery key first.
- On Android 17, Dens asks to reach devices on your network the first time you
  turn on Keybind relay or press **Listen**. If you said no, allow it for Dens
  in Android's settings, under **Apps**.
- On the Flatpak, version 1.12.0 asks for one more permission, to play and
  record sound, which it reads your computer's sound through. Dens can't
  install that update itself: update through your software center, which
  marks it "Requires additional permissions", or with `flatpak update`.

## Good to know

- Sound arrives about a tenth of a second late on a computer, a little more
  on a phone's speaker, and nearly half a second on Bluetooth headphones,
  most of it the headphones'. A video on the computer will be out of step
  with its sound on them.
- **Buffer**, under Relay, trades delay for smoothness: **High** rides out a
  busy Wi-Fi, and is what a phone uses until you choose.
- Joining a call on your phone while it plays a computer's sound moves that
  sound into the call. On Bluetooth headphones that means a phone call's
  quality until the call ends.
- On the phone we measured, playing a computer's sound with the screen off
  used about 5% of its battery an hour.
- A phone doesn't share its own sound, and no device relays your microphone.

## What the den learns

A den you're in introduces your devices to each other, passing notes between
them that only your devices can open. It learns that two of your devices
spoke, and when, but not what they said, nor your devices' addresses on your
network. The keys and the sound go straight between your devices, encrypted,
and never pass through the den.
