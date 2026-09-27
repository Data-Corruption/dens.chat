# Working on Dens

Orientation for coding agents and contributors. Read this before changing
anything, and read the design doc before changing anything it covers.

## What this is

Dens is self-hosted chat for small communities: text, DMs, voice and screen
share. One Go binary runs as a system-level service under its own account. It
always acts as a client, serving the chat UI to the desktop user's browser on
`127.0.0.1` and holding their keys, and it can also host one den (a
community). Linux (systemd) and Windows 11 (SCM) are supported for both roles.
The installers (`install.sh`, `install.ps1`) own install, update, restore and
uninstall.

The target design is [docs/dev/design.md](docs/dev/design.md).

## Current state: M0 in progress

The tree is a fork of the Sprout application template, and milestone M0 is
turning it into the design. Until M0 lands:

- Everything under "Removed from Sprout" in the design doc is scheduled for
  deletion. Don't extend it or build on it. That covers the per-user layout, the
  Scheduled Task and `service.stop` lease, detached maintenance jobs, instance
  PID markers and draining, dashboard TLS and login, the permission bitmask and
  the hash worker.
- Where code and the design doc disagree, the design doc describes where the
  code is going. If your change shows the design is wrong, fix the design doc in
  the same change.
- `spikes/` holds the throwaway experiments that validated the platform layer;
  see its README. M0 deletes it once `internal/platform/host` exists.

When M0 is done, delete this section and the "Removed from Sprout" list in the
design doc.

## Where things live

| Path | Owns |
|------|------|
| `cmd/main.go` | Process entry, global flags, signal handling, final error print |
| `internal/app` | `App` composition root, cleanup stack, update checks |
| `internal/app/commands` | CLI commands and the service coordinator |
| `internal/maintenance` | Lifecycle state (`state.json`), locks, migration guard |
| `internal/layout` | Every filesystem path and its permission policy; nothing else resolves paths |
| `internal/platform/database` | SQLite open and pool, ordered migrations, focused accessors per table |
| `internal/platform/http` | Listeners, router, middleware, handlers |
| `internal/platform/release` | Reads the root `version` pointer from the release host |
| `internal/platform/secrets` | Dashboard TLS material; removed in M0 |
| `internal/types` | Configuration shape |
| `internal/ui` | Embedded templates, vanilla JS modules, Tailwind/DaisyUI source |
| `internal/build` | Values baked in at build time |
| `pkg/` | Small reusable packages: locks, rotating logs, HTTP helpers, crypto, prompts, sd_notify |
| `scripts/build.sh`, `scripts/build/` | Project values (top block of `build.sh`), local builds, artifact helpers |
| `scripts/ci.sh`, `scripts/ci/` | Release planning, publication and recovery |
| `scripts/vendor.sh` | Pinned versions and SHA-256s for every third-party tool; the only fetcher |
| `scripts/install.sh`, `scripts/install.ps1` | The installers; templated by `build.sh` |
| `scripts/test.sh`, `scripts/test-*` | Test entrypoints and harnesses |
| `docs/dev/` | Internal docs: the design and the release process |
| `docs/` (everything else) | The public dens.chat site (Hugo and Hextra) |
| `spikes/` | Throwaway platform experiments; removed in M0 |

## Documents of record

- [docs/dev/design.md](docs/dev/design.md): processes, platform layer, identity
  and encryption, authentication, den features, media, security checklist,
  milestones.
- [docs/dev/release.md](docs/dev/release.md): publication order, resume,
  retention, signing identity.
- `docs/content/`: public docs for people using Dens.

When a reference document and the code disagree, the code is right and the
document is stale; fix the document in the same change. During M0 the design
doc is the exception described above.

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

**The installer is the only thing that mutates an installation.** The Go
binary never replaces itself, edits the systemd unit or SCM registration, or
runs migrations outside the `--migrate` path the installer authorizes with a
nonce. `dens update` only downloads, verifies and runs the installer.

**Invoking migration is the point of no return.** Before it, the installer can
roll back. After it, failure keeps the transitional state and the operator
reruns the installer. Do not add code that pretends a downgrade happened.

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

**OS-specific runtime code lives in the platform layer.** It goes in
`internal/platform/host` (created in M0) as a function pair in `_linux.go` and
`_windows.go`, and nothing above it gets build tags. A platform difference must
never reach the protocol, database or HTTP layers; if a feature seems to need
one, raise it as a design problem.

**Windows code compiles only under `GOOS=windows`.** After touching a
`_windows.go` file, run `GOOS=windows go vet ./...` and
`GOOS=windows go test -c -o /dev/null ./<pkg>` to catch build breaks; the
tests themselves run in CI on a Windows runner.

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
./scripts/test.sh -release     # release state machine against a local rclone backend
./scripts/test.sh -e2e         # install/update/uninstall across distros in Incus containers
./scripts/build.sh             # dev binary: isolated -dev storage, debug logs, auth bypass, no updates
./scripts/build.sh --prod      # production-mode binary for this architecture
./scripts/build.sh --prod-all  # all release binaries
gofmt -l ./cmd ./internal ./pkg && go vet ./... && GOOS=windows go vet ./...
```

The `-release` and `-e2e` harnesses run on Linux only. E2E keeps per-case logs
under `out/lifecycle-e2e-logs/<run>/`. Windows tests and the PowerShell
installer harness run in CI on a Windows runner.

Third-party tools (Tailwind, esbuild, cosign, rclone, shellcheck, goimports,
Hugo) are pinned by version and SHA-256 in `scripts/vendor.sh` and fetched into
the gitignored `tools/`. Never depend on `tools/` contents directly.

Generated and ignored: `internal/ui/assets/{css/output.css,js/output.js,manifest.json}`,
`out/`, `tools/`, `docs/out/`. Edit sources under
`internal/ui/assets/css/input.css` and `internal/ui/assets/js/src/`.

## Where to add things

- CLI command: constructor in `internal/app/commands`, registered in
  `commands.go`. A test scans the AST and fails if you forget to register it.
- Durable state: a migration step, then accessors beside the owning subsystem.
- HTTP route: a handler package under `internal/platform/http/router`, mounted
  on the client or den router, never both.
- OS-specific behavior: a seam in `internal/platform/host`.
- Project values (name, release URL, contact, ports): the block at the top of
  `scripts/build.sh`.

## Style

Explicit errors wrapped with `%w`, `errors.Join` for cleanup, `sync.Once` for
idempotent close, context cancellation as the cooperative stop everywhere
(doesn't need handling *everywhere*, e.g. database txns. Just try not to block
forever). Comments explain why an ordering or check exists, not what the next
line does. Tests use only the standard `testing` package, open real SQLite in
`t.TempDir()`, and spawn real subprocesses for cross-process claims. Keep it
that way.

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
