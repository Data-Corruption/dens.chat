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
hosts no den either.

The target design is [docs/dev/design.md](docs/dev/design.md), with each deployment form's own beside it: [installed.md](docs/dev/installed.md), [flatpak.md](docs/dev/flatpak.md) and [android.md](docs/dev/android.md).

## Where things live

Each Go package's doc comment says what it owns (`go doc ./internal/den`),
and each of the page's source files opens with a comment saying what it
holds.

| Path | Owns |
|------|------|
| `cmd/main.go` | Process entry, global flags, signal handling, final error print; beside it, the Windows resource objects that carry `dens.exe`'s manifest (generated and committed) |
| `internal/app` | `App` composition root, cleanup stack, update checks |
| `internal/app/commands` | CLI commands: desktop-user commands talk to the service; maintenance commands run transactions |
| `internal/service` | The service process: listeners, hosting (the switch that serves the den), control handlers, readiness |
| `internal/install` | Install, update, restore and uninstall transactions with a rollback journal; `system_linux.go` and `system_windows.go` hold every install-time OS step |
| `internal/maintenance` | Lifecycle state (`state.json`), locks, start authorization |
| `internal/layout` | The deployment forms, and every filesystem path and its permission policy; nothing else resolves paths |
| `internal/instance` | Per-instance config written at install: ports, desktop user, release URL, and v1.0's den role |
| `internal/denproto` | The client-to-den protocol both sides share: wire types, signed layouts, and every rule both apply alike, the cryptography of DMs, identities, sealed calls and group DMs among them |
| `internal/den` | The den this install hosts: members, devices, the event hub and change log, messages, files, calls' rules, events and badges, and what members seal, kept unread |
| `internal/denclient` | The dens this install has joined: each kept connected, its state, history and files served to the page through the cache, and everything sealed, sealed and opened here |
| `internal/relay` | Relay: links between the member's own installs on one network, which carry voice keys and a computer's sound |
| `internal/sfu` | The den's calls: Pion peer connections on the media ports, forwarding voice, shares and each call's soundboard track; `sfutest`, Pion in a browser's place, for tests and the den e2e |
| `internal/cache` | The client's cache of other dens, under `data/cache` and out of backups |
| `internal/shortcuts` | Voice keys in other programs: the shortcut helper (`dens shortcuts`) and the service's side of its stream, which also carries notifications and Relay's sound |
| `internal/notify` | What notifies the member, where it shows, and the sound it makes |
| `internal/youtube`, `internal/steam` | The covers of YouTube's players and Steam's cards, fetched for the page from fixed addresses, checked, and held in memory |
| `internal/lookup` | What those two share: the memory cache, the request that names nothing of the member, the capped read and the checks |
| `internal/media` | What a file is, metadata taken out without re-encoding, previews, smaller copies of photos and videos, and Opus for the soundboard; the client strips with it and the den checks with it |
| `internal/media/ffmpeg` | The media module: FFmpeg, libaom and Dens's C driver (`driver/`) in WebAssembly, translated to Go (`module/`, generated), and the worker process each job runs in |
| `internal/platform/host` | Runtime OS seams, the shortcut helper's and Android's among them |
| `internal/control` | CLI-to-service protocol over the control endpoint, with the stream the shortcut helper keeps |
| `internal/vault` | Data key envelope: host and password wraps, key check value, signing keys in locked memory |
| `internal/backup`, `internal/pairing` | Backup archives; one-time pairing tokens |
| `internal/platform/database` | SQLite open and pool, ordered migrations, focused accessors per table |
| `internal/platform/http` | Listeners, client and den routers, guards, handlers; `routetest`, which records a router's routes for the tests that fail on one given twice |
| `internal/platform/release` | Reads the root `version` pointer from the release host, and checks a release's signed checksums where no cosign runs; `sigstoretest`, a Sigstore of the tests' own |
| `internal/types` | Configuration shape |
| `internal/ui` | The page: a Preact app (JSX under `src/` grouped by area, CSS source at `css/input.css`, tests under `test/`), its shell template, built bundle, workers and worklets, sounds, fonts and emoji |
| `internal/build` | Values baked in at build time, the third-party notices the binary carries (`notices.txt`, generated and committed), and `winres`, which writes the resource objects in `cmd/` |
| `pkg/` | Small reusable packages: locks, rotating logs, HTTP helpers, prompts, migrations, sd_notify |
| `android/` | The Android app's shell: Kotlin with no AndroidX, its manifest and resources, the newer framework calls it makes behind a version check, and the release key's certificate (`release.pem`), which a release's APK must be signed by |
| `scripts/build.sh`, `scripts/build/` | Project values (top block of `build.sh`), local builds, the Go every script runs (`go.sh`), artifact helpers, and the Android app's APKs, built without Gradle, with the check that stands in for Android Lint's NewApi |
| `scripts/ci.sh`, `scripts/ci/` | Release planning, a pull request's test jobs chosen by the files it changes, publication and recovery, and the Flatpak's publication after each release |
| `scripts/vendor.sh` | Pinned versions and SHA-256s for every third-party tool; the only fetcher |
| `scripts/ffmpeg.sh` | Builds the media module from its pinned inputs, checks the committed one, packages FFmpeg's source for releases, and fuzzes the driver |
| `scripts/rnnoise.sh` | Builds RNNoise's module for the page from its pinned inputs, and checks the committed one |
| `scripts/emoji.sh`, `scripts/emoji/` | Writes the list of Unicode's emoji, the page's and `denproto`'s, from pinned inputs, and checks the committed ones; `scripts/emoji/` is the generator |
| `scripts/notices.sh`, `scripts/notices/` | Writes the third-party notices from the module cache and, for the pinned inputs that end up in the binary, the license copies kept in `scripts/notices/` |
| `scripts/install.sh`, `scripts/install.ps1` | The installer bootstraps; templated by `build.sh` |
| `scripts/flatpak.sh`, `scripts/flatpak/` | Makes the Flatpak's commit from a release's binary and commits it to a repository with its deltas and summary; beside them, the public key it's signed with (`dens.asc`) |
| `scripts/test.sh`, `scripts/test-*`, `scripts/test/` | Test entrypoints, lifecycle harnesses, fixture releases, the docs' link check (`scripts/test/links/`), the browser checks (`scripts/test/browser/`), and the Android e2e's container, drivers and den (`scripts/test/android/`), whose `phone.sh` and `bg.sh` also work a real phone |
| `scripts/dev/` | Manual testing's helpers: a manual test's builds staged as a prerelease (`stage.sh`), a Flatpak made from this tree for a VM, and a development den filled with messages or files |
| `spikes/` | When one is under way, a throwaway experiment that answers a design question before code depends on it, in its own Go module; it goes when the work it informs lands, its findings in the design docs, which name the commit that last held it |
| `docs/dev/` | Internal docs: the design (`design.md` and `design/`, a part per area) and each deployment form's, the den protocol (`protocol.md` and `protocol/`), the milestones and loose ideas, a step's loop (`workflow.md`), running the harnesses (`testing.md`), the lifecycle and the release process |
| `docs/` (everything else) | The public dens.chat site (Hugo and Hextra), with its own `README.md` and `DEPLOYMENT.md` |

## Documents of record

- [docs/dev/design.md](docs/dev/design.md) and `docs/dev/design/`: the
  overview first, with goals, the privacy model, processes and listeners,
  pairing, networking, deployment forms, the platform layer and versions; then
  a part per area: keys and sign-in, the page and the phone layout, members,
  messages, sync, notifications, players and cards, files and the media
  module, end-to-end encryption, calls and the SFU, the helper and Relay, and
  the security checklist.
- [docs/dev/installed.md](docs/dev/installed.md): the service under its own
  account on Linux and Windows: install, accounts and sandboxes, updates, paths
  and permissions, maintenance transactions and the installer scripts.
- [docs/dev/flatpak.md](docs/dev/flatpak.md): the Flatpak, run by the
  member in their session: its sandbox, locked start and remembered
  key, lifecycle with no installer, updates, its publishing, and its harness.
- [docs/dev/android.md](docs/dev/android.md): the Android app:
  its design, its build, its form, its shell and the page in its WebView, its
  calls, notifications and updates, and its e2e.
- [docs/dev/milestones.md](docs/dev/milestones.md): what each milestone built,
  and the plan for the one under way.
- [docs/dev/loose-ideas.md](docs/dev/loose-ideas.md): ideas and fixes no
  milestone has taken up yet.
- [docs/dev/workflow.md](docs/dev/workflow.md): a step's loop, from its branch
  to its merge, and a milestone's manual test and release.
- [docs/dev/testing.md](docs/dev/testing.md): running the harnesses: what each
  needs, takes, leaves behind and logs, and cleaning up after one.
- [docs/dev/lifecycle.md](docs/dev/lifecycle.md): the lifecycle state and
  locks every form shares, development instances, and the lifecycle e2e
  harnesses.
- [docs/dev/protocol.md](docs/dev/protocol.md) and `docs/dev/protocol/`: the
  client-to-den protocol, its conventions, errors, keys, objects and rate
  limits first, then a part per area: sign-in, the socket and sync, members,
  messages, files, end-to-end encryption and calls. What crosses the wire is
  stated there, and the design links to it.
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
an old one's name. A backup from any release restores. A migration step a
manual test's staged build ran on machines that matter is as frozen as a
released one. What the page keeps in the browser, and its theme codes, are
released formats too. A change to one of these comes with a test that reads its
released form.

**The release identity is frozen at the first release.** The cosign signing
identity includes the path `.github/workflows/release.yml`; never rename or
move that file. The artifact layout in `docs/dev/release.md` is fixed once
installed copies exist.

**Protocol changes follow the versioning rules.** Adding fields or message
types doesn't bump the client-den protocol version, and receivers ignore
unknown message types. Only breaking changes bump it (see
[Versioning and compatibility](docs/dev/design.md#versioning-and-compatibility)).

**OS seams live in two places.** Runtime seams go in
`internal/platform/host` as function pairs in `_linux.go` and `_windows.go`;
install-time steps go behind the `System` interface in `internal/install`.
Go builds `_linux.go` files for Android too, so where Android differs, an
`_android.go` file takes the seam's place and its Linux one says `!android`.
Beyond the seams, a package that owns a path, a lock or a log file holds its
own OS files (`layout`, `maintenance`, `pkg/xsyscall`, `pkg/xlog/rlog`), and
`pkg/sdnotify` stubs systemd's protocol out on Windows. A platform difference
must never reach the protocol, database or HTTP layers; if a feature seems to
need one, raise it as a design problem. What differs by how Dens is deployed,
its form (installed, a development build's instance, the Flatpak, or the
Android app), is chosen at runtime from `layout.Form`, as a `switch` on the
form where it differs, never by build tags. Like a platform difference, a form
never reaches the protocol or the database, and reaches the page only as what
it can do.

**Windows code compiles only under `GOOS=windows`.** After touching a
`_windows.go` file, run `./scripts/test.sh -lint`, which vets for Windows, and
`GOOS=windows go test -c -o /dev/null ./<pkg>` to catch build breaks. From
WSL, `./scripts/test.sh -windows` runs the Go tests natively on the Windows
host; CI runs them on a Windows runner. Android code likewise compiles only
under `GOOS=android`: `-lint` vets it for arm64 (amd64 needs cgo even to
vet), and `./scripts/test.sh -android-e2e` runs the host package's tests in
the emulator, as the app. A test closes what it opens before it ends: Windows
can't remove an open file, so `t.TempDir`'s cleanup fails there.

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
each file a test reads line by line or byte for byte, the notices the binary
embeds among them: Windows CI checks out the rest with CRLF, which `-windows`,
copying from the Linux checkout, never meets. The PowerShell is ASCII with
CRLF, which `-lint` checks as it parses it, and a few other files are CRLF; do
not "fix" them wholesale.

**Generated files are committed, and only their scripts write them.** Change
a file's inputs, run its script, and commit what it writes beside the change;
CI writes it again and fails on any difference.

- The media module, `internal/media/ffmpeg/module`: from the driver and
  `scripts/ffmpeg.sh`. What the module makes is untrusted like any member's
  file: check it as you would theirs. A test package that runs media jobs gets
  a `TestMain` that runs the worker when `ffmpeg.TestWorkerEnv` is set, and its
  Runner from `ffmpeg.TestRunner`.
- RNNoise's module, `internal/ui/assets/wasm/rnnoise.wasm`: from
  `scripts/rnnoise.sh` and `internal/ui/rnnoise`. The module imports nothing,
  and the voice worklet gives it nothing.
- The list of emoji, `internal/ui/assets/emoji.json` and
  `internal/denproto/emojilist.go`: from `scripts/emoji/`, and the pins and the
  Emoji version in `scripts/emoji.sh`.

The names' fonts are pinned, and never committed: the build copies them from
their packages into `internal/ui/assets/fonts`. A font is its pin in
`scripts/vendor.sh`'s `NAME_FONTS`, its name in `denproto.NameFonts` and in
`names.js`'s `FONTS`, in one order, and its license in `scripts/notices/`,
with the notices written again; tests fail on one that lacks any of them. It's
an Open Font License font with no Reserved Font Name, since the page serves a
subset of it under its own name.

## Build and test

```sh
./scripts/test.sh              # go test -race ./...; run constantly. 4 to 5 min, seconds for what's cached
./scripts/test.sh -lint        # gofmt, go vet for Linux, Windows and Android, the pinned shellcheck over every tracked script, the PowerShell parsed, and the docs' links; run before each commit. 25 s, 3 min the first time
./scripts/test.sh -js          # the page's tests on the pinned Node; run after touching the page's scripts. 30 s
./scripts/test.sh -docs        # the site built as the Docs workflow builds it, failing on any warning, and its icon checked; run after touching the site. 2 s
./scripts/test.sh -release     # release state machine against a local rclone backend. 50 s
./scripts/test.sh -e2e         # lifecycle e2e across the supported distros in Incus containers. 6 to 8 min
./scripts/test.sh -den-e2e     # a member joins a den through Caddy, chats, calls, and is banned, in three Incus containers. 9 to 12 min
./scripts/test.sh -flatpak-e2e # the Flatpak's publisher, then the Flatpak, made from this tree and published: launched, unlocked, updated and rolled back, in Incus containers. 9 to 15 min
./scripts/test.sh -android     # the Android app built, as CI's android-build job: vetted for Android, both APKs, the host tests for the emulator. 30 s with its tools fetched
./scripts/test.sh -android-e2e # the Android app, made from this tree, in Android 17's and 12's emulators: paired, joined to a den behind Caddy, chatting, sending and saving a photo, in a call off screen, and updated from fixture releases. 18 min
./scripts/test.sh -all         # every suite above, one after another. About an hour
scripts/test/android/phone.sh SERIAL COMMAND  # a phone through the Android container's adb: pair, connect, install, log, pull
scripts/test/android/bg.sh SERIAL [SECONDS]   # a phone's call with its screen off, ten minutes unless given, watched and summed up
./scripts/test.sh -windows     # from WSL: the Go tests, run natively on the Windows host. 5 min
scripts/test/browser/setup.sh  # the browser checks' Incus container, with this tree's build and the site's in it
scripts/test/browser/run.sh CHECK [ARGS...]  # one check, with the arguments its header lists, mostly browsers, before a manual test. Up to 15 min for a chain
./scripts/build.sh             # dev binary: runs a development instance as you, -dev storage, debug logs. 10 s, 2 s again
./scripts/build.sh --prod      # production-mode binary for this architecture, after the Go tests. 1 min with them cached
./scripts/build.sh --prod-all  # all release binaries, after the Go tests. 80 s with them cached
./scripts/build.sh --android   # the Android app's development APKs, out/android-{arm64,amd64}.apk. 20 s
./scripts/ffmpeg.sh            # regenerate the media module after changing its driver or build; --check takes 50 s once its libraries are built
./scripts/ffmpeg.sh --fuzz 12000x  # fuzz the driver in the module and natively under AddressSanitizer, for a count of runs, as CI does
./scripts/rnnoise.sh           # rebuild RNNoise's module for the page after changing its build; --check 3 s
./scripts/emoji.sh             # rewrite the list of Unicode's emoji after changing its generator or its pins; --check 1 s
./scripts/notices.sh           # rewrite the third-party notices after a dependency or pin changes; --check 4 s
go test ./internal/build/winres -args -update  # rewrite cmd's Windows resource objects after changing the manifest
```

Durations are on 16 cores with Go's caches warm, the e2e harnesses' as CI
runs them; run one heavy suite at a time. The scripts run go.mod's Go through
`GOTOOLCHAIN` (`scripts/build/go.sh`), whatever Go is on PATH, as CI does; a
`go` typed by hand runs PATH's.

The `-release`, `-e2e`, `-den-e2e`, `-flatpak-e2e` and `-android-e2e` harnesses,
and the browser checks, run on Linux only, the Android e2e on x86_64 with KVM.
The e2e harnesses' logs land under `out/`, the browser checks' stay in their
container; `./scripts/build.sh` empties `out/` in every mode but `--android`,
and with it the media module's build cache. The Windows harnesses (`scripts/test-lifecycle-e2e.ps1`,
`scripts/test-den-e2e.ps1`) install real services, so they run in CI or on a
Windows machine without Dens. [docs/dev/testing.md](docs/dev/testing.md) says
what each harness needs, leaves behind and logs, and how to clean up after one.

Third-party tools and frontend inputs (Tailwind, DaisyUI, esbuild, Preact,
cosign, rclone, shellcheck, Hugo, Node.js for the page's tests,
Caddy for the den e2e, the media module's FFmpeg, zlib, libaom, CMake,
wasi-sdk, binaryen and wasm2go, RNNoise's source and model, Unicode's emoji
test file and CLDR's annotations, the names' fonts, and the Android app's
NDK, SDK pieces, emulator, Kotlin and JDK) are pinned by version and SHA-256 in
`scripts/vendor.sh` and fetched into the gitignored `tools/`.
Never depend on `tools/` contents directly.

Generated and ignored: `internal/ui/assets/{css/output.css,js/output.js,manifest.json,fonts/}`,
`out/`, `tools/`, `docs/out/`. Edit sources under
`internal/ui/css/input.css` and `internal/ui/src/`. A clone
builds and tests before the page is built, serving a placeholder page, which
a binary a script builds refuses.

Search with `git grep -I` or `rg`: both skip the media module's 24 MB of
generated Go (`.gitattributes`, `.ignore`), which a bare `grep -r` reads.

## Working on a step

A step is a branch from main, named for it, and a pull request into main;
[docs/dev/workflow.md](docs/dev/workflow.md) has the loop. Run what the step's
testing section names, `-lint`, the Go race suite (or `go test -race
./internal/ui/...` and `-js` for changes to the page alone), and each matching
browser check; leave `-android-e2e` for changes to `android/`, Android host
seams, WebView/shell behavior, or a milestone's end. Commit by layer, Go, the
page, the shell, the checks and the docs, each commit that changes Go passing
alone; push, and open the pull request with `gh`, its text written for the
reviewer. It merges with a merge commit. Read CI with `gh`: a failure from its
job's log (`gh run view RUN --log-failed`), an e2e's logs from the run's
artifacts (`gh run download RUN -n NAME`). After the merge, sync main and
delete the branch, here and on GitHub.

## Where to add things

- CLI command: constructor in `internal/app/commands`, registered in
  `commands.go`. A test scans the AST and fails if you forget to register it.
- Durable state: a migration step, then accessors beside the owning subsystem.
- HTTP route: a handler on the client router (`internal/platform/http/client`)
  or the den router (`internal/platform/http/den`), never both. Each router's
  `TestNoRouteTwice` fails on a method and path given twice, which chi would
  take silently.
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
- **A line in `docs/dev/`** is for contributors, and describes how Dens
  behaves now, as one sentence a contributor can check against the code. No
  history, no recent refactor, no "we chose", no rejected alternative, no
  account of the session that produced it: those go in the commit and the
  PR. The rationale that matters is one clause, not a paragraph.
- **A public doc** under `docs/content/` is for people using Dens: members
  joining a den and owners hosting one. It says what Dens does, how to use
  it, and the constraints that affect them, especially who can read what. It
  defines a term before relying on it, gives rationale only where that helps
  the reader decide or understand a constraint, and holds no history either.
- **Text in the page** is a label, or one line of what to do. The guide
  holds the rest, linked ([The page's words](docs/dev/design/page.md#the-pages-words)).
- **A changelog line** says what a member notices, in their words, and for a
  fix what went wrong and since when.
- **A commit message** says what changed and why in a few lines; **PR text**
  is for the reviewer: what to look at, what was checked, what's deliberately
  left out.
- **The milestone plan** states options and the pick with one reason each,
  and leaves the rest to the build. It lives in `docs/dev/milestones.md`
  while the milestone is under way; once it's done, what the plan still
  teaches moves into the design docs' sections and the rest goes.

When a draft explains, cut: a reader who wants the why has the docs, the
commit and the review. The test is whether a line would be missed; if not, it
goes.
