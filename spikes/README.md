# Spikes

Throwaway experiments that answer a design question before the code that depends on it. Each is a directory in this Go module, whose dependencies never reach the product, and each goes once its findings are in `docs/dev/design.md`.

## The global shortcuts portal on Linux (M7.2)

Under Wayland the helper can't read keys itself: it asks the desktop, through the global shortcuts portal, to bind the three voice actions, and the desktop reports each press and release (see "Global shortcuts" in `docs/dev/design.md`). This spike drives the portal as the helper will, on GNOME and KDE, before the helper depends on it:

- Does the portal take an unsandboxed app that registers `chat.dens.Dens` (`org.freedesktop.host.portal.Registry`) with a `.desktop` file installed for it, and refuse one that doesn't?
- What does each desktop show when the app binds its shortcuts: a dialog, what it asks, whether it takes the preferred triggers, and whether a single key such as F8 can be one?
- Do presses and releases (`Activated`, `Deactivated`) arrive with any window in front, a game in full screen included, and how soon?
- Does the desktop remember the keys across sessions and logins, so it asks only once?
- Does `ConfigureShortcuts` (version 2) open the desktop's settings for them?

### How

- **`portal`** writes `~/.local/share/applications/chat.dens.Dens.desktop`, registers the app ID, reads the portal's version, creates a session, lists the shortcuts already bound, binds push to talk, toggle mute and push to mute (preferring F8, F9 and F10), and logs every press and release with how long it was held, until Ctrl+C. It logs to the console and to `portal-<desktop>.log` beside the binary, which from VMware's shared folder reaches the host.
- **`fake`** stands in for the portal on a private session bus, so the spike's calls can be checked without a desktop.

```sh
(cd spikes && CGO_ENABLED=0 go build -o ../out/spikes/portal/dens-portal-spike ./portal)
dbus-run-session -- sh -c 'fake & sleep 0.5; timeout -s INT 4 dens-portal-spike -no-desktop-file'
```

### Results

Fedora 44 Workstation (GNOME 50) and Fedora 44 KDE Plasma Desktop, under Wayland, in VMware Workstation Pro on Windows 11.

- **The app ID:** both portals took `Register("chat.dens.Dens")`, with the `.desktop` file in `~/.local/share/applications`. KDE's System Settings list the shortcuts under the file's name and icon.
- **The first bind** shows the desktop's dialog with the three actions and the preferred triggers, each one changeable.
  - GNOME's is "Add Keyboard Shortcuts" (Cancel or Add). It says the shortcuts work globally and can be changed on the app's page in Settings.
  - KDE's is "Global Shortcuts Requested", "Dens wants to register the following 3 shortcuts" (OK or Cancel), with a warning beside a trigger that clashes.
  - Single keys such as F8 are taken as triggers. The answer names each one (GNOME "Press F8", KDE "F8").
- **Remembered:** later sessions, and sessions after signing out and restarting, bind without a dialog in 5 to 104 ms.
  - KDE's `ListShortcuts` reports the bound shortcuts before a session binds them. GNOME's reports none until it does.
- **Presses** reach the session with any window in front, SuperTuxKart full screen included.
  - KDE sends one `Activated` and one `Deactivated` per press, with a timestamp of 0.
  - GNOME repeats `Activated` about every 30 ms while a key is held, after the keyboard's repeat delay, then sends one `Deactivated`, with timestamps in milliseconds. The helper takes the repeats as one press.
- **`ConfigureShortcuts`:**
  - GNOME's portal is version 1, and calling it fails as not implemented. Its keys change in GNOME Settings, on the app's page.
  - KDE's is version 2, and the call opened System Settings at Dens's shortcuts.
- **Media keys:** a keyboard whose top row sends media keys unless Fn is held never sends F8, so a binding to F8 never fires. GNOME shows its "unavailable" icon for the media key, which has nothing to act on.
