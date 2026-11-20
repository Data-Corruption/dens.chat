# Keys and sign-in

Part of [Dens's design](../design.md): the vault and its key, backups and moving a den, the den's sign-in, devices and their approval.

## Local identity and encryption

Each install has one local user, stored in an encrypted vault, and one random data key that encrypts it. The data key is wrapped twice: once bound to the host for unattended boot, and once by the user's password for backup and moving machines.

### What the vault holds

- The list of joined dens: URL, den identity key fingerprint, display name.
- One Ed25519 keypair per den, plus the current session token for each.
- The DM seal.
- Each den's record of the identities the member's Dens kept there, and those they verified.
- Local settings and preferences.

Sensitive fields in SQLite are encrypted one by one with the same data key (XChaCha20-Poly1305): on a client, the vault; on a den, message and DM text, and the names of uploads. A den's uploads are encrypted with it too (see [Storage](files.md#storage)), and so is everything a client caches from other dens, in a database and directory of the cache's own (see [The client's cache](sync.md#the-clients-cache)). This is a speed bump against scanners and backup snoopers, not protection from root or a live compromise, and it doesn't hide anything from the den owner, whose install holds the den's key. Metadata such as timestamps and IDs stays plaintext so queries work.

### Envelope encryption

1. Install generates a random 256-bit data key.
2. A host-bound copy lets the service start without a password:
   - Linux: an encrypted credential loaded with `LoadCredentialEncrypted=` in the unit. systemd decrypts it at start, bound to the host key or TPM, and hands it to the service in `$CREDENTIALS_DIRECTORY`.
   - Windows: a machine-scope DPAPI blob in the state directory, whose DACL allows only the service account, SYSTEM and Administrators. The service decrypts it at start.
   - The Flatpak: none, so it starts locked until the member types the local password, unless they ask it to remember. Then the app's secret from the Secret portal, kept in their keyring, wraps a copy, or the key is kept in a file where the portal can't be trusted (see [flatpak.md](../flatpak.md)).
   - Android: a Keystore key, which the app's shell uses to unwrap the data key at start (see [android.md](../android.md)).
3. A second copy is wrapped with a key derived from the local password (Argon2id) and kept in the storage root.

The privileged CLI does the host wrapping (at install and restore); the service only unwraps. Password wrapping and all field encryption are shared code.

### Setting the password

Installers never prompt, so they stay non-interactive. The first `dens open` leads to a page that sets the local password, and the service wraps the data key with it. `dens password` does the same from the command line, or changes the password given the current one, so a server never needs the page; the Flatpak keeps its password in its page, whose first asks whether to remember its key. Until a password exists, the service runs on the host-bound copy alone, and backup and key export stay disabled.

Because this is a root-managed system unit on Linux, it avoids the unprivileged `systemd-creds --user` path entirely. `LoadCredentialEncrypted=` arrived in systemd 250, which becomes the Linux minimum version. Every supported distro ships 252 or newer, and the platform spike confirmed encrypted credentials on 252 through 262.

### Backup, restore, move

- `dens backup` writes a consistent SQLite snapshot (`VACUUM INTO`), the uploads directory, and the password-wrapped data key. Never a raw copy of a live WAL database. It leaves out the client's cache, which holds only copies of what dens hold. While a backup is written, the den holds back taking deleted files off the disk, so the archive has every file its snapshot names. The archive is a tar at gzip's fastest level: the uploads are sealed, so they don't compress, and the default level takes them at an eighth of the speed, 116 MiB/s against 950 with Go 1.26.7, which releases build with.
- A backup cut short is never kept as whole. A body on the control endpoint comes in frames and ends with an empty one, so `dens backup` fails on one that stops partway and keeps nothing, and each write has a minute, so a long archive streams to its end; the command waits up to an hour for the database's snapshot, before the archive's first byte. The page's download drops its connection when the archive fails, so the browser marks the download failed. The service says at least how large the archive will be before it streams: `dens backup` refuses a folder without room for it, and the page shows the size.
- The settings' Backup exports the same archive as a download, which the Flatpak needs: its CLI's working directory is inside the sandbox, not the member's. After the local password, a client route gives a link that works once, for a minute, which streams the archive.
- The data directory's `tmp`, which holds files only while the service works on them (uploads arriving, scratch files, a backup's snapshot), is emptied as the service starts, so nothing a crash left there outlives it.
- `dens restore <file>`, run as root or Administrator, asks for the password, unwraps the data key, and re-wraps it for the new host (`systemd-creds` or DPAPI). Data is never re-encrypted. The archive's database comes before its uploads, so restore checks the password and the key as soon as it's out, before any upload; and it refuses a backup the disk hasn't room for, keeping a gigabyte free as uploads do, rather than holding archives to fixed sizes, which a den an owner lets hold more would outgrow, each upload with a preview being two files. Restore follows the same lifecycle steps as an update, and a backup from an older version migrates when the service starts; one from a newer version is refused. A session form's restore runs as its user, stopping its service first; the Flatpak's restored instance starts locked, and opens with the backup's local password.
- Pairing doesn't carry over: the service clears the restored data's browser sessions on its first start, so browsers pair again with `dens open`.
- Nor does hosting: a restored backup starts with hosting off, whatever it held, so a copy of a den never goes online by itself, as when a backup is restored to read it. Its owner turns hosting on with the backup's local password.
- A den moves to another computer with its **export**, not a backup. A backup holds the whole install, the client's accounts in every den it joined, with their device keys and DM seals, included, so a server restored from one would be the same devices as the owner's computer: online at once, and signing that device out from either signs out both. And the install a den left would keep its copy, which its client reaches on loopback whether hosting is on or off, so it would never follow the den. `dens den export`, and the settings' Move the den to another computer, write an archive in the backup's format whose database holds the den alone (`database.SnapshotDen`): the den's tables, `den` and `den_*`, as they are, and the vault's check value and data key sealed under the local password; every other table empty, the vault's DM seal gone, and a new install's settings. Those rows go with `secure_delete` on, and the copy is vacuumed, so no free page keeps them. It holds every file the den holds, DMs' included, with no way to leave them out: once the copy the den left is removed, and members' caches drop with the new sync epoch, nobody would have them any more. Its manifest says it's a den's export, and names the den. An export refuses while hosting is on, so nothing reaches the den after its snapshot; the page's Move turns hosting off first, after the local password. It's recorded in the configuration (`DenExported`), and turning hosting on forgets it, after a confirmation that another computer may host the den by now, and members would then reach whichever one its address points at.
- Restoring an export is restoring a backup: hosting starts off, no browser is paired, and the den starts a new sync epoch. The server's client holds no account, so it connects to nothing, and home's Finish setting up shows only while the den has no owner, which the dens view says, so a server holding an export offers nothing to set up. The restore names the export, and ends with `dens den hosting on` where a backup's says to pair a browser. A restore replaces everything the instance holds, so an export goes only where nothing would be lost, as a new server's, or an instance of its own on a computer whose Dens is in use: restore refuses one over an instance that hosts a den or has joined one, as soon as the export's database is out, saying what the instance holds, and pointing to an instance of its own, or an uninstall where nothing in it needs keeping, which is the deliberate way to empty an install, so no flag forces the restore. The same den, on an instance that has joined none, is a move done again with a later export, and restores. Restore runs as root, so it reads the instance's database without leaving a file beside it that the service account couldn't open: read-only while the service runs, through the WAL and index its connections keep, and as immutable once it's stopped, when closing has folded the WAL in; a WAL a crash left without its index it doesn't read, and refuses. A backup still replaces any instance, since replacing what's there is what restoring one is for. The Flatpak, which hosts no den, refuses an export before any upload is written. An older release restores an export as a backup, since it ignores the manifest's new field.
- The install a den left deletes its copy (`dens den remove`, the settings' Remove from this computer) once another computer serves the den: with hosting off, an export since hosting was last on, and the den answering at its address from this computer and proving its identity there, as the challenge of Manage's Test does, which with hosting off here means another computer serves it. A refusal names the check that failed, and for the address what to try: the other computer behind Caddy, the name pointing at it and the router forwarding there, or, where a router can't reach its own public address, a hosts entry with the server's local one. Then, after the local password, and with hosting held off, one transaction deletes every den table's rows, with `secure_delete` on, and the log is checkpointed; the uploads go, as backups being written let them; and the den forgets its identity, its key and what it held in memory of its members, so `OwnDen` reports none. The client's connection to the den starts again at its address, as any den's would, keeping the account there, owner or not, with its history and DMs, without a restart. The record of the export goes too. Files a crash leaves behind go as the service next starts, which tidies the uploads with no den as with one.
- `dens den hosting on` and `off` turn hosting on and off over the control endpoint, as the settings' switch does, so a server needs no browser: on takes the local password, and prints the den's address, the port Caddy forwards to and the media ports, or the port that's held and the installer flag that moves it; off asks first, which `--yes` skips. `dens status` shows the switch, why a den doesn't serve, and an export not followed by hosting.
- A restored den starts a new sync epoch on that first start, so its members' clients drop what they cached from it (see [What changed while away](sync.md#what-changed-while-away)). Its ID counters, but the change log's, move on to the restore's time in microseconds: the backup's are where they were when it was made, so a message, channel or member made after the restore could otherwise take the ID of one the restore lost, which a link to a message, in another den or outside Dens, where no epoch reaches, would then open. The time is past every ID the den gave out, counted from 1, or from an earlier restore's time at fewer than a million a second.
- Backups have the same format on both platforms, so a client or den can move between Linux and Windows.
- Changing the password re-wraps the data key only.
- Lost password and lost machine together means the local data is gone by design. Den accounts can still be recovered with recovery codes.

### Re-authentication for sensitive actions

There is no idle lock in v1; the OS screen lock covers someone at an unlocked desktop. Instead, a paired browser must re-enter the local password for sensitive actions: exporting a backup, viewing or exporting keys, viewing the DM seal, changing the local password, restoring, turning hosting on, which opens ports to the internet, having the Flatpak remember its key, and exporting the den or removing its copy. Someone at an unlocked machine could read chats, but not take the identity with them. An optional idle lock can come later.

### Service hygiene

- The data key and vault stay in memory only in the service process; nothing sensitive goes to logs.
- Core dumps are disabled (`LimitCORE=0` on Linux; `dens.exe` excluded from Windows Error Reporting), and key material is held in locked memory (`mlock` or `VirtualLock`) so it isn't swapped to disk: the data key, the seeds of the den's identity key and of each device key, and each den's DM seal. Go's Ed25519 caches expanded keys through weak pointers, which can't point outside the Go heap, so each signature expands a short-lived copy of the key and clears it. A DM key is opened from its sealed copy only while a message or file is sealed or opened, and cleared after. The member's identity comes from the seal as it's used, and its private keys are cleared after, but for the copies X-Wing's library keeps, which go with the garbage collector; a DM key sealed to an identity, once it opened, is kept in memory sealed with the seal, as a legacy DM key's copies are (see [Legacy DM keys](e2ee.md#legacy-dm-keys)), so it opens without X-Wing as it's used.

## Authentication

Members log in to a den by signing a challenge with a per-den Ed25519 key, fully transparently. The password is a fallback for new devices, recovery codes are the fallback for a forgotten password, and TOTP is cut from v1.

**Den-side auth is simple because the local service holds the credentials.** The browser never sees den tokens. A Go client can send `Authorization` headers on the WebSocket upgrade, which browsers can't, and it can re-sign a challenge any time. The key does the job refresh tokens usually do, so there are no refresh tokens, rotation or reuse detection.

### Joining (invite redemption)

1. The invite is a versioned string, carrying the den's ID, a code and the den's address, that the member pastes into a Join box (see [protocol's Identity and keys](../protocol.md#identity-and-keys) for its layout and [protocol's Invites](../protocol/auth.md#invites) for its uses). No invite links in v1, so nothing suggests a central account or server.
2. The client generates a new Ed25519 keypair for this den and pins the den's ID, its identity key's hash.
3. It sends the invite's code, username, a password verifier (see below) and the public key, signed with the new key. The den stores a hash of the verifier and the key.
4. The den returns 10 recovery codes, shown once and stored hashed.

### Password verifiers

The raw password never leaves the client. For each den, the client derives `verifier = Argon2id(password, salt = SHA-256("dens-den-password-v1" ‖ den_id ‖ username))` (t=3, m=64 MiB, p=4, 32 bytes) and sends that, and the den stores `SHA-256(verifier)`.

- A malicious or breached den learns nothing it can replay at another den, so reusing one password across dens, including the local password, is safe.
- Offline guessing against a den's database costs a full Argon2id per guess, paid by the attacker.
- The den never runs Argon2id itself, so login attempts cost it almost no CPU.
- Usernames can't change after joining, since they salt the verifier. Display names can.

### Normal login (invisible to the user)

1. Client sends its own random nonce and requests a challenge. The den returns a 32-byte nonce for the client (60 seconds, single use; with 100,000 outstanding, the oldest goes, rather than the challenge being refused) and signs both nonces and its own public address with the den identity key. The client aborts if the signature doesn't match the pinned fingerprint, and sends nothing more to an address the den didn't sign (see [Den identity and moving domains](#den-identity-and-moving-domains)).
2. Client signs `"dens-auth-v1" ‖ den_id ‖ key_id ‖ nonce`. The context string and den ID stop a signature being replayed elsewhere.
3. The den verifies the signature and issues an opaque 256-bit session token (stored as SHA-256), valid for 1 hour.
4. The WebSocket upgrade carries the token. Before expiry the client re-signs and sends an `auth.renew` frame on the open socket.
5. Revoking a key or banning a member deletes its sessions, and the den closes those sockets immediately.

### Fallbacks

- **New device:** username and password verifier plus a new public key. The den registers the key, labels it, and tells the member's other sessions, whose chat shows that a new device signed in. One of the member's other devices must also approve it (see [Approving new devices](#approving-new-devices)).
- **Forgotten password:** username, one recovery code, a new password verifier and a new public key. The code is spent, the new password replaces the old one, and every other device is signed out.
- **Change password:** from a signed-in session, with the current password or a recovery code. It signs out every other device: a device key outlives the password that added it, so anyone who had the old password may still hold one.
- **New recovery codes:** from a signed-in session, with the password, so a stolen session can't make codes to keep the account with.

A new device names the den with any invite from it, even a used one, which pins its identity, or with its address, which trusts the key the den proves there. Trusting an address is safe for the password, since a verifier made for one den is worthless at another. The page shows each den's ID, the start of its identity, so a member can compare it with someone else's.

Among a den's sections in the settings are **Devices** and **Password and recovery**. Devices lists each registered key with its label, when it was added and when it last signed in, and signs any of them out, this one included, closing their sockets at once. Password and recovery changes the password, which signs out every other device, and makes new recovery codes. A device that was signed out says so and offers to sign in again.

### Approving new devices

A password alone doesn't add a device: one of the member's other devices approves it, which makes their own devices a second factor with no authenticator app.

1. The new device signs in with the username and password as now. The den holds it as pending for 10 minutes and asks the member's other sessions, which show the request with the new device's label.
2. The new device and one of the member's existing devices, the one they approve from, run an exchange of one-time keys through the den: X25519 and ML-KEM-768 used together, so it stays safe if either is broken, and ML-KEM guards against traffic recorded now and decrypted later by a quantum computer. The new device starts it with its sign-in. It runs in commit-first order, three messages: a commitment to the new device's key, the existing device's key, then the new device's key. The new device's key is fixed before it sees the other's, and the other's is sent before it sees the new device's, so a den in the middle can't choose keys that make both sides' digits match; it gets one blind guess. Each device derives a 32-digit check code from the exchange and shows half of it, and keeps showing it until the other device no longer needs it: the new device until it's signed in, and the approving one, after it approves, until the member is done. The member types each device's digits into the other, so a request from someone else, whose screen the member can't see, can't be approved by accident; and if the den had swapped in keys of its own, the digits wouldn't match.
3. The existing device seals the member's DM seal with the exchange's key and sends it through the den, which carries it but can't open it. Only then does the den register the new device and start its session. The new device makes the member's identity from the seal, so it holds what their other devices do, with nothing more to hand over.
4. Refusing a request, or letting it expire, keeps the device out. A refusal also tells the member to change their password, since someone has it. A new password, starting over, or leaving the den cancels what's waiting.

- The new device says at once that it waits, Waiting for your other device with a spinner, over where to look: its dialog takes the sign-in from the request's answer, and the service publishes it to the pages as it starts waiting.
- On the approving device, a request opens as a dialog over whatever is on screen, Home, a den or a dialog, a sheet on a phone, one at a time, oldest first: who asks and in which den, It's mine or Refuse, then this device's digits under their code, beside a box for the new device's, and once approved, Approved with them still shown. It closes by itself once the new device connects, when the den publishes `device.connected` for the first socket of a device it let in by approval; a den older than v1.14 sends none, so there the member closes it, which ends its digits, as it lapsing does. Closed before it's approved, it waits as a line above Home and above that den's channel, whose Show opens it again. A refusal opens a second dialog that leads to changing the den password. That a new device signed in is one notice on the den's page, gone in a minute, and a New mark on its row in Devices for a day.

- Both checks matter: the existing device mustn't hand the seal to anyone else, and the new device mustn't take a seal from anyone else, or DMs it starts would be sealed under a key the den knows.
- The device that joins with an invite needs no approval. A member with no other device signs in with a recovery code, which needs none, and types their saved seal to read their DMs: the codes are the one way in without a device. With neither devices nor codes, the account can't be recovered.
- The den enforces approval, but DM privacy doesn't rest on it: a device the den lets in on its own gets no seal, so it can't read DMs, and the identity it would publish isn't the member's, which shows to everyone who kept theirs as a change, and in any comparison of codes.
- Approval keeps its typed digits, though every DM opens without any: a device the den slipped past it would take the seal, and with it every DM, and no comparison of codes would show it, since the DMs themselves were never touched. Fewer digits would do, since the den gets one blind guess, but an updated device would then show fewer than one that isn't asks for.

### Sign-in codes

A new device can scan what it would otherwise type, with no change to the sign-in underneath: two QR codes, each holding only what its screen shows beside it.

- *Add device's.* `dens-device:<address> <username>`: the den's address as this install keeps it and the member's username there, which Add device shows under the code as text, for a device with no camera. Scanned in Add a den, it opens the sign-in with both filled in; the den password is still typed.
- *The digits'.* The approving device's 16 digits, alone, which a numeric code holds smallest, shown above them from It's mine on, so the new device scans them before or after the approving one types its own. Scanned on the new device, they're checked as typed digits are.
- *Read as hostile.* A code is whatever someone shows the camera. It reads as one of the two or as nothing, by a grammar stricter than what's typed: an `https` address, or plain `http` on this computer (`localhost`, `127.x.x.x` or `[::1]`), a host name or IPv4 address in lowercase with maybe a port and nothing after it, 200 characters at most; a username's characters; or exactly 16 ASCII digits. `denproto.ReadSignInCode` and the page's `signincode.js` read by the same rules, both tested against `denproto/testdata/signincodes.json`, and what reads only ever fills the fields a member would type.
- *Made.* The service makes them with `rsc.io/qr`'s encoder (`denclient/codes.go`): error correction M, from version 2, so every code has an alignment pattern a reader corrects a tilted camera by, up to version 10, which holds an address of 168 characters with the longest username, and the mask that scores best by ISO/IEC 18004's penalty, which `rsc.io/qr` leaves to its caller. It hands the page each row's modules, which the page draws as an SVG, black on white with a quiet zone of four modules on every theme.
- *Read.* By the page's own reader (`qr.js`), from frames of the camera, the back one where there are two, scaled to 640 pixels at most, a few a second: a local threshold over blocks of 8 pixels, the finders by their 1:1:3:1:1 runs checked across and down, a module's size along the lines between them, the alignment pattern near where they put it, a projective map from the code's corners, then the format, Reed and Solomon's correction, and numeric, alphanumeric and byte segments, the bytes as UTF-8. It reads versions 1 to 10 and refuses any other kind of segment. The browser's `BarcodeDetector` doesn't stand in: Android's WebView lists QR codes and reads none where Google Play services lacks its barcode module, as the emulator does, and as phones without Google's services do, with no way for the page to tell; desktop Chromium on Linux and Windows, and Firefox, have none. Its tests read the codes the service makes (`denclient/testdata/qrcodes.json`, written by `TestPageCodes`) from their modules, and drawn turned, tilted, blurred, dim, noisy and among other shapes.
- *Scan shows* where the browser lists a camera, as one does before it's let use it, unnamed; the Android app asks for CAMERA as the page first opens the camera, and grants the page the camera alone. A refused camera says where to allow it, as a refused microphone does.

### Den identity and moving domains

- Creating a den generates its Ed25519 identity key. It is stored encrypted with the den's data key and included in den backups.
- Clients store each den under its key fingerprint; its address can change.
- The den signs its public address into every challenge answer. A client that reached it at another address sends nothing more there: it runs the challenge again at the signed address, and moves there only if the den proves the same key.
- So an owner moves a den by changing its address in the den's settings, and keeps the old name pointing at the den for a while. A connected client moves as soon as the den announces the new address and proves itself there; one that was off moves at its next start, through the old name. A member whose install was off for longer enters the new address by hand, where the same check applies.
- The signed address also stops a relay. A server at an address the den left, or any other, can pass the den's answers along, but the address in them sends the client to the den itself before it sends a proof, a password verifier or a token.
- A passing check means the new server runs from the den's backup, unlocked with its local password. Normally that's the owner; it cannot rule out someone who stole both.
- A den chooses the addresses its members' clients ask, so a hostile den could turn its members into a crowd asking someone else's server, or their own machines' services on loopback. So a client checks a new address for a den at most once a minute, never follows a den from an `https` address to a plain-`http` one, nor from an address elsewhere to one on its own computer, and follows no redirect from a den, which the protocol never sends.

### Why cut TOTP

TOTP protects against a stolen password. Here the password is only used when adding a device, every such login is announced to the member's existing devices, and online guessing is rate-limited, with each guess costing the guesser an Argon2id computation. A new device also needs approval from one of the member's other devices, which are a second factor already. The key, which does daily logins, is unphishable and never leaves the machine. TOTP adds setup friction for little gain; it can be added later with `pquerna/otp` as an option on the password fallback only.

### Rate limits

- Password and recovery attempts: per account and per IP, 10 and then one every 6 minutes; IPs held in memory only. A signed-in member's password checks, to change it or make new codes, count against the account's.
- Challenge requests and invite redemption: per IP.
- Messages: per member and channel, and one budget per member across every channel and DM, in messages and in bytes, so spreading messages over many channels or DMs doesn't multiply what a member may send. Message text counts against no storage limit, so this budget is what bounds how fast one member can grow the den's database.
- Unknown usernames and wrong passwords get the same error, and the check is a constant-time hash comparison either way, so neither the reply nor its timing shows which usernames exist.
- A request's body has a minute to arrive, as an upload has for each read, so a client that sends slowly can't hold the den's handlers forever.
- A sound played from the soundboard counts toward the member's writes, past the wait the owner sets between their sounds.

Limiter memory is bounded. Token buckets live in fixed-capacity LRU maps of 100,000 entries, a few MB each: when one is full, the least recently used bucket goes. IPv6 addresses are keyed by their /64 prefix so rotating addresses doesn't create new buckets. A flood from enough addresses can push a bucket out and start it over, which buys a password guesser little: a password alone doesn't add a device, and each guess costs them an Argon2id. The per-account limit is keyed by username and refills on its own, so it never locks an account, since lockouts let an attacker lock victims out: a stream of guesses can hold up the member's own password sign-ins, but never their devices, which sign in with keys. The nonces the den hands out for sign-ins are bounded the same way: with 100,000 outstanding, the oldest goes, rather than the den refusing every sign-in. A client uses its nonce within moments, so a flood can't outrun it.
