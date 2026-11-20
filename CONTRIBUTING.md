# Contributing

Thanks for looking. This file is short on purpose: [AGENTS.md](AGENTS.md) is
the real orientation for anyone changing code, human or otherwise, and
[docs/dev/design.md](docs/dev/design.md) is the design. Read those first;
[docs/dev/milestones.md](docs/dev/milestones.md) holds the plan for the
milestone under way, when there is one.

## Before you start

Open an issue before a large change. Dens keeps a deliberately small scope, so
these are out, and you shouldn't spend a weekend on them:

- protection from targeted investigations;
- federation between dens, or multi-tenancy;
- macOS support;
- custom permission systems, forums, threads or bots;
- new third-party modules that don't solve a non-trivial problem cleanly.

Bug reports need the OS and distro, architecture, the output of
`dens --build-vars`, and the exact command and error. Installer problems should
include `control/maintenance.log` from the storage directory the installer
names in its output.

## Development

Linux or WSL on `amd64`, Go 1.21 or later (the scripts switch it to
`go.mod`'s), Bash, `curl`, `gcc` (only because `go test -race` needs it) and
`xz` (for the pinned Node.js the page's tests run on). On `arm64`,
`vendor.sh` pins no rclone, Hugo, Android NDK or emulator. The e2e harnesses and the
browser checks need Incus, and the Android e2e KVM.

```sh
./scripts/test.sh            # go test -race ./...; run liberally
./scripts/test.sh -lint      # gofmt, go vet for each OS, and shellcheck; before each commit
./scripts/test.sh -js        # the page's tests; after touching its scripts
```

[AGENTS.md](AGENTS.md)'s Build and test lists the rest, with how long each
takes.

If you touched the installers or the release scripts, also run
`./scripts/test.sh -release` (a local fake remote, which needs rclone on PATH
and gpg) and, if you can, `./scripts/test.sh -e2e`. If you touched a
`_windows.go` file, `GOOS=windows go test -c -o /dev/null ./<pkg>` catches
build breaks locally; from WSL, `./scripts/test.sh -windows` runs the tests on
the Windows host, and CI runs them on Windows.

## Pull requests

- One concern per PR. Small is good.
- Explain why in the description, not what; the diff shows what.
- Docs live next to the code they describe. If your change makes
  `docs/dev/**`, `docs/content/**` or `AGENTS.md` wrong, fix them in the same
  PR.
- Tests use only the standard `testing` package, open real SQLite in
  `t.TempDir()`, and spawn real subprocesses for cross-process claims. Test
  files end in `_test.go`; a hygiene test fails on anything else.
- Fail closed. Unknown state, a permission that is too loose, a symlink where a
  file should be: reject with a clear error. Never repair silently.
- `.gitattributes` forces LF for `*.go` and `*.sh`. A few other files are
  deliberately CRLF; leave them.
- CI is gated on the `CI_ENABLED` repository variable. If your PR's checks show
  only the gate job, that is the variable, not your change.

## Security

Do not open a public issue for a vulnerability. Use GitHub's private
vulnerability reporting on this repository (Security tab, "Report a
vulnerability"). Include the version, platform, and a reproduction if you have
one. You'll get a reply; fixes ship as a normal forward release.

## License

MIT, see `LICENSE.md`. By contributing you agree your contribution is licensed
the same way.
