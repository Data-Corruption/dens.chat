# Running the harnesses

How to run Dens's harnesses: what each needs, how long it takes, what it leaves behind, how to clean up after it, and where its logs land. What each one checks is its form's doc's: the lifecycle and den harnesses in [lifecycle.md](lifecycle.md), the Flatpak's in [flatpak.md](flatpak.md#testing-the-flatpak), the Android app's in [android.md](android.md#testing-the-app), and what containers change about the service in [installed.md](installed.md#testing-the-linux-service-in-containers). AGENTS.md's Build and test lists every command with its duration.

## What every harness needs

- **Linux.** Everything here runs on Linux but `-windows` and the PowerShell harnesses. Hugo, rclone and Android's tools are pinned for x86_64 alone, so `-docs`, `-release`, `-flatpak-e2e`, `-android`, `-android-e2e` and the browser checks need it; `-e2e` and `-den-e2e` run on arm64 too.
- **`tools/`.** Each tool's first use fetches its pin into `tools/` through `scripts/vendor.sh`: about 10 GB for every harness, Android's SDK pieces 3 GB of it and its emulator 3.8 GB.
- **`out/`.** The harnesses' logs and builds land in `out/`, which `./scripts/build.sh` empties in every mode but `--android`, the media module's build cache with it. The browser checks' `setup.sh` runs it, and so does the Android e2e, through `setup.sh`, unless given `--no-build`. Copy what you need out of `out/` first, and don't build while a harness runs.
- **One at a time.** The heavy suites share Incus, the CPU and fixed ports, and the browser checks share their container's instances.
- **Incus.** `-e2e`, `-den-e2e` and `-flatpak-e2e` reach it through `scripts/test/incus.sh`: as root, as a member of `incus-admin`, or through passwordless `sudo -n incus`, since some distros run privileged. The browser checks, the Android e2e, `phone.sh`, `bg.sh` and `flatpak-vm.sh` run `incus` as you, which `incus-admin` allows. Incus 6.0 LTS runs on WSL's kernels. On a host that runs Docker, Docker's forwarding policy can drop the Incus bridge's traffic: CI adds `ACCEPT` rules for `incusbr0` to the `DOCKER-USER` chain (`.github/actions/setup-incus`), as such a host needs too.
- **Images.** Containers start from the `images:` remote, or from a local alias `dens-e2e/<distro>`, which takes precedence and keeps its image until it's made again. CI takes the remote's newest, so make a rolling distro's alias again before trusting a local pass:

  ```sh
  incus image alias delete dens-e2e/arch
  incus image copy images:archlinux/current local: --alias dens-e2e/arch
  ```

  `flatpak-vm.sh` starts from the alias alone: `incus image copy images:debian/trixie local: --alias dens-e2e/debian-13`.
- **Network.** A harness that stages fixture releases downloads Dens's first release, v1.0.0, from `releases.dens.chat` once, into `tools/dens-v1.0.0`, whatever `RELEASE_URL` names; containers install packages from their distros' mirrors.
- **Processes.** Stop a harness's processes by PID or by name. `pkill -f` or `pgrep -f` with a pattern that's also in the caller's own command line, as an `sh -c` or an `incus exec … sh -c` has it, matches the caller, unless a letter of it is bracketed (`[l]ogcat`). Don't edit a script while it runs: bash reads it as it goes.

## The race suite

`./scripts/test.sh` needs Go and gcc, as the race detector needs cgo. It runs `go test -race ./...`, then the media module's tests again without the race detector, which slows its workers fifty times over: 4 to 5 minutes, seconds for what's cached, and about 8 in CI.

A test that fails now and then in CI, and not here, usually loses a race CI's slower runners lose: run it under `-race` with `GOMAXPROCS` at 1 and 2, or `-test.cpu 2`, beside load. For a call's test, also see which path each caller took (`sfutest.Caller.Path`), since a caller's hang-up can be lost over TCP, and give the SFU's `pionLogs` a sink at every level for the run.

## Lint, the page's tests and the site

- `-lint` fetches the pinned shellcheck; the first vet for each OS adds 13 to 50 seconds. It lints the scripts and PowerShell Git tracks, so a new one counts once it's added, or after `git add -N`. It parses the PowerShell with Windows PowerShell 5.1 where WSL reaches `powershell.exe`, as CI's Windows job does, and otherwise with a `pwsh` on `PATH`, which takes syntax 5.1 refuses; with neither, it says so, and CI's parse is the first.
- `-js` runs on the pinned Node, which `xz` unpacks, with the pinned esbuild and Preact, bundling into `out/js-tests`.
- `-docs` builds with the pinned Hugo into `docs/out`, after `go mod download` in `docs/`, which needs the network.

## The release tests

`-release` needs rclone on `PATH`: `./scripts/vendor.sh rclone` fetches the pinned one, which CI puts there, and `test.sh` doesn't. It also needs gpg and git, renders the installers into `out/release`, and uses no network. 50 seconds.

## The lifecycle and den harnesses

- **`-e2e`** runs each distro in a fresh container, `dens-e2e-<distro>-<pid>`, deleted once it's done, but for a failed one under `KEEP_FAILED=true`. It runs Debian 12 and 13, Ubuntu 24.04 and 26.04, Fedora 43 and 44, and Arch; `--distros` picks some by `scripts/test/incus.sh`'s names, `--backup FILE` has the first restore FILE, and `--release-dir DIR` with `--installer-only` tests a staged release. Logs land in `out/lifecycle-e2e-logs/<run>/`: one per distro, the backup each made, and `summary.txt`; a failing step prints the unit's status, its journal and Dens's own log. 6 to 8 minutes in CI.
- **`-den-e2e`** runs three containers at once, `dens-den-e2e-{den,client,server}-<pid>`, with the pinned Caddy and a voice probe it builds with `go test -c`. `--den` and `--client` pick their distros, and `KEEP_FAILED=true` keeps them after a failure. Some steps are timed, a sign-out and a ban within 5 seconds and a call's length to the second. Logs land in `out/den-e2e-logs/<run>/`. 9 to 12 minutes in CI.
- A run's log directory is named for its start, in UTC, and its PID. A kept container opens with `incus exec NAME -- bash`. After a run that was killed, `incus list dens-` shows what's left, and `incus delete --force NAME` removes each.

### On Windows

The PowerShell harnesses run in an elevated Windows PowerShell 5.1, on a machine without Dens, since they install real services. Their fixture releases come from bash, into a directory that doesn't exist yet, and `scripts/test-den-e2e.ps1` also takes the pinned Caddy's zip:

```sh
bash scripts/test/fixture-releases.sh out/windows-e2e windows-amd64
bash scripts/vendor.sh caddy-windows   # prints caddy-windows=<the zip's path>
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts\test-lifecycle-e2e.ps1 -ReleaseDir out\windows-e2e
powershell -ExecutionPolicy Bypass -File scripts\test-den-e2e.ps1 -ReleaseDir out\windows-e2e -CaddyZip <the zip's path>
```

From WSL, build the fixtures into a Windows path such as `%TEMP%`, and copy the lifecycle harness beside them, since an elevated process may not reach `\\wsl$` paths. The den harness runs from a whole checkout on a Windows path, with Go on `PATH`: it builds the voice probe and reads test media from the tree. Under WSL's mirrored networking, a port a WSL process listened on can stay reserved on the Windows side after it exits, until WSL restarts: `-ClientPort` and `-SecondClientPort` move the harnesses off such ports, and the den harness's `-FreshClientPort`, `-HttpsPort` and `-HttpPort` its others. The den harness trusts Caddy's authority in the machine's certificate store, points `den.test` at loopback in the hosts file, and runs Caddy as the service `dens-e2e-caddy`, undoing all three at the end.

Both print the services' logs on a failure and uninstall. `-KeepOnFailure` leaves the installation for inspection: remove it with `dens uninstall --yes`, with `--instance` for each other instance, from an elevated terminal, and for the den harness `sc.exe delete dens-e2e-caddy`, its line in the hosts file and its certificate.

They run under `Set-StrictMode`, which throws as they run on what parsing passes, and CI is often their first run: try a new helper under strict mode first, taken out of the script with PowerShell's parser, dot-sourced, and fed JSON with its optional fields left out.

## The Flatpak harness

`-flatpak-e2e` refuses to start while `kernel.apparmor_restrict_unprivileged_userns` is 1. It needs Incus as above, Go and python3, and the network for Flathub's runtime, Debian's packages, and vendor.sh's Caddy and rclone. Its first run makes the image `dens-e2e/flatpak`, which stays, and `FRESH_IMAGE=true` makes it again; an image build that fails leaves its container, `dens-flatpak-e2e-image-<pid>`. `KEEP_FAILED=true` keeps the containers after a failure. Logs land in `out/flatpak-e2e-logs/<run>/`: the publisher's log, each launch's output, the Flatpak's service log and the member's session's journal, without each sandbox's scope starting. 9 to 15 minutes in CI.

`STAGE=DIR scripts/dev/flatpak-vm.sh VERSION` publishes from a container, `dens-flatpak-vm`, that stays, and keeps its signing key in `out/flatpak-vm/keys` too, since a VM trusts only the key it installed with. It needs rsync. A build it staged and the real release's `.flatpakref` both name their remote `dens`: on a VM that had a test build, `flatpak remote-delete dens` before installing the release.

## The Android app

- **`-android`** needs x86_64, Go, python3 and unzip, and fetches about 4 GB into `tools/` on its first run. It writes `out/android-{arm64,amd64}.apk` and `out/android-build`, and keeps the development signing key in `~/.config/dens-dev/android.p12`.
- **`-android-e2e`** also needs `/dev/kvm`, python3 and perl on the host, for the pinned exiftool, and Incus as you. It runs in two containers that stay: `dens-browsers`, which the browser checks' `setup.sh` makes, and `dens-android`, which `scripts/test/android/setup.sh` makes, with two emulator devices of 8 GB data partitions; each emulator gets 5 minutes to boot. It builds first unless given `--no-build`. The den, its Caddy and the twin's instance stay running in `dens-browsers`, and the emulators stop at the end. After a killed run, `incus exec dens-android -- /root/box.sh halt 5554` (and `5556`) stops an emulator, `scripts/test/android/den.sh down` the den, and the next boot clears an emulator's lock; `incus delete -f dens-android` starts over. Logs land in `out/android-e2e-logs/<run>/`: each step's log, the checks', the walk's pictures and audits, the logcats, and, for a wait that timed out, the screen as `screen-SERIAL.xml` and `.png`. 18 minutes. Its check that the page paired once can fail after an emulator's cold boot, for a cause not confirmed: run it again with `--no-build`.
- **`phone.sh` and `bg.sh`** work through `dens-android`, so even a real phone needs `/dev/kvm` and the emulator's download, and a development build, for `run-as` and DevTools; `bg.sh` needs a call the phone already joined. They keep what they pull in `out/android/SERIAL`. The app logs at its configured level, warn by default, the development APK's too: raise it in the settings' System, Logging, before reproducing something whose debug lines you need. A measurement of sleep or the battery likely needs wireless debugging and other apps' sound off, which keep a phone waking in short bursts; `dumpsys batterystats --history` gives the charge in mAh over time.

## The browser checks

CI doesn't run the browser checks: they're run by hand, before a manual test. `scripts/test/browser/setup.sh` makes or updates the container `dens-browsers`, which stays (`CONTAINER` renames it, for `run.sh` too). It needs x86_64, Incus as you, the `images:` remote or the alias `dens-e2e/debian-13`, and the network for Debian's packages and vendor.sh's Node, test media and Hugo; it builds the page and the site, running `./scripts/build.sh` unless given `--no-build`. Run it again after changing the page or the site: both are in the container.

`scripts/test/browser/run.sh CHECK [ARGS...]` runs a check, its arguments listed in `run.sh`'s header, printing OK or BAD for each thing it checks and exiting nonzero on any BAD; its development instances keep running after it, until the next check's prep starts them afresh, and the browsers and drivers of one cut short are stopped as the next starts. Screenshots stay in the container, `/home/tester/shot-<check>-*.png` and review's under `/home/tester/shot-review/`, as do the instances' logs, `/home/tester/m2a.log` to `m2e.log`: fetch one with `incus file pull dens-browsers/home/tester/FILE .`. By earlier runs, a check takes a few minutes, `calls` and Firefox's `events` more than ten, a pass of `review` about twelve, and a chain up to fifteen. `incus delete -f dens-browsers` starts over.

## The Go tests on Windows

`-windows` needs WSL with Windows interop. It runs the compiled tests from `%TEMP%\dens-go-tests`, at the same paths each run, gives each package 600 seconds, prints a failed package's last log lines, and removes the directory at the end; one killed leaves it. Windows Firewall can ask once about a test that listens beyond loopback, as the hosting tests do. It leaves out the tests that read the source tree, which CI's Windows runner runs. 5 minutes.

## Everything

`-all` runs the race suite, `-lint`, `-js`, `-docs`, `-release`, `-android`, `-e2e`, `-den-e2e`, `-flatpak-e2e` and `-android-e2e`, one after another, so it needs all of the above. About an hour.

## The generators

`scripts/ffmpeg.sh` needs `make`, `pkg-config`, `xz` and `cmp` on the host, gcc for `--asan` and `--fuzz`, and about 1.5 GB of vendor.sh's wasi-sdk, binaryen, wasm2go, CMake and sources. It builds in `out/ffmpeg`, which an inputs stamp keeps for the next run until `build.sh` empties `out/`, and keeps `configure.log` and `make.log` there, printing a failure's last lines. `--check` takes 50 seconds with its libraries built, and minutes cold.

## CI's logs

A failed job's own lines come from `gh run view RUN --log-failed`: setup-go's problem matcher misses `go test`'s failure lines, so a job's annotations can say nothing. Each e2e job uploads its logs as an artifact kept 14 days, `lifecycle-e2e-logs`, `den-e2e-logs` and `flatpak-e2e-logs`, and `media-fuzz` its failing inputs as `media-fuzz-failures`: `gh run download RUN -n den-e2e-logs`. Each job's time limit, from a minute for the gate to an hour for the release, is its `timeout-minutes` in `release.yml`. The repository variable `VERBOSE` prints each build step's output in the release jobs.
