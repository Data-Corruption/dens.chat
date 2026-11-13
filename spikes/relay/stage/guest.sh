#!/bin/sh
# The Relay spike on a Linux guest (spikes/relay/README.md): run from the
# VMware shared folder, /mnt/hgfs/dens-vm/relay. Every run's log lands in
# logs/ beside this script, named for the guest and the run.
#
# Usage: ./guest.sh COMMAND [ARGS]
#   info                        this guest's address, desktop, sound server and firewall
#   capture                     question 4 from the desktop session: 3.5 minutes, runs itself,
#                               and says when to disconnect the VM's sound card
#   flatpak-capture             question 4 from inside the Flatpak, without and with the sound socket
#   link PEER [a|b]             questions 1 and 2 from the desktop session: 2 minutes
#   sandbox PEER [a|b]          the same under the installed service's unit settings (asks for sudo)
#   flatpak-link PEER [a|b]     the same inside the Flatpak's sandbox, as it is today
#   hour PEER [a|b] [SECONDS]   questions 5 and 6: receive, and play on the page, for an hour
#   firewall public|back        Fedora: firewalld's public zone on this guest's interface, and back
#
# PEER is the other probe's address. The role is b unless given: Windows
# is always a.

set -eu
DIR=$(cd "$(dirname "$0")" && pwd)
LOGS=$DIR/logs
HOST=$(hostname 2>/dev/null || cat /etc/hostname)
STAMP=$(date +%H%M%S)
APP=chat.dens.Dens
mkdir -p "$LOGS"

# The probe runs from the guest's own disk: a shared folder may refuse to
# run a binary, and the Flatpak's sandbox sees only the app's own folder.
BIN=$HOME/.cache/relay-spike/relayprobe
FDIR=$HOME/.var/app/$APP/relay-spike
place() {
  mkdir -p "$(dirname "$BIN")"
  cp "$DIR/relayprobe-linux-amd64" "$BIN.new"
  chmod 755 "$BIN.new"
  mv "$BIN.new" "$BIN"
}
place_flatpak() {
  flatpak info "$APP" >/dev/null 2>&1 || { echo "Dens's Flatpak isn't installed here" >&2; exit 1; }
  mkdir -p "$FDIR"
  cp "$DIR/relayprobe-linux-amd64" "$FDIR/relayprobe"
  chmod 755 "$FDIR/relayprobe"
}
address() { ip -4 -o addr show scope global | awk '{print $4}' | cut -d/ -f1 | head -n 1; }
done_log() { echo; echo "Done. The log: logs/$(basename "$1")"; }

# say_at SECONDS TEXT: a line on the terminal that long from now.
say_at() { (sleep "$1"; printf '\n>>> %s\n' "$2") & }

cmd=${1:-}
[ $# -gt 0 ] && shift
case $cmd in
  info)
    {
      echo "host: $HOST   address: $(address)"
      echo "session: ${XDG_SESSION_TYPE:-?}   desktop: ${XDG_CURRENT_DESKTOP:-?}"
      grep -E '^(PRETTY_NAME|VERSION_ID)=' /etc/os-release
      uname -r
      echo "--- sound"
      (pactl info 2>/dev/null | grep -E 'Server Name|Server Version|Default Sink|Default Sample') || echo "no pactl"
      (wpctl status 2>/dev/null | sed -n '1,25p') || echo "no wpctl"
      ls -l "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/pulse/native" 2>&1
      echo "--- firewall"
      (firewall-cmd --state 2>&1; firewall-cmd --get-default-zone 2>&1; firewall-cmd --get-active-zones 2>&1) || :
      systemctl is-active firewalld firewall nftables 2>&1 | paste -sd' ' || :
      echo "--- flatpak"
      flatpak --version 2>&1 || :
      flatpak info "$APP" 2>&1 | grep -E 'Version|Origin|Installation|Commit|Runtime' || :
      flatpak info --show-permissions "$APP" 2>&1 || :
    } | tee "$LOGS/$HOST-info.txt"
    ;;
  capture)
    place
    log=$LOGS/$HOST-capture-$STAMP.log
    echo "Capturing this guest's sound for 3.5 minutes. The probe plays its own tone and"
    echo "changes the output by itself for the first two: just watch."
    say_at 125 "NOW disconnect the sound card: VMware's VM menu > Removable Devices > Sound Card > Disconnect."
    say_at 150 "Connect the sound card again, the same way."
    "$BIN" -quiet -send system -exercise -page off -for 215s -log "$log"
    done_log "$log"
    ;;
  flatpak-capture)
    place_flatpak
    echo "First inside the Flatpak as it is today, 10 seconds: the sound socket isn't there."
    flatpak run --command="$FDIR/relayprobe" "$APP" -quiet -send system -page off -for 10s -log "$FDIR/without.log" || :
    cp "$FDIR/without.log" "$LOGS/$HOST-flatpak-capture-without-$STAMP.log"
    echo "Now with the socket's permission, 3.5 minutes, as the capture run."
    say_at 125 "NOW disconnect the sound card: VMware's VM menu > Removable Devices > Sound Card > Disconnect."
    say_at 150 "Connect the sound card again, the same way."
    flatpak run --socket=pulseaudio --command="$FDIR/relayprobe" "$APP" -quiet -send system -exercise -page off -for 215s -log "$FDIR/with.log" || :
    cp "$FDIR/with.log" "$LOGS/$HOST-flatpak-capture-$STAMP.log"
    done_log "$LOGS/$HOST-flatpak-capture-$STAMP.log"
    ;;
  link)
    place
    peer=${1:?usage: guest.sh link PEER [a|b]}
    log=$LOGS/$HOST-link-$STAMP.log
    echo "Linking to $peer from the desktop session for 2 minutes. This guest is at $(address)."
    "$BIN" -peer "$peer" -role "${2:-b}" -keys synth -send tone -page off -for 120s -log "$log" | grep --line-buffered -E 'link |peer ' || :
    done_log "$log"
    ;;
  sandbox)
    peer=${1:?usage: guest.sh sandbox PEER [a|b]}
    log=$LOGS/$HOST-sandbox-$STAMP.log
    echo "Linking to $peer under the installed service's unit settings for 2 minutes (sudo). This guest is at $(address)."
    sudo install -m 755 "$DIR/relayprobe-linux-amd64" /usr/local/bin/relayprobe-spike
    sudo systemd-run --quiet --wait --collect --unit relayprobe-spike \
      -p DynamicUser=yes -p StateDirectory=relayprobe-spike -p UMask=0077 -p LimitCORE=0 \
      -p NoNewPrivileges=yes -p ProtectSystem=strict -p ProtectHome=yes -p PrivateTmp=yes -p PrivateDevices=yes \
      -p ProtectKernelTunables=yes -p ProtectKernelModules=yes -p ProtectKernelLogs=yes -p ProtectControlGroups=yes \
      -p ProtectClock=yes -p ProtectHostname=yes -p ProtectProc=invisible -p ProcSubset=pid \
      -p RestrictNamespaces=yes -p RestrictRealtime=yes -p RestrictSUIDSGID=yes -p LockPersonality=yes \
      -p MemoryDenyWriteExecute=yes -p RemoveIPC=yes -p CapabilityBoundingSet= -p AmbientCapabilities= \
      -p "RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK" -p SystemCallArchitectures=native \
      -p SystemCallFilter=@system-service -p SystemCallErrorNumber=EPERM \
      /usr/local/bin/relayprobe-spike -quiet -log /var/lib/relayprobe-spike/run.log \
      -peer "$peer" -role "${2:-b}" -keys synth -send tone -page 127.0.0.1:47880 -for 120s || echo "the unit failed: see journalctl -u relayprobe-spike"
    # The log is root's to read and this user's to keep.
    # shellcheck disable=SC2024
    sudo cat /var/lib/private/relayprobe-spike/run.log >"$log" 2>/dev/null || sudo cat /var/lib/relayprobe-spike/run.log >"$log"
    sudo rm -rf /var/lib/private/relayprobe-spike /var/lib/relayprobe-spike /usr/local/bin/relayprobe-spike
    grep -E 'where=|link  up|link  attempt' "$log" | cut -c1-160
    done_log "$log"
    ;;
  flatpak-link)
    place_flatpak
    peer=${1:?usage: guest.sh flatpak-link PEER [a|b]}
    echo "Linking to $peer from inside the Flatpak's sandbox for 2 minutes. This guest is at $(address)."
    rm -f "$FDIR/link.log"
    flatpak run --command="$FDIR/relayprobe" "$APP" -peer "$peer" -role "${2:-b}" -keys synth -send tone \
      -page 127.0.0.1:47880 -for 120s -log "$FDIR/link.log" | grep --line-buffered -E 'link |peer |page ' || :
    cp "$FDIR/link.log" "$LOGS/$HOST-flatpak-link-$STAMP.log"
    done_log "$LOGS/$HOST-flatpak-link-$STAMP.log"
    ;;
  hour)
    place
    peer=${1:?usage: guest.sh hour PEER [a|b] [SECONDS]}
    secs=${3:-3600}
    log=$LOGS/$HOST-hour-$STAMP.log
    echo "Receiving from $peer for $secs seconds. This guest is at $(address)."
    echo "What arrives plays to an output that goes nowhere, so the host never hears itself."
    (sleep 3; xdg-open "http://127.0.0.1:47880/?name=$HOST" >/dev/null 2>&1 || :) &
    echo ">>> A browser opens the player: click \"Play what arrives\" in it, and leave its tab open."
    "$BIN" -quiet -peer "$peer" -role "${2:-b}" -null-output -monitor -keys synth -page 127.0.0.1:47880 -for "${secs}s" -log "$log"
    done_log "$log"
    ;;
  firewall)
    iface=$(ip -4 -o route show default | awk '{print $5}' | head -n 1)
    # NetworkManager keeps the zone with the connection, so back means
    # the zone this found, not a reload.
    kept=$HOME/.cache/relay-spike/zone
    mkdir -p "$(dirname "$kept")"
    case ${1:-} in
      public)
        [ -f "$kept" ] || firewall-cmd --get-zone-of-interface="$iface" >"$kept"
        sudo firewall-cmd --zone=public --change-interface="$iface"
        ;;
      back)
        [ -f "$kept" ] || { echo "no zone was kept: guest.sh firewall public wasn't run" >&2; exit 1; }
        sudo firewall-cmd --zone="$(cat "$kept")" --change-interface="$iface"
        rm -f "$kept"
        ;;
      *) echo "usage: guest.sh firewall public|back" >&2; exit 2 ;;
    esac
    firewall-cmd --get-active-zones
    ;;
  *)
    sed -n '5,19p' "$0" >&2
    exit 2
    ;;
esac
