# The Android spike (M12)

Before M12 builds the Android form, this spike answers what the plans of M12 to M14 lean on, on the phones Dens has to work on: a Galaxy S24 Ultra and a Pixel 9 Pro, which should both have Android 17 by now (One UI 9 reached the S24 in October), and Android's emulator here. [android.md](../../docs/dev/android.md) has the design it tests.

**Dens Spike** (`chat.dens.spike`) is the shell M12 will build, cut down: Kotlin with no AndroidX, built without Gradle, for Android 17 (API 37) down to Android 12 (API 31), signed with a throwaway key. It never takes `chat.dens.Dens`: a package's first install fixes the key every update of it must carry. It carries two binaries in `lib/<abi>/`, which Android unpacks where an app may run them:

- `libdens.so`, this tree's Dens built for Android with cgo through the NDK. It's a development build, since the Android form doesn't exist yet: the real client and page, running as a development instance in the app's files, with its key in a plain file.
- `libprobe.so`, `aprobe`, a probe in `spikes/` that runs Dens's own seams (locked memory, low-priority children, the control endpoint) and the system's (DNS, interfaces, TLS, the local network, the system calls Android's filter allows, each in a child of its own); writes a heartbeat, a line a second on two clocks, whose gaps show when Android froze, slept or killed it; runs busy low-priority children, as media workers would be; and serves a page of the WebView's features, with Dens's own voice worklet and RNNoise's module. Media goes through the real client, from the page.

The shell does what M12's will. It runs Dens under a foreground service and starts it again when it dies; gets a pairing link from it and loads the page in a WebView, with no JavaScript bridge, sending every address but the page's to the phone's browser; grants the page the microphone, starting a microphone service as it does; wraps a key with the Keystore and hands it to the probe on its standard input; posts notifications; picks files for the page and saves its downloads; routes a call's sound; and installs an update of itself. A screen of its own shows the probe's report and the shell's, with a button for each test, and shares the report as text. Its WebView can be debugged, so the browser checks' driver reaches the page over adb, as it reaches Chromium.

## The questions

*The build (M12)*

1. **Dens for Android.** This tree with cgo through NDK r30, for arm64 and x86_64: does all of it build, at what size, with segments aligned for 16 KB pages? And the APK without Gradle, from aapt2, kotlinc, d8, zipalign and apksigner: what each costs to pin, how long a build takes, and how large the APK is with the binary compressed in it.

*The service under the shell (M12)*

2. **The child.** Running from the native library directory at API 37; the media workers it starts from its own binary; the control socket in the app's files, with `SO_PEERCRED`; names looked up through Android's resolver, with Private DNS on; `net.Interfaces`; locked memory past 64 KiB, with 4 KB and 16 KB pages; nice 19 and `SCHED_IDLE` for workers; `flock`; the time zone, the host name and `os.TempDir`; and any system call Android's filter kills the process for.
3. **Its lifetime.** The child with the app on screen; in the background with no foreground service, and with one of type `specialUse` or `remoteMessaging`; swiped from recents, force-stopped, and short of memory; across the app's update and a reboot; its `oom_score_adj` against the app's; and Samsung's sleeping apps.
4. **The key.** A Keystore AES key, in StrongBox where the phone has one, wrapping 32 bytes: how long the unwrap takes at start, whether it works while the phone is locked, and the bytes handed over standard input.

*The page in the WebView (M12, M13)*

5. **The page.** Pairing from the shell's link, the session's cookie across restarts, and the page's CSP, `Permissions-Policy`, socket, and `Host` and `Origin` checks as the WebView sends them.
6. **What the WebView has:** `RTCPeerConnection`, `getUserMedia`, `AudioWorklet` and RNNoise's module, `getDisplayMedia`, `setSinkId` on media elements and on `AudioContext`, `Notification`, `window.open`, full screen, the clipboard, and WebCodecs: decoding the phones' H.264 and HEVC, and encoding AV1, in hardware or not.
7. **What the shell must do:** files picked into uploads (a photo, a video, any file); downloads with the session's cookie (an attachment, and a backup's link); full-screen video; links opening in the browser while YouTube's player plays in its frame; the back button; the keyboard, and drawing edge to edge. And a screenshot of each screen at the phone's width, which sizes M13.

*Calls (M12, M14)*

8. **A call** between a phone and a desktop, through a den behind Caddy: the microphone's permissions, noise suppression and the gate, sound both ways, and echo on the loudspeaker; where the sound plays (earpiece, loudspeaker or Bluetooth), and whether the shell's `setCommunicationDevice` moves it; and a desktop's share, watched on the phone.
9. **A call off screen:** with the microphone service started as the page asks for the microphone, ten minutes with the screen off, the app in the background, a phone call coming in, and a move from Wi-Fi to mobile data; and the CPU and battery the ten minutes took.

*Media (M12)*

10. **Photos and videos.** The real client stripping and copying the phones' photos at 12, 50 and 200 megapixels, and the Pixel's Ultra HDR ones: how long each takes, and whether a 200-megapixel photo's copy fits the module's 768 MiB, which holds a 48-megapixel one, as it decodes the photo whole. And a phone video's copy, made by the module with one, two and four workers, and by WebCodecs in the WebView, with the app on screen and in the background: how long, how hot, and whether Android kills workers as phantom processes.

*Notifications, installing and staying connected (M12, M14)*

11. **Notifications** posted while the app is in the background, after `POST_NOTIFICATIONS`, whose tap opens the page at a channel.
12. **Installing and updating.** The APK installed from the browser on each phone, past Play Protect and Samsung's Auto Blocker; and the app updating itself through `PackageInstaller` with no prompt, with the service it runs meanwhile.
13. **Overnight**, if the S24 Ultra can spare a night: the foreground service holding a den's socket for eight hours with the screen off, how soon a DM arrives at the end of it, and the battery it took. And Android 17's local network permission, for a den at an address on the LAN.

Not in the spike: Dens's F-Droid repository, UnifiedPush, the updater's download and its words, Google's developer verification (enforced for sideloads from 2027), the phone layout's fixes, and 32-bit or x86 phones.

## How it runs

In three places, in order:

1. **Here**, in an Incus container, `dens-android-spike` (Debian 13), with `/dev/kvm`: the SDK's pieces, kotlinc and the emulator, each pinned by its URL and SHA-256 in `box.sh`, and Debian's JDK 21; the NDK is pinned in `build.sh`, here, where the Go builds run. vendor.sh takes the pins over in M12. That's about 7 GB of downloads, most of them system images. A development den in `dens-browsers` hosts, behind Caddy. The emulator answers what needs no real phone: 1, 2 and 3 as stock Android does them, 4 in software, 5 to 7, and 8 with the speech the browser checks call with, on Android 17 with 4 KB and 16 KB pages and on Android 12, the oldest the spike runs on.
2. **The S24 Ultra**, the long run: everything again on real hardware, and what the emulator can't do (a real microphone and speaker, Bluetooth, the screen off, mobile data, the 200-megapixel camera, Samsung's battery rules), in one or two sessions of about two hours, on the user's den behind Caddy, with their desktop in the calls.
3. **The Pixel 9 Pro**, in about an hour: installing, the report, a call, a 50-megapixel photo, WebCodecs on Tensor's encoder, and StrongBox on its Titan M2.

On the S24 Ultra, wireless debugging paired with the container's adb lets the spike install builds, read the app's logs and drive the page from here, so the user's part is what takes hands: talking, the screen, leaving Wi-Fi. adb has the shell's rights on the phone, shared storage included, so the spike keeps to its own app's processes, files and logs, and the user turns wireless debugging off whenever they like. Without it, each step is the user's, by hand, sending back the report and logs.

## Building it

`spikes/android/build.sh` makes everything: the container and its SDK, then Dens and the probe for both ABIs here, then the APKs in the container. `build.sh binaries` and `build.sh apk CODE` redo a part; CODE is the version code, which an update must raise.

- **The container**, `dens-android-spike`, from `dens-e2e/debian-13`, with `/dev/kvm` passed in, Debian's JDK 21 and `box.sh`, which runs inside it: `box.sh sdk` fetches each piece by its pinned SHA-256 (Google publishes only SHA-1s, which each matched) and lays out an SDK under `/opt/android/sdk`; `box.sh avd NAME IMAGE` makes an emulator's device; `box.sh apk ABI CODE` builds an APK.
- **The binaries**, here: NDK r30 in `tools/android-ndk-r30` (fetched by the repo's `download_verified`), the page's assets as `scripts/build.sh` makes them, and `go build` with `GOOS=android CGO_ENABLED=1`, the NDK's clang for API 31, Go 1.26.7 as CI has it, and a development build's flags with `-s -w -checklinkname=0`. Dens is `lib/ABI/libdens.so`, the probe `lib/ABI/libprobe.so`.
- **The APK**, in the container, each step timed: `aapt2` compiles the resources and links them with the manifest (the code names no resource, so there's no R class); `kotlinc` compiles `app/src` against the platform's `android.jar`; `d8` makes the dex with Kotlin's standard library; `zip` adds the dex and the binaries; then `zipalign` and `apksigner`, with a throwaway P-256 key that `out/android-spike/keys` keeps. `apicheck.py` lists every framework call newer than API 31, from the SDK's `api-versions.xml`, as Android Lint's NewApi check would: a build without Gradle has no Lint, and it found two calls that crash Android 12.

Beside them:

| Script | Does |
|--------|------|
| `phone.sh SERIAL ...` | A device, the emulator or a phone, through the container's adb, keeping to the spike's own files (`run-as`), log lines (by its UID) and screen: install, the den's root, joining, pressing a button, pulling the reports, pairing over wireless debugging |
| `drive.mjs` | The WebView over the DevTools protocol, which adb forwards: `eval`, `api` (the page's API, called from the page), `shot`, `go`, `tap`, `type` |
| `den.sh up` | The emulator's den: a development den in `dens-browsers` behind the pinned Caddy, with its internal authority, at that container's address, owned by `desk` on page port 18584 |
| `call.mjs desk\|phone SECONDS` | A call: Chromium as `desk` in `dens-browsers`, and the WebView, each with a fake microphone that speaks, saying what it sends and hears every 5 seconds |
| `media.mjs SERIAL FILE...` | Photos and videos attached to the page's composer, timed until each chip settles; `watch` notes the chips as the member attaches files by hand |
| `bg.sh SERIAL [TRIAL...]` | The screen-off trials: `bg.html`, a call inside one page (a tone through one peer connection to another, played as Dens plays a voice, with the microphone open), with the screen off for 110 s, not held, held, and held with its audio contexts made after the microphone; the page's lines each second and the spike's audio players as Android sees them |

The spike's screen takes `adb shell am start -n chat.dens.spike/.ReportActivity --es do BUTTON`, where BUTTON is a button's label in lowercase with dashes (`run-the-report`), and writes what it shows to `files/spike/last.txt`. `--es page URL` opens the page at URL instead, passing on `--ez hold` (hold the page as shown during calls, kept as set) and `--ez lock` (show it over the lock screen): the page's activity isn't exported, so adb can't start it itself. Its children take more environment from `files/spike/env`, a `KEY=VALUE` a line: the emulator's Dens gets `SSL_CERT_FILE` for its den's authority there.

## On the phones

**Before the S24 Ultra's sessions**, on the phone:

1. Its photos and downloads go to the PC, but for a few it's fine to share: adb reads shared storage, though the spike keeps to its own files.
2. Developer options on: Settings, About phone, Software information, and tap Build number seven times.
3. Note whether Auto Blocker is on (Settings, Security and privacy, Auto Blocker): question 12 installs with it as it is first.
4. An invite to peridot for the phone, from the desktop's Dens.

**Pairing**, at the start of a session: Settings, Developer options, Wireless debugging, on; then Pair device with pairing code, which shows an address and six digits. `phone.sh ADDRESS pair DIGITS` pairs the container's adb, and `phone.sh ADDRESS connect` connects to the address Wireless debugging shows above it, which is the phone's serial from then on. Android drops the connection when the phone leaves the network, and turning Wireless debugging off ends it.

The sessions' steps, with what each answers:

| Step | Who | Answers |
|------|-----|---------|
| Install from the phone's browser, served through `adb reverse` | User | 12 |
| Open the spike, allow notifications, run the report | User, then here | 2, 4 |
| The page: pair, set the local password, join peridot | User | 5 |
| The feature page, with real taps | User | 6 |
| A photo, a video and a file uploaded; an attachment and a backup downloaded; a video full screen; a link out; a YouTube player; back; the keyboard | User | 7, 10 |
| A call with the desktop: talking both ways, the loudspeaker's echo, the earpiece, Bluetooth | Both | 8 |
| The call off screen ten minutes, a phone call coming in, Wi-Fi to mobile data | User | 9 |
| The background, swiped from recents, Samsung's sleeping apps, a reboot | Here, then user | 3 |
| A notification in the background, and its tap | User | 11 |
| The spike updating itself | Here | 12 |
| Overnight on peridot, a DM at the end | User | 13 |

**The Pixel 9 Pro** is a friend's, so it goes by hand unless they want wireless debugging: install from the browser, run the report and the feature page, a call, a 50-megapixel photo, and the report shared back with the Share button.

## Results

### The emulator (2026-10-26)

On Android 17 (`CE2A.260420.051.A3`, WebView 145) with 4 KB and 16 KB pages, and on Android 12 (`SE1A.220826.008`, WebView 91, the image's own), each on four of the host's cores. What's left for the phones is at each question's end.

**1. The build.** The whole tree builds with cgo through NDK r30 for arm64 and x86_64, `-checklinkname=0` its only flag beyond a release's: 41 seconds each cold, 15 for both warm. The NDK aligns every segment at 16 KB, and the binaries run with 16 KB pages; they need Android's own `liblog`, `libdl` and `libc`. A release build is 52.0 MB for arm64; the spike's, a development build stripped as a release is, is 50.8 MB, and the probe 9 MB. The APK builds in about 11 seconds an ABI without Gradle (kotlinc 6 s, d8 3 s, zip 2 s, aapt2, zipalign and apksigner under 1 s), and is 26.2 MB for arm64, 28.9 for x86_64. The dex is 2.3 MB, 0.7 compressed: Kotlin's whole standard library, which R8 would shrink. What it pins: build-tools 63 MiB, the platform 64, platform-tools 9, the emulator 333, Kotlin 86, the NDK 704, each system image about 2 GB, and Debian's JDK. Without Gradle there's no Android Lint, and `apicheck.py`, from the SDK's own API database, found two calls that crashed Android 12, now guarded.

**2. The child.** It runs from the native library directory as `untrusted_app`, under Android's seccomp filter; the same binary copied into the app's files can't run (`permission denied`).

- System calls: on Android 17 the filter kills only `rseq` and `io_uring_setup`, which Go doesn't make. Android 12's also kills `faccessat2`, `openat2`, `close_range`, `clone3`, `epoll_pwait2` and `landlock_create_ruleset`. Go's standard library already avoids `faccessat2` on Android (`syscall.Faccessat`, `unix.Eaccess`) and makes `clone3` only for cgroups and time namespaces, and none of Dens's dependencies make the others: a trap for later code, such as `unix.Faccessat` with flags or `unix.CloseRange`, which the probe's list would catch.
- `StartLowPriority` works on both: its child runs at nice 19 under `SCHED_IDLE`.
- Locked memory: `host.AllocLocked` locks 16 pages (64 KiB) with 4 KB pages, **4 with 16 KB pages**, and 2,048 (8 MiB) on Android 12, then fails with ENOMEM. A member of two dens needs five, so M12 needs the fallback android.md describes.
- The control socket works in the app's files (a 50-byte path), and `SO_PEERCRED` names the app's UID.
- Names resolve through bionic (cgo). Go's own resolver fails, with no `resolv.conf` to read. TLS verifies against the system's certificates (`/system/etc/security/cacerts`, 145).
- `net.Interfaces` is refused (netlink's `bind` denied); `wlynxg/anet`, which Pion uses, lists the interfaces and addresses.
- Go's local zone is UTC unless `TZ` names one, which the shell passes; `tzdata` loads from the time zone APEX. `os.TempDir` is `TMPDIR`, which the shell sets to its cache; `os.Hostname` is `localhost`.
- SELinux logs a denial as Go reads `/proc/sys/net/core/somaxconn` on listen; Go falls back.

**3. Its lifetime**, on stock Android 17:

| Case | What happened |
|------|---------------|
| On screen | The app and its children at `oom_score_adj` 0 |
| Background, a `specialUse` or `remoteMessaging` service | The app at 50, then 200 after 90 s; never frozen; two minutes of forced Doze left no gap in the heartbeat, and the den's socket stayed up |
| Background, a plain started service | Android stopped the service 74 s after the app left the screen, which ended the children; the app was cached (900) and frozen at 2.5 min, and the freezer takes the app's whole cgroup, children too |
| Swiped from recents | Nothing changed |
| Force-stopped | Everything ended at once, with SIGKILL, and nothing restarts until the app is opened |
| The app's process killed, as the low-memory killer would | The children died with it; Android restarted the sticky service in a second, and Dens was back in 8 s. Android 12 stopped the restarted service "due to app idle" instead |
| Updated | `MY_PACKAGE_REPLACED` restarted the service from the background, Dens running again in 30 ms |
| Rebooted | `BOOT_COMPLETED` started the service, and Dens reached its den 7.5 s after boot |

The children keep the `oom_score_adj` they were started with, 0 in front or 200 behind, while the app's moves with its state: the low-memory killer picks the app, and its process group goes with it. The phones add a CPU that sleeps, Samsung's rules, and a night.

**4. The key**, in the emulator's software Keystore (no StrongBox, no TEE): made in 6 ms, unwrapped in under 5, and the 32 bytes reach the probe whole over standard input. StrongBox, the TEE and a locked phone are the phones'.

**5. The page.** It pairs from the link `dens open --print` gives the shell; its session's cookie outlives restarts and updates; `Host` and `Origin` pass as the WebView sends them, and the page's API answers from inside the WebView. The WebView doesn't know `usb` in `Permissions-Policy`, a warning. WebView 91 doesn't know `'wasm-unsafe-eval'`, so the CSP blocks RNNoise there: M12 needs a floor for the WebView's version. The WebView says `Android 10; K; wv` in its user agent. The shell logs a console message's source by file name alone, since during pairing the page's address carries its token.

**6. What the WebView has** (145): `RTCPeerConnection` with Opus and RED; `getUserMedia`, with echo cancellation and gain control; `AudioWorklet`, where Dens's worklet runs RNNoise (25 ms to compile, a report every 50 ms) in a context that runs without a tap; `setSinkId` on `AudioContext` but not on media elements; full screen; a wake lock; IndexedDB and 4.7 GB of storage. Missing: `getDisplayMedia` (no sharing from the phone), `Notification`, `navigator.share` and `selectAudioOutput`. `window.open` opens no window: it tries to navigate the page, which the shell sends to the browser. The Clipboard API is refused, reading and writing, even after a tap; `document.execCommand('copy')` works, so the page's copy button needs it as a fallback. WebCodecs: AV1 encodes in software (libaom), 102 fps at 720p here; H.264 and VP9 decode; HEVC decodes only in hardware, and Main 10 not at all here. The page is 411 CSS pixels wide, with a coarse pointer and no hover.

**7. What the shell must do**, each done:

- Files: the page's file input opens Android's picker through `onShowFileChooser`, and the WebView reads the `content:` address it returns, with content access off.
- Downloads: the WebView hands the shell a link with no name and a generic type. The service names the file in `Content-Disposition` and sends every download as `application/octet-stream`, so the shell guesses the name with no type, which would swap the extension for `.bin`, fetches it with the page's cookie, and saves it to Downloads through MediaStore.
- Links leave for the browser; YouTube's player plays in its sandboxed frame, once the member turns players on.
- Full screen comes through `onShowCustomView`, with the bars hidden, and Back leaves it, after Android's one-time hint, which takes the first Back.
- Back walks the page's history and then leaves the page; on a phone it should mean "up", from a channel to its den, which is M13's to design.
- The keyboard: the WebView shrinks above it, the composer with it, as the shell applies the IME's insets itself; Android draws every app edge to edge. Enter sends, where a phone's keyboard means a new line.

The screenshots of each screen at the phone's width are the phones' to take, at their sizes.

**8. A call**, a fake microphone speaking on each side: the WebView and Chromium hear each other, about 50 packets a second each way, none lost. The WebView's media goes straight to the den's media port, not through the phone's Dens. The page's `getUserMedia` reaches the shell's `onPermissionRequest`, which adds the microphone type to its service and grants it; ten seconds after Android's recording callback says the page stopped, the shell drops the type. The real microphone, the speaker, echo and routing are the phones'.

**9. Off screen:** with the microphone type added, the desktop heard the phone's speech at its levels for 140 s after the app left the screen. The screen off, a phone call and mobile data are the phones'.

**10. Media.** Photos at 12, 50 and 200 megapixels, made by FFmpeg: each had its copy in 1 to 3 seconds. The module decodes a JPEG into YUV, 1.5 bytes a pixel at 4:2:0, so a 200-megapixel photo takes about 300 MB and fits the 768 MiB; Go's preview limit (256 MiB) gives the original no preview. Videos: the page offered to copy each, and the emulator's WebView couldn't decode them (H.264 failed, HEVC Main 10 unsupported), so the module made the copies: an iPhone X's H.264 clip in 9 s, an iPhone 12 Pro's HEVC at 1080p60 (35 MB) in 83 s, a 60 fps clip in 3 s. Four low-priority children spun two minutes in the background beside the service, each getting 118 s of CPU; Android tracked them as phantom processes and killed none. The phones' own photos and videos, and WebCodecs on their hardware, are the phones'.

**11. Notifications** post from the background, and a tap opens the page at the notification's address.

**12. Updating itself:** through `PackageInstaller`, with `USER_ACTION_NOT_REQUIRED` and `UPDATE_PACKAGES_WITHOUT_USER_ACTION`, the spike installed its next version with no prompt in about a second; it had been installed by adb, with no installer of record, and is its own afterwards. `MY_PACKAGE_REPLACED` started its service again 40 ms later. An install from the browser, Play Protect and Auto Blocker are the phones'.

**13. The local network.** Android 17's permission blocks the phone's own subnet alone: TCP there times out after 5 s, and UDP fails at once with EPERM. A den at a private address beyond the router, as here, joins without it. A den on the phone's own Wi-Fi needs the permission, for the client's connection and for the WebView's media, and without it the page would show a timeout. The night is the S24's.

### The S24 Ultra (2026-10-26 and 27)

A Galaxy S24 Ultra (SM-S928U, One UI 9 on Android 17, patch 2026-09-05, Snapdragon 8 Gen 3, 4 KB pages, WebView 155), with power saving on as its owner keeps it: two sessions over wireless debugging from the container, a night and a three-hour run, on peridot behind Caddy, with the owner's Windows desktop in the calls. Spike versions 10 to 12; version 12 holds the page during a call (9).

**2. The child**, as on the emulator's Android 17: the same two system calls killed, the copy in the app's files refused, nice 19 under `SCHED_IDLE`, the control socket and `SO_PEERCRED`, interfaces through `anet`, the zone from `TZ`. Samsung raises `RLIMIT_MEMLOCK` to 64 MiB, so `AllocLocked` locks all a member needs; the Pixel keeps Android's 64 KiB. With Private DNS set to `dns.google`, names resolve through bionic over TLS, in 191 ms against 67 on Automatic, and Dens reaches its den. Go's own resolver fails as on the emulator.

**3. Its lifetime.** Samsung's defaults: battery Optimized, and "Put unused apps to sleep" on, with the spike in none of its lists.

| Case | What happened |
|------|---------------|
| Swiped from recents | Nothing changed |
| A night and the day after it, the screen off | No restarts in 14 hours |
| Battery Restricted | The app's network went as soon as it was in the background (the den's ping went unanswered, and names stopped resolving); Android stopped the service 40 s later and froze the app, which ran the service's `onDestroy` only when it thawed. Opening the app started the service again, and leaving stopped it again |
| Deep sleeping apps, or leaving Restricted just before | Android killed the whole app, and nothing started it until it was opened |
| Updated by adb | `MY_PACKAGE_REPLACED` started the service again, as on the emulator |

A phone that sleeps adds what the emulator couldn't show: Go's timers run on `CLOCK_MONOTONIC`, which stops while the CPU is suspended (13).

**4. The key**, in StrongBox: made in 46 ms, unwrapped in 50, and in 94 with the phone locked.

**5. The page** pairs, takes its local password and joins peridot by hand, and its session outlives restarts and updates.

**6. What the WebView has** (155): what the emulator's had, with the hardware's codecs. H.264, HEVC (Main and Main 10), AV1 and VP9 decode in hardware; HEVC encodes in hardware, H.264 in hardware at 720p but not at 1080p, and AV1 and VP9 only in software (AV1 at 107 fps at 720p). The Clipboard API is refused here too, so the page's copy buttons fail without a word.

**7. What the shell must do.** Picking files for uploads works, several at once too. For M13, the page on the phone: no swiping between its panels; no send button; the recovery codes' checkbox and the settings' tabs overflow; settings only a desktop needs show, and the copy says "this computer"; the comparison's hint says to hold Space; a copy's Full size button is cut off at the edge; and the viewer's gallery moves only by its arrows.

**8. A call** with the desktop through peridot: no loss, jitter 2 to 11 ms, a round trip of 5 to 7 ms, the media over UDP to peridot's public address. Echo cancellation held on the loudspeaker; the phone's speech is a little rough as it starts and stops. Where the sound goes:

- Chromium puts the audio mode in communication and the communication device on the loudspeaker as the page opens the microphone.
- The shell's `setCommunicationDevice` moved Android's route each time, as its log shows, but the call's sound stayed where it was. The page's audio played as media (`USAGE_MEDIA`), from both speakers, under the media volume rather than the call's, and never reached Bluetooth headphones, which Android keeps media off during a call.
- A stream's usage is fixed as Chromium opens it: an audio context the page makes before the microphone opens plays as media, one made after it as voice communication (bg.html's trials). Dens makes its meters' and its voice processor's contexts in the joining click, before the microphone, so the browser lets them start. bg.html's tone, which an audio element played as Dens plays a member's voice, showed as media throughout, so the voices need a context made after the microphone too.

**9. Off screen**, the call broke a minute after the page was hidden, and version 12's shell keeps it going:

- Chromium freezes a hidden page a minute after its sound stops (`kStopInBackground` in Blink's page scheduler). The minute starts from the later of the page hiding and its sound stopping, which Chromium registers about 30 s after the last of it.
- Freezing suspends the page's audio contexts, so the microphone's voice stops going out, and pauses its audio elements, which nothing plays again when the page thaws.
- In the owner's call, going Home while the desktop was quiet froze the page at 16:48:27, and from then on the phone played nothing. Turning the screen off later froze it again exactly 60 s after it hid, which stopped what it sent.
- Held, the page never froze. Version 12's shell tells the WebView its window shows while the page has the microphone (`onWindowVisibilityChanged`, passed `VISIBLE`). bg.html's trials kept sending, receiving and playing through 96 s of silence with the screen off, and a call with the desktop went five and a half minutes off screen, 100 s of it quiet, with its statistics arriving every 10 s and each side hearing the other. Not held, bg.html's page froze 90 s after its tone stopped, and came back with its audio element paused.
- Thawed or not, the page should play its audio elements again on `resume`.
- From Wi-Fi to mobile data, the call came back in 2 s: the den had dropped the page from it, so it joined again. Back to Wi-Fi, it resumed with no gap. A phone call coming in wasn't tried.
- In the call, the WebView's renderer took about half a core and the app's process about a fifth.

**10. Photos and videos**, each with the right colours, rotation and sound on the desktop:

| File | Its smaller copy |
|------|------------------|
| Photos at 12, 50 and 200 megapixels | 390, 319 and 303 KB, in 1 to 6 s. The 200-megapixel one's full size, 17 MB, gets no preview but a download button |
| 50 s at the camera's default, 1080p | The page's copy, 16 MB, in 38 s |
| 22 s of 8K at 30 fps, about 200 MB | The page's copy, 7.8 MB, in 1 min 55 s. The full size uploaded in about 6 s |
| The same, with the app sent Home during the copy | The page's copy stopped as the page hid. Dens took it over 30 s later ("the page went quiet"), and the module finished in 6 min 20 s, at 3.7 MB |

The module's four workers ran behind the service for those six minutes, and Android killed none. Copies with one or two workers need a knob the module doesn't have.

**11. Notifications** post from the background, and a tap opens the page.

**12. Installing and updating.** From Chrome: "File might be harmful", the switch that lets Chrome install apps, then Play Protect's "App scan recommended", whose scan said "Harmful app blocked". Cut-down builds, each Dens alone under a package name of its own and not debuggable, found why:

| Build | Play Protect |
|-------|--------------|
| Neither permission to install | Safe |
| `REQUEST_INSTALL_PACKAGES` and `UPDATE_PACKAGES_WITHOUT_USER_ACTION` | Harmful |
| `REQUEST_INSTALL_PACKAGES` alone | Safe |

So M12's updates go through Android's installer, a tap each. The spike updating itself, with the permission still in it, installed without a prompt from Android, but Play Protect scanned the new version, called it harmful, and installed it after Install anyway. The spike was its own installer of record after that. Auto Blocker was off, as on phones that shipped before One UI 6.1.1: adb and the browser's install both worked. Neither phone asked for Google's advanced flow, which the US doesn't enforce before 2027, and adb installed later builds without a word.

**13. Overnight, and a phone that sleeps.**

- The first night tested Doze but not a sleeping CPU. The owner streamed their desktop's sound through the phone to Bluetooth headphones until the morning, which held a wakelock. Through the idle day after, the phone slept 41% of the time, in naps of a second, kept up by alarms Android named only as `[timerfd]` (another app's, or wireless debugging's; it slept once both were off). Doze held 82% of the night, the den's socket stayed up through it, and a DM sent the next afternoon arrived over it.
- A second run, three hours on a desk with Wireless debugging off: the phone slept 94.9% of the time, waking 2.3 times a minute, mostly for the den's ping every 30 s, for a median sleep of 26 s. The whole phone drew 27 mAh an hour, 0.55% of its battery, always-on display included. The den's pings come to about 1% a night by estimate: 140 wakes an hour of about a second each. The socket held.
- With Go's timers on `CLOCK_MONOTONIC`, the session's renewal, due ten minutes before its hour is up, came late. The den closed the session with 4001 every hour, and Dens signed in again.
- After each close, Dens's wait of up to a second before reconnecting couldn't end until something else woke the phone, since nothing arrives on a closed socket. Dens was offline for 6 min 27 s after one close and 4 min 14 s after the next, and for 0.7 s after a third, when the phone happened to be awake.
- Durations in Dens's log count awake time alone: an hour's connection "closed after 9m44s".
- The first night's costs: Dens took 16 s of CPU in 13.6 hours, the shell's process 180 s (the WebView's threads stay after the page closes) and the probe 70 s. Android charged the spike 3.9 mAh for its 12¾ hours off screen.
- The local network permission blocks the router, on the phone's own subnet, as on the emulator.

### The Pixel 9 Pro (2026-10-27)

A friend's, by hand: Android 17 (API 37.1, patch 2026-09-05), Tensor G4, 4 KB pages, 16 GB, WebView 155, on mobile data. Spike version 12, copied over USB and installed from Files.

- **2.** Android's own `RLIMIT_MEMLOCK`, 64 KiB: `AllocLocked` locks 16 pages and fails, so stock phones need the fallback, and Samsung's 64 MiB is the exception. System calls, the resolvers and the refused copy are as on the S24.
- **4.** StrongBox, in the Titan M2: made in 77 ms, wrapped in 30, unwrapped in 34.
- **6.** WebCodecs on Tensor: AV1 encodes in hardware, at 67 fps at 720p against software's 99.6; H.264 encodes in hardware at 720p but not at 1080p; HEVC and VP9 encode in hardware; HEVC decodes only in hardware. The rest is as on the S24, the refused Clipboard API too.
- **8.** Its microphone sounds better than the S24's, its noise suppression stronger.
- **9.** Held, the call went on off screen both ways, but the sound it sent lost quality somewhere in the 90 s of quiet, and got it back when the screen came on. The S24's held its rate off screen, about 40 kbit/s while talking. With no logs, since the spike was uninstalled, the cause is open: the renderer's audio thread, where RNNoise runs, short of CPU off screen, or Opus's rate adapting on mobile data with the screen off. M12's logs tell them apart.
- **10.** An Ultra HDR photo, a 50-megapixel one and a video went through as on the S24.
- **12.** Play Protect called version 12 harmful, in the same words, though it no longer asks for `UPDATE_PACKAGES_WITHOUT_USER_ACTION`. The cut-down build it passed on the S24 had the same permissions, so the verdict comes from what only the spike has, the debugging and the probe, or from its package and key, which Play Protect had flagged before. So M12's development builds use a package name and key of their own, apart from its releases'.
