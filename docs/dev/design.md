# Dens — Design Doc

Sep 25, 2026 · @Matthew Pombo

The design Dens builds toward, and how it works now. This part is the overview: goals, the privacy model, the processes and their listeners, pairing, networking, the deployment forms and the platform layer, and versions. Each other part is read on its own:

- [Keys and sign-in](design/auth.md): the vault and its key, backups and moving a den, the den's sign-in, devices and their approval.
- [The page](design/page.md): its parts, words and look, and custom themes; [the phone layout](design/phone.md) has its own part.
- [Members](design/members.md): roles, handing the den on, leaving and bans, structure, profiles, names and their styles, events and badges.
- [Messages](design/messages.md): text and replies, emoji, reactions, polls, @everyone, compact links and links to messages.
- [Sync](design/sync.md): retention, disappearing messages, presence, the message list, the client's cache and what changed while away.
- [Notifications and sounds](design/notifications.md).
- [Players and cards](design/embeds.md): link previews, YouTube's players and Steam's cards.
- [Files](design/files.md), and [the media module](design/media.md), which strips, previews and makes smaller copies of videos.
- [End-to-end encryption](design/e2ee.md): the DM seal, identities and codes, DM keys, group DMs, notes to self and sealed calls.
- [Calls](design/calls.md), and [shares, the soundboard and the SFU](design/sfu.md).
- [The helper and Relay](design/helper.md): global shortcuts, and a member's own devices working together.
- [Security hardening](design/security.md): the checklist, by area.

What crosses the wire between a client and a den, its routes, fields, codes, limits and timings, is the [protocol](protocol.md)'s to state; the design says why, and what the page does, and links there rather than restating it. Each deployment form has its own doc: [installed.md](installed.md), [flatpak.md](flatpak.md) and [android.md](android.md).

## Overview

Dens is a self-hosted chat app with text, voice and screen share, for Linux and Windows, with a Flatpak for the Linux distributions the installer refuses and an app for Android, built so small communities can talk without a platform mining or owning their conversations. One install hosts at most one den (community). Every install is also a client: a local Go service that serves the chat UI to the user's own browser on localhost.

### Goals

- One den per install, sized for about 1,000 members, 500 online, 10 in voice and 2 screen shares; its owner sets the limits for calls (see [Limits for calls](design/sfu.md#limits-for-calls)).
- No central service. Identity lives on the user's machine; dens are independent servers.
- Credible exit: users and dens can move machines, and nothing depends on a company staying friendly.
- Linux and Windows for both client and den, from one codebase with a small platform layer. Linux is the recommended platform because its service sandbox is stronger, and the docs say so.
- Works in any modern browser; each milestone is tested in Chromium and Firefox.
- Manageable self-hosting: storage limits members can see and manage, no email dependency.

### Non-goals

- End-to-end encryption for channels. The den, and so its owner, can read channel messages and files, and the docs say so. The exceptions are end-to-end encrypted: DMs, group DMs, notes to self, and calls and screen shares (see [End-to-end encryption](design/e2ee.md)).
- Protection from targeted investigations or state actors.
- macOS support, multi-tenancy, federation between dens.
- Custom permission systems, forums, threads, bots or other feature rabbit holes in v1.

## Privacy model

Dens offers living-room privacy: a closed door, not a bunker. The branding pillars are ambient privacy, local-first and credible exit.

| Protects against | Does not protect against |
| --- | --- |
| Platforms mining or monetizing conversations | The den owner, who can read channel messages and files, and sees who DMs whom and when |
| Data brokers and ad profiling | Members screenshotting or repeating things |
| Bulk data requests to a large provider | A compromised device (client or den) |
| History disappearing when a company changes its terms | A targeted investigation or state actor |
| Casual filesystem scanners and backup snoopers (local encryption) | Traffic analysis: the den sees member IPs |
| The den owner reading DMs, group DMs and notes to self and the files in them, or listening to calls and watching shares (all end-to-end encrypted) | Members who compare codes over a channel the den's owner controls, or with someone pretending to be the other member, and a den that hands out keys of its own to members who never compare |

The public docs include a short philosophy section: being able to say dumb things, joke, vent and make art without a permanent searchable record is part of being human. They recommend Linux over Windows, Ungoogled Chromium, and not posting identifying details for low-stakes use, and point to Signal, Qubes OS and similar tools for higher-stakes threat models, noting that none of them are bulletproof.

Design rules that follow from this:

- Collect less: no IP addresses written to disk by default, and Caddy access logs stay off.
- Delete for real: deletions remove rows, with SQLite `secure_delete` on.
- Let conversations evaporate: an optional den-wide message retention period.
- Say plainly who can read what: in onboarding and the docs, once, and on the page by marking what's end-to-end encrypted.

## Architecture

Dens is one static Go binary that always runs the client and optionally runs a den. It runs as a hardened system-level service under a dedicated account, never as the person's desktop user: a systemd unit on Linux and an SCM (Service Control Manager) service on Windows (see [installed.md](installed.md)). The Flatpak and the Android app are the exceptions: they join dens and host none, and run as the person, in their session or under the app (see [flatpak.md](flatpak.md) and [android.md](android.md)).

### Traffic paths

| Path | Transport | Carries |
| --- | --- | --- |
| Browser ↔ local service | HTTP + WebSocket on loopback, at `localhost` (client listener) | UI, local API, signaling relay |
| Local service ↔ remote den | HTTPS + WSS via the den's Caddy | Auth, messages, presence, files, signaling |
| Local service ↔ own den | HTTP + WebSocket to the den listener on 127.0.0.1 | The same, for the den this install hosts |
| Browser ↔ den SFU | UDP (DTLS-SRTP), TCP fallback | Voice and screen share media only |
| Local service ↔ the member's other installs | UDP (DTLS, SCTP and SRTP) on their local network, introduced through a den | Relay's voice keys and a computer's sound |
| CLI ↔ local service | Unix socket (Linux), named pipe (Windows) | Pairing tokens, admin commands, backups |
| Local service ↔ YouTube | HTTPS | A linked video's title and picture, for a member with YouTube's players on |
| Local service ↔ Steam | HTTPS | A linked game's card, for a member with Steam's cards on |
| Browser ↔ YouTube | HTTPS, in a sandboxed frame | YouTube's player, once that member plays a video |

The browser never talks to a den over HTTP. All den content reaches the page through the local service, so it only ever runs under the localhost origin.

The owner's client reaches its own den over loopback, not through the public domain. That works before DNS and Caddy are set up, and on routers that can't route to their own public address (no hairpin NAT). The den still proves its identity key at every login, so loopback loses no authentication. Once the den moves to another computer and this install removes its copy, its client reaches the den at its address, as any member's does (see [Backup, restore, move](design/auth.md#backup-restore-move)). The client-to-den protocol is specified in [protocol.md](protocol.md).

### Two listeners, not one

Keep the client listener and den listener separate. Caddy forwards remote requests from 127.0.0.1, so on a single listener every remote den user would also reach the local-only client API (vault, pairing, admin) unless routing is perfect. Separate listeners make that mistake impossible:

- **Client listener:** `127.0.0.1:<client-port>` and `[::1]:<client-port>`, fixed port, strict `Host` and `Origin` checks, only ever used by the local browser. It binds both loopbacks: with only IPv4 bound, another local user can bind `[::1]` on the same port and catch browsers that resolve `localhost` to IPv6.
- **Den listener:** `127.0.0.1:<den-port>`, only exposed through Caddy, serves only den routes. It is TCP on both platforms: Caddy runs as its own user and couldn't reach a socket inside the private state directory.

### Pairing the browser

- The desktop user runs `dens open`, and the CLI connects to the control endpoint.
- The service authorizes the caller by the identity the OS reports for the connection (`SO_PEERCRED` UID on Linux, the pipe client's token SID on Windows) against the one desktop user recorded at install. The installer records the sudo caller (`SUDO_UID`) on Linux and the signed-in console user on Windows, with a `--user` flag to override either. No group membership is involved, so pairing works straight after install without logging out and back in.
- On Linux the socket is connectable by any local user (mode 0666) and the peer check does the authorization, as the system D-Bus does. On Windows the pipe's DACL grants SYSTEM and the service SID full access and the recorded user read and write-data rights only (`0x12019b`). Never grant the user `GENERIC_WRITE` on the pipe: it includes `FILE_CREATE_PIPE_INSTANCE`, which lets the user add server instances and pose as the service. The service reads the caller's SID by impersonating it at identification level, which needs no `SeImpersonatePrivilege`.
- The service keeps a listening pipe instance open at all times, creating the next instance before serving the current one, so no other process can re-create the name between connections.
- The CLI verifies the other end too. On Linux the peer UID must be the service account's. On Windows the pipe's server process ID must match the one the SCM reports for `dens-<name>`, and the service creates the pipe with `FILE_FLAG_FIRST_PIPE_INSTANCE`, so it fails closed if another process claimed the name first.
- The service returns a one-time token and its client port. The CLI opens `http://localhost:<port>/#token=…` (`xdg-open` on Linux, `ShellExecute` on Windows).
- The page lives at `localhost`, not `127.0.0.1`: YouTube's player won't play for a page at an IP address (see [YouTube players](design/embeds.md#youtube-players)). Browsers resolve `localhost` to loopback themselves, never through DNS, and treat it as a secure context, as they do `127.0.0.1`. A browser that loads the page at `127.0.0.1` or `[::1]` as a document is redirected to `localhost`, keeping the path and, since browsers carry a fragment across a redirect, the pairing token. Scripts and tests, which don't load it as a document, get their answer where they asked.
- The page exchanges it for an `HttpOnly`, `SameSite=Strict` session cookie and clears the fragment. On the first pairing, the page then asks for a new local password before anything else (see [Local identity and encryption](design/auth.md#local-identity-and-encryption)).
- A paired session lasts 30 days from its last use; the service extends it at most once a day. Its cookie is named for the client port (`dens_session_<port>`), because cookies aren't scoped by port and each instance has its own.
- The port comes from the authenticated service while it holds that listener on both loopbacks. Neither platform lets another account bind the same address and port, so no separate port-ownership check is needed. On Windows another account can bind the wildcard address on that port, but loopback connections still reach the more specific socket.

### Files in and out

- Uploads come from the browser's file picker, drag and drop, or a paste; downloads use `Content-Disposition: attachment` so the browser saves them as the desktop user.
- The service never needs write access to the user's home directory, which removes the Downloads permission step from install.

### Third-party notices

The binary includes others' code: Go's standard library and modules such as Pion and the SQLite driver; FFmpeg, zlib and libaom in the media module; RNNoise; Preact, Tailwind and DaisyUI in the page; and Unicode's emoji data, which the page's and the den's list of emoji is made from. Most of their licenses ask that copies of the binary carry their notices, and the LGPL also asks for FFmpeg's source, which each release publishes.

- `scripts/notices.sh` gathers every license into one file that the binary embeds: those of the Go modules it links, from the module cache, and those of the inputs `scripts/vendor.sh` pins. The file is committed, so a new dependency shows up in review as a new license, and CI writes it again and fails if it differs.
- `dens licenses` prints it, and the page's settings link to it.

## Networking and deployment

A den needs a domain, Caddy, and four forwarded ports. Caddy handles only HTTP and WebSocket traffic; WebRTC media must reach the den directly.

| Port | Protocol | Purpose | Required |
| --- | --- | --- | --- |
| 80 | TCP | ACME certificate challenges (Caddy) | Yes |
| 443 | TCP | HTTPS and WSS to the den listener (Caddy) | Yes |
| Media port, default 7881 | UDP | WebRTC media via UDP mux | Yes, for voice |
| Fallback port, default 7882 | TCP | ICE-TCP media fallback | Recommended |

The docs list which ports to forward but not how, since routers vary too much. They mention a small VPS as the easy alternative when forwarding isn't possible.

Members' calls reach the den at the addresses its name resolves to (see [Media addresses](design/sfu.md#media-addresses)), so the name must point at the den's machine or the router forwarding to it. The owner's own calls go to the machine's own addresses and work before any port is forwarded, so the guide has owners test a call from outside their network.

On Windows, the installer adds inbound Windows Firewall rules for the media ports at every install, scoped to `dens.exe` and those ports, and removes them on uninstall. Nothing listens there until the owner turns hosting on, so until then they let nothing in. The Caddy guide covers ports 80 and 443 on both platforms.

### Caddy

- The guide ships a minimal Caddyfile: the den domain reverse-proxies to the den listener.
- Access logging stays off (Caddy's default), in line with collecting less.
- A second den on the same machine is another instance with its own ports and a second site block for its domain or subdomain.
- Caddy runs as a service on both platforms: its packaged systemd unit on Linux, and on Windows as an SCM service, which Caddy supports natively (`sc.exe create caddy binPath= "…\caddy.exe run --config …"`).
- Reloading or restarting Caddy closes den WebSockets (close 1001). Clients resume without losing events.
- Distributions' packages of Caddy lag (Debian 13 ships 2.6), so the den e2e runs the Caddy `scripts/vendor.sh` pins.

### Den-side trust of proxy headers

- The den listener trusts `X-Forwarded-For` only from loopback, and uses it for rate limiting in memory only.

## Platform support

Dens supports Linux and Windows for both the client and the den. Linux is recommended. A Flatpak brings the client to other Linux distributions, and an Android app to phones; neither hosts a den.

### Deployment forms

Besides the system, Dens differs by how it's deployed: installed (the service under its own account), development (as the developer, with a plaintext key), the Flatpak (as the member, in their session) and Android (under the app's shell). `layout.Form` names the form, and `app.FormOf` chooses it at runtime: on Android (`GOOS=android`) Android, for a development build too, since the app's shell runs either; elsewhere a development build is Development, a sandbox whose `/.flatpak-info` and `FLATPAK_ID` name `chat.dens.Dens` the Flatpak, and anything else Installed, so the Flatpak runs the release binary as it is. Each seam that differs by form takes it, as a `switch` on the form rather than a type per form, since there are few forms and each differs from the others in a few lines: where the paths are, how the data key unwraps, whether the instance checks the release host for updates, which an installed one with a release host does, and the Android app, whose release names its host (`App.ChecksForUpdates`), the settings' update options with it, what the update notice says and what installs the update, how the CLI checks the service it talks to and what it advises when none is running, whether the helper runs inside the service, and whether the instance may host. A form `layout` doesn't know has no paths, so no process runs as one. Forms other than Installed are session forms (`Form.Session`): they run as their user with no service manager, write their own instance config and lifecycle state, and take no maintenance command but `restore`, which the Android app refuses too, since its shell would have to wrap the backup's key. As with a platform difference, a form never reaches the protocol or the database, and reaches the page only as what it can do.

| Form | Runs as | Its data key | Installed and updated by | Hosts a den | Its doc |
| --- | --- | --- | --- | --- | --- |
| Installed | A service under its own account: a systemd unit on Linux, an SCM service on Windows | Bound to the host: `systemd-creds` or DPAPI | Its maintenance commands, which the installer scripts run | Yes | [installed.md](installed.md) |
| Development | The developer, from a terminal | A plaintext file | The developer's own builds | Yes | [lifecycle.md's Development instances](lifecycle.md#development-instances) |
| The Flatpak | The member, in their session | None that opens without them, unless they ask it to remember | Flatpak | No | [flatpak.md](flatpak.md) |
| Android | The app's shell, as its child process | A Keystore key | Android's installer: the APK from the release host, then each update the app's updater checks | No | [android.md](android.md) |

### Platform layer

OS-specific runtime code lives in `internal/platform/host`, each seam a small function pair in `_linux.go` and `_windows.go`, and install-time steps sit behind `internal/install`'s `System`. Beyond those two packages, a package that owns a path, a lock or a log file holds its own files for each system (`layout`, whose checks of a private directory want its owner and mode 0700 on Linux and its owner and a protected DACL naming only the allowed SIDs on Windows, `maintenance`, `pkg/xsyscall` and `pkg/xlog/rlog`), and `pkg/sdnotify` stubs systemd's protocol out on Windows. Everything above them (listeners, `Host` and `Origin` checks, rendering, auth, crypto, rate limits, the SFU and the database) is shared code with no build tags.

| Seam | Linux | Windows |
| --- | --- | --- |
| Service host | sd_notify, `SIGTERM` | `x/sys/windows/svc` handler |
| Starting, stopping and querying a service | `systemctl` | The SCM |
| Host-bound data key, or a development instance's plain one | Read `$CREDENTIALS_DIRECTORY` | DPAPI unprotect |
| Control endpoint | Unix socket, `SO_PEERCRED` | Named pipe on `x/sys/windows` (no third-party module), client SID by identification-level impersonation, server PID check |
| Accounts: this process's, and one looked up by name | The user database | The account's SID |
| Locked key memory | `mlock` | `VirtualLock` |
| Low-priority child process | nice 19 and, where the sandbox allows it, `SCHED_IDLE` on the forking thread, set with `sched_setscheduler`, whose policy systemd's `RestrictRealtime=` can read, since it refuses `sched_setattr` | `IDLE_PRIORITY_CLASS` |
| Open a URL | `xdg-open` | `ShellExecute` |
| Free disk space | `statfs` | `GetDiskFreeSpaceEx` |
| A refused connection | `ECONNREFUSED` | `WSAECONNREFUSED` |
| A newer binary in this process's place, as the helper takes up an update | `exec`, keeping the session and the unit | A detached process of its own, as this one exits |
| Keys for the helper | The global shortcuts portal under Wayland, `XQueryKeymap` under X11 | `GetAsyncKeyState` |
| Notifications for the helper | The freedesktop notification service, or from the Flatpak, the Notification portal | Toasts, through Windows Runtime, with the Start menu shortcut they need |
| The computer's sound, for Relay | The default output's monitor, over PulseAudio's protocol | The default output's loopback, through WASAPI |
| The Flatpak's portals | Secret, Background and Notification, Flatpak's own `Spawn` and update monitor, and the app's name on the session bus | None |

Go builds `_linux.go` files for Android too, so where Android differs, an `_android.go` file takes the seam's place and its Linux one says `!android`: locked memory falls back to ordinary memory past the 64 KiB Android lets an app lock, the host-bound data key comes from the app's shell on standard input, as an installed service's comes from systemd, and the computer's sound is never captured, since a phone plays Relay's sound and sends none. Every other seam works on Android as on Linux, the control endpoint in the app's files among them.

The control endpoint stays up whatever its clients do. On Windows, a pipe instance whose client connected and closed before the service's `ConnectNamedPipe` reports `ERROR_NO_DATA`; the listener disconnects that instance and waits on it again, so it keeps holding the pipe's name, as go-winio does. The control server waits a moment and accepts again after any error but its listener closing, as `net/http` does, and serves at most 64 connections at once. On Linux it refuses another account's connection before reading its request, and since that can close the socket before the request is written, the client reads the service's answer even when sending fails.

Everything else that differs is install-time: the two installers, the unit file and SCM registration, account creation, ACLs and firewall rules, and how the helper starts at login: a scheduled task, or an autostart entry and a `.desktop` file.

Rules that keep the two implementations from drifting:

- A platform difference never reaches the protocol, database or HTTP layers. A feature that seems to need one is a design problem.
- The host package's tests run on both CI runners.
- Windows code compiles only under `GOOS=windows`, and Android's under `GOOS=android`; vet and build-test them on every change that touches them. Android's seams have their tests run in the emulator, by the Android e2e.

## Versioning and compatibility

Clients and dens negotiate an integer protocol version, separate from the release version, so most client updates never force a den update.

- Each release speaks a range of versions, from `denproto.MinVersion` to `denproto.Version`, and serves a client that speaks any of them.
- The client sends `Dens-Protocol` with its newest on every request and the WebSocket upgrade; the den replies with its own range (see [Versioning](protocol.md#versioning)).
- If the ranges overlap, both use the highest shared version: a client whose request a den refuses as newer than it speaks asks again, once, at the den's newest, and goes at that until a response's range says the den speaks a newer one, as once it updates. A socket keeps the version it opened at, so one whose upgrade's answer shows another shared version, as a den's that updated since the client last heard from it, opens again at that one, once, rather than going without group DMs until it next reconnects. If not, the client says the den needs a newer or older Dens.
- Version 2 added group DMs, and version 3 notes to self: each a kind of channel whose channels, messages and events an older client would take for the den breaking the protocol. Each kind of channel comes with a version, and a den gives a request or socket only the kinds its version has.
- Protocol bumps only for breaking changes; additive fields and message types don't bump it, and unknown message types are ignored.
- Keep `MinVersion` at least one version behind `Version`, so a den can lag one breaking release without locking members out.

### Data across versions

Installed copies hold data that every later version reads, so formats move forward rather than change in place:

- Database migrations go at the end, and a released step never changes. A step that remakes a table, as SQLite changes a table's checks, runs with foreign keys off, as SQLite's procedure for it says, since dropping the old table with them on would delete every row that refers to it; SQLite can't turn them off within a transaction, so the migrator turns them off on its connection for the whole run that includes such a step, checks every foreign key before it commits, and turns them on again. The steps, and the first release with each:

  | Step | Adds | First in |
  | --- | --- | --- |
  | 1 | The initial schema | v1.0.0 |
  | 2 | Each member's own sounds | v1.7.0 |
  | 3 | The soundboard: the den's named files and its limits for them, the first columns added to a table, with defaults a den made before them takes | v1.7.0 |
  | 4 | The den's limit for emoji, a column whose default an older den takes | v1.8.0 |
  | 5 | Reactions: a channel's, and a DM's sealed sets, which start empty | v1.8.0 |
  | 6 | Polls and their votes, which start empty, and a message's mark of staff's @everyone, whose default, unmarked, an older message takes | v1.8.0 |
  | 7 | Identities | v1.9.0 |
  | 8 | Group DMs, with `den_channels` made again, its counter kept where it was | v1.9.0 |
  | 9 | Disappearing messages: each DM's and group's time, Off, and who set it, none, and each message's time to go, none | v1.9.0 |
  | 10 | Spoilers: a channel's file's mark, which an older file goes without | v1.10.0 |
  | 11 | Notes to self, with `den_channels` made again, to take a fifth kind and the member whose notes it holds | v1.10.0 |
  | 12 | Events and who's interested, which start empty, and the den's limit for members' events, whose default, 0, an older den takes | v1.10.0 |
  | 13 | Badges and their awards, which start empty, and an event's staff-only mark, whose default, everyone's, an older event takes | v1.10.0 |
  | 14 | An award's hidden mark, whose default, shown, an older award takes: a step of its own, since test builds ran the thirteenth | v1.10.0 |
  | 15 | The chat background, a table that starts empty | v1.11.0 |
  | 16 | A member's name style, a column that starts empty | v1.11.0 |
  | 17 | A voice channel's longest call, a column of `den_channels` that starts empty and that only a voice channel takes | v1.13.0 |

  A test makes each released schema by running its steps alone, v1.6.0's, v1.7.0's, v1.8.0's, v1.9.0's, v1.10.1's and v1.12.0's, puts a row in every table, migrates it, and a backup made of it, and finds every row and table counter as it was, in every column the release had, the new columns at their defaults.
- `instance.json` and `state.json` decode strictly, so every field a release wrote stays readable, and a change of meaning gets a new field rather than an old one's name. An older binary never meets a newer file, since downgrades are refused.
- A backup from any release restores, and migrates as the service starts.
- A change to one of these comes with a test that reads its released form.
- The lifecycle harnesses install the first release, v1.0.0, host a den with it and update to the build under test, which must serve the den as it was; the release host keeps v1.0.0 for good. The Linux harness then checks the data later steps made, and a DM v1.0.0 opened (see [Testing the lifecycle](lifecycle.md#testing-the-lifecycle)).

An install that is both client and den still updates as one binary. If that blocks someone, the documented answer is to move the den to its own install.
