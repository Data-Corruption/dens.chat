# Dens — Design Doc

Sep 25, 2026 · @Matthew Pombo

## Overview

Dens is a self-hosted chat app for Linux and Windows with text, voice and screen share, built so small communities can talk without a platform mining or owning their conversations. One install hosts at most one den (community). Every install is also a client: a local Go service that serves the chat UI to the user's own browser on localhost.

**Goals**

- One den per install, sized for about 1,000 members, 500 online, 10 in voice and 2 screen shares.
- No central service. Identity lives on the user's machine; dens are independent servers.
- Credible exit: users and dens can move machines, and nothing depends on a company staying friendly.
- Linux and Windows for both client and den, from one codebase with a small platform layer. Linux is the recommended platform because its service sandbox is stronger, and the docs say so.
- Works in any modern browser; tested on LibreWolf, Ungoogled Chromium, Waterfox and Brave.
- Manageable self-hosting: storage limits, automatic media compression, no email dependency.

**Non-goals**

- End-to-end encryption. The den owner can read everything, and the docs say so.
- Protection from targeted investigations or state actors.
- macOS support, multi-tenancy, federation between dens.
- Custom permission systems, forums, threads, bots or other feature rabbit holes in v1.

## Privacy model

Dens offers living-room privacy: a closed door, not a bunker. The branding pillars are ambient privacy, local-first and credible exit.

| Protects against | Does not protect against |
| --- | --- |
| Platforms mining or monetizing conversations | The den owner, who can read all messages, DMs and media |
| Data brokers and ad profiling | Members screenshotting or repeating things |
| Bulk data requests to a large provider | A compromised device (client or den) |
| History disappearing when a company changes its terms | A targeted investigation or state actor |
| Casual filesystem scanners and backup snoopers (local encryption) | Traffic analysis: the den sees member IPs |

The public docs include a short philosophy section: being able to say dumb things, joke, vent and make art without a permanent searchable record is part of being human. They recommend Linux over Windows, LibreWolf, and not posting identifying details for low-stakes use, and point to Signal, Qubes OS and similar tools for higher-stakes threat models, noting that none of them are bulletproof.

Design rules that follow from this:

- Collect less: no IP addresses written to disk by default, and Caddy access logs stay off.
- Delete for real: deletions remove rows, with SQLite `secure_delete` on.
- Let conversations evaporate: an optional den-wide message retention period.
- Say plainly who can read what, in the UI as well as the docs.

## Architecture

Dens is one static Go binary, forked from Sprout, that always runs the client and optionally runs a den. It runs as a hardened system-level service under a dedicated account, never as the person's desktop user: a systemd unit on Linux and an SCM (Service Control Manager) service on Windows.

**Traffic paths**

| Path | Transport | Carries |
| --- | --- | --- |
| Browser ↔ local service | HTTP + WebSocket on 127.0.0.1 (client listener) | UI, local API, signaling relay |
| Local service ↔ remote den | HTTPS + WSS via the den's Caddy | Auth, messages, presence, files, signaling |
| Local service ↔ own den | HTTP + WebSocket to the den listener on 127.0.0.1 | The same, for the den this install hosts |
| Browser ↔ den SFU | UDP (DTLS-SRTP), TCP fallback | Voice and screen share media only |
| CLI ↔ local service | Unix socket (Linux), named pipe (Windows) | Pairing tokens, admin commands, backups |

The browser never talks to a den over HTTP. All den content reaches the page through the local service, so it only ever runs under the localhost origin.

The owner's client reaches its own den over loopback, not through the public domain. That works before DNS and Caddy are set up, and on routers that can't route to their own public address (no hairpin NAT). The den still proves its identity key at every login, so loopback loses no authentication. The client-to-den protocol is specified in [protocol.md](protocol.md).

**Client UI.** The page is a Preact app (about 10 KB) built by the pinned esbuild into one hashed bundle. Preact injects no scripts or styles at runtime, so the CSP stays `script-src 'self'` with no inline code. WebSockets, both den and local, use `github.com/coder/websocket`: small, context-first and without dependencies, since the standard library has none.

**Install and service account**

| | Linux | Windows |
| --- | --- | --- |
| Service | Templated system unit `dens@<name>.service` | SCM service `dens-<name>`, own process |
| Account | System user `dens-<name>` via sysusers.d, UID allocated by sysusers | Virtual account `NT SERVICE\dens-<name>` (no password, managed by the SCM) |
| Binary | `/usr/local/bin/dens`, root-owned | `C:\Program Files\Dens\dens.exe`, on the system `PATH` |
| State | `/var/lib/dens/<name>/` | `C:\ProgramData\Dens\<name>\` |
| Control endpoint | `/run/dens/<name>/control.sock` | `\\.\pipe\dens-<name>-control` |
| Stop | `SIGTERM` from systemd | SCM stop control |
| Readiness | `Type=notify`, `READY=1` | `SERVICE_RUNNING` status |
| Restart on crash | `Restart=on-failure` | SCM recovery actions |

- A system-level service, not `systemd --user` or a per-user Scheduled Task, keeps Dens running without a login, gives it its own account to sandbox, and gives both platforms a real graceful stop. On Linux it also enables `LoadCredentialEncrypted=` (see Local identity).
- Several installs per machine are separate instances (`dens@<name>` or `dens-<name>`), each with its own account, ports and storage. This is the advanced path for a second den.
- Each instance's ports are fixed at install and recorded in its config. `main` uses the defaults (client 8484, den listener 8485, media 7881/UDP and 7882/TCP); other instances pass theirs to the installer, which refuses ports already in use.
- The den role is an install option (`--den`). Turning it on later means rerunning the installer with `--den`, which records the den ports and, on Windows, adds the firewall rules. Creating the den itself (its identity key and owner account) then happens in the app.
- Sprout's signed releases, SQLite layer and `state.json` lifecycle phases carry over (see Service lifecycle).

**Privileged steps and updates**

Root or Administrator is used only for install, update, uninstall and restore; the service itself always runs unprivileged and sandboxed.

- The installer scripts (`install.sh`, `install.ps1`) only download a release, verify its cosign signature and checksums, and run the release binary's `install` command. Every privileged change is made by the binary's maintenance commands (`install`, `update`, `restore`, `uninstall`), so both platforms share one transaction engine (`internal/install`) and the scripts stay short enough to read before running them.
- Privileged steps: create the service account, install the binary, register the service, create the state directory with private permissions, record the desktop user allowed to pair, and generate and host-wrap the data key. On Windows they also add the binary to the system `PATH`, exclude it from Windows Error Reporting, and add firewall rules when the den role is enabled.
- Every maintenance command prints its plan before changing anything, and `--dry-run` (`-DryRun` on Windows) prints the plan and stops. Output goes to the terminal and to the instance's `control/maintenance.log`.
- Docs tell users to download and verify the cosign-signed installer (`install.sh` or `install.ps1`) before running it, never `curl | sudo bash` or `irm | iex`.
- Updates are manual: `sudo dens update` on Linux, or `dens update` from an elevated terminal on Windows (`sudo dens update` also works where Windows' built-in sudo is enabled). It downloads the latest installer and its cosign bundle, verifies them against the signing identity baked into the binary, and runs the installer in the foreground. There is no unattended auto-update.
- The service checks for new releases and shows an in-app notice with the update command; it never installs anything itself. Update checks can be turned off in settings, and the privacy docs state that a check only asks the release host for the latest version number, which reveals the install's IP to that host.

**Service lifecycle**

Only the service process opens the storage root. The root is private to the service account, so every command a desktop user runs (`dens open`, `dens backup`, admin commands) goes through the control endpoint, and every root or Administrator command is a maintenance transaction. With one process per instance and a service manager that stops it gracefully on both platforms, install, update, restore and uninstall follow the same steps on Linux and Windows:

1. The command takes `operation.lock` for each instance it touches, in name order, and publishes a transitional phase (`installing`, `updating`, `restoring` or `uninstalling`) and the target version in `state.json`. Every instance on a machine runs the one installed binary, so install and update move all of them through the transition.
2. It stops each service through the service manager and waits. systemd escalates to `SIGKILL` after `TimeoutStopSec`; on Windows the command runs `taskkill /F` on the process ID the SCM reports after the same timeout. (An elevated administrator can't open a virtual-account service's process for termination without the debug privilege; `taskkill /F` and `Stop-Process -Force` enable it themselves.)
3. It takes `lifecycle.lock` exclusively. The service holds that lock exclusively for its whole run, so acquiring it proves the service is down, and the same lock stops a second copy of the service from starting.
4. It changes the installation, journaling how to undo each step. A failure up to here undoes the journal and restores the previous state, including restarting services that were running.
5. It starts each service. A service whose state is transitional and names its version migrates its data before reporting ready; the command then publishes `ready`. An instance that was stopped before the transaction runs only long enough to migrate.

A service starts only when the phase is `ready` with its version, or a transition that targets its version. Otherwise it logs why and exits with status 78, which the unit's `RestartPreventExitStatus=` keeps systemd from retrying. On Windows it stops with a service-specific exit code, which the SCM treats as a stop rather than a crash, so recovery actions don't restart it.

Starting the services is the point of no return: a migrated database can't be opened by the previous binary, so a failure from here keeps the transitional state. The operator fixes the cause and runs the installer again, and the service finishes the migration when it starts.

Uninstall removes the instance's state directory, service registration, account and firewall rules. The binary, cosign and the unit template on Linux, or the `PATH` entry and error-reporting exclusion on Windows, go with the last instance. Windows can't delete a running executable, so an uninstall run from the installed `dens.exe` renames it and schedules the file for deletion at the next reboot.

**Two listeners, not one**

Keep the client listener and den listener separate. Caddy forwards remote requests from 127.0.0.1, so on a single listener every remote den user would also reach the local-only client API (vault, pairing, admin) unless routing is perfect. Separate listeners make that mistake impossible:

- **Client listener:** `127.0.0.1:<client-port>` and `[::1]:<client-port>`, fixed port, strict `Host` and `Origin` checks, only ever used by the local browser. It binds both loopbacks: with only IPv4 bound, another local user can bind `[::1]` on the same port and catch browsers that resolve `localhost` to IPv6.
- **Den listener:** `127.0.0.1:<den-port>`, only exposed through Caddy, serves only den routes. It is TCP on both platforms: Caddy runs as its own user and couldn't reach a socket inside the private state directory.

**Pairing the browser**

- The desktop user runs `dens open`, and the CLI connects to the control endpoint.
- The service authorizes the caller by the identity the OS reports for the connection (`SO_PEERCRED` UID on Linux, the pipe client's token SID on Windows) against the one desktop user recorded at install. The installer records the sudo caller (`SUDO_UID`) on Linux and the signed-in console user on Windows, with a `--user` flag to override either. No group membership is involved, so pairing works straight after install without logging out and back in.
- On Linux the socket is connectable by any local user (mode 0666) and the peer check does the authorization, as the system D-Bus does. On Windows the pipe's DACL grants SYSTEM and the service SID full access and the recorded user read and write-data rights only (`0x12019b`). Never grant the user `GENERIC_WRITE` on the pipe: it includes `FILE_CREATE_PIPE_INSTANCE`, which lets the user add server instances and pose as the service. The service reads the caller's SID by impersonating it at identification level, which needs no `SeImpersonatePrivilege`.
- The service keeps a listening pipe instance open at all times, creating the next instance before serving the current one, so no other process can re-create the name between connections.
- The CLI verifies the other end too. On Linux the peer UID must be the service account's. On Windows the pipe's server process ID must match the one the SCM reports for `dens-<name>`, and the service creates the pipe with `FILE_FLAG_FIRST_PIPE_INSTANCE`, so it fails closed if another process claimed the name first.
- The service returns a one-time token and its client port. The CLI opens `http://127.0.0.1:<port>/#token=…` (`xdg-open` on Linux, `ShellExecute` on Windows).
- The page exchanges it for an `HttpOnly`, `SameSite=Strict` session cookie and clears the fragment. On the first pairing, the page then asks for a new local password before anything else (see Local identity).
- A paired session lasts 30 days from its last use; the service extends it at most once a day. Its cookie is named for the client port (`dens_session_<port>`), because cookies aren't scoped by port and each instance has its own.
- The port comes from the authenticated service while it holds that listener on both loopbacks. Neither platform lets another account bind the same address and port, so no separate port-ownership check is needed. On Windows another account can bind the wildcard address on that port, but loopback connections still reach the more specific socket.

**Files in and out**

- Uploads come from the browser's file picker; downloads use `Content-Disposition: attachment` so the browser saves them as the desktop user.
- The service never needs write access to the user's home directory, which removes the Downloads permission step from install.

## Local identity and encryption

Each install has one local user, stored in an encrypted vault, and one random data key that encrypts it. The data key is wrapped twice: once bound to the host for unattended boot, and once by the user's password for backup and moving machines.

**What the vault holds**

- The list of joined dens: URL, den identity key fingerprint, display name.
- One Ed25519 keypair per den, plus the current session token for each.
- Local settings and preferences.

Sensitive fields in SQLite are encrypted one by one with the same data key (XChaCha20-Poly1305): on a client, the vault and cached den content; on a den, message and DM text. This is a speed bump against scanners and backup snoopers, not protection from root or a live compromise, and it doesn't hide anything from the den owner, whose install holds the den's key. Metadata such as timestamps and IDs stays plaintext so queries work.

**Envelope encryption**

1. Install generates a random 256-bit data key.
2. A host-bound copy lets the service start without a password:
   - Linux: an encrypted credential loaded with `LoadCredentialEncrypted=` in the unit. systemd decrypts it at start, bound to the host key or TPM, and hands it to the service in `$CREDENTIALS_DIRECTORY`.
   - Windows: a machine-scope DPAPI blob in the state directory, whose DACL allows only the service account, SYSTEM and Administrators. The service decrypts it at start.
3. A second copy is wrapped with a key derived from the local password (Argon2id) and kept in the storage root.

The privileged CLI does the host wrapping (at install and restore); the service only unwraps. Password wrapping and all field encryption are shared code.

**Setting the password.** Installers never prompt, so they stay non-interactive. The first `dens open` leads to a page that sets the local password, and the service wraps the data key with it. Until a password exists, the service runs on the host-bound copy alone, and backup and key export stay disabled.

Because this is a root-managed system unit on Linux, it avoids the unprivileged `systemd-creds --user` path entirely. `LoadCredentialEncrypted=` arrived in systemd 250, which becomes the Linux minimum version. Every supported distro ships 252 or newer, and the platform spike confirmed encrypted credentials on 252 through 262.

**Backup, restore, move**

- `dens backup` writes a consistent SQLite snapshot (`VACUUM INTO`), the uploads directory, and the password-wrapped data key. Never a raw copy of a live WAL database.
- `dens restore <file>`, run as root or Administrator, asks for the password, unwraps the data key, and re-wraps it for the new host (`systemd-creds` or DPAPI). Data is never re-encrypted. Restore follows the same lifecycle steps as an update, and a backup from an older version migrates when the service starts; one from a newer version is refused.
- Pairing doesn't carry over: the service clears the restored data's browser sessions on its first start, so browsers pair again with `dens open`.
- Backups have the same format on both platforms, so a client or den can move between Linux and Windows.
- Changing the password re-wraps the data key only.
- Lost password and lost machine together means the local data is gone by design. Den accounts can still be recovered with recovery codes.

**Re-authentication for sensitive actions**

There is no idle lock in v1; the OS screen lock covers someone at an unlocked desktop. Instead, a paired browser must re-enter the local password for sensitive actions: exporting a backup, viewing or exporting keys, changing the local password, and restoring. Someone at an unlocked machine could read chats, but not take the identity with them. An optional idle lock can come later.

**Service hygiene**

- The data key and vault stay in memory only in the service process; nothing sensitive goes to logs.
- Core dumps are disabled (`LimitCORE=0` on Linux; `dens.exe` excluded from Windows Error Reporting), and key material is held in locked memory (`mlock` or `VirtualLock`) so it isn't swapped to disk: the data key, and the seeds of the den's identity key and of each device key. Go's Ed25519 caches expanded keys through weak pointers, which can't point outside the Go heap, so each signature expands a short-lived copy of the key and clears it.

## Authentication

Members log in to a den by signing a challenge with a per-den Ed25519 key, fully transparently. The password is a fallback for new devices, recovery codes are the fallback for a forgotten password, and TOTP is cut from v1.

**Den-side auth is simple because the local service holds the credentials.** The browser never sees den tokens. A Go client can send `Authorization` headers on the WebSocket upgrade, which browsers can't, and it can re-sign a challenge any time. The key does the job refresh tokens usually do, so there are no refresh tokens, rotation or reuse detection.

**Joining (invite redemption)**

1. The invite is a versioned string (dens1: plus base64url of the den URL, identity key fingerprint and single-use code) that the member pastes into a Join box. No invite links in v1, so nothing suggests a central account or server.
2. The client generates a new Ed25519 keypair for this den and pins the den fingerprint.
3. It sends the code, username, a password verifier (see below) and the public key, signed with the new key. The den stores a hash of the verifier and the key.
4. The den returns 10 recovery codes, shown once and stored hashed.

**Password verifiers**

The raw password never leaves the client. For each den, the client derives `verifier = Argon2id(password, salt = SHA-256("dens-den-password-v1" ‖ den_id ‖ username))` (t=3, m=64 MiB, p=4, 32 bytes) and sends that, and the den stores `SHA-256(verifier)`.

- A malicious or breached den learns nothing it can replay at another den, so reusing one password across dens, including the local password, is safe.
- Offline guessing against a den's database costs a full Argon2id per guess, paid by the attacker.
- The den never runs Argon2id itself, so login attempts cost it almost no CPU.
- Usernames can't change after joining, since they salt the verifier. Display names can.

**Normal login (invisible to the user)**

1. Client sends its own random nonce and requests a challenge. The den returns a 32-byte nonce for the client (60 seconds, single use) and signs the client's nonce with the den identity key; the client aborts if the signature doesn't match the pinned fingerprint.
2. Client signs `"dens-auth-v1" ‖ den_id ‖ key_id ‖ nonce`. The context string and den ID stop a signature being replayed elsewhere.
3. The den verifies the signature and issues an opaque 256-bit session token (stored as SHA-256), valid for 1 hour.
4. The WebSocket upgrade carries the token. Before expiry the client re-signs and sends an `auth.renew` frame on the open socket.
5. Revoking a key or banning a member deletes its sessions, and the den closes those sockets immediately.

**Fallbacks**

- **New device, no backup:** username and password verifier plus a new public key. The den registers the key, labels it, and notifies the member's other sessions ("new device added").
- **Forgotten password:** username, one recovery code, a new password verifier and a new public key.
- **Change password:** from a signed-in session, with the current password or a recovery code.

A **Devices** page lists each registered key with its label, creation date and last-seen time, and can revoke any of them.

**Den identity and moving domains**

- Creating a den generates its Ed25519 identity key. It is stored encrypted with the den's data key and included in den backups.
- Clients store each den under its key fingerprint; the URL is an editable field.
- Manual migration in v1: the owner announces the new address, members edit the URL, and the client connects only if the den proves the pinned key.
- A passing check means the new server runs from the den's backup, unlocked with its local password. Normally that's the owner; it cannot rule out someone who stole both.
- A signed "moved" notice that updates the URL automatically is post-v1.

**Why cut TOTP**

TOTP protects against a stolen password. Here the password is only used when adding a device, every such login is announced to the member's existing devices, and online guessing is rate-limited, with each guess costing the guesser an Argon2id computation. The key, which does daily logins, is unphishable and never leaves the machine. TOTP adds setup friction for little gain; it can be added later with `pquerna/otp` as an option on the password fallback only.

**Rate limits**

- Password and recovery attempts: per account and per IP, with backoff; IPs held in memory only.
- Challenge requests and invite redemption: per IP.
- Unknown usernames and wrong passwords get the same error, and the check is a constant-time hash comparison either way, so neither the reply nor its timing shows which usernames exist.

Limiter memory is bounded. Per-IP token buckets live in a fixed-capacity LRU map (for example 100,000 entries, a few MB); an entry is dropped once its bucket has refilled and sat idle, and the least recently used entry is evicted when the map is full. IPv6 addresses are keyed by their /64 prefix so rotating addresses doesn't create new buckets. If eviction churn shows a flood, the endpoint falls back to a global limiter. Per-account backoff is bounded by the member count and never becomes a hard lockout, since lockouts let an attacker lock victims out.

## Den features

Three fixed roles, one level of channel groups, text and voice channels, and DMs. Anything finer-grained is a second den.

**Roles**

| Role | Can |
| --- | --- |
| Member | Read and post in visible channels, DM other members, join voice, upload within limits |
| Moderator | Everything a member can, plus delete messages, kick, ban, create invites, manage channels and groups, see staff-only channels |
| Owner | Everything a moderator can, plus manage moderators, den settings and limits; transfer ownership via CLI |

Channels can be marked staff-only (moderators and owner). There is no other visibility control.

**Structure**

- Channel groups are one level deep and contain text and voice channels.
- Text channels can have a description in the same markdown subset, up to 4,000 characters. Its first line shows next to the channel name, and a click expands or collapses the rest.
- DMs are one-to-one between members of the same den, stored on the den. The UI says the owner can read them.

**Names**

- Usernames are unique, 2 to 32 characters of `a-z`, `0-9` and `_`, lowercased on entry and fixed at join.
- Display names are free-form Unicode up to 32 characters. Control, bidi-override and zero-width characters are stripped, so a name can't disguise itself or reorder the text around it.

**Messages**

- Plain text with a small markdown subset, attachments, edits, deletes and replies. No raw HTML, ever.
- The subset: bold, italic, strikethrough, inline code, code blocks, quotes, spoilers, @mentions and bare URLs. There are no `[label](url)` links, so a link always shows where it goes. The browser parses it into a tree and builds the DOM with `createElement` and `textContent`, never `innerHTML`.
- Each member's read position per channel lives on the den, so it follows them across devices. Channels show unread state and mention counts. Browser notifications come later, and are opt-in.
- Edits carry the revision they were made against, and the den refuses a stale one. Nobody's edit silently overwrites another's, whether it comes from a second device or a co-editor. The member sees the newer text, with their own draft kept to reapply.
- **Shared messages (M1.6):** when posting, the author can name other members who may also edit the message. Only the author can delete it or change who may edit. It suits shared lists and plans.
- **Task checkboxes (M1.6):** lines starting with `[ ]` or `[x]` render as checkboxes in any message. Anyone who may edit the message can tick one, which the den applies as a single toggle, so two people ticking different boxes at once never lose a tick. Everyone else sees them read-only.
- Delete removes the row and its files; `secure_delete` overwrites the freed pages. A delete event tells clients to purge caches.
- Optional den-wide retention (for example 30 or 90 days), off by default, shown to members in den info.
- Ban revokes all of a member's keys, closes their sockets and blocks re-registration with that username.

**Compact links**

Not in M1. When a message is saved, the den rewrites known links to a site code plus ID and expands them at render time. Unknown links are stored unchanged.

| Site | Stored | Kept | Expands to |
| --- | --- | --- | --- |
| Reddit | post ID, optional comment ID | nothing else | `reddit.com` or `old.reddit.com`, per member setting |
| YouTube | video ID | timestamp (`t`) | `youtube.com/watch?v=…` |
| X | status ID | nothing else | `x.com/i/status/…` |

- Tracking parameters (`si`, `utm_*`, `feature`, share IDs) are dropped.
- Links are stored as structured spans (site, ID, position) next to the text.
- Size saving is modest: roughly 40–80 bytes per link, a few MB across 50,000 links. Per-field encryption overhead (nonce and tag, about 40 bytes) is similar in size.
- No link previews in v1: fetching them would reveal the den's or members' IPs to those sites. Could be an opt-in feature for v2. Previews would take a fixed height, with their content scaled to fit, so they never shift the message list.

**Presence at 500 online**

- One WebSocket per member; presence changes are coalesced and broadcast in batches every 1 to 2 seconds.
- Typing indicators are throttled per channel and only sent to members viewing that channel.

## Message list and sync

**Message list**

- A channel view holds one contiguous run of messages, about 200, never the whole channel. Scrolling near either end loads the next page (`before` or `after` the edge message) and trims the far end, so memory stays flat however far back a member scrolls.
- The view is attached to the live tail while it holds the newest message, and new messages append. Jumping to an old message (a reply's quote, a mention, later search) loads the page `around` it and detaches the view. New messages then only update a "new messages, jump to present" bar, and scrolling forward to the newest page reattaches it.
- With the window bounded, every loaded message is in the DOM. There is no per-row virtualization unless profiling shows a need.
- The layout doesn't jump. Images carry their dimensions from upload, so placeholders take their final size, and loading older pages keeps the view anchored on the message being read.
- Message IDs increase with time within a den and are never reused, so `before`, `after` and `around` are single index lookups.

**Sync and bandwidth**

Dens usually run on home connections, where upload bandwidth is scarce and every message goes out once per online member. The protocol keeps that fan-out small:

- **Resume, don't refetch.** Every change a member can see is an event with a sequence number. A client that reconnects sends the last one it saw and gets only what it missed, from an in-memory ring of recent events. After a den restart, or a gap longer than the ring, the den sends a fresh snapshot instead (a resync), and the client refetches only what's on screen.
- **Orderly restarts.** A stopping den closes its listener first, then tells every client it is restarting (WebSocket close 1012). Otherwise a quick client reconnects to the dying process, as the M1 spike showed. Clients wait a random delay of up to a few seconds, so 500 of them don't reconnect at once.
- **Scoped ephemeral traffic.** Presence is coalesced into batches every 1 to 2 seconds, and typing goes only to members viewing that channel.
- **Thumbnails, not originals.** Uploads get a small preview when they arrive. The message list shows previews, and originals load only when opened. The local service caches both (in memory until M6, then encrypted on disk) and serves them to the browser with `Cache-Control: no-store`, so they never sit unencrypted in the browser's cache.
- **Compression, measured.** Compressing each message separately (permessage-deflate) saves only 22% on chat events. Context takeover saves 65%, but costs about 820 KB of den memory per connection, around 400 MB at 500 online. A static dictionary shared by client and den, with deflate per frame, reaches 43% of raw with no per-connection memory, and the den compresses each event once for every recipient. Batching events under load brings it to 29%. M1 sends plain JSON. Frames carry arrays of events, so batching and a dictionary encoding can come later without a redesign. History pages over HTTP are gzipped, to 29% of raw.
- **Persistent cache and delta sync (M6).** An encrypted on-disk cache in the client, per-channel "changes since" queries and cached member lists, so a client restarted after a day away downloads only what changed. Until then, history lives in the client's memory, and a service restart refetches what's on screen.

## Files and media

Uploads are stored as-is except for stripped metadata, capped by owner-set limits; images and video are recompressed after an owner-set window.

**Limits (owner settings)**

- Max size per file, per member total, and den total. When a limit is hit, uploading is disabled with a clear message.
- Screen share and media settings live in the same place (see Voice and screen share).

**Metadata stripping**

- EXIF, GPS and similar metadata are stripped from images and video on upload, before anything is stored.
- Images: re-encode-free removal where the format allows; video: `ffmpeg -map_metadata -1 -c copy`.
- Members can see that stripping happened. There is no opt-out in v1.

**Retention and compression**

1. Originals are kept for an owner-set window (for example 14 days), shown on each attachment with a download button.
2. After the window, a worker job recompresses with the vendored ffmpeg and keeps the result only if it is smaller.
3. Jobs run one at a time at the lowest CPU priority so calls and streams keep their CPU: `IDLE_PRIORITY_CLASS` on Windows, and on Linux nice 19 plus `SCHED_IDLE`. Linux nice values and scheduling policies are per thread, so the service sets them on the thread that forks ffmpeg (then discards that thread), and every ffmpeg thread inherits them. There is no transient cgroup scope: the sandboxed service account can't create one, and a niced child already gets a small share next to the service's own threads.

**Serving**

- Everything downloads through the local service with `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff`.
- Inline previews only for a fixed list of raster image and video types. SVG and HTML are never rendered.

The vendored ffmpeg builds (Linux and Windows) must be LGPL-compatible or the project must meet GPL terms; pick the builds deliberately.

## Voice and screen share

The browser does all client-side media with standard APIs, and the den runs a Pion SFU that forwards packets without decoding. In the client role, the service only relays signaling and never touches media; the Pion SFU runs only when the same install is also acting as a den.

**Client side (browser)**

- Mic: `getUserMedia` with `echoCancellation`, `noiseSuppression` and `autoGainControl` on.
- Screen: `getDisplayMedia`, which goes through xdg-desktop-portal and PipeWire on Wayland.
- One `RTCPeerConnection` per member per voice channel, connected to the den.

**Signaling path**

1. The page creates an offer and sends it over its localhost WebSocket.
2. The local service forwards it as a `voice.*` message on the den WebSocket.
3. The den's Pion peer answers; ICE candidates travel back the same way.
4. Media then flows directly between the browser and the den over UDP, encrypted with DTLS-SRTP.
5. When tracks are added (someone joins, a share starts), the den sends a new offer and the page answers.

**Den SFU (Pion)**

- `SettingEngine.SetICEUDPMux` puts all media on one UDP port; `SetICETCPMux` adds a TCP fallback port.
- `SetNAT1To1IPs` advertises the public IP when the den sits behind a router.
- Interceptors: NACK, RTCP reports and TWCC for bandwidth estimation.
- Forward PLI keyframe requests to the sharer when a viewer joins a screen share.
- No TURN in v1: the den is directly reachable, so clients behind NAT connect outward. Add `pion/turn` on TCP 443 later if restrictive networks need it.
- Start from Pion's SFU-over-WebSocket example rather than a blank file.

**Bandwidth at target scale** (approximate)

| Case | Per stream | Den upload |
| --- | --- | --- |
| 10 people in voice (Opus) | 30–50 kbps | about 3–5 Mbps |
| 1 screen share, 20 viewers | about 3 Mbps | about 60 Mbps |
| 2 screen shares, 20 viewers each | about 3 Mbps | about 120 Mbps |

Screen share cost scales with viewers, not sharers. Owner settings: max concurrent shares, max resolution and frame rate, max bitrate per share, and max viewers per share. Simulcast (sender uploads high and low layers, SFU picks per viewer) is the later fix.

**Known risks**

- System audio in screen share is limited in Linux browsers; Chromium-based browsers on Windows support it. Test early; accept gaps in v1.
- Firefox and Chromium differ in small WebRTC details; each milestone is tested on all four target browsers.

## Networking and deployment

A den needs a domain, Caddy, and four forwarded ports. Caddy handles only HTTP and WebSocket traffic; WebRTC media must reach the den directly.

| Port | Protocol | Purpose | Required |
| --- | --- | --- | --- |
| 80 | TCP | ACME certificate challenges (Caddy) | Yes |
| 443 | TCP | HTTPS and WSS to the den listener (Caddy) | Yes |
| Media port, default 7881 | UDP | WebRTC media via UDP mux | Yes, for voice |
| Fallback port, default 7882 | TCP | ICE-TCP media fallback | Recommended |

The docs list which ports to forward but not how, since routers vary too much. They mention a small VPS as the easy alternative when forwarding isn't possible.

On Windows, the installer adds inbound Windows Firewall rules for the media ports, scoped to `dens.exe`, when the den role is enabled, and removes them on uninstall. The Caddy guide covers ports 80 and 443 on both platforms.

**Caddy**

- The guide ships a minimal Caddyfile: the den domain reverse-proxies to the den listener.
- Access logging stays off (Caddy's default), in line with collecting less.
- A second den on the same machine is another instance with its own ports and a second site block for its domain or subdomain.
- Caddy runs as a service on both platforms: its packaged systemd unit on Linux, and on Windows as an SCM service, which Caddy supports natively (`sc.exe create caddy binPath= "…\caddy.exe run --config …"`).
- Reloading or restarting Caddy closes den WebSockets (close 1001). Clients resume without losing events.

**Den-side trust of proxy headers**

- The den listener trusts `X-Forwarded-For` only from loopback, and uses it for rate limiting in memory only.

## Platform support

Dens supports Linux and Windows for both the client and the den. Linux is recommended.

### Linux

Dens supports five systemd desktop distro families. Non-systemd, server and developer-focused distros are cut. The hard floor is systemd 250 for `LoadCredentialEncrypted=`.

| Distro | Minimum | Notes |
| --- | --- | --- |
| Fedora Workstation and spins | Current releases | SELinux enforcing: test file labels for `/var/lib/dens` and the binary |
| Bazzite (Fedora Atomic) | Current releases | Read-only `/usr`: binary in `/usr/local/bin`, user via `/etc/sysusers.d`, units in `/etc/systemd/system`; Silverblue, Kinoite, Bluefin and Aurora considered on the same base |
| Debian | 12 | Ships systemd 252 |
| Ubuntu, Mint, Pop!\_OS | 24.04 base | 22.04 ships systemd 249, below the floor |
| Arch, CachyOS | Rolling | Manjaro and other derivatives considered, not tested |

Cut: Alpine, Void, Artix, Devuan, Gentoo, NixOS, openSUSE and MicroOS, Rocky, AlmaLinux and RHEL, and WSL2. SteamOS support is planned after v1 (see below).

**TPM**

`systemd-creds` binds to the TPM plus the host key when a TPM exists, and to the host key alone otherwise, so no separate fallback is needed. Host-key-only mode still protects copies of the data directory, but not a full disk image or root.

**Install layout rules (all distros)**

These keep immutable and atomic-update distros cheap to support, and are good practice everywhere:

- Install paths come from one layout module, never hardcoded, so a distro can override where the binary lives.
- Keep the `/etc` footprint minimal: the unit file, a `sysusers.d` file, nothing else.
- Define the service user in `/etc/sysusers.d` (creating the directory where the distro only ships `/usr/lib/sysusers.d`, as Debian 13 does) so it's recreated at boot. Don't fix its UID: sysusers silently picks another one when the requested UID is taken. `StateDirectory=` re-owns the state directory to the service account on start, so ownership follows the account.
- Keep the encrypted credential and all state under `/var/lib/dens/<name>/`; reference the credential by absolute path in `LoadCredentialEncrypted=`.
- Use `StateDirectory=` and `RuntimeDirectory=` instead of creating and chowning directories by hand or shipping tmpfiles.d files. The instance root and its `control/` directory (lifecycle state, locks, instance config and the encrypted credential) stay root-owned and readable by the service's group; `StateDirectory=dens/<name>/data` has systemd create the data directory and own it for the service account.

**SteamOS (planned after v1)**

With the rules above, SteamOS support later means: detect `ID=steamos`, put the binary under `/var/lib/dens/bin` (the root image is replaced on updates), and write an `/etc/atomic-update.conf.d/dens.conf` keep list, since updates since SteamOS 3.6 discard unlisted `/etc` changes ([Igalia write-up](https://blogs.igalia.com/berto/2025/02/05/keeping-your-system-wide-configuration-files-intact-after-updating-steamos/)). Scope it as client only, Desktop Mode, tested manually across a real OS update. Post v1 cause testing it will be harder as well.

### Windows

Windows 11, current supported releases, Home and Pro. Windows 10 and Windows Server are cut.

**Service setup**

- The SCM service runs as its virtual account with `SERVICE_SID_TYPE_RESTRICTED`. The token is write-restricted, so the service can write only where its service SID is explicitly granted: the state directory.
- Required privileges are trimmed to `SeChangeNotifyPrivilege`.
- The state directory gets a protected DACL (inheritance off, since `ProgramData` lets the Users group create files by default) granting SYSTEM and Administrators full control and the service SID modify.
- `%ProgramData%\Dens`, which holds the instance roots, lets Users list it, as `/var/lib/dens` is 0755 on Linux. Windows path normalization, which the SQLite driver uses, lists every parent of the database, so the service fails to open it otherwise.
- The host-bound data key is a machine-scope DPAPI blob under that DACL.
- `dens.exe` is excluded from Windows Error Reporting so crashes don't write memory dumps.
- A service has no console, so each instance registers an Application event log source under its service name, and the service records there why it stopped.

**Weaker than Linux, stated in the docs**

- There is no syscall filter, capability bounding set or read-only view of the filesystem. Writes are restricted; reads follow ordinary file ACLs, so the service can read anything the Users group can. User profiles are private by default.
- The host-bound key copy is protected only by its file ACL: any process that can read the blob can decrypt it with machine-scope DPAPI. On Linux, decryption also needs the root-only host key or the TPM. There is no TPM binding in v1, but every Windows 11 machine has TPM 2.0, so binding through the Platform Crypto Provider is a good post-v1 item.
- The binary and installer are not Authenticode-signed, so SmartScreen warns on first run. Integrity comes from the cosign signatures on every platform, and the install docs show the warning and explain how to verify the download before running it.

**Known risks**

- The platform spike showed sockets, interface enumeration, named pipes and child processes working under the restricted service SID. Pion itself and a real ffmpeg build are still untested there.

### Platform layer

All OS-specific runtime code lives in one package (`internal/platform/host`). Each seam is a small function pair in `_linux.go` and `_windows.go`. Everything above it (listeners, `Host` and `Origin` checks, rendering, auth, crypto, rate limits, the SFU and the database) is shared code with no build tags.

| Seam | Linux | Windows |
| --- | --- | --- |
| Service host | sd_notify, `SIGTERM` | `x/sys/windows/svc` handler |
| Host-bound data key | Read `$CREDENTIALS_DIRECTORY` | DPAPI unprotect |
| Control endpoint | Unix socket, `SO_PEERCRED` | Named pipe on `x/sys/windows` (no third-party module), client SID by identification-level impersonation, server PID check |
| Private directory check | Owner and mode 0700 | Owner and a protected DACL naming only the allowed SIDs |
| Locked key memory | `mlock` | `VirtualLock` |
| Low-priority child process | nice 19 and `SCHED_IDLE` on the forking thread | `IDLE_PRIORITY_CLASS` |
| Open a URL | `xdg-open` | `ShellExecute` |

Everything else that differs is install-time: the two installers, the unit file and SCM registration, account creation, ACLs and firewall rules.

Rules that keep the two implementations from drifting:

- A platform difference never reaches the protocol, database or HTTP layers. A feature that seems to need one is a design problem.
- Each seam has a contract test that runs on both CI runners.
- Windows code compiles only under `GOOS=windows`; vet and build-test it on every change that touches it.

**Testing the Linux service in containers.** The lifecycle harness runs in Incus containers, which change how units behave:

- In unprivileged containers LXC adds `/run/systemd/system/service.d/zzz-lxc-service.conf` to every service. It turns off `NoNewPrivileges`, credentials (including encrypted ones), `ProtectProc`, `ProcSubset`, `ProtectControlGroups` and `ProtectKernelTunables`, so a unit starts and passes while running unhardened and without its data key. The harness masks it (a `/dev/null` symlink at `/etc/systemd/system/service.d/zzz-lxc-service.conf`) and starts containers with `security.nesting=true`.
- systemd before 256 (Debian 12, Ubuntu 24.04) can't create its credential host secret in an unprivileged container, so those distros run in privileged containers.
- Privileged containers get the host kernel's `binfmt_misc` table, which systemd empties when the container shuts down. On WSL that stops Windows programs from running until WSL restarts, so the harness mounts it read-only in privileged containers.
- Containers share the host kernel and have no TPM or SELinux; those need a VM or a real install.

## Versioning and compatibility

Clients and dens negotiate an integer protocol version, separate from the release version, so most client updates never force a den update.

- Each release declares `protocol_current` and `protocol_min`, the oldest protocol it still speaks.
- The client sends `Dens-Protocol: <current>` on every request and the WebSocket upgrade; the den replies with its own range.
- If the ranges overlap, both use the highest shared version. If not, the client shows "this den needs a newer or older Dens" with both versions.
- Protocol bumps only for breaking changes; additive fields and message types don't bump it, and unknown message types are ignored.
- Aim to keep `protocol_min` one version behind current, so a den can lag one breaking release without locking members out.

An install that is both client and den still updates as one binary. If that blocks someone, the documented answer is to move the den to its own install.

## Security hardening

The localhost page is the most valuable target: an XSS there reaches every joined den, the vault and the local API. Any den member can send you content, so all of it is hostile input.

**Client page and listener**

- [x] CSP: `default-src 'none'`, with scripts, styles, images, fonts and connections limited to `'self'`, no inline scripts, `frame-ancestors 'none'`.
- [ ] Messages rendered by an escaping markdown subset; usernames, filenames and embeds treated as untrusted text.
- [x] Exact `Host` check (`127.0.0.1:<port>`, `[::1]:<port>` or `localhost:<port>`) against DNS rebinding.
- [ ] `Origin` check on every write and on the WebSocket upgrade.
- [x] Session cookie `HttpOnly`, `SameSite=Strict`; pairing tokens single use and short-lived.
- [x] Listener bound on both `127.0.0.1` and `::1`.
- [ ] Everything from a den is hostile input: the client service checks every den response against the protocol's types and limits before storing or forwarding it.

**Den listener**

- [x] Serves only den routes; no client or admin routes compiled into its router.
- [ ] Request size limits, WebSocket message size limits and per-connection rate limits.
- [ ] Invite codes: 128-bit random, single use, expiring, stored hashed.
- [ ] Bearer tokens only, never cookies, so no web page can make a browser act on a den.

**Control endpoint**

- [x] Every connection authorized by OS-reported peer identity against the recorded desktop user.
- [x] The CLI verifies the server's identity before trusting a response.
- [x] Windows pipe: `FILE_FLAG_FIRST_PIPE_INSTANCE`, `PIPE_REJECT_REMOTE_CLIENTS`, one listening instance at all times, DACL granting SYSTEM and the service SID full access and the recorded user only `0x12019b` (never `GENERIC_WRITE`).

**Service sandbox (Linux)**

- [x] `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp`, `PrivateDevices`, `RemoveIPC`.
- [x] `ProtectKernelTunables`, `ProtectKernelModules`, `ProtectKernelLogs`, `ProtectControlGroups`, `ProtectClock`, `ProtectHostname`, `ProtectProc=invisible`, `ProcSubset=pid`.
- [x] `RestrictNamespaces`, `RestrictRealtime`, `RestrictSUIDSGID`, `LockPersonality`, `MemoryDenyWriteExecute`.
- [x] Empty `CapabilityBoundingSet` and `AmbientCapabilities`, `SystemCallArchitectures=native`, `SystemCallFilter=@system-service`, `SystemCallErrorNumber=EPERM` so a blocked call fails and is logged instead of killing the service.
- [x] `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK`. Netlink is needed for interface enumeration, which Pion's ICE gathering uses; without it `net.Interfaces` fails.
- [x] `LimitCORE=0`, `UMask=0077`, `StateDirectory=dens/%i/data` (mode 0700) and `RuntimeDirectory=dens/%i` (mode 0755, for the socket) as the only writable paths.
- [x] `systemd-analyze security` rates the unit 1.5 ("OK") on every supported distro; the lifecycle e2e fails above that.

**Service sandbox (Windows)**

- [x] Virtual account, `SERVICE_SID_TYPE_RESTRICTED`, required privileges limited to `SeChangeNotifyPrivilege`.
- [x] Protected DACL on the state directory and data key blob: SYSTEM, Administrators and the service SID only.
- [x] `dens.exe` excluded from Windows Error Reporting.

**Logs and data**

- [ ] No message content, tokens, keys or IPs in logs; log IDs and event types only.
- [x] SQLite `secure_delete=ON`.
- [x] Signed releases: the installers and `dens update` verify cosign signatures against the release workflow's identity.

## Milestones

Each milestone ends usable on its own and is tested on Linux and Windows with all four target browsers. All platform-specific code lands in M0, while the seams are still cheap to change; later milestones are shared code.

| # | Milestone | Done when |
| --- | --- | --- |
| M0 | Foundation: Sprout fork trimmed to the single-process lifecycle, elevated installers for Linux and Windows, `dens@`/`dens-<name>` service, platform layer, two listeners, vault with envelope encryption, `dens open` pairing | On both platforms: browser pairs, vault survives reboot, backup restores on a second machine, including Linux to Windows and back |
| M1 | Text den: invites, key auth and fallbacks, roles, channels, groups, DMs, presence, uploads with limits and metadata stripping | Two machines chat through a Caddy-fronted den |
| M2 | 1:1 voice: Pion SFU with UDP mux, ICE-TCP, signaling relay | Clear two-person call across two home networks |
| M3 | Group voice: renegotiation on join and leave, mute, speaking indicators | 10-person call stays stable for an hour |
| M4 | Screen share: PLI forwarding, owner limits, viewer caps | 2 shares with 20 viewers within owner limits |
| M5 | Media retention and compression worker, message retention setting | Old originals replaced, calls unaffected during jobs |
| M6 | Sync efficiency: encrypted persistent client cache, per-channel delta sync, cached member lists, dictionary frame encoding | A client restarted after a day offline downloads only what changed |

M6 can move ahead of M2 if bandwidth shows up as a problem in testing.

**M1 steps.** M1 lands as six steps, each its own pull request, each ending with a check in the four target browsers:

| Step | Scope | Done when |
| --- | --- | --- |
| M1.1 Join | Den creation and owner account, invites, joining with a keypair and password verifier, key login and sessions, the den WebSocket with renewal and resume, protocol versioning, the Preact shell, Caddy on both platforms | A second machine joins through Caddy, with Linux and Windows dens, and stays connected across a den restart |
| M1.2 Chat | Channels and groups with descriptions, messages with the markdown subset, edits with revisions, deletes, replies, the windowed message list with jump to message, read positions and mentions | A client that was offline catches up without gaps or duplicates, and a reply jumps 5,000 messages back and returns to the present |
| M1.3 Community | Roles, staff-only channels, kick and ban, DMs, presence, typing | A ban closes the member's sockets right away |
| M1.4 Files | Upload limits, metadata stripping, thumbnails and image dimensions, attachments served through the local service | A phone photo with GPS data arrives stripped, and the list shows its thumbnail without layout shift |
| M1.5 Recovery | New-device login, recovery codes, password change, the Devices page | A member recovers on a fresh machine and revokes the old key |
| M1.6 Shared messages | Co-editors on messages, task checkboxes | Two members tick different boxes on one checklist at the same moment and both ticks stay |

Not in M1: compact links, message retention (M5), video uploads (they need ffmpeg), browser notifications, and the persistent cache (M6).

**M1 testing.** A den e2e harness runs beside the lifecycle harnesses, and each step extends it. On Linux, an Incus container hosts a den behind Caddy with Caddy's internal certificate authority, and a container on another distro trusts that authority, joins by name and must stay connected across a den restart. On Windows, one instance hosts a den behind Caddy running as a Windows service, and a second instance joins it. Cross-platform pairs (a WSL client with a Windows den, a Windows client with a Linux den) are checked by hand once per step. Caddy is pinned in `scripts/vendor.sh`, since distro packages lag (Debian 13 ships 2.6). [lifecycle.md](lifecycle.md) describes running the harnesses.

**After v1:** bookmarks (per member and per den, so a den's bookmarks always resolve against that den), signed den move notices, SteamOS, TPM binding for the Windows data key, optional TOTP on the password fallback, simulcast, TURN, and an optional idle lock.

## Open questions

- [ ] Which ffmpeg builds to vendor for Linux and Windows, and their license terms.
- [ ] Owner defaults for retention windows, upload limits and screen share caps.
- [ ] How much system-audio support in screen share is achievable on each browser.
- [ ] Uploads at rest on the den: encrypted with the data key like message text, or stored plain (decide in M1.4).
- [ ] The static dictionary for frame compression: what it's built from (never members' messages) and how its version is negotiated (M6).
- [ ] How the message renderer is tested: a pinned JavaScript runtime for unit tests, or a small Playwright suite.
- [ ] SELinux labels for the binary and `/var/lib/dens` on Fedora and Bazzite, which containers can't test; needs a VM or a real install.
