# The Relay spike (M20)

Relay links two of a member's own devices on one network, and carries two things: the voice keys of one to the call the other holds, and one's system sound to play on the other. Before M20 plans it, this spike answers what the plan leans on.

The design it tests:

- **The link** is Pion to Pion, straight across the LAN, with no listening port: each side sends to the other at once, so a firewall that lets replies in lets the link up. In Dens the den would introduce the two, the introduction sealed between the member's devices; the spike is told the other side's address by hand.
- **Keys** ride the link from the helper that reads them to the page in the call.
- **Sound** is read by the helper from the system's output, sent uncompressed (48 kHz, stereo, 16 bits, about 1.5 Mbps), and played by the page on the other side, through an audio worklet. On Android it plays as media, never as a call's.

The two setups it must serve: a desktop's sound on an Android phone, for headphones paired to the phone; and a member in a call on one of two computers behind a KVM, NixOS with the Flatpak and Windows 11, with the keys of whichever they sit at reaching the call, and the call's computer's sound playing on whichever device they use, a phone among them.

## The questions

*The link*

1. **Through firewalls, with no rules.** Two probes that send to each other at once: between Windows 11 with Defender Firewall as installed, Fedora with firewalld as installed, NixOS with its firewall as installed, and the phone on Wi-Fi, each pair both ways. From where Dens runs: under the installed service's sandbox on Linux, inside the Flatpak's, as a Windows service, and in the Android app. How long it takes to come up, and how soon each side knows the other is gone, as when Wi-Fi drops. Where it fails, which rule lets it through, which sizes the fallback.
2. **Keys.** A press read on one side acting on the other: the delay, on Ethernet and on Wi-Fi, and how soon a held key lets go once the link drops.

*Capture*

3. **Windows.** WASAPI loopback of the default output, from a process in the desktop session, as the helper is: its format, its CPU, ten minutes without a gap. With the output muted and at volume 0; as the default output changes; and with the output gone, as when a KVM takes a USB headset away, on a machine with another output and on one with none.
4. **Linux.** The monitor of the default output over PulseAudio's protocol, PipeWire's socket or PulseAudio's own: from the desktop session on Fedora's GNOME and KDE, and from inside the Flatpak with the socket's permission, on NixOS's GNOME. The same cases: muted, the default changing, the output gone. And what a Flatpak update that adds the permission asks of the member, from the command line and in GNOME's Software.

*Sound across*

5. **Uncompressed over the link.** Over Ethernet and Wi-Fi, an hour each: packets lost or late, the buffer that hides them, the two clocks drifting apart, and the CPU at each end.
6. **The delay,** from the source's output to the receiver's, by the same click recorded at both: over Ethernet, over Wi-Fi, and onto the phone, with wired and with Bluetooth headphones.
7. **The page playing it.** In Chromium and Firefox, a worklet fed from the service: with the tab in front, behind another, and in a minimized window, half an hour each.

*Android*

8. **As media, off screen.** The app playing a relay under the media volume, on Bluetooth headphones, with another app's sound left as it was; then an hour with the screen off, the page held as a call's is, the service with media playback's type and Wi-Fi kept awake: its gaps, and the battery it took.
9. **A call beside it.** What joining a call on the phone does to a relay already playing, on the loudspeaker and on Bluetooth headphones, and whether the relay comes back by itself as the call ends.

Not in the spike: the den's introductions and their seal, the settings' Relay section, a microphone relayed, Android as a source, a call played as media, sound compressed, and a share's sound in Firefox from the same capture.

## How it runs

One probe, `relayprobe`, built for Windows, Linux and Android: it links to a peer at an address, reads keys as the helper does, captures the system's sound, sends and receives it, serves the page that plays it, and writes a log of what it measured.

1. **Here**, in two Incus containers on one bridge, with nftables as each firewall in turn: the link's logic, the sound across it and the page's player in the container's Chromium and Firefox.
2. **The Windows 11 machine and the VMware guests**, bridged onto the LAN: Fedora 44's GNOME and KDE, and NixOS's GNOME with the Flatpak. Questions 1 to 7, each pair of systems.
3. **The S24**, in a development build of the app from this branch, its shell changed for questions 8 and 9, over wireless debugging as in M12's spike.
4. **The KVM**, on the other member's computers: question 3's and 4's output taken away, from one command and the log it leaves.

What takes hands is the user's: plugging and switching outputs, the phone's screen and headphones, and listening for gaps. The rest runs from here, and each run leaves a log.

## The probe

`relayprobe` is one Go program in the spikes module, with Dens's Pion and WebSocket versions, go-wca for WASAPI and jfreymuth/pulse for PulseAudio's protocol, both pure Go.

- **The link** (`link.go`): one peer connection, data channels only, with nothing passed between the probes first. Each side makes both certificates and both sets of ICE credentials from a shared secret, writes the other side's description itself with the one address it was told, binds UDP 47800 and sends. Three channels, set up without a handshake: `ctl`, reliable; `now` and `pcm`, unordered and never sent again. An attempt keeps asking for a minute, and a lost link dials again. SCTP's retransmission timer is capped at 100 ms (`-rto`), for what question 5 found. With `-sound rtp` the sound leaves `pcm` for RTP beside the channels (see Sound as RTP).
- **What rides it** (`session.go`, `keys.go`, `sound.go`): pings every 250 ms compare the two clocks, so a delay is one clock's reading against the other's; keys go as states, every key held, every 20 ms while any is and every 100 ms otherwise, and the receiver lets go of them once it hears nothing for 300 ms; sound goes in packets of 240 frames, 16-bit stereo at the source's rate, under a 24-byte header.
- **Capture** (`capture*.go`): the default output's loopback on Windows and its monitor on Linux, turned to 16-bit stereo, with a line a second of what the device gave, its level, mute and volume, and silence of the probe's own once the device gives nothing for 60 ms. It follows the default output: Windows' is asked each second, and on Linux a stream left on the old output's monitor is opened again. `-exercise` mutes the output and sets its volume to 0 by itself, and on Linux plays a tone and moves the default to a second output.
- **The player** (`page.go`, `web/`): a page on loopback, fed each packet over a WebSocket held by a worker, which hands it to an audio worklet. The worklet keeps 40 ms, or one and a half of the longest its output has lately waited between calls plus 10 ms if that's more, and plays up to 0.5% faster or slower to stay there, and reports its buffer, its underruns and the gaps between its calls every two seconds, into the probe's log. With `batch=N` in the page's address the probe hands it N packets to a message.
- **Clicks** (`click.html`, `clicks.go`): a page that clicks every two seconds; the source's probe times each click in its capture, the receiver's `-monitor` times it in its own output, and `relayprobe clicks` hears both through a microphone.

| Script | Runs |
|--------|------|
| `run/here.sh` | The two containers: the link through each firewall and start order, under the service's unit settings, with the peer killed and the path cut, long and short; sound and keys across; the capture's exercise; a click's delay; and the sound as RTP against the sound on its channel |
| `run/players.sh` | Question 7 in the container's Chromium and Firefox |
| `run/stage.sh` | Builds the probe and stages it, with the scripts below, in VMware's shared folder, `dens-vm\relay` |
| `stage/guest.sh` | A Linux guest's runs: capture, the link from the session, the service's sandbox and the Flatpak's, the hour |
| `stage/capture.ps1`, `stage/link.ps1`, `stage/service.ps1` | Windows' runs: capture, the link from the desktop session, and the link as a service |
| `stage/STEPS.txt` | The user's part, step by step |
| `stage/kvm/` | The package for the member with the KVM |
| `android/build.sh`, `run/phone.sh` | The app with the probe in it, and its runs on the phone |
| `run/hands.sh` | The phone's steps that take hands and ears, walked through at a terminal: listening, the clicks by microphone, and question 9's call |
| `run/summary.py` | Sums up a probe's log |

## Results

What ran here comes first, then the Windows machine and its guests, then the phone.

### Here, in the containers (2026-11-11)

Side a is `dens-browsers` (Debian 13), side b `dens-relay` (Fedora 44), on Incus's bridge under WSL 2. WSL's kernel can't load firewalld's own ruleset, so each firewall was the rule firewalld and NixOS come down to for a port nobody opened: replies in, and the rest dropped, as NixOS does, or rejected with ICMP's admin-prohibited, as firewalld does. `here.sh fwcheck` showed each keeping out a packet nobody asked for.

**1. The link**

- *Through the firewalls.* Every pair of none, drop and reject, with the two started together, a five seconds first, and b five seconds first: 21 runs, each up on its first attempt. The side that starts second is up 27 to 220 ms after it starts. Started together, 62 to 257 ms: through two firewalls the first checks cross and both are dropped, and the next, 200 ms on, come in as replies.
- *It only works while both keep sending.* The side that starts first must go on sending until the other does: its own sending is what lets the other's first packet in. The probe's attempt sends for a minute; Pion's seven tries would have stopped after about a second and a half.
- *Under the service's sandbox.* With every setting of Dens's unit and an account of its own, side b was up in 208 ms, and served its page on loopback.
- *Knowing the other is gone.* The probe's own rule, nothing heard on any channel for 300 ms, fired 300 to 320 ms after the peer was killed or the path cut. ICE is slower and is for dialing again, not for keys: with Pion's timeouts (disconnected after 5 s, failed after 25 s more, asking every 2 s) it said disconnected 6 s after the cut and failed at 30 s; with 1 s, 3 s and 500 ms, disconnected within 1.2 s and failed at 4 s.
- *Coming back.* A 12 s cut under Pion's timeouts healed by itself, but nothing was heard for 2.5 s after ICE was connected again: SCTP's timer, which question 5 caps. Under the short timeouts the link failed and dialed again, each attempt giving up after 4 s, and was up 3.0 to 3.3 s after the path returned.

**2. Keys**

- Over five minutes of presses each way, 600 changes a side: a change reached the other side in 0.2 ms at the median and 0.8 ms at worst, where a round trip is 0.3 ms. The link adds nothing of its own; what's left to measure is the network's delay on real machines.
- A key held when the peer dies or the path is cut is let go 300 to 320 ms after the last word from it, by the 300 ms rule above.
- With SCTP's timer as Pion has it, every 200 ms cut tripped that rule: the channel stood still for a second, so a held key was let go and pressed again. With the timer capped (question 5) only the cuts of a second did.

**4. Linux capture** (PulseAudio 17 itself, as the browsers' container has it; PipeWire is the guests' to answer)

- The default output's monitor records through jfreymuth/pulse in 5 ms pieces, with nothing missing over two minutes and no silence of the probe's needed.
- A muted output, or one at volume 0, gives a silent monitor: PulseAudio applies an output's volume in software before its monitor. A member who mutes the computer because they're listening on the phone would relay silence.
- When the default output changes, PulseAudio moves what's playing to the new one and leaves the recording on the old one's monitor, silent, even one opened by the name `@DEFAULT_MONITOR@`. The probe now asks each second where its stream sits, and opens it again once it has sat on the wrong monitor for two.

**5. Uncompressed over the link** (a tone each way, 200 packets a second, 1.57 Mbps)

- Five minutes each way with keys beside it: 58,000 packets a side, none lost, none more than 0.8 ms late, the longest wait between two 11 ms. Each probe took 14% of a core to send one stream and receive another under WSL.
- *A short cut costs a second, as Pion's SCTP comes.* The path cut for 200 ms every five seconds, as Wi-Fi roaming or a burst of loss would: each cut cost 1.0 to 1.1 s of sound. SCTP sends nothing new until its retransmission timer fires, and Pion's never waits less than a second, even on channels that never send again. The sender, which drops what it can't send once 32 KB wait, dropped 170 packets a cut.
- *Capped, it costs the cut.* `SetSCTPRTOMax` also lowers that floor. At 250 ms a 200 ms cut cost 260 to 350 ms; at 100 ms and at 50 ms, 210 to 310 ms, which is the cut itself as the script made it. A cut of a second costs a second at any setting. The probe now caps it at 100 ms.
- Sound as RTP, which has no such timer, was tried the day after: see Sound as RTP.

**6. The delay, in software** (both probes in one container; a browser's click page into the default output, the receiver's page into a second one, whose monitor times each click coming out)

- 88 to 95 ms from the source's capture of a click to the receiver's output of it, over ten clicks: the 40 ms buffer, about 12 ms of Chromium's own, the 32 to 40 ms PulseAudio's output reports, and 5 ms each of capture and packet.
- Asked for 20 ms, the worklet kept 41: Chromium's output asked every 21 ms at worst there, and it keeps one and a half of that and 10 ms. The buffer never fell under 25 ms.

**7. The page playing it** (Chromium 156 and Firefox ESR 153, on Xvfb with a window manager and PulseAudio's null sink; a tone across the link from the other container, 30 minutes, eight pages at once)

- Each browser played in a window in front, in a tab behind another, and in a minimized window; the hidden pages reported themselves hidden. All six played the whole half hour, and after the first minute, where the browsers themselves were starting, none ran dry: no underrun, no skip, nothing lost.
- A hidden page lost nothing to the browser's throttling: its own 100 ms timer ran at most 17 ms late, since both browsers leave a page that's playing sound alone.
- The buffer, set to 40 ms, averaged 38 ms and fell at its lowest to 8 ms, at a moment a build here loaded the machine. At that moment the page fed through its own thread (`via=main`) fell to 2.5 ms in Chromium and had the run's one underrun; the pages fed through the worker held 8 to 11 ms.
- Chromium's output asked for sound every 13 ms, 21 ms at the 99th percentile. Firefox's the same, but three times, every 9 minutes 44 seconds in all four of its pages at once, it waited 42 to 76 ms; the buffer held, at 25 ms.
- The null sink's clock ran 345 millionths slower than the tone's. The worklet held its depth by playing that much slower, 1.7 ms under its target, and never corrected by more than 0.5%.
- The link gave the pages every packet, the latest 9 ms late. The receiving probe took 15% of a core with eight pages to feed, and the sender 9% for its one stream, most of it in waking for each of 200 packets a second under WSL; the real machines' logs say what it costs there.

### On the Windows machine, from here (2026-11-11)

Run from WSL as the desktop user, capture alone, a few seconds each, with no socket beyond loopback.

**3. Windows capture**

- WASAPI's loopback of the default output opens from the desktop session through go-wca, with no cgo: `Speakers (Magni Mesh Dac)`, 48 kHz, 2 channels, 32-bit float, a 10 ms period, packets of 480 frames, the longest wait between two 13 ms.
- The probe's own conversion to 16 bits took 78 ms of processor in 6 s, 1.3% of a core. WASAPI's conversion (`-convert`) gave 16-bit stereo directly, for 109 ms in 6 s.
- A stream of silence played beside the capture (`-keep-warm`) opened and changed nothing while sound was playing; what the loopback gives in silence waits for the user's runs.

### On the Windows machine and its guests (2026-11-12)

Windows 11 on Ethernet at 10.0.0.43, its network set to Public and Defender Firewall on for every profile, as installed. Three VMware guests bridged onto the same LAN: Fedora 45 Workstation with GNOME and with KDE (PipeWire 1.6.11, firewalld in its FedoraWorkstation zone, which lets every port above 1024 in), and NixOS 26.05 with GNOME (PipeWire 1.6.6, NixOS's firewall on, Dens's Flatpak installed). The probe's logs are in the shared folder's `logs`; `run/summary.py` sums one up.

**1. The link**

- *Windows as a service, with no rule.* In session 0, under an account of its own with a restricted token, as Dens's service runs, the probe was up 18 ms after it started, three times: with Fedora's probe under the unit's sandbox, with Fedora's interface moved to firewalld's public zone, which rejects what nobody asked for, and with NixOS's probe inside the Flatpak's sandbox behind NixOS's firewall. Windows asked nothing and made no rule. With the rule Dens's installer makes for the media ports, for this program and port, nothing changed.
- *Windows from the desktop session.* Binding the port raised Defender's prompt, "some features were blocked on public and private networks". The link was already up, 19 ms after the probe started and before the prompt was answered: replies to what the probe sent came in regardless. Allow made two inbound rules for the program on Public. So the prompt is a desktop program's, for holding a port open, and the service, which holds the link in Dens, raises none.
- *Linux with Linux.* Fedora's session with NixOS inside the Flatpak: up 134 ms after the second started.
- The side that starts first fails every 30 s under Pion's ICE timeouts and dials again; the side that starts second links at once. The move to firewalld's public zone is in no log; its command answered `public`, with the guest's interface under it.

**2. Keys**

- F8 held on Windows, read as the helper reads it, reached Fedora within 0.3 ms of being read, eight changes of eight. Windows' own clock steps in half milliseconds, so what it measures of the other direction reads as zero.
- Between the two guests, 232 changes each way: 0.4 and 0.7 ms at the median, 2.4 and 3.9 ms at the 99th percentile, 9.2 ms at worst. Over the hour, 7,051 changes reached Windows with no state lost.

**3. Windows capture** (`Speakers (Magni Mesh Dac)`, 48 kHz float, from the desktop session)

- *Muted*, the loopback gives the sound as it was. *At volume 0* it gives silence: the loopback is taken after the output's volume and before its mute. A member listening on the phone mutes the computer, and must not turn it down.
- *Another default output*: the probe, asking each second, saw it within a second and was capturing the new one 0.2 s later. *The output unplugged*: the read failed with `AUDCLNT_E_DEVICE_INVALIDATED` at once, and the probe was capturing the output Windows fell back to 0.2 s later; plugged back, Windows made it the default again and the probe followed. The longest the capture gave nothing was 0.64 s, which the probe's own silence covered. With no other output to fall back to wasn't tried.
- *Paused*, the loopback went on giving packets of silence: the probe's own silence wasn't needed.
- The capture alone took 2% of a core. Over the hour of question 5 it ran 3,599 s with one second short, at the end, and never waited more than 24 ms for a packet.

**4. Linux capture** (PipeWire, through its PulseAudio socket)

- On all three guests the default output's monitor records, in pieces of 22 ms: PipeWire hands over its own 1,024 frames whatever the stream asks for.
- *Muted or at volume 0*, the monitor gives the sound as it was, unlike PulseAudio's own: PipeWire's monitor is taken before the output's volume.
- *Another default output*: PipeWire moved the recording to the new default's monitor itself, within the second. The probe's own following never had to act.
- *The VM's sound card disconnected*: the output stayed, and its monitor gave nothing for the 25 s; the probe's silence covered it and the sound came back with the card.
- *Inside the Flatpak* as it is today there's no socket: `/run/user/1000/pulse/native` doesn't exist there. With `--socket=pulseaudio` the capture is the session's, case for case. The sandboxed probe was refused when it tried to mute the output, set its volume or change the default, which Dens never needs.
- The capture took 2% to 2.5% of a core.

*The Flatpak gaining the permission* (two test builds, the second with the socket, in NixOS's GNOME)

- `flatpak update` lists it, "New chat.dens.Dens permissions: pulseaudio", above its usual question, and goes on at Y.
- GNOME Software marks the update "Requires additional permissions" in red, and says the permission lets the app listen to the microphone and play sound. It doesn't install such an update by itself.
- Clicking Update there installed it with no further question about the permission; if anything was asked it was the administrator's password, in the usual words.
- Dens's own page never offered this update, even after Dens was started again: only once Software had installed it did the page ask to restart. A build that adds a permission reaches a member through Software or the command line, not through the page's Install.

**5. Uncompressed over the link** (Windows' own sound to Fedora's GNOME for an hour, across VMware's bridge, not a wire)

- 706,006 packets, none lost; 18 more than 5 ms late and 8 more than 10 ms, the latest 12.9 ms; the longest wait between two 24 ms. Nothing dropped by the sender.
- The two computers' clocks drift 8 millionths apart. The player steered by a median of 8 millionths, its output being a null sink on the guest's own clock.
- Windows' probe, capturing and sending, took 5.3% of a core; Fedora's, receiving, feeding the page and listening to its own output, 6.9%. With a tone each way for two minutes the guests took 9% to 12% and Windows 5.5% to 7%.

**6. The delay**: the clicks at the hour's end went across, but the probe missed them. Its click finder wanted a click loud from its first frame, and at Windows' volume these rose over three. It's fixed (a click is now timed from its first frame over the quiet level), and the software delay on these machines waits for another short run.

**7. The page playing it** (Firefox 159 on Fedora's GNOME, the same hour)

- 58 minutes reported without an underrun or a skip. The buffer kept 45 ms, Firefox's output asking every 23 ms there, and never fell under 22 ms.
- The browser was minimized for the last 25 minutes. Under Wayland the page saw itself hidden for one second and then shown but unfocused, and played on the same.

**The KVM** wasn't run. The member's KVM switches the keyboard and mouse alone: the NixOS computer keeps its own speakers, and its picture reaches Windows through a capture card. No output is taken away there, and the unplugged output above answers the question for Windows.

### On the S24 Ultra (2026-11-12)

Android 17, WebView 157, on Wi-Fi at 10.0.0.33, off its charger; the spike's build of the app, `chat.dens.Dens.dev`, beside the released one; Windows' desktop session at the other end, sending its own sound.

**1. The link, from the Android app**

- *Android 17 keeps the app off its own network.* The probe's first packet to Windows failed with `sendto: operation not permitted`, and every one after it. The app needs `ACCESS_LOCAL_NETWORK`, new in Android 17 and asked of the member like the microphone; the spike's build declares it and adb granted it. Dens has no such permission today, since it only ever reached dens.
- With it, the link was up 226 ms after the phone's probe started, the phone locked and dozing.

**2. Keys, over Wi-Fi**

- 15,894 changes from the phone to Windows over the afternoon, most of it with the phone's screen off: 3.2 ms at the median, 6.3 ms at the 95th percentile, 16.9 ms at the 99th and 81.6 ms at worst, no state lost. A round trip took 4.7 ms at the median and 127 ms at worst.

**5. Uncompressed over Wi-Fi** (Windows' own sound to the phone)

- Nothing is lost: 308,000 packets in the first 25 minutes with the screen off, and 84,000 with it on, every one arrived. Windows never dropped one, though 24 KB once waited to be sent.
- They arrive late. With the app in front and the screen on, no packet was more than 20 ms late. Once the screen was off, or another app was in front, the phone's Wi-Fi handed them over in bunches: hundreds in each ten seconds came 20 to 50 ms late, and now and then one 110 to 160 ms late. The Wi-Fi lock the shell holds for low latency counts only while the app is in front with the screen on.
- So a 40 ms buffer isn't enough on a phone. With the screen off it ran dry 8 times in 25 minutes, and 19 times in seven minutes that began in front and ended behind another app. The user heard those as "little gaps", noticeable in music and not in speech.
- *An hour on Bluetooth headphones, screen off, with 150 ms kept.* 766,007 packets, none lost, 170 of them more than 50 ms late and the latest 118 ms. Late packets no longer ran the buffer dry. It ran dry four times all the same, each time just after the headphones' output had gone 134 to 187 ms without asking for sound and then asked for it all at once: the gap is the Bluetooth output's, and the buffer only fails to hide the catching up.
- *What it costs the phone.* The probe took 28% to 37% of a core, on a phone whose cores idle slowly, and the app's own process 17% to 22% beside it. A profile of the probe put a third of that in the scheduler waking goroutines, 18% in writing each packet to the page's socket, 16% in sending SCTP's acknowledgements back for sound that is never sent again, 8% in receiving, and next to nothing in decrypting. Packets four times the size (`-frames 960`, 20 ms) brought the probe from 34% to 25%, and twice the size to 32%: SCTP cuts a large message into as many datagrams as before, and acknowledges them as often. Sound as RTP, which acknowledges nothing, and packets handed to the page several at a time are what the profile points at; both were tried after (see Sound as RTP).

**6. The delay, by ear** (the click page on Windows, the PC's microphone hearing the PC's own click and the phone's)

- On the phone's speaker: 150 ms at the median, 121 to 161 ms, over six pairs.
- On Bluetooth headphones (Sony WH-1000XM6, A2DP): 435 ms, three pairs agreeing within a millisecond; the user's ear said "about half a second". The headphones' own delay is most of it.
- Wired headphones weren't tried.

**8. As media, off screen**

- Android lists the page's player as `USAGE_MEDIA`. The phone's volume keys show and move the media volume, and the relay follows. Another app's video played beside it, each at its own loudness.
- With the phone locked and its screen off the page went on playing, held as shown, the service with media playback's type: the worklet's calls never came more than 33 ms apart in 25 minutes. The gaps were the late packets above, not the page.
- The battery fell 118 mAh in those 24 minutes on the loudspeaker, and 255 mAh in the hour on Bluetooth headphones, 61% to 56%: about 5% of this phone an hour, against the 27 mAh an hour M12's spike measured for the phone lying idle with Dens connected.

**9. A call beside it** (no den on this build, so the probe's own page opened the microphone as a call's page does, the shell granting it and routing the call as it does for Dens; then it made its player again inside the "call", closed the microphone, and made the player once more)

- *On Bluetooth headphones.* When the microphone opened, Android moved the headphones to their call link, and Chromium suspended the relay's context: a relay playing as media goes silent for the call. A relay made again after the microphone opened played as `USAGE_VOICE_COMMUNICATION`, on the call link, at the same loudness but in a phone call's quality, "way lower" to the user's ear. When the microphone closed, the headphones went back to music's link and the same player came back as media, in full quality, by itself.
- *On the loudspeaker.* A relay playing as media kept playing when the microphone opened, perhaps a little worse. Made again inside the call it played as the call's sound: louder, and in good quality. When the microphone closed it went back to media by itself.
- So the user's thought holds, and on Bluetooth it's the only way a relay plays during a call at all: a relay started, or started again, once the call's microphone is open plays inside the call, on its route. On Bluetooth that costs its quality for as long as the call lasts.
- Chromium keeps a closed context's output stream for some seconds and hands it to the next context as it was opened: a player made again within that time stays what it was. The walk waits eight seconds.
- Not tried: a real call in Dens beside the relay, and whether a relay suspended by a call on Bluetooth comes back when the call ends without being made again.

### Sound as RTP (2026-11-12)

`-sound rtp` sends the sound as RTP beside the channels, which keep the keys, the pings and `ctl`: a second section in the description each probe writes for the other, one payload type of the link's own (16-bit PCM, `L16/48000/2`), each side's SSRC fixed so the description can name it, SRTP under the link's DTLS, and no interceptors, so nothing is reported, acknowledged or asked for again. A packet stays 240 frames; 288 is the most one datagram holds.

**In the containers** (`here.sh rtp`: a tone each way and keys, through two drop firewalls)

- The link was up in 71 to 76 ms as before, and the track arrived 4 ms after.
- *The processor.* Each probe, sending one stream and receiving one, took 8.8% to 9.1% of a core, against 12.1% to 12.9% with the sound on its channel.
- *Short cuts,* with SCTP's timer as Pion has it: a 200 ms cut cost 205 ms of sound, where it cost the channel 0.85 s; a cut of a second costs a second either way. The keys still ride SCTP, and still stood still after each cut, a change arriving up to 1.8 s late: the cap stays, for them.
- *Packets lost at random,* 1% and 5% each way, SCTP capped at 100 ms. As RTP the sound lost what the path lost, 1.0% and 4.8%, and nothing came late. On its channel every packet arrived: Pion's SCTP sends a lost one again even on a channel told never to, and it comes 50 to 150 ms late (at 5%, 203 of 10,000 more than 50 ms late, the latest 152 ms). So on the channel loss is lateness, which 150 ms of buffer hides and 40 ms doesn't; as RTP it's loss under either. The keys came through both: 26 ms at worst beside RTP, 74 ms on the channel's association at 5%.

**On the S24** (Windows' own sound to the phone, locked with its screen off, the page held shown, 150 ms kept; two to three minutes a run)

- *The probe* took 30% to 32% of a core with the sound on its channel and 21% to 23% as RTP, in two runs of each: 40.7 s of processor against 28.5 s in 169 s. SCTP's writer, the acknowledgements, fell from 17% of the profile to 4%. What's left is the scheduler waking for each packet (39%) and the page's socket (29%).
- *Windows,* capturing and sending, took 3.8% on the channel and 2.3% as RTP.
- Nothing was lost either way, and the player ran dry once in the four runs.
- *Packets handed to the page several at a time* (`batch=N`, as RTP). The phone's three processes over a minute, as `top` has them, in hundredths of a core:

| | WebView's renderer | The app | The probe | Together |
|---|---|---|---|---|
| On the channel, one a message | 37 | 24 | 32 | 93 |
| RTP, one a message | 37 | 24 | 22 | 82 |
| RTP, 4 a message (20 ms) | 29 | 21 | 20 | 70 |
| RTP, 8 a message (40 ms) | 26 | 20 | 19 | 65 |
| RTP, 16 a message (80 ms) | 23 | 18 | 19 | 59 |

- The buffer's lowest fell as the messages grew, 123, 113, 98 and 81 ms of the 150 kept, and never ran dry. The renderer, which the earlier runs didn't count, is the largest of the three.

### In the emulator (2026-11-11)

**8. As media** (Android 17's emulator, the spike's build of the app, the probe's own tone)

- The probe runs as the app's child from the native library directory, as Dens does, and serves its page on loopback; the page plays in a second WebView under the page's, 4 pixels high, held as shown.
- Android lists its player as `USAGE_MEDIA`, the service's types as special use and media playback, and the Wi-Fi lock as held.
- Screen off for two minutes: the page stayed shown and playing, with no underrun.
- The emulator's output asks for sound about every 100 ms, which emptied a 40 ms buffer over and over. The worklet now keeps one and a half times the longest wait between its calls, plus 10 ms, when that's more than its 40 ms: about 170 ms there. What a phone's output asks for, on its speaker and over Bluetooth, is the phone's to answer.

## For the plan

What the answers ask of M20's plan, most pressing first.

- *The link stands as designed.* Pion to Pion with nothing listening got through Defender Firewall on a Public network from a service with no rule, firewalld's reject, NixOS's firewall, the unit's sandbox, the Flatpak's and the Android app, in 20 to 260 ms. The fallback through the den isn't needed for firewalls. What the link can't cross is two networks: the guests on VMware's NAT never linked.
- *Both sides send until one answers.* Pion gives up a pair after 1.4 s; the plan sets the tries (`SetICEMaxBindingRequests`) for as long as an introduction is good, and has the den hand the introduction to both at once.
- *Android needs a permission Dens doesn't have.* `ACCESS_LOCAL_NETWORK`, declared and asked for when the member turns Relay on, as the microphone is asked for at the first call; without it every send fails. The service also takes media playback's type.
- *The sound goes as RTP, and SCTP's timer is capped for the keys.* As Pion comes, any cut of a fifth of a second stops every channel for a second. `SetSCTPRTOMax(100ms)` fixes that for the keys. The sound as RTP never stalls, takes a third less of the processor at both ends, and leaves the keys an association of their own. What it gives up is SCTP's sending a lost packet again, which hid loss under a phone's buffer; nothing was lost on this network in two million packets, and a lost packet is 5 ms the player can fade across.
- *Keys are settled.* States, not events; every 20 ms while a key is held; let go after 300 ms of nothing. A fraction of a millisecond on a wire, 3 ms to a phone, 82 ms at the worst moment of a sleeping phone's Wi-Fi.
- *Windows: the helper captures, the service links.* The service raises no firewall prompt and needs no rule; a desktop program holding the port raises one. Loopback through WASAPI works from the helper's session; it follows the default by asking each second and opens again when the device goes. Muted relays; volume 0 doesn't, which the page should say.
- *Linux: one path, PulseAudio's protocol.* On PipeWire the monitor ignores mute and volume and follows the default by itself. On PulseAudio it does neither, so the capture checks where it sits and opens again, and a muted output there relays silence.
- *The Flatpak needs `--socket=pulseaudio`, and its page can't deliver that update.* Software marks it as needing permissions and words it as the microphone; the page never offers it. The release that adds it needs its changelog and guide to say so, and members update that once through Software or the command line.
- *The buffer belongs to the receiver, and a phone needs more.* 40 ms held for an hour on desktop browsers. A phone off screen needs about 150 ms for its Wi-Fi's lateness, and on Bluetooth still drops a few times an hour to the headphones' own stalls. The user asked for a simple choice of low or high; a buffer that follows what it sees is the other way. The player wants a worker to hold its socket, and a target that follows the output's stride.
- *The delay is what it is.* About 90 ms in software, 150 ms on a phone's speaker, 435 ms on Bluetooth headphones, most of that the headphones'. Video on the computer with its sound on Bluetooth headphones will be out of step by that much.
- *The phone pays about 5% of its battery an hour.* Half a core between the probe and the app at 200 packets a second. RTP and four to eight packets to the page's message take a quarter to a third off that; the rest is 200 packets a second, which only compressing the sound would change, and that's out of scope.
- *A relay inside a call, on Android.* The user's thought, 2026-11-12, and question 9 bears it out: a relay started once the call's microphone is open plays as the call's sound on the call's route, and goes back to media by itself when the call ends. On Bluetooth a relay left as media goes silent for the call, so there the plan has to make the player again as a call starts; the price is a call's quality on the headphones while it lasts. Making it again must wait out Chromium's kept stream, or reuse the call's own context.
- *Dependencies.* go-wca with go-ole, and jfreymuth/pulse, both pure Go and small, carried the spike. The plan decides whether Dens takes them or writes the few calls each makes.

Not answered: Windows with no output left at all; wired headphones on the phone; a real call in Dens beside a relay, and a relay suspended by a call on Bluetooth coming back unaided; the delay in software between two real machines; another phone than the S24; a computer on Wi-Fi.
