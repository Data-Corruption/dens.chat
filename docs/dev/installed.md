# The installed service

How Dens runs installed, as a service under its own account on Linux and Windows: its install and account, its sandbox on each system, its paths and permissions, and exactly what install, update, restore and uninstall do. [lifecycle.md](lifecycle.md) has the terms, the lifecycle state and the locks every form shares.

## Install and service account

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

- A system-level service, not `systemd --user` or a per-user Scheduled Task, keeps Dens running without a login, gives it its own account to sandbox, and gives both platforms a real graceful stop. On Linux it also enables `LoadCredentialEncrypted=` (see Local identity in [design.md](design.md)).
- Several installs per machine are separate instances (`dens@<name>` or `dens-<name>`), each with its own account, ports and storage. This is the advanced path for a second den.
- Each instance's ports are fixed at install and recorded in its config. `main` uses the defaults (client 8484, den listener 8485, media 7881/UDP and 7882/TCP); other instances pass theirs to the installer, which refuses a client port already in use, and two of an instance's ports that are the same.
- Every installed instance can host a den (M9). Hosting is a switch in the page's settings, off until the desktop user turns it on with the local password, and turned off after a confirmation that members can't reach the den until it's back on. Creating the den itself (its identity key and owner account) then happens in the app.
  - The switch is the service's own preference, `types.Configuration.Hosting`. The first start of a version that has it takes it from the den role v1.0's installer recorded in the instance config (`den.enabled`), so an instance that hosted keeps hosting, and from then on the switch alone decides; nothing else reads the role.
  - Every instance opens its den as it starts, which before the den exists is one row's read. While the switch is on, the service binds the den listener and the media ports, starts the den's calls, its deleting of messages past retention and its forgetting of old changes, and serves it, under a context of its own. Turning it off cancels that and stops the den as a service stop does: its listener first, then its sockets, then its calls, keeping everything it holds, so turning it on again serves the den as it was.
  - A port another program holds, or one the system keeps for itself, fails turning hosting on, changing nothing, and the page names the port and the installer flag that moves it. With hosting on as the service starts, a port it can't bind leaves the rest of the service up, so the page can say why, and the den tries again every 30 seconds.
- Sprout's signed releases, SQLite layer and `state.json` lifecycle phases carry over (see Service lifecycle).

## Privileged steps and updates

Root or Administrator is used only for install, update, uninstall and restore; the service itself always runs unprivileged and sandboxed.

- The installer scripts (`install.sh`, `install.ps1`) only download a release, verify its cosign signature and checksums, and run the release binary's `install` command. Every privileged change is made by the binary's maintenance commands (`install`, `update`, `restore`, `uninstall`), so both platforms share one transaction engine (`internal/install`) and the scripts stay short enough to read before running them.
- Privileged steps: create the service account, install the binary, register the service, create the state directory with private permissions, record the desktop user allowed to pair, and generate and host-wrap the data key. On Windows they also add the binary to the system `PATH`, exclude it from Windows Error Reporting, and add the media ports' firewall rules, which let nothing in while hosting is off (M9).
- Every maintenance command prints its plan before changing anything, and `--dry-run` (`-DryRun` on Windows) prints the plan and stops. Output goes to the terminal and to the instance's `control/maintenance.log`.
- The install guide has people download the installer (`install.sh` or `install.ps1`) and run it, never `curl | sudo bash` or `irm | iex`, with checking its cosign signature first as an optional step, folded away, commands included. The installer checks everything it downloads; only a check before running it covers the script itself.
- Updates are manual: `sudo dens update` on Linux, or `dens update` from an elevated terminal on Windows (`sudo dens update` also works where Windows' built-in sudo is enabled). It downloads the latest installer and its cosign bundle, verifies them against the signing identity baked into the binary, and runs the installer in the foreground. There is no unattended auto-update.
- An installation never moves to an older release than the one installed (M8). A signature proves who built a release, not that it's the newest, so a release host or mirror that served an older signed release could otherwise roll installs back to holes since fixed. Installing the installed version again is allowed; it repairs.
- Only flags on the command line, never the environment, change where the installers and `dens update` fetch a release or whether they verify it (M8): `--release-url` and `--skip-verify` (`-ReleaseUrl` and `-SkipVerify` on Windows). An elevated Windows shell inherits the user's own environment variables, which anything running as them can set. A binary the release pipeline built refuses `--skip-verify`; the test harnesses install unsigned fixture releases with binaries built without the release identity.
- The service checks for new releases and shows an in-app notice with the update command; it never installs anything itself. Update checks can be turned off in settings, and the privacy docs state that a check only asks the release host for the latest version number, which reveals the install's IP to that host.

## Service lifecycle

Only the service process opens the storage root. The root is private to the service account, so every command a desktop user runs (`dens open`, `dens backup`, admin commands) goes through the control endpoint, and every root or Administrator command is a maintenance transaction. With one process per instance and a service manager that stops it gracefully on both platforms, install, update, restore and uninstall follow the same steps on Linux and Windows:

1. The command takes `operation.lock` for each instance it touches, in name order, and publishes a transitional phase (`installing`, `updating`, `restoring` or `uninstalling`) and the target version in `state.json`. Every instance on a machine runs the one installed binary, so install and update move all of them through the transition.
2. It stops each service through the service manager and waits. systemd escalates to `SIGKILL` after `TimeoutStopSec`; on Windows the command runs `taskkill /F` on the process ID the SCM reports after the same timeout. (An elevated administrator can't open a virtual-account service's process for termination without the debug privilege; `taskkill /F` and `Stop-Process -Force` enable it themselves.)
3. It takes `lifecycle.lock` exclusively. The service holds that lock exclusively for its whole run, so acquiring it proves the service is down, and the same lock stops a second copy of the service from starting.
4. It changes the installation, journaling how to undo each step. A failure up to here undoes the journal and restores the previous state, including restarting services that were running.
5. It starts each service. A service whose state is transitional and names its version migrates its data before reporting ready; the command then publishes `ready`. An instance that was stopped before the transaction runs only long enough to migrate.

A service starts only when the phase is `ready` with its version, or a transition that targets its version. Otherwise it logs why and exits with status 78, which the unit's `RestartPreventExitStatus=` keeps systemd from retrying. On Windows it stops with a service-specific exit code, which the SCM treats as a stop rather than a crash, so recovery actions don't restart it.

Starting the services is the point of no return: a migrated database can't be opened by the previous binary, so a failure from here keeps the transitional state. The operator fixes the cause and runs the installer again, and the service finishes the migration when it starts.

Uninstall removes the instance's state directory, service registration, account and firewall rules. The binary, cosign and the unit template on Linux, or the `PATH` entry and error-reporting exclusion on Windows, go with the last instance. Windows can't delete a running executable, so an uninstall run from the installed `dens.exe` renames it and schedules the file for deletion at the next reboot.

## Linux

Dens supports five systemd desktop distro families. Non-systemd, server and developer-focused distros are cut. The hard floor is systemd 250 for `LoadCredentialEncrypted=`.

| Distro | Minimum | Notes |
| --- | --- | --- |
| Fedora Workstation and spins | Current releases | SELinux enforcing: test file labels for `/var/lib/dens` and the binary |
| Bazzite (Fedora Atomic) | Current releases | Read-only `/usr`: binary in `/usr/local/bin`, user via `/etc/sysusers.d`, units in `/etc/systemd/system`; Silverblue, Kinoite, Bluefin and Aurora considered on the same base |
| Debian | 12 | Ships systemd 252 |
| Ubuntu, Mint, Pop!\_OS | 24.04 base | 22.04 ships systemd 249, below the floor |
| Arch, CachyOS | Rolling | Manjaro and other derivatives considered, not tested |

Cut: Alpine, Void, Artix, Devuan, Gentoo, NixOS, openSUSE and MicroOS, Rocky, AlmaLinux and RHEL, SteamOS, and WSL2. From M9, a Flatpak lets their users join dens (see [flatpak.md](flatpak.md)). The installer refuses NixOS and SteamOS before it changes anything, saying why (M8). Both keep paths it writes read-only (NixOS's `/etc/systemd/system`, SteamOS's root), so it would otherwise fail halfway.

### TPM

`systemd-creds` binds to the TPM plus the host key when a TPM exists, and to the host key alone otherwise, so no separate fallback is needed. Host-key-only mode still protects copies of the data directory, but not a full disk image or root.

### Install layout rules (all distros)

These keep immutable and atomic-update distros cheap to support, and are good practice everywhere:

- Install paths come from one layout module, never hardcoded, so a distro can override where the binary lives.
- Keep the `/etc` footprint minimal: the unit file, a `sysusers.d` file, nothing else.
- Define the service user in `/etc/sysusers.d` (creating the directory where the distro only ships `/usr/lib/sysusers.d`, as Debian 13 does) so it's recreated at boot. Don't fix its UID: sysusers silently picks another one when the requested UID is taken. `StateDirectory=` re-owns the state directory to the service account on start, so ownership follows the account.
- Keep the encrypted credential and all state under `/var/lib/dens/<name>/`; reference the credential by absolute path in `LoadCredentialEncrypted=`.
- Use `StateDirectory=` and `RuntimeDirectory=` instead of creating and chowning directories by hand or shipping tmpfiles.d files. The instance root and its `control/` directory (lifecycle state, locks, instance config and the encrypted credential) stay root-owned and readable by the service's group; `StateDirectory=dens/<name>/data` has systemd create the data directory and own it for the service account.

### In a container

Dens installs in an unprivileged LXC system container, as Incus and LXD make them, with `security.nesting=true` and systemd 256 or newer, such as Debian 13's, Fedora's or Arch's: an older systemd can't create its credential secret there. LXC adds a drop-in to every service in such a container (`zzz-lxc-service.conf`), which turns off the service's sandbox and the encrypted credential that carries its data key. The installer refuses to go on while it's in effect, and prints the commands that mask it (M8); otherwise the service would fail to start past the point of no return. The harnesses mask it the same way. In a container, the data key is bound to the container's credential secret, which root on the host can read, and voice keys work only while the page has focus: the shortcut helper in the host's desktop session can't reach the service, whose stream checks each end's account. A virtual machine needs none of this.

### Testing the Linux service in containers

The lifecycle harness runs in Incus containers, which change how units behave:

- In unprivileged containers LXC adds `/run/systemd/system/service.d/zzz-lxc-service.conf` to every service. It turns off `NoNewPrivileges`, credentials (including encrypted ones), `ProtectProc`, `ProcSubset`, `ProtectControlGroups` and `ProtectKernelTunables`, so a unit starts and passes while running unhardened and without its data key. The harness masks it (a `/dev/null` symlink at `/etc/systemd/system/service.d/zzz-lxc-service.conf`) and starts containers with `security.nesting=true`.
- systemd before 256 (Debian 12, Ubuntu 24.04) can't create its credential host secret in an unprivileged container, so those distros run in privileged containers.
- Privileged containers get the host kernel's `binfmt_misc` table, which systemd empties when the container shuts down. On WSL that stops Windows programs from running until WSL restarts, so the harness mounts it read-only in privileged containers.
- Containers share the host kernel and have no TPM or SELinux; those need a VM or a real install.

## Windows

Windows 11, current supported releases (24H2 and later), Home and Pro. Windows 10 and Windows Server are cut.

### Service setup

- The SCM service runs as its virtual account with `SERVICE_SID_TYPE_RESTRICTED`. The token is write-restricted, so the service can write only where its service SID is explicitly granted: the state directory.
- Required privileges are trimmed to `SeChangeNotifyPrivilege`.
- The state directory gets a protected DACL (inheritance off, since `ProgramData` lets the Users group create files by default) granting SYSTEM and Administrators full control and the service SID modify.
- `%ProgramData%\Dens`, which holds the instance roots, lets Users list it, as `/var/lib/dens` is 0755 on Linux. Windows path normalization, which the SQLite driver uses, lists every parent of the database, so the service fails to open it otherwise.
- The host-bound data key is a machine-scope DPAPI blob under that DACL.
- `dens.exe` is excluded from Windows Error Reporting so crashes don't write memory dumps.
- A service has no console, so each instance registers an Application event log source under its service name, and the service records there why it stopped.

### Weaker than Linux, stated in the docs

- There is no syscall filter, capability bounding set or read-only view of the filesystem. Writes are restricted; reads follow ordinary file ACLs, so the service can read anything the Users group can. User profiles are private by default.
- The host-bound key copy is protected only by its file ACL: any process that can read the blob can decrypt it with machine-scope DPAPI. On Linux, decryption also needs the root-only host key or the TPM. There is no TPM binding in v1, but every Windows 11 machine has TPM 2.0, so binding through the Platform Crypto Provider is a good post-v1 item.
- The binary and installer are not Authenticode-signed, so SmartScreen warns on first run. Integrity comes from the cosign signatures on every platform, and the install docs show the warning and explain how to verify the download before running it.

### Known risks

- The platform spike showed sockets, interface enumeration, named pipes and child processes working under the restricted service SID, and the den e2e's call runs Pion there (M2).

## Paths

Only `internal/layout` computes these.

| | Linux | Windows |
| --- | --- | --- |
| Instance root | `/var/lib/dens/<name>` | `%ProgramData%\Dens\<name>` |
| Control directory | `<root>/control` | `<root>\control` |
| Data directory | `<root>/data` (`db/`, `logs/`, `uploads/`, `tmp/`, `cache/`) | `<root>\data` (same) |
| Host-wrapped data key | `control/datakey.cred` (systemd-creds) | `control\datakey.dpapi` (machine-scope DPAPI) |
| Control endpoint | `/run/dens/<name>/control.sock` | `\\.\pipe\dens-<name>-control` |
| Binary | `/usr/local/bin/dens` | `%ProgramFiles%\Dens\dens.exe`, on the system `PATH` |
| cosign, kept for `dens update` | `/usr/local/lib/dens/cosign` | `%ProgramFiles%\Dens\cosign.exe` |
| Service | `dens@<name>.service`, from the template `/etc/systemd/system/dens@.service` | `dens-<name>` |
| Account | `dens-<name>`, from `/etc/sysusers.d/dens-<name>.conf` | `NT SERVICE\dens-<name>` |

`tmp/` holds files only while the service works on them, such as uploads arriving and a backup's snapshot, and the service empties it each time it starts (M8).

The control directory holds everything only maintenance commands may change:

| File | Contents |
| --- | --- |
| `state.json` | Lifecycle state (see [lifecycle.md](lifecycle.md)) |
| `operation.lock`, `lifecycle.lock` | Lock files (see [lifecycle.md](lifecycle.md)) |
| `instance.json` | Instance config: name, ports, desktop user, release URL, and v1.0's den role, which the hosting switch starts from (M9.1) |
| `maintenance.log` | Every maintenance command's output for this instance |
| `datakey.cred` or `datakey.dpapi` | The data key, wrapped for this host |

## Ownership and permissions

`layout.CheckControl` and `layout.CheckData` refuse anything looser than this, and a symlink anywhere a file or directory should be.

On Linux:

- `/var/lib/dens` and the instance root are `root:root 0755`.
- The control directory is `root:dens-<name> 0750` and its files `root:dens-<name> 0640`, so the service reads them and can't change them. The data key is `root:root 0600`: systemd reads it as root before starting the service, and hands the decrypted key over in `$CREDENTIALS_DIRECTORY`.
- The data directory is `dens-<name>:dens-<name> 0700`. The unit's `StateDirectory=dens/%i/data` creates it and keeps it owned by the service account.
- `RuntimeDirectory=dens/%i` (0755) holds the socket, which is 0666. Any local user can connect; the service authorizes each connection by its `SO_PEERCRED` UID, before reading anything from it (M8).

On Windows:

- `%ProgramData%\Dens` has a protected DACL: SYSTEM and Administrators get full control, and Users may list it (`0x1200a9`, not inherited), as with `/var/lib/dens` on Linux. The service needs that: Windows path normalization (Go's `filepath.EvalSymlinks`, which the SQLite driver uses) lists every parent directory of the database.
- The instance root has a protected DACL: SYSTEM and Administrators get full control, and the service SID gets read (`0x1200a9`). The control directory and its files inherit it.
- The data directory has a protected DACL: SYSTEM and Administrators get full control, and the service SID gets modify (`0x1301bf`).
- The control pipe's DACL grants SYSTEM and the service SID full access, and the desktop user `0x12019b` (read and write data, never `GENERIC_WRITE`). Other accounts can't open it.
- `%ProgramFiles%\Dens` inherits Program Files' permissions: everyone can run the binary, and only administrators can change it.

## Transactions

### Install and update

The installer scripts run `dens install` with the release they downloaded. It installs a new instance, or updates or reconfigures an existing one, and moves every other installed instance to its version too:

1. Refuse when any instance has a newer version than this binary's: the one it has installed, or the target of a transition it was in, since its service may have migrated to it. The refusal names both. The same version installs again, which repairs (M8).
2. Refuse v1.0's den option (`--den`, `-Den` in `install.ps1`): hosting is the settings' (M9.1). Check every port of the instance against its others, which v1.0 did for the den's only while it had the den role, so an update over a config whose ports clash names the flags that move them, and the client port against other programs. The den's ports matter only once hosting is on, which binds them as it starts.
3. Print the plan. `--dry-run` stops here.
4. Collect the members: the target instance plus every installed one, sorted by name. Create missing instance roots and take each member's `operation.lock`.
5. Read each member's state, and refuse an older version again, now that nothing can change it. Journal a restart of the services that were running, as the last step to undo, then publish `installing` (a fresh instance, or one whose first install never finished) or `updating` from the recorded version.
6. Stop the services and take each `lifecycle.lock`.
7. Create the account, install the binary and cosign, register each member's service and set its permissions.
8. Write the target's instance config and generate and host-wrap a data key for a fresh instance. Have the shortcut helper (`dens shortcuts`, M7) start at the desktop user's sign-in; it serves every instance that answers them.
   - On Linux, an XDG autostart entry, `/etc/xdg/autostart/chat.dens.Dens.desktop`, starts it. Beside it, `/usr/local/share/applications/chat.dens.Dens.desktop`, with Dens's mark as its icon in `/usr/local/share/icons/hicolor`, names Dens to the desktop, whose shortcuts portal needs it, and opens the page from the desktop's menu.
   - On Windows, a scheduled task, the desktop user's and named for their SID (`Dens shortcuts-<SID>`), starts it. Also add the media ports' firewall rules, scoped to `dens.exe` and those ports, at every install, so turning hosting on needs nothing elevated (M9.1); and the `PATH` entry and the error-reporting exclusion.
9. Release the lifecycle locks and commit the journal. This is the point of no return.
10. Start each service; it migrates as it starts (see Lifecycle state in [lifecycle.md](lifecycle.md)). Once it is running, publish `ready`. Stop again any existing instance that wasn't running before; it only ran to migrate. Then start the shortcut helper, so a desktop user who is signed in needn't sign out, ending one that runs the last version: on Linux the desktop user's systemd user manager restarts the autostart entry's unit (`app-chat.dens.Dens@autostart.service`) when they're signed in to a desktop, and on Windows the installer ends the task and runs it again. A helper started otherwise takes up the new binary itself when its services come back at a newer version.

A failure before step 9 undoes the journal in reverse. A failure after it leaves the transitional state; running the installer again starts a new transition to its version from the recorded one.

`dens update` reads the release URL from the instance config (or `--release-url`), and the release host's `version` pointer there: one that names an older version than the binary's own stops it (M8). It downloads `install.sh` or `install.ps1` and its `.cosign.bundle`, and verifies them with the kept cosign against the signing identity built into the binary. It then runs the installer in the foreground with `--update --instance <name>` and the release URL, and the installer takes it from there. `--skip-verify` skips the installer's check and the release's, for unsigned test releases, and passes it on; a binary that carries the signing identity, as every release's does, refuses it.

### Restore

`dens restore BACKUP` replaces one instance's data:

1. Refuse unless the instance is `ready` (or mid-`restoring`) at this binary's version.
2. Extract the backup into a staging directory beside the data directory. Refuse a backup of another application, one from a newer schema, a wrong password, and a data key that doesn't match the backup's key check value.
3. Take `operation.lock`, publish `restoring`, stop the service and take `lifecycle.lock`.
4. Move the data directory aside (`data.replaced-<time>`) and the staging directory into its place. Move the host-wrapped key aside, and wrap the backup's data key for this host.
5. Commit the journal, move this machine's logs into the new data directory, start the service and publish `ready`. The service migrates an older backup, clears its browser sessions, turns hosting off (M9.1) and starts its den on a new sync epoch as it starts, so members' clients drop what they cached from it. Then delete the replaced data and key, the client's cache with them: a backup leaves the cache out, and the restored install fills a new one as it reads.

### Uninstall

`dens uninstall` prints its plan (`--dry-run` stops there), takes `operation.lock`, publishes `uninstalling`, stops the service and takes `lifecycle.lock`. It then unregisters the service, its event log source on Windows and its firewall rules, and deletes the instance root and the account. The last instance also removes the binary, cosign, the unit template and the shortcut helper's autostart entry, `.desktop` file and icon on Linux, or the shortcut helpers' scheduled tasks, ending the helpers they run, the `PATH` entry and the error-reporting exclusion on Windows. A helper whose instances are all gone stops by itself. Windows can't delete a running executable, so when `dens.exe` uninstalls itself it's renamed to `dens.exe.old-<time>` and deleted at the next reboot.

Uninstall has no undo. Running it again finishes an interrupted one.

## Installer scripts

`install.sh` and `install.ps1` refuse to run without root or Administrator. Then they:

1. Read the release host's root `version` pointer once, and fetch that version's binary, `version` file and `checksums.txt`.
2. Verify `checksums.txt` against its cosign bundle with a pinned cosign release, whose own SHA-256 is baked into the script.
3. Check the binary and `version` against `checksums.txt`, and check the signed version is the one the pointer named.
4. Run the binary's `install` with `--release-url` set to where it came from, `--cosign` set to the verified cosign, and the remaining arguments.

`--uninstall` (`-Uninstall`) runs the installed binary's `uninstall` instead. `--update` is accepted for `dens update` and changes nothing.

Two flags change where a release comes from and whether it's verified, which `dens update` passes on (M8):

- `--release-url URL` (`-ReleaseUrl`) installs from another release host, such as a byte-for-byte mirror; the signatures still verify. It must be `https://`, or `file://` for local test releases.
- `--skip-verify` (`-SkipVerify`) skips the signature checks, for unsigned test releases only. The checksums still apply.

Nothing in the environment changes either: an elevated shell on Windows inherits its user's environment variables, which anything running as that user can set.
