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
- Manageable self-hosting: storage limits members can see and manage, no email dependency.

**Non-goals**

- End-to-end encryption for channels. The den, and so its owner, can read channel messages and files, and the docs say so. DMs are the exception: from M1.7 they're end-to-end encrypted (see End-to-end encrypted DMs).
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
| The den owner reading DMs and the photos in them (end-to-end encrypted from M1.7) | Members who compare DM check codes over a channel the den's owner controls, or with someone pretending to be the other member |

The public docs include a short philosophy section: being able to say dumb things, joke, vent and make art without a permanent searchable record is part of being human. They recommend Linux over Windows, LibreWolf, and not posting identifying details for low-stakes use, and point to Signal, Qubes OS and similar tools for higher-stakes threat models, noting that none of them are bulletproof.

Design rules that follow from this:

- Collect less: no IP addresses written to disk by default, and Caddy access logs stay off.
- Delete for real: deletions remove rows, with SQLite `secure_delete` on.
- Let conversations evaporate: an optional den-wide message retention period.
- Say plainly who can read what: in onboarding and the docs, once, and on the page by marking what's end-to-end encrypted (M8.3).

## Architecture

Dens is one static Go binary, forked from Sprout, that always runs the client and optionally runs a den. It runs as a hardened system-level service under a dedicated account, never as the person's desktop user: a systemd unit on Linux and an SCM (Service Control Manager) service on Windows.

**Traffic paths**

| Path | Transport | Carries |
| --- | --- | --- |
| Browser ↔ local service | HTTP + WebSocket on loopback, at `localhost` (client listener) | UI, local API, signaling relay |
| Local service ↔ remote den | HTTPS + WSS via the den's Caddy | Auth, messages, presence, files, signaling |
| Local service ↔ own den | HTTP + WebSocket to the den listener on 127.0.0.1 | The same, for the den this install hosts |
| Browser ↔ den SFU | UDP (DTLS-SRTP), TCP fallback | Voice and screen share media only |
| CLI ↔ local service | Unix socket (Linux), named pipe (Windows) | Pairing tokens, admin commands, backups |
| Local service ↔ YouTube | HTTPS | A linked video's title and picture, for a member who turned YouTube players on (M4.1) |
| Browser ↔ YouTube | HTTPS, in a sandboxed frame | YouTube's player, once that member plays a video (M4.1) |

The browser never talks to a den over HTTP. All den content reaches the page through the local service, so it only ever runs under the localhost origin.

The owner's client reaches its own den over loopback, not through the public domain. That works before DNS and Caddy are set up, and on routers that can't route to their own public address (no hairpin NAT). The den still proves its identity key at every login, so loopback loses no authentication. The client-to-den protocol is specified in [protocol.md](protocol.md).

**Client UI.** The page is a Preact app (about 10 KB) built by the pinned esbuild into one hashed bundle. Preact injects no scripts or styles at runtime, so the CSP stays `script-src 'self'` with no inline code. The stylesheet carries every DaisyUI theme, which a member picks in the settings and the browser keeps; until they pick one, the page follows the device's light or dark preference, with Light and Dark (M3.3). The page has no header (M3.3): a den's channel list holds the way home, at the left of the den's name, which shows the den's status only while it isn't connected, and the cog for the settings sits in the member's panel at its foot; home has a cog of its own, fixed at the bottom left, and a development instance's tab names it. The page keeps one WebSocket to the local service, and one that hasn't opened within a few seconds counts as down, so the page says it's out of touch rather than looking fine while it hears nothing. Views reload once it's back, and a reload applies again what changed while it was out, since the snapshot it fetched may be older. WebSockets, both den and local, use `github.com/coder/websocket`: small, context-first and without dependencies, since the standard library has none.

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
| Restart on failure | `Restart=on-failure` | SCM recovery actions, which apply to a crash, so a service that fails after starting exits as one (M8) |

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
- The install guide has people download the installer (`install.sh` or `install.ps1`) and run it, never `curl | sudo bash` or `irm | iex`, with checking its cosign signature first as an optional step, folded away, commands included. The installer checks everything it downloads; only a check before running it covers the script itself.
- Updates are manual: `sudo dens update` on Linux, or `dens update` from an elevated terminal on Windows (`sudo dens update` also works where Windows' built-in sudo is enabled). It downloads the latest installer and its cosign bundle, verifies them against the signing identity baked into the binary, and runs the installer in the foreground. There is no unattended auto-update.
- An installation never moves to an older release than the one installed (M8). A signature proves who built a release, not that it's the newest, so a release host or mirror that served an older signed release could otherwise roll installs back to holes since fixed. Installing the installed version again is allowed; it repairs.
- Only flags on the command line, never the environment, change where the installers and `dens update` fetch a release or whether they verify it (M8): `--release-url` and `--skip-verify` (`-ReleaseUrl` and `-SkipVerify` on Windows). An elevated Windows shell inherits the user's own environment variables, which anything running as them can set. A binary the release pipeline built refuses `--skip-verify`; the test harnesses install unsigned fixture releases with binaries built without the release identity.
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
- The service returns a one-time token and its client port. The CLI opens `http://localhost:<port>/#token=…` (`xdg-open` on Linux, `ShellExecute` on Windows).
- The page lives at `localhost`, not `127.0.0.1` (M4.1): YouTube's player won't play for a page at an IP address (see YouTube players). Browsers resolve `localhost` to loopback themselves, never through DNS, and treat it as a secure context, as they do `127.0.0.1`. A browser that loads the page at `127.0.0.1` or `[::1]` as a document is redirected to `localhost`, keeping the path and, since browsers carry a fragment across a redirect, the pairing token. Scripts and tests, which don't load it as a document, get their answer where they asked.
- The page exchanges it for an `HttpOnly`, `SameSite=Strict` session cookie and clears the fragment. On the first pairing, the page then asks for a new local password before anything else (see Local identity).
- A paired session lasts 30 days from its last use; the service extends it at most once a day. Its cookie is named for the client port (`dens_session_<port>`), because cookies aren't scoped by port and each instance has its own.
- The port comes from the authenticated service while it holds that listener on both loopbacks. Neither platform lets another account bind the same address and port, so no separate port-ownership check is needed. On Windows another account can bind the wildcard address on that port, but loopback connections still reach the more specific socket.

**Files in and out**

- Uploads come from the browser's file picker, drag and drop, or a paste; downloads use `Content-Disposition: attachment` so the browser saves them as the desktop user.
- The service never needs write access to the user's home directory, which removes the Downloads permission step from install.

**Third-party notices (M3)**

The binary includes others' code: Go's standard library and modules such as Pion and the SQLite driver; FFmpeg, zlib and libaom in the media module; RNNoise; and Preact, Tailwind and DaisyUI in the page. Most of their licenses ask that copies of the binary carry their notices, and the LGPL also asks for FFmpeg's source, which each release publishes.

- `scripts/notices.sh` gathers every license into one file that the binary embeds: those of the Go modules it links, from the module cache, and those of the inputs `scripts/vendor.sh` pins. The file is committed, so a new dependency shows up in review as a new license, and CI writes it again and fails if it differs.
- `dens licenses` prints it, and the page's settings link to it.

## Local identity and encryption

Each install has one local user, stored in an encrypted vault, and one random data key that encrypts it. The data key is wrapped twice: once bound to the host for unattended boot, and once by the user's password for backup and moving machines.

**What the vault holds**

- The list of joined dens: URL, den identity key fingerprint, display name.
- One Ed25519 keypair per den, plus the current session token for each.
- The DM seal (M1.7).
- Local settings and preferences.

Sensitive fields in SQLite are encrypted one by one with the same data key (XChaCha20-Poly1305): on a client, the vault; on a den, message and DM text, and the names of uploads. A den's uploads are encrypted with it too (see Files and media), and so is everything a client caches from other dens, in a database and directory of the cache's own (M6; see The client's cache). This is a speed bump against scanners and backup snoopers, not protection from root or a live compromise, and it doesn't hide anything from the den owner, whose install holds the den's key. Metadata such as timestamps and IDs stays plaintext so queries work.

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

- `dens backup` writes a consistent SQLite snapshot (`VACUUM INTO`), the uploads directory, and the password-wrapped data key. Never a raw copy of a live WAL database. It leaves out the client's cache, which holds only copies of what dens hold (M6). While a backup is written, the den holds back taking deleted files off the disk, so the archive has every file its snapshot names (M8).
- The data directory's `tmp`, which holds files only while the service works on them (uploads arriving, scratch files, a backup's snapshot), is emptied as the service starts, so nothing a crash left there outlives it (M8).
- `dens restore <file>`, run as root or Administrator, asks for the password, unwraps the data key, and re-wraps it for the new host (`systemd-creds` or DPAPI). Data is never re-encrypted. Restore follows the same lifecycle steps as an update, and a backup from an older version migrates when the service starts; one from a newer version is refused.
- Pairing doesn't carry over: the service clears the restored data's browser sessions on its first start, so browsers pair again with `dens open`.
- A restored den starts a new sync epoch on that first start, so its members' clients drop what they cached from it (M6; see What changed while away).
- Backups have the same format on both platforms, so a client or den can move between Linux and Windows.
- Changing the password re-wraps the data key only.
- Lost password and lost machine together means the local data is gone by design. Den accounts can still be recovered with recovery codes.

**Re-authentication for sensitive actions**

There is no idle lock in v1; the OS screen lock covers someone at an unlocked desktop. Instead, a paired browser must re-enter the local password for sensitive actions: exporting a backup, viewing or exporting keys, viewing the DM seal (M1.7), changing the local password, and restoring. Someone at an unlocked machine could read chats, but not take the identity with them. An optional idle lock can come later.

**Service hygiene**

- The data key and vault stay in memory only in the service process; nothing sensitive goes to logs.
- Core dumps are disabled (`LimitCORE=0` on Linux; `dens.exe` excluded from Windows Error Reporting), and key material is held in locked memory (`mlock` or `VirtualLock`) so it isn't swapped to disk: the data key, the seeds of the den's identity key and of each device key, and each den's DM seal. Go's Ed25519 caches expanded keys through weak pointers, which can't point outside the Go heap, so each signature expands a short-lived copy of the key and clears it. A DM key is opened from its sealed copy only while a message or file is sealed or opened, and cleared after.

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

1. Client sends its own random nonce and requests a challenge. The den returns a 32-byte nonce for the client (60 seconds, single use) and signs both nonces and its own public address with the den identity key. The client aborts if the signature doesn't match the pinned fingerprint, and sends nothing more to an address the den didn't sign (see Den identity and moving domains).
2. Client signs `"dens-auth-v1" ‖ den_id ‖ key_id ‖ nonce`. The context string and den ID stop a signature being replayed elsewhere.
3. The den verifies the signature and issues an opaque 256-bit session token (stored as SHA-256), valid for 1 hour.
4. The WebSocket upgrade carries the token. Before expiry the client re-signs and sends an `auth.renew` frame on the open socket.
5. Revoking a key or banning a member deletes its sessions, and the den closes those sockets immediately.

**Fallbacks**

- **New device:** username and password verifier plus a new public key. The den registers the key, labels it, and tells the member's other sessions, whose chat shows that a new device signed in. From M1.7, one of the member's other devices must also approve it (see Approving new devices).
- **Forgotten password:** username, one recovery code, a new password verifier and a new public key. The code is spent, the new password replaces the old one, and every other device is signed out.
- **Change password:** from a signed-in session, with the current password or a recovery code. It signs out every other device: a device key outlives the password that added it, so anyone who had the old password may still hold one.
- **New recovery codes:** from a signed-in session, with the password, so a stolen session can't make codes to keep the account with.

A new device names the den with any invite from it, even a used one, which pins its identity, or with its address, which trusts the key the den proves there. Trusting an address is safe for the password, since a verifier made for one den is worthless at another. The page shows each den's ID, the start of its identity, so a member can compare it with someone else's.

Each den on the home page has a **Devices and password** section. It lists each registered key with its label, when it was added and when it last signed in, and signs any of them out, this one included, closing their sockets at once. It also changes the password, which signs out every other device, and makes new recovery codes. A device that was signed out says so and offers to sign in again.

**Approving new devices (M1.7)**

From M1.7, a password alone doesn't add a device: one of the member's other devices approves it, which makes their own devices a second factor with no authenticator app.

1. The new device signs in with the username and password as now. The den holds it as pending for 10 minutes and asks the member's other sessions, which show the request with the new device's label.
2. The new device and one of the member's existing devices, the one they approve from, run the same exchange that starts a DM, through the den: the new device starts it with its sign-in. Each shows half of its check code (see End-to-end encrypted DMs), and keeps showing it until the other device no longer needs it: the new device until it's signed in, and the approving one, after it approves, until the member is done. The member types each device's digits into the other, so a request from someone else, whose screen the member can't see, can't be approved by accident; and if the den had swapped in keys of its own, the digits wouldn't match.
3. The existing device seals the member's DM seal with the exchange's key and sends it through the den, which carries it but can't open it. Only then does the den register the new device and start its session.
4. Refusing a request, or letting it expire, keeps the device out. A refusal also tells the member to change their password, since someone has it. A new password, starting over, or leaving the den cancels what's waiting.

- Both checks matter: the existing device mustn't hand the seal to anyone else, and the new device mustn't take a seal from anyone else, or DMs it starts would be sealed under a key the den knows.
- The device that joins with an invite needs no approval. A member with no other device signs in with a recovery code, which needs none, and types their saved seal to read their DMs: the codes are the one way in without a device. With neither devices nor codes, the account can't be recovered.
- The den enforces approval, but DM privacy doesn't rest on it: a device the den lets in on its own gets no seal, so it can't read DMs, and a DM it starts fails the check with anyone who compares with the real member.

**Den identity and moving domains**

- Creating a den generates its Ed25519 identity key. It is stored encrypted with the den's data key and included in den backups.
- Clients store each den under its key fingerprint; its address can change.
- The den signs its public address into every challenge answer. A client that reached it at another address sends nothing more there: it runs the challenge again at the signed address, and moves there only if the den proves the same key.
- So an owner moves a den by changing its address in the den's settings, and keeps the old name pointing at the den for a while. A connected client moves as soon as the den announces the new address and proves itself there; one that was off moves at its next start, through the old name. A member whose install was off for longer enters the new address by hand, where the same check applies.
- The signed address also stops a relay. A server at an address the den left, or any other, can pass the den's answers along, but the address in them sends the client to the den itself before it sends a proof, a password verifier or a token.
- A passing check means the new server runs from the den's backup, unlocked with its local password. Normally that's the owner; it cannot rule out someone who stole both.
- A den chooses the addresses its members' clients ask, so a hostile den could turn its members into a crowd asking someone else's server, or their own machines' services on loopback. So a client checks a new address for a den at most once a minute, never follows a den from an `https` address to a plain-`http` one, nor from an address elsewhere to one on its own computer, and follows no redirect from a den, which the protocol never sends (M8).

**Why cut TOTP**

TOTP protects against a stolen password. Here the password is only used when adding a device, every such login is announced to the member's existing devices, and online guessing is rate-limited, with each guess costing the guesser an Argon2id computation. From M1.7 a new device also needs approval from one of the member's other devices, which are a second factor already. The key, which does daily logins, is unphishable and never leaves the machine. TOTP adds setup friction for little gain; it can be added later with `pquerna/otp` as an option on the password fallback only.

**Rate limits**

- Password and recovery attempts: per account and per IP, 10 and then one every 6 minutes; IPs held in memory only. A signed-in member's password checks, to change it or make new codes, count against the account's.
- Challenge requests and invite redemption: per IP.
- Messages: per member and channel, and from M8 one budget per member across every channel and DM, in messages and in bytes, so spreading messages over many channels or DMs doesn't multiply what a member may send. Message text counts against no storage limit, so this budget is what bounds how fast one member can grow the den's database.
- Unknown usernames and wrong passwords get the same error, and the check is a constant-time hash comparison either way, so neither the reply nor its timing shows which usernames exist.
- A request's body has a minute to arrive, as an upload has for each read, so a client that sends slowly can't hold the den's handlers forever (M8).

Limiter memory is bounded. Token buckets live in fixed-capacity LRU maps of 100,000 entries, a few MB each: when one is full, the least recently used bucket goes. IPv6 addresses are keyed by their /64 prefix so rotating addresses doesn't create new buckets. A flood from enough addresses can push a bucket out and start it over, which buys a password guesser little: a password alone doesn't add a device (M1.7), and each guess costs them an Argon2id. The per-account limit is keyed by username and refills on its own, so it never locks an account, since lockouts let an attacker lock victims out: a stream of guesses can hold up the member's own password sign-ins, but never their devices, which sign in with keys. The nonces the den hands out for sign-ins are bounded the same way: with 100,000 outstanding, the oldest goes, rather than the den refusing every sign-in (M8). A client uses its nonce within moments, so a flood can't outrun it.

## Den features

Three fixed roles, one level of channel groups, text and voice channels, and DMs. Anything finer-grained is a second den.

**Roles**

| Role | Can |
| --- | --- |
| Member | Read and post in visible channels, DM other members, join voice, upload within limits |
| Moderator | Everything a member can, plus delete members' messages, remove and ban members, create invites, manage channels and groups, see staff-only channels |
| Owner | Everything a moderator can, plus manage moderators, den settings and limits |

Channels can be marked staff-only (moderators and owner). There is no other visibility control. Staff act only on those of a lower rank, so moderators can't remove, ban or delete the messages of other moderators or the owner. Their powers stop at DMs, whose text they can't see: in a DM, only a message's author deletes it (M8).

**Handing the den on (M8).** The den's machine has the last word on who owns it. Its desktop user, with the local password, makes a one-time owner invite with `dens den owner-invite`, which lasts an hour. Whoever redeems it joins as the owner, and the owner before them becomes a moderator. It hands on the owner's role, not the den: the den stays on the machine it runs on, which keeps the last word, since it can make another owner invite at any time. Moving the den to someone else's machine is a backup and a restore there (see Backup, restore, move). An owner invite is also the way back for an owner locked out of their account, as by signing out their own device with no recovery codes left: they join again under a new username.

**Leaving, removal and bans**

- Removing a member (a kick) signs them out everywhere at once and takes them off the member list. Their record stays, so their messages keep a name, and nobody else can take their username.
- A removed member can come back only with a new invite, since invites are the only way in. They come back as themselves, history included, by giving their den password with their old username.
- A ban does the same and keeps that username out, even with a valid invite. It can also delete their recent messages, in DMs too from M8, since the den deletes a DM's messages without reading them and that's the only way to clear DM spam, and revoke the invite they joined with. The UI offers removal and banning as one action with a ban option.
- Lifting a ban takes staff of the rank of whoever placed it, or higher: a ban the owner placed, only the owner lifts (M8).
- A ban can't stop someone from joining under a new name with a new invite: the den keeps no IP addresses, and each join uses a fresh key. Invites are the real door, which is why the removal dialog offers to revoke the one they used.
- Leaving on your own works like a removal, without the kick.

**Structure**

- Channel groups are one level deep and contain text and voice channels.
- Staff open a channel's settings from the ⋯ beside it in the channel list (M4.2), as members' rows in a call have one, which shows on hover or focus, and always on touch screens: its name, its group and its place in the list, whether it's staff-only, a text channel's description, a voice channel's bitrate, and deleting it. A group's ⋯ opens the group's the same way. A voice channel can't be opened, since clicking it joins its call, so its ⋯ is the way to its settings.
- Text channels can have a description in the same markdown subset, up to 4,000 characters. Its first line shows next to the channel name, and a click expands or collapses the rest.
- DMs are one-to-one between members of the same den, stored on the den and end-to-end encrypted (see End-to-end encrypted DMs). Closing a DM hides it until a new message arrives in it, and that follows the member across devices, like read positions.

**Profiles**

- Each den has its own profile for each member, since identity is per den: a display name and a bio of up to 300 characters in the markdown subset. Dens can't link a member's profiles across dens.
- Bios travel only when a profile card opens, which keeps snapshots small for 500-member dens.
- A profile can carry a picture and a banner: uploads the member crops in the page to a square and to three times as wide as high, zooming and dragging the image in a frame. The page draws the framed part into a new image, which leaves the original's metadata behind, and uploads that like any other image. Until someone sets a picture, their avatar is a circle colored by their member ID with the first letter or digit of their username, so every device draws the same one without the den storing or sending anything.
- A member who leaves or is removed loses their pictures along with their devices, so staff removing someone also takes down what they had on their profile.
- Profile cards show a role badge only for moderators and the owner.

**Names**

- Usernames are unique, 2 to 32 characters of `a-z`, `0-9` and `_`, lowercased on entry and fixed at join.
- Display names are free-form Unicode up to 32 characters. Control, bidi-override and zero-width characters are stripped, so a name can't disguise itself or reorder the text around it.

**Messages**

- Plain text with a small markdown subset, attachments, edits, deletes and replies. No raw HTML, ever.
- The subset: bold, italic, strikethrough, inline code, code blocks, quotes, spoilers, @mentions and bare URLs. There are no `[label](url)` links, so a link always shows where it goes. The page parses it into Preact nodes whose text is always text, never markup, and a test fails on any script that uses `innerHTML` or another way to turn a string into markup. Every message is someone else's input, so the parser takes time in proportion to a text's length, whatever its delimiters, and the page parses each text once rather than on every render (M8).
- A reply quotes its original, with the author and first line, and a click jumps there. The den sends that preview with the reply, so the quote reads without the original loaded, however far back it is. Once the original is deleted, the reply says so.
- A message's text that runs taller than about 16 lines, such as a pasted log or a wall of blank lines, shows its top under a fade, with Show more; Show less folds it back. It's held at that height from its first paint, so folding never moves the list, and the list's bounds and pages count it at that height. Nothing limits lines beyond the 4,000 characters: the den can't read a DM, and a hostile den or sender wouldn't keep a limit, so the page folds whatever arrives.
- Each member's read position per channel lives on the den, so it follows them across devices. Channels show unread state and mention counts. Browser notifications come later, and are opt-in.
- The den counts mentions and the page highlights them by one rule, written out in `protocol.md` and tested on both sides against the same cases. Mentions in code or inside links don't count, so a link to someone's profile doesn't ping them.
- Edits carry the revision they were made against, and the den refuses a stale one. Nobody's edit silently overwrites another's, whether it comes from a second device or a co-editor. The member sees the newer text, with their own draft kept to reapply.
- **Shared messages (M1.6):** when posting, the author can name up to 20 other members who may also edit the message, among those who can see the channel. Editors can't delete it, and only the author changes who may edit. It suits shared lists and plans.
- **Task checkboxes (M1.6):** lines starting with `[ ]` or `[x]` render as checkboxes in any message. Anyone who may edit the message can tick one, which the den applies as a change of its own, so two people ticking different boxes at once never lose a tick. A tick names its task's text, so one against a list that changed meanwhile is refused rather than landing on another line. Everyone else sees the boxes read-only.
- The composer's + opens a small menu above it, for attaching files and choosing who else may edit. A ? beside it opens a guide to writing a message: Enter and Shift+Enter, the markdown subset, mentions, links and how they're shortened, task lines and who can tick them, shared editing, ↑ to edit the last message, replies and files (M3.3).
- Delete removes the row and its files; `secure_delete` overwrites the freed pages. A delete event tells clients to purge caches, and the den keeps the message's ID and channel, never its text, author or files, for 30 days, so a client that was away drops its copies when it comes back (M6; see What changed while away).
- Optional den-wide retention, off by default: messages older than a period the owner sets are deleted with their files (M5; see Retention).
- Removal and bans revoke all of a member's keys and close their sockets at once (see Leaving, removal and bans).

**Retention (M5)**

An owner can have the den forget old conversations: with retention on, every message older than a period they set is deleted, with its files. A new den has it off.

- The period is a number of days, from 1 to 3,650, set in the den's settings beside the upload limits. Before a change takes effect, the settings say how many messages it deletes at once, and how much space their files take.
- It covers channels and DMs alike. The den can't read a DM, but it knows when each message was sent, which is all retention needs.
- A message's age counts from when it was sent. Edits don't extend it, and neither do replies: a reply to a message that has passed the period says the original is gone, as with any deleted message.
- The den deletes messages within the hour after they pass the period, the way it deletes anything: rows removed with `secure_delete` on, and files taken off the disk. It deletes the oldest first, in the order they were sent, a few hundred per transaction, so a first pass through years of history never holds the database for long. A change of period starts a pass at once, and so does starting the den, so one restored from a backup deletes what passed the period meanwhile.
- Clients drop what passed the period too. After each pass the den sends one event, not a delete for every message, naming the last message it deleted: every message up to it, in every channel, is gone. Each client drops the messages it holds up to that one, and the files on them, from memory and from its cache (M6). `ready` says how far retention has reached, so a client that was away drops them as it comes back. A channel whose last message went holds none, so nothing in it is unread.
- Members see the period on the den's card on the home page, and a channel's history starts with a line saying how long messages are kept, where it would otherwise say the channel starts.
- Profiles, channel descriptions and uploads waiting to be sent aren't messages, and retention leaves them alone. A backup keeps what the den held when it was made, and outlives the period until it's deleted; the docs tell owners so.

**Compact links (M3)**

When a message is sent or edited, or a channel's description or a member's bio is set, the den rewrites the links in it. Links to Reddit, YouTube and X, and Amazon's product pages, become one short form, which keeps only what identifies the post, video or product, and every other link loses its tracking parameters. In a private DM the sending service rewrites the text before sealing it, since the den can't read it. A rewritten link stays in the text as an ordinary URL, so everything that reads text reads it as before: the markdown subset, mentions, tasks, reply previews, edits and sealed DMs.

| Site | Kept | Written as |
| --- | --- | --- |
| YouTube | video ID, start time (`t`) | `https://www.youtube.com/watch?v=…&t=…` |
| Reddit | post ID, optional comment ID | `https://www.reddit.com/comments/…`, shown on `old.reddit.com` for members who prefer it |
| X | status ID | `https://x.com/i/status/…` |
| Amazon | product ID (ASIN), on the same store | `https://www.amazon.…/dp/…` |

- Tracking parameters come off every link: each site's own (YouTube's `si`, X's `s` and `t`, Reddit's `share_id`, Amazon's `ref` and its search and referral parameters), and a fixed list of those that track on any site, such as `utm_*`, `fbclid` and `gclid`. The protocol lists the forms each site's links take, and every parameter.
- Amazon's referral tag (`tag`) comes off with the rest, as it would with any link cleaner: it pays whoever shared the link, and tracks the click to them.
- A Reddit share link (`reddit.com/r/…/s/…`) stays as it is, apart from tracking parameters. Its code is Reddit's own record of who shared the link, and only Reddit can turn it into a post ID, which would mean Dens fetching from Reddit. Amazon's short links (`amzn.to/…`, `a.co/d/…`) stay as they are for the same reason.
- Hosts match exactly, as a URL parser reads them, so a lookalike link is never rewritten into a real one.
- The page shows links without knowing the rule, so the rule can take in more sites later without a protocol change. Links get shorter too, often by 20 to 80 bytes, but that's a side effect.
- No link previews in v1, apart from YouTube's players for members who turn them on (see YouTube players). Fetching a preview would reveal the den's or members' IPs to whoever runs the linked site, which any member can choose. Could be an opt-in feature for v2. Previews would take a fixed height, with their content scaled to fit, so they never shift the message list.

**YouTube players (M4.1)**

A member can play a linked YouTube video where it's posted, in the message list, rather than in a new tab. It's off until they turn it on in the settings' Links section, beside where Reddit links open, since it means their Dens and their browser contact YouTube. The browser keeps the choice, as it keeps the other.

- With players on, a YouTube link in a message gets a cover under the message, after its files: the video's picture with its title and channel over it, and a play button. Only links in the one form the den writes count (`https://www.youtube.com/watch?v=…`, with `&t=` and a start time if there is one), each video once, and at most three per message. A link in a spoiler gets none, and reply quotes, bios and channel descriptions get none either.
- The cover takes a 16:9 box, 400 pixels wide at most, before anything in it loads, and the player takes the same box, so the message list never moves.
- Clicking the cover swaps in YouTube's player from `www.youtube-nocookie.com`, which plays at once, from the link's start time.
- One video plays at a time: starting another puts the first back behind its cover. A playing video carries on while its message is scrolled out of view, so music and picture-in-picture keep going, and stops once its message leaves the part of the list that's loaded, or the member opens another channel.
- A video whose owner turned off embedding says it plays only on YouTube, with its link, instead of offering to play.

*The cover.* The member's local service fetches the title, channel and picture once a cover comes on screen, and the page shows them from its own origin, so its CSP lets in no image or connection from YouTube.

- The service asks YouTube's oEmbed endpoint for the title and channel, and `i.ytimg.com` for the picture, at addresses it builds from the video's ID: 11 of `A-Z`, `a-z`, `0-9`, `-` and `_`. It follows no redirects, reads at most 64 KiB of an answer and 256 KiB of a picture, and gives up after 10 seconds.
- The title and channel are cleaned as display names are, and shown as text. Nothing else in oEmbed's answer is used, the ready-made player it offers least of all. The picture must be a JPEG, which `internal/media` reads and strips, and the page gets it with the headers of a member's file, so the browser doesn't keep it.
- The service sends no cookies, referrer or user agent. It holds what it fetched in memory for an hour, and a failure for a minute, up to 16 MiB in all; asks once for a video that several covers want at the same time; and never logs a video's ID, which comes from a message.

*Who learns what.* The setting says so, in fewer words:

- YouTube sees the member's IP address for each video whose cover comes on screen, in channels and in DMs, and which videos they play. `youtube-nocookie.com` holds YouTube's cookies back until a video plays; after that, the browser's own rules on third-party cookies decide whether YouTube can tie the play to a Google account.
- Several members of a den looking up one video at about the same time could let YouTube guess they know each other.
- In a DM, a cover tells YouTube which video the member was sent. The den still can't read it.
- Nobody but YouTube learns anything. Its addresses are fixed, so a member can't point anyone's Dens at a server of their own, which is why other links get no previews. With players off, links show as they always have, and nothing on the page contacts YouTube.

*The player's frame.* YouTube's player is a frame from another origin, so the browser keeps it from the page, the page's storage and the local API.

- It's sandboxed to scripts, its own origin and popups: it can't navigate the page, download files or open dialogs. A popup, such as YouTube's "Watch on YouTube", opens in a new tab that can't reach the page, since the page's `Cross-Origin-Opener-Policy: same-origin` cuts it off.
- It's allowed autoplay, encrypted media, picture-in-picture and full screen, and nothing else. The page's `Permissions-Policy` keeps the microphone and camera from it whatever it asks.
- The page builds the frame's address from the ID and start time it checked itself, since a hostile den or sender could skip the den's rewrite. Its CSP takes frames only from `https://www.youtube-nocookie.com`.
- The page listens for no messages from the frame, and doesn't use YouTube's player API, which would need it to.

*The page's address.* YouTube's player needs a `Referer` naming the page that embeds it, and won't play for a page at an IP address: `127.0.0.1`, `[::1]` and LAN addresses all get "This video is unavailable", while host names, `localhost` included, play. So the page lives at `localhost` (see Pairing the browser). The frame sends YouTube the page's origin and nothing more (`strict-origin-when-cross-origin`); everything else the page loads sends no referrer at all.

**Presence at 500 online**

- One WebSocket per member; presence changes are coalesced and broadcast in batches every 1 to 2 seconds.
- Typing indicators are throttled per channel and only sent to members viewing that channel.

## Message list and sync

**Message list**

- A channel view holds one contiguous run of messages, never the whole channel: at most 200, and none that lie more than eight screens beyond what's shown. Scrolling near either end loads the next page (`before` or `after` the edge message) and cuts the far end, so memory stays flat however far back a member scrolls. A page is 50 messages, or as many as fill about three screens when the rows near that end are tall, but at least 10. The cut goes by the rows as they're laid out, so a run of photos, each a few hundred pixels tall, holds a few dozen, where one of short messages holds 200. Rows near the view are never cut, so what the member reads stays put.
- The view is attached to the live tail while it holds the newest message, and new messages append. Jumping to an old message (a reply's quote, a mention, later search) loads the page `around` it and detaches the view. New messages then only update a "new messages, jump to present" bar, and scrolling forward to the newest page reattaches it. A member reading further up a run that's full detaches the same way when the next message arrives, rather than the run growing under them.
- A load that replaces the run (opening a channel, a jump, coming back after a drop) applies again what changed while it was out, since the service may have taken its page first, so a message that arrives as a channel opens isn't lost.
- With the window bounded, every loaded message is in the DOM. There is no per-row virtualization: it would stop a playing video whose row scrolled out of range, and keep the browser's find from seeing what's loaded.
- The layout doesn't jump. Images carry their dimensions from upload, so placeholders take their final size, and loading older pages keeps the view anchored on the message being read. At the newest message, the view stays there while its box resizes or its text rewraps: a growing composer, a rotated phone.
- The read position moves once the newest message has been on screen, at most every two seconds per channel, and at once when the member leaves the channel. A busy channel costs the den one small write every few seconds, not one per message.
- Message IDs increase with time within a den and are never reused, so `before`, `after` and `around` are single index lookups.

**Sync and bandwidth**

Dens usually run on home connections, where upload bandwidth is scarce and every message goes out once per online member. The protocol keeps that fan-out small:

- **Resume, don't refetch.** Every change a member can see is an event with a sequence number. A client that reconnects sends the last one it saw and gets only what it missed, from an in-memory ring of recent events: the last 50,000 within 10 minutes, and from M8 within 64 MiB, so a member sending large messages fast can't grow the den's memory without bound. After a den restart, or a gap longer than the ring, the den sends a fresh snapshot instead (a resync), and the client refetches only what's on screen.
- **Orderly restarts.** A stopping den closes its listener first, then tells every client it is restarting (WebSocket close 1012). Otherwise a quick client reconnects to the dying process, as the M1 spike showed. Clients wait a random delay of up to a few seconds, so 500 of them don't reconnect at once.
- **Scoped ephemeral traffic.** Presence is coalesced into batches every 1 to 2 seconds, and typing goes only to members viewing that channel.
- **Thumbnails, not originals.** Uploads get a small preview when they arrive. The message list shows previews, and originals load only when opened. The local service keeps both, encrypted on disk (M6; see The client's cache), and serves them to the browser with `Cache-Control: no-store`, so they never sit unencrypted in the browser's cache.
- **Compression, measured.** Compressing each message separately (permessage-deflate) saves only 22% on chat events. Context takeover saves 65%, but costs about 820 KB of den memory per connection, around 400 MB at 500 online. A static dictionary shared by client and den, with deflate per frame, reaches 43% of raw with no per-connection memory, and the den compresses each event once for every recipient. Batching events under load brings it to 29%. From M6 the den socket uses permessage-deflate without context takeover, for frames of more than 512 bytes: `ready`, replays and busy batches shrink as history pages do, to under a third, while a single chat event, too small to gain much, goes as it is. It holds no memory per connection: the WebSocket library takes a compressor from a pool for each compressed write. History pages over HTTP are gzipped, to 29% of raw. A dictionary for small frames waits until measurements show live events matter.
- **The client's cache and the changes since (M6).** A client keeps what it fetched from other dens, encrypted on disk, and after a gap asks the den only what changed, so a client restarted after a day away downloads only that (see The client's cache and What changed while away).

**The client's cache (M6)**

A member's Dens keeps what it fetches from other dens: the files and previews the page showed, and from M6.2 the messages it read and those that arrived while it was connected. A photo seen once never downloads again, a channel read before opens without asking the den, and a restart of the client or the den costs only what changed meanwhile.

- The cache is a SQLite database and a directory of sealed files of its own, the data directory's `cache`, apart from the install's database. It holds only copies of what dens hold, so backups leave it out, a new format of it starts again from empty rather than migrating, and its writes never wait behind the den's on an owner's install.
- Everything in it is sealed with the data key: each message, and each 64 KiB chunk of a file, bound to its den, its ID and its place, so nothing moves between entries unnoticed. A chunk that doesn't open takes its file out of the cache, and the page gets the file from the den. A cache sealed under another key, or of another format, is emptied, not read.
- The install's own den isn't cached: the client reaches it over loopback, and its files are on the same disk, so a cache would only double the owner's storage. The client keeps the small memory cache it always had for it.
- It holds 2 GiB unless the member chooses another size, from 512 MiB to 50 GiB, in the settings' General section, which also shows how much it holds and empties it. Past its size, what was used longest ago goes first: a whole file, or from M6.2 a channel's messages, by when the member last read the channel. A file larger than a quarter of the size isn't kept, and a channel keeps its newest 20,000 messages at most, so a busy one followed for years can't take the whole cache.
- A file is kept as it's read, a chunk at a time, so a video keeps the parts that played, and seeking fetches only the chunks it lacks. The chunks are the 64 KiB the den seals its uploads in, so a fetch decrypts exactly the den's own chunks. They're written in the order they arrive, each with its index, rather than at their place in a sparse file, which Windows would allocate in full. A file being read stays until its last reader closes it, since Windows can't delete an open file.
- What the page asks for comes from the cache first and the den second, and what the den sends goes into the cache on its way to the page. Files reach the page with the headers they always had, so the browser still caches none of them.
- Each cached file records what it's on: a message and its channel, or a member's picture, which the client notes from what it passes the page. A deleted message, a file taken off one, a deleted channel or one the member no longer sees, a retention pass and a replaced picture each take out exactly their files. A file the client can't place, it doesn't keep, since it couldn't tell when the file goes.
- A den's cache goes when the member leaves it, is removed or banned, or the device is signed out, and when the member forgets the den.
- Messages (M6.2): the cache holds them as the den sends them, a DM's still sealed, and opens them as the page reads them, with the keys the client already holds. Beside them it keeps runs, the ranges of IDs in a channel for which it holds every message the den has. IDs increase with time, so an old range can lose messages but never gain one.
- A page of history comes from a run that covers it, and otherwise from the den, which then joins the cache, run and all. While connected, every new message goes into the cache, in every channel the member sees, so a channel that was busy while they were online opens without a request.
- A run that reaches its channel's newest message says so for one connection, the one it reached it in: while that lasts, every new message arrives as an event and joins it, so the newest page comes from the cache. A snapshot starts a new connection, and the runs that still reach their channel's newest message, as its read states say it, carry on in it. A channel that had new messages while the client was away asks the den for those after the newest it holds, a page or two, rather than its whole newest page; past two pages, it takes the newest page, and leaves a gap the member fills by scrolling.
- The cache follows the page's rules for a page fetched while events arrive: the higher revision wins, a message deleted meanwhile never comes back, and nothing retention reached goes in. A reply's quote follows its original as it changes or goes.
- The client keeps the den's state with it: its members, channels and groups, read states and DM keys. It names the version its list of members is current as of on the socket's upgrade, and `ready` then sends only the members changed since, which with 1,000 members is most of its size (M6.2).
- Offline reading (M6.2): a den the client can't reach shows as it last was, and what the cache holds of each channel reads as usual. A note over the messages says the den is offline and these are the messages this computer kept; it shows once the den has failed to connect, not while it connects, as at every start. A page the cache doesn't hold says the den is offline at once, rather than waiting out the den. Sending, calls and anything else that needs the den wait for it to come back.
- After a snapshot, the changes since reach the page too, as edits and deletes would: it may have loaded a channel from the cache before they came.

**What changed while away (M6)**

A client away from a den longer than its ring of events, or across the den's restart, gets a fresh `ready`, and needs to know what changed in what it cached. The den doesn't keep its events for that: they carry every message's text, so a store of them would be a second copy that every delete and retention pass would have to find, and replaying it would send a day of every channel, read or not. It keeps a change log of IDs instead.

- The log records each message edited or deleted, with its channel, and from M6.2 each member whose record changed: one row for each, its latest change, numbered in the order the changes happened. The newest number is the den's version. A retention pass and a deleted channel add nothing, since `ready` says how far retention reached and a deleted channel is missing from it.
- `ready` carries the den's sync epoch, its version, the floor (the oldest version a client may still ask from) and the last message retention deleted. A client that synced in the same epoch at a version no older than the floor asks for what changed since: the messages edited or deleted since then, up to the newest it holds, in channels it can see now. That is usually a few hundred bytes, in one request.
- The den filters by what the member can see now, not when the change happened, so a role change, or a channel that became staff-only and back, needs nothing special: two visibility levels and DMs make that one condition. A channel missing from `ready` takes its cache with it.
- New messages aren't in it. A channel the member opens fetches its newest page, and pages back from there, as it always did, from the cache where it can, so a busy channel nobody opens costs nothing.
- The log keeps 30 days of changes. A client whose version is older than the floor, or from another epoch, drops what it cached for the den and starts again. A client connected all along asks for the changes once a day, which keeps its version within the 30 days, and applies them as it applies events: twice changes nothing.
- A den restored from a backup starts a new epoch. The backup carries its database's counter of message IDs, so the restored den can give a new message an ID that a client holds for another message, one the backup never had; a client of another epoch drops its cache for the den instead.
- Live events still keep a connected client's cache current, and the ring still resumes a short gap without a `ready`. The change log covers what the ring can't.

## Files and media

Members attach files to messages and put pictures on their profiles. Images lose their metadata before they leave the member's machine. Photos and videos go as smaller copies unless the member sends them full size, and everything fits within owner-set limits that members see and manage (M5). Files in DMs are the exception from M1.7: the sender's service strips and encrypts them before upload, and the den stores only opaque blobs (see End-to-end encrypted DMs).

**Limits (owner settings)**

- Max size per file, per member total, and den total. A new den starts at 25 MiB per file, 2 GiB per member and 20 GiB for the den. The page checks a file's size before uploading it, and says why when the den refuses one.
- Uploads also stop while the den's disk has less than 1 GiB free, so they never fill the disk the database lives on.
- The limits for calls and screen shares live in the same place (M4.2; see Limits for calls).

**Metadata stripping**

- EXIF, GPS and similar metadata are stripped by the uploading member's own service, as the file streams to the den, so the den never receives a photo's location, and neither does its owner. The den runs the same check and refuses an image that still has any, which holds other clients to it.
- Images: removal without re-encoding, the same code on both sides (`internal/media`). JPEG, PNG, GIF and WebP keep only what decoding, color and animation need, and a JPEG keeps its orientation in a minimal EXIF block of its own; the protocol lists what stays. Stripping never touches pixels, so an image sent full size loses nothing; a smaller copy is a new image (see Smaller copies).
- Video and audio need ffmpeg, to copy their streams into a new container without the metadata (what `ffmpeg -map_metadata -1 -c copy` does), and so do the photo formats that can't be stripped without decoding them: HEIF (as iPhones save), TIFF, JPEG 2000 and Photoshop files, which the sender's machine turns into a JPEG, or a PNG when they have transparency. AVIF, JPEG XL and camera raw are refused, and the member told why, rather than sent with a location nobody took out: AVIF until after v1, when Dens strips it in place, and the other two because the module can't decode them. Dens doesn't strip video containers with code of its own. MP4 and Matroska hide metadata in many places, and ffmpeg already handles them, so there's one stripper to get right, not two.
- A video or audio file is stripped twice: by the sender's service, so the den never receives what it carried, and again by the den, which stores its own copy. The den can't check a container for metadata the way it checks an image, since FFmpeg's demuxers skip boxes they don't know, but nothing comes out of FFmpeg's muxer that the driver didn't copy. A photo in a format the module converts reaches the den as a JPEG or PNG, which the den checks as any image, and its name ends in the new format's extension.
- Video and audio in containers the module doesn't read, such as AVI, ASF, FLV and MPEG transport streams, are refused, as is a TIFF that holds camera raw.
- Other files, such as documents and archives, are sent as they are, with whatever they carry inside; the docs say so.
- Each file the member attaches shows a mark when metadata came out of it, which says on hover what metadata can hold. There is no opt-out in v1.

**Previews**

- The den makes an image's preview from what it stored: a JPEG, or a PNG with transparency, fitting 640 × 640, upright, from an animated image's first frame. One image decodes at a time, within a 256 MiB budget that fits a 50-megapixel phone photo; a larger image goes without a preview and downloads like any other file.
- A video's preview is its first frame, made the same way with ffmpeg from the copy the den stored. The module decodes the H.264 and HEVC phones record, but not VP8, VP9 or AV1, for their size, so a WebM or AV1 video's preview comes from the sender's page, which draws the first frame the browser plays and sends it beside the upload. The den decodes that image and makes its own preview from it, so it keeps nothing the page made as it was. Without one, the list shows a placeholder of the video's shape, which the container states without decoding. Audio has no preview.
- Uploads state a video's size as it displays, after its rotation, and its duration, so the list lays it out before it loads.
- Uploads state an image's size as it displays, so the message list gives a preview its final box before it loads (see Message list).

**Storage**

- A den encrypts uploads with its data key, like message text: 64 KiB chunks of XChaCha20-Poly1305 in the STREAM construction, so files of any size stream in and out in bounded memory, and a file cut short, reordered or swapped for another doesn't open. They sit under random names that only their database rows know, in the data directory's `uploads`, which backups include.
- A sealed file opens at any chunk: each 64 KiB chunk's nonce comes from its position, so reading from an offset decrypts only from the chunk it falls in.
- A file's ID is a den ID like any other, and its bytes never change under it, so clients can cache it by ID. IDs are not content hashes, which would let a member test whether a file they have is somewhere they can't see.
- An upload waits an hour for a message or a profile to use it. Deleting a message, a channel or a member's messages deletes their files, and a member who leaves loses their pictures and unused uploads.

**Smaller copies (M5)**

A phone's photos and videos hold far more than a chat shows. The viewer fits a photo to the window, and a 12-megapixel photo has several times the pixels a screen shows there. So the member's Dens sends a smaller copy of each photo and video unless they send it full size. Each member's space and the den's then last several times longer, and uploads finish sooner on a home connection; Signal, WhatsApp and Telegram send photos smaller by default for the same reasons. The sender's machine makes the copy, never the den, which couldn't make one for a DM anyway: the local service makes it with the media module, or a video's, where the member's browser can, the browser does, with its own encoders, many times faster (M5.5). Nothing changes a file once it's sent, so the choice is made before anyone sees it.

- It's on unless the member turns it off in the settings' General section ("Send photos and videos smaller"), which the browser keeps.
- A copy is made as the file is added to a message, before it uploads, since uploads start then and the message sends as soon as they're done. The local service makes both versions then: the full size, as the photo goes without a copy, which is the file stripped, or for a photo Dens converts, a JPEG or PNG of all its pixels; and the copy. It uploads the one to send, and keeps both, each in a scratch file sealed with a key of its own, until the message is sent, the photo is taken off it, or the hour a den keeps an upload waiting is up. A twelve-megapixel JPEG's copy takes about a third of a second, and each of a HEIC's two versions about half a second.
- Each file's chip shows the size it will send, and whether that's the smaller copy or full size. A photo the browser can't draw, such as a HEIC, shows the preview its upload has.
- A photo's copy fits 2,560 pixels on its longer side, never enlarged, upright, with its ICC profile. JPEGs, and the photos Dens converts (HEIF, TIFF, JPEG 2000 and Photoshop), become JPEGs at FFmpeg's quality 5, or PNGs when they have transparency. A PNG, usually a screenshot, stays a PNG and is only scaled down when it's larger than that, so its text stays sharp. The module's still makes each copy, decoding JPEGs and PNGs for it too. WebP stills, GIFs and anything animated go as they are.
- A copy goes only when it's at most three quarters of the full-size file's size. Otherwise the full-size file goes, as a smaller copy downloaded and sent again does.
- A photo or video over the den's size limit goes as a smaller copy whatever the setting, rather than being refused, when the copy fits, and its chip says why. A video's copy then takes the bitrate that fits the limit (see Smaller videos).
- On the ten phone photos among the media spike's fixtures, copies are 58 to 94% smaller than the full-size files, 82% in all, with an SSIM of 0.96 to 0.99 against the same photo scaled without loss. Lossy WebP came out the same size and a little sharper, which isn't worth an encoder the module lacks. AVIF came out half the size, but dens refuse AVIF until it's stripped in place after v1, and libaom took about 15 seconds a photo without SIMD, on one thread.

*Comparing before sending.* Clicking a chip's size opens the file in the viewer, to compare its two versions and pick one:

- It first shows the photo as the viewer shows everyone, fitted to the window, where the two look the same: at 2,560 pixels, a copy has about as many as the viewer shows on a 4K screen, and more than it shows on smaller ones. A second view shows the full-size photo at 100%, one of its pixels on each of the screen's, with the copy scaled up to match, as someone who downloads it would see it, and the member drags to look around. A click on the fitted photo looks at that spot at 100%, and a click at 100% fits it again.
- A switch flips between the versions in place, each labeled with its pixels and size, and holding Space shows the other one while it's held. A difference shows best when the picture changes in place, not side by side, where the eye compares two spots.
- It says how much of their space the member uses on the den, since whether full size is worth it depends on its cost as much as on its quality.
- "Send full size instead" switches the file, and then reads "Send smaller instead". It can do the same for the message's other photos, and it's disabled, with the reason, when the full-size file is over the den's limit. Switching uploads the other version and drops the one waiting. ← and → move between the message's photos.
- The page shows both versions from the local service, which kept them as the photo was added, so a HEIF, TIFF or Photoshop file, which browsers can't draw, compares like any other.
- A video compares as a photo does, its two versions playing in place, paused at the same moment, with each version's size, pixels and frame rate (M5.4). The copy is AV1, which every target browser plays; the full size is what the phone recorded, which a browser plays only if it decodes it: one that doesn't, such as Chromium without graphics that decode HEVC, which iPhones record, shows the copy alone and says why.

*Smaller videos (M5.4, M5.5).*

- The local service makes a video's copy with the media module, which carries libaom's realtime AV1 encoder (M5.4). As the video is added, it strips the video as it does today, which is its full size, plans the copy, and encodes it in chunks, each made in a worker of its own, as many at once as half the computer's cores, at the lowest priority, so calls keep their CPU. Each worker takes one chunk, or as many as keep each within ten seconds, cut at the keyframe nearest each cut where one is near: every chunk starts with a keyframe, which costs bits, so fewer chunks look better, and on the iPhone's HDR video one chunk a worker came out both faster and about two points higher (VMAF) than two. It muxes the chunks with the video's sound, copied as it is, into an MP4 that keeps the video's turn, and from there a video's copy goes as a photo's does: three quarters of the size or less, both versions kept until the message goes, and switching uploads the other. A copy that wouldn't come to three quarters, judged by the full size's bitrate, isn't made.
- Where WebCodecs decodes the video and encodes AV1 well, which in the video spike was Chrome and Edge, the page makes the copy instead, often in the GPU and many times faster (M5.5). A browser built on Chromium says so as it uploads the video; Firefox's encoders run slowly and miss their bitrate too far to fit a limit, so Firefox and Waterfox leave the copy to the module. Once the video is stripped, the local service offers the page the copy: what decoding the video stream's packets takes, and how to encode the copy. A browser that can't decode the video, such as Chrome without graphics that decode HEVC, which iPhones record, or can't encode the copy, declines, saying why, and the module makes it.
- The page takes the packets from the local service as it decodes them, keeps at most the frames a second the copy has, as the module does, crops each as the container asks, and hands it to the encoder at its own size, which scales it: drawing each through a canvas first cost the spike 15 to 20 points (VMAF). The encoder aims at the plan's bitrate, for quality rather than speed, with a keyframe every five seconds as the module's copies have, in hardware where the browser has it. The page sends the AV1 packets back as they come, and the local service checks them as they arrive, into a sealed scratch file, then muxes them with the video's sound as it muxes its own chunks, and checks the MP4 as it checks its own copy.
- The page's copy is at most 1080p (1,920 pixels on its longer side). Fitting a limit, it takes four fifths of the room where the module's takes nine tenths, since a browser's encoder misses its bitrate by more, and steps down through 720p to 480p as the module's does. A copy the page doesn't finish, the module makes: one it declines or fails, an offer it doesn't answer within ten seconds, a copy it sends nothing of for half a minute, packets that break the rules, or a page that goes away. One over the limit is offered once more, lower, as the module makes its own again.
- The module's scan reads a video's packets without decoding them, for its keyframes' times, its frames and its bytes, which the plan goes by. Its encode decodes a chunk from the keyframe before its start, keeps at most 30 frames a second, skipping the frames nothing refers to when a 60-frame video is halved, scales each with a bicubic filter, which costs a twentieth more time than bilinear and scored about two points higher (VMAF), and encodes them with libaom at its fastest realtime speed, starting with a keyframe, at the video's own times, so the copy keeps time with its sound. Its mux writes the chunks' packets and the original's sound into an MP4 with the original's turn, and refuses chunks whose configurations differ.
- The page follows each upload over a WebSocket of its own, opened as the upload starts, which says how far its copy and its upload have come: a request would wait behind the browser's six connections to the local service while uploads hold them, and Firefox reports a request's body sent only once the whole request is over. The page's copy goes over the same socket. The local service offers it, the page accepts or declines, then asks for the video's packets a window at a time, saying how many bytes it can take, and sends the copy's packets back as records: each packet's size, flags, time and duration. The local service takes only whole records, of a size a packet can be, with times only increasing and a keyframe first, within the largest file a den takes.
- The video spike (`spikes/README.md` at commit 98a9441) measured both ways on the media spike's phone videos. The module's encoder, without SIMD or threads, made about 5.5 frames a second at 720p and 2 at 1080p on a fast desktop, and workers side by side scaled to 35 with eight. WebCodecs in Chrome and Edge with an RTX 4090 decoded an iPhone's HEVC in hardware, HLG included, and encoded AV1 at 150 to 370 frames a second in hardware and about 100 in software, within 20% of the bitrate, at VMAF 86 to 93. Firefox and Waterfox decoded no HEVC, and encoded at 6 to 17 frames a second, two to four times over the bitrate. AV1 at 720p and 1.2 Mbps scored VMAF 70 to 84 on the short phone videos.
- On Windows with an RTX 4090, Chrome and Edge made the iPhone's 22-second 1080p HDR video into a 1080p copy in 5.5 seconds, decoding and encoding in hardware, at VMAF 83 and 2.6 Mbps against the 2.5 planned, where the module takes 24 seconds for its 720p at VMAF 66. Chromium on Linux, without graphics, made an iPhone's H.264 video's copy in software in 1.3 seconds, where the module takes 7.6.
- A copy is AV1, which every target browser plays, at 30 frames a second at most and about 0.04 bits a pixel each frame, which is 1.1 Mbps at 720p and 2.5 at 1080p. The module's copy is at most 720p (1,280 pixels on its longer side, never enlarged), since its 1080p is more than twice as slow: on an eight-core desktop it made the iPhone's 22-second 1080p HDR video, at 60 frames a second, into 720p in 24 seconds, about 27 frames a second, a tenth of its size at VMAF 66, and a four-core laptop takes a few times that. A browser's copy is at most 1080p. The video spike measured AV1 against VP9, which came out worse at the same size and overshot its bitrate; H.264 was never in it: x264 is GPL.
- A video over the den's limit gets the bitrate that fits it, with room for its sound and for the encoder overshooting, stepping down to 720p and then 480p as the bitrate falls. Below a floor it's refused as too long for the den, with about the longest it takes. A copy that comes out over the limit anyway is made once more at a lower bitrate.
- The copy keeps the video's turn, as stripping does, and drops a crop the container asks for by cropping each frame before it's encoded. An HDR video, as iPhones record, gets an eight-bit copy keeping its HDR and BT.2020 tags, which browsers tone-map: the spike found it as good as a ten-bit one. The page's encoder takes an HDR video's ten-bit frames as they decode and leaves the tags out of its AV1 stream, so the page's copy carries them in the MP4 alone, which Chrome, Firefox and FFmpeg follow.
- A video the module doesn't decode, VP8, VP9 or AV1 as a WebM holds, gets a copy only from a browser that does (M5.5). Without a copy, a video goes full size if it fits, and is refused otherwise, saying why.
- A copy takes a while, unlike a photo's, so the chip shows its progress, and then how much has gone to the den, and the message waits for it as it waits for uploads. While it's made, the chip offers to send the video full size instead, when that fits the den. A video sent full size by the setting gets no copy at all, which would take minutes to make for nothing, so it can't be compared; one over the limit gets its copy either way.
- The den can't decode AV1 to make the copy's poster, so the sender's Dens makes one from the full-size video's first frame, with the module, and sends it beside the copy. The page draws a WebM's, from its copy where it made one, as it does for a WebM sent full size.

**Managing files (M5)**

- Each den on the home page has a Files section for the member's own uploads there: how much of their space they use, and of the den's, and their files, largest first, each with what uses it: a message, with its channel or DM and the start of its text, a picture on their profile, or nothing while it waits to be sent. Open shows a file's message in its channel.
- A DM's files list the same way: the den knows only their sizes and the message that holds them, and the member's Dens opens the messages for their names and previews. A DM file's preview is a blob of its own, which goes with its file rather than in the list.
- A member deletes a file, which takes it out of its message, or swaps it for another they upload, which takes its place there, made smaller like any other unless they send it full size. A message left with neither text nor files is deleted. A picture on a profile is taken off from the list, and changed on the profile, whose new picture names the one it replaces, so that works at the limit too. A waiting upload is dropped.
- A swap works for a member at their limit: the replacement's upload names the file it replaces, and the den counts it against the space that file frees. It can only take that file's place, and goes within the hour if it doesn't.
- A file's bytes never change under its ID, so a delete or a swap is an edit of the message: it marks the message edited, and lists the file that's gone, so clients drop their copies. In a channel the den makes the change. In a DM, where each file's key and description live in the sealed text, the member's Dens seals the message again without the file, or with its replacement.
- A member drops an upload they won't send, by taking it off the composer or switching it between its smaller copy and full size, and its space is free at once rather than within the hour.

**Serving**

- Everything downloads through the local service, which keeps what it fetched from other dens in its cache (M6). It serves a file inline only when its own look at the bytes finds an image of one of the four kinds, or video or audio in a container the module writes, and then as exactly that type; anything else, SVG and HTML included, goes out as `application/octet-stream` with `Content-Disposition: attachment`. Every file carries `X-Content-Type-Options: nosniff` and `Content-Security-Policy: default-src 'none'; sandbox`, so even one opened on its own can't run anything. The page's policy allows media from itself (`media-src 'self'`).
- Clicking an image opens it in a viewer over the page, fitted to the window, with no zoom: the preview at once, and the original as it loads. When a message holds several images, ‹ and › at the viewer's sides, and the ← and → keys, move between them, and it shows which one it's on, as "2 of 5"; they stop at the ends (M5).
- Video and audio play as they download: the den answers byte ranges of a file, decrypting only the chunks a range covers, and the local service passes ranges through, from M6 keeping the chunks they cover, so the page's player starts at once and seeks. A DM's video works the same way, the local service fetching the sealed chunks a range covers and opening them with the file's key.
- Videos in the message list play with controls of the page's own, since browsers' own can't be kept up: they stay while a video is paused, showing where it stopped and how long it is, and while it plays they hide once the pointer rests or leaves. Audio keeps the browser's controls.
- A video's volume is a mute button, which shows a vertical volume slider on hover or keyboard focus, at any width (M3.3). The level is every video's: the browser keeps it, and every video follows it, playing or still to start, while muting stays each video's own. A video too narrow for its time beside the seek bar shows none; full screen shows it.
- The den serves every file as bytes to download, and never states a type a browser would act on.
- A file's name is text from a den: shown as text, and cleaned before it names a download.

**ffmpeg**

- ffmpeg is compiled to WebAssembly and translated to Go with wasm2go, the way the SQLite driver is built. One pure-Go build serves Linux and Windows, with no native binaries to vendor, and it runs under `MemoryDenyWriteExecute`, which rules out a WebAssembly JIT.
- The module holds FFmpeg 9.0's libraries and a small driver of Dens's own in C, not the `ffmpeg` command, which needs threads since FFmpeg 7.0 and parses options Dens has no use for. The driver exports what Dens does with media: probe, strip, still and poster; from M5.4, scan, encode and mux, which plan a video's copy, make it in chunks and put them together; and from M5.5, demux, which passes a video's packets to the page for a copy of its own. FFmpeg keeps only what those need: the containers phones and browsers use, and the image formats' own demuxers; the HEVC, H.264, MJPEG, PNG, TIFF, JPEG 2000 and Photoshop decoders; swscale; and the JPEG and PNG encoders, with zlib, and from M5.4 libaom's AV1 encoder, built for realtime alone in eight bits. VP8, VP9 and AV1 keep their parsers, which give a WebM's size without decoding it.
- Stripping copies the video and audio streams into a new container, and nothing else: no metadata, chapters, attachments, cover art, data tracks or subtitles, which some cameras fill with coordinates. A stream's side data comes along only from a list of what playback needs: rotation, cropping, stereo and 360° layouts, HDR's light levels and mastering display, Dolby Vision's configuration, and the ICC profile, as Dens keeps it in images. The copy names no encoder. A MOV is written out as an MP4, which Chrome plays where it won't play a MOV, and which can carry a Dolby Vision configuration, as FFmpeg writes it into MP4 when allowed unofficial boxes. The SEI user data H.264 and HEVC carry inside the stream, which iPhones write in every frame (19 bytes, no readable text), is dropped by the driver as it copies each frame, since Dens can't tell what's in it.
- A still assembles a HEIC's tile grid, crops it, turns it as its display matrix says, the way the ffmpeg command reads one, and keeps its ICC profile. A JPEG's turn is in its EXIF, which FFmpeg's demuxer doesn't read, so Dens passes the orientation `internal/media` found, and the driver picks the JPEG or PNG demuxer by the file's first bytes rather than trust FFmpeg's guess. A PNG stays a PNG, written with each row filtered the way that packs it smallest. The encoder gets a fresh frame carrying only that profile, so nothing else the source carried, EXIF included, reaches the file. It holds at most two full images at a time, so a 48-megapixel photo fits in 512 MB. A poster is a video's first frame, cropped as its container asks and turned. HDR video isn't tone-mapped, so an HDR video's poster looks washed out.
- The module is its own sandbox. It sees only its linear memory and the few functions Dens gives it: the input's bytes in and the output's bytes out, with no files, network or processes. A hostile file that takes over a decoder is stuck in the module's memory, which matters for a library parsing this many formats. The host keeps it so:
  - It answers the WASI calls wasi-libc makes with the clock, randomness and a log, and refuses any that would open a file, list a directory or reach a socket.
  - It reserves the module's memory up to the cap, and hands it over as a slice whose capacity is its length. wasm2go checks `memory.fill`, `memory.copy` and `memory.init` against a slice's capacity, so spare capacity would let them write past the end instead of trapping.
  - It starts the module at the size its memory import declares.
  - It recovers a trap, which wasm2go turns into a Go panic, as the end of the job.
- It runs in a worker process, a hidden command of the same binary, with a memory cap, a deadline, and a Go stack limit well below the default, so a decoder that loops, balloons or recurses ends its job and not the service. The worker holds no keys and opens no files: the module's reads, writes and seeks go to the service over the worker's standard input and output, and the service answers from the files it holds, the den's sealed with its data key. Jobs run one at a time at the lowest CPU priority so calls and streams keep their CPU: `IDLE_PRIORITY_CLASS` on Windows, and on Linux nice 19 plus `SCHED_IDLE`. A video copy's chunks are the exception (M5.4): they run in workers side by side, as many as half the computer's cores, at that same priority, so they take only time nothing else wants. Linux nice values and scheduling policies are per thread, so the service sets them on the thread that starts the worker process (then discards that thread), and every thread the worker starts inherits them. There is no transient cgroup scope: the sandboxed service account can't create one, and a niced child already gets a small share next to the service's own threads.
- A file the sender's service strips or converts spools first into a scratch file sealed with a key that lives only for the job, in blocks it can rewrite in place: the MP4 muxer writes its index last, and the driver then moves it to the front, so a player starts before the whole file arrives. Nothing of a member's file lies on disk in the clear, even for a moment.
- Dens probes and strips media, and makes stills, previews and smaller copies. It re-encodes a photo when browsers can't show its format or Dens can't strip it in place, as a HEIC, which becomes a JPEG on the sender's machine, and for a smaller copy, which the sender can decline (M5). Stripping copies streams, and runs at the disk's pace in the module; a still or a preview decodes one image or frame.
- Dens doesn't run a native ffmpeg confined by the operating system instead. That would take confinement written and tested for each OS, and a second program to build, sign, ship and keep patched, where the module holds a hostile file the same way on every platform, inside the service's own sandbox.
- The module holds a bug inside its memory, but doesn't find it: one that corrupts the module's own heap can still give a wrong result without a trap. So Dens's own C is fuzzed natively under AddressSanitizer with damaged files, beside the module, in its tests.
- The build leaves out GPL-only parts such as x264, so ffmpeg's terms stay LGPL beside Dens's MIT. Its source and build script ship with Dens, which lets anyone rebuild the binary with a changed ffmpeg, as the LGPL requires: each release publishes FFmpeg's exact source beside the binaries. wasi-sdk, binaryen, FFmpeg, zlib, libaom and CMake are pinned like the other build tools, and wasm2go through Go's checksum database.
- The translation is generated Go, which the repository keeps, as go-sqlite3 keeps SQLite's: a script with the pinned tools regenerates it, and CI regenerates it and fails if it differs. Building Dens needs nothing beyond Go, and a change to the module is a reviewed diff of its inputs.
- What it costs, as measured in the spike after M1 (`spikes/README.md` at commit 3cd230d): the module adds about 26 MiB to a binary and compiles cold in 20 seconds within 5 GB. libaom adds about 17 MB more, and takes a cold compile to about 8 GB (M5.4). Stripping runs at 0.5 to 2 GB/s, and a 4 GB phone video takes under 70 MB, since MP4's frame index grows by about 16 MB per GB; Matroska's stays flat. A 12-megapixel HEIC becomes a JPEG in 0.5 to 0.7 seconds, and a 1080p video's poster takes a quarter of a second; the translation runs at 1.3 to 1.9 times native.

## End-to-end encrypted DMs

From M1.7, DMs and the photos in them are end-to-end encrypted: the den stores and relays them but can't read them. Channels stay readable by the den, since staff moderate them and the den does the work that needs their text: mentions, reply previews and compact links.

**What it protects**

- The den's owner, and anyone holding the den's disk or backups, can't read DMs or see the photos in them.
- Keys pass through the den, so a den could hand out keys of its own and sit in the middle of a DM. The check code catches that before the DM's first message (see Starting a DM).
- Metadata stays visible: the den sees who DMs whom, when, and how much, and it can hold messages back.
- Members who compare check codes over a channel the den's owner controls, or with someone pretending to be the other member, aren't protected. Nothing here is bulletproof, and the app says so.
- There's no forward secrecy: a stolen device, or a leaked seal together with a copy of the den's data, opens all of that member's DM history.

**The DM seal**

All of it uses Go's standard library, plus the XChaCha20-Poly1305 that already seals data at rest.

- Each member has a DM seal: 256 random bits, made by their Dens the first time they join or create a den, and kept in the vault. It never reaches the den. Their other devices get it through approval (see Approving new devices), sealed to that device.
- One seal serves every den an install joins. Each den's DM keys are sealed under a key derived from the seal and the den's ID, so no den sees the seal, and what one den stores is useless at another. An install that joined dens before it was linked to the member's other devices can hold a different seal for some dens; each den's DMs use the seal they were set up with.
- The page shows the seal beside the recovery codes whenever the member joins a den, saying when it's the one the install's other dens use, and again on request after the local password: "This protects your end-to-end encrypted direct messages. Keep it secret and don't share it with anyone. You'll need it to read your DMs if you lose every device."
- The seal never changes. A member who loses it and every device starts over (see History and starting over).

**Starting a DM**

1. Alice presses Message on Bob's profile, and her Dens starts an exchange of one-time keys: X25519 and ML-KEM-768 used together, so a DM stays safe if either is broken, and ML-KEM guards against traffic recorded now and decrypted later by a quantum computer.
2. The exchange runs in commit-first order, through the den, as three messages that each Dens sends when it's online: a commitment to Alice's key, Bob's key, then Alice's key. Alice's key is fixed before she sees Bob's, and Bob's is sent before he sees Alice's, so a den in the middle can't choose keys that make both sides' codes match. It gets one blind guess. Between messages, each side's state waits on the den sealed with its member's seal, so whichever of their devices is online moves the exchange on.
3. Each Dens derives a 32-digit check code from the exchange. Alice's screen shows the first 16 digits and asks for the last 16; Bob's shows the last 16 and asks for the first 16. Each reads their digits out to the other and types the other's. Neither screen shows what its member types, so the check can't be passed by copying one's own screen. Each screen keeps its digits until the other member has typed them, so whoever checks first can still read theirs out.
4. Each Dens opens the DM only once its own member typed the other's digits correctly. Neither trusts the other side's confirmation, which would come through the den. Digits that don't match mean someone may be in the middle, and the exchange can start again.
5. The exchange's shared secret becomes the DM key. Each member's Dens seals its copy with their seal, bound to the den, the DM, the member and the key's ID, and stores it on the den once its member has checked. That copy is what lets them send with the key, and their other devices read with it. Alice can send as soon as she has checked; Bob reads it once he has too.

The page says where to compare, best first: in person; a voice or video call; another den one of them owns, never the den the DM is on, even when it's theirs, since whoever tampered with it could change the digits sent there too; several accounts on other services they know are really the other's; a den neither of them owns, whose owner could change what's there. It also says why: a tampered den can set up an impostor elsewhere ("I'm @Y on Twitter, let's compare there"), and hosting your own den narrows that risk, but nothing removes it.

**Messages and photos**

- The local service encrypts a DM's text with the DM key, bound to the channel, the author, the message's nonce and the key's ID, before sending, and decrypts what arrives before the page sees it. The browser never holds a key.
- The den keeps doing everything that doesn't need the text: ordering, history pages, edits with revisions, deletes, read state and unread counts (in a DM every message counts, so the den needn't read one), typing and closing.
- Photos: the sending service strips metadata and makes the thumbnail itself, with the same code the den uses for channel uploads, then encrypts the original and the thumbnail with a fresh key per file. The file's key, dimensions and type travel inside the encrypted message. The den stores two opaque blobs and counts their size against the upload limits.
- Work that moves to the client: reply quotes (the client decrypts the original itself), search, and compact links. Task checkboxes (M1.6) tick as an ordinary edit against the current revision.
- Calls aren't covered. Voice and screen share pass through the den's SFU, which can decrypt media hop by hop; end-to-end encrypted calls would need SFrame (insertable streams), after v1.

**History and starting over**

- A new device gets the seal from the device that approves it, fetches the member's sealed DM keys, and reads their DM history. A member signing in with a recovery code, with no other device, types their saved seal.
- DM keys don't change on their own. Keeping history costs forward secrecy: whoever gets a device, or the seal, can read what that member can. Signal makes the opposite choice, giving new devices no old messages; Dens keeps history, since members expect their DMs on every device, as with channels.
- A member who has lost their seal and every device starts over: their Dens makes a new seal, and the DM history sealed with the old one stays unreadable to them, while their partners keep theirs. Each of their DMs needs a new exchange and check before anything more is sent in it. Both sides then show a divider where the keys changed, saying who started over, when, and when the two checked again. A DM can have several.
- Starting over takes the den password, so a stolen session can't wipe a member's DM keys. It signs out their other devices there, since those hold the old seal, and they come back through approval. The new seal is the one the install starts new dens with from then on.
- A former member who joins again with another seal than the one they left with starts over the same way; with the same one, their keys and history are as they left them.
- Starting over is also how a member shuts out a stolen device, which holds the seal: signing it out stops it fetching from the den, and a new seal keeps what's written afterwards from it.

**In the app**

- The channel list has two tabs: Den and Direct messages.
- DMs say they're end-to-end encrypted, with a lock. Onboarding and the docs say the den can read channels; the page doesn't repeat it in each one (M8.3).
- A DM waiting for its check shows who started it, the digits to read out, a box for the other's digits, and the advice on where to compare.

## Voice and screen share

The browser does all client-side media with standard APIs, and the den runs a Pion SFU that forwards packets without decoding. In the client role, the service only relays signaling and never touches media; the Pion SFU runs only when the same install is also acting as a den.

**Calls (M2)**

- Calls happen in voice channels. Clicking one joins its call: the member's browser opens one `RTCPeerConnection` to the den, which carries their microphone to the den and everyone else's audio back.
- Anyone who can see a voice channel can join its call, and sees who is in it, listed by name, and who is muted. A staff-only voice channel's call is staff's, as its text channels are.
- A member is in one call at a time, on one device: joining from another device, or in another channel, moves them there. An install holds one call, which belongs to the page that joined it, so closing or reloading that page leaves the call.
- The den ends a member's call as soon as they can't see its channel: they leave or are removed or banned, the device is signed out, a role change hides a staff-only channel, or the channel is deleted.
- A call rides out a dropped connection (M3): the den holds it for 30 seconds after its socket closes, and the page takes it back once it reconnects (see Riding out a dropped connection). A den restart ends every call, and the page joins again once the den is back, trying a few times before it gives up and says why.
- Mute is the member's own: the page stops sending their microphone's sound, and the others see the mark. So is deafen (M3.3): the page plays nothing of the call and mutes the member, and the others see that they hear nothing either. Hearing again puts the microphone back as it was, and unmuting while deafened means hearing again too.
- Each member sets how loud everyone else plays for them (M3), from 0 to 100%, in the call's list. The browser keeps it by den and member, as it keeps the microphone and speaker, so someone turned down stays down in the next call, and the list shows the level beside anyone below full. It's the volume of the media element that plays them, so it stops at 100%: more would mean playing their audio through Web Audio instead.
- Staff can disconnect a member of a lower rank from a call, or mute them, which the den enforces (M3; see Staff in calls).
- Speaking indicators (M3): each page measures the audio it plays for each member, and its own microphone's as sent, and rings the avatar of whoever is speaking. Only members in the call see them, since only their pages play its audio, and the den sends nothing for them.
- A voice channel's call holds 15 members, and a den's calls 30 in all, unless the owner sets other caps (M4.2; see Limits for calls). Each member's audio goes out once to every other member of their call, so the den's upload grows with the square of a call's size: a full call of 15 sends about 3 Mbps while two people talk, and up to 24 if everyone does (see the bandwidth table).
- Each voice channel has a bitrate, which staff set in its settings like the rest of the channel (M4.2): from 16 to 128 kbps in steps of 8, and 96 for a new channel. The den writes it into every offer for the channel's call, so a change reaches the call in progress with each member's next offer. A lower bitrate costs the den less upload: each member talking sends it to every other member of the call.
- Calls aren't end-to-end encrypted. Media is encrypted between each browser and the den (DTLS-SRTP), and the den decrypts it to forward it, so the den's owner could listen in. Onboarding and the docs say so, as they say the den can read channels. End-to-end encrypted calls would need SFrame (insertable streams), after v1.
- DMs have no calls until then: a DM's call would pass through the den's SFU like any other, so its owner could listen in, unlike the DM's messages.
- The den's offer, which carries the fingerprint its DTLS must match, comes over the den socket, so a call reaches the den the member signed in to, as their messages do.
- Members in a call never see each other's addresses: everything passes through the den, which sees the address each member's media comes from, as it sees the address of each of their connections already.

**Client side (browser)**

- Mic: `getUserMedia` with `echoCancellation`, `noiseSuppression` and `autoGainControl` on. The member picks the microphone and the speaker in the call's settings, the speaker through `setSinkId`.
- Opus at up to the voice channel's bitrate, 96 kbps unless staff set another (M3, M4.2), where a browser left to itself sends 32: every offer from the den asks for it (`maxaveragebitrate`), and a browser takes a new offer's parameters for its encoder. Pion builds each offer after the first from the parameters the browser answered with, which don't ask for it, so the den writes its own into every offer.
- Screen: `getDisplayMedia`, which goes through xdg-desktop-portal and PipeWire on Wayland (M4.2; see Screen share).
- The peer connection names no STUN or TURN servers, so a call contacts nothing but the den.
- The page's `Permissions-Policy` grants the microphone and screen capture to the page itself (`microphone=(self)`, and `display-capture=(self)` from M4.2). The camera stays off.
- Noise suppression (M3): browsers' own suppressor takes out steady noise such as fans and hum, but not keyboards, a TV or other voices. So RNNoise runs instead unless a member turns it off: Xiph's small noise-suppression network (BSD), compiled to WebAssembly and run in an AudioWorklet between the microphone and the call, at 48 kHz in 10 ms frames. The browser still cancels echo first, since that needs the raw microphone and what the browser plays. Only one suppressor runs at a time, so the browser's is off (`noiseSuppression: false`) while RNNoise is on, and the encoder gets the cleaned audio. Others' audio keeps playing through media elements, which the echo canceller hears. The browser keeps the choice, as it keeps the microphone and speaker, and a change applies mid-call by replacing the track the call sends, without a new offer. Where RNNoise can't start, as in a browser that won't run its audio, the call goes on with the browser's own suppressor, and the call's settings say so; the member's choice stands, for the next call or another browser.
- `scripts/rnnoise.sh` builds RNNoise with the wasi-sdk and binaryen the media module pins, and writes the module into the page's assets. Its code is RNNoise 0.2's release, which matches the v0.2 tag's file for file; its model is the one that tag names, since the release carries another, without the float copies the model keeps for debugging. RNNoise's generic vector code, which WebAssembly builds take, includes Opus's `os_support.h`, which RNNoise doesn't carry, so `internal/ui/rnnoise` supplies the one macro it needs. The module is committed, as the media module is: CI builds it again and fails if it differs.
- The module is 1.47 MB and imports nothing. The page loads and compiles it only when a member turns RNNoise on, and posts the compiled module to the worklet, which every target browser takes. Compiling WebAssembly takes `'wasm-unsafe-eval'` in the page's CSP, which allows WebAssembly but not `eval`, and gives a script in the page nothing it couldn't do already; under today's CSP every target browser refuses to compile it.
- It costs about 220 µs of one core per 10 ms frame in every target browser, about 2%, and adds 30 ms to a call: RNNoise's own 20 ms, and 10 ms of gathering 480-sample frames from the browser's 128-sample blocks. It takes steady noise down by about 50 dB and keyboard clicks by 20, and keeps other voices, since it tells speech from noise, not one speaker from another. Dens uses the regular model: RNNoise's "little" one costs 40% less and is half the size, but does less on steady noise, and 2% of a core is cheap. The spike that measured all this is `spikes/README.md` at commit c5a882b.

**Signaling**

The page signals over its event socket to its local service, which relays on the den socket. The den makes every offer and the page only answers, so the two never offer at once:

1. The page asks its local service to join a voice channel, and the service sends `voice.join` to the den.
2. The den sets up its side and sends an offer: one audio section that receives the member's microphone, and one that sends each other member's audio, labeled with their member ID.
3. The local service checks the offer, replaces any candidates it carries with the den's media addresses (see below), and hands it to the page that joined.
4. The page attaches its microphone and answers. The local service takes the browser's candidates out of the answer, and from M8 the addresses in its connection lines, and passes it to the den.
5. Media flows directly between the browser and the den, over UDP or, where UDP is blocked, TCP, encrypted with DTLS-SRTP.
6. When someone joins or leaves the call, the den sends each other member a new offer, which adds or retires that member's section. One offer is outstanding per member at a time, and one not answered within 15 seconds ends that member's call.
7. When the page's connection to the media ports breaks, as on a move to another network, it asks for an ICE restart (M3). The den's next offer carries new ICE credentials, the local service writes the den's addresses into it afresh, and the browser checks its new paths. DTLS carries on, so the call keeps its keys and its sections.

The messages are in [protocol.md](protocol.md#voice-m2).

**Riding out a dropped connection (M3)**

A call's signaling and its media take different paths: the den socket runs from the member's local service through Caddy, and the media from their browser straight to the den's media ports. Losing one needn't end the call.

- When a call's socket closes, the den holds the call for 30 seconds. The member stays in it, their audio flows both ways if its path still does, and offers wait. That covers a Caddy reload, a blip that broke only the socket, and at least five of the client's reconnects.
- The page keeps its peer connection while its den is away, and asks to resume the call once the den is back. The den moves the held call to the new socket, which must be the same device's, and makes a new offer if the call changed meanwhile. The others notice nothing but a gap in that member's audio, if there was one. They see no mark during the hold, since the den can't tell whether the audio still flows.
- A socket the den closes for good ends its call at once: a removal, a ban, a revoked device, a new password, starting over, leaving and logging out all do. Any other close holds the call.
- An offer that was out when the socket closed waits, since Pion can't take an offer back, and goes again once the call is resumed. A page that already answered it sends the same answer again.
- A call that ends while held stays ended, and the resume says why, so a member disconnected by staff in that window isn't brought back by their own page. A call that can't be resumed, because the hold ran out or the den restarted, is joined again from the start.
- When the browser's own connection to the media ports breaks, as when a laptop moves from Wi-Fi to Ethernet or leaves home, the page asks for an ICE restart (step 7 above). The local service looks the den's addresses up again for it, since split DNS can give another address on another network.
- The local service holds nothing of a call across a dropped connection: it passes the page's resume on to the den, and writes the den's addresses into its offers afresh. So a call also rides out the service restarting, as on an update, if it's back within the hold. A service that stops closes its pages' sockets but leaves their calls alone, since only a page that goes away leaves its call.
- Closing or reloading the page ends its call at once, and a call whose media stops answering connectivity checks for 30 seconds ends as failed.

**Staff in calls (M3)**

- Staff disconnect members of a lower rank from calls, and mute them, under the rank rules of removal: moderators act on members, and the owner on anyone else.
- A disconnected member can join again at once. One who keeps coming back is removed or banned like anyone else.
- A staff mute makes the den forward nothing from the member, whatever their page sends. It belongs to the member, not the call, so leaving and joining again doesn't shed it. It lasts until staff lift it, the member leaves the den, or the den restarts, since the den keeps it in memory. Everyone who can see the call sees the mark.
- The member's page says what happened, without naming who did it.

**Voice controls (M3.3)**

- A member sends their voice one of two ways, chosen in the settings' Voice section. With voice activity, the default, the page sends only while they speak. With push to talk, it sends only while they hold their key.
- Voice activity is automatic by default: it opens for whatever RNNoise, which judges every 10 ms frame, takes for speech. A member can instead set a level with a slider, against a live meter of their microphone, and the page sends what's above it. Without RNNoise, the level is the only way.
- The gate runs in the audio thread on every 10 ms frame, after the noise suppressor, so it opens on a word's first syllable, not up to 100 ms late as a meter on the main thread would. It holds for 300 ms after speech stops, so words' ends and the gaps between them aren't cut, and it fades in and out over a few milliseconds, so it doesn't click. With RNNoise off, the same processor runs without it, for the gate alone. Where the processor can't run at all, the microphone goes as it is, with the browser's own suppressor: voice activity then sends everything, push to talk holds the track back itself, and the settings say so.
- Keys: push to talk, toggle mute (which serves as toggle to talk), and push to mute, which mutes while held. They work while the page has focus, as with any web page, and in other programs through the helper (M7; see Global shortcuts). A key held when the page loses focus counts as released, unless the helper covers it. They don't fire while the member types in a text field, unless the key types nothing there: a function key, or one held with Ctrl, Alt or Meta. A key fires while its own modifiers are held, whatever else is, so a player holding Shift to run still pushes to talk; of two actions on one key, the one with more modifiers takes the press. From M7 a mouse's side button can be a key too.
- What the gate holds back goes as silence, and every offer asks for Opus's discontinuous transmission (`usedtx=1`), so a member who isn't speaking sends a packet every 400 ms instead of 50 a second. That keeps big calls cheap for the den's upload: silent members cost almost nothing.
- Speaking rings follow what's sent, since they measure what each page plays: a member the gate holds back rings nowhere, and the member's own ring lights while their gate is open.
- At the foot of a den's channel list, the member's panel shows their picture and name, which open their profile to edit, and the cog for the settings. The panel is as tall as the message bar beside it, which it lines up with, and keeps the channel list's color.
- While in a call, a block sits above the panel and holds the call in a row: a speaker, green once connected, that names the channel on hover, and buttons to share their screen (M4.2), mute, deafen, open the Voice settings and leave. What the member should know of the call, such as a staff mute, shows in the block above the row, and while they share, the share's controls show there in a row of their own (M4.2). The block and the message bar are a shade lighter than the page, or as light where the page is already white, so a call shows at a glance against the channel list and the panel. On the other pages the call shows boxed, with the channel spelled out.
- Settings open as a dialog over whatever the page shows, so a den and a call stay in view. It closes with its ✕, Escape or a click outside it. Its General section starts with the theme, and its Voice section holds the microphone and speaker, noise suppression, how the member sends, the keys, and how they share their screen (M4.2), which the call's cog opens.
- Joining waits on the microphone, which the browser may first ask the member for, and the call says so after a moment. The voice processor's audio context is made in the click that joins, since a browser may hold back one made after its prompt until another click. The browser keeps all of these, as it keeps the microphone and speaker, except the keys, which the service keeps from M7 so the helper can watch them.

**Screen share (M4.2)**

A member in a call shares a screen, a window or a browser tab with the others in it, and each of them chooses whether to watch. A share passes through the den's SFU like the call's audio, and costs the den's upload once for each member watching it, so watching is opt-in, and the owner sets how much shares may cost (see Limits for calls).

*Sharing*

- The call's row has a button with a screen on it, between the speaker and mute, as the box on other pages has. It opens the browser's own picker, which offers a screen, a window or a tab and, where the browser can share sound, a choice to include it. The page asks the browser to leave the Dens tab out of the picker (`selfBrowserSurface: 'exclude'`), since sharing it would show the page inside itself.
- While the member shares, the button shows pressed, and stops the share, and a row of the share's own sits above the call's: it says they're live and how many watch, with their names on hover, and holds a button that shows their own screen in the player or hides it, and one to stop. The browser's own stop button, and closing what was shared, stop the share too.
- The member's own screen shows in the player as a preview, from what their browser captures, not sent back by the den: when they start sharing if the player holds nothing else, and otherwise once they ask, with the share's row or their own LIVE mark. It plays without sound, since they hear what they share already, and its ✕ hides it while they go on sharing. A whole screen shared with its preview in sight shows the preview inside itself, as in any app that has one.
- The share goes at the quality the member chose in the settings' Voice section, within the den's limits: its size (720p to 2160p) and frame rate (15, 30 or 60 a second), each at most the den's, and whether it suits text, which keeps detail and drops frames first, or motion, which keeps frames and drops detail first (the track's `contentHint`). The page caps the capture to the den's size and frame rate, and the sender to the den's bitrate. Before the member picks anything, the button says so when the den has as many shares as it allows, or allows none.
- A share nobody watches sends nothing: the page pauses its encoder (`active: false`) until someone watches, which saves the member's upload and processor. Chromium starts again with a keyframe and Firefox doesn't, so a new viewer's first picture comes from the den's keyframe request as their section opens (see The den's part).
- Sound depends on the browser. Chromium-based browsers share a tab's sound on every platform, and the whole system's on Windows; on Linux, the whole system's only behind a flag (`chrome://flags/#pulseaudio-loopback-for-screen-share`). Firefox shares no sound at all. The page asks for sound unless the member turned it off in the settings, and asks the browser to leave the page's own sound out of a system's (`restrictOwnAudio`, from Chrome 141), so the call's voices don't go back out to the call. A share without sound says so in its row.
- A share ends when its member stops it, leaves the call, moves to another, or their call ends, and when staff mute them. A call the page joins again after it dropped, as when the den restarts, takes back the share and the shares the member watched, which the den checks again.

*Watching*

- Everyone who can see the voice channel sees who in its call is sharing, with a LIVE mark beside their name, and who watches them. In the same call, the mark is a button that watches; anywhere else it joins the call and watches, as clicking the channel joins it.
- What a member watches plays in a player that floats over the page, above it and below dialogs such as the settings, so it stays as they open other channels, dens or home, and they keep reading and writing around it. They drag it by its bar and resize it from its corners; the browser keeps where it was and its size, and it stays inside the window as the window changes size.
- The player holds every share the member watches, and their own while it shows, side by side, two by two for three or four, and three across two rows for five. Clicking a share makes it fill the player while the others go on out of sight, which the bar counts, and clicking it again puts it back beside them. The bar's ✕ stops watching all of them, hides the member's own, and closes the player.
- Each share in the player has controls of the page's own, the same in every browser, which show on hover or focus: who is sharing, the volume of its sound (a mute button with a slider beside it, like the video player's), full screen, and a button to stop watching it. Deafening mutes shares' sound too, and the call's speaker plays it.
- A member watches shares in their own call only, and at most four at once. Leaving the call stops watching.

*The den's part*

- A share is VP9, the one video codec the den offers. Every target browser has sent and received it for years, and it keeps fine detail at bitrates a home connection can send, where VP8 blurs it. AV1 keeps more for the same bits: in the test container, held to 200 kbps, Chromium 154's 1080p test screen kept 20 frames a second with AV1, 14 with VP9 and 6 with VP8. But it costs more to encode, few machines have an encoder for it in hardware, and it's newer in Firefox (on by default from 136). H.264 is out: Firefox has it only through Cisco's OpenH264 plugin, which it downloads on its own, and the test container's Firefox ESR 153, without it, offered none.
- When a member first shares in a call, the den adds two sections to their offers: a video section it receives the screen on and, if the share has sound, an audio section for it. They stay for the rest of the call, for any later share. A member who watches gets the share's video, and its sound if it has any, in sections of their own, which come and go as they start and stop watching, and which later shares reuse as members' audio sections are reused.
- The den forwards a share as it forwards voice: only VP9, only from the member's own screen section and only while they share, each packet's payload under headers of the den's own, without the sender's header extensions.
- A viewer's browser can start decoding only at a keyframe, a whole picture, and the sharer's browser sends one only when asked. So the den asks the sharer for one, with a picture loss indication (PLI), as soon as a viewer's answer opens their section, and again when a paused share's packets start again, since a request that reaches a paused encoder may be lost; and it passes on as a PLI each PLI or full intra request (FIR) that a viewer's browser sends, as after packets it lost. A keyframe goes to every viewer and is many times the size of other frames, so the den sends a share at most one PLI every half second; a request inside that time gets one at its end.
- The den asks the sharer again for packets it lost (NACK), and sends viewers again what they lost from a buffer of what it sent, so a viewer's lost packets cost the sharer a keyframe only when the den no longer has them.
- The den can't decode a share, so it can't see its size or frame rate: those are the page's to keep, in the member's browser. What the den holds a share to is its bitrate. Its offer asks for no more than the den's share bitrate, in both forms browsers read: Firefox honors only `b=TIAS`, and Chromium `b=AS` and `b=TIAS` alike, as the test container showed. The den also drops whatever goes past the bitrate and a quarter more, with room for a keyframe's burst, so a page that ignores the offer gains nothing. A share's sound is held to a voice's limits, 256 kbps and 500 packets a second, and its Opus asks for stereo at 128 kbps, where voice is mono.
- Every viewer gets the one stream the sharer sends: a viewer on a slow connection loses frames, rather than getting a smaller stream. Simulcast, where the sharer sends two sizes and the den picks one for each viewer, comes after v1.

*Staff, and who sees what*

- A staff mute ends a member's share and keeps them from sharing until it's lifted, as the den forwards nothing from them. A staff disconnect ends the share with the call.
- Shares aren't end-to-end encrypted, like the rest of a call: the den decrypts every packet to forward it, so the den's owner could watch any share. The share button says so on hover, as voice channels say it of calls.
- Everyone who can see the voice channel sees who is sharing and who watches, as they see who is in the call.

**Limits for calls (M4.2)**

The owner sets these in the den's settings, in a section of their own beside the upload limits:

| Limit | New den | Range |
| --- | --- | --- |
| Members in one call | 15 | 2 to 30 |
| Members in calls across the den | 30 | 2 to 100 |
| Shares at once, across the den | 1 | 0, for none, to 10 |
| Viewers of one share | 8 | 1 to 29 |
| A share's bitrate | 2 Mbps | 0.25 to 50 Mbps |
| A share's size | 1080p | 720p, 1080p, 1440p or 2160p |
| A share's frame rate | 30 a second | 5 to 60 |

- A share's size caps its pixels, whatever its shape: 1080p allows as many as 1920 × 1080, so an ultrawide window comes out wider and shorter, and a portrait one taller and narrower.
- Beside the limits, the settings show what they cost the den's upload at most: shares times viewers times bitrate for video, and for voice, a full call at the den's busiest channel bitrate while two people talk. At a new den's limits, video takes at most 16 Mbps, which most home connections can send; an owner with a faster one raises them, and the guide shows how. The ceiling, 50 Mbps, leaves room for a 4K share at 60 frames a second.
- A call holds at most 30 members, since each offer has a section for every other member and must fit in 32 KiB: one for a member of a call of 30 who shares and watches four shares is about 23 KiB.
- The den sends its limits to members with its name and upload limits, so each page knows them before it asks, and checks them itself all the same. A lower limit applies to what starts afterwards: nobody is taken out of a call or a share.

**Global shortcuts (M7)**

A page sees keys only while it has focus, so push to talk can't reach it from a full-screen game. Discord's web app has the same limit, and its desktop app doesn't. Nor can the local service see keys: on Windows it runs in session 0, where Windows blocks all keyboard input to services, and on Linux it runs as its own account, outside the desktop session.

*The helper*

- A small helper, `dens shortcuts`, the same binary, runs as the desktop user in their session from login. One helper serves every instance on the machine: it finds them in the instance roots, which users may list, and holds a stream on each one's control endpoint, which answers only that instance's desktop user.
- The service sends the helper the member's keys, and whether one of its pages is in a call. The helper sends back each press and release as the action it's bound to: push to talk, toggle mute or push to mute. The service passes them to its pages, and the page in the call acts on them.
- The helper watches keys only while a page is in a call. Outside calls it watches nothing and costs nothing.
- The service keeps the keys, since the helper needs them, so every browser paired with the instance shares them. How the member sends, and the level, stay with the browser.
- A key is a keyboard key with any modifiers or, on Windows and in the page, one of the mouse's side buttons, which many players push to talk with. The page names a key by its place on the keyboard (`KeyboardEvent.code`), and the helper finds what the current layout makes of it.
- When the service comes back from an update at another version, the helper starts the new binary and exits.
- The helper's stream is the control endpoint's `shortcuts` request: JSON lines go both ways until either side closes it, each pinging every 15 seconds and giving up on the other after 45 without a line. The service sends `hello` with its version, then `state`, its keys and whether it holds a call, as either changes; the helper answers each with `source`, what it covers, and sends `press` for each press and release. An instance holds one stream from its desktop user's helper: a newer one replaces it, and the old one, told `replaced`, exits.
- The keys are the service's setting `voiceKeys`, which the settings' API takes whole. The page hears of them, and of the helper, over its socket with the service: `keys`, with the keys, whether a helper is connected, what it covers and its note, as any of them changes, and `key` for each press during a call, which the service drops outside one.

*Windows*

- The helper reads the bound keys' state every 10 ms (`GetAsyncKeyState`), with no keyboard hook, so it sees nothing else typed.
- While a program run as administrator has focus, Windows hides every key from the helper, as it does from Discord (User Interface Privilege Isolation). The settings say so.
- The installer registers a scheduled task that starts the helper at the desktop user's logon, with their interactive token, at normal priority, since a task's default of below normal could fall behind a game, and with no time limit. It runs the task at once, so nobody signs out after installing. Uninstalling the last instance ends the task and deletes it.
- The Windows spike (`spikes/README.md` at commit a03d820), on Windows 11 24H2 with a Ryzen 7 9800X3D: the reads saw a key pressed with a game full screen in front and in Steam, and nothing with an elevated terminal in front. At normal and below-normal priority, with the game running, they came every 10 ms at the median and the 99th percentile, one gap in some 15,000 reaching 20 ms, for 0.2 to 1% of one core. No game with anti-cheat was at hand.
- `dens.exe` carries a manifest whose `consoleAllocationPolicy` is `detached`, which Windows 11 24H2 and later honor: started outside a console, as at logon, it gets none, not even one `CREATE_NEW_CONSOLE` asks for, so the helper opens no window, while in a shell it runs as any console program does. Go doesn't embed manifests, so a resource file generated for each architecture sits beside `cmd/main.go`, and CI checks that it's current.

*Linux*

- Wayland: the desktop's global shortcuts portal, in GNOME 48 and later, KDE Plasma and Hyprland, reports a shortcut's press and release. There the desktop owns the keys. The member sets them up with it from the settings: the helper binds the three actions in a session of the portal, with the member's keys as the preferred triggers, and the desktop asks them to confirm or change each. The service keeps that they did, and as the helper starts it binds again, which the desktop answers without a dialog, since it remembers the keys for the app across sessions and sign-ins. A dialog turned down isn't asked again until the member asks, and stopping ends the session, which a new one replaces, unbound: a session binds once.
- The settings then show the desktop's names for the keys. Changing them opens the desktop's own settings for them where its portal can (`ConfigureShortcuts`, version 2, as KDE's), and otherwise says where they are (GNOME's portal is version 1: its Settings, under Apps, then Dens). The shortcuts' IDs never change, so the desktop asks only once.
- GNOME repeats a held key's press as the keyboard repeats, and reports one release; the helper passes one press and one release.
- The Linux spike (`spikes/README.md` at commit 0f99022) drove the portal as the helper does, on Fedora 44's GNOME 50 and KDE Plasma under Wayland. Both took the app ID once the `.desktop` file was installed, showed their dialog on the first bind with the preferred triggers, each changeable, and later bound without one in a few milliseconds, across sign-ins and restarts. Presses reached the session with a game full screen in front.
- A key bound on the desktop reaches no program but the helper, a game included, and the settings say so. A keyboard whose top row sends media keys unless Fn is held never sends a function key it doesn't hold Fn for.
- The portal refuses an unsandboxed app whose app ID no installed `.desktop` file backs (xdg-desktop-portal 1.21 and later), so the helper registers as `chat.dens.Dens` before anything else (`org.freedesktop.host.portal.Registry`), and the installer adds `chat.dens.Dens.desktop` to `/usr/local/share/applications`, with Dens's mark as its icon. The file also opens the page (`dens open`) from the desktop's menu, and GNOME keeps the app's shortcuts on its page in Settings. Older portals take the app ID from the helper's systemd unit, which the autostart entry's name sets.
- Desktops without the portal (GNOME before 48, Sway, COSMIC) keep the page's keys.
- X11: the helper reads the bound keys' state every 10 ms (`XQueryKeymap`), over a small X11 client of its own that connects to the local display with the X authority's cookie. A key's place maps to the X server's keycode through Linux's input codes. A mouse's buttons aren't keys to the X server, so side buttons stay the page's there.
- The installer adds an XDG autostart entry (`/etc/xdg/autostart/chat.dens.Dens.desktop`), which GNOME and KDE run as the systemd user unit `app-chat.dens.Dens@autostart.service`, and starts or restarts that unit in the desktop user's session through their systemd user manager, when they're signed in to a desktop. Uninstalling the last instance removes the entries and the icon; a helper still running stops once no instance answers it.
- The helper speaks D-Bus through `godbus/dbus/v5`, which needs nothing beyond `golang.org/x/sys`.

*The page*

- The settings' keys say whether they work in other programs, with an unobtrusive mark. Where the desktop owns the keys, the keys card offers to set them up with it, says to confirm them in its dialog while that's up, and once they're set up shows the desktop's names for them, with a way to change them and one to stop using them.
- While the helper covers a key, during a call the page ignores its own presses of it and acts on the helper's: on Windows and X11 the helper sees a key pressed in the page too, and toggle mute would fire twice. Outside a call the helper reads nothing, and the page's own keys work as before, except those the desktop owns, whose keys may be others. A key that types something still doesn't fire while the member types in a text field; one the desktop owns never reaches the field.
- A side button the member bound is the page's while it has focus: its press and release don't take the browser back or forward.
- The helper tells the service what its keys can do (whether the desktop confirms and names them) and what the system keeps from them, as a note the settings show, never which system it runs on, so no platform difference reaches the local API.

**Media addresses**

The den's media ports carry every call: one UDP port, and one TCP port as the fallback for networks that block UDP.

- The den runs ICE-lite. It never sends connectivity checks, only answers the browser's, so it needs no candidates of its own. Each port takes every call, which Pion's UDP and TCP muxes tell apart by the ICE username in the first packet.
- The member's local service writes the den's candidates into each offer itself: each address the den's name resolves to, with the den's two media ports. The browser sends media where the install already reaches the den, so the den needn't know its public address, and asks no outside STUN server for it. A member whose lookup gives the den's local address, as with split DNS at home, calls over the LAN.
- For a den on the same machine, which is the install's own den or one whose name resolves to loopback, the local service writes the machine's addresses on its other interfaces instead, since browsers don't use loopback for WebRTC. The owner's own calls then don't go through their router.
- So a den points a member's browser only at the addresses its name resolves to, at the media ports it gives. A den in control of its own DNS can make those any address, such as a device on the member's own network, but all the browser sends there is ICE's connectivity checks and DTLS, which don't act on anything.
- The den never learns members' local addresses. The local service takes the browser's candidates out of every answer, and puts the unspecified address (`0.0.0.0`, or `::`) in its connection, RTCP and origin lines (M8). A browser's first answer has it there, but each one after it has gathered names its default candidate in the `c=` line, often the computer's address on its local network, as Chromium and Firefox both do once a page may use the microphone. The den answers checks from wherever they arrive, so it needs none of them.
- Calls need the den's name to point at the den's machine, or the router in front of it, as the networking guide says. A tunnel or CDN that carries a den's HTTPS doesn't carry its calls.

**Den SFU (Pion)**

- `SettingEngine.SetICEUDPMux` puts all media on one UDP port; `SetICETCPMux` adds a TCP fallback port. Both listen on every interface, IPv4 and IPv6. The service binds them at start with the den listener, and doesn't start without them, as with its other ports.
- `SetLite`, with multicast DNS off: the den neither looks up members' `.local` names nor sends queries on its own network.
- A TCP connection that doesn't name a call within 5 seconds is closed, and the TCP port holds at most 128 at once, so a flood of idle connections costs bounded memory. Packets for no call are dropped.
- Voice is Opus only. The den reads a member's audio only from the section it asked them to send on, and forwards each packet's payload under headers of its own, without the sender's header extensions, at most 256 kbps and 500 packets a second from each member. Screen shares are VP9 only, forwarded the same way within the den's share bitrate (M4.2).
- Interceptors: NACK, RTCP reports and TWCC for bandwidth estimation.
- A share's keyframe requests: one as each viewer starts watching, and every PLI and FIR from a viewer passed on to the sharer as a PLI, at most one every half second per share (M4.2; see Screen share).
- A held call keeps its peer connection without a socket, and its offers wait until it's resumed. An ICE restart is Pion's own: the den's offer carries new credentials, which the UDP and TCP muxes match from then on (M3).
- A staff mute drops the member's packets before they're forwarded (M3).
- Pion's own log messages name addresses, so they reach the log only in development instances. The den logs each call's start, end and the reason it ended, by member and channel ID.
- No TURN in v1: the den is directly reachable, so clients behind NAT connect outward. Add `pion/turn` on TCP 443 later if restrictive networks need it.
- Pion is the only WebRTC stack in Go, and DTLS-SRTP and ICE aren't things to write. `pion/webrtc` v4 brings 15 more of the project's modules, plus `google/uuid` and `wlynxg/anet`, all MIT-licensed, and adds about 5 MB to the binary, including parts Dens doesn't use, such as data channels and a TURN client.
- Start from Pion's SFU-over-WebSocket example rather than a blank file.

**Bandwidth at target scale** (approximate)

| Case | Per stream | Den upload |
| --- | --- | --- |
| 10 people in voice (Opus at 96 kbps) | about 115 kbps talking, 1 silent (M3.3) | about 2 Mbps while two talk, up to 10 Mbps |
| 1 screen share at 2.5 Mbps, 10 viewers | about 2.6 Mbps | about 26 Mbps |
| 2 screen shares at 2.5 Mbps, 10 viewers each | about 2.6 Mbps | about 52 Mbps |
| 2 screen shares at 2.5 Mbps, 20 viewers each | about 2.6 Mbps | about 104 Mbps |

Screen share cost scales with viewers, not sharers, which is why watching is each member's choice and the owner caps shares, viewers and bitrate (M4.2; see Limits for calls). Simulcast (sender uploads high and low layers, SFU picks per viewer) is the later fix.

**Known risks**

- A share's sound depends on the browser (M4.2): Chromium-based browsers share a tab's sound everywhere and the whole system's on Windows, on Linux only behind a flag, and Firefox shares none. Accepted for v1; a share without sound says so.
- `restrictOwnAudio` keeps the call's voices out of a shared system's sound only from Chrome 141; the manual test checks it on Windows, where it matters.
- Firefox and Chromium differ in small WebRTC details; each milestone is tested on all four target browsers.
- Calls depend on the den's name resolving to the den. If dens behind tunnels need calls, an owner setting for a separate media address could come later.

## Networking and deployment

A den needs a domain, Caddy, and four forwarded ports. Caddy handles only HTTP and WebSocket traffic; WebRTC media must reach the den directly.

| Port | Protocol | Purpose | Required |
| --- | --- | --- | --- |
| 80 | TCP | ACME certificate challenges (Caddy) | Yes |
| 443 | TCP | HTTPS and WSS to the den listener (Caddy) | Yes |
| Media port, default 7881 | UDP | WebRTC media via UDP mux | Yes, for voice |
| Fallback port, default 7882 | TCP | ICE-TCP media fallback | Recommended |

The docs list which ports to forward but not how, since routers vary too much. They mention a small VPS as the easy alternative when forwarding isn't possible.

Members' calls reach the den at the addresses its name resolves to (see Media addresses), so the name must point at the den's machine or the router forwarding to it. The owner's own calls go to the machine's own addresses and work before any port is forwarded, so the guide has owners test a call from outside their network.

On Windows, the installer adds inbound Windows Firewall rules for the media ports, scoped to `dens.exe`, when the den role is enabled, and removes them on uninstall. The Caddy guide covers ports 80 and 443 on both platforms.

**Caddy**

- The guide ships a minimal Caddyfile: the den domain reverse-proxies to the den listener.
- Access logging stays off (Caddy's default), in line with collecting less.
- A second den on the same machine is another instance with its own ports and a second site block for its domain or subdomain.
- Caddy runs as a service on both platforms: its packaged systemd unit on Linux, and on Windows as an SCM service, which Caddy supports natively (`sc.exe create caddy binPath= "…\caddy.exe run --config …"`).
- Reloading or restarting Caddy closes den WebSockets (close 1001). Clients resume without losing events.
- Distributions' packages of Caddy lag (Debian 13 ships 2.6), so the den e2e runs the Caddy `scripts/vendor.sh` pins.

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

Cut: Alpine, Void, Artix, Devuan, Gentoo, NixOS, openSUSE and MicroOS, Rocky, AlmaLinux and RHEL, SteamOS, and WSL2. After v1, a Flatpak lets their users join dens (see Other distributions). The installer refuses NixOS and SteamOS before it changes anything, saying why (M8). Both keep paths it writes read-only (NixOS's `/etc/systemd/system`, SteamOS's root), so it would otherwise fail halfway.

**TPM**

`systemd-creds` binds to the TPM plus the host key when a TPM exists, and to the host key alone otherwise, so no separate fallback is needed. Host-key-only mode still protects copies of the data directory, but not a full disk image or root.

**Install layout rules (all distros)**

These keep immutable and atomic-update distros cheap to support, and are good practice everywhere:

- Install paths come from one layout module, never hardcoded, so a distro can override where the binary lives.
- Keep the `/etc` footprint minimal: the unit file, a `sysusers.d` file, nothing else.
- Define the service user in `/etc/sysusers.d` (creating the directory where the distro only ships `/usr/lib/sysusers.d`, as Debian 13 does) so it's recreated at boot. Don't fix its UID: sysusers silently picks another one when the requested UID is taken. `StateDirectory=` re-owns the state directory to the service account on start, so ownership follows the account.
- Keep the encrypted credential and all state under `/var/lib/dens/<name>/`; reference the credential by absolute path in `LoadCredentialEncrypted=`.
- Use `StateDirectory=` and `RuntimeDirectory=` instead of creating and chowning directories by hand or shipping tmpfiles.d files. The instance root and its `control/` directory (lifecycle state, locks, instance config and the encrypted credential) stay root-owned and readable by the service's group; `StateDirectory=dens/<name>/data` has systemd create the data directory and own it for the service account.

**In a container**

Dens installs in an unprivileged LXC system container, as Incus and LXD make them, with `security.nesting=true` and systemd 256 or newer, such as Debian 13's, Fedora's or Arch's: an older systemd can't create its credential secret there. LXC adds a drop-in to every service in such a container (`zzz-lxc-service.conf`), which turns off the service's sandbox and the encrypted credential that carries its data key. The installer refuses to go on while it's in effect, and prints the commands that mask it (M8); otherwise the service would fail to start past the point of no return. The harnesses mask it the same way. In a container, the data key is bound to the container's credential secret, which root on the host can read, and voice keys work only while the page has focus: the shortcut helper in the host's desktop session can't reach the service, whose stream checks each end's account. A virtual machine needs none of this.

**Other distributions: a Flatpak (after v1)**

After v1, a Flatpak brings the client to the distributions the installer doesn't support, NixOS and SteamOS among them, and to any other that runs Flatpak. It joins dens and hosts none: a den has to keep running when nobody is signed in, which an app in a desktop session can't, and needs Caddy beside it, so hosting stays with the installed service on a supported distribution.

- It's published on Flathub, which GNOME Software and KDE's Discover, SteamOS's included, show by default. Flathub builds and signs it, so the Flatpak rests on Flathub's builds rather than the release identity. Flathub builds from source, offline. Go's modules vendor; the page's build tools, Tailwind's standalone binary and esbuild, are prebuilt, so how they fit is open: perhaps the source archive carries the page's built assets.
- Dens runs in the member's session, as them, started at sign-in through the Background portal, with its data in the app's directory in their home. Development instances already run this way: as their user, with no service manager.
- Who can read what changes, and the public docs say so. The installed service runs as its own account, so a program running as the member can't read Dens's data or keys without root; the Flatpak runs as the member. Its data key is wrapped with a secret from the Secret portal, kept in their keyring, or else with the local password at each start. Flatpak's sandbox keeps other Flatpak apps out, but not programs the member runs outside it.
- Flatpak installs and updates it, so no installer transaction runs, and `dens update` points at Flatpak instead. The app migrates its data as it starts, and refuses data a newer version wrote, since Flatpak can roll an app back.
- Voice keys need no separate helper, since Dens already runs in the session: it binds them through the global shortcuts portal, which knows a sandboxed app by its Flatpak ID, or under X11 reads them over the X11 socket the Flatpak is allowed.
- An installed service and the Flatpak on one machine both want the client port; which gives way is settled when it's built.

### Windows

Windows 11, current supported releases (24H2 and later), Home and Pro. Windows 10 and Windows Server are cut.

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

- The platform spike showed sockets, interface enumeration, named pipes and child processes working under the restricted service SID, and the den e2e's call runs Pion there (M2).

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
| Keys for the helper (M7) | The global shortcuts portal under Wayland, `XQueryKeymap` under X11 | `GetAsyncKeyState` |

Everything else that differs is install-time: the two installers, the unit file and SCM registration, account creation, ACLs and firewall rules, and how the helper starts at login: a scheduled task, or an autostart entry and a `.desktop` file (M7).

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
- [x] Messages rendered by an escaping markdown subset; names and channel descriptions treated as untrusted text.
- [x] Filenames treated as untrusted text, and files shown inline only as the image type the local service found them to be, sandboxed and never cached by the browser (M1.4).
- [x] Exact `Host` check (`127.0.0.1:<port>`, `[::1]:<port>` or `localhost:<port>`) against DNS rebinding.
- [x] `Origin` check on every write and on the WebSocket upgrade.
- [x] Session cookie `HttpOnly`, `SameSite=Strict`; pairing tokens single use and short-lived.
- [x] Listener bound on both `127.0.0.1` and `::1`.
- [x] Everything from a den is hostile input: the client service checks every den response against the protocol's types and limits before storing or forwarding it.
- [x] A den's error codes reach the page only when they're codes the page knows, and a client follows a den to a new address at most once a minute, one check at a time, never to plain `http` or loopback for a den elsewhere, and follows no redirect (M8).
- [x] The page parses a message in time proportional to its length, and once per text (M8).
- [x] The CSP allows WebAssembly for RNNoise (`'wasm-unsafe-eval'`), but not `eval` (M3).
- [x] Rewritten links stay on their own site: hosts match exactly, as a URL parser reads them, so a lookalike is never rewritten into a real link (M3).
- [x] The page's links to the public guides open in a new tab, with no opener and no referrer (v1).
- [x] The page frames nothing but YouTube's player from `https://www.youtube-nocookie.com` (`frame-src`), only for a member who turned players on and clicked, sandboxed without top navigation, at an address the page built from an ID it checked; it takes no messages from the frame, and its popups can't reach the page (M4.1).
- [x] The local service fetches nothing from YouTube but the oEmbed and picture addresses it builds from a checked video ID, follows no redirects, caps what it reads, cleans titles as names, and serves a picture only as a JPEG it read and stripped, with a member's file's headers (M4.1).

**Den listener**

- [x] Serves only den routes; no client or admin routes compiled into its router.
- [x] Challenge answers sign the den's address, and clients send nothing to an address the den didn't sign, so no relay can pass a sign-in through (M1.5).
- [x] A password alone doesn't add a device: another of the member's devices approves it, or a recovery code stands in (M1.7).
- [x] A DM starts only after both members type each other's check digits, and the DM seal never reaches the den (M1.7).
- [x] Request size limits, WebSocket message size limits, and rate limits per IP and per member, with one budget for a member's messages across every channel and DM (M8). A request's body arrives within a minute (M8).
- [x] Invite codes: 128-bit random, single use unless staff allow more (at most 100), expiring, stored hashed.
- [x] Bearer tokens only, never cookies, so no web page can make a browser act on a den.
- [x] The den's ring of recent events is held to 64 MiB as well as its count and age (M8).
- [x] Staff powers stop at DMs, and a ban is lifted only by staff of the rank of whoever placed it, or higher (M8).
- [x] Only the desktop user of the den's machine, with the local password, makes an owner invite, which is used once within an hour (M8).

**Control endpoint**

- [x] Every connection authorized by OS-reported peer identity against the recorded desktop user.
- [x] The CLI verifies the server's identity before trusting a response.
- [x] Windows pipe: `FILE_FLAG_FIRST_PIPE_INSTANCE`, `PIPE_REJECT_REMOTE_CLIENTS`, one listening instance at all times, DACL granting SYSTEM and the service SID full access and the recorded user only `0x12019b` (never `GENERIC_WRITE`).
- [x] The endpoint keeps answering after a client that opens it and closes at once, and on Windows a service that fails after starting is restarted, as systemd restarts it on Linux (M8).

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

**Media**

- [x] ffmpeg runs only as the translated module, in a worker process with a memory cap, a deadline and a lower Go stack limit, reaching nothing but the functions Dens gives it.
- [x] The module's memory is a slice with no spare capacity, so bulk memory operations trap past its end.
- [x] Dens's C in the module is fuzzed with damaged files under AddressSanitizer in CI, beside the module.
- [x] Stripped files of every kind Dens takes are checked by a reader independent of FFmpeg, exiftool, in CI.

**Voice (M2)**

- [x] Only members who can see a voice channel join its call, and a call ends as soon as that stops.
- [x] The local service writes the den's media addresses into every offer, so a den points a member's browser only where its name resolves, and takes the browser's candidates out of every answer, and from M8 the addresses in its connection, RTCP and origin lines, so the den never learns members' local addresses.
- [x] The page's peer connection names no STUN or TURN server, and its `Permissions-Policy` grants the microphone to the page alone.
- [x] The local service passes the page only offers within 32 KiB, of audio and, from M4.2, the video below, and the den takes only answers that fit its offer, within 32 KiB.
- [x] The den forwards only Opus, under its own headers, at most 256 kbps and 500 packets a second from each member.
- [x] The media ports drop packets for no call, close a TCP connection that doesn't name one within 5 seconds, and hold at most 128 TCP connections.
- [x] Pion's log messages, which name addresses, stay out of release logs.

**Voice (M3)**

- [x] Only staff of a higher rank disconnect or mute a member, and the den enforces a mute by forwarding nothing from them.
- [x] A held call resumes only on a socket of the device that held it, within 30 seconds, and a socket the den closes for good ends its call at once.
- [x] A call that ended while held stays ended: its resume gets the reason, never a new call.

**Screen share (M4.2)**

- [x] The page's `Permissions-Policy` grants screen capture to the page alone (`display-capture=(self)`), and the page asks the browser's picker to leave out the Dens tab.
- [x] The local service passes the page audio only as Opus and video only as VP9 or AV1, so a den can't have the browser decode any other codec. The den offers VP9; AV1 passes too, so a later den can offer it without breaking older clients.
- [x] Only members of a call watch its shares, and the den forwards a share only while its member shares, only VP9 from their screen's section, under its own headers, within the den's share bitrate.
- [x] A staff mute ends a member's share, and the owner's limits cap the shares at once, a share's viewers and its bitrate, which the den enforces.

**Files and retention (M5)**

- [x] Smaller copies are made on the sender's machine, a photo's by the module and `internal/media` and a video's by the module, or from M5.5 by the browser and muxed by the module, and the den checks them like any upload: images for metadata, and video stripped again.
- [x] Only a file's uploader deletes or swaps it, and a replacement counts against the space its file frees only to take that file's place.
- [x] Retention deletes for real, in channels and DMs: rows with `secure_delete` on, files off the disk, and clients drop what passed the period.

**The client's cache (M6)**

- [x] Everything cached is sealed with the data key, each message and file chunk bound to its den, its ID and its place; a chunk that doesn't open drops its file, and a cache under another key or of another format is emptied, never read.
- [x] Deleted messages and files leave every client's cache: at once while it's connected, and from the changes since its version when it comes back. The den's change log holds IDs and channels only, for 30 days.
- [x] The den sends a member only changes in channels they can see now, and a client drops a channel's cache when it can no longer see it, and a den's when the member leaves, is removed or banned, or the device is signed out.
- [x] A restored den starts a new sync epoch, and a client never keeps a cache across epochs or from below the floor.
- [x] Compressed frames are held to the same limits as plain ones, as they inflate, and the cache keeps no file over a quarter of its size.

**Global shortcuts (M7)**

- [x] The helper watches only the bound keys, and only while a page is in a call, with no keyboard hook, and sends nothing but the bound actions' presses and releases; neither it nor the service logs them.
- [x] The helper's stream is a control request like any other: it answers only the instance's desktop user, by the identity the system reports.
- [x] The page acts on the helper's keys only in a call it holds, and never while the member types a key that types something.
- [x] Each desktop user's helper task runs as them, with their limited token; nothing of the helper runs elevated.

**Logs and data**

- [x] No message content, tokens, keys or IPs in logs; log IDs and event types only. From M8 that holds for the addresses Go's network errors name too, of dens and of the release host. Pion's own messages, which name addresses, reach the log only in development instances.
- [x] SQLite `secure_delete=ON`.
- [x] Signed releases: the installers and `dens update` verify cosign signatures against the release workflow's identity.
- [x] No release older than the installed one installs, and only command-line flags, never the environment, change where releases come from or skip their verification, which a binary the release pipeline built refuses (M8).
- [x] Nothing a crash leaves in the data directory's `tmp` outlives the next start, and a backup holds every file its snapshot names (M8).
- [x] The binary carries the license notices of everything it includes, and CI fails when they're stale (M3).

## Public site

dens.chat is a landing page and a few pages of docs for people using Dens, built with Hugo and Hextra in `docs/` (see `docs/README.md`), and published with v1.

**The landing page** says what Dens is in one plain paragraph, then argues for it in three sections, hosting your own (01), encrypted and pseudonymous (02) and free (03), with four cards on how it's built. It links into the docs at the install guide (`/docs/install/`, from an Install button beside the hero's caption), the hosting guide (`/docs/host/`) and Privacy (`/docs/privacy/`).

**The docs** cover what the app can't show for itself: what happens before Dens runs, outside it (a domain, a router, Caddy), and across devices and time (passwords, recovery, backups), and, once, who can read what.

- They say what things are for rather than which buttons to press, so the page can change without them going stale, and they hold no screenshots, for the same reason.
- They don't repeat the page's settings, which explain themselves a line each (M8.3), or the commands, which `--help` lists.
- Each fact lives on one page, and the others link to it.

| Page | Covers |
| --- | --- |
| Docs home (`/docs/`) | Two ways in: you got an invite, or you want to host a den |
| Install (`/docs/install/`) | What Dens runs on; downloading the installer and running it, with checking its signature first as an optional step; `dens open` and the local password; updating and uninstalling |
| Join a den (`/docs/join/`) | Pasting an invite; a username is for good; the den password and the local password; recovery codes; the DM recovery key; approving a new device; comparing a DM's check code |
| Host a den (`/docs/host/`) | What a den needs: a computer that stays on, upload speed and a domain; the installer's den option; creating the den; Caddy and the four ports; a call from outside the network as the test; a small VPS where ports can't be forwarded; invites, roles, call limits and auto-delete; what a den costs in upload |
| Backups and moving (`/docs/backups/`) | `dens backup` and `dens restore`; moving to another computer, between Linux and Windows too; handing the den on with `dens den owner-invite`; what's gone when both the password and the computer are |
| Privacy (`/docs/privacy/`) | Who can read what, in one table; what Dens doesn't protect against; what leaves your computer; Linux and Windows compared; the short philosophy, with pointers to Signal and Qubes OS |

**The page links to the docs** where what they explain comes up: an i beside the heading, whose tooltip says what the guide covers and whose click opens it in a new tab (`DocsTip`). It stands at the first local password, the recovery codes and DM recovery key shown after joining, Password and recovery in Manage, signing in on a new device and approving one, a DM's check code, and creating the den, and the settings' About links the docs' home. That's v1's onboarding; a fuller one, in the page's own first steps, can come after it.

What the rest of this document commits the docs to, by page:

- Install:
  - Downloading the installer and running it, with its signature check as an optional step (see Privileged steps and updates).
  - Linux is recommended, since its service sandbox is stronger (Goals).
  - What Windows shows before running an installer that isn't Authenticode-signed, and how to check the download (Weaker than Linux). What it shows needs checking on a clean machine first: SmartScreen marks files a browser downloads, and the installer runs through PowerShell with `-ExecutionPolicy Bypass`.
- Join a den: what's unusual about Dens, in plain words: the local password and den passwords, recovery codes and the DM recovery key, den IDs, approving new devices and checking a DM's code. Onboarding says the same things, shorter, in the same words (see Before the first release).
- Host a den:
  - Which ports to forward but not how, since routers vary too much, and a small VPS as the easy alternative (Networking and deployment).
  - A minimal Caddyfile, and Caddy as a service on both platforms, which covers ports 80 and 443 (Caddy).
  - Calls need the den's name to point at its machine, or the router in front of it: a tunnel or CDN carries a den's HTTPS but not its calls. The owner's own calls work before any port is forwarded, so the test is a call from outside the network (Media addresses).
  - How to raise the call limits on a faster connection (Limits for calls).
  - A backup keeps what the den held when it was made, past the auto-delete period, until the backup is deleted (Retention).
  - Dens is one program, its database and media tools built in, so a den needs only Caddy beside it.
  - What a den costs in upload: members pick up where they left off rather than reloading, presence goes out in batches, and pictures go as previews until opened (Sync and bandwidth).
  - A second den is a second instance, with ports of its own (Install and service account). An install that hosts updates as one binary with its client, and a den that can't wait on its owner's updates moves to an install of its own (Versioning and compatibility).
- Privacy:
  - The den can read channels and the files in them, could listen to calls and watch screen shares, and sees who DMs whom and when; DMs and the photos in them are end-to-end encrypted (Privacy model, End-to-end encrypted DMs, Calls). The docs say the den rather than its owner: whoever or whatever gets hold of the den's computer, someone else who uses it or malware on it, can do the same.
  - Files that aren't photos, videos or audio, such as documents and archives, go as they are, with whatever they carry inside (Metadata stripping).
  - An update check asks the release host for the latest version number, which shows it the install's IP address (Privileged steps and updates), and a member who turns on YouTube's players shows YouTube their IP address (YouTube players).
  - Windows is weaker than Linux, as Weaker than Linux lists.
  - The short philosophy section and its recommendations: Linux over Windows, LibreWolf, no identifying details for low-stakes use, and Signal, Qubes OS and similar tools for higher stakes, none of them bulletproof (Privacy model).

## Milestones

Each milestone ends usable on its own and is tested on Linux and Windows with all four target browsers. All platform-specific code lands in M0, while the seams are still cheap to change; later milestones are shared code, but for M7's keys and the helper's start at login.

| # | Milestone | Done when |
| --- | --- | --- |
| M0 | Foundation: Sprout fork trimmed to the single-process lifecycle, elevated installers for Linux and Windows, `dens@`/`dens-<name>` service, platform layer, two listeners, vault with envelope encryption, `dens open` pairing | On both platforms: browser pairs, vault survives reboot, backup restores on a second machine, including Linux to Windows and back |
| M1 | Text den: invites, key auth and fallbacks, roles, channels, groups, DMs, presence, uploads with limits and metadata stripping, end-to-end encrypted DMs | Two machines chat through a Caddy-fronted den |
| M2 | Voice: calls in voice channels through the Pion SFU, on a UDP mux with ICE-TCP fallback; the signaling relay, with offers from the den as members join and leave; mute; the call bar | A clear two-person call across two home networks in the four target browsers, over UDP and with UDP blocked, and from the den owner's own browser |
| M3 | Group voice: Opus at 96 kbps, speaking indicators, each member's own volume for everyone else, staff disconnecting and muting members in calls, calls that ride out a dropped connection, and RNNoise noise suppression unless a member turns it off. Compact links: Reddit, YouTube, X and Amazon product links in one short form, and tracking parameters off every link. Third-party notices in the binary | A call of everyone the manual test brings together stays stable for an hour, and a YouTube share link arrives without its tracking but with its timestamp, in a channel and in a DM |
| M4 | YouTube players, for members who turn them on. Screen share: PLI forwarding, owner limits, viewer caps; a voice channel's bitrate as an owner setting | A YouTube link plays where it's posted for a member who turned players on, and 2 shares with 20 viewers within owner limits |
| M5 | Message retention. Managing files: each member's uploads by size against their limit, deleting and swapping them. Smaller copies of photos and videos by default, compared with full size before sending. ← and → through a message's images in the viewer | A member at their limit frees space by deleting and swapping old attachments; a phone photo goes as a smaller copy unless its sender sends it full size, and a phone video too large for the den as a copy that fits; and a den with retention on removes messages and their files once they pass it |
| M6 | Sync efficiency: the client's encrypted cache of files and history, the changes since it last synced, cached member lists, offline reading, compressed large frames | A client restarted after a day offline downloads only what changed |
| M7 | Global shortcuts: push to talk and the other voice keys while another program has focus, through a helper in the desktop session (see Global shortcuts) | A member pushes to talk from a full-screen game, on Windows and on GNOME and KDE under Wayland |
| M8 | Hardening, from the audit before the first release: one budget for each member's messages, and the den's ring of events held to a size; a control endpoint and a service that stay up on Windows; no member's local address in a call's answers; text that can't freeze a page; no downgrades, and signature checks only a flag turns off; backups that hold every file; owner invites from the den's machine; a pass over the page's wording, placement and small details | One member flooding every channel and DM stays within their budget and the den within its memory; Dens on Windows keeps answering a client that closes the control pipe at once; the den finds no local address in answers from Chrome or Firefox; a channel of hostile messages leaves pages responsive; an older release won't install over a newer one |

**M8 steps.** M8 lands as three pull requests, each checked in the four target browsers. The first two are built and committed in layers (the den and the service, the client and the page, the installers and harnesses, then the docs); the third changes the page alone:

| Step | Scope | Done when |
| --- | --- | --- |
| M8.1 Den and service | Each member's message budget across channels and DMs, and the event ring held to 64 MiB; bans deleting recent DM messages, deletes in DMs left to their authors, and bans lifted by their placer's rank; owner invites from the den's machine; the Windows control pipe answering after an early close, the control server accepting again after an error, and the SCM restarting a service that fails after starting; `tmp` emptied at start, and backups holding back deletes; snapshots without a scan of every message; a deadline for request bodies; nonces, names and the den's update lock; NixOS and SteamOS refused, and LXC's service drop-in; the keyframe test that raced | A member flooding every channel and DM is held to their budget, and the den's ring to 64 MiB; Dens on Windows answers after a client closes the pipe at once, and comes back after a failure; an owner locked out of their account takes the den back with `dens den owner-invite`; a backup taken while files are deleted restores every file its database names |
| M8.2 Client, page and install | Answers without addresses; markdown in linear time, parsed once per text; no downgrades; flags, not the environment, for release sources and skipping verification; error codes, address changes and redirects from a hostile den; the browser drivers in the repository; the docs; Sprout's leftovers | The den finds no address in any answer from Chrome or Firefox; a channel of 200 hostile messages scrolls and takes typing notices without stalling; an older release won't install over a newer one, and nothing in the environment changes where an install fetches from or whether it verifies |
| M8.3 Polish | A pass over the page before the first release, from notes taken in its browser tests: its wording, the settings in four tabs, home as a launcher with a Manage dialog for each den, one way to add a den, and smaller details; nothing outside the page changes | Every note is done, or set aside by agreement, and the page does everything it did in the four target browsers |

**M8.** The audit before the first release ran on 2026-10-11, at commit 1172baf. It verified the security checklist's open items against the code, and found what the first two steps fix. Two of its findings it confirmed with probes: the Windows control pipe's early close, run natively, and the address in Chrome's answers, with headless Chrome on Windows. It measured two more: the markdown parser on hostile text, in the pinned Node, and the snapshot's scan, in SQLite at a million messages.

M8.1:

- *A member's message budget.* A member's messages, in every channel and DM together, go at most 10 at once and then one every half second. Their text and sealed DMs go at most 256 KiB at once and then 16 KiB a second. Each channel's own limit stays. These are starting values, as protocol.md's other limits are. The router takes a message from the budget once its body is read, after the channel's own limit. The ring of recent events (`Hub`) counts the bytes its events hold and drops the oldest past 64 MiB, as it drops them past 50,000 or 10 minutes. A client whose resume point goes with them gets a snapshot, as now. The ring drops its oldest in place, moving the rest down only once they take less than half its slice, so publishing never copies the whole ring.
- *Bans and DMs.* A removal's `delete_messages` takes the member's recent DM messages too, with their files, and each DM's other member gets `message.deleted` as for any delete. In a DM only the author deletes a message, and the page offers staff no delete there. `DELETE /api/bans/{id}` takes staff of the rank of `banned_by`'s role now, or higher. The client's list of bans gives each the placer's role as it last heard, so the page offers to lift only those it may.
- *Owner invites.* `dens den owner-invite`, run by the desktop user, asks the service over the control endpoint for a one-time invite with the owner role, which lasts an hour. It's a new operation, re-authenticated with the local password as `backup` is. `OwnerInvite` makes the invite as it does for a den with no owner, now for any den. The page's setup keeps its own, `SetupInvite`, for a first join that failed: it asks for no password, so it still makes one only while the den has no owner. Redeeming an owner invite makes the joiner the owner and the owner before them a moderator, in one transaction: the joiner reaches everyone as any joiner does, with `member.joined`, and the owner before them with `member.updated`. Both are staff, so neither's sockets start over. The command prints the invite string for the page's Join box. On the den's own machine, joining replaces a sign-in to the den that no longer works, as joining any den again does.
- *The Windows control pipe.* A pipe instance whose client connected and closed before the service's `ConnectNamedPipe` reports `ERROR_NO_DATA`. The listener disconnects that instance and waits on it again, so it keeps holding the pipe's name; go-winio retries the same case. `control.Server` waits a moment and accepts again after any error but the listener closing, as `net/http` does, rather than stopping the service. It serves at most 64 connections at once, and on Linux it refuses another account's connection before reading its request (`host.PeerBeforeRequest`). Since that refusal can close the socket before the client's request is written, the client reads the service's answer even when sending fails.
- *Restarts on Windows.* `host.RunService` reports a refusal (`ErrRefused`) as a stop with `ExitRefused`, which nothing retries, as now. On Linux any other error exits 1 for systemd to restart. On Windows `RunService` records it in the event log and exits the process without reporting the stop, which the SCM takes for a crash and restarts.
- *`tmp` and backups.* The service empties the data directory's `tmp` as it starts, before the den or the client use it, and the den's own sweep of its upload parts goes. While a backup is written, the den's `fileStore` holds back taking files off the disk, and takes them off once the backup is done. A file the walk no longer finds is skipped and logged, rather than failing the backup.
- *Snapshots.* `ready`'s newest message in each channel comes from a subquery per channel the member sees, which the `(channel_id, id)` index answers at once. The `max(id) … GROUP BY channel_id` it replaces reads every message: 38 ms natively at a million messages, for each of the up to 500 snapshots a den restart or a channel turning staff-only sends at once.
- *Slow bodies.* `decode` gives a request's body a minute, through `http.ResponseController`, as `idleReader` gives an upload's reads.
- *Smaller things.*
  - With 100,000 nonces outstanding, the oldest goes, rather than the challenge being refused.
  - `CleanName` drops Unicode's format characters (`Cf`) and the fillers that render blank (U+115F, U+1160, U+3164, U+FFA0 and U+2800), and a name must keep something visible.
  - `Den.Update` writes the database before it takes the lock every request reads the den's description under, with a lock of its own keeping two updates in order.
- *Distributions it can't install on.* `Preflight` reads `/etc/os-release`, or `/usr/lib/os-release` without it, and refuses NixOS and SteamOS by their `ID` before anything changes, saying why. In a container it also refuses while LXC's service drop-in is in effect: while `/run/systemd/system/service.d/zzz-lxc-service.conf` exists and no file of its name in `/etc/systemd/system/service.d` takes its place. The refusal gives the commands that mask it.
- *The keyframe test.* `TestKeyframeRequests` waits for two keyframe requests more than before the burst of PLIs, not two more than when it starts waiting. On a fast machine the first request had arrived already, so the test waited for a third that never comes, and failed main's CI after #27 merged.

M8.2:

- *Answers.* `denproto.StripCandidates` also writes `0.0.0.0`, or `::` for IPv6, into every `c=`, `a=rtcp:` and `o=` line, as a browser's first answer has them, for answers and offers alike. Pion reads none of them. Chromium and Firefox name the address only in `c=`; the other two are there for whatever browser comes next. The test's fixtures are each browser's second answer in a call, taken in the browser checks' container, with a documentation address in place of the container's. Firefox's names its host candidates there only as it does once a page may use the microphone, rather than its mDNS names, so its fixture was taken with that set.
- *Markdown.* Each line's code spans are found first, in one pass, and each delimiter's closers in one pass the first time it opens, so a line takes time in proportion to its length, whatever its delimiters. A code span now takes precedence over a delimiter around it, as CommonMark's do: a backtick paired inside a delimiter's span is paired as it is outside. The page had paired them differently, and so highlighted a mention the den didn't count. The den's copy of the parser, which finds links to clean (`CleanLinks`), takes the same rule, and `links.json` and `mentions.json` gain cases that pin it. `trimLink` counts parentheses once. At 4,000 characters, the den's worst case went from 74 ms a message, unclosed openers among links it rewrites, to 4 ms; at 16,000, the page's from 160 to 700 ms a line to about one.
- *Parsing once.* `Markdown` keeps its parse in `useMemo`, keyed by the text, the reader's username and whether tasks tick, and `Preview` keeps its own. A kept tree is the same vnode, which Preact skips without diffing, so a row that renders again, as every row does while someone types, costs nothing for its text. A tick goes through a ref, so it reaches the row's current handler. The page's tests render components into a small DOM of their own (`test/dom.js`), which they hadn't before.
- *Downgrades.* `install.Install` refuses when an instance has a newer version than its own: the one installed, or the target of a transition it was in, whose service may have migrated. It says both, and checks before the plan, so a dry run says so too, and again under the lock. The same version installs again, to repair. `dens update` reads the release pointer first and stops if it names an older version than its own.
- *Flags, not the environment.* The installer scripts take `--release-url` and `--skip-verify` (`-ReleaseUrl` and `-SkipVerify`) and read no `APP_*` variable. `dens update` takes `--skip-verify`, which a binary carrying the release identity refuses, and passes both to the installer as flags; skipping, it doesn't fetch the bundle. The fixture releases are built without the release identity, and the harnesses pass the flags.
- *A hostile den.*
  - The page shows a den's error code only when it's one of the protocol's (`denproto.KnownCode`, which a test checks against the package's source). Otherwise it says the den refused, with the HTTP status. A den's code goes into logs cut to 64 bytes, as its message is to 200.
  - A den's member checks a new address for it at most once a minute (`followGate`); a check ends within the request timeout, so they also go one at a time. A den reached elsewhere can't send its members to plain `http`, or to an address on their own computer (`checkMove`), whether it moves them by signing another address or by announcing one. A member takes an announced address only once the den proves itself there, so a hostile den can't leave its members' installs asking someone else's server after it's gone.
  - The den clients' `http.Client`s follow no redirects, as the YouTube lookups don't.
  - Log lines that carry Go's network errors say what went wrong, as `Explain` does, and not the address: the client's, and the update check's.
- *The browser checks.* The drivers M2 to M7's container checks ran are in `scripts/test/browser/`, each a check named for what it covers, with its prep script, on one launcher, `browsers.mjs`, and the prep scripts' helpers, `lib.sh`. `setup.sh` makes the container (Debian 13 with Chromium, Firefox ESR, Xvfb and PulseAudio) and puts this tree's development binary, the voice probe and the pinned Node.js in it, with the media the checks send: four phone fixtures pinned in `scripts/vendor.sh`, the media module's own fixtures, speech from espeak-ng, and a photo Chromium draws. `run.sh CHECK` runs one. CI doesn't run them; they're the check before each manual test. Moving them found four checks that later milestones had left behind, which now check what's there instead.
- *Docs.*
  - The public site's `127.0.0.1` becomes `localhost`.
  - AGENTS.md's longest rows become short ones.
  - What this document's finished milestone plans still teach, such as spike findings and measurements, moves into the sections it's about, and the rest of those plans goes; the milestone table stays.
  - lifecycle.md and release.md change with the flags.
- *Other distributions.* The installer's refusals give their reasons, and in a container the commands that mask LXC's drop-in, and name no guide. The distributions it refuses get a Flatpak after v1 (see Other distributions), which takes in SteamOS.
- *Sprout's leftovers.* `ui.isIgnored`, the template functions nothing uses and the un-hashed `/assets/` route go; `assetPath` fails on an asset the manifest lacks, rather than naming a path nothing serves. `pkg/x` and `pkg/crypto` go, and what else `pkg/` held that nothing called. `sfu.TestCaller` is `sfutest.Caller`, in `internal/sfu/sfutest`, which knows the den only by the protocol, as the page does: the stream a share comes in is `denproto.ScreenStream`. Told to reach a den on loopback, it gathers nowhere else, so the test binaries open nothing a firewall asks about.

M8.3:

- *Scope.* The page alone, from the notes taken in M8.2's browser tests: its words, where things sit, and how its forms and flows go. The den, the protocol and the local API stay as they are, since the page already gets everything the notes need; only the local service's messages that name the DM seal change, to the page's word for it. It's the one pass over the page's words before the first release, so wording held back for it comes due here. The page's tests change where they check wording, and gain cases for what the page now works out itself; the browser checks follow its new structure.
- *Its rules.*
  - Helper text is one line. The label does the work, and details, such as 2,560 pixels or 30 ms of delay, go in a tooltip.
  - The words people know: compress, cache, resolution, fps and shortcuts, not "goes smaller", "sealed with its key", "size", "a second" and "keys".
  - No implementation names: noise suppression, not RNNoise.
  - Only what applies. The page knows its browser and system, so it describes theirs alone, and it shows a figure, such as how much the cache holds, rather than a sentence about it.
  - A warning comes with the action it's about, in its confirmation, rather than ahead of it in a paragraph.
  - Nothing says the den or its owner can see something, which members take as given in a place someone else hosts (see Choices).
- *Settings,* in four tabs:
  - **Appearance**: the theme, with System, Light and Dark as three large choices, and the other themes in a menu of swatches under More themes. No footnote.
  - **Chat & media**:
    - Compress photos and videos: "Sends faster and saves space. You can still choose the original before sending." The sizes go in a tooltip.
    - Links: "Dens removes tracking parameters and shortens links," with a tooltip of examples, and Open Reddit links on old.reddit.com.
    - Play YouTube videos in Dens: "YouTube will see your IP address when previews load."
  - **Voice & video**:
    - Microphone and speaker.
    - Noise suppression: "Removes keyboard, fan and background noise."
    - Sending your voice: Voice activity or Push to talk. Under voice activity, Automatic sensitivity ("Detects when you're speaking") sits with the sensitivity slider it stands in for, which hides while it's on. The meter keeps no paragraph, and Test microphone sits beside it.
    - Keyboard shortcuts: each a field that reads Not set or its key, and records a key when clicked, with Clear beside a set one, and no description under its name. One line says where they work: "Work while Dens is focused. To use them in other apps, keep the Dens helper running," or, where the desktop holds them (M7.2), how to set them up with it, change them or stop. Nothing about a held key let go on switching away.
    - Screen share: Resolution, and Frame rate as 30 fps; Optimize for Clarity or Smoothness, with no paragraph; and Share audio, with one line for this browser and system: a tab's audio, or on Windows the system's too, or off where Firefox shares none. A den's lower limits show only in a call whose den sets them.
  - **System**:
    - Cache: "Reduces network use, and lets you view what's cached offline. Using 0 MB of 2 GB," with Keep up to and Clear cache.
    - Updates, as now.
    - Local password: "Protects Dens on this computer. It's separate from your den passwords." Change password shows the fields.
    - Logging.
    - This browser: Sign out, with `dens open` set as code.
    - About: the version, with the instance when it isn't main, which the home page's footer showed, and the licenses.
- *Home, a launcher.* Five sections in each den's card don't scale past two or three dens.
  - Each den is one row: its name, a dot for its state, named in words when it isn't connected, "Signed in as john · Owner", a large Open and a small Manage. A den that signed this device out, or no longer has its member, keeps Sign in again and Remove from this computer in its row.
  - Sign-ins waiting for this device's approval show above the list, as sign-ins in progress on this device do, and each says to refuse one that isn't yours and change your den password. So does a den whose DM recovery key this device lacks.
  - Add a den is one button, which asks for one thing: "Paste an invite or den address". An invite shows its den, then asks whether to join as someone new or sign in to the account you have; an address goes to signing in. Signing in goes a screen at a time: the username and den password, with "Forgot your password? Use a recovery code"; then "Approve this on another device", with "No other device? Use a recovery code"; then the digits to compare; then done. The service holds a sign-in under way, so a reload comes back to its screen.
  - The cog reads App settings, as the den page's does.
  - Creating the den this install hosts stays a card above the list until its owner account exists.
  - How long a den keeps messages, which its card said, shows in Manage's General.
- *Manage,* a dialog for each den, in two parts:
  - **This den**:
    - General: the den's address and Den ID, with copy buttons, which every member sees, since members compare Den IDs. The owner edits the name and the public address here ("The address members use to connect", with Test beside it), and the note to keep the old address working for a while shows once the address is edited.
    - Invites, for staff: Expires after, Max uses as a number, and Create invite; the new invite to copy; and Active invites (n), listed with Revoke.
    - Bans, for staff: "bob (@bob) · Banned Oct 12, 2026", with Unban and a tooltip on what a ban does.
    - Limits, the owner's: Uploads (Max file size, Storage per member and Total storage, with their units in the fields); Auto-delete messages after [90] days ("Applies to channels and DMs. Members can see this," with the count of what a new period deletes at once); and Calls ("Calls and screen shares pass through this computer's network. Lower these if calls stutter."). Calls offers Small group, a new den's limits, which need about 20 Mbps of upload at full load; Large group, 30 in a call, 60 in calls across the den and 2 shares of 15 viewers, about 70 Mbps; and Custom, the only one that shows the fields: Max people per call, Max people in calls (whole den), Allow screen sharing, Max shares at once, Max viewers per share, and a share's max bitrate, resolution and frame rate. "Upload needed at full load: about 20 Mbps" follows them as they change.
    - The edits in General and Limits gather under one Save changes bar, shown while something has changed, which sends them as one update. Closing with changes unsaved asks first.
  - **You in this den**:
    - Devices: the list, and Sign out, whose confirmation says it ends that device's access at once, and that signing in again takes the den password and another device's approval.
    - Password and recovery: Change password shows the fields, with "Signs out your other devices"; "10 codes left. Use one to sign in if you forget your password," with Generate new codes, whose confirmation says new codes replace all the old ones; and the DM recovery key, "You'll need this to read your DMs if you lose all your devices. Keep it secret," with Show key and Reset key, whose confirmation says what starting over does.
    - Your files: "0 B of 2 GB used", and the den's total for its owner only.
  - A den that isn't connected shows General alone, with Den moved? for one that can't be reached. Leave den sits at the foot of the sections.
- *The den page.*
  - The member list starts open, in every channel, until the member closes it, on screens wide enough to show it beside the channel.
  - A text channel's #, a voice channel's speaker, the one the call bar shows, and the lock on staff-only channels and DMs are drawn icons of one size and weight, in place of emoji.
  - The Direct messages tab shows its dot when a DM waits for this member: unread, or with check codes to compare, as when someone starts one.
  - The notes that the den or its owner can see something go: at the start of a channel, on a voice channel and the share button, and in the profile editor. A DM keeps its lock and Encrypted.
  - The alerts that point at the home page's sections, for a new device signed in or a sign-in refused, open Manage instead.
- *Dates* take one form, the medium one, as Oct 12, 2026, with the time where it matters, as when an invite expires.
- *The browser checks* open the settings at a tab and a den's Manage dialog at a section through two helpers in `browsers.mjs`, and find a channel by its name, since its # is an icon.
- *Choices.*
  - Who can read what. "Say plainly who can read what, in the UI as well as the docs" (see Identity and encryption) becomes: the page marks what's end-to-end encrypted, DMs, and onboarding, the milestone after this one, and the public docs say what a den's owner can read and hear, once, rather than a note in every place.
  - The DM seal is the DM recovery key on the page, the name other encrypted apps have taught people; recovery codes keep theirs. This document keeps "DM seal" for the thing itself.
  - No warning ahead of a full disk, which would take the den reporting its free space: the upload it refuses says so then ("This den is out of space for uploads").
  - Moderators keep Invites and Bans; General's edits and the limits are the owner's.

**M8 testing.**

- Go tests:
  - The den:
    - A member's messages over their budget, spread across channels and DMs, are refused with `rate_limited`, while each channel's own limit still holds.
    - The ring drops by bytes, and a resume from before the drop gets a snapshot.
    - A removal deletes the member's recent DM messages and their files, with an event to each DM's other member.
    - A staff delete in a DM is refused, and the author's goes through.
    - Only the owner lifts a ban the owner placed.
    - An owner invite moves the owner role, and the previous owner becomes a moderator.
    - The snapshot's read states match the old query's, on a den with DMs and staff-only channels.
    - A body sent slower than its deadline is cut off.
    - With the nonce map full, the oldest nonce goes.
    - A name of fillers alone is refused.
  - Install: `Preflight` refuses NixOS's and SteamOS's `os-release`, and a container where LXC's service drop-in is in effect, and goes on once it's masked.
  - The service: `tmp` is empty after a start that found files in it.
  - The backup: one taken while the den deletes files holds every file its snapshot names.
  - The control endpoint:
    - On Windows, a client opens the pipe and closes it before the next accept, and the next client is served, as the audit's probe ran.
    - The server accepts again after a failed accept.
    - On Linux, another account's connection is refused before its request is read.
  - `RunService` stops with `ExitRefused` on a refusal and exits as a crash on another error, through a seam for the exit.
  - Install:
    - An older binary is refused over a newer installation, and the same version installs again.
    - `dens update` refuses an older pointer.
    - A binary with the release identity refuses `--skip-verify`.
  - The protocol: answers from Chromium and Firefox come out with no address, and parse. They're fixtures taken in the container, with documentation addresses in place of real ones. Every error code the package declares is one the client may show, and hostile lines of 16,000 bytes yield their links within 100 ms, where the old parser took 160.
  - The client:
    - An unknown or overlong error code isn't shown.
    - A den's member checks one new address a minute; a den reached elsewhere can't move its members to an address on their computer, live or as they sign in again; and a redirect isn't followed.
    - A network error goes into a log line without its address.
  - `TestKeyframeRequests` passes many runs in a row with `GOMAXPROCS` at 1, 2 and the machine's.
- Page tests:
  - Hostile texts of 16,000 characters, four times a message's limit, parse within 50 ms, where the old parser took up to 700 and the new one about one.
  - A row's parse is kept across renders that don't change its text: a render with a new handler makes no new nodes for it.
  - A tick reaches the row's current handler.
  - The shared cases pin the code-span rule both parsers take.
- The harnesses:
  - The lifecycle e2e installs with the flags on both platforms, and the Linux one checks that an older fixture release won't install over a newer one.
  - The den e2e runs `dens den owner-invite`, and the member who redeems it becomes the owner.
- In the container, both browsers:
  - A call's answers reach the den without an address (`answers`). The den sits behind `den-proxy.mjs`, which decodes the frames clients send it and logs every answer, while the page records what the browser made: each browser names its address in its answers after the first, and no answer that reaches the den does. Against main's binary, the check finds the container's address.
  - A channel of 200 hostile messages scrolls and takes typing notices without stalling (`hostile`): no frame waits 400 ms. Against main's binary, frames waited up to 800 ms; this step's wait at most 34.
  - Every check M2 to M7 ran, on the shared launcher.
- The manual test:
  - The four target browsers on Windows.
  - A call through members joining and leaving, and a screen share.
  - A den restart.
  - An owner invite redeemed on another machine.
  - On Windows, the SCM's recovery actions on the service (`sc.exe qfailure dens-main`). Nothing outside the service makes it fail on purpose, so `TestServiceFailureEndsAsACrash`, run natively, checks that a failure ends the process as a crash, which those actions answer.
  - Optionally, M7.2's X11 path on a real X11 desktop, such as Linux Mint with Cinnamon.
- M8.3: the page's tests, with cases for what the page now works out itself: whether a pasted text is an invite or an address, which preset a den's call limits match and the upload a set of them needs, the share-audio line for each browser and system, and the one form of dates. The browser checks, following the page's new structure, in both browsers. The four target browsers on every screen a note touches.

**Before the first release:** a deep audit of the whole project, done on 2026-10-11: its security first, against the checklist in Security hardening, whose unticked items it verified, then its code, tests and docs. What it found is M8's first two steps. Then the public site, whose docs explain what's unusual about Dens in plain words: the local password and den passwords, recovery codes and the DM seal, den IDs, approving new devices and checking a DM's code, and who can read what. The page links to them where each comes up (see Public site), which is v1's onboarding. Then v1.0.0.

**After v1 loose ideas:** onboarding in the page's own first steps, with simple, careful explanations tried on people who haven't seen Dens, bookmarks (per member and per den, so a den's bookmarks always resolve against that den), polls from the composer's + menu, a Flatpak for joining dens from the distributions the installer doesn't support, SteamOS and NixOS among them (see Other distributions), TPM binding for the Windows data key, optional TOTP on the password fallback, simulcast, TURN, end-to-end encrypted calls with SFrame and calls in DMs with them, an optional idle lock, a static dictionary for compressing the den socket's small frames, should measurements show live events matter (built from the protocol's shapes, never members' messages, and named in the upgrade so client and den agree on its version), AVIF: stripped in place by `internal/media`, keeping the original as sent, with dav1d in the module for previews of AVIF images and AV1 videos, and dedup of each member's own channel uploads: their Dens hashes a file once it's stripped, the den keeps the hashes keyed with its data key so its database doesn't show which files it holds, and a file on several messages goes with the last of them.

## Open questions

- [ ] SELinux labels for the binary and `/var/lib/dens` on Fedora and Bazzite, which containers can't test; needs a VM or a real install.
