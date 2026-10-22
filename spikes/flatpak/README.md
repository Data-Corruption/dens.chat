# The Flatpak spike (M9)

Before M9.3 builds the Flatpak form, this spike answers what its code will lean on, on the desktops it has to work on. `fprobe` stands in for Dens inside the sandbox: `fprobe launch` does what `dens launch` will, and the service it becomes runs Dens's own control endpoint, `OpenURL`, locked memory, low-priority children and loopback listeners, owns the app ID on the session bus, serves a page at `http://localhost:8494`, and watches the update monitor. The app is `chat.dens.Spike`, so it never meets an installed Dens; each build carries a stripped `dens` too, for the deltas.

The questions, from `docs/dev/milestones.md`:

1. `dens launch` becoming the service under bubblewrap: how long it lives, a second launch reaching it, two launches at once, and signing out.
2. `RequestBackground`: the entry it writes and whether a dialog shows; GNOME's Background Apps listing a service that never had a window, and its close button.
3. `RetrieveSecret` across calls and restarts, on gnome-keyring, oo7 and KWallet before and after Frameworks 6.30; a keyring still locked after an automatic sign-in; how long an unlock prompt holds the start.
4. The global shortcuts portal from inside the sandbox without `WAYLAND_DISPLAY`, on each desktop, and on Plasma under X11.
5. The update monitor: when `UpdateAvailable` fires, `Update` to a build that asks for nothing new, and `Spawn` into it; and whether GNOME Software's automatic updates take Dens's remote.
6. The `.flatpakref` link in GNOME Software and Discover, and whether every client checks an Ed25519 key, or the key must be RSA-4096.
7. The smallest pinned toolchain that builds the commit, in CI and in a container, with the runtime pinned by its commit; and one real static delta between two stripped builds.

## What the container answered

In `dens-flatpak-spike`, an Incus container of Debian 13 (Flatpak 1.16.6, OSTree 2025.2, GnuPG 2.4.7), with a session bus and no desktop:

- **The toolchain (7):** `flatpak`, `ostree` and `gnupg`, gdk-pixbuf's SVG loader (`librsvg2-common`), which `build-export` checks the icon with, and AppStream's compose tool (`appstream-compose`), whose `appstreamcli compose` makes the data software centers list an app by, its license and icons included, as flatpak-builder would. No flatpak-builder, SDK or runtime: `flatpak build-init` refuses without the SDK installed, but a hand-written `metadata` file beside `files/`, then `build-finish` and `build-export`, make the commit. The metadata names the runtime by branch, `org.freedesktop.Platform/x86_64/26.08`, which Flathub has (commit `92659297…`, 2026-09-28); clients take that branch's newest commit, so the build pins nothing beyond the branch.
- **Ed25519 (6), in the CLI:** a repository signed with an Ed25519 GPG key installs, and a client given another key refuses it ("Can't check signature: public key not found"). The software centers are the VMs' to answer.
- **The delta (7):** v1.0.0's binary, stripped after the fact (55.5 MB), to this tree's release build (56.1 MB): bsdiff makes a 12.7 MB delta, where the binary alone gzips to 23.8 MB. Together they're under OSTree's 128 MB bsdiff limit, so `build-update-repo --generate-static-deltas` makes that delta itself, and raising the limit changes nothing. The limit bites once each binary passes about 64 MB; then `ostree static-delta generate --max-bsdiff-size` comes first.
- **The update monitor (5):** it polls every 30 minutes (`DEFAULT_UPDATE_POLL_TIMEOUT_SEC` in Flatpak's portal), first 30 minutes after the first monitor starts, and then reports the remote's commit. With the poll shortened, `UpdateAvailable` came on time; `Update` installed v2 in 1.2 s, and `Spawn` with `FLATPAK_SPAWN_FLAGS_LATEST_VERSION` started v2, which took over the socket, the page's port and the bus name 0.3 s after v1 let go of them. `Update` asks the member once ("Update Dens Spike? The application wants to update itself.", kept in the permission store as `flatpak`/`updates`), which the container granted ahead, and refuses a build that adds a permission: "Self update not supported, new version requires new permissions" (v3, which adds `--socket=wayland`).
- **Launching (1):** two launches at once make one service; the second reached it 39 ms later, over the control endpoint, whose peer check works between sandboxes. Every sandbox numbers its own processes, so the service is pid 2 in each; Flatpak's instance ID tells them apart.
- **Permissions:** `build-finish --socket=fallback-x11` writes `sockets=x11;fallback-x11;`. Flatpak drops X11 for `fallback-x11` only where the app has Wayland, so without `--socket=wayland` the app may get X11 under a Wayland session; `fprobe report` shows `DISPLAY` in each.

## Building it

```sh
spikes/flatpak/build.sh              # v1, v2 and v3 built; v1 published; staged
spikes/flatpak/build.sh publish v2   # then v3: as a release would publish them
```

It stages in VMware's shared folder, `C:\Users\datac\dens-vm\flatpak-spike` (`STAGE` moves it): `web/` holds the two repositories (`repo-ed`, signed with an Ed25519 key, and `repo-rsa`, with an RSA-4096 key), a `.flatpakref` and `.flatpakrepo` for each, and a page linking them; `vm/` holds the VMs' scripts; `results/` collects what they find. The keys live in the container and in `out/flatpak-spike/keys`, and come back into a new container, since a VM keeps trusting only the key it installed with.

## In the VMs

| VM | Stands for |
| --- | --- |
| Fedora 44 Workstation | GNOME 50, gnome-keyring, shortcuts portal version 1 |
| Fedora 45 Workstation (a clone of the 44 one, upgraded) | GNOME 51 |
| Fedora 44 KDE | Plasma 6.7, shortcuts version 2, KWallet from Frameworks 6.30 on, if its updates have it |
| Kubuntu 26.04 LTS | KDE before KWallet's fix: Plasma 6.6 with Frameworks 6.24, as SteamOS 3.8's Desktop Mode has Plasma 6.4.3 with Frameworks 6.16, and an X11 session |

**Once per VM.** Flatpak installs apps from repositories, as a package manager does, but each app runs in a sandbox and brings its own libraries, the runtime: Dens Spike uses the freedesktop runtime from Flathub, which the first install downloads (a few hundred MB, so it takes a while). Apps install for the whole system or for one user; the software center picks.

1. Kubuntu only, first: `sudo apt update && sudo apt full-upgrade -y && sudo apt install -y flatpak plasma-discover-backend-flatpak open-vm-tools-desktop`, then reboot.
2. The Fedora 45 clone only, first: `sudo dnf upgrade --refresh -y && sudo dnf system-upgrade download --releasever=45 -y && sudo dnf system-upgrade reboot`.
3. Mount the shared folder for good, so it outlives signing out (a mount made from a terminal dies with the session, and then reads fail with "Transport endpoint is not connected"). First check that VMware shares it: `vmware-hgfsclient` should print `dens-vm`; if it prints nothing, turn on Shared Folders in the VM's settings (Options, Shared Folders, Always enabled, with `dens-vm` as `C:\Users\datac\dens-vm`). Then make a systemd mount for it, which works whatever FUSE version the system has:
   ```sh
   sudo umount -l /mnt/hgfs 2>/dev/null; sudo sed -i '\|^\.host:/ /mnt/hgfs|d' /etc/fstab
   printf '[Unit]\nDescription=VMware shared folders\nAfter=vmtoolsd.service\n\n[Mount]\nWhat=.host:/\nWhere=/mnt/hgfs\nType=fuse.vmhgfs-fuse\nOptions=allow_other\n\n[Install]\nWantedBy=multi-user.target\n' | sudo tee /etc/systemd/system/mnt-hgfs.mount >/dev/null
   sudo systemctl daemon-reload && sudo systemctl enable --now mnt-hgfs.mount && ls /mnt/hgfs/dens-vm
   ```
   The last command should list `flatpak-spike`.
4. Give every terminal a `spike` command, which runs the probe inside the sandbox: `echo "alias spike='flatpak run --command=fprobe chat.dens.Spike'" >> ~/.bashrc`, then close the terminal.
5. Open a terminal, run `sh /mnt/hgfs/dens-vm/flatpak-spike/vm/serve.sh`, and leave it open: it serves the repositories at `http://localhost:8080/`, as the release host will serve Dens's. Do the steps below in a second terminal.

Screenshots of anything a step says to note go in `docs/dev/tmp/logs`, named for the VM and the step.

**Part 1, in every VM**, in order (✓ where a step applies):

| Step | F44 GNOME | F45 GNOME | F44 KDE | Kubuntu |
| --- | --- | --- | --- | --- |
| 1. Install from the link | ✓ | ✓ | ✓ | done |
| 2. Launch | ✓ | ✓ | ✓ | done |
| 3. `spike auto` | ✓ | ✓ | ✓ | done |
| 4. Background Apps | ✓ | ✓ | | |
| 5. Signing out, and starting at sign-in | ✓ | | ✓ | done |
| 6. Shortcuts | ✓ | ✓ | ✓, and under X11 | done |
| 7. A locked keyring | ✓ | ✓ | ✓ | done |
| 8. Collect | ✓ | ✓ | ✓ | done |

**1. Install from the link.** In the VM's browser, open `http://localhost:8080/` and click the first link, the Ed25519 one. The browser downloads `chat.dens.Spike.flatpakref`: open it from the download list or the file manager, and Software (GNOME) or Discover (KDE) shows "Dens spike (Ed25519)". Click Install, and wait while it downloads the runtime. Note any question or warning it shows, such as one about the windowing system. Then click the third link, `flatpak+https://…`: it can't download here, so only note which program opens it, if any. The RSA link is for a client that refuses the Ed25519 key; use it only if the install fails.

**2. Launch.**
- Open Dens Spike from the apps menu (search for it). Nothing shows for a moment, then the browser opens the spike's page: "Dens spike v1", with an instance number. If a dialog asks which app opens the link, note it, pick the browser, and screenshot it.
- Open it from the menu again: another tab opens on the same page, with the same instance number. That's a second launch reaching the running service, as `dens launch` will.
- In the terminal, `spike quit` stops the service. Then `flatpak run chat.dens.Spike launch & flatpak run chat.dens.Spike launch &` starts two at once: two tabs open, on one instance.

**3. `spike auto`** prints what the sandbox gives the app, checks locked memory and a low-priority child, and asks the desktop's keyring for the app's secret twice. If the keyring asks something, such as to create a wallet, unlock it, or allow access, answer as a member would, and note what it asked. KDE may ask to create a wallet: its default, GPG, fails without a GPG key, so pick Classic, and give it the password you sign in with, which lets KDE open it at sign-in. The two `RetrieveSecret` lines should show the same digest.

**4. Background Apps** (GNOME). With the service running (open Dens Spike from the menu), open the system menu at the top right after about 10 seconds: is there a Background Apps entry listing Dens Spike? Run `spike status "Connected to 2 dens"`: does that line show under Dens Spike there? Then press the close button beside it, and run `spike ping`: an error means the service stopped. The log says whether it stopped cleanly (`stopping: quit through …`) or was killed (its last line is earlier).

**5. Signing out, and starting at sign-in.**
- With the service running (open Dens Spike from the menu), `spike background -autostart` asks the desktop to start it at sign-in. Note any dialog, then `cat ~/.config/autostart/chat.dens.Spike.desktop`.
- Sign out, and **wait 30 seconds** at the login screen: the system keeps a signed-out user's processes for about 10 seconds, so a quicker return finds the old service still running. Sign back in, wait a few seconds, and run `spike ping`: it should answer, "how: at sign-in". No browser opens at sign-in.
- `grep -E 'stopping|starts' ~/.var/app/chat.dens.Spike/data/spike.log | tail -4` says how the old service stopped at the sign-out (`stopping: signal terminated`), or shows nothing for it if it was killed.
- Turn on automatic login (GNOME: Settings, System, Users, Automatic Login; KDE: System Settings, Login Screen (SDDM), Behavior, Automatically log in), restart the VM, and run `spike ping` again. Note whether the keyring asked for a password after the automatic login. Turn automatic login off again afterwards.

**6. Shortcuts.**
- `spike shortcuts`: the desktop shows a dialog for three shortcuts, F8, F9 and F10. Accept it. Then click on another window, press and hold F8, and let go: the terminal shows `ptt down` and `ptt up`. Ctrl+C in the terminal stops it.
- `spike shortcuts -configure`: the same, and then the desktop should open its settings for these shortcuts. Note whether it does.
- Fedora 44 KDE, under X11: `sudo dnf install -y plasma-workspace-x11`, sign out, pick Plasma (X11) from the session menu on the login screen, sign in, and run `spike shortcuts`, then `spike x11keys`, which reads the keys over X11 for 20 seconds as Dens's helper does: press F8 with another window in front. Sign back in to the usual session afterwards. If the X11 session doesn't start, note it and skip this.

**7. A locked keyring.**
- GNOME: `sh /mnt/hgfs/dens-vm/flatpak-spike/vm/lock.sh` locks the login keyring. Then `spike secret -count 2`, and **cancel** the password prompt each time: note what the terminal says. Lock it again, run `spike secret`, and this time unlock it.
- KDE: a session you signed in to with your password reopens the wallet by itself, so this takes an automatic login. With automatic login on and the autostart entry from step 5 in place, restart the VM. Soon after the desktop appears, the wallet asks for its password: **cancel** it, and cancel the second prompt too. Then `grep -E "Secret portal|RetrieveSecret" ~/.var/app/chat.dens.Spike/data/spike.log | tail -4`, and `spike secret`, entering the password this time. Turn automatic login off afterwards.
- A `DIFFERENT from the last answer` line after a cancel is KWallet's bug, which Kubuntu's Frameworks 6.24 has; Fedora 44 KDE's should not, if its Frameworks are 6.30 or newer.

**8. Collect:** `sh /mnt/hgfs/dens-vm/flatpak-spike/vm/collect.sh`. It writes this VM's versions, settings and logs to `results/` in the shared folder, which I read.

**Part 2, updates** (Fedora 44 GNOME and Fedora 44 KDE), with v2 published. v2 carries AppStream data, which v1 lacked, so it also shows whether that's what software centers list an app by.

1. Start `serve.sh` in its own terminal, as for installing.
2. The update monitor checks every 30 minutes. To wait less, have it check every 30 seconds, then start the service again: `spike quit; pkill -f /usr/libexec/flatpak-portal; /usr/libexec/flatpak-portal --replace --poll-timeout=30 &`, then open Dens Spike from the menu.
3. `sh /mnt/hgfs/dens-vm/flatpak-spike/vm/log.sh` shows the log as it grows: within a minute, an `UpdateAvailable` line. Meanwhile, does Software or Discover list the update? Don't install it there.
4. `spike update`: a dialog asks whether Dens Spike may update itself; allow it. The page in the browser turns to v2 by itself, and the terminal says the service restarted.
5. Software (GNOME): is Dens Spike in the Installed list now, and what license does its page show? Discover (KDE): the same on its page.
6. Tell me, and I publish v3, which asks for one more permission (Wayland).
   - `spike ping` should say v2; if it says v1, `spike quit` and open Dens Spike from the menu. If the terminal running the faster portal from step 2 has closed, run that command again.
   - Once `UpdateAvailable` names v3, `spike update`: it should say the update needs new permissions.
   - `flatpak update chat.dens.Spike` installs v3, asking about the new permission.
   - Within a minute the log has another `UpdateAvailable`, whose `local-commit` is v3's now. `spike update` then restarts the service into v3 without a dialog, as Dens would after the software center installed its update.
   - `spike report`: note `DISPLAY` and `WAYLAND_DISPLAY`.
7. Fedora 44 KDE only, KWallet with its fix: turn on automatic login, restart the VM, and **cancel** the wallet's prompt both times. Then `grep -E "Secret portal|RetrieveSecret" ~/.var/app/chat.dens.Spike/data/spike.log | tail -4`: with Frameworks 6.31 a cancel should fail (`response 2`), where Kubuntu's 6.24 answered with random secrets. Turn automatic login off afterwards.
8. Collect again.

If Software or Discover installs v2 by itself before step 4, note it: then `spike quit` and open Dens Spike from the menu, which starts v2.

The logs are `~/.var/app/chat.dens.Spike/data/spike.log`; `collect.sh` copies them.

## Results

**The answers**, from the container and the four VMs below:

1. *Launching.* Two launches at once make one service, and a later launch reaches it in under 4 ms over the control endpoint, whose peer check works between sandboxes. The service runs in its own scope under the member's systemd manager. Signing out stops it with SIGTERM once that manager stops, about 10 s after (logind's `UserStopDelaySec`; a quicker return finds it still running), but a service started from a terminal gets SIGHUP when the terminal goes. So `dens launch` should start the service through the Flatpak portal's `Spawn`, which nothing that ran the launch holds, rather than become it.
2. *Background.* `RequestBackground` is granted without a dialog on GNOME 50 and 51 and Plasma 6.6 and 6.7, in 3 to 7 ms, and writes `~/.config/autostart/<app>.desktop` with `Exec=flatpak run --command=fprobe chat.dens.Spike daemon …`. GNOME's Background Apps lists the windowless service within seconds and shows `SetStatus` live; its close button sends `org.freedesktop.Application.ActivateAction("quit")` to the app ID on the session bus, which the service takes to stop cleanly. Plasma has no such list.
3. *The secret.* The same 64 bytes across calls, restarts and sign-ins, from gnome-keyring 50, oo7 0.7 (Fedora 45's first choice), and KWallet with Frameworks 6.24 and 6.31. At sign-in the portal's Secret interface may not be there yet, for 0.1 to 4.5 s. A locked keyring, as after an automatic login, prompts, and the request waits as long as the member takes, from seconds to minutes. Cancelled, gnome-keyring, oo7 and KWallet 6.31 fail (response 2), but KWallet 6.24 answers success with new random bytes each time, which two answers that must match catch. A KDE session signed in with the password reopens its wallet without asking; a KDE with no wallet runs KWallet's wizard on the first request, whose default, GPG, fails without a GPG key.
4. *Shortcuts.* The portal works from the sandbox, with no `WAYLAND_DISPLAY` and no registry call, everywhere: GNOME 50 at version 1 (its dialog at the first bind only, no `ConfigureShortcuts`, the keys changed in Settings), GNOME 51 at version 2 (`ConfigureShortcuts` reopens its dialog), Plasma 6.6 and 6.7 at version 2 (`ConfigureShortcuts` opens System Settings), and Plasma 6.7 under X11, where Dens's X11 key reader works in the sandbox too.
5. *Updates.* The monitor polls every 30 minutes, the first time 30 minutes after it starts, and reports the remote's commit and the installed one. `Update` installs an update that asks for no new permission once the member allows it: KDE asks, but GNOME refuses for an app with no window ("Only the focused app is allowed to show a system access dialog"). `Spawn` with the newest version then restarts the service in 0.3 to 2.4 s. An update installed by something else shows at the monitor's next poll, and `Update` before then answers "nothing to update". Software and Discover list the update; Software didn't install it by itself within two hours. On GNOME, then, Dens can only say an update is waiting and restart once it's installed.
6. *The link.* GNOME Software 50 and 51 and Discover 6.6 and 6.7 open the `.flatpakref`, take `flatpak+https://`, and install from a repository signed with an Ed25519 key, adding Flathub for the runtime; on Fedora they ask for the admin password, installing for the whole system. Software warns that the app is "potentially unsafe" for `fallback-x11` (Legacy Windowing System), and for an unknown license without AppStream data, without which it doesn't list the app; an empty OARS rating has it claim the app has no chat. Discover warns of nothing, and fails if the `.flatpakref` is opened again once installed.
7. *The toolchain and the delta:* as the container answered.

Besides: locked memory works (RLIMIT_MEMLOCK 8 MiB), and so do low-priority children. With `fallback-x11` alone, Flatpak 1.16 gives the app X11 under Wayland and Flatpak 1.18 doesn't; with `--socket=wayland` too, the app has `WAYLAND_DISPLAY` and no `DISPLAY` on either.

The VMs' own findings follow.

**Kubuntu 26.04 LTS** (Discover):

- Step 1: opening the `.flatpakref` from the file manager showed it in Discover as "Dens spike (Ed25519)", with a note that the app comes from "dens-spike" at `http://localhost:8080` and that the repository's other software shows in Discover once it's installed; Size and Licenses unknown. Install took many seconds before its progress bar showed (the runtime), then finished: Discover's Installed lists Dens Spike with the metainfo's summary, 61.7 MiB, and Network Access as its only permission, with nothing about X11. The Ed25519 key works in Discover. Opening the `.flatpakref` again once installed fails: "Update Issue: There was an issue during the update or installation process", then "Could not open file:///…/chat.dens.Spike.flatpakref". Firefox hands the `flatpak+https://` link to the system's handler, which is Discover.
- Step 2: as expected, from the menu, again, and two at once.
- Step 3: the first secret had KWallet create a wallet: "The application 'xdg-desktop-portal' has requested to create a new wallet named 'kdewallet'", offering Classic (Blowfish) or GPG, with GPG chosen. With no GPG key, GPG fails ("Seems that your system has no keys suitable for encryption. Please set-up at least one encryption key, then try again.") and leaves an empty list of keys; Back, Classic and the password the member signs in with made the wallet, so KDE opens it at a sign-in that types the password. The service started from the terminal kept writing its log there, which the probe no longer does.
- Step 5: `RequestBackground` with autostart succeeded in 7 ms, with no dialog, and wrote `Exec=flatpak run --command=fprobe chat.dens.Spike daemon -autostart -secret`, with `X-XDP-Autostart` and `X-Flatpak`. A quick sign-out and back in left the running service running; the one started at sign-in found it and stopped.
- Step 6: the shortcuts dialog, and `ConfigureShortcuts`, work. The Plasma (X11) session, which Kubuntu 26.04 doesn't install or support, fails to start in the VM, and leaves a crash report.
- Versions: Plasma, Discover and xdg-desktop-portal-kde 6.6.6, xdg-desktop-portal 1.21.1, KWallet 6.24.0, Flatpak 1.16.6, GnuPG 2.4.8 with GPGME 2.0.1. Discover installed the app for the whole system. Portals: Background 2, Secret 1, GlobalShortcuts 2, OpenURI 5, Flatpak 7; `kde-portals.conf` sends Secret to `kwallet`.
- The sandbox: under Wayland, with `fallback-x11` alone, `DISPLAY=:0` and an Xauthority are there, so the app has X11, through Xwayland, without asking for Wayland. Locked memory works (RLIMIT_MEMLOCK 8 MiB, 1 MiB locked), and so does a child at nice 19 under SCHED_IDLE.
- The service runs in its own scope, `app-flatpak-chat.dens.Spike-<instance>.scope`, under the user's systemd manager, and the autostart entry's `flatpak run` as `app-chat.dens.Spike@autostart.service`. A sign-out waited out (logind's `UserStopDelaySec`, 10 s; no lingering) stops it with SIGTERM: `stopping: signal terminated`. The Background portal's monitor lists it as a background app, though Plasma shows no such list.
- Shortcuts: version 2, KDE's dialog on the first bind (answered in 8 s), none after, and `ListShortcuts` lists them before they're bound; timestamps 0.
- The secret: the first `RetrieveSecret`, which set off the wallet's creation, had no answer in the probe's 3 minutes while the wizard was up. Since then it answers in 2 to 4 ms with the same digest, across calls and restarts. At two of three sign-ins the service's first request failed with "No such interface “org.freedesktop.portal.Secret”": the journal shows xdg-desktop-portal, activated 4 s earlier, still bringing up KWallet's backend. Dens's service has to wait for the interface at sign-in rather than take its absence for no keyring; the probe now logs how long it took.
- Closing the wallet in a session signed in with the password doesn't make the next request ask for it: the password KDE took at sign-in opens it again. The prompt, and KWallet's random-secret bug, take a sign-in that never typed it, an automatic login.
- **KWallet's bug, seen:** after an automatic login, the service's request at sign-in brought up the wallet's password prompt. Cancelled, it answered success with 64 random bytes: digest `40909acc…` after 8.8 s, then `b922d84a…` after 7.2 s for the second request, cancelled too, where the real secret is `984f52e1…`. With the password typed, the next request answered `984f52e1…` after 6.2 s. Two answers that differ are how Dens tells: a single answer would have wrapped its key with noise. An unlock prompt holds the request for as long as the member takes, so nothing in the start may wait on it.

**Fedora 44 Workstation** (GNOME Shell 50.5, Software 50.4, gnome-keyring 50.0, xdg-desktop-portal 1.22.1 with -gnome 50.0, Flatpak 1.18.4):

- Install: Software opens the `.flatpakref` (and takes `flatpak+https://`), and warns before installing that "Dens spike (Ed25519) is potentially unsafe": Legacy Windowing System, for `fallback-x11`, and Unknown License. It asks for the admin password, since it installs for the whole system. The Ed25519 key works. Installed, the app isn't in Software's Installed list: the build had no AppStream data.
- Launching, `spike auto` and the secret: as on Kubuntu, with no prompt; the same digest across calls, restarts and sign-ins.
- Background Apps lists Dens Spike, and `SetStatus` shows under it at once. Its close button sent `org.freedesktop.Application.ActivateAction("quit")`, and the service stopped cleanly.
- Sign-out: SIGTERM; at sign-in the service starts, and the Secret portal was there 1.1 s and 2.3 s after it.
- Automatic login leaves the login keyring locked: "The login keyring did not get unlocked when you logged into your computer". The service's request waited on that prompt for minutes; a second request, made while the first still waited, succeeded once the keyring was unlocked, and the first then failed. Cancelled, a request fails (response 2); unlocked, it gives the same digest.
- Shortcuts: version 1. The first bind shows GNOME's dialog; later binds don't, `ConfigureShortcuts` is "not implemented", and the keys change only in Settings, Apps, Dens Spike. GNOME repeats `Activated` about every 30 ms while a key is held.
- Under Wayland, `DISPLAY` is empty: Flatpak 1.18 gives no X11 to `fallback-x11` there, where Kubuntu's 1.16 does.

**Fedora 45 Workstation** (GNOME Shell 51.1, Software 51.0, gnome-keyring 51.1 and oo7 0.7.0 beta, xdg-desktop-portal 1.22.1 with -gnome 51.0, Flatpak 1.18.4):

- Install, launching, Background Apps and `SetStatus`: as on Fedora 44.
- The Secret portal goes to oo7 first (`gnome-portals.conf`: `Secret=oo7-portal;gnome-keyring;`). The digest stays the same across calls, restarts and sign-ins. After an automatic login its prompt reads "An application wants access to the keyring “Login”, but it is locked"; the request waited 27 s for the password and gave the same digest. Cancelled, it fails (response 2). At sign-in the portal was there 1.2 s and 4.5 s after the service started.
- Shortcuts: version 2. `ConfigureShortcuts` opens GNOME's dialog again, with the current keys to change.
- A service started from a terminal stopped with SIGHUP when the terminal went at sign-out; one started from the menu or at sign-in stops with SIGTERM.
- After an automatic login, the dash flashed twice as the autostart entry ran.

**Fedora 44 KDE** (Plasma, Discover and xdg-desktop-portal-kde 6.7.5, Frameworks 6.31, xdg-desktop-portal 1.22.1, Flatpak 1.18.4, Plasma Login Manager):

- Install: Discover asks for the admin password, and otherwise as on Kubuntu, with no word about X11.
- The secret: no prompt in sessions signed in with the password; the same digest throughout. After an automatic login the wallet asked, and the request waited 79 s for the password. KDE's own settings warn, as automatic login is turned on, that it can't unlock the wallet and will ask at every sign-in.
- At sign-in the Secret portal was there 0.1 to 0.3 s after the service started.
- Shortcuts: version 2; `ConfigureShortcuts` opens System Settings at Dens Spike's shortcuts. Under X11 (Plasma's X11 session works here) the portal still binds and reports presses, with real timestamps where Wayland's are 0, and Dens's X11 key reader in the sandbox reads F8 too.
- Under Wayland, `DISPLAY` is empty, as on the other Fedoras.

**Updates** (Part 2):

- Fedora 44 GNOME: the monitor reported v2 (`remote-commit`), and Software listed the update once refreshed. `spike update` failed: "org.freedesktop.DBus.Error.AccessDenied: Only the focused app is allowed to show a system access dialog". GNOME shows the dialog asking whether an app may update itself only for the app in front, which a service with no window never is, so on GNOME Dens can't install its own update; it can tell the member and restart once Software has. With AppStream data in v2, Software lists Dens Spike in Installed, with its MIT license and website. Its empty `content_rating` made Software call it "suitable for everyone", with no "uncontrolled chat functionality": Dens's metainfo has to declare its chat and voice in OARS terms.
- Fedora 44 GNOME, step 6: v2 never installed, since the self-update can't ask and Software, which listed it, didn't install it by itself in the two hours before v3 came; the monitor, polling every 30 minutes, reported v2 and then v3. Every `spike update` got the same refusal. `flatpak update` asked about the new permission and went straight from v1 to v3, while the service kept running v1. v3, given Wayland, has `WAYLAND_DISPLAY=wayland-0` and still no `DISPLAY`.
- Fedora 44 KDE: the monitor didn't report v2 in time, though Discover listed it. `spike update` showed KDE's dialog, which the member allowed, then installed v2 and restarted into it, 2.4 s from the install's end to the new service. Discover shows the same AppStream data as Software, shorter.
- Fedora 44 KDE, step 6: `spike update` was refused for v3: "Self update not supported, new version requires new permissions". `flatpak update` asked about Wayland and installed it. The next `spike update` came before the monitor's next poll had noticed, so the monitor still took v2 for the newest, and Update answered "nothing to update" (Progress status 1). Dens should take that answer as a cue to restart into the newest build installed. Under Wayland v3 has `WAYLAND_DISPLAY=wayland-0`, and neither `DISPLAY` nor `XAUTHORITY`.
- Fedora 44 KDE, step 7, with Frameworks 6.31: after an automatic login, both cancelled prompts failed (response 2, after 31 s and 1.4 s). The fix holds.
