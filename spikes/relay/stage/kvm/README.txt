Relay test for Dens: what happens to a computer's sound when the KVM takes the headset away
============================================================================================

This is a small test program, relayprobe. It listens to what your computer is
playing (it only measures how loud it is; it records and sends nothing), and
writes a log of what the sound system did while you switch the KVM. It installs
nothing and changes no settings. It stops by itself after 4 minutes.

Do this once on Windows and once on NixOS. Each takes 4 minutes.

On Windows 11
  1. Play some music or a video, with sound, and leave it playing.
  2. Double-click windows.cmd. A black window opens and counts along.
     (If Windows says it protected your PC: More info, then Run anyway.)
  3. When it says SWITCH AWAY (after 1 minute): press the KVM to the other
     computer. When the 30 seconds are up, switch back.
     You can't see the window while you're away, so count to 30, or use a phone.
  4. It asks once more a minute later. Same again.
  5. When it says Done, send back the file kvm-windows.log from this folder.

On NixOS
  1. Play some music or a video, with sound, and leave it playing.
  2. In a terminal, in this folder:   sh nixos.sh
  3. SWITCH AWAY and back when it says so, twice, as on Windows.
  4. When it says Done, send back the two files kvm-nixos-session.log and
     kvm-nixos-flatpak.log from this folder.

That's all. Thank you!
