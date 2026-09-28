# Lifecycle

How an installation is laid out, when the service agrees to start, and exactly what install, update, restore and uninstall do. The design doc explains the reasons; this is the reference for changing `internal/layout`, `internal/maintenance`, `internal/install` or the installer scripts. Read it before touching them.

Terms used below:

- **Instance:** one installed copy of the service, with its own name (`main` unless another is given), account, ports and storage. Every instance on a machine runs the one installed binary.
- **Maintenance command:** `dens install`, `dens update`, `dens restore` or `dens uninstall`, run as root or Administrator.
- **Transaction:** one run of a maintenance command. It takes the locks, publishes a transitional phase, changes the installation, and keeps a journal that can undo the changes until the services start again.

## Paths

Only `internal/layout` computes these.

| | Linux | Windows |
| --- | --- | --- |
| Instance root | `/var/lib/dens/<name>` | `%ProgramData%\Dens\<name>` |
| Control directory | `<root>/control` | `<root>\control` |
| Data directory | `<root>/data` (`db/`, `logs/`, `uploads/`, `tmp/`) | `<root>\data` (same) |
| Host-wrapped data key | `control/datakey.cred` (systemd-creds) | `control\datakey.dpapi` (machine-scope DPAPI) |
| Control endpoint | `/run/dens/<name>/control.sock` | `\\.\pipe\dens-<name>-control` |
| Binary | `/usr/local/bin/dens` | `%ProgramFiles%\Dens\dens.exe`, on the system `PATH` |
| cosign, kept for `dens update` | `/usr/local/lib/dens/cosign` | `%ProgramFiles%\Dens\cosign.exe` |
| Service | `dens@<name>.service`, from the template `/etc/systemd/system/dens@.service` | `dens-<name>` |
| Account | `dens-<name>`, from `/etc/sysusers.d/dens-<name>.conf` | `NT SERVICE\dens-<name>` |

The control directory holds everything only maintenance commands may change:

| File | Contents |
| --- | --- |
| `state.json` | Lifecycle state (see below) |
| `operation.lock`, `lifecycle.lock` | Lock files (see below) |
| `instance.json` | Instance config: name, ports, den role, desktop user, release URL |
| `maintenance.log` | Every maintenance command's output for this instance |
| `datakey.cred` or `datakey.dpapi` | The data key, wrapped for this host |

## Ownership and permissions

`layout.CheckControl` and `layout.CheckData` refuse anything looser than this, and a symlink anywhere a file or directory should be.

On Linux:

- `/var/lib/dens` and the instance root are `root:root 0755`.
- The control directory is `root:dens-<name> 0750` and its files `root:dens-<name> 0640`, so the service reads them and can't change them. The data key is `root:root 0600`: systemd reads it as root before starting the service, and hands the decrypted key over in `$CREDENTIALS_DIRECTORY`.
- The data directory is `dens-<name>:dens-<name> 0700`. The unit's `StateDirectory=dens/%i/data` creates it and keeps it owned by the service account.
- `RuntimeDirectory=dens/%i` (0755) holds the socket, which is 0666. Any local user can connect; the service authorizes each connection by its `SO_PEERCRED` UID.

On Windows:

- `%ProgramData%\Dens` has a protected DACL: SYSTEM and Administrators get full control, and Users may list it (`0x1200a9`, not inherited), as with `/var/lib/dens` on Linux. The service needs that: Windows path normalization (Go's `filepath.EvalSymlinks`, which the SQLite driver uses) lists every parent directory of the database.
- The instance root has a protected DACL: SYSTEM and Administrators get full control, and the service SID gets read (`0x1200a9`). The control directory and its files inherit it.
- The data directory has a protected DACL: SYSTEM and Administrators get full control, and the service SID gets modify (`0x1301bf`).
- The control pipe's DACL grants SYSTEM and the service SID full access, and the desktop user `0x12019b` (read and write data, never `GENERIC_WRITE`). Other accounts can't open it.
- `%ProgramFiles%\Dens` inherits Program Files' permissions: everyone can run the binary, and only administrators can change it.

## Lifecycle state

`state.json` records `phase`, `version` (installed), `targetVersion` (during a transition) and `changedAt`. The phases are `ready`, `installing`, `updating`, `restoring` and `uninstalling`; every phase but `ready` is transitional.

When the service starts, `maintenance.AuthorizeStart` compares the state with the binary's version `V`:

| State | The service |
| --- | --- |
| `ready`, version `V` | Starts; the database schema must be current |
| Transitional, target `V` | Applies pending migrations, then starts. In `restoring` it also clears the restored browser sessions. |
| Anything else | Records why and exits with status 78 (a service-specific exit code on Windows), which neither service manager retries |

A service that refuses to start, or stops on an error, writes the reason to its log once the log is open. It also goes to the journal on Linux (`journalctl -u dens@<name>`, from the service's error output) and to the Application event log on Windows, under the source `dens-<name>` that install registers. Those two cover failures from before the log opens.

## Locks

| Lock | Who | How |
| --- | --- | --- |
| `operation.lock` | Maintenance commands | Exclusive; waits up to 5 minutes for another command |
| `lifecycle.lock` | The service, for its whole run | Exclusive without waiting; a second copy refuses to start. In production the service opens the file read-only, since it can't write the control directory. |
| `lifecycle.lock` | Maintenance commands, after stopping the service | Exclusive; waits up to 30 seconds |

Holding `lifecycle.lock` proves the service is down. A transaction over several instances takes each lock in instance-name order, so two commands can't deadlock.

## Transactions

### Install and update

The installer scripts run `dens install` with the release they downloaded. It installs a new instance, or updates or reconfigures an existing one, and moves every other installed instance to its version too:

1. Print the plan. `--dry-run` stops here.
2. Collect the members: the target instance plus every installed one, sorted by name. Create missing instance roots and take each member's `operation.lock`.
3. Read each member's state. Journal a restart of the services that were running, as the last step to undo, then publish `installing` (a fresh instance, or one whose first install never finished) or `updating` from the recorded version.
4. Stop the services and take each `lifecycle.lock`.
5. Create the account, install the binary and cosign, register each member's service and set its permissions.
6. Write the target's instance config and generate and host-wrap a data key for a fresh instance. On Windows, also add the den's firewall rules, the `PATH` entry and the error-reporting exclusion.
7. Release the lifecycle locks and commit the journal. This is the point of no return.
8. Start each service; it migrates as it starts (see Lifecycle state). Once it is running, publish `ready`. Stop again any existing instance that wasn't running before; it only ran to migrate.

A failure before step 7 undoes the journal in reverse. A failure after it leaves the transitional state; running the installer again starts a new transition to its version from the recorded one.

`dens update` reads the release URL from the instance config (or `--release-url`), downloads `install.sh` or `install.ps1` and its `.cosign.bundle`, and verifies them with the kept cosign against the signing identity built into the binary. It then runs the installer in the foreground with `--update --instance <name>`, and the installer takes it from there.

### Restore

`dens restore BACKUP` replaces one instance's data:

1. Refuse unless the instance is `ready` (or mid-`restoring`) at this binary's version.
2. Extract the backup into a staging directory beside the data directory. Refuse a backup of another application, one from a newer schema, a wrong password, and a data key that doesn't match the backup's key check value.
3. Take `operation.lock`, publish `restoring`, stop the service and take `lifecycle.lock`.
4. Move the data directory aside (`data.replaced-<time>`) and the staging directory into its place. Move the host-wrapped key aside, and wrap the backup's data key for this host.
5. Commit the journal, move this machine's logs into the new data directory, start the service and publish `ready`. The service migrates an older backup and clears its browser sessions as it starts. Then delete the replaced data and key.

### Uninstall

`dens uninstall` prints its plan (`--dry-run` stops there), takes `operation.lock`, publishes `uninstalling`, stops the service and takes `lifecycle.lock`. It then unregisters the service, its event log source on Windows and its firewall rules, and deletes the instance root and the account. The last instance also removes the binary, cosign and the unit template on Linux, or the `PATH` entry and error-reporting exclusion on Windows. Windows can't delete a running executable, so when `dens.exe` uninstalls itself it's renamed to `dens.exe.old-<time>` and deleted at the next reboot.

Uninstall has no undo. Running it again finishes an interrupted one.

## Installer scripts

`install.sh` and `install.ps1` refuse to run without root or Administrator. Then they:

1. Read the release host's root `version` pointer once, and fetch that version's binary, `version` file and `checksums.txt`.
2. Verify `checksums.txt` against its cosign bundle with a pinned cosign release, whose own SHA-256 is baked into the script.
3. Check the binary and `version` against `checksums.txt`, and check the signed version is the one the pointer named.
4. Run the binary's `install` with `--release-url` set to where it came from, `--cosign` set to the verified cosign, and the remaining arguments.

`--uninstall` (`-Uninstall`) runs the installed binary's `uninstall` instead. `--update` is accepted for `dens update` and changes nothing.

Two environment variables, which `dens update` also honors:

- `APP_RELEASE_URL` installs from another release host, such as a byte-for-byte mirror; the signatures still verify. It must be `https://`, or `file://` for local test releases.
- `APP_SKIP_VERIFY=true` skips the signature checks, for unsigned test releases only. The checksums still apply.

## Development instances

`./scripts/build.sh` builds a development binary into `out/`. Its `service run` runs a development instance in the foreground, as you:

- Storage is under `$XDG_DATA_HOME/dens-dev/<name>` (`~/.local/share/dens-dev` by default), or `%LOCALAPPDATA%\Dens-dev\<name>`. The control endpoint is `$XDG_RUNTIME_DIR/dens-dev/<name>/control.sock`, or `\\.\pipe\dens-dev-<name>-control`.
- The data key is a plaintext file, `control/datakey.dev`. Development data isn't protected; don't put real data in it.
- There are no transactions. The instance marks itself ready for the running version, and migrates whenever the version changes.
- `--port` sets the client port when the instance is first created; it's recorded in `instance.json` after that.
- `--den-port` makes the instance host a den on that port. Its owner can give it a loopback address such as `http://127.0.0.1:18485`, so a second development instance can join it without Caddy.
- It refuses to run as root, and maintenance commands refuse development builds.

The same binary's `open`, `status` and `backup` talk to the development instance.

## Testing the lifecycle

Both harnesses install unsigned fixture releases built by `scripts/test/fixture-releases.sh`: one version to install and a newer one to update to. Every backup they make uses the test password `correct horse battery staple`.

### Linux

`./scripts/test.sh -e2e` runs `scripts/test-lifecycle-e2e.sh`, which runs `scripts/test/lifecycle-guest.sh` in a fresh Incus system container for each supported distro. On each it installs through `install.sh` as a sudo user and checks permissions, the sandbox and the unit's `systemd-analyze security` rating. Then it pairs a browser, sets the password, backs up, checks another local user is refused, reboots, updates, adds a second instance that hosts a den, restores, and uninstalls.

- `--distros "debian arch"` picks distros; `--backup FILE` has the first distro restore FILE; `KEEP_FAILED=true` keeps failed containers.
- Each distro after the first restores the backup the previous one made. Logs and backups land in `out/lifecycle-e2e-logs/<run>/`.
- A local image alias `dens-e2e/<distro>` takes precedence over the `images:` remote.
- Debian 12 and Ubuntu 24.04 run privileged, since their systemd can't create its credential host secret in an unprivileged container. Privileged containers mount `binfmt_misc` read-only so systemd's shutdown can't clear the host's table, which on WSL holds Windows interop.

`--release-dir DIR` tests a staged release snapshot instead, without the update step; the release pipeline uses it to test changed installers.

### Windows

`scripts/test-lifecycle-e2e.ps1` runs the same flows against the real SCM service, with a service restart standing in for the reboot. It also checks the service's virtual account, restricted SID and single privilege, the directory DACLs, the `PATH` entry, the error-reporting exclusion and the den's firewall rules. It refuses to run where Dens is installed and uninstalls on failure. CI runs it with the exact installer candidate (`-InstallerCandidate`).

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

The den e2e harnesses check that a member on another machine can join a den through Caddy and stays connected across a den restart. Each M1 step extends them. They share the fixture releases, and on Linux the container helpers in `scripts/test/incus.sh`, with the lifecycle harnesses.

- **Linux:** `./scripts/test.sh -den-e2e` runs `scripts/test-den-e2e.sh`. A Debian 13 container runs the den behind the pinned Caddy, serving `https://den.test` with Caddy's internal certificate authority. A Fedora 44 container trusts that authority, installs Dens, and joins with an invite from the owner. `--den` and `--client` pick other distros. Logs land in `out/den-e2e-logs/<run>/`.
- **Windows:** `scripts/test-den-e2e.ps1` runs both ends on one machine. Instance `main` hosts the den behind Caddy running as a Windows service, and instance `second` joins it through `https://den.test:18443`. It trusts Caddy's authority in the machine certificate store and points `den.test` at loopback in the hosts file, and undoes both at the end. Like the lifecycle harness, it installs real services, so it runs in CI or on a Windows machine without Dens, elevated:

```sh
bash scripts/test/fixture-releases.sh out/windows-e2e windows-amd64
bash scripts/vendor.sh caddy-windows
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts\test-den-e2e.ps1 -ReleaseDir out\windows-e2e -CaddyZip tools\caddy_2.11.4_windows_amd64.zip
```
