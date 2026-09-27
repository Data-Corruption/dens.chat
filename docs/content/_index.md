---
title: Dens
toc: false
---

Dens is self-hosted chat for small communities: text channels, DMs, voice and
screen share, running on a machine that you or a friend control. There is no
central service and no account with a company. Each den (a community) is its
own server, and your identity lives on your own computer.

{{< callout type="info" >}}
Dens is in early development and has no releases yet.
{{< /callout >}}

## Living-room privacy

A den is a closed door, not a bunker. It keeps conversations away from
platforms that mine or sell them, and from bulk data requests to a large
provider. Your history doesn't disappear when a company changes its terms.

It does not hide anything from the person who runs the den. The den owner can
read every message, DM and file, and Dens says so in the app. Messages are not
end-to-end encrypted. If you need protection from a determined investigator,
use a tool built for that threat, such as [Signal](https://signal.org/).

Dens also collects as little as it can. It doesn't write IP addresses to disk,
deletions remove data for real, and a den can set messages to expire.

## How it works

- **Every install is a client.** A small background service on your computer
  serves the chat page to your own browser at `127.0.0.1` and keeps your keys.
  You log in to each den with a key that never leaves your machine.
- **Any install can also host one den.** A den is sized for about 1,000
  members, 500 online at once, 10 people in voice and 2 screen shares.
- **You can leave.** Clients and dens can move to new machines from a backup,
  and nothing depends on a company staying friendly.

## Platforms

Dens runs on Linux and Windows 11, for both joining and hosting dens. Linux is
recommended because its service sandbox is stronger. It works in any modern
browser and is tested on LibreWolf, Ungoogled Chromium, Waterfox and Brave.
