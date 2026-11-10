# Working on Dens

Orientation for coding agents and contributors. Read this before changing
anything, and read the design docs before changing anything they cover.

## What this is

Dens is self-hosted chat for small communities: text, DMs, voice and screen
share. One Go binary runs as a system-level service under its own account. It
always acts as a client, serving the chat UI to the desktop user's browser on
loopback, at `http://localhost:<port>`, and holding their keys, and it can
also host one den (a community). Linux (systemd) and Windows 11 (SCM) are supported for both roles.
The binary's elevated maintenance commands own install, update, restore and
uninstall; the installer scripts only download, verify and hand off to them.
On the distributions the installer refuses, a Flatpak runs the same binary as
the member, in their session, and hosts no den. On Android, an app runs it as
the child process of a thin Kotlin shell, with the page in a WebView, and
hosts no den either (M12).

The target design is [docs/dev/design.md](docs/dev/design.md), with each deployment form's own beside it: [installed.md](docs/dev/installed.md), [flatpak.md](docs/dev/flatpak.md) and [android.md](docs/dev/android.md).

## Where things live

| Path | Owns |
|------|------|
| `cmd/main.go` | Process entry, global flags, signal handling, final error print; beside it, the Windows resource objects that carry `dens.exe`'s manifest (generated and committed) |
| `internal/app` | `App` composition root, cleanup stack, update checks |
| `internal/app/commands` | CLI commands: desktop-user commands talk to the service; maintenance commands run transactions |
| `internal/service` | The service process: listeners, hosting (the switch that serves the den), control handlers, readiness |
| `internal/install` | Install, update, restore and uninstall transactions with a rollback journal; `system_linux.go` and `system_windows.go` hold every install-time OS step |
| `internal/maintenance` | Lifecycle state (`state.json`), locks, start authorization |
| `internal/layout` | The deployment forms, and every filesystem path and its permission policy; nothing else resolves paths |
| `internal/instance` | Per-instance config written at install: ports, desktop user, release URL, and v1.0's den role, which hosting starts from |
| `internal/denproto` | The client-to-den protocol both sides share: wire types, signed layouts, verifiers, invites, name rules, the rule that takes tracking out of links, the cryptography of private DMs and device approval, the list of Unicode's emoji (`emojilist.go`, generated and committed, M15.2), a poll's rule of what each member sees (M15.4), and members' identities, their codes, the DM keys sealed to them and the record of those kept (`identity.go`, M16.1), and a sealed call's keys, their copies, commitments and statements (`callkeys.go`, M16.2), and group DMs' changes and their rules, their names, and their messages and sets signed inside the seal (`groups.go`, M16.3), and the times a DM's or a group's messages disappear after (`disappearing.go`, M16.4), and the key a member's notes are sealed under, made from their seal (`notes.go`, M17.3), and events: their limits, wire types and checks (`events.go`, M17.4), and badges, an event's and a profile's, and an ended event as a link's card reads it (M17.5), and a name's style, its fonts and its checks (`namestyle.go`, M19.5) |
| `internal/den` | The den this install hosts: members, invites, sessions and devices, the event hub and its sockets, messages, uploads and files, retention, DM key relays, calls' rules, the soundboard's sounds and their playing (M14.3), the den's emoji (M15.2), reactions, a DM's sealed by its members (M15.3), polls, their totals sent to whom their rules allow, and their end, and staff's @everyone (M15.4), members' identities, the DM keys sealed to them and each member's sealed record (M16.1), a call's keys passed to its members, each their copy, and the latest from each kept while it lasts (M16.2), group DMs reserved, their changes checked and kept in order, their members, their keys for each membership and those leaving the den taken out, and nothing of them for a Dens speaking version 1 (M16.3), a DM's and a group's time to disappear, and its messages deleted at theirs on the timer that ends polls (M16.4), a channel's files sent as spoilers, and staff moving a member from one call to another (M17.2), members' notes, one each, made as they first open them, taking sealed messages alone, nothing of them for a Dens speaking version 1 or 2, and gone as their member leaves (M17.3), events: made within the owner's limit, changed and canceled by rank, Interested counted, started and ended by the timer, the start told to the interested alone, and staff's alone in a staff-only channel (M17.4), read by their IDs for links' cards, an ended one staying where it was seen, and badges: attached by staff, earned by a minute at a stretch in a live event's call, timed in memory and only the award written, and given with profiles (`badges.go`, M17.5), but those their members hide, on their own profiles alone (M17.6), a member's name style, checked and kept with their record (M19.5), and the change log clients sync from |
| `internal/denclient` | The dens this install has joined: keeping each connected, the den's state for the page, history and files through the cache, DMs and their seal, device approval, calls relayed for the page, and the smaller copies of photos and videos made before sending, and animated pictures (M15.1), and the den's emoji, kept in the cache ahead of their use (M15.2), and reactions, a DM's sets sealed and opened here, applied to the cache's copies (M15.3), as a poll's events are (M15.4), and identities: this member's, published, those of others kept on first use, their changes and the DMs that hold on one, codes compared, the record written every hour, and the DM keys sealed to identities, made, checked and opened here (M16.1), and a sealed call's keys, made as its members change, sealed to their identities, others' checked and opened here, and handed to the page whole (M16.2), and group DMs: their changes fetched and checked, their members as those make them, their keys for each membership, their names, and their messages and sets signed and checked here, and the protocol's version each den shares (M16.3), and disappearing messages: a DM's or a group's time set, and what the cache let go of at its time taken off the pages and out of the notifications (M16.4), and a DM's spoilers sealed with its files, and the move staff made of the page's call (M17.2), and the member's notes, opened, and sealed and opened here under the key their seal makes, never notifying or unread (M17.3), and the den's events, kept with its state for the page offline, and the start of one the member is interested in, notifying them (M17.4), and an event read for a link's card, from the state where it can be, and a profile's badges checked, hidden ones on this member's own alone (M17.5, M17.6), and a member's name style, checked as a den sends it and before one is set (M19.5) |
| `internal/sfu` | The den's calls: Pion peer connections on the media ports, offers, voice forwarded to the call and shares to their watchers, within the owner's limits, and each call's soundboard track, which plays the den's sounds; `sfutest`, Pion in a browser's place, and a sound as the page writes one, for tests and the den e2e |
| `internal/cache` | The client's cache of other dens (M6), under `data/cache` and out of backups: files, sealed as they're read, message history, each den's state and members, its size and eviction, and disappearing messages let go at their time, by a timer of its own (M16.4) |
| `internal/shortcuts` | Voice keys in other programs (M7): the service's side of the shortcut helper's stream, and the helper (`dens shortcuts`), which reads the keys during a call through Windows, X11 or Wayland's portal, and shows the member's notifications where no page can (M12.3) |
| `internal/notify` | Notifications (M12.3): what notifies the member and where it shows, a page through the browser, the helper or the Android app's shell, the settings and mutes, and taking a notification back once its channel is read; and from M14.2 where a message's sound plays, and the page's sounds' settings and which of them the member replaced, and from M15.4 staff's @everyone and its switch, and from M17.4 an event's start, and Events, on unless the member turns it off |
| `internal/youtube` | The covers of YouTube's players (M4.1): a linked video's title and picture, fetched for the page from YouTube's fixed addresses, checked, and held in memory |
| `internal/media` | What a file is, taking image metadata out without re-encoding, previews, which photos get smaller copies, a video's copy, planned to fit and made in chunks, or by the page, with what WebCodecs decodes a video by, and Opus in Ogg read packet by packet, for the soundboard, and an animated GIF's or WebP's frames made again as a picture's GIF (M15.1); the client strips with it and the den checks with it |
| `internal/media/ffmpeg` | The media module: FFmpeg, libaom and Dens's C driver (`driver/`) in WebAssembly, translated to Go (`module/`, generated), the worker process each job runs in, and the Runner that answers its reads and writes, with the chunks of a video's copy side by side |
| `internal/platform/host` | Runtime OS seams: service host, data key unwrap, control endpoint, locked memory, and the shortcut helper's keys (Windows' key state, X11's over a small client, and Wayland's global shortcuts portal over D-Bus) and its taking up a new binary, and the desktop's notifications for it (the freedesktop service, the Flatpak's portal, or Windows' toasts); Android's, as `_android.go` files, for locked memory and the data key from the app's shell |
| `internal/control` | CLI-to-service protocol over the control endpoint, with the stream the shortcut helper keeps |
| `internal/vault` | Data key envelope: host and password wraps, key check value, signing keys in locked memory |
| `internal/backup`, `internal/pairing` | Backup archives; one-time pairing tokens |
| `internal/platform/database` | SQLite open and pool, ordered migrations, focused accessors per table |
| `internal/platform/http` | Listeners, client and den routers, guards, handlers |
| `internal/platform/release` | Reads the root `version` pointer from the release host, and checks a release's signed checksums where no cosign runs, as the Android app's updater does, against Sigstore's trusted root, embedded (M12.4); `sigstoretest`, a Sigstore of the tests' own, for tests and the Android e2e's fixture releases |
| `internal/types` | Configuration shape |
| `internal/ui` | The page: a Preact app (JSX under `assets/js/src/`, tests under `test/`), its shell template and CSS, the share player, the copier that makes videos' smaller copies with WebCodecs, the soundboard, which prepares staff's sounds with WebCodecs too (M14.3), the voice worklet with RNNoise's module (`assets/wasm/rnnoise.wasm`, generated and committed), its sounds (`assets/sounds/`, Kenney's, CC0), and emoji: the picker, completion and Manage's Emoji, with the list of Unicode's emoji the page fetches (`assets/emoji.json`, generated and committed, M15.2), reactions' chips under messages (M15.3), polls, from the composer's + to under their question (M15.4), and comparing codes, the verified mark, the line where a member's keys changed and the hold (M16.1), and the worker that seals and opens a call's frames with SFrame (`assets/js/sframe.worker.js`), and the call's lock (M16.2), each sender sealed as its offer is applied, and stopped where its browser sends it around its seal, the worker counting each track's frames (M18), and group DMs: making one, their rows, their members beside them, their change lines and their gate (`groups.jsx`, M16.3), and disappearing messages: a DM's or a group's time beside it, in its header and as its line, each message's mark, and the list dropping them at their time (`disappearing.jsx`, M16.4), and Appearance's Verified mark (`verified.js`), and from M17.2 its Density (`density.js`), and from M19.1 the settings in Manage's frame, staff dragging channels, groups and members (`drag.js`), and spoilers' covers, and the member's notes: their row atop the DMs and their icon (`notes.jsx`, M17.3), and events: the Events row atop the Den tab, their pane, cards and form, and their times in the member's time zone (`denevents.js`, `denevents.jsx`, M17.4), and links to events, their chips and cards (`eventlinks.jsx`), and badges, in the event's form and card and on profile cards (M17.5), and the profile card as a card, staff's crown and shield, its ⋯, and Edit profile's eyes on badges (M17.6), and right-click menus (`menus.jsx`, M19.2), and themes of the member's own, their editor and their codes (`themes.js`), and the picture behind the chat (`background.js`, M19.4), and name styles: a name in its member's font and colors, moved to read on the reader's theme (`names.js`, `names.jsx`), Edit profile's Name style and Appearance's Name styles, and the fonts, which the build copies into `assets/fonts/`, and the sizes a message's photos, videos and YouTube players show at (`sizes.js`, M19.5) |
| `internal/build` | Values baked in at build time, the third-party notices the binary carries (`notices.txt`, generated and committed), and `winres`, which writes the resource objects in `cmd/` that put the Windows manifest in `dens.exe` |
| `pkg/` | Small reusable packages: locks, rotating logs, HTTP helpers, prompts, migrations, sd_notify |
| `android/` | The Android app's shell (M12): Kotlin with no AndroidX, its manifest and resources, the newer framework calls it makes behind a version check, and the release key's certificate (`release.pem`), which a release's APK must be signed by |
| `scripts/build.sh`, `scripts/build/` | Project values (top block of `build.sh`), local builds, artifact helpers, and the Android app's APKs, built without Gradle, with the check that stands in for Android Lint's NewApi |
| `scripts/ci.sh`, `scripts/ci/` | Release planning, a pull request's test jobs chosen by the files it changes, publication and recovery, and the Flatpak's publication after each release |
| `scripts/vendor.sh` | Pinned versions and SHA-256s for every third-party tool; the only fetcher |
| `scripts/ffmpeg.sh` | Builds the media module from its pinned inputs, checks the committed one, packages FFmpeg's source for releases, and fuzzes the driver |
| `scripts/rnnoise.sh` | Builds RNNoise's module for the page from its pinned inputs, and checks the committed one |
| `scripts/emoji.sh`, `scripts/emoji/` | Writes the list of Unicode's emoji, the page's and `denproto`'s, from Emoji's test file and CLDR's annotations, pinned in `vendor.sh`, up to the Emoji version the systems draw, and checks the committed ones; `scripts/emoji/` is the generator |
| `scripts/notices.sh`, `scripts/notices/` | Writes the third-party notices from the module cache and, for the pinned inputs that end up in the binary, the license copies kept in `scripts/notices/` |
| `scripts/install.sh`, `scripts/install.ps1` | The installer bootstraps; templated by `build.sh` |
| `scripts/flatpak.sh`, `scripts/flatpak/` | Makes the Flatpak's commit from a release's binary, with its desktop file, MetaInfo and icon, commits it to a repository, and writes the repository's deltas and summary; beside them, the public key it's signed with (`dens.asc`) |
| `scripts/test.sh`, `scripts/test-*`, `scripts/test/` | Test entrypoints, lifecycle harnesses, fixture releases, the browser checks (`scripts/test/browser/`), run by hand before each manual test, and the Android e2e's container, drivers, den and fixture releases (`scripts/test/android/`), whose `phone.sh` also works a phone over wireless debugging, and `bg.sh` holds its call off screen |
| `spikes/` | When one is under way, a throwaway experiment that answers a design question before code depends on it, in its own Go module; it goes when the work it informs lands, its findings in the design docs, which name the commit that last held it |
| `docs/dev/` | Internal docs: the design and each deployment form's, the milestones and loose ideas, the lifecycle, the den protocol and the release process |
| `docs/` (everything else) | The public dens.chat site (Hugo and Hextra) |

## Documents of record

- [docs/dev/design.md](docs/dev/design.md): processes, deployment forms and the
  platform layer, identity and encryption, authentication, den features, media,
  security checklist.
- [docs/dev/installed.md](docs/dev/installed.md): the service under its own
  account on Linux and Windows: install, accounts and sandboxes, updates, paths
  and permissions, maintenance transactions and the installer scripts.
- [docs/dev/flatpak.md](docs/dev/flatpak.md): the Flatpak (M9), run by the
  member in their session: its sandbox, locked start and remembered
  key, lifecycle with no installer, updates, its publishing, and its harness.
- [docs/dev/android.md](docs/dev/android.md): the Android app (M12 and M13):
  its design, its build, its form, its shell and the page in its WebView, its
  calls, notifications and updates, and its e2e.
- [docs/dev/milestones.md](docs/dev/milestones.md): what each milestone built,
  and the plan for the one under way.
- [docs/dev/loose-ideas.md](docs/dev/loose-ideas.md): ideas and fixes no
  milestone has taken up yet.
- [docs/dev/lifecycle.md](docs/dev/lifecycle.md): the lifecycle state and
  locks every form shares, development instances, and the lifecycle e2e
  harnesses.
- [docs/dev/protocol.md](docs/dev/protocol.md): the client-to-den protocol:
  authentication, the WebSocket, events, history and limits.
- [docs/dev/release.md](docs/dev/release.md): publication order, resume,
  retention, signing identity, and the Flatpak's key and job.
- `docs/content/`: public docs for people using Dens.

When a reference document and the code disagree, the code is right and the
document is stale; fix the document in the same change. The design docs also
cover what isn't built yet; if your change shows the design is wrong, fix it
in the same change.

## Rules that are not obvious from the code

**Fail closed.** Unknown state, unreadable state, a permission that's too
loose, a symlink where a file should be, an installer pair that won't verify:
reject and say why. Never repair silently; a wrong mode is evidence.

**The localhost page is the most valuable target.** An XSS there reaches every
joined den, the vault and the local API, and any den member can send content,
so all of it is hostile input. No raw HTML, ever: messages go through the
escaping markdown subset, and usernames, filenames and embeds are untrusted
text. Keep the CSP strict with no inline scripts. The only frame it allows is
YouTube's player from `youtube-nocookie.com`, sandboxed, and the page never
listens to a frame's messages. The client listener checks `Host` exactly, and
`Origin` on every write and WebSocket upgrade. The page lives at `localhost`,
not `127.0.0.1`: YouTube's player refuses a page at an IP address.

**Client and den stay apart.** Client routes and den routes live on separate
routers behind separate listeners. Never mount a client or admin route on the
den router, which Caddy exposes to the internet.

**Collect less.** Never log message content, tokens, keys or IP addresses; log
IDs and event types. Don't write IP addresses to disk. Deletes remove rows, and
SQLite `secure_delete` stays on.

**Only the maintenance commands change an installation.** `dens install`,
`update`, `restore` and `uninstall` run elevated as transactions in
`internal/install`. The installer scripts only download, verify and run
`install`, and `dens update` only fetches, verifies and runs the installer.
The service never replaces its binary, edits its unit or SCM registration, or
writes lifecycle state; it migrates its data only when `state.json` names its
version as the target of a transition. A session form's service, a development
instance's, the Flatpak's or the Android app's, has no installer, and publishes
its own lifecycle state, deciding under its lifecycle lock (see
`docs/dev/lifecycle.md`).

**Starting the services is the point of no return.** Until then a
transaction's journal undoes every step. Once a service may have migrated,
failure keeps the transitional state and the operator reruns the installer.
Do not add code that pretends a downgrade happened.

**Migrations are ordered functions in
`internal/platform/database/migration.go`.** Database steps and the
`user_version` bump commit together. Non-database side effects in a step must
be idempotent because an interrupted step can run again. A step that remakes a
table, as changing a table's checks takes, goes in with `AddRemaking`, whose
run turns foreign keys off and checks them before it commits: dropping a
table with them on deletes every row that refers to it.

**Released formats move forward.** v1.0.0 shipped, so installed copies hold
data every later version reads. Add migrations at the end and never change a
released step. `instance.json` and `state.json` decode strictly: keep every
field a release wrote readable, and give a new meaning a new field rather than
an old one's name. A backup from any release restores. A change to one of these
comes with a test that reads its released form.

**The release identity is frozen at the first release.** The cosign signing
identity includes the path `.github/workflows/release.yml`; never rename or
move that file. The artifact layout in `docs/dev/release.md` is fixed once
installed copies exist.

**Protocol changes follow the versioning rules.** Adding fields or message
types doesn't bump the client-den protocol version, and receivers ignore
unknown message types. Only breaking changes bump it (see the design doc).

**OS-specific code lives in two places.** Runtime seams go in
`internal/platform/host` as function pairs in `_linux.go` and `_windows.go`;
install-time steps go behind the `System` interface in `internal/install`.
Go builds `_linux.go` files for Android too, so where Android differs, an
`_android.go` file takes the seam's place and its Linux one says `!android`.
Nothing else gets build tags. A platform difference must never reach the
protocol, database or HTTP layers; if a feature seems to need one, raise it as
a design problem. What differs by how Dens is deployed, its form (installed,
a development build's instance, the Flatpak, or the Android app), is chosen
at runtime from `layout.Form`, as a `switch` on the form where it differs,
never by build tags. Like a platform difference, a form never reaches the
protocol or the database, and reaches the page only as what it can do.

**Windows code compiles only under `GOOS=windows`.** After touching a
`_windows.go` file, run `GOOS=windows go vet ./...` and
`GOOS=windows go test -c -o /dev/null ./<pkg>` to catch build breaks. From
WSL, `./scripts/test.sh -windows` runs the Go tests natively on the Windows
host; CI runs them on a Windows runner. Android code likewise compiles only
under `GOOS=android`: after touching an `_android.go` file, run
`GOOS=android GOARCH=arm64 go vet ./...` (amd64 needs cgo even to vet), and
`./scripts/test.sh -android-e2e` runs the host package's tests in the emulator,
as the app.

**SQLite uses a modest fixed pool (4) per process.** The Wasm driver gives each
connection its own memory sandbox and SQLite serializes writers anyway.
`go.mod` pins `ncruces/go-sqlite3` to a floor that fixes a Windows WAL
corruption bug; do not lower it.

**Rotating-log failures are sticky only when the file is.** `pkg/xlog/rlog`
disables itself after an I/O error on the log file; a lock-acquisition timeout
or a prune failure returns an error for that write and the next flush retries
with the buffer intact. Keep that split when touching the writer.

**Dependencies must earn their keep.** Standard library first;
`golang.org/x/` is fine. A new third-party module needs to solve a non-trivial
problem cleanly without dragging a tree behind it.

**The binary carries every third-party license.** After adding or upgrading a
Go module, or a pinned input whose code ends up in the binary, run
`./scripts/notices.sh` and commit what it writes. A new input of that kind
also needs its license copied into `scripts/notices/` from its source at the
pinned version, and a section in the script. A test fails on a module the
notices don't name, and CI on notices that aren't current.

**Test files end in `_test.go`, nothing else.** `cmd/hygiene_test.go` fails on
`*_test_*.go` names and on `testing` reaching the shipped binary.

**Line endings.** `.gitattributes` forces LF for `*.go` and `*.sh`, and for
the notices the binary embeds, which a test reads line by line. Some
PowerShell and a few other files are CRLF; do not "fix" them wholesale.

**The media module is generated, and committed.** Never edit
`internal/media/ffmpeg/module`. Change the driver or `scripts/ffmpeg.sh`, run
the script, and commit what it writes beside the change; CI regenerates it and
fails on any difference. What the module makes is untrusted like any member's
file: check it as you would theirs. A test package that runs media jobs gets a
`TestMain` that runs the worker when `ffmpeg.TestWorkerEnv` is set, and its
Runner from `ffmpeg.TestRunner`.

**So is RNNoise's module.** Never edit `internal/ui/assets/wasm/rnnoise.wasm`.
Change `scripts/rnnoise.sh` or `internal/ui/rnnoise`, run the script, and
commit what it writes; CI builds it again and fails on any difference. The
module imports nothing, and the voice worklet gives it nothing.

**And the list of emoji.** Never edit `internal/ui/assets/emoji.json` or
`internal/denproto/emojilist.go`. Change `scripts/emoji/`, or the pins or the
Emoji version in `scripts/emoji.sh`, run the script, and commit what it
writes; CI writes them again and fails on any difference.

**The names' fonts are pinned, and never committed.** The build copies them
from their packages into `internal/ui/assets/fonts`. A font is its pin in
`scripts/vendor.sh`'s `NAME_FONTS`, its name in `denproto.NameFonts` and in
`names.js`'s `FONTS`, in one order, and its license in `scripts/notices/`,
with the notices written again; tests fail on one that lacks any of them.

## Build and test

```sh
./scripts/test.sh              # go test -race ./... with embed placeholders; run constantly
./scripts/test.sh -lint        # pinned shellcheck over the shell scripts; run after touching them
./scripts/test.sh -js          # the page's tests on the pinned Node; run after touching the page's scripts
./scripts/test.sh -docs        # the site built as the Docs workflow builds it, failing on any warning, and its icon checked; run after touching the site
./scripts/test.sh -release     # release state machine against a local rclone backend
./scripts/test.sh -e2e         # lifecycle e2e across the supported distros in Incus containers
./scripts/test.sh -den-e2e     # a member joins a den through Caddy, chats, calls, and is banned, in two Incus containers
./scripts/test.sh -flatpak-e2e # the Flatpak's publisher, then the Flatpak, made from this tree and published: launched, unlocked, updated and rolled back, in Incus containers
./scripts/test.sh -android     # the Android app built, as CI's android-build job: vetted for Android, both APKs, the host tests for the emulator
./scripts/test.sh -android-e2e # the Android app, made from this tree, in Android 17's and 12's emulators: paired, joined to a den behind Caddy, chatting, sending and saving a photo, in a call off screen, and updated from fixture releases
scripts/test/android/phone.sh SERIAL COMMAND  # a phone through the Android container's adb: pair, connect, install, log, pull
scripts/test/android/bg.sh SERIAL [SECONDS]   # a phone's call with its screen off, ten minutes unless given, watched and summed up
./scripts/test.sh -windows     # from WSL: the Go tests, run natively on the Windows host
scripts/test/browser/setup.sh  # the browser checks' Incus container, with this tree's build and the site's in it
scripts/test/browser/run.sh CHECK [BROWSER...]  # one check in Chromium and Firefox, before a manual test
./scripts/build.sh             # dev binary: runs a development instance as you, -dev storage, debug logs
./scripts/build.sh --prod      # production-mode binary for this architecture
./scripts/build.sh --prod-all  # all release binaries
./scripts/build.sh --android   # the Android app's development APKs, out/android-{arm64,amd64}.apk
./scripts/ffmpeg.sh            # regenerate the media module after changing its driver or build
./scripts/ffmpeg.sh --fuzz 10m # fuzz the driver in the module and natively under AddressSanitizer
./scripts/rnnoise.sh           # rebuild RNNoise's module for the page after changing its build
./scripts/emoji.sh             # rewrite the list of Unicode's emoji after changing its generator or its pins
./scripts/notices.sh           # rewrite the third-party notices after a dependency or pin changes
go test ./internal/build/winres -args -update  # rewrite cmd's Windows resource objects after changing the manifest
gofmt -l ./cmd ./internal ./pkg && go vet ./... && GOOS=windows go vet ./... && GOOS=android GOARCH=arm64 go vet ./...
```

The `-release`, `-e2e`, `-den-e2e`, `-flatpak-e2e` and `-android-e2e` harnesses,
and the browser checks, run on Linux only, the Android e2e on x86_64 with KVM;
the harnesses' logs land under `out/`. The Windows harnesses (`scripts/test-lifecycle-e2e.ps1`,
`scripts/test-den-e2e.ps1`) install real services, so they run in CI or on a
Windows machine without Dens; see [docs/dev/lifecycle.md](docs/dev/lifecycle.md).

Third-party tools and frontend inputs (Tailwind, DaisyUI, esbuild, Preact,
cosign, rclone, shellcheck, goimports, Hugo, Node.js for the page's tests,
Caddy for the den e2e, the media module's FFmpeg, zlib, libaom, CMake,
wasi-sdk, binaryen and wasm2go, RNNoise's source and model, Unicode's emoji
test file and CLDR's annotations, the names' fonts, and the Android app's
NDK, SDK pieces, emulator, Kotlin and JDK) are pinned by version and SHA-256 in
`scripts/vendor.sh` and fetched into the gitignored `tools/`.
Never depend on `tools/` contents directly.

Generated and ignored: `internal/ui/assets/{css/output.css,js/output.js,manifest.json,fonts/}`,
`out/`, `tools/`, `docs/out/`. Edit sources under
`internal/ui/assets/css/input.css` and `internal/ui/assets/js/src/`.

## Where to add things

- CLI command: constructor in `internal/app/commands`, registered in
  `commands.go`. A test scans the AST and fails if you forget to register it.
- Durable state: a migration step, then accessors beside the owning subsystem.
- HTTP route: a handler on the client router (`internal/platform/http/client`)
  or the den router (`internal/platform/http/den`), never both.
- OS-specific behavior: a seam in `internal/platform/host`, or a `System`
  method in `internal/install` for an install-time step.
- Project values (name, release URL, contact, ports): the block at the top of
  `scripts/build.sh`.

## Style

Explicit errors wrapped with `%w`, `errors.Join` for cleanup, `sync.Once` for
idempotent close, context cancellation as the cooperative stop everywhere
(doesn't need handling *everywhere*, e.g. database txns. Just try not to block
forever). Comments explain why an ordering or check exists, not what the next
line does. Tests use only the standard `testing` package, open real SQLite in
`t.TempDir()`, and spawn real subprocesses for cross-process claims. The page's
tests likewise use only Node's `node:test` and `node:assert`, with no npm
packages. Where the Go and page code must agree, as on mentions, tasks and
links, both test against one shared file of cases in a `testdata` directory.
Keep it that way.

## Writing

What a change says matters as much as what it does, and runs long unless
held short. Every kind of text has one job:

- **A code comment** says why an ordering, a check or a limit exists, in a
  line or two above the code it explains. It never narrates the next line,
  the milestone that added it or the alternative that lost. A comment that
  restates its code goes.
- **A design doc line** describes how Dens behaves now, as one sentence a
  contributor can check against the code. No history, no "we chose", no
  account of the session that produced it; the rationale that matters is one
  clause, not a paragraph.
- **Text in the page** is a label, or one line of what to do. The guide
  holds the rest, linked (design.md's *The page's words*).
- **A changelog line** says what a member notices, in their words, and for a
  fix what went wrong and since when.
- **A commit message** says what changed and why in a few lines; **PR text**
  is for the reviewer: what to look at, what was checked, what's deliberately
  left out.
- **The milestone plan** states options and the pick with one reason each,
  and leaves the rest to the build.

When a draft explains, cut: a reader who wants the why has the docs, the
commit and the review. The test is whether a line would be missed; if not, it
goes.

## Documentation

Public docs under `docs/content/` are for people using Dens: members joining a
den and owners hosting one. Explain what it does, how to use it, and the
constraints that affect them, especially who can read what. Define unfamiliar
terms before relying on them. Internal docs under `docs/dev/` are for Dens
contributors.

Avoid "Doylist" framing: narrating implementation history, a recent refactor,
rejected alternatives, or the conversation that produced a change. These docs
are not a changelog. Describe current behavior directly; put change history
and implementation rationale in commits or PR descriptions. Include rationale
in user docs only when it helps the reader make a decision or understand a
constraint. The design doc may explain why a decision was made, but it still
describes the design, not how it evolved. A milestone's plan lives in
`docs/dev/milestones.md` while it's under way; once the milestone is done, what
the plan still teaches moves into the design docs' sections and the rest goes.
