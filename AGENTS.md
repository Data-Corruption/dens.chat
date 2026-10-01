# Working on Dens

Orientation for coding agents and contributors. Read this before changing
anything, and read the design doc before changing anything it covers.

## What this is

Dens is self-hosted chat for small communities: text, DMs, voice and screen
share. One Go binary runs as a system-level service under its own account. It
always acts as a client, serving the chat UI to the desktop user's browser on
`127.0.0.1` and holding their keys, and it can also host one den (a
community). Linux (systemd) and Windows 11 (SCM) are supported for both roles.
The binary's elevated maintenance commands own install, update, restore and
uninstall; the installer scripts only download, verify and hand off to them.

The target design is [docs/dev/design.md](docs/dev/design.md).

## Where things live

| Path | Owns |
|------|------|
| `cmd/main.go` | Process entry, global flags, signal handling, final error print |
| `internal/app` | `App` composition root, cleanup stack, update checks |
| `internal/app/commands` | CLI commands: desktop-user commands talk to the service; maintenance commands run transactions |
| `internal/service` | The service process: listeners, control handlers, readiness |
| `internal/install` | Install, update, restore and uninstall transactions with a rollback journal; `system_linux.go` and `system_windows.go` hold every install-time OS step |
| `internal/maintenance` | Lifecycle state (`state.json`), locks, start authorization |
| `internal/layout` | Every filesystem path and its permission policy; nothing else resolves paths |
| `internal/instance` | Per-instance config written at install: ports, den role, desktop user, release URL |
| `internal/denproto` | The client-to-den protocol both sides share: wire types, signed layouts, verifiers, invites, name rules, and the cryptography of private DMs and device approval |
| `internal/den` | The den this install hosts: identity key, members, invites, sessions, devices and recovery, sign-ins waiting for approval, the event hub and sockets, uploads, sealed on disk, and the DM key exchanges it relays |
| `internal/denclient` | The dens this install has joined: joining or signing in, keeping each one connected and following it when it moves, uploading, caching files for the page, the DM seal, sealing and opening DMs, and approving new devices |
| `internal/media` | What a file is, taking image metadata out without re-encoding, and previews; the client strips with it and the den checks with it |
| `internal/platform/host` | Runtime OS seams: service host, data key unwrap, control endpoint, locked memory |
| `internal/control` | CLI-to-service protocol over the control endpoint |
| `internal/vault` | Data key envelope: host and password wraps, key check value, signing keys in locked memory |
| `internal/backup`, `internal/pairing` | Backup archives; one-time pairing tokens |
| `internal/platform/database` | SQLite open and pool, ordered migrations, focused accessors per table |
| `internal/platform/http` | Listeners, client and den routers, guards, handlers |
| `internal/platform/release` | Reads the root `version` pointer from the release host |
| `internal/types` | Configuration shape |
| `internal/ui` | The page: a Preact app (JSX under `assets/js/src/`, tests under `test/`), its one shell template, Tailwind/DaisyUI source |
| `internal/build` | Values baked in at build time |
| `pkg/` | Small reusable packages: locks, rotating logs, HTTP helpers, crypto, prompts, sd_notify |
| `scripts/build.sh`, `scripts/build/` | Project values (top block of `build.sh`), local builds, artifact helpers |
| `scripts/ci.sh`, `scripts/ci/` | Release planning, publication and recovery |
| `scripts/vendor.sh` | Pinned versions and SHA-256s for every third-party tool; the only fetcher |
| `scripts/install.sh`, `scripts/install.ps1` | The installer bootstraps; templated by `build.sh` |
| `scripts/test.sh`, `scripts/test-*`, `scripts/test/` | Test entrypoints, lifecycle harnesses, fixture releases |
| `spikes/` | Throwaway experiments that answer a design question before code depends on it, in their own Go module; each goes once its findings are in the design doc |
| `docs/dev/` | Internal docs: the design, the lifecycle, the den protocol and the release process |
| `docs/` (everything else) | The public dens.chat site (Hugo and Hextra) |

## Documents of record

- [docs/dev/design.md](docs/dev/design.md): processes, platform layer, identity
  and encryption, authentication, den features, media, security checklist,
  milestones.
- [docs/dev/lifecycle.md](docs/dev/lifecycle.md): paths, lifecycle state,
  locks, maintenance transactions, development instances, and the lifecycle
  e2e harnesses.
- [docs/dev/protocol.md](docs/dev/protocol.md): the client-to-den protocol:
  authentication, the WebSocket, events, history and limits.
- [docs/dev/release.md](docs/dev/release.md): publication order, resume,
  retention, signing identity.
- `docs/content/`: public docs for people using Dens.

When a reference document and the code disagree, the code is right and the
document is stale; fix the document in the same change. The design doc also
covers what isn't built yet; if your change shows the design is wrong, fix it
in the same change.

## Rules that are not obvious from the code

**Fail closed.** Unknown state, unreadable state, a permission that's too
loose, a symlink where a file should be, an installer pair that won't verify:
reject and say why. Never repair silently; a wrong mode is evidence.

**The localhost page is the most valuable target.** An XSS there reaches every
joined den, the vault and the local API, and any den member can send content,
so all of it is hostile input. No raw HTML, ever: messages go through the
escaping markdown subset, and usernames, filenames and embeds are untrusted
text. Keep the CSP strict with no inline scripts. The client listener checks
`Host` exactly, and `Origin` on every write and WebSocket upgrade.

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
version as the target of a transition.

**Starting the services is the point of no return.** Until then a
transaction's journal undoes every step. Once a service may have migrated,
failure keeps the transitional state and the operator reruns the installer.
Do not add code that pretends a downgrade happened.

**Migrations are ordered functions in
`internal/platform/database/migration.go`.** Database steps and the
`user_version` bump commit together. Non-database side effects in a step must
be idempotent because an interrupted step can run again.

**No compatibility shims before the first release.** Change the initial
migration, layouts and protocols in place. There are no deprecation paths for
pre-release formats.

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
Nothing else gets build tags. A platform difference must never reach the
protocol, database or HTTP layers; if a feature seems to need one, raise it as
a design problem.

**Windows code compiles only under `GOOS=windows`.** After touching a
`_windows.go` file, run `GOOS=windows go vet ./...` and
`GOOS=windows go test -c -o /dev/null ./<pkg>` to catch build breaks. From
WSL, `./scripts/test.sh -windows` runs the Go tests natively on the Windows
host; CI runs them on a Windows runner.

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

**Test files end in `_test.go`, nothing else.** `cmd/hygiene_test.go` fails on
`*_test_*.go` names and on `testing` reaching the shipped binary.

**Line endings.** `.gitattributes` forces LF for `*.go` and `*.sh`. Some
PowerShell and a few other files are CRLF; do not "fix" them wholesale.

## Build and test

```sh
./scripts/test.sh              # go test -race ./... with embed placeholders; run constantly
./scripts/test.sh -lint        # pinned shellcheck over the shell scripts; run after touching them
./scripts/test.sh -js          # the page's tests on the pinned Node; run after touching the page's scripts
./scripts/test.sh -release     # release state machine against a local rclone backend
./scripts/test.sh -e2e         # lifecycle e2e across the supported distros in Incus containers
./scripts/test.sh -den-e2e     # a member joins a den through Caddy, chats, and is banned, in two Incus containers
./scripts/test.sh -windows     # from WSL: the Go tests, run natively on the Windows host
./scripts/build.sh             # dev binary: runs a development instance as you, -dev storage, debug logs
./scripts/build.sh --prod      # production-mode binary for this architecture
./scripts/build.sh --prod-all  # all release binaries
gofmt -l ./cmd ./internal ./pkg && go vet ./... && GOOS=windows go vet ./...
```

The `-release`, `-e2e` and `-den-e2e` harnesses run on Linux only; their logs
land under `out/`. The Windows harnesses (`scripts/test-lifecycle-e2e.ps1`,
`scripts/test-den-e2e.ps1`) install real services, so they run in CI or on a
Windows machine without Dens; see [docs/dev/lifecycle.md](docs/dev/lifecycle.md).

Third-party tools and frontend inputs (Tailwind, DaisyUI, esbuild, Preact,
cosign, rclone, shellcheck, goimports, Hugo, Node.js for the page's tests, and
Caddy for the den e2e) are pinned by version and SHA-256 in `scripts/vendor.sh`
and fetched into the gitignored `tools/`. Never depend on `tools/` contents
directly.

Generated and ignored: `internal/ui/assets/{css/output.css,js/output.js,manifest.json}`,
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
packages. Where the Go and page code must agree, as on mentions, both test
against one shared file of cases in a `testdata` directory. Keep it that way.

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
describes the design, not how it evolved.
