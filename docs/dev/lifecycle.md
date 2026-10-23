# Lifecycle

What every form shares of the lifecycle: when the service agrees to start, and the locks that keep each instance to one process; then development instances, and the harnesses that test it all. The design doc explains the reasons; [installed.md](installed.md) has the installed service's paths, permissions and transactions, and [flatpak.md](flatpak.md) the Flatpak's. Read them before changing `internal/layout`, `internal/maintenance`, `internal/install` or the installer scripts.

Terms used below:

- **Instance:** one installed copy of the service, with its own name (`main` unless another is given), account, ports and storage. Every instance on a machine runs the one installed binary.
- **Maintenance command:** `dens install`, `dens update`, `dens restore` or `dens uninstall`, run as root or Administrator.
- **Transaction:** one run of a maintenance command. It takes the locks, publishes a transitional phase, changes the installation, and keeps a journal that can undo the changes until the services start again.

## Lifecycle state

`state.json` records `phase`, `version` (installed), `targetVersion` (during a transition) and `changedAt`. The phases are `ready`, `installing`, `updating`, `restoring` and `uninstalling`; every phase but `ready` is transitional.

When the service starts, `maintenance.AuthorizeStart` compares the state with the binary's version `V`:

| State | The service |
| --- | --- |
| `ready`, version `V` | Starts; the database schema must be current |
| Transitional, target `V` | Applies pending migrations, then starts. In `restoring` it also clears the restored browser sessions. |
| Anything else | Records why and exits with status 78 (a service-specific exit code on Windows), which neither service manager retries |

A session form's service, a development instance's or the Flatpak's, has no installer to publish its transitions: it takes its lifecycle lock first, and only then reads `state.json` and decides, publishing what an installer would (`maintenance.SessionStart`; see Development instances, and [flatpak.md](flatpak.md)). It publishes `ready` itself once it has started.

A service that refuses to start, or stops on an error, writes the reason to its log once the log is open. It also goes to the journal on Linux (`journalctl -u dens@<name>`, from the service's error output) and to the Application event log on Windows, under the source `dens-<name>` that install registers. Those two cover failures from before the log opens.

A service that stops on any other error is started again: by the unit's `Restart=on-failure` on Linux, and on Windows by the SCM's recovery actions, after 5 seconds, 5 more, then 30, counted over a day. The SCM answers only a crash with them, so on such an error the service ends its process without reporting the stop (M8).

## Locks

| Lock | Who | How |
| --- | --- | --- |
| `operation.lock` | Maintenance commands | Exclusive; waits up to 5 minutes for another command |
| `lifecycle.lock` | The service, for its whole run | Exclusive without waiting; a second copy refuses to start. The installed service opens the file read-only, since it can't write the control directory. |
| `lifecycle.lock` | Maintenance commands, after stopping the service | Exclusive; waits up to 30 seconds |
| `lifecycle.lock` | A session form's service, before it reads the lifecycle state | Exclusive. A development instance's second copy refuses at once; the Flatpak's waits up to 30 seconds, for an older build handing over to it (M9.3) |
| `launch.lock`, in the Flatpak's runtime directory | `dens launch`, while it starts the service | Exclusive; waits for another launch, so two at once start one service (M9.3) |

Holding `lifecycle.lock` proves the service is down. A transaction over several instances takes each lock in instance-name order, so two commands can't deadlock.

## Development instances

`./scripts/build.sh` builds a development binary into `out/`. Its `service run` runs a development instance in the foreground, as you. That's the development form (`layout.Development`), a session form, which sets itself up as the maintenance commands set up an installed instance (see the design doc's Deployment forms):

- Storage is under `$XDG_DATA_HOME/dens-dev/<name>` (`~/.local/share/dens-dev` by default), or `%LOCALAPPDATA%\Dens-dev\<name>`. The control endpoint is `$XDG_RUNTIME_DIR/dens-dev/<name>/control.sock`, or `\\.\pipe\dens-dev-<name>-control`.
- The data key is a plaintext file, `control/datakey.dev`. Development data isn't protected; don't put real data in it.
- There are no transactions. Under its lifecycle lock, the instance publishes a transition to the running version, and migrates whichever way the version moved, since its data is disposable; a restore's stays one. It marks itself ready once started.
- `--port` sets the client port when the instance is first created; it's recorded in `instance.json` after that.
- `--den-port` gives the instance's den that port and turns its hosting on, as the settings would. Its owner can give it a loopback address such as `http://127.0.0.1:18485`, so a second development instance can join it without Caddy.
- `--media-udp-port` and `--media-tcp-port` move the den's calls off 7881 and 7882, so two development instances can host dens on one machine. A browser on Windows can't call a den running in WSL: under mirrored networking the two share an address, and WSL keeps what it sends to that address. Try calls with the browser and the instances on one system.
- The den's rate limits are lifted, so `scripts/dev/seed-chat.sh <instance> [count]` can fill a channel with thousands of messages, ending with a reply to the first, for testing the message list by hand.
- It refuses to run as root, and maintenance commands refuse development builds.

The same binary's `open`, `status` and `backup` talk to the development instance, and its `restore` replaces the instance's data as you, stopping its service first (see Restoring in [flatpak.md](flatpak.md)).

## Testing the lifecycle

Both harnesses install unsigned fixture releases built by `scripts/test/fixture-releases.sh`: one version to install and a newer one to update to, with `--skip-verify`. Their binaries carry no signing identity, as a release's do, so `dens update` takes the flag from them, and their versions sort after every real release's. Every backup they make uses the test password `correct horse battery staple`.

Each harness starts from Dens's first release, v1.0.0, as `scripts/vendor.sh` pins it by its signed checksums (`scripts/test/first-release.sh` stages it): it installs it with its den option, creates a den, and updates to the fixture, whose hosting switch must take the den role and serve the den as it was (M9.1). Then it uninstalls, and the flows below start from a clean machine. Installed copies hold what v1.0.0 wrote, so every release keeps this check, and the release host keeps v1.0.0 past its retention rule.

### Linux

`./scripts/test.sh -e2e` runs `scripts/test-lifecycle-e2e.sh`, which runs `scripts/test/lifecycle-guest.sh` in a fresh Incus system container for each supported distro. On each it installs through `install.sh` as a sudo user and checks permissions, the sandbox, the unit's `systemd-analyze security` rating, and the shortcut helper's autostart entry, `.desktop` file and icon, which the last uninstall removes. Then it pairs a browser, sets the password, checks the install hosts nothing until hosting is on, turns hosting on and off, backs up, checks another local user is refused, reboots, updates, checks that neither `dens update` nor the installer puts the older release over the newer one, adds a second instance and turns its hosting on, restores, which leaves hosting off, and uninstalls. Before all that, it checks the installer refuses v1.0's den option.

- `--distros "debian arch"` picks distros; `--backup FILE` has the first distro restore FILE; `KEEP_FAILED=true` keeps failed containers.
- Each distro after the first restores the backup the previous one made. Logs and backups land in `out/lifecycle-e2e-logs/<run>/`.
- A local image alias `dens-e2e/<distro>` takes precedence over the `images:` remote.
- Debian 12 and Ubuntu 24.04 run privileged, since their systemd can't create its credential host secret in an unprivileged container. Privileged containers mount `binfmt_misc` read-only so systemd's shutdown can't clear the host's table, which on WSL holds Windows interop.

`--release-dir DIR` tests a staged release snapshot instead, without the update step, though still updating from the first release to it; the release pipeline uses it to test changed installers. The flows check this tree's behavior, which a release built from another commit may not have, as v1.0.0 has no hosting switch, so for such a release `--installer-only` installs it through `install.sh`, verifying its signature with cosign, checks that the service answers as that release, and uninstalls. The release pipeline passes it for every release it didn't build in that run.

### Windows

`scripts/test-lifecycle-e2e.ps1` runs the same flows against the real SCM service, with a service restart standing in for the reboot. It also checks the service's virtual account, restricted SID and single privilege, the directory DACLs, the `PATH` entry, the error-reporting exclusion, each instance's media firewall rules, and the shortcut helper's scheduled task, which the last uninstall removes. It refuses to run where Dens is installed and uninstalls on failure. CI runs it with the exact installer candidate (`-InstallerCandidate`).

To run it on a spare Windows machine:

```sh
bash scripts/test/fixture-releases.sh out/windows-e2e windows-amd64
```

Then, from an elevated PowerShell in the checkout:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\test-lifecycle-e2e.ps1 -ReleaseDir out\windows-e2e
```

From WSL, build the fixtures into a Windows path such as `%TEMP%`, and copy the harness beside them, since an elevated process may not reach `\\wsl$` paths. With WSL's mirrored networking, a port that a WSL process listened on (a development instance, say) can stay reserved on the Windows side after the process exits, until WSL restarts. `-ClientPort` and `-SecondClientPort` move the harness off such ports.

### Across platforms

1. Run the Linux harness; each distro leaves `<distro>.backup` in its log directory.
2. Run the Windows harness with `-Backup` set to one of them and `-SaveBackup windows.backup`.
3. Run the Linux harness with `--backup windows.backup`.

## Testing dens

The den e2e harnesses check that a member on another machine can join a den through Caddy, chat with the owner, and stay connected across a den restart without losing a message. The den's machine installs with no option and turns hosting on through its page's API, with the local password, as its owner would (M9.1). The two then call each other in a voice channel: a voice probe, a test binary built from `internal/platform/http/client` with Pion in the browser's place, joins the call through each install's page socket as the page does, and each must hear the other, over UDP and with the member's side over TCP alone. Then their phone photo reaches the owner stripped, their phone video stripped, with its preview, and readable from any byte as a player seeks, and their iPhone HEIC as a JPEG. The owner opens a DM, which takes no message until both have typed each other's check digits, and then carries text, a photo and a video that the den stores only sealed. The two tick different boxes on a checklist the owner shared at the same moment, every tick of which must stay. The member recovers their account on a fresh install with a recovery code, whose new password signs the first install out at once, and reads the DM there once they've typed their DM seal. The new password alone doesn't sign the first install in again: the fresh install approves it, the two comparing digits, and hands it the seal, with which it reads the DM. Signed out once more from the fresh install's device list, it signs in again the same way, with the digits typed into the fresh install first, which must go on showing its own. Then a ban closes the member's connections at once and keeps their username out. Last, the den's machine makes an owner invite with `dens den owner-invite`, and whoever redeems it on the member's machine, under a new name, owns the den, with the owner before them a moderator (M8). Each milestone extends them. They share the fixture releases, and on Linux the container helpers in `scripts/test/incus.sh`, with the lifecycle harnesses.

- **Linux:** `./scripts/test.sh -den-e2e` runs `scripts/test-den-e2e.sh`. A Debian 13 container runs the den behind the pinned Caddy, serving `https://den.test` with Caddy's internal certificate authority. A Fedora 44 container trusts that authority, installs Dens, and joins with an invite from the owner; a second instance on it, `fresh`, stands in for the member's new machine. `--den` and `--client` pick other distros. Logs land in `out/den-e2e-logs/<run>/`.
- **Windows:** `scripts/test-den-e2e.ps1` runs both ends on one machine. Instance `main` hosts the den behind Caddy running as a Windows service, instance `second` joins it through `https://den.test:18443`, and instance `fresh` stands in for the member's new machine. It trusts Caddy's authority in the machine certificate store and points `den.test` at loopback in the hosts file, and undoes both at the end. It builds the voice probe with the Go on `PATH`, and its call runs Pion under the den's restricted service SID. Like the lifecycle harness, it installs real services, so it runs in CI or on a Windows machine without Dens, elevated:

```sh
bash scripts/test/fixture-releases.sh out/windows-e2e windows-amd64
bash scripts/vendor.sh caddy-windows
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts\test-den-e2e.ps1 -ReleaseDir out\windows-e2e -CaddyZip tools\caddy_2.11.4_windows_amd64.zip
```
