#!/bin/sh
# Relay test for Dens: see README.txt. One probe listens from your desktop
# session and one from inside Dens's Flatpak, both for the same 4 minutes.
cd "$(dirname "$0")" || exit 1
APP=chat.dens.Dens
mkdir -p "$HOME/.cache/relay-spike"
cp relayprobe-linux-amd64 "$HOME/.cache/relay-spike/relayprobe" && chmod 755 "$HOME/.cache/relay-spike/relayprobe"
rm -f kvm-nixos-session.log kvm-nixos-flatpak.log
echo "Listening to this computer's sound for 4 minutes. Keep something playing."
"$HOME/.cache/relay-spike/relayprobe" -quiet -send system -page off -for 240s -log "$PWD/kvm-nixos-session.log" &
if flatpak info "$APP" >/dev/null 2>&1; then
  F=$HOME/.var/app/$APP/relay-spike
  mkdir -p "$F"
  cp relayprobe-linux-amd64 "$F/relayprobe" && chmod 755 "$F/relayprobe"
  rm -f "$F/kvm.log"
  flatpak run --socket=pulseaudio --command="$F/relayprobe" "$APP" -quiet -send system -page off -for 240s -log "$F/kvm.log" &
else
  echo "(Dens's Flatpak isn't installed here, so only the session's probe runs.)"
fi
sleep 60
echo ">>> SWITCH AWAY now: press the KVM to the other computer, count to 30, switch back."
sleep 60
echo ">>> SWITCH AWAY once more: count to 30, switch back."
wait
[ -f "$HOME/.var/app/$APP/relay-spike/kvm.log" ] && cp "$HOME/.var/app/$APP/relay-spike/kvm.log" kvm-nixos-flatpak.log
echo
echo "Done. Please send back kvm-nixos-session.log and kvm-nixos-flatpak.log from this folder."
