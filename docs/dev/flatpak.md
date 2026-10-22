# The Flatpak

How Dens runs as a Flatpak (M9). [lifecycle.md](lifecycle.md) has the lifecycle state and the locks every form shares.

## Design

A Flatpak brings the client to the distributions the installer refuses, NixOS and SteamOS among them, and to any other that runs Flatpak. It joins dens and hosts none: a den has to keep running when nobody is signed in, which an app in a desktop session can't, and needs Caddy beside it, so hosting stays with the installed service.

- *What it is.* The release binary, unchanged, on the freedesktop runtime (26.08), which a static binary uses nothing from but every Flatpak names. It has no window: the page opens in the member's browser through the OpenURI portal, which `xdg-open` reaches from inside the sandbox, so `host.OpenURL` works as it is. A window would need WebKitGTK, whose 2.54 release turned WebRTC off, so it couldn't call; Go daemons on Flathub send their UI to the browser the same way.
- *In the session.* Dens runs as the member, from the app's own directories: its data in `$XDG_DATA_HOME/dens/<name>` (`~/.var/app/chat.dens.Dens/data/dens/main`), the client's cache in `$XDG_CACHE_HOME`, which backup tools skip, and the control socket in `$XDG_RUNTIME_DIR/app/chat.dens.Dens`, which every instance of the app shares, so `flatpak run --command=dens chat.dens.Dens open` reaches the running service. Its files are the member's alone, as a development instance's are.
  - The desktop file runs `dens launch`, as `flatpak run chat.dens.Dens` does. If the control socket answers with a service of its version or newer, it pairs and opens the page, then exits; one older than it quits for the newest build, which keeps a newer CLI from talking to an older service for long. Otherwise it starts the service through the Flatpak portal's `Spawn`, from the newest build installed, as a sandbox of its own, and waits for it to say, on a pipe it hands it, that it's ready, or why it stopped, which a desktop notification shows too, since nothing shows a launch's output from the apps menu. A sandbox ends with its first process, so a daemon forked from the launch wouldn't outlive it, and a service the launch became would end with whatever ran it, as when the terminal that ran it closes. Two launches at once start one service: one at a time starts it, and the lifecycle lock holds the services to one. The desktop file sets `StartupNotify=false`, since no window comes.
  - Once its key is remembered (see below), it starts at sign-in through the Background portal: `RequestBackground` with `autostart` and `dens service run`, which writes the autostart entry itself and grants an app's first request without a dialog. A Dens that starts locked can't reach the dens, so until then it starts when the member opens it. A setting turns starting at sign-in off. GNOME, KDE and the xapp desktops (Cinnamon, MATE, Xfce) implement the portal; elsewhere, as on Sway, Hyprland and COSMIC, the setting gives the line to add by hand.
  - GNOME lists a running Flatpak with no window under Background Apps, within seconds of its start, with the line the app sets through the portal's `SetStatus` under its name; the close button sends the app's `quit` action through `org.freedesktop.Application`, then `flatpak kill`. Dens owns `chat.dens.Dens` on the session bus and takes the action, which stops it cleanly; SQLite's WAL survives the kill anyway. Plasma lists no background apps.
  - The service runs in a scope of its own under the member's systemd manager, so signing out stops it with `SIGTERM` once that manager stops, about 10 seconds later (logind's `UserStopDelaySec`). Signing back in sooner finds it still running, and the one the sign-in starts steps aside.
- *The data key* has no copy that opens without the member. The service starts locked, and the page asks for the local password, whose copy of the key (Argon2id, as on every install) opens it; until then the member's dens stay disconnected, and they catch up once it's unlocked. Remember on this computer, beside the password field, off by default, keeps a copy that opens without it, so Dens starts unlocked from then on. It never keeps the password itself, which also opens the member's backups and may open more.
  - The remembered copy is wrapped with the app's secret from the Secret portal (`RetrieveSecret`), which the desktop keeps in the member's keyring and hands over through a pipe. That keeps it from someone who has the disk but not the member's sign-in, not from programs the member runs, which can ask the keyring too.
  - The portal can't be taken on trust. KWallet before KDE Frameworks 6.30 answers a cancelled unlock with a new random secret, as if it had succeeded, as Kubuntu 26.04's 6.24 does, and SteamOS 3.8 ships 6.16; gnome-keyring, oo7, which Fedora 45's GNOME asks first, and KWallet from 6.30 fail it. So Dens takes the portal's secret only once it has answered the same twice.
  - The portal can take its time. A locked keyring, as after an automatic sign-in, prompts, and the request waits for as long as the member takes, seconds or minutes, so the service asks in the background, one request at a time, while the page offers the local password, and whichever answers first unlocks it. At sign-in the portal's Secret interface can be missing for a few seconds while the desktop starts its backends, so the service waits for it before taking its absence for no backend. The wrap is authenticated, so a wrong secret fails to open it rather than yielding a wrong key. A remembered copy that stops opening, as when a keyring is reset, is never replaced on its own: Dens asks for the password again, and remembers anew once the portal answers the same twice.
  - Where the desktop has no Secret backend, or one that fails or answers differently, the remembered copy is the key in a file beside the lifecycle state, which keeps it from other Flatpaks and nothing else, and the option says so, and why.
  - A KDE with no wallet yet runs KWallet's wizard at the first request, whose default, GPG, fails without a GPG key. The docs say to pick Classic, with the password the member signs in with, which then opens the wallet at sign-in; after an automatic login it asks.
  - The settings' System tab turns Remember on, after the local password, and off, deleting the remembered copy. It also quits Dens, which no service manager stops.
  - Until the member sets the local password, at their first pairing, the key waits in a file, as a development instance's does; setting the password removes it, unless they ask Dens to remember.
- *Who can read what* changes, and the public docs say so. The installed service runs as its own account, so a program running as the member can't read Dens's data or keys without root. The Flatpak runs as the member, so their programs can reach its files, and a remembered copy of its key; without one, the files open only with the local password. Flatpak's sandbox keeps other Flatpak apps out.
- *Lifecycle.* No installer runs: `install`, `update` and `uninstall` refuse this form, and `restore` runs as the member while the service is stopped, given the backup through `flatpak run --file-forwarding`. The app writes its own instance config at first start, naming the member and a client port of its own, 8494, so it and an installed Dens never take each other's port or cookie. It publishes its own lifecycle state, as a development instance does, and decides under the lifecycle lock: the first build to start against data an older version left publishes the transition to itself, keeps a snapshot of the data made with the backup's code (which leaves the cache out), and migrates as it starts; data a newer version wrote it refuses, saying Dens was rolled back, since Flatpak can roll an app back, and the snapshot is what a member who did restores. Flatpak never touches a running instance, so an update takes effect when the service restarts: it starts the newest build through `Spawn`, which waits for its lock, and stops itself, releasing its lock, socket and port, and the new build migrates. Hosting is refused, and the settings say it needs Dens installed on Linux or Windows.
- *Updates.* Flatpak installs and updates it: GNOME Software along with every other app, Discover and SteamOS when the member updates there. The notice comes from Flatpak's own update monitor (`CreateUpdateMonitor`), which checks every 30 minutes, not the release host's `version` file, which moves before the Flatpak repository does. Installing through the monitor takes the member's OK, which KDE asks for and GNOME refuses to ask an app with no window for, and is refused to an update that asks for a new permission. So the notice offers Update where that works, says to update Dens in the software center where it doesn't, and offers Restart once an update is installed by anything (`Spawn` with the latest version).
- *Voice keys* need no separate helper, since Dens already runs in the session: the helper runs inside the service, over a pipe to the hub. It binds keys through the global shortcuts portal, which knows a sandboxed app by its Flatpak ID, wherever the portal's interface exists rather than only where `WAYLAND_DISPLAY` is set, and skips the host registry call, which a sandboxed app can't make: GNOME 50 at version 1, GNOME 51 and Plasma at version 2, and Plasma under X11 too. Under X11 it can also read them over the X11 socket, as the installed helper does.
- *Permissions:* `--share=network`, `--socket=wayland` and `--socket=fallback-x11`, and no files, which go through the browser. With Wayland granted, `fallback-x11` gives the app X11 only in an X11 session, on every Flatpak version; without it, Flatpak before 1.18 gives it Xwayland under Wayland too. Dens draws no window, and asks for Wayland for that alone. X11 lets an app read every key typed in an X11 session, which GNOME Software flags as it installs ("potentially unsafe": Legacy Windowing System); push to talk under X11 needs it.
- *Dens's own repository.* The Flatpak comes from a repository on the release host, not Flathub, signed with a GPG key: Flatpak checks no other kind of signature (support for Ed25519 has waited unmerged since 2021), and its system helper refuses an unsigned remote. Members install it from one link, `flatpak+https://releases.dens.chat/flatpak/chat.dens.Dens.flatpakref`, which GNOME Software 48 and Discover 6.4 open directly; the plain link and `flatpak install` are the fallbacks. The file carries the public key, an Ed25519 one, which GNOME Software 50 and 51, Discover 6.6 and 6.7 and `flatpak` all check, and adds Flathub where a system lacks it, for the runtime. A software center that installs for the whole system, as on Fedora, asks for the admin password. Software lists Dens, with its license and icon, from the AppStream data composed from its MetaInfo, whose content rating declares its chat and voice. Flathub would build Dens from source, offline, which the page's prebuilt Tailwind fights; takes no app without a window; and signs with its own key rather than the release identity. It stays an option should Dens grow a window.
  - Its commits are made from the release's binaries with `flatpak` itself, without flatpak-builder or the SDK: a written `metadata` naming the runtime's branch, `appstreamcli compose`, `flatpak build-finish` and `build-export`. `build-update-repo` writes the delta from the last build, with bsdiff while the old and new binaries are under OSTree's 128 MB together, as two stripped builds are: 12.7 MB from v1.0.0 to M9.2's build, whose binary alone gzips to 23.8 MB. Past that, `ostree static-delta generate --max-bsdiff-size` writes it first.
- *Beside an installed Dens* it's unsupported: the two would share a desktop file ID and the shortcuts portal's app ID. Their ports and cookies don't collide.
- *The spike* (`spikes/flatpak/README.md` at commit a80f371) ran the Flatpak's launches, portals, update and repository in a Debian 13 container and in VMs of Fedora 44 (GNOME 50 and Plasma 6.7), Fedora 45 (GNOME 51) and Kubuntu 26.04 (Plasma 6.6, whose KWallet is from before its fix), and its findings are the measurements above.

## The form and its paths

The release binary runs as the Flatpak (`layout.Flatpak`, M9.3) in a sandbox of the app `chat.dens.Dens`: one whose `/.flatpak-info` and `FLATPAK_ID` both name it. It's a session form, as a development instance is, run by the member in their session, as Design describes.

| | Path |
| --- | --- |
| Instance root | `$XDG_DATA_HOME/dens/<name>`, which Flatpak points at `~/.var/app/chat.dens.Dens/data/dens/<name>` |
| Control and data directories | `<root>/control` and `<root>/data`, as for every form, but for the cache |
| The client's cache | `$XDG_CACHE_HOME/dens/<name>` (`~/.var/app/chat.dens.Dens/cache/dens/<name>`), which backup tools skip |
| Control endpoint | `$XDG_RUNTIME_DIR/app/chat.dens.Dens/<name>/control.sock`, in the runtime directory every sandbox of the app shares |
| Snapshot | `<root>/snapshot/`, the last one only |

Every directory is the member's and `0700`, and every file `0600`; the checks refuse anything looser, as for a development instance. Its instance config, written at its first start, names the member and gives it client port 8494, apart from an installed Dens's.

## Starting

Under its lifecycle lock, which it waits up to 30 seconds for, the service compares `state.json` with its version `V`:

| State | The service |
| --- | --- |
| None | Publishes `installing` to `V`, and migrates: a new instance |
| `ready`, version `V` | Starts |
| `ready`, an older version | Keeps a snapshot of the data, publishes `updating` to `V`, and migrates: an update |
| `ready`, a newer version | Refuses: Dens was rolled back. The refusal names the snapshot, which a restore takes back |
| Transitional, target `V` | Migrates: a transition that didn't finish |
| Transitional, an older target | Takes it over: publishes the same phase to `V`, and migrates on from where the older build stopped. The snapshot from before that transition stays the one to restore |
| Transitional, a newer target | Refuses |

It publishes `ready` once it has its key and has done what the transition asked of the start, as a restore's clearing of browser sessions and its den's new sync epoch.

## The snapshot

The snapshot is a backup archive of the data an older version left, made with the backup's code before the update migrates them: the database through `VACUUM INTO`, and the uploads, but not the cache. It's made before the transition is published, so one that fails, as on a full disk, leaves the state as it was, and the start refuses, saying why; the next start tries again.

## The data key

The control directory holds the Flatpak's copies of its key that open it without the member, while it has them:

| File | Holds |
| --- | --- |
| `datakey.file` | The key itself: a new instance's, until its local password exists, or the remembered copy where no keyring can be trusted with it |
| `datakey.keyring` | The remembered copy, sealed (XChaCha20-Poly1305) under a key derived with HKDF-SHA256 from the secret the member's keyring keeps for the app, which the Secret portal gives |

With neither, the service starts locked. It opens its database, migrating it as the transition says, and serves the page its unlock screen and nothing else: every other route of the page's API answers 423, and the control endpoint takes only status, pairing and quit. The local password, or the keyring's secret opening `datakey.keyring` in the background, unlocks it, and then the rest of the service starts. A data key whose file is gone before any local password was set leaves nothing to open the data, and the start refuses, saying so.

## Restoring

Restoring runs as the member, given the backup through `flatpak run --command=dens --file-forwarding chat.dens.Dens restore @@ BACKUP @@`:

1. Refuse an instance with no lifecycle state, which hasn't started yet; any state else is fine, as the data go, which is how the snapshot comes back after a rollback.
2. Extract and check the backup as the installed restore does.
3. Take `operation.lock`, have a running service quit over the control endpoint, and take `lifecycle.lock`.
4. Publish `restoring` at this version.
5. Move the data aside (`data.replaced-<time>`) and the backup's in; delete the remembered copies of the key, which were the replaced data's, and empty the cache.

The next start resumes the restore: it clears the browser sessions, turns hosting off and starts a new sync epoch, starts locked, and, once the backup's local password opens it, publishes `ready` and removes the replaced data.

## Launching and restarting

The desktop file runs `dens launch`, as does `dens` with no command, which `flatpak run chat.dens.Dens` runs. It takes `launch.lock`, then asks the control endpoint for the service's status. A service of its version or newer is the one it pairs with, opening the page. One older than it quits, over the control endpoint, and with none it starts `dens service run --ready-fd 3` through the Flatpak portal's `Spawn`, from the newest build installed, as a sandbox of its own, handing it a pipe as descriptor 3, on which the service says `ready` once it serves, or why it stopped. A service of its version or newer that answers meanwhile, as one starting at sign-in, serves too. A launch that fails says why in a desktop notification, since nothing shows its output from the apps menu. The page's Restart, and Update once Flatpak has installed the update, start `dens service run` through `Spawn` the same way and quit, and the new service waits for the old one's lock.

## Testing the Flatpak

`./scripts/test.sh -flatpak-e2e` runs `scripts/test-flatpak-e2e.sh`, which makes the Flatpak from this tree's binary with `scripts/flatpak.sh`, into a repository signed with a test GPG key as the release's is, and runs it in a Debian 13 container with no desktop, as the member alice: her systemd user manager runs her session bus and Flatpak's portal, which checks for updates every few seconds there, and the app may update itself, as KDE asks the member once. The container has the portal's frontend, for the permission store, and no backends, so no keyring. Two launches at once start one service; the page's API pairs and sets the local password; a restarted service starts locked, refuses the settings, and opens with the password; Remember keeps the key in a file, waiting out the Secret portal's interface, and the next start is unlocked. The Flatpak joins a den behind Caddy in a second container, hosted as the den e2e's is, trusting its certificate authority through `SSL_CERT_FILE`, since the runtime has a store of its own. The monitor finds the fixture's next version once it's committed, which the page installs, restarting into it, which migrates after a snapshot, and the den reconnects. Rolled back to the build before with `flatpak update --commit`, the launch fails with the refusal, naming the snapshot, which `dens restore` takes back; that build then starts locked, pairs again and opens with the password, and the den reconnects.

The Flatpak's container starts from a local image, `dens-e2e/flatpak`, with Flatpak, the member and the freedesktop runtime installed, which the first run makes, downloading the runtime from Flathub; `FRESH_IMAGE=true` makes it again. `KEEP_FAILED=true` keeps failed containers, and logs land in `out/flatpak-e2e-logs/<run>/`.
